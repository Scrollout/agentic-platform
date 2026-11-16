// T039b: an empty `paths` is a contract with no HTTP operation, and the failure says so plainly.
package checks

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEmptyPathsReasonSaysNoHTTPOperation(t *testing.T) {
	contract := "openapi: 3.1.0\ninfo: {title: rename, version: '1'}\npaths: {}\n"
	body, err := json.Marshal(map[string]any{"contract": contract, "rules": []any{map[string]any{"id": "R-1", "system": "the tool"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range Fit(repoPattern(t), body) {
		if r.Check != "OnlyHttpOperations" {
			continue
		}
		if r.Passed {
			t.Fatalf("an empty paths passes: %s", r.Reason)
		}
		mustContain(t, r, "no HTTP operation")
		if strings.Contains(r.Reason, "paths (no operation)") {
			t.Errorf("the reason still reads as a path list: %q", r.Reason)
		}
		return
	}
	t.Fatal("no OnlyHttpOperations result")
}
