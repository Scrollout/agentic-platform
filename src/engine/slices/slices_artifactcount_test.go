package slices_test

// U0 on the engine side: slices.Slice carries ArtifactCount (JSON artifact_count) and the
// contract's slice schema accepts it.

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/slice"
)

func TestSliceHasArtifactCountField(t *testing.T) {
	f, ok := reflect.TypeOf(slices.Slice{}).FieldByName("ArtifactCount")
	if !ok {
		t.Fatal("slices.Slice has no ArtifactCount field")
	}
	if f.Type.Kind() != reflect.Int {
		t.Errorf("ArtifactCount is %s, want int", f.Type)
	}
	if tag := f.Tag.Get("json"); tag != "artifact_count" {
		t.Errorf("json tag %q, want artifact_count", tag)
	}
}

func TestSliceJSONWithArtifactCountValidates(t *testing.T) {
	schemas, err := slice.LoadSchemas("../api/preview/2026-11-12-preview")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../slice/testdata/slice-valid.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	check := func(v any) error {
		doc["artifact_count"] = v
		b, _ := json.Marshal(doc)
		return schemas.CheckSlice(b)
	}
	for _, n := range []any{0, 3} {
		if err := check(n); err != nil {
			t.Errorf("artifact_count %v rejected: %v", n, err)
		}
	}
	for _, n := range []any{-1, 1.5, "2"} {
		if check(n) == nil {
			t.Errorf("artifact_count %v accepted", n)
		}
	}
	delete(doc, "artifact_count")
	b, _ := json.Marshal(doc)
	if err := schemas.CheckSlice(b); err != nil {
		t.Errorf("a slice without artifact_count is refused; it is optional: %v", err)
	}
}
