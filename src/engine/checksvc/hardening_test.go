package checksvc_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The service's settings (cmd/check reads CHECK_*) must not reach the processes it starts: a
// project's test is the project author's code, and the engine's token is not its to read.
func TestChildProcessesDoNotInheritServiceSettings(t *testing.T) {
	settings := map[string]string{
		"CHECK_TOKEN":          token,
		"CHECK_LISTEN_ADDRESS": ":marker-listen",
		"CHECK_PROJECTS_DIR":   "/marker-projects",
		"CHECK_OAPI_CODEGEN":   "/marker-oapi",
		"CHECK_TEMPLATE_DIR":   "/marker-template",
		"CHECK_DEPLOYMENT":     "marker-deployment",
		"CHECK_PROCEDURE":      "marker-procedure",
		"CHECK_STEP":           "marker-step",
	}
	for k, v := range settings {
		t.Setenv(k, v)
	}
	root, dir := fixture(t, "review")
	var names []string
	for k := range settings {
		names = append(names, k)
	}
	// A rule test (R-9) that fails, saying where and printing every setting it can see.
	write(t, dir, "tests/env_test.go", []byte(fmt.Sprintf(`package tests

import (
	"os"
	"strings"
	"testing"
)

func TestR9Env(t *testing.T) {
	var seen []string
	for _, k := range strings.Split(%q, ",") {
		seen = append(seen, k+"="+os.Getenv(k))
	}
	t.Errorf("internal/greet/greet.go:7: %%s", strings.Join(seen, " "))
}
`, strings.Join(names, ","))))
	base := serve(t, checkConfig(t, root))
	st := step("review", nil, []string{"findings"}, nil, nil)
	for _, check := range []string{"NoBlockingFindingOpen", "FindingsCarryEvidence", "AllTestsPass"} {
		b := checkBody(t, dir, st, listFiles(t, dir), "c", reviewExamples(t))
		b["findings"] = []any{finding("R-9", "internal/greet/greet.go", 7, true)}
		r := post(t, base, "/checks/"+check+"/run", b)
		if r.status != 200 {
			t.Fatalf("%s: %d %.300s", check, r.status, r.body)
		}
		for k, v := range settings {
			if strings.Contains(string(r.body), v) {
				t.Errorf("%s: the response contains the value of %s: %.300s", check, k, r.body)
			}
		}
		if check == "NoBlockingFindingOpen" && (!strings.Contains(string(r.body), `"passed":false`) || !strings.Contains(string(r.body), "R-9")) {
			t.Errorf("the planted R-9 test did not run and block, so the leak was not exercised: %.300s", r.body)
		}
	}
}

// A project file far larger than x-limits.step-files-max-bytes (8 MiB) is refused as the contract
// says, and reading it does not hold it in memory.
func TestHugeFileIsNotReadIntoMemory(t *testing.T) {
	const huge = 1 << 30
	root, dir := fixture(t)
	p := filepath.Join(dir, "gen", "huge.bin")
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(p, huge); err != nil { // sparse: costs no disk
		t.Fatal(err)
	}
	base := serve(t, checkConfig(t, root))
	st := step("generate", []string{"gen/**"}, []string{"generated-code"}, nil, nil)
	alloc := func() uint64 { var m runtime.MemStats; runtime.ReadMemStats(&m); return m.TotalAlloc }
	start := alloc()
	wantProblem(t, post(t, base, "/step-files", common("p1", st, nil)), 422, "StepFilesTooLarge")
	if used := alloc() - start; used > 128<<20 {
		t.Errorf("readStepFiles allocated %d MiB for a 1 GiB file: it must stop at the limit", used>>20)
	}
	// A large file outside the writable paths: the answer is the contract's (never a crash), and
	// no more than a bounded amount of memory is used.
	os.Remove(p)
	q := filepath.Join(dir, "internal", "huge.bin")
	os.MkdirAll(filepath.Dir(q), 0o755)
	os.WriteFile(q, nil, 0o644)
	os.Truncate(q, huge)
	start = alloc()
	r := post(t, base, "/step-files", common("p1", st, nil))
	if r.status != 200 && r.status != 422 {
		t.Errorf("a huge file outside the writable paths: %d %.200s", r.status, r.body)
	}
	if used := alloc() - start; used > 128<<20 {
		t.Errorf("listing a 1 GiB file allocated %d MiB: hashing must stream", used>>20)
	}
}

// No response is larger than x-limits.response-body-max-bytes (24 MiB), and no reason is longer
// than 65536 characters, however much there is to report.
func TestResponsesStayWithinTheirLimits(t *testing.T) {
	const responseMax = 25165824
	root, dir := fixture(t)
	long := strings.Repeat("d", 90)
	var before []pf
	files := 0
	// 8 MiB spread over 9000 files with long paths, all writable.
	chunk := make([]byte, 8388608/9000)
	for i := 0; i < 9000; i++ {
		write(t, dir, fmt.Sprintf("gen/%s/%s%05d.go", long, strings.Repeat("n", 90), i), chunk)
		files++
	}
	for i := 0; i < 10000; i++ {
		before = append(before, pf{fmt.Sprintf("gen/%s/%s%05d.gone", long, strings.Repeat("g", 90), i), strings.Repeat("a", 64)})
	}
	base := serve(t, checkConfig(t, root))
	st := step("generate", []string{"gen/**"}, []string{"generated-code"}, nil, nil)
	r := post(t, base, "/step-files", common("p1", st, before))
	if r.status != 200 {
		t.Fatalf("readStepFiles: %d %.300s", r.status, r.body)
	}
	if len(r.body) > responseMax {
		t.Errorf("the response is %d bytes, over the limit %d", len(r.body), responseMax)
	}

	// A reason that would be huge is cut to the contract's 65536 characters.
	root2, dir2 := fixture(t)
	for i := 0; i < 3000; i++ {
		write(t, dir2, fmt.Sprintf("internal/%s/%s%05d.go", long, strings.Repeat("x", 90), i), nil)
	}
	base2 := serve(t, checkConfig(t, root2))
	st2 := step("tests", []string{"tests/**"}, []string{"tests"}, nil, nil)
	v := runCheck(t, base2, "ScopeKept", checkBody(t, dir2, st2, nil, "c", "e"))
	if v.Passed || len(v.Reason) > 65536 {
		t.Errorf("a failing reason must be at most 65536 characters: passed=%v, %d characters", v.Passed, len(v.Reason))
	}
}
