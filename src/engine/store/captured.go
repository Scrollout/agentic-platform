package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/agentic-platform/slice"
)

// Captured-pattern records (feature 011): captured/<id>/record.json holds a captured pattern's
// record, replaced atomically like a slice's, and captured/<id>/artifacts the hashes of the
// artifacts it holds, one per line, which only grows. A captured pattern is the second holder of
// an artifact besides the slices: an artifact it holds stays when the slice that stored it is
// erased. The store keeps the record's bytes; the engine API owns their format.
const (
	capturedDir  = "captured"
	recordFile   = "record.json"
	capturedHeld = "artifacts"
)

// openCaptured reads which artifacts each captured pattern holds. The caller is Open.
func (s *Store) openCaptured() error {
	s.held = map[slice.PatternID]map[slice.Hash]bool{}
	entries, err := os.ReadDir(filepath.Join(s.root, capturedDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, d := range entries {
		id, err := s.ids.ParsePatternID(d.Name())
		if err != nil {
			return fmt.Errorf("store: unexpected entry in %s: %w", capturedDir, err)
		}
		lines, err := readLines(filepath.Join(s.root, capturedDir, id.String(), capturedHeld))
		if err != nil {
			return err
		}
		for _, l := range lines {
			h, err := s.ids.ParseHash(string(l))
			if err != nil {
				return fmt.Errorf("store: captured pattern %s: bad artifact hash: %w", id, err)
			}
			s.hold(id, h)
		}
	}
	return nil
}

func (s *Store) hold(id slice.PatternID, h slice.Hash) {
	if s.held[id] == nil {
		s.held[id] = map[slice.Hash]bool{}
	}
	s.held[id][h] = true
}

// PutCapturedPattern makes the captured pattern hold the artifacts named by holds, then replaces
// its record, and returns only once both are durable. An ID that breaks the contract's
// pattern_id rule, a hash that breaks its hash rule, or a held artifact the store does not have is
// an error, and the record is not written.
func (s *Store) PutCapturedPattern(capturedID string, record []byte, holds []string) error {
	id, err := s.ids.ParsePatternID(capturedID)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	hashes := make([]slice.Hash, len(holds))
	for i, h := range holds {
		if hashes[i], err = s.ids.ParseHash(h); err != nil {
			return fmt.Errorf("store: %w", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.capturedFolder(id)
	if err != nil {
		return err
	}
	for _, h := range hashes {
		if s.held[id][h] {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.artifacts, h.String())); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("artifact %s: %w", h, ErrNotFound)
			}
			return err
		}
		if err := appendTo(dir, capturedHeld, []byte(h.String())); err != nil {
			return err
		}
		s.hold(id, h)
	}
	return writeAtomic(dir, recordFile, record)
}

// CapturedPattern returns the captured pattern's record, or an error wrapping ErrNotFound when
// it has none.
func (s *Store) CapturedPattern(capturedID string) ([]byte, error) {
	id, err := s.ids.ParsePatternID(capturedID)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(s.root, capturedDir, id.String(), recordFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("captured pattern %s: %w", id, ErrNotFound)
	}
	return b, err
}

// CapturedPatterns returns the record of every captured pattern that has one, by ID.
func (s *Store) CapturedPatterns() (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(s.root, capturedDir))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string][]byte{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, d := range entries {
		id, err := s.ids.ParsePatternID(d.Name())
		if err != nil {
			return nil, fmt.Errorf("store: unexpected entry in %s: %w", capturedDir, err)
		}
		b, err := os.ReadFile(filepath.Join(s.root, capturedDir, id.String(), recordFile))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("store: the record of captured pattern %s: %w", id, err)
		}
		out[id.String()] = b
	}
	return out, nil
}

// heldByCaptured reports whether a captured pattern holds the artifact. The caller holds s.mu.
func (s *Store) heldByCaptured(h slice.Hash) bool {
	for _, hashes := range s.held {
		if hashes[h] {
			return true
		}
	}
	return false
}

// capturedFolder returns the captured pattern's folder, creating it and captured/ (durably) if
// needed. The caller holds s.mu.
func (s *Store) capturedFolder(id slice.PatternID) (string, error) {
	top := filepath.Join(s.root, capturedDir)
	for _, d := range []struct{ dir, parent string }{{top, s.root}, {filepath.Join(top, id.String()), top}} {
		if _, err := os.Stat(d.dir); errors.Is(err, fs.ErrNotExist) {
			if err := os.Mkdir(d.dir, 0o700); err != nil {
				return "", err
			}
			if err := syncDir(d.parent); err != nil {
				return "", err
			}
		} else if err != nil {
			return "", err
		}
	}
	return filepath.Join(top, id.String()), nil
}
