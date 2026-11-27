// Package trials says what the trials of a trial set show together (feature 012, data model
// "Trial result"): how many ended, how many of those ended done, whether the builds are unstable,
// each trial's outcome and method, and what the set has spent. It reads only the run records it
// is given: no store, no model, no clock. It is the one owner of these rules; the engine API only
// sequences the trials and shows the result.
package trials

import "github.com/agentic-platform/engine/machine"

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
	Totals  Measures        // what the trial used (user story 2; zero until then)
}

// Measures is what one trial, or one step of it, used; a nil field is a measure its record does
// not carry ("not available"), never 0. User story 2 fills it.
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

// StepSpread is one step of the pattern over the trials whose record has it (user story 2).
type StepSpread struct {
	Step     string
	Reached  int
	Measures Spreads
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
	// Steps and Totals are the per-step and per-trial spreads (user story 2; empty until then).
	Steps  []StepSpread
	Totals Spreads
}

// Of computes the result of a set's trials, given in trial order, over the pattern's steps (in
// pattern order). A trial with nothing recorded is started and not ended.
func Of(steps []string, trials []Trial) Result {
	_ = steps // the per-step spread is user story 2's
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
		r.Outcomes = append(r.Outcomes, o)
	}
	r.Unstable = r.Done >= 1 && r.Done < r.Ended
	r.MethodsMixed = len(methods) > 1
	return r
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
