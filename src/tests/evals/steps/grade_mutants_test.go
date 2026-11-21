package steps

// Self-tests of MutantsKilled (feature 004, US2; README "Mutants"): no model, no Docker. The grader
// passes tests that kill every mutant, fails tests that let one live, never judges a mutant when
// the tests do not pass against the reference implementation, never runs when an earlier grader
// failed, never reaches the agent, and never changes the agent's project (the constitution: checks
// prove they can fail). The outcome matrix runs on a fake runner; a few tests use the host's go.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/agentic-platform/engine/checks"
)

const (
	mutBaseline   = "baseline"
	reasonNoBuild = "the tests do not build against the reference implementation"
	reasonNoPass  = "the tests do not pass against the reference implementation"
)

// mutantNames lists the table's names in order.
func mutantNames() []string {
	var out []string
	for _, m := range Mutants() {
		out = append(out, m.Name)
	}
	return out
}

// requireMutCounts asserts what every result of a run that reached its mutants must hold.
func requireMutCounts(t *testing.T, r Result) *Counts {
	t.Helper()
	if r.Grader != GMutants {
		t.Fatalf("the grader is %q, want %s", r.Grader, GMutants)
	}
	c := r.Counts
	if c == nil {
		t.Fatalf("no counts: %+v", r)
	}
	if c.Total != len(Mutants()) || c.Total != 8 || c.Passed < 0 || c.Passed > c.Total || len(c.Failed) != c.Total-c.Passed {
		t.Fatalf("inconsistent counts %+v for %d mutants", *c, len(Mutants()))
	}
	if c.Failed == nil {
		t.Fatalf("Failed is nil, want an empty list (it is JSON null otherwise): %+v", *c)
	}
	if !sort.StringsAreSorted(c.Failed) {
		t.Fatalf("Failed is not sorted: %q", c.Failed)
	}
	known := map[string]bool{}
	for _, n := range mutantNames() {
		known[n] = true
	}
	seen := map[string]bool{}
	for _, f := range failedOf(c) {
		if !known[f] || seen[f] {
			t.Fatalf("Failed names %q, which is no mutant or is named twice (%q)", f, c.Failed)
		}
		seen[f] = true
	}
	if r.Passed != (c.Passed == c.Total) {
		t.Fatalf("Passed is %v with %d of %d mutants killed", r.Passed, c.Passed, c.Total)
	}
	want := fmt.Sprintf("%d of %d mutants killed", c.Passed, c.Total)
	if len(c.Failed) > 0 {
		want += "; survived: " + strings.Join(c.Failed, ", ")
	}
	if r.Reason != want {
		t.Fatalf("the reason is %q, want %q", r.Reason, want)
	}
	return c
}

// requireStopped asserts a result that stopped before judging any mutant.
func requireStopped(t *testing.T, r Result, prefix string) {
	t.Helper()
	if r.Grader != GMutants || r.Passed || r.Counts != nil || !strings.HasPrefix(r.Reason, prefix) {
		t.Fatalf("want a failed %s with no counts and a reason starting %q, got %+v (counts %v)", GMutants, prefix, r, r.Counts)
	}
}

// ---- the fake runner ----

// mutFake is a checks.GoRunner that builds and runs nothing: it tells each build's variant from the
// -overlay it was given (the baseline's replacement of internal/app/app.go is the reference text,
// a mutant's is the reference with its one line replaced), writes a binary per package folder
// under the built package, and answers every run from the scripts below by variant name and the
// package folder's last element ("tests", "more").
type mutFake struct {
	mu   sync.Mutex
	ref  string
	muts []Mutant
	// build answers a build by variant name; nil answers success. write decides, per package
	// folder, whether the binary is written.
	build func(variant string) (write func(pkg string) bool, res checks.GoResult, err error)
	// run answers a run by variant name and package folder; nil: every run is an error.
	run func(variant, pkg string) (checks.GoResult, error)

	outs     map[string]string // -o folder -> variant
	builds   []string          // variant names in build order
	pkgArgs  []string          // the ./<pkg>/... argument of each build
	runs     []string          // "variant/pkg" per run call
	problems []string
}

func newMutFake(t *testing.T) *mutFake {
	t.Helper()
	return &mutFake{ref: Ref("impl")[implFile], muts: Mutants(), outs: map[string]string{}}
}

func (f *mutFake) variantOf(overlayJSON string) string {
	var o struct{ Replace map[string]string }
	if err := json.Unmarshal([]byte(overlayJSON), &o); err != nil || len(o.Replace) != 1 {
		f.problems = append(f.problems, "the overlay is not one replacement: "+overlayJSON)
		return "?"
	}
	for target, repl := range o.Replace {
		if !strings.HasSuffix(filepath.ToSlash(target), "/"+implFile) {
			f.problems = append(f.problems, "the overlay replaces "+target+", not "+implFile)
			return "?"
		}
		b, err := os.ReadFile(repl)
		if err != nil {
			f.problems = append(f.problems, "the replacement cannot be read: "+err.Error())
			return "?"
		}
		if string(b) == f.ref {
			return mutBaseline
		}
		for _, m := range f.muts {
			if string(b) == strings.Replace(f.ref, m.from, m.to, 1) {
				return m.Name
			}
		}
		f.problems = append(f.problems, "the replacement is neither the reference nor a mutant of it")
	}
	return "?"
}

func (f *mutFake) Go(dir string, args ...string) (checks.GoResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case len(args) > 0 && args[0] == "test":
		variant, out := mutBaseline+"(no overlay)", ""
		for i, a := range args {
			if p, ok := strings.CutPrefix(a, "-overlay="); ok {
				b, err := os.ReadFile(p)
				if err != nil {
					return checks.GoResult{}, err
				}
				variant = f.variantOf(string(b))
			}
			if a == "-o" && i+1 < len(args) {
				out = filepath.Clean(args[i+1])
			}
		}
		pkg := vtPkg(args)
		f.outs[out] = variant
		f.builds = append(f.builds, variant)
		f.pkgArgs = append(f.pkgArgs, pkg)
		write := func(string) bool { return true }
		if f.build != nil {
			w, res, err := f.build(variant)
			if err != nil || res.ExitCode != 0 {
				return res, err
			}
			if w != nil {
				write = w
			}
		}
		if err := os.MkdirAll(out, 0o750); err != nil {
			return checks.GoResult{}, err
		}
		for _, d := range vtTestFolders(dir, pkg) {
			base := filepath.Base(d)
			if write(base) {
				if err := os.WriteFile(filepath.Join(out, base+".test"), []byte("binary"), 0o600); err != nil {
					return checks.GoResult{}, err
				}
			}
		}
		return checks.GoResult{}, nil
	case len(args) > 1 && args[0] == "tool" && args[1] == "test2json":
		bin := ""
		for i, a := range args {
			if a == "-t" && i+1 < len(args) {
				bin = args[i+1]
			}
		}
		variant := f.outs[filepath.Clean(filepath.Dir(bin))]
		pkg := strings.TrimSuffix(filepath.Base(bin), ".test")
		f.runs = append(f.runs, variant+"/"+pkg)
		if f.run == nil {
			return checks.GoResult{}, fmt.Errorf("fake: no run script")
		}
		return f.run(variant, pkg)
	}
	return checks.GoResult{}, fmt.Errorf("fake: unexpected call %v", args)
}

func (f *mutFake) env() Env { return Env{Runner: f} }

// Answers of one test binary.
func mutPass() checks.GoResult {
	return vtEvents(vtCat(vtTest("TestFeatures", "pass"), vtPackage("pass"))...)
}

func mutFail() checks.GoResult {
	return vtEvents(vtCat([]vtEv{
		{"run", "TestFeatures", ""},
		{"run", "TestFeatures/Scenario", ""},
		{"output", "TestFeatures/Scenario", "--- FAIL: TestFeatures/Scenario (0.00s)\n"},
		{"fail", "TestFeatures/Scenario", ""},
		{"output", "TestFeatures", "--- FAIL: TestFeatures (0.00s)\n"},
		{"fail", "TestFeatures", ""},
	}, vtPackage("fail"))...)
}

// mutPanic: a test that started, printed a panic and never ended.
func mutPanic() checks.GoResult {
	return vtEvents(
		vtEv{"run", "TestFeatures", ""},
		vtEv{"output", "TestFeatures", "panic: runtime error: index out of range [0] with length 0\n"},
		vtEv{"fail", "", ""})
}

// mutSubFailOnly: only a sub-test reports a failure, and the top-level test ends passed.
func mutSubFailOnly() checks.GoResult {
	return vtEvents(
		vtEv{"run", "TestFeatures", ""},
		vtEv{"run", "TestFeatures/Scenario", ""},
		vtEv{"fail", "TestFeatures/Scenario", ""},
		vtEv{"pass", "TestFeatures", ""})
}

var errTimedOut = fmt.Errorf("timed out after 3m0s")

// reference passes and every mutant fails, but for the exceptions.
func (f *mutFake) script(exceptions map[string]func() (checks.GoResult, error)) {
	f.run = func(variant, pkg string) (checks.GoResult, error) {
		if g, ok := exceptions[variant+"/"+pkg]; ok {
			return g()
		}
		if g, ok := exceptions[variant]; ok {
			return g()
		}
		if variant == mutBaseline {
			return mutPass(), nil
		}
		return mutFail(), nil
	}
}

func answer(r checks.GoResult, err error) func() (checks.GoResult, error) {
	return func() (checks.GoResult, error) { return r, err }
}

// mutProject writes a project with one test package under tests/ (and the agent's own stub of the
// seam), plus extra files.
func mutProject(t *testing.T, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		"go.mod":                 "module example.com/svc\n\ngo 1.26\n",
		implFile:                 "package app\n\n// the agent's own seam; the grader replaces it\nfunc New() int { return 0 }\n",
		"tests/a_test.go":        "package tests\n",
		"tests/data.txt":         "data\n",
		"features/slice.feature": "Feature: x\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	root := t.TempDir()
	for p, c := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// A sanity check of the table the other tests rely on.
func TestGraderMutantsKilledTable(t *testing.T) {
	want := []string{"R-1/newest-first", "R-1/drops-first", "R-2/from-last", "R-2/zero-based",
		"R-2/always-one", "R-2/status-200", "R-3/first-byte", "R-3/accepts-empty"}
	if got := mutantNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the mutants are %q, want %q", got, want)
	}
	if GMutants != "MutantsKilled" {
		t.Fatalf("GMutants is %q", GMutants)
	}
}

// ---- real toolchain ----

// (a) and (h): the reference tests kill every mutant; the agent's project is what it was.
func TestGraderMutantsKilledReference(t *testing.T) {
	e, b := testEnv(t), base(t)
	proj := dir(t, testsGood(b))
	before := snapshot(t, proj)

	r := e.MutantsKilled(proj)
	if !r.Passed {
		t.Fatalf("the reference tests did not kill every mutant: %+v", r)
	}
	c := requireMutCounts(t, r)
	if c.Total != 8 || c.Passed != 8 || len(c.Failed) != 0 || r.Reason != "8 of 8 mutants killed" {
		t.Fatalf("counts %+v, reason %q, want 8 of 8 and nothing survived", *c, r.Reason)
	}
	if j, _ := json.Marshal(r.Counts); string(j) != `{"total":8,"passed":8,"failed":[]}` {
		t.Fatalf("the counts are %s as JSON", j)
	}
	requireSame(t, proj, before, "after MutantsKilled")
}

// (b) FR-005, FR-006, SC-002: tests with one step that checks nothing still bind every example and
// still fail against the stub, so the earlier graders pass them, and only the mutants show it.
func TestGraderMutantsKilledWeakTests(t *testing.T) {
	e, b := testEnv(t), base(t)
	f := testsGood(b)
	f["tests/features_test.go"] = replaceOnce(t, f["tests/features_test.go"],
		"\t\tif got := fmt.Sprint(w.body[\"id\"]); got != s {\n\t\t\treturn fmt.Errorf(\"the note has the id %s, want %s\", got, s)\n\t\t}\n", "")
	before, proj := dir(t, b), dir(t, f)
	pSnap := snapshot(t, proj)

	rs := e.GradeTests(before, proj)
	for _, g := range []string{GScope, GDeps, GTestsBuild, GBound, GFailsStub} {
		if r := verdict(t, rs, g); !r.Passed {
			t.Fatalf("the weak tests fail %s, so they prove nothing about %s: %s", g, GMutants, r.Reason)
		}
	}
	if Passed(rs) {
		t.Fatalf("the weak tests pass every grader: %+v", rs)
	}
	mustFail(t, rs, GMutants)
	if last := rs[len(rs)-1]; last.Grader != GMutants {
		t.Fatalf("the last result is %s, want %s", last.Grader, GMutants)
	}
	r := verdict(t, rs, GMutants)
	c := requireMutCounts(t, r)
	if c.Passed == c.Total {
		t.Fatalf("every mutant was killed: %+v", r)
	}
	requireFailedNames(t, c, "R-2/always-one", "R-2/zero-based")
	for _, f := range c.Failed {
		if strings.Contains(f, "(") {
			t.Fatalf("a survivor carries a suffix though it ran: %q", c.Failed)
		}
	}
	requireSame(t, proj, pSnap, "after GradeTests")
}

// (c) FR-008: tests that fail against the reference implementation get that reason and no counts.
func TestGraderMutantsKilledFailsAgainstReference(t *testing.T) {
	e, b := testEnv(t), base(t)
	f := testsGood(b)
	f["tests/features_test.go"] = replaceOnce(t, f["tests/features_test.go"], "w.status != n", "w.status == n")
	proj := dir(t, f)
	before := snapshot(t, proj)
	requireStopped(t, e.MutantsKilled(proj), reasonNoPass)
	requireSame(t, proj, before, "after MutantsKilled")
}

// (f) and (h): GradeTests ends with MutantsKilled, passed, for every tests case's clean fixture, and
// leaves the agent's project and the project it started from as they were.
func TestGraderMutantsKilledInGradeTests(t *testing.T) {
	e := testEnv(t)
	n := 0
	for _, c := range Cases() {
		if c.Agent != AgentTests {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			before, err := c.Before()
			if err != nil {
				t.Fatal(err)
			}
			bd, ad := dir(t, before), dir(t, Overlay(before, Ref("tests")))
			bSnap, aSnap := snapshot(t, bd), snapshot(t, ad)
			rs := e.GradeTests(bd, ad)
			mustPass(t, rs)
			last := rs[len(rs)-1]
			if last.Grader != GMutants || !last.Passed {
				t.Fatalf("the last result is %+v, want a passed %s", last, GMutants)
			}
			if k := requireMutCounts(t, last); k.Passed != 8 {
				t.Fatalf("counts %+v", *k)
			}
			seen := 0
			for _, r := range rs {
				if r.Grader == GMutants {
					seen++
				}
			}
			if seen != 1 {
				t.Fatalf("%d %s results in %+v", seen, GMutants, rs)
			}
			requireSame(t, ad, aSnap, "after GradeTests")
			requireSame(t, bd, bSnap, "(the starting project) after GradeTests")
		})
	}
	if n < 2 {
		t.Fatalf("only %d tests cases", n)
	}
}

// (d) and (e3): MutantsKilled runs only when every earlier grader passed; the reason names the
// FIRST failing grader and no counts are given.
func TestGraderMutantsKilledFailFast(t *testing.T) {
	e, b := testEnv(t), base(t)
	before := dir(t, b)
	for _, tc := range []struct {
		name  string
		files Files
		first string
	}{
		{"the tests do not build (d)", Overlay(testsGood(b), Files{"tests/broken_test.go": "package tests\n\nfunc {\n"}), GTestsBuild},
		{"an example is not bound (e3)", Files{}, GBound},
		{"a file outside the scope", Overlay(testsGood(b), Files{"docs/notes.md": "x\n"}), GScope},
		{"a forbidden import", Overlay(testsGood(b), Files{"tests/dep_test.go": "package tests\n\nimport _ \"os/exec\"\n"}), GDeps},
		{"two graders fail: the first is named", Overlay(testsGood(b),
			Files{"docs/notes.md": "x\n", "tests/broken_test.go": "package tests\n\nfunc {\n"}), GScope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.files
			if tc.first == GBound {
				f = testsGood(b)
				f["tests/features_test.go"] = replaceOnce(t, f["tests/features_test.go"], "the note has the id (", "the note has the identifier (")
			}
			proj := dir(t, f)
			pSnap := snapshot(t, proj)
			rs := e.GradeTests(before, proj)
			var first string
			for _, r := range rs[:len(rs)-1] {
				if !r.Passed {
					first = r.Grader
					break
				}
			}
			if first != tc.first {
				t.Fatalf("the first failing grader is %q, the case expects %q (%v)", first, tc.first, Failures(rs))
			}
			last := rs[len(rs)-1]
			if want := "not run: " + tc.first + " failed"; last.Grader != GMutants || last.Passed || last.Reason != want || last.Counts != nil {
				t.Fatalf("the last result is %+v, want %s failed with reason %q and no counts", last, GMutants, want)
			}
			requireSame(t, proj, pSnap, "after GradeTests")
		})
	}
}

// ---- (g) FR-009: the agent never sees a mutant ----

// leaks lists the needles text contains.
func leaks(text string, needles []string) []string {
	var out []string
	for _, n := range needles {
		if strings.Contains(text, n) {
			out = append(out, n)
		}
	}
	return out
}

func mutantNeedles() []string {
	var needles []string
	for _, m := range Mutants() {
		needles = append(needles, strings.TrimSpace(m.to), m.marker, m.Name)
	}
	return needles
}

// The check itself fails on planted leaks.
func TestGraderMutantsNotInCasesCheck(t *testing.T) {
	needles := mutantNeedles()
	for _, m := range Mutants() {
		for _, text := range []string{"x\n" + m.to + "y", "// " + m.Name, "code " + m.marker + " code"} {
			if len(leaks(text, needles)) == 0 {
				t.Errorf("a planted leak of %q was not found in %q", m.Name, text)
			}
		}
	}
	if l := leaks(Ref("impl")[implFile]+Ref("tests")["tests/features_test.go"], needles); len(l) > 0 {
		// the reference implementation holds the unchanged lines, never a replacement
		t.Errorf("the clean reference code holds %q", l)
	}
}

// No tests case's starting files or message holds a mutant's replacement line, its marker or its name.
// (The review cases plant R-2 and R-3 on purpose and are not in scope.)
func TestGraderMutantsNotInCases(t *testing.T) {
	e := testEnv(t)
	needles := mutantNeedles()
	n := 0
	for _, c := range Cases() {
		if c.Agent != AgentTests {
			continue
		}
		n++
		files, err := c.Before()
		if err != nil {
			t.Fatal(err)
		}
		for p, content := range files {
			if l := leaks(p, needles); len(l) > 0 {
				t.Errorf("%s: the path %s holds %q", c.Name, p, l)
			}
			if l := leaks(content, needles); len(l) > 0 {
				t.Errorf("%s: the file %s holds %q", c.Name, p, l)
			}
		}
		if l := leaks(Message(e.Steps[c.Agent], c.Slice, "/projects/p", files), needles); len(l) > 0 {
			t.Errorf("%s: the message holds %q", c.Name, l)
		}
	}
	if n < 2 {
		t.Fatalf("only %d tests cases", n)
	}
}

// ---- fake runner: what is built, what is run, what is counted ----

// Every package of tests/ is built against the reference and against each mutant, and nothing else
// is: the baseline first-class, each mutant exactly once, the agent's project untouched.
func TestGraderMutantsKilledFakeAllKilled(t *testing.T) {
	f := newMutFake(t)
	f.script(map[string]func() (checks.GoResult, error){
		"R-2/from-last":  answer(mutPanic(), nil),       // a panic is a kill
		"R-2/zero-based": answer(mutSubFailOnly(), nil), // so is a failed sub-test
	})
	proj := mutProject(t, map[string]string{
		"other/c_test.go":           "package other\n", // outside tests/: never built
		"internal/app/app_test.go":  "package app\n",
		".apeval-overlays/keep.txt": "the agent's own file, named like the grader's scratch folder\n",
		"tests/testdata/x_test.go":  "package x\n", // Go ignores testdata/
	})
	before := vtSnapshot(t, proj)

	r := f.env().MutantsKilled(proj)
	if len(f.problems) > 0 {
		t.Fatalf("the grader gave the build a bad overlay: %q", f.problems)
	}
	if !r.Passed {
		t.Fatalf("every mutant was killed but the result is %+v", r)
	}
	if c := requireMutCounts(t, r); c.Passed != 8 || r.Reason != "8 of 8 mutants killed" {
		t.Fatalf("%+v", r)
	}
	// 1 baseline + 8 mutants, each built once, from ./tests/...
	want := append([]string{mutBaseline}, mutantNames()...)
	sorted := append([]string(nil), f.builds...)
	sort.Strings(sorted)
	sort.Strings(want)
	if !reflect.DeepEqual(sorted, want) {
		t.Fatalf("the builds were %q, want the baseline and each mutant once: %q", f.builds, want)
	}
	for _, p := range f.pkgArgs {
		if p != "tests" {
			t.Fatalf("a build of ./%s/..., want only ./tests/...: %q", p, f.pkgArgs)
		}
	}
	for _, run := range f.runs {
		if !strings.HasSuffix(run, "/tests") {
			t.Fatalf("a run of %q: only the package under tests/ has a binary", run)
		}
	}
	if after := vtSnapshot(t, proj); !reflect.DeepEqual(before, after) {
		t.Fatalf("the agent's project changed:\nbefore %v\nafter  %v", before, after)
	}
}

// (e) FR-007, FR-013: a mutant is not killed by a run that timed out, one that cannot start, or a
// build that fails; each is named with its reason, sorted, and the total stays 8.
func TestGraderMutantsKilledFakeSurvivors(t *testing.T) {
	f := newMutFake(t)
	f.script(map[string]func() (checks.GoResult, error){
		"R-1/newest-first":  answer(checks.GoResult{}, errTimedOut),                      // timed out
		"R-1/drops-first":   answer(checks.GoResult{}, fmt.Errorf("exec: no such file")), // cannot start
		"R-2/always-one":    answer(mutPass(), nil),                                      // survives, ran
		"R-2/status-200":    answer(checks.GoResult{}, fmt.Errorf("timed out: the run exceeded 3m")),
		"R-3/accepts-empty": answer(vtEvents(), nil), // no event at all
	})
	f.build = func(v string) (func(string) bool, checks.GoResult, error) {
		switch v {
		case "R-3/first-byte":
			return nil, checks.GoResult{ExitCode: 1, Output: "tests/a_test.go:3: undefined"}, nil
		case "R-2/from-last":
			return nil, checks.GoResult{}, fmt.Errorf("the build could not start")
		}
		return nil, checks.GoResult{}, nil
	}
	proj := mutProject(t, nil)
	before := vtSnapshot(t, proj)

	r := f.env().MutantsKilled(proj)
	if r.Passed {
		t.Fatalf("surviving mutants passed: %+v", r)
	}
	c := requireMutCounts(t, r)
	want := []string{
		"R-1/drops-first (did not run)",
		"R-1/newest-first (timed out)",
		"R-2/always-one",
		"R-2/from-last (did not run)",
		"R-2/status-200 (timed out)",
		"R-3/accepts-empty (did not run)",
		"R-3/first-byte (did not run)",
	}
	if c.Total != 8 || c.Passed != 1 || !reflect.DeepEqual(c.Failed, want) {
		t.Fatalf("counts %+v, want 1 of 8 killed and survivors %q", *c, want)
	}
	if after := vtSnapshot(t, proj); !reflect.DeepEqual(before, after) {
		t.Fatalf("the agent's project changed")
	}
}

// A survivor's reason by how its binary ended: passed (bare), skipped everything, timed out.
func TestGraderMutantsKilledFakeSuffixes(t *testing.T) {
	skipped := vtEvents(vtEv{"run", "TestFeatures", ""}, vtEv{"skip", "TestFeatures", ""})
	for _, tc := range []struct {
		name string
		ans  func() (checks.GoResult, error)
		want string
	}{
		{"passed", answer(mutPass(), nil), "R-2/always-one"},
		{"timed out", answer(checks.GoResult{}, errTimedOut), "R-2/always-one (timed out)"},
		{"error", answer(checks.GoResult{}, fmt.Errorf("boom")), "R-2/always-one (did not run)"},
		{"skipped", answer(skipped, nil), "R-2/always-one (did not run)"},
		{"a run error that only mentions a time-out", answer(checks.GoResult{}, fmt.Errorf("the run timed out")), "R-2/always-one (did not run)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMutFake(t)
			f.script(map[string]func() (checks.GoResult, error){"R-2/always-one": tc.ans})
			c := requireMutCounts(t, f.env().MutantsKilled(mutProject(t, nil)))
			if c.Passed != 7 || !reflect.DeepEqual(c.Failed, []string{tc.want}) {
				t.Fatalf("counts %+v, want 7 of 8 and %q", *c, tc.want)
			}
		})
	}
}

// The tests must pass against the reference implementation, whatever the mutants do (FR-008): no
// counts, the reason says so, and the answers for the mutants (here all kills) are not used.
func TestGraderMutantsKilledFakeBaseline(t *testing.T) {
	cases := []struct {
		name   string
		build  func(v string) (func(string) bool, checks.GoResult, error)
		run    func() (checks.GoResult, error)
		prefix string
	}{
		{"a test fails", nil, answer(mutFail(), nil), reasonNoPass},
		{"a test panics", nil, answer(mutPanic(), nil), reasonNoPass},
		{"only a sub-test fails", nil, answer(mutSubFailOnly(), nil), reasonNoPass},
		{"the run times out", nil, answer(checks.GoResult{}, errTimedOut), reasonNoPass},
		{"the run cannot start", nil, answer(checks.GoResult{}, fmt.Errorf("boom")), reasonNoPass},
		{"no test runs", nil, answer(vtEvents(), nil), reasonNoPass},
		{"every test is skipped", nil, answer(vtEvents(vtEv{"run", "TestFeatures", ""}, vtEv{"skip", "TestFeatures", ""}), nil), reasonNoPass},
		{"no binary is written", func(v string) (func(string) bool, checks.GoResult, error) {
			return func(string) bool { return v != mutBaseline }, checks.GoResult{}, nil
		}, nil, reasonNoPass},
		{"it does not build", func(v string) (func(string) bool, checks.GoResult, error) {
			if v == mutBaseline {
				return nil, checks.GoResult{ExitCode: 1, Output: "tests/a_test.go:3:1: syntax error"}, nil
			}
			return nil, checks.GoResult{}, nil
		}, nil, reasonNoBuild},
		{"the build cannot start", func(v string) (func(string) bool, checks.GoResult, error) {
			if v == mutBaseline {
				return nil, checks.GoResult{}, fmt.Errorf("the build could not start")
			}
			return nil, checks.GoResult{}, nil
		}, nil, reasonNoBuild},
		{"it does not build, and nothing builds", func(string) (func(string) bool, checks.GoResult, error) {
			return nil, checks.GoResult{ExitCode: 2, Output: "no required module provides package"}, nil
		}, nil, reasonNoBuild},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newMutFake(t)
			f.build = tc.build
			f.run = func(variant, _ string) (checks.GoResult, error) {
				if variant != mutBaseline {
					return mutFail(), nil // every mutant "killed": not to be counted
				}
				if tc.run == nil {
					return mutPass(), nil
				}
				return tc.run()
			}
			proj := mutProject(t, nil)
			before := vtSnapshot(t, proj)
			requireStopped(t, f.env().MutantsKilled(proj), tc.prefix)
			if after := vtSnapshot(t, proj); !reflect.DeepEqual(before, after) {
				t.Fatalf("the agent's project changed")
			}
		})
	}

	t.Run("a project with no test package", func(t *testing.T) {
		f := newMutFake(t)
		f.run = func(string, string) (checks.GoResult, error) { return mutPass(), nil }
		proj := mutProject(t, nil)
		if err := os.Remove(filepath.Join(proj, "tests", "a_test.go")); err != nil {
			t.Fatal(err)
		}
		r := f.env().MutantsKilled(proj)
		if r.Grader != GMutants || r.Passed || r.Counts != nil || r.Reason == "" {
			t.Fatalf("a project with no tests passed or has counts: %+v", r)
		}
		if !strings.HasPrefix(r.Reason, reasonNoPass) && !strings.HasPrefix(r.Reason, reasonNoBuild) {
			t.Fatalf("the reason %q says neither that the tests do not build nor that they do not pass", r.Reason)
		}
	})
	t.Run("a missing folder", func(t *testing.T) {
		for _, p := range []string{filepath.Join(t.TempDir(), "absent"), ""} {
			f := newMutFake(t)
			f.script(nil)
			r := f.env().MutantsKilled(p)
			if r.Grader != GMutants || r.Passed || r.Reason == "" || r.Counts != nil {
				t.Fatalf("project %q: %+v", p, r)
			}
		}
	})
}

// (e2) Tests in two packages under tests/: a mutant is killed when a binary of either reports a
// failed test, even if the other passed, did not run or timed out; the reference must pass in both.
func TestGraderMutantsKilledFakeTwoPackages(t *testing.T) {
	extra := map[string]string{"tests/more/more_test.go": "package more\n"}

	t.Run("one package of two fails on a mutant", func(t *testing.T) {
		f := newMutFake(t)
		f.run = func(v, pkg string) (checks.GoResult, error) {
			switch {
			case v == mutBaseline:
				return mutPass(), nil
			case v == "R-1/newest-first" && pkg == "more", v == "R-1/drops-first" && pkg == "tests",
				v == "R-2/from-last" || v == "R-2/zero-based" || v == "R-2/always-one" || v == "R-2/status-200" || v == "R-3/accepts-empty":
				return mutFail(), nil
			case v == "R-3/first-byte" && pkg == "more":
				return mutPanic(), nil
			}
			return mutPass(), nil // the other package of such a mutant passes
		}
		r := f.env().MutantsKilled(mutProject(t, extra))
		if c := requireMutCounts(t, r); !r.Passed || c.Passed != 8 {
			t.Fatalf("a mutant failed in one package only, but the result is %+v", r)
		}
		for _, p := range []string{"tests", "more"} {
			if !contains(f.runs, mutBaseline+"/"+p) {
				t.Fatalf("the reference did not run in %s: %q", p, f.runs)
			}
		}
	})
	t.Run("a failure wins over a time-out and a package that did not run", func(t *testing.T) {
		f := newMutFake(t)
		f.run = func(v, pkg string) (checks.GoResult, error) {
			switch v {
			case mutBaseline:
				return mutPass(), nil
			case "R-1/newest-first":
				if pkg == "more" {
					return checks.GoResult{}, errTimedOut
				}
				return mutFail(), nil
			case "R-1/drops-first":
				if pkg == "more" {
					return checks.GoResult{}, fmt.Errorf("boom")
				}
				return mutFail(), nil
			}
			return mutFail(), nil
		}
		if c := requireMutCounts(t, f.env().MutantsKilled(mutProject(t, extra))); c.Passed != 8 {
			t.Fatalf("%+v", *c)
		}
	})
	t.Run("no kill when one package passes and the other does not run or times out", func(t *testing.T) {
		f := newMutFake(t)
		f.run = func(v, pkg string) (checks.GoResult, error) {
			switch v {
			case mutBaseline:
				return mutPass(), nil
			case "R-1/newest-first": // passes in tests, did not run in more
				if pkg == "more" {
					return checks.GoResult{}, fmt.Errorf("boom")
				}
				return mutPass(), nil
			case "R-1/drops-first": // passes in tests, timed out in more
				if pkg == "more" {
					return checks.GoResult{}, errTimedOut
				}
				return mutPass(), nil
			case "R-2/always-one": // timed out in tests, did not run in more: the time-out is named
				if pkg == "more" {
					return checks.GoResult{}, fmt.Errorf("boom")
				}
				return checks.GoResult{}, errTimedOut
			case "R-2/zero-based": // passes in both
				return mutPass(), nil
			}
			return mutFail(), nil
		}
		c := requireMutCounts(t, f.env().MutantsKilled(mutProject(t, extra)))
		want := []string{"R-1/drops-first (timed out)", "R-1/newest-first (did not run)", "R-2/always-one (timed out)", "R-2/zero-based"}
		if c.Passed != 4 || !reflect.DeepEqual(c.Failed, want) {
			t.Fatalf("counts %+v, want 4 of 8 and survivors %q", *c, want)
		}
	})
	t.Run("the reference must pass in every package", func(t *testing.T) {
		f := newMutFake(t)
		f.run = func(v, pkg string) (checks.GoResult, error) {
			if pkg == "more" && v == mutBaseline {
				return mutFail(), nil
			}
			if v == mutBaseline {
				return mutPass(), nil
			}
			return mutFail(), nil
		}
		requireStopped(t, f.env().MutantsKilled(mutProject(t, extra)), reasonNoPass)
	})
	t.Run("the reference has a package with no binary", func(t *testing.T) {
		f := newMutFake(t)
		f.build = func(v string) (func(string) bool, checks.GoResult, error) {
			return func(pkg string) bool { return v != mutBaseline || pkg != "more" }, checks.GoResult{}, nil
		}
		f.script(nil)
		requireStopped(t, f.env().MutantsKilled(mutProject(t, extra)), reasonNoPass)
	})
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
