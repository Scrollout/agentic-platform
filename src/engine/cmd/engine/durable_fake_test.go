package main

// The durable service for the tests that need a started engine (T019, owner-approved 2026-11-19):
// testDurable gives setupWith durableLinks whose namespace ap exists, whose runs run the real Run
// workflow (package durable) in Temporal's in-process test environment, each on the activities
// setupWith serves (Serve), and whose stop ends every run's workflow without letting it write
// anything more. A wait of an hour or more (a run waiting for the developer or a resume) lasts
// until the run is cancelled; shorter waits (the retry rule's) pass at once, as the test
// environment skips time.

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/api"
	"github.com/agentic-platform/engine/durable"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// testDurable returns the durable links of an engine under test; its runs end with the test.
func testDurable(t *testing.T) durableLinks {
	t.Helper()
	d := &fakeDurable{runs: map[string]*fakeRun{}}
	t.Cleanup(d.stop)
	ns := &fakeNamespaces{existing: map[string]time.Duration{durable.Namespace: api.Retention()}}
	return durableLinks{Runs: d, Namespaces: ns, Serve: func(a *durable.Activities) (func(), error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.acts = a
		return d.stop, nil
	}}
}

type fakeDurable struct {
	mu   sync.Mutex
	acts *durable.Activities // the served activities; nil until Serve
	runs map[string]*fakeRun

	// writing is held while an activity runs; stop takes it once stopped is set, so no activity
	// runs after stop returns.
	writing sync.RWMutex
	stopped bool
	cancel  []context.CancelFunc
}

type fakeRun struct {
	env  *testsuite.TestWorkflowEnvironment
	done chan struct{}
}

var _ api.Runs = (*fakeDurable)(nil)

func (d *fakeDurable) Start(_ context.Context, r api.RunStart) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch {
	case d.acts == nil || d.stopped:
		return fmt.Errorf("fake durable service: %w: no worker", api.ErrUnavailable)
	case d.runs[r.Run] != nil:
		return fmt.Errorf("fake durable service: run %s has a workflow already", r.Run)
	}
	var suite testsuite.WorkflowTestSuite
	suite.SetLogger(log.NewStructuredLogger(slog.New(slog.DiscardHandler)))
	env := suite.NewTestWorkflowEnvironment()
	env.SetTestTimeout(24 * time.Hour) // a run waits as long as the test lets it
	env.SetWorkflowRunTimeout(0)       // no run timeout, whose timer would fire while the run waits
	env.SetStartWorkflowOptions(client.StartWorkflowOptions{ID: r.Run, TaskQueue: r.TaskQueue})
	env.SetWorkerOptions(worker.Options{Interceptors: []interceptor.WorkerInterceptor{&longWaits{}}})
	env.RegisterWorkflowWithOptions(durable.Run, workflow.RegisterOptions{Name: "Run"})
	fakeAct(env, d, durable.CallWorker, (*durable.Activities).CallWorker)
	fakeAct(env, d, durable.RunCheck, (*durable.Activities).RunCheck)
	fakeAct(env, d, durable.RestoreProject, (*durable.Activities).RestoreProject)
	fakeAct(env, d, durable.CommitStep, (*durable.Activities).CommitStep)
	fakeAct(env, d, durable.LookupReuse, (*durable.Activities).LookupReuse)
	fakeAct(env, d, durable.PutResult, fakeNoResult((*durable.Activities).PutResult))
	fakeAct(env, d, durable.RecordEvent, fakeNoResult((*durable.Activities).RecordEvent))
	in := durable.RunInput{Run: r.Run, Slice: r.Slice, Project: r.Project, Pattern: r.Definition, Model: r.Model, Inputs: r.Inputs,
		Refs: r.Refs, ReplayOf: r.ReplayOf, Operation: r.Operation}
	run := &fakeRun{env: env, done: make(chan struct{})}
	d.runs[r.Run] = run
	go func() {
		defer close(run.done)
		defer func() { _ = recover() }() // a workflow the test left behind
		env.ExecuteWorkflow("Run", in)
	}()
	return nil
}

// fakeAct registers one activity of package durable on the served activities; after stop none
// runs, and stop cancels those running.
func fakeAct[I, O any](env *testsuite.TestWorkflowEnvironment, d *fakeDurable, name string,
	f func(a *durable.Activities, ctx context.Context, in I) (O, error)) {
	env.RegisterActivityWithOptions(func(ctx context.Context, in I) (O, error) {
		d.writing.RLock()
		defer d.writing.RUnlock()
		d.mu.Lock()
		if d.stopped {
			d.mu.Unlock()
			var zero O
			return zero, temporal.NewNonRetryableApplicationError("the engine has stopped", "EngineStopped", nil)
		}
		ctx, cancel := context.WithCancel(ctx)
		d.cancel = append(d.cancel, cancel)
		a := d.acts
		d.mu.Unlock()
		defer cancel()
		return f(a, ctx, in)
	}, activity.RegisterOptions{Name: name})
}

// fakeNoResult is an activity with only an error as one with an empty result.
func fakeNoResult[I any](f func(a *durable.Activities, ctx context.Context, in I) error) func(*durable.Activities, context.Context, I) (struct{}, error) {
	return func(a *durable.Activities, ctx context.Context, in I) (struct{}, error) {
		return struct{}{}, f(a, ctx, in)
	}
}

func (d *fakeDurable) running(id string) (*fakeRun, error) {
	d.mu.Lock()
	r := d.runs[id]
	d.mu.Unlock()
	if r == nil {
		return nil, fmt.Errorf("fake durable service: run %s: %w", id, api.ErrNotFound)
	}
	select {
	case <-r.done:
		return nil, fmt.Errorf("fake durable service: run %s has ended: %w", id, api.ErrNotFound)
	default:
	}
	return r, nil
}

// inLoop runs f in the test environment's main loop, where the environment may be used.
func (r *fakeRun) inLoop(f func()) { r.env.RegisterDelayedCallback(f, 0) }

func (d *fakeDurable) Signal(_ context.Context, id, name string) error {
	r, err := d.running(id)
	if err != nil {
		return err
	}
	r.inLoop(func() { r.env.SignalWorkflow(name, nil) })
	return nil
}

func (d *fakeDurable) Cancel(_ context.Context, id string) error {
	r, err := d.running(id)
	if err != nil {
		return err
	}
	r.inLoop(r.env.CancelWorkflow)
	return nil
}

func (d *fakeDurable) Exists(_ context.Context, id string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.runs[id] != nil, nil
}

func (d *fakeDurable) Delete(_ context.Context, id string) error {
	d.mu.Lock()
	r := d.runs[id]
	delete(d.runs, id)
	d.mu.Unlock()
	if r == nil {
		return fmt.Errorf("fake durable service: run %s: %w", id, api.ErrNotFound)
	}
	r.inLoop(r.env.CancelWorkflow)
	return nil
}

// stop ends every run's workflow; once it returns no activity runs or starts.
func (d *fakeDurable) stop() {
	d.mu.Lock()
	d.stopped = true
	for _, c := range d.cancel {
		c()
	}
	d.cancel = nil
	var all []*fakeRun
	for _, r := range d.runs {
		all = append(all, r)
	}
	d.mu.Unlock()
	d.writing.Lock() // the activities running have returned
	d.writing.Unlock()
	for _, r := range all {
		select {
		case <-r.done:
		default:
			r.inLoop(r.env.CancelWorkflow)
		}
	}
	for _, r := range all {
		select {
		case <-r.done:
		case <-time.After(10 * time.Second):
		}
	}
}

// longWaits makes a workflow's timer of an hour or more last until its context is cancelled.
type longWaits struct {
	interceptor.WorkerInterceptorBase
}

func (*longWaits) InterceptWorkflow(_ workflow.Context, next interceptor.WorkflowInboundInterceptor) interceptor.WorkflowInboundInterceptor {
	return &longWaitsIn{WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{Next: next}}
}

type longWaitsIn struct {
	interceptor.WorkflowInboundInterceptorBase
}

func (i *longWaitsIn) Init(out interceptor.WorkflowOutboundInterceptor) error {
	return i.Next.Init(&longWaitsOut{WorkflowOutboundInterceptorBase: interceptor.WorkflowOutboundInterceptorBase{Next: out}})
}

type longWaitsOut struct {
	interceptor.WorkflowOutboundInterceptorBase
}

func (o *longWaitsOut) NewTimer(ctx workflow.Context, d time.Duration) workflow.Future {
	return o.NewTimerWithOptions(ctx, d, workflow.TimerOptions{})
}

func (o *longWaitsOut) NewTimerWithOptions(ctx workflow.Context, d time.Duration, opts workflow.TimerOptions) workflow.Future {
	if d < time.Hour {
		return o.Next.NewTimerWithOptions(ctx, d, opts)
	}
	f, set := workflow.NewFuture(ctx)
	workflow.Go(ctx, func(g workflow.Context) {
		g.Done().Receive(g, nil)
		set.Set(nil, temporal.NewCanceledError())
	})
	return f
}
