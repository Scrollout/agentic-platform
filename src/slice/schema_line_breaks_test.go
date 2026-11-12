// Tests that the proposal's and slice's Gherkin `examples` and OpenAPI `contract` text must hold a
// real line break after the first non-blank character: text escaped with literal backslash-n
// sequences, one-line text and blank text are refused; comment, tag and JSON starts are accepted.
package slice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lineBreaksFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "line-breaks", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTextFieldsLineBreaks(t *testing.T) {
	s := loadSchemas(t)
	checks := map[string]func([]byte) error{"proposal": s.CheckProposal, "slice": s.CheckSlice}
	passes := []string{"valid", "examples-comment", "contract-comment", "examples-tag", "contract-json"}
	fails := []string{
		"examples-literal-n", "contract-literal-n", "examples-one-line", "contract-one-line",
		"examples-blank", "contract-blank", "examples-spaces", "contract-spaces", "contract-json-one-line",
	}
	for kind, check := range checks {
		for _, tag := range passes {
			name := kind + "-" + tag
			t.Run(name+" passes", func(t *testing.T) {
				if err := check(lineBreaksFixture(t, name)); err != nil {
					t.Errorf("%s rejected: %v", name, err)
				}
			})
		}
		for _, tag := range fails {
			name := kind + "-" + tag
			field := strings.SplitN(tag, "-", 2)[0]
			if field != "examples" {
				field = "contract"
			}
			t.Run(name+" fails", func(t *testing.T) {
				err := check(lineBreaksFixture(t, name))
				if err == nil {
					t.Fatalf("%s accepted", name)
				}
				if !strings.Contains(err.Error(), "/"+field) {
					t.Errorf("error does not name /%s: %v", field, err)
				}
			})
		}
	}
}
