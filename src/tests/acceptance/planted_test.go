//go:build docker

// T046: the planted-failure suite (SC-003, spec US2.2 to US2.6): each of five failures is stopped by
// a check in 3 of 3 trials, deterministically. A failure is injected by the step agents' answers
// in the scenario's step set, src/tests/recorded/steps/planted/<scenario>/ (answers.json, and the
// mappings made from it), which the recorded model serves (research D26, D34), so no live model is
// involved; the 3-of-3 trials against the live model are the step agents' evals (T041). A missing
// set fails the test and names the file; nothing is skipped.
//
// Each scenario has its own lab, and its three trials are runs of the same slice into different
// projects, each with its own step workers; a scenario with freshLabPerTrial instead starts a new
// lab, with a new result store, for each trial. A stopped run waits for the developer, so each trial
// ends by accepting the hand-over (which the next build needs: one active run at a time, FR-021).
package acceptance

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/tests/lab"
)

// scenario is one planted failure.
type scenario struct {
	name         string // its step set is planted/<name>
	spec         string // what it plants
	step         string // the step whose check must stop it
	check        string // the check that must fail (code steps)
	writableCode bool   // the code step's project root is writable, `.git` read-only (lab.WritableRoot)
	// freshLabPerTrial starts a new lab, with a new result store, for each trial, so each trial is
	// RUN-1 of its own lab, and the trials share one project name so one recording set serves them.
	// In one lab, trials 2 and 3 would reuse trial 1's kept open review rounds (research D36) and
	// make no review attempt (T100).
	freshLabPerTrial bool
}

var plantedScenarios = []scenario{
	// The code step's root is writable only here and in outside-scope: a test-only deviation that lets
	// the answer's extra write land, so the second guard (TestsUnchanged, ScopeKept) is what stops it.
	{name: "edit-judging-tests", step: "code", check: "TestsUnchanged", writableCode: true,
		spec: "the code step's answer also rewrites a file under tests/ that the tests step wrote (US2.2)"},
	{name: "outside-scope", step: "code", check: "ScopeKept", writableCode: true,
		spec: "the code step's answer also writes README.md, which no glob of the step's writable paths matches (US2.3)"},
	{name: "forbidden-dependency", step: "code", check: "DependenciesAllowed",
		spec: "the code step's answer imports os/exec, which the pattern forbids (US2.4)"},
	{name: "claims-done", step: "code", check: "AllTestsPass",
		spec: "the code step's answer says the work is done and writes no implementation (US2.5)"},
	{name: "non-converging-review", step: "review", freshLabPerTrial: true,
		spec: "each of review rounds 1 to 3 writes a new witness test that fails at a line of the code and names it; the code agent fixes after rounds 1 and 2 so that round's witness passes, and a new confirmed finding opens the next round (US2.6, research D35)"},
}

// attemptsExhausted (US2.7) is not one of the five planted failures: a step whose result keeps
// failing a check, here the code step's implementation failing an example in each of 3 attempts.
var attemptsExhausted = scenario{name: "attempts-exhausted", step: "code", check: "AllTestsPass",
	spec: "the code step's implementation builds but fails one example, in each of its three attempts (US2.7)"}

// plantedLab starts a lab over the scenario's step set, with the approved example slice.
func plantedLab(t *testing.T, sc scenario) (*lab.Lab, string) {
	t.Helper()
	var opts []lab.Option
	if sc.writableCode {
		opts = append(opts, lab.WritableRoot("code"))
	}
	return buildLab(t, plantedSet+"/"+sc.name, opts...)
}

// plantedTrial runs one build with the failure planted and requires the check to stop it: the step
// is not accepted, every attempt (or review round) is recorded, the run waits for the developer with
// the reason and does not complete. It ends by accepting the hand-over so another trial can follow.
func plantedTrial(t *testing.T, l *lab.Lab, slice string, sc scenario, project string) {
	t.Helper()
	op := startBuild(t, l, slice, project)
	r := waitRun(t, l, op.Target, 10*time.Minute, stopped)
	evs := events(t, l, r.ID)
	wellFormedRecord(t, evs)

	if r.Status != "waiting-for-developer" || r.Outcome != "" {
		t.Errorf("%s: the run is %q (outcome %q), want waiting-for-developer with no outcome yet (the failure was not stopped, or the run ended without the developer)", sc.name, r.Status, r.Outcome)
	}
	if r.Reason == "" {
		t.Errorf("%s: the run waits for the developer with no reason", sc.name)
	} else if !strings.Contains(strings.ToLower(r.Reason), sc.step) && (sc.check == "" || !strings.Contains(r.Reason, sc.check)) {
		t.Errorf("%s: the reason %q names neither the step %s nor the check %s", sc.name, r.Reason, sc.step, sc.check)
	}
	if n := count(evs, "RunCompleted", ""); n != 0 {
		t.Errorf("%s: the run completed %d time(s) with the failure planted", sc.name, n)
	}
	if n := count(evs, "StepAccepted", sc.step) + count(evs, "ResultReused", sc.step); n != 0 {
		t.Errorf("%s: step %s was accepted %d time(s) with the failure planted", sc.name, sc.step, n)
	}
	// Every step before the failing one was accepted (or reused from an earlier trial).
	acc := acceptedArtifacts(evs)
	for _, s := range stepOrder {
		if s == sc.step {
			break
		}
		if len(acc[s]) == 0 {
			t.Errorf("%s: step %s, before the failing step, was not accepted: record of kinds %v", sc.name, s, kindsOf(evs))
		}
	}

	if sc.step == "review" {
		// US2.6 and research D35: a finding blocks only when the witness test it names failed having
		// run its line, so each round's blocking finding names a witness in the rule's folder
		// review/r<n>/, a new file every round; the review stops after round 3 (FR-016).
		rounds := map[int]int{}
		witnessFiles := map[string]bool{}
		for _, e := range evs {
			if e.Kind != "FindingRecorded" || e.Finding == nil || !e.Finding.Blocking {
				continue
			}
			f := e.Finding
			rounds[e.Round]++
			if f.File == "" || f.Line == 0 || f.Rule == "" {
				t.Errorf("%s: a blocking finding without a rule, file and line: %+v", sc.name, *f)
			}
			w := f.Witness
			if w == nil {
				t.Errorf("%s: round %d: a blocking finding without the witness test that confirms it: %+v", sc.name, e.Round, *f)
				continue
			}
			m := witnessFile.FindStringSubmatch(w.File)
			n := strings.TrimPrefix(f.Rule, "R-")
			if m == nil || m[1] != n {
				t.Errorf("%s: round %d: the witness %q is not a test file in review/r%s/ for rule %s", sc.name, e.Round, w.File, n, f.Rule)
			}
			if !strings.HasPrefix(w.Test, "TestR"+n) || (len(w.Test) > len("TestR"+n) && w.Test[len("TestR"+n)] >= '0' && w.Test[len("TestR"+n)] <= '9') {
				t.Errorf("%s: round %d: the witness test %q does not start TestR%s, not followed by a digit", sc.name, e.Round, w.Test, n)
			}
			if witnessFiles[w.File] {
				t.Errorf("%s: round %d: the witness file %s was named before; each round writes a new one", sc.name, e.Round, w.File)
			}
			witnessFiles[w.File] = true
		}
		for _, n := range []int{1, 2, 3} {
			if rounds[n] == 0 {
				t.Errorf("%s: no confirmed blocking finding in review round %d; blocking findings per round %v", sc.name, n, rounds)
			}
		}
		if rounds[4] > 0 || count(evs, "AttemptMade", "review") != 3 {
			t.Errorf("%s: %d review attempt(s) were made, want exactly 3 rounds (FR-016)", sc.name, count(evs, "AttemptMade", "review"))
		}
		if !strings.Contains(strings.ToLower(r.Reason), "finding") {
			t.Errorf("%s: the hand-over reason %q does not name the open findings (US2.6)", sc.name, r.Reason)
		}
	} else {
		if n := count(evs, "AttemptMade", sc.step); n != 3 {
			t.Errorf("%s: %d attempts of step %s were made, want 3 (FR-016)", sc.name, n, sc.step)
		}
		if n := failedCheck(evs, sc.step, sc.check); n != 3 {
			t.Errorf("%s: check %s failed %d time(s) on step %s, want once per attempt (3)", sc.name, sc.check, n, sc.step)
		}
		if n := count(evs, "StepRejected", sc.step); n != 3 {
			t.Errorf("%s: step %s was rejected %d time(s), want 3", sc.name, sc.step, n)
		}
	}
	acceptHandOver(t, l, r.ID)
	l.StopStepWorkers(t, slice, project)
}

// witnessFile is a witness test file of rule R-<n>: review/r<n>/<name>_test.go (run-event schema).
var witnessFile = regexp.MustCompile(`^review/r([0-9]+)/[A-Za-z0-9_.-]+_test\.go$`)

func kindsOf(evs []Event) []string {
	var k []string
	for _, e := range evs {
		s := e.Kind
		if e.Step != "" {
			s += ":" + e.Step
		}
		k = append(k, s)
	}
	return k
}

// TestPlanted runs SC-003: each planted failure is stopped in 3 of 3 trials.
func TestPlanted(t *testing.T) {
	for _, sc := range plantedScenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			noMisses := func(t *testing.T, l *lab.Lab) {
				if n := l.Misses(t); n != 0 {
					t.Errorf("%s: the replay server saw %d request(s) no recording matched:\n%s", sc.name, n, l.MissesJSON(t))
				}
			}
			var l *lab.Lab
			var slice string
			if !sc.freshLabPerTrial {
				l, slice = plantedLab(t, sc)
			}
			// Each trial is a subtest TestPlanted/<scenario>/trial-<n>, so `ap test planted` reads
			// its result from the test events (src/cli/contract.md, research D33).
			for trial := 1; trial <= 3; trial++ {
				project := fmt.Sprintf("%s-%d", sc.name, trial)
				if sc.freshLabPerTrial {
					project = fmt.Sprintf("%s-1", sc.name)
				}
				t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) {
					if sc.freshLabPerTrial {
						// The trial's own lab, stopped when the trial ends.
						l, slice := plantedLab(t, sc)
						plantedTrial(t, l, slice, sc, project)
						noMisses(t, l)
						return
					}
					plantedTrial(t, l, slice, sc, project)
				})
			}
			if !sc.freshLabPerTrial {
				noMisses(t, l)
			}
		})
	}
}
