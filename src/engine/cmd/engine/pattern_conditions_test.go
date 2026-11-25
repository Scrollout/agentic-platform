package main

// T011 (feature 010): the pattern api-feature-go's fit conditions are the four of before,
// followed by RefusalDeclared and RequestBodiesSupported. The engine loads the copy it embeds
// (`pattern`, setup.go); the source file src/catalogue/api-feature-go/pattern.yaml is the
// authoritative one, so both are judged, and the two copies must agree. The version and the
// generate step's checks are pattern_values_test.go's.

import (
	"os"
	"reflect"
	"testing"

	"go.yaml.in/yaml/v3"
)

func patternConditions(t *testing.T, name string, b []byte) []string {
	t.Helper()
	var p struct {
		Conditions []string `yaml:"conditions"`
	}
	if err := yaml.Unmarshal(b, &p); err != nil {
		t.Fatalf("%s cannot be read: %v", name, err)
	}
	return p.Conditions
}

func TestPatternDeclaresTheFitConditions(t *testing.T) {
	source, err := os.ReadFile("../../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatalf("the source pattern.yaml cannot be read: %v", err)
	}
	if len(pattern) == 0 {
		t.Fatal("the embedded pattern is empty")
	}
	want := []string{"ContractIsOpenApi31", "OnlyHttpOperations", "ContractRefsLocal", "RulesNameOneSystem",
		"RefusalDeclared", "RequestBodiesSupported"}
	embeddedGot := patternConditions(t, "the embedded pattern", pattern)
	sourceGot := patternConditions(t, "the source pattern.yaml", source)
	if !reflect.DeepEqual(embeddedGot, want) {
		t.Errorf("the embedded pattern's conditions = %v, want %v", embeddedGot, want)
	}
	if !reflect.DeepEqual(sourceGot, want) {
		t.Errorf("the source pattern.yaml's conditions = %v, want %v", sourceGot, want)
	}
	if !reflect.DeepEqual(embeddedGot, sourceGot) {
		t.Errorf("the copies disagree: embedded %v, source %v", embeddedGot, sourceGot)
	}
}
