package slices

import (
	"context"
	"sync"
)

// The slice's check results under the engine's method (feature 006, research R4, ADR 0080).

// methodIf gives the service's method identity when every check ran, "" otherwise, so a slice
// whose check could not run is checked again on its next read.
func (s *Service) methodIf(ran bool) string {
	if !ran {
		return ""
	}
	return s.method
}

// changingLocked returns the mutex that serialises the changes of slice id. The caller holds s.mu.
func (s *Service) changingLocked(id string) *sync.Mutex {
	m := s.changing[id]
	if m == nil {
		m = &sync.Mutex{}
		s.changing[id] = m
	}
	return m
}

// current runs the checks of slice id again when they were produced under another method than
// the service's (or under none), before any operation reads the slice: the part checks, the
// contract's compatibility and its lint. Goals, rules, examples, terms and contract stay as they
// are; a part whose check now fails loses its approval (withdrawn). The slice then records the
// service's method, unless a check could not run, so the next read checks again; a store that
// fails keeps the new results in memory and the slice stale. oasdiff and the lint run without
// s.mu, under the slice's changing mutex, so a slow check holds up no other slice and two reads
// check once; an approval or answer that lands meanwhile is kept, and the checks run again when
// the contract or its baseline changed meanwhile, at most maxRechecks times. A service without a
// method re-checks nothing. NotFound and InvalidRequest are find's.
func (s *Service) current(ctx context.Context, id string) error {
	if s.method == "" {
		return nil
	}
	want := s.method
	s.mu.Lock()
	r, err := s.find(id)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if r.slice.ChecksMethod == want {
		s.mu.Unlock()
		return nil
	}
	m := s.changingLocked(id)
	s.mu.Unlock()
	m.Lock()
	defer m.Unlock()

	for range maxRechecks {
		s.mu.Lock()
		r, err := s.find(id)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		if r.slice.ChecksMethod == want {
			s.mu.Unlock()
			return nil
		}
		base, contract := r.baseline, r.slice.Contract
		s.mu.Unlock()

		compat, compatRan := s.compatibilityRan(ctx, base, contract)
		lint, lintRan := s.lintContractRan(ctx, contract)

		s.mu.Lock()
		r, err = s.find(id)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		if r.baseline != base || r.slice.Contract != contract {
			s.mu.Unlock()
			continue
		}
		next := &record{slice: clone(r.slice), baseline: r.baseline, compat: compat, lint: lint,
			dropped: r.dropped, examplesBase: r.examplesBase}
		if err := s.recheck(ctx, next, false); err != nil {
			s.mu.Unlock()
			return err
		}
		withdrawFailing(&next.slice, s.now())
		next.slice.State = state(&next.slice)
		next.slice.ChecksMethod = s.methodIf(compatRan && lintRan)
		if err := s.save(next); err != nil {
			s.log.Error("the slice's checks under the engine's method could not be stored; the next read checks again",
				"slice", id, "error", err.Error())
			next.slice.ChecksMethod = ""
		}
		*r = *next
		s.mu.Unlock()
		return nil
	}
	return nil // the contract kept changing; the next read checks again
}
