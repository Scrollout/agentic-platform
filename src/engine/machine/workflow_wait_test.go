package machine_test

// T012 cases (f) to (j): what a run waits for, and how a failing restore or commit is retried. The
// interface is in workflow_durable_test.go.

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/retry"
	"go.temporal.io/sdk/temporal"
)

const wfDay = 24 * time.Hour

// wfFailCode makes every check of the code step fail while *failing is true.
func wfFailCode(failing *bool) func(durable.CheckCall) (machine.CheckResult, error) {
	return func(c durable.CheckCall) (machine.CheckResult, error) {
		if c.Step.Name == "code" && *failing {
			return wfFail(c), nil
		}
		return wfDefaultCheck(c)
	}
}

// (f) A hand-over waits for resume, accept-hand-over or cancel, and expires 14 days after the
// hand-over: the hand-over here comes 3 days into the run (a wait for workers before it), so an
// expiry counted from the start, or from any later point, is caught. A resume after a hand-over
// starts the resumed step's attempts at 1.
func TestWorkflowHandOverWaitsForTheDeveloper(t *testing.T) {
	setup := func(t *testing.T, failing *bool) *wfHarness {
		h := newWF(t, wfPattern())
		*failing = true
		h.check = wfFailCode(failing)
		h.work = wfWorkersGoneForCalls("tests", 4) // the first call and the retry rule's 3 retries
		h.signalAt(3*wfDay, durable.SignalWorkersReady, nil)
		return h
	}
	handedOverAt := func(h *wfHarness) time.Time {
		ho := h.eventsOf("RunHandedOver")
		if len(ho) != 1 {
			h.t.Fatalf("hand-overs = %d, want 1: %v", len(ho), h.kinds())
		}
		return ho[0].At
	}

	t.Run("expires 14 days after the hand-over", func(t *testing.T) {
		var failing bool
		h := setup(t, &failing)
		var early []string
		h.env.RegisterDelayedCallback(func() { early = h.kinds() }, 3*wfDay+14*wfDay-time.Minute)
		h.run()
		h.wantOutcome(machine.OutcomeExpired)
		if slices.Contains(early, "RunExpired") {
			t.Fatal("expired before 14 days after the hand-over")
		}
		exp := h.eventsOf("RunExpired")
		if len(exp) != 1 || !strings.HasPrefix(exp[0].Detail, "the run waited 14 days for the developer") {
			t.Fatalf("RunExpired = %+v", exp)
		}
		if d := exp[0].At.Sub(handedOverAt(h)); d != 14*wfDay {
			t.Fatalf("expired %s after the hand-over, want 336h", d)
		}
		if last := h.kinds()[len(h.kinds())-1]; last != "RunExpired" {
			t.Fatalf("last event %s, want RunExpired", last)
		}
	})

	t.Run("accept-hand-over", func(t *testing.T) {
		var failing bool
		h := setup(t, &failing)
		h.signalAt(5*wfDay, durable.SignalAcceptHandOver, nil)
		h.run()
		h.wantOutcome(machine.OutcomeHandedOver)
		if k := h.kinds(); k[len(k)-1] != "HandOverAccepted" || len(h.eventsOf("HandOverAccepted")) != 1 {
			t.Fatalf("events %v, want one HandOverAccepted, last", k)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		var failing bool
		h := setup(t, &failing)
		h.signalAt(5*wfDay, durable.SignalCancel, nil)
		h.run()
		h.wantOutcome(machine.OutcomeCancelled)
		if k := h.kinds(); k[len(k)-1] != "RunCancelled" {
			t.Fatalf("events %v, want RunCancelled last", k)
		}
	})

	t.Run("resume starts the step's attempts at 1", func(t *testing.T) {
		var failing bool
		h := setup(t, &failing)
		var before int
		h.signalAt(5*wfDay, durable.SignalResume, func() { before = len(h.workerCalls("code")); failing = false })
		h.run()
		h.wantOutcome(machine.OutcomeDone)
		if before != 3 {
			t.Fatalf("code ran %d times before the resume, want 3", before)
		}
		if got := attemptsOf(h.workerCalls("code")); !slices.Equal(got, []int{1, 2, 3, 1}) {
			t.Fatalf("code attempts = %v, want [1 2 3 1]", got)
		}
		res := h.eventsOf("RunResumed")
		if len(res) < 2 || res[len(res)-1].Detail != "" { // the workers' RunResumed, then the developer's
			t.Fatalf("RunResumed = %+v, want the developer's without a detail", res)
		}
	})
}

// (g) A worker call failing WorkersNotReady is first made again by the retry rule (T046); when
// the first call and its 3 retries all fail, the run records RunInterrupted `the step workers are
// not running`, waits for workers-ready without calling again, then records RunResumed and
// continues.
func TestWorkflowWaitsForWorkersReady(t *testing.T) {
	h := newWF(t, wfPattern())
	h.work = wfWorkersGoneForCalls("tests", 4)
	var calls int
	var waiting []string
	h.signalAt(2*time.Hour, durable.SignalWorkersReady, func() { calls, waiting = len(h.workerCalls("tests")), h.kinds() })
	h.run()
	h.wantOutcome(machine.OutcomeDone)

	if calls != 4 {
		t.Fatalf("tests called %d times while waiting for its workers, want 4", calls)
	}
	if len(waiting) == 0 || waiting[len(waiting)-1] != "RunInterrupted" {
		t.Fatalf("while waiting the record ends %v, want RunInterrupted", waiting)
	}
	in := h.eventsOf("RunInterrupted")
	if len(in) != 1 || in[0].Detail != wfNoWorkers {
		t.Fatalf("RunInterrupted = %+v, want one with %q", in, wfNoWorkers)
	}
	i := h.index("RunInterrupted", 0)
	if h.index("RunResumed", i) != i+1 {
		t.Fatalf("RunResumed does not follow the interruption: %v", h.kinds())
	}
	if got := attemptsOf(h.workerCalls("tests")); !slices.Equal(got, []int{1, 1, 1, 1, 1}) {
		t.Fatalf("tests attempts = %v, want [1 1 1 1 1]", got)
	}
}

// (h) The model unavailable on every attempt leaves the run interrupted until resume.
func TestWorkflowModelUnavailableOnEveryAttemptWaitsForResume(t *testing.T) {
	h := newWF(t, wfPattern())
	down := true
	h.work = func(c durable.WorkerCall) (machine.Result, error) {
		if c.Step.Name == "tests" && down {
			return machine.Result{}, temporal.NewNonRetryableApplicationError("the model is unavailable", durable.ModelUnavailable, nil)
		}
		return wfOK(c), nil
	}
	var calls int
	var waiting []string
	h.signalAt(wfDay, durable.SignalResume, func() { calls, waiting, down = len(h.workerCalls("tests")), h.kinds(), false })
	h.run()
	h.wantOutcome(machine.OutcomeDone)

	if calls != 3 {
		t.Fatalf("tests called %d times before the resume, want its 3 attempts", calls)
	}
	if len(waiting) == 0 || waiting[len(waiting)-1] != "RunInterrupted" || slices.Contains(waiting, "RunHandedOver") {
		t.Fatalf("before the resume the record ends %v, want RunInterrupted and no hand-over", waiting)
	}
	in := h.eventsOf("RunInterrupted")
	if len(in) != 1 || !strings.Contains(in[0].Detail, "tests") || in[0].Detail == wfStopped {
		t.Fatalf("RunInterrupted = %+v, want one naming step tests", in)
	}
	res := h.eventsOf("RunResumed")
	if len(res) != 1 || res[0].Detail != "" {
		t.Fatalf("RunResumed = %+v, want the developer's, without a detail", res)
	}
}

// (i) cancel records RunCancelled and ends the run, whatever it waits for.
func TestWorkflowCancelEndsTheRun(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(h *wfHarness)
	}{
		{"waiting for the developer", func(h *wfHarness) {
			failing := true
			h.check = wfFailCode(&failing)
		}},
		{"waiting for its workers", func(h *wfHarness) { h.work = wfWorkersGoneForCalls("tests", 4) }},
		{"interrupted", func(h *wfHarness) {
			h.work = func(c durable.WorkerCall) (machine.Result, error) {
				if c.Step.Name == "tests" {
					return machine.Result{}, temporal.NewNonRetryableApplicationError("the model is unavailable", durable.ModelUnavailable, nil)
				}
				return wfOK(c), nil
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWF(t, wfPattern())
			tc.setup(h)
			var calls int
			h.signalAt(wfDay, durable.SignalCancel, func() { calls = len(h.callsOf(durable.CallWorker)) })
			h.run()
			h.wantOutcome(machine.OutcomeCancelled)
			k := h.kinds()
			if k[len(k)-1] != "RunCancelled" || len(h.eventsOf("RunCancelled")) != 1 {
				t.Fatalf("events %v, want one RunCancelled, last", k)
			}
			if got := len(h.callsOf(durable.CallWorker)); got != calls {
				t.Fatalf("the worker was called %d times after the cancel", got-calls)
			}
		})
	}
}

// (j) A failing restore or commit is retried per src/retry (the engine contract's x-limits) with
// durable sleeps whose random part comes from a side effect, then interrupts the run as feature
// 001 does; a resume makes the call again.
func TestWorkflowFailingRestoreOrCommitIsRetriedThenInterrupts(t *testing.T) {
	doc, err := os.ReadFile("../api/preview/2026-11-12-preview/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := retry.Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	const random = 0.8 // what the mocked side effect gives
	for _, tc := range []struct {
		name, activity, reason string
	}{
		{"restore", durable.RestoreProject, "restoreStep for attempt 1 of step generate failed 4 times"},
		{"commit", durable.CommitStep, "commitStep for step generate failed 4 times"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWF(t, wfPattern())
			h.env.OnSideEffect().Return(random)
			down := true
			h.fail = func(name string, in any) error {
				if name != tc.activity || !down {
					return nil
				}
				if r, ok := in.(machine.RestoreRequest); ok && r.Step.Name == "generate" {
					return errWFUnreachable
				}
				if r, ok := in.(machine.CommitRequest); ok && r.Step.Name == "generate" {
					return errWFUnreachable
				}
				return nil
			}
			var waiting []string
			h.signalAt(wfDay, durable.SignalResume, func() { waiting, down = h.kinds(), false })
			h.run()
			h.wantOutcome(machine.OutcomeDone)

			var at []time.Time
			for _, c := range h.callsOf(tc.activity) {
				var step string
				switch r := c.In.(type) {
				case machine.RestoreRequest:
					step = r.Step.Name
				case machine.CommitRequest:
					step = r.Step.Name
				}
				if step == "generate" && c.Err != nil {
					at = append(at, c.At)
				}
			}
			if len(at) != rule.Retries+1 {
				t.Fatalf("%d failed calls before the interruption, want %d (the first and %d retries)", len(at), rule.Retries+1, rule.Retries)
			}
			for n := 0; n < rule.Retries; n++ {
				want := rule.Wait(n, func() float64 { return random })
				if got := at[n+1].Sub(at[n]); got != want {
					t.Fatalf("retry %d waited %s, want %s (src/retry with the side effect's %v)", n, got, want, random)
				}
			}
			if len(waiting) == 0 || waiting[len(waiting)-1] != "RunInterrupted" || slices.Contains(waiting, "RunHandedOver") {
				t.Fatalf("before the resume the record ends %v, want RunInterrupted, never a hand-over", waiting)
			}
			in := h.eventsOf("RunInterrupted")
			if len(in) != 1 || !strings.HasPrefix(in[0].Detail, tc.reason) {
				t.Fatalf("RunInterrupted = %+v, want one starting %q", in, tc.reason)
			}
		})
	}
}
