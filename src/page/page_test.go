// T047: failing tests for the read-only run page (FR-024, US2.9, research D12/D18, ADR 0027).
//
// Interface the tests expect (package page):
//
//	type Config struct{ EngineURL, Token, Host string } // Host is the one accepted Host header, e.g. "127.0.0.1:8123"
//	func NewHandler(c Config) http.Handler
//	    GET /runs/{runId}                    run's events in seq order (engine GET /runs/{id}/events, following next_page_token)
//	    GET /compare?run={a}&with={b}        differences (engine GET /runs/{a}/comparison?with={b})
//	  Every engine call is a GET carrying "Authorization: Bearer <Token>" and api-version=2026-11-12-preview.
//	  Any non-GET request to the page gets 405; any other Host header gets 403 (or 421).
//
// Binding to 127.0.0.1 is not tested here: the page listens on :8080 inside its container and the
// page forwarder publishes the port on 127.0.0.1 only; scripts/tests/test_compose.py tests that.
package page_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/agentic-platform/page"
)

const host = "127.0.0.1:8123"

type fakeEngine struct {
	mu      sync.Mutex
	methods []string
	auth    []string
	srv     *httptest.Server
}

func newEngine(t *testing.T) *fakeEngine {
	t.Helper()
	f := &fakeEngine{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.methods = append(f.methods, r.Method)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		if r.Method != http.MethodGet {
			t.Errorf("engine received %s %s; the page may only GET", r.Method, r.URL.Path)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.URL.Query().Get("api-version") != "2026-11-12-preview" {
			t.Errorf("missing api-version on %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/runs/run-a/events":
			if r.URL.Query().Get("page_token") == "" {
				io.WriteString(w, `{"items":[
{"seq":1,"at":"2026-11-12T10:00:00Z","run":"run-a","kind":"RunStarted"},
{"seq":2,"at":"2026-11-12T10:00:01Z","run":"run-a","kind":"StepStarted","step":"plan-step"}],
"next_page_token":"p2"}`)
				return
			}
			io.WriteString(w, `{"items":[
{"seq":3,"at":"2026-11-12T10:00:02Z","run":"run-a","kind":"CheckFailed","step":"plan-step"},
{"seq":4,"at":"2026-11-12T10:00:03Z","run":"run-a","kind":"RunCompleted"}]}`)
		case "/runs/run-a/comparison":
			if r.URL.Query().Get("with") != "run-b" {
				t.Errorf("comparison without with=run-b: %s", r.URL)
			}
			io.WriteString(w, `{"items":[{"step":"build-step","field":"checks","left":"passed","right":"failed: lint broke"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func newPage(t *testing.T) (http.Handler, *fakeEngine) {
	f := newEngine(t)
	return page.NewHandler(page.Config{EngineURL: f.srv.URL, Token: "page-secret", Host: host}), f
}

func do(h http.Handler, method, path, hostHdr string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Host = hostHdr
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRunEventsShownInOrderAcrossPages(t *testing.T) {
	h, _ := newPage(t)
	w := do(h, "GET", "/runs/run-a", host)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	last := -1
	for _, k := range []string{"RunStarted", "StepStarted", "CheckFailed", "RunCompleted"} {
		i := strings.Index(body, k)
		if i < 0 || i < last {
			t.Fatalf("%s missing or out of order in page:\n%s", k, body)
		}
		last = i
	}
	if !strings.Contains(body, "plan-step") {
		t.Error("step name not shown")
	}
}

func TestComparisonShowsDifferences(t *testing.T) {
	h, _ := newPage(t)
	w := do(h, "GET", "/compare?run=run-a&with=run-b", host)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	for _, s := range []string{"build-step", "checks", "passed", "failed: lint broke"} {
		if !strings.Contains(w.Body.String(), s) {
			t.Errorf("comparison page lacks %q", s)
		}
	}
}

func TestOnlyGetReachesEngineAndPageRefusesOtherMethods(t *testing.T) {
	h, f := newPage(t)
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		w := do(h, m, "/runs/run-a", host)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status %d, want 405", m, w.Code)
		}
	}
	do(h, "GET", "/runs/run-a", host)
	do(h, "GET", "/compare?run=run-a&with=run-b", host)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.methods) == 0 {
		t.Fatal("no engine calls recorded")
	}
	for i, m := range f.methods {
		if m != "GET" {
			t.Errorf("engine call %d was %s", i, m)
		}
		if f.auth[i] != "Bearer page-secret" {
			t.Errorf("engine call %d auth %q", i, f.auth[i])
		}
	}
}

func TestUnexpectedHostRejectedBeforeEngineCall(t *testing.T) {
	h, f := newPage(t)
	for _, bad := range []string{"evil.example:8123", "127.0.0.1:9999", "127.0.0.1", "localhost:9999", "attacker.test", ""} {
		w := do(h, "GET", "/runs/run-a", bad)
		if w.Code != http.StatusForbidden && w.Code != http.StatusMisdirectedRequest {
			t.Errorf("Host %q: status %d, want 403/421", bad, w.Code)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.methods) != 0 {
		t.Errorf("engine was called %d times for rejected hosts", len(f.methods))
	}
}

func TestLocalhostHostAccepted(t *testing.T) {
	h, _ := newPage(t)
	if w := do(h, "GET", "/runs/run-a", "localhost:8123"); w.Code != 200 {
		t.Errorf("localhost:8123 status %d", w.Code)
	}
}

func TestNoFormOrStateChangingLink(t *testing.T) {
	h, _ := newPage(t)
	for _, p := range []string{"/runs/run-a", "/compare?run=run-a&with=run-b"} {
		b := strings.ToLower(do(h, "GET", p, host).Body.String())
		for _, bad := range []string{"<form", "<input", "<button", "<textarea", "<select", "method=\"post\"", "onclick", "onsubmit", "/resume", "/replay", "/hand-over", "/cancel", "/approvals"} {
			if strings.Contains(b, bad) {
				t.Errorf("%s contains %q", p, bad)
			}
		}
	}
}
