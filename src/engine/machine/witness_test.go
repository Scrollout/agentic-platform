package machine_test

// T076: the machine and the review's witnesses (research D35, ADR 0039), written before the code
// by a separate author. The review step declares [findings, witnesses] and writes review/**. A
// round's findings are recorded with their witness (run-event.schema.json finding.witness); the
// witnesses a round with open findings wrote stay in the project for the fix and the later rounds,
// so the project worker commits them with the review step before the fix starts, and the fix
// starts from them.
//
// ASSUMED SHAPE: Finding gains `Witness *Witness` (json "witness,omitempty"),
// Witness{File, Test string} (json "file", "test").

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/machine"
)

const witnessPath, witnessName = "review/r1/name_test.go", "TestR1NameLeftOut"

func witnessPatternOf() machine.Pattern {
	p := pattern()
	for i := range p.Steps {
		if p.Steps[i].Name == "review" {
			p.Steps[i].Outputs = []string{"findings", "witnesses"}
			p.Steps[i].Writable = []string{"review/**"}
		}
	}
	return p
}

func hasFile(fs []machine.File, path string) bool {
	return slices.ContainsFunc(fs, func(f machine.File) bool { return f.Path == path })
}

// witnessRun: round 1 finds R-1 with a witness and writes it; the fix runs; round 2 finds nothing.
func witnessRun(t *testing.T) (*fixture, *history, *oplog, machine.Run) {
	t.Helper()
	p := witnessPatternOf()
	f := newFixture(t, p)
	log := &oplog{}
	h := &history{log: log}
	f.m = machine.New(machine.Config{Pattern: p, Worker: loggedWorker{f.w, log}, Checker: f.c, Clock: f.clk, Events: f.ev,
		History: h, Sleep: h.sleep})
	f.c.fn = noBlocking
	code := machine.File{Path: "internal/a.go", SHA256: hash("code")}
	witness := machine.File{Path: witnessPath, SHA256: hash("witness")}
	f.w.fn["code"] = func(r machine.Request) (machine.Result, error) {
		res := ok(r)
		res.ProjectFiles = []machine.File{code}
		if hasFile(r.Before, witnessPath) {
			res.ProjectFiles = []machine.File{code, witness}
		}
		return res, nil
	}
	f.w.fn["review"] = func(r machine.Request) (machine.Result, error) {
		res := ok(r)
		res.ProjectFiles = []machine.File{code, witness}
		if r.Round == 1 {
			fd := blocking("R-1", "internal/a.go", 10)
			fd.Witness = &machine.Witness{File: witnessPath, Test: witnessName}
			res.Findings = []machine.Finding{fd}
		}
		return res, nil
	}
	run, err := f.m.Start(context.Background(), f.params("RUN-1"))
	if err != nil {
		t.Fatal(err)
	}
	return f, h, log, run
}

func TestFindingRecordedCarriesItsWitness(t *testing.T) {
	f, _, _, run := witnessRun(t)
	assertEnded(t, run, f.ev, "done")
	var got *machine.Finding
	for _, e := range f.ev.events {
		if e.Kind == "FindingRecorded" && e.Round == 1 {
			got = e.Finding
		}
	}
	if got == nil || got.Witness == nil || *got.Witness != (machine.Witness{File: witnessPath, Test: witnessName}) {
		t.Fatalf("the round 1 finding was recorded without its witness: %+v", got)
	}
	validate(t, f.ev.events)
}

func TestFixStartsFromTheWitnessesOfTheOpenRound(t *testing.T) {
	f, h, log, run := witnessRun(t)
	assertEnded(t, run, f.ev, "done")
	fixes := f.w.callsTo("code")
	if len(fixes) != 2 {
		t.Fatalf("code ran %d times, want the first run and one fix", len(fixes))
	}
	if !hasFile(fixes[1].Before, witnessPath) {
		t.Errorf("the fix's before %v lacks the witness %s", fixes[1].Before, witnessPath)
	}
	rs := h.restoresOf("code")
	if len(rs) != 2 || !hasFile(rs[1].Before, witnessPath) {
		t.Fatalf("the restore before the fix does not keep the witness: %+v", rs)
	}
	// The review round 1's witnesses are committed before the fix's restore.
	var commit *machine.CommitRequest
	for i, c := range h.commitsOf("review") {
		if c.Round == 1 {
			commit = &h.commitsOf("review")[i]
		}
	}
	if commit == nil || !hasFile(commit.ProjectFiles, witnessPath) {
		t.Fatalf("no commit of review round 1 with its witness: %+v", h.commitsOf("review"))
	}
	ops := log.all()
	ci, ri := -1, -1
	for i, op := range ops {
		if op == "commit review" && ci < 0 {
			ci = i
		}
		if strings.HasPrefix(op, "restore code ") {
			ri = i // the last restore of code: the fix's
		}
	}
	if ci < 0 || ri < 0 || ci > ri {
		t.Errorf("the review's commit (%d) must come before the fix's restore (%d): %v", ci, ri, ops)
	}
	// Round 2 starts from the fix, witness kept.
	reviews := h.restoresOf("review")
	if len(reviews) < 2 || !hasFile(reviews[len(reviews)-1].Before, witnessPath) {
		t.Errorf("round 2's restore lacks the witness: %+v", reviews)
	}
}
