package api

// U0: the Slice schema gains an optional integer artifact_count >= 0, the number of distinct
// artifacts stored for the slice's runs (owner-approved contract change, task T038).

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/api/gen"
)

func TestSliceSchemaHasOptionalArtifactCount(t *testing.T) {
	raw, err := os.ReadFile("preview/2026-11-12-preview/slice.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type    string   `json:"type"`
			Minimum *float64 `json:"minimum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(doc.Required, "artifact_count") {
		t.Errorf("artifact_count must be optional, but slice.schema.json requires it")
	}
	p, ok := doc.Properties["artifact_count"]
	if !ok {
		t.Fatal("slice.schema.json has no artifact_count property")
	}
	if p.Type != "integer" {
		t.Errorf("artifact_count type %q, want integer", p.Type)
	}
	if p.Minimum == nil || *p.Minimum != 0 {
		t.Errorf("artifact_count minimum %v, want 0", p.Minimum)
	}
}

func TestGeneratedSliceHasArtifactCount(t *testing.T) {
	f, ok := reflect.TypeOf(gen.Slice{}).FieldByName("ArtifactCount")
	if !ok {
		t.Fatal("gen.Slice has no ArtifactCount field; regenerate from the contract")
	}
	typ := f.Type
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Int {
		t.Errorf("ArtifactCount is %s, want int or *int", f.Type)
	}
	if tag := strings.Split(f.Tag.Get("json"), ",")[0]; tag != "artifact_count" {
		t.Errorf("ArtifactCount json tag %q, want artifact_count", f.Tag.Get("json"))
	}
}
