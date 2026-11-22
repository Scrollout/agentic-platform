package analyst

// Quarantine of live eval cases (owner-approved, T195), checked with no model and no Docker:
// exactly the approved cases are quarantined. That each quarantine names an open backlog task is
// checked by scripts/tests/test_eval_quarantine_open_task.py, which runs on the whole repository.

import "testing"

func TestExactlyTheApprovedCasesAreQuarantined(t *testing.T) {
	got := map[string]string{}
	for _, c := range Cases() {
		if c.Quarantine != "" {
			got[c.Name] = c.Quarantine
		}
	}
	for _, c := range RevisionCases() {
		if c.Quarantine != "" {
			if _, dup := got[c.Name]; dup {
				t.Errorf("case name %s is in both case lists", c.Name)
			}
			got[c.Name] = c.Quarantine
		}
	}
	want := map[string]string{"revise-inject-approvals-field": "T195"}
	if len(got) != len(want) {
		t.Errorf("quarantined cases = %v, want exactly %v", got, want)
	}
	for n, q := range want {
		if got[n] != q {
			t.Errorf("case %s: Quarantine = %q, want %q (all: %v)", n, got[n], q, got)
		}
	}
}
