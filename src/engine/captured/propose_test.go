package captured_test

// Tests of captured.Propose and captured.Check (feature 011, task T003: spec FR-001, FR-003,
// SC-006; research R4 and E1; contract draft contracts/captured-patterns.md "Go interface"). They
// are written from the interface's doc comment, never from an implementation: expected places
// come from a small independent scanner in this file (t3Scan), and a few places are pinned by
// hand.
//
// The fixtures are testdata/notes (the evals' accepted notes project and its slice) and
// testdata/limit (the same with feature 009's `maxLength: 200` and an example that refuses "a text
// of 201 characters"). A slice part's Place has Line 1 and Column in the part's canonical JSON
// text, the text Copy holds (an ambiguity the report names).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"encoding/json"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/captured"
)

// ---------------------------------------------------------------------------------------------
// Fixture loading

type t3Fixture struct {
	src      captured.Copy
	template captured.Template
	contract string
	feature  string
}

// t3Convert turns decoded JSON into canonical.Object and canonical.Array, the way the slices
// service's part builders (slices/canonical.go goalsPart and rulesPart) build a part: an object
// of strings and arrays.
func t3Convert(v any) any {
	switch v := v.(type) {
	case map[string]any:
		o := canonical.Object{}
		for k, e := range v {
			o[k] = t3Convert(e)
		}
		return o
	case []any:
		a := canonical.Array{}
		for _, e := range v {
			a = append(a, t3Convert(e))
		}
		return a
	default:
		return v
	}
}

func t3Read(t testing.TB, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// t3Copy builds a slice copy as slices.ApprovedParts does: each part's canonical JSON, the goals
// part {goals}, the rules part {rules, examples, terms} and the contract part {contract}.
func t3Copy(t testing.TB, goalsJSON, rulesJSON, feature, contract string) captured.Copy {
	t.Helper()
	var goals, rules []any
	if err := json.Unmarshal([]byte(goalsJSON), &goals); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(rulesJSON), &rules); err != nil {
		t.Fatal(err)
	}
	var c captured.Copy
	var err error
	if c.Goals, err = canonical.JSON(canonical.Object{"goals": t3Convert(goals)}); err != nil {
		t.Fatal(err)
	}
	if c.Rules, err = canonical.JSON(canonical.Object{"rules": t3Convert(rules), "examples": feature, "terms": canonical.Array{}}); err != nil {
		t.Fatal(err)
	}
	if c.Contract, err = canonical.JSON(canonical.Object{"contract": contract}); err != nil {
		t.Fatal(err)
	}
	return c
}

// t3Tree reads the files below dir as canonical.Files with slash paths relative to dir, in path
// order.
func t3Tree(t testing.TB, dir string) []canonical.File {
	t.Helper()
	var files []canonical.File
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		b, err := os.ReadFile(p)
		files = append(files, canonical.File{Path: filepath.ToSlash(rel), Content: b})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// t3Load reads testdata/<name>: slice/ (the copy), ref/tests (the `tests` output: the writable
// entries `tests/**` of the base pattern's tests step) and ref/impl (the `code` output: `cmd/**`
// and `internal/**` of the code step); `witnesses` is empty (no review round wrote a witness).
func t3Load(t testing.TB, name string) t3Fixture {
	t.Helper()
	root := filepath.Join("testdata", name)
	f := t3Fixture{
		contract: t3Read(t, root, "slice", "contract.yaml"),
		feature:  t3Read(t, root, "slice", "slice.feature"),
	}
	f.src = t3Copy(t, t3Read(t, root, "slice", "goals.json"), t3Read(t, root, "slice", "rules.json"), f.feature, f.contract)
	f.template = captured.Template{
		"tests":     t3Tree(t, filepath.Join(root, "ref", "tests")),
		"code":      t3Tree(t, filepath.Join(root, "ref", "impl")),
		"witnesses": nil,
	}
	return f
}

// t3WithContract is the fixture's copy with the contract text replaced.
func (f t3Fixture) withContract(t testing.TB, contract string) captured.Copy {
	t.Helper()
	c := f.src
	b, err := canonical.JSON(canonical.Object{"contract": contract})
	if err != nil {
		t.Fatal(err)
	}
	c.Contract = b
	return c
}

// t3WithFile is a template with file added to its output.
func t3WithFile(tpl captured.Template, output string, f canonical.File) captured.Template {
	out := captured.Template{}
	for k, v := range tpl {
		out[k] = slices.Clone(v)
	}
	out[output] = append(out[output], f)
	return out
}

// Inline text the notes fixture lacks (research E1): `nOTE` is none of the three forms; `NOTE`,
// `note_id` and `denote` are. Line 4, by hand: `var ` is columns 1-4, NOTE 5-8, `, ` 9-10, note_id
// 11-17 (note at 11), `, ` 18-19, denote 20-25 (note at 22), `, ` 26-27, nOTE 28-31.
var t3Forms = canonical.File{
	Path:    "internal/app/forms.go",
	Content: []byte("package app\n\n// nOTE stays as it is.\nvar NOTE, note_id, denote, nOTE = 1, 2, 3, 4\n"),
}

// The forms file's line 4 is `var NOTE, ...`: line numbers below count from the file's first line
// (`package app` is line 1, the blank line 2, the comment 3, the var 4).
const (
	t3FormsLine = 4
)

// Numbers around 200, by hand (columns of the line `2001 1.200 200ms v200 -200 "200" 200. 200`:
// 2001 1-4, 1.200 6-10, 200ms 12-16, v200 18-21, -200 23-26, "200" 28-32, 200. 34-37, 200 39-41),
// then a line of text that never matches, then 200 alone as the last bytes with no line feed.
var t3Numbers = canonical.File{
	Path:    "docs/numbers.txt",
	Content: []byte("2001 1.200 200ms v200 -200 \"200\" 200. 200\n200.5 _200 200_ a200 200a 1200\n200"),
}

// ---------------------------------------------------------------------------------------------
// The independent scanner

type t3Spec struct {
	Name  string
	Kind  captured.Kind
	Value string
}

type t3Cand struct {
	idx  int
	form captured.Form
	text string
}

func t3Cands(specs []t3Spec) []t3Cand {
	var out []t3Cand
	for i, s := range specs {
		if s.Value == "" { // an empty value occurs nowhere
			continue
		}
		if s.Kind != captured.KindName {
			out = append(out, t3Cand{i, captured.FormExact, s.Value})
			continue
		}
		out = append(out,
			t3Cand{i, captured.FormLower, s.Value},
			t3Cand{i, captured.FormCapitalised, strings.ToUpper(s.Value[:1]) + s.Value[1:]},
			t3Cand{i, captured.FormUpper, strings.ToUpper(s.Value)})
	}
	return out
}

func t3Word(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// t3WholeNumber is the contract's rule: the characters around a number are not digits, letters or
// `_`, and not a `.` followed by a digit (before the number, a `.` is followed by the number's
// first digit, so it excludes; after it, a `.` excludes only when a digit follows it).
func t3WholeNumber(text string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(text[:start])
		if t3Word(r) || r == '.' {
			return false
		}
	}
	if end < len(text) {
		r, n := utf8.DecodeRuneInString(text[end:])
		if t3Word(r) {
			return false
		}
		if r == '.' && end+n < len(text) {
			if next, _ := utf8.DecodeRuneInString(text[end+n:]); unicode.IsDigit(next) {
				return false
			}
		}
	}
	return true
}

type t3Hit struct {
	idx  int
	form captured.Form
	off  int
}

// t3Scan finds the places of every spec in text: left to right, the longest source form first at
// each position, never overlapping, a number only as a whole number.
func t3Scan(text string, specs []t3Spec) []t3Hit {
	cands := t3Cands(specs)
	var hits []t3Hit
	for i := 0; i < len(text); {
		best := -1
		for ci, c := range cands {
			if !strings.HasPrefix(text[i:], c.text) {
				continue
			}
			if specs[c.idx].Kind == captured.KindNumber && !t3WholeNumber(text, i, i+len(c.text)) {
				continue
			}
			if best < 0 || len(c.text) > len(cands[best].text) {
				best = ci
			}
		}
		if best < 0 {
			i++
			continue
		}
		hits = append(hits, t3Hit{cands[best].idx, cands[best].form, i})
		i += len(cands[best].text)
	}
	return hits
}

func t3Pos(text string, off int) (line, col int) {
	line = 1
	start := 0
	for i, r := range text[:off] {
		if r == '\n' {
			line++
			start = i + 1
		}
	}
	return line, utf8.RuneCountInString(text[start:off]) + 1
}

// t3Places is, per spec, every place in the copy's three parts and in the template's files
// (contents and paths).
func t3Places(src captured.Copy, tpl captured.Template, specs []t3Spec) [][]captured.Place {
	out := make([][]captured.Place, len(specs))
	add := func(part string, in captured.In, text string) {
		for _, h := range t3Scan(text, specs) {
			line, col := t3Pos(text, h.off)
			out[h.idx] = append(out[h.idx], captured.Place{Part: part, In: in, Line: line, Column: col, Form: h.form})
		}
	}
	add("slice/goals", captured.InContent, string(src.Goals))
	add("slice/rules", captured.InContent, string(src.Rules))
	add("slice/contract", captured.InContent, string(src.Contract))
	outputs := make([]string, 0, len(tpl))
	for o := range tpl {
		outputs = append(outputs, o)
	}
	sort.Strings(outputs)
	for _, o := range outputs {
		for _, f := range tpl[o] {
			add(f.Path, captured.InContent, string(f.Content))
			add(f.Path, captured.InPath, f.Path)
		}
	}
	return out
}

// t3Params builds the parameters of specs with their computed places.
func t3Params(src captured.Copy, tpl captured.Template, specs []t3Spec) []captured.Parameter {
	places := t3Places(src, tpl, specs)
	var out []captured.Parameter
	for i, s := range specs {
		p := captured.Parameter{Name: s.Name, Kind: s.Kind, Value: s.Value, Places: places[i],
			Source: captured.SourceName, Origin: captured.OriginProposed}
		if s.Kind != captured.KindName {
			p.Source, p.Origin = captured.SourceLiteral, captured.OriginAdded
		}
		out = append(out, p)
	}
	return out
}

var (
	t3Note  = t3Spec{"note", captured.KindName, "note"}
	t3Notes = t3Spec{"notes", captured.KindName, "notes"}
)

func t3Key(p captured.Place) string {
	return fmt.Sprintf("%s|%s|%04d|%04d|%s", p.Part, p.In, p.Line, p.Column, p.Form)
}

// t3SamePlaces fails when got and want differ as sets of (part, in, line, column, form); the order
// of a parameter's places is not part of the interface.
func t3SamePlaces(t testing.TB, what string, got, want []captured.Place) {
	t.Helper()
	g, w := map[string]int{}, map[string]int{}
	for _, p := range got {
		g[t3Key(p)]++
	}
	for _, p := range want {
		w[t3Key(p)]++
	}
	if reflect.DeepEqual(g, w) {
		return
	}
	var missing, extra []string
	for k, n := range w {
		if g[k] != n {
			missing = append(missing, k)
		}
	}
	for k, n := range g {
		if w[k] != n {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	t.Errorf("%s: %d places, want %d; missing %v; unexpected %v", what, len(got), len(want), clip(missing), clip(extra))
}

func clip(s []string) []string {
	if len(s) > 8 {
		return append(s[:8:8], fmt.Sprintf("... %d more", len(s)-8))
	}
	return s
}

func t3Find(ps []captured.Parameter, value string) *captured.Parameter {
	for i := range ps {
		if ps[i].Value == value {
			return &ps[i]
		}
	}
	return nil
}

func t3HasPlace(ps []captured.Place, want captured.Place) bool {
	for _, p := range ps {
		if t3Key(p) == t3Key(want) {
			return true
		}
	}
	return false
}

// t3NotStub fails when err is the stub's "not implemented", so a test that expects an error is
// not satisfied by the stub.
func t3NotStub(t testing.TB, err error) {
	t.Helper()
	if errors.Is(err, captured.ErrNotImplemented) {
		t.Fatalf("got the stub's %q, want the interface's own answer", err)
	}
}

// ---------------------------------------------------------------------------------------------
// Propose

// T003, FR-001, FR-003: `note` and `notes` are candidates, from operation and schema names (split
// at case changes) and schema property names, each with every place, in the parts and the
// template, contents and paths.
func TestProposeGivesNoteAndNotesWithEveryPlace(t *testing.T) {
	f := t3Load(t, "notes")
	got, err := captured.Propose(f.src, f.template)
	if err != nil {
		t.Fatal(err)
	}
	want := t3Places(f.src, f.template, []t3Spec{t3Notes, t3Note})
	for i, s := range []t3Spec{t3Notes, t3Note} {
		p := t3Find(got, s.Value)
		if p == nil {
			t.Fatalf("no candidate %q among %d", s.Value, len(got))
		}
		if p.Kind != captured.KindName || p.Source != captured.SourceName || p.Origin != captured.OriginProposed {
			t.Errorf("%s: kind %q source %q origin %q, want name, name, proposed", s.Value, p.Kind, p.Source, p.Origin)
		}
		if len(want[i]) == 0 {
			t.Fatalf("test bug: the scanner found no %q", s.Value)
		}
		t3SamePlaces(t, s.Value, p.Places, want[i])
	}
}

// T003: the exact places, by hand, so the scanner above is not its own judge. Columns count
// characters from 1; for a path, Line is 1.
func TestProposePinsPlacesByHand(t *testing.T) {
	f := t3Load(t, "notes")
	tpl := t3WithFile(f.template, "code", t3Forms)
	got, err := captured.Propose(f.src, tpl)
	if err != nil {
		t.Fatal(err)
	}
	notes, note := t3Find(got, "notes"), t3Find(got, "note")
	if notes == nil || note == nil {
		t.Fatalf("candidates note %v, notes %v", note != nil, notes != nil)
	}
	main, app := "cmd/notes/main.go", "internal/app/app.go"
	for _, w := range []captured.Place{
		// `// Command notes serves the notes API.` line 1: notes at 12 and 29.
		{Part: main, In: captured.InContent, Line: 1, Column: 12, Form: captured.FormLower},
		{Part: main, In: captured.InContent, Line: 1, Column: 29, Form: captured.FormLower},
		// the path `cmd/notes/main.go`: `cmd/` is 4 characters.
		{Part: main, In: captured.InPath, Line: 1, Column: 5, Form: captured.FormLower},
		// `// Package app is the in-memory notes service` line 1: notes at 33.
		{Part: app, In: captured.InContent, Line: 1, Column: 33, Form: captured.FormLower},
		// `// ListNotes returns every note, ...` line 29: `// List` is 7 characters.
		{Part: app, In: captured.InContent, Line: 29, Column: 8, Form: captured.FormCapitalised},
	} {
		if !t3HasPlace(notes.Places, w) {
			t.Errorf("notes lacks place %s", t3Key(w))
		}
		if t3HasPlace(note.Places, w) {
			t.Errorf("note has place %s that belongs to notes (longest first)", t3Key(w))
		}
	}
	forms := t3Forms.Path
	for _, w := range []captured.Place{
		{Part: forms, In: captured.InContent, Line: t3FormsLine, Column: 5, Form: captured.FormUpper},
		{Part: forms, In: captured.InContent, Line: t3FormsLine, Column: 11, Form: captured.FormLower},
		{Part: forms, In: captured.InContent, Line: t3FormsLine, Column: 22, Form: captured.FormLower},
	} {
		if !t3HasPlace(note.Places, w) {
			t.Errorf("note lacks place %s (NOTE, note_id, denote)", t3Key(w))
		}
	}
	// nOTE (columns 28-31) is none of the three forms: no place anywhere on it.
	for _, p := range append(slices.Clone(note.Places), notes.Places...) {
		if p.Part == forms && p.Line == t3FormsLine && p.Column >= 28 && p.Column <= 31 {
			t.Errorf("a place %s on nOTE, which is none of the three forms", t3Key(p))
		}
	}
	// The slice's own places: the contract holds `operationId: listNotes` and the schema `Note`.
	if !t3HasAnyIn(notes.Places, "slice/contract") || !t3HasAnyIn(note.Places, "slice/contract") ||
		!t3HasAnyIn(notes.Places, "slice/rules") || !t3HasAnyIn(note.Places, "slice/rules") {
		t.Error("note and notes each need places in slice/rules and slice/contract")
	}
}

func t3HasAnyIn(ps []captured.Place, part string) bool {
	for _, p := range ps {
		if p.Part == part {
			return true
		}
	}
	return false
}

// T003, research R4: longest first and never overlapping: no place of any candidate overlaps
// another's (same part, same line, overlapping columns), and a `notes` place is not counted again
// for `note`.
func TestProposePlacesNeverOverlap(t *testing.T) {
	f := t3Load(t, "notes")
	got, err := captured.Propose(f.src, f.template)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no candidates")
	}
	type span struct {
		part, in string
		line     int
		lo, hi   int
		who      string
	}
	var spans []span
	for _, p := range got {
		for _, pl := range p.Places {
			spans = append(spans, span{pl.Part, string(pl.In), pl.Line, pl.Column, pl.Column + utf8.RuneCountInString(p.Value), p.Value})
		}
	}
	sort.Slice(spans, func(i, j int) bool {
		a, b := spans[i], spans[j]
		if a.part != b.part {
			return a.part < b.part
		}
		if a.in != b.in {
			return a.in < b.in
		}
		if a.line != b.line {
			return a.line < b.line
		}
		return a.lo < b.lo
	})
	for i := 1; i < len(spans); i++ {
		a, b := spans[i-1], spans[i]
		if a.part == b.part && a.in == b.in && a.line == b.line && b.lo < a.hi {
			t.Fatalf("%q at %s %s:%d:%d-%d overlaps %q at column %d", a.who, a.in, a.part, a.line, a.lo, a.hi, b.who, b.lo)
		}
	}
}

// T003, FR-003: the other names the contract states are candidates too: schema property names
// (`id`, `text`, `message`) and the words of operation IDs and schema names split at case changes
// (`list`, `create`, `new`, `error`, with `notes` and `note`).
func TestProposeNamesFromTheContract(t *testing.T) {
	f := t3Load(t, "notes")
	got, err := captured.Propose(f.src, f.template)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range got {
		if seen[p.Name] {
			t.Errorf("two candidates named %q", p.Name)
		}
		seen[p.Name] = true
		if p.Kind == captured.KindName && p.Value != strings.ToLower(p.Value) {
			t.Errorf("name candidate %q is not in its lowercase form", p.Value)
		}
	}
	for _, v := range []string{"note", "notes", "list", "create", "new", "error", "id", "text", "message"} {
		if t3Find(got, v) == nil {
			t.Errorf("no candidate %q", v)
		}
	}
}

// T003, research E1: no status code is proposed. The notes contract declares no limit, so no
// number is a candidate (`200`, `201` and `400` are response statuses).
func TestProposeNoStatusCode(t *testing.T) {
	f := t3Load(t, "notes")
	got, err := captured.Propose(f.src, f.template)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if p.Kind == captured.KindNumber {
			t.Errorf("number candidate %q on a contract that declares no value", p.Value)
		}
		switch p.Value {
		case "200", "201", "400":
			t.Errorf("status code %s proposed", p.Value)
		}
	}
}

// T003, FR-003, SC-006: on the limit fixture the declared value `200` is a candidate, a number
// from a declared value, with every place it occurs as a whole number: the limit, the statuses
// that equal it, the example's and the code's `200`. `201` and `400` (statuses) are not proposed.
func TestProposeDeclaredValue(t *testing.T) {
	f := t3Load(t, "limit")
	got, err := captured.Propose(f.src, f.template)
	if err != nil {
		t.Fatal(err)
	}
	p := t3Find(got, "200")
	if p == nil {
		t.Fatal("the declared value 200 is not a candidate")
	}
	if p.Kind != captured.KindNumber || p.Source != captured.SourceDeclaredValue || p.Origin != captured.OriginProposed {
		t.Errorf("kind %q source %q origin %q, want number, declared-value, proposed", p.Kind, p.Source, p.Origin)
	}
	two := t3Spec{"limit", captured.KindNumber, "200"}
	want := t3Places(f.src, f.template, []t3Spec{two})[0]
	t3SamePlaces(t, "200", p.Places, want)
	// By hand: the contract part holds `maxLength: 200`; the rules part holds R-4's `200`; the
	// code holds `> 200` and the tests `= 200`.
	for _, part := range []string{"slice/contract", "slice/rules", "internal/app/app.go", "tests/features_test.go"} {
		if !t3HasAnyIn(p.Places, part) {
			t.Errorf("no place of 200 in %s", part)
		}
	}
	for _, q := range got {
		if q.Kind == captured.KindNumber && q.Value != "200" {
			t.Errorf("number candidate %q besides the declared 200", q.Value)
		}
		if q.Value == "201" || q.Value == "400" {
			t.Errorf("status code %s proposed", q.Value)
		}
	}
	// note and notes are still candidates on this fixture.
	if t3Find(got, "note") == nil || t3Find(got, "notes") == nil {
		t.Error("note and notes are not candidates on the limit fixture")
	}
}

// T003: a number is matched only as a whole number, by hand: in `2001 1.200 200ms v200 -200
// "200" 200. 200`, 200 occurs at columns 24 (`-200`), 29 (`"200"`), 34 (`200.`) and 39; in the
// next line `200.5 _200 200_ a200 200a 1200` nowhere; and as the last three bytes of the file,
// with no line feed after, at line 3 column 1.
func TestProposeMatchesWholeNumbersOnly(t *testing.T) {
	f := t3Load(t, "limit")
	tpl := t3WithFile(f.template, "code", t3Numbers)
	got, err := captured.Propose(f.src, tpl)
	if err != nil {
		t.Fatal(err)
	}
	p := t3Find(got, "200")
	if p == nil {
		t.Fatal("the declared value 200 is not a candidate")
	}
	var inFile []captured.Place
	for _, pl := range p.Places {
		if pl.Part == t3Numbers.Path {
			inFile = append(inFile, pl)
		}
	}
	mk := func(line, col int) captured.Place {
		return captured.Place{Part: t3Numbers.Path, In: captured.InContent, Line: line, Column: col, Form: captured.FormExact}
	}
	t3SamePlaces(t, "docs/numbers.txt", inFile, []captured.Place{mk(1, 24), mk(1, 29), mk(1, 34), mk(1, 39), mk(3, 1)})
}

// T003: the proposal is the same each time (one answer per slice).
func TestProposeIsRepeatable(t *testing.T) {
	f := t3Load(t, "limit")
	a, err := captured.Propose(f.src, f.template)
	if err != nil {
		t.Fatal(err)
	}
	b, err := captured.Propose(f.src, f.template)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) == 0 || !reflect.DeepEqual(a, b) {
		t.Errorf("two proposals differ or are empty (%d, %d candidates)", len(a), len(b))
	}
}

// T003, FR-003, research R4: a template whose outputs are empty (here `witnesses`, and all of
// them) still gives the slice's own places; the candidates are the slice's.
func TestProposeWithEmptyTemplateOutputs(t *testing.T) {
	f := t3Load(t, "notes")
	got, err := captured.Propose(f.src, captured.Template{"tests": nil, "code": nil, "witnesses": nil})
	if err != nil {
		t.Fatal(err)
	}
	p := t3Find(got, "notes")
	if p == nil {
		t.Fatal("no candidate notes")
	}
	want := t3Places(f.src, captured.Template{}, []t3Spec{t3Notes, t3Note})[0]
	t3SamePlaces(t, "notes", p.Places, want)
}

// ---------------------------------------------------------------------------------------------
// Check

// refused asserts err is a *Refused that names every one of names.
func t3Refused(t *testing.T, what string, err error, names ...string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: accepted, want a *Refused naming %v", what, names)
		return
	}
	t3NotStub(t, err)
	var r *captured.Refused
	if !errors.As(err, &r) {
		t.Errorf("%s: %T %q, want a *Refused", what, err, err)
		return
	}
	if r.Detail == "" {
		t.Errorf("%s: the refusal gives no detail", what)
	}
	for _, n := range names {
		if !strings.Contains(err.Error(), n) {
			t.Errorf("%s: %q does not name %q", what, err, n)
		}
	}
}

// T003: the clean fixture passes (a check proves it can pass): note and notes with every place on
// the notes fixture, and the declared 200 and the added 201 on the limit fixture.
func TestCheckAcceptsCleanParameters(t *testing.T) {
	f := t3Load(t, "notes")
	if err := captured.Check(f.src, f.template, t3Params(f.src, f.template, []t3Spec{t3Notes, t3Note})); err != nil {
		t.Errorf("notes: %v", err)
	}
	if err := captured.Check(f.src, f.template, nil); err != nil {
		t.Errorf("no parameters: %v", err)
	}
	l := t3Load(t, "limit")
	specs := []t3Spec{t3Notes, t3Note, {"limit", captured.KindNumber, "200"}, {"over", captured.KindNumber, "201"}}
	if err := captured.Check(l.src, l.template, t3Params(l.src, l.template, specs)); err != nil {
		t.Errorf("limit: %v", err)
	}
}

// t3Tokens is the fixture's copy with `# tokens: ...` added to its contract, so every token occurs
// in the slice and a refusal is about the value's form, count or equality, not its absence.
func t3Tokens(t *testing.T, f t3Fixture, tokens ...string) captured.Copy {
	return f.withContract(t, f.contract+"# tokens: "+strings.Join(tokens, " ")+"\n")
}

// T003, FR-001, research R4: two parameters whose values are equal in any case form are refused,
// the refusal naming both.
func TestCheckRefusesEqualValuesInAnyCaseForm(t *testing.T) {
	f := t3Load(t, "notes")
	cases := []struct {
		what string
		a, b t3Spec
	}{
		{"name and string in another case", t3Spec{"kind", captured.KindName, "note"}, t3Spec{"title", captured.KindString, "Note"}},
		{"name and string in upper case", t3Spec{"kind", captured.KindName, "note"}, t3Spec{"title", captured.KindString, "NOTE"}},
		{"name and string, same text", t3Spec{"kind", captured.KindName, "note"}, t3Spec{"title", captured.KindString, "note"}},
		{"two strings in two cases", t3Spec{"kind", captured.KindString, "Note"}, t3Spec{"title", captured.KindString, "nOTE"}},
		{"string and number, same text", t3Spec{"kind", captured.KindString, "200"}, t3Spec{"title", captured.KindNumber, "200"}},
	}
	for _, c := range cases {
		for _, order := range []bool{false, true} {
			a, b := c.a, c.b
			if order {
				a, b = b, a
			}
			src := t3Tokens(t, f, "nOTE", "200")
			ps := t3Params(src, f.template, []t3Spec{a, b})
			t3Refused(t, c.what, captured.Check(src, f.template, ps), a.Name, b.Name)
		}
	}
}

// T003, FR-001: a value outside its kind's form is refused, the refusal naming the parameter.
// A name is [a-z][a-z0-9]* with at least two letters; a number an integer or decimal without a
// sign; a string letters, digits, `_`, `-` and spaces. Every value occurs in the slice, so the form
// is what refuses it.
func TestCheckRefusesValuesOutsideTheirKindsForm(t *testing.T) {
	f := t3Load(t, "notes")
	bad := []t3Spec{
		{"v", captured.KindName, "Note"}, {"v", captured.KindName, "NOTE"}, {"v", captured.KindName, "1note"},
		{"v", captured.KindName, "note-x"}, {"v", captured.KindName, "note_x"}, {"v", captured.KindName, "note x"},
		{"v", captured.KindName, "a1"}, {"v", captured.KindName, "n"}, {"v", captured.KindName, "7"}, {"v", captured.KindName, ""},
		{"v", captured.KindNumber, "+5"}, {"v", captured.KindNumber, "-5"}, {"v", captured.KindNumber, "abc"},
		{"v", captured.KindNumber, "1e3"}, {"v", captured.KindNumber, "1.2.3"}, {"v", captured.KindNumber, "1,5"},
		{"v", captured.KindNumber, "5 5"}, {"v", captured.KindNumber, ""},
		{"v", captured.KindString, "a.b"}, {"v", captured.KindString, "a/b"}, {"v", captured.KindString, "a;b"},
		{"v", captured.KindString, "a\"b"}, {"v", captured.KindString, "a\tb"}, {"v", captured.KindString, "a\nb"},
		{"v", captured.KindString, ""},
	}
	for _, s := range bad {
		s.Name = "vname"
		src := t3Tokens(t, f, s.Value)
		ps := t3Params(src, f.template, []t3Spec{s})
		t3Refused(t, fmt.Sprintf("%s %q", s.Kind, s.Value), captured.Check(src, f.template, ps), "vname")
	}
	good := []t3Spec{
		{"v", captured.KindName, "ab"}, {"v", captured.KindName, "ab12"},
		{"v", captured.KindNumber, "0"}, {"v", captured.KindNumber, "3.14"}, {"v", captured.KindNumber, "200"},
		{"v", captured.KindString, "hello world"}, {"v", captured.KindString, "a_b-c 1"}, {"v", captured.KindString, "Q7"},
	}
	for _, s := range good {
		src := t3Tokens(t, f, s.Value)
		if err := captured.Check(src, f.template, t3Params(src, f.template, []t3Spec{s})); err != nil {
			t.Errorf("%s %q: %v", s.Kind, s.Value, err)
		}
	}
}

// T003, FR-001: a value of more than 64 characters is refused; 64 is accepted.
func TestCheckRefusesValueOver64Characters(t *testing.T) {
	f := t3Load(t, "notes")
	for _, k := range []struct {
		kind captured.Kind
		ch   string
	}{{captured.KindName, "a"}, {captured.KindNumber, "1"}, {captured.KindString, "b"}} {
		v64, v65 := strings.Repeat(k.ch, captured.ValueMaxChars), strings.Repeat(k.ch, captured.ValueMaxChars+1)
		src := t3Tokens(t, f, v64, v65)
		if err := captured.Check(src, f.template, t3Params(src, f.template, []t3Spec{{"long", k.kind, v64}})); err != nil {
			t.Errorf("%s of 64 characters: %v", k.kind, err)
		}
		t3Refused(t, string(k.kind)+" of 65 characters", captured.Check(src, f.template, t3Params(src, f.template, []t3Spec{{"long", k.kind, v65}})), "long")
	}
}

// T003, FR-001: an added value that does not occur in the slice is refused, naming the parameter.
func TestCheckRefusesValueNotInTheSlice(t *testing.T) {
	f := t3Load(t, "notes")
	for _, s := range []t3Spec{
		{"zebra", captured.KindName, "zebra"}, {"zebra", captured.KindNumber, "7777"}, {"zebra", captured.KindString, "no such text"},
		{"zebra", captured.KindNumber, "20"},
	} {
		if n := len(t3Places(f.src, captured.Template{}, []t3Spec{s})[0]); n != 0 {
			t.Fatalf("test bug: %q occurs %d times in the notes slice", s.Value, n)
		}
		t3Refused(t, string(s.Kind)+" "+s.Value, captured.Check(f.src, f.template, t3Params(f.src, f.template, []t3Spec{s})), "zebra")
	}
}

// T003, FR-001, SC-006: a number is matched only as a whole number, so `4242` is not in a slice
// that holds only `42421`, `1.4242`, `4242ms`, `v4242`, `4242.5`, `_4242`, `4242_`, `a4242` and
// `4242a`, and is in `-4242` and `"4242"`. (4242, not 200: the notes contract holds the status
// `"200"` as a whole number.)
func TestCheckNumberOccursOnlyAsWholeNumber(t *testing.T) {
	f := t3Load(t, "notes")
	spec := []t3Spec{{"limit", captured.KindNumber, "4242"}}
	if n := len(t3Places(f.src, captured.Template{}, spec)[0]); n != 0 {
		t.Fatalf("test bug: 4242 occurs %d times in the notes slice", n)
	}
	not := t3Tokens(t, f, "42421", "1.4242", "4242ms", "v4242", "4242.5", "_4242", "4242_", "a4242", "4242a")
	t3Refused(t, "4242 only inside other numbers and words", captured.Check(not, f.template, t3Params(not, f.template, spec)), "limit")
	for _, tok := range []string{"-4242", `"4242"`, "4242.", "(4242)"} {
		src := t3Tokens(t, f, tok)
		if err := captured.Check(src, f.template, t3Params(src, f.template, spec)); err != nil {
			t.Errorf("4242 in %q: %v", tok, err)
		}
	}
}

// T003, FR-001: at most 32 parameters (ParametersMax): 32 pass, 33 are refused.
func TestCheckRefusesMoreThan32Parameters(t *testing.T) {
	f := t3Load(t, "notes")
	var tokens []string
	var specs []t3Spec
	for i := 0; i <= captured.ParametersMax; i++ {
		v := fmt.Sprintf("zq%c%c", 'a'+i/26, 'a'+i%26)
		tokens = append(tokens, v)
		specs = append(specs, t3Spec{fmt.Sprintf("p%d", i), captured.KindName, v})
	}
	src := t3Tokens(t, f, tokens...)
	if err := captured.Check(src, f.template, t3Params(src, f.template, specs[:captured.ParametersMax])); err != nil {
		t.Errorf("32 parameters: %v", err)
	}
	if err := captured.Check(src, f.template, t3Params(src, f.template, specs)); err == nil {
		t.Error("33 parameters accepted")
	} else {
		t3Refused(t, "33 parameters", err)
	}
}

// T003, FR-001: at most 10000 places per parameter (PlacesMax), slice and template together: the
// string `QQ7` with 10000 places passes, with 10001 it is refused, naming the parameter.
func TestCheckRefusesMoreThan10000Places(t *testing.T) {
	f := t3Load(t, "notes")
	src := t3Tokens(t, f, "QQ7") // one place, in the contract
	spec := []t3Spec{{"many", captured.KindString, "QQ7"}}
	for _, c := range []struct {
		inFile int
		ok     bool
	}{{captured.PlacesMax - 1, true}, {captured.PlacesMax, false}} {
		tpl := t3WithFile(f.template, "code", canonical.File{Path: "internal/app/many.txt", Content: []byte(strings.Repeat("QQ7\n", c.inFile))})
		ps := t3Params(src, tpl, spec)
		if n := len(ps[0].Places); n != c.inFile+1 {
			t.Fatalf("test bug: %d places, want %d", n, c.inFile+1)
		}
		err := captured.Check(src, tpl, ps)
		if c.ok && err != nil {
			t.Errorf("%d places: %v", len(ps[0].Places), err)
		}
		if !c.ok {
			t3Refused(t, fmt.Sprintf("%d places", len(ps[0].Places)), err, "many")
		}
	}
}
