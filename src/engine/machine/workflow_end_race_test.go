package machine_test

// T071: a run's end event is the last event of its record. A cancel, or a platform stop (an
// activity ending with a heartbeat timeout), that lands while the end event is being written must
// not add an event after it, and the workflow's result is the outcome the record gives (FR-006,
// Principle X). The interface is in workflow_durable_test.go.

import (
	"slices"
	"testing"

	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	"go.temporal.io/sdk/temporal"
)

var wfEndKinds = []string{"RunCompleted", "HandOverAccepted", "RunExpired", "RunCancelled"}

// wfEndRaceCase is one end event and how to bring a run to it.
type wfEndRaceCase struct {
	kind  string
	want  machine.Outcome
	setup func(h *wfHarness) // makes the run reach the end event on its own
}

func wfEndRaceCases() []wfEndRaceCase {
	handOver := func(h *wfHarness) { // the code step's checks fail: the run hands over and waits
		failing := true
		h.check = wfFailCode(&failing)
	}
	return []wfEndRaceCase{
		{"RunCompleted", machine.OutcomeDone, func(h *wfHarness) {}},
		{"HandOverAccepted", machine.OutcomeHandedOver, func(h *wfHarness) {
			handOver(h)
			h.signalAt(wfDay, durable.SignalAcceptHandOver, nil)
		}},
		{"RunExpired", machine.OutcomeExpired, handOver}, // nothing arrives: 14 days pass
	}
}

func wfIsWrite(kind string) func(any) bool {
	return func(in any) bool { e, ok := in.(durable.EventRecord); return ok && e.Event.Kind == kind }
}

// wfOnWrite is a hook for h.fail: act runs once, in the RecordEvent activity that writes an event
// of kind, before the write takes effect; its error is the activity's.
func wfOnWrite(kind string, act func() error) func(string, any) error {
	is, done := wfIsWrite(kind), false
	return func(name string, in any) error {
		if name != durable.RecordEvent || !is(in) || done {
			return nil
		}
		done = true
		return act()
	}
}

// wfWantSingleEnd fails unless the record holds exactly one end event, it is the last event, the
// record's state is that outcome, and the workflow returned it without an error.
func wfWantSingleEnd(t *testing.T, h *wfHarness, kind string, want machine.Outcome) {
	t.Helper()
	ev := h.events()
	if len(ev) == 0 {
		t.Fatal("no events")
	}
	ends := 0
	for _, e := range ev {
		if slices.Contains(wfEndKinds, e.Kind) {
			ends++
		}
	}
	if ends != 1 {
		t.Errorf("%d end events, want exactly 1: %v", ends, h.kinds())
	}
	if last := ev[len(ev)-1].Kind; last != kind {
		t.Errorf("the record's last event is %s, want the end event %s: %v", last, kind, h.kinds())
	}
	if st := machine.StateOf(ev); st.Status != machine.StatusEnded || st.Outcome != want {
		t.Errorf("record state = %q/%q, want ended/%q: %v", st.Status, st.Outcome, want, h.kinds())
	}
	if got := h.outcome(); got != want {
		t.Errorf("workflow outcome = %q, want %q: %v", got, want, h.kinds())
	}
	wfValidate(t, ev)
}

// (a) A cancel that lands while the end event is written: the run ended with that event.
func TestEndRaceCancelDuringEndEventWrite(t *testing.T) {
	for _, tc := range wfEndRaceCases() {
		t.Run(tc.kind, func(t *testing.T) {
			h := newWF(t, wfPattern())
			tc.setup(h)
			h.fail = wfOnWrite(tc.kind, func() error { h.env.SignalWorkflow(durable.SignalCancel, nil); return nil })
			h.run()
			wfWantSingleEnd(t, h, tc.kind, tc.want)
		})
		// The write has not landed when the cancel arrives: the first call fails after the cancel,
		// the write is made again and lands, and it is still the end.
		t.Run(tc.kind+" write fails once after the cancel", func(t *testing.T) {
			h := newWF(t, wfPattern())
			tc.setup(h)
			h.fail = wfOnWrite(tc.kind, func() error {
				h.env.SignalWorkflow(durable.SignalCancel, nil)
				return errWFUnreachable
			})
			h.run()
			wfWantSingleEnd(t, h, tc.kind, tc.want)
		})
	}
}

// (a) The write lands, then the cancel arrives before the workflow takes the activity's answer.
func TestEndRaceCancelAfterEndEventWriteLanded(t *testing.T) {
	for _, tc := range wfEndRaceCases() {
		t.Run(tc.kind, func(t *testing.T) {
			h := newWF(t, wfPattern())
			tc.setup(h)
			is, sent := wfIsWrite(tc.kind), false
			h.stopAfter = func(name string, in any) bool { // never stops: it only signals after the write
				if name == durable.RecordEvent && is(in) && !sent {
					sent = true
					h.env.SignalWorkflow(durable.SignalCancel, nil)
				}
				return false
			}
			h.run()
			wfWantSingleEnd(t, h, tc.kind, tc.want)
		})
	}
}

// (b) A stop (heartbeat timeout) during the end event's write, before the write takes effect or
// after: the write is made again with the same seq, and nothing follows it.
func TestEndRaceStopDuringEndEventWrite(t *testing.T) {
	for _, tc := range wfEndRaceCases() {
		for _, after := range []bool{false, true} {
			name := tc.kind + " stop before the write"
			if after {
				name = tc.kind + " stop after the write"
			}
			t.Run(name, func(t *testing.T) {
				h := newWF(t, wfPattern())
				tc.setup(h)
				hook := wfFirst(durable.RecordEvent, 1, wfIsWrite(tc.kind))
				if after {
					h.stopAfter = hook
				} else {
					h.stop = hook
				}
				h.run()
				wfWantSingleEnd(t, h, tc.kind, tc.want)
				if n := h.stopPairs(); n != 0 {
					t.Errorf("%d stop pairs recorded for a stop during the end event's write, want none: %v", n, h.kinds())
				}
			})
		}
	}
}

// A stop and a cancel in the same write: still one end event, last.
func TestEndRaceStopAndCancelDuringEndEventWrite(t *testing.T) {
	for _, tc := range wfEndRaceCases() {
		t.Run(tc.kind, func(t *testing.T) {
			h := newWF(t, wfPattern())
			tc.setup(h)
			h.fail = wfOnWrite(tc.kind, func() error {
				h.env.SignalWorkflow(durable.SignalCancel, nil)
				return temporal.NewHeartbeatTimeoutError()
			})
			h.run()
			wfWantSingleEnd(t, h, tc.kind, tc.want)
		})
	}
}

// The expiry timer and the accept-hand-over signal on the same instant (14 days after the
// hand-over): whichever the workflow takes, the run ends once, with that event last.
func TestEndRaceExpiryAndAcceptHandOverTogether(t *testing.T) {
	h := newWF(t, wfPattern())
	failing := true
	h.check = wfFailCode(&failing)
	h.signalAt(14*wfDay, durable.SignalAcceptHandOver, nil)
	h.run()
	ev := h.events()
	last := ev[len(ev)-1].Kind
	want := map[string]machine.Outcome{"RunExpired": machine.OutcomeExpired, "HandOverAccepted": machine.OutcomeHandedOver}[last]
	if want == "" {
		t.Fatalf("the record ends %s, want RunExpired or HandOverAccepted: %v", last, h.kinds())
	}
	wfWantSingleEnd(t, h, last, want)
}

// Control: a cancel during a write that is not an end event ends the run with exactly one
// RunCancelled, last.
func TestEndRaceCancelDuringOtherEventWriteEndsCancelled(t *testing.T) {
	for _, kind := range []string{"StepStarted", "AttemptMade", "StepAccepted", "RunHandedOver"} {
		t.Run(kind, func(t *testing.T) {
			h := newWF(t, wfPattern())
			failing := true
			h.check = wfFailCode(&failing) // so that RunHandedOver is reached
			h.fail = wfOnWrite(kind, func() error { h.env.SignalWorkflow(durable.SignalCancel, nil); return nil })
			h.run()
			wfWantSingleEnd(t, h, "RunCancelled", machine.OutcomeCancelled)
		})
	}
}
