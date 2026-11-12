package slice

import (
	"fmt"
	"regexp"
	"strings"

	gherkin "github.com/cucumber/gherkin/go/v42"
	messages "github.com/cucumber/messages/go/v34"
)

// The tags a scenario carries for its rule and its example (data-model.md `examples`).
var (
	ruleTag    = regexp.MustCompile(`^@(R-[0-9]+)$`)
	exampleTag = regexp.MustCompile(`^@(E-[0-9]+)$`)
)

// ExampleTags checks the tags of the scenarios in Gherkin examples (check ExampleTagged,
// FR-003): each scenario carries exactly one @R-<n> tag, the one of the "Rule: R-<n>" block it
// sits under, and exactly one @E-<n> tag that no other scenario carries. It returns one finding
// per fault, on the rules part, with the scenario's name in the reason, or nil when all hold. An
// error means the text is not Gherkin.
func ExampleTags(text string) ([]Finding, error) {
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
	var out []Finding
	seen := map[string]string{} // E-<n> to the scenario that carries it first
	check := func(sc *messages.Scenario, rule string) {
		var rs, es []string
		for _, t := range sc.Tags {
			if m := ruleTag.FindStringSubmatch(t.Name); m != nil {
				rs = append(rs, m[1])
			} else if m := exampleTag.FindStringSubmatch(t.Name); m != nil {
				es = append(es, m[1])
			}
		}
		name := strings.TrimSpace(sc.Name)
		what := fmt.Sprintf("scenario %q", name)
		fail := func(id, reason string) { out = append(out, Finding{Part: "rules", ID: id, Reason: reason}) }
		switch {
		case len(rs) == 0:
			fail(rule, what+" has no @R-<n> tag")
		case len(rs) > 1:
			fail(rule, fmt.Sprintf("%s has %d @R-<n> tags; it serves one rule", what, len(rs)))
		case rule == "":
			fail(rs[0], fmt.Sprintf("%s is tagged @%s but is not under a \"Rule: %s\" block", what, rs[0], rs[0]))
		case rs[0] != rule:
			fail(rs[0], fmt.Sprintf("%s is tagged @%s but sits under \"Rule: %s\"", what, rs[0], rule))
		}
		switch {
		case len(es) == 0:
			fail(rule, what+" has no @E-<n> tag")
		case len(es) > 1:
			fail(rule, fmt.Sprintf("%s has %d @E-<n> tags; it is one example", what, len(es)))
		default:
			if first, ok := seen[es[0]]; ok {
				fail(es[0], fmt.Sprintf("%s repeats @%s of scenario %q; example IDs are unique", what, es[0], first))
			} else {
				seen[es[0]] = name
			}
		}
	}
	for _, fc := range gd.Feature.Children {
		switch {
		case fc.Scenario != nil:
			check(fc.Scenario, "")
		case fc.Rule != nil:
			for _, rc := range fc.Rule.Children {
				if rc.Scenario != nil {
					check(rc.Scenario, strings.TrimSpace(fc.Rule.Name))
				}
			}
		}
	}
	return out, nil
}
