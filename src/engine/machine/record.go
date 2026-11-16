package machine

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

var terminalOutcome = map[string]Outcome{
	"RunCompleted": OutcomeDone, "HandOverAccepted": OutcomeHandedOver,
	"RunExpired": OutcomeExpired, "RunCancelled": OutcomeCancelled,
}

// load rebuilds a run from its record, after a restart. A run whose last event is not terminal
// and does not hand it over is interrupted. The slice comes from RunStarted, the use so far from
// each attempt's usage and the running time from the events' times.
func (m *Machine) load(events []Event) (*run, error) {
	p := m.cfg.Pattern
	first := events[0]
	if first.Kind != "RunStarted" || first.Seq != 1 {
		return nil, fmt.Errorf("machine: run %s: the record does not begin with RunStarted", first.Run)
	}
	if first.Pattern != p.ref() {
		return nil, fmt.Errorf("machine: run %s was started with pattern %s, not %s", first.Run, first.Pattern, p.ref())
	}
	names := p.sliceInputs()
	if len(first.Inputs) != len(names) {
		return nil, fmt.Errorf("machine: run %s: RunStarted names %d inputs, the pattern %d", first.Run, len(first.Inputs), len(names))
	}
	r := &run{Run: Run{ID: first.Run, Slice: first.Slice, Project: first.Project, Pattern: first.Pattern}, refs: first.Refs, artifacts: map[string]string{},
		since: first.At}
	for i, n := range names {
		r.artifacts[n] = first.Inputs[i]
	}
	index := map[string]int{}
	for i, s := range p.Steps {
		index[s.Name] = i
	}
	openOf := map[int][]Finding{}
	var recent []Finding // the blocking findings recorded directly before the current event
	running := true
	for i, e := range events {
		if e.Seq != i+1 || e.Run != r.ID {
			return nil, fmt.Errorf("machine: run %s: event %d is out of order", r.ID, i+1)
		}
		r.seq = e.Seq
		si, known := index[e.Step]
		if e.Step != "" && !known {
			return nil, fmt.Errorf("machine: run %s: event %d names step %s, which the pattern has not", r.ID, e.Seq, e.Step)
		}
		review := known && isReview(p.Steps[si]) && si > 0
		fix := known && si+1 < len(p.Steps) && isReview(p.Steps[si+1]) && r.round > 0
		if e.Usage != nil {
			u := e.Usage.clamped()
			r.used.Tokens += u.Tokens
			r.used.CostUSD += u.CostUSD
		}
		before := recent // the findings directly before e (the open round's, when e leaves it open)
		if e.Kind != "FindingRecorded" {
			recent = nil
		}
		switch e.Kind {
		case "RunResumed":
			r.since, running = e.At, true
		case "RunInterrupted", "RunHandedOver":
			if running {
				r.elapsed += e.At.Sub(r.since)
			}
			running = false
		case "StepStarted":
			if review {
				r.round, r.fixing = max(e.Round, 1), false
			} else if fix {
				r.fixing, r.open = true, slices.Clone(openOf[r.round])
			}
		case "FindingRecorded":
			if review && e.Finding != nil && e.Finding.Blocking {
				recent = append(recent, *e.Finding)
			}
		case "StepRejected":
			// A review round left open (drive.go, keepOpenRound): with its witnesses, its files are
			// the project's and their commit is made again first; then the fix of the round
			// follows with its open findings, or the hand-over at the last round (research D36).
			if review && (e.OpenRound || e.Files != nil) {
				r.leaveOpen(p.Steps[si], e, before, "", openOf)
			}
		case "ResultReused":
			if review && e.OpenRound { // a reused open round: as a live one, nothing accepted
				reusedFrom := ""
				if a, ok := m.cfg.Results.Get(e.ReuseKey); ok {
					reusedFrom = a.Run
				}
				r.leaveOpen(p.Steps[si], e, before, reusedFrom, openOf)
				break
			}
			fallthrough
		case "StepAccepted":
			s := p.Steps[si]
			if len(e.Artifacts) != len(s.Outputs) {
				return nil, fmt.Errorf("machine: run %s: event %d gives %d artifacts for %d outputs", r.ID, e.Seq, len(e.Artifacts), len(s.Outputs))
			}
			for j, o := range s.Outputs {
				r.artifacts[o] = e.Artifacts[j]
			}
			if !slices.Contains(r.Accepted, s.Name) {
				r.Accepted = append(r.Accepted, s.Name)
			}
			reusedFrom := ""
			if a, ok := m.cfg.Results.Get(e.ReuseKey); ok && e.Kind == "ResultReused" {
				reusedFrom = a.Run
			}
			r.commit = commitOf(r, s, e, reusedFrom) // resume makes it again first
			r.before = slices.Clone(e.Files)
			switch {
			case review:
				r.pos, r.round, r.fixing, r.open, r.changed = si+1, 0, false, nil, nil
			case fix: // a reused fix has no StepStarted; the open event took its open findings
				r.changed, r.fixing = slices.Clone(e.ChangedLines), false
				r.round++
			default:
				r.pos = max(r.pos, si+1)
			}
		}
	}
	last := events[len(events)-1]
	r.Status, r.Reason, r.ExpiresAt = StatusInterrupted, last.Detail, last.At.Add(HandOverTTL)
	switch {
	case terminalOutcome[last.Kind] != "":
		r.Status, r.Outcome, r.ExpiresAt = StatusEnded, terminalOutcome[last.Kind], time.Time{}
	case last.Kind == "RunHandedOver":
		r.Status, r.Open = StatusWaiting, slices.Clone(openOf[r.round])
	case last.Kind == "RunInterrupted":
	default:
		r.elapsed += last.At.Sub(r.since)
		r.Reason = "the platform stopped during the run"
	}
	return r, nil
}

// leaveOpen is load's part for the event e that left review round e.Round open (a StepRejected
// or a ResultReused with open_round): its open findings are the blocking ones recorded directly
// before e, its commit (when it carries files) is made again first, and the fix comes next.
func (r *run) leaveOpen(s Step, e Event, recent []Finding, reusedFrom string, openOf map[int][]Finding) {
	openOf[e.Round] = slices.Clone(recent)
	r.round, r.fixing, r.open = e.Round, true, slices.Clone(recent)
	if e.Files != nil && len(s.Writable) > 0 {
		r.commit = commitOf(r, s, e, reusedFrom)
		r.before = slices.Clone(e.Files)
	}
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
