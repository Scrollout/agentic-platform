// Tests for the run store (T014, written before the implementation, T017).
//
// The store keeps run records (append-only events) and artifacts (content
// addressed by SHA-256) in one directory, and groups both by the slice they
// belong to so that erasure by slice (Principle XI) is possible. Events are
// acknowledged only after they are durably recorded (Principle X).
//
// API the tests call (package store; the store layout on disk is the
// implementer's choice, the tests only look at the directory generically):
//
//	var ErrNotFound error // returned (wrapped is fine) for an unknown artifact
//
//	type Event struct {
//		Seq  int    // set by the store; 1, 2, 3 ... per run, without gaps
//		Run  string // "RUN-<n>"
//		Kind string // e.g. "RunStarted"
//		Step string // optional
//	}
//
//	func Open(dir string, ids *slice.Schemas) (*Store, error) // (T017a: ids is the contract rules; the tests load them with testIDs) // creates dir contents; reopening continues the records
//	func (s *Store) Close() error
//
//	// Append records e under slice, assigns the next Seq of e.Run, and returns
//	// the event as stored. It returns only after the event is durably recorded.
//	// The first Append for a run ties the run to slice.
//	func (s *Store) Append(slice string, e Event) (Event, error)
//
//	// Events returns every event of run in Seq order; an unknown run gives none.
//	func (s *Store) Events(run string) ([]Event, error)
//
//	// PutArtifact stores data for slice and returns its lowercase hex SHA-256.
//	// The same bytes in the same slice are stored once.
//	func (s *Store) PutArtifact(slice string, data []byte) (string, error)
//
//	// Artifact returns the exact bytes stored under hash, or ErrNotFound.
//	// A string that is not a 64-digit lowercase hex hash is an error and never
//	// reaches the file system as a path.
//	func (s *Store) Artifact(hash string) ([]byte, error)
//
//	// EraseSlice removes every event of the slice's runs and every artifact
//	// stored for the slice, from disk. Other slices are untouched. An artifact
//	// that other slices also stored stays readable for them.
//	func (s *Store) EraseSlice(slice string) error
//
// There is deliberately no function that rewrites or deletes a single event.
package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func open(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir, testIDs(t))
	if err != nil {
		t.Fatalf("Open(%q): %v", dir, err)
	}
	return s
}

func appendEv(t *testing.T, s *Store, slice, run, kind string) Event {
	t.Helper()
	e, err := s.Append(slice, Event{Run: run, Kind: kind})
	if err != nil {
		t.Fatalf("Append(%s, %s, %s): %v", slice, run, kind, err)
	}
	return e
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// snapshot reads every regular file under dir, keyed by relative path.
func snapshot(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	m := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		m[rel] = b
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// anywhere reports whether needle occurs in any file content or path under dir.
func anywhere(t *testing.T, dir, needle string) bool {
	t.Helper()
	for p, b := range snapshot(t, dir) {
		if strings.Contains(p, needle) || bytes.Contains(b, []byte(needle)) {
			return true
		}
	}
	return false
}

func TestAppendAssignsIncreasingSeqPerRun(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()

	for i, kind := range []string{"RunStarted", "StepStarted", "StepAccepted"} {
		if got := appendEv(t, s, "S-1", "RUN-1", kind); got.Seq != i+1 {
			t.Fatalf("run RUN-1 event %d: Seq = %d, want %d", i, got.Seq, i+1)
		}
	}
	// Another run counts from 1 on its own.
	if got := appendEv(t, s, "S-1", "RUN-2", "RunStarted"); got.Seq != 1 {
		t.Fatalf("run RUN-2 first Seq = %d, want 1", got.Seq)
	}

	evs, err := s.Events("RUN-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("Events(RUN-1) = %d events, want 3", len(evs))
	}
	for i, e := range evs {
		if e.Seq != i+1 || e.Run != "RUN-1" {
			t.Errorf("event %d = %+v, want Seq %d of RUN-1", i, e, i+1)
		}
	}
	if evs[0].Kind != "RunStarted" || evs[2].Kind != "StepAccepted" {
		t.Errorf("kinds out of order: %+v", evs)
	}
}

func TestEventsOfUnknownRunIsEmpty(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()
	evs, err := s.Events("RUN-99")
	if err != nil || len(evs) != 0 {
		t.Fatalf("Events(RUN-99) = %v, %v; want no events, no error", evs, err)
	}
}

func TestAppendConcurrentlyLeavesNoGapsOrDuplicates(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()
	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Append("S-1", Event{Run: "RUN-1", Kind: "AttemptMade"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	evs, err := s.Events("RUN-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != n {
		t.Fatalf("got %d events, want %d", len(evs), n)
	}
	for i, e := range evs {
		if e.Seq != i+1 {
			t.Fatalf("event %d has Seq %d, want %d", i, e.Seq, i+1)
		}
	}
}

func TestReopenedStoreSeesEarlierEventsAndContinuesSeq(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	appendEv(t, s, "S-1", "RUN-1", "RunStarted")
	appendEv(t, s, "S-1", "RUN-1", "StepStarted")
	hash, err := s.PutArtifact("S-1", []byte("kept"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s = open(t, dir)
	defer s.Close()
	evs, err := s.Events("RUN-1")
	if err != nil || len(evs) != 2 || evs[1].Kind != "StepStarted" {
		t.Fatalf("after reopen Events = %+v, %v; want the 2 earlier events", evs, err)
	}
	if got := appendEv(t, s, "S-1", "RUN-1", "StepAccepted"); got.Seq != 3 {
		t.Fatalf("Seq after reopen = %d, want 3", got.Seq)
	}
	if b, err := s.Artifact(hash); err != nil || string(b) != "kept" {
		t.Fatalf("artifact after reopen = %q, %v", b, err)
	}
}

func TestAcknowledgedEventIsOnDiskBeforeClose(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	defer s.Close()
	appendEv(t, s, "S-1", "RUN-1", "RunStarted")
	// No Close yet: a crash now must not lose the acknowledged event.
	if !anywhere(t, dir, "RunStarted") {
		t.Fatal("an acknowledged event is not in any file under the store directory")
	}
}

func TestArtifactStoredOnceUnderItsSHA256(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	defer s.Close()
	data := []byte("the same bytes\x00\xff twice")

	h1, err := s.PutArtifact("S-1", data)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != sum(data) {
		t.Fatalf("hash = %s, want sha256 %s", h1, sum(data))
	}
	before := snapshot(t, dir)
	h2, err := s.PutArtifact("S-1", data)
	if err != nil {
		t.Fatal(err)
	}
	if h2 != h1 {
		t.Fatalf("second hash = %s, want %s", h2, h1)
	}
	after := snapshot(t, dir)
	if len(after) != len(before) {
		t.Fatalf("storing the same bytes again changed the file count: %d -> %d", len(before), len(after))
	}
	copies := 0
	for _, b := range after {
		if bytes.Equal(b, data) {
			copies++
		}
	}
	if copies != 1 {
		t.Fatalf("%d on-disk copies of the artifact, want 1", copies)
	}

	got, err := s.Artifact(h1)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("Artifact(%s) = %q, %v; want the exact bytes", h1, got, err)
	}
}

func TestArtifactEmptyAndDistinctContent(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()
	he, err := s.PutArtifact("S-1", nil)
	if err != nil || he != sum(nil) {
		t.Fatalf("empty artifact hash = %q, %v; want %s", he, err, sum(nil))
	}
	ha, _ := s.PutArtifact("S-1", []byte("a"))
	hb, _ := s.PutArtifact("S-1", []byte("b"))
	if ha == hb {
		t.Fatal("different bytes got the same hash")
	}
	if b, err := s.Artifact(hb); err != nil || string(b) != "b" {
		t.Fatalf("Artifact(hb) = %q, %v", b, err)
	}
}

func TestArtifactUnknownOrMalformedHash(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()
	if _, err := s.Artifact(sum([]byte("never stored"))); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown hash error = %v, want ErrNotFound", err)
	}
	for _, bad := range []string{"", "abc", "../../etc/passwd", strings.Repeat("A", 64), strings.Repeat("g", 64)} {
		if b, err := s.Artifact(bad); err == nil {
			t.Errorf("Artifact(%q) = %q, nil; want an error", bad, b)
		}
	}
}

func TestNoEventIsEverRewritten(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	defer s.Close()
	appendEv(t, s, "S-1", "RUN-1", "RunStarted")
	appendEv(t, s, "S-1", "RUN-1", "StepStarted")
	if _, err := s.PutArtifact("S-1", []byte("artifact")); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, dir)
	firstEvents, _ := s.Events("RUN-1")

	appendEv(t, s, "S-1", "RUN-1", "StepAccepted")
	appendEv(t, s, "S-1", "RUN-2", "RunStarted")
	if _, err := s.PutArtifact("S-1", []byte("another")); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, dir)

	// Every earlier file is still there and its earlier bytes are a prefix of
	// its later bytes: records only grow at the end.
	for p, old := range before {
		now, ok := after[p]
		if !ok {
			t.Errorf("file %s disappeared after more appends", p)
			continue
		}
		if !bytes.HasPrefix(now, old) {
			t.Errorf("file %s: earlier content was changed, not only appended to", p)
		}
	}
	grew := false
	for p, old := range before {
		if len(after[p]) > len(old) {
			grew = true
		}
	}
	if !grew {
		t.Error("no earlier file grew: the new event was recorded nowhere the old ones are")
	}

	evs, _ := s.Events("RUN-1")
	for i, e := range firstEvents {
		if evs[i] != e {
			t.Errorf("event %d changed: %+v -> %+v", i, e, evs[i])
		}
	}
}

func TestEraseSliceRemovesItsRecordsAndArtifactsOnly(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	defer s.Close()

	appendEv(t, s, "S-1", "RUN-1", "RunStarted")
	appendEv(t, s, "S-1", "RUN-1", "StepStarted")
	appendEv(t, s, "S-2", "RUN-2", "RunStarted")
	secretS1 := []byte("protected content of slice one")
	keepS2 := []byte("protected content of slice two")
	h1, _ := s.PutArtifact("S-1", secretS1)
	h2, _ := s.PutArtifact("S-2", keepS2)

	if err := s.EraseSlice("S-1"); err != nil {
		t.Fatal(err)
	}

	if evs, err := s.Events("RUN-1"); err != nil || len(evs) != 0 {
		t.Errorf("Events(RUN-1) after erase = %v, %v; want none", evs, err)
	}
	if _, err := s.Artifact(h1); !errors.Is(err, ErrNotFound) {
		t.Errorf("erased artifact error = %v, want ErrNotFound", err)
	}
	if anywhere(t, dir, string(secretS1)) || anywhere(t, dir, h1) {
		t.Error("erased artifact still on disk (content or file name)")
	}
	if anywhere(t, dir, "RUN-1") {
		t.Error("erased run's records still on disk")
	}

	if evs, _ := s.Events("RUN-2"); len(evs) != 1 || evs[0].Kind != "RunStarted" {
		t.Errorf("other slice's events = %+v, want its one event", evs)
	}
	if b, err := s.Artifact(h2); err != nil || !bytes.Equal(b, keepS2) {
		t.Errorf("other slice's artifact = %q, %v; want intact", b, err)
	}
	// The erased slice can be written again, and its run starts again at 1.
	if got := appendEv(t, s, "S-1", "RUN-1", "RunStarted"); got.Seq != 1 {
		t.Errorf("Seq after erase = %d, want 1", got.Seq)
	}
}

func TestEraseSliceSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	appendEv(t, s, "S-1", "RUN-1", "RunStarted")
	h, _ := s.PutArtifact("S-1", []byte("gone"))
	if err := s.EraseSlice("S-1"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s = open(t, dir)
	defer s.Close()
	if evs, _ := s.Events("RUN-1"); len(evs) != 0 {
		t.Errorf("erased events came back after reopen: %+v", evs)
	}
	if _, err := s.Artifact(h); !errors.Is(err, ErrNotFound) {
		t.Errorf("erased artifact came back after reopen: %v", err)
	}
}

func TestEraseSliceKeepsAnArtifactAnotherSliceAlsoStored(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()
	shared := []byte("identical bytes in two slices")
	h1, _ := s.PutArtifact("S-1", shared)
	h2, _ := s.PutArtifact("S-2", shared)
	if h1 != h2 {
		t.Fatalf("hashes differ: %s %s", h1, h2)
	}
	if err := s.EraseSlice("S-1"); err != nil {
		t.Fatal(err)
	}
	if b, err := s.Artifact(h2); err != nil || !bytes.Equal(b, shared) {
		t.Fatalf("S-2's artifact after erasing S-1 = %q, %v; want intact", b, err)
	}
}

func TestEraseUnknownSliceIsNotAnError(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()
	if err := s.EraseSlice("S-404"); err != nil {
		t.Fatalf("EraseSlice of an unknown slice = %v, want nil", err)
	}
}
