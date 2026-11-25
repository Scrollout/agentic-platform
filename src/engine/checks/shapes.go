package checks

// The request shapes of the approved contract (catalogue README, "Request check"; ADR 0087,
// ADR 0088): one collector and one renderer that the generate procedure and
// RequestShapesGenerated both use, so they cannot disagree about what a request body may hold.

import "errors"

// RequestShapesFile is the generated file that holds the request shapes.
const RequestShapesFile = "gen/request_shapes.go"

// RequestShape is what the approved contract allows one request body value to hold.
type RequestShape struct {
	// Types are the JSON Schema type names the value may have, from `type`; empty means any.
	Types []string
	// Properties are the declared members: `properties` in contract order, then the names only in
	// `required`, in `required` order, each of any type. Nil when the schema has neither
	// `properties` nor `required` nor `additionalProperties: false` (an object is then kept as
	// sent); `properties: {}` gives a non-nil, empty slice.
	Properties []Member
	// RequiredMembers are the names in `required`, in contract order.
	RequiredMembers []string
	// Closed is `additionalProperties: false`: an undeclared member is refused, not removed.
	Closed bool
	// Items is the shape of each array item; nil means any item.
	Items *RequestShape
}

// Member is one declared member of an object shape, by its exact name.
type Member struct {
	Name  string
	Shape *RequestShape
}

// RequestBody is one operation's request body.
type RequestBody struct {
	// Key is the method in upper case, a space and the path as the contract writes it
	// ("POST /notes"): the key the handler template passes to the request check.
	Key string
	// Required is `requestBody.required`; an empty body is refused only then.
	Required bool
	// Shape is the body's shape.
	Shape *RequestShape
}

var errShapesNotImplemented = errors.New("not implemented")

// RequestShapes finds every operation's request body in contract (the YAML text), in document
// order, following local $refs (a requestBody $ref to #/components/requestBodies included, every
// walk with a visited set); a $ref target is one shared *RequestShape, so a recursive schema is a
// cycle. `properties`, `required` and `additionalProperties` apply to an object value whether or
// not `type` is present. An error means the text is not one YAML document without duplicate keys,
// or it holds a request body the fit condition RequestBodiesSupported refuses; it names the
// location.
func RequestShapes(contract []byte) ([]RequestBody, error) {
	return nil, errShapesNotImplemented
}

// RenderRequestShapes is gen/request_shapes.go for bodies: the same bytes for the same bodies.
// Every shape is an unexported variable requestShape<N> (N from 1, in document order of first
// use) linked in init(), and every key and name is written with strconv.Quote.
func RenderRequestShapes(bodies []RequestBody) []byte {
	return nil
}

// RequestShapesGenerated passes when the project's gen/request_shapes.go equals
// RenderRequestShapes of the approved contract's request bodies; approvedContract is the
// contract's YAML text. It fails naming a missing or differing file, and fails closed, naming
// what it could not read, on a contract RequestShapes rejects.
func RequestShapesGenerated(project string, approvedContract []byte) []CheckResult {
	return []CheckResult{fail("RequestShapesGenerated", "not implemented")}
}
