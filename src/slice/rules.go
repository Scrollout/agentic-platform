package slice

import (
	"strings"

	gherkin "github.com/cucumber/gherkin/go/v42"
	messages "github.com/cucumber/messages/go/v34"
)

// RuleScenarios maps each rule ID R-<n> to the names of the scenarios of Gherkin examples that
// carry its @R-<n> tag, the tags of each scenario as godog runs it (a pickle: its own tags and
// those of its Rule, its Feature and, for a Scenario Outline, its Examples), in document order. A
// name godog runs twice is listed twice. godog runs each such scenario as a Go subtest of that
// name, which is how the review checks find a rule's scenarios in `go test` output (check
// contract, runCheck, FindingsCarryEvidence). An error means the text is not Gherkin.
func RuleScenarios(text string) (map[string][]string, error) {
	out := map[string][]string{}
	if strings.TrimSpace(text) == "" {
		return out, nil
	}
	ids := &messages.Incrementing{}
	gd, err := gherkin.ParseGherkinDocument(strings.NewReader(text), ids.NewId)
	if err != nil {
		return nil, err
	}
	for _, p := range gherkin.Pickles(*gd, "features/slice.feature", ids.NewId) {
		seen := map[string]bool{}
		for _, t := range p.Tags {
			if m := ruleTag.FindStringSubmatch(t.Name); m != nil && !seen[m[1]] {
				seen[m[1]] = true
				out[m[1]] = append(out[m[1]], p.Name)
			}
		}
	}
	return out, nil
}
