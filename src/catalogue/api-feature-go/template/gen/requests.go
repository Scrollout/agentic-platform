package gen

// The request check (catalogue README, "Request check"): every operation with a request body is
// served through refuseMalformed, which refuses a body the approved contract does not allow with
// 400 {"message": <text>} before anything of the operation runs, and passes on a body that holds
// only the declared members under their exact names. The shapes are gen/request_shapes.go,
// generated from the contract.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// requestBody is one operation's request body as the contract declares it.
type requestBody struct {
	Required bool          // an empty body is refused only when it is required
	Shape    *requestShape // nil means any JSON value
}

// requestShape is what the contract allows one value to hold.
type requestShape struct {
	Types           []string        // JSON Schema type names; empty means any
	Properties      []requestMember // declared members, in contract order; nil keeps an object as sent
	RequiredMembers []string        // in contract order
	Closed          bool            // additionalProperties: false
	Items           *requestShape   // each array item; nil means any
}

// requestMember is one declared member of an object, by its exact name.
type requestMember struct {
	Name  string
	Shape *requestShape
}

// maxRequestBody is the largest request body served, in bytes.
const maxRequestBody = 1048576

// requestPointerRunes is how much of a member's JSON Pointer a message quotes.
const requestPointerRunes = 64

// refuseMalformed serves next for the operation key ("<METHOD> <path>", the path as the contract
// writes it) only with a request body that operation's contract allows. A body that is larger
// than 1048576 bytes, empty while required, not exactly one JSON value (or nested deeper than
// 10000 levels), naming a member twice, or of a shape the contract does not allow is answered
// 400 with {"message": <text>}, the first problem found, and next never runs. Otherwise next
// runs with the body as sent, or, when members were removed or an integer was written in another
// form, with the body written again (compact, members in body order) and its length. The
// request's Content-Type is not judged. An operation without a request body is next itself.
func refuseMalformed(key string, next http.HandlerFunc) http.HandlerFunc {
	body, ok := requestBodies[key]
	if !ok {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var sent []byte
		if r.Body != nil {
			b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
			var tooLarge *http.MaxBytesError
			switch {
			case errors.As(err, &tooLarge):
				refuseRequest(w, "the request body is larger than 1048576 bytes")
				return
			case err != nil:
				refuseRequest(w, "the request body is not valid JSON")
				return
			}
			sent = b
		}
		out, message := checkRequestBody(body, sent)
		if message != "" {
			refuseRequest(w, message)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(out))
		r.ContentLength = int64(len(out))
		r.Header.Set("Content-Length", strconv.Itoa(len(out)))
		next(w, r)
	}
}

// refuseRequest answers 400 with {"message": message}.
func refuseRequest(w http.ResponseWriter, message string) {
	b, _ := json.Marshal(struct {
		Message string `json:"message"`
	}{message})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(b)
}

// checkRequestBody is the body to pass on for sent, or the refusal's message.
func checkRequestBody(body *requestBody, sent []byte) ([]byte, string) {
	if len(bytes.Trim(sent, " \t\r\n")) == 0 {
		if body.Required {
			return nil, "the request body is missing"
		}
		return sent, ""
	}
	if !json.Valid(sent) {
		return nil, "the request body is not valid JSON"
	}
	c := &requestCheck{dec: json.NewDecoder(bytes.NewReader(sent))}
	c.dec.UseNumber()
	v, message := c.read()
	if message != "" {
		return nil, message
	}
	changed, message := c.walk(v, body.Shape)
	if message != "" {
		return nil, message
	}
	if !changed {
		return sent, ""
	}
	var out bytes.Buffer
	writeRequestValue(&out, v)
	return out.Bytes(), ""
}

// requestValue is one JSON value, its members in body order and its number's text as sent.
type requestValue struct {
	kind    string // "object", "array", "string", "number", "boolean" or "null"
	text    string // a string's value, a number's text
	names   []string
	members []*requestValue
	items   []*requestValue
}

// requestCheck reads and walks one body; path is the JSON Pointer of the value being read or walked.
type requestCheck struct {
	dec  *json.Decoder
	path []string
}

// member is the message "member <pointer> <what>" for the value at the current path, its JSON
// Pointer cut to its first 64 runes and quoted with strconv.Quote.
func (c *requestCheck) member(what string) string {
	p := strings.Join(c.path, "")
	if r := []rune(p); len(r) > requestPointerRunes {
		p = string(r[:requestPointerRunes])
	}
	return "member " + strconv.Quote(p) + " " + what
}

func (c *requestCheck) push(seg string) {
	c.path = append(c.path, "/"+strings.ReplaceAll(strings.ReplaceAll(seg, "~", "~0"), "/", "~1"))
}

func (c *requestCheck) pop() { c.path = c.path[:len(c.path)-1] }

// read reads the next value of the body (already json.Valid), refusing a name used twice in one
// object: the first such name in the body is the one whose second occurrence comes first.
func (c *requestCheck) read() (*requestValue, string) {
	tok, err := c.dec.Token()
	if err != nil {
		return nil, "the request body is not valid JSON"
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '[' {
			v := &requestValue{kind: "array"}
			for i := 0; c.dec.More(); i++ {
				c.push(strconv.Itoa(i))
				item, message := c.read()
				c.pop()
				if message != "" {
					return nil, message
				}
				v.items = append(v.items, item)
			}
			_, _ = c.dec.Token()
			return v, ""
		}
		v := &requestValue{kind: "object"}
		var seen map[string]bool // made once an object has enough members to need it
		for c.dec.More() {
			nt, err := c.dec.Token()
			name, ok := nt.(string)
			if err != nil || !ok {
				return nil, "the request body is not valid JSON"
			}
			c.push(name)
			if seen == nil && len(v.names) >= 16 {
				seen = make(map[string]bool, 2*len(v.names))
				for _, n := range v.names {
					seen[n] = true
				}
			}
			if (seen == nil && v.has(name)) || seen[name] {
				return nil, c.member("is named twice")
			}
			if seen != nil {
				seen[name] = true
			}
			m, message := c.read()
			c.pop()
			if message != "" {
				return nil, message
			}
			v.names, v.members = append(v.names, name), append(v.members, m)
		}
		_, _ = c.dec.Token()
		return v, ""
	case string:
		return &requestValue{kind: "string", text: t}, ""
	case json.Number:
		return &requestValue{kind: "number", text: string(t)}, ""
	case bool:
		return &requestValue{kind: "boolean", text: strconv.FormatBool(t)}, ""
	default:
		return &requestValue{kind: "null"}, ""
	}
}

// walk checks v against s, depth first, and removes the members s does not declare. At each
// object: a required member missing, then an undeclared member when closed (body order), then
// each declared member present (contract order); array items in order. It reports whether v was
// changed, or the first problem's message.
func (c *requestCheck) walk(v *requestValue, s *requestShape) (bool, string) {
	if s == nil {
		return false, ""
	}
	changed := false
	if len(s.Types) > 0 {
		ok := false
		for _, t := range s.Types {
			if t == v.kind {
				ok = true
			}
		}
		if !ok && v.kind == "number" && requestTypesHold(s.Types, "integer") {
			if plain, isInt := requestPlainInteger(v.text); isInt {
				ok, changed = true, plain != v.text
				v.text = plain
			}
		}
		if !ok {
			if len(c.path) == 0 {
				return false, "the request body must be " + requestTypeList(s.Types)
			}
			return false, c.member("must be " + requestTypeList(s.Types))
		}
	}
	switch v.kind {
	case "object":
		return c.walkObject(v, s, changed)
	case "array":
		for i, item := range v.items {
			c.push(strconv.Itoa(i))
			ch, message := c.walk(item, s.Items)
			c.pop()
			if message != "" {
				return false, message
			}
			changed = changed || ch
		}
	}
	return changed, ""
}

// walkObject is walk for an object value. The declared names are looked up in the shape's index,
// made once per shape, so an object costs time for its own members only.
func (c *requestCheck) walkObject(v *requestValue, s *requestShape, changed bool) (bool, string) {
	for _, n := range s.RequiredMembers {
		if !v.has(n) {
			c.push(n)
			return false, c.member("is required")
		}
	}
	if len(v.names) == 0 || (s.Properties == nil && !s.Closed) {
		return changed, ""
	}
	index := requestMemberIndex(s)
	var present [][2]int // the position of each declared member in Properties, and in the body
	for i, n := range v.names {
		if p, ok := index[n]; ok {
			present = append(present, [2]int{p, i})
		} else if s.Closed {
			c.push(n)
			return false, c.member("is not declared")
		}
	}
	slices.SortFunc(present, func(a, b [2]int) int { return a[0] - b[0] })
	for _, p := range present {
		c.push(v.names[p[1]])
		ch, message := c.walk(v.members[p[1]], s.Properties[p[0]].Shape)
		c.pop()
		if message != "" {
			return false, message
		}
		changed = changed || ch
	}
	if s.Properties == nil || len(present) == len(v.names) {
		return changed, ""
	}
	names, members := make([]string, 0, len(present)), make([]*requestValue, 0, len(present))
	for i, n := range v.names {
		if _, ok := index[n]; ok {
			names, members = append(names, n), append(members, v.members[i])
		}
	}
	v.names, v.members = names, members
	return true, ""
}

// has reports whether the object v has a member named n.
func (v *requestValue) has(n string) bool {
	for _, x := range v.names {
		if x == n {
			return true
		}
	}
	return false
}

// requestMemberIndexes holds, per shape, the position in Properties of each declared name.
var requestMemberIndexes sync.Map // *requestShape -> map[string]int

// requestMemberIndex is the position in s.Properties of each declared name, made on first use.
func requestMemberIndex(s *requestShape) map[string]int {
	if m, ok := requestMemberIndexes.Load(s); ok {
		return m.(map[string]int)
	}
	m := make(map[string]int, len(s.Properties))
	for i, p := range s.Properties {
		if _, dup := m[p.Name]; !dup {
			m[p.Name] = i
		}
	}
	got, _ := requestMemberIndexes.LoadOrStore(s, m)
	return got.(map[string]int)
}

// requestPlainInteger decides, on its text alone, whether the JSON number text has a zero fractional
// part, and gives it as a plain integer: a plain literal as sent ("-0" as "0"); one with a
// fraction or exponent written without them, refused (false) when that plain form would exceed
// 32 digits. It never builds the number, so a huge exponent costs nothing.
func requestPlainInteger(text string) (string, bool) {
	sign, t := "", text
	if strings.HasPrefix(t, "-") {
		sign, t = "-", t[1:]
	}
	mantissa, exp, hasExp := strings.Cut(strings.ToLower(t), "e")
	whole, frac, hasFrac := strings.Cut(mantissa, ".")
	if !hasExp && !hasFrac {
		if strings.Trim(whole, "0") == "" {
			return "0", true
		}
		return text, true
	}
	digits := strings.TrimLeft(whole+frac, "0")
	if digits == "" {
		return "0", true
	}
	shift := -len(frac) // the power of ten that digits is multiplied by
	if hasExp {
		neg := strings.HasPrefix(exp, "-")
		e := strings.TrimLeft(strings.TrimLeft(exp, "+-"), "0")
		if len(e) > 9 { // more than a body could hold digits for: no integer of 32 digits or less
			return "", false
		}
		n, _ := strconv.Atoi("0" + e)
		if neg {
			n = -n
		}
		shift += n
	}
	if shift < 0 {
		if len(digits)-len(strings.TrimRight(digits, "0")) < -shift {
			return "", false // a non-zero digit after the point
		}
		digits = digits[:len(digits)+shift]
	} else {
		if len(digits)+shift > 32 {
			return "", false
		}
		digits += strings.Repeat("0", shift)
	}
	if len(digits) > 32 {
		return "", false
	}
	return sign + digits, true
}

// requestTypeList is the allowed types, each with its article, joined with " or ".
func requestTypeList(types []string) string {
	articles := map[string]string{
		"string": "a string", "integer": "an integer", "number": "a number", "boolean": "a boolean",
		"object": "an object", "array": "an array", "null": "null",
	}
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = articles[t]
		if out[i] == "" {
			out[i] = t
		}
	}
	return strings.Join(out, " or ")
}

func requestTypesHold(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// writeRequestValue writes v as compact JSON, numbers with their text.
func writeRequestValue(b *bytes.Buffer, v *requestValue) {
	switch v.kind {
	case "object":
		b.WriteByte('{')
		for i, n := range v.names {
			if i > 0 {
				b.WriteByte(',')
			}
			writeRequestString(b, n)
			b.WriteByte(':')
			writeRequestValue(b, v.members[i])
		}
		b.WriteByte('}')
	case "array":
		b.WriteByte('[')
		for i, item := range v.items {
			if i > 0 {
				b.WriteByte(',')
			}
			writeRequestValue(b, item)
		}
		b.WriteByte(']')
	case "string":
		writeRequestString(b, v.text)
	case "null":
		b.WriteString("null")
	default:
		b.WriteString(v.text)
	}
}

// writeRequestString writes s as a JSON string, without escaping HTML characters.
func writeRequestString(b *bytes.Buffer, s string) {
	var q bytes.Buffer
	enc := json.NewEncoder(&q)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	b.Write(bytes.TrimSuffix(q.Bytes(), []byte("\n")))
}
