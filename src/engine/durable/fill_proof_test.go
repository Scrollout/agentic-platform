package durable_test

// Feature 011, T006 (the `fill` cases): the workflow-level cases of a captured pattern's `fill`
// step, written before the code (T007) by a test author. Judged from spec FR-004/FR-005, the data
// model (Proof, "Run (added fields)"), the check contract's writeStep and T006/T007. Nothing here
// changes an existing test, helper or fixture.
//
// THE INTERFACE PINNED HERE. The code author (T007) implements exactly these symbols; this file
// is the only one of T006 that does not compile until then, and only on them.
//
// Package machine (src/engine/machine), an optional interface of the History a run gets, found by
// type assertion, so machine.History and its fakes stay unchanged. It reaches the check
// contract's `writeStep` (then reads the step's files back with `readStepFiles`):
//
//	type Writer interface {
//		// Write makes the files of the project that match r.Step.Writable exactly r.Files and
//		// returns the project's files afterwards. A repeat gives the same answer.
//		Write(ctx context.Context, r WriteRequest) (WriteResult, error)
//	}
//	type WriteRequest struct {
//		Run       string
//		Step      Step           // its Writable entries bound the paths
//		Attempt   int            // from 1
//		Round     int            // 0 for a fill step
//		Project   string
//		Before    []File         // the files the last accepted step left
//		Files     []canonical.File // the step's complete output files; empty for an empty output
//		TimeLimit time.Duration  // what is left of the run's time budget
//	}
//	type WriteResult struct {
//		Written, Removed []string // writeStep's answer, byte order
//		ProjectFiles     []File   // the project's files after the write (readStepFiles), byte order of path
//	}
//
// Package durable (src/engine/durable), in durable.go:
//
//	const WriteProject = "WriteProject" // the activity's registered name
//
//	// FillInput is what a run of a captured pattern's `fill` steps needs; RunInput.Fill is nil
//	// for any other run.
//	type FillInput struct {
//		Captured string            // the captured pattern's ID
//		Template map[string]string // output name -> hash of the files the source run ended with
//		Values   map[string]string // parameter name -> the value this run fills in
//	}
//	// RunInput gains:
//	//	Fill    *FillInput
//	//	NoReuse bool // the run reuses no earlier result (a proof): every step and check runs
//
//	// WriteCall is one attempt of a `fill` step: the fields of CallWorker's call (Run, Slice,
//	// Project, Step, Attempt, Round, Before, Artifacts, TimeLimit) and the captured pattern.
//	type WriteCall struct {
//		WorkerCall
//		Captured string
//		Template map[string]string
//		Values   map[string]string
//	}
//
// and in activities.go:
//
//	// CapturedParameters gives a captured pattern's parameters with their places.
//	type CapturedParameters interface {
//		Parameters(captured string) ([]captured.Parameter, error)
//	}
//	// Activities gains the field   Captured CapturedParameters
//	// and the activity (WorkerResult is CallWorker's, with no Model and no Usage):
//	func (a *Activities) WriteProject(ctx context.Context, c WriteCall) (WorkerResult, error)
//
// Rules pinned:
//   - A step whose Worker is "procedure:fill" is a fill step. The workflow makes no CallWorker
//     call for it: per attempt it calls RestoreProject, WriteProject, then RunCheck per declared
//     check; accepted, the next step's start (or the run's end) commits it. It reaches no model:
//     its events and reuse key carry no model, no agent digest and no usage.
//   - A run with a fill step and no RunInput.Fill, or a fill step whose outputs are not all in
//     Fill.Template, is refused as an invalid run before any activity (as a missing input is).
//   - A fill step's reuse key takes one more input after the step's inputs: the hash
//     `canonical.Hash(canonical.Object{<name>: <value>, ...})` of Fill.Values. No other step's
//     key changes. With RunInput.NoReuse no result is reused.
//   - WriteProject reads the step's template output(s) with Activities.Artifacts.Artifact (the
//     framed files, canonical.Unframe), fills them with captured.Fill over
//     Activities.Captured.Parameters(c.Captured) and c.Values, calls the History's Writer once
//     with the filled files (none for an empty output), stores the framed filled files with
//     Artifacts.PutArtifact(c.Slice, ...) and returns Outputs[name] = canonical.FramedHash of
//     them, ProjectFiles from the Writer's answer, Before = c.Before. An unreadable template
//     artifact, a History with no Writer, a fill that refuses, or a failing Writer gives an error
//     or a result whose Failure is set (as CallWorker's), and nothing is written when the template
//     could not be read or filled. With the source values the outputs' hashes equal the template's.
//   - The change goes behind the same workflow.GetVersion bump (`workflowVersion` 2): a history
//     recorded under version 1 replays on the code that ran it, a fill step then being a worker
//     step, and no WriteProject is scheduled under version 1.
//
// Choices left open and made simplest: a fill step is attempted once whatever the pattern's
// Attempts (the declaration sets 1; these cases use 1); an empty output still calls WriteProject
// and the Writer with no files (writeStep's "may be empty").

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/captured"
	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
	temporalworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

func fpHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// fpEmpty is the hash of an empty set of files: the framed form of no file is no bytes.
var fpEmpty = canonical.FramedHash(nil)

func fpPattern() machine.Pattern {
	return machine.Pattern{ID: "api-feature-go-notes", Version: "1.0.0", Attempts: 1, ReviewRounds: 1,
		RunBudget:  machine.Budget{Tokens: 3000000, CostUSD: 0.50, Duration: 30 * time.Minute},
		StepBudget: machine.Budget{Tokens: 1000000, CostUSD: 0.20, Duration: 10 * time.Minute},
		Steps: []machine.Step{
			{Name: "contract-check", Worker: "procedure:contract-check", Inputs: []string{"slice/contract"},
				Outputs: []string{"contract"}, Checks: []string{"ScopeKept"}, Writable: []string{"api/**"}},
			{Name: "tests", Worker: "procedure:fill", Inputs: []string{"contract"}, Outputs: []string{"tests"},
				Checks: []string{"TestsBuild", "ScopeKept"}, Writable: []string{"tests/**"}},
			{Name: "code", Worker: "procedure:fill", Inputs: []string{"contract", "tests"}, Outputs: []string{"code"},
				Checks: []string{"AllTestsPass"}, Writable: []string{"internal/**"}},
			{Name: "review", Worker: "procedure:fill", Inputs: []string{"contract", "tests", "code"}, Outputs: []string{"witnesses"},
				Checks: []string{"ScopeKept"}, Writable: []string{"review/**"}},
		}}
}

func fpInput() durable.RunInput {
	return durable.RunInput{Run: "RUN-7", Slice: "S-2", Project: "/projects/p2", Pattern: fpPattern(), Model: "claude-haiku-5-5",
		Inputs: map[string]string{"slice/contract": fpHash("contract")}, Refs: []string{"G-1", "R-1"},
		Fill: &durable.FillInput{Captured: "api-feature-go-notes",
			Template: map[string]string{"tests": fpHash("tests"), "code": fpHash("code"), "witnesses": fpEmpty},
			Values:   map[string]string{"thing": "memo"}}}
}

// fpRun is one run in Temporal's test environment with fake activities that log every call.
type fpRun struct {
	t   *testing.T
	env *testsuite.TestWorkflowEnvironment
	in  durable.RunInput

	// failCheck names "<step>/<check>" that fail; writeFails names the steps whose WriteProject
	// result has a Failure; reuse answers LookupReuse (nil: nothing kept).
	failCheck  map[string]bool
	writeFails map[string]bool
	reuse      func(l durable.ReuseLookup) durable.Reused

	mu      sync.Mutex
	log     []string
	workers []durable.WorkerCall
	writes  []durable.WriteCall
	checks  []durable.CheckCall
	commits []machine.CommitRequest
	lookups []durable.ReuseLookup
	events  []durable.EventRecord
	results map[string]durable.WorkerResult // WriteProject's results by step
}

func newFP(t *testing.T) *fpRun {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	suite.SetLogger(log.NewStructuredLogger(slog.New(slog.DiscardHandler)))
	f := &fpRun{t: t, env: suite.NewTestWorkflowEnvironment(), in: fpInput(), failCheck: map[string]bool{},
		writeFails: map[string]bool{}, results: map[string]durable.WorkerResult{}}
	f.env.SetTestTimeout(60 * time.Second)
	f.env.RegisterWorkflowWithOptions(durable.Run, workflow.RegisterOptions{Name: "Run"})
	reg := func(name string, fn any) { f.env.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name}) }
	add := func(format string, a ...any) {
		f.mu.Lock()
		f.log = append(f.log, fmt.Sprintf(format, a...))
		f.mu.Unlock()
	}
	reg(durable.CallWorker, func(_ context.Context, c durable.WorkerCall) (durable.WorkerResult, error) {
		add("worker %s", c.Step.Name)
		f.mu.Lock()
		f.workers = append(f.workers, c)
		f.mu.Unlock()
		out := map[string]string{}
		for _, o := range c.Step.Outputs {
			out[o] = fpHash(c.Step.Name + "/" + o)
		}
		files := append(slices.Clone(c.Before), machine.File{Path: "api/" + c.Step.Name + ".yaml", SHA256: fpHash("api/" + c.Step.Name)})
		return durable.WorkerResult{Outputs: out, Before: c.Before, ProjectFiles: files, Took: time.Second}, nil
	})
	reg(durable.WriteProject, func(_ context.Context, c durable.WriteCall) (durable.WorkerResult, error) {
		add("write %s", c.Step.Name)
		f.mu.Lock()
		f.writes = append(f.writes, c)
		f.mu.Unlock()
		if f.writeFails[c.Step.Name] {
			return durable.WorkerResult{Failure: "the project worker refused the write", Took: time.Second}, nil
		}
		out := map[string]string{}
		for _, o := range c.Step.Outputs {
			out[o] = c.Template[o]
		}
		files := slices.Clone(c.Before)
		if c.Template[c.Step.Outputs[0]] != fpEmpty { // an empty output writes no file
			files = append(files, machine.File{Path: "filled/" + c.Step.Name + ".go", SHA256: fpHash("filled/" + c.Step.Name)})
		}
		res := durable.WorkerResult{Outputs: out, Before: c.Before, ProjectFiles: files, Took: time.Second}
		f.mu.Lock()
		f.results[c.Step.Name] = res
		f.mu.Unlock()
		return res, nil
	})
	reg(durable.RunCheck, func(_ context.Context, c durable.CheckCall) (durable.CheckResult, error) {
		add("check %s %s", c.Step.Name, c.Name)
		f.mu.Lock()
		f.checks = append(f.checks, c)
		f.mu.Unlock()
		if f.failCheck[c.Step.Name+"/"+c.Name] {
			return durable.CheckResult{Passed: false, Reason: c.Name + " does not hold"}, nil
		}
		return durable.CheckResult{Passed: true, Reason: c.Name + " holds"}, nil
	})
	reg(durable.RestoreProject, func(_ context.Context, r machine.RestoreRequest) (durable.Ran, error) {
		add("restore %s", r.Step.Name)
		return durable.Ran{}, nil
	})
	reg(durable.CommitStep, func(_ context.Context, r machine.CommitRequest) (durable.Ran, error) {
		add("commit %s", r.Step.Name)
		f.mu.Lock()
		f.commits = append(f.commits, r)
		f.mu.Unlock()
		return durable.Ran{}, nil
	})
	reg(durable.LookupReuse, func(_ context.Context, l durable.ReuseLookup) (durable.Reused, error) {
		f.mu.Lock()
		f.lookups = append(f.lookups, l)
		f.mu.Unlock()
		if f.reuse != nil {
			return f.reuse(l), nil
		}
		return durable.Reused{}, nil
	})
	reg(durable.PutResult, func(context.Context, durable.ReusePut) error { return nil })
	reg(durable.RecordEvent, func(_ context.Context, r durable.EventRecord) error {
		add("record %s %s", r.Event.Kind, r.Event.Step)
		f.mu.Lock()
		f.events = append(f.events, r)
		f.mu.Unlock()
		return nil
	})
	return f
}

func (f *fpRun) run() {
	f.t.Helper()
	f.env.RegisterDelayedCallback(func() { f.env.SignalWorkflow(durable.SignalCancel, nil) }, 60*24*time.Hour)
	f.env.ExecuteWorkflow("Run", f.in)
	if !f.env.IsWorkflowCompleted() {
		f.t.Fatal("the workflow did not complete")
	}
}

func (f *fpRun) done() {
	f.t.Helper()
	f.run()
	if err := f.env.GetWorkflowError(); err != nil {
		f.t.Fatalf("workflow error: %v", err)
	}
	var r durable.RunResult
	if err := f.env.GetWorkflowResult(&r); err != nil || r.Outcome != machine.OutcomeDone {
		f.t.Fatalf("the run ended %q (%v), want done; log %v", r.Outcome, err, f.log)
	}
}

func (f *fpRun) at(entry string) int {
	f.t.Helper()
	i := slices.Index(f.log, entry)
	if i < 0 {
		f.t.Fatalf("%q was not called; log: %v", entry, f.log)
	}
	return i
}

func (f *fpRun) kinds() []string {
	var out []string
	for _, e := range f.events {
		out = append(out, e.Event.Kind+" "+e.Event.Step)
	}
	return out
}

func (f *fpRun) event(kind, step string) (machine.Event, bool) {
	for _, e := range f.events {
		if e.Event.Kind == kind && e.Event.Step == step {
			return e.Event, true
		}
	}
	return machine.Event{}, false
}

func (f *fpRun) writesOf(step string) (n int) {
	for _, w := range f.writes {
		if w.Step.Name == step {
			n++
		}
	}
	return n
}

// T006 clause: a `fill` step needs no worker; no agent worker and no model call is made.
func TestAFillStepIsWrittenNotWorkedAndMakesNoModelCall(t *testing.T) {
	f := newFP(t)
	f.done()
	var workers []string
	for _, w := range f.workers {
		workers = append(workers, w.Step.Name)
	}
	if !slices.Equal(workers, []string{"contract-check"}) {
		t.Errorf("CallWorker was called for %v, want only the procedure step contract-check: a fill step needs no worker", workers)
	}
	for _, step := range []string{"tests", "code", "review"} {
		if f.writesOf(step) != 1 {
			t.Errorf("WriteProject for %s was called %d times, want once", step, f.writesOf(step))
		}
		e, ok := f.event("StepAccepted", step)
		if !ok {
			t.Fatalf("no StepAccepted for %s: %v", step, f.kinds())
		}
		if e.Model != "" || e.AgentDigest != "" || e.Worker != "procedure:fill" || e.Usage != nil {
			t.Errorf("%s's StepAccepted names a model, agent or usage, or not the worker procedure:fill: %+v", step, e)
		}
	}
	for _, l := range f.lookups {
		if l.Parts.Model != "" || l.Parts.AgentDigest != "" {
			t.Errorf("the reuse key of %s covers a model or an agent digest: %+v", l.Parts.Step, l.Parts)
		}
	}
}

// T006 clause: each `fill` step renders (WriteProject), calls writeStep, runs the step's checks,
// then commitStep (the next step's start, or the run's end, makes the commit).
func TestAFillStepRestoresWritesChecksAndCommitsInThatOrder(t *testing.T) {
	f := newFP(t)
	f.done()
	for _, step := range []string{"tests", "code", "review"} {
		restore, write := f.at("restore "+step), f.at("write "+step)
		accepted, commit := f.at("record StepAccepted "+step), f.at("commit "+step)
		first := f.at("check " + step + " " + fpPattern().Steps[slices.IndexFunc(fpPattern().Steps, func(s machine.Step) bool { return s.Name == step })].Checks[0])
		if restore >= write || write >= first || first >= accepted || accepted >= commit {
			t.Errorf("%s: restore %d, write %d, first check %d, accepted %d, commit %d: want that order; log %v", step, restore, write, first, accepted, commit, f.log)
		}
	}
	if f.at("commit tests") > f.at("restore code") || f.at("commit code") > f.at("restore review") {
		t.Errorf("a step's commit must come before the next step's restore: %v", f.log)
	}
	if f.at("commit review") > f.at("record RunCompleted ") {
		t.Errorf("the last fill step's commit must come before RunCompleted: %v", f.log)
	}
	var tests []string
	for _, c := range f.checks {
		if c.Step.Name == "tests" {
			tests = append(tests, c.Name)
		}
	}
	if !slices.Equal(tests, []string{"TestsBuild", "ScopeKept"}) {
		t.Errorf("the checks run for tests = %v, want the declared ones in order", tests)
	}
}

// T006 clause: the write call carries the run, the step, the captured pattern, the template, the
// values and the files the last accepted step left; the checks judge what it wrote.
func TestTheWriteCallCarriesItsInputsAndTheChecksJudgeItsResult(t *testing.T) {
	f := newFP(t)
	f.done()
	var w durable.WriteCall
	for _, c := range f.writes {
		if c.Step.Name == "code" {
			w = c
		}
	}
	in := fpInput()
	if w.Run != in.Run || w.Slice != in.Slice || w.Project != in.Project || w.Attempt != 1 || w.Round != 0 {
		t.Errorf("write call identity = %s %s %s attempt %d round %d, want the run's, attempt 1, round 0", w.Run, w.Slice, w.Project, w.Attempt, w.Round)
	}
	if w.Captured != in.Fill.Captured || !mapsEqual(w.Template, in.Fill.Template) || !mapsEqual(w.Values, in.Fill.Values) {
		t.Errorf("write call captured/template/values = %q %v %v, want the run's Fill %+v", w.Captured, w.Template, w.Values, *in.Fill)
	}
	if want := f.results["tests"].ProjectFiles; !slices.Equal(w.Before, want) {
		t.Errorf("code's Before = %v, want the files after tests: %v", w.Before, want)
	}
	if !slices.Equal(w.Inputs, []string{fpHash("contract-check/contract"), in.Fill.Template["tests"]}) {
		t.Errorf("code's input hashes = %v, want contract then tests", w.Inputs)
	}
	if w.TimeLimit <= 0 {
		t.Errorf("TimeLimit = %v, want what is left of the run's budget", w.TimeLimit)
	}
	for _, c := range f.checks {
		if c.Step.Name != "code" {
			continue
		}
		got := f.results["code"]
		if !mapsEqual(c.Outputs, got.Outputs) || !slices.Equal(c.ProjectFiles, got.ProjectFiles) || c.Model != "" {
			t.Errorf("check %s was given outputs %v files %v model %q, want what WriteProject returned: %v %v", c.Name, c.Outputs, c.ProjectFiles, c.Model, got.Outputs, got.ProjectFiles)
		}
	}
}

func mapsEqual(a, b map[string]string) bool {
	return len(a) == len(b) && func() bool {
		for k, v := range a {
			if b[k] != v {
				return false
			}
		}
		return true
	}()
}

// T006 clause: a failing check fails the step; a `fill` step is never attempted twice.
func TestAFillStepWhoseCheckFailsIsNotAttemptedAgainAndHandsOver(t *testing.T) {
	f := newFP(t)
	f.failCheck["tests/TestsBuild"] = true
	f.run()
	if n := f.writesOf("tests"); n != 1 {
		t.Errorf("WriteProject for tests was called %d times, want once: a fill step is never attempted twice", n)
	}
	if f.writesOf("code") != 0 {
		t.Errorf("the step after a failed one was written: %v", f.log)
	}
	if _, ok := f.event("StepAccepted", "tests"); ok || slices.Contains(f.log, "commit tests") {
		t.Errorf("a step with a failing check was accepted or committed: %v", f.kinds())
	}
	var over string
	for _, e := range f.events {
		if e.Event.Kind == "RunHandedOver" {
			over = e.Event.Detail
		}
	}
	if !strings.Contains(over, "tests") || !strings.Contains(over, "TestsBuild") {
		t.Errorf("the hand-over %q does not name step tests and check TestsBuild", over)
	}
}

// T006 clause: a write that fails is not repeated either (edge: failure at the write step).
func TestAFillStepWhoseWriteFailsIsNotAttemptedAgainAndIsNotCommitted(t *testing.T) {
	f := newFP(t)
	f.writeFails["tests"] = true
	f.run()
	if n := f.writesOf("tests"); n != 1 {
		t.Errorf("WriteProject for tests was called %d times, want once", n)
	}
	checked := slices.ContainsFunc(f.checks, func(c durable.CheckCall) bool { return c.Step.Name == "tests" })
	if slices.Contains(f.log, "commit tests") || checked {
		t.Errorf("a failed write was committed or checked: %v", f.log)
	}
	if _, ok := f.event("StepAccepted", "tests"); ok {
		t.Errorf("a failed write was accepted")
	}
}

// T006 clause: a `fill` step whose template output is empty (a clean review's `witnesses`)
// writes nothing, commits, and its output hash is the empty set's.
func TestAFillStepWithAnEmptyTemplateOutputWritesNothingAndCommitsTheFilesItStartedFrom(t *testing.T) {
	f := newFP(t)
	f.done()
	var w durable.WriteCall
	for _, c := range f.writes {
		if c.Step.Name == "review" {
			w = c
		}
	}
	if w.Template["witnesses"] != fpEmpty {
		t.Fatalf("setup: the review's template output is not the empty set")
	}
	e, ok := f.event("StepAccepted", "review")
	if !ok || !slices.Equal(e.Artifacts, []string{fpEmpty}) {
		t.Errorf("review's StepAccepted artifacts = %v, want the empty set's hash %s", e.Artifacts, fpEmpty)
	}
	var c machine.CommitRequest
	for _, r := range f.commits {
		if r.Step.Name == "review" {
			c = r
		}
	}
	if !slices.Equal(c.ProjectFiles, c.Before) || len(c.Before) == 0 {
		t.Errorf("review's commit has files %v, want exactly the files it started from %v", c.ProjectFiles, c.Before)
	}
}

// T006 clause: the `fill` steps' reuse keys take the parameter values' hash; no other key changes.
func TestAFillStepsReuseKeyTakesTheHashOfTheValues(t *testing.T) {
	keys := func(values map[string]string) map[string][]string {
		f := newFP(t)
		f.in.Fill.Values = values
		f.done()
		out := map[string][]string{}
		for _, l := range f.lookups {
			out[l.Parts.Step] = l.Parts.Inputs
		}
		return out
	}
	memo, idea := keys(map[string]string{"thing": "memo"}), keys(map[string]string{"thing": "idea"})
	want, err := canonical.Hash(canonical.Object{"thing": "memo"})
	if err != nil {
		t.Fatal(err)
	}
	if got := memo["tests"]; !slices.Equal(got, []string{fpHash("contract-check/contract"), want}) {
		t.Errorf("tests' reuse inputs = %v, want its input hash then %s (the hash of the values)", got, want)
	}
	if slices.Equal(memo["tests"], idea["tests"]) || slices.Equal(memo["review"], idea["review"]) {
		t.Errorf("two runs that differ only in a value share a fill step's reuse inputs")
	}
	if len(memo["contract-check"]) != 1 || !slices.Equal(memo["contract-check"], idea["contract-check"]) {
		t.Errorf("a procedure step's reuse inputs changed with the values: %v vs %v", memo["contract-check"], idea["contract-check"])
	}
}

func fpFound(l durable.ReuseLookup, outputs []string) durable.Reused {
	var arts []string
	for _, o := range outputs {
		arts = append(arts, fpHash("kept/"+l.Parts.Step+"/"+o))
	}
	return durable.Reused{Key: "k-" + l.Parts.Step, Inputs: l.Parts.Inputs, Found: true, Artifacts: arts, Run: "RUN-1",
		Files: []machine.File{{Path: "kept/" + l.Parts.Step, SHA256: fpHash("kept/" + l.Parts.Step)}}}
}

// T006 clauses: a reused fill step is not written (normal runs reuse); a run with NoReuse (a
// proof) reuses no earlier result and runs every step.
func TestAReusedFillStepIsNotWrittenAndANoReuseRunReusesNothing(t *testing.T) {
	outputs := map[string][]string{}
	for _, s := range fpPattern().Steps {
		outputs[s.Name] = s.Outputs
	}
	reuse := func(l durable.ReuseLookup) durable.Reused { return fpFound(l, outputs[l.Parts.Step]) }

	f := newFP(t)
	f.reuse = reuse
	f.done()
	for _, step := range []string{"tests", "code", "review"} {
		if _, ok := f.event("ResultReused", step); !ok {
			t.Errorf("%s was not reused though a result was kept: %v", step, f.kinds())
		}
		if f.writesOf(step) != 0 {
			t.Errorf("a reused fill step %s was written", step)
		}
	}

	p := newFP(t)
	p.reuse = reuse
	p.in.NoReuse = true
	p.done()
	for _, e := range p.events {
		if e.Event.Kind == "ResultReused" {
			t.Errorf("a NoReuse run reused %s", e.Event.Step)
		}
	}
	if len(p.workers) != 1 || p.writesOf("tests") != 1 || p.writesOf("code") != 1 || p.writesOf("review") != 1 {
		t.Errorf("a NoReuse run did not run every step: workers %d, writes %v", len(p.workers), p.log)
	}
}

// Edge, fail closed: a fill step with no Fill input, or with an output the template lacks, is an
// invalid run, refused before any activity.
func TestAFillStepWithoutItsTemplateIsAnInvalidRunRefusedBeforeAnyActivity(t *testing.T) {
	cases := map[string]func(*durable.RunInput){
		"no Fill input":       func(in *durable.RunInput) { in.Fill = nil },
		"no template at all":  func(in *durable.RunInput) { in.Fill.Template = nil },
		"an output not in it": func(in *durable.RunInput) { delete(in.Fill.Template, "code") },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFP(t)
			mod(&f.in)
			f.run()
			if f.env.GetWorkflowError() == nil {
				t.Error("the run was not refused")
			}
			if len(f.log) != 0 {
				t.Errorf("activities ran before the refusal: %v", f.log)
			}
		})
	}
}

// ---- the recorded histories (the version gate) ----

func fpHistories(t *testing.T) map[string]*historypb.History {
	t.Helper()
	root, err := os.OpenRoot(filepath.Join("..", "machine", "testdata", "histories"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	files, err := fs.Glob(root.FS(), "*.json.gz")
	if err != nil || len(files) == 0 {
		t.Fatalf("no recorded history under machine/testdata/histories: %v", err)
	}
	out := map[string]*historypb.History{}
	for _, name := range files {
		out[name] = fpRead(t, root, name)
	}
	return out
}

func fpRead(t *testing.T, root *os.Root, name string) *historypb.History {
	t.Helper()
	fh, err := root.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fh.Close() }()
	zr, err := gzip.NewReader(fh)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	h, err := client.HistoryFromJSON(bytes.NewReader(b), client.HistoryJSONOptions{})
	if err != nil || len(h.Events) == 0 {
		t.Fatalf("%s: not a history: %v", name, err)
	}
	return h
}

func fpReplay(h *historypb.History) error {
	r := temporalworker.NewWorkflowReplayer()
	r.RegisterWorkflowWithOptions(durable.Run, workflow.RegisterOptions{Name: "Run"})
	return r.ReplayWorkflowHistory(log.NewStructuredLogger(slog.New(slog.DiscardHandler)), h)
}

// Regression guard (exempt from "seen failing"): the existing recorded histories still replay.
func TestTheRecordedHistoriesStillReplayWithTheFillPath(t *testing.T) {
	for name, h := range fpHistories(t) {
		t.Run(name, func(t *testing.T) {
			t.Logf("examined %s: %d events", name, len(h.Events))
			if err := fpReplay(h); err != nil {
				t.Fatalf("replay of %s failed: %v", name, err)
			}
		})
	}
}

// fpWithFillStep rewrites the recorded run's input so that its first step, contract-check, is a
// `fill` step with a Fill input, as a run of a captured pattern started under the new code would
// have; the activities the history recorded are unchanged.
func fpWithFillStep(t *testing.T, h *historypb.History) {
	t.Helper()
	p := h.Events[0].GetWorkflowExecutionStartedEventAttributes().GetInput().GetPayloads()[0]
	dec := json.NewDecoder(bytes.NewReader(p.Data))
	dec.UseNumber()
	var in map[string]any
	if err := dec.Decode(&in); err != nil {
		t.Fatal(err)
	}
	steps := in["Pattern"].(map[string]any)["Steps"].([]any)
	steps[0].(map[string]any)["Worker"] = "procedure:fill"
	in["Fill"] = map[string]any{"Captured": "c-notes", "Template": map[string]string{"contract": fpHash("contract"), "examples": fpHash("examples")},
		"Values": map[string]string{"thing": "memo"}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	p.Data = b
}

// fpSchedules says whether h schedules an activity called name.
func fpSchedules(h *historypb.History, name string) bool {
	for _, e := range h.Events {
		if a := e.GetActivityTaskScheduledEventAttributes(); a != nil && a.ActivityType.Name == name {
			return true
		}
	}
	return false
}

// fpRenameFirst renames the first scheduled activity called from to to, as a history recorded by
// code that scheduled to there.
func fpRenameFirst(t *testing.T, h *historypb.History, from, to string) {
	t.Helper()
	for _, e := range h.Events {
		if a := e.GetActivityTaskScheduledEventAttributes(); a != nil && a.ActivityType.Name == from {
			a.ActivityType.Name = to
			return
		}
	}
	t.Fatalf("the history schedules no %s", from)
}

// T006 clause, the version gate: a history recorded under workflow version 1 (before the fill
// path) in which a step is a `fill` step still replays: the fill path is behind the bump. Without
// the gate the replay finds WriteProject where CallWorker was recorded.
func TestAHistoryRecordedBeforeTheFillPathReplaysItsFillStepAsAWorkerStep(t *testing.T) {
	for name, h := range fpHistories(t) {
		t.Run(name, func(t *testing.T) {
			fpWithFillStep(t, h)
			if err := fpReplay(h); err != nil {
				t.Fatalf("replay of %s with a fill step under version 1 failed: %v", name, err)
			}
		})
	}
}

// T006 clause (planted): a history recorded under version 1 in which the fill path ran (a
// WriteProject where the version-1 code called CallWorker) fails the replay as nondeterminism.
func TestAPlantedHistoryInWhichTheFillPathRanWithoutItsVersionGateFailsTheReplay(t *testing.T) {
	for name, h := range fpHistories(t) {
		t.Run(name, func(t *testing.T) {
			if !fpSchedules(h, "CallWorker") {
				t.Logf("%s schedules no CallWorker (every step reused): nothing to plant", name)
				return
			}
			fpWithFillStep(t, h)
			fpRenameFirst(t, h, "CallWorker", durable.WriteProject)
			err := fpReplay(h)
			if err == nil {
				t.Fatalf("replay of %s passed with the fill path run under version 1", name)
			}
			if !strings.Contains(err.Error(), "TMPRL1100") && !strings.Contains(strings.ToLower(err.Error()), "nondeterministic") {
				t.Fatalf("replay failed, but not as a nondeterminism error: %v", err)
			}
		})
	}
}

// ---- the Writer interface and the WriteProject activity ----

type fpPlain struct{}

func (fpPlain) Restore(context.Context, machine.RestoreRequest) error { return nil }
func (fpPlain) Commit(context.Context, machine.CommitRequest) (bool, error) {
	return true, nil
}

type fpWriter struct {
	fpPlain
	mu    sync.Mutex
	reqs  []machine.WriteRequest
	after []machine.File
	err   error
}

func (w *fpWriter) Write(_ context.Context, r machine.WriteRequest) (machine.WriteResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.reqs = append(w.reqs, r)
	if w.err != nil {
		return machine.WriteResult{}, w.err
	}
	return machine.WriteResult{ProjectFiles: w.after}, nil
}

var (
	_ machine.History = fpPlain{}
	_ machine.History = (*fpWriter)(nil)
	_ machine.Writer  = (*fpWriter)(nil)
)

// Writer is optional: a History with only Restore and Commit is a History and not a Writer.
func TestWriterIsAnOptionalInterfaceOfAHistory(t *testing.T) {
	var h machine.History = fpPlain{}
	if _, ok := h.(machine.Writer); ok {
		t.Error("a History with no Write is a Writer")
	}
	h = &fpWriter{}
	if _, ok := h.(machine.Writer); !ok {
		t.Error("a History with Write is not a Writer")
	}
}

type fpArtifacts struct {
	mu   sync.Mutex
	data map[string][]byte
	puts []string // the slice of each PutArtifact
}

func (a *fpArtifacts) PutArtifact(slice string, b []byte) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := sha256.Sum256(b)
	k := hex.EncodeToString(h[:])
	a.data[k] = bytes.Clone(b)
	a.puts = append(a.puts, slice)
	return k, nil
}

func (a *fpArtifacts) Artifact(hash string) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if b, ok := a.data[hash]; ok {
		return b, nil
	}
	return nil, fs.ErrNotExist
}

type fpParams struct {
	ps  []captured.Parameter
	err error
}

func (p fpParams) Parameters(string) ([]captured.Parameter, error) { return p.ps, p.err }

// fpSetup is a captured pattern whose `tests` output holds service/note.go with the name `note`
// (value "note", kind name) at its path and in its content, and an empty `witnesses` output.
func fpSetup(t *testing.T) (*durable.Activities, *fpWriter, *fpArtifacts, durable.WriteCall, []canonical.File) {
	t.Helper()
	src := []canonical.File{{Path: "service/note.go", Content: []byte("package note\n")}}
	arts := &fpArtifacts{data: map[string][]byte{}}
	h, err := arts.PutArtifact("S-1", canonical.Framed(src))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := arts.PutArtifact("S-1", nil); err != nil {
		t.Fatal(err)
	}
	w := &fpWriter{after: []machine.File{{Path: "service/memo.go", SHA256: fpHash("package memo\n")}}}
	params := fpParams{ps: []captured.Parameter{{Name: "thing", Kind: captured.KindName, Value: "note", Source: captured.SourceName,
		Origin: captured.OriginProposed, Places: []captured.Place{
			{Part: "service/note.go", In: captured.InPath, Line: 1, Column: 9, Form: captured.FormLower},
			{Part: "service/note.go", In: captured.InContent, Line: 1, Column: 9, Form: captured.FormLower}}}}}
	a := &durable.Activities{History: w, Artifacts: arts, Captured: params}
	before := []machine.File{{Path: "api/openapi.yaml", SHA256: fpHash("api")}}
	c := durable.WriteCall{
		WorkerCall: durable.WorkerCall{Run: "RUN-7", Slice: "S-2", Project: "/projects/p2", Attempt: 1, Before: before,
			Step:      machine.Step{Name: "tests", Worker: "procedure:fill", Outputs: []string{"tests"}, Writable: []string{"service/**"}},
			TimeLimit: time.Minute},
		Captured: "api-feature-go-notes", Template: map[string]string{"tests": h, "witnesses": fpEmpty},
		Values: map[string]string{"thing": "memo"}}
	return a, w, arts, c, src
}

// T006 clauses: the step renders the template with the values, calls writeStep through the
// History's Writer once, and returns the hash of what it filled and the files after the write.
func TestWriteProjectRendersTheTemplateAndCallsTheWriterOnce(t *testing.T) {
	a, w, arts, c, _ := fpSetup(t)
	res, err := a.WriteProject(context.Background(), c)
	if err != nil || res.Failure != "" {
		t.Fatalf("WriteProject = %+v, %v", res, err)
	}
	filled := []canonical.File{{Path: "service/memo.go", Content: []byte("package memo\n")}}
	if len(w.reqs) != 1 {
		t.Fatalf("the Writer was called %d times, want once", len(w.reqs))
	}
	r := w.reqs[0]
	if r.Run != c.Run || r.Project != c.Project || r.Step.Name != "tests" || r.Attempt != 1 || r.Round != 0 ||
		!slices.Equal(r.Before, c.Before) || r.TimeLimit <= 0 || r.TimeLimit > c.TimeLimit {
		t.Errorf("the write request = %+v, want the call's run, project, step, attempt 1, round 0, before and a time limit within the call's", r)
	}
	if len(r.Files) != 1 || r.Files[0].Path != filled[0].Path || !bytes.Equal(r.Files[0].Content, filled[0].Content) {
		t.Errorf("the Writer was given %v, want %v (path and content filled)", r.Files, filled)
	}
	want := canonical.FramedHash(filled)
	if res.Outputs["tests"] != want {
		t.Errorf("Outputs[tests] = %s, want the hash of the filled files %s", res.Outputs["tests"], want)
	}
	if b, err := arts.Artifact(want); err != nil || !bytes.Equal(b, canonical.Framed(filled)) {
		t.Errorf("the filled files are not stored under their hash: %v", err)
	}
	if !slices.Equal(res.ProjectFiles, w.after) || !slices.Equal(res.Before, c.Before) || res.Model != "" || res.Usage != (machine.Usage{}) {
		t.Errorf("result files %v before %v model %q usage %+v, want the Writer's files, the call's before, no model, no usage", res.ProjectFiles, res.Before, res.Model, res.Usage)
	}
}

// T006 clause: a proof fills with the source values, so its output hash is the template's own.
func TestWriteProjectWithTheSourceValuesGivesTheTemplatesOwnHash(t *testing.T) {
	a, _, _, c, _ := fpSetup(t)
	c.Values = map[string]string{"thing": "note"}
	res, err := a.WriteProject(context.Background(), c)
	if err != nil || res.Failure != "" {
		t.Fatalf("WriteProject = %+v, %v", res, err)
	}
	if res.Outputs["tests"] != c.Template["tests"] {
		t.Errorf("Outputs[tests] = %s, want the template's hash %s", res.Outputs["tests"], c.Template["tests"])
	}
}

// T006 clause: an empty template output writes nothing: the Writer is given no file.
func TestWriteProjectOfAnEmptyOutputWritesNoFile(t *testing.T) {
	a, w, _, c, _ := fpSetup(t)
	c.Step = machine.Step{Name: "review", Worker: "procedure:fill", Outputs: []string{"witnesses"}, Writable: []string{"review/**"}}
	w.after = c.Before
	res, err := a.WriteProject(context.Background(), c)
	if err != nil || res.Failure != "" {
		t.Fatalf("WriteProject = %+v, %v", res, err)
	}
	if len(w.reqs) != 1 || len(w.reqs[0].Files) != 0 {
		t.Errorf("the Writer requests = %+v, want one with no file", w.reqs)
	}
	if res.Outputs["witnesses"] != fpEmpty || !slices.Equal(res.ProjectFiles, c.Before) {
		t.Errorf("Outputs[witnesses] = %s files %v, want the empty set's hash and the files it started from", res.Outputs["witnesses"], res.ProjectFiles)
	}
}

// Edge, fail closed: nothing is written when the template cannot be read or filled, and a History
// with no Writer or a failing Writer fails the call.
func TestWriteProjectFailsClosed(t *testing.T) {
	failed := func(res durable.WorkerResult, err error) bool { return err != nil || res.Failure != "" }
	t.Run("an unreadable template artifact", func(t *testing.T) {
		a, w, _, c, _ := fpSetup(t)
		c.Template["tests"] = fpHash("not stored")
		if res, err := a.WriteProject(context.Background(), c); !failed(res, err) || len(w.reqs) != 0 {
			t.Errorf("result %+v err %v, Writer calls %d: want a failure and no write", res, err, len(w.reqs))
		}
	})
	t.Run("a template output that is not framed files", func(t *testing.T) {
		a, w, arts, c, _ := fpSetup(t)
		h, _ := arts.PutArtifact("S-1", []byte("not framed"))
		c.Template["tests"] = h
		if res, err := a.WriteProject(context.Background(), c); !failed(res, err) || len(w.reqs) != 0 {
			t.Errorf("result %+v err %v, Writer calls %d: want a failure and no write", res, err, len(w.reqs))
		}
	})
	t.Run("a fill that refuses (two files to one path)", func(t *testing.T) {
		a, w, arts, c, _ := fpSetup(t)
		two := []canonical.File{{Path: "service/note.go", Content: []byte("a")}, {Path: "service/memo.go", Content: []byte("b")}}
		h, _ := arts.PutArtifact("S-1", canonical.Framed(two))
		c.Template["tests"] = h
		if res, err := a.WriteProject(context.Background(), c); !failed(res, err) || len(w.reqs) != 0 {
			t.Errorf("result %+v err %v, Writer calls %d: want a failure and no write", res, err, len(w.reqs))
		}
	})
	t.Run("a History with no Writer", func(t *testing.T) {
		a, _, _, c, _ := fpSetup(t)
		a.History = fpPlain{}
		if res, err := a.WriteProject(context.Background(), c); !failed(res, err) {
			t.Errorf("result %+v err %v: want a failure", res, err)
		}
	})
	t.Run("a Writer that fails", func(t *testing.T) {
		a, w, _, c, _ := fpSetup(t)
		w.err = errors.New("writeStep: 409 ProjectStateDiffers")
		res, err := a.WriteProject(context.Background(), c)
		if !failed(res, err) || len(res.Outputs) != 0 {
			t.Errorf("result %+v err %v: want a failure with no outputs", res, err)
		}
	})
}
