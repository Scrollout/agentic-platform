package procedures_test

// T009 (feature 009): failing tests, by a separate author, for the template's gen/config.go
// (catalogue README, "Configuration"; declared-values contract, "gen/config.go"). The service's
// configuration is read once, through gen.ListenAddress(), from LISTEN_ADDRESS, and is ":8080"
// when the variable is unset or empty; the file's doc comment is the project's documentation of
// its configuration and names LISTEN_ADDRESS and ":8080".
//
// These tests generate with the real template (src/catalogue/api-feature-go/template), not
// testdata/template, because the file is the template's. The contract is silent on a value of
// only spaces and on a malformed address (gen.ListenAddress does not judge it), so neither is
// pinned. Helpers here are prefixed cfg so they clash with no other test file.

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/checks"
	"github.com/agentic-platform/engine/procedures"
)

const cfgPath = "gen/config.go"

func cfgTemplateDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "catalogue", "api-feature-go", "template"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// cfgGenerate runs the generate procedure with the real template and the shared test contract.
func cfgGenerate(t *testing.T) (procedures.Result, []byte) {
	t.Helper()
	contract := readTestdata(t, "contract.yaml")
	cfg := procedures.GenerateConfig{OapiCodegen: oapiCodegen(t), Template: cfgTemplateDir(t), Env: goEnv()}
	res, err := procedures.Generate(context.Background(), cfg, genInputs(contract))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Completed {
		t.Fatalf("not completed: %s", res.Reason)
	}
	if len(res.Outputs) != 1 {
		t.Fatalf("want one output, got %d", len(res.Outputs))
	}
	return res, contract
}

func cfgFile(t *testing.T, res procedures.Result, p string) []byte {
	t.Helper()
	for _, f := range res.Outputs[0].Files {
		if f.Path == p {
			return f.Content
		}
	}
	t.Fatalf("the generated project has no %s", p)
	return nil
}

// The generated project holds the template's gen/config.go byte for byte, whose package doc
// comment names LISTEN_ADDRESS and ":8080", and which declares func ListenAddress() string.
func TestGenerateHoldsTheTemplatesConfig(t *testing.T) {
	res, _ := cfgGenerate(t)
	got := cfgFile(t, res, cfgPath)
	want, err := os.ReadFile(filepath.Join(cfgTemplateDir(t), "gen", "config.go"))
	if err != nil {
		t.Fatalf("the template must declare gen/config.go: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("gen/config.go of the project differs from the template's:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	af, err := parser.ParseFile(token.NewFileSet(), cfgPath, got, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if af.Name.Name != "gen" {
		t.Errorf("package %q, want gen", af.Name.Name)
	}
	if af.Doc == nil {
		t.Fatal("gen/config.go has no package doc comment: it is the project's documentation of its configuration")
	}
	doc := af.Doc.Text()
	for _, want := range []string{"LISTEN_ADDRESS", `":8080"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("the doc comment must name %s: %q", want, doc)
		}
	}
	var fn *ast.FuncDecl
	for _, d := range af.Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Recv == nil && f.Name.Name == "ListenAddress" {
			fn = f
		}
	}
	if fn == nil {
		t.Fatal("gen/config.go declares no func ListenAddress")
	}
	if fn.Type.Params.NumFields() != 0 || fn.Type.Results.NumFields() != 1 {
		t.Fatalf("ListenAddress must take nothing and return one value (a string)")
	}
	if id, ok := fn.Type.Results.List[0].Type.(*ast.Ident); !ok || id.Name != "string" {
		t.Errorf("ListenAddress must return a string")
	}
}

// A program built from the generated project prints gen.ListenAddress() as ":8080" with
// LISTEN_ADDRESS unset or empty, and as its value when set. The child's environment is given
// explicitly: nothing is inherited from the host.
func TestListenAddressFromTheEnvironment(t *testing.T) {
	res, contract := cfgGenerate(t)
	_ = cfgFile(t, res, cfgPath) // fails first, with the missing file, when the template has none
	dir := t.TempDir()
	writeOutput(t, dir, res, contract)
	mod := ""
	for _, l := range strings.Split(string(cfgFile(t, res, "go.mod")), "\n") {
		if f := strings.Fields(l); len(f) == 2 && f[0] == "module" {
			mod = f[1]
		}
	}
	if mod == "" {
		t.Fatal("go.mod names no module")
	}
	main := "package main\n\nimport (\n\t\"fmt\"\n\n\t\"" + mod + "/gen\"\n)\n\nfunc main() { fmt.Print(gen.ListenAddress()) }\n"
	p := filepath.Join(dir, "cmd", "cfgprint", "main.go")
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "cfgprint")
	build := exec.Command("go", "build", "-o", bin, "./cmd/cfgprint") //nolint:gosec // test builds a temp program
	build.Dir = dir
	build.Env = append(os.Environ(), "GOPROXY=off", "GOWORK=off", "GOFLAGS=-mod=vendor", "CGO_ENABLED=0", "GOMODCACHE="+t.TempDir())
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build of a program that prints gen.ListenAddress(): %v\n%s", err, out)
	}

	cases := []struct {
		name string
		env  []string
		want string
	}{
		{"unset", nil, ":8080"},
		{"empty", []string{"LISTEN_ADDRESS="}, ":8080"},
		{"a port", []string{"LISTEN_ADDRESS=:9090"}, ":9090"},
		{"a host and port", []string{"LISTEN_ADDRESS=127.0.0.1:9191"}, "127.0.0.1:9191"},
		{"another variable only", []string{"LISTEN_ADDRESS_X=:1", "OTHER=:2"}, ":8080"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command(bin) //nolint:gosec // test runs the temp program it built
			cmd.Env = c.env          // exactly these, never the host's
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("running the program: %v", err)
			}
			if string(out) != c.want {
				t.Fatalf("gen.ListenAddress() = %q with %v, want %q", out, c.env, c.want)
			}
		})
	}
}

// Generating twice gives the same bytes including gen/config.go, and GenerationRepeatable (go
// generate again, compare gen/) still passes on the project that holds it.
func TestGenerateWithConfigIsRepeatable(t *testing.T) {
	a, contract := cfgGenerate(t)
	b, _ := cfgGenerate(t)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("generating twice gave different bytes or hashes")
	}
	if !bytes.Equal(cfgFile(t, a, cfgPath), cfgFile(t, b, cfgPath)) {
		t.Fatal("gen/config.go differs between two generations")
	}
	dir := t.TempDir()
	writeOutput(t, dir, a, contract)
	t.Setenv("PATH", filepath.Dir(oapiCodegen(t))+string(os.PathListSeparator)+os.Getenv("PATH"))
	if rs := checks.GenerationDrift(checks.NewGoRunner(), dir); !allPass(rs) {
		t.Fatalf("GenerationRepeatable on the output: %+v", rs)
	}
}
