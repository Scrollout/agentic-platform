package api

// Feature 011, T064 (US3, review F1 of T022; FR-012, data model "withdrawn: final", Engineering
// Principles "Single owner for rules"): a named start judges the captured pattern's current record,
// not a copy read before the lock. startRun reads the named captured pattern (namedPattern) without
// the captured patterns' lock; namedStart takes the lock afterwards, so a withdrawal or an erasure
// that completed in between must still be seen, and the stale copy must never be saved over it.
//
// The rig's handler hides its sliceServer, so these tests build a second sliceServer over the same
// slices service and store (a sliceServer holds no state of its own but the lock and what the store
// and the runs' record hold), read the stale copy with readCaptured, change the captured pattern
// through the HTTP API, and call namedStart(slice, stale) directly. Written before the code (T065)
// by a test author; the code author must not change it (ADR 0029).
//
// Edge cases checked: a withdrawal after the read (the refusal and the stored state); a withdrawal
// after the read with a proof that ended since (completeProofs would change and save the stale copy);
// an erasure after the read (NotFound naming the captured pattern, the record not made again).
// Left out, and why: concurrent calls (the interleaving is made by calling in order, which is the
// state the race reaches); a change of the engine's method between the read and the call (T066's
// and T070's reach, another rule).

import (
	"errors"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/api/gen"
	sl "github.com/agentic-platform/engine/slices"
)

// nsServer is a sliceServer over the rig's slices service and store.
func (s *srRig) nsServer() *sliceServer {
	s.t.Helper()
	cfg := Config{Store: s.st, Model: "test-model"}
	ss := &sliceServer{server: &server{contract: engineContract}, svc: s.sp.svc, store: s.st, runs: newRuns(cfg, s.sp.svc)}
	ss.captured, _ = any(s.st).(CapturedStore)
	if ss.captured == nil || ss.runs == nil {
		s.t.Fatal("the rig's store keeps no captured patterns or runs")
	}
	return ss
}

func nsWantCode(t *testing.T, err error, code gen.ErrorCode) *sl.Error {
	t.Helper()
	var e *sl.Error
	if !errors.As(err, &e) || e.Code != string(code) {
		t.Fatalf("namedStart answered %v, want a %s error", err, code)
	}
	return e
}

// T064, FR-012: a record read while the captured pattern was approved, judged after its withdrawal
// completed, is refused CapturedPatternWithdrawn naming it, and the stored state stays withdrawn.
func TestNSNamedStartOfAStaleRecordAfterWithdrawalIsWithdrawn(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.mcLater()
	id := s.approved("notes")
	ss := s.nsServer()
	stale, err := ss.readCaptured(id)
	if err != nil || stale.State != gen.CapturedPatternStateApproved {
		t.Fatalf("setup: read %v, %v, want an approved record", stale, err)
	}
	s.mcWithdraw(id)

	_, err = ss.namedStart(later, stale)
	e := nsWantCode(t, err, gen.ErrorCodeCapturedPatternWithdrawn)
	if !strings.Contains(e.Detail, id) {
		t.Errorf("the refusal's detail %q does not name %s", e.Detail, id)
	}
	if got := s.captured(id); got.State != gen.CapturedPatternStateWithdrawn || got.Usable {
		t.Errorf("stored state %s usable %v, want withdrawn and not usable", got.State, got.Usable)
	}
}

// T064, FR-012: as above, with a proof that ended after the read, so that completing the stale
// copy's proofs changes it and the call would save it: the stored state still stays withdrawn.
func TestNSAProofEndedAfterTheReadNeverUndoesAWithdrawal(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.mcLater()
	id := s.approved("notes")
	ss := s.nsServer()
	stale, err := ss.readCaptured(id)
	if err != nil {
		t.Fatal(err)
	}
	before := len(stale.Proofs)
	s.prove(id, "proof-after-the-read")
	s.mcWithdraw(id)
	if n := len(s.captured(id).Proofs); n != before+1 {
		t.Fatalf("setup: the stored record has %d proofs, want %d", n, before+1)
	}

	_, err = ss.namedStart(later, stale)
	_ = nsWantCode(t, err, gen.ErrorCodeCapturedPatternWithdrawn)
	got := s.captured(id)
	if got.State != gen.CapturedPatternStateWithdrawn || got.Usable {
		t.Errorf("stored state %s usable %v, want withdrawn and not usable", got.State, got.Usable)
	}
	if len(got.Proofs) != before+1 {
		t.Errorf("stored record has %d proofs, want %d", len(got.Proofs), before+1)
	}
}

// T064, FR-013: a record read before the captured pattern was erased, judged after, is NotFound
// naming it, not another error, and the record is not made again.
func TestNSNamedStartOfAStaleRecordAfterErasureIsNotFound(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.mcLater()
	id := s.approved("notes")
	ss := s.nsServer()
	stale, err := ss.readCaptured(id)
	if err != nil {
		t.Fatal(err)
	}
	s.mcErase(id)

	_, err = ss.namedStart(later, stale)
	e := nsWantCode(t, err, gen.ErrorCodeNotFound)
	if !strings.Contains(e.Detail, id) {
		t.Errorf("the NotFound detail %q does not name %s", e.Detail, id)
	}
	s.mcWantGone(id)
}
