// Tests for an analyst answer that is unusable because it repeats a key (feature 007, US1):
// the analyst returns an error wrapping *slices.AnswerFault, and Create and Answer ask again with
// the fault's reason as feedback, up to the pattern's limits.attempts.
package slices_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/slices"
)

const faultKey = "examples"

func faultReason(key string) string {
	return fmt.Sprintf("the answer holds the key %q twice at %q", key, "/")
}

func faultErr(key string) error {
	return fmt.Errorf("analyst: %w", &slices.AnswerFault{Reason: faultReason(key)})
}

// openQuestionService returns a service whose first proposal asks Q-1, with the analyst's
// remaining results queued after it, and the created slice.
func openQuestionService(t *testing.T, rest ...analystResult) (*slices.Service, *fakeAnalyst, slices.Slice) {
	t.Helper()
	first := qProposal(t, slices.QuestionDraft{About: "R-1", Text: "Keep forever?", Options: []string{"yes", "no"}})
	a := &fakeAnalyst{results: append([]analystResult{{p: first}}, rest...)}
	s := newServiceWithPattern(t, a, apiPattern(t))
	return s, a, create(t, s, "Let me keep notes.")
}

func assertFaultFeedback(t *testing.T, in slices.AnalystInput) {
	t.Helper()
	if in.Previous != nil {
		t.Errorf("Previous = %+v after a fault, want nil (there is no usable proposal)", in.Previous)
	}
	if len(in.Errors) == 0 || !strings.Contains(strings.Join(in.Errors, "\n"), faultReason(faultKey)) {
		t.Errorf("Errors = %q, want the fault's reason %q", in.Errors, faultReason(faultKey))
	}
}

func assertFailedNamingKey(t *testing.T, err error) {
	t.Helper()
	var e *slices.Error
	if !errors.As(err, &e) || e.Code != "OperationFailed" {
		t.Fatalf("error %v, want *slices.Error with code OperationFailed", err)
	}
	if !strings.Contains(e.Detail, faultKey) {
		t.Errorf("detail %q does not name the key %q", e.Detail, faultKey)
	}
}

func TestCreateRetriesAfterAnAnswerFaultAndRecordsAttempts(t *testing.T) {
	a := &fakeAnalyst{results: []analystResult{{err: faultErr(faultKey)}, {p: notesProposal(t)}}}
	s := newServiceWithPattern(t, a, apiPattern(t))
	r, err := s.Create(context.Background(), "Let me keep notes.")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.calls != 2 {
		t.Fatalf("analyst called %d times, want 2", a.calls)
	}
	if in := a.inputs[0]; in.Previous != nil || len(in.Errors) != 0 {
		t.Errorf("first call got feedback: %+v %q", in.Previous, in.Errors)
	}
	assertFaultFeedback(t, a.inputs[1])
	if a.inputs[1].Description != "Let me keep notes." {
		t.Errorf("description on retry = %q", a.inputs[1].Description)
	}
	if len(r.Slice.History) != 1 || r.Slice.History[0].Kind != "proposed" || r.Slice.History[0].Attempts != 2 {
		t.Errorf("history %+v, want one proposed entry with attempts 2", r.Slice.History)
	}
	if r.Slice.Examples != notesProposal(t).Examples {
		t.Error("the stored slice does not hold the good proposal")
	}
}

func TestCreateFaultOnEveryAttemptFailsAndStoresNothing(t *testing.T) {
	a := &fakeAnalyst{results: []analystResult{{err: faultErr(faultKey)}}}
	s := newServiceWithPattern(t, a, apiPattern(t))
	_, err := s.Create(context.Background(), "Let me keep notes.")
	if err == nil {
		t.Fatal("Create succeeded though every answer was unusable")
	}
	if a.calls != 3 {
		t.Errorf("analyst called %d times, want 3 (limits.attempts)", a.calls)
	}
	assertFailedNamingKey(t, err)
	if got := s.List(); len(got) != 0 {
		t.Errorf("List has %d slices, want none", len(got))
	}
	assertNothingStored(t, s)
}

func TestCreateReturnsAnErrorThatIsNotAnAnswerFaultAtOnce(t *testing.T) {
	boom := errors.New("analyst: the model is down")
	a := &fakeAnalyst{results: []analystResult{{err: boom}, {p: notesProposal(t)}}}
	s := newServiceWithPattern(t, a, apiPattern(t))
	_, err := s.Create(context.Background(), "Let me keep notes.")
	if err == nil {
		t.Fatal("Create succeeded")
	}
	if a.calls != 1 {
		t.Errorf("analyst called %d times, want 1 (no retry)", a.calls)
	}
	if !errors.Is(err, boom) && !strings.Contains(err.Error(), boom.Error()) {
		t.Errorf("error %v does not carry the analyst's error", err)
	}
	var f *slices.AnswerFault
	if errors.As(err, &f) {
		t.Errorf("error %v wraps an AnswerFault, want none", err)
	}
	assertNothingStored(t, s)
}

func TestAnswerRetriesAfterAnAnswerFaultAndRecordsAttempts(t *testing.T) {
	good := notesProposal(t)
	s, a, sl := openQuestionService(t, analystResult{err: faultErr(faultKey)}, analystResult{p: good})
	got, err := s.Answer(context.Background(), sl.ID, []slices.Answer{{Question: "Q-1", Option: intp(0)}})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if a.calls != 3 {
		t.Fatalf("analyst called %d times, want 3 (proposal, faulty revision, good revision)", a.calls)
	}
	if in := a.inputs[1]; in.Current == nil || in.Previous != nil || len(in.Errors) != 0 {
		t.Errorf("first revision call: Current %v, Previous %v, Errors %q", in.Current, in.Previous, in.Errors)
	}
	retry := a.inputs[2]
	assertFaultFeedback(t, retry)
	if retry.Current == nil || len(retry.Current.Goals) != len(sl.Goals) {
		t.Errorf("retry's Current = %+v, want the slice's current proposal still set", retry.Current)
	}
	if len(retry.Questions) != 1 || retry.Questions[0].Answer == nil {
		t.Errorf("retry's questions = %+v, want Q-1 with its answer", retry.Questions)
	}
	if want := []string{"proposed", "answered", "revised"}; !equalStrings(historyKinds(got), want) {
		t.Fatalf("history %v, want %v", historyKinds(got), want)
	}
	if n := got.History[2].Attempts; n != 2 {
		t.Errorf("revised attempts = %d, want 2", n)
	}
	if n := got.History[0].Attempts; n != 1 {
		t.Errorf("proposed attempts = %d, want 1", n)
	}
}

func TestAnswerFaultOnEveryAttemptFailsAndLeavesTheSliceUnchanged(t *testing.T) {
	s, a, sl := openQuestionService(t, analystResult{err: faultErr(faultKey)})
	before, err := s.Get(sl.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Answer(context.Background(), sl.ID, []slices.Answer{{Question: "Q-1", Option: intp(0)}})
	if err == nil {
		t.Fatal("Answer succeeded though every revision was unusable")
	}
	if a.calls != 4 {
		t.Errorf("analyst called %d times, want 4 (proposal and 3 revision attempts)", a.calls)
	}
	assertFailedNamingKey(t, err)
	after, err := s.Get(sl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Parts, after.Parts) {
		t.Errorf("parts changed: %+v -> %+v", before.Parts, after.Parts)
	}
	if !reflect.DeepEqual(before.Approvals, after.Approvals) {
		t.Errorf("approvals changed: %+v -> %+v", before.Approvals, after.Approvals)
	}
	if !reflect.DeepEqual(before.Questions, after.Questions) {
		t.Errorf("questions changed (an answer was recorded?): %+v -> %+v", before.Questions, after.Questions)
	}
	for _, q := range after.Questions {
		if q.Answer != nil || !q.AnsweredAt.IsZero() {
			t.Errorf("question %s holds an answer: %+v", q.ID, q)
		}
	}
	if !reflect.DeepEqual(before.History, after.History) {
		t.Errorf("history changed: %v -> %v", historyKinds(before), historyKinds(after))
	}
	if after.State != before.State {
		t.Errorf("state %q -> %q", before.State, after.State)
	}
}

func TestAnswerReturnsAnErrorThatIsNotAnAnswerFaultAtOnce(t *testing.T) {
	boom := errors.New("analyst: the model is down")
	s, a, sl := openQuestionService(t, analystResult{err: boom}, analystResult{p: notesProposal(t)})
	before, err := s.Get(sl.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Answer(context.Background(), sl.ID, []slices.Answer{{Question: "Q-1", Option: intp(0)}})
	if err == nil {
		t.Fatal("Answer succeeded")
	}
	if a.calls != 2 {
		t.Errorf("analyst called %d times, want 2 (proposal, one revision, no retry)", a.calls)
	}
	var f *slices.AnswerFault
	if errors.As(err, &f) {
		t.Errorf("error %v wraps an AnswerFault, want none", err)
	}
	after, err := s.Get(sl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.History, after.History) || !reflect.DeepEqual(before.Questions, after.Questions) {
		t.Error("the slice changed though the analyst failed")
	}
}
