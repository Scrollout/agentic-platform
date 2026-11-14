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
