package main

// Tests for the engine's wiring (T038g engine part, T017b): setup reads the environment, embeds the
// contract's schemas, the pattern and the analyst definition, and serves the slice operations
// against the lint and analyst services (here httptest fakes imitating POST /lint and the
// Docker Agent serve-api endpoints).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	apiVersion = "2026-11-12-preview"
	notesText  = "An in-memory notes service. Listing returns all notes in creation order. " +
		"Creating a note needs non-empty text and returns it with an id starting at 1."
	clientTok = "client-token"
)

func fakeLint(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/lint" || r.Header.Get("Authorization") != "Bearer lint-token" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	t.Cleanup(s.Close)
	return s
}

func fakeAnalyst(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	proposal, err := os.ReadFile("../../../tests/evals/analyst/testdata/proposals/good-notes.json")
	if err != nil {
		t.Fatal(err)
	}
	ev := func(v any) string {
		b, _ := json.Marshal(v)
		return "data: " + string(b) + "\n\n"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"sess-1","title":"x"}`)
	})
	mux.HandleFunc("/api/sessions/sess-1/agent/analyst", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer analyst-token" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		for _, e := range []string{
			ev(map[string]any{"type": "agent_info", "model": "anthropic/claude-haiku-5-5"}),
			ev(map[string]any{"type": "agent_choice", "content": string(proposal)}),
			ev(map[string]any{"type": "stream_stopped", "reason": "normal"}),
		} {
			fmt.Fprint(w, e)
			w.(http.Flusher).Flush()
		}
	})
	mux.HandleFunc("/api/sessions/sess-1", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

type env map[string]string

func (e env) get(k string) string { return e[k] }

func goodEnv(lint, analyst, dir string) env {
	return env{
		"ENGINE_LISTEN_ADDRESS": ":0",
		"ENGINE_CLIENT_TOKEN":   clientTok, "ENGINE_PAGE_TOKEN": "page-token", "ENGINE_WORKER_TOKEN": "worker-token",
		"AP_LINT_URL": lint, "AP_LINT_TOKEN": "lint-token",
		"AP_ANALYST_URL": analyst, "AP_ANALYST_TOKEN": "analyst-token",
		"ENGINE_STORE_DIR": dir,
	}
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+clientTok)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestSetupRefusesAMissingOrEmptyVariableNamingIt(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"ENGINE_LISTEN_ADDRESS", "ENGINE_CLIENT_TOKEN", "ENGINE_PAGE_TOKEN", "ENGINE_WORKER_TOKEN",
		"AP_LINT_URL", "AP_LINT_TOKEN", "AP_ANALYST_URL", "AP_ANALYST_TOKEN", "ENGINE_STORE_DIR",
	} {
		for _, how := range []string{"missing", "empty"} {
			e := goodEnv("http://lint:8080", "http://analyst:8080", dir)
			if how == "missing" {
				delete(e, name)
			} else {
				e[name] = ""
			}
			h, closeFn, err := setup(e.get)
			if err == nil {
				if closeFn != nil {
					closeFn()
				}
				t.Errorf("%s %s: setup succeeded, want an error", name, how)
				continue
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("%s %s: error %q does not name the variable", name, how, err)
			}
			if h != nil {
				t.Errorf("%s %s: handler returned with an error", name, how)
			}
		}
	}
}

func TestSetupRefusesTokensThatAreNotAllDistinct(t *testing.T) {
	dir := t.TempDir()
	names := []string{"ENGINE_CLIENT_TOKEN", "ENGINE_PAGE_TOKEN", "ENGINE_WORKER_TOKEN", "AP_LINT_TOKEN", "AP_ANALYST_TOKEN"}
	for i, a := range names {
		for _, b := range names[i+1:] {
			e := goodEnv("http://lint:8080", "http://analyst:8080", dir)
			e[b] = e[a]
			if _, closeFn, err := setup(e.get); err == nil {
				if closeFn != nil {
					closeFn()
				}
				t.Errorf("%s = %s: setup succeeded, want an error", a, b)
			}
		}
	}
}

func TestSetupRefusesAStoreDirectoryThatDoesNotExist(t *testing.T) {
	e := goodEnv("http://lint:8080", "http://analyst:8080", t.TempDir()+"/missing")
	if _, closeFn, err := setup(e.get); err == nil {
		if closeFn != nil {
			closeFn()
		}
		t.Fatal("setup succeeded for a store directory that does not exist")
	}
}

// createNotes runs createSlice with the notes example and returns the slice as getSlice gives it.
func createNotes(t *testing.T, h http.Handler) map[string]any {
	t.Helper()
	w := do(t, h, "POST", "/slices?api-version="+apiVersion, `{"description":"`+notesText+`"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("createSlice = %d: %s", w.Code, w.Body)
	}
	var op struct {
		ID, Status string
		Result     map[string]string
		Error      map[string]any
	}
	if err := json.Unmarshal(w.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for op.Status != "succeeded" {
		if op.Status == "failed" || op.Status == "cancelled" || time.Now().After(deadline) {
			t.Fatalf("operation ended %q: %v", op.Status, op.Error)
		}
		time.Sleep(10 * time.Millisecond)
		r := do(t, h, "GET", "/operations/"+op.ID+"?api-version="+apiVersion, "")
		if r.Code != 200 {
			t.Fatalf("getOperation = %d: %s", r.Code, r.Body)
		}
		if err := json.Unmarshal(r.Body.Bytes(), &op); err != nil {
			t.Fatal(err)
		}
	}
	id := op.Result["slice"]
	if id == "" {
		t.Fatalf("operation result %v names no slice", op.Result)
	}
	r := do(t, h, "GET", "/slices/"+id+"?api-version="+apiVersion, "")
	if r.Code != 200 {
		t.Fatalf("getSlice = %d: %s", r.Code, r.Body)
	}
	var sl map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &sl); err != nil {
		t.Fatal(err)
	}
	return sl
}

func proposedDigest(t *testing.T, sl map[string]any) string {
	t.Helper()
	hist, _ := sl["history"].([]any)
	for _, h := range hist {
		if m, _ := h.(map[string]any); m["kind"] == "proposed" {
			if _, ok := m["model"].(string); !ok || m["model"] == "" {
				t.Errorf("the proposed entry names no model: %v", m)
			}
			d, _ := m["agent_digest"].(string)
			return d
		}
	}
	t.Fatalf("no proposed history entry: %v", hist)
	return ""
}

func TestSetupServesSliceOperationsWithEmbeddedSchemaPatternAndAnalyst(t *testing.T) {
	var calls atomic.Int32
	lint, analyst := fakeLint(t), fakeAnalyst(t, &calls)
	dir := t.TempDir()
	h, closeFn, err := setup(goodEnv(lint.URL, analyst.URL, dir).get)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeFn() })

	sl := createNotes(t, h)
	if n := calls.Load(); n != 1 {
		t.Errorf("analyst called %d times, want 1", n)
	}
	if sl["artifact_count"] != float64(0) {
		t.Errorf("artifact_count = %v, want 0", sl["artifact_count"])
	}
	checks, _ := sl["checks"].([]any)
	pat, err := os.ReadFile("../../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Conditions []string `yaml:"conditions"`
	}
	if err := yaml.Unmarshal(pat, &p); err != nil || len(p.Conditions) == 0 {
		t.Fatalf("pattern conditions: %v %v", p.Conditions, err)
	}
	seen := map[string]bool{}
	for _, c := range checks {
		m := c.(map[string]any)
		name, _ := m["check"].(string)
		if m["part"] == "contract" && m["passed"] != true {
			t.Errorf("a contract check failed: %v", m)
		}
		if m["passed"] == true {
			seen[name] = true
		}
	}
	for _, name := range append([]string{"ContractLint"}, p.Conditions...) {
		if !seen[name] {
			t.Errorf("no passed %s check in %v", name, checks)
		}
	}
	d := proposedDigest(t, sl)
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(d) {
		t.Errorf("agent_digest = %q, want 64 lowercase hex characters (SHA-256)", d)
	}

	// A second engine over the same store directory starts, and names the same analyst definition.
	closeFn()
	h2, close2, err := setup(goodEnv(lint.URL, analyst.URL, dir).get)
	if err != nil {
		t.Fatalf("second setup on the same store directory: %v", err)
	}
	t.Cleanup(func() { close2() })
	if d2 := proposedDigest(t, createNotes(t, h2)); d2 != d {
		t.Errorf("agent digest differs between starts of the same engine: %s vs %s", d, d2)
	}
}

func TestSetupDoesNotServeTheSkeleton(t *testing.T) {
	var calls atomic.Int32
	h, closeFn, err := setup(goodEnv(fakeLint(t).URL, fakeAnalyst(t, &calls).URL, t.TempDir()).get)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeFn() })
	// With slices wired, an unknown slice is NotFound and lists are served by the service.
	if w := do(t, h, "GET", "/slices/S-99?api-version="+apiVersion, ""); w.Code != http.StatusNotFound {
		t.Errorf("getSlice S-99 = %d, want 404", w.Code)
	}
	w := do(t, h, "GET", "/slices?api-version="+apiVersion, "")
	if w.Code != 200 {
		t.Fatalf("listSlices = %d: %s", w.Code, w.Body)
	}
}
