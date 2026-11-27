package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
)

// Job is one unit of work behind a state-changing call.
type Job struct {
	Kind   gen.OperationKind // the operation's kind, e.g. propose-slice
	Target string            // slice, run, captured pattern or trial set ID the operation concerns (S-<n>, RUN-<n>, a pattern ID or TS-<n>)
	Body   []byte            // the request body as received
}

// WorkResult is the result of a succeeded job; exactly one field is set.
type WorkResult struct{ Slice, Run, ReusedSlice, CapturedPattern, TrialSet string }

// WorkError ends the operation `failed` at once, without retries; its Code and Detail become
// the operation's `error` problem.
type WorkError struct {
	Code   gen.ErrorCode
	Detail string
	// Failures are the failing checks behind the refusal, kept in the error's `failures`.
	Failures []slices.Check
}

func (e *WorkError) Error() string { return string(e.Code) + ": " + e.Detail }

// Retryable wraps err so the work is tried again by the engine contract's retry rule
// (x-limits retry-attempts, with its waits), then the operation ends `failed` with
// `failed after <n> attempts: <err>` in `error.detail`. Any other error from the work is not
// retried and also ends the operation `failed`.
func Retryable(err error) error { return &retryable{err} }

// IsRetryable reports whether err is, or wraps, one that Retryable made.
func IsRetryable(err error) bool {
	var r *retryable
	return errors.As(err, &r)
}

type retryable struct{ err error }

func (r *retryable) Error() string { return r.err.Error() }
func (r *retryable) Unwrap() error { return r.err }

// failedCode is the code of an operation whose work failed other than with a WorkError,
// including when its retries are exhausted.
const failedCode = gen.ErrorCodeOperationFailed

// OperationsQueue is the task queue the operation workflows run on.
const OperationsQueue = "operations"

// Operations is the API's port to the durable service that runs each accepted operation's work
// to one end, across platform stops, as a workflow with the operation's ID (data model, "The
// engine's durable interface"). Package durable implements it on Temporal; without it the
// engine runs the same work in its own process.
type Operations interface {
	// Ready says whether the durable service answers: nil, or an error wrapping ErrUnavailable.
	Ready(ctx context.Context) error
	// Start starts the operation's workflow; one that exists, open or closed, is no error and is
	// not started again.
	Start(ctx context.Context, s OperationStart) error
	// Open says whether the operation's workflow has not closed; ErrNotFound when it has none.
	Open(ctx context.Context, id string) (bool, error)
	// Cancel cancels the operation's workflow, which then ends the operation `cancelled`;
	// ErrNotFound when it has none.
	Cancel(ctx context.Context, id string) error
	// Delete removes the operation's workflow; none is no error.
	Delete(ctx context.Context, id string) error
}

// OperationStart starts an operation's workflow: its ID, the contract operation that accepted
// it (Kind, its operationId) and its target, on TaskQueue (OperationsQueue), never twice
// (RejectDuplicate).
type OperationStart struct {
	ID, Kind, Target string
	TaskQueue        string
	RejectDuplicate  bool
}

// OperationStatus is an operation's status as the engine contract names it; OperationFailed and
// OperationCancelled are the ends an Operator's End writes.
type OperationStatus = gen.OperationStatus

const (
	OperationFailed    = gen.OperationStatusFailed
	OperationCancelled = gen.OperationStatusCancelled
)

// Operator does the work of an accepted operation, for the durable service's worker: Do does
// what the request asked, from the operation's record and body, and writes its end; End writes
// an end the workflow decided (its retries used up, or its cancellation). A repeat of Do finds
// the work it did before and ends the same way; the first end written wins.
type Operator interface {
	// Do returns nil once the operation has ended (or is no longer kept), a Retryable error to
	// be tried again by the retry rule, or ctx's error when ctx ended first.
	Do(ctx context.Context, id string) error
	// End writes the end: `failed` with detail, or `cancelled`.
	End(ctx context.Context, id string, status OperationStatus, detail string) error
}

// OperationRecords keeps the operation records and their request bodies (ADR 0072);
// *store.Store has it, durably.
type OperationRecords interface {
	NextOperationID() (string, error)
	PutOperation(r store.OperationRecord, body []byte) error
	// Operation returns the record and, until it has ended, its body; an error wrapping
	// store.ErrNotFound when none is kept.
	Operation(id string) (store.OperationRecord, []byte, error)
	OperationByKey(key string) (store.OperationRecord, bool, error)
	// Operations returns every record, ended or not, by number, lowest first.
	Operations() ([]store.OperationRecord, error)
	OpenOperations() ([]store.OperationRecord, error)
	EndOperation(id string, at time.Time, status string, result, problem json.RawMessage) (bool, error)
	RemoveOperationsEndedBefore(t time.Time) ([]string, error)
	DeleteOperation(id string) error
}

// operationKinds is the kind clients see of each contract operation that accepts one.
var operationKinds = map[string]gen.OperationKind{
	"createSlice": gen.OperationKindProposeSlice, "deleteSlice": gen.OperationKindDeleteSlice,
	"replaceGoals": gen.OperationKindReplaceGoals, "replaceRules": gen.OperationKindReplaceRules,
	"replaceContract": gen.OperationKindReplaceContract, "answerQuestions": gen.OperationKindAnswerQuestions,
	"approvePart": gen.OperationKindApprovePart, "startRun": gen.OperationKindRun, "replayRun": gen.OperationKindRun,
	"resumeRun": gen.OperationKindRun, "acceptHandOver": gen.OperationKindRun,
	"captureRun": gen.OperationKindCaptureRun, "replaceParameters": gen.OperationKindReplaceParameters,
	"makeProofSlice": gen.OperationKindMakeProofSlice, "approveCapturedPattern": gen.OperationKindApproveCapturedPattern,
	"withdrawCapturedPattern": gen.OperationKindWithdrawCapturedPattern, "eraseCapturedPattern": gen.OperationKindEraseCapturedPattern,
	"startTrialSet": gen.OperationKindRunTrials,
}

// isTrialsKind reports whether the contract operation kind accepts a trial set's operation, whose
// target is the set's ID.
func isTrialsKind(kind string) bool { return operationKinds[kind] == gen.OperationKindRunTrials }

func isRunKind(kind string) bool { return operationKinds[kind] == gen.OperationKindRun }

// isCapturedKind reports whether the contract operation kind accepts an operation on a captured
// pattern, whose target is the captured pattern's ID.
func isCapturedKind(kind string) bool {
	switch operationKinds[kind] {
	case gen.OperationKindCaptureRun, gen.OperationKindReplaceParameters, gen.OperationKindMakeProofSlice,
		gen.OperationKindApproveCapturedPattern, gen.OperationKindWithdrawCapturedPattern, gen.OperationKindEraseCapturedPattern:
		return true
	}
	return false
}

// sweepEvery is how often the outbox and the sweep run after the engine's start.
const sweepEvery = time.Minute

// Ends the engine writes itself.
const (
	detailRunNotStarted = "the run was not started; send the request again"
	detailClosedNoEnd   = "the operation's workflow closed with no end"
)

// errNotRecorded: the operation's record could not be written; nothing was accepted.
var errNotRecorded = errors.New("the operation could not be recorded")

// operations accepts operations into their records, has the durable service run their work
// (Operator), and answers for them from the records: the ID, the end and the key are the
// record's; whether the work runs, the workflow's.
type operations struct {
	records OperationRecords
	durable Operations
	now     func() time.Time
	keep    time.Duration // how long an ended operation and its key are kept
	// work does what an operation's request asked, from its record and body; set once by the
	// handler before anything is served.
	work func(ctx context.Context, rec store.OperationRecord, body []byte) (WorkResult, error)
	// cancelRun cancels a run operation's run; nil when the engine runs no runs.
	cancelRun func(ctx context.Context, run string) error
	// progress is a run operation's progress, read from its run's record; nil for none.
	progress func(run string) *operationProgress
	// cancelTrials ends a trial set cancelled and cancels its running trial, when its run-trials
	// operation is cancelled; nil when the engine keeps no trial sets.
	cancelTrials func(ctx context.Context, rec store.OperationRecord) error
	// failTrials ends a trial set `failed` once its run-trials operation has ended failed (data
	// model, "States" rule 1); nil when the engine keeps no trial sets.
	failTrials func(ctx context.Context, rec store.OperationRecord)
}

type operationProgress = struct {
	Done  int     `json:"done"`
	Step  *string `json:"step,omitempty"`
	Total int     `json:"total"`
}

var _ Operator = (*operations)(nil)

// requestHashes are the hashes of a state-changing request the front computed: body, of its
// method, path and body; key, of the token and its Idempotency-Key, "" without one.
type requestHashes struct{ body, key string }

type requestHashesKey struct{}

// record writes a new operation's record and the request's body for the contract operation
// opID on target; an error wrapping errNotRecorded wrote nothing.
func (o *operations) record(ctx context.Context, opID, target string) (store.OperationRecord, error) {
	body, _ := ctx.Value(rawBodyKey{}).([]byte)
	return o.recordBody(ctx, opID, target, body)
}

// recordBody is record with the body its work reads in place of the request's: the request's
// hashes (its key and body) stay the request's, so a repeat under its Idempotency-Key is judged
// as any other.
func (o *operations) recordBody(ctx context.Context, opID, target string, body []byte) (store.OperationRecord, error) {
	h, _ := ctx.Value(requestHashesKey{}).(requestHashes)
	if h.body == "" {
		sum := sha256.Sum256(body)
		h.body = hex.EncodeToString(sum[:])
	}
	id, err := o.records.NextOperationID()
	if err != nil {
		log.Printf("engine: an operation ID for %s: %v", opID, err)
		return store.OperationRecord{}, errNotRecorded
	}
	rec := store.OperationRecord{ID: id, Kind: opID, Target: target, Key: h.key, Body: h.body, AcceptedAt: o.now().UTC()}
	if body == nil {
		body = []byte{}
	}
	if err := o.records.PutOperation(rec, body); err != nil {
		log.Printf("engine: recording operation %s: %v", id, err)
		return store.OperationRecord{}, errNotRecorded
	}
	return rec, nil
}

// launch starts the workflow of a recorded operation. The operation is accepted either way: a
// workflow that does not start is started by the outbox (sweep).
func (o *operations) launch(ctx context.Context, rec store.OperationRecord) {
	if err := o.durable.Start(ctx, startOf(rec)); err != nil {
		log.Printf("engine: starting the workflow of operation %s: %v; the outbox starts it", rec.ID, err)
	}
}

// accept records an operation and starts its workflow, and returns it as accepted.
func (o *operations) accept(ctx context.Context, opID, target string) (gen.Operation, error) {
	rec, err := o.record(ctx, opID, target)
	if err != nil {
		return gen.Operation{}, err
	}
	o.launch(ctx, rec)
	return acceptedView(rec), nil
}

func startOf(rec store.OperationRecord) OperationStart {
	return OperationStart{ID: rec.ID, Kind: rec.Kind, Target: rec.Target, TaskQueue: OperationsQueue, RejectDuplicate: true}
}

// acceptedView is the operation as the 202 that accepted it showed it.
func acceptedView(rec store.OperationRecord) gen.Operation {
	return gen.Operation{Id: rec.ID, Kind: operationKinds[rec.Kind], Target: rec.Target, Status: gen.OperationStatusAccepted,
		CreatedAt: rec.AcceptedAt, UpdatedAt: rec.AcceptedAt}
}

// kept reports whether an operation is still answered for: until keep after its end.
func (o *operations) kept(rec store.OperationRecord) bool {
	return rec.EndedAt == nil || !o.now().After(rec.EndedAt.Add(o.keep))
}

// keyHeld reports whether a record still answers its Idempotency-Key: for keep from the first
// request (accepted_at), as the contract's x-limits say.
func (o *operations) keyHeld(rec store.OperationRecord) bool {
	return !o.now().After(rec.AcceptedAt.Add(o.keep))
}

// find returns a kept operation's record; found is false for an operation not kept.
func (o *operations) find(id string) (rec store.OperationRecord, found bool, err error) {
	if !operationIDRE.MatchString(id) {
		return rec, false, nil
	}
	rec, _, err = o.records.Operation(id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return rec, false, nil
	case err != nil:
		return rec, false, err
	}
	return rec, o.kept(rec), nil
}

// view is the operation as clients see it: its end from the record; before that `running` while
// its workflow is open or the durable service cannot be asked, `accepted` while it has none.
func (o *operations) view(ctx context.Context, rec store.OperationRecord) gen.Operation {
	v := acceptedView(rec)
	if rec.EndedAt != nil {
		v.Status, v.UpdatedAt = gen.OperationStatus(rec.Status), *rec.EndedAt
		if len(rec.Result) > 0 {
			var res opResult
			if json.Unmarshal(rec.Result, &res) == nil {
				v.Result = &res
			}
		}
		if len(rec.Error) > 0 {
			var p gen.Problem
			if json.Unmarshal(rec.Error, &p) == nil {
				v.Error = &p
			}
		}
	} else if _, err := o.durable.Open(ctx, rec.ID); !errors.Is(err, ErrNotFound) {
		v.Status = gen.OperationStatusRunning
	}
	if isRunKind(rec.Kind) && o.progress != nil {
		v.Progress = o.progress(rec.Target)
	}
	return v
}

// get returns a kept operation as clients see it.
func (o *operations) get(ctx context.Context, id string) (gen.Operation, bool, error) {
	rec, found, err := o.find(id)
	if err != nil || !found {
		return gen.Operation{}, false, err
	}
	return o.view(ctx, rec), true, nil
}

// cancel cancels a kept operation that has not ended: a run operation's run first (a run that
// has ended is no error), then its end `cancelled`, then its workflow; a run-trials operation's set
// first (it ends `cancelled` and its running trial is cancelled), then its end and its workflow
// likewise; any other operation's
// workflow, which ends it `cancelled` (or its record, while it has no workflow). It reports
// whether the operation is kept and whether it had ended; an error wrapping ErrUnavailable
// cancelled nothing.
func (o *operations) cancel(ctx context.Context, id string) (view gen.Operation, found, hadEnded bool, err error) {
	rec, found, err := o.find(id)
	switch {
	case err != nil || !found:
		return gen.Operation{}, found, false, err
	case rec.EndedAt != nil:
		return o.view(ctx, rec), true, true, nil
	}
	if isRunKind(rec.Kind) || isTrialsKind(rec.Kind) {
		switch {
		case isTrialsKind(rec.Kind) && o.cancelTrials != nil:
			if err := o.cancelTrials(ctx, rec); err != nil && !errors.Is(err, ErrNotFound) {
				return gen.Operation{}, true, false, err
			}
		case isRunKind(rec.Kind) && o.cancelRun != nil:
			if err := o.cancelRun(ctx, rec.Target); err != nil && !errors.Is(err, ErrNotFound) {
				return gen.Operation{}, true, false, err
			}
		}
		if err := o.end(rec.ID, gen.OperationStatusCancelled, nil, nil); err != nil {
			return gen.Operation{}, true, false, err
		}
		if err := o.durable.Cancel(ctx, rec.ID); err != nil && !errors.Is(err, ErrNotFound) {
			log.Printf("engine: cancelling the workflow of operation %s: %v", rec.ID, err)
		}
	} else {
		switch err := o.durable.Cancel(ctx, rec.ID); {
		case errors.Is(err, ErrNotFound): // no workflow yet: nothing else ends it
			if err := o.end(rec.ID, gen.OperationStatusCancelled, nil, nil); err != nil {
				return gen.Operation{}, true, false, err
			}
		case err != nil:
			return gen.Operation{}, true, false, err
		}
	}
	if again, _, err := o.records.Operation(rec.ID); err == nil {
		rec = again
	}
	return o.view(ctx, rec), true, false, nil
}

// end writes an operation's end once; a later end, or an operation no longer kept, changes
// nothing. Once a run-trials operation's recorded end is `failed`, its set ends `failed` too
// (failTrials), whichever end wrote it.
func (o *operations) end(id string, status gen.OperationStatus, res *opResult, problem *gen.Problem) error {
	var result, prob json.RawMessage
	if res != nil {
		result, _ = json.Marshal(res) // a struct of strings cannot fail
	}
	if problem != nil {
		prob, _ = json.Marshal(problem)
	}
	_, err := o.records.EndOperation(id, o.now().UTC(), string(status), result, prob)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err == nil && status == gen.OperationStatusFailed && o.failTrials != nil {
		if rec, _, rerr := o.records.Operation(id); rerr == nil && isTrialsKind(rec.Kind) && rec.Status == string(gen.OperationStatusFailed) {
			o.failTrials(context.Background(), rec)
		}
	}
	return err
}

// problemDetail is the detail of an ended operation's problem, "" for none.
func problemDetail(rec store.OperationRecord) string {
	var p gen.Problem
	if len(rec.Error) == 0 || json.Unmarshal(rec.Error, &p) != nil || p.Detail == nil {
		return ""
	}
	return *p.Detail
}

// Do does the work the operation's request asked for, from its record and body, and writes its
// end: succeeded with its result, or failed with its problem. A Retryable error from the work is
// returned for the workflow to try again; so is a failure to read the record or write the end.
func (o *operations) Do(ctx context.Context, id string) error {
	rec, body, err := o.records.Operation(id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil // erased with its slice: nothing to end
	case err != nil:
		return Retryable(fmt.Errorf("reading operation %s: %w", id, err))
	case rec.EndedAt != nil:
		return nil
	}
	res, err := runOnce(ctx, func(ctx context.Context) (WorkResult, error) {
		if o.work == nil {
			return WorkResult{}, errors.New("this engine has no worker for this kind of operation yet")
		}
		return o.work(ctx, rec, body)
	})
	var werr *WorkError
	switch {
	case err == nil:
		err = o.end(id, gen.OperationStatusSucceeded, res, nil)
	case ctx.Err() != nil:
		return ctx.Err()
	case IsRetryable(err):
		return err
	case errors.As(err, &werr):
		p := newProblem(0, werr.Code, werr.Detail)
		if len(werr.Failures) > 0 {
			f := checkResults(werr.Failures)
			p.Failures = &f
		}
		err = o.end(id, gen.OperationStatusFailed, nil, &p)
	default:
		p := newProblem(0, failedCode, err.Error())
		err = o.end(id, gen.OperationStatusFailed, nil, &p)
	}
	if err != nil {
		return Retryable(fmt.Errorf("writing the end of operation %s: %w", id, err))
	}
	return nil
}

// End writes the end the operation's workflow decided: `failed` (code OperationFailed) with
// detail, or `cancelled`.
func (o *operations) End(_ context.Context, id string, status gen.OperationStatus, detail string) error {
	if !ended(status) {
		return fmt.Errorf("operation %s: %q is no end", id, status)
	}
	var p *gen.Problem
	if status == gen.OperationStatusFailed {
		pr := newProblem(0, failedCode, detail)
		p = &pr
	}
	return o.end(id, status, nil, p)
}

func ended(s gen.OperationStatus) bool {
	return s == gen.OperationStatusSucceeded || s == gen.OperationStatusFailed || s == gen.OperationStatusCancelled
}

// openRun returns the open operation of any run kind on the run, if any.
func (o *operations) openRun(run string) (store.OperationRecord, bool, error) {
	open, err := o.records.OpenOperations()
	if err != nil {
		return store.OperationRecord{}, false, err
	}
	for _, r := range open {
		if isRunKind(r.Kind) && r.Target == run {
			return r, true, nil
		}
	}
	return store.OperationRecord{}, false, nil
}

// sweep is the outbox and the sweep (data model, "Outbox and sweep"): an open record without a
// workflow gets one; an open record whose workflow has closed ends failed; a record ended more
// than keep ago is removed. youngest skips records accepted after it, whose accepting request
// may still be starting them; at start nothing is skipped.
func (o *operations) sweep(ctx context.Context, youngest time.Time) {
	open, err := o.records.OpenOperations()
	if err != nil {
		log.Printf("engine: reading the open operations: %v", err)
		return
	}
	for _, rec := range open {
		if !youngest.IsZero() && rec.AcceptedAt.After(youngest) {
			continue
		}
		isOpen, err := o.durable.Open(ctx, rec.ID)
		switch {
		case errors.Is(err, ErrNotFound):
			if err := o.durable.Start(ctx, startOf(rec)); err != nil {
				log.Printf("engine: starting the workflow of operation %s: %v", rec.ID, err)
			}
		case err != nil || isOpen:
		default: // asked first, read after: an end written meanwhile is kept
			if again, _, err := o.records.Operation(rec.ID); err == nil && again.EndedAt == nil {
				if err := o.End(ctx, rec.ID, gen.OperationStatusFailed, detailClosedNoEnd); err != nil {
					log.Printf("engine: ending operation %s: %v", rec.ID, err)
				}
			}
		}
	}
	if _, err := o.records.RemoveOperationsEndedBefore(o.now().Add(-o.keep)); err != nil {
		log.Printf("engine: removing the operations ended more than %s ago: %v", o.keep, err)
	}
}

// sweepEveryMinute runs sweep every sweepEvery until ctx ends.
func (o *operations) sweepEveryMinute(ctx context.Context, running *sync.WaitGroup) {
	if running != nil {
		running.Add(1)
	}
	go func() {
		if running != nil {
			defer running.Done()
		}
		t := time.NewTicker(sweepEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				o.sweep(ctx, o.now().Add(-sweepEvery))
			}
		}
	}()
}

// writeAccepted answers a request whose Idempotency-Key a record holds with that record's first
// answer: 202 and the operation as accepted.
func writeAccepted(w http.ResponseWriter, rec store.OperationRecord) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(acceptedView(rec))
}

type opResult = struct {
	CapturedPattern *gen.PatternId  `json:"captured_pattern,omitempty"`
	ReusedSlice     *gen.SliceId    `json:"reused_slice,omitempty"`
	Run             *gen.RunId      `json:"run,omitempty"`
	Slice           *gen.SliceId    `json:"slice,omitempty"`
	TrialSet        *gen.TrialSetId `json:"trial_set,omitempty"`
}

// runOnce runs the work once; a panic or a result without exactly one field is a failure.
func runOnce(ctx context.Context, work func(ctx context.Context) (WorkResult, error)) (res *opResult, err error) {
	defer func() {
		if p := recover(); p != nil {
			res, err = nil, fmt.Errorf("the work stopped unexpectedly: %v", p)
		}
	}()
	wr, err := work(ctx)
	if err != nil {
		return nil, err
	}
	res = &opResult{}
	set := 0
	for _, f := range []struct {
		v   string
		dst **string
	}{{wr.Slice, &res.Slice}, {wr.Run, &res.Run}, {wr.ReusedSlice, &res.ReusedSlice}, {wr.CapturedPattern, &res.CapturedPattern},
		{wr.TrialSet, &res.TrialSet}} {
		if f.v != "" {
			v := f.v
			*f.dst = &v
			set++
		}
	}
	if set != 1 {
		return nil, fmt.Errorf("the work returned %d results, want exactly 1", set)
	}
	return res, nil
}

func waitOrCancel(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// localOperations is Operations in the engine's own process, for an engine without a durable
// service (Config.Operations nil): each operation's work runs in a goroutine with the same
// Operator and the same retry rule as the Operation workflow, and does not survive a stop.
type localOperations struct {
	op      Operator
	base    context.Context // the work's context ends with it
	running *sync.WaitGroup // counts each operation's work, when set
	sleep   func(ctx context.Context, d time.Duration)

	mu  sync.Mutex
	wfs map[string]*localWork
}

type localWork struct {
	cancel            context.CancelFunc
	cancelled, closed bool
}

var _ Operations = (*localOperations)(nil)

func (l *localOperations) Ready(context.Context) error { return nil }

func (l *localOperations) Start(_ context.Context, s OperationStart) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.wfs[s.ID]; ok {
		return nil
	}
	ctx, cancel := context.WithCancel(l.base)
	w := &localWork{cancel: cancel}
	l.wfs[s.ID] = w
	if l.running != nil {
		l.running.Add(1)
	}
	go func() {
		if l.running != nil {
			defer l.running.Done()
		}
		defer cancel()
		l.run(ctx, s.ID, w)
		l.mu.Lock()
		w.closed = true
		l.mu.Unlock()
	}()
	return nil
}

// run does the operation's work by the retry rule, as the Operation workflow does.
func (l *localOperations) run(ctx context.Context, id string, w *localWork) {
	rule := RetryRule()
	stopped := func() bool {
		if ctx.Err() == nil {
			return false
		}
		l.mu.Lock()
		cancelled := w.cancelled
		l.mu.Unlock()
		if cancelled {
			_ = l.op.End(context.Background(), id, gen.OperationStatusCancelled, "")
		}
		return true
	}
	for try := 0; ; try++ {
		err := l.op.Do(ctx, id)
		switch {
		case err == nil, stopped():
			return
		case !IsRetryable(err):
			_ = l.op.End(context.Background(), id, gen.OperationStatusFailed, err.Error())
			return
		case try == rule.Retries:
			_ = l.op.End(context.Background(), id, gen.OperationStatusFailed, fmt.Sprintf("failed after %d attempts: %v", try+1, err))
			return
		}
		l.sleep(ctx, rule.Wait(try, rand.Float64))
		if stopped() {
			return
		}
	}
}

func (l *localOperations) Open(_ context.Context, id string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.wfs[id]
	if !ok {
		return false, fmt.Errorf("operation %s: %w", id, ErrNotFound)
	}
	return !w.closed, nil
}

func (l *localOperations) Cancel(_ context.Context, id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.wfs[id]
	if !ok {
		return fmt.Errorf("operation %s: %w", id, ErrNotFound)
	}
	if !w.closed {
		w.cancelled = true
		w.cancel()
	}
	return nil
}

func (l *localOperations) Delete(_ context.Context, id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.wfs, id)
	return nil
}

// memoryRecords is OperationRecords in memory, for a Config.Store that keeps none: the same
// rules as the store's, lost at a stop.
type memoryRecords struct {
	mu   sync.Mutex
	last int
	recs map[string]*memoryRecord
}

type memoryRecord struct {
	rec  store.OperationRecord
	body []byte
}

var _ OperationRecords = (*memoryRecords)(nil)

func (m *memoryRecords) NextOperationID() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.last++
	return "OP-" + strconv.Itoa(m.last), nil
}

func (m *memoryRecords) PutOperation(r store.OperationRecord, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.recs[r.ID]; ok || r.ID == "" {
		return fmt.Errorf("operation %q is recorded already or has no ID", r.ID)
	}
	m.recs[r.ID] = &memoryRecord{rec: r, body: append([]byte{}, body...)}
	return nil
}

func (m *memoryRecords) Operation(id string) (store.OperationRecord, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.recs[id]
	if !ok {
		return store.OperationRecord{}, nil, fmt.Errorf("operation %s: %w", id, store.ErrNotFound)
	}
	if r.rec.EndedAt != nil {
		return r.rec, nil, nil
	}
	return r.rec, append([]byte{}, r.body...), nil
}

func (m *memoryRecords) OperationByKey(key string) (store.OperationRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var found store.OperationRecord
	ok := false
	for _, r := range m.recs {
		if key != "" && r.rec.Key == key && (!ok || opNumber(r.rec.ID) > opNumber(found.ID)) {
			found, ok = r.rec, true
		}
	}
	return found, ok, nil
}

func (m *memoryRecords) Operations() ([]store.OperationRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := make([]store.OperationRecord, 0, len(m.recs))
	for _, r := range m.recs {
		all = append(all, r.rec)
	}
	sortByNumber(all)
	return all, nil
}

func (m *memoryRecords) OpenOperations() ([]store.OperationRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	open := []store.OperationRecord{}
	for _, r := range m.recs {
		if r.rec.EndedAt == nil {
			open = append(open, r.rec)
		}
	}
	sortByNumber(open)
	return open, nil
}

func (m *memoryRecords) EndOperation(id string, at time.Time, status string, result, problem json.RawMessage) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.recs[id]
	switch {
	case !ok:
		return false, fmt.Errorf("operation %s: %w", id, store.ErrNotFound)
	case r.rec.EndedAt != nil:
		return false, nil
	}
	r.rec.EndedAt, r.rec.Status, r.rec.Result, r.rec.Error, r.body = &at, status, result, problem, nil
	return true, nil
}

func (m *memoryRecords) RemoveOperationsEndedBefore(t time.Time) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := []string{}
	for id, r := range m.recs {
		if r.rec.EndedAt != nil && r.rec.EndedAt.Before(t) {
			delete(m.recs, id)
			removed = append(removed, id)
		}
	}
	return removed, nil
}

func (m *memoryRecords) DeleteOperation(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.recs, id)
	return nil
}

func opNumber(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "OP-"))
	return n
}

func sortByNumber(rs []store.OperationRecord) {
	sort.Slice(rs, func(i, j int) bool { return opNumber(rs[i].ID) < opNumber(rs[j].ID) })
}

// operationIDRE is the contract's form of an operation ID, `OP-<n>`.
var operationIDRE = regexp.MustCompile(`^OP-[1-9][0-9]*$`)
