// T010 (feature 012, US1): the client's `trials <slice> <prefix> [--trials <k>]` and `trial-set <id>`,
// written against src/cli/contract.md ("Trial sets", "Commands"), the engine contract's startTrialSet,
// getTrialSet and getOperation (src/engine/api/preview/2026-11-12-preview/openapi.yaml, with its
// x-limits) and specs/012-run-variance/contracts/trial-sets.md, before the commands exist. Every command
// runs in process through cli.Run against the stand-in engine of runs_fake_test.go (called, not edited).
//
// The stand-in follows a trial set the way the engine does: each getTrialSet answer is the next stage of
// a script (the last one repeats), and getOperation answers the status of the stage the set is at.
//
// US1 lines only: the first line of the result, the trial lines, every exit code of the table. The trial
// and step tables are judged for their columns and rows (their measure values belong to US2, T016): a
// `trial-set` of an ended set prints the trial lines too or not (the contract says "prints the same" and
// "as the trial ends"), so only their text and order are judged when they are there. Every name here
// starts with ts so no other file collides with it.
//
// Edge cases checked: k 1, 3 (default), 5, and k 0, 6, -1, 1.5, text and an empty value (usage, no
// engine call); a bad slice or set ID; extra arguments; a set that stops (budget, time, slice-changed,
// workers) with and without an ended trial; a set cancelled with no trial, with a done trial; unstable and
// mixed methods separately and together; none done; a trial still running at the end of the script (no
// line, no row outcome); two trials ending between two reads (both lines, in order, once each); a set
// whose operation fails while the set reads running; every refusal of startTrialSet; an unknown set;
// a lost engine and an unreadable answer (exit 1, nothing cancelled); Ctrl-C and SIGTERM while following
// and before the 202 (exit 130 and 143, naming the operation, nothing cancelled); a lost answer to the
// start resent with the same Idempotency-Key.
package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	tsID    = "TS-1"
	tsOp    = "OP-5"
	tsAccep = "TS-1  operation OP-5"
)

// tsSteps are the pattern's steps in order; the trial and step tables have a row for each trial and step.
var tsSteps = []string{"tests", "code", "merge"}

// ---- the trial set as the engine gives it ----

type tsTrial struct{ run, outcome string } // outcome "" while the trial has not ended

type tsSet struct {
	state, stopped string // state: running, ended, cancelled, stopped
	k              int    // 0 means 3
	trials         []tsTrial
	mixed          bool
}

// json is a TrialSet valid by the engine contract: run IDs recorded for the started trials, the result
// computed over them, `ended_at` exactly when the set is not running.
func (s tsSet) json() map[string]any {
	if s.k == 0 {
		s.k = 3
	}
	ended, done := 0, 0
	outcomes, runs := []any{}, []any{}
	for i, tr := range s.trials {
		o := map[string]any{"trial": i + 1, "run": tr.run, "totals": map[string]any{}}
		if s.mixed {
			o["method"] = strings.Repeat(map[bool]string{true: "a", false: "b"}[i == 0], 64)
		}
		if tr.outcome != "" {
			o["outcome"] = tr.outcome
			ended++
			if tr.outcome == "done" {
				done++
			}
		}
		outcomes = append(outcomes, o)
		runs = append(runs, tr.run)
	}
	steps := []any{}
	for i, name := range tsSteps {
		st := map[string]any{"step": name, "reached": 0, "measures": map[string]any{}}
		if i < 2 && len(s.trials) > 0 {
			nums := []any{}
			for n := range s.trials {
				nums = append(nums, n+1)
			}
			st["reached"] = len(s.trials)
			st["files"] = map[string]any{"same": true, "groups": []any{map[string]any{"trials": nums}}}
		}
		steps = append(steps, st)
	}
	m := map[string]any{
		"id": tsID, "slice": "S-1", "trials": s.k, "project_prefix": "notes", "method": strings.Repeat("c", 64),
		"part_hashes": map[string]any{"goals": strings.Repeat("1", 64), "rules": strings.Repeat("2", 64), "contract": strings.Repeat("3", 64)},
		"budget":      map[string]any{"cost_usd": 2, "hours": 24},
		"operation":   tsOp, "state": s.state, "runs": runs, "created_at": tCreated,
		"result": map[string]any{"ended": ended, "done": done, "unstable": done >= 1 && done < ended,
			"methods_mixed": s.mixed, "outcomes": outcomes, "steps": steps, "totals": map[string]any{}},
	}
	if s.stopped != "" {
		m["stopped_because"] = s.stopped
	}
	if s.state != "running" {
		m["ended_at"] = "2026-11-14T10:30:00Z"
	}
	return m
}

// tsRuns are the run IDs of trials 1 to 5.
var tsRuns = []string{"RUN-21", "RUN-22", "RUN-23", "RUN-24", "RUN-25"}

// tsOf is a set with the trials' outcomes, "" for one still running (trial i has the run tsRuns[i]).
func tsOf(state, stopped string, k int, outcomes ...string) tsSet {
	s := tsSet{state: state, stopped: stopped, k: k}
	for i, o := range outcomes {
		s.trials = append(s.trials, tsTrial{tsRuns[i], o})
	}
	return s
}

// tsStage is one answer of getTrialSet and the status getOperation gives while the set is there.
type tsStage struct {
	set      tsSet
	opStatus string         // accepted, running, succeeded, cancelled, failed
	opError  map[string]any // the problem of a failed operation
}

// tsOpFor is the operation status that goes with a set's state.
func tsOpFor(state string) string {
	switch state {
	case "running":
		return "running"
	case "cancelled":
		return "cancelled"
	}
	return "succeeded"
}

func tsAt(s tsSet) tsStage { return tsStage{set: s, opStatus: tsOpFor(s.state)} }

// tsScript is a set that runs through the given stages; the last stage repeats.
type tsScript struct {
	t       *testing.T
	stages  []tsStage
	mu      sync.Mutex
	reads   int
	atFirst func() // called on the first getTrialSet, before it answers
}

func (sc *tsScript) stage() tsStage {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	i := min(max(sc.reads-1, 0), len(sc.stages)-1)
	return sc.stages[i]
}

func (sc *tsScript) readCount() int {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.reads
}

func (sc *tsScript) register(e *eng) {
	e.on("GET /trial-sets/"+tsID, func(w http.ResponseWriter, _ req) {
		sc.mu.Lock()
		sc.reads++
		first := sc.reads == 1
		sc.mu.Unlock()
		if first && sc.atFirst != nil {
			sc.atFirst()
		}
		writeJSON(w, 200, sc.stage().set.json())
	})
	e.on("GET /operations/"+tsOp, func(w http.ResponseWriter, _ req) {
		st := sc.stage()
		var result map[string]any
		if st.opStatus == "succeeded" {
			result = map[string]any{"trial_set": tsID}
		}
		op := operation(tsOp, "run-trials", st.opStatus, tsID, result)
		if st.opError != nil {
			op["error"] = st.opError
		}
		writeJSON(w, 200, op)
	})
}

// tsStart answers startTrialSet for S-1 with the operation OP-5 of the set TS-1.
func tsStart(e *eng) {
	e.reply("POST /slices/S-1/trial-sets", 202, operation(tsOp, "run-trials", "accepted", tsID, nil))
}

// tsRunning is a script that ends in the given set, after the stages of the trials ending one by one.
func tsEndingIn(t *testing.T, e *eng, stages ...tsSet) *tsScript {
	sc := &tsScript{t: t}
	for _, s := range stages {
		sc.stages = append(sc.stages, tsAt(s))
	}
	sc.register(e)
	return sc
}

// ---- reading the output ----

var tsTrialLine = regexp.MustCompile(`^trial [0-9]+  RUN-[0-9]+  \S+$`)

// tsSplit cuts the output at the first line of the result: what comes before it, that line, what follows.
func tsSplit(t *testing.T, out string) (before []string, first string, after []string) {
	t.Helper()
	ls := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, l := range ls {
		if strings.HasPrefix(l, tsID+"  slice ") {
			return ls[:i], l, ls[i+1:]
		}
	}
	t.Fatalf("no line of the result (%q...) in:\n%s", tsID+"  slice ", out)
	return nil, "", nil
}

func tsTrialLines(set tsSet) []string {
	var out []string
	for i, tr := range set.trials {
		if tr.outcome != "" {
			out = append(out, fmt.Sprintf("trial %d  %s  %s", i+1, tr.run, tr.outcome))
		}
	}
	return out
}

func tsNonBlank(ls []string) []string {
	var out []string
	for _, l := range ls {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// tsWantTables checks the lines after the first: the trials' table (the columns RUN, OUTCOME, ATTEMPTS,
// ROUNDS, SECONDS, TOKENS, COST; no TRIAL column) with one row per started trial in trial order, then the
// steps' table (STEP, REACHED, ATTEMPTS, ROUNDS, SECONDS, TOKENS, COST, FILES) with one row per step of
// the pattern in step order. The values of the measure columns are not judged (US2).
func tsWantTables(t *testing.T, after []string, set tsSet) {
	t.Helper()
	trialHdr := []string{"RUN", "OUTCOME", "ATTEMPTS", "ROUNDS", "SECONDS", "TOKENS", "COST"}
	stepHdr := []string{"STEP", "REACHED", "ATTEMPTS", "ROUNDS", "SECONDS", "TOKENS", "COST", "FILES"}
	same := func(a, b []string) bool { return strings.Join(a, " ") == strings.Join(b, " ") }
	th, sh := -1, -1
	for i, l := range after {
		switch f := strings.Fields(l); {
		case same(f, trialHdr):
			th = i
		case same(f, stepHdr):
			sh = i
		}
	}
	if th < 0 || sh < 0 || th > sh {
		t.Fatalf("want the trials' table (%v) then the steps' table (%v) after the first line, got:\n%s",
			trialHdr, stepHdr, strings.Join(after, "\n"))
	}
	rows := tsNonBlank(after[th+1 : sh])
	if len(rows) != len(set.trials) {
		t.Errorf("the trials' table has %d rows, want one per started trial (%d):\n%s", len(rows), len(set.trials), strings.Join(rows, "\n"))
	} else {
		for i, row := range rows {
			f := strings.Fields(row)
			want := set.trials[i].outcome
			if want == "" {
				want = "-"
			}
			if len(f) != len(trialHdr) || f[0] != set.trials[i].run || f[1] != want {
				t.Errorf("row %d of the trials' table = %q, want %d columns, the run %s and the outcome %s", i+1, row, len(trialHdr), set.trials[i].run, want)
			}
		}
	}
	var srows []string
	for _, l := range tsNonBlank(after[sh+1:]) {
		if strings.HasPrefix(l, "compared with ") {
			break
		}
		srows = append(srows, l)
	}
	if len(srows) != len(tsSteps) {
		t.Errorf("the steps' table has %d rows, want one per step (%d):\n%s", len(srows), len(tsSteps), strings.Join(srows, "\n"))
		return
	}
	for i, row := range srows {
		f := strings.Fields(row)
		if len(f) != len(stepHdr) || f[0] != tsSteps[i] {
			t.Errorf("row %d of the steps' table = %q, want %d columns and the step %s", i+1, row, len(stepHdr), tsSteps[i])
		}
	}
}

// wantTrialLines compares the trial lines printed before the result's first line: for `trials` every one
// of them, for `trial-set` of an ended set all of them or none.
func tsWantTrialLines(t *testing.T, before []string, want []string, mayBeNone bool) {
	t.Helper()
	if mayBeNone && len(before) == 0 {
		return
	}
	if strings.Join(before, "\n") != strings.Join(want, "\n") {
		t.Errorf("lines before the result:\n%s\nwant one line per ended trial, in trial order:\n%s", strings.Join(before, "\n"), strings.Join(want, "\n"))
	}
	for _, l := range before {
		if !tsTrialLine.MatchString(l) {
			t.Errorf("line %q before the result is not `trial <i>  RUN-<n>  <outcome>`", l)
		}
	}
}

// ---- trials: the accepted line, a line per trial, the result ----

func TestTrialsPrintsTheAcceptedLineThenOneLinePerTrialThenTheResult(t *testing.T) {
	e := newEng(t)
	tsStart(e)
	var out syncBuf
	var atFirst string
	sc := tsEndingIn(t, e,
		tsOf("running", "", 3),
		tsOf("running", "", 3, "done", ""),
		tsOf("running", "", 3, "done", "done", ""),
		tsOf("ended", "", 3, "done", "done", "done"))
	sc.atFirst = func() { atFirst = out.String() }
	var errb syncBuf
	code := runTo(e, &out, &errb, "trials", "S-1", "notes")
	r := result{code, out.String(), errb.String()}
	wantExit(t, r, 0)
	before, first, after := tsSplit(t, r.out)
	if len(before) == 0 || before[0] != tsAccep {
		t.Fatalf("the first line = %q, want %q\n%s", before, tsAccep, r.out)
	}
	tsWantTrialLines(t, before[1:], []string{"trial 1  RUN-21  done", "trial 2  RUN-22  done", "trial 3  RUN-23  done"}, false)
	if want := "TS-1  slice S-1  state ended  done 3/3"; first != want {
		t.Errorf("first line of the result = %q, want %q", first, want)
	}
	tsWantTables(t, after, tsOf("ended", "", 3, "done", "done", "done"))
	if atFirst != tsAccep+"\n" {
		t.Errorf("before the set was first read, stdout was %q, want only the accepted line", atFirst)
	}
}

func TestTrialsStartsTheSetWithThePrefixAndTheCountUnderOneKey(t *testing.T) {
	for _, c := range []struct {
		name  string
		extra []string
		want  int // 0: the engine's default, so the body may leave `trials` out or say 3
	}{
		{"default", nil, 0}, {"one", []string{"--trials", "1"}, 1}, {"two", []string{"--trials", "2"}, 2},
		{"three", []string{"--trials", "3"}, 3}, {"five", []string{"--trials", "5"}, 5},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEng(t)
			tsStart(e)
			tsEndingIn(t, e, tsOf("ended", "", 3, "done"))
			r := e.run(append([]string{"trials", "S-1", "notes"}, c.extra...)...)
			wantExit(t, r, 0)
			posts := e.got("POST", "/slices/S-1/trial-sets")
			if len(posts) != 1 {
				t.Fatalf("startTrialSet calls = %d, want 1", len(posts))
			}
			var b map[string]any
			if err := json.Unmarshal(posts[0].Body, &b); err != nil {
				t.Fatalf("body %q is not JSON: %v", posts[0].Body, err)
			}
			if b["project_prefix"] != "notes" {
				t.Errorf("body = %s, want the project_prefix notes", posts[0].Body)
			}
			n, has := b["trials"]
			switch {
			case c.want == 0 && has && n != float64(3):
				t.Errorf("body = %s, want no trials or the default 3", posts[0].Body)
			case c.want != 0 && n != float64(c.want):
				t.Errorf("body = %s, want trials %d", posts[0].Body, c.want)
			}
			for k := range b {
				if k != "project_prefix" && k != "trials" {
					t.Errorf("body = %s has the field %q the contract does not give", posts[0].Body, k)
				}
			}
			if key := posts[0].Header.Get("Idempotency-Key"); !keyShape.MatchString(key) {
				t.Errorf("Idempotency-Key = %q, want 32 lowercase hex digits", key)
			}
		})
	}
}

func TestTrialsPrintsATrialsLineOnlyOnceItEndsEachOnceInOrder(t *testing.T) {
	e := newEng(t)
	tsStart(e)
	// trial 1 is read running, then trials 1 and 2 are read ended together, then trial 3 ends.
	tsEndingIn(t, e,
		tsOf("running", "", 3, ""),
		tsOf("running", "", 3, "done", "handed-over", ""),
		tsOf("running", "", 3, "done", "handed-over", ""),
		tsOf("ended", "", 3, "done", "handed-over", "expired"))
	r := e.run("trials", "S-1", "notes")
	wantExit(t, r, 7)
	before, first, after := tsSplit(t, r.out)
	tsWantTrialLines(t, before[1:], []string{"trial 1  RUN-21  done", "trial 2  RUN-22  handed-over", "trial 3  RUN-23  expired"}, false)
	if want := "TS-1  slice S-1  state ended  done 1/3  unstable"; first != want {
		t.Errorf("first line of the result = %q, want %q", first, want)
	}
	tsWantTables(t, after, tsOf("ended", "", 3, "done", "handed-over", "expired"))
}

func TestTrialsTrialLineIsNotPrintedWhileItsRunIsStillRunning(t *testing.T) {
	e := newEng(t)
	tsStart(e)
	var out syncBuf
	var atLast string
	sc := tsEndingIn(t, e,
		tsOf("running", "", 2, ""),
		tsOf("running", "", 2, "done", ""),
		tsOf("running", "", 2, "done", ""),
		tsOf("ended", "", 2, "done", "done"))
	e.on("GET /trial-sets/"+tsID, func(w http.ResponseWriter, _ req) {
		sc.mu.Lock()
		sc.reads++
		n := sc.reads
		sc.mu.Unlock()
		if n == 4 { // after two reads that showed trial 2 still running
			atLast = out.String()
		}
		writeJSON(w, 200, sc.stage().set.json())
	})
	var errb syncBuf
	code := runTo(e, &out, &errb, "trials", "S-1", "notes", "--trials", "2")
	wantExit(t, result{code, out.String(), errb.String()}, 0)
	if want := tsAccep + "\ntrial 1  RUN-21  done\n"; atLast != want {
		t.Errorf("while trial 2 ran, stdout was %q, want %q", atLast, want)
	}
}

// ---- the result of every kind of end, and its exit code ----

type tsEnd struct {
	name  string
	set   tsSet
	first string
	exit  int
}

func tsEnds() []tsEnd {
	mixed := func(s tsSet) tsSet { s.mixed = true; return s }
	return []tsEnd{
		{"every trial done", tsOf("ended", "", 3, "done", "done", "done"), "TS-1  slice S-1  state ended  done 3/3", 0},
		{"one trial done", tsOf("ended", "", 1, "done"), "TS-1  slice S-1  state ended  done 1/1", 0},
		{"five trials done", tsOf("ended", "", 5, "done", "done", "done", "done", "done"), "TS-1  slice S-1  state ended  done 5/5", 0},
		{"one not done is unstable", tsOf("ended", "", 3, "done", "handed-over", "done"), "TS-1  slice S-1  state ended  done 2/3  unstable", 7},
		{"one done of three is unstable", tsOf("ended", "", 3, "expired", "done", "handed-over"), "TS-1  slice S-1  state ended  done 1/3  unstable", 7},
		{"none done is not unstable", tsOf("ended", "", 3, "expired", "handed-over", "expired"), "TS-1  slice S-1  state ended  done 0/3", 7},
		{"one trial not done", tsOf("ended", "", 1, "handed-over"), "TS-1  slice S-1  state ended  done 0/1", 7},
		{"methods mixed, every trial done", mixed(tsOf("ended", "", 3, "done", "done", "done")),
			"TS-1  slice S-1  state ended  done 3/3  methods mixed", 0},
		{"unstable and methods mixed", mixed(tsOf("ended", "", 3, "done", "expired", "done")),
			"TS-1  slice S-1  state ended  done 2/3  unstable  methods mixed", 7},
		{"stopped by the budget after done trials", tsOf("stopped", "budget", 3, "done", "done"),
			"TS-1  slice S-1  state stopped  stopped budget  done 2/2", 0},
		{"stopped by the time, the running trial cancelled", tsOf("stopped", "time", 3, "done", "cancelled"),
			"TS-1  slice S-1  state stopped  stopped time  done 1/2  unstable", 7},
		{"stopped because the slice changed, no trial ended", tsOf("stopped", "slice-changed", 3),
			"TS-1  slice S-1  state stopped  stopped slice-changed  done 0/0", 7},
		{"stopped because the workers are gone after one done trial", tsOf("stopped", "workers", 2, "done"),
			"TS-1  slice S-1  state stopped  stopped workers  done 1/1", 0},
		{"cancelled in the first trial", tsOf("cancelled", "", 3, "cancelled"), "TS-1  slice S-1  state cancelled  done 0/1", 6},
		{"cancelled after a done trial", tsOf("cancelled", "", 3, "done", "cancelled"),
			"TS-1  slice S-1  state cancelled  done 1/2  unstable", 6},
		{"cancelled before any trial", tsOf("cancelled", "", 3), "TS-1  slice S-1  state cancelled  done 0/0", 6},
	}
}

func TestTrialsEndsWithTheResultAndTheExitOfTheSetsEnd(t *testing.T) {
	for _, c := range tsEnds() {
		t.Run(c.name, func(t *testing.T) {
			e := newEng(t)
			tsStart(e)
			tsEndingIn(t, e, tsOf("running", "", c.set.k), c.set)
			r := e.run("trials", "S-1", "notes")
			wantExit(t, r, c.exit)
			before, first, after := tsSplit(t, r.out)
			if len(before) == 0 || before[0] != tsAccep {
				t.Fatalf("the first line = %q, want %q", before, tsAccep)
			}
			tsWantTrialLines(t, before[1:], tsTrialLines(c.set), false)
			if first != c.first {
				t.Errorf("first line of the result = %q, want %q", first, c.first)
			}
			tsWantTables(t, after, c.set)
			if e.anyPathWith("/cancel") {
				t.Error("following a set must not cancel it")
			}
		})
	}
}

func TestTrialSetOfAnEndedSetPrintsTheResultAndExitsByTheEnd(t *testing.T) {
	for _, c := range tsEnds() {
		t.Run(c.name, func(t *testing.T) {
			e := newEng(t)
			sc := tsEndingIn(t, e, c.set)
			r := e.run("trial-set", tsID)
			wantExit(t, r, c.exit)
			before, first, after := tsSplit(t, r.out)
			tsWantTrialLines(t, before, tsTrialLines(c.set), true)
			if first != c.first {
				t.Errorf("first line of the result = %q, want %q", first, c.first)
			}
			tsWantTables(t, after, c.set)
			if sc.readCount() == 0 {
				t.Error("getTrialSet was never called")
			}
			if n := len(e.got("POST", "/slices/S-1/trial-sets")); n != 0 {
				t.Errorf("trial-set started %d sets, want none", n)
			}
			if e.anyPathWith("/cancel") {
				t.Error("reading a set must not cancel it")
			}
		})
	}
}

func TestTrialSetFollowsARunningSetAndPrintsTheLinesAsTrialsEnd(t *testing.T) {
	e := newEng(t)
	sc := tsEndingIn(t, e,
		tsOf("running", "", 3, ""),
		tsOf("running", "", 3, "done", ""),
		tsOf("running", "", 3, "done", "handed-over", ""),
		tsOf("ended", "", 3, "done", "handed-over", "done"))
	r := e.run("trial-set", tsID)
	wantExit(t, r, 7)
	before, first, after := tsSplit(t, r.out)
	tsWantTrialLines(t, before, []string{"trial 1  RUN-21  done", "trial 2  RUN-22  handed-over", "trial 3  RUN-23  done"}, false)
	if want := "TS-1  slice S-1  state ended  done 2/3  unstable"; first != want {
		t.Errorf("first line of the result = %q, want %q", first, want)
	}
	tsWantTables(t, after, tsOf("ended", "", 3, "done", "handed-over", "done"))
	if sc.readCount() < 4 {
		t.Errorf("getTrialSet calls = %d, want it followed until the set ended (4 stages)", sc.readCount())
	}
}

func TestTrialSetFollowingACancelledSetExits6(t *testing.T) {
	e := newEng(t)
	tsEndingIn(t, e, tsOf("running", "", 3, ""), tsOf("cancelled", "", 3, "cancelled"))
	r := e.run("trial-set", tsID)
	wantExit(t, r, 6)
	before, first, _ := tsSplit(t, r.out)
	tsWantTrialLines(t, before, []string{"trial 1  RUN-21  cancelled"}, false)
	if want := "TS-1  slice S-1  state cancelled  done 0/1"; first != want {
		t.Errorf("first line of the result = %q, want %q", first, want)
	}
}

// ---- refusals and failures ----

func TestTrialsRefusedAtOnceExits4WithTheProblemAndFollowsNothing(t *testing.T) {
	cases := []struct {
		status int
		code   string
	}{
		{400, "InvalidRequest"}, {404, "NotFound"}, {409, "PatternDoesNotFit"}, {409, "NotApproved"},
		{409, "CapturedPatternWithdrawn"}, {409, "CapturedPatternBusy"}, {409, "WorkersNotReady"},
		{409, "ProjectExists"}, {409, "RunActive"}, {409, "IdempotencyKeyReused"},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			e := newEng(t)
			e.refuse("POST /slices/S-1/trial-sets", c.status, prob(c.code, c.code, "the engine says no to notes-2"))
			r := e.run("trials", "S-1", "notes")
			wantExit(t, r, 4)
			if r.out != "" {
				t.Errorf("stdout = %q, want nothing: no set was accepted", r.out)
			}
			wantContains(t, "stderr", r.err, c.code, "notes-2")
			if e.anyPathWith("/trial-sets/") || e.anyPathWith("/operations/") {
				t.Error("nothing was accepted, so nothing is followed")
			}
			if n := len(e.got("POST", "/slices/S-1/trial-sets")); n != 1 {
				t.Errorf("startTrialSet calls = %d, want 1: a refusal is not retried", n)
			}
		})
	}
}

func TestTrialSetOfAnUnknownSetExits4NotFound(t *testing.T) {
	e := newEng(t)
	e.refuse("GET /trial-sets/TS-9", 404, prob("NotFound", "NotFound", "no trial set TS-9"))
	r := e.run("trial-set", "TS-9")
	wantExit(t, r, 4)
	if r.out != "" {
		t.Errorf("stdout = %q, want nothing", r.out)
	}
	wantContains(t, "stderr", r.err, "NotFound")
}

func TestAFailedSetOperationPrintsItsProblemAndExits4(t *testing.T) {
	problem := prob("OperationFailed", "the operation failed", "the store is full")
	for _, args := range [][]string{{"trials", "S-1", "notes"}, {"trial-set", tsID}} {
		t.Run(args[0], func(t *testing.T) {
			e := newEng(t)
			tsStart(e)
			sc := &tsScript{t: t, stages: []tsStage{
				tsAt(tsOf("running", "", 3, "done", "")),
				{set: tsOf("running", "", 3, "done", ""), opStatus: "failed", opError: problem}}}
			sc.register(e)
			r := e.run(args...)
			wantExit(t, r, 4)
			wantContains(t, "stderr", oneLine(r.err), "OperationFailed", "the store is full")
			wantContains(t, "stdout", r.out, "trial 1  RUN-21  done")
			wantAbsent(t, "stdout", r.out, tsID+"  slice ")
		})
	}
}

func TestLosingTheEngineWhileFollowingASetExits1AndCancelsNothing(t *testing.T) {
	for _, args := range [][]string{{"trials", "S-1", "notes"}, {"trial-set", tsID}} {
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()
			e := newEng(t)
			tsStart(e)
			sc := tsEndingIn(t, e, tsOf("running", "", 3, ""))
			e.on("GET /trial-sets/"+tsID, func(w http.ResponseWriter, _ req) {
				sc.mu.Lock()
				sc.reads++
				n := sc.reads
				sc.mu.Unlock()
				if n == 1 {
					writeJSON(w, 200, tsOf("running", "", 3, "").json())
					return
				}
				problem(w, 503, "ServiceUnavailable")
			})
			r := e.run(args...)
			wantExit(t, r, 1)
			if !strings.HasPrefix(r.err, "engine: ") {
				t.Errorf("stderr = %q, want an engine error", r.err)
			}
			if e.anyPathWith("/cancel") {
				t.Error("losing the engine must not cancel the set")
			}
		})
	}
}

func TestAnUnreadableAnswerWhileFollowingASetExits1(t *testing.T) {
	for _, args := range [][]string{{"trials", "S-1", "notes"}, {"trial-set", tsID}} {
		t.Run(args[0], func(t *testing.T) {
			e := newEng(t)
			tsStart(e)
			tsEndingIn(t, e, tsOf("running", "", 3, ""))
			e.on("GET /trial-sets/"+tsID, func(w http.ResponseWriter, _ req) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(200)
				_, _ = w.Write([]byte("{not json"))
			})
			r := e.run(args...)
			wantExit(t, r, 1)
			if !strings.HasPrefix(r.err, "engine: ") {
				t.Errorf("stderr = %q, want an engine error", r.err)
			}
		})
	}
}

func TestALostAnswerToTheStartIsResentWithTheSameKeyAndTheSetStartsOnce(t *testing.T) {
	t.Parallel()
	e := newEng(t)
	var mu sync.Mutex
	n := 0
	e.on("POST /slices/S-1/trial-sets", func(w http.ResponseWriter, _ req) {
		mu.Lock()
		n++
		i := n
		mu.Unlock()
		if i == 1 {
			drop(w)
			return
		}
		writeJSON(w, 202, operation(tsOp, "run-trials", "accepted", tsID, nil))
	})
	tsEndingIn(t, e, tsOf("ended", "", 3, "done"))
	r := e.run("trials", "S-1", "notes")
	wantExit(t, r, 0)
	wantOneKey(t, e.got("POST", "/slices/S-1/trial-sets"), "")
	if got := len(e.got("POST", "/slices/S-1/trial-sets")); got != 2 {
		t.Errorf("startTrialSet tries = %d, want the lost one and its resend", got)
	}
	if c := strings.Count(r.out, tsAccep); c != 1 {
		t.Errorf("the accepted line is printed %d times, want 1:\n%s", c, r.out)
	}
}

// ---- Ctrl-C and SIGTERM detach ----

func TestSignalWhileFollowingASetDetachesNamingTheOperation(t *testing.T) {
	for _, s := range detachSignals {
		for _, args := range [][]string{{"trials", "S-1", "notes"}, {"trial-set", tsID}} {
			t.Run(s.name+"/"+args[0], func(t *testing.T) {
				e := newEng(t)
				tsStart(e)
				sc := &tsScript{t: t, stages: []tsStage{tsAt(tsOf("running", "", 3, "done", ""))}}
				sc.register(e)
				sigs := make(chan os.Signal, 1)
				done := e.start(sigs, args...)
				e.waitFor("GET", "/trial-sets/"+tsID, 1)
				sigs <- s.sig
				r := await(t, done, 5*time.Second)
				wantExit(t, r, s.exit)
				wantContains(t, "stderr", oneLine(r.err), tsOp)
				if e.anyPathWith("/cancel") {
					t.Error("detaching must not cancel the set: it continues")
				}
				if args[0] == "trials" {
					wantContains(t, "stdout", r.out, tsAccep)
				}
				wantAbsent(t, "stdout", r.out, tsID+"  slice ")
			})
		}
	}
}

func TestSignalBeforeTheStartIsAnsweredSaysItMayBeRecorded(t *testing.T) {
	for _, s := range detachSignals {
		t.Run(s.name, func(t *testing.T) {
			e := newEng(t)
			release := make(chan struct{})
			t.Cleanup(func() { close(release) })
			e.on("POST /slices/S-1/trial-sets", func(w http.ResponseWriter, _ req) {
				select {
				case <-release:
				case <-time.After(20 * time.Second):
				}
				writeJSON(w, 202, operation(tsOp, "run-trials", "accepted", tsID, nil))
			})
			sigs := make(chan os.Signal, 1)
			done := e.start(sigs, "trials", "S-1", "notes")
			e.waitFor("POST", "/slices/S-1/trial-sets", 1)
			sigs <- s.sig
			r := await(t, done, 5*time.Second)
			wantExit(t, r, s.exit)
			wantContains(t, "stderr", oneLine(r.err), "the request may have been recorded")
			if r.out != "" {
				t.Errorf("stdout = %q, want nothing before the 202", r.out)
			}
		})
	}
}

// ---- usage errors never reach the engine ----

func TestTrialsAndTrialSetUsageErrorsExit3WithoutCallingTheEngine(t *testing.T) {
	cases := [][]string{
		{"trials"},
		{"trials", "S-1"},
		{"trials", "S-1", "notes", "--trials"},
		{"trials", "S-1", "notes", "--trials", "0"},
		{"trials", "S-1", "notes", "--trials", "6"},
		{"trials", "S-1", "notes", "--trials", "100"},
		{"trials", "S-1", "notes", "--trials", "-1"},
		{"trials", "S-1", "notes", "--trials", "1.5"},
		{"trials", "S-1", "notes", "--trials", "two"},
		{"trials", "S-1", "notes", "--trials", ""},
		{"trials", "not-a-slice", "notes"},
		{"trials", "RUN-1", "notes"},
		{"trials", "S-", "notes"},
		{"trials", "S-1", "notes", "extra"},
		{"trials", "S-1", "notes", "--bogus", "2"},
		{"trial-set"},
		{"trial-set", "S-1"},
		{"trial-set", "RUN-1"},
		{"trial-set", "OP-5"},
		{"trial-set", "TS-"},
		{"trial-set", "TS-x"},
		{"trial-set", "ts-1"},
		{"trial-set", "TS-1", "extra"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := newEng(t)
			r := e.run(args...)
			wantExit(t, r, 3)
			// the commands exist: the usage error is theirs, not an unknown command's
			wantAbsent(t, "stderr", r.err, "unknown command")
			if e.total() != 0 {
				t.Errorf("engine calls = %d, want none for a usage error", e.total())
			}
			if r.out != "" {
				t.Errorf("stdout = %q, want nothing", r.out)
			}
		})
	}
}
