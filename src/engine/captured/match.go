package captured

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The parts of a slice copy, as a Place names them.
const (
	partGoals    = "slice/goals"
	partRules    = "slice/rules"
	partContract = "slice/contract"
)

// The forms of each kind's values (research R4). Go's regexp runs in linear time; it only ever
// sees one value of at most ValueMaxChars characters.
var (
	nameForm   = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	numberForm = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
	stringForm = regexp.MustCompile(`^[A-Za-z0-9_ -]+$`)
	paramName  = regexp.MustCompile(ParameterNamePattern)
)

// valueRefusal says why value is not of kind's form, or "" when it is.
func valueRefusal(kind Kind, value string) string {
	if value == "" {
		return "the value is empty"
	}
	if n := utf8.RuneCountInString(value); n > ValueMaxChars {
		return fmt.Sprintf("the value has %d characters, more than %d", n, ValueMaxChars)
	}
	switch kind {
	case KindName:
		if !nameForm.MatchString(value) {
			return fmt.Sprintf("the name %q is not a lowercase letter followed by lowercase letters and digits", value)
		}
		letters := 0
		for i := 0; i < len(value); i++ {
			if value[i] >= 'a' && value[i] <= 'z' {
				letters++
			}
		}
		if letters < 2 {
			return fmt.Sprintf("the name %q has fewer than two letters", value)
		}
	case KindNumber:
		if !numberForm.MatchString(value) {
			return fmt.Sprintf("%q is not an integer or a decimal without a sign", value)
		}
	case KindString:
		if !stringForm.MatchString(value) {
			return fmt.Sprintf("%q holds a character other than letters, digits, `_`, `-` and spaces", value)
		}
	default:
		return fmt.Sprintf("the kind %q is not name, number or string", kind)
	}
	return ""
}

// form is one text a parameter's value takes in the files: one of a name's three case forms, or
// a number's or a string's exact text.
type form struct {
	param int
	form  Form
	text  string
	whole bool // a number: matched only as a whole number
}

// forms gives the texts of value under kind, each with its case form.
func forms(param int, kind Kind, value string) []form {
	switch kind {
	case KindName:
		return []form{
			{param, FormLower, value, false},
			{param, FormCapitalised, strings.ToUpper(value[:1]) + value[1:], false},
			{param, FormUpper, strings.ToUpper(value), false},
		}
	case KindNumber:
		return []form{{param, FormExact, value, true}}
	default:
		return []form{{param, FormExact, value, false}}
	}
}

// in gives value's text in case form f: the same rule turns a source form into a later value's.
// An empty value (refused, but read on by Fit) is the empty text in every form.
func in(f Form, value string) string {
	switch f {
	case FormCapitalised:
		if value == "" {
			return ""
		}
		return strings.ToUpper(value[:1]) + value[1:]
	case FormUpper:
		return strings.ToUpper(value)
	default:
		return value
	}
}

// matcher finds the places of a set of parameters' source values in a text in one pass: left to
// right, at each position the longest form that occurs there, never overlapping. Its forms are
// indexed by their first byte, so a text is read once whatever its size; it never backtracks.
type matcher struct {
	forms   []form
	byFirst [256][]int // indices into forms, longest first
}

// newMatcher is the matcher of values, each a (kind, value) already of its kind's form (so not
// empty and pairwise different in every case form).
func newMatcher(kinds []Kind, values []string) *matcher {
	m := &matcher{}
	for i := range values {
		m.forms = append(m.forms, forms(i, kinds[i], values[i])...)
	}
	for i, f := range m.forms {
		b := f.text[0]
		m.byFirst[b] = append(m.byFirst[b], i)
	}
	for b := range m.byFirst {
		idx := m.byFirst[b]
		sort.SliceStable(idx, func(x, y int) bool { return len(m.forms[idx[x]].text) > len(m.forms[idx[y]].text) })
	}
	return m
}

// hit is one place a form occurs: its byte offset and length, and its line and column (counted
// from 1, columns in characters).
type hit struct {
	form         *form
	off          int
	line, column int
}

// scan calls found for every place in text, in order.
func (m *matcher) scan(text string, found func(h hit)) {
	line, col := 1, 1
	for i := 0; i < len(text); {
		b := text[i]
		var best *form
		for _, fi := range m.byFirst[b] {
			f := &m.forms[fi]
			if strings.HasPrefix(text[i:], f.text) && (!f.whole || wholeNumber(text, i, i+len(f.text))) {
				best = f
				break
			}
		}
		if best != nil {
			found(hit{best, i, line, col})
			i += len(best.text)
			col += len(best.text) // every form is ASCII without a line feed
			continue
		}
		switch {
		case b == '\n':
			line, col = line+1, 1
		case b&0xC0 != 0x80: // the first byte of a character
			col++
		}
		i++
	}
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// wholeNumber says whether text[start:end] stands as a whole number: the characters around it are
// not digits, letters or `_`, and not a `.` followed by a digit.
func wholeNumber(text string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(text[:start])
		if wordRune(r) || r == '.' { // a `.` before is followed by the number's first digit
			return false
		}
	}
	if end < len(text) {
		r, n := utf8.DecodeRuneInString(text[end:])
		if wordRune(r) {
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

// texts gives each text the places are found in: the copy's parts (ofSlice), then each template
// output in name order, each file's content and then its path.
func texts(src Copy, template Template, each func(part string, where In, text string, ofSlice bool)) {
	each(partGoals, InContent, string(src.Goals), true)
	each(partRules, InContent, string(src.Rules), true)
	each(partContract, InContent, string(src.Contract), true)
	outputs := make([]string, 0, len(template))
	for o := range template {
		outputs = append(outputs, o)
	}
	sort.Strings(outputs)
	for _, o := range outputs {
		for _, f := range template[o] {
			each(f.Path, InContent, string(f.Content), false)
			each(f.Path, InPath, f.Path, false)
		}
	}
}

// locate gives, per value, every place it occurs in src and template, and how many of them are
// in src.
func locate(src Copy, template Template, kinds []Kind, values []string) (places [][]Place, inSlice []int) {
	places, inSlice = make([][]Place, len(values)), make([]int, len(values))
	m := newMatcher(kinds, values)
	texts(src, template, func(part string, where In, text string, ofSlice bool) {
		m.scan(text, func(h hit) {
			places[h.form.param] = append(places[h.form.param], Place{Part: part, In: where, Line: h.line, Column: h.column, Form: h.form.form})
			if ofSlice {
				inSlice[h.form.param]++
			}
		})
	})
	return places, inSlice
}
