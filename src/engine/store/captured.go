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
// erased. The store keeps the record's bytes; the engine API owns their format. An erased captured
// pattern keeps only captured/<id>/erased, so its ID is never given again (EraseCapturedPattern).
const (
	capturedDir    = "captured"
	recordFile     = "record.json"
	capturedHeld   = "artifacts"
	capturedErased = "erased"
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
		if erased, err := exists(filepath.Join(s.root, capturedDir, id.String(), capturedErased)); err != nil {
			return err
		} else if erased {
			continue // an erasure in progress: what it still lists is held by no one
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
	if erased, err := exists(filepath.Join(dir, capturedErased)); err != nil {
		return err
	} else if erased {
		return fmt.Errorf("store: captured pattern %s was erased: its ID is never given again", id)
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
	dir := filepath.Join(s.root, capturedDir, id.String())
	if erased, err := exists(filepath.Join(dir, capturedErased)); err != nil {
		return nil, err
	} else if erased {
		return nil, fmt.Errorf("captured pattern %s was erased: %w", id, ErrNotFound)
	}
	b, err := os.ReadFile(filepath.Clean(filepath.Join(dir, recordFile)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("captured pattern %s: %w", id, ErrNotFound)
	}
	return b, err
}

// CapturedPatternErased reports whether the captured pattern was erased, or its erasure begun: its
// ID is then never given again.
func (s *Store) CapturedPatternErased(capturedID string) (bool, error) {
	id, err := s.ids.ParsePatternID(capturedID)
	if err != nil {
		return false, fmt.Errorf("store: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return exists(filepath.Join(s.root, capturedDir, id.String(), capturedErased))
}

// EraseCapturedPattern erases the captured pattern's record and every artifact it holds that no
// slice stored and no other captured pattern holds, and keeps its ID taken for good. The steps, in
// order, each durable before the next: the `erased` mark (from then on no reader finds the
// captured pattern and no record can be written under its ID), the record, the artifacts only it
// holds, then the list of what it held. A crash part way leaves the mark and the list, so calling
// it again completes the erasure; erasing an erased or unknown captured pattern only leaves the
// mark.
func (s *Store) EraseCapturedPattern(capturedID string) error {
	id, err := s.ids.ParsePatternID(capturedID)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.capturedFolder(id)
	if err != nil {
		return err
	}
	if err := writeAtomic(dir, capturedErased, nil); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, recordFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := syncDir(dir); err != nil {
		return err
	}
	lines, err := readLines(filepath.Join(dir, capturedHeld))
	if err != nil {
		return err
	}
	delete(s.held, id) // its holds no longer keep an artifact
	for _, l := range lines {
		h, err := s.ids.ParseHash(string(l))
		if err != nil {
			return fmt.Errorf("store: captured pattern %s: bad artifact hash: %w", id, err)
		}
		if s.heldByCaptured(h) || s.ownedBySlice(h) {
			continue
		}
		if err := os.Remove(filepath.Join(s.artifacts, h.String())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := syncDir(s.artifacts); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, capturedHeld)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return syncDir(dir)
}

// ownedBySlice reports whether a slice stored the artifact. The caller holds s.mu.
func (s *Store) ownedBySlice(h slice.Hash) bool {
	for _, hashes := range s.owned {
		if hashes[h] {
			return true
		}
	}
	return false
}

// exists reports whether path exists.
func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	}
	return false, err
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
		if erased, err := exists(filepath.Join(s.root, capturedDir, id.String(), capturedErased)); err != nil {
			return nil, err
		} else if erased {
			continue
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
