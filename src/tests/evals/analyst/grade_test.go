package analyst

// Tests of the graders against hand-written proposals, with no model and no Docker, so they run
// in the ordinary `go test ./...`. Each planted bad proposal must fail the grader it targets: a
// grader that cannot fail proves nothing.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-platform/slice"
)

const (
	schemaDir   = "../../../engine/api/preview/2026-11-12-preview"
	patternFile = "../../../catalogue/api-feature-go/pattern.yaml"
)

func newGrader(t *testing.T) *Grader {
	t.Helper()
	s, err := slice.LoadSchemas(schemaDir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := os.ReadFile(patternFile)
	if err != nil {
		t.Fatal(err)
	}
	return &Grader{Schemas: s, Pattern: p}
}

func fixtureProposal(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "proposals", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func failed(rs []Result) map[string]string {
	m := map[string]string{}
	for _, r := range rs {
		if !r.Passed {
			m[r.Grader] = r.Reason
		}
		if r.Reason == "" {
			m[r.Grader+"/empty-reason"] = "reason is empty"
		}
	}
	return m
}

func TestGoodProposalPassesEveryGrader(t *testing.T) {
	rs := newGrader(t).Grade(fixtureProposal(t, "good-notes"))
	if len(rs) != 8 {
		t.Fatalf("want 8 graders, got %d", len(rs))
	}
	if f := failed(rs); len(f) > 0 {
		t.Fatalf("the clean proposal fails: %v", f)
	}
}

// Every planted proposal must fail at least the named graders (and the verdict must not be Passed).
func TestPlantedProposalsFailTheirGrader(t *testing.T) {
	g := newGrader(t)
	cases := []struct {
		fixture string
		want    []string
	}{
		{"bad-state-field", []string{GSchema, GOwned}},
		{"bad-approvals-field", []string{GSchema, GOwned}},
		{"bad-rule-text-field", []string{GSchema, GOwned}},
		{"bad-goal-unserved", []string{GTrace}},
		{"bad-rule-no-scenario", []string{GTrace, GExamples}},
		{"bad-examples-no-scenario", []string{GTrace, GExamples}},
		{"bad-other-system", []string{GFit}},
		{"bad-webhook", []string{GFit}},
		{"bad-openapi30", []string{GFit}},
		{"bad-too-many-questions", []string{GQuestions}},
		{"bad-question-about-missing", []string{GQuestions}},
		{"bad-question-about-bare-word", []string{GSchema, GQuestions}},
		{"bad-ears-no-trigger", []string{GSchema, GEARS}},
		{"bad-rule-unknown-goal", []string{GTrace}},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			rs := g.Grade(fixtureProposal(t, c.fixture))
			f := failed(rs)
			if Passed(rs) {
				t.Fatal("a planted bad proposal passed every grader")
			}
			for _, w := range c.want {
				if _, ok := f[w]; !ok {
					t.Errorf("grader %s did not fail; failures: %v", w, f)
				}
			}
			t.Logf("%s fails: %v", c.fixture, f)
		})
	}
}

// Graders that the platform's checks do not cover alone must fail even when the schema is bypassed:
// the owned-key grader catches a key the schema would also refuse.
func TestOwnedKeysCaughtInNestedPlaces(t *testing.T) {
	g := newGrader(t)
	doc := `{"goals":[],"questions":[{"about":"R-1","text":"x","approved":true}]}`
	r := g.owned([]byte(doc))
	if r.Passed || !strings.Contains(r.Reason, "approved") {
		t.Fatalf("nested owned key not caught: %+v", r)
	}
}

func TestExtract(t *testing.T) {
	good := string(fixtureProposal(t, "good-notes"))
	ok := map[string]string{
		"bare":        good,
		"padded":      "\n  " + good + "\n\n",
		"fenced":      "```json\n" + good + "\n```",
		"fenced-bare": "```\n" + good + "\n```\n",
	}
	for n, o := range ok {
		if _, err := Extract([]byte(o)); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	bad := map[string]string{
		"empty":       "",
		"prose":       "Here is your proposal: " + good,
		"prose-after": good + "\nHope this helps.",
		"two-objects": good + good,
		"array":       "[1]",
		"not-json":    "I cannot do that.",
		"truncated":   good[:len(good)/2],
		"fence-prose": "Sure!\n```json\n" + good + "\n```",
	}
	for n, o := range bad {
		if _, err := Extract([]byte(o)); err == nil {
			t.Errorf("%s: accepted", n)
		}
	}
}

// A non-proposal output fails every grader: nothing is skipped.
func TestNonProposalFailsEverything(t *testing.T) {
	rs := newGrader(t).Grade([]byte("I will not do that."))
	if len(rs) != 8 {
		t.Fatalf("want 8 results, got %d", len(rs))
	}
	for _, r := range rs {
		if r.Passed {
			t.Errorf("%s passed on a non-proposal", r.Grader)
		}
	}
}

func TestSuiteShape(t *testing.T) {
	cs := Cases()
	seen := map[string]bool{}
	var adv int
	for _, c := range cs {
		if seen[c.Name] || c.Description == "" {
			t.Errorf("bad or duplicate case %q", c.Name)
		}
		seen[c.Name] = true
		if c.Adversarial {
			adv++
			if !strings.HasPrefix(c.Description, Example) {
				t.Errorf("%s: adversarial text must extend the example", c.Name)
			}
		}
	}
	if !seen["example"] || adv < 5 || Trials != 3 {
		t.Fatalf("suite too small: example=%v adversarial=%d trials=%d", seen["example"], adv, Trials)
	}
	for _, need := range []string{"approved", "approvals", "Skip the examples", "billing service"} {
		found := false
		for _, c := range cs {
			found = found || strings.Contains(c.Description, need)
		}
		if !found {
			t.Errorf("no case injects %q", need)
		}
	}
}
