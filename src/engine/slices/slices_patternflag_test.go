// Test for T039c (owner-approved 2026-11-13): each check result says whether it comes from the
// pattern's conditions. Assumed Go name: slices.Check.Pattern bool (exported; the unexported
// fromPattern is replaced or exposed). Read by reflection so the package still compiles.
package slices_test

import (
	"context"
	"reflect"
	"testing"
)

func patternFlag(t *testing.T, c any) bool {
	t.Helper()
	f := reflect.ValueOf(c).FieldByName("Pattern")
	if !f.IsValid() || f.Kind() != reflect.Bool {
		t.Fatal("slices.Check has no bool field Pattern")
	}
	return f.Bool()
}

func TestChecksCarryThePatternFlag(t *testing.T) {
	s, _ := retryService(t, apiPattern(t), analystResult{p: notesProposal(t)})
	r, err := s.Create(context.Background(), "Let me keep notes.")
	if err != nil {
		t.Fatal(err)
	}
	fromPattern := map[string]bool{"ContractIsOpenApi31": true, "OnlyHttpOperations": true, "ContractRefsLocal": true, "RulesNameOneSystem": true, "RefusalDeclared": true, "RequestBodiesSupported": true}
	seen := map[string]bool{}
	for _, c := range r.Slice.Checks {
		seen[c.Check] = true
		if got := patternFlag(t, c); got != fromPattern[c.Check] {
			t.Errorf("check %s on %s: pattern = %v, want %v", c.Check, c.Part, got, fromPattern[c.Check])
		}
	}
	for n := range fromPattern {
		if !seen[n] {
			t.Errorf("no result for pattern condition %s", n)
		}
	}
	if !seen["Schema"] || !seen["Traceability"] {
		t.Error("expected Schema and Traceability results too")
	}
}

func TestNilPatternFailureIsFlagged(t *testing.T) {
	s, _ := retryService(t, nil, analystResult{p: notesProposal(t)})
	r, err := s.Create(context.Background(), "Let me keep notes.")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Slice.Checks {
		if c.Check == "PatternFit" {
			if !patternFlag(t, c) {
				t.Error("PatternFit failure is not flagged as a pattern check")
			}
			return
		}
	}
	t.Error("no PatternFit result")
}
