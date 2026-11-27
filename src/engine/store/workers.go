package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// workersFile, at the store's root, holds the step workers' registrations (ADR 0071).
const workersFile = "workers.json"

// grantRE is the name of a grant file: the lowercase hex SHA-256 of the worker token (ADR 0034).
var grantRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// WorkerRegistration is where the step worker of one step of one slice was registered for
// Project (empty: for any project of the slice, as every registration kept before projects were
// named): its endpoint and Grant, the name of the grant file its token was issued by. It holds no
// token. A file written before Project existed reads with Project empty.
type WorkerRegistration struct {
	Slice, Step string
	Project     string `json:",omitempty"`
	Endpoint    string
	Grant       string
}

// WorkerRegistrations returns every kept registration, sorted by slice, then step, then project.
func (s *Store) WorkerRegistrations() ([]WorkerRegistration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readWorkers()
}

// PutWorkerRegistration adds r, or replaces the registration of r's slice, step and project, and returns
// only once it is durable. A slice ID that breaks the contract's rule, an empty step or endpoint,
// or a Grant that is no lowercase hex SHA-256 is an error and changes nothing.
func (s *Store) PutWorkerRegistration(r WorkerRegistration) error {
	if _, err := s.ids.ParseID(r.Slice); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if r.Step == "" || r.Endpoint == "" || !grantRE.MatchString(r.Grant) {
		return fmt.Errorf("store: a worker registration needs a step, an endpoint and a grant file's name")
	}
	return s.changeWorkers(func(all []WorkerRegistration) []WorkerRegistration {
		all = slices.DeleteFunc(all, func(w WorkerRegistration) bool {
			return w.Slice == r.Slice && w.Step == r.Step && w.Project == r.Project
		})
		return append(all, r)
	})
}

// DeleteWorkerRegistration removes the registration of the slice's step for project (empty: the
// one for any project); none is no error.
func (s *Store) DeleteWorkerRegistration(sliceID, step, project string) error {
	if _, err := s.ids.ParseID(sliceID); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return s.changeWorkers(func(all []WorkerRegistration) []WorkerRegistration {
		return slices.DeleteFunc(all, func(w WorkerRegistration) bool {
			return w.Slice == sliceID && w.Step == step && w.Project == project
		})
	})
}

// changeWorkers applies change to the kept registrations and writes them back atomically.
func (s *Store) changeWorkers(change func([]WorkerRegistration) []WorkerRegistration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readWorkers()
	if err != nil {
		return err
	}
	return s.writeWorkers(change(all))
}

// eraseWorkers removes the slice's registrations, of every project; the caller holds s.mu.
func (s *Store) eraseWorkers(slice string) error {
	all, err := s.readWorkers()
	if err != nil {
		return err
	}
	kept := slices.DeleteFunc(slices.Clone(all), func(w WorkerRegistration) bool { return w.Slice == slice })
	if len(kept) == len(all) {
		return nil
	}
	return s.writeWorkers(kept)
}

// readWorkers reads the registrations file; none is an empty list. The caller holds s.mu.
func (s *Store) readWorkers() ([]WorkerRegistration, error) {
	b, err := os.ReadFile(filepath.Join(s.root, workersFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: the worker registrations: %w", err)
	}
	var all []WorkerRegistration
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, fmt.Errorf("store: the worker registrations: %w", err)
	}
	sortWorkers(all)
	return all, nil
}

// writeWorkers replaces the registrations file; the caller holds s.mu.
func (s *Store) writeWorkers(all []WorkerRegistration) error {
	sortWorkers(all)
	if all == nil {
		all = []WorkerRegistration{}
	}
	b, err := json.Marshal(all)
	if err != nil {
		return err
	}
	return writeAtomic(s.root, workersFile, b)
}

func sortWorkers(all []WorkerRegistration) {
	slices.SortFunc(all, func(a, b WorkerRegistration) int {
		if c := strings.Compare(a.Slice, b.Slice); c != 0 {
			return c
		}
		if c := strings.Compare(a.Step, b.Step); c != 0 {
			return c
		}
		return strings.Compare(a.Project, b.Project)
	})
}
