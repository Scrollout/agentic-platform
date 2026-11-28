package machine_test

// T057: a failed worker call and a check that could not run report what they used and how long
// they ran (FR-014, FR-003, research R3 "Budgets"; data model, the durable interface, rows
// CallWorker and RunCheck). New in T057, against the interface: durable.WorkerResult.Failure and
// durable.CheckResult.Failure. A result with Failure is a failed call (the worker's, or a check
// that could not run), not an activity error; it keeps Usage and Took. They do not compile until
// the code adds the two fields; that is the expected failure.
//
// The harness of workflow_durable_test.go cannot carry Failure (its fakes return machine.Result
// and machine.CheckResult), so fuHarness below registers the same fakes with results of the
// durable types, and reuses that file's pattern, hashing and result helpers.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

type fuHarness struct {
	t   *testing.T
	env *testsuite.TestWorkflowEnvironment
	in  durable.RunInput
	// worker and check are the fakes (default: a good result, 1 s for the worker, 0 for a check).
	worker func(c durable.WorkerCall) (durable.WorkerResult, error)
	// only, when set, limits worker to the step of that name: every other step gets a good result
	// that used nothing and took no time. The failing step of these cases is an agent step, as
	// a procedure step records no usage.
	only  string
	check func(c durable.CheckCall) (durable.CheckResult, error)

	mu      sync.Mutex
	records []durable.EventRecord
	workers []durable.WorkerCall
}

func newFU(t *testing.T, p machine.Pattern) *fuHarness {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	h := &fuHarness{t: t, env: suite.NewTestWorkflowEnvironment()}
	h.in = durable.RunInput{Run: "RUN-1", Slice: "S-1", Project: "/projects/p1", Pattern: p, Model: wfModel,
		Inputs: map[string]string{"slice/contract": wfHash("contract"), "slice/rules": wfHash("rules")},
		Refs:   []string{"G-1", "R-1"}}
	h.env.SetStartTime(wfT0)
	h.env.SetTestTimeout(60 * time.Second)
	h.env.RegisterWorkflow(durable.Run)
	reg := func(fn any, name string) { h.env.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name}) }
	reg(func(_ context.Context, c durable.WorkerCall) (durable.WorkerResult, error) {
		h.mu.Lock()
		h.workers = append(h.workers, c)
		h.mu.Unlock()
		if h.worker != nil && (h.only == "" || c.Step.Name == h.only) {
			return h.worker(c)
		}
		took := time.Second
		if h.only != "" {
			took = 0
		}
		return wfWorkerResult(t, c, wfOK(c), took), nil
	}, durable.CallWorker)
	reg(func(_ context.Context, c durable.CheckCall) (durable.CheckResult, error) {
		if h.check != nil {
			return h.check(c)
		}
		r, _ := wfDefaultCheck(c)
		return wfCheckResult(t, r, 0), nil
	}, durable.RunCheck)
	reg(func(context.Context, machine.RestoreRequest) (durable.Ran, error) { return durable.Ran{}, nil }, durable.RestoreProject)
	reg(func(context.Context, machine.CommitRequest) (durable.Ran, error) { return durable.Ran{}, nil }, durable.CommitStep)
	reg(func(context.Context, durable.ReuseLookup) (durable.Reused, error) { return durable.Reused{}, nil }, durable.LookupReuse)
	reg(func(context.Context, durable.ReusePut) error { return nil }, durable.PutResult)
	reg(func(_ context.Context, r durable.EventRecord) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.records = append(h.records, r)
		return nil
	}, durable.RecordEvent)
	return h
}

func (h *fuHarness) run() {
	h.t.Helper()
	h.env.RegisterDelayedCallback(func() { h.env.SignalWorkflow(durable.SignalCancel, nil) }, 60*24*time.Hour)
	h.env.ExecuteWorkflow(durable.Run, h.in)
	if !h.env.IsWorkflowCompleted() {
		h.t.Fatal("the workflow did not complete")
	}
}

func (h *fuHarness) eventsOf(kind string) []machine.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []machine.Event
	for _, r := range h.records {
		if r.Event.Kind == kind {
			out = append(out, r.Event)
		}
	}
	return out
}

func (h *fuHarness) kinds() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var k []string
	for _, r := range h.records {
		k = append(k, r.Event.Kind)
	}
	return k
}

func (h *fuHarness) workerCalls(step string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, c := range h.workers {
		if c.Step.Name == step {
			n++
		}
	}
	return n
}

// handedOver wants exactly one RunHandedOver, none interrupted, and returns its detail.
func (h *fuHarness) handedOver() string {
	h.t.Helper()
	ho := h.eventsOf("RunHandedOver")
	if len(ho) != 1 {
		h.t.Fatalf("hand-overs = %d, want 1; events %v", len(ho), h.kinds())
	}
	if n := len(h.eventsOf("RunInterrupted")); n != 0 {
		h.t.Fatalf("%d interruptions, want none (a budget hands the run over); events %v", n, h.kinds())
	}
	return ho[0].Detail
}

// fuFail is a failed worker call's result: Failure, what it used, how long it ran. Model is kept.
func fuFail(c durable.WorkerCall, failure string, u machine.Usage, took time.Duration) durable.WorkerResult {
	r := durable.WorkerResult{Failure: failure, Usage: u, Took: took}
	if slices.Contains([]string{"tests", "code", "review"}, c.Step.Name) {
		r.Model = wfModel
	}
	return r
}

// fuPattern has the budgets of wfPattern but for the ones given (0: wfPattern's).
func fuPattern(attempts int, run, step machine.Budget) machine.Pattern {
	p := wfPattern()
	if attempts > 0 {
		p.Attempts = attempts
	}
	if run != (machine.Budget{}) {
		p.RunBudget = run
	}
	if step != (machine.Budget{}) {
		p.StepBudget = step
	}
	return p
}

func fuWantRejected(t *testing.T, h *fuHarness, step string, n int, failure string, u machine.Usage) {
	t.Helper()
	var got []machine.Event
	for _, e := range h.eventsOf("StepRejected") {
		if e.Step == step {
			got = append(got, e)
		}
	}
	if len(got) != n {
		t.Fatalf("%d StepRejected of %s, want %d; events %v", len(got), step, n, h.kinds())
	}
	for i, e := range got {
		if want := "the worker failed: " + failure; e.Detail != want {
			t.Fatalf("StepRejected %d detail %q, want %q", i+1, e.Detail, want)
		}
		if e.Usage == nil || *e.Usage != u {
			t.Fatalf("StepRejected %d usage %+v, want %+v: a failed attempt's use is recorded", i+1, e.Usage, u)
		}
	}
}

// (a) Failed attempts carry their usage on StepRejected and count toward the run's budget. Today
// CallWorker returns an empty result with the error, so every StepRejected has 0 tokens and 0
// cost and the run never passes its budget: after 3 failed attempts it is interrupted ("failed on
// all 3 attempts"), not handed over with the budget reason.
func TestFailedCallsUsageReachesTheRunBudget(t *testing.T) {
	t.Run("tokens: the third failed attempt passes the run's budget", func(t *testing.T) {
		u := machine.Usage{Tokens: 400, CostUSD: 0.01}
		h := newFU(t, fuPattern(0, machine.Budget{Tokens: 1000, CostUSD: 0.50, Duration: 30 * time.Minute}, machine.Budget{}))
		h.only = "tests"
		h.worker = func(c durable.WorkerCall) (durable.WorkerResult, error) {
			return fuFail(c, "model refused the request", u, time.Second), nil
		}
		h.run()
		fuWantRejected(t, h, "tests", 3, "model refused the request", u)
		want := "the run used 1200 tokens, past the run's token budget of 1000"
		if got := h.handedOver(); got != want {
			t.Fatalf("hand-over %q, want %q", got, want)
		}
		if n := h.workerCalls("tests"); n != 3 {
			t.Fatalf("%d worker calls, want 3", n)
		}
	})
	t.Run("cost: the second failed attempt passes the run's budget", func(t *testing.T) {
		u := machine.Usage{Tokens: 10, CostUSD: 0.30}
		h := newFU(t, fuPattern(0, machine.Budget{Tokens: 3000000, CostUSD: 0.50, Duration: 30 * time.Minute}, machine.Budget{CostUSD: 0.40, Tokens: 1000000, Duration: 10 * time.Minute}))
		h.only = "tests"
		h.worker = func(c durable.WorkerCall) (durable.WorkerResult, error) {
			return fuFail(c, "boom", u, time.Second), nil
		}
		h.run()
		fuWantRejected(t, h, "tests", 2, "boom", u)
		want := "the run cost $0.6000, past the run's cost budget of $0.5000"
		if got := h.handedOver(); got != want {
			t.Fatalf("hand-over %q, want %q", got, want)
		}
	})
	t.Run("a failed attempt over the step's budget on its own, on the same attempt it fails", func(t *testing.T) {
		u := machine.Usage{Tokens: 600}
		h := newFU(t, fuPattern(0, machine.Budget{}, machine.Budget{Tokens: 500, CostUSD: 0.20, Duration: 10 * time.Minute}))
		h.only = "tests"
		h.worker = func(c durable.WorkerCall) (durable.WorkerResult, error) {
			return fuFail(c, "boom", u, time.Second), nil
		}
		h.run()
		// The failure is recorded as today ("the worker failed: ..."), with its usage; then the
		// run is handed over for the budget, with no further attempt.
		fuWantRejected(t, h, "tests", 1, "boom", u)
		want := "step tests used 600 tokens, past the step's token budget of 500"
		if got := h.handedOver(); got != want {
			t.Fatalf("hand-over %q, want %q", got, want)
		}
		if n := h.workerCalls("tests"); n != 1 {
			t.Fatalf("%d worker calls, want 1: an over-budget failure is not tried again", n)
		}
	})
	t.Run("usage adds up across steps: an earlier accepted step's use counts with a failed one's", func(t *testing.T) {
		// tests succeeds with 600 tokens; code fails with 300 on each attempt:
		// 600 + 300 + 300 = 1200 > 1000 on the second failed attempt. The two procedure steps
		// before them succeed and record no usage.
		h := newFU(t, fuPattern(0, machine.Budget{Tokens: 1000, CostUSD: 0.50, Duration: 30 * time.Minute}, machine.Budget{}))
		h.worker = func(c durable.WorkerCall) (durable.WorkerResult, error) {
			switch c.Step.Name {
			case "contract-check", "generate":
				return wfWorkerResult(t, c, wfOK(c), time.Second), nil
			case "tests":
				r := wfWorkerResult(t, c, wfOK(c), time.Second)
				r.Usage = machine.Usage{Tokens: 600}
				return r, nil
			}
			return fuFail(c, "boom", machine.Usage{Tokens: 300}, time.Second), nil
		}
		h.run()
		fuWantRejected(t, h, "code", 2, "boom", machine.Usage{Tokens: 300})
		want := "the run used 1200 tokens, past the run's token budget of 1000"
		if got := h.handedOver(); got != want {
			t.Fatalf("hand-over %q, want %q", got, want)
		}
	})
}

// (a, edges) A failed result that reports nothing, or something negative, counts as nothing: the
// attempts end as they do for a failed call today (the run is interrupted after the last
// attempt), and a negative amount never takes from what the run has used.
func TestFailedCallsWithNoOrNegativeUsageCountAsZero(t *testing.T) {
	for _, tc := range []struct {
		name string
		u    machine.Usage
		took time.Duration
		want machine.Usage
	}{
		{"zero usage, zero time", machine.Usage{}, 0, machine.Usage{}},
		{"negative usage and time", machine.Usage{Tokens: -5000000, CostUSD: -9}, -time.Hour, machine.Usage{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newFU(t, wfPattern())
			h.only = "tests"
			h.worker = func(c durable.WorkerCall) (durable.WorkerResult, error) {
				return fuFail(c, "boom", tc.u, tc.took), nil
			}
			h.run()
			fuWantRejected(t, h, "tests", 3, "boom", tc.want)
			if n := len(h.eventsOf("RunInterrupted")); n != 1 {
				t.Fatalf("%d interruptions, want 1 (failed on all attempts); events %v", n, h.kinds())
			}
			if n := len(h.eventsOf("RunHandedOver")); n != 0 {
				t.Fatalf("handed over: %v", h.kinds())
			}
		})
	}
	t.Run("a negative failed attempt does not take from an earlier one's use", func(t *testing.T) {
		// attempt 1 uses 900 tokens (run budget 1000), attempt 2 reports -900, attempt 3 reports
		// 200: 1100 > 1000. Taking the negative would leave 200.
		h := newFU(t, fuPattern(0, machine.Budget{Tokens: 1000, CostUSD: 0.50, Duration: 30 * time.Minute}, machine.Budget{}))
		h.only = "tests"
		h.worker = func(c durable.WorkerCall) (durable.WorkerResult, error) {
			tokens := map[int]int64{1: 900, 2: -900, 3: 200}[c.Attempt]
			return fuFail(c, "boom", machine.Usage{Tokens: tokens}, time.Second), nil
		}
		h.run()
		want := "the run used 1100 tokens, past the run's token budget of 1000"
		if got := h.handedOver(); got != want {
			t.Fatalf("hand-over %q, want %q", got, want)
		}
	})
}

// (b) A failed call's Took counts toward the time budget. Today the workflow adds 0 time for a
// failed call, so the run is interrupted after 3 attempts instead of handed over.
func TestFailedCallsTookCountsTowardTheTimeBudget(t *testing.T) {
	t.Run("summed over attempts, the last allowed attempt passes the run's budget", func(t *testing.T) {
		// 4 attempts of 9 minutes: 27m after the third, 36m after the fourth, which is the last.
		h := newFU(t, fuPattern(4, machine.Budget{}, machine.Budget{}))
		h.only = "tests"
		h.worker = func(c durable.WorkerCall) (durable.WorkerResult, error) {
			return fuFail(c, "boom", machine.Usage{}, 9*time.Minute), nil
		}
		h.run()
		fuWantRejected(t, h, "tests", 4, "boom", machine.Usage{})
		want := "the run took 36m0s, past the run's time budget of 30m0s"
		if got := h.handedOver(); got != want {
			t.Fatalf("hand-over %q, want %q", got, want)
		}
		if n := h.workerCalls("tests"); n != 4 {
			t.Fatalf("%d worker calls, want 4", n)
		}
	})
	t.Run("one failed call past the step's time budget", func(t *testing.T) {
		h := newFU(t, wfPattern())
		h.only = "tests"
		h.worker = func(c durable.WorkerCall) (durable.WorkerResult, error) {
			return fuFail(c, "boom", machine.Usage{}, 11*time.Minute), nil
		}
		h.run()
		want := "step tests took 11m0s, past the step's time budget of 10m0s"
		if got := h.handedOver(); got != want {
			t.Fatalf("hand-over %q, want %q", got, want)
		}
		if n := h.workerCalls("tests"); n != 1 {
			t.Fatalf("%d worker calls, want 1", n)
		}
	})
}

// (a, retry) A worker that is not ready is still an activity error that the declared retry rule
// repeats, recording nothing; a failed result is not repeated that way, it is an attempt. Today
// the failed results do not compile; with the code, the call count tells the two apart.
func TestWorkersNotReadyIsStillRetriedAndAFailedResultIsAnAttempt(t *testing.T) {
	h := newFU(t, wfPattern())
	h.only = "tests"
	first := true
	h.worker = func(c durable.WorkerCall) (durable.WorkerResult, error) {
		if c.Step.Name == "tests" && first {
			first = false
			return durable.WorkerResult{}, temporal.NewNonRetryableApplicationError(wfNoWorkers, durable.WorkersNotReady, nil)
		}
		return fuFail(c, "boom", machine.Usage{Tokens: 7}, time.Second), nil
	}
	h.run()
	// 1 call that was not ready (retried, nothing recorded) + 3 attempts that failed.
	if n := h.workerCalls("tests"); n != 4 {
		t.Fatalf("%d worker calls, want 4 (1 not ready, then 3 attempts); events %v", n, h.kinds())
	}
	fuWantRejected(t, h, "tests", 3, "boom", machine.Usage{Tokens: 7})
	for _, e := range h.eventsOf("StepRejected") {
		if e.Attempt < 1 || e.Attempt > 3 {
			t.Fatalf("StepRejected of attempt %d: the not-ready call was recorded as an attempt", e.Attempt)
		}
	}
}

// (c) A check that could not run is a result with Failure and Took: CheckFailed with the reason
// "the check could not run: <Failure>", and its Took counts toward the run's time. Today the
// result's Reason is empty, so CheckFailed carries "<name> failed" and the run's time is short of
// the budget (no hand-over) when the time comes from checks only.
func TestCheckThatCouldNotRunCountsItsTook(t *testing.T) {
	cannot := func(c durable.CheckCall, failure string, took time.Duration) durable.CheckResult {
		return durable.CheckResult{Passed: false, Failure: failure, Took: took}
	}
	failedCheck := func(h *fuHarness, name string) machine.Event {
		for _, e := range h.eventsOf("CheckFailed") {
			if e.Check != nil && e.Check.Name == name && e.Step == "contract-check" && e.Attempt == 1 {
				return e
			}
		}
		h.t.Fatalf("no CheckFailed for %s; events %v", name, h.kinds())
		return machine.Event{}
	}
	t.Run("reason and time", func(t *testing.T) {
		h := newFU(t, wfPattern())
		h.worker = func(c durable.WorkerCall) (durable.WorkerResult, error) {
			return wfWorkerResult(t, c, wfOK(c), 0), nil
		}
		h.check = func(c durable.CheckCall) (durable.CheckResult, error) {
			if c.Step.Name == "contract-check" && c.Name == "ContractMatchesApproval" {
				return cannot(c, "the check service is unreachable", 31*time.Minute), nil
			}
			r, _ := wfDefaultCheck(c)
			return wfCheckResult(t, r, 0), nil
		}
		h.run()
		e := failedCheck(h, "ContractMatchesApproval")
		if want := "the check could not run: the check service is unreachable"; e.Check.Passed || e.Check.Reason != want {
			t.Fatalf("CheckFailed %+v, want not passed with reason %q", e.Check, want)
		}
		want := "the run took 31m0s, past the run's time budget of 30m0s"
		if got := h.handedOver(); got != want {
			t.Fatalf("hand-over %q, want %q", got, want)
		}
	})
	t.Run("the time adds to the checks and the attempt before it", func(t *testing.T) {
		// worker 1s + could-not-run check 16m + passing check 14m = 30m1s: past 30m only when
		// the could-not-run check's time counts.
		h := newFU(t, wfPattern())
		h.check = func(c durable.CheckCall) (durable.CheckResult, error) {
			if c.Step.Name == "contract-check" && c.Name == "ContractMatchesApproval" {
				return cannot(c, "boom", 16*time.Minute), nil
			}
			r, _ := wfDefaultCheck(c)
			return wfCheckResult(t, r, 14*time.Minute), nil
		}
		h.run()
		want := "the run took 30m1s, past the run's time budget of 30m0s"
		if got := h.handedOver(); got != want {
			t.Fatalf("hand-over %q, want %q", got, want)
		}
	})
	t.Run("a negative Took takes nothing from the run's time", func(t *testing.T) {
		h := newFU(t, wfPattern())
		h.worker = func(c durable.WorkerCall) (durable.WorkerResult, error) {
			return wfWorkerResult(t, c, wfOK(c), 0), nil
		}
		h.check = func(c durable.CheckCall) (durable.CheckResult, error) {
			if c.Step.Name == "contract-check" && c.Name == "ContractMatchesApproval" {
				return cannot(c, "boom", -time.Hour), nil
			}
			r, _ := wfDefaultCheck(c)
			return wfCheckResult(t, r, 31*time.Minute), nil
		}
		h.run()
		want := "the run took 31m0s, past the run's time budget of 30m0s"
		if got := h.handedOver(); got != want {
			t.Fatalf("hand-over %q, want %q", got, want)
		}
	})
	t.Run("zero Took: the failed check is just a failed check", func(t *testing.T) {
		h := newFU(t, wfPattern())
		h.check = func(c durable.CheckCall) (durable.CheckResult, error) {
			if c.Step.Name == "contract-check" && c.Name == "ContractMatchesApproval" && c.Attempt == 1 {
				return cannot(c, "boom", 0), nil
			}
			r, _ := wfDefaultCheck(c)
			return wfCheckResult(t, r, 0), nil
		}
		h.run()
		e := failedCheck(h, "ContractMatchesApproval")
		if want := "the check could not run: boom"; e.Check.Reason != want {
			t.Fatalf("reason %q, want %q", e.Check.Reason, want)
		}
		var out durable.RunResult
		if err := h.env.GetWorkflowResult(&out); err != nil || out.Outcome != machine.OutcomeDone {
			t.Fatalf("outcome %q (%v), want done: attempt 2 passes", out.Outcome, err)
		}
	})
}

// (d) The activities. Worker and Checker are the ports; Activities needs nothing else for a call
// with no findings or outputs.
type fuWorker struct {
	res machine.Result
	err error
	nap time.Duration
}

func (w fuWorker) Run(ctx context.Context, _ machine.Request) (machine.Result, error) {
	time.Sleep(w.nap)
	return w.res, w.err
}

type fuChecker struct {
	err error
	nap time.Duration
}

func (c fuChecker) Check(context.Context, machine.CheckRequest) (machine.CheckResult, error) {
	time.Sleep(c.nap)
	return machine.CheckResult{}, c.err
}

func fuStep() machine.Step { return wfPattern().Steps[2] } // tests: an agent step

// A worker's failure is a result: Failure, Model, Usage and Took; a nil error. Today it is an
// error ("WorkerFailed") with an empty result, so the nil-error assertion fails first.
func TestCallWorkerReturnsAFailedCallAsAResult(t *testing.T) {
	boom := errors.New("the model returned a 500: overloaded")
	for name, werr := range map[string]error{
		"a plain error":   boom,
		"a wrapped error": fmt.Errorf("step tests: %w", boom),
	} {
		t.Run(name, func(t *testing.T) {
			a := &durable.Activities{Worker: fuWorker{
				res: machine.Result{Model: wfModel, Usage: machine.Usage{Tokens: 1234, CostUSD: 0.0456}, Outputs: map[string]string{"tests": wfHash("x")}},
				err: werr, nap: 5 * time.Millisecond}}
			got, err := a.CallWorker(context.Background(), durable.WorkerCall{Run: "RUN-1", Slice: "S-1", Step: fuStep(), Attempt: 1})
			if err != nil {
				t.Fatalf("error %v, want nil: a failed call is a result", err)
			}
			if got.Failure != werr.Error() {
				t.Fatalf("Failure %q, want %q", got.Failure, werr.Error())
			}
			if got.Usage != (machine.Usage{Tokens: 1234, CostUSD: 0.0456}) || got.Model != wfModel {
				t.Fatalf("usage %+v, model %q: a failed call keeps what it used", got.Usage, got.Model)
			}
			if got.Took < 5*time.Millisecond {
				t.Fatalf("Took %s, want at least the 5ms the call ran", got.Took)
			}
		})
	}
	t.Run("a worker that reports no usage", func(t *testing.T) {
		a := &durable.Activities{Worker: fuWorker{err: boom}}
		got, err := a.CallWorker(context.Background(), durable.WorkerCall{Run: "RUN-1", Slice: "S-1", Step: fuStep(), Attempt: 1})
		if err != nil || got.Failure != boom.Error() || got.Usage != (machine.Usage{}) {
			t.Fatalf("result %+v, error %v, want Failure %q and no usage", got, err, boom.Error())
		}
	})
	t.Run("a successful call has no Failure", func(t *testing.T) {
		a := &durable.Activities{Worker: fuWorker{res: machine.Result{Model: wfModel, Usage: machine.Usage{Tokens: 5}}}}
		got, err := a.CallWorker(context.Background(), durable.WorkerCall{Run: "RUN-1", Slice: "S-1", Step: fuStep(), Attempt: 1})
		if err != nil || got.Failure != "" || got.Usage.Tokens != 5 {
			t.Fatalf("result %+v, error %v", got, err)
		}
	})
}

// WorkersNotReady and ModelUnavailable stay errors of their types, however wrapped, and a context
// end stays an error: none of them is an attempt that failed.
func TestCallWorkerKeepsItsRetryableErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		typ  string
	}{
		{"workers not ready", fmt.Errorf("calling the worker: %w", machine.ErrWorkersNotReady), durable.WorkersNotReady},
		{"model unavailable", fmt.Errorf("calling the model: %w", machine.ErrModelUnavailable), durable.ModelUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// the worker reports usage with the error: it still is an error, not a result
			a := &durable.Activities{Worker: fuWorker{res: machine.Result{Usage: machine.Usage{Tokens: 9}}, err: tc.err}}
			got, err := a.CallWorker(context.Background(), durable.WorkerCall{Run: "RUN-1", Slice: "S-1", Step: fuStep(), Attempt: 1})
			var ae *temporal.ApplicationError
			if !errors.As(err, &ae) || ae.Type() != tc.typ {
				t.Fatalf("error %v, want an application error of type %s (result %+v)", err, tc.typ, got)
			}
			if got.Failure != "" {
				t.Fatalf("Failure %q on an error", got.Failure)
			}
		})
	}
	t.Run("a call ended by its context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		a := &durable.Activities{Worker: fuWorker{err: ctx.Err()}}
		got, err := a.CallWorker(ctx, durable.WorkerCall{Run: "RUN-1", Slice: "S-1", Step: fuStep(), Attempt: 1})
		if err == nil || got.Failure != "" {
			t.Fatalf("result %+v, error %v: a context end is an error, not a failed attempt", got, err)
		}
	})
}

// A check that cannot run is a result: Passed false, Failure and Took; a nil error. Today it is
// an error ("CheckFailed") and Took is lost.
func TestRunCheckReturnsACheckThatCouldNotRunAsAResult(t *testing.T) {
	boom := errors.New("the check service is unreachable")
	a := &durable.Activities{Checker: fuChecker{err: boom, nap: 5 * time.Millisecond}}
	got, err := a.RunCheck(context.Background(), durable.CheckCall{Run: "RUN-1", Slice: "S-1", Step: fuStep(), Attempt: 1, Name: "TestsBuild"})
	if err != nil {
		t.Fatalf("error %v, want nil: a check that could not run is a result", err)
	}
	if got.Passed || got.Failure != boom.Error() || got.Took < 5*time.Millisecond {
		t.Fatalf("result %+v, want not passed, Failure %q and Took of at least 5ms", got, boom.Error())
	}
	t.Run("a check that ran has no Failure", func(t *testing.T) {
		a := &durable.Activities{Checker: fuChecker{}}
		got, err := a.RunCheck(context.Background(), durable.CheckCall{Run: "RUN-1", Slice: "S-1", Step: fuStep(), Attempt: 1, Name: "TestsBuild"})
		if err != nil || got.Failure != "" {
			t.Fatalf("result %+v, error %v", got, err)
		}
	})
}
