package steps_test

// T135 (owner, 2026-11-17; spec FR-017, US2/AC6; data-model "A later round's scope"; research D55,
// ADR 0055; check contract CheckRequest.round_scope): the checker sends `round_scope` to
// NoBlockingFindingOpen only, and only in a review round after the first, filled from the round's
// open findings and the lines the fix before it changed, as the machine gives them; never from
// the attempt's own result. Every other check, and round 1, gets no `round_scope`. Written before
// the code by a separate test author. Seams (read only): adapters_test.go (rig, checkFake),
// witness_test.go (witnessRig, witnessFinding).
//
// ASSUMED SHAPE: machine.CheckRequest gains `Open []machine.Finding` and
// `ChangedLines []machine.Line`, the same fields and types machine.Request has for the round
// (machine.go). They are set by reflection here, so this file compiles before they exist and
// fails, naming the missing field, until they do.

import (
	"reflect"
	"testing"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/steps"
)

// rsSetScope sets the request's Open and ChangedLines, failing the test when machine.CheckRequest
// has no such field of the type machine.Request gives it.
func rsSetScope(t *testing.T, r *machine.CheckRequest, open []machine.Finding, lines []machine.Line) {
	t.Helper()
	v := reflect.ValueOf(r).Elem()
	for name, val := range map[string]any{"Open": open, "ChangedLines": lines} {
		f := v.FieldByName(name)
		want := reflect.TypeOf(val)
		if !f.IsValid() || !f.CanSet() || f.Type() != want {
			t.Fatalf("machine.CheckRequest has no field %s %s (T135: the round's scope reaches the checker as machine.Request has it)", name, want)
		}
		f.Set(reflect.ValueOf(val))
	}
}

// rsOpenFinding is the round before's open finding: R-1 at greet.go:21 with its witness.
func rsOpenFinding() machine.Finding {
	return machine.Finding{Rule: "R-1", File: "internal/greet/greet.go", Line: 21, Text: "the empty name is not greeted", Blocking: true,
		Check: "AllTestsPass", Witness: &machine.Witness{File: witnessFile, Test: witnessTest}}
}

// rsCheck runs one check of the review step (step 3 of witnessPattern) in the round, with the
// scope set when open or lines is not nil, and returns the body the check service got.
func rsCheck(t *testing.T, name string, round int, open []machine.Finding, lines []machine.Line) map[string]any {
	t.Helper()
	r, _ := witnessRig(t, `{"findings":[]}`)
	review := r.cfg.Pattern.Steps[3]
	res := machine.Result{Outputs: map[string]string{"findings": hashB, "witnesses": hashC},
		Findings: []machine.Finding{witnessFinding()},
		// The review attempt's own changed lines: its witness. Never the round's scope.
		ChangedLines: []machine.Line{{File: witnessFile, Line: 1}},
		ProjectFiles: toFiles(r.chk.project)}
	req := machine.CheckRequest{Run: "RUN-7", Slice: "SLICE-1", Project: "proj-1", Step: review, Attempt: 1, Round: round,
		Name: name, Result: res, Inputs: []string{hashA}}
	if open != nil || lines != nil {
		rsSetScope(t, &req, open, lines)
	}
	if _, err := steps.NewChecker(r.cfg).Check(ctx5(t), req); err != nil {
		t.Fatal(err)
	}
	cs := r.chk.rec.find("POST", "/checks/"+name+"/run")
	if len(cs) != 1 {
		t.Fatalf("%s calls: %d, want 1", name, len(cs))
	}
	return cs[0].body
}

func rsWantOpen(t *testing.T, scope map[string]any, want []machine.Finding) {
	t.Helper()
	open, ok := scope["open"].([]any)
	if !ok || len(open) != len(want) {
		t.Fatalf("round_scope.open = %v, want %d findings", scope["open"], len(want))
	}
	for i, w := range want {
		g, _ := open[i].(map[string]any)
		if g["rule"] != w.Rule || g["file"] != w.File || g["line"] != float64(w.Line) || g["blocking"] != w.Blocking || g["text"] != w.Text {
			t.Errorf("round_scope.open[%d] = %v, want %+v", i, g, w)
		}
		if w.Witness != nil && !reflect.DeepEqual(g["witness"], map[string]any{"file": w.Witness.File, "test": w.Witness.Test}) {
			t.Errorf("round_scope.open[%d].witness = %v, want %+v", i, g["witness"], *w.Witness)
		}
	}
}

// NoBlockingFindingOpen in round 2 gets round_scope from the round's Open and ChangedLines.
func TestCheckerSendsRoundScopeToNoBlockingFindingOpenInALaterRound(t *testing.T) {
	open := []machine.Finding{rsOpenFinding()}
	lines := []machine.Line{{File: "internal/greet/greet.go", Line: 23}, {File: "cmd/m.go", Line: 3}}
	for _, round := range []int{2, 3} {
		b := rsCheck(t, "NoBlockingFindingOpen", round, open, lines)
		scope, ok := b["round_scope"].(map[string]any)
		if !ok {
			t.Fatalf("round %d: the NoBlockingFindingOpen request has no round_scope: %v", round, b)
		}
		if len(scope) != 2 {
			t.Errorf("round %d: round_scope = %v, want exactly open and changed_lines", round, scope)
		}
		rsWantOpen(t, scope, open)
		want := []any{map[string]any{"file": "internal/greet/greet.go", "line": float64(23)}, map[string]any{"file": "cmd/m.go", "line": float64(3)}}
		if !reflect.DeepEqual(scope["changed_lines"], want) {
			t.Errorf("round %d: round_scope.changed_lines = %v, want the fix's lines %v (not the review's own)", round, scope["changed_lines"], want)
		}
		if b["round"] != float64(round) {
			t.Errorf("round = %v, want %d", b["round"], round)
		}
	}
}

// A fix that changed no line: round_scope.changed_lines is sent, empty (the contract requires it).
func TestCheckerSendsEmptyChangedLinesInTheRoundScope(t *testing.T) {
	for _, lines := range [][]machine.Line{nil, {}} {
		b := rsCheck(t, "NoBlockingFindingOpen", 2, []machine.Finding{rsOpenFinding()}, lines)
		scope, ok := b["round_scope"].(map[string]any)
		if !ok {
			t.Fatalf("lines %#v: the request has no round_scope: %v", lines, b)
		}
		cl, ok := scope["changed_lines"].([]any)
		if !ok || len(cl) != 0 {
			t.Errorf("lines %#v: round_scope.changed_lines = %#v, want []", lines, scope["changed_lines"])
		}
		rsWantOpen(t, scope, []machine.Finding{rsOpenFinding()})
	}
}

// Round 1, and every other check in any round, get no round_scope.
func TestCheckerSendsNoRoundScopeElsewhere(t *testing.T) {
	t.Run("round 1", func(t *testing.T) {
		if b := rsCheck(t, "NoBlockingFindingOpen", 1, nil, nil); b["round_scope"] != nil {
			t.Errorf("round 1's NoBlockingFindingOpen request has round_scope %v", b["round_scope"])
		}
	})
	open := []machine.Finding{rsOpenFinding()}
	lines := []machine.Line{{File: "internal/greet/greet.go", Line: 23}}
	for _, name := range []string{"ScopeKept", "TestsUnchanged", "FindingsCarryEvidence"} {
		t.Run(name, func(t *testing.T) {
			b := rsCheck(t, name, 2, open, lines)
			if _, ok := b["round_scope"]; ok {
				t.Errorf("%s's request has round_scope %v; only NoBlockingFindingOpen's does", name, b["round_scope"])
			}
		})
	}
}
