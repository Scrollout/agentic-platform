package api

// T066 (feature 002, user story 3): what deleteSlice's work does to the durable service and to the
// operation records when it erases a slice. Written before the code (T029) by a test author.
// Judged from spec FR-017 and SC-006, the data model ("Erasing a slice", "Kept") and research R8.
//
// Proves, with the fake durable ports and clock of operations_durable_test.go:
//   - the durable service is asked to delete each run's workflow of the slice (Runs.Delete) and
//     each other operation's workflow whose target is the slice or one of its runs
//     (Operations.Delete), ended or still open, and nothing else: not the erasing operation's own
//     workflow, not another slice's runs or operations, not an operation of a run or slice whose ID
//     only starts like one of ours (S-10, RUN-10);
//   - those operations' records (and bodies) are gone at once (getOperation 404), each only after
//     its workflow's deletion succeeded (a failed deletion leaves the record; the repeat does both);
//   - the erasing operation ends succeeded, its record holds only the slice's ID (no run ID, no
//     other slice or operation ID), it is answered exactly 24 h after its end and is 404 one second
//     later, and its own workflow is never deleted;
//   - NotFound from either Delete is no error; an unavailable service is Retryable (the work is
//     repeated, and succeeds once the service answers); one that stays unavailable ends the
//     operation failed with the slice still there, and a later deleteSlice finishes the job and
//     also removes the failed operation's record;
//   - an operation still open on the slice loses its workflow and record, and its work, finding no
//     record, does not bring one back.
//
// Assumed beyond the brief: operation records of the slice are written straight into the store
// (PutOperation/EndOperation, kinds replaceGoals, startRun, resumeRun, deleteSlice), as the real
// handler would have left them, and runs are recorded as events (RunStarted, RunCompleted) so
// they have ended; the erasing operation is the real DELETE /slices/S-1. The wrapped fakes
// (seraOps, seraRuns) embed the existing ones and only add failing and NotFound answers and a
// list of attempts. Time: each test is under a second; the whole file under 5 s.
//
// Edge cases checked: no run and no other operation (no Delete call at all), prefix-like IDs
// (S-10, RUN-10), another slice's runs and operations, an open operation, a failed earlier
// deleteSlice operation, NotFound from runs only / operations only / both, unavailable for the
// first two calls (retried) and for good (failed, slice kept, repeat finishes), a record kept when
// its workflow deletion failed, exactly 24 h and 24 h plus a second after the end.
// Left untested: a store write failing mid-erasure, an engine restart in the middle of the work,
// the real Temporal deletion (T028), a concurrent operation accepted after the erasure began.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/api/gen"
	slicesvc "github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
)

// ---- wrapped fakes ----

// seraFail says how Delete answers: unavailable for the first failN calls (-1: always), or for the
// given IDs; NotFound for every call when notFound is set (the workflow is deleted nonetheless).
type seraFail struct {
	mu       sync.Mutex
	failN    int
	failIDs  map[string]bool
	notFound bool
	attempts []string
	deleted  []string // calls that were answered nil or NotFound
}

func (s *seraFail) answer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts = append(s.attempts, id)
	if s.failN != 0 || s.failIDs[id] {
		if s.failN > 0 {
			s.failN--
		}
		return fmt.Errorf("fake durable service: %w", ErrUnavailable)
	}
	s.deleted = append(s.deleted, id)
	if s.notFound {
		return fmt.Errorf("fake durable service: workflow %s: %w", id, ErrNotFound)
	}
	return nil
}
func (s *seraFail) set(fn func(s *seraFail)) { s.mu.Lock(); fn(s); s.mu.Unlock() }
func (s *seraFail) tried() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.attempts)
}

type seraOps struct {
	*dopOps
	f seraFail
}

func (o *seraOps) Delete(ctx context.Context, id string) error {
	if err := o.f.answer(id); err != nil {
		return err
	}
	return o.dopOps.Delete(ctx, id)
}

type seraRuns struct {
	*dopRuns
	f seraFail
}

func (r *seraRuns) Delete(ctx context.Context, id string) error {
	if err := r.f.answer(id); err != nil {
		return err
	}
	return r.dopRuns.Delete(ctx, id)
}

// ---- the rig ----

type seraRig struct {
	*dopRig
	sops  *seraOps
	sruns *seraRuns
}

func newSeraRig(t *testing.T, slicesN int) *seraRig {
	t.Helper()
	x := &dopRig{t: t, dir: t.TempDir(), projects: t.TempDir(), clock: newFakeClock(),
		analyst: &spAnalyst{results: []spResult{{p: spNotes(t)}}}, log: &dopLog{}, client: clientToken}
	x.runs = newDopRuns(x.log)
	x.ops = newDopOps(t, x.dir, x.log)
	s := &seraRig{dopRig: x, sops: &seraOps{dopOps: x.ops}, sruns: &seraRuns{dopRuns: x.runs}}
	s.bootWrapped()
	x.seed(slicesN, false)
	return s
}

// bootWrapped is dopRig.boot with the wrapped fakes as the durable ports.
func (s *seraRig) bootWrapped() {
	x, t := s.dopRig, s.t
	t.Helper()
	st, err := store.Open(x.dir, spSchemas(t))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	svc, err := slicesvc.New(slicesvc.Config{Schemas: spSchemas(t), Analyst: x.analyst, Diff: spDiff{}, Lint: spLint{},
		Pattern: spPattern(t), Records: st})
	if err != nil {
		t.Fatalf("the slices over the store: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	x.st, x.svc, x.cancel, x.running = st, svc, cancel, &sync.WaitGroup{}
	x.ops.set(func(f *dopOps) { f.ctx = ctx })
	rec := &recorder{}
	h, op := NewEngine(Config{
		Tokens: Tokens{Client: x.client, Page: pageToken, Worker: workerToken},
		Now:    x.clock.Now, Sleep: func(time.Duration) {}, Work: rec.work, Context: ctx, Running: x.running,
		Slices: svc, Store: st, WorkerGrants: stepGrants("S-1"), Projects: x.projects, Model: "test-model",
		StepWorker: &fakeStepWorker{st: st, data: map[string][]byte{}}, StepChecker: &fakeStepChecker{},
		Workers: NewWorkers(), Runs: s.sruns, Operations: s.sops,
	})
	if h == nil || op == nil {
		t.Fatal("NewEngine gave no handler or no Operator")
	}
	x.r, x.op = &rig{h: h, clock: x.clock, rec: rec}, op
	x.ops.use(op)
}

// endedRun records a run of a slice as its workflow would, up to RunCompleted.
func (s *seraRig) endedRun(sliceID, run string) {
	s.t.Helper()
	p, err := s.svc.Pattern()
	if err != nil {
		s.t.Fatal(err)
	}
	zero := "sha256:" + strings.Repeat("0", 64)
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	for i, kind := range []string{"RunStarted", "RunCompleted"} {
		e := storedEvent{Event: machineEvent(i+1, at.Add(time.Duration(i)*time.Second), kind, "")}
		e.Event.Run = run
		if kind == "RunStarted" {
			e.Event.Slice, e.Event.Project, e.Event.Pattern = sliceID, "notes-app", p.ID+"@"+p.Version
			e.Event.Inputs = []string{zero, zero, zero}
			e.Info = &runInfo{Project: "notes-app"}
		}
		data, _ := json.Marshal(e)
		if _, err := s.st.Append(sliceID, store.Event{Run: run, Kind: kind, Data: store.RawJSON(data)}); err != nil {
			s.t.Fatal(err)
		}
	}
	s.runs.set(func(f *dopRuns) { f.exists[run] = true })
}

// putOp writes an operation's record with a body; ended: with the status given, else it stays open.
func (s *seraRig) putOp(kind, target, status string) string {
	s.t.Helper()
	id, err := s.st.NextOperationID()
	if err != nil {
		s.t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(id))
	rec := store.OperationRecord{ID: id, Kind: kind, Target: target, Body: hex.EncodeToString(sum[:]), AcceptedAt: s.clock.Now()}
	if err := s.st.PutOperation(rec, []byte(`{}`)); err != nil {
		s.t.Fatal(err)
	}
	if status != "" {
		if _, err := s.st.EndOperation(id, s.clock.Now(), status, nil, nil); err != nil {
			s.t.Fatal(err)
		}
	}
	return id
}

type seraScene struct {
	victims []string // other operations of S-1 or of its runs, ended
	decoys  []string // operations that are not S-1's
}

// scene: S-1 with ended RUN-1, RUN-2 and operations on it and them; S-2 with RUN-3 and its
// operations; and operations on S-10 and RUN-10 (IDs that only start like ours).
func (s *seraRig) scene() seraScene {
	s.t.Helper()
	s.endedRun("S-1", "RUN-1")
	s.endedRun("S-1", "RUN-2")
	s.endedRun("S-2", "RUN-3")
	var sc seraScene
	sc.victims = []string{
		s.putOp("replaceGoals", "S-1", "succeeded"),
		s.putOp("startRun", "RUN-1", "succeeded"),
		s.putOp("resumeRun", "RUN-1", "failed"),
		s.putOp("startRun", "RUN-2", "succeeded"),
		s.putOp("deleteSlice", "S-1", "failed"),
	}
	sc.decoys = []string{
		s.putOp("replaceGoals", "S-2", "succeeded"),
		s.putOp("startRun", "RUN-3", "succeeded"),
		s.putOp("replaceGoals", "S-10", "succeeded"),
		s.putOp("startRun", "RUN-10", "succeeded"),
	}
	return sc
}

func (s *seraRig) deleteS1() string {
	s.t.Helper()
	s.clock.Advance(1100 * time.Millisecond)
	return s.accepted(s.do("DELETE", spURL("S-1", ""), "", ""), "delete-slice", "S-1").ID
}

func (s *seraRig) opAnswer(id string) reply {
	s.clock.Advance(1100 * time.Millisecond)
	return s.getOp(id)
}

func (s *seraRig) wantGone(ids ...string) {
	s.t.Helper()
	for _, id := range ids {
		s.opAnswer(id).problem(s.t, 404, gen.ErrorCodeNotFound)
		for _, n := range s.opFiles() {
			if strings.HasPrefix(n, id+".") {
				s.t.Errorf("operation %s: %s is still in the store", id, n)
			}
		}
	}
}

func (s *seraRig) wantKept(ids ...string) {
	s.t.Helper()
	for _, id := range ids {
		if p := s.opAnswer(id); p.Code != 200 {
			s.t.Errorf("operation %s = %d, want 200 (kept): %s", id, p.Code, p.Body.String())
		}
	}
}

func sorted(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}

func (s *seraRig) wantSliceGone() {
	s.t.Helper()
	s.clock.Advance(1100 * time.Millisecond)
	s.get(spURL("S-1", "")).problem(s.t, 404, gen.ErrorCodeNotFound)
	for _, e := range s.st.Runs() {
		if e.Slice == "S-1" {
			s.t.Errorf("the store still holds run %s of S-1", e.Run)
		}
	}
}

func (s *seraRig) wantOtherSliceKept() {
	s.t.Helper()
	s.clock.Advance(1100 * time.Millisecond)
	if p := s.get(spURL("S-2", "")); p.Code != 200 {
		s.t.Errorf("S-2 = %d after S-1's erasure, want 200", p.Code)
	}
	found := false
	for _, e := range s.st.Runs() {
		found = found || e.Run == "RUN-3"
	}
	if !found {
		s.t.Errorf("RUN-3 of S-2 is gone from the store")
	}
}

// ---- tests ----

func TestSliceErasureAsksTheDurableServiceToDeleteExactlyItsRunsAndOperations(t *testing.T) {
	x := newSeraRig(t, 2)
	sc := x.scene()
	own := x.deleteS1()
	end := x.settle(own)
	if end.Status != "succeeded" {
		t.Fatalf("the erasing operation %+v, want succeeded", end)
	}
	x.wantSliceGone()
	x.wantOtherSliceKept()
	if got, want := sorted(x.sruns.f.tried()), []string{"RUN-1", "RUN-2"}; !slices.Equal(got, want) {
		t.Errorf("run workflows deleted = %v, want %v (RUN-3 is S-2's, RUN-10 is nobody's here)", got, want)
	}
	if got, want := sorted(x.sops.f.tried()), sorted(sc.victims); !slices.Equal(got, want) {
		t.Errorf("operation workflows deleted = %v, want exactly %v (not the erasing %s, not %v)", got, want, own, sc.decoys)
	}
	x.wantGone(sc.victims...)
	x.wantKept(sc.decoys...)
}

func TestSliceErasureLeavesItsOwnOperationFollowedFor24hAfterItsEndOnly(t *testing.T) {
	x := newSeraRig(t, 2)
	x.scene()
	own := x.deleteS1()
	x.settle(own)
	if slices.Contains(x.sops.f.tried(), own) {
		t.Errorf("the erasing operation's own workflow %s was deleted", own)
	}
	// The record's end is written before the workflow closes: wait for the close.
	var ok bool
	var err error
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if ok, err = x.ops.Open(context.Background(), own); err != nil || !ok || time.Now().After(deadline) {
			break
		}
	}
	if err != nil || ok {
		t.Errorf("the erasing operation's workflow: open=%v err=%v, want closed and still there", ok, err)
	}
	rec := x.record(own)
	if rec["status"] != "succeeded" || rec["target"] != "S-1" {
		t.Fatalf("the record %v, want succeeded with target S-1", rec)
	}
	// It holds only the slice's ID: no other slice, run or operation ID anywhere in the record.
	raw, _ := json.Marshal(rec)
	idRE := regexp.MustCompile(`\b(S|RUN|OP)-[0-9]+\b`)
	for _, m := range idRE.FindAllString(string(raw), -1) {
		if m != "S-1" && m != own {
			t.Errorf("the erasing operation's record names %s: %s", m, raw)
		}
	}
	endedAt, err := time.Parse(time.RFC3339Nano, rec["ended_at"].(string))
	if err != nil {
		t.Fatalf("ended_at: %v", err)
	}
	keep := time.Duration(contractLimit(t, "idempotency-key-retention-hours")) * time.Hour
	x.clock.Advance(endedAt.Add(keep).Sub(x.clock.Now()))
	if p := x.r.do("GET", "/operations/"+own+"?api-version="+apiVersion, x.client, "", nil); p.Code != 200 {
		t.Fatalf("exactly %s after its end the erasing operation = %d, want 200: %s", keep, p.Code, p.Body.String())
	}
	x.clock.Advance(time.Second)
	x.r.do("GET", "/operations/"+own+"?api-version="+apiVersion, x.client, "", nil).problem(t, 404, gen.ErrorCodeNotFound)
}

func TestSliceErasureOfASliceWithNoRunAndNoOtherOperationDeletesNoWorkflow(t *testing.T) {
	x := newSeraRig(t, 1)
	own := x.deleteS1()
	if end := x.settle(own); end.Status != "succeeded" {
		t.Fatalf("operation %+v, want succeeded", end)
	}
	x.wantSliceGone()
	if r, o := x.sruns.f.tried(), x.sops.f.tried(); len(r)+len(o) != 0 {
		t.Errorf("workflows deleted: runs %v, operations %v, want none", r, o)
	}
}

func TestSliceErasureTreatsANotFoundWorkflowAsDeleted(t *testing.T) {
	for _, c := range []struct {
		name      string
		runs, ops bool
	}{{"runs", true, false}, {"operations", false, true}, {"both", true, true}} {
		t.Run(c.name, func(t *testing.T) {
			x := newSeraRig(t, 2)
			sc := x.scene()
			x.sruns.f.set(func(f *seraFail) { f.notFound = c.runs })
			x.sops.f.set(func(f *seraFail) { f.notFound = c.ops })
			own := x.deleteS1()
			if end := x.settle(own); end.Status != "succeeded" {
				t.Fatalf("operation %+v, want succeeded (NotFound is no error)", end)
			}
			x.wantSliceGone()
			x.wantGone(sc.victims...)
			x.wantKept(sc.decoys...)
			if got, want := sorted(x.sops.f.tried()), sorted(sc.victims); !slices.Equal(got, want) {
				t.Errorf("operation workflows asked = %v, want %v", got, want)
			}
		})
	}
}

func TestSliceErasureRetriesWhileTheDurableServiceIsUnavailableThenErasesTheSlice(t *testing.T) {
	retries := contractLimit(t, "retry-attempts")
	if retries < 3 {
		t.Skipf("retry-attempts is %d: two failures cannot be retried", retries)
	}
	for _, who := range []string{"runs", "operations"} {
		t.Run(who, func(t *testing.T) {
			x := newSeraRig(t, 2)
			sc := x.scene()
			f := &x.sops.f
			if who == "runs" {
				f = &x.sruns.f
			}
			f.set(func(f *seraFail) { f.failN = 2 })
			own := x.deleteS1()
			if end := x.settle(own); end.Status != "succeeded" {
				t.Fatalf("operation %+v, want succeeded once the service answers", end)
			}
			x.wantSliceGone()
			x.wantGone(sc.victims...)
			x.wantKept(sc.decoys...)
			x.wantOtherSliceKept()
			if n := len(f.tried()); n < 3 {
				t.Errorf("%s workflows: %d deletions asked, want the 2 failed ones repeated and the rest", who, n)
			}
			// A repeat does everything again: every run and operation is asked at least once.
			if got, want := sorted(x.sops.f.tried()), sorted(sc.victims); !slices.Equal(got, want) {
				t.Errorf("operation workflows asked = %v, want %v", got, want)
			}
			if got, want := sorted(x.sruns.f.tried()), []string{"RUN-1", "RUN-2"}; !slices.Equal(got, want) {
				t.Errorf("run workflows asked = %v, want %v", got, want)
			}
		})
	}
}

func TestSliceErasureThatStaysUnavailableFailsKeepsTheSliceAndAnOperationRecordUntilItsWorkflowIsDeleted(t *testing.T) {
	for _, who := range []string{"runs", "operations"} {
		t.Run(who, func(t *testing.T) {
			x := newSeraRig(t, 2)
			sc := x.scene()
			f := &x.sops.f
			if who == "runs" {
				f = &x.sruns.f
			}
			f.set(func(f *seraFail) { f.failN = -1 })
			first := x.deleteS1()
			end := x.settle(first)
			if end.Status != "failed" || end.Error == nil || !strings.Contains(end.Error.Detail, "attempts") {
				t.Fatalf("operation %+v, want failed after its attempts", end)
			}
			x.clock.Advance(1100 * time.Millisecond)
			if p := x.get(spURL("S-1", "")); p.Code != 200 {
				t.Fatalf("S-1 = %d after a failed erasure, want 200 (the store is erased last)", p.Code)
			}
			// No workflow deletion succeeded, so no record may have been removed.
			x.wantKept(sc.victims...)
			x.wantKept(first)

			// The service answers again: the repeat (a new deleteSlice) deletes both, workflow and record.
			f.set(func(f *seraFail) { f.failN = 0 })
			second := x.deleteS1()
			if end := x.settle(second); end.Status != "succeeded" {
				t.Fatalf("the repeat %+v, want succeeded", end)
			}
			x.wantSliceGone()
			x.wantOtherSliceKept()
			x.wantGone(append(slices.Clone(sc.victims), first)...)
			x.wantKept(sc.decoys...)
			if !slices.Contains(x.sops.f.tried(), first) {
				t.Errorf("the failed erasing operation %s targets S-1: its workflow was not deleted by the repeat", first)
			}
		})
	}
}

func TestSliceErasureKeepsAnOperationRecordWhoseWorkflowCouldNotBeDeleted(t *testing.T) {
	x := newSeraRig(t, 2)
	sc := x.scene()
	stuck := sc.victims[1]
	x.sops.f.set(func(f *seraFail) { f.failIDs = map[string]bool{stuck: true} })
	first := x.deleteS1()
	if end := x.settle(first); end.Status != "failed" {
		t.Fatalf("operation %+v, want failed while %s's workflow cannot be deleted", end, stuck)
	}
	x.wantKept(stuck) // its workflow deletion failed: its record is the only way to find it again
	x.sops.f.set(func(f *seraFail) { f.failIDs = nil; f.attempts = nil; f.deleted = nil })
	second := x.deleteS1()
	if end := x.settle(second); end.Status != "succeeded" {
		t.Fatalf("the repeat %+v, want succeeded", end)
	}
	if !slices.Contains(x.sops.f.tried(), stuck) {
		t.Errorf("the repeat did not delete %s's workflow", stuck)
	}
	x.wantGone(stuck)
	x.wantSliceGone()
}

func TestSliceErasureRemovesAnOperationStillOpenOnTheSliceAndItsWorkEndsNothing(t *testing.T) {
	x := newSeraRig(t, 2)
	sc := x.scene()
	open := x.putOp("replaceGoals", "S-1", "") // open: no end
	own := x.deleteS1()
	if end := x.settle(own); end.Status != "succeeded" {
		t.Fatalf("operation %+v, want succeeded", end)
	}
	if !slices.Contains(x.sops.f.tried(), open) {
		t.Errorf("the open operation %s lost no workflow: asked %v", open, x.sops.f.tried())
	}
	x.wantGone(append(slices.Clone(sc.victims), open)...)
	// Its work, still running or repeated, finds no record and ends nothing.
	_ = x.op.Do(context.Background(), open)
	_ = x.op.End(context.Background(), open, gen.OperationStatusSucceeded, "")
	x.wantGone(open)
	if slices.Contains(x.records(), open) {
		t.Errorf("a record of %s came back: %v", open, x.records())
	}
}
