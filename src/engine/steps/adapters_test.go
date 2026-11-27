package steps_test

// T054c: the engine's adapters to the step workers (Docker Agent `serve api`, procedure workers)
// and to the check service, against httptest fakes (research D27, D29, the check contract).

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/steps"
)

const (
	agentTok  = "agent-tok-1"
	procTok   = "proc-tok-2"
	checkTok  = "check-tok-3"
	apiVer    = "2026-11-13-preview"
	pinned    = "anthropic/claude-haiku-5-5"
	hashA     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB     = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hashC     = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	hashD     = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	testsStep = "tests"
)

func hex256(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

type endpoints map[string][2]string // step -> url, token

func (e endpoints) Endpoint(slice, project, step string) (string, string, bool) {
	v, ok := e[step]
	return v[0], v[1], ok
}

// recorder keeps what a fake received.
type recorder struct {
	mu    sync.Mutex
	calls []call
}
type call struct {
	method, path, query, auth, ctype string
	body                             map[string]any
}

func (r *recorder) add(req *http.Request) map[string]any {
	var m map[string]any
	b, _ := io.ReadAll(req.Body)
	_ = json.Unmarshal(b, &m)
	r.mu.Lock()
	r.calls = append(r.calls, call{req.Method, req.URL.Path, req.URL.RawQuery, req.Header.Get("Authorization"), req.Header.Get("Content-Type"), m})
	r.mu.Unlock()
	return m
}
func (r *recorder) all() []call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]call(nil), r.calls...)
}
func (r *recorder) find(method, path string) []call {
	var out []call
	for _, c := range r.all() {
		if c.method == method && c.path == path {
			out = append(out, c)
		}
	}
	return out
}

var approvedParts = map[string][]byte{
	"goals":    []byte(`{"goals":["g"]}`),
	"rules":    []byte(`{"rules":["r"]}`),
	"contract": []byte(`{"contract":"c"}`),
}

func sum(b []byte) string { return hex256(b) }

func toFiles(p []map[string]string) []machine.File {
	out := []machine.File{}
	for _, f := range p {
		out = append(out, machine.File{Path: f["path"], SHA256: f["sha256"]})
	}
	return out
}

func sse(v any) string { b, _ := json.Marshal(v); return "data: " + string(b) + "\n\n" }

// agentFake imitates `serve api` and the check service's readStepFiles on one server each.
type agentFake struct {
	rec    recorder
	model  string
	hung   bool
	srv    *httptest.Server
	events []string
}

func newAgent(t *testing.T, model string) *agentFake {
	f := &agentFake{model: model}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		f.rec.add(r)
		waitIfHung(f.hung, r)
		fmt.Fprint(w, `{"id":"sess-1"}`)
	})
	mux.HandleFunc("/api/sessions/sess-1", func(w http.ResponseWriter, r *http.Request) { f.rec.add(r) })
	mux.HandleFunc("/api/sessions/sess-1/agent/"+testsStep, func(w http.ResponseWriter, r *http.Request) {
		f.rec.add(r)
		evs := f.events
		if evs == nil {
			evs = []string{
				sse(map[string]any{"type": "agent_info", "model": f.model}),
				sse(map[string]any{"type": "agent_choice", "content": "done, I think"}),
				sse(map[string]any{"type": "stream_stopped", "reason": "normal"}),
			}
		}
		for _, e := range evs {
			fmt.Fprint(w, e)
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(func() { f.srv.Close() })
	return f
}

// waitIfHung blocks until the client gives up when hung.
func waitIfHung(hung bool, r *http.Request) {
	if hung {
		<-r.Context().Done()
	}
}

// checkFake imitates the check service: readStepFiles, runCheck and runProcedure.
type checkFake struct {
	rec     recorder
	srv     *httptest.Server
	status  int // 0 = 200
	body    string
	hung    bool
	files   []canonical.File
	removed []string
	project []map[string]string
	verdict map[string]any
	proc    map[string]any
}

func pf(path, hash string) map[string]string { return map[string]string{"path": path, "sha256": hash} }

func newCheck(t *testing.T) *checkFake {
	f := &checkFake{
		project: []map[string]string{pf("tests/a_test.go", hashA)},
		files:   []canonical.File{{Path: "tests/a_test.go", Content: []byte("package tests\n")}, {Path: "tests/b.feature", Content: []byte("Feature: b\n")}},
	}
	serve := func(w http.ResponseWriter, r *http.Request, ok map[string]any) {
		f.rec.add(r)
		waitIfHung(f.hung, r)
		if f.status != 0 {
			w.WriteHeader(f.status)
			fmt.Fprint(w, `{"type":"x","title":"x","code":"Busy"}`)
			return
		}
		if f.body != "" {
			fmt.Fprint(w, f.body)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ok)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/step-files", func(w http.ResponseWriter, r *http.Request) {
		var fc []map[string]string
		for _, x := range f.files {
			fc = append(fc, map[string]string{"path": x.Path, "sha256": hex256(x.Content), "content": base64.StdEncoding.EncodeToString(x.Content)})
		}
		rm := f.removed
		if rm == nil {
			rm = []string{}
		}
		serve(w, r, map[string]any{"sha256": canonical.FramedHash(f.files), "files": fc, "removed": rm, "project_files": f.project})
	})
	mux.HandleFunc("/checks/", func(w http.ResponseWriter, r *http.Request) {
		v := f.verdict
		if v == nil {
			v = map[string]any{"check": strings.Split(r.URL.Path, "/")[2], "passed": true, "reason": "fine", "refs": []string{}, "project_files": f.project}
		}
		serve(w, r, v)
	})
	mux.HandleFunc("/procedures/", func(w http.ResponseWriter, r *http.Request) { serve(w, r, f.proc) })
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

type store struct {
	mu   sync.Mutex
	puts [][]byte
	err  error
}

func (s *store) put(c []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	s.puts = append(s.puts, append([]byte(nil), c...))
	return hex256(c), nil
}

func pattern() machine.Pattern {
	return machine.Pattern{
		ID: "api-feature-go", Version: "1.0.0", Attempts: 3,
		StepBudget: machine.Budget{Tokens: 1000, CostUSD: 0.2, Duration: 2 * time.Minute},
		Steps: []machine.Step{
			{Name: "contract-check", Worker: "procedure:contract-check", Inputs: []string{"slice/contract", "slice/rules"}, Outputs: []string{"contract", "examples"}, Checks: []string{"ContractLintPasses"}},
			{Name: testsStep, Worker: "agent:tests", Inputs: []string{"contract", "examples"}, Outputs: []string{"tests"}, Checks: []string{"ScopeKept", "FindingsCarryEvidence"}},
		},
	}
}

type rig struct {
	agent *agentFake
	chk   *checkFake
	proc  *checkFake
	st    *store
	cfg   steps.Config
	lint  func(string) (bool, string, error)
	lints []string
}

func newRig(t *testing.T) *rig {
	r := &rig{agent: newAgent(t, pinned), chk: newCheck(t), proc: newCheck(t), st: &store{}}
	r.cfg = steps.Config{
		Pattern: pattern(),
		Endpoints: endpoints{
			testsStep:        {r.agent.srv.URL, agentTok},
			"contract-check": {r.proc.srv.URL, procTok},
		},
		CheckURL: r.chk.srv.URL, CheckToken: checkTok,
		Models:   map[string]string{testsStep: pinned},
		Projects: "/projects",
		Approved: func(slice string) (map[string][]byte, error) {
			if slice != "SLICE-1" {
				return nil, fmt.Errorf("unknown slice %s", slice)
			}
			return approvedParts, nil
		},
		Load:  func(h string) ([]byte, error) { return nil, fmt.Errorf("nothing stored under %s", h) },
		Store: r.st.put,
		Lint: func(ctx context.Context, c string) (bool, string, error) {
			r.lints = append(r.lints, c)
			if r.lint != nil {
				return r.lint(c)
			}
			return true, "lint clean", nil
		},
	}
	return r
}

func (r *rig) agentReq(step string) machine.Request {
	st := r.cfg.Pattern.Steps[1]
	return machine.Request{Run: "RUN-7", Slice: "SLICE-1", Project: "proj-1", Step: st, Attempt: 2, Inputs: []string{hashA, hashB}}
}

func ctx5(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestAgentAttemptSessionFlowAndOutput(t *testing.T) {
	r := newRig(t)
	req := r.agentReq(testsStep)
	req.Open = []machine.Finding{{Rule: "R-1", File: "tests/a_test.go", Line: 9, Text: "weak assertion zebra", Blocking: true}}
	res, err := steps.NewWorker(r.cfg).Run(ctx5(t), req)
	if err != nil {
		t.Fatal(err)
	}
	creates := r.agent.rec.find("POST", "/api/sessions")
	if len(creates) != 1 || creates[0].body["safety_policy"] != "autonomous" {
		t.Fatalf("session create: %+v", creates)
	}
	if title, _ := creates[0].body["title"].(string); title == "" {
		t.Fatal("the session has no title (an untitled session costs a second model call)")
	}
	runs := r.agent.rec.find("POST", "/api/sessions/sess-1/agent/tests")
	if len(runs) != 1 {
		t.Fatalf("run calls: %d", len(runs))
	}
	for _, c := range r.agent.rec.all() {
		if c.auth != "Bearer "+agentTok {
			t.Fatalf("%s %s sent auth %q", c.method, c.path, c.auth)
		}
	}
	msgs, _ := runs[0].body["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages: %v", runs[0].body)
	}
	m := msgs[0].(map[string]any)
	prompt, _ := m["content"].(string)
	if m["role"] != "user" {
		t.Fatalf("role %v", m["role"])
	}
	for _, want := range []string{hashA, hashB, "contract", "examples", "weak assertion zebra", "tests/a_test.go"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt lacks %q: %s", want, prompt)
		}
	}
	if len(r.agent.rec.find("DELETE", "/api/sessions/sess-1")) != 1 {
		t.Fatal("the session was not deleted")
	}
	// readStepFiles at the check service, with the check token.
	sf := r.chk.rec.find("POST", "/step-files")
	if len(sf) != 1 || sf[0].auth != "Bearer "+checkTok || sf[0].query != "api-version="+apiVer {
		t.Fatalf("readStepFiles: %+v", sf)
	}
	b := sf[0].body
	if b["run"] != "RUN-7" || b["attempt"] != float64(2) || b["project"] == "" || b["project"] == nil {
		t.Fatalf("readStepFiles body: %v", b)
	}
	if step, _ := b["step"].(map[string]any); step["name"] != testsStep || !reflect.DeepEqual(step["outputs"], []any{"tests"}) {
		t.Fatalf("step facts: %v", b["step"])
	}
	if tl, _ := b["time_limit_seconds"].(float64); tl < 1 || tl > 600 {
		t.Fatalf("time_limit_seconds %v", b["time_limit_seconds"])
	}
	if _, ok := b["before"].([]any); !ok {
		t.Fatalf("before must be an array: %v", b["before"])
	}
	// the output: the files' framed form, stored, its hash returned.
	framed := canonical.Framed(r.chk.files)
	if len(r.st.puts) != 1 || string(r.st.puts[0]) != string(framed) {
		t.Fatalf("stored: %q", r.st.puts)
	}
	if res.Outputs["tests"] != canonical.FramedHash(r.chk.files) || len(res.Outputs) != 1 {
		t.Fatalf("outputs: %v", res.Outputs)
	}
	if res.Model != pinned {
		t.Fatalf("model %q", res.Model)
	}
}

func TestAgentModelMismatchIsAnError(t *testing.T) {
	r := newRig(t)
	r.agent.model = "anthropic/claude-opus-9"
	_, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.agentReq(testsStep))
	if err == nil {
		t.Fatal("a different model must make the attempt an error")
	}
	if len(r.st.puts) != 0 {
		t.Fatal("nothing is stored for a rejected attempt")
	}
	if len(r.agent.rec.find("DELETE", "/api/sessions/sess-1")) != 1 {
		t.Fatal("the session was not deleted after the failure")
	}
}

func TestAgentStreamFailureIsAnError(t *testing.T) {
	r := newRig(t)
	r.agent.events = []string{sse(map[string]any{"type": "agent_info", "model": pinned}), sse(map[string]any{"type": "error", "error": "boom"})}
	if _, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.agentReq(testsStep)); err == nil {
		t.Fatal("an error event must fail the attempt")
	}
	if len(r.chk.rec.all()) != 0 {
		t.Fatal("files were read after a failed session")
	}
}

func TestUnregisteredStepIsAnError(t *testing.T) {
	r := newRig(t)
	r.cfg.Endpoints = endpoints{}
	if _, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.agentReq(testsStep)); err == nil {
		t.Fatal("an unregistered step must be an error")
	}
	if len(r.agent.rec.all())+len(r.chk.rec.all()) != 0 {
		t.Fatal("something was called for an unregistered step")
	}
}

func (r *rig) procReq() machine.Request {
	return machine.Request{Run: "RUN-7", Slice: "SLICE-1", Project: "proj-1", Step: r.cfg.Pattern.Steps[0], Attempt: 1, Inputs: []string{sum(approvedParts["contract"]), sum(approvedParts["rules"])}}
}

func procResult(files map[string][]canonical.File) map[string]any {
	var outs []map[string]any
	for _, n := range []string{"contract", "examples"} {
		var fc []map[string]string
		for _, f := range files[n] {
			fc = append(fc, map[string]string{"path": f.Path, "sha256": hex256(f.Content), "content": b64(string(f.Content))})
		}
		outs = append(outs, map[string]any{"name": n, "sha256": canonical.FramedHash(files[n]), "files": fc})
	}
	return map[string]any{"procedure": "contract-check", "completed": true, "reason": "copied", "outputs": outs, "project_files": []map[string]string{pf("api/openapi.yaml", hashA)}}
}

var procFiles = map[string][]canonical.File{
	"contract": {{Path: "api/openapi.yaml", Content: []byte("openapi: 3.1.0\n")}},
	"examples": {{Path: "features/slice.feature", Content: []byte("Feature: x\n")}},
}

func TestProcedureAttemptCallsEndpointAndStoresOutputs(t *testing.T) {
	r := newRig(t)
	r.proc.proc = procResult(procFiles)
	res, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.procReq())
	if err != nil {
		t.Fatal(err)
	}
	cs := r.proc.rec.find("POST", "/procedures/contract-check/run")
	if len(cs) != 1 || cs[0].auth != "Bearer "+procTok || cs[0].query != "api-version="+apiVer {
		t.Fatalf("runProcedure: %+v", cs)
	}
	b := cs[0].body
	if b["run"] != "RUN-7" || b["attempt"] != float64(1) {
		t.Fatalf("body: %v", b)
	}
	ins, _ := b["inputs"].([]any)
	if len(ins) != 2 {
		t.Fatalf("inputs: %v", b["inputs"])
	}
	for i, want := range []struct{ n, h string }{{"slice/contract", sum(approvedParts["contract"])}, {"slice/rules", sum(approvedParts["rules"])}} {
		in := ins[i].(map[string]any)
		if in["name"] != want.n || in["sha256"] != want.h {
			t.Errorf("input %d: %v", i, in)
		}
	}
	if len(r.chk.rec.all())+len(r.agent.rec.all()) != 0 {
		t.Fatal("a procedure attempt must call only its own worker")
	}
	for n, files := range procFiles {
		if got := res.Outputs[n]; got != canonical.FramedHash(files) {
			t.Errorf("output %s = %q", n, got)
		}
		found := false
		for _, p := range r.st.puts {
			found = found || string(p) == string(canonical.Framed(files))
		}
		if !found {
			t.Errorf("output %s was not stored", n)
		}
	}
	if len(res.Outputs) != 2 {
		t.Fatalf("outputs: %v", res.Outputs)
	}
}

func TestProcedureNotCompletedIsRejectedWithReason(t *testing.T) {
	r := newRig(t)
	r.proc.proc = map[string]any{"procedure": "contract-check", "completed": false, "reason": "contract is not one YAML document", "outputs": []any{}, "project_files": []any{}}
	_, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.procReq())
	if err == nil || !strings.Contains(err.Error(), "contract is not one YAML document") {
		t.Fatalf("want an error carrying the reason, got %v", err)
	}
	if len(r.st.puts) != 0 {
		t.Fatal("nothing is stored when the procedure did not complete")
	}
}

func TestProcedureNon200IsAnError(t *testing.T) {
	r := newRig(t)
	r.proc.status = 503
	if _, err := steps.NewWorker(r.cfg).Run(ctx5(t), r.procReq()); err == nil {
		t.Fatal("503 must be an error")
	}
}

func (r *rig) checkReq(name string, res machine.Result) machine.CheckRequest {
	res.ProjectFiles = toFiles(r.chk.project)
	return machine.CheckRequest{Run: "RUN-7", Slice: "SLICE-1", Project: "proj-1", Step: r.cfg.Pattern.Steps[1], Attempt: 2, Name: name, Result: res, Inputs: []string{hashA, hashB}}
}

func TestCheckerSendsRequestAndMapsVerdict(t *testing.T) {
	r := newRig(t)
	r.chk.verdict = map[string]any{"check": "ScopeKept", "passed": false, "reason": "tests/x.go is outside the scope", "refs": []string{"rule:R-1", "ex:E-2"}, "project_files": r.chk.project}
	res := machine.Result{
		Outputs:      map[string]string{"tests": hashA},
		Findings:     []machine.Finding{{Rule: "R-1", File: "internal/h.go", Line: 12, Text: "bad", Blocking: true, Check: "AllTestsPass"}, {Rule: "R-2", Text: "note"}},
		ChangedLines: []machine.Line{{File: "internal/h.go", Line: 12}, {File: "cmd/m.go", Line: 3}},
	}
	got, err := steps.NewChecker(r.cfg).Check(ctx5(t), r.checkReq("ScopeKept", res))
	if err != nil {
		t.Fatal(err)
	}
	if got.Passed || got.Reason != "tests/x.go is outside the scope" || !reflect.DeepEqual(got.Refs, []string{"rule:R-1", "ex:E-2"}) {
		t.Fatalf("result: %+v", got)
	}
	cs := r.chk.rec.find("POST", "/checks/ScopeKept/run")
	if len(cs) != 1 || cs[0].auth != "Bearer "+checkTok || cs[0].query != "api-version="+apiVer || !strings.HasPrefix(cs[0].ctype, "application/json") {
		t.Fatalf("runCheck: %+v", cs)
	}
	b := cs[0].body
	if b["run"] != "RUN-7" || b["attempt"] != float64(2) {
		t.Fatalf("body: %v", b)
	}
	if !reflect.DeepEqual(b["outputs"], []any{map[string]any{"name": "tests", "sha256": hashA}}) {
		t.Fatalf("outputs: %v", b["outputs"])
	}
	fs, _ := b["findings"].([]any)
	if len(fs) != 2 {
		t.Fatalf("findings: %v", b["findings"])
	}
	f0 := fs[0].(map[string]any)
	if f0["rule"] != "R-1" || f0["file"] != "internal/h.go" || f0["line"] != float64(12) || f0["text"] != "bad" || f0["blocking"] != true || f0["check"] != "AllTestsPass" {
		t.Fatalf("finding 0: %v", f0)
	}
	cl, _ := b["changed_lines"].([]any)
	if len(cl) != 2 || cl[1].(map[string]any)["file"] != "cmd/m.go" || cl[1].(map[string]any)["line"] != float64(3) {
		t.Fatalf("changed_lines: %v", b["changed_lines"])
	}
	// the contract's required fields are present.
	ap, _ := b["approved"].([]any)
	if len(ap) != 3 {
		t.Fatalf("approved must hold the three parts: %v", b["approved"])
	}
	for _, k := range []string{"earlier_steps", "inputs", "before"} {
		if _, ok := b[k].([]any); !ok {
			t.Errorf("%s must be an array: %v", k, b[k])
		}
	}
	if step, _ := b["step"].(map[string]any); step["name"] != testsStep {
		t.Fatalf("step: %v", b["step"])
	}
}

func TestCheckerBeforeIsThePreviousStepsProjectFiles(t *testing.T) {
	r := newRig(t)
	r.proc.proc = procResult(procFiles)
	w := steps.NewWorker(r.cfg)
	if _, err := w.Run(ctx5(t), r.procReq()); err != nil {
		t.Fatal(err)
	}
	prev := []any{map[string]any{"path": "api/openapi.yaml", "sha256": hashA}}
	res, err := w.Run(ctx5(t), r.agentReq(testsStep))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.chk.rec.find("POST", "/step-files")[0].body["before"]; !reflect.DeepEqual(got, prev) {
		t.Fatalf("readStepFiles before = %v, want %v", got, prev)
	}
	if _, err := steps.NewChecker(r.cfg).Check(ctx5(t), r.checkReq("ScopeKept", res)); err != nil {
		t.Fatal(err)
	}
	if got := r.chk.rec.find("POST", "/checks/ScopeKept/run")[0].body["before"]; !reflect.DeepEqual(got, prev) {
		t.Fatalf("runCheck before = %v, want %v", got, prev)
	}
}

func TestCheckerPassedVerdict(t *testing.T) {
	r := newRig(t)
	got, err := steps.NewChecker(r.cfg).Check(ctx5(t), r.checkReq("ScopeKept", machine.Result{Outputs: map[string]string{"tests": hashA}}))
	if err != nil || !got.Passed || got.Reason != "fine" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestCheckerNon200IsAFailedCheck(t *testing.T) {
	for _, code := range []int{400, 401, 404, 422, 429, 500, 503, 504, 201, 204} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			r := newRig(t)
			r.chk.status = code
			got, err := steps.NewChecker(r.cfg).Check(ctx5(t), r.checkReq("ScopeKept", machine.Result{}))
			if err == nil || got.Passed {
				t.Fatalf("status %d: got %+v, %v", code, got, err)
			}
		})
	}
}

func TestCheckerBadBodyAndTransportErrorsAreFailedChecks(t *testing.T) {
	r := newRig(t)
	r.chk.body = `{"check":"ScopeKept","passed":tru`
	if got, err := steps.NewChecker(r.cfg).Check(ctx5(t), r.checkReq("ScopeKept", machine.Result{})); err == nil || got.Passed {
		t.Fatalf("truncated body: %+v %v", got, err)
	}
	r.chk.body = ""
	r.cfg.CheckURL = "http://127.0.0.1:1" // nothing listens
	if got, err := steps.NewChecker(r.cfg).Check(ctx5(t), r.checkReq("ScopeKept", machine.Result{})); err == nil || got.Passed {
		t.Fatalf("transport error: %+v %v", got, err)
	}
}

func TestCheckerLintGoesToLintNotTheCheckService(t *testing.T) {
	r := newRig(t)
	r.proc.proc = procResult(procFiles)
	w := steps.NewWorker(r.cfg)
	res, err := w.Run(ctx5(t), r.procReq())
	if err != nil {
		t.Fatal(err)
	}
	r.lint = func(string) (bool, string, error) { return false, "info-contact missing", nil }
	ch := steps.NewChecker(r.cfg)
	got, err := ch.Check(ctx5(t), machine.CheckRequest{Run: "RUN-7", Slice: "SLICE-1", Project: "proj-1", Step: r.cfg.Pattern.Steps[0], Attempt: 1, Name: "ContractLintPasses", Result: res})
	if err != nil {
		t.Fatal(err)
	}
	if got.Passed || got.Reason != "info-contact missing" {
		t.Fatalf("%+v", got)
	}
	if len(r.lints) != 1 || r.lints[0] != "openapi: 3.1.0\n" {
		t.Fatalf("Lint received %q (want the contract output's api/openapi.yaml text)", r.lints)
	}
	if n := len(r.chk.rec.all()); n != 0 {
		t.Fatalf("ContractLintPasses reached the check service %d times", n)
	}
	// passing, and a lint error
	r.lint = func(string) (bool, string, error) { return true, "clean", nil }
	if got, err := ch.Check(ctx5(t), machine.CheckRequest{Run: "RUN-7", Slice: "SLICE-1", Project: "proj-1", Step: r.cfg.Pattern.Steps[0], Attempt: 1, Name: "ContractLintPasses", Result: res}); err != nil || !got.Passed {
		t.Fatalf("%+v %v", got, err)
	}
	r.lint = func(string) (bool, string, error) { return true, "", fmt.Errorf("lint unreachable") }
	if got, err := ch.Check(ctx5(t), machine.CheckRequest{Run: "RUN-7", Slice: "SLICE-1", Project: "proj-1", Step: r.cfg.Pattern.Steps[0], Attempt: 1, Name: "ContractLintPasses", Result: res}); err == nil || got.Passed {
		t.Fatalf("a lint error must be a failed check: %+v %v", got, err)
	}
	if len(r.chk.rec.all()) != 0 {
		t.Fatal("the check service was called")
	}
}

func TestFindingWithNonProjectPathIsDroppedAndFailsEvidence(t *testing.T) {
	for _, bad := range []string{"../etc/passwd", "/etc/passwd", "a//b.go", "dir/", "has space.go", "tab\t.go", "a/./b.go", ""} {
		t.Run(fmt.Sprintf("%q", bad), func(t *testing.T) {
			r := newRig(t)
			res := machine.Result{Findings: []machine.Finding{
				{Rule: "R-1", File: bad, Line: 1, Text: "bad path", Blocking: true},
				{Rule: "R-2", File: "internal/ok.go", Line: 2, Text: "good", Blocking: true},
			}}
			got, err := steps.NewChecker(r.cfg).Check(ctx5(t), r.checkReq("FindingsCarryEvidence", res))
			if err != nil {
				t.Fatal(err)
			}
			if got.Passed {
				t.Fatal("a finding with a non-project path must fail FindingsCarryEvidence, whatever the service says")
			}
			for _, c := range r.chk.rec.all() {
				fs, _ := c.body["findings"].([]any)
				for _, f := range fs {
					if file, _ := f.(map[string]any)["file"].(string); file != "internal/ok.go" {
						t.Fatalf("a finding with file %q reached the wire", file)
					}
				}
			}
		})
	}
}

func TestFindingWithNonProjectPathIsDroppedForOtherChecks(t *testing.T) {
	r := newRig(t)
	res := machine.Result{Findings: []machine.Finding{{Rule: "R-1", File: "../x", Line: 1, Text: "t", Blocking: true}, {Rule: "R-2", File: "a.go", Line: 2, Text: "t", Blocking: true}}}
	if _, err := steps.NewChecker(r.cfg).Check(ctx5(t), r.checkReq("NoBlockingFindingOpen", res)); err != nil {
		t.Fatal(err)
	}
	cs := r.chk.rec.find("POST", "/checks/NoBlockingFindingOpen/run")
	if len(cs) != 1 {
		t.Fatalf("calls: %d", len(cs))
	}
	if fs, _ := cs[0].body["findings"].([]any); len(fs) != 1 || fs[0].(map[string]any)["file"] != "a.go" {
		t.Fatalf("findings: %v", cs[0].body["findings"])
	}
}

func TestHangingServicesAreBoundedByContext(t *testing.T) {
	bound := func(t *testing.T, run func(ctx context.Context) error) {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- run(ctx) }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("a hung service must be an error")
			}
		case <-time.After(4 * time.Second):
			t.Fatal("the call outlived its context")
		}
	}
	t.Run("agent session create", func(t *testing.T) {
		r := newRig(t)
		r.agent.hung = true
		bound(t, func(ctx context.Context) error {
			_, err := steps.NewWorker(r.cfg).Run(ctx, r.agentReq(testsStep))
			return err
		})
	})
	t.Run("agent stream", func(t *testing.T) {
		r := newRig(t)
		stuck := make(chan struct{})
		t.Cleanup(func() { close(stuck) })
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
			if strings.HasSuffix(q.URL.Path, "/agent/tests") {
				w.(http.Flusher).Flush()
				select {
				case <-stuck:
				case <-q.Context().Done():
				}
				return
			}
			fmt.Fprint(w, `{"id":"sess-1"}`)
		}))
		t.Cleanup(srv.Close)
		r.cfg.Endpoints = endpoints{testsStep: {srv.URL, agentTok}}
		bound(t, func(ctx context.Context) error {
			_, err := steps.NewWorker(r.cfg).Run(ctx, r.agentReq(testsStep))
			return err
		})
	})
	t.Run("readStepFiles", func(t *testing.T) {
		r := newRig(t)
		r.chk.hung = true
		bound(t, func(ctx context.Context) error {
			_, err := steps.NewWorker(r.cfg).Run(ctx, r.agentReq(testsStep))
			return err
		})
	})
	t.Run("procedure", func(t *testing.T) {
		r := newRig(t)
		r.proc.hung = true
		bound(t, func(ctx context.Context) error { _, err := steps.NewWorker(r.cfg).Run(ctx, r.procReq()); return err })
	})
	t.Run("check", func(t *testing.T) {
		r := newRig(t)
		r.chk.hung = true
		bound(t, func(ctx context.Context) error {
			_, err := steps.NewChecker(r.cfg).Check(ctx, r.checkReq("ScopeKept", machine.Result{}))
			return err
		})
	})
}
