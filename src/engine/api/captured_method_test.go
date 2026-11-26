package api

// Feature 011, T018 (US3; FR-011 to FR-014; data model "States", "Proof" and "Runs that continue";
// contract "Engine API: /captured-patterns", withdrawCapturedPattern, eraseCapturedPattern,
// startRun, replayRun): the method rule, withdrawal and erasure of a captured pattern, through HTTP
// and the generated contract types, on the engine rig of captured_startrun_test.go (srRig: the real
// run workflow, faked step workers and checker, no model). Written before the code (T020) by a test
// author; the code author must not change it (ADR 0029).
//
// A captured pattern is made the way a client makes it (srRig.capture, prove, approve, a later slice
// made through the slice API). A change of the engine's method is simulated as T013 and T006 do:
// every recorded proof is given another method (srRig.setProofMethod), which is how the engine's
// method change looks to a captured pattern. A withdrawn captured pattern is reached through
// withdrawCapturedPattern ("api") and, for the refusals that already exist (a stub answers
// not-implemented until T020), also by planting `state: withdrawn` in the record ("planted"), so
// that the refusals are judged apart from the withdrawal itself.
//
// How erasure is observed through the API, and where the API has no reader: the captured pattern
// (getCapturedPattern and listCapturedPatterns), its proof slice (getSlice 404) and the proof runs
// (getRun 404), the runs of other slices (getRun, listRuns, still naming it), the replay (404). The
// template artifacts have no API reader, so they are read from the engine's store the way
// captured_proof_slice_test.go does (psTemplateBytes): an artifact is held by the slice that stored
// it and by a captured pattern (store/captured.go), so "the artifacts only it holds" are the ones no
// slice stores any more and no other captured pattern holds.
//
// Edge cases checked: the method rule (a new passing proof under the new method makes it usable and
// records the method; a failing one names the step and check; the latest proof decides: a later
// failing proof, and a proof still running); withdrawal from proposed, proven and approved, a
// run started before it finishing on the captured pattern, a run that has ended keeping its
// record, an unnamed start not using it and not naming it, a named one refused, withdrawal again
// (changes nothing, the record equal), every call of the state table's withdrawn column (parameters,
// proof slice, approval, a proof's start and replay, a replay of a run built from it) refused with
// nothing started, withdrawn before every later refusal (fit, workers, active run, project exists,
// proof required); the 403 of a worker and a page token with nothing changed; 404 of an unknown and
// of an erased captured pattern; withdrawal and erasure Busy while another operation of the captured
// pattern runs; erasure from every state; erasure Busy while a run built from it, a proof's run or
// a failed proof's run waiting for the developer has not ended, and allowed once it has; the proof
// slice and its runs erased, other slices' runs and the source slice kept; an erased ID never given
// again; a replay of a run built from an erased one; source slice erasure keeping the template, its
// use and a new proof; artifacts another captured pattern or slice also holds kept, the rest
// removed.
//
// Left out, and why: idempotency keys of the two calls (the generic rule of the front, tested for
// every mutating call elsewhere); a restart in the middle of an erasure (operations_restart_test.go's
// seam); a malformed captured pattern ID (the front's path rule, the same for every /{id} call);
// concurrent withdrawals (one operation at a time is the Busy case here); the CLI (T019).

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/api/gen"
)

// ---- calls and observations ----

func (s *srRig) mcWithdrawCall(id string) reply {
	return s.do("POST", apiPath("/captured-patterns/"+id+"/withdrawal"), "")
}

func (s *srRig) mcEraseCall(id string) reply {
	return s.do("DELETE", apiPath("/captured-patterns/"+id), "")
}

// mcAccepted checks the 202 of a captured pattern's operation and follows it to a success.
func (s *srRig) mcAccepted(r reply, kind, id string) {
	s.t.Helper()
	op := s.opBody(r, 202)
	if string(op.Kind) != kind || op.Target != id {
		s.t.Fatalf("operation kind/target = %s/%s, want %s/%s", op.Kind, op.Target, kind, id)
	}
	if got := s.waitOp(op.Id); got.Status != "succeeded" {
		s.t.Fatalf("%s of %s ended %+v", kind, id, got)
	}
}

func (s *srRig) mcWithdraw(id string) {
	s.t.Helper()
	s.mcAccepted(s.mcWithdrawCall(id), "withdraw-captured-pattern", id)
}

func (s *srRig) mcErase(id string) {
	s.t.Helper()
	s.mcAccepted(s.mcEraseCall(id), "erase-captured-pattern", id)
}

// mcPlantWithdrawn makes the record `withdrawn`, as T013's setProofMethod plants a method.
func (s *srRig) mcPlantWithdrawn(id string) {
	s.t.Helper()
	b, err := s.st.CapturedPattern(id)
	if err != nil {
		s.t.Fatal(err)
	}
	rec := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &rec); err != nil {
		s.t.Fatal(err)
	}
	rec["state"] = json.RawMessage(`"withdrawn"`)
	out, _ := json.Marshal(rec)
	if err := s.st.PutCapturedPattern(id, out, nil); err != nil {
		s.t.Fatal(err)
	}
}

// mcWithdrawBy withdraws through the API ("api") or by planting the state ("planted").
func (s *srRig) mcWithdrawBy(mode, id string) {
	s.t.Helper()
	if mode == "planted" {
		s.mcPlantWithdrawn(id)
		return
	}
	s.mcWithdraw(id)
	if got := s.captured(id); got.State != gen.CapturedPatternStateWithdrawn || got.Usable {
		s.t.Fatalf("after withdrawal: state %s usable %v, want withdrawn and not usable", got.State, got.Usable)
	}
}

var mcModes = []string{"api", "planted"}

// mcDeleteSlice deletes a slice that has no active run and waits until it is gone.
func (s *srRig) mcDeleteSlice(id string) {
	s.t.Helper()
	op := s.opBody(s.do("DELETE", spURL(id, ""), ""), 202)
	if got := s.waitOp(op.Id); got.Status != "succeeded" {
		s.t.Fatalf("deleteSlice %s ended %+v", id, got)
	}
	for end := time.Now().Add(5 * time.Second); s.do("GET", spURL(id, ""), "").Code != 404; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(end) {
			s.t.Fatalf("slice %s is still readable 5 s after its deletion succeeded", id)
		}
	}
}

func (s *srRig) mcWantDetailNaming(p gen.Problem, id string) {
	s.t.Helper()
	if p.Detail == nil || !strings.Contains(*p.Detail, id) {
		s.t.Errorf("the problem's detail %v does not name %s", p.Detail, id)
	}
}

// mcWantGone checks that no reader finds the captured pattern.
func (s *srRig) mcWantGone(id string) {
	s.t.Helper()
	s.do("GET", apiPath("/captured-patterns/"+id), "").problem(s.t, 404, gen.ErrorCodeNotFound)
	r := s.do("GET", apiPath("/captured-patterns"), "")
	if r.Code != 200 || strings.Contains(r.Body.String(), `"`+id+`"`) {
		s.t.Errorf("listCapturedPatterns = %d, naming %s still: %s", r.Code, id, r.Body.String())
	}
}

// mcLastProof is the newest proof of the captured pattern.
func (s *srRig) mcLastProof(id string) gen.Proof {
	s.t.Helper()
	cp := s.captured(id)
	if len(cp.Proofs) == 0 {
		s.t.Fatalf("captured pattern %s has no proof", id)
	}
	return cp.Proofs[len(cp.Proofs)-1]
}

// mcLater makes a later slice (the analyst's next proposal is a tasks slice) with its fill-step
// workers registered.
func (s *srRig) mcLater() string {
	s.t.Helper()
	later := s.addSlice("Let me keep tasks.", true)
	s.registerSteps(later, cpFillOnly)
	return later
}

func (s *srRig) mcGate() (release func()) {
	gate := make(chan struct{})
	s.set(func() { s.gate = gate })
	done := false
	return func() {
		if !done {
			done = true
			s.set(func() { s.gate = nil })
			close(gate)
		}
	}
}

// ---- the method rule (FR-011) ----

// T018, FR-011 (a regression guard for what T013 and T015 hold: the skip and ProofRequired after a
// method change): after the engine's method changed, a new passing proof under the new method makes
// the captured pattern usable again and records that method; the proofs before it stay.
func TestMCANewPassingProofUnderTheNewMethodMakesItUsableAndRecordsTheMethod(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.mcLater()
	id := s.approved("notes")
	current := s.mcLastProof(id).Method // the proof just made ran under the engine's method
	before := len(s.captured(id).Proofs)

	s.setProofMethod(id, cmMethodB) // the engine's method changed: the recorded proof is under another
	if got := s.captured(id); got.Usable || got.State != gen.CapturedPatternStateApproved {
		t.Fatalf("setup: state %s usable %v, want approved and not usable", got.State, got.Usable)
	}
	s.refused(later, "tasks-app", id, 409, gen.ErrorCodeProofRequired)

	s.prove(id, "proof-new-method")
	got := s.captured(id)
	p := got.Proofs[len(got.Proofs)-1]
	if len(got.Proofs) != before+1 || p.Outcome != gen.ProofOutcomePassed || p.Method != current || p.Method == cmMethodB {
		t.Errorf("the new proof = %+v among %d, want a passed one under the engine's method %s after %d", p, len(got.Proofs), current, before)
	}
	if !got.Usable || got.State != gen.CapturedPatternStateApproved {
		t.Errorf("state %s usable %v after the new proof, want approved and usable", got.State, got.Usable)
	}
	calls := s.w.callCount()
	op := s.startRun(later, "tasks-app", id) // named: no ProofRequired now
	s.wantChoice(op.Target, id+"@"+string(got.Version), gen.PatternReasonChosen, srWant(got, srTaskRepl), got.Source.Run)
	s.waitEnded(op.Target, "done")
	s.noAgentCalls(calls, "the run after the new proof")
}

// T018, FR-011: after a method change a failing proof under the new method leaves the captured
// pattern unusable and names what failed (the step and the check); the state stays approved; a
// later passing proof makes it usable.
func TestMCAFailingProofUnderTheNewMethodLeavesItUnusableNamingWhatFailed(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.mcLater()
	id := s.approved("notes")
	current := s.mcLastProof(id).Method
	s.setProofMethod(id, cmMethodB)

	s.set(func() { s.failCheck = "tests/TestsBuild" })
	bad := s.proofRun(id, "proof-bad")
	s.waitStatus(bad.Target, "waiting-for-developer")
	s.opBody(s.do("POST", apiPath("/operations/"+bad.Id+"/cancel"), ""), 202)
	s.waitEnded(bad.Target, "cancelled")
	s.set(func() { s.failCheck = "" })

	got := s.captured(id)
	p := got.Proofs[len(got.Proofs)-1]
	if p.Run != bad.Target || p.Outcome != gen.ProofOutcomeFailed || p.Method != current {
		t.Fatalf("the proof = %+v, want run %s failed under the engine's method %s", p, bad.Target, current)
	}
	if len(p.Failures) == 0 || p.Failures[0].Step == nil || *p.Failures[0].Step != "tests" ||
		p.Failures[0].Check == nil || *p.Failures[0].Check != "TestsBuild" {
		t.Errorf("the failures = %+v, want the step tests and the check TestsBuild named", p.Failures)
	}
	if got.Usable || got.State != gen.CapturedPatternStateApproved {
		t.Errorf("state %s usable %v after a failing proof, want approved and not usable", got.State, got.Usable)
	}
	pr := s.refused(later, "tasks-app", id, 409, gen.ErrorCodeProofRequired)
	s.mcWantDetailNaming(pr, id)

	s.prove(id, "proof-good")
	if got := s.captured(id); !got.Usable || got.State != gen.CapturedPatternStateApproved {
		t.Errorf("state %s usable %v after a passing proof, want approved and usable", got.State, got.Usable)
	}
}

// T018, FR-011 (a regression guard for T013's case): under the current method the latest proof
// decides. A later failing proof makes an approved captured pattern unusable; a proof still running
// does too (fail closed) and reads `running`; a new passing proof makes it usable again.
func TestMCTheLatestProofDecidesUsableAFailingOneAndARunningOne(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.mcLater()
	id := s.approved("notes")
	if !s.captured(id).Usable {
		t.Fatalf("setup: not usable after approval")
	}

	s.set(func() { s.failCheck = "tests/TestsBuild" })
	bad := s.proofRun(id, "proof-bad")
	s.waitStatus(bad.Target, "waiting-for-developer")
	s.opBody(s.do("POST", apiPath("/operations/"+bad.Id+"/cancel"), ""), 202)
	s.waitEnded(bad.Target, "cancelled")
	s.set(func() { s.failCheck = "" })
	if got := s.captured(id); got.Usable || s.mcLastProof(id).Outcome != gen.ProofOutcomeFailed {
		t.Errorf("after a failing proof: usable %v, latest outcome %s, want not usable and failed", got.Usable, s.mcLastProof(id).Outcome)
	}
	s.refused(later, "tasks-app", id, 409, gen.ErrorCodeProofRequired)

	release := s.mcGate()
	defer release()
	running := s.proofRun(id, "proof-running")
	s.waitStatus(running.Target, "running")
	if got := s.captured(id); got.Usable || s.mcLastProof(id).Outcome != gen.ProofOutcomeRunning {
		t.Errorf("while a proof runs: usable %v, latest outcome %s, want not usable and running", got.Usable, s.mcLastProof(id).Outcome)
	}
	s.refused(later, "tasks-three", id, 409, gen.ErrorCodeProofRequired)
	release()
	s.waitEnded(running.Target, "done")
	if got := s.captured(id); !got.Usable || s.mcLastProof(id).Outcome != gen.ProofOutcomePassed {
		t.Errorf("after the proof passed: usable %v, latest outcome %s, want usable and passed", got.Usable, s.mcLastProof(id).Outcome)
	}
}

// ---- withdrawal (FR-012) ----

// T018, FR-012: withdrawal stops the captured pattern's choice by new runs: an unnamed start uses the
// base pattern and does not name it in not_used (a withdrawn one is judged only when named), a named
// use answers 409 CapturedPatternWithdrawn naming it and starts nothing; the state is `withdrawn`
// and not usable, in every state it was withdrawn from.
func TestMCWithdrawalStopsItsChoiceAndANamedUseAnswersWithdrawn(t *testing.T) {
	for _, from := range []string{"proposed", "proven", "approved"} {
		t.Run(from, func(t *testing.T) {
			s := newSR(t, srTasks(t))
			later := s.mcLater()
			var id string
			switch from {
			case "proposed":
				id = s.capture("notes")
			case "proven":
				id = s.capture("notes")
				s.prove(id, "proof-notes")
			default:
				id = s.approved("notes")
			}
			if got := s.captured(id).State; string(got) != from {
				t.Fatalf("setup: state %s, want %s", got, from)
			}
			s.mcWithdraw(id)
			got := s.captured(id)
			if got.State != gen.CapturedPatternStateWithdrawn || got.Usable {
				t.Errorf("after withdrawal: state %s usable %v, want withdrawn and not usable", got.State, got.Usable)
			}
			p := s.refused(later, "tasks-app", id, 409, gen.ErrorCodeCapturedPatternWithdrawn)
			s.mcWantDetailNaming(p, id)
			op := s.startRun(later, "tasks-two", "") // unnamed: the base pattern, the withdrawn one not named
			s.wantChoice(op.Target, s.getRunView(op.Target).Pattern, gen.PatternReasonBase, nil, "")
			if strings.Contains(s.getRunView(op.Target).Pattern, id) {
				t.Errorf("the run uses %s: a withdrawn captured pattern is never chosen", s.getRunView(op.Target).Pattern)
			}
			s.wantNotUsed(op.Target)
			s.waitEnded(op.Target, "done")
		})
	}
}

// T018, FR-012: a run already started on the captured pattern finishes on it (also when it is
// withdrawn while the run is running, which is not Busy: only another of its operations is), and the
// records of earlier runs still name it.
func TestMCARunAlreadyStartedFinishesOnItAndEarlierRecordsStillNameIt(t *testing.T) {
	s := newSR(t, srTasks(t), srProposal(t, srSwap("chore", "chores")))
	one := s.mcLater()
	two := s.addSlice("Let me keep chores.", true)
	s.registerSteps(two, cpFillOnly)
	id := s.approved("notes")
	cp := s.captured(id)
	ref := id + "@" + string(cp.Version)
	tasks, chores := srWant(cp, srTaskRepl), srWant(cp, map[string]string{"note": "chore", "notes": "chores"})

	earlier := s.startRun(one, "tasks-earlier", "")
	s.waitEnded(earlier.Target, "done")
	calls := s.w.callCount()

	release := s.mcGate()
	defer release()
	held := s.startRun(two, "chores-held", "")
	s.waitStatus(held.Target, "running")
	s.mcWithdraw(id) // accepted and done while a run built from it is running
	if st := s.getRunView(held.Target).Status; st != "running" {
		t.Errorf("the run is %q after the withdrawal, want it still running", st)
	}
	release()
	s.waitEnded(held.Target, "done")
	s.wantChoice(held.Target, ref, gen.PatternReasonFits, chores, cp.Source.Run)
	s.wantChoice(earlier.Target, ref, gen.PatternReasonFits, tasks, cp.Source.Run)
	s.noAgentCalls(calls, "the runs started before the withdrawal")
	for _, r := range []string{earlier.Target, held.Target} {
		if v := s.getRunView(r); v.Pattern != ref {
			t.Errorf("run %s names %s after the withdrawal, want %s", r, v.Pattern, ref)
		}
	}
}

// T018, FR-012, the state table's withdrawn column, and the refusals that follow from it: every call
// answers 409 CapturedPatternWithdrawn and starts nothing (no run, no operation, no worker call) and
// changes nothing: replaceParameters (not CapturedPatternApproved, though it was approved),
// makeProofSlice, approveCapturedPattern, a proof's startRun, a proof run's replayRun, a replay of a
// run built from it, and a named startRun; withdrawing again changes nothing. "planted" is the
// regression guard for the refusals that exist before T020.
func TestMCAWithdrawnCapturedPatternRefusesEveryCallAndStartsNothing(t *testing.T) {
	for _, mode := range mcModes {
		t.Run(mode, func(t *testing.T) {
			s := newSR(t, srTasks(t))
			later := s.mcLater()
			id := s.approved("notes")
			cp := s.captured(id)
			proofSlice, proofRun := string(*cp.ProofSlice), cp.Proofs[0].Run
			built := s.startRun(later, "tasks-app", "")
			s.waitEnded(built.Target, "done")

			s.mcWithdrawBy(mode, id)
			before := s.captured(id)
			ops, runs, calls := s.opCount(), s.runCount(), s.w.callCount()

			s.mcWantDetailNaming(s.refused(later, "tasks-two", id, 409, gen.ErrorCodeCapturedPatternWithdrawn), id)
			s.refused(proofSlice, "proof-two", "", 409, gen.ErrorCodeCapturedPatternWithdrawn)
			s.mcWantDetailNaming(s.do("POST", apiPath("/runs/"+built.Target+"/replay"), `{"project":"replay-one"}`).
				problem(t, 409, gen.ErrorCodeCapturedPatternWithdrawn), id)
			s.refusedReplay(built.Target, "replay-two", 409, gen.ErrorCodeCapturedPatternWithdrawn)
			s.refusedReplay(string(proofRun), "proof-replay", 409, gen.ErrorCodeCapturedPatternWithdrawn)
			s.do("PUT", apiPath("/captured-patterns/"+id+"/parameters"), `{"parameters":[]}`).
				problem(t, 409, gen.ErrorCodeCapturedPatternWithdrawn)
			s.do("POST", apiPath("/captured-patterns/"+id+"/proof-slice"), "").problem(t, 409, gen.ErrorCodeCapturedPatternWithdrawn)
			s.do("POST", apiPath("/captured-patterns/"+id+"/approval"), "").problem(t, 409, gen.ErrorCodeCapturedPatternWithdrawn)

			if s.opCount() != ops || s.runCount() != runs || s.w.callCount() != calls {
				t.Errorf("the refused calls started something: operations %d->%d, runs %d->%d, worker calls %d->%d",
					ops, s.opCount(), runs, s.runCount(), calls, s.w.callCount())
			}
			if now := s.captured(id); !reflect.DeepEqual(now, before) {
				t.Errorf("a refused call changed the captured pattern:\n%+v\nwant %+v", now, before)
			}
			if mode == "api" { // withdrawing a withdrawn one changes nothing
				s.mcWithdraw(id)
				if now := s.captured(id); !reflect.DeepEqual(now, before) {
					t.Errorf("withdrawal again changed the captured pattern:\n%+v\nwant %+v", now, before)
				}
			}
		})
	}
}

// T018, FR-012, the state table: a proposed or proven captured pattern, withdrawn, answers
// CapturedPatternWithdrawn (not ProofRequired, not a parameters change) to approval and to a
// parameters change, and its proof slice is still refused.
func TestMCWithdrawnFromProposedOrProvenRefusesApprovalAndParameters(t *testing.T) {
	s := newSR(t)
	proposed := s.capture("alpha")
	proven := s.capture("beta")
	s.prove(proven, "proof-beta")
	for _, id := range []string{proposed, proven} {
		s.mcWithdraw(id)
		s.do("POST", apiPath("/captured-patterns/"+id+"/approval"), "").problem(t, 409, gen.ErrorCodeCapturedPatternWithdrawn)
		s.do("PUT", apiPath("/captured-patterns/"+id+"/parameters"), `{"parameters":[]}`).problem(t, 409, gen.ErrorCodeCapturedPatternWithdrawn)
		s.do("POST", apiPath("/captured-patterns/"+id+"/proof-slice"), "").problem(t, 409, gen.ErrorCodeCapturedPatternWithdrawn)
		if got := s.captured(id).State; got != gen.CapturedPatternStateWithdrawn {
			t.Errorf("%s is %s after the refused calls, want withdrawn", id, got)
		}
	}
}

// T018, FR-012, the contract's order: a withdrawn named captured pattern answers
// CapturedPatternWithdrawn before every later refusal: ProofRequired (its proof is under another
// method), PatternDoesNotFit (the slice differs), WorkersNotReady, RunActive and ProjectExists.
// Each is a start that would be refused for that reason were it not withdrawn.
func TestMCAWithdrawnNamedCapturedPatternAnswersWithdrawnBeforeTheRest(t *testing.T) {
	for _, mode := range mcModes {
		t.Run(mode, func(t *testing.T) {
			s := newSR(t, srDiffers(t), srTasks(t), srTasks(t), srProposal(t, srSwap("task", "tasks"), srReplace("shall list tasks", "shall list busy items")))
			differs := s.addSlice("Let me keep tasks and list every item.", true)
			noWorkers := s.addSlice("Tasks without workers.", true)
			ready := s.addSlice("Let me keep tasks.", true)
			s.registerSteps(ready, cpFillOnly)
			busySlice := s.addSlice("A busy slice.", true)
			registerAll(t, s.r, busySlice)
			id := s.approved("notes")
			s.mcWithdrawBy(mode, id)
			s.setProofMethod(id, cmMethodB) // also needs a proof

			const W = gen.ErrorCodeCapturedPatternWithdrawn
			s.refused(ready, "app-proof", id, 409, W)       // ProofRequired
			s.refused(differs, "app-fit", id, 409, W)       // PatternDoesNotFit
			s.refused(noWorkers, "app-workers", id, 409, W) // WorkersNotReady
			s.refused(ready, "src-notes", id, 409, W)       // ProjectExists

			release := s.mcGate() // RunActive
			defer release()
			busy := s.startRun(busySlice, "busy-app", cpBase)
			s.waitStatus(busy.Target, "running")
			s.refused(ready, "app-active", id, 409, W)
			release()
			s.waitEnded(busy.Target, "done")
		})
	}
}

// ---- who may withdraw and erase ----

// T018, FR-012, FR-013 and Principle V: withdrawCapturedPattern and eraseCapturedPattern with a
// worker or a page token answer 403 Forbidden and change nothing (no operation, the captured
// pattern equal, its proof slice and runs still there).
func TestMCWithdrawAndEraseWithAWorkerOrPageTokenAre403AndChangeNothing(t *testing.T) {
	s := newSR(t)
	id := s.approved("notes")
	before := s.captured(id)
	ops, runs := s.opCount(), s.runCount()
	for _, tok := range []string{workerToken, pageToken} {
		for _, do := range []func() reply{
			func() reply { return s.r.do("POST", apiPath("/captured-patterns/"+id+"/withdrawal"), tok, "", nil) },
			func() reply { return s.r.do("DELETE", apiPath("/captured-patterns/"+id), tok, "", nil) },
		} {
			s.r.clock.Advance(80 * time.Millisecond)
			do().problem(t, 403, gen.ErrorCodeForbidden)
		}
	}
	if now := s.captured(id); !reflect.DeepEqual(now, before) {
		t.Errorf("a refused call changed the captured pattern:\n%+v\nwant %+v", now, before)
	}
	if s.opCount() != ops || s.runCount() != runs {
		t.Errorf("refused calls created operations or runs: operations %d->%d, runs %d->%d", ops, s.opCount(), runs, s.runCount())
	}
	if r := s.do("GET", spURL(string(*before.ProofSlice), ""), ""); r.Code != 200 {
		t.Errorf("the proof slice after the refused calls: getSlice = %d", r.Code)
	}
}

// T018, contract: an unknown captured pattern is 404 NotFound for both calls, and so is an erased one.
func TestMCWithdrawAndEraseOfAnUnknownOrErasedCapturedPatternAre404(t *testing.T) {
	s := newSR(t)
	s.mcWithdrawCall(cpBase+"-nope").problem(t, 404, gen.ErrorCodeNotFound)
	s.mcEraseCall(cpBase+"-nope").problem(t, 404, gen.ErrorCodeNotFound)
	id := s.capture("notes")
	s.mcErase(id)
	ops := s.opCount()
	s.mcWithdrawCall(id).problem(t, 404, gen.ErrorCodeNotFound)
	s.mcEraseCall(id).problem(t, 404, gen.ErrorCodeNotFound)
	if s.opCount() != ops {
		t.Errorf("the refused calls created operations")
	}
}

// T018, contract: withdrawal and erasure answer 409 CapturedPatternBusy at once while another of the
// captured pattern's operations runs (the operation held open as captured_proof_slice_test.go holds
// one), and work once it has ended.
func TestMCWithdrawAndEraseWhileAnotherOperationRunsAreBusy(t *testing.T) {
	p := newPSRig(t)
	id, _ := p.psCaptureOK("notes", "src-notes")
	entered, release := p.lint.arm()
	defer release()
	p.psInFlight(entered, "POST", "/captured-patterns/"+id+"/proof-slice", "")
	first := p.do("GET", apiPath("/captured-patterns/"+id), "")
	p.psAs(clientToken, "POST", "/captured-patterns/"+id+"/withdrawal", "").problem(t, 409, gen.ErrorCodeCapturedPatternBusy)
	p.psAs(clientToken, "DELETE", "/captured-patterns/"+id, "").problem(t, 409, gen.ErrorCodeCapturedPatternBusy)
	if now := p.do("GET", apiPath("/captured-patterns/"+id), ""); now.Body.String() != first.Body.String() {
		t.Errorf("a refused call changed the captured pattern")
	}
	release()
	var r reply
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if r = p.psAs(clientToken, "POST", "/captured-patterns/"+id+"/withdrawal", ""); r.Code != 409 || time.Now().After(end) {
			break
		}
	}
	if got := p.psAccepted(r, "withdraw-captured-pattern", id); got.Status != "succeeded" {
		t.Fatalf("withdrawal after the operation ended: %+v", got)
	}
	if got := p.psGetCaptured(id).State; got != "withdrawn" {
		t.Errorf("state %s, want withdrawn", got)
	}
}

// ---- erasure (FR-013) ----

// T018, FR-013: erasing removes the captured pattern, its proof slice and that slice's runs (its
// proofs), and keeps the records of the runs of other slices built from it, which keep naming it,
// and the source slice and its runs; its ID is never given again (a new capture under it answers
// CapturedPatternExists); then an unnamed start uses the base pattern, a named one is NotFound, and
// a replay of a run built from it answers NotFound naming it.
func TestMCEraseRemovesThePatternItsProofSliceAndProofRunsAndKeepsOtherRunsRecords(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.mcLater()
	id := s.approved("notes")
	s.prove(id, "proof-two") // two proofs, both erased with the proof slice
	cp := s.captured(id)
	ref := id + "@" + string(cp.Version)
	proofSlice := string(*cp.ProofSlice)
	var proofRuns []string
	for _, p := range cp.Proofs {
		proofRuns = append(proofRuns, string(p.Run))
	}
	if len(proofRuns) != 2 {
		t.Fatalf("setup: %d proofs, want 2", len(proofRuns))
	}
	built := s.startRun(later, "tasks-app", "")
	s.waitEnded(built.Target, "done")
	sourceRun := cp.Source.Run

	s.mcErase(id)

	s.mcWantGone(id)
	s.do("GET", spURL(proofSlice, ""), "").problem(t, 404, gen.ErrorCodeNotFound)
	for _, r := range proofRuns {
		s.do("GET", apiPath("/runs/"+r), "").problem(t, 404, gen.ErrorCodeNotFound)
	}
	listed := map[string]bool{}
	for _, it := range s.listRuns("").Items {
		listed[it.ID] = true
	}
	for _, r := range proofRuns {
		if listed[r] {
			t.Errorf("listRuns still lists the proof run %s", r)
		}
	}
	if !listed[built.Target] || !listed[sourceRun] {
		t.Errorf("listRuns = %v, want the run %s built from it and the source run %s kept", listed, built.Target, sourceRun)
	}
	v := s.getRunView(built.Target)
	if v.Pattern != ref || v.Parameters == nil || v.SourceRun == nil || *v.SourceRun != sourceRun {
		t.Errorf("the run built from it = pattern %s, parameters %v, source %v; want it to keep naming %s and %s", v.Pattern, v.Parameters, v.SourceRun, ref, sourceRun)
	}
	if ev := s.rawEvents(built.Target); len(ev) == 0 || ev[0]["pattern"] != ref {
		t.Errorf("the events of the run built from it do not name %s: %v", ref, ev)
	}
	if r := s.do("GET", spURL("S-1", ""), ""); r.Code != 200 {
		t.Errorf("the source slice after the erasure: getSlice = %d, want 200", r.Code)
	}

	// no use of it, and its ID is never given again
	op := s.startRun(later, "tasks-two", "")
	if got := s.getRunView(op.Target).Pattern; strings.Contains(got, id) {
		t.Errorf("a run uses %s after the erasure", got)
	}
	s.wantNotUsed(op.Target)
	s.waitEnded(op.Target, "done")
	s.refused(later, "tasks-three", id, 404, gen.ErrorCodeNotFound)
	s.mcWantDetailNaming(s.do("POST", apiPath("/runs/"+built.Target+"/replay"), `{"project":"replay-one"}`).problem(t, 404, gen.ErrorCodeNotFound), id)
	s.refusedReplay(built.Target, "replay-two", 404, gen.ErrorCodeNotFound)
	again := s.startRun("S-1", "src-again", cpBase)
	s.waitEnded(again.Target, "done")
	s.do("POST", apiPath("/captured-patterns"), `{"run":"`+again.Target+`","name":"notes"}`).problem(t, 409, gen.ErrorCodeCapturedPatternExists)
}

// T018, FR-013: erasure works from every state (proposed with no proof slice, proven, approved and
// withdrawn), and every time the ID is held for good.
func TestMCEraseWorksFromEveryStateAndHoldsTheID(t *testing.T) {
	s := newSR(t)
	proposed := s.capture("alpha")
	proven := s.capture("beta")
	s.prove(proven, "proof-beta")
	approved := s.approved("gamma")
	withdrawn := s.approved("delta")
	s.mcWithdraw(withdrawn)
	for _, id := range []string{proposed, proven, approved, withdrawn} {
		s.mcErase(id)
		s.mcWantGone(id)
		again := s.startRun("S-1", "src-again-"+strings.TrimPrefix(id, cpBase+"-"), cpBase)
		s.waitEnded(again.Target, "done")
		s.do("POST", apiPath("/captured-patterns"), `{"run":"`+again.Target+`","name":"`+strings.TrimPrefix(id, cpBase+"-")+`"}`).
			problem(t, 409, gen.ErrorCodeCapturedPatternExists)
	}
}

// T018, FR-013 and the data model: erasure answers 409 CapturedPatternBusy at once while a run built
// from it has not ended, while its proof's run has not ended, and while a failed proof's run waits
// for the developer; it changes nothing, and works once the run has ended.
func TestMCEraseWhileARunBuiltFromItOrItsProofHasNotEndedIsBusy(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.mcLater()
	id := s.approved("notes")
	cp := s.captured(id)
	proofSlice := string(*cp.ProofSlice)

	refuse := func(what string) {
		t.Helper()
		before, ops := s.captured(id), s.opCount()
		s.mcEraseCall(id).problem(t, 409, gen.ErrorCodeCapturedPatternBusy)
		if now := s.captured(id); !reflect.DeepEqual(now, before) || s.opCount() != ops {
			t.Errorf("%s: the refused erasure changed something", what)
		}
		if r := s.do("GET", spURL(proofSlice, ""), ""); r.Code != 200 {
			t.Errorf("%s: the proof slice is gone after the refused erasure (getSlice %d)", what, r.Code)
		}
	}

	// a run built from it, running
	release := s.mcGate()
	defer release()
	built := s.startRun(later, "tasks-held", "")
	s.waitStatus(built.Target, "running")
	refuse("a run built from it is running")
	release()
	s.waitEnded(built.Target, "done")

	// a proof's run, running
	release = s.mcGate()
	defer release()
	proof := s.proofRun(id, "proof-held")
	s.waitStatus(proof.Target, "running")
	refuse("a proof's run is running")
	release()
	s.waitEnded(proof.Target, "done")

	// a failed proof's run waits for the developer: not ended
	s.set(func() { s.failCheck = "tests/TestsBuild" })
	bad := s.proofRun(id, "proof-bad")
	s.waitStatus(bad.Target, "waiting-for-developer")
	s.set(func() { s.failCheck = "" })
	refuse("a failed proof's run waits for the developer")
	s.opBody(s.do("POST", apiPath("/operations/"+bad.Id+"/cancel"), ""), 202)
	s.waitEnded(bad.Target, "cancelled")

	s.mcErase(id)
	s.mcWantGone(id)
	s.do("GET", spURL(proofSlice, ""), "").problem(t, 404, gen.ErrorCodeNotFound)
	s.do("GET", apiPath("/runs/"+bad.Target), "").problem(t, 404, gen.ErrorCodeNotFound)
}

// T018, FR-013 and spec edge case: erasing the source slice keeps the template artifacts (the
// captured pattern holds them), the captured pattern's state and use (a later slice still uses it
// with no agent step) and its source naming; a new proof after the erasure passes and counts.
func TestMCErasingTheSourceSliceKeepsTheTemplateAndItsUseAndANewProofPasses(t *testing.T) {
	s := newSR(t, srTasks(t))
	later := s.mcLater()
	id := s.approved("notes")
	cp := s.captured(id)
	want := map[string][]byte{}
	for k, h := range map[string]string{"tests": string(cp.Template.Tests), "code": string(cp.Template.Code), "witnesses": string(cp.Template.Witnesses)} {
		b, err := s.st.Artifact(h)
		if err != nil {
			t.Fatalf("setup: template %s: %v", k, err)
		}
		want[k] = b
	}

	s.mcDeleteSlice("S-1")
	s.do("GET", apiPath("/runs/"+cp.Source.Run), "").problem(t, 404, gen.ErrorCodeNotFound)

	for k, h := range map[string]string{"tests": string(cp.Template.Tests), "code": string(cp.Template.Code), "witnesses": string(cp.Template.Witnesses)} {
		b, err := s.st.Artifact(h)
		if err != nil || string(b) != string(want[k]) {
			t.Errorf("template %s after the source slice's erasure: %v, bytes kept %v", k, err, string(b) == string(want[k]))
		}
	}
	got := s.captured(id)
	if got.State != gen.CapturedPatternStateApproved || !got.Usable || got.Source.Run != cp.Source.Run || got.Source.Slice != cp.Source.Slice || got.Template != cp.Template {
		t.Errorf("after the source slice's erasure: state %s usable %v source %+v template %+v, want them unchanged", got.State, got.Usable, got.Source, got.Template)
	}
	calls := s.w.callCount()
	op := s.startRun(later, "tasks-app", "")
	s.wantChoice(op.Target, id+"@"+string(cp.Version), gen.PatternReasonFits, srWant(cp, srTaskRepl), cp.Source.Run)
	s.waitEnded(op.Target, "done")
	s.noAgentCalls(calls, "the run after the source slice's erasure")

	s.prove(id, "proof-after")
	if p := s.mcLastProof(id); p.Outcome != gen.ProofOutcomePassed || len(s.captured(id).Proofs) != len(cp.Proofs)+1 {
		t.Errorf("the proof after the erasure = %+v among %d, want a passed one after %d", p, len(s.captured(id).Proofs), len(cp.Proofs))
	}
}

// T018, FR-013: erasure removes the template artifacts only the captured pattern holds and keeps the
// others. An artifact a slice stored stays while that slice exists; one that another captured
// pattern holds stays while it does; once nothing holds it, it is gone.
func TestMCEraseRemovesTheArtifactsOnlyItHoldsAndKeepsTheRest(t *testing.T) {
	s := newSR(t)
	one := s.capture("one") // both are captured from S-1's runs: equal template files
	two := s.capture("two")
	templates := func(id string) map[string]string {
		c := s.captured(id)
		return map[string]string{"tests": string(c.Template.Tests), "code": string(c.Template.Code), "witnesses": string(c.Template.Witnesses)}
	}
	if !reflect.DeepEqual(templates(one), templates(two)) {
		t.Fatalf("setup: the two captured patterns hold different artifacts: %v and %v", templates(one), templates(two))
	}
	held := templates(one)
	wantReadable := func(what string, readable bool) {
		t.Helper()
		for k, h := range held {
			if _, err := s.st.Artifact(h); (err == nil) != readable {
				t.Errorf("%s: template %s readable = %v (%v), want %v", what, k, err == nil, err, readable)
			}
		}
	}

	s.mcErase(one) // the source slice still stores them, and the other holds them
	wantReadable("after erasing one, with the source slice and the other", true)

	three := s.capture("three")
	s.mcDeleteSlice("S-1") // now only captured patterns hold them
	wantReadable("after the source slice's erasure", true)
	s.mcErase(two)
	wantReadable("after erasing two, three still holds them", true)
	s.mcErase(three)
	wantReadable("after erasing the last holder", false)
}
