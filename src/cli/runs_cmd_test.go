// T055: runs, run, compare, artifact and take-over; the problem format, store text escaped, the
// environment and usage errors of the run commands (contract.md, "Inspecting runs", "Taking over",
// "Problems", "Store text in the terminal", "Configuration"). Seams: runs_fake_test.go.
package cli_test

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"text/tabwriter"
	"time"

	cli "github.com/agentic-platform/cli"
)

var artHash = strings.Repeat("ab", 32)

// ---- runs ----

func TestRunsListsEveryPageNewestFirst(t *testing.T) {
	e := newEng(t)
	r9 := runJSON(runSpec{id: "RUN-9", project: "p-two", status: "running"})
	r9["slice"] = "S-2"
	r8 := runJSON(runSpec{id: "RUN-8", project: "notes-2", status: "ended", outcome: "done", replayOf: "RUN-7"})
	r7 := runJSON(runSpec{id: "RUN-7", status: "waiting-for-developer", reason: "out of attempts"})
	e.on("GET /runs", func(w http.ResponseWriter, c req) {
		switch c.Query.Get("page_token") {
		case "":
			writeJSON(w, 200, map[string]any{"items": []any{r9, r8}, "next_page_token": "runs-2"})
		case "runs-2":
			writeJSON(w, 200, map[string]any{"items": []any{r7}})
		default:
			t.Errorf("page_token = %q, want runs-2", c.Query.Get("page_token"))
			writeProblem(w, 400, prob("InvalidRequest", "InvalidRequest", ""))
		}
	})
	r := e.run("runs")
	wantExit(t, r, 0)
	var want bytes.Buffer
	tw := tabwriter.NewWriter(&want, 0, 4, 2, ' ', 0) // as slices aligns its table
	for _, row := range []string{
		"RUN\tSLICE\tSTATUS\tOUTCOME\tPROJECT\tCREATED\tREPLAY OF",
		"RUN-9\tS-2\trunning\t-\tp-two\t" + tCreated + "\t-",
		"RUN-8\tS-1\tended\tdone\tnotes-2\t" + tCreated + "\tRUN-7",
		"RUN-7\tS-1\twaiting-for-developer\t-\tnotes\t" + tCreated + "\t-",
	} {
		tw.Write([]byte(row + "\n"))
	}
	tw.Flush()
	wantOut(t, r, want.String())
	if n := len(e.got("GET", "/runs")); n != 2 {
		t.Errorf("listRuns calls = %d, want 2", n)
	}
}

func TestRunsWithNoRuns(t *testing.T) {
	e := newEng(t)
	e.reply("GET /runs", 200, map[string]any{"items": []any{}})
	r := e.run("runs")
	wantExit(t, r, 0)
	wantOut(t, r, "No runs yet. Start one with build <slice> <project>.\n")
}

// ---- run <run> ----

const runHeader = "RUN-7  slice S-1  pattern tdd@1.0.0  project notes  operation OP-12  created " + tCreated

func TestRunPrintsTheHeaderEveryEventOfEveryPageAndTheEndLine(t *testing.T) {
	e := newEng(t)
	evs, want := fullRecord("RUN-7")
	fr := &fakeRun{t: t, id: "RUN-7", op: "OP-12",
		final:   runJSON(runSpec{id: "RUN-7", status: "ended", outcome: "done", replayOf: "RUN-3"}),
		finalOp: operation("OP-12", "run", "succeeded", "RUN-7", map[string]any{"run": "RUN-7"}),
		events:  evs, runningReads: 0, pageSize: 5}
	fr.register(e)
	r := e.run("run", "RUN-7")
	wantExit(t, r, 0)
	wantOut(t, r, lines(append(append([]string{runHeader + "  replay of RUN-3"}, want...), "RUN-7 ended done")...))
	if n := fr.pageCalls(); n != 3 {
		t.Errorf("listRunEvents calls = %d, want the 3 pages of 13 events", n)
	}
}

func TestRunExitsByTheRunState(t *testing.T) {
	for _, s := range endStates("RUN-7", "OP-12") {
		t.Run(s.name, func(t *testing.T) {
			e := newEng(t)
			fr := stateRun(t, "RUN-7", "OP-12", s, 0)
			fr.register(e)
			r := e.run("run", "RUN-7")
			wantExit(t, r, s.exit)
			wantOut(t, r, lines(append(append([]string{runHeader}, stateLines("RUN-7", s)...), s.endLine)...))
			if s.status != "ended" && fr.pageCalls() > 4 {
				t.Errorf("listRunEvents calls = %d: a run that has not ended is read until a page brings no new event", fr.pageCalls())
			}
		})
	}
}

func TestRunFollowsARunningRunUntilItEnds(t *testing.T) {
	e := newEng(t)
	s := endStates("RUN-7", "OP-12")[4] // expired
	fr := stateRun(t, "RUN-7", "OP-12", s, 3)
	fr.register(e)
	r := e.run("run", "RUN-7")
	wantExit(t, r, 5)
	wantOut(t, r, lines(append(append([]string{runHeader}, stateLines("RUN-7", s)...), s.endLine)...))
}

func TestRunUnknownIsNotFound(t *testing.T) {
	e := newEng(t)
	e.refuse("GET /runs/RUN-99", 404, prob("NotFound", "NotFound", "no run RUN-99"))
	r := e.run("run", "RUN-99")
	wantExit(t, r, 4)
	if got := strings.TrimRight(r.err, "\n"); got != "refused: NotFound: no run RUN-99" {
		t.Errorf("stderr = %q", r.err)
	}
	if r.out != "" {
		t.Errorf("stdout = %q, want nothing for a refusal", r.out)
	}
}

// ---- compare ----

func TestCompareRunsThatDoNotDiffer(t *testing.T) {
	e := newEng(t)
	e.reply("GET /runs/RUN-7/comparison", 200, map[string]any{"items": []any{}})
	r := e.run("compare", "RUN-7", "RUN-8")
	wantExit(t, r, 0)
	wantOut(t, r, "RUN-7 and RUN-8 do not differ.\n")
	if c := e.got("GET", "/runs/RUN-7/comparison"); len(c) != 1 {
		t.Errorf("compareRuns calls = %d, want 1", len(c))
	} else if c[0].Query.Get("with") != "RUN-8" {
		t.Errorf("with = %q, want RUN-8", c[0].Query.Get("with"))
	}
}

func TestCompareRunsThatDifferExits7WithTheTable(t *testing.T) {
	e := newEng(t)
	e.on("GET /runs/RUN-7/comparison", func(w http.ResponseWriter, c req) {
		if c.Query.Get("with") != "RUN-8" {
			t.Errorf("with = %q", c.Query.Get("with"))
		}
		switch c.Query.Get("page_token") {
		case "":
			writeJSON(w, 200, map[string]any{"items": []any{
				map[string]any{"step": "tests", "field": "artifacts", "left": rhArt, "right": rhArt2},
				map[string]any{"step": "code", "field": "checks", "left": "passed", "right": "failed: 2 tests fail\x1b[31m"},
			}, "next_page_token": "cmp-2"})
		case "cmp-2":
			writeJSON(w, 200, map[string]any{"items": []any{
				map[string]any{"step": "review", "field": "decision", "right": `{"verdict":"accept"}`},
			}})
		default:
			t.Errorf("page_token = %q", c.Query.Get("page_token"))
			writeProblem(w, 400, prob("InvalidRequest", "InvalidRequest", ""))
		}
	})
	r := e.run("compare", "RUN-7", "RUN-8")
	wantExit(t, r, 7)
	got := strings.Split(strings.TrimRight(r.out, "\n"), "\n")
	rows := []*regexp.Regexp{
		regexp.MustCompile(`^RUN-7 and RUN-8 differ in 3 place\(s\):$`),
		regexp.MustCompile(`^STEP {2,}FIELD {2,}RUN-7 {2,}RUN-8$`),
		regexp.MustCompile(`^tests {2,}artifacts {2,}` + rhArt + ` {2,}` + rhArt2 + `$`),
		regexp.MustCompile(`^code {2,}checks {2,}passed {2,}failed: 2 tests fail\\x1b\[31m$`),
		regexp.MustCompile(`^review {2,}decision {2,}- {2,}\S*verdict\S*accept\S*$`),
	}
	if len(got) != len(rows) {
		t.Fatalf("stdout has %d lines, want %d:\n%s", len(got), len(rows), r.out)
	}
	for i, re := range rows {
		if !re.MatchString(got[i]) {
			t.Errorf("line %d = %q, want %s", i+1, got[i], re)
		}
	}
	if n := len(e.got("GET", "/runs/RUN-7/comparison")); n != 2 {
		t.Errorf("compareRuns calls = %d, want every page", n)
	}
}

func TestCompareUnknownRunIsNotFound(t *testing.T) {
	e := newEng(t)
	e.refuse("GET /runs/RUN-7/comparison", 404, prob("NotFound", "NotFound", ""))
	r := e.run("compare", "RUN-7", "RUN-99")
	wantExit(t, r, 4)
	wantContains(t, "stderr", r.err, "refused: NotFound")
	if r.out != "" {
		t.Errorf("stdout = %q", r.out)
	}
}

// ---- artifact ----

func TestArtifactWritesExactlyTheBytes(t *testing.T) {
	for name, content := range map[string][]byte{
		"binary":           {0x00, 0x1b, '[', '2', 'J', 0xff, 0xfe, '\n', 'x', '\r'},
		"no trailing line": []byte("package main\n\nfunc main() {}"),
	} {
		t.Run(name, func(t *testing.T) {
			e := newEng(t)
			e.on("GET /runs/RUN-7/artifacts/"+artHash, func(w http.ResponseWriter, _ req) {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.WriteHeader(200)
				_, _ = w.Write(content)
			})
			r := e.run("artifact", "RUN-7", artHash)
			wantExit(t, r, 0)
			if r.out != string(content) {
				t.Errorf("stdout = %q, want exactly %q", r.out, content)
			}
		})
	}
}

func TestArtifactNotFoundWritesNothingToStandardOutput(t *testing.T) {
	e := newEng(t)
	e.refuse("GET /runs/RUN-7/artifacts/"+artHash, 404, prob("NotFound", "NotFound", ""))
	r := e.run("artifact", "RUN-7", artHash)
	wantExit(t, r, 4)
	if r.out != "" {
		t.Errorf("stdout = %q, want nothing", r.out)
	}
	if got := strings.TrimRight(r.err, "\n"); got != "refused: NotFound" {
		t.Errorf("stderr = %q", r.err)
	}
}

// ---- take-over ----

func TestTakeOverPrintsTheSuccessLineAndExits0(t *testing.T) {
	e := newEng(t)
	e.reply("POST /runs/RUN-7/hand-over", 202, operation("OP-12", "run", "running", "RUN-7", nil))
	var n int
	e.on("GET /operations/OP-12", func(w http.ResponseWriter, _ req) {
		n++
		st := "running"
		if n > 1 {
			st = "succeeded"
		}
		writeJSON(w, 200, operation("OP-12", "run", st, "RUN-7", map[string]any{"run": "RUN-7"}))
	})
	e.reply("GET /runs/RUN-7", 200, runJSON(runSpec{id: "RUN-7", status: "ended", outcome: "handed-over",
		reason: "the code step ran out of attempts"}))
	r := e.run("take-over", "RUN-7")
	wantExit(t, r, 0)
	wantOut(t, r, "RUN-7 ended handed-over: the code step ran out of attempts. The project notes is yours to finish.\n")
	if len(e.got("GET", "/operations/OP-12")) < 2 {
		t.Error("take-over follows its operation until it ends")
	}
	if len(e.got("GET", "/runs/RUN-7")) == 0 {
		t.Error("take-over reads the run")
	}
}

func TestTakeOverOfARunNotWaitingIsRefused(t *testing.T) {
	for _, code := range []string{"RunNotWaiting", "NotFound"} {
		t.Run(code, func(t *testing.T) {
			e := newEng(t)
			status := 409
			if code == "NotFound" {
				status = 404
			}
			e.refuse("POST /runs/RUN-7/hand-over", status, prob(code, code, "the run is running"))
			r := e.run("take-over", "RUN-7")
			wantExit(t, r, 4)
			if got := strings.TrimRight(r.err, "\n"); got != "refused: "+code+": the run is running" {
				t.Errorf("stderr = %q", r.err)
			}
			if r.out != "" {
				t.Errorf("stdout = %q", r.out)
			}
			if len(e.got("GET", "/runs/RUN-7")) > 0 {
				t.Error("the client does not check the status first")
			}
		})
	}
}

// ---- problems ----

func TestARefusalPrintsTheProblemFormat(t *testing.T) {
	e := newEng(t)
	e.refuse("POST /slices/S-1/runs", 409, prob("NotApproved", "A part is not approved", "goals changed since the approval",
		map[string]any{"part": "goals", "check": "GoalsPresent", "passed": false, "reason": "no goal"},
		map[string]any{"part": "rules", "check": "ExampleTagged", "passed": false, "reason": "a scenario lacks an @E tag"}))
	r := e.run("build", "S-1", "notes")
	wantExit(t, r, 4)
	want := "refused: NotApproved: A part is not approved: goals changed since the approval\n" +
		"  failed  GoalsPresent (goals): no goal\n" +
		"  failed  ExampleTagged (rules): a scenario lacks an @E tag\n"
	if r.err != want {
		t.Errorf("stderr:\n%q\nwant:\n%q", r.err, want)
	}
	if r.out != "" {
		t.Errorf("stdout = %q, want nothing for a refusal", r.out)
	}
}

func TestATitleEqualToTheCodeAndNoDetailAreLeftOut(t *testing.T) {
	e := newEng(t)
	e.refuse("POST /slices/S-1/runs", 409, prob("RunActive", "RunActive", ""))
	r := e.run("build", "S-1", "notes")
	wantExit(t, r, 4)
	if r.err != "refused: RunActive\n" {
		t.Errorf("stderr = %q", r.err)
	}
}

// ---- store text ----

func TestStoreTextIsPrintedEscaped(t *testing.T) {
	e := newEng(t)
	buildRoutes(e)
	evil := "first\nsecond \x1b[2J\r"
	escaped := `first\nsecond \x1b[2J\r`
	evs := []map[string]any{
		ev(1, "RUN-7", "RunStarted", map[string]any{"slice": "S-1", "pattern": pattern, "detail": evil}),
		ev(2, "RUN-7", "CheckFailed", map[string]any{"step": "code", "refs": []any{"R-1"},
			"check": map[string]any{"name": "AllTestsPass", "passed": false, "reason": evil}}),
		ev(3, "RUN-7", "FindingRecorded", map[string]any{"step": "review", "round": 1,
			"finding": map[string]any{"rule": "R-1", "text": evil, "blocking": false}}),
		ev(4, "RUN-7", "RunHandedOver", map[string]any{"detail": evil}),
	}
	fr := &fakeRun{t: t, id: "RUN-7", op: "OP-12",
		final:   runJSON(runSpec{id: "RUN-7", status: "waiting-for-developer", reason: evil}),
		finalOp: operation("OP-12", "run", "running", "RUN-7", nil),
		events:  evs, initial: 4, runningReads: 0}
	fr.register(e)
	r := e.run("build", "S-1", "notes")
	wantExit(t, r, 2)
	wantOut(t, r, lines(startedLine,
		"1  "+at(1)+"  RunStarted  slice S-1  pattern tdd@1.0.0  "+escaped,
		"2  "+at(2)+"  CheckFailed  code  AllTestsPass  refs R-1  "+escaped,
		"3  "+at(3)+"  FindingRecorded  review  round 1  suggestion  R-1  "+escaped,
		"4  "+at(4)+"  RunHandedOver  "+escaped,
		"RUN-7 waits for the developer: "+escaped+"; it expires "+tExpires+
			". Take it over with take-over RUN-7, or cancel it with cancel RUN-7."))
	if strings.ContainsAny(r.out+r.err, "\x1b\r") {
		t.Error("a control character from the store reached the terminal")
	}
}

func TestAProblemDetailFromTheStoreIsEscaped(t *testing.T) {
	e := newEng(t)
	buildRoutes(e)
	s := endStates("RUN-7", "OP-12")[2]
	fr := stateRun(t, "RUN-7", "OP-12", s, 0)
	fr.finalOp = operation("OP-12", "run", "failed", "RUN-7", nil)
	fr.finalOp["error"] = prob("OperationFailed", "the operation failed", "line one\n\x1b[2Jline two",
		map[string]any{"part": "rules", "check": "ExampleTagged", "passed": false, "reason": "a\nb"})
	fr.register(e)
	r := e.run("build", "S-1", "notes")
	wantExit(t, r, 4)
	want := `refused: OperationFailed: the operation failed: line one\n\x1b[2Jline two` + "\n" +
		`  failed  ExampleTagged (rules): a\nb` + "\n"
	if r.err != want {
		t.Errorf("stderr = %q, want %q", r.err, want)
	}
}

// ---- environment and usage ----

var runCommands = [][]string{
	{"build", "S-1", "notes"}, {"resume", "RUN-7"}, {"replay", "RUN-7", "notes-2"}, {"take-over", "RUN-7"},
	{"compare", "RUN-7", "RUN-8"}, {"runs"}, {"run", "RUN-7"}, {"artifact", "RUN-7", strings.Repeat("ab", 32)},
}

func TestRunCommandsWithoutEngineURLOrTokenExit3NamingBoth(t *testing.T) {
	for _, args := range runCommands {
		for _, env := range []cli.Env{{EngineToken: token}, {EngineURL: "http://127.0.0.1:1"}} {
			t.Run(strings.Join(args, "_"), func(t *testing.T) {
				r := runFor(env, 20*time.Second, args...)
				wantExit(t, r, 3)
				wantContains(t, "stderr", r.err, "ENGINE_URL", "ENGINE_TOKEN")
			})
		}
	}
}

func TestRunCommandUsageErrorsExit3WithoutCallingTheEngine(t *testing.T) {
	cases := [][]string{
		{"build"}, {"build", "S-1"}, {"build", "RUN-1", "notes"}, {"build", "S-1", "notes", "extra"},
		{"resume"}, {"resume", "S-1"}, {"resume", "RUN-1", "extra"},
		{"replay", "RUN-1"}, {"replay", "S-1", "notes"},
		{"take-over"}, {"take-over", "OP-1"},
		{"compare", "RUN-1"}, {"compare", "RUN-1", "OP-2"}, {"compare", "S-1", "RUN-2"},
		{"runs", "extra"},
		{"run"}, {"run", "S-1"}, {"run", "RUN-1", "extra"},
		{"artifact", "RUN-1"}, {"artifact", "S-1", artHash}, {"artifact", "RUN-1", "abc"},
		{"artifact", "RUN-1", strings.ToUpper(artHash)}, {"artifact", "RUN-1", artHash[:63]},
		{"artifact", "RUN-1", artHash + "0"}, {"artifact", "RUN-1", strings.Repeat("g", 64)},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := newEng(t)
			r := e.run(args...)
			wantExit(t, r, 3)
			if r.err == "" {
				t.Error("a usage error prints a message on standard error")
			}
			wantAbsent(t, "stderr", r.err, "not available yet")
			if e.total() != 0 {
				t.Errorf("the engine was called %d times", e.total())
			}
		})
	}
}
