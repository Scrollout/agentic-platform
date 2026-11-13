package checks_test

// T049c: EveryExampleBound counts only godog's own summary line, expands a Scenario Outline into
// one scenario per Examples row, and ignores `Scenario:` inside a docstring or a `#` comment of
// features/slice.feature. Written by a separate author from the implementation.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/checks"
)

func realBound(t *testing.T, kind string) []checks.CheckResult {
	t.Helper()
	r := needGo(t)
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	return checks.GodogStrict(r, filepath.Join("testdata", "everybound", filepath.FromSlash(kind)))
}

// A test that prints "2 scenarios (2 passed)" must not make up for the scenario godog left unrun.
func TestEveryExampleBoundIgnoresAScenarioCountAPrintedLineFakes(t *testing.T) {
	rs := realBound(t, "printed")
	if allPassed(rs) {
		t.Fatalf("a printed line inflated the scenario count: %+v", rs)
	}
	if !strings.Contains(reasons(rs), "scenarios") {
		t.Errorf("the reason should say not every example ran: %+v", rs)
	}
}

func TestEveryExampleBoundCountsEachOutlineRow(t *testing.T) {
	if rs := realBound(t, "outline/failing"); allPassed(rs) {
		t.Errorf("3 of 4 scenarios (outline rows expanded) ran, and it passed: %+v", rs)
	}
	if rs := realBound(t, "outline/passing"); !allPassed(rs) {
		t.Errorf("every outline row ran and it failed: %+v", rs)
	}
}

func TestEveryExampleBoundIgnoresScenarioTextInADocstringOrComment(t *testing.T) {
	if rs := realBound(t, "docstring/passing"); !allPassed(rs) {
		t.Errorf("every real scenario ran, but docstring or comment text was counted: %+v", rs)
	}
	if rs := realBound(t, "docstring/failing"); allPassed(rs) {
		t.Errorf("one real scenario did not run and it passed: %+v", rs)
	}
}
