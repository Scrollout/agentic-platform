// Package captured owns the rules of captured patterns (feature 011, research R1 and R4): which
// of a slice's values are proposed as parameters, the form each value must take, where each
// occurs, whether a later slice fits a captured pattern, and how its template is filled. The
// engine API (proposal, parameters), the run's pattern choice (fit) and the run workflow (the
// `fill` steps) call it; no other package judges these rules.
//
// The rules, stated once here and in the engine contract:
//
//   - A parameter's kind fixes the form of every value it takes, the source's and a later
//     slice's. A name is [a-z][a-z0-9]* with at least two letters (its lowercase form); it covers
//     that form, the capitalised form and the uppercase form, each replaced wherever it occurs,
//     inside longer words too (`NewNote`, `note_id`, `denote`); a plural is its own parameter, and
//     any other spelling (`nOTE`) is not replaced. A number is an integer or a decimal without a
//     sign, matched only as a whole number: the characters around it are not digits, letters or
//     `_`, and not a `.` followed by a digit (`200` is not in `2001`, `1.200`, `200ms` or `v200`;
//     it is in `-200` and `"200"`). A string is letters, digits, `_`, `-` and spaces. Every value
//     is at most ValueMaxChars characters.
//   - Places are found longest source form first, left to right, never overlapping, in contents
//     and in template file paths. Two parameters whose values are equal in any case form are
//     refused.
//   - A slice fits when one value per parameter, read from the first place of each and of its
//     form, fills the source slice's approved parts into exactly the approved slice's parts, no
//     two parameters take equal values, and the filled template paths are distinct and each
//     inside its step's writable entries.
//
// Limits: at most ParametersMax parameters, PlacesMax places per parameter, ValueMaxChars
// characters per value. A value that occurs where it was not meant is replaced everywhere; the
// proposal lists every place so the developer can drop such a parameter.
package captured

import (
	"errors"

	"github.com/agentic-platform/engine/canonical"
)

// The engine contract's limits and value rules (x-limits and slice-defs.schema.json), held here
// as named values; a contract test proves they equal the contract's.
const (
	// ParametersMax is x-limits.captured-pattern-parameters-max.
	ParametersMax = 32
	// PlacesMax is x-limits.parameter-places-max.
	PlacesMax = 10000
	// ValueMaxChars is x-limits.parameter-value-max-chars.
	ValueMaxChars = 64
	// ParameterNamePattern is slice-defs `parameter_name`; ParameterNameMaxChars its maxLength.
	ParameterNamePattern  = `^[a-z][a-z0-9-]*$`
	ParameterNameMaxChars = 64
	// ParameterValuePattern is slice-defs `parameter_value`, the union of the kinds' forms.
	ParameterValuePattern = `^[A-Za-z0-9_. -]+$`
	// CaptureNamePattern is slice-defs `capture_name`; CaptureNameMaxChars its maxLength.
	CaptureNamePattern  = `^[a-z0-9]+(-[a-z0-9]+)*$`
	CaptureNameMaxChars = 40
)

// Kind is a parameter's kind (engine contract, ParameterKind).
type Kind string

const (
	KindName   Kind = "name"
	KindNumber Kind = "number"
	KindString Kind = "string"
)

// Source is where a parameter's value came from (engine contract, ParameterSource).
type Source string

const (
	SourceName          Source = "name"
	SourceDeclaredValue Source = "declared-value"
	SourceLiteral       Source = "literal"
)

// Origin says whether the capture proposed a parameter or the developer added it.
type Origin string

const (
	OriginProposed Origin = "proposed"
	OriginAdded    Origin = "added"
)

// Form is the case form found at a place; FormExact for a number or a string.
type Form string

const (
	FormLower       Form = "lower"
	FormCapitalised Form = "capitalised"
	FormUpper       Form = "upper"
	FormExact       Form = "exact"
)

// In says whether a place is in a part's or file's content, or in a file's path.
type In string

const (
	InContent In = "content"
	InPath    In = "path"
)

// Place is one occurrence of a parameter's value. Part is `slice/goals`, `slice/rules` or
// `slice/contract` for a part of the slice copy, or a template file's path. Line and Column count
// from 1; for a place in a path, Line is 1 and Column the character in the path.
type Place struct {
	Part   string
	In     In
	Line   int
	Column int
	Form   Form
}

// Parameter is one parameter of a captured pattern: its name, kind, source value and every place
// that value occurs.
type Parameter struct {
	Name   string
	Kind   Kind
	Value  string
	Source Source
	Origin Origin
	Places []Place
}

// Copy is a slice's approved parts, each as its RFC 8785 canonical JSON (the text the part hash
// is taken over): the source slice's copy a captured pattern keeps, or a later slice to fit.
type Copy struct {
	Goals, Rules, Contract []byte
}

// Template is a captured pattern's filled outputs: per output name (`tests`, `code`,
// `witnesses`), the files the source run's project ended with.
type Template map[string][]canonical.File

// Refused is the error Check gives, and the refusal Fit gives for a value: the parameter and why
// (the contract's ParameterRefused, and `not_used`'s `parameter` and `detail`).
type Refused struct {
	Parameter string
	Detail    string
}

func (r *Refused) Error() string { return r.Parameter + ": " + r.Detail }

// Refusal is why a slice does not fit: either Place, the first place the approved slice differs
// from the filled source, or Parameter, a value refused (Parameter.Parameter and Detail). Paths
// names the template paths of a path refusal (two files filled to one path, or a filled path
// outside its step's writable entries).
type Refusal struct {
	Place     *Place
	Parameter *Refused
	Paths     []string
	Detail    string
}

// ErrNotImplemented was the answer of a function before its feature gave it its work; no
// function answers it now. Tests use it to tell a stub's answer from the interface's own.
var ErrNotImplemented = errors.New("captured: not implemented yet")
