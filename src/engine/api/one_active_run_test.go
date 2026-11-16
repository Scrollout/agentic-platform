package api

// T095 (FR-021, research D38, data-model "Active run"; the contract wording is T091's): at most one
// active run on the platform. startRun, resumeRun and replayRun are refused with 409 RunActive
// while any run, of any slice, has not ended (running, interrupted or waiting for the developer),
// naming that run and its slice; the slice-editing operations keep their per-slice refusal.
// Written before the code by a separate author, from the contract, D38 and the data model.
//
// What these tests assume where the sources leave it open:
//   - "naming that run and its slice" (D38): the problem's `detail` contains the active run's ID and
//     its slice's ID. The exact wording is not pinned.
//   - A refused start creates no operation (the next operation ID does not exist) and no run (the
//     run list does not grow), as the existing per-slice RunActive test pins for one slice.
//   - "each slice's machine expiring its due runs first" (T095): a run of S-1 that has waited past
//     14 days is ended `expired` by S-2's start itself, before S-2's run is recorded, so S-1's
//     `ended_at` is not after S-2's run's `created_at`. The rig's clock moves only when a request is
//     made (80 ms each), so a later read that expired it would give a later `ended_at`.
//   - resumeRun's refusal needs an interrupted run of S-2 and an active run of S-1 at once, which
//     the one-run rule itself no longer lets the API make. The record is built as an engine that
//     did not see the other run would have written it (the S-1 run is started by an engine over
//     the store with S-2's run hidden, as TestRestartedEngineRebuildsAReusedOpenRound hides a run),
//     then a restarted engine over the whole record is asked to resume S-2's run.
//   - S-2's proposal differs from S-1's in one rule's response (and one goal's text). The pattern's
//     steps take `slice/rules` and `slice/contract`, not `slice/goals`, so the rule is what makes
//     every reuse key differ: no result accepted for one slice's run is reused by the other's, and a
//     gated worker really holds the run that is started second.
//   - The slice edits checked are replaceGoals, replaceContract, approvePart and deleteSlice on S-2
//     (all list RunActive in the contract): each answers 202 and its operation succeeds.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
)

// newTwoSliceRig is a runs rig with two approved slices, S-1 and S-2, every declared step of each
// with a registered worker.
func newTwoSliceRig(t *testing.T) *runsRig {
	t.Helper()
	st, err := store.Open(t.TempDir(), spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	other := spNotes(t)
	other.Goals[0].Text = "Keep a note for another use"
	other.Rules[0].Response = "shall store the note durably" // slice/rules is a step input: no shared result
	sp := newSP(t, []spResult{{p: spNotes(t)}, {p: other}}, st)
	sp.approveAfterSeed(t)                // S-1
	sp.seed(t, "Let me keep more notes.") // S-2
	sp.approveAll(t, "S-2")
	x := &runsRig{t: t, sp: sp, st: st, projects: t.TempDir(),
		w: &fakeStepWorker{st: st, data: map[string][]byte{}}, c: &fakeStepChecker{}}
	x.bootBoth(st)
	return x
}

// bootBoth makes a handler over the slices service and st with the workers of both slices
// registered, as an engine (re)start does.
func (x *runsRig) bootBoth(st SliceStore) {
	x.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	x.r = newRig(x.t, func(cfg *Config) {
		cfg.Context = ctx
		cfg.Slices, cfg.Store = x.sp.svc, st
		cfg.WorkerGrants = mergeGrants(stepGrants("S-1"), stepGrants("S-2"))
		cfg.Projects = x.projects
		cfg.Model = "test-model"
		cfg.StepWorker, cfg.StepChecker = x.w, x.c
	})
	stopEngineAtEnd(x.t, x.r, cancel)
	registerAll(x.t, x.r, "S-1")
	registerAll(x.t, x.r, "S-2")
}

func (x *runsRig) startOf(slice, project string) reply {
	return x.do("POST", spURL(slice, "/runs"), `{"project":"`+project+`"}`)
}

// failEveryAttempt makes every attempt of the worker fail, so a run ends up interrupted.
func (x *runsRig) failEveryAttempt() {
	x.w.set(func(context.Context, machine.Request) (machine.Result, error) {
		return machine.Result{}, errors.New("the model is unavailable")
	})
}

// waitWorkerCalled waits until the worker has been called more than before times: the run started
// after the count was taken really runs a step instead of reusing every result.
func (x *runsRig) waitWorkerCalled(before int) {
	x.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for x.w.callCount() <= before {
		if time.Now().After(deadline) {
			x.t.Fatalf("setup: the step worker was not called; the run reused every result")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// wantRunActiveNaming checks a 409 RunActive whose detail names the active run and its slice.
func wantRunActiveNaming(t *testing.T, p reply, run, slice string) {
	t.Helper()
	pr := p.problem(t, 409, "RunActive")
	if pr.Detail == nil || !strings.Contains(*pr.Detail, run) || !strings.Contains(*pr.Detail, slice) {
		var d string
		if pr.Detail != nil {
			d = *pr.Detail
		}
		t.Errorf("RunActive detail = %q, want it to name run %s and slice %s", d, run, slice)
	}
}

func TestStartIsRefusedWhileARunOfAnotherSliceIsActive(t *testing.T) {
	for _, tc := range []struct {
		state string
		make  func(x *runsRig) (end func())
	}{
		{"running", func(x *runsRig) func() {
			release := x.gatedWorker()
			x.waitStatus(x.opOf(x.startOf("S-1", "notes-app"), "run").Target, "running")
			return func() { release(); x.waitEnded("RUN-1", "done") }
		}},
		{"interrupted", func(x *runsRig) func() {
			x.failEveryAttempt()
			x.waitStatus(x.opOf(x.startOf("S-1", "notes-app"), "run").Target, "interrupted")
			return func() {
				x.w.set(nil)
				x.opOf(x.do("POST", apiPath("/runs/RUN-1/resume"), ""), "run")
				x.waitEnded("RUN-1", "done")
			}
		}},
		{"waiting-for-developer", func(x *runsRig) func() {
			x.failStep("tests")
			x.waitStatus(x.opOf(x.startOf("S-1", "notes-app"), "run").Target, "waiting-for-developer")
			return func() {
				x.opOf(x.do("POST", apiPath("/runs/RUN-1/hand-over"), ""), "run")
				x.waitEnded("RUN-1", "handed-over")
				x.c.set(nil)
			}
		}},
	} {
		t.Run(tc.state, func(t *testing.T) {
			x := newTwoSliceRig(t)
			end := tc.make(x)

			wantRunActiveNaming(t, x.startOf("S-2", "other-app"), "RUN-1", "S-1")
			x.noMoreOperations("OP-2") // a refusal creates no operation
			if got := x.listRuns("").Items; len(got) != 1 || got[0].ID != "RUN-1" || got[0].Slice != "S-1" {
				t.Errorf("runs after the refused start = %+v, want S-1's RUN-1 alone", got)
			}
			x.do("GET", apiPath("/runs/RUN-2"), "").problem(t, 404, "NotFound")

			end()
			op := x.opOf(x.startOf("S-2", "other-app"), "run")
			if v := x.getRun(op.Target); v.Slice != "S-2" || v.Project != "other-app" {
				t.Errorf("S-2's start once S-1's run has ended = %+v, want a run of S-2 into other-app", v)
			}
			x.waitEnded(op.Target, "done")
		})
	}
}

func TestResumeAndReplayAreRefusedWhileARunOfAnotherSliceIsActive(t *testing.T) {
	t.Run("replay", func(t *testing.T) {
		x := newTwoSliceRig(t)
		two := x.opOf(x.startOf("S-2", "two-app"), "run") // RUN-1, of S-2
		x.waitEnded(two.Target, "done")
		release := x.gatedWorker()
		before := x.w.callCount()
		one := x.opOf(x.startOf("S-1", "notes-app"), "run") // RUN-2, of S-1
		x.waitWorkerCalled(before)
		x.waitStatus(one.Target, "running")

		wantRunActiveNaming(t, x.do("POST", apiPath("/runs/"+two.Target+"/replay"), `{"project":"again-app"}`), one.Target, "S-1")
		x.noMoreOperations("OP-3")
		if n := len(x.listRuns("").Items); n != 2 {
			t.Errorf("%d runs after the refused replay, want 2", n)
		}

		release()
		x.waitEnded(one.Target, "done")
		rop := x.opOf(x.do("POST", apiPath("/runs/"+two.Target+"/replay"), `{"project":"again-app"}`), "run")
		if v := x.waitEnded(rop.Target, "done"); v.Slice != "S-2" || v.ReplayOf == nil || *v.ReplayOf != two.Target {
			t.Errorf("the replay once S-1's run ended = %+v, want a run of S-2 replaying %s", v, two.Target)
		}
	})

	t.Run("resume", func(t *testing.T) {
		x := newTwoSliceRig(t)
		x.failEveryAttempt()
		two := x.opOf(x.startOf("S-2", "two-app"), "run") // RUN-1, of S-2
		x.waitStatus(two.Target, "interrupted")

		// An engine that does not see RUN-1 starts S-1's run, which then waits for the developer.
		x.w.set(nil)
		x.failStep("tests")
		x.bootBoth(hiddenRun{x.st, two.Target})
		one := x.opOf(x.startOf("S-1", "notes-app"), "run")
		if one.Target == two.Target {
			t.Fatalf("setup: S-1's run reused the ID %s", one.Target)
		}
		x.waitStatus(one.Target, "waiting-for-developer")

		// A restarted engine over the whole record: S-2's run is interrupted, S-1's is waiting.
		x.c.set(nil)
		x.bootBoth(x.st)
		if v := x.getRun(two.Target); v.Status != "interrupted" {
			t.Fatalf("setup: %s is %q after the restart, want interrupted", two.Target, v.Status)
		}
		if v := x.getRun(one.Target); v.Status != "waiting-for-developer" {
			t.Fatalf("setup: %s is %q after the restart, want waiting-for-developer", one.Target, v.Status)
		}
		wantRunActiveNaming(t, x.do("POST", apiPath("/runs/"+two.Target+"/resume"), ""), one.Target, "S-1")
		x.noMoreOperations("OP-1")
		if v := x.getRun(two.Target); v.Status != "interrupted" {
			t.Errorf("a refused resume changed %s: %+v", two.Target, v)
		}

		// Once S-1's run has ended, S-2's run is the only one not ended, and its resume is accepted.
		x.opOf(x.do("POST", apiPath("/runs/"+one.Target+"/hand-over"), ""), "run")
		x.waitEnded(one.Target, "handed-over")
		rop := x.opOf(x.do("POST", apiPath("/runs/"+two.Target+"/resume"), ""), "run")
		if rop.Target != two.Target {
			t.Errorf("resume operation target = %q, want %q", rop.Target, two.Target)
		}
		x.waitEnded(two.Target, "done")
	})
}

func TestARunOfAnotherSliceDueToExpireIsExpiredFirst(t *testing.T) {
	x := newTwoSliceRig(t)
	x.failStep("tests")
	one := x.opOf(x.startOf("S-1", "notes-app"), "run")
	x.waitStatus(one.Target, "waiting-for-developer")
	x.c.set(nil)

	x.r.clock.Advance(14*24*time.Hour + time.Hour) // S-1's run has waited past 14 days
	op := x.opOf(x.startOf("S-2", "other-app"), "run")

	// Read S-2's run first: a read of S-1's run would itself expire it.
	two := x.getRun(op.Target)
	if two.Slice != "S-2" {
		t.Errorf("the accepted start = %+v, want a run of S-2", two)
	}
	v := x.getRun(one.Target)
	if v.Status != "ended" || v.Outcome == nil || *v.Outcome != "expired" || v.EndedAt == nil {
		t.Fatalf("S-1's run after S-2's start = %+v, want ended expired", v)
	}
	ended, err1 := time.Parse(time.RFC3339Nano, *v.EndedAt)
	created, err2 := time.Parse(time.RFC3339Nano, two.CreatedAt)
	if err1 != nil || err2 != nil {
		t.Fatalf("timestamps: %v, %v", err1, err2)
	}
	if ended.After(created) {
		t.Errorf("S-1's run ended at %s, after S-2's run was created at %s: S-2's start did not expire it first", ended, created)
	}
	evs := x.allEvents(one.Target)
	if last := evs[len(evs)-1].Kind; last != "RunExpired" {
		t.Errorf("S-1's last event = %q, want RunExpired", last)
	}
	x.waitEnded(op.Target, "done")
}

func TestSliceEditsStayPerSlice(t *testing.T) {
	x := newTwoSliceRig(t)
	release := x.gatedWorker()
	run := x.opOf(x.startOf("S-1", "notes-app"), "run")
	x.waitWorkerCalled(0)
	x.waitStatus(run.Target, "running")

	edit := func(what, method, path, body string) {
		t.Helper()
		p := x.do(method, path, body)
		if p.Code != 202 {
			t.Fatalf("%s of S-2 while S-1's run is active = %d, want 202: %s", what, p.Code, p.Body.String())
		}
		op := x.waitOp(p.operation(t, 202).Id)
		if op.Status != "succeeded" {
			t.Fatalf("%s of S-2 while S-1's run is active: operation %+v, want succeeded", what, op)
		}
	}
	sl, err := x.sp.svc.Get("S-2")
	if err != nil {
		t.Fatal(err)
	}
	goals := sl.Goals
	goals[0].Text = "Keep a note while another slice builds"
	edit("replaceGoals", "PUT", spURL("S-2", "/goals"), jsonOf(t, map[string]any{"goals": goals}))
	edit("replaceContract", "PUT", spURL("S-2", "/contract"), jsonOf(t, map[string]string{"contract": sl.Contract + "# reviewed\n"}))
	if sl, err = x.sp.svc.Get("S-2"); err != nil {
		t.Fatal(err)
	}
	edit("approvePart", "POST", spURL("S-2", "/approvals"), approvalBody(string(slices.PartContract), sl.Parts.Contract))
	edit("deleteSlice", "DELETE", spURL("S-2", ""), "")
	if _, err := x.sp.svc.Get("S-2"); err == nil {
		t.Errorf("S-2 is still there after its deleteSlice succeeded")
	}

	// S-1's own run is untouched and S-1's own edits are still refused.
	if v := x.getRun(run.Target); v.Status != "running" {
		t.Errorf("S-1's run is %q after S-2's edits, want running", v.Status)
	}
	x.do("DELETE", spURL("S-1", ""), "").problem(t, 409, "RunActive")
	release()
	x.waitEnded(run.Target, "done")
}
