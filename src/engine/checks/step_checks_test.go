// T042: failing tests for the step checks (implemented in T048, checks/steps.go and friends).
//
// The API these tests fix, so the implementer follows it. Every check returns the existing
// CheckResult (Part, Check, Passed, Reason): a failing result's Reason names the violation (the
// path, import, rule or hash at fault), a passing one says what was confirmed. Pure Go over
// directories and values: no model, network or clock. Fixtures: testdata/steps/<check>/{clean,planted}.
//
//	// ScopeKept: every file added, changed or removed between the two project trees must match a
//	// writable glob (pattern.yaml `writable`; "dir/**" is everything below dir, other entries are
//	// exact paths). Fails naming each file outside.
//	func Scope(before, after string, writable []string) []CheckResult
//
//	// TestsUnchanged: files under testGlobs (same glob syntax) are byte-identical in both trees:
//	// none changed, added or removed.
//	func TestsUnchanged(before, after string, testGlobs []string) []CheckResult
//
//	// DependenciesAllowed: every import in the project's .go files and every DIRECT require in
//	// go.mod (no "// indirect") outside the project's own module must match allowed and no
//	// forbidden entry (entries are Go package patterns: "std", a path, or "path/..."; forbidden
//	// wins). The allowed list does not apply to indirect requires, but the forbidden list applies
//	// to every require, "// indirect" included.
//	func Dependencies(project string, allowed, forbidden []string) []CheckResult
//
//	// Also a failure for Dependencies: any `replace` directive in go.mod (a replace can swap an
//	// allowed module for another or for a local path), and any symlinked directory or symlinked
//	// .go file anywhere in the project. vendor/ stays out of the import scan: only the fixed
//	// generate procedure writes it, and Scope keeps every other step out of it.
//
//	// GoRunner runs the go toolchain in dir (args are the go arguments, e.g. "test", "./...") and
//	// reports the exit code and combined output. An error means the tool could not be started.
//	type GoRunner interface { Go(dir string, args ...string) (GoResult, error) }
//	type GoResult struct { ExitCode int; Output string }
//	func NewGoRunner() GoRunner // the real toolchain on PATH, with a default bound (minutes)
//	func NewGoRunnerWithTimeout(d time.Duration) GoRunner // a go command running past d is killed
//	// and the check that ran it FAILS, its reason saying it "timed out".
//
//	// TestsFailAgainstStub runs `go test ./tests/...`; passes only when it exits non-zero with at
//	// least one "--- FAIL" and no "[build failed]" or "[setup failed]" (a test that cannot build
//	// proves nothing).
//	func TestsFailAgainstStub(r GoRunner, project string) []CheckResult
//
//	// AllTestsPass runs `go test ./...`; passes only on exit code 0 and at least one test run: a
//	// project with no test files ("[no test files]" for every package, or "no packages") fails.
//	func AllTestsPass(r GoRunner, project string) []CheckResult
//
//	// GodogStrict runs the project's Gherkin examples through `go test ./tests/...` (first arg
//	// "test"): the tests run godog's TestSuite with godog.Options{Strict: true, Paths: features},
//	// so an undefined or pending step fails the test. Passes only on exit 0 and no "undefined"
//	// in the output and at least one scenario run ("No scenarios" or "0 scenarios" fails); the
//	// reason names "undefined" when it fails for that. The godog fixtures are
//	// real Go modules with a go.sum for godog v0.16.0 (src/deploy/images.lock); the real test runs
//	// offline (GOPROXY=off) from the module cache, which scripts/check.sh fills beforehand by
//	// downloading that version (the implementer adds that step). A cache without it fails the test.
//	func GodogStrict(r GoRunner, project string) []CheckResult
//
//	// GenerationDrift copies project to a temp dir, runs `go generate ./...` there (first arg
//	// "generate"), and passes only when every file under gen/ equals the project's (same set,
//	// same bytes).
//	func GenerationDrift(r GoRunner, project string) []CheckResult
//
//	// ContractMatchesApproval: approved is the hash the slice's approval stores: SHA-256 (lower-case
//	// hex) of the RFC 8785 form of {"contract": <text of api/openapi.yaml>} (slices.contractHash,
//	// research D17/D25). It is not the SHA-256 of the file's bytes. The implementation reuses that
//	// one function (single owner); the tests compute the same value independently.
//	func ContractHash(project, approved string) []CheckResult
package checks_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/checks"
)

// approvedHash is the slice approval's contract hash: SHA-256 of {"contract": <text>} in RFC 8785
// form. Only one member whose value is a string is hashed, so encoding/json (HTML escaping off)
// yields the canonical bytes for the ASCII fixtures used here.
func approvedHash(t *testing.T, project string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(project, "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(string(b)); err != nil {
		t.Fatal(err)
	}
	canon := `{"contract":` + strings.TrimSuffix(out.String(), "\n") + `}`
	sum := sha256.Sum256([]byte(canon))
	return hex.EncodeToString(sum[:])
}

func stepDir(check, kind string, more ...string) string {
	return filepath.Join(append([]string{"testdata", "steps", check, kind}, more...)...)
}

func allPassed(rs []checks.CheckResult) bool {
	for _, r := range rs {
		if !r.Passed {
			return false
		}
	}
	return len(rs) > 0
}

func reasons(rs []checks.CheckResult) string {
	var b strings.Builder
	for _, r := range rs {
		if !r.Passed {
			b.WriteString(r.Reason + "\n")
		}
	}
	return b.String()
}

// fakeGo answers each go subcommand with a canned result; gen, when set, runs on "generate".
type fakeGo struct {
	res map[string]checks.GoResult
	gen func(dir string)
}

func (f fakeGo) Go(dir string, args ...string) (checks.GoResult, error) {
	if args[0] == "generate" && f.gen != nil {
		f.gen(dir)
	}
	return f.res[args[0]], nil
}

func needGo(t *testing.T) checks.GoRunner {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatal("the go toolchain is not on PATH: this test runs the real toolchain and cannot pass without it")
	}
	t.Setenv("GOWORK", "off") // the fixtures are modules of their own, not part of any workspace
	return checks.NewGoRunner()
}

var (
	writable = []string{"tests/**"}
	allowed  = []string{"std", "github.com/oapi-codegen/runtime/..."}
	forbid   = []string{"os/exec", "syscall", "unsafe", "plugin", "net/rpc/..."}
)

func TestScope(t *testing.T) {
	if rs := checks.Scope(stepDir("scope", "clean", "before"), stepDir("scope", "clean", "after"), writable); !allPassed(rs) {
		t.Fatalf("clean change inside the writable paths rejected: %+v", rs)
	}
	rs := checks.Scope(stepDir("scope", "planted", "before"), stepDir("scope", "planted", "after"), writable)
	if allPassed(rs) || !strings.Contains(reasons(rs), "internal/x.go") {
		t.Fatalf("change outside the writable paths not rejected by name: %+v", rs)
	}
	// An added or removed file outside scope counts too.
	tmp := t.TempDir()
	if rs := checks.Scope(stepDir("scope", "clean", "before"), tmp, writable); allPassed(rs) {
		t.Fatalf("deleting files outside scope passed: %+v", rs)
	}
}

func TestTestsUnchanged(t *testing.T) {
	g := []string{"tests/**"}
	if rs := checks.TestsUnchanged(stepDir("testsunchanged", "clean", "before"), stepDir("testsunchanged", "clean", "after"), g); !allPassed(rs) {
		t.Fatalf("identical tests rejected: %+v", rs)
	}
	rs := checks.TestsUnchanged(stepDir("testsunchanged", "planted", "before"), stepDir("testsunchanged", "planted", "after"), g)
	if allPassed(rs) || !strings.Contains(reasons(rs), "tests/a_test.go") {
		t.Fatalf("edited test not rejected by name: %+v", rs)
	}
	// A test added or deleted by the code step also fails.
	if rs := checks.TestsUnchanged(stepDir("testsunchanged", "clean", "before"), t.TempDir(), g); allPassed(rs) {
		t.Fatalf("deleted test passed: %+v", rs)
	}
}

func TestDependencies(t *testing.T) {
	if rs := checks.Dependencies(stepDir("dependencies", "clean"), allowed, forbid); !allPassed(rs) {
		t.Fatalf("allowed imports and requires rejected: %+v", rs)
	}
	rs := checks.Dependencies(stepDir("dependencies", "planted"), allowed, forbid)
	got := reasons(rs)
	if allPassed(rs) {
		t.Fatalf("planted dependencies passed")
	}
	// os/exec is std (allowed) but forbidden: forbidden wins. evil/lib is in go.mod and imported
	// but on no list.
	for _, want := range []string{"os/exec", "github.com/evil/lib"} {
		if !strings.Contains(got, want) {
			t.Errorf("no reason names %s: %q", want, got)
		}
	}
}

func TestDependenciesIndirect(t *testing.T) {
	// An indirect require not on the allowed list passes; one on the forbidden list fails.
	mod := func(indirect string) string {
		d := t.TempDir()
		gomod := "module example.com/p\n\ngo 1.26\n\nrequire github.com/oapi-codegen/runtime v1.1.0\n\nrequire " + indirect + " // indirect\n"
		if err := os.WriteFile(filepath.Join(d, "go.mod"), []byte(gomod), 0o644); err != nil {
			t.Fatal(err)
		}
		return d
	}
	if rs := checks.Dependencies(mod("github.com/other/lib v1.0.0"), allowed, forbid); !allPassed(rs) {
		t.Fatalf("indirect require outside the allowed list rejected: %+v", rs)
	}
	rs := checks.Dependencies(mod("net/rpc/jsonrpc v1.0.0"), allowed, []string{"net/rpc/..."})
	if allPassed(rs) || !strings.Contains(reasons(rs), "net/rpc/jsonrpc") {
		t.Fatalf("indirect require on the forbidden list not rejected by name: %+v", rs)
	}
	rs = checks.Dependencies(mod("github.com/evil/lib v1.0.0"), allowed, []string{"github.com/evil/..."})
	if allPassed(rs) || !strings.Contains(reasons(rs), "github.com/evil/lib") {
		t.Fatalf("indirect forbidden module not rejected by name: %+v", rs)
	}
	// A direct require outside the allowed list still fails.
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "go.mod"), []byte("module example.com/p\n\ngo 1.26\n\nrequire github.com/other/lib v1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rs := checks.Dependencies(d, allowed, forbid); allPassed(rs) {
		t.Fatalf("direct require outside the allowed list passed: %+v", rs)
	}
}

func TestTestsFailAgainstStub(t *testing.T) {
	fail := checks.GoResult{ExitCode: 1, Output: "--- FAIL: TestExample\nFAIL"}
	cases := []struct {
		name string
		res  checks.GoResult
		pass bool
	}{
		{"failing tests", fail, true},
		{"tests pass against the stub", checks.GoResult{Output: "ok"}, false},
		{"tests do not build", checks.GoResult{ExitCode: 1, Output: "FAIL example.com/p/tests [build failed]"}, false},
	}
	for _, c := range cases {
		rs := checks.TestsFailAgainstStub(fakeGo{res: map[string]checks.GoResult{"test": c.res}}, "unused")
		if allPassed(rs) != c.pass {
			t.Errorf("%s: passed=%v, want %v: %+v", c.name, allPassed(rs), c.pass, rs)
		}
		if !c.pass && strings.TrimSpace(reasons(rs)) == "" {
			t.Errorf("%s: no reason", c.name)
		}
	}
}

func TestTestsFailAgainstStubReal(t *testing.T) {
	r := needGo(t)
	if rs := checks.TestsFailAgainstStub(r, stepDir("stubfail", "clean")); !allPassed(rs) {
		t.Fatalf("clean: %+v", rs)
	}
	if rs := checks.TestsFailAgainstStub(r, stepDir("stubfail", "planted")); allPassed(rs) {
		t.Fatalf("planted: a passing test counted as failing against the stub")
	}
}

func TestAllTestsPass(t *testing.T) {
	if rs := checks.AllTestsPass(fakeGo{res: map[string]checks.GoResult{"test": {Output: "ok"}}}, "unused"); !allPassed(rs) {
		t.Fatalf("passing run rejected: %+v", rs)
	}
	rs := checks.AllTestsPass(fakeGo{res: map[string]checks.GoResult{"test": {ExitCode: 1, Output: "--- FAIL: TestExample"}}}, "unused")
	if allPassed(rs) || !strings.Contains(reasons(rs), "TestExample") {
		t.Fatalf("failing run not rejected with the failing test named: %+v", rs)
	}
}

func TestAllTestsPassReal(t *testing.T) {
	r := needGo(t)
	if rs := checks.AllTestsPass(r, stepDir("allpass", "clean")); !allPassed(rs) {
		t.Fatalf("clean: %+v", rs)
	}
	if rs := checks.AllTestsPass(r, stepDir("allpass", "planted")); allPassed(rs) {
		t.Fatalf("planted: failing test passed")
	}
}

func TestGodogStrict(t *testing.T) {
	if rs := checks.GodogStrict(fakeGo{res: map[string]checks.GoResult{"test": {Output: "1 scenarios (1 passed)"}}}, "unused"); !allPassed(rs) {
		t.Fatalf("clean run rejected: %+v", rs)
	}
	undef := checks.GoResult{ExitCode: 1, Output: "1 scenarios (1 undefined)\nYou can implement step definitions for undefined steps"}
	rs := checks.GodogStrict(fakeGo{res: map[string]checks.GoResult{"test": undef}}, "unused")
	if allPassed(rs) || !strings.Contains(reasons(rs), "undefined") {
		t.Fatalf("undefined steps not rejected: %+v", rs)
	}
	// Exit 0 with "undefined" in the output must still fail (strictness is not left to the exit code).
	rs = checks.GodogStrict(fakeGo{res: map[string]checks.GoResult{"test": {Output: "1 scenarios (1 undefined)"}}}, "unused")
	if allPassed(rs) {
		t.Fatalf("undefined steps with exit 0 passed")
	}
}

// The real run is offline: godog v0.16.0 must already be in the module cache (scripts/check.sh
// downloads it). If it is not, the run fails here with the toolchain's message, not a skip.
func TestGodogStrictReal(t *testing.T) {
	r := needGo(t)
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	if rs := checks.GodogStrict(r, stepDir("godog", "clean")); !allPassed(rs) {
		t.Fatalf("clean: %+v", rs)
	}
	rs := checks.GodogStrict(r, stepDir("godog", "planted"))
	if allPassed(rs) || !strings.Contains(strings.ToLower(reasons(rs)), "undefined") {
		t.Fatalf("planted: undefined step not rejected as undefined: %+v", rs)
	}
}

func TestGenerationDrift(t *testing.T) {
	fresh := func(content string) fakeGo {
		return fakeGo{gen: func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, "gen", "gen.go"), []byte(content), 0o644); err != nil {
				t.Error(err)
			}
		}}
	}
	clean, err := os.ReadFile(stepDir("drift", "clean", "gen", "gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if rs := checks.GenerationDrift(fresh(string(clean)), stepDir("drift", "clean")); !allPassed(rs) {
		t.Fatalf("identical generation rejected: %+v", rs)
	}
	rs := checks.GenerationDrift(fresh(string(clean)), stepDir("drift", "planted"))
	if allPassed(rs) || !strings.Contains(reasons(rs), "gen/gen.go") {
		t.Fatalf("hand-edited generated file not rejected by name: %+v", rs)
	}
	// The check must not touch the project it inspects.
	if b, _ := os.ReadFile(stepDir("drift", "planted", "gen", "gen.go")); !strings.Contains(string(b), "hand edited") {
		t.Fatal("GenerationDrift modified the project it checks")
	}
}

func TestGenerationDriftReal(t *testing.T) {
	r := needGo(t)
	if rs := checks.GenerationDrift(r, stepDir("drift", "clean")); !allPassed(rs) {
		t.Fatalf("clean: %+v", rs)
	}
	if rs := checks.GenerationDrift(r, stepDir("drift", "planted")); allPassed(rs) {
		t.Fatalf("planted: drift not detected")
	}
}

func TestContractHash(t *testing.T) {
	approved := approvedHash(t, stepDir("contracthash", "clean"))
	// The plain SHA-256 of the file is not the approval's hash and must not match.
	raw, _ := os.ReadFile(stepDir("contracthash", "clean", "api", "openapi.yaml"))
	plain := sha256.Sum256(raw)
	if rs := checks.ContractHash(stepDir("contracthash", "clean"), hex.EncodeToString(plain[:])); allPassed(rs) {
		t.Fatalf("the raw file hash was accepted as the approved hash: %+v", rs)
	}
	if rs := checks.ContractHash(stepDir("contracthash", "clean"), approved); !allPassed(rs) {
		t.Fatalf("approved contract rejected: %+v", rs)
	}
	rs := checks.ContractHash(stepDir("contracthash", "planted"), approved)
	if allPassed(rs) || !strings.Contains(reasons(rs), "api/openapi.yaml") {
		t.Fatalf("changed contract not rejected by name: %+v", rs)
	}
	if rs := checks.ContractHash(t.TempDir(), approved); allPassed(rs) {
		t.Fatalf("missing contract passed")
	}
}

// TestEveryStepCheckFailsOnItsPlantedFixture runs each check on its clean fixture (must pass) and
// its planted one (must fail, reason naming the violation), using the fakes where a Go run is needed.
func TestEveryStepCheckFailsOnItsPlantedFixture(t *testing.T) {
	cleanDrift, _ := os.ReadFile(stepDir("drift", "clean", "gen", "gen.go"))
	regen := fakeGo{gen: func(dir string) { _ = os.WriteFile(filepath.Join(dir, "gen", "gen.go"), cleanDrift, 0o644) }}
	failRun := fakeGo{res: map[string]checks.GoResult{"test": {ExitCode: 1, Output: "--- FAIL: TestExample\n1 scenarios (1 undefined)"}}}
	passRun := fakeGo{res: map[string]checks.GoResult{"test": {Output: "ok"}}}
	approved := approvedHash(t, stepDir("contracthash", "clean"))

	cases := []struct {
		name           string
		clean, planted func() []checks.CheckResult
		want           string
	}{
		{"scope",
			func() []checks.CheckResult {
				return checks.Scope(stepDir("scope", "clean", "before"), stepDir("scope", "clean", "after"), writable)
			},
			func() []checks.CheckResult {
				return checks.Scope(stepDir("scope", "planted", "before"), stepDir("scope", "planted", "after"), writable)
			}, "internal/x.go"},
		{"tests unchanged",
			func() []checks.CheckResult {
				return checks.TestsUnchanged(stepDir("testsunchanged", "clean", "before"), stepDir("testsunchanged", "clean", "after"), []string{"tests/**"})
			},
			func() []checks.CheckResult {
				return checks.TestsUnchanged(stepDir("testsunchanged", "planted", "before"), stepDir("testsunchanged", "planted", "after"), []string{"tests/**"})
			}, "tests/a_test.go"},
		{"dependencies",
			func() []checks.CheckResult {
				return checks.Dependencies(stepDir("dependencies", "clean"), allowed, forbid)
			},
			func() []checks.CheckResult {
				return checks.Dependencies(stepDir("dependencies", "planted"), allowed, forbid)
			},
			"os/exec"},
		{"tests fail against stub",
			func() []checks.CheckResult { return checks.TestsFailAgainstStub(failRun, "unused") },
			func() []checks.CheckResult { return checks.TestsFailAgainstStub(passRun, "unused") },
			"pass"},
		{"all tests pass",
			func() []checks.CheckResult { return checks.AllTestsPass(passRun, "unused") },
			func() []checks.CheckResult { return checks.AllTestsPass(failRun, "unused") },
			"TestExample"},
		{"godog strict",
			func() []checks.CheckResult { return checks.GodogStrict(passRun, "unused") },
			func() []checks.CheckResult { return checks.GodogStrict(failRun, "unused") },
			"undefined"},
		{"generation drift",
			func() []checks.CheckResult { return checks.GenerationDrift(regen, stepDir("drift", "clean")) },
			func() []checks.CheckResult { return checks.GenerationDrift(regen, stepDir("drift", "planted")) },
			"gen/gen.go"},
		{"contract hash",
			func() []checks.CheckResult { return checks.ContractHash(stepDir("contracthash", "clean"), approved) },
			func() []checks.CheckResult { return checks.ContractHash(stepDir("contracthash", "planted"), approved) },
			"api/openapi.yaml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rs := c.clean(); !allPassed(rs) {
				t.Errorf("clean fixture failed: %+v", rs)
			}
			rs := c.planted()
			if allPassed(rs) {
				t.Fatalf("planted fixture passed: a check that cannot fail proves nothing")
			}
			if !strings.Contains(reasons(rs), c.want) {
				t.Errorf("reason does not name the violation %q: %q", c.want, reasons(rs))
			}
		})
	}
}

func TestAllTestsPassNeedsATestToRun(t *testing.T) {
	for _, out := range []string{"?   \texample.com/p/tests\t[no test files]", "warning: \"./...\" matched no packages\nno packages to test"} {
		rs := checks.AllTestsPass(fakeGo{res: map[string]checks.GoResult{"test": {Output: out}}}, "unused")
		if allPassed(rs) || strings.TrimSpace(reasons(rs)) == "" {
			t.Errorf("exit 0 with no test run passed (%q): %+v", out, rs)
		}
	}
}

func TestAllTestsPassNoTestFilesReal(t *testing.T) {
	r := needGo(t)
	if rs := checks.AllTestsPass(r, stepDir("allpass", "planted_notests")); allPassed(rs) {
		t.Fatalf("a project without test files passed")
	}
}

func TestGodogStrictNeedsAScenarioToRun(t *testing.T) {
	for _, out := range []string{"No scenarios\nNo steps", "0 scenarios\n0 steps"} {
		rs := checks.GodogStrict(fakeGo{res: map[string]checks.GoResult{"test": {Output: out}}}, "unused")
		if allPassed(rs) || strings.TrimSpace(reasons(rs)) == "" {
			t.Errorf("exit 0 with no scenario run passed (%q): %+v", out, rs)
		}
	}
}

func TestGodogStrictNoScenariosReal(t *testing.T) {
	r := needGo(t)
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	if rs := checks.GodogStrict(r, stepDir("godog", "planted_noscenarios")); allPassed(rs) {
		t.Fatalf("a suite that runs zero scenarios passed")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDependenciesReplaceAndSymlinks(t *testing.T) {
	base := "module example.com/p\n\ngo 1.26\n\nrequire github.com/oapi-codegen/runtime v1.1.0\n"
	ok := "package internal\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n"
	project := func(gomod string) string {
		d := t.TempDir()
		writeFile(t, filepath.Join(d, "go.mod"), gomod)
		writeFile(t, filepath.Join(d, "internal", "x.go"), ok)
		return d
	}
	if rs := checks.Dependencies(project(base), allowed, forbid); !allPassed(rs) {
		t.Fatalf("control project rejected: %+v", rs)
	}
	for name, gomod := range map[string]string{
		"replace by a forbidden module": base + "\nreplace github.com/oapi-codegen/runtime => github.com/evil/runtime v1.0.0\n",
		"replace by a local path":       base + "\nreplace github.com/oapi-codegen/runtime => ../elsewhere\n",
		"replace block":                 base + "\nreplace (\n\tgithub.com/oapi-codegen/runtime => ../elsewhere\n)\n",
		"replace of a version":          base + "\nreplace github.com/oapi-codegen/runtime v1.1.0 => github.com/oapi-codegen/runtime v1.0.0\n",
	} {
		rs := checks.Dependencies(project(gomod), allowed, forbid)
		if allPassed(rs) || !strings.Contains(reasons(rs), "replace") {
			t.Errorf("%s: not rejected naming the replace directive: %+v", name, rs)
		}
	}

	// Symlinks, created at test time: a linked directory and a linked .go file.
	d := project(base)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "y.go"), "package y\n\nimport \"os/exec\"\n\nvar _ = exec.Command\n")
	if err := os.Symlink(outside, filepath.Join(d, "linkeddir")); err != nil {
		t.Fatal(err)
	}
	rs := checks.Dependencies(d, allowed, forbid)
	if allPassed(rs) || !strings.Contains(reasons(rs), "linkeddir") {
		t.Errorf("symlinked directory not rejected by name: %+v", rs)
	}
	d = project(base)
	if err := os.Symlink(filepath.Join(outside, "y.go"), filepath.Join(d, "internal", "linked.go")); err != nil {
		t.Fatal(err)
	}
	rs = checks.Dependencies(d, allowed, forbid)
	if allPassed(rs) || !strings.Contains(reasons(rs), "linked.go") {
		t.Errorf("symlinked .go file not rejected by name: %+v", rs)
	}

	// vendor/ is not scanned.
	d = project(base)
	writeFile(t, filepath.Join(d, "vendor", "x", "y.go"), "package y\n\nimport \"os/exec\"\n\nvar _ = exec.Command\n")
	if rs := checks.Dependencies(d, allowed, forbid); !allPassed(rs) {
		t.Errorf("vendor/ was scanned: %+v", rs)
	}
}

func TestGoRunnerTimeout(t *testing.T) {
	needGo(t)
	r := checks.NewGoRunnerWithTimeout(5 * time.Second)
	start := time.Now()
	rs := checks.AllTestsPass(r, stepDir("allpass", "planted_sleep"))
	if allPassed(rs) || !strings.Contains(strings.ToLower(reasons(rs)), "timed out") {
		t.Fatalf("a command past the timeout did not fail the check with a timed out reason: %+v", rs)
	}
	if d := time.Since(start); d > 45*time.Second {
		t.Fatalf("the timeout did not stop the command: took %s", d)
	}
}
