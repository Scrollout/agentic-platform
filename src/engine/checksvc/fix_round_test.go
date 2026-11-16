package checksvc_test

// T083 (owner, 2026-11-14): the check contract's `round` also names the fix between review
// rounds (the code step run again with the open findings), which carries the round whose findings
// it fixes. The service takes a round on a call of the code step: runCheck, restoreStep and
// commitStep (its `Round: <n>` trailer). Written before the code by a separate author.

import (
	"testing"
)

func TestRunCheckOfTheCodeStepTakesARound(t *testing.T) {
	root, dir := fixture(t, "testspass")
	base := serve(t, checkConfig(t, root))
	st := step("code", codeWritable, []string{"code"}, nil, nil)
	write(t, dir, "internal/x.go", []byte("package internal\n"))
	before := listFiles(t, dir)
	for _, round := range []int{1, 2, 3} {
		b := checkBody(t, dir, st, before, "c", "e")
		b["round"] = round
		r := post(t, base, "/checks/ScopeKept/run", b)
		if r.status != 200 {
			t.Fatalf("a runCheck of the code step with round %d: want 200, got %d: %.300s", round, r.status, r.body)
		}
		if v := wantVerdict(t, r, "ScopeKept"); !v.Passed {
			t.Errorf("round %d: %s", round, v.Reason)
		}
	}
}

func TestCommitAndRestoreOfTheFixCarryTheRound(t *testing.T) {
	w := newPW(t)
	v1, _ := w.firstStep(t)
	code := step("code", codeWritable, []string{"code"}, nil, nil)

	// The code step's own commit, then a fix after review round 1 with the same attempt number.
	write(t, w.dir, "internal/greet.go", []byte("package internal\n"))
	b2 := commitBody(code, v1.ProjectFiles, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:10:00Z")
	v2 := wantVersion(t, post(t, w.base, commitPath, b2))
	wantCommitObject(t, w.dir, v2.Commit, v1.Commit, "2026-11-13T10:10:00Z", commitMessage("RUN-1", "code", 0, 1, ""))

	r := post(t, w.base, restorePath, func() map[string]any { b := restoreBody(code, v2.ProjectFiles); b["round"] = 1; return b }())
	if r.status != 200 {
		t.Fatalf("restoreStep of the fix with round 1: want 200, got %d: %.300s", r.status, r.body)
	}
	write(t, w.dir, "internal/greet.go", []byte("package internal\n\nconst Fixed = 1\n"))
	b3 := commitBody(code, v2.ProjectFiles, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:20:00Z")
	b3["round"] = 1
	v3 := wantVersion(t, post(t, w.base, commitPath, b3))
	if !v3.Created || v3.Commit == v2.Commit {
		t.Fatalf("the fix (round 1, attempt 1) is not a repeat of the code step (attempt 1): %+v", v3)
	}
	wantCommitObject(t, w.dir, v3.Commit, v2.Commit, "2026-11-13T10:20:00Z", commitMessage("RUN-1", "code", 1, 1, ""))
	if n := commitCount(t, w.dir); n != 3 {
		t.Errorf("want 3 commits, got %d", n)
	}

	// A repeat of the fix is the same commit; the same attempt in round 2 is a new one.
	again := wantVersion(t, post(t, w.base, commitPath, b3))
	if again.Created || again.Commit != v3.Commit {
		t.Errorf("a repeat of the fix: %+v, want %s with created false", again, v3.Commit)
	}
}
