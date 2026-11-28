package trials_test

import (
	"math"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/trials"
)

// Feature 012, T045 (US2, T019 B1-B3): the measures on the record shapes the engine writes, judged
// from spec Clarifications 2026-11-28, research R6 and the contract's TrialMeasures. The engine
// records one StepStarted per step round; each attempt then records AttemptMade (with usage) and
// StepAccepted or StepRejected; a failed worker's attempt records StepRejected with usage and no
// AttemptMade; the next attempt of the round has no new StepStarted. An attempt runs from its
// round's StepStarted, or from the previous attempt's StepRejected in the same round, to its
// StepAccepted or StepRejected. Own helpers (prefix rr), none shared with measures_test.go.

var rr0 = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

type rrRec struct {
	run string
	ev  []machine.Event
}

func rrNew(run string) *rrRec {
	r := &rrRec{run: run}
	return r.at(0, machine.Event{Kind: "RunStarted", Slice: "S-1", Pattern: "base"})
}

func (r *rrRec) at(sec int, e machine.Event) *rrRec {
	e.Seq, e.At, e.Run = len(r.ev)+1, rr0.Add(time.Duration(sec)*time.Second), r.run
	r.ev = append(r.ev, e)
	return r
}

func rrTrials(rs ...*rrRec) []trials.Trial {
	ts := make([]trials.Trial, len(rs))
	for i, r := range rs {
		ts[i] = trials.Trial{Number: i + 1, Run: r.run, Events: r.ev}
	}
	return ts
}

func rrUse(tokens int64, cost float64) *machine.Usage {
	return &machine.Usage{Tokens: tokens, CostUSD: cost}
}

func rrStep(t *testing.T, r trials.Result, name string) trials.StepSpread {
	t.Helper()
	for _, s := range r.Steps {
		if s.Step == name {
			return s
		}
	}
	t.Fatalf("step %q is not in the result %v", name, r.Steps)
	return trials.StepSpread{}
}

func rrWant(t *testing.T, what string, got *trials.Spread, v float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: not available, want %v", what, v)
		return
	}
	for _, g := range []float64{got.Lowest, got.Median, got.Highest} {
		if math.Abs(g-v) > 1e-9 {
			t.Errorf("%s: %v/%v/%v, want %v", what, got.Lowest, got.Median, got.Highest, v)
			return
		}
	}
}

func rrNone(t *testing.T, what string, got *trials.Spread) {
	t.Helper()
	if got != nil {
		t.Errorf("%s: %+v, want not available (nil), never 0", what, *got)
	}
}

var rrPattern = []string{"implement", "review"}

// twoAttempts plants B1's shape from sec: one StepStarted, attempt 1 made and rejected at +10,
// attempt 2 made and accepted at +25.
func twoAttempts(r *rrRec, sec int) *rrRec {
	return r.
		at(sec, machine.Event{Kind: "StepStarted", Step: "implement"}).
		at(sec+5, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 1, Usage: rrUse(10, 0.1)}).
		at(sec+10, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 1}).
		at(sec+20, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 2, Usage: rrUse(10, 0.1)}).
		at(sec+25, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 2, Artifacts: []string{"h"}})
}

// B1: with one StepStarted for two attempts, the step's seconds are both spans (10 + 15), not
// only the first.
func TestB1StepSecondsCountEveryAttemptOfARound(t *testing.T) {
	r := trials.Of(rrPattern, rrTrials(twoAttempts(rrNew("R-1"), 10)))
	rrWant(t, "seconds", rrStep(t, r, "implement").Measures.Seconds, 25)
	rrWant(t, "attempts", rrStep(t, r, "implement").Measures.Attempts, 2)
}

// B1: three attempts under one StepStarted: 10 + 15 + 20.
func TestB1ThreeAttemptsOfOneRound(t *testing.T) {
	a := rrNew("R-1").
		at(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		at(20, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 1, Usage: rrUse(1, 0.01)}).
		at(35, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 2, Usage: rrUse(1, 0.01)}).
		at(40, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 3, Usage: rrUse(1, 0.01)}).
		at(55, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 3})
	r := trials.Of(rrPattern, rrTrials(a))
	rrWant(t, "seconds", rrStep(t, r, "implement").Measures.Seconds, 45)
}

// B1: an interruption of 5 s inside attempt 2 is left out: 25 - 5.
func TestB1InterruptionInsideTheSecondAttemptIsLeftOut(t *testing.T) {
	a := rrNew("R-1").
		at(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		at(15, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 1, Usage: rrUse(10, 0.1)}).
		at(20, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 1}).
		at(25, machine.Event{Kind: "RunInterrupted"}).
		at(30, machine.Event{Kind: "RunResumed"}).
		at(33, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 2, Usage: rrUse(10, 0.1)}).
		at(35, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 2})
	r := trials.Of(rrPattern, rrTrials(a))
	// spans: 10 (start to rejection) + 15 (rejection to acceptance) - 5 interrupted = 20
	rrWant(t, "seconds", rrStep(t, r, "implement").Measures.Seconds, 20)
}

// B1: an interruption inside attempt 1 only, with attempt 2 clean.
func TestB1InterruptionInsideTheFirstAttemptIsLeftOut(t *testing.T) {
	a := rrNew("R-1").
		at(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		at(12, machine.Event{Kind: "RunInterrupted"}).
		at(18, machine.Event{Kind: "RunResumed"}).
		at(24, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 1, Usage: rrUse(1, 0.01)}).
		at(40, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 2, Usage: rrUse(1, 0.01)}).
		at(44, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 2})
	r := trials.Of(rrPattern, rrTrials(a))
	// attempt 1: 14 - 6 = 8; attempt 2: 20
	rrWant(t, "seconds", rrStep(t, r, "implement").Measures.Seconds, 28)
}

// B1: a second review round has its own StepStarted and adds its span to the first round's two
// attempts (25 + 7).
func TestB1ASecondRoundAddsItsOwnSpan(t *testing.T) {
	a := rrNew("R-1").
		at(10, machine.Event{Kind: "StepStarted", Step: "review", Round: 1}).
		at(15, machine.Event{Kind: "AttemptMade", Step: "review", Round: 1, Attempt: 1, Usage: rrUse(1, 0.01)}).
		at(20, machine.Event{Kind: "StepRejected", Step: "review", Round: 1, Attempt: 1}).
		at(30, machine.Event{Kind: "AttemptMade", Step: "review", Round: 1, Attempt: 2, Usage: rrUse(1, 0.01)}).
		at(35, machine.Event{Kind: "StepRejected", Step: "review", Round: 1, Attempt: 2}).
		at(100, machine.Event{Kind: "StepStarted", Step: "review", Round: 2}).
		at(103, machine.Event{Kind: "AttemptMade", Step: "review", Round: 2, Attempt: 1, Usage: rrUse(1, 0.01)}).
		at(107, machine.Event{Kind: "StepAccepted", Step: "review", Round: 2, Attempt: 1})
	r := trials.Of(rrPattern, rrTrials(a))
	rv := rrStep(t, r, "review")
	rrWant(t, "seconds", rv.Measures.Seconds, 32) // 10 + 15 from round 1, 7 from round 2; the 65 s gap is left out
	rrWant(t, "rounds", rv.Measures.ReviewRounds, 2)
	rrWant(t, "attempts", rv.Measures.Attempts, 3)
}

// B1: a step that ends rejected on its last attempt counts that span too.
func TestB1AStepEndingRejectedOnItsLastAttemptCountsThatSpan(t *testing.T) {
	a := rrNew("R-1").
		at(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		at(15, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 1, Usage: rrUse(1, 0.01)}).
		at(20, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 1}).
		at(35, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 2, Usage: rrUse(1, 0.01)}).
		at(40, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 2}).
		at(41, machine.Event{Kind: "RunFailed"})
	r := trials.Of(rrPattern, rrTrials(a))
	rrWant(t, "seconds", rrStep(t, r, "implement").Measures.Seconds, 30)
}

// B1: the spread over trials of different attempt counts uses the sum of each trial's spans.
func TestB1SecondsSpreadOverTrials(t *testing.T) {
	one := rrNew("R-1").
		at(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		at(15, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 1, Usage: rrUse(1, 0.01)}).
		at(20, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 1})
	r := trials.Of(rrPattern, rrTrials(one, twoAttempts(rrNew("R-2"), 10)))
	s := rrStep(t, r, "implement").Measures.Seconds
	if s == nil || s.Lowest != 10 || s.Highest != 25 || math.Abs(s.Median-17.5) > 1e-9 {
		t.Errorf("seconds %+v, want 10/17.5/25", s)
	}
}

// B2: step A's AttemptMade and step B's failed-worker StepRejected (usage, no AttemptMade) share
// round 0 and attempt 1; B's attempt must still count: the trial's attempts are 2, step B's 1,
// and the trial's tokens and cost include B's usage.
func TestB2AFailedWorkersRejectionIsNotHiddenByAnotherStepsAttempt(t *testing.T) {
	a := rrNew("R-1").
		at(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		at(15, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 1, Usage: rrUse(100, 0.5)}).
		at(20, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 1, Artifacts: []string{"h"}}).
		at(21, machine.Event{Kind: "StepStarted", Step: "review"}).
		at(30, machine.Event{Kind: "StepRejected", Step: "review", Attempt: 1, Usage: rrUse(40, 0.2)})
	r := trials.Of(rrPattern, rrTrials(a))
	tot := r.Outcomes[0].Totals
	if tot.Attempts == nil || *tot.Attempts != 2 {
		t.Errorf("trial attempts %v, want 2", tot.Attempts)
	}
	rrWant(t, "step review attempts", rrStep(t, r, "review").Measures.Attempts, 1)
	rrWant(t, "step implement attempts", rrStep(t, r, "implement").Measures.Attempts, 1)
	if tot.Tokens == nil || *tot.Tokens != 140 {
		t.Errorf("trial tokens %v, want 140 (review's 40 included)", tot.Tokens)
	}
	if tot.CostUSD == nil || math.Abs(*tot.CostUSD-0.7) > 1e-9 {
		t.Errorf("trial cost %v, want 0.7", tot.CostUSD)
	}
	rrWant(t, "step review tokens", rrStep(t, r, "review").Measures.Tokens, 40)
}

// B2: the same, with the order of the steps reversed (the failed worker's step first).
func TestB2FailedWorkerFirstAnotherStepsAttemptAfter(t *testing.T) {
	a := rrNew("R-1").
		at(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		at(15, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 1, Usage: rrUse(40, 0.2)}).
		at(20, machine.Event{Kind: "StepStarted", Step: "review"}).
		at(22, machine.Event{Kind: "AttemptMade", Step: "review", Attempt: 1, Usage: rrUse(5, 0.05)}).
		at(25, machine.Event{Kind: "StepAccepted", Step: "review", Attempt: 1})
	r := trials.Of(rrPattern, rrTrials(a))
	if tot := r.Outcomes[0].Totals; tot.Attempts == nil || *tot.Attempts != 2 {
		t.Errorf("trial attempts %v, want 2", tot.Attempts)
	}
	rrWant(t, "step implement attempts", rrStep(t, r, "implement").Measures.Attempts, 1)
}

// B2: in a second round, another step's AttemptMade at that round and attempt does not hide a
// failed worker's StepRejected either.
func TestB2SecondRoundSameAttemptNumberInTwoSteps(t *testing.T) {
	a := rrNew("R-1").
		at(10, machine.Event{Kind: "StepStarted", Step: "implement", Round: 2}).
		at(12, machine.Event{Kind: "AttemptMade", Step: "implement", Round: 2, Attempt: 1, Usage: rrUse(1, 0.01)}).
		at(14, machine.Event{Kind: "StepAccepted", Step: "implement", Round: 2, Attempt: 1}).
		at(15, machine.Event{Kind: "StepStarted", Step: "review", Round: 2}).
		at(18, machine.Event{Kind: "StepRejected", Step: "review", Round: 2, Attempt: 1, Usage: rrUse(2, 0.02)})
	r := trials.Of(rrPattern, rrTrials(a))
	rrWant(t, "step review attempts", rrStep(t, r, "review").Measures.Attempts, 1)
}

// B3: a procedure step's events carry no usage, across 3 trials: tokens and cost are not
// available (nil, never 0), attempts and seconds are present.
func TestB3AProcedureStepShowsNoTokensAndNoCost(t *testing.T) {
	proc := func(run string, extra int) *rrRec {
		r := rrNew(run).at(10, machine.Event{Kind: "StepStarted", Step: "implement"})
		for n := 1; n <= 1+extra; n++ {
			r.at(10+n*4, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: n, Worker: "procedure:generate"})
			if n <= extra {
				r.at(11+n*4, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: n})
			}
		}
		return r.at(12+(1+extra)*4, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 1 + extra})
	}
	r := trials.Of(rrPattern, rrTrials(proc("R-1", 0), proc("R-2", 0), proc("R-3", 1)))
	m := rrStep(t, r, "implement").Measures
	rrNone(t, "step tokens", m.Tokens)
	rrNone(t, "step cost", m.CostUSD)
	if m.Attempts == nil || m.Attempts.Lowest != 1 || m.Attempts.Highest != 2 {
		t.Errorf("step attempts %+v, want 1/1/2", m.Attempts)
	}
	if m.Seconds == nil {
		t.Errorf("step seconds not available, want present")
	}
	rrNone(t, "totals tokens", r.Totals.Tokens)
	rrNone(t, "totals cost", r.Totals.CostUSD)
	if r.Totals.Attempts == nil || r.Totals.Seconds == nil {
		t.Errorf("totals attempts %v seconds %v, want both present", r.Totals.Attempts, r.Totals.Seconds)
	}
	for i, o := range r.Outcomes {
		if o.Totals.Tokens != nil || o.Totals.CostUSD != nil {
			t.Errorf("trial %d totals tokens %v cost %v, want not available", i+1, o.Totals.Tokens, o.Totals.CostUSD)
		}
	}
}

// B3 edge: a usage of zero on an agent step is a measure of 0, not an absence.
func TestB3ZeroUsageOnAnAgentStepStillCountsAsZero(t *testing.T) {
	a := rrNew("R-1").
		at(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		at(12, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 1, Usage: rrUse(0, 0)}).
		at(14, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 1})
	r := trials.Of(rrPattern, rrTrials(a))
	rrWant(t, "tokens", rrStep(t, r, "implement").Measures.Tokens, 0)
	rrWant(t, "cost", rrStep(t, r, "implement").Measures.CostUSD, 0)
}

// B3 edge: a procedure step and an agent step in one trial: the step's own row has none, the
// trial's totals are the agent step's alone.
func TestB3MixedProcedureAndAgentStepsTotalsAreTheAgentsAlone(t *testing.T) {
	a := rrNew("R-1").
		at(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		at(12, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 1}).
		at(14, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 1}).
		at(15, machine.Event{Kind: "StepStarted", Step: "review"}).
		at(17, machine.Event{Kind: "AttemptMade", Step: "review", Attempt: 1, Usage: rrUse(30, 0.3)}).
		at(19, machine.Event{Kind: "StepAccepted", Step: "review", Attempt: 1})
	r := trials.Of(rrPattern, rrTrials(a))
	rrNone(t, "implement tokens", rrStep(t, r, "implement").Measures.Tokens)
	rrWant(t, "review tokens", rrStep(t, r, "review").Measures.Tokens, 30)
	rrWant(t, "totals tokens", r.Totals.Tokens, 30)
}

// A1: a procedure step whose worker fails on attempt 1 (StepRejected with no usage and no
// AttemptMade) and passes on attempt 2 shows attempts 2, at the step and in the trial's total,
// and tokens and cost not available. An attempt is an AttemptMade, or a StepRejected with no
// AttemptMade of the same step, round and attempt, usage or not.
func TestA1AProcedureWorkerFailureWithoutUsageStillCountsAsAnAttempt(t *testing.T) {
	a := rrNew("R-1").
		at(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		at(14, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 1, Detail: "the worker failed: boom"}).
		at(20, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 2, Worker: "procedure:generate"}).
		at(24, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 2})
	r := trials.Of(rrPattern, rrTrials(a))
	m := rrStep(t, r, "implement").Measures
	rrWant(t, "step attempts", m.Attempts, 2)
	rrNone(t, "step tokens", m.Tokens)
	rrNone(t, "step cost", m.CostUSD)
	rrWant(t, "step seconds", m.Seconds, 14)
	tot := r.Outcomes[0].Totals
	if tot.Attempts == nil || *tot.Attempts != 2 {
		t.Errorf("trial attempts %v, want 2", tot.Attempts)
	}
	if tot.Tokens != nil || tot.CostUSD != nil {
		t.Errorf("trial tokens %v cost %v, want not available", tot.Tokens, tot.CostUSD)
	}
	rrWant(t, "totals attempts", r.Totals.Attempts, 2)
}

// A1: a procedure worker that fails on every attempt, with no usage anywhere: attempts 2.
func TestA1TwoFailedProcedureAttemptsWithNoUsageAreTwoAttempts(t *testing.T) {
	a := rrNew("R-1").
		at(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		at(12, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 1}).
		at(15, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 2})
	r := trials.Of(rrPattern, rrTrials(a))
	rrWant(t, "step attempts", rrStep(t, r, "implement").Measures.Attempts, 2)
}

// A1: a check-failure StepRejected follows its attempt's AttemptMade (same step, round and
// attempt): the attempt is counted once, with or without usage on the StepRejected.
func TestA1ACheckFailureRejectionAfterItsAttemptMadeIsNotCountedTwice(t *testing.T) {
	for name, u := range map[string]*machine.Usage{"no usage": nil, "usage": rrUse(5, 0.05)} {
		a := rrNew("R-1").
			at(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
			at(12, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 1, Usage: u}).
			at(14, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 1, Usage: u, Detail: "a check failed"}).
			at(20, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 2, Usage: u}).
			at(22, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 2})
		r := trials.Of(rrPattern, rrTrials(a))
		rrWant(t, name+": step attempts", rrStep(t, r, "implement").Measures.Attempts, 2)
		if tot := r.Outcomes[0].Totals; tot.Attempts == nil || *tot.Attempts != 2 {
			t.Errorf("%s: trial attempts %v, want 2", name, tot.Attempts)
		}
	}
}
