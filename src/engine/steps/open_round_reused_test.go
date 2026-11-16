package steps_test

// T088 (5) (owner, 2026-11-15; research D36, ADR 0040; check contract CommitRequest.open_round, as
// widened by T089): the history client sends `open_round: true` with `round`, `reused_from` and
// `outputs` (the witnesses) for a reused open review round, and still sends no `open_round` for a
// fix or a reused accepted step. Written before the code by a separate test author.

import (
	"errors"
	"testing"
	"time"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/machine"
)

func reviewWithWitnesses() machine.Step {
	return machine.Step{Name: "review", Worker: "agent:review", Outputs: []string{"findings", "witnesses"},
		Writable: []string{"review/**"}, Checks: []string{"FindingsCarryEvidence", "NoBlockingFindingOpen"}}
}

func TestHistoryCommitOfAReusedOpenRoundSendsOpenRoundWithReusedFrom(t *testing.T) {
	f := newVersions(t)
	framed := canonical.Framed([]canonical.File{{Path: "review/r1/name_test.go", Content: []byte("package r1\n")}})
	h := hex256(framed)
	load := func(want string) ([]byte, error) {
		if want != h {
			return nil, errors.New("not stored")
		}
		return framed, nil
	}
	at := time.Date(2026, 11, 12, 11, 5, 0, 0, time.UTC)
	if _, err := historyFor(f, load).Commit(ctx10(t), machine.CommitRequest{Run: "RUN-2", Step: reviewWithWitnesses(), Round: 1,
		OpenRound: true, ReusedFrom: "RUN-1", Project: "proj-b", Before: filesBefore, ProjectFiles: filesAfter, AcceptedAt: at,
		Outputs: []machine.Artifact{{Name: "witnesses", SHA256: h}}, TimeLimit: time.Minute}); err != nil {
		t.Fatal(err)
	}
	b := f.rec.all()[0].body
	if b["open_round"] != true || b["round"] != float64(1) || b["reused_from"] != "RUN-1" || b["attempt"] != nil {
		t.Fatalf("open_round %v round %v reused_from %v attempt %v; want true, 1, RUN-1 and none",
			b["open_round"], b["round"], b["reused_from"], b["attempt"])
	}
	outs, _ := b["outputs"].([]any)
	if len(outs) != 1 {
		t.Fatalf("outputs = %v, want the witnesses only", b["outputs"])
	}
	if o, _ := outs[0].(map[string]any); o["name"] != "witnesses" || o["sha256"] != h {
		t.Fatalf("output = %v, want witnesses %s", o, h)
	}
}

func TestHistoryCommitOfAFixOrAReusedAcceptedStepSendsNoOpenRound(t *testing.T) {
	framed := canonical.Framed([]canonical.File{{Path: "review/r1/name_test.go", Content: []byte("package r1\n")}})
	h := hex256(framed)
	load := func(string) ([]byte, error) { return framed, nil }
	code := machine.Step{Name: "code", Worker: "agent:code", Outputs: []string{"code"}, Writable: []string{"internal/**"},
		Checks: []string{"ScopeKept", "AllTestsPass"}}
	for name, rq := range map[string]machine.CommitRequest{
		"the fix of round 1": {Run: "RUN-1", Step: code, Attempt: 1, Round: 1},
		"a reused review round accepted": {Run: "RUN-2", Step: reviewWithWitnesses(), Round: 2, ReusedFrom: "RUN-1",
			Outputs: []machine.Artifact{{Name: "witnesses", SHA256: h}}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newVersions(t)
			rq.Project, rq.Before, rq.ProjectFiles = "proj-a", filesBefore, filesAfter
			rq.AcceptedAt, rq.TimeLimit = time.Date(2026, 11, 12, 11, 0, 0, 0, time.UTC), time.Minute
			if _, err := historyFor(f, load).Commit(ctx10(t), rq); err != nil {
				t.Fatal(err)
			}
			if v, ok := f.rec.all()[0].body["open_round"]; ok {
				t.Fatalf("open_round %v sent for %s", v, name)
			}
		})
	}
}
