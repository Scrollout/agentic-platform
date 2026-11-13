package checks

// The step checks: rules on a step's output in the new project's folder, run after each step of
// the pattern (data-model.md, "Step checks"). Each is a function over directories and the
// pattern's values and returns CheckResults whose failing Reason names the file, import, rule or
// hash at fault. A check that cannot run fails (fail closed). No model, network or clock.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

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

// changedFiles lists, sorted, every path added, changed or removed between two trees.
func changedFiles(before, after map[string][]byte) []string {
	var out []string
	for p, b := range before {
		if a, ok := after[p]; !ok || !bytes.Equal(a, b) {
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

func change(before, after map[string][]byte, p string) string {
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

// matchGlob: "dir/**" matches everything below dir; any other entry is one exact path.
func matchGlob(globs []string, p string) bool {
	for _, g := range globs {
		if dir, ok := strings.CutSuffix(g, "/**"); ok {
			if strings.HasPrefix(p, dir+"/") {
				return true
			}
		} else if g == p {
			return true
		}
	}
	return false
}

func readTrees(check, before, after string) (map[string][]byte, map[string][]byte, *CheckResult) {
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
	return b, a, nil
}

// Scope (ScopeKept): every file added, changed or removed between the two trees matches a
// writable glob. One failing result per file outside them.
func Scope(before, after string, writable []string) []CheckResult {
	const check = "ScopeKept"
	b, a, bad := readTrees(check, before, after)
	if bad != nil {
		return []CheckResult{*bad}
	}
	var out []CheckResult
	for _, p := range changedFiles(b, a) {
		if !matchGlob(writable, p) {
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
	const check = "TestsUnchanged"
	b, a, bad := readTrees(check, before, after)
	if bad != nil {
		return []CheckResult{*bad}
	}
	var out []CheckResult
	n := 0
	for p := range b {
		if matchGlob(testGlobs, p) {
			n++
		}
	}
	for _, p := range changedFiles(b, a) {
		if matchGlob(testGlobs, p) {
			out = append(out, fail(check, fmt.Sprintf("test file %s was %s after the tests step", p, change(b, a, p))))
		}
	}
	if len(out) == 0 {
		return []CheckResult{pass(check, fmt.Sprintf("the %d test files are unchanged", n))}
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

// Dependencies (DependenciesAllowed): no replace directive in go.mod and no symlink in the
// project; every import in the project's .go files (vendor/ aside) and
// every direct require in go.mod outside the project's own module matches allowed and no
// forbidden entry; an indirect require (`// indirect`) must match no forbidden entry. Forbidden
// wins. One failing result per offending import or require.
func Dependencies(project string, allowed, forbidden []string) []CheckResult {
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
	// A replace can swap an allowed module for another one or for a local folder, so none is allowed.
	for _, r := range mf.Replace {
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
		if !strings.HasSuffix(p, ".go") {
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
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
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

// excerpt keeps the output lines that say what failed, or the last lines when none does, so a
// reason stays short.
func excerpt(output string, marks ...string) string {
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

// TestsFailAgainstStub runs `go test ./tests/...` and passes only when it exits non-zero with at
// least one "--- FAIL" and no "[build failed]" or "[setup failed]".
func TestsFailAgainstStub(r GoRunner, project string) []CheckResult {
	const check = "TestsFailAgainstStub"
	res, bad := runGo(check, r, project, "test", "./tests/...")
	switch {
	case bad != nil:
		return []CheckResult{*bad}
	case strings.Contains(res.Output, "[build failed]") || strings.Contains(res.Output, "[setup failed]"):
		return []CheckResult{fail(check, "the tests do not build, and a test that does not build proves nothing: "+excerpt(res.Output, "failed", ".go:"))}
	case res.ExitCode == 0:
		return []CheckResult{fail(check, "the tests pass against the generated stub, so they prove nothing")}
	case !strings.Contains(res.Output, "--- FAIL"):
		return []CheckResult{fail(check, "go test failed without a failing test: "+excerpt(res.Output))}
	}
	return []CheckResult{pass(check, "the tests build and fail against the stub: "+excerpt(res.Output, "--- FAIL"))}
}

// testsRan reports whether a go test run's output shows a package whose tests ran: an "ok" line
// without "[no tests to run]". "[no test files]" and "no packages to test" are no run.
func testsRan(output string) bool {
	for _, l := range strings.Split(output, "\n") {
		if (l == "ok" || strings.HasPrefix(l, "ok ") || strings.HasPrefix(l, "ok\t")) && !strings.Contains(l, "[no tests to run]") {
			return true
		}
	}
	return false
}

// AllTestsPass runs `go test ./...` and passes only on exit code 0 with at least one test run.
func AllTestsPass(r GoRunner, project string) []CheckResult {
	const check = "AllTestsPass"
	res, bad := runGo(check, r, project, "test", "./...")
	if bad != nil {
		return []CheckResult{*bad}
	}
	if res.ExitCode != 0 {
		return []CheckResult{fail(check, fmt.Sprintf("go test exited %d: %s", res.ExitCode, excerpt(res.Output, "--- FAIL", "[build failed]", "[setup failed]")))}
	}
	if !testsRan(res.Output) {
		return []CheckResult{fail(check, "go test ran no test, so nothing shows the code works: "+excerpt(res.Output, "no test files", "no packages", "no tests to run"))}
	}
	return []CheckResult{pass(check, "every test passes")}
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

// GodogStrict runs the Gherkin examples through `go test -v ./tests/...`, where godog's TestSuite
// runs them with Strict set (-v so godog's summary is shown for a passing run too). It passes only
// on exit 0 with no "undefined" in the output and a scenario run: godog's "No scenarios" or
// "0 scenarios" fails, and so does a run with neither a scenario count nor a package whose tests
// ran (strictness is not left to the exit code).
func GodogStrict(r GoRunner, project string) []CheckResult {
	const check = "GodogStrict"
	res, bad := runGo(check, r, project, "test", "-v", "./tests/...")
	switch {
	case bad != nil:
		return []CheckResult{*bad}
	case strings.Contains(res.Output, "undefined"):
		return []CheckResult{fail(check, "godog --strict found undefined steps: "+excerpt(res.Output, "undefined"))}
	case res.ExitCode != 0:
		return []CheckResult{fail(check, fmt.Sprintf("the godog run exited %d: %s", res.ExitCode, excerpt(res.Output, "--- FAIL", "failed", "pending")))}
	}
	if none, some := scenarioCounts(res.Output); none || (!some && !testsRan(res.Output)) {
		return []CheckResult{fail(check, "godog ran no scenario, so no example was checked: "+excerpt(res.Output, "scenarios", "no test files", "no packages", "no tests to run"))}
	}
	return []CheckResult{pass(check, "every scenario passes under godog --strict: "+excerpt(res.Output, "scenarios ("))}
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
	Rule            string `json:"rule"`
	File            string `json:"file"`
	Line            int    `json:"line"`
	Text            string `json:"text"`
	Blocking        bool   `json:"blocking"`
	ConfirmingCheck string `json:"confirming_check"`
}

// FindingsCarryEvidence returns one result per blocking finding: it passes only with a rule id, a
// file, a line > 0 and a confirming check found in confirmations with Passed == false.
// Non-blocking findings are suggestions and yield no result.
func FindingsCarryEvidence(findings []Finding, confirmations []CheckResult) []CheckResult {
	const check = "FindingsCarryEvidence"
	failed := map[string]bool{}
	for _, c := range confirmations {
		if !c.Passed {
			failed[c.Check] = true
		}
	}
	var out []CheckResult
	for _, f := range findings {
		if !f.Blocking {
			continue
		}
		name := f.Rule
		if name == "" {
			name = "the finding on " + f.File
			if f.File == "" {
				name = "a finding with no rule or file"
			}
		}
		var missing []string
		if f.Rule == "" {
			missing = append(missing, "no rule id")
		}
		if f.File == "" {
			missing = append(missing, "no file")
		}
		if f.Line <= 0 {
			missing = append(missing, "no line")
		}
		if f.ConfirmingCheck == "" {
			missing = append(missing, "no confirming check")
		} else if !failed[f.ConfirmingCheck] {
			missing = append(missing, fmt.Sprintf("its confirming check %s did not fail", f.ConfirmingCheck))
		}
		r := CheckResult{Part: "findings", Check: check}
		if len(missing) == 0 {
			r.Passed, r.Reason = true, fmt.Sprintf("%s at %s:%d is confirmed by %s failing", f.Rule, f.File, f.Line, f.ConfirmingCheck)
		} else {
			r.Reason = fmt.Sprintf("blocking finding %s has %s", name, strings.Join(missing, ", "))
		}
		out = append(out, r)
	}
	return out
}
