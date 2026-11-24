// T025 (feature 009): pins the collector limit stated in the catalogue README ("Limits of the
// collector"): a keyword whose value is a mapping or a sequence declares nothing itself, but the
// walk goes on inside it, so `default: {maxLength: 5}` declares 5, its pointer ending
// /default/maxLength. Pure Go over text; reuses the helpers of declared_values_test.go.
package checks_test

import (
	"strings"
	"testing"

	"github.com/agentic-platform/engine/checks"
)

const dwNote = "/components/schemas/Note/properties/size"

func dwContract(propBody string) string {
	return "openapi: 3.0.3\ncomponents:\n  schemas:\n    Note:\n      type: object\n      properties:\n        size:\n" + propBody
}

func dwWantOne(t *testing.T, vals []checks.Declared, pointer, keyword, text string) checks.Declared {
	t.Helper()
	if got := dvPointers(vals); !dvEqual(got, []string{pointer}) {
		t.Fatalf("want exactly one declared value at %s (nothing for the mapping or sequence keyword itself), got %q", pointer, got)
	}
	v := vals[0]
	if v.Keyword != keyword || v.Kind != "number" || v.Text != text || !dvNumberIs(v.Value, text) {
		t.Errorf("want keyword %s, kind number, text and value %s, got %+v", keyword, text, v)
	}
	return v
}

func TestDeclaredWalkMappingUnderDefaultDeclaresInnerValueOnly(t *testing.T) {
	vals := dvValues(t, dwContract("          default:\n            maxLength: 5\n"), nil)
	dwWantOne(t, vals, dwNote+"/default/maxLength", "maxLength", "5")
	if !strings.HasSuffix(vals[0].Pointer, "/default/maxLength") {
		t.Errorf("pointer must end /default/maxLength, got %s", vals[0].Pointer)
	}
}

func TestDeclaredWalkMappingUnderConstDeclaresInnerValueOnly(t *testing.T) {
	vals := dvValues(t, dwContract("          const:\n            maxLength: 5\n"), nil)
	dwWantOne(t, vals, dwNote+"/const/maxLength", "maxLength", "5")
}

func TestDeclaredWalkFlowMappingDefault(t *testing.T) {
	vals := dvValues(t, dwContract("          default: {maxLength: 5}\n"), nil)
	dwWantOne(t, vals, dwNote+"/default/maxLength", "maxLength", "5")
}

func TestDeclaredWalkSequenceOfMappingsUnderDefault(t *testing.T) {
	vals := dvValues(t, dwContract("          default:\n            - minimum: 3\n"), nil)
	dwWantOne(t, vals, dwNote+"/default/0/minimum", "minimum", "3")
}

func TestDeclaredWalkSequenceIndexesAndSeveralItems(t *testing.T) {
	vals := dvValues(t, dwContract("          default:\n            - minimum: 3\n            - {maximum: 9, maxItems: 2}\n"), nil)
	want := []string{dwNote + "/default/0/minimum", dwNote + "/default/1/maximum", dwNote + "/default/1/maxItems"}
	if got := dvPointers(vals); !dvEqual(got, want) {
		t.Fatalf("want %q in document order, got %q", want, got)
	}
}

func TestDeclaredWalkScalarSequenceItemsUnderDefaultDeclareNothing(t *testing.T) {
	vals := dvValues(t, dwContract("          default: [1, 2, three]\n"), nil)
	if got := dvPointers(vals); len(got) != 0 {
		t.Fatalf("scalar items of a default sequence are not enum items and declare nothing, got %q", got)
	}
}

func TestDeclaredWalkNestedDefaultInsideDefault(t *testing.T) {
	vals := dvValues(t, dwContract("          default:\n            default:\n              maxLength: 5\n            minLength: 1\n"), nil)
	want := []string{dwNote + "/default/default/maxLength", dwNote + "/default/minLength"}
	if got := dvPointers(vals); !dvEqual(got, want) {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestDeclaredWalkHiddenKeyInsideDefaultDeclaresNothing(t *testing.T) {
	for _, hidden := range []string{"x-meta", "example", "examples"} {
		vals := dvValues(t, dwContract("          default:\n            "+hidden+":\n              maxLength: 5\n            minLength: 1\n"), nil)
		dwWantOne(t, vals, dwNote+"/default/minLength", "minLength", "1")
	}
}

func TestDeclaredWalkEnumInsideDefaultKeepsEnumItems(t *testing.T) {
	vals := dvValues(t, dwContract("          default:\n            enum: [a, 7]\n"), nil)
	want := []string{dwNote + "/default/enum/0", dwNote + "/default/enum/1"}
	if got := dvPointers(vals); !dvEqual(got, want) {
		t.Fatalf("want %q, got %q", want, got)
	}
	for _, v := range vals {
		if v.Keyword != "enum" || v.Name != "" {
			t.Errorf("enum item %s: want keyword enum and no name, got %+v", v.Pointer, v)
		}
	}
	if vals[0].Kind != "string" || vals[1].Kind != "number" {
		t.Errorf("want kinds string, number, got %q, %q", vals[0].Kind, vals[1].Kind)
	}
}

func TestDeclaredWalkNonDeclaringKeyInsideDefaultDeclaresNothing(t *testing.T) {
	vals := dvValues(t, dwContract("          default:\n            length: 5\n            minLength: [1]\n"), nil)
	if got := dvPointers(vals); len(got) != 0 {
		t.Fatalf("an unknown key and a keyword with a sequence value declare nothing, got %q", got)
	}
}

func TestDeclaredWalkRenderNamesTheInnerValue(t *testing.T) {
	vals := dvValues(t, dwContract("          default:\n            maxLength: 5\n"), nil)
	dwWantOne(t, vals, dwNote+"/default/maxLength", "maxLength", "5")
	if vals[0].Name != "DeclaredNoteSizeDefaultMaxLength" {
		t.Errorf("want name DeclaredNoteSizeDefaultMaxLength, got %q", vals[0].Name)
	}
	want := dvHeader + "\n\n// DeclaredNoteSizeDefaultMaxLength is declared at \"" + dwNote + "/default/maxLength\".\nconst DeclaredNoteSizeDefaultMaxLength = 5"
	if got := strings.TrimRight(string(checks.RenderDeclared(vals)), "\n"); got != want {
		t.Errorf("RenderDeclared:\n want:\n%s\n got:\n%s", want, got)
	}
}

func TestDeclaredWalkRenderNamesSequenceItemWithIndex(t *testing.T) {
	vals := dvValues(t, dwContract("          default:\n            - minimum: 3\n"), nil)
	if vals[0].Name != "DeclaredNoteSizeDefault0Minimum" {
		t.Errorf("want name DeclaredNoteSizeDefault0Minimum, got %q", vals[0].Name)
	}
	if got := string(checks.RenderDeclared(vals)); !strings.Contains(got, "const DeclaredNoteSizeDefault0Minimum = 3\n") {
		t.Errorf("want the constant in the rendered file, got:\n%s", got)
	}
}
