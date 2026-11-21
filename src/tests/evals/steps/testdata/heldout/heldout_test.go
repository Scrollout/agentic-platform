// Package heldout_test holds the tests the code agent never sees: each checks one rule of the
// notes slice (R-1 to R-3) beyond the examples the agent was given.
package heldout_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"example.com/service/gen"
	"example.com/service/internal/app"
)

type note struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

// reply is one answer of the service: the status and the raw body.
type reply struct {
	status int
	body   []byte
}

// serve starts a fresh service for one test.
func serve(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(gen.Handler(app.New()))
	t.Cleanup(s.Close)
	return s
}

func do(t *testing.T, req *http.Request) reply {
	t.Helper()
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("request %s %s failed: %v", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the answer to %s %s failed: %v", req.Method, req.URL.Path, err)
	}
	return reply{status: resp.StatusCode, body: b}
}

func create(t *testing.T, s *httptest.Server, text string) reply {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, s.URL+"/notes", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return do(t, req)
}

func created(t *testing.T, s *httptest.Server, text string) note {
	t.Helper()
	r := create(t, s, text)
	if r.status != http.StatusCreated {
		t.Fatalf("creating %q: expected status 201, got %d, body %q", text, r.status, r.body)
	}
	var n note
	if err := json.Unmarshal(r.body, &n); err != nil {
		t.Fatalf("creating %q: expected a JSON note, got body %q (%v)", text, r.body, err)
	}
	return n
}

func list(t *testing.T, s *httptest.Server) []note {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.URL+"/notes", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	r := do(t, req)
	if r.status != http.StatusOK {
		t.Fatalf("listing: expected status 200, got %d, body %q", r.status, r.body)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(r.body)), "[") {
		t.Fatalf("listing: expected a JSON array, got body %q", r.body)
	}
	var ns []note
	if err := json.Unmarshal(r.body, &ns); err != nil {
		t.Fatalf("listing: expected an array of notes, got body %q (%v)", r.body, err)
	}
	return ns
}

func texts(ns []note) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Text
	}
	return out
}

// TestR1_EmptyList checks R-1: a service with no notes lists an empty array (not null).
func TestR1_EmptyList(t *testing.T) {
	s := serve(t)
	req, err := http.NewRequest(http.MethodGet, s.URL+"/notes", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	r := do(t, req)
	if r.status != http.StatusOK {
		t.Fatalf("expected status 200 and an empty array, got %d, body %q", r.status, r.body)
	}
	var raw json.RawMessage
	if err := json.Unmarshal(r.body, &raw); err != nil || !strings.HasPrefix(strings.TrimSpace(string(r.body)), "[") {
		t.Fatalf("expected a JSON array with no item, got body %q", r.body)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(r.body, &items); err != nil || len(items) != 0 {
		t.Fatalf("expected a JSON array with no item, got body %q", r.body)
	}
}

// TestR1_ThreeInOrder checks R-1: the list holds every note, in creation order.
func TestR1_ThreeInOrder(t *testing.T) {
	s := serve(t)
	for _, x := range []string{"a", "b", "c"} {
		created(t, s, x)
	}
	got := texts(list(t, s))
	if want := []string{"a", "b", "c"}; strings.Join(got, "|") != strings.Join(want, "|") || len(got) != 3 {
		t.Fatalf("expected the texts %q in that order, got %q", want, got)
	}
}

// TestR2_FiveIDs checks R-2: each created note gets the next id, 1 to 5 in creation order, and the
// list carries the same ids.
func TestR2_FiveIDs(t *testing.T) {
	s := serve(t)
	for i, x := range []string{"one", "two", "three", "four", "five"} {
		if n := created(t, s, x); n.ID != i+1 {
			t.Fatalf("creating %q: expected id %d, got %d", x, i+1, n.ID)
		}
	}
	ns := list(t, s)
	if len(ns) != 5 {
		t.Fatalf("expected 5 listed notes, got %d: %+v", len(ns), ns)
	}
	for i, n := range ns {
		if n.ID != i+1 {
			t.Fatalf("expected the listed ids 1 to 5 in order, got %+v", ns)
		}
	}
}

// TestR2_TextKept checks R-2: the text of a note is kept as sent, byte for byte, in the answer to
// the create and in the list.
func TestR2_TextKept(t *testing.T) {
	const text = "ünïcödé text with  two spaces"
	s := serve(t)
	n := created(t, s, text)
	if n.Text != text {
		t.Fatalf("expected the created text %q, got %q", text, n.Text)
	}
	ns := list(t, s)
	if len(ns) != 1 || ns[0].Text != text {
		t.Fatalf("expected one listed note with the text %q, got %+v", text, ns)
	}
}

// TestR3_RefusedNotListed checks R-3: a note refused for empty text is not stored.
func TestR3_RefusedNotListed(t *testing.T) {
	s := serve(t)
	created(t, s, "kept")
	if r := create(t, s, ""); r.status != http.StatusBadRequest {
		t.Fatalf("expected status 400 for empty text, got %d, body %q", r.status, r.body)
	}
	ns := list(t, s)
	if len(ns) != 1 || ns[0].Text != "kept" {
		t.Fatalf("expected the list to hold only the note \"kept\", got %+v", ns)
	}
}

// TestR3_RefusalIsAnError checks R-3: the refusal is status 400 with a JSON error object whose
// message is a string.
func TestR3_RefusalIsAnError(t *testing.T) {
	s := serve(t)
	r := create(t, s, "")
	if r.status != http.StatusBadRequest {
		t.Fatalf("expected status 400 for empty text, got %d, body %q", r.status, r.body)
	}
	var e map[string]json.RawMessage
	if err := json.Unmarshal(r.body, &e); err != nil {
		t.Fatalf("expected a JSON object with a message, got body %q", r.body)
	}
	// json.Unmarshal accepts null into a string, so the raw value must be checked: a JSON string
	// starts with a quote (a missing message is empty, null, a number or an object do not).
	raw := bytes.TrimSpace(e["message"])
	var msg string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &msg) != nil {
		t.Fatalf("expected a JSON object whose message is a string, got body %q", r.body)
	}
}
