package checksvc_test

// T088 (4) (owner, 2026-11-15; research D36, ADR 0040; check contract commitStep and
// CommitRequest.open_round, as widened by T089): an open review round reused from an earlier run
// is committed with `open_round: true`, `round`, `reused_from` and `outputs` (its witnesses). The
// project worker writes the witnesses from the outputs, commits with the subject `Step <step> round
// <round> left open in <run>` and the trailers Run, Step, Round and Reused-from; a repeat answers
// created false. Written before the code by a separate test author, from the contract.

import (
	"fmt"
	"sort"
	"testing"
)

const reusedWitness = "review/r1/name_test.go"

var reusedWitnessContent = []byte("package r1\n\nimport \"testing\"\n\nfunc TestR1NameLeftOut(t *testing.T) { t.Fail() }\n")

func reusedOpenRoundMessage(run, stepName string, round int, reusedFrom string) string {
	return fmt.Sprintf("Step %s round %d left open in %s\n\nRun: %s\nStep: %s\nRound: %d\nReused-from: %s\n",
		stepName, round, run, run, stepName, round, reusedFrom)
}

// reusedOpenRoundBody: RUN-2 reuses RUN-1's open review round 1 on top of RUN-2's first commit.
// The witness is not in the folder yet: the call writes it from the outputs.
func (w *pw) reusedOpenRoundBody(t *testing.T, before []pf) map[string]any {
	t.Helper()
	after := append([]pf{}, before...)
	after = append(after, pf{reusedWitness, sum(reusedWitnessContent)})
	sortPF(after)
	f := framed(map[string][]byte{reusedWitness: reusedWitnessContent})
	return map[string]any{
		"run": "RUN-2", "step": reviewWitnessStep(), "round": 1, "open_round": true, "reused_from": "RUN-1",
		"project": pwProject, "before": before, "project_files": after, "accepted_at": "2026-11-13T11:05:00Z",
		"outputs":            []any{map[string]any{"name": "witnesses", "sha256": sum(f), "content": b64(f)}},
		"time_limit_seconds": 120,
	}
}

func sortPF(l []pf) { sort.Slice(l, func(i, j int) bool { return l[i].Path < l[j].Path }) }

// firstStepOf commits contract-check's files as run's first commit.
func (w *pw) firstStepOf(t *testing.T, run string) pwVersion {
	t.Helper()
	write(t, w.dir, "api/openapi.yaml", []byte("openapi: 3.1.0\n"))
	write(t, w.dir, "features/slice.feature", []byte("Feature: F\n"))
	return wantVersion(t, post(t, w.base, commitPath, commitBody(ccStep(), nil, projectFiles(t, w.dir), run, 1, "2026-11-13T11:00:00Z")))
}

func TestReusedOpenRoundCommitWritesTheWitnessesAndSaysTheRoundWasLeftOpen(t *testing.T) {
	w := newPW(t)
	v1 := w.firstStepOf(t, "RUN-2")
	b := w.reusedOpenRoundBody(t, v1.ProjectFiles)
	v := wantVersion(t, post(t, w.base, commitPath, b))
	if !v.Created {
		t.Error("the reused open round's commit is made by this call: created must be true")
	}
	if got := read(t, w.dir, reusedWitness); string(got) != string(reusedWitnessContent) {
		t.Errorf("%s holds %q, want the output's bytes", reusedWitness, got)
	}
	if !samePF(v.ProjectFiles, toPF(b["project_files"])) {
		t.Errorf("project_files = %v, want %v", v.ProjectFiles, b["project_files"])
	}
	wantTree(t, w.dir, v.Commit, contents(t, w.dir))
	wantCommitObject(t, w.dir, v.Commit, v1.Commit, "2026-11-13T11:05:00Z", reusedOpenRoundMessage("RUN-2", "review", 1, "RUN-1"))

	snap := w.state(t)
	r := wantVersion(t, post(t, w.base, commitPath, b))
	if r.Created || r.Commit != v.Commit {
		t.Errorf("a repeat must answer %s with created false: %+v", v.Commit, r)
	}
	w.unchanged(t, snap, "a repeat of the reused open round")
	if n := commitCount(t, w.dir); n != 2 {
		t.Errorf("want 2 commits, got %d", n)
	}
}

func TestReusedOpenRoundOutputWithAnotherHashIsInputHashMismatch(t *testing.T) {
	w := newPW(t)
	v1 := w.firstStepOf(t, "RUN-2")
	b := w.reusedOpenRoundBody(t, v1.ProjectFiles)
	b["outputs"].([]any)[0].(map[string]any)["sha256"] = sum([]byte("another artifact"))
	snap := w.state(t)
	wantProblem(t, post(t, w.base, commitPath, b), 422, "InputHashMismatch")
	w.unchanged(t, snap, "an output whose content does not have its sha256")
}

func TestReusedOpenRoundWithoutRoundOrWithAttemptIsInvalid(t *testing.T) {
	for name, mut := range map[string]func(b map[string]any){
		"open_round without round":                func(b map[string]any) { delete(b, "round") },
		"open_round with attempt and reused_from": func(b map[string]any) { b["attempt"] = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			w := newPW(t)
			v1 := w.firstStepOf(t, "RUN-2")
			b := w.reusedOpenRoundBody(t, v1.ProjectFiles)
			snap := w.state(t)
			mut(b)
			wantProblem(t, post(t, w.base, commitPath, b), 400, "InvalidRequest")
			w.unchanged(t, snap, name)
		})
	}
}
