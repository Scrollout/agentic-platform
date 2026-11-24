package steps

// Quarantine of live eval cases, checked with no model and no Docker: exactly the approved case is
// quarantined, under an open backlog task (T202). That each quarantine names an open task is
// checked by scripts/tests/test_eval_quarantine_open_task.py, which runs on the whole repository.

import "testing"

func TestExactlyTheApprovedCasesAreQuarantined(t *testing.T) {
	got := map[string]string{}
	seen := map[string]bool{}
	for _, c := range Cases() {
		if seen[c.Name] {
			t.Errorf("case name %s appears twice", c.Name)
		}
		seen[c.Name] = true
		if c.Quarantine != "" {
			got[c.Name] = c.Quarantine
		}
	}
	want := map[string]string{"review/clean": "T202"}
	if len(got) != len(want) {
		t.Errorf("quarantined cases = %v, want exactly %v", got, want)
	}
	for n, q := range want {
		if !seen[n] {
			t.Errorf("case %s is not declared", n)
		}
		if got[n] != q {
			t.Errorf("case %s: Quarantine = %q, want %q (all: %v)", n, got[n], q, got)
		}
	}
}
