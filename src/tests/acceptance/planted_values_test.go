//go:build docker

// Feature 009 (no hard-coded values): the planted failures of the two values checks, each stopped
// in 3 of 3 trials, deterministically, as TestPlanted does for its five. The step agents' answers
// are in src/tests/recorded/steps/planted/<scenario>/ (answers.json, and the mappings made from it
// by synth-steps.sh), which the recorded model serves; no live model is involved.
//
//   - hard-coded-declared-value: the slice's contract declares maxLength 200 on the new note's text
//     (replaced through the API, then approved), and the code step's answer compares the text's
//     length with the literal 200: DeclaredValuesNotRepeated stops it.
//   - hard-coded-listen-address: the code step's answer listens on the literal ":8080" instead of
//     gen.ListenAddress(): DeploymentValuesConfigured stops it (SC-001).
package acceptance

import (
	"fmt"
	"strings"
	"testing"

	"github.com/agentic-platform/tests/lab"
)

var plantedValuesScenarios = []scenario{
	{name: "hard-coded-declared-value", step: "code", check: "DeclaredValuesNotRepeated",
		spec: "the code step's answer compares the new note's text length with the literal 200, which the contract declares as maxLength"},
	{name: "hard-coded-listen-address", step: "code", check: "DeploymentValuesConfigured",
		spec: "the code step's answer listens on the literal \":8080\" instead of gen.ListenAddress()"},
}

// The contract's addition for the declared-value scenario: the new note's text is declared at most
// 200 characters, in the schema the createNote request body names.
const (
	createNoteAnchor = "      x-rules: [R-2, R-3]\n"
	createNoteBody   = createNoteAnchor +
		"      requestBody:\n" +
		"        required: true\n" +
		"        content:\n" +
		"          application/json:\n" +
		"            schema: {$ref: '#/components/schemas/NewNote'}\n"
	newNoteSchema = "components:\n" +
		"  schemas:\n" +
		"    NewNote:\n" +
		"      type: object\n" +
		"      required: [text]\n" +
		"      properties:\n" +
		"        text: {type: string, maxLength: 200}\n"
)

// withTextLimit returns the example contract with maxLength 200 declared on the new note's text.
// It fails the test when the example contract is not the text it is written against, so a change
// of the example is noticed here rather than as a different scenario.
func withTextLimit(t *testing.T, contract string) string {
	t.Helper()
	if strings.Count(contract, createNoteAnchor) != 1 || strings.Contains(contract, "components:") || !strings.HasSuffix(contract, "\n") {
		t.Fatalf("the example contract is not the one this scenario adds maxLength to (createNote with x-rules [R-2, R-3], no components, ending in a newline):\n%s", contract)
	}
	return strings.Replace(contract, createNoteAnchor, createNoteBody, 1) + newNoteSchema
}

// declaredValueLab starts a lab over the scenario's step set and gives the example slice a contract
// that declares maxLength 200 on the new note's text, replaced through the API before the three
// parts are approved, so the approved contract is the changed one. It returns the lab and the slice.
func declaredValueLab(t *testing.T, sc scenario) (*lab.Lab, string) {
	t.Helper()
	requirePrerequisites(t)
	set := plantedSet + "/" + sc.name
	requireSet(t, set)
	l := lab.Start(t, lab.Build(), lab.Steps(set))
	id := l.Create(t, lab.Example)
	answerExample(t, l, id)
	l.Replace(t, id, "contract", map[string]any{"contract": withTextLimit(t, l.Get(t, id).Contract)})
	for _, p := range []string{"goals", "rules", "contract"} {
		l.Approve(t, id, p)
	}
	d := l.Get(t, id)
	if d.State != "approved" {
		t.Fatalf("the example slice is %q after the contract's replacement and three approvals, want approved (the changed contract must still fit the pattern); failed checks %+v", d.State, d.Failed())
	}
	if !strings.Contains(d.Contract, "maxLength: 200") {
		t.Fatalf("the approved contract does not declare maxLength 200:\n%s", d.Contract)
	}
	return l, id
}

// TestPlantedValues runs each planted value in 3 of 3 trials: the code step is not accepted, fails
// the named check in each of its attempts, and the run waits for the developer.
func TestPlantedValues(t *testing.T) {
	for _, sc := range plantedValuesScenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			var l *lab.Lab
			var slice string
			if sc.name == "hard-coded-declared-value" {
				l, slice = declaredValueLab(t, sc)
			} else {
				l, slice = plantedLab(t, sc)
			}
			for trial := 1; trial <= 3; trial++ {
				project := fmt.Sprintf("%s-%d", sc.name, trial)
				t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) {
					plantedTrial(t, l, slice, sc, project)
				})
			}
			if n := l.Misses(t); n != 0 {
				t.Errorf("%s: the replay server saw %d request(s) no recording matched:\n%s", sc.name, n, l.MissesJSON(t))
			}
		})
	}
}
