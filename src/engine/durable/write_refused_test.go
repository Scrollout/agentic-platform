package durable_test

// Feature 011, T040: a `fill` step's write that a resume cannot fix hands the run over; one a
// repeat can pass interrupts it. Judged from T040 and the owner's decision of 2026-11-26 under it,
// FR-009, and the seam's doc comments (machine.ErrWriteRefused, durable.WorkerResult.Refused).
// Nothing here changes an existing test, helper or fixture: it calls fpInput, fpPattern, fpHash,
// fpSetup and the fp* fakes of fill_proof_test.go. newFP's WriteProject fake can only fail with
// Failure set and never Refused, and a second registration of the same activity name is not
// possible, so the workflow cases register their own small rig (wrRun) in this file.
//
// Left out, because the task does not settle them: the run's time budget running out during a
// refused write; Captured.Parameters returning an error and the template's storing failing
// (PutArtifact) in WriteProject; a Writer's refusal arriving after the activity's own context
// ended.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

const (
	wrRefusal = "the write was refused: HTTP 422 StepFilesTooLarge"
	wrPlain   = "writeStep: HTTP 503 Busy"
)

// wrRun is a run in Temporal's test environment whose WriteProject answers per step: "refused"
// (Failure and Refused), "failed" (Failure only) or "" (the write is made). mode is read at each
// call, so a callback can change it between a hand-over and the resume.
type wrRun struct {
	t   *testing.T
	env *testsuite.TestWorkflowEnvironment
	in  durable.RunInput

	mu      sync.Mutex
	mode    map[string]string // step -> "refused", "failed" or ""
	log     []string
	writes  []string // step of each WriteProject call
	checks  []string // "step/check"
	commits []string
	events  []machine.Event
}

func newWR(t *testing.T) *wrRun {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	suite.SetLogger(log.NewStructuredLogger(slog.New(slog.DiscardHandler)))
	f := &wrRun{t: t, env: suite.NewTestWorkflowEnvironment(), in: fpInput(), mode: map[string]string{}}
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
		f.writes = append(f.writes, c.Step.Name)
		mode := f.mode[c.Step.Name]
		f.mu.Unlock()
		switch mode {
		case "refused":
			return durable.WorkerResult{Failure: wrRefusal, Refused: true, Took: time.Second}, nil
		case "failed":
			return durable.WorkerResult{Failure: wrPlain, Took: time.Second}, nil
		}
		out := map[string]string{}
		for _, o := range c.Step.Outputs {
			out[o] = c.Template[o]
		}
		files := append(slices.Clone(c.Before), machine.File{Path: "filled/" + c.Step.Name + ".go", SHA256: fpHash("filled/" + c.Step.Name)})
		return durable.WorkerResult{Outputs: out, Before: c.Before, ProjectFiles: files, Took: time.Second}, nil
	})
	reg(durable.RunCheck, func(_ context.Context, c durable.CheckCall) (durable.CheckResult, error) {
		add("check %s %s", c.Step.Name, c.Name)
		f.mu.Lock()
		f.checks = append(f.checks, c.Step.Name+"/"+c.Name)
		f.mu.Unlock()
		return durable.CheckResult{Passed: true, Reason: c.Name + " holds"}, nil
	})
	reg(durable.RestoreProject, func(_ context.Context, r machine.RestoreRequest) (durable.Ran, error) {
		add("restore %s", r.Step.Name)
		return durable.Ran{}, nil
	})
	reg(durable.CommitStep, func(_ context.Context, r machine.CommitRequest) (durable.Ran, error) {
		add("commit %s", r.Step.Name)
		f.mu.Lock()
		f.commits = append(f.commits, r.Step.Name)
		f.mu.Unlock()
		return durable.Ran{}, nil
	})
	reg(durable.LookupReuse, func(context.Context, durable.ReuseLookup) (durable.Reused, error) { return durable.Reused{}, nil })
	reg(durable.PutResult, func(context.Context, durable.ReusePut) error { return nil })
	reg(durable.RecordEvent, func(_ context.Context, r durable.EventRecord) error {
		add("record %s %s", r.Event.Kind, r.Event.Step)
		f.mu.Lock()
		f.events = append(f.events, r.Event)
		f.mu.Unlock()
		return nil
	})
	return f
}

// signalAt sends the signal after d of the test environment's clock and runs fn first.
func (f *wrRun) signalAt(d time.Duration, signal string, fn func()) {
	f.env.RegisterDelayedCallback(func() {
		if fn != nil {
			f.mu.Lock()
			fn()
			f.mu.Unlock()
		}
		f.env.SignalWorkflow(signal, nil)
	}, d)
}

// run executes the workflow; a run nobody answers is cancelled after 60 days.
func (f *wrRun) run() durable.RunResult {
	f.t.Helper()
	f.env.RegisterDelayedCallback(func() { f.env.SignalWorkflow(durable.SignalCancel, nil) }, 60*24*time.Hour)
	f.env.ExecuteWorkflow("Run", f.in)
	if !f.env.IsWorkflowCompleted() {
		f.t.Fatal("the workflow did not complete")
	}
	if err := f.env.GetWorkflowError(); err != nil {
		f.t.Fatalf("workflow error: %v", err)
	}
	var r durable.RunResult
	if err := f.env.GetWorkflowResult(&r); err != nil {
		f.t.Fatal(err)
	}
	return r
}

func (f *wrRun) count(kind string) (n int) {
	for _, e := range f.events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func (f *wrRun) details(kind string) (out []string) {
	for _, e := range f.events {
		if e.Kind == kind {
			out = append(out, e.Detail)
		}
	}
	return out
}

func (f *wrRun) wrote(step string) (n int) {
	for _, s := range f.writes {
		if s == step {
			n++
		}
	}
	return n
}

func (f *wrRun) checked(step string) bool {
	return slices.ContainsFunc(f.checks, func(c string) bool { return strings.HasPrefix(c, step+"/") })
}

func (f *wrRun) accepted(step string) bool {
	return slices.ContainsFunc(f.events, func(e machine.Event) bool { return e.Kind == "StepAccepted" && e.Step == step })
}

// wrHandedOver asserts the shared clauses of a refused write at step: handed over, naming the
// step and the reason, never interrupted; written once, not checked, not committed, not accepted.
func wrHandedOver(t *testing.T, f *wrRun, step string, writes int) {
	t.Helper()
	if n := f.count("RunHandedOver"); n != 1 {
		t.Fatalf("RunHandedOver events = %d, want 1; log %v", n, f.log)
	}
	if n := f.count("RunInterrupted"); n != 0 {
		t.Errorf("RunInterrupted events = %d, want none: a refusal a resume cannot fix hands over; details %v", n, f.details("RunInterrupted"))
	}
	over := f.details("RunHandedOver")[0]
	if !strings.Contains(over, step) || !strings.Contains(over, wrRefusal) {
		t.Errorf("the hand-over %q does not name step %s and the reason %q", over, step, wrRefusal)
	}
	if n := f.wrote(step); n != writes {
		t.Errorf("WriteProject for %s was called %d times, want %d", step, n, writes)
	}
	if f.checked(step) || slices.Contains(f.commits, step) || f.accepted(step) {
		t.Errorf("a refused write was checked, committed or accepted: %v", f.log)
	}
}

// T040 clause: a refused `fill` write (Refused set) ends the run handed over, not interrupted
// (then expired), after one write, with no check, commit or acceptance of the step. One row per
// fill step, so a refusal on a later step, after earlier ones were accepted, is a case too.
func TestARefusedFillWriteHandsTheRunOver(t *testing.T) {
	for _, step := range []string{"tests", "code", "review"} {
		t.Run(step, func(t *testing.T) {
			f := newWR(t)
			f.mode[step] = "refused"
			f.signalAt(24*time.Hour, durable.SignalAcceptHandOver, nil)
			r := f.run()
			if r.Outcome != machine.OutcomeHandedOver {
				t.Errorf("the run ended %q, want %q; log %v", r.Outcome, machine.OutcomeHandedOver, f.log)
			}
			wrHandedOver(t, f, step, 1)
			for _, later := range []string{"tests", "code", "review"}[slices.Index([]string{"tests", "code", "review"}, step)+1:] {
				if f.wrote(later) != 0 {
					t.Errorf("step %s after the refused one was written", later)
				}
			}
			for _, earlier := range []string{"tests", "code", "review"}[:slices.Index([]string{"tests", "code", "review"}, step)] {
				if !f.accepted(earlier) || !slices.Contains(f.commits, earlier) {
					t.Errorf("step %s before the refused one was not accepted and committed: %v", earlier, f.log)
				}
			}
		})
	}
}

// T040 clause: the run left alone after the hand-over expires (the hand-over TTL), still never
// interrupted.
func TestARefusedFillWriteLeftUnansweredExpiresAfterAHandOver(t *testing.T) {
	f := newWR(t)
	f.mode["tests"] = "refused"
	r := f.run()
	if r.Outcome != machine.OutcomeExpired {
		t.Errorf("the run ended %q, want %q", r.Outcome, machine.OutcomeExpired)
	}
	wrHandedOver(t, f, "tests", 1)
}

// Regression guard (passes now): a write failure that is not Refused (what a repeat can pass)
// still interrupts the run: no hand-over, one write, no check, no commit.
func TestAFailedFillWriteThatIsNotRefusedStillInterruptsTheRun(t *testing.T) {
	f := newWR(t)
	f.mode["tests"] = "failed"
	f.run()
	if n := f.count("RunInterrupted"); n != 1 {
		t.Fatalf("RunInterrupted events = %d, want 1; log %v", n, f.log)
	}
	if n := f.count("RunHandedOver"); n != 0 {
		t.Errorf("RunHandedOver events = %d, want none for a write a repeat can pass", n)
	}
	if intr := f.details("RunInterrupted")[0]; !strings.Contains(intr, "tests") || !strings.Contains(intr, wrPlain) {
		t.Errorf("the interruption %q does not name step tests and the reason %q", intr, wrPlain)
	}
	if f.wrote("tests") != 1 || f.checked("tests") || slices.Contains(f.commits, "tests") || f.accepted("tests") {
		t.Errorf("write %d, checked %v, committed, accepted: log %v", f.wrote("tests"), f.checked("tests"), f.log)
	}
}

// T040 clause: a resume of the handed-over run attempts the write again (the developer fixed the
// cause): the run records RunResumed, writes the step again, and goes on to the end.
func TestAResumeOfARunHandedOverByARefusedWriteWritesTheStepAgain(t *testing.T) {
	f := newWR(t)
	f.mode["code"] = "refused"
	f.signalAt(24*time.Hour, durable.SignalResume, func() { f.mode["code"] = "" })
	r := f.run()
	if r.Outcome != machine.OutcomeDone {
		t.Fatalf("the run ended %q, want done after the resume; log %v", r.Outcome, f.log)
	}
	if f.count("RunHandedOver") != 1 || f.count("RunResumed") != 1 || f.count("RunInterrupted") != 0 {
		t.Errorf("hand-overs %d, resumes %d, interruptions %d, want 1, 1, 0; log %v",
			f.count("RunHandedOver"), f.count("RunResumed"), f.count("RunInterrupted"), f.log)
	}
	if n := f.wrote("code"); n != 2 {
		t.Errorf("WriteProject for code was called %d times, want 2 (the refused one and the resume's)", n)
	}
	if n := f.wrote("tests"); n != 1 {
		t.Errorf("WriteProject for tests was called %d times, want once: the resume continues from the refused step", n)
	}
	if !f.accepted("code") || !slices.Contains(f.commits, "code") {
		t.Errorf("code was not accepted and committed after the resume: %v", f.log)
	}
}

// T040 edge: a refusal again at the resume hands the run over again (two hand-overs, no
// interruption), the write attempted once more each time.
func TestARefusalAtAResumeHandsTheRunOverAgain(t *testing.T) {
	f := newWR(t)
	f.mode["tests"] = "refused"
	f.signalAt(24*time.Hour, durable.SignalResume, nil)
	f.signalAt(48*time.Hour, durable.SignalAcceptHandOver, nil)
	r := f.run()
	if r.Outcome != machine.OutcomeHandedOver {
		t.Errorf("the run ended %q, want handed over; log %v", r.Outcome, f.log)
	}
	if f.count("RunHandedOver") != 2 || f.count("RunInterrupted") != 0 {
		t.Errorf("hand-overs %d, interruptions %d, want 2 and 0; log %v", f.count("RunHandedOver"), f.count("RunInterrupted"), f.log)
	}
	if n := f.wrote("tests"); n != 2 {
		t.Errorf("WriteProject for tests was called %d times, want 2", n)
	}
}

// ---- WriteProject's own refusals (activities level) ----

type wrWrapped struct{ error }

func (e wrWrapped) Unwrap() error { return e.error }

// wrRefused asserts a result that WriteProject gives for a refusal a repeat cannot pass: Failure
// set naming the reason, Refused, no activity error, no outputs, and nothing written.
func wrRefused(t *testing.T, w *fpWriter, res durable.WorkerResult, err error, reason string) {
	t.Helper()
	if err != nil {
		t.Errorf("WriteProject gave the activity error %v, want a result with Failure and Refused set", err)
	}
	if res.Failure == "" || !res.Refused {
		t.Errorf("result Failure %q Refused %v, want Failure set and Refused true", res.Failure, res.Refused)
	}
	if reason != "" && !strings.Contains(res.Failure, reason) {
		t.Errorf("Failure %q does not carry the reason %q", res.Failure, reason)
	}
	if len(res.Outputs) != 0 || res.Model != "" || res.Usage != (machine.Usage{}) {
		t.Errorf("a failed result holds more than Failure: %+v", res)
	}
	_ = w
}

// T040 clause: WriteProject's own refusals (an unreadable template artifact, a template that is
// not a set of files, a fill captured.Fill refuses, two outputs filling one path) are a result
// with Failure and Refused set, not an activity error, and nothing is written.
func TestWriteProjectRefusesWhatARepeatCannotPassAsARefusedResult(t *testing.T) {
	t.Run("an unreadable template artifact", func(t *testing.T) {
		a, w, _, c, _ := fpSetup(t)
		c.Template["tests"] = fpHash("not stored")
		res, err := a.WriteProject(context.Background(), c)
		wrRefused(t, w, res, err, "")
		if len(w.reqs) != 0 {
			t.Errorf("the Writer was called %d times, want none", len(w.reqs))
		}
	})
	t.Run("a template output that is not framed files", func(t *testing.T) {
		a, w, arts, c, _ := fpSetup(t)
		h, _ := arts.PutArtifact("S-1", []byte("not framed"))
		c.Template["tests"] = h
		res, err := a.WriteProject(context.Background(), c)
		wrRefused(t, w, res, err, "")
		if len(w.reqs) != 0 {
			t.Errorf("the Writer was called %d times, want none", len(w.reqs))
		}
	})
	t.Run("a fill that captured.Fill refuses (two files to one path)", func(t *testing.T) {
		a, w, arts, c, _ := fpSetup(t)
		two := []canonical.File{{Path: "service/note.go", Content: []byte("a")}, {Path: "service/memo.go", Content: []byte("b")}}
		h, _ := arts.PutArtifact("S-1", canonical.Framed(two))
		c.Template["tests"] = h
		res, err := a.WriteProject(context.Background(), c)
		wrRefused(t, w, res, err, "")
		if len(w.reqs) != 0 {
			t.Errorf("the Writer was called %d times, want none", len(w.reqs))
		}
	})
	t.Run("two outputs filling one path", func(t *testing.T) {
		a, w, arts, c, src := fpSetup(t)
		h, _ := arts.PutArtifact("S-1", canonical.Framed(src)) // the same template path as `tests`
		c.Step.Outputs = []string{"tests", "extra"}
		c.Template["extra"] = h
		res, err := a.WriteProject(context.Background(), c)
		wrRefused(t, w, res, err, "")
		if len(w.reqs) != 0 {
			t.Errorf("the Writer was called %d times, want none", len(w.reqs))
		}
	})
	t.Run("a Writer error wrapping machine.ErrWriteRefused", func(t *testing.T) {
		a, w, _, c, _ := fpSetup(t)
		w.err = fmt.Errorf("writeStep: HTTP 422 StepFilesTooLarge: %w", machine.ErrWriteRefused)
		res, err := a.WriteProject(context.Background(), c)
		wrRefused(t, w, res, err, "StepFilesTooLarge")
		if len(w.reqs) != 1 {
			t.Errorf("the Writer was called %d times, want once", len(w.reqs))
		}
	})
	t.Run("a Writer error wrapping it deeper in the chain", func(t *testing.T) {
		a, w, _, c, _ := fpSetup(t)
		w.err = wrWrapped{fmt.Errorf("history: %w", machine.ErrWriteRefused)}
		res, err := a.WriteProject(context.Background(), c)
		wrRefused(t, w, res, err, "")
	})
}

// T040 clause: a Writer error that does not wrap ErrWriteRefused is a result with Failure set and
// Refused false (the run interrupts). It passes now, so it is a regression guard.
func TestWriteProjectGivesAPlainWriteErrorAsAFailureThatIsNotRefused(t *testing.T) {
	a, w, _, c, _ := fpSetup(t)
	w.err = errors.New("writeStep: HTTP 503 Busy")
	res, err := a.WriteProject(context.Background(), c)
	if err != nil {
		t.Fatalf("WriteProject gave the activity error %v, want a result with Failure set", err)
	}
	if res.Failure == "" || res.Refused {
		t.Errorf("result Failure %q Refused %v, want Failure set and Refused false", res.Failure, res.Refused)
	}
}

// T040 clause: a History that is no Writer, or no captured patterns known, is an engine defect
// and stays an activity error (the run interrupts): an error, and no Refused result. Regression
// guards (pass now).
func TestWriteProjectWithNoWriterOrNoCapturedPatternsStaysAnActivityError(t *testing.T) {
	t.Run("a History that is no Writer", func(t *testing.T) {
		a, _, _, c, _ := fpSetup(t)
		a.History = fpPlain{}
		res, err := a.WriteProject(context.Background(), c)
		if err == nil || res.Refused {
			t.Errorf("result %+v err %v, want an activity error and no Refused", res, err)
		}
	})
	t.Run("no captured patterns known", func(t *testing.T) {
		a, _, _, c, _ := fpSetup(t)
		a.Captured = nil
		res, err := a.WriteProject(context.Background(), c)
		if err == nil || res.Refused {
			t.Errorf("result %+v err %v, want an activity error and no Refused", res, err)
		}
	})
}
