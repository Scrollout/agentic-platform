// Package checks holds the engine's deterministic checks: rules on a slice or a step's output that
// decide acceptance without a model (constitution XX).
package checks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// CheckResult is one check's verdict on one part of a slice. Its JSON form is the API's
// check_result. Reason is never empty: it says what failed, or what the check confirmed.
type CheckResult struct {
	Part   string `json:"part"`
	Check  string `json:"check"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason"`
	// Unreadable marks a failure because the slice's contract text cannot be read as exactly one
	// YAML document without duplicate keys: the contract was not checked at all, so whoever wrote
	// it may be asked for a readable one. It is not part of check_result.
	Unreadable bool `json:"-"`
}

// Fit evaluates every condition the pattern lists, in the pattern's order, against the slice.
//
// pattern is a pattern.yaml; slice is the JSON of a slice's parts, of which only "contract" (the
// OpenAPI text) and "rules" are read. It returns one result per listed condition, and the slice
// fits when every result passed. The conditions and their reasons are defined in the pattern's
// README (src/catalogue/api-feature-go/README.md).
//
// It fails closed: a condition it does not implement fails under its own name; a pattern whose
// condition list cannot be read yields one failing PatternFit result; a slice that is not JSON
// fails every condition; a contract that is not exactly one YAML document without duplicate keys
// fails every contract condition, and those results are marked Unreadable. It reads no clock,
// network or model.
func Fit(pattern []byte, slice []byte) []CheckResult {
	var p struct {
		Conditions []string `yaml:"conditions"`
	}
	if err := yaml.Unmarshal(pattern, &p); err != nil {
		return []CheckResult{{Part: "contract", Check: "PatternFit", Reason: "the pattern cannot be read: " + err.Error()}}
	}
	if len(p.Conditions) == 0 {
		return []CheckResult{{Part: "contract", Check: "PatternFit", Reason: "the pattern lists no conditions, so nothing can show the slice fits"}}
	}

	in := readSlice(slice)
	out := make([]CheckResult, 0, len(p.Conditions))
	for _, name := range p.Conditions {
		c, ok := fitConditions[name]
		if !ok {
			out = append(out, CheckResult{Part: "contract", Check: name,
				Reason: fmt.Sprintf("condition %s is not implemented by the pattern-fit check, so it cannot pass", name)})
			continue
		}
		passed, reason := c.eval(in)
		out = append(out, CheckResult{Part: c.part, Check: name, Passed: passed, Reason: reason,
			Unreadable: c.part == "contract" && in.contractUnreadable})
	}
	return out
}

// fitInput is the slice as the conditions see it. An *Err field set means that part could not be
// read, and every condition on it fails with that reason.
type fitInput struct {
	contract    *yaml.Node // root node of the contract's one document
	contractErr string
	// contractUnreadable is set when the contract text is there but is not exactly one YAML
	// document without duplicate keys (readContract failed).
	contractUnreadable bool
	rules              []fitRule
	rulesErr           string
}

type fitRule struct {
	ID     string  `json:"id"`
	System *string `json:"system"`
}

type fitCondition struct {
	part string
	eval func(fitInput) (bool, string)
}

// fitConditions maps each condition name the check implements to its part and its rule.
var fitConditions = map[string]fitCondition{
	"ContractIsOpenApi31": {"contract", onContract(contractIsOpenApi31)},
	"OnlyHttpOperations":  {"contract", onContract(onlyHttpOperations)},
	"ContractRefsLocal":   {"contract", onContract(contractRefsLocal)},
	"RulesNameOneSystem":  {"rules", rulesNameOneSystem},
}

func onContract(f func(root *yaml.Node) (bool, string)) func(fitInput) (bool, string) {
	return func(in fitInput) (bool, string) {
		if in.contractErr != "" {
			return false, in.contractErr
		}
		return f(in.contract)
	}
}

func readSlice(b []byte) fitInput {
	var s struct {
		Contract *string         `json:"contract"`
		Rules    json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		msg := "the slice is not valid JSON: " + err.Error()
		return fitInput{contractErr: msg, rulesErr: msg}
	}
	var in fitInput
	if s.Contract == nil {
		in.contractErr = "the slice has no contract"
	} else {
		in.contract, in.contractErr = readContract(*s.Contract)
		in.contractUnreadable = in.contractErr != ""
	}
	if len(s.Rules) == 0 || string(s.Rules) == "null" {
		in.rulesErr = "the slice has no rules"
	} else if err := json.Unmarshal(s.Rules, &in.rules); err != nil {
		in.rulesErr = "the slice's rules cannot be read: " + err.Error()
	}
	return in
}

// readContract parses the contract as exactly one YAML document. Decoding it once into a generic
// value rejects duplicate keys, aliases that contain themselves and excessive aliasing, so the
// node walks below are bounded.
func readContract(text string) (*yaml.Node, string) {
	dec := yaml.NewDecoder(strings.NewReader(text))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "the contract is empty"
		}
		return nil, "the contract is not valid YAML: " + err.Error()
	}
	var next yaml.Node
	if err := dec.Decode(&next); !errors.Is(err, io.EOF) {
		return nil, "the contract holds more than one YAML document"
	}
	var v any
	if err := doc.Decode(&v); err != nil {
		return nil, "the contract cannot be read as one YAML document: " + err.Error()
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, "the contract is not one YAML document"
	}
	return doc.Content[0], ""
}

var openApi31 = regexp.MustCompile(`^3\.1\.[0-9]+$`)

func contractIsOpenApi31(root *yaml.Node) (bool, string) {
	root = deref(root)
	if root.Kind != yaml.MappingNode {
		return false, "the contract's root is not a mapping, so it has no openapi value"
	}
	v := lookup(root, "openapi")
	if v == nil {
		return false, "the contract's openapi value is missing; want a string 3.1.x"
	}
	if v.Kind != yaml.ScalarNode || v.ShortTag() != "!!str" {
		return false, fmt.Sprintf("the contract's openapi value is not a string (found %s %q); want 3.1.x", v.ShortTag(), v.Value)
	}
	if !openApi31.MatchString(v.Value) {
		return false, fmt.Sprintf("the contract's openapi value is %q; want 3.1.x", v.Value)
	}
	return true, fmt.Sprintf("the contract is OpenAPI %s", v.Value)
}

var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

var pathItemFields = map[string]bool{"summary": true, "description": true, "servers": true, "parameters": true}

func onlyHttpOperations(root *yaml.Node) (bool, string) {
	root = deref(root)
	if root.Kind != yaml.MappingNode {
		return false, "the contract's root is not a mapping, so it has no paths"
	}
	var bad []string
	ops, noOps := 0, false
	paths := lookup(root, "paths")
	switch {
	case paths == nil:
		bad = append(bad, "paths (missing)")
	case paths.Kind != yaml.MappingNode:
		bad = append(bad, "paths (not a mapping)")
	default:
		for i := 0; i+1 < len(paths.Content); i += 2 {
			k, item := paths.Content[i], deref(paths.Content[i+1])
			loc := "paths/" + k.Value
			if item.Kind != yaml.MappingNode {
				bad = append(bad, loc+" (not a mapping)")
				continue
			}
			for j := 0; j+1 < len(item.Content); j += 2 {
				key, val := item.Content[j], deref(item.Content[j+1])
				kloc := loc + "/" + key.Value
				switch {
				case key.Kind != yaml.ScalarNode:
					bad = append(bad, loc+" (a key that is not a name)")
				case httpMethods[key.Value]:
					ops++
					if val.Kind != yaml.MappingNode {
						bad = append(bad, kloc+" (not a mapping)")
					} else if cb := lookup(val, "callbacks"); cb != nil && !empty(cb) {
						bad = append(bad, kloc+"/callbacks")
					}
				case pathItemFields[key.Value], strings.HasPrefix(key.Value, "x-"):
				default:
					// $ref and any key this version does not know.
					bad = append(bad, kloc)
				}
			}
		}
		noOps = ops == 0
	}
	bad = append(bad, nonEmptyEntries(lookup(root, "webhooks"), "webhooks")...)
	if comp := lookup(root, "components"); comp != nil && deref(comp).Kind == yaml.MappingNode {
		bad = append(bad, nonEmptyEntries(lookup(deref(comp), "pathItems"), "components/pathItems")...)
	}
	switch {
	case noOps && len(bad) > 0:
		return false, "the contract has no HTTP operation, and has more than HTTP operations a client calls: " + strings.Join(bad, ", ")
	case noOps:
		return false, "the contract has no HTTP operation (its paths are empty)"
	case len(bad) > 0:
		return false, "the contract has more than HTTP operations a client calls: " + strings.Join(bad, ", ")
	}
	return true, fmt.Sprintf("the contract has %d HTTP operations and no webhooks, callbacks or other path item keys", ops)
}

// nonEmptyEntries names each entry of a mapping at loc, or loc itself when it is a non-empty
// value of another kind. An absent or empty value names nothing.
func nonEmptyEntries(n *yaml.Node, loc string) []string {
	if n == nil || empty(n) {
		return nil
	}
	n = deref(n)
	if n.Kind != yaml.MappingNode {
		return []string{loc}
	}
	var out []string
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, loc+"/"+n.Content[i].Value)
	}
	return out
}

func contractRefsLocal(root *yaml.Node) (bool, string) {
	var bad []string
	var walk func(n *yaml.Node, loc string)
	walk = func(n *yaml.Node, loc string) {
		n = deref(n)
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				kloc := join(loc, k.Value)
				if k.Kind == yaml.ScalarNode && k.Value == "$ref" {
					dv := deref(v)
					if dv.Kind != yaml.ScalarNode || dv.ShortTag() != "!!str" || !strings.HasPrefix(dv.Value, "#/") {
						bad = append(bad, fmt.Sprintf("%s (%s)", kloc, describe(dv)))
					}
					continue
				}
				walk(v, kloc)
			}
		case yaml.SequenceNode:
			for i, c := range n.Content {
				walk(c, join(loc, strconv.Itoa(i)))
			}
		}
	}
	walk(root, "")
	if len(bad) > 0 {
		return false, "the contract has a $ref outside the document: " + strings.Join(bad, ", ")
	}
	return true, "every $ref in the contract points inside the document"
}

func rulesNameOneSystem(in fitInput) (bool, string) {
	if in.rulesErr != "" {
		return false, in.rulesErr
	}
	if len(in.rules) == 0 {
		return false, "the slice has no rules, so it names no system"
	}
	var names []string // distinct names, in order of first use
	ids := map[string][]string{}
	var missing []string
	for _, r := range in.rules {
		if r.System == nil {
			missing = append(missing, r.ID)
			continue
		}
		if _, seen := ids[*r.System]; !seen {
			names = append(names, *r.System)
		}
		ids[*r.System] = append(ids[*r.System], r.ID)
	}
	if len(missing) == 0 && len(names) == 1 {
		return true, fmt.Sprintf("all %d rules name the system %q", len(in.rules), names[0])
	}
	var parts []string
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%q (%s)", n, strings.Join(ids[n], ", ")))
	}
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("no system (%s)", strings.Join(missing, ", ")))
	}
	return false, "the rules do not name one system: " + strings.Join(parts, "; ")
}

// lookup returns the value of key in mapping m, or nil.
func lookup(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k := m.Content[i]; k.Kind == yaml.ScalarNode && k.Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// deref follows aliases to the node they name; readContract has already refused cycles.
func deref(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

// empty is true for null, an empty mapping or an empty sequence.
func empty(n *yaml.Node) bool {
	n = deref(n)
	switch n.Kind {
	case yaml.MappingNode, yaml.SequenceNode:
		return len(n.Content) == 0
	case yaml.ScalarNode:
		return n.ShortTag() == "!!null"
	}
	return false
}

func join(loc, seg string) string {
	if loc == "" {
		return seg
	}
	return loc + "/" + seg
}

func describe(n *yaml.Node) string {
	if n.Kind == yaml.ScalarNode {
		return strconv.Quote(n.Value)
	}
	return "not a string"
}
