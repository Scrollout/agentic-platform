package analyst

// Harness inputs (spec FR-010, research R3), checked with no model and no Docker:
// Selection reads EVAL_CASES and TrialCount reads EVAL_TRIALS.

import (
	"reflect"
	"strings"
	"testing"
)

func caseNames() []string {
	var n []string
	for _, c := range Cases() {
		n = append(n, c.Name)
	}
	return n
}

func revisionNames() []string {
	var n []string
	for _, c := range RevisionCases() {
		n = append(n, c.Name)
	}
	return n
}

func selNames(t *testing.T, env string) (cs, rs []string) {
	t.Helper()
	cases, revs, err := Selection(env)
	if err != nil {
		t.Fatalf("Selection(%q) failed: %v", env, err)
	}
	for _, c := range cases {
		cs = append(cs, c.Name)
	}
	for _, r := range revs {
		rs = append(rs, r.Name)
	}
	return cs, rs
}

func TestSelectionEmptyGivesEverything(t *testing.T) {
	cs, rs := selNames(t, "")
	if !reflect.DeepEqual(cs, caseNames()) || !reflect.DeepEqual(rs, revisionNames()) {
		t.Fatalf("empty env: got %v / %v, want every case of both suites", cs, rs)
	}
	if len(cs) == 0 || len(rs) == 0 {
		t.Fatal("both suites must have cases")
	}
}

func TestSelectionNamedCasesOfOneSuiteLeaveTheOtherEmpty(t *testing.T) {
	cs, rs := selNames(t, "example")
	if !reflect.DeepEqual(cs, []string{"example"}) || len(rs) != 0 {
		t.Fatalf("example: got %v / %v", cs, rs)
	}
	cs, rs = selNames(t, "revise-inject-approvals-field")
	if len(cs) != 0 || !reflect.DeepEqual(rs, []string{"revise-inject-approvals-field"}) {
		t.Fatalf("revision only: got %v / %v", cs, rs)
	}
}

func TestSelectionMixedSuites(t *testing.T) {
	cs, rs := selNames(t, "revise-inject-approvals-field,example")
	if !reflect.DeepEqual(cs, []string{"example"}) || !reflect.DeepEqual(rs, []string{"revise-inject-approvals-field"}) {
		t.Fatalf("mixed: got %v / %v", cs, rs)
	}
}

func TestSelectionTrimsSpacesAndCollapsesDuplicates(t *testing.T) {
	cs, rs := selNames(t, "  example , example,revise-inject-approvals-field  ,revise-inject-approvals-field ")
	if !reflect.DeepEqual(cs, []string{"example"}) || !reflect.DeepEqual(rs, []string{"revise-inject-approvals-field"}) {
		t.Fatalf("trim/dedup: got %v / %v", cs, rs)
	}
}

func TestSelectionKeepsEachSuitesOwnOrder(t *testing.T) {
	all := caseNames()
	if len(all) < 2 {
		t.Skip("needs two cases")
	}
	// ask in reverse order; the result follows Cases()
	var rev []string
	for i := len(all) - 1; i >= 0; i-- {
		rev = append(rev, all[i])
	}
	cs, rs := selNames(t, strings.Join(rev, ","))
	if !reflect.DeepEqual(cs, all) || len(rs) != 0 {
		t.Fatalf("case order: got %v, want %v (revisions %v)", cs, all, rs)
	}
	rall := revisionNames()
	var rrev []string
	for i := len(rall) - 1; i >= 0; i-- {
		rrev = append(rrev, rall[i])
	}
	cs, rs = selNames(t, strings.Join(rrev, ","))
	if !reflect.DeepEqual(rs, rall) || len(cs) != 0 {
		t.Fatalf("revision order: got %v, want %v (cases %v)", rs, rall, cs)
	}
}

func TestSelectionRejectsBadNames(t *testing.T) {
	for _, c := range []struct{ env, want string }{
		{"nope", "nope"},
		{"example,nope", "nope"},
		{"Example", "Example"},
		{"example,", "empty"},
		{",", "empty"},
		{" ", "empty"},
		{"a,,b", ""},
		{"example, ,revise-inject-approvals-field", "empty"},
	} {
		cs, rs, err := Selection(c.env)
		if err == nil {
			t.Errorf("Selection(%q) = %d/%d cases, want an error", c.env, len(cs), len(rs))
			continue
		}
		if c.want != "" && !strings.Contains(err.Error(), c.want) {
			t.Errorf("Selection(%q) error %q does not mention %q", c.env, err, c.want)
		}
	}
	// "a,,b": the error names the unknown name or says it is empty
	if _, _, err := Selection("a,,b"); err == nil || (!strings.Contains(err.Error(), "empty") && !strings.Contains(err.Error(), "a")) {
		t.Errorf("a,,b: error %v", err)
	}
}

func TestTrialCountDefault(t *testing.T) {
	k, err := TrialCount("")
	if err != nil || k != Trials {
		t.Fatalf("TrialCount(\"\") = %d, %v; want Trials", k, err)
	}
	if want := 3; k != want {
		t.Fatalf("the default is %d, want %d", k, want)
	}
}

func TestTrialCountAccepts(t *testing.T) {
	for _, c := range []struct {
		env  string
		want int
	}{{"1", 1}, {"3", 3}, {"9", 9}, {"10", 10}, {"19", 19}, {"20", 20}} {
		k, err := TrialCount(c.env)
		if err != nil || k != c.want {
			t.Errorf("TrialCount(%q) = %d, %v; want %d", c.env, k, err, c.want)
		}
	}
}

func TestTrialCountRejects(t *testing.T) {
	for _, env := range []string{"0", "21", "99", "-1", "+5", "010", "05", " 10", "10 ", "1e1", "3.5", "x", "١٠", "100", " ", "1 0", "\n3"} {
		if k, err := TrialCount(env); err == nil {
			t.Errorf("TrialCount(%q) = %d, want an error", env, k)
		}
	}
}
