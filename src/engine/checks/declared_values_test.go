// T003 (feature 009): failing tests, by a separate author, for the declared-value collector
// (DeclaredValues), gen/declared.go (RenderDeclared, DeclaredValuesGenerated, the constant names)
// and the code-step check DeclaredValuesNotRepeated. The contract they follow is the catalogue
// README ("Declared values"), the check contract's runCheck description and the Go doc comments
// of declared.go and values.go. Pure Go over text and temp folders: no model, no network, no
// Docker. The one build test runs the local go toolchain offline.
package checks_test

import (
	"bytes"
	"go/constant"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/checks"
)

// ---- helpers ---------------------------------------------------------------------------------

func dvValues(t *testing.T, contract string, taken map[string]bool) []checks.Declared {
	t.Helper()
	vals, err := checks.DeclaredValues([]byte(contract), taken)
	if err != nil {
		t.Fatalf("DeclaredValues: want the values of a valid contract, got error %v", err)
	}
	return vals
}

func dvFind(t *testing.T, vals []checks.Declared, pointer string) checks.Declared {
	t.Helper()
	for _, v := range vals {
		if v.Pointer == pointer {
			return v
		}
	}
	var have []string
	for _, v := range vals {
		have = append(have, v.Pointer)
	}
	t.Fatalf("want a declared value at %s; found only %q", pointer, have)
	return checks.Declared{}
}

func dvPointers(vals []checks.Declared) []string {
	var out []string
	for _, v := range vals {
		out = append(out, v.Pointer)
	}
	return out
}

func dvEqual(a, b []string) bool { return slices.Equal(a, b) }

// dvNumberIs reports whether v is the number written as lit, by Go's exact constant comparison.
func dvNumberIs(v constant.Value, lit string) bool {
	if v == nil {
		return false
	}
	kind := token.INT
	if strings.ContainsAny(lit, ".eE") {
		kind = token.FLOAT
	}
	want := constant.MakeFromLiteral(lit, kind, 0)
	if v.Kind() == constant.String || want.Kind() == constant.Unknown {
		return false
	}
	return constant.Compare(v, token.EQL, want)
}

func dvProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// dvLine is the 1-based line of the first line of code holding marker.
func dvLine(t *testing.T, code, marker string) int {
	t.Helper()
	for i, l := range strings.Split(code, "\n") {
		if strings.Contains(l, marker) {
			return i + 1
		}
	}
	t.Fatalf("fixture has no line with %q", marker)
	return 0
}

func dvFailures(rs []checks.CheckResult) []checks.CheckResult {
	var out []checks.CheckResult
	for _, r := range rs {
		if !r.Passed {
			out = append(out, r)
		}
	}
	return out
}

func dvEveryResultIs(t *testing.T, rs []checks.CheckResult, check string) {
	t.Helper()
	if len(rs) == 0 {
		t.Fatalf("%s: want at least one result, got none", check)
	}
	for _, r := range rs {
		if r.Part != "project" || r.Check != check {
			t.Errorf("%s: want every result with Part %q and Check %q, got Part %q Check %q", check, "project", check, r.Part, r.Check)
		}
	}
}

func dvWantAllFail(t *testing.T, rs []checks.CheckResult, check string, why string) {
	t.Helper()
	dvEveryResultIs(t, rs, check)
	for _, r := range rs {
		if r.Passed {
			t.Errorf("%s: want a failing result (%s), got a pass: %q", check, why, r.Reason)
		}
	}
}

func dvWantOnePass(t *testing.T, rs []checks.CheckResult, check string) checks.CheckResult {
	t.Helper()
	dvEveryResultIs(t, rs, check)
	if len(rs) != 1 || !rs[0].Passed {
		t.Fatalf("%s: want exactly one passing result, got %+v", check, rs)
	}
	return rs[0]
}

// ---- DeclaredValues: what is declared --------------------------------------------------------

const dvAllKeywords = `openapi: 3.1.0
components:
  schemas:
    S:
      type: object
      properties:
        a:
          maxLength: 1
          minLength: 2
          maximum: 3
          minimum: 4
          exclusiveMaximum: 5
          exclusiveMinimum: 6
          maxItems: 7
          minItems: 8
          maxProperties: 9
          minProperties: 10
          multipleOf: 11
          default: twelve
          const: 13
`

func TestDeclaredValuesFindsAllThirteenKeywords(t *testing.T) {
	vals := dvValues(t, dvAllKeywords, nil)
	kw := []string{"maxLength", "minLength", "maximum", "minimum", "exclusiveMaximum", "exclusiveMinimum",
		"maxItems", "minItems", "maxProperties", "minProperties", "multipleOf", "default", "const"}
	if len(vals) != len(kw) {
		t.Fatalf("want %d declared values (one per keyword), got %d: %q", len(kw), len(vals), dvPointers(vals))
	}
	for i, k := range kw {
		want := "/components/schemas/S/properties/a/" + k
		v := vals[i]
		if v.Pointer != want || v.Keyword != k {
			t.Errorf("value %d: want pointer %s keyword %s in document order, got %s %s", i, want, k, v.Pointer, v.Keyword)
		}
	}
	if v := dvFind(t, vals, "/components/schemas/S/properties/a/default"); v.Kind != "string" || v.Value == nil || constant.StringVal(v.Value) != "twelve" {
		t.Errorf("default: twelve: want kind string, value %q, got kind %q value %v", "twelve", v.Kind, v.Value)
	}
	if v := dvFind(t, vals, "/components/schemas/S/properties/a/const"); v.Kind != "number" || !dvNumberIs(v.Value, "13") {
		t.Errorf("const: 13: want kind number with value 13, got kind %q value %v", v.Kind, v.Value)
	}
	if v := dvFind(t, vals, "/components/schemas/S/properties/a/maxLength"); v.Name != "DeclaredSAMaxLength" {
		t.Errorf("maxLength under S.properties.a: want constant DeclaredSAMaxLength, got %q", v.Name)
	}
}

const dvWalk = `openapi: 3.1.0
components:
  schemas:
    Note:
      type: object
      maxProperties: 9
      properties:
        text:
          type: string
          minLength: 1
          maxLength: 200
          enum: [a, b]
          x-vendor:
            maxLength: 7
          example:
            maxLength: 8
          examples:
            - minLength: 9
        default:
          type: string
          const: fixed
        example:
          type: string
          maxLength: 33
        x-hidden:
          type: string
          maxLength: 34
        after:
          maxItems: 2
`

func TestDeclaredValuesWalkOrderHiddenSubtreesAndMappingKeywords(t *testing.T) {
	vals := dvValues(t, dvWalk, nil)
	want := []string{
		"/components/schemas/Note/maxProperties",
		"/components/schemas/Note/properties/text/minLength",
		"/components/schemas/Note/properties/text/maxLength",
		"/components/schemas/Note/properties/text/enum/0",
		"/components/schemas/Note/properties/text/enum/1",
		"/components/schemas/Note/properties/default/const",
		"/components/schemas/Note/properties/after/maxItems",
	}
	if got := dvPointers(vals); !dvEqual(got, want) {
		t.Fatalf("want exactly these pointers in document order (nothing below example, examples or x-*, a property named example or x-hidden hidden with its limits, a property named default declares nothing itself but its const is found):\n want %q\n  got %q", want, got)
	}
	for _, p := range want[3:5] {
		if v := dvFind(t, vals, p); v.Keyword != "enum" || v.Kind != "string" || v.Name != "" {
			t.Errorf("enum item %s: want keyword enum, kind string, no constant name, got keyword %q kind %q name %q", p, v.Keyword, v.Kind, v.Name)
		}
	}
	if v := dvFind(t, vals, want[2]); v.Keyword != "maxLength" || v.Kind != "number" || !dvNumberIs(v.Value, "200") || v.Text != "200" {
		t.Errorf("maxLength: 200: want keyword maxLength kind number value 200 text %q, got %+v", "200", v)
	}
}

func TestDeclaredValuesEnumItemsOfAnyKindAndNestedItems(t *testing.T) {
	vals := dvValues(t, `openapi: 3.1.0
components:
  schemas:
    S:
      enum: [1, "two", true, null, [x], {k: v}]
`, nil)
	if got, want := dvPointers(vals), []string{"/components/schemas/S/enum/0", "/components/schemas/S/enum/1", "/components/schemas/S/enum/2", "/components/schemas/S/enum/3"}; !dvEqual(got, want) {
		t.Fatalf("want only the scalar items of the enum (a sequence or mapping item declares nothing):\n want %q\n  got %q", want, got)
	}
	kinds := []string{"number", "string", "bool", "null"}
	for i, v := range vals {
		if v.Kind != kinds[i] || v.Name != "" {
			t.Errorf("enum item %d: want kind %s and no constant name, got kind %q name %q", i, kinds[i], v.Kind, v.Name)
		}
	}
}

func TestDeclaredValuesKeywordWithNonScalarValueDeclaresNothing(t *testing.T) {
	vals := dvValues(t, `openapi: 3.1.0
components:
  schemas:
    S:
      default: [1, 2]
      const: {a: 1}
      maxLength: 5
`, nil)
	if got, want := dvPointers(vals), []string{"/components/schemas/S/maxLength"}; !dvEqual(got, want) {
		t.Fatalf("want only maxLength (a keyword whose value is a sequence or mapping declares nothing):\n want %q\n  got %q", want, got)
	}
}

func TestDeclaredValuesPointerEncodingAndSequenceIndexes(t *testing.T) {
	vals := dvValues(t, `openapi: 3.1.0
paths:
  /notes/{id}:
    get:
      parameters:
        - name: a
          schema: {maxLength: 3}
        - name: b
          schema: {minLength: 2, default: 5}
components:
  schemas:
    S:
      properties:
        "a/b~c": {maxLength: 4}
        "~0": {minLength: 6}
`, nil)
	want := []string{
		"/paths/~1notes~1{id}/get/parameters/0/schema/maxLength",
		"/paths/~1notes~1{id}/get/parameters/1/schema/minLength",
		"/paths/~1notes~1{id}/get/parameters/1/schema/default",
		"/components/schemas/S/properties/a~1b~0c/maxLength",
		"/components/schemas/S/properties/~00/minLength",
	}
	if got := dvPointers(vals); !dvEqual(got, want) {
		t.Fatalf("want RFC 6901 pointers (~ as ~0, / as ~1, sequence items by 0-based index):\n want %q\n  got %q", want, got)
	}
}

func TestDeclaredValuesAnchorsFoundOnceAliasesAndMergeKeysNotFollowed(t *testing.T) {
	vals := dvValues(t, `openapi: 3.1.0
components:
  schemas:
    A: &a
      type: string
      maxLength: 5
    B:
      <<: *a
      minLength: 2
    C: *a
    D:
      maxItems: &n 7
      minItems: *n
`, nil)
	want := []string{
		"/components/schemas/A/maxLength",
		"/components/schemas/B/minLength",
		"/components/schemas/D/maxItems",
	}
	if got := dvPointers(vals); !dvEqual(got, want) {
		t.Fatalf("want a value under an anchor found once at the anchor, and neither aliases (*a, *n) nor merge keys (<<) followed:\n want %q\n  got %q", want, got)
	}
}

func TestDeclaredValuesKinds(t *testing.T) {
	vals := dvValues(t, `openapi: 3.1.0
components:
  schemas:
    S:
      properties:
        quoted: {default: "200"}
        plain: {default: 200}
        plus: {default: +200}
        exp: {default: 2e2}
        dot5: {default: .5}
        negdot5: {default: -.5}
        cent: {default: 0.01}
        neg: {default: -2.5}
        inf: {default: .inf}
        nan: {default: .nan}
        octal: {default: 010}
        hex: {default: 0x1F}
        o17: {default: 0o17}
        under: {default: 1_000}
        bool: {default: true}
        null: {default: null}
        word: {default: hello}
`, nil)
	p := func(n string) string { return "/components/schemas/S/properties/" + n + "/default" }
	type row struct{ name, kind, lit string }
	for _, r := range []row{
		{"plain", "number", "200"}, {"plus", "number", "200"}, {"exp", "number", "200"}, {"dot5", "number", "0.5"},
		{"negdot5", "number", "-0.5"}, {"cent", "number", "0.01"}, {"neg", "number", "-2.5"},
	} {
		v := dvFind(t, vals, p(r.name))
		if v.Kind != r.kind || !dvNumberIs(v.Value, r.lit) || v.Name == "" {
			t.Errorf("%s: want kind number, value %s and a constant name, got kind %q value %v name %q", r.name, r.lit, v.Kind, v.Value, v.Name)
		}
	}
	for n, text := range map[string]string{"plain": "200", "plus": "+200", "exp": "2e2", "dot5": ".5", "negdot5": "-.5"} {
		if v := dvFind(t, vals, p(n)); v.Text != text {
			t.Errorf("%s: want Text %q (as written in the contract), got %q", n, text, v.Text)
		}
	}
	if v := dvFind(t, vals, p("quoted")); v.Kind != "string" || v.Value == nil || v.Value.Kind() != constant.String || constant.StringVal(v.Value) != "200" || v.Name == "" {
		t.Errorf(`"200": want a string with value "200" and a constant name, never the number 200, got kind %q value %v name %q`, v.Kind, v.Value, v.Name)
	}
	if v := dvFind(t, vals, p("word")); v.Kind != "string" || v.Value == nil || constant.StringVal(v.Value) != "hello" {
		t.Errorf("hello: want a string, got kind %q value %v", v.Kind, v.Value)
	}
	for _, n := range []string{"inf", "nan", "octal", "hex", "o17", "under"} {
		if v := dvFind(t, vals, p(n)); v.Kind != "unjudged" || v.Value != nil || v.Name != "" {
			t.Errorf("%s: want kind unjudged, no value, no constant name, got kind %q value %v name %q", n, v.Kind, v.Value, v.Name)
		}
	}
	if v := dvFind(t, vals, p("bool")); v.Kind != "bool" || v.Value != nil || v.Name != "" {
		t.Errorf("true: want kind bool, no value, no constant name, got %+v", v)
	}
	if v := dvFind(t, vals, p("null")); v.Kind != "null" || v.Value != nil || v.Name != "" {
		t.Errorf("null: want kind null, no value, no constant name, got %+v", v)
	}
}

func TestDeclaredValuesRejectsWhatIsNotOneYAMLDocument(t *testing.T) {
	if vals, err := checks.DeclaredValues([]byte("maxLength: 1\n"), nil); err != nil || len(vals) != 1 {
		t.Fatalf("control: want one value and no error for a valid document, got %v, %v", dvPointers(vals), err)
	}
	for name, text := range map[string]string{
		"does not parse":       "a: [\n",
		"two documents":        "a: 1\n---\nb: 2\n",
		"duplicate key":        "maxLength: 1\nmaxLength: 2\n",
		"nested duplicate key": "components:\n  schemas:\n    S:\n      minLength: 1\n      minLength: 2\n",
		"tab indentation":      "a:\n\t- b\n",
	} {
		vals, err := checks.DeclaredValues([]byte(text), nil)
		if err == nil {
			t.Errorf("%s: want an error (the text is not one YAML document without duplicate keys), got values %v", name, dvPointers(vals))
		}
	}
}

// ---- DeclaredValues: names -------------------------------------------------------------------

func TestDeclaredValuesNames(t *testing.T) {
	vals := dvValues(t, `openapi: 3.1.0
paths:
  /notes/{id}:
    get:
      parameters:
        - name: n
          schema: {type: integer, default: 5}
components:
  schemas:
    NewNote:
      properties:
        text: {maxLength: 200}
`, nil)
	if v := dvFind(t, vals, "/components/schemas/NewNote/properties/text/maxLength"); v.Name != "DeclaredNewNoteTextMaxLength" {
		t.Errorf("want DeclaredNewNoteTextMaxLength, got %q", v.Name)
	}
	v := dvFind(t, vals, "/paths/~1notes~1{id}/get/parameters/0/schema/default")
	if v.Name != "DeclaredNotesIdGetParameters0Default" {
		t.Errorf("want DeclaredNotesIdGetParameters0Default (segments paths and schema dropped, ~1 decoded, characters outside [A-Za-z0-9] removed, next character upper-cased), got %q", v.Name)
	}
	if !token.IsIdentifier(v.Name) {
		t.Errorf("want a valid Go identifier for a path pointer, got %q", v.Name)
	}
}

const dvCollide = `openapi: 3.1.0
components:
  schemas:
    NewNote:
      properties:
        text: {maxLength: 1}
    New_Note:
      properties:
        text: {maxLength: 2}
    New-Note:
      properties:
        text: {maxLength: 3}
`

func TestDeclaredValuesNameCollisionsGetNextFreeSuffix(t *testing.T) {
	names := func(vals []checks.Declared) []string {
		var out []string
		for _, v := range vals {
			out = append(out, v.Name)
		}
		return out
	}
	base := "DeclaredNewNoteTextMaxLength"
	if got, want := names(dvValues(t, dvCollide, nil)), []string{base, base + "_2", base + "_3"}; !dvEqual(got, want) {
		t.Errorf("three pointers giving one name: want first free suffix in document order\n want %q\n  got %q", want, got)
	}
	if got, want := names(dvValues(t, dvCollide, map[string]bool{base: true})), []string{base + "_2", base + "_3", base + "_4"}; !dvEqual(got, want) {
		t.Errorf("base name in taken: want the next free suffix for the first and onwards\n want %q\n  got %q", want, got)
	}
	if got, want := names(dvValues(t, dvCollide, map[string]bool{base: true, base + "_2": true})), []string{base + "_3", base + "_4", base + "_5"}; !dvEqual(got, want) {
		t.Errorf("base and _2 in taken: want _3 first\n want %q\n  got %q", want, got)
	}
	if got, want := names(dvValues(t, dvCollide, map[string]bool{"Other": true})), []string{base, base + "_2", base + "_3"}; !dvEqual(got, want) {
		t.Errorf("an unrelated taken name changes nothing\n want %q\n  got %q", want, got)
	}
}

// ---- RenderDeclared --------------------------------------------------------------------------

const dvHeader = "// Code generated by the generate procedure from api/openapi.yaml. DO NOT EDIT.\n\npackage gen"

func TestRenderDeclaredExactShape(t *testing.T) {
	vals := []checks.Declared{
		{Pointer: "/components/schemas/N/properties/t/maxLength", Keyword: "maxLength", Kind: "number", Text: "200", Value: constant.MakeInt64(200), Name: "DeclaredNTMaxLength"},
		{Pointer: "/components/schemas/N/properties/t/enum/0", Keyword: "enum", Kind: "string", Text: "a", Value: constant.MakeString("a")},
		{Pointer: "/components/schemas/N/properties/t/default", Keyword: "default", Kind: "string", Text: "he said \"hi\"\n", Value: constant.MakeString("he said \"hi\"\n"), Name: "DeclaredNTDefault"},
		{Pointer: "/components/schemas/N/properties/b/default", Keyword: "default", Kind: "bool", Text: "true"},
		{Pointer: "/components/schemas/N/properties/z/default", Keyword: "default", Kind: "null", Text: "null"},
		{Pointer: "/components/schemas/N/properties/u/default", Keyword: "default", Kind: "unjudged", Text: "0x1F"},
		{Pointer: "/components/schemas/N/properties/r/multipleOf", Keyword: "multipleOf", Kind: "number", Text: "0.01", Value: constant.MakeFromLiteral("0.01", token.FLOAT, 0), Name: "DeclaredNRMultipleOf"},
	}
	want := dvHeader +
		"\n\n// DeclaredNTMaxLength is declared at " + strconv.Quote("/components/schemas/N/properties/t/maxLength") + ".\nconst DeclaredNTMaxLength = 200" +
		"\n\n// DeclaredNTDefault is declared at " + strconv.Quote("/components/schemas/N/properties/t/default") + ".\nconst DeclaredNTDefault = " + strconv.Quote("he said \"hi\"\n") +
		"\n\n// DeclaredNRMultipleOf is declared at " + strconv.Quote("/components/schemas/N/properties/r/multipleOf") + ".\nconst DeclaredNRMultipleOf = 0.01"
	got := strings.TrimRight(string(checks.RenderDeclared(vals)), "\n")
	if got != want {
		t.Errorf("RenderDeclared: want the header, a blank line, package gen, then per named value a blank line, the %%q pointer comment and the const (none for enum items, bool, null, unjudged), in order.\n want:\n%s\n got:\n%s", want, got)
	}
}

func TestRenderDeclaredWithNoNamedValueIsHeaderAndPackageOnly(t *testing.T) {
	for name, vals := range map[string][]checks.Declared{
		"nil":     nil,
		"empty":   {},
		"unnamed": {{Pointer: "/enum/0", Keyword: "enum", Kind: "string", Text: "a", Value: constant.MakeString("a")}, {Pointer: "/x/default", Keyword: "default", Kind: "bool", Text: "true"}},
	} {
		got := strings.TrimRight(string(checks.RenderDeclared(vals)), "\n")
		if got != dvHeader {
			t.Errorf("%s: want only the header line, a blank line and %q, got:\n%s", name, "package gen", got)
		}
	}
}

func TestRenderDeclaredIsDeterministicAndParsesAsGo(t *testing.T) {
	vals := dvValues(t, dvAllKeywords+`        e: {default: "q\"uote", maxLength: 200}
`, nil)
	a, b := checks.RenderDeclared(vals), checks.RenderDeclared(vals)
	if len(a) == 0 || !bytes.Equal(a, b) {
		t.Fatalf("want the same non-empty bytes for the same values, got %d and %d bytes", len(a), len(b))
	}
	f, err := parser.ParseFile(token.NewFileSet(), "declared.go", a, parser.ParseComments)
	if err != nil {
		t.Fatalf("want gen/declared.go to parse as Go, got %v\n%s", err, a)
	}
	if f.Name.Name != "gen" {
		t.Errorf("want package gen, got %s", f.Name.Name)
	}
	if !strings.HasPrefix(string(a), dvHeader) {
		t.Errorf("want the file to start with the header and package gen, got:\n%s", a)
	}
}

func TestRenderDeclaredNormalisesNumbersAndNeverWritesExactStrings(t *testing.T) {
	vals := dvValues(t, `openapi: 3.1.0
components:
  schemas:
    X:
      properties:
        a: {maximum: +200}
        b: {maximum: .5}
        c: {maximum: -.5}
        d: {maximum: 2e2}
        e: {maximum: 0.1}
        f: {maximum: 0x1F}
        g: {const: "q\"\n"}
`, nil)
	out := string(checks.RenderDeclared(vals))
	for _, line := range []string{
		"const DeclaredXAMaximum = 200\n",
		"const DeclaredXBMaximum = 0.5\n",
		"const DeclaredXCMaximum = -0.5\n",
		"const DeclaredXDMaximum = 2e2\n",
		"const DeclaredXEMaximum = 0.1\n",
		"const DeclaredXGConst = " + strconv.Quote("q\"\n") + "\n",
	} {
		if !strings.Contains(out+"\n", line) {
			t.Errorf("want the line %q (numbers as normalised text, strings by strconv.Quote)", strings.TrimSpace(line))
		}
	}
	if strings.Contains(out, "1/10") || strings.Contains(out, "XFMaximum") {
		t.Errorf("want no exact-fraction form and no constant for the unjudged 0x1F, got:\n%s", out)
	}
}

func TestRenderDeclaredKeepsOddKeysInsideTheComment(t *testing.T) {
	vals := dvValues(t, "openapi: 3.1.0\ncomponents:\n  schemas:\n    S:\n      properties:\n        \"x\\ny*/z\": {maxLength: 5}\n        \"e\\u2028f\": {minLength: 2}\n", nil)
	if len(vals) != 2 || vals[0].Name == "" || vals[1].Name == "" {
		t.Fatalf("want two named values for the odd keys, got %+v", vals)
	}
	out := checks.RenderDeclared(vals)
	f, err := parser.ParseFile(token.NewFileSet(), "declared.go", out, parser.ParseComments)
	if err != nil {
		t.Fatalf("want a file that parses as Go with a newline or */ in a key, got %v\n%s", err, out)
	}
	if n := len(f.Decls); n != 2 {
		t.Errorf("want exactly the two constants (nothing the key's text could smuggle into code), got %d declarations", n)
	}
	if want := strconv.Quote("/components/schemas/S/properties/x\ny*~1z/maxLength"); !strings.Contains(string(out), "declared at "+want+".") {
		t.Errorf("want the pointer written with %%q (%s) in the doc comment, got:\n%s", want, out)
	}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line != "" && !strings.HasPrefix(line, "//") && !strings.HasPrefix(line, "const ") && !strings.HasPrefix(line, "package ") {
			t.Errorf("want every line a comment, a const, the package clause or blank; got %q", line)
		}
	}
}

func TestRenderDeclaredBuildsAndEachConstantEqualsTheDeclaredValue(t *testing.T) {
	vals := dvValues(t, `openapi: 3.1.0
components:
  schemas:
    S:
      properties:
        p1: {maximum: 0.5, minimum: -2.5}
        p2: {multipleOf: 0.01}
        p3: {default: 2e2}
        p4: {maxLength: 200, default: "he\"llo\n\\"}
        p5: {enum: [a, b], default: true}
`, nil)
	dir := dvProject(t, map[string]string{
		"go.mod":          "module example.test\n\ngo 1.21\n",
		"gen/declared.go": string(checks.RenderDeclared(vals)),
		"main.go": `package main

import (
	"fmt"
	"os"

	"example.test/gen"
)

func main() {
	ok := gen.DeclaredSP1Maximum == 0.5 &&
		gen.DeclaredSP1Minimum == -2.5 &&
		gen.DeclaredSP2MultipleOf*100 == 1 &&
		gen.DeclaredSP3Default == 200 &&
		gen.DeclaredSP4MaxLength == 200 &&
		gen.DeclaredSP4Default == "he\"llo\n\\"
	if !ok {
		fmt.Println("constants differ from the declared values")
		os.Exit(1)
	}
	fmt.Println("constants equal the declared values")
}
`,
	})
	res, err := checks.NewGoRunner().Go(dir, "run", ".")
	if err != nil {
		t.Fatalf("could not run the go toolchain: %v", err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Output, "constants equal the declared values") {
		t.Errorf("want a program built with the rendered file to see every constant equal to its declared value (0.5, -2.5, 0.01, 2e2, 200, the quoted string); exit %d:\n%s", res.ExitCode, res.Output)
	}
}

// ---- DeclaredValuesGenerated -----------------------------------------------------------------

const dvGenContract = `openapi: 3.1.0
components:
  schemas:
    NewNote:
      type: object
      properties:
        text:
          type: string
          maxLength: 200
          default: hi
`

func TestDeclaredValuesGenerated(t *testing.T) {
	const check = "DeclaredValuesGenerated"
	rendered := checks.RenderDeclared(dvValues(t, dvGenContract, nil))
	if len(rendered) == 0 {
		t.Fatal("RenderDeclared returned no bytes")
	}

	t.Run("passes on the rendered file", func(t *testing.T) {
		dir := dvProject(t, map[string]string{"gen/declared.go": string(rendered)})
		r := dvWantOnePass(t, checks.DeclaredValuesGenerated(dir, []byte(dvGenContract)), check)
		if !strings.Contains(r.Reason, "gen/declared.go equals the 2 declared values of the approved contract") {
			t.Errorf("want the reason %q, got %q", "gen/declared.go equals the 2 declared values of the approved contract", r.Reason)
		}
	})
	t.Run("fails on a missing file naming it", func(t *testing.T) {
		dir := dvProject(t, map[string]string{"gen/other.go": "package gen\n"})
		rs := checks.DeclaredValuesGenerated(dir, []byte(dvGenContract))
		dvWantAllFail(t, rs, check, "gen/declared.go is missing")
		if !strings.Contains(rs[0].Reason, "gen/declared.go") {
			t.Errorf("want the reason to name gen/declared.go, got %q", rs[0].Reason)
		}
	})
	t.Run("fails on an edited file", func(t *testing.T) {
		dir := dvProject(t, map[string]string{"gen/declared.go": string(rendered) + "\nconst Extra = 1\n"})
		rs := checks.DeclaredValuesGenerated(dir, []byte(dvGenContract))
		dvWantAllFail(t, rs, check, "the file differs from the rendering")
		if !strings.Contains(rs[0].Reason, "gen/declared.go") {
			t.Errorf("want the reason to name gen/declared.go, got %q", rs[0].Reason)
		}
	})
	t.Run("fails on a one-byte difference", func(t *testing.T) {
		edited := strings.Replace(string(rendered), "= 200", "= 201", 1)
		dir := dvProject(t, map[string]string{"gen/declared.go": edited})
		dvWantAllFail(t, checks.DeclaredValuesGenerated(dir, []byte(dvGenContract)), check, "value 201 differs from the contract's 200")
	})
	t.Run("fails on a stale file after the contract changed", func(t *testing.T) {
		changed := strings.Replace(dvGenContract, "maxLength: 200", "maxLength: 300", 1)
		dir := dvProject(t, map[string]string{"gen/declared.go": string(rendered)})
		dvWantAllFail(t, checks.DeclaredValuesGenerated(dir, []byte(changed)), check, "the file renders the old contract")
	})
	t.Run("fails on an approved contract that does not parse", func(t *testing.T) {
		dir := dvProject(t, map[string]string{"gen/declared.go": string(rendered)})
		dvWantOnePass(t, checks.DeclaredValuesGenerated(dir, []byte(dvGenContract)), check) // control: the same file passes for a valid contract
		dvWantAllFail(t, checks.DeclaredValuesGenerated(dir, []byte("a: [\n")), check, "the approved contract does not parse")
		dvWantAllFail(t, checks.DeclaredValuesGenerated(dir, []byte("maxLength: 1\nmaxLength: 2\n")), check, "the approved contract holds a duplicate key")
	})
	t.Run("passes on a contract with no declared value and a header-only file", func(t *testing.T) {
		contract := "openapi: 3.1.0\ncomponents:\n  schemas:\n    S:\n      type: string\n"
		dir := dvProject(t, map[string]string{"gen/declared.go": string(checks.RenderDeclared(dvValues(t, contract, nil)))})
		dvWantOnePass(t, checks.DeclaredValuesGenerated(dir, []byte(contract)), check)
	})
	t.Run("names avoid the top-level names of the project's other gen files", func(t *testing.T) {
		other := "package gen\n\ntype DeclaredNewNoteTextMaxLength struct{}\n\nfunc DeclaredNewNoteTextDefault() {}\n"
		withTaken := checks.RenderDeclared(dvValues(t, dvGenContract, map[string]bool{"DeclaredNewNoteTextMaxLength": true, "DeclaredNewNoteTextDefault": true}))
		if !strings.Contains(string(withTaken), "DeclaredNewNoteTextMaxLength_2") {
			t.Fatalf("precondition: a taken name must get _2, got:\n%s", withTaken)
		}
		dir := dvProject(t, map[string]string{"gen/api.gen.go": other, "gen/declared.go": string(withTaken)})
		dvWantOnePass(t, checks.DeclaredValuesGenerated(dir, []byte(dvGenContract)), check)
		dir = dvProject(t, map[string]string{"gen/api.gen.go": other, "gen/declared.go": string(rendered)})
		dvWantAllFail(t, checks.DeclaredValuesGenerated(dir, []byte(dvGenContract)), check, "the file reuses names the other gen files declare, so the project would not build")
	})
}

// ---- DeclaredValuesNotRepeated ---------------------------------------------------------------

const dvNRContract = `openapi: 3.1.0
components:
  schemas:
    NewNote:
      type: object
      properties:
        text:
          type: string
          maxLength: 200
          default: hello world
        count:
          type: integer
          minimum: -5
          maximum: 10
          default: 10
        ratio:
          type: number
          multipleOf: 0.5
        kind:
          type: string
          enum: [alpha, beta]
          default: alpha
`

const dvNRCode = `package app

import "strconv"

var (
	a    = 200 // @dec
	b    = 0xC8 // @hex
	c    = 2e2 // @exp
	d    = 200.0 // @flt
	e    = -5 // @neg
	f    = -(5) // @paren
	g    = 5 // @pos
	h    = -200 // @negbig
	half = .5 // @half
	s1   = "hello world" // @interp
	s2   = ` + "`hello world`" + ` // @raw
	s3   = "hello\x20world" // @esc
	s4   = "hello" // @prefix
	s5   = "200" // @numstr
	en   = "beta" // @enum
	al   = "alpha" // @alpha
	ten  = 10 // @ten
	sum  = 3 + 4 // @sum
)

func p(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64) // @parse
}
`

const (
	dvTextMax = "/components/schemas/NewNote/properties/text/maxLength"
	dvTextDef = "/components/schemas/NewNote/properties/text/default"
	dvCntMin  = "/components/schemas/NewNote/properties/count/minimum"
	dvCntMax  = "/components/schemas/NewNote/properties/count/maximum"
	dvCntDef  = "/components/schemas/NewNote/properties/count/default"
	dvRatio   = "/components/schemas/NewNote/properties/ratio/multipleOf"
	dvKindDef = "/components/schemas/NewNote/properties/kind/default"
	dvKindE0  = "/components/schemas/NewNote/properties/kind/enum/0"
	dvKindE1  = "/components/schemas/NewNote/properties/kind/enum/1"
	dvHint    = "; if the literal does not stand for this value, write it another way"
)

func dvNRRun(t *testing.T, files map[string]string, writable []string, contract string) []checks.CheckResult {
	t.Helper()
	return checks.DeclaredValuesNotRepeated(dvProject(t, files), writable, []byte(contract))
}

func TestDeclaredValuesNotRepeatedFailsOncePerRepeatedLiteral(t *testing.T) {
	const check = "DeclaredValuesNotRepeated"
	const file = "internal/app/app.go"
	rs := dvNRRun(t, map[string]string{file: dvNRCode}, []string{"internal/**"}, dvNRContract)
	dvEveryResultIs(t, rs, check)

	type want struct {
		marker, name string
		pointers     []string
		enumHint     bool
	}
	wants := []want{
		{"@dec", "DeclaredNewNoteTextMaxLength", []string{dvTextMax}, false},
		{"@hex", "DeclaredNewNoteTextMaxLength", []string{dvTextMax}, false},
		{"@exp", "DeclaredNewNoteTextMaxLength", []string{dvTextMax}, false},
		{"@flt", "DeclaredNewNoteTextMaxLength", []string{dvTextMax}, false},
		{"@neg", "DeclaredNewNoteCountMinimum", []string{dvCntMin}, false},
		{"@half", "DeclaredNewNoteRatioMultipleOf", []string{dvRatio}, false},
		{"@interp", "DeclaredNewNoteTextDefault", []string{dvTextDef}, false},
		{"@raw", "DeclaredNewNoteTextDefault", []string{dvTextDef}, false},
		{"@esc", "DeclaredNewNoteTextDefault", []string{dvTextDef}, false},
		{"@enum", "", []string{dvKindE1}, true},
		{"@alpha", "DeclaredNewNoteKindDefault", []string{dvKindE0, dvKindDef}, true},
		{"@ten", "DeclaredNewNoteCountMaximum", []string{dvCntMax, dvCntDef}, false},
		{"@parse", "DeclaredNewNoteCountMaximum", []string{dvCntMax, dvCntDef}, false},
	}
	if got := len(dvFailures(rs)); got != len(wants) {
		for _, r := range rs {
			t.Logf("result passed=%v: %s", r.Passed, r.Reason)
		}
		t.Fatalf("want exactly %d failing results, one per repeated literal (not for -(5), 5, -200, \"hello\", \"200\", 3 + 4), got %d", len(wants), got)
	}
	for _, r := range rs {
		if r.Passed {
			t.Errorf("want only failures, got a pass: %q", r.Reason)
		}
	}
	for _, w := range wants {
		prefix := file + ":" + strconv.Itoa(dvLine(t, dvNRCode, w.marker)) + " repeats "
		var hit *checks.CheckResult
		n := 0
		for i := range rs {
			if strings.Contains(rs[i].Reason, prefix) {
				hit = &rs[i]
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s: want exactly one result starting %q (<file>:<line> repeats), got %d", w.marker, prefix, n)
			continue
		}
		r := hit.Reason
		if !strings.Contains(r, "declared at #"+strings.Join(w.pointers, ", #")) {
			t.Errorf("%s: want every declaring pointer after #, in document order, %q; got %q", w.marker, "#"+strings.Join(w.pointers, ", #"), r)
		}
		if w.enumHint {
			if !strings.Contains(r, "oapi-codegen") || !strings.Contains(r, "gen/api.gen.go") {
				t.Errorf("%s: want the enum hint (the constant oapi-codegen generated in gen/api.gen.go), got %q", w.marker, r)
			}
		}
		if w.name != "" && !strings.Contains(r, "use gen."+w.name) {
			t.Errorf("%s: want %q, got %q", w.marker, "use gen."+w.name, r)
		}
		if !strings.HasSuffix(strings.TrimSpace(r), strings.TrimSpace(dvHint)) || !strings.Contains(r, dvHint) {
			t.Errorf("%s: want the reason to end with %q, got %q", w.marker, dvHint, r)
		}
	}
	// the quoted value (%q) for literals that spell the value as the contract does
	for marker, q := range map[string]string{"@dec": `"200"`, "@interp": `"hello world"`, "@raw": `"hello world"`, "@esc": `"hello world"`, "@alpha": `"alpha"`} {
		prefix := file + ":" + strconv.Itoa(dvLine(t, dvNRCode, marker)) + " repeats " + q + ", declared at #"
		found := false
		for _, r := range rs {
			found = found || strings.Contains(r.Reason, prefix)
		}
		if !found {
			t.Errorf("%s: want the reason to read %q (the value as %%q, then the pointers)", marker, prefix)
		}
	}
}

func TestDeclaredValuesNotRepeatedStringNeverEqualsNumber(t *testing.T) {
	// declared number 200, literal "200": passes; declared string "200", literal 200: passes, "200" fails.
	numberContract := "openapi: 3.1.0\ncomponents:\n  schemas:\n    S:\n      properties:\n        t: {maxLength: 200}\n"
	rs := dvNRRun(t, map[string]string{"internal/a.go": "package a\n\nvar s = \"200\"\n"}, []string{"internal/**"}, numberContract)
	dvWantOnePass(t, rs, "DeclaredValuesNotRepeated")

	stringContract := "openapi: 3.1.0\ncomponents:\n  schemas:\n    S:\n      properties:\n        t: {default: \"200\"}\n"
	rs = dvNRRun(t, map[string]string{"internal/a.go": "package a\n\nvar n = 200\n"}, []string{"internal/**"}, stringContract)
	dvWantOnePass(t, rs, "DeclaredValuesNotRepeated")
	rs = dvNRRun(t, map[string]string{"internal/a.go": "package a\n\nvar s = \"200\"\n"}, []string{"internal/**"}, stringContract)
	dvWantAllFail(t, rs, "DeclaredValuesNotRepeated", `the string "200" repeats the declared string "200"`)
	if len(rs) != 1 || !strings.Contains(rs[0].Reason, "internal/a.go:3 repeats") {
		t.Errorf("want one failure naming internal/a.go:3, got %+v", rs)
	}
}

func TestDeclaredValuesNotRepeatedNegativeNumbers(t *testing.T) {
	// minimum: 5 declared; -5 is not 5, but a parenthesised -(5) is not negated, so its 5 repeats.
	pos := "openapi: 3.1.0\ncomponents:\n  schemas:\n    S:\n      properties:\n        t: {minimum: 5}\n"
	rs := dvNRRun(t, map[string]string{"internal/a.go": "package a\n\nvar x = -5\n"}, []string{"internal/**"}, pos)
	dvWantOnePass(t, rs, "DeclaredValuesNotRepeated")
	rs = dvNRRun(t, map[string]string{"internal/a.go": "package a\n\nvar x = -(5)\n"}, []string{"internal/**"}, pos)
	dvWantAllFail(t, rs, "DeclaredValuesNotRepeated", "-(5): the parenthesised literal is not negated, so it is 5 and repeats minimum: 5")
}

func TestDeclaredValuesNotRepeatedNeverJudgesTheExemptAndUnjudgedAndNonLiterals(t *testing.T) {
	contract := `openapi: 3.1.0
components:
  schemas:
    S:
      properties:
        a: {minLength: 0, minItems: 1, minimum: -1, exclusiveMinimum: 0.0, default: 0}
        b: {default: ""}
        c: {default: true, const: 0x1F, multipleOf: 010}
        d: {const: text, maxLength: 97, exclusiveMaximum: 3, default: null}
        e: {const: fmt, maxLength: 200}
`
	code := `package app

import "fmt"

// 200 "text" 97 are only in comments.

/* "text" 200 */

type T struct {
	A string ` + "`text`" + `
}

func f() {
	_ = 0
	_ = 1
	_ = -1
	_ = ""
	_ = 0.0
	_ = 0x0
	_ = 'a'
	_ = 3i
	_ = 31
	_ = 8
	_ = fmt.Sprint
}
`
	rs := dvNRRun(t, map[string]string{
		"internal/app/app.go":      code,
		"internal/app/app_test.go": "package app\n\nvar x = 200\nvar y = \"text\"\n",
	}, []string{"internal/**"}, contract)
	for _, r := range rs {
		t.Logf("result passed=%v: %s", r.Passed, r.Reason)
	}
	dvWantOnePass(t, rs, "DeclaredValuesNotRepeated")
}

func TestDeclaredValuesNotRepeatedJudgesOnlyWritableNonTestGoFiles(t *testing.T) {
	contract := "openapi: 3.1.0\ncomponents:\n  schemas:\n    S:\n      properties:\n        t: {maxLength: 200, default: \"other\"}\n"
	files := map[string]string{
		"internal/a.go":      "package a\n\nvar x = 7\nvar y = 8.5\nvar z = \"q\"\n",
		"internal/a_test.go": "package a\n\nvar t1 = 200\n",
		"other/o.go":         "package o\n\nvar o1 = 200\n",
		"internal/notes.txt": "200 other\n",
	}
	r := dvWantOnePass(t, dvNRRun(t, files, []string{"internal/**"}, contract), "DeclaredValuesNotRepeated")
	if want := "examined 1 files and 3 literals against 2 declared values: none repeated"; !strings.Contains(r.Reason, want) {
		t.Errorf("want the reason to say %q (only the writable, non-test .go file counts), got %q", want, r.Reason)
	}
	// an exact-path writable entry matches the one file
	files["cmd/main.go"] = "package main\n\nvar m = 200\n"
	rs := dvNRRun(t, files, []string{"internal/**", "cmd/main.go"}, contract)
	dvWantAllFail(t, rs, "DeclaredValuesNotRepeated", "cmd/main.go is writable and repeats 200")
	if len(rs) != 1 || !strings.Contains(rs[0].Reason, "cmd/main.go:3 repeats") {
		t.Errorf("want one failure naming cmd/main.go:3, got %+v", rs)
	}
}

func TestDeclaredValuesNotRepeatedPassesAndCountsWhatItExamined(t *testing.T) {
	const check = "DeclaredValuesNotRepeated"
	t.Run("a contract with no declared value", func(t *testing.T) {
		contract := "openapi: 3.1.0\ncomponents:\n  schemas:\n    S:\n      type: string\n"
		r := dvWantOnePass(t, dvNRRun(t, map[string]string{"internal/a.go": "package a\n\nvar x = 200\n"}, []string{"internal/**"}, contract), check)
		for _, want := range []string{"examined 1 files", "0 declared values", "none repeated"} {
			if !strings.Contains(r.Reason, want) {
				t.Errorf("want the reason to contain %q, got %q", want, r.Reason)
			}
		}
	})
	t.Run("code using the constants", func(t *testing.T) {
		code := "package app\n\nimport \"example.test/gen\"\n\nfunc max() int { return gen.DeclaredNewNoteTextMaxLength }\n"
		r := dvWantOnePass(t, dvNRRun(t, map[string]string{"internal/app/ok.go": code}, []string{"internal/**"}, dvNRContract), check)
		for _, want := range []string{"examined 1 files", "declared values", "none repeated"} {
			if !strings.Contains(r.Reason, want) {
				t.Errorf("want the reason to contain %q, got %q", want, r.Reason)
			}
		}
	})
	t.Run("import paths are not judged", func(t *testing.T) {
		contract := "openapi: 3.1.0\ncomponents:\n  schemas:\n    S:\n      properties:\n        t: {default: fmt}\n"
		dvWantOnePass(t, dvNRRun(t, map[string]string{"internal/a.go": "package a\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n"}, []string{"internal/**"}, contract), check)
	})
}

func TestDeclaredValuesNotRepeatedCutsALongValueInTheReason(t *testing.T) {
	long := strings.Repeat("abcdefghij", 7) // 70 characters
	contract := "openapi: 3.1.0\ncomponents:\n  schemas:\n    S:\n      properties:\n        t: {default: " + long + "}\n"
	rs := dvNRRun(t, map[string]string{"internal/a.go": "package a\n\nvar s = \"" + long + "\"\n"}, []string{"internal/**"}, contract)
	dvWantAllFail(t, rs, "DeclaredValuesNotRepeated", "the 70-character string repeats the declared default")
	r := rs[0].Reason
	if !strings.Contains(r, long[:64]) {
		t.Errorf("want the first 64 characters of the value in the reason, got %q", r)
	}
	if strings.Contains(r, long) {
		t.Errorf("want the value cut to 64 characters, but the reason holds all 70: %q", r)
	}
}

func TestDeclaredValuesNotRepeatedFailsClosed(t *testing.T) {
	const check = "DeclaredValuesNotRepeated"
	good := map[string]string{"internal/ok.go": "package a\n\nvar x = 7\n"}

	t.Run("a judged file that does not parse", func(t *testing.T) {
		files := map[string]string{"internal/ok.go": good["internal/ok.go"], "internal/bad.go": "package a\n\nfunc {\n"}
		rs := dvNRRun(t, files, []string{"internal/**"}, dvNRContract)
		if len(dvFailures(rs)) == 0 {
			t.Fatalf("want a failing result for the file that does not parse, got %+v", rs)
		}
		dvEveryResultIs(t, rs, check)
		named := false
		for _, r := range dvFailures(rs) {
			named = named || strings.Contains(r.Reason, "internal/bad.go")
		}
		if !named {
			t.Errorf("want the failure to name internal/bad.go, got %+v", rs)
		}
		for _, r := range rs {
			if r.Passed {
				t.Errorf("want no pass when a judged file could not be read, got %q", r.Reason)
			}
		}
	})
	t.Run("an unparseable file outside writable is not read", func(t *testing.T) {
		files := map[string]string{"internal/ok.go": good["internal/ok.go"], "other/bad.go": "package a\n\nfunc {\n"}
		dvWantOnePass(t, dvNRRun(t, files, []string{"internal/**"}, dvNRContract), check)
	})
	t.Run("a missing approved contract", func(t *testing.T) {
		dvWantOnePass(t, checks.DeclaredValuesNotRepeated(dvProject(t, good), []string{"internal/**"}, []byte(dvNRContract)), check) // control
		dvWantAllFail(t, checks.DeclaredValuesNotRepeated(dvProject(t, good), []string{"internal/**"}, nil), check, "the approved contract is missing")
	})
	t.Run("a broken approved contract", func(t *testing.T) {
		dvWantOnePass(t, checks.DeclaredValuesNotRepeated(dvProject(t, good), []string{"internal/**"}, []byte(dvNRContract)), check) // control
		for name, text := range map[string]string{"does not parse": "a: [\n", "duplicate key": "maxLength: 1\nmaxLength: 2\n", "two documents": "a: 1\n---\nb: 2\n"} {
			dvWantAllFail(t, checks.DeclaredValuesNotRepeated(dvProject(t, good), []string{"internal/**"}, []byte(text)), check, "the approved contract "+name)
		}
	})
}
