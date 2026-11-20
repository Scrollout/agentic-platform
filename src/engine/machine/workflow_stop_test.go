package machine_test

// T012 cases (a) to (e) and (m): the run workflow's events, and a stop (an activity ending with a
// heartbeat timeout) in the middle of a run. The interface is in workflow_durable_test.go.

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	"go.temporal.io/sdk/temporal"
)

type wfEv struct {
	Kind, Step     string
	Attempt, Round int
}

// (a) An uninterrupted run records, in order, the events feature 001's driver records for the same
// pattern and fakes: every check passes, the review finds nothing.
func TestWorkflowUninterruptedRunRecordsFeature001Events(t *testing.T) {
	h := newWF(t, wfPattern())
	h.run()
	h.wantOutcome(machine.OutcomeDone)

	want := []wfEv{{Kind: "RunStarted"}}
	for _, s := range wfPattern().Steps {
		round := 0
		if s.Name == "review" {
			round = 1
		}
		want = append(want, wfEv{"StepStarted", s.Name, 0, round}, wfEv{"AttemptMade", s.Name, 1, round})
		for range s.Checks {
			want = append(want, wfEv{"CheckPassed", s.Name, 1, round})
		}
		want = append(want, wfEv{"StepAccepted", s.Name, 1, round})
	}
	want = append(want, wfEv{Kind: "RunCompleted"})
	var got []wfEv
	for _, e := range h.events() {
		got = append(got, wfEv{e.Kind, e.Step, e.Attempt, e.Round})
	}
	if !slices.Equal(got, want) {
		t.Fatalf("events:\n got  %v\n want %v", got, want)
	}
	wfValidate(t, h.events())

	ev := h.events()
	if ev[0].Pattern != "api-feature-go@1.0.0" || ev[0].Slice != "S-1" || !slices.Equal(ev[0].Refs, h.in.Refs) {
		t.Fatalf("RunStarted = %+v", ev[0])
	}
	if !slices.Equal(ev[0].Inputs, []string{wfHash("contract"), wfHash("rules")}) {
		t.Fatalf("RunStarted inputs = %v, want the slice's parts sorted by name", ev[0].Inputs)
	}
	if h.records[0].Project != h.in.Project {
		t.Fatalf("RunStarted's record names project %q, want %q", h.records[0].Project, h.in.Project)
	}
	for _, e := range h.eventsOf("AttemptMade") {
		var step machine.Step
		for _, s := range wfPattern().Steps {
			if s.Name == e.Step {
				step = s
			}
		}
		model, round := "", 0
		if step.AgentDigest != "" {
			model = wfModel
		}
		if step.Name == "review" {
			round = 1
		}
		key := machine.ReuseKey(machine.KeyParts{PatternVersion: "api-feature-go@1.0.0", Step: e.Step, Inputs: e.Inputs,
			AgentDigest: step.AgentDigest, Model: model, Round: round})
		if e.ReuseKey != key || e.Worker != step.Worker || e.Model != model || e.AgentDigest != step.AgentDigest {
			t.Fatalf("AttemptMade %s = %+v, want worker %s, model %q, digest %q, reuse key %s", e.Step, e, step.Worker, model, step.AgentDigest, key)
		}
	}
	if got := h.commitsOf("code"); got != 1 || len(h.stamps) != 5 {
		t.Fatalf("commits = %d (code %d), want one per accepted step", len(h.stamps), got)
	}
}

// (b) A heartbeat timeout in the middle of an attempt: RunInterrupted `the platform stopped`, then
// RunResumed `continued after the platform stopped`; the project is restored before the repeat;
// the same attempt is made again with the same number and does not count as failed.
func TestWorkflowHeartbeatTimeoutRepeatsTheSameAttempt(t *testing.T) {
	h := newWF(t, wfPattern())
	h.stop = wfFirst(durable.CallWorker, 1, wfWorkerOf("code"))
	h.run()
	h.wantOutcome(machine.OutcomeDone)

	if got := attemptsOf(h.workerCalls("code")); !slices.Equal(got, []int{1, 1}) {
		t.Fatalf("code attempts = %v, want [1 1]: the stopped attempt made again with its number", got)
	}
	if n := h.stopPairs(); n != 1 {
		t.Fatalf("%d stop pairs, want 1: %v", n, h.kinds())
	}
	stopped := h.index("RunInterrupted", 0)
	if made := h.index("AttemptMade", stopped); made < 0 || h.events()[made].Step != "code" || h.events()[made].Attempt != 1 {
		t.Fatalf("after the stop the next attempt is not code's attempt 1: %v", h.kinds())
	}
	for _, e := range h.eventsOf("StepRejected") {
		t.Fatalf("the stopped attempt counted as failed: %+v", e)
	}
	// restored before the repeat: a restore for code attempt 1 lies between the stopped call and
	// the repeat
	isCode := func(c wfCall) bool {
		w, ok := c.In.(durable.WorkerCall)
		return ok && w.Step.Name == "code"
	}
	first, second := h.callIndex(0, isCode), h.callIndex(1, isCode)
	restored := h.callIndex(1, func(c wfCall) bool {
		r, ok := c.In.(machine.RestoreRequest)
		return ok && r.Step.Name == "code" && r.Attempt == 1
	})
	if first >= restored || restored >= second {
		t.Fatalf("restore for the repeat at call %d, want between the stopped call %d and the repeat %d", restored, first, second)
	}
}

// (b) A step whose checks fail 3 times across stops hands over, never a fourth attempt (FR-003).
func TestWorkflowChecksFailingThreeTimesAcrossStopsHandOver(t *testing.T) {
	h := newWF(t, wfPattern())
	h.check = func(c durable.CheckCall) (machine.CheckResult, error) {
		if c.Step.Name == "code" {
			return wfFail(c), nil
		}
		return wfDefaultCheck(c)
	}
	h.stop = wfFirst(durable.CallWorker, 2, wfWorkerOf("code")) // the first call of attempt 2
	h.run()

	if got := attemptsOf(h.workerCalls("code")); !slices.Equal(got, []int{1, 2, 2, 3}) {
		t.Fatalf("code attempts = %v, want [1 2 2 3]", got)
	}
	if n := h.stopPairs(); n != 1 {
		t.Fatalf("%d stop pairs, want 1: %v", n, h.kinds())
	}
	rejected := 0
	for _, e := range h.eventsOf("StepRejected") {
		if e.Step == "code" {
			rejected++
		}
	}
	if rejected != 3 || len(h.eventsOf("RunHandedOver")) != 1 {
		t.Fatalf("code rejected %d times, %d hand-overs; want 3 and 1: %v", rejected, len(h.eventsOf("RunHandedOver")), h.kinds())
	}
	if len(h.workerCalls("review")) != 0 {
		t.Fatal("review ran after code was not accepted")
	}
}

// (c) A stop between StepAccepted and its commit commits once: whether the commit had taken effect
// or not, the project holds one commit of the step, the step is not run again, and the next step
// starts only after the commit.
func TestWorkflowStopBetweenStepAcceptedAndCommitCommitsOnce(t *testing.T) {
	commitOf := func(in any) bool { return in.(machine.CommitRequest).Step.Name == "generate" }
	for _, tc := range []struct {
		name  string
		after bool // the commit took effect before the stop
	}{{"before the commit", false}, {"after the commit", true}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWF(t, wfPattern())
			if tc.after {
				h.stopAfter = wfFirst(durable.CommitStep, 1, commitOf)
			} else {
				h.stop = wfFirst(durable.CommitStep, 1, commitOf)
			}
			h.run()
			h.wantOutcome(machine.OutcomeDone)

			if got := h.commitsOf("generate"); got != 1 {
				t.Fatalf("generate committed %d times, want 1", got)
			}
			accepted := 0
			for _, e := range h.eventsOf("StepAccepted") {
				if e.Step == "generate" {
					accepted++
				}
			}
			if accepted != 1 || len(h.workerCalls("generate")) != 1 {
				t.Fatalf("generate accepted %d times, run %d times; want 1 and 1", accepted, len(h.workerCalls("generate")))
			}
			if n := h.stopPairs(); n != 1 {
				t.Fatalf("%d stop pairs, want 1: %v", n, h.kinds())
			}
			acceptedAt := slices.IndexFunc(h.events(), func(e machine.Event) bool { return e.Kind == "StepAccepted" && e.Step == "generate" })
			if acceptedAt < 0 || h.index("RunInterrupted", acceptedAt) < 0 {
				t.Fatalf("the stop is not recorded after generate's StepAccepted: %v", h.kinds())
			}
			isCommit := func(c wfCall) bool {
				r, ok := c.In.(machine.CommitRequest)
				return ok && r.Step.Name == "generate" && c.Err == nil
			}
			testsRestore := func(c wfCall) bool {
				r, ok := c.In.(machine.RestoreRequest)
				return ok && r.Step.Name == "tests"
			}
			if done, next := h.callIndex(0, isCommit), h.callIndex(0, testsRestore); done < 0 || next < done {
				t.Fatalf("tests started (call %d) before generate's commit succeeded (call %d)", next, done)
			}
		})
	}
}

// (d) A stop during a review round keeps the round's findings, the open findings and the scope it
// is given, and the review still runs at most 3 rounds.
func TestWorkflowStopDuringReviewRoundKeepsFindingsAndScope(t *testing.T) {
	h := newWF(t, wfPattern())
	f := wfBlocking("R-1", 12)
	fixed := []machine.Line{{File: "internal/app/app.go", Line: 12}}
	h.work = func(c durable.WorkerCall) (machine.Result, error) {
		r := wfOK(c)
		switch {
		case c.Step.Name == "review":
			r.Findings = []machine.Finding{f}
		case c.Step.Name == "code" && c.Round > 0:
			r.ChangedLines = fixed
		}
		return r, nil
	}
	isRound2 := func(in any) bool { w := in.(durable.WorkerCall); return w.Step.Name == "review" && w.Round == 2 }
	h.stop = wfFirst(durable.CallWorker, 1, isRound2)
	h.run()

	review := h.workerCalls("review")
	if got := roundsOf(review); !slices.Equal(got, []int{1, 2, 2, 3}) {
		t.Fatalf("review rounds = %v, want [1 2 2 3]: round 2 repeated, never a fourth", got)
	}
	if n := h.stopPairs(); n != 1 {
		t.Fatalf("%d stop pairs, want 1: %v", n, h.kinds())
	}
	hash := wfFindingHash(t, f)
	for _, c := range review[1:] { // round 2, its repeat, round 3
		if len(c.Open) != 1 || c.Open[0].SHA256 != hash || c.Open[0].Rule != "R-1" || !slices.Equal(c.ChangedLines, fixed) {
			t.Fatalf("review round %d (attempt %d) got open %+v, lines %v; want the open finding %s and %v", c.Round, c.Attempt, c.Open, c.ChangedLines, hash, fixed)
		}
	}
	for _, c := range h.checkCalls("review", "NoBlockingFindingOpen") {
		if c.Round >= 2 && (len(c.Open) != 1 || c.Open[0].SHA256 != hash || !slices.Equal(c.ChangedLines, fixed)) {
			t.Fatalf("round %d's check got scope %+v, %v", c.Round, c.Open, c.ChangedLines)
		}
	}
	perRound := map[int]int{}
	for i, e := range h.events() {
		if e.Kind == "FindingRecorded" {
			perRound[e.Round]++
			if r := h.records[i].Finding; r == nil || r.SHA256 != hash {
				t.Fatalf("FindingRecorded of round %d carries %+v, want the finding by hash %s", e.Round, r, hash)
			}
		}
	}
	if perRound[1] != 1 || perRound[2] != 1 || perRound[3] != 1 {
		t.Fatalf("findings recorded per round = %v, want one in each of rounds 1 to 3", perRound)
	}
	if fix := roundsOf(h.workerCalls("code")); !slices.Equal(fix, []int{0, 1, 2}) {
		t.Fatalf("code ran in rounds %v, want [0 1 2]", fix)
	}
	if len(h.eventsOf("RunHandedOver")) != 1 {
		t.Fatalf("no hand-over after the third round: %v", h.kinds())
	}
}

// (e) Tokens, cost and review rounds used before a stop still count.
func TestWorkflowUseBeforeAStopStillCounts(t *testing.T) {
	use := func(u machine.Usage) func(durable.WorkerCall) (machine.Result, error) {
		return func(c durable.WorkerCall) (machine.Result, error) { r := wfOK(c); r.Usage = u; return r, nil }
	}
	for _, tc := range []struct {
		name   string
		usage  machine.Usage
		reason string
	}{
		{"tokens", machine.Usage{Tokens: 900000}, "the run used 3600000 tokens, past the run's token budget of 3000000"},
		{"cost", machine.Usage{CostUSD: 0.15}, "the run cost $0.6000, past the run's cost budget of $0.5000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWF(t, wfPattern())
			h.work = use(tc.usage)
			h.stop = wfFirst(durable.CallWorker, 1, wfWorkerOf("tests"))
			h.run()
			if n := h.stopPairs(); n != 1 {
				t.Fatalf("%d stop pairs, want 1: %v", n, h.kinds())
			}
			ho := h.eventsOf("RunHandedOver")
			if len(ho) != 1 || ho[0].Detail != tc.reason {
				t.Fatalf("hand-overs %+v, want one with %q (3 steps before code counted)", ho, tc.reason)
			}
			if len(h.workerCalls("review")) != 0 {
				t.Fatal("review ran past the budget")
			}
		})
	}
	t.Run("rounds", func(t *testing.T) {
		h := newWF(t, wfPattern())
		h.work = func(c durable.WorkerCall) (machine.Result, error) {
			r := wfOK(c)
			if c.Step.Name == "review" {
				r.Findings = []machine.Finding{wfBlocking("R-1", 12)}
			}
			return r, nil
		}
		isFix := func(in any) bool { w := in.(durable.WorkerCall); return w.Step.Name == "code" && w.Round == 1 }
		h.stop = wfFirst(durable.CallWorker, 1, isFix)
		h.run()
		if got := roundsOf(h.workerCalls("review")); !slices.Equal(got, []int{1, 2, 3}) {
			t.Fatalf("review rounds = %v, want [1 2 3]", got)
		}
		if got := roundsOf(h.workerCalls("code")); !slices.Equal(got, []int{0, 1, 1, 2}) {
			t.Fatalf("code rounds = %v, want [0 1 1 2]: the stopped fix repeated in its round", got)
		}
		if len(h.eventsOf("RunHandedOver")) != 1 {
			t.Fatalf("no hand-over after round 3: %v", h.kinds())
		}
	})
}

// (e) The time budget sums the running times the activities report and ignores timer and signal
// waits: a 3-day wait for workers-ready and the retry sleeps of a failing restore count for
// nothing, and the reported times hand the run over as soon as they pass 30 minutes.
func TestWorkflowTimeBudgetSumsRunningTimes(t *testing.T) {
	h := newWF(t, wfPattern())
	h.took = func(c durable.WorkerCall) time.Duration {
		switch c.Step.Name {
		case "code":
			return 2*time.Minute + 59*time.Second // 29m59s in all: still within 30m
		case "review":
			return 2 * time.Second // 30m1s: past it
		}
		return 9 * time.Minute
	}
	h.work = wfWorkersGoneForCalls("tests", 4) // the first call and the retry rule's 3 retries: the run waits
	failed := 0
	h.restore = func(r machine.RestoreRequest) error {
		if r.Step.Name == "generate" && failed < 2 { // two retries: at least 3 s of durable sleep
			failed++
			return errWFUnreachable
		}
		return nil
	}
	h.signalAt(72*time.Hour, durable.SignalWorkersReady, nil)
	h.run()

	for _, s := range []string{"contract-check", "generate", "tests", "code"} {
		if !slices.ContainsFunc(h.eventsOf("StepAccepted"), func(e machine.Event) bool { return e.Step == s }) {
			t.Fatalf("%s not accepted: a wait counted toward the time budget? %v", s, h.kinds())
		}
	}
	ho := h.eventsOf("RunHandedOver")
	want := "the run took 30m1s, past the run's time budget of 30m0s"
	if len(ho) != 1 || ho[0].Detail != want {
		t.Fatalf("hand-overs %+v, want one with %q", ho, want)
	}
}

// (m) A stop while a check runs runs the check again.
func TestWorkflowStopDuringCheckRunsItAgain(t *testing.T) {
	h := newWF(t, wfPattern())
	h.stop = wfFirst(durable.RunCheck, 1, func(in any) bool {
		c := in.(durable.CheckCall)
		return c.Step.Name == "code" && c.Name == "AllTestsPass"
	})
	h.run()
	h.wantOutcome(machine.OutcomeDone)

	calls := h.checkCalls("code", "AllTestsPass")
	if len(calls) < 2 || calls[0].Attempt != 1 || calls[len(calls)-1].Attempt != 1 {
		t.Fatalf("AllTestsPass ran %d times, want again for attempt 1 after the stop", len(calls))
	}
	if n := h.stopPairs(); n != 1 {
		t.Fatalf("%d stop pairs, want 1: %v", n, h.kinds())
	}
	for _, e := range h.events() {
		if e.Step == "code" && (e.Kind == "StepRejected" || e.Kind == "CheckFailed") {
			t.Fatalf("the stopped check counted as failed: %+v", e)
		}
		if e.Step == "code" && e.Kind == "StepAccepted" && e.Attempt != 1 {
			t.Fatalf("code accepted on attempt %d, want 1", e.Attempt)
		}
	}
}

// (m) A check that cannot run (its call fails) still counts as failed (spec Edge Cases).
func TestWorkflowCheckThatCannotRunCountsAsFailed(t *testing.T) {
	h := newWF(t, wfPattern())
	cannot := wfFirst(durable.RunCheck, 1, func(in any) bool {
		c := in.(durable.CheckCall)
		return c.Step.Name == "code" && c.Name == "AllTestsPass"
	})
	h.fail = func(name string, in any) error {
		if cannot(name, in) {
			return temporal.NewNonRetryableApplicationError("the check service is unreachable", "CheckUnavailable", errors.New("connection refused"))
		}
		return nil
	}
	h.run()
	h.wantOutcome(machine.OutcomeDone)

	var failedCheck *machine.Event
	for _, e := range h.eventsOf("CheckFailed") {
		if e.Step == "code" && e.Attempt == 1 && e.Check != nil && e.Check.Name == "AllTestsPass" {
			failedCheck = &e
		}
	}
	if failedCheck == nil || failedCheck.Check.Passed || failedCheck.Check.Reason == "" {
		t.Fatalf("no CheckFailed with a reason for the check that could not run: %v", h.kinds())
	}
	if got := attemptsOf(h.workerCalls("code")); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("code attempts = %v, want [1 2]: attempt 1 rejected", got)
	}
	if n := h.stopPairs(); n != 0 {
		t.Fatalf("a failing check recorded %d stops", n)
	}
}
