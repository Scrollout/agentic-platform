package main

// Tests for T038b: slices survive an engine restart. The engine keeps its slices in the run
// store's volume (ENGINE_STORE_DIR), so a second setup on the same directory serves what the
// first one did: the same parts, hashes, checks, questions, answers, approvals, history and
// state, without calling the lint service or the analyst again; the next ID is never one given
// out before; a deleted slice stays deleted; the last approved contract stays the baseline of the
// compatibility check; and a slice record that cannot be read stops the engine (fail closed).
// Black box through setup. The fakes copy the patterns of setup_test.go.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// rsLint is a lint service that finds nothing and counts its calls.
func rsLint(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/lint" || r.Header.Get("Authorization") != "Bearer lint-token" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	t.Cleanup(s.Close)
	return s
}

// rsAnalyst answers with the good notes proposal. With stripAfterFirst, every call after the
// first answers without questions, as a revision after answers does.
func rsAnalyst(t *testing.T, calls *atomic.Int32, stripAfterFirst bool) *httptest.Server {
	t.Helper()
	raw, err := os.ReadFile("../../../tests/evals/analyst/testdata/proposals/good-notes.json")
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	withoutQuestions := map[string]any{}
	for k, v := range p {
		if k != "questions" {
			withoutQuestions[k] = v
		}
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
		n := calls.Add(1)
		body := p
		if stripAfterFirst && n > 1 {
			body = withoutQuestions
		}
		b, _ := json.Marshal(body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		for _, e := range []string{
			ev(map[string]any{"type": "agent_info", "model": "anthropic/claude-haiku-5-5"}),
			ev(map[string]any{"type": "agent_choice", "content": string(b)}),
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

type rsEngine struct {
	t       *testing.T
	h       http.Handler
	closeFn func() error
}

func rsStart(t *testing.T, lint, analyst *httptest.Server, dir string) *rsEngine {
	t.Helper()
	h, closeFn, err := setup(goodEnv(lint.URL, analyst.URL, dir).get)
	if err != nil {
		t.Fatalf("setup on %s: %v", dir, err)
	}
	e := &rsEngine{t: t, h: h, closeFn: closeFn}
	t.Cleanup(func() { closeFn() })
	return e
}

func (e *rsEngine) stop() {
	e.t.Helper()
	if err := e.closeFn(); err != nil {
		e.t.Fatalf("closing the engine: %v", err)
	}
}

func rsURL(path string) string { return path + "?api-version=" + apiVersion }

// op sends a write and follows its operation to the end; it returns the operation.
func (e *rsEngine) op(method, path, body string) struct {
	Status string
	Result map[string]string
	Error  map[string]any
} {
	e.t.Helper()
	w := rsDo(e.t, e.h, method, rsURL(path), body)
	if w.Code != http.StatusAccepted {
		e.t.Fatalf("%s %s = %d: %s", method, path, w.Code, w.Body)
	}
	var op struct {
		ID, Status string
		Result     map[string]string
		Error      map[string]any
	}
	if err := json.Unmarshal(w.Body.Bytes(), &op); err != nil {
		e.t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for op.Status != "succeeded" && op.Status != "failed" {
		if op.Status == "cancelled" || time.Now().After(deadline) {
			e.t.Fatalf("%s %s: operation ended %q", method, path, op.Status)
		}
		time.Sleep(10 * time.Millisecond)
		r := rsDo(e.t, e.h, "GET", rsURL("/operations/"+op.ID), "")
		if r.Code != 200 {
			e.t.Fatalf("getOperation = %d: %s", r.Code, r.Body)
		}
		op = struct {
			ID, Status string
			Result     map[string]string
			Error      map[string]any
		}{}
		if err := json.Unmarshal(r.Body.Bytes(), &op); err != nil {
			e.t.Fatal(err)
		}
	}
	if op.Status != "succeeded" {
		e.t.Fatalf("%s %s failed: %v", method, path, op.Error)
	}
	return struct {
		Status string
		Result map[string]string
		Error  map[string]any
	}{op.Status, op.Result, op.Error}
}

func (e *rsEngine) create() string {
	e.t.Helper()
	op := e.op("POST", "/slices", `{"description":"`+notesText+`"}`)
	id := op.Result["slice"]
	if id == "" {
		e.t.Fatalf("createSlice result %v names no slice", op.Result)
	}
	return id
}

func (e *rsEngine) get(id string) map[string]any {
	e.t.Helper()
	w := rsDo(e.t, e.h, "GET", rsURL("/slices/"+id), "")
	if w.Code != 200 {
		e.t.Fatalf("getSlice %s = %d: %s", id, w.Code, w.Body)
	}
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		e.t.Fatal(err)
	}
	return m
}

func (e *rsEngine) list() map[string]any {
	e.t.Helper()
	w := rsDo(e.t, e.h, "GET", rsURL("/slices"), "")
	if w.Code != 200 {
		e.t.Fatalf("listSlices = %d: %s", w.Code, w.Body)
	}
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		e.t.Fatal(err)
	}
	return m
}

func (e *rsEngine) listedIDs() []string {
	e.t.Helper()
	var ids []string
	items, _ := e.list()["items"].([]any)
	for _, it := range items {
		ids = append(ids, it.(map[string]any)["id"].(string))
	}
	return ids
}

func (e *rsEngine) answer(id string) {
	e.t.Helper()
	e.op("POST", "/slices/"+id+"/answers", `{"answers":[{"question":"Q-1","answer":{"option":0}},{"question":"Q-2","answer":{"text":"400"}}]}`)
}

func (e *rsEngine) approve(id, part string) {
	e.t.Helper()
	parts, _ := e.get(id)["parts"].(map[string]any)
	hash, _ := parts[part].(string)
	if hash == "" {
		e.t.Fatalf("slice %s has no hash for part %s: %v", id, part, parts)
	}
	e.op("POST", "/slices/"+id+"/approvals", `{"part":"`+part+`","content_hash":"`+hash+`"}`)
}

func rsSetup(t *testing.T, stripAfterFirst bool) (lint, analyst *httptest.Server, lintCalls, analystCalls *atomic.Int32, dir string) {
	t.Helper()
	lintCalls, analystCalls = new(atomic.Int32), new(atomic.Int32)
	return rsLint(t, lintCalls), rsAnalyst(t, analystCalls, stripAfterFirst), lintCalls, analystCalls, t.TempDir()
}

func TestASliceReadsBackIdenticallyAfterARestartWithoutCallingLintOrTheAnalyst(t *testing.T) {
	lint, analyst, lintCalls, analystCalls, dir := rsSetup(t, true)
	e := rsStart(t, lint, analyst, dir)
	id := e.create()
	e.answer(id)
	e.approve(id, "goals")
	e.approve(id, "contract")
	e.op("PUT", "/slices/"+id+"/goals", `{"goals":[{"id":"G-1","text":"Keep notes for good"},{"id":"G-2","text":"Read notes back"}]}`)

	before, listBefore := e.get(id), e.list()
	for _, field := range []string{"parts", "checks", "questions", "approvals", "history", "state"} {
		if v, ok := before[field]; !ok || v == nil {
			t.Fatalf("before the restart the slice has no %s: %v", field, before)
		}
	}
	if as, _ := before["approvals"].([]any); len(as) != 1 {
		t.Fatalf("want only the contract approval to hold after the goals change, got %v", before["approvals"])
	}
	lintBefore, analystBefore := lintCalls.Load(), analystCalls.Load()

	e.stop()
	e2 := rsStart(t, lint, analyst, dir)
	if after := e2.get(id); !reflect.DeepEqual(before, after) {
		b, _ := json.MarshalIndent(before, "", " ")
		a, _ := json.MarshalIndent(after, "", " ")
		t.Errorf("the slice differs after the restart\nbefore: %s\nafter: %s", b, a)
	}
	if after := e2.list(); !reflect.DeepEqual(listBefore, after) {
		t.Errorf("listSlices differs after the restart: %v vs %v", listBefore, after)
	}
	if n := lintCalls.Load(); n != lintBefore {
		t.Errorf("the lint service was called %d more times by the restart and the reads", n-lintBefore)
	}
	if n := analystCalls.Load(); n != analystBefore {
		t.Errorf("the analyst was called %d more times by the restart and the reads", n-analystBefore)
	}
}

func TestAnApprovalStillHoldsAfterARestartAndAChangeStillWithdrawsIt(t *testing.T) {
	lint, analyst, _, _, dir := rsSetup(t, true)
	e := rsStart(t, lint, analyst, dir)
	id := e.create()
	e.answer(id)
	e.approve(id, "goals")
	e.stop()

	e2 := rsStart(t, lint, analyst, dir)
	if as, _ := e2.get(id)["approvals"].([]any); len(as) != 1 {
		t.Fatalf("approvals after the restart: %v, want the goals approval", as)
	}
	e2.op("PUT", "/slices/"+id+"/goals", `{"goals":[{"id":"G-1","text":"Other"},{"id":"G-2","text":"Read notes back"}]}`)
	if as, _ := e2.get(id)["approvals"].([]any); len(as) != 0 {
		t.Errorf("a change to the goals after the restart left approvals %v", as)
	}
}

func TestSliceIDsAreNeverReusedAfterARestartAndDeletedSlicesStayDeleted(t *testing.T) {
	lint, analyst, _, _, dir := rsSetup(t, false)
	e := rsStart(t, lint, analyst, dir)
	ids := []string{e.create(), e.create(), e.create()}
	if !reflect.DeepEqual(ids, []string{"S-1", "S-2", "S-3"}) {
		t.Fatalf("ids %v", ids)
	}
	e.op("DELETE", "/slices/S-1", "")
	e.op("DELETE", "/slices/S-3", "") // the highest: a restart must not hand S-3 out again
	e.stop()

	e2 := rsStart(t, lint, analyst, dir)
	for _, gone := range []string{"S-1", "S-3"} {
		if w := rsDo(t, e2.h, "GET", rsURL("/slices/"+gone), ""); w.Code != http.StatusNotFound {
			t.Errorf("getSlice %s after the restart = %d, want 404", gone, w.Code)
		}
	}
	if got := e2.listedIDs(); !reflect.DeepEqual(got, []string{"S-2"}) {
		t.Errorf("listSlices after the restart = %v, want [S-2]", got)
	}
	if id := e2.create(); id != "S-4" {
		t.Errorf("the next slice after the restart is %s, want S-4", id)
	}
}

func TestTheApprovedContractStaysTheBaselineAfterARestart(t *testing.T) {
	lint, analyst, _, _, dir := rsSetup(t, true)
	e := rsStart(t, lint, analyst, dir)
	id := e.create()
	e.answer(id)
	e.approve(id, "contract")
	e.stop()

	breaking, err := os.ReadFile("../../slices/testdata/contract-notes-v2-breaking.yaml")
	if err != nil {
		t.Fatal(err)
	}
	e2 := rsStart(t, lint, analyst, dir)
	body, _ := json.Marshal(map[string]string{"contract": string(breaking)})
	e2.op("PUT", "/slices/"+id+"/contract", string(body))
	failed := false
	checks, _ := e2.get(id)["checks"].([]any)
	for _, c := range checks {
		m := c.(map[string]any)
		if m["check"] == "ContractCompatibility" && m["part"] == "contract" && m["passed"] == false {
			failed = true
		}
	}
	if !failed {
		t.Errorf("a breaking change after the restart did not fail ContractCompatibility: %v", checks)
	}
	if as, _ := e2.get(id)["approvals"].([]any); len(as) != 0 {
		t.Errorf("the contract approval still holds after a breaking change: %v", as)
	}
}

// recordFiles are the files outside artifacts/ that hold something of the slice.
func recordFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() && rel == "artifacts" {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), "Keep notes") {
				out = append(out, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestACorruptOrUnreadableSliceRecordStopsSetupNamingTheProblem(t *testing.T) {
	ruin := map[string]func(t *testing.T, path string){
		"garbage": func(t *testing.T, p string) {
			if err := os.WriteFile(p, []byte("\x00not a slice{{{"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"truncated": func(t *testing.T, p string) {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, b[:len(b)/2], 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"empty": func(t *testing.T, p string) {
			if err := os.WriteFile(p, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"unreadable": func(t *testing.T, p string) { // a directory where the file was cannot be read as one
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(p, 0o700); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, f := range ruin {
		t.Run(name, func(t *testing.T) {
			lint, analyst, _, _, dir := rsSetup(t, false)
			e := rsStart(t, lint, analyst, dir)
			e.create()
			e.stop()
			files := recordFiles(t, dir)
			if len(files) == 0 {
				t.Fatalf("no file in %s holds the slice after it was created: nothing is persisted", dir)
			}
			for _, p := range files {
				f(t, p)
			}
			h, closeFn, err := setup(goodEnv(lint.URL, analyst.URL, dir).get)
			if err == nil {
				closeFn()
				t.Fatalf("setup started (handler %v) over a %s slice record; it must fail closed", h != nil, name)
			}
			if h != nil || closeFn != nil {
				t.Errorf("setup returned a handler or close function with its error")
			}
			if msg := strings.ToLower(err.Error()); !strings.Contains(msg, "slice") {
				t.Errorf("the error %q does not name the slice record as the problem", err)
			}
		})
	}
}

// rsDo is do that waits and retries, bounded, when the engine's rate limit answers 429, honouring
// Retry-After (seconds); any other answer, and the last 429, is returned as it is.
func rsDo(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		w := do(t, h, method, path, body)
		if w.Code != http.StatusTooManyRequests || time.Now().After(deadline) {
			return w
		}
		wait := time.Second
		if n, err := strconv.Atoi(w.Header().Get("Retry-After")); err == nil && n >= 0 && n <= 5 {
			wait = time.Duration(n) * time.Second
		}
		time.Sleep(wait + 50*time.Millisecond)
	}
}
