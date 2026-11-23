// T008 (feature 009): failing tests, by a separate author, for the code-step check
// DeploymentValuesConfigured. The contract they follow is the catalogue README ("Deployment
// values"), the check contract's runCheck description and the Go doc comment of values.go; the
// bodies are not consulted. Pure Go over temp folders: no model, no network, no Docker.
//
// A fixture marks each offence it holds with a trailing comment `// want <kind>[, <kind>...]`
// (kinds: address, credential, env); a fixture with no marker must pass.
package checks_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/checks"
)

const (
	dvcfgSuffix = "belongs in configuration: read it through gen/config.go"
	dvcfgPass   = "no address, credential or environment read"
)

var dvcfgKinds = map[string]string{ //nolint:gosec // kind names, not credentials
	"address":    "an address",
	"credential": "a credential",
	"env":        "an environment read",
}

// dvcfgCase is one fixture file cmd/a.go: package main, the imports, top-level declarations and
// the statements of one function.
type dvcfgCase struct {
	name    string
	imports []string // import specs as written; nil is net/http and os
	top     string
	body    string
}

func (c dvcfgCase) source() string {
	imports := c.imports
	if imports == nil {
		imports = []string{`"net/http"`, `"os"`}
	}
	var b strings.Builder
	b.WriteString("package main\n\n")
	if len(imports) > 0 {
		b.WriteString("import (\n")
		for _, i := range imports {
			b.WriteString("\t" + i + "\n")
		}
		b.WriteString(")\n\n")
	}
	b.WriteString(c.top + "\n")
	b.WriteString("func f(req *http.Request, w http.ResponseWriter, tok, user string) {\n")
	b.WriteString(c.body + "\n}\n")
	return b.String()
}

func dvcfgProject(t *testing.T, files map[string]string) string {
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

// dvcfgWants are the "<file>:<line> holds <kind>:" prefixes the markers of src promise.
func dvcfgWants(t *testing.T, file, src string) []string {
	t.Helper()
	var out []string
	for i, l := range strings.Split(src, "\n") {
		_, after, ok := strings.Cut(l, "// want ")
		if !ok {
			continue
		}
		for _, k := range strings.Split(after, ",") {
			kind, known := dvcfgKinds[strings.TrimSpace(k)]
			if !known {
				t.Fatalf("fixture line %d: unknown kind %q", i+1, k)
			}
			out = append(out, fmt.Sprintf("%s:%d holds %s:", file, i+1, kind))
		}
	}
	return out
}

// dvcfgJudge checks the results against the prefixes wanted: with none, exactly one passing result
// that says how many files it examined; otherwise exactly one failing result per prefix, each
// ending with the configuration hint, and no passing result.
func dvcfgJudge(t *testing.T, rs []checks.CheckResult, wants []string, files int) {
	t.Helper()
	for _, r := range rs {
		if r.Check != "DeploymentValuesConfigured" || r.Part != "project" {
			t.Errorf("every result must be the project's DeploymentValuesConfigured, got %+v", r)
		}
	}
	if len(wants) == 0 {
		want := fmt.Sprintf("examined %d files: %s", files, dvcfgPass)
		if len(rs) != 1 || !rs[0].Passed || rs[0].Reason != want {
			t.Errorf("want one pass %q, got %+v", want, rs)
		}
		return
	}
	wants = slices.Clone(wants)
	sort.Strings(wants)
	var got []string
	for _, r := range rs {
		if r.Passed {
			t.Errorf("a failing check must not also pass: %+v", r)
			continue
		}
		if !strings.HasSuffix(r.Reason, dvcfgSuffix) {
			t.Errorf("reason must end %q: %q", dvcfgSuffix, r.Reason)
		}
		got = append(got, r.Reason)
	}
	if len(got) != len(wants) {
		t.Errorf("want %d failures %q, got %d: %q", len(wants), wants, len(got), got)
		return
	}
	sort.Strings(got)
	for i, w := range wants {
		if !strings.HasPrefix(got[i], w) {
			t.Errorf("failure %d: want prefix %q, got %q", i, w, got[i])
		}
	}
}

func dvcfgRun(t *testing.T, c dvcfgCase) {
	t.Helper()
	src := c.source()
	dir := dvcfgProject(t, map[string]string{"cmd/a.go": src})
	rs := checks.DeploymentValuesConfigured(dir, []string{"cmd/**"})
	dvcfgJudge(t, rs, dvcfgWants(t, "cmd/a.go", src), 1)
}

func dvcfgRunAll(t *testing.T, cases []dvcfgCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { dvcfgRun(t, c) })
	}
}

// dvcfgLiteral is a statement that holds lit as a string literal, marked want when want is set.
func dvcfgLiteral(lit, want string) dvcfgCase {
	body := "\t_ = " + fmt.Sprintf("%q", lit)
	if want != "" {
		body += " // want " + want
	}
	return dvcfgCase{name: "literal " + lit, body: body}
}

// ---- addresses -------------------------------------------------------------------------------

func TestDeploymentValuesConfiguredFailsOnAddresses(t *testing.T) {
	var cases []dvcfgCase
	for _, lit := range []string{"http://x.example/a", "grpc://h:1", "10.0.0.1", "1.2.3.4", "::1", ":8080",
		"localhost:1", "notes.local:443", "note:80", "[::1]:80", ":65535", ":1"} {
		cases = append(cases, dvcfgLiteral(lit, "address"))
	}
	cases = append(cases,
		dvcfgCase{name: "raw string", body: "\t_ = `http://x.example/a` // want address"},
		dvcfgCase{name: "parenthesised", body: "\t_ = (\":8080\") // want address"},
		dvcfgCase{name: "as an argument", body: "\t_ = http.ListenAndServe(\":8080\", nil) // want address"},
		dvcfgCase{name: "two lines", body: "\t_ = \"10.0.0.1\" // want address\n\t_ = \"::1\" // want address"},
	)
	dvcfgRunAll(t, cases)
}

func TestDeploymentValuesConfiguredIgnoresWhatIsNotAnAddress(t *testing.T) {
	var cases []dvcfgCase
	for _, lit := range []string{"12:30", "15:04", "a:b", "1.2.3", ":0", ":65536", ":70000", "localhost", "text",
		"/notes", "", "http://", "application/json", "note:", "name:80x"} {
		cases = append(cases, dvcfgLiteral(lit, ""))
	}
	dvcfgRunAll(t, cases)
}

// ---- credentials -----------------------------------------------------------------------------

func TestDeploymentValuesConfiguredFailsOnCredentials(t *testing.T) {
	dvcfgRunAll(t, []dvcfgCase{
		{name: "SetBasicAuth literal password", body: "\treq.SetBasicAuth(user, \"pw\") // want credential"},
		{name: "SetBasicAuth literal user", body: "\treq.SetBasicAuth(\"u\", tok) // want credential"},
		{name: "SetBasicAuth both literals", body: "\treq.SetBasicAuth(\"u\", \"pw\") // want credential, credential"},
		{name: "Authorization via Header().Set", body: "\tw.Header().Set(\"authorization\", \"Bearer x\") // want credential"},
		{name: "Authorization", body: "\treq.Header.Set(\"Authorization\", \"Bearer x\") // want credential"},
		{name: "AUTHORIZATION", body: "\treq.Header.Set(\"AUTHORIZATION\", \"Bearer x\") // want credential"},
		{name: "Proxy-Authorization", body: "\treq.Header.Set(\"Proxy-Authorization\", \"Basic eA==\") // want credential"},
		{name: "Cookie", body: "\treq.Header.Add(\"cookie\", \"sid=1\") // want credential"},
		{name: "Add X-Api-Key", body: "\treq.Header.Add(\"X-Api-Key\", \"k\") // want credential"},
		{name: "x-api-key lower case", body: "\treq.Header.Set(\"x-api-key\", \"k\") // want credential"},
		{name: "joined literals as value", body: "\treq.Header.Set(\"Authorization\", \"a\"+\"b\") // want credential"},
		{name: "joined literals whose parts are not credentials alone", body: "\treq.Header.Set(\"Authorization\", \"Bearer \"+\"x\") // want credential"},
		{name: "parenthesised value", body: "\treq.Header.Set(\"Authorization\", (\"x\")) // want credential"},
		{name: "short declaration", body: "\tpassword := \"p\" // want credential"},
		{name: "short declaration of a joined literal", body: "\tpassword := \"pa\" + \"ss\" // want credential"},
		{name: "assignment", body: "\tvar passwd string\n\tpasswd = \"p\" // want credential"},
		{name: "field assignment", body: "\tvar c struct{ APIKey string }\n\tc.APIKey = \"k\" // want credential"},
		{name: "api_key name", body: "\tapi_key := \"k\" // want credential"},
		{name: "private key name", body: "\tprivateKey := \"k\" // want credential"},
		{name: "credentials name", body: "\tcredentials := \"k\" // want credential"},
		{name: "const", body: "\tconst apiToken = \"t\" // want credential"},
		{name: "package var", top: "var dbPassword = \"p\" // want credential\n", body: ""},
		{name: "struct literal", body: "\t_ = struct{ Secret string }{Secret: \"s\"} // want credential"},
		{name: "map literal key", body: "\t_ = map[string]string{\"token\": \"t\"} // want credential"},
		{name: "raw string", body: "\tpassword := `p` // want credential"},
		{name: "upper case name", body: "\tPASSWORD := \"p\" // want credential"},
	})
}

// The README states these as limits of the check, not as intent: the tests pin them so that a
// change of the rule is a recorded change of the contract.
func TestDeploymentValuesConfiguredStatedLimitsOfCredentials(t *testing.T) {
	dvcfgRunAll(t, []dvcfgCase{
		{name: "const tokenHeader holds a header name and is judged a credential", body: "\tconst tokenHeader = \"X-Api-Key\" // want credential"},
		{name: "secretaryNote is judged a credential", body: "\tsecretaryNote := \"x\" // want credential"},
	})
}

func TestDeploymentValuesConfiguredIgnoresWhatIsNotACredential(t *testing.T) {
	dvcfgRunAll(t, []dvcfgCase{
		{name: "Set of another header", body: "\treq.Header.Set(\"Content-Type\", \"text/plain\")"},
		{name: "Add of another header", body: "\treq.Header.Add(\"Accept\", \"text/plain\")"},
		{name: "value from a variable", body: "\treq.Header.Set(\"Authorization\", tok)"},
		{name: "empty literal value", body: "\treq.Header.Set(\"Authorization\", \"\")"},
		{name: "empty joined value", body: "\treq.Header.Add(\"X-Api-Key\", \"\"+\"\")"},
		{name: "SetBasicAuth with variables", body: "\treq.SetBasicAuth(user, tok)"},
		{name: "SetBasicAuth with an empty literal", body: "\treq.SetBasicAuth(user, \"\")"},
		{name: "empty literal to a credential name", body: "\tpassword := \"\""},
		{name: "variable to a credential name", body: "\tpassword := tok"},
		{name: "literal to another name", body: "\tgreeting := \"hello\""},
		{name: "header name from a variable", body: "\treq.Header.Set(user, \"x\")"},
	})
}

// ---- environment reads -----------------------------------------------------------------------

func TestDeploymentValuesConfiguredFailsOnEnvironmentReads(t *testing.T) {
	dvcfgRunAll(t, []dvcfgCase{
		{name: "Getenv", body: "\t_ = os.Getenv(\"HOME\") // want env"},
		{name: "Getenv of a variable", body: "\t_ = os.Getenv(user) // want env"},
		{name: "LookupEnv", body: "\t_, _ = os.LookupEnv(\"HOME\") // want env"},
		{name: "Environ", body: "\t_ = os.Environ() // want env"},
		{name: "ExpandEnv", body: "\t_ = os.ExpandEnv(user) // want env"},
		{name: "through an import alias", imports: []string{`"net/http"`, `e "os"`}, body: "\t_ = e.Getenv(\"HOME\") // want env"},
		{name: "alias beside a plain import", imports: []string{`"net/http"`, `"os"`, `e "os"`}, body: "\t_, _ = e.LookupEnv(\"HOME\") // want env\n\t_ = os.Environ() // want env"},
		{name: "two reads on one line", body: "\t_, _ = os.Getenv(\"A\"), os.Getenv(\"B\") // want env, env"},
		{name: "as an argument", body: "\t_ = http.ListenAndServe(os.Getenv(\"ADDR\"), nil) // want env"},
	})
}

func TestDeploymentValuesConfiguredIgnoresWhatIsNotAnEnvironmentRead(t *testing.T) {
	const notThePackage = "type conf struct{}\n\nfunc (conf) Getenv(s string) string { return s }\n\nvar os conf\n"
	dvcfgRunAll(t, []dvcfgCase{
		{name: "other functions of os", body: "\t_ = os.Args\n\t_, _ = os.Hostname()\n\t_ = os.Getpid()"},
		{
			name:    "a value named os, os imported under _",
			imports: []string{`"net/http"`, `_ "os"`},
			top:     notThePackage,
			body:    "\t_ = os.Getenv(user)",
		},
		{
			name:    "a value named os, the package not imported",
			imports: []string{`"net/http"`},
			top:     notThePackage,
			body:    "\t_ = os.Getenv(user)",
		},
		{
			name:    "a value named os, the package imported under another name",
			imports: []string{`"net/http"`, `e "os"`},
			top:     notThePackage,
			body:    "\t_ = e.Args\n\t_ = os.Getenv(user)",
		},
	})
}

// ---- several offences, one offence ----------------------------------------------------------

func TestDeploymentValuesConfiguredFailsOncePerOffenceOnOneLine(t *testing.T) {
	dvcfgRunAll(t, []dvcfgCase{
		{name: "two addresses", body: "\t_, _ = \"10.0.0.1\", \":8080\" // want address, address"},
		{name: "an environment read and an address", body: "\t_, _ = os.Getenv(\"A\"), \"10.0.0.1\" // want env, address"},
		{name: "a credential and an address", body: "\tpassword, _ := \"p\", \"::1\" // want credential, address"},
		{name: "the same address twice", body: "\t_, _ = \":8080\", \":8080\" // want address, address"},
	})
}

// The one literal is a credential and an address (a URL with userinfo): it is one offence.
func TestDeploymentValuesConfiguredReportsAnAddressInACredentialCallOnce(t *testing.T) {
	for _, body := range []string{
		"\treq.Header.Set(\"Authorization\", \"http://u:p@h.example/x\")",
		"\tpassword := \"postgres://u:p@db.example:5432/x\"",
		"\treq.SetBasicAuth(user, \"10.0.0.1\")",
	} {
		t.Run(strings.TrimSpace(body), func(t *testing.T) {
			src := dvcfgCase{body: body}.source()
			dir := dvcfgProject(t, map[string]string{"cmd/a.go": src})
			rs := checks.DeploymentValuesConfigured(dir, []string{"cmd/**"})
			if len(rs) != 1 || rs[0].Passed {
				t.Fatalf("want exactly one failure for one literal, got %+v", rs)
			}
			line := 0
			for i, l := range strings.Split(src, "\n") {
				if strings.Contains(l, strings.TrimSpace(body)) {
					line = i + 1
				}
			}
			if !strings.HasPrefix(rs[0].Reason, fmt.Sprintf("cmd/a.go:%d holds a", line)) || !strings.HasSuffix(rs[0].Reason, dvcfgSuffix) {
				t.Errorf("want a reason naming cmd/a.go:%d and ending %q, got %q", line, dvcfgSuffix, rs[0].Reason)
			}
		})
	}
}

// ---- the reason never holds the literal ------------------------------------------------------

func TestDeploymentValuesConfiguredReasonNeverHoldsTheLiteral(t *testing.T) {
	long := strings.Repeat("zq", 40) // 80 characters, longer than the 64 a quoting reason would cut
	cases := []struct {
		name string
		body string
		no   []string // text no reason may hold
	}{
		{"URL with a password", "\tpassword := \"postgres://u:secret@db:5432/x\"", []string{"secret", "postgres", "db:5432", "@db"}},
		{"URL as an address", "\t_ = \"http://hunter2.example/path\"", []string{"hunter2", "http://", "/path"}},
		{"short credential", "\treq.Header.Set(\"Authorization\", \"Bearer s3cr3t\")", []string{"s3cr3t", "Bearer"}},
		{"IP address", "\t_ = \"203.0.113.77\"", []string{"203.0.113.77"}},
		{"host and port", "\t_ = \"internal-db.corp:5432\"", []string{"internal-db", "corp", "5432"}},
		{"long address", "\t_ = \"http://" + long + ".example/\"", []string{long, long[:20], "zqzq"}},
		{"long credential", "\tpassword := \"" + long + "\"", []string{long, long[:20], "zqzq"}},
		{"raw string credential", "\tpassword := `tr1cky`", []string{"tr1cky"}},
		{"joined credential", "\tpassword := \"half-one\" + \"half-two\"", []string{"half-one", "half-two"}},
		{"environment variable name", "\t_ = os.Getenv(\"MY_SECRET_NAME\")", []string{"MY_SECRET_NAME"}},
		{"credential name", "\tmyPasswordVar := \"x\"", []string{"myPasswordVar", "\"x\""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := dvcfgCase{body: c.body}.source()
			line := 0
			for i, l := range strings.Split(src, "\n") {
				if l == c.body {
					line = i + 1
				}
			}
			dir := dvcfgProject(t, map[string]string{"cmd/a.go": src})
			rs := checks.DeploymentValuesConfigured(dir, []string{"cmd/**"})
			fails := 0
			for _, r := range rs {
				if !r.Passed {
					fails++
					if want := fmt.Sprintf("cmd/a.go:%d holds a", line); !strings.HasPrefix(r.Reason, want) || !strings.HasSuffix(r.Reason, dvcfgSuffix) {
						t.Errorf("reason must start %q and end %q: %q", want, dvcfgSuffix, r.Reason)
					}
				}
				for _, no := range append(slices.Clone(c.no), dir) {
					if strings.Contains(r.Reason, no) {
						t.Errorf("reason must not hold %q: %q", no, r.Reason)
					}
				}
			}
			if fails != 1 {
				t.Errorf("want exactly one failure, got %+v", rs)
			}
		})
	}
}

// ---- passing and scope -----------------------------------------------------------------------

const dvcfgReferenceMain = `// Command notes serves the notes API.
package main

import (
	"log"
	"net/http"

	"example.com/service/gen"
	"example.com/service/internal/app"
)

func main() {
	log.Fatal(http.ListenAndServe(gen.ListenAddress(), gen.Handler(app.New())))
}
`

// dvcfgOffending holds a credential on line 6 and an address and an environment read on line 7.
const dvcfgOffending = `package gen

import "os"

func Listen() (string, string, string) {
	password := "p"
	return ":8080", os.Getenv("ADDR"), password
}
`

func TestDeploymentValuesConfiguredPassesOnTheReferenceMain(t *testing.T) {
	dir := dvcfgProject(t, map[string]string{"cmd/notes/main.go": dvcfgReferenceMain})
	dvcfgJudge(t, checks.DeploymentValuesConfigured(dir, []string{"cmd/**", "internal/**"}), nil, 1)
}

func TestDeploymentValuesConfiguredPassSaysHowManyFilesItExamined(t *testing.T) {
	dir := dvcfgProject(t, map[string]string{
		"cmd/notes/main.go":      dvcfgReferenceMain,
		"internal/app/app.go":    "package app\n\nfunc New() int { return 1 }\n",
		"internal/app/store.go":  "package app\n\nvar notes []string\n",
		"internal/app/readme.md": "http://x.example/a\n",
	})
	dvcfgJudge(t, checks.DeploymentValuesConfigured(dir, []string{"cmd/**", "internal/**"}), nil, 3)
}

func TestDeploymentValuesConfiguredJudgesOnlyTheStepsOwnGoFiles(t *testing.T) {
	dir := dvcfgProject(t, map[string]string{
		"cmd/notes/main.go":        dvcfgReferenceMain,
		"cmd/notes/main_test.go":   dvcfgOffending, // a test file the step matches
		"internal/app/app_test.go": dvcfgOffending,
		"gen/config.go":            dvcfgOffending, // another step's
		"tests/helper.go":          dvcfgOffending, // another step's
		"tools/gen.go":             dvcfgOffending, // not matched by writable
		"cmd/notes/data.txt":       "http://x.example/a\n",
		"cmd/notes/data.yaml":      "listen: \":8080\"\n",
	})
	dvcfgJudge(t, checks.DeploymentValuesConfigured(dir, []string{"cmd/**", "internal/**"}), nil, 1)
}

func TestDeploymentValuesConfiguredJudgesAFileThatWritableMatches(t *testing.T) {
	dir := dvcfgProject(t, map[string]string{
		"cmd/notes/main.go": dvcfgReferenceMain,
		"internal/app/a.go": dvcfgOffending,
		"gen/config.go":     dvcfgOffending,
	})
	rs := checks.DeploymentValuesConfigured(dir, []string{"cmd/**", "internal/**"})
	wants := []string{
		"internal/app/a.go:6 holds a credential:",
		"internal/app/a.go:7 holds an address:",
		"internal/app/a.go:7 holds an environment read:",
	}
	dvcfgJudge(t, rs, wants, 2)
}

func TestDeploymentValuesConfiguredNamesEachFileOfSeveral(t *testing.T) {
	dir := dvcfgProject(t, map[string]string{
		"cmd/a.go": "package main\n\nvar addr = \":8080\"\n",
		"cmd/b.go": "package main\n\nvar other = \"clean\"\n",
		"cmd/c.go": "package main\n\nvar (\n\tx = 1\n\tpassword = \"p\"\n)\n",
	})
	rs := checks.DeploymentValuesConfigured(dir, []string{"cmd/**"})
	dvcfgJudge(t, rs, []string{"cmd/a.go:3 holds an address:", "cmd/c.go:5 holds a credential:"}, 3)
}

func TestDeploymentValuesConfiguredPassesWhenNoFileIsJudged(t *testing.T) {
	t.Run("an empty project", func(t *testing.T) {
		dvcfgJudge(t, checks.DeploymentValuesConfigured(t.TempDir(), []string{"cmd/**"}), nil, 0)
	})
	t.Run("a project with no writable entry", func(t *testing.T) {
		dir := dvcfgProject(t, map[string]string{"cmd/a.go": dvcfgOffending})
		dvcfgJudge(t, checks.DeploymentValuesConfigured(dir, nil), nil, 0)
	})
	t.Run("a project no writable entry matches", func(t *testing.T) {
		dir := dvcfgProject(t, map[string]string{"cmd/a.go": dvcfgOffending})
		dvcfgJudge(t, checks.DeploymentValuesConfigured(dir, []string{"internal/**"}), nil, 0)
	})
}

// ---- fail closed -----------------------------------------------------------------------------

func TestDeploymentValuesConfiguredFailsClosedOnAFileThatDoesNotParse(t *testing.T) {
	cases := map[string]map[string]string{
		"alone":             {"cmd/a.go": "package main\n\nfunc {\n"},
		"beside a good one": {"cmd/a.go": "package main\n\nfunc {\n", "cmd/b.go": "package main\n"},
		"beside an offence": {"cmd/a.go": "package main\n\nfunc {\n", "cmd/b.go": "package main\n\nvar a = \":8080\"\n"},
		"no package clause": {"cmd/a.go": "var a = 1\n"},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			dir := dvcfgProject(t, files)
			rs := checks.DeploymentValuesConfigured(dir, []string{"cmd/**"})
			named := false
			for _, r := range rs {
				if r.Passed {
					t.Errorf("a file that does not parse must not pass: %+v", r)
				}
				if r.Check != "DeploymentValuesConfigured" || r.Part != "project" {
					t.Errorf("want the project's DeploymentValuesConfigured, got %+v", r)
				}
				named = named || strings.Contains(r.Reason, "cmd/a.go")
			}
			if len(rs) == 0 || !named {
				t.Errorf("want a failure naming cmd/a.go, got %+v", rs)
			}
		})
	}
}

func TestDeploymentValuesConfiguredIgnoresAnUnparseableFileItDoesNotJudge(t *testing.T) {
	dir := dvcfgProject(t, map[string]string{
		"cmd/a.go":      "package main\n",
		"cmd/a_test.go": "package main\n\nfunc {\n",
		"gen/x.go":      "package gen\n\nfunc {\n",
	})
	dvcfgJudge(t, checks.DeploymentValuesConfigured(dir, []string{"cmd/**"}), nil, 1)
}

// ---- stated limits of the forms --------------------------------------------------------------

func TestDeploymentValuesConfiguredStatedLimitsOfTheForms(t *testing.T) {
	dvcfgRunAll(t, []dvcfgCase{
		{name: "a port written as a number", body: "\t_ = http.ListenAndServe(fmt.Sprintf(\":%d\", 8080), nil)"},
		{name: "an address built from literals joined by +", body: "\t_ = \"http://\" + \"x.example/a\""},
		{name: "an address built at run time", body: "\t_ = \"http://\" + user"},
		{name: "a credential under another name", body: "\tpw := \"p\""},
		{name: "a credential in another call", body: "\treq.Header.Set(\"X-Auth\", \"x\")"},
		{name: "a credential value built at run time", body: "\treq.Header.Set(\"Authorization\", \"Bearer \"+tok)"},
	})
}
