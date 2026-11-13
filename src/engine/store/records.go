package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/agentic-platform/slice"
)

// Slice records: slices/<slice>/slice.json holds the slice's record and slice-sequence.json, at
// the store's root, the slice ID sequence, each replaced atomically (a temporary file synced,
// renamed over the old one, then the folder synced). The store keeps their bytes; the slices
// package owns their format. EraseSlice removes a slice's record with the rest of its folder.
const (
	sliceFile    = "slice.json"
	sequenceFile = "slice-sequence.json"
	tmpPrefix    = ".tmp-"
)

// SliceRecords returns the record of every slice that has one, by slice ID. A record that cannot
// be read is an error naming the slice.
func (s *Store) SliceRecords() (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.slices)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, d := range entries {
		sl, err := s.ids.ParseID(d.Name())
		if err != nil {
			return nil, fmt.Errorf("store: unexpected entry in %s: %w", s.slices, err)
		}
		b, err := os.ReadFile(filepath.Join(s.slices, sl.String(), sliceFile))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("store: the record of slice %s: %w", sl, err)
		}
		out[sl.String()] = b
	}
	return out, nil
}

// PutSliceRecord replaces the slice's record and returns only once it is durable.
func (s *Store) PutSliceRecord(sliceID string, record []byte) error {
	sl, err := s.ids.ParseID(sliceID)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.sliceDir(sl)
	if err != nil {
		return err
	}
	return writeAtomic(dir, sliceFile, record)
}

// DeleteSliceRecord removes the slice's record; a missing record or folder is no error.
func (s *Store) DeleteSliceRecord(sliceID string) error {
	sl, err := s.ids.ParseID(sliceID)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.slices, sl.String())
	if err := os.Remove(filepath.Join(dir, sliceFile)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return syncDir(dir)
}

// SliceSequence returns the stored slice ID sequence, or nil when none is stored.
func (s *Store) SliceSequence() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(s.root, sequenceFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: the slice ID sequence: %w", err)
	}
	return b, nil
}

// PutSliceSequence replaces the slice ID sequence and returns only once it is durable.
func (s *Store) PutSliceSequence(seq []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeAtomic(s.root, sequenceFile, seq)
}

// sliceDir returns the slice's folder, creating it (durably) if needed. The caller holds s.mu.
func (s *Store) sliceDir(sl slice.ID) (string, error) {
	dir := filepath.Join(s.slices, sl.String())
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return "", err
		}
		if err := syncDir(s.slices); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	return dir, nil
}

// writeAtomic replaces dir/name with data: a temporary file in dir, synced, renamed over name,
// then dir synced. A crash leaves the old file or the new one, never a part of either; Open
// removes leftover temporary files.
func writeAtomic(dir, name string, data []byte) error {
	tmp, err := os.CreateTemp(dir, tmpPrefix)
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		return err
	}
	return syncDir(dir)
}

// runSequenceFile, at the store's root, holds the number of the last run ID given out, replaced
// atomically like the slice ID sequence. It outlives every erasure, so a run ID is never reissued.
const runSequenceFile = "run-sequence.json"

// RunEntry is one run the store holds and the slice it belongs to.
type RunEntry struct{ Run, Slice string }

// Runs returns every run the store holds, by run number, lowest first.
func (s *Store) Runs() []RunEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RunEntry, 0, len(s.runSlice))
	for run, sl := range s.runSlice {
		out = append(out, RunEntry{Run: run.String(), Slice: sl.String()})
	}
	sort.Slice(out, func(i, j int) bool { return runNumber(out[i].Run) < runNumber(out[j].Run) })
	return out
}

// NextRunID gives out the next run ID, `RUN-<n>`, and returns it only once the sequence that gave
// it is durable: one more than both the last ID given out and the highest run the store holds. An
// ID is given out once, even when the run it was for is never recorded or is erased later.
func (s *Store) NextRunID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var seq struct {
		Last int64 `json:"last"`
	}
	b, err := os.ReadFile(filepath.Join(s.root, runSequenceFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return "", fmt.Errorf("store: the run ID sequence: %w", err)
	default:
		if err := json.Unmarshal(b, &seq); err != nil || seq.Last < 0 {
			return "", fmt.Errorf("store: the run ID sequence is unreadable: %v", err)
		}
	}
	for run := range s.runSlice {
		seq.Last = max(seq.Last, runNumber(run.String()))
	}
	seq.Last++
	id, err := s.ids.ParseRunID("RUN-" + strconv.FormatInt(seq.Last, 10))
	if err != nil {
		return "", fmt.Errorf("store: %w", err)
	}
	b, _ = json.Marshal(seq) // a struct of an int cannot fail
	if err := writeAtomic(s.root, runSequenceFile, b); err != nil {
		return "", err
	}
	return id.String(), nil
}

// runNumber is the number of a run ID the contract's rule accepted, `RUN-<n>`.
func runNumber(id string) int64 {
	n, _ := strconv.ParseInt(strings.TrimPrefix(id, "RUN-"), 10, 64)
	return n
}
