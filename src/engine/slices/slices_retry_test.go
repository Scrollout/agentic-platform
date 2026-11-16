// Tests for asking the analyst again (owner-approved 2026-11-12). They fail until Create retries.
//
// ASSUMPTION: the implementer adds to AnalystInput
//
//	Previous *Proposal // the unusable proposal of the attempt before; nil on the first call
//	Errors   []string  // what was wrong with it, one entry per finding
//
// If they are named differently, only analystFeedback below needs adapting.
//
// Which proposals are retried: those whose examples or contract text does not parse, and those
// that break the proposal schema. Other failing checks are stored as today. The attempt limit is
// `limits.attempts` of Config.Pattern. When all attempts are unusable, Create returns
// *Error{Code: OperationFailed} (the contract's universal error) naming examples or contract,
// and stores nothing.
package slices_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/slice"
)

const (
	badExamples = "this is not gherkin at all\n  Given nothing\n"
	badContract = "openapi: 3.1.0\ninfo: [unclosed\n  title: x\n"
)

// analystFeedback is what a call received about the proposal before it.
func analystFeedback(in slices.AnalystInput) (*slices.Proposal, []string) {
	return in.Previous, in.Errors
}

func retryService(t *testing.T, pattern []byte, results ...analystResult) (*slices.Service, *fakeAnalyst) {
	t.Helper()
	a := &fakeAnalyst{results: results}
	return newServiceWithPattern(t, a, pattern), a
}

func newServiceWithPattern(t *testing.T, a slices.Analyst, pattern []byte) *slices.Service {
	t.Helper()
	schemas, err := slice.LoadSchemas("../api/preview/2026-11-12-preview")
	if err != nil {
		t.Fatal(err)
	}
	s, err := slices.New(slices.Config{Schemas: schemas, Analyst: a, Diff: &fakeDiff{}, Pattern: pattern, Lint: &fakeLint{}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func withExamples(t *testing.T, ex string) slices.Proposal {
	p := notesProposal(t)
	p.Examples = ex
	return p
}

func withContract(t *testing.T, c string) slices.Proposal {
	p := notesProposal(t)
	p.Contract = c
	return p
}

func apiPattern(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assertNothingStored(t *testing.T, s *slices.Service) {
	t.Helper()
	if _, err := s.Get("S-1"); err == nil {
		t.Error("a slice was stored, want none")
	}
}

func TestBadExamplesThenGoodIsRetriedWithFeedback(t *testing.T) {
	good := notesProposal(t)
	s, a := retryService(t, apiPattern(t), analystResult{p: withExamples(t, badExamples)}, analystResult{p: good})
	r, err := s.Create(context.Background(), "Let me keep notes.")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.calls != 2 {
		t.Fatalf("analyst called %d times, want 2", a.calls)
	}
	if prev, errs := analystFeedback(a.inputs[0]); prev != nil || len(errs) != 0 {
		t.Errorf("first call got feedback: %v %v", prev, errs)
	}
	prev, errs := analystFeedback(a.inputs[1])
	if prev == nil || prev.Examples != badExamples {
		t.Errorf("second call's previous proposal = %+v, want the one with the bad examples", prev)
	}
	if !strings.Contains(strings.ToLower(strings.Join(errs, "\n")), "examples") {
		t.Errorf("second call's errors %q do not name the examples", errs)
	}
	if a.inputs[1].Description != "Let me keep notes." {
		t.Errorf("description on retry = %q", a.inputs[1].Description)
	}
	if r.Slice.Examples != good.Examples {
		t.Error("the stored slice does not hold the good proposal")
	}
	for _, c := range r.Slice.Checks {
		if !c.Passed && strings.Contains(c.Reason, "not valid Gherkin") {
			t.Errorf("a syntax failure was stored: %+v", c)
		}
	}
}

func TestBadContractThenGoodIsRetriedWithFeedback(t *testing.T) {
	s, a := retryService(t, apiPattern(t), analystResult{p: withContract(t, badContract)}, analystResult{p: notesProposal(t)})
	if _, err := s.Create(context.Background(), "Let me keep notes."); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.calls != 2 {
		t.Fatalf("analyst called %d times, want 2", a.calls)
	}
	prev, errs := analystFeedback(a.inputs[1])
	if prev == nil || prev.Contract != badContract {
		t.Errorf("previous proposal = %+v, want the one with the bad contract", prev)
	}
	if !strings.Contains(strings.ToLower(strings.Join(errs, "\n")), "contract") {
		t.Errorf("errors %q do not name the contract", errs)
	}
}

func TestProposalBreakingTheSchemaIsRetried(t *testing.T) {
	bad := notesProposal(t)
	bad.Goals[0].ID = "not-an-id"
	s, a := retryService(t, apiPattern(t), analystResult{p: bad}, analystResult{p: notesProposal(t)})
	if _, err := s.Create(context.Background(), "Let me keep notes."); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.calls != 2 {
		t.Fatalf("analyst called %d times, want 2", a.calls)
	}
	if prev, errs := analystFeedback(a.inputs[1]); prev == nil || len(errs) == 0 {
		t.Errorf("no feedback on retry: %v %v", prev, errs)
	}
}

func TestAlwaysUnparseableStopsAtTheAttemptLimit(t *testing.T) {
	for name, bad := range map[string]slices.Proposal{
		"examples": withExamples(t, badExamples),
		"contract": withContract(t, badContract),
	} {
		t.Run(name, func(t *testing.T) {
			s, a := retryService(t, apiPattern(t), analystResult{p: bad})
			_, err := s.Create(context.Background(), "Let me keep notes.")
			if err == nil {
				t.Fatal("Create succeeded with an unusable proposal")
			}
			if a.calls != 3 {
				t.Errorf("analyst called %d times, want 3 (limits.attempts)", a.calls)
			}
			var e *slices.Error
			if !errors.As(err, &e) || e.Code != "OperationFailed" {
				t.Fatalf("error %v, want *slices.Error with code OperationFailed", err)
			}
			if !strings.Contains(strings.ToLower(e.Detail), name) {
				t.Errorf("detail %q does not say what was wrong (%s)", e.Detail, name)
			}
			assertNothingStored(t, s)
		})
	}
}

func TestGoalWithoutRuleIsNotRetried(t *testing.T) {
	p := notesProposal(t)
	p.Rules = p.Rules[:1] // G-2 has no rule now
	p.Examples = "Feature: Notes\n  Rule: R-1\n    @R-1 @E-1\n    Scenario: Keep\n      Given a note\n"
	p.Contract = strings.Replace(p.Contract, "x-rules: [R-2]", "x-rules: [R-1]", 1)
	s, a := retryService(t, apiPattern(t), analystResult{p: p}, analystResult{p: notesProposal(t)})
	r, err := s.Create(context.Background(), "Let me keep notes.")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.calls != 1 {
		t.Errorf("analyst called %d times, want 1", a.calls)
	}
	failed := false
	for _, c := range r.Slice.Checks {
		if !c.Passed && c.Part == "goals" || !c.Passed && strings.Contains(c.Reason, "G-2") {
			failed = true
		}
	}
	if !failed {
		t.Errorf("no failed check for the goal without a rule: %+v", r.Slice.Checks)
	}
}

func TestOneAttemptPatternDoesNotRetry(t *testing.T) {
	s, a := retryService(t, fixture(t, "pattern-attempts-1.yaml"),
		analystResult{p: withExamples(t, badExamples)}, analystResult{p: notesProposal(t)})
	if _, err := s.Create(context.Background(), "Let me keep notes."); err == nil {
		t.Error("Create succeeded")
	}
	if a.calls != 1 {
		t.Errorf("analyst called %d times, want 1", a.calls)
	}
	assertNothingStored(t, s)
}

func TestAnalystErrorIsNotRetried(t *testing.T) {
	boom := errors.New("the model is down")
	s, a := retryService(t, apiPattern(t), analystResult{err: boom}, analystResult{p: notesProposal(t)})
	_, err := s.Create(context.Background(), "Let me keep notes.")
	if !errors.Is(err, boom) {
		t.Errorf("error %v, want it to wrap the analyst's", err)
	}
	if a.calls != 1 {
		t.Errorf("analyst called %d times, want 1", a.calls)
	}
	assertNothingStored(t, s)
}
