package api

// T043, engine side: step worker registrations outlive an engine restart (ADR 0071, data model
// "Step worker registrations", research R10, the contract's registerWorker). Written before the
// code (T045) by a test author; the existing rigs and helpers are used as they are (newSP,
// newRig, register, decodeWorker, writeGrantFile, grantFileName, unavailable, fakeRuns,
// durableRig.record, declaredSteps, endpointFor, runBody).
//
// Interface assumed, fixed by the task brief: package api's signatures do not change. With
// Config.Store a *store.Store and Config.GrantsDir set:
//   - registerWorker, by a worker token whose grant file is in GrantsDir, puts the registration
//     (slice, step, endpoint, the grant file's name) in the store before it answers 200;
//   - a new handler over the same store directory and grants directory, with a fresh
//     Config.Workers (an engine restart), gives the same endpoint from Workers.Endpoint and the
//     call token read from the grant file at that call;
//   - a registration whose grant file is gone counts as not registered;
//   - a failed store write answers 503 UnavailableProblem and keeps nothing;
//   - a write followed by a failing workers-ready signal answers 503 and leaves workers.json with
//     the earlier registration, or none;
//   - a registration made with a start-time Config.WorkerGrants token is not kept (a stated limit);
//   - once the slice is erased none of its registrations is kept or used.
//
// Assumed beyond the brief's list, from the ADR ("Revoking a grant (removing its file) still stops
// calls at once") and the data model ("a call reads the call token from the grant file"): on a
// running engine too, removing a grant file makes Workers.Endpoint report the step as not
// registered, and startRun's readiness counts a kept registration only while its grant file is
// there.

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/store"
)

// ---- the rig ----

// keptRig is an engine whose run store is a real store in dir and whose step workers register
// with grant files in grants. restart makes a new handler over the same directories, as an
// engine restart does: a newly opened store and a fresh Workers.
type keptRig struct {
	t      *testing.T
	sp     *spRig
	dir    string // the store's directory
	grants string // the grants folder
	st     *store.Store
	ws     *Workers
	r      *rig
	runs   *fakeRuns // the durable service; nil: none
	mutate func(*Config)
}

// newKeptRig makes S-1 (approved) and, for slices > 1, S-2 ... (seeded) and boots the engine.
func newKeptRig(t *testing.T, slices int, runs *fakeRuns, mutate func(*Config)) *keptRig {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir, spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	var results []spResult
	for i := 0; i < slices; i++ {
		results = append(results, spResult{p: spNotes(t)})
	}
	sp := newSP(t, results, st)
	sp.approveAfterSeed(t) // S-1
	for i := 1; i < slices; i++ {
		sp.seed(t, "Let me keep more notes.")
	}
	x := &keptRig{t: t, sp: sp, dir: dir, grants: t.TempDir(), st: st, runs: runs, mutate: mutate}
	x.boot()
	return x
}

func (x *keptRig) boot() {
	x.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	x.t.Cleanup(cancel)
	x.ws = NewWorkers()
	x.r = newRig(x.t, func(c *Config) {
		c.Context = ctx
		c.Slices, c.Store = x.sp.svc, x.st
		c.GrantsDir = x.grants
		c.Workers = x.ws
		if x.runs != nil {
			c.Runs = x.runs
		}
		if x.mutate != nil {
			x.mutate(c)
		}
	})
}

// restart opens the store again and makes a new handler over it with a fresh Workers; the
// earlier handler and store are not used again.
func (x *keptRig) restart() {
	x.t.Helper()
	st, err := store.Open(x.dir, spSchemas(x.t))
	if err != nil {
		x.t.Fatalf("reopening the store: %v", err)
	}
	x.st = st
	x.boot()
}

func keptWorkerToken(slice, step string) string { return "kept-worker-token-" + slice + "-" + step }
func keptCallToken(slice, step string) string   { return "kept-call-token-" + slice + "-" + step }

// grant writes the grant file of the slice's step's worker token, with its call token.
func (x *keptRig) grant(slice, step string) string {
	x.t.Helper()
	return writeGrantFile(x.t, x.grants, keptWorkerToken(slice, step), slice, step, keptCallToken(slice, step))
}

// reg registers the slice's step at endpoint with its grant file's token.
func (x *keptRig) reg(slice, step, endpoint string) reply {
	x.r.clock.Advance(1100 * time.Millisecond) // stay under every rate limit
	return register(x.r, keptWorkerToken(slice, step), endpoint)
}

// regOK grants and registers the slice's step at endpoint; the answer must be the 200 Worker.
func (x *keptRig) regOK(slice, step, endpoint string) {
	x.t.Helper()
	x.grant(slice, step)
	got := decodeWorker(x.t, x.reg(slice, step, endpoint))
	if want := (workerBody{slice, step, endpoint}); got != want {
		x.t.Fatalf("Worker = %+v, want %+v", got, want)
	}
}

// onDisk is what a newly opened store over the directory keeps: what the engine reads at start.
func (x *keptRig) onDisk() []store.WorkerRegistration {
	x.t.Helper()
	st, err := store.Open(x.dir, spSchemas(x.t))
	if err != nil {
		x.t.Fatalf("opening the store to read it: %v", err)
	}
	got, err := st.WorkerRegistrations()
	if err != nil {
		x.t.Fatalf("WorkerRegistrations: %v", err)
	}
	return got
}

func (x *keptRig) wantOnDisk(what string, want ...store.WorkerRegistration) {
	x.t.Helper()
	got := x.onDisk()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		x.t.Errorf("%s: the store keeps %+v, want %+v", what, got, want)
	}
}

func kept(slice, step, endpoint string) store.WorkerRegistration {
	return store.WorkerRegistration{Slice: slice, Step: step, Endpoint: endpoint, Grant: grantFileName(keptWorkerToken(slice, step))}
}

// wantEndpoint checks Workers.Endpoint for the slice's step: the endpoint and the call token of
// its grant file, or not registered when endpoint is "".
func (x *keptRig) wantEndpoint(what, slice, step, endpoint, callToken string) {
	x.t.Helper()
	url, tok, ok := x.ws.Endpoint(slice, step)
	switch {
	case endpoint == "" && ok:
		x.t.Errorf("%s: Endpoint(%s, %s) = %q, %q, registered; want not registered", what, slice, step, url, tok)
	case endpoint == "":
	case !ok:
		x.t.Errorf("%s: Endpoint(%s, %s) is not registered; want %q", what, slice, step, endpoint)
	case url != endpoint || tok != callToken:
		x.t.Errorf("%s: Endpoint(%s, %s) = %q, %q; want %q, %q", what, slice, step, url, tok, endpoint, callToken)
	}
}

// ---- kept before the answer, used after a restart ----

func TestARegistrationIsInTheStoreWhenRegisterWorkerAnswers(t *testing.T) {
	x := newKeptRig(t, 1, nil, nil)
	x.regOK("S-1", "code", "http://code-worker:8080")
	x.wantOnDisk("after the 200", kept("S-1", "code", "http://code-worker:8080"))
}

func TestAfterARestartTheKeptEndpointIsUsedWithTheGrantFilesCallToken(t *testing.T) {
	x := newKeptRig(t, 1, nil, nil)
	x.regOK("S-1", "code", "http://code-worker:8080")
	x.regOK("S-1", "review", "http://review-worker:8080")
	x.restart()
	x.wantEndpoint("after a restart", "S-1", "code", "http://code-worker:8080", keptCallToken("S-1", "code"))
	x.wantEndpoint("after a restart", "S-1", "review", "http://review-worker:8080", keptCallToken("S-1", "review"))
	x.wantEndpoint("a step never registered", "S-1", "tests", "", "")
	// The call token comes from the grant file at each call: the launcher replaces it, the next
	// call has the new one.
	writeGrantFile(t, x.grants, keptWorkerToken("S-1", "code"), "S-1", "code", "a-replaced-call-token")
	x.wantEndpoint("after the grant file was replaced", "S-1", "code", "http://code-worker:8080", "a-replaced-call-token")
	// A second restart reads the same.
	x.restart()
	x.wantEndpoint("after a second restart", "S-1", "review", "http://review-worker:8080", keptCallToken("S-1", "review"))
}

func TestRegisteringAgainReplacesTheKeptRegistration(t *testing.T) {
	x := newKeptRig(t, 1, nil, nil)
	x.regOK("S-1", "tests", "http://old-tests:8080")
	x.regOK("S-1", "tests", "http://new-tests:9090")
	x.wantOnDisk("after registering again", kept("S-1", "tests", "http://new-tests:9090"))
	x.restart()
	x.wantEndpoint("after a restart", "S-1", "tests", "http://new-tests:9090", keptCallToken("S-1", "tests"))
}

func TestARegistrationWithANewWorkerTokenReplacesTheOneOfTheOldToken(t *testing.T) {
	// `ap build-workers --resume` makes a new worker token, and grant file, for a step.
	x := newKeptRig(t, 1, nil, nil)
	old := x.grant("S-1", "code")
	decodeWorker(t, x.reg("S-1", "code", "http://code-1:8080"))
	const newTok, newCall = "kept-worker-token-S-1-code-second", "kept-call-token-S-1-code-second"
	writeGrantFile(t, x.grants, newTok, "S-1", "code", newCall)
	x.r.clock.Advance(1100 * time.Millisecond)
	decodeWorker(t, register(x.r, newTok, "http://code-2:8080"))
	x.wantOnDisk("after the new token registered",
		store.WorkerRegistration{Slice: "S-1", Step: "code", Endpoint: "http://code-2:8080", Grant: grantFileName(newTok)})
	if err := os.Remove(old); err != nil { // the old grant is revoked; the new one stands
		t.Fatal(err)
	}
	x.restart()
	x.wantEndpoint("after a restart", "S-1", "code", "http://code-2:8080", newCall)
}

func TestTheStoreHoldsNeitherTheWorkerTokenNorTheCallToken(t *testing.T) {
	x := newKeptRig(t, 1, nil, nil)
	for _, st := range declaredSteps {
		x.regOK("S-1", st, endpointFor(st))
	}
	x.wantOnDisk("after registering every step",
		kept("S-1", "code", endpointFor("code")), kept("S-1", "contract-check", endpointFor("contract-check")),
		kept("S-1", "generate", endpointFor("generate")), kept("S-1", "review", endpointFor("review")),
		kept("S-1", "tests", endpointFor("tests")))
	b, err := os.ReadFile(filepath.Join(x.dir, "workers.json"))
	if err != nil {
		t.Fatalf("the registrations are not in workers.json: %v", err)
	}
	for _, st := range declaredSteps {
		if !bytes.Contains(b, []byte(grantFileName(keptWorkerToken("S-1", st)))) {
			t.Errorf("workers.json lacks the grant hash of %s: %s", st, b)
		}
	}
	err = filepath.WalkDir(x.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		c, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, st := range declaredSteps {
			for _, tok := range []string{keptWorkerToken("S-1", st), keptCallToken("S-1", st)} {
				if bytes.Contains(c, []byte(tok)) || strings.Contains(p, tok) {
					t.Errorf("%s holds the token %q", p, tok)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ---- a removed grant ----

func TestAKeptRegistrationWhoseGrantFileIsGoneCountsAsNotRegistered(t *testing.T) {
	x := newKeptRig(t, 1, nil, nil)
	x.regOK("S-1", "code", "http://code-worker:8080")
	gone := x.grant("S-1", "review")
	decodeWorker(t, x.reg("S-1", "review", "http://review-worker:8080"))
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	x.restart()
	x.wantEndpoint("its grant file removed", "S-1", "review", "", "")
	x.wantEndpoint("its grant file kept", "S-1", "code", "http://code-worker:8080", keptCallToken("S-1", "code"))
}

func TestRemovingAGrantFileUnregistersItsStepOnARunningEngine(t *testing.T) {
	x := newKeptRig(t, 1, nil, nil)
	p := x.grant("S-1", "tests")
	decodeWorker(t, x.reg("S-1", "tests", "http://tests-worker:8080"))
	x.wantEndpoint("registered", "S-1", "tests", "http://tests-worker:8080", keptCallToken("S-1", "tests"))
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	x.wantEndpoint("its grant file removed", "S-1", "tests", "", "")
}

func TestAfterARestartStartRunCountsKeptWorkersWhileTheirGrantFilesAreThere(t *testing.T) {
	x := newKeptRig(t, 1, &fakeRuns{exists: map[string]bool{}}, nil)
	for _, st := range declaredSteps {
		x.regOK("S-1", st, endpointFor(st))
	}
	// One grant removed: the restarted engine is missing that step's worker.
	last := declaredSteps[len(declaredSteps)-1]
	p := filepath.Join(x.grants, grantFileName(keptWorkerToken("S-1", last)))
	held, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	x.restart()
	x.r.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).problem(t, 409, "WorkersNotReady")
	if got := x.runs.seen(); len(got) != 0 {
		t.Fatalf("durable calls = %v, want none for a refused start", got)
	}
	// The grant back: every step has its kept worker, and the run starts without any worker
	// registering again.
	if err := os.WriteFile(p, held, 0o600); err != nil {
		t.Fatal(err)
	}
	x.restart()
	x.r.clock.Advance(1100 * time.Millisecond)
	op := x.r.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).operation(t, 202)
	if string(op.Kind) != "run" {
		t.Fatalf("operation = %+v, want kind run", op)
	}
	if got := x.runs.seen(); len(got) != 1 || got[0].Op != "start" {
		t.Errorf("durable calls = %v, want one start", got)
	}
}

// ---- nothing kept on a 503 ----

func TestAFailedStoreWriteIs503AndKeepsNothing(t *testing.T) {
	x := newKeptRig(t, 1, nil, nil)
	x.grant("S-1", "code")
	// A folder where workers.json must go: no file can be renamed over it.
	planted := filepath.Join(x.dir, "workers.json")
	if err := os.Mkdir(planted, 0o700); err != nil {
		t.Fatal(err)
	}
	w := x.reg("S-1", "code", "http://code-worker:8080")
	unavailable(t, w)
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
	x.wantEndpoint("after the 503", "S-1", "code", "", "")
	if err := os.Remove(planted); err != nil {
		t.Fatal(err)
	}
	if got, err := x.st.WorkerRegistrations(); err != nil || len(got) != 0 {
		t.Errorf("the running engine's store keeps %+v, %v after the 503; want nothing", got, err)
	}
	x.wantOnDisk("after the 503")
	// The same request again, once the store can write, registers it.
	decodeWorker(t, x.reg("S-1", "code", "http://code-worker:8080"))
	x.wantOnDisk("registering again", kept("S-1", "code", "http://code-worker:8080"))
}

func TestAFailedStoreWriteIs503AndKeepsTheEarlierRegistration(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a mode 0500 folder; this case needs a non-root user")
	}
	x := newKeptRig(t, 1, nil, nil)
	x.regOK("S-1", "code", "http://code-1:8080")
	// The store's folder cannot take a new file: no temporary file can be made.
	if err := os.Chmod(x.dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(x.dir, 0o700) })
	unavailable(t, x.reg("S-1", "code", "http://code-2:8080"))
	if err := os.Chmod(x.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	x.wantEndpoint("after the 503", "S-1", "code", "http://code-1:8080", keptCallToken("S-1", "code"))
	x.wantOnDisk("after the 503", kept("S-1", "code", "http://code-1:8080"))
	x.restart()
	x.wantEndpoint("after the 503 and a restart", "S-1", "code", "http://code-1:8080", keptCallToken("S-1", "code"))
}

// waitingForWorkers records RUN-1 of S-1 as interrupted, waiting for the slice's step workers.
func (x *keptRig) waitingForWorkers() {
	x.t.Helper()
	(&durableRig{t: x.t, sp: x.sp, st: x.st}).record("RunInterrupted=the step workers are not running")
}

func TestAFailedWorkersReadySignalIs503AndKeepsNoNewRegistration(t *testing.T) {
	x := newKeptRig(t, 1, &fakeRuns{exists: map[string]bool{}}, nil)
	x.waitingForWorkers()
	first, last := declaredSteps[:len(declaredSteps)-1], declaredSteps[len(declaredSteps)-1]
	var want []store.WorkerRegistration
	for _, st := range first {
		x.regOK("S-1", st, endpointFor(st))
		want = append(want, kept("S-1", st, endpointFor(st)))
	}
	if got := x.runs.seen(); len(got) != 0 {
		t.Fatalf("durable calls = %v before the last step registered, want none", got)
	}
	x.runs.setDown(true)
	x.grant("S-1", last)
	unavailable(t, x.reg("S-1", last, endpointFor(last)))
	x.wantEndpoint("after the 503", "S-1", last, "", "")
	sortKept(want)
	x.wantOnDisk("after the 503", want...)
	x.restart()
	x.wantEndpoint("after the 503 and a restart", "S-1", last, "", "")
}

func TestAFailedWorkersReadySignalIs503AndKeepsTheEarlierRegistration(t *testing.T) {
	x := newKeptRig(t, 1, &fakeRuns{exists: map[string]bool{}}, nil)
	x.waitingForWorkers()
	var want []store.WorkerRegistration
	for _, st := range declaredSteps {
		x.regOK("S-1", st, endpointFor(st)) // the last one signals workers-ready
		want = append(want, kept("S-1", st, endpointFor(st)))
	}
	if got := x.runs.seen(); len(got) != 1 || got[0] != (durableCall{"signal", "RUN-1", "workers-ready"}) {
		t.Fatalf("durable calls = %v, want one workers-ready signal to RUN-1", got)
	}
	// RUN-1's record still shows it waiting, so registering again signals again; that fails.
	x.runs.setDown(true)
	last := declaredSteps[len(declaredSteps)-1]
	unavailable(t, x.reg("S-1", last, "http://moved:9090"))
	x.wantEndpoint("after the 503", "S-1", last, endpointFor(last), keptCallToken("S-1", last))
	sortKept(want)
	x.wantOnDisk("after the 503", want...)
	x.restart()
	x.wantEndpoint("after the 503 and a restart", "S-1", last, endpointFor(last), keptCallToken("S-1", last))
}

// sortKept sorts registrations by slice then step, as WorkerRegistrations lists them.
func sortKept(rs []store.WorkerRegistration) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0; j-- {
			a, b := rs[j-1], rs[j]
			if a.Slice < b.Slice || (a.Slice == b.Slice && a.Step <= b.Step) {
				break
			}
			rs[j-1], rs[j] = b, a
		}
	}
}

// ---- not kept ----

func TestARegistrationWithAStartTimeGrantIsNotKept(t *testing.T) {
	const tok = "start-time-worker-token-tests-0001"
	grants := map[string]WorkerGrant{tok: {Slice: "S-1", Step: "tests", CallToken: "start-time-call-token"}}
	x := newKeptRig(t, 1, nil, func(c *Config) { c.WorkerGrants = grants })
	decodeWorker(t, register(x.r, tok, "http://tests-worker:8080"))
	// It works on this engine, as before.
	if url, call, ok := x.ws.Endpoint("S-1", "tests"); !ok || url != "http://tests-worker:8080" || call != "start-time-call-token" {
		t.Errorf("Endpoint(S-1, tests) = %q, %q, %v; want the registration on the running engine", url, call, ok)
	}
	x.wantOnDisk("a start-time grant's registration")
	if anyFileHolds(t, x.dir, tok) || anyFileHolds(t, x.dir, "start-time-call-token") {
		t.Error("the store holds a start-time grant's token")
	}
	x.restart() // with the same start-time grants
	x.wantEndpoint("after a restart", "S-1", "tests", "", "")
}

func anyFileHolds(t *testing.T, dir, needle string) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		c, err := os.ReadFile(p)
		found = found || bytes.Contains(c, []byte(needle))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// ---- erasure ----

func TestDeleteSliceRemovesItsKeptRegistrationsAndKeepsAnothers(t *testing.T) {
	x := newKeptRig(t, 2, nil, nil)
	x.regOK("S-1", "code", "http://s1-code:8080")
	x.regOK("S-1", "review", "http://s1-review:8080")
	x.regOK("S-2", "code", "http://s2-code:8080")
	x.r.clock.Advance(1100 * time.Millisecond)
	op := x.r.do("DELETE", spURL("S-1", ""), clientToken, "", nil).operation(t, 202)
	x.r.waitFor(t, op.Id, "succeeded")
	x.wantOnDisk("after deleteSlice S-1", kept("S-2", "code", "http://s2-code:8080"))
	// S-1's grant files are still there (`ap down` removes them); the erasure alone stops their use.
	x.restart()
	x.wantEndpoint("an erased slice's step", "S-1", "code", "", "")
	x.wantEndpoint("an erased slice's step", "S-1", "review", "", "")
	x.wantEndpoint("another slice's step", "S-2", "code", "http://s2-code:8080", keptCallToken("S-2", "code"))
}

func TestARegistrationErasedWithItsSliceInTheStoreIsNotUsedAfterARestart(t *testing.T) {
	x := newKeptRig(t, 2, nil, nil)
	x.regOK("S-1", "code", "http://s1-code:8080")
	x.regOK("S-2", "tests", "http://s2-tests:8080")
	if err := x.st.EraseSlice("S-1"); err != nil {
		t.Fatalf("EraseSlice(S-1): %v", err)
	}
	x.wantOnDisk("after EraseSlice(S-1)", kept("S-2", "tests", "http://s2-tests:8080"))
	x.restart()
	x.wantEndpoint("an erased slice's step", "S-1", "code", "", "")
	x.wantEndpoint("another slice's step", "S-2", "tests", "http://s2-tests:8080", keptCallToken("S-2", "tests"))
}
