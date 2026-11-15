package checks_test

// T104 (converge Story 2, round 1; data-model "Refs"): a failing AllTestsPass or
// EveryExampleBound names, in CheckResult.Refs, the R-<n>/E-<n> of each scenario of
// features/slice.feature whose godog subtest fails, once each, in the file's order; a scenario
// without both tags names none; a passing check names none. Written before the code by a
// separate test author. Fixtures: testdata/refs/{tagged,undefined,passing}.

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/agentic-platform/engine/checks"
)

func refsDir(kind string) string { return filepath.Join("testdata", "refs", kind) }

func refsRunner(t *testing.T) checks.GoRunner {
	r := needGo(t)
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	return r
}

func refsOf(rs []checks.CheckResult) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Refs...)
	}
	return out
}

// Two tagged scenarios, one failing (R-1/E-2); an Outline whose second row fails (R-2/E-3); an
// untagged failing scenario names none.
func TestFailingScenariosNameTheirExamples(t *testing.T) {
	rs := checks.AllTestsPass(refsRunner(t), refsDir("tagged"))
	if allPassed(rs) {
		t.Fatalf("setup: AllTestsPass passed a project with failing scenarios: %+v", rs)
	}
	if got, want := refsOf(rs), []string{"R-1/E-2", "R-2/E-3"}; !slices.Equal(got, want) {
		t.Errorf("refs = %v, want %v (the passing scenario and the untagged one name nothing; the Outline once)", got, want)
	}
}

func TestUnboundStepsNameTheirExamples(t *testing.T) {
	rs := checks.GodogStrict(refsRunner(t), refsDir("undefined"))
	if allPassed(rs) {
		t.Fatalf("setup: EveryExampleBound passed an undefined step: %+v", rs)
	}
	if got, want := refsOf(rs), []string{"R-2/E-2"}; !slices.Equal(got, want) {
		t.Errorf("refs = %v, want %v", got, want)
	}
}

func TestPassingChecksNameNoRefs(t *testing.T) {
	r := refsRunner(t)
	for name, rs := range map[string][]checks.CheckResult{
		"AllTestsPass":      checks.AllTestsPass(r, refsDir("passing")),
		"EveryExampleBound": checks.GodogStrict(r, refsDir("passing")),
		// EveryExampleBound passes on scenarios that ran and failed: it names none either.
		"EveryExampleBound, scenarios failing": checks.GodogStrict(r, refsDir("tagged")),
	} {
		if !allPassed(rs) {
			t.Errorf("%s: setup: did not pass: %+v", name, rs)
		}
		if got := refsOf(rs); len(got) != 0 {
			t.Errorf("%s: a passing check names refs %v", name, got)
		}
	}
}

// Owner, 2026-11-16: a failing EveryExampleBound names only the scenarios with an undefined,
// pending or ambiguous step; AllTestsPass names every failing scenario.
func TestUnboundScenariosAloneAreNamedByEveryExampleBound(t *testing.T) {
	r := refsRunner(t)
	for _, kind := range []string{"undef", "pending", "ambiguous"} {
		t.Run(kind, func(t *testing.T) {
			rs := checks.GodogStrict(r, refsDir("mixed-"+kind))
			if allPassed(rs) {
				t.Fatalf("setup: EveryExampleBound passed a %s step: %+v", kind, rs)
			}
			if got, want := refsOf(rs), []string{"R-1/E-1"}; !slices.Equal(got, want) {
				t.Errorf("refs = %v, want %v (the scenario that fails against the stub is not named)", got, want)
			}
			// AllTestsPass, over the same project, names the scenario failing against the stub.
			if got := refsOf(checks.AllTestsPass(r, refsDir("mixed-"+kind))); !slices.Contains(got, "R-2/E-2") {
				t.Errorf("AllTestsPass refs = %v, want R-2/E-2 among them", got)
			}
		})
	}
}

// An Outline whose name holds a placeholder: godog's later rows get no #nn suffix and no header.
func TestAnOutlineWithAPlaceholderNameIsNamedOnce(t *testing.T) {
	rs := checks.AllTestsPass(refsRunner(t), refsDir("outline-placeholder"))
	if allPassed(rs) {
		t.Fatalf("setup: AllTestsPass passed a failing row: %+v", rs)
	}
	if got, want := refsOf(rs), []string{"R-1/E-1"}; !slices.Equal(got, want) {
		t.Errorf("refs = %v, want %v", got, want)
	}
}

func TestAnOutlineWithTwoFailingRowsIsNamedOnce(t *testing.T) {
	rs := checks.AllTestsPass(refsRunner(t), refsDir("outline-two-failing"))
	if allPassed(rs) {
		t.Fatalf("setup: AllTestsPass passed failing rows: %+v", rs)
	}
	if got, want := refsOf(rs), []string{"R-1/E-1"}; !slices.Equal(got, want) {
		t.Errorf("refs = %v, want %v", got, want)
	}
}

func TestAFailingScenarioWithoutAnExampleTagNamesNone(t *testing.T) {
	rs := checks.AllTestsPass(refsRunner(t), refsDir("no-example-tag"))
	if allPassed(rs) {
		t.Fatalf("setup: AllTestsPass passed a failing scenario: %+v", rs)
	}
	if got := refsOf(rs); len(got) != 0 {
		t.Errorf("refs = %v, want none (the scenario lacks @E-<n>)", got)
	}
}

// Two scenarios of one name, the second failing: godog's subtests are greet and greet#01.
func TestTwoScenariosOfOneNameNameTheFailingOne(t *testing.T) {
	rs := checks.AllTestsPass(refsRunner(t), refsDir("same-name"))
	if allPassed(rs) {
		t.Fatalf("setup: AllTestsPass passed a failing scenario: %+v", rs)
	}
	if got, want := refsOf(rs), []string{"R-2/E-2"}; !slices.Equal(got, want) {
		t.Errorf("refs = %v, want %v", got, want)
	}
}

// File order (6) is not tested separately: godog runs a suite's scenarios one after another in
// the file's order (no Concurrency option is set), so output order and file order cannot differ.

// A plain table-driven test with as many subtests as the feature has scenarios must not be taken
// for the godog suite: its failing subtest names no scenario.
func TestATableTestsSubtestsAreNotScenarios(t *testing.T) {
	rs := checks.AllTestsPass(refsRunner(t), refsDir("table"))
	if allPassed(rs) {
		t.Fatalf("setup: AllTestsPass passed a failing table test: %+v", rs)
	}
	if got := refsOf(rs); len(got) != 0 {
		t.Errorf("refs = %v, want none (the failing subtest is not a scenario)", got)
	}
}

// A suite that runs only the @run scenarios: the failing one is named by its header's location.
func TestAFilteredSuiteNamesItsFailingScenario(t *testing.T) {
	rs := checks.AllTestsPass(refsRunner(t), refsDir("filtered-run"))
	if allPassed(rs) {
		t.Fatalf("setup: AllTestsPass passed a failing scenario: %+v", rs)
	}
	if got, want := refsOf(rs), []string{"R-3/E-3"}; !slices.Equal(got, want) {
		t.Errorf("refs = %v, want %v", got, want)
	}
}
