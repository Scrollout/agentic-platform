package api

// T023 (feature 002, user story 2), part 2 of 3: operations across an engine restart. Written
// before the code (T025) by a test author; the interface, the rig and the fakes are in
// operations_durable_test.go. Judged from FR-009 to FR-011, the data model ("Operation record":
// the work and its repeat table, run operations, the outbox and sweep, Kept, Status).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/api/gen"
	slicesvc "github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
)

func TestDurableOpAfterARestartTheWorkIsDoneOnceFromItsRecordAndBody(t *testing.T) {
	x := newDopRig(t)
	x.ops.set(func(f *dopOps) { f.hold = true })
	op := x.accepted(x.do("POST", slicesURL(""), "", createBody), "propose-slice", "S-1")
	x.restart(false, false) // the workflow is still there: its work runs again on the new engine
	end := x.settle(op.ID)
	if end.ID != op.ID || end.Status != "succeeded" || end.Result["slice"] != "S-1" {
		t.Fatalf("after the restart %s reads %+v, want succeeded with slice S-1", op.ID, end)
	}
	sl, err := x.svc.Get("S-1")
	if err != nil || sl.Description != "A shopper can add an item to a basket." {
		t.Fatalf("slice S-1 = %+v (%v), want the one the kept body describes", sl, err)
	}
	if n := x.analyst.calls(); n != 1 {
		t.Errorf("the analyst was called %d times, want 1", n)
	}
	if n := len(x.svc.List()); n != 1 {
		t.Errorf("%d slices, want 1", n)
	}
	// Its end is read from the record, also after another restart with the service unreachable.
	x.restart(true, false)
	x.ops.set(func(f *dopOps) { f.down = true })
	if again := x.opView(x.getOp(op.ID), 200); again.Status != "succeeded" || again.Result["slice"] != "S-1" {
		t.Errorf("after a second restart %s reads %+v, want succeeded with slice S-1", op.ID, again)
	}
}

// Each kind's repeat after a stop finds its work done (data model, the repeat table): the work
// was done before the stop, the end was not written.
func TestDurableOpARepeatFindsItsWorkDone(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name, kind string
		seed       func(x *dopRig)
		send       func(x *dopRig) reply
		done       func(x *dopRig) // what the work did before the stop
		check      func(x *dopRig, before slicesvc.Slice)
	}{
		{"createSlice", "propose-slice", nil,
			func(x *dopRig) reply { return x.do("POST", slicesURL(""), "", createBody) },
			func(x *dopRig) {
				if _, err := x.svc.CreateAs(ctx, "S-1", "A shopper can add an item to a basket."); err != nil {
					x.t.Fatal(err)
				}
			},
			func(x *dopRig, _ slicesvc.Slice) {
				if n := x.analyst.calls(); n != 1 {
					x.t.Errorf("the analyst was called %d times, want 1 (no second call for a slice already created)", n)
				}
				if n := len(x.svc.List()); n != 1 {
					x.t.Errorf("%d slices, want 1", n)
				}
			}},
		{"deleteSlice", "delete-slice", func(x *dopRig) { x.seed(1, false) },
			func(x *dopRig) reply { return x.do("DELETE", spURL("S-1", ""), "", "") },
			func(x *dopRig) {
				if err := x.svc.Delete(ctx, "S-1"); err != nil {
					x.t.Fatal(err)
				}
			},
			func(x *dopRig, _ slicesvc.Slice) {
				if _, err := x.svc.Get("S-1"); err == nil {
					x.t.Errorf("S-1 exists after its deletion")
				}
			}},
		{"replaceGoals", "replace-goals", func(x *dopRig) { x.seed(1, false) },
			func(x *dopRig) reply {
				return x.do("PUT", spURL("S-1", "/goals"), "", x.goalsBody("Keep a note for good"))
			},
			func(x *dopRig) {
				sl, _ := x.svc.Get("S-1")
				goals := sl.Goals
				goals[0].Text = "Keep a note for good"
				if _, err := x.svc.ReplaceGoals(ctx, "S-1", goals); err != nil {
					x.t.Fatal(err)
				}
			},
			func(x *dopRig, before slicesvc.Slice) {
				after, _ := x.svc.Get("S-1")
				if after.Parts.Goals != before.Parts.Goals || len(after.History) != len(before.History) {
					x.t.Errorf("the repeat changed the slice: goals %s -> %s, history %d -> %d entries",
						before.Parts.Goals, after.Parts.Goals, len(before.History), len(after.History))
				}
			}},
		{"replaceContract", "replace-contract", func(x *dopRig) { x.seed(1, false) },
			func(x *dopRig) reply {
				sl, _ := x.svc.Get("S-1")
				return x.do("PUT", spURL("S-1", "/contract"), "", jsonOf(x.t, map[string]string{"contract": sl.Contract + "# reviewed\n"}))
			},
			func(x *dopRig) {
				sl, _ := x.svc.Get("S-1")
				if _, err := x.svc.ReplaceContract(ctx, "S-1", sl.Contract+"# reviewed\n"); err != nil {
					x.t.Fatal(err)
				}
			},
			func(x *dopRig, before slicesvc.Slice) {
				after, _ := x.svc.Get("S-1")
				if after.Parts.Contract != before.Parts.Contract || len(after.History) != len(before.History) {
					x.t.Errorf("the repeat changed the slice: contract %s -> %s, history %d -> %d entries",
						before.Parts.Contract, after.Parts.Contract, len(before.History), len(after.History))
				}
			}},
		{"approvePart", "approve-part", func(x *dopRig) { x.seed(1, false) },
			func(x *dopRig) reply {
				sl, _ := x.svc.Get("S-1")
				return x.do("POST", spURL("S-1", "/approvals"), "", approvalBody("goals", sl.Parts.Goals))
			},
			func(x *dopRig) {
				sl, _ := x.svc.Get("S-1")
				if _, err := x.svc.Approve(ctx, "S-1", slicesvc.PartGoals, sl.Parts.Goals); err != nil {
					x.t.Fatal(err)
				}
			},
			func(x *dopRig, before slicesvc.Slice) {
				after, _ := x.svc.Get("S-1")
				if len(after.Approvals) != len(before.Approvals) || len(after.History) != len(before.History) {
					x.t.Errorf("the repeat changed the slice: approvals %d -> %d, history %d -> %d entries",
						len(before.Approvals), len(after.Approvals), len(before.History), len(after.History))
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := newDopRig(t)
			if c.seed != nil {
				c.seed(x)
			}
			x.ops.set(func(f *dopOps) { f.hold = true })
			op := x.accepted(c.send(x), c.kind, "S-1")
			c.done(x)
			before, _ := x.svc.Get("S-1") // the slice as the work left it before the stop
			x.restart(false, false)
			end := x.settle(op.ID)
			if end.Status != "succeeded" || end.Result["slice"] != "S-1" {
				t.Fatalf("the repeat of %s ended %+v, want succeeded with slice S-1", c.name, end)
			}
			c.check(x, before)
		})
	}
}

func TestDurableOpARepeatedRunOperationOnlyFollowsItsRun(t *testing.T) {
	x := newDopRig(t)
	x.seed(1, true)
	x.registerAll()
	x.ops.set(func(f *dopOps) { f.hold = true })
	op := x.accepted(x.startRun(), "run", "RUN-1")
	x.recordRun()
	x.restart(false, false)
	x.registerAll()
	if p := x.opView(x.getOp(op.ID), 200); p.Status != "running" {
		t.Fatalf("after the restart %s is %q, want running while its run runs", op.ID, p.Status)
	}
	x.recordRunMore("RunCompleted")
	end := x.settle(op.ID)
	if end.Status != "succeeded" {
		t.Fatalf("operation %+v, want succeeded once its run is done", end)
	}
	if n := x.runs.startCount(); n != 1 {
		t.Errorf("the run was started %d times, want 1: a repeat only follows it", n)
	}
}

// recordRunMore appends events to RUN-1's record after recordRun wrote its start.
func (x *dopRig) recordRunMore(kinds ...string) {
	x.t.Helper()
	ev, err := x.st.Events("RUN-1")
	if err != nil {
		x.t.Fatal(err)
	}
	seq := len(ev)
	for _, k := range kinds {
		seq++
		kind, detail, _ := strings.Cut(k, "=")
		e := storedEvent{Event: machineEvent(seq, x.clock.Now(), kind, detail)}
		x.appendRunEvent(kind, e)
	}
}

func TestDurableOpARunOperationWhoseRunWasNeverStartedEndsFailed(t *testing.T) {
	x := newDopRig(t)
	x.seed(1, true)
	x.registerAll()
	x.ops.set(func(f *dopOps) { f.hold = true })
	op := x.accepted(x.startRun(), "run", "RUN-1")
	// The stop came before the run's start took effect: no workflow, no record of the run.
	x.runs.set(func(f *dopRuns) { delete(f.exists, "RUN-1") })
	x.restart(true, false)
	end := x.settle(op.ID)
	if end.Status != "failed" || end.Error == nil || end.Error.Detail != "the run was not started; send the request again" {
		t.Fatalf("operation %+v, want failed `the run was not started; send the request again`", end)
	}
}

func TestDurableOpTheOpenRunOperationAnswersResumeAndHandOverAlsoAfterARestart(t *testing.T) {
	for _, c := range []struct {
		name, kind, path string
	}{
		{"resumeRun", "RunInterrupted=the model is unavailable", "/runs/RUN-1/resume?api-version=" + apiVersion},
		{"acceptHandOver", "RunHandedOver=the step's checks failed 3 times", "/runs/RUN-1/hand-over?api-version=" + apiVersion},
	} {
		t.Run(c.name+" after startRun", func(t *testing.T) {
			x := newDopRig(t)
			x.seed(1, true)
			x.registerAll()
			x.ops.set(func(f *dopOps) { f.hold = true })
			start := x.accepted(x.startRun(), "run", "RUN-1")
			x.recordRun(c.kind)
			if got := x.accepted(x.do("POST", c.path, "", ""), "run", "RUN-1"); got.ID != start.ID {
				t.Fatalf("%s answered %s, want the open operation %s", c.name, got.ID, start.ID)
			}
			x.restart(false, true)
			x.registerAll()
			if got := x.accepted(x.do("POST", c.path, "", ""), "run", "RUN-1"); got.ID != start.ID {
				t.Fatalf("after a restart %s answered %s, want the open operation %s", c.name, got.ID, start.ID)
			}
			if r := x.records(); len(r) != 1 {
				t.Errorf("records %v, want only the start's", r)
			}
		})
	}
	t.Run("resumeRun with no open operation records one, then answers with it", func(t *testing.T) {
		x := newDopRig(t)
		x.seed(1, true)
		x.registerAll()
		x.ops.set(func(f *dopOps) { f.hold = true })
		x.recordRun("RunInterrupted=the model is unavailable")
		first := x.accepted(x.do("POST", "/runs/RUN-1/resume?api-version="+apiVersion, "resume-key", ""), "run", "RUN-1")
		if s := x.ops.startsOf(first.ID); len(s) != 1 || s[0].Kind != "resumeRun" || s[0].Target != "RUN-1" {
			t.Fatalf("workflow starts %+v, want one of kind resumeRun on RUN-1", s)
		}
		if got := x.accepted(x.do("POST", "/runs/RUN-1/resume?api-version="+apiVersion, "", ""), "run", "RUN-1"); got.ID != first.ID {
			t.Fatalf("a second resume answered %s, want the open %s", got.ID, first.ID)
		}
		x.restart(false, true)
		x.registerAll()
		// The same key after a restart is judged again: the open operation answers.
		if got := x.accepted(x.do("POST", "/runs/RUN-1/resume?api-version="+apiVersion, "resume-key", ""), "run", "RUN-1"); got.ID != first.ID {
			t.Fatalf("after a restart resume answered %s, want the open %s", got.ID, first.ID)
		}
	})
}

func TestDurableOpCreateSliceKeepsItsTargetAcrossARestart(t *testing.T) {
	x := newDopRig(t)
	x.ops.set(func(f *dopOps) { f.hold = true })
	held := x.accepted(x.do("POST", slicesURL(""), "", createBody), "propose-slice", "S-1")
	x.restart(false, true)
	// No other slice gets the held operation's target.
	x.ops.set(func(f *dopOps) { f.hold = false })
	other := x.accepted(x.do("POST", slicesURL(""), "", `{"description":"another use case"}`), "propose-slice", "S-2")
	if end := x.settle(other.ID); end.Result["slice"] != "S-2" {
		t.Fatalf("the new createSlice ended %+v, want slice S-2", end)
	}
	if _, err := x.svc.Create(context.Background(), "a third use case"); err != nil {
		t.Fatal(err)
	}
	if _, err := x.svc.Get("S-1"); err == nil {
		t.Fatal("S-1 exists before its operation's work ran: another slice took the reserved ID")
	}
	x.ops.release(held.ID)
	if end := x.settle(held.ID); end.Status != "succeeded" || end.Result["slice"] != "S-1" {
		t.Fatalf("the held createSlice ended %+v, want succeeded with slice S-1", end)
	}
}

func TestDurableOpARecordWithNoWorkflowIsStartedAtEngineStart(t *testing.T) {
	x := newDopRig(t)
	x.ops.set(func(f *dopOps) { f.hold = true })
	op := x.accepted(x.do("POST", slicesURL(""), "", createBody), "propose-slice", "S-1")
	x.restart(true, false) // the workflow was lost
	s := x.ops.startsOf(op.ID)
	if len(s) != 1 || s[0] != (OperationStart{ID: op.ID, Kind: "createSlice", Target: "S-1", TaskQueue: OperationsQueue, RejectDuplicate: true}) {
		t.Fatalf("at engine start: workflow starts %+v, want one for %s", s, op.ID)
	}
	if end := x.settle(op.ID); end.Status != "succeeded" || end.Result["slice"] != "S-1" {
		t.Fatalf("operation %+v, want succeeded with slice S-1", end)
	}
	if n := x.analyst.calls(); n != 1 {
		t.Errorf("the analyst was called %d times, want 1", n)
	}
}

func TestDurableOpARecordWhoseWorkflowClosedWithNoEndEndsFailed(t *testing.T) {
	x := newDopRig(t)
	x.seed(1, false)
	x.ops.set(func(f *dopOps) { f.hold = true })
	op := x.accepted(x.do("PUT", spURL("S-1", "/goals"), "", x.goalsBody("Keep a note for good")), "replace-goals", "S-1")
	x.ops.closeWithoutEnd(op.ID)
	x.restart(false, true)
	end := x.settle(op.ID)
	if end.Status != "failed" || end.Error == nil || end.Error.Detail != "the operation's workflow closed with no end" {
		t.Fatalf("operation %+v, want failed `the operation's workflow closed with no end`", end)
	}
}

func TestDurableOpIDsAreNeverGivenTwiceAcrossRestartsAndRemovals(t *testing.T) {
	x := newDopRig(t)
	x.seed(1, false)
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		op := x.accepted(x.do("PUT", spURL("S-1", "/goals"), "", x.goalsBody("Goal "+string(rune('a'+i)))), "replace-goals", "S-1")
		x.settle(op.ID)
		seen[op.ID] = true
	}
	// Past the retention every record is removed; the sequence goes on.
	x.clock.Advance(time.Duration(contractLimit(t, "idempotency-key-retention-hours"))*time.Hour + time.Hour)
	x.restart(true, false)
	for i := 0; i < 2; i++ {
		op := x.accepted(x.do("PUT", spURL("S-1", "/goals"), "", x.goalsBody("Later goal "+string(rune('a'+i)))), "replace-goals", "S-1")
		if seen[op.ID] {
			t.Fatalf("operation ID %s was given out again (seen %v)", op.ID, seen)
		}
		seen[op.ID] = true
		x.settle(op.ID)
	}
	x.getOp("OP-999").problem(t, 404, gen.ErrorCodeNotFound)
}

func TestDurableOpIsFollowedUntil24hAfterItsEndThen404AndItsKeyIsFree(t *testing.T) {
	x := newDopRig(t)
	x.seed(1, false)
	body := x.goalsBody("Keep a note for good")
	op := x.accepted(x.do("PUT", spURL("S-1", "/goals"), "keep-key", body), "replace-goals", "S-1")
	x.settle(op.ID)
	endedAt, err := time.Parse(time.RFC3339Nano, x.record(op.ID)["ended_at"].(string))
	if err != nil {
		t.Fatalf("ended_at: %v", err)
	}
	keep := time.Duration(contractLimit(t, "idempotency-key-retention-hours")) * time.Hour
	x.clock.Advance(endedAt.Add(keep).Sub(x.clock.Now()))
	get := func() reply {
		return x.r.do("GET", "/operations/"+op.ID+"?api-version="+apiVersion, x.client, "", nil)
	}
	if p := get(); p.Code != 200 {
		t.Fatalf("exactly %s after its end getOperation = %d, want 200: %s", keep, p.Code, p.Body.String())
	}
	x.clock.Advance(time.Second)
	get().problem(t, 404, gen.ErrorCodeNotFound)
	// The key is no longer held: the same key and body make a new operation.
	again := x.opView(x.r.do("PUT", spURL("S-1", "/goals"), x.client, body, map[string]string{"Idempotency-Key": "keep-key"}), 202)
	if again.ID == op.ID {
		t.Fatalf("the key still answered %s after its retention", op.ID)
	}
}

func (x *dopRig) appendRunEvent(kind string, e storedEvent) {
	x.t.Helper()
	data, err := json.Marshal(e)
	if err != nil {
		x.t.Fatal(err)
	}
	if _, err := x.st.Append("S-1", store.Event{Run: "RUN-1", Kind: kind, Data: store.RawJSON(data)}); err != nil {
		x.t.Fatal(err)
	}
}
