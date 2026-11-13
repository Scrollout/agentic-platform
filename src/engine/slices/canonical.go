package slices

import (
	"fmt"

	"github.com/agentic-platform/engine/canonical"
)

// The parts in canonical form: {goals}, {rules, examples, terms} and {contract}
// (slice-defs.schema.json goals_part, rules_part and contract_part; research D17). A part's hash
// is the SHA-256, in lowercase hex, of its RFC 8785 canonical JSON, written by package canonical,
// their one owner.

func goalsPart(goals []Goal) canonical.Object {
	arr := canonical.Array{}
	for _, g := range goals {
		arr = append(arr, canonical.Object{"id": g.ID, "text": g.Text})
	}
	return canonical.Object{"goals": arr}
}

func rulesPart(p RulesPart) canonical.Object {
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
	return canonical.Object{"rules": rules, "examples": p.Examples, "terms": terms}
}

func goalsHash(goals []Goal) (string, error) { return canonical.Hash(goalsPart(goals)) }

func rulesHash(p RulesPart) (string, error) { return canonical.Hash(rulesPart(p)) }

func contractHash(contract string) (string, error) {
	return canonical.ContractHash(contract)
}

// ApprovedParts returns each part of the slice ("goals", "rules", "contract") as its canonical
// JSON, whose SHA-256 is the hash its approval stores. A part not approved at its current content
// is NotApproved, so what it returns is always what the developer approved.
func (s *Service) ApprovedParts(id string) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.find(id)
	if err != nil {
		return nil, err
	}
	sl := &r.slice
	parts := map[Part]canonical.Object{
		PartGoals:    goalsPart(sl.Goals),
		PartRules:    rulesPart(RulesPart{Rules: sl.Rules, Examples: sl.Examples, Terms: sl.Terms}),
		PartContract: {"contract": sl.Contract},
	}
	out := map[string][]byte{}
	for _, part := range allParts {
		b, err := canonical.JSON(parts[part])
		if err != nil {
			return nil, err
		}
		h, _ := canonical.Hash(parts[part])
		approved := false
		for _, a := range sl.Approvals {
			approved = approved || (a.Part == part && a.ContentHash == h)
		}
		if !approved {
			return nil, &Error{Code: "NotApproved", Detail: fmt.Sprintf("the %s part is not approved at its current content", part)}
		}
		out[string(part)] = b
	}
	return out, nil
}
