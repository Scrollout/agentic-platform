// Tests for the terminal client's Story 1 commands (T039): describe, questions, show, edit, approve,
// slices, cancel, delete. They are written before the client exists and fail until it does.
//
// The API these tests drive, which the client must provide in package cli (module root, src/cli):
//
//	type Env struct{ EngineURL, EngineToken, Editor string } // ENGINE_URL, ENGINE_TOKEN, EDITOR
//	func Run(ctx context.Context, args []string, env Env, stdin io.Reader, stdout, stderr io.Writer) int
//
// Why a Run function and not a built binary: the commands are I/O around an HTTP client, so a function
// taking its arguments, environment and streams lets a test run the whole command in process against
// an httptest engine, with no process, no real environment variables and no terminal, and see the exit
// code as a return value. The binary's main only calls Run(ctx, os.Args[1:], envFromOS, os.Stdin,
// os.Stdout, os.Stderr) and exits with the result.
//
// Conventions these tests pin that contract.md leaves open (change them with the contract):
//   - `describe` takes the description from its arguments joined by spaces, else from standard input.
//   - `questions` reads one answer per open question from standard input, one per line: a number is the
//     option's position from 0 (as the API numbers it), any other text is a free answer.
//   - `edit` puts the part in a temporary file and runs `$EDITOR <file>`. goals and rules are the JSON of
//     the replace body ({"goals":...} and {"rules","examples","terms"}); contract is the YAML text itself.
//     It then calls the replace operation with the file's content and follows the operation. When the
//     slice then has a failing check it exits 1 and prints the check.
//   - `approve` approves the hash of the getSlice it just made and showed; PartFailsChecks exits 1,
//     every other refusal exits 4.
//   - `cancel` exits 0 once the operation is cancelled; an operation that ends cancelled under another
//     command (here describe) makes that command exit 6.
//   - A refused request prints the problem's `code` on standard error.
package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	cli "github.com/agentic-platform/cli"
)

const (
	apiVersion = "2026-11-12-preview"
	token      = "client-token-1"
)

var (
	hGoals    = strings.Repeat("a", 64)
	hRules    = strings.Repeat("b", 64)
	hContract = strings.Repeat("c", 64)
	hStale    = strings.Repeat("d", 64)
)

// ---- fake engine ----

type call struct {
	Method, Path string
	Query        url.Values
	Body         []byte
}

func (c call) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(c.Body, &m); err != nil {
		t.Fatalf("body of %s %s is not JSON: %v (%q)", c.Method, c.Path, err, c.Body)
	}
	return m
}

type fakeEngine struct {
	t      *testing.T
	mu     sync.Mutex
	calls  []call
	routes map[string]func(w http.ResponseWriter, c call)
	srv    *httptest.Server
}

func newFake(t *testing.T) *fakeEngine {
	f := &fakeEngine{t: t, routes: map[string]func(http.ResponseWriter, call){}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeEngine) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c := call{r.Method, r.URL.Path, r.URL.Query(), body}
	if r.Header.Get("Authorization") != "Bearer "+token {
		f.t.Errorf("%s %s: Authorization = %q, want the client token", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		problem(w, 401, "Unauthorized")
		return
	}
	if c.Query.Get("api-version") != apiVersion {
		f.t.Errorf("%s %s: api-version = %q", r.Method, r.URL.Path, c.Query.Get("api-version"))
		problem(w, 400, "UnsupportedApiVersionValue")
		return
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	h := f.routes[r.Method+" "+r.URL.Path]
	f.mu.Unlock()
	if h == nil {
		f.t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		problem(w, 404, "NotFound")
		return
	}
	h(w, c)
}

func (f *fakeEngine) on(route string, h func(w http.ResponseWriter, c call)) {
	f.mu.Lock()
	f.routes[route] = h
	f.mu.Unlock()
}

// reply registers a route answering a fixed status and JSON body.
func (f *fakeEngine) reply(route string, status int, v any) {
	f.on(route, func(w http.ResponseWriter, _ call) { writeJSON(w, status, v) })
}

func (f *fakeEngine) refuse(route string, status int, code string) {
	f.on(route, func(w http.ResponseWriter, _ call) { problem(w, status, code) })
}

// accept makes a state-changing route answer 202 with operation id, and the operation succeed with result.
func (f *fakeEngine) accept(route, id, kind, target string, result map[string]any) {
	f.reply(route, 202, operation(id, kind, "accepted", target, nil))
	f.reply("GET /operations/"+id, 200, operation(id, kind, "succeeded", target, result))
}

func (f *fakeEngine) called(method, path string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.Method == method && c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeEngine) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func problem(w http.ResponseWriter, status int, code string, failures ...map[string]any) {
	p := map[string]any{"type": "https://example.invalid/problems/" + code, "title": code, "status": status, "code": code}
	if len(failures) > 0 {
		p["failures"] = failures
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

func operation(id, kind, status, target string, result map[string]any) map[string]any {
	o := map[string]any{"id": id, "kind": kind, "status": status, "target": target,
		"created_at": "2026-11-12T10:00:00Z", "updated_at": "2026-11-12T10:00:01Z"}
	if result != nil {
		o["result"] = result
	}
	return o
}

// ---- fixtures built in code ----

type sliceOpts struct {
	id, title, state string
	questions        []map[string]any
	checks           []map[string]any
	hashes           [3]string
}

func slice(o sliceOpts) map[string]any {
	if o.id == "" {
		o.id = "S-1"
	}
	if o.title == "" {
		o.title = "Customer cancels an order"
	}
	if o.state == "" {
		o.state = "checked"
	}
	if o.hashes == [3]string{} {
		o.hashes = [3]string{hGoals, hRules, hContract}
	}
	if o.questions == nil {
		o.questions = []map[string]any{}
	}
	if o.checks == nil {
		o.checks = []map[string]any{{"part": "goals", "check": "GoalsPresent", "passed": true, "reason": "one goal"}}
	}
	return map[string]any{
		"id": o.id, "title": o.title, "description": "a customer can cancel an order", "state": o.state,
		"goals": []any{map[string]any{"id": "G-1", "text": "customer cancels an unshipped order"}},
		"terms": []any{map[string]any{"id": "T-1", "name": "Unshipped", "definition": "not yet handed to the carrier"}},
		"rules": []any{map[string]any{"id": "R-1", "goal": "G-1", "pattern": "ubiquitous", "system": "the shop",
			"response": "refund the order", "terms": []any{"T-1"}}},
		"rule_text": map[string]any{"R-1": "The shop shall refund the order, rule-sentence-marker."},
		"examples":  "@R-1\nRule: refund\n  @E-1\n  Scenario: refund\n",
		"contract":  "openapi: 3.1.0\ninfo:\n  title: contract-text-marker\n",
		"questions": o.questions,
		"parts":     map[string]any{"goals": o.hashes[0], "rules": o.hashes[1], "contract": o.hashes[2]},
		"approvals": []any{},
		"checks":    o.checks,
		"history":   []any{map[string]any{"kind": "proposed", "at": "2026-11-12T10:00:00Z", "model": "m", "agent_digest": hGoals, "refs": []any{}}},
	}
}

func failedCheck(part, name, reason string) map[string]any {
	return map[string]any{"part": part, "check": name, "passed": false, "reason": reason}
}

// ---- running the client ----

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type result struct {
	code     int
	out, err string
}

func (f *fakeEngine) env() cli.Env { return cli.Env{EngineURL: f.srv.URL, EngineToken: token} }

func runEnv(t *testing.T, env cli.Env, stdin string, args ...string) result {
	t.Helper()
	var out, errb syncBuf
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	code := cli.Run(ctx, args, env, strings.NewReader(stdin), &out, &errb)
	return result{code, out.String(), errb.String()}
}

func (f *fakeEngine) run(stdin string, args ...string) result {
	f.t.Helper()
	return runEnv(f.t, f.env(), stdin, args...)
}

func wantExit(t *testing.T, r result, code int) {
	t.Helper()
	if r.code != code {
		t.Fatalf("exit = %d, want %d\nstdout: %s\nstderr: %s", r.code, code, r.out, r.err)
	}
}

func wantContains(t *testing.T, what, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("%s lacks %q:\n%s", what, sub, s)
		}
	}
}

func wantAbsent(t *testing.T, what, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			t.Errorf("%s has %q:\n%s", what, sub, s)
		}
	}
}

// ---- usage and environment ----

func TestUsageErrorsExit3WithoutCallingTheEngine(t *testing.T) {
	cases := [][]string{
		{},
		{"frobnicate"},
		{"show"},
		{"show", "not-a-slice"},
		{"show", "S-1", "bogus"},
		{"edit", "S-1"},
		{"edit", "S-1", "terms"},
		{"approve", "S-1"},
		{"approve", "S-1", "bogus"},
		{"questions"},
		{"cancel", "S-1"},
		{"cancel"},
		{"delete"},
		{"delete", "RUN-1"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			f := newFake(t)
			env := f.env()
			env.Editor = "/bin/true"
			r := runEnv(t, env, "", args...)
			wantExit(t, r, 3)
			if r.err == "" {
				t.Error("a usage error prints a message on standard error")
			}
			if f.count() != 0 {
				t.Errorf("the engine was called %d times", f.count())
			}
		})
	}
}

func TestMissingEngineURLOrTokenIsAUsageError(t *testing.T) {
	for _, env := range []cli.Env{{EngineToken: token}, {EngineURL: "http://127.0.0.1:1"}} {
		wantExit(t, runEnv(t, env, "", "slices"), 3)
	}
}

// ---- describe ----

func describeRoutes(f *fakeEngine, sl map[string]any) {
	f.accept("POST /slices", "OP-1", "propose-slice", "S-1", map[string]any{"slice": "S-1"})
	f.reply("GET /slices/S-1", 200, sl)
}

func TestDescribeFromArguments(t *testing.T) {
	f := newFake(t)
	q := []map[string]any{{"id": "Q-1", "about": "R-1", "text": "Refund to the card or to credit?", "options": []any{"card", "credit"}}}
	describeRoutes(f, slice(sliceOpts{state: "questions-open", questions: q}))
	r := f.run("", "describe", "a customer can", "cancel an order")
	wantExit(t, r, 0)
	posts := f.called("POST", "/slices")
	if len(posts) != 1 {
		t.Fatalf("createSlice calls = %d", len(posts))
	}
	if got := posts[0].json(t)["description"]; got != "a customer can cancel an order" {
		t.Errorf("description = %q", got)
	}
	if len(f.called("GET", "/operations/OP-1")) == 0 || len(f.called("GET", "/slices/S-1")) == 0 {
		t.Error("describe follows the operation, then reads the slice")
	}
	wantContains(t, "stdout", r.out, "S-1", "Customer cancels an order", "questions-open")
}

func TestDescribeFromStandardInputWhenNoArguments(t *testing.T) {
	f := newFake(t)
	describeRoutes(f, slice(sliceOpts{}))
	wantExit(t, f.run("a customer can cancel\nan order, line two", "describe"), 0)
	if got := f.called("POST", "/slices")[0].json(t)["description"]; got != "a customer can cancel\nan order, line two" {
		t.Errorf("description = %q", got)
	}
}

func TestDescribeReusedSliceIsShown(t *testing.T) {
	f := newFake(t)
	f.accept("POST /slices", "OP-1", "propose-slice", "S-3", map[string]any{"reused_slice": "S-3"})
	f.reply("GET /slices/S-3", 200, slice(sliceOpts{id: "S-3", title: "Reused one"}))
	r := f.run("", "describe", "again")
	wantExit(t, r, 0)
	wantContains(t, "stdout", r.out, "S-3", "Reused one")
}

func TestDescribeRefusedPrintsTheProblemCode(t *testing.T) {
	f := newFake(t)
	f.refuse("POST /slices", 400, "NarrowDescription")
	r := f.run("", "describe", "everything")
	wantExit(t, r, 4)
	wantContains(t, "stderr", r.err, "NarrowDescription")
}

func TestDescribeOperationFailedWithProblemExits4(t *testing.T) {
	f := newFake(t)
	op := operation("OP-1", "propose-slice", "failed", "S-1", nil)
	op["error"] = map[string]any{"type": "x", "title": "TooManyQuestions", "code": "TooManyQuestions"}
	f.reply("POST /slices", 202, operation("OP-1", "propose-slice", "accepted", "S-1", nil))
	f.reply("GET /operations/OP-1", 200, op)
	r := f.run("", "describe", "too broad")
	wantExit(t, r, 4)
	wantContains(t, "stderr", r.err, "TooManyQuestions")
}

func TestDescribeOperationCancelledExits6(t *testing.T) {
	f := newFake(t)
	f.reply("POST /slices", 202, operation("OP-1", "propose-slice", "accepted", "S-1", nil))
	f.reply("GET /operations/OP-1", 200, operation("OP-1", "propose-slice", "cancelled", "S-1", nil))
	wantExit(t, f.run("", "describe", "x"), 6)
}

// ---- questions ----

func openQuestionsSlice() map[string]any {
	return slice(sliceOpts{state: "questions-open", questions: []map[string]any{
		{"id": "Q-1", "about": "R-1", "text": "Refund to the card or to credit?", "options": []any{"card", "credit"}},
		{"id": "Q-2", "about": "G-1", "text": "Which orders count as unshipped?"},
		{"id": "Q-3", "about": "T-1", "text": "Already answered question", "answer": map[string]any{"option": 0}, "answered_at": "2026-11-12T10:00:00Z"},
	}})
}

func TestQuestionsShowsOpenOnesAndRecordsAnswers(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, openQuestionsSlice())
	f.accept("POST /slices/S-1/answers", "OP-2", "answer-questions", "S-1", map[string]any{"slice": "S-1"})
	r := f.run("1\nthose not handed to the carrier\n", "questions", "S-1")
	wantExit(t, r, 0)
	wantContains(t, "stdout", r.out, "Q-1", "Refund to the card or to credit?", "card", "credit", "Q-2", "Which orders count as unshipped?")
	wantAbsent(t, "stdout", r.out, "Already answered question")
	posts := f.called("POST", "/slices/S-1/answers")
	if len(posts) != 1 {
		t.Fatalf("answerQuestions calls = %d", len(posts))
	}
	want := map[string]any{"answers": []any{
		map[string]any{"question": "Q-1", "answer": map[string]any{"option": float64(1)}},
		map[string]any{"question": "Q-2", "answer": map[string]any{"text": "those not handed to the carrier"}},
	}}
	if got := posts[0].json(t); !reflect.DeepEqual(got, want) {
		t.Errorf("answers body = %v, want %v", got, want)
	}
	if len(f.called("GET", "/operations/OP-2")) == 0 {
		t.Error("questions follows the answer operation")
	}
}

func TestQuestionsWithNoneOpenAnswersNothing(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, slice(sliceOpts{}))
	r := f.run("", "questions", "S-1")
	wantExit(t, r, 0)
	wantContains(t, "stdout", strings.ToLower(r.out), "no open questions")
	if len(f.called("POST", "/slices/S-1/answers")) != 0 {
		t.Error("nothing to answer, nothing posted")
	}
}

func TestQuestionsOptionOutOfRangeIsUsageAndPostsNothing(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, openQuestionsSlice())
	wantExit(t, f.run("7\nfree\n", "questions", "S-1"), 3)
	if len(f.called("POST", "/slices/S-1/answers")) != 0 {
		t.Error("an invalid answer must not be posted")
	}
}

func TestQuestionsRefusedPrintsCode(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, openQuestionsSlice())
	f.refuse("POST /slices/S-1/answers", 400, "InvalidRequest")
	r := f.run("0\nfree\n", "questions", "S-1")
	wantExit(t, r, 4)
	wantContains(t, "stderr", r.err, "InvalidRequest")
}

// ---- show ----

func failingSlice() map[string]any {
	return slice(sliceOpts{checks: []map[string]any{
		{"part": "goals", "check": "GoalsPresent", "passed": true, "reason": "one goal"},
		failedCheck("rules", "ExampleTagged", "scenario lacks an @E tag"),
	}})
}

func TestShowWholeSliceHasPartsHashesAndChecks(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, failingSlice())
	r := f.run("", "show", "S-1")
	wantExit(t, r, 0)
	wantContains(t, "stdout", r.out, "customer cancels an unshipped order", "rule-sentence-marker", "contract-text-marker",
		hGoals, hRules, hContract, "ExampleTagged", "scenario lacks an @E tag", "GoalsPresent")
	if f.count() != 1 {
		t.Errorf("show makes one call, made %d", f.count())
	}
}

func TestShowOnePartShowsOnlyThatPart(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, failingSlice())
	r := f.run("", "show", "S-1", "goals")
	wantExit(t, r, 0)
	wantContains(t, "stdout", r.out, "customer cancels an unshipped order", hGoals)
	wantAbsent(t, "stdout", r.out, "rule-sentence-marker", "contract-text-marker", hRules, hContract)
	r = f.run("", "show", "S-1", "contract")
	wantContains(t, "stdout", r.out, "contract-text-marker", hContract)
	wantAbsent(t, "stdout", r.out, hGoals)
}

func TestShowUnknownSliceExits4WithCode(t *testing.T) {
	f := newFake(t)
	f.refuse("GET /slices/S-9", 404, "NotFound")
	r := f.run("", "show", "S-9")
	wantExit(t, r, 4)
	wantContains(t, "stderr", r.err, "NotFound")
}

// ---- edit ----

// editor writes a script that saves the file it is given to dir/seen and replaces it with newContent.
func editor(t *testing.T, newContent string) (path, seen string) {
	t.Helper()
	dir := t.TempDir()
	path, seen = filepath.Join(dir, "ed.sh"), filepath.Join(dir, "seen")
	script := fmt.Sprintf("#!/bin/sh\ncp \"$1\" %q\ncat > \"$1\" <<'AP_EOF'\n%sAP_EOF\n", seen, newContent)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, seen
}

func editFake(t *testing.T, part string, sl map[string]any) *fakeEngine {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, sl)
	f.accept("PUT /slices/S-1/"+part, "OP-3", "replace-"+part, "S-1", map[string]any{"slice": "S-1"})
	return f
}

func runEdit(f *fakeEngine, ed, part string) result {
	env := f.env()
	env.Editor = ed
	return runEnv(f.t, env, "", "edit", "S-1", part)
}

func TestEditGoalsOpensEditorOnThePartAndReplacesIt(t *testing.T) {
	f := editFake(t, "goals", slice(sliceOpts{}))
	ed, seen := editor(t, `{"goals":[{"id":"G-1","text":"customer cancels any unshipped order"}]}`+"\n")
	r := runEdit(f, ed, "goals")
	wantExit(t, r, 0)
	b, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("the editor was not run on a file: %v", err)
	}
	wantContains(t, "the file the editor opened", string(b), "customer cancels an unshipped order")
	puts := f.called("PUT", "/slices/S-1/goals")
	if len(puts) != 1 {
		t.Fatalf("replaceGoals calls = %d", len(puts))
	}
	want := map[string]any{"goals": []any{map[string]any{"id": "G-1", "text": "customer cancels any unshipped order"}}}
	if got := puts[0].json(t); !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
	if len(f.called("GET", "/operations/OP-3")) == 0 {
		t.Error("edit follows the replace operation")
	}
}

func TestEditRulesHoldsRulesExamplesAndTerms(t *testing.T) {
	f := editFake(t, "rules", slice(sliceOpts{}))
	newBody := `{"rules":[{"id":"R-1","goal":"G-1","pattern":"ubiquitous","system":"the shop","response":"refund it all","terms":[]}],` +
		`"examples":"@R-1\nRule: r\n","terms":[]}`
	ed, seen := editor(t, newBody+"\n")
	wantExit(t, runEdit(f, ed, "rules"), 0)
	b, _ := os.ReadFile(seen)
	wantContains(t, "the file the editor opened", string(b), "refund the order", "Unshipped", "@E-1")
	puts := f.called("PUT", "/slices/S-1/rules")
	if len(puts) != 1 {
		t.Fatalf("replaceRules calls = %d", len(puts))
	}
	got := puts[0].json(t)
	for _, k := range []string{"rules", "examples", "terms"} {
		if _, ok := got[k]; !ok {
			t.Errorf("body lacks %q: %v", k, got)
		}
	}
	if got["examples"] != "@R-1\nRule: r\n" {
		t.Errorf("examples = %q, want a real line break text", got["examples"])
	}
}

func TestEditContractIsTheYAMLTextItself(t *testing.T) {
	f := editFake(t, "contract", slice(sliceOpts{}))
	newText := "openapi: 3.1.0\ninfo:\n  title: edited\n"
	ed, seen := editor(t, newText)
	wantExit(t, runEdit(f, ed, "contract"), 0)
	b, _ := os.ReadFile(seen)
	if string(b) != "openapi: 3.1.0\ninfo:\n  title: contract-text-marker\n" {
		t.Errorf("the editor opened %q, want the contract text exactly", b)
	}
	puts := f.called("PUT", "/slices/S-1/contract")
	if len(puts) != 1 {
		t.Fatalf("replaceContract calls = %d", len(puts))
	}
	if got := puts[0].json(t)["contract"]; got != newText {
		t.Errorf("contract = %q, want %q", got, newText)
	}
}

func TestEditThatLeavesAFailingCheckExits1AndPrintsIt(t *testing.T) {
	f := editFake(t, "rules", failingSlice())
	ed, _ := editor(t, `{"rules":[{"id":"R-1","goal":"G-1","pattern":"ubiquitous","system":"s","response":"r","terms":[]}],"examples":"x\n","terms":[]}`+"\n")
	r := runEdit(f, ed, "rules")
	wantExit(t, r, 1)
	wantContains(t, "output", r.out+r.err, "ExampleTagged", "scenario lacks an @E tag")
}

func TestEditRefusedWhileARunIsActiveExits4(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, slice(sliceOpts{}))
	f.refuse("PUT /slices/S-1/goals", 409, "RunActive")
	ed, _ := editor(t, `{"goals":[{"id":"G-1","text":"x"}]}`+"\n")
	r := runEdit(f, ed, "goals")
	wantExit(t, r, 4)
	wantContains(t, "stderr", r.err, "RunActive")
}

func TestEditFailingEditorReplacesNothing(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, slice(sliceOpts{}))
	r := runEdit(f, "/bin/false", "goals")
	if r.code == 0 {
		t.Error("a failed editor must not exit 0")
	}
	if len(f.called("PUT", "/slices/S-1/goals")) != 0 {
		t.Error("nothing is replaced when the editor fails")
	}
}

func TestEditWithoutEditorIsAUsageError(t *testing.T) {
	f := newFake(t)
	wantExit(t, f.run("", "edit", "S-1", "goals"), 3)
}

// ---- approve ----

func TestApproveUsesTheHashJustShown(t *testing.T) {
	f := newFake(t)
	var mu sync.Mutex
	gets := 0
	f.on("GET /slices/S-1", func(w http.ResponseWriter, _ call) {
		mu.Lock()
		gets++
		n := gets
		mu.Unlock()
		h := hGoals // the first read; any later read sees the part changed
		if n > 1 {
			h = hStale
		}
		writeJSON(w, 200, slice(sliceOpts{hashes: [3]string{h, hRules, hContract}}))
	})
	f.accept("POST /slices/S-1/approvals", "OP-4", "approve-part", "S-1", map[string]any{"slice": "S-1"})
	r := f.run("", "approve", "S-1", "goals")
	wantExit(t, r, 0)
	posts := f.called("POST", "/slices/S-1/approvals")
	if len(posts) != 1 {
		t.Fatalf("approvePart calls = %d", len(posts))
	}
	want := map[string]any{"part": "goals", "content_hash": hGoals}
	if got := posts[0].json(t); !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
	wantContains(t, "stdout", r.out, hGoals, "goals")
	if len(f.called("GET", "/operations/OP-4")) == 0 {
		t.Error("approve follows the operation")
	}
}

func TestApproveStaleHashExits4WithCode(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, slice(sliceOpts{}))
	f.refuse("POST /slices/S-1/approvals", 409, "StaleContentHash")
	r := f.run("", "approve", "S-1", "rules")
	wantExit(t, r, 4)
	wantContains(t, "stderr", r.err, "StaleContentHash")
}

func TestApproveFailingChecksExits1AndListsFailures(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, failingSlice())
	f.on("POST /slices/S-1/approvals", func(w http.ResponseWriter, _ call) {
		problem(w, 409, "PartFailsChecks", failedCheck("rules", "ExampleTagged", "scenario lacks an @E tag"))
	})
	r := f.run("", "approve", "S-1", "rules")
	wantExit(t, r, 1)
	wantContains(t, "output", r.out+r.err, "PartFailsChecks", "ExampleTagged", "scenario lacks an @E tag")
}

func TestApproveOpenContradictionExits4(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, slice(sliceOpts{}))
	f.refuse("POST /slices/S-1/approvals", 409, "ContradictionOpen")
	r := f.run("", "approve", "S-1", "goals")
	wantExit(t, r, 4)
	wantContains(t, "stderr", r.err, "ContradictionOpen")
}

// ---- slices ----

func TestSlicesListsEveryPage(t *testing.T) {
	f := newFake(t)
	f.on("GET /slices", func(w http.ResponseWriter, c call) {
		if c.Query.Get("page_token") == "" {
			writeJSON(w, 200, map[string]any{"items": []any{
				map[string]any{"id": "S-2", "title": "Second slice", "state": "approved", "latest_run": "RUN-5"},
			}, "next_page_token": "tok2"})
			return
		}
		if c.Query.Get("page_token") != "tok2" {
			t.Errorf("page_token = %q", c.Query.Get("page_token"))
		}
		writeJSON(w, 200, map[string]any{"items": []any{
			map[string]any{"id": "S-1", "title": "First slice", "state": "proposed"},
		}})
	})
	r := f.run("", "slices")
	wantExit(t, r, 0)
	wantContains(t, "stdout", r.out, "S-2", "Second slice", "approved", "RUN-5", "S-1", "First slice", "proposed")
	if len(f.called("GET", "/slices")) != 2 {
		t.Errorf("list calls = %d, want 2", len(f.called("GET", "/slices")))
	}
}

func TestSlicesEmptyExits0(t *testing.T) {
	f := newFake(t)
	f.reply("GET /slices", 200, map[string]any{"items": []any{}})
	wantExit(t, f.run("", "slices"), 0)
}

// ---- cancel ----

func TestCancelOperation(t *testing.T) {
	f := newFake(t)
	f.reply("POST /operations/OP-4/cancel", 202, operation("OP-4", "run", "running", "RUN-2", nil))
	f.reply("GET /operations/OP-4", 200, operation("OP-4", "run", "cancelled", "RUN-2", nil))
	r := f.run("", "cancel", "OP-4")
	wantExit(t, r, 0)
	wantContains(t, "stdout", strings.ToLower(r.out), "cancelled")
	if len(f.called("POST", "/operations/OP-4/cancel")) != 1 {
		t.Error("cancelOperation is called once")
	}
}

func TestCancelRunFindsItsOperation(t *testing.T) {
	f := newFake(t)
	f.reply("GET /runs/RUN-2", 200, map[string]any{"id": "RUN-2", "slice": "S-1", "pattern": "p@1", "project": "demo",
		"status": "running", "operation": "OP-7", "created_at": "2026-11-12T10:00:00Z", "updated_at": "2026-11-12T10:00:01Z"})
	f.reply("POST /operations/OP-7/cancel", 202, operation("OP-7", "run", "running", "RUN-2", nil))
	f.reply("GET /operations/OP-7", 200, operation("OP-7", "run", "cancelled", "RUN-2", nil))
	wantExit(t, f.run("", "cancel", "RUN-2"), 0)
	if len(f.called("POST", "/operations/OP-7/cancel")) != 1 {
		t.Error("the run's operation is cancelled")
	}
}

func TestCancelEndedOperationExits4(t *testing.T) {
	f := newFake(t)
	f.refuse("POST /operations/OP-4/cancel", 409, "OperationEnded")
	r := f.run("", "cancel", "OP-4")
	wantExit(t, r, 4)
	wantContains(t, "stderr", r.err, "OperationEnded")
}

// ---- delete ----

func runsPage(slices ...string) map[string]any {
	items := []any{}
	for i, s := range slices {
		items = append(items, map[string]any{"id": fmt.Sprintf("RUN-%d", i+1), "slice": s, "pattern": "p@1", "project": "demo",
			"status": "ended", "outcome": "done", "operation": fmt.Sprintf("OP-%d", i+10), "created_at": "2026-11-12T10:00:00Z",
			"updated_at": "2026-11-12T10:00:01Z", "ended_at": "2026-11-12T10:00:01Z"})
	}
	return map[string]any{"items": items}
}

// deleteFake serves slice S-1 with RUN-1 (its run) and RUN-2 (another slice's). The DELETE route
// records the standard output the client had written when the slice was erased.
func deleteFake(t *testing.T, out *syncBuf, atDelete *string) *fakeEngine {
	f := newFake(t)
	f.reply("GET /slices/S-1", 200, slice(sliceOpts{}))
	f.reply("GET /runs", 200, runsPage("S-1", "S-2"))
	f.on("DELETE /slices/S-1", func(w http.ResponseWriter, _ call) {
		*atDelete = out.String()
		writeJSON(w, 202, operation("OP-9", "delete-slice", "accepted", "S-1", nil))
	})
	f.reply("GET /operations/OP-9", 200, operation("OP-9", "delete-slice", "succeeded", "S-1", map[string]any{"slice": "S-1"}))
	return f
}

func runDelete(f *fakeEngine, out *syncBuf, stdin io.Reader, args ...string) (int, string) {
	var errb syncBuf
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	code := cli.Run(ctx, args, f.env(), stdin, out, &errb)
	return code, errb.String()
}

func TestDeleteShowsWhatItErasesThenErasesAfterTheTypedSliceID(t *testing.T) {
	var out syncBuf
	var atDelete string
	f := deleteFake(t, &out, &atDelete)
	code, errs := runDelete(f, &out, strings.NewReader("S-1\n"), "delete", "S-1")
	if code != 0 {
		t.Fatalf("exit = %d\nstdout: %s\nstderr: %s", code, out.String(), errs)
	}
	if len(f.called("DELETE", "/slices/S-1")) != 1 {
		t.Fatalf("deleteSlice calls = %d", len(f.called("DELETE", "/slices/S-1")))
	}
	wantContains(t, "what was shown before the erase", atDelete, "S-1", "Customer cancels an order", "RUN-1")
	wantContains(t, "what was shown before the erase", strings.ToLower(atDelete), "project folders")
	wantAbsent(t, "what was shown before the erase", atDelete, "RUN-2")
	if len(f.called("GET", "/operations/OP-9")) == 0 {
		t.Error("delete follows the operation")
	}
}

func TestDeleteErasesNothingOnAnyOtherAnswer(t *testing.T) {
	for _, answer := range []string{"S-2\n", "yes\n", "s-1\n", "\n", ""} {
		t.Run(fmt.Sprintf("%q", answer), func(t *testing.T) {
			var out syncBuf
			var atDelete string
			f := deleteFake(t, &out, &atDelete)
			code, _ := runDelete(f, &out, strings.NewReader(answer), "delete", "S-1")
			if code == 0 {
				t.Error("a refused confirmation must not exit 0")
			}
			if len(f.called("DELETE", "/slices/S-1")) != 0 {
				t.Error("nothing is erased without the typed slice ID")
			}
		})
	}
}

type failReader struct{ t *testing.T }

func (r failReader) Read([]byte) (int, error) {
	r.t.Error("--yes must not read standard input")
	return 0, io.EOF
}

func TestDeleteYesSkipsTheQuestion(t *testing.T) {
	var out syncBuf
	var atDelete string
	f := deleteFake(t, &out, &atDelete)
	code, errs := runDelete(f, &out, failReader{t}, "delete", "S-1", "--yes")
	if code != 0 {
		t.Fatalf("exit = %d\nstderr: %s", code, errs)
	}
	if len(f.called("DELETE", "/slices/S-1")) != 1 {
		t.Error("--yes erases at once")
	}
}

func TestDeleteRefusedWhileARunIsActiveExits4(t *testing.T) {
	var out syncBuf
	var atDelete string
	f := deleteFake(t, &out, &atDelete)
	f.refuse("DELETE /slices/S-1", 409, "RunActive")
	code, errs := runDelete(f, &out, strings.NewReader("S-1\n"), "delete", "S-1")
	if code != 4 {
		t.Fatalf("exit = %d, want 4", code)
	}
	wantContains(t, "stderr", errs, "RunActive")
}
