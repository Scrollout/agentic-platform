package slices

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/agentic-platform/slice"
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

	if was, err := slice.ExampleRules(current.Examples); err == nil {
		if now, err := slice.ExampleRules(revised.Examples); err == nil {
			for _, e := range was {
				i := slices.IndexFunc(now, func(n slice.ExampleRule) bool { return n.Example == e.Example })
				switch {
				case i < 0:
					drop(e.Example)
				case now[i].Rule != e.Rule:
					out = append(out, fmt.Sprintf("%s was under %s and is now under %s", e.Example, e.Rule, now[i].Rule))
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
