// Tests for LoadSchemasFS (review of the engine group): it must give the same
// rules as LoadSchemas(dir) and must load nothing outside the file system it is given.
package slice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

var idTable = struct{ ids, runs, hashes []string }{
	ids:  []string{"S-1", "S-007", "S-", "s-1", "S-1\n", "S-１", "S-a", "RUN-1", "", "../S-1"},
	runs: []string{"RUN-1", "RUN-0042", "RUN-", "run-1", "RUN-1\n", "S-1", "", "RUN-x"},
	hashes: []string{strings.Repeat("a", 64), strings.Repeat("A", 64), strings.Repeat("a", 63), strings.Repeat("a", 65),
		strings.Repeat("a", 63) + "g", strings.Repeat("a", 64) + "\n", ""},
}

func contractFiles(t *testing.T) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	ents, err := os.ReadDir(schemaDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".schema.json") {
			b, err := os.ReadFile(filepath.Join(schemaDir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			m[e.Name()] = &fstest.MapFile{Data: b}
		}
	}
	return m
}

func outcomes(s *Schemas) (out []bool) {
	for _, v := range idTable.ids {
		_, err := s.ParseID(v)
		out = append(out, err == nil)
	}
	for _, v := range idTable.runs {
		_, err := s.ParseRunID(v)
		out = append(out, err == nil)
	}
	for _, v := range idTable.hashes {
		_, err := s.ParseHash(v)
		out = append(out, err == nil)
	}
	return
}

func TestLoadSchemasFSGivesTheSameRulesAsLoadSchemas(t *testing.T) {
	ref := loadSchemas(t)
	want := outcomes(ref)
	var yes, no int
	for _, w := range want {
		if w {
			yes++
		} else {
			no++
		}
	}
	if yes < 3 || no < 3 {
		t.Fatalf("table is not mixed: %d accepted, %d refused", yes, no)
	}
	for name, load := range map[string]func() (*Schemas, error){
		"os.DirFS":     func() (*Schemas, error) { return LoadSchemasFS(os.DirFS(schemaDir)) },
		"fstest.MapFS": func() (*Schemas, error) { return LoadSchemasFS(contractFiles(t)) },
	} {
		t.Run(name, func(t *testing.T) {
			s, err := load()
			if err != nil {
				t.Fatal(err)
			}
			got := outcomes(s)
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("table entry %d: accepted=%v, LoadSchemas says %v", i, got[i], want[i])
				}
			}
			if err := s.CheckSlice(fixture(t, "slice-valid")); err != nil {
				t.Errorf("valid slice rejected: %v", err)
			}
			if s.CheckSlice(fixture(t, "slice-bad-goal-id")) == nil {
				t.Error("invalid slice accepted")
			}
			if s.CheckProposal(fixture(t, "proposal-with-approvals")) == nil {
				t.Error("invalid proposal accepted")
			}
		})
	}
}

func TestLoadSchemasFSLoadsNothingOutsideItsFileSystem(t *testing.T) {
	for name, ref := range map[string]string{
		"parent folder": "../x.json",
		"absolute path": "/etc/x.json",
		"file URL":      "file:///etc/x.json",
		"http URL":      "http://127.0.0.1:1/x.json",
		"https URL":     "https://example.invalid/x.json",
		"subfolder":     "sub/x.json",
	} {
		t.Run(name, func(t *testing.T) {
			m := contractFiles(t)
			// The outside file exists in every place a lax loader could look.
			m["x.json"] = &fstest.MapFile{Data: []byte(`{}`)}
			m["sub/x.json"] = &fstest.MapFile{Data: []byte(`{}`)}
			m["slice.schema.json"] = &fstest.MapFile{Data: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$ref":"` + ref + `"}`)}
			if s, err := LoadSchemasFS(m); err == nil {
				t.Fatalf("loaded a schema that references %q: %v", ref, s)
			}
		})
	}
	t.Run("control: a reference to a sibling file loads", func(t *testing.T) {
		m := contractFiles(t)
		m["x.json"] = &fstest.MapFile{Data: []byte(`{}`)}
		m["slice.schema.json"] = &fstest.MapFile{Data: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$ref":"x.json"}`)}
		if _, err := LoadSchemasFS(m); err != nil {
			t.Fatal(err)
		}
	})
}

func TestLoadSchemasFSFailsOnAMissingFile(t *testing.T) {
	m := contractFiles(t)
	delete(m, "slice-defs.schema.json")
	if _, err := LoadSchemasFS(m); err == nil {
		t.Fatal("loaded without slice-defs.schema.json")
	}
}
