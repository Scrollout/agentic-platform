package api

// T035 (feature 012, Principle III "Schema validation at the API and the domain types behind it
// MUST derive from the same contract-declared rule: ... or a contract test proves they match"):
// the trial limits the engine holds equal the contract's x-limits, the schemas' bounds repeat
// them, a planted contract copy with one value changed fails the comparison (Checks prove they
// can fail), and the engine refuses a contract whose trial limits are unusable.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

var trialLimitKeys = []string{"trials-max", "trials-default", "trial-set-cost-usd", "trial-set-hours"}

// trialRuleDiffs compares the trial limits the engine holds (got) with the contract given as its
// openapi.yaml and slice-defs.schema.json bytes: x-limits, and the bounds the schemas repeat. It
// returns one line per difference, each naming the contract item. An unreadable contract is one
// line too.
func trialRuleDiffs(got trialLimits, openapi, defs []byte) []string {
	var doc struct {
		Limits     map[string]any `yaml:"x-limits"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Minimum *int `yaml:"minimum"`
					Maximum *int `yaml:"maximum"`
					Default *int `yaml:"default"`
				} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(openapi, &doc); err != nil {
		return []string{"openapi.yaml: " + err.Error()}
	}
	var sd struct {
		Defs map[string]struct {
			Minimum *int `json:"minimum"`
			Maximum *int `json:"maximum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(defs, &sd); err != nil {
		return []string{"slice-defs.schema.json: " + err.Error()}
	}
	var diffs []string
	num := func(item string, have *int, want int) {
		if have == nil {
			diffs = append(diffs, fmt.Sprintf("%s is not declared; want %d", item, want))
		} else if *have != want {
			diffs = append(diffs, fmt.Sprintf("%s is %d in the contract; want %d", item, *have, want))
		}
	}
	engine := map[string]float64{
		"trials-max": float64(got.max), "trials-default": float64(got.def),
		"trial-set-cost-usd": got.costUSD, "trial-set-hours": float64(got.hours),
	}
	declared := map[string]float64{}
	for _, key := range trialLimitKeys {
		switch v := doc.Limits[key].(type) {
		case int:
			declared[key] = float64(v)
		case float64:
			declared[key] = v
		default:
			diffs = append(diffs, fmt.Sprintf("x-limits.%s is not a number in the contract", key))
			continue
		}
		if engine[key] != declared[key] {
			diffs = append(diffs, fmt.Sprintf("x-limits.%s is %v in the contract; the engine holds %v", key, declared[key], engine[key]))
		}
	}
	// The schemas' bounds repeat the engine's values, so a change of either side shows.
	max, def := got.max, got.def
	req := doc.Components.Schemas["TrialSetRequest"].Properties["trials"]
	num("TrialSetRequest.trials minimum", req.Minimum, 1)
	num("TrialSetRequest.trials maximum", req.Maximum, max)
	num("TrialSetRequest.trials default", req.Default, def)
	num("TrialSet.trials maximum", doc.Components.Schemas["TrialSet"].Properties["trials"].Maximum, max)
	tn, ok := sd.Defs["trial_number"]
	if !ok {
		diffs = append(diffs, "slice-defs $defs.trial_number is missing")
	}
	num("$defs.trial_number minimum", tn.Minimum, 1)
	num("$defs.trial_number maximum", tn.Maximum, max)
	return diffs
}

func readTrialContract(t *testing.T) (openapi, defs []byte) {
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

// T035: the engine's trial limits equal the contract's x-limits, and the schemas' bounds repeat
// them.
func TestTrialLimitsAndSchemaBoundsEqualTheContracts(t *testing.T) {
	openapi, defs := readTrialContract(t)
	if d := trialRuleDiffs(engineContract.trials, openapi, defs); len(d) > 0 {
		t.Fatalf("the trial limits differ from the contract:\n%s", strings.Join(d, "\n"))
	}
	c, err := parseContract(openapi, apiVersion)
	if err != nil {
		t.Fatal(err)
	}
	if c.trials != engineContract.trials {
		t.Fatalf("the started engine holds %+v; the contract file gives %+v", engineContract.trials, c.trials)
	}
}

// T035: a planted contract copy with one value changed fails the comparison, and the failure names
// the item.
func TestAPlantedContractCopyWithOneTrialLimitChangedFailsTheComparison(t *testing.T) {
	openapi, defs := readTrialContract(t)
	engine := engineContract.trials
	if d := trialRuleDiffs(engine, openapi, defs); len(d) > 0 {
		t.Fatalf("the unplanted contract already differs: %v", d)
	}
	text := string(openapi)
	// An x-limits value changed alone: the engine (loaded from the true contract) differs.
	for _, p := range []struct{ key, from, to string }{
		{"trials-max", "  trials-max: 5\n", "  trials-max: 6\n"},
		{"trials-default", "  trials-default: 3\n", "  trials-default: 4\n"},
		{"trial-set-cost-usd", "  trial-set-cost-usd: 2\n", "  trial-set-cost-usd: 3\n"},
		{"trial-set-hours", "  trial-set-hours: 24\n", "  trial-set-hours: 25\n"},
	} {
		t.Run("x-limits."+p.key, func(t *testing.T) {
			if strings.Count(text, p.from) != 1 {
				t.Fatalf("the contract has no single line %q to plant in", p.from)
			}
			d := trialRuleDiffs(engine, []byte(strings.Replace(text, p.from, p.to, 1)), defs)
			if len(d) == 0 || !strings.Contains(d[0], "x-limits."+p.key) {
				t.Fatalf("a changed %s gave %v, want a difference naming it first", p.key, d)
			}
		})
	}
	// A schema value of the openapi.yaml.
	for _, p := range []struct{ name, from, to string }{
		{"TrialSetRequest.trials minimum", "          minimum: 1\n          maximum: 5\n          default: 3\n", "          minimum: 2\n          maximum: 5\n          default: 3\n"},
		{"TrialSetRequest.trials maximum", "          maximum: 5\n          default: 3\n", "          maximum: 6\n          default: 3\n"},
		{"TrialSetRequest.trials default", "          maximum: 5\n          default: 3\n", "          maximum: 5\n          default: 2\n"},
		{"TrialSet.trials maximum", "          minimum: 1\n          maximum: 5\n          description: How many trials the set has, k.\n", "          minimum: 1\n          maximum: 6\n          description: How many trials the set has, k.\n"},
	} {
		t.Run(p.name, func(t *testing.T) {
			if strings.Count(text, p.from) != 1 {
				t.Fatalf("the contract has no single text %q to plant in", p.from)
			}
			d := trialRuleDiffs(engine, []byte(strings.Replace(text, p.from, p.to, 1)), defs)
			if len(d) != 1 || !strings.Contains(d[0], p.name) {
				t.Fatalf("a changed %s gave %v, want exactly one difference naming it", p.name, d)
			}
		})
	}
	// A bound of slice-defs.schema.json.
	for _, field := range []string{"minimum", "maximum"} {
		t.Run("$defs.trial_number."+field, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal(defs, &m); err != nil {
				t.Fatal(err)
			}
			def := m["$defs"].(map[string]any)["trial_number"].(map[string]any)
			v, ok := def[field].(float64)
			if !ok {
				t.Fatalf("$defs.trial_number.%s is %T, not a number", field, def[field])
			}
			def[field] = v + 1
			planted, _ := json.Marshal(m)
			d := trialRuleDiffs(engine, openapi, planted)
			if len(d) != 1 || !strings.Contains(d[0], "trial_number "+field) {
				t.Fatalf("a changed trial_number %s gave %v, want exactly one difference naming it", field, d)
			}
		})
	}
	// A missing definition is a difference (fail closed), not a pass.
	var m map[string]any
	if err := json.Unmarshal(defs, &m); err != nil {
		t.Fatal(err)
	}
	delete(m["$defs"].(map[string]any), "trial_number")
	planted, _ := json.Marshal(m)
	if d := trialRuleDiffs(engine, openapi, planted); len(d) == 0 {
		t.Fatal("a contract without trial_number passed the comparison")
	}
	// A missing x-limits key is a difference too.
	if d := trialRuleDiffs(engine, []byte(strings.Replace(text, "  trial-set-hours: 24\n", "", 1)), defs); len(d) == 0 {
		t.Fatal("a contract without trial-set-hours passed the comparison")
	}
}

// T035: the engine's loader, which engine start uses, refuses a contract whose trial limits are
// unusable, and the error names the key.
func TestTheEngineRefusesUnusableTrialLimits(t *testing.T) {
	raw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseContract(raw, apiVersion); err != nil {
		t.Fatalf("the unmodified contract is refused: %v", err)
	}
	set := func(key, v string) []byte {
		var old string
		for _, l := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(l, "  "+key+":") {
				old = l + "\n"
			}
		}
		if old == "" || strings.Count(string(raw), old) != 1 {
			t.Fatalf("the contract has no single x-limits.%s line", key)
		}
		if v == "" { // the key is removed
			return []byte(strings.Replace(string(raw), old, "", 1))
		}
		return []byte(strings.Replace(string(raw), old, "  "+key+": "+v+"\n", 1))
	}
	refused := func(name string, mutated []byte, key string) {
		t.Run(name, func(t *testing.T) {
			_, err := parseContract(mutated, apiVersion)
			if err == nil {
				t.Fatalf("the engine accepted %s", name)
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("the error does not name %s: %v", key, err)
			}
		})
	}
	for _, key := range []string{"trials-max", "trials-default", "trial-set-hours"} {
		refused(key+" missing", set(key, ""), key)
		refused(key+" zero", set(key, "0"), key)
		refused(key+" negative", set(key, "-1"), key)
		refused(key+" fraction", set(key, "2.5"), key)
		refused(key+" whole fraction", set(key, "3.0"), key)
		refused(key+" quoted", set(key, `"3"`), key)
		refused(key+" null", set(key, "null"), key)
		refused(key+" too large", set(key, "99999999999"), key)
	}
	cost := "trial-set-cost-usd"
	refused(cost+" missing", set(cost, ""), cost)
	refused(cost+" zero", set(cost, "0"), cost)
	refused(cost+" zero fraction", set(cost, "0.0"), cost)
	refused(cost+" negative", set(cost, "-1.5"), cost)
	refused(cost+" negative integer", set(cost, "-2"), cost)
	refused(cost+" words", set(cost, "two"), cost)
	refused(cost+" quoted", set(cost, `"2"`), cost)
	refused(cost+" null", set(cost, "null"), cost)
	refused(cost+" list", set(cost, "[2]"), cost)
	refused(cost+" infinity", set(cost, ".inf"), cost)
	refused(cost+" not a number", set(cost, ".nan"), cost)
	refused(cost+" negative infinity", set(cost, "-.inf"), cost)
	refused("default above the most", set("trials-default", "6"), "trials-default")

	// Valid values the engine accepts: a fractional cost, and a default equal to the most.
	for _, v := range []string{"0.5", "2.25", "7"} {
		if _, err := parseContract(set(cost, v), apiVersion); err != nil {
			t.Errorf("%s %s refused: %v", cost, v, err)
		}
	}
	if _, err := parseContract(set("trials-default", "5"), apiVersion); err != nil {
		t.Errorf("trials-default equal to trials-max refused: %v", err)
	}
	// Each key reaches its own field.
	c, err := parseContract(set("trial-set-hours", "31"), apiVersion)
	if err != nil || c.trials.hours != 31 || c.trials.max != 5 || c.trials.def != 3 {
		t.Errorf("trial-set-hours=31 gave %+v, %v", c.trials, err)
	}
}
