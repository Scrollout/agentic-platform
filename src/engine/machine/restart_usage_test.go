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
	"testing"

	"github.com/agentic-platform/engine/machine"
)

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
