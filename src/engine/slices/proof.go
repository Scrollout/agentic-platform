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

// ProofSlice gives the proof slice of the captured pattern named of, made from or restored to
// the copy parts: parts are the source slice's approved parts as ApprovedParts gives them (each
// part's canonical JSON by part name). Every call runs the part checks under the service's
// method, contract compatibility and lint included. With no proof slice yet, it makes one with
// exactly those parts, taking the next slice ID only once every check passed; its history is
// `made-for-proof` (no model, its parts), then one `approved` per part. An existing one keeps its
// ID: each part that differs from the copy is put back, recorded as one `changed` entry whose
// refs are exactly the restored parts (part order), its contract checked for compatibility as
// when the slice was made (against no earlier contract) when the contract is restored; then
// every part without an approval at its current hash (restored, or withdrawn by a method change)
// is approved, one `approved` entry each in part order. Nothing changes when every part equals
// the copy, is approved and the checks give what they gave before. A failing check is
// PartFailsChecks, listing every failing check, and makes or changes no slice. parts that are
// not three canonical parts are an error, and nothing is made or changed.
func (s *Service) ProofSlice(ctx context.Context, of string, parts map[string][]byte) (Slice, error) {
	return s.ProofSliceUnlessBusy(ctx, of, parts, nil)
}

// ProofSliceUnlessBusy is ProofSlice, except that before it restores any part of an existing
// proof slice it calls busy with that slice's ID, while no other change of the slice can land;
// an error from busy is returned as it is, and nothing changes. busy is not called when nothing
// differs from the copy (approving only) or when the slice is made; nil never refuses.
func (s *Service) ProofSliceUnlessBusy(ctx context.Context, of string, parts map[string][]byte, busy func(slice string) error) (Slice, error) {
	if of == "" {
		return Slice{}, errors.New("slices: a proof slice must name the captured pattern it proves")
	}
	cp, err := slicePartsOf(parts)
	if err != nil {
		return Slice{}, err
	}
	s.mu.Lock()
	id := s.proofSliceLocked(of)
	var m *sync.Mutex
	if id != "" {
		m = s.changingLocked(id)
	}
	s.mu.Unlock()
	if id == "" {
		return s.makeProofSlice(ctx, of, cp)
	}
	m.Lock()
	defer m.Unlock()
	return s.checkProofSlice(ctx, id, cp, busy)
}

// ProofSliceDiffers is the ID of the proof slice of the captured pattern of and whether any of
// its parts differs from the copy parts (as ProofSlice takes them), so that asking for it would
// restore a part; "" and false when there is none. It changes nothing and runs no check.
func (s *Service) ProofSliceDiffers(of string, parts map[string][]byte) (string, bool, error) {
	cp, err := slicePartsOf(parts)
	if err != nil {
		return "", false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.proofSliceLocked(of)
	if id == "" {
		return "", false, nil
	}
	return id, len(differing(&s.slices[id].slice, &cp)) > 0, nil
}

// ProofSliceOf returns the ID of the proof slice of the captured pattern named of, "" when there is
// none. It changes nothing and runs no check.
func (s *Service) ProofSliceOf(of string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proofSliceLocked(of)
}

// differing lists, in part order, the parts of sl whose hash differs from the copy cp's.
func differing(sl, cp *Slice) []Part {
	var out []Part
	for _, part := range allParts {
		if sl.Parts.of(part) != cp.Parts.of(part) {
			out = append(out, part)
		}
	}
	return out
}

// restore puts the copy cp's content of each part in parts into sl.
func restore(sl *Slice, cp *Slice, parts []Part) {
	for _, part := range parts {
		switch part {
		case PartGoals:
			sl.Goals = cp.Goals
		case PartRules:
			sl.Rules, sl.Examples, sl.Terms = cp.Rules, cp.Examples, cp.Terms
		case PartContract:
			sl.Contract = cp.Contract
		}
	}
	*sl = clone(*sl) // own cp's lists
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

// makeProofSlice makes and stores the proof slice of of from the copy sl (ProofSlice).
func (s *Service) makeProofSlice(ctx context.Context, of string, sl Slice) (Slice, error) {
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

// checkProofSlice restores the existing proof slice id to the copy cp, runs its part checks
// again and approves each part that holds no approval at its current hash (ProofSlice); busy, when
// not nil, is asked first when a part would be restored (ProofSliceUnlessBusy). The caller holds
// the slice's changing mutex, so no other change of its parts lands meanwhile.
func (s *Service) checkProofSlice(ctx context.Context, id string, cp Slice, busy func(string) error) (Slice, error) {
	// toCheck is the contract compatibility and lint judge for r restored to cp: a restored contract
	// against no earlier contract, as when the slice was made, else against the approved baseline.
	toCheck := func(r *record) (base, contract string, restored []Part) {
		restored = differing(&r.slice, &cp)
		if slices.Contains(restored, PartContract) {
			return "", cp.Contract, restored
		}
		return r.baseline, r.slice.Contract, restored
	}
	asked := false
	for range maxRechecks {
		s.mu.Lock()
		r, err := s.find(id)
		if err != nil {
			s.mu.Unlock()
			return Slice{}, err
		}
		base, contract, restored := toCheck(r)
		s.mu.Unlock()
		if len(restored) > 0 && busy != nil && !asked {
			if err := busy(id); err != nil {
				return Slice{}, err
			}
			asked = true
		}

		compat, compatRan := s.compatibilityRan(ctx, base, contract)
		lint, lintRan := s.lintContractRan(ctx, contract)

		s.mu.Lock()
		r, err = s.find(id)
		if err != nil {
			s.mu.Unlock()
			return Slice{}, err
		}
		if b, c, _ := toCheck(r); b != base || c != contract { // an approval of the contract landed meanwhile
			s.mu.Unlock()
			continue
		}
		res, err := s.approveProofSlice(r, &cp, restored, compat, lint, compatRan && lintRan)
		s.mu.Unlock()
		return res, err
	}
	return Slice{}, &Error{Code: "OperationFailed", Detail: fmt.Sprintf(
		"the approved contract of slice %s changed %d times while its checks ran; ask again", id, maxRechecks)}
}

// approveProofSlice finishes checkProofSlice with the contract's compatibility and lint results:
// it puts back the restored parts from cp (one `changed` entry), checks the slice, approves each
// part without an approval at its current hash, and stores the slice only when it changed. The
// caller holds s.mu.
func (s *Service) approveProofSlice(r *record, cp *Slice, restored []Part, compat, lint []Check, ran bool) (Slice, error) {
	next := &record{slice: clone(r.slice), baseline: r.baseline, compat: compat, lint: lint, dropped: r.dropped, examplesBase: r.examplesBase}
	restore(&next.slice, cp, restored)
	if slices.Contains(restored, PartRules) {
		next.dropped, next.examplesBase = dropExamples(r.dropped, r.examplesBase, next.slice.Examples)
	}
	if err := s.recheck(context.Background(), next, false); err != nil {
		return Slice{}, err
	}
	next.slice.ChecksMethod = s.methodIf(ran)
	if err := proofFailures(&next.slice); err != nil {
		return Slice{}, err
	}
	now := s.now()
	if len(restored) > 0 {
		refs := make([]string, len(restored))
		for i, part := range restored {
			refs[i] = string(part)
		}
		next.slice.History = append(next.slice.History, HistoryEntry{Kind: "changed", At: now, Refs: refs})
	}
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
