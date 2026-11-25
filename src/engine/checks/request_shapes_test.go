// T003 (feature 010): failing tests, by a separate author, for the request-shape collector
// (RequestShapes), gen/request_shapes.go (RenderRequestShapes) and the generate-step check
// RequestShapesGenerated, and the pinned GeneratedCodeUnchanged cases for the three files of the
// request check. The contract they follow is the catalogue README ("Fit conditions", "Request
// check"), specs/010-strict-requests/contracts/strict-requests.md and the Go doc comments of
// shapes.go. Pure Go over text and temp folders: no model, no network, no Docker, no go build.
//
// Choice on "builds and type-checks with oapi-codegen's output": the rendered file is NOT
// standalone. Its types requestBody and requestShape come from the template's gen/requests.go,
// which does not exist yet, and their field names are not stated by the README. So the test
// type-checks (go/types) the rendered file together with a stub file that declares guessed types
// and oapi-codegen-like names (NewNote, RequestShape1, ...). It tolerates the errors that come
// only from the guessed field and type names, and fails on a syntax error, a redeclaration (a
// collision with generated names) and an initialization cycle (a recursive schema built without
// the init() links). The real compile, with the real template, is T004's test.
package checks_test

import (
	"bytes"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/checks"
)

// ---- helpers ---------------------------------------------------------------------------------

// rqDoc is a contract with one POST /notes whose request body schema is the given YAML.
func rqDoc(schema string) string {
	return rqDocPaths("  /notes:\n    post:\n      requestBody:\n        required: true\n        content:\n          application/json:\n            schema:\n" +
		rqIndent(schema, 14) + "      responses:\n        '201':\n          description: ok\n")
}

func rqDocPaths(paths string) string {
	return "openapi: 3.1.0\npaths:\n" + paths
}

func rqIndent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString(pad + l + "\n")
	}
	return b.String()
}

// rqWithSchemas appends components.schemas to a contract.
func rqWithSchemas(doc, schemas string) string {
	return doc + "components:\n  schemas:\n" + rqIndent(schemas, 4)
}

func rqBodies(t *testing.T, contract string) []checks.RequestBody {
	t.Helper()
	bodies, err := checks.RequestShapes([]byte(contract))
	if err != nil {
		t.Fatalf("RequestShapes: want the bodies of a valid contract, got error %v\ncontract:\n%s", err, contract)
	}
	return bodies
}

func rqShape(t *testing.T, schema string) *checks.RequestShape {
	t.Helper()
	bodies := rqBodies(t, rqDoc(schema))
	if len(bodies) != 1 || bodies[0].Shape == nil {
		t.Fatalf("want one body with a shape, got %+v", bodies)
	}
	return bodies[0].Shape
}

func rqNames(ms []checks.Member) []string {
	out := []string{}
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}

func rqMember(t *testing.T, s *checks.RequestShape, name string) *checks.RequestShape {
	t.Helper()
	for _, m := range s.Properties {
		if m.Name == name {
			if m.Shape == nil {
				t.Fatalf("member %q has a nil Shape", name)
			}
			return m.Shape
		}
	}
	t.Fatalf("want a member %q, got %q", name, rqNames(s.Properties))
	return nil
}

// rqSame compares two shapes by content, safely on cycles.
func rqSame(a, b *checks.RequestShape, seen map[[2]*checks.RequestShape]bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a == b || seen[[2]*checks.RequestShape{a, b}] {
		return true
	}
	seen[[2]*checks.RequestShape{a, b}] = true
	if !slices.Equal(a.Types, b.Types) || !slices.Equal(a.RequiredMembers, b.RequiredMembers) || a.Closed != b.Closed ||
		(a.Properties == nil) != (b.Properties == nil) || len(a.Properties) != len(b.Properties) {
		return false
	}
	for i := range a.Properties {
		if a.Properties[i].Name != b.Properties[i].Name || !rqSame(a.Properties[i].Shape, b.Properties[i].Shape, seen) {
			return false
		}
	}
	return rqSame(a.Items, b.Items, seen)
}

// rqWithin fails the test when RequestShapes does not return in time (a walk without a visited set).
func rqWithin(t *testing.T, contract string) ([]checks.RequestBody, error) {
	t.Helper()
	type res struct {
		b   []checks.RequestBody
		err error
	}
	ch := make(chan res, 1)
	go func() {
		b, err := checks.RequestShapes([]byte(contract))
		ch <- res{b, err}
	}()
	select {
	case r := <-ch:
		return r.b, r.err
	case <-time.After(10 * time.Second):
		t.Fatalf("RequestShapes did not end in 10s on a cyclic contract:\n%s", contract)
		return nil, nil
	}
}

// ---- RequestShapes: which bodies -------------------------------------------------------------

func TestRequestShapesKeysRequiredAndOrder(t *testing.T) {
	contract := rqDocPaths(`  /zebra:
    parameters: []
    summary: not an operation
    post:
      requestBody:
        content:
          application/json:
            schema: {type: object}
      responses: {}
  /notes/{id}:
    get:
      responses: {}
    put:
      requestBody:
        required: true
        content:
          application/json:
            schema: {type: string}
      responses: {}
    delete:
      responses: {}
    patch:
      requestBody:
        required: false
        content:
          application/json:
            schema: {type: array}
      responses: {}
  /alpha:
    x-extra: {post: not-an-operation}
    post:
      requestBody:
        required: true
        content:
          application/json:
            schema: {type: boolean}
      responses: {}
`)
	bodies := rqBodies(t, contract)
	var keys []string
	req := map[string]bool{}
	for _, b := range bodies {
		keys = append(keys, b.Key)
		req[b.Key] = b.Required
		if b.Shape == nil {
			t.Errorf("%s: nil Shape", b.Key)
		}
	}
	want := []string{"POST /zebra", "PUT /notes/{id}", "PATCH /notes/{id}", "POST /alpha"}
	if !slices.Equal(keys, want) {
		t.Fatalf("want keys %q in document order (one per operation with a requestBody), got %q", want, keys)
	}
	if req["POST /zebra"] || req["PATCH /notes/{id}"] || !req["PUT /notes/{id}"] || !req["POST /alpha"] {
		t.Errorf("Required must follow requestBody.required (absent is false), got %v", req)
	}
}

func TestRequestShapesNoBodiesIsEmptyNotAnError(t *testing.T) {
	bodies, err := checks.RequestShapes([]byte(rqDocPaths("  /notes:\n    get:\n      responses: {}\n")))
	if err != nil || len(bodies) != 0 {
		t.Fatalf("a contract with no requestBody: want no bodies and no error, got %v, %v", bodies, err)
	}
}

func TestRequestShapesIsDeterministic(t *testing.T) {
	c := rqDoc("type: object\nproperties:\n  a: {type: string}\n  b: {type: integer}\n")
	if a, b := rqBodies(t, c), rqBodies(t, c); !rqSame(a[0].Shape, b[0].Shape, map[[2]*checks.RequestShape]bool{}) || a[0].Key != b[0].Key {
		t.Errorf("two derivations differ: %+v vs %+v", a, b)
	}
}

// ---- RequestShapes: types, members, closed, items --------------------------------------------

func TestRequestShapesTypes(t *testing.T) {
	cases := []struct {
		name, schema string
		want         []string
	}{
		{"a name", "type: string\n", []string{"string"}},
		{"a list", "type: [string, \"null\"]\n", []string{"string", "null"}},
		{"null first keeps its order", "type: [\"null\", string]\n", []string{"null", "string"}},
		{"a block list of one type and null", "type:\n  - integer\n  - \"null\"\n", []string{"integer", "null"}},
		{"a one-item list", "type: [object]\n", []string{"object"}},
		{"absent", "properties:\n  a: {type: string}\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := rqShape(t, c.schema).Types
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("Types: want %q, got %q", c.want, got)
			}
		})
	}
}

// A type list is one type and "null" only (commit 8285821): any other list is refused.
func TestRequestShapesTypeListsOtherThanOneTypeAndNullAreRefused(t *testing.T) {
	for name, list := range map[string]string{
		"block list of three": "type:\n  - integer\n  - number\n  - boolean\n",
		"all seven names":     "type: [\"null\", boolean, object, array, number, string, integer]\n",
		"two non-null types":  "type: [integer, number]\n",
		"two types and null":  "type: [string, integer, \"null\"]\n",
		"null twice":          "type: [\"null\", \"null\"]\n",
		"the same type twice": "type: [string, string]\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := checks.RequestShapes([]byte(rqDoc(list)))
			if err == nil || !strings.Contains(err.Error(), "paths//notes/post") {
				t.Errorf("want an error naming paths//notes/post, got %v", err)
			}
		})
	}
}

func TestRequestShapesPropertiesKeepContractOrderAndExactNames(t *testing.T) {
	s := rqShape(t, `type: object
required: [zed, text]
properties:
  zed: {type: string}
  text: {type: string}
  Text: {type: integer}
  alpha: {type: boolean}
`)
	if got, want := rqNames(s.Properties), []string{"zed", "text", "Text", "alpha"}; !slices.Equal(got, want) {
		t.Errorf("Properties: want %q (contract order, text and Text two members), got %q", want, got)
	}
	if got := rqMember(t, s, "text").Types; !slices.Equal(got, []string{"string"}) {
		t.Errorf("text: want [string], got %q", got)
	}
	if got := rqMember(t, s, "Text").Types; !slices.Equal(got, []string{"integer"}) {
		t.Errorf("Text: want [integer], got %q", got)
	}
	if want := []string{"zed", "text"}; !slices.Equal(s.RequiredMembers, want) {
		t.Errorf("RequiredMembers: want %q in contract order, got %q", want, s.RequiredMembers)
	}
}

func TestRequestShapesNamesOnlyInRequiredFollowThePropertiesInRequiredOrder(t *testing.T) {
	s := rqShape(t, `required: [zz, b, aa, a]
properties:
  b: {type: string}
  a: {type: integer}
`)
	if got, want := rqNames(s.Properties), []string{"b", "a", "zz", "aa"}; !slices.Equal(got, want) {
		t.Fatalf("Properties: want %q (properties, then names only in required in required order, none twice), got %q", want, got)
	}
	for _, n := range []string{"zz", "aa"} {
		m := rqMember(t, s, n)
		if len(m.Types) != 0 || m.Properties != nil || m.Items != nil || m.Closed || len(m.RequiredMembers) != 0 {
			t.Errorf("a name only in required is a member of any type, got %+v", *m)
		}
	}
	if want := []string{"zz", "b", "aa", "a"}; !slices.Equal(s.RequiredMembers, want) {
		t.Errorf("RequiredMembers: want %q, got %q", want, s.RequiredMembers)
	}
}

func TestRequestShapesPropertiesAndRequiredApplyWithoutType(t *testing.T) {
	s := rqShape(t, "required: [text]\nproperties:\n  text: {type: string}\n")
	if len(s.Types) != 0 {
		t.Errorf("no type: want empty Types, got %q", s.Types)
	}
	if rqNames(s.Properties)[0] != "text" || !slices.Equal(s.RequiredMembers, []string{"text"}) {
		t.Errorf("properties and required must apply without type, got %+v", s)
	}
}

func TestRequestShapesPropertiesNilOrEmpty(t *testing.T) {
	cases := []struct {
		name, schema string
		wantNil      bool
		wantNames    []string
		closed       bool
	}{
		{"type object alone", "type: object\n", true, nil, false},
		{"only a type string", "type: string\n", true, nil, false},
		{"empty schema object", "{}\n", true, nil, false},
		{"additionalProperties true", "type: object\nadditionalProperties: true\n", true, nil, false},
		{"properties empty", "type: object\nproperties: {}\n", false, []string{}, false},
		{"additionalProperties false alone: closed, every member undeclared", "type: object\nadditionalProperties: false\n", false, []string{}, true},
		{"additionalProperties false without a type", "additionalProperties: false\n", false, []string{}, true},
		{"required alone", "required: [a]\n", false, []string{"a"}, false},
		{"properties and closed", "properties:\n  a: {}\nadditionalProperties: false\n", false, []string{"a"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := rqShape(t, c.schema)
			if (s.Properties == nil) != c.wantNil {
				t.Fatalf("Properties nil = %v, want %v (%+v)", s.Properties == nil, c.wantNil, s.Properties)
			}
			if !c.wantNil && !slices.Equal(rqNames(s.Properties), c.wantNames) {
				t.Errorf("Properties: want %q, got %q", c.wantNames, rqNames(s.Properties))
			}
			if s.Closed != c.closed {
				t.Errorf("Closed: want %v, got %v", c.closed, s.Closed)
			}
		})
	}
}

func TestRequestShapesNestedObjectsAndItems(t *testing.T) {
	s := rqShape(t, `type: object
properties:
  tags:
    type: array
    items: {type: string}
  anything: {type: array}
  rows:
    type: array
    items:
      type: object
      required: [id]
      properties:
        id: {type: integer}
      additionalProperties: false
  matrix:
    type: array
    items:
      type: array
      items: {type: number}
`)
	if tags := rqMember(t, s, "tags"); tags.Items == nil || !slices.Equal(tags.Items.Types, []string{"string"}) {
		t.Errorf("tags.Items: want a string shape, got %+v", tags.Items)
	}
	if rqMember(t, s, "anything").Items != nil {
		t.Errorf("an array without items: Items must be nil (any item)")
	}
	rows := rqMember(t, s, "rows").Items
	if rows == nil || !rows.Closed || !slices.Equal(rows.RequiredMembers, []string{"id"}) || len(rows.Properties) != 1 {
		t.Errorf("rows.Items: want a closed object requiring id, got %+v", rows)
	}
	if m := rqMember(t, s, "matrix").Items; m == nil || m.Items == nil || !slices.Equal(m.Items.Types, []string{"number"}) {
		t.Errorf("matrix: want array of array of number, got %+v", m)
	}
	if s.Closed || s.Items != nil {
		t.Errorf("the root is neither closed nor an array: %+v", s)
	}
}

func TestRequestShapesClosedOnlyForFalse(t *testing.T) {
	if rqShape(t, "type: object\nadditionalProperties: true\nproperties: {a: {}}\n").Closed {
		t.Error("additionalProperties: true must not be Closed")
	}
	if !rqShape(t, "type: object\nadditionalProperties: false\nproperties: {a: {}}\n").Closed {
		t.Error("additionalProperties: false must be Closed")
	}
	if rqShape(t, "type: object\nproperties: {a: {}}\n").Closed {
		t.Error("an absent additionalProperties must not be Closed")
	}
}

func TestRequestShapesIgnoreValueKeywordsAnnotationsAndExtensions(t *testing.T) {
	plain := rqShape(t, "type: object\nrequired: [a]\nproperties:\n  a: {type: string}\n  n: {type: integer}\n  l: {type: array, items: {type: string}}\n")
	noisy := rqShape(t, `type: object
title: T
description: d
$comment: c
deprecated: true
readOnly: true
writeOnly: false
example: {a: x}
examples: [{a: x}]
default: {a: x}
x-internal: {allOf: [1], nullable: true}
minProperties: 1
maxProperties: 5
enum: [{a: x}]
const: {a: x}
required: [a]
properties:
  a: {type: string, maxLength: 5, minLength: 1, pattern: '^a', format: uuid, title: A, default: abc, enum: [abc], const: abc, x-go-type: string}
  n: {type: integer, minimum: 0, maximum: 9, exclusiveMinimum: 0, exclusiveMaximum: 10, multipleOf: 3, default: 3}
  l: {type: array, maxItems: 3, minItems: 1, uniqueItems: true, items: {type: string, maxLength: 2}}
`)
	if !rqSame(plain, noisy, map[[2]*checks.RequestShape]bool{}) {
		t.Errorf("value keywords, annotations and x- extensions must not change the shape:\nplain %+v\nnoisy %+v", plain, noisy)
	}
}

func TestRequestShapesAKeywordNameAsAPropertyNameIsAMember(t *testing.T) {
	s := rqShape(t, "type: object\nproperties:\n  allOf: {type: string}\n  nullable: {type: string}\n  $ref: {type: string}\n  not: {type: string}\n")
	if got, want := rqNames(s.Properties), []string{"allOf", "nullable", "$ref", "not"}; !slices.Equal(got, want) {
		t.Errorf("property names are never judged as keywords: want %q, got %q", want, got)
	}
}

// ---- RequestShapes: $ref ---------------------------------------------------------------------

func TestRequestShapesALocalRefTargetIsOneSharedShape(t *testing.T) {
	contract := rqWithSchemas(rqDocPaths(`  /notes:
    post:
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/NewNote'}
      responses: {}
  /other:
    post:
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                first: {$ref: '#/components/schemas/NewNote'}
                second: {$ref: '#/components/schemas/NewNote'}
      responses: {}
`), `NewNote:
  type: object
  required: [text]
  properties:
    text: {type: string}
`)
	bodies := rqBodies(t, contract)
	if len(bodies) != 2 {
		t.Fatalf("want 2 bodies, got %d", len(bodies))
	}
	root := bodies[0].Shape
	if len(root.Properties) != 1 || root.Properties[0].Name != "text" {
		t.Fatalf("the $ref must be followed, got %+v", root)
	}
	first, second := rqMember(t, bodies[1].Shape, "first"), rqMember(t, bodies[1].Shape, "second")
	if root != first || first != second {
		t.Errorf("a $ref target must be one shared shape: pointers %p, %p, %p", root, first, second)
	}
}

func TestRequestShapesFollowsARequestBodyRef(t *testing.T) {
	contract := `openapi: 3.1.0
paths:
  /notes:
    post:
      requestBody: {$ref: '#/components/requestBodies/NoteBody'}
      responses: {}
  /more:
    put:
      requestBody: {$ref: '#/components/requestBodies/NoteBody'}
      responses: {}
components:
  requestBodies:
    NoteBody:
      required: true
      content:
        application/json:
          schema:
            type: object
            required: [text]
            properties:
              text: {type: string}
`
	bodies := rqBodies(t, contract)
	if len(bodies) != 2 || bodies[0].Key != "POST /notes" || bodies[1].Key != "PUT /more" {
		t.Fatalf("want POST /notes and PUT /more, got %+v", bodies)
	}
	for _, b := range bodies {
		if !b.Required || b.Shape == nil || !slices.Equal(rqNames(b.Shape.Properties), []string{"text"}) {
			t.Errorf("%s: want Required and the text member through the requestBody $ref, got %+v", b.Key, b)
		}
	}
}

func TestRequestShapesARecursiveSchemaIsACycle(t *testing.T) {
	contract := rqWithSchemas(rqDoc("$ref: '#/components/schemas/Node'\n"), `Node:
  type: object
  properties:
    value: {type: string}
    children:
      type: array
      items: {$ref: '#/components/schemas/Node'}
    parent: {$ref: '#/components/schemas/Node'}
`)
	bodies, err := rqWithin(t, contract)
	if err != nil || len(bodies) != 1 {
		t.Fatalf("a recursive schema is valid: want one body, got %v, %v", bodies, err)
	}
	root := bodies[0].Shape
	if got := rqMember(t, root, "children").Items; got != root {
		t.Errorf("children.items must be the root shape itself (a cycle), got %p, want %p", got, root)
	}
	if got := rqMember(t, root, "parent"); got != root {
		t.Errorf("parent must be the root shape itself, got %p, want %p", got, root)
	}
}

func TestRequestShapesMutualRecursion(t *testing.T) {
	contract := rqWithSchemas(rqDoc("$ref: '#/components/schemas/A'\n"), `A:
  type: object
  properties:
    b: {$ref: '#/components/schemas/B'}
B:
  type: object
  properties:
    a: {$ref: '#/components/schemas/A'}
`)
	bodies, err := rqWithin(t, contract)
	if err != nil || len(bodies) != 1 {
		t.Fatalf("want one body, got %v, %v", bodies, err)
	}
	a := bodies[0].Shape
	if rqMember(t, rqMember(t, a, "b"), "a") != a {
		t.Errorf("B.a must be A's shape itself")
	}
}

func TestRequestShapesRefChainResolvesToItsTarget(t *testing.T) {
	contract := rqWithSchemas(rqDoc("$ref: '#/components/schemas/A'\n"), `A: {$ref: '#/components/schemas/B'}
B: {$ref: '#/components/schemas/C'}
C:
  type: object
  required: [text]
  properties:
    text: {type: string}
`)
	bodies, err := rqWithin(t, contract)
	if err != nil || len(bodies) != 1 {
		t.Fatalf("a $ref chain is valid: want one body, got %v, %v", bodies, err)
	}
	if s := bodies[0].Shape; s == nil || !slices.Equal(s.Types, []string{"object"}) || !slices.Equal(rqNames(s.Properties), []string{"text"}) {
		t.Errorf("the chain must resolve to C, got %+v", s)
	}
}

func TestRequestShapesRefLoopsEnd(t *testing.T) {
	// A $ref chain that never reaches a schema is an error naming the location; a schema that
	// refers to itself through items is a valid recursive shape. Either way the walk ends.
	bad := map[string]string{
		"self alias":  "A: {$ref: '#/components/schemas/A'}\n",
		"two aliases": "A: {$ref: '#/components/schemas/B'}\nB: {$ref: '#/components/schemas/A'}\n",
		"three loop":  "A: {$ref: '#/components/schemas/B'}\nB: {$ref: '#/components/schemas/C'}\nC: {$ref: '#/components/schemas/A'}\n",
	}
	for name, schemas := range bad {
		t.Run(name, func(t *testing.T) {
			bodies, err := rqWithin(t, rqWithSchemas(rqDoc("$ref: '#/components/schemas/A'\n"), schemas))
			if err == nil || !strings.Contains(err.Error(), "paths//notes/post") {
				t.Errorf("want an error naming paths//notes/post, got %v (bodies %+v)", err, bodies)
			}
		})
	}
	t.Run("inside an array", func(t *testing.T) {
		bodies, err := rqWithin(t, rqWithSchemas(rqDoc("$ref: '#/components/schemas/A'\n"), "A:\n  type: array\n  items: {$ref: '#/components/schemas/A'}\n"))
		if err != nil || len(bodies) != 1 || bodies[0].Shape == nil || bodies[0].Shape.Items != bodies[0].Shape {
			t.Errorf("want a valid recursive shape (Items is the root), got %v, %+v", err, bodies)
		}
	})
}

func TestRequestShapesOnlyReachedSchemasAreJudged(t *testing.T) {
	contract := rqWithSchemas(rqDoc("type: object\nproperties:\n  a: {type: string}\n"), "Unused:\n  allOf: [{type: string}]\n")
	bodies, err := checks.RequestShapes([]byte(contract))
	if err != nil || len(bodies) != 1 {
		t.Errorf("a schema no request body reaches is not judged: want one body, got %v, %v", bodies, err)
	}
}

// ---- RequestShapes: refused text -------------------------------------------------------------

func TestRequestShapesRefusesWhatTheFitConditionRefuses(t *testing.T) {
	const loc = "paths//notes/post"
	cases := []struct {
		name     string
		contract string
		want     []string // each must appear in the error
	}{
		{"allOf", rqDoc("allOf: [{type: object}]\n"), []string{loc, "allOf"}},
		{"oneOf in a member", rqDoc("type: object\nproperties:\n  a: {oneOf: [{type: string}]}\n"), []string{loc, "oneOf"}},
		{"anyOf", rqDoc("anyOf: [{type: string}]\n"), []string{loc, "anyOf"}},
		{"not", rqDoc("not: {type: string}\n"), []string{loc, "not"}},
		{"nullable", rqDoc("type: string\nnullable: true\n"), []string{loc, "nullable"}},
		{"patternProperties", rqDoc("patternProperties: {'^a': {type: string}}\n"), []string{loc, "patternProperties"}},
		{"an unknown keyword", rqDoc("type: object\nunevaluatedProperties: false\n"), []string{loc, "unevaluatedProperties"}},
		{"an unsupported keyword inside items", rqDoc("type: array\nitems: {if: {type: string}}\n"), []string{loc, "if"}},
		{"additionalProperties a schema", rqDoc("type: object\nadditionalProperties: {type: string}\n"), []string{loc, "additionalProperties"}},
		{"additionalProperties a string", rqDoc("type: object\nadditionalProperties: 'false'\n"), []string{loc, "additionalProperties"}},
		{"a $ref to another file", rqDoc("$ref: 'other.yaml#/components/schemas/NewNote'\n"), []string{loc, "$ref"}},
		{"a $ref to a URL", rqDoc("$ref: 'https://example.com/n.json'\n"), []string{loc, "$ref"}},
		{"a $ref that does not resolve", rqDoc("$ref: '#/components/schemas/Missing'\n"), []string{loc}},
		{"an empty type list", rqDoc("type: []\n"), []string{loc, "type"}},
		{"an unquoted null in a type list", rqDoc("type: [string, null]\n"), []string{loc, "type"}},
		{"a number in a type list", rqDoc("type: [1]\n"), []string{loc, "type"}},
		{"a list of two non-null types", rqDoc("type: [integer, number]\n"), []string{loc, "type"}},
		{"a list of two types and null", rqDoc("type: [string, integer, \"null\"]\n"), []string{loc, "type"}},
		{"an unknown type name", rqDoc("type: date\n"), []string{loc, "type"}},
		{"a boolean schema for the body", rqDoc("true\n"), []string{loc}},
		{"a boolean items", rqDoc("type: array\nitems: true\n"), []string{loc}},
		{"a boolean property schema", rqDoc("type: object\nproperties:\n  a: false\n"), []string{loc}},
		{"an anchor", rqDoc("type: object\nproperties:\n  a: &s {type: string}\n"), []string{loc}},
		{"no schema", rqDocPaths("  /notes:\n    post:\n      requestBody:\n        content:\n          application/json: {}\n      responses: {}\n"), []string{loc}},
		{"text/plain", rqDocPaths("  /notes:\n    post:\n      requestBody:\n        content:\n          text/plain:\n            schema: {type: string}\n      responses: {}\n"), []string{loc, "text/plain"}},
		{"json beside another media type", rqDocPaths("  /notes:\n    post:\n      requestBody:\n        content:\n          application/json:\n            schema: {type: object}\n          application/xml:\n            schema: {type: object}\n      responses: {}\n"), []string{loc, "application/xml"}},
		{"a charset media type", rqDocPaths("  /notes:\n    post:\n      requestBody:\n        content:\n          application/json; charset=utf-8:\n            schema: {type: object}\n      responses: {}\n"), []string{loc}},
		{"no content", rqDocPaths("  /notes:\n    post:\n      requestBody:\n        required: true\n      responses: {}\n"), []string{loc}},
		{"an unsupported keyword reached through a $ref",
			rqWithSchemas(rqDoc("$ref: '#/components/schemas/Bad'\n"), "Bad:\n  type: object\n  properties:\n    a: {allOf: [{type: string}]}\n"), []string{"allOf"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bodies, err := rqWithin(t, c.contract)
			if err == nil {
				t.Fatalf("want an error, got bodies %+v", bodies)
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("want the error to name %q, got %q", w, err.Error())
				}
			}
		})
	}
}

func TestRequestShapesAnAnchorOrAliasAnywhereInTheSchemaIsRefused(t *testing.T) {
	contract := rqWithSchemas(rqDoc("$ref: '#/components/schemas/A'\n"), "Base: &base {type: string}\nA:\n  type: object\n  properties:\n    a: *base\n")
	if bodies, err := checks.RequestShapes([]byte(contract)); err == nil {
		t.Errorf("an alias reached from a body must be an error, got %+v", bodies)
	}
}

func TestRequestShapesRefusesTextThatIsNotOneDocumentWithoutDuplicateKeys(t *testing.T) {
	good := rqDoc("type: object\n")
	cases := map[string]string{
		"unparsable":           "paths: [\n",
		"not a mapping":        "- a\n- b\n",
		"empty":                "",
		"two documents":        good + "---\nopenapi: 3.1.0\n",
		"a duplicate key":      rqDoc("type: object\nproperties:\n  a: {type: string}\n  a: {type: integer}\n"),
		"a duplicate top key":  "openapi: 3.1.0\nopenapi: 3.1.0\n" + strings.TrimPrefix(good, "openapi: 3.1.0\n"),
		"a duplicate path key": rqDocPaths("  /notes:\n    post: {responses: {}}\n  /notes:\n    get: {responses: {}}\n"),
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if bodies, err := checks.RequestShapes([]byte(c)); err == nil {
				t.Errorf("want an error, got bodies %+v", bodies)
			}
		})
	}
	t.Run("control: the same text, valid, parses", func(t *testing.T) {
		if bodies, err := checks.RequestShapes([]byte(good)); err != nil || len(bodies) != 1 {
			t.Errorf("control: want one body, got %v, %v", bodies, err)
		}
	})
}

// ---- RenderRequestShapes ---------------------------------------------------------------------

const rqHeader = "// Code generated by the generate procedure from api/openapi.yaml. DO NOT EDIT.\n\npackage gen\n"

func rqRender(t *testing.T, contract string) []byte {
	t.Helper()
	out := checks.RenderRequestShapes(rqBodies(t, contract))
	if len(out) == 0 {
		t.Fatal("RenderRequestShapes returned no bytes")
	}
	return out
}

func rqParse(t *testing.T, src []byte) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "request_shapes.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("the rendered file does not parse: %v\n%s", err, src)
	}
	return fset, f
}

var rqShapeVar = regexp.MustCompile(`^requestShape([0-9]+)$`)

// rqShapeVars returns the numbers N of the package-level variables requestShape<N>, in source order.
func rqShapeVars(f *ast.File) []int {
	var ns []int
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, s := range gd.Specs {
			for _, n := range s.(*ast.ValueSpec).Names {
				if m := rqShapeVar.FindStringSubmatch(n.Name); m != nil {
					v, _ := strconv.Atoi(m[1])
					ns = append(ns, v)
				}
			}
		}
	}
	return ns
}

func rqStrings(f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		if l, ok := n.(*ast.BasicLit); ok && l.Kind == token.STRING {
			if s, err := strconv.Unquote(l.Value); err == nil {
				out = append(out, s)
			}
		}
		return true
	})
	return out
}

func TestRenderRequestShapesHeaderAndPackage(t *testing.T) {
	out := rqRender(t, rqDoc("type: object\nproperties:\n  text: {type: string}\n"))
	if !strings.HasPrefix(string(out), rqHeader) {
		t.Errorf("want the file to start with exactly\n%q\ngot\n%q", rqHeader, string(out)[:min(len(out), len(rqHeader)+20)])
	}
	_, f := rqParse(t, out)
	if f.Name.Name != "gen" {
		t.Errorf("package: want gen, got %s", f.Name.Name)
	}
	if !regexp.MustCompile(`(?m)^var requestBodies\b`).Match(out) && !strings.Contains(string(out), "requestBodies") {
		t.Errorf("want the map requestBodies in the file, got:\n%s", out)
	}
}

func TestRenderRequestShapesIsDeterministic(t *testing.T) {
	contract := rqWithSchemas(rqDocPaths(`  /a:
    post:
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/N'}
      responses: {}
  /b:
    put:
      requestBody:
        required: true
        content:
          application/json:
            schema: {type: array, items: {$ref: '#/components/schemas/N'}}
      responses: {}
`), "N:\n  type: object\n  required: [x, y]\n  properties:\n    x: {type: string}\n    kids: {type: array, items: {$ref: '#/components/schemas/N'}}\n")
	first := rqRender(t, contract)
	for i := 0; i < 20; i++ {
		if again := rqRender(t, contract); string(again) != string(first) {
			t.Fatalf("run %d: the same contract gave different bytes:\n%s\n----\n%s", i, first, again)
		}
	}
	bodies := rqBodies(t, contract)
	renderedOnce := checks.RenderRequestShapes(bodies)
	for i := 0; i < 20; i++ {
		if renderedAgain := checks.RenderRequestShapes(bodies); !bytes.Equal(renderedOnce, renderedAgain) {
			t.Fatalf("render %d: the same bodies gave different bytes", i)
		}
	}
}

func TestRenderRequestShapesNoBodiesIsAValidFileWithAnEmptyMap(t *testing.T) {
	for name, bodies := range map[string][]checks.RequestBody{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			out := checks.RenderRequestShapes(bodies)
			if !strings.HasPrefix(string(out), rqHeader) {
				t.Fatalf("want the header and package gen, got %q", out)
			}
			_, f := rqParse(t, out)
			if ns := rqShapeVars(f); len(ns) != 0 {
				t.Errorf("no bodies: want no requestShape variables, got %v", ns)
			}
			var lits int
			ast.Inspect(f, func(n ast.Node) bool {
				if kv, ok := n.(*ast.KeyValueExpr); ok {
					lits++
					_ = kv
				}
				return true
			})
			if lits != 0 {
				t.Errorf("no bodies: want an empty map, found %d key/value entries in:\n%s", lits, out)
			}
			if !strings.Contains(string(out), "requestBodies") {
				t.Errorf("want the (empty) map requestBodies declared, got:\n%s", out)
			}
			if got := checks.RenderRequestShapes(nil); string(got) != string(out) && name == "empty" {
				t.Errorf("nil and empty bodies must render the same bytes")
			}
		})
	}
}

func TestRenderRequestShapesQuotesKeysAndNames(t *testing.T) {
	names := []string{`say "hi"`, "line1\nline2", "end*/ x", `back\slash`, "tab\there", "ünï", "plain"}
	var props, req strings.Builder
	for _, n := range names {
		props.WriteString("  " + strconv.Quote(n) + ": {type: string}\n")
		req.WriteString(strconv.Quote(n) + ", ")
	}
	only := `only "required" */ name`
	contract := rqDocPaths("  " + strconv.Quote(`/we"ird/*/{id}`) + ":\n    post:\n      requestBody:\n        content:\n          application/json:\n            schema:\n" +
		rqIndent("type: object\nrequired: ["+req.String()+strconv.Quote(only)+"]\nproperties:\n"+props.String(), 14) + "      responses: {}\n")
	bodies := rqBodies(t, contract)
	if len(bodies) != 1 || bodies[0].Key != `POST /we"ird/*/{id}` {
		t.Fatalf("precondition: want the key %q, got %+v", `POST /we"ird/*/{id}`, bodies)
	}
	out := checks.RenderRequestShapes(bodies)
	_, f := rqParse(t, out)
	lits := rqStrings(f)
	for _, want := range append(append([]string{bodies[0].Key}, names...), only) {
		if !slices.Contains(lits, want) {
			t.Errorf("want a string literal that unquotes to %q, got %q", want, lits)
		}
		if !strings.Contains(string(out), strconv.Quote(want)) {
			t.Errorf("want %s (strconv.Quote form) in the file", strconv.Quote(want))
		}
	}
	// Nothing of a name leaks outside a literal: a raw newline or an unescaped quote inside a
	// name would have broken the parse above; a name must not appear as a comment either.
	for _, cg := range f.Comments {
		if strings.Contains(cg.Text(), "line2") || strings.Contains(cg.Text(), "hi") {
			t.Errorf("a name must not be written into a comment: %q", cg.Text())
		}
	}
}

func TestRenderRequestShapesOneVariablePerSharedShapeInDocumentOrderOfFirstUse(t *testing.T) {
	contract := rqWithSchemas(rqDocPaths(`  /first:
    post:
      requestBody:
        content:
          application/json:
            schema: {type: string}
      responses: {}
  /second:
    post:
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/Shared'}
      responses: {}
  /third:
    post:
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/Shared'}
      responses: {}
  /fourth:
    post:
      requestBody:
        content:
          application/json:
            schema: {type: boolean}
      responses: {}
`), "Shared:\n  type: integer\n")
	out := rqRender(t, contract)
	_, f := rqParse(t, out)
	ns := rqShapeVars(f)
	if !slices.Equal(ns, []int{1, 2, 3}) {
		t.Fatalf("want exactly requestShape1, requestShape2, requestShape3 in that order (the shared target once), got %v in\n%s", ns, out)
	}
	// Each variable's declaration holds its own type, so the numbering follows first use.
	decl := map[int]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			for _, n := range vs.Names {
				if m := rqShapeVar.FindStringSubmatch(n.Name); m != nil {
					v, _ := strconv.Atoi(m[1])
					decl[v] = string(out[vs.Pos()-1 : vs.End()-1])
				}
			}
		}
	}
	for n, want := range map[int]string{1: `"string"`, 2: `"integer"`, 3: `"boolean"`} {
		if !strings.Contains(decl[n], want) {
			t.Errorf("requestShape%d: want its declaration to hold %s (document order of first use), got %q", n, want, decl[n])
		}
	}
	if c := strings.Count(string(out), "requestShape2"); c < 3 {
		t.Errorf("the shared shape requestShape2 must be used by both bodies (declaration plus two entries), found %d mentions", c)
	}
}

// Numbering is depth first, a parent before its children (requestShape1 is the first body's root).
func TestRenderRequestShapesNumbersAParentBeforeItsChild(t *testing.T) {
	out := rqRender(t, rqDoc("type: object\nproperties:\n  kid: {type: integer}\n  other: {type: boolean}\n"))
	_, f := rqParse(t, out)
	if !slices.Equal(rqShapeVars(f), []int{1, 2, 3}) {
		t.Fatalf("want requestShape1..3, got %v", rqShapeVars(f))
	}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, sp := range gd.Specs {
			vs := sp.(*ast.ValueSpec)
			text := string(out[vs.Pos()-1 : vs.End()-1])
			for n, want := range map[string]string{"requestShape1": `"object"`, "requestShape2": `"integer"`, "requestShape3": `"boolean"`} {
				if vs.Names[0].Name == n && !strings.Contains(text, want) {
					t.Errorf("%s: want its declaration to hold %s (parent first, then children in order), got %q", n, want, text)
				}
			}
		}
	}
}

func TestRenderRequestShapesLinksShapesInInit(t *testing.T) {
	contract := rqWithSchemas(rqDoc("$ref: '#/components/schemas/Node'\n"), `Node:
  type: object
  properties:
    children:
      type: array
      items: {$ref: '#/components/schemas/Node'}
`)
	out := rqRender(t, contract)
	_, f := rqParse(t, out)
	var init *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == "init" {
			init = fd
		}
	}
	if init == nil {
		t.Fatalf("want an init() that links the shapes, got:\n%s", out)
	}
	used := map[string]bool{}
	ast.Inspect(init, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && rqShapeVar.MatchString(id.Name) {
			used[id.Name] = true
		}
		return true
	})
	if !used["requestShape1"] || !used["requestShape2"] {
		t.Errorf("init() must link the root and its children (requestShape1 and requestShape2), it mentions %v", used)
	}
	if !slices.Equal(rqShapeVars(f), []int{1, 2}) {
		t.Errorf("want the Node and the children array as requestShape1 and requestShape2, got %v", rqShapeVars(f))
	}
}

// rqStub stands in for the template's gen/requests.go and oapi-codegen's output; see the file
// comment. NewNote and RequestShape1 are names a contract's schemas become.
const rqStub = `package gen

type requestShape struct {
	Types           []string
	Properties      []requestMember
	RequiredMembers []string
	Closed          bool
	Items           *requestShape
}

type requestMember struct {
	Name  string
	Shape *requestShape
}

type requestBody struct {
	Required bool
	Shape    *requestShape
}

type NewNote struct{ Text string }
type RequestShape1 struct{ Text string }
type RequestShape struct{}
type RequestBodies struct{}
type Node struct{ Children []Node }

func Handler() {}
`

// rqTypeCheck type-checks the rendered file with the stub and returns the errors that are not
// merely the stub's guessed names.
func rqTypeCheck(t *testing.T, rendered []byte) []string {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for name, src := range map[string][]byte{"request_shapes.go": rendered, "stub.go": []byte(rqStub)} {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("%s does not parse: %v\n%s", name, err, src)
		}
		files = append(files, f)
	}
	var errs []string
	conf := types.Config{Importer: importer.Default(), Error: func(err error) { errs = append(errs, err.Error()) }}
	_, _ = conf.Check("gen", fset, files, nil)
	var hard []string
	for _, e := range errs {
		if strings.Contains(e, "redeclared") || strings.Contains(e, "initialization cycle") ||
			strings.Contains(e, "declared and not used") || strings.Contains(e, "imported and not used") ||
			strings.Contains(e, "missing return") || strings.Contains(e, "syntax") {
			hard = append(hard, e)
		}
	}
	return hard
}

func TestRenderRequestShapesTypeChecksWithGeneratedCode(t *testing.T) {
	cases := map[string]string{
		"a recursive schema": rqWithSchemas(rqDoc("$ref: '#/components/schemas/Node'\n"), `Node:
  type: object
  required: [value]
  properties:
    value: {type: string}
    children:
      type: array
      items: {$ref: '#/components/schemas/Node'}
    parent: {$ref: '#/components/schemas/Node'}
`),
		"schemas named like generated types": rqWithSchemas(rqDoc("$ref: '#/components/schemas/NewNote'\n"),
			"NewNote:\n  type: object\n  properties:\n    next: {$ref: '#/components/schemas/RequestShape1'}\nRequestShape1:\n  type: object\n  properties:\n    back: {$ref: '#/components/schemas/NewNote'}\n"),
		"two bodies sharing a shape": rqWithSchemas(rqDocPaths("  /a:\n    post:\n      requestBody:\n        content:\n          application/json:\n            schema: {$ref: '#/components/schemas/NewNote'}\n      responses: {}\n  /b:\n    put:\n      requestBody:\n        required: true\n        content:\n          application/json:\n            schema: {$ref: '#/components/schemas/NewNote'}\n      responses: {}\n"),
			"NewNote:\n  type: object\n  required: [text]\n  properties:\n    text: {type: string}\n"),
		"awkward names": rqDoc("type: object\nrequired: ['a\"b', \"c\\nd\", 'e*/f']\nproperties:\n  'a\"b': {type: string}\n"),
	}
	for name, contract := range cases {
		t.Run(name, func(t *testing.T) {
			out := rqRender(t, contract)
			if errs := rqTypeCheck(t, out); len(errs) > 0 {
				t.Errorf("the rendered file does not type-check with generated code:\n%s\n---\n%s", strings.Join(errs, "\n"), out)
			}
		})
	}
	t.Run("no bodies", func(t *testing.T) {
		if errs := rqTypeCheck(t, checks.RenderRequestShapes(nil)); len(errs) > 0 {
			t.Errorf("the empty file does not type-check: %v", errs)
		}
	})
}

// ---- RequestShapesGenerated ------------------------------------------------------------------

const rqGenContract = `openapi: 3.1.0
paths:
  /notes:
    post:
      requestBody:
        required: true
        content:
          application/json:
            schema: {$ref: '#/components/schemas/NewNote'}
      responses: {}
  /notes/{id}:
    put:
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/NewNote'}
      responses: {}
components:
  schemas:
    NewNote:
      type: object
      required: [text]
      properties:
        text: {type: string}
      additionalProperties: false
`

func TestRequestShapesGenerated(t *testing.T) {
	const check = "RequestShapesGenerated"
	rendered := rqRender(t, rqGenContract)

	t.Run("passes on the rendered file, saying what it examined", func(t *testing.T) {
		dir := dvProject(t, map[string]string{"gen/request_shapes.go": string(rendered)})
		r := dvWantOnePass(t, checks.RequestShapesGenerated(dir, []byte(rqGenContract)), check)
		if !strings.Contains(r.Reason, "gen/request_shapes.go") || !regexp.MustCompile(`\b2\b`).MatchString(r.Reason) {
			t.Errorf("want the reason to name gen/request_shapes.go and the 2 request bodies it examined, got %q", r.Reason)
		}
	})
	t.Run("fails on a missing file naming it", func(t *testing.T) {
		dir := dvProject(t, map[string]string{"gen/other.go": "package gen\n"})
		rs := checks.RequestShapesGenerated(dir, []byte(rqGenContract))
		dvWantAllFail(t, rs, check, "the file is missing")
		if !strings.Contains(reasons(rs), "gen/request_shapes.go") {
			t.Errorf("want the reason to name gen/request_shapes.go, got %q", reasons(rs))
		}
	})
	t.Run("fails on an empty file", func(t *testing.T) {
		dir := dvProject(t, map[string]string{"gen/request_shapes.go": ""})
		dvWantAllFail(t, checks.RequestShapesGenerated(dir, []byte(rqGenContract)), check, "the file is empty")
	})
	t.Run("fails on an edited file", func(t *testing.T) {
		dir := dvProject(t, map[string]string{"gen/request_shapes.go": string(rendered) + "\nvar extra = 1\n"})
		rs := checks.RequestShapesGenerated(dir, []byte(rqGenContract))
		dvWantAllFail(t, rs, check, "the file was edited")
		if !strings.Contains(reasons(rs), "gen/request_shapes.go") {
			t.Errorf("want the reason to name gen/request_shapes.go, got %q", reasons(rs))
		}
	})
	t.Run("fails on a one-byte difference", func(t *testing.T) {
		edited := strings.Replace(string(rendered), `"text"`, `"Text"`, 1)
		if edited == string(rendered) {
			t.Fatal("precondition: the rendering must hold the name \"text\" quoted")
		}
		dir := dvProject(t, map[string]string{"gen/request_shapes.go": edited})
		dvWantAllFail(t, checks.RequestShapesGenerated(dir, []byte(rqGenContract)), check, "a name differs")
	})
	t.Run("fails on a file that dropped a requirement", func(t *testing.T) {
		weaker := string(rqRender(t, strings.Replace(rqGenContract, "      required: [text]\n", "", 1)))
		dir := dvProject(t, map[string]string{"gen/request_shapes.go": weaker})
		dvWantAllFail(t, checks.RequestShapesGenerated(dir, []byte(rqGenContract)), check, "the file is weaker than the contract")
	})
	t.Run("fails on a stale file after the contract changed", func(t *testing.T) {
		changed := strings.Replace(rqGenContract, "additionalProperties: false", "additionalProperties: true", 1)
		dir := dvProject(t, map[string]string{"gen/request_shapes.go": string(rendered)})
		dvWantAllFail(t, checks.RequestShapesGenerated(dir, []byte(changed)), check, "the file renders the old contract")
	})
	t.Run("fails on a file in another place", func(t *testing.T) {
		dir := dvProject(t, map[string]string{"request_shapes.go": string(rendered), "gen/declared.go": string(rendered)})
		dvWantAllFail(t, checks.RequestShapesGenerated(dir, []byte(rqGenContract)), check, "the file is not at gen/request_shapes.go")
	})
	t.Run("fails closed on an approved contract that cannot be read, naming it", func(t *testing.T) {
		dir := dvProject(t, map[string]string{"gen/request_shapes.go": string(rendered)})
		dvWantOnePass(t, checks.RequestShapesGenerated(dir, []byte(rqGenContract)), check) // control: the same file passes for a valid contract
		for name, c := range map[string]string{
			"unparsable":        "a: [\n",
			"duplicate key":     "openapi: 3.1.0\nopenapi: 3.1.0\n",
			"two documents":     rqGenContract + "---\na: b\n",
			"text/plain body":   rqDocPaths("  /notes:\n    post:\n      requestBody:\n        content:\n          text/plain:\n            schema: {type: string}\n      responses: {}\n"),
			"unsupported allOf": rqDoc("allOf: [{type: object}]\n"),
		} {
			rs := checks.RequestShapesGenerated(dir, []byte(c))
			dvWantAllFail(t, rs, check, name)
			if !strings.Contains(strings.ToLower(reasons(rs)), "contract") {
				t.Errorf("%s: want the reason to name the approved contract, got %q", name, reasons(rs))
			}
		}
	})
	t.Run("passes on a contract with no request body and a header-only file", func(t *testing.T) {
		contract := rqDocPaths("  /notes:\n    get:\n      responses: {}\n")
		dir := dvProject(t, map[string]string{"gen/request_shapes.go": string(checks.RenderRequestShapes(nil))})
		dvWantOnePass(t, checks.RequestShapesGenerated(dir, []byte(contract)), check)
	})
	t.Run("fails on a header-only file when the contract has a body", func(t *testing.T) {
		dir := dvProject(t, map[string]string{"gen/request_shapes.go": string(checks.RenderRequestShapes(nil))})
		dvWantAllFail(t, checks.RequestShapesGenerated(dir, []byte(rqGenContract)), check, "the file has no bodies")
	})
}

// ---- GeneratedCodeUnchanged pins the three files of the request check ------------------------

func TestGeneratedCodeUnchangedCoversTheRequestCheckFiles(t *testing.T) {
	genGlobs := []string{"gen/**", "go.mod", "go.sum", "vendor/**"}
	base := map[string]string{
		"gen/requests.go":           "package gen\n\nfunc refuseMalformed() {}\n",
		"gen/request_shapes.go":     string(checks.RenderRequestShapes(nil)),
		"gen/std-http-handler.tmpl": "{{/* copied from oapi-codegen */}}\nm.HandleFunc(x, refuseMalformed(k, h))\n",
		"gen/api.gen.go":            "package gen\n",
		"internal/app/app.go":       "package app\n",
	}
	copyWith := func(over map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range over {
			out[k] = v
		}
		return out
	}
	before := dvProject(t, base)

	t.Run("control: nothing changed passes", func(t *testing.T) {
		rs := checks.GeneratedCodeUnchanged(before, dvProject(t, base), genGlobs)
		dvWantOnePass(t, rs, "GeneratedCodeUnchanged")
	})
	t.Run("control: a change in the code step's own files passes", func(t *testing.T) {
		after := dvProject(t, copyWith(map[string]string{"internal/app/app.go": "package app\n\nvar x = 1\n"}))
		dvWantOnePass(t, checks.GeneratedCodeUnchanged(before, after, genGlobs), "GeneratedCodeUnchanged")
	})
	for _, file := range []string{"gen/requests.go", "gen/request_shapes.go", "gen/std-http-handler.tmpl"} {
		t.Run("fails when the code step edits "+file, func(t *testing.T) {
			after := dvProject(t, copyWith(map[string]string{file: base[file] + "// weakened by the code step\n"}))
			rs := checks.GeneratedCodeUnchanged(before, after, genGlobs)
			dvWantAllFail(t, rs, "GeneratedCodeUnchanged", file+" edited")
			if !strings.Contains(reasons(rs), file) {
				t.Errorf("want the reason to name %s, got %q", file, reasons(rs))
			}
		})
	}
	t.Run("fails when the code step deletes gen/request_shapes.go", func(t *testing.T) {
		files := copyWith(nil)
		delete(files, "gen/request_shapes.go")
		rs := checks.GeneratedCodeUnchanged(before, dvProject(t, files), genGlobs)
		dvWantAllFail(t, rs, "GeneratedCodeUnchanged", "file removed")
		if !strings.Contains(reasons(rs), "gen/request_shapes.go") {
			t.Errorf("want the reason to name gen/request_shapes.go, got %q", reasons(rs))
		}
	})
}
