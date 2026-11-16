// T075 (fresh review): tests for rules that code already holds, each seen failing against a
// mutant in a scratch copy before it passed on the tree: the `witness` schemas of both contracts,
// ScopeKept stopping generate under review/, and the binding exempting the review's `findings`.
package checksvc_test

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// The check contract's Finding.witness.file is a ProjectPath: one the run-event pattern allows but
// ProjectPath refuses (longer than its 256 characters) makes the request 400 InvalidRequest.
func TestWitnessFileThatIsNotAProjectPathIsInvalidRequest(t *testing.T) {
	root, dir := fixture(t, "witness")
	base := serve(t, checkConfig(t, root))
	if v := runCheck(t, base, "FindingsCarryEvidence", witBody(t, dir, false, witFinding("R-1", witGreet, 23, witR1, witT1))); !v.Passed {
		t.Fatalf("a valid witness was refused: %s", v.Reason)
	}
	long := "review/r1/" + strings.Repeat("a", 250) + "_test.go"
	for _, bad := range []string{long, "../review/r1/a_test.go"} {
		b := witBody(t, dir, false, witFinding("R-1", witGreet, 23, bad, witT1))
		wantProblem(t, post(t, base, "/checks/FindingsCarryEvidence/run", b), 400, "InvalidRequest")
	}
}

// run-event.schema.json's finding.witness: `file` in review/r<n>/ ending _test.go, `test`
// starting TestR<n>, both required.
func TestRunEventWitnessPatterns(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "api", "preview", "2026-11-12-preview"))
	if err != nil {
		t.Fatal(err)
	}
	u := (&url.URL{Scheme: "file", Path: filepath.ToSlash(dir) + "/run-event.schema.json"}).String() + "#/properties/finding"
	s, err := jsonschema.NewCompiler().Compile(u)
	if err != nil {
		t.Fatalf("compile the run event's finding: %v", err)
	}
	valid := func(w map[string]any) error {
		f := map[string]any{"rule": "R-1", "text": "t", "blocking": true, "file": witGreet, "line": 23, "witness": w}
		return s.Validate(f)
	}
	if err := valid(map[string]any{"file": witR1, "test": witT1}); err != nil {
		t.Fatalf("a valid witness was refused: %v", err)
	}
	for name, w := range map[string]map[string]any{
		"a file under tests/":            {"file": "tests/r1/a_test.go", "test": witT1},
		"a file not ending _test.go":     {"file": "review/r1/a.go", "test": witT1},
		"a rule folder with no number":   {"file": "review/rx/a_test.go", "test": witT1},
		"a file below the rule's folder": {"file": "review/r1/sub/a_test.go", "test": witT1},
		"a test not starting TestR<n>":   {"file": witR1, "test": "TestX1Name"},
		"a test with no rule number":     {"file": witR1, "test": "TestRName"},
		"no test":                        {"file": witR1},
		"no file":                        {"test": witT1},
	} {
		if valid(w) == nil {
			t.Errorf("%s was accepted: %v", name, w)
		}
	}
}

// Generate writing under review/ fails ScopeKept, naming the file (research D35, analyze W4).
func TestGenerateWritingUnderReviewFailsScopeKept(t *testing.T) {
	root, dir := fixture(t, "buildok")
	base := serve(t, checkConfig(t, root))
	before := listFiles(t, dir)
	const witness = "review/r1/planted_test.go"
	write(t, dir, witness, []byte("package r1\n"))
	st := step("generate", genWritable, []string{"generated-code"}, nil, nil)
	v := runCheck(t, base, "ScopeKept", checkBody(t, dir, st, before, "c", "e"))
	if v.Passed || !strings.Contains(v.Reason, witness) {
		t.Fatalf("generate wrote %s and passed ScopeKept, or the reason does not name it: %s", witness, v.Reason)
	}
}

// The review's `findings` is no set of files: the binding never recomputes it, as an output of the
// review or as an input of a later step, whatever hash the request gives.
func TestFindingsHashIsNotRecomputed(t *testing.T) {
	root, dir := fixture(t, "witness")
	base := serve(t, checkConfig(t, root))
	other := sum([]byte("the findings as the engine stored them"))
	b := witBody(t, dir, true)
	b["outputs"].([]any)[0].(map[string]any)["sha256"] = other
	if v := runCheck(t, base, "ScopeKept", b); !v.Passed {
		t.Errorf("the review's findings output was recomputed: %s", v.Reason)
	}
	fix := step("code", codeWritable, []string{"code"}, nil, nil)
	b = checkBody(t, dir, fix, listFiles(t, dir), "c", reviewExamples(t))
	b["earlier_steps"] = witEarlierOfFix()
	b["inputs"] = []any{map[string]any{"name": "findings", "sha256": other}}
	if v := runCheck(t, base, "ScopeKept", b); !v.Passed {
		t.Errorf("the fix step's findings input was recomputed: %s", v.Reason)
	}
}
