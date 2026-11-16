package machine_test

// T053e: a stored result with project files is reused in a new project, since commitStep writes
// the reused outputs' stored artifacts there (the T053a interim guard is lifted); a replay of a
// finished run gives the original's artifacts and project files for every step (US2.8, SC-005).
// The seam is described in history_test.go.

import (
	"slices"
	"testing"

	"github.com/agentic-platform/engine/machine"
)

func TestResultWithFilesIsReusedInANewProjectAndCommittedWithItsArtifacts(t *testing.T) {
	results := machine.NewResults()
	first := historyFixture(t, results)
	assertEnded(t, first.startIn("RUN-1", "proj-a"), first.ev, "done")

	second := historyFixture(t, results)
	assertEnded(t, second.startIn("RUN-2", "proj-b"), second.ev, "done")
	if len(second.w.calls) != 0 || len(second.h.restores) != 0 {
		t.Fatalf("the new project ran %d attempts and %d restores, want none: every step is reused", len(second.w.calls), len(second.h.restores))
	}
	reused := accepts(second.ev)
	if len(reused) != 5 || second.ev.count("ResultReused") != 5 {
		t.Fatalf("events = %v, want 5 ResultReused", second.ev.kinds())
	}
	if len(second.h.commits) != 5 {
		t.Fatalf("commits = %d, want one per reused step", len(second.h.commits))
	}
	var before []machine.File
	for i, e := range reused {
		c := second.h.commits[i]
		s := pattern().Steps[i]
		var want []machine.Artifact
		if !s.Reviews() {
			for j, o := range s.Outputs {
				want = append(want, machine.Artifact{Name: o, SHA256: e.Artifacts[j]})
			}
		}
		switch {
		case c.Step.Name != e.Step || c.Run != "RUN-2" || c.Project != "proj-b":
			t.Errorf("commit %d = %s/%s in %s, want RUN-2/%s in proj-b", i, c.Run, c.Step.Name, c.Project, e.Step)
		case c.ReusedFrom != "RUN-1" || c.Attempt != 0:
			t.Errorf("commit %s reused_from %q attempt %d, want RUN-1 and no attempt", e.Step, c.ReusedFrom, c.Attempt)
		case !c.AcceptedAt.Equal(e.At):
			t.Errorf("commit %s accepted_at = %v, want the ResultReused's at %v", e.Step, c.AcceptedAt, e.At)
		case len(c.Outputs) != len(want) || (len(want) > 0 && !slices.Equal(c.Outputs, want)):
			t.Errorf("commit %s outputs = %v, want the stored artifacts %v", e.Step, c.Outputs, want)
		case !sameFiles(c.ProjectFiles, filesAfter(pattern(), e.Step)) || !sameFiles(c.Before, before):
			t.Errorf("commit %s before %v project_files %v, want %v then %v", e.Step, c.Before, c.ProjectFiles, before, filesAfter(pattern(), e.Step))
		}
		before = c.ProjectFiles
	}
	validate(t, second.ev.events)
}

func TestReplayOfAFinishedRunGivesTheOriginalsArtifactsAndProjectFiles(t *testing.T) {
	results := machine.NewResults()
	orig := historyFixture(t, results)
	assertEnded(t, orig.startIn("RUN-1", "proj-a"), orig.ev, "done")
	replay := historyFixture(t, results)
	assertEnded(t, replay.startIn("RUN-2", "proj-b"), replay.ev, "done")

	a, b := accepts(orig.ev), accepts(replay.ev)
	if len(a) != 5 || len(b) != 5 {
		t.Fatalf("accepted steps: original %d, replay %d; want 5 each", len(a), len(b))
	}
	for i := range a {
		if b[i].Kind != "ResultReused" || b[i].Step != a[i].Step {
			t.Fatalf("replay event %d = %s %s, want ResultReused %s", i, b[i].Kind, b[i].Step, a[i].Step)
		}
		if !slices.Equal(a[i].Artifacts, b[i].Artifacts) {
			t.Errorf("step %s artifacts: original %v, replay %v", a[i].Step, a[i].Artifacts, b[i].Artifacts)
		}
		if !sameFiles(a[i].Files, b[i].Files) {
			t.Errorf("step %s project files: original %v, replay %v", a[i].Step, a[i].Files, b[i].Files)
		}
	}
	if len(orig.h.commits) != 5 || len(replay.h.commits) != 5 {
		t.Fatalf("commits: original %d, replay %d; want 5 each", len(orig.h.commits), len(replay.h.commits))
	}
	for i := range orig.h.commits {
		o, r := orig.h.commits[i], replay.h.commits[i]
		if !sameFiles(o.ProjectFiles, r.ProjectFiles) || !sameFiles(o.Before, r.Before) {
			t.Errorf("step %s commit: original before %v files %v, replay before %v files %v",
				o.Step.Name, o.Before, o.ProjectFiles, r.Before, r.ProjectFiles)
		}
	}
	if d := machine.Compare(orig.ev.events, replay.ev.events); len(d) != 0 {
		t.Errorf("Compare finds differences between the original and its replay: %+v", d)
	}
}
