//go:build docker

package lab

// T028 (feature 011, US1; spec FR-004, FR-005, FR-011; src/cli/contract.md, the launcher's `prove`
// paragraph, "End lines", "Run states and exit codes"; contracts/captured-patterns.md, `ap prove`):
// `ap prove <captured pattern> <project>` runs the client's `proof-slice`, then `build-workers`
// and `build` for the proof slice into <project>, and after the run's end line prints `Proof of
// <id>: passed` (exit 0) or `Proof of <id>: failed: ...` (exit 1, over the run state's own code).
//
// The lab does not run the launcher (it needs a Compose session). cpvProve does what the
// contract says the launcher does, with the real terminal client binary (src/cli/cmd/cli, run in
// a container on the lab network, as acceptance/us2_test.go does) and the lab's own stand-in for
// `ap build-workers` (l.StepWorkers): `proof-slice <id>` (its stdout is the proof slice's ID and a
// line feed, read here as the launcher reads it; its exit code ends the launcher), StepWorkers,
// `build <proof slice> <project>`. What the launcher script alone adds (the dispatch of the word
// `prove` in src/deploy/ap) is outside this file and needs a Compose session (reported to the
// owner with the task).
//
// One lab, one source build with the scripted step model, one capture; the cases run in order and
// share them: unknown captured pattern, passed proof, a second proof into another project, a failed
// proof (the run cancelled with `cancel`, reachable through the client and the API alone).
// The step set is a copy of `happy`: the scripted model writes the exchanges it answers into its
// set when the lab ends, and the committed set must not change.
//
// Fails today because `proof-slice` and `prove` do not exist: the client answers "unknown command"
// (exit 3), `POST /captured-patterns/{id}/proof-slice` is a stub, and no `Proof of` line is printed.
// Expected time: lab with images ~5 min, source build ~10 min, each proof 3 to 10 min; ~35 min.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// cpvRun is a run as far as this test reads it.
type cpvRun struct {
	ID      string `json:"id"`
	Slice   string `json:"slice"`
	Project string `json:"project"`
	Status  string `json:"status"`
	Outcome string `json:"outcome"`
	Proof   bool   `json:"proof"`
}

// cpvCaptured is a captured pattern as far as this test reads it (data model, Proof).
type cpvCaptured struct {
	ID         string `json:"id"`
	ProofSlice string `json:"proof_slice"`
	Proofs     []struct {
		Run      string            `json:"run"`
		Outcome  string            `json:"outcome"`
		Failures []json.RawMessage `json:"failures"`
	} `json:"proofs"`
}

func cpvGet(t testing.TB, l *Lab, path string, into any) int {
	t.Helper()
	st, _, b := l.Do("GET", path, ClientToken, nil, nil)
	if st == 200 {
		if err := json.Unmarshal(b, into); err != nil {
			t.Fatalf("GET %s: %v: %s", path, err, b)
		}
	}
	return st
}

func cpvRuns(t testing.TB, l *Lab) []cpvRun {
	t.Helper()
	var list struct {
		Items []cpvRun `json:"items"`
	}
	if st := cpvGet(t, l, "/runs", &list); st != 200 {
		t.Fatalf("listRuns = %d", st)
	}
	return list.Items
}

func cpvSliceCount(t testing.TB, l *Lab) int {
	t.Helper()
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if st := cpvGet(t, l, "/slices", &list); st != 200 {
		t.Fatalf("listSlices = %d", st)
	}
	return len(list.Items)
}

// cpvWaitEnded polls a run until it is not running, within limit.
func cpvWaitEnded(t testing.TB, l *Lab, id string, limit time.Duration) cpvRun {
	t.Helper()
	deadline := time.Now().Add(limit)
	var r cpvRun
	for time.Now().Before(deadline) {
		if st := cpvGet(t, l, "/runs/"+url.PathEscape(id), &r); st != 200 {
			t.Fatalf("getRun %s = %d", id, st)
		}
		if r.Status != "running" {
			return r
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("run %s did not stop within %s: %+v", id, limit, r)
	return r
}

// cpvBuildCLI builds the terminal client with the pinned Go image, offline (as acceptance's
// buildCLI does), into a folder the client container mounts.
func cpvBuildCLI(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	if err := os.Chmod(bin, 0o755); err != nil { //nolint:gosec // G302: the client container, another user, reads the folder
		t.Fatal(err)
	}
	cache := filepath.Join(cacheHome(), "agentic-platform", "go")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("docker", "run", "--rm", "--network", "none",
		"-v", filepath.Join(Root(t), "src")+":/src", "-w", "/src/cli", "-v", cache+":/gopath", "-v", bin+":/out",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-e", "GOPATH=/gopath", "-e", "GOCACHE=/gopath/cache", "-e", "GOTOOLCHAIN=local", "-e", "GOPROXY=off",
		"-e", "GOFLAGS=-mod=readonly", "-e", "CGO_ENABLED=0", "-e", "GOOS=linux",
		Pin(t, "golang"), "go", "build", "-trimpath", "-o", "/out/cli", "./cmd/cli").CombinedOutput()
	if err != nil {
		t.Fatalf("building the terminal client: %v: %s", err, out)
	}
	return bin
}

// cpvCmd is the terminal client run in a container on the lab network.
func cpvCmd(t testing.TB, l *Lab, bin string, args ...string) *exec.Cmd {
	a := []string{"run", "--rm", "--network", l.Net, "--read-only", "--user", "65532:65532", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "-v", bin + ":/c:ro",
		"-e", "ENGINE_URL=" + l.BaseURL(), "-e", "ENGINE_TOKEN=" + ClientToken, Pin(t, "alpine"), "/c/cli"}
	return exec.Command("docker", append(a, args...)...)
}

// cpvCLI runs the client to its end and returns standard output, standard error and the exit code.
func cpvCLI(t testing.TB, l *Lab, bin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := cpvCmd(t, l, bin, args...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	return so.String(), se.String(), cpvExit(t, err)
}

func cpvExit(t testing.TB, err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	t.Fatalf("docker: %v", err)
	return -1
}

var cpvSliceLine = regexp.MustCompile(`^S-[0-9]+\n$`)

// cpvProve is `ap prove <id> <project>` as the contract defines the launcher: proof-slice, then
// build-workers (l.StepWorkers), then build. during, when not nil, runs while `build` runs. It
// returns the proof slice (empty when proof-slice failed), build's standard output, and the exit
// code (proof-slice's own code when it failed). The step workers are removed when it returns.
func cpvProve(t *testing.T, l *Lab, bin, id, project string, during func()) (slice, out, errOut string, code int) {
	t.Helper()
	so, se, code := cpvCLI(t, l, bin, "proof-slice", id)
	if code != 0 {
		return "", so, se, code
	}
	if !cpvSliceLine.MatchString(so) {
		t.Fatalf("proof-slice %s printed %q on standard output, want the proof slice's ID and a line feed only", id, so)
	}
	slice = strings.TrimSpace(so)
	l.StepWorkers(t, slice, project)
	defer l.StopStepWorkers(t, slice, project)
	cmd := cpvCmd(t, l, bin, "build", slice, project)
	var ob, eb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &ob, &eb
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	if during != nil {
		wg.Add(1)
		go func() { defer wg.Done(); during() }()
	}
	err := cmd.Wait()
	wg.Wait()
	return slice, ob.String(), eb.String(), cpvExit(t, err)
}

// cpvLines is the non-empty lines of out.
func cpvLines(out string) []string {
	var ls []string
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			ls = append(ls, l)
		}
	}
	return ls
}

// cpvEndThenProof requires the proof outcome line after the run's end line, as the last line.
func cpvEndThenProof(t *testing.T, out, endPattern, proofPrefix string) (end, proof string) {
	t.Helper()
	ls := cpvLines(out)
	if len(ls) < 2 {
		t.Fatalf("output has %d lines, want an end line then a proof line:\n%s", len(ls), out)
	}
	end, proof = ls[len(ls)-2], ls[len(ls)-1]
	if !regexp.MustCompile(endPattern).MatchString(end) {
		t.Errorf("the line before the last is %q, want the run's end line matching %s:\n%s", end, endPattern, out)
	}
	if !strings.HasPrefix(proof, proofPrefix) {
		t.Errorf("the last line is %q, want it to start with %q (printed after the end line):\n%s", proof, proofPrefix, out)
	}
	return end, proof
}

// cpvTemplateFiles are the files the `happy` step set writes (answers.json), by project path, with
// the set's files holding their content.
var cpvTemplateFiles = map[string]string{
	"tests/features_test.go": "features_test.go.txt",
	"tests/handler_test.go":  "handler_test.go.txt",
	"internal/app/app.go":    "app.go.txt",
	"cmd/notes/main.go":      "main.go.txt",
}

func TestCapturedProveBuildsTheProofSliceAndSaysWhetherItPassed(t *testing.T) {
	// The scripted step model over a copy of `happy` (see the header).
	set := t.TempDir()
	happy := filepath.Join(Root(t), "src", "tests", "recorded", "steps", "happy")
	for _, c := range []struct{ from, to string }{{happy, set}, {filepath.Join(happy, "files"), filepath.Join(set, "files")}} {
		if err := copyFiles(c.from, c.to); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AP_LAB_MODEL", "scripted")
	bin := cpvBuildCLI(t)
	l := Start(t, Build(), Steps(set))

	// A done run to capture: the example slice built into `notes`, by the engine's API.
	slice := approvedExample(t, l)
	l.StepWorkers(t, slice, labProject)
	source := l.startRun(t, slice)
	if r := cpvWaitEnded(t, l, source, 30*time.Minute); r.Status != "ended" || r.Outcome != "done" {
		t.Fatalf("the source run is %+v, want ended done", r)
	}
	l.StopStepWorkers(t, slice, labProject)
	op := l.Wait(t, l.Accept(t, "POST", "/captured-patterns", map[string]string{"run": source, "name": "notes-api"}))
	if op.Status != "succeeded" || op.Target == "" {
		t.Fatalf("capturing %s: %+v (error %+v)", source, op, op.Error)
	}
	id := op.Target

	// FR-004, contract.md "prove": an unknown captured pattern is exit 4 (NotFound) and starts nothing.
	t.Run("an unknown captured pattern is exit 4 NotFound and starts nothing", func(t *testing.T) {
		slices, runs := cpvSliceCount(t, l), len(cpvRuns(t, l))
		got, out, errOut, code := cpvProve(t, l, bin, "no-such-pattern", "never", nil)
		if code != 4 || got != "" || !strings.Contains(out+errOut, "NotFound") {
			t.Errorf("prove of an unknown captured pattern: slice %q, exit %d, output %q %q; want no slice, exit 4 naming NotFound", got, code, out, errOut)
		}
		if n := cpvSliceCount(t, l); n != slices {
			t.Errorf("%d slices after, %d before: a proof slice was made for an unknown captured pattern", n, slices)
		}
		if n := len(cpvRuns(t, l)); n != runs {
			t.Errorf("%d runs after, %d before: a run was started", n, runs)
		}
		if _, err := os.Stat(filepath.Join(l.projects, "never")); err == nil {
			t.Error("a project folder `never` was made")
		}
	})

	// FR-004, FR-005, FR-011: a passed proof; the proof slice's run into <project>, the end line,
	// then `Proof of <id>: passed`, exit 0.
	var proofSlice string
	t.Run("a passed proof prints Proof passed after the end line and exits 0", func(t *testing.T) {
		got, out, errOut, code := cpvProve(t, l, bin, id, "proof-a", nil)
		proofSlice = got
		if code != 0 {
			t.Fatalf("prove exit %d, want 0; output:\n%s\n%s", code, out, errOut)
		}
		cpvEndThenProof(t, out, `^RUN-[0-9]+ ended done$`, "Proof of "+id+": passed")
		if got == slice {
			t.Errorf("the proof slice is the source slice %s", got)
		}
		var c cpvCaptured
		if st := cpvGet(t, l, "/captured-patterns/"+id, &c); st != 200 || c.ProofSlice != got || len(c.Proofs) != 1 || c.Proofs[0].Outcome != "passed" {
			t.Fatalf("the captured pattern is %d %+v, want proof_slice %s and one passed proof", st, c, got)
		}
		r := cpvWaitEnded(t, l, c.Proofs[0].Run, time.Minute)
		if !r.Proof || r.Slice != got || r.Project != "proof-a" || r.Outcome != "done" {
			t.Errorf("the proof's run is %+v, want a proof run of %s into proof-a, done", r, got)
		}
		// The project holds the filled files, equal to what the source run wrote.
		for rel, f := range cpvTemplateFiles {
			want, err := os.ReadFile(filepath.Join(happy, "files", f))
			if err != nil {
				t.Fatal(err)
			}
			if have, err := os.ReadFile(filepath.Join(l.projects, "proof-a", rel)); err != nil || !bytes.Equal(have, want) {
				t.Errorf("proof-a/%s: %v; differs from the source run's file: %v", rel, err, err == nil)
			}
		}
	})

	// Contract.md: running it again into another project builds again (a new proof, a new run).
	t.Run("running it again into another project builds again", func(t *testing.T) {
		got, out, errOut, code := cpvProve(t, l, bin, id, "proof-b", nil)
		if code != 0 {
			t.Fatalf("prove exit %d, want 0; output:\n%s\n%s", code, out, errOut)
		}
		cpvEndThenProof(t, out, `^RUN-[0-9]+ ended done$`, "Proof of "+id+": passed")
		if got != proofSlice {
			t.Errorf("the second proof used slice %s, want the same proof slice %s", got, proofSlice)
		}
		var c cpvCaptured
		cpvGet(t, l, "/captured-patterns/"+id, &c)
		if len(c.Proofs) != 2 || c.Proofs[1].Outcome != "passed" || c.Proofs[1].Run == c.Proofs[0].Run {
			t.Fatalf("proofs are %+v, want two passed proofs of two different runs", c.Proofs)
		}
		if r := cpvWaitEnded(t, l, c.Proofs[1].Run, time.Minute); r.Project != "proof-b" || !r.Proof {
			t.Errorf("the second proof's run is %+v, want a proof run into proof-b", r)
		}
		for rel := range cpvTemplateFiles {
			if _, err := os.Stat(filepath.Join(l.projects, "proof-b", rel)); err != nil {
				t.Errorf("proof-b/%s: %v", rel, err)
			}
		}
	})

	// A failed proof exits 1 over the run state's own code and says why. The failure made here is
	// a cancelled run (`cancel RUN-n` while it runs): reachable through the client alone. Handed
	// over, expired and differing files are not made here (see the report).
	t.Run("a failed proof prints Proof failed after the end line and exits 1", func(t *testing.T) {
		cancelled := ""
		_, out, errOut, code := cpvProve(t, l, bin, id, "proof-c", func() {
			deadline := time.Now().Add(5 * time.Minute)
			for time.Now().Before(deadline) {
				for _, r := range cpvRuns(t, l) {
					if r.Proof && r.Project == "proof-c" {
						cancelled = r.ID
						so, se, c := cpvCLI(t, l, bin, "cancel", r.ID)
						t.Logf("cancel %s: exit %d %s%s", r.ID, c, so, se)
						return
					}
				}
				time.Sleep(100 * time.Millisecond)
			}
		})
		if cancelled == "" {
			t.Fatal("the proof's run never appeared")
		}
		if code != 1 {
			t.Errorf("prove of a cancelled proof exit %d, want 1 (not the cancelled state's 6); output:\n%s\n%s", code, out, errOut)
		}
		_, proof := cpvEndThenProof(t, out, `^`+cancelled+` ended cancelled`, "Proof of "+id+": failed: ")
		if proof == "Proof of "+id+": failed: " {
			t.Errorf("the failed line names no failure: %q", proof)
		}
		var c cpvCaptured
		cpvGet(t, l, "/captured-patterns/"+id, &c)
		if n := len(c.Proofs); n != 3 || c.Proofs[2].Run != cancelled || c.Proofs[2].Outcome != "failed" || len(c.Proofs[2].Failures) == 0 {
			t.Errorf("proofs are %+v, want a third, failed proof of %s with its failure", c.Proofs, cancelled)
		}
	})
}
