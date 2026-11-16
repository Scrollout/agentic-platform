package procedures_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/procedures"
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func readTestdata(t *testing.T, parts ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"testdata"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// framed is the contract's framed form, written out independently of procedures.Framed:
// for each file in byte order of path, the path, LF, the length in decimal, LF, the bytes.
func framed(files []procedures.File) []byte {
	fs := append([]procedures.File(nil), files...)
	sort.Slice(fs, func(i, j int) bool { return fs[i].Path < fs[j].Path })
	var b bytes.Buffer
	for _, f := range fs {
		fmt.Fprintf(&b, "%s\n%d\n", f.Path, len(f.Content))
		b.Write(f.Content)
	}
	return b.Bytes()
}

// contractInput is a `slice/contract` input: the part's canonical JSON and its hash.
func contractInput(t *testing.T, text string) procedures.Input {
	t.Helper()
	h, err := canonical.ContractHash(text)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(map[string]any{"contract": text}) // ASCII text: equals the RFC 8785 form
	if err != nil {
		t.Fatal(err)
	}
	return procedures.Input{Name: "slice/contract", SHA256: h, Content: b}
}

// rulesInput is a `slice/rules` input; the procedure must read only `examples` from it.
func rulesInput(t *testing.T, examples string) procedures.Input {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"rules":    []any{map[string]any{"id": "R-1", "text": "A greeting names the person"}},
		"examples": examples,
		"terms":    []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return procedures.Input{Name: "slice/rules", SHA256: sum(b), Content: b}
}

func checkInputs(t *testing.T) ([]procedures.Input, string, string) {
	contract, examples := string(readTestdata(t, "contract.yaml")), string(readTestdata(t, "examples.feature"))
	return []procedures.Input{contractInput(t, contract), rulesInput(t, examples)}, contract, examples
}

func TestFramed(t *testing.T) {
	if got := procedures.Framed(nil); len(got) != 0 {
		t.Fatalf("an empty set must frame to no bytes, got %q", got)
	}
	if sum(procedures.Framed([]procedures.File{})) != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatal("an empty set must hash as the SHA-256 of no bytes")
	}
	files := []procedures.File{
		{Path: "b.txt", Content: []byte("two\nlines")},
		{Path: "a/z", Content: []byte{}},
		{Path: "a.go", Content: []byte("x")},
	}
	want := "a.go\n1\nxa/z\n0\nb.txt\n9\ntwo\nlines"
	if got := string(procedures.Framed(files)); got != want {
		t.Fatalf("framed form:\n got %q\nwant %q", got, want)
	}
	// Byte order of path, not locale or directory order: "a.go" < "a/z" because '.' (0x2e) < '/' (0x2f).
	if got := string(procedures.Framed([]procedures.File{files[1], files[0], files[2]})); got != want {
		t.Fatalf("Framed must sort by byte order of path whatever the input order: %q", got)
	}
}

func TestContractCheckOutputs(t *testing.T) {
	in, contract, examples := checkInputs(t)
	res, err := procedures.ContractCheck(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Completed {
		t.Fatalf("not completed: %s", res.Reason)
	}
	want := []procedures.Output{
		{Name: "contract", Files: []procedures.File{{Path: "api/openapi.yaml", Content: []byte(contract)}}},
		{Name: "examples", Files: []procedures.File{{Path: "features/slice.feature", Content: []byte(examples)}}},
	}
	if len(res.Outputs) != 2 {
		t.Fatalf("want outputs contract then examples, got %+v", res.Outputs)
	}
	for i, w := range want {
		g := res.Outputs[i]
		if g.Name != w.Name || !reflect.DeepEqual(g.Files, w.Files) {
			t.Errorf("output %d: got %q %+v, want %q %+v", i, g.Name, g.Files, w.Name, w.Files)
		}
		if g.SHA256 != sum(framed(w.Files)) || g.SHA256 != sum(procedures.Framed(g.Files)) {
			t.Errorf("output %s: hash %s is not the SHA-256 of its framed form", g.Name, g.SHA256)
		}
	}
}

func TestContractCheckInputHashMismatch(t *testing.T) {
	for _, idx := range []int{0, 1} {
		in, _, _ := checkInputs(t)
		in[idx].Content = append(append([]byte(nil), in[idx].Content...), ' ')
		res, err := procedures.ContractCheck(context.Background(), in)
		if !errors.Is(err, procedures.ErrInputHashMismatch) {
			t.Errorf("tampered %s: err = %v, want ErrInputHashMismatch", in[idx].Name, err)
		}
		if len(res.Outputs) != 0 {
			t.Errorf("tampered %s: outputs returned", in[idx].Name)
		}
	}
}

func TestContractCheckWrongInputs(t *testing.T) {
	in, _, _ := checkInputs(t)
	extra := procedures.Input{Name: "contract", SHA256: sum(nil), Content: nil}
	cases := map[string][]procedures.Input{
		"none":             nil,
		"no rules":         in[:1],
		"no contract":      in[1:],
		"extra input":      append(append([]procedures.Input(nil), in...), extra),
		"renamed contract": {{Name: "slice/goals", SHA256: in[0].SHA256, Content: in[0].Content}, in[1]},
	}
	for name, c := range cases {
		res, err := procedures.ContractCheck(context.Background(), c)
		if err != nil {
			t.Errorf("%s: a wrong input set is a refusal (Completed false), not an error: %v", name, err)
			continue
		}
		if res.Completed || strings.TrimSpace(res.Reason) == "" || len(res.Outputs) != 0 {
			t.Errorf("%s: want Completed false with a reason and no outputs, got %+v", name, res)
		}
	}
}

func TestContractCheckRepeatable(t *testing.T) {
	in, _, _ := checkInputs(t)
	a, errA := procedures.ContractCheck(context.Background(), in)
	b, errB := procedures.ContractCheck(context.Background(), in)
	if errA != nil || errB != nil {
		t.Fatal(errA, errB)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same inputs gave different results")
	}
}

// ---- generate: the real toolchain, offline ----

// oapi-codegen is found, in this order: AP_OAPI_CODEGEN (a path to the binary); otherwise it is
// built once per test run by `go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@<pin>`
// with GOPROXY=off, the pin read from src/deploy/images.lock. That needs the pinned module and its
// dependencies in the module cache (scripts/check.sh must prime it); if not, the tests fail with
// the toolchain's own message and never skip. The module cache is the toolchain's default
// (GOMODCACHE, else GOPATH/pkg/mod); the tests pass the process's GOMODCACHE through if it is set.
var (
	oapiOnce sync.Once
	oapiPath string
	oapiErr  error
)

func oapiCodegen(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("AP_OAPI_CODEGEN"); p != "" {
		return p
	}
	oapiOnce.Do(func() {
		lock, err := os.ReadFile(filepath.Join("..", "..", "deploy", "images.lock"))
		if err != nil {
			oapiErr = err
			return
		}
		var pin string
		for _, l := range strings.Split(string(lock), "\n") {
			if f := strings.Fields(l); len(f) == 2 && f[0] == "oapi-codegen" {
				pin = f[1]
			}
		}
		if pin == "" {
			oapiErr = errors.New("no oapi-codegen pin in src/deploy/images.lock")
			return
		}
		dir, err := os.MkdirTemp("", "oapi-codegen-bin")
		if err != nil {
			oapiErr = err
			return
		}
		cmd := exec.Command("go", "install", pin)
		cmd.Env = append(os.Environ(), "GOBIN="+dir, "GOPROXY=off", "GOWORK=off", "GOFLAGS=-mod=mod")
		if out, err := cmd.CombinedOutput(); err != nil {
			oapiErr = fmt.Errorf("go install %s (offline): %v\n%s", pin, err, out)
			return
		}
		oapiPath = filepath.Join(dir, "oapi-codegen")
	})
	if oapiErr != nil {
		t.Fatalf("oapi-codegen is not available offline: %v", oapiErr)
	}
	return oapiPath
}

func goEnv() []string {
	env := []string{"GOPROXY=off", "GOWORK=off", "GOFLAGS=-mod=mod", "CGO_ENABLED=0"}
	if m := os.Getenv("GOMODCACHE"); m != "" {
		env = append(env, "GOMODCACHE="+m)
	}
	return env
}

func genConfig(t *testing.T, template string) procedures.GenerateConfig {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", template))
	if err != nil {
		t.Fatal(err)
	}
	return procedures.GenerateConfig{OapiCodegen: oapiCodegen(t), Template: abs, Env: goEnv()}
}

func genInputs(contract []byte) []procedures.Input {
	fs := []procedures.File{{Path: "api/openapi.yaml", Content: contract}}
	b := procedures.Framed(fs)
	return []procedures.Input{{Name: "contract", SHA256: sum(b), Content: b}}
}

func generated(t *testing.T) procedures.Result {
	t.Helper()
	res, err := procedures.Generate(context.Background(), genConfig(t, "template"), genInputs(readTestdata(t, "contract.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Completed {
		t.Fatalf("not completed: %s", res.Reason)
	}
	return res
}

func writable(p string) bool {
	return strings.HasPrefix(p, "gen/") || strings.HasPrefix(p, "vendor/") || p == "go.mod" || p == "go.sum" || p == "internal/app/app.go"
}

func TestGenerateOutputAndWritableSet(t *testing.T) {
	res := generated(t)
	if len(res.Outputs) != 1 || res.Outputs[0].Name != "generated-code" {
		t.Fatalf("want one output generated-code, got %+v", res.Outputs)
	}
	o := res.Outputs[0]
	if o.SHA256 != sum(framed(o.Files)) || o.SHA256 != sum(procedures.Framed(o.Files)) {
		t.Fatal("output hash is not the SHA-256 of its framed form")
	}
	seen := map[string]bool{}
	prev := ""
	var haveGen, haveMod, haveSum, haveVendor bool
	for _, f := range o.Files {
		if !writable(f.Path) {
			t.Errorf("file outside the writable set: %s", f.Path)
		}
		if f.Path <= prev || seen[f.Path] {
			t.Errorf("files not in strict byte order of path at %s (after %s)", f.Path, prev)
		}
		prev, seen[f.Path] = f.Path, true
		haveGen = haveGen || strings.HasPrefix(f.Path, "gen/") && strings.HasSuffix(f.Path, ".go")
		haveVendor = haveVendor || f.Path == "vendor/modules.txt"
		haveMod, haveSum = haveMod || f.Path == "go.mod", haveSum || f.Path == "go.sum"
	}
	if !haveGen || !haveMod || !haveSum || !haveVendor {
		t.Fatalf("missing gen/*.go (%v), go.mod (%v), go.sum (%v) or vendor/modules.txt (%v)", haveGen, haveMod, haveSum, haveVendor)
	}
	// The template's go.mod and go.sum are the base of the result: the pin on the runtime stays.
	for _, f := range o.Files {
		if f.Path == "go.mod" && !bytes.Contains(f.Content, []byte("github.com/oapi-codegen/runtime v1.8.0")) {
			t.Error("go.mod lost the template's pin of oapi-codegen/runtime")
		}
	}
}

func TestGenerateBuildsOffline(t *testing.T) {
	res := generated(t)
	dir := t.TempDir()
	for _, f := range res.Outputs[0].Files {
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, f.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	// -mod=vendor: the build must need nothing but the files the procedure returned.
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOWORK=off", "GOFLAGS=-mod=vendor", "CGO_ENABLED=0", "GOMODCACHE="+t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... in the project made of the returned files: %v\n%s", err, out)
	}
}

func TestGenerateRepeatable(t *testing.T) {
	a, b := generated(t), generated(t)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("generating twice gave different bytes or hashes")
	}
}

func TestGenerateRejectedContract(t *testing.T) {
	res, err := procedures.Generate(context.Background(), genConfig(t, "template"), genInputs(readTestdata(t, "broken-contract.yaml")))
	if err != nil {
		t.Fatalf("a contract oapi-codegen rejects is Completed false, not an error: %v", err)
	}
	if res.Completed || len(res.Outputs) != 0 || !strings.Contains(res.Reason, "NoSuchSchema") {
		t.Fatalf("want Completed false naming NoSuchSchema, no outputs; got %+v", res)
	}
}

func TestGenerateTemplateSumDisagrees(t *testing.T) {
	res, err := procedures.Generate(context.Background(), genConfig(t, "template-badsum"), genInputs(readTestdata(t, "contract.yaml")))
	if err != nil {
		t.Fatalf("a go.sum that disagrees is Completed false, not an error: %v", err)
	}
	if res.Completed || len(res.Outputs) != 0 || strings.TrimSpace(res.Reason) == "" {
		t.Fatalf("want Completed false with a reason, no outputs; got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.Reason), "checksum") && !strings.Contains(res.Reason, "go.sum") {
		t.Errorf("reason should name the checksum or go.sum problem: %q", res.Reason)
	}
}

func TestGenerateInputProblems(t *testing.T) {
	cfg := genConfig(t, "template")
	in := genInputs(readTestdata(t, "contract.yaml"))
	bad := []procedures.Input{in[0]}
	bad[0].Content = append(append([]byte(nil), bad[0].Content...), '\n')
	if _, err := procedures.Generate(context.Background(), cfg, bad); !errors.Is(err, procedures.ErrInputHashMismatch) {
		t.Errorf("tampered contract: err = %v, want ErrInputHashMismatch", err)
	}
	for name, c := range map[string][]procedures.Input{"none": nil, "extra": {in[0], in[0]}} {
		res, err := procedures.Generate(context.Background(), cfg, c)
		if err != nil || res.Completed || strings.TrimSpace(res.Reason) == "" || len(res.Outputs) != 0 {
			t.Errorf("%s: want Completed false with a reason, no outputs; got %+v, %v", name, res, err)
		}
	}
}

// A missing tool or module cache is a fault of the environment: it must surface with the
// toolchain's message (in the error or the reason), and never as a completed run or a skip.
func TestGenerateMissingToolchainFails(t *testing.T) {
	in := genInputs(readTestdata(t, "contract.yaml"))
	cfg := genConfig(t, "template")
	cfg.OapiCodegen = filepath.Join(t.TempDir(), "no-such-oapi-codegen")
	res, err := procedures.Generate(context.Background(), cfg, in)
	if err == nil && res.Completed {
		t.Fatal("a missing oapi-codegen binary completed")
	}
	if msg := fmt.Sprint(err, res.Reason); !strings.Contains(msg, "no-such-oapi-codegen") {
		t.Errorf("message should name the missing binary: %q", msg)
	}

	cfg = genConfig(t, "template")
	cfg.Env = append(goEnv(), "GOMODCACHE="+t.TempDir()) // later entries win: an empty cache
	res, err = procedures.Generate(context.Background(), cfg, in)
	if err == nil && res.Completed {
		t.Fatal("an empty module cache completed")
	}
	if msg := fmt.Sprint(err, res.Reason); !strings.Contains(msg, "lookup disabled") {
		t.Errorf("message should carry the toolchain's `module lookup disabled by GOPROXY=off`: %q", msg)
	}
}
