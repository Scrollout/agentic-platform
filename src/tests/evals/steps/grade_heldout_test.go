package steps

// Self-tests of HeldOutTestsPass (feature 004, US1; README "Held-out tests"): no model, no Docker.
// The grader fails on planted violations and passes the reference implementation, it never runs
// when an earlier grader failed, it never reaches the agent, and it never changes the agent's
// project (the constitution: checks prove they can fail).

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const heldOutFile = "testdata/heldout/heldout_test.go"

// heldOutNames returns the top-level tests of the held-out file, found by parsing it: the one place
// the tests below learn the names from.
func heldOutNames(t *testing.T) []string {
	t.Helper()
	src, err := data.ReadFile(heldOutFile)
	if err != nil {
		t.Fatalf("the held-out file cannot be read: %v", err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), heldOutFile, src, 0)
	if err != nil {
		t.Fatalf("the held-out file does not parse: %v", err)
	}
	var names []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") || fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
			continue
		}
		if star, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr); ok {
			if sel, ok := star.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "T" {
				names = append(names, fn.Name.Name)
			}
		}
	}
	sort.Strings(names)
	return names
}

// bareName drops the " (timed out)" or " (did not run)" suffix of a failed name.
func bareName(s string) string {
	if i := strings.Index(s, " ("); i >= 0 {
		return s[:i]
	}
	return s
}

// failedOf is Counts.Failed without suffixes.
func failedOf(c *Counts) []string {
	var out []string
	for _, f := range c.Failed {
		out = append(out, bareName(f))
	}
	return out
}

// requireCounts asserts what every result of a run that reached its tests must hold: Total is the
// file's tests, Passed within 0..Total, Failed sorted and Total-Passed long, Passed exactly when
// nothing failed, and the reason counts the same.
func requireCounts(t *testing.T, r Result, names []string) *Counts {
	t.Helper()
	if r.Grader != GHeldOut {
		t.Fatalf("the grader is %q, want %s", r.Grader, GHeldOut)
	}
	c := r.Counts
	if c == nil {
		t.Fatalf("no counts: %+v", r)
	}
	if c.Total != len(names) || c.Passed < 0 || c.Passed > c.Total || len(c.Failed) != c.Total-c.Passed {
		t.Fatalf("inconsistent counts %+v for %d held-out tests", *c, len(names))
	}
	if !sort.StringsAreSorted(c.Failed) {
		t.Fatalf("Failed is not sorted: %q", c.Failed)
	}
	known := map[string]bool{}
	for _, n := range names {
		known[n] = true
	}
	for _, f := range failedOf(c) {
		if !known[f] {
			t.Fatalf("Failed names %q, which is no held-out test (%q)", f, names)
		}
	}
	if r.Passed != (c.Passed == c.Total) {
		t.Fatalf("Passed is %v with %d of %d tests passed", r.Passed, c.Passed, c.Total)
	}
	if want := fmt.Sprintf("%d of %d held-out tests passed", c.Passed, c.Total); !strings.HasPrefix(r.Reason, want) {
		t.Fatalf("the reason %q does not start with %q", r.Reason, want)
	}
	for _, f := range c.Failed {
		if !strings.Contains(r.Reason, f) {
			t.Fatalf("the reason %q does not name the failed test %q", r.Reason, f)
		}
	}
	if !r.Passed && !strings.Contains(r.Reason, "; failed: ") {
		t.Fatalf("the reason %q has no \"; failed: \" list", r.Reason)
	}
	return c
}

func requireFailedNames(t *testing.T, c *Counts, want ...string) {
	t.Helper()
	got := map[string]bool{}
	for _, f := range failedOf(c) {
		got[f] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Fatalf("Failed %q does not name %s", c.Failed, w)
		}
	}
}

// replaceOnce replaces from by to in src and fails unless from occurs exactly once.
func replaceOnce(t *testing.T, src, from, to string) string {
	t.Helper()
	if n := strings.Count(src, from); n != 1 {
		t.Fatalf("%q occurs %d times in the reference implementation, want 1", from, n)
	}
	return strings.Replace(src, from, to, 1)
}

// snapshot reads a project folder.
func snapshot(t *testing.T, d string) Files {
	t.Helper()
	f, err := Read(d)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// requireSame fails when the project folder is not byte for byte what it was, or has another file.
func requireSame(t *testing.T, d string, before Files, what string) {
	t.Helper()
	after := snapshot(t, d)
	if !reflect.DeepEqual(before, after) {
		var diff []string
		for p, c := range after {
			if old, ok := before[p]; !ok {
				diff = append(diff, "added "+p)
			} else if old != c {
				diff = append(diff, "changed "+p)
			}
		}
		for p := range before {
			if _, ok := after[p]; !ok {
				diff = append(diff, "removed "+p)
			}
		}
		sort.Strings(diff)
		t.Fatalf("the project folder changed %s: %v", what, diff)
	}
}

// A sanity check of the parser the other tests rely on: the file's six tests.
func TestGraderHeldOutNames(t *testing.T) {
	names := heldOutNames(t)
	if len(names) != 6 {
		t.Fatalf("the held-out file has %d tests %q, want 6", len(names), names)
	}
	for _, n := range names {
		if !strings.HasPrefix(n, "TestR1_") && !strings.HasPrefix(n, "TestR2_") && !strings.HasPrefix(n, "TestR3_") {
			t.Errorf("%s does not start with its rule", n)
		}
	}
}

func TestGraderHeldOutReference(t *testing.T) {
	e, b := testEnv(t), base(t)
	names := heldOutNames(t)
	proj := dir(t, codeGood(b))
	before := snapshot(t, proj)

	r := e.HeldOutTestsPass(proj)
	if !r.Passed {
		t.Fatalf("the reference implementation failed the held-out tests: %+v", r)
	}
	c := requireCounts(t, r, names)
	if c.Total != 6 || c.Passed != 6 || len(c.Failed) != 0 {
		t.Fatalf("counts %+v, want 6 of 6 and nothing failed", *c)
	}
	if !strings.HasPrefix(r.Reason, "6 of 6 held-out tests passed") {
		t.Fatalf("reason %q", r.Reason)
	}
	// (g) the agent's project is what it was: not changed, no heldout/ folder added
	requireSame(t, proj, before, "after HeldOutTestsPass")
	for p := range snapshot(t, proj) {
		if strings.HasPrefix(p, "heldout/") {
			t.Fatalf("the grader left %s in the agent's project", p)
		}
	}
	// repeated calls give the same result
	if r2 := e.HeldOutTestsPass(proj); !reflect.DeepEqual(r, r2) {
		t.Fatalf("a second call answered differently:\n%+v\n%+v", r, r2)
	}
	requireSame(t, proj, before, "after the second call")
}

func TestGraderHeldOutFitted(t *testing.T) {
	e, b := testEnv(t), base(t)
	names := heldOutNames(t)
	fitted, err := data.ReadFile("testdata/planted/fitted/internal/app/app.go")
	if err != nil {
		t.Fatal(err)
	}
	f := Overlay(codeGood(b), Files{implFile: string(fitted)})
	proj := dir(t, f)
	before := snapshot(t, proj)

	// it passes every reference test, so every grader before the held-out tests passes it
	rs := e.GradeCode(dir(t, codeBase(b)), proj, ExampleSlice().Contract)
	if r := verdict(t, rs, GAllPass); !r.Passed {
		t.Fatalf("the fitted fixture fails the reference tests, so it proves nothing: %s", r.Reason)
	}
	via := rs[len(rs)-1]
	if via.Grader != GHeldOut || via.Passed || strings.HasPrefix(via.Reason, "not run") {
		t.Fatalf("GradeCode did not run and fail the held-out tests on the fitted fixture: %+v", via)
	}
	requireSame(t, proj, before, "after GradeCode")

	r := e.HeldOutTestsPass(proj)
	if r.Passed {
		t.Fatalf("the fitted fixture passed the held-out tests: %+v", r)
	}
	c := requireCounts(t, r, names)
	if c.Total != 6 || c.Passed >= 6 {
		t.Fatalf("counts %+v", *c)
	}
	requireFailedNames(t, c, "TestR1_EmptyList", "TestR1_ThreeInOrder")
	if r.Reason != via.Reason || !reflect.DeepEqual(via.Counts, r.Counts) {
		t.Fatalf("GradeCode and a direct call disagree:\n%+v\n%+v", via, r)
	}
	requireSame(t, proj, before, "after the direct call")
}

// Single-rule breaks of the reference implementation: each fails the held-out tests of its rule.
func TestGraderHeldOutBreaks(t *testing.T) {
	e, b := testEnv(t), base(t)
	names := heldOutNames(t)
	ref := Ref("impl")[implFile]
	for _, tc := range []struct {
		name, prefix, from, to string
	}{
		{"the list newest first (R-1)", "TestR1_", refListLine,
			"\tout := make([]gen.Note, len(s.notes)); for i, n := range s.notes { out[len(out)-1-i] = n }; write(w, http.StatusOK, out)\n"},
		{"ids from 0 (R-2)", "TestR2_", refIDLine, "\tn := gen.Note{Id: len(s.notes), Text: in.Text}\n"},
		{"empty text accepted (R-3)", "TestR3_", refEmptyLine,
			"\tif err := json.NewDecoder(r.Body).Decode(&in); err != nil {\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj := dir(t, Overlay(codeGood(b), Files{implFile: replaceOnce(t, ref, tc.from, tc.to)}))
			r := e.HeldOutTestsPass(proj)
			if r.Passed {
				t.Fatalf("a break of the reference passed: %+v", r)
			}
			c := requireCounts(t, r, names)
			named := false
			for _, f := range failedOf(c) {
				named = named || strings.HasPrefix(f, tc.prefix)
			}
			if !named {
				t.Fatalf("no held-out test %s* failed: %q", tc.prefix, c.Failed)
			}
		})
	}
}

// Partial and total failures: 5 of 6, 0 of 6, and a handler that panics.
func TestGraderHeldOutCounts(t *testing.T) {
	e, b := testEnv(t), base(t)
	names := heldOutNames(t)
	ref := Ref("impl")[implFile]

	t.Run("5 of 6: only the text kept as sent", func(t *testing.T) {
		src := replaceOnce(t, ref, "\t\"encoding/json\"\n", "\t\"encoding/json\"\n\t\"strings\"\n")
		src = replaceOnce(t, src, refIDLine,
			"\tn := gen.Note{Id: len(s.notes) + 1, Text: strings.Join(strings.Fields(in.Text), \" \")}\n")
		r := e.HeldOutTestsPass(dir(t, Overlay(codeGood(b), Files{implFile: src})))
		c := requireCounts(t, r, names)
		if r.Passed || c.Passed != 5 || len(c.Failed) != 1 || c.Failed[0] != "TestR2_TextKept" {
			t.Fatalf("want 5 of 6 with TestR2_TextKept failed, got %+v", r)
		}
		if want := "5 of 6 held-out tests passed; failed: TestR2_TextKept"; r.Reason != want {
			t.Fatalf("reason %q, want %q", r.Reason, want)
		}
	})
	t.Run("0 of 6: every answer is a 500", func(t *testing.T) {
		src := "package app\n\nimport (\n\t\"net/http\"\n\n\t\"example.com/service/gen\"\n)\n\n" +
			"type broken struct{}\n\n" +
			"func (broken) ListNotes(w http.ResponseWriter, r *http.Request)  { w.WriteHeader(500) }\n" +
			"func (broken) CreateNote(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }\n\n" +
			"func New() gen.ServerInterface { return broken{} }\n"
		r := e.HeldOutTestsPass(dir(t, Overlay(codeGood(b), Files{implFile: src})))
		c := requireCounts(t, r, names)
		if r.Passed || c.Passed != 0 || !reflect.DeepEqual(failedOf(c), names) {
			t.Fatalf("want 0 of 6 with every test named, got %+v (counts %+v)", r, *c)
		}
		if !strings.HasPrefix(r.Reason, "0 of 6 held-out tests passed; failed: ") {
			t.Fatalf("reason %q", r.Reason)
		}
	})
	t.Run("a panic in a handler fails the tests that reach it, not the grader", func(t *testing.T) {
		// R-3/first-byte: empty text panics in CreateNote
		src := replaceOnce(t, ref, refEmptyLine,
			"\tif err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Text[0] == 0 {\n")
		r := e.HeldOutTestsPass(dir(t, Overlay(codeGood(b), Files{implFile: src})))
		c := requireCounts(t, r, names)
		if r.Passed {
			t.Fatalf("a panicking handler passed: %+v", r)
		}
		requireFailedNames(t, c, "TestR3_RefusalIsAnError", "TestR3_RefusedNotListed")
	})
}

func TestGraderHeldOutDoesNotBuild(t *testing.T) {
	e, b := testEnv(t), base(t)
	for name, f := range map[string]Files{
		"a syntax error":    Overlay(codeGood(b), Files{implFile: "package app\n\nfunc {\n"}),
		"no implementation": codeBase(b), // the seam stub, which builds and answers nothing the tests expect
	} {
		t.Run(name, func(t *testing.T) {
			r := e.HeldOutTestsPass(dir(t, f))
			if r.Grader != GHeldOut || r.Passed || r.Reason == "" {
				t.Fatalf("an unbuildable or empty project passed or has no reason: %+v", r)
			}
			if name == "a syntax error" {
				if r.Counts != nil {
					t.Fatalf("counts on a build failure: %+v", *r.Counts)
				}
				if !strings.Contains(strings.ToLower(r.Reason), "build") {
					t.Fatalf("the reason does not say the code does not build: %q", r.Reason)
				}
			}
			if r.Counts != nil {
				requireCounts(t, r, heldOutNames(t)) // the stub built: then the counts must be sound
			}
		})
	}
	t.Run("a missing folder", func(t *testing.T) {
		for _, p := range []string{filepath.Join(t.TempDir(), "absent"), ""} {
			r := e.HeldOutTestsPass(p)
			if r.Grader != GHeldOut || r.Passed || r.Reason == "" || r.Counts != nil {
				t.Fatalf("project %q: %+v", p, r)
			}
		}
	})
}

// An agent that wrote its own heldout/heldout_test.go (its step's scope forbids it) gets the real
// tests: the grader's file replaces it, and the agent's file stays as it was.
func TestGraderHeldOutOwnFileWins(t *testing.T) {
	e, b := testEnv(t), base(t)
	names := heldOutNames(t)
	fitted, err := data.ReadFile("testdata/planted/fitted/internal/app/app.go")
	if err != nil {
		t.Fatal(err)
	}
	var fake strings.Builder
	fake.WriteString("package heldout_test\n\nimport \"testing\"\n\n")
	for _, n := range names {
		fmt.Fprintf(&fake, "func %s(t *testing.T) {}\n", n)
	}
	const agentFile = "heldout/heldout_test.go"
	proj := dir(t, Overlay(codeGood(b), Files{implFile: string(fitted), agentFile: fake.String()}))
	before := snapshot(t, proj)
	r := e.HeldOutTestsPass(proj)
	if r.Passed {
		t.Fatalf("an agent-written heldout file that passes everything was used: %+v", r)
	}
	c := requireCounts(t, r, names)
	requireFailedNames(t, c, "TestR1_EmptyList", "TestR1_ThreeInOrder")
	requireSame(t, proj, before, "after HeldOutTestsPass")
	if got := snapshot(t, proj)[agentFile]; got != fake.String() {
		t.Fatalf("the agent's own %s was changed", agentFile)
	}
}

// (e) and (g): GradeCode ends with HeldOutTestsPass, passed, for every code case's clean project,
// and leaves the agent's project and the project it started from as they were.
func TestGraderHeldOutInGradeCode(t *testing.T) {
	e := testEnv(t)
	names := heldOutNames(t)
	n := 0
	for _, c := range Cases() {
		if c.Agent != AgentCode {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			before, err := c.Before()
			if err != nil {
				t.Fatal(err)
			}
			bd, ad := dir(t, before), dir(t, Overlay(before, Ref("impl")))
			bSnap, aSnap := snapshot(t, bd), snapshot(t, ad)
			rs := e.GradeCode(bd, ad, c.Slice.Contract)
			mustPass(t, rs)
			last := rs[len(rs)-1]
			if last.Grader != GHeldOut || !last.Passed {
				t.Fatalf("the last result is %+v, want a passed %s", last, GHeldOut)
			}
			if k := requireCounts(t, last, names); k.Total != 6 || k.Passed != 6 {
				t.Fatalf("counts %+v", *k)
			}
			seen := 0
			for _, r := range rs {
				if r.Grader == GHeldOut {
					seen++
				}
			}
			if seen != 1 {
				t.Fatalf("%d held-out results in %+v", seen, rs)
			}
			requireSame(t, ad, aSnap, "after GradeCode")
			requireSame(t, bd, bSnap, "(the starting project) after GradeCode")
		})
	}
	if n < 2 {
		t.Fatalf("only %d code cases", n)
	}
}

// (f2) The held-out tests run only when every earlier grader passed; the reason names the FIRST
// failing grader, and no counts are given.
func TestGraderHeldOutFailFast(t *testing.T) {
	e, b := testEnv(t), base(t)
	before := dir(t, codeBase(b))
	approved := ExampleSlice().Contract
	ref := Ref("impl")[implFile]
	emptyList := replaceOnce(t, ref, refListLine, "\twrite(w, http.StatusOK, []gen.Note{})\n")

	for _, tc := range []struct {
		name     string
		files    Files
		approved string
		first    string
	}{
		{"the reference tests fail", Overlay(codeGood(b), Files{implFile: emptyList}), approved, GAllPass},
		{"a file outside the scope", Overlay(codeGood(b), Files{"docs/notes.md": "x\n"}), approved, GScope},
		{"two graders fail: the first is named", Overlay(codeGood(b), Files{"docs/notes.md": "x\n", implFile: emptyList}), approved, GScope},
		// everything else passes; the contract is not the approved one
		{"the contract differs from the approval", codeGood(b), approved + "x: 1\n", GContract},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj := dir(t, tc.files)
			pSnap := snapshot(t, proj)
			rs := e.GradeCode(before, proj, tc.approved)
			if len(rs) < 2 {
				t.Fatalf("results: %+v", rs)
			}
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
			want := "not run: " + tc.first + " failed"
			if last.Grader != GHeldOut || last.Passed || last.Reason != want || last.Counts != nil {
				t.Fatalf("the last result is %+v, want %s failed with reason %q and no counts", last, GHeldOut, want)
			}
			requireSame(t, proj, pSnap, "after GradeCode")
		})
	}
}

// (f) FR-004: no case's starting files and no message name the held-out file or its tests.
func TestGraderHeldOutNotInCases(t *testing.T) {
	e := testEnv(t)
	needles := append([]string{"heldout_test.go", "testdata/heldout", "heldout/"}, heldOutNames(t)...)
	check := func(where, text string) {
		for _, n := range needles {
			if strings.Contains(text, n) {
				t.Errorf("%s contains %q", where, n)
			}
		}
	}
	for _, c := range Cases() {
		files, err := c.Before()
		if err != nil {
			t.Fatal(err)
		}
		for p, content := range files {
			check(c.Name+": the path "+p, p)
			check(c.Name+": the file "+p, content)
		}
		check(c.Name+": the message", Message(e.Steps[c.Agent], c.Slice, "/projects/p", files))
	}
}

// reaches reports whether a mounted host folder is the held-out folder, one of its ancestors, or
// inside it: any of which would show the agent the held-out file.
func reaches(mount, heldDir string) bool {
	mount, heldDir = filepath.Clean(mount), filepath.Clean(heldDir)
	sep := string(filepath.Separator)
	return mount == heldDir || strings.HasPrefix(heldDir, strings.TrimSuffix(mount, sep)+sep) || strings.HasPrefix(mount, heldDir+sep)
}

// The check itself fails on a planted mount.
func TestGraderHeldOutMountCheck(t *testing.T) {
	held := "/r/src/tests/evals/steps/testdata/heldout"
	for _, m := range []string{held, held + "/x", "/r/src/tests/evals/steps/testdata", "/r/src/tests/evals/steps", "/r", "/"} {
		if !reaches(m, held) {
			t.Errorf("%s was not found to reach %s", m, held)
		}
	}
	for _, m := range []string{"/r/src/agents", "/tmp/work", "/r/src/tests/evals/steps/testdata/heldout2", "/r/src/tests/evals/steps/testdata/ref"} {
		if reaches(m, held) {
			t.Errorf("%s was wrongly found to reach %s", m, held)
		}
	}
}

// FR-004: the trial agent container mounts only the project and the agent definitions.
func TestGraderHeldOutNotMounted(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	file, err := filepath.EvalSymlinks(filepath.Join(wd, filepath.FromSlash(heldOutFile)))
	if err != nil {
		t.Fatalf("the held-out file is not where the grader expects it: %v", err)
	}
	// The repository may be the file system's root (CI's container mounts it at /).
	if rel, err := filepath.Rel(root, file); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("the held-out file %s is not in the repository %s", file, root)
	}
	heldDir := filepath.Dir(file)
	mounts := trialMounts(t.TempDir(), filepath.Join(root, "src", "agents"))
	if len(mounts) == 0 {
		t.Fatal("the trial container mounts nothing")
	}
	for _, m := range mounts {
		host, _, ok := strings.Cut(m, ":")
		if !ok || !filepath.IsAbs(host) {
			t.Fatalf("mount %q is not <absolute host folder>:<container path>", m)
		}
		if resolved, err := filepath.EvalSymlinks(host); err == nil {
			host = resolved
		}
		if reaches(host, heldDir) {
			t.Errorf("mount %q reaches the held-out folder %s", m, heldDir)
		}
	}
}
