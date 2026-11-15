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

// runs drives and reads the runs: one machine per slice (a run is active per slice, as startRun's
// RunActive says), whose events go to the store through runEvents. Starts, resumes, replays and
// hand-overs are serialised by mu, so the refusals checked before one hold until its first event
// is recorded.
type runs struct {
	st      RunStore
	pattern machine.Pattern
	noRun   string // why no run can start, or ""
	worker  machine.Worker
	checker machine.Checker
	history machine.History
	model   string
	clock   machine.Clock
	results *machine.Results
	base    context.Context

	mu sync.Mutex // one start, resume, replay or hand-over at a time

	vmu      sync.Mutex
	machines map[string]*machine.Machine // slice -> its machine
	pending  map[string]*runInfo         // run -> the info its RunStarted records
	changed  map[string]chan struct{}    // run -> closed at its next recorded event
	current  map[string]string           // run -> the operation following it in this engine
}

type clockFunc func() time.Time

func (c clockFunc) Now() time.Time { return c() }

// newRuns returns the runs of the store, or nil when the store keeps no runs.
func newRuns(cfg Config, svc *sl.Service, now func() time.Time, base context.Context) *runs {
	st, ok := cfg.Store.(RunStore)
	if !ok {
		return nil
	}
	r := &runs{st: st, worker: cfg.StepWorker, checker: cfg.StepChecker, history: cfg.History, model: cfg.Model, clock: clockFunc(now),
		base: base, machines: map[string]*machine.Machine{}, pending: map[string]*runInfo{},
		changed: map[string]chan struct{}{}, current: map[string]string{}}
	switch p, err := svc.Pattern(); {
	case err != nil:
		r.noRun = "the pattern cannot be read: " + err.Error()
	default:
		r.pattern = MachinePattern(p)
	}
	switch {
	case r.worker == nil && r.checker == nil:
		r.noRun = "the engine has no way yet to ask a step worker to run an attempt or a check to run"
	case r.worker == nil:
		r.noRun = "the engine has no way yet to ask a step worker to run an attempt"
	case r.checker == nil:
		r.noRun = "the engine has no way yet to run a step's checks"
	}
	// An agent step's reuse key needs the model; without one no result is reused (machine.Config).
	agents := slices.ContainsFunc(r.pattern.Steps, func(s machine.Step) bool { return strings.HasPrefix(s.Worker, "agent:") })
	if r.model != "" || !agents {
		r.results = machine.NewResults()
		r.reloadResults()
	}
	return r
}

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

// reloadResults puts every result the record shows accepted, and every review round it shows left
// open, into the accepted-result store, so a restarted engine still reuses them. An open round is
// rebuilt from the StepRejected with open_round, the FindingRecorded events directly before it and
// that attempt's NoBlockingFindingOpen CheckFailed, or from a reused open round's FindingRecorded
// events directly before its ResultReused and that event's check (research D36). A run that
// cannot be read adds nothing; the first result under a key stays.
func (r *runs) reloadResults() {
	for _, e := range r.st.Runs() {
		events, info, err := r.read(e.Run)
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
				r.results.Put(kept)
			case (ev.Kind == "StepAccepted" || ev.Kind == "ResultReused") && ev.ReuseKey != "":
				r.results.Put(machine.Accepted{Key: ev.ReuseKey, Artifacts: slices.Clone(ev.Artifacts), Run: ev.Run, Lines: slices.Clone(ev.ChangedLines),
					Files: slices.Clone(ev.Files)})
				files = ev.Files
			}
		}
	}
}

// read returns a run's events and info from the record; none for an unknown run.
func (r *runs) read(run string) ([]machine.Event, *runInfo, error) {
	stored, err := r.st.Events(run)
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

// runEvents is one slice's machine's run record: machine.Events, Reader and Lister over the store.
type runEvents struct {
	r     *runs
	slice string
}

func (a runEvents) Append(e machine.Event) error {
	a.r.vmu.Lock()
	info := a.r.pending[e.Run]
	a.r.vmu.Unlock()
	if e.Kind != "RunStarted" {
		info = nil
	} else if info == nil {
		return fmt.Errorf("run %s: started without its project and operation", e.Run)
	}
	data, err := json.Marshal(storedEvent{Event: e, Info: info, Files: e.Files})
	if err != nil {
		return err
	}
	got, err := a.r.st.Append(a.slice, store.Event{Run: e.Run, Kind: e.Kind, Step: e.Step, Data: store.RawJSON(data)})
	if err != nil {
		return err
	}
	if got.Seq != e.Seq {
		return fmt.Errorf("run %s: the store recorded event %d as seq %d", e.Run, e.Seq, got.Seq)
	}
	a.r.vmu.Lock()
	defer a.r.vmu.Unlock()
	if info != nil {
		delete(a.r.pending, e.Run)
	}
	if ch, ok := a.r.changed[e.Run]; ok {
		close(ch)
		delete(a.r.changed, e.Run)
	}
	return nil
}

// Read reads a run of this slice back; a run of another slice is not this machine's.
func (a runEvents) Read(run string) ([]machine.Event, error) {
	events, _, err := a.r.read(run)
	if err != nil || len(events) == 0 || events[0].Slice != a.slice {
		return nil, err
	}
	return events, nil
}

func (a runEvents) Runs() ([]string, error) {
	var ids []string
	for _, e := range a.r.st.Runs() {
		if e.Slice == a.slice {
			ids = append(ids, e.Run)
		}
	}
	return ids, nil
}

// next returns a channel closed when the run's next event is recorded.
func (r *runs) next(run string) <-chan struct{} {
	r.vmu.Lock()
	defer r.vmu.Unlock()
	ch, ok := r.changed[run]
	if !ok {
		ch = make(chan struct{})
		r.changed[run] = ch
	}
	return ch
}

func (r *runs) machineFor(slice string) *machine.Machine {
	r.vmu.Lock()
	defer r.vmu.Unlock()
	m, ok := r.machines[slice]
	if !ok {
		m = machine.New(machine.Config{Pattern: r.pattern, Worker: r.worker, Checker: r.checker, Clock: r.clock,
			Events: runEvents{r: r, slice: slice}, Results: r.results, Model: r.model, History: r.history})
		r.machines[slice] = m
	}
	return m
}

// forget drops what the engine holds of a slice's runs, once its record is erased.
func (r *runs) forget(slice string) {
	r.vmu.Lock()
	defer r.vmu.Unlock()
	delete(r.machines, slice)
}

// find returns a run, its events and info; ok is false for a run the record does not hold. Runs
// that waited too long for the developer are ended `expired` first.
func (r *runs) find(id string) (run machine.Run, events []machine.Event, info *runInfo, ok bool, err error) {
	events, _, err = r.read(id)
	if err != nil || len(events) == 0 {
		return machine.Run{}, nil, nil, false, err
	}
	m := r.machineFor(events[0].Slice)
	_ = m.ExpireDue() // a run that cannot be ended now is ended at a later read
	run, ok = m.Get(id)
	if !ok {
		return machine.Run{}, nil, nil, false, fmt.Errorf("run %s is in the record but cannot be read", id)
	}
	// Read again, so the record holds at least what the run's state shows.
	if events, info, err = r.read(id); err != nil {
		return machine.Run{}, nil, nil, false, err
	}
	return run, events, info, true, nil
}

// active returns the run of the slice that has not ended, if any.
func (r *runs) active(slice string) (string, error) {
	m := r.machineFor(slice)
	_ = m.ExpireDue()
	for _, e := range r.st.Runs() {
		if e.Slice != slice {
			continue
		}
		run, ok := m.Get(e.Run)
		if !ok {
			return "", fmt.Errorf("run %s is in the record but cannot be read", e.Run)
		}
		if run.Status != machine.StatusEnded {
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

// canRun refuses WorkersNotReady when the engine cannot drive a run: no run store, no readable
// pattern, or no way to reach the step workers and checks.
func (s *sliceServer) canRun() error {
	switch {
	case s.runs == nil:
		return &sl.Error{Code: string(gen.ErrorCodeWorkersNotReady), Detail: "the engine keeps no run record"}
	case s.runs.noRun != "":
		return &sl.Error{Code: string(gen.ErrorCodeWorkersNotReady), Detail: s.runs.noRun}
	}
	return nil
}

// begin calls the machine (Start or Resume) in its own goroutine, bound to the engine's context,
// and returns once the run's first event of that call is recorded, or with the call's error when
// it recorded nothing. The caller holds r.mu. A machine error after that first event is not
// returned: the machine interrupts the run itself (recordLocked), and the record shows it.
func (r *runs) begin(id string, call func(ctx context.Context) error) error {
	recorded := r.next(id)
	done := make(chan error, 1)
	go func() { done <- call(r.base) }()
	select {
	case <-recorded:
		return nil
	case err := <-done:
		select {
		case <-recorded:
			return nil
		default:
		}
		if err == nil {
			err = fmt.Errorf("run %s: the machine recorded nothing", id)
		}
		return err
	}
}

// follow is the work of a run's operation: it lasts until the run ends and succeeds then, whatever
// the outcome. Cancelling the operation ends the run `cancelled`; when the engine stops instead,
// the machine interrupts the run, to be resumed.
func (s *sliceServer) follow(m *machine.Machine, id string, op *operation) func(ctx context.Context) (WorkResult, error) {
	total := len(s.runs.pattern.Steps)
	op.onCancel = func() { _, _ = m.Cancel(id) } // ErrRunEnded: it ended meanwhile
	return func(ctx context.Context) (WorkResult, error) {
		for {
			changed := s.runs.next(id)
			if run, ok := m.Get(id); ok {
				s.ops.set(op, func(v *gen.Operation) {
					v.Progress = &struct {
						Done  int     `json:"done"`
						Step  *string `json:"step,omitempty"`
						Total int     `json:"total"`
					}{Done: min(len(run.Accepted), total), Total: total}
				})
				if run.Status == machine.StatusEnded {
					if run.Outcome == machine.OutcomeCancelled {
						return WorkResult{}, &WorkError{Code: gen.ErrorCodeOperationFailed, Detail: "the run was cancelled"}
					}
					return WorkResult{Run: id}, nil
				}
			}
			select {
			case <-changed:
			case <-ctx.Done(): // cancelled: op.onCancel ends the run; the engine stopping interrupts it
				return WorkResult{}, ctx.Err()
			}
		}
	}
}

// operationOf returns the operation following the run in this engine, starting one when none does
// (after a restart, or once it has ended).
func (s *sliceServer) operationOf(m *machine.Machine, id string) gen.Operation {
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
	return s.ops.launch(op, s.follow(m, id, op))
}

// newRun records a new run of the slice and starts it: the run ID is given out by the record,
// RunStarted (with the project, the operation and replay_of) is durable before the operation
// exists, and the machine then drives the run while the operation follows it. The caller holds
// s.runs.mu and has checked every refusal.
func (s *sliceServer) newRun(slice, project, replayOf string, inputs map[string]string, refs []string) (gen.Operation, error) {
	id, err := s.runs.st.NextRunID()
	if err != nil {
		return gen.Operation{}, fmt.Errorf("a run ID: %w", err)
	}
	op := s.ops.reserve(gen.OperationKindRun, id)
	s.runs.vmu.Lock()
	s.runs.pending[id] = &runInfo{Project: project, Operation: op.view.Id, ReplayOf: replayOf}
	s.runs.vmu.Unlock()
	m := s.runs.machineFor(slice)
	err = s.runs.begin(id, func(ctx context.Context) error {
		_, err := m.Start(ctx, machine.StartParams{Run: id, Slice: slice, Project: project, Inputs: inputs, Refs: refs})
		return err
	})
	if err != nil {
		s.runs.vmu.Lock()
		delete(s.runs.pending, id)
		s.runs.vmu.Unlock()
		if errors.Is(err, machine.ErrRunActive) {
			return gen.Operation{}, &sl.Error{Code: string(gen.ErrorCodeRunActive), Detail: "a run of slice " + slice + " has not ended"}
		}
		return gen.Operation{}, fmt.Errorf("starting run %s: %w", id, err)
	}
	s.runs.vmu.Lock()
	s.runs.current[id] = op.view.Id
	s.runs.vmu.Unlock()
	return s.ops.launch(op, s.follow(m, id, op)), nil
}

// StartRun refuses in the contract's order, the first that applies, before any operation exists:
// an unknown slice, a pattern condition that fails, a part not approved at its current hash
// (Buildable); then WorkersNotReady while a step of the pattern has no registered worker for the
// slice; then RunActive while a run of the slice has not ended; then ProjectExists when the
// project's folder exists under the projects folder or a recorded run builds into it. A start
// that passes them all is WorkersNotReady while the engine cannot drive a run (canRun). Otherwise
// the run is recorded and started (newRun); the folder is not touched here.
func (s *sliceServer) StartRun(_ context.Context, req gen.StartRunRequestObject) (gen.StartRunResponseObject, error) {
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
		if run, err := s.runs.active(id); err != nil {
			return nil, err
		} else if run != "" {
			return s.refuse("startRun", &sl.Error{Code: string(gen.ErrorCodeRunActive), Detail: fmt.Sprintf("run %s of slice %s has not ended", run, id)})
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
	op, err := s.newRun(id, req.Body.Project, "", inputs, refs)
	if err != nil {
		return s.refuse("startRun", err)
	}
	return gen.StartRun202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// ResumeRun refuses an unknown run (NotFound), then one that is not interrupted
// (RunNotInterrupted), then WorkersNotReady, then RunActive while another run of the slice has not
// ended. RunResumed is recorded before it answers; the run's operation is the one following it.
func (s *sliceServer) ResumeRun(_ context.Context, req gen.ResumeRunRequestObject) (gen.ResumeRunResponseObject, error) {
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
	if other, err := s.runs.active(run.Slice); err != nil {
		return nil, err
	} else if other != "" && other != id {
		return s.refuse("resumeRun", &sl.Error{Code: string(gen.ErrorCodeRunActive), Detail: fmt.Sprintf("run %s of slice %s has not ended", other, run.Slice)})
	}
	m := s.runs.machineFor(run.Slice)
	err = s.runs.begin(id, func(ctx context.Context) error {
		_, err := m.Resume(ctx, id)
		return err
	})
	switch {
	case errors.Is(err, machine.ErrRunActive):
		return s.refuse("resumeRun", &sl.Error{Code: string(gen.ErrorCodeRunActive), Detail: fmt.Sprintf("run %s is being driven", id)})
	case errors.Is(err, machine.ErrNotInterrupted), errors.Is(err, machine.ErrRunEnded):
		return s.refuse("resumeRun", &sl.Error{Code: string(gen.ErrorCodeRunNotInterrupted), Detail: fmt.Sprintf("run %s is not interrupted", id)})
	case err != nil:
		return nil, fmt.Errorf("resuming run %s: %w", id, err)
	}
	return gen.ResumeRun202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: s.operationOf(m, id)}}, nil
}

// ReplayRun refuses an unknown run (NotFound), then one that has not ended (RunNotEnded), then
// WorkersNotReady, RunActive and ProjectExists as startRun does, for the earlier run's slice. The
// new run has the earlier run's inputs and refs and `replay_of`; it reuses every result accepted
// under the same reuse key.
func (s *sliceServer) ReplayRun(_ context.Context, req gen.ReplayRunRequestObject) (gen.ReplayRunResponseObject, error) {
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
	if other, err := s.runs.active(run.Slice); err != nil {
		return nil, err
	} else if other != "" {
		return s.refuse("replayRun", &sl.Error{Code: string(gen.ErrorCodeRunActive), Detail: fmt.Sprintf("run %s of slice %s has not ended", other, run.Slice)})
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
	op, err := s.newRun(run.Slice, req.Body.Project, id, inputs, first.Refs)
	if err != nil {
		return s.refuse("replayRun", err)
	}
	return gen.ReplayRun202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// AcceptHandOver refuses an unknown run (NotFound), then one not waiting for the developer
// (RunNotWaiting). HandOverAccepted is recorded before it answers with the run's operation.
func (s *sliceServer) AcceptHandOver(_ context.Context, req gen.AcceptHandOverRequestObject) (gen.AcceptHandOverResponseObject, error) {
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
	m := s.runs.machineFor(run.Slice)
	switch _, err := m.AcceptHandOver(id); {
	case errors.Is(err, machine.ErrNotWaiting):
		return s.refuse("acceptHandOver", &sl.Error{Code: string(gen.ErrorCodeRunNotWaiting), Detail: fmt.Sprintf("run %s is not waiting for the developer", id)})
	case err != nil:
		return nil, fmt.Errorf("accepting the hand-over of run %s: %w", id, err)
	}
	return gen.AcceptHandOver202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: s.operationOf(m, id)}}, nil
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
