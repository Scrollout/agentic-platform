package machine_test

// T053e (analyze re-check C4): data-model's "A new project" limit relies on the engine's first
// call to the project worker in a run being its first touch: restoreStep for attempt 1 of the
// first step in a fresh run, or a commitStep with `reused_from` in a replay. Seam: history_test.go.

import (
	"strings"
	"testing"

	"github.com/agentic-platform/engine/machine"
)

// firstHistoryCall is the first restore or commit the run made, as the oplog names it.
func firstHistoryCall(t *testing.T, x *hfix) string {
	t.Helper()
	for _, op := range x.log.all() {
		if strings.HasPrefix(op, "restore ") || strings.HasPrefix(op, "commit ") {
			return op
		}
	}
	t.Fatalf("the run made no History call: %v", x.log.all())
	return ""
}

func TestFirstHistoryCallOfAFreshRunIsRestoreForAttemptOne(t *testing.T) {
	x := historyFixture(t, nil)
	assertEnded(t, x.startIn("RUN-1", "proj-a"), x.ev, "done")
	first := pattern().Steps[0].Name
	if op := firstHistoryCall(t, x); op != "restore "+first+" 1 0" {
		t.Fatalf("first History call = %q, want restoreStep for attempt 1 of %s", op, first)
	}
	if r := x.h.restores[0]; r.Attempt != 1 || len(r.Before) != 0 {
		t.Fatalf("first restore = %+v, want attempt 1 with an empty before", r)
	}
}

func TestFirstHistoryCallOfAReplayIsACommitReusedFromTheOriginal(t *testing.T) {
	results := machine.NewResults()
	orig := historyFixture(t, results)
	assertEnded(t, orig.startIn("RUN-1", "proj-a"), orig.ev, "done")
	replay := historyFixture(t, results)
	assertEnded(t, replay.startIn("RUN-2", "proj-b"), replay.ev, "done")
	first := pattern().Steps[0].Name
	if op := firstHistoryCall(t, replay); op != "commit "+first {
		t.Fatalf("first History call of the replay = %q, want commitStep of %s", op, first)
	}
	if c := replay.h.commits[0]; c.ReusedFrom != "RUN-1" || c.Attempt != 0 || len(c.Before) != 0 {
		t.Fatalf("first commit = %+v, want reused_from RUN-1, no attempt, an empty before", c)
	}
}
