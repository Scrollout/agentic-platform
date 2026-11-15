// T088 (6) (owner, 2026-11-15; research D36; contract.md "Event lines", as widened by T089): a
// ResultReused with `open_round` prints `open round` after `reuse <reuse_key>`, and the check's
// reason, then `detail`, as its free text. Written before the code by a separate test author.
// Seams: runs_fake_test.go.
package cli_test

import (
	"testing"
)

func TestResultReusedOfAnOpenRoundPrintsOpenRoundAndTheChecksReason(t *testing.T) {
	e := newEng(t)
	run := "RUN-7"
	evs := []map[string]any{
		ev(1, run, "RunStarted", map[string]any{"slice": "S-1", "pattern": pattern}),
		ev(2, run, "FindingRecorded", map[string]any{"step": "review", "round": 1, "refs": []any{"R-1"},
			"finding": map[string]any{"rule": "R-1", "file": "src/a.go", "line": 3, "text": "the refund is not checked",
				"blocking": true, "check": "AllTestsPass"}}),
		ev(3, run, "ResultReused", map[string]any{"step": "review", "round": 1, "inputs": []any{rhIn1},
			"artifacts": []any{rhArt, rhArt2}, "reuse_key": rhReuse, "open_round": true, "refs": []any{"G-1"},
			"check":  map[string]any{"name": "NoBlockingFindingOpen", "passed": false, "reason": "1 blocking finding open"},
			"detail": "review round 1 left blocking findings open"}),
		ev(4, run, "ResultReused", map[string]any{"step": "code", "round": 1, "artifacts": []any{rhArt3}, "reuse_key": rhReuse2,
			"changed_lines": []any{map[string]any{"file": "src/a.go", "line": 3}}}),
		ev(5, run, "RunCompleted", nil),
	}
	want := []string{
		"1  " + at(1) + "  RunStarted  slice S-1  pattern tdd@1.0.0",
		"2  " + at(2) + "  FindingRecorded  review  round 1  blocking  R-1  src/a.go:3  check AllTestsPass  refs R-1  the refund is not checked",
		"3  " + at(3) + "  ResultReused  review  round 1  artifacts " + rhArt + "," + rhArt2 + "  reuse " + rhReuse +
			"  open round  refs G-1  1 blocking finding open  review round 1 left blocking findings open",
		"4  " + at(4) + "  ResultReused  code  round 1  artifacts " + rhArt3 + "  reuse " + rhReuse2 + "  changed src/a.go:3",
		"5  " + at(5) + "  RunCompleted",
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
