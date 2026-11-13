package machine_test

// T067 (FR-008): a run never starts a step or a check the pattern does not declare. The machine
// runs only the pattern's steps, and for each step only that step's declared checks. These tests
// pin that behaviour and may already pass.

import (
	"context"
	"slices"
	"testing"

	"github.com/agentic-platform/engine/machine"
)

// strictChecker fails the test for any check its step does not declare.
func strictChecker(t *testing.T, declared machine.Pattern) *checker {
	t.Helper()
	return &checker{fn: func(r machine.CheckRequest) (machine.CheckResult, error) {
		for _, s := range declared.Steps {
			if s.Name == r.Step.Name {
				if !slices.Contains(s.Checks, r.Name) {
					t.Errorf("check %q asked for step %q, which declares %v", r.Name, s.Name, s.Checks)
				}
				return pass(r), nil
			}
		}
		t.Errorf("check %q asked for step %q, which the pattern does not declare", r.Name, r.Step.Name)
		return pass(r), nil
	}}
}

func TestMachineRunsOnlyTheDeclaredStepsAndTheirDeclaredChecks(t *testing.T) {
	p := pattern()
	f := newFixture(t, p)
	f.c = strictChecker(t, p)
	f.m = machine.New(machine.Config{Pattern: p, Worker: f.w, Checker: f.c, Clock: f.clk, Events: f.ev})
	r := f.start("RUN-1")
	if r.Outcome != machine.OutcomeDone {
		t.Fatalf("outcome = %q, want done", r.Outcome)
	}
	for _, s := range p.Steps {
		if len(f.w.callsTo(s.Name)) == 0 {
			t.Errorf("declared step %q never ran", s.Name)
		}
	}
	for _, c := range f.w.calls {
		if !slices.ContainsFunc(p.Steps, func(s machine.Step) bool { return s.Name == c.Step.Name }) {
			t.Errorf("worker called for undeclared step %q", c.Step.Name)
		}
	}
	for _, e := range f.ev.events {
		if e.Step != "" && !slices.ContainsFunc(p.Steps, func(s machine.Step) bool { return s.Name == e.Step }) {
			t.Errorf("event %s names undeclared step %q", e.Kind, e.Step)
		}
		if e.Check != nil {
			s := p.Steps[slices.IndexFunc(p.Steps, func(s machine.Step) bool { return s.Name == e.Step })]
			if !slices.Contains(s.Checks, e.Check.Name) {
				t.Errorf("event records check %q for step %q, which declares %v", e.Check.Name, s.Name, s.Checks)
			}
		}
	}
}

func TestMachineWithAShorterPatternNeverCallsTheWorkerOfAStepItLacks(t *testing.T) {
	full := pattern()
	short := full
	short.Steps = full.Steps[:2] // contract-check, generate: no tests, code or review
	f := newFixture(t, short)
	f.c = strictChecker(t, short)
	f.w.fn["tests"] = func(machine.Request) (machine.Result, error) {
		t.Error("the worker of an undeclared step was called")
		return machine.Result{}, nil
	}
	f.m = machine.New(machine.Config{Pattern: short, Worker: f.w, Checker: f.c, Clock: f.clk, Events: f.ev})
	r, err := f.m.Start(context.Background(), f.params("RUN-1"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != machine.OutcomeDone {
		t.Fatalf("outcome = %q, want done", r.Outcome)
	}
	if !slices.Equal(r.Accepted, []string{"contract-check", "generate"}) {
		t.Errorf("accepted = %v, want only the declared steps", r.Accepted)
	}
	for _, name := range []string{"tests", "code", "review"} {
		if n := len(f.w.callsTo(name)); n != 0 {
			t.Errorf("undeclared step %q ran %d times", name, n)
		}
	}
	for _, c := range f.c.calls {
		if c.Name == "TestsBuild" || c.Name == "AllTestsPass" || c.Name == "NoBlockingFindingOpen" {
			t.Errorf("a check of an undeclared step ran: %s", c.Name)
		}
	}
}

func TestMachineAsksOnlyForTheChecksAStepDeclares(t *testing.T) {
	p := pattern()
	p.Steps = slices.Clone(p.Steps[:2])
	p.Steps[0].Checks = []string{"ContractMatchesApproval"} // ScopeKept dropped from this step
	f := newFixture(t, p)
	f.c = strictChecker(t, p)
	f.m = machine.New(machine.Config{Pattern: p, Worker: f.w, Checker: f.c, Clock: f.clk, Events: f.ev})
	f.start("RUN-1")
	for _, c := range f.c.calls {
		if c.Step.Name == "contract-check" && c.Name != "ContractMatchesApproval" {
			t.Errorf("contract-check was checked with %q", c.Name)
		}
	}
}
