// Regression tests for the fail-closed guards of Trace (T072 with T033a, review of the surviving
// mutants). Each case requires the finding of its own guard, not just any contract finding:
// an alias gives ID "*<anchor>", a merge key ID "<<", a mapping-shaped place that is not a mapping
// a reason saying "not a mapping", a scalar x-rules a reason saying "not a list", and an
// unreadable contract ID "contract" with Syntax set. opsDoc, goodOp and contractFindings are in
// trace_operations_test.go.
package slice

import (
	"encoding/json"
	"strings"
	"testing"
)

func withContract(t *testing.T, contract string) []byte {
	t.Helper()
	d := map[string]any{}
	if err := json.Unmarshal(opsDoc(t, goodOp), &d); err != nil {
		t.Fatal(err)
	}
	d["contract"] = contract
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func hasContractFinding(fs []Finding, ok func(Finding) bool) bool {
	for _, f := range contractFindings(fs) {
		if ok(f) {
			return true
		}
	}
	return false
}

func TestTraceNamesAYAMLAlias(t *testing.T) {
	c := "  /a:\n    get:\n      operationId: listA\n      x-rules: &r [R-1]\n      responses: {'200': {description: ok}}\n" +
		"  /b:\n    get:\n      operationId: listB\n      x-rules: *r\n      responses: {'200': {description: ok}}\n"
	got := Trace(opsDoc(t, goodOp+c))
	if !hasContractFinding(got, func(f Finding) bool { return f.ID == "*r" && strings.Contains(f.Reason, "alias") }) {
		t.Errorf("no contract finding with ID *r naming the alias: %+v", got)
	}
}

func TestTraceNamesAYAMLMergeKey(t *testing.T) {
	// An inline merge, with no alias, so only the merge-key guard can catch it.
	c := "  /b:\n    get:\n      <<: {x-rules: [R-1]}\n      operationId: listB\n      x-rules: [R-1]\n      responses: {'200': {description: ok}}\n"
	got := Trace(opsDoc(t, goodOp+c))
	if !hasContractFinding(got, func(f Finding) bool { return f.ID == "<<" && strings.Contains(f.Reason, "merge key") }) {
		t.Errorf("no contract finding with ID << naming the merge key: %+v", got)
	}
}

func TestTraceRejectsPlacesThatAreNotMappings(t *testing.T) {
	head := "openapi: 3.1.0\ninfo: {title: notes, version: '1'}\n"
	for name, contract := range map[string]string{
		"paths is a scalar":     head + "paths: 5\n",
		"path item is a scalar": head + "paths:\n" + goodOp + "  /a: 5\n",
		"operation is a scalar": head + "paths:\n" + goodOp + "  /a:\n    get: 5\n",
		"operation is a list":   head + "paths:\n" + goodOp + "  /a:\n    get: [operationId, listA]\n",
	} {
		t.Run(name, func(t *testing.T) {
			got := Trace(withContract(t, contract))
			if !hasContractFinding(got, func(f Finding) bool { return f.ID != "" && strings.Contains(f.Reason, "not a mapping") }) {
				t.Errorf("no contract finding saying it is not a mapping: %+v", got)
			}
		})
	}
}

func TestTraceRefusesAScalarXRulesAsNotAList(t *testing.T) {
	c := "  /free:\n    get:\n      operationId: listFree\n      x-rules: R-1\n      responses: {'200': {description: ok}}\n"
	got := Trace(opsDoc(t, goodOp+c))
	if !hasContractFinding(got, func(f Finding) bool {
		return strings.Contains(f.Reason, "listFree") && strings.Contains(f.Reason, "not a list")
	}) {
		t.Errorf("no contract finding saying listFree's x-rules is not a list: %+v", got)
	}
}

func TestTraceMarksAnUnreadableContractAsSyntax(t *testing.T) {
	got := Trace(withContract(t, "openapi: 3.1.0\ninfo: [unclosed\n  title: x\n"))
	var hits []Finding
	for _, f := range contractFindings(got) {
		if f.ID == "contract" {
			hits = append(hits, f)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("want one contract finding with ID contract, got %+v", got)
	}
	if !hits[0].Syntax {
		t.Errorf("the unreadable contract's finding is not marked Syntax: %+v", hits[0])
	}
}
