package steps_test

// T055: machine.ErrWorkersNotReady means only that the step worker's own call got no complete
// answer: an agent's Ask and a procedure's RunProcedure. A call to another service inside an
// attempt (the check service's readStepFiles after the agent answered, the store, the source of
// approved parts or stored outputs) that fails the same way is an ordinary rejected attempt.
//
// Non-worker calls found in steps.go inside an attempt: agent(): readStepFiles on the check
// service (an HTTP call). procedure(): content() calls Config.Approved and Config.Load, and
// store() calls Config.Store (in-process callbacks that may reach a network service and then
// return its transport error); agent() also calls Config.Store. before() makes no call.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/steps"
)

// goodAgent makes the rig's agent answer completely, spending usage.
func goodAgent(r *rig) {
	r.agent.events = []string{info(pinned), usageEvent(100, 50, 0.001), usageEvent(1200, 300, 0.0042), choice(), stopped()}
}

// checkServiceAt points the rig's check service at handler, a replacement for the fake.
func checkServiceAt(t *testing.T, r *rig, handler http.HandlerFunc) {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	r.cfg.CheckURL = srv.URL
}

// refusedCheckService points the check service at an address where nothing listens.
func refusedCheckService(t *testing.T, r *rig) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r.cfg.CheckURL = "http://" + l.Addr().String()
	_ = l.Close()
}

func wantSpentUsage(t *testing.T, res machine.Result) {
	t.Helper()
	if res.Usage.Tokens == 0 || res.Usage.CostUSD == 0 {
		t.Fatalf("the usage the agent spent was not returned: %+v", res.Usage)
	}
}

// The check service's transport failures after the agent answered fully.
func TestOwnCallsReadStepFilesTransportFailureIsNotWorkersNotReady(t *testing.T) {
	cases := map[string]func(t *testing.T, r *rig){
		"connection closed without a response": func(t *testing.T, r *rig) { checkServiceAt(t, r, closeBare) },
		"connection reset":                     func(t *testing.T, r *rig) { checkServiceAt(t, r, resetConn) },
		"connection refused":                   refusedCheckService,
		"no HTTP in the answer":                func(t *testing.T, r *rig) { checkServiceAt(t, r, garbage) },
		"body cut after the headers": func(t *testing.T, r *rig) {
			checkServiceAt(t, r, rawChunked("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 500\r\n\r\n", false))
		},
		"body cut in the middle": func(t *testing.T, r *rig) {
			checkServiceAt(t, r, rawChunked("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 500\r\n\r\n{\"files\":[", true))
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			goodAgent(r)
			setup(t, r)
			res, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.agentReq(testsStep))
			wantOtherError(t, err)
			wantSpentUsage(t, res)
			if len(r.agent.rec.find("POST", "/api/sessions")) != 1 {
				t.Fatal("the agent was not asked once; the case does not test what it says")
			}
		})
	}
}

// An answer from the check service, whatever its status, is not WorkersNotReady (guard).
func TestOwnCallsReadStepFilesHTTPStatusIsNotWorkersNotReady(t *testing.T) {
	for _, code := range []int{400, 404, 500, 502, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			r := newRig(t)
			goodAgent(r)
			r.chk.status = code
			res, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.agentReq(testsStep))
			wantOtherError(t, err)
			wantSpentUsage(t, res)
		})
	}
}

// The error text still says which call failed, through the %w wrapping.
func TestOwnCallsReadStepFilesErrorStaysIdentified(t *testing.T) {
	r := newRig(t)
	goodAgent(r)
	checkServiceAt(t, r, closeBare)
	_, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.agentReq(testsStep))
	wantOtherError(t, err)
	if !errors.Is(err, io.EOF) && !errors.As(err, new(*url.Error)) {
		t.Fatalf("the transport error is lost: %v", err)
	}
	if got := err.Error(); !strings.Contains(got, "readStepFiles") {
		t.Fatalf("the error does not name the failed call: %v", got)
	}
}

// In-process sources an attempt calls may reach a network service; their transport errors are
// not the step worker's.
func TestOwnCallsCallbacksTransportErrorsAreNotWorkersNotReady(t *testing.T) {
	transport := map[string]error{
		"url.Error":  &url.Error{Op: "Get", URL: "http://store/x", Err: errors.New("connection refused")},
		"EOF":        fmt.Errorf("fetching: %w", io.EOF),
		"unexpected": io.ErrUnexpectedEOF,
		"reset":      fmt.Errorf("read: %w", syscall.ECONNRESET),
	}
	for name, terr := range transport {
		t.Run("procedure, approved parts, "+name, func(t *testing.T) {
			r := newRig(t)
			r.proc.proc = procResult(procFiles)
			r.cfg.Approved = func(string) (map[string][]byte, error) { return nil, terr }
			_, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.procReq())
			wantOtherError(t, err)
		})
		t.Run("procedure, store, "+name, func(t *testing.T) {
			r := newRig(t)
			r.proc.proc = procResult(procFiles)
			r.cfg.Store = func([]byte) (string, error) { return "", terr }
			_, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.procReq())
			wantOtherError(t, err)
		})
		t.Run("agent, store, "+name, func(t *testing.T) {
			r := newRig(t)
			goodAgent(r)
			r.cfg.Store = func([]byte) (string, error) { return "", terr }
			res, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.agentReq(testsStep))
			wantOtherError(t, err)
			wantSpentUsage(t, res)
		})
	}
	t.Run("procedure, stored input, url.Error", func(t *testing.T) {
		// an input an earlier step made is loaded from the store
		r := newRig(t)
		r.proc.proc = procResult(procFiles)
		req := r.procReq()
		req.Step.Inputs = []string{"slice/contract", "earlier"}
		r.cfg.Pattern.Steps[0].Inputs = req.Step.Inputs
		req.Inputs = []string{req.Inputs[0], hashA}
		r.cfg.Load = func(string) ([]byte, error) { return nil, transport["url.Error"] }
		_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
		wantOtherError(t, err)
	})
}

// The worker's own cut-off call stays WorkersNotReady (guard, one each).
func TestOwnCallsWorkersOwnCallCutIsStillNotReady(t *testing.T) {
	t.Run("agent Ask, stream cut mid-body", func(t *testing.T) {
		info := chunk(sse(map[string]any{"type": "agent_info", "model": pinned}))
		r, req := goneRig(t, testsStep, agentServer(rawChunked(sseHead+info+"4", false)))
		_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
		wantNotReady(t, err)
	})
	t.Run("agent Ask, connection closed", func(t *testing.T) {
		r, req := goneRig(t, testsStep, agentServer(closeBare))
		_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
		wantNotReady(t, err)
	})
	t.Run("agent Ask, connection refused", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		_ = l.Close()
		r := newRig(t)
		r.cfg.Endpoints = endpoints{testsStep: {"http://" + addr, "tok"}}
		_, err = steps.NewWorker(r.cfg).Run(ctx5(t), r.agentReq(testsStep))
		wantNotReady(t, err)
	})
	t.Run("procedure RunProcedure, connection closed", func(t *testing.T) {
		r, req := goneRig(t, "contract-check", closeBare)
		_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
		wantNotReady(t, err)
	})
	t.Run("procedure RunProcedure, connection reset", func(t *testing.T) {
		r, req := goneRig(t, "contract-check", resetConn)
		_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
		wantNotReady(t, err)
	})
	t.Run("a working check service does not hide it", func(t *testing.T) {
		// the check service is healthy; only the worker is cut
		r, req := goneRig(t, testsStep, agentServer(closeBare))
		_, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
		wantNotReady(t, err)
		if len(r.chk.rec.all()) != 0 {
			t.Fatal("files were read after a failed Ask")
		}
	})
}

// A caller that gave up is not WorkersNotReady for the non-worker call either.
func TestOwnCallsCallerGivingUpIsNotWorkersNotReady(t *testing.T) {
	hang := func(w http.ResponseWriter, q *http.Request) {
		_, _ = io.Copy(io.Discard, q.Body)
		<-q.Context().Done()
	}
	t.Run("cancelled while readStepFiles is open", func(t *testing.T) {
		r := newRig(t)
		goodAgent(r)
		checkServiceAt(t, r, hang)
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(300*time.Millisecond, cancel)
		res, err := steps.NewWorker(r.cfg).Run(ctx, r.agentReq(testsStep))
		wantOtherError(t, err)
		wantSpentUsage(t, res)
	})
	t.Run("deadline while readStepFiles is open", func(t *testing.T) {
		r := newRig(t)
		goodAgent(r)
		checkServiceAt(t, r, hang)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		_, err := steps.NewWorker(r.cfg).Run(ctx, r.agentReq(testsStep))
		wantOtherError(t, err)
	})
	t.Run("cancelled, then readStepFiles fails by transport", func(t *testing.T) {
		r := newRig(t)
		goodAgent(r)
		ctx, cancel := context.WithCancel(context.Background())
		checkServiceAt(t, r, func(w http.ResponseWriter, q *http.Request) { cancel(); closeBare(w, q) })
		_, err := steps.NewWorker(r.cfg).Run(ctx, r.agentReq(testsStep))
		wantOtherError(t, err)
	})
	t.Run("cancelled, a callback returns a transport error", func(t *testing.T) {
		r := newRig(t)
		r.proc.proc = procResult(procFiles)
		ctx, cancel := context.WithCancel(context.Background())
		r.cfg.Store = func([]byte) (string, error) {
			cancel()
			return "", &url.Error{Op: "Post", URL: "http://s", Err: io.EOF}
		}
		_, err := steps.NewWorker(r.cfg).Run(ctx, r.procReq())
		wantOtherError(t, err)
	})
}
