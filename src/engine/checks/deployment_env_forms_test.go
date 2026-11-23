// Finding B1 of the US2 review (feature 009, T030): failing tests, by a separate author, for the
// forms of an environment read that DeploymentValuesConfigured must report: any use of os.Getenv,
// os.LookupEnv, os.Environ or os.ExpandEnv through an import name of os (a call, a method value, a
// parenthesised callee, a function value passed as an argument), and, under a dot import of os, the
// bare names. A line marked `// want env` must be reported once as an environment read; a fixture
// without a marker must pass. Pure Go over temp folders: no model, no network, no Docker.
package checks_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/checks"
)

const dvenvSuffix = "belongs in configuration: read it through gen/config.go"

// dvenvRun writes src as cmd/a.go, runs the check and compares the failures with the `// want env`
// markers (one expected failure per marker occurrence on a line).
func dvenvRun(t *testing.T, src string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cmd", "a.go")
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	var wants []string
	for i, l := range strings.Split(src, "\n") {
		if n := strings.Count(l, "// want env"); n > 0 {
			for range n {
				wants = append(wants, fmt.Sprintf("cmd/a.go:%d holds an environment read: %s", i+1, dvenvSuffix))
			}
		}
		if n := strings.Count(l, "// want env x2"); n > 0 {
			wants = append(wants, fmt.Sprintf("cmd/a.go:%d holds an environment read: %s", i+1, dvenvSuffix))
		}
	}
	rs := checks.DeploymentValuesConfigured(dir, []string{"cmd/**"})
	if len(wants) == 0 {
		if len(rs) != 1 || !rs[0].Passed {
			t.Errorf("want one pass, got %+v", rs)
		}
		return
	}
	var got []string
	for _, r := range rs {
		if r.Passed {
			t.Errorf("a failing check must not also pass: %+v", r)
			continue
		}
		got = append(got, r.Reason)
	}
	sort.Strings(wants)
	sort.Strings(got)
	if len(got) != len(wants) {
		t.Errorf("want %d failures %q, got %d: %q", len(wants), wants, len(got), got)
		return
	}
	for i := range wants {
		if got[i] != wants[i] {
			t.Errorf("failure %d: want %q, got %q", i, wants[i], got[i])
		}
	}
}

func dvenvFile(imports, body string) string {
	return "package main\n\nimport (\n" + imports + "\n)\n\n" + body + "\n"
}

func TestDeploymentValuesConfiguredFailsOnDotImportedEnvReads(t *testing.T) {
	cases := map[string]string{
		"Getenv":                           "_ = Getenv(\"X\") // want env",
		"LookupEnv":                        "_, _ = LookupEnv(\"X\") // want env",
		"Environ":                          "_ = Environ() // want env",
		"ExpandEnv":                        "_ = ExpandEnv(\"$X\") // want env",
		"dot import beside a named import": "_ = Getenv(\"X\") // want env\n\t_ = o.Getenv(\"Y\") // want env",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			imports := "\t. \"os\""
			if strings.Contains(name, "named") {
				imports += "\n\to \"os\""
			}
			dvenvRun(t, dvenvFile(imports, "func f() {\n\t"+body+"\n}"))
		})
	}
}

func TestDeploymentValuesConfiguredFailsOnEnvReadMethodValues(t *testing.T) {
	cases := map[string]struct{ imports, body string }{
		"os.Getenv":                            {"\t\"os\"", "get := os.Getenv // want env\n\t_ = get"},
		"alias LookupEnv":                      {"\to \"os\"", "f := o.LookupEnv // want env\n\t_ = f"},
		"reported once, not at the later call": {"\t\"os\"", "get := os.Getenv // want env\n\t_ = get(\"X\")"},
		"os.Environ and os.ExpandEnv":          {"\t\"os\"", "a, b := os.Environ, os.ExpandEnv // want env x2\n\t_, _ = a, b"},
		"var declaration":                      {"\t\"os\"", "var get = os.Getenv // want env\n\t_ = get"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dvenvRun(t, dvenvFile(c.imports, "func f() {\n\t"+c.body+"\n}"))
		})
	}
}

func TestDeploymentValuesConfiguredFailsOnParenthesisedEnvRead(t *testing.T) {
	dvenvRun(t, dvenvFile("\t\"os\"", "func f() {\n\t_ = (os.Getenv)(\"X\") // want env\n}"))
	dvenvRun(t, dvenvFile("\to \"os\"", "func f() {\n\t_, _ = ((o.LookupEnv))(\"X\") // want env\n}"))
}

func TestDeploymentValuesConfiguredFailsOnEnvReadPassedAsArgument(t *testing.T) {
	dvenvRun(t, dvenvFile("\t\"os\"", "func apply(func(string) string) {}\n\nfunc f() {\n\tapply(os.Getenv) // want env\n}"))
	dvenvRun(t, dvenvFile("\to \"os\"", "func apply(func(string) string) {}\n\nfunc f() {\n\tapply(o.ExpandEnv) // want env\n}"))
}

func TestDeploymentValuesConfiguredPassesWhatIsNotAnEnvRead(t *testing.T) {
	cases := map[string]string{
		"local Getenv without a dot import": dvenvFile("\t\"os\"",
			"func Getenv(k string) string { return k }\n\nfunc f() {\n\t_ = Getenv(\"X\")\n\t_ = os.Getpid()\n}"),
		"os.Getpid and os.Exit": dvenvFile("\t\"os\"", "func f() {\n\t_ = os.Getpid()\n\tos.Exit(1)\n}"),
		"os imported as _":      dvenvFile("\t_ \"os\"", "func Getenv(k string) string { return k }\n\nfunc f() {\n\t_ = Getenv(\"X\")\n}"),
		"method named Getenv on another value": dvenvFile("\t\"os\"",
			"type x struct{}\n\nfunc (x) Getenv(string) string { return \"\" }\n\nfunc f() {\n\tvar v x\n\t_ = v.Getenv(\"X\")\n\tget := v.Getenv\n\t_ = get\n\t_ = os.Getpid\n}"),
		"os-named local shadowing the import is not os": dvenvFile("\t\"fmt\"",
			"type osT struct{}\n\nfunc (osT) Getenv(string) string { return \"\" }\n\nfunc f() {\n\tos := osT{}\n\t_ = os.Getenv(\"X\")\n\tfmt.Println()\n}"),
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) { dvenvRun(t, src) })
	}
}
