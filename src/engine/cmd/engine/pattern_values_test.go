package main

// T012 (feature 009): the pattern api-feature-go is version 1.1.0, and its generate and code
// steps judge the declared values and the deployment values (data-model.md, "Pattern
// api-feature-go 1.1.0"). The engine loads the copy it embeds (`pattern`, setup.go), which
// scripts/generate.sh copies from the source file src/catalogue/api-feature-go/pattern.yaml; the
// source file is the authoritative one (generate.sh --check proves the copy matches it), so both
// are judged.

import (
	"os"
	"reflect"
	"testing"

	"go.yaml.in/yaml/v3"
)

func patternVersionAndChecks(t *testing.T, name string, b []byte) (string, map[string][]string) {
	t.Helper()
	var p struct {
		Version string `yaml:"version"`
		Steps   []struct {
			Name   string   `yaml:"name"`
			Checks []string `yaml:"checks"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(b, &p); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	checks := map[string][]string{}
	for _, s := range p.Steps {
		checks[s.Name] = s.Checks
	}
	return p.Version, checks
}

func TestPatternDeclaresTheValueChecks(t *testing.T) {
	source, err := os.ReadFile("../../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatal(err)
	}
	wantChecks := map[string][]string{
		"generate": {"GenerationRepeatable", "GeneratedCodeBuilds", "DeclaredValuesGenerated", "DependenciesAllowed", "ScopeKept"},
		"code": {"ScopeKept", "TestsUnchanged", "GeneratedCodeUnchanged", "ContractMatchesApproval", "DependenciesAllowed",
			"DeclaredValuesNotRepeated", "DeploymentValuesConfigured", "AllTestsPass"},
	}
	for name, b := range map[string][]byte{"the embedded pattern": pattern, "the source pattern.yaml": source} {
		version, checks := patternVersionAndChecks(t, name, b)
		if version != "1.1.0" {
			t.Errorf("%s: version = %q, want 1.1.0", name, version)
		}
		for step, want := range wantChecks {
			if got := checks[step]; !reflect.DeepEqual(got, want) {
				t.Errorf("%s: the %s step's checks = %v, want %v", name, step, got, want)
			}
		}
	}
}
