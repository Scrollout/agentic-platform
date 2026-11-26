package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/cucumber/godog"
)

type world struct {
	srv    *httptest.Server
	status int
	body   map[string]any
}

// The limit of a text is 200; one character over it is 201.
const textLimit, overLimit = 200, 201

var quoted = regexp.MustCompile(`"([^"]*)"`)

func (w *world) create(text string) error {
	b, _ := json.Marshal(map[string]string{"text": text})
	resp, err := http.Post(w.srv.URL+"/notes", "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	w.status, w.body = resp.StatusCode, nil
	_ = json.NewDecoder(resp.Body).Decode(&w.body)
	return nil
}

func (w *world) list() ([]string, error) {
	resp, err := http.Get(w.srv.URL + "/notes")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var notes []struct{ Text string }
	if err := json.NewDecoder(resp.Body).Decode(&notes); err != nil {
		return nil, fmt.Errorf("the list is not a JSON array of notes (status %d): %w", resp.StatusCode, err)
	}
	var texts []string
	for _, n := range notes {
		texts = append(texts, n.Text)
	}
	return texts, nil
}

func initScenario(sc *godog.ScenarioContext) {
	w := &world{}
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		w.srv, w.status, w.body = httptest.NewServer(handler()), 0, nil
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.srv.Close()
		return ctx, nil
	})
	sc.Step(`^the notes? (.+) w(?:as|ere) created$`, func(list string) error {
		for _, m := range quoted.FindAllStringSubmatch(list, -1) {
			if err := w.create(m[1]); err != nil {
				return err
			}
		}
		return nil
	})
	sc.Step(`^the user lists the notes$`, func() error { _, err := w.list(); return err })
	sc.Step(`^the notes are returned in the order (.+)$`, func(list string) error {
		got, err := w.list()
		if err != nil {
			return err
		}
		var want []string
		for _, m := range quoted.FindAllStringSubmatch(list, -1) {
			want = append(want, m[1])
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			return fmt.Errorf("the notes are %q, want %q", got, want)
		}
		return nil
	})
	sc.Step(`^the user creates a note with the text "([^"]*)"$`, w.create)
	sc.Step(`^the user creates a note with a text of (\d+) characters$`, func(n string) error {
		k, _ := strconv.Atoi(n)
		return w.create(strings.Repeat("x", k))
	})
	sc.Step(`^the response status is (\d+)$`, func(s string) error {
		if n, _ := strconv.Atoi(s); w.status != n {
			return fmt.Errorf("the status is %d, want %d", w.status, n)
		}
		return nil
	})
	sc.Step(`^the note has the id (\d+)$`, func(s string) error {
		if got := fmt.Sprint(w.body["id"]); got != s {
			return fmt.Errorf("the note has the id %s, want %s", got, s)
		}
		return nil
	})
}

func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initScenario,
		Options:             &godog.Options{Format: "pretty", Paths: []string{"../features"}, TestingT: t, Strict: true},
	}
	if suite.Run() != 0 {
		t.Fatal("godog: at least one scenario failed")
	}
}
