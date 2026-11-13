package checksvc_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/agentic-platform/engine/checksvc"
)

// ccBody is a runProcedure body for contract-check with the approved contract and examples.
func ccBody(t testing.TB, dir, contract, examples string) map[string]any {
	parts := approvedParts(contract, examples)
	in := func(p any, name string) any {
		m := p.(map[string]any)
		return map[string]any{"name": name, "sha256": m["sha256"], "content": m["content"]}
	}
	st := step("contract-check", []string{"api/openapi.yaml", "features/slice.feature"}, []string{"contract", "examples"}, nil, nil)
	b := common(filepath.Base(dir), st, nil)
	b["inputs"] = []any{in(parts[2], "slice/contract"), in(parts[1], "slice/rules")}
	return b
}

func generateBody(dir string, contract []byte, limit int) map[string]any {
	st := step("generate", genWritable, []string{"generated-code"}, nil, nil)
	b := common(filepath.Base(dir), st, nil)
	f := framed(map[string][]byte{"api/openapi.yaml": contract})
	b["inputs"] = []any{map[string]any{"name": "contract", "sha256": sum(f), "content": b64(f)}}
	b["time_limit_seconds"] = limit
	return b
}

type procResult struct {
	Procedure string `json:"procedure"`
	Completed bool   `json:"completed"`
	Reason    string `json:"reason"`
	Outputs   []struct {
		Name   string        `json:"name"`
		SHA256 string        `json:"sha256"`
		Files  []fileContent `json:"files"`
	} `json:"outputs"`
	ProjectFiles []pf `json:"project_files"`
}

func wantProcedure(t testing.TB, r resp, name string) procResult {
	t.Helper()
	if r.status != 200 {
		t.Fatalf("%s: want 200, got %d: %.500s", name, r.status, r.body)
	}
	var p procResult
	if err := strictJSON(r.body, &p); err != nil || p.Procedure != name || p.Reason == "" || p.Outputs == nil || p.ProjectFiles == nil {
		t.Fatalf("not a ProcedureResult for %s with reason, outputs and project_files set: %v %.400s", name, err, r.body)
	}
	return p
}

// ---- a generated project, made once through the service itself ----

var (
	genOnce sync.Once
	genPath string
	genErr  string
)

// generatedProject is a project folder holding what the generate worker wrote for the fixture
// contract, plus api/openapi.yaml: a clean fixture for the checks of the generate step.
func generatedProject(t testing.TB) string {
	t.Helper()
	oapi := needOapi(t)
	genOnce.Do(func() {
		root, err := sharedTemp("generated-project")
		if err != nil {
			genErr = err.Error()
			return
		}
		dir := filepath.Join(root, "p1")
		os.MkdirAll(dir, 0o755)
		cfg := workerConfig(t, root, "generate", "generate")
		cfg.OapiCodegen = oapi
		ts := httptest.NewServer(checksvc.NewHandler(cfg))
		defer ts.Close()
		contract := readTestdata(t, "contract.yaml")
		r := post(t, ts.URL, "/procedures/generate/run", generateBody(dir, contract, 300))
		var p procResult
		if r.status != 200 || json.Unmarshal(r.body, &p) != nil || !p.Completed {
			genErr = "generate did not complete: " + string(r.body)
			if len(genErr) > 600 {
				genErr = genErr[:600]
			}
			return
		}
		write(t, dir, "api/openapi.yaml", contract)
		genPath = dir
	})
	if genErr != "" {
		t.Fatal(genErr)
	}
	return genPath
}

// ---- 7. runProcedure ----

func TestContractCheckWritesExactlyItsFiles(t *testing.T) {
	root, dir := fixture(t)
	write(t, dir, "api/openapi.yaml", []byte("stale contract\n"))
	write(t, dir, "features/slice.feature", []byte("stale examples\n"))
	write(t, dir, "internal/keep.go", []byte("package internal\n"))
	write(t, dir, "README.md", []byte("kept\n"))
	write(t, dir, "api/other.yaml", []byte("kept\n"))
	base := serve(t, workerConfig(t, root, "contract-check", "contract-check"))
	contract, examples := string(readTestdata(t, "contract.yaml")), "Feature: Greetings\n\n  Rule: R-1 A greeting names the person\n"
	body := ccBody(t, dir, contract, examples)

	r := post(t, base, "/procedures/contract-check/run", body)
	p := wantProcedure(t, r, "contract-check")
	if !p.Completed {
		t.Fatalf("not completed: %s", p.Reason)
	}
	if len(p.Outputs) != 2 || p.Outputs[0].Name != "contract" || p.Outputs[1].Name != "examples" {
		t.Fatalf("one output per declared output, in the step's order: %+v", p.Outputs)
	}
	for i, want := range []struct{ path, text string }{{"api/openapi.yaml", contract}, {"features/slice.feature", examples}} {
		o := p.Outputs[i]
		if len(o.Files) != 1 || o.Files[0].Path != want.path {
			t.Fatalf("output %s must hold %s only: %+v", o.Name, want.path, o.Files)
		}
		raw, _ := base64.StdEncoding.DecodeString(o.Files[0].Content)
		if string(raw) != want.text || o.Files[0].SHA256 != sum(raw) {
			t.Errorf("%s is not the approved text byte for byte", want.path)
		}
		if o.SHA256 != sum(framed(map[string][]byte{want.path: raw})) {
			t.Errorf("output %s: sha256 is not the framed hash of its files", o.Name)
		}
		if string(read(t, dir, want.path)) != want.text {
			t.Errorf("%s was not written", want.path)
		}
	}
	if string(read(t, dir, "internal/keep.go")) != "package internal\n" || string(read(t, dir, "README.md")) != "kept\n" || string(read(t, dir, "api/other.yaml")) != "kept\n" {
		t.Error("a file outside the writable entries was touched")
	}
	if !reflect.DeepEqual(p.ProjectFiles, listFiles(t, dir)) {
		t.Errorf("project_files must be the folder after the writes: %v", p.ProjectFiles)
	}
	// A repeat of the same request leaves the same files and gives the same answer.
	snap := snapshot(t, dir)
	r2 := post(t, base, "/procedures/contract-check/run", body)
	if string(r2.body) != string(r.body) {
		t.Errorf("the repeat gave another response")
	}
	if d := sameSnapshot(snap, snapshot(t, dir)); d != "" {
		t.Errorf("the repeat changed the files: %s", d)
	}
}

func TestProcedureNotCompletedWritesNothing(t *testing.T) {
	cases := map[string]func(t *testing.T) (checksvc.Config, string, map[string]any){
		"contract-check, a contract that is not YAML": func(t *testing.T) (checksvc.Config, string, map[string]any) {
			root, dir := fixture(t)
			return workerConfig(t, root, "contract-check", "contract-check"), "contract-check", ccBody(t, dir, "a: [unclosed\n", "Feature: F\n")
		},
		"generate, a contract oapi-codegen rejects": func(t *testing.T) (checksvc.Config, string, map[string]any) {
			root, dir := fixture(t)
			return withOapi(t, workerConfig(t, root, "generate", "generate")), "generate", generateBody(dir, readTestdata(t, "broken-contract.yaml"), 300)
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, proc, body := mk(t)
			dir := filepath.Join(cfg.Project, "p1")
			write(t, dir, "api/openapi.yaml", []byte("kept\n"))
			write(t, dir, "features/slice.feature", []byte("kept\n"))
			write(t, dir, "gen/old.go", []byte("package gen\n"))
			write(t, dir, "go.mod", []byte("module kept\n"))
			write(t, dir, "vendor/stale/x.go", []byte("package stale\n"))
			snap := snapshot(t, dir)
			p := wantProcedure(t, post(t, serve(t, cfg), "/procedures/"+proc+"/run", body), proc)
			if p.Completed || len(p.Outputs) != 0 {
				t.Fatalf("a procedure that cannot produce its outputs answers completed false with no outputs: %+v", p)
			}
			noHost(t, p.Reason, cfg.Project, cfg.Template)
			if d := sameSnapshot(snap, snapshot(t, dir)); d != "" {
				t.Errorf("completed false writes nothing: %s", d)
			}
			if !reflect.DeepEqual(p.ProjectFiles, listFiles(t, dir)) {
				t.Errorf("project_files must be the folder as it stands: %v", p.ProjectFiles)
			}
		})
	}
}

func TestProcedureInputHashMismatch(t *testing.T) {
	for _, what := range []string{"content", "sha256"} {
		t.Run(what, func(t *testing.T) {
			root, dir := fixture(t)
			write(t, dir, "api/openapi.yaml", []byte("kept\n"))
			base := serve(t, workerConfig(t, root, "contract-check", "contract-check"))
			body := ccBody(t, dir, string(readTestdata(t, "contract.yaml")), "Feature: F\n")
			in := body["inputs"].([]any)[0].(map[string]any)
			if what == "content" {
				raw, _ := base64.StdEncoding.DecodeString(in["content"].(string))
				in["content"] = b64(append(raw[:len(raw)-1:len(raw)-1], ' ', raw[len(raw)-1]))
			} else {
				in["sha256"] = sum([]byte("not it"))
			}
			snap := snapshot(t, dir)
			p := wantProblem(t, post(t, base, "/procedures/contract-check/run", body), 422, "InputHashMismatch")
			if !strings.Contains(p.Detail, "slice/contract") {
				t.Errorf("detail must name the input: %q", p.Detail)
			}
			if d := sameSnapshot(snap, snapshot(t, dir)); d != "" {
				t.Errorf("nothing may be written: %s", d)
			}
		})
	}
}

func TestGenerateWritesExactlyTheWritableSetAndRemovesStaleFiles(t *testing.T) {
	root, dir := fixture(t)
	for p, c := range map[string]string{
		"gen/old.go": "package gen\n", "gen/sub/old2.go": "package sub\n", "vendor/stale/x.go": "package stale\n",
		"go.mod": "module stale\n", "go.sum": "stale\n",
		"internal/keep.go": "package internal\n", "tests/t_test.go": "package tests\n", "README.md": "kept\n",
		"api/openapi.yaml": "kept\n", "go.mod.bak": "kept\n", "genx/y.txt": "kept\n",
	} {
		write(t, dir, p, []byte(c))
	}
	notWritable := map[string][]byte{}
	for _, f := range listFiles(t, dir) {
		if !matches(genWritable, f.Path) {
			notWritable[f.Path] = read(t, dir, f.Path)
		}
	}
	base := serve(t, withOapi(t, workerConfig(t, root, "generate", "generate")))
	body := generateBody(dir, readTestdata(t, "contract.yaml"), 300)
	r := post(t, base, "/procedures/generate/run", body)
	p := wantProcedure(t, r, "generate")
	if !p.Completed {
		t.Fatalf("not completed: %s", p.Reason)
	}
	if len(p.Outputs) != 1 || p.Outputs[0].Name != "generated-code" {
		t.Fatalf("want the one output generated-code: %+v", p.Outputs)
	}
	o := p.Outputs[0]
	returned := map[string][]byte{}
	var paths []string
	for _, f := range o.Files {
		if !matches(genWritable, f.Path) {
			t.Errorf("a returned file is outside the writable entries: %s", f.Path)
		}
		raw, _ := base64.StdEncoding.DecodeString(f.Content)
		returned[f.Path] = raw
		paths = append(paths, f.Path)
	}
	if !sort.StringsAreSorted(paths) {
		t.Error("files are not in byte order of path")
	}
	if o.SHA256 != sum(framed(returned)) {
		t.Error("the output's sha256 is not the framed hash of its files")
	}
	// On disk: the files matching the writable entries are exactly the returned files.
	onDisk := stepFiles(t, dir, genWritable)
	if len(onDisk) != len(returned) {
		t.Errorf("%d writable files on disk, %d returned (stale files must be removed)", len(onDisk), len(returned))
	}
	for path, want := range returned {
		if got, ok := onDisk[path]; !ok || string(got) != string(want) {
			t.Errorf("%s on disk is not the returned file", path)
		}
	}
	for _, stale := range []string{"gen/old.go", "gen/sub/old2.go", "vendor/stale/x.go"} {
		if _, err := os.Lstat(filepath.Join(dir, stale)); err == nil {
			t.Errorf("stale %s was not removed", stale)
		}
	}
	for path, want := range notWritable {
		if string(read(t, dir, path)) != string(want) {
			t.Errorf("%s is outside the writable entries and must be untouched", path)
		}
	}
	if !reflect.DeepEqual(p.ProjectFiles, listFiles(t, dir)) {
		t.Errorf("project_files must be the folder after the writes")
	}
	// The written project builds offline from vendor/ alone.
	cmd := execIn(dir, []string{"GOFLAGS=-mod=vendor", "GOMODCACHE=" + t.TempDir()}, "go", "build", "./...")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("go build -mod=vendor ./... in the written project: %v\n%s", err, out)
	}
	// A repeat leaves the same files and gives the same answer.
	snap := snapshot(t, dir)
	r2 := post(t, base, "/procedures/generate/run", body)
	if string(r2.body) != string(r.body) {
		t.Error("the repeat gave another response")
	}
	if d := sameSnapshot(snap, snapshot(t, dir)); d != "" {
		t.Errorf("the repeat changed the files: %s", d)
	}
}

func TestProcedureTimeLimit(t *testing.T) {
	root, dir := fixture(t)
	base := serve(t, withOapi(t, workerConfig(t, root, "generate", "generate")))
	wantProblem(t, post(t, base, "/procedures/generate/run", generateBody(dir, readTestdata(t, "contract.yaml"), 1)), 504, "TimeLimitExceeded")
	// A repeat converges: with enough time it completes.
	p := wantProcedure(t, post(t, base, "/procedures/generate/run", generateBody(dir, readTestdata(t, "contract.yaml"), 300)), "generate")
	if !p.Completed {
		t.Fatalf("a repeat after a 504 must converge: %s", p.Reason)
	}
}
