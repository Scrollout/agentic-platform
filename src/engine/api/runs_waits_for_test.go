package api

// T034: a run's waits_for tells a run waiting for its step workers apart from one that waits to be
// resumed. Written from the engine contract (Run.waits_for, RunWaitsFor) alone.

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"

	"github.com/agentic-platform/engine/api/gen"
)

const workersDetail = "the step workers are not running"

// runSchemaCheck validates body against the contract's Run (or RunList) schema.
func runSchemaCheck(t *testing.T, name string, body []byte) {
	t.Helper()
	raw, err := fs.ReadFile(contractFiles, contractPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	asJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	root, err := jsonschema.UnmarshalJSON(bytes.NewReader(asJSON))
	if err != nil {
		t.Fatal(err)
	}
	comp := jsonschema.NewCompiler()
	comp.DefaultDraft(jsonschema.Draft2020)
	comp.UseLoader(contractLoader{files: contractFiles, dir: specDir})
	if err := comp.AddResource(schemaBase+"openapi.yaml", root); err != nil {
		t.Fatal(err)
	}
	sch, err := comp.Compile(schemaBase + "openapi.yaml#/components/schemas/" + name)
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if err := sch.Validate(v); err != nil {
		t.Fatalf("the body does not validate as %s: %v\n%s", name, err, body)
	}
}

func TestARunsWaitsForTellsStepWorkersFromResume(t *testing.T) {
	cases := []struct {
		name     string
		kinds    []string
		status   string
		waitsFor string // "" means absent
	}{
		{"waiting for step workers", []string{"RunInterrupted=" + workersDetail}, "interrupted", "step-workers"},
		{"interrupted for another reason", []string{"RunInterrupted=the model was unavailable on every attempt"}, "interrupted", "resume"},
		{"interrupted with no detail", []string{"RunInterrupted"}, "interrupted", "resume"},
		{"detail only resembling the workers one", []string{"RunInterrupted=the step workers are not running anymore"}, "interrupted", "resume"},
		{"running", nil, "running", ""},
		{"waiting for the developer", []string{"RunHandedOver=the step's checks failed 3 times"}, "waiting-for-developer", ""},
		{"ended done", []string{"RunCompleted"}, "ended", ""},
		{"ended cancelled", []string{"RunCancelled=the run was cancelled"}, "ended", ""},
		{"ended handed over", []string{"RunHandedOver=x", "HandOverAccepted"}, "ended", ""},
		{"resumed after a workers interruption", []string{"RunInterrupted=" + workersDetail, "RunResumed"}, "running", ""},
		{"cancelled while waiting for workers", []string{"RunInterrupted=" + workersDetail, "RunCancelled=the run was cancelled"}, "ended", ""},
		{"workers interruption then another reason", []string{"RunInterrupted=" + workersDetail, "RunResumed", "RunInterrupted=the model was unavailable on every attempt"}, "interrupted", "resume"},
		{"other interruption then workers", []string{"RunInterrupted=the model was unavailable on every attempt", "RunResumed", "RunInterrupted=" + workersDetail}, "interrupted", "step-workers"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := newDurableRig(t, true)
			x.record(c.kinds...)
			check := func(what string, body []byte, v map[string]any) {
				t.Helper()
				if v["status"] != c.status {
					t.Fatalf("%s: status = %v, want %s (the record did not give the expected state)", what, v["status"], c.status)
				}
				got, present := v["waits_for"]
				if c.waitsFor == "" {
					if present {
						t.Errorf("%s: waits_for = %v, want it absent for a run that is %s", what, got, c.status)
					}
					return
				}
				if got != c.waitsFor {
					t.Errorf("%s: waits_for = %v (present %v), want %q", what, got, present, c.waitsFor)
				}
			}
			p := x.do("GET", "/runs/RUN-1?api-version="+apiVersion, "")
			if p.Code != 200 {
				t.Fatalf("getRun = %d: %s", p.Code, p.Body.String())
			}
			var one map[string]any
			if err := json.Unmarshal(p.Body.Bytes(), &one); err != nil {
				t.Fatal(err)
			}
			check("getRun", p.Body.Bytes(), one)
			runSchemaCheck(t, "Run", p.Body.Bytes())

			l := x.do("GET", "/runs?api-version="+apiVersion, "")
			if l.Code != 200 {
				t.Fatalf("listRuns = %d: %s", l.Code, l.Body.String())
			}
			var list struct {
				Items []map[string]any `json:"items"`
			}
			if err := json.Unmarshal(l.Body.Bytes(), &list); err != nil {
				t.Fatal(err)
			}
			if len(list.Items) != 1 {
				t.Fatalf("listRuns items = %d, want 1: %s", len(list.Items), l.Body.String())
			}
			check("listRuns", l.Body.Bytes(), list.Items[0])
			runSchemaCheck(t, "RunList", l.Body.Bytes())
		})
	}
}

// The generated type carries the field, so a client reads it as a value of the contract's enum.
func TestARunsWaitsForDecodesIntoTheGeneratedType(t *testing.T) {
	x := newDurableRig(t, true)
	x.record("RunInterrupted=" + workersDetail)
	p := x.do("GET", "/runs/RUN-1?api-version="+apiVersion, "")
	var r gen.Run
	if err := json.Unmarshal(p.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.WaitsFor == nil || *r.WaitsFor != gen.RunWaitsForStepWorkers {
		t.Fatalf("WaitsFor = %v, want step-workers; body: %s", r.WaitsFor, p.Body.String())
	}
}
