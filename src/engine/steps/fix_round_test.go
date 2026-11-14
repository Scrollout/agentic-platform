package steps_test

// T083 (owner, 2026-11-14): a review fix carries the round whose findings it fixes. The adapters
// send that round of the code step to the services (readStepFiles, runCheck, restoreStep,
// commitStep) and put it in the agent's message: its first line is
// `Run <run>, step code, attempt <n>, review round <r>.` Written before the code by a separate
// author; the machine gives the fix's Request, CheckRequest, RestoreRequest and CommitRequest
// that Round (machine/fix_round_test.go).

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/steps"
)

// codeAgent imitates `serve api` for the code step and keeps the message it was given.
type codeAgent struct {
	mu     sync.Mutex
	prompt string
}

func newCodeAgent(t *testing.T) (*codeAgent, *httptest.Server) {
	a := &codeAgent{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"id":"sess-1"}`) })
	mux.HandleFunc("/api/sessions/sess-1", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/api/sessions/sess-1/agent/code", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		a.mu.Lock()
		a.prompt = string(b)
		a.mu.Unlock()
		fmt.Fprint(w, sse(map[string]any{"type": "agent_info", "model": pinned}))
		fmt.Fprint(w, sse(map[string]any{"type": "agent_choice", "content": "done"}))
		fmt.Fprint(w, sse(map[string]any{"type": "stream_stopped", "reason": "normal"}))
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return a, s
}

func fixRig(t *testing.T) (*rig, *codeAgent) {
	r := newRig(t)
	a, srv := newCodeAgent(t)
	r.cfg.Pattern = witnessPattern()
	r.cfg.Endpoints = endpoints{"code": {srv.URL, agentTok}}
	r.cfg.Models = map[string]string{"code": pinned}
	r.chk.files = nil
	r.chk.project = []map[string]string{pf("internal/greet/greet.go", hashA)}
	return r, a
}

func fixRequest(round, attempt int) machine.Request {
	return machine.Request{Run: "RUN-7", Slice: "SLICE-1", Project: "proj-1", Step: witnessPattern().Steps[2], Attempt: attempt, Round: round,
		Open: []machine.Finding{{Rule: "R-1", File: "internal/greet/greet.go", Line: 3, Text: "the name is left out", Blocking: true}}}
}

func TestFixMessageNamesTheReviewRound(t *testing.T) {
	for _, c := range []struct{ round, attempt int }{{1, 1}, {2, 3}} {
		r, agent := fixRig(t)
		if _, err := steps.NewWorker(r.cfg).Run(ctx5(t), fixRequest(c.round, c.attempt)); err != nil {
			t.Fatal(err)
		}
		first, _, _ := strings.Cut(promptOf(t, agent.prompt), "\n")
		if want := fmt.Sprintf("Run RUN-7, step code, attempt %d, review round %d.", c.attempt, c.round); first != want {
			t.Errorf("first line of the fix's message = %q, want %q", first, want)
		}
	}
	// The code step's own run has no round in its message.
	r, agent := fixRig(t)
	req := fixRequest(0, 1)
	req.Open = nil
	if _, err := steps.NewWorker(r.cfg).Run(ctx5(t), req); err != nil {
		t.Fatal(err)
	}
	if first, _, _ := strings.Cut(promptOf(t, agent.prompt), "\n"); first != "Run RUN-7, step code, attempt 1." {
		t.Errorf("first line of the code step's message = %q", first)
	}
}

// promptOf is the message's text out of the run call's JSON body (newlines are \n escapes there).
func promptOf(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `"content"`)
	if i < 0 {
		t.Fatalf("no message in %s", body)
	}
	s := body[i+len(`"content"`):]
	s = strings.TrimLeft(s, `: "`)
	return strings.ReplaceAll(s, `\n`, "\n")
}

func TestFixCallsCarryTheRoundToTheServices(t *testing.T) {
	r, _ := fixRig(t)
	req := fixRequest(1, 1)
	if _, err := steps.NewWorker(r.cfg).Run(ctx5(t), req); err != nil {
		t.Fatal(err)
	}
	sf := r.chk.rec.find("POST", "/step-files")
	if len(sf) != 1 || sf[0].body["round"] != float64(1) || sf[0].body["attempt"] != float64(1) {
		t.Fatalf("readStepFiles of the fix: %+v, want round 1 attempt 1", sf)
	}
	res := machine.Result{Outputs: map[string]string{"code": hashB}, ProjectFiles: toFiles(r.chk.project)}
	if _, err := steps.NewChecker(r.cfg).Check(ctx5(t), machine.CheckRequest{Run: "RUN-7", Slice: "SLICE-1", Project: "proj-1",
		Step: req.Step, Attempt: 1, Round: 1, Name: "ScopeKept", Result: res}); err != nil {
		t.Fatal(err)
	}
	cs := r.chk.rec.find("POST", "/checks/ScopeKept/run")
	if len(cs) != 1 || cs[0].body["round"] != float64(1) || cs[0].body["attempt"] != float64(1) {
		t.Fatalf("runCheck of the fix: %+v, want round 1", cs)
	}

	f := newVersions(t)
	if err := historyFor(f, nil).Restore(ctx10(t), machine.RestoreRequest{Run: "RUN-7", Step: req.Step, Attempt: 1, Round: 1,
		Project: "proj-a", Before: filesBefore, TimeLimit: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if _, err := historyFor(f, nil).Commit(ctx10(t), machine.CommitRequest{Run: "RUN-7", Step: req.Step, Attempt: 1, Round: 1,
		Project: "proj-a", Before: filesBefore, ProjectFiles: filesAfter, AcceptedAt: time.Now(), TimeLimit: time.Minute}); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.rec.all() {
		if c.body["round"] != float64(1) || c.body["attempt"] != float64(1) {
			t.Errorf("%s of the fix: round %v attempt %v, want 1 and 1", c.path, c.body["round"], c.body["attempt"])
		}
		if v, has := c.body["open_round"]; has {
			t.Errorf("%s of the fix sent open_round %v: a fix is an accepted step, not an open round", c.path, v)
		}
	}
	if len(f.rec.all()) != 2 {
		t.Errorf("want a restoreStep and a commitStep, got %d calls", len(f.rec.all()))
	}
}
