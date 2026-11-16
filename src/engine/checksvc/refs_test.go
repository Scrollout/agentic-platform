// T104 (converge Story 2, round 1; data-model "Refs", SC-006): a verdict's `refs` are the check's
// refs, deduped, in order, at most 256 (the check contract's CheckVerdict.refs maxItems), `[]`
// when it names none; a failing FindingsCarryEvidence names the rule of each finding at fault,
// NoBlockingFindingOpen the rule of each confirmed finding. Over HTTP only; written before the
// code by a separate test author. Fixtures: testdata/refs/witness (testdata/witness plus a
// witness for R-3 that fails having run greet.go:23) and testdata/refs/many (300 failing
// scenarios, R-1/E-1 to R-300/E-300).
package checksvc_test

import (
	"fmt"
	"slices"
	"testing"
)

func refsNoFile(rule string) map[string]any {
	return finding(rule, "no-such-file.go", 1, true) // blocking, its file is not a file of the project
}

func TestVerdictCarriesTheCheckRefs(t *testing.T) {
	root, dir := fixture(t, "refs", "witness")
	base := serve(t, checkConfig(t, root))

	// Deduped, in the findings' order.
	v := runCheck(t, base, "FindingsCarryEvidence", witBody(t, dir, false,
		refsNoFile("R-2"), refsNoFile("R-1"), refsNoFile("R-2"), refsNoFile("R-10")))
	if v.Passed {
		t.Fatalf("setup: FindingsCarryEvidence passed findings on a missing file: %s", v.Reason)
	}
	if want := []string{"R-2", "R-1", "R-10"}; !slices.Equal(v.Refs, want) {
		t.Errorf("refs = %v, want %v (deduped, in order)", v.Refs, want)
	}

	// None: a passing verdict, and a failing one that names nothing.
	v = runCheck(t, base, "FindingsCarryEvidence", witBody(t, dir, false, witFinding("R-1", witGreet, 23, witR1, witT1)))
	if !v.Passed {
		t.Fatalf("setup: FindingsCarryEvidence failed: %s", v.Reason)
	}
	if v.Refs == nil || len(v.Refs) != 0 {
		t.Errorf("a passing verdict: refs = %#v, want []", v.Refs)
	}
	r := post(t, base, "/checks/FindingsCarryEvidence/run", witBody(t, dir, false, refsNoFile("R-1")))
	if raw := wantVerdict(t, r, "FindingsCarryEvidence"); raw.Passed || len(raw.Refs) != 1 {
		t.Errorf("setup: one finding on a missing file must fail naming R-1: %+v", raw)
	}

	// Never above 256: 300 failing scenarios name 256 refs, the first 256 in the file's order.
	root2, dir2 := fixture(t, "refs", "many")
	base2 := serve(t, checkConfig(t, root2))
	v = runCheck(t, base2, "AllTestsPass", checkBody(t, dir2, step("code", codeWritable, []string{"code"}, nil, nil), nil, "c", "e"))
	if v.Passed {
		t.Fatalf("setup: AllTestsPass passed 300 failing scenarios")
	}
	if len(v.Refs) != 256 {
		t.Fatalf("refs has %d entries, want 256 (the contract's maxItems)", len(v.Refs))
	}
	for i, ref := range v.Refs {
		if want := fmt.Sprintf("R-%d/E-%d", i+1, i+1); ref != want {
			t.Fatalf("refs[%d] = %q, want %q", i, ref, want)
		}
	}
}

// Of two blocking findings, only the one on R-3 is confirmed by its witness (R-2's only prints):
// NoBlockingFindingOpen names R-3, and FindingsCarryEvidence, which passes, names none.
func TestFindingVerdictsNameTheRulesAtFault(t *testing.T) {
	root, dir := fixture(t, "refs", "witness")
	base := serve(t, checkConfig(t, root))
	fs := []map[string]any{
		witFinding("R-2", witGreet, 23, "review/r2/print_test.go", "TestR2PrintsOnly"),
		witFinding("R-3", witGreet, 23, "review/r3/line_test.go", "TestR3NameLeftOut"),
	}
	v := runCheck(t, base, "NoBlockingFindingOpen", witBody(t, dir, false, fs...))
	if v.Passed {
		t.Fatalf("setup: NoBlockingFindingOpen passed with a confirmed finding: %s", v.Reason)
	}
	if want := []string{"R-3"}; !slices.Equal(v.Refs, want) {
		t.Errorf("refs = %v, want %v", v.Refs, want)
	}
	v = runCheck(t, base, "FindingsCarryEvidence", witBody(t, dir, false, fs...))
	if !v.Passed || len(v.Refs) != 0 {
		t.Errorf("FindingsCarryEvidence: passed=%v refs=%v, want a pass naming none: %s", v.Passed, v.Refs, v.Reason)
	}
}
