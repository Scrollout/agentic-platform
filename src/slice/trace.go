package slice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	gherkin "github.com/cucumber/gherkin/go/v42"
	messages "github.com/cucumber/messages/go/v34"
	"go.yaml.in/yaml/v3"
)

// Finding is one broken link between the IDs of a slice: the part the developer must fix
// ("goals", "rules" or "contract"; "" when the document itself cannot be read), the offending ID,
// and a reason that names it. Syntax marks a finding that the examples or the contract text does
// not parse at all, so the links in it were not followed; its ID names the field ("examples",
// "contract").
type Finding struct {
	Part, ID, Reason string
	Syntax           bool
}

// The parts of a slice or proposal that Trace reads. Their shape is the schemas' rule
// (CheckSlice, CheckProposal); Trace only follows the IDs.
type traceDoc struct {
	Goals []struct {
		ID string `json:"id"`
	} `json:"goals"`
	Terms []struct {
		ID string `json:"id"`
	} `json:"terms"`
	Rules []struct {
		ID    string   `json:"id"`
		Goal  string   `json:"goal"`
		Terms []string `json:"terms"`
	} `json:"rules"`
	Examples string `json:"examples"`
	Contract string `json:"contract"`
}

// Trace checks that every link between the IDs of a slice or slice proposal (given as JSON)
// holds: each goal has a rule, each rule names an existing goal and existing terms and has a
// scenario, and each operation of the contract names existing rules. It returns every broken
// link, or nil when all hold.
//
// A scenario counts for a rule only when it sits under the "Rule:" block named with the rule's ID
// and carries the tag @<rule ID>.
func Trace(doc []byte) []Finding {
	var d traceDoc
	if err := json.Unmarshal(doc, &d); err != nil {
		return []Finding{{Reason: "the document is not valid JSON: " + err.Error()}}
	}
	var out []Finding

	goals, terms, rules := set(), set(), set()
	for _, g := range d.Goals {
		goals[g.ID] = true
	}
	for _, t := range d.Terms {
		terms[t.ID] = true
	}
	served := set()
	for _, r := range d.Rules {
		rules[r.ID] = true
		served[r.Goal] = true
	}
	for _, g := range d.Goals {
		if !served[g.ID] {
			out = append(out, Finding{Part: "goals", ID: g.ID, Reason: fmt.Sprintf("goal %s has no rule", g.ID)})
		}
	}

	refs, err := scenarioRefs(d.Examples)
	if err != nil {
		out = append(out, Finding{Part: "rules", ID: "examples", Reason: "the examples are not valid Gherkin: " + err.Error(), Syntax: true})
	}
	scenarios, unknown := exampleLinks(refs, rules)
	out = append(out, unknown...)
	for _, r := range d.Rules {
		if !goals[r.Goal] {
			out = append(out, Finding{Part: "rules", ID: r.ID, Reason: fmt.Sprintf("rule %s names goal %s, which does not exist", r.ID, r.Goal)})
		}
		for _, t := range r.Terms {
			if !terms[t] {
				out = append(out, Finding{Part: "rules", ID: t, Reason: fmt.Sprintf("rule %s uses term %s, which is not in terms", r.ID, t)})
			}
		}
		if err == nil && !scenarios[r.ID] {
			out = append(out, Finding{Part: "rules", ID: r.ID, Reason: fmt.Sprintf("rule %s has no scenario tagged @%s under a \"Rule: %s\" block", r.ID, r.ID, r.ID)})
		}
	}

	out = append(out, contractLinks(d.Contract, rules)...)
	return out
}

func set() map[string]bool { return map[string]bool{} }

// scenarioRef is what Trace reads of one scenario: the name of the "Rule:" block it sits under
// ("" for none), its name, and the IDs of its @R-<n> and @E-<n> tags.
type scenarioRef struct {
	block, name string
	rules, exs  []string
}

// scenarioRefs returns every scenario of Gherkin examples, in document order. An error means the
// text is not Gherkin.
func scenarioRefs(text string) ([]scenarioRef, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	gd, err := gherkin.ParseGherkinDocument(strings.NewReader(text), (&messages.Incrementing{}).NewId)
	if err != nil {
		return nil, err
	}
	if gd.Feature == nil {
		return nil, nil
	}
	var out []scenarioRef
	add := func(sc *messages.Scenario, block string) {
		r := scenarioRef{block: block, name: strings.TrimSpace(sc.Name)}
		for _, t := range sc.Tags {
			if m := ruleTag.FindStringSubmatch(t.Name); m != nil {
				r.rules = append(r.rules, m[1])
			} else if m := exampleTag.FindStringSubmatch(t.Name); m != nil {
				r.exs = append(r.exs, m[1])
			}
		}
		out = append(out, r)
	}
	for _, fc := range gd.Feature.Children {
		switch {
		case fc.Scenario != nil:
			add(fc.Scenario, "")
		case fc.Rule != nil:
			for _, rc := range fc.Rule.Children {
				if rc.Scenario != nil {
					add(rc.Scenario, strings.TrimSpace(fc.Rule.Name))
				}
			}
		}
	}
	return out, nil
}

// ruleID matches a rule ID as a "Rule:" block names it.
var ruleID = regexp.MustCompile(`^R-[0-9]+$`)

// exampleLinks returns the rule IDs that have a scenario under the "Rule:" block of that name,
// tagged with the same ID, and a finding for each scenario whose block or @R-<n> tag names a rule
// that is not in rules, naming the scenario's example and the rule.
func exampleLinks(scs []scenarioRef, rules map[string]bool) (map[string]bool, []Finding) {
	has := set()
	var out []Finding
	for _, sc := range scs {
		for _, r := range sc.rules {
			if r == sc.block {
				has[r] = true
			}
		}
		named := sc.rules
		if ruleID.MatchString(sc.block) && !slices.Contains(named, sc.block) {
			named = append([]string{sc.block}, named...)
		}
		what := fmt.Sprintf("scenario %q", sc.name)
		id := ""
		if len(sc.exs) > 0 {
			what = fmt.Sprintf("example %s (%s)", sc.exs[0], what)
			id = sc.exs[0]
		}
		for _, r := range named {
			if !rules[r] {
				fid := id
				if fid == "" {
					fid = r
				}
				out = append(out, Finding{Part: "rules", ID: fid, Reason: fmt.Sprintf("%s serves rule %s, which is not in rules", what, r)})
			}
		}
	}
	return has, out
}

// The fields of an OpenAPI path item that hold operations.
var methods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// contractLinks checks the operations of the OpenAPI document (under paths and webhooks), in
// document order: each has a non-empty operationId and a non-empty x-rules list of scalar rule IDs,
// each naming a rule in rules. It fails closed: a shape it cannot follow (a root, paths, path item
// or operation that is not a mapping, YAML aliases or merge keys) is a finding, never skipped.
func contractLinks(text string, rules map[string]bool) []Finding {
	fail := func(id, format string, a ...any) Finding {
		return Finding{Part: "contract", ID: id, Reason: fmt.Sprintf(format, a...)}
	}
	var root yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader([]byte(text))).Decode(&root); err != nil {
		f := fail("contract", "the contract cannot be read: %v", err)
		f.Syntax = true
		return []Finding{f}
	}
	if len(root.Content) == 0 {
		return nil
	}
	if out := aliases(&root); len(out) > 0 {
		return out
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return []Finding{fail("contract", "the contract is not a mapping (line %d); it must be an OpenAPI document", doc.Line)}
	}
	var out []Finding
	for _, section := range []string{"paths", "webhooks"} {
		items := field(doc, section)
		if items == nil {
			continue
		}
		if items.Kind != yaml.MappingNode {
			out = append(out, fail(section, "the contract's %s is not a mapping (line %d)", section, items.Line))
			continue
		}
		for i := 0; i+1 < len(items.Content); i += 2 {
			path, item := items.Content[i].Value, items.Content[i+1]
			if item.Kind != yaml.MappingNode {
				out = append(out, fail(path, "path item %s is not a mapping (line %d)", path, item.Line))
				continue
			}
			for _, m := range methods {
				op := field(item, m)
				if op == nil {
					continue
				}
				name := strings.ToUpper(m) + " " + path
				if op.Kind != yaml.MappingNode {
					out = append(out, fail(name, "operation %s is not a mapping (line %d)", name, op.Line))
					continue
				}
				if id := field(op, "operationId"); id != nil && id.Kind == yaml.ScalarNode && id.Value != "" {
					name = id.Value
				} else {
					out = append(out, fail(name, "operation %s has no operationId", name))
				}
				xr := field(op, "x-rules")
				switch {
				case xr == nil:
					out = append(out, fail(name, "operation %s has no x-rules; list the rules it serves", name))
					continue
				case xr.Kind != yaml.SequenceNode:
					out = append(out, fail(name, "operation %s has an x-rules that is not a list (line %d)", name, xr.Line))
					continue
				case len(xr.Content) == 0:
					out = append(out, fail(name, "operation %s has an empty x-rules; list the rules it serves", name))
					continue
				}
				for _, n := range xr.Content {
					switch {
					case n.Kind != yaml.ScalarNode:
						out = append(out, fail(name, "operation %s has an x-rules entry that is not a rule ID (line %d)", name, n.Line))
					case !rules[n.Value]:
						out = append(out, fail(n.Value, "operation %s names rule %s in x-rules, which does not exist", name, n.Value))
					}
				}
			}
		}
	}
	return out
}

// aliases returns a finding for each YAML alias and merge key in the tree under n: they let one
// node stand for another, which the checks do not follow.
func aliases(n *yaml.Node) []Finding {
	var out []Finding
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		switch {
		case n.Kind == yaml.AliasNode:
			out = append(out, Finding{Part: "contract", ID: "*" + n.Value, Reason: fmt.Sprintf("the contract uses YAML alias *%s (line %d); write the value out", n.Value, n.Line)})
			return
		case n.Kind == yaml.ScalarNode && n.Tag == "!!merge":
			out = append(out, Finding{Part: "contract", ID: "<<", Reason: fmt.Sprintf("the contract uses a YAML merge key (line %d); write the fields out", n.Line)})
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(n)
	return out
}

// field returns the value of key in the mapping n, or nil.
func field(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}
