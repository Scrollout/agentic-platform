package machine_test

// T012 cases (k), (l) and (n): every activity's options, what its payloads may hold, and the size
// of a run's history at feature 001's limits. The interface is in workflow_durable_test.go.

import (
	"encoding/base64"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	"go.temporal.io/sdk/temporal"
	"go.yaml.in/yaml/v3"
)

var wfActivities = []string{durable.CallWorker, durable.RunCheck, durable.RestoreProject, durable.CommitStep,
	durable.LookupReuse, durable.PutResult, durable.RecordEvent}

// wfOneReviewRound makes round 1 of the review find a confirmed blocking finding and round 2 none,
// so a run uses every activity, a fix and a FindingRecorded.
func wfOneReviewRound(c durable.WorkerCall) (machine.Result, error) {
	r := wfOK(c)
	if c.Step.Name == "review" && c.Round == 1 {
		r.Findings = []machine.Finding{wfBlocking("R-1", 12)}
	}
	if c.Step.Name == "code" && c.Round > 0 {
		r.ChangedLines = []machine.Line{{File: "internal/app/app.go", Line: 12}}
	}
	return r, nil
}

func wfTimeLimit(t *testing.T, in any) time.Duration {
	switch v := in.(type) {
	case durable.WorkerCall:
		return v.TimeLimit
	case durable.CheckCall:
		return v.TimeLimit
	case machine.RestoreRequest:
		return v.TimeLimit
	case machine.CommitRequest:
		return v.TimeLimit
	case durable.ReuseLookup:
		return v.TimeLimit
	case durable.ReusePut:
		return v.TimeLimit
	case durable.EventRecord:
		return v.TimeLimit
	}
	t.Fatalf("unknown activity input %T", in)
	return 0
}

// (k) Every activity has MaximumAttempts 1, a StartToClose of its call's time limit plus 30 s and
// a heartbeat timeout of 30 s.
func TestWorkflowActivityOptions(t *testing.T) {
	t.Run("timeouts", func(t *testing.T) {
		h := newWF(t, wfPattern())
		h.work = wfOneReviewRound
		h.run()
		h.wantOutcome(machine.OutcomeDone)
		for _, name := range wfActivities {
			calls := h.callsOf(name)
			if len(calls) == 0 {
				t.Fatalf("%s was never called", name)
			}
			for _, c := range calls {
				limit := wfTimeLimit(t, c.In)
				if c.StartToClose != limit+30*time.Second || c.Heartbeat != 30*time.Second {
					t.Fatalf("%s: StartToClose %s, heartbeat %s; want %s (its time limit %s + 30s) and 30s", name, c.StartToClose, c.Heartbeat, limit+30*time.Second, limit)
				}
				if limit <= 0 && name != durable.LookupReuse && name != durable.PutResult && name != durable.RecordEvent {
					t.Fatalf("%s has no time limit", name)
				}
			}
		}
	})
	// Temporal retries an activity whose policy allows more than one attempt; its fake then sees
	// attempt 2. Each activity's first call fails retryably once.
	for _, name := range wfActivities {
		t.Run("one attempt: "+name, func(t *testing.T) {
			h := newWF(t, wfPattern())
			h.work = wfOneReviewRound
			first := wfFirst(name, 1, func(any) bool { return true })
			h.fail = func(got string, in any) error {
				if first(got, in) {
					return temporal.NewApplicationError("failed once", "Planted")
				}
				return nil
			}
			h.signalAt(time.Hour, durable.SignalCancel, nil) // ends a run left waiting
			h.env.ExecuteWorkflow(durable.Run, h.in)
			if len(h.callsOf(name)) == 0 {
				t.Fatalf("%s was never called", name)
			}
			for _, c := range h.callsOf(name) {
				if c.Attempt != 1 {
					t.Fatalf("%s ran as Temporal's attempt %d: its retry policy is not MaximumAttempts 1", name, c.Attempt)
				}
			}
		})
	}
}

// (l) No activity's input or result, nor the workflow's input or result, holds a prompt, a
// completion, file content or finding text (FR-016). The fakes know all four (wfWorkerResult,
// wfCheckResult); each payload is searched for them as text and as base64.
func TestWorkflowPayloadsHoldNoContent(t *testing.T) {
	planted := []string{wfPrompt, wfCompletion, wfFileContent, wfFindingText}
	for _, tc := range []struct {
		name string
		work func(durable.WorkerCall) (machine.Result, error)
	}{
		{"a run with a fix", wfOneReviewRound},
		{"a run handed over with open findings", func(c durable.WorkerCall) (machine.Result, error) {
			r := wfOK(c)
			if c.Step.Name == "review" {
				r.Findings = []machine.Finding{wfBlocking("R-1", 12), wfBlocking("R-2", 20)}
			}
			return r, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWF(t, wfPattern())
			h.work = tc.work
			h.signalAt(time.Hour, durable.SignalAcceptHandOver, nil)
			h.run()
			if len(h.eventsOf("FindingRecorded")) == 0 {
				t.Fatal("no finding was recorded: the case would not test finding text")
			}
			var result durable.RunResult
			_ = h.env.GetWorkflowResult(&result)
			payloads := map[string]string{"the workflow's input": wfJSON(t, h.in), "the workflow's result": wfJSON(t, result)}
			examined := 0
			h.mu.Lock()
			calls := append([]wfCall(nil), h.calls...)
			h.mu.Unlock()
			for i, c := range calls {
				payloads[c.Name+" input #"+strconv.Itoa(i)] = wfJSON(t, c.In)
				if c.Out != nil {
					payloads[c.Name+" result #"+strconv.Itoa(i)] = wfJSON(t, c.Out)
				}
			}
			for where, p := range payloads {
				examined++
				for _, s := range planted {
					if strings.Contains(p, s) || strings.Contains(p, base64.StdEncoding.EncodeToString([]byte(s))) {
						t.Fatalf("%s holds %q: %s", where, s, p)
					}
				}
			}
			t.Logf("examined %d payloads of %d activity calls", examined, len(calls))
		})
	}
}

// wfCatalogue reads the catalogue's pattern (src/catalogue/api-feature-go/pattern.yaml).
func wfCatalogue(t *testing.T) machine.Pattern {
	t.Helper()
	b, err := os.ReadFile("../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatal(err)
	}
	type budget struct {
		Tokens  int64   `yaml:"tokens"`
		Cost    float64 `yaml:"cost_usd"`
		Minutes int     `yaml:"minutes"`
	}
	var f struct {
		ID      string `yaml:"id"`
		Version string `yaml:"version"`
		Limits  struct {
			Attempts     int `yaml:"attempts"`
			ReviewRounds int `yaml:"review_rounds"`
		} `yaml:"limits"`
		Budgets struct{ Run, Step budget } `yaml:"budgets"`
		Steps   []struct {
			Name         string   `yaml:"name"`
			Worker       string   `yaml:"worker"`
			AgentDigest  string   `yaml:"agent_digest"`
			Inputs       []string `yaml:"inputs"`
			Outputs      []string `yaml:"outputs"`
			Writable     []string `yaml:"writable"`
			Checks       []string `yaml:"checks"`
			Dependencies struct {
				Allowed   []string `yaml:"allowed"`
				Forbidden []string `yaml:"forbidden"`
			} `yaml:"dependencies"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	bud := func(x budget) machine.Budget {
		return machine.Budget{Tokens: x.Tokens, CostUSD: x.Cost, Duration: time.Duration(x.Minutes) * time.Minute}
	}
	p := machine.Pattern{ID: f.ID, Version: f.Version, Attempts: f.Limits.Attempts, ReviewRounds: f.Limits.ReviewRounds,
		RunBudget: bud(f.Budgets.Run), StepBudget: bud(f.Budgets.Step)}
	for _, s := range f.Steps {
		p.Steps = append(p.Steps, machine.Step{Name: s.Name, Worker: s.Worker, AgentDigest: s.AgentDigest, Inputs: s.Inputs,
			Outputs: s.Outputs, Writable: s.Writable, Checks: s.Checks,
			Dependencies: machine.Dependencies{Allowed: s.Dependencies.Allowed, Forbidden: s.Dependencies.Forbidden}})
	}
	if len(p.Steps) == 0 || p.Attempts != 3 || p.ReviewRounds != 3 {
		t.Fatalf("catalogue pattern read as %d steps, %d attempts, %d rounds", len(p.Steps), p.Attempts, p.ReviewRounds)
	}
	return p
}

// (n) A run at feature 001's limits (the catalogue pattern's steps, 3 attempts of every step and
// fix, 3 review rounds, every check) keeps its history under Temporal's warning of 10,240 events
// (research R9). The test environment shows no history, so the count is bounded from what the run
// did: each activity is at most 6 events (scheduled, started, closed, and the workflow task it
// wakes: scheduled, started, completed), each timer 7 (with its side effect), each signal 4, and
// the start and end 5.
func TestWorkflowHistoryStaysUnderTheLimit(t *testing.T) {
	p := wfCatalogue(t)
	h := newWF(t, p)
	h.work = func(c durable.WorkerCall) (machine.Result, error) {
		r := wfOK(c)
		if c.Step.Name == "review" && c.Round < 3 && c.Attempt == 3 {
			r.Findings = []machine.Finding{wfBlocking("R-1", 12)}
		}
		return r, nil
	}
	h.check = func(c durable.CheckCall) (machine.CheckResult, error) {
		if c.Attempt < 3 && c.Name == c.Step.Checks[0] { // attempts 1 and 2 of every step and round fail
			return wfFail(c), nil
		}
		return wfDefaultCheck(c)
	}
	h.run()
	h.wantOutcome(machine.OutcomeDone)

	steps := len(p.Steps) - 1 // every step but the review
	if got, want := len(h.callsOf(durable.CallWorker)), 3*steps+3*p.ReviewRounds+3*(p.ReviewRounds-1); got != want {
		t.Fatalf("%d worker calls, want %d: the run is not at the limits", got, want)
	}
	checks := 0
	for _, s := range p.Steps {
		checks += len(s.Checks)
	}
	activities := 0
	for _, name := range wfActivities {
		activities += len(h.callsOf(name))
	}
	h.mu.Lock()
	timers, signals := h.timers, h.signals
	h.mu.Unlock()
	bound := 6*activities + 7*timers + 4*signals + 5
	t.Logf("%d steps, %d checks; %d activity calls, %d timers, %d signals, %d events recorded: at most %d history events",
		len(p.Steps), checks, activities, timers, signals, len(h.events()), bound)
	if bound >= 10240 {
		t.Fatalf("history bound %d reaches Temporal's warning of 10,240 events", bound)
	}
}
