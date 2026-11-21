package analyst

// Quarantine of live eval cases (owner-approved, T185), checked with no model and no Docker: a
// quarantined case names an open backlog task, exactly the approved cases are quarantined, and
// OpenTask finds an open task line by its exact ID.

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// quarantineRoot finds the repository root; harness_evals_test.go has its own repoRoot but sits
// behind the evals tag, and this file runs without it.
func quarantineRoot(t testing.TB) string {
	t.Helper()
	d, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(d, "src", "deploy", "images.lock")); err == nil {
			return d
		}
		p := filepath.Dir(d)
		if p == d {
			t.Fatal("repository root not found (no src/deploy/images.lock above the working directory)")
		}
		d = p
	}
}

func TestQuarantinedCasesNameAnOpenBacklogTask(t *testing.T) {
	backlog, err := os.ReadFile(filepath.Join(quarantineRoot(t), "specs", "backlog.md"))
	if err != nil {
		t.Fatal(err)
	}
	idForm := regexp.MustCompile(`^T[0-9]+$`)
	examined := 0
	check := func(name, q string) {
		examined++
		if q == "" {
			return
		}
		if !idForm.MatchString(q) {
			t.Errorf("case %s: Quarantine %q is not a task ID (T<digits>)", name, q)
		}
		if !OpenTask(backlog, q) {
			t.Errorf("case %s: Quarantine %q is not an open task in specs/backlog.md", name, q)
		}
	}
	for _, c := range Cases() {
		check(c.Name, c.Quarantine)
	}
	for _, c := range RevisionCases() {
		check(c.Name, c.Quarantine)
	}
	if examined == 0 {
		t.Fatal("no case examined")
	}
}

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
	want := map[string]string{"example": "T185", "revise-inject-approvals-field": "T185"}
	if len(got) != len(want) {
		t.Errorf("quarantined cases = %v, want exactly %v", got, want)
	}
	for n, q := range want {
		if got[n] != q {
			t.Errorf("case %s: Quarantine = %q, want %q (all: %v)", n, got[n], q, got)
		}
	}
}

func TestOpenTask(t *testing.T) {
	backlog := []byte("# Backlog\n" +
		"- [ ] T185 Quarantined evals\n" +
		"- [X] T186 Done task\n" +
		"- [x] T187 Done lowercase\n" +
		"- [ ] T1850 Longer id\n" +
		"- [ ] T200 See also T185 and T300 here\n" +
		"Note: - [ ] T400 not at line start\n")
	cases := []struct {
		name, id string
		want     bool
	}{
		{"open task", "T185", true},
		{"closed task X", "T186", false},
		{"closed task x", "T187", false},
		{"prefix of a longer id is not a match", "T18", false},
		{"longer id open", "T1850", true},
		{"id that is a prefix of an open id", "T1", false},
		{"mentioned mid-line only", "T300", false},
		{"mentioned mid-line, task itself absent", "T400", false},
		{"unknown task", "T999", false},
		{"empty id", "", false},
	}
	for _, c := range cases {
		if got := OpenTask(backlog, c.id); got != c.want {
			t.Errorf("%s: OpenTask(%q) = %v, want %v", c.name, c.id, got, c.want)
		}
	}
	if OpenTask(nil, "T185") || OpenTask([]byte(""), "T185") {
		t.Error("an empty backlog holds no open task")
	}
	if !OpenTask([]byte("- [ ] T185 last line, no newline"), "T185") {
		t.Error("an open task on the last line without a newline is open")
	}
	if !OpenTask([]byte("- [X] T185 done\n- [ ] T185 reopened\n"), "T185") {
		t.Error("an open line after a closed one with the same ID is open")
	}
}
