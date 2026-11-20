package api

// T060 (T021 review R2-1; data model "Run record", FR-021, Principle X): a run whose workflow
// failed before it recorded RunStarted (for example the non-retryable InvalidRun) has no record, so
// nothing can end it as a record-based run. It is ended as never started: it is not active (it does
// not block another start with RunActive), getRun answers NotFound and listRuns does not list it,
// and its operation ends failed with OperationFailed carrying the workflow's reason. While its
// workflow is still open (not closed), a run without RunStarted still blocks with RunActive.
// Written before the code by a test author, from the task's text; the durable service is faked
// with closingRig, whose Closed answers for RUN-1 only.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/api/gen"
)

const invalidRunReason = "a run needs the slice's refs"

// startWithNoRecord starts RUN-1 and writes no event, as when its workflow failed before RunStarted.
func (x *closingRig) startWithNoRecord() gen.Operation {
	x.t.Helper()
	x.runs.mu.Lock()
	x.runs.exists["RUN-1"] = true
	x.runs.mu.Unlock()
	x.cr.arm()
	op := x.opOf(x.do("POST", spURL("S-1", "/runs"), `{"project":"notes-app"}`), 202)
	if op.Target != "RUN-1" {
		x.t.Fatalf("setup: the run is %s, want RUN-1", op.Target)
	}
	return op
}

func failedBeforeStart(*closingRig, int) (bool, string, error) { return true, invalidRunReason, nil }

func TestARunWhoseWorkflowFailedBeforeRunStartedDoesNotBlockAnotherStart(t *testing.T) {
	x := newClosingRig(t, failedBeforeStart)
	x.startWithNoRecord()
	p := x.do("POST", spURL("S-1", "/runs"), `{"project":"notes-app-2"}`)
	if p.Code != http.StatusAccepted {
		t.Fatalf("a start after a run whose workflow failed before RunStarted = %d, want 202 (not RunActive); body: %s",
			p.Code, p.Body.String())
	}
}

func TestTheOperationOfARunThatFailedBeforeRunStartedEndsFailedWithTheWorkflowsReason(t *testing.T) {
	x := newClosingRig(t, failedBeforeStart)
	op := x.startWithNoRecord()
	got := x.waitOp(op.Id, gen.OperationStatusFailed)
	if got.Error == nil || got.Error.Code != gen.ErrorCodeOperationFailed || got.Error.Detail == nil ||
		!strings.Contains(*got.Error.Detail, invalidRunReason) {
		t.Fatalf("operation error = %+v, want code OperationFailed with a detail containing %q", got.Error, invalidRunReason)
	}
}

func TestARunThatFailedBeforeRunStartedIsNeverStartedNotActiveNotListedNotFound(t *testing.T) {
	x := newClosingRig(t, failedBeforeStart)
	op := x.startWithNoRecord()
	x.waitOp(op.Id, gen.OperationStatusFailed)
	if ev := x.events(); len(ev) != 0 {
		t.Errorf("record = %v, want none: the run never started", kindsOf(ev))
	}
	if p := x.do("GET", apiPath("/runs/RUN-1"), ""); p.Code != http.StatusNotFound {
		t.Errorf("getRun RUN-1 = %d, want 404 (never started): %s", p.Code, p.Body.String())
	}
	if p := x.do("GET", apiPath("/runs"), ""); p.Code != http.StatusOK || strings.Contains(p.Body.String(), `"RUN-1"`) {
		t.Errorf("listRuns = %d %s, want 200 without RUN-1", p.Code, p.Body.String())
	}
	if p := x.do("POST", spURL("S-1", "/runs"), `{"project":"notes-app-2"}`); p.Code != http.StatusAccepted {
		t.Errorf("a start after it = %d, want 202: %s", p.Code, p.Body.String())
	}
}

// Guard: a run whose workflow is still open and has not recorded RunStarted is active.
func TestARunWhoseWorkflowIsStillOpenBeforeRunStartedStillBlocksWithRunActive(t *testing.T) {
	x := newClosingRig(t, func(*closingRig, int) (bool, string, error) { return false, "", nil })
	x.startWithNoRecord()
	p := x.do("POST", spURL("S-1", "/runs"), `{"project":"notes-app-2"}`)
	if p.Code != http.StatusConflict || !strings.Contains(p.Body.String(), "RunActive") {
		t.Fatalf("a start while RUN-1's workflow is open = %d, want 409 RunActive: %s", p.Code, p.Body.String())
	}
}
