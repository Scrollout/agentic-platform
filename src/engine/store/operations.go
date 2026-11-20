package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Operation records (ADR 0072, data model "Operation record"): operations/<id>.json holds an
// operation's record and operations/<id>.body its request body until the operation has ended,
// each replaced atomically, the body first. operation-sequence.json, at the store's root, holds
// the number of the last operation ID given out; it outlives every record, so an ID is never
// reissued.
const (
	operationsFolder      = "operations"
	operationSequenceFile = "operation-sequence.json"
	recordSuffix          = ".json"
	bodySuffix            = ".body"
)

var (
	operationIDRE = regexp.MustCompile(`^OP-[1-9][0-9]*$`)
	sha256RE      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// OperationRecord is what the store keeps of one operation: its ID (`OP-<n>`), Kind (the contract
// operation that accepted it), Target (the slice or run it acts on), Key (the Idempotency-Key's
// hash, "" for none) and Body (the request's hash), both lowercase hex SHA-256, and when it was
// accepted; once it has ended, when, its status and its result or problem.
type OperationRecord struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Target     string          `json:"target"`
	Key        string          `json:"key,omitempty"`
	Body       string          `json:"body"`
	AcceptedAt time.Time       `json:"accepted_at"`
	EndedAt    *time.Time      `json:"ended_at,omitempty"`
	Status     string          `json:"status,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      json.RawMessage `json:"error,omitempty"`
}

// NextOperationID gives out the next operation ID, `OP-<n>`, and returns it only once the sequence
// that gave it is durable: one more than both the last ID given out and the highest record the
// store holds. An ID is given out once, even when no record is written for it or its record is
// removed later.
func (s *Store) NextOperationID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var seq struct {
		Last int64 `json:"last"`
	}
	b, err := os.ReadFile(filepath.Join(s.root, operationSequenceFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return "", fmt.Errorf("store: the operation ID sequence: %w", err)
	default:
		if err := json.Unmarshal(b, &seq); err != nil || seq.Last < 0 {
			return "", fmt.Errorf("store: the operation ID sequence is unreadable: %v", err)
		}
	}
	ids, err := s.operationIDs(recordSuffix)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		seq.Last = max(seq.Last, operationNumber(id))
	}
	seq.Last++
	b, _ = json.Marshal(seq) // a struct of an int cannot fail
	if err := writeAtomic(s.root, operationSequenceFile, b); err != nil {
		return "", err
	}
	return "OP-" + strconv.FormatInt(seq.Last, 10), nil
}

// PutOperation keeps a new operation's record and its request body (empty when it had none), the
// body first, and returns only once both are durable; when the record cannot be written, the body
// is removed again. An ID that is no `OP-<n>` or that a record holds, a Key or Body that is no
// lowercase hex SHA-256, an empty Kind or Target, or a record that has ended is an error and
// changes nothing.
func (s *Store) PutOperation(r OperationRecord, body []byte) error {
	if err := parseOperationID(r.ID); err != nil {
		return err
	}
	switch {
	case r.Key != "" && !sha256RE.MatchString(r.Key):
		return fmt.Errorf("store: operation %s: its key is no lowercase hex SHA-256", r.ID)
	case !sha256RE.MatchString(r.Body):
		return fmt.Errorf("store: operation %s: its body hash is no lowercase hex SHA-256", r.ID)
	case r.Kind == "" || r.Target == "":
		return fmt.Errorf("store: operation %s needs a kind and a target", r.ID)
	case r.EndedAt != nil || r.Status != "" || len(r.Result) > 0 || len(r.Error) > 0:
		return fmt.Errorf("store: operation %s: a new record has not ended", r.ID)
	}
	rec, err := json.Marshal(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.operationsDir()
	if err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(dir, r.ID+recordSuffix)); err == nil {
		return fmt.Errorf("store: operation %s is recorded already", r.ID)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// A write can fail after its file is in place (the folder sync), so a failure removes every
	// file this call may have written: a 503 leaves nothing accepted (T064).
	if err := writeAtomic(dir, r.ID+bodySuffix, body); err != nil {
		_ = removeFiles(dir, r.ID+bodySuffix)
		return err
	}
	if err := writeAtomic(dir, r.ID+recordSuffix, rec); err != nil {
		_ = removeFiles(dir, r.ID+recordSuffix, r.ID+bodySuffix)
		return err
	}
	return nil
}

// Operation returns an operation's record and, while it has not ended, its request body (nil once
// it has); ErrNotFound when none is kept. An ID that is no `OP-<n>` is an error.
func (s *Store) Operation(id string) (OperationRecord, []byte, error) {
	if err := parseOperationID(id); err != nil {
		return OperationRecord{}, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.readOperation(id)
	if err != nil || r.EndedAt != nil {
		return r, nil, err
	}
	body, err := s.readOperationFile(id + bodySuffix)
	if err != nil {
		return OperationRecord{}, nil, fmt.Errorf("store: the body of operation %s: %w", id, err)
	}
	return r, body, nil
}

// OperationByKey returns the record that holds the key hash, the highest-numbered when several
// do; found is false when none does. An empty key, or one that is no SHA-256, matches none.
func (s *Store) OperationByKey(key string) (OperationRecord, bool, error) {
	if !sha256RE.MatchString(key) {
		return OperationRecord{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readOperations()
	if err != nil {
		return OperationRecord{}, false, err
	}
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Key == key {
			return all[i], true, nil
		}
	}
	return OperationRecord{}, false, nil
}

// OpenOperations returns the records of the operations that have not ended, by number, lowest
// first.
func (s *Store) OpenOperations() ([]OperationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readOperations()
	if err != nil {
		return nil, err
	}
	open := []OperationRecord{}
	for _, r := range all {
		if r.EndedAt == nil {
			open = append(open, r)
		}
	}
	return open, nil
}

// EndOperation writes an operation's end once: at, its status and its result or problem, then
// removes its body. ended is false, and nothing changes, when the record has ended already; an
// unknown operation is ErrNotFound. An empty status, or a result or problem that is not one JSON
// value, is an error.
func (s *Store) EndOperation(id string, at time.Time, status string, result, problem json.RawMessage) (bool, error) {
	if err := parseOperationID(id); err != nil {
		return false, err
	}
	if status == "" || (len(result) > 0 && !json.Valid(result)) || (len(problem) > 0 && !json.Valid(problem)) {
		return false, fmt.Errorf("store: the end of operation %s needs a status and JSON values", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.readOperation(id)
	if err != nil {
		return false, err
	}
	if r.EndedAt != nil {
		return false, nil
	}
	r.EndedAt, r.Status, r.Result, r.Error = &at, status, result, problem
	b, err := json.Marshal(r)
	if err != nil {
		return false, err
	}
	dir := filepath.Join(s.root, operationsFolder)
	if err := writeAtomic(dir, id+recordSuffix, b); err != nil {
		return false, err
	}
	// The end is durable: a body left by a failure here is removed at the next Open.
	_ = removeFiles(dir, id+bodySuffix)
	return true, nil
}

// RemoveOperationsEndedBefore removes, with their bodies, the records of the operations that
// ended before t, and returns their IDs. An operation that ended at t, or has not ended, stays.
func (s *Store) RemoveOperationsEndedBefore(t time.Time) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readOperations()
	if err != nil {
		return nil, err
	}
	removed := []string{}
	dir := filepath.Join(s.root, operationsFolder)
	for _, r := range all {
		if r.EndedAt == nil || !r.EndedAt.Before(t) {
			continue
		}
		if err := removeFiles(dir, r.ID+recordSuffix, r.ID+bodySuffix); err != nil {
			return removed, err
		}
		removed = append(removed, r.ID)
	}
	return removed, nil
}

// DeleteOperation removes an operation's record and body; none is no error. An ID that is no
// `OP-<n>` is an error.
func (s *Store) DeleteOperation(id string) error {
	if err := parseOperationID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return removeFiles(filepath.Join(s.root, operationsFolder), id+recordSuffix, id+bodySuffix)
}

// openOperations prepares the operations folder at Open: it removes leftover temporary files and
// every body whose record is missing (a stop between the two writes) or has ended.
func (s *Store) openOperations() error {
	dir, err := s.operationsDir()
	if err != nil {
		return err
	}
	tmps, _ := filepath.Glob(filepath.Join(dir, tmpPrefix+"*"))
	for _, t := range tmps {
		if err := os.Remove(t); err != nil {
			return err
		}
	}
	bodies, err := s.operationIDs(bodySuffix)
	if err != nil {
		return err
	}
	for _, id := range bodies {
		r, err := s.readOperation(id)
		switch {
		case errors.Is(err, ErrNotFound), err == nil && r.EndedAt != nil:
			if err := removeFiles(dir, id+bodySuffix); err != nil {
				return err
			}
		case err != nil:
			return err
		}
	}
	return nil
}

// operationsDir returns the operations folder, creating it (durably) if needed. The caller holds
// s.mu, or is Open.
func (s *Store) operationsDir() (string, error) {
	dir := filepath.Join(s.root, operationsFolder)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return "", err
		}
		if err := syncDir(s.root); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	return dir, nil
}

// operationIDs returns the IDs of the files in the operations folder with the suffix, by number,
// lowest first. A name that is no operation's is skipped. The caller holds s.mu, or is Open.
func (s *Store) operationIDs(suffix string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, operationsFolder))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), suffix); ok && e.Type().IsRegular() && operationIDRE.MatchString(id) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return operationNumber(ids[i]) < operationNumber(ids[j]) })
	return ids, nil
}

// readOperations reads every record, by number, lowest first. The caller holds s.mu.
func (s *Store) readOperations() ([]OperationRecord, error) {
	ids, err := s.operationIDs(recordSuffix)
	if err != nil {
		return nil, err
	}
	out := make([]OperationRecord, 0, len(ids))
	for _, id := range ids {
		r, err := s.readOperation(id)
		if errors.Is(err, ErrNotFound) { // removed meanwhile
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// readOperation reads one record; ErrNotFound when there is none. The caller holds s.mu.
func (s *Store) readOperation(id string) (OperationRecord, error) {
	b, err := s.readOperationFile(id + recordSuffix)
	if errors.Is(err, fs.ErrNotExist) {
		return OperationRecord{}, fmt.Errorf("operation %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return OperationRecord{}, err
	}
	var r OperationRecord
	if err := json.Unmarshal(b, &r); err != nil || r.ID != id {
		return OperationRecord{}, fmt.Errorf("store: the record of operation %s is unreadable: %v", id, err)
	}
	return r, nil
}

// readOperationFile reads a file of the operations folder through a root opened on it, so the
// name cannot reach outside the folder.
func (s *Store) readOperationFile(name string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Join(s.root, operationsFolder))
	if err != nil {
		return nil, err
	}
	b, err := root.ReadFile(name)
	if cerr := root.Close(); err == nil && cerr != nil {
		return nil, cerr
	}
	return b, err
}

// removeFiles removes the named files of dir, a missing one being no error, then syncs dir.
func removeFiles(dir string, names ...string) error {
	for _, n := range names {
		if err := os.Remove(filepath.Join(dir, n)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return syncDir(dir)
}

func parseOperationID(id string) error {
	if !operationIDRE.MatchString(id) {
		return fmt.Errorf("store: %q is not an operation ID", id)
	}
	return nil
}

// operationNumber is the number of an operation ID the rule accepted, `OP-<n>`.
func operationNumber(id string) int64 {
	n, _ := strconv.ParseInt(strings.TrimPrefix(id, "OP-"), 10, 64)
	return n
}
