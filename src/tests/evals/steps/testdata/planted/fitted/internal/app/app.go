// Package app is a planted violation for the HeldOutTestsPass self-test: an implementation fitted
// to the examples. It passes every reference test but keeps only the first two notes (a third
// create is answered 201 and then forgotten) and lists no notes as null instead of an empty array.
package app

import (
	"encoding/json"
	"net/http"
	"sync"

	"example.com/service/gen"
)

// Service keeps at most two notes.
type Service struct {
	mu    sync.Mutex
	next  int
	notes []gen.Note
}

var _ gen.ServerInterface = (*Service)(nil)

// New returns an empty service (the seam the tests serve).
func New() gen.ServerInterface { return &Service{} }

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ListNotes answers the kept notes; with none it answers null.
func (s *Service) ListNotes(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	write(w, http.StatusOK, s.notes)
}

// CreateNote answers 201 for every non-empty text but keeps only the first two notes.
func (s *Service) CreateNote(w http.ResponseWriter, r *http.Request) {
	var in gen.NewNote
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Text == "" {
		write(w, http.StatusBadRequest, gen.Error{Message: "the text of a note must not be empty"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	n := gen.Note{Id: s.next, Text: in.Text}
	if len(s.notes) < 2 {
		s.notes = append(s.notes, n)
	}
	write(w, http.StatusCreated, n)
}
