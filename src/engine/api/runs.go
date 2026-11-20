package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/machine"
	sl "github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
)

// RunStore is what the run operations need from the run store; *store.Store has it. A
// Config.Store without it holds no runs: the run lists are empty, a run is never found, and a
// start that passes every refusal is WorkersNotReady.
type RunStore interface {
	// Append records an event of a run of the slice durably and returns it with its seq.
	Append(sliceID string, e store.Event) (store.Event, error)
	// RecordEvent records an event whose seq the caller sets, once per seq: the same event again
	// writes nothing, other content under a held seq is an error wrapping store.ErrSeqTaken.
	RecordEvent(sliceID string, e store.Event) (store.Event, error)
	// Events returns a run's events in seq order; none for an unknown run.
	Events(runID string) ([]store.Event, error)
	// Artifact returns the bytes stored under a hash, or an error wrapping store.ErrNotFound.
	Artifact(hash string) ([]byte, error)
	// Runs lists every run the store holds with its slice, lowest run number first.
	Runs() []store.RunEntry
	// NextRunID gives out the next run ID once the sequence that gave it is durable.
	NextRunID() (string, error)
}

// Lists that issue page tokens for runs. A run's events and a comparison are lists of their own,
// named with their runs, so a token of one never reads another.
const (
	runsList       = "runs"
	eventsPageSize = 100
)

func eventsList(run string) string            { return "events:" + run }
func comparisonList(run, other string) string { return "comparison:" + run + ":" + other }

// followPoll is how often a run's operation reads the record again when no event it was told of
// arrives (the record may be written by a RunRecord the handler was not given).
const followPoll = 2 * time.Second

// recordWait is the longest a start waits for the run's RunStarted to be recorded before it is
// answered; the run is started either way (its workflow records it).
const recordWait = 10 * time.Second

// runInfo is what the run record keeps about a run beyond the machine's RunStarted event: the
// contract's Run fields no event carries. It is written in the same line as RunStarted, so a run
// is never recorded without it.
type runInfo struct {
	Project   string `json:"project"`
	Operation string `json:"operation"`
	ReplayOf  string `json:"replay_of,omitempty"`
}

// storedEvent is a store.Event's Data: the event in its run-event.schema.json form, on
// RunStarted the run's info, and on StepAccepted and ResultReused the project's files after the
// step (machine.Event.Files, which the contract's event does not carry).
type storedEvent struct {
	Event machine.Event  `json:"event"`
	Info  *runInfo       `json:"info,omitempty"`
	Files []machine.File `json:"files,omitempty"`
}

// RunRecord is the run record as the runs' workflows write it, and the accepted results their
// steps reuse: the engine's API reads both, the durable worker's activities write both
// (durable.Events, durable.Activities.Results). It is safe for concurrent use.
type RunRecord struct {
	st      RunStore
	results *machine.Results

	mu      sync.Mutex
	changed map[string]chan struct{} // run -> closed at its next recorded event
}

// NewRunRecord returns the record of the store's runs. Its accepted results are reloaded from the
// record, the first under a key kept: every result accepted and every review round left open.
// With an agent step in the pattern and no model, no result is reused (Results is nil), since an
// agent step's reuse key needs the model.
func NewRunRecord(st RunStore, p machine.Pattern, model string) *RunRecord {
	r := &RunRecord{st: st, changed: map[string]chan struct{}{}}
	agents := slices.ContainsFunc(p.Steps, func(s machine.Step) bool { return strings.HasPrefix(s.Worker, "agent:") })
	if model != "" || !agents {
		r.results = machine.NewResults()
		reload(r.st, r.results)
	}
	return r
}

// Results is the accepted-result store, nil when no result may be reused.
func (r *RunRecord) Results() *machine.Results { return r.results }

// Record writes e, whose seq the run's workflow set, to the record of the slice's runs: on
// RunStarted with the run's project (e.Project), operation and replay_of, and on an accepted or
// reused step with the project's files (e.Files). The same event again writes nothing; other
// content under a held seq is an error wrapping store.ErrSeqTaken.
func (r *RunRecord) Record(slice string, e machine.Event, operation, replayOf string) error {
	var info *runInfo
	if e.Kind == "RunStarted" {
		info = &runInfo{Project: e.Project, Operation: operation, ReplayOf: replayOf}
	}
	data, err := json.Marshal(storedEvent{Event: e, Info: info, Files: e.Files})
	if err != nil {
		return err
	}
	if _, err := r.st.RecordEvent(slice, store.Event{Seq: e.Seq, Run: e.Run, Kind: e.Kind, Step: e.Step, Data: store.RawJSON(data)}); err != nil {
		return fmt.Errorf("run %s: event %d: %w", e.Run, e.Seq, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if ch, ok := r.changed[e.Run]; ok {
		close(ch)
		delete(r.changed, e.Run)
	}
	return nil
}

// next returns a channel closed when the run's next event is recorded through r.
func (r *RunRecord) next(run string) <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.changed[run]
	if !ok {
		ch = make(chan struct{})
		r.changed[run] = ch
	}
	return ch
}

// reload puts every result the record shows accepted, and every review round it shows left
// open, into the accepted-result store, so a restarted engine still reuses them. An open round is
// rebuilt from the StepRejected with open_round, the FindingRecorded events directly before it and
// that attempt's NoBlockingFindingOpen CheckFailed, or from a reused open round's FindingRecorded
// events directly before its ResultReused and that event's check (research D36). A run that
// cannot be read adds nothing; the first result under a key stays.
func reload(st RunStore, results *machine.Results) {
	for _, e := range st.Runs() {
		events, info, err := readRun(st, e.Run)
		if err != nil || info == nil {
			continue
		}
		var recent []machine.Finding // the findings recorded directly before the current event
		var files []machine.File     // the project's files after the last step that left any
		checks := map[[3]any]*machine.EventCheck{}
		for _, ev := range events {
			before := recent
			if ev.Kind != "FindingRecorded" {
				recent = nil
			}
			switch {
			case ev.Kind == "FindingRecorded" && ev.Finding != nil:
				recent = append(recent, *ev.Finding)
			case ev.Kind == "CheckFailed" && ev.Check != nil && ev.Check.Name == "NoBlockingFindingOpen":
				c := *ev.Check
				checks[[3]any{ev.Step, ev.Round, ev.Attempt}] = &c
			case ev.OpenRound && ev.ReuseKey != "" && (ev.Kind == "StepRejected" || ev.Kind == "ResultReused"):
				kept := machine.Accepted{Key: ev.ReuseKey, Artifacts: slices.Clone(ev.Artifacts), Run: ev.Run, Files: slices.Clone(files),
					Open: true, Findings: slices.Clone(before), Check: checks[[3]any{ev.Step, ev.Round, ev.Attempt}]}
				if ev.Kind == "ResultReused" && ev.Check != nil {
					c := *ev.Check
					kept.Check = &c
				}
				if ev.Files != nil { // a review that writes nothing leaves the files it started from
					kept.Files, files = slices.Clone(ev.Files), ev.Files
				}
				results.Put(kept)
			case (ev.Kind == "StepAccepted" || ev.Kind == "ResultReused") && ev.ReuseKey != "":
				results.Put(machine.Accepted{Key: ev.ReuseKey, Artifacts: slices.Clone(ev.Artifacts), Run: ev.Run, Lines: slices.Clone(ev.ChangedLines),
					Files: slices.Clone(ev.Files)})
				files = ev.Files
			}
		}
	}
}

// readRun returns a run's events and info from the record; none for an unknown run.
func readRun(st RunStore, run string) ([]machine.Event, *runInfo, error) {
	stored, err := st.Events(run)
	if err != nil {
		return nil, nil, err
	}
	out := make([]machine.Event, 0, len(stored))
	var info *runInfo
	for _, e := range stored {
		var d storedEvent
		dec := json.NewDecoder(strings.NewReader(string(e.Data)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&d); err != nil {
			return nil, nil, fmt.Errorf("run %s: event %d cannot be read: %w", run, e.Seq, err)
		}
		if d.Event.Seq != e.Seq || d.Event.Run != run {
			return nil, nil, fmt.Errorf("run %s: event %d does not match its place in the record", run, e.Seq)
		}
		if d.Info != nil {
			info = d.Info
			d.Event.Project = d.Info.Project
		}
		d.Event.Files = d.Files
		out = append(out, d.Event)
	}
	if len(out) > 0 && info == nil {
		return nil, nil, fmt.Errorf("run %s: RunStarted records no project or operation", run)
	}
	return out, info, nil
}

// runs starts and reads the runs: each run is a workflow of the durable service (Config.Runs),
// which writes its record through the RunRecord; at most one run on the platform has not ended
// (FR-021, startRun's RunActive). Starts, resumes, replays and hand-overs are serialised by mu, so
// the refusals checked before one hold until the durable service has taken it.
type runs struct {
	st      RunStore
	rec     *RunRecord
	results *machine.Results // rec's, nil when no result is reused
	durable Runs
	pattern machine.Pattern
	noRun   string // why no run can start, or ""
	model   string

	mu sync.Mutex // one start, resume, replay or hand-over at a time

	vmu      sync.Mutex
	starting map[string]string // run -> its slice: started, its RunStarted not yet read back
	// awaitStart: the durable worker writes through rec (Config.Record), so a start is answered
	// once the run's RunStarted is recorded, and the run is found at once.
	awaitStart bool
	current    map[string]string // run -> the operation following it in this engine
}

// newRuns returns the runs of the store, or nil when the store keeps no runs.
func newRuns(cfg Config, svc *sl.Service) *runs {
	st, ok := cfg.Store.(RunStore)
	if !ok {
		return nil
	}
	r := &runs{st: st, durable: cfg.Runs, model: cfg.Model, starting: map[string]string{}, current: map[string]string{}}
	switch p, err := svc.Pattern(); {
	case err != nil:
		r.noRun = "the pattern cannot be read: " + err.Error()
	default:
		r.pattern = MachinePattern(p)
	}
	if r.durable == nil {
		r.noRun = "the engine has no durable service to run a run on"
	}
	r.rec, r.awaitStart = cfg.Record, cfg.Record != nil
	if r.rec == nil {
		r.rec = NewRunRecord(st, r.pattern, cfg.Model)
	}
	r.results = r.rec.Results()
	return r
}

// reloadResults puts what the record shows accepted, or left open, into the run's results (reload).
func (r *runs) reloadResults() { reload(r.st, r.results) }

// MachinePattern is the slices service's pattern as the machine follows it: its one translation.
func MachinePattern(p sl.Pattern) machine.Pattern {
	budget := func(b sl.PatternBudget) machine.Budget {
		return machine.Budget{Tokens: b.Tokens, CostUSD: b.CostUSD, Duration: time.Duration(b.Minutes * float64(time.Minute))}
	}
	mp := machine.Pattern{ID: p.ID, Version: p.Version, Attempts: p.Limits.Attempts, ReviewRounds: p.Limits.ReviewRounds,
		RunBudget: budget(p.Budgets.Run), StepBudget: budget(p.Budgets.Step)}
	for _, s := range p.Steps {
		mp.Steps = append(mp.Steps, machine.Step{Name: s.Name, Worker: s.Worker, AgentDigest: s.AgentDigest,
			Inputs: slices.Clone(s.Inputs), Outputs: slices.Clone(s.Outputs), Checks: slices.Clone(s.Checks),
			Writable: slices.Clone(s.Writable), Dependencies: machine.Dependencies{
				Allowed: slices.Clone(s.Dependencies.Allowed), Forbidden: slices.Clone(s.Dependencies.Forbidden)}})
	}
	return mp
}

func (r *runs) read(run string) ([]machine.Event, *runInfo, error) { return readRun(r.st, run) }

// forget drops what the engine holds of a slice's runs, once its record is erased.
func (r *runs) forget(slice string) {
	r.vmu.Lock()
	defer r.vmu.Unlock()
	for run, s := range r.starting {
		if s == slice {
			delete(r.starting, run)
		}
	}
}

// find returns a run's state as its record shows it, its events and info; ok is false for a run
// the record does not hold.
func (r *runs) find(id string) (run machine.Run, events []machine.Event, info *runInfo, ok bool, err error) {
	events, info, err = r.read(id)
	if err != nil || len(events) == 0 {
		return machine.Run{}, nil, nil, false, err
	}
	return machine.StateOf(events), events, info, true, nil
}

// active returns the run of the slice that has not ended, if any.
func (r *runs) active(slice string) (string, error) {
	run, _, err := r.activeWhere(func(s string) bool { return s == slice }, "")
	return run, err
}

// activeAny returns a run of any slice, other than except, that has not ended, and its slice
// (FR-021: at most one active run on the platform).
func (r *runs) activeAny(except string) (run, slice string, err error) {
	return r.activeWhere(func(string) bool { return true }, except)
}

// activeWhere returns the first run, other than except, of a slice that in accepts and that has
// not ended: a recorded run whose record shows no end, or a run started whose RunStarted is not
// recorded yet.
func (r *runs) activeWhere(in func(slice string) bool, except string) (run, slice string, err error) {
	recorded := map[string]bool{}
	for _, e := range r.st.Runs() {
		recorded[e.Run] = true
		if !in(e.Slice) || e.Run == except {
			continue
		}
		v, _, _, ok, err := r.find(e.Run)
		if err != nil {
			return "", "", err
		}
		if ok && v.Status != machine.StatusEnded {
			return e.Run, e.Slice, nil
		}
	}
	r.vmu.Lock()
	defer r.vmu.Unlock()
	for id, s := range r.starting {
		switch {
		case recorded[id]:
			delete(r.starting, id) // its record speaks for it now
		case in(s) && id != except:
			return id, s, nil
		}
	}
	return "", "", nil
}

// waitingForWorkers returns the run of the slice that waits for the slice's step workers (its
// record's last event is RunInterrupted `the step workers are not running`), or "".
func (r *runs) waitingForWorkers(slice string) (string, error) {
	for _, e := range r.st.Runs() {
		if e.Slice != slice {
			continue
		}
		v, _, _, ok, err := r.find(e.Run)
		if err != nil {
			return "", err
		}
		if ok && v.Status == machine.StatusInterrupted && v.Reason == machine.ErrWorkersNotReady.Error() {
			return e.Run, nil
		}
	}
	return "", nil
}

// projectTaken refuses a project whose folder exists under the projects folder, or that a recorded
// run already builds into (its folder may not be written yet).
func (s *sliceServer) projectTaken(project string) error {
	if r := s.runs; r != nil {
		for _, e := range r.st.Runs() {
			if _, info, err := r.read(e.Run); err != nil {
				return err
			} else if info != nil && info.Project == project {
				return &sl.Error{Code: string(gen.ErrorCodeProjectExists), Detail: fmt.Sprintf("run %s already builds into %s", e.Run, project)}
			}
		}
	}
	if s.projects == "" {
		return nil
	}
	held, err := projectHolds(filepath.Join(s.projects, project))
	if err != nil {
		return fmt.Errorf("whether project %s exists: %w", project, err)
	}
	if held != "" {
		return &sl.Error{Code: string(gen.ErrorCodeProjectExists), Detail: fmt.Sprintf("the projects folder already holds %s (%s)", project, held)}
	}
	return nil
}

// projectHolds says what makes the project folder dir taken, or "" when it is free (owner,
// 2026-11-13): a folder that does not exist, or one holding only empty folders and an empty
// `.git` (the skeleton `ap build-workers` makes), is free; a folder holding a file or a symbolic
// link at any depth, a `.git` holding anything, or dir itself not being a folder, is taken.
// Symbolic links are never followed.
func projectHolds(dir string) (string, error) {
	fi, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", err
	case !fi.IsDir():
		return "it is not a folder", nil
	}
	held := ""
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case !d.IsDir():
			held = "the file " + filepath.ToSlash(rel)
		case filepath.Dir(rel) == ".git":
			held = "a non-empty .git"
		default:
			return nil
		}
		return filepath.SkipAll
	})
	return held, err
}

// ready refuses WorkersNotReady while a step of the pattern has no registered worker for the
// slice, or the engine cannot run a step at all.
func (s *sliceServer) ready(slice string) error {
	steps := s.svc.Steps()
	if len(steps) == 0 {
		return &sl.Error{Code: string(gen.ErrorCodeWorkersNotReady), Detail: "the pattern declares no steps that a worker could run"}
	}
	if st := s.workers.missing(slice, steps); st != "" {
		return &sl.Error{Code: string(gen.ErrorCodeWorkersNotReady),
			Detail: fmt.Sprintf("step %s of the pattern has no registered worker for slice %s", st, slice)}
	}
	return nil
}

// canRun refuses WorkersNotReady when the engine cannot start a run: no run store, no readable
// pattern, or no durable service to run it on.
func (s *sliceServer) canRun() error {
	switch {
	case s.runs == nil:
		return &sl.Error{Code: string(gen.ErrorCodeWorkersNotReady), Detail: "the engine keeps no run record"}
	case s.runs.noRun != "":
		return &sl.Error{Code: string(gen.ErrorCodeWorkersNotReady), Detail: s.runs.noRun}
	}
	return nil
}

// follow is the work of a run's operation: it lasts until the run's record shows its end and
// succeeds then, whatever the outcome. Cancelling the operation cancels the run's workflow first,
// which ends the run `cancelled`; when the engine stops, the operation ends and the run goes on.
func (s *sliceServer) follow(id string, op *operation) func(ctx context.Context) (WorkResult, error) {
	total := len(s.runs.pattern.Steps)
	op.onCancel = func(ctx context.Context) error {
		if err := s.runs.durable.Cancel(ctx, id); err != nil && !errors.Is(err, ErrNotFound) { // not found: it has ended
			return err
		}
		return nil
	}
	return func(ctx context.Context) (WorkResult, error) {
		for {
			changed := s.runs.rec.next(id)
			run, events, _, ok, err := s.runs.find(id)
			if err != nil {
				return WorkResult{}, err
			}
			if ok {
				s.ops.set(op, func(v *gen.Operation) {
					v.Progress = &struct {
						Done  int     `json:"done"`
						Step  *string `json:"step,omitempty"`
						Total int     `json:"total"`
					}{Done: min(len(run.Accepted), total), Total: total}
				})
				if run.Status == machine.StatusEnded {
					if run.Outcome == machine.OutcomeCancelled {
						detail := "the run was cancelled"
						if d := events[len(events)-1].Detail; strings.HasPrefix(d, workflowFailed) {
							detail = d
						}
						return WorkResult{}, &WorkError{Code: gen.ErrorCodeOperationFailed, Detail: detail}
					}
					return WorkResult{Run: id}, nil
				}
				// a workflow that closed with no end on the record: the run is ended here (T058)
				if ended, detail, err := s.runs.closeIfFailed(ctx, id); err == nil && ended && detail != "" {
					return WorkResult{}, &WorkError{Code: gen.ErrorCodeOperationFailed, Detail: detail}
				}
			}
			select {
			case <-changed:
			case <-time.After(followPoll):
			case <-ctx.Done():
				return WorkResult{}, ctx.Err()
			}
		}
	}
}

// operationOf returns the operation following the run in this engine, starting one when none does
// (after a restart, or once it has ended).
func (s *sliceServer) operationOf(id string) gen.Operation {
	s.runs.vmu.Lock()
	cur := s.runs.current[id]
	s.runs.vmu.Unlock()
	if v, ok := s.ops.get(cur); ok && !ended(v.Status) {
		return v
	}
	op := s.ops.reserve(gen.OperationKindRun, id)
	s.runs.vmu.Lock()
	s.runs.current[id] = op.view.Id
	s.runs.vmu.Unlock()
	return s.ops.launch(op, s.follow(id, op))
}

// newRun starts a new run of the slice as a workflow of the durable service: the run ID is given
// out by the record, the operation reserved, and the workflow started with both; it records
// RunStarted (with the project, the operation and replay_of) as its first event. The caller holds
// s.runs.mu and has checked every refusal. An error wrapping ErrUnavailable started nothing.
func (s *sliceServer) newRun(ctx context.Context, slice, project, replayOf string, inputs map[string]string, refs []string) (gen.Operation, error) {
	id, err := s.runs.st.NextRunID()
	if err != nil {
		return gen.Operation{}, fmt.Errorf("a run ID: %w", err)
	}
	op := s.ops.reserve(gen.OperationKindRun, id)
	p := s.runs.pattern
	err = s.runs.durable.Start(ctx, RunStart{Run: id, Slice: slice, Project: project, ReplayOf: replayOf, Operation: op.view.Id,
		Pattern: p.ID + "@" + p.Version, Definition: p, Model: s.runs.model, Inputs: inputs, Refs: refs,
		TaskQueue: RunsQueue, RejectDuplicate: true})
	if err != nil {
		return gen.Operation{}, err
	}
	s.runs.vmu.Lock()
	s.runs.starting[id] = slice
	s.runs.current[id] = op.view.Id
	s.runs.vmu.Unlock()
	if s.runs.awaitStart {
		s.runs.recorded(ctx, id, recordWait)
	}
	return s.ops.launch(op, s.follow(id, op)), nil
}

// recorded waits until the run's record holds an event, at most limit or until ctx is done.
func (r *runs) recorded(ctx context.Context, id string, limit time.Duration) {
	deadline := time.After(limit)
	for {
		changed := r.rec.next(id)
		if events, _, err := r.read(id); err != nil || len(events) > 0 {
			return
		}
		select {
		case <-changed:
		case <-deadline:
			return
		case <-ctx.Done():
			return
		}
	}
}

// noOtherActiveRun refuses RunActive, naming the run and its slice, while a run of any slice other
// than except has not ended (FR-021). The caller holds s.runs.mu, so the answer holds until the
// start, resume or replay has been taken by the durable service.
// A run whose workflow closed with no end on its record is ended first (closeIfFailed), so a
// restart leaves no run that blocks every start.
func (s *sliceServer) noOtherActiveRun(except string) error {
	for {
		run, slice, err := s.runs.activeAny(except)
		switch {
		case err != nil:
			return err
		case run == "":
			return nil
		}
		if ended, _, err := s.runs.closeIfFailed(context.Background(), run); err == nil && ended {
			continue
		}
		return &sl.Error{Code: string(gen.ErrorCodeRunActive), Detail: fmt.Sprintf("run %s of slice %s has not ended", run, slice)}
	}
}

// workflowFailed begins the detail of the RunCancelled that ends a run whose workflow failed.
const workflowFailed = "the run's workflow failed: "

// closeIfFailed ends a run whose workflow has closed (or is gone) while its record shows no end:
// it appends RunCancelled as the record's next event, detail `the run's workflow failed:
// <reason>` (data model, "Run record"; T058). It asks the durable service first and reads the
// record after, so an end the workflow wrote before closing is never followed by a second one.
// ended says the run has ended (by this call or before); detail is the appended event's, or "".
// An unreachable durable service or a record it cannot write is an error, and nothing changed.
func (r *runs) closeIfFailed(ctx context.Context, id string) (ended bool, detail string, err error) {
	closed, reason, err := r.durable.Closed(ctx, id)
	switch {
	case errors.Is(err, ErrNotFound):
		closed = true
	case err != nil:
		return false, "", err
	}
	if !closed {
		return false, "", nil
	}
	if reason == "" {
		reason = "it closed with no end"
	}
	detail = workflowFailed + reason
	for {
		run, events, _, ok, err := r.find(id)
		if err != nil || !ok {
			return false, "", err
		}
		if run.Status == machine.StatusEnded {
			return true, "", nil
		}
		last := events[len(events)-1]
		e := machine.Event{Seq: last.Seq + 1, At: time.Now().UTC(), Run: id, Kind: "RunCancelled", Detail: detail,
			Refs: events[0].Refs}
		err = r.rec.Record(events[0].Slice, e, "", "")
		if err == nil {
			return true, detail, nil
		}
		if !errors.Is(err, store.ErrSeqTaken) { // another writer took the seq: read the record again
			return false, "", err
		}
	}
}

// signal sends a signal to the run's workflow; an error wrapping ErrUnavailable sent nothing.
func (s *sliceServer) signal(ctx context.Context, run, name string) error {
	if err := s.runs.durable.Signal(ctx, run, name); err != nil {
		return fmt.Errorf("run %s: %w", run, err)
	}
	return nil
}

// StartRun refuses in the contract's order, the first that applies, before any operation exists:
// an unknown slice, a pattern condition that fails, a part not approved at its current hash
// (Buildable); then WorkersNotReady while a step of the pattern has no registered worker for the
// slice; then RunActive while any run, of any slice, has not ended (FR-021); then ProjectExists
// when the project's folder exists under the projects folder or a recorded run builds into it. A start
// that passes them all is WorkersNotReady while the engine cannot start a run (canRun), and 503
// while the durable service cannot be reached. Otherwise the run's workflow is started (newRun);
// the folder is not touched here.
func (s *sliceServer) StartRun(ctx context.Context, req gen.StartRunRequestObject) (gen.StartRunResponseObject, error) {
	id := req.SliceIdParameter
	if err := s.svc.Buildable(id); err != nil {
		return s.refuse("startRun", err)
	}
	if err := s.ready(id); err != nil {
		return s.refuse("startRun", err)
	}
	if s.runs != nil {
		s.runs.mu.Lock()
		defer s.runs.mu.Unlock()
		if err := s.noOtherActiveRun(""); err != nil {
			return s.refuse("startRun", err)
		}
	}
	if err := s.projectTaken(req.Body.Project); err != nil {
		return s.refuse("startRun", err)
	}
	if err := s.canRun(); err != nil {
		return s.refuse("startRun", err)
	}
	slc, err := s.svc.Get(id)
	if err != nil {
		return s.refuse("startRun", err)
	}
	inputs := map[string]string{"slice/goals": slc.Parts.Goals, "slice/rules": slc.Parts.Rules, "slice/contract": slc.Parts.Contract}
	var refs []string
	for _, g := range slc.Goals {
		refs = append(refs, g.ID)
	}
	for _, r := range slc.Rules {
		refs = append(refs, r.ID)
	}
	op, err := s.newRun(ctx, id, req.Body.Project, "", inputs, refs)
	switch {
	case errors.Is(err, ErrUnavailable):
		return unavailableFor(err), nil
	case err != nil:
		return s.refuse("startRun", err)
	}
	return gen.StartRun202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// ResumeRun refuses an unknown run (NotFound), then one that is not interrupted
// (RunNotInterrupted), then WorkersNotReady, then RunActive while any other run, of any slice,
// has not ended (FR-021). It then signals `resume` to the run's workflow, which records RunResumed
// and continues, and answers with the operation following the run; 503 while the durable service
// cannot be reached.
func (s *sliceServer) ResumeRun(ctx context.Context, req gen.ResumeRunRequestObject) (gen.ResumeRunResponseObject, error) {
	id := req.RunIdParameter
	if err := s.badParameter([]string{id}, ""); err != nil {
		return s.refuse("resumeRun", err)
	}
	if s.runs == nil {
		return s.refuse("resumeRun", runNotFound(id))
	}
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	run, _, _, ok, err := s.runs.find(id)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return s.refuse("resumeRun", runNotFound(id))
	case run.Status != machine.StatusInterrupted:
		return s.refuse("resumeRun", &sl.Error{Code: string(gen.ErrorCodeRunNotInterrupted), Detail: fmt.Sprintf("run %s is %s", id, describe(run))})
	}
	if err := s.ready(run.Slice); err != nil {
		return s.refuse("resumeRun", err)
	}
	if err := s.canRun(); err != nil {
		return s.refuse("resumeRun", err)
	}
	if err := s.noOtherActiveRun(id); err != nil {
		return s.refuse("resumeRun", err)
	}
	switch err := s.signal(ctx, id, SignalResume); {
	case errors.Is(err, ErrUnavailable):
		return unavailableFor(err), nil
	case err != nil:
		return nil, fmt.Errorf("resuming run %s: %w", id, err)
	}
	return gen.ResumeRun202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: s.operationOf(id)}}, nil
}

// ReplayRun refuses an unknown run (NotFound), then one that has not ended (RunNotEnded), then
// WorkersNotReady, RunActive (any run of any slice) and ProjectExists as startRun does, for the
// earlier run's slice. The new run has the earlier run's inputs and refs and `replay_of`; it
// reuses every result accepted under the same reuse key.
func (s *sliceServer) ReplayRun(ctx context.Context, req gen.ReplayRunRequestObject) (gen.ReplayRunResponseObject, error) {
	id := req.RunIdParameter
	if err := s.badParameter([]string{id}, ""); err != nil {
		return s.refuse("replayRun", err)
	}
	if s.runs == nil {
		return s.refuse("replayRun", runNotFound(id))
	}
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	run, events, _, ok, err := s.runs.find(id)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return s.refuse("replayRun", runNotFound(id))
	case run.Status != machine.StatusEnded:
		return s.refuse("replayRun", &sl.Error{Code: string(gen.ErrorCodeRunNotEnded), Detail: fmt.Sprintf("run %s is %s", id, describe(run))})
	}
	if err := s.ready(run.Slice); err != nil {
		return s.refuse("replayRun", err)
	}
	if err := s.noOtherActiveRun(""); err != nil {
		return s.refuse("replayRun", err)
	}
	if err := s.projectTaken(req.Body.Project); err != nil {
		return s.refuse("replayRun", err)
	}
	if err := s.canRun(); err != nil {
		return s.refuse("replayRun", err)
	}
	first := events[0]
	names := s.runs.pattern.SliceInputs()
	if len(names) != len(first.Inputs) {
		return nil, fmt.Errorf("run %s: RunStarted names %d inputs, the pattern %d", id, len(first.Inputs), len(names))
	}
	inputs := map[string]string{}
	for i, n := range names {
		inputs[n] = first.Inputs[i]
	}
	op, err := s.newRun(ctx, run.Slice, req.Body.Project, id, inputs, first.Refs)
	switch {
	case errors.Is(err, ErrUnavailable):
		return unavailableFor(err), nil
	case err != nil:
		return s.refuse("replayRun", err)
	}
	return gen.ReplayRun202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// AcceptHandOver refuses an unknown run (NotFound), then one not waiting for the developer
// (RunNotWaiting). It then signals `accept-hand-over` to the run's workflow, which records
// HandOverAccepted and ends, and answers with the operation following the run; 503 while the
// durable service cannot be reached.
func (s *sliceServer) AcceptHandOver(ctx context.Context, req gen.AcceptHandOverRequestObject) (gen.AcceptHandOverResponseObject, error) {
	id := req.RunIdParameter
	if err := s.badParameter([]string{id}, ""); err != nil {
		return s.refuse("acceptHandOver", err)
	}
	if s.runs == nil {
		return s.refuse("acceptHandOver", runNotFound(id))
	}
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	run, _, _, ok, err := s.runs.find(id)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return s.refuse("acceptHandOver", runNotFound(id))
	case run.Status != machine.StatusWaiting:
		return s.refuse("acceptHandOver", &sl.Error{Code: string(gen.ErrorCodeRunNotWaiting), Detail: fmt.Sprintf("run %s is %s", id, describe(run))})
	}
	if err := s.canRun(); err != nil {
		return nil, err
	}
	switch err := s.signal(ctx, id, SignalAcceptHandOver); {
	case errors.Is(err, ErrUnavailable):
		return unavailableFor(err), nil
	case err != nil:
		return nil, fmt.Errorf("accepting the hand-over of run %s: %w", id, err)
	}
	return gen.AcceptHandOver202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: s.operationOf(id)}}, nil
}

func (s *sliceServer) GetRun(_ context.Context, req gen.GetRunRequestObject) (gen.GetRunResponseObject, error) {
	if err := s.badParameter([]string{req.RunIdParameter}, ""); err != nil {
		return s.refuse("getRun", err)
	}
	if s.runs == nil {
		return s.refuse("getRun", runNotFound(req.RunIdParameter))
	}
	v, ok, err := s.runView(req.RunIdParameter)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return s.refuse("getRun", runNotFound(req.RunIdParameter))
	}
	return gen.GetRun200JSONResponse{RunJSONResponse: gen.RunJSONResponse{Body: v}}, nil
}

// ListRuns lists the runs newest first; a page token names the number of the last run of its page.
func (s *sliceServer) ListRuns(_ context.Context, req gen.ListRunsRequestObject) (gen.ListRunsResponseObject, error) {
	var all []store.RunEntry
	if s.runs != nil {
		all = s.runs.st.Runs()
	}
	slices.Reverse(all)
	numbers := make([]int, len(all))
	for i, e := range all {
		numbers[i], _ = strconv.Atoi(strings.TrimPrefix(e.Run, "RUN-"))
	}
	from, to, next, ok := s.pages.keysetPage(runsList, req.Params.PageToken, numbers)
	if !ok {
		return problemResponse(badPageToken()), nil
	}
	list := gen.RunList{Items: make([]gen.Run, 0, to-from), NextPageToken: next}
	for _, e := range all[from:to] {
		v, found, err := s.runView(e.Run)
		if err != nil {
			return nil, err
		}
		if found {
			list.Items = append(list.Items, v)
		}
	}
	return gen.ListRuns200JSONResponse{RunListJSONResponse: gen.RunListJSONResponse{Body: list}}, nil
}

// runView is the run as the contract shows it.
func (s *sliceServer) runView(id string) (gen.Run, bool, error) {
	run, events, info, ok, err := s.runs.find(id)
	if err != nil || !ok {
		return gen.Run{}, false, err
	}
	first, last := events[0], events[len(events)-1]
	v := gen.Run{Id: id, Slice: run.Slice, Pattern: run.Pattern, Project: info.Project, Status: gen.RunStatus(run.Status),
		Operation: info.Operation, CreatedAt: first.At, UpdatedAt: last.At, Reason: opt(run.Reason)}
	s.runs.vmu.Lock()
	if cur, ok := s.runs.current[id]; ok {
		v.Operation = cur
	}
	s.runs.vmu.Unlock()
	if info.ReplayOf != "" {
		r := info.ReplayOf
		v.ReplayOf = &r
	}
	if run.Status == machine.StatusEnded {
		o, at := gen.RunOutcome(run.Outcome), last.At
		v.Outcome, v.EndedAt = &o, &at
		if run.Outcome == machine.OutcomeDone {
			v.Reason = nil
		}
	} else if !run.ExpiresAt.IsZero() {
		at := run.ExpiresAt
		v.ExpiresAt = &at
	}
	return v, true, nil
}

// ListRunEvents returns the record in seq order, eventsPageSize per page. A page token names how
// many events came before its page; until the run ends, the last page has one too.
func (s *sliceServer) ListRunEvents(_ context.Context, req gen.ListRunEventsRequestObject) (gen.ListRunEventsResponseObject, error) {
	id := req.RunIdParameter
	if err := s.badParameter([]string{id}, ""); err != nil {
		return s.refuse("listRunEvents", err)
	}
	if s.runs == nil {
		return s.refuse("listRunEvents", runNotFound(id))
	}
	run, events, _, ok, err := s.runs.find(id)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return s.refuse("listRunEvents", runNotFound(id))
	}
	from := 0
	if t := req.Params.PageToken; t != nil {
		place, issued := s.pages.place(eventsList(id), *t)
		n, err := strconv.Atoi(place)
		if !issued || err != nil || n < 1 || n > len(events) {
			return problemResponse(badPageToken()), nil
		}
		from = n
	}
	to := min(from+eventsPageSize, len(events))
	list := gen.RunEventList{Items: make([]gen.RunEventSchema, 0, to-from)}
	for _, e := range events[from:to] {
		v, err := contractEvent(e)
		if err != nil {
			return nil, err
		}
		list.Items = append(list.Items, v)
	}
	if to < len(events) || run.Status != machine.StatusEnded {
		t := s.pages.issue(eventsList(id), strconv.Itoa(to))
		list.NextPageToken = &t
	}
	return gen.ListRunEvents200JSONResponse{RunEventListJSONResponse: gen.RunEventListJSONResponse{Body: list}}, nil
}

// contractEvent is a machine event in the contract's form; both are run-event.schema.json.
func contractEvent(e machine.Event) (gen.RunEventSchema, error) {
	var v gen.RunEventSchema
	b, err := json.Marshal(e)
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	if err != nil {
		return gen.RunEventSchema{}, fmt.Errorf("run %s: event %d in the contract's form: %w", e.Run, e.Seq, err)
	}
	return v, nil
}

// GetArtifact returns an artifact the run's record names (an input or an artifact of an event).
// The record keeps no media type, so it is served as application/octet-stream.
func (s *sliceServer) GetArtifact(_ context.Context, req gen.GetArtifactRequestObject) (gen.GetArtifactResponseObject, error) {
	id, hash := req.RunIdParameter, req.Hash
	if err := s.badParameter([]string{id}, hash); err != nil {
		return s.refuse("getArtifact", err)
	}
	if s.runs == nil {
		return s.refuse("getArtifact", runNotFound(id))
	}
	_, events, _, ok, err := s.runs.find(id)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return s.refuse("getArtifact", runNotFound(id))
	}
	named := slices.ContainsFunc(events, func(e machine.Event) bool {
		return slices.Contains(e.Inputs, hash) || slices.Contains(e.Artifacts, hash)
	})
	if !named {
		return s.refuse("getArtifact", &sl.Error{Code: string(gen.ErrorCodeNotFound), Detail: fmt.Sprintf("the record of run %s names no artifact %s", id, hash)})
	}
	b, err := s.runs.st.Artifact(hash)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return s.refuse("getArtifact", &sl.Error{Code: string(gen.ErrorCodeNotFound), Detail: fmt.Sprintf("artifact %s is not stored", hash)})
	case err != nil:
		return nil, fmt.Errorf("artifact %s: %w", hash, err)
	}
	return gen.GetArtifact200AsteriskResponse{ArtifactAsteriskResponse: gen.ArtifactAsteriskResponse{
		Body: bytes.NewReader(b), ContentType: "application/octet-stream", ContentLength: int64(len(b))}}, nil
}

// CompareRuns lists the differences between two runs' records (machine.Compare), 50 per page; a
// page token names the offset of its page in the comparison of those two runs.
func (s *sliceServer) CompareRuns(_ context.Context, req gen.CompareRunsRequestObject) (gen.CompareRunsResponseObject, error) {
	id, with := req.RunIdParameter, req.Params.With
	if err := s.badParameter([]string{id, with}, ""); err != nil {
		return s.refuse("compareRuns", err)
	}
	if s.runs == nil {
		return s.refuse("compareRuns", runNotFound(id))
	}
	var records [2][]machine.Event
	for i, r := range []string{id, with} {
		_, events, _, ok, err := s.runs.find(r)
		switch {
		case err != nil:
			return nil, err
		case !ok:
			return s.refuse("compareRuns", runNotFound(r))
		}
		records[i] = events
	}
	diffs := machine.Compare(records[0], records[1])
	from, to, next, ok := s.pages.offsetPage(comparisonList(id, with), req.Params.PageToken, len(diffs))
	if !ok {
		return problemResponse(badPageToken()), nil
	}
	list := gen.DifferenceList{Items: make([]gen.Difference, 0, to-from), NextPageToken: next}
	for _, d := range diffs[from:to] {
		list.Items = append(list.Items, gen.Difference{Step: d.Step, Field: gen.DifferenceField(d.Field), Left: opt(d.Left), Right: opt(d.Right)})
	}
	return gen.CompareRuns200JSONResponse{DifferenceListJSONResponse: gen.DifferenceListJSONResponse{Body: list}}, nil
}

// badParameter refuses a run ID or hash that breaks the contract's rule, or "" when every value fits.
func (s *sliceServer) badParameter(runIDs []string, hash string) error {
	for _, id := range runIDs {
		if !fits(s.contract.runID, id) {
			return &sl.Error{Code: string(gen.ErrorCodeInvalidRequest), Detail: fmt.Sprintf("%q is not a run ID", id)}
		}
	}
	if hash != "" && !fits(s.contract.hash, hash) {
		return &sl.Error{Code: string(gen.ErrorCodeInvalidRequest), Detail: fmt.Sprintf("%q is not a SHA-256 hash", hash)}
	}
	return nil
}

func runNotFound(id string) error {
	return &sl.Error{Code: string(gen.ErrorCodeNotFound), Detail: "run " + id + " does not exist"}
}

// describe says what a run is, for a refusal's detail.
func describe(r machine.Run) string {
	if r.Status == machine.StatusEnded {
		return "ended (" + string(r.Outcome) + ")"
	}
	return string(r.Status)
}

func (p problemResponse) VisitResumeRunResponse(w http.ResponseWriter) error      { return p.write(w) }
func (p problemResponse) VisitReplayRunResponse(w http.ResponseWriter) error      { return p.write(w) }
func (p problemResponse) VisitAcceptHandOverResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitGetRunResponse(w http.ResponseWriter) error         { return p.write(w) }
func (p problemResponse) VisitListRunsResponse(w http.ResponseWriter) error       { return p.write(w) }
func (p problemResponse) VisitListRunEventsResponse(w http.ResponseWriter) error  { return p.write(w) }
func (p problemResponse) VisitGetArtifactResponse(w http.ResponseWriter) error    { return p.write(w) }
func (p problemResponse) VisitCompareRunsResponse(w http.ResponseWriter) error    { return p.write(w) }
