package procedures_test

// T049b: Generate's output also holds the seam internal/app/app.go, `func New() gen.ServerInterface`
// returning gen.Stub{}. The tests step's tests always build the server with app.New(), and the code
// step replaces the file. The module, seam included, builds offline from vendor/ alone, and a test
// that calls app.New() builds and fails against it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const seamPath = "internal/app/app.go"

func TestGenerateOutputHoldsTheSeam(t *testing.T) {
	var seam string
	found := false
	for _, f := range generated(t).Outputs[0].Files {
		if f.Path == seamPath {
			seam, found = string(f.Content), true
		}
	}
	if !found {
		t.Fatalf("the generated-code output lacks %s", seamPath)
	}
	for _, want := range []string{"package app", "func New() gen.ServerInterface", "gen.Stub{}"} {
		if !strings.Contains(seam, want) {
			t.Errorf("%s lacks %q:\n%s", seamPath, want, seam)
		}
	}
}

func TestSeamBuildsOfflineAndATestAgainstItFails(t *testing.T) {
	res := generated(t)
	dir := t.TempDir()
	var module string
	put := func(rel string, b []byte) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range res.Outputs[0].Files {
		put(f.Path, f.Content)
		if f.Path == "go.mod" {
			module = regexp.MustCompile(`(?m)^module\s+(\S+)`).FindStringSubmatch(string(f.Content))[1]
		}
	}
	put("tests/seam_test.go", []byte(`package tests

import (
	"testing"

	"`+module+`/gen"
	"`+module+`/internal/app"
)

func TestServerIsNotTheStub(t *testing.T) {
	if _, ok := app.New().(gen.Stub); ok {
		t.Fatal("the server is still the stub, which answers 501")
	}
}
`))
	run := func(args ...string) (string, error) {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		// -mod=vendor: nothing but the files the procedure returned, and no network
		cmd.Env = append(os.Environ(), "GOPROXY=off", "GOWORK=off", "GOFLAGS=-mod=vendor", "CGO_ENABLED=0", "GOMODCACHE="+t.TempDir())
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run("build", "./..."); err != nil {
		t.Fatalf("the generated project with the seam does not build offline: %v\n%s", err, out)
	}
	out, err := run("test", "./tests/...")
	if err == nil {
		t.Fatalf("a test against the stub seam passed:\n%s", out)
	}
	if strings.Contains(out, "[build failed]") || strings.Contains(out, "[setup failed]") || !strings.Contains(out, "--- FAIL") {
		t.Fatalf("the test must build and fail against the stub, not fail to build:\n%s", out)
	}
}
