package main

// T053e regression (fresh review, 2026-11-14): the engine as setup wires it calls the project
// worker at AP_PROJECT_URL with AP_PROJECT_TOKEN. Through setup's real wiring, a run of the notes
// slice reaches its first restoreStep (contract-check, attempt 1) at an httptest project worker,
// which must receive `Authorization: Bearer <AP_PROJECT_TOKEN>` and the contract's RestoreRequest.
// Guards against: cfg.History not set in setup, the History not handed to the run's machine
// (api/runs.go machineFor), and another token (AP_CHECK_TOKEN) sent to the project worker.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type projectCall struct {
	path, query, auth, ctype string
	body                     map[string]any
}

// fakeProjectWorker answers every restoreStep with the folder holding `before`, and records it.
func fakeProjectWorker(t *testing.T) (*httptest.Server, func() []projectCall) {
	var mu sync.Mutex
	var calls []projectCall
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		mu.Lock()
		calls = append(calls, projectCall{r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), m})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/project-versions/restore" {
			before := m["before"]
			if before == nil {
				before = []any{}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"restored": []string{}, "project_files": before})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": "x", "status": 500, "code": "WriteFailed"})
	}))
	t.Cleanup(s.Close)
	return s, func() []projectCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]projectCall(nil), calls...)
	}
}

// writeGrant writes a step worker's grant as `ap build-workers` does.
func writeGrant(t *testing.T, dir, token, slice, step, callToken string) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"slice": slice, "step": step, "call_token": callToken})
	s := sha256.Sum256([]byte(token))
	if err := os.WriteFile(filepath.Join(dir, hex.EncodeToString(s[:])), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTheEngineCallsTheProjectWorkerWithItsTokenAndTheContractsRestoreRequest(t *testing.T) {
	lint, analyst, _, _, dir := rsSetup(t, true)
	pw, calls := fakeProjectWorker(t)
	// every step worker refuses its call, so the run stops after contract-check's attempts
	stepWorker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	t.Cleanup(stepWorker.Close)
	grants := t.TempDir()

	e := goodEnv(lint.URL, analyst.URL, dir)
	e["AP_PROJECT_URL"], e["AP_PROJECT_TOKEN"] = pw.URL, "the-project-token"
	e["ENGINE_GRANTS_DIR"] = grants
	h, closeFn, err := setupWith(e.get, testDurable(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeFn() })
	eng := &rsEngine{t: t, h: h, closeFn: closeFn}

	id := eng.create()
	eng.answer(id)
	for _, part := range []string{"goals", "rules", "contract"} {
		eng.approve(id, part)
	}
	for _, step := range []string{"contract-check", "generate", "tests", "code", "review"} {
		tok := "worker-" + step
		writeGrant(t, grants, tok, id, step, "call-"+step)
		req := httptest.NewRequest("POST", rsURL("/workers"), strings.NewReader(`{"endpoint":"`+strings.Replace(stepWorker.URL, "127.0.0.1", "localhost", 1)+`"}`))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code/100 != 2 {
			t.Fatalf("registerWorker %s = %d: %s", step, w.Code, w.Body)
		}
	}
	if w := rsDo(t, h, "POST", rsURL("/slices/"+id+"/runs"), `{"project":"notes-app"}`); w.Code/100 != 2 {
		t.Fatalf("startRun = %d: %s", w.Code, w.Body)
	}

	var first projectCall
	deadline := time.Now().Add(15 * time.Second)
	for {
		if cs := calls(); len(cs) > 0 {
			first = cs[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no call reached the project worker at AP_PROJECT_URL")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if first.path != "/project-versions/restore" || first.query != "api-version=2026-11-13-preview" {
		t.Fatalf("first call = %s?%s, want restoreStep", first.path, first.query)
	}
	if first.auth != "Bearer the-project-token" {
		t.Fatalf("Authorization = %q, want the AP_PROJECT_TOKEN", first.auth)
	}
	if !strings.HasPrefix(first.ctype, "application/json") {
		t.Fatalf("Content-Type = %q", first.ctype)
	}
	b := first.body
	step, _ := b["step"].(map[string]any)
	before, isList := b["before"].([]any)
	limit, _ := b["time_limit_seconds"].(float64)
	switch {
	case !strings.HasPrefix(asString(b["run"]), "RUN-") || b["project"] != "notes-app" || b["attempt"] != float64(1):
		t.Fatalf("body = %v, want a RUN-<n> of project notes-app, attempt 1", b)
	case step["name"] != "contract-check" || step["writable"] == nil || step["outputs"] == nil || step["dependencies"] == nil:
		t.Fatalf("step = %v, want contract-check's StepFacts", step)
	case !isList || len(before) != 0:
		t.Fatalf("before = %#v, want an empty list", b["before"])
	case b["round"] != nil:
		t.Fatalf("round = %v, want none", b["round"])
	case limit < 1 || limit > 600:
		t.Fatalf("time_limit_seconds = %v, want 1 to 600", b["time_limit_seconds"])
	}
	for k := range b {
		switch k {
		case "run", "step", "attempt", "round", "project", "before", "time_limit_seconds":
		default:
			t.Errorf("restoreStep body has %q, which RestoreRequest does not allow", k)
		}
	}
}

func asString(v any) string { s, _ := v.(string); return s }
