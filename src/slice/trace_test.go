// Tests for the traceability checks (task T026). They fail until T033
// implements trace.go.
//
// API the tests call (package slice):
//
//	type Finding struct{ Part, ID, Reason string }
//	func Trace(doc []byte) []Finding
//
// Trace takes the JSON of a slice or of a slice proposal (both carry goals,
// terms, rules, examples and contract; other properties are ignored) and
// returns every broken link, or nil when all links hold. It is a package
// function and not a method of Schemas because it needs no schema: the schemas
// (CheckSlice, CheckProposal) decide shape, Trace decides links between IDs.
// data-model.md gives Part as one of the three approvable parts, so Part is
// "goals", "rules" or "contract", the part the developer must fix. ID is the
// offending id (the goal, rule or term ID, or the rule ID an operation names
// that does not exist). Reason is free text that must contain ID.
// Where each defect belongs:
//
//	goal with no rule                  Part "goals",    ID the goal
//	rule naming a missing goal         Part "rules",    ID the rule
//	rule using a term not in terms     Part "rules",    ID the term
//	rule with no scenario in its block Part "rules",    ID the rule
//	operation x-rules names no rule    Part "contract", ID the missing rule
//
// Examples are Gherkin text: a scenario counts for a rule only when it sits
// under a "Rule:" block of that rule and carries the tag @R-<n>.
// Malformed input returns one Finding with Part "" and a non-empty Reason.
package slice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func traceFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "trace", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTraceFullyLinkedSlicePasses(t *testing.T) {
	if got := Trace(traceFixture(t, "linked")); len(got) != 0 {
		t.Errorf("linked slice rejected: %+v", got)
	}
}

func TestTraceRejectsEachBrokenLink(t *testing.T) {
	cases := []struct{ fixture, part, id string }{
		{"goal-without-rule", "goals", "G-3"},
		{"rule-missing-goal", "rules", "R-2"},
		{"rule-missing-term", "rules", "T-7"},
		{"rule-without-scenario", "rules", "R-2"},
		{"operation-missing-rule", "contract", "R-8"},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			got := Trace(traceFixture(t, c.fixture))
			if len(got) != 1 {
				t.Fatalf("want exactly one finding, got %+v", got)
			}
			f := got[0]
			if f.Part != c.part || f.ID != c.id {
				t.Errorf("got part %q id %q, want part %q id %q", f.Part, f.ID, c.part, c.id)
			}
			if !strings.Contains(f.Reason, c.id) {
				t.Errorf("reason %q does not name %s", f.Reason, c.id)
			}
		})
	}
}

func TestTraceMalformedJSONIsAFinding(t *testing.T) {
	got := Trace([]byte("{not json"))
	if len(got) != 1 || got[0].Reason == "" {
		t.Errorf("want one finding with a reason, got %+v", got)
	}
}
