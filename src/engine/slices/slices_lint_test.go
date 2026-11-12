// Tests for the contract lint and the ExampleTagged check (task T037b). They fail until package
// slices has the API below.
//
// The lint is Spectral with OpenAPI's standard rules (not the engine's own ruleset), behind an
// interface like ContractDiff so the tests need no Node:
//
//	type LintFinding struct{ Level, Code, Text string } // Level "error", "warn" or "info"
//	type ContractLint interface{ Lint(ctx context.Context, contract string) ([]LintFinding, error) }
//	// Config gains `Lint ContractLint`; nil fails closed with a failed ContractLint check
//	// (slices_nolint_test.go).
//	const CheckContractLint = "ContractLint"
//
// With a Lint, every slice carries one ContractLint result on the contract part: passed when no
// finding has Level "error"; otherwise one failed result per error finding, whose Reason holds
// its Code and Text; a lint that cannot run (error) fails the check with the error in the Reason.
// Warnings and info do not fail it. Lint runs when the slice is created and when its contract
// changes. A failed lint makes Approve of the contract part give PartFailsChecks.
//
// ExampleTagged (spec data-model `examples`, FR-003): a check on the rules part, name
// CheckExampleTagged = "ExampleTagged". It fails for each scenario that lacks its @R-<n> or
// @E-<n> tag, whose @R-<n> differs from its `Rule: R-<n>` block, or whose @E-<n> repeats
// another scenario's; the Reason names the scenario. It passes (one passed result) otherwise.
package slices_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/slice"
)

type fakeLint struct {
	mu        sync.Mutex
	contracts []string
	findings  []slices.LintFinding
	err       error
}

func (l *fakeLint) Lint(_ context.Context, contract string) ([]slices.LintFinding, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.contracts = append(l.contracts, contract)
	return l.findings, l.err
}

func lintService(t *testing.T, l slices.ContractLint) *slices.Service {
	t.Helper()
	schemas, err := slice.LoadSchemas("../api/preview/2026-11-12-preview")
	if err != nil {
		t.Fatal(err)
	}
	pattern, err := os.ReadFile("../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := slices.New(slices.Config{Schemas: schemas, Pattern: pattern, Lint: l,
		Analyst: &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}, Diff: &fakeDiff{}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func checksNamed(s slices.Slice, name string) []slices.Check {
	var out []slices.Check
	for _, c := range s.Checks {
		if c.Check == name {
			out = append(out, c)
		}
	}
	return out
}

func TestLintPassesAndRunsOnTheSlicesContract(t *testing.T) {
	l := &fakeLint{findings: []slices.LintFinding{{Level: "warn", Code: "info-contact", Text: "no contact"}}}
	s := lintService(t, l)
	sl := create(t, s, "Let me keep notes.")
	cs := checksNamed(sl, slices.CheckContractLint)
	if len(cs) != 1 || !cs[0].Passed || cs[0].Part != "contract" {
		t.Fatalf("want one passed ContractLint on the contract (warnings do not fail): %+v", cs)
	}
	if len(l.contracts) != 1 || l.contracts[0] != sl.Contract {
		t.Errorf("lint must read the slice's contract: %q", l.contracts)
	}
	approveAll(t, s, sl)
}

func TestLintErrorsAreStoredAsFailedChecks(t *testing.T) {
	l := &fakeLint{findings: []slices.LintFinding{
		{Level: "error", Code: "operation-operationId", Text: "operation must have operationId"},
		{Level: "error", Code: "info-description", Text: "info needs a description"},
		{Level: "warn", Code: "tag-description", Text: "tag needs description"}}}
	s := lintService(t, l)
	sl := create(t, s, "Let me keep notes.")
	var fails []slices.Check
	for _, c := range checksNamed(sl, slices.CheckContractLint) {
		if !c.Passed {
			fails = append(fails, c)
		}
	}
	if len(fails) != 2 || fails[0].Part != "contract" {
		t.Fatalf("want one failed check per error finding on the contract: %+v", fails)
	}
	if !strings.Contains(fails[0].Reason, "operation-operationId") || !strings.Contains(fails[0].Reason, "operation must have operationId") {
		t.Errorf("reason must name the rule and its text: %q", fails[0].Reason)
	}
	if strings.Contains(fails[0].Reason+fails[1].Reason, "tag-description") {
		t.Errorf("warnings are not failures")
	}
	_, err := s.Approve(context.Background(), sl.ID, slices.PartContract, sl.Parts.Contract)
	if code(t, err) != "PartFailsChecks" {
		t.Errorf("the contract cannot be approved: %v", err)
	}
	// The other parts are unaffected.
	if _, err := s.Approve(context.Background(), sl.ID, slices.PartGoals, sl.Parts.Goals); err != nil {
		t.Errorf("goals: %v", err)
	}
}

func TestLintThatCannotRunFailsTheCheck(t *testing.T) {
	l := &fakeLint{err: errors.New("spectral: command not found")}
	s := lintService(t, l)
	sl := create(t, s, "Let me keep notes.")
	cs := failedChecks(sl, "contract")
	var got []slices.Check
	for _, c := range cs {
		if c.Check == slices.CheckContractLint {
			got = append(got, c)
		}
	}
	if len(got) != 1 || strings.Contains(got[0].Reason, "spectral: command not found") || !strings.Contains(got[0].Reason, "could not run") {
		t.Fatalf("a lint that cannot run fails closed, saying only that it could not run, with no runner text: %+v", got)
	}
}

func TestLintRunsAgainWhenTheContractChanges(t *testing.T) {
	l := &fakeLint{}
	s := lintService(t, l)
	sl := create(t, s, "Let me keep notes.")
	l.mu.Lock()
	l.findings = []slices.LintFinding{{Level: "error", Code: "x", Text: "bad"}}
	l.mu.Unlock()
	changed := sl.Contract + "# edited\n"
	got, err := s.ReplaceContract(context.Background(), sl.ID, changed)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.contracts) != 2 || l.contracts[1] != changed {
		t.Errorf("lint must run on the changed contract: %d calls", len(l.contracts))
	}
	if len(failedChecks(got, "contract")) == 0 {
		t.Errorf("the new finding must fail the check")
	}
}

// ---- ExampleTagged ----

const taggedOK = "Feature: Notes\n  Rule: R-1\n    @R-1 @E-1\n    Scenario: Keep\n      Given a note\n  Rule: R-2\n    @R-2 @E-2\n    Scenario: List\n      Given notes\n"

func exampleCheck(t *testing.T, examples string) []slices.Check {
	t.Helper()
	s, _, _ := setup(t)
	sl := create(t, s, "Let me keep notes.")
	got, err := s.ReplaceRules(context.Background(), sl.ID, slices.RulesPart{Rules: sl.Rules, Examples: examples, Terms: sl.Terms})
	if err != nil {
		t.Fatal(err)
	}
	return checksNamed(got, slices.CheckExampleTagged)
}

func TestExampleTaggedPasses(t *testing.T) {
	cs := exampleCheck(t, taggedOK)
	if len(cs) != 1 || !cs[0].Passed || cs[0].Part != "rules" {
		t.Errorf("want one passed ExampleTagged on rules: %+v", cs)
	}
	s, _, _ := setup(t)
	if got := checksNamed(create(t, s, "x"), slices.CheckExampleTagged); len(got) != 1 || !got[0].Passed {
		t.Errorf("the notes fixture is tagged correctly: %+v", got)
	}
}

func TestExampleTaggedFailures(t *testing.T) {
	cases := []struct{ name, examples, scenario string }{
		{"no rule tag", strings.Replace(taggedOK, "@R-1 @E-1", "@E-1", 1), "Keep"},
		{"no example tag", strings.Replace(taggedOK, "@R-2 @E-2", "@R-2", 1), "List"},
		{"no tags", strings.Replace(taggedOK, "@R-1 @E-1\n", "", 1), "Keep"},
		{"wrong rule", strings.Replace(taggedOK, "@R-2 @E-2", "@R-1 @E-2", 1), "List"},
		{"repeated example id", strings.Replace(taggedOK, "@R-2 @E-2", "@R-2 @E-1", 1), "List"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var fails []slices.Check
			for _, ch := range exampleCheck(t, c.examples) {
				if !ch.Passed {
					fails = append(fails, ch)
				}
			}
			if len(fails) == 0 || fails[0].Part != "rules" {
				t.Fatalf("want a failed ExampleTagged on rules: %+v", fails)
			}
			if !strings.Contains(fails[0].Reason, c.scenario) {
				t.Errorf("reason must name the scenario %q: %q", c.scenario, fails[0].Reason)
			}
		})
	}
}

func TestExampleTaggedFailureBlocksApprovalOfRules(t *testing.T) {
	s, _, _ := setup(t)
	sl := create(t, s, "Let me keep notes.")
	got, err := s.ReplaceRules(context.Background(), sl.ID,
		slices.RulesPart{Rules: sl.Rules, Examples: strings.Replace(taggedOK, "@R-1 @E-1", "@E-1", 1), Terms: sl.Terms})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(context.Background(), sl.ID, slices.PartRules, got.Parts.Rules); code(t, err) != "PartFailsChecks" {
		t.Errorf("want PartFailsChecks, got %v", err)
	}
}
