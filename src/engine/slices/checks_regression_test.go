package slices_test

// Regression: replacing the goals or the rules (contract unchanged) dropped the ContractLint
// result the slice had since its proposal. A slice carries the latest result of every check.

import (
	"context"
	"testing"

	"github.com/agentic-platform/engine/slices"
)

func TestUnchangedContractKeepsItsLintAndCompatibilityResults(t *testing.T) {
	ctx := context.Background()
	for name, change := range map[string]func(s *slices.Service, sl slices.Slice) (slices.Slice, error){
		"goals": func(s *slices.Service, sl slices.Slice) (slices.Slice, error) {
			g := append([]slices.Goal(nil), sl.Goals...)
			g[0].Text += " and keep it"
			return s.ReplaceGoals(ctx, sl.ID, g)
		},
		"rules": func(s *slices.Service, sl slices.Slice) (slices.Slice, error) {
			r := append([]slices.Rule(nil), sl.Rules...)
			r[0].Response += " at once"
			return s.ReplaceRules(ctx, sl.ID, slices.RulesPart{Rules: r, Examples: sl.Examples, Terms: sl.Terms})
		},
	} {
		t.Run(name, func(t *testing.T) {
			l := &fakeLint{}
			s := lintService(t, l)
			sl := create(t, s, "Let me keep notes.")
			before := checksNamed(sl, slices.CheckContractLint)
			compat := checksNamed(sl, slices.CheckContractCompatibility)
			if len(before) != 1 {
				t.Fatalf("the proposal must carry one ContractLint result: %+v", sl.Checks)
			}
			got, err := change(s, sl)
			if err != nil {
				t.Fatal(err)
			}
			if got.Parts.Contract != sl.Parts.Contract {
				t.Fatalf("the contract must be unchanged")
			}
			if after := checksNamed(got, slices.CheckContractLint); len(after) != 1 || after[0] != before[0] {
				t.Errorf("ContractLint after the change %+v, want %+v", after, before)
			}
			if after := checksNamed(got, slices.CheckContractCompatibility); len(after) != len(compat) {
				t.Errorf("ContractCompatibility after the change %+v, want %+v", after, compat)
			}
			if len(l.contracts) != 1 {
				t.Errorf("lint ran %d times, want once: an unchanged contract is not linted again", len(l.contracts))
			}
			stored, err := s.Get(sl.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(checksNamed(stored, slices.CheckContractLint)) != 1 {
				t.Errorf("the stored slice lost its ContractLint result: %+v", stored.Checks)
			}
		})
	}
}
