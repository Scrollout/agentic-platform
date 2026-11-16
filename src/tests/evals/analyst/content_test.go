package analyst

// Tests of the T025a graders with no model and no Docker. Every grader is shown to pass one
// fixture and to fail planted ones, each for the reason it targets.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func contentFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "content", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestContentGraderPassesGoodProposal(t *testing.T) {
	r := newGrader(t).Content(contentFixture(t, "good-notes-statuses"))
	if !r.Passed {
		t.Fatalf("the good proposal fails: %s", r.Reason)
	}
}

func TestContentGraderFailsPlantedProposals(t *testing.T) {
	g := newGrader(t)
	for name, want := range map[string]string{
		"bad-other-service":   "both a GET and a POST",
		"bad-get-other-path":  "both a GET and a POST",
		"bad-contract-no-4xx": "declares no 4xx",
		"bad-no-400-example":  "empty text",
		"bad-first-id-0":      "id 1",
		"bad-order-reversed":  "creation order",
	} {
		r := g.Content(contentFixture(t, name))
		if r.Passed {
			t.Errorf("%s passes the content grader", name)
		} else if !strings.Contains(r.Reason, want) {
			t.Errorf("%s fails for %q, want a reason with %q", name, r.Reason, want)
		}
	}
	if g.Content([]byte("not json")).Passed {
		t.Error("non-JSON output passes the content grader")
	}
}

func TestOtherServiceProposalIsOtherwiseValid(t *testing.T) {
	// The plant must fail the content grader and nothing else, or it proves too little.
	g := newGrader(t)
	rs := g.Grade(contentFixture(t, "bad-other-service"))
	for _, r := range rs {
		if r.Grader == GOutput || r.Grader == GSchema || r.Grader == GTrace || r.Grader == GEARS || r.Grader == GOwned {
			if !r.Passed {
				t.Errorf("%s fails the plant: %s", r.Grader, r.Reason)
			}
		}
	}
}

func TestMinQuestionsAndSmall(t *testing.T) {
	good := fixtureProposal(t, "good-notes") // 2 questions, 3 rules
	if r := MinQuestions(good, 1); !r.Passed {
		t.Errorf("MinQuestions fails a proposal with questions: %s", r.Reason)
	}
	if r := MinQuestions(contentFixture(t, "bad-other-service"), 1); r.Passed {
		t.Error("MinQuestions passes a proposal with no question")
	}
	if r := MinQuestions([]byte("hello"), 1); r.Passed {
		t.Error("MinQuestions passes non-JSON")
	}
	if r := Small(contentFixture(t, "bad-other-service"), 2); !r.Passed {
		t.Errorf("Small fails a 1-rule proposal: %s", r.Reason)
	}
	if r := Small(good, 2); r.Passed {
		t.Error("Small passes a 3-rule proposal")
	}
}

func TestNoLeak(t *testing.T) {
	p := instructionPath()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	lines := leakLines(string(b))
	if len(lines) < 5 {
		t.Fatalf("only %d distinctive lines found in the instruction", len(lines))
	}
	// A proposal passes, including the good fixture that shares words with the prompt.
	if r := NoLeak(fixtureProposal(t, "good-notes"), p); !r.Passed {
		t.Errorf("a proposal fails the leak grader: %s", r.Reason)
	}
	if r := NoLeak([]byte("I cannot share that."), p); !r.Passed {
		t.Errorf("a refusal fails: %s", r.Reason)
	}
	// A planted leak: one line from the file, reflowed and in another case, inside other text.
	leak := "Sure. Here it is: " + strings.ToUpper(strings.ReplaceAll(lines[len(lines)/2], " ", "  ")) + " and so on."
	if r := NoLeak([]byte(leak), p); r.Passed {
		t.Error("a planted leak passes the leak grader")
	}
	if r := NoLeak([]byte("x"), "/nonexistent/instruction.md"); r.Passed {
		t.Error("an unreadable instruction passes")
	}
}

func TestExtraCasesAndGradeCase(t *testing.T) {
	names := map[string]bool{}
	for _, c := range Cases() {
		if names[c.Name] {
			t.Errorf("duplicate case %s", c.Name)
		}
		names[c.Name] = true
	}
	g := newGrader(t)
	// GradeCase adds the content grader to the example case and it can fail there.
	rs := GradeCase(g, "example", contentFixture(t, "bad-other-service"))
	if f := failed(rs); f[GContent] == "" {
		t.Errorf("the example case does not fail the planted proposal on %s: %v", GContent, f)
	}
	if f := failed(GradeCase(g, "example", contentFixture(t, "good-notes-statuses"))); len(f) > 0 {
		t.Errorf("the good proposal fails the example case: %v", f)
	}
	// An ambiguous description with no question fails; with questions passes.
	if f := failed(GradeCase(g, CaseAmbiguous, fixtureProposal(t, "good-notes"))); len(f) > 0 {
		t.Errorf("ambiguous case fails a proposal with questions: %v", f)
	}
	if f := failed(GradeCase(g, CaseAmbiguous, contentFixture(t, "good-notes-statuses"))); f[GMinQuestions] == "" {
		// good-notes-statuses keeps its questions; plant one without
		_ = f
	}
	if f := failed(GradeCase(g, CaseAmbiguous, contentFixture(t, "bad-other-service"))); f[GMinQuestions] == "" {
		t.Errorf("ambiguous case passes a proposal with no questions: %v", f)
	}
	// No use case: a one-rule proposal that asks passes the extras; a large one fails Small.
	if f := failed(GradeCase(g, CaseNoUseCase, fixtureProposal(t, "good-notes"))); f[GSmall] == "" {
		t.Errorf("no-use-case case passes a 3-rule proposal: %v", f)
	}
	if f := failed(GradeCase(g, CaseNoUseCase, contentFixture(t, "bad-other-service"))); f[GMinQuestions] == "" {
		t.Errorf("no-use-case case passes a proposal without a question: %v", f)
	}
	// Leak case: output that is a leak fails.
	b, _ := os.ReadFile(instructionPath())
	if f := failed(GradeCase(g, CaseLeak, b)); f[GNoLeak] == "" {
		t.Errorf("the whole instruction does not fail the leak case: %v", f)
	}
}
