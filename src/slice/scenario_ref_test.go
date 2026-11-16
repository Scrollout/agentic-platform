package slice

// T104: ScenarioRef is `R-<n>/E-<n>` from a scenario's one @R-<n> and one @E-<n> tag; empty
// otherwise. Written before the code by a separate test author.

import (
	"testing"

	messages "github.com/cucumber/messages/go/v34"
)

func TestScenarioRef(t *testing.T) {
	sc := func(tags ...string) *messages.Scenario {
		s := &messages.Scenario{}
		for _, n := range tags {
			s.Tags = append(s.Tags, &messages.Tag{Name: n})
		}
		return s
	}
	for name, c := range map[string]struct {
		tags []string
		want string
	}{
		"one rule, one example":  {[]string{"@R-1", "@E-2"}, "R-1/E-2"},
		"example tag first":      {[]string{"@E-2", "@R-1"}, "R-1/E-2"},
		"other tags are ignored": {[]string{"@run", "@R-3", "@E-10"}, "R-3/E-10"},
		"no tags":                {nil, ""},
		"only a rule":            {[]string{"@R-1"}, ""},
		"only an example":        {[]string{"@E-1"}, ""},
		"a rule, two examples":   {[]string{"@R-1", "@E-1", "@E-2"}, ""},
		"two rules, one example": {[]string{"@R-1", "@R-2", "@E-1"}, ""},
	} {
		if got := ScenarioRef(sc(c.tags...)); got != c.want {
			t.Errorf("%s: ScenarioRef(%v) = %q, want %q", name, c.tags, got, c.want)
		}
	}
}
