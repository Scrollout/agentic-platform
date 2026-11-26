package api

// Feature 011, T013 (US2; FR-008 to FR-010, FR-006, FR-011; research R6; data model "Run (added
// fields)" and "Runs that continue"; contract "Engine API: /captured-patterns", startRun and
// replayRun): the pattern choice at startRun, through HTTP and the generated contract types, on the
// engine rig of captured_proof_test.go (the real run workflow, faked step workers and checker, no
// model). Written before the code (T015) by a test author; the code author must not change it
// (ADR 0029).
//
// How a later slice is made: through the slice API, from the source slice's proposal with `note`
// replaced by `task` and `notes` by `tasks` in every part (srSwap), created and approved part by part
// as a developer does; the analyst fake answers one proposal per createSlice. A pattern is made the
// way a client does: a base run of S-1, captureRun, a proof (proof slice and its run), approval.
//
// "makes no agent worker or gateway call" is observed the way the rigs observe worker calls: the
// fake step worker (fakeStepWorker.calls) records the step of every call, and the only worker calls
// a captured run may make are the two non-agent steps, contract-check and generate (as a proof does,
// captured_proof_test.go); a call for tests, code or review is an agent call, which is the only
// way a model or the gateway is reached. A captured run's accepted tests, code and witnesses are
// also compared with the framed hash of the template files filled by hand (srFilled).
//
// Edge cases checked: the choice at 202 time (readable at once, before the run progresses) and at
// the end; run and RunStarted agree; the base pattern when nothing, a proposed, a proven or an
// unfit approved captured pattern exists (and none of them is named in `not_used`); a named base
// pattern (`chosen`); a named usable captured pattern when a newer one also fits; the most recently
// approved one wins by approval order, not creation order, and a newer approved one that does not
// fit is skipped; a captured pattern with no parameter fits only the identical slice (parameters
// `{}`); a method change, a later failing proof, and a proof still running make an approved one
// unusable (skipped and named `needs-proof`; named: ProofRequired; a new passing proof makes it
// usable again); a fitting unusable one is named, an unfit unusable one is not; a named proposed or
// proven pattern is CapturedPatternNotApproved while its proof slice's own run starts; a named one
// that does not fit (first difference named; refused value named); an unnamed start refused only
// for a value uses the base pattern and names it `refused-value`, also when it is unusable; every
// refusal starts nothing (no run, no operation, no worker call); the order of the refusals T015
// reaches; resume and take-over keep the choice; replay keeps the recorded values with no new fit,
// is refused ProofRequired after a failing proof and after a method change; capturing a replay
// whose slice changed, a replay of a run with no part hashes, and a run built from a captured
// pattern; a filled check that fails hands the run over with no agent step; two slices that differ
// only in a goals-only value never share a filled result; the base pattern's own fit conditions.
//
// Left out, and why: a withdrawn captured pattern (T018); Busy at a proof's start (T027, T018);
// `not_used` order (the contract says "in the order judged", which is not specified further);
// which captured pattern wins when two were approved at the very same instant (not specified);
// erasure of the captured pattern before a replay (T018's and the erase cases); a restart in the
// middle of a start (operations_restart_test.go's seam); the CLI (T030); the real fill and project
// writes (T006/T014: the rig's project writer writes nothing).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
)

// ---- the rig ----

// srRig is the cpRig with grants for twelve slices and the helpers a later slice needs.
type srRig struct {
	*cpRig
	baseRef string // the base pattern's ref, from the first base run
}

func srGrants() map[string]WorkerGrant {
	var gs []map[string]WorkerGrant
	for i := 1; i <= 12; i++ {
		gs = append(gs, stepGrants(fmt.Sprintf("S-%d", i)))
	}
	return mergeGrants(gs...)
}

// newSR makes the rig: S-1 is the notes slice, approved; the analyst answers `later` in order to
// the createSlice calls that follow.
func newSR(t *testing.T, later ...slices.Proposal) *srRig {
	t.Helper()
	st, err := store.Open(t.TempDir(), spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	results := []spResult{{p: spNotes(t)}}
	for _, p := range later {
		results = append(results, spResult{p: p})
	}
	sp := newSP(t, results, st)
	sp.approveAfterSeed(t)
	x := &runsRig{t: t, sp: sp, st: st, projects: t.TempDir(), w: &fakeStepWorker{st: st, data: map[string][]byte{}}, c: &fakeStepChecker{}}
	c := &cpRig{runsRig: x}
	ctx, cancel := context.WithCancel(context.Background())
	x.r = newRig(t, func(cfg *Config) {
		cfg.Context = ctx
		cfg.Slices, cfg.Store = sp.svc, st
		cfg.WorkerGrants = srGrants()
		cfg.Projects = x.projects
		cfg.Model = "test-model"
		cfg.StepWorker, cfg.StepChecker = x.w, x.c
	})
	stopEngineAtEnd(t, x.r, cancel)
	registerAll(t, x.r, cpSourceS)
	x.w.set(c.work)
	x.c.set(c.check)
	return &srRig{cpRig: c}
}

// srProposal is the notes proposal with each edit applied to its JSON text.
func srProposal(t *testing.T, edits ...func(string) string) slices.Proposal {
	t.Helper()
	b, err := json.Marshal(spNotes(t))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, e := range edits {
		s = e(s)
	}
	var p slices.Proposal
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		t.Fatalf("the edited proposal is not a proposal: %v", err)
	}
	return p
}

func srReplace(old, with string) func(string) string {
	return func(s string) string { return strings.ReplaceAll(s, old, with) }
}

func srCapital(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

// srSwap replaces `note` and `notes` in every case form the proposal uses (note, Note, notes, Notes).
func srSwap(note, notes string) func(string) string {
	return func(s string) string {
		for _, r := range [][2]string{{"notes", notes}, {"Notes", srCapital(notes)}, {"note", note}, {"Note", srCapital(note)}} {
			s = strings.ReplaceAll(s, r[0], r[1])
		}
		return s
	}
}

// the later slices' proposals
func srTasks(t *testing.T) slices.Proposal { return srProposal(t, srSwap("task", "tasks")) }

// srDiffers also changes a rule's response: it differs from the source outside every parameter.
func srDiffers(t *testing.T) slices.Proposal {
	return srProposal(t, srSwap("task", "tasks"), srReplace("shall list tasks", "shall list every item"))
}

// srRefused has a value outside the name form (a space) where the source had `note`.
func srRefused(t *testing.T) slices.Proposal { return srProposal(t, srSwap("to do", "tasks")) }

// srBadContract fails the base pattern's own condition (the contract is not OpenAPI 3.1).
func srBadContract(t *testing.T) slices.Proposal {
	return srProposal(t, srSwap("task", "tasks"), srReplace("3.1.0", "3.0.3"))
}

// addSlice creates a slice through the API and, when approve, approves its three parts.
func (s *srRig) addSlice(description string, approve bool) string {
	s.t.Helper()
	op := s.opBody(s.do("POST", apiPath("/slices"), `{"description":"`+description+`"}`), 202)
	got := s.waitOp(op.Id)
	id := got.Result["slice"]
	if got.Status != "succeeded" || id == "" {
		s.t.Fatalf("createSlice %q ended %+v", description, got)
	}
	if !approve {
		return id
	}
	var sl struct {
		Parts map[string]string `json:"parts"`
	}
	r := s.do("GET", spURL(id, ""), "")
	if err := json.Unmarshal(r.Body.Bytes(), &sl); err != nil || r.Code != 200 {
		s.t.Fatalf("getSlice %s = %d: %s", id, r.Code, r.Body.String())
	}
	for _, part := range []string{"goals", "rules", "contract"} {
		a := s.opBody(s.do("POST", spURL(id, "/approvals"), `{"part":"`+part+`","content_hash":"`+sl.Parts[part]+`"}`), 202)
		if w := s.waitOp(a.Id); w.Status != "succeeded" {
			s.t.Fatalf("approving the %s of %s ended %+v", part, id, w)
		}
	}
	return id
}

// registerSteps registers the workers of the named steps (comma separated) of a slice.
func (s *srRig) registerSteps(slice, steps string) {
	s.t.Helper()
	for _, st := range strings.Split(steps, ",") {
		if w := register(s.r, stepToken(slice, st), endpointFor(st)); w.Code != 200 {
			s.t.Fatalf("register %s of %s: %d %s", st, slice, w.Code, w.Body.String())
		}
	}
}

func (s *srRig) start(slice, project, pattern string) reply {
	body := `{"project":"` + project + `"`
	if pattern != "" {
		body += `,"pattern":"` + pattern + `"`
	}
	return s.do("POST", spURL(slice, "/runs"), body+`}`)
}

// startRun starts a run, which must be accepted as an operation of kind run.
func (s *srRig) startRun(slice, project, pattern string) gen.Operation {
	s.t.Helper()
	op := s.opBody(s.start(slice, project, pattern), 202)
	if string(op.Kind) != "run" || !runIDRE.MatchString(op.Target) {
		s.t.Fatalf("startRun answered %+v, want an operation of kind run targeting RUN-<n>", op)
	}
	return op
}

// capture makes a base run of S-1 (the base pattern is named: a captured pattern may fit S-1 too)
// and captures it as a proposed captured pattern; it returns the captured pattern's ID.
func (s *srRig) capture(name string) string {
	s.t.Helper()
	op := s.startRun("S-1", "src-"+name, cpBase)
	run := s.waitEnded(op.Target, "done")
	if s.baseRef == "" {
		s.baseRef = run.Pattern
	}
	c := s.opBody(s.do("POST", apiPath("/captured-patterns"), `{"run":"`+run.ID+`","name":"`+name+`"}`), 202)
	if got := s.waitOp(c.Id); got.Status != "succeeded" {
		s.t.Fatalf("capture of %s ended %+v", run.ID, got)
	}
	return c.Target
}

func (s *srRig) captured(id string) gen.CapturedPattern {
	s.t.Helper()
	r := s.do("GET", apiPath("/captured-patterns/"+id), "")
	var cp gen.CapturedPattern
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &cp) != nil {
		s.t.Fatalf("getCapturedPattern %s = %d: %s", id, r.Code, r.Body.String())
	}
	return cp
}

// proofRun asks for the proof slice, registers the workers it needs (a fill step needs none) and
// starts the proof's run.
func (s *srRig) proofRun(id, project string) gen.Operation {
	s.t.Helper()
	op := s.opBody(s.do("POST", apiPath("/captured-patterns/"+id+"/proof-slice"), ""), 202)
	got := s.waitOp(op.Id)
	if got.Status != "succeeded" || got.Result["slice"] == "" {
		s.t.Fatalf("proof-slice of %s ended %+v", id, got)
	}
	s.registerSteps(got.Result["slice"], cpFillOnly)
	return s.startRun(got.Result["slice"], project, "")
}

// prove runs a proof that must pass and returns its run.
func (s *srRig) prove(id, project string) string {
	s.t.Helper()
	op := s.proofRun(id, project)
	s.waitEnded(op.Target, "done")
	return op.Target
}

func (s *srRig) approve(id string) {
	s.t.Helper()
	op := s.opBody(s.do("POST", apiPath("/captured-patterns/"+id+"/approval"), ""), 202)
	if got := s.waitOp(op.Id); got.Status != "succeeded" {
		s.t.Fatalf("approval of %s ended %+v", id, got)
	}
}

// approved captures, proves and approves a captured pattern with its proposed parameters.
func (s *srRig) approved(name string) string {
	s.t.Helper()
	id := s.capture(name)
	s.prove(id, "proof-"+name)
	s.approve(id)
	return id
}

// replaceParameters sends exactly these name/value pairs.
func (s *srRig) replaceParameters(id string, nv ...string) {
	s.t.Helper()
	var ps []gen.ParameterValueRequest
	for i := 0; i+1 < len(nv); i += 2 {
		ps = append(ps, gen.ParameterValueRequest{Name: nv[i], Value: nv[i+1]})
	}
	if ps == nil {
		ps = []gen.ParameterValueRequest{}
	}
	b, _ := json.Marshal(gen.ParametersRequest{Parameters: ps})
	op := s.opBody(s.do("PUT", apiPath("/captured-patterns/"+id+"/parameters"), string(b)), 202)
	if got := s.waitOp(op.Id); got.Status != "succeeded" {
		s.t.Fatalf("replaceParameters of %s ended %+v", id, got)
	}
}

// setProofMethod makes every recorded proof of the captured pattern one under method m, which is
// how a change of the engine's method looks to a captured pattern (as captured_proof_seams_test.go
// does for one proof).
func (s *srRig) setProofMethod(id, m string) {
	s.t.Helper()
	b, err := s.st.CapturedPattern(id)
	if err != nil {
		s.t.Fatal(err)
	}
	rec := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &rec); err != nil {
		s.t.Fatal(err)
	}
	var proofs []map[string]json.RawMessage
	if err := json.Unmarshal(rec["proofs"], &proofs); err != nil || len(proofs) == 0 {
		s.t.Fatalf("the record's proofs = %s (%v), want some", rec["proofs"], err)
	}
	for _, p := range proofs {
		p["method"] = json.RawMessage(`"` + m + `"`)
	}
	rec["proofs"], _ = json.Marshal(proofs)
	out, _ := json.Marshal(rec)
	if err := s.st.PutCapturedPattern(id, out, nil); err != nil {
		s.t.Fatal(err)
	}
}

// agentCalls are the calls the step worker received from call number `from` on for the three
// steps of an agent: a model or gateway call can only come through them.
func (s *srRig) agentCalls(from int) []string {
	s.w.mu.Lock()
	defer s.w.mu.Unlock()
	var out []string
	for _, n := range s.w.calls[from:] {
		if n == "tests" || n == "code" || n == "review" {
			out = append(out, n)
		}
	}
	return out
}

func (s *srRig) noAgentCalls(from int, what string) {
	s.t.Helper()
	if got := s.agentCalls(from); len(got) != 0 {
		s.t.Errorf("%s made agent worker calls %v, want none: a captured pattern's run makes no model or gateway call", what, got)
	}
}

// runCount is how many runs the engine lists, opCount how many operations it holds.
func (s *srRig) runCount() int { return len(s.listRuns("").Items) }

func (s *srRig) opCount() int {
	n := 0
	for s.do("GET", apiPath("/operations/OP-"+fmt.Sprint(n+1)), "").Code == 200 {
		n++
	}
	return n
}

// refused starts a run that must be refused at once with status and code, and that must start
// nothing: no run, no operation, no worker call.
func (s *srRig) refused(slice, project, pattern string, status int, code gen.ErrorCode) gen.Problem {
	s.t.Helper()
	runs, ops, calls := s.runCount(), s.opCount(), s.w.callCount()
	p := s.start(slice, project, pattern).problem(s.t, status, code)
	if s.runCount() != runs || s.opCount() != ops || s.w.callCount() != calls {
		s.t.Errorf("the refused start of %s (pattern %q) started something: runs %d->%d, operations %d->%d, worker calls %d->%d",
			slice, pattern, runs, s.runCount(), ops, s.opCount(), calls, s.w.callCount())
	}
	return p
}

// srWant is the parameter values a later slice should give: each source value, replaced when repl
// has it.
func srWant(cp gen.CapturedPattern, repl map[string]string) map[string]string {
	out := map[string]string{}
	for _, p := range cp.Parameters {
		out[p.Name] = p.Value
		if v, ok := repl[p.Value]; ok {
			out[p.Name] = v
		}
	}
	return out
}

func srParam(t *testing.T, cp gen.CapturedPattern, value string) string {
	t.Helper()
	for _, p := range cp.Parameters {
		if p.Value == value {
			return p.Name
		}
	}
	t.Fatalf("the captured pattern has no parameter with the value %q: %+v", value, cp.Parameters)
	return ""
}

// wantChoice checks the pattern choice the Run and its RunStarted name.
func (s *srRig) wantChoice(run, pattern string, reason gen.PatternReason, params map[string]string, source string) gen.Run {
	s.t.Helper()
	v := s.getRunView(run)
	gotReason := gen.PatternReason("(none)")
	if v.PatternReason != nil {
		gotReason = *v.PatternReason
	}
	if v.Pattern != pattern || gotReason != reason {
		s.t.Errorf("run %s names pattern %s, reason %s; want %s, %s", run, v.Pattern, gotReason, pattern, reason)
	}
	got := map[string]string{}
	if v.Parameters != nil {
		for k, val := range *v.Parameters {
			got[k] = val
		}
	}
	if params == nil {
		if len(got) != 0 || v.SourceRun != nil {
			s.t.Errorf("run %s names parameters %v and source run %v, want none: it is not a captured pattern's run", run, got, v.SourceRun)
		}
	} else {
		if v.Parameters == nil || cpJSON(s.t, got) != cpJSON(s.t, params) {
			s.t.Errorf("run %s parameters = %v, want %v", run, v.Parameters, params)
		}
		if v.SourceRun == nil || *v.SourceRun != source {
			s.t.Errorf("run %s source_run = %v, want %s", run, v.SourceRun, source)
		}
	}
	if v.Proof != nil && bool(*v.Proof) {
		s.t.Errorf("run %s is marked a proof", run)
	}
	ev := s.rawEvents(run)[0]
	if ev["kind"] != "RunStarted" || ev["pattern"] != pattern || ev["pattern_reason"] != string(reason) {
		s.t.Errorf("RunStarted of %s = %v, want pattern %s and pattern_reason %s", run, ev, pattern, reason)
	}
	if params != nil {
		if b, _ := json.Marshal(ev["parameters"]); string(b) != cpJSON(s.t, params) || ev["source_run"] != source {
			s.t.Errorf("RunStarted of %s: parameters %s, source_run %v; want %s, %s", run, b, ev["source_run"], cpJSON(s.t, params), source)
		}
	}
	return v
}

type srUnused struct{ pattern, reason, parameter string }

// wantNotUsed checks `not_used` of the Run and of its RunStarted as a set (the contract fixes no
// more order than "the order judged").
func (s *srRig) wantNotUsed(run string, want ...srUnused) {
	s.t.Helper()
	v := s.getRunView(run)
	var got []string
	var list gen.NotUsed
	if v.NotUsed != nil {
		list = *v.NotUsed
	}
	for _, e := range list {
		param := ""
		if e.Parameter != nil {
			param = *e.Parameter
		}
		got = append(got, e.Pattern+"/"+string(e.Reason)+"/"+param)
		if e.Detail == "" {
			s.t.Errorf("not_used entry %+v of %s has no detail", e, run)
		}
	}
	var wants []string
	for _, w := range want {
		wants = append(wants, w.pattern+"/"+w.reason+"/"+w.parameter)
	}
	if !sameSet(got, wants) {
		s.t.Errorf("run %s not_used = %v, want %v", run, got, wants)
	}
	ev := s.rawEvents(run)[0]
	n := 0
	if l, ok := ev["not_used"].([]any); ok {
		n = len(l)
	}
	if n != len(want) {
		s.t.Errorf("RunStarted of %s names %d not_used entries (%v), want %d", run, n, ev["not_used"], len(want))
	}
}

// srFilled is the framed hash of one file, the template of the cpRig's source run with `note`
// replaced by `task` by hand.
func srFilled(path, content string) string {
	return canonical.FramedHash([]canonical.File{{Path: path, Content: []byte(content)}})
}

// acceptedHashes maps each step of a run to the one artifact its StepAccepted names.
func (s *srRig) acceptedHashes(run string) map[string]string {
	out := map[string]string{}
	for _, e := range s.allEvents(run) {
		if e.Kind == "StepAccepted" && len(e.Artifacts) == 1 {
			out[e.Step] = e.Artifacts[0]
		}
	}
	return out
}

func (s *srRig) kinds(run string, kind string) []string {
	var steps []string
	for _, e := range s.allEvents(run) {
		if e.Kind == kind {
			steps = append(steps, e.Step)
		}
	}
	return steps
}

var srTaskRepl = map[string]string{"note": "task", "notes": "tasks"}

// ---- the choice ----

// FR-008, FR-009, T013 first clause: without `pattern` the approved, usable, fitting captured
// pattern is used; the 202 is the run's operation; the Run (readable at once) and its RunStarted
// carry pattern id@version, `fits`, the values and the source run; the run makes no agent call and
// its accepted tests, code and witnesses are the filled template; no proof flag; no not_used.
func TestSRUnnamedStartUsesTheApprovedFittingCapturedPatternAndNoAgentStep(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.addSlice("Let me keep tasks.", true)
	s.registerSteps(later, cpFillOnly) // a fill step needs no worker
	id := s.approved("notes")
	cp := s.captured(id)
	srParam(t, cp, "note")
	srParam(t, cp, "notes")
	if cp.Template.Tests != srFilled("tests/note_test.go", "package tests // note\n") || cp.Template.Code != srFilled("internal/app/note.go", "package app // note\n") {
		t.Fatalf("setup: the template is %+v, want the framed source files the rig's worker wrote (checked by hand)", cp.Template)
	}
	want := srWant(cp, srTaskRepl)
	calls := s.w.callCount()

	op := s.startRun(later, "tasks-app", "")
	ref := id + "@" + string(cp.Version)
	s.wantChoice(op.Target, ref, gen.PatternReasonFits, want, cp.Source.Run) // at once, before the run has progressed
	s.wantNotUsed(op.Target)
	run := s.waitEnded(op.Target, "done")
	v := s.wantChoice(run.ID, ref, gen.PatternReasonFits, want, cp.Source.Run)
	if v.Slice != later || run.Slice != later {
		t.Errorf("the run is of slice %s, want %s", v.Slice, later)
	}
	s.noAgentCalls(calls, "the run")
	// contract-check and generate are each worked or reused (owner-approved 2026-11-26: the rig's
	// contract-check gives one artifact for every slice, so generate may reuse a result an earlier
	// run of the same pattern kept; R4 adds the values hash only to fill steps)
	if got, reused := s.w.callCount()-calls, len(s.kinds(run.ID, "ResultReused")); got+reused != 2 || got < 1 {
		t.Errorf("the run made %d worker calls and reused %d results, want 2 in all (contract-check and generate)", got, reused)
	}
	h := s.acceptedHashes(run.ID)
	if w := srFilled("tests/task_test.go", "package tests // task\n"); h["tests"] != w {
		t.Errorf("accepted tests %s, want the filled template's %s", h["tests"], w)
	}
	if w := srFilled("internal/app/task.go", "package app // task\n"); h["code"] != w {
		t.Errorf("accepted code %s, want the filled template's %s", h["code"], w)
	}
	if h["review"] != cpEmpty {
		t.Errorf("accepted witnesses %s, want the empty set's %s", h["review"], cpEmpty)
	}
}

// T013: with no usable fitting captured pattern the base pattern is used (`base`, no parameters,
// no source run); a captured pattern that is proposed, proven, or approved but does not fit is not
// named in not_used (only a fitting one that needs a proof, or one refused for a value, is).
func TestSRStartWithoutAUsableFittingCapturedPatternUsesTheBasePattern(t *testing.T) {
	s := newSR(t, srTasks(t), srDiffers(t))
	tasks := s.addSlice("Let me keep tasks.", true)
	differs := s.addSlice("Let me keep tasks and list every item.", true)
	registerAll(t, s.r, tasks)
	registerAll(t, s.r, differs)

	// no captured pattern at all
	calls := s.w.callCount()
	op := s.startRun(tasks, "tasks-one", "")
	s.baseRef = s.getRunView(op.Target).Pattern
	s.waitEnded(op.Target, "done")
	s.wantChoice(op.Target, s.baseRef, gen.PatternReasonBase, nil, "")
	s.wantNotUsed(op.Target)
	if got := s.agentCalls(calls); len(got) != 3 {
		t.Errorf("the base pattern's run made agent calls %v, want tests, code and review", got)
	}

	// a proposed and a proven (not approved) captured pattern that fit: not used, not named
	proposed := s.capture("proposed")
	proven := s.capture("proven")
	s.prove(proven, "proof-proven")
	if got := s.captured(proven).State; got != gen.CapturedPatternStateProven {
		t.Fatalf("setup: state %s, want proven", got)
	}
	op = s.startRun(tasks, "tasks-two", "")
	s.waitEnded(op.Target, "done")
	s.wantChoice(op.Target, s.baseRef, gen.PatternReasonBase, nil, "")
	s.wantNotUsed(op.Target)

	// approved, but the slice differs outside every parameter: base, and not named
	s.approve(proven)
	_ = proposed
	op = s.startRun(differs, "differs-one", "")
	s.waitEnded(op.Target, "done")
	s.wantChoice(op.Target, s.baseRef, gen.PatternReasonBase, nil, "")
	s.wantNotUsed(op.Target)
}

// T013: `pattern` naming the base pattern uses it (`chosen`) although a captured pattern fits;
// `pattern` naming a usable captured pattern uses that one (`chosen`, the values, the source run)
// even when a more recently approved one fits too.
func TestSRNamedBasePatternAndNamedUsableCapturedPatternAreChosen(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.addSlice("Let me keep tasks.", true)
	registerAll(t, s.r, later)
	older := s.approved("older")
	newer := s.approved("newer")
	cp := s.captured(older)
	want := srWant(cp, srTaskRepl)

	calls := s.w.callCount()
	op := s.startRun(later, "base-app", cpBase)
	s.waitEnded(op.Target, "done")
	s.wantChoice(op.Target, s.baseRef, gen.PatternReasonChosen, nil, "")
	if got := s.agentCalls(calls); len(got) != 3 {
		t.Errorf("the named base pattern's run made agent calls %v, want tests, code and review", got)
	}

	calls = s.w.callCount()
	op = s.startRun(later, "older-app", older)
	s.wantChoice(op.Target, older+"@"+string(cp.Version), gen.PatternReasonChosen, want, cp.Source.Run)
	s.waitEnded(op.Target, "done")
	s.noAgentCalls(calls, "the run of the named captured pattern")
	if s.captured(newer).State != gen.CapturedPatternStateApproved {
		t.Errorf("setup: the newer captured pattern is not approved")
	}
}

// FR-006: a `pattern` naming a proposed or a proven captured pattern is CapturedPatternNotApproved
// and starts nothing; its proof slice's own run still starts (and ends done).
func TestSRNamedProposedOrProvenCapturedPatternIsNotApprovedAndStartsNothing(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.addSlice("Let me keep tasks.", true)
	registerAll(t, s.r, later)
	id := s.capture("notes")

	p := s.refused(later, "tasks-app", id, 409, gen.ErrorCodeCapturedPatternNotApproved)
	if p.Detail == nil || !strings.Contains(*p.Detail, "proposed") {
		t.Errorf("the detail %v does not say the captured pattern is proposed", p.Detail)
	}
	s.prove(id, "proof-app") // its proof slice's own run is not refused
	if got := s.captured(id).State; got != gen.CapturedPatternStateProven {
		t.Fatalf("setup: state %s, want proven", got)
	}
	p = s.refused(later, "tasks-app", id, 409, gen.ErrorCodeCapturedPatternNotApproved)
	if p.Detail == nil || !strings.Contains(*p.Detail, "proven") {
		t.Errorf("the detail %v does not say the captured pattern is proven", p.Detail)
	}
	op := s.startRun(later, "tasks-two", "") // unnamed: the proven one is not used, the base pattern is
	s.wantChoice(op.Target, s.baseRef, gen.PatternReasonBase, nil, "")
	s.wantNotUsed(op.Target)
	s.waitEnded(op.Target, "done")
}

// FR-011: with the engine's method changed after approval, an approved captured pattern is skipped
// and named in not_used as needs-proof, a named one answers ProofRequired and starts nothing; a new
// passing proof makes it usable again.
func TestSRAMethodChangeSkipsTheCapturedPatternAndNamesItNeedsProof(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.addSlice("Let me keep tasks.", true)
	registerAll(t, s.r, later)
	id := s.approved("notes")
	cp := s.captured(id)
	s.setProofMethod(id, cmMethodB)
	if got := s.captured(id); got.Usable || got.State != gen.CapturedPatternStateApproved {
		t.Fatalf("setup: state %s usable %v, want approved and not usable", got.State, got.Usable)
	}

	s.refused(later, "tasks-app", id, 409, gen.ErrorCodeProofRequired)
	op := s.startRun(later, "tasks-app", "")
	s.baseRef = s.getRunView(op.Target).Pattern
	s.wantChoice(op.Target, s.baseRef, gen.PatternReasonBase, nil, "")
	s.wantNotUsed(op.Target, srUnused{pattern: id, reason: "needs-proof"})
	if strings.Contains(s.baseRef, id) {
		t.Errorf("the run uses %s: the captured pattern must be skipped", s.baseRef)
	}
	s.waitEnded(op.Target, "done")

	s.prove(id, "proof-again") // a new proof under the current method
	if got := s.captured(id); !got.Usable {
		t.Fatalf("setup: not usable after a passing proof: %+v", got)
	}
	calls := s.w.callCount()
	op = s.startRun(later, "tasks-two", "")
	s.wantChoice(op.Target, id+"@"+string(cp.Version), gen.PatternReasonFits, srWant(cp, srTaskRepl), cp.Source.Run)
	s.wantNotUsed(op.Target)
	s.waitEnded(op.Target, "done")
	s.noAgentCalls(calls, "the run after the new proof")
}

// FR-011: a later failing proof, and a proof still running, make an approved captured pattern
// unusable the same way (needs-proof when unnamed, ProofRequired when named).
func TestSRALaterFailingProofOrAProofStillRunningMakesTheCapturedPatternUnusable(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.addSlice("Let me keep tasks.", true)
	registerAll(t, s.r, later)
	id := s.approved("notes")
	cp := s.captured(id)

	// a failing proof: its run waits for the developer; cancelled, the latest proof stays failed
	s.set(func() { s.failCheck = "tests/TestsBuild" })
	bad := s.proofRun(id, "proof-bad")
	s.waitStatus(bad.Target, "waiting-for-developer")
	s.opBody(s.do("POST", apiPath("/operations/"+bad.Id+"/cancel"), ""), 202)
	s.waitEnded(bad.Target, "cancelled")
	s.set(func() { s.failCheck = "" })
	if got := s.captured(id); got.Usable || got.State != gen.CapturedPatternStateApproved {
		t.Fatalf("setup: state %s usable %v after a failing proof, want approved and not usable", got.State, got.Usable)
	}
	s.refused(later, "tasks-app", id, 409, gen.ErrorCodeProofRequired)
	op := s.startRun(later, "tasks-app", "")
	base := s.getRunView(op.Target).Pattern
	s.wantChoice(op.Target, base, gen.PatternReasonBase, nil, "")
	s.wantNotUsed(op.Target, srUnused{pattern: id, reason: "needs-proof"})
	s.waitEnded(op.Target, "done")

	// a proof still running: the latest proof has not passed (fail closed)
	gate := make(chan struct{})
	s.set(func() { s.gate = gate })
	running := s.proofRun(id, "proof-running")
	s.waitStatus(running.Target, "running")
	if got := s.captured(id); got.Usable {
		t.Errorf("the captured pattern is usable while its newest proof runs")
	}
	s.refused(later, "tasks-three", id, 409, gen.ErrorCodeProofRequired)
	// unnamed: the run of the proof is active, so the start is refused after the choice, as for
	// any start; the choice itself cannot be read from a run that was not started
	s.refused(later, "tasks-three", "", 409, gen.ErrorCodeRunActive)
	close(gate)
	s.waitEnded(running.Target, "done")
	op = s.startRun(later, "tasks-four", "")
	s.wantChoice(op.Target, id+"@"+string(cp.Version), gen.PatternReasonFits, srWant(cp, srTaskRepl), cp.Source.Run)
	s.waitEnded(op.Target, "done")
}

// R6: the most recently approved usable fitting captured pattern is used: by the time of its
// approval, not of its creation; an approved pattern that does not fit is skipped (and not named);
// a captured pattern with no parameter fits only the identical slice (parameters {}); a fitting
// one that needs a proof is skipped and named while an older usable one is used.
func TestSRTheMostRecentlyApprovedFittingUsablePatternWins(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.addSlice("Let me keep tasks.", true)
	registerAll(t, s.r, later)
	a, b, c := s.capture("alpha"), s.capture("beta"), s.capture("gamma")
	s.replaceParameters(c) // no parameter: only the identical slice fits
	for _, id := range []string{a, b, c} {
		s.prove(id, "proof-"+id)
	}
	for _, id := range []string{b, a, c} { // a was created first and approved after b; c last
		s.approve(id)
	}
	ca, cc := s.captured(a), s.captured(c)
	want := srWant(ca, srTaskRepl)

	op := s.startRun(later, "tasks-one", "")
	s.wantChoice(op.Target, a+"@"+string(ca.Version), gen.PatternReasonFits, want, ca.Source.Run) // not c (it does not fit) and not b (older approval)
	s.wantNotUsed(op.Target)
	s.waitEnded(op.Target, "done")

	// the identical slice (S-1) fits all three: the most recently approved, c, which has no parameter
	op = s.startRun("S-1", "again-app", "")
	s.wantChoice(op.Target, c+"@"+string(cc.Version), gen.PatternReasonFits, map[string]string{}, cc.Source.Run)
	if v := s.getRunView(op.Target); v.Parameters == nil {
		t.Errorf("a captured pattern with no parameter has an empty `parameters`, not an absent one")
	}
	s.waitEnded(op.Target, "done")

	// a needs a new proof (and fits): skipped and named; b is the most recent of the usable ones
	s.setProofMethod(a, cmMethodB)
	op = s.startRun(later, "tasks-two", "")
	cb := s.captured(b)
	s.wantChoice(op.Target, b+"@"+string(cb.Version), gen.PatternReasonFits, srWant(cb, srTaskRepl), cb.Source.Run)
	s.wantNotUsed(op.Target, srUnused{pattern: a, reason: "needs-proof"})
	s.waitEnded(op.Target, "done")

	// both need a proof: the base pattern, and both named; c (which does not fit) is not
	s.setProofMethod(b, cmMethodB)
	op = s.startRun(later, "tasks-three", "")
	s.wantChoice(op.Target, s.getRunView(op.Target).Pattern, gen.PatternReasonBase, nil, "")
	s.wantNotUsed(op.Target, srUnused{pattern: a, reason: "needs-proof"}, srUnused{pattern: b, reason: "needs-proof"})
	s.waitEnded(op.Target, "done")
}

// ---- fit ----

// T013: a named captured pattern that does not fit answers PatternDoesNotFit naming the first
// difference or the refused value, and starts nothing; without `pattern`, a pattern refused only
// for a value is named in the run with the parameter and why, and the base pattern is used, also
// when that pattern is unusable (refused-value, not needs-proof); a slice that differs elsewhere
// is not named.
func TestSRNamedCapturedPatternThatDoesNotFitIsPatternDoesNotFit(t *testing.T) {
	s := newSR(t, srDiffers(t), srRefused(t))
	differs := s.addSlice("Let me keep tasks and list every item.", true)
	refusedSlice := s.addSlice("Let me keep to dos.", true)
	registerAll(t, s.r, differs)
	registerAll(t, s.r, refusedSlice)
	id := s.approved("notes")
	cp := s.captured(id)
	noteName := srParam(t, cp, "note")

	p := s.refused(differs, "differs-app", id, 409, gen.ErrorCodePatternDoesNotFit)
	if p.Detail == nil || !strings.Contains(*p.Detail, "rules") {
		t.Errorf("the detail %v does not name the first place that differs (the rules part)", p.Detail)
	}
	p = s.refused(refusedSlice, "todo-app", id, 409, gen.ErrorCodePatternDoesNotFit)
	if p.Detail == nil || !strings.Contains(*p.Detail, noteName) {
		t.Errorf("the detail %v does not name the refused parameter %q", p.Detail, noteName)
	}

	// without `pattern`: refused only for a value -> base, named refused-value with the parameter
	op := s.startRun(refusedSlice, "todo-app", "")
	base := s.getRunView(op.Target).Pattern
	s.wantChoice(op.Target, base, gen.PatternReasonBase, nil, "")
	s.wantNotUsed(op.Target, srUnused{pattern: id, reason: "refused-value", parameter: noteName})
	s.waitEnded(op.Target, "done")

	// the same when the captured pattern is also unusable: the fit decides first
	s.setProofMethod(id, cmMethodB)
	op = s.startRun(refusedSlice, "todo-two", "")
	s.wantNotUsed(op.Target, srUnused{pattern: id, reason: "refused-value", parameter: noteName})
	s.waitEnded(op.Target, "done")

	// a slice that differs elsewhere is base and names nothing, usable or not
	op = s.startRun(differs, "differs-one", "")
	s.wantChoice(op.Target, base, gen.PatternReasonBase, nil, "")
	s.wantNotUsed(op.Target)
	s.waitEnded(op.Target, "done")
}

// T013, spec edge case: two slices that differ only in a value that occurs in the goals alone
// never reuse each other's filled results. The captured pattern holds a literal that occurs in the
// goals only; no step reads the goals, so only the parameters' values tell the two slices' fill
// steps apart. The control: the two slices' contract-check (a step that is not filled and reads the
// same inputs) does reuse the result, so the fill steps are not kept apart by the engine's rules
// for slices alone.
func TestSRTwoSlicesDifferingOnlyInAGoalsValueShareNoFilledResult(t *testing.T) {
	s := newSR(t,
		srProposal(t, srSwap("task", "tasks"), srReplace("Keep a ", "Add a ")),
		srProposal(t, srSwap("task", "tasks"), srReplace("Keep a ", "Make a ")))
	one := s.addSlice("Add tasks.", true)
	two := s.addSlice("Make tasks.", true)
	s.registerSteps(one, cpFillOnly)
	s.registerSteps(two, cpFillOnly)
	id := s.capture("goals")
	s.replaceParameters(id, "item", "note", "items", "notes", "opening", "Keep a")
	if cp := s.captured(id); len(cp.Parameters) != 3 {
		t.Fatalf("setup: parameters %+v, want the three sent", cp.Parameters)
	}
	s.prove(id, "proof-goals")
	s.approve(id)
	cp := s.captured(id)

	first := s.startRun(one, "add-app", "")
	s.wantChoice(first.Target, id+"@"+string(cp.Version), gen.PatternReasonFits,
		map[string]string{"item": "task", "items": "tasks", "opening": "Add a"}, cp.Source.Run)
	s.waitEnded(first.Target, "done")
	second := s.startRun(two, "make-app", "")
	s.wantChoice(second.Target, id+"@"+string(cp.Version), gen.PatternReasonFits,
		map[string]string{"item": "task", "items": "tasks", "opening": "Make a"}, cp.Source.Run)
	s.waitEnded(second.Target, "done")

	reused := s.kinds(second.Target, "ResultReused")
	for _, step := range reused {
		if step == "tests" || step == "code" || step == "review" {
			t.Errorf("the second slice's run reused the result of fill step %s: two slices that differ only in a value share no filled result", step)
		}
	}
	if !contains(reused, "contract-check") {
		t.Errorf("control: the second run reused %v, want contract-check among them (same inputs, not filled)", reused)
	}
}

// T013 / FR-010 data: a captured pattern with no parameter fits only the identical slice: a later
// slice that differs only in its names does not fit it.
func TestSRAPatternWithNoParameterDoesNotFitALaterSlice(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.addSlice("Let me keep tasks.", true)
	registerAll(t, s.r, later)
	id := s.capture("fixed")
	s.replaceParameters(id)
	s.prove(id, "proof-fixed")
	s.approve(id)
	s.refused(later, "tasks-app", id, 409, gen.ErrorCodePatternDoesNotFit)
	op := s.startRun(later, "tasks-app", "")
	s.wantChoice(op.Target, s.getRunView(op.Target).Pattern, gen.PatternReasonBase, nil, "")
	s.wantNotUsed(op.Target)
	s.waitEnded(op.Target, "done")
	op = s.startRun("S-1", "same-app", id) // named, the identical slice: fits, chosen, parameters {}
	cp := s.captured(id)
	s.wantChoice(op.Target, id+"@"+string(cp.Version), gen.PatternReasonChosen, map[string]string{}, cp.Source.Run)
	s.waitEnded(op.Target, "done")
}

// ---- the order of the refusals ----

// Contract, startRun: InvalidRequest or InvalidProject, NotFound, PatternDoesNotFit (the base
// pattern's conditions), NotApproved, CapturedPatternNotApproved, ProofRequired, PatternDoesNotFit
// (a named captured pattern), WorkersNotReady, RunActive, ProjectExists. (Withdrawn and Busy come
// between NotApproved and CapturedPatternNotApproved; they are T018's and T027's.) Each case holds
// two refusals at once; the earlier must be the one answered. Each refusal starts nothing.
func TestSRRefusalsComeInTheContractsOrder(t *testing.T) {
	s := newSR(t,
		srTasks(t),       // unapproved: fits the base pattern
		srBadContract(t), // unapproved: fails the base pattern's conditions
		srDiffers(t),     // approved, differs, no workers
		srTasks(t),       // approved, fits, no workers
		srTasks(t),       // approved, fits, ready
		srProposal(t, srSwap("task", "tasks"), srReplace("shall list tasks", "shall list busy items"))) // approved, base run that waits
	unapproved := s.addSlice("Unapproved tasks.", false)
	bad := s.addSlice("A bad contract.", false)
	differs := s.addSlice("Let me keep tasks and list every item.", true)
	noWorkers := s.addSlice("Tasks without workers.", true)
	ready := s.addSlice("Let me keep tasks.", true)
	s.registerSteps(ready, cpFillOnly)
	busySlice := s.addSlice("A busy slice.", true)
	registerAll(t, s.r, busySlice)

	proposed := s.capture("proposed")
	usable := s.approved("usable")
	stale := s.approved("stale")
	s.setProofMethod(stale, cmMethodB)

	// InvalidProject and InvalidRequest come before NotFound
	s.start(unapproved, "Bad Name", "no-such").problem(t, 400, gen.ErrorCodeInvalidProject)
	s.do("POST", spURL(unapproved, "/runs"), `{"project":"app","pattern":"Bad Id"}`).problem(t, 400, gen.ErrorCodeInvalidRequest)
	// NotFound (a pattern naming no pattern) comes before NotApproved
	s.refused(unapproved, "app", "no-such", 404, gen.ErrorCodeNotFound)
	// the base pattern's conditions come before NotApproved and before every captured pattern's
	p := s.refused(bad, "app", usable, 409, gen.ErrorCodePatternDoesNotFit)
	if p.Failures == nil || len(*p.Failures) == 0 {
		t.Errorf("PatternDoesNotFit for the base conditions lists the failing conditions in failures: %+v", p)
	}
	s.refused(bad, "app", "", 409, gen.ErrorCodePatternDoesNotFit)
	// NotApproved before CapturedPatternNotApproved, ProofRequired
	s.refused(unapproved, "app", proposed, 409, gen.ErrorCodeNotApproved)
	s.refused(unapproved, "app", stale, 409, gen.ErrorCodeNotApproved)
	// CapturedPatternNotApproved before ProofRequired-or-fit and before WorkersNotReady
	s.refused(differs, "app", proposed, 409, gen.ErrorCodeCapturedPatternNotApproved)
	s.refused(noWorkers, "app", proposed, 409, gen.ErrorCodeCapturedPatternNotApproved)
	// ProofRequired before the captured pattern's PatternDoesNotFit
	s.refused(differs, "app", stale, 409, gen.ErrorCodeProofRequired)
	// the captured pattern's PatternDoesNotFit before WorkersNotReady (and the slice has no workers)
	p = s.refused(differs, "app", usable, 409, gen.ErrorCodePatternDoesNotFit)
	if p.Failures != nil && len(*p.Failures) != 0 {
		t.Errorf("the captured pattern's PatternDoesNotFit lists base conditions: %+v", *p.Failures)
	}

	// WorkersNotReady (a step that is not a fill step has no worker) before RunActive
	s.set(func() { s.failCheck = "tests/TestsBuild" })
	busy := s.startRun(busySlice, "busy-app", cpBase) // new inputs: no result is reused, so the step runs and fails
	s.waitStatus(busy.Target, "waiting-for-developer")
	s.refused(noWorkers, "app", usable, 409, gen.ErrorCodeWorkersNotReady)
	// RunActive before ProjectExists
	s.refused(ready, "busy-app", usable, 409, gen.ErrorCodeRunActive)
	s.refused(ready, "src-usable", usable, 409, gen.ErrorCodeRunActive)
	s.opBody(s.do("POST", apiPath("/runs/"+busy.Target+"/hand-over"), ""), 202)
	s.waitEnded(busy.Target, "handed-over")
	s.set(func() { s.failCheck = "" })
	// ProjectExists, then the start
	s.refused(ready, "busy-app", usable, 409, gen.ErrorCodeProjectExists)
	s.refused(ready, "src-usable", "", 409, gen.ErrorCodeProjectExists)
	op := s.startRun(ready, "ready-app", usable)
	s.waitEnded(op.Target, "done")
}

// The base pattern's fit conditions still run: a slice that fails them is PatternDoesNotFit with
// failures, also when a usable captured pattern is named or would be chosen, and also when no part
// is approved (contract: "even when no part is approved").
// (Covered in TestSRRefusalsComeInTheContractsOrder with a named pattern and without.)

// T013: a `pattern` naming no pattern is NotFound and starts nothing; a name outside the pattern
// ID form is InvalidRequest (regression guards: they pass before T015).
func TestSRAPatternNamingNoPatternStartsNothing(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.addSlice("Let me keep tasks.", true)
	registerAll(t, s.r, later)
	s.refused(later, "tasks-app", "api-feature-go-nope", 404, gen.ErrorCodeNotFound)
	s.refused(later, "tasks-app", "no-such", 404, gen.ErrorCodeNotFound)
	s.do("POST", spURL(later, "/runs"), `{"project":"tasks-app","pattern":"UPPER CASE"}`).problem(t, 400, gen.ErrorCodeInvalidRequest)
	s.refused("S-99", "tasks-app", "", 404, gen.ErrorCodeNotFound)
}

// ---- runs that continue ----

// T013: resume and take-over of a captured run keep its pattern and parameters (and source run,
// reason and not_used); no agent step is started.
func TestSRResumeAndTakeOverOfACapturedRunKeepItsPatternAndParameters(t *testing.T) {
	s := newSR(t, srTasks(t), srProposal(t, srSwap("chore", "chores"))) // another value: no result of the first run is reused
	one := s.addSlice("Let me keep tasks.", true)
	two := s.addSlice("Let me keep chores.", true)
	s.registerSteps(one, cpFillOnly)
	s.registerSteps(two, cpFillOnly)
	id := s.approved("notes")
	cp := s.captured(id)
	ref, want := id+"@"+string(cp.Version), srWant(cp, srTaskRepl)
	calls := s.w.callCount()

	// resume: the step worker of generate is down, the run is interrupted, then continues
	s.set(func() { s.failWorker = "contract-check" })
	op := s.startRun(one, "resume-app", "")
	s.waitStatus(op.Target, "interrupted")
	s.wantChoice(op.Target, ref, gen.PatternReasonFits, want, cp.Source.Run)
	s.set(func() { s.failWorker = "" })
	s.opBody(s.do("POST", apiPath("/runs/"+op.Target+"/resume"), ""), 202)
	s.waitEnded(op.Target, "done")
	s.wantChoice(op.Target, ref, gen.PatternReasonFits, want, cp.Source.Run)
	s.wantNotUsed(op.Target)

	// take-over: a filled check fails, the run waits for the developer, who takes it over
	s.set(func() { s.failCheck = "tests/TestsBuild" })
	op = s.startRun(two, "takeover-app", "")
	s.waitStatus(op.Target, "waiting-for-developer")
	chores := srWant(cp, map[string]string{"note": "chore", "notes": "chores"})
	s.wantChoice(op.Target, ref, gen.PatternReasonFits, chores, cp.Source.Run)
	s.opBody(s.do("POST", apiPath("/runs/"+op.Target+"/hand-over"), ""), 202)
	s.waitEnded(op.Target, "handed-over")
	s.wantChoice(op.Target, ref, gen.PatternReasonFits, chores, cp.Source.Run)
	s.noAgentCalls(calls, "the resumed and the taken-over run")
}

// T013, data model "Runs that continue": a replay of a captured run uses the same captured pattern
// with the values the run recorded, with no new fit (here the slice has changed since, so a new
// fit would refuse it); it is refused ProofRequired, starting nothing, when the latest proof
// failed, and when the method changed; it is usable again after a new passing proof.
func TestSRReplayOfACapturedRunUsesTheSameCapturedPatternWhileUsable(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.addSlice("Let me keep tasks.", true)
	s.registerSteps(later, cpFillOnly)
	id := s.approved("notes")
	cp := s.captured(id)
	ref, want := id+"@"+string(cp.Version), srWant(cp, srTaskRepl)
	first := s.startRun(later, "tasks-app", "")
	s.waitEnded(first.Target, "done")
	calls := s.w.callCount()

	s.changeGoals(later) // the slice no longer equals what the run was filled from
	op := s.opBody(s.do("POST", apiPath("/runs/"+first.Target+"/replay"), `{"project":"replay-one"}`), 202)
	if string(op.Kind) != "run" || !runIDRE.MatchString(op.Target) || op.Target == first.Target {
		t.Fatalf("replayRun answered %+v, want an operation of kind run for a new run", op)
	}
	v := s.wantChoice(op.Target, ref, gen.PatternReasonChosen, want, cp.Source.Run)
	if v.ReplayOf == nil || *v.ReplayOf != first.Target {
		t.Errorf("replay_of = %v, want %s", v.ReplayOf, first.Target)
	}
	s.waitEnded(op.Target, "done")
	s.noAgentCalls(calls, "the replay")

	// a failing proof: the replay is refused, ProofRequired, and starts nothing
	s.set(func() { s.failCheck = "tests/TestsBuild" })
	bad := s.proofRun(id, "proof-bad")
	s.waitStatus(bad.Target, "waiting-for-developer")
	s.opBody(s.do("POST", apiPath("/operations/"+bad.Id+"/cancel"), ""), 202)
	s.waitEnded(bad.Target, "cancelled")
	s.set(func() { s.failCheck = "" })
	s.refusedReplay(first.Target, "replay-two", 409, gen.ErrorCodeProofRequired)
	s.prove(id, "proof-good")
	r := s.opBody(s.do("POST", apiPath("/runs/"+first.Target+"/replay"), `{"project":"replay-three"}`), 202)
	s.waitEnded(r.Target, "done")

	// a method change: the same
	s.setProofMethod(id, cmMethodB)
	s.refusedReplay(first.Target, "replay-four", 409, gen.ErrorCodeProofRequired)
}

func (s *srRig) refusedReplay(run, project string, status int, code gen.ErrorCode) {
	s.t.Helper()
	runs, ops, calls := s.runCount(), s.opCount(), s.w.callCount()
	s.do("POST", apiPath("/runs/"+run+"/replay"), `{"project":"`+project+`"}`).problem(s.t, status, code)
	if s.runCount() != runs || s.opCount() != ops || s.w.callCount() != calls {
		s.t.Errorf("the refused replay of %s started something", run)
	}
}

// changeGoals changes the second goal's text of a slice through the API.
func (s *srRig) changeGoals(slice string) {
	s.t.Helper()
	var sl struct {
		Goals []map[string]any `json:"goals"`
	}
	r := s.do("GET", spURL(slice, ""), "")
	if err := json.Unmarshal(r.Body.Bytes(), &sl); err != nil || r.Code != 200 || len(sl.Goals) < 2 {
		s.t.Fatalf("getSlice %s = %d: %s", slice, r.Code, r.Body.String())
	}
	sl.Goals[1]["text"] = fmt.Sprint(sl.Goals[1]["text"]) + " all"
	b, _ := json.Marshal(map[string]any{"goals": sl.Goals})
	op := s.opBody(s.do("PUT", spURL(slice, "/goals"), string(b)), 202)
	if got := s.waitOp(op.Id); got.Status != "succeeded" {
		s.t.Fatalf("replaceGoals of %s ended %+v", slice, got)
	}
}

// ---- capture and the runs that continue ----

// T013: capturing a replay whose slice changed after the earlier run started, or a replay of a run
// with no part hashes, answers RunNotCapturable; a replay of an unchanged slice's run is captured
// (control: the refusal is about the change).
func TestSRCapturingAReplayWhoseSliceChangedOrThatHasNoPartHashesIsRunNotCapturable(t *testing.T) {
	s := newSR(t)
	r1 := s.startRun("S-1", "base-one", cpBase)
	s.waitEnded(r1.Target, "done")

	// control: the replay of a run of the unchanged slice is captured
	same := s.opBody(s.do("POST", apiPath("/runs/"+r1.Target+"/replay"), `{"project":"replay-same"}`), 202)
	s.waitEnded(same.Target, "done")
	c := s.opBody(s.do("POST", apiPath("/captured-patterns"), `{"run":"`+same.Target+`","name":"fromreplay"}`), 202)
	if got := s.waitOp(c.Id); got.Status != "succeeded" {
		t.Fatalf("control: capturing the replay of an unchanged slice's run ended %+v", got)
	}

	// no part hashes: a copy of the earlier run's record without them, then its replay
	clone := s.cloneRun(r1.Target, func(started map[string]any) { delete(started, "part_hashes") })
	noHashes := s.opBody(s.do("POST", apiPath("/runs/"+clone+"/replay"), `{"project":"replay-none"}`), 202)
	s.waitEnded(noHashes.Target, "done")
	s.do("POST", apiPath("/captured-patterns"), `{"run":"`+noHashes.Target+`","name":"nohashes"}`).problem(t, 409, gen.ErrorCodeRunNotCapturable)

	// the slice changed after the earlier run started: the replay carries the earlier run's hashes
	s.changeGoals("S-1")
	changed := s.opBody(s.do("POST", apiPath("/runs/"+r1.Target+"/replay"), `{"project":"replay-changed"}`), 202)
	s.waitEnded(changed.Target, "done")
	p := s.do("POST", apiPath("/captured-patterns"), `{"run":"`+changed.Target+`","name":"changed"}`).problem(t, 409, gen.ErrorCodeRunNotCapturable)
	if p.Detail == nil || !strings.Contains(*p.Detail, "goals") {
		t.Errorf("the detail %v does not name the part that changed (goals)", p.Detail)
	}
}

// cloneRun copies a run's record under a new run ID, editing its RunStarted (as the capture
// tests do), and returns the new run.
func (s *srRig) cloneRun(from string, edit func(started map[string]any)) string {
	s.t.Helper()
	evs, err := s.st.Events(from)
	if err != nil || len(evs) == 0 {
		s.t.Fatalf("cloneRun: the run %s has no record: %v", from, err)
	}
	id, err := s.st.NextRunID()
	if err != nil {
		s.t.Fatal(err)
	}
	for _, e := range evs {
		var data map[string]json.RawMessage
		if err := json.Unmarshal([]byte(e.Data), &data); err != nil {
			s.t.Fatal(err)
		}
		var ev map[string]any
		if err := json.Unmarshal(data["event"], &ev); err != nil {
			s.t.Fatal(err)
		}
		ev["run"] = id
		if e.Kind == "RunStarted" {
			edit(ev)
		}
		data["event"], _ = json.Marshal(ev)
		raw, _ := json.Marshal(data)
		if _, err := s.st.RecordEvent("S-1", store.Event{Seq: e.Seq, Run: id, Kind: e.Kind, Step: e.Step, Data: store.RawJSON(raw)}); err != nil {
			s.t.Fatal(err)
		}
	}
	s.waitEnded(id, "done")
	return id
}

// T013, FR-001: capturing a run built from a captured pattern answers RunNotCapturable.
func TestSRCapturingARunBuiltFromACapturedPatternIsRunNotCapturable(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.addSlice("Let me keep tasks.", true)
	s.registerSteps(later, cpFillOnly)
	s.approved("notes")
	op := s.startRun(later, "tasks-app", "")
	s.waitEnded(op.Target, "done")
	s.do("POST", apiPath("/captured-patterns"), `{"run":"`+op.Target+`","name":"again"}`).problem(t, 409, gen.ErrorCodeRunNotCapturable)
}

// ---- no agent step when a filled check fails ----

// T013, US2 scenario 4: a run from a captured pattern whose filled check fails is handed over
// naming the failing check; no agent step is started to repair it (the fill step is not attempted
// again, and no agent worker call is made); taking it over ends it handed over.
func TestSRAFilledCheckThatFailsHandsTheRunOverWithNoAgentStep(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.addSlice("Let me keep tasks.", true)
	s.registerSteps(later, cpFillOnly)
	id := s.approved("notes")
	cp := s.captured(id)
	s.set(func() { s.failCheck = "tests/TestsBuild" })
	calls := s.w.callCount()

	op := s.startRun(later, "tasks-app", "")
	v := s.waitStatus(op.Target, "waiting-for-developer")
	if v.Reason == "" {
		t.Errorf("a run waiting for the developer has a reason: %+v", v)
	}
	s.wantChoice(op.Target, id+"@"+string(cp.Version), gen.PatternReasonFits, srWant(cp, srTaskRepl), cp.Source.Run)
	s.noAgentCalls(calls, "the handed-over run")
	// the events are read once the run has ended (owner-approved 2026-11-26): until then
	// listRunEvents keeps a next_page_token on its last page (contract)
	s.opBody(s.do("POST", apiPath("/runs/"+op.Target+"/hand-over"), ""), 202)
	s.waitEnded(op.Target, "handed-over")
	var failed []string
	for _, e := range s.allEvents(op.Target) {
		if e.Kind == "CheckFailed" && e.Check != nil {
			failed = append(failed, e.Step+"/"+e.Check.Name)
		}
	}
	if len(failed) != 1 || failed[0] != "tests/TestsBuild" {
		t.Errorf("failed checks %v, want exactly tests/TestsBuild: the run is handed over at the first failing filled check, with no repair attempt", failed)
	}
	n := 0
	for _, step := range s.kinds(op.Target, "AttemptMade") {
		if step == "tests" {
			n++
		}
	}
	if n > 1 {
		t.Errorf("%d attempts were made of the filled step tests, want at most 1: a fill step is never attempted twice", n)
	}
	s.noAgentCalls(calls, "the run after its take-over")
}
