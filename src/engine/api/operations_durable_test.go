package api

// T023 (feature 002, user story 2), part 1 of 3: accepting an operation durably, the 503 answers,
// and cancelling. Written before the code (T025) by a test author. Judged from spec FR-008 to
// FR-012 and FR-016, the data model ("Operation record": every bullet and both tables; "The
// engine's durable interface": Operation, DoOperation, EndOperation, Operations, Operator),
// research R4 and R6, ADR 0072 and the engine contract (info text, x-limits, getOperation,
// cancelOperation, UnavailableProblem, IdempotencyKey). The other parts are
// operations_restart_test.go and operations_keys_test.go; this file holds their rig and fakes.
//
// Interface assumed, fixed by the task brief (package api):
//
//	type Operations interface {
//		Ready(ctx context.Context) error                    // nil, or wrapping ErrUnavailable
//		Start(ctx context.Context, s OperationStart) error  // an existing workflow (open or closed) is no error
//		Open(ctx context.Context, id string) (bool, error)  // ErrNotFound when it has none
//		Cancel(ctx context.Context, id string) error        // ErrNotFound, ErrUnavailable
//		Delete(ctx context.Context, id string) error
//	}
//	type OperationStart struct {
//		ID, Kind, Target string // Kind: the accepting operationId, e.g. "createSlice"
//		TaskQueue        string // OperationsQueue
//		RejectDuplicate  bool   // always true
//	}
//	const OperationsQueue = "operations"
//	type Operator interface {
//		Do(ctx context.Context, id string) error
//		End(ctx context.Context, id string, status gen.OperationStatus, detail string) error
//	}
//	func IsRetryable(err error) bool
//	func NewEngine(cfg Config) (http.Handler, Operator)
//	Config.Operations Operations // durable with Config.Store a *store.Store
//
// The fake Operations below does what the Operation workflow does: Start runs the handler's
// Operator.Do in a goroutine, retries a retryable error up to x-limits.retry-attempts times (no
// waits), then calls End(failed, "failed after <n> attempts: <reason>"); a cancelled workflow
// ends its work and calls End(cancelled). An engine restart is a new store, slices.Service and
// handler over the same store directory, with a new fake whose workflows are "still there" (the
// open ones run Do again with the new Operator, as a lost heartbeat does) or "lost".
//
// Assumed beyond the brief, from the data model's text:
//   - the records are files operations/OP-<n>.json (fields id, kind, target, key, body,
//     accepted_at, ended_at, status, result, error) with the body in operations/OP-<n>.body,
//     in the store's directory; the tests read them only to see what is written and when;
//   - an operation is still answered exactly 24 h after its end ("a record ended more than 24 h
//     ago is removed") and 404 one second later;
//   - with the durable service unreachable, registerWorker answers 503 as every other state
//     change does (R6), whether or not a run waits for the slice's workers;
//   - a run operation whose run's workflow does not exist and whose run has no record ends
//     failed `the run was not started; send the request again` when its work runs.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/api/gen"
	slicesvc "github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// ---- a log shared by the fakes, to see the order of calls ----

type dopLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *dopLog) add(s string) { l.mu.Lock(); l.lines = append(l.lines, s); l.mu.Unlock() }
func (l *dopLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.lines)
}
func (l *dopLog) index(s string) int { return slices.Index(l.all(), s) }

// ---- the fake Runs ----

type dopRuns struct {
	mu        sync.Mutex
	log       *dopLog
	down      bool            // every call fails ErrUnavailable
	startDown bool            // Start fails ErrUnavailable
	exists    map[string]bool // runs that have a workflow
	ended     map[string]bool // runs whose workflow has closed: Cancel is ErrNotFound
	starts    []RunStart
}

func newDopRuns(log *dopLog) *dopRuns {
	return &dopRuns{log: log, exists: map[string]bool{}, ended: map[string]bool{}}
}

func (f *dopRuns) unavailable() error { return fmt.Errorf("fake durable service: %w", ErrUnavailable) }

func (f *dopRuns) Start(_ context.Context, r RunStart) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down || f.startDown {
		return f.unavailable()
	}
	f.log.add("runs.start " + r.Run)
	f.exists[r.Run] = true
	f.starts = append(f.starts, r)
	return nil
}
func (f *dopRuns) Signal(_ context.Context, run, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return f.unavailable()
	}
	f.log.add("runs.signal " + run + " " + name)
	return nil
}
func (f *dopRuns) Cancel(_ context.Context, run string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return f.unavailable()
	}
	f.log.add("runs.cancel " + run)
	if f.ended[run] || !f.exists[run] {
		return fmt.Errorf("fake durable service: run %s: %w", run, ErrNotFound)
	}
	return nil
}
func (f *dopRuns) Exists(_ context.Context, run string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return false, f.unavailable()
	}
	return f.exists[run], nil
}
func (f *dopRuns) Closed(_ context.Context, run string) (bool, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return false, "", f.unavailable()
	}
	if !f.exists[run] {
		return false, "", fmt.Errorf("fake durable service: run %s: %w", run, ErrNotFound)
	}
	return f.ended[run], "", nil
}
func (f *dopRuns) Delete(_ context.Context, run string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return f.unavailable()
	}
	f.log.add("runs.delete " + run)
	delete(f.exists, run)
	return nil
}
func (f *dopRuns) startCount() int         { f.mu.Lock(); defer f.mu.Unlock(); return len(f.starts) }
func (f *dopRuns) set(fn func(f *dopRuns)) { f.mu.Lock(); fn(f); f.mu.Unlock() }

// ---- the fake Operations: the Operation workflow ----

type dopWF struct {
	start     OperationStart
	closed    bool
	cancelled bool
	running   bool
	cancel    context.CancelFunc
}

// dopAtStart is what the store held when a workflow was started.
type dopAtStart struct {
	record, body bool
	bodyBytes    []byte
}

type dopOps struct {
	t       *testing.T
	dir     string // the store's directory
	log     *dopLog
	retries int

	mu        sync.Mutex
	ctx       context.Context // the engine's: Do's context ends with it
	op        Operator
	hold      bool // Start records the workflow but does not run its work
	down      bool // Ready, Start, Open and Cancel fail ErrUnavailable
	startDown bool // Start fails ErrUnavailable
	wfs       map[string]*dopWF
	starts    []OperationStart
	atStart   map[string]dopAtStart
	doCalls   map[string]int
	wg        sync.WaitGroup
}

func newDopOps(t *testing.T, dir string, log *dopLog) *dopOps {
	return &dopOps{t: t, dir: dir, log: log, retries: contractLimit(t, "retry-attempts"),
		wfs: map[string]*dopWF{}, atStart: map[string]dopAtStart{}, doCalls: map[string]int{}}
}

func (f *dopOps) unavailable() error { return fmt.Errorf("fake durable service: %w", ErrUnavailable) }

func (f *dopOps) Ready(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return f.unavailable()
	}
	return nil
}

func (f *dopOps) Start(_ context.Context, s OperationStart) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down || f.startDown {
		return f.unavailable()
	}
	f.log.add("ops.start " + s.ID)
	f.starts = append(f.starts, s)
	if _, ok := f.wfs[s.ID]; ok {
		return nil // RejectDuplicate: an existing workflow is no error, and it is not run again
	}
	rec, recErr := os.Stat(filepath.Join(f.dir, "operations", s.ID+".json"))
	body, bodyErr := os.ReadFile(filepath.Join(f.dir, "operations", s.ID+".body"))
	f.atStart[s.ID] = dopAtStart{record: recErr == nil && rec.Mode().IsRegular(), body: bodyErr == nil, bodyBytes: body}
	f.wfs[s.ID] = &dopWF{start: s}
	if !f.hold {
		f.launchLocked(s.ID)
	}
	return nil
}

func (f *dopOps) Open(_ context.Context, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return false, f.unavailable()
	}
	wf, ok := f.wfs[id]
	if !ok {
		return false, fmt.Errorf("fake durable service: operation %s: %w", id, ErrNotFound)
	}
	return !wf.closed, nil
}

func (f *dopOps) Cancel(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return f.unavailable()
	}
	f.log.add("ops.cancel " + id + " record=" + dopRecordStatus(f.dir, id))
	wf, ok := f.wfs[id]
	if !ok {
		return fmt.Errorf("fake durable service: operation %s: %w", id, ErrNotFound)
	}
	if wf.closed {
		return nil
	}
	wf.cancelled = true
	if wf.running {
		wf.cancel() // the work's goroutine ends it cancelled
		return nil
	}
	wf.closed = true // never ran: the workflow's cancellation writes the end
	op, ctx := f.op, f.ctx
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		if op != nil {
			_ = op.End(ctx, id, gen.OperationStatusCancelled, "")
		}
	}()
	return nil
}

func (f *dopOps) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log.add("ops.delete " + id)
	delete(f.wfs, id)
	return nil
}

// use gives the fake the Operator of the handler it serves and runs the work of every open
// workflow that is not held (after a restart: the work repeated, as after a lost heartbeat).
func (f *dopOps) use(op Operator) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.op = op
	if f.hold {
		return
	}
	for id, wf := range f.wfs {
		if !wf.closed && !wf.running {
			f.launchLocked(id)
		}
	}
}

// release runs the work of a held, open workflow.
func (f *dopOps) release(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if wf, ok := f.wfs[id]; ok && !wf.closed && !wf.running {
		f.launchLocked(id)
	}
}

func (f *dopOps) launchLocked(id string) {
	wf := f.wfs[id]
	if f.op == nil {
		return // run by use once the Operator is known
	}
	ctx, cancel := context.WithCancel(f.ctx)
	wf.running, wf.cancel = true, cancel
	op, engine := f.op, f.ctx
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		defer cancel()
		for n := 1; ; n++ {
			f.mu.Lock()
			f.doCalls[id]++
			f.mu.Unlock()
			err := op.Do(ctx, id)
			f.mu.Lock()
			cancelled := wf.cancelled
			f.mu.Unlock()
			switch {
			case cancelled:
				_ = op.End(engine, id, gen.OperationStatusCancelled, "")
			case err == nil:
			case engine.Err() != nil: // the engine stopped: the workflow stays open
				f.mu.Lock()
				wf.running = false
				f.mu.Unlock()
				return
			case IsRetryable(err) && n <= f.retries:
				continue
			case IsRetryable(err):
				_ = op.End(engine, id, gen.OperationStatusFailed, fmt.Sprintf("failed after %d attempts: %v", n, err))
			}
			f.mu.Lock()
			wf.closed, wf.running = true, false
			f.mu.Unlock()
			return
		}
	}()
}

// carry is the fake a restarted engine finds: the same workflows when they are still there, none
// when they were lost. Its work is not run until use.
func (f *dopOps) carry(lost bool) *dopOps {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := newDopOps(f.t, f.dir, f.log)
	if !lost {
		for id, wf := range f.wfs {
			n.wfs[id] = &dopWF{start: wf.start, closed: wf.closed}
		}
	}
	return n
}

func (f *dopOps) set(fn func(f *dopOps)) { f.mu.Lock(); fn(f); f.mu.Unlock() }
func (f *dopOps) startsOf(id string) []OperationStart {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []OperationStart
	for _, s := range f.starts {
		if s.ID == id {
			out = append(out, s)
		}
	}
	return out
}
func (f *dopOps) allStarts() []OperationStart {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.starts)
}
func (f *dopOps) startedWith(id string) dopAtStart {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.atStart[id]
}
func (f *dopOps) closeWithoutEnd(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if wf, ok := f.wfs[id]; ok {
		wf.closed = true
	}
}

// wait waits for every goroutine of the fake to return (after the engine's context ended).
func (f *dopOps) wait(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() { f.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the fake workflows' work did not return within 10 s of the engine's stop")
	}
}

// ---- the rig: a real store and slices.Service, the fakes, NewEngine ----

type dopRig struct {
	t        *testing.T
	dir      string
	projects string
	clock    *fakeClock
	analyst  *spAnalyst
	log      *dopLog
	runs     *dopRuns
	ops      *dopOps
	client   string // the session's client token

	st      *store.Store
	svc     *slicesvc.Service
	r       *rig
	op      Operator
	cancel  context.CancelFunc
	running *sync.WaitGroup
}

func newDopRig(t *testing.T) *dopRig {
	t.Helper()
	x := &dopRig{t: t, dir: t.TempDir(), projects: t.TempDir(), clock: newFakeClock(),
		analyst: &spAnalyst{results: []spResult{{p: spNotes(t)}}}, log: &dopLog{}, client: clientToken}
	x.runs = newDopRuns(x.log)
	x.ops = newDopOps(t, x.dir, x.log)
	x.boot()
	return x
}

func (x *dopRig) boot() {
	x.t.Helper()
	st, err := store.Open(x.dir, spSchemas(x.t))
	if err != nil {
		x.t.Fatalf("opening the store: %v", err)
	}
	svc, err := slicesvc.New(slicesvc.Config{Schemas: spSchemas(x.t), Analyst: x.analyst, Diff: spDiff{}, Lint: spLint{},
		Pattern: spPattern(x.t), Records: st})
	if err != nil {
		x.t.Fatalf("the slices over the store: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	x.t.Cleanup(cancel)
	x.st, x.svc, x.cancel, x.running = st, svc, cancel, &sync.WaitGroup{}
	x.ops.set(func(f *dopOps) { f.ctx = ctx })
	rec := &recorder{}
	cfg := Config{
		Tokens:       Tokens{Client: x.client, Page: pageToken, Worker: workerToken},
		Now:          x.clock.Now,
		Sleep:        func(time.Duration) {},
		Work:         rec.work,
		Context:      ctx,
		Running:      x.running,
		Slices:       svc,
		Store:        st,
		WorkerGrants: stepGrants("S-1"),
		Projects:     x.projects,
		Model:        "test-model",
		StepWorker:   &fakeStepWorker{st: st, data: map[string][]byte{}},
		StepChecker:  &fakeStepChecker{},
		Workers:      NewWorkers(),
		Runs:         x.runs,
		Operations:   x.ops,
	}
	h, op := NewEngine(cfg)
	if h == nil || op == nil {
		x.t.Fatal("NewEngine gave no handler or no Operator")
	}
	x.r, x.op = &rig{h: h, clock: x.clock, rec: rec}, op
	x.ops.use(op)
}

// restart stops the engine (its context ends, its work returns) and starts a new one over the
// same store directory; the durable service's workflows are still there unless lost. hold: the
// new fake does not run work until release.
func (x *dopRig) restart(lost, hold bool) {
	x.t.Helper()
	x.cancel()
	x.ops.wait(x.t)
	waitReturns(x.t, x.running)
	x.ops = x.ops.carry(lost)
	x.ops.set(func(f *dopOps) { f.hold = hold })
	x.boot()
}

func (x *dopRig) do(method, path, key, body string) reply {
	x.clock.Advance(80 * time.Millisecond)
	var h map[string]string
	if key != "" {
		h = map[string]string{"Idempotency-Key": key}
	}
	return x.r.do(method, path, x.client, body, h)
}

func (x *dopRig) get(path string) reply { return x.do("GET", path, "", "") }

func (x *dopRig) getOp(id string) reply {
	return x.get("/operations/" + id + "?api-version=" + apiVersion)
}

// opView reads an operation answer with its status code.
func (x *dopRig) opView(p reply, status int) spOp {
	x.t.Helper()
	p.operation(x.t, status)
	var op spOp
	if err := json.Unmarshal(p.Body.Bytes(), &op); err != nil {
		x.t.Fatal(err)
	}
	return op
}

func (x *dopRig) accepted(p reply, kind, target string) spOp {
	x.t.Helper()
	op := x.opView(p, 202)
	if op.Kind != kind || op.Target != target {
		x.t.Fatalf("operation kind/target = %s/%s, want %s/%s", op.Kind, op.Target, kind, target)
	}
	return op
}

// settle follows the operation with getOperation until it has ended.
func (x *dopRig) settle(id string) spOp {
	x.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		x.clock.Advance(1100 * time.Millisecond) // stay under the rate limit
		op := x.opView(x.r.do("GET", "/operations/"+id+"?api-version="+apiVersion, x.client, "", nil), 200)
		switch op.Status {
		case "succeeded", "failed", "cancelled":
			return op
		}
		if time.Now().After(deadline) {
			x.t.Fatalf("operation %s is still %q after 10 s", id, op.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// opFiles are the names in the store's operations folder.
func (x *dopRig) opFiles() []string {
	x.t.Helper()
	entries, err := os.ReadDir(filepath.Join(x.dir, "operations"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		x.t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// records are the operation records in the store's operations folder.
func (x *dopRig) records() []string {
	var out []string
	for _, n := range x.opFiles() {
		if strings.HasSuffix(n, ".json") {
			out = append(out, strings.TrimSuffix(n, ".json"))
		}
	}
	return out
}

func (x *dopRig) record(id string) map[string]any {
	x.t.Helper()
	raw, err := os.ReadFile(filepath.Clean(filepath.Join(x.dir, "operations", id+".json")))
	if err != nil {
		x.t.Fatalf("the record of %s: %v", id, err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		x.t.Fatalf("the record of %s is not JSON: %v", id, err)
	}
	return m
}

func dopRecordStatus(dir, id string) string {
	raw, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "operations", id+".json")))
	if err != nil {
		return "none"
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return "unreadable"
	}
	s, _ := m["status"].(string)
	return s
}

// seed creates S-1 (and more) through the service; approved approves every part of S-1.
func (x *dopRig) seed(n int, approved bool) {
	x.t.Helper()
	for i := 0; i < n; i++ {
		if _, err := x.svc.Create(context.Background(), "Let me keep notes."); err != nil {
			x.t.Fatal(err)
		}
	}
	if approved {
		(&spRig{svc: x.svc}).approveAll(x.t, "S-1")
	}
}

// registerAll registers S-1's step workers with the current handler.
func (x *dopRig) registerAll() {
	x.t.Helper()
	x.clock.Advance(1100 * time.Millisecond)
	registerAll(x.t, x.r, "S-1")
}

// recordRun writes RUN-1's record (slice S-1) as its workflow would, and marks its workflow as
// existing.
func (x *dopRig) recordRun(kinds ...string) {
	x.t.Helper()
	(&durableRig{t: x.t, sp: &spRig{svc: x.svc}, st: x.st}).record(kinds...)
	x.runs.set(func(f *dopRuns) { f.exists["RUN-1"] = true })
}

func (x *dopRig) goalsBody(text string) string {
	x.t.Helper()
	sl, err := x.svc.Get("S-1")
	if err != nil {
		x.t.Fatal(err)
	}
	goals := sl.Goals
	goals[0].Text = text
	return jsonOf(x.t, map[string]any{"goals": goals})
}

func (x *dopRig) startRun() reply {
	return x.do("POST", spURL("S-1", "/runs"), "", `{"project":"notes-app"}`)
}

// ---- the 503 answer, validated against the contract ----

// dopUnavailable checks a 503 with Retry-After whose body the contract's UnavailableProblem
// schema accepts, and that the contract's ErrorCode has no value Unavailable.
func dopUnavailable(t *testing.T, p reply) {
	t.Helper()
	unavailable(t, p)
	raw, err := fs.ReadFile(contractFiles, contractPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	asJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	root, err := jsonschema.UnmarshalJSON(bytes.NewReader(asJSON))
	if err != nil {
		t.Fatal(err)
	}
	comp := jsonschema.NewCompiler()
	comp.DefaultDraft(jsonschema.Draft2020)
	comp.UseLoader(contractLoader{files: contractFiles, dir: specDir})
	if err := comp.AddResource(schemaBase+"openapi.yaml", root); err != nil {
		t.Fatal(err)
	}
	sch, err := comp.Compile(schemaBase + "openapi.yaml#/components/schemas/UnavailableProblem")
	if err != nil {
		t.Fatal(err)
	}
	body, err := jsonschema.UnmarshalJSON(bytes.NewReader(p.Body.Bytes()))
	if err != nil {
		t.Fatalf("the 503 body is not JSON: %v", err)
	}
	if err := sch.Validate(body); err != nil {
		t.Fatalf("the 503 body does not validate as UnavailableProblem: %v\n%s", err, p.Body.String())
	}
	var c struct {
		Components struct {
			Schemas struct {
				ErrorCode struct {
					Enum []string `yaml:"enum"`
				} `yaml:"ErrorCode"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Components.Schemas.ErrorCode.Enum) == 0 || slices.Contains(c.Components.Schemas.ErrorCode.Enum, "Unavailable") {
		t.Fatalf("the contract's ErrorCode enum is %v: it must exist and not hold Unavailable", c.Components.Schemas.ErrorCode.Enum)
	}
}

// ---- accepting ----

func TestDurableOpIsAnswered202OnlyAfterItsRecordBodyAndWorkflowExist(t *testing.T) {
	x := newDopRig(t)
	x.ops.set(func(f *dopOps) { f.hold = true })
	op := x.accepted(x.do("POST", slicesURL(""), "", createBody), "propose-slice", "S-1")
	starts := x.ops.startsOf(op.ID)
	if len(starts) != 1 {
		t.Fatalf("the operation's workflow was started %d times before the 202, want 1: %v", len(starts), x.ops.allStarts())
	}
	want := OperationStart{ID: op.ID, Kind: "createSlice", Target: "S-1", TaskQueue: OperationsQueue, RejectDuplicate: true}
	if starts[0] != want {
		t.Errorf("start = %+v, want %+v", starts[0], want)
	}
	if OperationsQueue != "operations" {
		t.Errorf("OperationsQueue = %q, want operations", OperationsQueue)
	}
	at := x.ops.startedWith(op.ID)
	if !at.record || !at.body {
		t.Fatalf("when its workflow was started the store held record %v, body %v; want both", at.record, at.body)
	}
	if string(at.bodyBytes) != createBody {
		t.Errorf("the kept body is %q, want the request's exact bytes %q", at.bodyBytes, createBody)
	}
	rec := x.record(op.ID)
	if rec["id"] != op.ID || rec["kind"] != "createSlice" || rec["target"] != "S-1" || rec["accepted_at"] == nil || rec["ended_at"] != nil {
		t.Errorf("record = %v, want id %s, kind createSlice, target S-1, accepted_at and no ended_at", rec, op.ID)
	}
	// The work runs; its end is written once and the body is then removed (ADR 0072).
	x.ops.release(op.ID)
	end := x.settle(op.ID)
	if end.Status != "succeeded" || end.Result["slice"] != "S-1" {
		t.Fatalf("operation %+v, want succeeded with slice S-1", end)
	}
	if slices.Contains(x.opFiles(), op.ID+".body") {
		t.Errorf("the body %s.body is still kept after the end: %v", op.ID, x.opFiles())
	}
	if rec := x.record(op.ID); rec["status"] != "succeeded" || rec["ended_at"] == nil {
		t.Errorf("record after the end = %v, want status succeeded with ended_at", rec)
	}
}

func TestDurableOpRunOperationStartsItsRunBeforeItsWorkflow(t *testing.T) {
	x := newDopRig(t)
	x.seed(1, true)
	x.registerAll()
	x.ops.set(func(f *dopOps) { f.hold = true })
	op := x.accepted(x.startRun(), "run", "RUN-1")
	starts := x.ops.startsOf(op.ID)
	if len(starts) != 1 || starts[0] != (OperationStart{ID: op.ID, Kind: "startRun", Target: "RUN-1", TaskQueue: OperationsQueue, RejectDuplicate: true}) {
		t.Fatalf("operation workflow starts = %+v, want one of kind startRun on RUN-1", starts)
	}
	r, o := x.log.index("runs.start RUN-1"), x.log.index("ops.start "+op.ID)
	if r < 0 || o < 0 || r > o {
		t.Fatalf("calls %v: want the run started before the operation's workflow", x.log.all())
	}
	if at := x.ops.startedWith(op.ID); !at.record || !at.body || string(at.bodyBytes) != `{"project":"notes-app"}` {
		t.Errorf("at the workflow's start: %+v, want the record and the exact body", at)
	}
}

func TestDurableOpWhenReadyFailsIs503AndWritesNothing(t *testing.T) {
	x := newDopRig(t)
	x.seed(1, false)
	before, _ := x.svc.Get("S-1")
	x.ops.set(func(f *dopOps) { f.down = true })
	dopUnavailable(t, x.do("PUT", spURL("S-1", "/goals"), "", x.goalsBody("Keep a note for good")))
	dopUnavailable(t, x.do("POST", slicesURL(""), "", createBody))
	if files := x.opFiles(); len(files) != 0 {
		t.Errorf("the operations folder holds %v after the 503s, want nothing", files)
	}
	if s := x.ops.allStarts(); len(s) != 0 {
		t.Errorf("workflows started: %v", s)
	}
	if after, _ := x.svc.Get("S-1"); after.Parts.Goals != before.Parts.Goals {
		t.Errorf("the goals changed although the answer was 503")
	}
	x.ops.set(func(f *dopOps) { f.down = false })
	x.getOp("OP-1").problem(t, 404, gen.ErrorCodeNotFound)
}

func TestDurableOpAFailingRecordWriteIs503AndAcceptsNothing(t *testing.T) {
	x := newDopRig(t)
	x.seed(1, false)
	before, _ := x.svc.Get("S-1")
	body := x.goalsBody("Keep a note for good")
	// The operations folder is a regular file: no body or record can be created in it.
	opsDir := filepath.Join(x.dir, "operations")
	if err := os.RemoveAll(opsDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opsDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	dopUnavailable(t, x.do("PUT", spURL("S-1", "/goals"), "key-write", body))
	if err := os.Remove(opsDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(opsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if files := x.opFiles(); len(files) != 0 {
		t.Errorf("the operations folder holds %v after the 503, want nothing", files)
	}
	if s := x.ops.allStarts(); len(s) != 0 {
		t.Errorf("workflows started for a 503: %v", s)
	}
	if after, _ := x.svc.Get("S-1"); after.Parts.Goals != before.Parts.Goals {
		t.Errorf("the goals changed although the answer was 503")
	}
	// The 503 was not kept under the key: the same request again is judged again and accepted.
	op := x.accepted(x.do("PUT", spURL("S-1", "/goals"), "key-write", body), "replace-goals", "S-1")
	if end := x.settle(op.ID); end.Status != "succeeded" {
		t.Fatalf("the resent request's operation %+v, want succeeded", end)
	}
}

func TestDurableOpAWorkflowStartThatFailsAfterTheRecordIsStill202(t *testing.T) {
	x := newDopRig(t)
	x.ops.set(func(f *dopOps) { f.startDown = true })
	op := x.accepted(x.do("POST", slicesURL(""), "", createBody), "propose-slice", "S-1")
	if !slices.Contains(x.records(), op.ID) {
		t.Fatalf("the accepted operation %s has no record: %v", op.ID, x.opFiles())
	}
	if p := x.opView(x.getOp(op.ID), 200); p.Status != "accepted" {
		t.Errorf("status = %q while it has no workflow, want accepted", p.Status)
	}
	// The outbox starts it at the next engine start (and every minute, not driven here).
	x.restart(true, false)
	if s := x.ops.startsOf(op.ID); len(s) == 0 || s[0] != (OperationStart{ID: op.ID, Kind: "createSlice", Target: "S-1", TaskQueue: OperationsQueue, RejectDuplicate: true}) {
		t.Fatalf("at engine start the outbox started %+v, want the operation's workflow", s)
	}
	if end := x.settle(op.ID); end.Status != "succeeded" || end.Result["slice"] != "S-1" {
		t.Fatalf("operation %+v, want succeeded with slice S-1", end)
	}
	if n := x.analyst.calls(); n != 1 {
		t.Errorf("the analyst was called %d times, want 1", n)
	}
}

func TestDurableOpAStartRunWhoseRunCannotStartIs503AndKeepsNoRecord(t *testing.T) {
	x := newDopRig(t)
	x.seed(1, true)
	x.registerAll()
	x.runs.set(func(f *dopRuns) { f.startDown = true })
	dopUnavailable(t, x.startRun())
	if r := x.records(); len(r) != 0 {
		t.Errorf("records %v are kept after the run could not start, want none", r)
	}
	if s := x.ops.allStarts(); len(s) != 0 {
		t.Errorf("operation workflows started: %v", s)
	}
}

// ---- the durable service unreachable (R6) ----

func TestDurableOpUnreachableEveryStateChangeIs503(t *testing.T) {
	cases := []struct {
		name  string
		setup func(x *dopRig)
		send  func(x *dopRig) reply
	}{
		{"createSlice", nil, func(x *dopRig) reply { return x.do("POST", slicesURL(""), "", createBody) }},
		{"replaceGoals", nil, func(x *dopRig) reply {
			return x.do("PUT", spURL("S-1", "/goals"), "", x.goalsBody("Keep a note for good"))
		}},
		{"replaceContract", nil, func(x *dopRig) reply {
			sl, _ := x.svc.Get("S-1")
			return x.do("PUT", spURL("S-1", "/contract"), "", jsonOf(x.t, map[string]string{"contract": sl.Contract + "# reviewed\n"}))
		}},
		{"approvePart", nil, func(x *dopRig) reply {
			sl, _ := x.svc.Get("S-2")
			return x.do("POST", spURL("S-2", "/approvals"), "", approvalBody("goals", sl.Parts.Goals))
		}},
		{"deleteSlice", nil, func(x *dopRig) reply { return x.do("DELETE", spURL("S-2", ""), "", "") }},
		{"startRun", nil, func(x *dopRig) reply { return x.startRun() }},
		{"resumeRun", func(x *dopRig) { x.recordRun("RunInterrupted=the model is unavailable") }, func(x *dopRig) reply {
			return x.do("POST", "/runs/RUN-1/resume?api-version="+apiVersion, "", "")
		}},
		{"acceptHandOver", func(x *dopRig) { x.recordRun("RunHandedOver=the step's checks failed 3 times") }, func(x *dopRig) reply {
			return x.do("POST", "/runs/RUN-1/hand-over?api-version="+apiVersion, "", "")
		}},
		{"cancelOperation", func(x *dopRig) {
			x.ops.set(func(f *dopOps) { f.hold = true })
			x.accepted(x.do("PUT", spURL("S-1", "/goals"), "", x.goalsBody("Held")), "replace-goals", "S-1")
		}, func(x *dopRig) reply {
			return x.do("POST", "/operations/OP-1/cancel?api-version="+apiVersion, "", "")
		}},
		{"registerWorker", nil, func(x *dopRig) reply {
			x.clock.Advance(1100 * time.Millisecond)
			return register(x.r, stepToken("S-1", "code"), "http://code-2:8080")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := newDopRig(t)
			x.seed(2, true)
			x.registerAll()
			if c.setup != nil {
				c.setup(x)
			}
			records := x.records()
			x.ops.set(func(f *dopOps) { f.down = true })
			x.runs.set(func(f *dopRuns) { f.down = true })
			dopUnavailable(t, c.send(x))
			if got := x.records(); !slices.Equal(got, records) {
				t.Errorf("records %v after the 503, want %v", got, records)
			}
		})
	}
}

func TestDurableOpUnreachableEveryQueryStillAnswers(t *testing.T) {
	x := newDopRig(t)
	x.seed(1, false)
	ended := x.accepted(x.do("PUT", spURL("S-1", "/goals"), "", x.goalsBody("Keep a note for good")), "replace-goals", "S-1")
	if e := x.settle(ended.ID); e.Status != "succeeded" {
		t.Fatalf("operation %+v, want succeeded", e)
	}
	x.ops.set(func(f *dopOps) { f.hold = true })
	open := x.accepted(x.do("PUT", spURL("S-1", "/goals"), "", x.goalsBody("Keep a note for ever")), "replace-goals", "S-1")
	x.recordRun("RunInterrupted=the model is unavailable")
	x.ops.set(func(f *dopOps) { f.down = true })
	x.runs.set(func(f *dopRuns) { f.down = true })

	for _, path := range []string{
		slicesURL(""), spURL("S-1", ""), "/runs?api-version=" + apiVersion, "/runs/RUN-1?api-version=" + apiVersion,
		"/runs/RUN-1/events?api-version=" + apiVersion, "/providers/Agentic.Engine/operations",
	} {
		if p := x.get(path); p.Code != 200 {
			t.Errorf("GET %s = %d while the durable service is unreachable, want 200: %s", path, p.Code, p.Body.String())
		}
	}
	if e := x.opView(x.getOp(ended.ID), 200); e.Status != "succeeded" || e.Result["slice"] != "S-1" {
		t.Errorf("the ended operation reads %+v, want succeeded from its record", e)
	}
	if o := x.opView(x.getOp(open.ID), 200); o.Status != "running" {
		t.Errorf("the open operation reads %q while Temporal cannot be asked, want running", o.Status)
	}
}

// ---- cancelling ----

func TestDurableOpCancellingARunOperationCancelsItsRunFirst(t *testing.T) {
	for _, c := range []struct {
		name  string
		kinds []string
		ended bool
	}{
		{"waiting for the developer", []string{"RunHandedOver=the step's checks failed 3 times"}, false},
		{"its run has ended", []string{"RunCompleted"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			x := newDopRig(t)
			x.seed(1, true)
			x.registerAll()
			x.ops.set(func(f *dopOps) { f.hold = true })
			op := x.accepted(x.startRun(), "run", "RUN-1")
			x.recordRun(c.kinds...)
			x.runs.set(func(f *dopRuns) { f.ended["RUN-1"] = c.ended })
			x.accepted(x.do("POST", "/operations/"+op.ID+"/cancel?api-version="+apiVersion, "", ""), "run", "RUN-1")
			rc := x.log.index("runs.cancel RUN-1")
			oc := x.log.index("ops.cancel " + op.ID + " record=cancelled")
			if rc < 0 || oc < 0 || rc > oc {
				t.Fatalf("calls %v: want the run cancelled, then the end cancelled written, then the workflow cancelled", x.log.all())
			}
			end := x.settle(op.ID)
			if end.Status != "cancelled" {
				t.Fatalf("operation %+v, want cancelled (never failed: the run was cancelled)", end)
			}
			x.do("POST", "/operations/"+op.ID+"/cancel?api-version="+apiVersion, "", "").problem(t, 409, gen.ErrorCodeOperationEnded)
		})
	}
}

func TestDurableOpCancellingAnEndedOperationIs409AndAnUnreachableServiceIs503(t *testing.T) {
	x := newDopRig(t)
	x.seed(1, false)
	op := x.accepted(x.do("PUT", spURL("S-1", "/goals"), "", x.goalsBody("Keep a note for good")), "replace-goals", "S-1")
	x.settle(op.ID)
	x.do("POST", "/operations/"+op.ID+"/cancel?api-version="+apiVersion, "", "").problem(t, 409, gen.ErrorCodeOperationEnded)

	x.ops.set(func(f *dopOps) { f.hold = true })
	held := x.accepted(x.do("PUT", spURL("S-1", "/goals"), "", x.goalsBody("Held")), "replace-goals", "S-1")
	x.ops.set(func(f *dopOps) { f.down = true })
	dopUnavailable(t, x.do("POST", "/operations/"+held.ID+"/cancel?api-version="+apiVersion, "", ""))
	if s := dopRecordStatus(x.dir, held.ID); s != "" {
		t.Errorf("the record's status is %q after a 503 cancel, want none written", s)
	}
}
