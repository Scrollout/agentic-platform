package api

// T005 (feature 011, FR-003, Principle III "Schema validation at the API and the domain types
// behind it MUST derive from the same contract-declared rule: ... or a contract test proves they
// match"): the limits and the `parameter_name`, `parameter_value` and `capture_name` rules that
// package engine/captured holds as named values equal the loaded engine contract's, and a planted
// contract copy with one of them changed fails the comparison (Checks prove they can fail).

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/captured"
	"go.yaml.in/yaml/v3"
)

// capturedRuleDiffs compares engine/captured's limits and value rules with the contract given as
// its openapi.yaml and slice-defs.schema.json bytes, and returns one line per difference, each
// naming the contract item. An unreadable contract is one line too.
func capturedRuleDiffs(openapi, defs []byte) []string {
	var doc struct {
		Limits     map[string]any `yaml:"x-limits"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					MaxItems *int `yaml:"maxItems"`
				} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(openapi, &doc); err != nil {
		return []string{"openapi.yaml: " + err.Error()}
	}
	var sd struct {
		Defs map[string]struct {
			Pattern   string `json:"pattern"`
			MaxLength *int   `json:"maxLength"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(defs, &sd); err != nil {
		return []string{"slice-defs.schema.json: " + err.Error()}
	}
	var diffs []string
	num := func(item string, got *int, want int) {
		if got == nil {
			diffs = append(diffs, fmt.Sprintf("%s is not declared; engine/captured holds %d", item, want))
		} else if *got != want {
			diffs = append(diffs, fmt.Sprintf("%s is %d in the contract; engine/captured holds %d", item, *got, want))
		}
	}
	xl := func(key string, want int) {
		v, ok := doc.Limits[key].(int)
		if !ok {
			num("x-limits."+key, nil, want)
			return
		}
		num("x-limits."+key, &v, want)
	}
	xl("captured-pattern-parameters-max", captured.ParametersMax)
	xl("parameter-places-max", captured.PlacesMax)
	xl("parameter-value-max-chars", captured.ValueMaxChars)

	// The schemas' maxItems repeat the limits.
	num("ParametersRequest.parameters maxItems", doc.Components.Schemas["ParametersRequest"].Properties["parameters"].MaxItems, captured.ParametersMax)
	num("CapturedPattern.parameters maxItems", doc.Components.Schemas["CapturedPattern"].Properties["parameters"].MaxItems, captured.ParametersMax)
	num("Parameter.places maxItems", doc.Components.Schemas["Parameter"].Properties["places"].MaxItems, captured.PlacesMax)

	rule := func(def, patternWant string, maxWant int) {
		d, ok := sd.Defs[def]
		if !ok {
			diffs = append(diffs, "slice-defs $defs."+def+" is missing")
			return
		}
		if d.Pattern != patternWant {
			diffs = append(diffs, fmt.Sprintf("$defs.%s pattern is %q in the contract; engine/captured holds %q", def, d.Pattern, patternWant))
		}
		num("$defs."+def+" maxLength", d.MaxLength, maxWant)
	}
	rule("parameter_name", captured.ParameterNamePattern, captured.ParameterNameMaxChars)
	rule("parameter_value", captured.ParameterValuePattern, captured.ValueMaxChars)
	rule("capture_name", captured.CaptureNamePattern, captured.CaptureNameMaxChars)
	return diffs
}

func readCapturedContract(t *testing.T) (openapi, defs []byte) {
	t.Helper()
	openapi, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	defs, err = os.ReadFile(specDir + "/slice-defs.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return openapi, defs
}

// T005: the named values of engine/captured equal the contract's.
func TestCapturedLimitsAndValueRulesEqualTheContracts(t *testing.T) {
	openapi, defs := readCapturedContract(t)
	if d := capturedRuleDiffs(openapi, defs); len(d) > 0 {
		t.Fatalf("engine/captured differs from the contract:\n%s", strings.Join(d, "\n"))
	}
}

// T005: a planted contract copy with one limit or rule changed fails the comparison, and the
// failure names the item (the comparison can fail).
func TestAPlantedContractCopyWithOneLimitChangedFailsTheComparison(t *testing.T) {
	openapi, defs := readCapturedContract(t)
	if d := capturedRuleDiffs(openapi, defs); len(d) > 0 {
		t.Fatalf("the unplanted contract already differs: %v", d)
	}
	// An x-limits line of the openapi.yaml, found in the file so the test fails if it moves.
	for _, key := range []string{"captured-pattern-parameters-max", "parameter-places-max", "parameter-value-max-chars"} {
		t.Run("x-limits."+key, func(t *testing.T) {
			line := "  " + key + ": " + fmt.Sprint(contractLimit(t, key)) + "\n"
			if strings.Count(string(openapi), line) != 1 {
				t.Fatalf("the contract has no single line %q to plant in", line)
			}
			planted := strings.Replace(string(openapi), line, "  "+key+": "+fmt.Sprint(contractLimit(t, key)+1)+"\n", 1)
			d := capturedRuleDiffs([]byte(planted), defs)
			if len(d) != 1 || !strings.Contains(d[0], key) {
				t.Fatalf("a changed %s gave %v, want exactly one difference naming it", key, d)
			}
		})
	}
	// A maxItems of the openapi.yaml schemas.
	for _, plant := range []struct{ name, from, to string }{
		{"ParametersRequest.parameters maxItems", "          maxItems: 32\n          description: Every parameter the captured pattern keeps", "          maxItems: 31\n          description: Every parameter the captured pattern keeps"},
		{"Parameter.places maxItems", "          maxItems: 10000\n", "          maxItems: 9999\n"},
	} {
		t.Run(plant.name, func(t *testing.T) {
			if strings.Count(string(openapi), plant.from) != 1 {
				t.Fatalf("the contract has no single text %q to plant in", plant.from)
			}
			d := capturedRuleDiffs([]byte(strings.Replace(string(openapi), plant.from, plant.to, 1)), defs)
			if len(d) != 1 || !strings.Contains(d[0], plant.name) {
				t.Fatalf("a changed %s gave %v, want exactly one difference naming it", plant.name, d)
			}
		})
	}
	// A rule of slice-defs.schema.json.
	for _, plant := range []struct{ def, field string }{
		{"parameter_name", "pattern"}, {"parameter_name", "maxLength"},
		{"parameter_value", "pattern"}, {"parameter_value", "maxLength"},
		{"capture_name", "pattern"}, {"capture_name", "maxLength"},
	} {
		t.Run(plant.def+"."+plant.field, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal(defs, &m); err != nil {
				t.Fatal(err)
			}
			def := m["$defs"].(map[string]any)[plant.def].(map[string]any)
			switch v := def[plant.field].(type) {
			case string:
				def[plant.field] = v + "x"
			case float64:
				def[plant.field] = v + 1
			default:
				t.Fatalf("$defs.%s.%s is %T, not a pattern or a length", plant.def, plant.field, v)
			}
			planted, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			d := capturedRuleDiffs(openapi, planted)
			if len(d) != 1 || !strings.Contains(d[0], plant.def) {
				t.Fatalf("a changed %s.%s gave %v, want exactly one difference naming %s", plant.def, plant.field, d, plant.def)
			}
		})
	}
	// A missing rule is a difference too (fail closed), not a pass.
	var m map[string]any
	if err := json.Unmarshal(defs, &m); err != nil {
		t.Fatal(err)
	}
	delete(m["$defs"].(map[string]any), "capture_name")
	planted, _ := json.Marshal(m)
	if d := capturedRuleDiffs(openapi, planted); len(d) == 0 {
		t.Fatal("a contract without capture_name passed the comparison")
	}
}
