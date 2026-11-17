// T135 (owner, 2026-11-17; spec FR-017, US2/AC6; data-model "A later round's scope"; research D55,
// ADR 0055): in a review round after the first, NoBlockingFindingOpen counts a finding its witness
// run confirms as confirmed only within the request's `round_scope`: its rule and file equal those
// of an entry of `round_scope.open` (a repeat; the line may differ), or its file and line equal an
// entry of `round_scope.changed_lines`. A finding confirmed outside the scope is a suggestion: not
// in `confirmed` or `refs`, and named in the reason as `finding <position> (<rule> <file>:<line>)
// is outside round <round>'s scope: a suggestion`. A NoBlockingFindingOpen request with
// `round_scope` and no `round`, with `round` 1 and `round_scope`, or with `round` above 1 and no
// `round_scope` is refused with 400 InvalidRequest; every other check ignores `round_scope`.
//
// Over HTTP only, against the committed check contract
// (src/check/api/preview/2026-11-13-preview/openapi.yaml, CheckRequest.round_scope, runCheck,
// CheckVerdict.confirmed). Written before the code by a separate test author. `<position>` in the
// reason is read as the contract uses "position" for `confirmed`: 0-based, in the request's
// `findings`. Seams (read only): witness_test.go (witBody, witFinding, the testdata/witness
// fixture), confirmed_findings_test.go (cnVerdict, cnConfirmed, cnPassed, cnShow),
// helpers_test.go. Needs the Go toolchain and the module cache (the witness runs), not git.
package checksvc_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

const (
	rsOther = "internal/other/other.go"
	// rsR2Witness confirms R-2 at internal/other/other.go:5 (Answer returns 41): written into the
	// test's copy of the witness fixture only, so that two findings of two rules in two files
	// can be confirmed in one request.
	rsR2Witness = "review/r2/answer_test.go"
	rsR2Test    = "TestR2AnswerIs42"
)

var rsR2WitnessSrc = []byte(`package r2

import (
	"testing"

	"example.com/w/internal/other"
)

// The witness of R-2: it runs other.go:5 and fails.
func TestR2AnswerIs42(t *testing.T) {
	if got := other.Answer(); got != 42 {
		t.Errorf("Answer() = %d, want 42", got)
	}
}
`)

// rsGreet is a finding its witness confirms: R-1 at internal/greet/greet.go:23.
func rsGreet() map[string]any { return witFinding("R-1", witGreet, 23, witR1, witT1) }

// rsAnswer is a finding its witness confirms: R-2 at internal/other/other.go:5.
func rsAnswer() map[string]any { return witFinding("R-2", rsOther, 5, rsR2Witness, rsR2Test) }

// rsOpen is an entry of round_scope.open: a finding the round before recorded as blocking.
func rsOpen(rule, file string, line int) map[string]any {
	n := strings.TrimPrefix(rule, "R-")
	return witFinding(rule, file, line, "review/r"+n+"/earlier_test.go", "TestR"+n+"Earlier")
}

func rsLine(file string, line int) map[string]any { return map[string]any{"file": file, "line": line} }

// rsScope is a round_scope; nil lists are sent as [].
func rsScope(open []map[string]any, changed []map[string]any) map[string]any {
	o, c := []any{}, []any{}
	for _, x := range open {
		o = append(o, x)
	}
	for _, x := range changed {
		c = append(c, x)
	}
	return map[string]any{"open": o, "changed_lines": c}
}

// rsServe serves the check service over the witness fixture with the R-2 witness added.
func rsServe(t *testing.T) (base, dir string) {
	t.Helper()
	root, dir := fixture(t, "witness")
	write(t, dir, rsR2Witness, rsR2WitnessSrc)
	return serve(t, checkConfig(t, root)), dir
}

// rsBody is a NoBlockingFindingOpen body of a review round with the scope (round 0: no `round`;
// scope nil: no `round_scope`).
func rsBody(t *testing.T, dir string, round int, scope map[string]any, fs ...map[string]any) map[string]any {
	t.Helper()
	b := witBody(t, dir, false, fs...)
	if round > 0 {
		b["round"] = round
	}
	if scope != nil {
		b["round_scope"] = scope
	}
	return b
}

func rsRefs(t *testing.T, v map[string]json.RawMessage) []string {
	t.Helper()
	var refs []string
	if err := json.Unmarshal(v["refs"], &refs); err != nil {
		t.Fatalf("refs = %s: %v", v["refs"], err)
	}
	return refs
}

func rsReason(t *testing.T, v map[string]json.RawMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(v["reason"], &s); err != nil {
		t.Fatalf("reason = %s: %v", v["reason"], err)
	}
	return s
}

// rsOutside is the reason's part for a finding confirmed outside the round's scope.
func rsOutside(position int, rule, file string, line, round int) string {
	return "finding " + itoa(position) + " (" + rule + " " + file + ":" + itoa(line) + ") is outside round " + itoa(round) + "'s scope: a suggestion"
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// rsParts are the reason's parts, split on "; ".
func rsParts(reason string) []string { return strings.Split(reason, "; ") }

// Setup check, and T135 "round 1 without round_scope": round 1 (and no round) judge as before,
// every confirmed finding blocks.
func TestRoundScopeRoundOneWithoutScopeConfirmsAsBefore(t *testing.T) {
	base, dir := rsServe(t)
	for _, round := range []int{0, 1} {
		v := cnVerdict(t, base, "NoBlockingFindingOpen", rsBody(t, dir, round, nil, rsGreet(), rsAnswer()))
		if cnPassed(t, v) {
			t.Fatalf("round %d: NoBlockingFindingOpen passed with two confirmed findings: %s", round, cnShow(v))
		}
		if got := cnConfirmed(t, v); !slices.Equal(got, []int{0, 1}) {
			t.Errorf("round %d: confirmed = %v, want [0 1] (both witnesses confirm): %s", round, got, cnShow(v))
		}
		if r := rsReason(t, v); strings.Contains(r, "scope") {
			t.Errorf("round %d: the reason speaks of a scope, which round 1 has none of: %s", round, r)
		}
	}
}

// T135 "round 1 with it and round above 1 without it (400 InvalidRequest), and round_scope without
// round"; every other check ignores round_scope.
func TestRoundScopeRefusedWhereTheRoundDoesNotMatch(t *testing.T) {
	base, dir := rsServe(t)
	scope := rsScope([]map[string]any{rsOpen("R-1", witGreet, 23)}, nil)
	cases := []struct {
		name  string
		round int
		scope map[string]any
	}{
		{"round 1 with round_scope", 1, scope},
		{"round 2 without round_scope", 2, nil},
		{"round 3 without round_scope", 3, nil},
		{"round_scope without round", 0, scope},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := post(t, base, "/checks/NoBlockingFindingOpen/run", rsBody(t, dir, c.round, c.scope, rsGreet()))
			wantProblem(t, r, 400, "InvalidRequest")
		})
	}
	for _, check := range []string{"FindingsCarryEvidence", "TestsUnchanged", "ScopeKept"} {
		for _, c := range cases {
			t.Run(check+" ignores it: "+c.name, func(t *testing.T) {
				r := post(t, base, "/checks/"+check+"/run", rsBody(t, dir, c.round, c.scope, rsGreet()))
				if r.status != 200 {
					t.Errorf("%s: want 200 (only NoBlockingFindingOpen reads round_scope), got %d: %.400s", check, r.status, r.body)
				}
			})
		}
	}
}

// T135: which confirmed finding blocks in a later round.
func TestRoundScopeKeepsConfirmedWithinTheScope(t *testing.T) {
	base, dir := rsServe(t)
	type fs = []map[string]any
	cases := []struct {
		name      string
		round     int
		open      fs
		changed   fs
		findings  fs
		passed    bool
		confirmed []int
		refs      []string // exactly
		outside   []string // reason parts, each exactly
	}{
		{name: "a repeat (same rule and file) at a moved line blocks",
			round: 2, open: fs{rsOpen("R-1", witGreet, 21)}, findings: fs{rsGreet()},
			confirmed: []int{0}, refs: []string{"R-1"}},
		{name: "a repeat blocks with no changed lines, in round 3",
			round: 3, open: fs{rsOpen("R-1", witGreet, 11)}, findings: fs{rsGreet()},
			confirmed: []int{0}, refs: []string{"R-1"}},
		{name: "the same rule in another file is no repeat: a suggestion",
			round: 2, open: fs{rsOpen("R-1", rsOther, 5)}, findings: fs{rsGreet()},
			passed: true, confirmed: []int{}, refs: []string{},
			outside: []string{rsOutside(0, "R-1", witGreet, 23, 2)}},
		{name: "another rule in the same file is no repeat: a suggestion",
			round: 2, open: fs{rsOpen("R-2", witGreet, 23)}, findings: fs{rsGreet()},
			passed: true, confirmed: []int{}, refs: []string{},
			outside: []string{rsOutside(0, "R-1", witGreet, 23, 2)}},
		{name: "a finding on a changed line blocks",
			round: 2, open: fs{rsOpen("R-3", rsOther, 5)}, changed: fs{rsLine(witGreet, 23)}, findings: fs{rsGreet()},
			confirmed: []int{0}, refs: []string{"R-1"}},
		{name: "a finding on an unchanged line is a suggestion named in the reason",
			round: 2, open: fs{rsOpen("R-3", rsOther, 5)}, changed: fs{rsLine(witGreet, 21), rsLine(rsOther, 23)}, findings: fs{rsGreet()},
			passed: true, confirmed: []int{}, refs: []string{},
			outside: []string{rsOutside(0, "R-1", witGreet, 23, 2)}},
		{name: "of two confirmed, only the one on a changed line blocks; the other is named",
			round: 2, open: fs{rsOpen("R-3", "cmd/x.go", 1)}, changed: fs{rsLine(witGreet, 23)},
			findings:  fs{rsAnswer(), rsGreet()},
			confirmed: []int{1}, refs: []string{"R-1"},
			outside: []string{rsOutside(0, "R-2", rsOther, 5, 2)}},
		{name: "every confirmed finding outside the scope: passes with confirmed empty",
			round: 3, open: fs{rsOpen("R-3", "cmd/x.go", 1)}, changed: fs{rsLine("cmd/x.go", 2)},
			findings: fs{rsGreet(), finding("R-4", witGreet, 21, false), rsAnswer()},
			passed:   true, confirmed: []int{}, refs: []string{},
			outside: []string{rsOutside(0, "R-1", witGreet, 23, 3), rsOutside(2, "R-2", rsOther, 5, 3)}},
		{name: "empty changed_lines: only a repeat blocks",
			round: 2, open: fs{rsOpen("R-2", rsOther, 1)}, findings: fs{rsGreet(), rsAnswer()},
			confirmed: []int{1}, refs: []string{"R-2"},
			outside: []string{rsOutside(0, "R-1", witGreet, 23, 2)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := cnVerdict(t, base, "NoBlockingFindingOpen", rsBody(t, dir, c.round, rsScope(c.open, c.changed), c.findings...))
			if got := cnPassed(t, v); got != c.passed {
				t.Errorf("passed = %v, want %v: %s", got, c.passed, cnShow(v))
			}
			if got := cnConfirmed(t, v); !slices.Equal(got, c.confirmed) {
				t.Errorf("confirmed = %v, want %v: %s", got, c.confirmed, cnShow(v))
			}
			if got := rsRefs(t, v); !slices.Equal(got, c.refs) {
				t.Errorf("refs = %v, want %v (an out-of-scope finding is not in refs): %s", got, c.refs, cnShow(v))
			}
			reason := rsReason(t, v)
			parts := rsParts(reason)
			for _, want := range c.outside {
				if !slices.Contains(parts, want) {
					t.Errorf("the reason lacks the part %q (joined by \"; \"): %q", want, reason)
				}
			}
			if len(c.outside) == 0 && strings.Contains(reason, "outside round") {
				t.Errorf("the reason names a finding outside the scope, though every confirmed one is within it: %q", reason)
			}
			for _, p := range c.confirmed {
				f := c.findings[p]
				if !strings.Contains(reason, f["rule"].(string)) || !strings.Contains(reason, f["file"].(string)) {
					t.Errorf("the reason does not name the blocking finding %s at %s: %q", f["rule"], f["file"], reason)
				}
			}
		})
	}
}
