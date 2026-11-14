package checksvc_test

// T076 (owner, 2026-11-14): commitStep records an open review round (check contract, commitStep
// and CommitRequest.open_round; data-model, "A review round" and "Commits"). Written before the
// code by the test author.

import (
	"fmt"
	"testing"
)

func openRoundMessage(run, stepName string, round, attempt int) string {
	return fmt.Sprintf("Step %s round %d left open in %s\n\nRun: %s\nStep: %s\nRound: %d\nAttempt: %d\n",
		stepName, round, run, run, stepName, round, attempt)
}

func reviewWitnessStep() map[string]any {
	return step("review", []string{"review/**"}, []string{"findings", "witnesses"}, nil, nil)
}

// openRoundBody: the review's round 1, attempt 1, left open with a witness, on top of the first step.
func (w *pw) openRoundBody(t *testing.T, before []pf) map[string]any {
	t.Helper()
	write(t, w.dir, "review/r1/name_test.go", []byte("package r1\n"))
	b := commitBody(reviewWitnessStep(), before, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:05:00Z")
	b["round"], b["open_round"] = 1, true
	return b
}

func TestOpenRoundCommitSaysTheRoundWasLeftOpen(t *testing.T) {
	w := newPW(t)
	v1, _ := w.firstStep(t)
	b := w.openRoundBody(t, v1.ProjectFiles)
	v := wantVersion(t, post(t, w.base, commitPath, b))
	if !v.Created {
		t.Error("the open round's commit is made by this call: created must be true")
	}
	wantCommitObject(t, w.dir, v.Commit, v1.Commit, "2026-11-13T10:05:00Z", openRoundMessage("RUN-1", "review", 1, 1))
	wantTree(t, w.dir, v.Commit, contents(t, w.dir))

	// A repeat answers the same commit with created false and changes nothing.
	snap := w.state(t)
	r := wantVersion(t, post(t, w.base, commitPath, b))
	if r.Created || r.Commit != v.Commit {
		t.Errorf("a repeat must answer %s with created false: %+v", v.Commit, r)
	}
	w.unchanged(t, snap, "a repeat of the open round")
	if n := commitCount(t, w.dir); n != 2 {
		t.Errorf("want 2 commits, got %d", n)
	}
}

func TestOpenRoundWithoutRoundOrWithReusedFromIsInvalid(t *testing.T) {
	for name, mut := range map[string]func(b map[string]any){
		"open_round without round": func(b map[string]any) { delete(b, "round") },
		"open_round with reused_from": func(b map[string]any) {
			delete(b, "attempt")
			b["reused_from"] = "RUN-0"
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newPW(t)
			v1, _ := w.firstStep(t)
			b := w.openRoundBody(t, v1.ProjectFiles)
			snap := w.state(t)
			mut(b)
			wantProblem(t, post(t, w.base, commitPath, b), 400, "InvalidRequest")
			w.unchanged(t, snap, name)
		})
	}
}
