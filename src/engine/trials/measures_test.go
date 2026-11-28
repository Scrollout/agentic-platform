package trials_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/trials"
)

// Feature 012 user story 2 (T015): what the trials' effort spreads to, per step and per trial,
// and which steps' files differ. Every case plants run records and goes through trials.Of.

var mt0 = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

// rec plants one run record; add appends an event `sec` seconds after RunStarted.
type rec struct {
	run string
	ev  []machine.Event
}

func newRec(run string) *rec {
	r := &rec{run: run}
	return r.add(0, machine.Event{Kind: "RunStarted", Slice: "S-1", Pattern: "base"})
}

func (r *rec) add(sec int, e machine.Event) *rec {
	e.Seq, e.At, e.Run = len(r.ev)+1, mt0.Add(time.Duration(sec)*time.Second), r.run
	r.ev = append(r.ev, e)
	return r
}

func (r *rec) trial(n int) trials.Trial { return trials.Trial{Number: n, Run: r.run, Events: r.ev} }

func use(tokens int64, cost float64) *machine.Usage {
	return &machine.Usage{Tokens: tokens, CostUSD: cost}
}

// attempts plants n attempts of step: each StepStarted, AttemptMade (with usage), then a
// StepRejected for all but the last and a StepAccepted for the last; 10 s per attempt span.
func (r *rec) attempts(step string, start, n int, u *machine.Usage, out ...string) *rec {
	for i := 1; i <= n; i++ {
		s := start + (i-1)*10
		r.add(s, machine.Event{Kind: "StepStarted", Step: step})
		r.add(s+5, machine.Event{Kind: "AttemptMade", Step: step, Attempt: i, Usage: u})
		if i < n {
			r.add(s+10, machine.Event{Kind: "StepRejected", Step: step, Attempt: i})
		} else {
			r.add(s+10, machine.Event{Kind: "StepAccepted", Step: step, Attempt: i, Artifacts: out})
		}
	}
	return r
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func wantSpread(t *testing.T, what string, got *trials.Spread, lo, med, hi float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: not available, want %v/%v/%v", what, lo, med, hi)
		return
	}
	if !near(got.Lowest, lo) || !near(got.Median, med) || !near(got.Highest, hi) {
		t.Errorf("%s: %v/%v/%v, want %v/%v/%v", what, got.Lowest, got.Median, got.Highest, lo, med, hi)
	}
}

func wantNone(t *testing.T, what string, got *trials.Spread) {
	t.Helper()
	if got != nil {
		t.Errorf("%s: %+v, want not available (nil), never 0", what, *got)
	}
}

func step(t *testing.T, r trials.Result, name string) trials.StepSpread {
	t.Helper()
	for _, s := range r.Steps {
		if s.Step == name {
			return s
		}
	}
	t.Fatalf("step %q is not in the result %v", name, r.Steps)
	return trials.StepSpread{}
}

func numbered(rs ...*rec) []trials.Trial {
	ts := make([]trials.Trial, len(rs))
	for i, r := range rs {
		ts[i] = r.trial(i + 1)
	}
	return ts
}

func TestStepsListEveryPatternStepInOrder(t *testing.T) {
	for name, ts := range map[string][]trials.Trial{
		"no trials":        nil,
		"nothing recorded": {{Number: 1, Run: "R-1"}},
	} {
		r := trials.Of(pattern, ts)
		if len(r.Steps) != 2 || r.Steps[0].Step != "implement" || r.Steps[1].Step != "review" {
			t.Fatalf("%s: steps %+v, want implement then review", name, r.Steps)
		}
		for _, s := range r.Steps {
			if s.Reached != 0 || s.Files != nil || s.Measures != (trials.Spreads{}) {
				t.Errorf("%s: step %s %+v, want reached 0, no measures, no files", name, s.Step, s)
			}
		}
		if r.Totals != (trials.Spreads{}) {
			t.Errorf("%s: totals %+v, want none", name, r.Totals)
		}
	}
}

func TestStepNoTrialReachedIsListedWithReachedZero(t *testing.T) {
	a := newRec("R-1").attempts("implement", 10, 1, use(100, 0.5), "h1")
	b := newRec("R-2").attempts("implement", 10, 1, use(100, 0.5), "h1")
	r := trials.Of(pattern, numbered(a, b))
	rv := step(t, r, "review")
	if rv.Reached != 0 || rv.Files != nil || rv.Measures != (trials.Spreads{}) {
		t.Errorf("review %+v, want reached 0, empty measures, no files", rv)
	}
	if im := step(t, r, "implement"); im.Reached != 2 {
		t.Errorf("implement reached %d, want 2", im.Reached)
	}
	if len(r.Steps) != 2 || r.Steps[0].Step != "implement" {
		t.Errorf("steps %+v, want pattern order", r.Steps)
	}
}

func TestReachedCountsTrialsWhoseRecordHasTheStep(t *testing.T) {
	a := newRec("R-1").attempts("implement", 10, 1, use(1, 0.1), "h").attempts("review", 30, 1, use(1, 0.1), "r")
	b := newRec("R-2").add(10, machine.Event{Kind: "StepStarted", Step: "implement"}) // started, never accepted
	c := newRec("R-3")                                                                // nothing but RunStarted
	r := trials.Of(pattern, numbered(a, b, c))
	if got := step(t, r, "implement").Reached; got != 2 {
		t.Errorf("implement reached %d, want 2", got)
	}
	if got := step(t, r, "review").Reached; got != 1 {
		t.Errorf("review reached %d, want 1", got)
	}
}

func TestAttemptsOddCountMedian(t *testing.T) {
	// A code step needing 1, 1 and 2 attempts shows 1/1/2 (quickstart 1).
	r := trials.Of(pattern, numbered(
		newRec("R-1").attempts("implement", 10, 1, use(10, 0.1), "h"),
		newRec("R-2").attempts("implement", 10, 1, use(10, 0.1), "h"),
		newRec("R-3").attempts("implement", 10, 2, use(10, 0.1), "h2"),
	))
	wantSpread(t, "attempts", step(t, r, "implement").Measures.Attempts, 1, 1, 2)
}

func TestAttemptsEvenCountMedianIsTheMeanOfTheTwoMiddle(t *testing.T) {
	r := trials.Of(pattern, numbered(
		newRec("R-1").attempts("implement", 10, 1, use(10, 0.1), "h"),
		newRec("R-2").attempts("implement", 10, 2, use(10, 0.1), "h"),
		newRec("R-3").attempts("implement", 10, 3, use(10, 0.1), "h"),
		newRec("R-4").attempts("implement", 10, 4, use(10, 0.1), "h"),
	))
	wantSpread(t, "attempts", step(t, r, "implement").Measures.Attempts, 1, 2.5, 4)
}

func TestMedianIsOfSortedValuesNotOfTrialOrder(t *testing.T) {
	r := trials.Of(pattern, numbered(
		newRec("R-1").attempts("implement", 10, 5, use(1, 0.1), "h"),
		newRec("R-2").attempts("implement", 10, 1, use(1, 0.1), "h"),
		newRec("R-3").attempts("implement", 10, 3, use(1, 0.1), "h"),
	))
	wantSpread(t, "attempts", step(t, r, "implement").Measures.Attempts, 1, 3, 5)
}

func TestSingleTrialSpreadIsThatValueThrice(t *testing.T) {
	r := trials.Of(pattern, numbered(newRec("R-1").attempts("implement", 10, 2, use(7, 0.25), "h")))
	m := step(t, r, "implement").Measures
	wantSpread(t, "attempts", m.Attempts, 2, 2, 2)
	wantSpread(t, "tokens", m.Tokens, 14, 14, 14)
}

func TestAttemptsStepRejectedWithUsageAndNoAttemptMadeCounts(t *testing.T) {
	// Attempt 1 ends as a StepRejected carrying usage with no AttemptMade (the worker failed after
	// spending); attempt 2 is a normal AttemptMade, accepted: two attempts.
	a := newRec("R-1").
		add(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		add(15, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 1, Usage: use(30, 0.3)}).
		add(20, machine.Event{Kind: "StepStarted", Step: "implement"}).
		add(25, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 2, Usage: use(50, 0.5)}).
		add(30, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 2, Artifacts: []string{"h"}})
	r := trials.Of(pattern, numbered(a))
	m := step(t, r, "implement").Measures
	wantSpread(t, "attempts", m.Attempts, 2, 2, 2)
	wantSpread(t, "tokens", m.Tokens, 80, 80, 80)
	wantSpread(t, "cost", m.CostUSD, 0.8, 0.8, 0.8)
}

func TestAttemptsStepRejectedAfterAnAttemptMadeIsNotCountedTwice(t *testing.T) {
	// The StepRejected of attempt 1 carries usage too, but the attempt has its AttemptMade.
	a := newRec("R-1").
		add(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		add(15, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 1, Usage: use(30, 0.3)}).
		add(20, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 1, Usage: use(30, 0.3)}).
		add(30, machine.Event{Kind: "StepStarted", Step: "implement"}).
		add(35, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 2, Usage: use(50, 0.5)}).
		add(40, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 2})
	r := trials.Of(pattern, numbered(a))
	wantSpread(t, "attempts", step(t, r, "implement").Measures.Attempts, 2, 2, 2)
}

func TestAttemptsStepRejectedWithoutUsageAndWithoutAttemptMadeIsAnAttempt(t *testing.T) {
	a := newRec("R-1").
		add(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		add(15, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 1}).
		add(20, machine.Event{Kind: "StepStarted", Step: "implement"}).
		add(25, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 2, Usage: use(5, 0.05)}).
		add(30, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 2})
	r := trials.Of(pattern, numbered(a))
	wantSpread(t, "attempts", step(t, r, "implement").Measures.Attempts, 2, 2, 2)
}

func TestReviewRoundsIsTheHighestRound(t *testing.T) {
	round := func(r *rec, rd int, at int) {
		r.add(at, machine.Event{Kind: "StepStarted", Step: "review", Round: rd}).
			add(at+2, machine.Event{Kind: "AttemptMade", Step: "review", Round: rd, Attempt: 1, Usage: use(1, 0.01)})
	}
	a := newRec("R-1")
	round(a, 1, 10)
	a.add(20, machine.Event{Kind: "StepRejected", Step: "review", Round: 1})
	round(a, 2, 30)
	a.add(40, machine.Event{Kind: "StepRejected", Step: "review", Round: 2})
	round(a, 3, 50)
	a.add(60, machine.Event{Kind: "StepAccepted", Step: "review", Round: 3})
	b := newRec("R-2")
	round(b, 1, 10)
	b.add(20, machine.Event{Kind: "StepAccepted", Step: "review", Round: 1})
	r := trials.Of(pattern, numbered(a, b))
	wantSpread(t, "rounds", step(t, r, "review").Measures.ReviewRounds, 1, 2, 3)
}

func TestReviewRoundsNotAvailableWhenNoEventCarriesARound(t *testing.T) {
	r := trials.Of(pattern, numbered(newRec("R-1").attempts("implement", 10, 1, use(1, 0.1), "h")))
	wantNone(t, "rounds", step(t, r, "implement").Measures.ReviewRounds)
}

func TestSecondsAreTheSumOfTheAttemptSpans(t *testing.T) {
	// 10 s per attempt span: 1, 2 and 3 attempts give 10, 20 and 30 seconds.
	r := trials.Of(pattern, numbered(
		newRec("R-1").attempts("implement", 5, 1, use(1, 0.1), "h"),
		newRec("R-2").attempts("implement", 5, 2, use(1, 0.1), "h"),
		newRec("R-3").attempts("implement", 5, 3, use(1, 0.1), "h"),
	))
	wantSpread(t, "seconds", step(t, r, "implement").Measures.Seconds, 10, 20, 30)
}

func TestSecondsLeaveOutTimeBetweenAttemptSpans(t *testing.T) {
	// Spans 10 s and 20 s with a 100 s gap between them: 30, not 130.
	a := newRec("R-1").
		add(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		add(20, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 1, Usage: use(1, 0.1)}).
		add(120, machine.Event{Kind: "StepStarted", Step: "implement"}).
		add(140, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 2})
	r := trials.Of(pattern, numbered(a))
	wantSpread(t, "seconds", step(t, r, "implement").Measures.Seconds, 30, 30, 30)
}

func TestSecondsLeaveOutInterruptedTime(t *testing.T) {
	// Started at 10, interrupted 20 to 80, accepted at 100: 90 s span less 60 s interrupted = 30.
	a := newRec("R-1").
		add(10, machine.Event{Kind: "StepStarted", Step: "implement"}).
		add(15, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 1, Usage: use(1, 0.1)}).
		add(20, machine.Event{Kind: "RunInterrupted"}).
		add(80, machine.Event{Kind: "RunResumed"}).
		add(100, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 1})
	r := trials.Of(pattern, numbered(a))
	wantSpread(t, "seconds", step(t, r, "implement").Measures.Seconds, 30, 30, 30)
}

func TestSecondsLeaveOutTwoInterruptions(t *testing.T) {
	a := newRec("R-1").
		add(0, machine.Event{Kind: "StepStarted", Step: "implement"}).
		add(10, machine.Event{Kind: "RunInterrupted"}).
		add(50, machine.Event{Kind: "RunResumed"}).
		add(60, machine.Event{Kind: "RunInterrupted"}).
		add(70, machine.Event{Kind: "RunResumed"}).
		add(80, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 1, Usage: use(1, 0.1)}).
		add(90, machine.Event{Kind: "StepAccepted", Step: "implement", Attempt: 1})
	r := trials.Of(pattern, numbered(a))
	// 90 s less 40 s and 10 s.
	wantSpread(t, "seconds", step(t, r, "implement").Measures.Seconds, 40, 40, 40)
}

func TestSecondsEvenCountMedianIsAFraction(t *testing.T) {
	r := trials.Of(pattern, numbered(
		newRec("R-1").attempts("implement", 5, 1, use(1, 0.1), "h"),
		newRec("R-2").attempts("implement", 5, 2, use(1, 0.1), "h"),
	))
	wantSpread(t, "seconds", step(t, r, "implement").Measures.Seconds, 10, 15, 20)
}

func TestTokensAndCostAreSumsOfUsage(t *testing.T) {
	r := trials.Of(pattern, numbered(
		newRec("R-1").attempts("implement", 5, 2, use(100, 0.25), "h"), // 200, 0.50
		newRec("R-2").attempts("implement", 5, 1, use(300, 0.10), "h"), // 300, 0.10
		newRec("R-3").attempts("implement", 5, 1, use(50, 1.00), "h"),  // 50, 1.00
	))
	m := step(t, r, "implement").Measures
	wantSpread(t, "tokens", m.Tokens, 50, 200, 300)
	wantSpread(t, "cost", m.CostUSD, 0.10, 0.50, 1.00)
}

func TestTokensEvenCountMedian(t *testing.T) {
	r := trials.Of(pattern, numbered(
		newRec("R-1").attempts("implement", 5, 1, use(100, 0.1), "h"),
		newRec("R-2").attempts("implement", 5, 1, use(201, 0.2), "h"),
	))
	wantSpread(t, "tokens", step(t, r, "implement").Measures.Tokens, 100, 150.5, 201)
}

func TestZeroUsageIsAMeasureOfZeroNotUnavailable(t *testing.T) {
	// A worker that reports usage of zero did report it: the spread is 0/0/0, not "not available".
	r := trials.Of(pattern, numbered(newRec("R-1").attempts("implement", 5, 1, use(0, 0), "h")))
	m := step(t, r, "implement").Measures
	wantSpread(t, "tokens", m.Tokens, 0, 0, 0)
	wantSpread(t, "cost", m.CostUSD, 0, 0, 0)
}

func TestFixedProcedureHasNoTokensOrCostNeverZero(t *testing.T) {
	// A fixed procedure records no usage: tokens and cost are not available, the other measures stay.
	mk := func(run string, n int) *rec { return newRec(run).attempts("implement", 5, n, nil, "h") }
	r := trials.Of(pattern, numbered(mk("R-1", 1), mk("R-2", 2), mk("R-3", 1)))
	m := step(t, r, "implement").Measures
	wantNone(t, "tokens", m.Tokens)
	wantNone(t, "cost", m.CostUSD)
	wantSpread(t, "attempts", m.Attempts, 1, 1, 2)
	wantSpread(t, "seconds", m.Seconds, 10, 10, 20)
	// Per trial and over the set alike.
	for _, o := range r.Outcomes {
		if o.Totals.Tokens != nil || o.Totals.CostUSD != nil {
			t.Errorf("trial %d totals tokens %v cost %v, want not available", o.Trial, o.Totals.Tokens, o.Totals.CostUSD)
		}
	}
	wantNone(t, "totals tokens", r.Totals.Tokens)
	wantNone(t, "totals cost", r.Totals.CostUSD)
}

func TestSpreadIsOverTheTrialsThatCarryTheMeasure(t *testing.T) {
	// Trial 2 reached the step with a fixed result (no usage): the spread of tokens is over trial 1 and 3.
	r := trials.Of(pattern, numbered(
		newRec("R-1").attempts("implement", 5, 1, use(100, 1), "h"),
		newRec("R-2").attempts("implement", 5, 1, nil, "h"),
		newRec("R-3").attempts("implement", 5, 1, use(300, 3), "h"),
	))
	m := step(t, r, "implement").Measures
	wantSpread(t, "tokens", m.Tokens, 100, 200, 300)
	wantSpread(t, "cost", m.CostUSD, 1, 2, 3)
	wantSpread(t, "attempts", m.Attempts, 1, 1, 1)
}

func TestSpreadIsOnlyOverTheTrialsThatReachedTheStep(t *testing.T) {
	// Trial 3 never reached review; it is no value of review's spread.
	two := func(run string, rv int) *rec {
		return newRec(run).attempts("implement", 5, 1, use(1, 0.1), "h").attempts("review", 30, rv, use(10, 1), "r")
	}
	r := trials.Of(pattern, numbered(two("R-1", 2), two("R-2", 4), newRec("R-3").attempts("implement", 5, 1, use(1, 0.1), "h")))
	rv := step(t, r, "review")
	if rv.Reached != 2 {
		t.Fatalf("review reached %d, want 2", rv.Reached)
	}
	wantSpread(t, "attempts", rv.Measures.Attempts, 2, 3, 4)
}

func TestPerTrialTotalsSumOverTheSteps(t *testing.T) {
	a := newRec("R-1").
		attempts("implement", 10, 2, use(100, 0.5), "h"). // 2 attempts, 200 tokens, 1.0
		attempts("review", 40, 1, use(40, 0.2), "r")      // 1 attempt, 40 tokens, 0.2
	a.add(60, machine.Event{Kind: "RunCompleted"})
	b := newRec("R-2").attempts("implement", 10, 1, use(10, 0.1), "h")
	b.add(30, machine.Event{Kind: "RunCompleted"})
	r := trials.Of(pattern, numbered(a, b))
	o := r.Outcomes[0].Totals
	if o.Attempts == nil || *o.Attempts != 3 {
		t.Errorf("trial 1 attempts %v, want 3", o.Attempts)
	}
	if o.Tokens == nil || *o.Tokens != 240 {
		t.Errorf("trial 1 tokens %v, want 240", o.Tokens)
	}
	if o.CostUSD == nil || !near(*o.CostUSD, 1.2) {
		t.Errorf("trial 1 cost %v, want 1.2", o.CostUSD)
	}
	if o.Seconds == nil || !near(*o.Seconds, 60) {
		t.Errorf("trial 1 seconds %v, want 60 (RunStarted to its end)", o.Seconds)
	}
	o2 := r.Outcomes[1].Totals
	if o2.Attempts == nil || *o2.Attempts != 1 || o2.Seconds == nil || !near(*o2.Seconds, 30) {
		t.Errorf("trial 2 totals %+v, want attempts 1 seconds 30", o2)
	}
	// The totals row is the spread of the trials' totals.
	wantSpread(t, "totals attempts", r.Totals.Attempts, 1, 2, 3)
	wantSpread(t, "totals tokens", r.Totals.Tokens, 10, 125, 240)
	wantSpread(t, "totals cost", r.Totals.CostUSD, 0.1, 0.65, 1.2)
	wantSpread(t, "totals seconds", r.Totals.Seconds, 30, 45, 60)
}

func TestPerTrialTotalReviewRoundsIsTheHighestRoundOverTheSteps(t *testing.T) {
	a := newRec("R-1").
		add(5, machine.Event{Kind: "StepStarted", Step: "implement", Round: 2}).
		add(6, machine.Event{Kind: "AttemptMade", Step: "implement", Round: 2, Attempt: 1, Usage: use(1, 0.1)}).
		add(7, machine.Event{Kind: "StepAccepted", Step: "implement", Round: 2}).
		add(8, machine.Event{Kind: "StepStarted", Step: "review", Round: 3}).
		add(9, machine.Event{Kind: "AttemptMade", Step: "review", Round: 3, Attempt: 1, Usage: use(1, 0.1)}).
		add(10, machine.Event{Kind: "StepAccepted", Step: "review", Round: 3})
	r := trials.Of(pattern, numbered(a))
	if g := r.Outcomes[0].Totals.ReviewRounds; g == nil || *g != 3 {
		t.Errorf("trial rounds %v, want 3 (the highest round, not the sum 2 + 3)", g)
	}
}

func TestTotalSecondsLeaveOutInterruptedTime(t *testing.T) {
	a := newRec("R-1").
		add(10, machine.Event{Kind: "RunInterrupted"}).
		add(70, machine.Event{Kind: "RunResumed"}).
		add(100, machine.Event{Kind: "RunCompleted"})
	r := trials.Of(pattern, numbered(a))
	if g := r.Outcomes[0].Totals.Seconds; g == nil || !near(*g, 40) {
		t.Errorf("trial seconds %v, want 40 (100 less 60 interrupted)", g)
	}
}

func TestATrialStillRunningCountsToItsLastEvent(t *testing.T) {
	a := newRec("R-1").attempts("implement", 5, 1, use(100, 0.5), "h").
		add(45, machine.Event{Kind: "StepStarted", Step: "review"})
	b := newRec("R-2").attempts("implement", 5, 1, use(100, 0.5), "h")
	b.add(20, machine.Event{Kind: "RunCompleted"})
	r := trials.Of(pattern, numbered(a, b))
	if r.Outcomes[0].Outcome != "" {
		t.Fatalf("trial 1 outcome %q, want none while it runs", r.Outcomes[0].Outcome)
	}
	if g := r.Outcomes[0].Totals.Seconds; g == nil || !near(*g, 45) {
		t.Errorf("running trial seconds %v, want 45 (RunStarted to its last event)", g)
	}
	if g := r.Outcomes[0].Totals.Tokens; g == nil || *g != 100 {
		t.Errorf("running trial tokens %v, want 100", g)
	}
	// It is reached, and counts in the spread of the step it finished.
	im := step(t, r, "implement")
	if im.Reached != 2 {
		t.Errorf("implement reached %d, want 2", im.Reached)
	}
	wantSpread(t, "implement attempts", im.Measures.Attempts, 1, 1, 1)
	if rv := step(t, r, "review"); rv.Reached != 1 || rv.Files != nil {
		t.Errorf("review %+v, want reached 1 and no files (not accepted yet)", rv)
	}
	wantSpread(t, "totals seconds", r.Totals.Seconds, 20, 32.5, 45)
}

func TestATrialWithNothingRecordedHasNoTotals(t *testing.T) {
	done := newRec("R-2").attempts("implement", 5, 1, use(9, 0.9), "h").add(30, machine.Event{Kind: "RunCompleted"})
	r := trials.Of(pattern, []trials.Trial{{Number: 1, Run: "R-1"}, done.trial(2)})
	if o := r.Outcomes[0].Totals; o != (trials.Measures{}) {
		t.Errorf("empty trial totals %+v, want none", o)
	}
	// The set's totals are over the trial that has them.
	wantSpread(t, "totals tokens", r.Totals.Tokens, 9, 9, 9)
	wantSpread(t, "totals attempts", r.Totals.Attempts, 1, 1, 1)
}

func TestNoTrialCarriesAMeasureLeavesItNilEverywhere(t *testing.T) {
	a := newRec("R-1").add(5, machine.Event{Kind: "StepStarted", Step: "implement"}).
		add(9, machine.Event{Kind: "StepAccepted", Step: "implement", Artifacts: []string{"h"}})
	r := trials.Of(pattern, numbered(a))
	m := step(t, r, "implement").Measures
	wantNone(t, "tokens", m.Tokens)
	wantNone(t, "cost", m.CostUSD)
	wantNone(t, "rounds", m.ReviewRounds)
	o := r.Outcomes[0].Totals
	if o.Tokens != nil || o.CostUSD != nil || o.ReviewRounds != nil {
		t.Errorf("trial totals %+v, want tokens, cost and rounds not available", o)
	}
}

func accepted(run string, kind string, out ...string) *rec {
	return newRec(run).
		add(5, machine.Event{Kind: "StepStarted", Step: "implement"}).
		add(8, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 1, Usage: use(1, 0.1)}).
		add(9, machine.Event{Kind: kind, Step: "implement", Attempt: 1, Artifacts: out})
}

func groupsEqual(a, b [][]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if fmt.Sprint(a[i]) != fmt.Sprint(b[i]) {
			return false
		}
	}
	return true
}

func TestFilesSameWhenEveryAcceptedTrialHasTheSameHashes(t *testing.T) {
	r := trials.Of(pattern, numbered(
		accepted("R-1", "StepAccepted", "h1", "h2"),
		accepted("R-2", "StepAccepted", "h1", "h2"),
		accepted("R-3", "StepAccepted", "h1", "h2"),
	))
	f := step(t, r, "implement").Files
	if f == nil || !f.Same {
		t.Errorf("files %+v, want same", f)
	}
}

func TestFilesDifferNamesGroupsInOrderOfTheirFirstTrial(t *testing.T) {
	// 1: A, 2: B, 3: A, 4: C, 5: B  ->  [1 3] [2 5] [4]
	r := trials.Of(pattern, numbered(
		accepted("R-1", "StepAccepted", "A"),
		accepted("R-2", "StepAccepted", "B"),
		accepted("R-3", "StepAccepted", "A"),
		accepted("R-4", "StepAccepted", "C"),
		accepted("R-5", "StepAccepted", "B"),
	))
	f := step(t, r, "implement").Files
	if f == nil || f.Same {
		t.Fatalf("files %+v, want differing", f)
	}
	if want := [][]int{{1, 3}, {2, 5}, {4}}; !groupsEqual(f.Groups, want) {
		t.Errorf("groups %v, want %v", f.Groups, want)
	}
}

func TestFilesGroupsNameTrialNumbersNotPositions(t *testing.T) {
	ts := []trials.Trial{
		accepted("R-7", "StepAccepted", "B").trial(2),
		accepted("R-8", "StepAccepted", "A").trial(4),
		accepted("R-9", "StepAccepted", "B").trial(5),
	}
	f := step(t, trials.Of(pattern, ts), "implement").Files
	if f == nil || f.Same {
		t.Fatalf("files %+v, want differing", f)
	}
	if want := [][]int{{2, 5}, {4}}; !groupsEqual(f.Groups, want) {
		t.Errorf("groups %v, want %v", f.Groups, want)
	}
}

func TestFilesThirdTrialDiffers(t *testing.T) {
	// Quickstart 1: the files differ in the third trial.
	r := trials.Of(pattern, numbered(
		accepted("R-1", "StepAccepted", "h"),
		accepted("R-2", "StepAccepted", "h"),
		accepted("R-3", "StepAccepted", "other"),
	))
	f := step(t, r, "implement").Files
	if f == nil || f.Same {
		t.Fatalf("files %+v, want differing", f)
	}
	if want := [][]int{{1, 2}, {3}}; !groupsEqual(f.Groups, want) {
		t.Errorf("groups %v, want %v", f.Groups, want)
	}
}

func TestFilesAreOfTheLastAcceptance(t *testing.T) {
	// Trial 1 accepted "old" then, after a fix round, "h"; trial 2 accepted "h": same.
	a := accepted("R-1", "StepAccepted", "old").
		add(20, machine.Event{Kind: "StepAccepted", Step: "implement", Round: 2, Artifacts: []string{"h"}})
	r := trials.Of(pattern, numbered(a, accepted("R-2", "StepAccepted", "h")))
	if f := step(t, r, "implement").Files; f == nil || !f.Same {
		t.Errorf("files %+v, want same (last acceptance counts)", f)
	}
}

func TestFilesResultReusedCounts(t *testing.T) {
	r := trials.Of(pattern, numbered(
		accepted("R-1", "StepAccepted", "h"),
		accepted("R-2", "ResultReused", "h"),
	))
	if f := step(t, r, "implement").Files; f == nil || !f.Same {
		t.Errorf("files %+v, want same (a reused result has the hashes)", f)
	}
	r = trials.Of(pattern, numbered(
		accepted("R-1", "StepAccepted", "h"),
		accepted("R-2", "ResultReused", "x"),
	))
	f := step(t, r, "implement").Files
	if f == nil || f.Same || !groupsEqual(f.Groups, [][]int{{1}, {2}}) {
		t.Errorf("files %+v, want differing [1] [2]", f)
	}
}

func TestFilesLastOfAcceptedAndReusedWins(t *testing.T) {
	a := accepted("R-1", "StepAccepted", "old").
		add(20, machine.Event{Kind: "ResultReused", Step: "implement", Round: 2, Artifacts: []string{"h"}})
	r := trials.Of(pattern, numbered(a, accepted("R-2", "StepAccepted", "h")))
	if f := step(t, r, "implement").Files; f == nil || !f.Same {
		t.Errorf("files %+v, want same", f)
	}
}

func TestFilesOnlyOverTrialsThatAcceptedTheStep(t *testing.T) {
	// Trial 2 reached the step and was rejected, trial 3 is still on it: neither has output to compare.
	b := newRec("R-2").
		add(5, machine.Event{Kind: "StepStarted", Step: "implement"}).
		add(8, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 1, Usage: use(1, 0.1), Artifacts: []string{"zzz"}}).
		add(9, machine.Event{Kind: "StepRejected", Step: "implement", Attempt: 1})
	c := newRec("R-3").add(5, machine.Event{Kind: "StepStarted", Step: "implement"})
	r := trials.Of(pattern, numbered(accepted("R-1", "StepAccepted", "h"), b, c))
	im := step(t, r, "implement")
	if im.Reached != 3 {
		t.Errorf("reached %d, want 3", im.Reached)
	}
	if im.Files == nil || !im.Files.Same {
		t.Errorf("files %+v, want same over the one trial that accepted", im.Files)
	}
}

func TestFilesSingleAcceptingTrialIsSame(t *testing.T) {
	r := trials.Of(pattern, numbered(accepted("R-1", "StepAccepted", "h")))
	if f := step(t, r, "implement").Files; f == nil || !f.Same {
		t.Errorf("files %+v, want same", f)
	}
}

func TestFilesNilWhenNoTrialAcceptedTheStep(t *testing.T) {
	b := newRec("R-1").add(5, machine.Event{Kind: "StepStarted", Step: "implement"}).
		add(8, machine.Event{Kind: "AttemptMade", Step: "implement", Attempt: 1, Usage: use(1, 0.1), Artifacts: []string{"x"}})
	r := trials.Of(pattern, numbered(b))
	if f := step(t, r, "implement").Files; f != nil {
		t.Errorf("files %+v, want none when no trial accepted the step", f)
	}
}

func TestFilesNoOutputDiffersFromSomeOutput(t *testing.T) {
	r := trials.Of(pattern, numbered(
		accepted("R-1", "StepAccepted"),
		accepted("R-2", "StepAccepted", "x"),
	))
	f := step(t, r, "implement").Files
	if f == nil || f.Same || !groupsEqual(f.Groups, [][]int{{1}, {2}}) {
		t.Errorf("files %+v, want differing [1] [2]", f)
	}
	r = trials.Of(pattern, numbered(accepted("R-1", "StepAccepted"), accepted("R-2", "StepAccepted")))
	if f := step(t, r, "implement").Files; f == nil || !f.Same {
		t.Errorf("files %+v, want same when both have no output", f)
	}
}

func TestFilesAreJudgedPerStep(t *testing.T) {
	mk := func(run, impl, rev string) *rec {
		return accepted(run, "StepAccepted", impl).
			add(20, machine.Event{Kind: "StepStarted", Step: "review"}).
			add(25, machine.Event{Kind: "StepAccepted", Step: "review", Artifacts: []string{rev}})
	}
	r := trials.Of(pattern, numbered(mk("R-1", "h", "r1"), mk("R-2", "h", "r2")))
	if f := step(t, r, "implement").Files; f == nil || !f.Same {
		t.Errorf("implement files %+v, want same", f)
	}
	if f := step(t, r, "review").Files; f == nil || f.Same {
		t.Errorf("review files %+v, want differing", f)
	}
}

// SC-003: planted sets whose files differ are marked, those whose files are equal are not.
func TestSC003DifferingSetsAreMarkedEqualSetsAreNot(t *testing.T) {
	cases := []struct {
		name   string
		hashes []string // each trial's one output hash
		differ bool
	}{
		{"two equal", []string{"a", "a"}, false},
		{"five equal", []string{"a", "a", "a", "a", "a"}, false},
		{"two differ", []string{"a", "b"}, true},
		{"last differs", []string{"a", "a", "a", "a", "b"}, true},
		{"first differs", []string{"b", "a", "a"}, true},
		{"all differ", []string{"a", "b", "c", "d", "e"}, true},
		{"one trial", []string{"a"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rs := make([]*rec, len(c.hashes))
			for i, h := range c.hashes {
				rs[i] = accepted(fmt.Sprintf("R-%d", i+1), "StepAccepted", h)
			}
			f := step(t, trials.Of(pattern, numbered(rs...)), "implement").Files
			if f == nil {
				t.Fatal("no files verdict for a step every trial accepted")
			}
			if f.Same == c.differ {
				t.Errorf("same %v for hashes %v, want same %v", f.Same, c.hashes, !c.differ)
			}
			if c.differ && len(f.Groups) < 2 {
				t.Errorf("groups %v, want at least two for differing files", f.Groups)
			}
		})
	}
}

func TestOfKeepsOutcomesWhileFillingMeasures(t *testing.T) {
	a := accepted("R-1", "StepAccepted", "h").add(30, machine.Event{Kind: "RunCompleted"})
	b := accepted("R-2", "StepAccepted", "h").add(30, machine.Event{Kind: "HandOverAccepted"})
	r := trials.Of(pattern, numbered(a, b))
	if r.Ended != 2 || r.Done != 1 || !r.Unstable || len(r.Outcomes) != 2 || r.Outcomes[0].Totals.Attempts == nil {
		t.Errorf("result %+v, want outcomes kept and totals filled", r)
	}
}
