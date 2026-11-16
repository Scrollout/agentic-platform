// Tests for validating slice and proposal documents against the engine's JSON
// Schemas (task T013). They fail until T016 implements schema.go.
//
// API the tests call (package slice):
//
//	func LoadSchemas(dir string) (*Schemas, error)
//	func (s *Schemas) CheckSlice(doc []byte) error
//	func (s *Schemas) CheckProposal(doc []byte) error
//
// LoadSchemas reads slice.schema.json, slice-proposal.schema.json and
// slice-defs.schema.json from dir. The slice module does not import the engine
// and holds no copy of the schemas: callers pass the directory of the engine's
// contract version (the tests use ../engine/api/preview/2026-11-12-preview).
// Check* returns nil for a valid JSON document. Otherwise it returns an error
// whose message holds the JSON Pointer of the failing part (for example
// "/rules/0") and the reason, naming the property or limit involved (for
// example "precondition"). Malformed JSON also returns an error.
// The implementation needs github.com/santhosh-tekuri/jsonschema/v6.
package slice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const schemaDir = "../engine/api/preview/2026-11-12-preview"

func loadSchemas(t *testing.T) *Schemas {
	t.Helper()
	s, err := LoadSchemas(schemaDir)
	if err != nil {
		t.Fatalf("LoadSchemas(%q): %v", schemaDir, err)
	}
	return s
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCleanDocumentsPass(t *testing.T) {
	s := loadSchemas(t)
	if err := s.CheckSlice(fixture(t, "slice-valid")); err != nil {
		t.Errorf("clean slice rejected: %v", err)
	}
	if err := s.CheckProposal(fixture(t, "proposal-valid")); err != nil {
		t.Errorf("clean proposal rejected: %v", err)
	}
}

// Each planted fixture breaks exactly one rule; the error must say where
// (path) and why (reason).
func TestPlantedFixturesAreRejected(t *testing.T) {
	cases := []struct {
		fixture  string
		proposal bool
		path     string
		reason   string
	}{
		{"slice-state-no-precondition", false, "/rules/0", "precondition"},
		{"slice-event-no-trigger", false, "/rules/0", "trigger"},
		{"slice-unwanted-no-trigger", false, "/rules/0", "trigger"},
		{"slice-option-no-feature", false, "/rules/0", "feature"},
		{"slice-bad-goal-id", false, "/goals/0/id", "G-"},
		{"slice-too-many-questions", false, "/questions", "10"},
		{"slice-long-answer", false, "/questions/0/answer", "200"},
		{"proposal-with-approvals", true, "", "approvals"},
		{"proposal-state-no-precondition", true, "/rules/0", "precondition"},
	}
	s := loadSchemas(t)
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			doc := fixture(t, c.fixture)
			var err error
			if c.proposal {
				err = s.CheckProposal(doc)
			} else {
				err = s.CheckSlice(doc)
			}
			if err == nil {
				t.Fatal("document accepted, want rejection")
			}
			msg := err.Error()
			if !strings.Contains(msg, c.path) || !strings.Contains(msg, c.reason) {
				t.Errorf("error %q lacks path %q or reason %q", msg, c.path, c.reason)
			}
		})
	}
}

func TestMalformedJSONIsRejected(t *testing.T) {
	s := loadSchemas(t)
	if s.CheckSlice([]byte("{")) == nil || s.CheckProposal([]byte("{")) == nil {
		t.Error("malformed JSON accepted")
	}
}
