package captured

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/agentic-platform/engine/checks"
)

// readMaxBytes bounds the text read for one value at its first place: enough for a value one
// character over ValueMaxChars, each character up to four bytes, so an over-long value is read
// and refused by its form rather than missed.
const readMaxBytes = 4 * (ValueMaxChars + 1)

// Fit says whether approved fits a captured pattern whose source is src: the values (by
// parameter name) that fill src into exactly approved, or ok false with the refusal. writable
// gives each template output's step's writable entries, which every filled path must match.
//
// The source's parts (goals, rules, contract, in that order) are split into literal text and the
// places of the parameters' source values, found as Propose and Fill find them. Each literal must
// equal the approved part's text where it falls; at a parameter's first place its value is read
// from the approved part (the text up to where the following literal starts) in that place's case
// form, and must be of the parameter's kind's form, not be a Go keyword (a name; its own source
// value excepted, which the source run passed every check with), and differ from every other
// parameter's value in every case form; at each later place the approved part must hold that
// value in the place's case form. The filled template paths must then be distinct (two template files with different
// paths filled to one path) and each inside its output's writable entries; an output with files
// and no entry in writable has no path inside.
//
// A refusal is the first that applies, in this order: the first place, in reading order, the
// approved slice differs (Place, its column in characters in the approved part); then the first
// value refused, in reading order (Parameter); then paths (Paths, the template's paths). A value
// refused is read on as that parameter's value, so a difference anywhere later in the slice
// (also at a later place of the same parameter) is told as its Place, and the value refusal
// only when the slice otherwise matches. Every refusal carries a Detail in words.
//
// Limits: a value is read as the shortest text after which the following literal starts, while
// one of its kind's form is preferred; where two places of different parameters touch with no
// literal between them, the first takes the longest run of its form's characters. A parameter
// whose source value occurs nowhere in src is refused (Check never keeps one). The cost is linear
// in the sizes of src, approved and the template's paths.
func Fit(src Copy, template Template, writable map[string][]string, parameters []Parameter, approved Copy) (values map[string]string, ok bool, refusal Refusal) {
	if err := checkSet(parameters); err != nil {
		r := err.(*Refused)
		return nil, false, Refusal{Parameter: r, Detail: "the captured pattern's parameter " + r.Error()}
	}
	kinds, sources := kindsValues(parameters)
	f := &fitter{ps: parameters, m: newMatcher(kinds, sources), values: make([]string, len(parameters)), read: make([]bool, len(parameters))}
	for _, part := range []struct {
		name     string
		src, app []byte
	}{{partGoals, src.Goals, approved.Goals}, {partRules, src.Rules, approved.Rules}, {partContract, src.Contract, approved.Contract}} {
		if ref, bad := f.part(part.name, string(part.src), string(part.app)); bad {
			return nil, false, ref
		}
	}
	if f.refused != nil {
		return nil, false, *f.refused
	}
	values = make(map[string]string, len(parameters))
	for i, p := range parameters {
		if !f.read[i] {
			r := &Refused{Parameter: p.Name, Detail: fmt.Sprintf("its source value %q occurs nowhere in the source slice, so no value can be read for it", p.Value)}
			return nil, false, Refusal{Parameter: r, Detail: r.Error()}
		}
		values[p.Name] = f.values[i]
	}
	if ref, bad := fitPaths(template, writable, parameters, values); bad {
		return nil, false, ref
	}
	return values, true, Refusal{}
}

// fitter holds the values read so far while the parts are compared, and the first value
// refused, which is told only when no place differs.
type fitter struct {
	ps      []Parameter
	m       *matcher
	values  []string
	read    []bool
	refused *Refusal
}

// refuse keeps the value refusal of parameter p with why, unless one is kept already.
func (f *fitter) refuse(p int, why string) {
	if f.refused == nil {
		r := &Refused{Parameter: f.ps[p].Name, Detail: why}
		f.refused = &Refusal{Parameter: r, Detail: r.Error()}
	}
}

// part compares one part of the source with the approved part's text a and gives the first
// place that differs; a value refused is kept (refuse) and read on as the parameter's value.
func (f *fitter) part(name, s, a string) (Refusal, bool) {
	var holes []hit
	f.m.scan(s, func(h hit) { holes = append(holes, h) })
	j, last := 0, 0
	for k, h := range holes {
		lit := s[last:h.off]
		if d := differs(a[j:], lit, false); d >= 0 {
			return placeRefusal(name, a, j+d, "", "the approved slice differs from the captured pattern's source"), true
		}
		j += len(lit)
		last = h.off + len(h.form.text)
		p := h.form.param
		if f.read[p] {
			want := in(h.form.form, f.values[p])
			if d := differs(a[j:], want, false); d >= 0 {
				return placeRefusal(name, a, j+d, h.form.form,
					fmt.Sprintf("the approved slice differs from the captured pattern's source: %s's value %q is expected here in its %s form", f.ps[p].Name, f.values[p], h.form.form)), true
			}
			j += len(want)
			continue
		}
		next, final := s[last:], true
		if k+1 < len(holes) {
			next, final = s[last:holes[k+1].off], false
		}
		t, found := f.readValue(a[j:], h.form, next, final)
		if !found {
			return placeRefusal(name, a, j, h.form.form,
				fmt.Sprintf("the approved slice differs from the captured pattern's source: no value of %s is followed by the source's text here", f.ps[p].Name)), true
		}
		v, ofForm := valueOf(h.form.form, t)
		if !ofForm {
			return placeRefusal(name, a, j, h.form.form,
				fmt.Sprintf("the approved slice differs from the captured pattern's source: %q is not the %s form of a value of %s", t, h.form.form, f.ps[p].Name)), true
		}
		if why := valueRefusal(f.ps[p].Kind, v); why != "" {
			f.refuse(p, why)
		} else if why := reservedRefusal(f.ps[p].Kind, v, f.ps[p].Value); why != "" {
			f.refuse(p, why)
		}
		for q := range f.ps {
			if f.read[q] && strings.EqualFold(f.values[q], v) {
				f.refuse(p, fmt.Sprintf("its value %q equals the value of %s in a case form", v, f.ps[q].Name))
				break
			}
		}
		f.values[p], f.read[p] = v, true
		j += len(t)
	}
	if d := differs(a[j:], s[last:], true); d >= 0 {
		return placeRefusal(name, a, j+d, "", "the approved slice differs from the captured pattern's source"), true
	}
	return Refusal{}, false
}

// differs gives the first byte at which a does not start with lit (with final, also a byte of a
// after lit), or -1.
func differs(a, lit string, final bool) int {
	n := min(len(a), len(lit))
	for i := 0; i < n; i++ {
		if a[i] != lit[i] {
			return i
		}
	}
	if len(a) < len(lit) || (final && len(a) > len(lit)) {
		return n
	}
	return -1
}

// readValue reads the text at a parameter's first place from rest: the text before the following
// literal next (the end of the part when final), at most readMaxBytes bytes.
func (f *fitter) readValue(rest string, fm *form, next string, final bool) (string, bool) {
	if final {
		l := len(rest) - len(next)
		if l < 0 || l > readMaxBytes || rest[l:] != next {
			return "", false
		}
		return rest[:l], true
	}
	if next == "" {
		l := 0
		for l < len(rest) && l < readMaxBytes && valueByte(fm, l, rest[l]) {
			l++
		}
		return rest[:l], true
	}
	probe := next[:min(len(next), readMaxBytes)]
	first := -1
	for l := 0; l <= readMaxBytes && l+len(probe) <= len(rest); l++ {
		if !strings.HasPrefix(rest[l:], probe) {
			continue
		}
		if first < 0 {
			first = l
		}
		if v, ok := valueOf(fm.form, rest[:l]); ok && valueRefusal(f.ps[fm.param].Kind, v) == "" {
			return rest[:l], true
		}
	}
	if first < 0 {
		return "", false
	}
	return rest[:first], true
}

// valueByte says whether b, the i-th byte of a value's text in form fm, is one of its form's
// characters.
func valueByte(fm *form, i int, b byte) bool {
	lower, upper, digit := b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9'
	switch fm.form {
	case FormLower:
		return lower || digit
	case FormCapitalised:
		if i == 0 {
			return upper
		}
		return lower || digit
	case FormUpper:
		return upper || digit
	}
	if fm.whole {
		return digit || b == '.'
	}
	return lower || upper || digit || b == '_' || b == '-' || b == ' '
}

// valueOf gives the value whose text in case form fm is t, and false when t is no such text.
func valueOf(fm Form, t string) (string, bool) {
	if t == "" {
		return "", true // refused by its kind's form
	}
	var v string
	switch fm {
	case FormCapitalised:
		v = strings.ToLower(t[:1]) + t[1:]
	case FormUpper:
		v = strings.ToLower(t)
	default:
		return t, true
	}
	return v, v != "" && in(fm, v) == t
}

// placeRefusal is the refusal at byte off of the approved part a.
func placeRefusal(part, a string, off int, fm Form, detail string) Refusal {
	line := 1 + strings.Count(a[:off], "\n")
	col := 1 + utf8.RuneCountInString(a[strings.LastIndexByte(a[:off], '\n')+1:off])
	p := &Place{Part: part, In: InContent, Line: line, Column: col, Form: fm}
	return Refusal{Place: p, Detail: fmt.Sprintf("%s at %s line %d column %d", detail, part, line, col)}
}

// fitPaths fills the template's paths with values and refuses two template files with different
// paths filled to one path, or a filled path outside its output's writable entries.
func fitPaths(template Template, writable map[string][]string, parameters []Parameter, values map[string]string) (Refusal, bool) {
	outputs := make([]string, 0, len(template))
	for o := range template {
		outputs = append(outputs, o)
	}
	sort.Strings(outputs)
	fill := filler(parameters, values)
	from := map[string]string{}
	for _, o := range outputs {
		for _, file := range template[o] {
			path := fill(file.Path)
			if other, dup := from[path]; dup && other != file.Path {
				return Refusal{Paths: []string{other, file.Path},
					Detail: fmt.Sprintf("the template files %s and %s both fill to the path %s", other, file.Path, path)}, true
			}
			from[path] = file.Path
			if !checks.MatchGlob(writable[o], path) {
				return Refusal{Paths: []string{file.Path},
					Detail: fmt.Sprintf("the template file %s fills to the path %s, outside the writable entries %v of the step of %s", file.Path, path, writable[o], o)}, true
			}
		}
	}
	return Refusal{}, false
}

// filler gives the function that replaces every place of parameters' source values in a text by
// values, each in its place's case form; parameters and values have passed checkSet and Fill's
// value checks (or Fit's reading).
func filler(parameters []Parameter, values map[string]string) func(string) string {
	kinds, sources := kindsValues(parameters)
	m := newMatcher(kinds, sources)
	return func(text string) string {
		var b bytes.Buffer
		last := 0
		m.scan(text, func(h hit) {
			b.WriteString(text[last:h.off])
			b.WriteString(in(h.form.form, values[parameters[h.form.param].Name]))
			last = h.off + len(h.form.text)
		})
		if last == 0 {
			return text
		}
		b.WriteString(text[last:])
		return b.String()
	}
}
