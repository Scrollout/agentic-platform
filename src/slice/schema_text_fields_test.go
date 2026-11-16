// Tests that the proposal's and slice's Gherkin `examples` and OpenAPI `contract` text refuse a
// double-encoded string (first non-whitespace character is a double quote, the value holding an
// escaped JSON string with literal \n), and accept a value with a quote later on.
package slice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func textFieldsFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "text-fields", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTextFieldsDoubleEncoding(t *testing.T) {
	s := loadSchemas(t)
	checks := map[string]func([]byte) error{"proposal": s.CheckProposal, "slice": s.CheckSlice}
	for kind, check := range checks {
		t.Run(kind+"/valid passes", func(t *testing.T) {
			if err := check(textFieldsFixture(t, kind+"-valid")); err != nil {
				t.Errorf("valid %s rejected: %v", kind, err)
			}
		})
		t.Run(kind+"/quote later in the text passes", func(t *testing.T) {
			if err := check(textFieldsFixture(t, kind+"-inner-quotes")); err != nil {
				t.Errorf("%s with a quote inside the text rejected: %v", kind, err)
			}
		})
		for _, field := range []string{"examples", "contract"} {
			for _, tag := range []string{"double-encoded", "leading-space"} {
				name := kind + "-" + field + "-" + tag
				t.Run(name+" fails", func(t *testing.T) {
					err := check(textFieldsFixture(t, name))
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
}
