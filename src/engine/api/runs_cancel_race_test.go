package api

// Regression: cancelling a run's operation right after the 202 ends the run `cancelled` every time,
// even when the cancel lands before the run operation's goroutine has started.

import (
	"net/http"
	"testing"
	"time"
)

func TestCancellingARunsOperationRightAfterTheStartEndsTheRunCancelled(t *testing.T) {
	deadline := time.Now().Add(60 * time.Second)
	for i := 0; i < 30; i++ {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of 30 runs finished inside the time budget", i)
		}
		x := newRunsRig(t)
		x.gatedWorker()
		op := x.startOK("race-" + string(rune('a'+i%26)) + string(rune('a'+i/26)))
		x.do("POST", "/operations/"+op.ID+"/cancel?api-version="+apiVersion, "").operation(t, http.StatusAccepted)
		run := x.waitStatus(op.Target, "ended")
		if run.Outcome == nil || *run.Outcome != "cancelled" {
			t.Fatalf("iteration %d: run %s ended with outcome %v, want cancelled", i, run.ID, run.Outcome)
		}
		r := x.r.waitFor(t, op.ID, "cancelled")
		if string(r.Status) != "cancelled" {
			t.Fatalf("iteration %d: operation %s reads %q, want cancelled", i, op.ID, r.Status)
		}
	}
}
