package api

// Feature 011, T033 (US1): the cases of T006 whose first author could not reach them, now that the
// owner-approved seams of durable_service_test.go exist (T002, 2026-11-26): DurableHoldRun holds a
// run before its first recorded event, DurableAlterFilled alters the bytes a `fill` step stores.
// Run on the engine rig of captured_proof_test.go (cpRig: the real Run workflow on Temporal's test
// environment, the step worker and the checker faked), through HTTP and the generated types. From
// spec FR-004 to FR-006 and FR-011, the data model ("Proof", "Busy") and T006. Written before
// T029 by a test author; the code author must not change them (ADR 0029).
//
// Two things these cases do beyond HTTP, both on the captured pattern's own store record
// (CapturedStore, the engine's store), and both state the engine itself can be in:
//   - a captured pattern whose Proof is missing for a run whose RunStarted has `proof: true` (the
//     engine stopped between the run's start and the Proof's write);
//   - a Proof recorded under another method (the engine was upgraded to a new method after the
//     proof). The rig's events carry no method of their own and method.Current() is the process's,
//     so the record is the only place to put a proof of another method.
// They edit only the keys `proofs` and `state` of the record's JSON and leave every other byte.
//
// A start waits up to recordWait (10 s, real time) for the run's RunStarted (runs.go), so a held
// run's startRun answers 202 after that wait; the busy test pays it once.
//
// Edge cases checked: the answer lost after the workflow exists (one Proof, also read many times
// and listed, while running and after the end, and the operation follows the run); no Proof on
// record while the run is running and after it passed (one, never two, repeated reads, the list);
// the Proof made on read keeps the run's values and method; PUT /parameters busy while the start
// is accepted but RunStarted is not recorded, not busy once the run ended; a changed byte in one
// file names exactly that file and its step (not the unchanged files), the captured pattern is not
// proven, approval answers ProofRequired, and the same rig proves it again when the plant is gone;
// a Proof of another method refuses approval and is not usable, a new proof under the current
// method allows it and approval records that proof.
// Left out: a proof whose run waits for a human at the moment of the lost answer (the answer is
// lost at Start, before any step); two captured patterns at once (one rig, one pattern); a
// restart between the lost answer and the read (captured_proof_test.go has the restart cases).

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/canonical"
)

// DurableHoldRun, when set, is called in the durable test service's goroutine for each run before
// its workflow records anything; it returns when the run may go on. Set only by tests.
var DurableHoldRun atomic.Pointer[func(run string)]

// DurableAlterFilled, when set, gives the files a `fill` step is about to store and returns the
// files stored instead. Set only by tests.
var DurableAlterFilled atomic.Pointer[func(files []canonical.File) []canonical.File]

// lostAnswerRuns is the durable test service whose first Start of a proof run creates the workflow
// and then fails with a timeout that counts as unavailable (the answer is lost).
type lostAnswerRuns struct {
	DurableTestService
	mu   sync.Mutex
	lost int
}

func (l *lostAnswerRuns) Start(ctx context.Context, r RunStart) error {
	if err := l.DurableTestService.Start(ctx, r); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if r.Proof && l.lost == 0 {
		l.lost++
		return fmt.Errorf("starting %s: %w: %w", r.Run, ErrUnavailable, context.DeadlineExceeded)
	}
	return nil
}

func (l *lostAnswerRuns) lostCount() int { l.mu.Lock(); defer l.mu.Unlock(); return l.lost }

// bootLostAnswers replaces the rig's handler with one over the same store whose durable service
// loses the answer of the first proof start.
func (c *cpRig) bootLostAnswers() *lostAnswerRuns {
	ctx, cancel := context.WithCancel(context.Background())
	x := c.runsRig
	l := &lostAnswerRuns{}
	x.r = newRig(x.t, func(cfg *Config) {
		cfg.Context = ctx
		cfg.Slices, cfg.Store = x.sp.svc, x.st
		cfg.WorkerGrants = mergeGrants(stepGrants(cpSourceS), stepGrants(cpProofOf))
		cfg.Projects = x.projects
		cfg.Model = "test-model"
		cfg.StepWorker, cfg.StepChecker = x.w, x.c
		p, err := cfg.Slices.Pattern()
		if err != nil {
			x.t.Fatal(err)
		}
		rec := NewRunRecord(x.st, MachinePattern(p), cfg.Model)
		svc := NewDurableTestService()
		svc.Use(DurablePorts{Worker: cfg.StepWorker, Checker: cfg.StepChecker, Record: rec, Artifacts: x.st, Now: cfg.Now, Context: ctx})
		x.t.Cleanup(svc.Close)
		l.DurableTestService = svc
		cfg.Runs, cfg.Record = l, rec
	})
	x.r.clock.onAdvance(l.Tick)
	registerAll(x.t, x.r, cpSourceS) // a new handler has no registered workers
	stopEngineAtEnd(x.t, x.r, cancel)
	return l
}

// editRecord changes the captured pattern's stored record: f gets the record's keys and may
// replace some of them; every key it leaves is written back as it was.
func (c *cpRig) editRecord(f func(m map[string]json.RawMessage)) {
	c.t.Helper()
	b, err := c.st.CapturedPattern(cpID)
	if err != nil {
		c.t.Fatal(err)
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &m); err != nil {
		c.t.Fatalf("the captured pattern's record is not a JSON object: %v", err)
	}
	f(m)
	out, err := json.Marshal(m)
	if err != nil {
		c.t.Fatal(err)
	}
	if err := c.st.PutCapturedPattern(cpID, out, nil); err != nil {
		c.t.Fatal(err)
	}
}

// listed returns the captured pattern as listCapturedPatterns shows it.
func (c *cpRig) listed() gen.CapturedPattern {
	c.t.Helper()
	l := c.do("GET", apiPath("/captured-patterns"), "")
	var list gen.CapturedPatternList
	if l.Code != 200 || json.Unmarshal(l.Body.Bytes(), &list) != nil || len(list.Items) != 1 {
		c.t.Fatalf("listCapturedPatterns = %d %s, want one captured pattern", l.Code, l.Body.String())
	}
	return list.Items[0]
}

// wantOneProof reads the captured pattern three ways and fails unless each shows exactly one
// Proof, of run, and returns the last one read.
func (c *cpRig) wantOneProof(run string) gen.Proof {
	c.t.Helper()
	var last gen.Proof
	for i, cp := range []gen.CapturedPattern{c.pattern(), c.pattern(), c.listed()} {
		if len(cp.Proofs) != 1 || cp.Proofs[0].Run != run {
			c.t.Fatalf("read %d shows proofs %+v, want exactly one, of run %s", i, cp.Proofs, run)
		}
		last = cp.Proofs[0]
	}
	return last
}

// T006: a lost start answer (startedAnyway) keeps exactly one Proof. The durable service creates
// the proof's workflow, then the start fails with a timeout: the engine finds the workflow, accepts
// the start (202, an operation that follows the run), and the run has exactly one Proof, while it
// runs, after it ended, however many times it is read, and in the list (data model, "Proof"; FR-005).
func TestALostStartAnswerOfAProofKeepsExactlyOneProof(t *testing.T) {
	c := newCP(t)
	lost := c.bootLostAnswers()
	cp := c.capture()
	slice := c.proofSlice()
	gate := make(chan struct{})
	c.set(func() { c.gate = gate })
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(gate) }) })
	p := c.startProof(slice, "proof-app")
	if lost.lostCount() != 1 {
		t.Fatalf("setup: the rig lost %d start answers, want 1", lost.lostCount())
	}
	op := c.opOf(p, "run") // 202: the workflow exists, so the start is accepted
	if got := c.wantOneProof(op.Target); got.Outcome != gen.ProofOutcomeRunning || got.Method != cp.Method {
		t.Errorf("the proof of the run that has not ended = %+v, want running, method %s", got, cp.Method)
	}
	once.Do(func() { close(gate) })
	c.waitEnded(op.Target, "done")
	got := c.wantOneProof(op.Target)
	if got.Outcome != gen.ProofOutcomePassed || len(got.Failures) != 0 || cpJSON(t, got.Parameters) != cpJSON(t, cpValues(cp)) {
		t.Errorf("the proof after the end = %+v, want passed with the source values and no failure", got)
	}
	if st := c.pattern().State; st != gen.CapturedPatternStateProven {
		t.Errorf("state %s after the passing proof, want proven", st)
	}
}

// T006: a `proof: true` run that has no Proof (the engine stopped between the run's recorded
// RunStarted and the Proof's write) gets one on read, from the run: running while the run has not
// ended, under the run's values, and passed once it ended, never two (data model, "Proof": "the
// Proof-completion function creates it from that run"; FR-005).
func TestAProofRunWithNoProofGetsOneOnRead(t *testing.T) {
	c := newCP(t)
	cp := c.capture()
	gate := make(chan struct{})
	c.set(func() { c.gate = gate })
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(gate) }) })
	op := c.prove("proof-app") // RunStarted is recorded when it answers; the worker is held
	c.editRecord(func(m map[string]json.RawMessage) {
		m["proofs"] = json.RawMessage(`[]`)
		m["state"] = json.RawMessage(`"proposed"`)
	})
	got := c.wantOneProof(op.Target)
	if got.Outcome != gen.ProofOutcomeRunning || got.Method != cp.Method || cpJSON(t, got.Parameters) != cpJSON(t, cpValues(cp)) || len(got.Failures) != 0 {
		t.Errorf("the proof made on read = %+v, want running, method %s, the source values, no failure", got, cp.Method)
	}
	if st := c.pattern().State; st == gen.CapturedPatternStateProven {
		t.Errorf("a proof of a run that has not ended made the pattern %s", st)
	}
	once.Do(func() { close(gate) })
	c.waitEnded(op.Target, "done")
	if got := c.wantOneProof(op.Target); got.Outcome != gen.ProofOutcomePassed {
		t.Errorf("the proof after the end = %+v, want passed", got)
	}
	if st := c.pattern().State; st != gen.CapturedPatternStateProven {
		t.Errorf("state %s, want proven", st)
	}
}

// T006: PUT /parameters sent between a proof start's 202 and its recorded RunStarted answers 409
// CapturedPatternBusy: busy is judged from the proof slice's runs, and a run that is accepted with
// no RunStarted yet is one (data model, "Busy"; FR-011). The run is held before its first event, so
// the captured pattern shows no Proof yet; once the run ends the same PUT is accepted.
func TestReplaceParametersBetweenAProofStartsAnswerAndItsRunStartedIsBusy(t *testing.T) {
	c := newCP(t)
	cp := c.capture()
	slice := c.proofSlice()
	gate := make(chan struct{})
	hold := func(string) { <-gate }
	DurableHoldRun.Store(&hold)
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(gate) }); DurableHoldRun.Store(nil) })
	op := c.opOf(c.startProof(slice, "proof-app"), "run") // the 202, after the start's wait for RunStarted
	if n := len(c.pattern().Proofs); n != 0 {
		t.Fatalf("setup: %d proofs before the run recorded RunStarted, want none (the hold did not hold)", n)
	}
	same := make([]gen.ParameterValueRequest, 0, len(cp.Parameters))
	for _, p := range cp.Parameters {
		same = append(same, gen.ParameterValueRequest{Name: p.Name, Value: p.Value})
	}
	b, _ := json.Marshal(gen.ParametersRequest{Parameters: same})
	c.problem(c.do("PUT", apiPath("/captured-patterns/"+cpID+"/parameters"), string(b)), 409, "CapturedPatternBusy")
	once.Do(func() { close(gate) })
	c.waitEnded(op.Target, "done")
	if got := c.waitOp(c.opBody(c.do("PUT", apiPath("/captured-patterns/"+cpID+"/parameters"), string(b)), 202).Id); got.Status != "succeeded" {
		t.Errorf("replaceParameters after the run ended = %+v, want succeeded", got)
	}
}

// T006: a planted template byte change fails the proof naming the file: the filled `code` file
// differs from the template's by one appended line, its checks pass, and the proof fails naming the
// step and the path, and only that file. The captured pattern is not proven and approval answers
// ProofRequired; with the plant gone a new proof of the same rig passes (data model, "Proof"; FR-005).
func TestAPlantedTemplateByteChangeFailsTheProofNamingTheFile(t *testing.T) {
	const planted = "internal/app/note.go"
	c := newCP(t)
	c.capture()
	alter := func(files []canonical.File) []canonical.File {
		out := make([]canonical.File, len(files))
		for i, f := range files {
			out[i] = canonical.File{Path: f.Path, Content: append([]byte(nil), f.Content...)}
			if f.Path == planted {
				out[i].Content = append(out[i].Content, "// planted\n"...)
			}
		}
		return out
	}
	DurableAlterFilled.Store(&alter)
	t.Cleanup(func() { DurableAlterFilled.Store(nil) })
	op := c.prove("proof-app")
	c.waitEnded(op.Target, "done") // every check passed: only the comparison with the template fails it
	cp := c.pattern()
	p := c.lastProof(cp)
	if p.Outcome != gen.ProofOutcomeFailed || len(p.Failures) != 1 {
		t.Fatalf("the proof = %+v, want failed with exactly one failure, the planted file", p)
	}
	f := p.Failures[0]
	if f.Path == nil || *f.Path != planted || f.Step == nil || *f.Step != "code" || f.Check != nil || f.Reason == "" {
		t.Errorf("the failure = %+v, want step code, path %s, no check, a reason", f, planted)
	}
	if cp.State == gen.CapturedPatternStateProven || cp.Usable {
		t.Errorf("state %s usable %v after a failed proof", cp.State, cp.Usable)
	}
	c.problem(c.do("POST", apiPath("/captured-patterns/"+cpID+"/approval"), ""), 409, "ProofRequired")
	DurableAlterFilled.Store(nil)
	again := c.prove("proof-two")
	c.waitEnded(again.Target, "done")
	if got := c.lastProof(c.pattern()); got.Run != again.Target || got.Outcome != gen.ProofOutcomePassed {
		t.Errorf("the proof without the plant = %+v, want passed: the failure was the planted byte", got)
	}
}

// T006: a proof under another method does not count for approval: with a passing proof of another
// method as the latest, approval answers ProofRequired and the captured pattern is not usable; a
// new proof under the engine's method allows approval, which records that proof (data model,
// "Proof" and "States"; FR-006).
func TestAProofUnderAnotherMethodDoesNotCountForApproval(t *testing.T) {
	c := newCP(t)
	c.capture()
	op := c.prove("proof-app")
	c.waitEnded(op.Target, "done")
	if got := c.pattern(); got.State != gen.CapturedPatternStateProven || c.lastProof(got).Outcome != gen.ProofOutcomePassed {
		t.Fatalf("setup: %+v, want a passed proof and state proven", got)
	}
	c.editRecord(func(m map[string]json.RawMessage) {
		var proofs []map[string]json.RawMessage
		if err := json.Unmarshal(m["proofs"], &proofs); err != nil || len(proofs) != 1 {
			c.t.Fatalf("the record's proofs = %s (%v), want one", m["proofs"], err)
		}
		proofs[0]["method"] = json.RawMessage(`"` + cmMethodB + `"`)
		b, err := json.Marshal(proofs)
		if err != nil {
			c.t.Fatal(err)
		}
		m["proofs"] = b
	})
	if p := c.lastProof(c.pattern()); p.Method != cmMethodB || p.Outcome != gen.ProofOutcomePassed {
		t.Fatalf("setup: the proof = %+v, want passed under %s", p, cmMethodB)
	}
	c.problem(c.do("POST", apiPath("/captured-patterns/"+cpID+"/approval"), ""), 409, "ProofRequired")
	if got := c.pattern(); got.Usable || got.Approval != nil || got.State == gen.CapturedPatternStateApproved {
		t.Errorf("after a refused approval: state %s usable %v approval %+v, want none of them approved or usable", got.State, got.Usable, got.Approval)
	}
	again := c.prove("proof-two")
	c.waitEnded(again.Target, "done")
	if p := c.lastProof(c.pattern()); p.Run != again.Target || p.Outcome != gen.ProofOutcomePassed || p.Method == cmMethodB {
		t.Fatalf("the new proof = %+v, want passed under the engine's own method", p)
	}
	if got := c.waitOp(c.opBody(c.do("POST", apiPath("/captured-patterns/"+cpID+"/approval"), ""), 202).Id); got.Status != "succeeded" {
		t.Fatalf("approval after a proof under the current method ended %+v", got)
	}
	if got := c.pattern(); got.State != gen.CapturedPatternStateApproved || got.Approval == nil || got.Approval.Proof != again.Target || !got.Usable {
		t.Errorf("after approval: state %s usable %v approval %+v, want approved, usable, with proof %s", got.State, got.Usable, got.Approval, again.Target)
	}
}
