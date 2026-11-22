// Tests for feature 007, US2 (T008): on Answer, a revision that lacks a goal, term, rule, example
// or operation ID of the slice's current proposal, or that moves a kept rule to another goal or a
// kept example under another rule, is unusable: it is sent back with one fault per problem, after
// the existing faults, up to the pattern's limits.attempts. They fail until Answer compares the
// revision with the current proposal.
package slices_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	pslices "github.com/agentic-platform/engine/slices"
)

const (
	secretAnswer = "SECRET-ANSWER-TEXT-9141"

	keptExamplesR1Only = "Feature: Notes\n  Rule: R-1\n    @R-1 @E-1\n    Scenario: Keep\n      Given a note\n"
	keptExamplesR2Only = "Feature: Notes\n  Rule: R-2\n    @R-2 @E-2\n    Scenario: List\n      Given notes\n"
	keptSwapped        = "Feature: Notes\n  Rule: R-1\n    @R-1 @E-2\n    Scenario: List\n      Given notes\n" +
		"  Rule: R-2\n    @R-2 @E-1\n    Scenario: Keep\n      Given a note\n"
	keptContractKeepOnly = "openapi: 3.1.0\ninfo:\n  title: Notes\n  version: '1'\npaths:\n  /notes:\n    post:\n      operationId: keepNote\n      x-rules: [R-1]\n      responses:\n        '201':\n          description: The note was kept.\n"
	keptNewExamples      = "Feature: Notes\n  Rule: R-1\n    @R-1 @E-1\n    Scenario: Keep\n      Given a note\n" +
		"  Rule: R-2\n    @R-2 @E-2\n    Scenario: List\n      Given notes\n" +
		"  Rule: R-3\n    @R-3 @E-3\n    Scenario: Empty\n      Given a trash\n"
	keptNewContract = "openapi: 3.1.0\ninfo:\n  title: Notes\n  version: '1'\npaths:\n  /notes:\n    get:\n      operationId: listNotes\n      x-rules: [R-2]\n      responses:\n        '200':\n          description: The notes.\n    post:\n      operationId: keepNote\n      x-rules: [R-1]\n      responses:\n        '201':\n          description: The note was kept.\n" +
		"  /trash:\n    post:\n      operationId: emptyTrash\n      x-rules: [R-3]\n      responses:\n        '204':\n          description: Emptied.\n"
)

// keptService returns a service whose analyst first proposes the notes fixture with a question,
// then gives the revisions in order (the last repeats), and the created slice.
func keptService(t *testing.T, revisions ...pslices.Proposal) (*pslices.Service, *fakeAnalyst, pslices.Slice) {
	t.Helper()
	rs := []analystResult{{p: qProposal(t, pslices.QuestionDraft{About: "R-1", Text: "Keep forever?", Options: []string{"yes", "no"}})}}
	for _, p := range revisions {
		rs = append(rs, analystResult{p: p})
	}
	a := &fakeAnalyst{results: rs}
	s := newServiceWithPattern(t, a, apiPattern(t))
	return s, a, create(t, s, "Let me keep notes.")
}

func answerQ1(s *pslices.Service, id string) (pslices.Slice, error) {
	return s.Answer(context.Background(), id, []pslices.Answer{{Question: "Q-1", Text: secretAnswer}})
}

func idFault(e string) bool {
	return strings.Contains(e, "drops ") || strings.Contains(e, " served ") || strings.Contains(e, " was under ")
}

func countOf(errs []string, want string) int {
	n := 0
	for _, e := range errs {
		if e == want {
			n++
		}
	}
	return n
}

func dropFault(id string) string { return "the revision drops " + id + " of the current proposal" }

func TestRevisionLosingAnIDIsSentBackWithItsFault(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(p *pslices.Proposal)
		faults []string
	}{
		{"goal", func(p *pslices.Proposal) { p.Goals = p.Goals[:1] }, []string{dropFault("G-2")}},
		{"term", func(p *pslices.Proposal) { p.Terms = nil; p.Rules[0].Terms = []string{} }, []string{dropFault("T-1")}},
		{"rule", func(p *pslices.Proposal) { p.Rules = p.Rules[:1]; p.Examples = keptExamplesR1Only }, []string{dropFault("R-2"), dropFault("E-2")}},
		{"example", func(p *pslices.Proposal) { p.Examples = keptExamplesR2Only }, []string{dropFault("E-1")}},
		{"operation", func(p *pslices.Proposal) { p.Contract = keptContractKeepOnly }, []string{dropFault("listNotes")}},
		{"several at once", func(p *pslices.Proposal) {
			p.Goals = p.Goals[:1]
			p.Contract = keptContractKeepOnly
			p.Examples = keptExamplesR1Only
		}, []string{dropFault("G-2"), dropFault("E-2"), dropFault("listNotes")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bad, good := notesProposal(t), notesProposal(t)
			c.mutate(&bad)
			good.Goals[0].Text = "Keep a note for good"
			s, a, sl := keptService(t, bad, good)
			// the third call (second attempt) must already see the fault; the fake repeats `good`
			got, err := answerQ1(s, sl.ID)
			if err != nil {
				t.Fatalf("Answer: %v", err)
			}
			if a.calls != 3 {
				t.Fatalf("analyst called %d times, want 3 (proposal, refused revision, good revision)", a.calls)
			}
			retry := a.inputs[2]
			for _, f := range c.faults {
				if n := countOf(retry.Errors, f); n != 1 {
					t.Errorf("errors %q hold %q %d times, want once", retry.Errors, f, n)
				}
			}
			if n := len(slices.DeleteFunc(slices.Clone(retry.Errors), func(e string) bool { return !strings.Contains(e, "drops ") })); n != len(c.faults) {
				t.Errorf("errors %q hold %d drop faults, want %d (one per lost ID)", retry.Errors, n, len(c.faults))
			}
			if retry.Previous == nil || retry.Previous.Examples != bad.Examples || retry.Previous.Contract != bad.Contract || len(retry.Previous.Goals) != len(bad.Goals) {
				t.Errorf("Previous = %+v, want the refused revision", retry.Previous)
			}
			if retry.Current == nil || retry.Current.Examples != sl.Examples || retry.Current.Contract != sl.Contract || len(retry.Current.Goals) != len(sl.Goals) {
				t.Errorf("Current = %+v, want the slice's current proposal unchanged", retry.Current)
			}
			if h := got.History[len(got.History)-1]; h.Kind != "revised" || attemptsOfEntry(t, h) != 2 {
				t.Errorf("last history entry %q attempts %d, want revised with 2", h.Kind, attemptsOfEntry(t, h))
			}
			if got.Goals[0].Text != good.Goals[0].Text {
				t.Error("the stored slice does not hold the good revision")
			}
		})
	}
}

func TestRevisionMovingAKeptRuleToAnotherGoalIsSentBack(t *testing.T) {
	bad, good := notesProposal(t), notesProposal(t)
	bad.Rules[1].Goal = "G-1"
	s, a, sl := keptService(t, bad, good)
	if _, err := answerQ1(s, sl.ID); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if a.calls != 3 {
		t.Fatalf("analyst called %d times, want 3", a.calls)
	}
	if want := "R-2 served G-2 and now serves G-1"; countOf(a.inputs[2].Errors, want) != 1 {
		t.Errorf("errors %q lack %q", a.inputs[2].Errors, want)
	}
}

func TestRevisionMovingAKeptExampleUnderAnotherRuleIsSentBack(t *testing.T) {
	bad, good := notesProposal(t), notesProposal(t)
	bad.Examples = keptSwapped
	s, a, sl := keptService(t, bad, good)
	if _, err := answerQ1(s, sl.ID); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if a.calls != 3 {
		t.Fatalf("analyst called %d times, want 3", a.calls)
	}
	for _, want := range []string{"E-1 was under R-1 and is now under R-2", "E-2 was under R-2 and is now under R-1"} {
		if countOf(a.inputs[2].Errors, want) != 1 {
			t.Errorf("errors %q lack %q", a.inputs[2].Errors, want)
		}
	}
}

func TestRevisionWithNewIDsAndTheOldOnesIsAcceptedAtOnce(t *testing.T) {
	p := notesProposal(t)
	p.Goals = append(p.Goals, pslices.Goal{ID: "G-3", Text: "Empty the trash"})
	p.Terms = append(p.Terms, pslices.Term{ID: "T-2", Name: "Trash", Definition: "the notes that were removed"})
	p.Rules = append(p.Rules, pslices.Rule{ID: "R-3", Goal: "G-3", Pattern: "ubiquitous", System: "the notes service",
		Response: "shall empty the trash", Terms: []string{"T-2"}})
	p.Examples, p.Contract = keptNewExamples, keptNewContract
	s, a, sl := keptService(t, p)
	got, err := answerQ1(s, sl.ID)
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if a.calls != 2 {
		t.Fatalf("analyst called %d times, want 2 (no retry)", a.calls)
	}
	if h := got.History[len(got.History)-1]; attemptsOfEntry(t, h) != 1 {
		t.Errorf("revised attempts = %d, want 1", attemptsOfEntry(t, h))
	}
	assertAllChecksPass(t, got)
}

func TestRevisionLosingAnIDOnEveryAttemptFailsAndLeavesTheSliceUnchanged(t *testing.T) {
	bad := notesProposal(t)
	bad.Rules = bad.Rules[:1]
	bad.Examples = keptExamplesR1Only
	s, a, sl := keptService(t, bad)
	before, err := s.Get(sl.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = answerQ1(s, sl.ID)
	var e *pslices.Error
	if !errors.As(err, &e) || e.Code != "OperationFailed" {
		t.Fatalf("error %v, want OperationFailed", err)
	}
	if a.calls != 4 {
		t.Errorf("analyst called %d times, want 4 (proposal and 3 revision attempts)", a.calls)
	}
	if !strings.Contains(e.Detail, dropFault("R-2")) {
		t.Errorf("detail %q does not name the fault for R-2", e.Detail)
	}
	for i, in := range a.inputs[2:] {
		if in.Previous == nil || countOf(in.Errors, dropFault("R-2")) != 1 {
			t.Errorf("attempt %d input: Previous %v, errors %q", i+2, in.Previous, in.Errors)
		}
	}
	after, err := s.Get(sl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("the slice changed: parts %v/%v, approvals %v/%v, questions %v/%v, history %v/%v",
			before.Parts, after.Parts, before.Approvals, after.Approvals, before.Questions, after.Questions, before.History, after.History)
	}
	for _, q := range after.Questions {
		if q.Answer != nil || !q.AnsweredAt.IsZero() {
			t.Errorf("question %s holds an answer: %+v", q.ID, q)
		}
	}
}

func TestIDFaultsNeverHoldTheAnswersText(t *testing.T) {
	bad := notesProposal(t)
	bad.Goals = bad.Goals[:1]
	bad.Rules[1].Goal = "G-1"
	bad.Examples = keptSwapped
	bad.Contract = keptContractKeepOnly
	s, a, sl := keptService(t, bad)
	_, err := answerQ1(s, sl.ID)
	var e *pslices.Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v, want *Error", err)
	}
	if strings.Contains(e.Detail, secretAnswer) {
		t.Errorf("detail %q holds the answer's text", e.Detail)
	}
	for _, in := range a.inputs[2:] {
		var n int
		for _, f := range in.Errors {
			if idFault(f) {
				n++
				if strings.Contains(f, secretAnswer) {
					t.Errorf("fault %q holds the answer's text", f)
				}
			}
		}
		if n < 4 {
			t.Errorf("errors %q hold %d ID faults, want at least 4", in.Errors, n)
		}
	}
}

func TestExistingFaultsComeBeforeIDFaults(t *testing.T) {
	bad := untaggedR2(t, "List") // R-2 has no scenario tagged for it: a link finding
	bad.Goals = bad.Goals[:1]    // and G-2 is lost
	s, a, sl := keptService(t, bad, notesProposal(t))
	if _, err := answerQ1(s, sl.ID); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	errs := a.inputs[2].Errors
	first := slices.IndexFunc(errs, idFault)
	if first < 0 || countOf(errs, dropFault("G-2")) != 1 {
		t.Fatalf("errors %q lack the fault for G-2", errs)
	}
	if first == 0 {
		t.Fatalf("errors %q hold no existing fault before the ID faults", errs)
	}
	for _, f := range errs[first:] {
		if !idFault(f) {
			t.Errorf("errors %q: existing fault %q follows an ID fault", errs, f)
		}
	}
}

func TestCurrentExamplesThatDoNotParseAreNotChecked(t *testing.T) {
	s, a, sl := keptService(t, notesProposal(t))
	broken := "Feature: Notes\n  Rule R-1 @E-7 not valid gherkin\n  @E-8 Scenario Outline Examples ||\n"
	if _, err := s.ReplaceRules(context.Background(), sl.ID, pslices.RulesPart{Rules: sl.Rules, Terms: sl.Terms, Examples: broken}); err != nil {
		t.Fatal(err)
	}
	if _, err := answerQ1(s, sl.ID); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if a.calls != 2 {
		t.Errorf("analyst called %d times, want 2 (nothing to check against)", a.calls)
	}
}

func TestCurrentContractThatDoesNotParseIsNotChecked(t *testing.T) {
	s, a, sl := keptService(t, notesProposal(t))
	broken := "openapi: 3.1.0\ninfo: [unclosed\n  operationId: gone\n"
	if _, err := s.ReplaceContract(context.Background(), sl.ID, broken); err != nil {
		t.Fatal(err)
	}
	if _, err := answerQ1(s, sl.ID); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if a.calls != 2 {
		t.Errorf("analyst called %d times, want 2 (nothing to check against)", a.calls)
	}
}

func TestKeptRuleWhoseGoalWasRemovedMayServeAnyGoal(t *testing.T) {
	s, a, sl := keptService(t)
	rev := notesProposal(t)
	rev.Goals = rev.Goals[:1]
	rev.Rules[1].Goal = "G-1"
	a.results = append(a.results, analystResult{p: rev})
	if _, err := s.ReplaceGoals(context.Background(), sl.ID, sl.Goals[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err := answerQ1(s, sl.ID); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if a.calls != 2 {
		t.Errorf("analyst called %d times, want 2: R-2's goal G-2 is not in the current proposal, so R-2 may serve G-1", a.calls)
	}
}

func TestCreateIsNotCheckedAgainstAnyProposal(t *testing.T) {
	p := notesProposal(t)
	p.Goals = p.Goals[:1]
	p.Rules = p.Rules[:1]
	p.Examples = keptExamplesR1Only
	p.Contract = keptContractKeepOnly
	s, a := retryService(t, apiPattern(t), analystResult{p: p})
	r, err := s.Create(context.Background(), "Let me keep notes.")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.calls != 1 || len(a.inputs[0].Errors) != 0 {
		t.Errorf("analyst called %d times with errors %q, want once and none", a.calls, a.inputs[0].Errors)
	}
	if attemptsOfEntry(t, r.Slice.History[0]) != 1 {
		t.Errorf("attempts = %d, want 1", attemptsOfEntry(t, r.Slice.History[0]))
	}
}
