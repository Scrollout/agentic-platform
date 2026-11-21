package api

// T058 (fresh review B2; data model "Run record" and the Runs port; research R3 "Defects and a
// store that does not answer"): a run whose workflow closed without writing the end of its record
// is ended by its operation. While the record shows no end, the operation asks Runs.Closed on every
// poll; when the workflow has closed (or has none) and the record, read again, still shows no end,
// the engine appends RunCancelled with the detail `the run's workflow failed: <reason>` as the
// record's next event and the operation fails OperationFailed with that detail. An unavailable
// durable service only keeps it polling. Written before the code by a test author, judged from
// the task's text and the data model, with the durable service faked (closingRuns over fakeRuns).
//
// Interface assumed: Runs.Closed(ctx, run) (closed bool, reason string, err error), an error
// matching ErrNotFound for a run with no workflow and ErrUnavailable for an unreachable service.
//
// The fake answers only for RUN-1 and only once the test has written the run's events (armed), so
// the answer never depends on when the operation's first poll runs. Polls are 2 s apart (followPoll).

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/store"

	"context"
)

// closingRuns is fakeRuns whose Closed answers as the test says.
type closingRuns struct {
	*fakeRuns
	mu     sync.Mutex
	armed  bool
	calls  int
	answer func(n int) (closed bool, reason string, err error) // n counts the armed calls of RUN-1 from 1
}

func (c *closingRuns) Closed(_ context.Context, run string) (bool, string, error) {
	c.mu.Lock()
	if run != "RUN-1" || !c.armed {
		c.mu.Unlock()
		return false, "", nil
	}
	c.calls++
	n := c.calls
	c.mu.Unlock()
	return c.answer(n)
}

func (c *closingRuns) arm()       { c.mu.Lock(); c.armed = true; c.mu.Unlock() }
func (c *closingRuns) asked() int { c.mu.Lock(); defer c.mu.Unlock(); return c.calls }

type closingRig struct {
	*durableRig
	cr *closingRuns
}

// newClosingRig is newDurableRig with registered workers and a durable service that answers Closed
// by answer.
func newClosingRig(t *testing.T, answer func(x *closingRig, n int) (bool, string, error)) *closingRig {
	t.Helper()
	st, err := store.Open(t.TempDir(), spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	sp := newSP(t, []spResult{{p: spNotes(t)}}, st)
	sp.approveAfterSeed(t)
	d := &durableRig{t: t, sp: sp, st: st, runs: &fakeRuns{exists: map[string]bool{}},
		w: &fakeStepWorker{st: st, data: map[string][]byte{}}, c: &fakeStepChecker{}, wk: NewWorkers()}
	x := &closingRig{durableRig: d}
	x.cr = &closingRuns{fakeRuns: d.runs, answer: func(n int) (bool, string, error) { return answer(x, n) }}
	projects := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d.r = newRig(t, func(cfg *Config) {
		cfg.Context = ctx
		cfg.Slices, cfg.Store = sp.svc, st
		cfg.WorkerGrants = stepGrants("S-1")
		cfg.Projects = projects
		cfg.Model = "test-model"
		cfg.StepWorker, cfg.StepChecker = d.w, d.c
		cfg.Workers = d.wk
		cfg.Runs = x.cr
	})
	registerAll(t, d.r, "S-1")
	return x
}

// startRecorded starts RUN-1, writes its events (RunStarted, then kinds) as its workflow would, and
// lets the fake answer Closed.
func (x *closingRig) startRecorded(kinds ...string) gen.Operation {
	x.t.Helper()
	op := x.opOf(x.do("POST", spURL("S-1", "/runs"), `{"project":"notes-app"}`), 202)
	if op.Target != "RUN-1" {
		x.t.Fatalf("setup: the run is %s, want RUN-1", op.Target)
	}
	x.record(kinds...)
	x.cr.arm()
	return op
}

// appendKind appends one event to RUN-1's record as its workflow would.
func (x *closingRig) appendKind(kind, detail string) {
	x.t.Helper()
	n := len(x.events())
	e := storedEvent{Event: machineEvent(n+1, time.Date(2026, 11, 19, 13, 0, 0, 0, time.UTC), kind, detail)}
	data, _ := json.Marshal(e)
	if _, err := x.st.Append("S-1", store.Event{Run: "RUN-1", Kind: kind, Data: store.RawJSON(data)}); err != nil {
		x.t.Fatal(err)
	}
}

func (x *closingRig) events() []machine.Event {
	x.t.Helper()
	ev, _, err := readRun(x.st, "RUN-1")
	if err != nil {
		x.t.Fatal(err)
	}
	return ev
}

func kindsOf(ev []machine.Event) []string {
	var k []string
	for _, e := range ev {
		k = append(k, e.Kind)
	}
	return k
}

// waitOp follows the operation until it has the status, for at most 12 s (a few polls).
func (x *closingRig) waitOp(id string, want gen.OperationStatus) gen.Operation {
	x.t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for {
		x.r.clock.Advance(1100 * time.Millisecond) // stay under the rate limit
		op := x.r.do("GET", "/operations/"+id+"?api-version="+apiVersion, clientToken, "", nil).operation(x.t, 200)
		if op.Status == want {
			return op
		}
		if time.Now().After(deadline) {
			x.t.Fatalf("operation %s is %q after 12s, want %q; record %v; Closed asked %d times",
				id, op.Status, want, kindsOf(x.events()), x.cr.asked())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (x *closingRig) wantFailedWith(op gen.Operation, detail string) {
	x.t.Helper()
	if op.Error == nil || op.Error.Code != gen.ErrorCodeOperationFailed || op.Error.Detail == nil || *op.Error.Detail != detail {
		var got string
		if op.Error != nil && op.Error.Detail != nil {
			got = *op.Error.Detail
		}
		x.t.Fatalf("operation error = %+v (detail %q), want code OperationFailed with detail %q", op.Error, got, detail)
	}
}

// wantOneEnd checks that the record is want (kinds), has exactly one end event, its seqs follow
// each other from 1 and it reads as ended.
func (x *closingRig) wantRecord(want ...string) []machine.Event {
	x.t.Helper()
	ev := x.events()
	if got := kindsOf(ev); strings.Join(got, ",") != strings.Join(want, ",") {
		x.t.Fatalf("record = %v, want %v", got, want)
	}
	ends := 0
	for i, e := range ev {
		if e.Seq != i+1 || e.Run != "RUN-1" || e.At.IsZero() {
			x.t.Errorf("event %d = %+v, want seq %d of RUN-1 with a time", i, e, i+1)
		}
		switch e.Kind {
		case "RunCompleted", "HandOverAccepted", "RunExpired", "RunCancelled":
			ends++
		}
	}
	if ends != 1 {
		x.t.Errorf("the record holds %d end events, want exactly 1: %v", ends, want)
	}
	return ev
}

func TestARunWhoseWorkflowClosedWithNoEndIsEndedCancelledAndItsOperationFails(t *testing.T) {
	const prefix = "the run's workflow failed: "
	for _, tc := range []struct {
		name   string
		kinds  []string // events after RunStarted
		answer func() (bool, string, error)
		detail string
	}{
		{"failed with a reason", nil, func() (bool, string, error) {
			return true, "run RUN-1: the run record could not be written: disk full", nil
		}, prefix + "run RUN-1: the run record could not be written: disk full"},
		{"closed with no reason", nil, func() (bool, string, error) { return true, "", nil }, prefix + "it closed with no end"},
		{"workflow no longer found (retention)", nil, func() (bool, string, error) {
			return false, "", fmt.Errorf("fake durable service: run RUN-1: %w", ErrNotFound)
		}, prefix + "it closed with no end"},
		{"interrupted run", []string{"RunInterrupted=the model is unavailable"}, func() (bool, string, error) {
			return true, "boom", nil
		}, prefix + "boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := newClosingRig(t, func(_ *closingRig, _ int) (bool, string, error) { return tc.answer() })
			op := x.startRecorded(tc.kinds...)
			got := x.waitOp(op.Id, gen.OperationStatusFailed)
			x.wantFailedWith(got, tc.detail)

			want := []string{"RunStarted"}
			for _, k := range tc.kinds {
				kind, _, _ := strings.Cut(k, "=") // "Kind=detail": the record's kinds are compared
				want = append(want, kind)
			}
			ev := x.wantRecord(append(want, "RunCancelled")...)
			if last := ev[len(ev)-1]; last.Detail != tc.detail {
				t.Errorf("RunCancelled detail = %q, want %q", last.Detail, tc.detail)
			}
			if st := machine.StateOf(ev); st.Status != machine.StatusEnded || st.Outcome != machine.OutcomeCancelled || st.Reason != tc.detail {
				t.Errorf("the record reads as %+v, want ended, cancelled, reason %q", st, tc.detail)
			}
			v := x.getRunView("RUN-1")
			if v.Status != "ended" || v.Outcome == nil || *v.Outcome != "cancelled" {
				t.Errorf("getRun = status %q outcome %v, want ended, cancelled", v.Status, v.Outcome)
			}
			// The run is over, so a new run is not refused RunActive.
			p := x.do("POST", spURL("S-1", "/runs"), `{"project":"notes-app-2"}`)
			if p.Code == 409 {
				t.Fatalf("a new startRun after the run's end is refused: %s", p.Body.String())
			}
			x.opOf(p, 202)
			// Nothing more was appended to the ended run (one end, once), however often it is asked.
			time.Sleep(100 * time.Millisecond)
			x.wantRecord(append(want, "RunCancelled")...)
		})
	}
}

func (x *closingRig) getRunView(id string) runView {
	x.t.Helper()
	p := x.do("GET", apiPath("/runs/"+id), "")
	if p.Code != 200 {
		x.t.Fatalf("getRun %s = %d: %s", id, p.Code, p.Body.String())
	}
	var v runView
	if err := json.Unmarshal(p.Body.Bytes(), &v); err != nil {
		x.t.Fatal(err)
	}
	return v
}

func TestARunWhoseRecordHasItsEndGetsNothingAppendedWhateverTheWorkflowDid(t *testing.T) {
	for _, tc := range []struct {
		name, end string
		reason    string
		cancelled bool
	}{
		{"completed, workflow closed cleanly", "RunCompleted", "", false},
		{"completed, workflow closed with a late error", "RunCompleted", "a late failure", false},
		{"cancelled, workflow closed", "RunCancelled=the run was cancelled by the developer", "x", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := newClosingRig(t, func(*closingRig, int) (bool, string, error) { return true, tc.reason, nil })
			op := x.startRecorded(tc.end)
			if tc.cancelled {
				got := x.waitOp(op.Id, gen.OperationStatusFailed)
				x.wantFailedWith(got, "the run was cancelled") // as today
			} else {
				got := x.waitOp(op.Id, gen.OperationStatusSucceeded)
				if got.Result == nil || got.Result.Run == nil || *got.Result.Run != "RUN-1" {
					t.Fatalf("result = %+v, want run RUN-1", got.Result)
				}
			}
			kind, _, _ := strings.Cut(tc.end, "=")
			x.wantRecord("RunStarted", kind)
		})
	}
}

func TestTheEndThatLandsWhileClosedIsAskedIsKeptAndNothingIsAppended(t *testing.T) {
	for _, tc := range []struct{ name, kind, detail string }{
		{"completed", "RunCompleted", ""},
		{"cancelled", "RunCancelled", "the run was cancelled by the developer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := newClosingRig(t, func(x *closingRig, n int) (bool, string, error) {
				if n == 1 { // the workflow's last write lands as it closes
					x.appendKind(tc.kind, tc.detail)
				}
				return true, "", nil
			})
			op := x.startRecorded()
			if tc.kind == "RunCancelled" {
				x.wantFailedWith(x.waitOp(op.Id, gen.OperationStatusFailed), "the run was cancelled")
			} else {
				x.waitOp(op.Id, gen.OperationStatusSucceeded)
			}
			ev := x.wantRecord("RunStarted", tc.kind)
			if ev[1].Detail != tc.detail {
				t.Errorf("%s detail = %q, want the workflow's %q, not the engine's", tc.kind, ev[1].Detail, tc.detail)
			}
		})
	}
}

func TestARunIsLeftAloneWhileItsWorkflowRunsOrTheDurableServiceDoesNotAnswer(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first func() (bool, string, error) // the first two answers
	}{
		{"unavailable", func() (bool, string, error) { return false, "", fmt.Errorf("fake: %w", ErrUnavailable) }},
		{"not closed", func() (bool, string, error) { return false, "", nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := newClosingRig(t, func(_ *closingRig, n int) (bool, string, error) {
				if n <= 2 {
					return tc.first()
				}
				return true, "the service came back", nil
			})
			op := x.startRecorded()
			deadline := time.Now().Add(12 * time.Second)
			for x.cr.asked() < 2 {
				if time.Now().After(deadline) {
					t.Fatalf("Closed was asked %d times in 12 s, want at least 2 (the operation polls)", x.cr.asked())
				}
				time.Sleep(5 * time.Millisecond)
			}
			// Two answers were "not closed" or an error: the run is still running, nothing appended.
			if got := x.waitOp(op.Id, gen.OperationStatusRunning); got.Status != gen.OperationStatusRunning {
				t.Fatalf("operation = %q", got.Status)
			}
			if got := kindsOf(x.events()); strings.Join(got, ",") != "RunStarted" {
				t.Fatalf("record = %v, want only RunStarted while the answer is not closed", got)
			}
			// Once it answers closed, the run ends once.
			x.wantFailedWith(x.waitOp(op.Id, gen.OperationStatusFailed), "the run's workflow failed: the service came back")
			x.wantRecord("RunStarted", "RunCancelled")
		})
	}
}
