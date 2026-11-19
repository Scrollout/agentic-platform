package machine_test

// T135 (owner, 2026-11-17; spec FR-017, US2/AC6; data-model "A later round's scope"; research D55,
// ADR 0055): in a review round after the first, NoBlockingFindingOpen judges within the round's
// scope, which the engine takes from the run: the open findings the round was given and the lines
// the fix before it changed (a round after a stop: workflow_stop_test.go, T012 (d)). The machine
// records as blocking exactly the findings in the check's `confirmed` (recordedFindings), so a
// finding the check left out as outside the scope is recorded `blocking: false`, and a round whose
// NoBlockingFindingOpen passed is accepted. Written before the code by a separate test author.
// Seams (read only): open_round_reuse_test.go (newOR, everyRoundOpen, roundFindings, findingsEqual),
// open_round_resume_test.go (blockingOf), confirmed_findings_test.go
// (asSuggestion, fixCalls), witness_test.go (witnessPatternOf), machine_test.go.
//
// ASSUMED SHAPE: machine.CheckRequest gains `Open []machine.Finding` and
// `ChangedLines []machine.Line`, the fields machine.Request has for the round. They are read by
// reflection here, so this file compiles before they exist and fails, naming the missing field,
// until they do.

import (
	"reflect"
	"slices"
	"testing"

	"github.com/agentic-platform/engine/machine"
)

// rsScopeOf is the check request's Open and ChangedLines.
func rsScopeOf(t *testing.T, r machine.CheckRequest) ([]machine.Finding, []machine.Line) {
	t.Helper()
	v := reflect.ValueOf(r)
	get := func(name string, want reflect.Type) reflect.Value {
		f := v.FieldByName(name)
		if !f.IsValid() || f.Type() != want {
			t.Fatalf("machine.CheckRequest has no field %s %s (T135: the round's scope reaches the checker as machine.Request has it)", name, want)
		}
		return f
	}
	open := get("Open", reflect.TypeOf([]machine.Finding(nil))).Interface().([]machine.Finding)
	lines := get("ChangedLines", reflect.TypeOf([]machine.Line(nil))).Interface().([]machine.Line)
	return open, lines
}

// rsOpenChecks are the NoBlockingFindingOpen requests of the review, by round.
func rsOpenChecks(x *orfix) map[int][]machine.CheckRequest {
	out := map[int][]machine.CheckRequest{}
	for _, c := range x.c.calls {
		if c.Name == "NoBlockingFindingOpen" && c.Step.Name == "review" {
			out[c.Round] = append(out[c.Round], c)
		}
	}
	return out
}

// rsReviewCall is the review's request of the round and attempt.
func rsReviewCall(t *testing.T, x *orfix, round, attempt int) machine.Request {
	t.Helper()
	for _, c := range x.w.callsTo("review") {
		if c.Round == round && c.Attempt == attempt {
			return c
		}
	}
	t.Fatalf("setup: no review request of round %d attempt %d", round, attempt)
	return machine.Request{}
}

// rsWantScope: every NoBlockingFindingOpen request of the round carries the scope the review of
// that round's attempt was given, and that scope is open and lines.
func rsWantScope(t *testing.T, x *orfix, round int, open []machine.Finding, lines []machine.Line) {
	t.Helper()
	cs := rsOpenChecks(x)[round]
	if len(cs) == 0 {
		t.Fatalf("setup: no NoBlockingFindingOpen request in review round %d", round)
	}
	for _, c := range cs {
		gotOpen, gotLines := rsScopeOf(t, c)
		rv := rsReviewCall(t, x, round, c.Attempt)
		if !findingsEqual(gotOpen, rv.Open) || !slices.Equal(gotLines, rv.ChangedLines) {
			t.Errorf("round %d attempt %d: the check got open %+v, lines %+v; the review was given open %+v, lines %+v",
				round, c.Attempt, gotOpen, gotLines, rv.Open, rv.ChangedLines)
		}
		if !findingsEqual(gotOpen, open) || !slices.Equal(gotLines, lines) {
			t.Errorf("round %d attempt %d: the check got open %+v, lines %+v; want open %+v, lines %+v",
				round, c.Attempt, gotOpen, gotLines, open, lines)
		}
	}
}

// fixLines are the lines newOR's fix of round r reports changing.
func fixLines(r int) []machine.Line { return []machine.Line{{File: "code/f.go", Line: 10 * r}} }

func TestLaterRoundsCheckGetsTheRoundsScope(t *testing.T) {
	x := newOR(t, witnessPatternOf(), nil, everyRoundOpen)
	r := x.startIn("RUN-1", "proj-a")
	if r.Status != machine.StatusWaiting {
		t.Fatalf("setup: run = %s, want handed over after 3 open rounds: %v", r.Status, x.ev.kinds())
	}
	for _, c := range rsOpenChecks(x)[1] {
		if open, lines := rsScopeOf(t, c); len(open) != 0 || len(lines) != 0 {
			t.Errorf("round 1's check got open %+v, lines %+v; round 1 has no scope", open, lines)
		}
	}
	for _, round := range []int{2, 3} {
		rsWantScope(t, x, round, blockingOf(everyRoundOpen(round-1)), fixLines(round-1))
	}
}

// rsRound2 is a review whose round 1 confirms R-1 and whose round 2 makes the claims claims2.
func rsRound2(claims2 []machine.Finding) func(int) []machine.Finding {
	return func(round int) []machine.Finding {
		switch round {
		case 1:
			fd := blocking("R-1", "code/f.go", 10)
			fd.Witness = &machine.Witness{File: witnessPath, Test: witnessName}
			return []machine.Finding{fd}
		case 2:
			return slices.Clone(claims2)
		}
		return nil
	}
}

// rsOutOfScope is round 2's new claim on a line the fix did not change: its witness confirms it,
// the scope does not.
func rsOutOfScope() machine.Finding {
	f := blocking("R-2", "code/g.go", 99)
	f.Witness = &machine.Witness{File: "review/r2/g_test.go", Test: "TestR2G"}
	return f
}

// rsRepeat is round 2's claim that repeats the open R-1 at a moved line.
func rsRepeat() machine.Finding {
	f := blocking("R-1", "code/f.go", 12)
	f.Witness = &machine.Witness{File: "review/r1/again_test.go", Test: "TestR1Again"}
	return f
}

// rsScoped is NoBlockingFindingOpen as the check service gives it: round 1 confirms every blocking
// claim; in round 2 it confirms the positions in2 and names the others as outside the scope.
func rsScoped(in2 []int) func(machine.CheckRequest) (machine.CheckResult, error) {
	return func(r machine.CheckRequest) (machine.CheckResult, error) {
		if r.Name != "NoBlockingFindingOpen" {
			return pass(r), nil
		}
		confirmed := []int{}
		for i, f := range r.Result.Findings {
			if f.Blocking && (r.Round == 1 || slices.Contains(in2, i)) {
				confirmed = append(confirmed, i)
			}
		}
		if len(confirmed) == 0 {
			res := pass(r)
			res.Reason = "finding 0 (R-2 code/g.go:99) is outside round 2's scope: a suggestion"
			res.Confirmed = []int{}
			return res, nil
		}
		res := fail(r)
		res.Confirmed = confirmed
		return res, nil
	}
}

// T135: a round-2 finding the check left out of `confirmed` (outside the scope) is recorded as a
// suggestion, and the round, its NoBlockingFindingOpen passed, is accepted: no fix, no round 3.
func TestOutOfScopeFindingIsASuggestionAndTheRoundIsAccepted(t *testing.T) {
	x := newOR(t, witnessPatternOf(), nil, rsRound2([]machine.Finding{rsOutOfScope()}))
	x.c.fn = rsScoped(nil)
	assertEnded(t, x.startIn("RUN-1", "proj-a"), x.ev, "done")

	if got, want := roundFindings(x.ev.events, 2), []machine.Finding{asSuggestion(rsOutOfScope())}; !findingsEqual(got, want) {
		t.Errorf("review round 2 recorded %+v, want the out-of-scope claim as a suggestion %+v", got, want)
	}
	accepted := false
	for _, e := range x.ev.events {
		if e.Kind == "StepAccepted" && e.Step == "review" && e.Round == 2 {
			accepted = true
		}
		if e.Step == "review" && e.Round == 2 && e.Kind == "StepRejected" {
			t.Errorf("review round 2 was rejected (%q), want it accepted", e.Detail)
		}
		if e.Kind == "CheckPassed" && e.Step == "review" && e.Round == 2 && e.Check != nil && e.Check.Name == "NoBlockingFindingOpen" &&
			e.Check.Reason != "finding 0 (R-2 code/g.go:99) is outside round 2's scope: a suggestion" {
			t.Errorf("round 2's NoBlockingFindingOpen recorded the reason %q, want the check's", e.Check.Reason)
		}
	}
	if !accepted {
		t.Errorf("review round 2 was not accepted: %v", x.ev.kinds())
	}
	if n := len(fixCalls(x.fixture, 2)); n != 0 {
		t.Errorf("a fix ran %d time(s) after round 2, whose only claim is a suggestion", n)
	}
	if n := len(x.w.callsTo("review")); n != 2 {
		t.Errorf("review ran %d rounds, want 2", n)
	}
	validate(t, x.ev.events)
}

// T135: of round 2's two claims, the check confirms only the repeat (position 0); the machine
// records exactly that one as blocking, the out-of-scope one as a suggestion, and the fix of round 2
// is given only the repeat.
func TestOnlyTheConfirmedLaterRoundFindingIsRecordedAsBlocking(t *testing.T) {
	x := newOR(t, witnessPatternOf(), nil, rsRound2([]machine.Finding{rsRepeat(), rsOutOfScope()}))
	x.c.fn = rsScoped([]int{0})
	assertEnded(t, x.startIn("RUN-1", "proj-a"), x.ev, "done") // round 3 finds nothing
	if got, want := roundFindings(x.ev.events, 2), []machine.Finding{rsRepeat(), asSuggestion(rsOutOfScope())}; !findingsEqual(got, want) {
		t.Errorf("review round 2 recorded %+v, want the repeat blocking and the out-of-scope claim a suggestion %+v", got, want)
	}
	fixes := fixCalls(x.fixture, 2)
	if len(fixes) == 0 {
		t.Fatalf("no fix ran after review round 2, which confirmed the repeat: %v", x.ev.kinds())
	}
	for _, c := range fixes {
		if !findingsEqual(c.Open, []machine.Finding{rsRepeat()}) {
			t.Errorf("the fix of round 2 is given open %+v, want only the repeat %+v", c.Open, rsRepeat())
		}
	}
	validate(t, x.ev.events)
}
