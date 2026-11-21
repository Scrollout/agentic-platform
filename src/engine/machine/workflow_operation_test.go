package machine_test

// T023 (feature 002, user story 2), the Operation workflow: written before the code (T025) by a
// test author, with the activities mocked in Temporal's test environment, as the Run workflow's
// tests do. Judged from FR-010, the data model ("Operation record": the work, Ending; "Execution
// state in Temporal"; the sketch rows Operation, DoOperation, EndOperation; "Retry rule"),
// research R3 and R5.
//
// Interface assumed, fixed by the task brief (package durable):
//
//	func Operation(ctx workflow.Context, in OperationInput) error // workflow type "Operation"
//	type OperationInput struct{ ID, Kind, Target string }
//	type EndOperationInput struct{ ID, Status, Detail string }
//	// activities "DoOperation" (input: the operation ID) and "EndOperation" (EndOperationInput)
//
// DoOperation's retryable failure is a Temporal ApplicationError of type "Retryable"; the
// workflow retries it by the one retry rule (src/retry, the engine contract's x-limits) with
// durable sleeps, and when the retries are used up EndOperation writes `failed` with `failed
// after <n> attempts: <reason>`. Any other DoOperation error is not retried, except a heartbeat
// timeout (a lost engine), which repeats DoOperation without counting a retry. A cancelled
// workflow writes `cancelled` through EndOperation.
//
// Assumed beyond the brief: the workflow's result is not judged (only its calls); the retry waits
// are judged to lie within the rule's bounds for the random part in [0, 1], as the workflow may
// draw it however it likes deterministically; a DoOperation that returns nil ends the workflow
// with no EndOperation (DoOperation wrote the end). The replayed history of a real stop is not
// reproduced: a stop is a heartbeat timeout between retries, which is how Temporal shows a lost
// engine to the workflow.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/durable"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

const opReason = "the analyst cannot be reached"

type opCall struct {
	name    string
	in      any
	attempt int32
	at      time.Time
}

type opHarness struct {
	t   *testing.T
	env *testsuite.TestWorkflowEnvironment
	// do answers the n-th DoOperation call (from 1); nil: nil.
	do    func(ctx context.Context, n int) error
	mu    sync.Mutex
	calls []opCall
}

func newOpWF(t *testing.T) *opHarness {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	h := &opHarness{t: t, env: suite.NewTestWorkflowEnvironment()}
	h.env.SetStartTime(wfT0)
	h.env.SetTestTimeout(30 * time.Second)
	h.env.RegisterWorkflowWithOptions(durable.Operation, workflow.RegisterOptions{Name: "Operation"})
	h.env.RegisterActivityWithOptions(func(ctx context.Context, id string) error {
		n := h.log(ctx, "DoOperation", id)
		if h.do == nil {
			return nil
		}
		return h.do(ctx, n)
	}, activity.RegisterOptions{Name: "DoOperation"})
	h.env.RegisterActivityWithOptions(func(ctx context.Context, in durable.EndOperationInput) error {
		h.log(ctx, "EndOperation", in)
		return nil
	}, activity.RegisterOptions{Name: "EndOperation"})
	return h
}

// log records a call and returns how many calls of that name were made, this one included.
func (h *opHarness) log(ctx context.Context, name string, in any) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, opCall{name: name, in: in, attempt: activity.GetInfo(ctx).Attempt, at: h.env.Now()})
	n := 0
	for _, c := range h.calls {
		if c.name == name {
			n++
		}
	}
	return n
}

func (h *opHarness) run() {
	h.t.Helper()
	h.env.ExecuteWorkflow("Operation", durable.OperationInput{ID: "OP-7", Kind: "createSlice", Target: "S-3"})
	if !h.env.IsWorkflowCompleted() {
		h.t.Fatal("the Operation workflow did not complete")
	}
}

func (h *opHarness) of(name string) []opCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []opCall
	for _, c := range h.calls {
		if c.name == name {
			out = append(out, c)
		}
	}
	return out
}

func opRetryable() error {
	return temporal.NewApplicationError(opReason, "Retryable")
}

// wantWaits checks each DoOperation call after a retryable failure waited the rule's retry wait
// for its retry number, the random part within [0, Jitter].
func (h *opHarness) wantWaits(calls []opCall, retries []int) {
	h.t.Helper()
	rule := wfRetryRule(h.t)
	for i, k := range retries {
		low := rule.Wait(k, func() float64 { return 0 })
		high := rule.Wait(k, func() float64 { return 1 })
		if got := calls[i+1].at.Sub(calls[i].at); got < low || got > high {
			h.t.Fatalf("the wait before retry %d is %s, want between %s and %s (src/retry)", k, got, low, high)
		}
	}
}

func TestOperationWorkflowRetriesARetryableFailureByTheRuleThenEndsItFailed(t *testing.T) {
	rule := wfRetryRule(t)
	h := newOpWF(t)
	h.do = func(context.Context, int) error { return opRetryable() }
	h.run()
	do := h.of("DoOperation")
	if len(do) != rule.Retries+1 {
		t.Fatalf("DoOperation called %d times, want %d (the first and %d retries)", len(do), rule.Retries+1, rule.Retries)
	}
	for _, c := range do {
		if c.in != "OP-7" {
			t.Fatalf("DoOperation's input = %v, want the operation ID OP-7", c.in)
		}
		if c.attempt != 1 {
			t.Fatalf("Temporal retried DoOperation itself (attempt %d): the retries are the workflow's", c.attempt)
		}
	}
	retries := make([]int, rule.Retries)
	for k := range retries {
		retries[k] = k
	}
	h.wantWaits(do, retries)
	end := h.of("EndOperation")
	want := durable.EndOperationInput{ID: "OP-7", Status: "failed", Detail: fmt.Sprintf("failed after %d attempts: %s", rule.Retries+1, opReason)}
	if len(end) != 1 || end[0].in != want {
		t.Fatalf("EndOperation calls %+v, want one with %+v", end, want)
	}
	if !end[0].at.After(do[len(do)-1].at) && !end[0].at.Equal(do[len(do)-1].at) {
		t.Fatalf("EndOperation came before the last DoOperation")
	}
}

func TestOperationWorkflowARetryThatSucceedsEndsWithNoEndOperation(t *testing.T) {
	h := newOpWF(t)
	h.do = func(_ context.Context, n int) error {
		if n <= 2 {
			return opRetryable()
		}
		return nil
	}
	h.run()
	if n := len(h.of("DoOperation")); n != 3 {
		t.Fatalf("DoOperation called %d times, want 3", n)
	}
	h.wantWaits(h.of("DoOperation"), []int{0, 1})
	if end := h.of("EndOperation"); len(end) != 0 {
		t.Fatalf("EndOperation called %+v after DoOperation succeeded (it wrote the end)", end)
	}
}

// A lost engine between retries (a heartbeat timeout) repeats DoOperation without counting a
// retry, and the retries made before it still count: four retryable failures in all, with one
// stop among them, end the operation failed after 4 attempts.
func TestOperationWorkflowAHeartbeatTimeoutRepeatsDoOperationWithoutCountingARetry(t *testing.T) {
	rule := wfRetryRule(t)
	stopAt := 3 // the third call is lost: after two retryable failures
	h := newOpWF(t)
	h.do = func(_ context.Context, n int) error {
		if n == stopAt {
			return temporal.NewHeartbeatTimeoutError()
		}
		return opRetryable()
	}
	h.run()
	do := h.of("DoOperation")
	if len(do) != rule.Retries+2 {
		t.Fatalf("DoOperation called %d times, want %d (%d attempts and the one repeat after the stop)", len(do), rule.Retries+2, rule.Retries+1)
	}
	end := h.of("EndOperation")
	want := durable.EndOperationInput{ID: "OP-7", Status: "failed", Detail: fmt.Sprintf("failed after %d attempts: %s", rule.Retries+1, opReason)}
	if len(end) != 1 || end[0].in != want {
		t.Fatalf("EndOperation calls %+v, want one with %+v: the stop is no attempt and the retries before it count", end, want)
	}
	// The retry after the repeat waits the rule's wait for retry 2, not retry 0 again.
	h.wantWaits(do[stopAt:], []int{2})
}

func TestOperationWorkflowAHeartbeatTimeoutAloneIsRepeatedUntilTheWorkEnds(t *testing.T) {
	rule := wfRetryRule(t)
	stops := rule.Retries + 2 // more stops than the rule has retries
	h := newOpWF(t)
	h.do = func(_ context.Context, n int) error {
		if n <= stops {
			return temporal.NewHeartbeatTimeoutError()
		}
		return nil
	}
	h.run()
	if n := len(h.of("DoOperation")); n != stops+1 {
		t.Fatalf("DoOperation called %d times, want %d: a stop never uses up the retries", n, stops+1)
	}
	if end := h.of("EndOperation"); len(end) != 0 {
		t.Fatalf("EndOperation called %+v, want none", end)
	}
}

func TestOperationWorkflowAnErrorThatIsNotRetryableIsNotRetried(t *testing.T) {
	h := newOpWF(t)
	h.do = func(context.Context, int) error {
		return temporal.NewNonRetryableApplicationError("the record cannot be read", "Defect", nil)
	}
	h.run()
	if n := len(h.of("DoOperation")); n != 1 {
		t.Fatalf("DoOperation called %d times, want 1", n)
	}
}

func TestOperationWorkflowCancelledEndsTheOperationCancelled(t *testing.T) {
	t.Run("while its work runs", func(t *testing.T) {
		h := newOpWF(t)
		// The work cancels the workflow once it runs, and then waits (real time, at most 5 s) for its
		// context to end, heartbeating as the real activity does.
		var once sync.Once
		h.do = func(ctx context.Context, _ int) error {
			once.Do(func() { go h.env.CancelWorkflow() })
			deadline := time.Now().Add(5 * time.Second)
			for ctx.Err() == nil && time.Now().Before(deadline) {
				activity.RecordHeartbeat(ctx)
				time.Sleep(10 * time.Millisecond)
			}
			if ctx.Err() == nil {
				return errors.New("the work's context was not cancelled")
			}
			return ctx.Err()
		}
		h.run()
		h.wantCancelled()
	})
	t.Run("while it waits to retry", func(t *testing.T) {
		h := newOpWF(t)
		h.do = func(context.Context, int) error { return opRetryable() }
		first := wfRetryRule(t).Wait(0, func() float64 { return 0 })
		h.env.RegisterDelayedCallback(func() { h.env.CancelWorkflow() }, first/2)
		h.run()
		if n := len(h.of("DoOperation")); n != 1 {
			t.Fatalf("DoOperation called %d times, want 1: cancelled during the first wait", n)
		}
		h.wantCancelled()
	})
}

func (h *opHarness) wantCancelled() {
	h.t.Helper()
	end := h.of("EndOperation")
	want := durable.EndOperationInput{ID: "OP-7", Status: "cancelled"}
	if len(end) != 1 {
		h.t.Fatalf("EndOperation calls %+v, want one with %+v", end, want)
	}
	got := end[0].in.(durable.EndOperationInput)
	if got.ID != want.ID || got.Status != want.Status || strings.HasPrefix(got.Detail, "failed") {
		h.t.Fatalf("EndOperation = %+v, want %+v", got, want)
	}
	if err := h.env.GetWorkflowError(); err != nil && !errors.Is(err, context.Canceled) && !temporal.IsCanceledError(err) {
		h.t.Logf("the workflow ended with %v", err)
	}
}
