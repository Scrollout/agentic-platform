// Tests for task T072 (FR-003; data-model.md `contract`: "every operation has an operationId and
// x-rules: [R-…]"). They fail until Trace rejects an operation with no operationId, or with a
// missing or empty x-rules, on the contract part, with a reason that names the operation.
//
// Seam: the existing package function Trace(doc []byte) []Finding. An operation with no
// operationId is named by its method and path (for example "GET /plain"); one with an
// operationId is named by it.
package slice

import (
	"encoding/json"
	"strings"
	"testing"
)

// opsDoc returns a slice whose goals, rules and examples are fully linked, with the given
// operations under paths: each entry is the YAML of one path item, keyed by its path.
func opsDoc(t *testing.T, contractPaths string) []byte {
	t.Helper()
	d := map[string]any{
		"goals": []map[string]string{{"id": "G-1", "text": "Keep notes"}},
		"terms": []map[string]string{},
		"rules": []map[string]any{{"id": "R-1", "goal": "G-1", "pattern": "ubiquitous",
			"system": "the notes service", "response": "shall store a note", "terms": []string{}}},
		"examples": "Feature: Notes\n  Rule: R-1\n    @R-1 @E-1\n    Scenario: Create a note\n      When the user posts a note\n      Then the note is stored\n",
		"contract": "openapi: 3.1.0\ninfo: {title: notes, version: '1'}\npaths:\n" + contractPaths,
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const goodOp = "  /notes:\n    post:\n      operationId: createNote\n      x-rules: [R-1]\n      responses: {'201': {description: created}}\n"

func contractFindings(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Part == "contract" {
			out = append(out, f)
		}
	}
	return out
}

func TestTraceAcceptsOperationsWithIDAndRules(t *testing.T) {
	if got := Trace(opsDoc(t, goodOp)); len(got) != 0 {
		t.Errorf("a fully linked contract was rejected: %+v", got)
	}
}

func TestTraceRejectsOperationWithoutIDOrRules(t *testing.T) {
	cases := []struct {
		name, path string
		names      []string // each must appear in the finding's reason
	}{
		{"no operationId", "  /plain:\n    get:\n      x-rules: [R-1]\n      responses: {'200': {description: ok}}\n",
			[]string{"GET", "/plain"}},
		{"empty operationId", "  /blank:\n    get:\n      operationId: ''\n      x-rules: [R-1]\n      responses: {'200': {description: ok}}\n",
			[]string{"GET", "/blank"}},
		{"no x-rules", "  /free:\n    get:\n      operationId: listFree\n      responses: {'200': {description: ok}}\n",
			[]string{"listFree", "x-rules"}},
		{"empty x-rules", "  /empty:\n    get:\n      operationId: listEmpty\n      x-rules: []\n      responses: {'200': {description: ok}}\n",
			[]string{"listEmpty", "x-rules"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := contractFindings(Trace(opsDoc(t, goodOp+c.path)))
			if len(got) != 1 {
				t.Fatalf("want one finding on the contract part, got %+v", got)
			}
			for _, n := range c.names {
				if !strings.Contains(got[0].Reason, n) {
					t.Errorf("reason %q does not name %q", got[0].Reason, n)
				}
			}
			if strings.Contains(got[0].Reason, "createNote") {
				t.Errorf("reason %q names the sound operation createNote", got[0].Reason)
			}
		})
	}
}

func TestTraceNamesEachBadOperation(t *testing.T) {
	two := "  /free:\n    get:\n      operationId: listFree\n      responses: {'200': {description: ok}}\n" +
		"  /plain:\n    delete:\n      x-rules: [R-1]\n      responses: {'204': {description: gone}}\n"
	got := contractFindings(Trace(opsDoc(t, goodOp+two)))
	if len(got) != 2 {
		t.Fatalf("want two findings on the contract part, got %+v", got)
	}
	all := got[0].Reason + "\n" + got[1].Reason
	for _, n := range []string{"listFree", "DELETE", "/plain"} {
		if !strings.Contains(all, n) {
			t.Errorf("no finding names %q: %+v", n, got)
		}
	}
}
