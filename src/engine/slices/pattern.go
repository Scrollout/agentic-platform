package slices

import (
	"errors"
	"fmt"
	"slices"

	"go.yaml.in/yaml/v3"
)

// PatternBudget is one budget of a pattern: model tokens, cost in USD and minutes.
type PatternBudget struct {
	Tokens  int64   `yaml:"tokens"`
	CostUSD float64 `yaml:"cost_usd"`
	Minutes float64 `yaml:"minutes"`
}

// PatternStep is one step of a pattern, as a run follows it.
type PatternStep struct {
	Name        string   `yaml:"name"`
	Worker      string   `yaml:"worker"`
	AgentDigest string   `yaml:"agent_digest"`
	Inputs      []string `yaml:"inputs"`
	Outputs     []string `yaml:"outputs"`
	Checks      []string `yaml:"checks"`
}

// Pattern is what a run follows of the pattern the slices are checked against: its ID and
// version, limits, budgets and steps, in the pattern's order.
type Pattern struct {
	ID      string `yaml:"id"`
	Version string `yaml:"version"`
	Limits  struct {
		Attempts     int `yaml:"attempts"`
		ReviewRounds int `yaml:"review_rounds"`
	} `yaml:"limits"`
	Budgets struct {
		Run  PatternBudget `yaml:"run"`
		Step PatternBudget `yaml:"step"`
	} `yaml:"budgets"`
	Steps []PatternStep `yaml:"steps"`
}

// Pattern returns the pattern's run view, read from the pattern.yaml the service was made with, its
// one home. A missing or unreadable pattern, or one without an ID, a version, steps or step names,
// is an error (fail closed: no run can start).
func (s *Service) Pattern() (Pattern, error) {
	var p Pattern
	if s.pattern == nil {
		return p, errors.New("slices: no pattern")
	}
	if err := yaml.Unmarshal(s.pattern, &p); err != nil {
		return Pattern{}, fmt.Errorf("slices: the pattern is unreadable: %w", err)
	}
	if p.ID == "" || p.Version == "" || len(p.Steps) == 0 {
		return Pattern{}, errors.New("slices: the pattern has no ID, version or steps")
	}
	for _, st := range p.Steps {
		if st.Name == "" {
			return Pattern{}, errors.New("slices: a step of the pattern has no name")
		}
	}
	p.Steps = slices.Clone(p.Steps)
	return p, nil
}
