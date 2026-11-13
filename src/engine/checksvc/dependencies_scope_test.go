package checksvc_test

// T049b: DependenciesAllowed judges only the step's own writable files. At the code step the tests
// under tests/ (the tests step's files, guarded by TestsUnchanged) may import godog, which the code
// step's allowed list lacks; a forbidden import under internal/ still fails.

import (
	"strings"
	"testing"
)

func depsProject(t *testing.T, dir string) {
	write(t, dir, "go.mod", []byte("module example.com/p\n\ngo 1.26\n\nrequire github.com/cucumber/godog v0.16.0 // indirect\n"))
	write(t, dir, "internal/ok.go", []byte("package internal\n\nimport _ \"fmt\"\n"))
	write(t, dir, "tests/steps_test.go", []byte("package tests\n\nimport (\n\t\"testing\"\n\n\t\"github.com/cucumber/godog\"\n)\n\nfunc TestX(t *testing.T) { _ = godog.Options{} }\n"))
}

func codeDeps(t *testing.T, dir, root string) verdict {
	st := step("code", codeWritable, []string{"code"}, []string{"std", "github.com/oapi-codegen/runtime/..."}, forbid)
	body := checkBody(t, dir, st, nil, string(readTestdata(t, "contract.yaml")), approvedExamples(t))
	return runCheck(t, serve(t, checkConfig(t, root)), "DependenciesAllowed", body)
}

func TestCodeStepDependenciesIgnoreTheTestsFolder(t *testing.T) {
	root, dir := fixture(t)
	depsProject(t, dir)
	if v := codeDeps(t, dir, root); !v.Passed {
		t.Fatalf("a godog import under tests/ failed the code step's DependenciesAllowed: %s", v.Reason)
	}
}

func TestCodeStepDependenciesStillJudgeItsOwnFiles(t *testing.T) {
	root, dir := fixture(t)
	depsProject(t, dir)
	write(t, dir, "internal/bad.go", []byte("package internal\n\nimport _ \"os/exec\"\n"))
	v := codeDeps(t, dir, root)
	if v.Passed || !strings.Contains(v.Reason, "os/exec") {
		t.Fatalf("a forbidden import under internal/ must fail and be named: passed=%v %s", v.Passed, v.Reason)
	}
	if strings.Contains(v.Reason, "godog") {
		t.Errorf("the tests folder's godog import must not be judged: %s", v.Reason)
	}
}
