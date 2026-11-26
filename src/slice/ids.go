package slice

import (
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ID is a slice ID. Its only constructor is Schemas.ParseID, so a non-zero ID always meets the
// contract's slice_id rule.
type ID struct{ s string }

// RunID is a run ID, made only by Schemas.ParseRunID (the contract's run_id rule).
type RunID struct{ s string }

// Hash is a SHA-256 hash in lowercase hex, made only by Schemas.ParseHash (the contract's hash
// rule).
type Hash struct{ s string }

// PatternID is a pattern's ID, made only by Schemas.ParsePatternID (the contract's pattern_id
// rule): a base pattern's or a captured pattern's.
type PatternID struct{ s string }

func (id ID) String() string    { return id.s }
func (id RunID) String() string { return id.s }
func (h Hash) String() string   { return h.s }

func (id PatternID) String() string { return id.s }

// IsZero reports whether id was never parsed.
func (id ID) IsZero() bool { return id.s == "" }

// IsZero reports whether id was never parsed.
func (id RunID) IsZero() bool { return id.s == "" }

// IsZero reports whether h was never parsed.
func (h Hash) IsZero() bool { return h.s == "" }

// idRules are the value rules of slice-defs.schema.json the ID types parse against. The
// definitions themselves are the rule: they are compiled and applied as the schemas apply them,
// so no pattern is repeated here.
type idRules struct {
	slice, run, hash, pattern *jsonschema.Schema
}

func compileIDRules(c *jsonschema.Compiler, defs string) (idRules, error) {
	var r idRules
	for _, d := range []struct {
		name string
		to   **jsonschema.Schema
	}{{"slice_id", &r.slice}, {"run_id", &r.run}, {"hash", &r.hash}, {"pattern_id", &r.pattern}} {
		s, err := c.Compile(defs + "#/$defs/" + d.name)
		if err != nil {
			return idRules{}, err
		}
		*d.to = s
	}
	return r, nil
}

// ParseID returns v as a slice ID, or an error if v breaks the contract's slice_id rule.
func (s *Schemas) ParseID(v string) (ID, error) {
	if err := parse(s.ids.slice, "slice ID", v); err != nil {
		return ID{}, err
	}
	return ID{v}, nil
}

// ParseRunID returns v as a run ID, or an error if v breaks the contract's run_id rule.
func (s *Schemas) ParseRunID(v string) (RunID, error) {
	if err := parse(s.ids.run, "run ID", v); err != nil {
		return RunID{}, err
	}
	return RunID{v}, nil
}

// ParseHash returns v as a hash, or an error if v breaks the contract's hash rule.
func (s *Schemas) ParseHash(v string) (Hash, error) {
	if err := parse(s.ids.hash, "hash", v); err != nil {
		return Hash{}, err
	}
	return Hash{v}, nil
}

// ParsePatternID returns v as a pattern ID, or an error if v breaks the contract's pattern_id
// rule.
func (s *Schemas) ParsePatternID(v string) (PatternID, error) {
	if err := parse(s.ids.pattern, "pattern ID", v); err != nil {
		return PatternID{}, err
	}
	return PatternID{v}, nil
}

func parse(rule *jsonschema.Schema, what, v string) error {
	if rule.Validate(v) == nil {
		return nil
	}
	if rule.Pattern != nil {
		return fmt.Errorf("invalid %s %q: it must match %s", what, v, rule.Pattern)
	}
	return fmt.Errorf("invalid %s %q", what, v)
}
