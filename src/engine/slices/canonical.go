package slices

import "github.com/agentic-platform/engine/canonical"

// The part hashes: the SHA-256, in lowercase hex, of the RFC 8785 canonical JSON form of {goals},
// {rules, examples, terms} and {contract} (slice-defs.schema.json goals_part, rules_part and
// contract_part; research D17), written by package canonical, their one owner.

func goalsHash(goals []Goal) (string, error) {
	arr := canonical.Array{}
	for _, g := range goals {
		arr = append(arr, canonical.Object{"id": g.ID, "text": g.Text})
	}
	return canonical.Hash(canonical.Object{"goals": arr})
}

func rulesHash(p RulesPart) (string, error) {
	rules := canonical.Array{}
	for _, r := range p.Rules {
		o := canonical.Object{"id": r.ID, "goal": r.Goal, "pattern": r.Pattern, "system": r.System, "response": r.Response}
		// The optional clauses are omitted when empty, as Rule's JSON form omits them.
		for name, v := range map[string]string{"precondition": r.Precondition, "trigger": r.Trigger, "feature": r.Feature} {
			if v != "" {
				o[name] = v
			}
		}
		terms := canonical.Array{} // a nil list is the empty list
		for _, t := range r.Terms {
			terms = append(terms, t)
		}
		o["terms"] = terms
		rules = append(rules, o)
	}
	terms := canonical.Array{}
	for _, t := range p.Terms {
		terms = append(terms, canonical.Object{"id": t.ID, "name": t.Name, "definition": t.Definition})
	}
	return canonical.Hash(canonical.Object{"rules": rules, "examples": p.Examples, "terms": terms})
}

func contractHash(contract string) (string, error) {
	return canonical.ContractHash(contract)
}
