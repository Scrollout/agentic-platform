package machine_test

// T046 (owner decision 2026-11-19, research R10 "Found next"; data model, Run states): a step
// worker call that fails WorkersNotReady is made again by the one retry rule (src/retry, the engine
// contract's x-limits: 3 retries, durable sleeps whose random part comes from a side effect), and
// nothing is recorded for the retries. A retry that succeeds continues the run with no
// RunInterrupted; only when all 4 calls fail does the run record RunInterrupted `the step workers
// are not running` and wait for workers-ready, as T012 (g) says. The interface is in
// workflow_durable_test.go.

import (
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/retry"
	"go.temporal.io/sdk/temporal"
)

// wfRandom is what the mocked side effect gives as the retry wait's random part.
const wfRandom = 0.8

// wfRetryRule is the declared retry rule, read from the engine contract as case (j) reads it.
func wfRetryRule(t *testing.T) retry.Rule {
	t.Helper()
	doc, err := os.ReadFile("../api/preview/2026-11-12-preview/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := retry.Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

// wfWorkersGoneForCalls makes the first n calls of step fail WorkersNotReady; every other call
// succeeds.
func wfWorkersGoneForCalls(step string, n int) func(durable.WorkerCall) (machine.Result, error) {
	var mu sync.Mutex
	seen := 0
	return func(c durable.WorkerCall) (machine.Result, error) {
		if c.Step.Name == step {
			mu.Lock()
			seen++
			gone := seen <= n
			mu.Unlock()
			if gone {
				return machine.Result{}, temporal.NewNonRetryableApplicationError(wfNoWorkers, durable.WorkersNotReady, nil)
			}
		}
		return wfOK(c), nil
	}
}

// workerCallsAt are the CallWorker calls of step as the fake saw them, in order.
func (h *wfHarness) workerCallsAt(step string) []wfCall {
	var out []wfCall
	for _, c := range h.callsOf(durable.CallWorker) {
		if c.In.(durable.WorkerCall).Step.Name == step {
			out = append(out, c)
		}
	}
	return out
}

// wfEventSigs is the record without its times: kind, step, attempt, round and detail of every event.
func wfEventSigs(ev []machine.Event) []string {
	out := make([]string, len(ev))
	for i, e := range ev {
		out[i] = fmt.Sprintf("%s/%s/%d/%d/%s", e.Kind, e.Step, e.Attempt, e.Round, e.Detail)
	}
	return out
}

// wantRuleWaits checks that the first n+1 calls are n retries apart by the rule's waits, with the
// mocked side effect's random part, and that Temporal itself retried none of them.
func wantRuleWaits(t *testing.T, rule retry.Rule, calls []wfCall, n int) {
	t.Helper()
	for _, c := range calls {
		if c.Attempt != 1 {
			t.Fatalf("Temporal retried a worker call (its attempt %d): the retries are the workflow's", c.Attempt)
		}
	}
	for k := 0; k < n; k++ {
		want := rule.Wait(k, func() float64 { return wfRandom })
		if got := calls[k+1].At.Sub(calls[k].At); got != want {
			t.Fatalf("retry %d of the worker call waited %s, want %s (src/retry with the side effect's %v)", k, got, want, wfRandom)
		}
	}
}

// (a) A worker gone for the first 1, 2 or 3 calls of a step: the call is made again by the retry
// rule with the same attempt number, the run ends done with no RunInterrupted and no RunResumed,
// and (c) nothing is recorded for the retries: the record is that of an uninterrupted run.
func TestWorkflowWorkersNotReadyCallIsRetriedByTheRule(t *testing.T) {
	rule := wfRetryRule(t)
	if rule.Retries != 3 {
		t.Fatalf("the declared rule has %d retries; this test is written for 3", rule.Retries)
	}

	base := newWF(t, wfPattern())
	base.run()
	base.wantOutcome(machine.OutcomeDone)
	uninterrupted := wfEventSigs(base.events())

	for gone := 1; gone <= rule.Retries; gone++ {
		t.Run(fmt.Sprintf("gone for %d calls", gone), func(t *testing.T) {
			h := newWF(t, wfPattern())
			h.env.OnSideEffect().Return(wfRandom)
			h.work = wfWorkersGoneForCalls("tests", gone)
			h.run()

			if in, res := h.eventsOf("RunInterrupted"), h.eventsOf("RunResumed"); len(in) != 0 || len(res) != 0 {
				t.Fatalf("RunInterrupted %+v, RunResumed %+v, want neither: a retry that succeeds continues the run", in, res)
			}
			h.wantOutcome(machine.OutcomeDone)
			calls := h.workerCallsAt("tests")
			if len(calls) != gone+1 {
				t.Fatalf("tests called %d times, want %d (%d refused, then the retry that succeeds)", len(calls), gone+1, gone)
			}
			if got, want := attemptsOf(h.workerCalls("tests")), slices.Repeat([]int{1}, gone+1); !slices.Equal(got, want) {
				t.Fatalf("tests attempts = %v, want %v: a retry is the same call", got, want)
			}
			wantRuleWaits(t, rule, calls, gone)
			if got := wfEventSigs(h.events()); !slices.Equal(got, uninterrupted) {
				t.Fatalf("the record differs from an uninterrupted run's: a retry recorded something\n got %v\nwant %v", got, uninterrupted)
			}
			wfValidate(t, h.events())
		})
	}
}

// (b) A worker gone for all 4 calls (the first and the rule's 3 retries): after the 4th call the
// run records exactly one RunInterrupted `the step workers are not running` and calls no more
// until workers-ready; then it records RunResumed, makes the same call again and continues.
func TestWorkflowWorkersNotReadyOnEveryRetryWaitsForWorkersReady(t *testing.T) {
	rule := wfRetryRule(t)
	all := rule.Retries + 1
	base := newWF(t, wfPattern())
	base.run()
	base.wantOutcome(machine.OutcomeDone)

	h := newWF(t, wfPattern())
	h.env.OnSideEffect().Return(wfRandom)
	h.work = wfWorkersGoneForCalls("tests", all)
	const readyAt = 2 * time.Hour
	var calls int
	var waiting []string
	h.signalAt(readyAt, durable.SignalWorkersReady, func() { calls, waiting = len(h.workerCalls("tests")), h.kinds() })
	h.run()

	if calls != all {
		t.Fatalf("tests called %d times before workers-ready, want %d (the first and %d retries, then the wait)", calls, all, rule.Retries)
	}
	if len(waiting) == 0 || waiting[len(waiting)-1] != "RunInterrupted" {
		t.Fatalf("while waiting the record ends %v, want RunInterrupted", waiting)
	}
	in := h.eventsOf("RunInterrupted")
	if len(in) != 1 || in[0].Detail != wfNoWorkers {
		t.Fatalf("RunInterrupted = %+v, want one with %q", in, wfNoWorkers)
	}
	h.wantOutcome(machine.OutcomeDone)
	at := h.workerCallsAt("tests")
	wantRuleWaits(t, rule, at, rule.Retries)
	if in[0].At.Before(at[all-1].At) {
		t.Fatalf("RunInterrupted at %s, before the %dth call at %s: the run waits only after every retry failed", in[0].At, all, at[all-1].At)
	}
	if len(at) != all+1 || at[all].At.Before(wfT0.Add(readyAt)) {
		t.Fatalf("tests called %d times, the last at %v; want %d, the last after workers-ready", len(at), at[len(at)-1].At, all+1)
	}
	i := h.index("RunInterrupted", 0)
	if h.index("RunResumed", i) != i+1 {
		t.Fatalf("RunResumed does not follow the interruption: %v", h.kinds())
	}
	if got, want := attemptsOf(h.workerCalls("tests")), slices.Repeat([]int{1}, all+1); !slices.Equal(got, want) {
		t.Fatalf("tests attempts = %v, want %v", got, want)
	}
	// without its one interruption pair the record is an uninterrupted run's: the retries, before
	// and after the wait, recorded nothing
	ev := h.events()
	rest := wfEventSigs(slices.Delete(slices.Clone(ev), i, i+2))
	if want := wfEventSigs(base.events()); !slices.Equal(rest, want) {
		t.Fatalf("without RunInterrupted and RunResumed the record differs from an uninterrupted run's\n got %v\nwant %v", rest, want)
	}
	wfValidate(t, ev)
}
