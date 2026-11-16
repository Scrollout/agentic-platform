//go:build docker

// T030: conformance of the engine's slice and operation operations to its contract. Schemathesis
// (the pinned image) runs in a container on the lab network (src/tests/lab) against the running
// engine, whose analyst answers from the recorded model; and a Go check proves every Run status
// and outcome combination against the contract's status/outcome rule (T011d, R3-4) using the
// contract's own schema. A missing Docker is a failure, never a skip.
package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-platform/tests/lab"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// What Schemathesis checks for each operation, and the token the operation takes. Every listed
// operation takes the client token; the GET operations also take the page token (the contract's
// `security`), so each of those runs a second time with it.
var operations = []struct {
	id   string
	page bool
}{
	{"createSlice", false},
	{"listSlices", true},
	{"getSlice", true},
	{"replaceGoals", false},
	{"replaceRules", false},
	{"replaceContract", false},
	{"deleteSlice", false},
	{"answerQuestions", false},
	{"approvePart", false},
	{"getOperation", true},
	{"cancelOperation", false},
	{"listProviderOperations", true},
}

func TestSchemathesisAgainstTheEngine(t *testing.T) {
	l := lab.Start(t)
	// One real slice, from the recorded model, so that reads and writes meet an existing slice too.
	l.Create(t, lab.Example)

	opts := []string{"--checks", "all", "--max-examples", "30", "--generation-deterministic",
		"--rate-limit", "15/s", "--no-color", "--coverage-no-report"}
	for _, op := range operations {
		run := func(token, who string) {
			t.Run(op.id+"/"+who, func(t *testing.T) {
				// The contract declares no 405, so an undeclared method's 404 is what it allows
				// (unsupported_method would demand 405). The operations with a `page_token` refuse
				// a token the engine did not issue with 400 InvalidRequest, as the contract says,
				// though any string fits the parameter's schema (positive_data_acceptance).
				exclude := "unsupported_method"
				if op.id == "listSlices" || op.id == "listProviderOperations" {
					exclude += ",positive_data_acceptance"
				}
				out, code := l.Schemathesis(t, token, []string{op.id}, append(opts, "--exclude-checks", exclude)...)
				if code != 0 {
					t.Errorf("schemathesis %s with the %s token exited %d:\n%s", op.id, who, code, out)
				}
				if !strings.Contains(out, "test cases") && !strings.Contains(out, "Test cases") {
					t.Errorf("schemathesis %s ran no test cases:\n%s", op.id, out)
				}
			})
		}
		run(lab.ClientToken, "client")
		if op.page {
			run(lab.PageToken, "page")
		}
	}
	// Whatever Schemathesis sent to the analyst was answered, or failed the operation; no
	// request of the recorded description missed.
	if logs := l.EngineLogs(); strings.Contains(logs, "panic") {
		t.Errorf("the engine panicked:\n%s", logs)
	}
}

// runSchema compiles the contract's Run schema, resolving its references to the schema files
// beside the contract, as the contract declares them.
func runSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	dir, err := filepath.Abs("../../engine/api/preview/2026-11-12-preview")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	doc = jsonable(doc)
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	url := "file://" + filepath.Join(dir, "openapi.json")
	if err := c.AddResource(url, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(url + "#/components/schemas/Run")
	if err != nil {
		t.Fatalf("compiling components/schemas/Run: %v", err)
	}
	return s
}

// jsonable turns YAML's decoded maps into the JSON model the schema library reads.
func jsonable(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = jsonable(e)
		}
		return x
	case map[any]any:
		m := map[string]any{}
		for k, e := range x {
			m[fmt.Sprint(k)] = jsonable(e)
		}
		return m
	case []any:
		for i, e := range x {
			x[i] = jsonable(e)
		}
		return x
	default:
		return v
	}
}

// The contract's rule (RunStatus, RunOutcome and Run): a run that has `ended` has an `outcome`
// and an `ended_at`; a run in any other status has neither.
func TestRunStatusAndOutcomeRule(t *testing.T) {
	s := runSchema(t)
	statuses := []string{"running", "interrupted", "waiting-for-developer", "ended"}
	outcomes := []string{"", "done", "handed-over", "expired", "cancelled"} // "" is no outcome
	n := 0
	for _, status := range statuses {
		for _, outcome := range outcomes {
			for _, endedAt := range []bool{false, true} {
				doc := map[string]any{
					"id": "RUN-1", "slice": "S-1", "pattern": "api-feature-go@1.0.0", "project": "notes",
					"status": status, "operation": "OP-2",
					"created_at": "2026-11-12T10:00:00Z", "updated_at": "2026-11-12T10:05:00Z",
				}
				if outcome != "" {
					doc["outcome"] = outcome
				}
				if endedAt {
					doc["ended_at"] = "2026-11-12T10:05:00Z"
				}
				want := outcome == "" && !endedAt // not ended: neither
				if status == "ended" {
					want = outcome != "" && endedAt // ended: both
				}
				err := s.Validate(doc)
				name := fmt.Sprintf("status=%s outcome=%q ended_at=%v", status, outcome, endedAt)
				if (err == nil) != want {
					t.Errorf("%s: the contract's Run schema says valid=%v, the rule says %v (%v)", name, err == nil, want, err)
				}
				n++
			}
		}
	}
	if n != 40 {
		t.Fatalf("checked %d combinations, want 40", n)
	}
	// Anything outside the enums is invalid whatever else is set.
	for _, bad := range []map[string]any{
		{"status": "paused"},
		{"status": "ended", "outcome": "failed", "ended_at": "2026-11-12T10:05:00Z"},
		{"status": "ended", "outcome": "done", "ended_at": "yesterday"},
	} {
		doc := map[string]any{
			"id": "RUN-1", "slice": "S-1", "pattern": "api-feature-go@1.0.0", "project": "notes",
			"operation": "OP-2", "created_at": "2026-11-12T10:00:00Z", "updated_at": "2026-11-12T10:05:00Z",
		}
		for k, v := range bad {
			doc[k] = v
		}
		if err := s.Validate(doc); err == nil {
			b, _ := json.Marshal(doc)
			t.Errorf("the contract's Run schema accepts %s", b)
		}
	}
	// A run with a field missing is invalid.
	for _, drop := range []string{"id", "slice", "pattern", "project", "status", "operation", "created_at", "updated_at"} {
		doc := map[string]any{
			"id": "RUN-1", "slice": "S-1", "pattern": "api-feature-go@1.0.0", "project": "notes", "status": "running",
			"operation": "OP-2", "created_at": "2026-11-12T10:00:00Z", "updated_at": "2026-11-12T10:05:00Z",
		}
		delete(doc, drop)
		if err := s.Validate(doc); err == nil {
			t.Errorf("the contract's Run schema accepts a run without %s", drop)
		}
	}
}

// The refusals the contract states for the `api-version` parameter and `page_token`, which the
// Schemathesis runs above leave out (see the exclusions there): checked directly.
func TestApiVersionAndPageTokenRefusals(t *testing.T) {
	l := lab.Start(t)
	want := func(path, token string, status int, code string) {
		t.Helper()
		st, _, b := l.Raw("GET", path, token, nil, nil)
		var p lab.Problem
		_ = json.Unmarshal(b, &p)
		if st != status || p.Code != code {
			t.Errorf("GET %s = %d %s, want %d %s", path, st, b, status, code)
		}
	}
	want("/slices", lab.ClientToken, 400, "MissingApiVersionParameter")
	want("/slices?api-version=2026-11-12", lab.ClientToken, 400, "UnsupportedApiVersionValue")
	want("/slices?api-version=0000-00-00", lab.ClientToken, 400, "UnsupportedApiVersionValue")
	want("/slices?api-version=2026-11-12-preview&page_token=not-issued", lab.ClientToken, 400, "InvalidRequest")
	want("/providers/Agentic.Engine/operations?page_token=not-issued", lab.ClientToken, 400, "InvalidRequest")
	// The refusal names the versions the engine serves.
	_, _, b := l.Raw("GET", "/slices?api-version=2026-11-12", lab.ClientToken, nil, nil)
	if !strings.Contains(string(b), lab.APIVersion) {
		t.Errorf("UnsupportedApiVersionValue does not list %s in supported_versions: %s", lab.APIVersion, b)
	}
}
