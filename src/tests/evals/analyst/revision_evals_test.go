//go:build evals

package analyst

// T155: the live revision cases. Each trial is one analyst call with the user message the engine
// builds for the case (GradeRevision's input from RevisionInput), with no retry, graded by
// GradeRevision; a case passes only when all Trials trials pass (pass^k). The test's name starts
// with TestAnalystEvals so that `run.sh live` (-run 'TestAnalystEvals') runs it after the
// description cases, and its stub test's with TestHarnessStub so that `run.sh stub` runs it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	engine "github.com/agentic-platform/engine/slices"
)

// runRevision runs every revision case Trials times, one analyst call per trial. Outputs, and the
// message each case sends, go to dir.
func (l *lab) runRevision(cases []RevisionCase, agentDir, agentFile, dir string) []CaseReport {
	g := l.grader()
	must(l.t, os.MkdirAll(dir, 0o755))
	var reps []CaseReport
	for _, c := range cases {
		in, err := g.RevisionInput(context.Background(), c)
		must(l.t, err)
		msg := engine.AnalystMessage(in)
		must(l.t, os.WriteFile(filepath.Join(dir, c.Name+".message.txt"), []byte(msg), 0o644))
		cr := CaseReport{Name: c.Name, Adversarial: c.Adversarial, Attack: c.Attack, Passed: true, Quarantine: c.Quarantine}
		for i := 1; i <= Trials; i++ {
			start := time.Now()
			out, events, usage, stderr, err := l.analyst(agentDir, agentFile, msg)
			tr := Trial{N: i, Attempts: 1, Duration: time.Since(start).Round(time.Millisecond).String(), Usage: usage}
			if err != nil {
				tr.ExitErr = err.Error()
				tr.Stderr = tail(string(stderr), 600)
			}
			tr.Results = GradeRevision(g, in, out)
			tr.Passed = err == nil && Passed(tr.Results)
			must(l.t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s.trial%d.out", c.Name, i)), out, 0o644))
			must(l.t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s.trial%d.ndjson", c.Name, i)), events, 0o644))
			cr.Passed = cr.Passed && tr.Passed
			cr.Trials = append(cr.Trials, tr)
		}
		reps = append(reps, cr)
	}
	return reps
}

// mergeReport adds reps to dir/report.json, the file the CI summary reads, keeping the cases the
// description run wrote there and replacing any earlier copy of these cases.
func mergeReport(t *testing.T, dir string, reps []CaseReport) {
	t.Helper()
	p := filepath.Join(dir, "report.json")
	var all []CaseReport
	b, err := os.ReadFile(p)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &all); err != nil {
			t.Fatalf("%s cannot be read: %v", p, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		t.Fatal(err)
	}
	mine := map[string]bool{}
	for _, r := range reps {
		mine[r.Name] = true
	}
	var keep []CaseReport
	for _, r := range all {
		if !mine[r.Name] {
			keep = append(keep, r)
		}
	}
	j, err := json.MarshalIndent(append(keep, reps...), "", "  ")
	must(t, err)
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(p, j, 0o644))
}

// revisionSelection is the revision cases EVAL_CASES names (all when it is unset).
func revisionSelection() []RevisionCase {
	cases := RevisionCases()
	only := os.Getenv("EVAL_CASES")
	if only == "" {
		return cases
	}
	want := map[string]bool{}
	for _, n := range strings.Split(only, ",") {
		want[strings.TrimSpace(n)] = true
	}
	var sel []RevisionCase
	for _, c := range cases {
		if want[c.Name] {
			sel = append(sel, c)
		}
	}
	return sel
}

// TestAnalystEvalsRevision is the live revision suite (T155): every revision case Trials times
// through the real gateway.
func TestAnalystEvalsRevision(t *testing.T) {
	key := keyFile(t) // fails loudly, never skips
	root := repoRoot(t)
	agentFile := filepath.Join(root, "src", "agents", "analyst.yaml")
	if _, err := os.Stat(agentFile); err != nil {
		t.Fatalf("the analyst agent definition is missing: %v", err)
	}
	cases := revisionSelection()
	if len(cases) == 0 {
		t.Skipf("EVAL_CASES %q names no revision case", os.Getenv("EVAL_CASES"))
	}
	l := newLab(t)
	l.gateway(key)
	dir := reportDir(l)
	sub := filepath.Join(dir, "revision")
	reps := l.runRevision(cases, filepath.Dir(agentFile), "analyst.yaml", sub)
	ok := print(t, reps, sub)
	mergeReport(t, dir, reps)
	if !ok {
		t.Fatal("the analyst failed at least one revision case (pass^3 needs all trials to pass)")
	}
}

// TestHarnessStubRevision proves the revision harness with no model and no key: the stub gateway
// returns a canned revision, and a good one must pass every trial and a bad one fail them.
func TestHarnessStubRevision(t *testing.T) {
	dir, err := filepath.Abs("testdata")
	must(t, err)
	for _, c := range []struct {
		fixture string
		want    bool
	}{{"good-revised", true}, {"bad-revised-dropped-rules", false}} {
		t.Run(c.fixture, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(dir, "revision", c.fixture+".json"))
			must(t, err)
			l := newLab(t)
			l.stub(string(b))
			rd := t.TempDir()
			reps := l.runRevision(RevisionCases()[:1], dir, "stub-agent.yaml", rd)
			print(t, reps, rd)
			if reps[0].Passed != c.want {
				t.Fatalf("case passed=%v, want %v", reps[0].Passed, c.want)
			}
			for _, tr := range reps[0].Trials {
				if tr.ExitErr != "" {
					t.Fatalf("the agent container failed: %s %s", tr.ExitErr, tr.Stderr)
				}
				if len(tr.Results) != 11 {
					t.Fatalf("trial %d: want 11 graders, got %d", tr.N, len(tr.Results))
				}
			}
		})
	}
}
