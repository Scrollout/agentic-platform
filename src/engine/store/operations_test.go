package store

// T022: the operation records the engine keeps (ADR 0072, data model "Operation record"). Written
// before the code by a test author, judged from the ADR, the data model and the interface fixed by
// the task brief:
//
//	type OperationRecord struct { ID, Kind, Target, Key, Body string; AcceptedAt time.Time; EndedAt *time.Time;
//	    Status string; Result, Error json.RawMessage }  // JSON: id kind target key,omitempty body accepted_at ended_at,omitempty status,omitempty result,omitempty error,omitempty
//	func (s *Store) NextOperationID() (string, error)
//	func (s *Store) PutOperation(r OperationRecord, body []byte) error
//	func (s *Store) Operation(id string) (OperationRecord, []byte, error)   // ErrNotFound when none; body nil once ended
//	func (s *Store) OperationByKey(key string) (OperationRecord, bool, error)
//	func (s *Store) OpenOperations() ([]OperationRecord, error)             // not ended, ascending number
//	func (s *Store) EndOperation(id string, at time.Time, status string, result, problem json.RawMessage) (bool, error)
//	func (s *Store) RemoveOperationsEndedBefore(t time.Time) ([]string, error)
//	func (s *Store) DeleteOperation(id string) error
//
// On disk: operations/<id>.json and operations/<id>.body, each written atomically, the body first,
// with the mode of the store's other files; operation-sequence.json at the store root.
//
// Assumed beyond the brief: an ID is "OP-" and a positive decimal number without leading zeros
// (the tests use only unmistakably valid and invalid IDs); a Key, when present, and a Body are
// lowercase hex SHA-256 (64 characters); PutOperation of a record with an empty Key stores none;
// the sequence's file content is not asserted, only its effect; temporary files are those whose
// name starts with ".tmp-" (the store's prefix) and none may remain in operations/ or the root.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

var opT0 = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

func opRec(id, key, body string) OperationRecord {
	return OperationRecord{ID: id, Kind: "createSlice", Target: "S-1", Key: key, Body: body, AcceptedAt: opT0}
}

func opPut(t *testing.T, s *Store, r OperationRecord, body []byte) {
	t.Helper()
	if err := s.PutOperation(r, body); err != nil {
		t.Fatalf("PutOperation(%s): %v", r.ID, err)
	}
}

func opNext(t *testing.T, s *Store) string {
	t.Helper()
	id, err := s.NextOperationID()
	if err != nil {
		t.Fatalf("NextOperationID: %v", err)
	}
	return id
}

func opEnd(t *testing.T, s *Store, id string, at time.Time) {
	t.Helper()
	ended, err := s.EndOperation(id, at, "succeeded", json.RawMessage(`{"ok":true}`), nil)
	if err != nil || !ended {
		t.Fatalf("EndOperation(%s) = %v, %v; want true, nil", id, ended, err)
	}
}

func opIDs(rs []OperationRecord) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

func opPath(dir, id, ext string) string { return filepath.Join(dir, "operations", id+ext) }

func opExists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestNextOperationIDCountsUpFromOne(t *testing.T) {
	s := open(t, t.TempDir())
	for i, want := range []string{"OP-1", "OP-2", "OP-3"} {
		if got := opNext(t, s); got != want {
			t.Errorf("call %d = %s, want %s", i+1, got, want)
		}
	}
}

func TestNextOperationIDContinuesAfterReopen(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	opNext(t, s)
	opNext(t, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = open(t, dir)
	if got := opNext(t, s); got != "OP-3" {
		t.Errorf("after reopen = %s, want OP-3", got)
	}
}

func TestAnIDGivenOutWithoutARecordIsNeverReissued(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	opNext(t, s) // OP-1, never recorded
	s = open(t, dir)
	if got := opNext(t, s); got != "OP-2" {
		t.Errorf("got %s, want OP-2", got)
	}
}

func TestNoOperationIDIsReissuedAfterDeleteOrRetention(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	for i := 1; i <= 3; i++ {
		id := opNext(t, s)
		opPut(t, s, opRec(id, "", sum([]byte(id))), []byte("x"))
	}
	if err := s.DeleteOperation("OP-3"); err != nil { // the highest, so a "max of records" rule would reuse it
		t.Fatal(err)
	}
	if got := opNext(t, s); got != "OP-4" {
		t.Errorf("after deleting the highest = %s, want OP-4", got)
	}
	opPut(t, s, opRec("OP-4", "", sum([]byte("4"))), nil)
	opEnd(t, s, "OP-4", opT0)
	opEnd(t, s, "OP-1", opT0)
	opEnd(t, s, "OP-2", opT0)
	if ids, err := s.RemoveOperationsEndedBefore(opT0.Add(time.Hour)); err != nil || len(ids) != 3 {
		t.Fatalf("RemoveOperationsEndedBefore = %v, %v; want 3 ids", ids, err)
	}
	if got := opNext(t, s); got != "OP-5" {
		t.Errorf("after every record is removed = %s, want OP-5", got)
	}
	s = open(t, dir)
	if got := opNext(t, s); got != "OP-6" {
		t.Errorf("after reopen with no records = %s, want OP-6", got)
	}
}

func TestNextOperationIDConcurrentCallsGiveDistinctIDs(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	const n = 40
	var wg sync.WaitGroup
	got := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], errs[i] = s.NextOperationID()
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for i, id := range got {
		if errs[i] != nil {
			t.Fatalf("call %d: %v", i, errs[i])
		}
		if seen[id] {
			t.Errorf("%s given out twice", id)
		}
		seen[id] = true
	}
	for i := 1; i <= n; i++ {
		if !seen["OP-"+itoa(i)] {
			t.Errorf("OP-%d missing: the IDs are not gap-free", i)
		}
	}
	// And durable: the next after a reopen follows them.
	s = open(t, dir)
	if id := opNext(t, s); id != "OP-"+itoa(n+1) {
		t.Errorf("after reopen = %s, want OP-%d", id, n+1)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestARecordAndItsBodyRoundTripAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	body := []byte("{\"goals\": [1,\n 2]}  \x00\xff")
	r := opRec("OP-1", sum([]byte("k")), sum(body))
	opPut(t, s, r, body)
	check := func(s *Store) {
		t.Helper()
		got, gotBody, err := s.Operation("OP-1")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, r) {
			t.Errorf("record = %+v, want %+v", got, r)
		}
		if !bytes.Equal(gotBody, body) {
			t.Errorf("body = %q, want %q", gotBody, body)
		}
	}
	check(s)
	check(open(t, dir))
}

func TestAnEmptyBodyIsAnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	opPut(t, s, opRec("OP-1", "", sum(nil)), nil)
	b, err := os.ReadFile(opPath(dir, "OP-1", ".body"))
	if err != nil || len(b) != 0 {
		t.Fatalf("body file = %q, %v; want an existing empty file", b, err)
	}
	_, got, err := s.Operation("OP-1")
	if err != nil || len(got) != 0 {
		t.Errorf("Operation body = %q, %v; want empty", got, err)
	}
}

func TestTheRecordFileHasExactlyTheContractedFieldNames(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	key, body := sum([]byte("k")), sum([]byte("b"))
	opPut(t, s, opRec("OP-1", key, body), []byte("b"))
	read := func() map[string]any {
		t.Helper()
		b, err := os.ReadFile(opPath(dir, "OP-1", ".json"))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("record file is not JSON: %v", err)
		}
		return m
	}
	m := read()
	want := map[string]any{"id": "OP-1", "kind": "createSlice", "target": "S-1", "key": key, "body": body,
		"accepted_at": "2026-11-20T10:00:00Z"}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("open record = %v, want %v (optional fields absent, not null)", m, want)
	}
	opEnd(t, s, "OP-1", opT0.Add(time.Minute))
	m = read()
	want["ended_at"] = "2026-11-20T10:01:00Z"
	want["status"] = "succeeded"
	want["result"] = map[string]any{"ok": true}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("ended record = %v, want %v", m, want)
	}
}

func TestARecordWithoutAKeyOmitsIt(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	opPut(t, s, opRec("OP-1", "", sum([]byte("b"))), []byte("b"))
	b, _ := os.ReadFile(opPath(dir, "OP-1", ".json"))
	if strings.Contains(string(b), `"key"`) {
		t.Errorf("record file holds a key field: %s", b)
	}
}

func TestOperationByKeyFindsTheRecordHoldingTheKeyHash(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	k1, k2 := sum([]byte("k1")), sum([]byte("k2"))
	opPut(t, s, opRec("OP-1", k1, sum([]byte("a"))), []byte("a"))
	opPut(t, s, opRec("OP-2", "", sum([]byte("b"))), []byte("b"))
	opPut(t, s, opRec("OP-3", k2, sum([]byte("c"))), []byte("c"))
	for _, tc := range []struct{ key, id string }{{k1, "OP-1"}, {k2, "OP-3"}} {
		for _, st := range []*Store{s, open(t, dir)} {
			r, ok, err := st.OperationByKey(tc.key)
			if err != nil || !ok || r.ID != tc.id {
				t.Errorf("OperationByKey(%s) = %v, %v, %v; want %s", tc.key[:8], r.ID, ok, err, tc.id)
			}
		}
	}
	for _, key := range []string{sum([]byte("other")), "", "not-a-hash"} {
		if r, ok, err := s.OperationByKey(key); ok || err != nil {
			t.Errorf("OperationByKey(%q) = %v, %v, %v; want not found, no error (records without a key never match)", key, r.ID, ok, err)
		}
	}
}

func TestOperationByKeyFindsAnEndedRecordAndNotARemovedOne(t *testing.T) {
	s := open(t, t.TempDir())
	k := sum([]byte("k"))
	opPut(t, s, opRec("OP-1", k, sum([]byte("a"))), []byte("a"))
	opEnd(t, s, "OP-1", opT0)
	if r, ok, err := s.OperationByKey(k); err != nil || !ok || r.Status != "succeeded" {
		t.Errorf("ended: %+v, %v, %v; want found with its status", r, ok, err)
	}
	if err := s.DeleteOperation("OP-1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.OperationByKey(k); ok || err != nil {
		t.Errorf("after delete: ok=%v err=%v; want not found", ok, err)
	}
}

func TestOperationOfAnUnknownIDIsNotFound(t *testing.T) {
	s := open(t, t.TempDir())
	if _, _, err := s.Operation("OP-7"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestEndingWritesOnceAndRemovesTheBody(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	opPut(t, s, opRec("OP-1", "", sum([]byte("a"))), []byte("a"))
	end := opT0.Add(5 * time.Second)
	result := json.RawMessage(`{"slice":"S-1"}`)
	ended, err := s.EndOperation("OP-1", end, "succeeded", result, nil)
	if err != nil || !ended {
		t.Fatalf("first end = %v, %v; want true", ended, err)
	}
	if opExists(opPath(dir, "OP-1", ".body")) {
		t.Error("the body file is still there after the end")
	}
	before := snapshot(t, dir)
	for _, st := range []string{"failed", "cancelled", "succeeded"} {
		ended, err = s.EndOperation("OP-1", end.Add(time.Hour), st, nil, json.RawMessage(`{"code":"X"}`))
		if err != nil || ended {
			t.Errorf("second end (%s) = %v, %v; want false, nil", st, ended, err)
		}
	}
	if after := snapshot(t, dir); !reflect.DeepEqual(before, after) {
		t.Error("a second end changed the store")
	}
	for _, st := range []*Store{s, open(t, dir)} {
		r, body, err := st.Operation("OP-1")
		if err != nil {
			t.Fatal(err)
		}
		if r.EndedAt == nil || !r.EndedAt.Equal(end) || r.Status != "succeeded" ||
			string(r.Result) != `{"slice":"S-1"}` || len(r.Error) != 0 {
			t.Errorf("record = %+v, want the first end", r)
		}
		if body != nil {
			t.Errorf("body = %q, want nil once ended", body)
		}
	}
}

func TestEndingWithAProblemKeepsTheProblemAndNoResult(t *testing.T) {
	s := open(t, t.TempDir())
	opPut(t, s, opRec("OP-1", "", sum([]byte("a"))), []byte("a"))
	problem := json.RawMessage(`{"code":"RunNotStarted","detail":"d"}`)
	if ended, err := s.EndOperation("OP-1", opT0, "failed", nil, problem); err != nil || !ended {
		t.Fatalf("end = %v, %v", ended, err)
	}
	r, _, _ := s.Operation("OP-1")
	if r.Status != "failed" || r.EndedAt == nil || string(r.Error) != string(problem) || len(r.Result) != 0 {
		t.Errorf("record = %+v", r)
	}
	opPut(t, s, opRec("OP-2", "", sum([]byte("b"))), nil)
	if ended, err := s.EndOperation("OP-2", opT0, "cancelled", nil, nil); err != nil || !ended {
		t.Fatalf("cancel = %v, %v", ended, err)
	}
	if r, _, _ := s.Operation("OP-2"); r.Status != "cancelled" || r.EndedAt == nil {
		t.Errorf("cancelled record = %+v", r)
	}
}

func TestEndingAnUnknownOperationIsNotFound(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	ended, err := s.EndOperation("OP-9", opT0, "succeeded", nil, nil)
	if !errors.Is(err, ErrNotFound) || ended {
		t.Errorf("= %v, %v; want false, ErrNotFound", ended, err)
	}
	if opExists(opPath(dir, "OP-9", ".json")) {
		t.Error("ending an unknown operation created a record")
	}
}

func TestOpenOperationsAreByAscendingNumberAndExcludeEndedOnes(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	for _, id := range []string{"OP-10", "OP-2", "OP-1", "OP-11", "OP-3"} {
		opPut(t, s, opRec(id, "", sum([]byte(id))), []byte(id))
	}
	opEnd(t, s, "OP-3", opT0)
	want := []string{"OP-1", "OP-2", "OP-10", "OP-11"}
	for _, st := range []*Store{s, open(t, dir)} {
		got, err := st.OpenOperations()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(opIDs(got), want) {
			t.Errorf("OpenOperations = %v, want %v", opIDs(got), want)
		}
	}
}

func TestOpenOperationsOfAnEmptyStoreIsEmpty(t *testing.T) {
	got, err := open(t, t.TempDir()).OpenOperations()
	if err != nil || len(got) != 0 {
		t.Errorf("= %v, %v; want none", got, err)
	}
}

func TestRetentionRemovesOnlyOperationsEndedBeforeTheTime(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	cut := opT0.Add(time.Hour)
	ends := map[string]*time.Time{
		"OP-1": ptr(cut.Add(-time.Second)), // removed
		"OP-2": ptr(cut),                   // exactly at the cut: kept
		"OP-3": ptr(cut.Add(time.Second)),  // kept
		"OP-4": nil,                        // open: kept
		"OP-5": ptr(cut.Add(-time.Hour)),   // removed
	}
	for id, at := range ends {
		opPut(t, s, opRec(id, "", sum([]byte(id))), []byte(id))
		if at != nil {
			opEnd(t, s, id, *at)
		}
	}
	got, err := s.RemoveOperationsEndedBefore(cut)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if want := []string{"OP-1", "OP-5"}; !reflect.DeepEqual(got, want) {
		t.Errorf("removed = %v, want %v", got, want)
	}
	for _, id := range []string{"OP-1", "OP-5"} {
		if _, _, err := s.Operation(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", id, err)
		}
		for _, ext := range []string{".json", ".body"} {
			if opExists(opPath(dir, id, ext)) {
				t.Errorf("%s%s is still on disk", id, ext)
			}
		}
	}
	for _, id := range []string{"OP-2", "OP-3", "OP-4"} {
		if _, _, err := s.Operation(id); err != nil {
			t.Errorf("%s was removed: %v", id, err)
		}
	}
	if again, err := s.RemoveOperationsEndedBefore(cut); err != nil || len(again) != 0 {
		t.Errorf("second call = %v, %v; want none", again, err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestDeleteOperationRemovesRecordAndBodyAndNoneIsNoError(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	opPut(t, s, opRec("OP-1", "", sum([]byte("a"))), []byte("a"))
	opPut(t, s, opRec("OP-2", "", sum([]byte("b"))), []byte("b"))
	if err := s.DeleteOperation("OP-1"); err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{".json", ".body"} {
		if opExists(opPath(dir, "OP-1", ext)) {
			t.Errorf("OP-1%s is still on disk", ext)
		}
	}
	if _, _, err := s.Operation("OP-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if _, _, err := s.Operation("OP-2"); err != nil {
		t.Errorf("OP-2: %v", err)
	}
	if err := s.DeleteOperation("OP-1"); err != nil {
		t.Errorf("deleting again: %v", err)
	}
	if err := s.DeleteOperation("OP-99"); err != nil {
		t.Errorf("deleting an unknown one: %v", err)
	}
	// An ended record (no body file) is removed as well.
	opEnd(t, s, "OP-2", opT0)
	if err := s.DeleteOperation("OP-2"); err != nil {
		t.Fatal(err)
	}
	if opExists(opPath(dir, "OP-2", ".json")) {
		t.Error("the ended record is still on disk")
	}
}

func TestOpenRemovesABodyWithNoRecordAndKeepsOneWithARecord(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	opPut(t, s, opRec("OP-1", "", sum([]byte("a"))), []byte("a"))
	opNext(t, s)
	// A stop between the two writes: a body, no record.
	orphan := opPath(dir, "OP-2", ".body")
	if err := os.WriteFile(orphan, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	s = open(t, dir)
	if opExists(orphan) {
		t.Error("the body with no record is still there after Open")
	}
	if _, body, err := s.Operation("OP-1"); err != nil || string(body) != "a" {
		t.Errorf("OP-1 = %q, %v; its body must stay", body, err)
	}
	if got := opNext(t, s); got != "OP-3" {
		t.Errorf("next = %s, want OP-3 (the orphan's ID was given out)", got)
	}
}

func TestOpenRemovesATemporaryFileOfAnInterruptedOperationWrite(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	opPut(t, s, opRec("OP-1", "", sum([]byte("a"))), []byte("a"))
	tmp := filepath.Join(dir, "operations", tmpPrefix+"123")
	if err := os.WriteFile(tmp, []byte("part"), 0o600); err != nil {
		t.Fatal(err)
	}
	open(t, dir)
	if opExists(tmp) {
		t.Error("a temporary file in operations/ survived Open")
	}
	if _, _, err := open(t, dir).Operation("OP-1"); err != nil {
		t.Errorf("OP-1: %v", err)
	}
}

func TestInvalidInputChangesNothingOnDisk(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	good := sum([]byte("g"))
	opPut(t, s, opRec("OP-1", good, good), []byte("keep"))
	before := snapshot(t, dir)

	ended := opRec("OP-3", "", good)
	at := opT0
	ended.EndedAt, ended.Status = &at, "succeeded"
	upper := strings.ToUpper(sum([]byte("u")))
	cases := map[string]OperationRecord{
		"empty ID":          opRec("", "", good),
		"no number":         opRec("OP-", "", good),
		"not a number":      opRec("OP-x", "", good),
		"lowercase prefix":  opRec("op-3", "", good),
		"a slice ID":        opRec("S-3", "", good),
		"path in the ID":    opRec("../OP-3", "", good),
		"short key":         opRec("OP-3", "abc", good),
		"uppercase key":     opRec("OP-3", upper, good),
		"non-hex key":       opRec("OP-3", strings.Repeat("g", 64), good),
		"empty body hash":   opRec("OP-3", "", ""),
		"short body hash":   opRec("OP-3", "", "abc"),
		"uppercase body":    opRec("OP-3", "", upper),
		"non-hex body hash": opRec("OP-3", "", strings.Repeat("z", 64)),
		"ended record":      ended,
		"an existing ID":    opRec("OP-1", "", sum([]byte("other"))),
	}
	for name, r := range cases {
		if err := s.PutOperation(r, []byte("new")); err == nil {
			t.Errorf("%s: PutOperation succeeded, want an error", name)
		}
		if after := snapshot(t, dir); !reflect.DeepEqual(before, after) {
			t.Errorf("%s: the store changed", name)
		}
	}
	if r, body, err := s.Operation("OP-1"); err != nil || string(body) != "keep" || r.Body != good {
		t.Errorf("the existing OP-1 = %+v, %q, %v; want unchanged", r, body, err)
	}
}

func TestOperationLookupsRefuseAnInvalidIDWithoutTouchingDisk(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	opPut(t, s, opRec("OP-1", "", sum([]byte("a"))), []byte("a"))
	before := snapshot(t, dir)
	for _, id := range []string{"", "../operations/OP-1", "OP-1/../OP-1", "OP-x"} {
		if _, _, err := s.Operation(id); err == nil {
			t.Errorf("Operation(%q) succeeded", id)
		}
		if _, err := s.EndOperation(id, opT0, "succeeded", nil, nil); err == nil {
			t.Errorf("EndOperation(%q) succeeded", id)
		}
		if err := s.DeleteOperation(id); err == nil && id != "" {
			// An invalid ID names no record, so deleting it may be a no-op; it must never reach another file.
			_ = err
		}
	}
	if after := snapshot(t, dir); !reflect.DeepEqual(before, after) {
		t.Error("a lookup with an invalid ID changed the store")
	}
}

func TestWritesLeaveNoTemporaryFileAndKeepTheStoresFileModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store") // a folder Open creates itself
	s := open(t, dir)
	if err := s.PutSliceSequence([]byte(`{"last":1}`)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		id := opNext(t, s)
		opPut(t, s, opRec(id, "", sum([]byte(id))), []byte(id))
	}
	opEnd(t, s, "OP-1", opT0)
	if err := s.DeleteOperation("OP-2"); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(d.Name(), tmpPrefix) {
			t.Errorf("temporary file left behind: %s", p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	mode := func(p string) fs.FileMode {
		t.Helper()
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		return fi.Mode()
	}
	want := mode(filepath.Join(dir, sequenceFile))
	for _, p := range []string{opPath(dir, "OP-1", ".json"), opPath(dir, "OP-3", ".json"),
		opPath(dir, "OP-3", ".body"), filepath.Join(dir, "operation-sequence.json")} {
		if got := mode(p); got != want || !got.IsRegular() || got.Perm()&0o077 != 0 {
			t.Errorf("%s mode = %v, want %v (owner only, as the store's other files)", p, got, want)
		}
	}
	if m := mode(dir); m.Perm() != 0o700 {
		t.Errorf("store root mode = %v, want 700", m.Perm())
	}
}

func TestBodiesAreKeptApartPerOperation(t *testing.T) {
	s := open(t, t.TempDir())
	opPut(t, s, opRec("OP-1", "", sum([]byte("a"))), []byte("alpha"))
	opPut(t, s, opRec("OP-2", "", sum([]byte("b"))), []byte("beta"))
	opEnd(t, s, "OP-1", opT0)
	if _, b, _ := s.Operation("OP-2"); string(b) != "beta" {
		t.Errorf("OP-2 body = %q after ending OP-1, want beta", b)
	}
}

func TestOperationsAreConcurrencySafe(t *testing.T) {
	s := open(t, t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := s.NextOperationID()
			if err != nil {
				t.Error(err)
				return
			}
			if err := s.PutOperation(opRec(id, "", sum([]byte(id))), []byte(id)); err != nil {
				t.Error(err)
			}
			_, _ = s.OpenOperations()
		}()
	}
	wg.Wait()
	got, err := s.OpenOperations()
	if err != nil || len(got) != 20 {
		t.Errorf("OpenOperations = %d records, %v; want 20", len(got), err)
	}
}
