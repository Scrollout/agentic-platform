package machine_test

// T058 (fresh review B2; data model "Run record", research R3 "Defects and a store that does not
// answer"): the run workflow's record write. A RecordEvent that fails with anything but SeqTaken (a
// stop and the run's cancellation have their own paths) is retried by the declared retry rule and
// then once a minute until it is written; it never ends the workflow with an error. SeqTaken (the
// record holds that seq with other content) still fails the workflow, at once. Written before the
// code by a test author with the harness of workflow_durable_test.go; time skips.

import (
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/retry"
	"go.temporal.io/sdk/temporal"
)

var errWFRecordDown = errors.New("the run record is unreachable")

// wfRecordCalls are RecordEvent's calls for one seq, in order.
func wfRecordCalls(h *wfHarness, seq int) []wfCall {
	var out []wfCall
	for _, c := range h.callsOf(durable.RecordEvent) {
		if c.In.(durable.EventRecord).Event.Seq == seq {
			out = append(out, c)
		}
	}
	return out
}

func TestWorkflowRecordWriteFailingPastTheRuleIsRetriedEveryMinuteUntilWritten(t *testing.T) {
	doc, err := os.ReadFile("../api/preview/2026-11-12-preview/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := retry.Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	const random = 0.8 // what the mocked side effect gives

	base := newWF(t, wfPattern()) // the same run with a record that always answers
	base.run()
	base.wantOutcome(machine.OutcomeDone)
	wantKinds := base.kinds()

	// Three failures more than the rule's calls (the first and its retries) put some retries past it.
	fails := rule.Retries + 1 + 3
	for _, tc := range []struct {
		name string
		seq  int
	}{
		{"the first event", 1},
		{"a later event", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWF(t, wfPattern())
			h.env.OnSideEffect().Return(random)
			failed := 0
			h.fail = func(name string, in any) error {
				if name == durable.RecordEvent && in.(durable.EventRecord).Event.Seq == tc.seq && failed < fails {
					failed++
					return errWFRecordDown
				}
				return nil
			}
			h.run()
			h.wantOutcome(machine.OutcomeDone) // a workflow error here is the defect: RecordFailed

			calls := wfRecordCalls(h, tc.seq)
			if len(calls) != fails+1 {
				t.Fatalf("seq %d was sent %d times, want %d (%d failures, then the write)", tc.seq, len(calls), fails+1, fails)
			}
			for i, c := range calls {
				if (c.Err != nil) != (i < fails) {
					t.Fatalf("call %d of seq %d: error %v; want the first %d to fail and the last to succeed", i, tc.seq, c.Err, fails)
				}
			}
			for n := 0; n < fails; n++ {
				want := time.Minute // past the rule: once a minute
				if n < rule.Retries {
					want = rule.Wait(n, func() float64 { return random })
				}
				if got := calls[n+1].At.Sub(calls[n].At); got != want {
					t.Fatalf("wait %d before seq %d was sent again was %s, want %s (the rule's %d retries, then one minute)",
						n, tc.seq, got, want, rule.Retries)
				}
			}
			written := 0
			for _, e := range h.events() {
				if e.Seq == tc.seq {
					written++
				}
			}
			if written != 1 {
				t.Errorf("seq %d is on the record %d times, want once", tc.seq, written)
			}
			if got := h.kinds(); !slices.Equal(got, wantKinds) {
				t.Errorf("record = %v, want the run that never failed: %v", got, wantKinds)
			}
			for i, e := range h.events() {
				if e.Seq != i+1 {
					t.Fatalf("event %d has seq %d: a gap or a repeat after the failed writes", i, e.Seq)
				}
			}
		})
	}
}

func TestWorkflowRecordSeqTakenStillFailsTheWorkflowAtOnce(t *testing.T) {
	h := newWF(t, wfPattern())
	const seq = 3
	h.fail = func(name string, in any) error {
		if name == durable.RecordEvent && in.(durable.EventRecord).Event.Seq == seq {
			return temporal.NewNonRetryableApplicationError("seq taken", durable.SeqTaken, nil)
		}
		return nil
	}
	h.run()
	err := h.env.GetWorkflowError()
	var app *temporal.ApplicationError
	if err == nil || !errors.As(err, &app) || app.Type() != durable.SeqTaken {
		t.Fatalf("workflow error = %v, want an application error of type %s", err, durable.SeqTaken)
	}
	if n := len(wfRecordCalls(h, seq)); n != 1 {
		t.Errorf("seq %d was sent %d times, want once: SeqTaken is not retried", seq, n)
	}
	if got := h.events(); len(got) != seq-1 {
		t.Errorf("record holds %d events %v, want the %d before the taken seq", len(got), h.kinds(), seq-1)
	}
}
