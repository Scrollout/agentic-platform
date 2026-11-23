package checks_test

// Regression: a judged file that does not parse fails closed naming the file only. The reason
// never quotes the file's text (a URL literal can carry a password) and never a host path.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/checks"
)

const prContract = "openapi: 3.1.0\n"

const prLong = "http://svc-user:LongSecretValue@internal-host.example.com/path/with/a/very/long/tail/0123456789" //nolint:gosec // planted test data, not a credential

type prCase struct {
	name     string
	body     string
	distinct []string // text the reason must not contain
}

func prCases() []prCase {
	return []prCase{
		{"interpreted", "package a\nvar a = 1 \"http://u:pw@h.example/\"\n", []string{"http://u:pw@h.example/", "u:pw", "pw@", "h.example"}},
		{"raw", "package a\nvar a = 1 `http://u:pw@h.example/`\n", []string{"http://u:pw@h.example/", "u:pw", "pw@", "h.example"}},
		{"longer than 64", "package a\nvar a = 1 \"" + prLong + "\"\n", []string{prLong, "svc-user", "LongSecretValue", "internal-host", "0123456789", "very/long"}},
	}
}

func prProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func prWantSafeFailure(t *testing.T, rs []checks.CheckResult, check, file string, forbidden []string) {
	t.Helper()
	if len(rs) != 1 {
		t.Fatalf("want exactly one result, got %d: %+v", len(rs), rs)
	}
	r := rs[0]
	if r.Check != check || r.Passed {
		t.Fatalf("want %s failing, got %+v", check, r)
	}
	if !strings.Contains(r.Reason, file) {
		t.Errorf("reason must name %s: %q", file, r.Reason)
	}
	for _, f := range forbidden {
		if strings.Contains(r.Reason, f) {
			t.Errorf("reason must not contain %q: %q", f, r.Reason)
		}
	}
}

func TestDeploymentValuesConfiguredUnparseableReasonQuotesNothing(t *testing.T) {
	for _, c := range prCases() {
		t.Run(c.name, func(t *testing.T) {
			dir := prProject(t, map[string]string{"cmd/a.go": c.body})
			rs := checks.DeploymentValuesConfigured(dir, []string{"cmd/**"})
			prWantSafeFailure(t, rs, "DeploymentValuesConfigured", "cmd/a.go", append(c.distinct, dir))
		})
	}
}

func TestDeclaredValuesNotRepeatedUnparseableReasonQuotesNothing(t *testing.T) {
	for _, c := range prCases() {
		t.Run(c.name, func(t *testing.T) {
			dir := prProject(t, map[string]string{"cmd/a.go": c.body})
			rs := checks.DeclaredValuesNotRepeated(dir, []string{"cmd/**"}, []byte(prContract))
			prWantSafeFailure(t, rs, "DeclaredValuesNotRepeated", "cmd/a.go", append(c.distinct, dir))
		})
	}
}

func TestDeclaredValuesNotRepeatedUnparseableGenFileQuotesNothing(t *testing.T) {
	for _, c := range prCases() {
		t.Run(c.name, func(t *testing.T) {
			dir := prProject(t, map[string]string{
				"cmd/a.go":     "package a\n",
				"gen/other.go": c.body,
			})
			rs := checks.DeclaredValuesNotRepeated(dir, []string{"cmd/**"}, []byte(prContract))
			if len(rs) != 1 || rs[0].Passed || rs[0].Check != "DeclaredValuesNotRepeated" {
				t.Fatalf("want exactly one failing DeclaredValuesNotRepeated result, got %+v", rs)
			}
			for _, f := range append(c.distinct, dir) {
				if strings.Contains(rs[0].Reason, f) {
					t.Errorf("reason must not contain %q: %q", f, rs[0].Reason)
				}
			}
		})
	}
}
