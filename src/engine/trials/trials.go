// Package trials says what the trials of a trial set show together (feature 012, data model
// "Trial result"): how many ended, how many of those ended done, whether the builds are unstable,
// each trial's outcome and method, and what the set has spent. It reads only the run records it
// is given: no store, no model, no clock. It is the one owner of these rules; the engine API only
// sequences the trials and shows the result.
package trials

import (
	"slices"
	"strings"
	"time"

	"github.com/agentic-platform/engine/machine"
)

// Trial is one started trial of a set: its number (1..k), its run ID and its run's record in
// order (empty while nothing is recorded yet).
type Trial struct {
	Number int
	Run    string
	Events []machine.Event
}

// Outcome is one started trial in the result.
type Outcome struct {
	Trial   int
	Run     string
	Outcome machine.Outcome // "" while the run has not ended (machine.StateOf)
	Method  string          // the last non-empty `method` on its events ("" when they carry none)
	Totals  Measures        // what the trial used (measures)
}

// Measures is what one trial, or one step of it, used; a nil field is a measure its record does
// not carry ("not available"), never 0. Attempts are the AttemptMade events plus each StepRejected
// with no AttemptMade of the same step, round and attempt (a failed worker or a refused result),
// usage or not; ReviewRounds the highest round; Seconds, for a step, the sum of its attempt spans
// (an attempt runs from its round's StepStarted, or from the previous attempt's StepRejected in the
// same round, to its StepAccepted or StepRejected), for a trial RunStarted to its end (its last
// event while it runs), either without the time the run was interrupted (RunInterrupted to
// RunResumed); Tokens and CostUSD the sums of usage, none for a step whose events carry none (a
// procedure step's). For a trial,
// attempts, tokens and cost are over all its steps, review rounds the highest round of any.
type Measures struct {
	Attempts, ReviewRounds, Tokens *int
	Seconds, CostUSD               *float64
}

// Spread is the lowest, median and highest of one measure over the trials that carry it.
type Spread struct{ Lowest, Median, Highest float64 }

// Spreads holds each measure of Measures as a Spread; nil when no trial carries it.
type Spreads struct {
	Attempts, ReviewRounds, Seconds, Tokens, CostUSD *Spread
}

// StepSpread is one step of the pattern over the trials whose record has it: how many reached it,
// its measures' spreads, and whether the trials that accepted it agree on its files (nil when none
// accepted it).
type StepSpread struct {
	Step     string
	Reached  int
	Measures Spreads
	Files    *StepFiles
}

// StepFiles says whether the trials that accepted a step have the same output hashes (the
// `artifacts` of the step's last StepAccepted or ResultReused). Groups holds the trial numbers by
// equal hashes, the groups in order of their first trial; one group when Same.
type StepFiles struct {
	Same   bool
	Groups [][]int
}

// Result is what a set's trials show together.
type Result struct {
	// Ended counts the trials whose run has ended (done, handed-over, expired or cancelled); Done
	// those of them that ended done.
	Ended, Done int
	// Unstable: at least one trial ended done and at least one ended otherwise.
	Unstable bool
	// MethodsMixed: the trials' methods (the non-empty Outcome.Method values) are not all one.
	MethodsMixed bool
	// Outcomes holds one per started trial, in the order given.
	Outcomes []Outcome
	// Steps holds one per step of the pattern, in pattern order; Totals the spreads of the trials'
	// totals.
	Steps  []StepSpread
	Totals Spreads
}

// Of computes the result of a set's trials, given in trial order, over the pattern's steps (in
// pattern order). A trial with nothing recorded is started and not ended, and carries no measure.
func Of(steps []string, trials []Trial) Result {
	r := Result{Outcomes: make([]Outcome, 0, len(trials))}
	methods := map[string]bool{}
	for _, t := range trials {
		o := Outcome{Trial: t.Number, Run: t.Run}
		if len(t.Events) > 0 {
			if st := machine.StateOf(t.Events); st.Status == machine.StatusEnded {
				o.Outcome = st.Outcome
				r.Ended++
				if st.Outcome == machine.OutcomeDone {
					r.Done++
				}
			}
		}
		for _, e := range t.Events {
			if e.Method != "" {
				o.Method = e.Method
			}
		}
		if o.Method != "" {
			methods[o.Method] = true
		}
		o.Totals = totalsOf(t.Events)
		r.Outcomes = append(r.Outcomes, o)
	}
	r.Unstable = r.Done >= 1 && r.Done < r.Ended
	r.MethodsMixed = len(methods) > 1
	totals := make([]Measures, len(r.Outcomes))
	for i, o := range r.Outcomes {
		totals[i] = o.Totals
	}
	r.Totals = spreadsOf(totals)
	r.Steps = make([]StepSpread, 0, len(steps))
	for _, name := range steps {
		r.Steps = append(r.Steps, stepOf(name, trials))
	}
	return r
}

// stepOf is one step over the trials whose record has it.
func stepOf(name string, trials []Trial) StepSpread {
	sp := StepSpread{Step: name}
	var ms []Measures
	type accepted struct {
		trial  int
		hashes string
	}
	var acc []accepted
	for _, t := range trials {
		if !slices.ContainsFunc(t.Events, func(e machine.Event) bool { return e.Step == name }) {
			continue
		}
		sp.Reached++
		ms = append(ms, measuresOf(t.Events, name))
		last := -1
		for i, e := range t.Events {
			if e.Step == name && (e.Kind == "StepAccepted" || e.Kind == "ResultReused") {
				last = i
			}
		}
		if last >= 0 {
			acc = append(acc, accepted{t.Number, strings.Join(t.Events[last].Artifacts, "\x00")})
		}
	}
	sp.Measures = spreadsOf(ms)
	if len(acc) > 0 {
		f := &StepFiles{}
		index := map[string]int{}
		for _, a := range acc {
			i, ok := index[a.hashes]
			if !ok {
				i = len(f.Groups)
				index[a.hashes] = i
				f.Groups = append(f.Groups, nil)
			}
			f.Groups[i] = append(f.Groups[i], a.trial)
		}
		f.Same = len(f.Groups) == 1
		sp.Files = f
	}
	return sp
}

// attemptKey is one attempt of one step: its step, round and attempt.
type attemptKey struct {
	step           string
	round, attempt int
}

// measuresOf is what the events of step (all events when step is "") used. Seconds, for a step,
// are its attempt spans; a trial's are left to totalsOf.
func measuresOf(events []machine.Event, step string) Measures {
	var m Measures
	attempts, rounds, tokens, cost := 0, 0, 0, 0.0
	used := false
	made := map[attemptKey]bool{}
	for _, e := range events {
		if (step == "" || e.Step == step) && e.Kind == "AttemptMade" {
			made[attemptKey{e.Step, e.Round, e.Attempt}] = true
		}
	}
	var seconds time.Duration
	spans := false
	// The step's attempt span that has not ended: from its round's StepStarted, or from the
	// previous attempt's StepRejected in the same round (afterRejection, of round openRound).
	var open *time.Time
	openRound, afterRejection := 0, false
	var paused time.Duration // interrupted time inside the open span
	var interrupted *time.Time
	for _, e := range events {
		switch e.Kind {
		case "RunInterrupted":
			if interrupted == nil {
				at := e.At
				interrupted = &at
			}
		case "RunResumed":
			if interrupted != nil {
				if open != nil {
					paused += max(e.At.Sub(laterOf(*interrupted, *open)), 0)
				}
				interrupted = nil
			}
		}
		if e.Step == "" || (step != "" && e.Step != step) {
			continue
		}
		switch e.Kind {
		case "AttemptMade":
			attempts++
		case "StepRejected":
			if !made[attemptKey{e.Step, e.Round, e.Attempt}] { // a failed worker or a refused result
				attempts++
			}
		}
		if e.Round > rounds {
			rounds = e.Round
		}
		if e.Usage != nil {
			used = true
			tokens += int(e.Usage.Tokens)
			cost += e.Usage.CostUSD
		}
		if step == "" {
			continue
		}
		switch e.Kind {
		case "StepStarted":
			at := e.At
			open, openRound, afterRejection, paused = &at, e.Round, false, 0
		case "StepAccepted", "StepRejected":
			if open != nil && (!afterRejection || e.Round == openRound) {
				d := e.At.Sub(*open) - paused
				if interrupted != nil {
					d -= max(e.At.Sub(laterOf(*interrupted, *open)), 0)
				}
				seconds += max(d, 0)
				spans = true
			}
			open = nil
			if e.Kind == "StepRejected" { // the next attempt of the round, if any, starts here
				at := e.At
				open, openRound, afterRejection, paused = &at, e.Round, true, 0
			}
		}
	}
	if attempts > 0 {
		m.Attempts = &attempts
	}
	if rounds > 0 {
		m.ReviewRounds = &rounds
	}
	if used {
		m.Tokens, m.CostUSD = &tokens, &cost
	}
	if spans {
		sec := seconds.Seconds()
		m.Seconds = &sec
	}
	return m
}

// laterOf is the later of a and b.
func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// totalsOf is what a trial used over all its steps; none for a trial with nothing recorded.
func totalsOf(events []machine.Event) Measures {
	if len(events) == 0 {
		return Measures{}
	}
	m := measuresOf(events, "")
	rounds := 0
	for _, e := range events {
		rounds = max(rounds, e.Round)
	}
	m.ReviewRounds = nil
	if rounds > 0 {
		m.ReviewRounds = &rounds
	}
	first, last := events[0].At, events[len(events)-1].At
	var interrupted time.Duration
	var since *time.Time
	for _, e := range events {
		switch e.Kind {
		case "RunInterrupted":
			if since == nil {
				at := e.At
				since = &at
			}
		case "RunResumed":
			if since != nil {
				interrupted += e.At.Sub(*since)
				since = nil
			}
		}
	}
	if since != nil {
		interrupted += last.Sub(*since)
	}
	sec := max(last.Sub(first)-interrupted, 0).Seconds()
	m.Seconds = &sec
	return m
}

// spreadsOf is each measure's spread over the measures that carry it.
func spreadsOf(ms []Measures) Spreads {
	var att, rnd, sec, tok, cost []float64
	for _, m := range ms {
		if m.Attempts != nil {
			att = append(att, float64(*m.Attempts))
		}
		if m.ReviewRounds != nil {
			rnd = append(rnd, float64(*m.ReviewRounds))
		}
		if m.Seconds != nil {
			sec = append(sec, *m.Seconds)
		}
		if m.Tokens != nil {
			tok = append(tok, float64(*m.Tokens))
		}
		if m.CostUSD != nil {
			cost = append(cost, *m.CostUSD)
		}
	}
	return Spreads{Attempts: spreadOf(att), ReviewRounds: spreadOf(rnd), Seconds: spreadOf(sec), Tokens: spreadOf(tok),
		CostUSD: spreadOf(cost)}
}

// spreadOf is the lowest, median and highest of vs, nil for none; the median of an even count is
// the mean of the two middle values.
func spreadOf(vs []float64) *Spread {
	if len(vs) == 0 {
		return nil
	}
	vs = slices.Clone(vs)
	slices.Sort(vs)
	n := len(vs)
	med := vs[n/2]
	if n%2 == 0 {
		med = (vs[n/2-1] + vs[n/2]) / 2
	}
	return &Spread{Lowest: vs[0], Median: med, Highest: vs[n-1]}
}

// Spent is what the trials have cost so far, in US dollars: the sum of `usage.cost_usd` over
// every event of their records (a run records each attempt's use once, on its AttemptMade or,
// when it made none, on its StepRejected).
func Spent(trials []Trial) float64 {
	var sum float64
	for _, t := range trials {
		for _, e := range t.Events {
			if e.Usage != nil {
				sum += e.Usage.CostUSD
			}
		}
	}
	return sum
}

// budgetSlack absorbs the rounding of summed costs, so a set whose cost so far plus the next
// trial's budget equals its budget exactly may still start it.
const budgetSlack = 1e-9

// MayStart says whether a set that has spent spent may start a trial whose own run budget is
// next: only while spent plus next is at most the set's budget (research R5).
func MayStart(spent, next, budget float64) bool { return spent+next <= budget+budgetSlack }
