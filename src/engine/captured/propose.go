package captured

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/agentic-platform/engine/checks"
)

// contractText is the contract's YAML text that the copy's contract part holds.
func contractText(src Copy) (string, error) {
	var part struct {
		Contract *string `json:"contract"`
	}
	if err := json.Unmarshal(src.Contract, &part); err != nil || part.Contract == nil {
		return "", errors.New("captured: the slice copy's contract part is not {contract: text}")
	}
	return *part.Contract, nil
}

// contractNames gives the names the contract states, in byte order: each word, in its lowercase
// form, of every schema property name, operation ID and schema name, split at case changes and at
// characters other than letters and digits; only words of a name's form.
func contractNames(contract string) ([]string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(contract), &root); err != nil {
		return nil, fmt.Errorf("captured: the slice's contract is unreadable: %w", err)
	}
	seen, components := map[string]bool{}, componentsOf(&root)
	add := func(s string) {
		for _, w := range words(s) {
			if valueRefusal(KindName, w) == "" {
				seen[w] = true
			}
		}
	}
	var walk func(n *yaml.Node, schemas bool)
	walk = func(n *yaml.Node, schemas bool) {
		switch n.Kind {
		case yaml.DocumentNode, yaml.SequenceNode:
			for _, c := range n.Content {
				walk(c, false)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				if k.Kind != yaml.ScalarNode {
					continue
				}
				if schemas { // the keys of components/schemas and of properties are names
					add(k.Value)
				}
				if k.Value == "operationId" && v.Kind == yaml.ScalarNode {
					add(v.Value)
					continue
				}
				walk(v, k.Value == "properties" || (k.Value == "schemas" && n == components))
			}
		}
	}
	walk(&root, false)
	out := make([]string, 0, len(seen))
	for w := range seen {
		out = append(out, w)
	}
	sort.Strings(out)
	return out, nil
}

// componentsOf is the contract's top-level `components` mapping, or nil.
func componentsOf(root *yaml.Node) *yaml.Node {
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	top := root.Content[0]
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == "components" && top.Content[i+1].Kind == yaml.MappingNode {
			return top.Content[i+1]
		}
	}
	return nil
}

// words splits s at case changes (`createNote`, `HTTPServer`) and at characters other than
// letters and digits, each word in lowercase.
func words(s string) []string {
	var out []string
	rs := []rune(s)
	start := -1
	for i, r := range rs {
		alnum := unicode.IsLetter(r) || unicode.IsDigit(r)
		if !alnum {
			if start >= 0 {
				out = append(out, strings.ToLower(string(rs[start:i])))
				start = -1
			}
			continue
		}
		if start >= 0 && unicode.IsUpper(r) {
			prev := rs[i-1]
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if !unicode.IsUpper(prev) || nextLower {
				out = append(out, strings.ToLower(string(rs[start:i])))
				start = i
			}
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, strings.ToLower(string(rs[start:])))
	}
	return out
}

// declaredName is a parameter name for the declared value at a JSON Pointer: the pointer's
// named segments as words joined by `-` (`/components/schemas/NewNote/properties/text/maxLength`
// gives `new-note-text-max-length`), at most ParameterNameMaxChars characters.
func declaredName(pointer string) string {
	skip := map[string]bool{"components": true, "schemas": true, "properties": true, "paths": true, "content": true, "schema": true}
	var ws []string
	for _, seg := range strings.Split(pointer, "/") {
		if skip[seg] {
			continue
		}
		for _, w := range words(seg) {
			if w = strings.Map(func(r rune) rune {
				if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
					return r
				}
				return -1
			}, w); w != "" {
				ws = append(ws, w)
			}
		}
	}
	name := strings.Join(ws, "-")
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		name = "value-" + name
	}
	name = strings.TrimRight(name, "-")
	if len(name) > ParameterNameMaxChars {
		name = strings.TrimRight(name[:ParameterNameMaxChars], "-")
	}
	return name
}

// Propose gives the parameter candidates of a done run's slice copy and template: each of the
// slice's names (schema property names, and the words of operation IDs and schema names split at
// case changes) and each value its contract declares, as a parameter with every place it occurs,
// longest first and never overlapping. No status code is proposed.
//
// A candidate is left out when its value is not of its kind's form, equals an earlier
// candidate's in a case form, or (with every other candidate's places taken first) has no place
// in the slice or more than PlacesMax places. Names come first in byte order, then declared values
// in the contract's order. An unreadable contract part is an error.
func Propose(src Copy, template Template) ([]Parameter, error) {
	text, err := contractText(src)
	if err != nil {
		return nil, err
	}
	names, err := contractNames(text)
	if err != nil {
		return nil, err
	}
	declared, err := checks.DeclaredValues([]byte(text), nil)
	if err != nil {
		return nil, fmt.Errorf("captured: the slice's declared values cannot be read: %w", err)
	}
	var cands []Parameter
	taken, named := map[string]bool{}, map[string]bool{}
	addCand := func(p Parameter) {
		if valueRefusal(p.Kind, p.Value) != "" || taken[strings.ToLower(p.Value)] {
			return
		}
		base := p.Name
		for n := 2; named[p.Name]; n++ {
			suffix := fmt.Sprintf("-%d", n)
			p.Name = strings.TrimRight(base[:min(len(base), ParameterNameMaxChars-len(suffix))], "-") + suffix
		}
		taken[strings.ToLower(p.Value)], named[p.Name] = true, true
		cands = append(cands, p)
	}
	for _, w := range names {
		addCand(Parameter{Name: w, Kind: KindName, Value: w, Source: SourceName, Origin: OriginProposed})
	}
	for _, d := range declared {
		var kind Kind
		switch d.Kind {
		case "number":
			kind = KindNumber
		case "string":
			kind = KindString
		default:
			continue
		}
		addCand(Parameter{Name: declaredName(d.Pointer), Kind: kind, Value: d.Text, Source: SourceDeclaredValue, Origin: OriginProposed})
	}
	// A candidate taken whole by longer ones, or over the places limit, is dropped, and the
	// others' places found again without it.
	for {
		kinds, values := kindsValues(cands)
		places, inSlice := locate(src, template, kinds, values)
		kept := cands[:0:0]
		for i := range cands {
			if inSlice[i] > 0 && len(places[i]) <= PlacesMax {
				c := cands[i]
				c.Places = places[i]
				kept = append(kept, c)
			}
		}
		if len(kept) == len(cands) {
			return kept, nil
		}
		cands = kept
	}
}

func kindsValues(ps []Parameter) ([]Kind, []string) {
	kinds, values := make([]Kind, len(ps)), make([]string, len(ps))
	for i, p := range ps {
		kinds[i], values[i] = p.Kind, p.Value
	}
	return kinds, values
}

// checkSet judges parameters without the slice: their count, names, each value's form and no two
// equal in a case form. A refusal is a *Refused.
func checkSet(parameters []Parameter) error {
	if len(parameters) > ParametersMax {
		return &Refused{Parameter: parameters[ParametersMax].Name,
			Detail: fmt.Sprintf("%d parameters, more than %d", len(parameters), ParametersMax)}
	}
	byValue, byName := map[string]string{}, map[string]bool{}
	for _, p := range parameters {
		if len(p.Name) > ParameterNameMaxChars || !paramName.MatchString(p.Name) {
			return &Refused{Parameter: p.Name, Detail: fmt.Sprintf("the name is not %s of at most %d characters", ParameterNamePattern, ParameterNameMaxChars)}
		}
		if byName[p.Name] {
			return &Refused{Parameter: p.Name, Detail: "two parameters have this name"}
		}
		byName[p.Name] = true
		if why := valueRefusal(p.Kind, p.Value); why != "" {
			return &Refused{Parameter: p.Name, Detail: why}
		}
		low := strings.ToLower(p.Value)
		if other, dup := byValue[low]; dup {
			return &Refused{Parameter: p.Name, Detail: fmt.Sprintf("its value %q equals the value of %s in a case form", p.Value, other)}
		}
		byValue[low] = p.Name
	}
	return nil
}

// Check judges a captured pattern's parameters against its copy and template before they are
// kept: each value of its kind's form, no two equal in any case form, each occurring in src,
// at most ParametersMax parameters and PlacesMax places each. A refusal is a *Refused.
func Check(src Copy, template Template, parameters []Parameter) error {
	_, err := Locate(src, template, parameters)
	return err
}

// Locate gives parameters with Places set to every place each value occurs in src and template,
// found as Propose and Fill find them, once Check's rules pass; otherwise the *Refused that Check
// gives.
func Locate(src Copy, template Template, parameters []Parameter) ([]Parameter, error) {
	if err := checkSet(parameters); err != nil {
		return nil, err
	}
	kinds, values := kindsValues(parameters)
	places, inSlice := locate(src, template, kinds, values)
	out := make([]Parameter, len(parameters))
	for i, p := range parameters {
		if inSlice[i] == 0 {
			return nil, &Refused{Parameter: p.Name, Detail: fmt.Sprintf("its value %q does not occur in the slice", p.Value)}
		}
		if len(places[i]) > PlacesMax {
			return nil, &Refused{Parameter: p.Name, Detail: fmt.Sprintf("its value %q has %d places, more than %d", p.Value, len(places[i]), PlacesMax)}
		}
		p.Places = places[i]
		out[i] = p
	}
	return out, nil
}
