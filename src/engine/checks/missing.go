package checks

// The step checks T054e added: ExamplesMatchApproval, GeneratedCodeBuilds, TestsBuild,
// GeneratedCodeUnchanged and the review checks that rerun a finding's rule tests
// (NoBlockingFindingOpen, and FindingsCarryEvidence as FindingsRerun). The check
// contract's runCheck states each rule; these are its one implementation.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	slicedoc "github.com/agentic-platform/slice"
)

// examplesFile is where the contract-check step copies the approved examples.
const examplesFile = "features/slice.feature"

// readError says why a project file cannot be read, without the host path an *fs.PathError holds.
func readError(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "it does not exist"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// ExamplesMatchApproval: features/slice.feature is byte for byte approved, the `examples` text of
// the slice's approved rules part.
func ExamplesMatchApproval(project, approved string) []CheckResult {
	const check = "ExamplesMatchApproval"
	b, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(examplesFile)))
	if err != nil {
		return []CheckResult{fail(check, examplesFile+" cannot be read: "+readError(err))}
	}
	if string(b) != approved {
		return []CheckResult{fail(check, fmt.Sprintf("%s (%d bytes) is not byte for byte the approved examples (%d bytes)", examplesFile, len(b), len(approved)))}
	}
	return []CheckResult{pass(check, examplesFile+" is the approved examples")}
}

// GeneratedCodeBuilds: `go build -mod=vendor ./...` of the project, offline: the module builds
// from vendor/ alone, so a module that is only in the module cache does not count.
func GeneratedCodeBuilds(r GoRunner, project string) []CheckResult {
	const check = "GeneratedCodeBuilds"
	res, bad := runGo(check, r, project, "build", "-mod=vendor", "./...")
	if bad != nil {
		return []CheckResult{*bad}
	}
	if res.ExitCode != 0 {
		return []CheckResult{fail(check, fmt.Sprintf("go build -mod=vendor exited %d: %s", res.ExitCode, excerpt(HidePaths(res.Output, project), ".go:", "go.mod", "vendor")))}
	}
	return []CheckResult{pass(check, "the project builds offline from vendor/")}
}

// TestsBuild: every test package of the project compiles (`go test -c` into a folder outside the
// project); no test is run. A project with no test package fails: there is nothing to judge.
func TestsBuild(r GoRunner, project string) []CheckResult {
	const check = "TestsBuild"
	out, err := os.MkdirTemp("", "testsbuild-")
	if err != nil {
		return []CheckResult{fail(check, "no folder for the test binaries: "+err.Error())}
	}
	defer os.RemoveAll(out)
	res, bad := runGo(check, r, project, "test", "-c", "-vet=off", "-o", out+string(filepath.Separator), "./...")
	if bad != nil {
		return []CheckResult{*bad}
	}
	output := HidePaths(strings.ReplaceAll(res.Output, out, "<binaries>"), project)
	if res.ExitCode != 0 {
		return []CheckResult{fail(check, fmt.Sprintf("the test packages do not build (go test -c exited %d): %s", res.ExitCode, excerpt(output, ".go:", "[build failed]", "[setup failed]")))}
	}
	bins, err := os.ReadDir(out)
	if err != nil || len(bins) == 0 {
		return []CheckResult{fail(check, "the project has no test package to build")}
	}
	return []CheckResult{pass(check, fmt.Sprintf("the %d test packages build", len(bins)))}
}

// GeneratedCodeUnchanged: like TestsUnchanged, for the globs of the step that wrote the generated
// code (gen/**, go.mod, go.sum, vendor/**): none added, changed or removed.
func GeneratedCodeUnchanged(before, after string, genGlobs []string) []CheckResult {
	b, a, bad := readTrees("GeneratedCodeUnchanged", before, after)
	if bad != nil {
		return []CheckResult{*bad}
	}
	return GeneratedCodeUnchangedOf(b, a, genGlobs)
}

// GeneratedCodeUnchangedOf is GeneratedCodeUnchanged over two Trees.
func GeneratedCodeUnchangedOf(b, a Tree, genGlobs []string) []CheckResult {
	return unchangedOf("GeneratedCodeUnchanged", "generated file", "generate", b, a, genGlobs)
}

// ---- the rerun of a finding's rule tests ----

// testEvent is one line of `go test -json` (cmd/test2json).
type testEvent struct {
	Action  string
	Package string
	Test    string
	Output  string
}

// testRun is one `go test -json` run: each test's end and output, by package and name.
type testRun struct {
	project   string
	tests     []testKey
	failed    map[testKey]bool
	output    map[testKey][]string
	scenarios map[string][]string // rule ID to its scenarios' names (slice.RuleScenarios)
}

type testKey struct{ pkg, name string }

// rerunTests runs every test of the project once, offline, against the code as it stands, and
// reads its JSON events. Tests that do not build, or output that is not go test's, fail the check:
// what cannot be judged never passes.
func rerunTests(check string, r GoRunner, project string) (*testRun, *CheckResult) {
	run := &testRun{project: project, failed: map[testKey]bool{}, output: map[testKey][]string{}, scenarios: map[string][]string{}}
	if b, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(examplesFile))); err == nil {
		sc, err := slicedoc.RuleScenarios(string(b))
		if err != nil {
			f := fail(check, examplesFile+" is not Gherkin, so its rule scenarios cannot be found: "+err.Error())
			return nil, &f
		}
		run.scenarios = sc
	} else if !errors.Is(err, fs.ErrNotExist) {
		f := fail(check, examplesFile+" cannot be read: "+readError(err))
		return nil, &f
	}
	res, bad := runGo(check, r, project, "test", "-json", "-count=1", "./...")
	if bad != nil {
		return nil, bad
	}
	var plain []string
	events := 0
	sc := bufio.NewScanner(strings.NewReader(res.Output))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		var e testEvent
		if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &e) != nil {
			if strings.TrimSpace(line) != "" {
				plain = append(plain, line)
			}
			continue
		}
		events++
		if e.Action == "build-fail" || (e.Test == "" && (strings.Contains(e.Output, "[build failed]") || strings.Contains(e.Output, "[setup failed]"))) {
			f := fail(check, "the tests do not build, so no rule test can be rerun: "+excerpt(HidePaths(strings.Join(plain, "\n")+"\n"+e.Output, project), ".go:", "failed"))
			return nil, &f
		}
		if e.Test == "" {
			continue
		}
		k := testKey{e.Package, e.Test}
		switch e.Action {
		case "run":
			run.tests = append(run.tests, k)
		case "output":
			run.output[k] = append(run.output[k], e.Output)
		case "fail":
			run.failed[k] = true
		}
	}
	if events == 0 || (res.ExitCode != 0 && len(run.failed) == 0) {
		f := fail(check, fmt.Sprintf("go test exited %d without a test result, so no rule test can be rerun: %s", res.ExitCode, excerpt(HidePaths(strings.Join(plain, "\n"), project))))
		return nil, &f
	}
	return run, nil
}

// namesRule reports whether a Go test or subtest name has a part holding R<n>, R_<n> or R-<n>,
// not preceded by an uppercase letter or a digit and not followed by a digit.
func namesRule(name, rule string) bool {
	n, ok := strings.CutPrefix(rule, "R-")
	if !ok {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		for _, m := range ruleName.FindAllStringSubmatchIndex(part, -1) {
			// The digits are greedy, so no digit follows; the letter before must not be an
			// uppercase letter or a digit.
			if before := m[0] - 1; before >= 0 && (part[before] >= 'A' && part[before] <= 'Z' || part[before] >= '0' && part[before] <= '9') {
				continue
			}
			if part[m[2]:m[3]] == n {
				return true
			}
		}
	}
	return false
}

// ruleName is a rule named in a Go test name: R<n>, R_<n> or R-<n>, its number captured.
var ruleName = regexp.MustCompile(`R[-_]?([0-9]+)`)

// subtestName is the name Go's testing package gives a subtest run under name: spaces become
// underscores and characters that do not print are escaped (testing.rewrite).
func subtestName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsSpace(r):
			b.WriteByte('_')
		case !strconv.IsPrint(r):
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var duplicateSuffix = regexp.MustCompile(`#[0-9]+$`)

// isScenario reports whether a test is the subtest godog runs for one of the scenarios.
func isScenario(name string, scenarios []string) bool {
	name = duplicateSuffix.ReplaceAllString(name, "")
	for _, s := range scenarios {
		if strings.HasSuffix(name, "/"+subtestName(s)) {
			return true
		}
	}
	return false
}

// namesFile reports whether output names the project file as `<file>:<line>`: the project path
// itself (preceded by nothing, `./` or a character that cannot be part of a path), or the
// project folder's absolute path, `/`, and the project path.
func namesFile(output, project, file string) bool {
	if regexp.MustCompile(`(^|[^A-Za-z0-9._~+@=,/-])(\./)?` + regexp.QuoteMeta(file) + `:[0-9]`).MatchString(output) {
		return true
	}
	for _, dir := range projectDirs(project) {
		if regexp.MustCompile(regexp.QuoteMeta(dir+"/"+file) + `:[0-9]`).MatchString(output) {
			return true
		}
	}
	return false
}

// confirmedBy returns the name of a test of rule that failed with output naming file, or "".
func (run *testRun) confirmedBy(rule, file string) string {
	for _, k := range run.tests {
		if !run.failed[k] || !(namesRule(k.name, rule) || isScenario(k.name, run.scenarios[rule])) {
			continue
		}
		var out strings.Builder
		for _, o := range run.tests {
			if o.pkg == k.pkg && (o.name == k.name || strings.HasPrefix(o.name, k.name+"/")) {
				out.WriteString(strings.Join(run.output[o], ""))
			}
		}
		if namesFile(out.String(), run.project, file) {
			return k.name
		}
	}
	return ""
}

// claimed reports whether a finding claims to block at a file: blocking, with a rule and a file.
func claimed(f Finding) bool { return f.Blocking && f.Rule != "" && f.File != "" }

// NoBlockingFindingOpen (owner, 2026-11-13): a review finding is confirmed by rerunning the
// tests of the rule it names (godog scenarios tagged @R-<n>, and Go tests that name the rule)
// against the current code; it blocks only if one of them fails with output naming the file it
// names. One failing result per confirmed blocking finding, naming its rule and file; a finding
// whose rule's tests pass, fail somewhere else or do not exist, or that is not blocking, is a
// suggestion. Without a confirmed one, one passing result. The tests run only when some finding
// claims to block.
func NoBlockingFindingOpen(r GoRunner, project string, findings []Finding) []CheckResult {
	const check = "NoBlockingFindingOpen"
	if !slices.ContainsFunc(findings, claimed) {
		return []CheckResult{pass(check, "no finding claims to block")}
	}
	run, bad := rerunTests(check, r, project)
	if bad != nil {
		return []CheckResult{*bad}
	}
	var out []CheckResult
	n := 0
	for _, f := range findings {
		if !claimed(f) {
			continue
		}
		n++
		if t := run.confirmedBy(f.Rule, f.File); t != "" {
			out = append(out, CheckResult{Part: "findings", Check: check, Reason: fmt.Sprintf("blocking finding %s at %s:%d is open: its rule's test %s fails there", f.Rule, f.File, f.Line, t)})
		}
	}
	if len(out) == 0 {
		return []CheckResult{pass(check, fmt.Sprintf("none of the %d blocking findings is confirmed by its rule's tests failing at its file", n))}
	}
	return out
}

// FindingsRerun is FindingsCarryEvidence as the check service judges it (owner, 2026-11-13):
// every blocking finding names a rule, a file the project holds as a regular file and a line of at
// least 1, and the rerun of its rule's tests (as NoBlockingFindingOpen reruns them) can be judged.
// The rerun decides whether a finding blocks: one it does not confirm is a suggestion and does not
// fail this check, and the `check` a finding names is not consulted. One failing result per
// blocking finding without that evidence, or one for a rerun that cannot be judged; else one
// passing result saying which findings the rerun confirmed. No blocking finding passes.
func FindingsRerun(r GoRunner, project string, findings []Finding) []CheckResult {
	const check = "FindingsCarryEvidence"
	var blocking []Finding
	for _, f := range findings {
		if f.Blocking {
			blocking = append(blocking, f)
		}
	}
	if len(blocking) == 0 {
		return []CheckResult{pass(check, "no finding is blocking, so none needs evidence")}
	}
	var out []CheckResult
	for _, f := range blocking {
		var missing []string
		if f.Rule == "" {
			missing = append(missing, "no rule")
		}
		if f.File == "" {
			missing = append(missing, "no file")
		} else if fi, err := os.Lstat(filepath.Join(project, filepath.FromSlash(f.File))); err != nil || !fi.Mode().IsRegular() {
			missing = append(missing, "its file "+f.File+" is not a file of the project")
		}
		if f.Line < 1 {
			missing = append(missing, "no line of at least 1")
		}
		if len(missing) > 0 {
			out = append(out, CheckResult{Part: "findings", Check: check, Reason: fmt.Sprintf("blocking finding %s carries no evidence: %s", findingName(f), strings.Join(missing, ", "))})
		}
	}
	run, bad := rerunTests(check, r, project)
	if bad != nil {
		return append(out, *bad)
	}
	if len(out) > 0 {
		return out
	}
	var said []string
	for _, f := range blocking {
		if t := run.confirmedBy(f.Rule, f.File); t != "" {
			said = append(said, fmt.Sprintf("%s at %s:%d is confirmed by its rule's test %s failing there", f.Rule, f.File, f.Line, t))
		} else {
			said = append(said, fmt.Sprintf("%s at %s:%d is a suggestion: no test of its rule fails there", f.Rule, f.File, f.Line))
		}
	}
	return []CheckResult{pass(check, strings.Join(said, "; "))}
}

// findingName names a finding by its rule and file, or what it has of them.
func findingName(f Finding) string {
	switch {
	case f.Rule != "" && f.File != "":
		return f.Rule + " at " + f.File
	case f.Rule != "":
		return f.Rule
	case f.File != "":
		return "on " + f.File
	}
	return "with no rule or file"
}
