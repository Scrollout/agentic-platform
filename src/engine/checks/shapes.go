package checks

// The request shapes of the approved contract (catalogue README, "Request check"; ADR 0087,
// ADR 0088): one collector and one renderer that the generate procedure and
// RequestShapesGenerated both use, so they cannot disagree about what a request body may hold.

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// RequestShapesFile is the generated file that holds the request shapes.
const RequestShapesFile = "gen/request_shapes.go"

// RequestShape is what the approved contract allows one request body value to hold.
type RequestShape struct {
	// Types are the JSON Schema type names the value may have, from `type`; empty means any.
	Types []string
	// Properties are the declared members: `properties` in contract order, then the names only in
	// `required`, in `required` order, each of any type. Nil when the schema has neither
	// `properties` nor `required` nor `additionalProperties: false` (an object is then kept as
	// sent); `properties: {}` gives a non-nil, empty slice.
	Properties []Member
	// RequiredMembers are the names in `required`, in contract order.
	RequiredMembers []string
	// Closed is `additionalProperties: false`: an undeclared member is refused, not removed.
	Closed bool
	// Items is the shape of each array item; nil means any item.
	Items *RequestShape
}

// Member is one declared member of an object shape, by its exact name.
type Member struct {
	Name  string
	Shape *RequestShape
}

// RequestBody is one operation's request body.
type RequestBody struct {
	// Key is the method in upper case, a space and the path as the contract writes it
	// ("POST /notes"): the key the handler template passes to the request check.
	Key string
	// Required is `requestBody.required`; an empty body is refused only then.
	Required bool
	// Shape is the body's shape.
	Shape *RequestShape
}

// The keys a request body's schema may use (catalogue README, RequestBodiesSupported). The keys
// that shape the value (type, properties, required, additionalProperties, items, and $ref) are
// the cases of shapeWalk.schema and shapeWalk.follow, their one owner. Every other key is refused,
// except one starting "x-". RequestShapes and the fit conditions RequestBodiesSupported (through
// the same walk) and RefusalDeclared (annotations) share them.
var (
	// requestValueKeywords limit a value; the request check leaves them to the code.
	requestValueKeywords = map[string]bool{
		"maxLength": true, "minLength": true, "pattern": true, "format": true, "maximum": true, "minimum": true,
		"exclusiveMaximum": true, "exclusiveMinimum": true, "multipleOf": true, "maxItems": true, "minItems": true,
		"uniqueItems": true, "maxProperties": true, "minProperties": true, "enum": true, "const": true,
	}
	// requestAnnotationKeywords describe a value and change nothing the request check does.
	requestAnnotationKeywords = map[string]bool{
		"title": true, "description": true, "default": true, "examples": true, "example": true,
		"deprecated": true, "readOnly": true, "writeOnly": true, "$comment": true,
	}
	// requestTypeNames are the JSON Schema type names a `type` may use.
	requestTypeNames = map[string]bool{
		"null": true, "boolean": true, "object": true, "array": true, "number": true, "string": true, "integer": true,
	}
)

// jsonMediaType is the one media type a request body may have.
const jsonMediaType = "application/json"

// RequestShapes finds every operation's request body in contract (the YAML text), in document
// order, following local $refs (a requestBody $ref to #/components/requestBodies included, every
// walk with a visited set); a $ref target is one shared *RequestShape, so a recursive schema is a
// cycle. `properties`, `required` and `additionalProperties` apply to an object value whether or
// not `type` is present. An error means the text is not one YAML document without duplicate keys,
// or it holds a request body the fit condition RequestBodiesSupported refuses; it names the
// location.
func RequestShapes(contract []byte) ([]RequestBody, error) {
	root, why := readContract(string(contract))
	if why != "" {
		return nil, errors.New(why)
	}
	bodies, problems := requestBodiesOf(root)
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return bodies, nil
}

// requestBodiesOf is RequestShapes on the contract's parsed root: the bodies, or every problem the
// walk found, each naming its location, in document order. The fit condition RequestBodiesSupported
// is these problems.
func requestBodiesOf(root *yaml.Node) ([]RequestBody, []string) {
	w := &shapeWalk{root: root, shapes: map[*yaml.Node]*RequestShape{}}
	bodies := w.bodies()
	return bodies, w.problems
}

// shapeWalk builds the shapes of one contract and collects what it refuses.
type shapeWalk struct {
	root     *yaml.Node
	shapes   map[*yaml.Node]*RequestShape // a schema node's shape, made once, so a $ref target is shared
	from     string                       // the operation being walked, as paths/<path>/<method>
	problems []string
}

// refuse records one problem at loc, naming the operation it was reached from when loc is not
// inside it.
func (w *shapeWalk) refuse(loc, format string, args ...any) {
	p := loc + ": " + fmt.Sprintf(format, args...)
	if loc != w.from && !strings.HasPrefix(loc, w.from+"/") {
		p += " (reached from " + w.from + ")"
	}
	w.problems = append(w.problems, p)
}

// bodies is every operation's request body, in document order.
func (w *shapeWalk) bodies() []RequestBody {
	ops, why := bodyOperations(w.root)
	if why != "" {
		w.problems = append(w.problems, why)
		return nil
	}
	var out []RequestBody
	for _, op := range ops {
		w.from = op.loc
		if b, ok := w.body(op.body, w.from+"/requestBody"); ok {
			b.Key = strings.ToUpper(op.method) + " " + op.path
			out = append(out, b)
		}
	}
	return out
}

// bodyOperation is one operation that has a requestBody key.
type bodyOperation struct {
	loc, method, path string     // loc is paths/<path>/<method>
	op, body          *yaml.Node // the operation and its requestBody value
}

// bodyOperations is every operation with a requestBody key, in document order, or why the
// contract's root or paths cannot hold operations. No paths is no operation.
func bodyOperations(root *yaml.Node) ([]bodyOperation, string) {
	root = deref(root)
	if root.Kind != yaml.MappingNode {
		return nil, "the contract's root is not a mapping"
	}
	paths := lookup(root, "paths")
	if paths == nil {
		return nil, ""
	}
	if paths = deref(paths); paths.Kind != yaml.MappingNode {
		return nil, "paths is not a mapping"
	}
	var out []bodyOperation
	for i := 0; i+1 < len(paths.Content); i += 2 {
		path, item := paths.Content[i].Value, deref(paths.Content[i+1])
		if item.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(item.Content); j += 2 {
			method, op := item.Content[j].Value, deref(item.Content[j+1])
			if !httpMethods[method] || op.Kind != yaml.MappingNode {
				continue
			}
			if rb := lookup(op, "requestBody"); rb != nil {
				out = append(out, bodyOperation{loc: "paths/" + path + "/" + method, method: method, path: path, op: op, body: rb})
			}
		}
	}
	return out, ""
}

// body reads one requestBody at loc, following a $ref to it.
func (w *shapeWalk) body(n *yaml.Node, loc string) (RequestBody, bool) {
	n, loc, ok := w.follow(n, loc, false)
	if !ok {
		return RequestBody{}, false
	}
	if n.Kind != yaml.MappingNode {
		w.refuse(loc, "the request body is not a mapping")
		return RequestBody{}, false
	}
	var b RequestBody
	if r := lookup(n, "required"); r != nil {
		if b.Required, ok = yamlBool(deref(r)); !ok {
			w.refuse(loc+"/required", "required is not true or false")
			return RequestBody{}, false
		}
	}
	content := lookup(n, "content")
	if content == nil || deref(content).Kind != yaml.MappingNode || len(deref(content).Content) == 0 {
		w.refuse(loc, "no content: a request body must have the media type %s", jsonMediaType)
		return RequestBody{}, false
	}
	content = deref(content)
	var others []string
	var media *yaml.Node
	for i := 0; i+1 < len(content.Content); i += 2 {
		if name := content.Content[i].Value; name == jsonMediaType {
			media = deref(content.Content[i+1])
		} else {
			others = append(others, name)
		}
	}
	if len(others) > 0 {
		w.refuse(loc, "media types other than %s: %s", jsonMediaType, strings.Join(others, ", "))
		return RequestBody{}, false
	}
	var schema *yaml.Node
	if media.Kind == yaml.MappingNode {
		schema = lookup(media, "schema")
	}
	if schema == nil {
		w.refuse(loc, "no schema")
		return RequestBody{}, false
	}
	b.Shape = w.schema(schema, loc+"/content/"+jsonMediaType+"/schema")
	return b, b.Shape != nil
}

// follow resolves n through local $refs to the node they name, with a visited set; loc is then
// the target's location. A $ref that is not local, does not resolve or loops is refused, and so,
// in a schema, is a key beside a $ref other than an annotation or an x- key.
func (w *shapeWalk) follow(n *yaml.Node, loc string, schema bool) (*yaml.Node, string, bool) {
	seen := map[*yaml.Node]bool{}
	for {
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			w.refuse(loc, "a YAML anchor or alias is not supported in a request body")
			return nil, loc, false
		}
		if n.Kind != yaml.MappingNode {
			return n, loc, true
		}
		ref := lookup(n, "$ref")
		if ref == nil {
			return n, loc, true
		}
		if seen[n] {
			w.refuse(loc, "$ref loops and never reaches a schema")
			return nil, loc, false
		}
		if schema && !w.refSiblings(n, loc) {
			return nil, loc, false
		}
		seen[n] = true
		target, at, ok := w.resolve(ref, loc)
		if !ok {
			return nil, loc, false
		}
		n, loc = target, at
	}
}

// resolve is the node a local $ref value names, and its location.
func (w *shapeWalk) resolve(ref *yaml.Node, loc string) (*yaml.Node, string, bool) {
	ref = deref(ref)
	if ref.Kind != yaml.ScalarNode || ref.ShortTag() != "!!str" || !strings.HasPrefix(ref.Value, "#/") {
		w.refuse(loc, "$ref %s is not a local reference (#/...)", describe(ref))
		return nil, loc, false
	}
	n, at, ok := localTarget(w.root, ref.Value)
	if !ok {
		w.refuse(loc, "$ref %q does not resolve in the contract", ref.Value)
		return nil, loc, false
	}
	return n, at, true
}

// localTarget is the node the local reference ref ("#/...", JSON Pointer escapes ~1 and ~0)
// names in the document at root, and its location; false when ref is not local or names nothing.
func localTarget(root *yaml.Node, ref string) (*yaml.Node, string, bool) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, "", false
	}
	n := root
	var segs []string
	for _, seg := range strings.Split(ref[2:], "/") {
		seg = strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
		segs = append(segs, seg)
		n = deref(n)
		var next *yaml.Node
		switch n.Kind {
		case yaml.MappingNode:
			next = lookup(n, seg)
		case yaml.SequenceNode:
			if i, err := strconv.Atoi(seg); err == nil && i >= 0 && i < len(n.Content) {
				next = n.Content[i]
			}
		}
		if next == nil {
			return nil, "", false
		}
		n = next
	}
	return n, strings.Join(segs, "/"), true
}

// schema is the shape of the schema at n (location loc), or nil when it is refused.
func (w *shapeWalk) schema(n *yaml.Node, loc string) *RequestShape {
	n, loc, ok := w.follow(n, loc, true)
	if !ok {
		return nil
	}
	if s, ok := w.shapes[n]; ok {
		return s
	}
	switch {
	case n.Kind == yaml.ScalarNode && n.ShortTag() == "!!bool":
		w.refuse(loc, "a boolean schema is not supported in a request body")
		return nil
	case n.Kind != yaml.MappingNode:
		w.refuse(loc, "the schema is not a mapping")
		return nil
	}
	s := &RequestShape{}
	w.shapes[n] = s // before the children, so a recursive schema meets this shape again
	var props []Member
	hasProps := false
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		key, at := k.Value, loc+"/"+k.Value
		switch {
		case k.Kind != yaml.ScalarNode || k.Anchor != "":
			w.refuse(loc, "a key that is not a plain name is not supported in a request body")
		case key == "type":
			s.Types = w.types(v, at)
		case key == "properties":
			hasProps = true
			props = w.properties(v, at)
		case key == "required":
			s.RequiredMembers = w.required(v, at)
		case key == "additionalProperties":
			if open, ok := yamlBool(v); !ok || v.Anchor != "" {
				w.refuse(at, "additionalProperties is not supported in a request body unless it is true or false")
			} else {
				s.Closed = !open
			}
		case key == "items":
			s.Items = w.schema(v, at)
		case requestValueKeywords[key], requestAnnotationKeywords[key], strings.HasPrefix(key, "x-"):
			w.anchorsIn(v, at, key)
		default:
			w.refuse(at, "%s is not supported in a request body", key)
		}
	}
	if hasProps || len(s.RequiredMembers) > 0 || s.Closed {
		s.Properties = append([]Member{}, props...)
		for _, name := range s.RequiredMembers {
			if !hasMember(s.Properties, name) {
				s.Properties = append(s.Properties, Member{Name: name, Shape: &RequestShape{}})
			}
		}
	}
	return s
}

// refSiblings refuses each key of the $ref mapping n other than $ref, an annotation or an x- key,
// and a YAML anchor or alias in it; it reports whether n passed.
func (w *shapeWalk) refSiblings(n *yaml.Node, loc string) bool {
	before := len(w.problems)
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		if k != "$ref" && !requestAnnotationKeywords[k] && !strings.HasPrefix(k, "x-") {
			w.refuse(loc, "%s beside $ref is not supported in a request body", k)
		}
	}
	w.anchorsIn(n, loc, "$ref")
	return len(w.problems) == before
}

// yamlBool is the value of a YAML boolean, in any spelling YAML reads as one (false, False, FALSE,
// ...), and whether n is one; a quoted string is not.
func yamlBool(n *yaml.Node) (bool, bool) {
	var b bool
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!bool" || n.Decode(&b) != nil {
		return false, false
	}
	return b, true
}

// anchorsIn refuses a YAML anchor or alias anywhere in the value of keyword at loc.
func (w *shapeWalk) anchorsIn(n *yaml.Node, loc, keyword string) {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		w.refuse(loc, "a YAML anchor or alias under %s is not supported in a request body", keyword)
		return
	}
	for _, c := range n.Content {
		w.anchorsIn(c, loc, keyword)
	}
}

// types reads a `type` value: one name, or a list of one name and "null".
func (w *shapeWalk) types(v *yaml.Node, loc string) []string {
	name := func(n *yaml.Node) (string, bool) {
		if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!str" || n.Anchor != "" || !requestTypeNames[n.Value] {
			return "", false
		}
		return n.Value, true
	}
	switch v.Kind {
	case yaml.ScalarNode:
		if t, ok := name(v); ok {
			return []string{t}
		}
		w.refuse(loc, "type %s is not one of the JSON types null, boolean, object, array, number, string, integer", describe(v))
		return nil
	case yaml.SequenceNode:
		var out []string
		for _, item := range v.Content {
			t, ok := name(item)
			if !ok && item.ShortTag() == "!!null" {
				w.refuse(loc, "type lists an unquoted null; write \"null\"")
				return nil
			}
			if !ok {
				w.refuse(loc, "type lists %s, which is not a JSON type written as a string", describe(item))
				return nil
			}
			out = append(out, t)
		}
		nulls := 0
		for _, t := range out {
			if t == "null" {
				nulls++
			}
		}
		if len(out) == 1 || (len(out) == 2 && nulls == 1) {
			return out
		}
		w.refuse(loc, "a type list is supported in a request body only as one type and \"null\"")
		return nil
	}
	w.refuse(loc, "type is neither a name nor a list")
	return nil
}

// properties reads the members under `properties`, in contract order.
func (w *shapeWalk) properties(v *yaml.Node, loc string) []Member {
	if v.Kind != yaml.MappingNode || v.Anchor != "" {
		w.refuse(loc, "properties is not a mapping of names to schemas")
		return nil
	}
	var out []Member
	for i := 0; i+1 < len(v.Content); i += 2 {
		k := v.Content[i]
		if k.Kind != yaml.ScalarNode {
			w.refuse(loc, "a property name that is not a plain name is not supported in a request body")
			continue
		}
		if s := w.schema(v.Content[i+1], loc+"/"+k.Value); s != nil {
			out = append(out, Member{Name: k.Value, Shape: s})
		}
	}
	return out
}

// required reads the names under `required`, in contract order.
func (w *shapeWalk) required(v *yaml.Node, loc string) []string {
	if v.Kind != yaml.SequenceNode || v.Anchor != "" {
		w.refuse(loc, "required is not a list of names")
		return nil
	}
	var out []string
	for _, n := range v.Content {
		if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!str" || n.Anchor != "" {
			w.refuse(loc, "required lists %s, which is not a name", describe(n))
			return nil
		}
		if contains(out, n.Value) {
			w.refuse(loc, "required lists %q twice", n.Value)
			return nil
		}
		out = append(out, n.Value)
	}
	return out
}

func hasMember(ms []Member, name string) bool {
	for _, m := range ms {
		if m.Name == name {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

const requestShapesHeader = "// Code generated by the generate procedure from api/openapi.yaml. DO NOT EDIT.\n\npackage gen\n"

// RenderRequestShapes is gen/request_shapes.go for bodies: the same bytes for the same bodies.
// Every shape is an unexported variable requestShape<N> (N from 1, depth first, a shape before its
// members and items, in the bodies' order; a shared shape once, at its first use), declared
// without links and linked in init(), so a recursive schema builds. Every key and name is written
// with strconv.Quote. The types it fills, requestBody, requestShape and requestMember, are the
// template's gen/requests.go.
func RenderRequestShapes(bodies []RequestBody) []byte {
	num := map[*RequestShape]int{}
	var order []*RequestShape
	var number func(s *RequestShape)
	number = func(s *RequestShape) {
		if s == nil || num[s] != 0 {
			return
		}
		order = append(order, s)
		num[s] = len(order)
		for _, m := range s.Properties {
			number(m.Shape)
		}
		number(s.Items)
	}
	for _, b := range bodies {
		number(b.Shape)
	}
	name := func(s *RequestShape) string { return "requestShape" + strconv.Itoa(num[s]) }

	var b strings.Builder
	b.WriteString(requestShapesHeader)
	b.WriteString("\nvar requestBodies = map[string]*requestBody{\n")
	for _, body := range bodies {
		var fields []string
		if body.Required {
			fields = append(fields, "Required: true")
		}
		if body.Shape != nil {
			fields = append(fields, "Shape: "+name(body.Shape))
		}
		fmt.Fprintf(&b, "%s: {%s},\n", strconv.Quote(body.Key), strings.Join(fields, ", "))
	}
	b.WriteString("}\n")
	var links []string
	for _, s := range order {
		var fields []string
		if len(s.Types) > 0 {
			fields = append(fields, "Types: "+quotedList(s.Types))
		}
		if len(s.RequiredMembers) > 0 {
			fields = append(fields, "RequiredMembers: "+quotedList(s.RequiredMembers))
		}
		if s.Closed {
			fields = append(fields, "Closed: true")
		}
		fmt.Fprintf(&b, "\nvar %s = &requestShape{%s}\n", name(s), strings.Join(fields, ", "))
		if s.Properties != nil {
			var ms []string
			for _, m := range s.Properties {
				shape := "nil"
				if m.Shape != nil {
					shape = name(m.Shape)
				}
				ms = append(ms, fmt.Sprintf("{Name: %s, Shape: %s}", strconv.Quote(m.Name), shape))
			}
			links = append(links, fmt.Sprintf("%s.Properties = []requestMember{%s}", name(s), strings.Join(ms, ", ")))
		}
		if s.Items != nil {
			links = append(links, fmt.Sprintf("%s.Items = %s", name(s), name(s.Items)))
		}
	}
	if len(links) > 0 {
		b.WriteString("\nfunc init() {\n" + strings.Join(links, "\n") + "\n}\n")
	}
	src := []byte(b.String())
	if formatted, err := format.Source(src); err == nil {
		return formatted
	}
	return src
}

// quotedList is a Go []string literal of list, each item written with strconv.Quote.
func quotedList(list []string) string {
	q := make([]string, len(list))
	for i, s := range list {
		q[i] = strconv.Quote(s)
	}
	return "[]string{" + strings.Join(q, ", ") + "}"
}

// RequestShapesGenerated passes when the project's gen/request_shapes.go equals
// RenderRequestShapes of the approved contract's request bodies; approvedContract is the
// contract's YAML text. It fails naming a missing or differing file, and fails closed, naming
// what it could not read, on a contract RequestShapes rejects.
func RequestShapesGenerated(project string, approvedContract []byte) []CheckResult {
	const check = "RequestShapesGenerated"
	bodies, err := RequestShapes(approvedContract)
	if err != nil {
		return []CheckResult{fail(check, "the approved contract's request bodies cannot be read: "+err.Error())}
	}
	got, err := fs.ReadFile(os.DirFS(project), RequestShapesFile)
	if errors.Is(err, os.ErrNotExist) {
		return []CheckResult{fail(check, RequestShapesFile+" is missing; the generate procedure writes it from the approved contract")}
	}
	if err != nil {
		return []CheckResult{fail(check, RequestShapesFile+" cannot be read: "+err.Error())}
	}
	if !bytes.Equal(got, RenderRequestShapes(bodies)) {
		return []CheckResult{fail(check, fmt.Sprintf("%s differs from the %d request bodies of the approved contract; it is generated, never edited", RequestShapesFile, len(bodies)))}
	}
	return []CheckResult{pass(check, fmt.Sprintf("%s equals the %d request bodies of the approved contract", RequestShapesFile, len(bodies)))}
}
