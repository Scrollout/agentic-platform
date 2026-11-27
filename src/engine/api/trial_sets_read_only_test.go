package api

// T036 of feature 012 (owner 2026-11-28; spec FR-007, SC-005; data model "States" rules 1-4; ADR 0106):
// a read (`getTrialSet`, `listTrialSets`) writes nothing. For a set whose record is `running` but
// whose operation ended `failed` (the crash window), a read SHOWS `failed`, with `failed_because` (the
// operation's detail, or its code when the detail is empty, missing or blank) and `ended_at` (the
// operation's end time, not the time of the read). The repair, writing the record `failed` and
// cancelling the unended last trial until its record ends, runs on `startTrialSet`, `deleteSlice` and
// the engine's start. A failed operation is final: what a read showed before the repair is what the
// repaired record holds and what every later read shows. Written before the code by a test author
// separate from the code author; the rigs and helpers of operations_durable_test.go,
// trial_sets_failed_test.go and trial_sets_failed_reason_test.go are used as they are.
//
// Edge cases checked: getTrialSet and listTrialSets alike; detail given, empty, missing from the
// problem and blank, each showing the reason the contract wants; ended_at is the operation's end, set
// apart from the read's time by advancing the clock; the whole store tree is byte-identical after
// reads (no record, no operation, no run record written); no cancel reaches the durable service and
// the trial's run is not ended by a read; the engine's start repairs with no read at all: the record
// is `failed` and the trial cancelled; the cancel that cannot land because the service is down leaves
// the record `failed` and the trial not cancelled, and the next engine start once the service is back
// cancels it; a trial whose record has not ended is cancelled again at each engine start; nothing is
// cancelled and no byte changes at an engine start once its record has ended; a read after the repair
// shows the same `failed_because` and `ended_at` as the read before it, the record holds those same
// values, no byte changes and no second cancel; the result counts the cancelled trial ended (and not
// done) only after its record ends, never at the read of the crash window.
//
// Not planted here, covered elsewhere: a set that is not `running` (never changed by its operation's
// failure, trial_sets_failed_test.go), the repair at startTrialSet and deleteSlice (the same file).

import (
	"encoding/json"
	"testing"
	"time"
)

// ropProblems are the failed operation's problems a crash can leave, with the reason a set shows for each.
func ropProblems(t *testing.T) map[string]struct{ problem, reason string } {
	t.Helper()
	b, err := json.Marshal(newProblem(0, failedCode, "failed after 4 attempts: planted crash window"))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]struct{ problem, reason string }{
		"detail":            {string(b), "failed after 4 attempts: planted crash window"},
		"empty detail":      {`{"type":"about:blank","title":"The operation failed","code":"OperationFailed","detail":""}`, "OperationFailed"},
		"no detail field":   {`{"type":"about:blank","title":"The operation failed","code":"OperationFailed"}`, "OperationFailed"},
		"whitespace detail": {`{"type":"about:blank","title":"The operation failed","code":"OperationFailed","detail":"   \t "}`, "OperationFailed"},
	}
}

// ropPlant plants the crash window on the fake durable service: a set of two trials whose trial 1
// (RUN-1) runs and whose operation has ended `failed` at `at`, the set's record still `running`, the
// engine restarted (its work held) over that store. The clock is then an hour past `at`.
func ropPlant(t *testing.T, problem string) (x *dopRig, op spOp, at time.Time) {
	t.Helper()
	x, op = tfStartedSet(t)
	x.ops.release(op.ID)
	x.waitStarts(1)
	x.recordRun() // RUN-1 is running: its workflow wrote its start
	x.restart(false, true)
	at = x.clock.Now().UTC().Truncate(time.Second)
	plant := func() {
		if ended, err := x.st.EndOperation(op.ID, at, "failed", nil, json.RawMessage(problem)); err != nil || !ended {
			t.Fatalf("planting the failed end of %s: ended %v, %v", op.ID, ended, err)
		}
	}
	plant()
	if s := tfRawState(t, x.st, op.Target); s != "running" {
		t.Fatalf("the planted crash window has the set %q, want running", s)
	}
	x.clock.Advance(time.Hour) // the time of a read is not the operation's end
	return x, op, at
}

// ropRead reads the set through getTrialSet or listTrialSets.
func ropRead(t *testing.T, x *dopRig, via, id string) map[string]any {
	t.Helper()
	if via == "getTrialSet" {
		return x.tfSetOf(id)
	}
	page := tfMap(t, x.get(spURL("S-1", "/trial-sets")), "listTrialSets")
	items, _ := page["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("listTrialSets = %v, want the one set", page)
	}
	return items[0].(map[string]any)
}

// ropWaitCancels waits until the durable service has been asked to cancel the run at least n times.
func ropWaitCancels(t *testing.T, x *dopRig, run string, n int) {
	t.Helper()
	end := time.Now().Add(10 * time.Second)
	for x.tfCancels(run) < n {
		if time.Now().After(end) {
			t.Fatalf("%d cancels of %s after 10 s, want at least %d; calls %v", x.tfCancels(run), run, n, x.log.all())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ropRecord is the set's record bytes as the store holds them.
func ropRecord(t *testing.T, x *dopRig, id string) string {
	t.Helper()
	_, b, err := x.st.TrialSet(id)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ropSettleTime is how long a negative check waits for an engine start that must do nothing.
const ropSettleTime = 400 * time.Millisecond

func TestTSROAReadOfACrashLeftSetWritesNothingAndShowsItFailedFromItsOperation(t *testing.T) { // T036
	for name, c := range ropProblems(t) {
		for _, via := range []string{"getTrialSet", "listTrialSets"} {
			t.Run(name+"/"+via, func(t *testing.T) {
				x, op, at := ropPlant(t, c.problem)
				treeBefore := storeTree(t, x.dir)
				v := ropRead(t, x, via, op.Target)
				tfWantFailed(t, v, c.reason)
				if got, _ := v["failed_because"].(string); got != c.reason {
					t.Errorf("failed_because = %q, want %q", got, c.reason)
				}
				tfWantEndedAt(t, v, at)
				// Reading again, through both entry points and after more time, shows the same and writes nothing.
				x.clock.Advance(time.Hour)
				for _, again := range []string{"getTrialSet", "listTrialSets"} {
					w := ropRead(t, x, again, op.Target)
					if w["state"] != "failed" || w["failed_because"] != v["failed_because"] || w["ended_at"] != v["ended_at"] {
						t.Errorf("%s = %v, want what the first read showed: %v", again, w, v)
					}
				}
				if s := tfRawState(t, x.st, op.Target); s != "running" {
					t.Errorf("after reads the record is %q, want running", s)
				}
				if n := x.tfCancels("RUN-1"); n != 0 {
					t.Errorf("%d cancels of RUN-1 after reads, want none: a read cancels nothing", n)
				}
				if r := tfMap(t, x.get("/runs/RUN-1?api-version="+apiVersion), "getRun RUN-1"); r["status"] == "ended" {
					t.Errorf("RUN-1 = %v after reads, want it not ended", r)
				}
				if treeAfter := storeTree(t, x.dir); !sameTree(treeBefore, treeAfter) {
					t.Errorf("reads changed the store: %s", treeDiff(treeBefore, treeAfter))
				}
			})
		}
	}
}

func TestTSROBTheEnginesStartRepairsACrashLeftSetWithNoRead(t *testing.T) { // T036
	const reason = "failed after 4 attempts: planted crash window"
	problem := ropProblems(t)["detail"].problem
	t.Run("record failed and trial cancelled", func(t *testing.T) {
		x, op, at := ropPlant(t, problem)
		x.restart(false, true) // the engine's start; no read follows
		tfWaitRawState(t, x.st, op.Target, "failed")
		ropWaitCancels(t, x, "RUN-1", 1)
		var rec map[string]any
		if err := json.Unmarshal([]byte(ropRecord(t, x, op.Target)), &rec); err != nil {
			t.Fatal(err)
		}
		tfWantFailed(t, rec, reason)
		tfWantEndedAt(t, rec, at)
	})
	t.Run("cancelled again at each start while the record has not ended", func(t *testing.T) {
		x, op, _ := ropPlant(t, problem)
		x.restart(false, true)
		tfWaitRawState(t, x.st, op.Target, "failed")
		for want := 1; want <= 3; want++ {
			ropWaitCancels(t, x, "RUN-1", want)
			time.Sleep(ropSettleTime) // the start's cancel is made once; the next start makes the next
			if n := x.tfCancels("RUN-1"); n != want {
				t.Fatalf("%d cancels of RUN-1 after %d starts, want %d: one cancel at each start", n, want, want)
			}
			x.restart(false, true)
		}
		ropWaitCancels(t, x, "RUN-1", 4)
	})
	t.Run("the service is down at the start", func(t *testing.T) {
		x, op, _ := ropPlant(t, problem)
		x.runs.set(func(f *dopRuns) { f.down = true })
		x.restart(false, true)
		tfWaitRawState(t, x.st, op.Target, "failed") // the record is written though the cancel cannot land
		time.Sleep(ropSettleTime)
		if n := x.tfCancels("RUN-1"); n != 0 {
			t.Fatalf("%d cancels reached the down service", n)
		}
		x.runs.set(func(f *dopRuns) { f.down = false })
		x.restart(false, true) // the next start, the service is back
		ropWaitCancels(t, x, "RUN-1", 1)
		if s := tfRawState(t, x.st, op.Target); s != "failed" {
			t.Errorf("the record is %q, want failed", s)
		}
	})
	t.Run("nothing more once the trial's record has ended", func(t *testing.T) {
		x, op, _ := ropPlant(t, problem)
		x.restart(false, true)
		tfWaitRawState(t, x.st, op.Target, "failed")
		ropWaitCancels(t, x, "RUN-1", 1)
		x.recordRunMore("RunCancelled") // the run's workflow ends its record
		time.Sleep(ropSettleTime)
		n, rec, tree := x.tfCancels("RUN-1"), ropRecord(t, x, op.Target), storeTree(t, x.dir)
		x.restart(false, true)
		x.restart(false, true)
		time.Sleep(ropSettleTime)
		if got := x.tfCancels("RUN-1"); got != n {
			t.Errorf("%d cancels of RUN-1 after its record ended, want the %d before: nothing more", got, n)
		}
		if got := ropRecord(t, x, op.Target); got != rec {
			t.Errorf("the record changed by later starts:\n%s\nwas:\n%s", got, rec)
		}
		if after := storeTree(t, x.dir); !sameTree(tree, after) {
			t.Errorf("later engine starts changed the store: %s", treeDiff(tree, after))
		}
	})
}

func TestTSROCAReadAfterTheRepairShowsWhatTheReadBeforeShowedAndChangesNothing(t *testing.T) { // T036
	const reason = "failed after 4 attempts: planted crash window"
	for _, via := range []string{"getTrialSet", "listTrialSets"} {
		t.Run(via, func(t *testing.T) {
			x, op, at := ropPlant(t, ropProblems(t)["detail"].problem)
			before := ropRead(t, x, via, op.Target) // the crash window, read
			tfWantFailed(t, before, reason)
			tfWantEndedAt(t, before, at)
			res, _ := before["result"].(map[string]any)
			if res["ended"] != float64(0) || res["done"] != float64(0) {
				t.Errorf("result = %v before the repair, want no trial ended: its record has not ended", res)
			}
			x.restart(false, true) // the repair
			tfWaitRawState(t, x.st, op.Target, "failed")
			ropWaitCancels(t, x, "RUN-1", 1)
			// The repaired record holds what the read showed.
			var rec map[string]any
			if err := json.Unmarshal([]byte(ropRecord(t, x, op.Target)), &rec); err != nil {
				t.Fatal(err)
			}
			if rec["failed_because"] != before["failed_because"] {
				t.Errorf("the repaired record has failed_because %v, the read showed %v", rec["failed_because"], before["failed_because"])
			}
			tfWantEndedAt(t, rec, at)
			x.recordRunMore("RunCancelled") // the cancelled trial's record ends
			time.Sleep(ropSettleTime)
			n, recBytes, tree := x.tfCancels("RUN-1"), ropRecord(t, x, op.Target), storeTree(t, x.dir)
			x.clock.Advance(time.Hour)
			after := ropRead(t, x, via, op.Target)
			tfWantFailed(t, after, reason)
			if after["failed_because"] != before["failed_because"] || after["ended_at"] != before["ended_at"] || after["state"] != "failed" {
				t.Errorf("read after the repair = %v, want the same state, failed_because and ended_at as before it: %v", after, before)
			}
			res, _ = after["result"].(map[string]any)
			if res["ended"] != float64(1) || res["done"] != float64(0) {
				t.Errorf("result = %v after the repair, want the cancelled trial counted ended and not done", res)
			}
			if got := x.tfCancels("RUN-1"); got != n {
				t.Errorf("%d cancels of RUN-1 after a read, want the %d before it: a read cancels nothing", got, n)
			}
			if got := ropRecord(t, x, op.Target); got != recBytes {
				t.Errorf("a read changed the record:\n%s\nwas:\n%s", got, recBytes)
			}
			if treeAfter := storeTree(t, x.dir); !sameTree(tree, treeAfter) {
				t.Errorf("a read changed the store: %s", treeDiff(tree, treeAfter))
			}
		})
	}
}

// sameTree is whether two store trees (storeTree) hold the same files with the same bytes.
func sameTree(a, b map[string]string) bool { return treeDiff(a, b) == "" }

// treeDiff names the files that differ between two store trees; "" when there are none.
func treeDiff(a, b map[string]string) string {
	out := ""
	for p, c := range b {
		if old, ok := a[p]; !ok {
			out += " added " + p
		} else if old != c {
			out += " changed " + p
		}
	}
	for p := range a {
		if _, ok := b[p]; !ok {
			out += " removed " + p
		}
	}
	return out
}
