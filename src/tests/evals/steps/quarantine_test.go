package steps

// Quarantine of live eval cases, checked with no model and no Docker: no case is quarantined (T202,
// which held review/clean out, was closed by feature 010: a malformed request is refused, and a
// grader judges it). That a quarantine names an open task, should one return, is checked by
// scripts/tests/test_eval_quarantine_open_task.py, which runs on the whole repository.

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
	if len(got) != 0 {
		t.Errorf("quarantined cases = %v, want none", got)
	}
	if !seen["review/clean"] {
		t.Error("case review/clean is not declared")
	}
}
