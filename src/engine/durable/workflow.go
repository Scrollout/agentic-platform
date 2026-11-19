package durable

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/agentic-platform/engine/api"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/retry"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// The Run workflow is the step machine's rules (src/engine/machine, data model "Run states"): the
// pattern's steps in order, reuse, attempts, review rounds, budgets, hand-over and outcome. It is
// deterministic: it reads time only through workflow.Now, randomness only through
// workflow.SideEffect (the retry wait's random part), waits only on durable timers and signals,
// and does no I/O; every call is an activity.
//
// A platform stop is an activity ending with a heartbeat timeout. It is not a failure: the run
// records RunInterrupted `the platform stopped` and RunResumed `continued after the platform
// stopped`, then makes the call again; a worker call or a check makes its attempt again with the
// same number, after restoring the project. The time budget sums the running times the
// activities report, so downtime and waits never count.

// Details of the events a stop and a wait record (data model, "Run record").
const (
	detailStopped   = "the platform stopped"
	detailContinued = "continued after the platform stopped"
	detailNoWorkers = "the step workers are not running"
)

// noBlockingFindingOpen is the review check whose failure means a round ended with open findings,
// not a rejected attempt.
const noBlockingFindingOpen = "NoBlockingFindingOpen"

// Every activity's options (research R3): Temporal never retries an activity (the declared rule
// does, research R5); StartToClose is the call's own time limit plus callSlack; a stopped engine
// is found by the heartbeat timeout.
const (
	callSlack        = 30 * time.Second
	heartbeatTimeout = 3 * heartbeatEvery
	minCallLeft      = time.Second // the least of the run's time budget a project-worker call is made with
)

// workflowVersion is the Run workflow's code version: a change to the commands a run issues
// bumps it behind workflow.GetVersion, so runs in flight replay on the code that started them.
const workflowVersion = 1

var hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// How a part of the run returns early. errAgain: the developer resumed; the run continues from the
// step it is in. errRepeat: the same attempt again (a stop, or the workers are back). errEnded:
// the run has ended.
var (
	errAgain  = errors.New("durable: the run continues from its step")
	errRepeat = errors.New("durable: the attempt is made again")
	errEnded  = errors.New("durable: the run has ended")
)

type verdict int

const (
	accepted verdict = iota
	rejected
	workerFailed
	findingsOpen
	overBudget
)

// run is a run's workflow state: what Temporal keeps of a run (data model, "Execution state").
type run struct {
	in      RunInput
	ref     string // the pattern's <id>@<version>
	rule    retry.Rule
	ctx     workflow.Context // cancelled by the signal cancel or the workflow's cancellation
	root    workflow.Context // not cancelled: records the cancellation
	outcome machine.Outcome  // set when the run has ended

	seq       int
	artifacts map[string]string // input or output name to hash
	used      machine.Usage
	elapsed   time.Duration // the running time the activities reported
	pos       int           // index of the step being run
	round     int           // review round, 0 before the first
	fixing    bool          // the fix step runs before the next review round
	open      []Finding     // blocking findings of the last review round
	changed   []machine.Line
	before    []machine.File         // the project's files after the last accepted step
	commit    *machine.CommitRequest // the last accepted step's commit, nil before the first
	committed bool
}

// Run runs one run to its outcome. A run that waits (a hand-over, an interruption, the step
// workers) keeps the workflow open until a signal or its expiry ends or continues it. An error is
// a defect: the run's record could not be written as the run holds it.
func Run(ctx workflow.Context, in RunInput) (RunResult, error) {
	_ = workflow.GetVersion(ctx, "run", workflow.DefaultVersion, workflowVersion)
	r := &run{in: in, ref: in.Pattern.ID + "@" + in.Pattern.Version, rule: api.RetryRule(), artifacts: map[string]string{}}
	r.root, _ = workflow.NewDisconnectedContext(ctx)
	var cancel workflow.CancelFunc
	r.ctx, cancel = workflow.WithCancel(ctx)
	workflow.Go(ctx, func(g workflow.Context) {
		workflow.GetSignalChannel(g, SignalCancel).Receive(g, nil)
		cancel()
	})
	if len(in.Refs) == 0 {
		return RunResult{}, temporal.NewNonRetryableApplicationError("a run needs the slice's refs", "InvalidRun", nil)
	}
	names := in.Pattern.SliceInputs()
	hashes := make([]string, len(names))
	for i, n := range names {
		if !hashRE.MatchString(in.Inputs[n]) {
			return RunResult{}, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("input %s is missing or not a SHA-256 hash", n), "InvalidRun", nil)
		}
		hashes[i] = in.Inputs[n]
		r.artifacts[n] = hashes[i]
	}
	err := r.record(&EventRecord{Event: machine.Event{Kind: "RunStarted", Slice: in.Slice, Pattern: r.ref, Inputs: hashes,
		Refs: in.Refs}, Project: in.Project, Operation: in.Operation, ReplayOf: in.ReplayOf})
	for err == nil || errors.Is(err, errAgain) {
		err = r.steps()
	}
	if r.outcome == "" && r.ctx.Err() != nil { // cancelled
		err = r.end(r.root, "RunCancelled", machine.OutcomeCancelled, "the run was cancelled", nil)
	}
	if r.outcome != "" {
		return RunResult{Outcome: r.outcome}, nil
	}
	return RunResult{}, err
}

func (r *run) pattern() machine.Pattern { return r.in.Pattern }

func isAgent(s machine.Step) bool { return strings.HasPrefix(s.Worker, "agent:") }

// steps runs the pattern's steps from the one the run is in, then ends the run done.
func (r *run) steps() error {
	steps := r.pattern().Steps
	for r.pos < len(steps) {
		s := steps[r.pos]
		var err error
		if s.Reviews() && r.pos > 0 {
			err = r.review(steps[r.pos-1], s)
		} else {
			_, _, err = r.step(s, 0, nil, nil)
		}
		if err != nil {
			return err
		}
		r.pos++
		r.round, r.fixing, r.open, r.changed = 0, false, nil, nil
	}
	if err := r.flush(); err != nil { // done only after the last accepted step's commit
		return err
	}
	if err := r.end(r.ctx, "RunCompleted", machine.OutcomeDone, "", nil); err != nil {
		return err
	}
	return errEnded
}

// review runs the review step in rounds; after a round with open blocking findings the fix step
// (the step before the review) runs again with them, and the next round gets only those findings
// and the lines the fix changed (research D11).
func (r *run) review(fix, review machine.Step) error {
	rounds := max(r.pattern().ReviewRounds, 1)
	if r.round == 0 {
		r.round = 1
	}
	for {
		if r.fixing && r.round >= rounds { // resumed after the last round was left open
			return r.handOverOpen()
		}
		if r.fixing { // the fix carries the round whose findings it fixes
			fixed, _, err := r.step(fix, r.round, r.open, nil)
			if err != nil {
				return err
			}
			r.changed, r.fixing = fixed.ChangedLines, false
			r.round++
		}
		res, v, err := r.step(review, r.round, r.open, r.changed)
		if err != nil {
			return err
		}
		if v == accepted {
			return nil
		}
		r.open = nil
		for _, f := range res.Findings {
			if f.Blocking {
				r.open = append(r.open, f)
			}
		}
		if r.round >= rounds {
			return r.handOverOpen()
		}
		r.fixing = true
	}
}

// handOverOpen hands the run over after the last review round was left open, once that round's
// commit is made. The detail's list of the open findings is added by RecordEvent.
func (r *run) handOverOpen() error {
	if err := r.flush(); err != nil {
		return err
	}
	return r.handOver(fmt.Sprintf("review found blocking findings still open after %d rounds", r.round), slices.Clone(r.open))
}

// openDetail is the detail of the event that leaves review round n open, live or reused.
func openDetail(round int) string {
	return fmt.Sprintf("review round %d left blocking findings open", round)
}

// stepKey gives the lookup of a step's reuse key (feature 001's key, research D8): the key's
// parts, and the open findings and changed lines a fix or a later review round is given. Without
// open findings the key, and the inputs it covers, are known here; with them the key covers the
// findings' text, which only the store holds, so LookupReuse computes it (keyed). The review
// step's key also covers its round (research D36); no other step's does.
func (r *run) stepKey(s machine.Step, round int, inputs []string, open []Finding, changed []machine.Line) (l ReuseLookup, keyInputs []string, key string) {
	model := ""
	if isAgent(s) {
		model = r.in.Model
	}
	if !s.Reviews() {
		round = 0
	}
	parts := machine.KeyParts{PatternVersion: r.ref, Step: s.Name, Inputs: slices.Clone(inputs), AgentDigest: s.AgentDigest,
		Model: model, Round: round}
	l = ReuseLookup{Slice: r.in.Slice, Parts: parts, Open: slices.Clone(open), Changed: slices.Clone(changed)}
	if len(open) > 0 {
		return l, slices.Clone(inputs), ""
	}
	keyInputs, key = machine.KeyOf(parts, nil, changed)
	l.Key = key
	return l, keyInputs, key
}

// step reuses a kept result for the step's key, or runs its attempts until one is accepted (or,
// in review, a round ends with open findings). Any other end hands the run over or interrupts it,
// returned as an error.
func (r *run) step(s machine.Step, round int, open []Finding, changed []machine.Line) (WorkerResult, verdict, error) {
	inputs := make([]string, len(s.Inputs))
	for i, name := range s.Inputs {
		h, ok := r.artifacts[name]
		if !ok {
			return WorkerResult{}, rejected, r.handOver(fmt.Sprintf("step %s has no input %s", s.Name, name), nil)
		}
		inputs[i] = h
	}
	l, keyInputs, key := r.stepKey(s, round, inputs, open, changed)
	if err := r.flush(); err != nil { // the last accepted step's commit comes before anything for this one
		return WorkerResult{}, rejected, err
	}
	a, err := r.lookup(l)
	if err != nil {
		return WorkerResult{}, rejected, err
	}
	if key == "" && a.Key != "" { // the key that covers the open findings' text
		keyInputs, key = a.Inputs, a.Key
	}
	found := a.Found && len(a.Artifacts) == len(s.Outputs)
	if found && a.Open && s.Reviews() { // a review round left open in an earlier run (research D36)
		return r.reuseOpenRound(s, round, keyInputs, key, a)
	}
	if found && !a.Open {
		e := &EventRecord{Event: machine.Event{Kind: "ResultReused", Step: s.Name, Round: round, Worker: s.Worker,
			Pattern: r.ref, Inputs: keyInputs, Artifacts: a.Artifacts, ReuseKey: key, Refs: r.in.Refs,
			ChangedLines: slices.Clone(a.Lines)}, Files: slices.Clone(a.Files)}
		if isAgent(s) {
			e.Event.AgentDigest, e.Event.Model = s.AgentDigest, r.in.Model
		}
		if err := r.record(e); err != nil {
			return WorkerResult{}, rejected, err
		}
		r.accept(s, e.Event, e.Files, a.Run)
		out := map[string]string{}
		for i, name := range s.Outputs {
			out[name] = a.Artifacts[i]
		}
		return WorkerResult{Outputs: out, ChangedLines: slices.Clone(a.Lines), ProjectFiles: slices.Clone(a.Files)}, accepted, nil
	}

	if err := r.record(&EventRecord{Event: machine.Event{Kind: "StepStarted", Step: s.Name, Round: round, Refs: r.in.Refs}}); err != nil {
		return WorkerResult{}, rejected, err
	}
	attempts := max(r.pattern().Attempts, 1)
	var last string
	failures := 0
	for n := 1; n <= attempts; {
		if why := r.runOverBudget(); why != "" {
			return WorkerResult{}, overBudget, r.handOver(why, nil)
		}
		if err := r.restore(s, n, round); err != nil {
			return WorkerResult{}, rejected, err
		}
		res, v, why, err := r.attempt(s, attemptOf{n, round, open, changed, inputs, keyInputs, key})
		if errors.Is(err, errRepeat) {
			continue
		}
		if err != nil {
			return res, v, err
		}
		switch v {
		case accepted, findingsOpen:
			return res, v, nil
		case overBudget:
			return res, v, r.handOver(why, nil)
		case workerFailed:
			failures++
		}
		last = why
		n++
	}
	if failures == attempts {
		return WorkerResult{}, workerFailed, r.interrupt(fmt.Sprintf(
			"the worker of step %s failed on all %d attempts; the last: %s", s.Name, attempts, last))
	}
	return WorkerResult{}, rejected, r.handOver(fmt.Sprintf("step %s was not accepted after %d attempts; the last: %s",
		s.Name, attempts, last), nil)
}

// reuseOpenRound records a review round left open in an earlier run as reused (research D36): its
// kept findings, in order, then a ResultReused with open_round and the kept NoBlockingFindingOpen
// result. Nothing runs and the round is not accepted.
func (r *run) reuseOpenRound(s machine.Step, round int, keyInputs []string, key string, a Reused) (WorkerResult, verdict, error) {
	for _, f := range a.Findings {
		if err := r.record(&EventRecord{Event: machine.Event{Kind: "FindingRecorded", Step: s.Name, Round: round,
			Refs: []string{f.Rule}}, Finding: &f}); err != nil {
			return WorkerResult{}, rejected, err
		}
	}
	e := &EventRecord{Event: machine.Event{Kind: "ResultReused", Step: s.Name, Round: round, OpenRound: true, Worker: s.Worker,
		Pattern: r.ref, Inputs: keyInputs, Artifacts: slices.Clone(a.Artifacts), ReuseKey: key, Refs: r.in.Refs,
		Detail: openDetail(round)}, Files: slices.Clone(a.Files)}
	if a.Check != nil {
		c := *a.Check
		e.Event.Check = &c
	}
	if isAgent(s) {
		e.Event.AgentDigest, e.Event.Model = s.AgentDigest, r.in.Model
	}
	if err := r.record(e); err != nil {
		return WorkerResult{}, rejected, err
	}
	if len(s.Writable) > 0 { // as a live open round: a review that writes nothing leaves no commit
		r.keepOpenRound(s, e.Event, e.Files, a.Run)
	}
	out := map[string]string{}
	for i, name := range s.Outputs {
		out[name] = a.Artifacts[i]
	}
	return WorkerResult{Outputs: out, Findings: slices.Clone(a.Findings), ProjectFiles: slices.Clone(a.Files)}, findingsOpen, nil
}

// accept makes the step's artifacts and the project's files after it the run's and its commit
// the one to make next (flush). e is the recorded StepAccepted or ResultReused.
func (r *run) accept(s machine.Step, e machine.Event, files []machine.File, reusedFrom string) {
	r.commit, r.committed = r.commitOf(s, e, files, reusedFrom), false
	r.before = slices.Clone(files)
	for i, name := range s.Outputs {
		r.artifacts[name] = e.Artifacts[i]
	}
}

// keepOpenRound makes the files a review round with open findings left the run's, and their
// commit the one to make next. The round is not accepted: it sets no artifact of the run.
func (r *run) keepOpenRound(s machine.Step, e machine.Event, files []machine.File, reusedFrom string) {
	r.commit, r.committed = r.commitOf(s, e, files, reusedFrom), false
	r.before = slices.Clone(files)
}

// commitOf is the commit of the step a StepAccepted or ResultReused event accepted, or of the
// review round a StepRejected left open, made on the files the run held before it.
func (r *run) commitOf(s machine.Step, e machine.Event, files []machine.File, reusedFrom string) *machine.CommitRequest {
	c := &machine.CommitRequest{Run: r.in.Run, Step: s, Attempt: e.Attempt, Round: e.Round, Project: r.in.Project,
		Before: slices.Clone(r.before), ProjectFiles: slices.Clone(files), AcceptedAt: e.At}
	if e.Kind == "StepRejected" || e.OpenRound {
		c.OpenRound = true
	}
	if e.Kind == "ResultReused" {
		c.Attempt, c.ReusedFrom = 0, reusedFrom
		for i, o := range s.Outputs {
			if s.Reviews() && o == "findings" { // the review's findings are not a set of project files
				continue
			}
			if i < len(e.Artifacts) {
				c.Outputs = append(c.Outputs, machine.Artifact{Name: o, SHA256: e.Artifacts[i]})
			}
		}
	}
	return c
}

type attemptOf struct {
	n, round          int
	open              []Finding
	changed           []machine.Line
	inputs, keyInputs []string
	key               string
}

// attempt runs one attempt and its checks and records them. The reason says why it was not
// accepted. errRepeat: the attempt is to be made again with the same number.
func (r *run) attempt(s machine.Step, at attemptOf) (WorkerResult, verdict, string, error) {
	limit := r.callLimit()
	var res WorkerResult
	err := r.exec(r.ctx, CallWorker, limit, WorkerCall{Run: r.in.Run, Slice: r.in.Slice, Project: r.in.Project, Step: s,
		Attempt: at.n, Round: at.round, Inputs: slices.Clone(at.inputs), Open: slices.Clone(at.open),
		ChangedLines: slices.Clone(at.changed), Before: slices.Clone(r.before), Artifacts: r.artifactsCopy(), TimeLimit: limit}, &res)
	switch {
	case err == nil:
	case r.ctx.Err() != nil:
		return res, rejected, "", errEnded
	case isStop(err):
		return res, rejected, "", r.stopPair(errRepeat)
	case errType(err) == WorkersNotReady:
		return res, rejected, "", r.waitForWorkers()
	}
	use := machine.Usage{Tokens: max(res.Usage.Tokens, 0), CostUSD: max(res.Usage.CostUSD, 0)}
	r.used.Tokens += use.Tokens
	r.used.CostUSD += use.CostUSD
	r.elapsed += max(res.Took, 0)
	budget := r.overBudget(s, use, res.Took)
	base := machine.Event{Step: s.Name, Attempt: at.n, Round: at.round, Refs: r.in.Refs}
	// the attempt's use goes on its AttemptMade, or on its StepRejected when it made none
	used := base
	used.Usage = &use

	if err != nil {
		why := "the worker failed: " + errText(err)
		if err := r.reject(used, why); err != nil {
			return res, rejected, why, err
		}
		if budget != "" {
			return res, overBudget, budget, nil
		}
		return res, workerFailed, why, nil
	}
	artifacts, perr := r.parse(s, res)
	if perr != nil {
		why := "the worker's result was refused: " + perr.Error()
		if budget != "" {
			return res, overBudget, budget, r.reject(used, why)
		}
		return res, rejected, why, r.reject(used, why)
	}

	made := used
	made.Kind, made.Worker, made.Model, made.Pattern = "AttemptMade", s.Worker, res.Model, r.ref
	if isAgent(s) {
		made.AgentDigest = s.AgentDigest
	}
	made.Inputs, made.Artifacts, made.ReuseKey = at.keyInputs, artifacts, at.key
	if err := r.record(&EventRecord{Event: made}); err != nil {
		return res, rejected, "", err
	}
	if budget != "" {
		return res, overBudget, budget, r.reject(base, budget)
	}

	var failed []string
	var openCheck *machine.EventCheck // the recorded NoBlockingFindingOpen result
	var confirmed []int               // NoBlockingFindingOpen's confirmed findings (ADR 0042)
	for _, name := range s.Checks {
		var cr CheckResult
		cerr := r.exec(r.ctx, RunCheck, limit, CheckCall{Run: r.in.Run, Slice: r.in.Slice, Project: r.in.Project, Step: s,
			Attempt: at.n, Round: at.round, Name: name, Inputs: slices.Clone(at.inputs), Model: res.Model, Outputs: res.Outputs,
			Findings: res.Findings, Changed: res.ChangedLines, Before: res.Before, ProjectFiles: res.ProjectFiles,
			Open: slices.Clone(at.open), ChangedLines: slices.Clone(at.changed), TimeLimit: limit}, &cr)
		switch {
		case cerr == nil:
			r.elapsed += max(cr.Took, 0)
		case r.ctx.Err() != nil:
			return res, rejected, "", errEnded
		case isStop(cerr): // the check runs again, in the attempt made again
			return res, rejected, "", r.stopPair(errRepeat)
		default:
			cr = CheckResult{Passed: false, Reason: "the check could not run: " + errText(cerr)}
		}
		e := base
		e.Kind, e.Check = "CheckPassed", &machine.EventCheck{Name: name, Passed: cr.Passed, Reason: cr.Reason}
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
		if name == noBlockingFindingOpen {
			c := *e.Check
			openCheck = &c
			if cerr == nil {
				confirmed = cr.Confirmed
			}
		}
		if err := r.record(&EventRecord{Event: e}); err != nil {
			return res, rejected, "", err
		}
	}

	if s.Reviews() { // only a finding the check confirmed is blocking (ADR 0042)
		res.Findings = recordedFindings(res.Findings, confirmed)
	}
	open := s.Reviews() && slices.Equal(failed, []string{noBlockingFindingOpen}) && hasBlocking(res.Findings)
	if len(failed) == 0 || open {
		for _, f := range res.Findings {
			e := base
			e.Kind, e.Refs = "FindingRecorded", []string{f.Rule}
			if err := r.record(&EventRecord{Event: e, Finding: &f}); err != nil {
				return res, rejected, "", err
			}
		}
	}
	switch {
	case len(failed) == 0:
		made.Kind, made.Usage, made.ChangedLines = "StepAccepted", nil, slices.Clone(res.ChangedLines)
		rec := &EventRecord{Event: made, Files: slices.Clone(res.ProjectFiles)}
		if err := r.record(rec); err != nil {
			return res, rejected, "", err
		}
		if err := r.put(ReusePut{Key: at.key, Artifacts: artifacts, Run: r.in.Run, Lines: slices.Clone(res.ChangedLines),
			Files: slices.Clone(res.ProjectFiles)}); err != nil {
			return res, rejected, "", err
		}
		r.accept(s, rec.Event, rec.Files, "")
		return res, accepted, "", nil
	case open:
		why := openDetail(at.round)
		rej := base
		rej.Kind, rej.Detail, rej.OpenRound = "StepRejected", why, true
		rej.ReuseKey, rej.Artifacts = at.key, slices.Clone(artifacts)
		rec := &EventRecord{Event: rej}
		// A review that writes witnesses: the open round's files are committed like an accepted
		// step's, so the fix and the next round start from them.
		kept := slices.Clone(r.before) // a review that writes nothing leaves the files it started from
		if len(s.Writable) > 0 {
			rec.Files = slices.Clone(res.ProjectFiles)
			kept = slices.Clone(res.ProjectFiles)
		}
		if err := r.record(rec); err != nil {
			return res, findingsOpen, why, err
		}
		// The round's result is kept, marked open, so a later run of the same input reuses it.
		if err := r.put(ReusePut{Key: at.key, Artifacts: slices.Clone(artifacts), Run: r.in.Run, Files: kept, Open: true,
			Findings: slices.Clone(res.Findings), Check: openCheck}); err != nil {
			return res, findingsOpen, why, err
		}
		if rec.Files != nil {
			r.keepOpenRound(s, rec.Event, rec.Files, "")
		}
		return res, findingsOpen, why, nil
	}
	why := "failed checks: " + strings.Join(failed, ", ")
	return res, rejected, why, r.reject(base, why)
}

// recordedFindings is the findings as the record holds them: a copy in which a finding is blocking
// only when the agent claimed it blocking and confirmed names its position.
func recordedFindings(in []Finding, confirmed []int) []Finding {
	out := slices.Clone(in)
	for i := range out {
		out[i].Blocking = out[i].Blocking && slices.Contains(confirmed, i)
	}
	return out
}

func hasBlocking(fs []Finding) bool {
	return slices.ContainsFunc(fs, func(f Finding) bool { return f.Blocking })
}

func (r *run) reject(base machine.Event, why string) error {
	base.Kind, base.Detail = "StepRejected", why
	return r.record(&EventRecord{Event: base})
}

// parse rejects a result the run record could not hold; a worker's output is never trusted.
func (r *run) parse(s machine.Step, res WorkerResult) ([]string, error) {
	if isAgent(s) {
		switch want := r.in.Model; {
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
	for _, f := range res.Findings {
		if !hashRE.MatchString(f.SHA256) {
			return nil, fmt.Errorf("finding %s is not stored by its SHA-256 hash", f.Rule)
		}
	}
	return out, nil
}

// overBudget names the budget an attempt took the step or the run past, or is empty.
func (r *run) overBudget(s machine.Step, u machine.Usage, took time.Duration) string {
	sb := r.pattern().StepBudget
	switch {
	case u.Tokens > sb.Tokens:
		return fmt.Sprintf("step %s used %d tokens, past the step's token budget of %d", s.Name, u.Tokens, sb.Tokens)
	case u.CostUSD > sb.CostUSD:
		return fmt.Sprintf("step %s cost $%.4f, past the step's cost budget of $%.4f", s.Name, u.CostUSD, sb.CostUSD)
	case took > sb.Duration:
		return fmt.Sprintf("step %s took %s, past the step's time budget of %s", s.Name, took, sb.Duration)
	}
	return r.runOverBudget()
}

// runOverBudget compares the run's use with its budget; its time is the activities' running time.
func (r *run) runOverBudget() string {
	rb := r.pattern().RunBudget
	switch {
	case r.used.Tokens > rb.Tokens:
		return fmt.Sprintf("the run used %d tokens, past the run's token budget of %d", r.used.Tokens, rb.Tokens)
	case r.used.CostUSD > rb.CostUSD:
		return fmt.Sprintf("the run cost $%.4f, past the run's cost budget of $%.4f", r.used.CostUSD, rb.CostUSD)
	case r.elapsed > rb.Duration:
		return fmt.Sprintf("the run took %s, past the run's time budget of %s", r.elapsed, rb.Duration)
	}
	return ""
}

// left is what is left of the run's time budget.
func (r *run) left() time.Duration { return r.pattern().RunBudget.Duration - r.elapsed }

// callLimit is a worker call's or a check's time limit: what is left of the run's time budget, at
// most the step's, at least a second.
func (r *run) callLimit() time.Duration {
	limit := r.left()
	if sb := r.pattern().StepBudget.Duration; sb > 0 && sb < limit {
		limit = sb
	}
	return max(limit, time.Second)
}

func (r *run) artifactsCopy() map[string]string {
	out := make(map[string]string, len(r.artifacts))
	for k, v := range r.artifacts {
		out[k] = v
	}
	return out
}

// restore calls restoreStep before an attempt: the project folder is made to hold the files the
// last accepted step left, undoing whatever an earlier attempt wrote (the run's compensation).
func (r *run) restore(s machine.Step, attempt, round int) error {
	rq := machine.RestoreRequest{Run: r.in.Run, Step: s, Attempt: attempt, Round: round, Project: r.in.Project,
		Before: slices.Clone(r.before)}
	return r.callProject(RestoreProject, fmt.Sprintf("restoreStep for attempt %d of step %s", attempt, s.Name),
		func(left time.Duration) any { rq.TimeLimit = left; return rq })
}

// flush makes the last accepted step's commit unless it was made.
func (r *run) flush() error {
	if r.commit == nil || r.committed {
		return nil
	}
	rq := *r.commit
	err := r.callProject(CommitStep, "commitStep for step "+rq.Step.Name,
		func(left time.Duration) any { rq.TimeLimit = left; return rq })
	if err == nil {
		r.committed = true
	}
	return err
}

// callProject makes one project-worker call with what is left of the run's time budget, retried
// by the declared rule. When it still fails, or less than minCallLeft is left, the run is
// interrupted (never handed over) and a resume continues the step, making the call again.
func (r *run) callProject(name, what string, in func(left time.Duration) any) error {
	left := r.left()
	if left < minCallLeft {
		return r.interrupt(fmt.Sprintf("%s was not called: less than %s of the run's time budget is left", what, minCallLeft))
	}
	var ran Ran
	err := r.retried(name, left, in(left), &ran)
	var f *callFailed
	if errors.As(err, &f) {
		return r.interrupt(fmt.Sprintf("%s failed %d times; the last: %s", what, r.rule.Retries+1, errText(f.err)))
	}
	if err == nil {
		r.elapsed += max(ran.Took, 0)
	}
	return err
}

// lookup finds the result kept under a step's key; a store that cannot be read finds none (the
// step runs, with no key when its key covers open findings).
func (r *run) lookup(l ReuseLookup) (Reused, error) {
	var a Reused
	err := r.retried(LookupReuse, 0, l, &a)
	var f *callFailed
	if errors.As(err, &f) {
		return Reused{}, nil
	}
	return a, err
}

// put keeps a result under its key; a store that cannot be written, or a step without a key,
// keeps none (nothing is reused).
func (r *run) put(p ReusePut) error {
	if p.Key == "" {
		return nil
	}
	err := r.retried(PutResult, 0, p, nil)
	var f *callFailed
	if errors.As(err, &f) {
		return nil
	}
	return err
}

// callFailed is a call that failed on its first try and on every retry.
type callFailed struct{ err error }

func (c *callFailed) Error() string { return c.err.Error() }

// retried makes a call, and again after a stop, and retries a failed call by the declared rule
// (src/retry, the engine contract's x-limits) after a durable sleep. When every try failed it
// returns *callFailed.
func (r *run) retried(name string, limit time.Duration, in, out any) error {
	for try := 0; ; {
		err := r.exec(r.ctx, name, limit, in, out)
		switch {
		case err == nil:
			return nil
		case r.ctx.Err() != nil:
			return errEnded
		case isStop(err):
			if err := r.stopPair(nil); err != nil {
				return err
			}
			continue
		case try == r.rule.Retries:
			return &callFailed{err}
		}
		if err := r.sleep(r.rule.Wait(try, r.random)); err != nil {
			return err
		}
		try++
	}
}

// exec runs one activity with its options: no retry by Temporal, StartToClose of its time limit
// plus callSlack, and the heartbeat timeout.
func (r *run) exec(ctx workflow.Context, name string, limit time.Duration, in, out any) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: limit + callSlack,
		HeartbeatTimeout: heartbeatTimeout, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1}})
	return workflow.ExecuteActivity(ctx, name, in).Get(ctx, out)
}

// sleep waits d on a durable timer; a cancelled run ends.
func (r *run) sleep(d time.Duration) error {
	if err := workflow.Sleep(r.ctx, d); err != nil {
		return errEnded
	}
	return nil
}

// random is the retry wait's random part, in [0, 1): the workflow's only side effect.
func (r *run) random() float64 {
	var f float64
	_ = workflow.SideEffect(r.ctx, func(workflow.Context) any { return rand.Float64() }).Get(&f)
	return f
}

// isStop reports whether an activity ended because the platform stopped: a heartbeat timeout.
func isStop(err error) bool {
	var t *temporal.TimeoutError
	return errors.As(err, &t) && t.TimeoutType() == enumspb.TIMEOUT_TYPE_HEARTBEAT
}

// errType is an activity error's application error type, or "".
func errType(err error) string {
	var a *temporal.ApplicationError
	if errors.As(err, &a) {
		return a.Type()
	}
	return ""
}

// errText is what an activity's error says, without Temporal's wrapping.
func errText(err error) string {
	var a *temporal.ApplicationError
	if !errors.As(err, &a) {
		return err.Error()
	}
	msg := a.Message()
	if c := errors.Unwrap(a); c != nil {
		msg += ": " + errText(c)
	}
	return msg
}

// record appends one event to the run's record with the next seq, stamped now. A stop during the
// write writes it again (the store accepts the same event once per seq), then records the stop. A
// cancellation during the write finishes it on the run's uncancellable context, since the write
// may already have landed and the next event needs the next seq, then ends the run. A failed write
// is retried by the declared rule; a seq held with other content, or a write that keeps failing, is
// a defect and ends the workflow with an error.
func (r *run) record(e *EventRecord) error { return r.recordOn(r.ctx, e) }

func (r *run) recordOn(ctx workflow.Context, e *EventRecord) error {
	e.Slice = r.in.Slice
	e.Event.Seq, e.Event.At, e.Event.Run = r.seq+1, workflow.Now(ctx), r.in.Run
	stopped, cancelled := false, false
	for try := 0; ; {
		err := r.exec(ctx, RecordEvent, 0, e, nil)
		if err == nil {
			break
		}
		switch {
		case ctx.Err() != nil:
			ctx, cancelled = r.root, true
			continue
		case isStop(err):
			stopped = true
			continue
		case errType(err) == SeqTaken:
			return temporal.NewNonRetryableApplicationError(fmt.Sprintf("run %s: event %d (%s) is not the one the record holds",
				r.in.Run, e.Event.Seq, e.Event.Kind), SeqTaken, err)
		case try == r.rule.Retries:
			return temporal.NewNonRetryableApplicationError(fmt.Sprintf("run %s: the run record could not be written: %s",
				r.in.Run, errText(err)), "RecordFailed", err)
		}
		if err := workflow.Sleep(ctx, r.rule.Wait(try, r.random)); err != nil {
			ctx, cancelled = r.root, true
		}
		try++
	}
	r.seq++
	if cancelled {
		return errEnded
	}
	if stopped {
		return r.stopPair(nil)
	}
	return nil
}

// stopPair records that the platform stopped and the run continued by itself, and returns then.
func (r *run) stopPair(then error) error {
	if err := r.record(&EventRecord{Event: machine.Event{Kind: "RunInterrupted", Refs: r.in.Refs, Detail: detailStopped}}); err != nil {
		return err
	}
	if err := r.record(&EventRecord{Event: machine.Event{Kind: "RunResumed", Refs: r.in.Refs, Detail: detailContinued}}); err != nil {
		return err
	}
	return then
}

// end records the run's terminal event and ends it.
func (r *run) end(ctx workflow.Context, kind string, o machine.Outcome, detail string, open []Finding) error {
	if err := r.recordOn(ctx, &EventRecord{Event: machine.Event{Kind: kind, Refs: r.in.Refs, Detail: detail}, Open: open}); err != nil {
		return err
	}
	r.outcome = o
	return nil
}

func (r *run) handOver(reason string, open []Finding) error {
	return r.pause("RunHandedOver", reason, open, SignalResume, SignalAcceptHandOver)
}

func (r *run) interrupt(reason string) error {
	return r.pause("RunInterrupted", reason, nil, SignalResume)
}

// waitForWorkers interrupts the run until the step workers are registered again (or the developer
// resumes it); then the same attempt is made again.
func (r *run) waitForWorkers() error {
	err := r.pause("RunInterrupted", detailNoWorkers, nil, SignalWorkersReady, SignalResume)
	if errors.Is(err, errAgain) {
		return errRepeat
	}
	return err
}

// pause records kind with the reason and waits for one of the signals, the run's cancellation, or
// HandOverTTL after the event, when the run expires. A resume (or workers-ready) records RunResumed
// and returns errAgain; accept-hand-over ends the run handed over. open are the findings the
// reason lists.
func (r *run) pause(kind, reason string, open []Finding, signals ...string) error {
	e := &EventRecord{Event: machine.Event{Kind: kind, Refs: r.in.Refs, Detail: reason}, Open: open}
	if err := r.record(e); err != nil {
		return err
	}
	tctx, stop := workflow.WithCancel(r.ctx)
	defer stop()
	got := ""
	sel := workflow.NewSelector(r.ctx)
	for _, name := range signals {
		sel.AddReceive(workflow.GetSignalChannel(r.ctx, name), func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(r.ctx, nil)
			got = name
		})
	}
	sel.AddFuture(workflow.NewTimer(tctx, e.Event.At.Add(machine.HandOverTTL).Sub(workflow.Now(r.ctx))), func(workflow.Future) {
		got = "expired"
	})
	sel.AddReceive(r.ctx.Done(), func(workflow.ReceiveChannel, bool) {})
	sel.Select(r.ctx)
	switch {
	case r.ctx.Err() != nil:
		return errEnded
	case got == "expired":
		why := "the run waited 14 days for the developer"
		if reason != "" {
			why += "; it stopped because " + reason
		}
		if err := r.end(r.ctx, "RunExpired", machine.OutcomeExpired, why, open); err != nil {
			return err
		}
		return errEnded
	case got == SignalAcceptHandOver:
		if err := r.end(r.ctx, "HandOverAccepted", machine.OutcomeHandedOver, reason, open); err != nil {
			return err
		}
		return errEnded
	}
	if err := r.record(&EventRecord{Event: machine.Event{Kind: "RunResumed", Refs: r.in.Refs}}); err != nil {
		return err
	}
	return errAgain
}
