package durable_test

// Feature 012, T045 (US2, T019 B3): a step whose worker is a procedure (`procedure:<name>`, not
// only `procedure:fill`) reaches no model, so the events of its attempts carry no usage (nil),
// as a `fill` step's already do; an agent step's events carry usage, and a usage of zero is still
// a usage (0 tokens, $0), not an absence. Judged from the spec's Clarifications 2026-11-28 and the
// run event contract's `usage`. Own rig (names prefixed pu), so no existing test file is touched.

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// puRun runs a two-step pattern (a procedure step, then an agent step), two attempts each; the
// first attempt of each step fails in its worker after spending, the second succeeds.
type puRun struct {
	t      *testing.T
	env    *testsuite.TestWorkflowEnvironment
	mu     sync.Mutex
	events []machine.Event
	// usage the fake worker reports per step and attempt (index 0 is attempt 1).
	usage map[string][2]machine.Usage
}

func newPU(t *testing.T) *puRun {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	suite.SetLogger(log.NewStructuredLogger(slog.New(slog.DiscardHandler)))
	p := &puRun{t: t, env: suite.NewTestWorkflowEnvironment(), usage: map[string][2]machine.Usage{}}
	p.env.SetTestTimeout(60 * time.Second)
	p.env.RegisterWorkflowWithOptions(durable.Run, workflow.RegisterOptions{Name: "Run"})
	reg := func(name string, fn any) { p.env.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name}) }
	reg(durable.CallWorker, func(_ context.Context, c durable.WorkerCall) (durable.WorkerResult, error) {
		u := p.usage[c.Step.Name][c.Attempt-1]
		res := durable.WorkerResult{Usage: u, Took: time.Second}
		if c.Attempt == 1 {
			res.Failure = "the worker failed after spending"
			return res, nil
		}
		res.Model = "claude-haiku-5-5"
		res.Outputs = map[string]string{}
		for _, o := range c.Step.Outputs {
			res.Outputs[o] = fpHash(c.Step.Name + "/" + o)
		}
		res.Before = c.Before
		res.ProjectFiles = c.Before
		return res, nil
	})
	reg(durable.RunCheck, func(context.Context, durable.CheckCall) (durable.CheckResult, error) {
		return durable.CheckResult{Passed: true, Reason: "holds"}, nil
	})
	reg(durable.RestoreProject, func(context.Context, machine.RestoreRequest) (durable.Ran, error) { return durable.Ran{}, nil })
	reg(durable.CommitStep, func(context.Context, machine.CommitRequest) (durable.Ran, error) { return durable.Ran{}, nil })
	reg(durable.LookupReuse, func(context.Context, durable.ReuseLookup) (durable.Reused, error) { return durable.Reused{}, nil })
	reg(durable.PutResult, func(context.Context, durable.ReusePut) error { return nil })
	reg(durable.RecordEvent, func(_ context.Context, r durable.EventRecord) error {
		p.mu.Lock()
		p.events = append(p.events, r.Event)
		p.mu.Unlock()
		return nil
	})
	return p
}

func puPattern() machine.Pattern {
	return machine.Pattern{ID: "pu-pattern", Version: "1.0.0", Attempts: 2, ReviewRounds: 1,
		RunBudget:  machine.Budget{Tokens: 3000000, CostUSD: 5, Duration: 30 * time.Minute},
		StepBudget: machine.Budget{Tokens: 1000000, CostUSD: 2, Duration: 10 * time.Minute},
		Steps: []machine.Step{
			{Name: "generate", Worker: "procedure:generate", Inputs: []string{"slice/contract"}, Outputs: []string{"gen"}},
			{Name: "code", Worker: "agent:coder", AgentDigest: "sha256:" + fpHash("coder"),
				Inputs: []string{"gen"}, Outputs: []string{"code"}},
		}}
}

func (p *puRun) done() {
	p.t.Helper()
	in := durable.RunInput{Run: "RUN-9", Slice: "S-2", Project: "/projects/p2", Pattern: puPattern(), Model: "claude-haiku-5-5",
		Inputs: map[string]string{"slice/contract": fpHash("contract")}, Refs: []string{"G-1"}}
	p.env.RegisterDelayedCallback(func() { p.env.SignalWorkflow(durable.SignalCancel, nil) }, 60*24*time.Hour)
	p.env.ExecuteWorkflow("Run", in)
	if !p.env.IsWorkflowCompleted() {
		p.t.Fatal("the workflow did not complete")
	}
	if err := p.env.GetWorkflowError(); err != nil {
		p.t.Fatalf("workflow error: %v", err)
	}
	var r durable.RunResult
	if err := p.env.GetWorkflowResult(&r); err != nil || r.Outcome != machine.OutcomeDone {
		p.t.Fatalf("the run ended %q (%v), want done; events %v", r.Outcome, err, p.kinds())
	}
}

func (p *puRun) kinds() (out []string) {
	for _, e := range p.events {
		out = append(out, e.Kind+" "+e.Step)
	}
	return out
}

func (p *puRun) of(kind, step string) []machine.Event {
	var out []machine.Event
	for _, e := range p.events {
		if e.Kind == kind && e.Step == step {
			out = append(out, e)
		}
	}
	return out
}

// A procedure step reaches no model: its AttemptMade and StepRejected carry no usage, even
// though the worker reported a usage (0 tokens, $0), and a StepAccepted carries none either.
func TestAProcedureStepsEventsCarryNoUsage(t *testing.T) {
	p := newPU(t)
	p.usage["generate"] = [2]machine.Usage{{}, {}}
	p.usage["code"] = [2]machine.Usage{{Tokens: 40, CostUSD: 0.04}, {Tokens: 60, CostUSD: 0.06}}
	p.done()
	made, rej, acc := p.of("AttemptMade", "generate"), p.of("StepRejected", "generate"), p.of("StepAccepted", "generate")
	if len(made) != 1 || len(rej) != 1 || len(acc) != 1 {
		t.Fatalf("generate recorded %d AttemptMade, %d StepRejected, %d StepAccepted, want 1 each: %v", len(made), len(rej), len(acc), p.kinds())
	}
	for _, e := range append(append(made, rej...), acc...) {
		if e.Usage != nil {
			t.Errorf("%s of the procedure step generate carries usage %+v, want none (nil): it reaches no model", e.Kind, *e.Usage)
		}
	}
}

// An agent step's events carry usage: the failed attempt's use on its StepRejected (it made no
// AttemptMade), the made attempt's on its AttemptMade.
func TestAnAgentStepsEventsCarryUsage(t *testing.T) {
	p := newPU(t)
	p.usage["generate"] = [2]machine.Usage{{}, {}}
	p.usage["code"] = [2]machine.Usage{{Tokens: 40, CostUSD: 0.04}, {Tokens: 60, CostUSD: 0.06}}
	p.done()
	rej, made := p.of("StepRejected", "code"), p.of("AttemptMade", "code")
	if len(rej) != 1 || len(made) != 1 {
		t.Fatalf("code recorded %d StepRejected and %d AttemptMade, want 1 each: %v", len(rej), len(made), p.kinds())
	}
	if rej[0].Usage == nil || rej[0].Usage.Tokens != 40 || rej[0].Usage.CostUSD != 0.04 {
		t.Errorf("code's StepRejected usage %+v, want 40 tokens $0.04", rej[0].Usage)
	}
	if made[0].Usage == nil || made[0].Usage.Tokens != 60 || made[0].Usage.CostUSD != 0.06 {
		t.Errorf("code's AttemptMade usage %+v, want 60 tokens $0.06", made[0].Usage)
	}
}

// A usage of zero on an agent step is still usage (a count of 0), never an absence.
func TestAnAgentStepWithZeroUsageStillCarriesUsage(t *testing.T) {
	p := newPU(t)
	p.usage["generate"] = [2]machine.Usage{{}, {}}
	p.usage["code"] = [2]machine.Usage{{}, {}}
	p.done()
	for _, e := range append(p.of("StepRejected", "code"), p.of("AttemptMade", "code")...) {
		if e.Usage == nil {
			t.Errorf("%s of the agent step code carries no usage, want 0 tokens and $0 (present)", e.Kind)
		}
	}
}
