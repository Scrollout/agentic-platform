package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/machine"
	sl "github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
	"github.com/agentic-platform/engine/trials"
)

// The trial-sets resource (engine contract, tag trial-sets; feature 012, data model "Trial set"):
// build an approved slice several times, reusing nothing, and see how much the builds vary. A set
// is one operation of kind run-trials whose work, runTrials, starts its trials one after another,
// each as startRun without `pattern` starts a run (runChoice, newRun) into its own project, with
// NoReuse and the set on its RunStarted, and follows each to its end. What the trials show
// together is package trials' (trials.Of); this file only sequences them.

// TrialSetStore keeps the trial sets' records (feature 012); *store.Store has it. The engine owns
// a record's format; the store keeps its bytes in its slice's folder and erases it with the slice.
type TrialSetStore interface {
	// NextTrialSetID gives out `TS-<n>`, durable on return, never given again.
	NextTrialSetID() (string, error)
	// PutTrialSet replaces the set's record, durably; an unknown slice is an error.
	PutTrialSet(sliceID, trialSetID string, record []byte) error
	// TrialSet returns the set's slice and record, or an error wrapping store.ErrNotFound.
	TrialSet(trialSetID string) (sliceID string, record []byte, err error)
	// TrialSets returns the records of the slice's sets by ID.
	TrialSets(sliceID string) (map[string][]byte, error)
}

// trialPoll is how often a set's work looks again at its trial, its clock and the other runs when
// nothing it was told of happened.
const trialPoll = 200 * time.Millisecond

// trialSetIDRE is the contract's form of a trial-set ID.
var trialSetIDRE = regexp.MustCompile(`^TS-[1-9][0-9]*$`)

// trialSetsList is the list that issues page tokens for a slice's trial sets.
func trialSetsList(slice string) string { return "trial-sets:" + slice }

// trialBudget is a set's own budget: x-limits trial-set-cost-usd and trial-set-hours when it was
// accepted.
type trialBudget struct {
	CostUSD float64 `json:"cost_usd"`
	Hours   int     `json:"hours"`
}

// trialSetAccepted is what a set's operation holds as its body: the request as the engine read it
// and, beside it, what the set was accepted with, so its work can write the set when the
// acceptance could not (data model, "Write order and repeats").
type trialSetAccepted struct {
	Slice         string             `json:"slice"`
	Trials        int                `json:"trials"`
	ProjectPrefix string             `json:"project_prefix"`
	Method        string             `json:"method"`
	PartHashes    machine.PartHashes `json:"part_hashes"`
	Budget        trialBudget        `json:"budget"`
}

// trialSetRecord is a trial set as the engine keeps it (data model, "Trial set"); its result is
// computed from its trials' run records on each read, never kept.
type trialSetRecord struct {
	ID string `json:"id"`
	trialSetAccepted
	Operation      string                 `json:"operation"`
	State          gen.TrialSetState      `json:"state"`
	StoppedBecause gen.TrialSetStopReason `json:"stopped_because,omitempty"`
	Runs           []string               `json:"runs"` // trial i's run ID, written before its run starts
	CreatedAt      time.Time              `json:"created_at"`
	EndedAt        *time.Time             `json:"ended_at,omitempty"`
}

// project is trial n's project.
func (a trialSetAccepted) project(n int) string { return fmt.Sprintf("%s-%d", a.ProjectPrefix, n) }

func (r trialSetRecord) deadline() time.Time {
	return r.CreatedAt.Add(time.Duration(r.Budget.Hours) * time.Hour)
}

// trialSetStore is the store of trial sets, or the engine's fault when it keeps none.
func (s *sliceServer) trialSetStore() (TrialSetStore, error) {
	if s.trialSets == nil || s.runs == nil {
		return nil, errors.New("the engine keeps no trial sets: its store keeps no runs or trial sets")
	}
	return s.trialSets, nil
}

// StartTrialSet refuses in the contract's order, the first that applies, before anything is
// recorded: an unknown slice (NotFound), then startRun's refusals for a run of the slice without
// `pattern` (runChoice), then WorkersNotReady naming the first project, in trial order, and a step
// without a ready worker, then ProjectExists naming the first project that is taken, then RunActive
// while another set of the slice is running or its operation is open. Otherwise it reserves the
// set's ID, records its operation (kind run-trials, target the set) with what the set is accepted
// with, then the set itself (`running`), and answers 202. When the operation cannot be recorded
// nothing is kept and the answer is 503; when the set cannot, the operation's work writes it.
func (s *sliceServer) StartTrialSet(ctx context.Context, req gen.StartTrialSetRequestObject) (gen.StartTrialSetResponseObject, error) {
	ts, err := s.trialSetStore()
	if err != nil {
		return nil, err
	}
	id := req.SliceIdParameter
	lim := s.contract.trials
	k := lim.def
	if req.Body.Trials != nil {
		k = *req.Body.Trials
	}
	if k < 1 || k > lim.max { // the schema's bound, also when the contract and the engine disagree
		return s.refuse("startTrialSet", &sl.Error{Code: string(gen.ErrorCodeInvalidRequest),
			Detail: fmt.Sprintf("trials is %d; a set has 1 to %d", k, lim.max)})
	}
	a := trialSetAccepted{Slice: id, Trials: k, ProjectPrefix: req.Body.ProjectPrefix,
		Budget: trialBudget{CostUSD: lim.costUSD, Hours: lim.hours}}
	_, steps, err := s.runChoice(id, nil)
	if err != nil {
		return s.refuse("startTrialSet", err)
	}
	for n := 1; n <= k; n++ {
		if err := s.ready(id, a.project(n), steps); err != nil {
			return s.refuse("startTrialSet", err)
		}
	}
	for n := 1; n <= k; n++ {
		if err := s.projectTaken(a.project(n)); err != nil {
			return s.refuse("startTrialSet", err)
		}
	}
	s.tmu.Lock()
	defer s.tmu.Unlock()
	if err := s.noRunningSet(id); err != nil {
		return s.refuse("startTrialSet", err)
	}
	if err := s.canRun(); err != nil {
		return s.refuse("startTrialSet", err)
	}
	slc, err := s.svc.Get(id)
	if err != nil {
		return s.refuse("startTrialSet", err)
	}
	a.PartHashes = machine.PartHashes{Goals: slc.Parts.Goals, Rules: slc.Parts.Rules, Contract: slc.Parts.Contract}
	if a.Method, err = currentMethod(); err != nil {
		return nil, err
	}
	setID, err := ts.NextTrialSetID()
	if err != nil {
		log.Printf("engine: a trial-set ID: %v", err)
		return unavailableFor(errNotRecorded), nil
	}
	body, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	rec, err := s.ops.recordBody(ctx, "startTrialSet", setID, body)
	if err != nil {
		return unavailableFor(err), nil
	}
	set := trialSetRecord{ID: setID, trialSetAccepted: a, Operation: rec.ID, State: gen.TrialSetStateRunning, Runs: []string{},
		CreatedAt: rec.AcceptedAt}
	if err := s.putSet(set); err != nil {
		log.Printf("engine: recording trial set %s: %v; its operation %s writes it", setID, err, rec.ID)
	}
	s.ops.launch(ctx, rec)
	return gen.StartTrialSet202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: acceptedView(rec)}}, nil
}

// noRunningSet refuses RunActive while a set of the slice is running: its run-trials operation is
// open, or its record is `running` and its operation not known to have ended.
func (s *sliceServer) noRunningSet(slice string) error {
	if s.trialSets == nil {
		return nil
	}
	busy := func(set string) error {
		return &sl.Error{Code: string(gen.ErrorCodeRunActive), Detail: fmt.Sprintf("trial set %s of slice %s is running", set, slice)}
	}
	open, err := s.ops.records.OpenOperations()
	if err != nil {
		return err
	}
	for _, rec := range open {
		if !isTrialsKind(rec.Kind) {
			continue
		}
		_, body, err := s.ops.records.Operation(rec.ID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		} else if err != nil {
			return err
		}
		var a trialSetAccepted
		if json.Unmarshal(body, &a) != nil || a.Slice == slice {
			return busy(rec.Target) // an unreadable body is taken to be the slice's
		}
	}
	all, err := s.trialSets.TrialSets(slice)
	if err != nil {
		return err
	}
	for id, b := range all {
		var r trialSetRecord
		if err := json.Unmarshal(b, &r); err != nil {
			return fmt.Errorf("trial set %s: %w", id, err)
		}
		if r.State != gen.TrialSetStateRunning {
			continue
		}
		if op, _, err := s.ops.records.Operation(r.Operation); err == nil && op.EndedAt == nil {
			return busy(id)
		}
	}
	return nil
}

// readSet returns the set's record; an error wrapping store.ErrNotFound when none is kept.
func (s *sliceServer) readSet(id string) (trialSetRecord, error) {
	_, b, err := s.trialSets.TrialSet(id)
	if err != nil {
		return trialSetRecord{}, err
	}
	var r trialSetRecord
	if err := json.Unmarshal(b, &r); err != nil {
		return trialSetRecord{}, fmt.Errorf("trial set %s: %w", id, err)
	}
	if r.Runs == nil {
		r.Runs = []string{}
	}
	return r, nil
}

func (s *sliceServer) putSet(r trialSetRecord) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.trialSets.PutTrialSet(r.Slice, r.ID, b)
}

// openSet returns the set of a run-trials operation: its record, or, when its acceptance could not
// write it, the record written now from what the operation holds. The caller holds s.tmu.
func (s *sliceServer) openSet(op store.OperationRecord, body []byte) (trialSetRecord, error) {
	r, err := s.readSet(op.Target)
	if !errors.Is(err, store.ErrNotFound) {
		return r, err
	}
	var a trialSetAccepted
	if err := json.Unmarshal(body, &a); err != nil || a.Slice == "" {
		return trialSetRecord{}, fmt.Errorf("operation %s holds no trial set to write: %v", op.ID, err)
	}
	r = trialSetRecord{ID: op.Target, trialSetAccepted: a, Operation: op.ID, State: gen.TrialSetStateRunning, Runs: []string{},
		CreatedAt: op.AcceptedAt}
	if err := s.putSet(r); err != nil {
		return trialSetRecord{}, Retryable(fmt.Errorf("writing trial set %s: %w", op.Target, err))
	}
	return r, nil
}

// endSet ends a running set in state (with why for `stopped`); a set that has ended already is
// left as it is.
func (s *sliceServer) endSet(id string, state gen.TrialSetState, why gen.TrialSetStopReason) error {
	s.tmu.Lock()
	defer s.tmu.Unlock()
	r, err := s.readSet(id)
	if err != nil {
		return err
	}
	if r.State != gen.TrialSetStateRunning {
		return nil
	}
	at := s.ops.now().UTC()
	r.State, r.StoppedBecause, r.EndedAt = state, why, &at
	return s.putSet(r)
}

// runTrials is the work of a run-trials operation (data model, "Write order and repeats"). For
// each trial in turn it follows the trial recorded last until its run ends, accepting its
// hand-over when it waits for the developer; then, before the next, it checks the set's time,
// its cost budget, the slice's approvals against the set's part_hashes and the next project's
// workers, and stops the set at the first that fails; it waits while another run of the platform
// has not ended; then it writes the next trial's run ID into the set and starts that run. A
// repeat follows the run it recorded, or starts that same ID when it has neither a workflow nor a
// record, so a trial is never started twice. It succeeds once the set has ended or stopped; a run
// it cannot start is Retryable.
func (s *sliceServer) runTrials(ctx context.Context, op store.OperationRecord, body []byte) (WorkResult, error) {
	if _, err := s.trialSetStore(); err != nil {
		return WorkResult{}, err
	}
	res := WorkResult{TrialSet: op.Target}
	s.tmu.Lock()
	_, err := s.openSet(op, body)
	s.tmu.Unlock()
	if err != nil {
		return WorkResult{}, err
	}
	for {
		set, err := s.readSet(op.Target)
		switch {
		case errors.Is(err, store.ErrNotFound):
			return WorkResult{}, &WorkError{Code: gen.ErrorCodeNotFound, Detail: "trial set " + op.Target + " was erased"}
		case err != nil:
			return WorkResult{}, Retryable(err)
		case set.State != gen.TrialSetStateRunning:
			return res, nil
		}
		n, pending := len(set.Runs), ""
		if n > 0 {
			last := set.Runs[n-1]
			started, err := s.trialStarted(ctx, last)
			if err != nil {
				return WorkResult{}, Retryable(err)
			}
			if !started {
				pending = last // recorded and never started: started now with the same ID
			} else {
				if err := s.followTrial(ctx, set, last); err != nil {
					return WorkResult{}, err
				}
				s.awaitTrialOperation(ctx, last)
				if set, err = s.readSet(op.Target); err != nil {
					return WorkResult{}, Retryable(err)
				}
				if set.State != gen.TrialSetStateRunning || len(set.Runs) != n {
					continue
				}
				if n == set.Trials {
					if err := s.endSet(set.ID, gen.TrialSetStateEnded, ""); err != nil {
						return WorkResult{}, Retryable(err)
					}
					continue
				}
			}
		}
		number := n + 1
		if pending != "" {
			number = n
		}
		start, why, err := s.beforeTrial(set, number)
		switch {
		case err != nil:
			return WorkResult{}, Retryable(err)
		case why == gen.TrialSetStopReasonTime:
			if err := s.stopForTime(ctx, set); err != nil {
				return WorkResult{}, Retryable(err)
			}
			continue
		case why != "":
			if err := s.endSet(set.ID, gen.TrialSetStateStopped, why); err != nil {
				return WorkResult{}, Retryable(err)
			}
			continue
		}
		waiting, err := s.startTrial(ctx, set, start, pending)
		if err != nil {
			return WorkResult{}, Retryable(err)
		}
		if waiting {
			select {
			case <-time.After(trialPoll):
			case <-ctx.Done():
				return WorkResult{}, ctx.Err()
			}
		}
	}
}

// trialStarted says whether the trial's run was started: it has a workflow, a record, or this
// engine started it.
func (s *sliceServer) trialStarted(ctx context.Context, run string) (bool, error) {
	exists, err := s.runs.durable.Exists(ctx, run)
	if err != nil || exists || s.runs.started(run) {
		return exists || s.runs.started(run), err
	}
	_, _, _, ok, err := s.runs.find(run)
	return ok, err
}

// beforeTrial checks what must hold before trial number of set starts, in the contract's order,
// and gives the run to start: the set's hours not passed (else `time`), its cost so far plus the
// trial's own run budget within its budget (`budget`), every part of the slice still approved at
// the set's part_hashes (`slice-changed`), and a ready worker of the trial's project for every
// step that needs one (`workers`). An error is the engine's.
func (s *sliceServer) beforeTrial(set trialSetRecord, number int) (RunStart, gen.TrialSetStopReason, error) {
	if !s.ops.now().Before(set.deadline()) {
		return RunStart{}, gen.TrialSetStopReasonTime, nil
	}
	start, steps, choiceErr := s.runChoice(set.Slice, nil)
	var refusal *sl.Error
	if choiceErr != nil && !errors.As(choiceErr, &refusal) {
		return RunStart{}, "", choiceErr
	}
	next := s.runs.pattern.RunBudget.CostUSD
	if choiceErr == nil && start.Definition.ID != "" {
		next = start.Definition.RunBudget.CostUSD
	}
	ts, err := s.trialsOf(set)
	if err != nil {
		return RunStart{}, "", err
	}
	if !trials.MayStart(trials.Spent(ts), next, set.Budget.CostUSD) {
		return RunStart{}, gen.TrialSetStopReasonBudget, nil
	}
	if choiceErr != nil {
		return RunStart{}, gen.TrialSetStopReasonSliceChanged, nil
	}
	slc, err := s.svc.Get(set.Slice)
	switch {
	case errors.As(err, &refusal):
		return RunStart{}, gen.TrialSetStopReasonSliceChanged, nil
	case err != nil:
		return RunStart{}, "", err
	}
	if (machine.PartHashes{Goals: slc.Parts.Goals, Rules: slc.Parts.Rules, Contract: slc.Parts.Contract}) != set.PartHashes {
		return RunStart{}, gen.TrialSetStopReasonSliceChanged, nil
	}
	if err := s.ready(set.Slice, set.project(number), steps); err != nil {
		return RunStart{}, gen.TrialSetStopReasonWorkers, nil
	}
	if err := s.sliceStart(&start, set.Slice); err != nil {
		return RunStart{}, "", err
	}
	start.Project, start.NoReuse, start.TrialSet, start.Trial = set.project(number), true, set.ID, number
	return start, "", nil
}

// startTrial writes the trial's run ID into the set, a new one unless pending is a recorded ID
// that never started, and starts its run through newRun with that ID. It does neither while the
// set no longer runs or has changed, and reports waiting while another run of the platform has not
// ended (the set waits; it does not fail).
func (s *sliceServer) startTrial(ctx context.Context, set trialSetRecord, start RunStart, pending string) (waiting bool, err error) {
	s.tmu.Lock()
	defer s.tmu.Unlock()
	cur, err := s.readSet(set.ID)
	if err != nil || cur.State != gen.TrialSetStateRunning || len(cur.Runs) != len(set.Runs) {
		return false, err
	}
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	var refusal *sl.Error
	switch err := s.noOtherActiveRun(""); {
	case errors.As(err, &refusal) && refusal.Code == string(gen.ErrorCodeRunActive):
		return true, nil
	case err != nil:
		return false, err
	}
	if err := s.fillErased(start); err != nil {
		return true, nil // the pattern choice is made again
	}
	run := pending
	if run == "" {
		if run, err = s.runs.st.NextRunID(); err != nil {
			return false, err
		}
		cur.Runs = append(cur.Runs, run)
		if err := s.putSet(cur); err != nil {
			return false, err
		}
	}
	start.Run = run
	if _, err := s.newRun(ctx, "startRun", start); err != nil {
		return false, fmt.Errorf("starting trial %d of trial set %s: %w", start.Trial, set.ID, err)
	}
	return false, nil
}

// followTrial waits until the trial's run has ended, reading its record on every change: a run
// that waits for the developer is handed over by the set (research R4), a workflow that closed
// with no end on the record ends the run (closeIfFailed), and the set's hours passing cancels the
// run and stops the set `time`. It returns ctx's error when ctx ends first (the engine stops, or
// the operation is cancelled); the run goes on.
func (s *sliceServer) followTrial(ctx context.Context, set trialSetRecord, run string) error {
	for {
		changed := s.runs.rec.next(run)
		st, _, _, ok, err := s.runs.find(run)
		if err != nil {
			return Retryable(err)
		}
		if ok && st.Status == machine.StatusEnded {
			return nil
		}
		if ok && st.Status == machine.StatusWaiting {
			s.acceptTrialHandOver(ctx, run)
		}
		if ended, _, err := s.runs.closeIfFailed(ctx, run); err == nil && ended {
			return nil
		}
		if !s.ops.now().Before(set.deadline()) {
			return s.stopForTime(ctx, set)
		}
		select {
		case <-changed:
		case <-time.After(trialPoll):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// acceptTrialHandOver accepts the hand-over of a trial that waits for the developer, as
// acceptHandOver does, and waits until its record shows it, so a hand-over of the developer's
// that comes meanwhile is answered RunNotWaiting. A run that no longer waits is left alone: the
// set reads it again.
func (s *sliceServer) acceptTrialHandOver(ctx context.Context, run string) {
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	st, _, _, ok, err := s.runs.find(run)
	if err != nil || !ok || st.Status != machine.StatusWaiting {
		return
	}
	if _, err := s.signalFollowed(ctx, "acceptHandOver", run, SignalAcceptHandOver); err != nil {
		log.Printf("engine: accepting the hand-over of trial run %s: %v", run, err)
		return
	}
	deadline := time.After(recordWait)
	for {
		changed := s.runs.rec.next(run)
		if st, _, _, ok, err := s.runs.find(run); err != nil || !ok || st.Status != machine.StatusWaiting {
			return
		}
		select {
		case <-changed:
		case <-time.After(trialPoll):
		case <-deadline:
			return
		case <-ctx.Done():
			return
		}
	}
}

// stopForTime cancels the set's running trial, waits until its end is on its record, and then
// stops the set `time`, so the stopped set's result counts that trial as ended (data model: 24
// hours passed and the running trial was cancelled). It returns ctx's error when ctx ends first;
// a repeat cancels again, which is no error for a run that has ended.
func (s *sliceServer) stopForTime(ctx context.Context, set trialSetRecord) error {
	if n := len(set.Runs); n > 0 {
		run := set.Runs[n-1]
		if err := s.cancelTrial(ctx, run); err != nil {
			return Retryable(err)
		}
		if err := s.awaitTrialEnd(ctx, run); err != nil {
			return err
		}
	}
	if err := s.endSet(set.ID, gen.TrialSetStateStopped, gen.TrialSetStopReasonTime); err != nil {
		return Retryable(err)
	}
	return nil
}

// awaitTrialOperation waits, at most followPoll (the longest a run operation's follow takes to
// see its run's end), until the ended trial's run operation has written its end too, so the set
// moves on (or ends) once everything of its trial has ended. An operation whose work is not
// running (its workflow held or stopped) does not hold the set longer.
func (s *sliceServer) awaitTrialOperation(ctx context.Context, run string) {
	deadline := time.After(followPoll)
	for {
		if _, open, err := s.ops.openRun(run); err != nil || !open {
			return
		}
		select {
		case <-time.After(trialPoll / 4):
		case <-deadline:
			return
		case <-ctx.Done():
			return
		}
	}
}

// awaitTrialEnd waits until the trial's record shows its end, or until the run is known never to
// have started (closeIfFailed ends a run whose workflow closed with no end on its record).
func (s *sliceServer) awaitTrialEnd(ctx context.Context, run string) error {
	for {
		changed := s.runs.rec.next(run)
		st, _, _, ok, err := s.runs.find(run)
		if err != nil {
			return Retryable(err)
		}
		if ok && st.Status == machine.StatusEnded {
			return nil
		}
		if ended, _, err := s.runs.closeIfFailed(ctx, run); err == nil && ended {
			return nil
		}
		select {
		case <-changed:
		case <-time.After(trialPoll):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// cancelTrial cancels a trial's run through its open run operation (cancelOperation's way), or the
// run itself when none is open; a run that has ended, or has no workflow, is no error.
func (s *sliceServer) cancelTrial(ctx context.Context, run string) error {
	if st, _, _, ok, err := s.runs.find(run); err == nil && ok && st.Status == machine.StatusEnded {
		return nil
	}
	rec, open, err := s.ops.openRun(run)
	if err != nil {
		return err
	}
	if open {
		_, _, _, err := s.ops.cancel(ctx, rec.ID)
		return err
	}
	if err := s.runs.durable.Cancel(ctx, run); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

// cancelTrialSet is the cancellation of a set's run-trials operation: the set ends `cancelled`
// (written from the operation when its acceptance could not write it) and its running trial is
// cancelled; no trial starts after (startTrial reads the set first).
func (s *sliceServer) cancelTrialSet(ctx context.Context, op store.OperationRecord) error {
	if s.trialSets == nil || s.runs == nil {
		return nil
	}
	s.tmu.Lock()
	_, body, err := s.ops.records.Operation(op.ID)
	if err != nil {
		s.tmu.Unlock()
		return err
	}
	set, err := s.openSet(op, body)
	if err == nil && set.State == gen.TrialSetStateRunning {
		at := s.ops.now().UTC()
		set.State, set.EndedAt = gen.TrialSetStateCancelled, &at
		err = s.putSet(set)
	}
	s.tmu.Unlock()
	if err != nil {
		return err
	}
	if n := len(set.Runs); n > 0 {
		return s.cancelTrial(ctx, set.Runs[n-1])
	}
	return nil
}

// trialsOf is the set's started trials with their records.
func (s *sliceServer) trialsOf(set trialSetRecord) ([]trials.Trial, error) {
	out := make([]trials.Trial, 0, len(set.Runs))
	for i, run := range set.Runs {
		events, _, err := s.runs.read(run)
		if err != nil {
			return nil, err
		}
		out = append(out, trials.Trial{Number: i + 1, Run: run, Events: events})
	}
	return out, nil
}

// setView is the set as the contract shows it, with its result computed from its trials' records.
func (s *sliceServer) setView(set trialSetRecord) (gen.TrialSet, error) {
	ts, err := s.trialsOf(set)
	if err != nil {
		return gen.TrialSet{}, err
	}
	steps := make([]string, 0, len(s.runs.pattern.Steps))
	for _, st := range s.runs.pattern.Steps {
		steps = append(steps, st.Name)
	}
	r := trials.Of(steps, ts)
	v := gen.TrialSet{Id: set.ID, Slice: set.Slice, Trials: set.Trials, ProjectPrefix: set.ProjectPrefix, Method: set.Method,
		PartHashes: gen.PartHashes{Goals: set.PartHashes.Goals, Rules: set.PartHashes.Rules, Contract: set.PartHashes.Contract},
		Operation:  set.Operation, State: set.State, Runs: slices.Clone(set.Runs), CreatedAt: set.CreatedAt, EndedAt: set.EndedAt,
		Result: gen.TrialResult{Ended: r.Ended, Done: r.Done, Unstable: r.Unstable, MethodsMixed: r.MethodsMixed,
			Outcomes: make([]gen.TrialOutcome, 0, len(r.Outcomes)), Steps: []gen.StepSpread{}}}
	v.Budget.CostUsd, v.Budget.Hours = float32(set.Budget.CostUSD), set.Budget.Hours
	if set.StoppedBecause != "" {
		why := set.StoppedBecause
		v.StoppedBecause = &why
	}
	for _, o := range r.Outcomes {
		t := gen.TrialOutcome{Trial: o.Trial, Run: o.Run, Method: opt(o.Method)}
		if o.Outcome != "" {
			out := gen.RunOutcome(o.Outcome)
			t.Outcome = &out
		}
		v.Result.Outcomes = append(v.Result.Outcomes, t)
	}
	return v, nil
}

// GetTrialSet shows one set with its result, read from the records: no model is called and no
// record changes. An unknown or erased set is NotFound.
func (s *sliceServer) GetTrialSet(_ context.Context, req gen.GetTrialSetRequestObject) (gen.GetTrialSetResponseObject, error) {
	if _, err := s.trialSetStore(); err != nil {
		return nil, err
	}
	missing := &sl.Error{Code: string(gen.ErrorCodeNotFound), Detail: "trial set " + req.TrialSetId + " does not exist"}
	if !trialSetIDRE.MatchString(req.TrialSetId) {
		return s.refuse("getTrialSet", missing)
	}
	set, err := s.readSet(req.TrialSetId)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return s.refuse("getTrialSet", missing)
	case err != nil:
		return nil, err
	}
	v, err := s.setView(set)
	if err != nil {
		return nil, err
	}
	return gen.GetTrialSet200JSONResponse{TrialSetJSONResponse: gen.TrialSetJSONResponse{Body: v}}, nil
}

// ListTrialSets lists the slice's sets newest first, each as getTrialSet shows it; a page token
// names the number of the last set of its page. An unknown slice is NotFound.
func (s *sliceServer) ListTrialSets(_ context.Context, req gen.ListTrialSetsRequestObject) (gen.ListTrialSetsResponseObject, error) {
	ts, err := s.trialSetStore()
	if err != nil {
		return nil, err
	}
	id := req.SliceIdParameter
	if _, err := s.svc.Get(id); err != nil {
		return s.refuse("listTrialSets", err)
	}
	all, err := ts.TrialSets(id)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(all))
	for set := range all {
		ids = append(ids, set)
	}
	number := func(set string) int { n, _ := strconv.Atoi(strings.TrimPrefix(set, "TS-")); return n }
	slices.SortFunc(ids, func(a, b string) int { return number(b) - number(a) })
	numbers := make([]int, len(ids))
	for i, set := range ids {
		numbers[i] = number(set)
	}
	from, to, next, ok := s.pages.keysetPage(trialSetsList(id), req.Params.PageToken, numbers)
	if !ok {
		return problemResponse(badPageToken()), nil
	}
	list := gen.TrialSetList{Items: make([]gen.TrialSet, 0, to-from), NextPageToken: next}
	for _, set := range ids[from:to] {
		var r trialSetRecord
		if err := json.Unmarshal(all[set], &r); err != nil {
			return nil, fmt.Errorf("trial set %s: %w", set, err)
		}
		if r.Runs == nil {
			r.Runs = []string{}
		}
		v, err := s.setView(r)
		if err != nil {
			return nil, err
		}
		list.Items = append(list.Items, v)
	}
	return gen.ListTrialSets200JSONResponse{TrialSetListJSONResponse: gen.TrialSetListJSONResponse{Body: list}}, nil
}

// setIDs are the IDs of the slice's sets; none when the engine keeps no sets.
func (s *sliceServer) setIDs(slice string) ([]string, error) {
	if s.trialSets == nil {
		return nil, nil
	}
	all, err := s.trialSets.TrialSets(slice)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	return ids, nil
}

func (p problemResponse) VisitStartTrialSetResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitListTrialSetsResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitGetTrialSetResponse(w http.ResponseWriter) error   { return p.write(w) }
func (u unavailableAnswer) VisitStartTrialSetResponse(w http.ResponseWriter) error {
	return u.write(w)
}

// ---- an engine that keeps no slices ----

func (s *server) StartTrialSet(context.Context, gen.StartTrialSetRequestObject) (gen.StartTrialSetResponseObject, error) {
	return gen.StartTrialSet404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the slice")}, nil
}

func (s *server) ListTrialSets(context.Context, gen.ListTrialSetsRequestObject) (gen.ListTrialSetsResponseObject, error) {
	return gen.ListTrialSets404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the slice")}, nil
}

func (s *server) GetTrialSet(context.Context, gen.GetTrialSetRequestObject) (gen.GetTrialSetResponseObject, error) {
	return gen.GetTrialSet404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the trial set")}, nil
}
