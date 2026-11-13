package machine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
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
func (x *driver) record(e Event) error { return x.recordp(&e) }

// recordp is record, filling in the event's seq, time and run.
func (x *driver) recordp(e *Event) error {
	x.m.mu.Lock()
	defer x.m.mu.Unlock()
	return x.recordLocked(e)
}

// recordLocked appends an event; when the append fails the run is interrupted (it can be resumed).
func (x *driver) recordLocked(e *Event) error {
	err := x.m.emitp(x.r, e)
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
	if err := x.recordLocked(&Event{Kind: kind, Refs: x.r.refs, Detail: reason}); err != nil {
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
	if err := x.flush(ctx); err != nil { // `done` only after the last accepted step's commit
		return err
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

// accept makes the step's artifacts and the project's files after it the run's, counts it
// accepted and makes its commit the one to make next (flush). e is the recorded StepAccepted or
// ResultReused; reusedFrom names the run whose result a ResultReused reuses.
func (x *driver) accept(s Step, e Event, reusedFrom string) {
	artifacts, files := e.Artifacts, e.Files
	x.r.commit, x.r.committed = commitOf(x.r, s, e, reusedFrom), false
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
	if err := x.flush(ctx); err != nil { // the last accepted step's commit comes before anything for this one
		return Result{}, rejected, err
	}
	if a, ok := x.m.cfg.Results.Get(key); ok && len(a.Artifacts) == len(s.Outputs) {
		e := Event{Kind: "ResultReused", Step: s.Name, Round: round, Worker: s.Worker, Pattern: x.r.Pattern,
			Inputs: keyInputs, Artifacts: a.Artifacts, ReuseKey: key, Refs: x.r.refs, ChangedLines: slices.Clone(a.Lines),
			Files: slices.Clone(a.Files)}
		if isAgent(s) {
			e.AgentDigest, e.Model = s.AgentDigest, x.m.cfg.Model
		}
		if err := x.recordp(&e); err != nil {
			return Result{}, rejected, err
		}
		x.accept(s, e, a.Run)
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
		if err := x.restore(ctx, s, a, round); err != nil {
			return Result{}, rejected, err
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
		if err := x.recordp(&made); err != nil {
			return res, rejected, "", err
		}
		x.m.cfg.Results.Put(Accepted{Key: at.key, Artifacts: artifacts, Run: x.r.ID, Lines: slices.Clone(res.ChangedLines),
			Files: slices.Clone(res.ProjectFiles)})
		x.accept(s, made, "")
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

// historyRetries is how often a failed restoreStep or commitStep is retried (D7).
const historyRetries = 3

// historyBackoff is the wait before retry n (0-based): exponential backoff with jitter over the
// window 1s<<n, as api/operations.go's backoff, but never 0: half the window plus a random part
// of the other half, in (window/2, window].
func historyBackoff(n int) time.Duration {
	window := time.Second << n
	return window/2 + rand.N(window/2) + 1
}

// historyMinLeft is the least of the run's time budget a History call is made with.
const historyMinLeft = time.Second

// commitOf is the commit of the step a StepAccepted or ResultReused event accepted, made on the
// files r held before it (r.before, not yet replaced).
func commitOf(r *run, s Step, e Event, reusedFrom string) *CommitRequest {
	c := &CommitRequest{Run: r.ID, Step: s, Attempt: e.Attempt, Round: e.Round, Project: r.Project,
		Before: slices.Clone(r.before), ProjectFiles: slices.Clone(e.Files), AcceptedAt: e.At}
	if e.Kind == "ResultReused" {
		c.Attempt, c.ReusedFrom = 0, reusedFrom
		if !s.Reviews() { // the review's findings are not a set of project files
			for i, o := range s.Outputs {
				if i < len(e.Artifacts) {
					c.Outputs = append(c.Outputs, Artifact{Name: o, SHA256: e.Artifacts[i]})
				}
			}
		}
	}
	return c
}

// left is what is left of the run's time budget.
func (x *driver) left() time.Duration {
	return x.pattern().RunBudget.Duration - (x.r.elapsed + x.m.cfg.Clock.Now().Sub(x.r.since))
}

func (x *driver) sleep(ctx context.Context, d time.Duration) error {
	if x.m.cfg.Sleep != nil {
		return x.m.cfg.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// callHistory makes one History call, given what is left of the run's budget, retried after a
// wait with jitter (historyBackoff) up to historyRetries times. When it still fails, or less
// than historyMinLeft is left, the run is interrupted (never handed over) with the reason; the
// error halts the run.
func (x *driver) callHistory(ctx context.Context, step, what string, call func(ctx context.Context, left time.Duration) error) error {
	var err error
	for try := 0; ; try++ {
		left := x.left()
		if left < historyMinLeft {
			return x.interrupt(fmt.Sprintf("%s was not called: less than %s of the run's time budget is left", what, historyMinLeft))
		}
		cctx, cancel := context.WithTimeout(ctx, left)
		err = call(cctx, left)
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return x.interrupt(fmt.Sprintf("the run stopped during step %s", step))
		}
		if try == historyRetries {
			break
		}
		if serr := x.sleep(ctx, historyBackoff(try)); serr != nil {
			return x.interrupt(fmt.Sprintf("the run stopped during step %s", step))
		}
	}
	return x.interrupt(fmt.Sprintf("%s failed %d times; the last: %v", what, historyRetries+1, err))
}

// restore calls restoreStep before an attempt: the project folder is made to hold the files the
// last accepted step left, undoing whatever an earlier attempt wrote (the run's compensation).
func (x *driver) restore(ctx context.Context, s Step, attempt, round int) error {
	h := x.m.cfg.History
	if h == nil {
		return nil
	}
	rq := RestoreRequest{Run: x.r.ID, Step: s, Attempt: attempt, Round: round, Project: x.r.Project,
		Before: slices.Clone(x.r.before)}
	return x.callHistory(ctx, s.Name, fmt.Sprintf("restoreStep for attempt %d of step %s", attempt, s.Name),
		func(ctx context.Context, left time.Duration) error {
			rq.TimeLimit = left
			return h.Restore(ctx, rq)
		})
}

// flush makes the last accepted step's commit unless it was made since the run started driving;
// Resume always makes it again (a repeat is safe).
func (x *driver) flush(ctx context.Context) error {
	h, c := x.m.cfg.History, x.r.commit
	if h == nil || c == nil || x.r.committed {
		return nil
	}
	err := x.callHistory(ctx, c.Step.Name, "commitStep for step "+c.Step.Name,
		func(ctx context.Context, left time.Duration) error {
			rq := *c
			rq.TimeLimit = left
			_, err := h.Commit(ctx, rq)
			return err
		})
	if err == nil {
		x.r.committed = true
	}
	return err
}
