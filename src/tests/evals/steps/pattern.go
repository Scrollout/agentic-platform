package steps

import (
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// PatternStep is what the graders take from one step of the api-feature-go pattern: the same
// values the engine's checks get, read from the catalogue's pattern.yaml, so an eval and a run
// cannot disagree about a step's writable paths or its allowed dependencies.
type PatternStep struct {
	Name         string   `yaml:"name"`
	Inputs       []string `yaml:"inputs"`
	Outputs      []string `yaml:"outputs"`
	Writable     []string `yaml:"writable"`
	Checks       []string `yaml:"checks"`
	Dependencies struct {
		Allowed   []string `yaml:"allowed"`
		Forbidden []string `yaml:"forbidden"`
	} `yaml:"dependencies"`
}

// LoadPattern reads src/catalogue/api-feature-go/pattern.yaml below root and returns its steps by
// name.
func LoadPattern(root string) (map[string]PatternStep, error) {
	b, err := os.ReadFile(filepath.Join(root, "src", "catalogue", "api-feature-go", "pattern.yaml"))
	if err != nil {
		return nil, err
	}
	var p struct {
		Steps []PatternStep `yaml:"steps"`
	}
	if err := yaml.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	out := map[string]PatternStep{}
	for _, s := range p.Steps {
		out[s.Name] = s
	}
	for _, n := range []string{"generate", "tests", "code", "review"} {
		if _, ok := out[n]; !ok {
			return nil, fmt.Errorf("pattern.yaml has no step %s", n)
		}
	}
	return out, nil
}
