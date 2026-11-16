// Regression tests for T071 (review of the surviving mutants): slices.New refuses contract
// schemas that set no free-answer maxLength, or no questions maxItems, each on its own, since the
// service would have no limit to enforce. Seam: slice.LoadSchemasFS over an in-memory copy of the
// schemas with one keyword removed (lfsDir is in limits_from_schema_test.go).
package slices_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/slice"
)

// lrSchemas loads the contract's schemas with the text cut from the named file.
func lrSchemas(t *testing.T, file, cut string) *slice.Schemas {
	t.Helper()
	m := fstest.MapFS{}
	ents, err := os.ReadDir(lfsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".schema.json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(lfsDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if e.Name() == file {
			if n := strings.Count(s, cut); n != 1 {
				t.Fatalf("%s: %q occurs %d times, want once", file, cut, n)
			}
			s = strings.Replace(s, cut, "", 1)
		}
		m[e.Name()] = &fstest.MapFile{Data: []byte(s)}
	}
	sc, err := slice.LoadSchemasFS(m)
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func TestNewRefusesSchemasWithoutALimit(t *testing.T) {
	pattern, err := os.ReadFile("../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string][2]string{
		"no answer maxLength":   {"slice-defs.schema.json", `"maxLength": 200,`},
		"no questions maxItems": {"slice.schema.json", `"maxItems": 10,`},
	} {
		t.Run(name, func(t *testing.T) {
			sc := lrSchemas(t, c[0], c[1])
			a := &fakeAnalyst{results: []analystResult{{p: notesProposal(t)}}}
			if _, err := slices.New(slices.Config{Schemas: sc, Analyst: a, Diff: &fakeDiff{}, Pattern: pattern, Lint: &fakeLint{}}); err == nil {
				t.Error("New accepted schemas that set no such limit")
			}
		})
	}
}
