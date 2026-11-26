package api

// T027 (feature 011, US1; FR-005, FR-006, FR-010, FR-011; data model "Proof slice" and "Runs that
// continue"; contract "Engine API: /captured-patterns", makeProofSlice, startRun, replayRun):
// the proof slice of a captured pattern, through HTTP and the generated contract types, on the
// engine rig of runs_ops_test.go with faked step workers and no model. Written before the code
// (T029) by a test author; the code author must not change it (ADR 0029).
//
// A captured pattern is made the only way a client can: a done run of S-1, then captureRun (T007).
// The tests compile against the T001 stubs and fail until captureRun and makeProofSlice exist.
//
// Seams: runs_ops_test.go (runsRig, its fakes), slices_read_test.go (spRig, spOp), workers_test.go
// (grants, registration), open_round_reload_test.go (rawEvents), slices_checks_method_test.go
// (cmMethodA, cmMethodB: a method change is a second slices.Service under another method over
// the same store, which is the slices' records, as an engine restarted on a new method is).
//
// How a part's check fails and how an operation is held open (no existing seam holds a captured
// pattern's operation open, so both go through the slices service's contract lint, which the
// operation runs through the service's part checks): psLint fails every contract on demand, and
// holds the next contract lint call until released.
//
// Edge cases checked: the proof slice's parts, description, history and proof_of in getSlice and
// listSlices, with the response checked against slice.schema.json; a failing part check (no
// slice made, no proof_slice set, the next ask works); createSlice with the source's description
// and with the proof slice's own description; asking again (same slice, same history, no new
// slice); a method change that withdrew a part's approval (failing again: PartFailsChecks and the
// slice as it was; passing: re-approved, one `approved` entry per withdrawn part); erasure of the
// proof slice, then of the source slice and its runs, each time a new proof slice from the copy;
// a proof slice made while another operation runs (Busy); a proof's start, with and without
// `pattern`, and a replay of a proof run, while an operation runs (Busy), and a non-proof start
// of the source slice naming the captured pattern in the same state (CapturedPatternNotApproved,
// never Busy); `pattern` naming the base pattern, another captured pattern, an unknown pattern,
// and a part changed and approved again (PatternDoesNotFit, NotFound, nothing started); deleting
// the proof slice right after its run ended keeps the proof, the template and its artifacts;
// worker and page tokens (403, nothing made); an unknown captured pattern (404); a proof run is
// not capturable.
//
// Left out, and why: a captured pattern that is proven, approved or unusable (needs a passing
// proof, which needs the fill path of T015 and the rig's durable service to run it: the rig's
// activities are fixed in durable_service_test.go, an existing helper, so whether it can run a
// `fill` step is for T006/T029 to settle; the cases "whatever its state" and "also when unusable"
// are therefore proved here for `proposed` only); a withdrawn one (T018's); a restart in the
// middle of the operation (operations_restart_test.go's seam); the CLI (T008, T028).

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	goslices "slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
)

// ---- the lint that fails on demand and holds on demand ----

type psLint struct {
	mu      sync.Mutex
	fail    bool
	armed   bool
	entered chan struct{}
	gate    chan struct{}
}

func (l *psLint) setFail(v bool) { l.mu.Lock(); l.fail = v; l.mu.Unlock() }

// arm makes the next Lint call block until release; entered is closed when it has blocked.
func (l *psLint) arm() (entered <-chan struct{}, release func()) {
	e, g := make(chan struct{}), make(chan struct{})
	l.mu.Lock()
	l.armed, l.entered, l.gate = true, e, g
	l.mu.Unlock()
	var once sync.Once
	return e, func() { once.Do(func() { close(g) }) }
}

func (l *psLint) Lint(ctx context.Context, _ string) ([]slices.LintFinding, error) {
	l.mu.Lock()
	armed, e, g, fail := l.armed, l.entered, l.gate, l.fail
	l.armed = false
	l.mu.Unlock()
	if armed {
		close(e)
		select {
		case <-g:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if fail {
		return []slices.LintFinding{{Level: "error", Code: "oas3-schema", Text: "invalid"}}, nil
	}
	return nil, nil
}

// ---- the rig ----

// psRig is a runsRig whose slices service keeps its records in the engine's store (so a second
// service over it is an engine restarted under another method), whose lint psLint controls, and
// whose handler grants step workers to S-1 to S-8.
type psRig struct {
	*runsRig
	lint       *psLint
	registered []string
}

func newPSRig(t *testing.T) *psRig {
	t.Helper()
	st, err := store.Open(t.TempDir(), spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	lint := &psLint{}
	sp := newSP(t, []spResult{{p: spNotes(t)}}, st, func(c *slices.Config) {
		c.Lint, c.Records, c.Method = lint, st, cmMethodA
	})
	sp.approveAfterSeed(t)
	p := &psRig{lint: lint, runsRig: &runsRig{t: t, sp: sp, st: st, projects: t.TempDir(),
		w: &fakeStepWorker{st: st, data: map[string][]byte{}}, c: &fakeStepChecker{}}}
	p.w.set(p.psWork) // a capture reads a template: each output a framed file set (owner-approved 2026-11-26)
	p.psBoot()
	p.psRegister("S-1")
	return p
}

// psWork is the step worker: each declared output, except the review's findings, is written as
// the framed form of one file inside the step's first writable entry, as a real step output is.
func (p *psRig) psWork(_ context.Context, r machine.Request) (machine.Result, error) {
	out := map[string]string{}
	for _, name := range r.Step.Outputs {
		b := []byte("[]")
		if name != "findings" {
			dir := "out/"
			if len(r.Step.Writable) > 0 {
				dir = strings.TrimSuffix(r.Step.Writable[0], "**")
			}
			b = canonical.Framed([]canonical.File{{Path: dir + name + ".txt", Content: []byte("content of " + r.Step.Name + "/" + name + "\n")}})
		}
		h, err := p.st.PutArtifact("S-1", b)
		if err != nil {
			return machine.Result{}, err
		}
		p.w.mu.Lock()
		p.w.data[h] = b
		p.w.mu.Unlock()
		out[name] = h
	}
	return machine.Result{Model: "test-model", Outputs: out, Usage: machine.Usage{Tokens: 10}, Done: true}, nil
}

func psGrants() map[string]WorkerGrant {
	var gs []map[string]WorkerGrant
	for _, id := range []string{"S-1", "S-2", "S-3", "S-4", "S-5", "S-6", "S-7", "S-8"} {
		gs = append(gs, stepGrants(id))
	}
	return mergeGrants(gs...)
}

// psBoot makes a handler over the current slices service and the store, as an engine restart does.
func (p *psRig) psBoot() {
	ctx, cancel := context.WithCancel(context.Background())
	p.r = newRig(p.t, func(cfg *Config) {
		cfg.Context = ctx
		cfg.Slices, cfg.Store = p.sp.svc, p.st
		cfg.WorkerGrants = psGrants()
		cfg.Projects = p.projects
		cfg.Model = "test-model"
		cfg.StepWorker, cfg.StepChecker = p.w, p.c
	})
	stopEngineAtEnd(p.t, p.r, cancel)
	for _, id := range p.registered {
		registerAll(p.t, p.r, id)
	}
}

// psRegister registers the step workers of a slice (a proof is a run of its proof slice).
func (p *psRig) psRegister(slice string) {
	p.t.Helper()
	registerAll(p.t, p.r, slice)
	if !goslices.Contains(p.registered, slice) {
		p.registered = append(p.registered, slice)
	}
}

// psReboot restarts the engine on a slices service under another method over the same records.
func (p *psRig) psReboot(method string) {
	p.t.Helper()
	svc, err := slices.New(slices.Config{Schemas: p.sp.schemas, Analyst: p.sp.analyst, Diff: spDiff{}, Lint: p.lint,
		Pattern: spPattern(p.t), Records: p.st, Method: method})
	if err != nil {
		p.t.Fatal(err)
	}
	p.sp.svc = svc
	p.psBoot()
}

func (p *psRig) psAs(token, method, path, body string) reply {
	p.r.clock.Advance(80 * time.Millisecond)
	return p.r.do(method, apiPath(path), token, body, nil)
}

// ---- operations ----

type psOpView struct {
	ID     string            `json:"id"`
	Kind   string            `json:"kind"`
	Status string            `json:"status"`
	Target string            `json:"target"`
	Result map[string]string `json:"result"`
	Error  *struct {
		Code     string            `json:"code"`
		Detail   string            `json:"detail"`
		Failures []json.RawMessage `json:"failures"`
	} `json:"error"`
}

// psSettle follows an operation until it has ended, for at most 15 seconds.
func (p *psRig) psSettle(id string) psOpView {
	p.t.Helper()
	for end := time.Now().Add(15 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		r := p.do("GET", apiPath("/operations/"+id), "")
		if r.Code != 200 {
			p.t.Fatalf("getOperation %s = %d: %s", id, r.Code, r.Body.String())
		}
		var op psOpView
		if err := json.Unmarshal(r.Body.Bytes(), &op); err != nil {
			p.t.Fatal(err)
		}
		switch op.Status {
		case "succeeded", "failed", "cancelled":
			return op
		}
		if time.Now().After(end) {
			p.t.Fatalf("operation %s is still %q after 15 s", id, op.Status)
		}
	}
}

// psAccepted checks a 202 for an operation of kind and target, and follows it.
func (p *psRig) psAccepted(r reply, kind, target string) psOpView {
	p.t.Helper()
	op := r.operation(p.t, http.StatusAccepted)
	if string(op.Kind) != kind || op.Target != target {
		p.t.Fatalf("operation kind/target = %s/%s, want %s/%s", op.Kind, op.Target, kind, target)
	}
	return p.psSettle(op.Id)
}

// ---- captured patterns, slices and runs as the tests read them ----

type psCaptured struct {
	ID         string            `json:"id"`
	State      string            `json:"state"`
	ProofSlice *string           `json:"proof_slice"`
	Parts      map[string]string `json:"parts"`
	Template   map[string]string `json:"template"`
	Parameters []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"parameters"`
	Source struct {
		Run   string `json:"run"`
		Slice string `json:"slice"`
	} `json:"source"`
	Proofs []struct {
		Run        string            `json:"run"`
		Method     string            `json:"method"`
		Outcome    string            `json:"outcome"`
		Parameters map[string]string `json:"parameters"`
		Failures   []json.RawMessage `json:"failures"`
	} `json:"proofs"`
}

func (c psCaptured) values() map[string]string {
	m := map[string]string{}
	for _, pa := range c.Parameters {
		m[pa.Name] = pa.Value
	}
	return m
}

func (p *psRig) psGetCaptured(id string) psCaptured {
	p.t.Helper()
	r := p.do("GET", apiPath("/captured-patterns/"+id), "")
	if r.Code != 200 {
		p.t.Fatalf("getCapturedPattern %s = %d: %s", id, r.Code, r.Body.String())
	}
	var c psCaptured
	if err := json.Unmarshal(r.Body.Bytes(), &c); err != nil {
		p.t.Fatal(err)
	}
	return c
}

// psCaptureOK captures a run and returns the captured pattern's ID, which the operation targets.
func (p *psRig) psCaptureOK(name, project string) (id, run string) {
	p.t.Helper()
	run = p.runDone(project).ID
	r := p.do("POST", apiPath("/captured-patterns"), `{"run":"`+run+`","name":"`+name+`"}`)
	op := r.operation(p.t, http.StatusAccepted)
	if string(op.Kind) != "capture-run" {
		p.t.Fatalf("capture operation kind = %q, want capture-run", op.Kind)
	}
	if got := p.psSettle(op.Id); got.Status != "succeeded" {
		p.t.Fatalf("capture of %s: %+v", run, got)
	}
	return op.Target, run
}

// psSlice is getSlice's answer, with the raw object.
type psSlice struct {
	spSliceFull
	raw map[string]json.RawMessage
}

func (s psSlice) str(key string) (string, bool) {
	v, ok := s.raw[key]
	if !ok {
		return "", false
	}
	var out string
	if err := json.Unmarshal(v, &out); err != nil {
		return "", false
	}
	return out, true
}

func (p *psRig) psGetSlice(id string) psSlice {
	p.t.Helper()
	r := p.do("GET", spURL(id, ""), "")
	if r.Code != 200 {
		p.t.Fatalf("getSlice %s = %d: %s", id, r.Code, r.Body.String())
	}
	if err := p.sp.schemas.CheckSlice(r.Body.Bytes()); err != nil {
		p.t.Errorf("getSlice %s does not match slice.schema.json: %v", id, err)
	}
	var s psSlice
	if err := json.Unmarshal(r.Body.Bytes(), &s.spSliceFull); err != nil {
		p.t.Fatal(err)
	}
	if err := json.Unmarshal(r.Body.Bytes(), &s.raw); err != nil {
		p.t.Fatal(err)
	}
	return s
}

func (s psSlice) kinds() []string {
	var out []string
	for _, h := range s.History {
		out = append(out, h.Kind)
	}
	return out
}

// psListed returns the slice IDs listSlices gives, and each one's proof_of.
func (p *psRig) psListed() (ids []string, proofOf map[string]string) {
	p.t.Helper()
	r := p.do("GET", apiPath("/slices"), "")
	if r.Code != 200 {
		p.t.Fatalf("listSlices = %d: %s", r.Code, r.Body.String())
	}
	var l struct {
		Items []struct {
			ID      string  `json:"id"`
			ProofOf *string `json:"proof_of"`
		} `json:"items"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &l); err != nil {
		p.t.Fatal(err)
	}
	proofOf = map[string]string{}
	for _, it := range l.Items {
		ids = append(ids, it.ID)
		if it.ProofOf != nil {
			proofOf[it.ID] = *it.ProofOf
		}
	}
	return ids, proofOf
}

// psMakeProofSlice asks for the proof slice and follows the operation to its end.
func (p *psRig) psMakeProofSlice(id string) psOpView {
	p.t.Helper()
	return p.psAccepted(p.do("POST", apiPath("/captured-patterns/"+id+"/proof-slice"), ""), "make-proof-slice", id)
}

// psProofSliceOK asks for the proof slice, which must succeed, and returns its ID.
func (p *psRig) psProofSliceOK(id string) string {
	p.t.Helper()
	op := p.psMakeProofSlice(id)
	if op.Status != "succeeded" || !regexp.MustCompile(`^S-[0-9]+$`).MatchString(op.Result["slice"]) {
		p.t.Fatalf("makeProofSlice of %s: %+v (error %+v), want succeeded with result.slice an S-<n>", id, op, op.Error)
	}
	return op.Result["slice"]
}

// psDeleteSlice deletes a slice and waits until it is gone.
func (p *psRig) psDeleteSlice(id string) {
	p.t.Helper()
	op := p.do("DELETE", spURL(id, ""), "").operation(p.t, http.StatusAccepted)
	if got := p.psSettle(op.Id); got.Status != "succeeded" {
		p.t.Fatalf("deleteSlice %s: %+v", id, got)
	}
	for end := time.Now().Add(5 * time.Second); p.do("GET", spURL(id, ""), "").Code != 404; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(end) {
			p.t.Fatalf("slice %s is still readable 5 s after its deletion succeeded", id)
		}
	}
}

type psRun struct {
	runView
	Proof         *bool             `json:"proof"`
	PatternReason string            `json:"pattern_reason"`
	Parameters    map[string]string `json:"parameters"`
	SourceRun     *string           `json:"source_run"`
}

func (p *psRig) psGetRun(id string) psRun {
	p.t.Helper()
	r := p.do("GET", apiPath("/runs/"+id), "")
	if r.Code != 200 {
		p.t.Fatalf("getRun %s = %d: %s", id, r.Code, r.Body.String())
	}
	var v psRun
	if err := json.Unmarshal(r.Body.Bytes(), &v); err != nil {
		p.t.Fatal(err)
	}
	return v
}

// psStart starts a run of a slice into a project; pattern may be "".
func (p *psRig) psStart(slice, project, pattern string) reply {
	body := `{"project":"` + project + `"`
	if pattern != "" {
		body += `,"pattern":"` + pattern + `"`
	}
	return p.do("POST", spURL(slice, "/runs"), body+`}`)
}

// psStartProof starts a proof of the proof slice, which must be accepted, and returns the run ID.
func (p *psRig) psStartProof(slice, project string) string {
	p.t.Helper()
	return p.opOf(p.psStart(slice, project, ""), "run").Target
}

// psEnd lets a run end whatever its outcome (a proof the rig cannot finish is cancelled after 4 s)
// and returns it; it fails after 20 s.
func (p *psRig) psEnd(run string) psRun {
	p.t.Helper()
	start, cancelled := time.Now(), false
	for ; ; time.Sleep(5 * time.Millisecond) {
		v := p.psGetRun(run)
		if v.Status == "ended" {
			return v
		}
		if !cancelled && time.Since(start) > 4*time.Second {
			p.do("POST", apiPath("/operations/"+v.Operation+"/cancel"), "")
			cancelled = true
		}
		if time.Since(start) > 20*time.Second {
			p.t.Fatalf("run %s is %q after 20 s", run, v.Status)
		}
	}
}

// psInFlight posts a request that makes an operation which the armed lint then holds, and returns
// once the operation is running and held. The request must be answered 202 (the part checks run
// in the operation, not in the call).
func (p *psRig) psInFlight(entered <-chan struct{}, method, path, body string) {
	p.t.Helper()
	got := make(chan reply, 1)
	go func() { got <- p.psAs(clientToken, method, path, body) }()
	select {
	case r := <-got:
		if r.Code != http.StatusAccepted {
			p.t.Fatalf("%s %s = %d, want 202: %s", method, path, r.Code, r.Body.String())
		}
	case <-time.After(3 * time.Second):
		p.t.Fatalf("%s %s did not answer in 3 s: it must be accepted at once and run its part checks as an operation", method, path)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		p.t.Fatalf("the operation of %s %s never ran a part check under the current method", method, path)
	}
}

// psTemplateBytes returns the bytes the store holds for each template output ("" nil: none).
func (p *psRig) psTemplateBytes(c psCaptured) map[string][]byte {
	out := map[string][]byte{}
	for _, k := range []string{"tests", "code", "witnesses"} {
		b, err := p.st.Artifact(c.Template[k])
		if err == nil {
			out[k] = b
		}
	}
	return out
}

// ---- makeProofSlice: the slice, its parts, description, history and proof_of ----

func TestPSMakeProofSliceMakesAnApprovedSliceFromTheCopy(t *testing.T) { // T027, FR-005, data model "Proof slice"
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	source := p.psGetSlice("S-1")

	op := p.psMakeProofSlice(id)
	if op.Status != "succeeded" {
		t.Fatalf("operation %+v (error %+v), want succeeded", op, op.Error)
	}
	ps := op.Result["slice"]
	if !regexp.MustCompile(`^S-[0-9]+$`).MatchString(ps) || ps == "S-1" {
		t.Fatalf("result.slice = %q, want the new slice's ID (not the source S-1)", ps)
	}

	got := p.psGetSlice(ps)
	if got.State != "approved" {
		t.Errorf("state %q, want approved", got.State)
	}
	if got.Description != "Proof of "+id {
		t.Errorf("description %q, want %q", got.Description, "Proof of "+id)
	}
	if v, ok := got.str("proof_of"); !ok || v != id {
		t.Errorf("proof_of = %q (present %v), want %q", v, ok, id)
	}
	if _, ok := source.raw["proof_of"]; ok {
		t.Errorf("the source slice has proof_of: %s", source.raw["proof_of"])
	}

	// The parts equal the captured pattern's copy, and so the source slice's approved parts.
	copyParts := p.psGetCaptured(id).Parts
	for _, part := range []string{"goals", "rules", "contract"} {
		if got.Parts[part] == "" || got.Parts[part] != copyParts[part] || got.Parts[part] != source.Parts[part] {
			t.Errorf("part %s: proof slice %q, copy %q, source %q; want all equal", part, got.Parts[part], copyParts[part], source.Parts[part])
		}
	}
	same(t, "goals", got.Goals, source.Goals)
	same(t, "rules", got.Rules, source.Rules)
	same(t, "terms", got.Terms, source.Terms)
	same(t, "examples", got.Examples, source.Examples)
	same(t, "contract", got.Contract, source.Contract)

	// Approved on every part, each approval on the part's hash, every check passed.
	if len(got.Approvals) != 3 {
		t.Errorf("%d approvals, want 3: %+v", len(got.Approvals), got.Approvals)
	}
	for _, a := range got.Approvals {
		if a.ContentHash != got.Parts[a.Part] {
			t.Errorf("approval of %s is on %q, the part is %q", a.Part, a.ContentHash, got.Parts[a.Part])
		}
	}
	for _, c := range got.Checks {
		if !c.Passed {
			t.Errorf("check %s of %s failed on an approved proof slice: %s", c.Check, c.Part, c.Reason)
		}
	}

	// History: made-for-proof (no model, its parts), then one approved per part.
	if k := got.kinds(); len(k) != 4 || k[0] != "made-for-proof" || k[1] != "approved" || k[2] != "approved" || k[3] != "approved" {
		t.Fatalf("history %v, want [made-for-proof approved approved approved]", k)
	}
	first := got.History[0]
	if first.Model != "" || first.AgentDigest != "" {
		t.Errorf("made-for-proof names a model %q or agent %q; the engine made it with no model", first.Model, first.AgentDigest)
	}
	if want := []string{"contract", "goals", "rules"}; !sameSet(first.Refs, want) {
		t.Errorf("made-for-proof refs %v, want its parts %v", first.Refs, want)
	}
	var approved []string
	for _, h := range got.History[1:] {
		if len(h.Refs) != 1 {
			t.Errorf("approved entry refs %v, want the one part", h.Refs)
			continue
		}
		approved = append(approved, h.Refs[0])
	}
	if want := []string{"contract", "goals", "rules"}; !sameSet(approved, want) {
		t.Errorf("approved entries name %v, want each of %v once", approved, want)
	}

	// listSlices shows proof_of, and the captured pattern names its proof slice.
	ids, proofOf := p.psListed()
	if proofOf[ps] != id || len(proofOf) != 1 {
		t.Errorf("listSlices proof_of = %v over %v, want only %s: %s", proofOf, ids, ps, id)
	}
	if c := p.psGetCaptured(id); c.ProofSlice == nil || *c.ProofSlice != ps {
		t.Errorf("proof_slice = %v, want %s", c.ProofSlice, ps)
	}
	// The call made no model call and no other work.
	p.sp.noWork(t)
	if n := p.sp.analyst.calls(); n != 1 {
		t.Errorf("the analyst was called %d times; only the source slice's creation (1) may call it", n)
	}
}

func sameSet(got, want []string) bool {
	g := goslices.Clone(got)
	goslices.Sort(g)
	w := goslices.Clone(want)
	goslices.Sort(w)
	return goslices.Equal(g, w)
}

func TestPSAPartThatFailsACheckFailsTheOperationAndMakesNoSlice(t *testing.T) { // T027, PartFailsChecks
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	p.lint.setFail(true)

	op := p.psMakeProofSlice(id)
	if op.Status != "failed" || op.Error == nil || op.Error.Code != "PartFailsChecks" {
		t.Fatalf("operation %+v (error %+v), want failed with PartFailsChecks", op, op.Error)
	}
	if len(op.Error.Failures) == 0 {
		t.Errorf("PartFailsChecks lists no failures")
	}
	if ids, _ := p.psListed(); len(ids) != 1 || ids[0] != "S-1" {
		t.Errorf("slices %v after the failed operation, want only S-1 (no slice made)", ids)
	}
	if c := p.psGetCaptured(id); c.ProofSlice != nil {
		t.Errorf("proof_slice = %s after a failed operation, want none", *c.ProofSlice)
	}

	// Once the part passes, the next ask makes the slice.
	p.lint.setFail(false)
	ps := p.psProofSliceOK(id)
	if got := p.psGetSlice(ps); got.State != "approved" {
		t.Errorf("state %q after the second ask, want approved", got.State)
	}
	if ids, _ := p.psListed(); len(ids) != 2 {
		t.Errorf("slices %v, want S-1 and the proof slice", ids)
	}
}

func TestPSCreateSliceNeverReturnsTheProofSlice(t *testing.T) { // T027, "createSlice never reuses it"
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	ps := p.psProofSliceOK(id)

	for name, description := range map[string]string{
		"the source's description": "Let me keep notes.",
		"the proof slice's own":    "Proof of " + id,
	} {
		t.Run(name, func(t *testing.T) {
			r := p.do("POST", apiPath("/slices"), `{"description":`+jsonOf(t, description)+`}`)
			op := r.operation(t, http.StatusAccepted)
			got := p.psSettle(op.Id)
			if got.Status != "succeeded" {
				t.Fatalf("createSlice: %+v", got)
			}
			// createSlice answers a new slice as `slice` or an approved one it reuses as `reused_slice`.
			given := got.Result["slice"]
			if given == "" {
				given = got.Result["reused_slice"]
			}
			if given == ps {
				t.Errorf("createSlice returned the proof slice %s", ps)
			}
			if s := p.psGetSlice(given); s.raw["proof_of"] != nil {
				t.Errorf("the slice createSlice returned has proof_of %s", s.raw["proof_of"])
			}
		})
	}
	// The proof slice is as it was.
	if k := p.psGetSlice(ps).kinds(); len(k) != 4 {
		t.Errorf("proof slice history %v after createSlice, want it unchanged", k)
	}
}

func TestPSAskedAgainGivesTheSameSlice(t *testing.T) { // T027, "asked again it gives the same slice"
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	ps := p.psProofSliceOK(id)
	before := p.psGetSlice(ps)

	for i := range 2 {
		if again := p.psProofSliceOK(id); again != ps {
			t.Fatalf("ask %d gave %s, want the same slice %s", i+2, again, ps)
		}
	}
	after := p.psGetSlice(ps)
	same(t, "history", after.History, before.History)
	same(t, "approvals", after.Approvals, before.Approvals)
	if ids, _ := p.psListed(); len(ids) != 2 {
		t.Errorf("slices %v, want S-1 and %s only", ids, ps)
	}
}

func TestPSAfterAMethodChangeItChecksAndApprovesThePartsAgain(t *testing.T) { // T027, data model "Proof slice"
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	ps := p.psProofSliceOK(id)

	// The engine restarts under method B, whose contract lint rejects the contract: the read
	// re-checks the stored slice and withdraws the part's approval.
	p.lint.setFail(true)
	p.psReboot(cmMethodB)
	stale := p.psGetSlice(ps)
	if stale.State == "approved" {
		t.Fatalf("proof slice is approved under B although its contract fails the lint")
	}
	withdrawn := map[string]bool{}
	for _, h := range stale.History {
		if h.Kind == "withdrawn" {
			for _, r := range h.Refs {
				withdrawn[r] = true
			}
		}
	}
	if len(withdrawn) == 0 {
		t.Fatalf("history %v holds no withdrawn entry: the method change must withdraw the failing part's approval", stale.kinds())
	}

	// Asked while the part still fails: PartFailsChecks, and the slice is as it is.
	op := p.psMakeProofSlice(id)
	if op.Status != "failed" || op.Error == nil || op.Error.Code != "PartFailsChecks" {
		t.Fatalf("operation %+v (error %+v), want failed with PartFailsChecks", op, op.Error)
	}
	still := p.psGetSlice(ps)
	same(t, "history after the refused ask", still.History, stale.History)
	if still.State == "approved" {
		t.Errorf("state approved after a refused ask")
	}

	// Asked when it passes: the same slice, approved again, one approved entry per withdrawn part.
	p.lint.setFail(false)
	if again := p.psProofSliceOK(id); again != ps {
		t.Fatalf("gave %s, want the same slice %s", again, ps)
	}
	got := p.psGetSlice(ps)
	if got.State != "approved" {
		t.Errorf("state %q, want approved again", got.State)
	}
	if m, _ := got.str("checks_method"); m != cmMethodB {
		t.Errorf("checks_method %q, want %q", m, cmMethodB)
	}
	if len(got.Approvals) != 3 {
		t.Errorf("%d approvals, want 3", len(got.Approvals))
	}
	added := got.History[len(stale.History):]
	var refs []string
	for _, h := range added {
		if h.Kind != "approved" || len(h.Refs) != 1 {
			t.Errorf("entry %+v added by the second ask, want only approved entries of one part each", h)
			continue
		}
		refs = append(refs, h.Refs[0])
	}
	var want []string
	for r := range withdrawn {
		want = append(want, r)
	}
	if !sameSet(refs, want) {
		t.Errorf("approved entries added for %v, want one for each withdrawn part %v", refs, want)
	}
}

func TestPSAfterTheProofSliceIsErasedItMakesANewOneAlsoAfterTheSourceIsErased(t *testing.T) { // T027
	p := newPSRig(t)
	id, run := p.psCaptureOK("notes", "notes-app")
	copyParts := p.psGetCaptured(id).Parts
	first := p.psProofSliceOK(id)

	p.psDeleteSlice(first)
	if c := p.psGetCaptured(id); c.ProofSlice != nil {
		t.Errorf("proof_slice = %s after its slice was erased, want none", *c.ProofSlice)
	}
	second := p.psProofSliceOK(id)
	if second == first {
		t.Errorf("a new proof slice has the erased one's ID %s; IDs are never reused", first)
	}
	if got := p.psGetSlice(second); got.State != "approved" || got.Parts["contract"] != copyParts["contract"] {
		t.Errorf("new proof slice: state %q, contract part %q, want approved on the copy", got.State, got.Parts["contract"])
	}

	// Now the source slice and its runs are erased too: the copy is all there is.
	p.psDeleteSlice(second)
	p.psDeleteSlice("S-1")
	p.do("GET", apiPath("/runs/"+run), "").problem(t, 404, "NotFound")
	third := p.psProofSliceOK(id)
	got := p.psGetSlice(third)
	if third == second || third == first || got.State != "approved" {
		t.Errorf("after the source's erasure: slice %s (state %q), want a new approved one (earlier %s, %s)", third, got.State, first, second)
	}
	for _, part := range []string{"goals", "rules", "contract"} {
		if got.Parts[part] != copyParts[part] {
			t.Errorf("part %s %q, want the copy's %q", part, got.Parts[part], copyParts[part])
		}
	}
	if v, _ := got.str("proof_of"); v != id {
		t.Errorf("proof_of %q, want %q", v, id)
	}
	c := p.psGetCaptured(id)
	if c.Source.Run != run || c.Source.Slice != "S-1" {
		t.Errorf("source = %+v, want it kept naming %s and S-1", c.Source, run)
	}
	if c.ProofSlice == nil || *c.ProofSlice != third {
		t.Errorf("proof_slice = %v, want %s", c.ProofSlice, third)
	}
}

func TestPSAProofSliceAskedWhileAnotherOperationRunsIsBusy(t *testing.T) { // T027, CapturedPatternBusy
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	entered, release := p.lint.arm()
	t.Cleanup(release)
	p.psInFlight(entered, "POST", "/captured-patterns/"+id+"/proof-slice", "")

	p.psAs(clientToken, "POST", "/captured-patterns/"+id+"/proof-slice", "").problem(t, 409, "CapturedPatternBusy")

	release()
	// The first operation is not harmed by the refused one: it makes the slice, and a later ask gives it.
	var ps string
	for end := time.Now().Add(10 * time.Second); ps == "" && time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if c := p.psGetCaptured(id); c.ProofSlice != nil {
			ps = *c.ProofSlice
		}
	}
	if ps == "" {
		t.Fatal("no proof slice 10 s after the held operation was released")
	}
	if ids, _ := p.psListed(); len(ids) != 2 {
		t.Errorf("slices %v, want S-1 and one proof slice", ids)
	}
	if again := p.psProofSliceOK(id); again != ps {
		t.Errorf("a later ask gave %s, want %s", again, ps)
	}
}

// ---- startRun of the proof slice ----

func TestPSStartRunOfTheProofSliceIsAProofUnderTheCapturedPattern(t *testing.T) { // T027, FR-005
	p := newPSRig(t)
	id, source := p.psCaptureOK("notes", "notes-app")
	c := p.psGetCaptured(id)
	ps := p.psProofSliceOK(id)
	p.psRegister(ps)

	run := p.psStartProof(ps, "proof-a")
	v := p.psGetRun(run) // readable as soon as startRun answers
	if v.Proof == nil || !*v.Proof {
		t.Errorf("run %s has proof %v, want true", run, v.Proof)
	}
	if v.Slice != ps {
		t.Errorf("slice %q, want the proof slice %s", v.Slice, ps)
	}
	if v.Pattern != id+"@1.0.0" {
		t.Errorf("pattern %q, want the captured pattern %s@1.0.0 whatever its state (%s)", v.Pattern, id, c.State)
	}
	if v.PatternReason != "chosen" {
		t.Errorf("pattern_reason %q, want chosen", v.PatternReason)
	}
	if v.SourceRun == nil || *v.SourceRun != source {
		t.Errorf("source_run %v, want %s", v.SourceRun, source)
	}
	same(t, "parameters", v.Parameters, c.values())

	p.psEnd(run) // a running run's events always have a next page; read them once it has ended
	evs := p.rawEvents(run)
	if len(evs) == 0 || evs[0]["kind"] != "RunStarted" || evs[0]["proof"] != true || evs[0]["pattern"] != id+"@1.0.0" ||
		evs[0]["pattern_reason"] != "chosen" || evs[0]["source_run"] != source {
		t.Errorf("first event %v, want RunStarted with proof true, pattern %s@1.0.0, pattern_reason chosen, source_run %s", evs, id, source)
	}
	if evs[0]["part_hashes"] == nil {
		t.Errorf("RunStarted of a proof records no part_hashes")
	}

	// An ordinary run of the source slice is no proof.
	if v := p.psGetRun(source); v.Proof != nil && *v.Proof {
		t.Errorf("the source run %s has proof true", source)
	}
}

func TestPSStartRunOfTheProofSliceRefusesAnotherPatternOrAChangedPart(t *testing.T) { // T027, PatternDoesNotFit
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	other, _ := p.psCaptureOK("other", "notes-app-2")
	ps := p.psProofSliceOK(id)
	p.psRegister(ps)
	runsBefore := len(p.listRuns("").Items)

	// The base pattern, and another captured pattern, are not the one a proof proves.
	p.psStart(ps, "proof-b", "api-feature-go").problem(t, 409, "PatternDoesNotFit")
	p.psStart(ps, "proof-b", other).problem(t, 409, "PatternDoesNotFit")
	// A pattern naming no pattern.
	p.psStart(ps, "proof-b", "no-such-pattern").problem(t, 404, "NotFound")

	// A part changed and approved again no longer equals the copy.
	sl, err := p.sp.svc.Get(ps)
	if err != nil {
		t.Fatal(err)
	}
	goals := sl.Goals
	goals[0].Text = goals[0].Text + " and keep it for good"
	changed, err := p.sp.svc.ReplaceGoals(t.Context(), ps, goals)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.sp.svc.Approve(t.Context(), ps, slices.PartGoals, changed.Parts.Goals); err != nil {
		t.Fatal(err)
	}
	if got := p.psGetSlice(ps); got.State != "approved" {
		t.Fatalf("the changed proof slice is %q, want approved again (the test needs it approved)", got.State)
	}
	p.psStart(ps, "proof-b", "").problem(t, 409, "PatternDoesNotFit")
	p.psStart(ps, "proof-b", id).problem(t, 409, "PatternDoesNotFit")

	if n := len(p.listRuns("").Items); n != runsBefore {
		t.Errorf("%d runs, want %d: a refused start makes no run", n, runsBefore)
	}
}

// ---- Busy ----

func TestPSAProofStartAndAProofReplayWhileAnotherOperationRunsAreBusy(t *testing.T) { // T027
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	ps := p.psProofSliceOK(id)
	p.psRegister(ps)
	proof := p.psStartProof(ps, "proof-c")
	p.psEnd(proof)

	// The engine restarts under method B: the proof slice is stale, so asking for it again checks
	// its parts, and the held lint keeps that operation running.
	p.psReboot(cmMethodB)
	entered, release := p.lint.arm()
	t.Cleanup(release)
	p.psInFlight(entered, "POST", "/captured-patterns/"+id+"/proof-slice", "")
	runs := len(p.listRuns("").Items)

	// The captured pattern is proposed, so a proof's start answers Busy before anything that
	// concerns the state: not CapturedPatternNotApproved, not ProofRequired.
	p.psStart(ps, "proof-d", "").problem(t, 409, "CapturedPatternBusy")
	p.psStart(ps, "proof-d", id).problem(t, 409, "CapturedPatternBusy")
	// A proof run's replay follows the same rule, and is never ProofRequired.
	p.psAs(clientToken, "POST", "/runs/"+proof+"/replay", `{"project":"proof-e"}`).problem(t, 409, "CapturedPatternBusy")
	// A start of another slice that names the captured pattern never answers Busy: it judges the
	// pattern by its recorded state.
	p.psStart("S-1", "notes-named", id).problem(t, 409, "CapturedPatternNotApproved")
	if n := len(p.listRuns("").Items); n != runs {
		t.Errorf("%d runs, want %d: a refused start or replay makes no run", n, runs)
	}

	// When the operation has ended, a proof starts again. Until the released operation ends, an
	// ask still answers CapturedPatternBusy, so wait for that end first (as above, at most 10 s).
	release()
	path := "/captured-patterns/" + id + "/proof-slice"
	r := p.psAs(clientToken, "POST", path, "")
	for end := time.Now().Add(10 * time.Second); r.Code == http.StatusConflict && time.Now().Before(end); {
		r.problem(t, 409, "CapturedPatternBusy")
		time.Sleep(10 * time.Millisecond)
		r = p.psAs(clientToken, "POST", path, "")
	}
	if op := p.psAccepted(r, "make-proof-slice", id); op.Status != "succeeded" || op.Result["slice"] != ps {
		t.Fatalf("makeProofSlice after the release: %+v (error %+v), want succeeded giving %s", op, op.Error, ps)
	}
	p.psEnd(p.psStartProof(ps, "proof-f"))
}

// ---- replay of a proof run ----

func TestPSAReplayOfAProofRunIsANewProofWithNoReuse(t *testing.T) { // T027, "a replay of a proof run is a new proof"
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	c := p.psGetCaptured(id)
	ps := p.psProofSliceOK(id)
	p.psRegister(ps)
	first := p.psStartProof(ps, "proof-g")
	p.psEnd(first)

	// The captured pattern is proposed, not usable: a replay is a proof all the same.
	replay := p.opOf(p.psAs(clientToken, "POST", "/runs/"+first+"/replay", `{"project":"proof-h"}`), "run").Target
	v := p.psGetRun(replay)
	if v.Proof == nil || !*v.Proof || v.ReplayOf == nil || *v.ReplayOf != first || v.Slice != ps {
		t.Errorf("replay %+v, want a proof run of %s that replays %s", v, ps, first)
	}
	if v.Pattern != id+"@1.0.0" || v.PatternReason != "chosen" {
		t.Errorf("replay pattern %q reason %q, want %s@1.0.0 chosen", v.Pattern, v.PatternReason, id)
	}
	same(t, "parameters", v.Parameters, c.values())
	p.psEnd(replay)
	for _, e := range p.rawEvents(replay) {
		if e["kind"] == "ResultReused" {
			t.Errorf("the replay of a proof run reused a result: %v; a proof reuses no earlier result", e)
		}
	}
	if first := p.rawEvents(replay); len(first) == 0 || first[0]["proof"] != true {
		t.Errorf("the replay's RunStarted lacks proof true: %v", first)
	}

	// Both proofs are recorded, newest last.
	got := p.psGetCaptured(id)
	if len(got.Proofs) != 2 || got.Proofs[0].Run != first || got.Proofs[1].Run != replay {
		t.Errorf("proofs %+v, want [%s %s]", got.Proofs, first, replay)
	}
	for _, pr := range got.Proofs {
		if pr.Outcome == "running" {
			t.Errorf("proof of %s is running after its run ended", pr.Run)
		}
	}
}

// ---- deleteSlice of the proof slice ----

func TestPSDeleteSliceOfTheProofSliceKeepsTheProofsAndTheTemplate(t *testing.T) { // T027
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	before := p.psGetCaptured(id)
	bytesBefore := p.psTemplateBytes(before)
	for _, k := range []string{"tests", "code"} {
		if len(bytesBefore[k]) == 0 {
			t.Fatalf("the store holds no template artifact for %s (%s) after the capture", k, before.Template[k])
		}
	}
	ps := p.psProofSliceOK(id)
	p.psRegister(ps)
	run := p.psStartProof(ps, "proof-i")
	p.psEnd(run)

	// Delete right after the run ended, with no read of the captured pattern in between: the
	// deletion completes the proof first, so that it is kept.
	p.psDeleteSlice(ps)

	p.do("GET", apiPath("/runs/"+run), "").problem(t, 404, "NotFound")
	got := p.psGetCaptured(id)
	if got.ProofSlice != nil {
		t.Errorf("proof_slice = %s after its slice was deleted, want none", *got.ProofSlice)
	}
	if len(got.Proofs) != 1 || got.Proofs[0].Run != run {
		t.Fatalf("proofs %+v, want the one proof of %s kept", got.Proofs, run)
	}
	if o := got.Proofs[0].Outcome; o != "passed" && o != "failed" {
		t.Errorf("proof outcome %q, want it completed (passed or failed) before the run was erased", o)
	}
	if got.Proofs[0].Method == "" {
		t.Errorf("the kept proof names no method")
	}
	same(t, "proof parameters", got.Proofs[0].Parameters, before.values())
	same(t, "template", got.Template, before.Template)
	bytesAfter := p.psTemplateBytes(got)
	for k, b := range bytesBefore {
		if string(bytesAfter[k]) != string(b) {
			t.Errorf("template artifact %s changed or is gone after the proof slice was deleted", k)
		}
	}
	if ids, _ := p.psListed(); len(ids) != 1 {
		t.Errorf("slices %v, want only S-1", ids)
	}
}

// ---- tokens, unknown IDs ----

func TestPSMakeProofSliceWithAWorkerOrPageTokenIs403AndMakesNothing(t *testing.T) { // T027
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	for name, tok := range map[string]string{"worker": workerToken, "page": pageToken, "step worker": stepToken("S-1", "tests")} {
		t.Run(name, func(t *testing.T) {
			p.psAs(tok, "POST", "/captured-patterns/"+id+"/proof-slice", "").problem(t, 403, "Forbidden")
		})
	}
	p.psAs("", "POST", "/captured-patterns/"+id+"/proof-slice", "").problem(t, 401, "Unauthorized")
	if ids, _ := p.psListed(); len(ids) != 1 {
		t.Errorf("slices %v, want only S-1: a refused call makes no slice", ids)
	}
	if c := p.psGetCaptured(id); c.ProofSlice != nil {
		t.Errorf("proof_slice = %s after refused calls", *c.ProofSlice)
	}
}

func TestPSMakeProofSliceOfAnUnknownCapturedPatternIs404(t *testing.T) { // T027, contract: every /{id} call
	p := newPSRig(t)
	p.psCaptureOK("notes", "notes-app")
	p.psAs(clientToken, "POST", "/captured-patterns/api-feature-go-nope/proof-slice", "").problem(t, 404, "NotFound")
	if ids, _ := p.psListed(); len(ids) != 1 {
		t.Errorf("slices %v, want only S-1", ids)
	}
}

// ---- capturing a proof run ----

func TestPSCapturingAProofRunIsRunNotCapturable(t *testing.T) { // T027, FR-001 and the data model's capture refusals
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "notes-app")
	ps := p.psProofSliceOK(id)
	p.psRegister(ps)
	run := p.psStartProof(ps, "proof-j")
	if v := p.psEnd(run); v.Outcome == nil || *v.Outcome != "done" {
		// RunNotDone comes before RunNotCapturable for a run that did not end done, so the case
		// needs a proof run that ended done, which the rig's durable service may not give (see the
		// header: whether it can run a `fill` step is for T006/T029 to settle).
		t.Skipf("the proof run ended %v in the rig; capturing a proof run is RunNotCapturable only for a done one", v.Outcome)
	}
	p.psAs(clientToken, "POST", "/captured-patterns", `{"run":"`+run+`","name":"again"}`).problem(t, 409, "RunNotCapturable")
	p.psAs(clientToken, "GET", "/captured-patterns/api-feature-go-again", "").problem(t, 404, "NotFound")
}
