package api

// T039c through the API: every check result in getSlice carries a boolean `pattern` (JSON name
// assumed: "pattern", required in slice-defs.schema.json check_result), true for conditions of the
// catalogue pattern and false for the engine's own checks.

import (
	"encoding/json"
	"testing"
)

func TestGetSliceChecksCarryPatternFlag(t *testing.T) {
	sp := newNotes(t)
	sp.seed(t, "Let me keep notes.")
	r := sp.do("GET", spURL("S-1", ""), clientToken, "", nil)
	if r.Code != 200 {
		t.Fatalf("getSlice = %d: %s", r.Code, r.Body.String())
	}
	var s struct {
		Checks []map[string]any `json:"checks"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"ContractIsOpenApi31": true, "OnlyHttpOperations": true, "ContractRefsLocal": true, "RulesNameOneSystem": true, "RefusalDeclared": true, "RequestBodiesSupported": true}
	seen := map[string]bool{}
	for _, c := range s.Checks {
		name, _ := c["check"].(string)
		seen[name] = true
		flag, ok := c["pattern"].(bool)
		if !ok {
			t.Errorf("check %s has no boolean pattern: %v", name, c)
			continue
		}
		if flag != want[name] {
			t.Errorf("check %s: pattern = %v, want %v", name, flag, want[name])
		}
	}
	for n := range want {
		if !seen[n] {
			t.Errorf("no result for %s", n)
		}
	}
}
