package machine_test

// Failing tests for the approved additive contract change: usage and slice in the run record, and a
// machine that reads every run from the record when it is built.
//
// Contract change assumed (run-event.schema.json, snake_case like the rest of the contract):
//
//	usage: {tokens: integer >= 0, cost_usd: number >= 0}, optional, on AttemptMade events;
//	slice: the run's slice ID, optional, on RunStarted (required there).
//
// Go interface assumed: Event gains Usage *machine.Usage `json:"usage,omitempty"` and Slice string
// `json:"slice,omitempty"`; machine.Usage gets json tags tokens and cost_usd. A second optional
// interface, machine.Lister{ Runs() ([]string, error) }, is implemented by an Events sink next to
// Reader; New (or the first call) loads every listed run, so a run that exists only in the record
// and has not ended counts as active. (A separate interface keeps the existing readable sink in
// reuse_test.go valid.)

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/agentic-platform/engine/machine"
)

// listSink is a readable, listable event record.
type listSink struct{ recSink }

func (s listSink) Runs() ([]string, error) {
	var out []string
	for _, e := range s.events {
		if !slices.Contains(out, e.Run) {
			out = append(out, e.Run)
		}
	}
	return out, nil
}

// interruptedRecord runs RUN-1 on a first machine until its code step fails (model unavailable),
// every earlier step using the given usage; the machine is then dropped.
func interruptedRecord(t *testing.T, p machine.Pattern, use machine.Usage) (listSink, *machine.Results, *clock) {
	t.Helper()
	results, ev, clk := machine.NewResults(), listSink{recSink{&sink{}}}, &clock{now: t0}
	w := &worker{fn: map[string]func(machine.Request) (machine.Result, error){
		"code": func(machine.Request) (machine.Result, error) {
			return machine.Result{}, errors.New("model unavailable")
		},
	}}
	for _, s := range p.Steps {
		if s.Name != "code" {
			w.fn[s.Name] = func(r machine.Request) (machine.Result, error) {
				res := ok(r)
				res.Usage = use
				return res, nil
			}
		}
	}
	m := machine.New(machine.Config{Pattern: p, Worker: w, Checker: &checker{fn: noBlocking}, Clock: clk,
		Events: ev, Results: results, Model: "claude-haiku-5-5"})
	r, err := m.Start(context.Background(), (&fixture{t: t}).params("RUN-1"))
	if err != nil || r.Status != machine.StatusInterrupted {
		t.Fatalf("setup: %v, status %s", err, r.Status)
	}
	return ev, results, clk
}

func restarted(p machine.Pattern, ev listSink, results *machine.Results, clk *clock, w *worker) *machine.Machine {
	return machine.New(machine.Config{Pattern: p, Worker: w, Checker: &checker{fn: noBlocking}, Clock: clk,
		Events: ev, Results: results, Model: "claude-haiku-5-5"})
}

func TestRunBudgetCountsUseFromBeforeTheRestart(t *testing.T) {
	cases := map[string]struct {
		budget machine.Budget
		use    machine.Usage
	}{
		"tokens": {machine.Budget{Tokens: 100, CostUSD: 100, Duration: 30 * 60e9}, machine.Usage{Tokens: 30, CostUSD: 0.001}},
		"cost":   {machine.Budget{Tokens: 1e9, CostUSD: 0.10, Duration: 30 * 60e9}, machine.Usage{Tokens: 1, CostUSD: 0.03}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p := pattern()
			p.RunBudget = c.budget
			// three accepted steps use 90% of the budget; code (and review) would add another 60%
			ev, results, clk := interruptedRecord(t, p, c.use)
			w := &worker{fn: map[string]func(machine.Request) (machine.Result, error){}}
			w.fn["code"] = func(r machine.Request) (machine.Result, error) {
				res := ok(r)
				res.Usage = c.use
				return res, nil
			}
			r, err := restarted(p, ev, results, clk, w).Resume(context.Background(), "RUN-1")
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != machine.StatusWaiting || ev.events[len(ev.events)-1].Kind != "RunHandedOver" {
				t.Fatalf("run = %s/%s, last event %s; want handed over for crossing the run budget",
					r.Status, r.Outcome, ev.events[len(ev.events)-1].Kind)
			}
		})
	}
}

func TestSliceIsRestoredFromTheRecord(t *testing.T) {
	ev, results, clk := interruptedRecord(t, pattern(), machine.Usage{Tokens: 1})
	m := restarted(pattern(), ev, results, clk, &worker{})
	r, found := m.Get("RUN-1")
	if !found || r.Slice != "S-1" || r.Status != machine.StatusInterrupted {
		t.Fatalf("Get = %+v found=%v, want slice S-1 and interrupted", r, found)
	}
}

func TestUnendedRunInTheRecordIsActive(t *testing.T) {
	ev, results, clk := interruptedRecord(t, pattern(), machine.Usage{Tokens: 1})
	w := &worker{}
	m := restarted(pattern(), ev, results, clk, w)
	f := &fixture{t: t}
	_, err := m.Start(context.Background(), f.params("RUN-2")) // no Get or Resume first
	if !errors.Is(err, machine.ErrRunActive) {
		t.Fatalf("Start = %v, want ErrRunActive", err)
	}
	if len(w.calls) != 0 {
		t.Fatal("the refused run ran")
	}
	// an ended run in the record does not block
	if _, err := m.Cancel("RUN-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted(pattern(), ev, results, clk, &worker{}).Start(context.Background(), f.params("RUN-3")); err != nil {
		t.Fatalf("Start after the other run ended: %v", err)
	}
}

func TestUsageAndSliceInEventsValidateAgainstTheSchema(t *testing.T) {
	f := reuseFixture(t, machine.NewResults())
	f.w.fn["tests"] = func(r machine.Request) (machine.Result, error) {
		res := ok(r)
		res.Usage = machine.Usage{Tokens: 1200, CostUSD: 0.004}
		return res, nil
	}
	f.start("RUN-1")
	validate(t, f.ev.events)
	if f.ev.events[0].Kind != "RunStarted" || f.ev.events[0].Slice != "S-1" {
		t.Fatalf("RunStarted slice = %q", f.ev.events[0].Slice)
	}
	found := false
	for _, e := range f.ev.events {
		if e.Kind == "AttemptMade" && e.Step == "tests" {
			found = e.Usage != nil && e.Usage.Tokens == 1200 && e.Usage.CostUSD == 0.004
		}
	}
	if !found {
		t.Fatal("the AttemptMade event of tests does not carry usage")
	}
}

// --- review gaps -------------------------------------------------------------------------------
//
// Further additions assumed:
//	Event gains ChangedLines []machine.Line `json:"changed_lines,omitempty"` (contract: optional
//	  array of {file, line}, the shape of Result.ChangedLines), set on StepAccepted of a step whose
//	  result reported changed lines; a machine restarted over a record with an empty Results store
//	  still hands them to the next review round.
//	An agent step with an empty Config.Model is refused when the run starts: Start returns an error
//	  (New has no error return), no agent worker is called and no agent step is accepted.
//	Negative Usage from a worker counts as 0 (live, in the event, and when read back).

func TestPlatformStoppingDuringLastAttemptInterruptsTheRun(t *testing.T) {
	f := newFixture(t, pattern())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.c.fn = func(r machine.CheckRequest) (machine.CheckResult, error) {
		if r.Step.Name == "tests" && r.Attempt < 3 {
			return fail(r), nil
		}
		return pass(r), nil
	}
	f.w.fn["tests"] = func(r machine.Request) (machine.Result, error) {
		if r.Attempt == 3 {
			cancel() // the platform stops during the last attempt
			return machine.Result{}, ctx.Err()
		}
		return ok(r), nil
	}
	r, _ := f.m.Start(ctx, f.params("RUN-1"))
	if r.Status != machine.StatusInterrupted {
		t.Fatalf("status = %s/%s, want interrupted (not handed over)", r.Status, r.Outcome)
	}
	if f.ev.count("RunHandedOver") != 0 {
		t.Fatalf("run handed over: %v", f.ev.kinds())
	}
	if got, _ := f.m.Get("RUN-1"); got.Status != machine.StatusInterrupted {
		t.Fatalf("Get = %s", got.Status)
	}
}

func TestRestartKeepsTheFixsChangedLinesForTheNextReviewRound(t *testing.T) {
	lines := []machine.Line{{File: "internal/thing.go", Line: 12}, {File: "internal/thing.go", Line: 13}}
	finding := blocking("R-1", "internal/thing.go", 12)
	ev, clk := listSink{recSink{&sink{}}}, &clock{now: t0}
	down := true
	codeCalls := 0
	mk := func(results *machine.Results, round2 func(machine.Request) (machine.Result, error)) (*machine.Machine, *worker) {
		w := &worker{fn: map[string]func(machine.Request) (machine.Result, error){
			"code": func(r machine.Request) (machine.Result, error) {
				codeCalls++
				res := ok(r)
				if codeCalls == 2 { // the fix
					res.ChangedLines = lines
				}
				return res, nil
			},
			"review": func(r machine.Request) (machine.Result, error) {
				if r.Round <= 1 {
					res := ok(r)
					res.Findings = []machine.Finding{finding}
					return res, nil
				}
				return round2(r)
			},
		}}
		return machine.New(machine.Config{Pattern: pattern(), Worker: w, Checker: &checker{fn: noBlocking}, Clock: clk,
			Events: ev, Results: results, Model: "claude-haiku-5-5"}), w
	}
	m1, _ := mk(machine.NewResults(), func(r machine.Request) (machine.Result, error) {
		if down {
			return machine.Result{}, errors.New("model unavailable")
		}
		return ok(r), nil
	})
	r, err := m1.Start(context.Background(), (&fixture{t: t}).params("RUN-1"))
	if err != nil || r.Status != machine.StatusInterrupted {
		t.Fatalf("setup: %v, %s", err, r.Status)
	}
	withLines := 0
	for _, e := range ev.events {
		if e.Kind == "StepAccepted" && len(e.ChangedLines) > 0 {
			withLines++
			if !slices.Equal(e.ChangedLines, lines) || e.Step != "code" {
				t.Fatalf("changed_lines on %s = %v", e.Step, e.ChangedLines)
			}
		}
	}
	if withLines != 1 {
		t.Fatalf("the record carries changed_lines on %d accepted events, want 1 (the fix)", withLines)
	}
	validate(t, ev.events)

	// restart with an EMPTY result store: only the record can carry the lines
	var seen machine.Request
	m2, _ := mk(machine.NewResults(), func(r machine.Request) (machine.Result, error) {
		seen = r
		return ok(r), nil
	})
	r, err = m2.Resume(context.Background(), "RUN-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEnded(t, r, ev.sink, "done")
	if seen.Round < 2 || !slices.Equal(seen.ChangedLines, lines) || len(seen.Open) != 1 || seen.Open[0] != finding {
		t.Fatalf("round %d got open %v, changed lines %v; want the open finding and the fix's lines", seen.Round, seen.Open, seen.ChangedLines)
	}
}

func negUse(t *testing.T) machine.Pattern {
	p := pattern()
	p.RunBudget = machine.Budget{Tokens: 100, CostUSD: 100, Duration: 30 * 60e9}
	return p
}

func TestNegativeUsageCountsAsZeroLive(t *testing.T) {
	p := negUse(t)
	f := newFixture(t, p)
	f.m = machine.New(machine.Config{Pattern: p, Worker: f.w, Checker: f.c, Clock: f.clk, Events: f.ev,
		Results: machine.NewResults(), Model: "claude-haiku-5-5"})
	f.c.fn = noBlocking
	use := map[string]int64{"contract-check": -1000, "generate": 60, "tests": 60}
	for step, n := range use {
		f.w.fn[step] = func(r machine.Request) (machine.Result, error) {
			res := ok(r)
			res.Usage = machine.Usage{Tokens: n, CostUSD: float64(n) / 1000}
			return res, nil
		}
	}
	r := f.start("RUN-1")
	if r.Status != machine.StatusWaiting {
		t.Fatalf("status = %s; a negative count must not offset the 120 tokens used (budget 100)", r.Status)
	}
	for _, e := range f.ev.events {
		if e.Usage != nil && (e.Usage.Tokens < 0 || e.Usage.CostUSD < 0) {
			t.Fatalf("event %d records negative usage %+v", e.Seq, *e.Usage)
		}
	}
	validate(t, f.ev.events)
}

func TestNegativeUsageCountsAsZeroAfterRestart(t *testing.T) {
	p := negUse(t)
	results, ev, clk := machine.NewResults(), listSink{recSink{&sink{}}}, &clock{now: t0}
	w := &worker{fn: map[string]func(machine.Request) (machine.Result, error){
		"code": func(machine.Request) (machine.Result, error) {
			return machine.Result{}, errors.New("model unavailable")
		},
	}}
	for step, n := range map[string]int64{"contract-check": -1000, "generate": 60, "tests": 30} {
		w.fn[step] = func(r machine.Request) (machine.Result, error) {
			res := ok(r)
			res.Usage = machine.Usage{Tokens: n}
			return res, nil
		}
	}
	m1 := machine.New(machine.Config{Pattern: p, Worker: w, Checker: &checker{fn: noBlocking}, Clock: clk,
		Events: ev, Results: results, Model: "claude-haiku-5-5"})
	if r, err := m1.Start(context.Background(), (&fixture{t: t}).params("RUN-1")); err != nil || r.Status != machine.StatusInterrupted {
		t.Fatalf("setup: %v, %s", err, r.Status)
	}
	w2 := &worker{fn: map[string]func(machine.Request) (machine.Result, error){
		"code": func(r machine.Request) (machine.Result, error) {
			res := ok(r)
			res.Usage = machine.Usage{Tokens: 30}
			return res, nil
		},
	}}
	r, err := restarted(p, ev, results, clk, w2).Resume(context.Background(), "RUN-1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != machine.StatusWaiting { // 0 + 60 + 30 + 30 = 120 > 100
		t.Fatalf("status = %s, want handed over (the -1000 reads back as 0)", r.Status)
	}
}

func TestAgentStepWithoutAModelIsRefused(t *testing.T) {
	w := &worker{fn: map[string]func(machine.Request) (machine.Result, error){}}
	ev := &sink{}
	m := machine.New(machine.Config{Pattern: pattern(), Worker: w, Checker: &checker{fn: noBlocking}, Clock: &clock{now: t0},
		Events: ev, Results: machine.NewResults()}) // Model empty
	_, err := m.Start(context.Background(), (&fixture{t: t}).params("RUN-1"))
	if err == nil {
		t.Fatal("Start succeeded although the agent steps have no model id for the reuse key")
	}
	for _, c := range w.calls {
		if len(c.Step.Worker) > 6 && c.Step.Worker[:6] == "agent:" {
			t.Fatalf("agent step %s ran", c.Step.Name)
		}
	}
	for _, e := range ev.events {
		if e.Kind == "StepAccepted" && (e.Step == "tests" || e.Step == "code" || e.Step == "review") {
			t.Fatalf("agent step %s accepted without a model", e.Step)
		}
	}
}

// A fix served from Results (ResultReused) must carry changed_lines in the record too, so a restart
// on an empty Results store still gives the next review round the fix's lines.
func TestRestartKeepsChangedLinesOfAReusedFix(t *testing.T) {
	lines := []machine.Line{{File: "internal/thing.go", Line: 12}}
	finding := blocking("R-1", "internal/thing.go", 12)
	ev, clk, shared := listSink{recSink{&sink{}}}, &clock{now: t0}, machine.NewResults()
	codeCalls := 0
	newWorker := func(round2 func(machine.Request) (machine.Result, error)) *worker {
		return &worker{fn: map[string]func(machine.Request) (machine.Result, error){
			"code": func(r machine.Request) (machine.Result, error) {
				codeCalls++
				res := ok(r)
				if codeCalls == 2 {
					res.ChangedLines = lines
				}
				return res, nil
			},
			"review": func(r machine.Request) (machine.Result, error) {
				if r.Round <= 1 {
					res := ok(r)
					res.Findings = []machine.Finding{finding}
					return res, nil
				}
				return round2(r)
			},
		}}
	}
	down := func(machine.Request) (machine.Result, error) {
		return machine.Result{}, errors.New("model unavailable")
	}
	build := func(w *worker, results *machine.Results) *machine.Machine {
		return machine.New(machine.Config{Pattern: pattern(), Worker: w, Checker: &checker{fn: noBlocking}, Clock: clk,
			Events: ev, Results: results, Model: "claude-haiku-5-5"})
	}
	f := &fixture{t: t}
	// run 1 accepts the fix, then its review round 2 fails; its accepted results are stored
	m := build(newWorker(down), shared)
	if r, err := m.Start(context.Background(), f.params("RUN-1")); err != nil || r.Status != machine.StatusInterrupted {
		t.Fatalf("setup 1: %v, %s", err, r.Status)
	}
	if _, err := m.Cancel("RUN-1"); err != nil {
		t.Fatal(err)
	}
	// run 2 is served the code steps, fix included, from Results, and is interrupted at round 2 again
	calls := codeCalls
	if r, err := m.Start(context.Background(), f.params("RUN-2")); err != nil || r.Status != machine.StatusInterrupted {
		t.Fatalf("setup 2: %v, %s", err, r.Status)
	}
	if codeCalls != calls {
		t.Fatalf("the code step ran %d times in run 2, want it reused", codeCalls-calls)
	}
	reused := 0
	for _, e := range ev.events {
		if e.Run == "RUN-2" && e.Kind == "ResultReused" && len(e.ChangedLines) > 0 {
			reused++
			if !slices.Equal(e.ChangedLines, lines) {
				t.Fatalf("ResultReused changed_lines = %v", e.ChangedLines)
			}
		}
	}
	if reused != 1 {
		t.Fatalf("ResultReused events with changed_lines = %d, want 1", reused)
	}

	var seen machine.Request
	w3 := newWorker(func(r machine.Request) (machine.Result, error) { seen = r; return ok(r), nil })
	r, err := build(w3, machine.NewResults()).Resume(context.Background(), "RUN-2")
	if err != nil || r.Status != machine.StatusEnded {
		t.Fatalf("resume: %v, %s", err, r.Status)
	}
	if !slices.Equal(seen.ChangedLines, lines) {
		t.Fatalf("round %d got changed lines %v, want %v", seen.Round, seen.ChangedLines, lines)
	}
}
