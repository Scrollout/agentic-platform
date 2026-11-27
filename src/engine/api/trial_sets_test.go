package api

// T007 of feature 012 (US1; FR-001 to FR-004, FR-007, FR-010 to FR-013; SC-004 to SC-006;
// research R1, R2, R4, R5, R7; data model "Trial set", "Slice trial", "Write order and repeats"):
// the engine's trial sets through HTTP. Written before the code (T009) by a test author, judged
// from the spec, the engine contract (startTrialSet, listTrialSets, getTrialSet, TrialSet,
// x-limits), the data model and the interface sheet; the code author must not change it (ADR
// 0029). The existing rigs and helpers are used as they are (runsRig and its fakes, newSP, newRig,
// the proof tests' ways of holding a run, dopRig and its fakes); only new helpers are in this file.
//
// Two rigs:
//   - trRig (the engine rig of runs_ops_test.go with its durable test service, as feature 011's
//     psRig is built): real runs on faked step workers, no model; a worker and a checker the
//     tests steer per project (hold a project's first call, charge a cost, fail a check), the
//     rig's fake clock, an engine without a durable Operations (the operation's job runs in the
//     engine, as in the proof tests). Used for everything a run's progress shows.
//   - dopRig (operations_durable_test.go: fake durable Runs and Operations, the job's work held
//     and driven by the test through the handler's Operator, a run's record written by the test
//     as its workflow would): used for what needs the job stopped and repeated, and for the
//     failed writes at acceptance.
//
// Seams assumed (the interface sheet fixes the rest):
//   - the operation of a set has kind `run-trials`, its target is the set's ID, and its result
//     holds `trial_set` (contract: Operation.result gains trial_set);
//   - a trial's `RunStart` has `TrialSet` and `Trial` (read by name through reflection so that
//     the file compiles before T009), `NoReuse` and `RejectDuplicate`, its Project is
//     `<prefix>-<i>`;
//   - how the job is driven step by step: through the handler's Operator (`Do`), as the existing
//     operation-job tests do; a job that cannot start its trial's run returns a retryable error;
//   - the registry of step workers is looked into by a test the way workers_project_test.go
//     does (a Workers made by the test; a registration removed from its map);
//   - the slice is edited through the slices service the engine serves (ReplaceGoals, Approve),
//     as the proof-slice tests change a method;
//   - a trial that leaves `waiting` before the job's hand-over signal (RunNotWaiting) cannot be
//     held between the job's read and its signal by any existing seam; the case is written
//     against the observable behaviour: the developer's own hand-over answers the same call,
//     and the set must end whichever of the two arrives first;
//   - the 24 hours are the contract's `x-limits.trial-set-hours`, moved on the rig's clock.
//
// Edge cases checked: trials 0, 6 (limit + 1), negative, fractional, a string, a missing prefix,
// an empty, upper-case, leading-hyphen, 62-character, underscore, slash and spaced prefix, an extra
// field, InvalidRequest before NotFound; nothing recorded after each refusal (no operation, no
// run, no set file, no worker call); the longest prefix (61) with the most trials (limit) and the
// default count; the refusals in the contract's order (PatternDoesNotFit, NotApproved,
// WorkersNotReady naming the first project and a step, ProjectExists naming the first existing
// folder, RunActive for another set) each with nothing started; tokens that are not a client's
// (403); an Idempotency-Key repeated with the same body and with another; trial order (a trial
// does not start while the one before runs), project names, `trial_set`/`trial` on RunStarted and
// Run, no ResultReused although earlier run results are kept; another active run (the set waits,
// does not fail, starts when it ends); a hand-over of a waiting trial by the set (reason names the
// set) and the next trial; a hand-over by the developer at the same time; cost: the rule's
// boundary (cost so far + pattern run budget exactly the set's budget starts, above it stops),
// the sum never above the set's budget, no trial after the stop; hours: a running trial cancelled
// and no other started, and a set that still waits for another run; slice-changed for a withdrawn
// approval and for an edit approved again (a different hash); workers gone for the next project;
// cancel (the running trial cancelled, none after, the set cancelled, the slice free again);
// reads change nothing (store tree, worker calls, operations); list newest first with the same
// fields as getTrialSet; 404 for an unknown set and slice; erasure of the slice removes its sets;
// limits and budgets equal the contract's; and, on the fake durable service: a failed operation
// write (503, nothing kept, the resent request accepted), a failed set write after the
// operation's (the job writes the set from the operation's record; the slice edited meanwhile
// stops it; a second start and a deleteSlice meanwhile are RunActive), the job repeated after a
// stop (follows the recorded run, never a second; a recorded ID with no workflow and no record is
// started once with the same ID, one `run` operation), deleteSlice while a set runs with no run.
//
// Left out, and why: CapturedPatternWithdrawn and CapturedPatternBusy for a proof slice (T027's
// rig holds them; they need a captured pattern); a page of more than 50 sets (50 sets of a slice
// are too costly; the page size is the list contract's, proven for runs); the result's per-step
// measures (US2, T012); the method of a run mixed with another (T006).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	slicesvc "github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
	"go.yaml.in/yaml/v3"
)

// ---- views of the answers ----

type trSetView struct {
	ID             string            `json:"id"`
	Slice          string            `json:"slice"`
	Trials         int               `json:"trials"`
	ProjectPrefix  string            `json:"project_prefix"`
	Method         string            `json:"method"`
	PartHashes     map[string]string `json:"part_hashes"`
	Operation      string            `json:"operation"`
	State          string            `json:"state"`
	StoppedBecause string            `json:"stopped_because"`
	Runs           []string          `json:"runs"`
	CreatedAt      string            `json:"created_at"`
	EndedAt        *string           `json:"ended_at"`
	Budget         struct {
		CostUSD float64 `json:"cost_usd"`
		Hours   int     `json:"hours"`
	} `json:"budget"`
	Result struct {
		Ended        int  `json:"ended"`
		Done         int  `json:"done"`
		Unstable     bool `json:"unstable"`
		MethodsMixed bool `json:"methods_mixed"`
		Outcomes     []struct {
			Trial   int     `json:"trial"`
			Run     string  `json:"run"`
			Outcome *string `json:"outcome"`
		} `json:"outcomes"`
	} `json:"result"`
}

type trRunView struct {
	ID       string  `json:"id"`
	Project  string  `json:"project"`
	Status   string  `json:"status"`
	Outcome  *string `json:"outcome"`
	TrialSet string  `json:"trial_set"`
	Trial    int     `json:"trial"`
}

type trEvent struct {
	Seq      int    `json:"seq"`
	Kind     string `json:"kind"`
	Detail   string `json:"detail"`
	At       string `json:"at"`
	TrialSet string `json:"trial_set"`
	Trial    int    `json:"trial"`
	Usage    *struct {
		CostUSD float64 `json:"cost_usd"`
	} `json:"usage"`
}

// ---- the steered worker and checker ----

// trCtl steers the faked step workers of every project of a trRig.
type trCtl struct {
	mu      sync.Mutex
	gates   map[string]chan struct{}
	seen    map[string]bool
	started []string          // projects, in the order of their first worker call
	perStep float64           // what each of generate, tests and code reports as its cost
	failing map[string]bool   // projects whose `tests` check fails
	project map[string]string // run -> its project, as the worker calls showed it
	calls   int
}

// hold makes the first and every later worker call of the project wait until release.
func (c *trCtl) hold(project string) (release func()) {
	g := make(chan struct{})
	c.mu.Lock()
	c.gates[project] = g
	c.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(g) }) }
}

func (c *trCtl) releaseAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for p, g := range c.gates {
		select {
		case <-g:
		default:
			close(g)
		}
		delete(c.gates, p)
	}
}

func (c *trCtl) startedProjects() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.started)
}

func (c *trCtl) setCost(totalPerRun float64) {
	c.mu.Lock()
	c.perStep = totalPerRun / 3
	c.mu.Unlock()
}

func (c *trCtl) failTests(project string) {
	c.mu.Lock()
	c.failing[project] = true
	c.mu.Unlock()
}

func (c *trCtl) work(w *fakeStepWorker) func(context.Context, machine.Request) (machine.Result, error) {
	return func(ctx context.Context, r machine.Request) (machine.Result, error) {
		c.mu.Lock()
		c.calls++
		c.project[r.Run] = r.Project
		if !c.seen[r.Project] {
			c.seen[r.Project] = true
			c.started = append(c.started, r.Project)
		}
		g, per := c.gates[r.Project], c.perStep
		c.mu.Unlock()
		if g != nil {
			select {
			case <-g:
			case <-ctx.Done():
				return machine.Result{}, ctx.Err()
			}
		}
		res, err := w.produce(r)
		if err == nil && per > 0 && (r.Step.Name == "generate" || r.Step.Name == "tests" || r.Step.Name == "code") {
			res.Usage.CostUSD = per
		}
		return res, err
	}
}

func (c *trCtl) check(_ context.Context, r machine.CheckRequest) (machine.CheckResult, error) {
	c.mu.Lock()
	fail := c.failing[c.project[r.Run]] && r.Step.Name == "tests"
	c.mu.Unlock()
	if fail {
		return machine.CheckResult{Passed: false, Reason: "the tests step is wrong"}, nil
	}
	return machine.CheckResult{Passed: true}, nil
}

// ---- the rig ----

type trRig struct {
	*runsRig
	dir     string
	ws      *Workers
	ctl     *trCtl
	running *sync.WaitGroup
}

// newTRRig is an engine over a durable test service with S-1 approved (and the slices the
// analyst results after the first make, unapproved), its workers registered for nobody yet.
func newTRRig(t *testing.T, more ...spResult) *trRig {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir, spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	sp := newSP(t, append([]spResult{{p: spNotes(t)}}, more...), st,
		func(c *slicesvc.Config) { c.Records = st }) // the store holds slices/S-1/ from the slice's creation, as in service
	sp.approveAfterSeed(t)
	x := &trRig{dir: dir, ws: NewWorkers(), running: &sync.WaitGroup{},
		ctl: &trCtl{gates: map[string]chan struct{}{}, seen: map[string]bool{}, failing: map[string]bool{}, project: map[string]string{}},
		runsRig: &runsRig{t: t, sp: sp, st: st, projects: t.TempDir(),
			w: &fakeStepWorker{st: st, data: map[string][]byte{}}, c: &fakeStepChecker{}}}
	x.w.set(x.ctl.work(x.w))
	x.c.set(x.ctl.check)
	ctx, cancel := context.WithCancel(context.Background())
	x.r = newRig(t, func(cfg *Config) {
		cfg.Context, cfg.Running = ctx, x.running
		cfg.Slices, cfg.Store = sp.svc, st
		cfg.WorkerGrants = stepGrants("S-1")
		cfg.Projects = x.projects
		cfg.Model = "test-model"
		cfg.StepWorker, cfg.StepChecker = x.w, x.c
		cfg.Workers = x.ws
	})
	stopEngineAtEnd(t, x.r, cancel)
	t.Cleanup(func() { // the jobs of sets still open end with the engine, before the folders go
		x.ctl.releaseAll()
		cancel()
		waitReturns(t, x.running)
	})
	return x
}

// registerProject registers every declared step's worker of S-1 for the project.
func (x *trRig) registerProject(project string) {
	x.t.Helper()
	for _, st := range declaredSteps {
		x.r.clock.Advance(1100 * time.Millisecond)
		body := `{"endpoint":"` + endpointFor(st) + `","project":"` + project + `"}`
		if w := x.r.do("POST", "/workers?api-version="+apiVersion, stepToken("S-1", st), body, nil); w.Code != 200 {
			x.t.Fatalf("register %s for %s: status %d: %s", st, project, w.Code, w.Body.String())
		}
	}
}

// registerSet registers the workers of <prefix>-1 to <prefix>-k.
func (x *trRig) registerSet(prefix string, k int) {
	x.t.Helper()
	for i := 1; i <= k; i++ {
		x.registerProject(fmt.Sprintf("%s-%d", prefix, i))
	}
}

// unregister removes one step's registration of one project (its worker is gone).
func (x *trRig) unregister(project, step string) {
	x.ws.mu.Lock()
	defer x.ws.mu.Unlock()
	if _, ok := x.ws.m["S-1"][stepOf{step: step, project: project}]; !ok {
		x.t.Fatalf("no registration of %s for %s to remove", step, project)
	}
	delete(x.ws.m["S-1"], stepOf{step: step, project: project})
}

func trBody(prefix string, trials int) string {
	if trials < 0 {
		return `{"project_prefix":"` + prefix + `"}`
	}
	return fmt.Sprintf(`{"project_prefix":"%s","trials":%d}`, prefix, trials)
}

func (x *trRig) startSet(prefix string, trials int) reply {
	return x.do("POST", spURL("S-1", "/trial-sets"), trBody(prefix, trials))
}

var trSetIDRE = regexp.MustCompile(`^TS-[0-9]+$`)

// startSetOK accepts a set: 202, an operation of kind run-trials whose target is the set.
func (x *trRig) startSetOK(prefix string, trials int) spOp {
	x.t.Helper()
	p := x.startSet(prefix, trials)
	op := p.operation(x.t, http.StatusAccepted)
	if string(op.Kind) != "run-trials" || !trSetIDRE.MatchString(op.Target) {
		x.t.Fatalf("operation kind/target = %s/%s, want run-trials/TS-<n>", op.Kind, op.Target)
	}
	return spOp{ID: op.Id, Kind: string(op.Kind), Status: string(op.Status), Target: op.Target}
}

func (x *trRig) getSet(id string) trSetView {
	x.t.Helper()
	p := x.do("GET", apiPath("/trial-sets/"+id), "")
	if p.Code != 200 {
		x.t.Fatalf("getTrialSet %s = %d: %s", id, p.Code, p.Body.String())
	}
	var v trSetView
	if err := json.Unmarshal(p.Body.Bytes(), &v); err != nil {
		x.t.Fatal(err)
	}
	return v
}

// waitSet polls getTrialSet until cond holds, for at most 30 seconds.
func (x *trRig) waitSet(id, what string, cond func(trSetView) bool) trSetView {
	x.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		v := x.getSet(id)
		if cond(v) {
			return v
		}
		if time.Now().After(deadline) {
			x.t.Fatalf("set %s is %q (stopped %q, %d runs) after 30 s, want %s", id, v.State, v.StoppedBecause, len(v.Runs), what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (x *trRig) waitState(id, state string) trSetView {
	x.t.Helper()
	return x.waitSet(id, state, func(v trSetView) bool { return v.State == state })
}

func (x *trRig) waitStopped(id, because string) trSetView {
	x.t.Helper()
	v := x.waitState(id, "stopped")
	if v.StoppedBecause != because {
		x.t.Fatalf("set %s stopped because %q, want %q", id, v.StoppedBecause, because)
	}
	return v
}

func (x *trRig) waitRuns(id string, n int) trSetView {
	x.t.Helper()
	return x.waitSet(id, fmt.Sprintf("%d runs", n), func(v trSetView) bool { return len(v.Runs) >= n })
}

func (x *trRig) trialRun(id string) trRunView {
	x.t.Helper()
	p := x.do("GET", apiPath("/runs/"+id), "")
	if p.Code != 200 {
		x.t.Fatalf("getRun %s = %d: %s", id, p.Code, p.Body.String())
	}
	var v trRunView
	if err := json.Unmarshal(p.Body.Bytes(), &v); err != nil {
		x.t.Fatal(err)
	}
	return v
}

// trialRunOrNone reads a run, answering ok=false while the run does not exist yet: the set lists
// a trial's run as soon as it has chosen the run's id, a moment before the run is recorded.
func (x *trRig) trialRunOrNone(id string) (trRunView, bool) {
	x.t.Helper()
	if p := x.do("GET", apiPath("/runs/"+id), ""); p.Code == 404 {
		return trRunView{}, false
	}
	return x.trialRun(id), true
}

func (x *trRig) waitRunEnded(id string) trRunView {
	x.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if v, ok := x.trialRunOrNone(id); ok && v.Status == "ended" {
			return v
		} else if !ok && time.Now().Before(deadline) {
			// not recorded yet
		} else if time.Now().After(deadline) {
			x.t.Fatalf("run %s is %q after 30 s, want ended", id, v.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (x *trRig) waitRunStatus(id, status string) trRunView {
	x.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if v, ok := x.trialRunOrNone(id); ok && v.Status == status {
			return v
		} else if !ok && time.Now().Before(deadline) {
			// not recorded yet
		} else if time.Now().After(deadline) {
			x.t.Fatalf("run %s is %q after 30 s, want %s", id, v.Status, status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitRunLeftRunning waits until the run is waiting for the developer or has ended, both stable
// once reached by a run whose tests fail; it never depends on seeing the transient wait.
func (x *trRig) waitRunLeftRunning(id string) trRunView {
	x.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if v, ok := x.trialRunOrNone(id); ok && (v.Status == "waiting-for-developer" || v.Status == "ended") {
			return v
		} else if !ok && time.Now().Before(deadline) {
			// not recorded yet
		} else if time.Now().After(deadline) {
			x.t.Fatalf("run %s is %q after 30 s, want waiting-for-developer or ended", id, v.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (x *trRig) trialEvents(run string) []trEvent {
	x.t.Helper()
	var all []trEvent
	q := ""
	for range 20 {
		p := x.do("GET", apiPath("/runs/"+run+"/events"+q), "")
		if p.Code != 200 {
			x.t.Fatalf("listRunEvents %s = %d: %s", run, p.Code, p.Body.String())
		}
		var pg struct {
			Items []trEvent `json:"items"`
			Next  *string   `json:"next_page_token"`
		}
		if err := json.Unmarshal(p.Body.Bytes(), &pg); err != nil {
			x.t.Fatal(err)
		}
		all = append(all, pg.Items...)
		if pg.Next == nil {
			return all
		}
		q = "&page_token=" + url.QueryEscape(*pg.Next)
	}
	x.t.Fatal("listRunEvents did not end after 20 pages")
	return nil
}

func trAt(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("event time %q: %v", s, err)
	}
	return at
}

func trKinds(evs []trEvent) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.Kind)
	}
	return out
}

// settleOp follows an operation until it has ended, for at most 30 seconds.
func (x *trRig) settleOp(id string) spOp {
	x.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		p := x.do("GET", apiPath("/operations/"+id), "")
		var op spOp
		if p.Code != 200 || json.Unmarshal(p.Body.Bytes(), &op) != nil {
			x.t.Fatalf("getOperation %s = %d: %s", id, p.Code, p.Body.String())
		}
		switch op.Status {
		case "succeeded", "failed", "cancelled":
			return op
		}
		if time.Now().After(deadline) {
			x.t.Fatalf("operation %s is still %q after 30 s", id, op.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// settled waits for the set to end, its operation to succeed, and returns the set.
func (x *trRig) settled(op spOp, state string) trSetView {
	x.t.Helper()
	v := x.waitState(op.Target, state)
	if end := x.settleOp(op.ID); end.Status != "succeeded" || end.Result["trial_set"] != op.Target {
		x.t.Errorf("operation of the %s set %s = %+v, want succeeded with result trial_set %s", state, op.Target, end, op.Target)
	}
	if v.EndedAt == nil {
		x.t.Errorf("set %s is %s and has no ended_at", v.ID, v.State)
	}
	return v
}

func (x *trRig) outcomeOf(run string) string {
	v := x.trialRun(run)
	if v.Outcome == nil {
		return ""
	}
	return *v.Outcome
}

func (x *trRig) outcomes(v trSetView) []string {
	var out []string
	for _, r := range v.Runs {
		out = append(out, x.outcomeOf(r))
	}
	return out
}

// nothingRecorded checks that a refused start left no operation, run, set file or worker call.
func (x *trRig) nothingRecorded(what string, calls int) {
	x.t.Helper()
	x.noMoreOperations("OP-1")
	if n := len(x.listRuns("").Items); n != 0 {
		x.t.Errorf("%s: %d runs recorded, want none", what, n)
	}
	if n := x.ctl.callCount(); n != calls {
		x.t.Errorf("%s: %d worker calls, want %d", what, n, calls)
	}
	if names := storeNamesWith(x.t, x.dir, "TS-"); len(names) != 0 {
		x.t.Errorf("%s: the store holds %v, want no trial set", what, names)
	}
}

func (c *trCtl) callCount() int { c.mu.Lock(); defer c.mu.Unlock(); return c.calls }

// storeNamesWith lists the paths under dir whose name has the substring.
func storeNamesWith(t *testing.T, dir, sub string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(dir, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(filepath.Base(p), sub) {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// storeTree is every file under dir with its content, to see that a read changed nothing.
func storeTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.Mode().IsRegular() {
			b, err := os.ReadFile(filepath.Clean(p)) //nolint:gosec // test reads its own temp store
			if err != nil {
				return err
			}
			out[p] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// patternRunCostUSD is `budgets.run.cost_usd` of the base pattern (read, never a literal).
func patternRunCostUSD(t *testing.T) float64 {
	t.Helper()
	var p struct {
		Budgets struct {
			Run struct {
				CostUSD float64 `yaml:"cost_usd"`
			} `yaml:"run"`
		} `yaml:"budgets"`
	}
	if err := yaml.Unmarshal(spPattern(t), &p); err != nil || p.Budgets.Run.CostUSD <= 0 {
		t.Fatalf("the pattern's budgets.run.cost_usd: %v, %v", p.Budgets.Run.CostUSD, err)
	}
	return p.Budgets.Run.CostUSD
}

// setCostUSD is the sum of the usage of every attempt recorded on the set's runs.
func (x *trRig) setCostUSD(v trSetView) float64 {
	x.t.Helper()
	var sum float64
	for _, r := range v.Runs {
		for _, e := range x.trialEvents(r) {
			if e.Kind == "AttemptMade" && e.Usage != nil {
				sum += e.Usage.CostUSD
			}
		}
	}
	return sum
}

func sameStrings(got, want []string) bool { return slices.Equal(got, want) }

// ---- the request: what is refused ----

func TestTSBadRequestsAreInvalidRequestAndRecordNothing(t *testing.T) { // T007: 400, nothing recorded
	x := newTRRig(t)
	x.registerSet("trial", 6)
	calls := x.ctl.callCount()
	tooMany := contractLimit(t, "trials-max") + 1
	long := strings.Repeat("a", 62)
	cases := map[string]string{
		"trials 0":           trBody("trial", 0),
		"trials limit+1":     trBody("trial", tooMany),
		"trials negative":    `{"project_prefix":"trial","trials":-1}`,
		"trials fractional":  `{"project_prefix":"trial","trials":2.5}`,
		"trials a string":    `{"project_prefix":"trial","trials":"3"}`,
		"no prefix":          `{"trials":3}`,
		"empty prefix":       trBody("", 3),
		"upper-case prefix":  trBody("Trial", 3),
		"leading hyphen":     trBody("-trial", 3),
		"62 characters":      trBody(long, 3),
		"underscore":         trBody("my_trial", 3),
		"slash":              trBody("a/b", 3),
		"space":              trBody("my trial", 3),
		"an extra field":     `{"project_prefix":"trial","trials":3,"extra":1}`,
		"an empty body":      `{}`,
		"project_prefix num": `{"project_prefix":7,"trials":3}`,
	}
	for name, body := range cases {
		x.do("POST", spURL("S-1", "/trial-sets"), body).problem(t, 400, "InvalidRequest")
		x.nothingRecorded(name, calls)
	}
	// InvalidRequest comes before NotFound.
	x.do("POST", spURL("S-99", "/trial-sets"), trBody("trial", 0)).problem(t, 400, "InvalidRequest")
	x.do("POST", spURL("S-99", "/trial-sets"), trBody("trial", 3)).problem(t, 404, "NotFound")
	x.nothingRecorded("unknown slice", calls)
}

func TestTSOnlyAClientTokenMayStartOrReadASet(t *testing.T) { // Principle V; every call is the client's
	x := newTRRig(t)
	x.registerSet("trial", 3)
	for name, tok := range map[string]string{"worker": stepToken("S-1", "generate"), "page": pageToken, "none": ""} {
		want := 403
		if tok == "" {
			want = 401
		}
		if p := x.r.do("POST", spURL("S-1", "/trial-sets"), tok, trBody("trial", 3), nil); p.Code != want {
			t.Errorf("startTrialSet with a %s token = %d, want %d: %s", name, p.Code, want, p.Body.String())
		}
		x.r.clock.Advance(80 * time.Millisecond)
	}
	x.nothingRecorded("a start with another token", 0)
}

func TestTSRefusalsComeInTheContractsOrderAndStartNothing(t *testing.T) { // T007: data model order, at once
	x := newTRRig(t, spResult{p: srBadContract(t)})
	x.sp.seed(t, "A bad contract.") // S-2: unapproved, and it does not fit the base pattern
	prefix := "trial"
	start := func(slice string) reply {
		return x.do("POST", spURL(slice, "/trial-sets"), trBody(prefix, 3))
	}
	check := func(what string, p reply, status int, code string) problemView {
		t.Helper()
		pv := decodeProblem2(t, p, what, status, code)
		x.nothingRecorded(what, 0)
		return pv
	}
	// NotFound; then PatternDoesNotFit before NotApproved (S-2 has neither workers nor approval).
	check("unknown slice", start("S-99"), 404, "NotFound")
	check("S-2 does not fit", start("S-2"), 409, "PatternDoesNotFit")
	// NotApproved before WorkersNotReady: no worker is registered yet.
	sl, err := x.sp.svc.Get("S-1")
	if err != nil {
		t.Fatal(err)
	}
	goals := sl.Goals
	goals[0].Text = "Keep a note for good"
	if _, err := x.sp.svc.ReplaceGoals(context.Background(), "S-1", goals); err != nil { // withdraws the approval
		t.Fatal(err)
	}
	check("a part no longer approved", start("S-1"), 409, "NotApproved")
	x.sp.approveAll(t, "S-1")
	// WorkersNotReady names the first project, in trial order, and a step without a worker.
	pv := check("no workers", start("S-1"), 409, "WorkersNotReady")
	if !strings.Contains(pv.Detail, prefix+"-1") || strings.Contains(pv.Detail, prefix+"-2") {
		t.Errorf("WorkersNotReady names %q, want the first project %s-1 only", pv.Detail, prefix)
	}
	x.registerProject(prefix + "-1")
	x.registerProject(prefix + "-2")
	pv = check("trial 3 has no workers", start("S-1"), 409, "WorkersNotReady")
	if !strings.Contains(pv.Detail, prefix+"-3") || !anyStepNamed(pv.Detail) {
		t.Errorf("WorkersNotReady names %q, want the project %s-3 and a step", pv.Detail, prefix)
	}
	x.registerProject(prefix + "-3")
	// WorkersNotReady before ProjectExists; ProjectExists names the first existing folder.
	for _, d := range []string{prefix + "-3", prefix + "-2"} { // a folder holding a file is taken; an empty one is free
		if err := os.Mkdir(filepath.Join(x.projects, d), 0o755); err != nil { //nolint:gosec // a project folder as build-workers makes it
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(x.projects, d, "main.go"), []byte("package main\n"), 0o644); err != nil { //nolint:gosec // a project file as a build makes it
			t.Fatal(err)
		}
	}
	pv = check("existing folders", start("S-1"), 409, "ProjectExists")
	if !strings.Contains(pv.Detail, prefix+"-2") || strings.Contains(pv.Detail, prefix+"-3") {
		t.Errorf("ProjectExists names %q, want the first existing folder %s-2", pv.Detail, prefix)
	}
	for _, d := range []string{prefix + "-3", prefix + "-2"} {
		if err := os.RemoveAll(filepath.Join(x.projects, d)); err != nil {
			t.Fatal(err)
		}
	}
	// The same body, with nothing in the way, is accepted: every refusal above left the slice free.
	op := x.startSetOK(prefix, 3)
	x.settled(op, "ended")
}

type problemView struct{ Code, Detail string }

func decodeProblem2(t *testing.T, p reply, what string, status int, code string) problemView {
	t.Helper()
	if p.Code != status {
		t.Fatalf("%s: status = %d, want %d; body: %s", what, p.Code, status, p.Body.String())
	}
	var v problemView
	var raw struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(p.Body.Bytes(), &raw); err != nil {
		t.Fatalf("%s: %v: %s", what, err, p.Body.String())
	}
	if raw.Code != code {
		t.Fatalf("%s: code = %q, want %q: %s", what, raw.Code, code, p.Body.String())
	}
	v.Code, v.Detail = raw.Code, raw.Detail
	return v
}

func anyStepNamed(s string) bool {
	for _, st := range declaredSteps {
		if strings.Contains(s, st) {
			return true
		}
	}
	return false
}

// ---- running: order, projects, reuse ----

func TestTSTrialsRunOneAfterAnotherIntoTheirOwnProjectsReusingNothing(t *testing.T) { // T007, SC-004, FR-002
	x := newTRRig(t)
	x.registerProject("notes-app")
	earlier := x.runDone("notes-app") // an earlier run of the slice: its results are kept
	if earlier.ID != "RUN-1" {
		t.Fatalf("the earlier run is %s, want RUN-1", earlier.ID)
	}
	k := contractLimit(t, "trials-default")
	x.registerSet("trial", k)
	rel1, rel2 := x.ctl.hold("trial-1"), x.ctl.hold("trial-2")
	op := x.startSetOK("trial", -1) // no `trials`: the default
	set := x.waitRuns(op.Target, 1)
	if set.State != "running" || set.Trials != k || set.ProjectPrefix != "trial" || set.Operation != op.ID || set.Slice != "S-1" {
		t.Fatalf("the set while its first trial runs: %+v", set)
	}
	if set.Budget.CostUSD != float64(contractLimit(t, "trial-set-cost-usd")) || set.Budget.Hours != contractLimit(t, "trial-set-hours") {
		t.Errorf("budget = %+v, want the contract's x-limits trial-set-cost-usd and trial-set-hours", set.Budget)
	}
	x.waitRunStatus(set.Runs[0], "running")
	time.Sleep(200 * time.Millisecond) // a second trial would have started by now
	if got := x.waitRuns(op.Target, 1).Runs; len(got) != 1 || len(x.listRuns("").Items) != 2 {
		t.Fatalf("while trial 1 runs: set runs %v, %d runs of the slice, want one trial and the earlier run", got, len(x.listRuns("").Items))
	}
	if sp := x.ctl.startedProjects(); !sameStrings(sp, []string{"notes-app", "trial-1"}) {
		t.Fatalf("projects that had a worker call: %v, want notes-app, trial-1 (trial 2 only after trial 1 ended)", sp)
	}
	rel1()
	end1 := x.waitRunEnded(set.Runs[0])
	set = x.waitRuns(op.Target, 2)
	x.waitRunStatus(set.Runs[1], "running")
	time.Sleep(100 * time.Millisecond)
	if len(x.getSet(op.Target).Runs) != 2 {
		t.Fatalf("a third trial started while trial 2 runs")
	}
	rel2()
	v := x.settled(op, "ended")
	if len(v.Runs) != k {
		t.Fatalf("runs = %v, want %d trials", v.Runs, k)
	}
	if sp := x.ctl.startedProjects(); !sameStrings(sp, []string{"notes-app", "trial-1", "trial-2", "trial-3"}) {
		t.Errorf("trials' worker calls started in the order %v", sp)
	}
	if v.Result.Ended != k || v.Result.Done != k || v.Result.Unstable || len(v.Result.Outcomes) != k {
		t.Errorf("result = %+v, want %d ended, %d done, not unstable, one outcome per trial", v.Result, k, k)
	}
	var prevEnd time.Time
	for i, run := range v.Runs {
		n := i + 1
		rv := x.trialRun(run)
		if rv.Project != fmt.Sprintf("trial-%d", n) || rv.TrialSet != op.Target || rv.Trial != n || rv.Status != "ended" || x.outcomeOf(run) != "done" {
			t.Errorf("trial %d's run = %+v, want project trial-%d, trial_set %s, trial %d, ended done", n, rv, n, op.Target, n)
		}
		o := v.Result.Outcomes[i]
		if o.Trial != n || o.Run != run || o.Outcome == nil || *o.Outcome != "done" {
			t.Errorf("outcome %d = %+v, want trial %d, run %s, done", i, o, n, run)
		}
		evs := x.trialEvents(run)
		first := evs[0]
		if first.Kind != "RunStarted" || first.TrialSet != op.Target || first.Trial != n {
			t.Errorf("trial %d's RunStarted = %+v, want trial_set %s and trial %d", n, first, op.Target, n)
		}
		for _, e := range evs[1:] {
			if e.TrialSet != "" || e.Trial != 0 {
				t.Errorf("trial %d: %s carries trial_set or trial; only RunStarted does", n, e.Kind)
			}
			if e.Kind == "ResultReused" {
				t.Errorf("trial %d reused a kept result although it must reuse nothing (SC-004): %v", n, trKinds(evs))
			}
		}
		if !slices.Contains(trKinds(evs), "StepAccepted") || evs[len(evs)-1].Kind != "RunCompleted" {
			t.Errorf("trial %d's record %v: want every step run and RunCompleted", n, trKinds(evs))
		}
		if i > 0 && trAt(t, first.At).Before(prevEnd) {
			t.Errorf("trial %d started at %s, before trial %d ended at %s", n, first.At, n-1, prevEnd)
		}
		prevEnd = trAt(t, evs[len(evs)-1].At)
	}
	if end1.TrialSet != op.Target || end1.Trial != 1 {
		t.Errorf("getRun of trial 1 after its end = %+v, want trial_set and trial kept", end1)
	}
	// An ordinary run is not a trial.
	if ev := x.trialEvents("RUN-1")[0]; ev.TrialSet != "" || ev.Trial != 0 {
		t.Errorf("the earlier ordinary run carries trial_set or trial: %+v", ev)
	}
	if rv := x.trialRun("RUN-1"); rv.TrialSet != "" || rv.Trial != 0 {
		t.Errorf("getRun of the ordinary run carries trial_set or trial: %+v", rv)
	}
}

func TestTSTheMostTrialsInTheLongestPrefixAllRunAndTheCostRuleAtItsBoundaryStillStarts(t *testing.T) { // T007: limits; the budget rule is "at most"
	x := newTRRig(t)
	k := contractLimit(t, "trials-max")
	prefix := strings.Repeat("p", 61)
	x.registerSet(prefix, k)
	// Four trials of $0.375 cost $1.50 and a pattern run budget of $0.50 makes exactly the set's
	// $2: the fifth is within its budget and starts.
	x.ctl.setCost(0.375)
	budget, run := float64(contractLimit(t, "trial-set-cost-usd")), patternRunCostUSD(t)
	if float64(k-1)*0.375+run != budget {
		t.Fatalf("the contract's limits moved: this case needs (trials-max-1) x 0.375 + the run budget to equal the set's budget")
	}
	op := x.startSetOK(prefix, k)
	v := x.settled(op, "ended")
	if len(v.Runs) != k || v.Result.Ended != k || v.StoppedBecause != "" {
		t.Fatalf("set = %d runs, %d ended, stopped %q; want all %d trials and no stop (the cost rule is 'at most')", len(v.Runs), v.Result.Ended, v.StoppedBecause, k)
	}
	if c := x.setCostUSD(v); c > budget+1e-9 {
		t.Errorf("the set's cost %.3f passed its budget %.2f", c, budget)
	}
	for i, r := range v.Runs {
		if p := x.trialRun(r).Project; p != fmt.Sprintf("%s-%d", prefix, i+1) {
			t.Errorf("trial %d's project = %q", i+1, p)
		}
	}
}

func TestTSAnotherActiveRunMakesTheSetWaitAndNotFail(t *testing.T) { // T007: wait, not fail
	x := newTRRig(t)
	x.registerProject("notes-app")
	x.registerSet("trial", 2)
	rel := x.ctl.hold("notes-app")
	ordinary := x.startOK("notes-app")
	x.waitRunStatus(ordinary.Target, "running")
	op := x.startSetOK("trial", 2) // not RunActive: the set waits for the run
	time.Sleep(300 * time.Millisecond)
	v := x.getSet(op.Target)
	if v.State != "running" || len(v.Runs) != 0 {
		t.Fatalf("while another run is active the set is %+v, want running with no trial", v)
	}
	if sp := x.ctl.startedProjects(); !sameStrings(sp, []string{"notes-app"}) {
		t.Fatalf("a trial started while an ordinary run of the slice runs: %v", sp)
	}
	if end := x.do("GET", apiPath("/operations/"+op.ID), ""); end.Code != 200 || !strings.Contains(end.Body.String(), `"running"`) {
		t.Errorf("the set's operation while it waits: %d %s, want running", end.Code, end.Body.String())
	}
	rel()
	x.waitRunEnded(ordinary.Target)
	v = x.settled(op, "ended")
	if len(v.Runs) != 2 || v.Result.Done != 2 {
		t.Errorf("after the other run ended the set = %+v, want both trials done", v)
	}
}

// ---- a trial that waits for the developer ----

func TestTSATrialThatWaitsForTheDeveloperIsHandedOverBySetAndTheNextStarts(t *testing.T) { // T007, R4
	x := newTRRig(t)
	x.registerSet("trial", 3)
	x.ctl.failTests("trial-2")
	op := x.startSetOK("trial", 3)
	v := x.settled(op, "ended")
	if len(v.Runs) != 3 {
		t.Fatalf("runs = %v, want 3 trials: the set hands the waiting trial over and goes on", v.Runs)
	}
	if got := x.outcomes(v); !sameStrings(got, []string{"done", "handed-over", "done"}) {
		t.Fatalf("outcomes = %v, want done, handed-over, done", got)
	}
	if v.Result.Ended != 3 || v.Result.Done != 2 || !v.Result.Unstable {
		t.Errorf("result = %+v, want done 2 of 3 ended, unstable (SC-001)", v.Result)
	}
	evs := x.trialEvents(v.Runs[1])
	last := evs[len(evs)-1]
	if last.Kind != "HandOverAccepted" || !strings.Contains(last.Detail, op.Target) {
		t.Errorf("the handed-over trial ends with %s %q, want HandOverAccepted whose detail names %s", last.Kind, last.Detail, op.Target)
	}
	if third := x.trialEvents(v.Runs[2])[0]; trAt(t, third.At).Before(trAt(t, last.At)) {
		t.Errorf("trial 3 started at %s, before trial 2's hand-over at %s", third.At, last.At)
	}
}

func TestTSATrialThatLeavesWaitingBeforeTheSignalDoesNotFailTheSet(t *testing.T) { // T007: RunNotWaiting, read again
	x := newTRRig(t)
	x.registerSet("trial", 2)
	x.ctl.failTests("trial-1")
	op := x.startSetOK("trial", 2)
	set := x.waitRuns(op.Target, 1)
	// The set hands the run over as soon as its record shows it waiting, so "waiting" may pass
	// unseen: wait for the run to have reached the wait (waiting or already handed over), which
	// is stable, never for the transient state itself.
	x.waitRunLeftRunning(set.Runs[0])
	// The developer's own hand-over races the set's: whichever comes second finds the run not waiting.
	p := x.do("POST", apiPath("/runs/"+set.Runs[0]+"/hand-over"), "")
	switch p.Code {
	case 202:
	case 409:
		p.problem(t, 409, "RunNotWaiting")
	default:
		t.Fatalf("the developer's hand-over = %d: %s", p.Code, p.Body.String())
	}
	v := x.settled(op, "ended")
	if len(v.Runs) != 2 || x.outcomeOf(v.Runs[0]) != "handed-over" || x.outcomeOf(v.Runs[1]) != "done" {
		t.Errorf("set = %+v, outcomes %v, want trial 1 handed-over and trial 2 done; the set did not fail", v, x.outcomes(v))
	}
}

// ---- budgets ----

func TestTSASetWhoseCostSoFarPlusARunBudgetWouldPassItsBudgetStopsBudget(t *testing.T) { // T007, SC-006, R5
	x := newTRRig(t)
	x.registerSet("trial", 5)
	per := 0.45
	x.ctl.setCost(per)
	budget, run := float64(contractLimit(t, "trial-set-cost-usd")), patternRunCostUSD(t)
	want := 0
	for cost := 0.0; want < 5 && cost+run <= budget; want++ {
		cost += per
	}
	if want == 0 || want == 5 {
		t.Fatalf("the contract's limits moved: %d of 5 trials fit; the case needs the rule to stop a set midway", want)
	}
	op := x.startSetOK("trial", 5)
	v := x.settled(op, "stopped")
	if v.StoppedBecause != "budget" {
		t.Fatalf("stopped because %q, want budget", v.StoppedBecause)
	}
	if len(v.Runs) != want || v.Result.Ended != want {
		t.Fatalf("%d trials started and %d ended, want %d: the next would pass the budget, so none starts", len(v.Runs), v.Result.Ended, want)
	}
	if c := x.setCostUSD(v); c > budget {
		t.Errorf("the set's cost %.2f passed its budget %.2f (SC-006)", c, budget)
	}
	if sp := x.ctl.startedProjects(); len(sp) != want {
		t.Errorf("trials with a worker call: %v, want %d", sp, want)
	}
	if got := x.listRuns("").Items; len(got) != want {
		t.Errorf("%d runs of the slice, want %d", len(got), want)
	}
}

func TestTSAfterTheHoursTheRunningTrialIsCancelledAndTheSetStopsTime(t *testing.T) { // T007: 24 hours, fake clock
	x := newTRRig(t)
	x.registerSet("trial", 3)
	x.ctl.hold("trial-1") // never released: the trial is running when the hours pass
	op := x.startSetOK("trial", 3)
	set := x.waitRuns(op.Target, 1)
	x.waitRunStatus(set.Runs[0], "running")
	x.r.clock.Advance(time.Duration(contractLimit(t, "trial-set-hours"))*time.Hour + time.Minute)
	v := x.waitStopped(op.Target, "time")
	if len(v.Runs) != 1 || x.waitRunEnded(v.Runs[0]).Status != "ended" || x.outcomeOf(v.Runs[0]) != "cancelled" {
		t.Errorf("after the hours: runs %v, outcomes %v; want the one running trial cancelled and no other started", v.Runs, x.outcomes(v))
	}
	if end := x.settleOp(op.ID); end.Status != "succeeded" {
		t.Errorf("the operation of a set stopped `time` = %+v, want succeeded", end)
	}
	if v.Result.Ended != 1 || v.Result.Done != 0 {
		t.Errorf("result = %+v, want the cancelled trial counted as ended and not done", v.Result)
	}
}

func TestTSASetThatWaitsForAnotherRunStopsTimeWhenTheHoursPass(t *testing.T) { // T007: a wait has a timeout (Principle X)
	x := newTRRig(t)
	x.registerProject("notes-app")
	x.registerSet("trial", 2)
	x.ctl.hold("notes-app")
	x.startOK("notes-app")
	op := x.startSetOK("trial", 2)
	time.Sleep(200 * time.Millisecond)
	x.r.clock.Advance(time.Duration(contractLimit(t, "trial-set-hours"))*time.Hour + time.Minute)
	v := x.waitStopped(op.Target, "time")
	if len(v.Runs) != 0 || v.Result.Ended != 0 || v.EndedAt == nil {
		t.Errorf("set = %+v, want stopped `time` with no trial", v)
	}
	if end := x.settleOp(op.ID); end.Status != "succeeded" {
		t.Errorf("operation = %+v, want succeeded", end)
	}
}

// ---- the slice changes ----

func (x *trRig) editGoals(text string) {
	x.t.Helper()
	sl, err := x.sp.svc.Get("S-1")
	if err != nil {
		x.t.Fatal(err)
	}
	goals := sl.Goals
	goals[0].Text = text
	if _, err := x.sp.svc.ReplaceGoals(context.Background(), "S-1", goals); err != nil {
		x.t.Fatal(err)
	}
}

func TestTSTheSliceLosingAnApprovalBetweenTrialsStopsSliceChanged(t *testing.T) { // T007
	x := newTRRig(t)
	x.registerSet("trial", 3)
	rel := x.ctl.hold("trial-1")
	op := x.startSetOK("trial", 3)
	set := x.waitRuns(op.Target, 1)
	x.waitRunStatus(set.Runs[0], "running")
	x.editGoals("Keep a note for good") // the goals are not approved any more
	rel()
	v := x.waitStopped(op.Target, "slice-changed")
	if len(v.Runs) != 1 || x.waitRunEnded(v.Runs[0]).Status != "ended" {
		t.Errorf("runs = %v, want only the trial that had started, ended: no trial starts on a part that is not approved", v.Runs)
	}
	if end := x.settleOp(op.ID); end.Status != "succeeded" {
		t.Errorf("operation = %+v, want succeeded", end)
	}
}

func TestTSAPartEditedAndApprovedAgainBetweenTrialsStopsSliceChanged(t *testing.T) { // T007, F1: part_hashes
	x := newTRRig(t)
	x.registerSet("trial", 3)
	before, err := x.sp.svc.Get("S-1")
	if err != nil {
		t.Fatal(err)
	}
	rel := x.ctl.hold("trial-1")
	op := x.startSetOK("trial", 3)
	set := x.waitRuns(op.Target, 1)
	if set.PartHashes["goals"] != before.Parts.Goals || set.PartHashes["rules"] != before.Parts.Rules || set.PartHashes["contract"] != before.Parts.Contract {
		t.Fatalf("part_hashes = %v, want the approved parts' hashes at acceptance (%s, %s, %s)", set.PartHashes, before.Parts.Goals, before.Parts.Rules, before.Parts.Contract)
	}
	x.waitRunStatus(set.Runs[0], "running")
	x.editGoals("Keep a note for good")
	after := x.sp.approveAll(t, "S-1") // every part approved again, the goals at a new hash
	if after.Parts.Goals == before.Parts.Goals {
		t.Fatal("the edit did not change the goals' hash")
	}
	rel()
	v := x.waitStopped(op.Target, "slice-changed")
	if len(v.Runs) != 1 {
		t.Errorf("runs = %v, want one: a part approved again at another hash differs from the set's part_hashes", v.Runs)
	}
	if v.PartHashes["goals"] != before.Parts.Goals {
		t.Errorf("part_hashes of the stopped set = %v, want those at acceptance", v.PartHashes)
	}
}

func TestTSTheNextProjectsWorkersGoneStopsWorkers(t *testing.T) { // T007
	x := newTRRig(t)
	x.registerSet("trial", 3)
	rel := x.ctl.hold("trial-1")
	op := x.startSetOK("trial", 3)
	set := x.waitRuns(op.Target, 1)
	x.waitRunStatus(set.Runs[0], "running")
	x.unregister("trial-2", "code")
	rel()
	v := x.waitStopped(op.Target, "workers")
	if len(v.Runs) != 1 {
		t.Errorf("runs = %v, want only trial 1: the next project has no worker for a step", v.Runs)
	}
	if end := x.settleOp(op.ID); end.Status != "succeeded" {
		t.Errorf("operation = %+v, want succeeded", end)
	}
	if got := x.ctl.startedProjects(); !sameStrings(got, []string{"trial-1"}) {
		t.Errorf("projects with a worker call: %v, want trial-1 only", got)
	}
}

// ---- cancelling ----

func TestTSCancellingTheOperationCancelsTheRunningTrialStartsNoOtherAndEndsTheSetCancelled(t *testing.T) { // T007
	x := newTRRig(t)
	x.registerSet("trial", 3)
	x.ctl.hold("trial-1")
	op := x.startSetOK("trial", 3)
	set := x.waitRuns(op.Target, 1)
	x.waitRunStatus(set.Runs[0], "running")
	x.do("POST", apiPath("/operations/"+op.ID+"/cancel"), "").operation(t, 202)
	v := x.waitState(op.Target, "cancelled")
	if end := x.settleOp(op.ID); end.Status != "cancelled" {
		t.Errorf("operation = %+v, want cancelled", end)
	}
	if len(v.Runs) != 1 || x.waitRunEnded(v.Runs[0]).Status != "ended" || x.outcomeOf(v.Runs[0]) != "cancelled" {
		t.Errorf("runs %v, outcomes %v; want the running trial cancelled and no other started", v.Runs, x.outcomes(v))
	}
	if v.EndedAt == nil || v.StoppedBecause != "" {
		t.Errorf("a cancelled set has ended_at and no stopped_because: %+v", v)
	}
	time.Sleep(150 * time.Millisecond)
	if n := len(x.getSet(op.Target).Runs); n != 1 {
		t.Errorf("%d trials after the cancel, want 1", n)
	}
	// The slice is free again for another set.
	x.registerSet("again", 1)
	again := x.startSetOK("again", 1)
	x.settled(again, "ended")
}

// ---- one set at a time ----

func TestTSASecondSetOfTheSliceWhileOneRunsIsRunActiveAndADeleteIsRefused(t *testing.T) { // T007
	x := newTRRig(t)
	x.registerSet("trial", 2)
	x.registerSet("other", 2)
	rel := x.ctl.hold("trial-1")
	op := x.startSetOK("trial", 2)
	set := x.waitRuns(op.Target, 1)
	x.waitRunStatus(set.Runs[0], "running")
	x.do("POST", spURL("S-1", "/trial-sets"), trBody("other", 2)).problem(t, 409, "RunActive")
	x.do("DELETE", spURL("S-1", ""), "").problem(t, 409, "RunActive")
	// ProjectExists comes before RunActive.
	if err := os.Mkdir(filepath.Join(x.projects, "other-2"), 0o755); err != nil { //nolint:gosec // a project folder as build-workers makes it
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(x.projects, "other-2", "main.go"), []byte("package main\n"), 0o644); err != nil { //nolint:gosec // a project file; an empty folder is free
		t.Fatal(err)
	}
	x.do("POST", spURL("S-1", "/trial-sets"), trBody("other", 2)).problem(t, 409, "ProjectExists")
	rel()
	if got := x.settled(op, "ended").Runs; len(got) != 2 {
		t.Errorf("runs = %v after the refusals, want 2: a refused start changed nothing", got)
	}
	if n := len(x.listRuns("").Items); n != 2 {
		t.Errorf("%d runs of the slice, want 2", n)
	}
}

func TestTSARepeatedStartWithTheSameKeyIsTheSameSetAndAnotherBodyIsKeyReused(t *testing.T) { // contract: Idempotency-Key
	x := newTRRig(t)
	x.registerSet("trial", 3)
	rel := x.ctl.hold("trial-1")
	hdr := map[string]string{"Idempotency-Key": "set-key-1"}
	first := x.r.do("POST", spURL("S-1", "/trial-sets"), clientToken, trBody("trial", 2), hdr)
	op := first.operation(t, 202)
	x.r.clock.Advance(80 * time.Millisecond)
	again := x.r.do("POST", spURL("S-1", "/trial-sets"), clientToken, trBody("trial", 2), hdr)
	if got := again.operation(t, 202); got.Id != op.Id || got.Target != op.Target {
		t.Errorf("the same key and body gave operation %s/%s, want the first %s/%s", got.Id, got.Target, op.Id, op.Target)
	}
	x.r.clock.Advance(80 * time.Millisecond)
	x.r.do("POST", spURL("S-1", "/trial-sets"), clientToken, trBody("trial", 3), hdr).problem(t, 409, "IdempotencyKeyReused")
	rel()
	x.settled(spOp{ID: op.Id, Target: op.Target}, "ended")
	if n := len(x.listRuns("").Items); n != 2 {
		t.Errorf("%d runs, want the 2 trials of one set: a repeated key made no second set", n)
	}
}

// ---- reading ----

func TestTSReadingASetCallsNoModelAndChangesNoRecord(t *testing.T) { // T007, SC-005
	x := newTRRig(t)
	x.registerSet("trial", 2)
	op := x.startSetOK("trial", 2)
	v := x.settled(op, "ended")
	calls := x.ctl.callCount()
	before := storeTree(t, x.dir)
	for range 3 {
		if got := x.getSet(v.ID); !reflect.DeepEqual(got, v) {
			t.Errorf("a repeated read differs: %+v vs %+v", got, v)
		}
		if p := x.do("GET", spURL("S-1", "/trial-sets"), ""); p.Code != 200 {
			t.Fatalf("listTrialSets = %d: %s", p.Code, p.Body.String())
		}
	}
	if n := x.ctl.callCount(); n != calls {
		t.Errorf("reading made %d worker (model) calls", n-calls)
	}
	after := storeTree(t, x.dir)
	if !reflect.DeepEqual(before, after) {
		for p, c := range after {
			if before[p] != c {
				t.Errorf("reading changed or added %s", p)
			}
		}
		for p := range before {
			if _, ok := after[p]; !ok {
				t.Errorf("reading removed %s", p)
			}
		}
	}
	x.noMoreOperations("OP-" + fmt.Sprint(len(x.recordedOps())+1))
}

// recordedOps are the operations the engine holds, found by reading getOperation upwards.
func (x *trRig) recordedOps() []string {
	var out []string
	for n := 1; n < 50; n++ {
		id := fmt.Sprintf("OP-%d", n)
		if x.do("GET", apiPath("/operations/"+id), "").Code != 200 {
			break
		}
		out = append(out, id)
	}
	return out
}

func TestTSListIsNewestFirstWithTheSameFieldsAsGetAndUnknownOnesAre404(t *testing.T) { // T007, contract
	x := newTRRig(t)
	x.registerSet("first", 1)
	x.registerSet("second", 2)
	a := x.startSetOK("first", 1)
	x.settled(a, "ended")
	b := x.startSetOK("second", 2)
	x.settled(b, "ended")
	p := x.do("GET", spURL("S-1", "/trial-sets"), "")
	if p.Code != 200 {
		t.Fatalf("listTrialSets = %d: %s", p.Code, p.Body.String())
	}
	var pg struct {
		Items []trSetView `json:"items"`
		Next  *string     `json:"next_page_token"`
	}
	if err := json.Unmarshal(p.Body.Bytes(), &pg); err != nil {
		t.Fatal(err)
	}
	if len(pg.Items) != 2 || pg.Items[0].ID != b.Target || pg.Items[1].ID != a.Target || pg.Next != nil {
		t.Fatalf("list = %+v, want %s then %s (newest first), one page", pg.Items, b.Target, a.Target)
	}
	for _, it := range pg.Items {
		if want := x.getSet(it.ID); !reflect.DeepEqual(it, want) {
			t.Errorf("list item %s = %+v, want what getTrialSet answers: %+v", it.ID, it, want)
		}
	}
	x.do("GET", apiPath("/trial-sets/TS-99"), "").problem(t, 404, "NotFound")
	x.do("GET", spURL("S-99", "/trial-sets"), "").problem(t, 404, "NotFound")
}

func TestTSErasingTheSliceErasesItsSets(t *testing.T) { // T007, FR-011
	x := newTRRig(t)
	x.registerSet("trial", 2)
	op := x.startSetOK("trial", 2)
	v := x.settled(op, "ended")
	del := x.do("DELETE", spURL("S-1", ""), "").operation(t, 202)
	if end := x.settleOp(del.Id); end.Status != "succeeded" {
		t.Fatalf("deleteSlice = %+v", end)
	}
	x.do("GET", apiPath("/trial-sets/"+v.ID), "").problem(t, 404, "NotFound")
	x.do("GET", spURL("S-1", "/trial-sets"), "").problem(t, 404, "NotFound")
	if names := storeNamesWith(t, x.dir, v.ID); len(names) != 0 {
		t.Errorf("after the erasure the store still holds %v", names)
	}
}

// ---- the fake durable service: the job stopped and repeated, failed writes ----

// trStarted reads a field of RunStart by name, so this file compiles before the field exists.
func trStarted(s RunStart, field string) any {
	f := reflect.ValueOf(s).FieldByName(field)
	if !f.IsValid() {
		return nil
	}
	return f.Interface()
}

func (x *dopRig) startSet(prefix string, trials int) reply {
	return x.do("POST", spURL("S-1", "/trial-sets"), "", trBody(prefix, trials))
}

func (x *dopRig) getSet(id string) trSetView {
	x.t.Helper()
	p := x.get("/trial-sets/" + id + "?api-version=" + apiVersion)
	if p.Code != 200 {
		x.t.Fatalf("getTrialSet %s = %d: %s", id, p.Code, p.Body.String())
	}
	var v trSetView
	if err := json.Unmarshal(p.Body.Bytes(), &v); err != nil {
		x.t.Fatal(err)
	}
	return v
}

// waitStarts waits until the fake durable service has been asked to start n runs.
func (x *dopRig) waitStarts(n int) {
	x.t.Helper()
	for end := time.Now().Add(10 * time.Second); x.runs.startCount() < n; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(end) {
			x.t.Fatalf("%d runs started after 10 s, want %d", x.runs.startCount(), n)
		}
	}
}

// opsTargeting counts the operation records whose target is the run.
func (x *dopRig) opsTargeting(target string) int {
	n := 0
	for _, id := range x.records() {
		if x.record(id)["target"] == target {
			n++
		}
	}
	return n
}

func TestTSAFailedOperationWriteAtStartTrialSetIs503AndKeepsNothing(t *testing.T) { // T007, data model Write order 1
	x := newDopRig(t)
	x.seed(1, true)
	x.registerAll()
	opsDir := filepath.Join(x.dir, "operations")
	if err := os.RemoveAll(opsDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opsDir, nil, 0o600); err != nil { // no record can be created in a file
		t.Fatal(err)
	}
	dopUnavailable(t, x.do("POST", spURL("S-1", "/trial-sets"), "key-set", trBody("trial", 2)))
	if err := os.Remove(opsDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(opsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if names := storeNamesWith(t, x.dir, "TS-"); len(names) != 0 {
		t.Errorf("the store holds %v after the 503, want no set", names)
	}
	if files := x.opFiles(); len(files) != 0 || len(x.ops.allStarts()) != 0 || x.runs.startCount() != 0 {
		t.Errorf("after the 503: operation files %v, workflows %v, runs started %d; want nothing", files, x.ops.allStarts(), x.runs.startCount())
	}
	p := x.get("/slices/S-1/trial-sets?api-version=" + apiVersion)
	if p.Code != 200 || !strings.Contains(p.Body.String(), `"items":[]`) {
		t.Errorf("listTrialSets after the 503 = %d %s, want an empty page", p.Code, p.Body.String())
	}
	// The 503 was not kept under the key: the same request is judged again and accepted, and
	// nothing blocks the slice (no orphan set).
	x.ops.set(func(f *dopOps) { f.hold = true })
	if op := x.opView(x.do("POST", spURL("S-1", "/trial-sets"), "key-set", trBody("trial", 2)), 202); op.Kind != "run-trials" || !trSetIDRE.MatchString(op.Target) {
		t.Errorf("the resent request's operation = %+v, want run-trials on TS-<n>", op)
	}
}

func TestTSAFailedSetWriteAfterTheOperationsLeavesTheJobToWriteTheSetFromTheOperation(t *testing.T) { // T007, F1/N1
	x := newDopRig(t)
	x.seed(1, true)
	x.registerAll()
	x.ops.set(func(f *dopOps) { f.hold = true })
	acceptance, err := x.svc.Get("S-1")
	if err != nil {
		t.Fatal(err)
	}
	sliceDir := filepath.Join(x.dir, "slices", "S-1")
	if err := os.Chmod(sliceDir, 0o500); err != nil { //nolint:gosec // a folder needs its search bit; the set cannot be written into its slice's folder
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sliceDir, 0o700) }) //nolint:gosec // a folder needs its search bit
	p := x.startSet("trial", 2)
	opv := x.opView(p, 202)
	if opv.Kind != "run-trials" || !trSetIDRE.MatchString(opv.Target) {
		t.Fatalf("operation = %+v, want a 202 of kind run-trials on TS-<n>: the operation stands when the set's write fails", opv)
	}
	if err := os.Chmod(sliceDir, 0o700); err != nil { //nolint:gosec // a folder needs its search bit
		t.Fatal(err)
	}
	// Meanwhile the operation is open and the set is not written: both blocks hold.
	x.startSet("other", 2).problem(t, 409, "RunActive")
	x.do("DELETE", spURL("S-1", ""), "", "").problem(t, 409, "RunActive")
	// The slice is edited meanwhile: it loses an approval.
	goals := acceptance.Goals
	goals[0].Text = "Keep a note for good"
	if _, err := x.svc.ReplaceGoals(context.Background(), "S-1", goals); err != nil {
		t.Fatal(err)
	}
	x.ops.release(opv.ID)
	end := x.settle(opv.ID)
	if end.Status != "succeeded" {
		t.Fatalf("operation = %+v, want succeeded (a set stopped is a set that ended its work)", end)
	}
	v := x.getSet(opv.Target)
	if v.State != "stopped" || v.StoppedBecause != "slice-changed" || len(v.Runs) != 0 || v.EndedAt == nil {
		t.Fatalf("set = %+v, want stopped `slice-changed` with no trial, written by the job", v)
	}
	if v.Trials != 2 || v.ProjectPrefix != "trial" || v.Operation != opv.ID || v.Slice != "S-1" || v.Method == "" {
		t.Errorf("set = %+v, want the operation's body (2 trials, prefix trial), its operation and its method", v)
	}
	if v.PartHashes["goals"] != acceptance.Parts.Goals || v.PartHashes["rules"] != acceptance.Parts.Rules || v.PartHashes["contract"] != acceptance.Parts.Contract {
		t.Errorf("part_hashes = %v, want those recorded at acceptance, not those of the edited slice", v.PartHashes)
	}
	if n := x.runs.startCount(); n != 0 {
		t.Errorf("%d runs started, want none", n)
	}
}

func TestTSAFailedSetWriteThenAnUnchangedSliceRunsTheSetFromTheOperationsRecord(t *testing.T) { // T007
	x := newDopRig(t)
	x.seed(1, true)
	x.registerAll()
	x.ops.set(func(f *dopOps) { f.hold = true })
	sliceDir := filepath.Join(x.dir, "slices", "S-1")
	if err := os.Chmod(sliceDir, 0o500); err != nil { //nolint:gosec // a folder needs its search bit
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sliceDir, 0o700) }) //nolint:gosec // a folder needs its search bit
	opv := x.opView(x.startSet("trial", 1), 202)
	if err := os.Chmod(sliceDir, 0o700); err != nil { //nolint:gosec // a folder needs its search bit
		t.Fatal(err)
	}
	x.ops.release(opv.ID)
	x.waitStarts(1)
	var s RunStart
	x.runs.set(func(f *dopRuns) { s = f.starts[0] })
	if s.Run != "RUN-1" || s.Project != "trial-1" || !s.NoReuse || !s.RejectDuplicate || trStarted(s, "TrialSet") != opv.Target || trStarted(s, "Trial") != 1 {
		t.Errorf("trial 1 was started as %+v, want RUN-1 into trial-1, NoReuse, RejectDuplicate, trial_set %s and trial 1", s, opv.Target)
	}
	if v := x.getSet(opv.Target); v.State != "running" || !sameStrings(v.Runs, []string{"RUN-1"}) {
		t.Errorf("set = %+v, want running with RUN-1 recorded", v)
	}
	// Cancelling the set's operation cancels its running trial and ends the set cancelled.
	x.recordRun()
	x.do("POST", apiPath("/operations/"+opv.ID+"/cancel"), "", "").operation(t, 202)
	if end := x.settle(opv.ID); end.Status != "cancelled" {
		t.Errorf("operation = %+v, want cancelled", end)
	}
	if x.log.index("runs.cancel RUN-1") < 0 {
		t.Errorf("calls %v: want the running trial's run cancelled", x.log.all())
	}
	if v := x.getSet(opv.Target); v.State != "cancelled" {
		t.Errorf("set = %+v, want cancelled", v)
	}
	if n := x.runs.startCount(); n != 1 {
		t.Errorf("%d runs started, want 1: none after the cancel", n)
	}
}

func TestTSADeleteSliceAndASecondSetAreRunActiveWhileASetRunsWithNoRunActive(t *testing.T) { // T007: set running, between trials
	x := newDopRig(t)
	x.seed(1, true)
	x.registerAll()
	x.ops.set(func(f *dopOps) { f.hold = true }) // the job has not started: the set runs, no run exists
	opv := x.opView(x.startSet("trial", 2), 202)
	if v := x.getSet(opv.Target); v.State != "running" {
		t.Fatalf("set = %+v, want running", v)
	}
	x.startSet("other", 2).problem(t, 409, "RunActive")
	x.do("DELETE", spURL("S-1", ""), "", "").problem(t, 409, "RunActive")
	if n := x.runs.startCount(); n != 0 {
		t.Errorf("%d runs started by refusals", n)
	}
}

func TestTSARepeatedJobFollowsTheRunItRecordedAndNeverStartsASecond(t *testing.T) { // T007, Write order 2
	x := newDopRig(t)
	x.seed(1, true)
	x.registerAll()
	x.ops.set(func(f *dopOps) { f.hold = true })
	opv := x.opView(x.startSet("trial", 1), 202)
	x.ops.release(opv.ID)
	x.waitStarts(1)
	x.recordRun() // the run's workflow wrote its start
	// The engine stops while the trial runs; the workflow of the set is still there and repeats the job.
	x.restart(false, false)
	x.registerAll()
	if p := x.opView(x.getOp(opv.ID), 200); p.Status != "running" {
		t.Fatalf("after the restart %s is %q, want running while its trial runs", opv.ID, p.Status)
	}
	x.recordRunMore("RunCompleted")
	end := x.settle(opv.ID)
	if end.Status != "succeeded" {
		t.Fatalf("operation = %+v, want succeeded once its trial is done", end)
	}
	if n := x.runs.startCount(); n != 1 {
		t.Errorf("the trial's run was started %d times, want 1: the repeated job follows it", n)
	}
	if n := x.opsTargeting("RUN-1"); n != 1 {
		t.Errorf("%d operations target RUN-1, want one `run` operation", n)
	}
	v := x.getSet(opv.Target)
	if v.State != "ended" || !sameStrings(v.Runs, []string{"RUN-1"}) || v.Result.Ended != 1 || v.Result.Done != 1 {
		t.Errorf("set = %+v, want ended with RUN-1 done", v)
	}
}

func TestTSARecordedRunIDWithNoWorkflowAndNoRecordIsStartedOnceWithTheSameID(t *testing.T) { // T007, Write order 2
	x := newDopRig(t)
	x.seed(1, true)
	x.registerAll()
	x.ops.set(func(f *dopOps) { f.hold = true })
	opv := x.opView(x.startSet("trial", 1), 202)
	do := func(ctx context.Context) error { return x.op.Do(ctx, opv.ID) }
	// The first attempt cannot start the run: the ID is recorded in the set, the workflow is not.
	x.runs.set(func(f *dopRuns) { f.startDown = true })
	failed := make(chan error, 1)
	go func() { failed <- do(context.Background()) }()
	select {
	case err := <-failed:
		if err == nil || !IsRetryable(err) {
			t.Fatalf("the job that could not start its trial returned %v, want a retryable error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the job did not return a retryable error when its trial's run could not be started")
	}
	if ok, _ := x.runs.Exists(context.Background(), "RUN-1"); ok || x.runs.startCount() != 0 {
		t.Fatalf("a workflow for RUN-1 exists, or a start was counted: the case needs neither workflow nor record")
	}
	if n := x.opsTargeting("RUN-1"); n != 0 {
		t.Fatalf("%d operations target RUN-1 before its run started, want none", n)
	}
	// The job repeats: it starts the recorded ID, once.
	x.runs.set(func(f *dopRuns) { f.startDown = false })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- do(ctx) }()
	x.waitStarts(1)
	var s RunStart
	x.runs.set(func(f *dopRuns) { s = f.starts[0] })
	if s.Run != "RUN-1" || s.Project != "trial-1" || !s.NoReuse || !s.RejectDuplicate || trStarted(s, "TrialSet") != opv.Target || trStarted(s, "Trial") != 1 {
		t.Errorf("the repeated job started %+v, want the recorded RUN-1 into trial-1, NoReuse, RejectDuplicate, trial_set %s, trial 1", s, opv.Target)
	}
	x.recordRun()
	x.recordRunMore("RunCompleted")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the repeated job returned %v, want nil once its trial is done", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the repeated job did not end after its trial's run ended")
	}
	if n := x.runs.startCount(); n != 1 {
		t.Errorf("%d runs started, want exactly one for the recorded ID", n)
	}
	if n := x.opsTargeting("RUN-1"); n != 1 {
		t.Errorf("%d operations target RUN-1, want one `run` operation", n)
	}
	if v := x.getSet(opv.Target); v.State != "ended" || !sameStrings(v.Runs, []string{"RUN-1"}) {
		t.Errorf("set = %+v, want ended with RUN-1 only", v)
	}
}
