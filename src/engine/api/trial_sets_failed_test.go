package api

// T031 of feature 012 (US1, review B1; spec Clarifications 2026-11-28, FR-003; data model "States"
// rules 1-4; ADR 0106; engine contract `TrialSetState` `failed` and `TrialSet.failed_because`): a
// trial set whose `run-trials` operation ends `failed` ends `failed` with the operation's problem
// `detail` as `failed_because`, its unended last trial is cancelled, and a set that was left `running`
// by a crash between the two writes is repaired by the next read, start or delete. Written before the
// code (T032) by a test author, judged from the spec, the contract and the data model; the code author
// must not change it (ADR 0029). The rigs and helpers of trial_sets_test.go and
// operations_durable_test.go are used as they are; only new helpers are in this file.
//
// Two rigs, as in trial_sets_test.go:
//   - trRig (real runs on faked step workers; the operation's job runs in the engine under the retry
//     rule): a trial whose start is refused for good, and the crash window planted with the
//     store's own EndOperation (the operation ended `failed` while the set record says `running`, the
//     job's trial held), read through each of the four entry points; a 24-hour stop racing it;
//   - dopRig (fake durable Runs and Operations): a job whose retries run out because the durable
//     service is down (its running trial cannot be cancelled then, and is cancelled by the next read
//     once the service is back), the generic workflow's `End` (Operator.End) on a held job, late
//     failures of operations whose set has ended, stopped or been cancelled, a repeated failed end, a
//     failed set with no trial or with its trial ended, cancelOperation, erasure.
//
// Edge cases checked: failed against ended, stopped and cancelled (a set that is not `running` is
// never changed by its operation's failure, whether the failure arrives through End or is found by a
// read); a repeated failed end and repeated reads change nothing (the record's bytes, ended_at,
// failed_because); a failed set with no trial started, with its trial already ended (nothing is
// cancelled) and with its trial unended (cancelled, the run ends `cancelled`, counted ended and not
// done); the cancel that cannot land (the service is down), the set stays `failed`, the trial not
// ended, the next read once the service is back cancels it, and a read after the trial's end cancels
// nothing again; a refused start ends the set `failed` after the trial before it ended and starts no
// further trial; the set is ended right after the operation, without any read (the record is looked at
// directly); the set's reason is the operation's `detail`, equal to what getOperation shows; a failed
// set has `ended_at` and no `stopped_because`; getTrialSet and listTrialSets agree; a failed set does
// not block its slice (a new set starts; deleteSlice is accepted); erasing the slice erases the failed
// set and its record; cancelOperation of the failed set's operation answers OperationEnded and the set
// stays `failed`; the 24 hours passing while the failure is recorded ends the set in exactly one
// state, `failed` or `stopped` `time`, never both, the same on every later read.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/store"
)

// ---- helpers ----

// tfMap decodes an answer's JSON object, so that absent fields can be told from empty ones.
func tfMap(t *testing.T, p reply, what string) map[string]any {
	t.Helper()
	if p.Code != http.StatusOK {
		t.Fatalf("%s = %d: %s", what, p.Code, p.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(p.Body.Bytes(), &m); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	return m
}

// tfWantFailed checks a trial set as the contract shows a failed one: state `failed`, `ended_at`, the
// reason in `failed_because` (any non-empty one when reason is empty), no `stopped_because`.
func tfWantFailed(t *testing.T, m map[string]any, reason string) {
	t.Helper()
	got, _ := m["failed_because"].(string)
	if m["state"] != "failed" {
		t.Fatalf("set %v: state = %v (failed_because %v), want failed", m["id"], m["state"], m["failed_because"])
	}
	switch {
	case m["ended_at"] == nil:
		t.Errorf("set %v is failed and has no ended_at", m["id"])
	case got == "" || (reason != "" && got != reason):
		t.Errorf("set %v: failed_because = %q, want %q", m["id"], got, reason)
	}
	if _, has := m["stopped_because"]; has {
		t.Errorf("set %v is failed and has stopped_because %v", m["id"], m["stopped_because"])
	}
}

// tfWantNotFailed checks a set that must not carry the failure: no `failed_because`.
func tfWantNotFailed(t *testing.T, m map[string]any, state string) {
	t.Helper()
	if m["state"] != state {
		t.Errorf("set %v: state = %v, want %s", m["id"], m["state"], state)
	}
	if v, has := m["failed_because"]; has {
		t.Errorf("set %v is %v and has failed_because %v", m["id"], m["state"], v)
	}
}

// tfRawState is the state in the set's record as the store holds it, with no read of the engine's:
// "" when the store has no record.
func tfRawState(t *testing.T, st *store.Store, id string) string {
	t.Helper()
	_, b, err := st.TrialSet(id)
	if err != nil {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("the record of %s: %v", id, err)
	}
	s, _ := m["state"].(string)
	return s
}

// tfWaitRawState waits, without reading through the engine, until the set's record has the state.
func tfWaitRawState(t *testing.T, st *store.Store, id, state string) {
	t.Helper()
	end := time.Now().Add(10 * time.Second)
	for tfRawState(t, st, id) != state {
		if time.Now().After(end) {
			t.Fatalf("the record of %s is %q after 10 s with no read, want %q: the set ends right after its operation", id, tfRawState(t, st, id), state)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// tfEndOp writes the end `failed` of the operation into the store as the Operation workflow does, with
// the problem a failed operation has; it is the crash window's first write.
func tfEndOp(t *testing.T, st *store.Store, now time.Time, id, detail string) {
	t.Helper()
	b, err := json.Marshal(newProblem(0, failedCode, detail))
	if err != nil {
		t.Fatal(err)
	}
	if ended, err := st.EndOperation(id, now.UTC(), "failed", nil, b); err != nil || !ended {
		t.Fatalf("planting the failed end of %s: ended %v, %v", id, ended, err)
	}
}

// tfEditSet rewrites fields of the set's record in the store, to plant what a crash or an earlier
// engine left.
func tfEditSet(t *testing.T, st *store.Store, id string, edit func(m map[string]any)) {
	t.Helper()
	slice, b, err := st.TrialSet(id)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	if b, err = json.Marshal(m); err != nil {
		t.Fatal(err)
	}
	if err := st.PutTrialSet(slice, id, b); err != nil {
		t.Fatal(err)
	}
}

// tfSetOf reads a set through getTrialSet on the engine rig.
func (x *trRig) tfSetOf(id string) map[string]any {
	x.t.Helper()
	return tfMap(x.t, x.do("GET", apiPath("/trial-sets/"+id), ""), "getTrialSet "+id)
}

// tfOpDetail is the `detail` of an operation that has ended `failed`, as getOperation gives it.
func tfOpDetail(t *testing.T, op spOp) string {
	t.Helper()
	if op.Status != "failed" || op.Error == nil || op.Error.Detail == "" {
		t.Fatalf("operation = %+v, want failed with a problem detail", op)
	}
	return op.Error.Detail
}

// tfRefusedSet runs a set of 3 trials on the engine rig whose second trial's start is refused for good:
// its project is made non-empty after acceptance while trial 1 runs. It returns the set's operation.
func tfRefusedSet(x *trRig) spOp {
	x.registerSet("trial", 3)
	rel := x.ctl.hold("trial-1")
	op := x.startSetOK("trial", 3)
	set := x.waitRuns(op.Target, 1)
	x.waitRunStatus(set.Runs[0], "running")
	dir := filepath.Join(x.projects, "trial-2")
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // a project folder as build-workers makes it
		x.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil { //nolint:gosec // a project file
		x.t.Fatal(err)
	}
	rel()
	return op
}

// ---- the engine rig: a refused start, the crash window ----

func TestTSFATrialWhoseStartIsRefusedForGoodEndsTheSetFailedWithTheRefusalAndStartsNoOther(t *testing.T) { // T031
	x := newTRRig(t)
	op := tfRefusedSet(x)
	end := x.settleOp(op.ID)
	reason := tfOpDetail(t, end)
	if !strings.Contains(reason, "trial-2") {
		t.Errorf("the operation's detail %q does not name the refused trial's project trial-2", reason)
	}
	tfWaitRawState(t, x.st, op.Target, "failed") // right after the operation's end, with no read of the set
	v := x.tfSetOf(op.Target)
	tfWantFailed(t, v, reason)
	set := x.getSet(op.Target)
	if set.Result.Ended != 1 || set.Result.Done != 1 {
		t.Errorf("result = %+v, want trial 1 (done) ended and no other", set.Result)
	}
	if got := x.ctl.startedProjects(); !sameStrings(got, []string{"trial-1"}) {
		t.Errorf("workers were called for %v, want only trial-1: no further trial starts", got)
	}
	if n := len(x.listRuns("").Items); n != 1 {
		t.Errorf("%d runs of the slice, want 1: the refused trial and the one after it have no run", n)
	}
	if out := x.outcomeOf(set.Runs[0]); out != "done" {
		t.Errorf("trial 1 ended %q, want done: the refusal cancels nothing else", out)
	}
	time.Sleep(3 * trialPoll)
	if again := x.tfSetOf(op.Target); again["ended_at"] != v["ended_at"] || again["failed_because"] != v["failed_because"] {
		t.Errorf("set after waiting = %v, want what it was: %v", again, v)
	}
	// getTrialSet and listTrialSets agree.
	page := tfMap(t, x.do("GET", spURL("S-1", "/trial-sets"), ""), "listTrialSets")
	items, _ := page["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("listTrialSets = %v, want the one set", page)
	}
	tfWantFailed(t, items[0].(map[string]any), reason)
	// The slice is free for a new set.
	x.registerSet("again", 1)
	x.settled(x.startSetOK("again", 1), "ended")
}

func TestTSFErasingTheSliceOfAFailedSetErasesItAndItsRecord(t *testing.T) { // T031: deleteSlice after a failed set
	x := newTRRig(t)
	op := tfRefusedSet(x)
	tfOpDetail(t, x.settleOp(op.ID))
	x.waitState(op.Target, "failed")
	del := x.do("DELETE", spURL("S-1", ""), "").operation(t, 202)
	if end := x.settleOp(del.Id); end.Status != "succeeded" {
		t.Fatalf("deleteSlice after a failed set = %+v, want succeeded", end)
	}
	x.do("GET", apiPath("/trial-sets/"+op.Target), "").problem(t, 404, "NotFound")
	x.do("GET", spURL("S-1", "/trial-sets"), "").problem(t, 404, "NotFound")
	if names := storeNamesWith(t, x.dir, op.Target); len(names) != 0 {
		t.Errorf("after the erasure the store still holds %v", names)
	}
}

// tfCrashWindow starts a set of 3 trials with trial 1 held and running, and ends its operation
// `failed` in the store alone, as the Operation workflow does before the set is written: the set's
// record still says `running`, its trial still runs (a crash between the two writes).
func tfCrashWindow(t *testing.T, x *trRig, reason string) (op spOp, run string) {
	t.Helper()
	x.registerSet("trial", 3)
	x.registerSet("again", 1)
	x.ctl.hold("trial-1")
	op = x.startSetOK("trial", 3)
	run = x.waitRuns(op.Target, 1).Runs[0]
	x.waitRunStatus(run, "running")
	tfEndOp(t, x.st, x.r.clock.Now(), op.ID, reason)
	if s := tfRawState(t, x.st, op.Target); s != "running" {
		t.Fatalf("the planted crash window has the set %q, want running", s)
	}
	return op, run
}

func TestTSFASetLeftRunningByACrashIsRepairedOnceByTheNextReadStartOrDelete(t *testing.T) { // T031: data model States rule 3
	const reason = "failed after 4 attempts: planted crash window"
	for _, via := range []string{"getTrialSet", "listTrialSets", "startTrialSet", "deleteSlice"} {
		t.Run(via, func(t *testing.T) {
			x := newTRRig(t)
			op, run := tfCrashWindow(t, x, reason)
			var newOp spOp
			switch via {
			case "getTrialSet":
				tfWantFailed(t, x.tfSetOf(op.Target), reason)
			case "listTrialSets":
				page := tfMap(t, x.do("GET", spURL("S-1", "/trial-sets"), ""), "listTrialSets")
				items, _ := page["items"].([]any)
				if len(items) != 1 {
					t.Fatalf("listTrialSets = %v, want the one set", page)
				}
				tfWantFailed(t, items[0].(map[string]any), reason)
			case "startTrialSet":
				newOp = x.startSetOK("again", 1) // accepted: the failed set no longer holds the slice
				if s := tfRawState(t, x.st, op.Target); s != "failed" {
					t.Fatalf("after startTrialSet the old set is %q, want failed", s)
				}
			case "deleteSlice":
				p := x.do("DELETE", spURL("S-1", ""), "")
				if p.Code == http.StatusConflict { // the cancelled trial's run may still be ending
					if s := tfRawState(t, x.st, op.Target); s != "failed" {
						t.Fatalf("deleteSlice was refused RunActive with the set %q, want it repaired to failed first", s)
					}
					x.waitRunEnded(run)
					p = x.do("DELETE", spURL("S-1", ""), "")
				}
				del := p.operation(t, 202)
				if end := x.settleOp(del.Id); end.Status != "succeeded" {
					t.Fatalf("deleteSlice of a slice whose set was left running by a crash = %+v, want succeeded", end)
				}
				x.do("GET", apiPath("/trial-sets/"+op.Target), "").problem(t, 404, "NotFound")
				return
			}
			// The trial is cancelled: its run ends `cancelled`, and the result counts it ended, not done.
			if r := x.waitRunEnded(run); r.Status != "ended" || x.outcomeOf(run) != "cancelled" {
				t.Errorf("trial 1 = %+v (outcome %q), want ended cancelled", r, x.outcomeOf(run))
			}
			v := x.tfSetOf(op.Target)
			tfWantFailed(t, v, reason)
			if set := x.getSet(op.Target); set.Result.Ended != 1 || set.Result.Done != 0 || len(set.Runs) != 1 {
				t.Errorf("result = %+v with runs %v, want the cancelled trial counted ended and not done, and no other trial", set.Result, set.Runs)
			}
			// Once: a later read changes nothing, however long after.
			x.r.clock.Advance(time.Hour)
			if again := x.tfSetOf(op.Target); again["ended_at"] != v["ended_at"] || again["failed_because"] != v["failed_because"] {
				t.Errorf("set read again = %v, want what the repair wrote: %v", again, v)
			}
			// The operation keeps its own end, and the slice is free.
			if end := x.settleOp(op.ID); tfOpDetail(t, end) != reason {
				t.Errorf("operation = %+v, want the failed end that was recorded", end)
			}
			if newOp.ID == "" {
				newOp = x.startSetOK("again", 1)
			}
			x.settled(newOp, "ended")
		})
	}
}

func TestTSFATimeStopRacingTheFailureEndsTheSetInExactlyOneState(t *testing.T) { // T031: the first recorded wins
	const reason = "failed after 4 attempts: racing the time stop"
	x := newTRRig(t)
	op, run := tfCrashWindow(t, x, reason)
	x.r.clock.Advance(time.Duration(contractLimit(t, "trial-set-hours"))*time.Hour + time.Minute)
	v := x.waitSet(op.Target, "failed or stopped", func(v trSetView) bool { return v.State != "running" })
	x.waitRunEnded(run)
	first := x.tfSetOf(op.Target)
	switch v.State {
	case "failed":
		tfWantFailed(t, first, reason)
	case "stopped":
		tfWantNotFailed(t, first, "stopped")
		if first["stopped_because"] != "time" {
			t.Errorf("set stopped because %v, want time", first["stopped_because"])
		}
	default:
		t.Fatalf("set is %q, want failed or stopped", v.State)
	}
	for range 3 {
		time.Sleep(trialPoll)
		if again := x.tfSetOf(op.Target); again["state"] != first["state"] || again["ended_at"] != first["ended_at"] ||
			again["failed_because"] != first["failed_because"] || again["stopped_because"] != first["stopped_because"] {
			t.Fatalf("set = %v, then %v: it ends in exactly one state, the first recorded", first, again)
		}
	}
	if out := x.outcomeOf(run); out != "cancelled" {
		t.Errorf("trial 1 ended %q, want cancelled", out)
	}
}

// ---- the fake durable service ----

// tfStartedSet accepts a set of two trials on the fake durable service with its job held (not run).
func tfStartedSet(t *testing.T) (*dopRig, spOp) {
	t.Helper()
	x := newDopRig(t)
	x.seed(1, true)
	x.registerAll()
	x.ops.set(func(f *dopOps) { f.hold = true })
	return x, x.opView(x.startSet("trial", 2), 202)
}

func (x *dopRig) tfSetOf(id string) map[string]any {
	x.t.Helper()
	return tfMap(x.t, x.get("/trial-sets/"+id+"?api-version="+apiVersion), "getTrialSet "+id)
}

func (x *dopRig) tfEnd(op spOp, detail string) {
	x.t.Helper()
	if err := x.op.End(x.t.Context(), op.ID, gen.OperationStatusFailed, detail); err != nil {
		x.t.Fatal(err)
	}
}

func (x *dopRig) tfCancels(run string) int {
	n := 0
	for _, l := range x.log.all() {
		if l == "runs.cancel "+run {
			n++
		}
	}
	return n
}

func TestTSFAJobWhoseRetriesRunOutEndsTheSetFailedAndCancelsItsTrialOnceTheServiceIsBack(t *testing.T) { // T031
	x, op := tfStartedSet(t)
	x.ops.release(op.ID)
	x.waitStarts(1)
	x.recordRun() // RUN-1 is running: its workflow wrote its start
	// The engine restarts and the durable service is down: the job, repeated, cannot reach it.
	x.restart(false, true)
	x.registerAll()
	x.runs.set(func(f *dopRuns) { f.down = true })
	x.ops.release(op.ID)
	end := x.settle(op.ID)
	if end.Status != "failed" || end.Error == nil || !strings.Contains(end.Error.Detail, "failed after") {
		t.Fatalf("operation = %+v, want failed once the retry rule's attempts are used up", end)
	}
	tfWaitRawState(t, x.st, op.Target, "failed") // ended right after the operation, with no read
	// The cancel cannot land while the service is down: the set is failed and the trial is not ended.
	v := x.tfSetOf(op.Target)
	tfWantFailed(t, v, end.Error.Detail)
	if n := x.tfCancels("RUN-1"); n != 0 {
		t.Fatalf("%d cancels reached the down service", n)
	}
	if r := tfMap(t, x.get("/runs/RUN-1?api-version="+apiVersion), "getRun RUN-1"); r["status"] == "ended" {
		t.Fatalf("RUN-1 = %v, want not ended while its cancel cannot land", r)
	}
	// The service is back: the next read cancels the trial, and the set is the same failed set.
	x.runs.set(func(f *dopRuns) { f.down = false })
	again := x.tfSetOf(op.Target)
	if again["ended_at"] != v["ended_at"] || again["failed_because"] != v["failed_because"] || again["state"] != "failed" {
		t.Errorf("set after the service is back = %v, want %v", again, v)
	}
	if x.tfCancels("RUN-1") == 0 {
		t.Fatalf("calls %v: want the running trial cancelled by the first read after the service is back", x.log.all())
	}
	// Its record ends: nothing is cancelled again by later reads.
	x.recordRunMore("RunCancelled")
	n := x.tfCancels("RUN-1")
	x.tfSetOf(op.Target)
	x.tfSetOf(op.Target)
	if got := x.tfCancels("RUN-1"); got != n {
		t.Errorf("%d cancels of RUN-1 after its record ended, want the %d before: a repair cancels until the record ends only", got, n)
	}
	if set := x.getSetMap(op.Target); set["state"] != "failed" {
		t.Errorf("set = %v, want failed", set)
	}
}

func (x *dopRig) getSetMap(id string) map[string]any { return x.tfSetOf(id) }

func TestTSFTheGenericWorkflowsFailedEndEndsTheSetFailedWithNoTrialAndFreesTheSlice(t *testing.T) { // T031
	x, op := tfStartedSet(t)
	x.tfEnd(op, "failed after 4 attempts: the work could not start a trial")
	tfWaitRawState(t, x.st, op.Target, "failed")
	end := x.settle(op.ID)
	reason := tfOpDetail(t, spOp(end))
	v := x.tfSetOf(op.Target)
	tfWantFailed(t, v, reason)
	if runs, _ := v["runs"].([]any); len(runs) != 0 {
		t.Errorf("runs = %v, want none: no trial had started", runs)
	}
	res, _ := v["result"].(map[string]any)
	if res["ended"] != float64(0) || res["done"] != float64(0) {
		t.Errorf("result = %v, want no trial ended", res)
	}
	if n := x.runs.startCount(); n != 0 {
		t.Errorf("%d runs started, want none", n)
	}
	for _, l := range x.log.all() {
		if strings.HasPrefix(l, "runs.cancel") {
			t.Errorf("calls %v: a set with no trial cancels nothing", x.log.all())
		}
	}
	// The slice is free: a second set is accepted.
	x.opView(x.startSet("other", 1), 202)
}

func TestTSFARepeatedFailedEndAndRepeatedReadsChangeNothing(t *testing.T) { // T031
	x, op := tfStartedSet(t)
	x.tfEnd(op, "first reason")
	v := x.tfSetOf(op.Target)
	tfWantFailed(t, v, "first reason")
	raw := func() string {
		_, b, err := x.st.TrialSet(op.Target)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	before := raw()
	x.clock.Advance(time.Hour)
	x.tfEnd(op, "second reason") // the workflow repeats its end: the operation's first end stands
	list := tfMap(t, x.get("/slices/S-1/trial-sets?api-version="+apiVersion), "listTrialSets")
	if items, _ := list["items"].([]any); len(items) != 1 {
		t.Fatalf("listTrialSets = %v, want the one set", list)
	} else {
		tfWantFailed(t, items[0].(map[string]any), "first reason")
	}
	x.tfSetOf(op.Target)
	if after := raw(); after != before {
		t.Errorf("the record changed by a repeated end and reads:\n%s\nwas:\n%s", after, before)
	}
	if end := x.settle(op.ID); tfOpDetail(t, spOp(end)) != "first reason" {
		t.Errorf("operation = %+v, want its first failed end", end)
	}
}

func TestTSFAFailedSetWhoseTrialHasEndedCancelsNothing(t *testing.T) { // T031: the unended last trial only
	x, op := tfStartedSet(t)
	x.recordRun()
	x.recordRunMore("RunCompleted") // RUN-1 has ended done
	tfEditSet(t, x.st, op.Target, func(m map[string]any) { m["runs"] = []string{"RUN-1"} })
	x.tfEnd(op, "failed after 4 attempts: planted")
	v := x.tfSetOf(op.Target)
	tfWantFailed(t, v, "failed after 4 attempts: planted")
	res, _ := v["result"].(map[string]any)
	if res["ended"] != float64(1) || res["done"] != float64(1) {
		t.Errorf("result = %v, want the ended trial counted ended and done", res)
	}
	x.tfSetOf(op.Target)
	if n := x.tfCancels("RUN-1"); n != 0 {
		t.Errorf("%d cancels of RUN-1, want none: it had ended", n)
	}
}

func TestTSFASetThatHasEndedStoppedOrBeenCancelledIsNeverChangedByItsOperationsFailure(t *testing.T) { // T031
	for _, c := range []struct{ state, stopped string }{{"ended", ""}, {"stopped", "budget"}, {"cancelled", ""}} {
		t.Run(c.state, func(t *testing.T) {
			x, op := tfStartedSet(t)
			x.clock.Advance(time.Minute)
			at := x.clock.Now().UTC().Format(time.RFC3339Nano)
			tfEditSet(t, x.st, op.Target, func(m map[string]any) {
				m["state"], m["ended_at"] = c.state, at
				if c.stopped != "" {
					m["stopped_because"] = c.stopped
				}
			})
			x.tfEnd(op, "late failure") // the failure arrives after the set's own end
			for _, via := range []string{"getTrialSet", "listTrialSets"} {
				var v map[string]any
				switch via {
				case "getTrialSet":
					v = x.tfSetOf(op.Target)
				case "listTrialSets":
					items, _ := tfMap(t, x.get("/slices/S-1/trial-sets?api-version="+apiVersion), "listTrialSets")["items"].([]any)
					if len(items) != 1 {
						t.Fatalf("listTrialSets = %v", items)
					}
					v = items[0].(map[string]any)
				}
				tfWantNotFailed(t, v, c.state)
				if v["stopped_because"] != nilIfEmpty(c.stopped) {
					t.Errorf("%s: stopped_because = %v, want %q", via, v["stopped_because"], c.stopped)
				}
			}
			if s := tfRawState(t, x.st, op.Target); s != c.state {
				t.Errorf("the record is %q, want %s", s, c.state)
			}
			if x.runs.startCount() != 0 {
				t.Errorf("a failure of the operation of a %s set started a run", c.state)
			}
		})
	}
}

// nilIfEmpty is the JSON-decoded form of an optional string: nil when it is empty.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func TestTSFCancellingTheOperationOfAFailedSetIsOperationEndedAndTheSetStaysFailed(t *testing.T) { // T031
	x, op := tfStartedSet(t)
	x.tfEnd(op, "failed after 4 attempts: planted")
	x.do("POST", "/operations/"+op.ID+"/cancel?api-version="+apiVersion, "", "").problem(t, 409, gen.ErrorCodeOperationEnded)
	tfWantFailed(t, x.tfSetOf(op.Target), "failed after 4 attempts: planted")
	if end := x.settle(op.ID); end.Status != "failed" {
		t.Errorf("operation = %+v, want it still failed", end)
	}
}

func TestTSFErasingTheSliceOfASetFailedWithNoTrialErasesIt(t *testing.T) { // T031
	x, op := tfStartedSet(t)
	x.tfEnd(op, "failed after 4 attempts: planted")
	tfWantFailed(t, x.tfSetOf(op.Target), "")
	del := x.accepted(x.do("DELETE", spURL("S-1", ""), "", ""), "delete-slice", "S-1")
	x.ops.release(del.ID) // the rig holds every operation's work since tfStartedSet
	if end := x.settle(del.ID); end.Status != "succeeded" {
		t.Fatalf("deleteSlice = %+v, want succeeded: a failed set does not block it", end)
	}
	x.do("GET", "/trial-sets/"+op.Target+"?api-version="+apiVersion, "", "").problem(t, 404, "NotFound")
	if names := storeNamesWith(t, x.dir, op.Target); len(names) != 0 {
		t.Errorf("after the erasure the store still holds %v", names)
	}
}
