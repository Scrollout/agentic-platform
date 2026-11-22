// Tests for task T022 (feature 007): ExampleRules gives each example ID with the rule it sits
// under, as the contract in README.md states.
package slice

import (
	"reflect"
	"testing"
)

func TestExampleRules(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []ExampleRule
	}{
		{"one example under its rule", `Feature: Notes
  Rule: R-1
    @R-1 @E-1
    Scenario: store
      Given a note
`, []ExampleRule{{Example: "E-1", Rule: "R-1"}}},
		{"document order across rules", `Feature: Notes
  Rule: R-2
    @R-2 @E-5
    Scenario: a
      Given x

  Rule: R-1
    @R-1 @E-2
    Scenario: b
      Given x
    @R-1 @E-9
    Scenario: c
      Given x
`, []ExampleRule{{"E-5", "R-2"}, {"E-2", "R-1"}, {"E-9", "R-1"}}},
		{"duplicate ID keeps its first rule", `Feature: Notes
  Rule: R-1
    @E-1
    Scenario: a
      Given x
  Rule: R-2
    @E-1 @E-3
    Scenario: b
      Given x
`, []ExampleRule{{"E-1", "R-1"}, {"E-3", "R-2"}}},
		{"scenarios outside a Rule: R-<n> block are not listed", `Feature: Notes
  @E-1
  Scenario: directly under feature
    Given x
  Rule: notes
    @E-2
    Scenario: under a named rule
      Given x
  Rule: R-1 extra
    @E-3
    Scenario: under a longer name
      Given x
  Rule: R-1
    @E-4
    Scenario: listed
      Given x
`, []ExampleRule{{"E-4", "R-1"}}},
		{"several E tags on one scenario", `Feature: Notes
  Rule: R-1
    @E-1 @E-2 @E-3
    Scenario: many
      Given x
`, []ExampleRule{{"E-1", "R-1"}, {"E-2", "R-1"}, {"E-3", "R-1"}}},
		{"other tags are ignored", `Feature: Notes
  Rule: R-1
    @R-1 @E-x @e-1 @E-7 @E-
    Scenario: tags
      Given x
`, []ExampleRule{{"E-7", "R-1"}}},
		{"scenario outline counts", `Feature: Notes
  Rule: R-1
    @E-1
    Scenario Outline: outline
      Given <v>
      Examples:
        | v |
        | 1 |
`, []ExampleRule{{"E-1", "R-1"}}},
		{"rule name with surrounding spaces", "Feature: Notes\n  Rule:   R-2  \n    @E-1\n    Scenario: a\n      Given x\n",
			[]ExampleRule{{"E-1", "R-2"}}},
		{"rule without scenarios", "Feature: Notes\n  Rule: R-1\n    Example: none\n      Given x\n", nil},
		{"feature without scenarios", "Feature: Notes\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ExampleRules(c.text)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestExampleRulesEmptyText(t *testing.T) {
	for _, text := range []string{"", "   ", "\n\t \n"} {
		got, err := ExampleRules(text)
		if err != nil {
			t.Errorf("%q: error %v", text, err)
		}
		if len(got) != 0 {
			t.Errorf("%q: got %v, want none", text, got)
		}
	}
}

func TestExampleRulesNotGherkin(t *testing.T) {
	got, err := ExampleRules("this is not gherkin\n  Rule: R-1\n")
	if err == nil {
		t.Fatalf("want an error, got %v", got)
	}
}
