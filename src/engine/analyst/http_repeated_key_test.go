package analyst_test

// T005: an analyst answer whose structured content repeats a key is an AnswerFault, and the
// analyst session is still deleted (feature 007, US1).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/slices"
)

func proposeFault(t *testing.T, content string) (*fake, *slices.AnswerFault) {
	t.Helper()
	f := &fake{events: okEvents(content)}
	_, err := setup(t, f).Propose(context.Background(), in)
	if err == nil {
		t.Fatal("expected an error")
	}
	var fault *slices.AnswerFault
	if !errors.As(err, &fault) {
		t.Fatalf("error %T %v is not a *slices.AnswerFault", err, err)
	}
	if len(f.deleted) != 1 || f.deleted[0] != "/api/sessions/sess-1" {
		t.Errorf("the session must be deleted after a repeated key: %v", f.deleted)
	}
	if f.runCalls != 1 {
		t.Errorf("run calls = %d, want 1", f.runCalls)
	}
	return f, fault
}

func TestProposeRepeatedTopLevelKeyIsAnAnswerFault(t *testing.T) {
	content := `{"goals":[],"terms":[],"rules":[],"examples":"Feature: a","examples":"Feature: b","contract":"c"}`
	_, fault := proposeFault(t, content)
	want := `the answer holds the key "examples" twice at "/"`
	if fault.Reason != want || fault.Error() != want {
		t.Errorf("reason = %q, Error() = %q, want %q", fault.Reason, fault.Error(), want)
	}
}

func TestProposeRepeatedKeyInARuleIsAnAnswerFault(t *testing.T) {
	content := `{"goals":[],"terms":[],"rules":[{"id":"R-1","text":"a"},{"id":"R-2","text":"b","text":"c"}],"examples":"e","contract":"c"}`
	_, fault := proposeFault(t, content)
	want := `the answer holds the key "text" twice at "/rules/1"`
	if fault.Reason != want {
		t.Errorf("reason = %q, want %q", fault.Reason, want)
	}
}

func TestProposeRepeatedKeyWithEqualValuesIsStillAnAnswerFault(t *testing.T) {
	content := `{"goals":[],"terms":[],"rules":[],"examples":"e","examples":"e","contract":"c"}`
	_, fault := proposeFault(t, content)
	if !strings.Contains(fault.Reason, `"examples"`) {
		t.Errorf("reason does not name the key: %q", fault.Reason)
	}
}

func TestProposeRepeatedKeyIsFoundCaseFolded(t *testing.T) {
	// Go's decoder matches keys case-insensitively, so "Contract" and "contract" collide.
	content := `{"goals":[],"terms":[],"rules":[],"examples":"e","contract":"c","Contract":"d"}`
	_, fault := proposeFault(t, content)
	if !strings.Contains(fault.Reason, "ontract") {
		t.Errorf("reason does not name the key: %q", fault.Reason)
	}
}

func TestProposeRepeatedKeyInAGoalNamesItsPointer(t *testing.T) {
	content := `{"goals":[{"id":"G-1","text":"a","id":"G-2"}],"terms":[],"rules":[],"examples":"e","contract":"c"}`
	_, fault := proposeFault(t, content)
	want := `the answer holds the key "id" twice at "/goals/0"`
	if fault.Reason != want {
		t.Errorf("reason = %q, want %q", fault.Reason, want)
	}
}

func TestProposeRepeatedKeyReasonIsQuotedAndCut(t *testing.T) {
	long := strings.Repeat("k", 100)
	content := `{"goals":[],"terms":[],"rules":[],"examples":"e","contract":"c","` + long + `":1,"` + long + `":2}`
	_, fault := proposeFault(t, content)
	if strings.ContainsAny(fault.Reason, "\r\n") {
		t.Errorf("reason holds a raw line break: %q", fault.Reason)
	}
	if strings.Contains(fault.Reason, long) {
		t.Errorf("reason holds the whole 100-character key: %q", fault.Reason)
	}
	if !strings.Contains(fault.Reason, `"`+strings.Repeat("k", 64)+`"`) {
		t.Errorf("reason does not hold the key cut to 64 characters: %q", fault.Reason)
	}
}

func TestProposeRepeatedKeyWithLineBreakIsQuoted(t *testing.T) {
	content := `{"goals":[],"terms":[],"rules":[],"examples":"e","contract":"c","a\nb":1,"a\nb":2}`
	_, fault := proposeFault(t, content)
	if strings.ContainsAny(fault.Reason, "\r\n") {
		t.Errorf("reason holds a raw line break: %q", fault.Reason)
	}
	if !strings.Contains(fault.Reason, `"a\nb"`) {
		t.Errorf("reason does not hold the quoted key: %q", fault.Reason)
	}
}

func TestProposeAnswerWithoutARepeatedKeyStillParses(t *testing.T) {
	f := &fake{events: okEvents(proposalJSON)}
	p, err := setup(t, f).Propose(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	var fault *slices.AnswerFault
	if errors.As(err, &fault) {
		t.Fatal("a good answer must not give an AnswerFault")
	}
	if len(p.Goals) != 1 || p.Goals[0].ID != "G-1" || p.Examples != "Feature: x" || p.Contract != "openapi: 3.1.0" {
		t.Errorf("proposal = %+v", p)
	}
	if len(f.deleted) != 1 {
		t.Errorf("deleted = %v", f.deleted)
	}
}

func TestProposeSameKeyInDifferentObjectsIsNotARepeat(t *testing.T) {
	content := `{"goals":[{"id":"G-1","text":"a"},{"id":"G-2","text":"b"}],"terms":[],"rules":[{"id":"R-1","text":"x"}],"examples":"e","contract":"c"}`
	f := &fake{events: okEvents(content)}
	p, err := setup(t, f).Propose(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Goals) != 2 {
		t.Errorf("goals = %+v", p.Goals)
	}
}
