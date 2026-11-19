package steps_test

// T048: a step worker that is gone while a call is open (it closed the connection, reset it, or
// cut the stream) is ErrWorkersNotReady, like a refused dial; a worker that answered, however
// badly, is not, and neither is a caller that gave up (research R10, "Found next 2").

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/steps"
)

// goneRig is a rig whose step workers are one fake server running handler.
func goneRig(t *testing.T, step string, handler http.HandlerFunc) (*rig, machine.Request) {
	r := newRig(t)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	r.cfg.Endpoints = endpoints{step: {srv.URL, "tok"}}
	if step == testsStep {
		return r, r.agentReq(step)
	}
	return r, r.procReq()
}

// closeBare closes the connection without a response.
func closeBare(w http.ResponseWriter, _ *http.Request) {
	c, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		_ = c.Close()
	}
}

// resetConn resets the connection (RST, not FIN).
func resetConn(w http.ResponseWriter, _ *http.Request) {
	c, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetLinger(0)
	}
	_ = c.Close()
}

// agentServer answers the session create and delete, and run for the agent route.
func agentServer(run http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, q *http.Request) {
		if strings.HasSuffix(q.URL.Path, "/agent/tests") {
			run(w, q)
			return
		}
		if q.Method == http.MethodPost {
			_, _ = fmt.Fprint(w, `{"id":"sess-1"}`)
		}
	}
}

// rawChunked hijacks, writes raw (headers and a partial chunked body) and closes, reset or not.
func rawChunked(raw string, reset bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		c, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_, _ = fmt.Fprint(c, raw)
		if tc, ok := c.(*net.TCPConn); ok && reset {
			_ = tc.SetLinger(0)
		}
		time.Sleep(50 * time.Millisecond) // let the client read what was sent
		_ = c.Close()
	}
}

const sseHead = "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n"

func chunk(s string) string { return fmt.Sprintf("%x\r\n%s\r\n", len(s), s) }

func wantNotReady(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, machine.ErrWorkersNotReady) {
		t.Fatalf("want ErrWorkersNotReady, got %v", err)
	}
}

func wantOtherError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("want an error")
	}
	if errors.Is(err, machine.ErrWorkersNotReady) {
		t.Fatalf("must not be ErrWorkersNotReady: %v", err)
	}
}

func TestWorkerGoneProcedureClosedWithoutResponse(t *testing.T) {
	r, req := goneRig(t, "contract-check", closeBare)
	_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
	wantNotReady(t, err)
	// repeated calls give the same answer
	_, err = steps.NewWorker(r.cfg).Run(ctx5(t), req)
	wantNotReady(t, err)
}

func TestWorkerGoneProcedureConnectionReset(t *testing.T) {
	r, req := goneRig(t, "contract-check", resetConn)
	_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
	wantNotReady(t, err)
}

func TestWorkerGoneProcedureCutInTheBody(t *testing.T) {
	// headers and part of the body promised by Content-Length, then closed
	for name, raw := range map[string]string{
		"after the headers, 0 bytes of body": "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 500\r\n\r\n",
		"partial body":                       "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 500\r\n\r\n{\"procedure\":\"con",
	} {
		t.Run(name, func(t *testing.T) {
			r, req := goneRig(t, "contract-check", rawChunked(raw, false))
			_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
			wantNotReady(t, err)
		})
	}
}

func TestWorkerGoneAgentRunClosedWithoutResponse(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{"closed": closeBare, "reset": resetConn} {
		t.Run(name, func(t *testing.T) {
			r, req := goneRig(t, testsStep, agentServer(h))
			_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
			wantNotReady(t, err)
		})
	}
}

func TestWorkerGoneAgentStreamCutMidBody(t *testing.T) {
	info := chunk(sse(map[string]any{"type": "agent_info", "model": pinned}))
	choice := chunk(sse(map[string]any{"type": "agent_choice", "content": "partial"}))
	cases := map[string]string{
		"after the headers, 0 bytes": sseHead,
		"after some events":          sseHead + info + choice,
		"inside a chunk":             sseHead + info + "40\r\ndata: {\"type\":\"agent_ch",
		"inside a chunk header":      sseHead + info + "4",
	}
	for name, raw := range cases {
		for _, reset := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s reset=%v", name, reset), func(t *testing.T) {
				r, req := goneRig(t, testsStep, agentServer(rawChunked(raw, reset)))
				_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
				wantNotReady(t, err)
				if len(r.chk.rec.all()) != 0 {
					t.Fatal("files were read after a cut stream")
				}
			})
		}
	}
}

func TestWorkerGoneRefusedDialIsStillNotReady(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close() // nothing listens there now
	for _, step := range []string{"contract-check", testsStep} {
		r := newRig(t)
		r.cfg.Endpoints = endpoints{step: {"http://" + addr, "tok"}}
		req := r.procReq()
		if step == testsStep {
			req = r.agentReq(step)
		}
		_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
		wantNotReady(t, err)
	}
}

func TestWorkerAnsweredIsNotWorkersNotReady(t *testing.T) {
	t.Run("procedure HTTP 500 with a body", func(t *testing.T) {
		r, req := goneRig(t, "contract-check", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(500)
			_, _ = fmt.Fprint(w, `{"type":"x","title":"boom","code":"Internal"}`)
		})
		_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
		wantOtherError(t, err)
	})
	t.Run("procedure HTTP 500 with an empty body", func(t *testing.T) {
		r, req := goneRig(t, "contract-check", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
		_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
		wantOtherError(t, err)
	})
	t.Run("procedure 200 with an empty body", func(t *testing.T) {
		r, req := goneRig(t, "contract-check", func(w http.ResponseWriter, _ *http.Request) {})
		_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
		wantOtherError(t, err)
	})
	t.Run("agent run HTTP 500", func(t *testing.T) {
		r, req := goneRig(t, testsStep, agentServer(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(500)
			_, _ = fmt.Fprint(w, "boom")
		}))
		_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
		wantOtherError(t, err)
	})
	t.Run("agent error event is ErrModelUnavailable", func(t *testing.T) {
		r, req := goneRig(t, testsStep, agentServer(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, sse(map[string]any{"type": "agent_info", "model": pinned}), sse(map[string]any{"type": "error", "error": "boom"}))
		}))
		_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
		wantOtherError(t, err)
		if !errors.Is(err, machine.ErrModelUnavailable) {
			t.Fatalf("want ErrModelUnavailable, got %v", err)
		}
	})
}

func TestWorkerCallerGivingUpIsNotWorkersNotReady(t *testing.T) {
	hang := func(w http.ResponseWriter, q *http.Request) {
		_, _ = io.Copy(io.Discard, q.Body) // so the server notices the client leaving
		<-q.Context().Done()
	}
	run := func(t *testing.T, step string, h http.HandlerFunc, ctx context.Context) error {
		r, req := goneRig(t, step, h)
		_, err := steps.NewWorker(r.cfg).Run(ctx, req)
		return err
	}
	for _, step := range []string{"contract-check", testsStep} {
		h := http.HandlerFunc(hang)
		if step == testsStep {
			h = agentServer(hang)
		}
		t.Run(step+" cancelled while the call is open", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(200*time.Millisecond, cancel)
			wantOtherError(t, run(t, step, h, ctx))
		})
		t.Run(step+" deadline while the call is open", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			wantOtherError(t, run(t, step, h, ctx))
		})
		t.Run(step+" cancelled before the call", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			wantOtherError(t, run(t, step, h, ctx))
		})
	}
}

// T051: a 200 stream that ends without stream_stopped means the worker ended it, so it is gone.
func TestWorkerGoneAgentStreamEndsWithoutStreamStopped(t *testing.T) {
	info := sse(map[string]any{"type": "agent_info", "model": pinned})
	usage := sse(map[string]any{"type": "token_usage", "session_id": "s", "usage": map[string]any{"input_tokens": 3, "output_tokens": 4, "cost": 0.001}})
	run := func(t *testing.T, body ...string) error {
		r, req := goneRig(t, testsStep, agentServer(func(w http.ResponseWriter, _ *http.Request) {
			for _, b := range body {
				_, _ = fmt.Fprint(w, b)
			}
		}))
		_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
		return err
	}
	t.Run("complete events but without stream_stopped", func(t *testing.T) {
		wantNotReady(t, run(t, info, sse(map[string]any{"type": "agent_choice", "content": "x"})))
	})
	t.Run("empty body", func(t *testing.T) { wantNotReady(t, run(t)) })
	t.Run("only agent_info and token_usage, clean end", func(t *testing.T) { wantNotReady(t, run(t, info, usage)) })
	t.Run("a last event without its blank line", func(t *testing.T) {
		wantNotReady(t, run(t, info, strings.TrimSuffix(sse(map[string]any{"type": "agent_choice", "content": "x"}), "\n\n")))
	})
	t.Run("stream_stopped with reason error was answered", func(t *testing.T) {
		wantOtherError(t, run(t, info, sse(map[string]any{"type": "agent_choice", "content": "x"}), sse(map[string]any{"type": "stream_stopped", "reason": "error"})))
	})
	t.Run("stream_stopped normal but no agent_choice was answered", func(t *testing.T) {
		wantOtherError(t, run(t, info, sse(map[string]any{"type": "stream_stopped", "reason": "normal"})))
	})
}
