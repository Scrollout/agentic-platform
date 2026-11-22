package machine_test

// T010 (feature 006, US1; research R3): the reuse key covers the method identity, on the engine's
// side. A lookup is registered with the real durable.Activities (its LookupReuse and PutResult)
// over an in-memory result store; every other activity is the harness's fake (workflow_durable_test.go).
//
// THE INTERFACE (the smallest these tests need; the code author adds it):
//
//	durable.Activities.Method method.ID  // exists: the identity the engine runs under; zero = none
//	durable.Reused.Method string         // the method hex the returned Key was computed with
//	machine.KeyParts.Method string       // written into the key after the round, only when not empty
//	machine.Event.Method string          // `json:"method,omitempty"`
//
// Behaviour: with Activities.Method set, LookupReuse computes the key itself from the parts with
// that identity (ignoring any key the workflow passes), also with open findings and changed
// lines, and returns Method; the workflow records the returned key, inputs and method on the
// step's events whenever the returned Key is not empty, and otherwise keeps its own key.
//
// Cases (each a test below):
//   (a) TestLookupReuseComputesTheKeyUnderTheMethod: the key, inputs and Method, whatever key is passed
//   (b) TestLookupReuseFindsOnlyAResultKeptUnderTheSameMethod
//   (c) TestLookupReuseWithoutAMethodKeepsTodaysKey
//   (d) TestWorkflowRecordsTheKeyInputsAndMethodOnTheStepsEvents: events conform to the schema
//   (e) TestWorkflowReusesOnlyAResultAcceptedUnderTheSameMethod: A reused; B, none, and an older run without method are not
//   (f) TestWorkflowDoesNotReuseAnOpenRoundUnderAnotherMethod
//   (g) TestWorkflowResumedUnderANewMethodLooksUpTheRestUnderIt
//   (h) TestWorkflowFailedLookupLeavesTheMethodLessKey
//   (i) TestWorkflowWithoutAMethodKeepsTodaysKeysAndEvents
//
// Edge cases checked: a bogus or empty key passed with open findings; changed lines with and
// without a passed key; the review round in the key; a key computed under A passed while the
// activities run under B (B must not find it); a result kept under the method-less key looked up
// under A; the positive control (the same run under A is reused) for every "not reused" case; the
// open round's own event (open_round) naming the method; events without a reuse_key (RunStarted,
// StepStarted, RunInterrupted ...) carrying no method; the key recomputable from an event's own
// fields; a stop between two steps with the identity changing during it; a lookup failing on
// every call, with results still kept (under the method-less keys) and never reused.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/method"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
)

const mwPattern = "api-feature-go@1.0.0"

func mwID(t *testing.T, digit string) method.ID {
	t.Helper()
	id, err := method.Parse(strings.Repeat(digit, 64))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// mwArtifacts is an in-memory artifact store, which holds the findings' text by hash.
type mwArtifacts struct {
	mu sync.Mutex
	by map[string][]byte
}

func (s *mwArtifacts) PutArtifact(_ string, data []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.by == nil {
		s.by = map[string][]byte{}
	}
	h := sha256.Sum256(data)
	k := hex.EncodeToString(h[:])
	s.by[k] = slices.Clone(data)
	return k, nil
}

func (s *mwArtifacts) Artifact(hash string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.by[hash]
	if !ok {
		return nil, fmt.Errorf("no artifact %s", hash)
	}
	return b, nil
}

// mwStore puts a finding's text in the store and returns the finding by hash.
func mwStore(t *testing.T, s *mwArtifacts, f machine.Finding) durable.Finding {
	t.Helper()
	h, err := s.PutArtifact("S-1", []byte(wfJSON(t, f)))
	if err != nil {
		t.Fatal(err)
	}
	return durable.Finding{SHA256: h, Rule: f.Rule, File: f.File, Line: f.Line, Blocking: f.Blocking, Check: f.Check, Witness: f.Witness}
}

// ---- LookupReuse on its own ------------------------------------------------------------------

func mwParts() machine.KeyParts {
	return machine.KeyParts{PatternVersion: mwPattern, Step: "review", Inputs: []string{wfHash("i1"), wfHash("i2")},
		AgentDigest: wfDigest, Model: wfModel, Round: 2}
}

func TestLookupReuseComputesTheKeyUnderTheMethod(t *testing.T) {
	a, b := mwID(t, "a"), mwID(t, "b")
	arts := &mwArtifacts{}
	f := wfBlocking("R-1", 12)
	open := []durable.Finding{mwStore(t, arts, f)}
	changed := []machine.Line{{File: "internal/thing.go", Line: 12}, {File: "internal/thing.go", Line: 40}}
	withMethod := func(id method.ID) machine.KeyParts { p := mwParts(); p.Method = id.String(); return p }

	cases := []struct {
		name    string
		open    []durable.Finding
		changed []machine.Line
	}{
		{"plain", nil, nil},
		{"changed lines", nil, changed},
		{"open findings", open, nil},
		{"open findings and changed lines", open, changed},
	}
	for _, tc := range cases {
		var openM []machine.Finding
		if len(tc.open) > 0 {
			openM = []machine.Finding{f}
		}
		for _, id := range []method.ID{a, b} {
			wantIn, wantKey := machine.KeyOf(withMethod(id), openM, tc.changed)
			// The keys the workflow may pass: none (it cannot know the findings' text), a key
			// that is not one, and the method-less key it computes itself.
			noMethodIn, noMethodKey := machine.KeyOf(mwParts(), nil, tc.changed)
			_ = noMethodIn
			for _, passed := range []string{"", "bogus-key", noMethodKey} {
				t.Run(fmt.Sprintf("%s/%s.../passed %q", tc.name, id.String()[:1], passed), func(t *testing.T) {
					acts := &durable.Activities{Results: machine.NewResults(), Artifacts: arts, Method: id}
					got, err := acts.LookupReuse(context.Background(), durable.ReuseLookup{Key: passed, Slice: "S-1",
						Parts: mwParts(), Open: tc.open, Changed: tc.changed})
					if err != nil {
						t.Fatal(err)
					}
					if got.Key != wantKey {
						t.Errorf("Key = %s, want %s (the key under method %s)", got.Key, wantKey, id)
					}
					if !slices.Equal(got.Inputs, wantIn) {
						t.Errorf("Inputs = %v, want %v", got.Inputs, wantIn)
					}
					if got.Method != id.String() {
						t.Errorf("Method = %q, want %q", got.Method, id.String())
					}
					if got.Found {
						t.Errorf("Found on an empty store")
					}
				})
			}
		}
		// The two identities, and none, give three different keys.
		_, ka := machine.KeyOf(withMethod(a), openM, tc.changed)
		_, kb := machine.KeyOf(withMethod(b), openM, tc.changed)
		_, k0 := machine.KeyOf(mwParts(), openM, tc.changed)
		if ka == kb || ka == k0 || kb == k0 {
			t.Errorf("%s: the key does not depend on the method: %s %s %s", tc.name, ka, kb, k0)
		}
	}
}

func TestLookupReuseFindsOnlyAResultKeptUnderTheSameMethod(t *testing.T) {
	a, b := mwID(t, "a"), mwID(t, "b")
	arts := &mwArtifacts{}
	results := machine.NewResults()
	partsA := mwParts()
	partsA.Method = a.String()
	_, keyA := machine.KeyOf(partsA, nil, nil)
	_, keyNone := machine.KeyOf(mwParts(), nil, nil)
	out := []string{wfHash("out")}
	results.Put(machine.Accepted{Key: keyA, Artifacts: out, Run: "RUN-0"})
	// A result kept without a method (an older run), under a key no method-bearing lookup computes.
	results.Put(machine.Accepted{Key: keyNone, Artifacts: []string{wfHash("old")}, Run: "RUN-old"})

	look := func(id method.ID, passed string) durable.Reused {
		t.Helper()
		acts := &durable.Activities{Results: results, Artifacts: arts, Method: id}
		got, err := acts.LookupReuse(context.Background(), durable.ReuseLookup{Key: passed, Slice: "S-1", Parts: mwParts()})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	for _, passed := range []string{"", keyNone, keyA} {
		got := look(a, passed)
		if !got.Found || got.Key != keyA || !slices.Equal(got.Artifacts, out) || got.Run != "RUN-0" {
			t.Errorf("under A (passed %q): %+v, want RUN-0's result under %s", passed, got, keyA)
		}
		if got := look(b, passed); got.Found {
			t.Errorf("under B (passed %q): found %+v, want a result kept under A not to be", passed, got)
		}
	}
	// The result kept without a method is not found under A, though the workflow passes its key.
	if got := look(a, keyNone); got.Found && got.Run == "RUN-old" {
		t.Errorf("a result kept without a method was reused under A")
	}
}

func TestLookupReuseWithoutAMethodKeepsTodaysKey(t *testing.T) {
	arts := &mwArtifacts{}
	f := wfBlocking("R-1", 12)
	open := []durable.Finding{mwStore(t, arts, f)}
	changed := []machine.Line{{File: "internal/thing.go", Line: 12}}
	acts := &durable.Activities{Results: machine.NewResults(), Artifacts: arts}

	// The workflow's own key, without open findings: returned as given, no method.
	wantIn, wantKey := machine.KeyOf(mwParts(), nil, changed)
	got, err := acts.LookupReuse(context.Background(), durable.ReuseLookup{Key: wantKey, Slice: "S-1", Parts: mwParts(), Changed: changed})
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != wantKey || !slices.Equal(got.Inputs, wantIn) || got.Method != "" {
		t.Errorf("with the key passed: %+v, want key %s, inputs %v, no method", got, wantKey, wantIn)
	}
	// Open findings and no key: the key the activity computes, without a method.
	wantIn, wantKey = machine.KeyOf(mwParts(), []machine.Finding{f}, changed)
	got, err = acts.LookupReuse(context.Background(), durable.ReuseLookup{Slice: "S-1", Parts: mwParts(), Open: open, Changed: changed})
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != wantKey || !slices.Equal(got.Inputs, wantIn) || got.Method != "" {
		t.Errorf("with open findings: %+v, want key %s, inputs %v, no method", got, wantKey, wantIn)
	}
}

// ---- the workflow ------------------------------------------------------------------------------

// mwSwitch holds the activities a run's lookups and puts use, so a test can change the identity
// while the run is stopped (the engine restarted with a new build).
type mwSwitch struct {
	mu   sync.Mutex
	acts *durable.Activities
}

func (s *mwSwitch) get() *durable.Activities  { s.mu.Lock(); defer s.mu.Unlock(); return s.acts }
func (s *mwSwitch) set(a *durable.Activities) { s.mu.Lock(); s.acts = a; s.mu.Unlock() }

// mwRun is a workflow harness whose lookups and puts are the real durable.Activities.
type mwRun struct {
	*wfHarness
	sw         *mwSwitch
	arts       *mwArtifacts
	failLookup bool

	mu      sync.Mutex
	lookups []mwLookup
}

type mwLookup struct {
	Step string
	Got  durable.Reused
}

// mwNew makes a run RUN-<run> whose activities run under id (the zero ID: none), over the shared
// result and artifact stores. openRound makes review round 1 leave a blocking finding open.
func mwNew(t *testing.T, run string, id method.ID, results *machine.Results, arts *mwArtifacts, openRound bool) *mwRun {
	t.Helper()
	r := &mwRun{wfHarness: newWF(t, wfPattern()), sw: &mwSwitch{}, arts: arts}
	r.sw.set(&durable.Activities{Results: results, Artifacts: arts, Method: id})
	r.in.Run = run
	r.work = func(c durable.WorkerCall) (machine.Result, error) {
		res := wfOK(c)
		if openRound && c.Step.Name == "review" && c.Round == 1 {
			res.Findings = []machine.Finding{wfBlocking("R-1", 12)}
		}
		for _, f := range res.Findings { // the worker stores the findings' text
			if _, err := arts.PutArtifact(c.Slice, []byte(wfJSON(t, f))); err != nil {
				return machine.Result{}, err
			}
		}
		return res, nil
	}
	r.env.RegisterActivityWithOptions(func(ctx context.Context, l durable.ReuseLookup) (durable.Reused, error) {
		if r.failLookup {
			return durable.Reused{}, temporal.NewNonRetryableApplicationError("the store cannot be read", "ReuseFailed", nil)
		}
		got, err := r.sw.get().LookupReuse(ctx, l)
		if err == nil {
			r.mu.Lock()
			r.lookups = append(r.lookups, mwLookup{l.Parts.Step, got})
			r.mu.Unlock()
		}
		return got, err
	}, activity.RegisterOptions{Name: durable.LookupReuse, DisableAlreadyRegisteredCheck: true})
	r.env.RegisterActivityWithOptions(func(ctx context.Context, p durable.ReusePut) error {
		return r.sw.get().PutResult(ctx, p)
	}, activity.RegisterOptions{Name: durable.PutResult, DisableAlreadyRegisteredCheck: true})
	return r
}

func (r *mwRun) done() {
	r.t.Helper()
	r.run()
	r.wantOutcome(machine.OutcomeDone)
	wfValidate(r.t, r.recorded())
}

// recorded is the run's events as the real RecordEvent writes them: a FindingRecorded's finding,
// which travels by hash, put back from the artifact store.
func (r *mwRun) recorded() []machine.Event {
	r.t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wfHarness.mu.Lock()
	defer r.wfHarness.mu.Unlock()
	var out []machine.Event
	for _, rec := range r.records {
		e := rec.Event
		if rec.Finding != nil {
			b, err := r.arts.Artifact(rec.Finding.SHA256)
			if err != nil {
				r.t.Fatal(err)
			}
			var f machine.Finding
			if err := json.Unmarshal(b, &f); err != nil {
				r.t.Fatal(err)
			}
			f.Blocking = rec.Finding.Blocking
			e.Finding = &f
		}
		out = append(out, e)
	}
	return out
}

func (r *mwRun) workerCalled() int { return len(r.callsOf(durable.CallWorker)) }

// mwKeyKinds are the events that carry a reuse_key (data model; research R3).
func mwKeyed(e machine.Event) bool { return e.ReuseKey != "" }

// mwRecompute is the key an event's own fields give under a method ("" for none), for the kinds
// that carry the key's inputs.
func mwRecompute(t *testing.T, e machine.Event, id string) string {
	t.Helper()
	var step *machine.Step
	for i, s := range wfPattern().Steps {
		if s.Name == e.Step {
			step = &wfPattern().Steps[i]
		}
	}
	if step == nil {
		t.Fatalf("event %d names no step of the pattern: %q", e.Seq, e.Step)
	}
	p := machine.KeyParts{PatternVersion: mwPattern, Step: e.Step, Inputs: e.Inputs, AgentDigest: step.AgentDigest, Method: id}
	if strings.HasPrefix(step.Worker, "agent:") {
		p.Model = wfModel
	}
	if e.Step == "review" {
		p.Round = e.Round
	}
	return machine.ReuseKey(p)
}

// wantMethod checks every event of the run: one that carries a reuse_key names the method (id, or
// none when id is ""), the key is the one its own fields give under it, and no other event names one.
func (r *mwRun) wantMethod(id string) {
	r.t.Helper()
	keyed := 0
	for _, e := range r.events() {
		if !mwKeyed(e) {
			if e.Method != "" {
				r.t.Errorf("event %d (%s) has no reuse_key but names method %q", e.Seq, e.Kind, e.Method)
			}
			continue
		}
		keyed++
		if e.Method != id {
			r.t.Errorf("event %d (%s %s) names method %q, want %q", e.Seq, e.Kind, e.Step, e.Method, id)
		}
		switch e.Kind {
		case "AttemptMade", "StepAccepted", "ResultReused":
			if got := mwRecompute(r.t, e, id); got != e.ReuseKey {
				r.t.Errorf("event %d (%s %s): reuse_key %s, the event's own fields under %q give %s", e.Seq, e.Kind, e.Step, e.ReuseKey, id, got)
			}
			if id != "" && mwRecompute(r.t, e, "") == e.ReuseKey {
				r.t.Errorf("event %d (%s %s): the key does not cover the method", e.Seq, e.Kind, e.Step)
			}
		}
	}
	if keyed == 0 {
		r.t.Fatalf("no event carries a reuse_key: %v", r.kinds())
	}
}

func mwMethodInJSON(t *testing.T, e machine.Event) (string, bool) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(wfJSON(t, e)), &m); err != nil {
		t.Fatal(err)
	}
	v, ok := m["method"].(string)
	return v, ok
}

func TestWorkflowRecordsTheKeyInputsAndMethodOnTheStepsEvents(t *testing.T) {
	a := mwID(t, "a")
	for name, open := range map[string]bool{"a plain run": false, "a run with an open review round and a fix": true} {
		t.Run(name, func(t *testing.T) {
			r := mwNew(t, "RUN-1", a, machine.NewResults(), &mwArtifacts{}, open)
			r.done()
			r.wantMethod(a.String())
			// Every step's events (accepted, attempt made) carry it, and the event marshals it as `method`.
			for _, s := range wfPattern().Steps {
				n := 0
				for _, e := range r.eventsOf("StepAccepted") {
					if e.Step == s.Name {
						n++
						if m, ok := mwMethodInJSON(t, e); !ok || m != a.String() {
							t.Errorf("StepAccepted %s marshals method %q (present %v), want %s", s.Name, m, ok, a)
						}
					}
				}
				if n == 0 {
					t.Errorf("step %s was never accepted", s.Name)
				}
			}
			if open {
				var rej *machine.Event
				for _, e := range r.eventsOf("StepRejected") {
					if e.OpenRound {
						rej = &e
					}
				}
				if rej == nil || rej.Method != a.String() || rej.ReuseKey == "" {
					t.Fatalf("the StepRejected that leaves the round open = %+v, want key and method %s", rej, a)
				}
				// The fix's lookup had open findings, so the key was computed by the activity, under A.
				fix := false
				for _, l := range r.lookups {
					if l.Step == "code" && len(l.Got.Inputs) > 0 && l.Got.Method == a.String() && l.Got.Key != "" {
						fix = true
					}
				}
				if !fix {
					t.Errorf("no lookup of the fix returned a key and Method %s: %+v", a, r.lookups)
				}
			}
			// Every lookup returned the key and method of the identity the engine runs under.
			for _, l := range r.lookups {
				if l.Got.Method != a.String() || l.Got.Key == "" {
					t.Errorf("lookup of %s returned %+v, want Method %s", l.Step, l.Got, a)
				}
			}
		})
	}
}

func TestWorkflowReusesOnlyAResultAcceptedUnderTheSameMethod(t *testing.T) {
	a, b := mwID(t, "a"), mwID(t, "b")
	results, arts := machine.NewResults(), &mwArtifacts{}

	// An older run, without a method: its keys are the method-less ones.
	old := mwNew(t, "RUN-0", method.ID{}, results, arts, false)
	old.done()
	old.wantMethod("")
	if results.Len() == 0 {
		t.Fatal("the run without a method kept nothing")
	}
	// Under A nothing of it is reused: every step runs.
	first := mwNew(t, "RUN-1", a, results, arts, false)
	first.done()
	first.wantMethod(a.String())
	if n := len(first.eventsOf("ResultReused")); n != 0 {
		t.Fatalf("under A, %d steps were reused from a run without a method", n)
	}
	if first.workerCalled() == 0 {
		t.Fatal("under A no worker ran")
	}
	// Under A again: every step is reused, naming A, and no worker runs.
	again := mwNew(t, "RUN-2", a, results, arts, false)
	again.done()
	again.wantMethod(a.String())
	if n, want := len(again.eventsOf("ResultReused")), len(wfPattern().Steps); n != want {
		t.Fatalf("under A again, %d ResultReused, want %d: %v", n, want, again.kinds())
	}
	if n := again.workerCalled(); n != 0 {
		t.Fatalf("under A again, %d worker calls, want none", n)
	}
	// Under B, nothing accepted under A is reused: every step runs and names B.
	other := mwNew(t, "RUN-3", b, results, arts, false)
	other.done()
	other.wantMethod(b.String())
	if n := len(other.eventsOf("ResultReused")); n != 0 {
		t.Fatalf("under B, %d steps were reused from A", n)
	}
	for _, s := range wfPattern().Steps {
		if len(other.workerCalls(s.Name)) == 0 {
			t.Errorf("under B, step %s did not run", s.Name)
		}
	}
	// Under B again it is reused (B's own results), A's are still kept.
	backB := mwNew(t, "RUN-4", b, results, arts, false)
	backB.done()
	if n := len(backB.eventsOf("ResultReused")); n != len(wfPattern().Steps) {
		t.Fatalf("under B again, %d ResultReused, want %d", n, len(wfPattern().Steps))
	}
}

func TestWorkflowDoesNotReuseAnOpenRoundUnderAnotherMethod(t *testing.T) {
	a, b := mwID(t, "a"), mwID(t, "b")
	results, arts := machine.NewResults(), &mwArtifacts{}

	first := mwNew(t, "RUN-1", a, results, arts, true)
	first.done()
	first.wantMethod(a.String())
	if rounds := roundsOf(first.workerCalls("review")); len(rounds) < 2 {
		t.Fatalf("review ran in rounds %v, want an open round 1 and a round 2", rounds)
	}

	// Under A the open round is reused, its event naming A: the control for the case below.
	same := mwNew(t, "RUN-2", a, results, arts, true)
	same.done()
	same.wantMethod(a.String())
	openReused := 0
	for _, e := range same.eventsOf("ResultReused") {
		if e.Step == "review" && e.OpenRound {
			openReused++
			if e.Method != a.String() {
				t.Errorf("the reused open round names method %q, want %s", e.Method, a)
			}
		}
	}
	if openReused != 1 {
		t.Fatalf("under A, %d open rounds reused, want 1: %v", openReused, same.kinds())
	}
	if n := same.workerCalled(); n != 0 {
		t.Fatalf("under A again, %d worker calls, want none", n)
	}

	// Under B no step, and so not the open round, is reused: review round 1 runs again.
	other := mwNew(t, "RUN-3", b, results, arts, true)
	other.done()
	other.wantMethod(b.String())
	if n := len(other.eventsOf("ResultReused")); n != 0 {
		t.Fatalf("under B, %d steps were reused from A: %v", n, other.kinds())
	}
	if rounds := roundsOf(other.workerCalls("review")); len(rounds) < 2 || rounds[0] != 1 {
		t.Fatalf("under B review ran in rounds %v, want it to start with round 1 again", rounds)
	}
	for _, e := range other.eventsOf("StepRejected") {
		if e.OpenRound && e.Method != b.String() {
			t.Errorf("the open round under B names method %q, want %s", e.Method, b)
		}
	}
}

func TestWorkflowResumedUnderANewMethodLooksUpTheRestUnderIt(t *testing.T) {
	a, b := mwID(t, "a"), mwID(t, "b")
	results, arts := machine.NewResults(), &mwArtifacts{}
	r := mwNew(t, "RUN-1", a, results, arts, false)
	// The engine stops while the commit of `generate` is made (the next lookup, of `tests`, comes
	// after it) and comes back as a build with identity B.
	stopCommit := wfFirst(durable.CommitStep, 1, func(in any) bool { return in.(machine.CommitRequest).Step.Name == "generate" })
	r.stop = func(name string, in any) bool {
		if !stopCommit(name, in) {
			return false
		}
		r.sw.set(&durable.Activities{Results: results, Artifacts: arts, Method: b})
		return true
	}
	r.done()
	if n := r.stopPairs(); n != 1 {
		t.Fatalf("%d stop pairs, want 1: %v", n, r.kinds())
	}
	r.mu.Lock()
	lookups := slices.Clone(r.lookups)
	r.mu.Unlock()
	want := map[string]string{"contract-check": a.String(), "generate": a.String(), "tests": b.String(), "code": b.String(), "review": b.String()}
	seen := map[string]bool{}
	for _, l := range lookups {
		seen[l.Step] = true
		if l.Got.Method != want[l.Step] {
			t.Errorf("lookup of %s under %q, want %q", l.Step, l.Got.Method, want[l.Step])
		}
	}
	for step := range want {
		if !seen[step] {
			t.Errorf("step %s was never looked up", step)
		}
	}
	for _, e := range r.events() {
		if !mwKeyed(e) {
			if e.Method != "" {
				t.Errorf("event %d (%s) has no key but names method %q", e.Seq, e.Kind, e.Method)
			}
			continue
		}
		if e.Method != want[e.Step] {
			t.Errorf("event %d (%s %s) names method %q, want %q", e.Seq, e.Kind, e.Step, e.Method, want[e.Step])
		}
		if got := mwRecompute(t, e, want[e.Step]); e.Kind != "StepRejected" && got != e.ReuseKey {
			t.Errorf("event %d (%s %s): key %s, its own fields under %s give %s", e.Seq, e.Kind, e.Step, e.ReuseKey, want[e.Step], got)
		}
	}
	// The earlier steps' results are kept under A, the later under B: a run under B reuses only the latter.
	later := mwNew(t, "RUN-2", b, results, arts, false)
	later.done()
	for _, e := range later.eventsOf("ResultReused") {
		if e.Step == "contract-check" || e.Step == "generate" {
			t.Errorf("step %s accepted under A before the upgrade was reused under B", e.Step)
		}
	}
	if n := len(later.eventsOf("ResultReused")); n != 3 {
		t.Errorf("under B %d steps reused, want the 3 accepted after the upgrade", n)
	}
}

func TestWorkflowFailedLookupLeavesTheMethodLessKey(t *testing.T) {
	a := mwID(t, "a")
	results, arts := machine.NewResults(), &mwArtifacts{}
	r := mwNew(t, "RUN-1", a, results, arts, false)
	r.failLookup = true
	r.done()
	r.wantMethod("") // no event names a method, and each key is the one its own fields give without one
	if len(r.lookups) != 0 {
		t.Fatalf("lookups returned %v although every one failed", r.lookups)
	}
	if results.Len() == 0 {
		t.Fatal("nothing was kept: a failed lookup must still leave a key to keep the result under")
	}
	for _, e := range r.eventsOf("StepAccepted") {
		if _, ok := results.Get(e.ReuseKey); !ok {
			t.Errorf("the result of %s is not kept under its event's key", e.Step)
		}
	}
	// A later run under A, whose lookups work, does not reuse them.
	later := mwNew(t, "RUN-2", a, results, arts, false)
	later.done()
	later.wantMethod(a.String())
	if n := len(later.eventsOf("ResultReused")); n != 0 {
		t.Fatalf("%d steps reused from a run whose lookups failed", n)
	}
	if later.workerCalled() == 0 {
		t.Fatal("no worker ran under A")
	}
}

func TestWorkflowWithoutAMethodKeepsTodaysKeysAndEvents(t *testing.T) {
	for name, open := range map[string]bool{"a plain run": false, "an open round and a fix": true} {
		t.Run(name, func(t *testing.T) {
			results, arts := machine.NewResults(), &mwArtifacts{}
			r := mwNew(t, "RUN-1", method.ID{}, results, arts, open)
			r.done()
			r.wantMethod("")
			for _, l := range r.lookups {
				if l.Got.Method != "" {
					t.Errorf("lookup of %s returned Method %q without an identity", l.Step, l.Got.Method)
				}
			}
			for _, e := range r.events() {
				if _, ok := mwMethodInJSON(t, e); ok {
					t.Errorf("event %d (%s) marshals a method", e.Seq, e.Kind)
				}
			}
			// Reuse works as it did: a second run without a method reuses every step.
			again := mwNew(t, "RUN-2", method.ID{}, results, arts, open)
			again.done()
			if n := len(again.eventsOf("ResultReused")); n < len(wfPattern().Steps) {
				t.Fatalf("without a method %d steps reused, want at least %d", n, len(wfPattern().Steps))
			}
			again.wantMethod("")
		})
	}
}
