// Package machine is the run state machine: it runs a pattern's steps in order, accepts a step's
// attempt only when every one of its checks passes (an agent's own claim of being done counts for
// nothing, Principle XX), retries a rejected attempt up to the pattern's attempts, runs the review
// step in rounds, enforces the run and step budgets, and hands the run over to the developer when
// it cannot finish. Every event it records marshals to the contract's run-event.schema.json.
//
// An attempt the worker fails (an error, such as an unavailable model) is a rejected attempt; when
// every attempt of a step fails so, or the run's context is cancelled (the platform stops), the run
// is interrupted and Resume continues it without running an accepted step again. A step whose
// reuse key is in Config.Results is not run: its accepted result is reused, in any project.
//
// With Config.History, the project worker's version history is the run's compensation and its
// commits (data-model.md, run rules): Restore before every attempt, Commit after every accepted
// or reused step and before anything for the next step or the run's outcome. A failing call is
// retried with backoff up to 3 times (D7), then the run is interrupted, never handed over, and
// Resume makes the call again first; with less than 1 s of the run's time budget left the call is
// not made and the run is interrupted.
//
// The machine holds its lock only while it reads or changes a run and appends an event, never
// while a worker or a check runs, so a worker may call Start, Get or Cancel.
package machine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// HandOverTTL is how long a run waits for the developer before it ends `expired` (research D14).
const HandOverTTL = 14 * 24 * time.Hour

// noBlockingFindingOpen is the review check whose failure means a round ended with open findings,
// not a rejected attempt.
const noBlockingFindingOpen = "NoBlockingFindingOpen"

var (
	// ErrRunActive: another run has no outcome yet (FR-021), or the run ID is in use by one.
	ErrRunActive = errors.New("machine: a run is active")
	// ErrNotWaiting: the run is not waiting for the developer.
	ErrNotWaiting = errors.New("machine: the run is not waiting for the developer")
	// ErrRunEnded: the run has already ended.
	ErrRunEnded = errors.New("machine: the run has ended")
	// ErrNotInterrupted: the run is not interrupted, so it cannot be resumed.
	ErrNotInterrupted = errors.New("machine: the run is not interrupted")
	// ErrUnknownRun: no run has this ID.
	ErrUnknownRun = errors.New("machine: no such run")
	// ErrWorkersNotReady: a Worker returns it (wrapped) when the step workers of the run's slice
	// are not running, so the attempt was not made (the run waits for them).
	ErrWorkersNotReady = errors.New("the step workers are not running")
	// ErrModelUnavailable: a Worker returns it (wrapped) when the step's model cannot be reached.
	ErrModelUnavailable = errors.New("the model is unavailable")
)

// errHalt stops the driving loop: the run waits for the developer, has ended or cannot be recorded.
var errHalt = errors.New("machine: run halted")

// Status is a run's status, as the contract's RunStatus.
type Status string

const (
	StatusRunning     Status = "running"
	StatusInterrupted Status = "interrupted"
	StatusWaiting     Status = "waiting-for-developer"
	StatusEnded       Status = "ended"
)

// Outcome is an ended run's outcome, as the contract's RunOutcome.
type Outcome string

const (
	OutcomeDone       Outcome = "done"
	OutcomeHandedOver Outcome = "handed-over"
	OutcomeExpired    Outcome = "expired"
	OutcomeCancelled  Outcome = "cancelled"
)

// Budget bounds model tokens, cost in USD and wall-clock time.
type Budget struct {
	Tokens   int64
	CostUSD  float64
	Duration time.Duration
}

// Usage is what one attempt used.
type Usage struct {
	Tokens  int64   `json:"tokens"`
	CostUSD float64 `json:"cost_usd"`
}

// clamped counts a negative amount a worker reports as 0.
func (u Usage) clamped() Usage { return Usage{Tokens: max(u.Tokens, 0), CostUSD: max(u.CostUSD, 0)} }

// Step is one step of a pattern.
type Step struct {
	Name        string
	Worker      string // agent:<name> or procedure:<name>
	AgentDigest string
	Inputs      []string
	Outputs     []string
	Checks      []string
	// Writable are the paths the step may write (the catalogue's glob dialect) and Dependencies
	// the imports its files may and may not add, copied from the pattern for its worker and checks.
	Writable     []string
	Dependencies Dependencies
}

// Dependencies are a step's allowed and forbidden import patterns, as the pattern declares them.
type Dependencies struct{ Allowed, Forbidden []string }

// File is one regular file of the project folder, by path and the SHA-256 of its content.
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Pattern is the part of a catalogue pattern the machine follows.
type Pattern struct {
	ID, Version  string
	Attempts     int
	ReviewRounds int
	RunBudget    Budget
	StepBudget   Budget // per attempt, or per review round
	Steps        []Step
}

func (p Pattern) ref() string { return p.ID + "@" + p.Version }

// Finding is a review finding. A blocking one carries a file and a line; Check is informational
// (run-event.schema.json): the review checks confirm a finding by rerunning its rule's tests.
type Finding struct {
	Rule     string `json:"rule"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Text     string `json:"text"`
	Blocking bool   `json:"blocking"`
	Check    string `json:"check,omitempty"`
	// Witness is the new witness test the review wrote for a blocking finding (research D35);
	// the check service decides from it whether the finding blocks.
	Witness *Witness `json:"witness,omitempty"`
}

// Witness is a review finding's witness test: a Go test file under review/r<n>/ and the test
// function TestR<n>... it declares (run-event.schema.json finding.witness).
type Witness struct {
	File string `json:"file"`
	Test string `json:"test"`
}

// Line is one line of a file in the project folder.
type Line struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// Request is one attempt of a step, or one attempt of one review round.
type Request struct {
	Run          string
	Step         Step
	Attempt      int
	Round        int       // review rounds and the fix after round r (r), from 1
	Open         []Finding // the open blocking findings: for a fix of the code, and review rounds after the first
	ChangedLines []Line    // review rounds after the first: the lines the fix changed
	Inputs       []string  // hashes of the step's declared inputs, in order
	Slice        string    // the slice the run builds
	Project      string    // the project folder the run builds into
	// Before are the project's files after the last accepted step, nil when no step was accepted
	// in this run yet (or none is known after a restart): the files the attempt starts from.
	Before []File
	// Artifacts are the hashes of every input and accepted output the run holds, by name: where
	// the content of a file in Before can be found (an accepted output's framed form).
	Artifacts map[string]string
}

// Result is what a worker returns for one attempt. Done is the agent's claim and is ignored.
type Result struct {
	Model        string
	Outputs      map[string]string // output name to artifact hash
	Usage        Usage
	Done         bool
	Findings     []Finding
	ChangedLines []Line
	// Before and ProjectFiles are the project's files when the attempt started and after it
	// (the worker reads them), so a check can judge the change and the next step start from it.
	Before, ProjectFiles []File
	// Content holds, by output name, the bytes stored under the output's hash, for a check that
	// reads an output's content (in memory only: a check never outlives its attempt).
	Content map[string][]byte
}

// Worker runs one attempt of a step. An error is a rejected attempt.
type Worker interface {
	Run(ctx context.Context, r Request) (Result, error)
}

// CheckRequest asks for one check of one attempt.
type CheckRequest struct {
	Run     string
	Step    Step
	Attempt int
	Name    string
	Result  Result
	Round   int      // review rounds and the fix after round r (r), from 1
	Inputs  []string // hashes of the step's declared inputs, in order
	Slice   string   // the slice the run builds
	Project string   // the project folder the run builds into
	// Open and ChangedLines are what the attempt was given (Request): in a review round after the
	// first, the round's scope (research D55).
	Open         []Finding
	ChangedLines []Line
}

// CheckResult is a check's verdict; Refs are the slice items it concerns, if it names any.
type CheckResult struct {
	Passed bool
	Reason string
	Refs   []string
	// Confirmed, on NoBlockingFindingOpen's result only, holds the 0-based positions, in the
	// attempt's findings, of the blocking findings its witness runs confirmed (ADR 0042). Only
	// these are recorded and used as blocking; every other finding is a suggestion.
	Confirmed []int
}

// Checker runs one check. An error counts as a failed check.
type Checker interface {
	Check(ctx context.Context, r CheckRequest) (CheckResult, error)
}

// Clock gives the time.
type Clock interface{ Now() time.Time }

// Events receives the run record, one event at a time, in order.
type Events interface{ Append(Event) error }

// EventCheck is a check's verdict in an event.
type EventCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason"`
}

// Event is one entry of a run record; it marshals to run-event.schema.json.
type Event struct {
	Seq     int       `json:"seq"`
	At      time.Time `json:"at"`
	Run     string    `json:"run"`
	Slice   string    `json:"slice,omitempty"`
	Kind    string    `json:"kind"`
	Step    string    `json:"step,omitempty"`
	Attempt int       `json:"attempt,omitempty"`
	Round   int       `json:"round,omitempty"`
	// OpenRound is true on the StepRejected that leaves a review round open and on the
	// ResultReused that reuses such a round's kept result (research D36).
	OpenRound   bool     `json:"open_round,omitempty"`
	Worker      string   `json:"worker,omitempty"`
	Model       string   `json:"model,omitempty"`
	AgentDigest string   `json:"agent_digest,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	Inputs      []string `json:"inputs,omitempty"`
	Artifacts   []string `json:"artifacts,omitempty"`
	ReuseKey    string   `json:"reuse_key,omitempty"`
	Usage       *Usage   `json:"usage,omitempty"`
	// ChangedLines, on StepAccepted or ResultReused, are the lines the accepted result reports changing (a fix);
	// the next review round is given them, also after a restart.
	ChangedLines []Line      `json:"changed_lines,omitempty"`
	Refs         []string    `json:"refs,omitempty"`
	Check        *EventCheck `json:"check,omitempty"`
	Finding      *Finding    `json:"finding,omitempty"`
	Detail       string      `json:"detail,omitempty"`
	// Project, on RunStarted, is the run's project folder, and Files, on StepAccepted and
	// ResultReused, the project's files after the step. Neither is part of the run-event
	// contract: an Events sink keeps them beside the event, so a run read back still has them.
	Project string `json:"-"`
	Files   []File `json:"-"`
}

// MarshalJSON keeps `inputs` and `artifacts` present, even empty, on the kinds that require them.
func (e Event) MarshalJSON() ([]byte, error) {
	type plain Event
	b, err := json.Marshal(plain(e))
	if err != nil || (e.Kind != "AttemptMade" && e.Kind != "StepAccepted" && e.Kind != "ResultReused") {
		return b, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for _, k := range []string{"inputs", "artifacts"} {
		if _, ok := m[k]; !ok {
			m[k] = json.RawMessage("[]")
		}
	}
	return json.Marshal(m)
}

// Run is a run's state.
type Run struct {
	ID        string
	Slice     string
	Project   string
	Pattern   string
	Status    Status
	Outcome   Outcome
	Reason    string
	ExpiresAt time.Time // while waiting for the developer or interrupted
	Accepted  []string  // the steps accepted or reused, in order
	Open      []Finding // the blocking findings open when the review ran out of rounds
}

// StartParams starts a run: the slice's part hashes by input name (`slice/contract`, ...) and the
// slice's refs, which every event without refs of its own names.
type StartParams struct {
	Run     string
	Slice   string
	Project string
	Inputs  map[string]string
	Refs    []string
}

// History is the project's version history, kept by the project worker (check contract,
// restoreStep and commitStep; data-model.md, run rules). An error, a time-out included, means the
// call did not take effect; a repeat of either call is safe.
type History interface {
	// Restore makes the project folder hold Before, from the last commit, before an attempt.
	Restore(ctx context.Context, r RestoreRequest) error
	// Commit records an accepted or reused step as the project's next commit; created is false
	// for a repeat of a commit already made.
	Commit(ctx context.Context, r CommitRequest) (created bool, err error)
}

// RestoreRequest is restoreStep for the attempt about to start.
type RestoreRequest struct {
	Run       string
	Step      Step
	Attempt   int // from 1
	Round     int // review rounds and the fix after round r (r), from 1; 0 otherwise
	Project   string
	Before    []File        // the files the last accepted step left; empty before the first
	TimeLimit time.Duration // what is left of the run's time budget
}

// CommitRequest is commitStep for one accepted or reused step.
type CommitRequest struct {
	Run        string
	Step       Step
	Attempt    int    // the accepted attempt (StepAccepted); 0 for a reused result
	Round      int    // the event's round, 0 when it has none
	ReusedFrom string // ResultReused only: the run that accepted the result, or left the round open
	// OpenRound is true only for a review round left with blocking findings open (data-model, "A
	// review round"): Round is that round's, and Attempt with AcceptedAt its StepRejected's, or,
	// for a reused open round, ReusedFrom with AcceptedAt its ResultReused's `at`.
	OpenRound    bool
	Project      string
	Before       []File     // the previous accepted step's project files, empty for the first
	ProjectFiles []File     // the event's Files
	AcceptedAt   time.Time  // the `at` of the StepAccepted or ResultReused event (StepRejected for a live open round)
	Outputs      []Artifact // ResultReused only: each declared output that is a set of project files
	TimeLimit    time.Duration
}

// Artifact is a stored output by name and hash.
type Artifact struct{ Name, SHA256 string }

// Config wires a Machine. Results (optional) is the accepted-result store; Model is the model the
// pattern's agents use, part of every agent step's reuse key. History (optional) is the project's
// version history: with it the machine calls Restore before every attempt and Commit after every
// accepted or reused step. Sleep is the wait before a retried History call; nil waits in real
// time.
type Config struct {
	Pattern Pattern
	Worker  Worker
	Checker Checker
	Clock   Clock
	Events  Events
	Results *Results
	Model   string
	History History
	Sleep   func(ctx context.Context, d time.Duration) error
}

// Reader is implemented by an Events sink that can read a run's record back, so a machine made
// after a restart can resume a run it did not start.
type Reader interface {
	Read(run string) ([]Event, error)
}

// Lister is implemented by an Events sink that can list the runs in its record. The machine then
// reads every listed run before its first call, so a run that exists only in the record and has
// not ended is active.
type Lister interface {
	Runs() ([]string, error)
}

// Machine runs one run at a time.
type Machine struct {
	cfg    Config
	mu     sync.Mutex
	runs   map[string]*run
	loaded bool // the listed runs were read
}

// loadListed reads every run the record lists, once; the lock is held. Until it succeeds every
// call fails, so a run in the record is never overlooked.
func (m *Machine) loadListed() error {
	if m.loaded {
		return nil
	}
	if l, ok := m.cfg.Events.(Lister); ok {
		ids, err := l.Runs()
		if err != nil {
			return fmt.Errorf("machine: listing the run record: %w", err)
		}
		for _, id := range ids {
			if _, err := m.lookup(id); err != nil {
				return err
			}
		}
	}
	m.loaded = true
	return nil
}

// run is a run's state and where its driving stands. Fields below Run are the driver's; they
// change only while driving is true, by the one goroutine driving the run.
type run struct {
	Run
	refs    []string
	seq     int
	driving bool
	stopped chan struct{} // closed when the current driver has stopped driving
	cancel  context.CancelFunc

	artifacts map[string]string // input or output name to hash
	used      Usage
	elapsed   time.Duration  // running time before the current stretch
	since     time.Time      // start of the current running stretch
	pos       int            // index of the step being run
	round     int            // review round, 0 before the first
	fixing    bool           // the fix step runs before the next review round
	open      []Finding      // blocking findings of the last review round
	changed   []Line         // the lines the last fix changed
	before    []File         // the project's files after the last accepted step
	commit    *CommitRequest // the last accepted step's commit, nil before the first
	committed bool           // commit was made since the run (re)started driving
}

// New makes a Machine.
func New(cfg Config) *Machine { return &Machine{cfg: cfg, runs: map[string]*run{}} }

// Get returns a copy of the run, reading it from the record when the machine does not hold it.
func (m *Machine) Get(id string) (Run, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.find(id)
	if err != nil {
		return Run{}, false
	}
	return r.copy(), true
}

func (r *run) copy() Run {
	c := r.Run
	c.Accepted = slices.Clone(r.Accepted)
	c.Open = slices.Clone(r.Open)
	return c
}

func (m *Machine) find(id string) (*run, error) {
	if err := m.loadListed(); err != nil {
		return nil, err
	}
	return m.lookup(id)
}

// lookup finds a run, reading it from the record when Events is a Reader; the lock is held.
func (m *Machine) lookup(id string) (*run, error) {
	if r, ok := m.runs[id]; ok {
		return r, nil
	}
	rd, ok := m.cfg.Events.(Reader)
	if !ok {
		return nil, ErrUnknownRun
	}
	events, err := rd.Read(id)
	if err != nil {
		return nil, fmt.Errorf("machine: run %s: reading the run record: %w", id, err)
	}
	if len(events) == 0 {
		return nil, ErrUnknownRun
	}
	r, err := m.load(events)
	if err != nil {
		return nil, err
	}
	if last := events[len(events)-1]; stoppedDuring(last.Kind) {
		// The platform stopped during the run (T085; data-model, Run): the record says so once,
		// stamped with the last event's time, so the downtime counts toward neither the time
		// budget nor the expiry. A failed append leaves the run interrupted in memory only; the
		// next start appends it.
		_ = m.emitAt(r, &Event{Kind: "RunInterrupted", Refs: r.refs, Detail: r.Reason}, last.At)
	}
	m.runs[id] = r
	return r, nil
}

// stoppedDuring reports whether a run whose last event is of kind was stopped by the platform: the
// kind is no end, no hand-over and no RunInterrupted.
func stoppedDuring(kind string) bool {
	return terminalOutcome[kind] == "" && kind != "RunHandedOver" && kind != "RunInterrupted"
}

// SliceInputs are the inputs no step produces (the slice's parts, such as `slice/contract`),
// sorted by name: the order of RunStarted's inputs.
func (p Pattern) SliceInputs() []string { return p.sliceInputs() }

// sliceInputs are the inputs no step produces (the slice's parts), sorted by name.
func (p Pattern) sliceInputs() []string {
	made := map[string]bool{}
	for _, s := range p.Steps {
		for _, o := range s.Outputs {
			made[o] = true
		}
	}
	var in []string
	for _, s := range p.Steps {
		for _, i := range s.Inputs {
			if !made[i] && !slices.Contains(in, i) {
				in = append(in, i)
			}
		}
	}
	slices.Sort(in)
	return in
}

// Start starts a run and drives it until it ends, waits for the developer or is interrupted. A run
// is refused with ErrRunActive while another run has no outcome, a waiting one included.
func (m *Machine) Start(ctx context.Context, p StartParams) (Run, error) {
	if len(p.Refs) == 0 {
		return Run{}, errors.New("machine: a run needs the slice's refs")
	}
	if m.cfg.Results != nil && m.cfg.Model == "" {
		for _, s := range m.cfg.Pattern.Steps {
			if isAgent(s) {
				return Run{}, fmt.Errorf("machine: step %s runs an agent, but no model is set for its reuse key", s.Name)
			}
		}
	}
	names := m.cfg.Pattern.sliceInputs()
	hashes := make([]string, len(names))
	for i, n := range names {
		if !hashRE.MatchString(p.Inputs[n]) {
			return Run{}, fmt.Errorf("machine: input %s is missing or not a SHA-256 hash", n)
		}
		hashes[i] = p.Inputs[n]
	}
	m.mu.Lock()
	if err := m.loadListed(); err != nil {
		m.mu.Unlock()
		return Run{}, err
	}
	for _, o := range m.runs {
		if o.Status != StatusEnded || o.ID == p.Run {
			m.mu.Unlock()
			return Run{}, ErrRunActive
		}
	}
	r := &run{Run: Run{ID: p.Run, Slice: p.Slice, Project: p.Project, Pattern: m.cfg.Pattern.ref(), Status: StatusRunning},
		refs: slices.Clone(p.Refs), since: m.cfg.Clock.Now(), artifacts: map[string]string{}}
	for i, n := range names {
		r.artifacts[n] = hashes[i]
	}
	if err := m.emit(r, Event{Kind: "RunStarted", Slice: r.Slice, Project: r.Project, Pattern: r.Pattern, Inputs: hashes, Refs: r.refs}); err != nil {
		m.mu.Unlock()
		return Run{}, err
	}
	m.runs[r.ID] = r
	r.driving, r.stopped = true, make(chan struct{})
	m.mu.Unlock()
	return m.drive(ctx, r)
}

// Resume continues an interrupted run from the step it stopped in; accepted steps do not run again.
// A run seen interrupted whose driver has not yet stopped is resumed once that driver has stopped
// (T102); the wait ends early, with ctx's error, when ctx is done.
func (m *Machine) Resume(ctx context.Context, id string) (Run, error) {
	m.mu.Lock()
	r, err := m.find(id)
	for err == nil && r.Status == StatusInterrupted && r.driving {
		stopped := r.stopped
		m.mu.Unlock()
		select {
		case <-stopped:
		case <-ctx.Done():
			return Run{}, ctx.Err()
		}
		m.mu.Lock()
		r, err = m.find(id)
	}
	switch {
	case err != nil:
	case r.Status == StatusEnded:
		err = ErrRunEnded
	case r.Status != StatusInterrupted:
		err = ErrNotInterrupted
	}
	if err == nil {
		for _, o := range m.runs {
			if o != r && o.Status != StatusEnded {
				err = ErrRunActive
			}
		}
	}
	if err != nil {
		m.mu.Unlock()
		return Run{}, err
	}
	if err := m.emit(r, Event{Kind: "RunResumed", Refs: r.refs}); err != nil {
		m.mu.Unlock()
		return r.copy(), err
	}
	r.Status, r.Reason, r.ExpiresAt = StatusRunning, "", time.Time{}
	r.since, r.driving, r.stopped = m.cfg.Clock.Now(), true, make(chan struct{})
	r.committed = false // resume commits the last accepted step again first; a repeat is safe
	m.mu.Unlock()
	return m.drive(ctx, r)
}

func (m *Machine) drive(ctx context.Context, r *run) (Run, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.mu.Lock()
	r.cancel = cancel
	m.mu.Unlock()
	err := (&driver{m: m, r: r}).steps(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	r.cancel, r.driving = nil, false
	close(r.stopped)
	if errors.Is(err, errHalt) {
		err = nil
	}
	return r.copy(), err
}

// AcceptHandOver ends a run waiting for the developer `handed-over`.
func (m *Machine) AcceptHandOver(id string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.find(id)
	if err != nil {
		return Run{}, err
	}
	if r.Status != StatusWaiting {
		return r.copy(), ErrNotWaiting
	}
	if err := m.end(r, "HandOverAccepted", OutcomeHandedOver, r.Reason); err != nil {
		return r.copy(), err
	}
	return r.copy(), nil
}

// Cancel ends a run that has not ended `cancelled`, whether it is running, interrupted or waiting.
// A running run stops at its next event; nothing the current attempt did is accepted.
func (m *Machine) Cancel(id string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.find(id)
	if err != nil {
		return Run{}, err
	}
	if r.Status == StatusEnded {
		return r.copy(), ErrRunEnded
	}
	if err := m.end(r, "RunCancelled", OutcomeCancelled, "the run was cancelled"); err != nil {
		return r.copy(), err
	}
	if r.cancel != nil {
		r.cancel()
	}
	return r.copy(), nil
}

// ExpireDue ends `expired` every run it holds that waits for the developer or is interrupted and
// whose ExpiresAt is not after the clock's time. It returns the first error appending an event;
// that run stays as it was.
func (m *Machine) ExpireDue() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.loadListed(); err != nil {
		return err
	}
	now := m.cfg.Clock.Now()
	ids := make([]string, 0, len(m.runs))
	for id := range m.runs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var first error
	for _, id := range ids {
		r := m.runs[id]
		if (r.Status != StatusWaiting && r.Status != StatusInterrupted) || r.driving || r.ExpiresAt.After(now) {
			continue
		}
		reason := "the run waited 14 days for the developer"
		if r.Reason != "" {
			reason += "; it stopped because " + r.Reason
		}
		if err := m.end(r, "RunExpired", OutcomeExpired, reason); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// emit appends one event of the run; the lock is held. A run that has ended takes no more events
// (errHalt). On an append error nothing changes and the error is returned.
func (m *Machine) emit(r *run, e Event) error { return m.emitp(r, &e) }

// emitp is emit, filling in the event's seq, time and run.
func (m *Machine) emitp(r *run, e *Event) error { return m.emitAt(r, e, m.cfg.Clock.Now()) }

// emitAt is emitp with the event stamped at, not the clock's time.
func (m *Machine) emitAt(r *run, e *Event, at time.Time) error {
	if r.Status == StatusEnded {
		return errHalt
	}
	e.Seq, e.At, e.Run = r.seq+1, at, r.ID
	if err := m.cfg.Events.Append(*e); err != nil {
		return fmt.Errorf("machine: run %s: the run record could not be written: %w", r.ID, err)
	}
	r.seq++
	return nil
}

// end records the terminal event and ends the run; the lock is held.
func (m *Machine) end(r *run, kind string, o Outcome, reason string) error {
	if err := m.emit(r, Event{Kind: kind, Refs: r.refs, Detail: reason}); err != nil {
		return err
	}
	r.Status, r.Outcome, r.Reason, r.ExpiresAt = StatusEnded, o, reason, time.Time{}
	return nil
}
