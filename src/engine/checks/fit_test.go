// T028: failing tests for the pattern-fit check (implemented in T035, checks/fit.go).
//
// The API these tests fix, so the implementer follows it:
//
//		// Fit evaluates every condition the pattern lists, in the pattern's order, against the slice.
//		func Fit(pattern []byte, slice []byte) []CheckResult
//
//		type CheckResult struct { Part, Check string; Passed bool; Reason string }
//
//	  - pattern is the bytes of a pattern.yaml; slice is the JSON of a slice's parts (the proposal
//	    shape: "rules" array and "contract" string, other fields ignored).
//	  - CheckResult mirrors the schema's check_result (part, check, passed, reason). Check is the
//	    condition's name, so callers need no mapping. Part is "contract" or "rules" per the pattern
//	    README. Reason is never empty, passing or failing.
//	  - One result per listed condition, passing or failing, so the caller can show what was
//	    checked. A slice fits when every result passed. Pure function: no model, no network, no
//	    clock (constitution XX, research D10), hence plain bytes in and a slice of results out.
//	  - Fail closed: a condition name the check does not implement yields a failing result for that
//	    name (part "contract"); a pattern that cannot be parsed, or a slice that is not valid JSON,
//	    yields at least one failing result and no passing one.
//
// A plain exported struct is used rather than the generated API type so the package does not
// depend on the HTTP layer; its JSON form is the schema's check_result.
package checks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var conditions = []string{"ContractIsOpenApi31", "OnlyHttpOperations", "ContractRefsLocal", "RulesNameOneSystem", "RefusalDeclared", "RequestBodiesSupported"}

var contractConditions = []string{"ContractIsOpenApi31", "OnlyHttpOperations", "ContractRefsLocal", "RefusalDeclared", "RequestBodiesSupported"}

// repoPattern reads the real declaration, found by walking up to the repository's pattern file.
func repoPattern(t *testing.T) []byte {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		p := filepath.Join(dir, "src", "catalogue", "api-feature-go", "pattern.yaml")
		if b, err := os.ReadFile(p); err == nil {
			return b
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("src/catalogue/api-feature-go/pattern.yaml not found above the test directory")
		}
		dir = parent
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "fit", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// slice builds a slice from a contract fixture and a rules fixture.
func slice(t *testing.T, contract, rules string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"rules":    json.RawMessage(fixture(t, rules)),
		"contract": string(fixture(t, contract)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func byCheck(t *testing.T, rs []CheckResult) map[string]CheckResult {
	t.Helper()
	m := map[string]CheckResult{}
	for _, r := range rs {
		if _, dup := m[r.Check]; dup {
			t.Fatalf("two results for %s", r.Check)
		}
		if r.Reason == "" {
			t.Errorf("%s: empty reason", r.Check)
		}
		m[r.Check] = r
	}
	return m
}

// expect asserts exactly the named conditions fail and every other listed condition passes, with
// the right part on each.
func expect(t *testing.T, rs []CheckResult, failing ...string) map[string]CheckResult {
	t.Helper()
	if len(rs) != len(conditions) {
		t.Fatalf("got %d results, want one per condition (%d): %+v", len(rs), len(conditions), rs)
	}
	for i, c := range conditions {
		if rs[i].Check != c {
			t.Errorf("result %d is %s, want %s (pattern order)", i, rs[i].Check, c)
		}
	}
	m := byCheck(t, rs)
	fails := map[string]bool{}
	for _, f := range failing {
		fails[f] = true
	}
	for _, c := range conditions {
		wantPart := "contract"
		if c == "RulesNameOneSystem" {
			wantPart = "rules"
		}
		if m[c].Part != wantPart {
			t.Errorf("%s: part %q, want %q", c, m[c].Part, wantPart)
		}
		if m[c].Passed == fails[c] {
			t.Errorf("%s: passed=%v, want %v (reason %q)", c, m[c].Passed, !fails[c], m[c].Reason)
		}
	}
	return m
}

func mustContain(t *testing.T, r CheckResult, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(r.Reason, p) {
			t.Errorf("%s reason %q does not name %q", r.Check, r.Reason, p)
		}
	}
}

func TestNotesSliceFitsApiFeatureGo(t *testing.T) {
	rs := Fit(repoPattern(t), fixture(t, "notes.json"))
	expect(t, rs)
}

func TestLocalRefsAreAccepted(t *testing.T) {
	expect(t, Fit(repoPattern(t), slice(t, "contract-local-ref.yaml", "rules-notes.json")))
}

func TestWebhookDoesNotFit(t *testing.T) {
	m := expect(t, Fit(repoPattern(t), slice(t, "contract-webhook.yaml", "rules-notes.json")), "OnlyHttpOperations")
	mustContain(t, m["OnlyHttpOperations"], "webhooks/noteAdded")
}

func TestCallbackDoesNotFit(t *testing.T) {
	m := expect(t, Fit(repoPattern(t), slice(t, "contract-callback.yaml", "rules-notes.json")), "OnlyHttpOperations")
	mustContain(t, m["OnlyHttpOperations"], "paths//notes/post/callbacks")
}

func TestRulesNamingAnotherSystemDoNotFit(t *testing.T) {
	m := expect(t, Fit(repoPattern(t), slice(t, "contract-notes.yaml", "rules-two-systems.json")), "RulesNameOneSystem")
	mustContain(t, m["RulesNameOneSystem"], "the notes service", "the billing service", "R-1", "R-2", "R-3")
}

func TestSystemNamesAreComparedExactly(t *testing.T) {
	m := expect(t, Fit(repoPattern(t), slice(t, "contract-notes.yaml", "rules-name-variant.json")), "RulesNameOneSystem")
	mustContain(t, m["RulesNameOneSystem"], "the notes service", "notes service", "R-1", "R-2")
}

func TestNoRulesDoNotFit(t *testing.T) {
	expect(t, Fit(repoPattern(t), slice(t, "contract-notes.yaml", "rules-empty.json")), "RulesNameOneSystem")
}

func TestNotOpenApi31DoesNotFit(t *testing.T) {
	m := expect(t, Fit(repoPattern(t), slice(t, "contract-openapi30.yaml", "rules-notes.json")), "ContractIsOpenApi31")
	mustContain(t, m["ContractIsOpenApi31"], "3.0.3")
}

func TestMissingOpenApiVersionDoesNotFit(t *testing.T) {
	m := expect(t, Fit(repoPattern(t), slice(t, "contract-no-version.yaml", "rules-notes.json")), "ContractIsOpenApi31")
	mustContain(t, m["ContractIsOpenApi31"], "openapi")
}

func TestNonLocalRefDoesNotFit(t *testing.T) {
	m := expect(t, Fit(repoPattern(t), slice(t, "contract-external-ref.yaml", "rules-notes.json")), "ContractRefsLocal")
	mustContain(t, m["ContractRefsLocal"], "paths//notes/post", "$ref")
}

func TestUnreadableContractsFailEveryContractCondition(t *testing.T) {
	for _, name := range []string{"contract-unparseable.yaml", "contract-multi-document.yaml", "contract-duplicate-key.yaml"} {
		t.Run(name, func(t *testing.T) {
			expect(t, Fit(repoPattern(t), slice(t, name, "rules-notes.json")), contractConditions...)
		})
	}
}

func TestUnknownConditionFailsClosed(t *testing.T) {
	p := repoPattern(t)
	edited := strings.Replace(string(p), "  - RulesNameOneSystem\n", "  - RulesNameOneSystem\n  - ModelSaysItFits\n", 1)
	if edited == string(p) {
		t.Fatal("pattern.yaml no longer lists RulesNameOneSystem as a condition line; update this test")
	}
	rs := Fit([]byte(edited), fixture(t, "notes.json"))
	m := byCheck(t, rs)
	if len(rs) != len(conditions)+1 {
		t.Fatalf("got %d results, want %d", len(rs), len(conditions)+1)
	}
	for _, c := range conditions {
		if !m[c].Passed {
			t.Errorf("%s should still pass: %s", c, m[c].Reason)
		}
	}
	r, ok := m["ModelSaysItFits"]
	if !ok || r.Passed {
		t.Fatalf("unknown condition must fail: %+v", r)
	}
	mustContain(t, r, "ModelSaysItFits")
}

func TestUnparseablePatternOrSliceFailsClosed(t *testing.T) {
	for name, args := range map[string][2][]byte{
		"pattern": {[]byte("conditions: [unclosed\n"), nil},
		"slice":   {nil, []byte("{not json")},
	} {
		t.Run(name, func(t *testing.T) {
			p, s := args[0], args[1]
			if p == nil {
				p = repoPattern(t)
			}
			if s == nil {
				s = fixture(t, "notes.json")
			}
			rs := Fit(p, s)
			if len(rs) == 0 {
				t.Fatal("no results: an unreadable input must not read as a fit")
			}
			for _, r := range rs {
				if r.Passed {
					t.Errorf("%s passed on unreadable input", r.Check)
				}
			}
		})
	}
}
