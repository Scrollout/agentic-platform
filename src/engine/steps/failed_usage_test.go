package steps_test

// T054c review: an agent attempt that fails after spending reports what it spent in the failed
// Result, so the run's budget counts it (Principle X). The usage is the last token_usage event's
// totals (research D29): tokens are input plus output.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/steps"
)

func usageEvent(in, out int, cost float64) string {
	return sse(map[string]any{"type": "token_usage", "session_id": "sess-1",
		"usage": map[string]any{"input_tokens": in, "output_tokens": out, "cost": cost}})
}

func wantSpent(t *testing.T, res machine.Result, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("the attempt must be an error")
	}
	if res.Usage.Tokens != 1500 || res.Usage.CostUSD != 0.0042 {
		t.Fatalf("usage of the failed attempt = %+v, want 1500 tokens and $0.0042 (the last token_usage)", res.Usage)
	}
}

func info(model string) string { return sse(map[string]any{"type": "agent_info", "model": model}) }
func choice() string           { return sse(map[string]any{"type": "agent_choice", "content": "x"}) }
func stopped() string          { return sse(map[string]any{"type": "stream_stopped", "reason": "normal"}) }

func TestFailedAttemptReportsUsage_ModelMismatch(t *testing.T) {
	r := newRig(t)
	r.agent.events = []string{info("anthropic/claude-opus-9"), usageEvent(100, 50, 0.001), usageEvent(1200, 300, 0.0042), choice(), stopped()}
	res, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.agentReq(testsStep))
	wantSpent(t, res, err)
}

func TestFailedAttemptReportsUsage_StreamErrorEvent(t *testing.T) {
	r := newRig(t)
	r.agent.events = []string{info(pinned), usageEvent(100, 50, 0.001), usageEvent(1200, 300, 0.0042),
		sse(map[string]any{"type": "error", "error": "boom"})}
	res, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.agentReq(testsStep))
	wantSpent(t, res, err)
}

func TestFailedAttemptReportsUsage_ReadStepFilesFails(t *testing.T) {
	r := newRig(t)
	r.agent.events = []string{info(pinned), usageEvent(100, 50, 0.001), usageEvent(1200, 300, 0.0042), choice(), stopped()}
	r.chk.status = 500
	res, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.agentReq(testsStep))
	wantSpent(t, res, err)
}

func TestFailedAttemptReportsUsage_ContextTimeoutMidStream(t *testing.T) {
	r := newRig(t)
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if strings.HasSuffix(q.URL.Path, "/agent/tests") {
			for _, e := range []string{info(pinned), usageEvent(100, 50, 0.001), usageEvent(1200, 300, 0.0042)} {
				fmt.Fprint(w, e)
			}
			w.(http.Flusher).Flush()
			select {
			case <-stuck:
			case <-q.Context().Done():
			}
			return
		}
		fmt.Fprint(w, `{"id":"sess-1"}`)
	}))
	t.Cleanup(srv.Close)
	r.cfg.Endpoints = endpoints{testsStep: {srv.URL, agentTok}}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	res, err := steps.NewWorker(r.cfg).Run(ctx, r.agentReq(testsStep))
	wantSpent(t, res, err)
}
