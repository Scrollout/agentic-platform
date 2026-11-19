package api

// T013, the engine API over the durable service: runs are workflows and signals, the run record
// still gives a run's status and outcome. Written before the code (T018) by a test author, with
// the engine's durable interface faked. Judged from spec FR-004, FR-005, the data model ("The
// engine's durable interface", the Runs port, "Run states") and the engine contract.
//
// Interface assumed, the smallest that the data model's sketch needs (the code author, T018,
// writes it in package api; the adapter in src/engine/durable implements it):
//
//	// Runs is the API's port to the durable service: what it does for the API is start a run,
//	// tell it something, stop it, and say whether it exists.
//	type Runs interface {
//		// Start starts one run's workflow. Run is "RUN-<n>", TaskQueue is "runs" and
//		// RejectDuplicate is true, so a run is never started twice.
//		Start(ctx context.Context, r RunStart) error
//		// Signal sends the named signal to the run: "workers-ready", "resume",
//		// "accept-hand-over" or "cancel".
//		Signal(ctx context.Context, run, name string) error
//		// Cancel cancels the run's workflow (the run's operation was cancelled).
//		Cancel(ctx context.Context, run string) error
//		// Exists says whether the run has a workflow.
//		Exists(ctx context.Context, run string) (bool, error)
//		// Delete removes the run's workflow (erasing a slice).
//		Delete(ctx context.Context, run string) error
//	}
//	type RunStart struct {
//		Run, Slice, Project, ReplayOf string
//		Pattern                       string // "<id>@<version>"
//		Inputs                        map[string]string // slice parts by hash
//		TaskQueue                     string
//		RejectDuplicate               bool
//	}
//	var ErrUnavailable error // every method: the durable service cannot be reached (errors.Is)
//	var ErrNotFound error    // Signal, Cancel, Delete: no such workflow (errors.Is)
//	Config.Runs Runs         // required for runs; with it set no run is driven in-process
//
// Behaviour assumed: an error wrapping ErrUnavailable from Runs on a state change is answered
// 503 UnavailableProblem (code Unavailable) with Retry-After, nothing changed; registerWorker
// signals "workers-ready" to the run of that slice whose record's last event is RunInterrupted
// with detail "the step workers are not running", once the slice has a registered worker for
// every step of the pattern. There is no cancelRun operation in the contract: cancelOperation
// on a run's operation is the one way, and it calls Runs.Cancel for the run.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/store"
)

// ---- the fake ----

type durableCall struct{ Op, Run, Name string }

type fakeRuns struct {
	mu     sync.Mutex
	calls  []durableCall
	starts []RunStart
	down   bool            // every call fails ErrUnavailable
	exists map[string]bool // runs that have a workflow
}

func (f *fakeRuns) record(c durableCall) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return fmt.Errorf("fake durable service: %w", ErrUnavailable)
	}
	f.calls = append(f.calls, c)
	return nil
}

func (f *fakeRuns) Start(_ context.Context, r RunStart) error {
	if err := f.record(durableCall{Op: "start", Run: r.Run}); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, r)
	return nil
}
func (f *fakeRuns) Signal(_ context.Context, run, name string) error {
	return f.record(durableCall{Op: "signal", Run: run, Name: name})
}
func (f *fakeRuns) Cancel(_ context.Context, run string) error {
	return f.record(durableCall{Op: "cancel", Run: run})
}
func (f *fakeRuns) Delete(_ context.Context, run string) error {
	return f.record(durableCall{Op: "delete", Run: run})
}
func (f *fakeRuns) Exists(_ context.Context, run string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return false, fmt.Errorf("fake durable service: %w", ErrUnavailable)
	}
	return f.exists[run], nil
}
func (f *fakeRuns) seen() []durableCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]durableCall(nil), f.calls...)
}
func (f *fakeRuns) setDown(v bool) { f.mu.Lock(); f.down = v; f.mu.Unlock() }

// ---- the rig: existing helpers, a fake durable service ----

type durableRig struct {
	t    *testing.T
	sp   *spRig
	st   *store.Store
	runs *fakeRuns
	w    *fakeStepWorker
	c    *fakeStepChecker
	r    *rig
	wk   *Workers
}

func newDurableRig(t *testing.T, registered bool) *durableRig {
	t.Helper()
	st, err := store.Open(t.TempDir(), spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	sp := newSP(t, []spResult{{p: spNotes(t)}}, st)
	sp.approveAfterSeed(t)
	x := &durableRig{t: t, sp: sp, st: st, runs: &fakeRuns{exists: map[string]bool{}},
		w: &fakeStepWorker{st: st, data: map[string][]byte{}}, c: &fakeStepChecker{}, wk: NewWorkers()}
	projects := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	x.r = newRig(t, func(cfg *Config) {
		cfg.Context = ctx
		cfg.Slices, cfg.Store = sp.svc, st
		cfg.WorkerGrants = stepGrants("S-1")
		cfg.Projects = projects
		cfg.Model = "test-model"
		cfg.StepWorker, cfg.StepChecker = x.w, x.c
		cfg.Workers = x.wk
		cfg.Runs = x.runs
	})
	if registered {
		registerAll(t, x.r, "S-1")
	}
	return x
}

func (x *durableRig) do(method, path, body string) reply {
	x.r.clock.Advance(80 * time.Millisecond)
	return x.r.do(method, path, clientToken, body, nil)
}

// record writes events of RUN-1 (slice S-1) as the run's workflow would: RunStarted first, then
// the kinds given, each with its detail when "Kind=detail".
func (x *durableRig) record(kinds ...string) {
	x.t.Helper()
	p, err := x.sp.svc.Pattern()
	if err != nil {
		x.t.Fatal(err)
	}
	zero := "sha256:" + strings.Repeat("0", 64)
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	for i, k := range append([]string{"RunStarted"}, kinds...) {
		kind, detail, _ := strings.Cut(k, "=")
		e := storedEvent{Event: machineEvent(i+1, at.Add(time.Duration(i)*time.Second), kind, detail)}
		if kind == "RunStarted" {
			e.Event.Slice, e.Event.Project, e.Event.Pattern = "S-1", "notes-app", p.ID+"@"+p.Version
			e.Event.Inputs = []string{zero, zero, zero}
			e.Info = &runInfo{Project: "notes-app", Operation: "OP-1"}
		}
		data, _ := json.Marshal(e)
		if _, err := x.st.Append("S-1", store.Event{Run: "RUN-1", Kind: kind, Data: store.RawJSON(data)}); err != nil {
			x.t.Fatal(err)
		}
	}
}

func (x *durableRig) opOf(p reply, status int) gen.Operation {
	x.t.Helper()
	return p.operation(x.t, status)
}

func unavailable(t *testing.T, p reply) {
	t.Helper()
	if p.Code != 503 {
		t.Fatalf("status = %d, want 503; body: %s", p.Code, p.Body.String())
	}
	if ra, err := strconv.Atoi(p.Header().Get("Retry-After")); err != nil || ra < 1 {
		t.Fatalf("Retry-After = %q, want a number of seconds", p.Header().Get("Retry-After"))
	}
	var pr gen.UnavailableProblem
	if err := json.Unmarshal(p.Body.Bytes(), &pr); err != nil || string(pr.Code) != "Unavailable" {
		t.Fatalf("body is not an UnavailableProblem with code Unavailable (%v): %s", err, p.Body.String())
	}
}

func machineEvent(seq int, at time.Time, kind, detail string) machine.Event {
	return machine.Event{Seq: seq, At: at, Run: "RUN-1", Kind: kind, Detail: detail}
}

// ---- startRun ----

func TestStartRunStartsWorkflowRunNOnTaskQueueRunsRejectingDuplicates(t *testing.T) {
	x := newDurableRig(t, true)
	op := x.opOf(x.do("POST", spURL("S-1", "/runs"), `{"project":"notes-app"}`), 202)
	if string(op.Kind) != "run" || !runIDRE.MatchString(op.Target) {
		t.Fatalf("operation = %+v, want kind run and a RUN-<n> target", op)
	}
	if len(x.runs.starts) != 1 {
		t.Fatalf("the durable service was asked to start %d workflows, want 1", len(x.runs.starts))
	}
	s := x.runs.starts[0]
	if s.Run != op.Target || !runIDRE.MatchString(s.Run) || s.Slice != "S-1" || s.Project != "notes-app" {
		t.Errorf("start = %+v, want run %s of slice S-1, project notes-app", s, op.Target)
	}
	if s.TaskQueue != "runs" {
		t.Errorf("task queue = %q, want runs", s.TaskQueue)
	}
	if !s.RejectDuplicate {
		t.Errorf("RejectDuplicate is false, want true")
	}
	if len(s.Inputs) == 0 {
		t.Errorf("the start carries no inputs' hashes")
	}
	if n := x.w.callCount(); n != 0 {
		t.Errorf("the engine drove the run itself: %d worker calls, want 0 (the workflow does)", n)
	}
}

func TestStartRunWhenTheDurableServiceIsUnreachableIs503AndRecordsNothing(t *testing.T) {
	x := newDurableRig(t, true)
	x.runs.setDown(true)
	unavailable(t, x.do("POST", spURL("S-1", "/runs"), `{"project":"notes-app"}`))
	if len(x.runs.seen()) != 0 || len(x.runs.starts) != 0 {
		t.Errorf("a start was accepted by the fake while down")
	}
	if got := x.st.Runs(); len(got) != 0 {
		t.Errorf("the store holds runs %v after a refused start", got)
	}
}

// ---- resumeRun, acceptHandOver, cancelling the run's operation ----

func TestResumeRunSignalsResume(t *testing.T) {
	x := newDurableRig(t, true)
	x.record("RunInterrupted=the model is unavailable")
	op := x.opOf(x.do("POST", "/runs/RUN-1/resume?api-version="+apiVersion, ""), 202)
	if string(op.Kind) != "run" || op.Target != "RUN-1" {
		t.Fatalf("operation = %+v, want kind run, target RUN-1", op)
	}
	if got := x.runs.seen(); len(got) != 1 || got[0] != (durableCall{"signal", "RUN-1", "resume"}) {
		t.Errorf("durable calls = %v, want one resume signal to RUN-1", got)
	}
}

func TestResumeRunWhenTheDurableServiceIsUnreachableIs503(t *testing.T) {
	x := newDurableRig(t, true)
	x.record("RunInterrupted=the model is unavailable")
	x.runs.setDown(true)
	unavailable(t, x.do("POST", "/runs/RUN-1/resume?api-version="+apiVersion, ""))
}

func TestAcceptHandOverSignalsAcceptHandOver(t *testing.T) {
	x := newDurableRig(t, true)
	x.record("RunHandedOver=the step's checks failed 3 times")
	op := x.opOf(x.do("POST", "/runs/RUN-1/hand-over?api-version="+apiVersion, ""), 202)
	if string(op.Kind) != "run" || op.Target != "RUN-1" {
		t.Fatalf("operation = %+v, want kind run, target RUN-1", op)
	}
	if got := x.runs.seen(); len(got) != 1 || got[0] != (durableCall{"signal", "RUN-1", "accept-hand-over"}) {
		t.Errorf("durable calls = %v, want one accept-hand-over signal to RUN-1", got)
	}
}

func TestCancellingTheRunsOperationCancelsTheRunsWorkflow(t *testing.T) {
	x := newDurableRig(t, true)
	op := x.opOf(x.do("POST", spURL("S-1", "/runs"), `{"project":"notes-app"}`), 202)
	x.opOf(x.do("POST", "/operations/"+op.Id+"/cancel?api-version="+apiVersion, ""), 202)
	var cancels, signals []durableCall
	for _, c := range x.runs.seen() {
		switch {
		case c.Op == "cancel":
			cancels = append(cancels, c)
		case c.Op == "signal" && c.Name == "cancel":
			signals = append(signals, c)
		}
	}
	// The run's workflow is cancelled by a cancellation or by the cancel signal, once.
	if len(cancels)+len(signals) != 1 || (len(cancels) == 1 && cancels[0].Run != op.Target) || (len(signals) == 1 && signals[0].Run != op.Target) {
		t.Errorf("durable calls = %v, want one cancel of %s", x.runs.seen(), op.Target)
	}
}

// ---- registerWorker ----

func TestRegisterWorkerSendsWorkersReadyToTheRunWaitingForTheSlicesWorkers(t *testing.T) {
	x := newDurableRig(t, false)
	x.record("RunInterrupted=the step workers are not running")
	for i, st := range declaredSteps {
		if w := register(x.r, stepToken("S-1", st), endpointFor(st)); w.Code != 200 {
			t.Fatalf("register %s: %d: %s", st, w.Code, w.Body.String())
		}
		got := x.runs.seen()
		if i < len(declaredSteps)-1 && len(got) != 0 {
			t.Fatalf("after %d of %d workers: calls %v, want none until the slice's workers are all registered", i+1, len(declaredSteps), got)
		}
	}
	if got := x.runs.seen(); len(got) != 1 || got[0] != (durableCall{"signal", "RUN-1", "workers-ready"}) {
		t.Errorf("durable calls = %v, want one workers-ready signal to RUN-1", got)
	}
}

func TestRegisterWorkerSignalsNoRunThatIsNotWaitingForWorkers(t *testing.T) {
	x := newDurableRig(t, false)
	x.record("RunInterrupted=the model is unavailable") // interrupted for another reason
	for _, st := range declaredSteps {
		register(x.r, stepToken("S-1", st), endpointFor(st))
	}
	if got := x.runs.seen(); len(got) != 0 {
		t.Errorf("durable calls = %v, want none: the run waits for the developer, not for workers", got)
	}
}

func TestRegisterWorkerWhenTheDurableServiceIsUnreachableIs503AndChangesNothing(t *testing.T) {
	x := newDurableRig(t, false)
	x.record("RunInterrupted=the step workers are not running")
	for _, st := range declaredSteps[:len(declaredSteps)-1] {
		register(x.r, stepToken("S-1", st), endpointFor(st))
	}
	x.runs.setDown(true)
	last := declaredSteps[len(declaredSteps)-1]
	unavailable(t, register(x.r, stepToken("S-1", last), endpointFor(last)))
	if _, _, ok := x.wk.Endpoint("S-1", last); ok {
		t.Errorf("the worker %s is registered although the answer was 503", last)
	}
	// The same request again, once the service is back, registers it and signals.
	x.runs.setDown(false)
	if w := register(x.r, stepToken("S-1", last), endpointFor(last)); w.Code != 200 {
		t.Fatalf("registering again: %d: %s", w.Code, w.Body.String())
	}
	if got := x.runs.seen(); len(got) != 1 || got[0] != (durableCall{"signal", "RUN-1", "workers-ready"}) {
		t.Errorf("durable calls = %v, want one workers-ready signal to RUN-1", got)
	}
}

// ---- status and outcome come from the record ----

func TestARunsStatusAndOutcomeComeFromItsRecord(t *testing.T) {
	cases := []struct {
		name            string
		kinds           []string
		status, outcome string
	}{
		{"interrupted", []string{"RunInterrupted=the step workers are not running"}, "interrupted", ""},
		{"waiting", []string{"RunHandedOver=the step's checks failed 3 times"}, "waiting-for-developer", ""},
		{"done", []string{"RunCompleted"}, "ended", "done"},
		{"cancelled", []string{"RunCancelled=the run was cancelled"}, "ended", "cancelled"},
		{"handed over", []string{"RunHandedOver=x", "HandOverAccepted"}, "ended", "handed-over"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := newDurableRig(t, true)
			x.record(c.kinds...)
			p := x.do("GET", "/runs/RUN-1?api-version="+apiVersion, "")
			if p.Code != 200 {
				t.Fatalf("getRun = %d: %s", p.Code, p.Body.String())
			}
			var v runView
			if err := json.Unmarshal(p.Body.Bytes(), &v); err != nil {
				t.Fatal(err)
			}
			if v.Status != c.status {
				t.Errorf("status = %q, want %q", v.Status, c.status)
			}
			if c.outcome == "" && v.Outcome != nil || c.outcome != "" && (v.Outcome == nil || *v.Outcome != c.outcome) {
				t.Errorf("outcome = %v, want %q", v.Outcome, c.outcome)
			}
			if n := len(x.runs.seen()); n != 0 {
				t.Errorf("reading a run made %d durable calls, want none", n)
			}
		})
	}
}
