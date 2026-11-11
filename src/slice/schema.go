// Package slice checks slice and proposal documents against the engine's JSON Schemas.
package slice

import (
	"bytes"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Schemas holds the compiled slice and proposal schemas of one contract version.
type Schemas struct {
	slice, proposal *jsonschema.Schema
}

// LoadSchemas compiles slice.schema.json and slice-proposal.schema.json from
// dir; their references resolve to slice-defs.schema.json in the same dir.
func LoadSchemas(dir string) (*Schemas, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	s, err := c.Compile(filepath.Join(abs, "slice.schema.json"))
	if err != nil {
		return nil, err
	}
	p, err := c.Compile(filepath.Join(abs, "slice-proposal.schema.json"))
	if err != nil {
		return nil, err
	}
	return &Schemas{slice: s, proposal: p}, nil
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
