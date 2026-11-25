//go:build evals

package steps

// Quarantine in the live harness's report (T045), with no model and no Docker. printReport lives
// behind the evals tag, so these run with: go test -tags evals -run PrintReport ./evals/steps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// logCapture records what printReport logs, so the printed verdict line can be checked.
type logCapture struct {
	testing.TB
	out strings.Builder
}

func (l *logCapture) Log(a ...any) {
	for _, x := range a {
		if s, ok := x.(string); ok {
			l.out.WriteString(s)
			l.out.WriteString("\n")
		}
	}
}

func (l *logCapture) Logf(format string, a ...any) { l.TB.Logf(format, a...) }

func quarantineReps(quarantinedPasses, countedPasses bool) []CaseReport {
	mk := func(name string, pass bool, q string) CaseReport {
		return CaseReport{Name: name, Agent: "review", Passed: pass, Quarantine: q,
			Trials: []Trial{{N: 1, Passed: pass}, {N: 2, Passed: pass}, {N: 3, Passed: pass}}}
	}
	return []CaseReport{
		mk("synthetic/quarantined", quarantinedPasses, "T999"),
		mk("counted-1", true, ""),
		mk("counted-2", countedPasses, ""),
	}
}

func readQuarantineReport(t *testing.T, dir string) []map[string]any {
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

func TestPrintReportIgnoresAFailingQuarantinedCase(t *testing.T) {
	dir := t.TempDir()
	if !printReport(t, quarantineReps(false, true), dir) {
		t.Error("printReport = false, want true: only the quarantined case failed")
	}
	rep := readQuarantineReport(t, dir)
	if len(rep) != 3 {
		t.Fatalf("report holds %d cases, want 3 (a quarantined case is still reported)", len(rep))
	}
	if rep[0]["quarantined_by"] != "T999" {
		t.Errorf("quarantined case: quarantined_by = %v, want T999", rep[0]["quarantined_by"])
	}
	if rep[0]["passed"] != false {
		t.Errorf("the quarantined case's own result must stay recorded as failed, got %v", rep[0]["passed"])
	}
	for _, c := range rep[1:] {
		if _, has := c["quarantined_by"]; has {
			t.Errorf("counted case %v has a quarantined_by key", c["case"])
		}
		if c["passed"] != true {
			t.Errorf("counted case %v: passed = %v, want true", c["case"], c["passed"])
		}
	}
}

func TestPrintReportFailsOnAFailingCountedCase(t *testing.T) {
	for _, quarantinedPasses := range []bool{false, true} {
		if printReport(t, quarantineReps(quarantinedPasses, false), t.TempDir()) {
			t.Errorf("printReport = true, want false: a counted case failed (quarantined passes=%v)", quarantinedPasses)
		}
	}
}

func TestPrintReportPassesWhenEverythingPasses(t *testing.T) {
	dir := t.TempDir()
	if !printReport(t, quarantineReps(true, true), dir) {
		t.Error("printReport = false, want true")
	}
	rep := readQuarantineReport(t, dir)
	if rep[0]["quarantined_by"] != "T999" {
		t.Error("a passing quarantined case still carries quarantined_by")
	}
	if rep[0]["passed"] != true {
		t.Errorf("the quarantined case's own result must stay recorded as passed, got %v", rep[0]["passed"])
	}
}

func TestPrintReportWithOnlyQuarantinedCasesFailing(t *testing.T) {
	reps := []CaseReport{{Name: "synthetic/quarantined", Quarantine: "T999", Trials: []Trial{{N: 1}}}}
	if !printReport(t, reps, t.TempDir()) {
		t.Error("printReport = false, want true: no counted case failed")
	}
}

func TestPrintReportSaysAQuarantinedCaseIsNotCounted(t *testing.T) {
	for _, pass := range []bool{false, true} {
		c := &logCapture{TB: t}
		printReport(c, quarantineReps(pass, true), t.TempDir())
		want := "[" + verdictWord(pass) + ", quarantined by T999, not counted] synthetic/quarantined"
		line := ""
		for _, l := range strings.Split(c.out.String(), "\n") {
			if strings.Contains(l, want) {
				line = l
			}
		}
		if line == "" {
			t.Fatalf("no line for the quarantined case (%q) in:\n%s", want, c.out.String())
		}
		if strings.Contains(c.out.String(), "counted-1") && strings.Count(c.out.String(), "not counted") != 1 {
			t.Errorf("only the quarantined case may say it is not counted:\n%s", c.out.String())
		}
	}
}
