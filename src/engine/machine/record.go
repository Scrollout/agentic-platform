package machine

import (
	"encoding/json"
	"slices"
	"strings"
)

var terminalOutcome = map[string]Outcome{
	"RunCompleted": OutcomeDone, "HandOverAccepted": OutcomeHandedOver,
	"RunExpired": OutcomeExpired, "RunCancelled": OutcomeCancelled,
}

// Difference is one field of one step that differs between two runs, as the contract's
// Difference schema; Left or Right is empty when that run has no value.
type Difference struct {
	Step, Field, Left, Right string
}

// stepView is what Compare looks at for one step of one run.
type stepView struct{ artifacts, checks, decision string }

// view reads, per step, the artifacts it settled on (the accepted or reused ones, otherwise the
// last attempt's), its checks (`passed` when accepted or reused, otherwise the failed checks of
// its last attempt) and its decision as JSON.
func view(events []Event) (map[string]*stepView, []string) {
	out := map[string]*stepView{}
	var order []string
	var failed []string
	get := func(step string) *stepView {
		v, ok := out[step]
		if !ok {
			v = &stepView{}
			out[step] = v
			order = append(order, step)
		}
		return v
	}
	for _, e := range events {
		if e.Step == "" {
			continue
		}
		v := get(e.Step)
		switch e.Kind {
		case "AttemptMade":
			v.artifacts, failed = strings.Join(e.Artifacts, ","), nil
		case "CheckFailed":
			failed = append(failed, e.Check.Reason)
			v.checks = "failed: " + strings.Join(failed, "; ")
		case "CheckPassed":
			if len(failed) == 0 {
				v.checks = "passed"
			}
		case "ResultReused":
			if e.OpenRound { // read as the original open round: its artifacts, failed check and decision
				v.artifacts, failed = strings.Join(e.Artifacts, ","), nil
				if e.Check != nil {
					v.checks = "failed: " + e.Check.Reason
				}
				v.decision = rejectedDecision(e.Detail)
				break
			}
			v.artifacts, v.checks, failed = strings.Join(e.Artifacts, ","), "passed", nil
			v.decision = `{"accepted":true}`
		case "StepAccepted":
			v.artifacts, v.checks, failed = strings.Join(e.Artifacts, ","), "passed", nil
			v.decision = `{"accepted":true}`
		case "StepRejected":
			v.decision = rejectedDecision(e.Detail)
		}
	}
	return out, order
}

func rejectedDecision(reason string) string {
	b, _ := json.Marshal(struct {
		Accepted bool   `json:"accepted"`
		Reason   string `json:"reason"`
	}{false, reason})
	return string(b)
}

// Compare lists the fields that differ between two runs' records, in step order (the first run's
// order, then steps only the second reached) and then field order (artifacts, checks, decision).
func Compare(a, b []Event) []Difference {
	va, order := view(a)
	vb, orderB := view(b)
	for _, s := range orderB {
		if !slices.Contains(order, s) {
			order = append(order, s)
		}
	}
	var out []Difference
	for _, s := range order {
		l, r := va[s], vb[s]
		if l == nil {
			l = &stepView{}
		}
		if r == nil {
			r = &stepView{}
		}
		for _, f := range []struct{ name, l, r string }{
			{"artifacts", l.artifacts, r.artifacts}, {"checks", l.checks, r.checks}, {"decision", l.decision, r.decision},
		} {
			if f.l != f.r {
				out = append(out, Difference{Step: s, Field: f.name, Left: f.l, Right: f.r})
			}
		}
	}
	return out
}

// StateOf is a run's state as its record shows it, the record being written by the run's
// workflow (data model, "Run states"): ended with the outcome of an end event; waiting for the
// developer after RunHandedOver; interrupted after RunInterrupted; otherwise running (a platform
// stop records RunInterrupted then RunResumed, so a stopped run that continued is running). Reason
// is the detail of the event that ended, handed over or interrupted the run; ExpiresAt is 14 days
// (HandOverTTL) after a hand-over or interruption. Accepted lists the steps accepted or reused,
// in order. It reads nothing else and needs no pattern; an empty record gives a zero Run.
func StateOf(events []Event) Run {
	if len(events) == 0 {
		return Run{}
	}
	first, last := events[0], events[len(events)-1]
	r := Run{ID: first.Run, Slice: first.Slice, Project: first.Project, Pattern: first.Pattern, Status: StatusRunning}
	for _, e := range events {
		if (e.Kind == "StepAccepted" || e.Kind == "ResultReused" && !e.OpenRound) && !slices.Contains(r.Accepted, e.Step) {
			r.Accepted = append(r.Accepted, e.Step)
		}
	}
	switch {
	case terminalOutcome[last.Kind] != "":
		r.Status, r.Outcome, r.Reason = StatusEnded, terminalOutcome[last.Kind], last.Detail
	case last.Kind == "RunHandedOver":
		r.Status, r.Reason, r.ExpiresAt = StatusWaiting, last.Detail, last.At.Add(HandOverTTL)
	case last.Kind == "RunInterrupted":
		r.Status, r.Reason, r.ExpiresAt = StatusInterrupted, last.Detail, last.At.Add(HandOverTTL)
	}
	return r
}
