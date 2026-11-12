package analyst_test

// T038f: the engine's client of the analyst service (Docker Agent `serve api`), against an httptest
// server that imitates its endpoints and SSE events (research D27).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/analyst"
	"github.com/agentic-platform/engine/slices"
)

const token = "tok-123"

type fake struct {
	mu       sync.Mutex
	events   []string // raw SSE lines of the run stream, each already "data: ...\n\n"
	status   int      // run status, 0 = 200
	create   int      // create status, 0 = 200
	hold     chan struct{}
	creates  []map[string]any
	runBody  map[string]any
	runCalls int
	deleted  []string
	auths    []string
	ctypes   []string
}

func ev(v any) string {
	b, _ := json.Marshal(v)
	return "data: " + string(b) + "\n\n"
}

func okEvents(content string) []string {
	return []string{
		ev(map[string]any{"type": "session_title", "title": "t"}),
		ev(map[string]any{"type": "agent_info", "model": "anthropic/claude-haiku-5-5"}),
		ev(map[string]any{"type": "agent_choice", "content": content}),
		ev(map[string]any{"type": "stream_stopped", "reason": "normal"}),
	}
}

func (f *fake) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		f.ctypes = append(f.ctypes, r.Header.Get("Content-Type"))
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		f.creates = append(f.creates, m)
		if f.create != 0 {
			http.Error(w, "no", f.create)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"sess-1","title":"x"}`)
	})
	mux.HandleFunc("/api/sessions/sess-1/agent/analyst", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.runCalls++
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		f.ctypes = append(f.ctypes, r.Header.Get("Content-Type"))
		json.NewDecoder(r.Body).Decode(&f.runBody)
		events, status, hold := f.events, f.status, f.hold
		f.mu.Unlock()
		if status != 0 {
			http.Error(w, "no", status)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl := w.(http.Flusher)
		fmt.Fprint(w, ": ping\n\n")
		for _, e := range events {
			fmt.Fprint(w, e)
			fl.Flush()
		}
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
			}
		}
	})
	mux.HandleFunc("/api/sessions/sess-1", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		if r.Method == http.MethodDelete {
			f.deleted = append(f.deleted, r.URL.Path)
		}
		w.WriteHeader(200)
	})
	return mux
}

func setup(t *testing.T, f *fake) *analyst.HTTP {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return analyst.NewHTTP(srv.URL, token, "sha256:digest")
}

const proposalJSON = `{"goals":[{"id":"G-1","text":"Keep notes"}],"terms":[],"rules":[],"examples":"Feature: x","contract":"openapi: 3.1.0"}`

var in = slices.AnalystInput{Description: "An in-memory notes service."}

func TestProposeHappyPath(t *testing.T) {
	f := &fake{events: okEvents(proposalJSON)}
	a := setup(t, f)
	p, err := a.Propose(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if p.Model != "anthropic/claude-haiku-5-5" || p.AgentDigest != "sha256:digest" {
		t.Errorf("model/digest = %q / %q", p.Model, p.AgentDigest)
	}
	if len(p.Goals) != 1 || p.Goals[0].ID != "G-1" || p.Contract != "openapi: 3.1.0" || p.Examples != "Feature: x" {
		t.Errorf("proposal = %+v", p)
	}
	if len(f.creates) != 1 {
		t.Fatalf("creates = %d", len(f.creates))
	}
	if title, _ := f.creates[0]["title"].(string); title == "" {
		t.Errorf("session title must be non-empty (an untitled session costs a second model call): %v", f.creates[0])
	}
	if f.creates[0]["safety_policy"] != "restricted" {
		t.Errorf("safety_policy = %v", f.creates[0]["safety_policy"])
	}
	msgs, _ := f.runBody["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %v", f.runBody)
	}
	m := msgs[0].(map[string]any)
	if m["role"] != "user" || m["content"] != slices.AnalystMessage(in) {
		t.Errorf("message = %v", m)
	}
	for i, a := range f.auths {
		if a != "Bearer "+token {
			t.Errorf("request %d Authorization = %q", i, a)
		}
	}
	for i, c := range f.ctypes {
		if c != "application/json" {
			t.Errorf("request %d Content-Type = %q", i, c)
		}
	}
	if len(f.deleted) != 1 || f.deleted[0] != "/api/sessions/sess-1" {
		t.Errorf("deleted = %v", f.deleted)
	}
}

func TestProposeSendsTheRetryMessage(t *testing.T) {
	f := &fake{events: okEvents(proposalJSON)}
	a := setup(t, f)
	retry := slices.AnalystInput{Description: "d", Previous: &slices.Proposal{}, Errors: []string{"bad"}}
	if _, err := a.Propose(context.Background(), retry); err != nil {
		t.Fatal(err)
	}
	got := f.runBody["messages"].([]any)[0].(map[string]any)["content"]
	if got != slices.AnalystMessage(retry) {
		t.Errorf("content = %v", got)
	}
}

func TestProposeUsesTheLastAgentChoice(t *testing.T) {
	f := &fake{events: []string{
		ev(map[string]any{"type": "agent_info", "model": "m1"}),
		ev(map[string]any{"type": "agent_choice", "content": "not json at all"}),
		ev(map[string]any{"type": "agent_choice", "content": proposalJSON}),
		ev(map[string]any{"type": "stream_stopped", "reason": "normal"}),
	}}
	p, err := setup(t, f).Propose(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if p.Model != "m1" || len(p.Goals) != 1 {
		t.Errorf("p = %+v", p)
	}
}

func TestProposeReadsLongLines(t *testing.T) {
	big := strings.Repeat("x", 200_000)
	content, _ := json.Marshal(map[string]any{"goals": []any{}, "terms": []any{}, "rules": []any{}, "examples": big, "contract": "c"})
	f := &fake{events: okEvents(string(content))}
	p, err := setup(t, f).Propose(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Examples) != 200_000 {
		t.Errorf("examples length = %d", len(p.Examples))
	}
}

func TestProposeFailures(t *testing.T) {
	info := ev(map[string]any{"type": "agent_info", "model": "m"})
	choice := ev(map[string]any{"type": "agent_choice", "content": proposalJSON})
	stop := func(r string) string { return ev(map[string]any{"type": "stream_stopped", "reason": r}) }
	cases := map[string]*fake{
		"error event": {events: []string{info,
			ev(map[string]any{"type": "error", "error": "model failed: HTTP 404"}), stop("error")}},
		"error event then normal stop": {events: []string{info, choice,
			ev(map[string]any{"type": "error", "error": "boom"}), stop("normal")}},
		"no stream_stopped":   {events: []string{info, choice}},
		"reason error":        {events: []string{info, choice, stop("error")}},
		"reason cancelled":    {events: []string{info, choice, stop("cancelled")}},
		"no agent_choice":     {events: []string{info, stop("normal")}},
		"unparseable content": {events: []string{info, ev(map[string]any{"type": "agent_choice", "content": "{nope"}), stop("normal")}},
		"run 401":             {status: 401},
		"run 409":             {status: 409},
		"run 500":             {status: 500},
		"create 401":          {create: 401},
		"create 500":          {create: 500},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			a := setup(t, f)
			if _, err := a.Propose(context.Background(), in); err == nil {
				t.Fatal("expected an error")
			}
			if f.runCalls > 1 {
				t.Errorf("run was retried: %d calls", f.runCalls)
			}
			if f.create == 0 && len(f.deleted) != 1 {
				t.Errorf("the session must be deleted after a failure too: %v", f.deleted)
			}
		})
	}
}

func TestProposeStopsOnContextCancellation(t *testing.T) {
	f := &fake{events: []string{ev(map[string]any{"type": "agent_info", "model": "m"})}, hold: make(chan struct{})}
	a := setup(t, f)
	defer close(f.hold)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := a.Propose(ctx, in); done <- err }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Propose did not stop after cancellation")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		n := len(f.deleted)
		f.mu.Unlock()
		if n == 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("the session was not deleted after cancellation")
}

var _ slices.Analyst = (*analyst.HTTP)(nil)
var _ = io.EOF
