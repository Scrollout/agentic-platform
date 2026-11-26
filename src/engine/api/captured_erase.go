package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/machine"
	sl "github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
)

// Withdrawal and erasure of a captured pattern (feature 011, FR-012 to FR-014; data model "States";
// contract withdrawCapturedPattern and eraseCapturedPattern). Both are operations, one at a time per
// captured pattern like the others in captured.go.

// ---- withdrawal ----

// WithdrawCapturedPattern refuses at once, before any operation exists: an unknown or erased
// captured pattern (NotFound) and one with another operation that has not ended
// (CapturedPatternBusy). Otherwise it accepts the withdrawal, whose target is the captured
// pattern, in any state; withdrawing a withdrawn one is accepted and changes nothing. A run that
// has started is not stopped: it finishes on the captured pattern.
func (s *sliceServer) WithdrawCapturedPattern(ctx context.Context, req gen.WithdrawCapturedPatternRequestObject) (gen.WithdrawCapturedPatternResponseObject, error) {
	if _, err := s.capturedStore(); err != nil {
		return nil, err
	}
	s.cmu.Lock()
	defer s.cmu.Unlock()
	r, err := s.readCaptured(req.CapturedPatternId)
	if err != nil {
		return s.refuse("withdrawCapturedPattern", err)
	}
	if err := s.capturedBusy(r, false); err != nil {
		return s.refuse("withdrawCapturedPattern", err)
	}
	op, err := s.ops.accept(ctx, "withdrawCapturedPattern", r.ID)
	if err != nil {
		return unavailableFor(err), nil
	}
	return gen.WithdrawCapturedPattern202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// withdraw is the work of a withdrawal: the captured pattern becomes `withdrawn`, keeping its
// parameters, proofs, approval and proof slice. A withdrawn one is left as it is, so a repeat
// changes nothing.
func (s *sliceServer) withdraw(id string) (WorkResult, error) {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	r, err := s.readCaptured(id)
	if err != nil {
		return WorkResult{}, s.workError("withdrawCapturedPattern", err)
	}
	if r.State != gen.CapturedPatternStateWithdrawn {
		r.State = gen.CapturedPatternStateWithdrawn
		if err := s.saveCaptured(r); err != nil {
			return WorkResult{}, Retryable(err)
		}
	}
	return WorkResult{CapturedPattern: id}, nil
}

// ---- erasure ----

// EraseCapturedPattern refuses at once, before any operation exists: an unknown or erased
// captured pattern (NotFound), and CapturedPatternBusy while another of its operations has not
// ended, a run of its proof slice is starting or has not ended, or a run built from it, of any
// slice, is starting or has not ended (a run waiting for the developer has not ended). Otherwise
// it accepts the erasure, whose target is the captured pattern, in any state.
func (s *sliceServer) EraseCapturedPattern(ctx context.Context, req gen.EraseCapturedPatternRequestObject) (gen.EraseCapturedPatternResponseObject, error) {
	if _, err := s.capturedStore(); err != nil {
		return nil, err
	}
	if s.runs != nil {
		s.runs.mu.Lock() // no run starts between the busy check and the acceptance
		defer s.runs.mu.Unlock()
	}
	s.cmu.Lock()
	defer s.cmu.Unlock()
	r, err := s.readCaptured(req.CapturedPatternId)
	if err != nil {
		return s.refuse("eraseCapturedPattern", err)
	}
	if err := s.erasureBusy(r, ""); err != nil {
		return s.refuse("eraseCapturedPattern", err)
	}
	op, err := s.ops.accept(ctx, "eraseCapturedPattern", r.ID)
	if err != nil {
		return unavailableFor(err), nil
	}
	return gen.EraseCapturedPattern202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// erasureBusy is CapturedPatternBusy while an operation of r other than own has not ended, a run
// of its proof slice is starting or has not ended, or a run built from it is. The caller holds the
// runs' lock (or no runs are kept) and s.cmu.
func (s *sliceServer) erasureBusy(r *capturedRecord, own string) error {
	if err := s.otherOperation(r, own); err != nil {
		return err
	}
	if err := s.proofRunBusy(s.svc.ProofSliceOf(r.ID)); err != nil {
		return err
	}
	return s.builtRunBusy(r)
}

// builtRunBusy is CapturedPatternBusy while a run that follows r (a proof or a run of another
// slice) is starting or has not ended. A run whose workflow closed with no end on its record is
// ended first (closeIfFailed), as noOtherActiveRun does. The caller holds the runs' lock.
func (s *sliceServer) builtRunBusy(r *capturedRecord) error {
	if s.runs == nil {
		return nil
	}
	ref := r.ref()
	var open []string
	recorded := map[string]bool{}
	for _, e := range s.runs.st.Runs() {
		recorded[e.Run] = true
		v, events, _, ok, err := s.runs.find(e.Run)
		switch {
		case err != nil:
			return err
		case ok && events[0].Pattern == ref && v.Status != machine.StatusEnded:
			open = append(open, e.Run)
		}
	}
	s.runs.vmu.Lock()
	for id := range s.runs.starting {
		if !recorded[id] && s.runs.startedAs[id].start.Pattern == ref {
			open = append(open, id)
		}
	}
	s.runs.vmu.Unlock()
	slices.Sort(open)
	for _, run := range open {
		if s.runs.durable != nil {
			if ended, _, err := s.runs.closeIfFailed(context.Background(), run); err == nil && ended {
				continue
			}
		}
		return &sl.Error{Code: string(gen.ErrorCodeCapturedPatternBusy),
			Detail: fmt.Sprintf("run %s, built from captured pattern %s, has not ended", run, r.ID)}
	}
	return nil
}

// erase is the work of an erasure. Under the runs' lock, so no run starts, resumes or is replayed
// meanwhile, it judges busy again (a run that started after the acceptance fails the operation),
// then erases, in this order:
//  1. the proof slice, as deleteSlice erases a slice: its proofs are completed into the record
//     first, then its runs, their artifacts that nothing else holds, and the slice; the record
//     then names no proof slice;
//  2. the captured pattern in the store: its ID is marked erased (from then on every reader answers
//     NotFound and a capture under the ID CapturedPatternExists), then its record, then the
//     template artifacts no slice stored and no other captured pattern holds.
//
// A crash after step 1 leaves a whole captured pattern with no proof slice, which reads as any
// such captured pattern does; a crash in step 2 leaves the mark, so nothing reads it. The
// operation's retry completes either. Runs of other slices built from it keep their records.
func (s *sliceServer) erase(ctx context.Context, rec store.OperationRecord) (WorkResult, error) {
	id := rec.Target
	if s.runs != nil {
		s.runs.mu.Lock()
		defer s.runs.mu.Unlock()
	}
	s.cmu.Lock()
	r, err := s.readCaptured(id)
	if isNotFound(err) {
		defer s.cmu.Unlock()
		erased, err := s.captured.CapturedPatternErased(id)
		switch {
		case err != nil:
			return WorkResult{}, Retryable(err)
		case !erased:
			return WorkResult{}, s.workError("eraseCapturedPattern", capturedNotFound(id))
		}
		if err := s.captured.EraseCapturedPattern(id); err != nil { // a repeat completes it
			return WorkResult{}, Retryable(err)
		}
		return WorkResult{CapturedPattern: id}, nil
	}
	if err == nil {
		err = s.erasureBusy(r, rec.ID)
	}
	s.cmu.Unlock()
	if err != nil {
		return WorkResult{}, s.workError("eraseCapturedPattern", err)
	}
	if ps := s.svc.ProofSliceOf(id); ps != "" {
		if err := s.eraseSlice(ctx, ps, rec.ID); err != nil {
			return WorkResult{}, s.workError("eraseCapturedPattern", err)
		}
	}
	s.cmu.Lock()
	defer s.cmu.Unlock()
	if err := s.captured.EraseCapturedPattern(id); err != nil {
		return WorkResult{}, Retryable(err)
	}
	return WorkResult{CapturedPattern: id}, nil
}

func (p problemResponse) VisitWithdrawCapturedPatternResponse(w http.ResponseWriter) error {
	return p.write(w)
}
func (p problemResponse) VisitEraseCapturedPatternResponse(w http.ResponseWriter) error {
	return p.write(w)
}
func (u unavailableAnswer) VisitWithdrawCapturedPatternResponse(w http.ResponseWriter) error {
	return u.write(w)
}
func (u unavailableAnswer) VisitEraseCapturedPatternResponse(w http.ResponseWriter) error {
	return u.write(w)
}
