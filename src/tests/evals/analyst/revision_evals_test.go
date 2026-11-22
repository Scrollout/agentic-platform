//go:build evals

package analyst

// T155: the live revision cases. Each trial runs through the platform's Create and Answer with
// its retry (the first attempt's message is the one the engine builds for the case, as
// RevisionInput returns it), the attempts reported, and is graded by GradeRevision; a case passes only when all k trials pass (pass^k). The test's name starts
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

// revisionAnalyst returns the earlier proposal on Create (no current proposal) and hands every
// call of Answer, each attempt of its retry, to next.
type revisionAnalyst struct {
	earlier engine.Proposal
	next    engine.Analyst
}

func (a *revisionAnalyst) Propose(ctx context.Context, in engine.AnalystInput) (engine.Proposal, error) {
	if in.Current == nil {
		return a.earlier, nil
	}
	return a.next.Propose(ctx, in)
}

// runRevision runs every revision case l.trials times, each trial through Create and Answer with
// the platform's retry. Outputs, and the message each case's first attempt sends, go to dir.
func (l *lab) runRevision(cases []RevisionCase, agentDir, agentFile, dir string) []CaseReport {
	g := l.grader()
	must(l.t, os.MkdirAll(dir, 0o755))
	var reps []CaseReport
	for _, c := range cases {
		in, err := g.RevisionInput(context.Background(), c)
		must(l.t, err)
		earlier, err := EarlierProposal()
		must(l.t, err)
		msg := engine.AnalystMessage(in)
		must(l.t, os.WriteFile(filepath.Join(dir, c.Name+".message.txt"), []byte(msg), 0o644))
		cr := CaseReport{Name: c.Name, Adversarial: c.Adversarial, Attack: c.Attack, Passed: true, Quarantine: c.Quarantine}
		for i := 1; i <= l.trials; i++ {
			start := time.Now()
			da := &dockerAnalyst{l: l, agentDir: agentDir, agentFile: agentFile}
			svc, err := engine.New(engine.Config{Schemas: g.Schemas, Analyst: &revisionAnalyst{earlier: earlier, next: da},
				Diff: revisionNoDiff{}, Pattern: g.Pattern})
			must(l.t, err)
			var answerErr error
			res, err := svc.Create(context.Background(), RevisionDescription)
			if err != nil {
				answerErr = fmt.Errorf("the engine did not store the earlier proposal: %w", err)
			} else {
				_, answerErr = svc.Answer(context.Background(), res.Slice.ID, c.Answers)
			}
			out, events := da.last, da.events
			tr := Trial{N: i, Attempts: da.calls, Duration: time.Since(start).Round(time.Millisecond).String(), Usage: da.usage}
			if answerErr != nil {
				tr.ExitErr = answerErr.Error()
				tr.Stderr = tail(string(da.lastStderr), 600)
			}
			tr.Results = GradeRevision(g, in, out)
			tr.Passed = answerErr == nil && Passed(tr.Results)
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

// TestAnalystEvalsRevision is the live revision suite (T155): every revision case EVAL_CASES
// names (default all) k times (EVAL_TRIALS, default Trials) through the real gateway.
func TestAnalystEvalsRevision(t *testing.T) {
	_, cases, err := Selection(os.Getenv("EVAL_CASES"))
	if err != nil {
		t.Fatalf("%v", err)
	}
	k, err := TrialCount(os.Getenv("EVAL_TRIALS"))
	if err != nil {
		t.Fatalf("%v", err)
	}
	key := keyFile(t) // fails loudly, never skips
	root := repoRoot(t)
	agentFile := filepath.Join(root, "src", "agents", "analyst.yaml")
	if _, err := os.Stat(agentFile); err != nil {
		t.Fatalf("the analyst agent definition is missing: %v", err)
	}
	if len(cases) == 0 {
		t.Skipf("EVAL_CASES %q names no revision case", os.Getenv("EVAL_CASES"))
	}
	l := newLab(t)
	l.trials = k
	l.gateway(key)
	dir := reportDir(l)
	sub := filepath.Join(dir, "revision")
	reps := l.runRevision(cases, filepath.Dir(agentFile), "analyst.yaml", sub)
	ok := print(t, reps, sub)
	mergeReport(t, dir, reps)
	if !ok {
		t.Fatalf("the analyst failed at least one revision case (pass^%d needs all trials to pass)", k)
	}
}

// TestHarnessStubRevision proves the revision harness with no model and no key: the stub gateway
// returns a canned revision, and a good one must pass every trial and a bad one fail them.
func TestHarnessStubRevision(t *testing.T) {
	dir, err := filepath.Abs("testdata")
	must(t, err)
	for _, c := range []struct {
		fixture  string
		want     bool
		attempts int
		fault    string // part of the refusal Answer gives after the last attempt
	}{
		{"good-revised", true, 1, ""},
		// 3 is limits.attempts of src/catalogue/api-feature-go/pattern.yaml: a bad revision is
		// asked for again until the last attempt.
		{"bad-revised-dropped-rules", false, 3, "the revision drops R-1 of the current proposal"},
		{"bad-revised-repeated-key", false, 3, `the answer holds the key "examples" twice at "/"`},
	} {
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
				switch {
				case c.want && tr.ExitErr != "":
					t.Fatalf("trial %d: the good revision was refused: %s %s", tr.N, tr.ExitErr, tr.Stderr)
				case !c.want && !strings.HasPrefix(tr.ExitErr, "OperationFailed: "):
					t.Fatalf("trial %d: want the platform's refusal (OperationFailed), got %q %s", tr.N, tr.ExitErr, tr.Stderr)
				case !c.want && !strings.Contains(tr.ExitErr, c.fault):
					t.Fatalf("trial %d: the refusal lacks %q: %s", tr.N, c.fault, tr.ExitErr)
				}
				if tr.Attempts != c.attempts {
					t.Fatalf("trial %d: %d attempts, want %d", tr.N, tr.Attempts, c.attempts)
				}
				if len(tr.Results) != 11 {
					t.Fatalf("trial %d: want 11 graders, got %d", tr.N, len(tr.Results))
				}
			}
		})
	}
}
