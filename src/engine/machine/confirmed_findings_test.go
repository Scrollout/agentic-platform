package machine_test

// T096 (owner, 2026-11-15; research D39 and D36, ADR 0042; data-model "Evidence-backed finding"):
// only a finding NoBlockingFindingOpen confirmed is recorded and used as blocking. Written before
// the code by a separate test author. Seams: open_round_reuse_test.go (orfix, newOR and helpers),
// open_round_resume_test.go (fixKeyOf), witness_test.go (witnessPatternOf), machine_test.go.
//
// ASSUMED SHAPE: machine.CheckResult gains `Confirmed []int`: the 0-based positions, in the
// findings the check was given (CheckRequest.Result.Findings), of the blocking findings
// NoBlockingFindingOpen confirmed. The machine records each finding of a review attempt with
// `blocking` true exactly when it is confirmed; a review round is open only when
// NoBlockingFindingOpen is the only failed check and it confirmed at least one finding; the fix,
// the next round, the hand-over, Run.Open and a kept open round use only the recorded blocking
// findings; the `findings` artifact stays the agent's answer as given.

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/machine"
)

// cfConfirmed is the claim the fake check confirms (rule R-1); cfUnconfirmed is a blocking claim
// with a witness that the fake check does not confirm (rule R-2).
func cfConfirmed() machine.Finding {
	f := blocking("R-1", "code/a.go", 10)
	f.Witness = &machine.Witness{File: witnessPath, Test: witnessName}
	return f
}

func cfUnconfirmed() machine.Finding {
	f := blocking("R-2", "code/b.go", 20)
	f.Witness = &machine.Witness{File: "review/r2/b_test.go", Test: "TestR2B"}
	return f
}

// asSuggestion is f as the record must hold it when it is not confirmed: only blocking differs.
func asSuggestion(f machine.Finding) machine.Finding {
	f.Blocking = false
	return f
}

// cfTwoClaims: review round 1 makes both claims, the confirmed one first; later rounds none.
func cfTwoClaims(round int) []machine.Finding {
	if round != 1 {
		return nil
	}
	return []machine.Finding{cfConfirmed(), cfUnconfirmed()}
}

// cfOnlyConfirmed: review round 1 makes only the claim that is confirmed.
func cfOnlyConfirmed(round int) []machine.Finding {
	if round != 1 {
		return nil
	}
	return []machine.Finding{cfConfirmed()}
}

// cfEveryRound: every round makes both claims.
func cfEveryRound(int) []machine.Finding { return []machine.Finding{cfConfirmed(), cfUnconfirmed()} }

// confirmRule is a NoBlockingFindingOpen that confirms, by position, the blocking findings of the
// rule: it fails with them as Confirmed when there is one, and passes with none otherwise. Every
// other check passes.
func confirmRule(rule string) func(machine.CheckRequest) (machine.CheckResult, error) {
	return func(r machine.CheckRequest) (machine.CheckResult, error) {
		if r.Name != "NoBlockingFindingOpen" {
			return pass(r), nil
		}
		confirmed := []int{}
		for i, f := range r.Result.Findings {
			if f.Blocking && f.Rule == rule {
				confirmed = append(confirmed, i)
			}
		}
		if len(confirmed) == 0 {
			res := pass(r)
			res.Confirmed = []int{}
			return res, nil
		}
		res := fail(r)
		res.Confirmed = confirmed
		return res, nil
	}
}

// fixCalls are the worker's calls to the fix (the code step after review round n).
func fixCalls(f *fixture, round int) []machine.Request {
	var out []machine.Request
	for _, c := range f.w.callsTo("code") {
		if c.Round == round {
			out = append(out, c)
		}
	}
	return out
}

func TestUnconfirmedBlockingClaimIsRecordedAsASuggestion(t *testing.T) {
	x := newOR(t, witnessPatternOf(), machine.NewResults(), cfTwoClaims)
	x.c.fn = confirmRule("R-1")
	assertEnded(t, x.startIn("RUN-1", "proj-a"), x.ev, "done")

	got := roundFindings(x.ev.events, 1)
	want := []machine.Finding{cfConfirmed(), asSuggestion(cfUnconfirmed())}
	if !findingsEqual(got, want) {
		t.Errorf("review round 1 recorded %+v, want the confirmed claim blocking and the unconfirmed one a suggestion: %+v", got, want)
	}

	fixes := fixCalls(x.fixture, 1)
	if len(fixes) == 0 {
		t.Fatalf("no fix ran after review round 1, which confirmed R-1: %v", x.ev.kinds())
	}
	for _, c := range fixes {
		if !findingsEqual(c.Open, []machine.Finding{cfConfirmed()}) {
			t.Errorf("the fix's request has open %+v, want only the confirmed claim %+v", c.Open, cfConfirmed())
		}
	}
	for _, c := range x.w.callsTo("review") {
		if c.Round == 2 && !findingsEqual(c.Open, []machine.Finding{cfConfirmed()}) {
			t.Errorf("review round 2 is given open %+v, want only the confirmed claim", c.Open)
		}
	}

	// The fix's key covers only the confirmed claim: it equals the fix's key in a run whose round 1
	// made the confirmed claim alone.
	alone := newOR(t, witnessPatternOf(), machine.NewResults(), cfOnlyConfirmed)
	alone.c.fn = confirmRule("R-1")
	assertEnded(t, alone.startIn("RUN-1", "proj-a"), alone.ev, "done")
	if got, want := fixKeyOf(t, x.ev.events, 1), fixKeyOf(t, alone.ev.events, 1); got != want {
		t.Errorf("the fix's reuse_key = %s, want %s, the key for the confirmed claim alone", got, want)
	}
	validate(t, x.ev.events)
}

func TestBlockingClaimWithoutAWitnessNeverReachesTheFix(t *testing.T) {
	claim := blocking("R-1", "code/f.go", 1)
	claim.Witness = nil // blocking: true, no witness
	x := newOR(t, witnessPatternOf(), machine.NewResults(), func(round int) []machine.Finding {
		if round != 1 {
			return nil
		}
		return []machine.Finding{claim}
	})
	x.c.fn = func(r machine.CheckRequest) (machine.CheckResult, error) {
		res := pass(r)
		if r.Name == "NoBlockingFindingOpen" {
			res.Reason = "none of the 1 blocking findings is confirmed by its witness test"
			res.Confirmed = []int{}
		}
		return res, nil
	}
	assertEnded(t, x.startIn("RUN-1", "proj-a"), x.ev, "done")

	accepted := false
	for _, e := range x.ev.events {
		if e.Kind == "StepAccepted" && e.Step == "review" && e.Round == 1 {
			accepted = true
		}
		if e.Kind == "StepRejected" && e.Step == "review" {
			t.Errorf("review round 1 was rejected (%q), want it accepted", e.Detail)
		}
	}
	if !accepted {
		t.Errorf("review round 1 was not accepted: %v", x.ev.kinds())
	}
	if got := roundFindings(x.ev.events, 1); !findingsEqual(got, []machine.Finding{asSuggestion(claim)}) {
		t.Errorf("review round 1 recorded %+v, want the claim as a suggestion %+v", got, asSuggestion(claim))
	}
	if n := len(x.w.callsTo("code")); n != 1 {
		t.Errorf("code ran %d times, want once: no fix runs for an unconfirmed claim", n)
	}
	if n := len(x.w.callsTo("review")); n != 1 {
		t.Errorf("review ran %d times, want one round", n)
	}
	validate(t, x.ev.events)
}

func TestHandOverNamesOnlyConfirmedFindings(t *testing.T) {
	p := witnessPatternOf()
	x := newOR(t, p, machine.NewResults(), cfEveryRound)
	x.c.fn = confirmRule("R-1")
	r := x.startIn("RUN-1", "proj-a")
	if r.Status != machine.StatusWaiting || x.ev.count("RunHandedOver") != 1 {
		t.Fatalf("run = %s, want handed over after %d open rounds: %v", r.Status, p.ReviewRounds, x.ev.kinds())
	}
	if !strings.Contains(r.Reason, "R-1") || !strings.Contains(r.Reason, "code/a.go") {
		t.Errorf("the hand-over reason %q does not name the confirmed finding R-1 at code/a.go", r.Reason)
	}
	if strings.Contains(r.Reason, "R-2") || strings.Contains(r.Reason, "code/b.go") {
		t.Errorf("the hand-over reason %q names the unconfirmed claim R-2 at code/b.go", r.Reason)
	}
	ho := eventsOf(x.ev.events, "RunHandedOver")[0]
	if ho.Detail != r.Reason {
		t.Errorf("RunHandedOver detail = %q, want the run's reason %q", ho.Detail, r.Reason)
	}
	want := []machine.Finding{cfConfirmed()}
	if !findingsEqual(r.Open, want) {
		t.Errorf("Run.Open = %+v, want only the confirmed finding %+v", r.Open, want)
	}
	if got, _ := x.m.Get("RUN-1"); !findingsEqual(got.Open, want) {
		t.Errorf("Get: Run.Open = %+v, want only the confirmed finding", got.Open)
	}
	for _, c := range x.w.callsTo("code") {
		if c.Round > 0 && !findingsEqual(c.Open, want) {
			t.Errorf("the fix of round %d is given %+v, want only the confirmed finding", c.Round, c.Open)
		}
	}
	// A restarted engine reads the run back from its record: Run.Open is the recorded blocking
	// findings, so still only the confirmed one.
	back, ok := x.build().Get("RUN-1")
	if !ok {
		t.Fatal("a restarted engine does not find RUN-1")
	}
	if back.Status != machine.StatusWaiting || !findingsEqual(back.Open, want) {
		t.Errorf("after a restart: status %s, Run.Open = %+v, want waiting and only the confirmed finding", back.Status, back.Open)
	}
	validate(t, x.ev.events)
}

func TestARoundWithNoConfirmedFindingIsNotOpen(t *testing.T) {
	claim := blocking("R-1", "code/f.go", 1)
	claim.Witness = nil // no witness: noBlocking fails and confirms nothing
	results := machine.NewResults()
	x := newOR(t, witnessPatternOf(), results, func(int) []machine.Finding { return []machine.Finding{claim} })
	x.c.fn = noBlocking
	r := x.startIn("RUN-1", "proj-a")
	if r.Status != machine.StatusWaiting {
		t.Fatalf("run = %s, want handed over after the review's attempts were rejected: %v", r.Status, x.ev.kinds())
	}

	rejected := 0
	for _, e := range x.ev.events {
		if e.Kind != "StepRejected" || e.Step != "review" {
			continue
		}
		rejected++
		if e.OpenRound {
			t.Errorf("review round %d attempt %d: StepRejected carries open_round, but no finding was confirmed", e.Round, e.Attempt)
		}
		if e.Detail == openDetail(e.Round) {
			t.Errorf("review round %d attempt %d: StepRejected says the round was left open", e.Round, e.Attempt)
		}
		if !strings.Contains(e.Detail, "NoBlockingFindingOpen") {
			t.Errorf("review round %d attempt %d: StepRejected detail %q, want the failed check named", e.Round, e.Attempt, e.Detail)
		}
	}
	if attempts := max(witnessPatternOf().Attempts, 1); rejected != attempts {
		t.Errorf("review attempts rejected = %d, want all %d of round 1", rejected, attempts)
	}
	for _, c := range x.w.callsTo("review") {
		if c.Round != 1 {
			t.Errorf("review round %d ran, want only round 1's attempts", c.Round)
		}
	}
	if n := len(x.w.callsTo("code")); n != 1 {
		t.Errorf("code ran %d times, want once: a round with no confirmed finding has no fix", n)
	}
	for _, e := range eventsOf(x.ev.events, "AttemptMade") {
		if e.Step != "review" {
			continue
		}
		if a, found := results.Get(e.ReuseKey); found && a.Open {
			t.Errorf("review round %d attempt %d kept an open result", e.Round, e.Attempt)
		}
	}
	if strings.Contains(r.Reason, "still open") {
		t.Errorf("the hand-over reason %q says findings are open; none was confirmed", r.Reason)
	}
	validate(t, x.ev.events)
}

func TestAReusedOpenRoundGivesTheFixOnlyConfirmedFindings(t *testing.T) {
	p := witnessPatternOf()
	results := machine.NewResults()
	one := newOR(t, p, results, cfTwoClaims)
	one.c.fn = confirmRule("R-1")
	assertEnded(t, one.startIn("RUN-1", "proj-a"), one.ev, "done")
	recorded := roundFindings(one.ev.events, 1)
	if !findingsEqual(recorded, []machine.Finding{cfConfirmed(), asSuggestion(cfUnconfirmed())}) {
		t.Fatalf("RUN-1 recorded %+v in round 1, want the confirmed claim blocking and the other a suggestion", recorded)
	}

	// The kept findings are the recorded ones.
	var openKey string
	for _, e := range one.ev.events {
		if isOpenRejected(e) && e.Round == 1 {
			openKey = e.ReuseKey
		}
	}
	if openKey == "" {
		t.Fatalf("RUN-1 did not leave review round 1 open: %v", one.ev.kinds())
	}
	kept, found := results.Get(openKey)
	if !found || !kept.Open {
		t.Fatalf("no open result kept under review round 1's key")
	}
	if !findingsEqual(kept.Findings, recorded) {
		t.Errorf("kept findings = %+v, want the recorded ones %+v", kept.Findings, recorded)
	}

	// A replay reuses the round: it records the kept findings, and its fix has RUN-1's key.
	two := newOR(t, p, results, cfTwoClaims)
	two.c.fn = confirmRule("R-1")
	assertEnded(t, two.startIn("RUN-2", "proj-b"), two.ev, "done")
	if got := roundFindings(two.ev.events, 1); !findingsEqual(got, recorded) {
		t.Errorf("RUN-2 recorded %+v for the reused round 1, want RUN-1's recorded findings %+v", got, recorded)
	}
	fixKey := fixKeyOf(t, one.ev.events, 1)
	var replayFix string
	for _, e := range two.ev.events {
		if e.Kind == "ResultReused" && e.Step == "code" && e.Round == 1 {
			replayFix = e.ReuseKey
		}
	}
	if replayFix != fixKey {
		t.Errorf("RUN-2's fix reuse_key = %q, want RUN-1's %s", replayFix, fixKey)
	}

	// A replay whose fix has no kept result runs the fix live, given the same confirmed list.
	noFix := machine.NewResults()
	for _, s := range settled(one.ev.events) {
		if s.step == "code" && s.round == 1 {
			continue
		}
		if a, ok := results.Get(s.key); ok {
			noFix.Put(a)
		}
	}
	three := newOR(t, p, noFix, cfTwoClaims)
	three.c.fn = confirmRule("R-1")
	assertEnded(t, three.startIn("RUN-3", "proj-c"), three.ev, "done")
	if !slices.ContainsFunc(three.ev.events, isOpenReused(1)) {
		t.Fatalf("setup: RUN-3 did not reuse review round 1: %v", three.ev.kinds())
	}
	fixes := fixCalls(three.fixture, 1)
	if len(fixes) == 0 {
		t.Fatalf("RUN-3 ran no fix after the reused round 1: %v", three.ev.kinds())
	}
	for _, c := range fixes {
		if !findingsEqual(c.Open, []machine.Finding{cfConfirmed()}) {
			t.Errorf("RUN-3's fix is given %+v, want only the confirmed claim %+v", c.Open, cfConfirmed())
		}
	}
	if got := fixKeyOf(t, three.ev.events, 1); got != fixKey {
		t.Errorf("RUN-3's fix reuse_key = %s, want RUN-1's %s", got, fixKey)
	}
	validate(t, two.ev.events)
	validate(t, three.ev.events)
}

func TestTheFindingsArtifactStaysTheAgentsAnswer(t *testing.T) {
	p := witnessPatternOf()
	results := machine.NewResults()
	x := newOR(t, p, results, cfTwoClaims)
	x.c.fn = confirmRule("R-1")
	findingsAt := slices.Index(p.Steps[len(p.Steps)-1].Outputs, "findings")
	live := x.w.fn["review"]
	var given []machine.Finding // the agent's answer in round 1, as the worker returned it
	var answer []byte
	x.w.fn["review"] = func(r machine.Request) (machine.Result, error) {
		res, err := live(r)
		if err != nil || r.Round != 1 {
			return res, err
		}
		b, jerr := json.Marshal(res.Findings)
		if jerr != nil {
			t.Fatal(jerr)
		}
		given, answer = res.Findings, slices.Clone(b)
		res.Outputs["findings"] = hash(string(b))
		res.Content = map[string][]byte{"findings": b}
		return res, nil
	}
	assertEnded(t, x.startIn("RUN-1", "proj-a"), x.ev, "done")
	if answer == nil {
		t.Fatal("setup: review round 1 did not run")
	}
	answerHash := hash(string(answer))

	// The answer as given: both claims blocking; it is not changed in place.
	var stored []machine.Finding
	if err := json.Unmarshal(answer, &stored); err != nil {
		t.Fatal(err)
	}
	if !findingsEqual(stored, []machine.Finding{cfConfirmed(), cfUnconfirmed()}) {
		t.Fatalf("setup: the answer is %+v, want both claims blocking", stored)
	}
	if !findingsEqual(given, []machine.Finding{cfConfirmed(), cfUnconfirmed()}) {
		t.Errorf("the worker's findings were changed in place: %+v, want the agent's answer with both claims blocking", given)
	}

	// The checks judge the claims as given.
	for _, c := range x.c.calls {
		if c.Step.Name == "review" && c.Round == 1 &&
			!findingsEqual(c.Result.Findings, []machine.Finding{cfConfirmed(), cfUnconfirmed()}) {
			t.Errorf("check %s was given %+v, want the agent's claims as given", c.Name, c.Result.Findings)
		}
	}

	// The findings artifact the record names, on the attempt, the open round and the kept result,
	// is the answer's hash.
	var made, rej *machine.Event
	for i, e := range x.ev.events {
		if e.Kind == "AttemptMade" && e.Step == "review" && e.Round == 1 {
			made = &x.ev.events[i]
		}
		if isOpenRejected(e) && e.Round == 1 {
			rej = &x.ev.events[i]
		}
	}
	if made == nil || rej == nil {
		t.Fatalf("setup: review round 1 was not left open: %v", x.ev.kinds())
	}
	if made.Artifacts[findingsAt] != answerHash {
		t.Errorf("AttemptMade's findings artifact = %s, want the answer's hash %s", made.Artifacts[findingsAt], answerHash)
	}
	if len(rej.Artifacts) <= findingsAt || rej.Artifacts[findingsAt] != answerHash {
		t.Errorf("the open StepRejected's artifacts = %v, want the answer's hash %s for findings", rej.Artifacts, answerHash)
	}
	if a, ok := results.Get(made.ReuseKey); !ok || len(a.Artifacts) <= findingsAt || a.Artifacts[findingsAt] != answerHash {
		t.Errorf("the kept result's artifacts = %v (found %v), want the answer's hash %s for findings", a.Artifacts, ok, answerHash)
	}

	// While the record holds the verdict.
	if got := roundFindings(x.ev.events, 1); !findingsEqual(got, []machine.Finding{cfConfirmed(), asSuggestion(cfUnconfirmed())}) {
		t.Errorf("review round 1 recorded %+v, want the unconfirmed claim with blocking false", got)
	}
	validate(t, x.ev.events)
}
