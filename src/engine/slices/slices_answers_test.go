// Tests for answering questions and deleting slices (task T037b). They fail until T037b adds the
// API below to package slices; existing tests, helpers and fixtures are reused, not changed.
//
// Minimal API (package slices), from openapi.yaml answerQuestions and deleteSlice and
// slice-defs.schema.json `question`:
//
//	// Answer is the developer's answer to one question: exactly one of Option (the position of
//	// a chosen option, from 0) or Text (at most 200 characters).
//	type Answer struct{ Question string; Option *int; Text string }
//	// Question gains the recorded answer (nil while open) and its time.
//	//   type Question struct{ ID, About, Text string; Options []string; Answer *Answer; AnsweredAt time.Time }
//	// A question is open while Answer is nil. State is questions-open while one is open.
//	func (s *Service) Answer(ctx context.Context, id string, answers []Answer) (Slice, error)
//	// Answer records the answers, asks the analyst to revise (AnalystInput.Current is the slice's
//	// current proposal; AnalystInput.Questions are ALL its questions, answered ones with Answer
//	// set; Description is unchanged), takes the revised parts, checks again, and appends history
//	// "answered" then "revised" (the latter names the revision's Model and AgentDigest). The
//	// revision's Questions are NEW questions only; they get the next Q-<n> IDs.
//	// Errors: NotFound (no slice), QuestionNotOpen (a question that is already answered or does
//	// not exist), InvalidRequest (no answers, an option out of range, neither or both of
//	// Option and Text), TooManyQuestions (the slice would exceed 10 questions in total).
//	// A refused Answer changes nothing (no answer recorded, no history).
//
//	// Delete erases the slice and its records (deleteSlice). Runs are a later task.
//	func (s *Service) Delete(ctx context.Context, id string) error // NotFound when unknown
package slices_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/agentic-platform/engine/slices"
)

func qProposal(t *testing.T, qs ...slices.QuestionDraft) slices.Proposal {
	p := notesProposal(t)
	p.Questions = qs
	return p
}

func intp(i int) *int { return &i }

func historyKinds(s slices.Slice) []string {
	var k []string
	for _, h := range s.History {
		k = append(k, h.Kind)
	}
	return k
}

func equalStrings(a, b []string) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

// twoQuestions returns a service whose first proposal asks Q-1 (rules, with options) and Q-2
// (goals, free text); the second call revises: goal G-1 gets new text and no new question.
func twoQuestions(t *testing.T) (*slices.Service, *fakeAnalyst, slices.Slice) {
	first := qProposal(t,
		slices.QuestionDraft{About: "R-1", Text: "Keep forever?", Options: []string{"yes", "no"}},
		slices.QuestionDraft{About: "G-1", Text: "Which notes?"})
	revised := notesProposal(t)
	revised.Goals[0].Text = "Keep a note for good"
	revised.Model, revised.AgentDigest = "claude-revise-test", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	a := &fakeAnalyst{results: []analystResult{{p: first}, {p: revised}}}
	s := newService(t, a, &fakeDiff{})
	sl := create(t, s, "Let me keep notes.")
	if sl.State != "questions-open" || len(sl.Questions) != 2 {
		t.Fatalf("setup: state %q, %d questions", sl.State, len(sl.Questions))
	}
	return s, a, sl
}

func TestAnswerRecordsAnswersAndRevisesThroughAnalyst(t *testing.T) {
	s, a, sl := twoQuestions(t)
	got, err := s.Answer(context.Background(), sl.ID, []slices.Answer{
		{Question: "Q-1", Option: intp(0)}, {Question: "Q-2", Text: "All of them"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.calls != 2 {
		t.Fatalf("analyst calls %d, want 2 (proposal, revision)", a.calls)
	}
	in := a.inputs[1]
	if in.Description != "Let me keep notes." || in.Current == nil || len(in.Current.Goals) != 2 || in.Current.Goals[0].Text != "Keep a <note> & café" {
		t.Errorf("revision input must carry the description and the current proposal: %+v", in)
	}
	if len(in.Questions) != 2 || in.Questions[0].Answer == nil || in.Questions[0].Answer.Option == nil ||
		*in.Questions[0].Answer.Option != 0 || in.Questions[1].Answer == nil || in.Questions[1].Answer.Text != "All of them" {
		t.Errorf("revision input must carry the questions with their answers: %+v", in.Questions)
	}
	for _, q := range got.Questions {
		if q.Answer == nil || q.AnsweredAt.IsZero() {
			t.Errorf("question %s: answer and time must be recorded: %+v", q.ID, q)
		}
	}
	if got.Goals[0].Text != "Keep a note for good" {
		t.Errorf("the revised parts must replace the slice's: %q", got.Goals[0].Text)
	}
	if got.Parts.Goals != sha(`{"goals":[{"id":"G-1","text":"Keep a note for good"},{"id":"G-2","text":"List\tnotes"}]}`) {
		t.Errorf("part hashes must follow the revision")
	}
	if want := []string{"proposed", "answered", "revised"}; !equalStrings(historyKinds(got), want) {
		t.Errorf("history %v, want %v", historyKinds(got), want)
	}
	rev := got.History[2]
	if rev.Model != "claude-revise-test" || rev.AgentDigest == "" {
		t.Errorf("a revision names its model and agent digest: %+v", rev)
	}
	if got.State != "checked" {
		t.Errorf("state %q, want checked once no question is open", got.State)
	}
}

func TestAnsweredQuestionsStopBlockingApproval(t *testing.T) {
	ctx := context.Background()
	s, _, sl := twoQuestions(t)
	if _, err := s.Approve(ctx, sl.ID, slices.PartRules, sl.Parts.Rules); code(t, err) != "ContradictionOpen" {
		t.Fatalf("want ContradictionOpen before answering, got %v", err)
	}
	got, err := s.Answer(ctx, sl.ID, []slices.Answer{{Question: "Q-1", Option: intp(1)}, {Question: "Q-2", Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	got = approveAll(t, s, got)
	if got.State != "approved" {
		t.Errorf("state %q, want approved", got.State)
	}
}

func TestAnswerOneOfTwoKeepsTheOtherBlocking(t *testing.T) {
	ctx := context.Background()
	first := qProposal(t,
		slices.QuestionDraft{About: "R-1", Text: "a?"}, slices.QuestionDraft{About: "G-1", Text: "b?"})
	// The revision keeps asking nothing new; Q-2 stays open because it was not answered.
	a := &fakeAnalyst{results: []analystResult{{p: first}, {p: notesProposal(t)}}}
	s := newService(t, a, &fakeDiff{})
	sl := create(t, s, "Let me keep notes.")
	got, err := s.Answer(ctx, sl.ID, []slices.Answer{{Question: "Q-1", Text: "yes"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "questions-open" {
		t.Errorf("state %q, want questions-open while Q-2 is open", got.State)
	}
	if got.Questions[0].Answer == nil || got.Questions[1].Answer != nil {
		t.Errorf("only Q-1 is answered: %+v", got.Questions)
	}
	if _, err := s.Approve(ctx, sl.ID, slices.PartGoals, got.Parts.Goals); code(t, err) != "ContradictionOpen" {
		t.Errorf("Q-2 still blocks goals: %v", err)
	}
	if _, err := s.Approve(ctx, sl.ID, slices.PartRules, got.Parts.Rules); err != nil {
		t.Errorf("Q-1 is answered, rules can be approved: %v", err)
	}
}

func TestAnswerQuestionNotOpen(t *testing.T) {
	ctx := context.Background()
	s, a, sl := twoQuestions(t)
	if _, err := s.Answer(ctx, sl.ID, []slices.Answer{{Question: "Q-1", Option: intp(0)}, {Question: "Q-2", Text: "x"}}); err != nil {
		t.Fatal(err)
	}
	calls := a.calls
	// RESOLVED: an unknown question is not open either (the contract has no other 409 for it).
	for _, q := range []string{"Q-1", "Q-9"} {
		_, err := s.Answer(ctx, sl.ID, []slices.Answer{{Question: q, Text: "again"}})
		if code(t, err) != "QuestionNotOpen" {
			t.Errorf("answer to %s: want QuestionNotOpen, got %v", q, err)
		}
	}
	if a.calls != calls {
		t.Errorf("a refused answer must not run the analyst")
	}
	got, _ := s.Get(sl.ID)
	if want := []string{"proposed", "answered", "revised"}; !equalStrings(historyKinds(got), want) {
		t.Errorf("a refused answer must not change history: %v", historyKinds(got))
	}
}

func TestAnswerRefusals(t *testing.T) {
	ctx := context.Background()
	s, a, sl := twoQuestions(t)
	if _, err := s.Answer(ctx, "S-99", []slices.Answer{{Question: "Q-1", Text: "x"}}); code(t, err) != "NotFound" {
		t.Errorf("unknown slice: want NotFound, got %v", err)
	}
	bad := map[string][]slices.Answer{
		"none":           nil,
		"option range":   {{Question: "Q-1", Option: intp(2)}},
		"neither":        {{Question: "Q-1"}},
		"both":           {{Question: "Q-1", Option: intp(0), Text: "x"}},
		"option no opts": {{Question: "Q-2", Option: intp(0)}},
		"text too long":  {{Question: "Q-2", Text: string(make([]rune, 201))}},
	}
	for name, ans := range bad {
		if _, err := s.Answer(ctx, sl.ID, ans); code(t, err) != "InvalidRequest" {
			t.Errorf("%s: want InvalidRequest, got %v", name, err)
		}
	}
	if a.calls != 1 {
		t.Errorf("refused answers must not run the analyst (calls %d)", a.calls)
	}
}

func TestAnswerTooManyQuestionsOnRevision(t *testing.T) {
	ctx := context.Background()
	var six, five []slices.QuestionDraft
	for i := 0; i < 6; i++ {
		six = append(six, slices.QuestionDraft{About: "R-1", Text: fmt.Sprintf("q%d?", i)})
	}
	for i := 0; i < 5; i++ { // 6 + 5 = 11 in total
		five = append(five, slices.QuestionDraft{About: "R-1", Text: fmt.Sprintf("n%d?", i)})
	}
	a := &fakeAnalyst{results: []analystResult{{p: qProposal(t, six...)}, {p: qProposal(t, five...)}}}
	s := newService(t, a, &fakeDiff{})
	sl := create(t, s, "Let me keep notes.")
	var ans []slices.Answer
	for _, q := range sl.Questions {
		ans = append(ans, slices.Answer{Question: q.ID, Text: "ok"})
	}
	_, err := s.Answer(ctx, sl.ID, ans)
	if code(t, err) != "TooManyQuestions" {
		t.Fatalf("want TooManyQuestions, got %v", err)
	}
	// RESOLVED: a refused revision leaves the slice as it was; the questions stay open.
	got, _ := s.Get(sl.ID)
	if got.State != "questions-open" || got.Questions[0].Answer != nil || len(got.History) != 1 {
		t.Errorf("slice must be unchanged: state %q, history %v", got.State, historyKinds(got))
	}
}

func TestAnswerNewQuestionsGetNextIDs(t *testing.T) {
	ctx := context.Background()
	first := qProposal(t, slices.QuestionDraft{About: "R-1", Text: "a?"})
	second := qProposal(t, slices.QuestionDraft{About: "R-2", Text: "b?"})
	a := &fakeAnalyst{results: []analystResult{{p: first}, {p: second}}}
	s := newService(t, a, &fakeDiff{})
	sl := create(t, s, "Let me keep notes.")
	got, err := s.Answer(ctx, sl.ID, []slices.Answer{{Question: "Q-1", Text: "ok"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Questions) != 2 || got.Questions[1].ID != "Q-2" || got.Questions[1].Answer != nil || got.Questions[0].Answer == nil {
		t.Errorf("questions %+v", got.Questions)
	}
	if got.State != "questions-open" {
		t.Errorf("state %q", got.State)
	}
}

func TestAnswerWithdrawsApprovalOfAChangedPart(t *testing.T) {
	ctx := context.Background()
	s, _, sl := twoQuestions(t)
	got, err := s.Answer(ctx, sl.ID, []slices.Answer{{Question: "Q-1", Option: intp(0)}, {Question: "Q-2", Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := approval(got, slices.PartGoals); ok {
		t.Errorf("no approval can exist yet")
	}
	// The revision changed goals only; its hash differs from the first proposal's.
	if got.Parts.Goals == sl.Parts.Goals || got.Parts.Contract != sl.Parts.Contract {
		t.Errorf("goals hash must change, contract hash must not")
	}
}

// ---- deletion ----

func TestDeleteErasesTheSlice(t *testing.T) {
	ctx := context.Background()
	s, a, _ := setup(t)
	sl := create(t, s, "Let me keep notes.")
	sl = approveAll(t, s, sl)
	if err := s.Delete(ctx, sl.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(sl.ID); code(t, err) != "NotFound" {
		t.Errorf("Get after Delete: want NotFound, got %v", err)
	}
	if _, err := s.ReplaceGoals(ctx, sl.ID, nil); code(t, err) != "NotFound" {
		t.Errorf("ReplaceGoals after Delete: want NotFound")
	}
	if _, err := s.Approve(ctx, sl.ID, slices.PartGoals, sl.Parts.Goals); code(t, err) != "NotFound" {
		t.Errorf("Approve after Delete: want NotFound")
	}
	if err := s.Delete(ctx, sl.ID); code(t, err) != "NotFound" {
		t.Errorf("second Delete: want NotFound, got %v", err)
	}
	// The approved slice is gone, so the same description is proposed anew, with a new ID.
	r, err := s.Create(ctx, "Let me keep notes.")
	if err != nil {
		t.Fatal(err)
	}
	if r.Reused || a.calls != 2 || r.Slice.ID == sl.ID {
		t.Errorf("reused=%v calls=%d id=%s (old %s): a deleted slice is not offered again and its ID is not reused", r.Reused, a.calls, r.Slice.ID, sl.ID)
	}
}

func TestDeleteUnknownAndMalformedID(t *testing.T) {
	s, _, _ := setup(t)
	if err := s.Delete(context.Background(), "S-99"); code(t, err) != "NotFound" {
		t.Errorf("unknown: %v", err)
	}
	if err := s.Delete(context.Background(), "nope"); code(t, err) != "InvalidRequest" {
		t.Errorf("malformed: %v", err)
	}
}

func TestDeleteLeavesOtherSlices(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	a1 := create(t, s, "one")
	a2 := create(t, s, "two")
	if err := s.Delete(ctx, a1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(a2.ID); err != nil {
		t.Errorf("the other slice must stay: %v", err)
	}
}
