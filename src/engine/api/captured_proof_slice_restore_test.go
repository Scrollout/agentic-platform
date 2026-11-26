package api

// T048 (feature 011, US2; data model "Proof slice"; spec Clarifications 2026-11-26 about the
// hand-changed proof slice; contract makeProofSlice and the `changed` kind of slice.schema.json):
// makeProofSlice restores a proof slice's hand-changed part from the captured pattern's copy,
// through HTTP only, on the rig of captured_proof_slice_test.go (newPSRig and its helpers, used
// unchanged). Written before the code (T049) by a test author; the code author must not change
// it (ADR 0029). The slices service's own cases are in slices/proof_restore_test.go.
//
// A part is changed by hand the way TestPSStartRunOfTheProofSliceRefusesAnotherPatternOrAChangedPart
// does it: the slices service's ReplaceGoals/ReplaceContract on the proof slice (the API has no
// way to edit a proof slice's parts: a developer does it through the same service).
//
// Edge cases checked: the same slice ID, no new slice, parts equal to the copy (the part hashes
// before the change), history `changed` (refs exactly the restored part) then `approved`; a proof
// startRun after a restore (it starts, instead of PatternDoesNotFit); a restore that would change
// a part while a proof run has not ended (409 CapturedPatternBusy or a failed operation with it,
// slice and history untouched, no `changed` entry), then the same ask after the run ended; asking
// with nothing to restore while that run has not ended (allowed, the same slice, no new entry);
// a restore whose check fails only for the copy (PartFailsChecks with failures, slice unchanged,
// then the next ask restores).
//
// Left out, and why: a restore of two parts (the slices tests prove the one `changed` entry
// naming both); a method change plus a hand change (the slices tests; the API adds nothing);
// a part changed back to the copy but unapproved while a run has not ended (the contract says
// "asking with nothing to restore is allowed" and does not say whether approving counts: raised
// as ambiguous in the report instead of fixed in a test); withdrawn and another operation
// running (T027's tests, unchanged by T048).

import (
	"context"
	"net/http"
	goslices "slices"
	"strings"
	"sync"
	"testing"

	"github.com/agentic-platform/engine/slices"
)

// prLint passes every contract, except one that contains failOn, which it fails.
type prLint struct {
	mu     sync.Mutex
	failOn string
}

func (l *prLint) Lint(_ context.Context, contract string) ([]slices.LintFinding, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failOn != "" && strings.Contains(contract, l.failOn) {
		return []slices.LintFinding{{Level: "error", Code: "oas3-schema", Text: "invalid"}}, nil
	}
	return nil, nil
}

func (l *prLint) setFailOn(s string) { l.mu.Lock(); l.failOn = s; l.mu.Unlock() }

// prOwnLint restarts the engine on a slices service, under the same method over the same
// records (so nothing is checked again), whose lint is a prLint.
func (p *psRig) prOwnLint() *prLint {
	p.t.Helper()
	l := &prLint{}
	svc, err := slices.New(slices.Config{Schemas: p.sp.schemas, Analyst: p.sp.analyst, Diff: spDiff{}, Lint: l,
		Pattern: spPattern(p.t), Records: p.st, Method: cmMethodA})
	if err != nil {
		p.t.Fatal(err)
	}
	p.sp.svc = svc
	p.psBoot()
	return l
}

// prChangeGoals changes the proof slice's goals by hand, and returns the slice as it was.
func (p *psRig) prChangeGoals(ps string) psSlice {
	p.t.Helper()
	before := p.psGetSlice(ps)
	sl, err := p.sp.svc.Get(ps)
	if err != nil {
		p.t.Fatal(err)
	}
	goals := goslices.Clone(sl.Goals)
	goals[0].Text += " and keep it for good"
	if _, err := p.sp.svc.ReplaceGoals(context.Background(), ps, goals); err != nil {
		p.t.Fatal(err)
	}
	if got := p.psGetSlice(ps); got.Parts["goals"] == before.Parts["goals"] || got.State == "approved" {
		p.t.Fatalf("the hand change left the goals part %s (state %q): the setup is wrong", got.Parts["goals"], got.State)
	}
	return before
}

// prRestored checks a restored proof slice against the slice as it was made: the same ID, parts
// equal to the copy, approved, and the history that follows n entries.
func (p *psRig) prRestored(ps string, made psSlice, n int, changed, approved []string) {
	p.t.Helper()
	got := p.psGetSlice(ps)
	same(p.t, "parts", got.Parts, made.Parts)
	same(p.t, "goals", got.Goals, made.Goals)
	if got.State != "approved" || len(got.Approvals) != 3 {
		p.t.Errorf("state %q with %d approvals, want approved with 3", got.State, len(got.Approvals))
	}
	if ids, _ := p.psListed(); len(ids) != 2 {
		p.t.Errorf("slices %v, want S-1 and the one proof slice", ids)
	}
	if len(got.History) < n {
		p.t.Fatalf("history %v is shorter than before the restore (%d)", got.kinds(), n)
	}
	added := got.History[n:]
	if len(changed) > 0 {
		if len(added) == 0 || added[0].Kind != "changed" || !sameSet(added[0].Refs, changed) {
			p.t.Errorf("history added %+v, want a changed entry whose refs are exactly %v first", added, changed)
			return
		}
		added = added[1:]
	}
	var refs []string
	for _, h := range added {
		if h.Kind != "approved" || len(h.Refs) != 1 {
			p.t.Errorf("history entry %+v, want an approved entry of one part", h)
			continue
		}
		refs = append(refs, h.Refs[0])
	}
	if !goslices.Equal(refs, approved) {
		p.t.Errorf("approved entries for %v, want one each for %v", refs, approved)
	}
}

// prBusy asks for the proof slice and checks it is refused with CapturedPatternBusy: at once (409)
// or as the failed operation (the contract says "answers", and the judge is the API's).
func (p *psRig) prBusy(id string) {
	p.t.Helper()
	r := p.psAs(clientToken, "POST", "/captured-patterns/"+id+"/proof-slice", "")
	if r.Code == http.StatusConflict {
		r.problem(p.t, 409, "CapturedPatternBusy")
		return
	}
	op := p.psAccepted(r, "make-proof-slice", id)
	if op.Status != "failed" || op.Error == nil || op.Error.Code != "CapturedPatternBusy" {
		p.t.Fatalf("a restore while a run has not ended: %d, operation %+v (error %+v), want CapturedPatternBusy", r.Code, op, op.Error)
	}
}

// prRunNotEnded starts a proof of the proof slice and checks it has not ended, which the tests
// need (the rig cannot finish a proof: psEnd cancels one after 4 s).
func (p *psRig) prRunNotEnded(ps, project string) string {
	p.t.Helper()
	p.psRegister(ps)
	run := p.psStartProof(ps, project)
	if v := p.psGetRun(run); v.Status == "ended" {
		p.t.Fatalf("run %s has ended at once: the setup is wrong", run)
	}
	return run
}

func TestPSAHandChangedPartIsRestoredFromTheCopyAndAProofStarts(t *testing.T) { // T048, data model "Proof slice"
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	ps := p.psProofSliceOK(id)
	p.psRegister(ps)
	made := p.psGetSlice(ps)
	p.prChangeGoals(ps)
	// The developer approves the changed part (as T027's test does): the slice is approved but no
	// longer equals the copy, so a proof is refused until the part is restored.
	if _, err := p.sp.svc.Approve(context.Background(), ps, slices.PartGoals, p.psGetSlice(ps).Parts["goals"]); err != nil {
		t.Fatal(err)
	}
	changed := p.psGetSlice(ps)
	if changed.State != "approved" {
		t.Fatalf("the changed proof slice is %q, want approved (the setup needs it approved)", changed.State)
	}
	p.psStart(ps, "proof-r", "").problem(t, 409, "PatternDoesNotFit")

	if again := p.psProofSliceOK(id); again != ps {
		t.Fatalf("makeProofSlice gave %s, want the same slice %s", again, ps)
	}
	p.prRestored(ps, made, len(changed.History), []string{"goals"}, []string{"goals"})
	if c := p.psGetCaptured(id); c.ProofSlice == nil || *c.ProofSlice != ps {
		t.Errorf("proof_slice %v, want %s", c.ProofSlice, ps)
	}

	// The restored slice equals the copy, so a proof starts.
	run := p.psStartProof(ps, "proof-r")
	if v := p.psGetRun(run); v.Proof == nil || !*v.Proof || v.Slice != ps {
		t.Errorf("run %+v, want a proof run of %s", v, ps)
	}
	p.psEnd(run)

	// Asked again, it changes nothing.
	n := len(p.psGetSlice(ps).History)
	if again := p.psProofSliceOK(id); again != ps {
		t.Fatalf("a second ask gave %s, want %s", again, ps)
	}
	p.prRestored(ps, made, n, nil, nil)
}

func TestPSARestoreWhileAProofRunHasNotEndedIsBusyAndChangesNothing(t *testing.T) { // T048, CapturedPatternBusy
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	ps := p.psProofSliceOK(id)
	made := p.psGetSlice(ps)
	run := p.prRunNotEnded(ps, "proof-b1")
	p.prChangeGoals(ps)
	changed := p.psGetSlice(ps)

	p.prBusy(id)
	still := p.psGetSlice(ps)
	same(t, "history after the refused restore", still.History, changed.History)
	same(t, "parts after the refused restore", still.Parts, changed.Parts)
	same(t, "goals after the refused restore", still.Goals, changed.Goals)
	if still.State == "approved" {
		t.Errorf("state approved after a refused restore")
	}

	// The refusal is for as long as the run has not ended.
	p.psEnd(run)
	if again := p.psProofSliceOK(id); again != ps {
		t.Fatalf("the ask after the run ended gave %s, want %s", again, ps)
	}
	p.prRestored(ps, made, len(changed.History), []string{"goals"}, []string{"goals"})
}

func TestPSAskingWithNothingToRestoreWhileAProofRunHasNotEndedGivesTheSlice(t *testing.T) { // T048, "asking with nothing to restore is allowed"
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	ps := p.psProofSliceOK(id)
	made := p.psGetSlice(ps)
	run := p.prRunNotEnded(ps, "proof-b2")

	if again := p.psProofSliceOK(id); again != ps {
		t.Fatalf("makeProofSlice gave %s, want the same slice %s", again, ps)
	}
	p.prRestored(ps, made, len(made.History), nil, nil)
	p.psEnd(run)
}

func TestPSARestoreWhoseChecksFailFailsTheOperationAndLeavesTheSliceAsItWas(t *testing.T) { // T048, PartFailsChecks
	p := newPSRig(t)
	lint := p.prOwnLint()
	id, _ := p.psCaptureOK("notes", "notes-app")
	ps := p.psProofSliceOK(id)
	made := p.psGetSlice(ps)

	// The hand change of the contract passes the lint; the copy's contract will not.
	sl, err := p.sp.svc.Get(ps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.sp.svc.ReplaceContract(context.Background(), ps, strings.Replace(sl.Contract, "listNotes", "listAllNotes", 1)); err != nil {
		t.Fatal(err)
	}
	changed := p.psGetSlice(ps)
	if changed.Parts["contract"] == made.Parts["contract"] {
		t.Fatalf("the hand change left the contract equal to the copy: the setup is wrong")
	}
	lint.setFailOn("listNotes")

	op := p.psMakeProofSlice(id)
	if op.Status != "failed" || op.Error == nil || op.Error.Code != "PartFailsChecks" || len(op.Error.Failures) == 0 {
		t.Fatalf("operation %+v (error %+v), want failed with PartFailsChecks listing the failures", op, op.Error)
	}
	still := p.psGetSlice(ps)
	same(t, "parts", still.Parts, changed.Parts)
	same(t, "history", still.History, changed.History)
	same(t, "approvals", still.Approvals, changed.Approvals)
	same(t, "contract", still.Contract, changed.Contract)
	if ids, _ := p.psListed(); len(ids) != 2 {
		t.Errorf("slices %v, want S-1 and the proof slice", ids)
	}
	if c := p.psGetCaptured(id); c.ProofSlice == nil || *c.ProofSlice != ps {
		t.Errorf("proof_slice %v after the failed restore, want %s", c.ProofSlice, ps)
	}

	// Once the checks pass, the next ask restores.
	lint.setFailOn("")
	if again := p.psProofSliceOK(id); again != ps {
		t.Fatalf("the next ask gave %s, want %s", again, ps)
	}
	p.prRestored(ps, made, len(changed.History), []string{"contract"}, []string{"contract"})
}
