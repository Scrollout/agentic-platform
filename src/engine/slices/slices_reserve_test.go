// Tests for T038j: when the ID sequence cannot be saved, Reserve must not hand out an ID that a
// restart could reissue. Reserve() string keeps its signature, so the failure shows in what
// follows: the ID is not reserved, CreateAs with it is refused, nothing is stored, and Create
// fails. After the store recovers, the next ID has never been stored.
package slices_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/slice"
)

// memRecords is an in-memory slices.Records whose sequence write can be made to fail.
type memRecords struct {
	mu      sync.Mutex
	recs    map[string][]byte
	seq     []byte
	seqFail bool
	// stored lists every slice ID a record was ever written under.
	stored []string
}

func newMemRecords() *memRecords { return &memRecords{recs: map[string][]byte{}} }

func (m *memRecords) SliceRecords() (map[string][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string][]byte{}
	for k, v := range m.recs {
		out[k] = v
	}
	return out, nil
}

func (m *memRecords) PutSliceRecord(id string, record []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recs[id] = record
	m.stored = append(m.stored, id)
	return nil
}

func (m *memRecords) DeleteSliceRecord(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.recs, id)
	return nil
}

func (m *memRecords) SliceSequence() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seq, nil
}

func (m *memRecords) PutSliceSequence(seq []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seqFail {
		return errors.New("disk full")
	}
	m.seq = seq
	return nil
}

func (m *memRecords) setSeqFail(v bool) { m.mu.Lock(); m.seqFail = v; m.mu.Unlock() }

func (m *memRecords) everStored(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.stored {
		if s == id {
			return true
		}
	}
	return false
}

func serviceOn(t *testing.T, rec slices.Records) *slices.Service {
	t.Helper()
	schemas, err := slice.LoadSchemas("../api/preview/2026-11-12-preview")
	if err != nil {
		t.Fatal(err)
	}
	pattern, err := os.ReadFile("../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatal(err)
	}
	a := &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}
	s, err := slices.New(slices.Config{Schemas: schemas, Analyst: a, Diff: &fakeDiff{}, Pattern: pattern,
		Lint: &fakeLint{}, Records: rec})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReserveWhenTheSequenceCannotBeSavedMakesNoReservation(t *testing.T) {
	ctx := context.Background()
	rec := newMemRecords()
	s := serviceOn(t, rec)
	first := create(t, s, "Let me keep notes.") // a stored slice, so S-1 exists

	rec.setSeqFail(true)
	failed := s.Reserve()

	// The ID, if any was returned, is not a reservation: CreateAs is refused with a code.
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("CreateAs panicked: %v", r)
			}
		}()
		_, err = s.CreateAs(ctx, failed, "Let me keep other notes.")
	}()
	var se *slices.Error
	if err == nil || !errors.As(err, &se) || se.Code == "" {
		t.Fatalf("CreateAs(%q) after a failed sequence save = %v, want an error with a code", failed, err)
	}
	if got := s.List(); len(got) != 1 || got[0].ID != first.ID {
		t.Errorf("slices after the refused CreateAs = %v, want only %s", ids(got), first.ID)
	}
	if failed != "" && failed != first.ID && rec.everStored(failed) {
		t.Errorf("a record was stored under the unreserved ID %q", failed)
	}

	// Create draws from the same sequence and cannot save it either.
	if _, err := s.Create(ctx, "Let me keep more notes."); err == nil {
		t.Error("Create succeeded while the ID sequence could not be saved")
	}
	if got := s.List(); len(got) != 1 {
		t.Errorf("slices after the refused Create = %v, want only %s", ids(got), first.ID)
	}

	// The store recovers: the next ID was never stored, here and after a restart.
	rec.setSeqFail(false)
	next := s.Reserve()
	if next == "" || next == first.ID || rec.everStored(next) {
		t.Fatalf("Reserve after recovery = %q, want an ID never stored (stored: %v)", next, rec.stored)
	}
	r, err := s.CreateAs(ctx, next, "Let me keep yet more notes.")
	if err != nil || r.Slice.ID != next {
		t.Fatalf("CreateAs(%q) after recovery = %v, %v", next, r.Slice.ID, err)
	}
	restarted := serviceOn(t, rec)
	if after := restarted.Reserve(); after == first.ID || after == next {
		t.Errorf("after a restart Reserve reissued %q", after)
	}
}
