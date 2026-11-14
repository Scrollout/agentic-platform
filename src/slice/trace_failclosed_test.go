// Tests for task T033a, done in one unit with T072 (T033 review S3, S4). They fail until Trace
// fails closed on contract shapes it skips today: an x-rules that is not a list, an x-rules entry
// that is not a scalar, a contract root that is not a mapping, and YAML aliases or merge keys each
// give at least one finding on the contract part, with a non-empty ID (an empty ID names nothing
// the developer can find). Examples that are not Gherkin give a finding with a non-empty ID.
//
// Seam: Trace(doc []byte) []Finding. The exact ID and wording are the implementer's; for the
// operation cases the reason must name the operation. opsDoc and goodOp are in
// trace_operations_test.go.
package slice

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTraceFailsClosedOnContractShapes(t *testing.T) {
	cases := []struct {
		name, contract string
		names          string // the reason of some contract finding must contain it; "" for none
	}{
		{"x-rules is a scalar", "  /free:\n    get:\n      operationId: listFree\n      x-rules: R-1\n      responses: {'200': {description: ok}}\n", "listFree"},
		{"x-rules is a mapping", "  /free:\n    get:\n      operationId: listFree\n      x-rules: {rule: R-1}\n      responses: {'200': {description: ok}}\n", "listFree"},
		{"x-rules entry is a list", "  /free:\n    get:\n      operationId: listFree\n      x-rules: [R-1, [R-1]]\n      responses: {'200': {description: ok}}\n", "listFree"},
		{"x-rules entry is a mapping", "  /free:\n    get:\n      operationId: listFree\n      x-rules: [R-1, {id: R-1}]\n      responses: {'200': {description: ok}}\n", "listFree"},
		{"x-rules is an alias", "  /a:\n    get:\n      operationId: listA\n      x-rules: &r [R-1]\n      responses: {'200': {description: ok}}\n  /b:\n    get:\n      operationId: listB\n      x-rules: *r\n      responses: {'200': {description: ok}}\n", ""},
		{"operation is an alias", "  /a:\n    get: &op\n      operationId: listA\n      x-rules: [R-1]\n      responses: {'200': {description: ok}}\n  /b:\n    get: *op\n", ""},
		{"operation uses a merge key", "  /a:\n    get: &op\n      operationId: listA\n      x-rules: [R-1]\n      responses: {'200': {description: ok}}\n  /b:\n    get:\n      <<: *op\n      operationId: listB\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := contractFindings(Trace(opsDoc(t, goodOp+c.contract)))
			if len(got) == 0 {
				t.Fatal("no finding on the contract part")
			}
			named := c.names == ""
			for _, f := range got {
				if f.ID == "" {
					t.Errorf("finding with an empty ID: %+v", f)
				}
				if c.names != "" && strings.Contains(f.Reason, c.names) {
					named = true
				}
			}
			if !named {
				t.Errorf("no finding names operation %s: %+v", c.names, got)
			}
		})
	}
}

func TestTraceFailsClosedOnAContractRootThatIsNotAMapping(t *testing.T) {
	for name, contract := range map[string]string{
		"a list":   "- openapi: 3.1.0\n- paths: {}\n",
		"a scalar": "just some words\n",
	} {
		t.Run(name, func(t *testing.T) {
			d := map[string]any{}
			if err := json.Unmarshal(opsDoc(t, goodOp), &d); err != nil {
				t.Fatal(err)
			}
			d["contract"] = contract
			b, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			got := contractFindings(Trace(b))
			if len(got) == 0 {
				t.Fatal("no finding on the contract part")
			}
			for _, f := range got {
				if f.ID == "" {
					t.Errorf("finding with an empty ID: %+v", f)
				}
			}
		})
	}
}

func TestTraceGivesAGherkinParseFailureAnID(t *testing.T) {
	d := map[string]any{}
	if err := json.Unmarshal(opsDoc(t, goodOp), &d); err != nil {
		t.Fatal(err)
	}
	d["examples"] = "Feature: Notes\n  Rule: R-1\n    @R-1 @E-1\n    Not a keyword\n"
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExampleTags(d["examples"].(string)); err == nil {
		t.Fatal("setup: the examples parse as Gherkin")
	}
	var parse []Finding
	for _, f := range Trace(b) {
		if strings.Contains(f.Reason, "Gherkin") {
			parse = append(parse, f)
		}
	}
	if len(parse) != 1 {
		t.Fatalf("want one finding whose reason says the examples are not Gherkin, got %+v", Trace(b))
	}
	if parse[0].ID == "" {
		t.Errorf("the parse finding has an empty ID: %+v", parse[0])
	}
}
