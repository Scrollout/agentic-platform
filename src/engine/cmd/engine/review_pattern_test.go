package main

// T076: the review step of src/catalogue/api-feature-go/pattern.yaml as research D35 and ADR 0039
// make it: it writes its witness tests under review/** and declares two outputs, its findings
// and the witnesses; it builds and keeps them; its dependencies are the tests step's without
// godog. Written before the change, by a separate author.

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type reviewPatternStep struct {
	Name         string   `yaml:"name"`
	Writable     []string `yaml:"writable"`
	Outputs      []string `yaml:"outputs"`
	Checks       []string `yaml:"checks"`
	Dependencies struct {
		Allowed   []string `yaml:"allowed"`
		Forbidden []string `yaml:"forbidden"`
	} `yaml:"dependencies"`
}

func readPatternSteps(t *testing.T) map[string]reviewPatternStep {
	t.Helper()
	b, err := os.ReadFile("../../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Steps []reviewPatternStep `yaml:"steps"`
	}
	if err := yaml.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	out := map[string]reviewPatternStep{}
	for _, s := range p.Steps {
		out[s.Name] = s
	}
	return out
}

func TestPatternReviewStepWritesWitnesses(t *testing.T) {
	steps := readPatternSteps(t)
	review, ok := steps["review"]
	if !ok {
		t.Fatal("the pattern has no review step")
	}
	if !reflect.DeepEqual(review.Writable, []string{"review/**"}) {
		t.Errorf("review writable = %v, want [review/**]", review.Writable)
	}
	if !reflect.DeepEqual(review.Outputs, []string{"findings", "witnesses"}) {
		t.Errorf("review outputs = %v, want [findings witnesses]", review.Outputs)
	}
	want := []string{"ScopeKept", "DependenciesAllowed", "TestsBuild", "TestsUnchanged", "FindingsCarryEvidence", "NoBlockingFindingOpen"}
	if !reflect.DeepEqual(review.Checks, want) {
		t.Errorf("review checks = %v, want %v", review.Checks, want)
	}
	tests := steps["tests"]
	var allowed []string
	for _, a := range tests.Dependencies.Allowed {
		if !strings.Contains(a, "godog") {
			allowed = append(allowed, a)
		}
	}
	if len(allowed) == len(tests.Dependencies.Allowed) {
		t.Fatalf("the tests step allows no godog (%v), so this test proves nothing", tests.Dependencies.Allowed)
	}
	if !reflect.DeepEqual(review.Dependencies.Allowed, allowed) {
		t.Errorf("review dependencies.allowed = %v, want the tests step's without godog %v", review.Dependencies.Allowed, allowed)
	}
	if !reflect.DeepEqual(review.Dependencies.Forbidden, tests.Dependencies.Forbidden) {
		t.Errorf("review dependencies.forbidden = %v, want the tests step's %v", review.Dependencies.Forbidden, tests.Dependencies.Forbidden)
	}
	// No other step writes under review/ (ScopeKept judges generate, whose root is writable).
	for name, s := range steps {
		if name != "review" && slices.ContainsFunc(s.Writable, func(w string) bool { return strings.HasPrefix(w, "review") }) {
			t.Errorf("step %s may write %v under review/", name, s.Writable)
		}
	}
}
