package slice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	gherkin "github.com/cucumber/gherkin/go/v42"
	messages "github.com/cucumber/messages/go/v34"
	"go.yaml.in/yaml/v3"
)

// Finding is one broken link between the IDs of a slice: the part the developer must fix
// ("goals", "rules" or "contract"; "" when the document itself cannot be read), the offending ID,
// and a reason that names it.
type Finding struct{ Part, ID, Reason string }

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
			out = append(out, Finding{"goals", g.ID, fmt.Sprintf("goal %s has no rule", g.ID)})
		}
	}

	scenarios, err := scenariosByRule(d.Examples)
	if err != nil {
		out = append(out, Finding{Part: "rules", Reason: "the examples are not valid Gherkin: " + err.Error()})
	}
	for _, r := range d.Rules {
		if !goals[r.Goal] {
			out = append(out, Finding{"rules", r.ID, fmt.Sprintf("rule %s names goal %s, which does not exist", r.ID, r.Goal)})
		}
		for _, t := range r.Terms {
			if !terms[t] {
				out = append(out, Finding{"rules", t, fmt.Sprintf("rule %s uses term %s, which is not in terms", r.ID, t)})
			}
		}
		if err == nil && !scenarios[r.ID] {
			out = append(out, Finding{"rules", r.ID, fmt.Sprintf("rule %s has no scenario tagged @%s under a \"Rule: %s\" block", r.ID, r.ID, r.ID)})
		}
	}

	ops, err := operationRules(d.Contract)
	if err != nil {
		return append(out, Finding{Part: "contract", Reason: "the contract cannot be read: " + err.Error()})
	}
	for _, op := range ops {
		for _, id := range op.rules {
			if !rules[id] {
				out = append(out, Finding{"contract", id, fmt.Sprintf("operation %s names rule %s in x-rules, which does not exist", op.name, id)})
			}
		}
	}
	return out
}

func set() map[string]bool { return map[string]bool{} }

// scenariosByRule returns the rule IDs that have at least one scenario under the Rule: block of
// that name, tagged with the same ID.
func scenariosByRule(text string) (map[string]bool, error) {
	has := set()
	if strings.TrimSpace(text) == "" {
		return has, nil
	}
	gd, err := gherkin.ParseGherkinDocument(strings.NewReader(text), (&messages.Incrementing{}).NewId)
	if err != nil {
		return nil, err
	}
	if gd.Feature == nil {
		return has, nil
	}
	for _, fc := range gd.Feature.Children {
		if fc.Rule == nil {
			continue
		}
		id := strings.TrimSpace(fc.Rule.Name)
		for _, rc := range fc.Rule.Children {
			if rc.Scenario == nil {
				continue
			}
			for _, tag := range rc.Scenario.Tags {
				if tag.Name == "@"+id {
					has[id] = true
				}
			}
		}
	}
	return has, nil
}

type operation struct {
	name  string
	rules []string
}

// The fields of an OpenAPI path item that hold operations.
var methods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// operationRules returns each operation of the OpenAPI document (under paths and webhooks), in
// document order, with the rule IDs of its x-rules.
func operationRules(text string) ([]operation, error) {
	var root yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader([]byte(text))).Decode(&root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return nil, nil
	}
	var ops []operation
	for _, section := range []string{"paths", "webhooks"} {
		items := field(root.Content[0], section)
		if items == nil || items.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(items.Content); i += 2 {
			for _, m := range methods {
				op := field(items.Content[i+1], m)
				if op == nil {
					continue
				}
				name := strings.ToUpper(m) + " " + items.Content[i].Value
				if id := field(op, "operationId"); id != nil && id.Value != "" {
					name = id.Value
				}
				var ids []string
				if xr := field(op, "x-rules"); xr != nil && xr.Kind == yaml.SequenceNode {
					for _, n := range xr.Content {
						ids = append(ids, n.Value)
					}
				}
				ops = append(ops, operation{name, ids})
			}
		}
	}
	return ops, nil
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
