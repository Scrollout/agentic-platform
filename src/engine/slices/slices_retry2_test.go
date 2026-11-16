// Tests for T037c (owner-approved 2026-11-13). They fail until the implementer
//   - marks the unreadable-contract findings of checks.Fit as unusable, so Create asks again;
//   - adds an optional integer `attempts` (>= 1) to HistoryEntry (Go: HistoryEntry.Attempts int; schema:
//     slice.schema.json history item "attempts"), set on "proposed" and "revised" entries.
//
// The Go field is read by reflection so the rest of the package still compiles.
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

const (
	dupKeyContract   = "openapi: 3.1.0\ninfo:\n  title: x\n  title: y\npaths: {}\n"
	multiDocContract = "openapi: 3.1.0\ninfo:\n  title: x\npaths: {}\n---\nopenapi: 3.1.0\ninfo:\n  title: y\npaths: {}\n"
)

func attemptsOfEntry(t *testing.T, h slices.HistoryEntry) int {
	t.Helper()
	f := reflect.ValueOf(h).FieldByName("Attempts")
	if !f.IsValid() || f.Kind() != reflect.Int {
		t.Fatal("slices.HistoryEntry has no int field Attempts")
	}
	return int(f.Int())
}

func TestFitUnreadableContractIsRetriedWithFeedback(t *testing.T) {
	for name, bad := range map[string]string{"duplicate keys": dupKeyContract, "several documents": multiDocContract} {
		t.Run(name, func(t *testing.T) {
			s, a := retryService(t, apiPattern(t), analystResult{p: withContract(t, bad)}, analystResult{p: notesProposal(t)})
			r, err := s.Create(context.Background(), "Let me keep notes.")
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if a.calls != 2 {
				t.Fatalf("analyst called %d times, want 2", a.calls)
			}
			prev, errs := analystFeedback(a.inputs[1])
			if prev == nil || prev.Contract != bad {
				t.Errorf("previous proposal = %+v, want the unreadable one", prev)
			}
			if !strings.Contains(strings.ToLower(strings.Join(errs, "\n")), "contract") {
				t.Errorf("errors %q do not name the contract", errs)
			}
			if r.Slice.Contract != notesProposal(t).Contract {
				t.Error("the stored slice does not hold the good proposal")
			}
		})
	}
}

func TestFitUnreadableContractEndsOperationFailed(t *testing.T) {
	for name, bad := range map[string]string{"duplicate keys": dupKeyContract, "several documents": multiDocContract} {
		t.Run(name, func(t *testing.T) {
			s, a := retryService(t, apiPattern(t), analystResult{p: withContract(t, bad)})
			_, err := s.Create(context.Background(), "Let me keep notes.")
			var e *slices.Error
			if !errors.As(err, &e) || e.Code != "OperationFailed" {
				t.Fatalf("error %v, want OperationFailed", err)
			}
			if a.calls != 3 {
				t.Errorf("analyst called %d times, want 3", a.calls)
			}
			if !strings.Contains(strings.ToLower(e.Detail), "contract") {
				t.Errorf("detail %q does not name the contract", e.Detail)
			}
			assertNothingStored(t, s)
		})
	}
}

func TestProposedEntryRecordsAttempts(t *testing.T) {
	for _, n := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			var rs []analystResult
			for i := 1; i < n; i++ {
				rs = append(rs, analystResult{p: withExamples(t, badExamples)})
			}
			rs = append(rs, analystResult{p: notesProposal(t)})
			s, _ := retryService(t, apiPattern(t), rs...)
			r, err := s.Create(context.Background(), "Let me keep notes.")
			if err != nil {
				t.Fatal(err)
			}
			if got := attemptsOfEntry(t, r.Slice.History[0]); got != n || r.Slice.History[0].Kind != "proposed" {
				t.Errorf("first entry %q attempts = %d, want proposed with %d", r.Slice.History[0].Kind, got, n)
			}
		})
	}
}

func TestNilPatternGivesOneAttemptAndRecordsIt(t *testing.T) {
	s, a := retryService(t, nil, analystResult{p: withExamples(t, badExamples)}, analystResult{p: notesProposal(t)})
	_, err := s.Create(context.Background(), "Let me keep notes.")
	if err == nil {
		t.Fatal("Create succeeded with unusable examples")
	}
	if a.calls != 1 {
		t.Errorf("analyst called %d times, want 1", a.calls)
	}
	s, a = retryService(t, nil, analystResult{p: notesProposal(t)})
	r, err := s.Create(context.Background(), "Let me keep notes.")
	if err != nil || a.calls != 1 {
		t.Fatalf("err %v, calls %d", err, a.calls)
	}
	if got := attemptsOfEntry(t, r.Slice.History[0]); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
}

func TestRevisedEntryRecordsAttempts(t *testing.T) {
	first := qProposal(t, slices.QuestionDraft{About: "R-1", Text: "Keep forever?", Options: []string{"yes", "no"}})
	bad := withExamples(t, badExamples)
	good := notesProposal(t)
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
	if want := []string{"proposed", "answered", "revised"}; !equalStrings(historyKinds(got), want) {
		t.Fatalf("history %v", historyKinds(got))
	}
	if n := attemptsOfEntry(t, got.History[0]); n != 1 {
		t.Errorf("proposed attempts = %d, want 1", n)
	}
	if n := attemptsOfEntry(t, got.History[2]); n != 2 {
		t.Errorf("revised attempts = %d, want 2", n)
	}
}

func TestTooManyQuestionsIsNotRetried(t *testing.T) {
	p := notesProposal(t)
	for i := 0; i < 11; i++ {
		p.Questions = append(p.Questions, slices.QuestionDraft{About: "G-1", Text: fmt.Sprintf("Question %d?", i)})
	}
	s, a := retryService(t, apiPattern(t), analystResult{p: p}, analystResult{p: notesProposal(t)})
	_, err := s.Create(context.Background(), "Let me keep notes.")
	var e *slices.Error
	if !errors.As(err, &e) || e.Code != "TooManyQuestions" {
		t.Fatalf("error %v, want TooManyQuestions", err)
	}
	if a.calls != 1 {
		t.Errorf("analyst called %d times, want 1", a.calls)
	}
	assertNothingStored(t, s)
}

func TestNarrowDescriptionIsNotRetried(t *testing.T) {
	s, a := retryService(t, apiPattern(t), analystResult{err: slices.ErrMultipleUseCases}, analystResult{p: notesProposal(t)})
	_, err := s.Create(context.Background(), "Notes and billing and chat.")
	var e *slices.Error
	if !errors.As(err, &e) || e.Code != "NarrowDescription" {
		t.Fatalf("error %v, want NarrowDescription", err)
	}
	if a.calls != 1 {
		t.Errorf("analyst called %d times, want 1", a.calls)
	}
	assertNothingStored(t, s)
}
