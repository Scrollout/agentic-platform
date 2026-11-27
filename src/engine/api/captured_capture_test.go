package api

// T005 (feature 011, user story 1; FR-001 to FR-003, FR-006, FR-007, FR-012, FR-014): capturing a
// run as a captured pattern and changing its parameters, through the engine's HTTP API, on the
// run rig of runs_ops_test.go (faked step workers and checker, no model). Judged from the engine
// contract (tag captured-patterns), the data model and the contract draft, never from an
// implementation. Written before the code (T007) by a test author; the code author never changes
// it (ADR 0029).
//
// What the rig adds to runsRig:
//   - a worker that writes each step's files as a framed file set (the artifact form of a step
//     output), so a template can be read from the run (capFiles);
//   - a store wrapper (gateStore) that can hold or fail a read of an artifact, to hold a
//     captured-pattern operation open (CapturedPatternBusy) and to fail one (a failed capture
//     leaves no captured pattern). The only seam it uses is the artifact read every
//     implementation needs to read a template;
//   - cloneRun, which copies a recorded run under a new ID with a change to its RunStarted, to
//     make the runs a capture refuses (older records, other base version, a captured pattern's or
//     a proof's run) without changing any existing file.
//
// Not observable through the API, so not tested here: the captured pattern's declaration (it is
// tested as captured.Declaration's output, in TestCapturedDeclaration...), which `getCapturedPattern`
// does not return; makeProofSlice, withdrawal and erasure (T027, T018); proofs (T006).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/captured"
	"github.com/agentic-platform/engine/machine"
	slc "github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// ---- the template the faked workers write ----

const (
	capTestsPath   = "tests/notes_test.go"
	capWitnessPath = "review/r1/empty_test.go"
)

// capTestsFile has the name `note` in all three case forms, inside longer words (`note_id`,
// `denote`), and `notes` in its path and in `TestNotes` (longest first: `Notes` is not a `note`).
// Lines and columns (from 1): NOTE 3:4, Note 3:12, note_id 3:26, denote's note 3:40, Notes 4:10.
func capTestsFile(extra string) canonical.File {
	return canonical.File{Path: capTestsPath, Content: []byte("package tests\n\n// NOTE: a Note is kept; note_id and denote too.\nfunc TestNotes() {}\n" + extra)}
}

func capCodeFiles(fixed bool) []canonical.File {
	body := "func NewNote() {}\n"
	if fixed {
		body = "func NewNote() { /* the fix of review round 1 */ }\n"
	}
	return []canonical.File{
		{Path: "internal/app/notes.go", Content: []byte("package app\n\n" + body)},
		{Path: "cmd/notes/main.go", Content: []byte("package main\n\nfunc main() {}\n")},
	}
}

func capWitnessFiles() []canonical.File {
	return []canonical.File{{Path: capWitnessPath, Content: []byte("package r1\n\nfunc TestR1EmptyText() {}\n")}}
}

// capOutputs maps each step output of the base pattern to the path prefixes of the files it holds
// in the project (the step's writable entries).
var capOutputs = map[string][]string{"tests": {"tests/"}, "code": {"internal/", "cmd/"}, "witnesses": {"review/"}}

func capFramedHash(files []canonical.File) string { return canonical.FramedHash(files) }

// ---- the rig ----

// gateStore is the run store with a held or failing artifact read.
type gateStore struct {
	*store.Store
	mu      sync.Mutex
	hold    chan struct{} // non-nil: Artifact blocks until it is closed
	reading chan struct{} // closed by the first Artifact call that blocks
	once    *sync.Once
	fail    error
}

func (g *gateStore) Artifact(hash string) ([]byte, error) {
	g.mu.Lock()
	hold, reading, once, fail := g.hold, g.reading, g.once, g.fail
	g.mu.Unlock()
	if fail != nil {
		return nil, fail
	}
	if hold != nil {
		once.Do(func() { close(reading) })
		select {
		case <-hold:
		case <-time.After(20 * time.Second):
			return nil, errors.New("gateStore: held for 20 s")
		}
	}
	return g.Store.Artifact(hash)
}

// holdReads makes every Artifact read block until release; blocked is closed when one blocks.
func (g *gateStore) holdReads(t *testing.T) (blocked <-chan struct{}, release func()) {
	t.Helper()
	g.mu.Lock()
	g.hold, g.reading, g.once = make(chan struct{}), make(chan struct{}), new(sync.Once)
	hold, reading := g.hold, g.reading
	g.mu.Unlock()
	var once sync.Once
	release = func() {
		once.Do(func() {
			g.mu.Lock()
			g.hold = nil
			g.mu.Unlock()
			close(hold)
		})
	}
	t.Cleanup(release)
	return reading, release
}

func (g *gateStore) failReads(err error) {
	g.mu.Lock()
	g.fail = err
	g.mu.Unlock()
}

type capRig struct {
	t  *testing.T
	x  *runsRig
	gs *gateStore

	mu        *sync.Mutex
	byRun     map[string]map[string][]canonical.File // run -> step output -> its files in the project
	fixed     map[string]bool                        // runs whose code step has made its review fix
	extraTest string                                 // appended to the tests file (a variant)
	reviewFix bool                                   // review round 1 leaves a blocking finding with a witness; the code step fixes it
}

func newCapRig(t *testing.T) *capRig {
	t.Helper()
	st, err := store.Open(t.TempDir(), spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	sp := newSP(t, []spResult{{p: spNotes(t)}}, st)
	sp.approveAfterSeed(t)
	x := &runsRig{t: t, sp: sp, st: st, projects: t.TempDir(),
		w: &fakeStepWorker{st: st, data: map[string][]byte{}}, c: &fakeStepChecker{}}
	c := &capRig{t: t, x: x, gs: &gateStore{Store: st}, mu: new(sync.Mutex), byRun: map[string]map[string][]canonical.File{}, fixed: map[string]bool{}}
	x.w.set(c.work)
	c.boot()
	return c
}

// with is the rig for subtest t: its failures end that subtest, not the parent test.
func (c *capRig) with(t *testing.T) *capRig {
	cc, xx := *c, *c.x
	xx.t = t
	cc.t, cc.x = t, &xx
	return &cc
}

// boot makes a handler over the same slices service and store, as an engine restart does.
func (c *capRig) boot() {
	x := c.x
	ctx, cancel := context.WithCancel(context.Background())
	x.r = newRig(c.t, func(cfg *Config) {
		cfg.Context = ctx
		cfg.Slices, cfg.Store = x.sp.svc, c.gs
		cfg.WorkerGrants = stepGrants("S-1")
		cfg.Projects = x.projects
		cfg.Model = "test-model"
		cfg.StepWorker = x.w
		cfg.StepChecker = x.c
	})
	stopEngineAtEnd(c.t, x.r, cancel)
	registerAll(c.t, x.r, "S-1")
}

// work is the step worker: each declared output is written as the framed form of its files.
func (c *capRig) work(_ context.Context, r machine.Request) (machine.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	proj := c.byRun[r.Run]
	if proj == nil {
		proj = map[string][]canonical.File{}
		c.byRun[r.Run] = proj
	}
	fixing := r.Step.Name == "code" && len(r.Open) > 0
	out := map[string]string{}
	for _, name := range r.Step.Outputs {
		var files []canonical.File
		var raw []byte
		switch name {
		case "contract":
			files = []canonical.File{{Path: "api/openapi.yaml", Content: []byte("openapi: 3.1.0\n")}}
		case "examples":
			files = []canonical.File{{Path: "features/slice.feature", Content: []byte("Feature: Notes\n")}}
		case "generated-code":
			files = []canonical.File{{Path: "gen/notes.gen.go", Content: []byte("package gen\n")}}
		case "tests":
			files = []canonical.File{capTestsFile(c.extraTest)}
		case "code":
			files = capCodeFiles(fixing || c.fixed[r.Run])
		case "witnesses":
			if c.reviewFix {
				files = capWitnessFiles()
			}
		case "findings":
			raw = []byte("[]")
		default:
			return machine.Result{}, fmt.Errorf("capRig: the worker does not know the output %q", name)
		}
		if raw == nil {
			raw = canonical.Framed(files)
			proj[name] = files
		}
		if name == "code" && fixing {
			c.fixed[r.Run] = true
		}
		h, err := c.gs.PutArtifact("S-1", raw)
		if err != nil {
			return machine.Result{}, err
		}
		out[name] = h
	}
	res := machine.Result{Model: "test-model", Outputs: out, Usage: machine.Usage{Tokens: 10}, Done: true}
	if c.reviewFix && r.Step.Reviews() && r.Round == 1 {
		res.Findings = []machine.Finding{{Rule: "R-1", File: "internal/app/notes.go", Line: 3, Text: "an empty text is accepted", Blocking: true,
			Check: "AllTestsPass", Witness: &machine.Witness{File: capWitnessPath, Test: "TestR1EmptyText"}}}
	}
	if fixing {
		res.ChangedLines = []machine.Line{{File: "internal/app/notes.go", Line: 3}}
	}
	res.ProjectFiles = c.projectFiles(proj)
	return res, nil
}

func (c *capRig) projectFiles(proj map[string][]canonical.File) []machine.File {
	var fs []machine.File
	for _, files := range proj {
		for _, f := range files {
			fs = append(fs, machine.File{Path: f.Path, SHA256: capSHA(f.Content)})
		}
	}
	slices.SortFunc(fs, func(a, b machine.File) int { return strings.Compare(a.Path, b.Path) })
	return fs
}

// capSHA is a project file's content hash (machine.File).
func capSHA(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// finalFiles are the files the run's project ended with whose path starts with one of prefixes.
func (c *capRig) finalFiles(run string, prefixes []string) []canonical.File {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []canonical.File
	for _, files := range c.byRun[run] {
		for _, f := range files {
			if slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(f.Path, p) }) {
				out = append(out, f)
			}
		}
	}
	return out
}

// reviewFixChecker is the checker of a review round that leaves a blocking finding open: the
// NoBlockingFindingOpen check fails while the round has a blocking finding, every other passes.
func (c *capRig) reviewFixChecker() {
	c.x.c.set(func(_ context.Context, r machine.CheckRequest) (machine.CheckResult, error) {
		if r.Name == "NoBlockingFindingOpen" {
			var confirmed []int
			for i, f := range r.Result.Findings {
				if f.Blocking {
					confirmed = append(confirmed, i)
				}
			}
			if len(confirmed) > 0 {
				return machine.CheckResult{Passed: false, Reason: "1 blocking finding open", Confirmed: confirmed}, nil
			}
		}
		return machine.CheckResult{Passed: true, Reason: r.Name + " holds"}, nil
	})
}

// ---- calls ----

func (c *capRig) send(token, method, path, key, body string) reply {
	c.x.r.clock.Advance(80 * time.Millisecond)
	var hdr map[string]string
	if key != "" {
		hdr = map[string]string{"Idempotency-Key": key}
	}
	return c.x.r.do(method, apiPath(path), token, body, hdr)
}

func (c *capRig) call(method, path, key, body string) reply {
	return c.send(clientToken, method, path, key, body)
}

func capBody(run, name string) string {
	if name == "" {
		return fmt.Sprintf(`{"run":%q}`, run)
	}
	return fmt.Sprintf(`{"run":%q,"name":%q}`, run, name)
}

func (c *capRig) capture(run, name string) reply {
	return c.call("POST", "/captured-patterns", "", capBody(run, name))
}

// params builds a PUT body from name, value pairs.
func capParams(pairs ...string) string {
	var items []string
	for i := 0; i+1 < len(pairs); i += 2 {
		items = append(items, fmt.Sprintf(`{"name":%q,"value":%q}`, pairs[i], pairs[i+1]))
	}
	return `{"parameters":[` + strings.Join(items, ",") + `]}`
}

func (c *capRig) put(id, body string) reply {
	return c.call("PUT", "/captured-patterns/"+id+"/parameters", "", body)
}

func (c *capRig) approve(id string) reply {
	return c.call("POST", "/captured-patterns/"+id+"/approval", "", "")
}

// accepted checks a 202 whose operation has the kind and target, and follows it to its end.
func (c *capRig) accepted(p reply, kind, target string) spOp {
	c.t.Helper()
	op := c.acceptedOnly(p, kind, target)
	return c.x.waitOp(op.ID)
}

func (c *capRig) acceptedOnly(p reply, kind, target string) spOp {
	c.t.Helper()
	if p.Code != http.StatusAccepted {
		c.t.Fatalf("status = %d, want 202; body: %s", p.Code, p.Body.String())
	}
	var op spOp
	if err := json.Unmarshal(p.Body.Bytes(), &op); err != nil {
		c.t.Fatalf("not an operation: %v: %s", err, p.Body.String())
	}
	if op.Kind != kind || op.Target != target {
		c.t.Fatalf("operation kind/target = %s/%s, want %s/%s", op.Kind, op.Target, kind, target)
	}
	return op
}

func (c *capRig) succeeded(p reply, kind, target string) spOp {
	c.t.Helper()
	op := c.accepted(p, kind, target)
	if op.Status != "succeeded" || op.Result["captured_pattern"] != target {
		c.t.Fatalf("operation %+v, want succeeded with result.captured_pattern %s", op, target)
	}
	return op
}

// failed checks an operation that ends failed with the code, whose detail names each of names.
func (c *capRig) failed(p reply, kind, target string, code gen.ErrorCode, names ...string) spOp {
	c.t.Helper()
	op := c.accepted(p, kind, target)
	if op.Status != "failed" || op.Error == nil || op.Error.Code != string(code) {
		c.t.Fatalf("operation %+v, want failed with %s", op, code)
	}
	for _, n := range names {
		if !strings.Contains(op.Error.Detail, n) {
			c.t.Errorf("the failure's detail %q does not name %q", op.Error.Detail, n)
		}
	}
	return op
}

// opCount is how many operations the engine holds (OP-1, OP-2, ... without a gap).
func (c *capRig) opCount() int {
	n := 0
	for {
		r := c.call("GET", "/operations/OP-"+fmt.Sprint(n+1), "", "")
		if r.Code == http.StatusNotFound {
			return n
		}
		if r.Code != 200 {
			c.t.Fatalf("getOperation OP-%d = %d: %s", n+1, r.Code, r.Body.String())
		}
		n++
	}
}

// refused checks a synchronous refusal: the status, the code, and no operation created.
func (c *capRig) refused(p func() reply, status int, code gen.ErrorCode) gen.Problem {
	c.t.Helper()
	before := c.opCount()
	pr := p().problem(c.t, status, code)
	if after := c.opCount(); after != before {
		c.t.Errorf("a refused call created %d operation(s)", after-before)
	}
	return pr
}

func (c *capRig) get(id string) gen.CapturedPattern {
	c.t.Helper()
	r := c.call("GET", "/captured-patterns/"+id, "", "")
	if r.Code != 200 {
		c.t.Fatalf("getCapturedPattern %s = %d: %s", id, r.Code, r.Body.String())
	}
	return decodeStrict[gen.CapturedPattern](c.t, r.Body.Bytes())
}

// decodeStrict reads JSON into T and fails on a field the contract does not declare.
func decodeStrict[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v T
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("%T: %v: %s", v, err, b)
	}
	return v
}

func (c *capRig) list(query string) gen.CapturedPatternList {
	c.t.Helper()
	r := c.call("GET", "/captured-patterns"+query, "", "")
	if r.Code != 200 {
		c.t.Fatalf("listCapturedPatterns = %d: %s", r.Code, r.Body.String())
	}
	return decodeStrict[gen.CapturedPatternList](c.t, r.Body.Bytes())
}

// ---- runs in every state ----

// runDone starts a run of S-1 and waits until it is done.
func (c *capRig) runDone(project string) string {
	c.t.Helper()
	op := c.x.startOK(project)
	c.x.waitEnded(op.Target, "done")
	return op.Target
}

// setup is a done run and its captured pattern, named by default (`api-feature-go-s-1`).
func (c *capRig) setup() (run, id string) {
	c.t.Helper()
	run = c.runDone("notes-app")
	id = capBase + "-s-1"
	c.succeeded(c.capture(run, ""), "capture-run", id)
	return run, id
}

// cloneRun records a copy of the run under a new run ID, with edit applied to its RunStarted
// event (the run-event form), and returns the new ID: the runs a capture refuses, made through
// the store as an older or another engine would have recorded them.
func (c *capRig) cloneRun(from string, edit func(started map[string]any)) string {
	c.t.Helper()
	evs, err := c.gs.Events(from)
	if err != nil || len(evs) == 0 {
		c.t.Fatalf("cloneRun: the run %s has no record: %v", from, err)
	}
	id, err := c.gs.NextRunID()
	if err != nil {
		c.t.Fatal(err)
	}
	for _, e := range evs {
		var data map[string]json.RawMessage
		if err := json.Unmarshal([]byte(e.Data), &data); err != nil {
			c.t.Fatal(err)
		}
		var ev map[string]any
		if err := json.Unmarshal(data["event"], &ev); err != nil {
			c.t.Fatal(err)
		}
		ev["run"] = id
		if e.Kind == "RunStarted" {
			edit(ev)
		}
		data["event"], _ = json.Marshal(ev)
		raw, _ := json.Marshal(data)
		if _, err := c.gs.RecordEvent("S-1", store.Event{Seq: e.Seq, Run: id, Kind: e.Kind, Step: e.Step, Data: store.RawJSON(raw)}); err != nil {
			c.t.Fatal(err)
		}
	}
	c.x.waitEnded(id, "done")
	return id
}

// partHashes are the slice's approved parts' hashes now, as a RunStarted records them.
func (c *capRig) partHashes() map[string]any {
	sl, err := c.x.sp.svc.Get("S-1")
	if err != nil {
		c.t.Fatal(err)
	}
	return map[string]any{"goals": sl.Parts.Goals, "rules": sl.Parts.Rules, "contract": sl.Parts.Contract}
}

// base is the base pattern the engine runs: its ID and `id@version`.
var capBase, capBaseRef = func() (string, string) {
	b, err := os.ReadFile("../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		panic(err)
	}
	var p struct{ ID, Version string }
	if err := yaml.Unmarshal(b, &p); err != nil {
		panic(err)
	}
	return p.ID, p.ID + "@" + p.Version
}()

func paramByValue(ps []gen.Parameter, value string) *gen.Parameter {
	for i := range ps {
		if ps[i].Value == value {
			return &ps[i]
		}
	}
	return nil
}

func paramNamed(ps []gen.Parameter, name string) *gen.Parameter {
	for i := range ps {
		if ps[i].Name == name {
			return &ps[i]
		}
	}
	return nil
}

var hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ---- RunNotDone ----

// T005 / FR-001, contract captureRun: a run that has not ended done is refused at once with
// RunNotDone naming its state, and nothing is created (no operation, no captured pattern).
func TestCaptureOfARunThatIsNotDoneFailsRunNotDone(t *testing.T) {
	type state struct {
		name string
		want string // the state the problem names
		make func(c *capRig) string
	}
	handOver := func(c *capRig) string {
		c.x.failStep("tests")
		op := c.x.startOK("notes-app")
		c.x.waitStatus(op.Target, "waiting-for-developer")
		return op.Target
	}
	states := []state{
		{"running", "running", func(c *capRig) string {
			c.x.gatedWorker()
			return c.x.startOK("notes-app").Target
		}},
		{"interrupted", "interrupted", func(c *capRig) string {
			c.x.w.set(func(context.Context, machine.Request) (machine.Result, error) {
				return machine.Result{}, errors.New("the model is unavailable")
			})
			op := c.x.startOK("notes-app")
			c.x.waitStatus(op.Target, "interrupted")
			return op.Target
		}},
		{"waiting for the developer", "waiting-for-developer", handOver},
		{"handed over", "handed-over", func(c *capRig) string {
			run := handOver(c)
			c.x.opOf(c.x.do("POST", apiPath("/runs/"+run+"/hand-over"), ""), "run")
			c.x.waitEnded(run, "handed-over")
			return run
		}},
		{"expired", "expired", func(c *capRig) string {
			run := handOver(c)
			waitForTimer(c.t, c.gs, run)
			c.x.c.set(nil)
			c.x.r.clock.Advance(14*24*time.Hour + time.Hour)
			c.x.waitEnded(run, "expired")
			return run
		}},
		{"cancelled", "cancelled", func(c *capRig) string {
			c.x.gatedWorker()
			op := c.x.startOK("notes-app")
			c.x.do("POST", apiPath("/operations/"+op.ID+"/cancel"), "").operation(c.t, 202)
			c.x.waitEnded(op.Target, "cancelled")
			return op.Target
		}},
	}
	for _, s := range states {
		t.Run(s.name, func(t *testing.T) {
			c := newCapRig(t)
			run := s.make(c)
			p := c.refused(func() reply { return c.capture(run, "") }, 409, gen.ErrorCodeRunNotDone)
			if p.Detail == nil || !strings.Contains(*p.Detail, s.want) {
				t.Errorf("RunNotDone's detail %v does not name the state %q", p.Detail, s.want)
			}
			if items := c.list("").Items; len(items) != 0 {
				t.Errorf("a refused capture left %d captured pattern(s)", len(items))
			}
			c.x.do("GET", apiPath("/captured-patterns/"+capBase+"-s-1"), "").problem(t, 404, gen.ErrorCodeNotFound)
		})
	}
}

// T005 / FR-001: the state is judged when the call is made: the same run captures once it is done.
func TestCaptureOfARunThatHasNotEndedWorksOnceItIsDone(t *testing.T) {
	c := newCapRig(t)
	gate := make(chan struct{})
	var once sync.Once
	c.x.w.set(func(ctx context.Context, r machine.Request) (machine.Result, error) {
		select {
		case <-gate:
		case <-ctx.Done():
			return machine.Result{}, ctx.Err()
		}
		return c.work(ctx, r)
	})
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	op := c.x.startOK("notes-app")
	c.refused(func() reply { return c.capture(op.Target, "") }, 409, gen.ErrorCodeRunNotDone)
	release()
	c.x.waitEnded(op.Target, "done")
	c.succeeded(c.capture(op.Target, ""), "capture-run", capBase+"-s-1")
}

// ---- the proposed captured pattern ----

// T005 / FR-001, FR-002, FR-003, FR-007, data model "Captured pattern": a done run gives a
// proposed captured pattern `<base id>-<name>` (the slice ID in lower case without a name), version
// 1.0.0, naming its source run and slice, the base pattern and the method, holding the source
// slice's approved part hashes and the template's hashes, with proposed parameters and places;
// it survives an engine restart and is listed.
func TestCaptureOfADoneRunProposesACapturedPattern(t *testing.T) {
	c := newCapRig(t)
	run := c.runDone("notes-app")
	sl, err := c.x.sp.svc.Get("S-1")
	if err != nil {
		t.Fatal(err)
	}
	id := capBase + "-s-1"
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(id) {
		t.Fatalf("setup: %q is not a pattern_id", id)
	}
	op := c.succeeded(c.capture(run, ""), "capture-run", id)
	if op.Target != id {
		t.Errorf("the operation's target %q, want the new captured pattern %q", op.Target, id)
	}

	cp := c.get(id)
	if cp.Id != id || string(cp.Version) != "1.0.0" || cp.Base != capBaseRef {
		t.Errorf("id/version/base = %s/%s/%s, want %s/1.0.0/%s", cp.Id, cp.Version, cp.Base, id, capBaseRef)
	}
	if cp.Source.Run != run || cp.Source.Slice != "S-1" {
		t.Errorf("source = %+v, want %s of S-1", cp.Source, run)
	}
	if !hashRE.MatchString(cp.Method) {
		t.Errorf("method %q is not a hash", cp.Method)
	}
	if cp.Parts.Goals != sl.Parts.Goals || cp.Parts.Rules != sl.Parts.Rules || cp.Parts.Contract != sl.Parts.Contract {
		t.Errorf("parts %+v, want the source slice's %+v", cp.Parts, sl.Parts)
	}
	for out, got := range map[string]string{"tests": cp.Template.Tests, "code": cp.Template.Code, "witnesses": cp.Template.Witnesses} {
		if want := capFramedHash(c.finalFiles(run, capOutputs[out])); got != want {
			t.Errorf("template %s = %s, want the hash of the project's final files, %s", out, got, want)
		}
	}
	if cp.State != gen.CapturedPatternStateProposed || cp.Usable || cp.Approval != nil || cp.ProofSlice != nil || len(cp.Proofs) != 0 || cp.CreatedAt.IsZero() {
		t.Errorf("a new captured pattern is proposed, not usable, with no approval, proof slice or proof: %+v", cp)
	}
	var raw map[string]json.RawMessage
	r := c.call("GET", "/captured-patterns/"+id, "", "")
	_ = json.Unmarshal(r.Body.Bytes(), &raw)
	if string(raw["proofs"]) != "[]" {
		t.Errorf("proofs = %s, want [] (the contract requires the list)", raw["proofs"])
	}

	// Proposed parameters: the names note and notes, each with its places.
	note, notes := paramByValue(cp.Parameters, "note"), paramByValue(cp.Parameters, "notes")
	if note == nil || notes == nil {
		t.Fatalf("proposed parameters %+v, want the names note and notes", cp.Parameters)
	}
	for _, p := range cp.Parameters {
		if !regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(p.Name) || len(p.Places) == 0 || p.Origin != gen.ParameterOriginProposed {
			t.Errorf("parameter %+v: want a valid name, places, origin proposed", p)
		}
		if p.Kind == gen.ParameterKindNumber {
			t.Errorf("parameter %+v: no status code or other number is proposed", p)
		}
	}
	for _, p := range []*gen.Parameter{note, notes} {
		if p.Kind != gen.ParameterKindName || p.Source != gen.ParameterSourceName {
			t.Errorf("%s: kind/source %s/%s, want name/name", p.Name, p.Kind, p.Source)
		}
	}
	has := func(p *gen.Parameter, part string, in gen.ParameterPlaceIn, line, col int, form gen.ParameterPlaceForm) bool {
		return slices.ContainsFunc(p.Places, func(pl gen.ParameterPlace) bool {
			return pl.Part == part && pl.In == in && (line == 0 || pl.Line == line) && (col == 0 || pl.Column == col) && pl.Form == form
		})
	}
	for _, w := range []struct {
		in        gen.ParameterPlaceIn
		line, col int
		form      gen.ParameterPlaceForm
	}{{"content", 3, 4, "upper"}, {"content", 3, 12, "capitalised"}, {"content", 3, 26, "lower"}, {"content", 3, 40, "lower"}} {
		if !has(note, capTestsPath, w.in, w.line, w.col, w.form) {
			t.Errorf("note has no %s place %d:%d (%s) in %s: %+v", w.in, w.line, w.col, w.form, capTestsPath, note.Places)
		}
	}
	if has(note, capTestsPath, "content", 4, 10, "capitalised") {
		t.Errorf("`Notes` in TestNotes counted for note as well as notes: places overlap")
	}
	if !has(notes, capTestsPath, "content", 4, 10, "capitalised") {
		t.Errorf("notes has no place for `Notes` in TestNotes: %+v", notes.Places)
	}
	if !has(notes, capTestsPath, "path", 1, 7, "lower") || !has(notes, "cmd/notes/main.go", "path", 1, 5, "lower") {
		t.Errorf("notes has no place in the paths %s (column 7) and cmd/notes/main.go (column 5): %+v", capTestsPath, notes.Places)
	}
	if !has(note, "slice/goals", "content", 0, 0, "lower") && !has(note, "slice/goals", "content", 0, 0, "capitalised") {
		t.Errorf("note has no place in slice/goals: %+v", note.Places)
	}
	for _, p := range cp.Parameters {
		for _, pl := range p.Places {
			if pl.Line < 1 || pl.Column < 1 {
				t.Errorf("%s place %+v: line and column count from 1", p.Name, pl)
			}
			if pl.In == "path" && pl.Line != 1 {
				t.Errorf("%s place %+v: a place in a path has line 1", p.Name, pl)
			}
		}
	}

	// Listed, and the same after an engine restart; its ID is held (CapturedPatternExists).
	if items := c.list("").Items; len(items) != 1 || items[0].Id != id {
		t.Errorf("list = %+v, want the one captured pattern", items)
	}
	c.boot()
	if again := c.get(id); !reflect.DeepEqual(again, cp) {
		t.Errorf("after a restart: %+v\nwant %+v", again, cp)
	}
	c.refused(func() reply { return c.capture(run, "") }, 409, gen.ErrorCodeCapturedPatternExists)
}

// T005 / FR-007: the ID is `<base id>-<name>`; a name outside `capture_name` or a body outside
// CaptureRequest is 400 InvalidRequest at once; an unknown run is 404; an ID held answers
// CapturedPatternExists at once, also for another run's capture under the same name.
func TestCaptureNameAndBodyRules(t *testing.T) {
	c := newCapRig(t)
	run := c.runDone("notes-app")
	for _, bad := range []struct{ name, body string }{
		{"41 characters", capBody(run, strings.Repeat("a", 41))},
		{"uppercase", capBody(run, "Notes")},
		{"underscore", capBody(run, "my_notes")},
		{"double hyphen", capBody(run, "a--b")},
		{"leading hyphen", capBody(run, "-a")},
		{"trailing hyphen", capBody(run, "a-")},
		{"empty name", `{"run":"` + run + `","name":""}`},
		{"a space", capBody(run, "my notes")},
		{"name not a string", `{"run":"` + run + `","name":5}`},
		{"an unknown field", `{"run":"` + run + `","extra":1}`},
		{"no run", `{"name":"x"}`},
		{"a run that is not a run ID", capBody("notes", "x")},
		{"no body", ``},
	} {
		t.Run("400 "+bad.name, func(t *testing.T) {
			c := c.with(t)
			c.refused(func() reply { return c.call("POST", "/captured-patterns", "", bad.body) }, 400, gen.ErrorCodeInvalidRequest)
		})
	}
	c.refused(func() reply { return c.capture("RUN-99", "") }, 404, gen.ErrorCodeNotFound)
	if items := c.list("").Items; len(items) != 0 {
		t.Fatalf("a refused capture left %d captured pattern(s)", len(items))
	}

	long := strings.Repeat("a", 40) // the limit
	c.succeeded(c.capture(run, long), "capture-run", capBase+"-"+long)
	c.succeeded(c.capture(run, "x"), "capture-run", capBase+"-x") // a single character
	c.succeeded(c.capture(run, "v2-3"), "capture-run", capBase+"-v2-3")
	c.succeeded(c.capture(run, ""), "capture-run", capBase+"-s-1")
	for _, name := range []string{"x", "", long} { // the same ID again
		n := name
		id := capBase + "-" + name
		if name == "" {
			id = capBase + "-s-1"
		}
		c.refused(func() reply { return c.capture(run, n) }, 409, gen.ErrorCodeCapturedPatternExists)
		if cp := c.get(id); cp.Source.Run != run || cp.State != gen.CapturedPatternStateProposed {
			t.Errorf("the refused capture changed %s: %+v", id, cp)
		}
	}
	if items := c.list("").Items; len(items) != 4 {
		t.Errorf("%d captured patterns, want the 4 captured", len(items))
	}
}

// T005 / FR-002, research R1 (captured.Declaration): the captured pattern's declaration is the
// base pattern's with each agent step's worker `procedure:fill`, `limits.attempts` and
// `limits.review_rounds` 1 and the review step without FindingsCarryEvidence and
// NoBlockingFindingOpen, with AllTestsPass added after its other checks (last; ADR 0101), and
// with outputs [witnesses], valid against pattern.schema.json, everything else equal. The declaration is not in getCapturedPattern, so it is judged on the
// function that makes it. (agent_digest is left out of the comparison: the contract does not say
// whether a `fill` step keeps it.)
func TestCapturedDeclarationIsTheBasePatternWithFillSteps(t *testing.T) {
	base, err := os.ReadFile("../../catalogue/api-feature-go/pattern.yaml")
	if err != nil {
		t.Fatal(err)
	}
	decl, err := captured.Declaration(base)
	if err != nil {
		t.Fatalf("Declaration: %v", err)
	}

	// Valid against pattern.schema.json.
	abs, err := filepath.Abs(specDir + "/pattern.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := jsonschema.NewCompiler().Compile("file://" + abs)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(decl, &got); err != nil {
		t.Fatalf("the declaration is not YAML: %v", err)
	}
	asJSON, _ := json.Marshal(got)
	var inst any
	_ = json.Unmarshal(asJSON, &inst)
	if err := schema.Validate(inst); err != nil {
		t.Errorf("the declaration is not valid against pattern.schema.json: %v", err)
	}

	// The expected declaration, made by hand from the base pattern's.
	var want map[string]any
	if err := yaml.Unmarshal(base, &want); err != nil {
		t.Fatal(err)
	}
	want["limits"] = map[string]any{"attempts": 1, "review_rounds": 1}
	for _, s := range want["steps"].([]any) {
		st := s.(map[string]any)
		if strings.HasPrefix(st["worker"].(string), "agent:") {
			st["worker"] = "procedure:fill"
		}
		if slices.Contains(toStrings(st["checks"]), "NoBlockingFindingOpen") {
			st["outputs"] = []any{"witnesses"}
			var checks []any
			for _, ck := range toStrings(st["checks"]) {
				if ck != "NoBlockingFindingOpen" && ck != "FindingsCarryEvidence" {
					checks = append(checks, ck)
				}
			}
			checks = append(checks, "AllTestsPass")
			st["checks"] = checks
		}
	}
	norm := func(m map[string]any) map[string]any {
		b, _ := json.Marshal(m)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		for _, s := range out["steps"].([]any) {
			delete(s.(map[string]any), "agent_digest")
		}
		return out
	}
	if g, w := norm(got), norm(want); !reflect.DeepEqual(g, w) {
		gb, _ := json.MarshalIndent(g, "", " ")
		wb, _ := json.MarshalIndent(w, "", " ")
		t.Errorf("the declaration differs from the base pattern's with fill steps.\ngot:\n%s\nwant:\n%s", gb, wb)
	}
	for _, s := range got["steps"].([]any) {
		st := s.(map[string]any)
		if w := st["worker"].(string); strings.HasPrefix(w, "agent:") {
			t.Errorf("step %v keeps the agent worker %s", st["name"], w)
		}
	}
}

func toStrings(v any) []string {
	var out []string
	for _, e := range v.([]any) {
		out = append(out, e.(string))
	}
	return out
}

// ---- the template's hashes ----

// T005 / R3, data model `template`: the template hashes are the artifact hashes of each output's
// step's last acceptance, `StepAccepted` or `ResultReused` alike, so they equal the files the
// project ended with: for a run whose `tests` and `code` were reused ...
func TestTemplateHashesEqualTheFinalProjectForAReusedRun(t *testing.T) {
	c := newCapRig(t)
	first := c.runDone("notes-app")
	calls := c.x.w.callCount()
	second := c.runDone("second-app")
	if c.x.w.callCount() != calls {
		t.Fatalf("setup: the second run called a worker %d time(s); it should reuse every result", c.x.w.callCount()-calls)
	}
	reused := map[string]string{}
	for _, e := range c.x.allEvents(second) {
		if e.Kind == "ResultReused" && len(e.Artifacts) > 0 {
			reused[e.Step] = e.Artifacts[0]
		}
	}
	if reused["tests"] == "" || reused["code"] == "" {
		t.Fatalf("setup: the second run has no ResultReused for tests and code: %v", reused)
	}
	id := capBase + "-reused"
	c.succeeded(c.capture(second, id[len(capBase)+1:]), "capture-run", id)
	cp := c.get(id)
	if cp.Source.Run != second {
		t.Errorf("source run %s, want %s", cp.Source.Run, second)
	}
	for out, got := range map[string]string{"tests": cp.Template.Tests, "code": cp.Template.Code, "witnesses": cp.Template.Witnesses} {
		want := capFramedHash(c.finalFiles(first, capOutputs[out]))
		if got != want {
			t.Errorf("template %s = %s, want the hash of the final project's files, %s", out, got, want)
		}
	}
	if cp.Template.Tests != reused["tests"] || cp.Template.Code != reused["code"] {
		t.Errorf("template tests/code %s/%s, want the reused results' artifacts %s/%s", cp.Template.Tests, cp.Template.Code, reused["tests"], reused["code"])
	}
}

// ... and for a run whose review round's fix changed `code` (R3: after any review-round fix) with
// the witness the review wrote.
func TestTemplateHashesEqualTheFinalProjectAfterAReviewFix(t *testing.T) {
	c := newCapRig(t)
	c.reviewFix = true
	c.reviewFixChecker()
	run := c.runDone("notes-app")
	var fixes int
	for _, e := range c.x.allEvents(run) {
		if e.Kind == "StepAccepted" && e.Step == "code" {
			fixes++
		}
	}
	if fixes < 2 {
		t.Fatalf("setup: the code step was accepted %d time(s); want the step and its review fix", fixes)
	}
	id := capBase + "-fixed"
	c.succeeded(c.capture(run, "fixed"), "capture-run", id)
	cp := c.get(id)
	final := c.finalFiles(run, capOutputs["code"])
	if want := capFramedHash(capCodeFiles(true)); capFramedHash(final) != want {
		t.Fatalf("setup: the project's final code is not the fixed code")
	}
	if cp.Template.Code != capFramedHash(final) {
		t.Errorf("template code = %s, want the hash of the fixed code, %s", cp.Template.Code, capFramedHash(final))
	}
	if cp.Template.Code == capFramedHash(capCodeFiles(false)) {
		t.Errorf("template code is the hash of the code before the review's fix")
	}
	if want := capFramedHash(capWitnessFiles()); cp.Template.Witnesses != want {
		t.Errorf("template witnesses = %s, want the review's witness files' hash %s", cp.Template.Witnesses, want)
	}
}

// ---- runs that cannot be captured ----

// T005 / FR-001, data model: a run whose slice changed after it started (a part's hash differs
// from the one its RunStarted records, or the part is no longer approved) is RunNotCapturable,
// naming the part, at once.
func TestCaptureRefusesARunWhoseSliceChangedAfterItStarted(t *testing.T) {
	for _, part := range []string{"goals", "rules"} {
		for _, reapprove := range []bool{false, true} {
			name := part + " replaced"
			if reapprove {
				name += " and approved again"
			}
			t.Run(name, func(t *testing.T) {
				c := newCapRig(t)
				run := c.runDone("notes-app")
				// The record names the hashes the run started on (the slice as it is now).
				hashes := c.partHashes()
				clone := c.cloneRun(run, func(ev map[string]any) { ev["part_hashes"] = hashes })
				svc := c.x.sp.svc
				sl, err := svc.Get("S-1")
				if err != nil {
					t.Fatal(err)
				}
				ctx := t.Context()
				switch part {
				case "goals":
					g := sl.Goals
					g[0].Text = "Keep a note for good"
					_, err = svc.ReplaceGoals(ctx, "S-1", g)
				case "rules":
					rules := sl.Rules
					rules[0].Response = "shall store the note durably"
					for i := range rules {
						if rules[i].Terms == nil {
							rules[i].Terms = []string{}
						}
					}
					_, err = svc.ReplaceRules(ctx, "S-1", slc.RulesPart{Rules: rules, Examples: sl.Examples, Terms: sl.Terms})
				}
				if err != nil {
					t.Fatal(err)
				}
				if reapprove {
					now, _ := svc.Get("S-1")
					hash := map[string]string{"goals": now.Parts.Goals, "rules": now.Parts.Rules}[part]
					if _, err := svc.Approve(ctx, "S-1", slc.Part(part), hash); err != nil {
						t.Fatal(err)
					}
				}
				p := c.refused(func() reply { return c.capture(clone, "") }, 409, gen.ErrorCodeRunNotCapturable)
				if p.Detail == nil || !strings.Contains(*p.Detail, part) {
					t.Errorf("RunNotCapturable's detail %v does not name the part %q", p.Detail, part)
				}
				// The real run, whose record the engine wrote, is refused the same way.
				p = c.refused(func() reply { return c.capture(run, "") }, 409, gen.ErrorCodeRunNotCapturable)
				if p.Detail == nil || !strings.Contains(*p.Detail, part) {
					t.Errorf("RunNotCapturable's detail %v does not name the part %q (the engine's own run)", p.Detail, part)
				}
				if items := c.list("").Items; len(items) != 0 {
					t.Errorf("a refused capture left %d captured pattern(s)", len(items))
				}
			})
		}
	}
}

// T005 / FR-001, data model "Parameter": a run whose RunStarted has no part hashes (started
// before feature 011), a run recorded under another base pattern version than the engine's (the
// problem names both), a run of a captured pattern and a proof run are RunNotCapturable at once;
// the same record with part hashes and the base pattern captures.
func TestCaptureRefusesRunsThatCannotBeASource(t *testing.T) {
	c := newCapRig(t)
	run := c.runDone("notes-app")
	hashes := c.partHashes()
	for _, tc := range []struct {
		name  string
		edit  func(ev map[string]any)
		names []string // what the detail names
	}{
		{"no part hashes (started before feature 011)", func(ev map[string]any) { delete(ev, "part_hashes") }, nil},
		{"another base pattern version", func(ev map[string]any) {
			ev["part_hashes"], ev["pattern"] = hashes, capBase+"@1.1.0"
		}, []string{capBase + "@1.1.0", capBaseRef}},
		{"another base pattern", func(ev map[string]any) {
			ev["part_hashes"], ev["pattern"] = hashes, "other-pattern@1.2.0"
		}, []string{"other-pattern@1.2.0", capBaseRef}},
		{"a run of a captured pattern", func(ev map[string]any) {
			ev["part_hashes"], ev["pattern"], ev["pattern_reason"] = hashes, capBase+"-notes@1.0.0", "fits"
			ev["source_run"], ev["parameters"] = run, map[string]any{"note": "task"}
		}, nil},
		{"a proof run", func(ev map[string]any) {
			ev["part_hashes"], ev["pattern"], ev["pattern_reason"], ev["proof"] = hashes, capBase+"-notes@1.0.0", "chosen", true
			ev["source_run"], ev["parameters"] = run, map[string]any{"note": "note"}
		}, nil},
		{"a proof flag on the base pattern's run", func(ev map[string]any) {
			ev["part_hashes"], ev["proof"] = hashes, true
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := c.with(t)
			clone := c.cloneRun(run, tc.edit)
			p := c.refused(func() reply { return c.capture(clone, "") }, 409, gen.ErrorCodeRunNotCapturable)
			for _, n := range tc.names {
				if p.Detail == nil || !strings.Contains(*p.Detail, n) {
					t.Errorf("RunNotCapturable's detail %v does not name %q", p.Detail, n)
				}
			}
			c.x.do("GET", apiPath("/captured-patterns/"+capBase+"-s-1"), "").problem(t, 404, gen.ErrorCodeNotFound)
		})
	}
	// Planted-violation counterpart: the same record with part hashes and the engine's base
	// pattern is a source, so the refusals above are for their cause.
	clean := c.cloneRun(run, func(ev map[string]any) { ev["part_hashes"] = hashes })
	c.succeeded(c.capture(clean, "clean"), "capture-run", capBase+"-clean")
}

// ---- PUT /parameters ----

// T005 / FR-003, data model "Parameter": PUT /parameters takes {name, value} and the engine sets
// kind, source and places: a value among the slice's names is kind name, source name; any other
// number that occurs is kind number, source literal; any other string is kind string, source
// literal; a parameter sent again keeps them; parameters not sent are removed; an empty list leaves
// none; the captured pattern stays proposed.
func TestReplaceParametersSetsKindsSourcesAndPlaces(t *testing.T) {
	c := newCapRig(t)
	_, id := c.setup()
	before := c.get(id)
	if len(before.Parameters) < 2 {
		t.Fatalf("setup: %d proposed parameters", len(before.Parameters))
	}
	noteName := paramByValue(before.Parameters, "note").Name

	body := capParams(noteName, "note", "keep", "keep", "created", "201", "phrase", "shall store")
	c.succeeded(c.put(id, body), "replace-parameters", id)
	cp := c.get(id)
	if cp.State != gen.CapturedPatternStateProposed {
		t.Errorf("state %s after replacing the parameters of a proposed one, want proposed", cp.State)
	}
	if len(cp.Parameters) != 4 || paramByValue(cp.Parameters, "notes") != nil {
		t.Fatalf("parameters %+v, want exactly the four sent (notes removed)", cp.Parameters)
	}
	for _, w := range []struct {
		name, value string
		kind        gen.ParameterKind
		source      gen.ParameterSource
	}{
		{noteName, "note", gen.ParameterKindName, gen.ParameterSourceName},
		{"keep", "keep", gen.ParameterKindName, gen.ParameterSourceName},
		{"created", "201", gen.ParameterKindNumber, gen.ParameterSourceLiteral},
		{"phrase", "shall store", gen.ParameterKindString, gen.ParameterSourceLiteral},
	} {
		p := paramNamed(cp.Parameters, w.name)
		if p == nil || p.Value != w.value || p.Kind != w.kind || p.Source != w.source || len(p.Places) == 0 {
			t.Errorf("parameter %s = %+v, want value %q kind %s source %s with places", w.name, p, w.value, w.kind, w.source)
			continue
		}
		if w.source == gen.ParameterSourceLiteral && p.Origin != gen.ParameterOriginAdded {
			t.Errorf("%s: origin %s, want added", w.name, p.Origin)
		}
		if w.kind != gen.ParameterKindName && slices.ContainsFunc(p.Places, func(pl gen.ParameterPlace) bool { return pl.Form != "exact" }) {
			t.Errorf("%s: a number or string place has a case form other than exact: %+v", w.name, p.Places)
		}
	}
	if p := paramNamed(cp.Parameters, "created"); p != nil && !slices.ContainsFunc(p.Places, func(pl gen.ParameterPlace) bool { return pl.Part == "slice/contract" }) {
		t.Errorf("201 has no place in slice/contract: %+v", p.Places)
	}
	if reflect.DeepEqual(cp.Parameters, before.Parameters) {
		t.Errorf("the parameters did not change")
	}

	// Sent again: the kind and source are kept.
	c.succeeded(c.put(id, body), "replace-parameters", id)
	if again := c.get(id); !reflect.DeepEqual(again.Parameters, cp.Parameters) {
		t.Errorf("the same parameters sent again gave %+v, want %+v", again.Parameters, cp.Parameters)
	}

	// An empty list leaves none.
	c.succeeded(c.put(id, `{"parameters":[]}`), "replace-parameters", id)
	if cp := c.get(id); len(cp.Parameters) != 0 || cp.State != gen.CapturedPatternStateProposed {
		t.Errorf("after an empty list: %d parameters, state %s", len(cp.Parameters), cp.State)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(c.call("GET", "/captured-patterns/"+id, "", "").Body.Bytes(), &raw)
	if string(raw["parameters"]) != "[]" {
		t.Errorf("parameters = %s, want []", raw["parameters"])
	}
}

// T005 / contract replaceParameters, slice-defs parameter_name, parameter_value: a body outside
// the schema is 400 InvalidRequest at once, no operation, nothing changed: 33 parameters, a
// 65-character value, a value outside `parameter_value`, an empty value, a bad name, an unknown
// or missing field; the limits themselves (32 parameters, 64 characters) pass the schema.
func TestReplaceParametersBodyOutsideTheSchemaIs400(t *testing.T) {
	c := newCapRig(t)
	_, id := c.setup()
	snapshot := c.get(id)
	many := func(n int) string {
		var pairs []string
		for i := range n {
			pairs = append(pairs, fmt.Sprintf("p%d", i), fmt.Sprintf("v%d", i))
		}
		return capParams(pairs...)
	}
	for _, bad := range []struct{ name, body string }{
		{"33 parameters", many(33)},
		{"a 65-character value", capParams("x", strings.Repeat("a", 65))},
		{"a slash in a value", capParams("x", "a/b")},
		{"a non-ASCII value", capParams("x", "café")},
		{"a quote in a value", capParams("x", `a"b`)},
		{"an empty value", capParams("x", "")},
		{"an uppercase name", capParams("Note", "note")},
		{"a name starting with a digit", capParams("1note", "note")},
		{"an underscore in a name", capParams("my_note", "note")},
		{"a 65-character name", capParams(strings.Repeat("a", 65), "note")},
		{"an empty name", capParams("", "note")},
		{"an unknown field of a parameter", `{"parameters":[{"name":"x","value":"note","kind":"name"}]}`},
		{"a parameter without a value", `{"parameters":[{"name":"x"}]}`},
		{"an unknown field of the body", `{"parameters":[],"extra":1}`},
		{"no parameters field", `{}`},
		{"parameters not a list", `{"parameters":"note"}`},
		{"a value that is not a string", `{"parameters":[{"name":"x","value":5}]}`},
		{"no body", ``},
	} {
		t.Run(bad.name, func(t *testing.T) {
			c := c.with(t)
			c.refused(func() reply { return c.put(id, bad.body) }, 400, gen.ErrorCodeInvalidRequest)
		})
	}
	if now := c.get(id); !reflect.DeepEqual(now, snapshot) {
		t.Errorf("a refused call changed the captured pattern:\n%+v\nwant %+v", now, snapshot)
	}
	// The limits are inside the schema: 32 parameters and a 64-character value are accepted at
	// once (202); the operation then judges them against the slice (T005 below).
	for _, ok := range []struct{ name, body string }{
		{"32 parameters", many(32)},
		{"a 64-character value", capParams("x", strings.Repeat("a", 64))},
		{"a 64-character name", capParams(strings.Repeat("a", 64), "note")},
	} {
		t.Run("accepted "+ok.name, func(t *testing.T) {
			c := c.with(t)
			op := c.acceptedOnly(c.put(id, ok.body), "replace-parameters", id)
			c.x.waitOp(op.ID)
		})
	}
}

// T005 / FR-003, contract replaceParameters, T003's refusals: the operation fails
// ParameterRefused, naming the parameter and why, for a value outside its kind's form, two values
// equal in any case form, a value that does not occur in the slice, and more than 10000 places;
// the parameters stay as they were.
func TestReplaceParametersRefusalsFailTheOperationWithParameterRefused(t *testing.T) {
	c := newCapRig(t)
	_, id := c.setup()
	snapshot := c.get(id)
	for _, tc := range []struct {
		name string
		body string
		why  []string // names the detail has
	}{
		// 3.1.0 occurs in the contract but is neither an integer or decimal nor letters, digits, _, - and spaces.
		{"a value outside its kind's form", capParams("version", "3.1.0"), []string{"version"}},
		{"a value that does not occur in the slice", capParams("zebra", "zebra"), []string{"zebra"}},
		{"a number that does not occur as a whole number", capParams("amount", "20"), []string{"amount"}}, // 20 is in 201 and 200 only as part of a number
		{"two values equal in any case form", capParams("lower", "note", "capital", "Note"), []string{"lower", "capital"}},
		{"two names for one value", capParams("one", "note", "two", "note"), []string{"one", "two"}},
		{"the first of several refused", capParams("fine", "note", "zebra", "zebra"), []string{"zebra"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := c.with(t)
			c.failed(c.put(id, tc.body), "replace-parameters", id, gen.ErrorCodeParameterRefused, tc.why...)
			if now := c.get(id); !reflect.DeepEqual(now, snapshot) {
				t.Errorf("a failed operation changed the captured pattern:\n%+v\nwant %+v", now, snapshot)
			}
		})
	}
}

// T005 / FR-003, contract replaceParameters, x-limits.parameter-places-max: a value with more
// than 10000 places is ParameterRefused naming it; one with exactly 10000 is accepted. The slice
// holds `shall` twice (R-1 and R-2), the template's tests file holds it n more times.
func TestReplaceParametersRefusesMoreThanTenThousandPlaces(t *testing.T) {
	for _, tc := range []struct {
		name   string
		extra  int // occurrences of `shall` in the template
		places int // total
		ok     bool
	}{
		{"10000 places", 9998, 10000, true},
		{"10001 places", 9999, 10001, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCapRig(t)
			c.extraTest = strings.Repeat("shall\n", tc.extra)
			_, id := c.setup()
			body := capParams("modal", "shall")
			if tc.ok {
				c.succeeded(c.put(id, body), "replace-parameters", id)
				p := paramNamed(c.get(id).Parameters, "modal")
				if p == nil || len(p.Places) != tc.places {
					t.Fatalf("modal = %+v, want %d places", p, tc.places)
				}
				return
			}
			c.failed(c.put(id, body), "replace-parameters", id, gen.ErrorCodeParameterRefused, "modal")
			if paramNamed(c.get(id).Parameters, "modal") != nil {
				t.Errorf("a refused parameter was kept")
			}
		})
	}
}

// ---- the state table, `proposed` column ----

// T005 / data model "States": for a proposed captured pattern replaceParameters changes the
// parameters (above), approveCapturedPattern answers 409 ProofRequired at once, and every call on
// an unknown ID answers 404 NotFound.
func TestProposedCapturedPatternStateTableRows(t *testing.T) {
	c := newCapRig(t)
	_, id := c.setup()
	snapshot := c.get(id)
	c.refused(func() reply { return c.approve(id) }, 409, gen.ErrorCodeProofRequired)
	if now := c.get(id); !reflect.DeepEqual(now, snapshot) || now.Approval != nil || now.State != gen.CapturedPatternStateProposed {
		t.Errorf("a refused approval changed the captured pattern: %+v", now)
	}
	unknown := capBase + "-nope"
	c.refused(func() reply { return c.call("GET", "/captured-patterns/"+unknown, "", "") }, 404, gen.ErrorCodeNotFound)
	c.refused(func() reply { return c.put(unknown, capParams("note", "note")) }, 404, gen.ErrorCodeNotFound)
	c.refused(func() reply { return c.approve(unknown) }, 404, gen.ErrorCodeNotFound)
}

// T005 / FR-014, Principle IV, data model "States": one operation at a time per captured
// pattern: another state change while one runs answers 409 CapturedPatternBusy at once; the same
// Idempotency-Key and body then gives the running operation; once it ended, a change works.
// The running operation is held at its first artifact read (the template).
func TestASecondStateChangeWhileOneRunsIsCapturedPatternBusy(t *testing.T) {
	c := newCapRig(t)
	_, id := c.setup()
	blocked, release := c.gs.holdReads(t)

	type result struct{ r reply }
	first := make(chan result, 1)
	go func() {
		first <- result{c.call("PUT", "/captured-patterns/"+id+"/parameters", "busy-key", capParams("note", "note", "keep", "keep"))}
	}()
	var r1 reply
	select { // the 202 comes first: the work runs after acceptance (Principle IV)
	case res := <-first:
		r1 = res.r
	case <-time.After(10 * time.Second):
		t.Fatal("the first call did not answer in 10 s")
	}
	select {
	case <-blocked: // the operation is reading the template
	case <-time.After(10 * time.Second):
		t.Fatal("no artifact read in 10 s: the operation cannot be held open to observe CapturedPatternBusy")
	}
	op := c.acceptedOnly(r1, "replace-parameters", id)

	before := c.opCount()
	c.call("PUT", "/captured-patterns/"+id+"/parameters", "", capParams("note", "note")).problem(t, 409, gen.ErrorCodeCapturedPatternBusy)
	c.call("PUT", "/captured-patterns/"+id+"/parameters", "other-key", capParams("keep", "keep")).problem(t, 409, gen.ErrorCodeCapturedPatternBusy)
	if after := c.opCount(); after != before {
		t.Errorf("a Busy refusal created %d operation(s)", after-before)
	}
	// The same key and body is the same request: the running operation again, not Busy.
	again := c.acceptedOnly(c.call("PUT", "/captured-patterns/"+id+"/parameters", "busy-key", capParams("note", "note", "keep", "keep")), "replace-parameters", id)
	if again.ID != op.ID {
		t.Errorf("the same key and body gave %s, want the running operation %s", again.ID, op.ID)
	}
	// A different body under the key is refused as for other calls.
	c.call("PUT", "/captured-patterns/"+id+"/parameters", "busy-key", capParams("note", "note")).problem(t, 409, gen.ErrorCodeIdempotencyKeyReused)

	release()
	if done := c.x.waitOp(op.ID); done.Status != "succeeded" {
		t.Fatalf("the held operation ended %+v, want succeeded", done)
	}
	c.succeeded(c.put(id, capParams("note", "note")), "replace-parameters", id)
	if cp := c.get(id); len(cp.Parameters) != 1 {
		t.Errorf("parameters %+v, want the last change (one)", cp.Parameters)
	}
}

// ---- tokens ----

// T005 / Principle V, contract: captureRun, replaceParameters and approveCapturedPattern accept
// the client token only: a worker or page token is 403 and changes nothing.
func TestCapturedPatternWritesWithAWorkerOrPageTokenAre403(t *testing.T) {
	c := newCapRig(t)
	run, id := c.setup()
	snapshot := c.get(id)
	ops := c.opCount()
	for tokName, tok := range map[string]string{"worker": workerToken, "page": pageToken} {
		t.Run(tokName, func(t *testing.T) {
			c := c.with(t)
			for name, call := range map[string]func() reply{
				"captureRun": func() reply { return c.send(tok, "POST", "/captured-patterns", "", capBody(run, "another")) },
				"replaceParameters": func() reply {
					return c.send(tok, "PUT", "/captured-patterns/"+id+"/parameters", "", capParams("keep", "keep"))
				},
				"approveCapturedPattern": func() reply { return c.send(tok, "POST", "/captured-patterns/"+id+"/approval", "", "") },
				"replaceParameters (junk)": func() reply {
					return c.send(tok, "PUT", "/captured-patterns/"+id+"/parameters", "", `{"parameters":5}`)
				},
			} {
				call().problem(t, 403, gen.ErrorCodeForbidden)
				_ = name
			}
			if tokName == "worker" {
				c.send(tok, "GET", "/captured-patterns", "", "").problem(t, 403, gen.ErrorCodeForbidden)
				c.send(tok, "GET", "/captured-patterns/"+id, "", "").problem(t, 403, gen.ErrorCodeForbidden)
			}
		})
	}
	if now := c.get(id); !reflect.DeepEqual(now, snapshot) {
		t.Errorf("a refused call changed the captured pattern:\n%+v\nwant %+v", now, snapshot)
	}
	if n := c.opCount(); n != ops {
		t.Errorf("refused calls created %d operation(s)", n-ops)
	}
	c.x.do("GET", apiPath("/captured-patterns/"+capBase+"-another"), "").problem(t, 404, gen.ErrorCodeNotFound)
	// The page token may read.
	if r := c.send(pageToken, "GET", "/captured-patterns/"+id, "", ""); r.Code != 200 {
		t.Errorf("getCapturedPattern with the page token = %d, want 200", r.Code)
	}
	if r := c.send(pageToken, "GET", "/captured-patterns", "", ""); r.Code != 200 {
		t.Errorf("listCapturedPatterns with the page token = %d, want 200", r.Code)
	}
}

// ---- list ----

// T005 / contract listCapturedPatterns: newest first, at most 50 per page, opaque page tokens
// (a token the engine did not issue is 400), and the same order across pages.
func TestListCapturedPatternsIsNewestFirstAndPaginated(t *testing.T) {
	c := newCapRig(t)
	run := c.runDone("notes-app")
	if items := c.list("").Items; len(items) != 0 {
		t.Fatalf("an empty engine lists %d captured patterns", len(items))
	}
	var created []string // in order of capture; the names are not in ID order
	for i := range 51 {
		name := fmt.Sprintf("c%02d", (i*7)%51)
		c.succeeded(c.capture(run, name), "capture-run", capBase+"-"+name)
		created = append(created, capBase+"-"+name)
	}
	want := slices.Clone(created)
	slices.Reverse(want)

	p1 := c.list("")
	if len(p1.Items) != 50 || p1.NextPageToken == nil || *p1.NextPageToken == "" {
		t.Fatalf("page 1 has %d items and token %v, want 50 and a token", len(p1.Items), p1.NextPageToken)
	}
	p2 := c.list("&page_token=" + *p1.NextPageToken)
	if len(p2.Items) != 1 || p2.NextPageToken != nil && *p2.NextPageToken != "" {
		t.Fatalf("page 2 has %d items and token %v, want 1 and none", len(p2.Items), p2.NextPageToken)
	}
	var got []string
	for _, it := range append(p1.Items, p2.Items...) {
		got = append(got, it.Id)
	}
	if !slices.Equal(got, want) {
		t.Errorf("order %v\nwant newest first %v", got, want)
	}
	for _, tok := range []string{"bogus", "!!!", "MTAw"} {
		c.call("GET", "/captured-patterns&page_token="+tok, "", "").problem(t, 400, gen.ErrorCodeInvalidRequest)
	}
}

// ---- Idempotency-Key ----

// T005 / contract, FR-014: a reused Idempotency-Key with the same body gives the first operation
// (one captured pattern, one change); with another body it is 409 IdempotencyKeyReused as for other
// calls, for a capture and for a parameters change.
func TestAReusedIdempotencyKeyWithAnotherBodyAnswersAsForOtherCalls(t *testing.T) {
	c := newCapRig(t)
	run := c.runDone("notes-app")
	first := c.acceptedOnly(c.call("POST", "/captured-patterns", "k-capture", capBody(run, "one")), "capture-run", capBase+"-one")
	again := c.acceptedOnly(c.call("POST", "/captured-patterns", "k-capture", capBody(run, "one")), "capture-run", capBase+"-one")
	if again.ID != first.ID {
		t.Errorf("the same key and body gave %s, want %s", again.ID, first.ID)
	}
	c.x.waitOp(first.ID)
	c.call("POST", "/captured-patterns", "k-capture", capBody(run, "two")).problem(t, 409, gen.ErrorCodeIdempotencyKeyReused)
	c.x.do("GET", apiPath("/captured-patterns/"+capBase+"-two"), "").problem(t, 404, gen.ErrorCodeNotFound)
	if items := c.list("").Items; len(items) != 1 {
		t.Errorf("%d captured patterns, want the one", len(items))
	}
	// The same key and body after the operation ended still gives the first operation, not
	// CapturedPatternExists.
	if third := c.acceptedOnly(c.call("POST", "/captured-patterns", "k-capture", capBody(run, "one")), "capture-run", capBase+"-one"); third.ID != first.ID {
		t.Errorf("the key after the end gave %s, want %s", third.ID, first.ID)
	}

	id := capBase + "-one"
	put := c.acceptedOnly(c.call("PUT", "/captured-patterns/"+id+"/parameters", "k-put", capParams("note", "note")), "replace-parameters", id)
	c.x.waitOp(put.ID)
	c.call("PUT", "/captured-patterns/"+id+"/parameters", "k-put", capParams("keep", "keep")).problem(t, 409, gen.ErrorCodeIdempotencyKeyReused)
	if ps := c.get(id).Parameters; len(ps) != 1 || ps[0].Value != "note" {
		t.Errorf("parameters %+v, want the first body's", ps)
	}
	// A key reused on another path with the same body is reused too.
	c.call("POST", "/captured-patterns/"+id+"/approval", "k-put", "").problem(t, 409, gen.ErrorCodeIdempotencyKeyReused)
}

// ---- a failed capture ----

// T005 / contract captureRun: a capture whose operation fails leaves no captured pattern
// (getCapturedPattern answers 404, the list is empty) and holds no ID: the same capture works
// once the cause is gone. The failure is the template's artifacts being unreadable.
func TestAFailedCaptureLeavesNoCapturedPattern(t *testing.T) {
	c := newCapRig(t)
	run := c.runDone("notes-app")
	c.gs.failReads(errors.New("the artifact store is unreadable"))
	p := c.capture(run, "")
	if p.Code == http.StatusAccepted {
		var op spOp
		_ = json.Unmarshal(p.Body.Bytes(), &op)
		if end := c.x.waitOp(op.ID); end.Status != "failed" || end.Error == nil {
			t.Fatalf("operation %+v, want failed with an error", end)
		}
	} else if p.Code < 500 {
		t.Fatalf("status %d, want 202 with a failing operation or a retryable 5xx: %s", p.Code, p.Body.String())
	}
	c.x.do("GET", apiPath("/captured-patterns/"+capBase+"-s-1"), "").problem(t, 404, gen.ErrorCodeNotFound)
	if items := c.list("").Items; len(items) != 0 {
		t.Fatalf("a failed capture left %d captured pattern(s)", len(items))
	}
	c.gs.failReads(nil)
	c.succeeded(c.capture(run, ""), "capture-run", capBase+"-s-1")
}
