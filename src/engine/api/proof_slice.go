package api

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"

	"github.com/agentic-platform/engine/api/gen"
	sl "github.com/agentic-platform/engine/slices"
)

// The proof side of captured patterns (feature 011, data model "Proof slice" and "Runs that
// continue"): makeProofSlice, a startRun of a proof slice and a replay of a proof run. A proof is
// an ordinary run of the proof slice under the captured pattern, with its source values, that
// reuses no earlier result.

// MakeProofSlice refuses at once, before any operation exists: an unknown captured pattern
// (NotFound), a withdrawn one (CapturedPatternWithdrawn) and one with another operation that has
// not ended (CapturedPatternBusy). Otherwise it accepts the request, whose target is the captured
// pattern; the operation's result names the proof slice.
func (s *sliceServer) MakeProofSlice(ctx context.Context, req gen.MakeProofSliceRequestObject) (gen.MakeProofSliceResponseObject, error) {
	if _, err := s.capturedStore(); err != nil {
		return nil, err
	}
	s.cmu.Lock()
	defer s.cmu.Unlock()
	r, err := s.readCaptured(req.CapturedPatternId)
	if err != nil {
		return s.refuse("makeProofSlice", err)
	}
	if r.State == gen.CapturedPatternStateWithdrawn {
		return s.refuse("makeProofSlice", stateRefusal(r))
	}
	if err := s.capturedBusy(r, false); err != nil {
		return s.refuse("makeProofSlice", err)
	}
	op, err := s.ops.accept(ctx, "makeProofSlice", r.ID)
	if err != nil {
		return unavailableFor(err), nil
	}
	return gen.MakeProofSlice202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// makeProofSlice is the work of makeProofSlice: the slices service gives the proof slice, made
// from the captured copy when there is none (PartFailsChecks when a part fails a check), and the
// captured pattern records it as its proof slice. A repeat gives the same slice.
func (s *sliceServer) makeProofSlice(ctx context.Context, id string) (WorkResult, error) {
	s.cmu.Lock()
	r, err := s.readCaptured(id)
	s.cmu.Unlock()
	if err != nil {
		return WorkResult{}, s.workError("makeProofSlice", err)
	}
	if r.State == gen.CapturedPatternStateWithdrawn {
		return WorkResult{}, s.workError("makeProofSlice", stateRefusal(r))
	}
	parts := map[string][]byte{"goals": []byte(r.Copy.Goals), "rules": []byte(r.Copy.Rules), "contract": []byte(r.Copy.Contract)}
	slc, err := s.svc.ProofSlice(ctx, r.ID, parts)
	if err != nil {
		return WorkResult{}, s.workError("makeProofSlice", err)
	}
	s.cmu.Lock()
	defer s.cmu.Unlock()
	if r, err = s.readCaptured(id); err != nil {
		return WorkResult{}, s.workError("makeProofSlice", err)
	}
	if r.ProofSlice != slc.ID {
		r.ProofSlice = slc.ID
		if err := s.saveCaptured(r); err != nil {
			return WorkResult{}, Retryable(err)
		}
	}
	return WorkResult{Slice: slc.ID}, nil
}

// namedPattern resolves startRun's `pattern`: nil for none or the engine's base pattern, else the
// captured pattern it names; a name of no pattern is NotFound.
func (s *sliceServer) namedPattern(name *gen.PatternId) (*capturedRecord, error) {
	if name == nil || (s.runs != nil && *name == s.runs.pattern.ID) {
		return nil, nil
	}
	missing := &sl.Error{Code: string(gen.ErrorCodeNotFound), Detail: fmt.Sprintf("no pattern is named %s", *name)}
	if s.captured == nil {
		return nil, missing
	}
	r, err := s.readCaptured(*name)
	if isNotFound(err) {
		return nil, missing
	}
	return r, err
}

// proofStart is the pattern choice of a run of the proof slice slc, which proves the captured
// pattern of: that captured pattern with its source values, whatever its state, refused with
// NotFound when it was erased, CapturedPatternWithdrawn, CapturedPatternBusy while another of its
// operations runs, then PatternDoesNotFit when named is a pattern other than it or a part of slc
// no longer equals the copy. It records slc as the captured pattern's proof slice when the
// record does not name it yet.
func (s *sliceServer) proofStart(slc sl.Slice, of string, named *gen.PatternId) (RunStart, error) {
	if _, err := s.capturedStore(); err != nil {
		return RunStart{}, err
	}
	s.cmu.Lock()
	defer s.cmu.Unlock()
	r, err := s.readCaptured(of)
	if err != nil {
		return RunStart{}, err
	}
	if r.State == gen.CapturedPatternStateWithdrawn {
		return RunStart{}, stateRefusal(r)
	}
	if err := s.capturedBusy(r, false); err != nil {
		return RunStart{}, err
	}
	doesNotFit := func(format string, a ...any) error {
		return &sl.Error{Code: string(gen.ErrorCodePatternDoesNotFit), Detail: fmt.Sprintf(format, a...)}
	}
	if named != nil && *named != r.ID {
		return RunStart{}, doesNotFit("slice %s is the proof slice of %s: its runs are proofs of %s, not runs of %s", slc.ID, r.ID, r.ID, *named)
	}
	for _, p := range []struct {
		part      sl.Part
		now, copy string
	}{{sl.PartGoals, slc.Parts.Goals, r.Parts.Goals}, {sl.PartRules, slc.Parts.Rules, r.Parts.Rules},
		{sl.PartContract, slc.Parts.Contract, r.Parts.Contract}} {
		if p.now != p.copy {
			return RunStart{}, doesNotFit("the %s part of slice %s no longer equals the copy of captured pattern %s it was made from", p.part, slc.ID, r.ID)
		}
	}
	if r.ProofSlice != slc.ID {
		r.ProofSlice = slc.ID
		if err := s.saveCaptured(r); err != nil {
			return RunStart{}, err
		}
	}
	return proofRun(r)
}

// proofReplay is the pattern choice of a replay of a proof run whose RunStarted is started: a new
// proof of the captured pattern it names, with its current source values, refused with NotFound
// when the captured pattern was erased, CapturedPatternWithdrawn and CapturedPatternBusy, never
// ProofRequired.
func (s *sliceServer) proofReplay(started string) (RunStart, error) {
	if _, err := s.capturedStore(); err != nil {
		return RunStart{}, err
	}
	id, version, _ := strings.Cut(started, "@")
	s.cmu.Lock()
	defer s.cmu.Unlock()
	r, err := s.readCaptured(id)
	if err != nil {
		return RunStart{}, err
	}
	if version != capturedVersion {
		return RunStart{}, fmt.Errorf("a proof of %s names version %s, captured patterns have %s", id, version, capturedVersion)
	}
	if r.State == gen.CapturedPatternStateWithdrawn {
		return RunStart{}, stateRefusal(r)
	}
	if err := s.capturedBusy(r, false); err != nil {
		return RunStart{}, err
	}
	return proofRun(r)
}

// proofRun is the start of a proof of r: its declaration, `chosen`, its source values and source
// run, `proof`, no reuse, and the fill of its `fill` steps from its template with those values.
func proofRun(r *capturedRecord) (RunStart, error) {
	p, err := r.pattern()
	if err != nil {
		return RunStart{}, err
	}
	values := r.values()
	return RunStart{Definition: p, PatternReason: string(gen.PatternReasonChosen), Parameters: values, SourceRun: r.SourceRun,
		Proof: true, NoReuse: true, Fill: &RunFill{Captured: r.ID, Template: maps.Clone(r.Template), Values: maps.Clone(values)}}, nil
}

// namedRefusal refuses a run of a slice other than a proof slice that names the captured pattern
// r: CapturedPatternWithdrawn, or CapturedPatternNotApproved while it is proposed or proven. An
// approved one is used only once the usable rule and the fit exist (feature 011's T015): until
// then the start is the engine's fault, and nothing starts.
func namedRefusal(r *capturedRecord) error {
	switch r.State {
	case gen.CapturedPatternStateWithdrawn:
		return stateRefusal(r)
	case gen.CapturedPatternStateProposed, gen.CapturedPatternStateProven:
		return &sl.Error{Code: string(gen.ErrorCodeCapturedPatternNotApproved),
			Detail: fmt.Sprintf("captured pattern %s is %s, not approved", r.ID, r.State)}
	}
	return errors.New("a run of an approved captured pattern is not implemented yet: " + r.ID)
}

// completeBeforeErasure completes the proofs of the captured pattern of from the runs of its proof
// slice, before that slice's runs are erased, so a finished proof is kept. The caller holds the
// runs' lock.
func (s *sliceServer) completeBeforeErasure(of, slice string) error {
	if s.captured == nil {
		return nil
	}
	s.cmu.Lock()
	defer s.cmu.Unlock()
	r, err := s.readCaptured(of)
	switch {
	case isNotFound(err):
		return nil
	case err != nil:
		return err
	}
	if r.ProofSlice != slice && r.ProofSlice != "" {
		return nil
	}
	r.ProofSlice = slice // the slices service names it the proof slice, whatever the record held
	m, err := currentMethod()
	if err != nil {
		return err
	}
	if _, err := s.completeProofs(r, m); err != nil {
		return err
	}
	if err := s.saveCaptured(r); err != nil {
		return Retryable(err)
	}
	return nil
}

// forgetProofSlice clears proof_slice in every captured pattern whose proof slice is the erased
// slice; each keeps its proofs and its template.
func (s *sliceServer) forgetProofSlice(slice string) error {
	if s.captured == nil || s.runs == nil {
		return nil
	}
	s.cmu.Lock()
	defer s.cmu.Unlock()
	all, err := s.allCaptured()
	if err != nil {
		return Retryable(err)
	}
	for _, r := range all {
		if r.ProofSlice != slice {
			continue
		}
		r.ProofSlice = ""
		if err := s.saveCaptured(r); err != nil {
			return Retryable(err)
		}
	}
	return nil
}

func (p problemResponse) VisitMakeProofSliceResponse(w http.ResponseWriter) error { return p.write(w) }
func (u unavailableAnswer) VisitMakeProofSliceResponse(w http.ResponseWriter) error {
	return u.write(w)
}
