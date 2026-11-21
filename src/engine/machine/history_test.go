package machine_test

// T053e: failing tests for the engine's calls to the project worker (check contract commitStep and
// restoreStep; data-model.md, run rules: pivot, compensation, timeout, commits, what a replay makes
// identical; research D7, D29, D30; ADR 0032). Written before the code, by a separate author.
//
// Seam assumed (new; the smallest the tests need, close to the contract):
//
//	type History interface {
//	    Restore(ctx context.Context, r RestoreRequest) error
//	    Commit(ctx context.Context, r CommitRequest) (created bool, err error)
//	}
//	type RestoreRequest struct {   // restoreStep: the attempt about to start
//	    Run       string
//	    Step      Step
//	    Attempt   int           // from 1
//	    Round     int           // review rounds only, from 1; 0 otherwise
//	    Project   string
//	    Before    []File        // the files the last accepted step left; empty before the first
//	    TimeLimit time.Duration // what is left of the run's time budget, by Config.Clock
//	}
//	type CommitRequest struct {    // commitStep: one accepted or reused step
//	    Run          string
//	    Step         Step
//	    Attempt      int        // the accepted attempt (StepAccepted); 0 for a reused result
//	    Round        int        // the event's round, 0 when it has none
//	    ReusedFrom   string     // ResultReused only: the run that accepted the result
//	    Project      string
//	    Before       []File     // the previous accepted step's project files, empty for the first
//	    ProjectFiles []File     // the event's Files: the accepted attempt's or the reused result's
//	    AcceptedAt   time.Time  // the `at` of the StepAccepted or ResultReused event, unchanged
//	    Outputs      []Artifact // ResultReused only: each declared output that is a set of
//	                            // project files (not the review's findings), in the step's order,
//	                            // with the stored hash the event records
//	    TimeLimit    time.Duration // what is left of the run's time budget
//	}
//	type Artifact struct{ Name, SHA256 string }
//	machine.Config gains:
//	    History History                                   // nil: no version history, no calls
//	    Sleep   func(ctx context.Context, d time.Duration) error // the wait before a retry; nil: real time
//
// Rules pinned: Restore is called before every attempt of every step (a reused step has no
// attempt); Commit after every StepAccepted and ResultReused, before anything for the next step
// and before RunCompleted. A Restore or Commit that fails (any error, a time-out included) is
// retried after a Sleep with a positive wait, at most 3 times (D7); then the run is interrupted
// (never handed over) with the error in its reason, and Resume repeats the call first. Resume
// always commits the last accepted step again before going on (a repeat is safe). With less than
// 1 s of the run's time budget left, the engine does not call: the run is interrupted. The
// steps-side adapter is steps.NewHistory(steps.Config) (see steps/history_test.go).

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
)

// oplog is the order of everything the machine does: worker runs, history calls and events.
type oplog struct {
	mu  sync.Mutex
	ops []string
}

func (l *oplog) add(format string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ops = append(l.ops, fmt.Sprintf(format, a...))
}

func (l *oplog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.ops)
}

func (l *oplog) index(op string) int { return slices.Index(l.all(), op) }

// history is the fake version history.
type history struct {
	log       *oplog
	restores  []machine.RestoreRequest
	commits   []machine.CommitRequest
	restoreFn func(machine.RestoreRequest) error
	commitFn  func(machine.CommitRequest) (bool, error)
	sleeps    []time.Duration
}

func (h *history) Restore(_ context.Context, r machine.RestoreRequest) error {
	h.restores = append(h.restores, r)
	h.log.add("restore %s %d %d", r.Step.Name, r.Attempt, r.Round)
	if h.restoreFn != nil {
		return h.restoreFn(r)
	}
	return nil
}

func (h *history) Commit(_ context.Context, r machine.CommitRequest) (bool, error) {
	h.commits = append(h.commits, r)
	h.log.add("commit %s", r.Step.Name)
	if h.commitFn != nil {
		return h.commitFn(r)
	}
	return true, nil
}

func (h *history) sleep(_ context.Context, d time.Duration) error {
	h.sleeps = append(h.sleeps, d)
	h.log.add("sleep")
	return nil
}

func (h *history) commitsOf(step string) []machine.CommitRequest {
	var out []machine.CommitRequest
	for _, c := range h.commits {
		if c.Step.Name == step {
			out = append(out, c)
		}
	}
	return out
}

func (h *history) restoresOf(step string) []machine.RestoreRequest {
	var out []machine.RestoreRequest
	for _, c := range h.restores {
		if c.Step.Name == step {
			out = append(out, c)
		}
	}
	return out
}

// loggedWorker logs every attempt before the fake worker runs it.
type loggedWorker struct {
	w   *worker
	log *oplog
}

func (l loggedWorker) Run(ctx context.Context, r machine.Request) (machine.Result, error) {
	l.log.add("run %s %d %d", r.Step.Name, r.Attempt, r.Round)
	return l.w.Run(ctx, r)
}

// loggedSink logs every event and can read the record back (a restarted machine resumes from it).
type loggedSink struct {
	*sink
	log *oplog
}

func (s loggedSink) Append(e machine.Event) error {
	s.log.add("event %s %s", e.Kind, e.Step)
	return s.sink.Append(e)
}

func (s loggedSink) Read(run string) ([]machine.Event, error) {
	var out []machine.Event
	for _, e := range s.events {
		if e.Run == run {
			out = append(out, e)
		}
	}
	return out, nil
}

// filesAfter are the project's files after the named step: each step up to it (review aside,
// which writes no file) leaves one file. Paths are in byte order.
func filesAfter(p machine.Pattern, step string) []machine.File {
	var out []machine.File
	for _, s := range p.Steps {
		if !s.Reviews() {
			out = append(out, machine.File{Path: s.Name + "/f.go", SHA256: hash("file/" + s.Name)})
		}
		if s.Name == step {
			break
		}
	}
	slices.SortFunc(out, func(a, b machine.File) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// hfix is a run with a fake version history. Outputs depend only on the step, so a later run's
// keys match and every step is reused; every step reports the project's files after it.
type hfix struct {
	*fixture
	h       *history
	log     *oplog
	sk      loggedSink
	results *machine.Results
	step    time.Duration // how long each attempt takes on the clock
}

func historyFixture(t *testing.T, results *machine.Results) *hfix {
	t.Helper()
	f := newFixture(t, pattern())
	log := &oplog{}
	x := &hfix{fixture: f, h: &history{log: log}, log: log, sk: loggedSink{f.ev, log}, results: results,
		step: 1500 * time.Millisecond}
	for _, s := range pattern().Steps {
		name := s.Name
		f.w.fn[name] = func(r machine.Request) (machine.Result, error) {
			f.clk.Advance(x.step)
			res := ok(r)
			for o := range res.Outputs {
				res.Outputs[o] = hash(name + "/" + o)
			}
			res.Before, res.ProjectFiles = r.Before, filesAfter(pattern(), name)
			return res, nil
		}
	}
	f.c.fn = noBlocking
	f.m = x.build()
	return x
}

// build makes a machine over the fixture's parts; a second call is a restarted engine.
func (x *hfix) build() *machine.Machine {
	return machine.New(machine.Config{Pattern: pattern(), Worker: loggedWorker{x.w, x.log}, Checker: x.c,
		Clock: x.clk, Events: x.sk, Results: x.results, Model: "claude-haiku-5-5", History: x.h, Sleep: x.h.sleep})
}

func (x *hfix) startIn(id, project string) machine.Run {
	x.t.Helper()
	p := x.params(id)
	p.Project = project
	r, err := x.m.Start(context.Background(), p)
	if err != nil {
		x.t.Fatalf("Start(%s): %v", id, err)
	}
	return r
}

// accepts are the StepAccepted and ResultReused events, in order.
func accepts(ev *sink) []machine.Event {
	var out []machine.Event
	for _, e := range ev.events {
		if e.Kind == "StepAccepted" || e.Kind == "ResultReused" {
			out = append(out, e)
		}
	}
	return out
}

func sameFiles(a, b []machine.File) bool {
	return len(a) == len(b) && (len(a) == 0 || slices.Equal(a, b))
}

func assertInterrupted(t *testing.T, r machine.Run, ev *sink, reason string) {
	t.Helper()
	if r.Status != machine.StatusInterrupted {
		t.Fatalf("status = %s (%s), want interrupted; events %v", r.Status, r.Reason, ev.kinds())
	}
	if ev.count("RunHandedOver") != 0 || ev.count("RunCompleted") != 0 {
		t.Fatalf("an interrupted run was handed over or completed: %v", ev.kinds())
	}
	if last := ev.events[len(ev.events)-1].Kind; last != "RunInterrupted" {
		t.Fatalf("last event = %s, want RunInterrupted (%v)", last, ev.kinds())
	}
	if reason != "" && !strings.Contains(r.Reason, reason) {
		t.Fatalf("reason %q does not name %q", r.Reason, reason)
	}
}

// --- compensation: restoreStep before every attempt ------------------------------------------

func TestRestoreBeforeEveryAttemptOfEveryStep(t *testing.T) {
	x := historyFixture(t, nil)
	// tests is rejected once; review round 1 leaves a blocking finding, so code is fixed and
	// review runs round 2
	inner := x.w.fn["review"]
	x.w.fn["review"] = func(r machine.Request) (machine.Result, error) {
		res, err := inner(r)
		if r.Round == 1 {
			res.Findings = []machine.Finding{blocking("R-1", "internal/app/app.go", 3)}
		}
		return res, err
	}
	x.c.fn = func(r machine.CheckRequest) (machine.CheckResult, error) {
		if r.Step.Name == "tests" && r.Attempt == 1 {
			return fail(r), nil
		}
		return noBlocking(r)
	}
	assertEnded(t, x.startIn("RUN-1", "proj-a"), x.ev, "done")

	if len(x.h.restores) != len(x.w.calls) {
		t.Fatalf("restores = %d, attempts = %d; want one restore per attempt", len(x.h.restores), len(x.w.calls))
	}
	ops := x.log.all()
	for i, op := range ops {
		rest, isRun := strings.CutPrefix(op, "run ")
		if !isRun {
			continue
		}
		j := i - 1
		for j >= 0 && strings.HasPrefix(ops[j], "event ") {
			j--
		}
		if j < 0 || ops[j] != "restore "+rest {
			t.Fatalf("attempt %q not preceded by its restore; ops: %v", op, ops)
		}
	}
	for i, c := range x.w.calls {
		r := x.h.restores[i]
		if r.Run != "RUN-1" || r.Project != "proj-a" || r.Step.Name != c.Step.Name || r.Attempt != c.Attempt || r.Round != c.Round {
			t.Errorf("restore %d = %+v, want the attempt %s/%d/%d of RUN-1 in proj-a", i, r, c.Step.Name, c.Attempt, c.Round)
		}
		if !sameFiles(r.Before, c.Before) {
			t.Errorf("restore %d before = %v, the attempt starts from %v", i, r.Before, c.Before)
		}
	}
	if b := x.h.restores[0].Before; len(b) != 0 {
		t.Errorf("first restore before = %v, want empty", b)
	}
	// the second attempt of tests starts from generate's files, not what attempt 1 left
	if tr := x.h.restoresOf("tests"); len(tr) != 2 || !sameFiles(tr[1].Before, filesAfter(pattern(), "generate")) {
		t.Errorf("tests restores = %+v, want 2, the second before generate's files", tr)
	}
	if rr := x.h.restoresOf("review"); len(rr) != 2 || rr[0].Round != 1 || rr[1].Round != 2 {
		t.Errorf("review restores = %+v, want rounds 1 and 2", rr)
	}
	validate(t, x.ev.events)
}

// --- commitStep after every accepted step -----------------------------------------------------

func TestCommitAfterEveryStepAcceptedWithAttemptRoundAndEventTime(t *testing.T) {
	x := historyFixture(t, nil)
	x.c.fn = func(r machine.CheckRequest) (machine.CheckResult, error) {
		if r.Step.Name == "code" && r.Attempt == 1 {
			return fail(r), nil
		}
		return noBlocking(r)
	}
	assertEnded(t, x.startIn("RUN-1", "proj-a"), x.ev, "done")

	acc := accepts(x.ev)
	if len(x.h.commits) != len(acc) || len(acc) != 5 {
		t.Fatalf("commits = %d for %d accepted steps, want 5 and 5", len(x.h.commits), len(acc))
	}
	var before []machine.File
	for i, e := range acc {
		c := x.h.commits[i]
		switch {
		case c.Run != "RUN-1" || c.Project != "proj-a" || c.Step.Name != e.Step:
			t.Errorf("commit %d = %s/%s in %s, want RUN-1/%s in proj-a", i, c.Run, c.Step.Name, c.Project, e.Step)
		case c.Attempt != e.Attempt || c.Round != e.Round || c.ReusedFrom != "" || len(c.Outputs) != 0:
			t.Errorf("commit %s: attempt %d round %d reused %q outputs %v; want attempt %d round %d, nothing reused",
				e.Step, c.Attempt, c.Round, c.ReusedFrom, c.Outputs, e.Attempt, e.Round)
		case !c.AcceptedAt.Equal(e.At):
			t.Errorf("commit %s accepted_at = %v, want the StepAccepted's at %v", e.Step, c.AcceptedAt, e.At)
		case !sameFiles(c.ProjectFiles, e.Files) || !sameFiles(c.ProjectFiles, filesAfter(pattern(), e.Step)):
			t.Errorf("commit %s project_files = %v, want %v", e.Step, c.ProjectFiles, e.Files)
		case !sameFiles(c.Before, before):
			t.Errorf("commit %s before = %v, want the previous step's files %v", e.Step, c.Before, before)
		}
		before = e.Files
	}
	if c := x.h.commitsOf("code"); len(c) != 1 || c[0].Attempt != 2 {
		t.Errorf("code commits = %+v, want one, of attempt 2", c)
	}
}

func TestCommitComesBeforeTheNextStepAndBeforeTheOutcome(t *testing.T) {
	x := historyFixture(t, nil)
	assertEnded(t, x.startIn("RUN-1", "proj-a"), x.ev, "done")
	ops := x.log.all()
	for i, op := range ops {
		if !strings.HasPrefix(op, "event StepAccepted ") {
			continue
		}
		step := strings.TrimPrefix(op, "event StepAccepted ")
		if i+1 >= len(ops) || ops[i+1] != "commit "+step {
			t.Fatalf("%q not followed at once by its commit; ops: %v", op, ops)
		}
	}
	if done, last := x.log.index("event RunCompleted "), x.log.index("commit review"); last < 0 || done < last {
		t.Fatalf("RunCompleted at %d, the last commit at %d; want the commit first: %v", done, last, ops)
	}
}

func TestReviewStepCommitNamesTheRoundThatAcceptedIt(t *testing.T) {
	x := historyFixture(t, nil)
	inner := x.w.fn["review"]
	x.w.fn["review"] = func(r machine.Request) (machine.Result, error) {
		res, err := inner(r)
		if r.Round == 1 {
			res.Findings = []machine.Finding{blocking("R-1", "internal/app/app.go", 3)}
		}
		return res, err
	}
	assertEnded(t, x.startIn("RUN-1", "proj-a"), x.ev, "done")
	rc := x.h.commitsOf("review")
	if len(rc) != 1 || rc[0].Round != 2 || rc[0].Attempt != 1 {
		t.Fatalf("review commits = %+v, want one, round 2 attempt 1", rc)
	}
	if cc := x.h.commitsOf("code"); len(cc) != 2 {
		t.Fatalf("code commits = %d, want 2 (the first acceptance and the fix)", len(cc))
	}
}

// --- timeout ----------------------------------------------------------------------------------

func TestHistoryCallsGetWhatIsLeftOfTheRunBudget(t *testing.T) {
	x := historyFixture(t, nil)
	x.step = time.Minute
	assertEnded(t, x.startIn("RUN-1", "proj-a"), x.ev, "done")
	run := pattern().RunBudget.Duration
	if len(x.h.restores) == 0 || len(x.h.commits) == 0 {
		t.Fatalf("restores = %d, commits = %d; want both called", len(x.h.restores), len(x.h.commits))
	}
	if got := x.h.restores[0].TimeLimit; got != run {
		t.Errorf("first restore time limit = %s, want the whole run budget %s", got, run)
	}
	if got := x.h.commits[0].TimeLimit; got != run-time.Minute {
		t.Errorf("first commit time limit = %s, want %s", got, run-time.Minute)
	}
}

func TestLessThanASecondLeftInterruptsWithoutCalling(t *testing.T) {
	x := historyFixture(t, nil)
	p := pattern()
	p.StepBudget.Duration = p.RunBudget.Duration // the attempt itself stays within budget
	x.step = p.RunBudget.Duration - 500*time.Millisecond
	x.m = machine.New(machine.Config{Pattern: p, Worker: loggedWorker{x.w, x.log}, Checker: x.c, Clock: x.clk,
		Events: x.sk, Model: "claude-haiku-5-5", History: x.h, Sleep: x.h.sleep})
	r := x.startIn("RUN-1", "proj-a")
	if x.ev.count("StepAccepted") != 1 {
		t.Fatalf("setup: events = %v", x.ev.kinds())
	}
	if len(x.h.commits) != 0 {
		t.Fatalf("commitStep called with %s left: %+v", x.h.commits[0].TimeLimit, x.h.commits[0])
	}
	assertInterrupted(t, r, x.ev, "")
}

// --- a failing call: retried with backoff, then interrupted, repeated on resume ---------------

// --- a crash between StepAccepted and its commit ----------------------------------------------
