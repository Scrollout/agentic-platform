//go:build docker

// T014 (feature 002, US1, SC-001, FR-007, FR-016, ADR 0070): a run survives a stop of the engine.
// For each of four stop points (an agent step, a procedure step, a review round, a version-history
// commit) the lab's engine container is killed at that point and started again (l.RestartEngine);
// nobody calls resumeRun. Each of 3 trials has its own lab (a new result store, so the run is RUN-1
// and nothing is reused from an earlier trial) over the `resume` step set, whose recordings name the
// project `notes-resume`. The run must end `done` by itself, with every step accepted once and the
// artifacts of an uninterrupted run of the same slice (the baseline: the same set, its own lab,
// no stop). The continued run is then replayed (US2.8's way): no attempt, no model call, the same
// bytes. After the restart the namespace `ap` has 24 h retention, and no row of the lab's
// `durable-store` holds text of the recordings' prompts, completions or findings.
//
// How each stop point is reached (the run's events through the API, never a sleep; a call that
// must hang is held by pausing the container it goes to, which makes the stop exact). A kill on
// an event alone can land while the run's workflow is between two activities; then no activity is
// lost, nothing times out, and the run correctly records no stop (FR-006's limit). So for the
// three stop points that hang on an activity, the test also waits until the durable service shows
// that activity (stopPoint.hangs) as STARTED in the run's workflow, and only then kills the engine:
//   - agent step: the `resume` set holds the code step's first model answer for 30 s;
//     l.WaitModelRequest returns once the model has the request, so the engine is killed inside the
//     step worker's call, the model call in flight.
//   - procedure step: the generate step's container is paused before the run starts; the engine
//     records StepStarted for generate, then its call hangs; the engine is killed once the
//     workflow's CallWorker activity is STARTED.
//   - review round: the same with the review step's container; killed once StepStarted for review is
//     recorded and its CallWorker activity is STARTED.
//   - commit: the code step's answer is held as for the agent step; once the model has the request
//     the project worker (the commit's service) is paused. The step is accepted by the checks, which
//     the project worker does not serve, and the commit of an accepted step is made before the next
//     step starts (machine/drive.go flush), so the engine hangs in the code step's commit; it is
//     killed once StepAccepted for code is recorded, with no StepStarted for review yet, and the
//     workflow's CommitStep activity is STARTED.
//
// After the restart a paused step container is restarted (its stale, never-answered request would
// otherwise run beside the continued attempt), and the paused project worker is resumed (its stale
// commit is repeat-safe by its stamp, research R3). A missing Docker is a failure (lab.Start).
package acceptance

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/tests/lab"
)

// The project the `resume` set's recordings name (their requests say /projects/notes-resume).
const stopProject = "notes-resume"

// The details of the two events a stop of the platform records (data model, Run record).
const (
	stopInterruptedDetail = "the platform stopped"
	stopResumedDetail     = "continued after the platform stopped"
)

const stopTrials = 3 // SC-001

// baseline is what an uninterrupted run of the slice gives.
type baseline struct {
	outcome   string
	artifacts map[string][]string // step -> accepted artifact hashes
	content   map[string][]byte   // artifact hash -> bytes
}

// stopPoint is one place the engine is killed at.
type stopPoint struct {
	name string
	// pauseStep is the step whose container is paused before the run starts, "" for none.
	pauseStep string
	// reach waits until the engine is at the stop point (pausing the project worker where that is
	// the stop) and returns the run record to judge the moment by.
	reach func(t *testing.T, l *lab.Lab, slice, run string) []Event
	// inside returns why the record is not that of the stop point, or "".
	inside func(evs []Event) string
	// hangs is the type of the activity the stop point hangs on ("" for none): the kill waits until
	// the run's workflow shows it as a STARTED pending activity.
	hangs string
	// pausedProjectWorker is whether reach paused the lab's project worker.
	pausedProjectWorker bool
}

func spStarted(step string) func(evs []Event) bool {
	return func(evs []Event) bool { return count(evs, "StepStarted", step) > 0 }
}

var stopPoints = []stopPoint{
	{
		name: "agent-step",
		reach: func(t *testing.T, l *lab.Lab, slice, run string) []Event {
			l.WaitModelRequest(t, run, "code", 10*time.Minute)
			return events(t, l, run)
		},
		inside: func(evs []Event) string {
			if count(evs, "StepAccepted", "tests") == 0 || count(evs, "StepAccepted", "code") > 0 {
				return "the code step is not the one under way"
			}
			return ""
		},
	},
	{
		name:      "procedure-step",
		pauseStep: "generate",
		hangs:     "CallWorker",
		reach: func(t *testing.T, l *lab.Lab, slice, run string) []Event {
			return spWaitEvents(t, l, run, 10*time.Minute, "StepStarted for generate", spStarted("generate"))
		},
		inside: func(evs []Event) string {
			if count(evs, "StepAccepted", "contract-check") == 0 || count(evs, "StepAccepted", "generate") > 0 {
				return "the generate step is not the one under way"
			}
			return ""
		},
	},
	{
		name:      "review-round",
		pauseStep: "review",
		hangs:     "CallWorker",
		reach: func(t *testing.T, l *lab.Lab, slice, run string) []Event {
			return spWaitEvents(t, l, run, 10*time.Minute, "StepStarted for review", spStarted("review"))
		},
		inside: func(evs []Event) string {
			if count(evs, "StepAccepted", "code") == 0 || count(evs, "StepAccepted", "review") > 0 {
				return "the review round is not the one under way"
			}
			return ""
		},
	},
	{
		name:                "version-history-commit",
		hangs:               "CommitStep",
		pausedProjectWorker: true,
		reach: func(t *testing.T, l *lab.Lab, slice, run string) []Event {
			// The restore of the code step's attempt is done once the model has its request, so
			// pausing the project worker now leaves only the commit of the accepted step to hang.
			l.WaitModelRequest(t, run, "code", 10*time.Minute)
			lab.Docker(t, "pause", l.ProjectWorker)
			t.Cleanup(func() { lab.Run("docker", "unpause", l.ProjectWorker) })
			return spWaitEvents(t, l, run, 10*time.Minute,
				"StepAccepted for code (its commit, made before the next step starts, then hangs: the project worker is paused; "+
					"if the run workflow commits before it records this event, the stop point needs another observation)",
				func(evs []Event) bool { return count(evs, "StepAccepted", "code") > 0 })
		},
		inside: func(evs []Event) string {
			if count(evs, "StepStarted", "review") > 0 {
				return "the review step has started, so the code step's commit was made"
			}
			return ""
		},
	},
}

// TestStopPoints is SC-001: for each stop point, 3 trials of a run stopped there and continued.
func TestStopPoints(t *testing.T) {
	var base baseline
	t.Run("uninterrupted", func(t *testing.T) {
		l, slice := buildLab(t, resumeSet)
		op := startBuild(t, l, slice, stopProject)
		r := waitRun(t, l, op.Target, 30*time.Minute, ended)
		l.StopStepWorkers(t, slice, stopProject)
		evs := events(t, l, op.Target)
		wellFormedRecord(t, evs)
		if r.Outcome != "done" {
			t.Fatalf("the uninterrupted run is %+v, want done; record %v", r, kindsOf(evs))
		}
		b := baseline{artifacts: acceptedArtifacts(evs), content: map[string][]byte{}}
		for _, s := range stepOrder {
			if len(b.artifacts[s]) == 0 {
				t.Fatalf("the uninterrupted run accepted no artifact for step %s: %v", s, kindsOf(evs))
			}
			for _, h := range b.artifacts[s] {
				b.content[h] = artifact(t, l, op.Target, h)
			}
		}
		b.outcome = r.Outcome
		base = b
	})
	for _, sp := range stopPoints {
		sp := sp
		for trial := 1; trial <= stopTrials; trial++ {
			t.Run(fmt.Sprintf("%s/trial-%d", sp.name, trial), func(t *testing.T) {
				if base.outcome == "" {
					t.Fatal("the uninterrupted run did not end done, so there is nothing to compare with")
				}
				stopTrial(t, sp, base)
			})
		}
	}
}

// stopTrial runs one trial in its own lab.
func stopTrial(t *testing.T, sp stopPoint, base baseline) {
	l, slice := buildLab(t, resumeSet)
	l.StepWorkers(t, slice, stopProject)
	var paused string
	if sp.pauseStep != "" {
		paused = spWorker(t, slice, sp.pauseStep)
		lab.Docker(t, "pause", paused)
	}
	op := startRun(t, l, slice, stopProject)
	run := op.Target

	at := sp.reach(t, l, slice, run)
	if why := sp.inside(at); why != "" {
		t.Fatalf("%s: the engine would not be stopped at the stop point: %s; record %v", sp.name, why, kindsOf(at))
	}
	if sp.hangs != "" {
		spWaitActivityStarted(t, l, run, sp.hangs, 2*time.Minute)
	}
	l.RestartEngine(t) // kills the engine's container and starts it again; nobody calls resumeRun
	if paused != "" {
		lab.Docker(t, "unpause", paused) // runsc cannot signal a paused container
		lab.Docker(t, "restart", "-t", "0", paused)
	}
	if sp.pausedProjectWorker {
		lab.Docker(t, "unpause", l.ProjectWorker)
	}

	r := spAwaitEnded(t, l, run, 15*time.Minute)
	evs := events(t, l, run)
	wellFormedRecord(t, evs)
	if r.Outcome != base.outcome {
		t.Fatalf("%s: the continued run is %+v, want the outcome %s of the uninterrupted run; record %v", sp.name, r, base.outcome, kindsOf(evs))
	}

	// The stop was seen by the run, and the platform continued it by itself.
	interrupted, resumed := count(evs, "RunInterrupted", ""), count(evs, "RunResumed", "")
	if interrupted == 0 {
		t.Errorf("%s: the record has no RunInterrupted, so the run was not stopped at the stop point: %v", sp.name, kindsOf(evs))
	}
	if interrupted != resumed {
		t.Errorf("%s: %d RunInterrupted and %d RunResumed, want one pair per stop", sp.name, interrupted, resumed)
	}
	for _, e := range evs {
		switch {
		case e.Kind == "RunInterrupted" && e.Detail != stopInterruptedDetail:
			t.Errorf("%s: RunInterrupted detail %q, want %q", sp.name, e.Detail, stopInterruptedDetail)
		case e.Kind == "RunResumed" && e.Detail != stopResumedDetail:
			t.Errorf("%s: RunResumed detail %q, want %q (a developer's resume has none; nobody resumed)", sp.name, e.Detail, stopResumedDetail)
		}
	}

	// No step accepted twice, and a stop does not count as a failed attempt (FR-003).
	for _, s := range stepOrder {
		if n := count(evs, "StepAccepted", s); n != 1 {
			t.Errorf("%s: step %s was accepted %d time(s), want once", sp.name, s, n)
		}
		if n := count(evs, "ResultReused", s) + count(evs, "StepRejected", s); n != 0 {
			t.Errorf("%s: step %s has %d reuse(s) or rejection(s), want none", sp.name, s, n)
		}
	}

	// The artifacts are the uninterrupted run's, byte for byte.
	got := acceptedArtifacts(evs)
	for _, s := range stepOrder {
		if strings.Join(got[s], ",") != strings.Join(base.artifacts[s], ",") {
			t.Errorf("%s: step %s artifacts %v, uninterrupted run %v", sp.name, s, got[s], base.artifacts[s])
			continue
		}
		for _, h := range got[s] {
			if !bytes.Equal(artifact(t, l, run, h), base.content[h]) {
				t.Errorf("%s: artifact %s of step %s differs in content from the uninterrupted run's", sp.name, h, s)
			}
		}
	}

	// FR-007, US1 AC6: the continued run, replayed, gives the same bytes without a model call.
	l.StepWorkers(t, slice, stopProject+"-replay")
	rop := l.Wait(t, l.Accept(t, "POST", "/runs/"+run+"/replay", map[string]string{"project": stopProject + "-replay"}))
	l.StopStepWorkers(t, slice, stopProject+"-replay")
	if rop.Status != "succeeded" {
		t.Fatalf("%s: replayRun: %+v (error %+v)", sp.name, rop, rop.Error)
	}
	rr := getRun(t, l, rop.Result.Run)
	revs := events(t, l, rr.ID)
	wellFormedRecord(t, revs)
	if rr.ReplayOf != run || rr.Status != "ended" || rr.Outcome != "done" {
		t.Errorf("%s: the replay is %+v, want a done run, replay_of %s", sp.name, rr, run)
	}
	if n := count(revs, "AttemptMade", ""); n != 0 {
		t.Errorf("%s: the replay made %d attempt(s), so it called a model or a step worker; every step must reuse its result", sp.name, n)
	}
	rgot := acceptedArtifacts(revs)
	for _, s := range stepOrder {
		if count(revs, "ResultReused", s) != 1 {
			t.Errorf("%s: step %s: %d ResultReused in the replay, want 1", sp.name, s, count(revs, "ResultReused", s))
		}
		if strings.Join(rgot[s], ",") != strings.Join(got[s], ",") {
			t.Errorf("%s: step %s artifacts %v in the replay, %v in the run", sp.name, s, rgot[s], got[s])
			continue
		}
		for _, h := range rgot[s] {
			if !bytes.Equal(artifact(t, l, rr.ID, h), artifact(t, l, run, h)) {
				t.Errorf("%s: artifact %s of step %s differs between the run and its replay", sp.name, h, s)
			}
		}
	}
	var cmp struct{ Items []json.RawMessage }
	get(t, l, "/runs/"+rr.ID+"/comparison?with="+run, &cmp)
	if len(cmp.Items) != 0 {
		t.Errorf("%s: the replay differs from the continued run in %d place(s): %s", sp.name, len(cmp.Items), cmp.Items)
	}
	if n := l.Misses(t); n != 0 {
		t.Errorf("%s: the replay server saw %d request(s) no recording matched:\n%s", sp.name, n, l.MissesJSON(t))
	}

	// ADR 0070: the namespace exists after the restart, with 24 h retention.
	spNamespace(t, l)
	// FR-016: nothing of the recordings' text is in the durable store.
	spStoreHoldsNoText(t, l)
}

// spAwaitEnded polls a run until it has ended, tolerating the engine not answering yet; it never
// calls resumeRun. A run that stays interrupted or waiting fails with its last state.
func spAwaitEnded(t *testing.T, l *lab.Lab, id string, limit time.Duration) Run {
	t.Helper()
	deadline := time.Now().Add(limit)
	var r Run
	last := ""
	for time.Now().Before(deadline) {
		st, _, b := l.Do("GET", "/runs/"+id, lab.ClientToken, nil, nil)
		if st == 200 && json.Unmarshal(b, &r) == nil {
			if r.Status == "ended" {
				return r
			}
			last = fmt.Sprintf("%+v", r)
		} else {
			last = fmt.Sprintf("GET /runs/%s = %d", id, st)
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("run %s did not end by itself within %s after the engine was started again; last: %s", id, limit, last)
	return r
}

// spWaitEvents polls the run record until pred holds, and fails when the run ends first or the
// limit passes.
func spWaitEvents(t *testing.T, l *lab.Lab, run string, limit time.Duration, what string, pred func([]Event) bool) []Event {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		evs := events(t, l, run)
		if pred(evs) {
			return evs
		}
		if getRun(t, l, run).Status == "ended" {
			t.Fatalf("run %s ended before the record showed %s: %v", run, what, kindsOf(evs))
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("run %s: no %s within %s", run, what, limit)
	return nil
}

// spWorker is the name of the lab's step container for a step of the slice (lab.StepWorkers names
// it aptest-lab-<pid>-worker-<slice>-<step>-<project hash>).
func spWorker(t *testing.T, slice, step string) string {
	t.Helper()
	out, code := lab.Run("docker", "ps", "--filter", fmt.Sprintf("name=aptest-lab-%d-worker-%s-%s-", os.Getpid(), strings.ToLower(slice), step),
		"--format", "{{.Names}}")
	names := strings.Fields(out)
	if code != 0 || len(names) != 1 {
		t.Fatalf("the lab has %d step container(s) for step %s (exit %d): %s", len(names), step, code, out)
	}
	return names[0]
}

// ---- the durable services -------------------------------------------------------------------------

// spDurable finds the lab's durable network, server and store from the engine's networks.
func spDurable(t *testing.T, l *lab.Lab) (network, server, store string) {
	t.Helper()
	list := func(args ...string) []string {
		out, code := lab.Run("docker", args...)
		if code != 0 {
			t.Fatalf("docker %s: %s", strings.Join(args, " "), out)
		}
		return strings.Fields(out)
	}
	for _, n := range list("inspect", "-f", `{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}`, l.Engine) {
		if strings.HasPrefix(n, "aptest-lab-durable-net-") {
			network = n
		}
	}
	if network == "" {
		t.Fatalf("the engine is on no durable network")
	}
	for _, c := range list("network", "inspect", "-f", `{{range .Containers}}{{.Name}} {{end}}`, network) {
		if c != l.Engine {
			server = c
		}
	}
	if server == "" {
		t.Fatalf("no durable server on network %s", network)
	}
	for _, n := range list("inspect", "-f", `{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}`, server) {
		if n == network {
			continue
		}
		for _, c := range list("network", "inspect", "-f", `{{range .Containers}}{{.Name}} {{end}}`, n) {
			if strings.HasPrefix(c, "aptest-lab-durable-store-") {
				store = c
			}
		}
	}
	if store == "" {
		t.Fatalf("no durable-store next to the server %s", server)
	}
	return
}

// spWaitActivityStarted polls the durable service until the workflow of the run (its ID is the run
// ID, in the namespace ap) has a pending activity of the given type in the state STARTED, that is,
// the engine's worker has taken the call and it is hanging, so a kill now loses it. A describe that
// fails or whose output is not JSON is tried again (the service may be busy); at the limit the test
// fails with the last output. SCHEDULED does not count: the worker has not taken the call yet.
func spWaitActivityStarted(t *testing.T, l *lab.Lab, run, activity string, limit time.Duration) {
	t.Helper()
	network, _, _ := spDurable(t, l)
	deadline := time.Now().Add(limit)
	last := ""
	for {
		out, code := lab.Run("docker", "run", "--rm", "--network", network, "--read-only", "--cap-drop", "ALL",
			"--security-opt", "no-new-privileges", "--user", "1000:1000", "--tmpfs", "/tmp:mode=1777,size=16m", "-e", "HOME=/tmp",
			"--memory", "128m", "--pids-limit", "64", "--entrypoint", "temporal", lab.Pin(t, "temporal-admin-tools"),
			"workflow", "describe", "--address", "durable:7233", "--namespace", "ap", "--workflow-id", run, "--output", "json")
		last = fmt.Sprintf("exit %d: %.800s", code, out)
		if code == 0 {
			if start := strings.Index(out, "{"); start >= 0 {
				var doc any
				if json.Unmarshal([]byte(out[start:]), &doc) == nil && spActivityStarted(doc, activity) {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s: no pending activity %s in state STARTED within %s; last describe: %s", run, activity, limit, last)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// spActivityStarted reports whether the described workflow has a pending activity of the type in
// the state STARTED. The CLI's JSON names the type as an object {"name": ...} or as a string, and
// the state as PENDING_ACTIVITY_STATE_STARTED or Started; both forms are accepted.
func spActivityStarted(doc any, activity string) bool {
	pending, _ := spFind(doc, "pendingActivities")
	list, _ := pending.([]any)
	for _, p := range list {
		m, _ := p.(map[string]any)
		name := ""
		switch ty := m["activityType"].(type) {
		case string:
			name = ty
		case map[string]any:
			name, _ = ty["name"].(string)
		}
		state, _ := m["state"].(string)
		if name == activity && strings.HasSuffix(strings.ToUpper(state), "STARTED") {
			return true
		}
	}
	return false
}

// spNamespace requires the namespace `ap` to exist with 24 h retention, described by the pinned
// admin tools' CLI on the durable network (ADR 0070).
func spNamespace(t *testing.T, l *lab.Lab) {
	t.Helper()
	network, _, _ := spDurable(t, l)
	out, code := lab.Run("docker", "run", "--rm", "--network", network, "--read-only", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "--user", "1000:1000", "--tmpfs", "/tmp:mode=1777,size=16m", "-e", "HOME=/tmp",
		"--memory", "128m", "--pids-limit", "64", "--entrypoint", "temporal", lab.Pin(t, "temporal-admin-tools"),
		"operator", "namespace", "describe", "--address", "durable:7233", "--namespace", "ap", "--output", "json")
	if code != 0 {
		t.Errorf("the namespace ap cannot be described (exit %d): %s", code, out)
		return
	}
	start := strings.Index(out, "{")
	if start < 0 {
		t.Errorf("the namespace description is not JSON: %.300s", out)
		return
	}
	var doc any
	if err := json.Unmarshal([]byte(out[start:]), &doc); err != nil {
		t.Errorf("the namespace description is not JSON: %v: %.300s", err, out)
		return
	}
	ttl, ok := spFind(doc, "workflowExecutionRetentionTtl")
	if !ok {
		t.Errorf("the namespace description names no workflowExecutionRetentionTtl: %.600s", out)
		return
	}
	var d time.Duration
	switch v := ttl.(type) {
	case string:
		p, err := time.ParseDuration(v)
		if err != nil {
			t.Errorf("retention %q is not a duration", v)
			return
		}
		d = p
	case float64:
		d = time.Duration(v * float64(time.Second))
	}
	if d != 24*time.Hour {
		t.Errorf("namespace ap has retention %v (%v), want 24h", ttl, d)
	}
}

func spFind(v any, key string) (any, bool) {
	switch x := v.(type) {
	case map[string]any:
		if r, ok := x[key]; ok {
			return r, true
		}
		for _, c := range x {
			if r, ok := spFind(c, key); ok {
				return r, true
			}
		}
	case []any:
		for _, c := range x {
			if r, ok := spFind(c, key); ok {
				return r, true
			}
		}
	}
	return nil, false
}

var (
	spHex   = regexp.MustCompile(`\\\\x([0-9a-fA-F]{8,})`)
	spHash  = regexp.MustCompile(`[0-9a-f]{64}`)
	spNames = regexp.MustCompile(`RUN-[0-9]+`)
)

// spStoreHoldsNoText dumps every row of the lab's durable-store (both databases) and requires that
// no line of a recording's prompt, completion or finding text is in it, in the text or in a byte
// column (pg_dump writes bytea as \\x<hex>). The check reports what it examined, finds a line
// planted in the dump, and requires the dump to hold the run (so it did read workflow data).
func spStoreHoldsNoText(t *testing.T, l *lab.Lab) {
	t.Helper()
	_, _, store := spDurable(t, l)
	var blob []byte
	for _, db := range []string{"temporal", "temporal_visibility"} {
		out, code := lab.Run("docker", "exec", store, "pg_dump", "-U", "temporal", "-d", db, "--data-only")
		if code != 0 {
			t.Fatalf("pg_dump of %s in %s: exit %d: %.300s", db, store, code, out)
		}
		blob = append(blob, out...)
		blob = append(blob, '\n')
		for _, m := range spHex.FindAllSubmatch([]byte(out), -1) {
			if b, err := hex.DecodeString(string(m[1])); err == nil {
				blob = append(blob, b...)
				blob = append(blob, '\n')
			}
		}
	}
	needles := spNeedles(t)
	t.Logf("durable-store: %d bytes examined (text and decoded byte columns) for %d lines of recorded text", len(blob), len(needles))
	if !spNames.Match(blob) {
		t.Errorf("the dump of durable-store holds no run ID, so it did not examine the workflow's rows")
	}
	if len(spFound(append([]byte("planted: "+needles[0]+"\n"), blob...), needles[:1])) == 0 {
		t.Fatalf("the search does not find a planted line of recorded text")
	}
	if found := spFound(blob, needles); len(found) > 0 {
		t.Errorf("durable-store holds %d line(s) of recorded prompt, completion or finding text (FR-016), for example: %q", len(found), found[0])
	}
}

func spFound(blob []byte, needles []string) []string {
	var f []string
	for _, n := range needles {
		if bytes.Contains(blob, []byte(n)) {
			f = append(f, n)
		}
	}
	return f
}

// spNeedles are the lines (at least 30 characters, none holding an artifact hash, which a workflow
// legitimately carries) of the `resume` set's recorded text: the prompts of its requests, the
// files and texts of its answers, and the text of any finding.
func spNeedles(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join(lab.Root(t), "src", "tests", "recorded", "steps", "resume")
	set := map[string]bool{}
	addLines := func(s string) {
		for _, line := range strings.Split(s, "\n") {
			if line = strings.TrimSpace(line); len(line) >= 30 && !spHash.MatchString(line) {
				set[line] = true
			}
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "files", "*"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		addLines(string(b))
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				if s, ok := c.(string); ok && (k == "text" || k == "message") {
					addLines(s)
				} else {
					walk(c)
				}
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	read := func(path string) any {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return v
	}
	walk(read(filepath.Join(dir, "answers.json")))
	maps, _ := filepath.Glob(filepath.Join(dir, "mappings", "*.json"))
	for _, m := range maps {
		if mm, ok := read(m).(map[string]any); ok {
			walk(mm["request"]) // the prompts: the request's messages
		}
	}
	var out []string
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatalf("no recorded text found under %s", dir)
	}
	return out
}
