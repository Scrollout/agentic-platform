package api

// Feature 011, T072 (T025 round 1, finding B1; ADR 0101, FR-009, research R1): a run built from a
// captured pattern whose template holds a witness test (a file under review/r1/) is judged by
// AllTestsPass at its filled review step, so a witness that fails against the filled code keeps
// the run from ending done. Written before the code (T073) by a test author; the code author must
// not change it (ADR 0029).
//
// The real path, on the rig of captured_startrun_test.go (the real run workflow, faked step worker
// and checker, no model): a base run of S-1 whose review step writes one witness test, captureRun,
// a proof run, approval, then the start of a later slice. The rig's checker cannot run a Go test,
// so it stands for the project's tests: it fails AllTestsPass at the review step when told to (as
// a failing filled witness would) and records every check request of the review step.
//
// Edge cases checked: the review step's check request names AllTestsPass (once, so the
// declaration does not repeat it); it is asked after the base review step's kept checks
// (ScopeKept, DependenciesAllowed, TestsBuild, TestsUnchanged, in that order), and neither
// FindingsCarryEvidence nor NoBlockingFindingOpen is asked at all; failing it hands the run over at
// that first failure with one attempt of the review step and no agent call, and the run does not
// end done; passing it ends the run done; the template really holds the witness (the framed
// review/r1/ file), so the run is judged on a captured witness.
//
// Left out, and why: a source run with no witnesses and a review step with no checks list (the
// declaration's pure function, judged by captured_capture_test.go and the captured package's
// tests; an empty template's run is TestSRUnnamedStart...); a real Go test run (the checker
// service's own tests, outside this rig).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sync"
	"testing"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/machine"
)

const (
	wtPath    = "review/r1/empty_text_test.go"
	wtContent = "package review // witness\n"
)

// wtRig is the srRig whose source runs write one witness test, and whose checker records the
// checks asked of the review step and can fail AllTestsPass there.
type wtRig struct {
	*srRig
	mu      sync.Mutex
	failAll bool
	asked   []string // the review step's check names, in the order asked
}

func newWT(t *testing.T) *wtRig {
	t.Helper()
	w := &wtRig{srRig: newSR(t, srTasks(t))}
	w.w.set(func(ctx context.Context, r machine.Request) (machine.Result, error) {
		res, err := w.work(ctx, r)
		if err != nil || r.Step.Name != "review" {
			return res, err
		}
		files := []canonical.File{{Path: wtPath, Content: []byte(wtContent)}}
		h, err := w.st.PutArtifact(cpSourceS, canonical.Framed(files))
		if err != nil {
			return res, err
		}
		res.Outputs["witnesses"] = h
		sum := sha256.Sum256([]byte(wtContent))
		res.ProjectFiles = append(res.ProjectFiles, machine.File{Path: wtPath, SHA256: hex.EncodeToString(sum[:])})
		return res, nil
	})
	w.c.set(func(_ context.Context, r machine.CheckRequest) (machine.CheckResult, error) {
		w.mu.Lock()
		fail := w.failAll
		if r.Step.Name == "review" {
			w.asked = append(w.asked, r.Name)
		}
		w.mu.Unlock()
		if fail && r.Step.Name == "review" && r.Name == "AllTestsPass" {
			return machine.CheckResult{Passed: false, Reason: "a witness test fails"}, nil
		}
		return machine.CheckResult{Passed: true}, nil
	})
	return w
}

// approvedWithWitness makes an approved captured pattern whose template holds the witness test,
// and a later slice that fits it.
func (w *wtRig) approvedWithWitness(t *testing.T) (later string) {
	t.Helper()
	later = w.addSlice("Let me keep tasks.", true)
	w.registerSteps(later, cpFillOnly)
	id := w.approved("notes")
	want := canonical.FramedHash([]canonical.File{{Path: wtPath, Content: []byte(wtContent)}})
	if got := w.captured(id).Template.Witnesses; got != want {
		t.Fatalf("setup: the template's witnesses are %s, want the framed hash %s of %s", got, want, wtPath)
	}
	w.mu.Lock()
	w.asked = nil
	w.mu.Unlock()
	return later
}

func (w *wtRig) reviewChecks() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.asked)
}

// ADR 0101: with AllTestsPass failing at the filled review step (as a failing filled witness
// would), the run does not end done: it is handed over at that failure after one attempt, with no
// agent call; the review step's check request names AllTestsPass.
func TestAFailingWitnessAtTheFilledReviewStepKeepsACapturedRunFromDone(t *testing.T) {
	w := newWT(t)
	later := w.approvedWithWitness(t)
	w.mu.Lock()
	w.failAll = true
	w.mu.Unlock()
	calls := w.w.callCount()

	op := w.startRun(later, "tasks-app", "")
	v := w.waitRun(op.Target, "waiting for the developer or ended", func(v runView) bool {
		return v.Status == "waiting-for-developer" || v.Status == "ended"
	})
	if v.Status == "ended" && v.Outcome != nil && *v.Outcome == "done" {
		t.Fatalf("run %s ended done although AllTestsPass fails at its review step (checks asked there: %v)", op.Target, w.reviewChecks())
	}
	if v.Status != "waiting-for-developer" || v.Reason == "" {
		t.Errorf("run %s is %q (outcome %v, reason %q), want waiting-for-developer with a reason", op.Target, v.Status, v.Outcome, v.Reason)
	}
	w.noAgentCalls(calls, "the run")
	if !slices.Contains(w.reviewChecks(), "AllTestsPass") {
		t.Fatalf("the review step's checks asked: %v, want AllTestsPass among them", w.reviewChecks())
	}
	w.opBody(w.do("POST", apiPath("/runs/"+op.Target+"/hand-over"), ""), 202)
	w.waitEnded(op.Target, "handed-over")

	var failed []string
	for _, e := range w.allEvents(op.Target) {
		if e.Kind == "CheckFailed" && e.Check != nil {
			failed = append(failed, e.Step+"/"+e.Check.Name)
		}
	}
	if len(failed) != 1 || failed[0] != "review/AllTestsPass" {
		t.Errorf("failed checks %v, want exactly review/AllTestsPass", failed)
	}
	attempts := 0
	for _, step := range w.kinds(op.Target, "AttemptMade") {
		if step == "review" {
			attempts++
		}
	}
	if attempts != 1 {
		t.Errorf("%d attempts of the filled review step, want 1", attempts)
	}
}

// ADR 0101: with every check passing, the same run ends done, and the review step's checks are
// the base review step's kept checks in their order, then AllTestsPass once, last; neither
// FindingsCarryEvidence nor NoBlockingFindingOpen is asked.
func TestACapturedRunWithAPassingWitnessEndsDoneAfterAllTestsPassAtTheReviewStep(t *testing.T) {
	w := newWT(t)
	later := w.approvedWithWitness(t)
	calls := w.w.callCount()

	op := w.startRun(later, "tasks-app", "")
	w.waitEnded(op.Target, "done")
	w.noAgentCalls(calls, "the run")
	want := []string{"ScopeKept", "DependenciesAllowed", "TestsBuild", "TestsUnchanged", "AllTestsPass"}
	if got := w.reviewChecks(); !slices.Equal(got, want) {
		t.Errorf("the review step's checks asked %v, want %v", got, want)
	}
}
