package slices

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// The proof slice of a captured pattern (feature 011, data model "Proof slice"; ADR 0097, ADR
// 0098): an approved slice the engine makes from the captured pattern's copy of its source slice,
// described `Proof of <captured pattern>`, whose runs are the captured pattern's proofs.

// ProofOf returns the captured pattern the slice is the proof slice of, "" for any other slice,
// without running its checks again. An unknown slice is NotFound, a malformed ID InvalidRequest.
func (s *Service) ProofOf(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.find(id)
	if err != nil {
		return "", err
	}
	return r.slice.ProofOf, nil
}

// ProofSlice gives the proof slice of the captured pattern named of, making it from parts when
// there is none: parts are the source slice's approved parts as ApprovedParts gives them (each
// part's canonical JSON by part name), and the slice made from them has exactly those parts. Every
// call runs the part checks under the service's method, contract compatibility and lint
// included. A new slice takes the next slice ID only once every check passed; its history is
// `made-for-proof` (no model, its parts), then one `approved` per part. An existing one keeps its
// ID and history; a part whose approval a method change withdrew is approved again, one
// `approved` entry each, and nothing changes when every part is approved and the checks give what
// they gave before. A failing check is PartFailsChecks, listing every failing check, and makes or
// changes no slice. parts that are not three canonical parts are an error, and nothing is made.
func (s *Service) ProofSlice(ctx context.Context, of string, parts map[string][]byte) (Slice, error) {
	if of == "" {
		return Slice{}, errors.New("slices: a proof slice must name the captured pattern it proves")
	}
	s.mu.Lock()
	id := s.proofSliceLocked(of)
	var m *sync.Mutex
	if id != "" {
		m = s.changingLocked(id)
	}
	s.mu.Unlock()
	if id == "" {
		return s.makeProofSlice(ctx, of, parts)
	}
	m.Lock()
	defer m.Unlock()
	return s.checkProofSlice(ctx, id)
}

// proofSliceLocked is the ID of the proof slice of the captured pattern of, "" when there is
// none. The caller holds s.mu.
func (s *Service) proofSliceLocked(of string) string {
	for id, r := range s.slices {
		if r.slice.ProofOf == of {
			return id
		}
	}
	return ""
}

// makeProofSlice makes and stores the proof slice of of from parts (ProofSlice).
func (s *Service) makeProofSlice(ctx context.Context, of string, parts map[string][]byte) (Slice, error) {
	sl, err := slicePartsOf(parts)
	if err != nil {
		return Slice{}, err
	}
	sl.Description, sl.ProofOf = "Proof of "+of, of
	rec := &record{slice: sl, examplesBase: sl.Examples}
	if err := s.recheck(ctx, rec, true); err != nil {
		return Slice{}, err
	}
	if err := proofFailures(&rec.slice); err != nil {
		return Slice{}, err
	}
	now := s.now()
	rec.slice.History = []HistoryEntry{{Kind: "made-for-proof", At: now, Refs: []string{string(PartGoals), string(PartRules), string(PartContract)}}}
	for _, part := range allParts {
		rec.slice.Approvals = append(rec.slice.Approvals, Approval{Part: part, ContentHash: rec.slice.Parts.of(part), At: now})
		rec.slice.History = append(rec.slice.History, HistoryEntry{Kind: "approved", At: now, Refs: []string{string(part)}})
	}
	rec.baseline = rec.slice.Contract
	rec.slice.State = state(&rec.slice)

	s.mu.Lock()
	defer s.mu.Unlock()
	if id := s.proofSliceLocked(of); id != "" { // made meanwhile
		return clone(s.slices[id].slice), nil
	}
	id, err := s.nextID()
	if err != nil {
		return Slice{}, err
	}
	rec.slice.ID = id
	if err := s.save(rec); err != nil {
		return Slice{}, err
	}
	s.slices[id] = rec
	return clone(rec.slice), nil
}

// checkProofSlice runs the part checks of the existing proof slice id again and approves each
// part that holds no approval at its current hash (ProofSlice). The caller holds the slice's
// changing mutex.
func (s *Service) checkProofSlice(ctx context.Context, id string) (Slice, error) {
	for range maxRechecks {
		s.mu.Lock()
		r, err := s.find(id)
		if err != nil {
			s.mu.Unlock()
			return Slice{}, err
		}
		base, contract := r.baseline, r.slice.Contract
		s.mu.Unlock()

		compat, compatRan := s.compatibilityRan(ctx, base, contract)
		lint, lintRan := s.lintContractRan(ctx, contract)

		s.mu.Lock()
		r, err = s.find(id)
		if err != nil {
			s.mu.Unlock()
			return Slice{}, err
		}
		if r.baseline != base || r.slice.Contract != contract { // an approval of the contract landed meanwhile
			s.mu.Unlock()
			continue
		}
		res, err := s.approveProofSlice(r, compat, lint, compatRan && lintRan)
		s.mu.Unlock()
		return res, err
	}
	return Slice{}, &Error{Code: "OperationFailed", Detail: fmt.Sprintf(
		"the approved contract of slice %s changed %d times while its checks ran; ask again", id, maxRechecks)}
}

// approveProofSlice finishes checkProofSlice with the contract's compatibility and lint results,
// storing the slice only when it changed. The caller holds s.mu.
func (s *Service) approveProofSlice(r *record, compat, lint []Check, ran bool) (Slice, error) {
	next := &record{slice: clone(r.slice), baseline: r.baseline, compat: compat, lint: lint, dropped: r.dropped, examplesBase: r.examplesBase}
	if err := s.recheck(context.Background(), next, false); err != nil {
		return Slice{}, err
	}
	next.slice.ChecksMethod = s.methodIf(ran)
	if err := proofFailures(&next.slice); err != nil {
		return Slice{}, err
	}
	now := s.now()
	for _, part := range allParts {
		h := next.slice.Parts.of(part)
		if slices.ContainsFunc(next.slice.Approvals, func(a Approval) bool { return a.Part == part && a.ContentHash == h }) {
			continue
		}
		next.slice.Approvals = append(slices.DeleteFunc(next.slice.Approvals, func(a Approval) bool { return a.Part == part }),
			Approval{Part: part, ContentHash: h, At: now})
		next.slice.History = append(next.slice.History, HistoryEntry{Kind: "approved", At: now, Refs: []string{string(part)}})
		if part == PartContract {
			next.baseline = next.slice.Contract
		}
	}
	next.slice.State = state(&next.slice)
	if len(next.slice.History) == len(r.slice.History) && next.slice.ChecksMethod == r.slice.ChecksMethod &&
		slices.Equal(next.slice.Checks, r.slice.Checks) && next.slice.State == r.slice.State {
		return clone(r.slice), nil
	}
	if err := s.save(next); err != nil {
		return Slice{}, err
	}
	*r = *next
	return clone(r.slice), nil
}

// proofFailures is PartFailsChecks listing every failing check of sl, nil when none fails.
func proofFailures(sl *Slice) error {
	fails := failing(sl)
	if len(fails) == 0 {
		return nil
	}
	var names []string
	for _, f := range fails {
		if !slices.Contains(names, f.Part) {
			names = append(names, f.Part)
		}
	}
	return &Error{Code: "PartFailsChecks", Detail: fmt.Sprintf("the %s part of the proof slice fails its checks", strings.Join(names, " and ")),
		Failures: fails}
}

// slicePartsOf reads the three parts in their canonical JSON (goals_part, rules_part,
// contract_part) into a slice with those parts, whose part hashes are the SHA-256 of the bytes
// given. A missing part, a field outside its part, or bytes that are not the part's canonical
// form are an error.
func slicePartsOf(parts map[string][]byte) (Slice, error) {
	var g struct {
		Goals []Goal `json:"goals"`
	}
	var r RulesPart
	var c struct {
		Contract string `json:"contract"`
	}
	for _, p := range []struct {
		part Part
		into any
	}{{PartGoals, &g}, {PartRules, &r}, {PartContract, &c}} {
		b, ok := parts[string(p.part)]
		if !ok {
			return Slice{}, fmt.Errorf("slices: the proof slice's %s part is missing", p.part)
		}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(p.into); err != nil {
			return Slice{}, fmt.Errorf("slices: the proof slice's %s part is unreadable: %w", p.part, err)
		}
	}
	sl := clone(Slice{Goals: g.Goals, Rules: r.Rules, Examples: r.Examples, Terms: r.Terms, Contract: c.Contract})
	normalise(&sl)
	if err := setParts(&sl); err != nil {
		return Slice{}, fmt.Errorf("slices: the proof slice's parts: %w", err)
	}
	for _, part := range allParts {
		sum := sha256.Sum256(parts[string(part)])
		if hex.EncodeToString(sum[:]) != sl.Parts.of(part) {
			return Slice{}, fmt.Errorf("slices: the proof slice's %s part is not in its canonical form", part)
		}
	}
	return sl, nil
}
