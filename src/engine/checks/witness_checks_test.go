// T075: failing tests, by a separate author, for the witness rule of ADR 0039 (research D35; check
// contract runCheck: FindingsCarryEvidence, NoBlockingFindingOpen; EarlierStep: TestsUnchanged).
// They replace ADR 0031's rerun of the rule's tests: only the witness test a blocking finding
// names is run, with coverage, and the finding blocks only when that test fails having run the
// finding's line, less a baseline run of the same package with `-run '^$'`.
//
// ASSUMED SHAPES (the implementer may rename the fields, not drop what they carry):
//
//	type Witness struct {
//		File string `json:"file"` // review/r<n>/<name>_test.go
//		Test string `json:"test"` // TestR<n>..., a test function File declares
//	}
//	// Finding gains: Witness *Witness `json:"witness,omitempty"`
//
//	// Review is what the witness rule reads beyond the project and the findings.
//	type Review struct {
//		Before  Tree     // the project before the attempt (CheckRequest.before): a witness here is not new
//		Own     []string // the judged step's writable entries (review/**): a witness is one of its files
//		Fixable []string // the writable entries of the step whose outputs hold `code`
//	}
//
//	func FindingsCarryEvidence(r GoRunner, project string, rv Review, findings []Finding) []CheckResult
//	func NoBlockingFindingOpen(r GoRunner, project string, rv Review, findings []Finding) []CheckResult
//
//	// WitnessesUnchangedOf is TestsUnchanged's witness rule (check name "TestsUnchanged"): no path
//	// of b matching witnessGlobs is changed or removed in a; a path of a matching them that b does
//	// not hold fails unless mayAdd (the judged step is the review step). Each failing result names
//	// the path.
//	func WitnessesUnchangedOf(b, a Tree, witnessGlobs []string, mayAdd bool) []CheckResult
//
// Fixture: testdata/witness/, a module with no dependency (example.com/w). internal/greet/greet.go
// line 11 runs in init, line 15 from a package-level initialiser, line 21 only for an empty name,
// line 23 for any other name; review/r<n>/ holds the witnesses described in each file.
package checks_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/checks"
)

const (
	witGreet = "internal/greet/greet.go"
	witOther = "internal/other/other.go"
)

var (
	witCodeWritable   = []string{"internal/**", "cmd/**"}
	witReviewWritable = []string{"review/**"}
)

// witProject copies testdata/witness into a new folder, so a file the checks leave behind shows.
func witProject(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src := filepath.Join("testdata", "witness")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// witTree is every regular file of dir, by project path, as a Tree; drop leaves out the paths it
// matches.
func witTree(t *testing.T, dir string, drop ...string) checks.Tree {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if checks.MatchGlob(drop, rel) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[rel] = b
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return checks.HashTree(files)
}

// witReview is the review step's view of project: every witness is new (before holds no file
// under review/), the review writes review/**, and the code step writes internal/** and cmd/**.
func witReview(t *testing.T, project string) checks.Review {
	return checks.Review{Before: witTree(t, project, "review/**"), Own: witReviewWritable, Fixable: witCodeWritable}
}

func witFinding(rule, file string, line int, wfile, wtest string) checks.Finding {
	f := checks.Finding{Rule: rule, File: file, Line: line, Text: "the greeting leaves the name out", Blocking: true}
	if wfile != "" {
		f.Witness = &checks.Witness{File: wfile, Test: wtest}
	}
	return f
}

const (
	witR1 = "review/r1/name_test.go"
	witT1 = "TestR1NameLeftOut"
)

// witBoth runs both checks; carry and open are whether FindingsCarryEvidence and
// NoBlockingFindingOpen must pass.
func witBoth(t *testing.T, r checks.GoRunner, project string, rv checks.Review, fs []checks.Finding, carry, open bool) (string, string) {
	t.Helper()
	c := checks.FindingsCarryEvidence(r, project, rv, fs)
	if allPassed(c) != carry {
		t.Errorf("FindingsCarryEvidence passed=%v, want %v: %+v", allPassed(c), carry, c)
	}
	o := checks.NoBlockingFindingOpen(r, project, rv, fs)
	if allPassed(o) != open {
		t.Errorf("NoBlockingFindingOpen passed=%v, want %v: %+v", allPassed(o), open, o)
	}
	return reasons(c), reasons(o)
}

// A witness that fails having run the finding's line confirms it: the finding blocks, naming its
// rule and file, and FindingsCarryEvidence passes (the finding carries its evidence).
func TestWitnessThatFailsHavingRunTheLineBlocks(t *testing.T) {
	r := needGo(t)
	p := witProject(t)
	_, open := witBoth(t, r, p, witReview(t, p), []checks.Finding{witFinding("R-1", witGreet, 23, witR1, witT1)}, true, false)
	if !strings.Contains(open, "R-1") || !strings.Contains(open, witGreet) {
		t.Errorf("the reason names neither R-1 nor %s: %s", witGreet, open)
	}
	if abs, _ := filepath.Abs(p); strings.Contains(open, abs) || strings.Contains(open, p) {
		t.Errorf("the reason names the host path: %s", open)
	}
	// One confirmed finding among suggestions blocks, and only it is named.
	_, open = witBoth(t, r, p, witReview(t, p), []checks.Finding{
		witFinding("R-2", witGreet, 23, "review/r2/print_test.go", "TestR2PrintsOnly"),
		witFinding("R-1", witGreet, 23, witR1, witT1),
	}, true, false)
	if strings.Contains(open, "R-2") {
		t.Errorf("only R-1 is confirmed, but the reason names R-2: %s", open)
	}
}

// Every other blocking finding is a suggestion: both checks pass.
func TestWitnessSuggestions(t *testing.T) {
	r := needGo(t)
	p := witProject(t)
	rv := witReview(t, p)
	cases := []struct {
		name string
		f    checks.Finding
	}{
		{"a witness that only prints the location and fails without running it",
			witFinding("R-2", witGreet, 23, "review/r2/print_test.go", "TestR2PrintsOnly")},
		{"a witness that fails before it reaches the line (an unconditional t.Fatal first)",
			witFinding("R-3", witGreet, 23, "review/r3/first_test.go", "TestR3FailsFirst")},
		{"an always-failing witness naming a line init runs (the -run '^$' baseline covers it)",
			witFinding("R-4", witGreet, 11, "review/r4/init_test.go", "TestR4Always")},
		{"an always-failing witness naming a line a package-level initialiser runs",
			witFinding("R-4", witGreet, 15, "review/r4/init_test.go", "TestR4Always")},
		{"an always-failing witness naming a line its TestMain runs",
			witFinding("R-5", witGreet, 23, "review/r5/main_test.go", "TestR5Always")},
		{"a witness that passes having run the line",
			witFinding("R-6", witGreet, 21, "review/r6/pass_test.go", "TestR6Passes")},
		{"a witness that fails having run only another line",
			witFinding("R-1", witGreet, 21, witR1, witT1)},
		{"a witness that fails having run only another file",
			witFinding("R-1", witOther, 5, witR1, witT1)},
		{"a finding on generated code, which the code step cannot write",
			witFinding("R-1", "gen/gen.go", 5, witR1, witT1)},
		{"a finding on a derived-test helper, which the code step cannot write",
			witFinding("R-1", "tests/helpers.go", 5, witR1, witT1)},
		{"a witness in another rule's folder",
			witFinding("R-2", witGreet, 23, witR1, witT1)},
		{"a witness naming a test its file does not declare (another file of the folder does)",
			witFinding("R-1", witGreet, 23, witR1, "TestR1Elsewhere")},
		{"a witness test named for another rule (TestR12 is not TestR1)",
			witFinding("R-1", witGreet, 23, witR1, "TestR12Other")},
		{"a blocking finding with no witness, whose rule's derived test fails naming the line",
			witFinding("R-7", witGreet, 23, "", "")},
		{"a blocking finding with no witness", witFinding("R-1", witGreet, 23, "", "")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			witBoth(t, r, p, rv, []checks.Finding{c.f}, true, true)
		})
	}
	t.Run("a witness that is not new (in before)", func(t *testing.T) {
		old := witReview(t, p)
		old.Before = witTree(t, p)
		witBoth(t, r, p, old, []checks.Finding{witFinding("R-1", witGreet, 23, witR1, witT1)}, true, true)
	})
	t.Run("a witness that is not one of the judged step's own files", func(t *testing.T) {
		code := witReview(t, p)
		code.Own = witCodeWritable
		witBoth(t, r, p, code, []checks.Finding{witFinding("R-1", witGreet, 23, witR1, witT1)}, true, true)
	})
	t.Run("a finding that is not blocking", func(t *testing.T) {
		f := witFinding("R-1", witGreet, 23, witR1, witT1)
		f.Blocking = false
		witBoth(t, r, p, rv, []checks.Finding{f}, true, true)
	})
}

// A witness that does not build cannot be judged: both checks fail, never pass by default.
func TestWitnessThatDoesNotBuildFails(t *testing.T) {
	r := needGo(t)
	p := witProject(t)
	if err := os.MkdirAll(filepath.Join(p, "review", "r9"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "review", "r9", "broken_test.go"), []byte("package r9\n\nimport \"testing\"\n\nfunc TestR9Broken(t *testing.T) { undefinedName() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	carry, open := witBoth(t, r, p, witReview(t, p), []checks.Finding{witFinding("R-9", witGreet, 23, "review/r9/broken_test.go", "TestR9Broken")}, false, false)
	if abs, _ := filepath.Abs(p); strings.Contains(carry+open, abs) {
		t.Errorf("the reasons name the host path: %s %s", carry, open)
	}
}

// The existing evidence rule stays: a blocking finding whose file the project does not hold fails
// FindingsCarryEvidence, witness or not.
func TestWitnessFindingOnAMissingFileFailsEvidence(t *testing.T) {
	r := needGo(t)
	p := witProject(t)
	rs := checks.FindingsCarryEvidence(r, p, witReview(t, p), []checks.Finding{witFinding("R-1", "internal/missing.go", 3, witR1, witT1)})
	if allPassed(rs) || !strings.Contains(reasons(rs), "internal/missing.go") {
		t.Fatalf("a blocking finding on a file the project does not hold passed, or the reason does not name it: %+v", rs)
	}
}

// recordingGo runs the real toolchain and records every go command's folder and arguments.
type recordingGo struct {
	inner checks.GoRunner
	calls *[]witCall
}

type witCall struct {
	dir  string
	args []string
}

func (g recordingGo) Go(dir string, args ...string) (checks.GoResult, error) {
	*g.calls = append(*g.calls, witCall{dir, append([]string(nil), args...)})
	return g.inner.Go(dir, args...)
}

// flagValue is the value of -name in args, given as -name=v or -name v.
func flagValue(args []string, name string) (string, bool) {
	for i, a := range args {
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v, true
		}
		if a == name && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

// The run is `go test -json -count=1 -covermode=set -coverpkg=./... -coverprofile=<outside the
// project> -run '^<test>$' ./review/r<n>/` and its baseline with `-run '^$'`; no other test of the
// project runs, the derived tests and the other witnesses included; the project is left as it
// was. Offline: the runner's environment (checks.OfflineGo) and a module with no dependency.
func TestWitnessRunIsTheWitnessAloneWithCoverageOutsideTheProject(t *testing.T) {
	needGo(t)
	p := witProject(t)
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	before := witTree(t, p)
	for _, check := range []string{"FindingsCarryEvidence", "NoBlockingFindingOpen"} {
		t.Run(check, func(t *testing.T) {
			var calls []witCall
			r := recordingGo{checks.NewGoRunner(), &calls}
			fs := []checks.Finding{witFinding("R-1", witGreet, 23, witR1, witT1), witFinding("R-7", witGreet, 23, "", "")}
			if check == "FindingsCarryEvidence" {
				checks.FindingsCarryEvidence(r, p, witReview(t, p), fs)
			} else {
				checks.NoBlockingFindingOpen(r, p, witReview(t, p), fs)
			}
			var witness, baseline bool
			for _, c := range calls {
				if len(c.args) == 0 || c.args[0] != "test" {
					continue
				}
				pkgs := []string{}
				for i := 1; i < len(c.args); i++ {
					if a := c.args[i]; !strings.HasPrefix(a, "-") {
						if prev := c.args[i-1]; prev == "-run" || prev == "-coverprofile" || prev == "-coverpkg" || prev == "-covermode" {
							continue
						}
						pkgs = append(pkgs, a)
					}
				}
				if len(pkgs) != 1 || strings.TrimSuffix(pkgs[0], "/") != "./review/r1" {
					t.Errorf("a go test ran packages %v, not ./review/r1/ alone: %v", pkgs, c.args)
				}
				for _, want := range []string{"-json", "-count=1"} {
					if !strings.Contains(" "+strings.Join(c.args, " ")+" ", " "+want+" ") {
						t.Errorf("go test without %s: %v", want, c.args)
					}
				}
				if v, _ := flagValue(c.args, "-covermode"); v != "set" {
					t.Errorf("-covermode is %q, not set: %v", v, c.args)
				}
				if v, _ := flagValue(c.args, "-coverpkg"); v != "./..." {
					t.Errorf("-coverpkg is %q, not ./...: %v", v, c.args)
				}
				prof, ok := flagValue(c.args, "-coverprofile")
				if !ok || !filepath.IsAbs(prof) {
					t.Errorf("-coverprofile %q is not an absolute path outside the project: %v", prof, c.args)
				} else if rel, err := filepath.Rel(abs, prof); err == nil && !strings.HasPrefix(rel, "..") {
					t.Errorf("the coverage profile %s is inside the project: %v", prof, c.args)
				}
				switch run, _ := flagValue(c.args, "-run"); run {
				case "^" + witT1 + "$":
					witness = true
				case "^$":
					baseline = true
				default:
					t.Errorf("go test ran -run %q, neither the witness nor the baseline: %v", run, c.args)
				}
			}
			if !witness || !baseline {
				t.Errorf("want the witness run and its baseline (-run '^$'), got %v", calls)
			}
			after := witTree(t, p)
			for path := range after {
				if _, ok := before[path]; !ok {
					t.Errorf("the check left %s in the project", path)
				}
			}
			for path, h := range before {
				if after[path] != h {
					t.Errorf("the check changed or removed %s", path)
				}
			}
		})
	}
}

// The same findings over the same code give the same verdict.
func TestWitnessVerdictRepeats(t *testing.T) {
	r := needGo(t)
	p := witProject(t)
	fs := []checks.Finding{witFinding("R-1", witGreet, 23, witR1, witT1), witFinding("R-4", witGreet, 15, "review/r4/init_test.go", "TestR4Always")}
	a := checks.NoBlockingFindingOpen(r, p, witReview(t, p), fs)
	b := checks.NoBlockingFindingOpen(r, p, witReview(t, p), fs)
	if allPassed(a) != allPassed(b) || reasons(a) != reasons(b) {
		t.Fatalf("two runs differ: %+v vs %+v", a, b)
	}
}

// TestsUnchanged's witness rule: no step changes or removes a witness in before; only the review
// step adds one.
func TestWitnessesUnchanged(t *testing.T) {
	const old, added = "review/r1/old_test.go", "review/r2/new_test.go"
	b := checks.Tree{old: "h-old", "internal/greet/greet.go": "h-g"}
	with := func(edit func(a checks.Tree)) checks.Tree {
		a := checks.Tree{}
		for k, v := range b {
			a[k] = v
		}
		edit(a)
		return a
	}
	cases := []struct {
		name   string
		a      checks.Tree
		mayAdd bool
		pass   bool
		names  string
	}{
		{"nothing under review/ differs, a code file does", with(func(a checks.Tree) { a["internal/greet/greet.go"] = "h-g2" }), false, true, ""},
		{"the review changes a witness", with(func(a checks.Tree) { a[old] = "h-new" }), true, false, old},
		{"the review removes a witness", with(func(a checks.Tree) { delete(a, old) }), true, false, old},
		{"the fix step changes a witness", with(func(a checks.Tree) { a[old] = "h-new" }), false, false, old},
		{"the fix step removes a witness", with(func(a checks.Tree) { delete(a, old) }), false, false, old},
		{"a later review round adds one", with(func(a checks.Tree) { a[added] = "h-a" }), true, true, ""},
		{"a step other than the review adds one", with(func(a checks.Tree) { a[added] = "h-a" }), false, false, added},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rs := checks.WitnessesUnchangedOf(b, c.a, witReviewWritable, c.mayAdd)
			if allPassed(rs) != c.pass {
				t.Fatalf("passed=%v, want %v: %+v", allPassed(rs), c.pass, rs)
			}
			for _, r := range rs {
				if r.Check != "TestsUnchanged" {
					t.Errorf("the result is %q's, not TestsUnchanged's", r.Check)
				}
			}
			if c.names != "" && !strings.Contains(reasons(rs), c.names) {
				t.Errorf("the reason does not name %s: %s", c.names, reasons(rs))
			}
		})
	}
}
