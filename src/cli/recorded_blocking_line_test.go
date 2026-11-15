// T096 (owner, 2026-11-15; research D39, ADR 0042; contract.md "Event lines"): a FindingRecorded
// line prints `blocking` or `suggestion` from the finding's recorded `blocking`, which is the
// check's verdict, not the agent's claim: an unconfirmed claim, recorded with `blocking: false`,
// prints `suggestion` although it carries a witness. Written before the code by a separate test
// author. Seams: runs_fake_test.go, open_round_line_test.go.
package cli_test

import (
	"testing"
)

func TestRecordedBlockingDecidesTheFindingLine(t *testing.T) {
	e := newEng(t)
	run := "RUN-7"
	evs := []map[string]any{
		ev(1, run, "RunStarted", map[string]any{"slice": "S-1", "pattern": pattern}),
		ev(2, run, "FindingRecorded", map[string]any{"step": "review", "attempt": 1, "round": 1, "refs": []any{"R-1"},
			"finding": map[string]any{"rule": "R-1", "file": "src/a.go", "line": 3, "text": "the refund is not checked",
				"blocking": true, "witness": map[string]any{"file": "review/r1/refund_test.go", "test": "TestR1Refund"}}}),
		ev(3, run, "FindingRecorded", map[string]any{"step": "review", "attempt": 1, "round": 1, "refs": []any{"R-2"},
			"finding": map[string]any{"rule": "R-2", "file": "src/b.go", "line": 7, "text": "the total may overflow",
				"blocking": false, "witness": map[string]any{"file": "review/r2/total_test.go", "test": "TestR2Total"}}}),
		ev(4, run, "RunCompleted", nil),
	}
	want := []string{
		"1  " + at(1) + "  RunStarted  slice S-1  pattern tdd@1.0.0",
		"2  " + at(2) + "  FindingRecorded  review  attempt 1  round 1  blocking  R-1  src/a.go:3  refs R-1  the refund is not checked",
		"3  " + at(3) + "  FindingRecorded  review  attempt 1  round 1  suggestion  R-2  src/b.go:7  refs R-2  the total may overflow",
		"4  " + at(4) + "  RunCompleted",
	}
	fr := &fakeRun{t: t, id: run, op: "OP-12",
		final:   runJSON(runSpec{id: run, status: "ended", outcome: "done"}),
		finalOp: operation("OP-12", "run", "succeeded", run, map[string]any{"run": run}),
		events:  evs, runningReads: 0, pageSize: 10}
	fr.register(e)
	r := e.run("run", run)
	wantExit(t, r, 0)
	wantOut(t, r, lines(append(append([]string{runHeader}, want...), run+" ended done")...))
}
