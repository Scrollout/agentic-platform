package machine_test

// T076 (owner, 2026-11-14): the commit of an open review round says so (check contract,
// CommitRequest.open_round; data-model, "A review round" and "Commits"). Written before the code
// by the test author.
//
// ASSUMED SHAPE: CommitRequest gains `OpenRound bool`: true only for an open review round's
// commit, with that round's Attempt and Round, AcceptedAt the StepRejected's at.

import (
	"testing"

	"github.com/agentic-platform/engine/machine"
)

func TestOpenRoundCommitCarriesOpenRound(t *testing.T) {
	x := witnessFixture(t, nil, true)
	assertEnded(t, x.startIn("RUN-1", "proj-a"), x.ev, "done")
	var rejected *machine.Event
	for i, e := range x.ev.events {
		if e.Kind == "StepRejected" && e.Step == "review" && e.Round == 1 {
			rejected = &x.ev.events[i]
		}
	}
	if rejected == nil {
		t.Fatalf("no StepRejected for review round 1: %v", x.ev.kinds())
	}
	var open []machine.CommitRequest
	for _, c := range x.h.commits {
		if c.OpenRound {
			open = append(open, c)
		} else if c.Step.Name == "review" && c.Round == 1 {
			t.Errorf("review round 1's commit does not say the round was left open: %+v", c)
		}
	}
	if len(open) != 1 {
		t.Fatalf("open-round commits = %d, want 1 (review round 1): %+v", len(open), x.h.commits)
	}
	c := open[0]
	if c.Step.Name != "review" || c.Round != 1 || c.Attempt != rejected.Attempt || c.Attempt < 1 || c.ReusedFrom != "" {
		t.Errorf("the open round's commit = %s round %d attempt %d reused_from %q, want review round 1 attempt %d",
			c.Step.Name, c.Round, c.Attempt, c.ReusedFrom, rejected.Attempt)
	}
	if !c.AcceptedAt.Equal(rejected.At) {
		t.Errorf("accepted_at = %v, want the StepRejected's at %v", c.AcceptedAt, rejected.At)
	}
}

func TestReusedResultsCommitWithoutOpenRound(t *testing.T) {
	results := machine.NewResults()
	first := witnessFixture(t, results, false)
	assertEnded(t, first.startIn("RUN-1", "proj-a"), first.ev, "done")
	second := witnessFixture(t, results, false)
	assertEnded(t, second.startIn("RUN-2", "proj-b"), second.ev, "done")
	for _, h := range []*history{first.h, second.h} {
		for _, c := range h.commits {
			if c.OpenRound {
				t.Errorf("an accepted or reused step's commit sends open_round: %+v", c)
			}
		}
	}
	if len(second.h.commits) == 0 || second.h.commits[0].ReusedFrom == "" {
		t.Fatalf("the second run reused nothing: %+v", second.h.commits)
	}
}
