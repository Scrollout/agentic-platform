package checks

// The step checks: rules on a step's output in the new project's folder, run after each step of
// the pattern (data-model.md, "Step checks"). Each is a function over directories and the
// pattern's values and returns CheckResults whose failing Reason names the file, import, rule or
// hash at fault. A check that cannot run fails (fail closed). No model, network or clock.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	gherkin "github.com/cucumber/gherkin/go/v42"
	messages "github.com/cucumber/messages/go/v34"
	"golang.org/x/mod/modfile"

	"github.com/agentic-platform/engine/canonical"
)

const projectPart = "project"

func fail(check, reason string) CheckResult {
	return CheckResult{Part: projectPart, Check: check, Reason: reason}
}

func pass(check, reason string) CheckResult {
	return CheckResult{Part: projectPart, Check: check, Passed: true, Reason: reason}
}

// readTree returns every regular file below root by its slash-separated relative path.
func readTree(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", p)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = b
		return nil
	})
	return files, err
}

// Tree is a set of project files by slash-separated path, each with the SHA-256 of its bytes in
// lowercase hex: what the comparing checks compare, so they can compare a folder with the hash
// list a step's accepted attempt left (check contract, `before`) as well as with another folder.
type Tree map[string]string

// HashTree is the Tree of files read whole.
func HashTree(files map[string][]byte) Tree {
	t := Tree{}
	for p, b := range files {
		h := sha256.Sum256(b)
		t[p] = hex.EncodeToString(h[:])
	}
	return t
}

// changedFiles lists, sorted, every path added, changed or removed between two trees.
func changedFiles(before, after map[string][]byte) []string {
	return changedPaths(HashTree(before), HashTree(after))
}

func changedPaths(before, after Tree) []string {
	var out []string
	for p, b := range before {
		if a, ok := after[p]; !ok || a != b {
			out = append(out, p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func change(before, after Tree, p string) string {
	_, inBefore := before[p]
	_, inAfter := after[p]
	switch {
	case !inBefore:
		return "added"
	case !inAfter:
		return "removed"
	}
	return "changed"
}

func readTrees(check, before, after string) (Tree, Tree, *CheckResult) {
	b, err := readTree(before)
	if err != nil {
		r := fail(check, "the project before the step cannot be read: "+err.Error())
		return nil, nil, &r
	}
	a, err := readTree(after)
	if err != nil {
		r := fail(check, "the project after the step cannot be read: "+err.Error())
		return nil, nil, &r
	}
	return HashTree(b), HashTree(a), nil
}

// Scope (ScopeKept): every file added, changed or removed between the two trees matches a
// writable glob. One failing result per file outside them.
func Scope(before, after string, writable []string) []CheckResult {
	b, a, bad := readTrees("ScopeKept", before, after)
	if bad != nil {
		return []CheckResult{*bad}
	}
	return ScopeOf(b, a, writable)
}

// ScopeOf is Scope over two Trees.
func ScopeOf(b, a Tree, writable []string) []CheckResult {
	const check = "ScopeKept"
	var out []CheckResult
	for _, p := range changedPaths(b, a) {
		if !MatchGlob(writable, p) {
			out = append(out, fail(check, fmt.Sprintf("%s was %s, but the step may write only %s", p, change(b, a, p), strings.Join(writable, ", "))))
		}
	}
	if len(out) == 0 {
		return []CheckResult{pass(check, "every changed file is inside the step's writable paths")}
	}
	return out
}

// TestsUnchanged: files under testGlobs are byte-identical in both trees, none changed, added or
// removed. One failing result per file that differs.
func TestsUnchanged(before, after string, testGlobs []string) []CheckResult {
	b, a, bad := readTrees("TestsUnchanged", before, after)
	if bad != nil {
		return []CheckResult{*bad}
	}
	return TestsUnchangedOf(b, a, testGlobs)
}

// TestsUnchangedOf is TestsUnchanged over two Trees.
func TestsUnchangedOf(b, a Tree, testGlobs []string) []CheckResult {
	return unchangedOf("TestsUnchanged", "test file", "tests", b, a, testGlobs)
}

// unchangedOf is the rule of TestsUnchanged and GeneratedCodeUnchanged: no path matching the
// earlier step's globs is added, changed or removed. One failing result per path that differs.
func unchangedOf(check, what, step string, b, a Tree, globs []string) []CheckResult {
	var out []CheckResult
	n := 0
	for p := range b {
		if MatchGlob(globs, p) {
			n++
		}
	}
	for _, p := range changedPaths(b, a) {
		if MatchGlob(globs, p) {
			out = append(out, fail(check, fmt.Sprintf("%s %s was %s after the %s step", what, p, change(b, a, p), step)))
		}
	}
	if len(out) == 0 {
		return []CheckResult{pass(check, fmt.Sprintf("the %d %ss are unchanged", n, what))}
	}
	return out
}

// isStd follows the go command's rule: a standard-library path has no dot in its first element.
func isStd(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// matchPackage reports whether path matches one of the Go package patterns: "std", an exact
// path, or "path/..." (the path itself and everything below it). module marks a go.mod require,
// which "std" never matches.
func matchPackage(patterns []string, path string, module bool) (string, bool) {
	for _, p := range patterns {
		switch {
		case p == "std":
			if !module && isStd(path) {
				return p, true
			}
		case strings.HasSuffix(p, "/..."):
			base := strings.TrimSuffix(p, "/...")
			if path == base || strings.HasPrefix(path, base+"/") {
				return p, true
			}
		case p == path:
			return p, true
		}
	}
	return "", false
}

// Dependencies (DependenciesAllowed) over the whole project: no replace directive in go.mod and
// no symlink in the project; every import in the project's .go files (vendor/ aside) and
// every direct require in go.mod outside the project's own module matches allowed and no
// forbidden entry; an indirect require (`// indirect`) must match no forbidden entry. Forbidden
// wins. One failing result per offending import or require.
func Dependencies(project string, allowed, forbidden []string) []CheckResult {
	return dependencies(project, func(string) bool { return true }, allowed, forbidden)
}

// DependenciesOf is DependenciesAllowed as a step is judged (owner, 2026-11-13): only the step's
// own writable files, matched by its writable globs, are judged against its lists, since another
// step's files were judged by that step's lists and ScopeKept keeps them unchanged. go.mod's
// replace directives and requires are judged only when go.mod is one of them; a symlink anywhere
// in the project still fails.
func DependenciesOf(project string, writable, allowed, forbidden []string) []CheckResult {
	return dependencies(project, func(rel string) bool { return MatchGlob(writable, rel) }, allowed, forbidden)
}

func dependencies(project string, judged func(rel string) bool, allowed, forbidden []string) []CheckResult {
	const check = "DependenciesAllowed"
	gomod, err := os.ReadFile(filepath.Join(project, "go.mod"))
	if err != nil {
		return []CheckResult{fail(check, "go.mod cannot be read, so the dependencies cannot be checked: "+err.Error())}
	}
	mf, err := modfile.Parse("go.mod", gomod, nil)
	if err != nil {
		return []CheckResult{fail(check, "go.mod cannot be parsed: "+err.Error())}
	}
	if mf.Module == nil || mf.Module.Mod.Path == "" {
		return []CheckResult{fail(check, "go.mod names no module")}
	}
	module := mf.Module.Mod.Path
	var out []CheckResult
	ownMod := judged("go.mod")
	// A replace can swap an allowed module for another one or for a local folder, so none is allowed.
	for _, r := range mf.Replace {
		if !ownMod {
			break
		}
		out = append(out, fail(check, fmt.Sprintf("go.mod has replace %s => %s, and no replace directive is allowed", r.Old.Path, r.New.Path)))
	}
	own := func(p string) bool { return p == module || strings.HasPrefix(p, module+"/") }

	judge := func(where, verb, path string, isModule, indirect bool) {
		if own(path) {
			return
		}
		if p, bad := matchPackage(forbidden, path, isModule); bad {
			out = append(out, fail(check, fmt.Sprintf("%s %s %s, which the pattern forbids (%s)", where, verb, path, p)))
		} else if _, ok := matchPackage(allowed, path, isModule); !ok && !indirect {
			out = append(out, fail(check, fmt.Sprintf("%s %s %s, which no allowed entry matches", where, verb, path)))
		}
	}
	for _, r := range mf.Require {
		if !ownMod {
			break
		}
		verb := "requires"
		if r.Indirect {
			verb = "requires indirectly"
		}
		judge("go.mod", verb, r.Mod.Path, true, r.Indirect)
	}

	fset := token.NewFileSet()
	err = filepath.WalkDir(project, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(project, p)
		rel = filepath.ToSlash(rel)
		// A symlink could bring files from outside the project into the build unscanned.
		if d.Type()&fs.ModeSymlink != 0 {
			out = append(out, fail(check, rel+" is a symlink, and no symlink is allowed in the project"))
			return nil
		}
		if d.IsDir() {
			// vendor/ is not scanned: only the fixed generate procedure writes it (from the requires
			// checked above), and ScopeKept keeps every other step out of it.
			if rel == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || !judged(rel) {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("%s cannot be parsed: %w", rel, err)
		}
		seen := map[string]bool{}
		for _, im := range f.Imports {
			path := strings.Trim(im.Path.Value, "\"`")
			if !seen[path] {
				seen[path] = true
				judge(rel, "imports", path, false, false)
			}
		}
		return nil
	})
	if err != nil {
		return append(out, fail(check, "the project's Go files cannot be read: "+err.Error()))
	}
	if len(out) == 0 {
		return []CheckResult{pass(check, "every import and require outside the project's module is allowed")}
	}
	return out
}

// GoRunner runs the go toolchain in dir (args are the go arguments, e.g. "test", "./...") and
// reports the exit code and combined output. An error means the tool could not be started.
type GoRunner interface {
	Go(dir string, args ...string) (GoResult, error)
}

// GoResult is one go command's exit code and combined output.
type GoResult struct {
	ExitCode int
	Output   string
}

// DefaultGoTimeout bounds one go command of NewGoRunner: 10 minutes, the api-feature-go
// pattern's step budget (budgets.step.minutes), so no check outlasts the step it checks. A caller
// holding a pattern passes its step budget to NewGoRunnerWithTimeout instead.
const DefaultGoTimeout = 10 * time.Minute

type goRunner struct{ timeout time.Duration }

// NewGoRunner returns the real go toolchain on PATH, each command bounded by DefaultGoTimeout. The
// project is run as its own module (GOWORK=off), with the local toolchain only and no module proxy
// (GOTOOLCHAIN=local, GOPROXY=off), so a check never reaches the network: modules come from
// vendor/ or the cache.
func NewGoRunner() GoRunner { return goRunner{timeout: DefaultGoTimeout} }

// NewGoRunnerWithTimeout is NewGoRunner with each go command bounded by d. A command still running
// after d is killed with every process it started (the test binaries too), and the runner returns
// an error saying it timed out, so the check that ran it fails.
func NewGoRunnerWithTimeout(d time.Duration) GoRunner { return goRunner{timeout: d} }

func (g goRunner) Go(dir string, args ...string) (GoResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Env = CommandEnv()
	// Its own process group, killed whole on timeout: killing only go would leave a test binary
	// running and holding the output pipe open.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return GoResult{}, fmt.Errorf("timed out after %s", g.timeout)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return GoResult{ExitCode: exit.ExitCode(), Output: string(out)}, nil
	}
	if err != nil {
		return GoResult{}, err
	}
	return GoResult{Output: string(out)}, nil
}

// durations are the times go test prints: " (0.01s)" after a test, "\t0.123s" after a package.
var durations = regexp.MustCompile(`( \([0-9]+(\.[0-9]+)?s\)|\t[0-9]+(\.[0-9]+)?s)`)

// excerpt keeps the output lines that say what failed, or the last lines when none does, so a
// reason stays short.
func excerpt(output string, marks ...string) string {
	// Durations vary between runs of the same code, and the same request gives the same answer.
	output = durations.ReplaceAllString(output, "")
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var keep []string
	for _, l := range lines {
		for _, m := range marks {
			if strings.Contains(l, m) {
				keep = append(keep, strings.TrimSpace(l))
				break
			}
		}
	}
	if len(keep) == 0 {
		keep = lines[max(0, len(lines)-5):]
	}
	if len(keep) > 10 {
		keep = keep[:10]
	}
	return strings.Join(keep, "; ")
}

func runGo(check string, r GoRunner, dir string, args ...string) (GoResult, *CheckResult) {
	res, err := r.Go(dir, args...)
	if err != nil {
		f := fail(check, fmt.Sprintf("go %s could not run: %v", strings.Join(args, " "), err))
		return res, &f
	}
	return res, nil
}

// testArgs are the arguments of every go test a test check runs: one event per test from the go
// tool (-json), never a cached result (-count=1); research D37.
func testArgs(pkgs string) []string { return []string{"test", "-json", "-count=1", pkgs} }

// The folders whose _test.go files declare the tests (research D37): the tests step's and the
// review step's witnesses. Other test files (the code step's own) run but declare nothing.
var (
	declaredInTests = []string{"tests"}
	declaredAll     = []string{"tests", "review"}
)

// names lists the tests, each with its package's import path, for a reason.
func names(ks []testKey) string {
	s := make([]string, len(ks))
	for i, k := range ks {
		s[i] = k.String()
	}
	return strings.Join(s, ", ")
}

// withoutResult is the declared tests that have no result event (pass, fail or skip): they
// vanished, never having run.
func withoutResult(declared []testKey, ev testEvents) []testKey {
	var out []testKey
	for _, k := range declared {
		if _, ok := ev.results[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}

// TestsFailAgainstStub runs `go test -json -count=1 ./tests/...` and passes only when the tests
// build, every declared test under tests/ has a result event (pass, fail or skip), so none
// vanished, and at least one has fail (research D37).
func TestsFailAgainstStub(r GoRunner, project string) []CheckResult {
	const check = "TestsFailAgainstStub"
	res, bad := runGo(check, r, project, testArgs("./tests/...")...)
	if bad != nil {
		return []CheckResult{*bad}
	}
	ev := readEvents(res.Output)
	if ev.buildFailed {
		return []CheckResult{fail(check, "the tests do not build, and a test that does not build proves nothing: "+excerpt(ev.text, "failed", ".go:"))}
	}
	declared, err := declaredTests(project, declaredInTests...)
	if err != nil {
		return []CheckResult{fail(check, "the declared tests cannot be read: "+err.Error())}
	}
	if len(declared) == 0 {
		return []CheckResult{fail(check, "tests/ declares no test, so nothing can fail against the stub")}
	}
	if gone := withoutResult(declared, ev); len(gone) > 0 {
		return []CheckResult{fail(check, "declared tests have no result event, so they never ran (the code under test may end the test binary): "+names(gone))}
	}
	var failing []testKey
	for _, k := range declared {
		if ev.results[k] == "fail" {
			failing = append(failing, k)
		}
	}
	if len(failing) == 0 {
		return []CheckResult{fail(check, "the tests pass against the generated stub, so they prove nothing")}
	}
	return []CheckResult{pass(check, "the tests build and fail against the stub: "+names(failing))}
}

// AllTestsPass runs `go test -json -count=1 ./...` and passes only when it exits 0 and every
// declared test (under tests/ and review/) has a pass event (research D37). One failing result
// per declared test that failed, was skipped or has no event (it vanished, for example because
// the code's init ended the test binary), naming it with its package; one for a non-zero exit
// (a build failure, or a failing test of the code step's own); and one when the project declares
// no test.
func AllTestsPass(r GoRunner, project string) []CheckResult {
	const check = "AllTestsPass"
	res, bad := runGo(check, r, project, testArgs("./...")...)
	if bad != nil {
		return []CheckResult{*bad}
	}
	ev := readEvents(res.Output)
	declared, err := declaredTests(project, declaredAll...)
	if err != nil {
		return []CheckResult{fail(check, "the declared tests cannot be read: "+err.Error())}
	}
	var out []CheckResult
	for _, k := range declared {
		switch a, ok := ev.results[k]; {
		case !ok:
			out = append(out, fail(check, "declared test "+k.String()+" has no result event, so it never ran (the code under test may end the test binary)"))
		case a == "fail":
			out = append(out, fail(check, "declared test "+k.String()+" failed"))
		case a == "skip":
			out = append(out, fail(check, "declared test "+k.String()+" was skipped, and a skipped test shows nothing"))
		}
	}
	if res.ExitCode != 0 {
		out = append(out, fail(check, fmt.Sprintf("go test exited %d: %s", res.ExitCode, excerpt(ev.text, "--- FAIL", "[build failed]", "[setup failed]", ".go:"))))
	}
	if len(declared) == 0 {
		out = append(out, fail(check, "the project declares no test under tests/ or review/, so nothing shows the code works"))
	}
	if len(out) > 0 {
		return out
	}
	return []CheckResult{pass(check, fmt.Sprintf("every one of the %d declared tests passes", len(declared)))}
}

// scenarioCounts reads godog's summary lines: none says whether one reports zero scenarios ("No
// scenarios", "0 scenarios"), some whether one reports a positive count ("3 scenarios (3 passed)").
func scenarioCounts(output string) (none, some bool) {
	for _, l := range strings.Split(output, "\n") {
		f := strings.Fields(l)
		if len(f) < 2 || f[1] != "scenarios" {
			continue
		}
		if n, err := strconv.Atoi(f[0]); err == nil && n > 0 {
			some = true
		} else {
			none = true
		}
	}
	return none, some
}

// GodogStrict (EveryExampleBound) runs the Gherkin examples through `go test -json -count=1
// ./tests/...`, where godog's TestSuite runs them with Strict set (-json, which implies -v, so
// godog's summary is shown for a passing run too); godog's summary and the `=== RUN` lines are
// read from the events' output, rejoined into the text go test prints. It judges binding, not results (owner, 2026-11-13): the tests step's tests must fail
// against the stub, so the exit code is not read. It passes only when the tests build, no step is
// undefined, pending or ambiguous, and scenarios ran: at least one, and when the project's
// features/slice.feature can be read, as many as godog's own summary counts (scenariosRun) and
// at least as many as the file has, each Outline row counted (featureScenarios). godog's "No
// scenarios" or "0 scenarios" fails. A run with no scenario count falls back on the events
// (research D37): it passes that rule only when tests/ declares a test and every declared test
// there has a result event (pass, fail or skip); a vanished one is named.
func GodogStrict(r GoRunner, project string) []CheckResult {
	const check = "GodogStrict"
	res, bad := runGo(check, r, project, testArgs("./tests/...")...)
	if bad != nil {
		return []CheckResult{*bad}
	}
	ev := readEvents(res.Output)
	text := ansiColour.ReplaceAllString(ev.text, "") // godog colours its counts
	switch {
	case ev.buildFailed || strings.Contains(text, "[build failed]") || strings.Contains(text, "[setup failed]"):
		return []CheckResult{fail(check, "the tests do not build, so no example is bound: "+excerpt(text, "failed", ".go:"))}
	case strings.Contains(text, "undefined"):
		return []CheckResult{fail(check, "godog --strict found undefined steps: "+excerpt(text, "undefined"))}
	case unboundSteps.MatchString(text):
		return []CheckResult{fail(check, "godog --strict found pending or ambiguous steps: "+excerpt(text, "pending", "ambiguous"))}
	}
	none, some := scenarioCounts(text)
	if none {
		return []CheckResult{fail(check, "godog ran no scenario, so no example was checked: "+excerpt(text, "scenarios"))}
	}
	if !some {
		declared, err := declaredTests(project, declaredInTests...)
		switch {
		case err != nil:
			return []CheckResult{fail(check, "godog printed no scenario count, and the declared tests cannot be read: "+err.Error())}
		case len(declared) == 0:
			return []CheckResult{fail(check, "godog ran no scenario and tests/ declares no test, so no example was checked: "+excerpt(text, "no test files", "no packages", "no tests to run"))}
		}
		if gone := withoutResult(declared, ev); len(gone) > 0 {
			return []CheckResult{fail(check, "godog printed no scenario count, and declared tests have no result event, so they never ran: "+names(gone))}
		}
	}
	want, ok, err := featureScenarios(project)
	if err != nil {
		return []CheckResult{fail(check, "features/slice.feature is not Gherkin, so its scenarios cannot be counted: "+err.Error())}
	}
	if ok {
		if got := scenariosRun(text); got < want {
			return []CheckResult{fail(check, fmt.Sprintf("godog ran %d scenarios, and features/slice.feature has %d, so not every example ran", got, want))}
		}
	}
	return []CheckResult{pass(check, "every scenario runs with no undefined, pending or ambiguous step: "+excerpt(text, "scenarios ("))}
}

// ansiColour is a terminal colour sequence.
var ansiColour = regexp.MustCompile("\x1b\\[[0-9;]*m")

// unboundSteps matches godog's summary counts of pending or ambiguous steps ("1 pending").
var unboundSteps = regexp.MustCompile(`(?i)\b[1-9][0-9]* (pending|ambiguous)\b|ambiguous step definition`)

// featureScenarios is the number of scenarios godog runs from the project's
// features/slice.feature: its pickles, as the Gherkin parser godog uses compiles them, so each
// Examples row of a Scenario Outline is one scenario and `Scenario:` in a docstring or a `#`
// comment is none. ok is false when the file cannot be read; err is set when it is not Gherkin.
func featureScenarios(project string) (n int, ok bool, err error) {
	b, err := os.ReadFile(filepath.Join(project, "features", "slice.feature"))
	if err != nil {
		return 0, false, nil
	}
	ids := &messages.Incrementing{}
	gd, err := gherkin.ParseGherkinDocument(bytes.NewReader(b), ids.NewId)
	if err != nil {
		return 0, true, err
	}
	return len(gherkin.Pickles(*gd, "features/slice.feature", ids.NewId)), true, nil
}

// godogScenarios is godog's own scenario summary line, "<n> scenarios (<k> passed, ...)", at the
// start of a line as godog prints it; godogSteps is the steps line godog prints right after it.
var (
	godogScenarios = regexp.MustCompile(`^([0-9]+) scenarios? \(([0-9]+ (passed|failed|pending|undefined|ambiguous|skipped)(, )?)+\)$`)
	godogSteps     = regexp.MustCompile(`^[0-9]+ steps? \(([0-9]+ (passed|failed|pending|undefined|ambiguous|skipped)(, )?)+\)$`)
)

// scenariosRun adds up the scenario counts of godog's own summaries: a scenarios line directly
// followed by its steps line, and per top-level test (`=== RUN   TestX`) only the last one, which
// godog prints when its run ends. A line a test prints that looks like a count, alone or inside a
// scenario before godog's summary, does not add to it.
func scenariosRun(output string) int {
	n, last := 0, -1
	lines := strings.Split(output, "\n")
	for i, l := range lines {
		l = strings.TrimRight(l, "\r")
		if rest, ok := strings.CutPrefix(l, "=== RUN   "); ok && !strings.Contains(rest, "/") {
			if last >= 0 {
				n += last
			}
			last = -1
			continue
		}
		m := godogScenarios.FindStringSubmatch(l)
		if m == nil || i+1 >= len(lines) || !godogSteps.MatchString(strings.TrimRight(lines[i+1], "\r")) {
			continue
		}
		if k, err := strconv.Atoi(m[1]); err == nil {
			last = k
		}
	}
	if last >= 0 {
		n += last
	}
	return n
}

// genOnly keeps the files under gen/.
func genOnly(tree map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for p, b := range tree {
		if strings.HasPrefix(p, "gen/") {
			out[p] = b
		}
	}
	return out
}

// GenerationDrift (GenerationRepeatable) runs `go generate ./...` in a copy of the project and
// passes only when the files under gen/ equal the project's: same set, same bytes. The project
// itself is not touched.
func GenerationDrift(r GoRunner, project string) []CheckResult {
	const check = "GenerationRepeatable"
	tmp, err := os.MkdirTemp("", "drift-")
	if err != nil {
		return []CheckResult{fail(check, "no folder for a fresh generation: "+err.Error())}
	}
	defer os.RemoveAll(tmp)
	fresh := filepath.Join(tmp, "p")
	if err := os.CopyFS(fresh, os.DirFS(project)); err != nil {
		return []CheckResult{fail(check, "the project cannot be copied for a fresh generation: "+err.Error())}
	}
	before, err := readTree(project)
	if err != nil {
		return []CheckResult{fail(check, "the project cannot be read: "+err.Error())}
	}
	want := genOnly(before)
	if len(want) == 0 {
		return []CheckResult{fail(check, "the project has no generated files under gen/")}
	}
	res, bad := runGo(check, r, fresh, "generate", "./...")
	if bad != nil {
		return []CheckResult{*bad}
	}
	if res.ExitCode != 0 {
		return []CheckResult{fail(check, fmt.Sprintf("go generate exited %d: %s", res.ExitCode, excerpt(res.Output)))}
	}
	after, err := readTree(fresh)
	if err != nil {
		return []CheckResult{fail(check, "the fresh generation cannot be read: "+err.Error())}
	}
	got := genOnly(after)
	var out []CheckResult
	for _, p := range changedFiles(want, got) {
		what := "differs from"
		if _, ok := got[p]; !ok {
			what = "is missing from"
		} else if _, ok := want[p]; !ok {
			what = "is only in"
		}
		out = append(out, fail(check, fmt.Sprintf("%s %s a fresh generation (generated code is never edited by hand)", p, what)))
	}
	if len(out) == 0 {
		return []CheckResult{pass(check, fmt.Sprintf("the %d files under gen/ equal a fresh generation", len(want)))}
	}
	return out
}

// ContractHash (ContractMatchesApproval): the hash an approval of the contract part stores
// (canonical.ContractHash of api/openapi.yaml's text, the one owner of that hash) equals approved.
func ContractHash(project, approved string) []CheckResult {
	const check, file = "ContractMatchesApproval", "api/openapi.yaml"
	b, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(file)))
	if err != nil {
		return []CheckResult{fail(check, file+" cannot be read: "+err.Error())}
	}
	got, err := canonical.ContractHash(string(b))
	if err != nil {
		return []CheckResult{fail(check, file+" cannot be hashed as the approval does: "+err.Error())}
	}
	if got != approved {
		return []CheckResult{fail(check, fmt.Sprintf("%s hashes to %s, not the approved %s", file, got, approved))}
	}
	return []CheckResult{pass(check, file+" is the approved contract")}
}

// Finding is a review finding.
type Finding struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Text     string `json:"text"`
	Blocking bool   `json:"blocking"`
	// Witness is the new witness test a blocking finding carries (witness.go); nil when it names
	// none, which makes it a suggestion.
	Witness *Witness `json:"witness,omitempty"`
}
