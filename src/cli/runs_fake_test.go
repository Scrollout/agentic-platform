// T055: the stand-in engine for the run commands' tests (build, resume, replay, take-over, compare,
// artifact, runs, run). Written by a separate author from src/cli/contract.md and the engine contract
// alone, before the commands exist; the tests in follow_test.go, idempotency_test.go and
// runs_cmd_test.go fail until they do.
//
// Seams these tests assume (the implementer provides them in package cli):
//
//   - Every command runs in process through cli.Run(ctx, args, env, stdin, stdout, stderr), as the Story 1
//     tests do.
//
//   - Signals reach the client through a new field of cli.Env:
//
//     Signals <-chan os.Signal // SIGINT (os.Interrupt) and SIGTERM; nil means none arrive
//
//     The binary's main fills it with signal.Notify(ch, os.Interrupt, syscall.SIGTERM) and no longer
//     cancels Run's context on a signal. A test sends os.Interrupt or syscall.SIGTERM on the channel and
//     sees exit 130 or 143. Run's context is never cancelled in these tests.
//
//   - The retry waits are the contract's real ones (1, 2 and 4 seconds, up to half again of jitter);
//     the tests that lose every try run in parallel and take about 10 seconds each.
//
// The stand-in records every request with its headers, so a test can see the Idempotency-Key, and can
// drop a connection without an answer.
package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	cli "github.com/agentic-platform/cli"
)

// ---- the stand-in engine ----

type req struct {
	Method, Path string
	Query        url.Values
	Header       http.Header
	Body         []byte
	At           time.Time
}

type eng struct {
	t      *testing.T
	mu     sync.Mutex
	reqs   []req
	routes map[string]func(http.ResponseWriter, req)
	srv    *httptest.Server
}

func newEng(t *testing.T) *eng {
	e := &eng{t: t, routes: map[string]func(http.ResponseWriter, req){}}
	e.srv = httptest.NewServer(http.HandlerFunc(e.serve))
	t.Cleanup(e.srv.Close)
	return e
}

func (e *eng) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c := req{r.Method, r.URL.Path, r.URL.Query(), r.Header.Clone(), body, time.Now()}
	if r.Header.Get("Authorization") != "Bearer "+token {
		e.t.Errorf("%s %s: Authorization = %q, want the client token", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		problem(w, 401, "Unauthorized")
		return
	}
	if c.Query.Get("api-version") != apiVersion {
		e.t.Errorf("%s %s: api-version = %q", r.Method, r.URL.Path, c.Query.Get("api-version"))
		problem(w, 400, "UnsupportedApiVersionValue")
		return
	}
	e.mu.Lock()
	e.reqs = append(e.reqs, c)
	h := e.routes[r.Method+" "+r.URL.Path]
	e.mu.Unlock()
	if h == nil {
		e.t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		problem(w, 404, "NotFound")
		return
	}
	h(w, c)
}

func (e *eng) on(route string, h func(http.ResponseWriter, req)) {
	e.mu.Lock()
	e.routes[route] = h
	e.mu.Unlock()
}

func (e *eng) reply(route string, status int, v any) {
	e.on(route, func(w http.ResponseWriter, _ req) { writeJSON(w, status, v) })
}

// refuse answers route with the problem p (see prob).
func (e *eng) refuse(route string, status int, p map[string]any) {
	e.on(route, func(w http.ResponseWriter, _ req) { writeProblem(w, status, p) })
}

func (e *eng) got(method, path string) []req {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []req
	for _, c := range e.reqs {
		if c.Method == method && c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

func (e *eng) total() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.reqs)
}

// anyPathWith reports whether any request's path contains sub.
func (e *eng) anyPathWith(sub string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.reqs {
		if strings.Contains(c.Path, sub) {
			return true
		}
	}
	return false
}

// waitFor waits until method path has been called n times.
func (e *eng) waitFor(method, path string, n int) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(e.got(method, path)) < n {
		if time.Now().After(deadline) {
			e.t.Fatalf("%s %s was called %d times in 5 s, want %d", method, path, len(e.got(method, path)), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *eng) env() cli.Env { return cli.Env{EngineURL: e.srv.URL, EngineToken: token} }

// run runs one command to its end, with no signal.
func (e *eng) run(args ...string) result {
	e.t.Helper()
	return runFor(e.env(), 40*time.Second, args...)
}

func runFor(env cli.Env, limit time.Duration, args ...string) result {
	var out, errb syncBuf
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	code := cli.Run(ctx, args, env, strings.NewReader(""), &out, &errb)
	return result{code, out.String(), errb.String()}
}

// start runs one command in the background with sigs as its signals; the result arrives on the channel.
func (e *eng) start(sigs <-chan os.Signal, args ...string) <-chan result {
	env := e.env()
	env.Signals = sigs
	done := make(chan result, 1)
	go func() { done <- runFor(env, 40*time.Second, args...) }()
	return done
}

func await(t *testing.T, done <-chan result, within time.Duration) result {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(within):
		t.Fatalf("the command did not return within %v", within)
		return result{}
	}
}

// drop closes the connection without an answer.
func drop(w http.ResponseWriter) {
	c, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		c.Close()
	}
}

func prob(code, title, detail string, failures ...map[string]any) map[string]any {
	p := map[string]any{"type": "https://example.invalid/problems/" + code, "title": title, "code": code}
	if detail != "" {
		p["detail"] = detail
	}
	if len(failures) > 0 {
		p["failures"] = failures
	}
	return p
}

func writeProblem(w http.ResponseWriter, status int, p map[string]any) {
	p["status"] = status
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	writeBody(w, p)
}

func writeBody(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }

// oneLine joins s's words with single spaces, so a message wrapped over lines compares as one.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func wantOut(t *testing.T, r result, want string) {
	t.Helper()
	if r.out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s\nstderr: %s", r.out, want, r.err)
	}
}

// ---- runs and their records ----

const (
	tCreated = "2026-11-14T10:00:00Z"
	tExpires = "2026-11-28T10:00:00Z"
	pattern  = "tdd@1.0.0"
)

type runSpec struct {
	id, op, project, replayOf string
	status, outcome, reason   string
}

// runJSON is the run as getRun and listRuns give it, valid by the engine contract's Run schema.
func runJSON(s runSpec) map[string]any {
	if s.op == "" {
		s.op = "OP-12"
	}
	if s.project == "" {
		s.project = "notes"
	}
	m := map[string]any{"id": s.id, "slice": "S-1", "pattern": pattern, "project": s.project, "status": s.status,
		"operation": s.op, "created_at": tCreated, "updated_at": "2026-11-14T10:05:00Z"}
	if s.status == "ended" {
		m["outcome"] = s.outcome
		m["ended_at"] = "2026-11-14T10:05:00Z"
	}
	if s.status == "waiting-for-developer" || s.status == "interrupted" {
		m["expires_at"] = tExpires
	}
	if s.reason != "" {
		m["reason"] = s.reason
	}
	if s.replayOf != "" {
		m["replay_of"] = s.replayOf
	}
	return m
}

// fakeRun serves one run, its record and its operation. The run is running for its first
// runningReads getRun answers, each revealing perRead more events after the first initial ones;
// after that it is final, with every event visible. listRunEvents pages pageSize events; until the run
// has ended (status ended), the last page keeps a next_page_token.
type fakeRun struct {
	t            *testing.T
	id, op       string
	final        map[string]any
	finalOp      map[string]any
	events       []map[string]any
	initial      int
	perRead      int
	runningReads int
	pageSize     int
	onFirstPage  func()
	eventsAnswer func(w http.ResponseWriter, n int) bool // n-th listRunEvents call from 1; true when it answered

	mu         sync.Mutex
	reads      int
	pages      int
	lastIssued string
}

func (fr *fakeRun) isFinal() bool { return fr.reads > fr.runningReads }

func (fr *fakeRun) visible() int {
	if fr.isFinal() {
		return len(fr.events)
	}
	return min(len(fr.events), fr.initial+fr.reads*fr.perRead)
}

func (fr *fakeRun) register(e *eng) {
	if fr.pageSize == 0 {
		fr.pageSize = 2
	}
	e.on("GET /runs/"+fr.id, func(w http.ResponseWriter, _ req) {
		fr.mu.Lock()
		fr.reads++
		var body map[string]any
		if fr.isFinal() {
			body = fr.final
		} else {
			body = map[string]any{}
			for k, v := range fr.final {
				body[k] = v
			}
			body["status"] = "running"
			for _, k := range []string{"outcome", "ended_at", "reason", "expires_at"} {
				delete(body, k)
			}
		}
		fr.mu.Unlock()
		writeJSON(w, 200, body)
	})
	e.on("GET /runs/"+fr.id+"/events", func(w http.ResponseWriter, c req) {
		fr.mu.Lock()
		fr.pages++
		n := fr.pages
		first := n == 1
		fr.mu.Unlock()
		if first && fr.onFirstPage != nil {
			fr.onFirstPage()
		}
		if fr.eventsAnswer != nil && fr.eventsAnswer(w, n) {
			return
		}
		fr.mu.Lock()
		defer fr.mu.Unlock()
		tok := c.Query.Get("page_token")
		if tok != fr.lastIssued {
			fr.t.Errorf("listRunEvents call %d: page_token %q, want the last next_page_token %q", n, tok, fr.lastIssued)
		}
		from := 0
		if tok != "" {
			if _, err := fmt.Sscanf(tok, "after-%d", &from); err != nil {
				writeProblem(w, 400, prob("InvalidRequest", "InvalidRequest", "not a page token this list issued"))
				return
			}
		}
		vis := fr.visible()
		to := min(from+fr.pageSize, vis)
		if from > to {
			from = to
		}
		items := fr.events[from:to]
		body := map[string]any{"items": items}
		ended := fr.isFinal() && fr.final["status"] == "ended"
		if to < vis || !ended {
			next := fmt.Sprintf("after-%d", to)
			body["next_page_token"] = next
			fr.lastIssued = next
		} else {
			fr.lastIssued = "(no further page: the last page had no next_page_token)"
		}
		writeJSON(w, 200, body)
	})
	e.on("GET /operations/"+fr.op, func(w http.ResponseWriter, _ req) {
		fr.mu.Lock()
		final := fr.isFinal()
		fr.mu.Unlock()
		if final {
			writeJSON(w, 200, fr.finalOp)
			return
		}
		writeJSON(w, 200, operation(fr.op, "run", "running", fr.id, nil))
	})
}

func (fr *fakeRun) pageCalls() int {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	return fr.pages
}

// ---- the record every test prints ----

var (
	rhAgent  = strings.Repeat("1", 64)
	rhIn1    = strings.Repeat("2", 64)
	rhIn2    = strings.Repeat("3", 64)
	rhArt    = strings.Repeat("4", 64)
	rhReuse  = strings.Repeat("5", 64)
	rhArt2   = strings.Repeat("6", 64)
	rhReuse2 = strings.Repeat("7", 64)
	rhArt3   = strings.Repeat("8", 64)
	rhReuse3 = strings.Repeat("9", 64)
)

func at(seq int) string { return fmt.Sprintf("2026-11-14T10:%02d:%02dZ", seq/60, seq%60) }

func ev(seq int, run, kind string, fields map[string]any) map[string]any {
	m := map[string]any{"seq": seq, "at": at(seq), "run": run, "kind": kind}
	for k, v := range fields {
		m[k] = v
	}
	return m
}

func attempt(step string, n int, worker string) map[string]any {
	m := map[string]any{"step": step, "attempt": n, "worker": worker, "pattern": pattern,
		"inputs": []any{rhIn1, rhIn2}, "artifacts": []any{rhArt}, "reuse_key": rhReuse}
	if strings.HasPrefix(worker, "agent:") {
		m["model"] = "model-a"
		m["agent_digest"] = rhAgent
	}
	return m
}

func with(m map[string]any, kv ...any) map[string]any {
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

// fullRecord is a record with every kind of line the contract's table gives fields to, and the lines
// the client prints for it.
func fullRecord(run string) ([]map[string]any, []string) {
	evs := []map[string]any{
		ev(1, run, "RunStarted", map[string]any{"slice": "S-1", "pattern": pattern}),
		ev(2, run, "StepStarted", map[string]any{"step": "tests"}),
		ev(3, run, "AttemptMade", with(attempt("tests", 1, "agent:test-writer"),
			"usage", map[string]any{"tokens": 1200, "cost_usd": 0.0123}, "refs", []any{"R-1"})),
		ev(4, run, "CheckPassed", map[string]any{"step": "tests", "attempt": 1, "refs": []any{"R-1", "R-1/E-1"},
			"check": map[string]any{"name": "TestsFailAgainstStub", "passed": true, "reason": "the tests fail against the stub"}}),
		ev(5, run, "StepAccepted", with(attempt("tests", 1, "agent:test-writer"),
			"changed_lines", []any{map[string]any{"file": "tests/a_test.go", "line": 3}, map[string]any{"file": "tests/b_test.go", "line": 10}},
			"detail", "accepted at once")),
		ev(6, run, "StepStarted", map[string]any{"step": "code"}),
		ev(7, run, "StepRejected", map[string]any{"step": "code", "attempt": 1,
			"usage": map[string]any{"tokens": 50, "cost_usd": 0.5}, "detail": "the worker failed"}),
		ev(8, run, "CheckFailed", map[string]any{"step": "code", "attempt": 2, "refs": []any{"R-1"}, "detail": "see the log",
			"check": map[string]any{"name": "AllTestsPass", "passed": false, "reason": "2 tests fail"}}),
		ev(9, run, "ResultReused", map[string]any{"step": "code", "artifacts": []any{rhArt2}, "reuse_key": rhReuse2}),
		ev(10, run, "FindingRecorded", map[string]any{"step": "review", "attempt": 1, "round": 1,
			"finding": map[string]any{"rule": "R-1", "file": "src/a.go", "line": 3, "text": "the refund is not checked",
				"blocking": true, "check": "AllTestsPass"}}),
		ev(11, run, "FindingRecorded", map[string]any{"step": "review", "round": 1,
			"finding": map[string]any{"rule": "R-2", "text": "prefer a table", "blocking": false}}),
		ev(12, run, "StepAccepted", with(attempt("merge", 1, "procedure:merge"),
			"inputs", []any{rhArt2}, "artifacts", []any{rhArt3}, "reuse_key", rhReuse3)),
		ev(13, run, "RunCompleted", nil),
	}
	lines := []string{
		"1  " + at(1) + "  RunStarted  slice S-1  pattern tdd@1.0.0",
		"2  " + at(2) + "  StepStarted  tests",
		"3  " + at(3) + "  AttemptMade  tests  attempt 1  worker agent:test-writer  model model-a  agent " + rhAgent +
			"  inputs " + rhIn1 + "," + rhIn2 + "  artifacts " + rhArt + "  reuse " + rhReuse + "  tokens 1200  cost $0.0123  refs R-1",
		"4  " + at(4) + "  CheckPassed  tests  attempt 1  TestsFailAgainstStub  refs R-1,R-1/E-1  the tests fail against the stub",
		"5  " + at(5) + "  StepAccepted  tests  attempt 1  worker agent:test-writer  model model-a  agent " + rhAgent +
			"  inputs " + rhIn1 + "," + rhIn2 + "  artifacts " + rhArt + "  reuse " + rhReuse +
			"  changed tests/a_test.go:3,tests/b_test.go:10  accepted at once",
		"6  " + at(6) + "  StepStarted  code",
		"7  " + at(7) + "  StepRejected  code  attempt 1  tokens 50  cost $0.5000  the worker failed",
		"8  " + at(8) + "  CheckFailed  code  attempt 2  AllTestsPass  refs R-1  2 tests fail  see the log",
		"9  " + at(9) + "  ResultReused  code  artifacts " + rhArt2 + "  reuse " + rhReuse2,
		"10  " + at(10) + "  FindingRecorded  review  attempt 1  round 1  blocking  R-1  src/a.go:3  check AllTestsPass  the refund is not checked",
		"11  " + at(11) + "  FindingRecorded  review  round 1  suggestion  R-2  prefer a table",
		"12  " + at(12) + "  StepAccepted  merge  attempt 1  worker procedure:merge  inputs " + rhArt2 + "  artifacts " + rhArt3 + "  reuse " + rhReuse3,
		"13  " + at(13) + "  RunCompleted",
	}
	return evs, lines
}

// endState is one row of the contract's "Run states and exit codes" with its end line.
type endState struct {
	name, status, outcome, reason, opStatus string
	last                                    map[string]any // the record's last event, after RunStarted and StepStarted
	lastLine                                string
	endLine                                 string
	exit                                    int
}

func endStates(run, op string) []endState {
	return []endState{
		{name: "done", status: "ended", outcome: "done", opStatus: "succeeded",
			last: ev(3, run, "RunCompleted", nil), lastLine: "3  " + at(3) + "  RunCompleted",
			endLine: run + " ended done", exit: 0},
		{name: "waiting-for-developer", status: "waiting-for-developer", reason: "the code step ran out of attempts", opStatus: "running",
			last:     ev(3, run, "RunHandedOver", map[string]any{"detail": "the code step ran out of attempts"}),
			lastLine: "3  " + at(3) + "  RunHandedOver  the code step ran out of attempts",
			endLine: run + " waits for the developer: the code step ran out of attempts; it expires " + tExpires +
				". Take it over with take-over " + run + ", or cancel it with cancel " + run + ".", exit: 2},
		{name: "interrupted", status: "interrupted", reason: "the platform stopped", opStatus: "running",
			last: ev(3, run, "RunInterrupted", nil), lastLine: "3  " + at(3) + "  RunInterrupted",
			endLine: run + " is interrupted: the platform stopped; it expires " + tExpires +
				". Resume it with resume " + run + ", or cancel it with cancel " + run + ".", exit: 2},
		{name: "handed-over", status: "ended", outcome: "handed-over", reason: "taken over by the developer", opStatus: "succeeded",
			last: ev(3, run, "HandOverAccepted", nil), lastLine: "3  " + at(3) + "  HandOverAccepted",
			endLine: run + " ended handed-over: taken over by the developer", exit: 2},
		{name: "expired", status: "ended", outcome: "expired", reason: "waited 14 days for the developer", opStatus: "succeeded",
			last: ev(3, run, "RunExpired", nil), lastLine: "3  " + at(3) + "  RunExpired",
			endLine: run + " ended expired: waited 14 days for the developer", exit: 5},
		{name: "cancelled", status: "ended", outcome: "cancelled", opStatus: "cancelled",
			last: ev(3, run, "RunCancelled", nil), lastLine: "3  " + at(3) + "  RunCancelled",
			endLine: run + " ended cancelled", exit: 6},
	}
}

// stateRun is a short run that ends in s after running for runningReads getRun answers.
func stateRun(t *testing.T, run, op string, s endState, runningReads int) *fakeRun {
	var result map[string]any
	if s.opStatus == "succeeded" {
		result = map[string]any{"run": run}
	}
	return &fakeRun{t: t, id: run, op: op,
		final:   runJSON(runSpec{id: run, op: op, status: s.status, outcome: s.outcome, reason: s.reason}),
		finalOp: operation(op, "run", s.opStatus, run, result),
		events: []map[string]any{
			ev(1, run, "RunStarted", map[string]any{"slice": "S-1", "pattern": pattern}),
			ev(2, run, "StepStarted", map[string]any{"step": "tests"}),
			s.last,
		},
		initial: 1, perRead: 1, runningReads: runningReads}
}

func stateLines(run string, s endState) []string {
	return []string{"1  " + at(1) + "  RunStarted  slice S-1  pattern tdd@1.0.0", "2  " + at(2) + "  StepStarted  tests", s.lastLine}
}

func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }
