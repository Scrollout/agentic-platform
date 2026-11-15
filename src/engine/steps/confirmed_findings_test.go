package steps_test

// T096 (owner, 2026-11-15; research D39, ADR 0042; check contract CheckVerdict.confirmed): the
// checker maps NoBlockingFindingOpen's `confirmed`, positions in the request's `findings`, back to
// positions in the attempt's findings (machine.CheckResult.Confirmed), also when projectFindings
// dropped a finding before the call. A position out of range, or on a finding not sent as
// blocking, fails the check with the reason. Written before the code by a separate test author.
// Seams: adapters_test.go (rig, checkFake), witness_test.go (witnessRig).
//
// ASSUMED SHAPE: machine.CheckResult gains `Confirmed []int`.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/steps"
)

// cfFindings are an attempt's findings: the first names no project file and is dropped before the
// call, so the request's findings are the attempt's 1, 2 and 3 at positions 0, 1 and 2; the
// attempt's 2 is a suggestion.
func cfFindings() []machine.Finding {
	return []machine.Finding{
		{Rule: "R-1", File: "../x.go", Line: 1, Text: "outside the project", Blocking: true},
		{Rule: "R-2", File: "internal/greet/greet.go", Line: 23, Text: "the name is left out", Blocking: true,
			Witness: &machine.Witness{File: witnessFile, Test: witnessTest}},
		{Rule: "R-3", Text: "rename the handler"},
		{Rule: "R-4", File: "internal/greet/greet.go", Line: 21, Text: "the empty name is not greeted", Blocking: true,
			Witness: &machine.Witness{File: "review/r4/empty_test.go", Test: "TestR4Empty"}},
	}
}

// cfCheck runs NoBlockingFindingOpen on the review step with the service answering passed and
// confirmed (nil: no `confirmed` sent).
func cfCheck(t *testing.T, findings []machine.Finding, passed bool, confirmed []int) (machine.CheckResult, *rig, error) {
	t.Helper()
	r, _ := witnessRig(t, `{"findings":[]}`)
	v := map[string]any{"check": "NoBlockingFindingOpen", "passed": passed, "reason": "R-2 is open", "refs": []string{},
		"project_files": r.chk.project}
	if confirmed != nil {
		v["confirmed"] = confirmed
	}
	r.chk.verdict = v
	review := r.cfg.Pattern.Steps[3]
	res := machine.Result{Outputs: map[string]string{"findings": hashB, "witnesses": hashC}, Findings: findings,
		ProjectFiles: toFiles(r.chk.project)}
	got, err := steps.NewChecker(r.cfg).Check(ctx5(t), machine.CheckRequest{Run: "RUN-7", Slice: "SLICE-1", Project: "proj-1",
		Step: review, Attempt: 1, Round: 1, Name: "NoBlockingFindingOpen", Result: res, Inputs: []string{hashA}})
	return got, r, err
}

// sentRules are the rules of the findings the checker sent, in order.
func sentRules(t *testing.T, r *rig) []string {
	t.Helper()
	cs := r.chk.rec.find("POST", "/checks/NoBlockingFindingOpen/run")
	if len(cs) != 1 {
		t.Fatalf("NoBlockingFindingOpen calls: %d, want 1", len(cs))
	}
	fs, _ := cs[0].body["findings"].([]any)
	var out []string
	for _, f := range fs {
		rule, _ := f.(map[string]any)["rule"].(string)
		out = append(out, rule)
	}
	return out
}

func TestConfirmedPositionsMapBackToTheAttemptsFindings(t *testing.T) {
	cases := []struct {
		name      string
		findings  []machine.Finding
		confirmed []int
		want      []int
	}{
		{"one dropped before the call, both blocking confirmed", cfFindings(), []int{0, 2}, []int{1, 3}},
		{"one dropped before the call, the later one confirmed", cfFindings(), []int{2}, []int{3}},
		{"none dropped", cfFindings()[1:], []int{0, 2}, []int{0, 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, r, err := cfCheck(t, c.findings, false, c.confirmed)
			if err != nil {
				t.Fatal(err)
			}
			if rules := sentRules(t, r); !slices.Equal(rules, []string{"R-2", "R-3", "R-4"}) {
				t.Fatalf("setup: the request's findings are %v, want R-2, R-3, R-4", rules)
			}
			if got.Passed {
				t.Errorf("the check passed; the service said it failed: %+v", got)
			}
			if !slices.Equal(got.Confirmed, c.want) {
				t.Errorf("Confirmed = %v, want %v: the request's %v as the attempt's positions", got.Confirmed, c.want, c.confirmed)
			}
		})
	}
}

func TestAPassingVerdictConfirmsNothing(t *testing.T) {
	got, _, err := cfCheck(t, cfFindings(), true, []int{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Passed || len(got.Confirmed) != 0 {
		t.Errorf("result = %+v, want passed with nothing confirmed", got)
	}
}

func TestAConfirmedPositionThatIsNoBlockingFindingFailsTheCheck(t *testing.T) {
	cases := []struct {
		name      string
		passed    bool
		confirmed []int
		bad       []int // the positions that are not the request's blocking findings
	}{
		{"past the request's findings", false, []int{0, 3}, []int{3}},
		{"far past the request's findings", false, []int{7}, []int{7}},
		{"negative", false, []int{-1}, []int{-1}},
		{"a finding sent as a suggestion", false, []int{1}, []int{1}},
		{"a finding sent as a suggestion, beside a blocking one", false, []int{0, 1}, []int{1}},
		{"on a passing verdict, past the request's findings", true, []int{3}, []int{3}},
		{"on a passing verdict, a finding sent as a suggestion", true, []int{1}, []int{1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := cfCheck(t, cfFindings(), c.passed, c.confirmed)
			msg := got.Reason
			if err != nil {
				msg = err.Error()
			}
			if err == nil && got.Passed {
				t.Fatalf("the check passed with confirmed %v: %+v", c.confirmed, got)
			}
			if len(got.Confirmed) != 0 {
				t.Errorf("Confirmed = %v, want none from a verdict whose confirmed %v is not the request's blocking findings", got.Confirmed, c.confirmed)
			}
			if !strings.Contains(msg, "confirmed") {
				t.Errorf("the failure %q does not say that confirmed %v is wrong", msg, c.confirmed)
			}
			for _, p := range c.bad {
				// the position as a number of its own, not the 1 of a rule such as R-1
				named := regexp.MustCompile(`(^|[^A-Za-z0-9-])` + regexp.QuoteMeta(fmt.Sprint(p)) + `($|[^0-9])`)
				if !named.MatchString(msg) {
					t.Errorf("the failure %q does not name the position %d", msg, p)
				}
			}
		})
	}
}
