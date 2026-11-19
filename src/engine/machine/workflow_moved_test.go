package machine_test

// T039: three cases of the old driver's resume tests carried over to the run workflow, with a stop
// (an activity ending with a heartbeat timeout) or the developer's resume signal in place of a
// restart and Machine.Resume. The interface and the harness are in workflow_durable_test.go.
//
//	TestWorkflowStopDuringAnOpenRoundsCommitKeepsItsWitnesses  witness_guard_test.go TestResumeAfterAnOpenReviewRoundKeepsItsWitnesses
//	TestWorkflowStopKeepsChangedLinesOfAReusedFix              restart_usage_test.go TestRestartKeepsChangedLinesOfAReusedFix
//	TestWorkflowResumeKeepsAcceptedStepsWithFiles              reuse_project_test.go TestResumeInTheSameProjectKeepsAcceptedStepsWithFiles

import (
	"context"
	"slices"
	"testing"

	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
)

// wfWithFiles makes each step in steps (every step when steps is empty) report the files it started
// from plus one file of its own; the review, in a pattern whose review writes, also writes extra.
func wfWithFiles(c durable.WorkerCall, r machine.Result, extra *machine.File, steps ...string) machine.Result {
	if len(steps) > 0 && !slices.Contains(steps, c.Step.Name) {
		return r
	}
	files := slices.Clone(c.Before)
	own := machine.File{Path: c.Step.Name + "/f.go", SHA256: wfHash("file/" + c.Step.Name)}
	if !hasFile(files, own.Path) {
		files = append(files, own)
	}
	if extra != nil && c.Step.Name == "review" && !hasFile(files, extra.Path) {
		files = append(files, *extra)
	}
	slices.SortFunc(files, func(a, b machine.File) int {
		switch {
		case a.Path < b.Path:
			return -1
		case a.Path > b.Path:
			return 1
		}
		return 0
	})
	r.Before, r.ProjectFiles = c.Before, files
	return r
}

// A stop during the commit of a review round left open (a blocking finding with a witness, the
// witness among the round's project files): the round's commit carries the witness and sets
// open_round, it is made before the fix's restore, and the fix starts from files that include
// the witness; the run ends done.
func TestWorkflowStopDuringAnOpenRoundsCommitKeepsItsWitnesses(t *testing.T) {
	p := wfPattern()
	for i := range p.Steps {
		if p.Steps[i].Name == "review" {
			p.Steps[i].Outputs = []string{"findings", "witnesses"}
			p.Steps[i].Writable = []string{"review/**"}
		}
	}
	witness := machine.File{Path: witnessPath, SHA256: wfHash("witness")}
	isRound1Commit := func(in any) bool {
		r := in.(machine.CommitRequest)
		return r.Step.Name == "review" && r.Round == 1
	}
	for _, tc := range []struct {
		name  string
		after bool // the commit took effect before the stop
	}{{"before the commit", false}, {"after the commit", true}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWF(t, p)
			h.work = func(c durable.WorkerCall) (machine.Result, error) {
				r := wfWithFiles(c, wfOK(c), &witness)
				if c.Step.Name == "review" && c.Round == 1 {
					f := wfBlocking("R-1", 1)
					f.File, f.Witness = "code/f.go", &machine.Witness{File: witnessPath, Test: witnessName}
					r.Findings = []machine.Finding{f}
				}
				return r, nil
			}
			if tc.after {
				h.stopAfter = wfFirst(durable.CommitStep, 1, isRound1Commit)
			} else {
				h.stop = wfFirst(durable.CommitStep, 1, isRound1Commit)
			}
			h.run()
			h.wantOutcome(machine.OutcomeDone)

			if n := h.stopPairs(); n != 1 {
				t.Fatalf("%d stop pairs, want 1: %v", n, h.kinds())
			}
			var rej *machine.Event
			for i, e := range h.events() {
				if e.Kind == "StepRejected" && e.Step == "review" && e.Round == 1 {
					rej = &e
					if !hasFile(h.records[i].Files, witnessPath) {
						t.Fatalf("the open round's StepRejected does not carry the witness: %+v", h.records[i].Files)
					}
				}
			}
			if rej == nil || !rej.OpenRound {
				t.Fatalf("no StepRejected leaving review round 1 open: %v", h.kinds())
			}
			var round1 []machine.CommitRequest
			for _, s := range h.stamps {
				if s.Step.Name == "review" && s.Round == 1 {
					round1 = append(round1, s)
				}
			}
			if len(round1) != 1 || !hasFile(round1[0].ProjectFiles, witnessPath) {
				t.Fatalf("review round 1's commits = %+v, want one with the witness in its project files", round1)
			}
			if !round1[0].OpenRound {
				t.Fatalf("the open round's commit does not send open_round: %+v", round1[0])
			}
			var fixes []machine.RestoreRequest
			for _, c := range h.callsOf(durable.RestoreProject) {
				if r := c.In.(machine.RestoreRequest); r.Step.Name == "code" && r.Round == 1 {
					fixes = append(fixes, r)
				}
			}
			if len(fixes) != 1 || !hasFile(fixes[0].Before, witnessPath) {
				t.Fatalf("the fix's restores = %+v, want one starting from the witness", fixes)
			}
			committed := h.callIndex(0, func(c wfCall) bool {
				r, ok := c.In.(machine.CommitRequest)
				return ok && c.Err == nil && r.Step.Name == "review" && r.Round == 1
			})
			fixRestore := h.callIndex(0, func(c wfCall) bool {
				r, ok := c.In.(machine.RestoreRequest)
				return ok && r.Step.Name == "code" && r.Round == 1
			})
			if committed < 0 || fixRestore < committed {
				t.Fatalf("the fix was restored (call %d) before the round's commit succeeded (call %d)", fixRestore, committed)
			}
		})
	}
}

// The fix of review round 1 is served from reuse (a kept result with changed lines), then the
// platform stops during review round 2: the ResultReused event carries the lines, the fix is not
// run, and round 2 and its repeat get the reused fix's changed lines.
func TestWorkflowStopKeepsChangedLinesOfAReusedFix(t *testing.T) {
	lines := []machine.Line{{File: "internal/thing.go", Line: 12}}
	h := newWF(t, wfPattern())
	h.work = func(c durable.WorkerCall) (machine.Result, error) {
		r := wfOK(c)
		if c.Step.Name == "review" && c.Round == 1 {
			r.Findings = []machine.Finding{wfBlocking("R-1", 12)}
		}
		return r, nil
	}
	var lookups []durable.ReuseLookup
	// LookupReuse keeps one result: the fix of the open finding, accepted by an earlier run.
	h.env.RegisterActivityWithOptions(func(_ context.Context, l durable.ReuseLookup) (durable.Reused, error) {
		h.mu.Lock()
		lookups = append(lookups, l)
		h.mu.Unlock()
		if l.Parts.Step == "code" && len(l.Open) > 0 {
			return durable.Reused{Key: wfHash("fix-key"), Inputs: l.Parts.Inputs, Found: true, Run: "RUN-0",
				Artifacts: []string{wfHash("code/fixed")}, Lines: lines}, nil
		}
		return durable.Reused{}, nil
	}, activity.RegisterOptions{Name: durable.LookupReuse, DisableAlreadyRegisteredCheck: true})
	isRound2 := func(in any) bool { w := in.(durable.WorkerCall); return w.Step.Name == "review" && w.Round == 2 }
	h.stop = wfFirst(durable.CallWorker, 1, isRound2)
	h.run()
	h.wantOutcome(machine.OutcomeDone)

	if !slices.ContainsFunc(lookups, func(l durable.ReuseLookup) bool { return l.Parts.Step == "code" && len(l.Open) > 0 }) {
		t.Fatal("the fix's reuse key was never looked up")
	}
	if got := roundsOf(h.workerCalls("code")); !slices.Equal(got, []int{0}) {
		t.Fatalf("code ran in rounds %v, want [0]: the fix reused", got)
	}
	reused := 0
	for _, e := range h.eventsOf("ResultReused") {
		if e.Step == "code" {
			reused++
			if !slices.Equal(e.ChangedLines, lines) {
				t.Fatalf("ResultReused changed_lines = %v, want %v", e.ChangedLines, lines)
			}
		}
	}
	if reused != 1 {
		t.Fatalf("ResultReused events of the fix = %d, want 1", reused)
	}
	if n := h.stopPairs(); n != 1 {
		t.Fatalf("%d stop pairs, want 1: %v", n, h.kinds())
	}
	review := h.workerCalls("review")
	if got := roundsOf(review); !slices.Equal(got, []int{1, 2, 2}) {
		t.Fatalf("review rounds = %v, want [1 2 2]: round 2 repeated after the stop", got)
	}
	for _, c := range review[1:] {
		if !slices.Equal(c.ChangedLines, lines) {
			t.Fatalf("review round %d got changed lines %v, want %v", c.Round, c.ChangedLines, lines)
		}
	}
}

// Steps whose workers report project files are accepted, then the model is unavailable on every
// attempt of a later step and the run waits until the developer's resume: after it only the steps
// not yet accepted are called again, they start from the accepted steps' files, and the run ends
// done.
func TestWorkflowResumeKeepsAcceptedStepsWithFiles(t *testing.T) {
	h := newWF(t, wfPattern())
	down := true
	h.work = func(c durable.WorkerCall) (machine.Result, error) {
		if c.Step.Name == "code" && down {
			return machine.Result{}, temporal.NewNonRetryableApplicationError("the model is unavailable", durable.ModelUnavailable, nil)
		}
		return wfWithFiles(c, wfOK(c), nil, "generate", "tests"), nil
	}
	var before int
	var waiting []string
	h.signalAt(wfDay, durable.SignalResume, func() { before, waiting, down = len(h.callsOf(durable.CallWorker)), h.kinds(), false })
	h.run()
	h.wantOutcome(machine.OutcomeDone)

	if len(waiting) == 0 || waiting[len(waiting)-1] != "RunInterrupted" {
		t.Fatalf("before the resume the record ends %v, want RunInterrupted", waiting)
	}
	calls := h.callsOf(durable.CallWorker)
	if before == 0 || before >= len(calls) {
		t.Fatalf("%d worker calls before the resume of %d in all", before, len(calls))
	}
	for _, c := range calls[before:] {
		if w := c.In.(durable.WorkerCall); !slices.Contains([]string{"code", "review"}, w.Step.Name) {
			t.Errorf("after the resume %s ran again", w.Step.Name)
		}
	}
	for _, s := range []string{"generate", "tests"} {
		if got := h.commitsOf(s); got != 1 {
			t.Errorf("%s committed %d times, want 1", s, got)
		}
	}
	code := h.workerCalls("code")
	if last := code[len(code)-1]; !hasFile(last.Before, "generate/f.go") || !hasFile(last.Before, "tests/f.go") {
		t.Fatalf("code after the resume started from %v, want the accepted steps' files", last.Before)
	}
}
