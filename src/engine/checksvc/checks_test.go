package checksvc_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"

	"go.yaml.in/yaml/v3"
)

func appendTo(t testing.TB, dir, rel, s string) {
	t.Helper()
	write(t, dir, rel, append(read(t, dir, rel), s...))
}

func reviewExamples(t testing.TB) string {
	return string(readTestdata(t, "review", "features", "slice.feature"))
}

func finding(rule, file string, line int, blocking bool) map[string]any {
	// `check` is the schema's confirming check; kept because run-event.schema.json requires it of a
	// blocking finding today (ambiguity A1 in the report).
	f := map[string]any{"rule": rule, "text": "the greeting leaves the name out", "blocking": blocking}
	if file != "" {
		f["file"], f["line"] = file, line
	}
	if blocking {
		f["check"] = "AllTestsPass"
	}
	return f
}

// ---- 3. every pattern check but ContractLintPasses: clean passes, planted fails ----

type pcase struct {
	check string
	step  map[string]any
	// base writes the project; planted picks the planted variant of a fixture that differs by
	// fixture. before is listed after base.
	base func(t *testing.T, dir string, planted bool)
	// change is the attempt: it returns what a planted failure's reason must contain ("" = nothing).
	change func(t *testing.T, dir string, planted bool) string
	mut    func(t *testing.T, dir string, b map[string]any)
	oapi   bool
}

var (
	genWritable  = []string{"gen/**", "go.mod", "go.sum", "vendor/**", "internal/app/app.go"}
	codeWritable = []string{"internal/**", "cmd/**"}
	forbid       = []string{"os/exec", "syscall", "unsafe", "plugin", "net/rpc/..."}
)

func fromTestdata(parts ...string) func(t *testing.T, dir string, planted bool) {
	return func(t *testing.T, dir string, _ bool) {
		copyTree(t, filepath.Join(append([]string{"testdata"}, parts...)...), dir)
	}
}

func eitherTestdata(clean, planted string) func(t *testing.T, dir string, planted bool) {
	return func(t *testing.T, dir string, p bool) {
		d := clean
		if p {
			d = planted
		}
		copyTree(t, filepath.Join("testdata", d), dir)
	}
}

func contractFiles(t *testing.T, dir string, _ bool) {
	write(t, dir, "api/openapi.yaml", readTestdata(t, "contract.yaml"))
	write(t, dir, "features/slice.feature", []byte(approvedExamples(t)))
}

func approvedExamples(t testing.TB) string {
	return string(readTestdata(t, "godog", "clean", "features", "slice.feature"))
}

func withFindings(fs ...map[string]any) func(t *testing.T, dir string, b map[string]any) {
	return func(t *testing.T, dir string, b map[string]any) {
		l := []any{}
		for _, f := range fs {
			l = append(l, f)
		}
		b["findings"] = l
	}
}

var patternCases = []pcase{
	{check: "ScopeKept", step: step("generate", []string{"gen/**"}, []string{"generated-code"}, nil, nil),
		base: fromTestdata("buildok"),
		change: func(t *testing.T, dir string, planted bool) string {
			if planted {
				write(t, dir, "internal/x.go", []byte("package internal\n"))
				return "internal/x.go"
			}
			write(t, dir, "gen/new.go", []byte("package gen\n"))
			return ""
		}},
	{check: "DependenciesAllowed", step: step("tests", []string{"tests/**"}, []string{"tests"}, []string{"std"}, forbid),
		base: fromTestdata("testspass"),
		change: func(t *testing.T, dir string, planted bool) string {
			if planted {
				write(t, dir, "tests/bad_test.go", []byte("package tests\n\nimport _ \"os/exec\"\n"))
				return "os/exec"
			}
			write(t, dir, "tests/good_test.go", []byte("package tests\n\nimport _ \"fmt\"\n"))
			return ""
		}},
	{check: "ContractMatchesApproval", step: step("contract-check", []string{"api/openapi.yaml", "features/slice.feature"}, []string{"contract", "examples"}, nil, nil),
		base: contractFiles,
		change: func(t *testing.T, dir string, planted bool) string {
			if planted {
				appendTo(t, dir, "api/openapi.yaml", "# edited\n")
				return "api/openapi.yaml"
			}
			return ""
		}},
	{check: "ExamplesMatchApproval", step: step("contract-check", []string{"api/openapi.yaml", "features/slice.feature"}, []string{"contract", "examples"}, nil, nil),
		base: contractFiles,
		change: func(t *testing.T, dir string, planted bool) string {
			if planted {
				appendTo(t, dir, "features/slice.feature", "\n")
				return "features/slice.feature"
			}
			return ""
		}},
	{check: "GenerationRepeatable", step: step("generate", genWritable, []string{"generated-code"}, nil, nil), oapi: true,
		base: func(t *testing.T, dir string, _ bool) { copyTree(t, generatedProject(t), dir) },
		change: func(t *testing.T, dir string, planted bool) string {
			if !planted {
				return ""
			}
			for _, f := range listFiles(t, dir) {
				if strings.HasPrefix(f.Path, "gen/") && strings.HasSuffix(f.Path, ".go") {
					appendTo(t, dir, f.Path, "\n// edited by hand\n")
					return f.Path
				}
			}
			t.Fatal("the generated project has no gen/*.go")
			return ""
		}},
	{check: "GeneratedCodeBuilds", step: step("generate", genWritable, []string{"generated-code"}, nil, nil), oapi: true,
		base: func(t *testing.T, dir string, _ bool) { copyTree(t, generatedProject(t), dir) },
		change: func(t *testing.T, dir string, planted bool) string {
			if planted {
				write(t, dir, "gen/zz_bad.go", []byte("package gen\n\nconst Bad = undefinedName\n"))
				return "gen/zz_bad.go"
			}
			return ""
		}},
	{check: "TestsBuild", step: step("tests", []string{"tests/**"}, []string{"tests"}, nil, nil),
		base: fromTestdata("testspass"),
		change: func(t *testing.T, dir string, planted bool) string {
			if planted {
				write(t, dir, "tests/broken_test.go", readTestdata(t, "testsnobuild", "tests", "broken_test.go"))
				return "tests/broken_test.go"
			}
			return ""
		}},
	{check: "EveryExampleBound", step: step("tests", []string{"tests/**"}, []string{"tests"}, nil, nil),
		base: eitherTestdata("godog/clean", "godog/planted"),
		change: func(t *testing.T, dir string, planted bool) string {
			return map[bool]string{true: "undefined"}[planted]
		}},
	{check: "TestsFailAgainstStub", step: step("tests", []string{"tests/**"}, []string{"tests"}, nil, nil),
		base:   eitherTestdata("testsfail", "testspass"),
		change: func(t *testing.T, dir string, planted bool) string { return "" }},
	{check: "TestsUnchanged", step: step("code", codeWritable, []string{"code"}, nil, nil),
		base: fromTestdata("testspass"),
		mut: func(t *testing.T, dir string, b map[string]any) {
			b["earlier_steps"] = []any{step("tests", []string{"tests/**"}, []string{"tests"}, nil, nil)}
			delete(b["earlier_steps"].([]any)[0].(map[string]any), "dependencies")
		},
		change: func(t *testing.T, dir string, planted bool) string {
			if planted {
				appendTo(t, dir, "tests/ok_test.go", "// edited\n")
				return "tests/ok_test.go"
			}
			write(t, dir, "internal/x.go", []byte("package internal\n"))
			return ""
		}},
	{check: "GeneratedCodeUnchanged", step: step("code", codeWritable, []string{"code"}, nil, nil),
		base: fromTestdata("buildok"),
		mut: func(t *testing.T, dir string, b map[string]any) {
			e := step("generate", genWritable, []string{"generated-code"}, nil, nil)
			delete(e, "dependencies")
			b["earlier_steps"] = []any{e}
		},
		change: func(t *testing.T, dir string, planted bool) string {
			if planted {
				appendTo(t, dir, "gen/gen.go", "// edited\n")
				return "gen/gen.go"
			}
			write(t, dir, "internal/x.go", []byte("package internal\n"))
			return ""
		}},
	{check: "AllTestsPass", step: step("code", codeWritable, []string{"code"}, nil, nil),
		base:   eitherTestdata("testspass", "testsfail"),
		change: func(t *testing.T, dir string, planted bool) string { return "" }},
	{check: "FindingsCarryEvidence", step: step("review", nil, []string{"findings"}, nil, nil),
		base: fromTestdata("review"),
		mut:  nil, // set per variant below
		change: func(t *testing.T, dir string, planted bool) string {
			return map[bool]string{true: "internal/missing.go"}[planted]
		}},
	// The witness fixture less review/, which the attempt adds, so every witness is new; the step,
	// its outputs and earlier steps are the review's (witness_test.go), set per variant below.
	{check: "NoBlockingFindingOpen", step: step("review", []string{"review/**"}, []string{"findings"}, nil, nil),
		base: func(t *testing.T, dir string, _ bool) {
			copyTree(t, filepath.Join("testdata", "witness"), dir)
			if err := os.RemoveAll(filepath.Join(dir, "review")); err != nil {
				t.Fatal(err)
			}
		},
		change: func(t *testing.T, dir string, planted bool) string {
			copyTree(t, filepath.Join("testdata", "witness", "review"), filepath.Join(dir, "review"))
			return map[bool]string{true: "R-1"}[planted]
		}},
	// T012 (feature 009): the value checks. DeclaredValuesGenerated judges gen/declared.go of the
	// generated project against the approved contract (which declares no value here).
	{check: "DeclaredValuesGenerated", step: step("generate", genWritable, []string{"generated-code"}, nil, nil), oapi: true,
		base: func(t *testing.T, dir string, _ bool) { copyTree(t, generatedProject(t), dir) },
		change: func(t *testing.T, dir string, planted bool) string {
			if planted {
				appendTo(t, dir, "gen/declared.go", "\nconst Invented = 1\n")
				return "gen/declared.go"
			}
			return ""
		}},
	// DeclaredValuesNotRepeated judges the code step's files against the APPROVED contract (in the
	// request), here one declaring maxLength 200; other literals (0, 1, a string) are not judged.
	{check: "DeclaredValuesNotRepeated", step: step("code", codeWritable, []string{"code"}, nil, nil),
		base: func(t *testing.T, dir string, _ bool) {
			copyTree(t, filepath.Join("testdata", "buildok"), dir)
			write(t, dir, "api/openapi.yaml", []byte(dvContract("200")))
			write(t, dir, "gen/declared.go", []byte(dvDeclared("200")))
		},
		mut: func(t *testing.T, dir string, b map[string]any) {
			b["approved"] = approvedParts(dvContract("200"), approvedExamples(t))
		},
		change: func(t *testing.T, dir string, planted bool) string {
			limit := "gen.DeclaredNewNoteTextMaxLength"
			if planted {
				limit = "200"
			}
			write(t, dir, "internal/note/note.go", []byte("package note\n\nimport \"example.com/p/gen\"\n\nvar _ = gen.Name\n\n"+
				"// TooLong says whether text exceeds the limit.\nfunc TooLong(text string) bool {\n\tif text == \"\" || len(text) < 1 {\n\t\treturn false\n\t}\n"+
				"\treturn len(text) > "+limit+"\n}\n"))
			if planted {
				return "internal/note/note.go"
			}
			return ""
		}},
	// DeploymentValuesConfigured: the service's listen address comes from gen/config.go.
	{check: "DeploymentValuesConfigured", step: step("code", codeWritable, []string{"code"}, nil, nil),
		base: fromTestdata("buildok"),
		change: func(t *testing.T, dir string, planted bool) string {
			listen := "gen.ListenAddress()"
			if planted {
				listen = "\":8080\""
			}
			write(t, dir, "cmd/notes/main.go", []byte("package main\n\nimport (\n\t\"net/http\"\n\n\t\"example.com/p/gen\"\n)\n\n"+
				"func main() {\n\t_ = gen.Name\n\t_ = http.ListenAndServe("+listen+", nil)\n}\n"))
			if planted {
				return "cmd/notes/main.go"
			}
			return ""
		}},
}

func TestEveryPatternCheckIsCovered(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "catalogue", "api-feature-go", "pattern.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Steps []struct {
			Checks []string `yaml:"checks"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, s := range p.Steps {
		for _, c := range s.Checks {
			if c != "ContractLintPasses" {
				want[c] = true
			}
		}
	}
	got := map[string]bool{}
	for _, c := range patternCases {
		got[c.check] = true
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("the cases cover %v, the pattern's checks other than ContractLintPasses are %v", got, want)
	}
}

func TestPatternChecksCleanPassPlantedFail(t *testing.T) {
	for _, pc := range patternCases {
		for _, planted := range []bool{false, true} {
			name := pc.check + "/clean"
			if planted {
				name = pc.check + "/planted"
			}
			t.Run(name, func(t *testing.T) {
				root, dir := fixture(t)
				pc.base(t, dir, planted)
				before := listFiles(t, dir)
				want := pc.change(t, dir, planted)
				mut := pc.mut
				if pc.check == "FindingsCarryEvidence" {
					f := finding("R-1", "internal/greet/greet.go", 7, true)
					if planted {
						f = finding("R-1", "internal/missing.go", 7, true)
					}
					mut = withFindings(f)
				}
				if pc.check == "NoBlockingFindingOpen" {
					f := witFinding("R-2", witGreet, 23, "review/r2/print_test.go", "TestR2PrintsOnly")
					if planted {
						f = witFinding("R-1", witGreet, 23, witR1, witT1)
					}
					mut = func(t *testing.T, dir string, b map[string]any) {
						withFindings(f)(t, dir, b)
						b["step"] = witReviewStep()
						b["outputs"] = []any{
							map[string]any{"name": "findings", "sha256": sum(nil)},
							map[string]any{"name": "witnesses", "sha256": sum(framed(stepFiles(t, dir, witReviewWritable)))},
						}
						b["earlier_steps"] = witEarlierOfReview()
					}
				}
				c := checkConfig(t, root)
				if pc.oapi {
					c = withOapi(t, c)
				}
				base := serve(t, c)
				examples := approvedExamples(t)
				if strings.HasPrefix(pc.check, "Findings") || strings.HasPrefix(pc.check, "NoBlocking") {
					examples = reviewExamples(t)
				}
				body := checkBody(t, dir, pc.step, before, string(readTestdata(t, "contract.yaml")), examples)
				if mut != nil {
					mut(t, dir, body)
				}
				snap := snapshot(t, dir)
				v := runCheck(t, base, pc.check, body)
				if d := sameSnapshot(snap, snapshot(t, dir)); d != "" {
					t.Errorf("a check must not write the project folder: %s", d)
				}
				if !reflect.DeepEqual(v.ProjectFiles, listFiles(t, dir)) {
					t.Errorf("project_files is not the folder's regular files: %v", v.ProjectFiles)
				}
				noHost(t, v.Reason, root)
				if !planted {
					if !v.Passed {
						t.Fatalf("a clean project failed %s: %s", pc.check, v.Reason)
					}
					return
				}
				if v.Passed {
					t.Fatalf("a planted failure passed %s: %s", pc.check, v.Reason)
				}
				if want != "" && !strings.Contains(v.Reason, want) {
					t.Errorf("the reason must name %q: %s", want, v.Reason)
				}
			})
		}
	}
}

// ---- 4. binding: the verdict concerns the files the engine stored ----

func TestOutputHashThatDiffersFromFolderFails(t *testing.T) {
	root, dir := fixture(t, "buildok")
	base := serve(t, checkConfig(t, root))
	st := step("generate", genWritable, []string{"generated-code"}, nil, nil)
	body := checkBody(t, dir, st, listFiles(t, dir), "c", "e")
	if v := runCheck(t, base, "ScopeKept", body); !v.Passed {
		t.Fatalf("the matching hash was rejected: %s", v.Reason)
	}
	body["outputs"].([]any)[0].(map[string]any)["sha256"] = sum([]byte("something else"))
	v := runCheck(t, base, "ScopeKept", body)
	if v.Passed || !strings.Contains(v.Reason, "generated-code") {
		t.Fatalf("an output hash that differs from the folder must give passed false naming the output: %+v", v)
	}
	// Any check, not only ScopeKept.
	if v := runCheck(t, base, "AllTestsPass", body); v.Passed || !strings.Contains(v.Reason, "generated-code") {
		t.Fatalf("AllTestsPass: the same: %+v", v)
	}
}

func TestInputHashThatDiffersFromFolderFails(t *testing.T) {
	root, dir := fixture(t, "testspass")
	base := serve(t, checkConfig(t, root))
	st := step("code", codeWritable, []string{"code"}, nil, nil)
	earlier := step("tests", []string{"tests/**"}, []string{"tests"}, nil, nil)
	delete(earlier, "dependencies")
	mk := func(h string) map[string]any {
		b := checkBody(t, dir, st, listFiles(t, dir), "c", "e")
		b["earlier_steps"] = []any{earlier}
		b["inputs"] = []any{
			map[string]any{"name": "tests", "sha256": h},
			// Not sets of project files: never recomputed, whatever hash they carry.
			map[string]any{"name": "slice/rules", "sha256": sum([]byte("whatever"))},
		}
		return b
	}
	right := sum(framed(stepFiles(t, dir, []string{"tests/**"})))
	if v := runCheck(t, base, "ScopeKept", mk(right)); !v.Passed {
		t.Fatalf("the matching input hash was rejected: %s", v.Reason)
	}
	v := runCheck(t, base, "ScopeKept", mk(sum([]byte("other"))))
	if v.Passed || !strings.Contains(v.Reason, "tests") {
		t.Fatalf("an input hash that differs from the folder must give passed false naming the input: %+v", v)
	}
}

func TestNonFileOutputsAreNotRecomputed(t *testing.T) {
	root, dir := fixture(t, "review")
	base := serve(t, checkConfig(t, root))
	st := step("review", nil, []string{"findings"}, nil, nil)
	body := checkBody(t, dir, st, listFiles(t, dir), "c", reviewExamples(t))
	body["outputs"] = []any{map[string]any{"name": "findings", "sha256": sum([]byte("the worker's report"))}}
	if v := runCheck(t, base, "ScopeKept", body); !v.Passed {
		t.Fatalf("the review step's findings output has no files to recompute: %s", v.Reason)
	}
}

func TestSeveralOutputsBindToWritableEntries(t *testing.T) {
	root, dir := fixture(t)
	base := serve(t, checkConfig(t, root))
	contractCheck := step("contract-check", []string{"api/openapi.yaml", "features/slice.feature"}, []string{"contract", "examples"}, nil, nil)
	contractFiles(t, dir, false)
	body := checkBody(t, dir, contractCheck, nil, string(readTestdata(t, "contract.yaml")), approvedExamples(t))
	if v := runCheck(t, base, "ContractMatchesApproval", body); !v.Passed {
		t.Fatalf("output i = the files of writable entry i, and these hashes are that: %s", v.Reason)
	}
	// Swapped: the hash of features/slice.feature under `contract`.
	o := body["outputs"].([]any)
	o[0].(map[string]any)["sha256"], o[1].(map[string]any)["sha256"] = o[1].(map[string]any)["sha256"], o[0].(map[string]any)["sha256"]
	if v := runCheck(t, base, "ContractMatchesApproval", body); v.Passed || !strings.Contains(v.Reason, "contract") {
		t.Fatalf("swapped output hashes must fail naming the output: %+v", v)
	}
}

func TestTamperedApprovedIsInputHashMismatch(t *testing.T) {
	root, dir := fixture(t)
	contractFiles(t, dir, false)
	base := serve(t, checkConfig(t, root))
	st := step("contract-check", []string{"api/openapi.yaml", "features/slice.feature"}, []string{"contract", "examples"}, nil, nil)
	for i, part := range []string{"goals", "rules", "contract"} {
		for _, what := range []string{"content", "sha256"} {
			body := checkBody(t, dir, st, nil, string(readTestdata(t, "contract.yaml")), approvedExamples(t))
			a := body["approved"].([]any)[i].(map[string]any)
			if what == "content" {
				raw, _ := base64.StdEncoding.DecodeString(a["content"].(string))
				a["content"] = b64(append(raw[:len(raw)-1:len(raw)-1], ' ', raw[len(raw)-1]))
			} else {
				a["sha256"] = sum([]byte("not the part"))
			}
			// Even a check that would pass reads nothing of a tampered approval.
			for _, check := range []string{"ContractMatchesApproval", "ScopeKept"} {
				p := wantProblem(t, post(t, base, "/checks/"+check+"/run", body), 422, "InputHashMismatch")
				if !strings.Contains(p.Detail, part) {
					t.Errorf("%s of %s: detail must name the part: %q", what, part, p.Detail)
				}
			}
		}
	}
}

func TestProjectFilesListsTheFolder(t *testing.T) {
	root, dir := fixture(t, "review")
	write(t, dir, "z/deep/er/file.txt", []byte("x"))
	write(t, dir, "A.txt", []byte("upper case sorts before lower case"))
	os.MkdirAll(filepath.Join(dir, "empty", "dir"), 0o755)
	base := serve(t, checkConfig(t, root))
	st := step("generate", genWritable, []string{"generated-code"}, nil, nil)
	v := runCheck(t, base, "ScopeKept", checkBody(t, dir, st, nil, "c", "e"))
	want := listFiles(t, dir)
	if !reflect.DeepEqual(v.ProjectFiles, want) {
		t.Fatalf("project_files must be every regular file in byte order of path, with its SHA-256:\n got %v\nwant %v", v.ProjectFiles, want)
	}
	if want[0].Path != "A.txt" {
		t.Fatal("test setup: A.txt should sort first")
	}
}

// ---- 5. path attacks: the project folder ----

func TestProjectFolderThatIsNotAFolderIsUnreadable(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "secret.txt", []byte("outside"))
	os.MkdirAll(filepath.Join(root, "p1", "gen"), 0o755)
	write(t, filepath.Join(root, "p1"), "gen/x.go", []byte("package gen\n"))
	write(t, root, "afile", []byte("a regular file, not a folder"))
	for name, target := range map[string]string{
		"lnk-dir": "p1", "lnk-abs": outside, "lnk-file": "p1/gen/x.go", "lnk-dangling": "nowhere", "lnk-root": ".", "lnk-up": "..",
	} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	snap := snapshot(t, root)
	cfg := checkConfig(t, root)
	check, worker := serve(t, cfg), serve(t, workerConfig(t, root, "contract-check", "contract-check"))
	for _, name := range []string{"lnk-dir", "lnk-abs", "lnk-file", "lnk-dangling", "lnk-root", "lnk-up", "afile", "does-not-exist"} {
		fake := filepath.Join(root, name)
		st := step("generate", []string{"gen/**"}, []string{"generated-code"}, nil, nil)
		b := common(name, st, nil)
		cb := common(name, st, nil)
		cb["approved"], cb["earlier_steps"], cb["inputs"], cb["outputs"] = approvedParts("c", "e"), []any{}, []any{}, []any{map[string]any{"name": "generated-code", "sha256": sum(nil)}}
		pb := contractCheckBody(t, fake)
		pb["project"] = name
		for op, r := range map[string]resp{
			"readStepFiles": post(t, check, "/step-files", b),
			"runCheck":      post(t, check, "/checks/ScopeKept/run", cb),
			"runProcedure":  post(t, worker, "/procedures/contract-check/run", pb),
		} {
			p := wantProblem(t, r, 422, "UnreadableProject")
			if strings.Contains(p.Detail, root) || strings.Contains(p.Detail, outside) {
				t.Errorf("%s %s: detail names a host path: %q", op, name, p.Detail)
			}
		}
	}
	if d := sameSnapshot(snap, snapshot(t, root)); d != "" {
		t.Errorf("nothing may be written: %s", d)
	}
}

// ---- 5. path attacks: entries inside the project folder ----

type entry struct {
	name   string
	make   func(t *testing.T, dir, outside string)
	named  string // the path the detail or reason must name ("" = cannot be asserted)
	isLink bool
}

func entries() []entry {
	return []entry{
		{"symlink to a file", func(t *testing.T, dir, out string) { symlink(t, filepath.Join(out, "secret.txt"), dir, "gen/link.go") }, "gen/link.go", true},
		{"symlink to a file by a relative path", func(t *testing.T, dir, out string) { symlink(t, "../go.mod", dir, "gen/rel.go") }, "gen/rel.go", true},
		{"symlink to a folder", func(t *testing.T, dir, out string) { symlink(t, out, dir, "gen/dirlink") }, "gen/dirlink", true},
		{"symlink that replaces a writable folder", func(t *testing.T, dir, out string) {
			os.RemoveAll(filepath.Join(dir, "internal"))
			symlink(t, out, dir, "internal")
		}, "internal", true},
		{"dangling symlink", func(t *testing.T, dir, out string) { symlink(t, "nowhere", dir, "gen/dangling") }, "gen/dangling", true},
		{"symlink to the project folder itself", func(t *testing.T, dir, out string) { symlink(t, ".", dir, "gen/self") }, "gen/self", true},
		{"symlink to the folder above", func(t *testing.T, dir, out string) { symlink(t, "../..", dir, "gen/up") }, "gen/up", true},
		{"symlink to the projects root", func(t *testing.T, dir, out string) { symlink(t, filepath.Dir(dir), dir, "projects") }, "projects", true},
		{"symlink loop", func(t *testing.T, dir, out string) { symlink(t, "loop", dir, "loop") }, "loop", true},
		{"named pipe", func(t *testing.T, dir, out string) {
			if err := syscall.Mkfifo(filepath.Join(dir, "gen", "pipe"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "gen/pipe", false},
		{"file name with a space", func(t *testing.T, dir, out string) { write(t, dir, "gen/a b.go", []byte("x")) }, "", false},
		{"file name with a control character", func(t *testing.T, dir, out string) { write(t, dir, "gen/a\x01b.go", []byte("x")) }, "", false},
		{"file name with a newline", func(t *testing.T, dir, out string) { write(t, dir, "gen/a\nb.go", []byte("x")) }, "", false},
		{"file name that is not ASCII", func(t *testing.T, dir, out string) { write(t, dir, "gen/café.go", []byte("x")) }, "", false},
		{"file name with a colon", func(t *testing.T, dir, out string) { write(t, dir, "gen/a:b.go", []byte("x")) }, "gen/a:b.go", false},
		{"path longer than 256 characters", func(t *testing.T, dir, out string) {
			a := strings.Repeat("a", 100)
			write(t, dir, "gen/"+a+"/"+a+"/"+a+".go", []byte("x"))
		}, "", false},
	}
}

func symlink(t *testing.T, target, dir, rel string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
}

func entryProject(t *testing.T, e entry) (root, dir, outside string) {
	root, dir = fixture(t, "review")
	write(t, dir, "gen/x.go", []byte("package gen\n"))
	outside = t.TempDir()
	write(t, outside, "secret.txt", []byte("outside content"))
	e.make(t, dir, outside)
	return
}

func TestUnreadableEntriesAreNeverRead(t *testing.T) {
	for _, e := range entries() {
		t.Run(e.name, func(t *testing.T) {
			root, dir, outside := entryProject(t, e)
			base := serve(t, checkConfig(t, root))
			st := step("generate", genWritable, []string{"generated-code"}, nil, nil)
			before := snapshot(t, dir)
			// readStepFiles: 422 UnreadableProject, wherever in the folder the entry is.
			p := wantProblem(t, post(t, base, "/step-files", common("p1", st, nil)), 422, "UnreadableProject")
			if e.named != "" && !strings.Contains(p.Detail, e.named) {
				t.Errorf("detail must name %q: %q", e.named, p.Detail)
			}
			for _, h := range []string{root, outside} {
				if strings.Contains(p.Detail, h) {
					t.Errorf("detail names the host path %s: %q", h, p.Detail)
				}
			}
			// runCheck: a failed check naming it, never a pass by default.
			body := checkBody(t, dir, st, nil, "c", "e")
			for _, check := range []string{"ScopeKept", "DependenciesAllowed"} {
				v := runCheck(t, base, check, body)
				if v.Passed {
					t.Errorf("%s passed on a folder with %s", check, e.name)
				}
				if e.named != "" && !strings.Contains(v.Reason, e.named) {
					t.Errorf("%s: the reason must name %q: %s", check, e.named, v.Reason)
				}
				noHost(t, v.Reason, root, outside)
				for _, f := range v.ProjectFiles {
					if e.named != "" && f.Path == e.named {
						t.Errorf("project_files lists %s, which is never read", f.Path)
					}
					if f.Path == "gen/link.go" || strings.Contains(f.Path, "secret") {
						t.Errorf("project_files lists %s, reached through a link", f.Path)
					}
				}
			}
			if d := sameSnapshot(before, snapshot(t, dir)); d != "" {
				t.Errorf("nothing may be written: %s", d)
			}
			if string(read(t, outside, "secret.txt")) != "outside content" {
				t.Error("a file outside the project was changed")
			}
		})
	}
}

func TestProcedureNeverWritesThroughLinks(t *testing.T) {
	for _, e := range entries() {
		t.Run(e.name, func(t *testing.T) {
			root, dir, outside := entryProject(t, e)
			base := serve(t, workerConfig(t, root, "contract-check", "contract-check"))
			before, outBefore := snapshot(t, dir), snapshot(t, outside)
			p := wantProblem(t, post(t, base, "/procedures/contract-check/run", contractCheckBody(t, dir)), 422, "UnreadableProject")
			if strings.Contains(p.Detail, root) || strings.Contains(p.Detail, outside) {
				t.Errorf("detail names a host path: %q", p.Detail)
			}
			if d := sameSnapshot(before, snapshot(t, dir)); d != "" {
				t.Errorf("422 UnreadableProject is answered before anything is written: %s", d)
			}
			if d := sameSnapshot(outBefore, snapshot(t, outside)); d != "" {
				t.Errorf("a file outside the project was written: %s", d)
			}
		})
	}
}

// A symlink at the very path the procedure writes, or above it: writing through it would leave
// the project.
func TestProcedureWritableLinksAreRefused(t *testing.T) {
	for name, mk := range map[string]func(t *testing.T, dir, out string){
		"api is a link to a folder": func(t *testing.T, dir, out string) { symlink(t, out, dir, "api") },
		"api/openapi.yaml is a link to a file": func(t *testing.T, dir, out string) {
			symlink(t, filepath.Join(out, "secret.txt"), dir, "api/openapi.yaml")
		},
		"features/slice.feature is a dangling link": func(t *testing.T, dir, out string) {
			symlink(t, filepath.Join(out, "new.txt"), dir, "features/slice.feature")
		},
		"features is a link to the project root": func(t *testing.T, dir, out string) { symlink(t, dir, dir, "features") },
	} {
		t.Run(name, func(t *testing.T) {
			root, dir := fixture(t)
			outside := t.TempDir()
			write(t, outside, "secret.txt", []byte("outside content"))
			mk(t, dir, outside)
			base := serve(t, workerConfig(t, root, "contract-check", "contract-check"))
			before, outBefore := snapshot(t, dir), snapshot(t, outside)
			wantProblem(t, post(t, base, "/procedures/contract-check/run", contractCheckBody(t, dir)), 422, "UnreadableProject")
			if d := sameSnapshot(before, snapshot(t, dir)); d != "" {
				t.Errorf("project changed: %s", d)
			}
			if d := sameSnapshot(outBefore, snapshot(t, outside)); d != "" {
				t.Errorf("outside changed: %s", d)
			}
		})
	}
}

func TestMoreThanTenThousandFilesIsUnreadable(t *testing.T) {
	root, dir := fixture(t)
	for i := 0; i < 10000; i++ {
		write(t, dir, fmt.Sprintf("f/%05d.txt", i), nil)
	}
	base := serve(t, checkConfig(t, root))
	st := step("generate", []string{"gen/**"}, []string{"generated-code"}, nil, nil)
	r := post(t, base, "/step-files", common("p1", st, nil))
	if r.status != 200 {
		t.Fatalf("10000 files are the most a project may hold: %d %.300s", r.status, r.body)
	}
	write(t, dir, "f/10000.txt", nil)
	wantProblem(t, post(t, base, "/step-files", common("p1", st, nil)), 422, "UnreadableProject")
	wantProblem(t, post(t, base, "/checks/ScopeKept/run", checkBody(t, dir, st, nil, "c", "e")), 422, "UnreadableProject")
}

// ---- 6. readStepFiles ----

type stepFilesResp struct {
	SHA256       string        `json:"sha256"`
	Files        []fileContent `json:"files"`
	Removed      []string      `json:"removed"`
	ProjectFiles []pf          `json:"project_files"`
}

type fileContent struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Content string `json:"content"`
}

func readFiles(t testing.TB, r resp) stepFilesResp {
	t.Helper()
	if r.status != 200 {
		t.Fatalf("readStepFiles: %d %.400s", r.status, r.body)
	}
	var s stepFilesResp
	if err := strictJSON(r.body, &s); err != nil || s.Files == nil || s.Removed == nil || s.ProjectFiles == nil {
		t.Fatalf("not a StepFiles with files, removed and project_files all set: %v %.300s", err, r.body)
	}
	return s
}

func TestReadStepFilesReturnsExactlyTheWritableFiles(t *testing.T) {
	root, dir := fixture(t)
	for p, c := range map[string]string{
		"gen/a.go": "package gen\n", "gen/sub/b.go": "package sub\n", "go.mod": "module m\n", "go.sum": "",
		"vendor/m/x.go": "package m\n", "vendor/modules.txt": "# m v1\n",
		"go.mod.bak": "no", "gen2/x.go": "no", "internal/y.go": "no", "api/openapi.yaml": "no", "tests/t_test.go": "no", "agen/x.go": "no",
	} {
		write(t, dir, p, []byte(c))
	}
	os.MkdirAll(filepath.Join(dir, "gen", "emptydir"), 0o755)
	before := []pf{{"gen/gone.go", sum(nil)}, {"vendor/gone/z.go", sum(nil)}, {"internal/gone.go", sum(nil)}, {"go.sum", sum(nil)}, {"go.work", sum(nil)}}
	base := serve(t, checkConfig(t, root))
	st := step("generate", genWritable, []string{"generated-code"}, nil, nil)
	s := readFiles(t, post(t, base, "/step-files", common("p1", st, before)))

	want := stepFiles(t, dir, genWritable)
	var paths []string
	for p := range want {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var got []string
	for _, f := range s.Files {
		got = append(got, f.Path)
		raw, err := base64.StdEncoding.DecodeString(f.Content)
		if err != nil || string(raw) != string(want[f.Path]) {
			t.Errorf("%s: content %q is not the file's bytes", f.Path, f.Content)
		}
		if f.SHA256 != sum(want[f.Path]) {
			t.Errorf("%s: sha256 is not the file's hash", f.Path)
		}
	}
	if !reflect.DeepEqual(got, paths) {
		t.Fatalf("files must be exactly the regular files matching the writable entries, in byte order:\n got %v\nwant %v", got, paths)
	}
	if s.SHA256 != sum(framed(want)) {
		t.Errorf("sha256 must be the framed hash of the files")
	}
	if !reflect.DeepEqual(s.Removed, []string{"gen/gone.go", "vendor/gone/z.go"}) {
		t.Errorf("removed must be the writable paths `before` lists that the folder no longer holds: %v", s.Removed)
	}
	if !reflect.DeepEqual(s.ProjectFiles, listFiles(t, dir)) {
		t.Errorf("project_files must be every regular file of the folder: %v", s.ProjectFiles)
	}

	// No writable entries: no files, whatever `before` says.
	none := step("review", nil, []string{"findings"}, nil, nil)
	s = readFiles(t, post(t, base, "/step-files", common("p1", none, before)))
	if len(s.Files) != 0 || len(s.Removed) != 0 || s.SHA256 != sum(nil) {
		t.Errorf("a step with no writable entries gets no files and the hash of no bytes: %+v", s)
	}
	// Entries matching nothing.
	st2 := step("tests", []string{"nothing/**", "no.file"}, []string{"tests"}, nil, nil)
	s = readFiles(t, post(t, base, "/step-files", common("p1", st2, nil)))
	if len(s.Files) != 0 || s.SHA256 != sum(nil) {
		t.Errorf("no matching file gives no files and the hash of no bytes: %+v", s)
	}
}

func TestStepFilesLimitIsInclusive(t *testing.T) {
	const limit = 8388608
	for name, sizes := range map[string][]int{
		"one file at the limit":    {limit},
		"two files at the limit":   {limit / 2, limit / 2},
		"one file over the limit":  {limit + 1},
		"two files over the limit": {limit/2 + 1, limit / 2},
		"a file and an empty file": {limit, 0},
		"many small files over":    {limit - 10, 5, 6},
	} {
		t.Run(name, func(t *testing.T) {
			root, dir := fixture(t)
			total := 0
			for i, n := range sizes {
				write(t, dir, fmt.Sprintf("gen/f%d.bin", i), make([]byte, n))
				total += n
			}
			// Bytes outside the writable paths do not count.
			write(t, dir, "internal/big.bin", make([]byte, limit))
			base := serve(t, checkConfig(t, root))
			st := step("generate", []string{"gen/**"}, []string{"generated-code"}, nil, nil)
			r := post(t, base, "/step-files", common("p1", st, nil))
			if total > limit {
				wantProblem(t, r, 422, "StepFilesTooLarge")
				return
			}
			s := readFiles(t, r)
			if len(s.Files) != len(sizes) {
				t.Errorf("%d files expected: %d", len(sizes), len(s.Files))
			}
		})
	}
}

// ---- 8. the same request gives the same response ----

func TestSameRequestSameResponse(t *testing.T) {
	root, dir := fixture(t, "review")
	write(t, dir, "gen/x.go", []byte("package gen\n"))
	base := serve(t, checkConfig(t, root))
	reviewStep := step("review", nil, []string{"findings"}, nil, nil)
	ct := string(readTestdata(t, "contract.yaml"))
	mk := func(check string, extra func(b map[string]any)) (string, map[string]any) {
		var b map[string]any
		switch check {
		case "NoBlockingFindingOpen", "FindingsCarryEvidence":
			b = checkBody(t, dir, reviewStep, listFiles(t, dir), ct, reviewExamples(t))
			b["findings"] = []any{finding("R-1", "internal/greet/greet.go", 7, true)}
		default:
			b = checkBody(t, dir, step("generate", []string{"gen/**"}, []string{"generated-code"}, nil, nil), nil, ct, reviewExamples(t))
		}
		if extra != nil {
			extra(b)
		}
		return "/checks/" + check + "/run", b
	}
	reqs := map[string]func() (string, map[string]any){
		"ScopeKept": func() (string, map[string]any) { return mk("ScopeKept", nil) },
		"AllTestsPass failing": func() (string, map[string]any) {
			return mk("AllTestsPass", func(b map[string]any) { b["step"] = reviewStep; b["outputs"] = outputsFor(t, dir, reviewStep) })
		},
		"NoBlockingFindingOpen": func() (string, map[string]any) { return mk("NoBlockingFindingOpen", nil) },
		"TestsFailAgainstStub": func() (string, map[string]any) {
			return mk("TestsFailAgainstStub", func(b map[string]any) { b["step"] = reviewStep; b["outputs"] = outputsFor(t, dir, reviewStep) })
		},
	}
	for name, f := range reqs {
		t.Run(name, func(t *testing.T) {
			path, b := f()
			first := post(t, base, path, b)
			if first.status != 200 {
				t.Fatalf("%d %.300s", first.status, first.body)
			}
			// run, attempt and round only name the call in the log.
			for i, tweak := range []func(){
				func() {},
				func() { b["run"], b["attempt"] = "RUN-77", 3 },
				func() {
					b["round"] = 2
					if path == "/checks/NoBlockingFindingOpen/run" {
						b["round_scope"] = map[string]any{"open": b["findings"], "changed_lines": []any{}}
					}
				},
			} {
				tweak()
				again := post(t, base, path, b)
				if again.status != 200 || string(again.body) != string(first.body) {
					t.Errorf("repeat %d gave another response:\n%s\nvs\n%s", i, first.body, again.body)
				}
			}
		})
	}
	st := step("generate", genWritable, []string{"generated-code"}, nil, nil)
	b := common("p1", st, nil)
	a1 := post(t, base, "/step-files", b)
	b["run"], b["attempt"] = "RUN-9", 2
	if a2 := post(t, base, "/step-files", b); string(a1.body) != string(a2.body) {
		t.Error("readStepFiles: the same request gave another response")
	}
}
