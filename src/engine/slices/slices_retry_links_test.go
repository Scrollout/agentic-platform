// Tests for T125 (owner, 2026-11-17; research D48): every slice.Trace finding of the analyst's
// proposal (a broken link) is sent back to the analyst with its other faults, up to the pattern's
// limits.attempts. When the last attempt still has link findings only, that attempt's slice is
// stored with its failed checks; a schema, syntax or unreadable-contract fault on the last attempt
// still ends in OperationFailed with nothing stored. Revisions through answers take the same path.
// They fail until propose/unusable send link findings back.
package slices_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/slices"
)

// untaggedR2 is the notes fixture with the @R-2 tag dropped from the scenario under "Rule: R-2",
// as an answer that follows the inject-skip-links instruction gives: rule R-2 has no scenario.
func untaggedR2(t *testing.T, scenario string) slices.Proposal {
	t.Helper()
	p := notesProposal(t)
	p.Examples = "Feature: Notes\n  Rule: R-1\n    @R-1 @E-1\n    Scenario: Keep\n      Given a note\n" +
		"  Rule: R-2\n    @E-2\n    Scenario: " + scenario + "\n      Given notes\n"
	return p
}

func joined(errs []string) string { return strings.Join(errs, "\n") }

func assertAllChecksPass(t *testing.T, sl slices.Slice) {
	t.Helper()
	for _, c := range sl.Checks {
		if !c.Passed {
			t.Errorf("failed check stored: %+v", c)
		}
	}
}

// The control: the corrected answer alone passes every check, so a failed check below comes from
// the link finding, not the fixture.
func TestNotesProposalPassesEveryCheck(t *testing.T) {
	s, _ := retryService(t, apiPattern(t), analystResult{p: notesProposal(t)})
	r, err := s.Create(context.Background(), "Let me keep notes.")
	if err != nil {
		t.Fatal(err)
	}
	assertAllChecksPass(t, r.Slice)
}

func TestRuleWithoutScenarioIsSentBack(t *testing.T) {
	bad, good := untaggedR2(t, "List"), notesProposal(t)
	s, a := retryService(t, apiPattern(t), analystResult{p: bad}, analystResult{p: good})
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
	if prev == nil || prev.Examples != bad.Examples {
		t.Errorf("second call's previous proposal = %+v, want the one without @R-2", prev)
	}
	if !strings.Contains(joined(errs), "R-2") || !strings.Contains(joined(errs), "scenario") {
		t.Errorf("second call's errors %q do not name rule R-2's missing scenario", errs)
	}
	if a.inputs[1].Description != "Let me keep notes." {
		t.Errorf("description on the second call = %q", a.inputs[1].Description)
	}
	if r.Slice.Examples != good.Examples {
		t.Error("the stored slice does not hold the corrected answer")
	}
	assertAllChecksPass(t, r.Slice)
	if h := r.Slice.History[0]; h.Kind != "proposed" || attemptsOfEntry(t, h) != 2 {
		t.Errorf("first history entry %q attempts %d, want proposed with 2", h.Kind, attemptsOfEntry(t, h))
	}
}

// Every kind of link Trace follows is sent back, and the errors name the offending ID.
func TestEveryLinkFindingIsSentBack(t *testing.T) {
	cases := map[string]struct {
		edit func(p *slices.Proposal)
		want string // in the errors sent back
	}{
		"rule names an unknown goal": {func(p *slices.Proposal) { p.Rules[1].Goal = "G-9" }, "G-9"},
		"rule uses an unknown term":  {func(p *slices.Proposal) { p.Rules[1].Terms = []string{"T-9"} }, "T-9"},
		"example serves an unknown rule": {func(p *slices.Proposal) {
			p.Examples += "  Rule: R-7\n    @R-7 @E-3\n    Scenario: Extra\n      Given more\n"
		}, "R-7"},
		"operation names an unknown rule": {func(p *slices.Proposal) {
			p.Contract = strings.Replace(p.Contract, "x-rules: [R-2]", "x-rules: [R-8]", 1)
		}, "R-8"},
		"operation without x-rules": {func(p *slices.Proposal) {
			p.Contract = strings.Replace(p.Contract, "      x-rules: [R-2]\n", "", 1)
		}, "listNotes"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			bad := notesProposal(t)
			c.edit(&bad)
			s, a := retryService(t, apiPattern(t), analystResult{p: bad}, analystResult{p: notesProposal(t)})
			r, err := s.Create(context.Background(), "Let me keep notes.")
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if a.calls != 2 {
				t.Fatalf("analyst called %d times, want 2", a.calls)
			}
			if _, errs := analystFeedback(a.inputs[1]); !strings.Contains(joined(errs), c.want) {
				t.Errorf("errors %q do not name %s", errs, c.want)
			}
			assertAllChecksPass(t, r.Slice)
		})
	}
}

func TestLinkFindingsOnEveryAttemptStoreTheLastAttempt(t *testing.T) {
	first, second, last := untaggedR2(t, "List one"), untaggedR2(t, "List two"), untaggedR2(t, "List three")
	s, a := retryService(t, apiPattern(t), analystResult{p: first}, analystResult{p: second}, analystResult{p: last})
	r, err := s.Create(context.Background(), "Let me keep notes.")
	if err != nil {
		t.Fatalf("Create: %v (link findings only must be stored, not fail)", err)
	}
	if a.calls != 3 {
		t.Errorf("analyst called %d times, want 3 (limits.attempts)", a.calls)
	}
	if r.Slice.Examples != last.Examples {
		t.Errorf("stored examples %q, want the last attempt's", r.Slice.Examples)
	}
	failed := false
	for _, c := range r.Slice.Checks {
		if !c.Passed && c.Check == slices.CheckTraceability && strings.Contains(c.Reason, "R-2") {
			failed = true
		}
	}
	if !failed {
		t.Errorf("no failed traceability check naming R-2: %+v", r.Slice.Checks)
	}
	if h := r.Slice.History[0]; h.Kind != "proposed" || attemptsOfEntry(t, h) != 3 {
		t.Errorf("first history entry %q attempts %d, want proposed with 3", h.Kind, attemptsOfEntry(t, h))
	}
	if got, err := s.Get(r.Slice.ID); err != nil || got.Examples != last.Examples {
		t.Errorf("Get(%s) = %q, %v; want the last attempt stored", r.Slice.ID, got.Examples, err)
	}
}

func TestSyntaxFaultOnTheLastAttemptFailsAfterLinkFindings(t *testing.T) {
	s, a := retryService(t, apiPattern(t),
		analystResult{p: untaggedR2(t, "List one")}, analystResult{p: untaggedR2(t, "List two")},
		analystResult{p: withExamples(t, badExamples)})
	_, err := s.Create(context.Background(), "Let me keep notes.")
	var e *slices.Error
	if !errors.As(err, &e) || e.Code != "OperationFailed" {
		t.Fatalf("error %v, want *slices.Error with code OperationFailed", err)
	}
	if a.calls != 3 {
		t.Errorf("analyst called %d times, want 3", a.calls)
	}
	if !strings.Contains(strings.ToLower(e.Detail), "examples") {
		t.Errorf("detail %q does not name the examples", e.Detail)
	}
	assertNothingStored(t, s)
}

func TestRevisionWithLinkFindingIsSentBack(t *testing.T) {
	first := qProposal(t, slices.QuestionDraft{About: "R-1", Text: "Keep forever?", Options: []string{"yes", "no"}})
	bad, good := untaggedR2(t, "List"), notesProposal(t)
	good.Goals[0].Text = "Keep a note for good"
	a := &fakeAnalyst{results: []analystResult{{p: first}, {p: bad}, {p: good}}}
	s := newServiceWithPattern(t, a, apiPattern(t))
	sl := create(t, s, "Let me keep notes.")
	got, err := s.Answer(context.Background(), sl.ID, []slices.Answer{{Question: "Q-1", Option: intp(0)}})
	if err != nil {
		t.Fatal(err)
	}
	if a.calls != 3 {
		t.Fatalf("analyst called %d times, want 3", a.calls)
	}
	prev, errs := analystFeedback(a.inputs[2])
	if prev == nil || prev.Examples != bad.Examples {
		t.Errorf("third call's previous proposal = %+v, want the revision without @R-2", prev)
	}
	if !strings.Contains(joined(errs), "R-2") {
		t.Errorf("third call's errors %q do not name R-2", errs)
	}
	if a.inputs[2].Current == nil || len(a.inputs[2].Questions) == 0 {
		t.Error("the call that resends a revision lost its current proposal or questions")
	}
	if got.Goals[0].Text != good.Goals[0].Text || got.Examples != good.Examples {
		t.Error("the stored slice does not hold the corrected revision")
	}
	assertAllChecksPass(t, got)
	if want := []string{"proposed", "answered", "revised"}; !equalStrings(historyKinds(got), want) {
		t.Fatalf("history %v", historyKinds(got))
	}
	if n := attemptsOfEntry(t, got.History[2]); n != 2 {
		t.Errorf("revised attempts = %d, want 2", n)
	}
}
