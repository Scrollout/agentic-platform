// T055: build, resume and replay print the run ID line, follow the run's record page by page, print
// the end line and exit by the run's state; Ctrl-C and SIGTERM detach (contract.md, "Idempotency and
// the run ID", "Following a run", "Run states and exit codes"). Seams: runs_fake_test.go.
package cli_test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	cli "github.com/agentic-platform/cli"
)

const startedLine = "RUN-7 started: building S-1 into notes (operation OP-12)"

// buildRoutes answers startRun for S-1 with the run operation OP-12 targeting RUN-7.
func buildRoutes(e *eng) {
	e.reply("POST /slices/S-1/runs", 202, operation("OP-12", "run", "accepted", "RUN-7", nil))
}

func TestBuildPrintsTheRunLineThenEveryEventThenTheEndLine(t *testing.T) {
	e := newEng(t)
	buildRoutes(e)
	evs, want := fullRecord("RUN-7")
	var out syncBuf
	var atFirstPage string
	fr := &fakeRun{t: t, id: "RUN-7", op: "OP-12",
		final:   runJSON(runSpec{id: "RUN-7", status: "ended", outcome: "done"}),
		finalOp: operation("OP-12", "run", "succeeded", "RUN-7", map[string]any{"run": "RUN-7"}),
		events:  evs, initial: 3, perRead: 4, runningReads: 3, pageSize: 2,
		onFirstPage: func() { atFirstPage = out.String() }}
	fr.register(e)

	var errb syncBuf
	code := runTo(e, &out, &errb, "build", "S-1", "notes")
	r := result{code, out.String(), errb.String()}
	wantExit(t, r, 0)
	wantOut(t, r, lines(append(append([]string{startedLine}, want...), "RUN-7 ended done")...))
	if atFirstPage != startedLine+"\n" {
		t.Errorf("before the first event was read, stdout was %q, want only the run line", atFirstPage)
	}
	posts := e.got("POST", "/slices/S-1/runs")
	if len(posts) != 1 {
		t.Fatalf("startRun calls = %d, want 1", len(posts))
	}
	if got := posts[0].Body; oneLine(string(got)) != `{"project":"notes"}` {
		t.Errorf("startRun body = %s, want {\"project\":\"notes\"}", got)
	}
	if fr.pageCalls() < len(evs)/2 {
		t.Errorf("listRunEvents calls = %d, want every page of %d events at 2 a page", fr.pageCalls(), len(evs))
	}
	if len(e.got("GET", "/operations/OP-12")) == 0 {
		t.Error("following reads the run's operation before it ends")
	}
}

func runTo(e *eng, out, errb *syncBuf, args ...string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	return cli.Run(ctx, args, e.env(), strings.NewReader(""), out, errb)
}

func TestBuildExitsByTheRunState(t *testing.T) {
	for _, s := range endStates("RUN-7", "OP-12") {
		t.Run(s.name, func(t *testing.T) {
			e := newEng(t)
			buildRoutes(e)
			stateRun(t, "RUN-7", "OP-12", s, 2).register(e)
			r := e.run("build", "S-1", "notes")
			wantExit(t, r, s.exit)
			wantOut(t, r, lines(append(append([]string{startedLine}, stateLines("RUN-7", s)...), s.endLine)...))
		})
	}
}

func TestResumePrintsTheWholeRecordAgainAndExitsByState(t *testing.T) {
	for _, s := range endStates("RUN-7", "OP-12") {
		t.Run(s.name, func(t *testing.T) {
			e := newEng(t)
			e.reply("POST /runs/RUN-7/resume", 202, operation("OP-12", "run", "running", "RUN-7", nil))
			fr := stateRun(t, "RUN-7", "OP-12", s, 1)
			fr.initial = 2 // the record before the interruption
			fr.register(e)
			r := e.run("resume", "RUN-7")
			wantExit(t, r, s.exit)
			wantOut(t, r, lines(append(append([]string{"RUN-7 resumed (operation OP-12)"}, stateLines("RUN-7", s)...), s.endLine)...))
			if p := e.got("GET", "/runs/RUN-7/events"); len(p) == 0 || p[0].Query.Get("page_token") != "" {
				t.Error("resume reads the record from its first event, without a page token")
			}
		})
	}
}

func TestReplayNamesTheNewRunAndFollowsIt(t *testing.T) {
	for _, s := range endStates("RUN-8", "OP-13") {
		t.Run(s.name, func(t *testing.T) {
			e := newEng(t)
			e.reply("POST /runs/RUN-3/replay", 202, operation("OP-13", "run", "accepted", "RUN-8", nil))
			stateRun(t, "RUN-8", "OP-13", s, 1).register(e)
			r := e.run("replay", "RUN-3", "notes-2")
			wantExit(t, r, s.exit)
			wantOut(t, r, lines(append(append([]string{"RUN-8 started: replaying RUN-3 into notes-2 (operation OP-13)"},
				stateLines("RUN-8", s)...), s.endLine)...))
			posts := e.got("POST", "/runs/RUN-3/replay")
			if len(posts) != 1 || oneLine(string(posts[0].Body)) != `{"project":"notes-2"}` {
				t.Errorf("replayRun calls %d, body %q", len(posts), firstBody(posts))
			}
			if e.anyPathWith("/runs/RUN-3/events") {
				t.Error("replay follows the new run, not the replayed one")
			}
		})
	}
}

func firstBody(rs []req) string {
	if len(rs) == 0 {
		return ""
	}
	return string(rs[0].Body)
}

func TestFollowingARunWhoseOperationFailedPrintsItsProblemAndExits4(t *testing.T) {
	e := newEng(t)
	buildRoutes(e)
	s := endStates("RUN-7", "OP-12")[2] // interrupted, but its operation failed
	fr := stateRun(t, "RUN-7", "OP-12", s, 1)
	fr.finalOp = operation("OP-12", "run", "failed", "RUN-7", nil)
	fr.finalOp["error"] = prob("OperationFailed", "the operation failed", "the store is full")
	fr.register(e)
	r := e.run("build", "S-1", "notes")
	wantExit(t, r, 4)
	wantOut(t, r, lines(append([]string{startedLine}, stateLines("RUN-7", s)...)...))
	if got := strings.TrimRight(r.err, "\n"); got != "refused: OperationFailed: the operation failed: the store is full" {
		t.Errorf("stderr = %q, want the operation's problem", r.err)
	}
}

func TestFollowingWaitsBetweenEmptyPagesAndReadsTheRun(t *testing.T) {
	e := newEng(t)
	buildRoutes(e)
	s := endStates("RUN-7", "OP-12")[0]
	fr := stateRun(t, "RUN-7", "OP-12", s, 3)
	fr.perRead = 0 // nothing new until the run ends
	fr.initial = 2
	fr.register(e)
	r := e.run("build", "S-1", "notes")
	wantExit(t, r, 0)
	wantOut(t, r, lines(append(append([]string{startedLine}, stateLines("RUN-7", s)...), s.endLine)...))
	pages := e.got("GET", "/runs/RUN-7/events")
	runs := e.got("GET", "/runs/RUN-7")
	if len(runs) < 4 {
		t.Fatalf("getRun calls = %d, want one after each page with no new event until the run ends", len(runs))
	}
	// Between empty pages the waits are 100 ms, then 200 ms, then 400 ms and more: well under the request limit.
	if d := runs[len(runs)-1].At.Sub(pages[0].At); d < 600*time.Millisecond {
		t.Errorf("four reads of the run came within %v of the first page, want waits of 100 ms doubling", d)
	}
	if len(pages) > 12 {
		t.Errorf("listRunEvents calls = %d for 4 reads of a run with no new event", len(pages))
	}
}

func TestFollowingLostEngineExits1NamingTheRun(t *testing.T) {
	t.Parallel()
	e := newEng(t)
	buildRoutes(e)
	fr := stateRun(t, "RUN-7", "OP-12", endStates("RUN-7", "OP-12")[0], 1000)
	fr.eventsAnswer = func(w http.ResponseWriter, n int) bool {
		if n == 1 {
			return false
		}
		problem(w, 503, "ServiceUnavailable")
		return true
	}
	fr.register(e)
	r := e.run("build", "S-1", "notes")
	wantExit(t, r, 1)
	msg := strings.TrimRight(r.err, "\n")
	if !strings.HasPrefix(msg, "engine: lost the engine while following RUN-7 (") ||
		!strings.HasSuffix(msg, "); the run goes on or is interrupted: see run RUN-7") {
		t.Errorf("stderr = %q, want the lost-engine message naming RUN-7", r.err)
	}
	if n := len(e.got("GET", "/runs/RUN-7/events")); n != 5 {
		t.Errorf("listRunEvents calls = %d, want the first page and 4 tries of the next", n)
	}
	if e.anyPathWith("/cancel") {
		t.Error("losing the engine must not cancel the run")
	}
}

func TestFollowingAnUnreadableAnswerExits1(t *testing.T) {
	e := newEng(t)
	buildRoutes(e)
	fr := stateRun(t, "RUN-7", "OP-12", endStates("RUN-7", "OP-12")[0], 1000)
	fr.register(e)
	e.on("GET /runs/RUN-7", func(w http.ResponseWriter, _ req) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("{not json"))
	})
	r := e.run("build", "S-1", "notes")
	wantExit(t, r, 1)
	if !strings.HasPrefix(r.err, "engine: ") {
		t.Errorf("stderr = %q, want an engine error", r.err)
	}
}

// ---- Ctrl-C and SIGTERM detach ----

var detachSignals = []struct {
	name string
	sig  os.Signal
	exit int
}{{"SIGINT", os.Interrupt, 130}, {"SIGTERM", syscall.SIGTERM, 143}}

// endlessRun is a run that keeps running with a growing record.
func endlessRun(t *testing.T, e *eng) {
	evs, _ := fullRecord("RUN-7")
	fr := &fakeRun{t: t, id: "RUN-7", op: "OP-12", final: runJSON(runSpec{id: "RUN-7", status: "ended", outcome: "done"}),
		finalOp: operation("OP-12", "run", "succeeded", "RUN-7", map[string]any{"run": "RUN-7"}),
		events:  evs, initial: 2, perRead: 0, runningReads: 1 << 30}
	fr.register(e)
}

func wantDetached(t *testing.T, e *eng, r result, exit int, run string) {
	t.Helper()
	wantExit(t, r, exit)
	want := "stopped following " + run + "; it continues. Follow it with run " + run + ", cancel it with cancel " + run + "."
	if !strings.Contains(oneLine(r.err), want) {
		t.Errorf("stderr = %q, want %q", r.err, want)
	}
	if e.anyPathWith("/cancel") {
		t.Error("detaching must not cancel the run")
	}
}

func TestSignalWhileFollowingABuildDetaches(t *testing.T) {
	for _, s := range detachSignals {
		t.Run(s.name, func(t *testing.T) {
			e := newEng(t)
			buildRoutes(e)
			endlessRun(t, e)
			sigs := make(chan os.Signal, 1)
			done := e.start(sigs, "build", "S-1", "notes")
			e.waitFor("GET", "/runs/RUN-7", 1)
			sigs <- s.sig
			r := await(t, done, 5*time.Second)
			wantDetached(t, e, r, s.exit, "RUN-7")
			wantContains(t, "stdout", r.out, startedLine)
		})
	}
}

func TestSignalWhileRunFollowsDetaches(t *testing.T) {
	for _, s := range detachSignals {
		t.Run(s.name, func(t *testing.T) {
			e := newEng(t)
			endlessRun(t, e)
			sigs := make(chan os.Signal, 1)
			done := e.start(sigs, "run", "RUN-7")
			e.waitFor("GET", "/runs/RUN-7/events", 2)
			sigs <- s.sig
			wantDetached(t, e, await(t, done, 5*time.Second), s.exit, "RUN-7")
		})
	}
}

func TestSignalWhileResumeOrReplayFollowsDetaches(t *testing.T) {
	for _, s := range detachSignals {
		for _, args := range [][]string{{"resume", "RUN-7"}, {"replay", "RUN-3", "notes-2"}} {
			t.Run(s.name+"/"+args[0], func(t *testing.T) {
				e := newEng(t)
				e.reply("POST /runs/RUN-7/resume", 202, operation("OP-12", "run", "running", "RUN-7", nil))
				e.reply("POST /runs/RUN-3/replay", 202, operation("OP-12", "run", "accepted", "RUN-7", nil))
				endlessRun(t, e)
				sigs := make(chan os.Signal, 1)
				done := e.start(sigs, args...)
				e.waitFor("GET", "/runs/RUN-7", 1)
				sigs <- s.sig
				wantDetached(t, e, await(t, done, 5*time.Second), s.exit, "RUN-7")
			})
		}
	}
}

func TestSignalBeforeTheMutatingCallIsAnsweredSaysItMayBeRecorded(t *testing.T) {
	for _, s := range detachSignals {
		t.Run(s.name, func(t *testing.T) {
			e := newEng(t)
			release := make(chan struct{})
			t.Cleanup(func() { close(release) })
			e.on("POST /slices/S-1/runs", func(w http.ResponseWriter, c req) {
				select {
				case <-release:
				case <-time.After(20 * time.Second):
				}
				writeJSON(w, 202, operation("OP-12", "run", "accepted", "RUN-7", nil))
			})
			sigs := make(chan os.Signal, 1)
			done := e.start(sigs, "build", "S-1", "notes")
			e.waitFor("POST", "/slices/S-1/runs", 1)
			sigs <- s.sig
			r := await(t, done, 5*time.Second)
			wantExit(t, r, s.exit)
			wantContains(t, "stderr", oneLine(r.err), "stopped; the request may have been recorded: see runs")
			if r.out != "" {
				t.Errorf("stdout = %q, want nothing before the 202", r.out)
			}
		})
	}
}

func TestSignalWhileAStory1CommandFollowsItsOperationDetaches(t *testing.T) {
	for _, s := range detachSignals {
		t.Run(s.name, func(t *testing.T) {
			e := newEng(t)
			e.reply("POST /slices", 202, operation("OP-1", "propose-slice", "accepted", "S-1", nil))
			e.reply("GET /operations/OP-1", 200, operation("OP-1", "propose-slice", "running", "S-1", nil))
			sigs := make(chan os.Signal, 1)
			done := e.start(sigs, "describe", "a customer can cancel an order")
			e.waitFor("GET", "/operations/OP-1", 1)
			sigs <- s.sig
			r := await(t, done, 5*time.Second)
			wantExit(t, r, s.exit)
			wantContains(t, "stderr", r.err, "OP-1")
			if e.anyPathWith("/cancel") {
				t.Error("detaching must not cancel the operation")
			}
		})
	}
}

func TestSignalWhileTakeOverFollowsItsOperationDetaches(t *testing.T) {
	for _, s := range detachSignals {
		t.Run(s.name, func(t *testing.T) {
			e := newEng(t)
			e.reply("POST /runs/RUN-7/hand-over", 202, operation("OP-12", "run", "running", "RUN-7", nil))
			e.reply("GET /operations/OP-12", 200, operation("OP-12", "run", "running", "RUN-7", nil))
			e.reply("GET /runs/RUN-7", 200, runJSON(runSpec{id: "RUN-7", status: "waiting-for-developer", reason: "out of attempts"}))
			sigs := make(chan os.Signal, 1)
			done := e.start(sigs, "take-over", "RUN-7")
			e.waitFor("GET", "/operations/OP-12", 1)
			sigs <- s.sig
			r := await(t, done, 5*time.Second)
			wantExit(t, r, s.exit)
			wantContains(t, "stderr", r.err, "OP-12")
			if e.anyPathWith("/cancel") {
				t.Error("detaching must not cancel the operation")
			}
		})
	}
}
