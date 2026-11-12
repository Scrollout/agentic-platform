// Tests for checks that cross parts (T040 review, spec US1.5, FR-004, FR-005): a slice is never
// approved and never buildable while any check on any part fails. After a change, every part that
// now has a failing check loses its approval. Existing helpers are reused, not changed.
package slices_test

import (
	"context"
	stdslices "slices"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/slices"
)

// crossFailing returns the parts that have a failing check.
func crossFailing(s slices.Slice) []slices.Part {
	var out []slices.Part
	for _, p := range []slices.Part{slices.PartGoals, slices.PartRules, slices.PartContract} {
		if len(failedChecks(s, string(p))) > 0 {
			out = append(out, p)
		}
	}
	return out
}

// crossAssertSound fails unless the slice is sound: no part with a failing check holds an
// approval, the state is not approved, and Buildable refuses with NotApproved.
func crossAssertSound(t *testing.T, s *slices.Service, sl slices.Slice) {
	t.Helper()
	failing := crossFailing(sl)
	if len(failing) == 0 {
		t.Fatalf("setup: the change must leave a failing check: %+v", sl.Checks)
	}
	for _, p := range failing {
		if _, ok := approval(sl, p); ok {
			t.Errorf("the %s part fails a check but keeps its approval", p)
		}
	}
	if sl.State == "approved" {
		t.Errorf("state approved while %v fail checks", failing)
	}
	err := s.Buildable(sl.ID)
	if err == nil {
		t.Errorf("Buildable accepts a slice whose %v parts fail checks", failing)
	} else if c := code(t, err); c != "NotApproved" {
		t.Errorf("Buildable code %q, want NotApproved", c)
	}
}

// crossApproveRest approves, in order, every part that is not approved at its current hash and
// passes its checks, and ignores the refusal of a part that fails.
func crossApproveRest(t *testing.T, s *slices.Service, sl slices.Slice) slices.Slice {
	t.Helper()
	for _, p := range []slices.Part{slices.PartGoals, slices.PartRules, slices.PartContract} {
		hash := map[slices.Part]string{slices.PartGoals: sl.Parts.Goals, slices.PartRules: sl.Parts.Rules, slices.PartContract: sl.Parts.Contract}[p]
		if got, err := s.Approve(context.Background(), sl.ID, p, hash); err == nil {
			sl = got
		} else if c := code(t, err); c != "PartFailsChecks" && c != "PatternDoesNotFit" {
			t.Fatalf("approve %s: %v", p, err)
		}
	}
	return sl
}

func TestReplacingGoalsThatLeaveARuleDanglingWithdrawsTheRulesApproval(t *testing.T) {
	ctx := context.Background()
	s, a, _ := setup(t)
	sl := approveAll(t, s, create(t, s, "Let me keep notes."))
	before := len(sl.History)
	got, err := s.ReplaceGoals(ctx, sl.ID, []slices.Goal{sl.Goals[0]})
	if err != nil {
		t.Fatal(err)
	}
	if len(failedChecks(got, "rules")) == 0 {
		t.Fatalf("setup: R-2 names the missing G-2, so the rules part must fail: %+v", got.Checks)
	}
	if _, ok := approval(got, slices.PartRules); ok {
		t.Errorf("the rules approval must be withdrawn: the rules part now fails Traceability")
	}
	if len(got.History) <= before || !stdslices.ContainsFunc(got.History[before:], func(h slices.HistoryEntry) bool {
		return stdslices.Contains(h.Refs, string(slices.PartRules))
	}) {
		t.Errorf("the withdrawal of the rules approval must be in the history: %+v", got.History[before:])
	}
	crossAssertSound(t, s, got)

	// Approving the goals must not make the slice approved or buildable.
	got, err = s.Approve(ctx, sl.ID, slices.PartGoals, got.Parts.Goals)
	if err != nil {
		t.Fatal(err)
	}
	crossAssertSound(t, s, got)

	// Reuse: the same description is not offered the unsound slice.
	r, err := s.Create(ctx, "Let me keep notes.")
	if err != nil {
		t.Fatal(err)
	}
	if r.Reused || r.Slice.ID == sl.ID || a.calls != 2 {
		t.Errorf("an unsound slice must not be offered for reuse: reused=%v id=%s analyst calls=%d", r.Reused, r.Slice.ID, a.calls)
	}
}

func TestDroppingARuleTheContractNamesWithdrawsTheContractApproval(t *testing.T) {
	ctx := context.Background()
	s, a, _ := setup(t)
	sl := approveAll(t, s, create(t, s, "Let me keep notes."))
	rules := slices.RulesPart{Rules: sl.Rules[:1], Terms: sl.Terms,
		Examples: "Feature: Notes\n  Rule: R-1\n    @R-1 @E-1\n    Scenario: Keep\n      Given a note\n"}
	got, err := s.ReplaceRules(ctx, sl.ID, rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(failedChecks(got, "contract")) == 0 {
		t.Fatalf("setup: x-rules names the dropped R-2, so the contract part must fail: %+v", got.Checks)
	}
	if _, ok := approval(got, slices.PartContract); ok {
		t.Errorf("the contract approval must be withdrawn: the contract part now fails Traceability")
	}
	crossAssertSound(t, s, got)

	got = crossApproveRest(t, s, got)
	crossAssertSound(t, s, got)

	r, err := s.Create(ctx, "Let me keep notes.")
	if err != nil {
		t.Fatal(err)
	}
	if r.Reused || a.calls != 2 {
		t.Errorf("an unsound slice must not be offered for reuse: reused=%v analyst calls=%d", r.Reused, a.calls)
	}
}

func TestAnAnswerThatRevisesAPartWithdrawsApprovalsOfPartsItNowBreaks(t *testing.T) {
	ctx := context.Background()
	first := qProposal(t, slices.QuestionDraft{About: "contract/listNotes", Text: "Which paths?"})
	revised := notesProposal(t)
	revised.Goals = revised.Goals[:1] // drops G-2, which R-2 names
	a := &fakeAnalyst{results: []analystResult{{p: first}, {p: revised}}}
	s := newService(t, a, &fakeDiff{})
	sl := create(t, s, "Let me keep notes.")
	// The open question concerns the contract only, so goals and rules can be approved now.
	for _, p := range []struct {
		part slices.Part
		hash string
	}{{slices.PartGoals, sl.Parts.Goals}, {slices.PartRules, sl.Parts.Rules}} {
		var err error
		if sl, err = s.Approve(ctx, sl.ID, p.part, p.hash); err != nil {
			t.Fatalf("approve %s: %v", p.part, err)
		}
	}
	got, err := s.Answer(ctx, sl.ID, []slices.Answer{{Question: "Q-1", Text: "all of them"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(failedChecks(got, "rules")) == 0 {
		t.Fatalf("setup: R-2 names the dropped G-2, so the rules part must fail: %+v", got.Checks)
	}
	if _, ok := approval(got, slices.PartRules); ok {
		t.Errorf("the revision broke the rules part; its approval must be withdrawn")
	}
	crossAssertSound(t, s, got)

	got = crossApproveRest(t, s, got)
	crossAssertSound(t, s, got)

	r, err := s.Create(ctx, "Let me keep notes.")
	if err != nil {
		t.Fatal(err)
	}
	if r.Reused || r.Slice.ID == sl.ID {
		t.Errorf("an unsound slice must not be offered for reuse: reused=%v id=%s", r.Reused, r.Slice.ID)
	}
}

func TestApprovingAFailingPartAfterACrossPartChangeIsRefused(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	sl := approveAll(t, s, create(t, s, "Let me keep notes."))
	got, err := s.ReplaceGoals(ctx, sl.ID, []slices.Goal{sl.Goals[0]})
	if err != nil {
		t.Fatal(err)
	}
	// Whatever the rules approval's fate, a new approval of the failing part is refused.
	_, err = s.Approve(ctx, sl.ID, slices.PartRules, got.Parts.Rules)
	if err == nil || !strings.Contains(code(t, err), "PartFailsChecks") {
		t.Errorf("approving a part with a failing check: got %v, want PartFailsChecks", err)
	}
}
