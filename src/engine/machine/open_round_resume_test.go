package machine_test

// Helpers of T088 (2)'s open-round resume tests (research D36, ADR 0040), which T019 removed with
// the old driver's resume (owner, 2026-11-19): round_scope_test.go and confirmed_findings_test.go
// still use them. Seams: open_round_reuse_test.go (orfix and its helpers).

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
)

// cutAfter is the record up to and including the first event that last matches.
func cutAfter(t *testing.T, events []machine.Event, last func(machine.Event) bool) []machine.Event {
	t.Helper()
	for i, e := range events {
		if last(e) {
			return slices.Clone(events[:i+1])
		}
	}
	t.Fatalf("setup: no event to cut the record after: %v", kindsOf(events))
	return nil
}

func kindsOf(events []machine.Event) []string {
	var k []string
	for _, e := range events {
		k = append(k, e.Kind+" "+e.Step)
	}
	return k
}

// resumeOn is a restarted engine over the cut record: x's machine is rebuilt over the record and
// resumes the run.
func resumeOn(t *testing.T, x *orfix, cut []machine.Event) machine.Run {
	t.Helper()
	x.ev.events = slices.Clone(cut)
	x.clk.now = cut[len(cut)-1].At.Add(time.Minute)
	x.m = x.build()
	r, err := x.m.Resume(context.Background(), cut[0].Run)
	if err != nil {
		t.Fatalf("Resume(%s): %v", cut[0].Run, err)
	}
	return r
}

func isOpenReused(round int) func(machine.Event) bool {
	return func(e machine.Event) bool {
		return e.Kind == "ResultReused" && e.Step == "review" && e.Round == round
	}
}

func blockingOf(fs []machine.Finding) []machine.Finding {
	var out []machine.Finding
	for _, f := range fs {
		if f.Blocking {
			out = append(out, f)
		}
	}
	return out
}

// fixKeyOf is the reuse key of the fix of review round n in a live run.
func fixKeyOf(t *testing.T, events []machine.Event, n int) string {
	t.Helper()
	for _, e := range events {
		if e.Kind == "StepAccepted" && e.Step == "code" && e.Round == n {
			return e.ReuseKey
		}
	}
	t.Fatalf("setup: no accepted fix of round %d", n)
	return ""
}
