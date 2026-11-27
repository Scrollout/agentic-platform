package store

// T029 (feature 012, US1): the trial-set records the engine keeps (data model "Trial set", "Write
// order"; research R1). Written before the code (T009) by a test author, judged from the spec, the
// data model and the interface sheet fixed by the task brief:
//
//	func (s *Store) NextTrialSetID() (string, error)                                 // `TS-<n>`, durable on return, never given again
//	func (s *Store) PutTrialSet(sliceID, trialSetID string, record []byte) error    // replaces the set's record in its slice's folder
//	func (s *Store) TrialSet(trialSetID string) (sliceID string, record []byte, err error) // ErrNotFound when none is kept
//	func (s *Store) TrialSets(sliceID string) (map[string][]byte, error)             // every set of the slice, by ID
//	EraseSlice(slice) also removes that slice's sets.
//
// The record is opaque bytes: the engine API owns its format. Tests use JSON with a `runs` list
// only to show a record rewritten with one more run ID reads back with the runs in order.
//
// Edge cases checked: the record comes back byte for byte (binary, newlines, no trailing newline);
// it lives under the slice's folder and nowhere else; it survives a reopen; a second Put replaces;
// runs added one by one keep their order, across a reopen; an unknown slice, a malformed slice ID
// and malformed set IDs are refused and write nothing (the directory is unchanged); a set never
// kept and a set of an erased slice are ErrNotFound; a slice with no sets lists none; erasing one
// slice keeps another slice's sets and erasing a slice without sets is no error; IDs count up
// from 1, continue after a reopen, are never reissued after an ID given out without a record, nor
// after the slice (and the highest set) is erased, nor after a reopen then; concurrent calls give
// distinct IDs; erased sets leave no bytes on disk; the sequence for sets is its own (run and
// operation IDs do not move it).

import (
	"bytes"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func tsNext(t *testing.T, s *Store) string {
	t.Helper()
	id, err := s.NextTrialSetID()
	if err != nil {
		t.Fatalf("NextTrialSetID: %v", err)
	}
	return id
}

// tsSlice makes the slice known to the store (a slice exists once something is recorded for it).
func tsSlice(t *testing.T, s *Store, sliceID string) {
	t.Helper()
	appendEv(t, s, sliceID, "RUN-"+strings.TrimPrefix(sliceID, "S-")+"00", "RunStarted")
}

func tsPut(t *testing.T, s *Store, sliceID, id string, record []byte) {
	t.Helper()
	if err := s.PutTrialSet(sliceID, id, record); err != nil {
		t.Fatalf("PutTrialSet(%s, %s): %v", sliceID, id, err)
	}
}

func tsGet(t *testing.T, s *Store, id string) (string, []byte) {
	t.Helper()
	sl, rec, err := s.TrialSet(id)
	if err != nil {
		t.Fatalf("TrialSet(%s): %v", id, err)
	}
	return sl, rec
}

// tsWhere returns the files under slices/<sliceID>/ and the files elsewhere that contain needle.
func tsWhere(t *testing.T, dir, sliceID string, needle []byte) (inside, outside []string) {
	t.Helper()
	for p, b := range snapshot(t, dir) {
		if !bytes.Contains(b, needle) {
			continue
		}
		if strings.HasPrefix(filepath.ToSlash(p), "slices/"+sliceID+"/") {
			inside = append(inside, p)
		} else {
			outside = append(outside, p)
		}
	}
	return inside, outside
}

func TestNextTrialSetIDCountsUpFromOne(t *testing.T) {
	s := open(t, t.TempDir())
	for i, want := range []string{"TS-1", "TS-2", "TS-3"} {
		if got := tsNext(t, s); got != want {
			t.Errorf("call %d = %s, want %s", i+1, got, want)
		}
	}
}

func TestTrialSetIDsAreTheirOwnSequence(t *testing.T) {
	s := open(t, t.TempDir())
	if _, err := s.NextRunID(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NextOperationID(); err != nil {
		t.Fatal(err)
	}
	if got := tsNext(t, s); got != "TS-1" {
		t.Errorf("after a run and an operation ID = %s, want TS-1", got)
	}
	if got, err := s.NextRunID(); err != nil || got != "RUN-2" {
		t.Errorf("NextRunID after a trial-set ID = %s, %v; want RUN-2", got, err)
	}
}

func TestNextTrialSetIDContinuesAfterReopen(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	tsNext(t, s)
	tsNext(t, s)
	s = open(t, dir)
	if got := tsNext(t, s); got != "TS-3" {
		t.Errorf("after reopen = %s, want TS-3", got)
	}
}

func TestATrialSetIDGivenOutWithoutARecordIsNeverReissued(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	tsNext(t, s) // TS-1, never recorded (a refused start, or a crash before the write)
	s = open(t, dir)
	if got := tsNext(t, s); got != "TS-2" {
		t.Errorf("got %s, want TS-2", got)
	}
}

func TestNoTrialSetIDIsReissuedAfterItsSliceIsErased(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	tsSlice(t, s, "S-1")
	for i := 1; i <= 3; i++ {
		tsPut(t, s, "S-1", tsNext(t, s), []byte(`{"runs":[]}`))
	}
	if err := s.EraseSlice("S-1"); err != nil { // erases the highest set, so a "max of records" rule would reuse TS-3
		t.Fatal(err)
	}
	if got := tsNext(t, s); got != "TS-4" {
		t.Errorf("after erasing the slice = %s, want TS-4", got)
	}
	s = open(t, dir)
	if got := tsNext(t, s); got != "TS-5" {
		t.Errorf("after reopen with no sets = %s, want TS-5", got)
	}
}

func TestConcurrentNextTrialSetIDCallsGiveDistinctIDs(t *testing.T) {
	s := open(t, t.TempDir())
	const n = 20
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, err := s.NextTrialSetID()
			if err != nil {
				t.Errorf("NextTrialSetID: %v", err)
			}
			ids[i] = id
		}(i)
	}
	wg.Wait()
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("%s given twice", id)
		}
		seen[id] = true
	}
	if got := tsNext(t, s); got != "TS-21" {
		t.Errorf("after %d calls = %s, want TS-21", n, got)
	}
}

func TestATrialSetRecordIsReadBackByteForByte(t *testing.T) {
	s := open(t, t.TempDir())
	tsSlice(t, s, "S-1")
	for name, rec := range map[string][]byte{
		"json":                  []byte(`{"id":"TS-1","runs":["RUN-1"]}`),
		"newlines, no trailing": []byte("a\n\nb\r\n  c"),
		"binary":                {0x00, 0xff, 0x0a, 0x80, 0x00},
		"one trailing newline":  []byte("{}\n"),
	} {
		id := tsNext(t, s)
		tsPut(t, s, "S-1", id, rec)
		sl, got := tsGet(t, s, id)
		if sl != "S-1" || !bytes.Equal(got, rec) {
			t.Errorf("%s: TrialSet = %s, %q; want S-1, %q", name, sl, got, rec)
		}
	}
}

func TestATrialSetIsKeptInItsSlicesFolderAndReadBackAfterARestart(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	tsSlice(t, s, "S-1")
	tsSlice(t, s, "S-2")
	rec := []byte(`{"id":"TS-1","marker":"tsmarker-9f3a","runs":["RUN-1","RUN-2"]}`)
	id := tsNext(t, s)
	tsPut(t, s, "S-1", id, rec)
	inside, outside := tsWhere(t, dir, "S-1", []byte("tsmarker-9f3a"))
	if len(inside) == 0 {
		t.Errorf("the record is in no file under slices/S-1/")
	}
	if len(outside) != 0 {
		t.Errorf("the record is also in %v, outside its slice's folder", outside)
	}
	s = open(t, dir)
	if sl, got := tsGet(t, s, id); sl != "S-1" || !bytes.Equal(got, rec) {
		t.Errorf("after reopen: TrialSet = %s, %q; want S-1, %q", sl, got, rec)
	}
	if got, err := s.TrialSets("S-1"); err != nil || !reflect.DeepEqual(got, map[string][]byte{id: rec}) {
		t.Errorf("after reopen: TrialSets(S-1) = %v, %v", got, err)
	}
	if got, err := s.TrialSets("S-2"); err != nil || len(got) != 0 {
		t.Errorf("TrialSets(S-2) = %v, %v; want none", got, err)
	}
}

func TestPuttingATrialSetAgainReplacesItsRecord(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	tsSlice(t, s, "S-1")
	id := tsNext(t, s)
	tsPut(t, s, "S-1", id, []byte(`{"runs":[]}`))
	tsPut(t, s, "S-1", id, []byte(`{"runs":["RUN-1"]}`))
	if _, got := tsGet(t, s, id); string(got) != `{"runs":["RUN-1"]}` {
		t.Errorf("record = %q, want the second", got)
	}
	if got, err := s.TrialSets("S-1"); err != nil || len(got) != 1 {
		t.Errorf("TrialSets = %v, %v; want one set", got, err)
	}
	if inside, outside := tsWhere(t, dir, "S-1", []byte(`"runs":[]`)); len(inside)+len(outside) != 0 {
		t.Errorf("the replaced record is still on disk: %v %v", inside, outside)
	}
}

func TestRunsWrittenOneByOneKeepTheirOrder(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	tsSlice(t, s, "S-1")
	id := tsNext(t, s)
	// Run IDs are reserved from the run sequence, so a later trial may have a lower or higher
	// number than the one before; the record keeps the order they were written in.
	runs := []string{"RUN-7", "RUN-3", "RUN-12"}
	for i := range runs {
		want := `{"runs":["` + strings.Join(runs[:i+1], `","`) + `"]}`
		tsPut(t, s, "S-1", id, []byte(want))
		if _, got := tsGet(t, s, id); string(got) != want {
			t.Errorf("after %d runs: %q, want %q", i+1, got, want)
		}
		if i == 1 {
			s = open(t, dir) // a restart between two trials
		}
	}
	if _, got := tsGet(t, s, id); string(got) != `{"runs":["RUN-7","RUN-3","RUN-12"]}` {
		t.Errorf("record = %q", got)
	}
}

func TestSetsOfOneSliceAreListedByIDAndEveryOneOfThem(t *testing.T) {
	s := open(t, t.TempDir())
	tsSlice(t, s, "S-1")
	tsSlice(t, s, "S-2")
	want := map[string][]byte{}
	for i := 0; i < 12; i++ { // more than a page, so no limit hides one
		id := tsNext(t, s)
		rec := []byte(`{"n":` + id[3:] + `}`)
		sl := "S-1"
		if i%4 == 3 {
			sl = "S-2"
		} else {
			want[id] = rec
		}
		tsPut(t, s, sl, id, rec)
	}
	got, err := s.TrialSets("S-1")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("TrialSets(S-1) = %v, %v; want %v", got, err, want)
	}
}

func TestATrialSetThatWasNeverKeptIsNotFound(t *testing.T) {
	s := open(t, t.TempDir())
	tsSlice(t, s, "S-1")
	id := tsNext(t, s) // given out, never put
	for _, q := range []string{id, "TS-99"} {
		if _, _, err := s.TrialSet(q); !errors.Is(err, ErrNotFound) {
			t.Errorf("TrialSet(%s) err = %v, want ErrNotFound", q, err)
		}
	}
	if _, _, err := s.TrialSet("not-an-id"); err == nil {
		t.Error("TrialSet(not-an-id) gave no error")
	}
}

func TestASliceWithNoTrialSetsListsNone(t *testing.T) {
	s := open(t, t.TempDir())
	tsSlice(t, s, "S-1")
	if got, err := s.TrialSets("S-1"); err != nil || len(got) != 0 {
		t.Errorf("TrialSets(S-1) = %v, %v; want none and no error", got, err)
	}
}

func TestAnUnknownSliceOrABadIDWritesNothing(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	tsSlice(t, s, "S-1")
	id := tsNext(t, s)
	before := snapshot(t, dir)
	for _, c := range []struct{ sl, id, why string }{
		{"S-9", id, "unknown slice"},
		{"", id, "empty slice ID"},
		{"../S-1", id, "slice ID with a path"},
		{"s-1", id, "lowercase slice ID"},
		{"S-1", "", "empty set ID"},
		{"S-1", "TS-", "no number"},
		{"S-1", "TS-x", "not a number"},
		{"S-1", "ts-1", "lowercase"},
		{"S-1", "TS--1", "negative"},
		{"S-1", "TS-1 ", "trailing space"},
		{"S-1", "TS-1/../../x", "path in the ID"},
		{"S-1", "../TS-1", "path before the ID"},
		{"S-1", "RUN-1", "another kind of ID"},
		{"S-1", "TS-1\n", "trailing newline"},
	} {
		if err := s.PutTrialSet(c.sl, c.id, []byte("x")); err == nil {
			t.Errorf("%s: PutTrialSet(%q, %q) gave no error", c.why, c.sl, c.id)
		}
	}
	if after := snapshot(t, dir); !reflect.DeepEqual(before, after) {
		t.Errorf("a refused PutTrialSet changed the store directory")
	}
	if _, _, err := s.TrialSet(id); !errors.Is(err, ErrNotFound) {
		t.Errorf("TrialSet(%s) after refused puts: err = %v, want ErrNotFound", id, err)
	}
}

func TestErasingASliceRemovesItsTrialSets(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	tsSlice(t, s, "S-1")
	id1, id2 := tsNext(t, s), tsNext(t, s)
	tsPut(t, s, "S-1", id1, []byte(`{"marker":"erase-me-71c2"}`))
	tsPut(t, s, "S-1", id2, []byte(`{"marker":"erase-me-71c2"}`))
	if err := s.EraseSlice("S-1"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{id1, id2} {
		if _, _, err := s.TrialSet(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("TrialSet(%s) of an erased slice: err = %v, want ErrNotFound", id, err)
		}
	}
	if got, err := s.TrialSets("S-1"); err != nil || len(got) != 0 {
		t.Errorf("TrialSets(S-1) after erasure = %v, %v; want none", got, err)
	}
	if anywhere(t, dir, "erase-me-71c2") {
		t.Error("the erased sets' content is still on disk")
	}
	s = open(t, dir)
	if _, _, err := s.TrialSet(id1); !errors.Is(err, ErrNotFound) {
		t.Errorf("after reopen: TrialSet(%s) err = %v, want ErrNotFound", id1, err)
	}
	// An erased slice is unknown again: nothing may be written to it.
	if err := s.PutTrialSet("S-1", tsNext(t, s), []byte("x")); err == nil {
		t.Error("PutTrialSet into an erased slice gave no error")
	}
}

func TestErasingOneSliceKeepsAnotherSlicesTrialSets(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	tsSlice(t, s, "S-1")
	tsSlice(t, s, "S-2")
	a, b := tsNext(t, s), tsNext(t, s)
	tsPut(t, s, "S-1", a, []byte(`{"slice":1}`))
	tsPut(t, s, "S-2", b, []byte(`{"slice":2}`))
	if err := s.EraseSlice("S-1"); err != nil {
		t.Fatal(err)
	}
	for _, st := range []*Store{s, open(t, dir)} {
		if sl, rec := tsGet(t, st, b); sl != "S-2" || string(rec) != `{"slice":2}` {
			t.Errorf("TrialSet(%s) = %s, %q; want S-2 and its record", b, sl, rec)
		}
		if got, err := st.TrialSets("S-2"); err != nil || !reflect.DeepEqual(got, map[string][]byte{b: []byte(`{"slice":2}`)}) {
			t.Errorf("TrialSets(S-2) = %v, %v", got, err)
		}
	}
}

func TestErasingASliceWithoutTrialSetsIsNoError(t *testing.T) {
	s := open(t, t.TempDir())
	tsSlice(t, s, "S-1")
	if err := s.EraseSlice("S-1"); err != nil {
		t.Errorf("EraseSlice: %v", err)
	}
	if err := s.EraseSlice("S-1"); err != nil {
		t.Errorf("EraseSlice again: %v", err)
	}
}
