package captured_test

// Tests of captured.Fit (feature 011, task T012: spec User Story 2 scenarios 1, 2 and 5, FR-008 to
// FR-010, SC-005, SC-006, Edge Cases; research R4 "Fit" and "Paths"; contract draft
// contracts/captured-patterns.md "Go interface"). They are written from `Fit`'s and `Refusal`'s
// doc comments in captured.go, never from an implementation.
//
// Parameters are built from the independent scanner of propose_test.go (t3Params), not from
// Propose. A later slice is built from the fixture's own files by text replacement (strings
// replacers, whose left-to-right rule is the contract's "longest first"), so the expected values
// and the expected filled files are computed by hand from the fixture, never from Fit or Fill.
//
// What a refusal's Place pins: Part and In are exact; Line is 1 (a slice part is one line of
// canonical JSON, the convention propose_test.go pins for Propose); Column is only bounded, from 1
// up to the first column at which the approved part differs from the filled source (the contract
// leaves open whether the place is the differing text or the hole that holds it; the report names
// this). Which of two differing parts comes first is not pinned: every planted case differs in
// one part.

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/captured"
)

// ---------------------------------------------------------------------------------------------
// Helpers

// t12Writable is the writable entries of the base pattern's steps (t3Load: the `tests` output is
// `tests/**`; the `code` output is `cmd/**` and `internal/**`), in the dialect `dir/**` or one
// exact path.
var t12Writable = map[string][]string{
	"tests":     {"tests/**"},
	"code":      {"cmd/**", "internal/**"},
	"witnesses": {"tests/**"},
}

var (
	t12Limit = t3Spec{"limit", captured.KindNumber, "280"}
	t12Over  = t3Spec{"over", captured.KindNumber, "281"}
)

// t12Slice is the text of a slice's four source files.
type t12Slice struct{ goals, rules, feature, contract string }

func t12Read(t testing.TB, name string) t12Slice {
	t.Helper()
	return t12Slice{
		goals:    t3Read(t, "testdata", name, "slice", "goals.json"),
		rules:    t3Read(t, "testdata", name, "slice", "rules.json"),
		feature:  t3Read(t, "testdata", name, "slice", "slice.feature"),
		contract: t3Read(t, "testdata", name, "slice", "contract.yaml"),
	}
}

func (s t12Slice) copy(t testing.TB) captured.Copy {
	t.Helper()
	return t3Copy(t, s.goals, s.rules, s.feature, s.contract)
}

func (s t12Slice) apply(r *strings.Replacer) t12Slice {
	return t12Slice{r.Replace(s.goals), r.Replace(s.rules), r.Replace(s.feature), r.Replace(s.contract)}
}

func t12Cap(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

// t12Names replaces the three case forms of notes, then of note (the longer first).
func t12Names(note, notes string) []string {
	return []string{
		"notes", notes, "Notes", t12Cap(notes), "NOTES", strings.ToUpper(notes),
		"note", note, "Note", t12Cap(note), "NOTE", strings.ToUpper(note),
	}
}

func t12Replacer(pairs ...[]string) *strings.Replacer {
	var all []string
	for _, p := range pairs {
		all = append(all, p...)
	}
	return strings.NewReplacer(all...)
}

// t12Once replaces the one occurrence of old in s with repl and fails when old is not there once,
// so a planted change cannot silently miss.
func t12Once(t testing.TB, s, old, repl string) string {
	t.Helper()
	if n := strings.Count(s, old); n != 1 {
		t.Fatalf("test bug: %q occurs %d times in the text, want 1", old, n)
	}
	return strings.Replace(s, old, repl, 1)
}

func t12Parts(c captured.Copy) [3]struct {
	name string
	text []byte
} {
	return [3]struct {
		name string
		text []byte
	}{{"slice/goals", c.Goals}, {"slice/rules", c.Rules}, {"slice/contract", c.Contract}}
}

// t12Diff is the first part (goals, rules, contract) in which a and b differ, and the 1-based
// column (in characters) of the first difference; "" when they are equal.
func t12Diff(a, b captured.Copy) (part string, col int) {
	pa, pb := t12Parts(a), t12Parts(b)
	for i := range pa {
		if bytes.Equal(pa[i].text, pb[i].text) {
			continue
		}
		n := 0
		for n < len(pa[i].text) && n < len(pb[i].text) && pa[i].text[n] == pb[i].text[n] {
			n++
		}
		return pa[i].name, utf8.RuneCount(pa[i].text[:n]) + 1
	}
	return "", 0
}

func t12Stub(t testing.TB, ref captured.Refusal) {
	t.Helper()
	if ref.Detail == captured.ErrNotImplemented.Error() {
		t.Fatalf("got the stub's answer %q, want the interface's own", ref.Detail)
	}
}

// t12Fits asserts a fit with exactly the wanted values and no refusal.
func t12Fits(t testing.TB, what string, values map[string]string, ok bool, ref captured.Refusal, want map[string]string) {
	t.Helper()
	if !ok {
		t12Stub(t, ref)
		t.Errorf("%s: does not fit (%s), want a fit with %v", what, t12Describe(ref), want)
		return
	}
	if len(values) != len(want) || (len(want) > 0 && !reflect.DeepEqual(values, want)) {
		t.Errorf("%s: values %v, want %v", what, values, want)
	}
	if ref.Place != nil || ref.Parameter != nil || len(ref.Paths) != 0 {
		t.Errorf("%s: a fit carries a refusal: %s", what, t12Describe(ref))
	}
}

func t12Describe(ref captured.Refusal) string {
	var parts []string
	if ref.Place != nil {
		parts = append(parts, fmt.Sprintf("place %+v", *ref.Place))
	}
	if ref.Parameter != nil {
		parts = append(parts, fmt.Sprintf("parameter %q: %s", ref.Parameter.Parameter, ref.Parameter.Detail))
	}
	if len(ref.Paths) > 0 {
		parts = append(parts, fmt.Sprintf("paths %v", ref.Paths))
	}
	return strings.Join(append(parts, "detail "+ref.Detail), "; ")
}

// t12DiffersAt asserts a refusal by a difference: Place set, Parameter unset, in the part wantPart
// (and equal to the part clean and planted first differ in), content, line 1, a column from 1 to
// the first differing column.
func t12DiffersAt(t testing.TB, what string, ok bool, ref captured.Refusal, clean, planted captured.Copy, wantPart string) {
	t.Helper()
	if ok {
		t.Errorf("%s: fits, want a refusal at %s", what, wantPart)
		return
	}
	t12Stub(t, ref)
	part, col := t12Diff(clean, planted)
	if part != wantPart {
		t.Fatalf("test bug: the planted change is in %s, not %s", part, wantPart)
	}
	if ref.Place == nil {
		t.Errorf("%s: no place named: %s", what, t12Describe(ref))
		return
	}
	if ref.Parameter != nil {
		t.Errorf("%s: a parameter is named for a difference: %s", what, t12Describe(ref))
	}
	p := ref.Place
	if p.Part != wantPart || p.In != captured.InContent {
		t.Errorf("%s: place %+v, want part %s in content", what, *p, wantPart)
	}
	if p.Line != 1 {
		t.Errorf("%s: place line %d, want 1 (a part is one line of canonical JSON)", what, p.Line)
	}
	if p.Column < 1 || p.Column > col {
		t.Errorf("%s: place column %d, want from 1 to %d (the first difference)", what, p.Column, col)
	}
}

// t12Refuses asserts a refusal of a value: Parameter set and naming wantParam (and every other of
// also in its text), a detail, no place.
func t12Refuses(t testing.TB, what string, ok bool, ref captured.Refusal, wantParam string, also ...string) {
	t.Helper()
	if ok {
		t.Errorf("%s: fits, want a refusal naming %q", what, wantParam)
		return
	}
	t12Stub(t, ref)
	if ref.Parameter == nil {
		t.Errorf("%s: no parameter named: %s", what, t12Describe(ref))
		return
	}
	if ref.Parameter.Detail == "" {
		t.Errorf("%s: the refusal gives no reason", what)
	}
	if ref.Place != nil {
		t.Errorf("%s: a place is named for a value: %s", what, t12Describe(ref))
	}
	text := ref.Parameter.Parameter + " " + ref.Parameter.Detail
	if len(also) == 0 && ref.Parameter.Parameter != wantParam {
		t.Errorf("%s: parameter %q, want %q", what, ref.Parameter.Parameter, wantParam)
	}
	for _, n := range append([]string{wantParam}, also...) {
		if !strings.Contains(text, n) {
			t.Errorf("%s: %q does not name %q", what, text, n)
		}
	}
}

// t12Pattern is a captured pattern over a fixture: its source copy, template and parameters.
type t12Pattern struct {
	src  captured.Copy
	tpl  captured.Template
	ps   []captured.Parameter
	slc  t12Slice
	spec []t3Spec
}

func t12Make(t testing.TB, fixture string, specs ...t3Spec) t12Pattern {
	t.Helper()
	f := t3Load(t, fixture)
	return t12Pattern{src: f.src, tpl: f.template, ps: t3Params(f.src, f.template, specs), slc: t12Read(t, fixture), spec: specs}
}

func (p t12Pattern) fit(approved captured.Copy) (map[string]string, bool, captured.Refusal) {
	return captured.Fit(p.src, p.tpl, t12Writable, p.ps, approved)
}

// ---------------------------------------------------------------------------------------------
// User Story 2 scenario 1: a slice that equals the source once the values are replaced fits

// T012, FR-008, scenario 1: the `task`/`tasks` slice fits the notes pattern, and the fit names
// the value of each parameter. The template holds the inline-forms file too (`note_id`,
// `denote`, `nOTE`), whose words do not change the slice's fit. Expected values: the replacement
// is note to task, notes to tasks, by hand.
func TestFitTaskSliceFitsWithItsValues(t *testing.T) {
	f := t3Load(t, "notes")
	tpl := t3WithFile(f.template, "code", t3Forms)
	specs := []t3Spec{t3Notes, t3Note}
	ps := t3Params(f.src, tpl, specs)
	s := t12Read(t, "notes")
	approved := s.apply(t12Replacer(t12Names("task", "tasks"))).copy(t)
	if bytes.Equal(approved.Rules, f.src.Rules) || bytes.Equal(approved.Contract, f.src.Contract) {
		t.Fatal("test bug: the task slice equals the notes slice")
	}
	want := map[string]string{"note": "task", "notes": "tasks"}
	values, ok, ref := captured.Fit(f.src, tpl, t12Writable, ps, approved)
	t12Fits(t, "task/tasks", values, ok, ref, want)
	// Filling the template with the fit's values gives the expected files, by hand (t3Expect).
	if ok {
		for _, out := range []string{"tests", "code"} {
			got, err := captured.Fill(tpl[out], ps, values)
			if err != nil {
				t.Fatalf("%s: %v", out, err)
			}
			if !reflect.DeepEqual(t3ByPath(t, got), t3Expect(tpl[out], "task", "tasks")) {
				t.Errorf("%s: the fit's values do not fill the template into the expected files", out)
			}
		}
	}
}

// T012, FR-008: the order of the parameters is not part of the interface; the same values come
// back; a second call gives the same answer; the inputs are not changed.
func TestFitIsIndependentOfParameterOrderAndRepeatable(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	approved := p.slc.apply(t12Replacer(t12Names("task", "tasks"))).copy(t)
	want := map[string]string{"note": "task", "notes": "tasks"}
	keepSrc := captured.Copy{Goals: bytes.Clone(p.src.Goals), Rules: bytes.Clone(p.src.Rules), Contract: bytes.Clone(p.src.Contract)}
	keepApproved := captured.Copy{Goals: bytes.Clone(approved.Goals), Rules: bytes.Clone(approved.Rules), Contract: bytes.Clone(approved.Contract)}
	v1, ok, ref := p.fit(approved)
	t12Fits(t, "first call", v1, ok, ref, want)
	v2, ok, ref := p.fit(approved)
	t12Fits(t, "second call", v2, ok, ref, want)
	rev := []captured.Parameter{p.ps[1], p.ps[0]}
	v3, ok, ref := captured.Fit(p.src, p.tpl, t12Writable, rev, approved)
	t12Fits(t, "reversed parameters", v3, ok, ref, want)
	if !reflect.DeepEqual(p.src, keepSrc) || !reflect.DeepEqual(approved, keepApproved) {
		t.Error("Fit changed its source or approved copy")
	}
}

// T012, FR-008 (regression guard for the identity: values equal to the source's): the source
// slice itself fits, with each parameter's source value.
func TestFitSourceSliceFitsWithItsOwnValues(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	values, ok, ref := p.fit(p.src)
	t12Fits(t, "source", values, ok, ref, map[string]string{"note": "note", "notes": "notes"})
}

// T012, FR-008, research R4 ("a value containing a source form is not replaced again"): values
// that contain a parameter's source value, or equal another parameter's source value, fit: the
// fit reads values, it never replaces twice. A swap of the two names fits too.
func TestFitValuesThatContainOrEqualSourceValues(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	for _, c := range []struct{ note, notes string }{
		{"notebook", "notebooks"}, // contains the source value of both
		{"notes", "note"},         // a swap
		{"notes", "notess"},       // one is the other's source value
		{"item", "items"},
	} {
		approved := p.slc.apply(t12Replacer(t12Names(c.note, c.notes))).copy(t)
		values, ok, ref := p.fit(approved)
		t12Fits(t, c.note+"/"+c.notes, values, ok, ref, map[string]string{"note": c.note, "notes": c.notes})
	}
}

// T012, FR-008, research R4 (case forms): the lower, capitalised and upper forms each hold the
// value in their form, and a name has two to 64 characters of lowercase letters and digits with at
// least two letters: `ab` and a 64 character name fit.
func TestFitNameLengthBoundaries(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	for _, name := range []string{"ab", "a2b", strings.Repeat("a", 63) + "b"} {
		plural := "x" + name[1:] // another value of the same length
		approved := p.slc.apply(t12Replacer(t12Names(name, plural))).copy(t)
		values, ok, ref := p.fit(approved)
		t12Fits(t, name, values, ok, ref, map[string]string{"note": name, "notes": plural})
	}
}

// ---------------------------------------------------------------------------------------------
// SC-006: the limit and its derived value

// T012, SC-006, T039: on testdata/limit-280 (limit 280, derived 281, equal to no other literal), a
// slice with 100/101 fits, the values come back, and filling the template with them gives the
// expected code: every 280 and 281 of the source files is 100 and 101, by hand (strings.NewReplacer;
// grep shows 280 and 281 occur in the fixture only as the limit and the derived value, and 100 and
// 101 nowhere; the statuses 200 and 201 stay). With the names changed too, the same.
func TestFitLimit280SliceWith100And101FitsAndFills(t *testing.T) {
	p := t12Make(t, "limit-280", t3Notes, t3Note, t12Limit, t12Over)
	nums := []string{"280", "100", "281", "101"}
	for _, c := range []struct {
		what string
		rep  *strings.Replacer
		want map[string]string
		note [2]string
	}{
		{"limits only", t12Replacer(nums), map[string]string{"note": "note", "notes": "notes", "limit": "100", "over": "101"}, [2]string{"note", "notes"}},
		{"limits and names", t12Replacer(nums, t12Names("task", "tasks")), map[string]string{"note": "task", "notes": "tasks", "limit": "100", "over": "101"}, [2]string{"task", "tasks"}},
	} {
		approved := p.slc.apply(c.rep).copy(t)
		values, ok, ref := p.fit(approved)
		t12Fits(t, c.what, values, ok, ref, c.want)
		if !ok {
			continue
		}
		for _, out := range []string{"tests", "code"} {
			got, err := captured.Fill(p.tpl[out], p.ps, values)
			if err != nil {
				t.Fatalf("%s %s: %v", c.what, out, err)
			}
			want := map[string]string{}
			rep := t12Replacer(nums, t12Names(c.note[0], c.note[1]))
			for _, in := range p.tpl[out] {
				want[rep.Replace(in.Path)] = rep.Replace(string(in.Content))
			}
			if !reflect.DeepEqual(t3ByPath(t, got), want) {
				t.Errorf("%s %s: the filled files are not the source files with 280, 281 replaced by 100, 101", c.what, out)
			}
		}
		app := t3ByPath(t, mustFill(t, p, values, "code"))["internal/app/app.go"]
		if strings.Contains(app, "280") || strings.Contains(app, "281") || !strings.Contains(app, "> 100 {") {
			t.Errorf("%s: app.go does not carry 100 in every place of 280", c.what)
		}
	}
}

func mustFill(t testing.TB, p t12Pattern, values map[string]string, out string) []canonical.File {
	t.Helper()
	got, err := captured.Fill(p.tpl[out], p.ps, values)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// T012, SC-006, FR-008: a slice that equals the source only with each status unchanged: the
// statuses 200 and 201 stay in the slice with another limit (they are not the limit's places), so
// the same fit holds for a limit that equals a status (`200`/`201` as the new values), and for a
// swap of the two, and a decimal; the fit does not relate the two values.
func TestFitLimitValuesAreFreeNumbers(t *testing.T) {
	p := t12Make(t, "limit-280", t3Notes, t3Note, t12Limit, t12Over)
	for _, c := range []struct{ limit, over string }{
		{"200", "201"},   // equal to the statuses of the slice; the statuses stay
		{"281", "280"},   // a swap
		{"281", "282"},   // one is the other's source value
		{"100", "281"},   // only one changed
		{"1.5", "2.5"},   // decimals
		{"0", "1"},       // the smallest
		{"1000", "1001"}, // longer than the source
	} {
		approved := p.slc.apply(t12Replacer([]string{"280", c.limit, "281", c.over})).copy(t)
		// The statuses are never the limit's places: the slice still holds `"200":` and `"201":`.
		if !strings.Contains(p.slc.apply(t12Replacer([]string{"280", c.limit, "281", c.over})).contract, `"200":`) {
			t.Fatal("test bug: the status 200 left the contract")
		}
		values, ok, ref := p.fit(approved)
		t12Fits(t, c.limit+"/"+c.over, values, ok, ref, map[string]string{"note": "note", "notes": "notes", "limit": c.limit, "over": c.over})
	}
}

// ---------------------------------------------------------------------------------------------
// SC-005 and scenario 2: a slice that differs outside a parameter does not fit

// t12Planted is one planted change to the notes slice with the names already replaced by task and
// tasks, and the part it lies in.
type t12Planted struct {
	name string
	part string
	edit func(t testing.TB, s t12Slice) t12Slice
}

func t12PlantedSet() []t12Planted {
	return []t12Planted{
		{"another rule (added)", "slice/rules", func(t testing.TB, s t12Slice) t12Slice {
			s.rules = t12Once(t, s.rules, `"terms": []}`+"\n]", `"terms": []},`+"\n"+
				`  {"id": "R-4", "goal": "G-1", "pattern": "ubiquitous", "system": "the tasks service", "response": "shall count the tasks", "terms": []}`+"\n]")
			return s
		}},
		{"another rule (changed response)", "slice/rules", func(t testing.TB, s t12Slice) t12Slice {
			s.rules = t12Once(t, s.rules, "in creation order", "in reverse order")
			return s
		}},
		{"another rule (removed)", "slice/rules", func(t testing.TB, s t12Slice) t12Slice {
			i := strings.Index(s.rules, `  {"id": "R-3"`)
			if i < 0 {
				t.Fatal("test bug: no R-3")
			}
			s.rules = strings.TrimSuffix(strings.TrimRight(s.rules[:i], ", \n"), ",") + "\n]\n"
			return s
		}},
		{"another goal", "slice/goals", func(t testing.TB, s t12Slice) t12Slice {
			s.goals = t12Once(t, s.goals, "short tasks", "long tasks")
			return s
		}},
		{"another example (added scenario)", "slice/rules", func(t testing.TB, s t12Slice) t12Slice {
			s.feature += "\n    @R-3 @E-5\n    Scenario: Blank text is refused\n      When the user creates a task with the text \" \"\n      Then the response status is 400\n"
			return s
		}},
		{"another example (changed step)", "slice/rules", func(t testing.TB, s t12Slice) t12Slice {
			s.feature = t12Once(t, s.feature, `And the task has the id 1`, `And the task has the id 2`)
			return s
		}},
		{"another example status (in the example)", "slice/rules", func(t testing.TB, s t12Slice) t12Slice {
			s.feature = t12Once(t, s.feature, "Then the response status is 201", "Then the response status is 200")
			return s
		}},
		{"another member", "slice/contract", func(t testing.TB, s t12Slice) t12Slice {
			s.contract = t12Once(t, s.contract, "    NewTask:", "        done:\n          type: boolean\n    NewTask:")
			return s
		}},
		{"another member (renamed)", "slice/contract", func(t testing.TB, s t12Slice) t12Slice {
			s.contract = t12Once(t, s.contract, "required: [id, text]", "required: [id, body]")
			return s
		}},
		{"another operation", "slice/contract", func(t testing.TB, s t12Slice) t12Slice {
			s.contract = t12Once(t, s.contract, "components:\n", "    delete:\n      operationId: deleteTask\n      responses:\n        \"204\":\n          description: Gone.\ncomponents:\n")
			return s
		}},
		{"another operation (renamed)", "slice/contract", func(t testing.TB, s t12Slice) t12Slice {
			s.contract = t12Once(t, s.contract, "operationId: createTask", "operationId: addTask")
			return s
		}},
		{"another status", "slice/contract", func(t testing.TB, s t12Slice) t12Slice {
			s.contract = t12Once(t, s.contract, `"400":`, `"422":`)
			return s
		}},
		{"another status (added response)", "slice/contract", func(t testing.TB, s t12Slice) t12Slice {
			s.contract = t12Once(t, s.contract, "components:\n", "components:\n"+"# 404\n")
			return s
		}},
		{"two holes of one parameter, the second differs", "slice/contract", func(t testing.TB, s t12Slice) t12Slice {
			// `tasks` was the value in the goals and rules; the path holds `items` instead.
			s.contract = t12Once(t, s.contract, "  /tasks:", "  /items:")
			return s
		}},
		{"two holes of one parameter, the rules differ", "slice/rules", func(t testing.TB, s t12Slice) t12Slice {
			s.rules = t12Once(t, s.rules, `"the tasks service", "response": "shall return`, `"the items service", "response": "shall return`)
			return s
		}},
		{"capitalised form left lowercase", "slice/rules", func(t testing.TB, s t12Slice) t12Slice {
			s.feature = t12Once(t, s.feature, "Feature: Tasks", "Feature: tasks")
			return s
		}},
		{"a rule title changed", "slice/rules", func(t testing.TB, s t12Slice) t12Slice {
			s.feature = t12Once(t, s.feature, "Rule: R-1 Listing", "Rule: R-1 ")
			return s
		}},
	}
}

// T012, SC-005, scenario 2, FR-008: a planted set of slices that differ from the source outside a
// parameter does not fit, 0 of N fit, and each names a place in the part where it differs.
func TestFitPlantedDifferencesDoNotFit(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	clean := p.slc.apply(t12Replacer(t12Names("task", "tasks")))
	// The clean slice fits (the check can pass).
	values, ok, ref := p.fit(clean.copy(t))
	t12Fits(t, "clean slice", values, ok, ref, map[string]string{"note": "task", "notes": "tasks"})
	fits := 0
	set := t12PlantedSet()
	for _, c := range set {
		t.Run(c.name, func(t *testing.T) {
			planted := c.edit(t, clean)
			_, ok, ref := p.fit(planted.copy(t))
			if ok {
				fits++
			}
			t12DiffersAt(t, c.name, ok, ref, clean.copy(t), planted.copy(t), c.part)
		})
	}
	if fits != 0 {
		t.Errorf("%d of %d planted slices fit, want 0", fits, len(set))
	}
}

// T012, SC-005, FR-008: the same planted set against the limit pattern: another status of the
// slice (the 400 refusal) and an unrelated number do not fit, while a changed limit does.
func TestFitLimitPatternPlantedDifferences(t *testing.T) {
	p := t12Make(t, "limit-280", t3Notes, t3Note, t12Limit, t12Over)
	clean := p.slc.apply(t12Replacer([]string{"280", "100", "281", "101"}))
	values, ok, ref := p.fit(clean.copy(t))
	t12Fits(t, "clean slice", values, ok, ref, map[string]string{"note": "note", "notes": "notes", "limit": "100", "over": "101"})
	for _, c := range []struct {
		name, part string
		edit       func(s t12Slice) t12Slice
	}{
		{"the status 201 of the example", "slice/rules", func(s t12Slice) t12Slice {
			s.feature = t12Once(t, s.feature, "Then the response status is 201", "Then the response status is 101")
			return s
		}},
		{"the status 200 of the contract", "slice/contract", func(s t12Slice) t12Slice {
			s.contract = t12Once(t, s.contract, `"200":`, `"100":`)
			return s
		}},
		{"a limit left at the source's value in one place", "slice/contract", func(s t12Slice) t12Slice {
			s.contract = t12Once(t, s.contract, "maxLength: 100", "maxLength: 280")
			return s
		}},
		{"the derived value left at the source's in the step (its first place, the title, holds the new value)", "slice/rules", func(s t12Slice) t12Slice {
			s.feature = t12Once(t, s.feature, "with a text of 101 characters", "with a text of 281 characters")
			return s
		}},
		{"the limit's two places differ", "slice/rules", func(s t12Slice) t12Slice {
			s.rules = t12Once(t, s.rules, "longer than 100 characters", "longer than 99 characters")
			return s
		}},
	} {
		planted := c.edit(clean)
		_, ok, ref := p.fit(planted.copy(t))
		t12DiffersAt(t, c.name, ok, ref, clean.copy(t), planted.copy(t), c.part)
	}
}

// T012, SC-005, FR-008: of two differences in one part the first is named: the place is not after
// the first difference.
func TestFitNamesTheFirstDifferenceOfAPart(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	clean := p.slc.apply(t12Replacer(t12Names("task", "tasks")))
	early := t12Once(t, clean.contract, "  /tasks:", "  /items:")
	both := t12Once(t, early, `"400":`, `"422":`)
	late := t12Once(t, clean.contract, `"400":`, `"422":`)
	for _, c := range []struct {
		what     string
		contract string
	}{{"early and late", both}, {"early only", early}, {"late only", late}} {
		s := clean
		s.contract = c.contract
		_, ok, ref := p.fit(s.copy(t))
		t12DiffersAt(t, c.what, ok, ref, clean.copy(t), s.copy(t), "slice/contract")
	}
	// The early and the late differ in their column: the place of both is at or before the early one.
	_, colBoth := t12Diff(clean.copy(t), func() captured.Copy { s := clean; s.contract = both; return s.copy(t) }())
	_, colLate := t12Diff(clean.copy(t), func() captured.Copy { s := clean; s.contract = late; return s.copy(t) }())
	if colBoth >= colLate {
		t.Fatalf("test bug: the early difference (column %d) is not before the late one (%d)", colBoth, colLate)
	}
}

// T012, SC-005, research R4 (E1): `note_id` left unchanged where `note` became `task` does not
// fit: a case form inside a longer word is a place of the parameter. The same slice with `task_id`
// fits, and `denote` becomes `detask`, `nOTE` stays (it is no form), so changing `nOTE` does not fit.
func TestFitWordsInsideLongerWords(t *testing.T) {
	f := t3Load(t, "notes")
	s := t12Read(t, "notes")
	// The source gets `note_id`, `NOTE_ID`, `Note_id`, `denote` and `nOTE` in its contract.
	s.contract = t12Once(t, s.contract, "        id:\n          type: integer\n", "        note_id:\n          type: integer\n")
	s.contract += "# NOTE_ID, Note_id, denote, nOTE\n"
	src := s.copy(t)
	specs := []t3Spec{t3Notes, t3Note}
	p := t12Pattern{src: src, tpl: f.template, ps: t3Params(src, f.template, specs), slc: s, spec: specs}
	if !strings.Contains(string(src.Contract), "note_id") {
		t.Fatal("test bug")
	}
	clean := s.apply(t12Replacer(t12Names("task", "tasks")))
	if !strings.Contains(clean.contract, "task_id") || !strings.Contains(clean.contract, "detask") || !strings.Contains(clean.contract, "nOTE") {
		t.Fatalf("test bug: the clean slice lacks task_id, detask or nOTE: %q", clean.contract)
	}
	values, ok, ref := p.fit(clean.copy(t))
	t12Fits(t, "task_id, detask, nOTE", values, ok, ref, map[string]string{"note": "task", "notes": "tasks"})

	for _, c := range []struct{ what, old, repl string }{
		{"note_id left unchanged", "task_id:", "note_id:"},
		{"denote left unchanged", "detask", "denote"},
		{"NOTE_ID left unchanged", "TASK_ID", "NOTE_ID"},
		{"Note_id left unchanged", "Task_id", "Note_id"},
		{"nOTE changed", "nOTE", "tASK"},
	} {
		planted := clean
		planted.contract = t12Once(t, planted.contract, c.old, c.repl)
		_, ok, ref := p.fit(planted.copy(t))
		t12DiffersAt(t, c.what, ok, ref, clean.copy(t), planted.copy(t), "slice/contract")
	}
}

// T012, FR-008: a slice cut off anywhere, or with a byte added, or with a part missing, does not
// fit and is refused with a place (no panic, no fit by reading a hole past the end).
func TestFitTruncatedAndExtendedPartsDoNotFit(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	clean := p.slc.apply(t12Replacer(t12Names("task", "tasks"))).copy(t)
	for cut := 0; cut < len(clean.Goals); cut++ {
		a := clean
		a.Goals = clean.Goals[:cut]
		if _, ok, ref := p.fit(a); ok {
			t.Fatalf("goals cut at %d fits", cut)
		} else if ref.Place == nil && ref.Parameter == nil {
			t12Stub(t, ref)
			t.Fatalf("goals cut at %d: no place or parameter named: %s", cut, t12Describe(ref))
		}
	}
	for _, c := range []struct {
		what string
		edit func(c *captured.Copy)
	}{
		{"goals extended", func(c *captured.Copy) { c.Goals = append(bytes.Clone(c.Goals), 'x') }},
		{"rules cut by one", func(c *captured.Copy) { c.Rules = c.Rules[:len(c.Rules)-1] }},
		{"contract extended", func(c *captured.Copy) { c.Contract = append(bytes.Clone(c.Contract), ' ') }},
		{"goals empty", func(c *captured.Copy) { c.Goals = nil }},
		{"rules empty", func(c *captured.Copy) { c.Rules = []byte{} }},
		{"contract empty", func(c *captured.Copy) { c.Contract = nil }},
		{"every part empty", func(c *captured.Copy) { *c = captured.Copy{} }},
	} {
		a := captured.Copy{Goals: clean.Goals, Rules: clean.Rules, Contract: clean.Contract}
		c.edit(&a)
		_, ok, ref := p.fit(a)
		if ok {
			t.Errorf("%s: fits", c.what)
		} else if ref.Place == nil && ref.Parameter == nil {
			t12Stub(t, ref)
			t.Errorf("%s: no place or parameter named: %s", c.what, t12Describe(ref))
		}
	}
}

// ---------------------------------------------------------------------------------------------
// Scenario 5: a value outside its parameter's form, and equal values

// T012, FR-009, scenario 5: a value outside the name form does not fit, naming the parameter and
// why: a space, a hyphen, an underscore, a digit first, a single letter, one letter with a digit,
// uppercase letters in the lowercase form, 65 characters, and a character outside ASCII.
func TestFitNameOutsideItsFormDoesNotFit(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	for _, v := range []string{"to do", "to-do", "to_do", "1ab", "a", "a1", "a123", "to.do", "tâche", strings.Repeat("a", 65)} {
		approved := p.slc.apply(t12Replacer(t12Names(v, "tasks"))).copy(t)
		_, ok, ref := p.fit(approved)
		t12Refuses(t, fmt.Sprintf("note=%q", v), ok, ref, "note")
	}
	// The plural is its own parameter: a bad value for it names it.
	approved := p.slc.apply(t12Replacer(t12Names("task", "to do"))).copy(t)
	_, ok, ref := p.fit(approved)
	t12Refuses(t, "notes=to do", ok, ref, "notes")
}

// T012, FR-009, scenario 5: a name where a number was, a sign, a space, a letter in a number, an
// exponent and 65 digits do not fit, naming the parameter; a decimal and an integer do (the
// limit-280 fixture).
func TestFitNumberOutsideItsFormDoesNotFit(t *testing.T) {
	p := t12Make(t, "limit-280", t3Notes, t3Note, t12Limit, t12Over)
	for _, v := range []string{"many", "-100", "+100", "1 00", "1e2", "10a", "1,5", "0x10", "100%", strings.Repeat("1", 65)} {
		approved := p.slc.apply(t12Replacer([]string{"280", v, "281", "101"})).copy(t)
		_, ok, ref := p.fit(approved)
		t12Refuses(t, fmt.Sprintf("limit=%q", v), ok, ref, "limit")
		// The same value where the derived number was names the other parameter.
		approved = p.slc.apply(t12Replacer([]string{"280", "100", "281", v})).copy(t)
		_, ok, ref = p.fit(approved)
		t12Refuses(t, fmt.Sprintf("over=%q", v), ok, ref, "over")
	}
	// 64 digits is the limit of a value and fits.
	v := strings.Repeat("1", 64)
	approved := p.slc.apply(t12Replacer([]string{"280", v, "281", "101"})).copy(t)
	values, ok, ref := p.fit(approved)
	t12Fits(t, "64 digits", values, ok, ref, map[string]string{"note": "note", "notes": "notes", "limit": v, "over": "101"})
}

// T012, FR-009, scenario 5: a string takes letters, digits, `_`, `-` and spaces; another character
// (here `;`, `.`, a letter outside ASCII) does not fit, naming the parameter.
func TestFitStringOutsideItsFormDoesNotFit(t *testing.T) {
	order := t3Spec{"order", captured.KindString, "creation order"}
	p := t12Make(t, "notes", t3Notes, t3Note, order)
	for _, v := range []string{"arrival time", "a-b_c 9", "X", "Arrival Time"} {
		approved := p.slc.apply(t12Replacer([]string{"creation order", v})).copy(t)
		values, ok, ref := p.fit(approved)
		t12Fits(t, "order="+v, values, ok, ref, map[string]string{"note": "note", "notes": "notes", "order": v})
	}
	for _, v := range []string{"arrival;time", "v1.2", "arrivée", "a/b"} {
		approved := p.slc.apply(t12Replacer([]string{"creation order", v})).copy(t)
		_, ok, ref := p.fit(approved)
		t12Refuses(t, fmt.Sprintf("order=%q", v), ok, ref, "order")
	}
}

// T012, FR-009, Edge Cases, scenario 5: two parameters given equal values do not fit, naming both
// parameters: note and notes both `item`; limit and over both `100`; equal in another case form
// (`item` and `Item` read from the same slice cannot differ, so only same-case equality is
// planted here).
func TestFitEqualValuesDoNotFit(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	approved := p.slc.apply(t12Replacer(t12Names("item", "item"))).copy(t)
	_, ok, ref := p.fit(approved)
	t12Refuses(t, "note=notes=item", ok, ref, "note", "notes")

	l := t12Make(t, "limit-280", t3Notes, t3Note, t12Limit, t12Over)
	approved = l.slc.apply(t12Replacer([]string{"280", "100", "281", "100"})).copy(t)
	_, ok, ref = l.fit(approved)
	t12Refuses(t, "limit=over=100", ok, ref, "limit", "over")
}

// ---------------------------------------------------------------------------------------------
// Paths (research R4 "Paths")

// T012, FR-008, research R4: values that give two template files the same filled path do not fit,
// naming the paths (the template's own paths); with the source values the same files are two files
// and fit.
func TestFitTwoFilesAtOneFilledPathDoNotFit(t *testing.T) {
	f := t3Load(t, "notes")
	one := canonical.File{Path: "internal/app/note.go", Content: []byte("package app\n")}
	two := canonical.File{Path: "internal/app/task.go", Content: []byte("package app\n\nvar x = 1\n")}
	tpl := t3WithFile(t3WithFile(f.template, "code", one), "code", two)
	specs := []t3Spec{t3Notes, t3Note}
	ps := t3Params(f.src, tpl, specs)
	s := t12Read(t, "notes")

	values, ok, ref := captured.Fit(f.src, tpl, t12Writable, ps, f.src)
	t12Fits(t, "source values: note.go and task.go are two files", values, ok, ref, map[string]string{"note": "note", "notes": "notes"})

	approved := s.apply(t12Replacer(t12Names("task", "tasks"))).copy(t)
	_, ok, ref = captured.Fit(f.src, tpl, t12Writable, ps, approved)
	t12RefusesPaths(t, "note.go fills to task.go", ok, ref, one.Path, two.Path)

	// The clash comes from the value only: `a_note.go` fills to `a_tasks.go`, which is a file.
	a := canonical.File{Path: "internal/app/a_note.go", Content: []byte("package app\n")}
	b := canonical.File{Path: "internal/app/a_tasks.go", Content: []byte("package app\n")}
	tpl2 := t3WithFile(t3WithFile(f.template, "code", a), "code", b)
	ps2 := t3Params(f.src, tpl2, specs)
	approved = s.apply(t12Replacer(t12Names("tasks", "taskss"))).copy(t)
	_, ok, ref = captured.Fit(f.src, tpl2, t12Writable, ps2, approved)
	t12RefusesPaths(t, "a_note.go fills to a_tasks.go", ok, ref, a.Path, b.Path)
}

// T012, FR-008, research R4: a filled path outside its step's writable entries does not fit,
// naming the template path; with the source values every path is inside and the slice fits (an
// entry may be a directory glob `dir/**` or one exact path).
func TestFitFilledPathOutsideWritableEntriesDoesNotFit(t *testing.T) {
	p := t12Make(t, "notes", t3Notes, t3Note)
	approved := p.slc.apply(t12Replacer(t12Names("task", "tasks"))).copy(t)
	for _, c := range []struct {
		what     string
		writable map[string][]string
	}{
		{"a directory entry of the old name", map[string][]string{"tests": {"tests/**"}, "code": {"cmd/notes/**", "internal/**"}}},
		{"an exact entry of the old path", map[string][]string{"tests": {"tests/**"}, "code": {"cmd/notes/main.go", "internal/**"}}},
	} {
		// With the source values every path is where it was and inside: the slice fits (the check
		// can pass).
		values, ok, ref := captured.Fit(p.src, p.tpl, c.writable, p.ps, p.src)
		t12Fits(t, c.what+" (source values)", values, ok, ref, map[string]string{"note": "note", "notes": "notes"})
		_, ok, ref = captured.Fit(p.src, p.tpl, c.writable, p.ps, approved)
		t12RefusesPaths(t, c.what, ok, ref, "cmd/notes/main.go")
	}
	// With the entries of the base pattern the task slice's paths (cmd/tasks/main.go) are inside.
	values, ok, ref := p.fit(approved)
	t12Fits(t, "inside the entries", values, ok, ref, map[string]string{"note": "task", "notes": "tasks"})
}

// t12RefusesPaths asserts a refusal by paths: Paths holds every name, no place or parameter, a detail.
func t12RefusesPaths(t testing.TB, what string, ok bool, ref captured.Refusal, names ...string) {
	t.Helper()
	if ok {
		t.Errorf("%s: fits, want a refusal naming the paths %v", what, names)
		return
	}
	t12Stub(t, ref)
	if ref.Detail == "" {
		t.Errorf("%s: no detail: %s", what, t12Describe(ref))
	}
	if ref.Place != nil || ref.Parameter != nil {
		t.Errorf("%s: a place or parameter is named for a path: %s", what, t12Describe(ref))
	}
	for _, n := range names {
		found := false
		for _, p := range ref.Paths {
			if p == n {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: Paths %v does not name %q", what, ref.Paths, n)
		}
	}
}

// ---------------------------------------------------------------------------------------------
// A pattern with no parameter, and linear matching

// T012, spec Edge Cases: a captured pattern with no parameter fits only the slice identical to the
// source: the source fits with no values, any difference (even the names) does not.
func TestFitPatternWithNoParameterFitsOnlyTheIdenticalSlice(t *testing.T) {
	f := t3Load(t, "notes")
	s := t12Read(t, "notes")
	for _, params := range [][]captured.Parameter{nil, {}} {
		values, ok, ref := captured.Fit(f.src, f.template, t12Writable, params, f.src)
		t12Fits(t, "identical", values, ok, ref, map[string]string{})
		approved := s.apply(t12Replacer(t12Names("task", "tasks"))).copy(t)
		_, ok, ref = captured.Fit(f.src, f.template, t12Writable, params, approved)
		t12DiffersAt(t, "names changed", ok, ref, f.src, approved, "slice/goals")
		one := s
		one.contract = strings.Replace(one.contract, "version: 1.0.0", "version: 1.0.1", 1)
		_, ok, ref = captured.Fit(f.src, f.template, t12Writable, params, one.copy(t))
		t12DiffersAt(t, "one character", ok, ref, f.src, one.copy(t), "slice/contract")
	}
	// A slice with no content at all, with no parameter and no template, fits itself.
	values, ok, ref := captured.Fit(captured.Copy{}, captured.Template{}, nil, nil, captured.Copy{})
	t12Fits(t, "empty source and approved", values, ok, ref, map[string]string{})
}

// t12Places is the parameters of specs (names only) with their places in the copy's contract part,
// found by plain string search (t3Params's scanner is too slow for a mebibyte): every case form,
// line 1, the column in characters (the text is ASCII). The 32 names are all different 4 letter
// words, so no two places overlap.
func t12Places(src captured.Copy, specs []t3Spec) []captured.Parameter {
	text := string(src.Contract)
	var out []captured.Parameter
	for _, s := range specs {
		p := captured.Parameter{Name: s.Name, Kind: s.Kind, Value: s.Value, Source: captured.SourceName, Origin: captured.OriginProposed}
		for _, f := range []struct {
			form captured.Form
			text string
		}{{captured.FormLower, s.Value}, {captured.FormCapitalised, t12Cap(s.Value)}, {captured.FormUpper, strings.ToUpper(s.Value)}} {
			for from := 0; ; {
				i := strings.Index(text[from:], f.text)
				if i < 0 {
					break
				}
				p.Places = append(p.Places, captured.Place{Part: "slice/contract", In: captured.InContent, Line: 1, Column: from + i + 1, Form: f.form})
				from += i + len(f.text)
			}
		}
		out = append(out, p)
	}
	return out
}

// T012, FR-008, research R4: matching is linear: a 1 MiB slice with 32 parameters (each in
// about 860 places, under the limit of 10000) fits in under 1 s, and a slice of the same size
// differing only at its very end is refused in under 1 s.
func TestFitIsLinearOnAMebibyteWith32Parameters(t *testing.T) {
	const names = 32
	var specs []t3Spec
	var from, to, toNames []string
	for i := 0; i < names; i++ {
		src := "zq" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		dst := "yxw" + src[2:]
		specs = append(specs, t3Spec{"p" + src[2:], captured.KindName, src})
		from = append(from, src)
		to = append(to, dst)
		toNames = append(toNames, src, dst, t12Cap(src), t12Cap(dst), strings.ToUpper(src), strings.ToUpper(dst))
	}
	var b strings.Builder
	forms := []func(string) string{func(s string) string { return s }, t12Cap, strings.ToUpper}
	for rep := 0; b.Len() < 1<<20; rep++ {
		for i, n := range from {
			b.WriteString("# ")
			b.WriteString(forms[(rep+i)%3](n))
			b.WriteString(" lorem ipsum dolor sit amet\n")
		}
	}
	contract := b.String()
	s := t12Read(t, "notes")
	s.contract = contract
	src := s.copy(t)
	if len(src.Contract) < 1<<20 {
		t.Fatalf("test bug: the contract part has %d bytes, want at least 1 MiB", len(src.Contract))
	}
	tpl := captured.Template{}
	ps := t12Places(src, specs)
	for _, p := range ps {
		if n := len(p.Places); n == 0 || n > captured.PlacesMax {
			t.Fatalf("test bug: %s has %d places", p.Name, n)
		}
	}
	approved := s.apply(strings.NewReplacer(toNames...)).copy(t)
	want := map[string]string{}
	for i := range from {
		want[specs[i].Name] = to[i]
	}
	start := time.Now()
	values, ok, ref := captured.Fit(src, tpl, nil, ps, approved)
	took := time.Since(start)
	t12Fits(t, "1 MiB, 32 parameters", values, ok, ref, want)
	if took > time.Second {
		t.Errorf("a 1 MiB slice with 32 parameters took %v, want under 1 s", took)
	}
	// A difference at the very end: the whole slice is read, and refused.
	late := approved
	late.Contract = bytes.Clone(approved.Contract)
	late.Contract[len(late.Contract)-3] ^= 1
	start = time.Now()
	_, ok, ref = captured.Fit(src, tpl, nil, ps, late)
	took = time.Since(start)
	t12DiffersAt(t, "difference at the end", ok, ref, approved, late, "slice/contract")
	if took > time.Second {
		t.Errorf("a refusal at the end of a 1 MiB slice took %v, want under 1 s", took)
	}
}
