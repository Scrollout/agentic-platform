package durable

import (
	"fmt"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// Connect returns a client of the durable service at hostPort (host:port) in the namespace ap
// that connects at its first call, so the engine checks everything else first.
func Connect(hostPort string) (client.Client, error) {
	c, err := client.NewLazyClient(client.Options{HostPort: hostPort, Namespace: Namespace})
	if err != nil {
		return nil, fmt.Errorf("durable: %s: %w", hostPort, err)
	}
	return c, nil
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
