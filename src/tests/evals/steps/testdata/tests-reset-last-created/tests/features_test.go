package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
	"time"

	"example.com/service/gen"

	"github.com/cucumber/godog"
)

// world is the state of one scenario: the server under test, the last response and the
// note returned by the last successful create.
type world struct {
	srv         *httptest.Server
	client      *http.Client
	lastStatus  int
	lastBody    []byte
	lastCreated *gen.Note
}

func (w *world) url(path string) string { return w.srv.URL + path }

// do sends one request and records the status and body of the response.
func (w *world) do(method, path string, body any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, w.url(path), reader)
	if err != nil {
		return fmt.Errorf("building %s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading %s %s response: %w", method, path, err)
	}
	w.lastStatus = resp.StatusCode
	w.lastBody = data
	w.lastCreated = nil
	return nil
}

// createNote posts a note and returns an error unless the server answers 201.
func (w *world) createNote(text string) error {
	if err := w.do(http.MethodPost, "/notes", gen.NewNote{Text: text}); err != nil {
		return err
	}
	if w.lastStatus != http.StatusCreated {
		return fmt.Errorf("creating note %q: expected status 201, got %d", text, w.lastStatus)
	}
	var note gen.Note
	if err := json.Unmarshal(w.lastBody, &note); err != nil {
		return fmt.Errorf("creating note %q: expected a Note body, got %q: %w", text, w.lastBody, err)
	}
	w.lastCreated = &note
	return nil
}

var quoted = regexp.MustCompile(`"([^"]*)"`)

// givenNotes creates each quoted note in order.
func (w *world) givenNotes(list string) error {
	matches := quoted.FindAllStringSubmatch(list, -1)
	if len(matches) == 0 {
		return fmt.Errorf("no quoted note texts in %q", list)
	}
	for _, m := range matches {
		if err := w.createNote(m[1]); err != nil {
			return err
		}
	}
	return nil
}

func (w *world) checkStatus(want int) error {
	if w.lastStatus != want {
		return fmt.Errorf("expected response status %d, got %d (body %q)", want, w.lastStatus, w.lastBody)
	}
	return nil
}

func (w *world) theNotesAreListed(list string) error {
	if err := w.do(http.MethodGet, "/notes", nil); err != nil {
		return err
	}
	if err := w.checkStatus(http.StatusOK); err != nil {
		return fmt.Errorf("listing notes: %w", err)
	}
	var notes []gen.Note
	if err := json.Unmarshal(w.lastBody, &notes); err != nil {
		return fmt.Errorf("listing notes: expected an array of Note, got %q: %w", w.lastBody, err)
	}
	var want []string
	for _, m := range quoted.FindAllStringSubmatch(list, -1) {
		want = append(want, m[1])
	}
	var got []string
	for _, n := range notes {
		got = append(got, n.Text)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		return fmt.Errorf("expected notes in order %q, got %q", want, got)
	}
	return nil
}

func (w *world) theNoteHasID(idText string) error {
	want, err := strconv.Atoi(idText)
	if err != nil {
		return fmt.Errorf("bad expected id %q: %w", idText, err)
	}
	if w.lastCreated == nil {
		return fmt.Errorf("expected a created note with id %d, but the last response was status %d with body %q", want, w.lastStatus, w.lastBody)
	}
	if w.lastCreated.Id != want {
		return fmt.Errorf("expected note id %d, got %d", want, w.lastCreated.Id)
	}
	return nil
}

func TestFeatures(t *testing.T) {
	var w *world

	suite := godog.TestSuite{
		Name: "notes",
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			s.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
				srv := httptest.NewServer(handler())
				w = &world{srv: srv, client: &http.Client{Timeout: 10 * time.Second}}
				return ctx, nil
			})
			s.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
				if w != nil && w.srv != nil {
					w.srv.Close()
				}
				return ctx, nil
			})

			// R-1 / E-1
			s.Step(`^the notes (".*") (?:were|was) created$`, func(list string) error { return w.givenNotes(list) })
			s.Step(`^the user lists the notes$`, func() error { return w.do(http.MethodGet, "/notes", nil) })
			s.Step(`^the notes are returned in the order (".*")$`, func(list string) error { return w.theNotesAreListed(list) })

			// R-2 / E-2, E-3
			s.Step(`^the user creates a note with the text "([^"]*)"$`, func(text string) error {
				return w.do(http.MethodPost, "/notes", gen.NewNote{Text: text})
			})
			s.Step(`^the response status is (\d+)$`, func(code string) error {
				want, err := strconv.Atoi(code)
				if err != nil {
					return fmt.Errorf("bad expected status %q: %w", code, err)
				}
				return w.checkStatus(want)
			})
			s.Step(`^the note has the id (\d+)$`, func(id string) error { return w.theNoteHasID(id) })

			// R-3 / E-4 uses "the user creates a note with the text" and "the response status is".
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../features/slice.feature"},
			Strict:   true,
			TestingT: t,
		},
	}

	if status := suite.Run(); status != 0 {
		t.Fatalf("godog suite failed with status %d", status)
	}
}
