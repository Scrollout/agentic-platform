// T075: failing tests, by a separate author, for the check service's witness rule (ADR 0039,
// research D35), over HTTP only, against the committed check contract
// (src/check/api/preview/2026-11-13-preview/openapi.yaml): runCheck's FindingsCarryEvidence and
// NoBlockingFindingOpen run only the witness test a blocking finding names, with coverage less a
// `-run '^$'` baseline, and EarlierStep's TestsUnchanged covers the witnesses.
//
// The review step is sent as the pattern will hold it (research D35, T076): writable
// [review/**], outputs [findings, witnesses], findings being its non-file output. The code step
// (the fix step) is the earlier step whose outputs hold `code`.
//
// Fixture: testdata/witness/ (the same module as engine/checks' testdata/witness/, no
// dependency): internal/greet/greet.go line 11 runs in init, 15 from a package-level initialiser,
// 21 only for an empty name, 23 for any other; review/r<n>/ holds the witnesses.
package checksvc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	witReviewWritable = []string{"review/**"}
	witReviewOutputs  = []string{"findings", "witnesses"}
)

func witEarlier(name string, writable, outputs []string) map[string]any {
	return map[string]any{"name": name, "writable": writable, "outputs": outputs}
}

// witEarlierOfReview are the pattern's steps before the review.
func witEarlierOfReview() []any {
	return []any{
		witEarlier("generate", genWritable, []string{"generated-code"}),
		witEarlier("tests", []string{"tests/**"}, []string{"tests"}),
		witEarlier("code", codeWritable, []string{"code"}),
	}
}

// witEarlierOfFix are the fix step's earlier steps: the review among them (T076).
func witEarlierOfFix() []any {
	return []any{
		witEarlier("generate", genWritable, []string{"generated-code"}),
		witEarlier("tests", []string{"tests/**"}, []string{"tests"}),
		witEarlier("review", witReviewWritable, witReviewOutputs),
	}
}

func witReviewStep() map[string]any {
	return step("review", witReviewWritable, witReviewOutputs, []string{"std"}, forbid)
}

// witBody is a runCheck body of the review step for the folder as it stands; before is the
// folder less review/ unless keepWitnesses (then every witness is in before, so none is new).
func witBody(t *testing.T, dir string, keepWitnesses bool, fs ...map[string]any) map[string]any {
	t.Helper()
	var before []pf
	for _, f := range listFiles(t, dir) {
		if keepWitnesses || !strings.HasPrefix(f.Path, "review/") {
			before = append(before, f)
		}
	}
	b := checkBody(t, dir, step("code", codeWritable, []string{"code"}, nil, nil), before, "c", reviewExamples(t))
	b["step"] = witReviewStep()
	b["outputs"] = []any{
		map[string]any{"name": "findings", "sha256": sum(nil)},
		map[string]any{"name": "witnesses", "sha256": sum(framed(stepFiles(t, dir, witReviewWritable)))},
	}
	b["earlier_steps"] = witEarlierOfReview()
	l := []any{}
	for _, f := range fs {
		l = append(l, f)
	}
	b["findings"] = l
	return b
}

func witFinding(rule, file string, line int, wfile, wtest string) map[string]any {
	f := finding(rule, file, line, true)
	if wfile != "" {
		f["witness"] = map[string]any{"file": wfile, "test": wtest}
	}
	return f
}

const (
	witGreet = "internal/greet/greet.go"
	witR1    = "review/r1/name_test.go"
	witT1    = "TestR1NameLeftOut"
)

func TestReviewFindingConfirmedByItsWitness(t *testing.T) {
	root, dir := fixture(t, "witness")
	base := serve(t, checkConfig(t, root))
	type fs = []map[string]any
	cases := []struct {
		name         string
		findings     fs
		keep         bool // every witness is in before
		carry, open  bool // FindingsCarryEvidence passes, NoBlockingFindingOpen passes
		rule, inOpen string
	}{
		{name: "a witness that fails having run the finding's line: blocks",
			findings: fs{witFinding("R-1", witGreet, 23, witR1, witT1)}, carry: true, open: false, rule: "R-1"},
		{name: "a witness that only prints the location: a suggestion",
			findings: fs{witFinding("R-2", witGreet, 23, "review/r2/print_test.go", "TestR2PrintsOnly")}, carry: true, open: true},
		{name: "a witness that fails before it runs the line: a suggestion",
			findings: fs{witFinding("R-3", witGreet, 23, "review/r3/first_test.go", "TestR3FailsFirst")}, carry: true, open: true},
		{name: "an always-failing witness naming a line init runs: a suggestion (baseline)",
			findings: fs{witFinding("R-4", witGreet, 11, "review/r4/init_test.go", "TestR4Always")}, carry: true, open: true},
		{name: "an always-failing witness naming a package-level initialiser's line: a suggestion (baseline)",
			findings: fs{witFinding("R-4", witGreet, 15, "review/r4/init_test.go", "TestR4Always")}, carry: true, open: true},
		{name: "an always-failing witness naming a line its TestMain runs: a suggestion (baseline)",
			findings: fs{witFinding("R-5", witGreet, 23, "review/r5/main_test.go", "TestR5Always")}, carry: true, open: true},
		{name: "a witness that fails having run only another line: a suggestion",
			findings: fs{witFinding("R-1", witGreet, 21, witR1, witT1)}, carry: true, open: true},
		{name: "a finding on gen/, which the code step cannot write: a suggestion",
			findings: fs{witFinding("R-1", "gen/gen.go", 5, witR1, witT1)}, carry: true, open: true},
		{name: "a finding on tests/, which the code step cannot write: a suggestion",
			findings: fs{witFinding("R-1", "tests/helpers.go", 5, witR1, witT1)}, carry: true, open: true},
		{name: "a witness that is not new (in before): a suggestion",
			findings: fs{witFinding("R-1", witGreet, 23, witR1, witT1)}, keep: true, carry: true, open: true},
		{name: "a witness in another rule's folder: a suggestion",
			findings: fs{witFinding("R-2", witGreet, 23, witR1, witT1)}, carry: true, open: true},
		{name: "a witness naming a test its file does not declare: a suggestion",
			findings: fs{witFinding("R-1", witGreet, 23, witR1, "TestR1Elsewhere")}, carry: true, open: true},
		{name: "a blocking finding without a witness, its rule's derived test failing there: a suggestion",
			findings: fs{witFinding("R-7", witGreet, 23, "", "")}, carry: true, open: true},
		{name: "a blocking finding without a witness: a suggestion",
			findings: fs{witFinding("R-1", witGreet, 23, "", "")}, carry: true, open: true},
		{name: "the named check is not consulted",
			findings: fs{func() map[string]any {
				f := witFinding("R-1", witGreet, 23, witR1, witT1)
				f["check"] = "NoSuchCheck"
				return f
			}()},
			carry: true, open: false, rule: "R-1"},
		{name: "only the confirmed finding is named",
			findings: fs{witFinding("R-2", witGreet, 23, "review/r2/print_test.go", "TestR2PrintsOnly"), witFinding("R-1", witGreet, 23, witR1, witT1)},
			carry:    true, open: false, rule: "R-1", inOpen: "R-2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			snap := snapshot(t, dir)
			carry := runCheck(t, base, "FindingsCarryEvidence", witBody(t, dir, c.keep, c.findings...))
			if carry.Passed != c.carry {
				t.Errorf("FindingsCarryEvidence passed=%v, want %v: %s", carry.Passed, c.carry, carry.Reason)
			}
			open := runCheck(t, base, "NoBlockingFindingOpen", witBody(t, dir, c.keep, c.findings...))
			if open.Passed != c.open {
				t.Errorf("NoBlockingFindingOpen passed=%v, want %v: %s", open.Passed, c.open, open.Reason)
			}
			if c.rule != "" && (!strings.Contains(open.Reason, c.rule) || !strings.Contains(open.Reason, witGreet)) {
				t.Errorf("the reason must name %s and %s: %s", c.rule, witGreet, open.Reason)
			}
			if c.inOpen != "" && strings.Contains(open.Reason, c.inOpen) {
				t.Errorf("the reason names %s, which is a suggestion: %s", c.inOpen, open.Reason)
			}
			noHost(t, carry.Reason+open.Reason, root)
			// The coverage profiles are written outside the project: the folder is as it was.
			if d := sameSnapshot(snap, snapshot(t, dir)); d != "" {
				t.Errorf("the witness run wrote the project folder: %s", d)
			}
		})
	}
}

// A witness that does not build cannot be judged: both checks answer passed false.
func TestReviewWitnessThatDoesNotBuildFails(t *testing.T) {
	root, dir := fixture(t, "witness")
	write(t, dir, "review/r9/broken_test.go", []byte("package r9\n\nimport \"testing\"\n\nfunc TestR9Broken(t *testing.T) { undefinedName() }\n"))
	base := serve(t, checkConfig(t, root))
	for _, check := range []string{"FindingsCarryEvidence", "NoBlockingFindingOpen"} {
		v := runCheck(t, base, check, witBody(t, dir, false, witFinding("R-9", witGreet, 23, "review/r9/broken_test.go", "TestR9Broken")))
		if v.Passed {
			t.Errorf("%s passed although the witness does not build: %s", check, v.Reason)
		}
		noHost(t, v.Reason, root)
	}
}

// The run is offline: with GOPROXY off and no module cache to read from (an empty GOMODCACHE),
// the fixture's witness still runs and confirms the finding, since the run fetches nothing.
func TestReviewWitnessRunIsOffline(t *testing.T) {
	root, dir := fixture(t, "witness")
	c := checkConfig(t, root)
	c.Env = append(append([]string{}, c.Env...), "GOMODCACHE="+t.TempDir(), "GOFLAGS=-mod=mod")
	base := serve(t, c)
	v := runCheck(t, base, "NoBlockingFindingOpen", witBody(t, dir, false, witFinding("R-1", witGreet, 23, witR1, witT1)))
	if v.Passed || !strings.Contains(v.Reason, "R-1") {
		t.Fatalf("offline, the witness that fails having run the line must still block: %s", v.Reason)
	}
}

// TestsUnchanged covers the witnesses in before: no step changes or removes one, the review step
// of a later round included; only the review step adds one.
func TestTestsUnchangedCoversWitnesses(t *testing.T) {
	const witness, added = witR1, "review/r8/new_test.go"
	newWitness := []byte("package r8\n\nimport \"testing\"\n\nfunc TestR8New(t *testing.T) {}\n")
	type edit func(t *testing.T, dir string)
	change := func(t *testing.T, dir string) { appendTo(t, dir, witness, "// edited\n") }
	remove := func(t *testing.T, dir string) {
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(witness))); err != nil {
			t.Fatal(err)
		}
	}
	add := func(t *testing.T, dir string) { write(t, dir, added, newWitness) }
	code := func(t *testing.T, dir string) { appendTo(t, dir, witGreet, "// fixed\n") }
	cases := []struct {
		name   string
		review bool // the judged step is the review (round 2), else the fix step
		edit   edit
		pass   bool
		names  string
	}{
		{"the review of a later round changes a witness", true, change, false, witness},
		{"the review of a later round removes a witness", true, remove, false, witness},
		{"the review of a later round adds one", true, add, true, ""},
		{"the fix step changes a witness", false, change, false, witness},
		{"the fix step removes a witness", false, remove, false, witness},
		{"the fix step adds one", false, add, false, added},
		{"the fix step changes only code", false, code, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, dir := fixture(t, "witness")
			base := serve(t, checkConfig(t, root))
			before := listFiles(t, dir)
			c.edit(t, dir)
			var b map[string]any
			if c.review {
				b = witBody(t, dir, true)
				b["before"] = before
				b["round"] = 2
			} else {
				st := step("code", codeWritable, []string{"code"}, nil, nil)
				b = checkBody(t, dir, st, before, "c", reviewExamples(t))
				b["earlier_steps"] = witEarlierOfFix()
			}
			v := runCheck(t, base, "TestsUnchanged", b)
			if v.Passed != c.pass {
				t.Fatalf("TestsUnchanged passed=%v, want %v: %s", v.Passed, c.pass, v.Reason)
			}
			if c.names != "" && !strings.Contains(v.Reason, c.names) {
				t.Errorf("the reason must name %s: %s", c.names, v.Reason)
			}
			noHost(t, v.Reason, root)
		})
	}
}
