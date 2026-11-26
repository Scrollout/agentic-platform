package api

// T050 (feature 011, US2; contract makeProofSlice operation codes; data model "Proof slice"): the
// work of makeProofSlice judges "a restore would change a part while a proof run has not ended"
// again, when it runs, because a part may be changed by hand between the acceptance (nothing
// differed, so it was accepted) and that judgement. Then the operation fails with the error code
// CapturedPatternBusy, not OperationFailed, and the proof slice is untouched. Through HTTP and the
// slices service's hand-change calls, on the rig of captured_proof_slice_test.go and the helpers
// of captured_proof_slice_restore_test.go, all used unchanged. Written before the code by a test
// author; the code author must not change it (ADR 0029).
//
// The seam is no sleep: a hand change of the contract (ReplaceContract) holds the proof slice's
// changing lock while it runs its lint, which psLint.arm holds. The request is made while the
// change is held: nothing differs yet, so it is accepted (202), and its work cannot judge before
// it gets that lock, that is after the change has landed. Releasing the lint lets the change land
// first, whatever the scheduling.
//
// Edge cases checked: the 202 (not the immediate 409, since nothing differed at the request); the
// operation's error code; the slice's parts, goals, contract, approvals, state and history unchanged
// and no `changed` entry; no new slice; proof_slice still named; the same ask after the run ended
// restores (the same slice, one `changed` entry of exactly the contract, then `approved`).
//
// Left out, and why: a hand change of goals or rules (they run no lint, so no existing seam holds
// the work before its judgement; the code path judged is the same one); the immediate 409 and the
// no-difference ask (T048's tests); a run that ends while the work waits (the rig cannot end a
// proof on demand: psEnd cancels after 4 s).

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPSAPartChangedByHandAfterAcceptanceFailsTheWorkWithCapturedPatternBusy(t *testing.T) { // T050, contract makeProofSlice operation codes
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	ps := p.psProofSliceOK(id)
	made := p.psGetSlice(ps)
	run := p.prRunNotEnded(ps, "proof-bw")

	sl, err := p.sp.svc.Get(ps)
	if err != nil {
		t.Fatal(err)
	}
	contract := strings.Replace(sl.Contract, "listNotes", "listAllNotes", 1)
	if contract == sl.Contract {
		t.Fatal("the hand change would leave the contract as it is: the setup is wrong")
	}
	entered, release := p.lint.arm()
	t.Cleanup(release)
	changed := make(chan error, 1)
	go func() {
		_, err := p.sp.svc.ReplaceContract(context.Background(), ps, contract)
		changed <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the hand change never ran its lint")
	}

	// Nothing differs yet: the request is accepted, and its work waits for the change.
	r := p.psAs(clientToken, "POST", "/captured-patterns/"+id+"/proof-slice", "")
	if r.Code != http.StatusAccepted {
		t.Fatalf("a request while nothing differs = %d, want 202: %s", r.Code, r.Body.String())
	}
	op := r.operation(t, http.StatusAccepted)
	release()
	select {
	case err := <-changed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the hand change did not land after the lint was released")
	}
	done := p.psSettle(op.Id)
	if done.Status != "failed" || done.Error == nil || done.Error.Code != "CapturedPatternBusy" {
		t.Fatalf("operation %+v (error %+v), want failed with CapturedPatternBusy, as the part was changed by hand while the run has not ended", done, done.Error)
	}

	got := p.psGetSlice(ps)
	if got.Parts["contract"] == made.Parts["contract"] {
		t.Fatalf("the hand change did not land: the setup is wrong")
	}
	if got.State == "approved" {
		t.Errorf("state approved after a refused restore")
	}
	same(t, "goals part", got.Parts["goals"], made.Parts["goals"])
	same(t, "rules part", got.Parts["rules"], made.Parts["rules"])
	same(t, "contract", got.Contract, contract)
	t.Logf("history %+v", got.History)
	if ids, _ := p.psListed(); len(ids) != 2 {
		t.Errorf("slices %v, want S-1 and the one proof slice", ids)
	}
	if c := p.psGetCaptured(id); c.ProofSlice == nil || *c.ProofSlice != ps {
		t.Errorf("proof_slice %v after the failed work, want %s", c.ProofSlice, ps)
	}
	before := p.psGetSlice(ps)

	// The same ask once the run ended restores.
	p.psEnd(run)
	if again := p.psProofSliceOK(id); again != ps {
		t.Fatalf("the ask after the run ended gave %s, want %s", again, ps)
	}
	p.prRestored(ps, made, len(before.History), []string{"contract"}, []string{"contract"})
}
