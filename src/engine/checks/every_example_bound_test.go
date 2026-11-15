package checks_test

// T049b: EveryExampleBound (GodogStrict) judges "every example runs, none is undefined or pending",
// not the exit code: the tests step's tests must fail against the stub, so a run that exits non-zero
// because steps fail still passes when every scenario ran with every step defined.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/checks"
)

func boundDir(kind string) string { return filepath.Join("testdata", "everybound", kind) }

func TestEveryExampleBoundIgnoresTheExitCodeWhenEveryExampleRan(t *testing.T) {
	failing := checks.GoResult{ExitCode: 1, Output: jsonTestRun("example.com/p/tests", "TestFeatures", "fail", "2 scenarios (2 failed)", "4 steps (2 failed, 2 skipped)")}
	if rs := checks.GodogStrict(fakeGo{res: map[string]checks.GoResult{"test": failing}}, "unused"); !allPassed(rs) {
		t.Fatalf("scenarios that ran and failed against the stub were rejected: %+v", rs)
	}
}

func TestEveryExampleBoundRejectsPendingAndUndefinedWhateverTheExitCode(t *testing.T) {
	for name, c := range map[string]struct {
		res  checks.GoResult
		want string
	}{
		"pending":   {checks.GoResult{ExitCode: 1, Output: "--- FAIL: TestFeatures\n2 scenarios (1 failed, 1 pending)"}, "pending"},
		"undefined": {checks.GoResult{ExitCode: 1, Output: "2 scenarios (1 failed, 1 undefined)"}, "undefined"},
	} {
		rs := checks.GodogStrict(fakeGo{res: map[string]checks.GoResult{"test": c.res}}, "unused")
		if allPassed(rs) || !strings.Contains(strings.ToLower(reasons(rs)), c.want) {
			t.Errorf("%s: %+v", name, rs)
		}
	}
}

// The real runs are offline, from the module cache, as TestGodogStrictReal.
func TestEveryExampleBoundReal(t *testing.T) {
	r := needGo(t)
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	if rs := checks.GodogStrict(r, boundDir("failing")); !allPassed(rs) {
		t.Errorf("every example ran against the stub and failed, which is the tests step's goal: %+v", rs)
	}
	for kind, want := range map[string]string{"undefined": "undefined", "pending": "pending", "filtered": ""} {
		rs := checks.GodogStrict(r, boundDir(kind))
		if allPassed(rs) {
			t.Errorf("%s: a run that left an example undefined, pending or unrun passed", kind)
		} else if want != "" && !strings.Contains(strings.ToLower(reasons(rs)), want) {
			t.Errorf("%s: the reason must name %q: %+v", kind, want, rs)
		}
	}
}
