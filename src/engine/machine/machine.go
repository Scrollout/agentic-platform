// Package machine is the run state machine: it runs a pattern's steps in order, accepts a step's
// attempt only when every one of its checks passes (an agent's own claim of being done counts for
// nothing, Principle XX), retries a rejected attempt up to the pattern's attempts, runs the review
// step in rounds, enforces the run and step budgets, and hands the run over to the developer when
// it cannot finish. Every event it records marshals to the contract's run-event.schema.json.
//
// An attempt the worker fails (an error, such as an unavailable model) is a rejected attempt; when
// every attempt of a step fails so, or the run's context is cancelled (the platform stops), the run
// is interrupted and Resume continues it without running an accepted step again. A step whose
// reuse key is in Config.Results is not run: its accepted result is reused.
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

// Finding is a review finding. It blocks only with a file, a line and the check that confirms it.
type Finding struct {
	Rule     string `json:"rule"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Text     string `json:"text"`
	Blocking bool   `json:"blocking"`
	Check    string `json:"check,omitempty"`
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
	Round        int       // review rounds only, from 1
	Open         []Finding // the open blocking findings: for a fix of the code, and review rounds after the first
	ChangedLines []Line    // review rounds after the first: the lines the fix changed
	Inputs       []string  // hashes of the step's declared inputs, in order
}

// Result is what a worker returns for one attempt. Done is the agent's claim and is ignored.
type Result struct {
	Model        string
	Outputs      map[string]string // output name to artifact hash
	Usage        Usage
	Done         bool
	Findings     []Finding
	ChangedLines []Line
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
}

// CheckResult is a check's verdict; Refs are the slice items it concerns, if it names any.
type CheckResult struct {
	Passed bool
	Reason string
	Refs   []string
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
	Seq         int       `json:"seq"`
	At          time.Time `json:"at"`
	Run         string    `json:"run"`
	Slice       string    `json:"slice,omitempty"`
	Kind        string    `json:"kind"`
	Step        string    `json:"step,omitempty"`
	Attempt     int       `json:"attempt,omitempty"`
	Round       int       `json:"round,omitempty"`
	Worker      string    `json:"worker,omitempty"`
	Model       string    `json:"model,omitempty"`
	AgentDigest string    `json:"agent_digest,omitempty"`
	Pattern     string    `json:"pattern,omitempty"`
	Inputs      []string  `json:"inputs,omitempty"`
	Artifacts   []string  `json:"artifacts,omitempty"`
	ReuseKey    string    `json:"reuse_key,omitempty"`
	Usage       *Usage    `json:"usage,omitempty"`
	// ChangedLines, on StepAccepted or ResultReused, are the lines the accepted result reports changing (a fix);
	// the next review round is given them, also after a restart.
	ChangedLines []Line      `json:"changed_lines,omitempty"`
	Refs         []string    `json:"refs,omitempty"`
	Check        *EventCheck `json:"check,omitempty"`
	Finding      *Finding    `json:"finding,omitempty"`
	Detail       string      `json:"detail,omitempty"`
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
// Run is a run's state.
type Run struct {
	ID        string
	Slice     string
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
	Run    string
	Slice  string
	Inputs map[string]string
	Refs   []string
}

// Config wires a Machine. Results (optional) is the accepted-result store; Model is the model the
// pattern's agents use, part of every agent step's reuse key.
type Config struct {
	Pattern Pattern
	Worker  Worker
	Checker Checker
	Clock   Clock
	Events  Events
	Results *Results
	Model   string
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
	cancel  context.CancelFunc

	artifacts map[string]string // input or output name to hash
	used      Usage
	elapsed   time.Duration // running time before the current stretch
	since     time.Time     // start of the current running stretch
	pos       int           // index of the step being run
	round     int           // review round, 0 before the first
	fixing    bool          // the fix step runs before the next review round
	open      []Finding     // blocking findings of the last review round
	changed   []Line        // the lines the last fix changed
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
	m.runs[id] = r
	return r, nil
}

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
	r := &run{Run: Run{ID: p.Run, Slice: p.Slice, Pattern: m.cfg.Pattern.ref(), Status: StatusRunning},
		refs: slices.Clone(p.Refs), since: m.cfg.Clock.Now(), artifacts: map[string]string{}}
	for i, n := range names {
		r.artifacts[n] = hashes[i]
	}
	if err := m.emit(r, Event{Kind: "RunStarted", Slice: r.Slice, Pattern: r.Pattern, Inputs: hashes, Refs: r.refs}); err != nil {
		m.mu.Unlock()
		return Run{}, err
	}
	m.runs[r.ID] = r
	r.driving = true
	m.mu.Unlock()
	return m.drive(ctx, r)
}

// Resume continues an interrupted run from the step it stopped in; accepted steps do not run again.
func (m *Machine) Resume(ctx context.Context, id string) (Run, error) {
	m.mu.Lock()
	r, err := m.find(id)
	switch {
	case err != nil:
	case r.Status == StatusEnded:
		err = ErrRunEnded
	case r.Status != StatusInterrupted:
		err = ErrNotInterrupted
	case r.driving:
		err = ErrRunActive
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
	r.since, r.driving = m.cfg.Clock.Now(), true
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
func (m *Machine) emit(r *run, e Event) error {
	if r.Status == StatusEnded {
		return errHalt
	}
	e.Seq, e.At, e.Run = r.seq+1, m.cfg.Clock.Now(), r.ID
	if err := m.cfg.Events.Append(e); err != nil {
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
