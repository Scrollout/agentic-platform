package slices

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"

	gherkin "github.com/cucumber/gherkin/go/v42"
	messages "github.com/cucumber/messages/go/v34"
	"go.yaml.in/yaml/v3"
)

// keptFaults returns what a revision loses of the current proposal (data-model "Kept IDs"), one
// fault per item, in the current proposal's order of goals, terms, rules, examples, then
// operations: each goal, term, rule, example or operation ID the revision lacks; each kept rule
// that now serves another goal, when the current proposal has the goal it served; and each kept
// example that now sits under another rule. Examples or a contract that do not parse, in either
// proposal, are not compared. Each fault names IDs only.
func keptFaults(current Proposal, revised Slice) []string {
	var out []string
	drop := func(id string) { out = append(out, "the revision drops "+id+" of the current proposal") }
	missing := func(ids, has []string) {
		for _, id := range ids {
			if !slices.Contains(has, id) {
				drop(id)
			}
		}
	}

	goalIDs := uniqueIDs(current.Goals, func(g Goal) string { return g.ID })
	missing(goalIDs, uniqueIDs(revised.Goals, func(g Goal) string { return g.ID }))
	missing(uniqueIDs(current.Terms, func(t Term) string { return t.ID }),
		uniqueIDs(revised.Terms, func(t Term) string { return t.ID }))

	for _, r := range firstOfEach(current.Rules, func(r Rule) string { return r.ID }) {
		i := slices.IndexFunc(revised.Rules, func(n Rule) bool { return n.ID == r.ID })
		switch {
		case i < 0:
			drop(r.ID)
		case revised.Rules[i].Goal != r.Goal && slices.Contains(goalIDs, r.Goal):
			out = append(out, fmt.Sprintf("%s served %s and now serves %s", r.ID, r.Goal, revised.Rules[i].Goal))
		}
	}

	if was, ok := exampleRules(current.Examples); ok {
		if now, ok := exampleRules(revised.Examples); ok {
			for _, e := range was {
				i := slices.IndexFunc(now, func(n exampleRule) bool { return n.example == e.example })
				switch {
				case i < 0:
					drop(e.example)
				case now[i].rule != e.rule:
					out = append(out, fmt.Sprintf("%s was under %s and is now under %s", e.example, e.rule, now[i].rule))
				}
			}
		}
	}

	if was, ok := operationIDs(current.Contract); ok {
		if now, ok := operationIDs(revised.Contract); ok {
			missing(was, now)
		}
	}
	return out
}

// firstOfEach returns the items of v whose key no earlier item has, in order.
func firstOfEach[T any](v []T, key func(T) string) []T {
	var out []T
	seen := map[string]bool{}
	for _, x := range v {
		if k := key(x); !seen[k] {
			seen[k] = true
			out = append(out, x)
		}
	}
	return out
}

// uniqueIDs returns the keys of v, each once, in order.
func uniqueIDs[T any](v []T, key func(T) string) []string {
	var out []string
	for _, x := range firstOfEach(v, key) {
		out = append(out, key(x))
	}
	return out
}

// exampleRule is an example's ID and the rule whose "Rule:" block it sits under.
type exampleRule struct{ example, rule string }

// blockRule matches the name of a "Rule:" block that names a rule.
var blockRule = regexp.MustCompile(`^R-[0-9]+$`)

// exampleTagRe matches an example's tag.
var exampleTagRe = regexp.MustCompile(`^@(E-[0-9]+)$`)

// exampleRules returns each E-<n> that a scenario under a "Rule: R-<n>" block of Gherkin examples
// carries as an @E-<n> tag, with that rule, in document order; an ID carried twice keeps its
// first rule. It reports false when the text is not Gherkin.
func exampleRules(text string) ([]exampleRule, bool) {
	if strings.TrimSpace(text) == "" {
		return nil, true
	}
	gd, err := gherkin.ParseGherkinDocument(strings.NewReader(text), (&messages.Incrementing{}).NewId)
	if err != nil {
		return nil, false
	}
	if gd.Feature == nil {
		return nil, true
	}
	var out []exampleRule
	for _, fc := range gd.Feature.Children {
		if fc.Rule == nil || !blockRule.MatchString(strings.TrimSpace(fc.Rule.Name)) {
			continue
		}
		rule := strings.TrimSpace(fc.Rule.Name)
		for _, rc := range fc.Rule.Children {
			if rc.Scenario == nil {
				continue
			}
			for _, t := range rc.Scenario.Tags {
				m := exampleTagRe.FindStringSubmatch(t.Name)
				if m != nil && !slices.ContainsFunc(out, func(e exampleRule) bool { return e.example == m[1] }) {
					out = append(out, exampleRule{example: m[1], rule: rule})
				}
			}
		}
	}
	return out, true
}

// operationIDs returns the operationId of each operation under the paths and webhooks of an
// OpenAPI document, each once, in document order, following YAML aliases. It reports false when
// the text is not YAML.
func operationIDs(text string) ([]string, bool) {
	var root yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader([]byte(text))).Decode(&root); err != nil {
		if strings.TrimSpace(text) == "" {
			return nil, true
		}
		return nil, false
	}
	if len(root.Content) == 0 {
		return nil, true
	}
	var out []string
	doc := root.Content[0]
	for _, section := range []string{"paths", "webhooks"} {
		items := yamlField(doc, section)
		if items == nil || items.Kind != yaml.MappingNode {
			continue
		}
		for i := 1; i < len(items.Content); i += 2 {
			item := items.Content[i]
			for _, m := range []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"} {
				id := yamlField(yamlField(item, m), "operationId")
				if id != nil && id.Kind == yaml.ScalarNode && id.Value != "" && !slices.Contains(out, id.Value) {
					out = append(out, id.Value)
				}
			}
		}
	}
	return out, true
}

// yamlField returns the value of key in the mapping n, aliases followed, or nil.
func yamlField(n *yaml.Node, key string) *yaml.Node {
	n = unalias(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return unalias(n.Content[i+1])
		}
	}
	return nil
}

// unalias returns the node an alias stands for, or n itself.
func unalias(n *yaml.Node) *yaml.Node {
	for seen := 0; n != nil && n.Kind == yaml.AliasNode && seen < 64; seen++ {
		n = n.Alias
	}
	return n
}
