// Package slice checks slice and proposal documents against the engine's JSON Schemas, and parses
// the values those schemas give a rule of their own (slice and run IDs, hashes) into types that
// hold only valid values.
//
// The module holds no copy of the schemas: callers pass the files of one contract version, as a
// directory (LoadSchemas) or as a file system such as an embedded one (LoadSchemasFS).
package slice

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Schemas holds the compiled schemas of one contract version.
type Schemas struct {
	slice, proposal *jsonschema.Schema
	ids             idRules
}

// LoadSchemas compiles slice.schema.json, slice-proposal.schema.json and the value rules of
// slice-defs.schema.json from dir; references resolve to files in the same dir.
func LoadSchemas(dir string) (*Schemas, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	base := (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs) + "/"}).String()
	return load(os.DirFS(abs), base)
}

// LoadSchemasFS is LoadSchemas for the files at the root of fsys, such as the contract version
// a program embeds. References resolve only to files in fsys.
func LoadSchemasFS(fsys fs.FS) (*Schemas, error) {
	return load(fsys, "file:///contract/")
}

func load(fsys fs.FS, base string) (*Schemas, error) {
	c := jsonschema.NewCompiler()
	c.UseLoader(fsLoader{fsys: fsys, base: base})
	s, err := c.Compile(base + "slice.schema.json")
	if err != nil {
		return nil, err
	}
	p, err := c.Compile(base + "slice-proposal.schema.json")
	if err != nil {
		return nil, err
	}
	ids, err := compileIDRules(c, base+"slice-defs.schema.json")
	if err != nil {
		return nil, err
	}
	return &Schemas{slice: s, proposal: p, ids: ids}, nil
}

// fsLoader loads the schema files under base from fsys, and nothing else.
type fsLoader struct {
	fsys fs.FS
	base string
}

func (l fsLoader) Load(u string) (any, error) {
	rest, ok := strings.CutPrefix(u, l.base)
	name, err := url.PathUnescape(rest)
	if !ok || err != nil || !fs.ValidPath(name) || path.Dir(name) != "." {
		return nil, fmt.Errorf("slice: schema %s is outside the contract", u)
	}
	b, err := fs.ReadFile(l.fsys, name)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(b))
}

// CheckSlice returns nil if doc is a valid slice. Otherwise its error names
// each failing JSON Pointer and reason, nested causes included.
func (s *Schemas) CheckSlice(doc []byte) error { return check(s.slice, doc) }

// CheckProposal returns nil if doc is a valid proposal, with errors as CheckSlice.
func (s *Schemas) CheckProposal(doc []byte) error { return check(s.proposal, doc) }

func check(sch *jsonschema.Schema, doc []byte) error {
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return err
	}
	return sch.Validate(v)
}
