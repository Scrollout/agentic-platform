// Tests for the Service additions of T038 (U1): List, Reserve, CreateAs, CheckAnswers,
// CheckApprove, Buildable.
//
// Interface targeted:
//
//	func (s *Service) List() []Slice                                  // newest first, deleted absent
//	func (s *Service) Reserve() string                                // next S-<n>, no slice yet
//	func (s *Service) CreateAs(ctx, id, description string) (Result, error)
//	func (s *Service) CheckAnswers(id string, answers []Answer) error // Answer's refusals, no analyst call
//	func (s *Service) CheckApprove(id string, part Part, hash string) error
//	func (s *Service) Buildable(id string) error                      // PatternDoesNotFit, then NotApproved
package slices_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/slices"
)

func num(t *testing.T, id string) int {
	t.Helper()
	n, err := strconv.Atoi(strings.TrimPrefix(id, "S-"))
	if err != nil || !strings.HasPrefix(id, "S-") {
		t.Fatalf("%q is not S-<n>", id)
	}
	return n
}

func TestListIsNewestFirstAndWithoutDeleted(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	if got := s.List(); len(got) != 0 {
		t.Fatalf("empty service lists %d slices", len(got))
	}
	a := create(t, s, "Let me keep notes.")
	b := create(t, s, "Let me keep other notes.")
	c := create(t, s, "Let me keep more notes.")
	got := s.List()
	if len(got) != 3 || got[0].ID != c.ID || got[1].ID != b.ID || got[2].ID != a.ID {
		t.Fatalf("List = %v, want %s, %s, %s", ids(got), c.ID, b.ID, a.ID)
	}
	if err := s.Delete(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	got = s.List()
	if len(got) != 2 || got[0].ID != c.ID || got[1].ID != a.ID {
		t.Errorf("after Delete, List = %v, want %s, %s", ids(got), c.ID, a.ID)
	}
	got[0].Goals = nil // a copy: the service is unchanged
	if g, _ := s.Get(c.ID); len(g.Goals) == 0 {
		t.Error("List returned the stored slice, not a copy")
	}
}

func ids(sl []slices.Slice) []string {
	var out []string
	for _, s := range sl {
		out = append(out, s.ID)
	}
	return out
}

func TestReserveGivesIncreasingIDsAndCreatesNothing(t *testing.T) {
	s, a, _ := setup(t)
	r1, r2 := s.Reserve(), s.Reserve()
	if num(t, r2) != num(t, r1)+1 {
		t.Errorf("reserved %s then %s, want consecutive", r1, r2)
	}
	if len(s.List()) != 0 || a.calls != 0 {
		t.Errorf("Reserve created a slice or called the analyst")
	}
	if _, err := s.Get(r1); code(t, err) != "NotFound" {
		t.Errorf("a reserved ID is not a slice: %v", err)
	}
}

func TestCreateAsUsesTheReservedID(t *testing.T) {
	s, _, _ := setup(t)
	id := s.Reserve()
	r, err := s.CreateAs(context.Background(), id, "Let me keep notes.")
	if err != nil {
		t.Fatal(err)
	}
	if r.Slice.ID != id {
		t.Errorf("slice ID %s, want %s", r.Slice.ID, id)
	}
	if got, err := s.Get(id); err != nil || got.Description != "Let me keep notes." || got.State == "" {
		t.Errorf("Get(%s) = %+v, %v", id, got, err)
	}
}

func TestCreateAsRefusesUnreservedAndUsedIDs(t *testing.T) {
	ctx := context.Background()
	s, a, _ := setup(t)
	id := s.Reserve()
	if _, err := s.CreateAs(ctx, "S-"+strconv.Itoa(num(t, id)+50), "Let me keep notes."); err == nil {
		t.Error("an ID never reserved was accepted")
	}
	if _, err := s.CreateAs(ctx, "S-0x", "Let me keep notes."); err == nil {
		t.Error("a malformed ID was accepted")
	}
	if _, err := s.CreateAs(ctx, id, "Let me keep notes."); err != nil {
		t.Fatal(err)
	}
	calls := a.calls
	if _, err := s.CreateAs(ctx, id, "Let me keep other notes."); err == nil {
		t.Error("an ID already used was accepted")
	}
	if a.calls != calls || len(s.List()) != 1 {
		t.Errorf("a refused CreateAs ran the analyst or stored a slice")
	}
}

func TestIDsAreNeverReused(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	first := create(t, s, "Let me keep notes.")
	if err := s.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	// A reserved ID whose create failed (empty description) is not given out again.
	lost := s.Reserve()
	if _, err := s.CreateAs(ctx, lost, ""); err == nil {
		t.Fatal("empty description accepted")
	}
	seen := map[string]bool{first.ID: true, lost: true}
	for i := 0; i < 3; i++ {
		id := s.Reserve()
		if seen[id] {
			t.Fatalf("ID %s given out twice", id)
		}
		seen[id] = true
	}
	c := create(t, s, "Let me keep other notes.") // Create draws from the same sequence
	if seen[c.ID] {
		t.Fatalf("Create gave %s, already out", c.ID)
	}
	if num(t, c.ID) <= num(t, first.ID) {
		t.Errorf("Create gave %s after %s", c.ID, first.ID)
	}
}

func TestCheckAnswersRefusesLikeAnswerWithoutSideEffects(t *testing.T) {
	ctx := context.Background()
	s, a, sl := twoQuestions(t)
	before, _ := s.Get(sl.ID)
	if err := s.CheckAnswers(sl.ID, []slices.Answer{{Question: "Q-1", Option: intp(0)}, {Question: "Q-2", Text: "All"}}); err != nil {
		t.Fatalf("valid answers refused: %v", err)
	}
	if err := s.CheckAnswers("S-99", []slices.Answer{{Question: "Q-1", Text: "x"}}); code(t, err) != "NotFound" {
		t.Errorf("unknown slice: %v", err)
	}
	if err := s.CheckAnswers(sl.ID, []slices.Answer{{Question: "Q-9", Text: "x"}}); code(t, err) != "QuestionNotOpen" {
		t.Errorf("unknown question: %v", err)
	}
	if err := s.CheckAnswers(sl.ID, []slices.Answer{{Question: "Q-2", Text: string(make([]rune, 201))}}); code(t, err) != "InvalidRequest" {
		t.Errorf("too long: %v", err)
	}
	if a.calls != 1 {
		t.Errorf("CheckAnswers called the analyst (calls %d)", a.calls)
	}
	after, _ := s.Get(sl.ID)
	if len(after.History) != len(before.History) || after.State != "questions-open" || after.Questions[0].Answer != nil {
		t.Errorf("CheckAnswers changed the slice")
	}
	// An answered question is no longer open.
	if _, err := s.Answer(ctx, sl.ID, []slices.Answer{{Question: "Q-1", Option: intp(0)}, {Question: "Q-2", Text: "All"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckAnswers(sl.ID, []slices.Answer{{Question: "Q-1", Option: intp(1)}}); code(t, err) != "QuestionNotOpen" {
		t.Errorf("answered question: %v", err)
	}
}

func TestCheckApproveRefusesLikeApproveWithoutApproving(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	sl := create(t, s, "Let me keep notes.")
	if err := s.CheckApprove(sl.ID, slices.PartGoals, sl.Parts.Goals); err != nil {
		t.Fatalf("approvable part refused: %v", err)
	}
	if got, _ := s.Get(sl.ID); len(got.Approvals) != 0 || len(got.History) != len(sl.History) {
		t.Error("CheckApprove approved the part")
	}
	if err := s.CheckApprove(sl.ID, slices.PartGoals, sha("stale")); code(t, err) != "StaleContentHash" {
		t.Errorf("stale hash: %v", err)
	}
	if err := s.CheckApprove("S-99", slices.PartGoals, sl.Parts.Goals); code(t, err) != "NotFound" {
		t.Errorf("unknown slice: %v", err)
	}

	// PartFailsChecks: a goal ID that breaks the schema fails the goals part's Schema check.
	bad, err := s.ReplaceGoals(ctx, sl.ID, []slices.Goal{{ID: "X-1", Text: "bad id"}, sl.Goals[1]})
	if err != nil {
		t.Fatal(err)
	}
	if len(failedChecks(bad, "goals")) == 0 {
		t.Fatal("setup: the goals part does not fail its checks")
	}
	if err := s.CheckApprove(bad.ID, slices.PartGoals, bad.Parts.Goals); code(t, err) != "PartFailsChecks" {
		t.Errorf("failing part: %v", err)
	}
}

func TestCheckApproveContradictionOpen(t *testing.T) {
	s, _, sl := twoQuestions(t) // Q-1 concerns R-1 (rules), Q-2 concerns G-1 (goals)
	if err := s.CheckApprove(sl.ID, slices.PartRules, sl.Parts.Rules); code(t, err) != "ContradictionOpen" {
		t.Errorf("rules with an open question: %v", err)
	}
	if err := s.CheckApprove(sl.ID, slices.PartGoals, sl.Parts.Goals); code(t, err) != "ContradictionOpen" {
		t.Errorf("goals with an open question: %v", err)
	}
	if err := s.CheckApprove(sl.ID, slices.PartContract, sl.Parts.Contract); err != nil {
		t.Errorf("contract has no open question: %v", err)
	}
	if got, _ := s.Get(sl.ID); len(got.Approvals) != 0 {
		t.Error("CheckApprove approved a part")
	}
}

func TestCheckApprovePatternDoesNotFit(t *testing.T) {
	s := rvService(t, notesProposal(t), nil, nil) // no pattern: PatternFit fails closed
	sl := create(t, s, "Let me keep notes.")
	if err := s.CheckApprove(sl.ID, slices.PartContract, sl.Parts.Contract); code(t, err) != "PatternDoesNotFit" {
		t.Errorf("contract: %v", err)
	}
	if got, _ := s.Get(sl.ID); len(got.Approvals) != 0 {
		t.Error("CheckApprove approved a part")
	}
}

func TestBuildable(t *testing.T) {
	ctx := context.Background()
	t.Run("unknown slice", func(t *testing.T) {
		s, _, _ := setup(t)
		if err := s.Buildable("S-99"); code(t, err) != "NotFound" {
			t.Errorf("got %v", err)
		}
	})
	t.Run("pattern does not fit, nothing approved", func(t *testing.T) {
		s := rvService(t, notesProposal(t), nil, nil)
		sl := create(t, s, "Let me keep notes.")
		if err := s.Buildable(sl.ID); code(t, err) != "PatternDoesNotFit" {
			t.Errorf("got %v", err)
		}
	})
	t.Run("fit but nothing approved", func(t *testing.T) {
		s, _, _ := setup(t)
		sl := create(t, s, "Let me keep notes.")
		if err := s.Buildable(sl.ID); code(t, err) != "NotApproved" {
			t.Errorf("got %v", err)
		}
	})
	t.Run("each missing approval", func(t *testing.T) {
		for _, skip := range []slices.Part{slices.PartGoals, slices.PartRules, slices.PartContract} {
			s, _, _ := setup(t)
			sl := create(t, s, "Let me keep notes.")
			for _, p := range []slices.Part{slices.PartGoals, slices.PartRules, slices.PartContract} {
				if p == skip {
					continue
				}
				if _, err := s.Approve(ctx, sl.ID, p, rvHash(sl, p)); err != nil {
					t.Fatalf("approve %s: %v", p, err)
				}
			}
			if err := s.Buildable(sl.ID); code(t, err) != "NotApproved" {
				t.Errorf("without %s: got %v", skip, err)
			}
		}
	})
	t.Run("approval stale after a change", func(t *testing.T) {
		s, _, _ := setup(t)
		sl := approveAll(t, s, create(t, s, "Let me keep notes."))
		if err := s.Buildable(sl.ID); err != nil {
			t.Fatalf("fully approved slice: %v", err)
		}
		changed, err := s.ReplaceGoals(ctx, sl.ID, []slices.Goal{{ID: sl.Goals[0].ID, Text: "A changed goal"}, sl.Goals[1]})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Buildable(changed.ID); code(t, err) != "NotApproved" {
			t.Errorf("after the goals changed: %v", err)
		}
	})
	t.Run("fully approved", func(t *testing.T) {
		s, _, _ := setup(t)
		sl := approveAll(t, s, create(t, s, "Let me keep notes."))
		if err := s.Buildable(sl.ID); err != nil {
			t.Errorf("got %v", err)
		}
	})
}
