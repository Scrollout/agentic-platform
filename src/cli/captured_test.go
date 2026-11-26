// T008 (feature 011, US1): the terminal client's captured-pattern commands and the proof outcome
// line, written against src/cli/contract.md ("Captured patterns (feature 011)", the command rows,
// "Run states and exit codes", "Problems", "Store text in the terminal") and the engine contract's
// tag `captured-patterns`, before the commands exist. Every command runs in process through cli.Run
// against the stand-in engine of runs_fake_test.go.
//
// Where the contract leaves an exact output text open, the tests check the facts, not the layout:
// the layout of `patterns` and `pattern <id>`, the line `capture`, `--drop`, `--add` and
// `approve-pattern` print on success, and the word for `usable`. Every name here starts with cp so
// no other new file collides with it.
package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// ---- fixtures ----

const (
	cpID    = "tdd-s-1"
	cpRef   = "tdd-s-1@1.0.0"
	cpProof = "S-9"
)

var cpKeyForm = regexp.MustCompile(`^[0-9a-f]{32}$`)

func cpPlace(part, in string, line, col int, form string) map[string]any {
	return map[string]any{"part": part, "in": in, "line": line, "column": col, "form": form}
}

func cpParam(name, kind, value string, places ...map[string]any) map[string]any {
	if len(places) == 0 {
		places = []map[string]any{cpPlace("slice/rules", "content", 2, 3, "lower")}
	}
	ps := make([]any, len(places))
	for i, p := range places {
		ps[i] = p
	}
	return map[string]any{"name": name, "kind": kind, "value": value, "source": "name", "origin": "proposed", "places": ps}
}

func cpProofRec(run, outcome string, failures ...map[string]any) map[string]any {
	fs := make([]any, len(failures))
	for i, f := range failures {
		fs[i] = f
	}
	return map[string]any{"run": run, "method": strings.Repeat("e", 64), "outcome": outcome, "failures": fs,
		"parameters": map[string]any{}}
}

// cpPattern is a CapturedPattern valid by the engine contract's schema.
func cpPattern(state string, usable bool, params []map[string]any, proofs ...map[string]any) map[string]any {
	ps := make([]any, len(params))
	for i, p := range params {
		ps[i] = p
	}
	prs := make([]any, len(proofs))
	for i, p := range proofs {
		prs[i] = p
	}
	h := func(c string) string { return strings.Repeat(c, 64) }
	return map[string]any{"id": cpID, "version": "1.0.0", "base": "tdd@1.0.0", "method": h("e"),
		"source":     map[string]any{"run": "RUN-3", "slice": "S-1"},
		"parts":      map[string]any{"goals": h("a"), "rules": h("b"), "contract": h("c")},
		"template":   map[string]any{"tests": h("1"), "code": h("2"), "witnesses": h("3")},
		"parameters": ps, "proofs": prs, "state": state, "usable": usable, "created_at": tCreated}
}

func cpTwoParams() []map[string]any {
	return []map[string]any{
		cpParam("note", "name", "note", cpPlace("slice/contract", "content", 37, 14, "capitalised"),
			cpPlace("src/note.go", "path", 1, 5, "lower")),
		cpParam("limit", "number", "10", cpPlace("slice/rules", "content", 4, 9, "exact")),
	}
}

// cpKey checks that exactly n requests were made and that each carries an Idempotency-Key.
func cpKey(t *testing.T, reqs []req, n int) string {
	t.Helper()
	if len(reqs) != n {
		t.Fatalf("requests = %d, want %d", len(reqs), n)
	}
	var first string
	for i, c := range reqs {
		k := c.Header.Get("Idempotency-Key")
		if !cpKeyForm.MatchString(k) {
			t.Errorf("request %d: Idempotency-Key = %q, want 32 lowercase hex digits", i+1, k)
		}
		if i == 0 {
			first = k
		}
	}
	return first
}

func cpBody(t *testing.T, c req) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(c.Body, &m); err != nil {
		t.Fatalf("body of %s %s is not JSON: %v (%q)", c.Method, c.Path, err, c.Body)
	}
	return m
}

// cpOp makes route answer 202 with operation id and that operation end as status with result (or
// with problem p when status is failed).
func cpOp(e *eng, route, id, kind, target, status string, result map[string]any, p map[string]any) {
	e.reply(route, 202, operation(id, kind, "accepted", target, nil))
	op := operation(id, kind, status, target, result)
	if p != nil {
		p["status"] = 422
		op["error"] = p
	}
	e.reply("GET /operations/"+id, 200, op)
}

func cpLines(s string) []string { return strings.Split(strings.TrimRight(s, "\n"), "\n") }

func cpHasLine(s, want string) bool {
	for _, l := range cpLines(s) {
		if strings.TrimSpace(l) == want {
			return true
		}
	}
	return false
}

func cpLineWith(s string, subs ...string) bool {
	for _, l := range cpLines(s) {
		ok := true
		for _, sub := range subs {
			ok = ok && strings.Contains(l, sub)
		}
		if ok {
			return true
		}
	}
	return false
}

func cpNoCalls(t *testing.T, e *eng, r result, code int) {
	t.Helper()
	wantExit(t, r, code)
	if r.err == "" {
		t.Error("a usage error prints a message on standard error")
	}
	if strings.Contains(r.err, "unknown command") {
		t.Errorf("stderr = %q: the command must exist, and refuse these arguments", r.err)
	}
	if r.out != "" {
		t.Errorf("stdout = %q, want nothing", r.out)
	}
	if e.total() != 0 {
		t.Errorf("the engine was called %d times", e.total())
	}
}

// ---- ap capture ----

// Covers T008: `capture` calls captureRun with an Idempotency-Key, follows the operation, reads the
// new captured pattern and prints its parameters (contract.md command table).
func TestCpCaptureCallsFollowsAndPrintsTheParameters(t *testing.T) {
	e := newEng(t)
	cpOp(e, "POST /captured-patterns", "OP-20", "capture-run", cpID, "succeeded", map[string]any{"captured_pattern": cpID}, nil)
	e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proposed", false, cpTwoParams()))
	r := e.run("capture", "RUN-3", "--name", "s-1")
	wantExit(t, r, 0)
	wantContains(t, "stdout", r.out, cpID, "note", "limit", "10")
	posts := e.got("POST", "/captured-patterns")
	cpKey(t, posts, 1)
	if b := cpBody(t, posts[0]); len(b) != 2 || b["run"] != "RUN-3" || b["name"] != "s-1" {
		t.Errorf("captureRun body = %s, want {run: RUN-3, name: s-1}", posts[0].Body)
	}
	if len(e.got("GET", "/operations/OP-20")) == 0 {
		t.Error("capture follows the operation")
	}
	if len(e.got("GET", "/captured-patterns/"+cpID)) == 0 {
		t.Error("capture reads the captured pattern the operation names")
	}
}

// Covers T008: without --name the body carries no name (the engine defaults it).
func TestCpCaptureWithoutNameSendsNoName(t *testing.T) {
	e := newEng(t)
	cpOp(e, "POST /captured-patterns", "OP-20", "capture-run", cpID, "succeeded", map[string]any{"captured_pattern": cpID}, nil)
	e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proposed", false, cpTwoParams()))
	wantExit(t, e.run("capture", "RUN-3"), 0)
	posts := e.got("POST", "/captured-patterns")
	if len(posts) != 1 || oneLine(string(posts[0].Body)) != `{"run":"RUN-3"}` {
		t.Errorf("captureRun body = %q, want {\"run\":\"RUN-3\"}", firstBody(posts))
	}
}

// Covers T008: the longest name the contract allows (40 characters) is sent unchanged (boundary).
func TestCpCaptureNameOfFortyCharactersIsSent(t *testing.T) {
	name := strings.Repeat("a", 20) + "-" + strings.Repeat("b", 19)
	if len(name) != 40 {
		t.Fatal("fixture: the name must be 40 characters")
	}
	e := newEng(t)
	cpOp(e, "POST /captured-patterns", "OP-20", "capture-run", "tdd-"+name, "succeeded", map[string]any{"captured_pattern": "tdd-" + name}, nil)
	p := cpPattern("proposed", false, cpTwoParams())
	p["id"] = "tdd-" + name
	e.reply("GET /captured-patterns/tdd-"+name, 200, p)
	wantExit(t, e.run("capture", "RUN-3", "--name", name), 0)
	if b := cpBody(t, e.got("POST", "/captured-patterns")[0]); b["name"] != name {
		t.Errorf("name sent = %v, want %q", b["name"], name)
	}
}

// Covers T008: a capture whose operation is still running is followed until it ends.
func TestCpCaptureFollowsARunningOperation(t *testing.T) {
	e := newEng(t)
	e.reply("POST /captured-patterns", 202, operation("OP-20", "capture-run", "accepted", cpID, nil))
	n := 0
	e.on("GET /operations/OP-20", func(w http.ResponseWriter, _ req) {
		n++
		if n < 3 {
			writeJSON(w, 200, operation("OP-20", "capture-run", "running", cpID, nil))
			return
		}
		writeJSON(w, 200, operation("OP-20", "capture-run", "succeeded", cpID, map[string]any{"captured_pattern": cpID}))
	})
	e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proposed", false, cpTwoParams()))
	r := e.run("capture", "RUN-3")
	wantExit(t, r, 0)
	if n < 3 {
		t.Errorf("operation reads = %d, want at least 3 (two running, then succeeded)", n)
	}
	if len(e.got("GET", "/captured-patterns/"+cpID)) == 0 {
		t.Error("the captured pattern is read only after the operation ends")
	}
}

// Covers T008: a capture with no parameter prints the contract's `No parameters` line.
func TestCpCaptureWithNoParameterPrintsTheNoParametersLine(t *testing.T) {
	e := newEng(t)
	cpOp(e, "POST /captured-patterns", "OP-20", "capture-run", cpID, "succeeded", map[string]any{"captured_pattern": cpID}, nil)
	e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proposed", false, nil))
	r := e.run("capture", "RUN-3")
	wantExit(t, r, 0)
	if !cpHasLine(r.out, "No parameters: only a slice identical to the source fits.") {
		t.Errorf("stdout lacks the No parameters line:\n%s", r.out)
	}
}

// Covers T008: every refusal at once is exit 4 with the problem on standard error and no
// operation followed (RunNotDone names the state; RunNotCapturable; CapturedPatternExists;
// NotFound; InvalidRequest).
func TestCpCaptureRefusalsExit4WithTheProblem(t *testing.T) {
	for _, c := range []struct {
		status       int
		code, detail string
	}{
		{409, "RunNotDone", "the run is waiting-for-developer"},
		{409, "RunNotCapturable", "the run was built under another base pattern version"},
		{409, "CapturedPatternExists", "tdd-s-3 was held by an erased one"},
		{404, "NotFound", "no run RUN-3"},
		{400, "InvalidRequest", "name: does not match the pattern"},
	} {
		t.Run(c.code, func(t *testing.T) {
			e := newEng(t)
			e.refuse("POST /captured-patterns", c.status, prob(c.code, c.code, c.detail))
			r := e.run("capture", "RUN-3")
			wantExit(t, r, 4)
			if want := "refused: " + c.code + ": " + c.detail + "\n"; r.err != want {
				t.Errorf("stderr = %q, want %q", r.err, want)
			}
			if r.out != "" {
				t.Errorf("stdout = %q, want nothing for a refusal", r.out)
			}
			if e.total() != 1 {
				t.Errorf("engine calls = %d, want only the refused one", e.total())
			}
		})
	}
}

// Covers T008: a capture whose operation fails is exit 4 with the problem, and no captured
// pattern is read (it does not exist: getCapturedPattern answers 404).
func TestCpCaptureOperationFailedExit4AndReadsNothing(t *testing.T) {
	e := newEng(t)
	cpOp(e, "POST /captured-patterns", "OP-20", "capture-run", cpID, "failed", nil,
		prob("OperationFailed", "OperationFailed", "the run's files are unreadable"))
	r := e.run("capture", "RUN-3")
	wantExit(t, r, 4)
	if want := "refused: OperationFailed: the run's files are unreadable\n"; r.err != want {
		t.Errorf("stderr = %q, want %q", r.err, want)
	}
	if r.out != "" {
		t.Errorf("stdout = %q, want nothing", r.out)
	}
	if e.anyPathWith("/captured-patterns/") {
		t.Error("a failed capture leaves no captured pattern, so none is read")
	}
}

// Covers T008: a cancelled capture operation is exit 6, as for any operation.
func TestCpCaptureOperationCancelledExit6(t *testing.T) {
	e := newEng(t)
	cpOp(e, "POST /captured-patterns", "OP-20", "capture-run", cpID, "cancelled", nil, nil)
	r := e.run("capture", "RUN-3")
	wantExit(t, r, 6)
	if e.anyPathWith("/captured-patterns/") {
		t.Error("a cancelled capture reads no captured pattern")
	}
}

// Covers T008 (usage): a missing or malformed run, a missing flag value, an extra argument or an
// unknown flag is exit 3 and the engine is not called.
func TestCpCaptureUsageErrorsExit3WithoutCalls(t *testing.T) {
	for _, args := range [][]string{
		{"capture"},
		{"capture", "S-1"},
		{"capture", "run-3"},
		{"capture", "RUN-3", "--name"},
		{"capture", "RUN-3", "extra"},
		{"capture", "RUN-3", "RUN-4"},
		{"capture", "RUN-3", "--bogus", "x"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := newEng(t)
			cpNoCalls(t, e, e.run(args...), 3)
		})
	}
}

// ---- ap patterns ----

// Covers T008: `patterns` reads every page of listCapturedPatterns and prints them in the engine's
// order (newest first), each with its ID and state.
func TestCpPatternsListsEveryPageNewestFirst(t *testing.T) {
	e := newEng(t)
	mk := func(id, state string, usable bool) map[string]any {
		p := cpPattern(state, usable, nil)
		p["id"] = id
		return p
	}
	e.on("GET /captured-patterns", func(w http.ResponseWriter, c req) {
		switch c.Query.Get("page_token") {
		case "":
			writeJSON(w, 200, map[string]any{"items": []any{mk("tdd-s-3", "approved", true), mk("tdd-s-2", "proposed", false)},
				"next_page_token": "tok2"})
		case "tok2":
			writeJSON(w, 200, map[string]any{"items": []any{mk("tdd-s-1", "withdrawn", false)}})
		default:
			t.Errorf("page_token = %q", c.Query.Get("page_token"))
		}
	})
	r := e.run("patterns")
	wantExit(t, r, 0)
	i3, i2, i1 := strings.Index(r.out, "tdd-s-3"), strings.Index(r.out, "tdd-s-2"), strings.Index(r.out, "tdd-s-1")
	if i3 < 0 || i2 < i3 || i1 < i2 {
		t.Errorf("want tdd-s-3, tdd-s-2, tdd-s-1 in that order:\n%s", r.out)
	}
	if !cpLineWith(r.out, "tdd-s-3", "approved") || !cpLineWith(r.out, "tdd-s-2", "proposed") || !cpLineWith(r.out, "tdd-s-1", "withdrawn") {
		t.Errorf("each line names its captured pattern's state:\n%s", r.out)
	}
	if n := len(e.got("GET", "/captured-patterns")); n != 2 {
		t.Errorf("list calls = %d, want 2", n)
	}
}

// Covers T008: no captured pattern is exit 0 (its text is the contract's to settle).
func TestCpPatternsWithNoneExits0(t *testing.T) {
	e := newEng(t)
	e.reply("GET /captured-patterns", 200, map[string]any{"items": []any{}})
	wantExit(t, e.run("patterns"), 0)
}

// Covers T008 (usage): `patterns` takes no argument.
func TestCpPatternsTakesNoArguments(t *testing.T) {
	e := newEng(t)
	cpNoCalls(t, e, e.run("patterns", "extra"), 3)
}

// ---- ap pattern ----

// Covers T008: `pattern <id>` prints the parameters with their places, the proofs, the state and
// usable. Places are checked as facts of one line (part, line, column, form).
func TestCpPatternShowsParametersPlacesProofsStateAndUsable(t *testing.T) {
	e := newEng(t)
	p := cpPattern("approved", true, cpTwoParams(),
		cpProofRec("RUN-5", "failed", map[string]any{"step": "code", "check": "AllTestsPass", "reason": "2 tests fail"}),
		cpProofRec("RUN-6", "passed"))
	p["approval"] = map[string]any{"by": "client", "at": tCreated, "proof": "RUN-6"}
	p["proof_slice"] = cpProof
	e.reply("GET /captured-patterns/"+cpID, 200, p)
	r := e.run("pattern", cpID)
	wantExit(t, r, 0)
	wantContains(t, "stdout", r.out, cpID, "note", "limit", "approved", "RUN-5", "RUN-6", "failed", "passed",
		"AllTestsPass", "2 tests fail")
	for _, w := range [][]string{
		{"slice/contract", "37", "14", "capitalised"},
		{"src/note.go", "path"},
		{"slice/rules", "4", "9", "exact"},
	} {
		if !cpLineWith(r.out, w...) {
			t.Errorf("no single line names the place %v:\n%s", w, r.out)
		}
	}
	if !regexp.MustCompile(`(?i)usable\W+(true|yes)\b`).MatchString(r.out) {
		t.Errorf("stdout does not say usable is true or yes:\n%s", r.out)
	}
	if cpHasLine(r.out, "No parameters: only a slice identical to the source fits.") {
		t.Error("a captured pattern with parameters prints no No parameters line")
	}
	if len(e.got("GET", "/captured-patterns/"+cpID)) != 1 {
		t.Error("pattern reads the captured pattern once")
	}
}

// Covers T008: usable false is shown as false (an approved one whose latest proof failed).
func TestCpPatternShowsAnUnusablePatternAsNotUsable(t *testing.T) {
	e := newEng(t)
	e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("approved", false, cpTwoParams(), cpProofRec("RUN-6", "failed",
		map[string]any{"reason": "the run expired"})))
	r := e.run("pattern", cpID)
	wantExit(t, r, 0)
	if !regexp.MustCompile(`(?i)usable\W+(false|no)\b`).MatchString(r.out) {
		t.Errorf("stdout does not say usable is false or no:\n%s", r.out)
	}
	wantContains(t, "stdout", r.out, "the run expired")
}

// Covers T008: a captured pattern with no parameter prints the `No parameters` line, whole.
func TestCpPatternWithNoParameterPrintsTheNoParametersLine(t *testing.T) {
	e := newEng(t)
	e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proposed", false, nil))
	r := e.run("pattern", cpID)
	wantExit(t, r, 0)
	if !cpHasLine(r.out, "No parameters: only a slice identical to the source fits.") {
		t.Errorf("stdout lacks the No parameters line:\n%s", r.out)
	}
}

// Covers T008 and contract.md "Store text in the terminal": a proof's failure reason is store text,
// printed escaped, so it cannot move the cursor or fake a line.
func TestCpPatternEscapesStoreText(t *testing.T) {
	e := newEng(t)
	e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proposed", false, nil,
		cpProofRec("RUN-6", "failed", map[string]any{"reason": "line one\n\x1b[2Jline two"})))
	r := e.run("pattern", cpID)
	wantExit(t, r, 0)
	wantAbsent(t, "stdout", r.out, "\x1b", "line one\n")
	wantContains(t, "stdout", r.out, `line one\n\x1b[2Jline two`)
}

// Covers T008: an unknown or erased ID is exit 4 NotFound.
func TestCpPatternUnknownIsNotFound(t *testing.T) {
	e := newEng(t)
	e.refuse("GET /captured-patterns/"+cpID, 404, prob("NotFound", "NotFound", "no captured pattern "+cpID))
	r := e.run("pattern", cpID)
	wantExit(t, r, 4)
	if want := "refused: NotFound: no captured pattern " + cpID + "\n"; r.err != want {
		t.Errorf("stderr = %q, want %q", r.err, want)
	}
	if r.out != "" {
		t.Errorf("stdout = %q", r.out)
	}
}

// Covers T008 (usage): a missing, malformed (outside `^[a-z0-9-]+$`) or extra ID is exit 3.
func TestCpPatternUsageErrorsExit3WithoutCalls(t *testing.T) {
	for _, args := range [][]string{
		{"pattern"},
		{"pattern", "Tdd-S-1"},
		{"pattern", "tdd_s_1"},
		{"pattern", cpID, "extra"},
		{"pattern", cpID, "--bogus"},
		{"pattern", cpID, "--drop"},
		{"pattern", cpID, "--add"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := newEng(t)
			cpNoCalls(t, e, e.run(args...), 3)
		})
	}
}

// ---- ap pattern <id> --drop / --add ----

func cpParamsRoutes(e *eng, params []map[string]any) {
	e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proposed", false, params))
	cpOp(e, "PUT /captured-patterns/"+cpID+"/parameters", "OP-21", "replace-parameters", cpID, "succeeded",
		map[string]any{"captured_pattern": cpID}, nil)
}

func cpSent(t *testing.T, e *eng) map[string]string {
	t.Helper()
	puts := e.got("PUT", "/captured-patterns/"+cpID+"/parameters")
	cpKey(t, puts, 1)
	var b struct {
		Parameters []map[string]any `json:"parameters"`
	}
	if err := json.Unmarshal(puts[0].Body, &b); err != nil {
		t.Fatalf("body %q: %v", puts[0].Body, err)
	}
	out := map[string]string{}
	for _, p := range b.Parameters {
		if len(p) != 2 {
			t.Errorf("parameter %v: want only name and value", p)
		}
		out[fmt.Sprint(p["name"])] = fmt.Sprint(p["value"])
	}
	if len(out) != len(b.Parameters) {
		t.Errorf("parameters %v hold a name twice", b.Parameters)
	}
	return out
}

func cpWantParams(t *testing.T, got, want map[string]string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) { // fmt prints a map in key order
		t.Errorf("parameters sent = %v, want %v", got, want)
	}
}

// Covers T008: --drop reads the captured pattern, sends PUT /parameters with every other current
// parameter unchanged (name and source value only), with an Idempotency-Key, and follows it.
func TestCpDropSendsTheOthersUnchanged(t *testing.T) {
	e := newEng(t)
	cpParamsRoutes(e, cpTwoParams())
	r := e.run("pattern", cpID, "--drop", "note")
	wantExit(t, r, 0)
	cpWantParams(t, cpSent(t, e), map[string]string{"limit": "10"})
	if len(e.got("GET", "/operations/OP-21")) == 0 {
		t.Error("--drop follows the operation")
	}
}

// Covers T008: dropping the only parameter sends an empty list, `{"parameters":[]}`, not null.
func TestCpDropOfTheLastParameterSendsAnEmptyList(t *testing.T) {
	e := newEng(t)
	cpParamsRoutes(e, cpTwoParams()[:1])
	wantExit(t, e.run("pattern", cpID, "--drop", "note"), 0)
	puts := e.got("PUT", "/captured-patterns/"+cpID+"/parameters")
	if len(puts) != 1 || oneLine(string(puts[0].Body)) != `{"parameters":[]}` {
		t.Errorf("body = %q, want {\"parameters\":[]}", firstBody(puts))
	}
}

// Covers T008: --add name=value sends the others unchanged plus the new one.
func TestCpAddWithNameSendsItBesideTheOthers(t *testing.T) {
	e := newEng(t)
	cpParamsRoutes(e, cpTwoParams())
	wantExit(t, e.run("pattern", cpID, "--add", "count=3"), 0)
	cpWantParams(t, cpSent(t, e), map[string]string{"note": "note", "limit": "10", "count": "3"})
}

// Covers T008: a value with a space after `name=` is one value.
func TestCpAddKeepsSpacesInTheValue(t *testing.T) {
	e := newEng(t)
	cpParamsRoutes(e, nil)
	wantExit(t, e.run("pattern", cpID, "--add", "title=two words"), 0)
	cpWantParams(t, cpSent(t, e), map[string]string{"title": "two words"})
}

// Covers T008: --add <value> takes the name from the value.
func TestCpAddWithoutNameTakesTheNameFromTheValue(t *testing.T) {
	e := newEng(t)
	cpParamsRoutes(e, cpTwoParams()[1:])
	wantExit(t, e.run("pattern", cpID, "--add", "widget"), 0)
	cpWantParams(t, cpSent(t, e), map[string]string{"limit": "10", "widget": "widget"})
}

// Covers T008, boundary: a 64-character value is the longest valid name, taken as the name.
func TestCpAddNameFromAValueOfSixtyFourCharacters(t *testing.T) {
	v := strings.Repeat("a", 64)
	e := newEng(t)
	cpParamsRoutes(e, nil)
	wantExit(t, e.run("pattern", cpID, "--add", v), 0)
	cpWantParams(t, cpSent(t, e), map[string]string{v: v})
}

// Covers T008: a value that is not a valid name, with no name given, is refused with exit 3 and
// nothing is sent (uppercase, a leading digit, a space, an underscore, a dot, 65 characters, a
// number).
func TestCpAddRefusesAValueThatIsNotAValidName(t *testing.T) {
	for _, v := range []string{"Widget", "WIDGET", "7up", "10", "3.5", "two words", "a_b", "a.b", strings.Repeat("a", 65)} {
		t.Run(v, func(t *testing.T) {
			e := newEng(t)
			cpNoCalls(t, e, e.run("pattern", cpID, "--add", v), 3)
		})
	}
}

// Covers T008: refusals at once (approved, withdrawn, busy, a body outside the schema, an unknown
// ID) are exit 4 with the problem; nothing further is followed.
func TestCpParameterChangeRefusalsExit4(t *testing.T) {
	for _, c := range []struct {
		status       int
		code, detail string
	}{
		{409, "CapturedPatternApproved", "it is approved"},
		{409, "CapturedPatternWithdrawn", "it is withdrawn"},
		{409, "CapturedPatternBusy", "a proof's run has not ended"},
		{400, "InvalidRequest", "parameters: more than 32"},
		{404, "NotFound", "no captured pattern"},
	} {
		t.Run(c.code, func(t *testing.T) {
			e := newEng(t)
			e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proposed", false, cpTwoParams()))
			e.refuse("PUT /captured-patterns/"+cpID+"/parameters", c.status, prob(c.code, c.code, c.detail))
			r := e.run("pattern", cpID, "--drop", "note")
			wantExit(t, r, 4)
			if want := "refused: " + c.code + ": " + c.detail + "\n"; r.err != want {
				t.Errorf("stderr = %q, want %q", r.err, want)
			}
			if r.out != "" {
				t.Errorf("stdout = %q, want nothing for a refusal", r.out)
			}
		})
	}
}

// Covers T008: an operation that fails ParameterRefused is exit 4 and prints the parameter and why.
func TestCpParameterRefusedByTheOperationExit4(t *testing.T) {
	e := newEng(t)
	e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proposed", false, nil))
	cpOp(e, "PUT /captured-patterns/"+cpID+"/parameters", "OP-21", "replace-parameters", cpID, "failed", nil,
		prob("ParameterRefused", "ParameterRefused", "widget: does not occur in the slice"))
	r := e.run("pattern", cpID, "--add", "widget")
	wantExit(t, r, 4)
	if want := "refused: ParameterRefused: widget: does not occur in the slice\n"; r.err != want {
		t.Errorf("stderr = %q, want %q", r.err, want)
	}
}

// ---- ap proof-slice ----

// Covers T008: `proof-slice` calls makeProofSlice with an Idempotency-Key, follows the operation and
// prints only the proof slice's ID and a line feed on standard output (the launcher reads it).
func TestCpProofSlicePrintsOnlyTheSliceID(t *testing.T) {
	e := newEng(t)
	cpOp(e, "POST /captured-patterns/"+cpID+"/proof-slice", "OP-22", "make-proof-slice", cpID, "succeeded",
		map[string]any{"slice": cpProof}, nil)
	r := e.run("proof-slice", cpID)
	wantExit(t, r, 0)
	wantOut(t, r, cpProof+"\n")
	cpKey(t, e.got("POST", "/captured-patterns/"+cpID+"/proof-slice"), 1)
	if len(e.got("GET", "/operations/OP-22")) == 0 {
		t.Error("proof-slice follows the operation")
	}
}

// Covers T008: refusals at once and a failed operation (PartFailsChecks lists its failures) are
// exit 4 with nothing on standard output, so the launcher never reads a slice ID from them.
func TestCpProofSliceFailuresPrintNothingOnStdout(t *testing.T) {
	route := "POST /captured-patterns/" + cpID + "/proof-slice"
	t.Run("withdrawn", func(t *testing.T) {
		e := newEng(t)
		e.refuse(route, 409, prob("CapturedPatternWithdrawn", "CapturedPatternWithdrawn", "it is withdrawn"))
		r := e.run("proof-slice", cpID)
		wantExit(t, r, 4)
		wantOut(t, r, "")
		if want := "refused: CapturedPatternWithdrawn: it is withdrawn\n"; r.err != want {
			t.Errorf("stderr = %q, want %q", r.err, want)
		}
	})
	t.Run("busy", func(t *testing.T) {
		e := newEng(t)
		e.refuse(route, 409, prob("CapturedPatternBusy", "CapturedPatternBusy", "another operation runs"))
		r := e.run("proof-slice", cpID)
		wantExit(t, r, 4)
		wantOut(t, r, "")
	})
	t.Run("not found", func(t *testing.T) {
		e := newEng(t)
		e.refuse(route, 404, prob("NotFound", "NotFound", ""))
		r := e.run("proof-slice", cpID)
		wantExit(t, r, 4)
		wantOut(t, r, "")
		if r.err != "refused: NotFound\n" {
			t.Errorf("stderr = %q", r.err)
		}
	})
	t.Run("part fails checks", func(t *testing.T) {
		e := newEng(t)
		cpOp(e, route, "OP-22", "make-proof-slice", cpID, "failed", nil, prob("PartFailsChecks", "PartFailsChecks", "a part fails a check",
			map[string]any{"part": "rules", "check": "ExampleTagged", "passed": false, "reason": "a scenario lacks an @E tag"}))
		r := e.run("proof-slice", cpID)
		wantExit(t, r, 4)
		wantOut(t, r, "")
		want := "refused: PartFailsChecks: a part fails a check\n  failed  ExampleTagged (rules): a scenario lacks an @E tag\n"
		if r.err != want {
			t.Errorf("stderr = %q, want %q", r.err, want)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		e := newEng(t)
		cpOp(e, route, "OP-22", "make-proof-slice", cpID, "cancelled", nil, nil)
		r := e.run("proof-slice", cpID)
		wantExit(t, r, 6)
		wantOut(t, r, "")
	})
}

// Covers T008 (usage): a missing, malformed or extra argument is exit 3 and nothing is called.
func TestCpProofSliceUsageErrorsExit3WithoutCalls(t *testing.T) {
	for _, args := range [][]string{{"proof-slice"}, {"proof-slice", "TDD"}, {"proof-slice", cpID, "extra"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := newEng(t)
			cpNoCalls(t, e, e.run(args...), 3)
		})
	}
}

// ---- ap approve-pattern ----

// Covers T008: `approve-pattern` calls approveCapturedPattern with an Idempotency-Key, follows the
// operation, exits 0 and names the captured pattern; each invocation makes its own key.
func TestCpApproveCallsFollowsAndUsesANewKeyEachTime(t *testing.T) {
	e := newEng(t)
	route := "POST /captured-patterns/" + cpID + "/approval"
	cpOp(e, route, "OP-23", "approve-captured-pattern", cpID, "succeeded", map[string]any{"captured_pattern": cpID}, nil)
	r := e.run("approve-pattern", cpID)
	wantExit(t, r, 0)
	wantContains(t, "stdout", r.out, cpID)
	if len(e.got("GET", "/operations/OP-23")) == 0 {
		t.Error("approve-pattern follows the operation")
	}
	wantExit(t, e.run("approve-pattern", cpID), 0)
	posts := e.got("POST", "/captured-patterns/"+cpID+"/approval")
	cpKey(t, posts, 2)
	if posts[0].Header.Get("Idempotency-Key") == posts[1].Header.Get("Idempotency-Key") {
		t.Error("two invocations sent the same Idempotency-Key")
	}
}

// Covers T008: ProofRequired, CapturedPatternWithdrawn, CapturedPatternBusy and NotFound refuse at
// once with exit 4 and the problem.
func TestCpApproveRefusalsExit4(t *testing.T) {
	for _, c := range []struct {
		status       int
		code, detail string
	}{
		{409, "ProofRequired", "its latest proof did not pass under the current method"},
		{409, "CapturedPatternWithdrawn", "it is withdrawn"},
		{409, "CapturedPatternBusy", "another operation runs"},
		{404, "NotFound", "no captured pattern"},
	} {
		t.Run(c.code, func(t *testing.T) {
			e := newEng(t)
			e.refuse("POST /captured-patterns/"+cpID+"/approval", c.status, prob(c.code, c.code, c.detail))
			r := e.run("approve-pattern", cpID)
			wantExit(t, r, 4)
			if want := "refused: " + c.code + ": " + c.detail + "\n"; r.err != want {
				t.Errorf("stderr = %q, want %q", r.err, want)
			}
			if r.out != "" {
				t.Errorf("stdout = %q, want nothing for a refusal", r.out)
			}
		})
	}
}

// Covers T008: a failed approval operation is exit 4 with its problem.
func TestCpApproveOperationFailedExit4(t *testing.T) {
	e := newEng(t)
	cpOp(e, "POST /captured-patterns/"+cpID+"/approval", "OP-23", "approve-captured-pattern", cpID, "failed", nil,
		prob("OperationFailed", "OperationFailed", "the store is full"))
	r := e.run("approve-pattern", cpID)
	wantExit(t, r, 4)
	if want := "refused: OperationFailed: the store is full\n"; r.err != want {
		t.Errorf("stderr = %q, want %q", r.err, want)
	}
}

// Covers T008 (usage).
func TestCpApproveUsageErrorsExit3WithoutCalls(t *testing.T) {
	for _, args := range [][]string{{"approve-pattern"}, {"approve-pattern", "Tdd"}, {"approve-pattern", cpID, "extra"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := newEng(t)
			cpNoCalls(t, e, e.run(args...), 3)
		})
	}
}

// ---- the proof outcome line ----

const (
	cpStarted = "RUN-7 started: building S-9 into proofs with " + cpRef + " (chosen) (operation OP-12)"
	cpStart   = "1  " + "2026-11-14T10:00:01Z" + "  RunStarted  slice S-9  pattern " + cpRef + "  reason chosen  proof"
	cpHeader  = "RUN-7  slice S-9  pattern " + cpRef + "  reason chosen  proof  project proofs  operation OP-12  created " + tCreated
)

// cpProofRun is stateRun's short run made a proof run of cpID: its record and its run say `proof`.
func cpProofRun(t *testing.T, s endState, runningReads int) *fakeRun {
	fr := stateRun(t, "RUN-7", "OP-12", s, runningReads)
	for k, v := range map[string]any{"slice": "S-9", "pattern": cpRef, "pattern_reason": "chosen", "proof": true, "project": "proofs"} {
		fr.final[k] = v
	}
	fr.events[0] = ev(1, "RUN-7", "RunStarted", map[string]any{"slice": "S-9", "pattern": cpRef, "pattern_reason": "chosen", "proof": true})
	return fr
}

func cpProofRecord(s endState) []string {
	return []string{cpStart, "2  " + at(2) + "  StepStarted  tests", s.lastLine}
}

func cpStartProof(e *eng) {
	e.reply("POST /slices/"+cpProof+"/runs", 202, operation("OP-12", "run", "accepted", "RUN-7", nil))
}

var cpStates = func() map[string]endState {
	m := map[string]endState{}
	for _, s := range endStates("RUN-7", "OP-12") {
		m[s.name] = s
	}
	return m
}()

func cpFail(reason string) map[string]any { return map[string]any{"reason": reason} }

// Covers T008 and contract.md "Run states and exit codes": after a proof's run (`proof` true) the
// client reads the captured pattern and prints, after the end line, `Proof of <id>: passed`
// (exit 0), `failed: ...` (exit 1 over the state's code: a hand-over, an expiry, a cancellation, a
// done run with differing files) or `running` (an interrupted run, the run state's exit 2).
func TestCpProofOutcomeLineAndExitByHowTheProofEnded(t *testing.T) {
	cases := []struct {
		name    string
		state   string
		outcome string
		fails   []map[string]any
		line    string
		exit    int
	}{
		{"passed", "done", "passed", nil, "Proof of " + cpID + ": passed", 0},
		{"differing files", "done", "failed",
			[]map[string]any{{"step": "code", "path": "src/a.go", "reason": "differs from the template"}},
			"Proof of " + cpID + ": failed: code src/a.go: differs from the template", 1},
		{"handed over, waiting for the developer", "waiting-for-developer", "failed",
			[]map[string]any{{"step": "code", "check": "AllTestsPass", "reason": "2 tests fail"}},
			"Proof of " + cpID + ": failed: code AllTestsPass: 2 tests fail", 1},
		{"handed over, taken over", "handed-over", "failed", []map[string]any{cpFail("the run was handed over")},
			"Proof of " + cpID + ": failed: the run was handed over", 1},
		{"expired", "expired", "failed", []map[string]any{cpFail("the run expired")},
			"Proof of " + cpID + ": failed: the run expired", 1},
		{"cancelled", "cancelled", "failed", []map[string]any{cpFail("the run was cancelled")},
			"Proof of " + cpID + ": failed: the run was cancelled", 1},
		{"running while interrupted", "interrupted", "running", nil, "Proof of " + cpID + ": running", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := cpStates[c.state]
			e := newEng(t)
			cpStartProof(e)
			cpProofRun(t, s, 2).register(e)
			e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proposed", false, cpTwoParams(), cpProofRec("RUN-7", c.outcome, c.fails...)))
			r := e.run("build", cpProof, "proofs")
			wantExit(t, r, c.exit)
			wantOut(t, r, lines(append(append([]string{cpStarted}, cpProofRecord(s)...), s.endLine, c.line)...))
			if len(e.got("GET", "/captured-patterns/"+cpID)) == 0 {
				t.Error("the client reads the captured pattern after the run")
			}
		})
	}
}

// Covers T008: several failures print on one line, each as `<step> <check>: <reason>`,
// `<step> <path>: <reason>` or `<reason>`, joined by `; ` in the engine's order.
func TestCpProofFailuresAreJoinedInOrder(t *testing.T) {
	s := cpStates["waiting-for-developer"]
	e := newEng(t)
	cpStartProof(e)
	cpProofRun(t, s, 1).register(e)
	e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proposed", false, nil, cpProofRec("RUN-7", "failed",
		map[string]any{"step": "tests", "check": "TestsFailAgainstStub", "reason": "the tests pass"},
		map[string]any{"step": "code", "path": "src/a.go", "reason": "differs from the template"},
		cpFail("the run waits for the developer"))))
	r := e.run("build", cpProof, "proofs")
	wantExit(t, r, 1)
	want := "Proof of " + cpID + ": failed: tests TestsFailAgainstStub: the tests pass; code src/a.go: differs from the template; the run waits for the developer"
	wantOut(t, r, lines(append(append([]string{cpStarted}, cpProofRecord(s)...), s.endLine, want)...))
}

// Covers T008 and "Store text in the terminal": a failure reason with a line break cannot make a
// second `Proof of` line.
func TestCpProofLineEscapesTheFailureReason(t *testing.T) {
	s := cpStates["expired"]
	e := newEng(t)
	cpStartProof(e)
	cpProofRun(t, s, 1).register(e)
	e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proposed", false, nil, cpProofRec("RUN-7", "failed",
		cpFail("bad\nProof of "+cpID+": passed\x1b[2J"))))
	r := e.run("build", cpProof, "proofs")
	wantExit(t, r, 1)
	want := "Proof of " + cpID + `: failed: bad\nProof of ` + cpID + `: passed\x1b[2J`
	wantOut(t, r, lines(append(append([]string{cpStarted}, cpProofRecord(s)...), s.endLine, want)...))
}

// Covers T008: the line is about this run's proof, matched by run, when the captured pattern has
// several proofs (an older failed one and a newer one of another run).
func TestCpProofLineIsThisRunsProofAmongSeveral(t *testing.T) {
	s := cpStates["done"]
	e := newEng(t)
	cpStartProof(e)
	cpProofRun(t, s, 1).register(e)
	e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proven", false, nil,
		cpProofRec("RUN-2", "failed", cpFail("the run expired")),
		cpProofRec("RUN-7", "passed"),
		cpProofRec("RUN-9", "failed", cpFail("the run was cancelled"))))
	r := e.run("build", cpProof, "proofs")
	wantExit(t, r, 0)
	wantOut(t, r, lines(append(append([]string{cpStarted}, cpProofRecord(s)...), s.endLine, "Proof of "+cpID+": passed")...))
}

// Covers T008: `run <run>` of a proof run prints the header with `proof` and, after the end line, the
// proof line, with the exit code of the proof.
func TestCpRunOfAProofRunPrintsTheProofLine(t *testing.T) {
	for _, c := range []struct {
		state, outcome, line string
		exit                 int
		fails                []map[string]any
	}{
		{"done", "passed", "Proof of " + cpID + ": passed", 0, nil},
		{"cancelled", "failed", "Proof of " + cpID + ": failed: the run was cancelled", 1, []map[string]any{cpFail("the run was cancelled")}},
	} {
		t.Run(c.state, func(t *testing.T) {
			s := cpStates[c.state]
			e := newEng(t)
			cpProofRun(t, s, 0).register(e)
			e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proposed", false, nil, cpProofRec("RUN-7", c.outcome, c.fails...)))
			r := e.run("run", "RUN-7")
			wantExit(t, r, c.exit)
			wantOut(t, r, lines(append(append([]string{cpHeader}, cpProofRecord(s)...), s.endLine, c.line)...))
		})
	}
}

// Covers T008: `replay` of a proof run is a new proof run; the client prints its proof line.
func TestCpReplayOfAProofRunPrintsTheProofLine(t *testing.T) {
	s := cpStates["done"]
	e := newEng(t)
	e.reply("POST /runs/RUN-3/replay", 202, operation("OP-12", "run", "accepted", "RUN-7", nil))
	cpProofRun(t, s, 1).register(e)
	e.reply("GET /captured-patterns/"+cpID, 200, cpPattern("proven", false, nil, cpProofRec("RUN-7", "passed")))
	r := e.run("replay", "RUN-3", "proofs")
	wantExit(t, r, 0)
	wantOut(t, r, lines(append(append([]string{"RUN-7 started: replaying RUN-3 into proofs (operation OP-12)"},
		cpProofRecord(s)...), s.endLine, "Proof of "+cpID+": passed")...))
}

// Covers T008: a run that is not a proof prints no proof line and the client never reads a
// captured pattern for it (an unexpected call would fail the test).
func TestCpOrdinaryRunPrintsNoProofLine(t *testing.T) {
	s := cpStates["done"]
	e := newEng(t)
	e.reply("POST /slices/S-1/runs", 202, operation("OP-12", "run", "accepted", "RUN-7", nil))
	stateRun(t, "RUN-7", "OP-12", s, 1).register(e)
	r := e.run("build", "S-1", "notes")
	wantExit(t, r, 0)
	wantAbsent(t, "stdout", r.out, "Proof of")
	if e.anyPathWith("/captured-patterns") {
		t.Error("an ordinary run reads no captured pattern")
	}
}

// ---- slices and show ----

// Covers T008: `slices` shows a `PROOF OF` column: the captured pattern for a proof slice, `-` for
// any other (found by the header's column, so the column's place is free).
func TestCpSlicesShowsTheProofOfColumn(t *testing.T) {
	e := newEng(t)
	e.reply("GET /slices", 200, map[string]any{"items": []any{
		map[string]any{"id": cpProof, "title": "Proof of " + cpID, "state": "approved", "latest_run": "RUN-7", "proof_of": cpID},
		map[string]any{"id": "S-1", "title": "First slice", "state": "approved"},
	}})
	r := e.run("slices")
	wantExit(t, r, 0)
	ls := cpLines(r.out)
	col := strings.Index(ls[0], "PROOF OF")
	if col < 0 {
		t.Fatalf("the header has no PROOF OF column:\n%s", r.out)
	}
	for id, want := range map[string]string{cpProof: cpID, "S-1": "-"} {
		found := false
		for _, l := range ls[1:] {
			if strings.HasPrefix(l, id+" ") && len(l) > col {
				found = true
				if !strings.HasPrefix(l[col:], want+" ") && l[col:] != want {
					t.Errorf("row %s: PROOF OF cell = %q, want %q:\n%s", id, l[col:], want, r.out)
				}
			}
		}
		if !found {
			t.Errorf("no row for %s:\n%s", id, r.out)
		}
	}
}

// Covers T008: `show` of a proof slice prints `proof of <captured pattern>`; of any other slice
// prints no such line.
func TestCpShowPrintsTheProofOfLine(t *testing.T) {
	e := newEng(t)
	sl := slice(sliceOpts{id: cpProof, title: "Proof of " + cpID})
	sl["proof_of"] = cpID
	e.reply("GET /slices/"+cpProof, 200, sl)
	e.reply("GET /slices/S-1", 200, slice(sliceOpts{}))
	r := e.run("show", cpProof)
	wantExit(t, r, 0)
	if !cpHasLine(r.out, "proof of "+cpID) {
		t.Errorf("stdout lacks the line `proof of %s`:\n%s", cpID, r.out)
	}
	r = e.run("show", "S-1")
	wantExit(t, r, 0)
	wantAbsent(t, "stdout", r.out, "proof of")
}
