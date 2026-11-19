package machine_test

// T083 (owner, 2026-11-14): a review fix carries the round whose findings it fixes (round 1 for
// the fix after round 1). The machine runs the fix with that round, so its events, its check calls,
// its commitStep (and restoreStep) calls and the worker's request name it; its attempts count from
// 1 in each round; a resumed run continues a fix with the same round (data-model, "Review rounds").
// Written before the code by a separate author.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
)

// roundHistory records the calls of the project worker.
type roundHistory struct {
	mu       sync.Mutex
	restores []machine.RestoreRequest
	commits  []machine.CommitRequest
}

func (h *roundHistory) Restore(_ context.Context, r machine.RestoreRequest) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.restores = append(h.restores, r)
	return nil
}

func (h *roundHistory) Commit(_ context.Context, r machine.CommitRequest) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.commits = append(h.commits, r)
	return true, nil
}

// reviewBlockingIn makes the review step's rounds in `rounds` leave a blocking finding.
func reviewBlockingIn(f *fixture, rounds ...int) {
	f.w.fn["review"] = func(r machine.Request) (machine.Result, error) {
		res := ok(r)
		for _, n := range rounds {
			if r.Round == n {
				res.Findings = []machine.Finding{blocking("R-1", "internal/a.go", 10)}
			}
		}
		return res, nil
	}
}

// codeEvents are the events of the code step in order.
func codeEvents(events []machine.Event) []machine.Event {
	var out []machine.Event
	for _, e := range events {
		if e.Step == "code" {
			out = append(out, e)
		}
	}
	return out
}

type attemptKey struct{ attempt, round int }

func TestFixAfterReviewRoundCarriesTheRoundInEventsChecksAndHistory(t *testing.T) {
	f := newFixture(t, pattern())
	h := &roundHistory{}
	f.m = machine.New(machine.Config{Pattern: pattern(), Worker: f.w, Checker: f.c, Clock: f.clk, Events: f.ev,
		Results: machine.NewResults(), Model: "claude-haiku-5-5", History: h,
		Sleep: func(context.Context, time.Duration) error { return nil }})
	f.c.fn = noBlocking
	reviewBlockingIn(f, 1)
	assertEnded(t, f.start("RUN-1"), f.ev, "done")

	// the worker's requests: the first code run has no round, the fix has round 1, attempt 1
	code := f.w.callsTo("code")
	if len(code) != 2 {
		t.Fatalf("want 2 code runs (the step and one fix), got %d", len(code))
	}
	if code[0].Round != 0 || code[0].Attempt != 1 {
		t.Errorf("the code step's own run: round %d attempt %d, want 0 and 1", code[0].Round, code[0].Attempt)
	}
	if code[1].Round != 1 || code[1].Attempt != 1 || len(code[1].Open) != 1 {
		t.Errorf("the fix after round 1: round %d attempt %d open %d, want round 1, attempt 1, 1 open", code[1].Round, code[1].Attempt, len(code[1].Open))
	}

	// the events of the fix: everything after the first StepAccepted of code carries round 1
	ce := codeEvents(f.ev.events)
	first := -1
	for i, e := range ce {
		if e.Kind == "StepAccepted" {
			first = i
			break
		}
	}
	if first < 0 || first == len(ce)-1 {
		t.Fatalf("setup: no fix events after the code step's StepAccepted: %v", f.ev.kinds())
	}
	for i, e := range ce {
		want := 0
		if i > first {
			want = 1
		}
		if e.Round != want {
			t.Errorf("code event %s (attempt %d) has round %d, want %d", e.Kind, e.Attempt, e.Round, want)
		}
	}
	kinds := map[string]bool{}
	for _, e := range ce[first+1:] {
		kinds[e.Kind] = true
	}
	for _, k := range []string{"StepStarted", "AttemptMade", "CheckPassed", "StepAccepted"} {
		if !kinds[k] {
			t.Errorf("the fix recorded no %s event", k)
		}
	}
	validate(t, f.ev.events)

	// the check calls of the fix
	for _, c := range f.c.calls {
		if c.Step.Name != "code" {
			continue
		}
		want := 0
		if len(c.Result.Findings) == 0 && c.Attempt == 1 && c.Round != 0 {
			want = 1
		}
		if c.Round != want {
			t.Errorf("check %s of code: round %d, want %d", c.Name, c.Round, want)
		}
	}
	var fixChecks int
	for _, c := range f.c.calls {
		if c.Step.Name == "code" && c.Round == 1 {
			fixChecks++
		}
	}
	if fixChecks != len(pattern().Steps[3].Checks) {
		t.Errorf("the fix's checks called with round 1: %d, want %d", fixChecks, len(pattern().Steps[3].Checks))
	}

	// restoreStep and commitStep of the fix
	var restores, commits []int
	for _, r := range h.restores {
		if r.Step.Name == "code" {
			restores = append(restores, r.Round)
		}
	}
	for _, c := range h.commits {
		if c.Step.Name == "code" {
			commits = append(commits, c.Round)
		}
	}
	if len(restores) != 2 || restores[0] != 0 || restores[1] != 1 {
		t.Errorf("restoreStep rounds of code = %v, want [0 1]", restores)
	}
	if len(commits) != 2 || commits[0] != 0 || commits[1] != 1 {
		t.Errorf("commitStep rounds of code = %v, want [0 1] (the fix's commit carries `Round: 1`)", commits)
	}
}

func TestFixAttemptsCountFromOneInEachRound(t *testing.T) {
	f := newFixture(t, pattern())
	f.c.fn = func(r machine.CheckRequest) (machine.CheckResult, error) {
		// the first attempt of the round-1 fix fails its check
		if r.Step.Name == "code" && r.Round == 1 && r.Attempt == 1 && r.Name == "ScopeKept" {
			return fail(r), nil
		}
		return noBlocking(r)
	}
	reviewBlockingIn(f, 1, 2)
	assertEnded(t, f.start("RUN-1"), f.ev, "done")
	var got []attemptKey
	for _, c := range f.w.callsTo("code")[1:] {
		got = append(got, attemptKey{c.Attempt, c.Round})
	}
	want := []attemptKey{{1, 1}, {2, 1}, {1, 2}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("the fixes' (attempt, round) = %v, want %v: attempts count from 1 in each round", got, want)
	}
	var rejected, started []attemptKey
	for _, e := range f.ev.events {
		if e.Step != "code" || e.Round == 0 {
			continue
		}
		switch e.Kind {
		case "StepRejected":
			rejected = append(rejected, attemptKey{e.Attempt, e.Round})
		case "StepStarted":
			started = append(started, attemptKey{0, e.Round})
		}
	}
	if len(rejected) != 1 || rejected[0] != (attemptKey{1, 1}) {
		t.Errorf("StepRejected of fixes = %v, want one: attempt 1 of round 1", rejected)
	}
	if len(started) != 2 || started[0].round != 1 || started[1].round != 2 {
		t.Errorf("StepStarted of fixes = %v, want one in round 1 and one in round 2", started)
	}
	validate(t, f.ev.events)
}

// A run interrupted after the fix of round 1 was accepted and before review round 2 started resumes
// with review round 2: its StepStarted, check calls and worker request carry round 2, and it gets
// round 1's open findings and the lines the fix changed.
func TestResumeAfterAcceptedFixBeforeNextReviewRoundRunsRoundTwo(t *testing.T) {
	fixed := []machine.Line{{File: "internal/a.go", Line: 10}}
	reviewFn := func(r machine.Request) (machine.Result, error) {
		res := ok(r)
		if r.Round == 1 {
			res.Findings = []machine.Finding{blocking("R-1", "internal/a.go", 10)}
		}
		return res, nil
	}
	codeFn := func(r machine.Request) (machine.Result, error) {
		res := ok(r)
		if len(r.Open) > 0 {
			res.ChangedLines = fixed
		}
		return res, nil
	}
	// the first engine runs the whole run; its record is then cut after the fix's StepAccepted
	results, full, clk := machine.NewResults(), listSink{recSink{&sink{}}}, &clock{now: t0}
	w1 := &worker{fn: map[string]func(machine.Request) (machine.Result, error){"review": reviewFn, "code": codeFn}}
	r, err := machine.New(machine.Config{Pattern: pattern(), Worker: w1, Checker: &checker{fn: noBlocking}, Clock: clk,
		Events: full, Results: results, Model: "claude-haiku-5-5"}).Start(context.Background(), (&fixture{t: t}).params("RUN-1"))
	if err != nil || r.Status != machine.StatusEnded {
		t.Fatalf("setup: %v, %s", err, r.Status)
	}
	cut := -1
	for i, e := range full.events {
		if e.Step == "code" && e.Round == 1 && e.Kind == "StepAccepted" {
			cut = i + 1
		}
	}
	if cut < 0 || full.events[cut].Kind != "StepStarted" || full.events[cut].Step != "review" || full.events[cut].Round != 2 {
		t.Fatalf("setup: the event after the fix's StepAccepted is not review round 2's StepStarted: %v", full.kinds())
	}
	ev := listSink{recSink{&sink{events: append([]machine.Event(nil), full.events[:cut]...)}}}

	w2 := &worker{fn: map[string]func(machine.Request) (machine.Result, error){"review": reviewFn, "code": codeFn}}
	c2 := &checker{fn: noBlocking}
	r, err = machine.New(machine.Config{Pattern: pattern(), Worker: w2, Checker: c2, Clock: clk, Events: ev,
		Results: machine.NewResults(), Model: "claude-haiku-5-5"}).Resume(context.Background(), "RUN-1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != machine.StatusEnded || r.Outcome != machine.OutcomeDone {
		t.Fatalf("resumed run = %s/%s %s", r.Status, r.Outcome, r.Reason)
	}
	if n := len(w2.callsTo("code")); n != 0 {
		t.Errorf("the accepted fix ran again: %d code runs", n)
	}
	rev := w2.callsTo("review")
	if len(rev) != 1 || rev[0].Round != 2 || len(rev[0].Open) != 1 || rev[0].Open[0].Rule != "R-1" ||
		len(rev[0].ChangedLines) != 1 || rev[0].ChangedLines[0] != fixed[0] {
		t.Fatalf("resumed review requests = %+v, want one at round 2 with R-1 open and the fix's changed lines", rev)
	}
	for _, c := range c2.calls {
		if c.Step.Name == "review" && c.Round != 2 {
			t.Errorf("check %s of the resumed review has round %d, want 2", c.Name, c.Round)
		}
	}
	started := 0
	for _, e := range ev.events[cut:] {
		if e.Step == "review" && e.Kind == "StepStarted" {
			started++
			if e.Round != 2 {
				t.Errorf("review StepStarted after the resume has round %d, want 2", e.Round)
			}
		}
	}
	if started != 1 {
		t.Errorf("review StepStarted after the resume: %d, want 1", started)
	}
	validate(t, ev.events)
}
