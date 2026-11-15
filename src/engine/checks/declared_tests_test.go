// T093: failing tests, by a separate author, for research D37 and ADR 0041: a test check passes
// only on a `go test -json -count=1` result event for every declared test, never on a summary
// line (data-model.md, "How a check runs Go", "All tests pass", "Tests fail against the stub").
//
// The rules these tests fix (check contract runCheck, T092):
//
//   - A declared test is a top-level function `TestXxx(t *testing.T)` of a `_test.go` file under
//     tests/** or review/**, whose name is `Test` followed by nothing or by a character that is not
//     a lower-case letter, with its package's import path in the module. Subtests, TestMain,
//     benchmarks, examples, fuzz targets and tests outside tests/** and review/** are not.
//   - A result event is a `pass`, `fail` or `skip` event for the test's package and name.
//   - AllTestsPass: `go test -json -count=1 ./...` exits 0 and every declared test has a `pass`
//     event; one with no event, `fail` or `skip` fails the check, naming it; no declared test fails.
//   - TestsFailAgainstStub: `go test -json -count=1 ./tests/...`: the tests build, every declared
//     test under tests/** has a result event, and at least one has `fail`.
//   - EveryExampleBound's fallback (no countable feature file): every declared test under
//     tests/** has a result event.
//   - A witness run whose binary exits before it writes its profile cannot be judged:
//     FindingsCarryEvidence fails and the finding is not confirmed.
//
// No new API is assumed: the tests use the existing checks.AllTestsPass, TestsFailAgainstStub,
// GodogStrict, FindingsCarryEvidence and NoBlockingFindingOpen. "Naming" a test is checked as
// the reason containing the test's name, and its import path where two packages share the name.
//
// The probe (T057 R1): an application package whose init() calls os.Exit(0) when os.Args[0] ends
// in ".test". The test binary exits 0 before any test runs, and `go test` still prints `ok`.
package checks_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/checks"
)

const dtModule = "example.com/dt"

// dtProbeApp is the probe's application package: its init ends a test binary with exit code 0.
const dtProbeApp = `package app

import (
	"os"
	"strings"
)

func init() {
	if strings.HasSuffix(os.Args[0], ".test") {
		os.Exit(0)
	}
}

// Answer is what the declared tests check.
func Answer() int { return 41 }
`

// dtProbeTests are two declared tests that use the probe's package; both fail if they ever run.
const dtProbeTests = `package tests

import (
	"testing"

	"example.com/dt/internal/app"
)

func TestProbeAnswerOne(t *testing.T) {
	if app.Answer() != 42 {
		t.Fatal("wrong answer")
	}
}

func TestProbeAnswerTwo(t *testing.T) {
	if app.Answer() != 42 {
		t.Fatal("wrong answer")
	}
}
`

// dtProject writes a module example.com/dt with the given files (slash paths) into a new folder.
func dtProject(t *testing.T, files map[string]string) string {
	t.Helper()
	d := t.TempDir()
	writeFile(t, filepath.Join(d, "go.mod"), "module "+dtModule+"\n\ngo 1.21\n")
	for p, c := range files {
		writeFile(t, filepath.Join(d, filepath.FromSlash(p)), c)
	}
	return d
}

// dtProbeProject is the probe: the app package exits under a test binary, and tests/ declares
// TestProbeAnswerOne and TestProbeAnswerTwo, which would fail if they ran.
func dtProbeProject(t *testing.T, more map[string]string) string {
	t.Helper()
	files := map[string]string{
		"internal/app/app.go": dtProbeApp,
		"tests/probe_test.go": dtProbeTests,
	}
	for p, c := range more {
		files[p] = c
	}
	return dtProject(t, files)
}

// dtRealGo is the offline toolchain the other real-run tests use, from a module with no
// dependency.
func dtRealGo(t *testing.T) checks.GoRunner {
	t.Helper()
	r := needGo(t)
	t.Setenv("GOPROXY", "off")
	return r
}

// dtProbeIsReal proves the probe does what T057 R1 found: plain `go test` exits 0 and prints
// `ok` with tests that would fail, so the check under test has something to catch.
func dtProbeIsReal(t *testing.T, r checks.GoRunner, project, pkgs string) {
	t.Helper()
	res, err := r.Go(project, "test", "-count=1", pkgs)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Output, "ok") {
		t.Fatalf("the probe does not hide its failing tests from plain go test (exit %d): %s", res.ExitCode, res.Output)
	}
}

// dtEvent is one test2json event.
type dtEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test,omitempty"`
	Output  string `json:"Output,omitempty"`
}

// dtEvents is `go test -json` output: for each test given as "<package suffix>.<Test>:<action>"
// a run event and its result event, then one end event per package (fail when one of its tests
// failed). The package suffix is relative to the module ("tests", "review/r1").
func dtEvents(tests ...string) string {
	var lines []string
	var order []string
	failed := map[string]bool{}
	add := func(e dtEvent) {
		b, _ := json.Marshal(e)
		lines = append(lines, string(b))
	}
	for _, s := range tests {
		pt, action, _ := strings.Cut(s, ":")
		i := strings.LastIndex(pt, ".")
		pkg, name := dtModule+"/"+pt[:i], pt[i+1:]
		if !contains(order, pkg) {
			order = append(order, pkg)
			add(dtEvent{Action: "start", Package: pkg})
		}
		add(dtEvent{Action: "run", Package: pkg, Test: name})
		add(dtEvent{Action: action, Package: pkg, Test: name})
		if action == "fail" {
			failed[pkg] = true
		}
	}
	for _, pkg := range order {
		a := "pass"
		if failed[pkg] {
			a = "fail"
		}
		add(dtEvent{Action: a, Package: pkg})
	}
	return strings.Join(lines, "\n") + "\n"
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// dtFakeGo answers `go test` with fixed -json output and exit code and records the arguments of
// every go command; any other go command (a `go list` the implementation may use) runs on the
// real toolchain.
type dtFakeGo struct {
	res  checks.GoResult
	real checks.GoRunner
	args *[][]string
}

func (f dtFakeGo) Go(dir string, args ...string) (checks.GoResult, error) {
	if f.args != nil {
		*f.args = append(*f.args, append([]string(nil), args...))
	}
	if len(args) > 0 && args[0] == "test" {
		return f.res, nil
	}
	if f.real == nil {
		return checks.GoResult{}, nil
	}
	return f.real.Go(dir, args...)
}

// The probe: the app's init() exits 0 under a test binary, so go test exits 0 and prints `ok`
// while the declared tests never run. AllTestsPass fails and names each declared test that
// vanished.
func TestAllTestsPassFailsWhenInitExitsUnderTest(t *testing.T) {
	r := dtRealGo(t)
	p := dtProbeProject(t, nil)
	dtProbeIsReal(t, r, p, "./...")
	rs := checks.AllTestsPass(r, p)
	if allPassed(rs) {
		t.Fatalf("the probe passed AllTestsPass: its declared tests never ran: %+v", rs)
	}
	for _, name := range []string{"TestProbeAnswerOne", "TestProbeAnswerTwo"} {
		if !strings.Contains(reasons(rs), name) {
			t.Errorf("the reason does not name the vanished test %s: %s", name, reasons(rs))
		}
	}
}

const (
	dtPassingTests = "package tests\n\nimport \"testing\"\n\nfunc TestTestsPassA(t *testing.T) {}\n\nfunc TestTestsPassB(t *testing.T) {}\n"
	dtPassingR1    = "package r1\n\nimport \"testing\"\n\nfunc TestR1Holds(t *testing.T) {}\n"
)

func TestAllTestsPassNeedsAPassEventForEveryDeclaredTest(t *testing.T) {
	r := dtRealGo(t)
	base := map[string]string{"tests/pass_test.go": dtPassingTests, "review/r1/r1_test.go": dtPassingR1}
	with := func(more map[string]string) string {
		files := map[string]string{}
		for k, v := range base {
			files[k] = v
		}
		for k, v := range more {
			files[k] = v
		}
		return dtProject(t, files)
	}

	t.Run("every declared test under tests and review passes", func(t *testing.T) {
		if rs := checks.AllTestsPass(r, with(nil)); !allPassed(rs) {
			t.Fatalf("every declared test passed, and the check failed: %+v", rs)
		}
	})
	t.Run("one declared test skips", func(t *testing.T) {
		p := with(map[string]string{"review/r2/skip_test.go": "package r2\n\nimport \"testing\"\n\nfunc TestR2Skipped(t *testing.T) { t.Skip(\"not now\") }\n"})
		rs := checks.AllTestsPass(r, p)
		if allPassed(rs) {
			t.Fatalf("a skipped declared test passed AllTestsPass: %+v", rs)
		}
		if !strings.Contains(reasons(rs), "TestR2Skipped") {
			t.Errorf("the reason does not name the skipped test: %s", reasons(rs))
		}
	})
	t.Run("one declared test fails", func(t *testing.T) {
		p := with(map[string]string{"tests/fail_test.go": "package tests\n\nimport \"testing\"\n\nfunc TestTestsFailsHere(t *testing.T) { t.Fatal(\"no\") }\n"})
		rs := checks.AllTestsPass(r, p)
		if allPassed(rs) {
			t.Fatalf("a failing declared test passed AllTestsPass: %+v", rs)
		}
		if !strings.Contains(reasons(rs), "TestTestsFailsHere") {
			t.Errorf("the reason does not name the failing test: %s", reasons(rs))
		}
	})
	t.Run("a declared test in a package that does not build", func(t *testing.T) {
		p := with(map[string]string{"tests/broken/broken_test.go": "package broken\n\nimport \"testing\"\n\nfunc TestBrokenBuild(t *testing.T) { undefinedName() }\n"})
		if rs := checks.AllTestsPass(r, p); allPassed(rs) {
			t.Fatalf("a declared test whose package does not build passed AllTestsPass: %+v", rs)
		}
	})
}

// The declared-test rule, judged through AllTestsPass against fixed -json events: the run passes
// when only TestX and Test have pass events, so nothing else is declared; it fails when TestX's or
// Test's event is missing, so both are.
func TestDeclaredTestsAreTopLevelTestFunctions(t *testing.T) {
	needGo(t)
	files := map[string]string{
		"tests/kinds_test.go": `package tests

import "testing"

func TestX(t *testing.T) {
	t.Run("sub", func(t *testing.T) {})
}

func Test(t *testing.T) {}

// Not a test by the go test rule: a lower-case letter follows Test.
func Testx(t *testing.T) {}

func TestMain(m *testing.M) { m.Run() }

func BenchmarkX(b *testing.B) {}

func ExampleX() {}

func FuzzX(f *testing.F) { f.Fuzz(func(t *testing.T, b []byte) {}) }
`,
		// A test of the code step's own, outside tests/** and review/**: run, never declared.
		"internal/app/app_test.go": "package app\n\nimport \"testing\"\n\nfunc TestOwnOfTheCodeStep(t *testing.T) {}\n",
		"internal/app/app.go":      "package app\n",
		// Under a folder whose name only starts with tests: not tests/**.
		"testsx/other_test.go": "package testsx\n\nimport \"testing\"\n\nfunc TestNotUnderTests(t *testing.T) {}\n",
	}
	p := dtProject(t, files)
	real := checks.NewGoRunner()
	run := func(events string) []checks.CheckResult {
		return checks.AllTestsPass(dtFakeGo{res: checks.GoResult{Output: events}, real: real}, p)
	}

	if rs := run(dtEvents("tests.TestX:pass", "tests.Test:pass")); !allPassed(rs) {
		t.Errorf("TestX and Test passed and nothing else is declared, and AllTestsPass failed: %+v", rs)
	}
	rs := run(dtEvents("tests.Test:pass"))
	if allPassed(rs) || !strings.Contains(reasons(rs), "TestX") {
		t.Errorf("TestX has no event and AllTestsPass passed or does not name it: %+v", rs)
	}
	if rs := run(dtEvents("tests.TestX:pass")); allPassed(rs) {
		t.Errorf("Test (the bare name) has no event and AllTestsPass passed: %+v", rs)
	}
	// The subtest's own events do not stand in for its parent.
	if rs := run(dtEvents("tests.TestX/sub:pass", "tests.Test:pass")); allPassed(rs) {
		t.Errorf("only TestX's subtest has an event, and AllTestsPass passed: %+v", rs)
	}
}

func TestTestsFailAgainstStubNeedsAnEventForEveryDeclaredTest(t *testing.T) {
	r := dtRealGo(t)
	const failing = "package tests\n\nimport \"testing\"\n\nfunc TestStubFailsOne(t *testing.T) { t.Fatal(\"stub returns 501\") }\n"
	const passing = "package tests\n\nimport \"testing\"\n\nfunc TestStubPassesTwo(t *testing.T) {}\n"

	t.Run("every declared test has an event and one fails", func(t *testing.T) {
		if rs := checks.TestsFailAgainstStub(r, dtProject(t, map[string]string{"tests/a_test.go": failing})); !allPassed(rs) {
			t.Fatalf("a failing declared test against the stub was rejected: %+v", rs)
		}
	})
	t.Run("one fails and another passes", func(t *testing.T) {
		p := dtProject(t, map[string]string{"tests/a_test.go": failing, "tests/b_test.go": passing})
		if rs := checks.TestsFailAgainstStub(r, p); !allPassed(rs) {
			t.Fatalf("one failing and one passing declared test were rejected: %+v", rs)
		}
	})
	t.Run("every declared test passes", func(t *testing.T) {
		if rs := checks.TestsFailAgainstStub(r, dtProject(t, map[string]string{"tests/b_test.go": passing})); allPassed(rs) {
			t.Fatalf("tests that all pass against the stub passed: %+v", rs)
		}
	})
	t.Run("the probe", func(t *testing.T) {
		p := dtProbeProject(t, nil)
		dtProbeIsReal(t, r, p, "./tests/...")
		rs := checks.TestsFailAgainstStub(r, p)
		if allPassed(rs) {
			t.Fatalf("the probe passed TestsFailAgainstStub: %+v", rs)
		}
		for _, name := range []string{"TestProbeAnswerOne", "TestProbeAnswerTwo"} {
			if !strings.Contains(reasons(rs), name) {
				t.Errorf("the reason does not name the vanished test %s: %s", name, reasons(rs))
			}
		}
	})
	t.Run("a declared test with no event while another fails", func(t *testing.T) {
		// tests/vanish's tests import the probe's package and vanish; tests/a's test fails.
		p := dtProject(t, map[string]string{
			"internal/app/app.go":        dtProbeApp,
			"tests/vanish/probe_test.go": strings.Replace(dtProbeTests, "package tests", "package vanish", 1),
			"tests/a_test.go":            failing,
		})
		rs := checks.TestsFailAgainstStub(r, p)
		if allPassed(rs) {
			t.Fatalf("a declared test vanished while another failed, and the check passed: %+v", rs)
		}
		if !strings.Contains(reasons(rs), "TestProbeAnswerOne") {
			t.Errorf("the reason does not name the vanished test: %s", reasons(rs))
		}
	})
}

// A project whose only tests are the code step's own, outside tests/** and review/**, passing:
// it declares no test, so nothing shows the slice's rules hold.
func TestAllTestsPassFailsWithNoDeclaredTest(t *testing.T) {
	r := dtRealGo(t)
	p := dtProject(t, map[string]string{
		"internal/app/app.go":      "package app\n\nfunc Answer() int { return 42 }\n",
		"internal/app/app_test.go": "package app\n\nimport \"testing\"\n\nfunc TestOwnAnswer(t *testing.T) {\n\tif Answer() != 42 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n",
		"tests/helpers.go":         "package tests\n\n// Help is a helper with no test.\nfunc Help() int { return 1 }\n",
	})
	if rs := checks.AllTestsPass(r, p); allPassed(rs) {
		t.Fatalf("a project with no declared test passed AllTestsPass: %+v", rs)
	}
}

// skip is a result event: one declared test skipped and another failing passes the check.
func TestSkipIsAResultEventForTestsFailAgainstStub(t *testing.T) {
	r := dtRealGo(t)
	p := dtProject(t, map[string]string{
		"tests/a_test.go": "package tests\n\nimport \"testing\"\n\nfunc TestStubSkipped(t *testing.T) { t.Skip(\"later\") }\n\nfunc TestStubFails(t *testing.T) { t.Fatal(\"stub returns 501\") }\n",
	})
	if rs := checks.TestsFailAgainstStub(r, p); !allPassed(rs) {
		t.Fatalf("a skipped declared test beside a failing one was rejected: %+v", rs)
	}
}

// Two packages declare TestX: tests/alpha's runs and passes, tests/beta's vanishes (its package
// imports the probe's). A pass event for alpha's TestX does not count for beta's.
func TestDeclaredTestsAreMatchedByImportPath(t *testing.T) {
	r := dtRealGo(t)
	p := dtProject(t, map[string]string{
		"internal/app/app.go":   dtProbeApp,
		"tests/alpha/x_test.go": "package alpha\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
		"tests/beta/x_test.go":  "package beta\n\nimport (\n\t\"testing\"\n\n\t\"example.com/dt/internal/app\"\n)\n\nfunc TestX(t *testing.T) {\n\tif app.Answer() != 42 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n",
	})
	dtProbeIsReal(t, r, p, "./...")
	rs := checks.AllTestsPass(r, p)
	if allPassed(rs) {
		t.Fatalf("tests/beta's TestX never ran, and alpha's pass counted for it: %+v", rs)
	}
	if !strings.Contains(reasons(rs), dtModule+"/tests/beta") || !strings.Contains(reasons(rs), "TestX") {
		t.Errorf("the reason does not name beta's TestX by its import path %s/tests/beta: %s", dtModule, reasons(rs))
	}
}

// EveryExampleBound's fallback, used when features/slice.feature cannot be counted (here it does
// not exist, and the tests print no godog summary): every declared test under tests/** has a
// result event. skip and fail count; a vanished test does not.
func TestEveryExampleBoundFallbackReadsEvents(t *testing.T) {
	r := dtRealGo(t)
	t.Run("the probe", func(t *testing.T) {
		p := dtProbeProject(t, nil)
		dtProbeIsReal(t, r, p, "./tests/...")
		rs := checks.GodogStrict(r, p)
		if allPassed(rs) {
			t.Fatalf("the probe passed EveryExampleBound's fallback: no declared test ran: %+v", rs)
		}
		if !strings.Contains(reasons(rs), "TestProbeAnswerOne") {
			t.Errorf("the reason does not name the vanished test: %s", reasons(rs))
		}
	})
	t.Run("a skipped declared test counts as a result event", func(t *testing.T) {
		p := dtProject(t, map[string]string{"tests/a_test.go": "package tests\n\nimport \"testing\"\n\nfunc TestBoundSkipped(t *testing.T) { t.Skip(\"later\") }\n"})
		if rs := checks.GodogStrict(r, p); !allPassed(rs) {
			t.Fatalf("a skipped declared test is a result event, and the fallback failed: %+v", rs)
		}
	})
	t.Run("a failing declared test counts as a result event", func(t *testing.T) {
		// The tests step's tests fail against the stub; EveryExampleBound judges binding, not results.
		p := dtProject(t, map[string]string{"tests/a_test.go": "package tests\n\nimport \"testing\"\n\nfunc TestBoundFails(t *testing.T) { t.Fatal(\"stub returns 501\") }\n"})
		if rs := checks.GodogStrict(r, p); !allPassed(rs) {
			t.Fatalf("a failing declared test is a result event, and the fallback failed: %+v", rs)
		}
	})
}

// The witness rule with -json (ADR 0039 and D37): a confirmed witness stays confirmed; a witness
// whose package init exits 0 under a test binary leaves no event and no usable profile, so the run
// cannot be judged: FindingsCarryEvidence fails, and NoBlockingFindingOpen does not report the
// finding open (confirmed).
func TestWitnessBaselineHoldsWithJSON(t *testing.T) {
	r := dtRealGo(t)
	fs := []checks.Finding{witFinding("R-1", witGreet, 23, witR1, witT1)}

	t.Run("a confirmed witness stays confirmed", func(t *testing.T) {
		p := witProject(t)
		if rs := checks.FindingsCarryEvidence(r, p, witReview(t, p), fs); !allPassed(rs) {
			t.Errorf("FindingsCarryEvidence failed on a witness that fails having run the line: %+v", rs)
		}
		rs := checks.NoBlockingFindingOpen(r, p, witReview(t, p), fs)
		if allPassed(rs) || !strings.Contains(reasons(rs), "R-1") {
			t.Errorf("the confirmed witness no longer blocks: %+v", rs)
		}
	})
	t.Run("a witness whose package init exits under a test binary", func(t *testing.T) {
		p := witProject(t)
		writeFile(t, filepath.Join(p, "internal", "greet", "quit.go"), "package greet\n\nimport (\n\t\"os\"\n\t\"strings\"\n)\n\nfunc init() {\n\tif strings.HasSuffix(os.Args[0], \".test\") {\n\t\tos.Exit(0)\n\t}\n}\n")
		rv := witReview(t, p)
		if rs := checks.FindingsCarryEvidence(r, p, rv, fs); allPassed(rs) {
			t.Errorf("the witness never ran and left no profile, and FindingsCarryEvidence passed: %+v", rs)
		}
		rs := checks.NoBlockingFindingOpen(r, p, rv, fs)
		for _, c := range rs {
			if c.Part == "findings" || strings.Contains(c.Reason, "is open") {
				t.Errorf("a witness that never ran confirmed its finding: %+v", c)
			}
		}
	})
}

// hasFlag reports whether args holds the flag exactly.
func hasFlag(args []string, flag string) bool {
	return contains(args, flag)
}

// Every go test of a test check carries -json and -count=1: AllTestsPass (./...),
// TestsFailAgainstStub and EveryExampleBound (./tests/...), and both witness runs.
func TestEveryTestCheckRunsCount1AndJSON(t *testing.T) {
	needGo(t)
	p := dtProject(t, map[string]string{"tests/a_test.go": "package tests\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n"})
	for name, c := range map[string]struct {
		run  func(r checks.GoRunner) []checks.CheckResult
		pkgs string
	}{
		"AllTestsPass":         {func(r checks.GoRunner) []checks.CheckResult { return checks.AllTestsPass(r, p) }, "./..."},
		"TestsFailAgainstStub": {func(r checks.GoRunner) []checks.CheckResult { return checks.TestsFailAgainstStub(r, p) }, "./tests/..."},
		"EveryExampleBound":    {func(r checks.GoRunner) []checks.CheckResult { return checks.GodogStrict(r, p) }, "./tests/..."},
	} {
		t.Run(name, func(t *testing.T) {
			var calls [][]string
			c.run(dtFakeGo{res: checks.GoResult{Output: dtEvents("tests.TestA:pass")}, real: checks.NewGoRunner(), args: &calls})
			n := 0
			for _, a := range calls {
				if len(a) == 0 || a[0] != "test" {
					continue
				}
				n++
				for _, want := range []string{"-json", "-count=1", c.pkgs} {
					if !hasFlag(a, want) {
						t.Errorf("go test without %s: %v", want, a)
					}
				}
			}
			if n == 0 {
				t.Fatalf("%s ran no go test: %v", name, calls)
			}
		})
	}
	t.Run("both witness runs", func(t *testing.T) {
		wp := witProject(t)
		var calls []witCall
		r := recordingGo{checks.NewGoRunner(), &calls}
		checks.NoBlockingFindingOpen(r, wp, witReview(t, wp), []checks.Finding{witFinding("R-1", witGreet, 23, witR1, witT1)})
		var witness, baseline bool
		for _, c := range calls {
			if len(c.args) == 0 || c.args[0] != "test" {
				continue
			}
			for _, want := range []string{"-json", "-count=1"} {
				if !hasFlag(c.args, want) {
					t.Errorf("witness go test without %s: %v", want, c.args)
				}
			}
			switch run, _ := flagValue(c.args, "-run"); run {
			case "^" + witT1 + "$":
				witness = true
			case "^$":
				baseline = true
			}
		}
		if !witness || !baseline {
			t.Errorf("want the witness run and its baseline, got %v", calls)
		}
	})
}

// The converted fakes of the committed tests (owner, 2026-11-15, T093): what `go test -json`
// writes for the summary text they fed before.

// jsonTestRun is the `go test -json` output of package pkg running one top-level test that ends
// with action (pass, fail or skip), having printed lines (godog's summary, for example): the
// test's run and output events, the "--- PASS/FAIL/SKIP" line the testing package prints, its
// result event, then the package's summary output and end event, as test2json writes them.
func jsonTestRun(pkg, test, action string, lines ...string) string {
	var out []string
	add := func(e map[string]any) {
		b, _ := json.Marshal(e)
		out = append(out, string(b))
	}
	add(map[string]any{"Action": "start", "Package": pkg})
	add(map[string]any{"Action": "run", "Package": pkg, "Test": test})
	add(map[string]any{"Action": "output", "Package": pkg, "Test": test, "Output": "=== RUN   " + test + "\n"})
	for _, l := range lines {
		add(map[string]any{"Action": "output", "Package": pkg, "Test": test, "Output": l + "\n"})
	}
	add(map[string]any{"Action": "output", "Package": pkg, "Test": test, "Output": "--- " + strings.ToUpper(action) + ": " + test + " (0.00s)\n"})
	add(map[string]any{"Action": action, "Package": pkg, "Test": test, "Elapsed": 0})
	if action == "fail" {
		add(map[string]any{"Action": "output", "Package": pkg, "Output": "FAIL\n"})
		add(map[string]any{"Action": "output", "Package": pkg, "Output": "FAIL\t" + pkg + "\t0.002s\n"})
		add(map[string]any{"Action": "fail", "Package": pkg, "Elapsed": 0.002})
	} else {
		add(map[string]any{"Action": "output", "Package": pkg, "Output": "PASS\n"})
		add(map[string]any{"Action": "output", "Package": pkg, "Output": "ok  \t" + pkg + "\t0.002s\n"})
		add(map[string]any{"Action": "pass", "Package": pkg, "Elapsed": 0.002})
	}
	return strings.Join(out, "\n") + "\n"
}

// jsonBuildFailed is the `go test -json` output of package pkg whose test binary does not build.
func jsonBuildFailed(pkg string) string {
	ip := pkg + " [" + pkg + ".test]"
	var out []string
	add := func(e map[string]any) {
		b, _ := json.Marshal(e)
		out = append(out, string(b))
	}
	add(map[string]any{"ImportPath": ip, "Action": "build-output", "Output": "# " + ip + "\n"})
	add(map[string]any{"ImportPath": ip, "Action": "build-output", "Output": "tests/a_test.go:5:33: undefined: undefinedName\n"})
	add(map[string]any{"ImportPath": ip, "Action": "build-fail"})
	add(map[string]any{"Action": "start", "Package": pkg})
	add(map[string]any{"Action": "output", "Package": pkg, "Output": "FAIL\t" + pkg + " [build failed]\n"})
	add(map[string]any{"Action": "fail", "Package": pkg, "Elapsed": 0, "FailedBuild": ip})
	return strings.Join(out, "\n") + "\n"
}
