package machine_test

// T009 (feature 006, US1; FR-003, FR-004, FR-008; research R3): the reuse key covers the method
// identity, and the events that carry a reuse_key name it. Written before the code, from the spec
// and the contracts (method README, run-event.schema.json `method`).
//
// ASSUMED SHAPE (until the code exists these do not compile):
//
//	machine.KeyParts gains `Method string` (the identity's hex): written into the key after the
//	  round, only when not empty, so an empty Method gives today's key.
//	machine.Event gains `Method string` (json "method,omitempty").
//	machine.Config gains `Method method.ID` (the zero ID is none).

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/method"
)

const (
	methodA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	methodB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func mustMethod(t *testing.T, s string) method.ID {
	t.Helper()
	id, err := method.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// rmFix is a fixture over the witness pattern (an open review round is possible), its machine
// built with the given identity (the zero ID is none) over a shared results store.
func rmFix(t *testing.T, results *machine.Results, id method.ID, findings func(int) []machine.Finding) *orfix {
	t.Helper()
	x := newOR(t, witnessPatternOf(), results, findings)
	x.m = machine.New(machine.Config{Pattern: x.p, Worker: loggedWorker{x.w, x.log}, Checker: x.c, Clock: x.clk,
		Events: x.sk, Results: x.results, Model: orModel, History: x.h, Sleep: x.h.sleep, Method: id})
	return x
}

func rmParts() machine.KeyParts {
	return machine.KeyParts{PatternVersion: "1.0.0", Step: "code",
		Inputs: []string{hash("a"), hash("b")}, AgentDigest: digest, Model: "claude-haiku-5-5"}
}

func TestMethodChangesTheKey(t *testing.T) {
	without := machine.ReuseKey(rmParts())
	a, b := rmParts(), rmParts()
	a.Method, b.Method = methodA, methodB
	ka, kb := machine.ReuseKey(a), machine.ReuseKey(b)
	if ka == without || kb == without || ka == kb {
		t.Fatalf("keys: none %s, A %s, B %s; want three different keys", without, ka, kb)
	}
	if ka != machine.ReuseKey(a) {
		t.Fatal("a key with a method is not deterministic")
	}
}

// Pinned before the change, computed from the framing of today's ReuseKey (not from the code
// under test): the key of rmParts() with no method, and with round 2.
func TestEmptyMethodGivesTodaysKey(t *testing.T) {
	const today = "86156c4d89ee14f266b09812c0b6575f6d6da49bb3e9c0c44c5376578e6846a3"
	const todayRound2 = "14fe7ac5d1ce6a11e6c62d8a51febbcf7688d310b5a3aff53cccb94505f05483"
	p := rmParts()
	p.Method = ""
	if got := machine.ReuseKey(p); got != today {
		t.Errorf("key with an empty method = %s, want today's %s", got, today)
	}
	p.Round = 2
	if got := machine.ReuseKey(p); got != todayRound2 {
		t.Errorf("key with round 2 and an empty method = %s, want today's %s", got, todayRound2)
	}
}

func TestKeyWithMethodDiffersFromEveryKeyWithoutOne(t *testing.T) {
	var without, with []string
	for round := 0; round <= 3; round++ {
		p := rmParts()
		p.Round = round
		without = append(without, machine.ReuseKey(p))
		p.Method = methodA
		with = append(with, machine.ReuseKey(p))
		p.Method = methodB
		with = append(with, machine.ReuseKey(p))
	}
	for i, k := range with {
		if slices.Contains(without, k) {
			t.Errorf("key %d with a method equals a key without one: %s", i, k)
		}
	}
	all := slices.Concat(without, with)
	seen := map[string]bool{}
	for _, k := range all {
		if seen[k] {
			t.Errorf("key %s is given by two different parts", k)
		}
		seen[k] = true
	}
}

// ownKey is the key an event's own fields give, as a reader would recompute it.
func ownKey(e machine.Event) string {
	p := machine.KeyParts{PatternVersion: e.Pattern, Step: e.Step, Inputs: e.Inputs, AgentDigest: e.AgentDigest,
		Method: e.Method}
	for _, s := range witnessPatternOf().Steps { // the model is part of the key of an agent step only
		if s.Name == e.Step && strings.HasPrefix(s.Worker, "agent:") {
			p.Model = orModel
		}
	}
	if e.Step != "review" {
		return machine.ReuseKey(p)
	}
	p.Round = e.Round
	return machine.ReuseKey(p)
}

var keyedKinds = []string{"AttemptMade", "StepAccepted", "ResultReused"}

// keyed are the events that carry a reuse_key: those kinds and the StepRejected of an open round.
func keyed(e machine.Event) bool {
	return slices.Contains(keyedKinds, e.Kind) || isOpenRejected(e)
}

func TestEventsNameTheMethodTheirKeyCovers(t *testing.T) {
	results := machine.NewResults()
	one := rmFix(t, results, mustMethod(t, methodA), round1Open)
	assertEnded(t, one.startIn("RUN-1", "proj-a"), one.ev, "done")
	two := rmFix(t, results, mustMethod(t, methodA), round1Open)
	assertEnded(t, two.startIn("RUN-2", "proj-b"), two.ev, "done")

	for name, x := range map[string]*orfix{"RUN-1": one, "RUN-2": two} {
		var nKeyed, nOpen int
		for _, e := range x.ev.events {
			if !keyed(e) {
				if e.Method != "" {
					t.Errorf("%s: %s carries method %q, want none", name, e.Kind, e.Method)
				}
				continue
			}
			nKeyed++
			if e.Kind == "StepRejected" || (e.Kind == "ResultReused" && e.OpenRound) {
				nOpen++
			}
			if e.Method != methodA {
				t.Errorf("%s: %s %s round %d carries method %q, want %q", name, e.Kind, e.Step, e.Round, e.Method, methodA)
			}
			if isOpenRejected(e) { // carries no pattern or inputs: its key is its attempt's, whose fields give it
				i := slices.IndexFunc(x.ev.events, func(m machine.Event) bool {
					return m.Kind == "AttemptMade" && m.Step == e.Step && m.Round == e.Round && m.Attempt == e.Attempt
				})
				if i < 0 || x.ev.events[i].ReuseKey != e.ReuseKey || x.ev.events[i].Method != e.Method {
					t.Errorf("%s: open StepRejected round %d: key %s method %q differ from its AttemptMade's", name, e.Round, e.ReuseKey, e.Method)
				}
				continue
			}
			if want := ownKey(e); e.ReuseKey != want {
				t.Errorf("%s: %s %s round %d: reuse_key %s, want the key of its own fields %s", name, e.Kind, e.Step, e.Round, e.ReuseKey, want)
			}
		}
		if nKeyed == 0 || nOpen == 0 {
			t.Errorf("%s: setup: %d keyed events, %d open-round events; want both", name, nKeyed, nOpen)
		}
		validate(t, x.ev.events)
	}
	if two.ev.count("ResultReused") == 0 || len(two.w.calls) != 0 {
		t.Fatalf("setup: RUN-2 reused %d results with %d worker calls", two.ev.count("ResultReused"), len(two.w.calls))
	}
	if one.ev.count("AttemptMade") == 0 || one.ev.count("StepAccepted") == 0 {
		t.Fatalf("setup: RUN-1 events %v", one.ev.kinds())
	}
}

func TestResultIsReusedOnlyUnderItsMethod(t *testing.T) {
	a, b := mustMethod(t, methodA), mustMethod(t, methodB)
	results := machine.NewResults()
	one := rmFix(t, results, a, round1Open)
	assertEnded(t, one.startIn("RUN-1", "proj-a"), one.ev, "done")
	stored := results.Len()

	// Under A again: every step and round is reused, the worker is not called.
	same := rmFix(t, results, a, round1Open)
	assertEnded(t, same.startIn("RUN-2", "proj-a"), same.ev, "done")
	if len(same.w.calls) != 0 || same.ev.count("StepAccepted") != 0 || same.ev.count("ResultReused") == 0 {
		t.Fatalf("under A: %d worker calls, events %v; want only reuse", len(same.w.calls), same.ev.kinds())
	}
	if results.Len() != stored {
		t.Errorf("a reuse under A changed the store: %d results, want %d", results.Len(), stored)
	}

	// Under B: nothing is reused, every step runs, and its result is kept under B's keys.
	other := rmFix(t, results, b, round1Open)
	assertEnded(t, other.startIn("RUN-3", "proj-a"), other.ev, "done")
	if other.ev.count("ResultReused") != 0 {
		t.Fatalf("under B: %d results reused from A", other.ev.count("ResultReused"))
	}
	for _, s := range witnessPatternOf().Steps {
		if len(other.w.callsTo(s.Name)) == 0 {
			t.Errorf("under B: step %s was not run", s.Name)
		}
	}
	if results.Len() != 2*stored {
		t.Errorf("results = %d, want %d: B's results beside A's", results.Len(), 2*stored)
	}
	for _, e := range other.ev.events {
		if keyed(e) && e.Method != methodB {
			t.Errorf("under B: %s carries method %q", e.Kind, e.Method)
		}
	}
	validate(t, other.ev.events)

	// Under B again: now B's results are reused.
	again := rmFix(t, results, b, round1Open)
	assertEnded(t, again.startIn("RUN-4", "proj-a"), again.ev, "done")
	if len(again.w.calls) != 0 {
		t.Errorf("under B again: %d worker calls, want 0", len(again.w.calls))
	}
}

// A result recorded without a method (an engine before the feature, or no identity) is not
// reused under a method, and a method's result is not reused without one.
func TestResultWithoutMethodIsNotReusedUnderOne(t *testing.T) {
	results := machine.NewResults()
	none := rmFix(t, results, method.ID{}, round1Open)
	assertEnded(t, none.startIn("RUN-1", "proj-a"), none.ev, "done")
	for _, e := range none.ev.events {
		if e.Method != "" {
			t.Errorf("no method configured: %s carries method %q", e.Kind, e.Method)
		}
		if keyed(e) && !isOpenRejected(e) && e.ReuseKey != ownKey(e) { // an open StepRejected has its attempt's key
			t.Errorf("no method configured: %s %s round %d: key %s is not today's key %s", e.Kind, e.Step, e.Round, e.ReuseKey, ownKey(e))
		}
		if b, _ := json.Marshal(e); strings.Contains(string(b), `"method"`) {
			t.Errorf("no method configured: %s JSON has a method: %s", e.Kind, b)
		}
	}
	validate(t, none.ev.events)

	under := rmFix(t, results, mustMethod(t, methodA), round1Open)
	assertEnded(t, under.startIn("RUN-2", "proj-a"), under.ev, "done")
	if under.ev.count("ResultReused") != 0 {
		t.Fatalf("under A: %d results reused from a run recorded without a method", under.ev.count("ResultReused"))
	}
	for _, s := range witnessPatternOf().Steps {
		if len(under.w.callsTo(s.Name)) == 0 {
			t.Errorf("under A: step %s was not run", s.Name)
		}
	}

	// The reverse: a machine without an identity still reuses its own kind of result (today's behaviour).
	plain := rmFix(t, results, method.ID{}, round1Open)
	assertEnded(t, plain.startIn("RUN-3", "proj-a"), plain.ev, "done")
	if len(plain.w.calls) != 0 || plain.ev.count("ResultReused") == 0 {
		t.Errorf("no method again: %d worker calls, %d reused; want today's reuse", len(plain.w.calls), plain.ev.count("ResultReused"))
	}
}

// A stored result whose key was computed with another method but the same other parts is not
// found: seeded by hand, under B's key of the first step, and looked up under A.
func TestSeededResultUnderAnotherMethodIsNotFound(t *testing.T) {
	results := machine.NewResults()
	p := witnessPatternOf()
	first := p.Steps[0]
	in := []string{hash("contract"), hash("rules")}
	parts := machine.KeyParts{PatternVersion: orPattern, Step: first.Name, Inputs: in}
	parts.Method = methodB
	results.Put(machine.Accepted{Key: machine.ReuseKey(parts), Artifacts: []string{hash("x"), hash("y")}, Run: "RUN-0"})

	x := rmFix(t, results, mustMethod(t, methodA), func(int) []machine.Finding { return nil })
	assertEnded(t, x.startIn("RUN-1", "proj-a"), x.ev, "done")
	if len(x.w.callsTo(first.Name)) == 0 {
		t.Fatalf("step %s reused a result stored under method B while A is current", first.Name)
	}
	for _, e := range x.ev.events {
		if e.Kind == "ResultReused" {
			t.Errorf("%s reused under A", e.Step)
		}
	}

	// The same seed under A's key is found.
	parts.Method = methodA
	seeded := machine.NewResults()
	seeded.Put(machine.Accepted{Key: machine.ReuseKey(parts), Artifacts: []string{hash("x"), hash("y")}, Run: "RUN-0"})
	y := rmFix(t, seeded, mustMethod(t, methodA), func(int) []machine.Finding { return nil })
	y.startIn("RUN-2", "proj-a")
	if len(y.w.callsTo(first.Name)) != 0 || y.ev.count("ResultReused") == 0 {
		t.Fatalf("the seeded result under A's key was not reused: %v", y.ev.kinds())
	}
	if got := fmt.Sprint(y.ev.events[1].Method); y.ev.events[1].Kind == "ResultReused" && got != methodA {
		t.Errorf("ResultReused method = %q, want %q", got, methodA)
	}
}
