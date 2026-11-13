package api

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/slices"
)

// Job is one unit of work behind a state-changing call.
type Job struct {
	Kind   gen.OperationKind // the operation's kind, e.g. propose-slice
	Target string            // slice or run ID the operation concerns (S-<n> or RUN-<n>)
	Body   []byte            // the request body as received
}

// WorkResult is the result of a succeeded job; exactly one field is set.
type WorkResult struct{ Slice, Run, ReusedSlice string }

// WorkError ends the operation `failed` at once, without retries; its Code and Detail become
// the operation's `error` problem.
type WorkError struct {
	Code   gen.ErrorCode
	Detail string
	// Failures are the failing checks behind the refusal, kept in the error's `failures`.
	Failures []slices.Check
}

func (e *WorkError) Error() string { return string(e.Code) + ": " + e.Detail }

// Retryable wraps err so the job is retried with exponential backoff and jitter up to 3 times
// after the first attempt, then the operation stays visible as `failed` with err's text in
// `error.detail`. Any other error from Work is not retried and also ends the operation `failed`.
func Retryable(err error) error { return &retryable{err} }

type retryable struct{ err error }

func (r *retryable) Error() string { return r.err.Error() }
func (r *retryable) Unwrap() error { return r.err }

const (
	retries     = 3
	backoffBase = time.Second
	backoffCap  = 30 * time.Second
)

// failedCode is the code of an operation whose work failed other than with a WorkError,
// including when its retries are exhausted.
const failedCode = gen.ErrorCodeOperationFailed

// operations keeps every operation and runs its job: the local form of a queue and a worker
// pool, with failed operations kept visible as the dead letter (research D7).
type operations struct {
	base  context.Context // operations are cancelled when it is done
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration)
	work  func(ctx context.Context, job Job) (WorkResult, error)

	mu   sync.Mutex
	ops  map[string]*operation
	last int
}

type operation struct {
	view   gen.Operation
	cancel context.CancelFunc
}

func ended(s gen.OperationStatus) bool {
	return s == gen.OperationStatusSucceeded || s == gen.OperationStatusFailed || s == gen.OperationStatusCancelled
}

// start records a new operation and runs its job with the host's Work in its own goroutine.
func (s *operations) start(job Job) gen.Operation {
	return s.startWith(job.Kind, job.Target, func(ctx context.Context) (WorkResult, error) {
		if s.work == nil {
			return WorkResult{}, errors.New("this engine has no worker for this kind of operation yet")
		}
		return s.work(ctx, job)
	})
}

// startWith records a new operation of kind on target and runs work in its own goroutine, with
// the same lifecycle, retries and cancellation as any other job.
func (s *operations) startWith(kind gen.OperationKind, target string, work func(ctx context.Context) (WorkResult, error)) gen.Operation {
	return s.launch(s.reserve(kind, target), work)
}

// reserve gives out the next operation ID for an operation of kind on target without recording
// it: until launch, the operation does not exist for any caller. An operation reserved and never
// launched leaves its ID unused.
func (s *operations) reserve(kind gen.OperationKind, target string) *operation {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last++
	now := s.now()
	return &operation{view: gen.Operation{
		Id: "OP-" + strconv.Itoa(s.last), Kind: kind, Target: target,
		Status: gen.OperationStatusAccepted, CreatedAt: now, UpdatedAt: now,
	}}
}

// launch records a reserved operation and runs work in its own goroutine.
func (s *operations) launch(op *operation, work func(ctx context.Context) (WorkResult, error)) gen.Operation {
	ctx, cancel := context.WithCancel(s.base)
	s.mu.Lock()
	op.cancel = cancel
	s.ops[op.view.Id] = op
	view := op.view
	s.mu.Unlock()
	go s.run(ctx, op, work)
	return view
}

func (s *operations) get(id string) (gen.Operation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op, ok := s.ops[id]
	if !ok {
		return gen.Operation{}, false
	}
	return op.view, true
}

// cancel asks the operation's job to stop; the operation ends `cancelled` when it has. It
// reports whether the operation exists and whether it had already ended.
func (s *operations) cancelOp(id string) (view gen.Operation, found, hadEnded bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op, ok := s.ops[id]
	if !ok {
		return gen.Operation{}, false, false
	}
	if ended(op.view.Status) {
		return op.view, true, true
	}
	op.cancel()
	return op.view, true, false
}

func (s *operations) set(op *operation, change func(v *gen.Operation)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(&op.view)
	op.view.UpdatedAt = s.now()
}

func (s *operations) run(ctx context.Context, op *operation, work func(ctx context.Context) (WorkResult, error)) {
	defer op.cancel()
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			s.set(op, func(v *gen.Operation) { v.Status = gen.OperationStatusCancelled })
			return
		}
		s.set(op, func(v *gen.Operation) { v.Status = gen.OperationStatusRunning })
		res, err := runOnce(ctx, work)
		var werr *WorkError
		var rerr *retryable
		switch {
		case err == nil:
			s.set(op, func(v *gen.Operation) { v.Status, v.Result = gen.OperationStatusSucceeded, res })
			return
		case ctx.Err() != nil:
			s.set(op, func(v *gen.Operation) { v.Status = gen.OperationStatusCancelled })
			return
		case errors.As(err, &werr):
			p := newProblem(0, werr.Code, werr.Detail)
			if len(werr.Failures) > 0 {
				f := checkResults(werr.Failures)
				p.Failures = &f
			}
			s.fail(op, p)
			return
		case errors.As(err, &rerr) && attempt < retries:
			s.sleep(ctx, backoff(attempt))
		case errors.As(err, &rerr):
			s.fail(op, newProblem(0, failedCode, fmt.Sprintf("failed after %d attempts: %v", attempt+1, err)))
			return
		default:
			s.fail(op, newProblem(0, failedCode, err.Error()))
			return
		}
	}
}

func (s *operations) fail(op *operation, p gen.Problem) {
	s.set(op, func(v *gen.Operation) { v.Status, v.Error = gen.OperationStatusFailed, &p })
}

type opResult = struct {
	ReusedSlice *gen.SliceId `json:"reused_slice,omitempty"`
	Run         *gen.RunId   `json:"run,omitempty"`
	Slice       *gen.SliceId `json:"slice,omitempty"`
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
	}{{wr.Slice, &res.Slice}, {wr.Run, &res.Run}, {wr.ReusedSlice, &res.ReusedSlice}} {
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

// backoff is the wait before retry n (0-based): full jitter over an exponential window.
func backoff(n int) time.Duration {
	window := min(backoffCap, backoffBase<<n)
	return rand.N(window + 1)
}

func waitOrCancel(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}
