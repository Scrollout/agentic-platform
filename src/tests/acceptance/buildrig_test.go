//go:build docker

// T046: the shared rig of the US2 and planted-failure acceptance tests. It starts the lab
// (src/tests/lab) that builds and runs the platform's own services (lab.Build()) over one step
// recording set (lab.Steps), gets the example slice approved through the engine API, starts the
// step workers of each run's project (l.StepWorkers) and gives the tests typed reads of runs
// (research D26, D34, ADR 0038).
//
// What does not exist yet is named in `prerequisites`, and each test fails with that list instead of
// a bare HTTP error. A missing Docker is a failure, never a skip (lab.Start).
package acceptance

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/tests/lab"
)

// The pattern's steps, in order (src/catalogue/api-feature-go/pattern.yaml).
var stepOrder = []string{"contract-check", "generate", "tests", "code", "review"}

// The step recording sets, as lab.Steps names them (under src/tests/recorded/steps/): each holds
// answers.json (what the step agents answer, T054k) and the mappings made from it.
const (
	happySet   = "happy"   // the step agents answering the example slice correctly (TestUS2)
	resumeSet  = "resume"  // the same, with the code step's first answer held 30 s (US2.10 only)
	plantedSet = "planted" // planted/<scenario> holds the injected answers of that scenario
)

// Run and Event are the engine's run and run-event, as far as the tests read them.
type Run struct {
	ID        string `json:"id"`
	Slice     string `json:"slice"`
	Pattern   string `json:"pattern"`
	Project   string `json:"project"`
	Status    string `json:"status"`
	Outcome   string `json:"outcome"`
	Reason    string `json:"reason"`
	Operation string `json:"operation"`
	ReplayOf  string `json:"replay_of"`
	EndedAt   string `json:"ended_at"`
	ExpiresAt string `json:"expires_at"`
}

type Event struct {
	Seq         int      `json:"seq"`
	At          string   `json:"at"`
	Run         string   `json:"run"`
	Slice       string   `json:"slice"`
	Kind        string   `json:"kind"`
	Step        string   `json:"step"`
	Attempt     int      `json:"attempt"`
	Round       int      `json:"round"`
	Worker      string   `json:"worker"`
	Model       string   `json:"model"`
	AgentDigest string   `json:"agent_digest"`
	Pattern     string   `json:"pattern"`
	Artifacts   []string `json:"artifacts"`
	ReuseKey    string   `json:"reuse_key"`
	Refs        []string `json:"refs"`
	Check       *struct {
		Name   string `json:"name"`
		Passed bool   `json:"passed"`
		Reason string `json:"reason"`
	} `json:"check"`
	Finding *struct {
		Rule     string `json:"rule"`
		File     string `json:"file"`
		Line     int    `json:"line"`
		Text     string `json:"text"`
		Blocking bool   `json:"blocking"`
		Check    string `json:"check"` // informational: no check consults it (run-event schema)
		Witness  *struct {
			File string `json:"file"`
			Test string `json:"test"`
		} `json:"witness"`
	} `json:"finding"`
	Detail string `json:"detail"`
}

// prerequisites lists what the acceptance tests need and the repository does not have yet.
func prerequisites(t testing.TB) []string {
	root := lab.Root(t)
	var missing []string
	exists := func(p string) bool { _, err := os.Stat(filepath.Join(root, p)); return err == nil }
	for _, a := range []string{"tests", "code", "review"} {
		if !exists("src/agents/" + a + ".yaml") {
			missing = append(missing, "T049: the step agent src/agents/"+a+".yaml")
		}
	}
	if !exists("src/engine/procedures") {
		missing = append(missing, "T050: the fixed procedures src/engine/procedures (contract-check, generate)")
	}
	if !exists("src/engine/cmd/check") {
		missing = append(missing, "T054c: the check service src/engine/cmd/check")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "src/deploy/ap")); strings.Contains(string(b), "build-workers: not available yet") {
		missing = append(missing, "T053a: the launcher's build-workers (per-slice step containers)")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "src/cli/cli.go")); strings.Contains(string(b), `"build": "T055"`) {
		missing = append(missing, "T055: the CLI's build, resume, replay, take-over, compare, artifact, runs and run commands")
	}
	return missing
}

// requirePrerequisites fails the test, listing everything that is missing.
func requirePrerequisites(t *testing.T) {
	t.Helper()
	if m := prerequisites(t); len(m) > 0 {
		t.Fatalf("the platform cannot build yet; missing:\n  - %s", strings.Join(m, "\n  - "))
	}
}

// mappingName is a mapping file the generator (synth-steps.sh) makes: <run>-<step>-a<n>[-r<r>]-t<k>.json,
// lowercase, for example run-1-code-a1-t1.json or run-1-review-a1-r2-t3.json.
var mappingName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*-a[0-9]+(-r[0-9]+)?-t[0-9]+\.json$`)

// requireSet fails the test, naming every problem, unless the step set (as lab.Steps names it)
// holds its answers.json and, unless the generator is running, at least one mapping with a name the
// generator makes. The generator (AP_LAB_MODEL=scripted) makes the mappings, so only answers.json
// is required then.
func requireSet(t *testing.T, set string) {
	t.Helper()
	dir := filepath.Join(lab.Root(t), "src/tests/recorded/steps", filepath.FromSlash(set))
	rel := "src/tests/recorded/steps/" + set
	var problems []string
	if _, err := os.Stat(filepath.Join(dir, "answers.json")); err != nil {
		problems = append(problems, rel+"/answers.json is missing")
	}
	if os.Getenv("AP_LAB_MODEL") != "scripted" {
		var names []string
		ents, _ := os.ReadDir(filepath.Join(dir, "mappings"))
		for _, e := range ents {
			if e.Type().IsRegular() {
				names = append(names, e.Name())
				if !mappingName.MatchString(e.Name()) {
					problems = append(problems, rel+"/mappings/"+e.Name()+" is not named <run>-<step>-a<n>[-r<r>]-t<k>.json")
				}
			}
		}
		if len(names) == 0 {
			problems = append(problems, rel+"/mappings/ holds no mapping")
		}
	}
	if len(problems) > 0 {
		t.Fatalf("the step recordings are not there (write the set's answers.json, then make the mappings with "+
			"`sh src/tests/recorded/synth-steps.sh`, as src/tests/recorded/README.md describes):\n  - %s", strings.Join(problems, "\n  - "))
	}
}

// buildLab starts a lab that builds the platform's services and answers the step agents from the
// step set (lab.Steps; "" for none: the step agents then have no answer), with any further options,
// and returns it with the example slice, approved. The first slice of a lab is S-1. The step workers
// of a run's project are started by startBuild.
func buildLab(t *testing.T, set string, opts ...lab.Option) (*lab.Lab, string) {
	t.Helper()
	requirePrerequisites(t)
	opts = append([]lab.Option{lab.Build()}, opts...)
	if set != "" {
		requireSet(t, set)
		opts = append(opts, lab.Steps(set))
	}
	l := lab.Start(t, opts...)
	id := l.Create(t, lab.Example)
	answerExample(t, l, id)
	for _, p := range []string{"goals", "rules", "contract"} {
		l.Approve(t, id, p)
	}
	if d := l.Get(t, id); d.State != "approved" {
		t.Fatalf("the example slice is %q after its three approvals, want approved", d.State)
	}
	return l, id
}

// startBuild starts the step workers of the project, then the run (startRun). The caller stops the
// workers with l.StopStepWorkers(t, slice, project) once the run has ended.
func startBuild(t *testing.T, l *lab.Lab, slice, project string) lab.Operation {
	t.Helper()
	l.StepWorkers(t, slice, project)
	return startRun(t, l, slice, project)
}

// ---- typed reads --------------------------------------------------------------------------------

// do is l.Do with the client token, retrying a 429 RateLimited up to 3 times, so a burst of the
// suites' own reads does not trip the engine's per-token limit (that limit is T015's test, not
// these suites'). It would wait for Retry-After, but l.Do returns only the status, the
// Content-Type and the body, so the header is always missing here and it waits 1 s, the limit's
// window.
func do(l *lab.Lab, method, path string, body any) (int, string, []byte) {
	st, ct, b := l.Do(method, path, lab.ClientToken, body, nil)
	for retry := 0; retry < 3 && st == 429; retry++ {
		time.Sleep(time.Second)
		st, ct, b = l.Do(method, path, lab.ClientToken, body, nil)
	}
	return st, ct, b
}

// refused is l.Refused through do: the request must be refused with the status and problem code,
// as application/problem+json, and it returns the problem.
func refused(t testing.TB, l *lab.Lab, method, path string, body any, status int, code string) lab.Problem {
	t.Helper()
	st, ct, b := do(l, method, path, body)
	var p lab.Problem
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatalf("%s %s = %d %s: not a problem: %s", method, path, st, ct, b)
	}
	if st != status || p.Code != code {
		t.Fatalf("%s %s = %d %s, want %d %s: %s", method, path, st, p.Code, status, code, b)
	}
	if !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("%s %s: Content-Type %q, want application/problem+json", method, path, ct)
	}
	return p
}

func get(t testing.TB, l *lab.Lab, path string, into any) {
	t.Helper()
	st, _, b := do(l, "GET", path, nil)
	if st != 200 {
		t.Fatalf("GET %s = %d: %s", path, st, b)
	}
	if err := json.Unmarshal(b, into); err != nil {
		t.Fatalf("GET %s: %v: %s", path, err, b)
	}
}

func getRun(t testing.TB, l *lab.Lab, id string) Run {
	t.Helper()
	var r Run
	get(t, l, "/runs/"+url.PathEscape(id), &r)
	return r
}

// events reads the whole run record, page by page, until a page has no next_page_token.
func events(t testing.TB, l *lab.Lab, runID string) []Event {
	t.Helper()
	var all []Event
	tok := ""
	for i := 0; i < 100; i++ {
		p := "/runs/" + url.PathEscape(runID) + "/events"
		if tok != "" {
			p += "?page_token=" + url.QueryEscape(tok)
		}
		var page struct {
			Items []Event `json:"items"`
			Next  string  `json:"next_page_token"`
		}
		get(t, l, p, &page)
		all = append(all, page.Items...)
		if page.Next == "" {
			return all
		}
		tok = page.Next
		if len(page.Items) == 0 {
			// A running run's last page still has a token; the record read so far is what there is.
			return all
		}
	}
	return all
}

// startRun sends startRun and returns the operation as acknowledged. It does not use lab.Accept so
// that a refusal is described, not just numbered.
func startRun(t testing.TB, l *lab.Lab, slice, project string) lab.Operation {
	t.Helper()
	st, _, b := l.Do("POST", "/slices/"+slice+"/runs", lab.ClientToken, map[string]string{"project": project}, nil)
	if st != 202 {
		t.Fatalf("startRun %s %s = %d, want 202: %s", slice, project, st, b)
	}
	var op lab.Operation
	if err := json.Unmarshal(b, &op); err != nil || op.ID == "" || op.Target == "" {
		t.Fatalf("startRun: no operation with a target in %s", b)
	}
	return op
}

// waitRun polls a run until stop(run) holds, within the limit.
func waitRun(t testing.TB, l *lab.Lab, id string, limit time.Duration, stop func(Run) bool) Run {
	t.Helper()
	deadline := time.Now().Add(limit)
	var r Run
	for time.Now().Before(deadline) {
		r = getRun(t, l, id)
		if stop(r) {
			return r
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("run %s did not reach the wanted state within %s; last state %+v", id, limit, r)
	return r
}

func ended(r Run) bool { return r.Status == "ended" }

// stopped is a run that is not going on by itself: ended, waiting for the developer or interrupted.
func stopped(r Run) bool { return r.Status != "running" }

// kinds counts the events of a kind for a step ("" for any step).
func count(evs []Event, kind, step string) int {
	n := 0
	for _, e := range evs {
		if e.Kind == kind && (step == "" || e.Step == step) {
			n++
		}
	}
	return n
}

func failedCheck(evs []Event, step, check string) int {
	n := 0
	for _, e := range evs {
		if e.Kind == "CheckFailed" && e.Step == step && e.Check != nil && e.Check.Name == check && !e.Check.Passed {
			n++
		}
	}
	return n
}

func first(evs []Event, pred func(Event) bool) int {
	for i, e := range evs {
		if pred(e) {
			return i
		}
	}
	return -1
}

// wellFormedRecord requires the record's order and the traceability of every check (SC-006,
// FR-018): `seq` from 1 without gaps, `at` not decreasing, RunStarted first with the pattern,
// every check event naming refs, every attempt naming its model, agent definition and pattern.
func wellFormedRecord(t testing.TB, evs []Event) {
	t.Helper()
	if len(evs) == 0 {
		t.Fatalf("the run record is empty")
	}
	if evs[0].Kind != "RunStarted" || evs[0].Pattern == "" || evs[0].Slice == "" {
		t.Errorf("the record does not start with RunStarted naming the slice and the pattern: %+v", evs[0])
	}
	for i, e := range evs {
		if e.Seq != i+1 {
			t.Errorf("event %d has seq %d, want %d (no gaps)", i, e.Seq, i+1)
		}
		if i > 0 && e.At < evs[i-1].At {
			t.Errorf("event %d (%s) is dated %s, before the one before it, %s", e.Seq, e.Kind, e.At, evs[i-1].At)
		}
		switch e.Kind {
		case "CheckPassed", "CheckFailed":
			if e.Check == nil || len(e.Refs) == 0 {
				t.Errorf("event %d %s names no check or no goal, rule or example it concerns: %+v", e.Seq, e.Kind, e)
			}
		case "AttemptMade", "StepAccepted":
			if strings.HasPrefix(e.Worker, "agent:") && (e.Model == "" || e.AgentDigest == "") {
				t.Errorf("event %d %s of agent step %s names no model or agent definition: %+v", e.Seq, e.Kind, e.Step, e)
			}
			if e.Pattern == "" || e.ReuseKey == "" || len(e.Artifacts) == 0 {
				t.Errorf("event %d %s of step %s lacks the pattern, the reuse key or the artifacts: %+v", e.Seq, e.Kind, e.Step, e)
			}
		}
	}
}

// artifact reads an artifact's bytes through getArtifact.
func artifact(t testing.TB, l *lab.Lab, run, hash string) []byte {
	t.Helper()
	st, _, b := do(l, "GET", "/runs/"+run+"/artifacts/"+hash, nil)
	if st != 200 {
		t.Fatalf("getArtifact %s %s = %d: %s", run, hash, st, b)
	}
	return b
}

// acceptedArtifacts maps each step to the artifacts of its StepAccepted or ResultReused event.
func acceptedArtifacts(evs []Event) map[string][]string {
	m := map[string][]string{}
	for _, e := range evs {
		if e.Kind == "StepAccepted" || e.Kind == "ResultReused" {
			m[e.Step] = e.Artifacts
		}
	}
	return m
}

// acceptHandOver takes over a run that waits for the developer, and checks the run ends handed-over.
func acceptHandOver(t testing.TB, l *lab.Lab, runID string) {
	t.Helper()
	if op := l.Wait(t, l.Accept(t, "POST", "/runs/"+runID+"/hand-over", nil)); op.Status != "succeeded" {
		t.Fatalf("acceptHandOver %s: %+v (error %+v)", runID, op, op.Error)
	}
	r := getRun(t, l, runID)
	if r.Status != "ended" || r.Outcome != "handed-over" || r.Reason == "" {
		t.Errorf("after the hand-over run %s is %+v, want ended, handed-over, with the reason", runID, r)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

// answerExample answers the example slice's two questions as US1 does (Q-1 option 0, Q-2 "400").
func answerExample(t *testing.T, l *lab.Lab, id string) {
	t.Helper()
	body := map[string]any{"answers": []any{
		map[string]any{"question": "Q-1", "answer": map[string]any{"option": 0}},
		map[string]any{"question": "Q-2", "answer": map[string]any{"text": "400"}},
	}}
	if op := l.Wait(t, l.Accept(t, "POST", "/slices/"+id+"/answers", body)); op.Status != "succeeded" {
		t.Fatalf("answerQuestions: %+v (error %+v)", op, op.Error)
	}
}
