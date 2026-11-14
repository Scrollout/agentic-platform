// Tests for task T073 (FR-003: every example links to the rule it serves). They fail until an
// example whose @R-<n> tag and "Rule: R-<n>" block name a rule that is not in `rules` is
// rejected, naming the example and the rule.
//
// Seam: Trace(doc []byte) []Finding, the Traceability check. ExampleTags takes only the Gherkin
// text, so it cannot know which rules exist; Trace reads `rules` and `examples` together, and its
// doc already says it follows the links between the IDs. The finding is on the rules part (the
// examples belong to it, data-model.md `parts`); its reason names the example's E-<n> ID and the
// missing rule's ID.
package slice

import (
	"encoding/json"
	"strings"
	"testing"
)

func unknownRuleDoc(t *testing.T, examples string) []byte {
	t.Helper()
	d := map[string]any{
		"goals": []map[string]string{{"id": "G-1", "text": "Keep notes"}},
		"terms": []map[string]string{},
		"rules": []map[string]any{{"id": "R-1", "goal": "G-1", "pattern": "ubiquitous",
			"system": "the notes service", "response": "shall store a note", "terms": []string{}}},
		"examples": examples,
		"contract": "openapi: 3.1.0\ninfo: {title: notes, version: '1'}\npaths:\n  /notes:\n    post:\n      operationId: createNote\n      x-rules: [R-1]\n      responses: {'201': {description: created}}\n",
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const knownRuleExamples = "Feature: Notes\n  Rule: R-1\n    @R-1 @E-1\n    Scenario: Create a note\n      When the user posts a note\n      Then the note is stored\n"

func TestTraceAcceptsExamplesOfKnownRules(t *testing.T) {
	if got := Trace(unknownRuleDoc(t, knownRuleExamples)); len(got) != 0 {
		t.Errorf("examples of existing rules were rejected: %+v", got)
	}
}

func TestTraceRejectsExampleOfARuleNotInRules(t *testing.T) {
	ex := knownRuleExamples + "\n  Rule: R-9\n    @R-9 @E-2\n    Scenario: Archive a note\n      When the user archives a note\n      Then the note is hidden\n"
	got := Trace(unknownRuleDoc(t, ex))
	var hits []Finding
	for _, f := range got {
		if strings.Contains(f.Reason, "R-9") {
			hits = append(hits, f)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("want one finding naming rule R-9, got %+v", got)
	}
	f := hits[0]
	if f.Part != "rules" {
		t.Errorf("part %q, want rules", f.Part)
	}
	if !strings.Contains(f.Reason, "E-2") {
		t.Errorf("reason %q does not name the example E-2", f.Reason)
	}
	if strings.Contains(f.Reason, "E-1") {
		t.Errorf("reason %q names the sound example E-1", f.Reason)
	}
}
