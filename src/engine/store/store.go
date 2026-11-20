// Package store keeps each run's events, the artifacts runs produce and each slice's record,
// durably and grouped by slice, so that a run can be read back in order, a slice survives a
// restart, and a slice's data can be erased.
//
// Slice IDs, run IDs and artifact hashes follow the rules of the contract the store is opened
// with (slice-defs.schema.json, through package slice); every string a method takes is parsed
// against them first. A run belongs to the slice of its first event. One Store at a time may use
// a directory.
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
	"sync"

	"github.com/agentic-platform/slice"
)

// ErrNotFound is returned (wrapped) for an artifact the store does not hold.
var ErrNotFound = errors.New("store: not found")

// Event is one entry of a run record.
type Event struct {
	Seq  int    `json:"seq"` // set by the store: 1, 2, 3 ... per run, without gaps
	Run  string `json:"run"`
	Kind string `json:"kind"`
	Step string `json:"step,omitempty"`
	// Data is the whole event as its writer encodes it (the engine: the run-event.schema.json
	// form and what it records about the run); the store keeps it as given.
	Data RawJSON `json:"data,omitempty"`
}

// RawJSON is one JSON value kept as its text: written as it is, read back as it was. Unlike
// json.RawMessage it is comparable, so an Event is too.
type RawJSON string

func (r RawJSON) MarshalJSON() ([]byte, error) {
	if r == "" {
		return []byte("null"), nil
	}
	if !json.Valid([]byte(r)) {
		return nil, errors.New("store: event data is not one JSON value")
	}
	return []byte(r), nil
}

func (r *RawJSON) UnmarshalJSON(b []byte) error {
	*r = RawJSON(b)
	return nil
}

// On disk: artifacts/<sha256> holds each artifact once; slices/<slice>/events.jsonl holds the
// slice's events and slices/<slice>/artifacts the hashes the slice stored, one per line. Both
// files only grow; erasure removes the slice's folder and every artifact no other slice holds.
// The slice's record (slice.json) and the slice ID sequence are in records.go.
const (
	eventsFile = "events.jsonl"
	ownedFile  = "artifacts"
)

// Store is a run store in one directory. It is safe for concurrent use.
type Store struct {
	mu        sync.Mutex
	ids       *slice.Schemas // the contract's rules for slice IDs, run IDs and hashes
	root      string
	artifacts string
	slices    string
	events    map[slice.RunID][]Event          // run -> events in Seq order
	runSlice  map[slice.RunID]slice.ID         // run -> slice
	owned     map[slice.ID]map[slice.Hash]bool // slice -> hashes it stored
}

// Open opens the store in dir, creating it if needed, and continues any records already there.
// IDs and hashes are parsed with ids, the schemas of the contract version the engine serves.
func Open(dir string, ids *slice.Schemas) (*Store, error) {
	if ids == nil {
		return nil, errors.New("store: no contract schemas to parse IDs with")
	}
	s := &Store{
		ids:       ids,
		root:      dir,
		artifacts: filepath.Join(dir, "artifacts"),
		slices:    filepath.Join(dir, "slices"),
		events:    map[slice.RunID][]Event{},
		runSlice:  map[slice.RunID]slice.ID{},
		owned:     map[slice.ID]map[slice.Hash]bool{},
	}
	// The store holds request bodies and run content: a folder it creates, its own included, is
	// its owner's only. An existing folder keeps its mode.
	for _, d := range []string{dir, s.artifacts, s.slices} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	// Leftovers of a PutArtifact, or of a slice record or sequence write, interrupted before its
	// rename were never acknowledged.
	var tmps []string
	for _, pattern := range []string{filepath.Join(s.artifacts, tmpPrefix+"*"), filepath.Join(dir, tmpPrefix+"*"),
		filepath.Join(s.slices, "*", tmpPrefix+"*")} {
		m, _ := filepath.Glob(pattern)
		tmps = append(tmps, m...)
	}
	for _, t := range tmps {
		if err := os.Remove(t); err != nil {
			return nil, err
		}
	}
	if err := s.openOperations(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.slices)
	if err != nil {
		return nil, err
	}
	for _, d := range entries {
		sl, err := ids.ParseID(d.Name())
		if err != nil {
			return nil, fmt.Errorf("store: unexpected entry in %s: %w", s.slices, err)
		}
		lines, err := readLines(filepath.Join(s.slices, sl.String(), eventsFile))
		if err != nil {
			return nil, err
		}
		for _, l := range lines {
			var e Event
			if err := json.Unmarshal(l, &e); err != nil {
				return nil, fmt.Errorf("store: slice %s: bad event: %w", sl, err)
			}
			run, err := ids.ParseRunID(e.Run)
			if err != nil {
				return nil, fmt.Errorf("store: slice %s: bad event: %w", sl, err)
			}
			s.events[run] = append(s.events[run], e)
			s.runSlice[run] = sl
		}
		lines, err = readLines(filepath.Join(s.slices, sl.String(), ownedFile))
		if err != nil {
			return nil, err
		}
		for _, l := range lines {
			h, err := ids.ParseHash(string(l))
			if err != nil {
				return nil, fmt.Errorf("store: slice %s: bad artifact hash: %w", sl, err)
			}
			s.own(sl, h)
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
func (s *Store) Append(sliceID string, e Event) (Event, error) { return s.record(sliceID, e, false) }

// ErrSeqTaken is returned (wrapped) by RecordEvent for a Seq that is held with other content, or
// that lies beyond the next one.
var ErrSeqTaken = errors.New("store: seq taken")

// RecordEvent records e, whose Seq the caller sets, under slice, and returns the event as stored.
// A Seq one beyond the run's last is appended; a Seq already held with an equal event succeeds and
// writes nothing; a Seq held with other content, or beyond the next one, is ErrSeqTaken; a Seq
// below 1 is refused. Otherwise it behaves like Append, durability and validation included.
func (s *Store) RecordEvent(sliceID string, e Event) (Event, error) {
	return s.record(sliceID, e, true)
}

// record is Append (callerSeq false: the store sets Seq) or RecordEvent (callerSeq true: the caller's Seq).
func (s *Store) record(sliceID string, e Event, callerSeq bool) (Event, error) {
	sl, err := s.ids.ParseID(sliceID)
	if err != nil {
		return Event{}, fmt.Errorf("store: %w", err)
	}
	run, err := s.ids.ParseRunID(e.Run)
	if err != nil {
		return Event{}, fmt.Errorf("store: %w", err)
	}
	if e.Kind == "" {
		return Event{}, fmt.Errorf("store: event of run %s has no kind", run)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner, ok := s.runSlice[run]; ok && owner != sl {
		return Event{}, fmt.Errorf("store: run %s belongs to slice %s, not %s", run, owner, sl)
	}
	held := s.events[run]
	if callerSeq {
		switch {
		case e.Seq < 1:
			return Event{}, fmt.Errorf("store: event of run %s has seq %d, below 1", run, e.Seq)
		case e.Seq <= len(held):
			if held[e.Seq-1] == e {
				return e, nil
			}
			return Event{}, fmt.Errorf("run %s seq %d: %w", run, e.Seq, ErrSeqTaken)
		case e.Seq > len(held)+1:
			return Event{}, fmt.Errorf("run %s seq %d is beyond the next, %d: %w", run, e.Seq, len(held)+1, ErrSeqTaken)
		}
	} else {
		e.Seq = len(held) + 1
	}
	line, err := json.Marshal(e)
	if err != nil {
		return Event{}, err
	}
	if err := s.appendLine(sl, eventsFile, line); err != nil {
		return Event{}, err
	}
	s.events[run] = append(s.events[run], e)
	s.runSlice[run] = sl
	return e, nil
}

// Events returns every event of run in Seq order; an unknown run gives none, an invalid run ID
// an error.
func (s *Store) Events(runID string) ([]Event, error) {
	run, err := s.ids.ParseRunID(runID)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.events[run]...), nil
}

// PutArtifact stores data for slice and returns its lowercase hex SHA-256. It returns only after
// the artifact is durably stored. The same bytes are stored once, however many slices store them.
func (s *Store) PutArtifact(sliceID string, data []byte) (string, error) {
	sl, err := s.ids.ParseID(sliceID)
	if err != nil {
		return "", fmt.Errorf("store: %w", err)
	}
	sum := sha256.Sum256(data)
	hash, err := s.ids.ParseHash(hex.EncodeToString(sum[:]))
	if err != nil {
		return "", fmt.Errorf("store: the contract's hash rule refuses a SHA-256: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owned[sl][hash] {
		return hash.String(), nil
	}
	// Ownership first: an artifact on disk is then always one some slice's erasure removes.
	if err := s.appendLine(sl, ownedFile, []byte(hash.String())); err != nil {
		return "", err
	}
	s.own(sl, hash)
	path := filepath.Join(s.artifacts, hash.String())
	if _, err := os.Stat(path); err == nil {
		return hash.String(), nil
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
	if err := syncDir(s.artifacts); err != nil {
		return "", err
	}
	return hash.String(), nil
}

// Artifact returns the exact bytes stored under hash, or ErrNotFound. A hash that breaks the
// contract's hash rule is an error and never reaches the file system as a path.
func (s *Store) Artifact(hashID string) ([]byte, error) {
	hash, err := s.ids.ParseHash(hashID)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	b, err := os.ReadFile(filepath.Join(s.artifacts, hash.String()))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("artifact %s: %w", hash, ErrNotFound)
	}
	return b, err
}

// ArtifactCount returns the number of distinct artifacts stored for the slice's runs: the same
// bytes stored twice count once. An unknown or erased slice has none; a slice ID that breaks the
// contract's rule is an error.
func (s *Store) ArtifactCount(sliceID string) (int, error) {
	sl, err := s.ids.ParseID(sliceID)
	if err != nil {
		return 0, fmt.Errorf("store: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.owned[sl]), nil
}

// EraseSlice removes every event of the slice's runs, every artifact stored for the slice and its
// step workers' registrations, from disk. An artifact another slice also stored stays readable for it. Erasing an unknown
// slice does nothing; an interrupted erasure is completed by calling it again.
func (s *Store) EraseSlice(sliceID string) error {
	sl, err := s.ids.ParseID(sliceID)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Artifacts first, so a crash part way leaves the ownership list that finds them again.
	for hash := range s.owned[sl] {
		if s.heldElsewhere(sl, hash) {
			continue
		}
		if err := os.Remove(filepath.Join(s.artifacts, hash.String())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := syncDir(s.artifacts); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(s.slices, sl.String())); err != nil {
		return err
	}
	if err := syncDir(s.slices); err != nil {
		return err
	}
	if err := s.eraseWorkers(sl.String()); err != nil {
		return err
	}
	delete(s.owned, sl)
	for run, owner := range s.runSlice {
		if owner == sl {
			delete(s.runSlice, run)
			delete(s.events, run)
		}
	}
	return nil
}

func (s *Store) own(sl slice.ID, hash slice.Hash) {
	if s.owned[sl] == nil {
		s.owned[sl] = map[slice.Hash]bool{}
	}
	s.owned[sl][hash] = true
}

func (s *Store) heldElsewhere(sl slice.ID, hash slice.Hash) bool {
	for other, hashes := range s.owned {
		if other != sl && hashes[hash] {
			return true
		}
	}
	return false
}

// appendLine appends line to the slice's file name and syncs it, and on creation its folders.
func (s *Store) appendLine(sl slice.ID, name string, line []byte) error {
	dir, err := s.sliceDir(sl)
	if err != nil {
		return err
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
