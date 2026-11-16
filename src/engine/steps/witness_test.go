package steps_test

// T076: the engine's adapters for the review step of research D35 and ADR 0039, written before
// the code by a separate author. The review declares two outputs, [findings, witnesses], and
// writes review/**: an attempt stores its findings (the answer's JSON) and its witnesses (the
// files readStepFiles returns, framed); each finding's `witness` reaches the check service; and
// the fix step (the step before the review) has the review among its earlier steps, so
// TestsUnchanged covers the witnesses.
//
// ASSUMED SHAPE: machine.Finding gains `Witness *machine.Witness` (json "witness,omitempty"),
// machine.Witness{File, Test string} (json "file", "test"), as run-event.schema.json's
// finding.witness.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/steps"
)

const witnessFile, witnessTest = "review/r1/name_test.go", "TestR1NameLeftOut"

func witnessPattern() machine.Pattern {
	return machine.Pattern{
		ID: "api-feature-go", Version: "1.0.0", Attempts: 3, ReviewRounds: 3,
		StepBudget: machine.Budget{Tokens: 1000, CostUSD: 0.2, Duration: 2 * time.Minute},
		Steps: []machine.Step{
			{Name: "generate", Worker: "procedure:generate", Outputs: []string{"generated-code"},
				Writable: []string{"gen/**", "go.mod", "go.sum", "vendor/**", "internal/app/app.go"}},
			{Name: "tests", Worker: "agent:tests", Outputs: []string{"tests"}, Writable: []string{"tests/**"}},
			{Name: "code", Worker: "agent:code", Outputs: []string{"code"}, Writable: []string{"internal/**", "cmd/**"},
				Checks: []string{"ScopeKept", "TestsUnchanged"}},
			{Name: "review", Worker: "agent:review", Inputs: []string{"code"}, Outputs: []string{"findings", "witnesses"},
				Writable: []string{"review/**"}, Checks: []string{"ScopeKept", "TestsUnchanged", "FindingsCarryEvidence", "NoBlockingFindingOpen"}},
		},
	}
}

func witnessFinding() machine.Finding {
	return machine.Finding{Rule: "R-1", File: "internal/greet/greet.go", Line: 23, Text: "the name is left out", Blocking: true,
		Witness: &machine.Witness{File: witnessFile, Test: witnessTest}}
}

// reviewAgent imitates `serve api` for the review step, answering with findings.
func reviewAgent(t *testing.T, answer string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"id":"sess-1"}`) })
	mux.HandleFunc("/api/sessions/sess-1", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/api/sessions/sess-1/agent/review", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sse(map[string]any{"type": "agent_info", "model": pinned}))
		fmt.Fprint(w, sse(map[string]any{"type": "agent_choice", "content": answer}))
		fmt.Fprint(w, sse(map[string]any{"type": "stream_stopped", "reason": "normal"}))
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func witnessRig(t *testing.T, answer string) (*rig, *httptest.Server) {
	r := newRig(t)
	agent := reviewAgent(t, answer)
	r.cfg.Pattern = witnessPattern()
	r.cfg.Endpoints = endpoints{"review": {agent.URL, agentTok}}
	r.cfg.Models = map[string]string{"review": pinned}
	r.chk.files = []canonical.File{{Path: witnessFile, Content: []byte("package r1\n")}}
	r.chk.project = []map[string]string{pf("internal/greet/greet.go", hashA), pf(witnessFile, hex256([]byte("package r1\n")))}
	return r, agent
}

// A review attempt stores two outputs: findings, the answer's findings as JSON (witness kept),
// and witnesses, the framed files under review/**.
func TestReviewAttemptStoresFindingsAndWitnesses(t *testing.T) {
	answer, err := json.Marshal(map[string]any{"findings": []machine.Finding{witnessFinding()}})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := witnessRig(t, string(answer))
	review := r.cfg.Pattern.Steps[3]
	res, err := steps.NewWorker(r.cfg).Run(ctx5(t), machine.Request{Run: "RUN-7", Slice: "SLICE-1", Project: "proj-1",
		Step: review, Attempt: 1, Round: 1, Inputs: []string{hashA}})
	if err != nil {
		t.Fatalf("a review attempt with outputs [findings witnesses] was refused: %v", err)
	}
	if len(res.Outputs) != 2 {
		t.Fatalf("outputs = %v, want findings and witnesses", res.Outputs)
	}
	if got, want := res.Outputs["witnesses"], canonical.FramedHash(r.chk.files); got != want {
		t.Errorf("witnesses = %s, want the framed hash of the review/** files %s", got, want)
	}
	var stored []machine.Finding
	if err := json.Unmarshal(res.Content["findings"], &stored); err != nil || hex256(res.Content["findings"]) != res.Outputs["findings"] {
		t.Fatalf("findings content %q (hash %s): %v", res.Content["findings"], res.Outputs["findings"], err)
	}
	if len(stored) != 1 || stored[0].Witness == nil || *stored[0].Witness != (machine.Witness{File: witnessFile, Test: witnessTest}) {
		t.Errorf("the stored findings lost the witness: %+v", stored)
	}
	if len(res.Findings) != 1 || !reflect.DeepEqual(res.Findings[0], witnessFinding()) {
		t.Errorf("result findings = %+v", res.Findings)
	}
	if string(res.Content["witnesses"]) != string(canonical.Framed(r.chk.files)) {
		t.Errorf("witnesses content = %q", res.Content["witnesses"])
	}
	puts := map[string]bool{}
	for _, p := range r.st.puts {
		puts[hex256(p)] = true
	}
	if !puts[res.Outputs["findings"]] || !puts[res.Outputs["witnesses"]] {
		t.Errorf("both outputs must be stored: %v", res.Outputs)
	}
}

// The check request for the review names both outputs and carries each finding's witness.
func TestCheckerSendsTheWitness(t *testing.T) {
	r, _ := witnessRig(t, `{"findings":[]}`)
	review := r.cfg.Pattern.Steps[3]
	res := machine.Result{Outputs: map[string]string{"findings": hashB, "witnesses": hashC}, Findings: []machine.Finding{witnessFinding()},
		ProjectFiles: toFiles(r.chk.project)}
	if _, err := steps.NewChecker(r.cfg).Check(ctx5(t), machine.CheckRequest{Run: "RUN-7", Slice: "SLICE-1", Project: "proj-1",
		Step: review, Attempt: 1, Round: 1, Name: "NoBlockingFindingOpen", Result: res, Inputs: []string{hashA}}); err != nil {
		t.Fatal(err)
	}
	cs := r.chk.rec.find("POST", "/checks/NoBlockingFindingOpen/run")
	if len(cs) != 1 {
		t.Fatalf("calls: %d", len(cs))
	}
	b := cs[0].body
	want := []any{map[string]any{"name": "findings", "sha256": hashB}, map[string]any{"name": "witnesses", "sha256": hashC}}
	if !reflect.DeepEqual(b["outputs"], want) {
		t.Errorf("outputs = %v, want %v", b["outputs"], want)
	}
	fs, _ := b["findings"].([]any)
	if len(fs) != 1 {
		t.Fatalf("findings: %v", b["findings"])
	}
	if w := fs[0].(map[string]any)["witness"]; !reflect.DeepEqual(w, map[string]any{"file": witnessFile, "test": witnessTest}) {
		t.Errorf("witness = %v", w)
	}
}

// The fix step (the step before the review) is judged with the review among its earlier steps,
// so TestsUnchanged covers the witnesses (check contract, EarlierStep).
func TestFixStepHasTheReviewAmongItsEarlierSteps(t *testing.T) {
	r, _ := witnessRig(t, `{"findings":[]}`)
	code := r.cfg.Pattern.Steps[2]
	res := machine.Result{Outputs: map[string]string{"code": hashB}, ProjectFiles: toFiles(r.chk.project)}
	if _, err := steps.NewChecker(r.cfg).Check(ctx5(t), machine.CheckRequest{Run: "RUN-7", Slice: "SLICE-1", Project: "proj-1",
		Step: code, Attempt: 1, Name: "TestsUnchanged", Result: res}); err != nil {
		t.Fatal(err)
	}
	cs := r.chk.rec.find("POST", "/checks/TestsUnchanged/run")
	if len(cs) != 1 {
		t.Fatalf("calls: %d", len(cs))
	}
	es, _ := cs[0].body["earlier_steps"].([]any)
	var names []string
	var review map[string]any
	for _, e := range es {
		m := e.(map[string]any)
		names = append(names, m["name"].(string))
		if m["name"] == "review" {
			review = m
		}
	}
	if review == nil {
		t.Fatalf("earlier steps %v lack the review", names)
	}
	if !reflect.DeepEqual(review["writable"], []any{"review/**"}) || !reflect.DeepEqual(review["outputs"], []any{"findings", "witnesses"}) {
		t.Errorf("the review as an earlier step: %v", review)
	}
	for _, n := range []string{"generate", "tests"} {
		found := false
		for _, m := range names {
			found = found || m == n
		}
		if !found {
			t.Errorf("earlier steps %v lack %s", names, n)
		}
	}
}
