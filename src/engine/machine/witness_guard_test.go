package machine_test

// T076, review round 1: guards of the witnesses' commits (data-model, "A review round"; FR-017),
// by the test author. A reused review commits its witnesses output. (A stop during an open
// round's commit: workflow_moved_test.go.)

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
