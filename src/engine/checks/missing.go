package checks

// The step checks T054e added: ExamplesMatchApproval, GeneratedCodeBuilds, TestsBuild and
// GeneratedCodeUnchanged. The check contract's runCheck states each rule; these are its one
// implementation. The review checks, FindingsCarryEvidence and NoBlockingFindingOpen, judge
// findings by their witness tests (witness.go, ADR 0039).

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// TestsBuild: every test package of the project compiles (`go test -c -count=1` into a folder outside the
// project); no test is run. A project with no test package fails: there is nothing to judge.
func TestsBuild(r GoRunner, project string) []CheckResult {
	const check = "TestsBuild"
	out, err := os.MkdirTemp("", "testsbuild-")
	if err != nil {
		return []CheckResult{fail(check, "no folder for the test binaries: "+err.Error())}
	}
	defer os.RemoveAll(out)
	res, bad := runGo(check, r, project, "test", "-c", "-count=1", "-vet=off", "-o", out+string(filepath.Separator), "./...")
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

// ---- shared with the witness rule (witness.go) ----

// testEvent is one line of `go test -json` (cmd/test2json).
type testEvent struct {
	Action  string
	Package string
	Test    string
	Output  string
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
