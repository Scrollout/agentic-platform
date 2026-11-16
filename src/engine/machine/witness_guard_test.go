package machine_test

// T076, review round 1: guards of the witnesses' commits (data-model, "A review round"; FR-017),
// by the test author. A restart after an open review round rebuilds its files and its pending
// commit from the StepRejected event, so the fix starts from the round's witnesses; a reused
// review commits its witnesses output.

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
)

type wfix struct {
	*fixture
	h       *history
	log     *oplog
	sk      loggedSink
	results *machine.Results
}

// witnessFixture: every step leaves one file; the review also writes the witness; with open, review
// round 1 finds R-1 with that witness (the fix then keeps it).
func witnessFixture(t *testing.T, results *machine.Results, open bool) *wfix {
	t.Helper()
	p := witnessPatternOf()
	f := newFixture(t, p)
	log := &oplog{}
	x := &wfix{fixture: f, h: &history{log: log}, log: log, sk: loggedSink{f.ev, log}, results: results}
	witness := machine.File{Path: witnessPath, SHA256: hash("witness")}
	for _, s := range p.Steps {
		name := s.Name
		f.w.fn[name] = func(r machine.Request) (machine.Result, error) {
			f.clk.Advance(time.Second)
			res := ok(r)
			for o := range res.Outputs {
				res.Outputs[o] = hash(name + "/" + o)
			}
			files := slices.Clone(r.Before)
			if !hasFile(files, name+"/f.go") {
				files = append(files, machine.File{Path: name + "/f.go", SHA256: hash("file/" + name)})
			}
			if name == "review" && !hasFile(files, witnessPath) {
				files = append(files, witness)
			}
			slices.SortFunc(files, func(a, b machine.File) int {
				if a.Path < b.Path {
					return -1
				} else if a.Path > b.Path {
					return 1
				}
				return 0
			})
			res.Before, res.ProjectFiles = r.Before, files
			if name == "review" && open && r.Round == 1 {
				fd := blocking("R-1", "code/f.go", 1)
				fd.Witness = &machine.Witness{File: witnessPath, Test: witnessName}
				res.Findings = []machine.Finding{fd}
			}
			return res, nil
		}
	}
	f.c.fn = noBlocking
	f.m = x.build()
	return x
}

func (x *wfix) build() *machine.Machine {
	return machine.New(machine.Config{Pattern: witnessPatternOf(), Worker: loggedWorker{x.w, x.log}, Checker: x.c,
		Clock: x.clk, Events: x.sk, Results: x.results, Model: "claude-haiku-5-5", History: x.h, Sleep: x.h.sleep})
}

func (x *wfix) startIn(id, project string) machine.Run {
	x.t.Helper()
	p := x.params(id)
	p.Project = project
	r, err := x.m.Start(context.Background(), p)
	if err != nil {
		x.t.Fatalf("Start(%s): %v", id, err)
	}
	return r
}

// M4: a restart right after an open review round's StepRejected rebuilds the round's files and its
// pending commit from that event: the first call on resume commits the round with its witness,
// and the fix's restore starts from it.
func TestResumeAfterAnOpenReviewRoundKeepsItsWitnesses(t *testing.T) {
	orig := witnessFixture(t, nil, true)
	assertEnded(t, orig.startIn("RUN-1", "proj-a"), orig.ev, "done")
	var cut []machine.Event
	for _, e := range orig.ev.events {
		cut = append(cut, e)
		if e.Kind == "StepRejected" && e.Step == "review" && e.Round == 1 {
			break
		}
	}
	if last := cut[len(cut)-1]; last.Kind != "StepRejected" || !hasFile(last.Files, witnessPath) {
		t.Fatalf("the open round's StepRejected does not carry the witness: %+v", last)
	}

	x := witnessFixture(t, nil, true)
	x.ev.events = slices.Clone(cut)
	x.clk.now = cut[len(cut)-1].At.Add(time.Minute)
	m := x.build() // a restarted engine reading the record
	r, err := m.Resume(context.Background(), "RUN-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEnded(t, r, x.ev, "done")
	if len(x.h.commits) == 0 || x.h.commits[0].Step.Name != "review" || x.h.commits[0].Round != 1 ||
		!hasFile(x.h.commits[0].ProjectFiles, witnessPath) {
		t.Fatalf("the first call on resume is not review round 1's commit with its witness: %+v", x.h.commits)
	}
	if !x.h.commits[0].OpenRound {
		t.Fatalf("the open round's commit rebuilt on resume does not send open_round: %+v", x.h.commits[0])
	}
	fixes := x.h.restoresOf("code")
	if len(fixes) != 1 || !hasFile(fixes[0].Before, witnessPath) {
		t.Fatalf("the fix's restore after resume does not start from the witness: %+v", fixes)
	}
	if x.log.index("commit review") > x.log.index("restore code 1 1") {
		t.Errorf("the fix was restored before the round's commit: %v", x.log.all())
	}
}

// M8b: a reused review whose outputs are [findings, witnesses] commits its witnesses output (a set
// of project files), never its findings.
func TestReusedReviewCommitsItsWitnesses(t *testing.T) {
	results := machine.NewResults()
	first := witnessFixture(t, results, false)
	assertEnded(t, first.startIn("RUN-1", "proj-a"), first.ev, "done")
	second := witnessFixture(t, results, false)
	assertEnded(t, second.startIn("RUN-2", "proj-b"), second.ev, "done")
	if len(second.w.calls) != 0 {
		t.Fatalf("the second run ran %d attempts, want every step reused", len(second.w.calls))
	}
	rc := second.h.commitsOf("review")
	if len(rc) != 1 {
		t.Fatalf("review commits = %d, want 1", len(rc))
	}
	want := []machine.Artifact{{Name: "witnesses", SHA256: hash("review/witnesses")}}
	if !slices.Equal(rc[0].Outputs, want) {
		t.Fatalf("the reused review commits outputs %v, want %v", rc[0].Outputs, want)
	}
	if !hasFile(rc[0].ProjectFiles, witnessPath) {
		t.Errorf("the reused review's commit lacks the witness: %v", rc[0].ProjectFiles)
	}
}
