package captured_test

// Tests of captured.Fit's order of refusals (feature 011, task T051, review B1 of T017): a value
// refused together with a later difference is not "only because of a refused value" (FR-010, the
// engine contract's `not_used` reason `refused-value`: "it would fit but for a value outside its
// parameter's form, or equal to another parameter's in a case form"). FR-008: a slice that does
// not fit is told the first place it differs or the value refused; Fit's doc comment: a refusal is
// the first that applies, in reading order, a difference (Place) or a value refused (Parameter).
// A value refused is therefore the refusal only when nothing in the slice differs from the
// captured pattern's source apart from the values: a difference LATER in the slice than the bad
// value is still a difference, and Fit tells it as a Place, not as a Parameter.
//
// Written from Fit's and Refusal's doc comments and the spec, never from an implementation. They
// reuse fit_test.go's fixtures and helpers (t12Make, t12Replacer, t12Names, t12Once, t12Refuses,
// t12DiffersAt) without changing them.
//
// How the expected place is computed, by hand: a part of the slice is one line of canonical JSON
// (line 1); the parts are read goals, rules, contract, and the rules part holds the feature text
// (`examples`) before the rules array (canonical key order). A planted change lies in one part,
// and the change is the first difference of the slice from the filled source (the bad values are
// the values read, so the text that holds them is not a difference). The expected place is
// therefore the part the edit is in, in content, line 1, and a column from 1 to the first column
// at which the planted part differs from the same slice without the edit (t12DiffersAt bounds
// the column the way fit_test.go does: the contract leaves open whether the place is the
// differing text or the hole before it).
//
// Where the first place of each parameter lies (by hand, from testdata/notes): `notes` first in
// the goals ("Keep short notes ..."); `note` first in the rules part, the feature text's "Rule:
// R-1 Listing returns every note in creation order". So with `note` bad the value is read in
// slice/rules; with `notes` bad it is read in slice/goals; with both equal it is refused at the
// `note` place (equal to `notes` read in the goals).

import (
	"fmt"
	"strings"
	"testing"
)

// t51Bad is a pair of values with a refused one, the parameter(s) the value refusal names, and
// where the value is read (the part of the first refused place).
type t51Bad struct {
	what        string
	note, notes string
	names       []string // the first is the parameter the refusal names; the others are in its text
	firstPart   string
}

func t51Bads() []t51Bad {
	return []t51Bad{
		{"a name outside its form (a space)", "to do", "tasks", []string{"note"}, "slice/rules"},
		{"a number where a name was", "42", "tasks", []string{"note"}, "slice/rules"},
		{"two parameters given equal values", "item", "item", []string{"note", "notes"}, "slice/rules"},
		{"the plural outside its form (a space)", "task", "to do", []string{"notes"}, "slice/goals"},
		{"the plural a number", "task", "42", []string{"notes"}, "slice/goals"},
		{"both outside their form", "42", "to do", []string{"notes"}, "slice/goals"},
	}
}

// t51Edit is a planted change to a slice whose values are the bad ones, and the part it lies in.
type t51Edit struct {
	name string
	part string
	edit func(t testing.TB, b t51Bad, s t12Slice) t12Slice
}

func t51Rules(name, old, repl string) t51Edit {
	return t51Edit{name, "slice/rules", func(t testing.TB, _ t51Bad, s t12Slice) t12Slice {
		s.rules = t12Once(t, s.rules, old, repl)
		return s
	}}
}

func t51Feature(name, old, repl string) t51Edit {
	return t51Edit{name, "slice/rules", func(t testing.TB, _ t51Bad, s t12Slice) t12Slice {
		s.feature = t12Once(t, s.feature, old, repl)
		return s
	}}
}

func t51Contract(name, old, repl string) t51Edit {
	return t51Edit{name, "slice/contract", func(t testing.TB, _ t51Bad, s t12Slice) t12Slice {
		s.contract = t12Once(t, s.contract, old, repl)
		return s
	}}
}

func t51Goals(name, old, repl string) t51Edit {
	return t51Edit{name, "slice/goals", func(t testing.TB, _ t51Bad, s t12Slice) t12Slice {
		s.goals = t12Once(t, s.goals, old, repl)
		return s
	}}
}

// t51Later are differences in the rules part (after the feature text's first `note`) and in the
// contract, none of which holds a case form of a parameter, so they read the same whatever the
// values; the same kinds as fit_test.go's planted set (another rule, another example, another
// member, another operation, another status).
func t51Later() []t51Edit {
	return []t51Edit{
		t51Feature("another example (status in a later step)", "Then the response status is 201", "Then the response status is 200"),
		{"another example (added scenario)", "slice/rules", func(t testing.TB, _ t51Bad, s t12Slice) t12Slice {
			s.feature += "\n    @R-3 @E-5\n    Scenario: Blank text is refused\n      When the user creates a thing with the text \" \"\n      Then the response status is 400\n"
			return s
		}},
		t51Rules("another rule (changed response)", "the first being 1", "the first being 0"),
		t51Rules("another rule (changed status)", "with a 400", "with a 422"),
		{"another rule (added)", "slice/rules", func(t testing.TB, _ t51Bad, s t12Slice) t12Slice {
			s.rules = t12Once(t, s.rules, `"terms": []}`+"\n]", `"terms": []},`+"\n"+
				`  {"id": "R-4", "goal": "G-1", "pattern": "ubiquitous", "system": "the service", "response": "shall count", "terms": []}`+"\n]")
			return s
		}},
		t51Contract("another status", `"400":`, `"422":`),
		t51Contract("another member (renamed)", "required: [id, text]", "required: [id, body]"),
		t51Contract("another member (added)", "    Error:\n", "        done:\n          type: boolean\n    Error:\n"),
		t51Contract("another operation", "components:\n", "    delete:\n      operationId: removeIt\n      responses:\n        \"204\":\n          description: Gone.\ncomponents:\n"),
	}
}

// t51Slices builds the slice with the bad values and the same slice with the edit applied.
func t51Slices(t testing.TB, p t12Pattern, b t51Bad, e t51Edit) (clean, planted t12Slice) {
	t.Helper()
	clean = p.slc.apply(t12Replacer(t12Names(b.note, b.notes)))
	return clean, e.edit(t, b, clean)
}

// T051, FR-008, FR-010, Fit's doc ("a refusal is the first that applies, in reading order"): each
// of the bad values combined with a later difference is refused by that difference: a Place in
// the part of the difference and no value refusal, so the captured pattern is not named
// `refused-value` for it. Seen failing before the fix: the value refusal is returned as soon as
// the value is read, whatever follows it.
func TestFitRefusedValueWithALaterDifferenceIsAPlaceRefusal(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	later := t51Later()
	for _, b := range t51Bads() {
		for _, e := range later {
			t.Run(b.what+"/"+e.name, func(t *testing.T) {
				clean, planted := t51Slices(t, p, b, e)
				_, ok, ref := p.fit(planted.copy(t))
				t12DiffersAt(t, b.what+" and "+e.name, ok, ref, clean.copy(t), planted.copy(t), e.part)
			})
		}
	}
}

// T051, FR-010: a bad value read first in the goals (the plural, the pair) and a difference later
// in the same part (right after the value, or at the end), and a difference in the goals before
// the value: all are differences, a Place in slice/goals.
func TestFitRefusedValueWithADifferenceInTheSameGoalsPart(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	for _, b := range t51Bads() {
		if b.firstPart != "slice/goals" {
			continue
		}
		for _, e := range []t51Edit{
			t51Goals("later in the goals (after the value)", "in the order they were created", "in any order"),
			t51Goals("the end of the goals text", "they were created.", "they were made."),
			t51Goals("before the value", "Keep short", "Keep tiny"),
		} {
			t.Run(b.what+"/"+e.name, func(t *testing.T) {
				clean, planted := t51Slices(t, p, b, e)
				_, ok, ref := p.fit(planted.copy(t))
				t12DiffersAt(t, b.what+" and "+e.name, ok, ref, clean.copy(t), planted.copy(t), e.part)
			})
		}
	}
}

// T051, FR-008: a difference that comes BEFORE the bad value is a Place refusal (regression
// guard: it holds before the fix). The goals precede the rules part where `note` is first read;
// within the rules part the feature text's rule title precedes the "every note" place.
func TestFitDifferenceBeforeTheRefusedValueIsAPlaceRefusal(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	for _, b := range t51Bads() {
		if b.firstPart != "slice/rules" {
			continue
		}
		for _, e := range []t51Edit{
			t51Goals("another goal (an earlier part)", "Keep short", "Keep tiny"),
			t51Feature("the rule title before the value", "Listing returns", "Showing returns"),
		} {
			t.Run(b.what+"/"+e.name, func(t *testing.T) {
				clean, planted := t51Slices(t, p, b, e)
				_, ok, ref := p.fit(planted.copy(t))
				t12DiffersAt(t, b.what+" and "+e.name, ok, ref, clean.copy(t), planted.copy(t), e.part)
			})
		}
	}
}

// T051, FR-008, FR-009: the first place of a parameter holds the bad value and a later place of
// the same parameter holds another value. The later place differs from the filled source (the
// value read), so it is a Place refusal in the part of the later place, not the value refusal.
// For `note` the later place is the rules array's "creates a <value>"; for `notes` the feature
// text's "Feature: <Value>" (the capitalised form).
func TestFitBadValueAtTheFirstPlaceAndAnotherAtALaterPlaceIsAPlaceRefusal(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	for _, b := range t51Bads() {
		var e t51Edit
		if b.names[0] == "note" {
			v := b.note
			e = t51Rules("a later place of note holds another value", `"trigger": "the user creates a `+v+`"`, `"trigger": "the user creates a `+v+` x"`)
		} else {
			v := t12Cap(b.notes)
			e = t51Feature("a later place of notes holds another value", "Feature: "+v+"\n", "Feature: "+v+" x\n")
		}
		t.Run(b.what+"/"+e.name, func(t *testing.T) {
			clean, planted := t51Slices(t, p, b, e)
			_, ok, ref := p.fit(planted.copy(t))
			t12DiffersAt(t, b.what+" and "+e.name, ok, ref, clean.copy(t), planted.copy(t), e.part)
		})
	}
}

// T051, FR-008, FR-009 (regression guard, holds before the fix): a good value at the first place
// and a bad one at a later place is a difference from the filled source, a Place in the part of
// the later place, not a value refusal.
func TestFitGoodValueThenABadOneAtALaterPlaceIsAPlaceRefusal(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	clean := p.slc.apply(t12Replacer(t12Names("task", "tasks")))
	for _, bad := range []string{"to do", "42"} {
		planted := clean
		planted.rules = t12Once(t, planted.rules, `"trigger": "the user creates a task"`, `"trigger": "the user creates a `+bad+`"`)
		_, ok, ref := p.fit(planted.copy(t))
		t12DiffersAt(t, "later place "+bad, ok, ref, clean.copy(t), planted.copy(t), "slice/rules")
	}
}

// T051, FR-009, FR-010, scenario 5 of User Story 2 (regression guard, holds before the fix): with
// the value refusal alone, the rest of the slice equal to the source but for the parameters, Fit
// still refuses the value, naming the parameter, and names no place.
func TestFitRefusedValueAloneIsStillTheValueRefusal(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	for _, b := range t51Bads() {
		approved := p.slc.apply(t12Replacer(t12Names(b.note, b.notes))).copy(t)
		_, ok, ref := p.fit(approved)
		t12Refuses(t, fmt.Sprintf("%s (note=%q, notes=%q)", b.what, b.note, b.notes), ok, ref, b.names[0], b.names[1:]...)
	}
}

// T051, FR-010: differences in two parts at once with a bad value: the first difference in
// reading order is told, the rules part's before the contract's.
func TestFitRefusedValueWithDifferencesInTwoPartsNamesTheFirst(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	b := t51Bads()[0]
	clean := p.slc.apply(t12Replacer(t12Names(b.note, b.notes)))
	planted := clean
	planted.rules = t12Once(t, planted.rules, "with a 400", "with a 422")
	planted.contract = t12Once(t, planted.contract, `"400":`, `"422":`)
	_, ok, ref := p.fit(planted.copy(t))
	t12DiffersAt(t, "rules and contract", ok, ref, clean.copy(t), planted.copy(t), "slice/rules")
	if ok || ref.Place == nil || !strings.HasPrefix(ref.Place.Part, "slice/rules") {
		t.Errorf("the first difference in reading order is in the rules part; got %s", t12Describe(ref))
	}
}
