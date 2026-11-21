//go:build evals

package analyst

// Quarantine in the live harness's report (T185), with no model and no Docker. print lives behind
// the evals tag, so these run with: go test -tags evals -run TestPrint ./evals/analyst

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func quarantineReps(quarantinedPasses, counted2Passes bool) []CaseReport {
	mk := func(name string, pass bool, q string) CaseReport {
		return CaseReport{Name: name, Passed: pass, Quarantine: q,
			Trials: []Trial{{N: 1, Passed: pass}, {N: 2, Passed: pass}, {N: 3, Passed: pass}}}
	}
	return []CaseReport{
		mk("example", quarantinedPasses, "T185"),
		mk("counted-1", true, ""),
		mk("counted-2", counted2Passes, ""),
	}
}

func readReport(t *testing.T, dir string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "report.json")) //nolint:gosec // G304: the test's own t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPrintIgnoresAFailingQuarantinedCase(t *testing.T) {
	dir := t.TempDir()
	if !print(t, quarantineReps(false, true), dir) {
		t.Error("print = false, want true: only the quarantined case failed")
	}
	rep := readReport(t, dir)
	if len(rep) != 3 {
		t.Fatalf("report holds %d cases, want 3 (a quarantined case is still reported)", len(rep))
	}
	if rep[0]["quarantined_by"] != "T185" {
		t.Errorf("quarantined case: quarantined_by = %v, want T185", rep[0]["quarantined_by"])
	}
	if rep[0]["passed"] != false {
		t.Errorf("the quarantined case's own result must stay recorded as failed, got %v", rep[0]["passed"])
	}
	for _, c := range rep[1:] {
		if _, has := c["quarantined_by"]; has {
			t.Errorf("counted case %v has a quarantined_by key", c["case"])
		}
	}
}

func TestPrintFailsOnAFailingCountedCase(t *testing.T) {
	for _, quarantinedPasses := range []bool{false, true} {
		if print(t, quarantineReps(quarantinedPasses, false), t.TempDir()) {
			t.Errorf("print = true, want false: a counted case failed (quarantined passes=%v)", quarantinedPasses)
		}
	}
}

func TestPrintPassesWhenEverythingPasses(t *testing.T) {
	dir := t.TempDir()
	if !print(t, quarantineReps(true, true), dir) {
		t.Error("print = false, want true")
	}
	if readReport(t, dir)[0]["quarantined_by"] != "T185" {
		t.Error("a passing quarantined case still carries quarantined_by")
	}
}

func TestPrintWithOnlyQuarantinedCasesFailing(t *testing.T) {
	reps := []CaseReport{{Name: "example", Quarantine: "T185", Trials: []Trial{{N: 1}}}}
	if !print(t, reps, t.TempDir()) {
		t.Error("print = false, want true: no counted case failed")
	}
}
