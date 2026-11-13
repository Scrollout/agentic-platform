package machine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
)

// driver drives one run; only one exists per run at a time (run.driving).
type driver struct {
	m *Machine
	r *run
}

func (x *driver) pattern() Pattern { return x.m.cfg.Pattern }

func isReview(s Step) bool { return s.Reviews() }

// Reviews reports whether the step is the review step, the one run in rounds: its checks include
// NoBlockingFindingOpen. Its output is its findings, not a set of files.
func (s Step) Reviews() bool { return slices.Contains(s.Checks, noBlockingFindingOpen) }

func isAgent(s Step) bool { return strings.HasPrefix(s.Worker, "agent:") }

// record appends an event under the lock.
func (x *driver) record(e Event) error {
	x.m.mu.Lock()
	defer x.m.mu.Unlock()
	return x.recordLocked(e)
}

// recordLocked appends an event; when the append fails the run is interrupted (it can be resumed).
func (x *driver) recordLocked(e Event) error {
	err := x.m.emit(x.r, e)
	if err != nil && !errors.Is(err, errHalt) {
		x.stopLocked(StatusInterrupted, "the run record could not be written", nil)
	}
	return err
}

func (x *driver) stopLocked(st Status, reason string, open []Finding) {
	now := x.m.cfg.Clock.Now()
	x.r.elapsed += now.Sub(x.r.since)
	x.r.Status, x.r.Reason, x.r.Open, x.r.ExpiresAt = st, reason, open, now.Add(HandOverTTL)
}

// pause records kind (RunHandedOver or RunInterrupted) and stops the run in status st; it always
// returns a halting error.
func (x *driver) pause(kind string, st Status, reason string, open []Finding) error {
	x.m.mu.Lock()
	defer x.m.mu.Unlock()
	if err := x.recordLocked(Event{Kind: kind, Refs: x.r.refs, Detail: reason}); err != nil {
		return err
	}
	x.stopLocked(st, reason, open)
	return errHalt
}

func (x *driver) handOver(reason string, open []Finding) error {
	return x.pause("RunHandedOver", StatusWaiting, reason, open)
}

func (x *driver) interrupt(reason string) error {
	return x.pause("RunInterrupted", StatusInterrupted, reason, nil)
}

func (x *driver) steps(ctx context.Context) error {
	steps := x.pattern().Steps
	for x.r.pos < len(steps) {
		s := steps[x.r.pos]
		var err error
		if isReview(s) && x.r.pos > 0 {
			err = x.review(ctx, steps[x.r.pos-1], s)
		} else {
			_, _, err = x.step(ctx, s, 0, nil, nil)
		}
		if err != nil {
			return err
		}
		x.r.pos++
		x.r.round, x.r.fixing, x.r.open, x.r.changed = 0, false, nil, nil
	}
	x.m.mu.Lock()
	defer x.m.mu.Unlock()
	return x.m.end(x.r, "RunCompleted", OutcomeDone, "")
}

// review runs the review step in rounds; after a round with open blocking findings the fix step
// (the step before the review) runs again with them, and the next round gets only those findings
// and the lines the fix changed (research D11). Where it stands is kept in the run, so Resume
// continues the round it stopped in.
func (x *driver) review(ctx context.Context, fix, review Step) error {
	rounds := max(x.pattern().ReviewRounds, 1)
	if x.r.round == 0 {
		x.r.round = 1
	}
	for {
		if x.r.fixing {
			fixed, _, err := x.step(ctx, fix, 0, x.r.open, nil)
			if err != nil {
				return err
			}
			x.r.changed, x.r.fixing = fixed.ChangedLines, false
			x.r.round++
		}
		res, v, err := x.step(ctx, review, x.r.round, x.r.open, x.r.changed)
		if err != nil {
			return err
		}
		if v == accepted {
			return nil
		}
		x.r.open = nil
		for _, f := range res.Findings {
			if f.Blocking {
				x.r.open = append(x.r.open, f)
			}
		}
		if x.r.round >= rounds {
			list := make([]string, len(x.r.open))
			for i, f := range x.r.open {
				list[i] = fmt.Sprintf("%s at %s:%d (%s)", f.Rule, f.File, f.Line, f.Text)
			}
			return x.handOver(fmt.Sprintf("review found blocking findings still open after %d rounds: %s",
				x.r.round, strings.Join(list, "; ")), slices.Clone(x.r.open))
		}
		x.r.fixing = true
	}
}

type verdict int

const (
	accepted verdict = iota
	rejected
	workerFailed
	findingsOpen
	overBudget
)

// stepKey gives the inputs a step's reuse key covers (its declared inputs, plus for a fix or a
// later review round the findings and lines it is given) and the key, both known before it runs.
func (x *driver) stepKey(s Step, inputs []string, open []Finding, changed []Line) ([]string, string) {
	in := slices.Clone(inputs)
	if len(open) > 0 || len(changed) > 0 {
		in = append(in, contextHash(open, changed))
	}
	model := ""
	if isAgent(s) {
		model = x.m.cfg.Model
	}
	return in, ReuseKey(KeyParts{PatternVersion: x.r.Pattern, Step: s.Name, Inputs: in,
		AgentDigest: s.AgentDigest, Model: model})
}

// artifacts copies the run's artifacts by name.
func (x *driver) artifacts() map[string]string {
	x.m.mu.Lock()
	defer x.m.mu.Unlock()
	return maps.Clone(x.r.artifacts)
}

// reusable says whether an accepted result can stand for the step in this run. One that left
// project files is reused only in the project they were written into: nothing yet writes a reused
// result's files into a new project (T053a), so a replay into one would start the next step from
// files the folder does not hold. Until T053a lifts this, such a step runs again there.
func (x *driver) reusable(a Accepted) bool {
	return len(a.Files) == 0 || a.Project == x.r.Project
}

// accept makes the step's artifacts and the project's files after it the run's, and counts it
// accepted.
func (x *driver) accept(s Step, artifacts []string, files []File) {
	x.m.mu.Lock()
	defer x.m.mu.Unlock()
	x.r.before = slices.Clone(files)
	for i, name := range s.Outputs {
		x.r.artifacts[name] = artifacts[i]
	}
	if !slices.Contains(x.r.Accepted, s.Name) {
		x.r.Accepted = append(x.r.Accepted, s.Name)
	}
}

// step reuses an accepted result for the step's key, or runs its attempts until one is accepted
// (or, in review, a round ends with open findings). Any other end hands the run over, interrupts
// or stops it, returned as an error.
func (x *driver) step(ctx context.Context, s Step, round int, open []Finding, changed []Line) (Result, verdict, error) {
	inputs := make([]string, len(s.Inputs))
	for i, name := range s.Inputs {
		h, ok := x.r.artifacts[name]
		if !ok {
			return Result{}, rejected, x.handOver(fmt.Sprintf("step %s has no input %s", s.Name, name), nil)
		}
		inputs[i] = h
	}
	keyInputs, key := x.stepKey(s, inputs, open, changed)
	if a, ok := x.m.cfg.Results.Get(key); ok && len(a.Artifacts) == len(s.Outputs) && x.reusable(a) {
		e := Event{Kind: "ResultReused", Step: s.Name, Round: round, Worker: s.Worker, Pattern: x.r.Pattern,
			Inputs: keyInputs, Artifacts: a.Artifacts, ReuseKey: key, Refs: x.r.refs, ChangedLines: slices.Clone(a.Lines),
			Files: slices.Clone(a.Files)}
		if isAgent(s) {
			e.AgentDigest, e.Model = s.AgentDigest, x.m.cfg.Model
		}
		if err := x.record(e); err != nil {
			return Result{}, rejected, err
		}
		x.accept(s, a.Artifacts, a.Files)
		out := map[string]string{}
		for i, name := range s.Outputs {
			out[name] = a.Artifacts[i]
		}
		return Result{Outputs: out, ChangedLines: slices.Clone(a.Lines), ProjectFiles: slices.Clone(a.Files)}, accepted, nil
	}

	if err := x.record(Event{Kind: "StepStarted", Step: s.Name, Round: round, Refs: x.r.refs}); err != nil {
		return Result{}, rejected, err
	}
	attempts := max(x.pattern().Attempts, 1)
	var last string
	failures := 0
	for a := 1; a <= attempts; a++ {
		if ctx.Err() != nil {
			return Result{}, rejected, x.stopped(s)
		}
		if why := x.runOverBudget(); why != "" {
			return Result{}, overBudget, x.handOver(why, nil)
		}
		res, v, why, err := x.attempt(ctx, s, attemptOf{a, round, open, changed, inputs, keyInputs, key})
		if err != nil {
			return res, v, err
		}
		if v == accepted || v == findingsOpen {
			return res, v, nil
		}
		if ctx.Err() != nil { // the platform stopped during the attempt: interrupted, never handed over
			return Result{}, rejected, x.stopped(s)
		}
		switch v {
		case overBudget:
			return res, v, x.handOver(why, nil)
		case workerFailed:
			failures++
		}
		last = why
	}
	if ctx.Err() != nil {
		return Result{}, rejected, x.stopped(s)
	}
	if failures == attempts {
		return Result{}, workerFailed, x.interrupt(fmt.Sprintf(
			"the worker of step %s failed on all %d attempts; the last: %s", s.Name, attempts, last))
	}
	return Result{}, rejected, x.handOver(fmt.Sprintf("step %s was not accepted after %d attempts; the last: %s",
		s.Name, attempts, last), nil)
}

// stopped interrupts a run whose context was cancelled (the platform stops), unless Cancel ended it.
func (x *driver) stopped(s Step) error {
	return x.interrupt(fmt.Sprintf("the run stopped during step %s", s.Name))
}

var hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// parse rejects a result the run record could not hold; a worker's output is never trusted.
func (x *driver) parse(s Step, res Result) ([]string, error) {
	if isAgent(s) {
		switch want := x.m.cfg.Model; {
		case res.Model == "":
			return nil, errors.New("the worker named no model")
		case want != "" && res.Model != want:
			return nil, fmt.Errorf("the worker used model %s, not the pattern's %s", res.Model, want)
		}
	}
	out := make([]string, len(s.Outputs))
	for i, name := range s.Outputs {
		h := res.Outputs[name]
		if !hashRE.MatchString(h) {
			return nil, fmt.Errorf("output %s is missing or not a SHA-256 hash", name)
		}
		out[i] = h
	}
	return out, nil
}

type attemptOf struct {
	n, round          int
	open              []Finding
	changed           []Line
	inputs, keyInputs []string
	key               string
}

// attempt runs one attempt and its checks and records them. The reason says why it was not accepted.
func (x *driver) attempt(ctx context.Context, s Step, at attemptOf) (Result, verdict, string, error) {
	clk := x.m.cfg.Clock
	began := clk.Now()
	res, werr := x.m.cfg.Worker.Run(ctx, Request{Run: x.r.ID, Step: s, Attempt: at.n, Round: at.round,
		Open: slices.Clone(at.open), ChangedLines: slices.Clone(at.changed), Inputs: slices.Clone(at.inputs),
		Slice: x.r.Slice, Project: x.r.Project, Before: slices.Clone(x.r.before), Artifacts: x.artifacts()})
	use := res.Usage.clamped()
	x.r.used.Tokens += use.Tokens
	x.r.used.CostUSD += use.CostUSD
	budget := x.overBudget(s, use, clk.Now().Sub(began))
	base := Event{Step: s.Name, Attempt: at.n, Round: at.round, Refs: x.r.refs}
	// the attempt's use goes on its AttemptMade, or on its StepRejected when it made none, so the
	// run's use can be summed from the record after a restart
	used := base
	used.Usage = &use

	if werr != nil {
		why := "the worker failed: " + werr.Error()
		if err := x.reject(used, why); err != nil {
			return res, rejected, why, err
		}
		if budget != "" {
			return res, overBudget, budget, nil
		}
		return res, workerFailed, why, nil
	}
	artifacts, perr := x.parse(s, res)
	if perr != nil {
		why := "the worker's result was refused: " + perr.Error()
		if budget != "" {
			return res, overBudget, budget, x.reject(used, why)
		}
		return res, rejected, why, x.reject(used, why)
	}

	made := used
	made.Kind, made.Worker, made.Model, made.Pattern = "AttemptMade", s.Worker, res.Model, x.r.Pattern
	if isAgent(s) {
		made.AgentDigest = s.AgentDigest
	}
	made.Inputs, made.Artifacts, made.ReuseKey = at.keyInputs, artifacts, at.key
	if err := x.record(made); err != nil {
		return res, rejected, "", err
	}
	if budget != "" {
		return res, overBudget, budget, x.reject(base, budget)
	}

	var failed []string
	for _, name := range s.Checks {
		cr, cerr := x.m.cfg.Checker.Check(ctx, CheckRequest{Run: x.r.ID, Step: s, Attempt: at.n, Name: name, Result: res,
			Round: at.round, Inputs: slices.Clone(at.inputs), Slice: x.r.Slice, Project: x.r.Project})
		if cerr != nil {
			cr = CheckResult{Passed: false, Reason: "the check could not run: " + cerr.Error()}
		}
		e := base
		e.Kind, e.Check = "CheckPassed", &EventCheck{Name: name, Passed: cr.Passed, Reason: cr.Reason}
		if !cr.Passed {
			e.Kind = "CheckFailed"
			failed = append(failed, name)
		}
		if e.Check.Reason == "" {
			e.Check.Reason = name + " " + map[bool]string{true: "passed", false: "failed"}[cr.Passed]
		}
		if len(cr.Refs) > 0 {
			e.Refs = cr.Refs
		}
		if err := x.record(e); err != nil {
			return res, rejected, "", err
		}
	}

	open := isReview(s) && slices.Equal(failed, []string{noBlockingFindingOpen})
	if len(failed) == 0 || open {
		for _, f := range res.Findings {
			e := base
			e.Kind, e.Finding, e.Refs = "FindingRecorded", &f, []string{f.Rule}
			if err := x.record(e); err != nil {
				return res, rejected, "", err
			}
		}
	}
	switch {
	case len(failed) == 0:
		made.Kind, made.Usage, made.ChangedLines = "StepAccepted", nil, slices.Clone(res.ChangedLines)
		made.Files = slices.Clone(res.ProjectFiles)
		if err := x.record(made); err != nil {
			return res, rejected, "", err
		}
		x.m.cfg.Results.Put(Accepted{Key: at.key, Artifacts: artifacts, Run: x.r.ID, Lines: slices.Clone(res.ChangedLines),
			Files: slices.Clone(res.ProjectFiles), Project: x.r.Project})
		x.accept(s, artifacts, res.ProjectFiles)
		return res, accepted, "", nil
	case open:
		why := fmt.Sprintf("review round %d left blocking findings open", at.round)
		return res, findingsOpen, why, x.reject(base, why)
	}
	why := "failed checks: " + strings.Join(failed, ", ")
	return res, rejected, why, x.reject(base, why)
}

func (x *driver) reject(base Event, why string) error {
	base.Kind, base.Detail = "StepRejected", why
	return x.record(base)
}

// overBudget names the budget an attempt took the step or the run past, or is empty.
func (x *driver) overBudget(s Step, u Usage, took time.Duration) string {
	sb := x.pattern().StepBudget
	switch {
	case u.Tokens > sb.Tokens:
		return fmt.Sprintf("step %s used %d tokens, past the step's token budget of %d", s.Name, u.Tokens, sb.Tokens)
	case u.CostUSD > sb.CostUSD:
		return fmt.Sprintf("step %s cost $%.4f, past the step's cost budget of $%.4f", s.Name, u.CostUSD, sb.CostUSD)
	case took > sb.Duration:
		return fmt.Sprintf("step %s took %s, past the step's time budget of %s", s.Name, took, sb.Duration)
	}
	return x.runOverBudget()
}

// runOverBudget compares the run's use with its budget; its time counts only while it runs.
func (x *driver) runOverBudget() string {
	rb := x.pattern().RunBudget
	took := x.r.elapsed + x.m.cfg.Clock.Now().Sub(x.r.since)
	switch {
	case x.r.used.Tokens > rb.Tokens:
		return fmt.Sprintf("the run used %d tokens, past the run's token budget of %d", x.r.used.Tokens, rb.Tokens)
	case x.r.used.CostUSD > rb.CostUSD:
		return fmt.Sprintf("the run cost $%.4f, past the run's cost budget of $%.4f", x.r.used.CostUSD, rb.CostUSD)
	case took > rb.Duration:
		return fmt.Sprintf("the run took %s, past the run's time budget of %s", took, rb.Duration)
	}
	return ""
}
