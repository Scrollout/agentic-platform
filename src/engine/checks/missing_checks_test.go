// T054e: failing tests, by a separate author, for the step checks the pattern declares and
// engine/checks does not have yet: ExamplesMatchApproval, GeneratedCodeBuilds, TestsBuild,
// GeneratedCodeUnchanged and NoBlockingFindingOpen.
//
// ASSUMED FUNCTION SHAPES (the implementer follows the existing ones: `ContractHash(project,
// approved)`, `TestsUnchanged(before, after, globs)`, `GodogStrict(r, project)`). Every check
// returns []CheckResult; a failing result's Reason names the file, rule or hash at fault (project
// paths only), and a pass returns at least one passing result.
//
//	// ExamplesMatchApproval: project/features/slice.feature is byte for byte approved, the
//	// `examples` text of the slice's approved rules part (check contract, runCheck).
//	func ExamplesMatchApproval(project, approved string) []CheckResult
//
//	// GeneratedCodeBuilds: `go build ./...` of the project, offline, from vendor/ (the project is
//	// built with -mod=vendor, so a module that is only in the module cache does not count).
//	func GeneratedCodeBuilds(r GoRunner, project string) []CheckResult
//
//	// TestsBuild: every test package builds (compiles); the tests are not judged, only built.
//	func TestsBuild(r GoRunner, project string) []CheckResult
//
//	// GeneratedCodeUnchanged: like TestsUnchanged, for the globs of the step that wrote the
//	// generated code (gen/**, go.mod, go.sum, vendor/**): none added, changed or removed.
//	func GeneratedCodeUnchanged(before, after string, genGlobs []string) []CheckResult
//
//	// NoBlockingFindingOpen: the owner's choice of 2026-11-13: a review finding is confirmed by
//	// rerunning the tests of the rule it names (godog scenarios tagged @R-<n>, and Go tests that
//	// name the rule) against the current code in project; it blocks only if they fail at the file
//	// it names. A blocking finding so confirmed fails the check, naming its rule and file. A finding
//	// whose rule's tests pass, or fail somewhere else, or one that is not blocking, is a
//	// suggestion and does not fail it. No blocking finding at all passes (one passing result).
//	func NoBlockingFindingOpen(r GoRunner, project string, findings []Finding) []CheckResult
//
// Fixtures: testdata/missing/. The real Go runs are offline (needGo sets GOPROXY=off through the
// runner) and fail, never skip, when the toolchain or module cache is missing.
package checks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/checks"
)

func mcDir(parts ...string) string {
	return filepath.Join(append([]string{"testdata", "missing"}, parts...)...)
}

func TestExamplesMatchApprovalMissing(t *testing.T) {
	approved, err := os.ReadFile(mcDir("examples", "clean", "features", "slice.feature"))
	if err != nil {
		t.Fatal(err)
	}
	if rs := checks.ExamplesMatchApproval(mcDir("examples", "clean"), string(approved)); !allPassed(rs) {
		t.Fatalf("a feature file equal to the approved examples was rejected: %+v", rs)
	}
	rs := checks.ExamplesMatchApproval(mcDir("examples", "planted"), string(approved))
	if allPassed(rs) || !strings.Contains(reasons(rs), "features/slice.feature") {
		t.Fatalf("a feature file with one more line feed than the approved examples was accepted, or the reason does not name the file: %+v", rs)
	}
	// Not a copy of the approval at all: the file is missing.
	rs = checks.ExamplesMatchApproval(t.TempDir(), string(approved))
	if allPassed(rs) || !strings.Contains(reasons(rs), "features/slice.feature") {
		t.Fatalf("a project with no features/slice.feature passed, or the reason does not name it: %+v", rs)
	}
	// Byte for byte: the approved text differing by one byte fails the clean file too.
	if rs := checks.ExamplesMatchApproval(mcDir("examples", "clean"), strings.TrimSuffix(string(approved), "\n")); allPassed(rs) {
		t.Fatal("approved examples differing from the file by a trailing line feed were accepted")
	}
}

func TestGeneratedCodeBuildsMissing(t *testing.T) {
	r := needGo(t)
	if rs := checks.GeneratedCodeBuilds(r, mcDir("buildok")); !allPassed(rs) {
		t.Fatalf("a project that builds was rejected: %+v", rs)
	}
	rs := checks.GeneratedCodeBuilds(r, mcDir("buildbroken"))
	if allPassed(rs) || !strings.Contains(reasons(rs), "gen/gen.go") {
		t.Fatalf("a project that does not compile passed, or the reason does not name gen/gen.go: %+v", rs)
	}
	// The module is in the module cache but not in vendor/: built from vendor/ it does not build.
	if rs := checks.GeneratedCodeBuilds(r, mcDir("buildnovendor")); allPassed(rs) {
		t.Fatalf("a project whose dependency is not vendored passed: the build must use vendor/ alone")
	}
}

func TestGeneratedCodeBuildsNamesNoHostPath(t *testing.T) {
	r := needGo(t)
	abs, err := filepath.Abs(mcDir("buildbroken"))
	if err != nil {
		t.Fatal(err)
	}
	if got := reasons(checks.GeneratedCodeBuilds(r, mcDir("buildbroken"))); strings.Contains(got, abs) {
		t.Fatalf("the reason names the host path %s, not project paths only: %s", abs, got)
	}
}

func TestTestsBuildMissing(t *testing.T) {
	r := needGo(t)
	if rs := checks.TestsBuild(r, mcDir("testspass")); !allPassed(rs) {
		t.Fatalf("test packages that build were rejected: %+v", rs)
	}
	rs := checks.TestsBuild(r, mcDir("testsnobuild"))
	if allPassed(rs) || !strings.Contains(reasons(rs), "tests/broken_test.go") {
		t.Fatalf("a test package that does not compile passed, or the reason does not name tests/broken_test.go: %+v", rs)
	}
}

func TestGeneratedCodeUnchangedMissing(t *testing.T) {
	globs := []string{"gen/**", "go.mod", "go.sum", "vendor/**"}
	before := mcDir("unchanged", "before")
	if rs := checks.GeneratedCodeUnchanged(before, mcDir("unchanged", "clean"), globs); !allPassed(rs) {
		t.Fatalf("a change outside the generated code (internal/x.go) was rejected: %+v", rs)
	}
	rs := checks.GeneratedCodeUnchanged(before, mcDir("unchanged", "planted"), globs)
	got := reasons(rs)
	if allPassed(rs) {
		t.Fatal("changed, added and removed generated files passed")
	}
	for _, p := range []string{"gen/gen.go", "gen/extra.go", "go.mod"} {
		if !strings.Contains(got, p) {
			t.Errorf("the reason does not name %s: %s", p, got)
		}
	}
	if strings.Contains(got, "internal/x.go") {
		t.Errorf("the reason names internal/x.go, which is not generated code: %s", got)
	}
	// A removal counts too.
	rs = checks.GeneratedCodeUnchanged(mcDir("unchanged", "planted"), before, globs)
	if allPassed(rs) || !strings.Contains(reasons(rs), "gen/extra.go") {
		t.Fatalf("a removed generated file passed, or the reason does not name it: %+v", rs)
	}
}

// A finding is a suggestion unless it is blocking and the tests of its rule fail at its file.
func TestNoBlockingFindingOpenMissing(t *testing.T) {
	r := needGo(t)
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	project := mcDir("review")
	const file = "internal/greet/greet.go"
	f := func(rule, path string, blocking bool) checks.Finding {
		return checks.Finding{Rule: rule, File: path, Line: 7, Text: "the greeting leaves the name out", Blocking: blocking}
	}

	rs := checks.NoBlockingFindingOpen(r, project, []checks.Finding{f("R-1", file, true)})
	if allPassed(rs) {
		t.Fatalf("a blocking finding whose rule's tests fail at its file did not block: %+v", rs)
	}
	if got := reasons(rs); !strings.Contains(got, "R-1") || !strings.Contains(got, file) {
		t.Fatalf("the reason names neither the rule R-1 nor %s: %s", file, got)
	}

	for name, fs := range map[string][]checks.Finding{
		"no findings":                         nil,
		"a blocking finding whose tests pass": {f("R-2", file, true)},
		"a non-blocking finding, tests fail":  {f("R-1", file, false)},
		// R-1's tests fail at internal/greet/greet.go, not at this file: not confirmed.
		"a blocking finding naming another file": {f("R-1", "internal/other/other.go", true)},
		// R10 is not R-1 and XR3 is not R-3: TestR10Collides and TestXR3Collides fail at other.go.
		"R-3 at the file its test does not fail at": {f("R-3", file, true)},
		"a rule no test names":                      {f("R-99", file, true)},
	} {
		if rs := checks.NoBlockingFindingOpen(r, project, fs); !allPassed(rs) {
			t.Errorf("%s must not block: %+v", name, rs)
		}
	}

	// A Go test named for the rule (TestR3Names fails at other.go:3, the subtest R_4_sub at greet.go:9).
	for rule, path := range map[string]string{"R-3": "internal/other/other.go", "R-4": file} {
		rs := checks.NoBlockingFindingOpen(r, project, []checks.Finding{f(rule, path, true)})
		if allPassed(rs) || !strings.Contains(reasons(rs), rule) || !strings.Contains(reasons(rs), path) {
			t.Errorf("a Go test that names %s fails at %s, so the finding blocks, naming both: %+v", rule, path, rs)
		}
	}

	// One confirmed blocking finding among suggestions still blocks, and only it is named.
	rs = checks.NoBlockingFindingOpen(r, project, []checks.Finding{f("R-2", file, true), f("R-1", file, true)})
	if allPassed(rs) || !strings.Contains(reasons(rs), "R-1") || strings.Contains(reasons(rs), "R-2") {
		t.Fatalf("expected R-1 alone to block: %+v", rs)
	}
}

// The same findings over the same code give the same verdict (no model, no clock).
func TestNoBlockingFindingOpenRepeats(t *testing.T) {
	r := needGo(t)
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	fs := []checks.Finding{{Rule: "R-2", File: "internal/greet/greet.go", Line: 7, Text: "t", Blocking: true}}
	a := checks.NoBlockingFindingOpen(r, mcDir("review"), fs)
	b := checks.NoBlockingFindingOpen(r, mcDir("review"), fs)
	if allPassed(a) != allPassed(b) || reasons(a) != reasons(b) {
		t.Fatalf("two runs differ: %+v vs %+v", a, b)
	}
}
