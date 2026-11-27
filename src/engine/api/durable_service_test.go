package api_test

// The rig's durable test service (T019, owner-approved 2026-11-19; see durable_rig_test.go): each
// run is the real Run workflow of package durable, run in Temporal's in-process test environment
// with the activities of package durable on the latest handler's ports. Signals and cancellations
// go to the running workflow. The workflow's clock is the rig's: its events are stamped with the
// rig clock's time, and a wait of an hour or more (a run waiting for the developer, 14 days) ends
// when the rig clock reaches its end; shorter waits (the retry rule's) pass at once, as the test
// environment skips time. This file is package api_test because package durable imports package
// api.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/agentic-platform/engine/api"
	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

func init() {
	api.NewDurableTestService = func() api.DurableTestService {
		return &testDurable{runs: map[string]*testRun{}}
	}
}

// clockSignal wakes a workflow's long waits to look at the rig clock again.
const clockSignal = "rig-clock"

// longWait is the shortest wait that follows the rig clock.
const longWait = time.Hour

type testDurable struct {
	mu     sync.Mutex
	ports  api.DurablePorts
	runs   map[string]*testRun
	closed bool
}

type testRun struct {
	env   *testsuite.TestWorkflowEnvironment
	done  chan struct{}
	waits map[int]time.Time // the long waits not ended: their end, by the rig clock
	next  int
}

var _ api.DurableTestService = (*testDurable)(nil)

func (s *testDurable) Use(p api.DurablePorts) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ports = p
}

func (s *testDurable) now() time.Time {
	s.mu.Lock()
	now := s.ports.Now
	s.mu.Unlock()
	return now()
}

// act registers one activity of package durable, run on the latest ports in the handler's
// context and counted in its Running; once that context has ended (the engine stopped) none
// starts.
func act[I, O any](env *testsuite.TestWorkflowEnvironment, s *testDurable, name string,
	f func(a *durable.Activities, ctx context.Context, in I) (O, error)) {
	env.RegisterActivityWithOptions(func(ctx context.Context, in I) (O, error) {
		s.mu.Lock()
		p := s.ports
		s.mu.Unlock()
		if p.Context.Err() != nil {
			var zero O
			return zero, temporal.NewNonRetryableApplicationError("the engine has stopped", "EngineStopped", nil)
		}
		if p.Running != nil {
			p.Running.Add(1)
			defer p.Running.Done()
		}
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		defer context.AfterFunc(p.Context, cancel)()
		return f(activitiesOn(s, p), ctx, in)
	}, activity.RegisterOptions{Name: name})
}

// noResult is an activity with only an error as one with an empty result.
func noResult[I any](f func(a *durable.Activities, ctx context.Context, in I) error) func(*durable.Activities, context.Context, I) (struct{}, error) {
	return func(a *durable.Activities, ctx context.Context, in I) (struct{}, error) {
		return struct{}{}, f(a, ctx, in)
	}
}

// activitiesOn are package durable's activities on p.
func activitiesOn(s *testDurable, p api.DurablePorts) *durable.Activities {
	var h machine.History = noHistory{}
	if p.History != nil {
		h = p.History
	}
	a := &durable.Activities{Worker: p.Worker, Checker: p.Checker, History: h, Results: p.Record.Results(),
		Artifacts: alterableArtifacts{p.Artifacts}, Events: testEvents{s: s, rec: p.Record}}
	if cs, ok := p.Artifacts.(api.CapturedStore); ok { // the store that keeps the captured patterns gives a fill step its parameters
		a.Captured = api.CapturedParameters{Store: cs}
	}
	return a
}

// alterableArtifacts is the artifact store the activities use, where a test (api.DurableAlterFilled,
// feature 011 T033, owner-approved 2026-11-26) may alter the bytes of the files a `fill` step
// stores; nothing else it stores is touched.
type alterableArtifacts struct{ api.DurableArtifacts }

func (a alterableArtifacts) PutArtifact(slice string, data []byte) (string, error) {
	if f := api.DurableAlterFilled.Load(); f != nil {
		if files, err := canonical.Unframe(data); err == nil && len(files) > 0 {
			data = canonical.Framed((*f)(files))
		}
	}
	return a.DurableArtifacts.PutArtifact(slice, data)
}

// testEvents writes to the run record until the service is closed.
type testEvents struct {
	s   *testDurable
	rec *api.RunRecord
}

func (e testEvents) Record(slice string, ev machine.Event, operation, replayOf string) error {
	e.s.mu.Lock()
	closed := e.s.closed
	e.s.mu.Unlock()
	if closed {
		return nil
	}
	return e.rec.Record(slice, ev, operation, replayOf)
}

// noHistory is a project without a version history: restoring and committing do nothing.
type noHistory struct{}

func (noHistory) Restore(context.Context, machine.RestoreRequest) error       { return nil }
func (noHistory) Commit(context.Context, machine.CommitRequest) (bool, error) { return true, nil }

// Write (machine.Writer) writes nothing and answers the files it was given as the project's files.
func (noHistory) Write(_ context.Context, r machine.WriteRequest) (machine.WriteResult, error) {
	var res machine.WriteResult
	for _, f := range r.Files {
		sum := sha256.Sum256(f.Content)
		res.Written = append(res.Written, f.Path)
		res.ProjectFiles = append(res.ProjectFiles, machine.File{Path: f.Path, SHA256: hex.EncodeToString(sum[:])})
	}
	slices.Sort(res.Written)
	slices.SortFunc(res.ProjectFiles, func(a, b machine.File) int { return strings.Compare(a.Path, b.Path) })
	return res, nil
}

func (s *testDurable) Start(_ context.Context, r api.RunStart) error {
	if r.Pattern != r.Definition.ID+"@"+r.Definition.Version {
		return fmt.Errorf("durable test service: run %s names pattern %s, its definition is %s@%s", r.Run, r.Pattern,
			r.Definition.ID, r.Definition.Version)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return fmt.Errorf("durable test service: %w: closed", api.ErrUnavailable)
	case s.runs[r.Run] != nil:
		return fmt.Errorf("durable test service: run %s has a workflow already", r.Run)
	}
	var suite testsuite.WorkflowTestSuite
	suite.SetLogger(log.NewStructuredLogger(slog.New(slog.DiscardHandler)))
	env := suite.NewTestWorkflowEnvironment()
	env.SetTestTimeout(24 * time.Hour) // a run waits as long as the test lets it
	env.SetWorkflowRunTimeout(0)       // no run timeout, whose timer would fire while the run waits
	env.SetStartWorkflowOptions(client.StartWorkflowOptions{ID: r.Run, TaskQueue: r.TaskQueue})
	env.SetWorkerOptions(worker.Options{Interceptors: []interceptor.WorkerInterceptor{&rigClock{s: s, run: r.Run}}})
	env.RegisterWorkflowWithOptions(durable.Run, workflow.RegisterOptions{Name: "Run"})
	act(env, s, durable.CallWorker, (*durable.Activities).CallWorker)
	act(env, s, durable.WriteProject, (*durable.Activities).WriteProject)
	act(env, s, durable.RunCheck, (*durable.Activities).RunCheck)
	act(env, s, durable.RestoreProject, (*durable.Activities).RestoreProject)
	act(env, s, durable.CommitStep, (*durable.Activities).CommitStep)
	act(env, s, durable.LookupReuse, (*durable.Activities).LookupReuse)
	act(env, s, durable.PutResult, noResult((*durable.Activities).PutResult))
	act(env, s, durable.RecordEvent, noResult((*durable.Activities).RecordEvent))
	in := durable.RunInput{Run: r.Run, Slice: r.Slice, Project: r.Project, Pattern: r.Definition, Model: r.Model, Inputs: r.Inputs,
		Refs: r.Refs, ReplayOf: r.ReplayOf, Operation: r.Operation, PatternReason: r.PatternReason, Parameters: r.Parameters,
		SourceRun: r.SourceRun, Proof: r.Proof, NotUsed: r.NotUsed, NoReuse: r.NoReuse, TrialSet: r.TrialSet, Trial: r.Trial}
	if r.Fill != nil {
		in.Fill = &durable.FillInput{Captured: r.Fill.Captured, Template: r.Fill.Template, Values: r.Fill.Values}
	}
	run := &testRun{env: env, done: make(chan struct{}), waits: map[int]time.Time{}}
	s.runs[r.Run] = run
	go func() {
		defer close(run.done)
		defer func() { _ = recover() }() // a workflow the test left behind
		if hold := api.DurableHoldRun.Load(); hold != nil {
			(*hold)(r.Run) // a test holds the run before its first recorded event (feature 011 T033)
		}
		env.ExecuteWorkflow("Run", in)
	}()
	return nil
}

// running returns the run's workflow while it runs.
func (s *testDurable) running(id string) (*testRun, error) {
	s.mu.Lock()
	r := s.runs[id]
	s.mu.Unlock()
	if r == nil {
		return nil, fmt.Errorf("durable test service: run %s: %w", id, api.ErrNotFound)
	}
	select {
	case <-r.done:
		return nil, fmt.Errorf("durable test service: run %s has ended: %w", id, api.ErrNotFound)
	default:
	}
	return r, nil
}

// inLoop runs f in the test environment's main loop, where the environment may be used.
func (r *testRun) inLoop(f func()) { r.env.RegisterDelayedCallback(f, 0) }

func (s *testDurable) Signal(_ context.Context, id, name string) error {
	r, err := s.running(id)
	if err != nil {
		return err
	}
	r.inLoop(func() { r.env.SignalWorkflow(name, nil) })
	return nil
}

func (s *testDurable) Cancel(_ context.Context, id string) error {
	r, err := s.running(id)
	if err != nil {
		return err
	}
	r.inLoop(r.env.CancelWorkflow)
	return nil
}

func (s *testDurable) Exists(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[id] != nil, nil
}

func (s *testDurable) Closed(_ context.Context, id string) (bool, string, error) {
	s.mu.Lock()
	r := s.runs[id]
	s.mu.Unlock()
	if r == nil {
		return false, "", fmt.Errorf("durable test service: run %s: %w", id, api.ErrNotFound)
	}
	select {
	case <-r.done:
	default:
		return false, "", nil
	}
	err := r.env.GetWorkflowError()
	if err == nil {
		return true, "", nil
	}
	for e := err; e != nil; {
		if app, ok := e.(*temporal.ApplicationError); ok {
			return true, app.Message(), nil
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			break
		}
		e = u.Unwrap()
	}
	return true, err.Error(), nil
}

func (s *testDurable) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	r := s.runs[id]
	delete(s.runs, id)
	s.mu.Unlock()
	if r == nil {
		return fmt.Errorf("durable test service: run %s: %w", id, api.ErrNotFound)
	}
	r.inLoop(r.env.CancelWorkflow)
	return nil
}

func (s *testDurable) Tick() {
	now := s.now()
	var due []*testRun
	s.mu.Lock()
	for _, r := range s.runs {
		for _, end := range r.waits {
			if !now.Before(end) {
				due = append(due, r)
				break
			}
		}
	}
	s.mu.Unlock()
	for _, r := range due {
		r.inLoop(func() { r.env.SignalWorkflow(clockSignal, nil) })
	}
	for _, r := range due {
		select {
		case <-r.done:
		case <-time.After(10 * time.Second):
		}
	}
}

func (s *testDurable) Waiting(run string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.runs[run]
	return r != nil && len(r.waits) > 0
}

func (s *testDurable) Close() {
	s.mu.Lock()
	s.closed = true
	var all []*testRun
	for _, r := range s.runs {
		all = append(all, r)
	}
	s.mu.Unlock()
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

// rigClock gives a run's workflow the rig clock: workflow.Now is the rig clock's time, and a timer
// of longWait or more ends when the rig clock reaches its end.
type rigClock struct {
	interceptor.WorkerInterceptorBase
	s   *testDurable
	run string
}

func (c *rigClock) InterceptWorkflow(_ workflow.Context, next interceptor.WorkflowInboundInterceptor) interceptor.WorkflowInboundInterceptor {
	return &rigClockIn{WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{Next: next}, c: c}
}

type rigClockIn struct {
	interceptor.WorkflowInboundInterceptorBase
	c *rigClock
}

func (i *rigClockIn) Init(out interceptor.WorkflowOutboundInterceptor) error {
	return i.Next.Init(&rigClockOut{WorkflowOutboundInterceptorBase: interceptor.WorkflowOutboundInterceptorBase{Next: out}, c: i.c})
}

type rigClockOut struct {
	interceptor.WorkflowOutboundInterceptorBase
	c *rigClock
}

func (o *rigClockOut) Now(workflow.Context) time.Time { return o.c.s.now() }

func (o *rigClockOut) NewTimer(ctx workflow.Context, d time.Duration) workflow.Future {
	return o.NewTimerWithOptions(ctx, d, workflow.TimerOptions{})
}

func (o *rigClockOut) NewTimerWithOptions(ctx workflow.Context, d time.Duration, opts workflow.TimerOptions) workflow.Future {
	if d < longWait {
		return o.Next.NewTimerWithOptions(ctx, d, opts)
	}
	s := o.c.s
	end := s.now().Add(d)
	s.mu.Lock()
	r := s.runs[o.c.run]
	if r == nil { // deleted
		s.mu.Unlock()
		return o.Next.NewTimerWithOptions(ctx, d, opts)
	}
	r.next++
	id := r.next
	r.waits[id] = end
	s.mu.Unlock()
	f, set := workflow.NewFuture(ctx)
	wake := workflow.GetSignalChannel(ctx, clockSignal)
	workflow.Go(ctx, func(g workflow.Context) {
		defer func() {
			s.mu.Lock()
			delete(r.waits, id)
			s.mu.Unlock()
		}()
		for {
			if !s.now().Before(end) {
				set.Set(nil, nil)
				return
			}
			sel := workflow.NewSelector(g)
			sel.AddReceive(wake, func(c workflow.ReceiveChannel, _ bool) { c.Receive(g, nil) })
			sel.AddReceive(g.Done(), func(workflow.ReceiveChannel, bool) {})
			sel.Select(g)
			if g.Err() != nil {
				set.Set(nil, temporal.NewCanceledError())
				return
			}
		}
	})
	return f
}
