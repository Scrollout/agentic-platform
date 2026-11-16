package api

// deleteSlice and the slice's runs: refused with RunActive while a run has not ended, and erasing the
// runs and their events once it has. Judged from the contract's deleteSlice (x-error-codes 409).

import (
	"net/http"
	"testing"
	"time"
)

func (x *runsRig) deleteSlice() reply {
	return x.do("DELETE", spURL("S-1", ""), "")
}

// wantRunKept checks that the slice and the run are still readable and the run is listed.
func (x *runsRig) wantRunKept(id string) {
	x.t.Helper()
	if p := x.do("GET", spURL("S-1", ""), ""); p.Code != 200 {
		x.t.Errorf("getSlice after a refused delete = %d: %s", p.Code, p.Body.String())
	}
	x.getRun(id)
	if got := x.listRuns("").Items; len(got) != 1 || got[0].ID != id {
		x.t.Errorf("listRuns after a refused delete = %+v, want %s", got, id)
	}
	if len(x.events(id, "").Items) == 0 {
		x.t.Errorf("the events of %s were erased by a refused delete", id)
	}
}

func (x *runsRig) wantRunGone(id string) {
	x.t.Helper()
	if got := x.listRuns("").Items; len(got) != 0 {
		x.t.Errorf("listRuns after the delete = %+v, want none", got)
	}
	x.do("GET", apiPath("/runs/"+id), "").problem(x.t, 404, "NotFound")
	x.do("GET", apiPath("/runs/"+id+"/events"), "").problem(x.t, 404, "NotFound")
}

func TestDeleteSliceWhileARunIsRunningIsRunActiveAndErasesNothing(t *testing.T) {
	x := newRunsRig(t)
	release := x.gatedWorker()
	op := x.startOK("notes-app")
	x.waitStatus(op.Target, "running")

	x.deleteSlice().problem(t, 409, "RunActive")
	x.wantRunKept(op.Target)

	release()
	x.waitEnded(op.Target, "done")
	x.wantRunKept(op.Target)
}

func TestDeleteSliceWhileARunWaitsForTheDeveloperIsRunActive(t *testing.T) {
	x := newRunsRig(t)
	x.failStep("tests")
	op := x.startOK("notes-app")
	x.waitStatus(op.Target, "waiting-for-developer")

	x.deleteSlice().problem(t, 409, "RunActive")
	x.wantRunKept(op.Target)
	if v := x.getRun(op.Target); v.Status != "waiting-for-developer" {
		t.Errorf("run is %q after a refused delete, want waiting-for-developer", v.Status)
	}
}

func TestDeleteSliceAfterTheRunEndedErasesItsRunsAndEventsForGood(t *testing.T) {
	x := newRunsRig(t)
	v := x.runDone("notes-app")

	op := x.deleteSlice().operation(t, http.StatusAccepted)
	if string(op.Kind) != "delete-slice" || op.Target != "S-1" {
		t.Fatalf("delete operation = %+v, want delete-slice of S-1", op)
	}
	// The delete is an operation: wait (at most 5 s) until the slice is gone.
	deadline := time.Now().Add(5 * time.Second)
	for x.do("GET", spURL("S-1", ""), "").Code != 404 {
		if time.Now().After(deadline) {
			t.Fatalf("the slice is still readable 5s after deleteSlice")
		}
		time.Sleep(5 * time.Millisecond)
	}
	x.wantRunGone(v.ID)

	// The run's goroutine has finished by now, and must not write the run back.
	time.Sleep(200 * time.Millisecond)
	x.wantRunGone(v.ID)

	// A new handler over the same store still finds nothing.
	x.boot(x.w, x.c)
	x.wantRunGone(v.ID)
}
