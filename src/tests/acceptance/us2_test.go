//go:build docker

// T046: user story 2 (spec scenarios US2.1 to US2.10 and the model-unavailable edge case) through
// the engine's HTTP API, with the step agents answering from the recorded model (research D19,
// D26, D34), and through the terminal client where the scenario is about the terminal (US2.9). The
// five planted failures (US2.2 to US2.6) have their 3-trial suite in planted_test.go, not here;
// US2.7 (attempts exhausted) is one trial. A missing Docker is a failure, never a skip.
//
// The host cannot dial the lab network, so the "generated engine client" of the story is the same
// requests the generated client sends, made through the lab's in-network client (lab.Do) and read
// into the contract's types; the terminal client is the real binary, run in a container on the lab
// network. Needs the recordings and platform pieces named in buildrig_test.go.
package acceptance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/tests/lab"
)

// happyLab is a lab whose step agents answer correctly (the set `happy`).
func happyLab(t *testing.T) (*lab.Lab, string) {
	t.Helper()
	return buildLab(t, happySet)
}

// cli runs the terminal client in a container on the lab network and returns its output and exit code.
func cli(t *testing.T, l *lab.Lab, bin string, args ...string) (string, int) {
	t.Helper()
	a := []string{"run", "--rm", "--network", l.Net, "--read-only", "--user", "65532:65532", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "-v", bin + ":/c:ro",
		"-e", "ENGINE_URL=" + l.BaseURL(), "-e", "ENGINE_TOKEN=" + lab.ClientToken, lab.Pin(t, "alpine"), "/c/cli"}
	return lab.Run("docker", append(a, args...)...)
}

// buildCLI builds the terminal client as the lab builds the engine: the pinned Go image, offline.
func buildCLI(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	os.Chmod(bin, 0o755)
	root := lab.Root(t)
	home, _ := os.UserCacheDir()
	cache := filepath.Join(home, "agentic-platform", "go")
	os.MkdirAll(cache, 0o755)
	out, err := exec.Command("docker", "run", "--rm", "--network", "none",
		"-v", filepath.Join(root, "src")+":/src", "-w", "/src/cli", "-v", cache+":/gopath", "-v", bin+":/out",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-e", "GOPATH=/gopath", "-e", "GOCACHE=/gopath/cache", "-e", "GOTOOLCHAIN=local", "-e", "GOPROXY=off",
		"-e", "GOFLAGS=-mod=readonly", "-e", "CGO_ENABLED=0", "-e", "GOOS=linux",
		lab.Pin(t, "golang"), "go", "build", "-trimpath", "-o", "/out/cli", "./cmd/cli").CombinedOutput()
	if err != nil {
		t.Fatalf("building the terminal client failed (is src/cli/api/gen generated: scripts/generate.sh?): %v: %s", err, out)
	}
	return bin
}

// TestUS2 follows one lab through the happy path: build (US2.1, Principle X), replay (US2.8),
// inspect and compare (US2.9). Subtests run in order and share the first run.
func TestUS2(t *testing.T) {
	l, slice := happyLab(t)
	var first Run
	var firstEvents []Event
	var replay Run
	var replayEvents []Event

	t.Run("US2.1 a build of an approved slice derives tests that fail before any code, and ends done only when they pass", func(t *testing.T) {
		begin := time.Now()
		op := startBuild(t, l, slice, "notes")
		if op.Kind != "run" || !strings.HasPrefix(op.Target, "RUN-") {
			t.Fatalf("the operation startRun returns is %+v, want kind run targeting the new run", op)
		}
		// Principle X: acknowledged only after it is recorded. The run and its first event answer at once.
		r := getRun(t, l, op.Target)
		if r.ID != op.Target || r.Slice != slice || r.Project != "notes" || r.Operation != op.ID || r.Pattern != "api-feature-go@1.1.0" {
			t.Errorf("getRun right after the 202 answers %+v, want the run recorded for %s, project notes, operation %s", r, slice, op.ID)
		}
		if evs := events(t, l, op.Target); len(evs) == 0 || evs[0].Kind != "RunStarted" {
			t.Errorf("listRunEvents right after the 202 answers %v, want RunStarted first", kindsOf(evs))
		}
		var list struct{ Items []Run }
		get(t, l, "/runs", &list)
		if len(list.Items) != 1 || list.Items[0].ID != op.Target {
			t.Errorf("listRuns right after the 202 = %+v, want the new run", list.Items)
		}
		if d := l.Get(t, slice); d.LatestRun != op.Target {
			t.Errorf("the slice's latest_run is %q, want %s", d.LatestRun, op.Target)
		}
		// One active run at a time (FR-021): a second build is refused while this one runs.
		if r.Status == "running" {
			refused(t, l, "POST", "/slices/"+slice+"/runs", map[string]string{"project": "notes-2"}, 409, "RunActive")
		}

		first = waitRun(t, l, op.Target, 30*time.Minute, stopped)
		l.StopStepWorkers(t, slice, "notes")
		firstEvents = events(t, l, first.ID)
		if first.Status != "ended" || first.Outcome != "done" || first.EndedAt == "" {
			t.Fatalf("the run is %+v (reason %q), want ended with outcome done; record %v", first, first.Reason, kindsOf(firstEvents))
		}
		if d := time.Since(begin); d > 30*time.Minute {
			t.Errorf("the build took %s, SC-007 allows 30 minutes", d)
		}
		wellFormedRecord(t, firstEvents)
		if o := l.Wait(t, op.ID); o.Status != "succeeded" || o.Result.Run != first.ID {
			t.Errorf("the run's operation is %+v, want succeeded with result.run %s", o, first.ID)
		}

		// Every step accepted, in the pattern's order, and the record ends with RunCompleted once.
		pos := map[string]int{}
		for _, s := range stepOrder {
			pos[s] = first2(firstEvents, func(e Event) bool { return e.Kind == "StepAccepted" && e.Step == s })
			if pos[s] < 0 {
				t.Errorf("step %s has no StepAccepted: record %v", s, kindsOf(firstEvents))
			}
		}
		for i := 1; i < len(stepOrder); i++ {
			if pos[stepOrder[i]] < pos[stepOrder[i-1]] {
				t.Errorf("step %s was accepted before %s", stepOrder[i], stepOrder[i-1])
			}
		}
		if n := count(firstEvents, "RunCompleted", ""); n != 1 || firstEvents[len(firstEvents)-1].Kind != "RunCompleted" {
			t.Errorf("the record holds %d RunCompleted and ends with %s, want one, last", n, firstEvents[len(firstEvents)-1].Kind)
		}

		// Tests derived from the examples exist, and failed against the stub before any code: the
		// tests step's TestsFailAgainstStub passed (the tests do fail there) before the code step's
		// first attempt, and every example is bound to a test.
		stub := first2(firstEvents, func(e Event) bool {
			return e.Kind == "CheckPassed" && e.Step == "tests" && e.Check != nil && e.Check.Name == "TestsFailAgainstStub"
		})
		codeStart := first2(firstEvents, func(e Event) bool { return e.Kind == "AttemptMade" && e.Step == "code" })
		if stub < 0 || codeStart < 0 || stub > codeStart {
			t.Errorf("TestsFailAgainstStub passed at event %d, the code step's first attempt is event %d: the tests must fail before any code", stub, codeStart)
		}
		if first2(firstEvents, func(e Event) bool {
			return e.Kind == "CheckPassed" && e.Step == "tests" && e.Check != nil && e.Check.Name == "EveryExampleBound"
		}) < 0 {
			t.Errorf("EveryExampleBound did not pass on the tests step")
		}
		pass := first2(firstEvents, func(e Event) bool {
			return e.Kind == "CheckPassed" && e.Step == "code" && e.Check != nil && e.Check.Name == "AllTestsPass"
		})
		if pass < 0 || pass < codeStart {
			t.Errorf("AllTestsPass did not pass after the code step's attempt (event %d, attempt %d)", pass, codeStart)
		}
		// Every accepted result is stored and readable, and its content hashes to its name (an empty
		// set of files is stored too, as e3b0c442...).
		for s, hs := range acceptedArtifacts(firstEvents) {
			for _, h := range hs {
				sum := sha256.Sum256(artifact(t, l, first.ID, h))
				if got := hex.EncodeToString(sum[:]); got != h {
					t.Errorf("artifact %s of step %s hashes to %s, not its name", h, s, got)
				}
			}
		}
		if l.Misses(t) != 0 {
			t.Errorf("the replay server saw requests no recording matched:\n%s", l.MissesJSON(t))
		}
	})

	t.Run("US2.8 replaying an ended run reuses every accepted result and gives byte-identical artifacts", func(t *testing.T) {
		if first.ID == "" {
			t.Fatal("US2.1 did not produce a finished run")
		}
		// The replay's project has its own step workers, which the engine needs registered although
		// no step is run again.
		l.StepWorkers(t, slice, "notes-replay")
		// A replay needs an ended run (RunNotEnded) and a new project (ProjectExists); US2.1 removed the step workers, so ProjectExists is only reached once the replay project's are registered.
		refused(t, l, "POST", "/runs/"+first.ID+"/replay", map[string]string{"project": "notes"}, 409, "ProjectExists")
		op := l.Accept(t, "POST", "/runs/"+first.ID+"/replay", map[string]string{"project": "notes-replay"})
		o := l.Wait(t, op)
		l.StopStepWorkers(t, slice, "notes-replay")
		if o.Status != "succeeded" || o.Result.Run == "" {
			t.Fatalf("replayRun: %+v (error %+v)", o, o.Error)
		}
		replay = getRun(t, l, o.Result.Run)
		replayEvents = events(t, l, replay.ID)
		if replay.ReplayOf != first.ID || replay.Slice != first.Slice || replay.Pattern != first.Pattern || replay.Status != "ended" || replay.Outcome != "done" {
			t.Errorf("the replay is %+v, want a done run of the same slice and pattern, replay_of %s", replay, first.ID)
		}
		if n := count(replayEvents, "AttemptMade", ""); n != 0 {
			t.Errorf("the replay made %d attempt(s); every step must reuse its accepted result", n)
		}
		a, b := acceptedArtifacts(firstEvents), acceptedArtifacts(replayEvents)
		for _, s := range stepOrder {
			if count(replayEvents, "ResultReused", s) != 1 {
				t.Errorf("step %s: %d ResultReused in the replay, want 1", s, count(replayEvents, "ResultReused", s))
			}
			if fmt.Sprint(a[s]) != fmt.Sprint(b[s]) || len(a[s]) == 0 {
				t.Errorf("step %s: artifacts %v in the run, %v in the replay, want the same hashes", s, a[s], b[s])
			}
			for _, h := range b[s] {
				if !bytes.Equal(artifact(t, l, first.ID, h), artifact(t, l, replay.ID, h)) {
					t.Errorf("artifact %s of step %s differs in content between the run and its replay", h, s)
				}
			}
		}
		wellFormedRecord(t, replayEvents)
		var cmp struct{ Items []json.RawMessage }
		get(t, l, "/runs/"+replay.ID+"/comparison?with="+first.ID, &cmp)
		if len(cmp.Items) != 0 {
			t.Errorf("a replay differs from its run in %d place(s): %s", len(cmp.Items), cmp.Items)
		}
	})

	t.Run("US2.9 two finished runs can be inspected and compared, in the terminal client and through the API", func(t *testing.T) {
		if first.ID == "" || replay.ID == "" {
			t.Fatal("US2.1 and US2.8 did not produce two finished runs")
		}
		// Through the API: every artifact, link, check result and decision, in order.
		for _, r := range []Run{first, replay} {
			evs := events(t, l, r.ID)
			wellFormedRecord(t, evs)
		}
		var cmp struct {
			Items []struct{ Step, Field string }
		}
		get(t, l, "/runs/"+first.ID+"/comparison?with="+first.ID, &cmp)
		if len(cmp.Items) != 0 {
			t.Errorf("a run differs from itself in %v", cmp.Items)
		}
		refused(t, l, "GET", "/runs/"+first.ID+"/comparison", nil, 400, "InvalidRequest")
		refused(t, l, "GET", "/runs/"+first.ID+"/comparison?with=RUN-999", nil, 404, "NotFound")

		// In the terminal.
		bin := buildCLI(t)
		out, code := cli(t, l, bin, "runs")
		if code != 0 || !strings.Contains(out, first.ID) || !strings.Contains(out, replay.ID) {
			t.Errorf("`runs` exit %d, output %q, want both runs listed", code, out)
		}
		out, code = cli(t, l, bin, "run", first.ID)
		if code != 0 {
			t.Errorf("`run %s` exit %d: %s", first.ID, code, out)
		}
		for _, s := range stepOrder {
			if !strings.Contains(out, s) {
				t.Errorf("`run %s` does not show step %s: %s", first.ID, s, out)
			}
		}
		for s, hs := range acceptedArtifacts(firstEvents) {
			for _, h := range hs {
				if !strings.Contains(out, h) {
					t.Errorf("`run %s` does not show artifact %s of step %s", first.ID, h, s)
				}
			}
		}
		for _, n := range []string{"TestsFailAgainstStub", "AllTestsPass", "R-1"} {
			if !strings.Contains(out, n) {
				t.Errorf("`run %s` shows no %s (check results with the rule or example they concern)", first.ID, n)
			}
		}
		out, code = cli(t, l, bin, "compare", first.ID, replay.ID)
		if code != 0 {
			t.Errorf("`compare %s %s` exit %d, want 0 for runs that do not differ: %s", first.ID, replay.ID, code, out)
		}
		out, code = cli(t, l, bin, "artifact", first.ID, firstEvents[first2(firstEvents, func(e Event) bool { return e.Kind == "StepAccepted" && e.Step == "tests" })].Artifacts[0])
		if code != 0 || out == "" {
			t.Errorf("`artifact` exit %d, output %q, want the artifact's content", code, out)
		}
	})

	if n := l.Misses(t); n != 0 {
		t.Errorf("the replay server saw %d request(s) no recording matched:\n%s", n, l.MissesJSON(t))
	}
}

func first2(evs []Event, pred func(Event) bool) int { return first(evs, pred) }

// One trial of a planted failure, as the story's scenario. TestPlanted runs the five planted
// failures, 3 trials each.
func usPlanted(t *testing.T, sc scenario) {
	l, slice := plantedLab(t, sc)
	plantedTrial(t, l, slice, sc, sc.name+"-1")
	if n := l.Misses(t); n != 0 {
		t.Errorf("%s: the replay server saw %d request(s) no recording matched:\n%s", sc.name, n, l.MissesJSON(t))
	}
}

func TestUS2_7_AStepThatKeepsFailingIsHandedOverAfterThreeAttempts(t *testing.T) {
	usPlanted(t, attemptsExhausted)
}

// US2.10: a run interrupted before it ended continues without repeating an accepted step. Its lab
// has its own step set, `resume`, whose answer to the code step's first request is held for 30 s.
// Once the model has received that request, the tests step is accepted and the code step is under
// way; the platform stops (l.RestartEngine kills the engine's container and starts it again), the
// run shows as interrupted, and resumeRun finishes it.
func TestUS2_10_AnInterruptedRunResumesWithoutRepeatingAcceptedSteps(t *testing.T) {
	l, slice := buildLab(t, resumeSet)
	op := startBuild(t, l, slice, "notes-resume")
	l.WaitModelRequest(t, op.Target, "code", 10*time.Minute)
	before := events(t, l, op.Target)
	if count(before, "StepAccepted", "tests") == 0 || count(before, "StepAccepted", "review") > 0 {
		t.Fatalf("the run was not between its tests step and its end when the platform was stopped: record %v", kindsOf(before))
	}
	accepted := map[string]int{}
	for _, s := range stepOrder {
		accepted[s] = count(before, "StepAccepted", s)
	}
	// RestartEngine returns when the engine answers again, with the workers registered again (in the
	// real platform, the launcher does); the run's record must have survived the stop.
	l.RestartEngine(t)
	var r Run
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		st, _, b := l.Do("GET", "/runs/"+op.Target, lab.ClientToken, nil, nil)
		if st == 200 && json.Unmarshal(b, &r) == nil {
			break
		}
		if st == 404 {
			t.Fatalf("after the engine's restart run %s is unknown (404): the run record did not survive (the lab keeps /store on a host folder)", op.Target)
		}
		time.Sleep(time.Second)
	}
	if evs := events(t, l, op.Target); count(evs, "RunInterrupted", "") == 0 {
		t.Errorf("the record after the restart has no RunInterrupted: %v", kindsOf(evs))
	}
	// After the restart the run continues by itself (feature 002, FR-005), so nobody resumes it.
	r = waitRun(t, l, op.Target, 30*time.Minute, ended)
	l.StopStepWorkers(t, slice, "notes-resume")
	evs := events(t, l, op.Target)
	wellFormedRecord(t, evs)
	if r.Outcome != "done" {
		t.Fatalf("the resumed run is %+v, want done; record %v", r, kindsOf(evs))
	}
	if count(evs, "RunResumed", "") != 1 {
		t.Errorf("the record holds %d RunResumed, want 1", count(evs, "RunResumed", ""))
	}
	for _, s := range stepOrder {
		if accepted[s] > 0 && (count(evs, "StepAccepted", s) != accepted[s] || count(evs, "AttemptMade", s) > accepted[s]) {
			t.Errorf("step %s was accepted before the stop and is repeated after it: %d accepted, %d attempts", s, count(evs, "StepAccepted", s), count(evs, "AttemptMade", s))
		}
	}
	// A run that is not interrupted cannot be resumed.
	refused(t, l, "POST", "/runs/"+op.Target+"/resume", nil, 409, "RunNotInterrupted")
	if n := l.Misses(t); n != 0 {
		t.Errorf("the replay server saw %d request(s) no recording matched:\n%s", n, l.MissesJSON(t))
	}
}

// Edge case: the model is unavailable (here the recorded model answers nothing for the step
// agents: 404, as `misses` lists). The step fails as not accepted, is retried within its attempts,
// and the run can be resumed later (FR-020), not lost.
func TestUS2_ModelUnavailableFailsTheStepAndKeepsTheRunResumable(t *testing.T) {
	// No lab.Steps: the replay server serves the analyst's recordings only, so no step agent has an
	// answer; the step workers still start and take the replay server as their model (StepModel).
	// This lab ends with misses, by design, so it does not check for none.
	l, slice := buildLab(t, "")
	op := startBuild(t, l, slice, "notes-nomodel")
	r := waitRun(t, l, op.Target, 10*time.Minute, stopped)
	evs := events(t, l, op.Target)
	wellFormedRecord(t, evs)
	if r.Status == "ended" && r.Outcome == "done" {
		t.Fatalf("the run is done although no step agent could reach a model")
	}
	if r.Status != "interrupted" && r.Status != "waiting-for-developer" {
		t.Errorf("the run is %q, want interrupted or waiting-for-developer (not lost, not ended)", r.Status)
	}
	if r.Reason == "" && r.Status == "waiting-for-developer" {
		t.Errorf("the run waits for the developer with no reason")
	}
	step := "tests" // the first agent step
	if n := count(evs, "StepAccepted", step); n != 0 {
		t.Errorf("step %s was accepted without a model", step)
	}
	if n := count(evs, "StepRejected", step); n < 1 || n > 3 {
		t.Errorf("step %s was rejected %d time(s), want 1 to 3: retried within its attempts", step, n)
	}
	for _, e := range evs {
		if e.Kind == "StepRejected" && (e.Attempt < 1 || e.Attempt > 3) {
			t.Errorf("a rejected attempt %d outside 1 to 3: %+v", e.Attempt, e)
		}
	}
	if l.Misses(t) == 0 {
		t.Errorf("the replay server lists no unanswered request: the step agent never asked for the model")
	}
	if r.Status == "interrupted" {
		st, _, b := l.Do("POST", "/runs/"+r.ID+"/resume", lab.ClientToken, nil, nil)
		if st != 202 {
			t.Errorf("resumeRun of the interrupted run = %d %s, want 202", st, b)
		}
	}
}

// What the run operations do before any worker exists: reads of nothing, and the refusals the
// contract lists. Needs only the lab, so it runs (and must pass) before T049, T050, T053a and T054c.
func TestUS2_RunOperationsAnswerAsTheContractSays(t *testing.T) {
	l := lab.Start(t)
	var list struct {
		Items []json.RawMessage `json:"items"`
		Next  string            `json:"next_page_token"`
	}
	get(t, l, "/runs", &list)
	if len(list.Items) != 0 || list.Items == nil {
		t.Errorf("listRuns of a platform with no runs = %v (nil %v), want items []", list.Items, list.Items == nil)
	}
	for _, p := range []string{"/runs/RUN-1", "/runs/RUN-1/events", "/runs/RUN-1/comparison?with=RUN-2"} {
		refused(t, l, "GET", p, nil, 404, "NotFound")
	}
	refused(t, l, "GET", "/runs/RUN-1/artifacts/"+strings.Repeat("0", 64), nil, 404, "NotFound")
	refused(t, l, "POST", "/runs/RUN-1/resume", nil, 404, "NotFound")
	refused(t, l, "POST", "/runs/RUN-1/replay", map[string]string{"project": "again"}, 404, "NotFound")
	refused(t, l, "POST", "/runs/RUN-1/hand-over", nil, 404, "NotFound")
	refused(t, l, "POST", "/operations/OP-999/cancel", nil, 404, "NotFound")
	refused(t, l, "POST", "/slices/S-9/runs", map[string]string{"project": "x"}, 404, "NotFound")

	id := l.Create(t, lab.Example)
	refused(t, l, "POST", "/slices/"+id+"/runs", map[string]any{}, 400, "InvalidRequest")
	refused(t, l, "POST", "/slices/"+id+"/runs", map[string]string{"project": "../x"}, 400, "InvalidProject")
	refused(t, l, "POST", "/slices/"+id+"/runs", map[string]string{"project": "Notes Project"}, 400, "InvalidProject")
	// The contract's order: NotApproved comes before WorkersNotReady (US1.4 holds for the run operation).
	refused(t, l, "POST", "/slices/"+id+"/runs", map[string]string{"project": "notes"}, 409, "NotApproved")
	// A refused build creates no run and no operation (Principle X: rejected by policy).
	get(t, l, "/runs", &list)
	if len(list.Items) != 0 {
		t.Errorf("listRuns after refused builds = %s, want no runs", list.Items)
	}
}
