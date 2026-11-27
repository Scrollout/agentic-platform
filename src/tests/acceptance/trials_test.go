//go:build docker

package acceptance

// T012 (feature 012, US1; spec FR-001 to FR-004, FR-012, FR-013, SC-004, SC-005; research R3;
// src/cli/contract.md "Trial sets"; the engine contract's startTrialSet, getTrialSet and
// listTrialSets): `ap trials <slice> <prefix> --trials 2` on the notes slice.
//
// The launcher's `ap trials` is `build-workers <slice> <prefix>-<i>` for i = 1..k, then the
// client's `trials <slice> <prefix> --trials <k>`. A lab has no launcher, so the test does what the
// launcher does, as captured_prove_test.go's cpvProve does for `ap prove`: it starts each project's
// step workers (l.StepWorkers) and registers them with their project (research R3: the lab's own
// registration names no project, so a second project's workers would replace the first's), then
// runs the client `trials` in the lab's client container.
//
// One lab, the notes (example) slice approved, the cases in order, each using what the ones before
// it made:
//
//	1. the workers of both projects (trial-1, trial-2) are up and registered; the step containers'
//	   memory for the two projects is logged (research R3, plan P2: the owner judges it)
//	2. `trials S-1 trial --trials 2` prints the set and its operation, one line per trial in order
//	   as it ends, then the result `done 2/2`; it exits 0
//	3. the two runs: exactly two, one after the other (RUN-2 starts after RUN-1 ended), each in
//	   its own project with its own files, each a done run naming the set and its position
//	4. no trial reuses a result (SC-004): no ResultReused, an attempt of every agent step in each
//	   record, and the step model received a request of every agent step of each run
//	5. the set: getTrialSet, listTrialSets and the set's operation tell the same
//	6. `trial-set TS-1` prints `done 2/2` and no `unstable`, exits 0; reading the set, by the
//	   client and the API, adds no model request and changes no run record (SC-005)
//
// The recorded model: the step agents are answered by the scripted step model over a temporary
// copy of the `happy` set, as captured_test.go does. Reason: the recorded `happy` mappings match
// the whole request body exactly, and the body names `RUN-1` and `/projects/notes`
// (steps/happy/mappings/run-1-code-a1-t1.json), so only a run RUN-1 into a project `notes` is
// answered; the second trial is RUN-2 into `<prefix>-2` and would get 404 from the replay server.
// The scripted server picks its answer by step, attempt and round only, for any run and project,
// and as often as asked. No recording is changed or made. The analyst still answers from the
// replay server's existing recordings (the example description).
//
// Edge cases checked: a set of 2 (not the default 3: exactly two runs exist, no third starts); the
// order of the trials (RUN-2 starts only after RUN-1 ended, FR-003); a trial running on another
// trial's workers (each project's folder holds its own code step's files, so a run into
// trial-1 did not write into trial-2); the lab's registration without a project, which would
// serve either project, is replaced by registrations with one; each trial a fresh first build
// (no ResultReused, an attempt and a model request for each agent step of each run); the set's
// header line exactly (`done 2/2`, no `unstable`, no `stopped`, no `methods mixed`); the client's
// exit code 0; the trial lines in trial order before the result; the run's trial fields and
// the RunStarted event's; the set resource's runs in trial order and its operation, of kind
// run-trials and targeting the set, succeeded; reads change nothing (byte-equal records and
// no new model request on either model server, by request IDs and exchange names); a client that
// does not end within its limit fails the test; a stray run (a third) fails the count.
// Left out (other tasks' tests): refusals before the set starts (T006 to T008), the measures and
// the files' agreement (US2, T015 and T016), the comparison with a previous set (US3), the
// budget, cancel and time limits (T007, T008), the launcher itself (T004, T010).
//
// Expected to fail first before T009 and T011 (and T005): the client has no `trials` command and
// the engine's startTrialSet answers "not implemented" (T001's generated server), so case 2 fails
// with the client's usage error (exit 3) or the engine's refusal, and cases 3 to 6 stop for want
// of the set. Cases 1 passes (the lab's workers and the registration with `project`).
// Expected time: lab ~5 min, two builds 3 to 10 min each; ~25 min.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/tests/lab"
)

const (
	trlSlicePrefix = "trial"
	trlTrials      = 2
	trlLimit       = 40 * time.Minute // the client's own cap (a build is 3 to 10 minutes, SC-007 allows 30)
)

// The agent steps of the base pattern (the others are fixed procedures that use no model).
var trlAgentSteps = []string{"tests", "code", "review"}

type trlRun struct {
	ID       string `json:"id"`
	Slice    string `json:"slice"`
	Project  string `json:"project"`
	Status   string `json:"status"`
	Outcome  string `json:"outcome"`
	EndedAt  string `json:"ended_at"`
	TrialSet string `json:"trial_set"`
	Trial    int    `json:"trial"`
}

type trlStarted struct {
	Kind     string `json:"kind"`
	At       string `json:"at"`
	TrialSet string `json:"trial_set"`
	Trial    int    `json:"trial"`
}

type trlSet struct {
	ID             string   `json:"id"`
	Slice          string   `json:"slice"`
	Trials         int      `json:"trials"`
	ProjectPrefix  string   `json:"project_prefix"`
	Operation      string   `json:"operation"`
	State          string   `json:"state"`
	StoppedBecause string   `json:"stopped_because"`
	Runs           []string `json:"runs"`
	Result         struct {
		Ended        int  `json:"ended"`
		Done         int  `json:"done"`
		Unstable     bool `json:"unstable"`
		MethodsMixed bool `json:"methods_mixed"`
		Outcomes     []struct {
			Trial   int    `json:"trial"`
			Run     string `json:"run"`
			Outcome string `json:"outcome"`
		} `json:"outcomes"`
	} `json:"result"`
	EndedAt string `json:"ended_at"`
}

// trlHash mirrors the lab's projectHash (src/tests/lab/stepworkers.go): the 12 hex digits of the
// project's SHA-256 that name its step containers and tokens.
func trlHash(project string) string {
	h := sha256.Sum256([]byte(project))
	return hex.EncodeToString(h[:])[:12]
}

func trlContainer(slice, step, project string) string {
	return fmt.Sprintf("aptest-lab-%d-worker-%s-%s-%s", os.Getpid(), strings.ToLower(slice), step, trlHash(project))
}

// trlRegister registers the project's step containers, which l.StepWorkers started, with the
// project named, as the launcher's build-workers does (research R3). The token is the lab's
// (src/tests/lab/stepworker_tokens.go), a constant of the test installation.
func trlRegister(t *testing.T, l *lab.Lab, slice, project string) {
	t.Helper()
	for _, step := range stepOrder {
		name := trlContainer(slice, step, project)
		if out, code := lab.Run("docker", "inspect", "--format", "{{.State.Running}}", name); code != 0 || strings.TrimSpace(out) != "true" {
			t.Fatalf("no running step container %s (the lab's container naming changed? exit %d: %s)", name, code, out)
		}
		token := "lab-step-worker-" + strings.ToLower(slice) + "-" + step + "-" + trlHash(project)
		body := map[string]string{"endpoint": "http://" + name + ":8080", "project": project}
		if st, _, b := l.Do("POST", "/workers", token, body, nil); st != 200 {
			t.Fatalf("registerWorker %s %s project %s = %d, want 200: %s", slice, step, project, st, b)
		}
	}
}

var trlMem = regexp.MustCompile(`^([0-9.]+)\s*([KMGT]?i?B|kB)$`)

// trlMiB reads docker stats' "12.3MiB" into MiB.
func trlMiB(s string) (float64, bool) {
	m := trlMem.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	switch m[2] {
	case "B":
		return v / (1 << 20), true
	case "KiB":
		return v / 1024, true
	case "kB":
		return v * 1000 / (1 << 20), true
	case "MiB":
		return v, true
	case "MB":
		return v * 1e6 / (1 << 20), true
	case "GiB":
		return v * 1024, true
	case "GB":
		return v * 1e9 / (1 << 20), true
	}
	return 0, false
}

// trlLogMemory records the memory of the step containers of the projects in the test log (research
// R3; the owner judges it) and requires that every step of every project was measured.
func trlLogMemory(t *testing.T, slice string, projects ...string) {
	t.Helper()
	var all []string
	owner := map[string]string{}
	for _, p := range projects {
		for _, s := range stepOrder {
			n := trlContainer(slice, s, p)
			all = append(all, n)
			owner[n] = p
		}
	}
	out, code := lab.Run("docker", append([]string{"stats", "--no-stream", "--format", "{{.Name}}\t{{.MemUsage}}"}, all...)...)
	if code != 0 {
		t.Errorf("docker stats of the step containers: exit %d: %s", code, out)
		return
	}
	per := map[string]float64{}
	n := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		name, usage, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		used, _, _ := strings.Cut(usage, "/")
		mib, ok := trlMiB(used)
		p := owner[strings.TrimSpace(name)]
		if !ok || p == "" {
			t.Errorf("docker stats line %q: not a step container's memory", line)
			continue
		}
		per[p] += mib
		n[p]++
		t.Logf("memory: %s %s", name, strings.TrimSpace(usage))
	}
	total := 0.0
	for _, p := range projects {
		if n[p] != len(stepOrder) {
			t.Errorf("memory of project %s: %d of %d step containers measured", p, n[p], len(stepOrder))
		}
		t.Logf("memory: project %s, %d step containers: %.1f MiB", p, n[p], per[p])
		total += per[p]
	}
	t.Logf("memory: %d projects, %d step containers: %.1f MiB in all (research R3, plan P2)", len(projects), len(all), total)
}

func trlRaw(t testing.TB, l *lab.Lab, path string) []byte {
	t.Helper()
	st, _, b := do(l, "GET", path, nil)
	if st != 200 {
		t.Fatalf("GET %s = %d: %s", path, st, b)
	}
	return b
}

func trlRunOf(t testing.TB, l *lab.Lab, id string) trlRun {
	t.Helper()
	var r trlRun
	if err := json.Unmarshal(trlRaw(t, l, "/runs/"+id), &r); err != nil {
		t.Fatalf("GET /runs/%s: %v", id, err)
	}
	return r
}

// trlStartedOf is the run's RunStarted event, which is first in the record.
func trlStartedOf(t testing.TB, l *lab.Lab, run string) trlStarted {
	t.Helper()
	var page struct {
		Items []trlStarted `json:"items"`
	}
	if err := json.Unmarshal(trlRaw(t, l, "/runs/"+run+"/events"), &page); err != nil || len(page.Items) == 0 {
		t.Fatalf("the record of %s: %v, %d events", run, err, len(page.Items))
	}
	if page.Items[0].Kind != "RunStarted" {
		t.Fatalf("the record of %s starts with %s, want RunStarted", run, page.Items[0].Kind)
	}
	return page.Items[0]
}

func trlTime(t testing.TB, what, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("%s %q is not a time: %v", what, s, err)
	}
	return ts
}

func TestTrials(t *testing.T) {
	// The scripted step model over a copy of `happy` (see the header: the replay mappings serve
	// only RUN-1 into `notes`).
	set := t.TempDir()
	happy := filepath.Join(lab.Root(t), "src", "tests", "recorded", "steps", "happy")
	for _, c := range []struct{ from, to string }{{happy, set}, {filepath.Join(happy, "files"), filepath.Join(set, "files")}} {
		if err := cpvCopyFiles(c.from, c.to); err != nil {
			t.Fatal(err)
		}
	}
	requirePrerequisites(t)
	t.Setenv("AP_LAB_MODEL", "scripted")
	bin := buildCLI(t)
	l := lab.Start(t, lab.Build(), lab.Steps(set))

	slice := l.Create(t, lab.Example)
	answerExample(t, l, slice)
	for _, p := range []string{"goals", "rules", "contract"} {
		l.Approve(t, slice, p)
	}
	if d := l.Get(t, slice); d.State != "approved" {
		t.Fatalf("the notes slice is %q after its three approvals, want approved", d.State)
	}
	projects := []string{trlSlicePrefix + "-1", trlSlicePrefix + "-2"}
	runIDs := []string{"RUN-1", "RUN-2"}

	var (
		out, errOut string
		code        int
		header      string
		operation   string
		before      capLog
		ready       bool
		finished    bool
	)
	need := func(t *testing.T, ok bool, what string) {
		t.Helper()
		if !ok {
			t.Fatalf("not run: %s", what)
		}
	}
	headerRE := regexp.MustCompile(`^TS-1  slice ` + regexp.QuoteMeta(slice) + `  state ended  done 2/2$`)
	findHeader := func(out string) string {
		for _, ln := range cpvLines(out) {
			if strings.HasPrefix(ln, "TS-1  slice ") {
				return ln
			}
		}
		return ""
	}

	t.Run("1 both projects have their step workers; the containers' memory is recorded", func(t *testing.T) {
		for _, p := range projects {
			l.StepWorkers(t, slice, p)
		}
		for _, p := range projects {
			trlRegister(t, l, slice, p)
		}
		trlLogMemory(t, slice, projects...)
		ready = true
	})

	t.Run("2 trials follows the two trials in order and prints the result", func(t *testing.T) {
		need(t, ready, "the workers are not up")
		before = capSeen(t, l)
		begin := time.Now()
		c := capStart(t, l, bin, trlLimit, "trials", slice, trlSlicePrefix, "--trials", strconv.Itoa(trlTrials))
		out, errOut, code = c.wait(t, "trials "+slice+" "+trlSlicePrefix, trlLimit)
		t.Logf("trials %s %s --trials %d took %s (exit %d)", slice, trlSlicePrefix, trlTrials, time.Since(begin).Round(time.Second), code)
		if code != 0 {
			t.Fatalf("trials exited %d, want 0 (both trials end done); stdout:\n%s\nstderr:\n%s", code, out, errOut)
		}
		lines := cpvLines(out)
		if len(lines) == 0 {
			t.Fatalf("trials printed nothing on standard output; stderr: %s", errOut)
		}
		first := regexp.MustCompile(`^TS-1  operation (OP-[0-9]+)$`).FindStringSubmatch(lines[0])
		if first == nil {
			t.Fatalf("the first line is %q, want `TS-1  operation OP-<m>`", lines[0])
		}
		operation = first[1]
		at := func(want string) int {
			for i, ln := range lines {
				if ln == want {
					return i
				}
			}
			return -1
		}
		i1, i2 := at("trial 1  RUN-1  done"), at("trial 2  RUN-2  done")
		header = findHeader(out)
		ih := -1
		for i, ln := range lines {
			if ln == header {
				ih = i
			}
		}
		if i1 < 1 || i2 < 1 || i1 >= i2 {
			t.Errorf("the output holds `trial 1  RUN-1  done` at line %d and `trial 2  RUN-2  done` at %d, want both after the first line and in trial order:\n%s", i1, i2, out)
		}
		if header == "" || !headerRE.MatchString(header) {
			t.Errorf("the result's first line is %q, want `TS-1  slice %s  state ended  done 2/2` (no unstable, stopped or methods mixed)", header, slice)
		} else if ih < i2 {
			t.Errorf("the result (line %d) comes before the last trial's line (%d)", ih, i2)
		}
		if strings.Contains(strings.ToLower(out), "unstable") {
			t.Errorf("the output names `unstable` for two done trials:\n%s", out)
		}
		finished = true
	})

	t.Run("3 two runs, one after the other, each in its own project and set position", func(t *testing.T) {
		need(t, finished, "the set did not end")
		var list struct {
			Items []trlRun `json:"items"`
		}
		if err := json.Unmarshal(trlRaw(t, l, "/runs"), &list); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, r := range list.Items {
			ids = append(ids, r.ID)
		}
		sort.Strings(ids)
		if strings.Join(ids, ",") != strings.Join(runIDs, ",") {
			t.Fatalf("the lab's runs are %v, want exactly %v (a set of %d)", ids, runIDs, trlTrials)
		}
		var ended [2]time.Time
		var started [2]time.Time
		for i, id := range runIDs {
			r := trlRunOf(t, l, id)
			if r.Slice != slice || r.Project != projects[i] || r.Status != "ended" || r.Outcome != "done" {
				t.Errorf("%s is %+v, want slice %s, project %s, ended, done", id, r, slice, projects[i])
			}
			if r.TrialSet != "TS-1" || r.Trial != i+1 {
				t.Errorf("%s carries trial_set %q and trial %d, want TS-1 and %d", id, r.TrialSet, r.Trial, i+1)
			}
			s := trlStartedOf(t, l, id)
			if s.TrialSet != "TS-1" || s.Trial != i+1 {
				t.Errorf("%s's RunStarted carries trial_set %q and trial %d, want TS-1 and %d", id, s.TrialSet, s.Trial, i+1)
			}
			started[i] = trlTime(t, id+" RunStarted at", s.At)
			ended[i] = trlTime(t, id+" ended_at", r.EndedAt)
			// Its own project's workers wrote its files (FR-013): the code step's file is in the folder.
			if _, err := os.Stat(filepath.Join(l.ProjectsDir(), projects[i], "internal", "app", "app.go")); err != nil {
				t.Errorf("project %s holds no internal/app/app.go: %v (the trial ran on another project's workers?)", projects[i], err)
			}
		}
		if started[1].Before(ended[0]) {
			t.Errorf("RUN-2 started at %s, before RUN-1 ended at %s: trials run one after another (FR-003)", started[1], ended[0])
		}
	})

	t.Run("4 no trial reuses a result: every agent step makes its attempt and its model request", func(t *testing.T) {
		need(t, finished, "the set did not end")
		after := capSeen(t, l)
		for _, id := range runIDs {
			evs := events(t, l, id)
			wellFormedRecord(t, evs)
			if n := count(evs, "ResultReused", ""); n != 0 {
				t.Errorf("%s records %d ResultReused: a trial reuses no result (SC-004)", id, n)
			}
			for _, s := range trlAgentSteps {
				if count(evs, "AttemptMade", s) == 0 {
					t.Errorf("%s records no AttemptMade of agent step %s (SC-004)", id, s)
				}
				if count(evs, "StepAccepted", s) == 0 {
					t.Errorf("%s records no StepAccepted of step %s", id, s)
				}
				prefix := strings.ToLower(id) + "-" + s + "-a"
				found := false
				for n := range after.exchanges {
					if strings.HasPrefix(n, prefix) {
						found = true
					}
				}
				if !found {
					t.Errorf("the step model received no request of %s, step %s (SC-004: model use for every agent step); exchanges %v", id, s, trlNames(after.exchanges))
				}
			}
		}
		if len(capNewSince(before, after)) == 0 {
			t.Error("the model servers show no request since the client started the set")
		}
	})

	t.Run("5 the set, its listing and its operation tell the same", func(t *testing.T) {
		need(t, finished, "the set did not end")
		var s trlSet
		if err := json.Unmarshal(trlRaw(t, l, "/trial-sets/TS-1"), &s); err != nil {
			t.Fatal(err)
		}
		if s.ID != "TS-1" || s.Slice != slice || s.Trials != trlTrials || s.ProjectPrefix != trlSlicePrefix || s.State != "ended" || s.StoppedBecause != "" || s.EndedAt == "" {
			t.Errorf("getTrialSet = %+v, want TS-1 of %s, 2 trials, prefix %s, ended, not stopped, with ended_at", s, slice, trlSlicePrefix)
		}
		if strings.Join(s.Runs, ",") != "RUN-1,RUN-2" {
			t.Errorf("the set's runs are %v, want [RUN-1 RUN-2] in trial order", s.Runs)
		}
		r := s.Result
		if r.Ended != 2 || r.Done != 2 || r.Unstable || r.MethodsMixed || len(r.Outcomes) != 2 {
			t.Errorf("the result is %+v, want 2 ended, 2 done, not unstable, methods not mixed, 2 outcomes", r)
		}
		for i, o := range r.Outcomes {
			if i < 2 && (o.Trial != i+1 || o.Run != runIDs[i] || o.Outcome != "done") {
				t.Errorf("outcome %d is %+v, want trial %d, %s, done", i, o, i+1, runIDs[i])
			}
		}
		if s.Operation != operation {
			t.Errorf("the set's operation is %q, the client printed %q", s.Operation, operation)
		}
		var op lab.Operation
		if err := json.Unmarshal(trlRaw(t, l, "/operations/"+operation), &op); err != nil {
			t.Fatal(err)
		}
		if op.Kind != "run-trials" || op.Target != "TS-1" || op.Status != "succeeded" {
			t.Errorf("the operation is %+v, want kind run-trials, target TS-1, succeeded", op)
		}
		var list struct {
			Items []trlSet `json:"items"`
		}
		if err := json.Unmarshal(trlRaw(t, l, "/slices/"+slice+"/trial-sets"), &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Items) != 1 || list.Items[0].ID != "TS-1" || list.Items[0].Result.Done != 2 || list.Items[0].Result.Ended != 2 {
			t.Errorf("listTrialSets = %+v, want TS-1 with done 2 of 2 ended", list.Items)
		}
	})

	t.Run("6 trial-set prints done 2/2 without unstable; reading adds no model request and changes no run", func(t *testing.T) {
		need(t, finished, "the set did not end")
		paths := []string{"/runs/RUN-1", "/runs/RUN-2", "/runs/RUN-1/events", "/runs/RUN-2/events", "/trial-sets/TS-1"}
		snap := func() map[string][]byte {
			m := map[string][]byte{}
			for _, p := range paths {
				m[p] = trlRaw(t, l, p)
			}
			return m
		}
		recordsBefore, seenBefore := snap(), capSeen(t, l)

		var outs [2]string
		for i := range outs {
			so, se, c := cpvCLI(t, l, bin, "trial-set", "TS-1")
			if c != 0 {
				t.Errorf("trial-set TS-1 exited %d, want 0: %s%s", c, so, se)
			}
			outs[i] = so
			if h := findHeader(so); !headerRE.MatchString(h) {
				t.Errorf("trial-set TS-1's first line of the result is %q, want `TS-1  slice %s  state ended  done 2/2`:\n%s", h, slice, so)
			} else if h != header {
				t.Errorf("trial-set prints %q, trials printed %q", h, header)
			}
			if strings.Contains(strings.ToLower(so), "unstable") {
				t.Errorf("trial-set TS-1 names `unstable` for two done trials:\n%s", so)
			}
		}
		if outs[0] != outs[1] {
			t.Errorf("two reads of the same ended set print different results:\n%s\n---\n%s", outs[0], outs[1])
		}
		trlRaw(t, l, "/slices/"+slice+"/trial-sets")

		recordsAfter, seenAfter := snap(), capSeen(t, l)
		if added := capNewSince(seenBefore, seenAfter); len(added) != 0 {
			t.Errorf("reading the set made %d model requests, want 0 (SC-005): %v", len(added), added)
		}
		for _, p := range paths {
			if !bytes.Equal(recordsBefore[p], recordsAfter[p]) {
				t.Errorf("GET %s changed by reading the set (SC-005)", p)
			}
		}
		// An unknown set is refused, as the contract says (404 NotFound), and changes nothing.
		refused(t, l, "GET", "/trial-sets/TS-99", nil, 404, "NotFound")
	})
}

func trlNames(m map[string]bool) []string {
	var n []string
	for k := range m {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}
