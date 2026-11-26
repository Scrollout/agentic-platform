// Package app is the in-memory notes service: the code step replaces the seam New.
package app

import (
	"encoding/json"
	"net/http"
	"sync"

	"example.com/service/gen"
)

// Service keeps the notes in creation order; ids start at 1.
type Service struct {
	mu    sync.Mutex
	notes []gen.Note
}

var _ gen.ServerInterface = (*Service)(nil)

// New returns an empty service (the seam the tests serve).
func New() gen.ServerInterface { return &Service{notes: []gen.Note{}} }

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ListNotes returns every note, in creation order.
func (s *Service) ListNotes(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	write(w, http.StatusOK, s.notes)
}

// CreateNote stores a note with the next id, or refuses empty text or a text over 280 characters with a 400.
func (s *Service) CreateNote(w http.ResponseWriter, r *http.Request) {
	var in gen.NewNote
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Text == "" || len(in.Text) > 280 {
		write(w, http.StatusBadRequest, gen.Error{Message: "the text of a note must not be empty or exceed 280 characters"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := gen.Note{Id: len(s.notes) + 1, Text: in.Text}
	s.notes = append(s.notes, n)
	write(w, http.StatusCreated, n)
}
