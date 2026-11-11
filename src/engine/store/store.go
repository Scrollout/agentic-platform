// Package store keeps each run's events and the artifacts runs produce, durably and grouped by
// slice, so that a run can be read back in order and a slice's data can be erased.
//
// Limits: slice IDs are "S-<n>" and run IDs "RUN-<n>"; a run belongs to the slice of its first
// event. One Store at a time may use a directory.
package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

// ErrNotFound is returned (wrapped) for an artifact the store does not hold.
var ErrNotFound = errors.New("store: not found")

// Event is one entry of a run record.
type Event struct {
	Seq  int    `json:"seq"` // set by the store: 1, 2, 3 ... per run, without gaps
	Run  string `json:"run"`
	Kind string `json:"kind"`
	Step string `json:"step,omitempty"`
}

var (
	sliceID = regexp.MustCompile(`^S-[0-9]+$`)
	runID   = regexp.MustCompile(`^RUN-[0-9]+$`)
	hashID  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// On disk: artifacts/<sha256> holds each artifact once; slices/<slice>/events.jsonl holds the
// slice's events and slices/<slice>/artifacts the hashes the slice stored, one per line. Both
// files only grow; erasure removes the slice's folder and every artifact no other slice holds.
const (
	eventsFile = "events.jsonl"
	ownedFile  = "artifacts"
)

// Store is a run store in one directory. It is safe for concurrent use.
type Store struct {
	mu        sync.Mutex
	artifacts string
	slices    string
	events    map[string][]Event         // run -> events in Seq order
	runSlice  map[string]string          // run -> slice
	owned     map[string]map[string]bool // slice -> hashes it stored
}

// Open opens the store in dir, creating it if needed, and continues any records already there.
func Open(dir string) (*Store, error) {
	s := &Store{
		artifacts: filepath.Join(dir, "artifacts"),
		slices:    filepath.Join(dir, "slices"),
		events:    map[string][]Event{},
		runSlice:  map[string]string{},
		owned:     map[string]map[string]bool{},
	}
	for _, d := range []string{s.artifacts, s.slices} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	// Leftovers of a PutArtifact interrupted before its rename were never acknowledged.
	tmps, _ := filepath.Glob(filepath.Join(s.artifacts, ".tmp-*"))
	for _, t := range tmps {
		if err := os.Remove(t); err != nil {
			return nil, err
		}
	}
	entries, err := os.ReadDir(s.slices)
	if err != nil {
		return nil, err
	}
	for _, d := range entries {
		slice := d.Name()
		if !sliceID.MatchString(slice) {
			return nil, fmt.Errorf("store: unexpected entry %q in %s", slice, s.slices)
		}
		lines, err := readLines(filepath.Join(s.slices, slice, eventsFile))
		if err != nil {
			return nil, err
		}
		for _, l := range lines {
			var e Event
			if err := json.Unmarshal(l, &e); err != nil {
				return nil, fmt.Errorf("store: slice %s: bad event: %w", slice, err)
			}
			s.events[e.Run] = append(s.events[e.Run], e)
			s.runSlice[e.Run] = slice
		}
		lines, err = readLines(filepath.Join(s.slices, slice, ownedFile))
		if err != nil {
			return nil, err
		}
		for _, l := range lines {
			if !hashID.Match(l) {
				return nil, fmt.Errorf("store: slice %s: bad artifact hash %q", slice, l)
			}
			s.own(slice, string(l))
		}
	}
	return s, nil
}

// Close releases the store. The store holds no open files between calls, so nothing is lost
// by not calling it.
func (s *Store) Close() error { return nil }

// Append records e under slice, assigns the next Seq of e.Run, and returns the event as stored.
// It returns only after the event is durably recorded; on error nothing was acknowledged and the
// call may be retried. The first Append for a run ties the run to slice; appending that run under
// another slice is an error.
func (s *Store) Append(slice string, e Event) (Event, error) {
	if !sliceID.MatchString(slice) || !runID.MatchString(e.Run) || e.Kind == "" {
		return Event{}, fmt.Errorf("store: invalid event %+v for slice %q", e, slice)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner, ok := s.runSlice[e.Run]; ok && owner != slice {
		return Event{}, fmt.Errorf("store: run %s belongs to slice %s, not %s", e.Run, owner, slice)
	}
	e.Seq = len(s.events[e.Run]) + 1
	line, err := json.Marshal(e)
	if err != nil {
		return Event{}, err
	}
	if err := s.appendLine(slice, eventsFile, line); err != nil {
		return Event{}, err
	}
	s.events[e.Run] = append(s.events[e.Run], e)
	s.runSlice[e.Run] = slice
	return e, nil
}

// Events returns every event of run in Seq order; an unknown run gives none.
func (s *Store) Events(run string) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.events[run]...), nil
}

// PutArtifact stores data for slice and returns its lowercase hex SHA-256. It returns only after
// the artifact is durably stored. The same bytes are stored once, however many slices store them.
func (s *Store) PutArtifact(slice string, data []byte) (string, error) {
	if !sliceID.MatchString(slice) {
		return "", fmt.Errorf("store: invalid slice %q", slice)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owned[slice][hash] {
		return hash, nil
	}
	// Ownership first: an artifact on disk is then always one some slice's erasure removes.
	if err := s.appendLine(slice, ownedFile, []byte(hash)); err != nil {
		return "", err
	}
	s.own(slice, hash)
	path := filepath.Join(s.artifacts, hash)
	if _, err := os.Stat(path); err == nil {
		return hash, nil
	}
	tmp, err := os.CreateTemp(s.artifacts, ".tmp-")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return hash, syncDir(s.artifacts)
}

// Artifact returns the exact bytes stored under hash, or ErrNotFound. A hash that is not 64
// lowercase hex digits is an error.
func (s *Store) Artifact(hash string) ([]byte, error) {
	if !hashID.MatchString(hash) {
		return nil, fmt.Errorf("store: invalid hash %q", hash)
	}
	b, err := os.ReadFile(filepath.Join(s.artifacts, hash))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("artifact %s: %w", hash, ErrNotFound)
	}
	return b, err
}

// EraseSlice removes every event of the slice's runs and every artifact stored for the slice,
// from disk. An artifact another slice also stored stays readable for it. Erasing an unknown
// slice does nothing; an interrupted erasure is completed by calling it again.
func (s *Store) EraseSlice(slice string) error {
	if !sliceID.MatchString(slice) {
		return fmt.Errorf("store: invalid slice %q", slice)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Artifacts first, so a crash part way leaves the ownership list that finds them again.
	for hash := range s.owned[slice] {
		if s.heldElsewhere(slice, hash) {
			continue
		}
		if err := os.Remove(filepath.Join(s.artifacts, hash)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := syncDir(s.artifacts); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(s.slices, slice)); err != nil {
		return err
	}
	if err := syncDir(s.slices); err != nil {
		return err
	}
	delete(s.owned, slice)
	for run, owner := range s.runSlice {
		if owner == slice {
			delete(s.runSlice, run)
			delete(s.events, run)
		}
	}
	return nil
}

func (s *Store) own(slice, hash string) {
	if s.owned[slice] == nil {
		s.owned[slice] = map[string]bool{}
	}
	s.owned[slice][hash] = true
}

func (s *Store) heldElsewhere(slice, hash string) bool {
	for other, hashes := range s.owned {
		if other != slice && hashes[hash] {
			return true
		}
	}
	return false
}

// appendLine appends line to the slice's file name and syncs it, and on creation its folders.
func (s *Store) appendLine(slice, name string, line []byte) error {
	dir := filepath.Join(s.slices, slice)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return err
		}
		if err := syncDir(s.slices); err != nil {
			return err
		}
	}
	path := filepath.Join(dir, name)
	_, statErr := os.Stat(path)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && errors.Is(statErr, fs.ErrNotExist) {
		err = syncDir(dir)
	}
	return err
}

// readLines returns the complete lines of path (none if it does not exist). A last line without
// its newline is the remains of a write that was never acknowledged; it is cut off.
func readLines(path string) ([][]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if end := bytes.LastIndexByte(b, '\n') + 1; end < len(b) {
		if err := os.Truncate(path, int64(end)); err != nil {
			return nil, err
		}
		b = b[:end]
	}
	var lines [][]byte
	for _, l := range bytes.Split(b, []byte("\n")) {
		if len(l) > 0 {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}
