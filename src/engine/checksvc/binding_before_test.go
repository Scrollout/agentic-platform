package checksvc_test

// T082 (owner, 2026-11-14): runCheck binds an earlier step's output given as an input through
// `before`, not by recomputing its hash from the folder: each of the input's files in the folder
// must equal its entry in `before`, except a path the judged step may itself write. Written
// before the code by a separate author, with the real pattern's writable globs (api-feature-go).
//
// The request's hash for such an input is the hash the engine stored when the earlier step was
// accepted (here: generate's, with the stub seam); the folder may legitimately no longer hash to
// it once the code step replaced internal/app/app.go.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	seamStub = "package app\n\nfunc New() any { return nil } // the generated stub\n"
	seamReal = "package app\n\nfunc New() any { return 1 } // the real implementation\n"
)

// seamProject is the project after generate and tests were accepted: the generated files with the
// stub seam, go.mod and a tests/ file. It returns the folder, the generate and tests outputs'
// hashes as the engine stored them, and the files (the next step's `before`).
func seamProject(t *testing.T) (root, dir, genHash, testsHash string, before []pf) {
	t.Helper()
	root, dir = fixture(t, "buildok")
	write(t, dir, "internal/app/app.go", []byte(seamStub))
	write(t, dir, "tests/greet_test.go", []byte("package tests\n"))
	genHash = sum(framed(stepFiles(t, dir, genWritable)))
	testsHash = sum(framed(stepFiles(t, dir, []string{"tests/**"})))
	return root, dir, genHash, testsHash, listFiles(t, dir)
}

func earlierOf(names ...string) []any {
	all := map[string]map[string]any{
		"generate": witEarlier("generate", genWritable, []string{"generated-code"}),
		"tests":    witEarlier("tests", []string{"tests/**"}, []string{"tests"}),
		"code":     witEarlier("code", codeWritable, []string{"code"}),
	}
	out := []any{}
	for _, n := range names {
		out = append(out, all[n])
	}
	return out
}

func codeBody(t *testing.T, dir string, before []pf, genHash, testsHash string) map[string]any {
	t.Helper()
	b := checkBody(t, dir, step("code", codeWritable, []string{"code"}, nil, nil), before, "c", "e")
	b["earlier_steps"] = earlierOf("generate", "tests")
	b["inputs"] = []any{
		map[string]any{"name": "generated-code", "sha256": genHash},
		map[string]any{"name": "tests", "sha256": testsHash},
		map[string]any{"name": "slice/rules", "sha256": sum([]byte("not files"))},
	}
	return b
}

func TestCodeStepThatReplacedTheSeamBindsItsGeneratedCodeInput(t *testing.T) {
	root, dir, genHash, testsHash, before := seamProject(t)
	base := serve(t, checkConfig(t, root))
	write(t, dir, "internal/app/app.go", []byte(seamReal))
	write(t, dir, "internal/greet/greet.go", []byte("package greet\n"))
	b := codeBody(t, dir, before, genHash, testsHash)
	if sum(framed(stepFiles(t, dir, genWritable))) == genHash {
		t.Fatal("setup: the seam replacement must change generate's output hash")
	}
	v := runCheck(t, base, "ScopeKept", b)
	if !v.Passed {
		t.Fatalf("the code step may write internal/app/app.go, so the input generated-code binds: %s", v.Reason)
	}
}

func TestCodeStepChangingGeneratedFileOutsideItsWritableFailsNamingTheInput(t *testing.T) {
	for name, change := range map[string]func(t *testing.T, dir string){
		"edited gen file": func(t *testing.T, dir string) { appendTo(t, dir, "gen/gen.go", "// edited\n") },
		"edited go.mod":   func(t *testing.T, dir string) { appendTo(t, dir, "go.mod", "// edited\n") },
		"removed gen":     func(t *testing.T, dir string) { removeProjectFile(t, dir, "gen/gen.go") },
	} {
		t.Run(name, func(t *testing.T) {
			root, dir, genHash, testsHash, before := seamProject(t)
			base := serve(t, checkConfig(t, root))
			write(t, dir, "internal/app/app.go", []byte(seamReal))
			change(t, dir)
			v := runCheck(t, base, "ScopeKept", codeBody(t, dir, before, genHash, testsHash))
			if v.Passed || !strings.Contains(v.Reason, "generated-code") {
				t.Fatalf("a changed file of generated-code that the code step may not write must fail naming the input: %+v", v)
			}
			noHost(t, v.Reason, root)
		})
	}
}

func TestCodeStepChangingAnEarlierTestsFileFailsNamingTheInput(t *testing.T) {
	root, dir, genHash, testsHash, before := seamProject(t)
	base := serve(t, checkConfig(t, root))
	write(t, dir, "internal/app/app.go", []byte(seamReal))
	appendTo(t, dir, "tests/greet_test.go", "// edited\n")
	v := runCheck(t, base, "ScopeKept", codeBody(t, dir, before, genHash, testsHash))
	if v.Passed || !strings.Contains(v.Reason, "tests") {
		t.Fatalf("the code step may not write tests/**: the input tests must fail naming it: %+v", v)
	}
}

func TestReviewStepAfterSeamReplacementBindsItsInputs(t *testing.T) {
	root, dir, genHash, testsHash, _ := seamProject(t)
	base := serve(t, checkConfig(t, root))
	// The code step was accepted: its files are in the folder, and the accepted list is the
	// review's `before`.
	write(t, dir, "internal/app/app.go", []byte(seamReal))
	write(t, dir, "internal/greet/greet.go", []byte("package greet\n"))
	codeHash := sum(framed(stepFiles(t, dir, codeWritable)))
	before := listFiles(t, dir)
	// The review adds a witness.
	write(t, dir, "review/r1/name_test.go", []byte("package r1\n"))
	b := checkBody(t, dir, step("code", codeWritable, []string{"code"}, nil, nil), before, "c", "e")
	b["step"] = witReviewStep()
	b["outputs"] = []any{
		map[string]any{"name": "findings", "sha256": sum(nil)},
		map[string]any{"name": "witnesses", "sha256": sum(framed(stepFiles(t, dir, []string{"review/**"})))},
	}
	b["earlier_steps"] = earlierOf("generate", "tests", "code")
	b["inputs"] = []any{
		map[string]any{"name": "generated-code", "sha256": genHash},
		map[string]any{"name": "tests", "sha256": testsHash},
		map[string]any{"name": "code", "sha256": codeHash},
	}
	v := runCheck(t, base, "ScopeKept", b)
	if !v.Passed {
		t.Fatalf("a review step after a code step that replaced the seam binds: %s", v.Reason)
	}
	// The review changing the seam is still caught, naming the input it changed.
	appendTo(t, dir, "internal/greet/greet.go", "// edited by the review\n")
	b["outputs"] = []any{
		map[string]any{"name": "findings", "sha256": sum(nil)},
		map[string]any{"name": "witnesses", "sha256": sum(framed(stepFiles(t, dir, []string{"review/**"})))},
	}
	v = runCheck(t, base, "ScopeKept", b)
	if v.Passed || !strings.Contains(v.Reason, "code") {
		t.Fatalf("a review that changed a file of the code input must fail naming it: %+v", v)
	}
}

func removeProjectFile(t *testing.T, dir, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
}

// A file added within an input's entries, not in `before`, which the judged step may not write,
// fails the binding naming the input, even where a later step may write the input's paths (the
// seam file internal/app/app.go is in generated-code and in code's writable), so the input's
// hash is not recomputed.
func TestReviewStepAddingAFileToAnInputFailsNamingTheInput(t *testing.T) {
	for name, tc := range map[string]struct{ path, input string }{
		"generated-code": {"gen/extra.go", "generated-code"},
		"code":           {"internal/extra/extra.go", "code"},
	} {
		t.Run(name, func(t *testing.T) {
			root, dir, genHash, testsHash, _ := seamProject(t)
			base := serve(t, checkConfig(t, root))
			write(t, dir, "internal/app/app.go", []byte(seamReal))
			write(t, dir, "internal/greet/greet.go", []byte("package greet\n"))
			codeHash := sum(framed(stepFiles(t, dir, codeWritable)))
			before := listFiles(t, dir)
			write(t, dir, "review/r1/name_test.go", []byte("package r1\n"))
			write(t, dir, tc.path, []byte("package extra\n")) // added by the review, outside review/**
			b := checkBody(t, dir, step("code", codeWritable, []string{"code"}, nil, nil), before, "c", "e")
			b["step"] = witReviewStep()
			b["outputs"] = []any{
				map[string]any{"name": "findings", "sha256": sum(nil)},
				map[string]any{"name": "witnesses", "sha256": sum(framed(stepFiles(t, dir, []string{"review/**"})))},
			}
			b["earlier_steps"] = earlierOf("generate", "tests", "code")
			b["inputs"] = []any{
				map[string]any{"name": "generated-code", "sha256": genHash},
				map[string]any{"name": "tests", "sha256": testsHash},
				map[string]any{"name": "code", "sha256": codeHash},
			}
			v := runCheck(t, base, "ScopeKept", b)
			if v.Passed || !strings.Contains(v.Reason, "the input "+tc.input+" differs from before") || !strings.Contains(v.Reason, tc.path+" was added") {
				t.Fatalf("a file added to the input %s must fail binding naming it: %+v", tc.input, v)
			}
		})
	}
}
