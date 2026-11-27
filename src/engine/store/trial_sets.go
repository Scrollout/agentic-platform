package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Trial-set records (feature 012, data model "Trial set"): slices/<slice>/trial-sets/<id>.json
// holds one set's record, replaced atomically; the store keeps its bytes and the engine API owns
// its format. trial-set-sequence.json, at the store's root, holds the number of the last set ID
// given out; it outlives every erasure, so a set ID is never given again. EraseSlice removes a
// slice's sets with the rest of its folder.
const (
	trialSetsFolder      = "trial-sets"
	trialSetSequenceFile = "trial-set-sequence.json"
)

var trialSetIDRE = regexp.MustCompile(`^TS-[1-9][0-9]*$`)

// NextTrialSetID gives out the next trial-set ID, `TS-<n>`, and returns it only once the sequence
// that gave it is durable. An ID is given out once, even when no record is written for it or its
// slice is erased later.
func (s *Store) NextTrialSetID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var seq struct {
		Last int64 `json:"last"`
	}
	b, err := os.ReadFile(filepath.Join(s.root, trialSetSequenceFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return "", fmt.Errorf("store: the trial-set ID sequence: %w", err)
	default:
		if err := json.Unmarshal(b, &seq); err != nil || seq.Last < 0 {
			return "", fmt.Errorf("store: the trial-set ID sequence is unreadable: %v", err)
		}
	}
	seq.Last++
	b, _ = json.Marshal(seq) // a struct of an int cannot fail
	if err := writeAtomic(s.root, trialSetSequenceFile, b); err != nil {
		return "", err
	}
	return "TS-" + strconv.FormatInt(seq.Last, 10), nil
}

// PutTrialSet replaces the set's record in its slice's folder and returns only once it is
// durable. A slice the store holds no folder for (never recorded, or erased), a malformed slice
// ID, or an ID that is no `TS-<n>` is an error and writes nothing.
func (s *Store) PutTrialSet(sliceID, trialSetID string, record []byte) error {
	sl, err := s.ids.ParseID(sliceID)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if !trialSetIDRE.MatchString(trialSetID) {
		return fmt.Errorf("store: %q is no trial-set ID", trialSetID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.slices, sl.String())
	if fi, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("store: slice %s: %w", sl, ErrNotFound)
	} else if err != nil {
		return err
	} else if !fi.IsDir() {
		return fmt.Errorf("store: slice %s is no folder", sl)
	}
	sets := filepath.Join(dir, trialSetsFolder)
	if _, err := os.Stat(sets); errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(sets, 0o700); err != nil {
			return err
		}
		if err := syncDir(dir); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return writeAtomic(sets, trialSetID+recordSuffix, record)
}

// TrialSet returns the set's slice and record; an error wrapping ErrNotFound when none is kept
// (never kept, or erased with its slice). An ID that is no `TS-<n>` is an error.
func (s *Store) TrialSet(trialSetID string) (sliceID string, record []byte, err error) {
	if !trialSetIDRE.MatchString(trialSetID) {
		return "", nil, fmt.Errorf("store: %q is no trial-set ID", trialSetID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	matches, err := filepath.Glob(filepath.Join(s.slices, "*", trialSetsFolder, trialSetID+recordSuffix))
	if err != nil {
		return "", nil, err
	}
	for _, m := range matches {
		sl := filepath.Base(filepath.Dir(filepath.Dir(m)))
		if _, err := s.ids.ParseID(sl); err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Clean(m))
		if err != nil {
			return "", nil, err
		}
		return sl, b, nil
	}
	return "", nil, fmt.Errorf("store: trial set %s: %w", trialSetID, ErrNotFound)
}

// TrialSets returns the records of every set of the slice, by set ID; none for a slice without
// sets or an unknown one.
func (s *Store) TrialSets(sliceID string) (map[string][]byte, error) {
	sl, err := s.ids.ParseID(sliceID)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.slices, sl.String(), trialSetsFolder)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string][]byte{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), recordSuffix)
		if !ok || !trialSetIDRE.MatchString(id) || !e.Type().IsRegular() {
			continue
		}
		b, err := os.ReadFile(filepath.Clean(filepath.Join(dir, e.Name())))
		if err != nil {
			return nil, err
		}
		out[id] = b
	}
	return out, nil
}
