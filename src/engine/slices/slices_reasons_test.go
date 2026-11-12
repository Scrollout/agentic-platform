// Tests for T037a: schema failure reasons name the part and the JSON pointer of the fault and
// carry no local file path. slice.CheckSlice errors carry absolute file:// schema URLs; none
// may reach a reason, a Check or an Error (the engine is a service: a path is a leak).
//
// No new API: Check.Part names the part, Check.Reason holds the JSON pointer ("/rules/0/pattern")
// within the proposal document and says what is wrong in words, never a URL.
package slices_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/slices"
)

func assertNoPaths(t *testing.T, what, text string) {
	t.Helper()
	wd, _ := os.Getwd()
	abs, _ := filepath.Abs("..")
	for _, bad := range []string{"file://", "file:/", wd, abs, "/home/", "/tmp/", "openapi.yaml", ".schema.json", "slice-defs"} {
		if strings.Contains(text, bad) {
			t.Errorf("%s leaks %q: %q", what, bad, text)
		}
	}
}

func schemaFails(sl slices.Slice) []slices.Check {
	var out []slices.Check
	for _, c := range sl.Checks {
		if c.Check == slices.CheckSchema && !c.Passed {
			out = append(out, c)
		}
	}
	return out
}

func TestSchemaReasonsNamePartAndPointerWithoutPaths(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	sl := create(t, s, "Let me keep notes.")

	rules := append([]slices.Rule(nil), sl.Rules...)
	rules[0].Pattern = "bogus"
	gotR, err := s.ReplaceRules(ctx, sl.ID, slices.RulesPart{Rules: rules, Examples: sl.Examples, Terms: sl.Terms})
	if err != nil {
		t.Fatal(err)
	}
	goals := []slices.Goal{{ID: "X-1", Text: "bad id"}, sl.Goals[1]}
	// A fresh slice, so the rules fault above does not sit beside the goals one.
	sl2 := create(t, s, "Let me keep other notes.")
	gotG, err := s.ReplaceGoals(ctx, sl2.ID, goals)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		checks  []slices.Check
		part    string
		pointer string
	}{
		{"rule pattern", schemaFails(gotR), "rules", "/rules/0/pattern"},
		{"goal id", schemaFails(gotG), "goals", "/goals/0/id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if len(c.checks) == 0 {
				t.Fatal("want a failed Schema check")
			}
			for _, ch := range c.checks {
				assertNoPaths(t, "reason", ch.Reason)
				if ch.Part != c.part {
					t.Errorf("part %q, want %q", ch.Part, c.part)
				}
			}
			if !strings.Contains(c.checks[0].Reason, c.pointer) {
				t.Errorf("reason must hold the JSON pointer %s: %q", c.pointer, c.checks[0].Reason)
			}
			if !strings.Contains(c.checks[0].Reason, c.part) {
				t.Errorf("reason must name the part %q: %q", c.part, c.checks[0].Reason)
			}
		})
	}

	// The refusal that lists the failures, and its text, carry no path either.
	_, err = s.Approve(ctx, sl.ID, slices.PartRules, gotR.Parts.Rules)
	var e *slices.Error
	if code(t, err) != "PartFailsChecks" {
		t.Fatalf("want PartFailsChecks, got %v", err)
	}
	e = err.(*slices.Error)
	assertNoPaths(t, "Error text", e.Error())
	assertNoPaths(t, "Detail", e.Detail)
}

func TestMissingFieldReasonNamesPartPointerAndField(t *testing.T) {
	s, _, _ := setup(t)
	sl := create(t, s, "Let me keep notes.")
	rules := append([]slices.Rule(nil), sl.Rules...)
	rules[1].System = "" // required by the schema (minLength) or present but empty
	got, err := s.ReplaceRules(context.Background(), sl.ID, slices.RulesPart{Rules: rules, Examples: sl.Examples, Terms: sl.Terms})
	if err != nil {
		t.Fatal(err)
	}
	fails := schemaFails(got)
	if len(fails) == 0 || fails[0].Part != "rules" || !strings.Contains(fails[0].Reason, "/rules/1/system") {
		t.Fatalf("want a rules failure at /rules/1/system: %+v", fails)
	}
	assertNoPaths(t, "reason", fails[0].Reason)
}

func TestUnusableProposalFaultsCarryNoPaths(t *testing.T) {
	p := notesProposal(t)
	p.Rules[0].Pattern = "bogus"
	s := newService(t, &fakeAnalyst{results: []analystResult{{p: p}}}, &fakeDiff{})
	_, err := s.Create(context.Background(), "Let me keep notes.")
	if code(t, err) != "OperationFailed" {
		t.Fatalf("want OperationFailed, got %v", err)
	}
	e := err.(*slices.Error)
	if !strings.Contains(e.Detail, "/rules/0/pattern") {
		t.Errorf("detail must hold the pointer: %q", e.Detail)
	}
	assertNoPaths(t, "Detail", e.Detail)
	assertNoPaths(t, "Error text", e.Error())
}

func TestAnalystRetryErrorsCarryNoPaths(t *testing.T) {
	bad := notesProposal(t)
	bad.Rules[0].Pattern = "bogus"
	a := &fakeAnalyst{results: []analystResult{{p: bad}, {p: notesProposal(t)}}}
	s := newService(t, a, &fakeDiff{})
	if _, err := s.Create(context.Background(), "Let me keep notes."); err != nil && a.calls < 2 {
		t.Skipf("pattern allows one attempt: %v", err)
	}
	for _, in := range a.inputs {
		for _, e := range in.Errors {
			assertNoPaths(t, "analyst error", e)
		}
	}
}
