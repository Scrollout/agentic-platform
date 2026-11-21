package store

// T064 (F1 of the T027 review): a failed PutOperation leaves nothing behind, also when the record's
// write fails AFTER its rename (the folder sync fails). Data model "Accepting": a failed write is
// 503 and what was written is removed; an ID is never reissued (FR-011).
//
// Interface fixed by the brief: `syncDir` is a package variable, `var syncDir = func(dir string)
// error {...}`, that an internal test may replace. Until the code author makes it so, this file
// does not compile: that is the expected "seen failing".
//
// Edge cases checked: the folder sync failing for the remove too; a stale temporary file; a
// failure that hits only the operations folder; the failed ID is not reissued, also after reopen;
// the guards (failing body write, working sync). Not tested: the remove itself failing.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	wfKey  = strings.Repeat("a", 64)
	wfBody = strings.Repeat("b", 64)
)

// failSync replaces syncDir for the test: it fails for the operations folder of dir when when()
// holds, and passes every other call through to the real one.
func failSync(t *testing.T, dir string, when func() bool) {
	t.Helper()
	real := syncDir
	ops := filepath.Join(dir, operationsFolder)
	syncDir = func(d string) error {
		if filepath.Clean(d) == ops && when() {
			return errors.New("injected folder sync failure")
		}
		return real(d)
	}
	t.Cleanup(func() { syncDir = real })
}

func wfNothingLeft(t *testing.T, s *Store, dir, id, key string) {
	t.Helper()
	for _, ext := range []string{recordSuffix, bodySuffix} {
		if opExists(opPath(dir, id, ext)) {
			t.Errorf("%s%s is left after the failed PutOperation", id, ext)
		}
	}
	if _, _, err := s.Operation(id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Operation(%s) error = %v, want ErrNotFound", id, err)
	}
	if _, found, err := s.OperationByKey(key); err != nil || found {
		t.Errorf("OperationByKey = found %v, err %v; want not found", found, err)
	}
	open, err := s.OpenOperations()
	if err != nil || len(open) != 0 {
		t.Errorf("OpenOperations = %v, %v; want none", opIDs(open), err)
	}
	tmps, _ := filepath.Glob(filepath.Join(dir, operationsFolder, tmpPrefix+"*"))
	if len(tmps) != 0 {
		t.Errorf("temporary files left: %v", tmps)
	}
}

func TestPutOperationWhoseRecordFolderSyncFailsLeavesNothingAndNeverReissuesTheID(t *testing.T) {
	// removeSyncFails: the folder sync fails for the cleanup's remove too.
	for name, removeSyncFails := range map[string]bool{"cleanup sync works": false, "cleanup sync fails too": true} {
		t.Run(name, func(t *testing.T) {
			origSyncDir := syncDir
			dir := t.TempDir()
			s := open(t, dir)
			id := opNext(t, s)
			failed := false
			failSync(t, dir, func() bool {
				// The record's write: its file exists after the rename. With removeSyncFails the
				// folder sync keeps failing once it has failed, so the cleanup's sync fails too.
				if opExists(opPath(dir, id, recordSuffix)) || (removeSyncFails && failed) {
					failed = true
					return true
				}
				return false
			})
			if err := s.PutOperation(opRec(id, wfKey, wfBody), []byte("body")); err == nil {
				t.Fatal("PutOperation succeeded though the record's folder sync failed")
			}
			wfNothingLeft(t, s, dir, id, wfKey)
			if next := opNext(t, s); next == id {
				t.Errorf("the failed ID %s was reissued", id)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			syncDir = origSyncDir
			s = open(t, dir)
			wfNothingLeft(t, s, dir, id, wfKey)
			if next := opNext(t, s); next == id || next == "OP-2" {
				t.Errorf("after reopen next ID = %s; %s was given out and OP-2 after it", next, id)
			}
		})
	}
}

func TestPutOperationWhoseBodyWriteFailsLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	id := opNext(t, s)
	failSync(t, dir, func() bool { return !opExists(opPath(dir, id, recordSuffix)) })
	if err := s.PutOperation(opRec(id, wfKey, wfBody), []byte("body")); err == nil {
		t.Fatal("PutOperation succeeded though the body's folder sync failed")
	}
	wfNothingLeft(t, s, dir, id, wfKey)
	if next := opNext(t, s); next == id {
		t.Errorf("the failed ID %s was reissued", id)
	}
}

func TestPutOperationWithAWorkingSyncReadsBack(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	id := opNext(t, s)
	r := opRec(id, wfKey, wfBody)
	if err := s.PutOperation(r, []byte("body")); err != nil {
		t.Fatal(err)
	}
	got, body, err := s.Operation(id)
	if err != nil || got.ID != id || got.Key != wfKey || string(body) != "body" {
		t.Errorf("Operation = %+v, %q, %v; want the record and its body", got, body, err)
	}
	if _, err := os.Stat(opPath(dir, id, bodySuffix)); err != nil {
		t.Error(err)
	}
}
