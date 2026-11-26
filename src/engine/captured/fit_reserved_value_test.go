package captured_test

// T062 (feature 011, US2; T061 option A, owner decision 2026-11-27; spec Clarification "Q (converge
// US2, T061)", FR-008, FR-010, User Story 2 scenario 5; research R4): the Go keywords are
// reserved. A later slice whose `name` value is one of the 25 keywords does not fit: Fit refuses
// the value with a Parameter refusal naming the parameter and the word. Only where Fit reads a
// value: Propose still offers a keyword that is a source value. A longer word (`types`) is not
// reserved; a capitalised or upper-case word is not a name value at all (outside the form, refused
// by form as before), so it appears here only as a case form of a fitting value in the files.
// FR-008's order holds: a place difference anywhere in the slice wins over the value refusal.
// A later value equal to its parameter's own source value is not refused as a keyword
// (Clarification "Q (T062)", owner 2026-11-27).
//
// Expected values are computed by hand: the notes fixture with `note` (and Note, NOTE) replaced by
// the word and `notes` by `tasks`, so the rest of the slice equals the source and the only
// refusal is the value's. It reuses fit_test.go's helpers without changing them.

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/captured"
)

// t62Keywords are the 25 keywords of the Go specification.
var t62Keywords = []string{
	"break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough", "for",
	"func", "go", "goto", "if", "import", "interface", "map", "package", "range", "return", "select",
	"struct", "switch", "type", "var",
}

// t62Names asserts that the refusal's detail names the word as a whole word (not inside another).
func t62Names(t testing.TB, what string, ref captured.Refusal, word string) {
	t.Helper()
	if ref.Parameter == nil {
		return
	}
	re := regexp.MustCompile(`(^|[^A-Za-z0-9])` + regexp.QuoteMeta(word) + `([^A-Za-z0-9]|$)`)
	if !re.MatchString(ref.Parameter.Detail) {
		t.Errorf("%s: the detail %q does not name the word %q", what, ref.Parameter.Detail, word)
	}
}

// T062, T061 option A, FR-010, scenario 5: the plural `notes` (first read in the goals) holding a
// keyword, `note` a fitting word: a Parameter refusal naming `notes` and the word, no place.
// The singular `note` (first read in the rules part) holding a keyword: names `note`. All 25 rows
// for each; `go` and `if` pass the two-letter minimum, so the keyword is their only refusal.
func TestFitGoKeywordAsNameValueDoesNotFit(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	for _, w := range t62Keywords {
		approved := p.slc.apply(t12Replacer(t12Names("task", w))).copy(t)
		_, ok, ref := p.fit(approved)
		what := fmt.Sprintf("notes=%q", w)
		t12Refuses(t, what, ok, ref, "notes")
		t62Names(t, what, ref, w)

		approved = p.slc.apply(t12Replacer(t12Names(w, "tasks"))).copy(t)
		_, ok, ref = p.fit(approved)
		what = fmt.Sprintf("note=%q", w)
		t12Refuses(t, what, ok, ref, "note")
		t62Names(t, what, ref, w)
	}
}

// T062, FR-008 order (place, value, paths): a keyword value plus a place difference is the Place
// refusal in the part of the difference, never the value refusal: a difference in the goals, the
// rules and the contract; both parameters.
func TestFitGoKeywordWithADifferenceIsAPlaceRefusal(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	for _, w := range []string{"func", "type", "go"} {
		for _, names := range [][]string{t12Names("task", w), t12Names(w, "tasks")} {
			clean := p.slc.apply(t12Replacer(names))
			for _, e := range []struct {
				name, part string
				edit       func(s t12Slice) t12Slice
			}{
				{"changed status in the rules", "slice/rules", func(s t12Slice) t12Slice {
					s.rules = t12Once(t, s.rules, "with a 400", "with a 422")
					return s
				}},
				{"another status in the contract", "slice/contract", func(s t12Slice) t12Slice {
					s.contract = t12Once(t, s.contract, `"400":`, `"422":`)
					return s
				}},
				{"later in the goals", "slice/goals", func(s t12Slice) t12Slice {
					s.goals = t12Once(t, s.goals, "in the order they were created", "in any order")
					return s
				}},
			} {
				planted := e.edit(clean)
				_, ok, ref := p.fit(planted.copy(t))
				t12DiffersAt(t, fmt.Sprintf("%v and %s", names[:2], e.name), ok, ref, clean.copy(t), planted.copy(t), e.part)
			}
		}
	}
}

// T062, FR-008, FR-009: a keyword at the first place of a parameter and another value at a later
// place of the same parameter: the later place differs from the value read, so it is a Place
// refusal, not the keyword's value refusal.
func TestFitGoKeywordAtFirstPlaceAndAnotherLaterIsAPlaceRefusal(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	clean := p.slc.apply(t12Replacer(t12Names("func", "tasks")))
	planted := clean
	planted.rules = t12Once(t, planted.rules, `"trigger": "the user creates a func"`, `"trigger": "the user creates a func x"`)
	_, ok, ref := p.fit(planted.copy(t))
	t12DiffersAt(t, "later place of note", ok, ref, clean.copy(t), planted.copy(t), "slice/rules")
}

// T062, FR-010: two parameters both holding keywords: not fit, a value refusal. Fit's doc: the
// first value refused in reading order is told; `notes` is read in the goals before `note` is read
// in the rules part, so it names `notes` and its word.
func TestFitTwoGoKeywordValuesNameTheFirstRead(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	approved := p.slc.apply(t12Replacer(t12Names("func", "type"))).copy(t)
	_, ok, ref := p.fit(approved)
	t12Refuses(t, "note=func notes=type", ok, ref, "notes")
	t62Names(t, "note=func notes=type", ref, "type")
}

// T062, FR-010: a keyword that is also equal to the other parameter's value: still one value
// refusal (Parameter, no place) naming a parameter; the spec does not say which reason wins.
func TestFitGoKeywordEqualToTheOtherValueIsAValueRefusal(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	approved := p.slc.apply(t12Replacer(t12Names("func", "func"))).copy(t)
	_, ok, ref := p.fit(approved)
	if ok {
		t.Fatal("note=notes=func fits")
	}
	t12Stub(t, ref)
	if ref.Place != nil || ref.Parameter == nil || ref.Parameter.Detail == "" ||
		(ref.Parameter.Parameter != "note" && ref.Parameter.Parameter != "notes") {
		t.Errorf("want a value refusal naming note or notes, got %s", t12Describe(ref))
	}
}

// T062, T061 option A (guards, hold before the fix): longer words and words that merely contain a
// keyword are not reserved, so they fit with their values.
func TestFitWordsAroundGoKeywordsStillFit(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	for _, w := range []string{"types", "typed", "gofer", "funcs", "iff", "forx", "mapping", "ifs", "go2", "var1"} {
		approved := p.slc.apply(t12Replacer(t12Names(w, "tasks"))).copy(t)
		values, ok, ref := p.fit(approved)
		t12Fits(t, "note="+w, values, ok, ref, map[string]string{"note": w, "notes": "tasks"})

		approved = p.slc.apply(t12Replacer(t12Names("task", w))).copy(t)
		values, ok, ref = p.fit(approved)
		t12Fits(t, "notes="+w, values, ok, ref, map[string]string{"note": "task", "notes": w})
	}
}

// T062, T061 option A (guard, holds before the fix): Propose does not refuse a keyword. A source
// slice whose name is `type` (notes -> tasks, note -> type) still has `type` offered.
func TestProposeStillOffersAGoKeywordSourceValue(t *testing.T) {
	f := t3Load(t, "notes")
	s := t12Read(t, "notes").apply(t12Replacer(t12Names("type", "tasks")))
	ps, err := captured.Propose(s.copy(t), f.template)
	if err != nil {
		t.Fatalf("Propose refused a source slice with a keyword name: %v", err)
	}
	if t3Find(ps, "type") == nil {
		t.Errorf("Propose offers no parameter with the value type: %v", ps)
	}
}

// t62SourcePattern is the notes pattern with its source names replaced: note by note, notes by
// notes, in the slice and in the template's paths and contents alike.
func t62SourcePattern(t testing.TB, note, notes string) t12Pattern {
	t.Helper()
	r := t12Replacer(t12Names(note, notes))
	f := t3Load(t, "notes")
	slc := t12Read(t, "notes").apply(r)
	tpl := captured.Template{}
	for out, files := range f.template {
		for _, fl := range files {
			tpl[out] = append(tpl[out], canonical.File{Path: r.Replace(fl.Path), Content: []byte(r.Replace(string(fl.Content)))})
		}
	}
	src := slc.copy(t)
	specs := []t3Spec{{"note", captured.KindName, note}, {"notes", captured.KindName, notes}}
	return t12Pattern{src: src, tpl: tpl, ps: t3Params(src, tpl, specs), slc: slc, spec: specs}
}

// T062, Clarification "Q (T062)" (owner, 2026-11-27), FR-008: a captured pattern whose source
// value is a Go keyword (`select`, offered by Propose) fits a later slice that keeps that value;
// another keyword value is still refused, also one that is the other parameter's source value.
// `select` occurs nowhere in the notes fixture, so the replaced source has no other place of it.
func TestFitGoKeywordSourceValueKeptStillFits(t *testing.T) {
	p := t62SourcePattern(t, "select", "selects")

	values, ok, ref := p.fit(p.slc.copy(t))
	t12Fits(t, "the source slice", values, ok, ref, map[string]string{"note": "select", "notes": "selects"})

	keep := t12Read(t, "notes").apply(t12Replacer(t12Names("select", "tasks"))).copy(t)
	values, ok, ref = p.fit(keep)
	t12Fits(t, "note=select kept", values, ok, ref, map[string]string{"note": "select", "notes": "tasks"})

	other := t12Read(t, "notes").apply(t12Replacer(t12Names("goto", "selects"))).copy(t)
	_, ok, ref = p.fit(other)
	t12Refuses(t, "note=goto", ok, ref, "note")
	t62Names(t, "note=goto", ref, "goto")

	swapped := t12Read(t, "notes").apply(t12Replacer(t12Names("task", "select"))).copy(t)
	_, ok, ref = p.fit(swapped)
	t12Refuses(t, "notes=select (note's source value)", ok, ref, "notes")
	t62Names(t, "notes=select", ref, "select")
}
