package steps_test

// T076 (owner, 2026-11-14): steps.NewHistory sends commitStep's `open_round: true` with the round
// and attempt for an open review round, and never otherwise (check contract, CommitRequest).
// Written before the code by the test author; machine.CommitRequest is assumed to gain
// `OpenRound bool`.

import (
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
)

func TestHistoryCommitOfAnOpenRoundSendsOpenRound(t *testing.T) {
	f := newVersions(t)
	at := time.Date(2026, 11, 12, 10, 5, 0, 0, time.UTC)
	review := machine.Step{Name: "review", Worker: "agent:review", Outputs: []string{"findings", "witnesses"},
		Writable: []string{"review/**"}, Checks: []string{"FindingsCarryEvidence", "NoBlockingFindingOpen"}}
	if _, err := historyFor(f, nil).Commit(ctx10(t), machine.CommitRequest{Run: "RUN-1", Step: review, Attempt: 2, Round: 1,
		OpenRound: true, Project: "proj-a", Before: filesBefore, ProjectFiles: filesAfter, AcceptedAt: at, TimeLimit: time.Minute}); err != nil {
		t.Fatal(err)
	}
	b := f.rec.all()[0].body
	if b["open_round"] != true || b["round"] != float64(1) || b["attempt"] != float64(2) || b["reused_from"] != nil {
		t.Fatalf("open_round %v round %v attempt %v reused_from %v; want true, 1, 2 and none", b["open_round"], b["round"], b["attempt"], b["reused_from"])
	}
}

func TestHistoryCommitOfAnAcceptedStepSendsNoOpenRound(t *testing.T) {
	f := newVersions(t)
	if _, err := historyFor(f, nil).Commit(ctx10(t), machine.CommitRequest{Run: "RUN-1", Step: historyStep(), Attempt: 1,
		Project: "proj-a", Before: filesBefore, ProjectFiles: filesAfter, AcceptedAt: time.Now(), TimeLimit: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if v, ok := f.rec.all()[0].body["open_round"]; ok {
		t.Fatalf("open_round %v sent for an accepted step", v)
	}
}
