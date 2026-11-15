// T096 (owner, 2026-11-15; research D39, ADR 0042): NoBlockingFindingOpen's verdict carries
// `confirmed`, the 0-based positions, ascending, in the request's `findings` of the blocking
// findings its witness runs confirmed, on every verdict it gives, `[]` when it passes; no other
// check sends it. Over HTTP only, against the committed check contract
// (src/check/api/preview/2026-11-13-preview/openapi.yaml, CheckVerdict.confirmed). Written before
// the code by a separate test author. Seams: witness_test.go (witBody, witFinding and the
// testdata/witness fixture), helpers_test.go. Needs the Go toolchain and the module cache (the
// witness runs), not git.
package checksvc_test

import (
	"encoding/json"
	"slices"
	"testing"
)

// cnVerdict posts a runCheck and returns its verdict's fields as raw JSON, so an absent
// `confirmed` can be told from an empty one.
func cnVerdict(t *testing.T, base, check string, body map[string]any) map[string]json.RawMessage {
	t.Helper()
	r := post(t, base, "/checks/"+check+"/run", body)
	if r.status != 200 {
		t.Fatalf("%s: want 200, got %d: %.600s", check, r.status, r.body)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("%s: the response is not JSON: %v: %.300s", check, err, r.body)
	}
	return m
}

// cnConfirmed is the verdict's `confirmed`; it fails the test when the verdict has none.
func cnConfirmed(t *testing.T, v map[string]json.RawMessage) []int {
	t.Helper()
	raw, ok := v["confirmed"]
	if !ok {
		t.Fatalf("NoBlockingFindingOpen's verdict has no confirmed: %s", cnShow(v))
	}
	var c []int
	if err := json.Unmarshal(raw, &c); err != nil || c == nil {
		t.Fatalf("confirmed = %s, want an array of positions: %v", raw, err)
	}
	return c
}

func cnPassed(t *testing.T, v map[string]json.RawMessage) bool {
	t.Helper()
	var p bool
	if err := json.Unmarshal(v["passed"], &p); err != nil {
		t.Fatalf("passed = %s: %v", v["passed"], err)
	}
	return p
}

// cnShow is the verdict without its project_files.
func cnShow(v map[string]json.RawMessage) string {
	short := map[string]json.RawMessage{}
	for k, x := range v {
		if k != "project_files" {
			short[k] = x
		}
	}
	b, _ := json.Marshal(short)
	return string(b)
}

// cnMixed: positions 0 and 3 are confirmed by their witnesses (both fail having run greet.go:23);
// 1 is a blocking claim whose witness only prints (a suggestion); 2 is not blocking.
func cnMixed() []map[string]any {
	return []map[string]any{
		witFinding("R-1", witGreet, 23, witR1, witT1),
		witFinding("R-2", witGreet, 23, "review/r2/print_test.go", "TestR2PrintsOnly"),
		finding("R-3", witGreet, 23, false),
		witFinding("R-1", witGreet, 23, "review/r1/elsewhere_test.go", "TestR1Elsewhere"),
	}
}

func TestNoBlockingFindingOpenNamesTheConfirmedPositions(t *testing.T) {
	root, dir := fixture(t, "witness")
	base := serve(t, checkConfig(t, root))

	v := cnVerdict(t, base, "NoBlockingFindingOpen", witBody(t, dir, false, cnMixed()...))
	if cnPassed(t, v) {
		t.Fatalf("NoBlockingFindingOpen passed with two confirmed findings: %s", cnShow(v))
	}
	if got := cnConfirmed(t, v); !slices.Equal(got, []int{0, 3}) {
		t.Errorf("confirmed = %v, want [0 3], ascending: %s", got, cnShow(v))
	}

	// The same findings in another order: the positions follow the request.
	m := cnMixed()
	reordered := []map[string]any{m[3], m[2], m[1], m[0]}
	v = cnVerdict(t, base, "NoBlockingFindingOpen", witBody(t, dir, false, reordered...))
	if got := cnConfirmed(t, v); !slices.Equal(got, []int{0, 3}) {
		t.Errorf("reordered: confirmed = %v, want [0 3]: %s", got, cnShow(v))
	}
	m = cnMixed()
	v = cnVerdict(t, base, "NoBlockingFindingOpen", witBody(t, dir, false, m[1], m[2], m[3]))
	if got := cnConfirmed(t, v); !slices.Equal(got, []int{2}) {
		t.Errorf("one confirmed, last: confirmed = %v, want [2]: %s", got, cnShow(v))
	}
}

func TestNoBlockingFindingOpenSendsAnEmptyConfirmedWhenItPasses(t *testing.T) {
	root, dir := fixture(t, "witness")
	base := serve(t, checkConfig(t, root))
	m := cnMixed()
	cases := []struct {
		name     string
		findings []map[string]any
	}{
		{"no findings", nil},
		{"a suggestion and a blocking claim its witness does not confirm", []map[string]any{m[2], m[1]}},
		{"a blocking claim without a witness", []map[string]any{witFinding("R-1", witGreet, 23, "", "")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := cnVerdict(t, base, "NoBlockingFindingOpen", witBody(t, dir, false, c.findings...))
			if !cnPassed(t, v) {
				t.Fatalf("setup: NoBlockingFindingOpen failed: %s", cnShow(v))
			}
			if got := cnConfirmed(t, v); len(got) != 0 {
				t.Errorf("confirmed = %v, want []: %s", got, cnShow(v))
			}
		})
	}
}

// A verdict that fails before any witness runs (here an output whose hash is not the folder's)
// still carries confirmed, empty.
func TestNoBlockingFindingOpenSendsConfirmedOnEveryVerdict(t *testing.T) {
	root, dir := fixture(t, "witness")
	base := serve(t, checkConfig(t, root))
	b := witBody(t, dir, false, cnMixed()...)
	b["outputs"] = []any{
		map[string]any{"name": "findings", "sha256": sum(nil)},
		map[string]any{"name": "witnesses", "sha256": sum([]byte("not the folder's witnesses"))},
	}
	v := cnVerdict(t, base, "NoBlockingFindingOpen", b)
	if cnPassed(t, v) {
		t.Fatalf("setup: NoBlockingFindingOpen passed with an output that is not the folder's: %s", cnShow(v))
	}
	if got := cnConfirmed(t, v); len(got) != 0 {
		t.Errorf("confirmed = %v, want [] when no witness ran: %s", got, cnShow(v))
	}
}

func TestNoOtherCheckSendsConfirmed(t *testing.T) {
	root, dir := fixture(t, "witness")
	base := serve(t, checkConfig(t, root))
	for _, check := range []string{"FindingsCarryEvidence", "TestsUnchanged", "ScopeKept"} {
		t.Run(check, func(t *testing.T) {
			v := cnVerdict(t, base, check, witBody(t, dir, false, cnMixed()...))
			if raw, ok := v["confirmed"]; ok {
				t.Errorf("%s's verdict sends confirmed %s; only NoBlockingFindingOpen does: %s", check, raw, cnShow(v))
			}
		})
	}
}
