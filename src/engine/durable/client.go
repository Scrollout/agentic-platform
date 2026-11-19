package durable

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Dial connects to the durable service at hostPort (host:port) in the namespace ap.
func Dial(ctx context.Context, hostPort string) (client.Client, error) {
	c, err := client.DialContext(ctx, client.Options{HostPort: hostPort, Namespace: Namespace})
	if err != nil {
		return nil, fmt.Errorf("durable: connecting to %s: %w", hostPort, err)
	}
	return c, nil
}

// RegisterNamespace registers the namespace ap with the retention given when the durable service
// at hostPort has none; an existing namespace is kept unchanged (ADR 0070).
func RegisterNamespace(ctx context.Context, hostPort string, retention time.Duration) error {
	nc, err := client.NewNamespaceClient(client.Options{HostPort: hostPort})
	if err != nil {
		return fmt.Errorf("durable: connecting to %s: %w", hostPort, err)
	}
	defer nc.Close()
	_, err = nc.Describe(ctx, Namespace)
	var missing *serviceerror.NamespaceNotFound
	switch {
	case err == nil:
		return nil
	case !errors.As(err, &missing):
		return fmt.Errorf("durable: reading the namespace %s: %w", Namespace, err)
	}
	err = nc.Register(ctx, &workflowservice.RegisterNamespaceRequest{Namespace: Namespace,
		WorkflowExecutionRetentionPeriod: durationpb.New(retention)})
	var exists *serviceerror.NamespaceAlreadyExists
	if err != nil && !errors.As(err, &exists) {
		return fmt.Errorf("durable: registering the namespace %s: %w", Namespace, err)
	}
	return nil
}

// NewRunsWorker is the worker of the task queue runs: the Run workflow and its activities on a.
// The caller starts and stops it.
func NewRunsWorker(c client.Client, a *Activities) worker.Worker {
	w := worker.New(c, RunsQueue, worker.Options{})
	w.RegisterWorkflowWithOptions(Run, workflow.RegisterOptions{Name: "Run"})
	for name, fn := range map[string]any{
		CallWorker: a.CallWorker, RunCheck: a.RunCheck, RestoreProject: a.RestoreProject, CommitStep: a.CommitStep,
		LookupReuse: a.LookupReuse, PutResult: a.PutResult, RecordEvent: a.RecordEvent,
	} {
		w.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name})
	}
	return w
}
