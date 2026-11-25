// T024 (feature 010), by a test author: regression tests for review round 1 of US1 (findings F1,
// F2 and F3). The README ("Fit conditions", "Limits of the request check") says a boolean in the
// contract counts in any spelling YAML reads as one, for additionalProperties and requestBody.required
// alike, and that any key beside a $ref other than an annotation or an x- key is refused.
// Helpers (rqDoc, rqShape, rqWithin, rqWithSchemas, rqIndent) come from request_shapes_test.go.
package checks_test

import (
	"strings"
	"testing"
)

// rqRequiredDoc is a contract with one POST /notes whose requestBody.required is written as given.
func rqRequiredDoc(required string) string {
	return rqDocPaths("  /notes:\n    post:\n      requestBody:\n        required: " + required +
		"\n        content:\n          application/json:\n            schema: {type: object}\n      responses: {}\n")
}

func TestRequestShapesAdditionalPropertiesBooleanSpellings(t *testing.T) {
	cases := []struct {
		spelling string
		closed   bool
	}{
		{"false", true}, {"False", true}, {"FALSE", true},
		{"true", false}, {"True", false}, {"TRUE", false},
	}
	for _, c := range cases {
		t.Run(c.spelling, func(t *testing.T) {
			s := rqShape(t, "type: object\nproperties:\n  a: {type: string}\nadditionalProperties: "+c.spelling+"\n")
			if s.Closed != c.closed {
				t.Errorf("additionalProperties: %s gave Closed=%v, want %v", c.spelling, s.Closed, c.closed)
			}
		})
	}
	t.Run("False on a nested member closes only that member", func(t *testing.T) {
		s := rqShape(t, "type: object\nproperties:\n  a:\n    type: object\n    additionalProperties: False\n")
		if s.Closed || !rqMember(t, s, "a").Closed {
			t.Errorf("want the root open and /a closed, got root Closed=%v", s.Closed)
		}
	})
	t.Run("False on items closes the items", func(t *testing.T) {
		s := rqShape(t, "type: array\nitems:\n  type: object\n  additionalProperties: FALSE\n")
		if s.Items == nil || !s.Items.Closed {
			t.Errorf("want closed items, got %+v", s.Items)
		}
	})
	t.Run("False without a type still closes (the keyword applies to an object value)", func(t *testing.T) {
		if s := rqShape(t, "properties:\n  a: {type: string}\nadditionalProperties: False\n"); !s.Closed {
			t.Error("want Closed with no type")
		}
	})
	t.Run("a quoted string is not a boolean", func(t *testing.T) {
		for _, v := range []string{"'False'", "'false'", "'FALSE'"} {
			if _, err := rqWithin(t, rqDoc("type: object\nadditionalProperties: "+v+"\n")); err == nil {
				t.Errorf("additionalProperties: %s (a string) must be refused", v)
			}
		}
	})
}

func TestRequestShapesRequiredBooleanSpellings(t *testing.T) {
	cases := []struct {
		spelling string
		want     bool
	}{
		{"true", true}, {"True", true}, {"TRUE", true},
		{"false", false}, {"False", false}, {"FALSE", false},
	}
	for _, c := range cases {
		t.Run(c.spelling, func(t *testing.T) {
			bodies := rqBodies(t, rqRequiredDoc(c.spelling))
			if len(bodies) != 1 {
				t.Fatalf("want one body, got %+v", bodies)
			}
			if bodies[0].Required != c.want {
				t.Errorf("requestBody.required: %s gave Required=%v, want %v", c.spelling, bodies[0].Required, c.want)
			}
		})
	}
	t.Run("absent is not required", func(t *testing.T) {
		doc := rqDocPaths("  /notes:\n    post:\n      requestBody:\n        content:\n          application/json:\n            schema: {type: object}\n      responses: {}\n")
		if b := rqBodies(t, doc); len(b) != 1 || b[0].Required {
			t.Errorf("want one body, not required, got %+v", b)
		}
	})
	t.Run("a followed requestBody $ref reads its own spelling", func(t *testing.T) {
		doc := rqDocPaths("  /notes:\n    post:\n      requestBody:\n        $ref: '#/components/requestBodies/B'\n      responses: {}\n")
		doc = rqWithSchemas(doc, "x: {type: string}\n")
		doc += "  requestBodies:\n    B:\n      required: True\n      content:\n        application/json:\n          schema: {type: object}\n"
		if b := rqBodies(t, doc); len(b) != 1 || !b[0].Required {
			t.Errorf("want Required for required: True behind a $ref, got %+v", b)
		}
	})
}

func TestRequestShapesKeysBesideARefAreRefused(t *testing.T) {
	const loc = "paths//notes/post"
	refd := func(root, sib string) string {
		return rqWithSchemas(rqDoc(strings.ReplaceAll(root, "SIB", sib)), "Note:\n  type: object\n  properties:\n    a: {type: string}\n")
	}
	siblings := map[string]string{
		"not":        "not: {type: string}",
		"allOf":      "allOf: [{type: object}]",
		"nullable":   "nullable: true",
		"maxLength":  "maxLength: 3",
		"type":       "type: object",
		"properties": "properties: {b: {type: string}}",
		"required":   "required: [a]",
	}
	places := map[string]string{
		"the body":    "$ref: '#/components/schemas/Note'\nSIB\n",
		"a member":    "type: object\nproperties:\n  m:\n    $ref: '#/components/schemas/Note'\n    SIB\n",
		"array items": "type: array\nitems:\n  $ref: '#/components/schemas/Note'\n  SIB\n",
	}
	for pname, place := range places {
		for key, sib := range siblings {
			t.Run(pname+"/"+key, func(t *testing.T) {
				bodies, err := rqWithin(t, refd(place, sib))
				if err == nil {
					t.Fatalf("want an error, got bodies %+v", bodies)
				}
				for _, w := range []string{loc, key} {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("want the error to name %q, got %q", w, err.Error())
					}
				}
			})
		}
	}
	t.Run("a sibling inside a component reached through a $ref", func(t *testing.T) {
		doc := rqWithSchemas(rqDoc("$ref: '#/components/schemas/A'\n"),
			"A:\n  type: object\n  properties:\n    m:\n      $ref: '#/components/schemas/B'\n      not: {type: string}\nB: {type: string}\n")
		_, err := rqWithin(t, doc)
		if err == nil || !strings.Contains(err.Error(), "not") {
			t.Errorf("want an error naming not, got %v", err)
		}
	})
	t.Run("an unreached component with a sibling is not judged", func(t *testing.T) {
		doc := rqWithSchemas(rqDoc("type: object\nproperties:\n  a: {type: string}\n"),
			"Unused:\n  $ref: '#/components/schemas/Other'\n  not: {type: string}\nOther: {type: string}\n")
		if b, err := rqWithin(t, doc); err != nil || len(b) != 1 {
			t.Errorf("want one body, got %v, %v", b, err)
		}
	})
}

func TestRequestShapesAnnotationsAndExtensionsBesideARefAreFine(t *testing.T) {
	siblings := []string{
		"description: a note",
		"x-note: anything",
		"x-deep: {allOf: [1], not: 2}",
		"title: Note",
		"default: {a: x}",
		"examples: [{a: x}]",
		"example: {a: x}",
		"deprecated: true",
		"readOnly: true",
		"writeOnly: true",
		"$comment: hello",
	}
	for _, sib := range siblings {
		t.Run(sib, func(t *testing.T) {
			doc := rqWithSchemas(rqDoc("$ref: '#/components/schemas/Note'\n"+sib+"\n"), "Note:\n  type: object\n  properties:\n    a: {type: string}\n")
			bodies, err := rqWithin(t, doc)
			if err != nil {
				t.Fatalf("want no error, got %v", err)
			}
			if len(bodies) != 1 || bodies[0].Shape == nil || len(bodies[0].Shape.Properties) != 1 {
				t.Errorf("want the shape of Note, got %+v", bodies)
			}
		})
	}
}
