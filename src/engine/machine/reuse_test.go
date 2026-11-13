package machine_test

// T044: failing tests for reuse, resume, replay and run comparison (T051/T052). Additions to the
// interface stated in machine_test.go (smallest the tests need):
//
//	machine.ReuseKey(machine.KeyParts{PatternVersion, Step, Inputs []string (hashes), AgentDigest,
//	  Model}) string: deterministic; differs when any part differs (data-model.md, research D8).
//	machine.NewResults() *machine.Results: the accepted-result store, found by key. Config.Results
//	  holds it and Config.Model the model id used in the key (one model for the pattern's agents).
//	  Results.Get(key) (machine.Accepted{Key, Artifacts []string, Run}, bool); Results.Len() int.
//	Before running a step the machine looks up its key in Results: found, it appends ResultReused
//	  (Step, ReuseKey, Artifacts) and does not call the Worker; otherwise it runs the step and, on
//	  StepAccepted, stores the result. StepAccepted also carries ReuseKey and Artifacts.
//	Event gains fields Step, Attempt (already used), ReuseKey string, Artifacts []string.
//	Run.Status gains machine.StatusInterrupted: a step whose attempts all fail because the worker
//	  errors (e.g. model unavailable) interrupts the run (data-model.md, "A failing worker").
//	Machine.Resume(ctx, id) (Run, error) continues an interrupted run on the same machine without
//	  re-running accepted steps (no ResultReused is needed for them); ErrNotInterrupted if the run
//	  is not interrupted, ErrRunEnded if it ended.
//	Restart: the Events sink may also implement Read(run string) ([]Event, error) (optional
//	  interface machine.Reader, no new Config field). A machine built by machine.New over the same
//	  Events and Results treats a run whose last event is not terminal as interrupted; Resume(ctx,
//	  id) on it continues from the events without re-running any accepted step.
//	Replay is Start with a new run id, the same inputs and the same Results store; no new API.
//	machine.Compare(a, b []Event) []machine.Difference{Step, Field ("artifacts"|"checks"|
//	  "decision"), Left, Right string, "" when absent} lists only differing fields, in step then
//	  field order, as the contract's Difference schema. Left for artifacts is the hash (several
//	  joined by ","), for checks "passed" or "failed: <reason>".

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/agentic-platform/engine/machine"
)

func keyParts() machine.KeyParts {
	return machine.KeyParts{PatternVersion: "1.0.0", Step: "code",
		Inputs: []string{hash("a"), hash("b")}, AgentDigest: digest, Model: "claude-haiku-5-5"}
}

func TestReuseKeyDeterministicAndSensitive(t *testing.T) {
	base := machine.ReuseKey(keyParts())
	if base == "" || base != machine.ReuseKey(keyParts()) {
		t.Fatalf("key not deterministic: %q", base)
	}
	variants := map[string]func(p *machine.KeyParts){
		"pattern version": func(p *machine.KeyParts) { p.PatternVersion = "1.0.1" },
		"step":            func(p *machine.KeyParts) { p.Step = "review" },
		"input hash":      func(p *machine.KeyParts) { p.Inputs = []string{hash("a"), hash("c")} },
		"agent digest":    func(p *machine.KeyParts) { p.AgentDigest = hash("other") },
		"model":           func(p *machine.KeyParts) { p.Model = "claude-sonnet-5-5" },
	}
	seen := map[string]string{base: "base"}
	for name, mut := range variants {
		p := keyParts()
		mut(&p)
		k := machine.ReuseKey(p)
		if prev, dup := seen[k]; dup {
			t.Errorf("%s gives the same key as %s", name, prev)
		}
		seen[k] = name
	}
}

// reuseFixture is a fixture with a shared accepted-result store and a worker whose output hashes
// change on every call, so a re-run can never produce the same bytes by accident.
func reuseFixture(t *testing.T, results *machine.Results) *fixture {
	f := newFixture(t, pattern())
	n := 0
	for _, s := range pattern().Steps {
		name := s.Name
		f.w.fn[name] = func(r machine.Request) (machine.Result, error) {
			n++
			res := ok(r)
			for o := range res.Outputs {
				res.Outputs[o] = hash(fmt.Sprintf("%s/%s/call%d", name, o, n))
			}
			return res, nil
		}
	}
	f.c.fn = noBlocking
	f.m = machine.New(machine.Config{Pattern: pattern(), Worker: f.w, Checker: f.c, Clock: f.clk,
		Events: f.ev, Results: results, Model: "claude-haiku-5-5"})
	return f
}

func artifactsOf(ev *sink, kinds ...string) map[string][]string {
	out := map[string][]string{}
	for _, e := range ev.events {
		if slices.Contains(kinds, e.Kind) {
			out[e.Step] = e.Artifacts
		}
	}
	return out
}

func TestAcceptedResultIsReusedWithoutRunningTheStep(t *testing.T) {
	results := machine.NewResults()
	first := reuseFixture(t, results)
	assertEnded(t, first.start("RUN-1"), first.ev, "done")
	if results.Len() != 5 {
		t.Fatalf("results stored = %d, want 5", results.Len())
	}

	second := reuseFixture(t, results)
	r := second.start("RUN-2")
	assertEnded(t, r, second.ev, "done")
	if len(second.w.calls) != 0 {
		t.Fatalf("worker called %d times although every result was accepted", len(second.w.calls))
	}
	if second.ev.count("ResultReused") != 5 || second.ev.count("StepAccepted") != 0 {
		t.Fatalf("events = %v", second.ev.kinds())
	}
	for _, e := range second.ev.events {
		if e.Kind != "ResultReused" {
			continue
		}
		a, found := results.Get(e.ReuseKey)
		if e.ReuseKey == "" || !found || !slices.Equal(a.Artifacts, e.Artifacts) || a.Run != "RUN-1" {
			t.Errorf("ResultReused for %s: key %q found=%v artifacts=%v", e.Step, e.ReuseKey, found, e.Artifacts)
		}
	}
	validate(t, second.ev.events)
}

func TestChangedInputIsNotReused(t *testing.T) {
	results := machine.NewResults()
	reuseFixture(t, results).start("RUN-1")
	f := reuseFixture(t, results)
	p := f.params("RUN-2")
	p.Inputs["slice/rules"] = hash("rules changed")
	if _, err := f.m.Start(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"contract-check", "tests", "code", "review"} { // all read slice/rules, directly or not
		if len(f.w.callsTo(step)) == 0 {
			t.Errorf("%s was reused although its inputs changed", step)
		}
	}
}

func TestResumeContinuesWithoutRepeatingAcceptedSteps(t *testing.T) {
	results := machine.NewResults()
	f := reuseFixture(t, results)
	down := true
	inner := f.w.fn["code"]
	f.w.fn["code"] = func(r machine.Request) (machine.Result, error) {
		if down {
			return machine.Result{}, errors.New("model unavailable")
		}
		return inner(r)
	}
	r := f.start("RUN-1")
	if r.Status != machine.StatusInterrupted {
		t.Fatalf("status = %s, want interrupted", r.Status)
	}
	if !slices.Equal(r.Accepted, []string{"contract-check", "generate", "tests"}) {
		t.Fatalf("accepted = %v", r.Accepted)
	}
	before := len(f.w.calls)
	down = false
	r, err := f.m.Resume(context.Background(), "RUN-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEnded(t, r, f.ev, "done")
	for _, c := range f.w.calls[before:] {
		if !slices.Contains([]string{"code", "review"}, c.Step.Name) {
			t.Errorf("resume ran %s again", c.Step.Name)
		}
	}
	if f.ev.count("StepAccepted") != 5 {
		t.Fatalf("each step accepted exactly once, events = %v", f.ev.kinds())
	}
	validate(t, f.ev.events)
	if _, err := f.m.Resume(context.Background(), "RUN-1"); !errors.Is(err, machine.ErrRunEnded) {
		t.Fatalf("resume of an ended run: %v, want ErrRunEnded", err)
	}
}

func TestReplayOfEndedRunGivesByteIdenticalArtifacts(t *testing.T) {
	results := machine.NewResults()
	first := reuseFixture(t, results)
	first.start("RUN-1")
	second := reuseFixture(t, results)
	second.start("RUN-2")

	a := artifactsOf(first.ev, "StepAccepted")
	b := artifactsOf(second.ev, "ResultReused")
	if len(a) != 5 || !equalMaps(a, b) {
		t.Fatalf("artifact hashes differ:\n first  %v\n replay %v", a, b)
	}
	if d := machine.Compare(first.ev.events, second.ev.events); len(d) != 0 {
		t.Fatalf("replay differs from the original: %+v", d)
	}
}

func equalMaps(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !slices.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

func TestCompareListsOnlyDifferingFields(t *testing.T) {
	// Two independent runs (separate stores): the code output differs and the review is rejected in run two.
	one := newFixture(t, pattern())
	one.c.fn = noBlocking
	one.start("RUN-1")

	two := newFixture(t, pattern())
	two.w.fn["code"] = func(r machine.Request) (machine.Result, error) {
		res := ok(r)
		res.Outputs["code"] = hash("a different implementation")
		return res, nil
	}
	two.c.fn = func(r machine.CheckRequest) (machine.CheckResult, error) {
		if r.Step.Name == "review" && r.Name == "FindingsCarryEvidence" {
			return fail(r), nil
		}
		return noBlocking(r)
	}
	two.start("RUN-2")

	got := machine.Compare(one.ev.events, two.ev.events)
	codeLeft := hash("code/code/1")
	want := []machine.Difference{
		{Step: "code", Field: "artifacts", Left: codeLeft, Right: hash("a different implementation")},
		// run two's review fails its check on all 3 attempts and is handed over, so its artifacts are
		// the last attempt's and its decision is a rejection
		{Step: "review", Field: "artifacts", Left: hash("review/findings/1"), Right: hash("review/findings/3")},
		{Step: "review", Field: "checks", Left: "passed", Right: "failed: FindingsCarryEvidence does not hold"},
		{Step: "review", Field: "decision", Left: `{"accepted":true}`,
			Right: `{"accepted":false,"reason":"failed checks: FindingsCarryEvidence"}`},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("differences:\n got  %+v\n want %+v", got, want)
	}
	if same := machine.Compare(one.ev.events, one.ev.events); len(same) != 0 {
		t.Fatalf("a run differs from itself: %+v", same)
	}
}

func TestCompareShowsMissingSideAsAbsent(t *testing.T) {
	full := newFixture(t, pattern())
	full.c.fn = noBlocking
	full.start("RUN-1")
	short := newFixture(t, pattern())
	short.failChecks("tests") // never reaches code or review
	short.start("RUN-2")

	for _, d := range machine.Compare(full.ev.events, short.ev.events) {
		if d.Step == "code" && d.Field == "artifacts" {
			if d.Left == "" || d.Right != "" {
				t.Fatalf("code artifacts: left %q right %q, want a left value and no right value", d.Left, d.Right)
			}
			return
		}
	}
	t.Fatal("no difference reported for the step the second run never reached")
}

// recSink is the fake readable event record.
type recSink struct{ *sink }

func (s recSink) Read(run string) ([]machine.Event, error) {
	var out []machine.Event
	for _, e := range s.events {
		if e.Run == run {
			out = append(out, e)
		}
	}
	return out, nil
}

func TestRestartedMachineResumesFromTheRecordWithoutRepeatingAcceptedSteps(t *testing.T) {
	results := machine.NewResults()
	ev := recSink{&sink{}}
	clk := &clock{now: t0}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w1 := &worker{fn: map[string]func(machine.Request) (machine.Result, error){
		"code": func(r machine.Request) (machine.Result, error) { cancel(); return machine.Result{}, ctx.Err() },
	}}
	build := func(w *worker) *machine.Machine {
		return machine.New(machine.Config{Pattern: pattern(), Worker: w, Checker: &checker{fn: noBlocking},
			Clock: clk, Events: ev, Results: results, Model: "claude-haiku-5-5"})
	}
	m1 := build(w1)
	f := &fixture{t: t, ev: ev.sink}
	if _, err := m1.Start(ctx, f.params("RUN-1")); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if ev.count("StepAccepted") != 3 || ev.count("RunCompleted") != 0 {
		t.Fatalf("setup: events = %v", ev.kinds())
	}
	// the first machine is dropped; a new one reads the same record
	w2 := &worker{fn: map[string]func(machine.Request) (machine.Result, error){}}
	m2 := build(w2)
	r, err := m2.Resume(context.Background(), "RUN-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEnded(t, r, ev.sink, "done")
	for _, c := range w2.calls {
		if c.Step.Name != "code" && c.Step.Name != "review" {
			t.Errorf("restart repeated accepted step %s", c.Step.Name)
		}
	}
	if ev.count("StepAccepted") != 5 {
		t.Fatalf("each step accepted once, events = %v", ev.kinds())
	}
}
