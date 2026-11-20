package durable

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/agentic-platform/engine/api"
	"github.com/agentic-platform/engine/api/gen"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// The Operation workflow does one accepted operation's work to one end (data model, "Operation
// record"): its one activity, DoOperation, does the work from the operation's record and body in
// the engine store and writes the end there. A retryable failure is tried again by the declared
// retry rule (src/retry, the engine contract's x-limits) after durable sleeps, so the retries
// made count across a stop; when they are used up, EndOperation writes `failed`. A stop (the
// activity's heartbeat lost) repeats DoOperation without counting a retry. A cancelled workflow
// writes `cancelled`. The workflow holds only the operation's ID, kind and target and its
// retries; never the request body or the outcome (FR-016).

// The Operation workflow's activities, as registered.
const (
	DoOperation  = "DoOperation"
	EndOperation = "EndOperation"
)

// Retryable is the application error type of a DoOperation failure the retry rule retries.
const Retryable = "Retryable"

// operationLimit bounds one DoOperation call: a run operation follows its run, which waits for
// the developer at most 14 days after a hand-over (data model, "Run states"), so the bound only
// ends a call that no longer answers.
const operationLimit = 30 * 24 * time.Hour

// OperationInput starts an operation's workflow: its ID, the contract operation that accepted it
// and its target.
type OperationInput struct{ ID, Kind, Target string }

// EndOperationInput is the end the workflow writes: `failed` with Detail, or `cancelled`.
type EndOperationInput struct{ ID, Status, Detail string }

// Operation runs one operation's work to its end.
func Operation(ctx workflow.Context, in OperationInput) error {
	rule := api.RetryRule()
	random := func() float64 {
		var f float64
		_ = workflow.SideEffect(ctx, func(workflow.Context) any { return randomFraction() }).Get(&f)
		return f
	}
	opts := workflow.ActivityOptions{StartToCloseTimeout: operationLimit, HeartbeatTimeout: heartbeatTimeout,
		RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1}}
	for try := 0; ; {
		actx := workflow.WithActivityOptions(ctx, opts)
		err := workflow.ExecuteActivity(actx, DoOperation, in.ID).Get(actx, nil)
		switch {
		case err == nil:
			return nil
		case ctx.Err() != nil:
			return endOperation(ctx, EndOperationInput{ID: in.ID, Status: string(gen.OperationStatusCancelled)})
		case isStop(err):
			continue
		case errType(err) != Retryable:
			return endOperation(ctx, EndOperationInput{ID: in.ID, Status: string(gen.OperationStatusFailed), Detail: errText(err)})
		case try == rule.Retries:
			return endOperation(ctx, EndOperationInput{ID: in.ID, Status: string(gen.OperationStatusFailed),
				Detail: fmt.Sprintf("failed after %d attempts: %s", try+1, errText(err))})
		}
		if err := workflow.Sleep(ctx, rule.Wait(try, random)); err != nil {
			return endOperation(ctx, EndOperationInput{ID: in.ID, Status: string(gen.OperationStatusCancelled)})
		}
		try++
	}
}

// randomFraction is a uniform random number in [0, 1) from crypto/rand: 53 random bits, as
// math/rand's Float64. A failed read gives 0, the wait without its random part.
func randomFraction() float64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0
	}
	return float64(binary.BigEndian.Uint64(b[:])>>11) / (1 << 53)
}

// endOperation writes the end through EndOperation, on a context the workflow's cancellation does
// not reach, so a cancelled operation still ends.
func endOperation(ctx workflow.Context, in EndOperationInput) error {
	dctx, _ := workflow.NewDisconnectedContext(ctx)
	dctx = workflow.WithActivityOptions(dctx, workflow.ActivityOptions{StartToCloseTimeout: callSlack,
		RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1}})
	if err := workflow.ExecuteActivity(dctx, EndOperation, in).Get(dctx, nil); err != nil {
		return err
	}
	if in.Status == string(gen.OperationStatusCancelled) {
		return temporal.NewCanceledError()
	}
	return nil
}

// OperationActivities are the Operation workflow's activities, on the engine's Operator.
type OperationActivities struct{ Operator api.Operator }

// DoOperation does the operation's work (Operator.Do), heartbeating while it runs. A retryable
// failure is an application error of type Retryable; when the workflow is cancelled, the work's
// context ends.
func (a *OperationActivities) DoOperation(ctx context.Context, id string) error {
	_, _, err := timed(ctx, 0, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, a.Operator.Do(ctx, id)
	})
	switch {
	case err == nil:
		return nil
	case api.IsRetryable(err):
		return temporal.NewApplicationError(err.Error(), Retryable)
	case ctx.Err() != nil:
		return ctx.Err()
	}
	return temporal.NewNonRetryableApplicationError(err.Error(), "OperationDefect", nil)
}

// EndOperation writes the end the workflow decided (Operator.End).
func (a *OperationActivities) EndOperation(ctx context.Context, in EndOperationInput) error {
	return a.Operator.End(ctx, in.ID, gen.OperationStatus(in.Status), in.Detail)
}

// NewOperationsWorker is the worker of the task queue operations: the Operation workflow and its
// activities on op. The caller starts and stops it.
func NewOperationsWorker(c client.Client, op api.Operator) worker.Worker {
	w := worker.New(c, OperationsQueue, worker.Options{})
	w.RegisterWorkflowWithOptions(Operation, workflow.RegisterOptions{Name: "Operation"})
	a := &OperationActivities{Operator: op}
	w.RegisterActivityWithOptions(a.DoOperation, activity.RegisterOptions{Name: DoOperation})
	w.RegisterActivityWithOptions(a.EndOperation, activity.RegisterOptions{Name: EndOperation})
	return w
}

// Operations is api.Operations on the durable service: each operation is the workflow
// Operation, ID the operation's, on the task queue operations of the namespace ap.
type Operations struct{ c client.Client }

// NewOperations returns the operations of the client's namespace.
func NewOperations(c client.Client) *Operations { return &Operations{c: c} }

var _ api.Operations = (*Operations)(nil)

// Ready says whether the durable service answers its health check.
func (o *Operations) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if _, err := o.c.CheckHealth(ctx, &client.CheckHealthRequest{}); err != nil {
		return fmt.Errorf("durable: the health check: %w: %v", api.ErrUnavailable, err)
	}
	return nil
}

// Start starts the operation's workflow; one that exists, open or closed, is no error.
func (o *Operations) Start(ctx context.Context, s api.OperationStart) error {
	opts := client.StartWorkflowOptions{ID: s.ID, TaskQueue: s.TaskQueue, WorkflowExecutionErrorWhenAlreadyStarted: true}
	if s.RejectDuplicate {
		opts.WorkflowIDReusePolicy = enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE
		opts.WorkflowIDConflictPolicy = enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	_, err := o.c.ExecuteWorkflow(ctx, opts, "Operation", OperationInput{ID: s.ID, Kind: s.Kind, Target: s.Target})
	var started *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &started) {
		return nil
	}
	return classify("starting operation "+s.ID, err)
}

// Open says whether the operation's workflow has not closed; api.ErrNotFound when it has none.
func (o *Operations) Open(ctx context.Context, id string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	d, err := o.c.DescribeWorkflow(ctx, id, "")
	if err = classify("reading operation "+id, err); err != nil {
		return false, err
	}
	switch d.Status {
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED:
		return true, nil
	}
	return false, nil
}

// Cancel asks the operation's workflow to cancel; it then writes the end `cancelled`.
func (o *Operations) Cancel(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return classify("cancelling operation "+id, o.c.CancelWorkflow(ctx, id, ""))
}

// Delete removes the operation's workflow and its history; none is no error.
func (o *Operations) Delete(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	_, err := o.c.WorkflowService().DeleteWorkflowExecution(ctx, &workflowservice.DeleteWorkflowExecutionRequest{
		Namespace: Namespace, WorkflowExecution: workflowExecution(id)})
	if err = classify("deleting operation "+id, err); errors.Is(err, api.ErrNotFound) {
		return nil
	}
	return err
}
