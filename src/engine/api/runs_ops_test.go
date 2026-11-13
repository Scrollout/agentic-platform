package api

// T054, the engine's run operations: startRun that passes every refusal, getRun, listRuns,
// listRunEvents, getArtifact, compareRuns, resumeRun, replayRun and acceptHandOver, judged from the
// contract, data-model and constitution (Principle X) only. Written before the code by a separate
// author.
//
// Interface assumed (fixed by the task brief):
//
//	Config.StepWorker  machine.Worker   // runs a step attempt
//	Config.StepChecker machine.Checker  // runs a check
//
// Both nil: a start that passes every refusal is refused WorkersNotReady, nothing recorded. The run's
// pattern comes from the slices service and the run record goes to Config.Store, which these tests
// make a real *store.Store, so a new handler over the same store sees the runs.
//
// Ambiguities the contract leaves open, and what these tests assume:
//   - Two concurrent startRuns for one project: the folder does not exist until a worker writes it,
//     so the loser is RunActive (same slice); ProjectExists is also accepted there.
//   - replayRun on a run that has not ended: RunNotEnded is taken to come before RunActive and
//     WorkersNotReady (the contract lists it first for replayRun); the replay of a running slice's
//     run would also be RunActive.
//   - acceptHandOver returns "the run's operation": only its kind (run) and target (the run) are
//     checked, not whether its ID is the start's, nor its final status.
//   - A getArtifact for an artifact the store holds that the run's record does not name is 404.
//   - getArtifact's Content-Type is not checked: the machine records no media type.
//   - A malformed artifact hash is 400 InvalidRequest (the path parameter's schema).
//   - A replay reuses every accepted result, so the step worker is not called for it.
//   - A run whose worker keeps erroring is `interrupted` (machine package doc); a run whose check
//     keeps failing is `waiting-for-developer`.
//   - The 100-per-page cut of listRunEvents is exercised with 120 non-blocking review findings, each
//     a FindingRecorded event (machine drive.go); a run with fewer events never needs a second page.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/store"
)

// ---- fakes ----

// fakeStepWorker is the machine.Worker the tests control. Its default writes one deterministic
// artifact per declared output to the store and returns it.
type fakeStepWorker struct {
	mu    sync.Mutex
	st    *store.Store
	fn    func(ctx context.Context, r machine.Request) (machine.Result, error)
	calls []string
	data  map[string][]byte // hash to the bytes the worker stored
}

func (w *fakeStepWorker) set(fn func(ctx context.Context, r machine.Request) (machine.Result, error)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fn = fn
}

func (w *fakeStepWorker) callCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.calls)
}

func (w *fakeStepWorker) bytesOf(hash string) ([]byte, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	b, ok := w.data[hash]
	return b, ok
}

func (w *fakeStepWorker) Run(ctx context.Context, r machine.Request) (machine.Result, error) {
	w.mu.Lock()
	w.calls = append(w.calls, r.Step.Name)
	fn := w.fn
	w.mu.Unlock()
	if fn != nil {
		return fn(ctx, r)
	}
	return w.produce(r)
}

func (w *fakeStepWorker) produce(r machine.Request) (machine.Result, error) {
	out := map[string]string{}
	for _, name := range r.Step.Outputs {
		b := []byte("content of " + r.Step.Name + "/" + name)
		h, err := w.st.PutArtifact("S-1", b)
		if err != nil {
			return machine.Result{}, err
		}
		w.mu.Lock()
		w.data[h] = b
		w.mu.Unlock()
		out[name] = h
	}
	return machine.Result{Model: "test-model", Outputs: out, Usage: machine.Usage{Tokens: 10}, Done: true}, nil
}

// fakeStepChecker is the machine.Checker the tests control; it passes every check by default.
type fakeStepChecker struct {
	mu sync.Mutex
	fn func(ctx context.Context, r machine.CheckRequest) (machine.CheckResult, error)
}

func (c *fakeStepChecker) set(fn func(ctx context.Context, r machine.CheckRequest) (machine.CheckResult, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fn = fn
}

func (c *fakeStepChecker) Check(ctx context.Context, r machine.CheckRequest) (machine.CheckResult, error) {
	c.mu.Lock()
	fn := c.fn
	c.mu.Unlock()
	if fn != nil {
		return fn(ctx, r)
	}
	return machine.CheckResult{Passed: true}, nil
}

// gatedWorker returns a worker function that blocks every call until release is closed or the
// call's context ends, then does what the default does.
func (x *runsRig) gatedWorker() (release func()) {
	gate := make(chan struct{})
	var once sync.Once
	x.w.set(func(ctx context.Context, r machine.Request) (machine.Result, error) {
		select {
		case <-gate:
		case <-ctx.Done():
			return machine.Result{}, ctx.Err()
		}
		return x.w.produce(r)
	})
	release = func() { once.Do(func() { close(gate) }) }
	x.t.Cleanup(release)
	return release
}

// failStep makes the checker fail every check of the step.
func (x *runsRig) failStep(step string) {
	x.c.set(func(_ context.Context, r machine.CheckRequest) (machine.CheckResult, error) {
		if r.Step.Name == step {
			return machine.CheckResult{Passed: false, Reason: "the " + step + " step is wrong"}, nil
		}
		return machine.CheckResult{Passed: true}, nil
	})
}

// ---- rig ----

type runsRig struct {
	t        *testing.T
	sp       *spRig
	st       *store.Store
	w        *fakeStepWorker
	c        *fakeStepChecker
	projects string
	r        *rig
}

func newRunsRig(t *testing.T) *runsRig {
	t.Helper()
	st, err := store.Open(t.TempDir(), spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	sp := newSP(t, []spResult{{p: spNotes(t)}}, st)
	sp.approveAfterSeed(t)
	x := &runsRig{t: t, sp: sp, st: st, projects: t.TempDir(),
		w: &fakeStepWorker{st: st, data: map[string][]byte{}}, c: &fakeStepChecker{}}
	x.boot(x.w, x.c)
	registerAll(t, x.r, "S-1")
	return x
}

// boot makes a handler over the same slices service and store, as an engine restart does.
func (x *runsRig) boot(w machine.Worker, c machine.Checker) {
	ctx, cancel := context.WithCancel(context.Background())
	x.r = newRig(x.t, func(cfg *Config) {
		cfg.Context = ctx
		cfg.Slices, cfg.Store = x.sp.svc, x.st
		cfg.WorkerGrants = stepGrants("S-1")
		cfg.Projects = x.projects
		cfg.Model = "test-model"
		if w != nil {
			cfg.StepWorker = w
		}
		if c != nil {
			cfg.StepChecker = c
		}
	})
	stopEngineAtEnd(x.t, x.r, cancel)
}

// stopEngineAtEnd registers a cleanup that cancels the engine's base context and waits, for at
// most 5 seconds, until no run of the handler is still running, so no run goroutine writes to
// the store while the test's temporary directories are removed. Call it after the directories
// are made: cleanups run last in, first out.
func stopEngineAtEnd(t *testing.T, r *rig, cancel context.CancelFunc) {
	t.Helper()
	t.Cleanup(func() {
		cancel()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			r.clock.Advance(80 * time.Millisecond)
			p := r.do("GET", apiPath("/runs"), clientToken, "", nil)
			var pg runPage
			if p.Code != 200 || json.Unmarshal(p.Body.Bytes(), &pg) != nil {
				return
			}
			busy := false
			for _, v := range pg.Items {
				busy = busy || v.Status == "running"
			}
			if !busy {
				time.Sleep(20 * time.Millisecond) // let a goroutine that just ended finish its last write
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
}

// do advances the rig's clock first, so a long test never meets the per-token rate limit.
func (x *runsRig) do(method, path, body string) reply {
	x.r.clock.Advance(80 * time.Millisecond)
	return x.r.do(method, path, clientToken, body, nil)
}

func apiPath(p string) string {
	if !strings.Contains(p, "?") {
		p = strings.Replace(p, "&", "?", 1) // a query built with a leading "&" starts the query string
	}
	if strings.Contains(p, "?") {
		return p + "&api-version=" + apiVersion
	}
	return p + "?api-version=" + apiVersion
}

func (x *runsRig) start(project string) reply {
	return x.do("POST", spURL("S-1", "/runs"), `{"project":"`+project+`"}`)
}

// startOK starts a run and returns its operation, which must be a 202 of kind run.
func (x *runsRig) startOK(project string) spOp {
	x.t.Helper()
	return x.opOf(x.start(project), "run")
}

func (x *runsRig) opOf(p reply, kind string) spOp {
	x.t.Helper()
	op := p.operation(x.t, http.StatusAccepted)
	if string(op.Kind) != kind {
		x.t.Fatalf("operation kind = %q, want %q", op.Kind, kind)
	}
	if !runIDRE.MatchString(op.Target) {
		x.t.Fatalf("operation target = %q, want RUN-<n>", op.Target)
	}
	return spOp{ID: op.Id, Kind: string(op.Kind), Status: string(op.Status), Target: op.Target}
}

var runIDRE = regexp.MustCompile(`^RUN-[0-9]+$`)

type runView struct {
	ID        string  `json:"id"`
	Slice     string  `json:"slice"`
	Pattern   string  `json:"pattern"`
	Project   string  `json:"project"`
	Status    string  `json:"status"`
	Outcome   *string `json:"outcome"`
	Reason    string  `json:"reason"`
	ExpiresAt *string `json:"expires_at"`
	Operation string  `json:"operation"`
	ReplayOf  *string `json:"replay_of"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	EndedAt   *string `json:"ended_at"`
}

func (x *runsRig) getRun(id string) runView {
	x.t.Helper()
	p := x.do("GET", apiPath("/runs/"+id), "")
	if p.Code != 200 {
		x.t.Fatalf("getRun %s = %d: %s", id, p.Code, p.Body.String())
	}
	var v runView
	if err := json.Unmarshal(p.Body.Bytes(), &v); err != nil {
		x.t.Fatal(err)
	}
	return v
}

// waitRun polls getRun until cond holds, for at most 5 seconds.
func (x *runsRig) waitRun(id, what string, cond func(runView) bool) runView {
	x.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		v := x.getRun(id)
		if cond(v) {
			return v
		}
		if time.Now().After(deadline) {
			x.t.Fatalf("run %s is %q (outcome %v) after 5s, want %s", id, v.Status, v.Outcome, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (x *runsRig) waitStatus(id, status string) runView {
	x.t.Helper()
	return x.waitRun(id, status, func(v runView) bool { return v.Status == status })
}

func (x *runsRig) waitEnded(id, outcome string) runView {
	x.t.Helper()
	v := x.waitRun(id, "ended "+outcome, func(v runView) bool { return v.Status == "ended" })
	if v.Outcome == nil || *v.Outcome != outcome {
		x.t.Fatalf("run %s ended with outcome %v, want %q", id, v.Outcome, outcome)
	}
	if v.EndedAt == nil {
		x.t.Errorf("an ended run has ended_at")
	}
	return v
}

// runDone starts a run, lets it finish and returns it.
func (x *runsRig) runDone(project string) runView {
	x.t.Helper()
	op := x.startOK(project)
	return x.waitEnded(op.Target, "done")
}

type eventView struct {
	Seq       int      `json:"seq"`
	Run       string   `json:"run"`
	Slice     string   `json:"slice"`
	Kind      string   `json:"kind"`
	Step      string   `json:"step"`
	Artifacts []string `json:"artifacts"`
	Check     *struct {
		Name   string `json:"name"`
		Passed bool   `json:"passed"`
	} `json:"check"`
}

type eventPage struct {
	Items []eventView `json:"items"`
	Next  *string     `json:"next_page_token"`
}

func (x *runsRig) events(id, query string) eventPage {
	x.t.Helper()
	p := x.do("GET", apiPath("/runs/"+id+"/events"+query), "")
	if p.Code != 200 {
		x.t.Fatalf("listRunEvents %s = %d: %s", id, p.Code, p.Body.String())
	}
	var pg eventPage
	if err := json.Unmarshal(p.Body.Bytes(), &pg); err != nil {
		x.t.Fatal(err)
	}
	return pg
}

func (x *runsRig) allEvents(id string) []eventView {
	x.t.Helper()
	var all []eventView
	q := ""
	for range 20 {
		pg := x.events(id, q)
		all = append(all, pg.Items...)
		if pg.Next == nil {
			return all
		}
		q = "&page_token=" + url.QueryEscape(*pg.Next)
	}
	x.t.Fatal("listRunEvents did not end after 20 pages")
	return nil
}

type runPage struct {
	Items []runView `json:"items"`
	Next  *string   `json:"next_page_token"`
}

func (x *runsRig) listRuns(query string) runPage {
	x.t.Helper()
	p := x.do("GET", apiPath("/runs"+query), "")
	if p.Code != 200 {
		x.t.Fatalf("listRuns = %d: %s", p.Code, p.Body.String())
	}
	var pg runPage
	if err := json.Unmarshal(p.Body.Bytes(), &pg); err != nil {
		x.t.Fatal(err)
	}
	return pg
}

type diffView struct {
	Step  string  `json:"step"`
	Field string  `json:"field"`
	Left  *string `json:"left"`
	Right *string `json:"right"`
}

func (x *runsRig) compare(id, with string) (items []diffView, raw string) {
	x.t.Helper()
	p := x.do("GET", apiPath("/runs/"+id+"/comparison?with="+with), "")
	if p.Code != 200 {
		x.t.Fatalf("compareRuns %s with %s = %d: %s", id, with, p.Code, p.Body.String())
	}
	var pg struct {
		Items []diffView `json:"items"`
	}
	if err := json.Unmarshal(p.Body.Bytes(), &pg); err != nil {
		x.t.Fatal(err)
	}
	return pg.Items, p.Body.String()
}

func (x *runsRig) noMoreOperations(after string) {
	x.t.Helper()
	x.do("GET", apiPath("/operations/"+after), "").problem(x.t, 404, "NotFound")
}

// ---- startRun ----

func TestStartRunIsRecordedBeforeItIsAcknowledged(t *testing.T) {
	x := newRunsRig(t)
	release := x.gatedWorker() // the run cannot get past its first step
	op := x.startOK("notes-app")
	if op.Target != "RUN-1" {
		t.Fatalf("target = %q, want RUN-1 for the first run", op.Target)
	}
	// Right after the 202, with the run still running: the run and its first event exist.
	v := x.getRun("RUN-1")
	if v.ID != "RUN-1" || v.Slice != "S-1" || v.Project != "notes-app" || v.Operation != op.ID {
		t.Errorf("run = %+v, want RUN-1 of S-1, project notes-app, operation %s", v, op.ID)
	}
	if v.Pattern != "api-feature-go@1.0.0" {
		t.Errorf("pattern = %q, want api-feature-go@1.0.0 (the pattern the slice was checked against)", v.Pattern)
	}
	if v.Status != "running" || v.Outcome != nil || v.EndedAt != nil || v.ReplayOf != nil {
		t.Errorf("a running run has no outcome, ended_at or replay_of: %+v", v)
	}
	if v.CreatedAt == "" || v.UpdatedAt == "" {
		t.Errorf("created_at and updated_at are required: %+v", v)
	}
	pg := x.events("RUN-1", "")
	if len(pg.Items) == 0 || pg.Items[0].Kind != "RunStarted" || pg.Items[0].Slice != "S-1" ||
		pg.Items[0].Seq != 1 || pg.Items[0].Run != "RUN-1" {
		t.Fatalf("the record must start with seq 1 RunStarted naming S-1: %+v", pg.Items)
	}
	release()
	x.waitEnded("RUN-1", "done")
}

func TestStartRunWithoutAWorkerOrCheckerIsWorkersNotReadyAndRecordsNothing(t *testing.T) {
	cases := map[string]struct {
		w machine.Worker
		c machine.Checker
	}{
		"both nil":   {nil, nil},
		"no worker":  {nil, &fakeStepChecker{}},
		"no checker": {&fakeStepWorker{data: map[string][]byte{}}, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			x := newRunsRig(t)
			x.boot(c.w, c.c)
			registerAll(t, x.r, "S-1") // the step workers are registered; the engine cannot run them
			x.start("notes-app").problem(t, 409, "WorkersNotReady")
			x.noMoreOperations("OP-1")
			if n := len(x.listRuns("").Items); n != 0 {
				t.Errorf("%d runs recorded for a refused start", n)
			}
			x.do("GET", apiPath("/runs/RUN-1"), "").problem(t, 404, "NotFound")
		})
	}
}

func TestARunWhoseChecksAllPassEndsDone(t *testing.T) {
	x := newRunsRig(t)
	op := x.startOK("notes-app")
	v := x.waitEnded(op.Target, "done")
	if v.ExpiresAt != nil {
		t.Errorf("an ended run has no expires_at: %+v", v)
	}
	if got := x.waitOp(op.ID); got.Status != "succeeded" {
		t.Errorf("operation of a done run = %q, want succeeded", got.Status)
	}
	evs := x.allEvents(op.Target)
	if evs[len(evs)-1].Kind != "RunCompleted" {
		t.Errorf("last event = %q, want RunCompleted", evs[len(evs)-1].Kind)
	}
	for i, e := range evs {
		if e.Seq != i+1 || e.Run != op.Target {
			t.Fatalf("event %d = seq %d run %s, want seq %d of %s (seq from 1, no gaps)", i, e.Seq, e.Run, i+1, op.Target)
		}
	}
	accepted := map[string]bool{}
	for _, e := range evs {
		if e.Kind == "StepAccepted" {
			accepted[e.Step] = true
		}
	}
	for _, s := range declaredSteps {
		if !accepted[s] {
			t.Errorf("step %s has no StepAccepted event", s)
		}
	}
}

// waitOp follows an operation of the current handler until it has ended.
func (x *runsRig) waitOp(id string) spOp {
	x.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r := x.do("GET", apiPath("/operations/"+id), "")
		if r.Code != 200 {
			x.t.Fatalf("getOperation %s = %d: %s", id, r.Code, r.Body.String())
		}
		var op spOp
		if err := json.Unmarshal(r.Body.Bytes(), &op); err != nil {
			x.t.Fatal(err)
		}
		switch op.Status {
		case "succeeded", "failed", "cancelled":
			return op
		}
		if time.Now().After(deadline) {
			x.t.Fatalf("operation %s is still %q after 5s", id, op.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ---- hand-over ----

func TestAStepThatKeepsFailingWaitsForTheDeveloperAndAcceptingTheHandOverEndsItHandedOver(t *testing.T) {
	x := newRunsRig(t)
	x.failStep("tests")
	op := x.startOK("notes-app")
	v := x.waitStatus(op.Target, "waiting-for-developer")
	if v.Reason == "" || v.ExpiresAt == nil || v.Outcome != nil {
		t.Errorf("a waiting run has a reason and expires_at, and no outcome: %+v", v)
	}
	// A run waiting for the developer is active (data-model), so another build is refused.
	x.start("other-app").problem(t, 409, "RunActive")

	hop := x.opOf(x.do("POST", apiPath("/runs/"+op.Target+"/hand-over"), ""), "run")
	if hop.Target != op.Target {
		t.Errorf("hand-over operation target = %q, want %q", hop.Target, op.Target)
	}
	x.waitEnded(op.Target, "handed-over")
	evs := x.allEvents(op.Target)
	var kinds []string
	for _, e := range evs {
		kinds = append(kinds, e.Kind)
	}
	if !contains(kinds, "RunHandedOver") || kinds[len(kinds)-1] != "HandOverAccepted" {
		t.Errorf("record should hold RunHandedOver and end with HandOverAccepted: %v", kinds)
	}
	// The slice is free for a new run once the first has ended.
	x.failStep("")
	x.startOK("other-app")
}

func TestAcceptHandOverOnARunNotWaitingIsRunNotWaiting(t *testing.T) {
	x := newRunsRig(t)
	release := x.gatedWorker()
	op := x.startOK("notes-app")
	x.do("POST", apiPath("/runs/"+op.Target+"/hand-over"), "").problem(t, 409, "RunNotWaiting") // running
	release()
	x.waitEnded(op.Target, "done")
	x.do("POST", apiPath("/runs/"+op.Target+"/hand-over"), "").problem(t, 409, "RunNotWaiting") // ended
	if v := x.getRun(op.Target); v.Outcome == nil || *v.Outcome != "done" {
		t.Errorf("a refused hand-over changed the run: %+v", v)
	}
	x.do("POST", apiPath("/runs/RUN-99/hand-over"), "").problem(t, 404, "NotFound")
}

// ---- RunActive and concurrency ----

func TestASecondStartForTheSliceWhileTheFirstHasNotEndedIsRunActive(t *testing.T) {
	x := newRunsRig(t)
	release := x.gatedWorker()
	x.startOK("notes-app")
	x.start("other-app").problem(t, 409, "RunActive")
	x.noMoreOperations("OP-2") // a refusal creates no operation
	if n := len(x.listRuns("").Items); n != 1 {
		t.Errorf("%d runs, want 1", n)
	}
	release()
	x.waitEnded("RUN-1", "done")
	if op := x.startOK("other-app"); op.Target != "RUN-2" {
		t.Errorf("target = %q, want RUN-2 once the first run has ended", op.Target)
	}
}

func TestConcurrentStartsForOneProjectAcceptExactlyOne(t *testing.T) {
	x := newRunsRig(t)
	release := x.gatedWorker()
	const n = 8
	codes := make([]int, n)
	bodies := make([]reply, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bodies[i] = x.start("notes-app")
			codes[i] = bodies[i].Code
		}()
	}
	wg.Wait()
	accepted := 0
	for i, c := range codes {
		switch c {
		case 202:
			accepted++
		case 409:
			var p struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(bodies[i].Body.Bytes(), &p)
			if p.Code != "RunActive" && p.Code != "ProjectExists" {
				t.Errorf("start %d refused with %q, want RunActive (or ProjectExists)", i, p.Code)
			}
		default:
			t.Errorf("start %d = %d: %s", i, c, bodies[i].Body.String())
		}
	}
	if accepted != 1 {
		t.Fatalf("%d concurrent starts accepted, want exactly 1", accepted)
	}
	if got := len(x.listRuns("").Items); got != 1 {
		t.Errorf("%d runs recorded, want 1", got)
	}
	release()
	x.waitEnded("RUN-1", "done")
}

// ---- restart ----

func TestRunIDsKeepCountingAfterARestartAndTheEarlierRunIsListed(t *testing.T) {
	x := newRunsRig(t)
	x.runDone("notes-app")
	x.boot(x.w, x.c) // a new handler over the same store
	registerAll(t, x.r, "S-1")
	got := x.listRuns("")
	if len(got.Items) != 1 || got.Items[0].ID != "RUN-1" || got.Items[0].Status != "ended" {
		t.Fatalf("restarted engine lists %+v, want the ended RUN-1", got.Items)
	}
	if v := x.getRun("RUN-1"); v.Outcome == nil || *v.Outcome != "done" || v.Slice != "S-1" || v.Project != "notes-app" {
		t.Errorf("RUN-1 after restart = %+v", v)
	}
	if evs := x.allEvents("RUN-1"); len(evs) == 0 || evs[0].Kind != "RunStarted" {
		t.Errorf("RUN-1's record after restart starts with %+v", evs)
	}
	op := x.startOK("second-app")
	if op.Target != "RUN-2" {
		t.Fatalf("target after restart = %q, want RUN-2 (RUN-1 must not be reissued)", op.Target)
	}
	x.waitEnded("RUN-2", "done")
	if got := x.listRuns("").Items; len(got) != 2 || got[0].ID != "RUN-2" {
		t.Errorf("runs newest first = %+v, want RUN-2 then RUN-1", got)
	}
}

// ---- cancel ----

func TestCancellingTheRunsOperationEndsTheRunCancelled(t *testing.T) {
	x := newRunsRig(t)
	x.gatedWorker() // blocks until its context ends
	op := x.startOK("notes-app")
	x.do("POST", apiPath("/operations/"+op.ID+"/cancel"), "").operation(t, 202)
	x.waitEnded(op.Target, "cancelled")
	if got := x.waitOp(op.ID); got.Status != "cancelled" {
		t.Errorf("operation = %q, want cancelled", got.Status)
	}
	evs := x.allEvents(op.Target)
	if evs[len(evs)-1].Kind != "RunCancelled" {
		t.Errorf("last event = %q, want RunCancelled", evs[len(evs)-1].Kind)
	}
	// The slice is free again.
	x.w.set(nil)
	again := x.startOK("again-app")
	x.waitEnded(again.Target, "done")
}

// ---- resume ----

func TestResumeRunOnARunThatIsNotInterruptedIsRunNotInterrupted(t *testing.T) {
	x := newRunsRig(t)
	release := x.gatedWorker()
	op := x.startOK("notes-app")
	x.do("POST", apiPath("/runs/"+op.Target+"/resume"), "").problem(t, 409, "RunNotInterrupted") // running
	release()
	x.waitEnded(op.Target, "done")
	x.do("POST", apiPath("/runs/"+op.Target+"/resume"), "").problem(t, 409, "RunNotInterrupted") // ended
	x.do("POST", apiPath("/runs/RUN-99/resume"), "").problem(t, 404, "NotFound")
}

func TestResumeRunOnAnInterruptedRunContinuesIt(t *testing.T) {
	x := newRunsRig(t)
	x.w.set(func(context.Context, machine.Request) (machine.Result, error) {
		return machine.Result{}, errors.New("the model is unavailable") // every attempt fails
	})
	op := x.startOK("notes-app")
	x.waitStatus(op.Target, "interrupted")
	x.w.set(nil)
	rop := x.opOf(x.do("POST", apiPath("/runs/"+op.Target+"/resume"), ""), "run")
	if rop.Target != op.Target {
		t.Errorf("resume operation target = %q, want %q", rop.Target, op.Target)
	}
	x.waitEnded(op.Target, "done")
	var resumed bool
	for _, e := range x.allEvents(op.Target) {
		resumed = resumed || e.Kind == "RunResumed"
	}
	if !resumed {
		t.Errorf("the record has no RunResumed")
	}
}

// ---- replay and compare ----

func TestReplayRunOnARunThatHasNotEndedIsRunNotEnded(t *testing.T) {
	x := newRunsRig(t)
	release := x.gatedWorker()
	op := x.startOK("notes-app")
	x.do("POST", apiPath("/runs/"+op.Target+"/replay"), `{"project":"again-app"}`).problem(t, 409, "RunNotEnded")
	x.do("POST", apiPath("/runs/RUN-99/replay"), `{"project":"again-app"}`).problem(t, 404, "NotFound")
	release()
	x.waitEnded(op.Target, "done")
}

func TestReplayOfAnEndedRunStartsANewRunAndAnIdenticalReplayDiffersInNothing(t *testing.T) {
	x := newRunsRig(t)
	first := x.runDone("notes-app")
	before := x.w.callCount()

	rop := x.opOf(x.do("POST", apiPath("/runs/RUN-1/replay"), `{"project":"again-app"}`), "run")
	if rop.Target != "RUN-2" {
		t.Fatalf("replay target = %q, want RUN-2", rop.Target)
	}
	second := x.waitEnded("RUN-2", "done")
	if second.ReplayOf == nil || *second.ReplayOf != "RUN-1" {
		t.Errorf("replay_of = %v, want RUN-1", second.ReplayOf)
	}
	if second.Slice != first.Slice || second.Pattern != first.Pattern || second.Project != "again-app" || second.Operation != rop.ID {
		t.Errorf("replay = %+v, want slice and pattern of %+v, project again-app, operation %s", second, first, rop.ID)
	}
	if first.ReplayOf != nil {
		t.Errorf("an ordinary run has no replay_of")
	}
	if n := x.w.callCount() - before; n != 0 {
		t.Errorf("the step worker ran %d times for a replay that reuses every accepted result", n)
	}
	for _, pair := range [][2]string{{"RUN-1", "RUN-2"}, {"RUN-2", "RUN-1"}} {
		items, raw := x.compare(pair[0], pair[1])
		if len(items) != 0 || !strings.Contains(raw, `"items":[]`) {
			t.Errorf("compare %s with %s = %s, want an empty items array (identical replay)", pair[0], pair[1], raw)
		}
	}
	if items, _ := x.compare("RUN-1", "RUN-1"); len(items) != 0 {
		t.Errorf("a run differs from itself in %v", items)
	}
}

func TestCompareRunsListsOnlyTheFieldsThatDiffer(t *testing.T) {
	x := newRunsRig(t)
	x.failStep("tests")
	op := x.startOK("notes-app")
	x.waitStatus(op.Target, "waiting-for-developer")
	x.opOf(x.do("POST", apiPath("/runs/"+op.Target+"/hand-over"), ""), "run")
	x.waitEnded(op.Target, "handed-over")
	x.c.set(nil)
	x.runDone("second-app")

	items, _ := x.compare("RUN-1", "RUN-2")
	if len(items) == 0 {
		t.Fatal("no differences between a handed-over run and a done run")
	}
	steps := map[string]int{}
	var testsChecks *diffView
	for i, d := range items {
		steps[d.Step]++
		if !contains(declaredSteps, d.Step) || !contains([]string{"artifacts", "checks", "decision"}, d.Field) {
			t.Errorf("difference %+v names an unknown step or field", d)
		}
		if d.Left == nil && d.Right == nil || (d.Left != nil && d.Right != nil && *d.Left == *d.Right) {
			t.Errorf("difference %+v lists a field that does not differ", d)
		}
		if d.Step == "tests" && d.Field == "checks" {
			testsChecks = &items[i]
		}
	}
	if testsChecks == nil || testsChecks.Left == nil || !strings.HasPrefix(*testsChecks.Left, "failed: ") ||
		testsChecks.Right == nil || *testsChecks.Right != "passed" {
		t.Errorf("tests/checks difference = %+v, want left `failed: <reason>` and right `passed`", testsChecks)
	}
	for _, s := range []string{"contract-check", "generate"} {
		if steps[s] != 0 {
			t.Errorf("step %s is identical in both runs but is listed %d times", s, steps[s])
		}
	}
	// Step order.
	last := -1
	for _, d := range items {
		i := indexOf(declaredSteps, d.Step)
		if i < last {
			t.Errorf("differences are not in step order: %+v", items)
			break
		}
		last = i
	}
}

func TestCompareRunsRefusesWhatItCannotCompare(t *testing.T) {
	x := newRunsRig(t)
	x.runDone("notes-app")
	x.do("GET", apiPath("/runs/RUN-1/comparison"), "").problem(t, 400, "InvalidRequest")
	x.do("GET", apiPath("/runs/RUN-1/comparison?with=nope"), "").problem(t, 400, "InvalidRequest")
	x.do("GET", apiPath("/runs/RUN-1/comparison?with=RUN-99"), "").problem(t, 404, "NotFound")
	x.do("GET", apiPath("/runs/RUN-99/comparison?with=RUN-1"), "").problem(t, 404, "NotFound")
	x.do("GET", apiPath("/runs/RUN-1/comparison?with=RUN-1&page_token=forged"), "").problem(t, 400, "InvalidRequest")
}

// ---- reads: unknown things and artifacts ----

func TestUnknownRunsAndArtifactsAre404(t *testing.T) {
	x := newRunsRig(t)
	x.runDone("notes-app")
	unrelated, err := x.st.PutArtifact("S-1", []byte("stored but never named by the run"))
	if err != nil {
		t.Fatal(err)
	}
	const missing = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, path := range []string{
		"/runs/RUN-99",
		"/runs/RUN-99/events",
		"/runs/RUN-99/artifacts/" + missing,
		"/runs/RUN-1/artifacts/" + missing,
		"/runs/RUN-1/artifacts/" + unrelated, // the store has it, the run's record does not name it
	} {
		t.Run(path, func(t *testing.T) { x.do("GET", apiPath(path), "").problem(t, 404, "NotFound") })
	}
	x.do("GET", apiPath("/runs/RUN-1/artifacts/not-a-hash"), "").problem(t, 400, "InvalidRequest")
}

func TestGetArtifactReturnsTheBytesTheRunProduced(t *testing.T) {
	x := newRunsRig(t)
	x.runDone("notes-app")
	var hashes []string
	for _, e := range x.allEvents("RUN-1") {
		if e.Kind == "StepAccepted" {
			hashes = append(hashes, e.Artifacts...)
		}
	}
	if len(hashes) == 0 {
		t.Fatal("no accepted step names an artifact")
	}
	for _, h := range hashes {
		want, ok := x.w.bytesOf(h)
		if !ok {
			t.Fatalf("the record names artifact %s the worker never produced", h)
		}
		p := x.do("GET", apiPath("/runs/RUN-1/artifacts/"+h), "")
		if p.Code != 200 || p.Body.String() != string(want) {
			t.Errorf("getArtifact %s = %d %q, want 200 %q", h, p.Code, p.Body.String(), want)
		}
	}
	// The page token may read too.
	r := x.r.do("GET", apiPath("/runs/RUN-1/artifacts/"+hashes[0]), pageToken, "", nil)
	if r.Code != 200 {
		t.Errorf("getArtifact with the page token = %d", r.Code)
	}
}

// ---- pagination ----

func TestListRunsPagesAreFiftyNewestFirstWithOpaqueTokens(t *testing.T) {
	x := newRunsRig(t)
	const total = 51
	for i := range total {
		x.runDone(fmt.Sprintf("app-%d", i))
	}
	first := x.listRuns("")
	if len(first.Items) != 50 || first.Next == nil {
		t.Fatalf("first page has %d items and token %v, want 50 and a token", len(first.Items), first.Next)
	}
	if first.Items[0].ID != "RUN-51" || first.Items[49].ID != "RUN-2" {
		t.Errorf("first page runs from %s to %s, want RUN-51 to RUN-2", first.Items[0].ID, first.Items[49].ID)
	}
	second := x.listRuns("&page_token=" + url.QueryEscape(*first.Next))
	if len(second.Items) != 1 || second.Items[0].ID != "RUN-1" || second.Next != nil {
		t.Errorf("second page = %+v, want RUN-1 alone and no token", second)
	}
	for _, bad := range []string{"forged", "AAAA", "UlVOLTE", *first.Next + "x"} {
		x.do("GET", apiPath("/runs?page_token="+url.QueryEscape(bad)), "").problem(t, 400, "InvalidRequest")
	}
	// A token another list issued is not accepted.
	x.do("GET", apiPath("/runs/RUN-1/events?page_token="+url.QueryEscape(*first.Next)), "").problem(t, 400, "InvalidRequest")
}

func TestListRunEventsPagesAreAHundredAndFollowTheirTokens(t *testing.T) {
	x := newRunsRig(t)
	x.w.set(func(_ context.Context, r machine.Request) (machine.Result, error) {
		res, err := x.w.produce(r)
		if r.Step.Name == "review" {
			for i := range 120 {
				res.Findings = append(res.Findings, machine.Finding{Rule: "R-1", Text: fmt.Sprintf("note %d", i)})
			}
		}
		return res, err
	})
	x.runDone("notes-app")
	first := x.events("RUN-1", "")
	if len(first.Items) != 100 || first.Next == nil {
		t.Fatalf("first page has %d events and token %v, want 100 and a token", len(first.Items), first.Next)
	}
	second := x.events("RUN-1", "&page_token="+url.QueryEscape(*first.Next))
	if len(second.Items) == 0 || second.Items[0].Seq != 101 {
		t.Fatalf("second page starts at %+v, want seq 101", second.Items)
	}
	if second.Next != nil {
		t.Errorf("an ended run's last page has a next_page_token")
	}
	if all := x.allEvents("RUN-1"); all[len(all)-1].Seq != len(all) {
		t.Errorf("pages together hold %d events ending at seq %d", len(all), all[len(all)-1].Seq)
	}
	for _, bad := range []string{"forged", "AAAA", *first.Next + "x"} {
		x.do("GET", apiPath("/runs/RUN-1/events?page_token="+url.QueryEscape(bad)), "").problem(t, 400, "InvalidRequest")
	}
}

func TestListRunEventsOfARunInProgressKeepsATokenForTheEventsRecordedSince(t *testing.T) {
	x := newRunsRig(t)
	release := x.gatedWorker()
	x.startOK("notes-app")
	pg := x.events("RUN-1", "")
	if pg.Next == nil {
		t.Fatal("a run that has not ended must still offer a next_page_token on its last page")
	}
	seen := len(pg.Items)
	release()
	x.waitEnded("RUN-1", "done")
	var more []eventView
	next := pg.Next
	for next != nil {
		p := x.events("RUN-1", "&page_token="+url.QueryEscape(*next))
		more = append(more, p.Items...)
		next = p.Next
	}
	if len(more) == 0 || more[0].Seq != seen+1 {
		t.Fatalf("events since the token = %+v, want them to start at seq %d", more, seen+1)
	}
	if more[len(more)-1].Kind != "RunCompleted" {
		t.Errorf("the last event is %q, want RunCompleted", more[len(more)-1].Kind)
	}
}

// ---- small helpers ----

func contains(s []string, v string) bool { return indexOf(s, v) >= 0 }

func indexOf(s []string, v string) int {
	for i, e := range s {
		if e == v {
			return i
		}
	}
	return -1
}
