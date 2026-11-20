package machine_test

// T012: failing tests for the run workflow (T016 makes (k) and (l) pass, T017 the rest). They use
// Temporal's testsuite.WorkflowTestSuite: activities are fakes registered under their names, and
// time skips. They do not compile until T016 adds go.temporal.io/sdk v1.51.0 and the package
// durable; that is the expected failure (research R3, R9).
//
// THE INTERFACE (data model, "The engine's durable interface"; the smallest the tests need).
// Package github.com/agentic-platform/engine/durable (src/engine/durable). It may import
// package machine for its domain types; machine's rules (T017) are the workflow's code.
//
// Workflow, registered by its function name "Run", task queue "runs", ID RUN-<n>:
//
//	func Run(ctx workflow.Context, in RunInput) (RunResult, error)
//	type RunInput struct {
//		Run, Slice, Project string
//		Pattern  machine.Pattern
//		Model    string            // the model the pattern's agents use (part of a reuse key)
//		Inputs   map[string]string // the slice's part hashes by input name (machine.StartParams)
//		Refs     []string          // the slice's refs, on every event without refs of its own
//		ReplayOf string            // "" for a run that is not a replay
//	}
//	type RunResult struct{ Outcome machine.Outcome }
//
// An ended run returns its outcome and a nil error. A run that waits (hand-over, interruption)
// keeps the workflow open until a signal or its expiry ends or continues it.
//
// Activities, registered under these names (constants of package durable whose value is the name):
//
//	CallWorker     func(context.Context, WorkerCall) (WorkerResult, error)
//	RunCheck       func(context.Context, CheckCall) (CheckResult, error)
//	RestoreProject func(context.Context, machine.RestoreRequest) (Ran, error)
//	CommitStep     func(context.Context, machine.CommitRequest) (Ran, error)
//	LookupReuse    func(context.Context, ReuseLookup) (Reused, error)
//	PutResult      func(context.Context, ReusePut) error
//	RecordEvent    func(context.Context, EventRecord) error
//
//	type WorkerCall struct { // machine.Request, with findings by hash
//		Run, Slice, Project string
//		Step                machine.Step
//		Attempt, Round      int
//		Inputs              []string          // hashes of the step's declared inputs, in order
//		Open                []Finding         // the open blocking findings (a fix, a later round)
//		ChangedLines        []machine.Line    // a later round: the lines the fix changed
//		Before              []machine.File
//		Artifacts           map[string]string
//		TimeLimit           time.Duration     // the call's time limit
//	}
//	type WorkerResult struct { // machine.Result without content: outputs by hash, findings by hash
//		Model                string
//		Outputs              map[string]string
//		Usage                machine.Usage
//		Findings             []Finding
//		ChangedLines         []machine.Line
//		Before, ProjectFiles []machine.File
//		Took                 time.Duration // how long the call ran
//	}
//	type Finding struct { // machine.Finding without its text; the text stays in the artifact store
//		SHA256     string // the hash of the finding (its machine.Finding JSON)
//		Rule, File string
//		Line       int
//		Blocking   bool
//		Check      string
//		Witness    *machine.Witness
//	}
//	type CheckCall struct { // machine.CheckRequest, with the attempt's result by hash
//		Run, Slice, Project  string
//		Step                 machine.Step
//		Attempt, Round       int
//		Name                 string
//		Inputs               []string
//		Outputs              map[string]string // the attempt's outputs by name
//		Findings             []Finding         // the attempt's findings, in the worker's order
//		Before, ProjectFiles []machine.File
//		Open                 []Finding
//		ChangedLines         []machine.Line
//		TimeLimit            time.Duration
//	}
//	type CheckResult struct { // machine.CheckResult and its running time
//		Passed    bool
//		Reason    string
//		Refs      []string
//		Confirmed []int
//		Took      time.Duration
//	}
//	type Ran struct{ Took time.Duration } // RestoreProject's and CommitStep's result
//	type ReuseLookup struct{ Key string; TimeLimit time.Duration }
//	type Reused struct{ Found bool }       // T017 adds the reused result's fields
//	type ReusePut struct{ Key string; TimeLimit time.Duration } // T017 adds the result's fields
//	type EventRecord struct { // the sketch's (slice, run, seq, event): run and seq are in Event
//		Slice     string
//		Event     machine.Event  // Event.Finding is nil: a FindingRecorded's finding is Finding
//		Finding   *Finding
//		Project   string         // Event.Project, which machine.Event does not marshal
//		Files     []machine.File // Event.Files, likewise
//		TimeLimit time.Duration
//	}
//
// The tests add no field beyond these; T016 and T017 may add fields.
//
// Errors (activity application error types, constants of package durable whose value is the name):
// WorkersNotReady and ModelUnavailable from CallWorker, SeqTaken from RecordEvent.
//
// Signals (constants of package durable, no payload): SignalWorkersReady "workers-ready",
// SignalResume "resume", SignalAcceptHandOver "accept-hand-over", SignalCancel "cancel".
//
// Choices T012 and the sketch left open, made simplest:
//   - The platform stopping is an activity that ends with a heartbeat timeout
//     (*temporal.TimeoutError, TIMEOUT_TYPE_HEARTBEAT); the fakes return
//     temporal.NewHeartbeatTimeoutError(). Any other error is the call failing.
//   - Every activity's input has TimeLimit, the call's time limit; its StartToClose is
//     TimeLimit + 30 s.
//   - Restore and commit take feature 001's machine.RestoreRequest and machine.CommitRequest.
//   - Cancel is the signal "cancel"; the run returns RunResult{Outcome: "cancelled"}.
//   - The retry wait's random part is the workflow's only workflow.SideEffect, a float64 in
//     [0, 1) given to retry.Rule.Wait; case (j) mocks it with OnSideEffect.
//   - A repeat after a stop, or after workers-ready, makes the same attempt with the same number.
//
// The tests register it as:
//
//	env.RegisterWorkflow(durable.Run)
//	env.RegisterActivityWithOptions(fake, activity.RegisterOptions{Name: durable.CallWorker}) // each
//	env.ExecuteWorkflow(durable.Run, in)
//	env.SignalWorkflow(durable.SignalResume, nil)
//
// Cases of T012 and their tests:
//   (a) TestWorkflowUninterruptedRunRecordsFeature001Events                       workflow_stop_test.go
//   (b) TestWorkflowHeartbeatTimeoutRepeatsTheSameAttempt,
//       TestWorkflowChecksFailingThreeTimesAcrossStopsHandOver                      workflow_stop_test.go
//   (c) TestWorkflowStopBetweenStepAcceptedAndCommitCommitsOnce                   workflow_stop_test.go
//   (d) TestWorkflowStopDuringReviewRoundKeepsFindingsAndScope                    workflow_stop_test.go
//   (e) TestWorkflowUseBeforeAStopStillCounts, TestWorkflowTimeBudgetSumsRunningTimes workflow_stop_test.go
//   (m) TestWorkflowStopDuringCheckRunsItAgain, TestWorkflowCheckThatCannotRunCountsAsFailed workflow_stop_test.go
//   (f) TestWorkflowHandOverWaitsForTheDeveloper                                  workflow_wait_test.go
//   (g) TestWorkflowWaitsForWorkersReady                                          workflow_wait_test.go
//   (h) TestWorkflowModelUnavailableOnEveryAttemptWaitsForResume                  workflow_wait_test.go
//   (i) TestWorkflowCancelEndsTheRun                                              workflow_wait_test.go
//   (j) TestWorkflowFailingRestoreOrCommitIsRetriedThenInterrupts                 workflow_wait_test.go
//   (k) TestWorkflowActivityOptions                                               workflow_limits_test.go
//   (l) TestWorkflowPayloadsHoldNoContent                                         workflow_limits_test.go
//   (n) TestWorkflowHistoryStaysUnderTheLimit                                     workflow_limits_test.go

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

var wfT0 = time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)

const (
	wfModel  = "claude-haiku-5-5"
	wfDigest = "2222222222222222222222222222222222222222222222222222222222222222"
	// Content only an activity may see (FR-016): none of it may reach a payload. Not hex, so no
	// hash contains it by chance.
	wfPrompt      = "zq-PROMPT-the-agent-was-told"
	wfCompletion  = "zq-COMPLETION-the-model-answered"
	wfFileContent = "zq-FILE-CONTENT-package-app"
	wfFindingText = "zq-FINDING-TEXT-wrong-status-code"
	// The detail texts of a stop (data model, Run record).
	wfStopped   = "the platform stopped"
	wfContinued = "continued after the platform stopped"
	wfNoWorkers = "the step workers are not running"
)

func wfHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// wfPattern is feature 001's test pattern: five steps, two checks each, 3 attempts, 3 rounds.
func wfPattern() machine.Pattern {
	return machine.Pattern{
		ID: "api-feature-go", Version: "1.0.0", Attempts: 3, ReviewRounds: 3,
		RunBudget:  machine.Budget{Tokens: 3000000, CostUSD: 0.50, Duration: 30 * time.Minute},
		StepBudget: machine.Budget{Tokens: 1000000, CostUSD: 0.20, Duration: 10 * time.Minute},
		Steps: []machine.Step{
			{Name: "contract-check", Worker: "procedure:contract-check", Inputs: []string{"slice/contract", "slice/rules"},
				Outputs: []string{"contract", "examples"}, Checks: []string{"ContractMatchesApproval", "ScopeKept"}},
			{Name: "generate", Worker: "procedure:generate", Inputs: []string{"contract"},
				Outputs: []string{"generated-code"}, Checks: []string{"GeneratedCodeBuilds", "ScopeKept"}},
			{Name: "tests", Worker: "agent:tests", AgentDigest: wfDigest,
				Inputs:  []string{"contract", "examples", "generated-code", "slice/rules"},
				Outputs: []string{"tests"}, Checks: []string{"TestsBuild", "TestsFailAgainstStub"}},
			{Name: "code", Worker: "agent:code", AgentDigest: wfDigest,
				Inputs:  []string{"contract", "examples", "generated-code", "tests", "slice/rules"},
				Outputs: []string{"code"}, Checks: []string{"ScopeKept", "AllTestsPass"}},
			{Name: "review", Worker: "agent:review", AgentDigest: wfDigest,
				Inputs:  []string{"contract", "examples", "generated-code", "tests", "code", "slice/rules"},
				Outputs: []string{"findings"}, Checks: []string{"FindingsCarryEvidence", "NoBlockingFindingOpen"}},
		},
	}
}

// wfCall is one activity call as the fake saw it.
type wfCall struct {
	Name                    string
	In, Out                 any
	Err                     error
	Attempt                 int32 // Temporal's attempt of this activity: 1 unless Temporal retried it
	StartToClose, Heartbeat time.Duration
	At                      time.Time // the test clock when the call started
}

// wfHarness runs durable.Run in a test environment with fake activities. The fakes' behaviour is
// set by the hook fields before run; each hook may be nil (the default below).
type wfHarness struct {
	t   *testing.T
	env *testsuite.TestWorkflowEnvironment
	in  durable.RunInput

	// work is the step worker (default wfOK); took its running time (default 1 s).
	work func(c durable.WorkerCall) (machine.Result, error)
	took func(c durable.WorkerCall) time.Duration
	// check is the check service (default wfDefaultCheck); checkTook its running time (default 0).
	check     func(c durable.CheckCall) (machine.CheckResult, error)
	checkTook func(c durable.CheckCall) time.Duration
	// restore and commit are the project worker (default: succeed); commit runs only for a new stamp.
	restore func(r machine.RestoreRequest) error
	commit  func(r machine.CommitRequest) error
	// stop (before the call takes effect) and stopAfter (after it took effect) end a call with a
	// heartbeat timeout: the platform stopped.
	stop, stopAfter func(name string, in any) bool
	// fail ends a call with this error before it takes effect when it returns one.
	fail func(name string, in any) error

	mu      sync.Mutex
	calls   []wfCall
	records []durable.EventRecord // RecordEvent's accepted records, in seq order
	stamps  []machine.CommitRequest
	timers  int
	signals int
}

func newWF(t *testing.T, p machine.Pattern) *wfHarness {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	h := &wfHarness{t: t, env: suite.NewTestWorkflowEnvironment()}
	h.in = durable.RunInput{Run: "RUN-1", Slice: "S-1", Project: "/projects/p1", Pattern: p, Model: wfModel,
		Inputs: map[string]string{"slice/contract": wfHash("contract"), "slice/rules": wfHash("rules")},
		Refs:   []string{"G-1", "R-1", "R-1/E-1", "contract/getThing"}}
	h.env.SetStartTime(wfT0)
	h.env.SetTestTimeout(60 * time.Second)
	h.env.SetOnTimerScheduledListener(func(string, time.Duration) { h.mu.Lock(); h.timers++; h.mu.Unlock() })
	h.env.RegisterWorkflow(durable.Run)

	wfRegister(h, durable.CallWorker, func(_ context.Context, c durable.WorkerCall) (durable.WorkerResult, error) {
		work := h.work
		if work == nil {
			work = func(c durable.WorkerCall) (machine.Result, error) { return wfOK(c), nil }
		}
		res, err := work(c)
		if err != nil {
			return durable.WorkerResult{}, err
		}
		took := time.Second
		if h.took != nil {
			took = h.took(c)
		}
		return wfWorkerResult(t, c, res, took), nil
	})
	wfRegister(h, durable.RunCheck, func(_ context.Context, c durable.CheckCall) (durable.CheckResult, error) {
		check := h.check
		if check == nil {
			check = wfDefaultCheck
		}
		res, err := check(c)
		if err != nil {
			return durable.CheckResult{}, err
		}
		var took time.Duration
		if h.checkTook != nil {
			took = h.checkTook(c)
		}
		return wfCheckResult(t, res, took), nil
	})
	wfRegister(h, durable.RestoreProject, func(_ context.Context, r machine.RestoreRequest) (durable.Ran, error) {
		if h.restore != nil {
			if err := h.restore(r); err != nil {
				return durable.Ran{}, err
			}
		}
		return durable.Ran{}, nil
	})
	wfRegister(h, durable.CommitStep, func(_ context.Context, r machine.CommitRequest) (durable.Ran, error) {
		if h.commit != nil {
			if err := h.commit(r); err != nil {
				return durable.Ran{}, err
			}
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		if !slices.ContainsFunc(h.stamps, func(s machine.CommitRequest) bool { return wfStamp(s) == wfStamp(r) }) {
			h.stamps = append(h.stamps, r) // a repeat of a commit already made creates nothing
		}
		return durable.Ran{}, nil
	})
	wfRegister(h, durable.LookupReuse, func(_ context.Context, _ durable.ReuseLookup) (durable.Reused, error) {
		return durable.Reused{}, nil // nothing is reused: every step runs
	})
	wfRegisterErr(h, durable.PutResult, func(_ context.Context, _ durable.ReusePut) error { return nil })
	wfRegisterErr(h, durable.RecordEvent, func(_ context.Context, r durable.EventRecord) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		for _, have := range h.records { // the store accepts each seq once (data model, Run record)
			if have.Event.Seq == r.Event.Seq {
				if wfJSON(t, have) == wfJSON(t, r) {
					return nil
				}
				return temporal.NewNonRetryableApplicationError("seq taken", durable.SeqTaken, nil)
			}
		}
		h.records = append(h.records, r)
		return nil
	})
	return h
}

// wfRegister registers a fake activity under name, logging every call.
func wfRegister[I, O any](h *wfHarness, name string, fn func(context.Context, I) (O, error)) {
	h.env.RegisterActivityWithOptions(func(ctx context.Context, in I) (O, error) {
		c := h.begin(ctx, name, in)
		var out O
		var err error
		if err = h.before(name, in); err == nil {
			out, err = fn(ctx, in)
			if err == nil && h.stopAfter != nil && h.stopAfter(name, in) {
				err = temporal.NewHeartbeatTimeoutError()
			}
		}
		c.Out, c.Err = out, err
		h.end(c)
		return out, err
	}, activity.RegisterOptions{Name: name})
}

// wfRegisterErr is wfRegister for an activity with no result.
func wfRegisterErr[I any](h *wfHarness, name string, fn func(context.Context, I) error) {
	h.env.RegisterActivityWithOptions(func(ctx context.Context, in I) error {
		c := h.begin(ctx, name, in)
		err := h.before(name, in)
		if err == nil {
			err = fn(ctx, in)
			if err == nil && h.stopAfter != nil && h.stopAfter(name, in) {
				err = temporal.NewHeartbeatTimeoutError()
			}
		}
		c.Err = err
		h.end(c)
		return err
	}, activity.RegisterOptions{Name: name})
}

func (h *wfHarness) begin(ctx context.Context, name string, in any) wfCall {
	info := activity.GetInfo(ctx)
	return wfCall{Name: name, In: in, Attempt: info.Attempt, StartToClose: info.StartToCloseTimeout,
		Heartbeat: info.HeartbeatTimeout, At: h.env.Now()}
}

func (h *wfHarness) before(name string, in any) error {
	if h.stop != nil && h.stop(name, in) {
		return temporal.NewHeartbeatTimeoutError()
	}
	if h.fail != nil {
		return h.fail(name, in)
	}
	return nil
}

func (h *wfHarness) end(c wfCall) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, c)
}

// run starts the workflow and returns when it has completed or the test clock is 60 days on; a run
// still waiting then is cancelled, so every test ends.
func (h *wfHarness) run() {
	h.t.Helper()
	h.env.RegisterDelayedCallback(func() { h.env.SignalWorkflow(durable.SignalCancel, nil) }, 60*24*time.Hour)
	h.env.ExecuteWorkflow(durable.Run, h.in)
	if !h.env.IsWorkflowCompleted() {
		h.t.Fatal("the workflow did not complete")
	}
}

// signalAt sends a signal d after the run's start; before it runs, at is called (when not nil).
func (h *wfHarness) signalAt(d time.Duration, name string, at func()) {
	h.env.RegisterDelayedCallback(func() {
		if at != nil {
			at()
		}
		h.mu.Lock()
		h.signals++
		h.mu.Unlock()
		h.env.SignalWorkflow(name, nil)
	}, d)
}

// outcome is the ended run's outcome; the workflow must have ended without an error.
func (h *wfHarness) outcome() machine.Outcome {
	h.t.Helper()
	if err := h.env.GetWorkflowError(); err != nil {
		h.t.Fatalf("workflow error: %v", err)
	}
	var r durable.RunResult
	if err := h.env.GetWorkflowResult(&r); err != nil {
		h.t.Fatal(err)
	}
	return r.Outcome
}

func (h *wfHarness) wantOutcome(o machine.Outcome) {
	h.t.Helper()
	if got := h.outcome(); got != o {
		h.t.Fatalf("outcome = %q, want %q; events %v", got, o, h.kinds())
	}
}

func (h *wfHarness) events() []machine.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]machine.Event, len(h.records))
	for i, r := range h.records {
		out[i] = r.Event
	}
	return out
}

func (h *wfHarness) kinds() []string {
	var k []string
	for _, e := range h.events() {
		k = append(k, e.Kind)
	}
	return k
}

func (h *wfHarness) eventsOf(kind string) []machine.Event {
	var out []machine.Event
	for _, e := range h.events() {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// index is the position of the first event of kind at or after from, or -1.
func (h *wfHarness) index(kind string, from int) int {
	ev := h.events()
	for i := max(from, 0); i < len(ev); i++ {
		if ev[i].Kind == kind {
			return i
		}
	}
	return -1
}

func (h *wfHarness) callsOf(name string) []wfCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []wfCall
	for _, c := range h.calls {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

// callIndex is the position, in every call, of the n-th (from 0) call matching f, or -1.
func (h *wfHarness) callIndex(n int, f func(wfCall) bool) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, c := range h.calls {
		if f(c) {
			if n == 0 {
				return i
			}
			n--
		}
	}
	return -1
}

func (h *wfHarness) workerCalls(step string) []durable.WorkerCall {
	var out []durable.WorkerCall
	for _, c := range h.callsOf(durable.CallWorker) {
		if w := c.In.(durable.WorkerCall); w.Step.Name == step {
			out = append(out, w)
		}
	}
	return out
}

func (h *wfHarness) checkCalls(step, name string) []durable.CheckCall {
	var out []durable.CheckCall
	for _, c := range h.callsOf(durable.RunCheck) {
		if k := c.In.(durable.CheckCall); k.Step.Name == step && (name == "" || k.Name == name) {
			out = append(out, k)
		}
	}
	return out
}

func attemptsOf(calls []durable.WorkerCall) []int {
	var out []int
	for _, c := range calls {
		out = append(out, c.Attempt)
	}
	return out
}

func roundsOf(calls []durable.WorkerCall) []int {
	var out []int
	for _, c := range calls {
		out = append(out, c.Round)
	}
	return out
}

// wfOK is a step worker's good result: outputs by hash; the agent steps name the model.
func wfOK(c durable.WorkerCall) machine.Result {
	out := map[string]string{}
	for _, o := range c.Step.Outputs {
		out[o] = wfHash(fmt.Sprintf("%s/%s/%d/%d", c.Step.Name, o, c.Round, c.Attempt))
	}
	res := machine.Result{Outputs: out}
	if strings.HasPrefix(c.Step.Worker, "agent:") {
		res.Model = wfModel
	}
	return res
}

// wfBlocking is a confirmed blocking review finding whose text is wfFindingText.
func wfBlocking(rule string, line int) machine.Finding {
	return machine.Finding{Rule: rule, File: "internal/app/app.go", Line: line, Text: wfFindingText, Blocking: true,
		Check: "AllTestsPass", Witness: &machine.Witness{File: "review/r1/x_test.go", Test: "TestR1X"}}
}

func wfFindingHash(t *testing.T, f machine.Finding) string { return wfHash(wfJSON(t, f)) }

// wfWorkerResult is what the CallWorker activity returns for the worker's result: the worker knew
// the prompt, the completion and every output's content; the result type keeps what it declares.
// A field the type adds for any of them (Text, Content, Prompt, Completion) carries the planted
// text into the payload, and case (l) finds it.
func wfWorkerResult(t *testing.T, c durable.WorkerCall, r machine.Result, took time.Duration) durable.WorkerResult {
	r.Content = map[string][]byte{}
	for _, o := range c.Step.Outputs {
		r.Content[o] = []byte(wfFileContent)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(wfJSON(t, r)), &m); err != nil {
		t.Error(err)
	}
	m["Took"], m["Prompt"], m["Completion"] = took, wfPrompt, wfCompletion
	if fs, ok := m["Findings"].([]any); ok {
		for i, f := range fs {
			f.(map[string]any)["sha256"] = wfFindingHash(t, r.Findings[i])
		}
	}
	var out durable.WorkerResult
	if err := json.Unmarshal([]byte(wfJSON(t, m)), &out); err != nil {
		t.Errorf("a worker's result does not convert to durable.WorkerResult: %v", err)
	}
	return out
}

// wfCheckResult is the RunCheck activity's result, made the same way: the check knew its log.
func wfCheckResult(t *testing.T, r machine.CheckResult, took time.Duration) durable.CheckResult {
	var m map[string]any
	if err := json.Unmarshal([]byte(wfJSON(t, r)), &m); err != nil {
		t.Error(err)
	}
	m["Took"], m["Log"], m["Output"] = took, wfFileContent, wfCompletion
	var out durable.CheckResult
	if err := json.Unmarshal([]byte(wfJSON(t, m)), &out); err != nil {
		t.Errorf("a check's result does not convert to durable.CheckResult: %v", err)
	}
	return out
}

func wfPass(c durable.CheckCall) machine.CheckResult {
	return machine.CheckResult{Passed: true, Reason: c.Name + " holds"}
}

func wfFail(c durable.CheckCall) machine.CheckResult {
	return machine.CheckResult{Passed: false, Reason: c.Name + " does not hold"}
}

// wfDefaultCheck passes every check but NoBlockingFindingOpen when the attempt has a blocking
// finding with a witness, which it confirms (ADR 0042).
func wfDefaultCheck(c durable.CheckCall) (machine.CheckResult, error) {
	if c.Name == "NoBlockingFindingOpen" {
		var confirmed []int
		for i, f := range c.Findings {
			if f.Blocking && f.Witness != nil {
				confirmed = append(confirmed, i)
			}
		}
		if len(confirmed) > 0 {
			r := wfFail(c)
			r.Confirmed = confirmed
			return r, nil
		}
	}
	return wfPass(c), nil
}

// wfStamp is a commit's stamp: a repeat with the same stamp is the same commit.
func wfStamp(r machine.CommitRequest) string {
	return fmt.Sprintf("%s/%s/%d/%d/%t/%s", r.Run, r.Step.Name, r.Attempt, r.Round, r.OpenRound, r.ReusedFrom)
}

func (h *wfHarness) commitsOf(step string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, s := range h.stamps {
		if s.Step.Name == step {
			n++
		}
	}
	return n
}

// wfFirst returns a hook that is true for the n-th (from 1) call of name that matches f only.
func wfFirst(name string, n int, f func(in any) bool) func(string, any) bool {
	var mu sync.Mutex
	seen := 0
	return func(got string, in any) bool {
		if got != name || !f(in) {
			return false
		}
		mu.Lock()
		defer mu.Unlock()
		seen++
		return seen == n
	}
}

func wfWorkerOf(step string) func(any) bool {
	return func(in any) bool { return in.(durable.WorkerCall).Step.Name == step }
}

func wfJSON(t *testing.T, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		t.Errorf("marshal %T: %v", v, err)
	}
	return string(b)
}

// wfValidate checks every event against run-event.schema.json and that seq runs 1, 2, ... without
// a gap.
func wfValidate(t *testing.T, events []machine.Event) {
	t.Helper()
	dir, err := filepath.Abs("../api/preview/2026-11-12-preview")
	if err != nil {
		t.Fatal(err)
	}
	u := (&url.URL{Scheme: "file", Path: filepath.ToSlash(dir) + "/run-event.schema.json"}).String()
	s, err := jsonschema.NewCompiler().Compile(u)
	if err != nil {
		t.Fatalf("compile run-event schema: %v", err)
	}
	for i, e := range events {
		v, err := jsonschema.UnmarshalJSON(strings.NewReader(wfJSON(t, e)))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(v); err != nil {
			t.Fatalf("event %d (%s) is not a valid run event: %v", i+1, e.Kind, err)
		}
		if e.Seq != i+1 {
			t.Fatalf("event %d has seq %d, want %d", i+1, e.Seq, i+1)
		}
	}
}

// stopPairs counts the RunInterrupted `the platform stopped` that are directly followed
// by RunResumed `continued after the platform stopped`, and fails on a stop event out of that pair.
func (h *wfHarness) stopPairs() int {
	h.t.Helper()
	ev := h.events()
	n := 0
	for i, e := range ev {
		if e.Kind == "RunInterrupted" && e.Detail == wfStopped {
			if i+1 >= len(ev) || ev[i+1].Kind != "RunResumed" || ev[i+1].Detail != wfContinued {
				h.t.Fatalf("RunInterrupted %q at seq %d is not followed by RunResumed %q: %v", wfStopped, e.Seq, wfContinued, h.kinds())
			}
			n++
		}
	}
	return n
}

var errWFUnreachable = errors.New("the project worker is unreachable")
