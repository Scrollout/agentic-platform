package checks

// The declared values of the approved contract (catalogue README, "Declared values"; ADR 0084,
// ADR 0086): one collector that the generate procedure, DeclaredValuesGenerated and
// DeclaredValuesNotRepeated all use, so they cannot disagree about what a declared value is.

import (
	"errors"
	"go/constant"
)

// errNotImplemented marks the contract's Go interface before its bodies exist (feature 009,
// T001); every caller fails closed on it.
var errNotImplemented = errors.New("not implemented")

// Declared is one value the contract states under a schema keyword.
type Declared struct {
	// Pointer is the RFC 6901 JSON Pointer of the keyword's value, for example
	// /components/schemas/NewNote/properties/text/maxLength.
	Pointer string
	// Keyword is the schema keyword (maxLength ... const), or enum for an item of an enum.
	Keyword string
	// Kind is "number", "string", "bool" or "null", or "unjudged" for a number no Go constant
	// reads the same way (.inf, .nan, 010, 0x1F, 0o17, 1_000).
	Kind string
	// Text is the scalar's text as written in the contract.
	Text string
	// Value is the number or string; nil for bool, null and unjudged.
	Value constant.Value
	// Name is the Go constant in gen/declared.go; "" for enum items, bool, null and unjudged.
	Name string
}

// DeclaredValues finds every declared value of contract (the YAML text), in document order, and
// names them, avoiding the names in taken (the top-level names of the other files in gen/).
// An error means the text is not one YAML document without duplicate keys.
func DeclaredValues(contract []byte, taken map[string]bool) ([]Declared, error) {
	return nil, errNotImplemented
}

// RenderDeclared is gen/declared.go for values: the same bytes for the same values.
func RenderDeclared(values []Declared) []byte {
	return nil
}

// DeclaredValuesGenerated passes when the project's gen/declared.go equals RenderDeclared of the
// approved contract's values (named avoiding the project's other gen/ files' top-level names).
// approvedContract is the contract's YAML text.
func DeclaredValuesGenerated(project string, approvedContract []byte) []CheckResult {
	return []CheckResult{fail("DeclaredValuesGenerated", "the check is not implemented")}
}
