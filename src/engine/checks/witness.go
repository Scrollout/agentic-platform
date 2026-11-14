package checks

// The review checks of ADR 0039 (research D35): a blocking finding is confirmed only by the new
// witness test it names, run alone with coverage, less a baseline run of the same package. The
// check contract's runCheck (FindingsCarryEvidence, NoBlockingFindingOpen) and EarlierStep
// (TestsUnchanged) state each rule; these are its one implementation.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

// Witness is the new witness test a blocking finding carries: a Go test file in the folder
// review/r<n>/ of the finding's rule R-<n>, and the test function TestR<n>... it declares.
type Witness struct {
	File string `json:"file"`
	Test string `json:"test"`
}

// Review is what the witness rule reads beyond the project and the findings.
type Review struct {
	// Before is the project before the attempt (the check request's `before`): a witness here is
	// not new.
	Before Tree
	// Own is the judged step's writable entries (review/**): a witness is one of its files.
	Own []string
	// Fixable is the writable entries of the step whose outputs hold `code` (the fix step): a
	// finding on a file it cannot change is a suggestion.
	Fixable []string
}

// ruleID is a rule `R-<n>`, its number captured.
var ruleID = regexp.MustCompile(`^R-([0-9]+)$`)

// witnessFor is the witness run a finding asks for: the package folder, and the reason it is a
// suggestion when it asks for none.
type witnessFor struct {
	dir        string // review/r<n>
	suggestion string // why the finding is a suggestion without running anything; "" to run
}

// eligible decides, without running anything, whether a blocking finding's witness is to be run:
// the finding names a file the fix step may change and a line, and its witness is a new file of
// the judged step in its rule's folder that declares the test it names. Anything else is a
// suggestion. A witness file that does not parse is run, so its build failure is reported.
func eligible(project string, rv Review, f Finding) witnessFor {
	if f.Witness == nil {
		return witnessFor{suggestion: "it names no witness test"}
	}
	m := ruleID.FindStringSubmatch(f.Rule)
	if m == nil {
		return witnessFor{suggestion: "its rule is not R-<n>"}
	}
	n := m[1]
	if !MatchGlob(rv.Fixable, f.File) {
		return witnessFor{suggestion: "its file " + f.File + " is not one the fix step may change"}
	}
	if !regularFile(project, f.File) || f.Line < 1 {
		return witnessFor{suggestion: "it names no line of a project file"}
	}
	w := *f.Witness
	dir := "review/r" + n
	switch {
	case path.Dir(w.File) != dir || !strings.HasSuffix(w.File, "_test.go"):
		return witnessFor{suggestion: fmt.Sprintf("its witness %s is not a _test.go file in its rule's folder %s/", w.File, dir)}
	case !MatchGlob(rv.Own, w.File):
		return witnessFor{suggestion: "its witness " + w.File + " is not one of the judged step's files"}
	case !regularFile(project, w.File):
		return witnessFor{suggestion: "its witness " + w.File + " is not a file of the project"}
	}
	if _, old := rv.Before[w.File]; old {
		return witnessFor{suggestion: "its witness " + w.File + " is not new (it was there before the attempt)"}
	}
	if !regexp.MustCompile(`^TestR` + n + `([^0-9]|$)`).MatchString(w.Test) {
		return witnessFor{suggestion: fmt.Sprintf("its witness test %q is not named TestR%s...", w.Test, n)}
	}
	if declared, parsed := declaresTest(project, w.File, w.Test); parsed && !declared {
		return witnessFor{suggestion: fmt.Sprintf("its witness %s declares no test %s", w.File, w.Test)}
	}
	return witnessFor{dir: dir}
}

func regularFile(project, p string) bool {
	if p == "" {
		return false
	}
	fi, err := os.Lstat(filepath.Join(project, filepath.FromSlash(p)))
	return err == nil && fi.Mode().IsRegular()
}

// declaresTest reports whether the Go file declares the top-level function name (no receiver);
// parsed is false when the file does not parse.
func declaresTest(project, file, name string) (declared, parsed bool) {
	af, err := parser.ParseFile(token.NewFileSet(), filepath.Join(project, filepath.FromSlash(file)), nil, parser.SkipObjectResolution)
	if err != nil {
		return false, false
	}
	for _, d := range af.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
			return true, true
		}
	}
	return false, true
}

// witnessRun runs the finding's witness test alone, offline, with coverage written outside the
// project, and, when it failed having run the finding's line, the baseline (`-run '^$'`), which
// holds what package initialisation runs. confirmed is whether the witness failed having run
// the line and the baseline does not cover it. A witness that does not build, a go command that
// fails to start or output that cannot be judged fails the check.
func witnessRun(check string, r GoRunner, project string, f Finding, dir string) (confirmed bool, bad *CheckResult) {
	failf := func(format string, a ...any) *CheckResult {
		c := fail(check, fmt.Sprintf("the witness %s of blocking finding %s ", f.Witness.File, findingName(f))+fmt.Sprintf(format, a...))
		return &c
	}
	gomod, err := os.ReadFile(filepath.Join(project, "go.mod"))
	if err != nil {
		return false, failf("cannot be run: go.mod cannot be read: %s", readError(err))
	}
	module := modfile.ModulePath(gomod)
	if module == "" {
		return false, failf("cannot be run: go.mod names no module")
	}
	tmp, err := os.MkdirTemp("", "witness-")
	if err != nil {
		return false, failf("cannot be run: no folder for its coverage profile: %v", err)
	}
	defer os.RemoveAll(tmp)
	importPath := module + "/" + f.File

	run := func(name, pattern string) (failed bool, covered bool, bad *CheckResult) {
		profile := filepath.Join(tmp, name+".out")
		res, b := runGo(check, r, project, "test", "-json", "-count=1", "-covermode=set", "-coverpkg=./...",
			"-coverprofile="+profile, "-run", pattern, "./"+dir+"/")
		if b != nil {
			return false, false, b
		}
		output := HidePaths(strings.ReplaceAll(res.Output, tmp, "<profile folder>"), project)
		ended, failedTests, buildFailed := readTestEvents(res.Output)
		if buildFailed || (res.ExitCode != 0 && len(failedTests) == 0) {
			return false, false, failf("does not build or cannot be run (go test exited %d): %s", res.ExitCode, excerpt(output, ".go:", "failed", "FAIL"))
		}
		if !ended {
			return false, false, failf("gave no go test result: %s", excerpt(output))
		}
		prof, err := os.ReadFile(profile)
		if err != nil {
			return false, false, failf("left no coverage profile, so the run cannot be judged")
		}
		cov, err := coversLine(prof, importPath, f.Line)
		if err != nil {
			return false, false, failf("left a coverage profile that cannot be read: %v", err)
		}
		return failedTests[f.Witness.Test], cov, nil
	}

	failed, covered, b := run("witness", "^"+regexp.QuoteMeta(f.Witness.Test)+"$")
	if b != nil || !failed || !covered {
		return false, b
	}
	_, inBaseline, b := run("baseline", "^$")
	if b != nil {
		return false, b
	}
	return !inBaseline, nil
}

// readTestEvents reads `go test -json` output: whether any package ended (an event with no test
// whose action is pass, fail or skip), the top-level tests that failed, and whether the package
// did not build.
func readTestEvents(out string) (ended bool, failed map[string]bool, buildFailed bool) {
	failed = map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var e testEvent
		line := sc.Text()
		if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		switch {
		case e.Action == "build-fail":
			buildFailed = true
		case e.Test == "" && (strings.Contains(e.Output, "[build failed]") || strings.Contains(e.Output, "[setup failed]")):
			buildFailed = true
		case e.Test == "" && (e.Action == "pass" || e.Action == "fail" || e.Action == "skip"):
			ended = true
		case e.Test != "" && e.Action == "fail" && !strings.Contains(e.Test, "/"):
			failed[e.Test] = true
		}
	}
	return ended, failed, buildFailed
}

// coversLine reports whether a `set` coverage profile has a block of the file importPath whose
// lines include line with a count above 0.
func coversLine(profile []byte, importPath string, line int) (bool, error) {
	lines := strings.Split(string(profile), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "mode:") {
		return false, fmt.Errorf("no mode line")
	}
	for _, l := range lines[1:] {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		// <file>:<startLine>.<startCol>,<endLine>.<endCol> <statements> <count>
		file, rest, ok := strings.Cut(l, ":")
		if !ok {
			return false, fmt.Errorf("a line without a file: %q", l)
		}
		if file != importPath {
			continue
		}
		var sl, sc, el, ec, stmts, count int
		if _, err := fmt.Sscanf(rest, "%d.%d,%d.%d %d %d", &sl, &sc, &el, &ec, &stmts, &count); err != nil {
			return false, fmt.Errorf("a block that cannot be read: %q", l)
		}
		if count > 0 && sl <= line && line <= el {
			return true, nil
		}
	}
	return false, nil
}

// judgeWitnesses runs, for each blocking finding with a witness to run, its witness run, and
// returns which findings it confirmed (by index), why each other blocking finding is a
// suggestion, and the results of runs that cannot be judged.
func judgeWitnesses(check string, r GoRunner, project string, rv Review, findings []Finding) (confirmed map[int]bool, said []string, bad []CheckResult) {
	confirmed = map[int]bool{}
	for i, f := range findings {
		if !f.Blocking {
			continue
		}
		at := fmt.Sprintf("%s at %s:%d", f.Rule, f.File, f.Line)
		w := eligible(project, rv, f)
		if w.suggestion != "" {
			said = append(said, at+" is a suggestion: "+w.suggestion)
			continue
		}
		ok, b := witnessRun(check, r, project, f, w.dir)
		switch {
		case b != nil:
			bad = append(bad, *b)
		case ok:
			confirmed[i] = true
			said = append(said, fmt.Sprintf("%s is confirmed: its witness %s fails having run the line", at, f.Witness.Test))
		default:
			said = append(said, fmt.Sprintf("%s is a suggestion: its witness %s does not fail having run the line (less what package initialisation runs)", at, f.Witness.Test))
		}
	}
	return confirmed, said, bad
}

// FindingsCarryEvidence (ADR 0039): every blocking finding names a rule, a file the project holds
// as a regular file and a line of at least 1, and the witness run of each blocking finding that
// carries a witness to run can be judged. The witness run only decides whether a finding blocks:
// one it does not confirm, and one without a witness, is a suggestion and leaves this check
// passing; the `check` a finding names is not consulted. One failing result per blocking finding
// without that evidence and per witness run that cannot be judged (a witness that does not
// build); else one passing result saying what each blocking finding is.
func FindingsCarryEvidence(r GoRunner, project string, rv Review, findings []Finding) []CheckResult {
	const check = "FindingsCarryEvidence"
	var out []CheckResult
	var judged []Finding
	n := 0
	for _, f := range findings {
		if !f.Blocking {
			continue
		}
		n++
		var missing []string
		if f.Rule == "" {
			missing = append(missing, "no rule")
		}
		if f.File == "" {
			missing = append(missing, "no file")
		} else if !regularFile(project, f.File) {
			missing = append(missing, "its file "+f.File+" is not a file of the project")
		}
		if f.Line < 1 {
			missing = append(missing, "no line of at least 1")
		}
		if len(missing) > 0 {
			out = append(out, fail(check, fmt.Sprintf("blocking finding %s carries no evidence: %s", findingName(f), strings.Join(missing, ", "))))
			continue
		}
		judged = append(judged, f)
	}
	if n == 0 {
		return []CheckResult{pass(check, "no finding is blocking, so none needs evidence")}
	}
	_, said, bad := judgeWitnesses(check, r, project, rv, judged)
	out = append(out, bad...)
	if len(out) > 0 {
		return out
	}
	return []CheckResult{pass(check, strings.Join(said, "; "))}
}

// NoBlockingFindingOpen (ADR 0039): no blocking finding is confirmed by its witness run. One
// failing result per confirmed blocking finding, naming its rule and file, and per witness run
// that cannot be judged; every other finding is a suggestion. Without either, one passing result.
func NoBlockingFindingOpen(r GoRunner, project string, rv Review, findings []Finding) []CheckResult {
	const check = "NoBlockingFindingOpen"
	confirmed, _, bad := judgeWitnesses(check, r, project, rv, findings)
	out := bad
	n := 0
	for i, f := range findings {
		if f.Blocking {
			n++
		}
		if confirmed[i] {
			out = append(out, CheckResult{Part: "findings", Check: check, Reason: fmt.Sprintf("blocking finding %s at %s:%d is open: its witness %s (%s) fails having run that line", f.Rule, f.File, f.Line, f.Witness.Test, f.Witness.File)})
		}
	}
	if len(out) == 0 {
		return []CheckResult{pass(check, "none of the "+strconv.Itoa(n)+" blocking findings is confirmed by its witness test")}
	}
	return out
}

// WitnessesUnchangedOf is TestsUnchanged's witness rule (check contract, EarlierStep): no path of
// b matching witnessGlobs is changed or removed in a, by any step; a path of a matching them that
// b does not hold fails unless mayAdd (the judged step is the review step). One failing result
// per path at fault, naming it.
func WitnessesUnchangedOf(b, a Tree, witnessGlobs []string, mayAdd bool) []CheckResult {
	const check = "TestsUnchanged"
	var out []CheckResult
	n := 0
	for p := range b {
		if MatchGlob(witnessGlobs, p) {
			n++
		}
	}
	for _, p := range changedPaths(b, a) {
		if !MatchGlob(witnessGlobs, p) {
			continue
		}
		switch c := change(b, a, p); {
		case c != "added":
			out = append(out, fail(check, fmt.Sprintf("witness test %s was %s; no step changes or removes a witness test", p, c)))
		case !mayAdd:
			out = append(out, fail(check, fmt.Sprintf("witness test %s was added; only the review step adds witness tests", p)))
		}
	}
	if len(out) == 0 {
		return []CheckResult{pass(check, fmt.Sprintf("the %d witness tests are unchanged", n))}
	}
	return out
}
