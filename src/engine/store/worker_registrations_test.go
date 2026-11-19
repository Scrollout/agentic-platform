package store

// T043, store side: the step worker registrations the engine keeps (ADR 0071, data model "Step
// worker registrations", research R10). Written before the code (T045) by a test author, judged
// from the ADR, the data model and the interface fixed by the task brief:
//
//	type WorkerRegistration struct{ Slice, Step, Endpoint, Grant string } // Grant: lowercase hex SHA-256 naming the grant file (ADR 0034)
//	func (s *Store) WorkerRegistrations() ([]WorkerRegistration, error) // every kept registration, sorted by slice then step
//	func (s *Store) PutWorkerRegistration(r WorkerRegistration) error  // adds or replaces the one for r.Slice and r.Step
//	func (s *Store) DeleteWorkerRegistration(slice, step string) error // removes it; absent is no error
//	EraseSlice(slice) also removes that slice's registrations.
//
// On disk: one file, <store dir>/workers.json, written atomically (a temporary file renamed over
// it), with the mode of the store's other files. It holds no token.
//
// Assumed beyond the brief, from the package doc ("every string a method takes is parsed against
// [the contract's rules] first") and the constitution's "Values that check themselves": a slice ID
// that breaks the contract's rule, and a Grant that is not a lowercase hex SHA-256 (the contract's
// hash rule), are refused and change nothing on disk.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const wrFile = "workers.json"

// wrHash is the lowercase hex SHA-256 of token: the name of its grant file (ADR 0034).
func wrHash(token string) string {
	s := sha256.Sum256([]byte(token))
	return hex.EncodeToString(s[:])
}

func wrReg(slice, step, endpoint, workerToken string) WorkerRegistration {
	return WorkerRegistration{Slice: slice, Step: step, Endpoint: endpoint, Grant: wrHash(workerToken)}
}

func wrPut(t *testing.T, s *Store, r WorkerRegistration) {
	t.Helper()
	if err := s.PutWorkerRegistration(r); err != nil {
		t.Fatalf("PutWorkerRegistration(%+v): %v", r, err)
	}
}

func wrList(t *testing.T, s *Store) []WorkerRegistration {
	t.Helper()
	got, err := s.WorkerRegistrations()
	if err != nil {
		t.Fatalf("WorkerRegistrations: %v", err)
	}
	return got
}

func wrWant(t *testing.T, what string, got, want []WorkerRegistration) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: WorkerRegistrations = %+v, want %+v", what, got, want)
	}
}

func TestAPutRegistrationIsListed(t *testing.T) {
	s := open(t, t.TempDir())
	wrWant(t, "an empty store", wrList(t, s), nil)
	r := wrReg("S-1", "code", "http://code:8080", "worker-token-code")
	wrPut(t, s, r)
	wrWant(t, "after one put", wrList(t, s), []WorkerRegistration{r})
}

func TestPuttingAgainReplacesTheRegistrationOfThatSliceAndStep(t *testing.T) {
	s := open(t, t.TempDir())
	wrPut(t, s, wrReg("S-1", "code", "http://old:8080", "worker-token-old"))
	other := wrReg("S-1", "tests", "http://tests:8080", "worker-token-tests")
	wrPut(t, s, other)
	newer := wrReg("S-1", "code", "http://new:9090", "worker-token-new")
	wrPut(t, s, newer)
	wrWant(t, "after replacing code", wrList(t, s), []WorkerRegistration{newer, other})
}

func TestRegistrationsAreListedBySliceThenStep(t *testing.T) {
	s := open(t, t.TempDir())
	in := []WorkerRegistration{
		wrReg("S-2", "generate", "http://b:8080", "t1"),
		wrReg("S-1", "review", "http://c:8080", "t2"),
		wrReg("S-3", "code", "http://d:8080", "t3"),
		wrReg("S-1", "code", "http://e:8080", "t4"),
		wrReg("S-2", "code", "http://f:8080", "t5"),
		wrReg("S-1", "generate", "http://g:8080", "t6"),
	}
	for _, r := range in {
		wrPut(t, s, r)
	}
	want := []WorkerRegistration{in[3], in[5], in[1], in[4], in[0], in[2]}
	wrWant(t, "sorted", wrList(t, s), want)
	wrWant(t, "sorted after a reopen", wrList(t, open(t, s.root)), want)
}

func TestDeleteRemovesOnlyThatRegistrationAndAnAbsentOneIsNoError(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	// Nothing kept yet, not even the file: deleting is no error.
	if err := s.DeleteWorkerRegistration("S-1", "code"); err != nil {
		t.Fatalf("DeleteWorkerRegistration on an empty store: %v", err)
	}
	a := wrReg("S-1", "code", "http://a:8080", "ta")
	b := wrReg("S-1", "tests", "http://b:8080", "tb")
	c := wrReg("S-2", "code", "http://c:8080", "tc")
	for _, r := range []WorkerRegistration{a, b, c} {
		wrPut(t, s, r)
	}
	if err := s.DeleteWorkerRegistration("S-1", "code"); err != nil {
		t.Fatalf("DeleteWorkerRegistration(S-1, code): %v", err)
	}
	wrWant(t, "after the delete", wrList(t, s), []WorkerRegistration{b, c})
	for _, k := range [][2]string{{"S-1", "code"}, {"S-1", "review"}, {"S-7", "code"}} {
		if err := s.DeleteWorkerRegistration(k[0], k[1]); err != nil {
			t.Errorf("DeleteWorkerRegistration(%s, %s) of an absent registration: %v", k[0], k[1], err)
		}
	}
	wrWant(t, "after deleting absent ones", wrList(t, s), []WorkerRegistration{b, c})
	wrWant(t, "after a reopen", wrList(t, open(t, dir)), []WorkerRegistration{b, c})
}

func TestRegistrationsSurviveAReopenOfTheStore(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	a := wrReg("S-1", "code", "http://a:8080", "ta")
	b := wrReg("S-1", "review", "http://b:8080", "tb")
	wrPut(t, s, a)
	wrPut(t, s, b)
	wrPut(t, s, wrReg("S-2", "tests", "http://gone:8080", "tc"))
	if err := s.DeleteWorkerRegistration("S-2", "tests"); err != nil {
		t.Fatal(err)
	}
	a2 := wrReg("S-1", "code", "http://a2:8080", "ta2")
	wrPut(t, s, a2)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2 := open(t, dir)
	wrWant(t, "after a reopen", wrList(t, s2), []WorkerRegistration{a2, b})
	// The reopened store goes on from what it read.
	c := wrReg("S-3", "code", "http://c:8080", "td")
	wrPut(t, s2, c)
	wrWant(t, "after a put on the reopened store", wrList(t, open(t, dir)), []WorkerRegistration{a2, b, c})
}

func TestEraseSliceRemovesThatSlicesRegistrationsAndKeepsAnothers(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	appendEv(t, s, "S-1", "RUN-1", "RunStarted") // S-1 has records besides its registrations
	keep := wrReg("S-2", "code", "http://s2-code:8080", "t-s2-code")
	wrPut(t, s, wrReg("S-1", "code", "http://s1-code:8080", "t-s1-code"))
	wrPut(t, s, wrReg("S-1", "review", "http://s1-review:8080", "t-s1-review"))
	wrPut(t, s, keep)
	if err := s.EraseSlice("S-1"); err != nil {
		t.Fatalf("EraseSlice(S-1): %v", err)
	}
	wrWant(t, "after erasing S-1", wrList(t, s), []WorkerRegistration{keep})
	wrWant(t, "after erasing S-1 and a reopen", wrList(t, open(t, dir)), []WorkerRegistration{keep})
	b, err := os.ReadFile(filepath.Join(dir, wrFile))
	if err != nil {
		t.Fatalf("reading %s: %v", wrFile, err)
	}
	for _, gone := range []string{"s1-code", "s1-review", wrHash("t-s1-code"), wrHash("t-s1-review")} {
		if bytes.Contains(b, []byte(gone)) {
			t.Errorf("%s still holds %q of the erased slice: %s", wrFile, gone, b)
		}
	}
}

func TestEraseSliceRemovesRegistrationsOfASliceWithNoOtherRecords(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	keep := wrReg("S-1", "code", "http://s1:8080", "t1")
	wrPut(t, s, keep)
	wrPut(t, s, wrReg("S-2", "tests", "http://s2:8080", "t2")) // S-2 has no events or artifacts
	if err := s.EraseSlice("S-2"); err != nil {
		t.Fatalf("EraseSlice(S-2): %v", err)
	}
	wrWant(t, "after erasing S-2", wrList(t, s), []WorkerRegistration{keep})
	wrWant(t, "after erasing S-2 and a reopen", wrList(t, open(t, dir)), []WorkerRegistration{keep})
}

func TestTheRegistrationsFileHoldsTheEndpointAndGrantHashButNoToken(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	const workerTok = "planted-worker-token-7f3a9c"
	const callTok = "planted-call-token-41d2e8" // the grant file's call_token; the store never gets it
	r := wrReg("S-1", "generate", "http://generate-worker:8080", workerTok)
	wrPut(t, s, r)
	b, err := os.ReadFile(filepath.Join(dir, wrFile))
	if err != nil {
		t.Fatalf("the registration is not in %s: %v", wrFile, err)
	}
	for _, want := range []string{r.Endpoint, r.Grant, "S-1", "generate"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("%s lacks %q: %s", wrFile, want, b)
		}
	}
	for _, tok := range []string{workerTok, callTok} {
		if anywhere(t, dir, tok) {
			t.Errorf("the store directory holds the token %q", tok)
		}
	}
}

func TestTheRegistrationsFileHasTheModeOfTheStoresOtherFiles(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	appendEv(t, s, "S-1", "RUN-1", "RunStarted")
	if err := s.PutSliceSequence([]byte(`{"last":1}`)); err != nil {
		t.Fatal(err)
	}
	wrPut(t, s, wrReg("S-1", "code", "http://code:8080", "t"))
	mode := func(rel string) fs.FileMode {
		t.Helper()
		fi, err := os.Stat(filepath.Join(dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		return fi.Mode()
	}
	got := mode(wrFile)
	for _, other := range []string{filepath.Join("slices", "S-1", eventsFile), sequenceFile} {
		if want := mode(other); got != want {
			t.Errorf("%s mode = %v, want %v as %s", wrFile, got, want, other)
		}
	}
	if !got.IsRegular() || got.Perm()&0o077 != 0 {
		t.Errorf("%s mode = %v, want a regular file only its owner can read", wrFile, got)
	}
	// A replacement keeps the mode.
	wrPut(t, s, wrReg("S-1", "code", "http://code2:8080", "t2"))
	if again := mode(wrFile); again != got {
		t.Errorf("%s mode after a replacement = %v, want %v", wrFile, again, got)
	}
}

func TestTheRegistrationsFileIsReplacedByARenameAndLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	wrPut(t, s, wrReg("S-1", "code", "http://a:8080", "ta"))
	path := filepath.Join(dir, wrFile)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wrPut(t, s, wrReg("S-1", "tests", "http://b:8080", "tb"))
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Written in place, a reader could see part of the old file and part of the new one; a
	// rename gives the path a new file and leaves the old one whole.
	if os.SameFile(before, after) {
		t.Errorf("%s was rewritten in place, not replaced by a rename of a complete file", wrFile)
	}
	if b, _ := os.ReadFile(path); bytes.Equal(b, old) {
		t.Errorf("%s did not change on a put", wrFile)
	}
	if err := s.DeleteWorkerRegistration("S-1", "tests"); err != nil {
		t.Fatal(err)
	}
	if deleted, err := os.Stat(path); err == nil && os.SameFile(after, deleted) {
		t.Errorf("%s was rewritten in place by a delete", wrFile)
	}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".tmp") || strings.HasSuffix(d.Name(), ".tmp") {
			t.Errorf("a temporary file is left: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAnInvalidSliceIDOrGrantIsRefusedAndChangesNothing(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	kept := wrReg("S-1", "code", "http://a:8080", "ta")
	wrPut(t, s, kept)
	before := treeOf(t, dir)
	good := wrHash("tb")
	for _, r := range []WorkerRegistration{
		{Slice: "", Step: "tests", Endpoint: "http://b:8080", Grant: good},
		{Slice: "../S-1", Step: "tests", Endpoint: "http://b:8080", Grant: good},
		{Slice: "s-1", Step: "tests", Endpoint: "http://b:8080", Grant: good},
		{Slice: "RUN-1", Step: "tests", Endpoint: "http://b:8080", Grant: good},
		{Slice: "S-1", Step: "tests", Endpoint: "http://b:8080", Grant: ""},
		{Slice: "S-1", Step: "tests", Endpoint: "http://b:8080", Grant: strings.ToUpper(good)},
		{Slice: "S-1", Step: "tests", Endpoint: "http://b:8080", Grant: good[:63]},
		{Slice: "S-1", Step: "tests", Endpoint: "http://b:8080", Grant: "planted-worker-token-in-place-of-its-hash"},
	} {
		if err := s.PutWorkerRegistration(r); err == nil {
			t.Errorf("PutWorkerRegistration(%+v) = nil, want an error", r)
		}
	}
	sameTree(t, "refused puts", before, treeOf(t, dir))
	wrWant(t, "after refused puts", wrList(t, s), []WorkerRegistration{kept})
}
