package durable

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentic-platform/engine/api"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/types/known/durationpb"
)

// callTimeout bounds each call the engine's API makes to the durable service, so a request is
// answered 503 instead of hanging while the service cannot be reached.
const callTimeout = 10 * time.Second

// Runs is api.Runs on the durable service: each run is the workflow Run, ID the run's, on the
// task queue runs of the namespace ap.
type Runs struct{ c client.Client }

// NewRuns returns the runs of the client's namespace.
func NewRuns(c client.Client) *Runs { return &Runs{c: c} }

var _ api.Runs = (*Runs)(nil)

// Start starts the run's workflow; a run whose workflow exists, or existed, is refused.
func (r *Runs) Start(ctx context.Context, s api.RunStart) error {
	if s.Pattern != s.Definition.ID+"@"+s.Definition.Version {
		return fmt.Errorf("durable: run %s names pattern %s, its definition is %s@%s", s.Run, s.Pattern, s.Definition.ID, s.Definition.Version)
	}
	opts := client.StartWorkflowOptions{ID: s.Run, TaskQueue: s.TaskQueue, WorkflowExecutionErrorWhenAlreadyStarted: true}
	if s.RejectDuplicate {
		opts.WorkflowIDReusePolicy = enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE
		opts.WorkflowIDConflictPolicy = enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL
	}
	in := RunInput{Run: s.Run, Slice: s.Slice, Project: s.Project, Pattern: s.Definition, Model: s.Model, Inputs: s.Inputs,
		Refs: s.Refs, ReplayOf: s.ReplayOf, Operation: s.Operation, NoReuse: s.NoReuse, PatternReason: s.PatternReason,
		Parameters: s.Parameters, SourceRun: s.SourceRun, Proof: s.Proof, NotUsed: s.NotUsed}
	if f := s.Fill; f != nil {
		in.Fill = &FillInput{Captured: f.Captured, Template: f.Template, Values: f.Values}
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	_, err := r.c.ExecuteWorkflow(ctx, opts, "Run", in)
	return classify("starting run "+s.Run, err)
}

// Signal sends a signal without a payload to the run's workflow.
func (r *Runs) Signal(ctx context.Context, run, name string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return classify("signalling "+name+" to run "+run, r.c.SignalWorkflow(ctx, run, "", name, nil))
}

// Cancel asks the run's workflow to cancel; the run records RunCancelled.
func (r *Runs) Cancel(ctx context.Context, run string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return classify("cancelling run "+run, r.c.CancelWorkflow(ctx, run, ""))
}

// Exists says whether the run has a workflow, running or closed and still kept.
func (r *Runs) Exists(ctx context.Context, run string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	_, err := r.c.DescribeWorkflow(ctx, run, "")
	switch err = classify("reading run "+run, err); {
	case err == nil:
		return true, nil
	case errors.Is(err, api.ErrNotFound):
		return false, nil
	}
	return false, err
}

// Closed says whether the run's workflow has closed, and why when it did not complete: the
// failure's message, or how Temporal ended it. A run with no workflow is api.ErrNotFound.
func (r *Runs) Closed(ctx context.Context, run string) (bool, string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	d, err := r.c.DescribeWorkflow(ctx, run, "")
	if err = classify("reading run "+run, err); err != nil {
		return false, "", err
	}
	switch d.Status {
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED:
		return false, "", nil
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return true, "", nil
	}
	werr := r.c.GetWorkflow(ctx, run, "").Get(ctx, nil)
	var app *temporal.ApplicationError
	switch {
	case werr == nil:
		return true, "", nil
	case errors.As(werr, &app):
		return true, app.Message(), nil
	case errors.Unwrap(werr) != nil:
		return true, errors.Unwrap(werr).Error(), nil
	}
	return true, werr.Error(), nil
}

// Delete removes the run's workflow and its history.
func (r *Runs) Delete(ctx context.Context, run string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	_, err := r.c.WorkflowService().DeleteWorkflowExecution(ctx, &workflowservice.DeleteWorkflowExecutionRequest{
		Namespace: Namespace, WorkflowExecution: workflowExecution(run)})
	return classify("deleting run "+run, err)
}

func workflowExecution(run string) *commonpb.WorkflowExecution {
	return &commonpb.WorkflowExecution{WorkflowId: run}
}

// classify wraps a durable-service error as api.ErrNotFound when the workflow does not exist, and
// as api.ErrUnavailable when the service could not be reached or did not answer in time.
func classify(what string, err error) error {
	var (
		notFound    *serviceerror.NotFound
		unavailable *serviceerror.Unavailable
		deadline    *serviceerror.DeadlineExceeded
		exhausted   *serviceerror.ResourceExhausted
		canceled    *serviceerror.Canceled
		namespace   *serviceerror.NamespaceNotFound // not served yet
	)
	switch {
	case err == nil:
		return nil
	case errors.As(err, &notFound):
		return fmt.Errorf("durable: %s: %w: %v", what, api.ErrNotFound, err)
	case errors.As(err, &unavailable), errors.As(err, &deadline), errors.As(err, &exhausted), errors.As(err, &canceled),
		errors.As(err, &namespace), errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("durable: %s: %w: %v", what, api.ErrUnavailable, err)
	}
	return fmt.Errorf("durable: %s: %w", what, err)
}

// Namespaces registers and describes namespaces on the durable service (ADR 0070).
type Namespaces struct{ c client.NamespaceClient }

// NewNamespaces returns the namespaces of the durable service at hostPort.
func NewNamespaces(hostPort string) (*Namespaces, error) {
	nc, err := client.NewNamespaceClient(client.Options{HostPort: hostPort})
	if err != nil {
		return nil, fmt.Errorf("durable: connecting to %s: %w", hostPort, err)
	}
	return &Namespaces{c: nc}, nil
}

// Describe says whether the namespace exists and, if so, its retention.
func (n *Namespaces) Describe(ctx context.Context, name string) (time.Duration, bool, error) {
	d, err := n.c.Describe(ctx, name)
	var missing *serviceerror.NamespaceNotFound
	switch {
	case errors.As(err, &missing):
		return 0, false, nil
	case err != nil:
		return 0, false, fmt.Errorf("durable: reading the namespace %s: %w", name, err)
	}
	return d.GetConfig().GetWorkflowExecutionRetentionTtl().AsDuration(), true, nil
}

// Register creates the namespace with the retention; one registered meanwhile is kept.
func (n *Namespaces) Register(ctx context.Context, name string, retention time.Duration) error {
	err := n.c.Register(ctx, &workflowservice.RegisterNamespaceRequest{Namespace: name,
		WorkflowExecutionRetentionPeriod: durationpb.New(retention)})
	var exists *serviceerror.NamespaceAlreadyExists
	if err != nil && !errors.As(err, &exists) {
		return fmt.Errorf("durable: registering the namespace %s: %w", name, err)
	}
	return nil
}

// Close closes the connection.
func (n *Namespaces) Close() { n.c.Close() }
