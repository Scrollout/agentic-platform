package machine_test

// Helpers of T088 (2)'s open-round resume tests (research D36, ADR 0040). T019 and T039 removed
// those tests and the helpers that resumed a run with the old driver (owner, 2026-11-19); their
// cases live in the workflow tests (workflow_*_test.go). round_scope_test.go and
// confirmed_findings_test.go still use these. Seams: open_round_reuse_test.go (orfix and its
// helpers).

import (
	"testing"

	"github.com/agentic-platform/engine/machine"
)

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
