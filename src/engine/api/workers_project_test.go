package api

// T003 of feature 012 (FR-013, research R3, ADR 0103): the engine keeps one step worker
// registration per slice, step and project. Written before the code (T005) by a test author,
// judged from the contract (WorkerRegistration, Worker, registerWorker, WorkersNotReady), the data
// model (Worker registration), research R3 and ADR 0103. The existing rigs and helpers are used as
// they are (newSP, newRig, newKeptRig, register, declaredSteps, endpointFor, stepGrants,
// writeGrantFile, grantFileName, unavailable, fakeRuns, machineEvent); only new helpers are here.
//
// Interface assumed, fixed by the task brief:
//   - registerWorker's body is {"endpoint", "project"}, project optional; the answer carries
//     `project` when one was sent and none when none was;
//   - (*Workers).Endpoint(slice, project, step) finds the registration of that slice, step and
//     project, else the one without a project; project "" finds only the one without a project;
//   - registering again for the same slice, step and project replaces only that one;
//   - startRun, resumeRun (the run's project) and replayRun (the new project) are
//     WorkersNotReady, naming the step and the project, unless every step has the project's own
//     registration or one without a project;
//   - `workers-ready` goes only to a run of the slice that waits for its step workers and whose
//     project has every step (own or without a project);
//   - store.WorkerRegistration has Project; DeleteWorkerRegistration(slice, step, project);
//     kept registrations survive a restart per project; erasing a slice removes every project's.
//
// Cases deliberately not pinned: the order the store lists registrations in (compared as sets);
// which of two waiting runs is signalled when a registration without a project completes the
// steps of both (the task says only that a project-less registration counts for any project).

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/store"
)

const (
	projAlpha = "alpha"
	projBeta  = "beta"
)

// projLimit is the longest project_name the contract allows: 63 characters.
var projLimit = "a" + strings.Repeat("0", 62)

// ---- helpers (new, in this file only) ----

func projToken(slice, step, project string) string {
	return "proj-worker-token-" + slice + "-" + step + "-" + project
}
func projCall(slice, step, project string) string {
	return "proj-call-token-" + slice + "-" + step + "-" + project
}
func projEndpoint(step, project string) string {
	if project == "" {
		return endpointFor(step)
	}
	return "http://" + project + "-" + step + ":8080"
}

// projGrants grants, for each project (and "" for none), one worker token per declared step of
// the slice, with its own call token: a worker built for a project has tokens of its own.
func projGrants(slice string, projects ...string) map[string]WorkerGrant {
	g := map[string]WorkerGrant{}
	for _, p := range projects {
		for _, st := range declaredSteps {
			g[projToken(slice, st, p)] = WorkerGrant{Slice: slice, Step: st, CallToken: projCall(slice, st, p)}
		}
	}
	return g
}

// regBody is a registerWorker body; project "" leaves the field out.
func regBody(endpoint, project string) string {
	b := `{"endpoint":"` + endpoint + `"`
	if project != "" {
		b += `,"project":"` + project + `"`
	}
	return b + "}"
}

func regProject(r *rig, token, endpoint, project string) reply {
	r.clock.Advance(1100 * time.Millisecond) // stay under every rate limit
	return r.do("POST", "/workers?api-version="+apiVersion, token, regBody(endpoint, project), nil)
}

// projWorker is the Worker answer as it reads: project is nil when the answer has none.
type projWorker struct {
	Slice, Step, Endpoint string
	Project               *string
}

func decodeProjWorker(t *testing.T, w reply) projWorker {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	var out projWorker
	out.Slice, _ = raw["slice"].(string)
	out.Step, _ = raw["step"].(string)
	out.Endpoint, _ = raw["endpoint"].(string)
	want := 3
	if p, ok := raw["project"]; ok {
		s, isString := p.(string)
		if !isString {
			t.Fatalf("Worker.project is not a string: %s", w.Body.String())
		}
		out.Project = &s
		want = 4
	}
	if len(raw) != want {
		t.Errorf("Worker has fields beyond slice, step, endpoint and project: %s", w.Body.String())
	}
	return out
}

func wantProjWorker(t *testing.T, got projWorker, slice, step, endpoint, project string) {
	t.Helper()
	if got.Slice != slice || got.Step != step || got.Endpoint != endpoint {
		t.Errorf("Worker = %+v, want %s %s %s", got, slice, step, endpoint)
	}
	switch {
	case project == "" && got.Project != nil:
		t.Errorf("Worker.project = %q, want none (none was sent)", *got.Project)
	case project != "" && (got.Project == nil || *got.Project != project):
		t.Errorf("Worker.project = %v, want %q", got.Project, project)
	}
}

// projRig is an engine with a fake durable service, a Workers it can look into, and worker tokens
// granted at start for every project given.
type projRig struct {
	t    *testing.T
	sp   *spRig
	st   *store.Store
	runs *fakeRuns
	ws   *Workers
	r    *rig
}

func newProjRig(t *testing.T, projects ...string) *projRig {
	t.Helper()
	st, err := store.Open(t.TempDir(), spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	sp := newSP(t, []spResult{{p: spNotes(t)}}, st)
	sp.approveAfterSeed(t)
	x := &projRig{t: t, sp: sp, st: st, runs: &fakeRuns{exists: map[string]bool{}}, ws: NewWorkers()}
	projects2 := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	x.r = newRig(t, func(c *Config) {
		c.Context = ctx
		c.Slices, c.Store = sp.svc, st
		c.WorkerGrants = projGrants("S-1", projects...)
		c.Projects = projects2
		c.Model = "test-model"
		c.StepWorker, c.StepChecker = &fakeStepWorker{st: st, data: map[string][]byte{}}, &fakeStepChecker{}
		c.Workers = x.ws
		c.Runs = x.runs
	})
	return x
}

// reg registers the step's worker of the project (""= none) and returns the answer, which must be 200.
func (x *projRig) reg(step, project string) projWorker {
	x.t.Helper()
	return decodeProjWorker(x.t, regProject(x.r, projToken("S-1", step, project), projEndpoint(step, project), project))
}

func (x *projRig) regSteps(project string, steps ...string) {
	x.t.Helper()
	for _, st := range steps {
		wantProjWorker(x.t, x.reg(st, project), "S-1", st, projEndpoint(st, project), project)
	}
}

// wantEndpoint checks Workers.Endpoint(slice, lookupProject, step): the registration made for
// ownerProject (its endpoint and call token), or none when owner is "none".
func wantEndpointOf(t *testing.T, ws *Workers, what, slice, lookup, step, owner string) {
	t.Helper()
	url, tok, ok := ws.Endpoint(slice, lookup, step)
	if owner == "none" {
		if ok {
			t.Errorf("%s: Endpoint(%s, %q, %s) = %q, %q; want not registered", what, slice, lookup, step, url, tok)
		}
		return
	}
	if !ok {
		t.Errorf("%s: Endpoint(%s, %q, %s) is not registered; want the registration of %q", what, slice, lookup, step, owner)
		return
	}
	if url != projEndpoint(step, owner) || tok != projCall(slice, step, owner) {
		t.Errorf("%s: Endpoint(%s, %q, %s) = %q, %q; want %q, %q (the registration of %q)", what, slice, lookup, step,
			url, tok, projEndpoint(step, owner), projCall(slice, step, owner), owner)
	}
}

func (x *projRig) wantEndpoint(what, lookup, step, owner string) {
	x.t.Helper()
	wantEndpointOf(x.t, x.ws, what, "S-1", lookup, step, owner)
}

// recordRun writes the events of a run of S-1 into project as its workflow would: RunStarted,
// then the kinds given ("Kind" or "Kind=detail").
func recordRun(t *testing.T, sp *spRig, st *store.Store, run, project string, kinds ...string) {
	t.Helper()
	p, err := sp.svc.Pattern()
	if err != nil {
		t.Fatal(err)
	}
	zero := "sha256:" + strings.Repeat("0", 64)
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	for i, k := range append([]string{"RunStarted"}, kinds...) {
		kind, detail, _ := strings.Cut(k, "=")
		e := storedEvent{Event: machineEvent(i+1, at.Add(time.Duration(i)*time.Second), kind, detail)}
		e.Event.Run = run
		if kind == "RunStarted" {
			e.Event.Slice, e.Event.Project, e.Event.Pattern = "S-1", project, p.ID+"@"+p.Version
			e.Event.Inputs = []string{zero, zero} // the base pattern has two slice inputs (a replay needs as many)
			e.Info = &runInfo{Project: project, Operation: "OP-1"}
		}
		data, _ := json.Marshal(e)
		if _, err := st.Append("S-1", store.Event{Run: run, Kind: kind, Data: store.RawJSON(data)}); err != nil {
			t.Fatal(err)
		}
	}
}

const waitingForTheWorkers = "RunInterrupted=the step workers are not running"

// signalsTo is the durable calls that are signals, as "run name".
func signalsOf(f *fakeRuns) []string {
	var out []string
	for _, c := range f.seen() {
		if c.Op == "signal" {
			out = append(out, c.Run+" "+c.Name)
		}
	}
	return out
}

func wantSignals(t *testing.T, what string, f *fakeRuns, want ...string) {
	t.Helper()
	got := signalsOf(f)
	if !slices.Equal(got, want) {
		t.Errorf("%s: signals = %q, want %q", what, got, want)
	}
}

// wantNotReady checks a 409 WorkersNotReady whose detail names the step and the project.
func wantNotReady(t *testing.T, p reply, step, project string) {
	t.Helper()
	pr := p.problem(t, 409, "WorkersNotReady")
	d := ""
	if pr.Detail != nil {
		d = *pr.Detail
	}
	if step != "" && !strings.Contains(d, step) {
		t.Errorf("detail %q does not name the step %s", d, step)
	}
	if !strings.Contains(d, project) {
		t.Errorf("detail %q does not name the project %s", d, project)
	}
}

func (x *projRig) start(project string) reply {
	x.r.clock.Advance(1100 * time.Millisecond)
	return x.r.do("POST", spURL("S-1", "/runs"), clientToken, `{"project":"`+project+`"}`, nil)
}

// ---- two projects of one slice ----

func TestTwoProjectsOfOneSliceRegisterTheirWorkersAndBothStayRegistered(t *testing.T) {
	x := newProjRig(t, projAlpha, projBeta)
	for _, st := range declaredSteps {
		x.regSteps(projAlpha, st)
		x.regSteps(projBeta, st) // registering beta's worker leaves alpha's in place
	}
	for _, st := range declaredSteps {
		x.wantEndpoint("alpha's run", projAlpha, st, projAlpha)
		x.wantEndpoint("beta's run", projBeta, st, projBeta)
	}
}

func TestARunsLookupGivesOnlyItsOwnProjectsEndpoint(t *testing.T) {
	x := newProjRig(t, projAlpha, projBeta)
	x.regSteps(projAlpha, "code")
	x.wantEndpoint("alpha, registered", projAlpha, "code", projAlpha)
	x.wantEndpoint("beta, which has none", projBeta, "code", "none")
	x.wantEndpoint("no project: only a registration without one counts", "", "code", "none")
	x.wantEndpoint("alpha, a step it has none for", projAlpha, "review", "none")
	x.regSteps(projBeta, "code")
	x.wantEndpoint("alpha, after beta registered", projAlpha, "code", projAlpha)
	x.wantEndpoint("beta, registered", projBeta, "code", projBeta)
}

func TestTheAnswerCarriesTheProjectOnlyWhenOneWasSent(t *testing.T) {
	x := newProjRig(t, projAlpha, "")
	got := decodeProjWorker(t, regProject(x.r, projToken("S-1", "tests", projAlpha), "http://t:8080", projAlpha))
	wantProjWorker(t, got, "S-1", "tests", "http://t:8080", projAlpha)
	got = decodeProjWorker(t, regProject(x.r, projToken("S-1", "tests", ""), "http://t:8080", ""))
	wantProjWorker(t, got, "S-1", "tests", "http://t:8080", "")
}

// ---- a registration without a project ----

func TestARegistrationWithoutAProjectServesAnyProjectOfItsSlice(t *testing.T) {
	x := newProjRig(t, "")
	x.regSteps("", "code")
	for _, lookup := range []string{projAlpha, projBeta, projLimit, ""} {
		x.wantEndpoint("project "+lookup, lookup, "code", "")
	}
	wantEndpointOf(t, x.ws, "another slice", "S-2", projAlpha, "code", "none")
}

func TestAProjectsOwnRegistrationIsUsedBeforeOneWithoutAProject(t *testing.T) {
	for _, tc := range []struct {
		name  string
		order []string
	}{{"project-less first", []string{"", projAlpha}}, {"project first", []string{projAlpha, ""}}} {
		t.Run(tc.name, func(t *testing.T) {
			x := newProjRig(t, "", projAlpha)
			for _, p := range tc.order {
				x.regSteps(p, "code")
			}
			x.wantEndpoint("alpha has its own", projAlpha, "code", projAlpha)
			x.wantEndpoint("beta has none of its own", projBeta, "code", "")
			x.wantEndpoint("no project", "", "code", "")
			// Registering the one without a project again does not displace alpha's own.
			x.regSteps("", "code")
			x.wantEndpoint("alpha after the project-less one registered again", projAlpha, "code", projAlpha)
		})
	}
}

// ---- one registration per slice, step and project ----

func TestRegisteringAgainReplacesOnlyTheSameProjectsRegistration(t *testing.T) {
	x := newProjRig(t, "", projAlpha, projBeta)
	x.regSteps("", "code")
	x.regSteps(projAlpha, "code")
	x.regSteps(projBeta, "code")
	// Alpha's worker is rebuilt: the same slice, step and project, a new endpoint.
	again := decodeProjWorker(t, regProject(x.r, projToken("S-1", "code", projAlpha), "http://alpha-new:9090", projAlpha))
	wantProjWorker(t, again, "S-1", "code", "http://alpha-new:9090", projAlpha)
	if url, tok, ok := x.ws.Endpoint("S-1", projAlpha, "code"); !ok || url != "http://alpha-new:9090" || tok != projCall("S-1", "code", projAlpha) {
		t.Errorf("alpha = %q, %q, %v; want the new endpoint with alpha's call token", url, tok, ok)
	}
	x.wantEndpoint("beta unchanged", projBeta, "code", projBeta)
	x.wantEndpoint("the one without a project unchanged", "", "code", "")
}

func TestTwoProjectsMayRegisterTheSameEndpoint(t *testing.T) {
	x := newProjRig(t, projAlpha, projBeta)
	const shared = "http://shared:8080"
	for _, p := range []string{projAlpha, projBeta} {
		wantProjWorker(t, decodeProjWorker(t, regProject(x.r, projToken("S-1", "code", p), shared, p)), "S-1", "code", shared, p)
	}
	for _, p := range []string{projAlpha, projBeta} {
		if url, tok, ok := x.ws.Endpoint("S-1", p, "code"); !ok || url != shared || tok != projCall("S-1", "code", p) {
			t.Errorf("%s = %q, %q, %v; want the shared endpoint with its own call token", p, url, tok, ok)
		}
	}
	// Moving one leaves the other where it was.
	decodeProjWorker(t, regProject(x.r, projToken("S-1", "code", projBeta), "http://beta-moved:8080", projBeta))
	if url, _, ok := x.ws.Endpoint("S-1", projAlpha, "code"); !ok || url != shared {
		t.Errorf("alpha = %q, %v after beta moved; want %q", url, ok, shared)
	}
}

func TestProjectNamesAreCheckedAtTheContractsLimits(t *testing.T) {
	x := newProjRig(t, "")
	tok := projToken("S-1", "code", "")
	for _, ok := range []string{projLimit, "0", "9a", "a-", "a-b-c"} {
		got := decodeProjWorker(t, regProject(x.r, tok, "http://svc:8080", ok))
		wantProjWorker(t, got, "S-1", "code", "http://svc:8080", ok)
	}
	if got := len(projLimit); got != 63 {
		t.Fatalf("test limit name has %d characters, want 63", got)
	}
	for _, bad := range []string{projLimit + "0", "Alpha", "-alpha", "al_pha", "al pha", "alpha/", "../alpha", "alp.ha"} {
		regProject(x.r, tok, "http://svc:8080", bad).problem(t, 400, "InvalidRequest")
		wantEndpointOf(t, x.ws, "after refusing "+bad, "S-1", bad, "code", "none")
	}
	// An empty project is not a project: the field is either a project_name or absent.
	x.r.clock.Advance(1100 * time.Millisecond)
	x.r.do("POST", "/workers?api-version="+apiVersion, tok, `{"endpoint":"http://svc:8080","project":""}`, nil).problem(t, 400, "InvalidRequest")
}

// ---- readiness: startRun, resumeRun, replayRun ----

func TestStartRunIntoAProjectWithNoWorkersOfItsOwnIsWorkersNotReadyNamingStepAndProject(t *testing.T) {
	x := newProjRig(t, projAlpha, projBeta)
	x.regSteps(projAlpha, declaredSteps...) // alpha is complete; beta has none
	wantNotReady(t, x.start(projBeta), "", projBeta)
	x.regSteps(projBeta, declaredSteps[:len(declaredSteps)-1]...) // beta lacks only review
	last := declaredSteps[len(declaredSteps)-1]
	wantNotReady(t, x.start(projBeta), last, projBeta)
	x.sp.noOperation(t)
	if got := x.runs.seen(); len(got) != 0 {
		t.Errorf("durable calls = %v for refused starts, want none", got)
	}
	// Another project's workers do not count, and the project that has every step starts.
	x.regSteps(projBeta, last)
	if op := x.start(projBeta).operation(t, 202); string(op.Kind) != "run" {
		t.Errorf("operation = %+v, want kind run", op)
	}
	if len(x.runs.starts) != 1 || x.runs.starts[0].Project != projBeta {
		t.Errorf("starts = %+v, want one into %s", x.runs.starts, projBeta)
	}
}

func TestStartRunCountsRegistrationsWithoutAProjectOnlyForStepsTheProjectLacks(t *testing.T) {
	last := declaredSteps[len(declaredSteps)-1]
	rest := declaredSteps[:len(declaredSteps)-1]
	t.Run("every step without a project serves any project", func(t *testing.T) {
		x := newProjRig(t, "")
		x.regSteps("", declaredSteps...)
		if op := x.start(projBeta).operation(t, 202); string(op.Kind) != "run" {
			t.Errorf("operation = %+v, want kind run", op)
		}
	})
	t.Run("own for one step, without a project for the rest", func(t *testing.T) {
		x := newProjRig(t, "", projAlpha)
		x.regSteps("", rest...)
		x.regSteps(projAlpha, last)
		wantNotReady(t, x.start(projBeta), last, projBeta) // beta has neither for review
		x.sp.noOperation(t)
		if op := x.start(projAlpha).operation(t, 202); string(op.Kind) != "run" {
			t.Errorf("operation = %+v, want kind run", op)
		}
	})
	t.Run("a project's own steps and none for the rest is not ready", func(t *testing.T) {
		x := newProjRig(t, projAlpha)
		x.regSteps(projAlpha, rest...)
		wantNotReady(t, x.start(projAlpha), last, projAlpha)
	})
}

func TestResumeRunChecksTheWorkersOfTheRunsProject(t *testing.T) {
	last := declaredSteps[len(declaredSteps)-1]
	x := newProjRig(t, "", projAlpha, projBeta)
	recordRun(t, x.sp, x.st, "RUN-1", projAlpha, "RunInterrupted=the model is unavailable")
	resume := func() reply {
		x.r.clock.Advance(1100 * time.Millisecond)
		return x.r.do("POST", "/runs/RUN-1/resume?api-version="+apiVersion, clientToken, "", nil)
	}
	x.regSteps(projBeta, declaredSteps...) // another project's workers do not count
	wantNotReady(t, resume(), "", projAlpha)
	x.regSteps(projAlpha, declaredSteps[:len(declaredSteps)-1]...)
	wantNotReady(t, resume(), last, projAlpha)
	if got := x.runs.seen(); len(got) != 0 {
		t.Fatalf("durable calls = %v for refused resumes, want none", got)
	}
	x.regSteps("", last) // the one without a project serves alpha's last step
	if op := resume().operation(t, 202); op.Target != "RUN-1" {
		t.Errorf("operation = %+v, want a run operation of RUN-1", op)
	}
	if got := x.runs.seen(); len(got) != 1 || got[0] != (durableCall{"signal", "RUN-1", "resume"}) {
		t.Errorf("durable calls = %v, want one resume signal to RUN-1", got)
	}
}

func TestReplayRunChecksTheWorkersOfTheNewProject(t *testing.T) {
	last := declaredSteps[len(declaredSteps)-1]
	replay := func(x *projRig, project string) reply {
		x.r.clock.Advance(1100 * time.Millisecond)
		return x.r.do("POST", "/runs/RUN-1/replay?api-version="+apiVersion, clientToken, `{"project":"`+project+`"}`, nil)
	}
	t.Run("the new project has none", func(t *testing.T) {
		x := newProjRig(t, projAlpha, projBeta)
		recordRun(t, x.sp, x.st, "RUN-1", projAlpha, "RunCompleted")
		x.regSteps(projAlpha, declaredSteps...) // the earlier run's project is complete, the new one is not
		wantNotReady(t, replay(x, projBeta), "", projBeta)
		x.regSteps(projBeta, declaredSteps[:len(declaredSteps)-1]...)
		wantNotReady(t, replay(x, projBeta), last, projBeta)
		if got := x.runs.seen(); len(got) != 0 {
			t.Errorf("durable calls = %v for refused replays, want none", got)
		}
	})
	t.Run("the new project has its workers, the earlier one has none", func(t *testing.T) {
		x := newProjRig(t, projBeta)
		recordRun(t, x.sp, x.st, "RUN-1", projAlpha, "RunCompleted")
		x.regSteps(projBeta, declaredSteps...)
		op := replay(x, projBeta).operation(t, 202)
		if string(op.Kind) != "run" {
			t.Errorf("operation = %+v, want kind run", op)
		}
		if len(x.runs.starts) != 1 || x.runs.starts[0].Project != projBeta {
			t.Errorf("starts = %+v, want one into %s", x.runs.starts, projBeta)
		}
	})
	t.Run("workers without a project serve the new project", func(t *testing.T) {
		x := newProjRig(t, "")
		recordRun(t, x.sp, x.st, "RUN-1", projAlpha, "RunCompleted")
		x.regSteps("", declaredSteps...)
		if op := replay(x, projBeta).operation(t, 202); string(op.Kind) != "run" {
			t.Errorf("operation = %+v, want kind run", op)
		}
	})
}

// ---- workers-ready ----

func TestWorkersReadyGoesOnlyToTheWaitingRunOfTheRegisteringProject(t *testing.T) {
	x := newProjRig(t, projAlpha, projBeta)
	recordRun(t, x.sp, x.st, "RUN-1", projAlpha, waitingForTheWorkers)
	recordRun(t, x.sp, x.st, "RUN-2", projBeta, waitingForTheWorkers)
	x.regSteps(projAlpha, declaredSteps[:len(declaredSteps)-1]...)
	wantSignals(t, "alpha one step short", x.runs)
	x.regSteps(projAlpha, declaredSteps[len(declaredSteps)-1])
	wantSignals(t, "alpha complete: only alpha's run", x.runs, "RUN-1 workers-ready")
	x.regSteps(projBeta, declaredSteps...)
	wantSignals(t, "beta complete: now beta's run, alpha's not again", x.runs, "RUN-1 workers-ready", "RUN-2 workers-ready")
}

func TestWorkersReadyNeedsEveryStepOfTheRunsProjectOwnOrWithoutAProject(t *testing.T) {
	x := newProjRig(t, "", projAlpha)
	recordRun(t, x.sp, x.st, "RUN-1", projAlpha, waitingForTheWorkers)
	recordRun(t, x.sp, x.st, "RUN-2", projBeta, waitingForTheWorkers)
	x.regSteps("", declaredSteps[:2]...)
	wantSignals(t, "two steps without a project", x.runs)
	x.regSteps(projAlpha, declaredSteps[2:]...)
	// Alpha has its own for three steps and the project-less two: complete. Beta has only two.
	wantSignals(t, "alpha complete through the mix, beta not", x.runs, "RUN-1 workers-ready")
}

func TestARegistrationWithoutAProjectWakesTheWaitingRunOfAProjectItCompletes(t *testing.T) {
	x := newProjRig(t, "")
	recordRun(t, x.sp, x.st, "RUN-1", projBeta, waitingForTheWorkers)
	x.regSteps("", declaredSteps[:len(declaredSteps)-1]...)
	wantSignals(t, "one step short", x.runs)
	x.regSteps("", declaredSteps[len(declaredSteps)-1])
	wantSignals(t, "every step without a project", x.runs, "RUN-1 workers-ready")
}

func TestRegisteringAProjectAgainWhileAnotherProjectsRunWaitsSignalsNothing(t *testing.T) {
	x := newProjRig(t, projAlpha, projBeta)
	recordRun(t, x.sp, x.st, "RUN-1", projBeta, waitingForTheWorkers)
	x.regSteps(projAlpha, declaredSteps...)
	again := decodeProjWorker(t, regProject(x.r, projToken("S-1", "code", projAlpha), "http://alpha-new:9090", projAlpha))
	wantProjWorker(t, again, "S-1", "code", "http://alpha-new:9090", projAlpha)
	wantSignals(t, "alpha registered twice, beta's run waiting", x.runs)
	x.regSteps(projBeta, declaredSteps[:len(declaredSteps)-1]...)
	wantSignals(t, "beta one step short", x.runs)
	x.regSteps(projBeta, declaredSteps[len(declaredSteps)-1])
	wantSignals(t, "beta complete", x.runs, "RUN-1 workers-ready")
}

// ---- kept across a restart ----

// grantProject writes the grant file of the slice's step's worker token for the project, with a
// call token of its own, and returns the token.
func grantProject(x *keptRig, step, project string) string {
	x.t.Helper()
	tok := "kept-" + projToken("S-1", step, project)
	writeGrantFile(x.t, x.grants, tok, "S-1", step, "kept-"+projCall("S-1", step, project))
	return tok
}

// regKept grants and registers the slice's step for the project; the answer must be the 200 Worker.
func regKept(x *keptRig, step, project string) {
	x.t.Helper()
	tok := grantProject(x, step, project)
	x.r.clock.Advance(1100 * time.Millisecond)
	wantProjWorker(x.t, decodeProjWorker(x.t, regProject(x.r, tok, projEndpoint(step, project), project)),
		"S-1", step, projEndpoint(step, project), project)
}

// keptRegistration is the registration the store holds for the project.
func keptRegistration(step, project string) store.WorkerRegistration {
	return store.WorkerRegistration{Slice: "S-1", Step: step, Project: project, Endpoint: projEndpoint(step, project),
		Grant: grantFileName("kept-" + projToken("S-1", step, project))}
}

// wantStored compares the store's registrations with want as sets: the order is not pinned.
func wantStored(x *keptRig, what string, want ...store.WorkerRegistration) {
	x.t.Helper()
	got := x.onDisk()
	key := func(r store.WorkerRegistration) string { return r.Slice + "/" + r.Step + "/" + r.Project }
	less := func(a, b store.WorkerRegistration) int { return strings.Compare(key(a), key(b)) }
	slices.SortFunc(got, less)
	slices.SortFunc(want, less)
	if len(got) != len(want) || (len(want) > 0 && !slices.Equal(got, want)) {
		x.t.Errorf("%s: the store keeps %+v, want %+v", what, got, want)
	}
}

// wantKeptEndpoint checks Workers.Endpoint after a restart: the registration kept for owner, whose
// call token is read from owner's grant file.
func wantKeptEndpoint(x *keptRig, what, lookup, step, owner string) {
	x.t.Helper()
	url, tok, ok := x.ws.Endpoint("S-1", lookup, step)
	if owner == "none" {
		if ok {
			x.t.Errorf("%s: Endpoint(S-1, %q, %s) = %q, %q; want not registered", what, lookup, step, url, tok)
		}
		return
	}
	if !ok || url != projEndpoint(step, owner) || tok != "kept-"+projCall("S-1", step, owner) {
		x.t.Errorf("%s: Endpoint(S-1, %q, %s) = %q, %q, %v; want %q, %q (the registration of %q)", what, lookup, step,
			url, tok, ok, projEndpoint(step, owner), "kept-"+projCall("S-1", step, owner), owner)
	}
}

func TestRegistrationsByProjectSurviveAnEngineRestart(t *testing.T) {
	x := newKeptRig(t, 1, nil, nil)
	regKept(x, "code", projAlpha)
	regKept(x, "code", projBeta)
	regKept(x, "review", projAlpha)
	regKept(x, "tests", "")
	wantStored(x, "after registering", keptRegistration("code", projAlpha), keptRegistration("code", projBeta),
		keptRegistration("review", projAlpha), keptRegistration("tests", ""))
	for i := 0; i < 2; i++ { // a second restart reads the same
		x.restart()
		wantKeptEndpoint(x, "alpha's code", projAlpha, "code", projAlpha)
		wantKeptEndpoint(x, "beta's code", projBeta, "code", projBeta)
		wantKeptEndpoint(x, "alpha's review", projAlpha, "review", projAlpha)
		wantKeptEndpoint(x, "beta has no review", projBeta, "review", "none")
		wantKeptEndpoint(x, "the one without a project serves alpha", projAlpha, "tests", "")
		wantKeptEndpoint(x, "the one without a project serves beta", projBeta, "tests", "")
		wantKeptEndpoint(x, "no project has no code", "", "code", "none")
	}
}

func TestAKeptRegistrationWithoutAProjectServesAProjectRunAfterARestart(t *testing.T) {
	// Registrations kept before this change have no project; a run into a project still finds them.
	x := newKeptRig(t, 1, &fakeRuns{exists: map[string]bool{}}, nil)
	for _, st := range declaredSteps {
		regKept(x, st, "")
	}
	x.restart()
	wantKeptEndpoint(x, "a project's run", projAlpha, "code", "")
	x.r.clock.Advance(1100 * time.Millisecond)
	op := x.r.do("POST", spURL("S-1", "/runs"), clientToken, `{"project":"alpha-app"}`, nil).operation(t, 202)
	if string(op.Kind) != "run" {
		t.Fatalf("operation = %+v, want kind run", op)
	}
	if got := x.runs.seen(); len(got) != 1 || got[0].Op != "start" {
		t.Errorf("durable calls = %v, want one start", got)
	}
}

func TestAfterARestartStartRunCountsKeptWorkersOfTheRunsProjectOnly(t *testing.T) {
	x := newKeptRig(t, 1, &fakeRuns{exists: map[string]bool{}}, nil)
	for _, st := range declaredSteps {
		regKept(x, st, projAlpha)
	}
	x.restart()
	x.r.clock.Advance(1100 * time.Millisecond)
	wantNotReady(t, x.r.do("POST", spURL("S-1", "/runs"), clientToken, `{"project":"beta"}`, nil), "", projBeta)
	if got := x.runs.seen(); len(got) != 0 {
		t.Fatalf("durable calls = %v for a refused start, want none", got)
	}
	x.r.clock.Advance(1100 * time.Millisecond)
	if op := x.r.do("POST", spURL("S-1", "/runs"), clientToken, `{"project":"alpha"}`, nil).operation(t, 202); string(op.Kind) != "run" {
		t.Errorf("operation = %+v, want kind run", op)
	}
}

func TestAFailedStoreWriteWhileRegisteringAProjectKeepsNothingNewAndEveryEarlierOne(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a mode 0500 folder; this case needs a non-root user")
	}
	x := newKeptRig(t, 1, nil, nil)
	regKept(x, "code", "")
	regKept(x, "code", projAlpha)
	betaTok := grantProject(x, "code", projBeta)
	alphaTok := grantProject(x, "code", projAlpha)
	if err := os.Chmod(x.dir, 0o500); err != nil { //nolint:gosec // a folder needs its search bit; no temporary file can be made
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(x.dir, 0o700) }) //nolint:gosec // a folder needs its search bit
	unavailable(t, regProject(x.r, betaTok, projEndpoint("code", projBeta), projBeta))
	unavailable(t, regProject(x.r, alphaTok, "http://alpha-new:9090", projAlpha)) // and an earlier one stays as it was
	if err := os.Chmod(x.dir, 0o700); err != nil {                                //nolint:gosec // a folder needs its search bit
		t.Fatal(err)
	}
	wantKeptEndpoint(x, "beta's, refused: the one without a project", projBeta, "code", "")
	wantKeptEndpoint(x, "alpha's, refused: its earlier one", projAlpha, "code", projAlpha)
	wantStored(x, "after the 503s", keptRegistration("code", ""), keptRegistration("code", projAlpha))
	x.restart()
	wantKeptEndpoint(x, "beta after a restart", projBeta, "code", "")
	wantKeptEndpoint(x, "alpha after a restart", projAlpha, "code", projAlpha)
}

func TestAFailedWorkersReadySignalWhileRegisteringAProjectPutsThatProjectBackAndNoOther(t *testing.T) {
	x := newKeptRig(t, 1, &fakeRuns{exists: map[string]bool{}}, nil)
	recordRun(t, x.sp, x.st, "RUN-1", projBeta, waitingForTheWorkers)
	last := declaredSteps[len(declaredSteps)-1]
	var want []store.WorkerRegistration
	for _, st := range declaredSteps {
		regKept(x, st, projAlpha) // alpha's run is not waiting: nothing is signalled
		want = append(want, keptRegistration(st, projAlpha))
	}
	for _, st := range declaredSteps[:len(declaredSteps)-1] {
		regKept(x, st, projBeta)
		want = append(want, keptRegistration(st, projBeta))
	}
	if got := signalsOf(x.runs); len(got) != 0 {
		t.Fatalf("signals = %q before beta's last step, want none", got)
	}
	tok := grantProject(x, last, projBeta)
	x.runs.setDown(true)
	unavailable(t, regProject(x.r, tok, projEndpoint(last, projBeta), projBeta))
	wantKeptEndpoint(x, "beta's last step, refused", projBeta, last, "none")
	wantKeptEndpoint(x, "alpha's last step, untouched", projAlpha, last, projAlpha)
	wantStored(x, "after the 503", want...)
	x.restart()
	wantKeptEndpoint(x, "beta's last step after a restart", projBeta, last, "none")
	wantKeptEndpoint(x, "alpha's last step after a restart", projAlpha, last, projAlpha)
}

func TestDeleteWorkerRegistrationRemovesOnlyThatProjectsRegistration(t *testing.T) {
	x := newKeptRig(t, 1, nil, nil)
	for _, p := range []string{"", projAlpha, projBeta} {
		if err := x.st.PutWorkerRegistration(keptRegistration("code", p)); err != nil {
			t.Fatalf("PutWorkerRegistration(%q): %v", p, err)
		}
	}
	wantStored(x, "after three puts", keptRegistration("code", ""), keptRegistration("code", projAlpha), keptRegistration("code", projBeta))
	moved := keptRegistration("code", projAlpha)
	moved.Endpoint = "http://alpha-moved:8080"
	if err := x.st.PutWorkerRegistration(moved); err != nil { // replaces alpha's only
		t.Fatal(err)
	}
	wantStored(x, "after replacing alpha's", keptRegistration("code", ""), moved, keptRegistration("code", projBeta))
	if err := x.st.DeleteWorkerRegistration("S-1", "code", projAlpha); err != nil {
		t.Fatal(err)
	}
	wantStored(x, "after deleting alpha's", keptRegistration("code", ""), keptRegistration("code", projBeta))
	if err := x.st.DeleteWorkerRegistration("S-1", "code", ""); err != nil { // only the one without a project
		t.Fatal(err)
	}
	wantStored(x, "after deleting the one without a project", keptRegistration("code", projBeta))
	if err := x.st.DeleteWorkerRegistration("S-1", "code", projAlpha); err != nil { // none is no error
		t.Errorf("deleting a registration that is not there: %v", err)
	}
}

// ---- erasing the slice ----

func TestDeleteSliceRemovesEveryProjectsRegistrationsInMemoryAndInTheStore(t *testing.T) {
	x := newKeptRig(t, 2, nil, nil)
	regKept(x, "code", projAlpha)
	regKept(x, "code", projBeta)
	regKept(x, "code", "")
	regKept(x, "review", projAlpha)
	// Another slice's registration, which the erasure keeps.
	x.grant("S-2", "code")
	x.r.clock.Advance(1100 * time.Millisecond)
	decodeProjWorker(t, regProject(x.r, keptWorkerToken("S-2", "code"), "http://s2-code:8080", projAlpha))
	x.r.clock.Advance(1100 * time.Millisecond)
	op := x.r.do("DELETE", spURL("S-1", ""), clientToken, "", nil).operation(t, 202)
	x.r.waitFor(t, op.Id, "succeeded")
	for _, p := range []string{projAlpha, projBeta, projLimit, ""} {
		wantKeptEndpoint(x, "S-1 after the erasure, project "+p, p, "code", "none")
		wantKeptEndpoint(x, "S-1 after the erasure, project "+p, p, "review", "none")
	}
	got := x.onDisk()
	if len(got) != 1 || got[0].Slice != "S-2" || got[0].Project != projAlpha {
		t.Errorf("the store keeps %+v, want only S-2's registration for %s", got, projAlpha)
	}
	x.restart()
	wantKeptEndpoint(x, "S-1 after the erasure and a restart", projAlpha, "code", "none")
	if url, _, ok := x.ws.Endpoint("S-2", projAlpha, "code"); !ok || url != "http://s2-code:8080" {
		t.Errorf("S-2's registration = %q, %v after S-1's erasure and a restart; want it kept", url, ok)
	}
}

func TestAProjectsRegistrationErasedWithItsSliceInTheStoreIsNotUsedAfterARestart(t *testing.T) {
	x := newKeptRig(t, 1, nil, nil)
	regKept(x, "code", projAlpha)
	regKept(x, "code", projBeta)
	if err := x.st.EraseSlice("S-1"); err != nil {
		t.Fatalf("EraseSlice(S-1): %v", err)
	}
	wantStored(x, "after EraseSlice")
	x.restart()
	wantKeptEndpoint(x, "alpha", projAlpha, "code", "none")
	wantKeptEndpoint(x, "beta", projBeta, "code", "none")
}
