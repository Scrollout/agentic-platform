package api

// T017 (US3, FR-004, FR-009, SC-002, SC-003; research R3): listRunEvents names the method identity
// a step was judged under in `method`, on the events that carry a reuse_key (AttemptMade,
// StepAccepted, ResultReused and the StepRejected that leaves a review round open) and on no other
// kind; an event recorded before feature 006 has none and still conforms to run-event.schema.json;
// two engines given the same identity record the same `method`. Written before the code (T018) by
// a test author.
//
// Seams: runs_ops_test.go (runsRig, its fakes, rawEvents), open_round_reload_test.go
// (openRoundWorker). Package durable imports package api, so the run is driven by the machine
// (machine.Config.Method, which durable.Activities.Method feeds) writing to the run record
// (RunRecord) the handler reads, not through the durable test service, whose helpers take no
// method.
//
// Edge cases checked: a ResultReused of an open round (run 2 over run 1's open round) carries the
// method; a StepRejected that is not an open round, a CheckFailed, FindingRecorded, StepStarted,
// RunStarted and RunEnded carry none; a run under no identity (a record as before feature 006,
// also with the stored line itself holding no "method") has none on any event and conforms; the
// identity is the same on every event of a run and on a second run that reuses under it; the field
// is read back from the record of a handler that did not run the run. Not covered: a failed reuse
// lookup (durable.Activities.Lookup failing leaves the method empty), which needs the Temporal
// activities and their lookup port.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/method"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	remA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	remB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// remEvents lets the machine write the run record the handler reads.
type remEvents struct{ rec *RunRecord }

func (e remEvents) Append(ev machine.Event) error { return e.rec.Record("S-1", ev, "OP-1", "") }

// remRun runs run on x's store under the method identity id ("" for none) with the machine,
// reusing the accepted results of rec, and returns the run.
func (x *runsRig) remRun(rec *RunRecord, id, run string) machine.Run {
	x.t.Helper()
	var mid method.ID
	if id != "" {
		var err error
		if mid, err = method.Parse(id); err != nil {
			x.t.Fatal(err)
		}
	}
	p, err := x.sp.svc.Pattern()
	if err != nil {
		x.t.Fatal(err)
	}
	mp := MachinePattern(p)
	m := machine.New(machine.Config{Pattern: mp, Worker: x.w, Checker: x.c, Clock: x.r.clock,
		Events: remEvents{rec}, Results: rec.Results(), Model: "test-model", Method: mid})
	inputs := map[string]string{}
	for _, n := range mp.SliceInputs() {
		inputs[n] = strings.Repeat("0", 64)
	}
	r, err := m.Start(context.Background(), machine.StartParams{Run: run, Slice: "S-1", Project: "notes-" + run,
		Inputs: inputs, Refs: []string{"G-1"}})
	if err != nil {
		x.t.Fatal(err)
	}
	return r
}

func (x *runsRig) remRecord() *RunRecord {
	x.t.Helper()
	p, err := x.sp.svc.Pattern()
	if err != nil {
		x.t.Fatal(err)
	}
	return NewRunRecord(x.st, MachinePattern(p), "test-model")
}

func remSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join(specDir, "run-event.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := jsonschema.NewCompiler().Compile(abs)
	if err != nil {
		t.Fatalf("run-event.schema.json: %v", err)
	}
	return s
}

// remCheck fails the test for each event that does not conform to run-event.schema.json, and
// returns the kinds seen with a `method` and the events, by kind, that lack one.
func remCheck(t *testing.T, s *jsonschema.Schema, evs []map[string]any, want string) {
	t.Helper()
	carries := map[string]bool{"AttemptMade": true, "StepAccepted": true, "ResultReused": true}
	for i, e := range evs {
		b, _ := json.Marshal(e)
		var doc any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(doc); err != nil {
			t.Errorf("event %d (%v) does not conform to run-event.schema.json: %v", i, e["kind"], err)
		}
		kind, _ := e["kind"].(string)
		wantMethod := want != "" && (carries[kind] || (kind == "StepRejected" && e["open_round"] == true))
		got, has := e["method"]
		switch {
		case wantMethod && got != want:
			t.Errorf("event %d %s step %v: method = %v, want %s", i, kind, e["step"], got, want)
		case !wantMethod && has:
			t.Errorf("event %d %s (open_round %v) has method %v, want none", i, kind, e["open_round"], got)
		}
	}
}

func remKinds(evs []map[string]any) map[string]int {
	n := map[string]int{}
	for _, e := range evs {
		k, _ := e["kind"].(string)
		if k == "StepRejected" && e["open_round"] == true {
			k = "StepRejected(open)"
		}
		n[k]++
	}
	return n
}

func TestListRunEventsNamesTheMethodOnTheEventsThatCarryAReuseKey(t *testing.T) {
	s := remSchema(t)
	x := newRunsRig(t)
	x.openRoundWorker()
	rec := x.remRecord()
	if r := x.remRun(rec, remA, "RUN-1"); r.Outcome != machine.OutcomeDone {
		t.Fatalf("RUN-1 outcome %q (%s), want done", r.Outcome, r.Reason)
	}
	if r := x.remRun(rec, remA, "RUN-2"); r.Outcome != machine.OutcomeDone {
		t.Fatalf("RUN-2 outcome %q (%s), want done", r.Outcome, r.Reason)
	}
	for _, run := range []string{"RUN-1", "RUN-2"} {
		evs := x.rawEvents(run)
		remCheck(t, s, evs, remA)
		n := remKinds(evs)
		if n["RunStarted"] == 0 {
			t.Errorf("%s has no RunStarted: %v", run, n)
		}
		if run == "RUN-1" && (n["AttemptMade"] == 0 || n["StepAccepted"] == 0 || n["StepRejected(open)"] == 0) {
			t.Errorf("RUN-1 lacks an AttemptMade, a StepAccepted or an open-round StepRejected: %v", n)
		}
		if run == "RUN-2" && n["ResultReused"] == 0 {
			t.Errorf("RUN-2 reused nothing: %v", n)
		}
	}
	if n := remKinds(x.rawEvents("RUN-2")); n["AttemptMade"] != 0 {
		t.Errorf("RUN-2 made attempts under the same method: %v", n)
	}
}

func TestListRunEventsNamesNoMethodOnARejectedStepThatIsNotAnOpenRound(t *testing.T) {
	s := remSchema(t)
	x := newRunsRig(t)
	p, err := x.sp.svc.Pattern()
	if err != nil {
		t.Fatal(err)
	}
	x.failStep(p.Steps[0].Name)
	x.remRun(x.remRecord(), remA, "RUN-1")
	// The run does not end (it waits for the developer): read the first page.
	rep := x.do("GET", apiPath("/runs/RUN-1/events"), "")
	var pg struct {
		Items []map[string]any `json:"items"`
	}
	if rep.Code != 200 || json.Unmarshal(rep.Body.Bytes(), &pg) != nil {
		t.Fatalf("listRunEvents = %d: %s", rep.Code, rep.Body.String())
	}
	evs := pg.Items
	remCheck(t, s, evs, remA)
	n := remKinds(evs)
	if n["StepRejected"] == 0 || n["AttemptMade"] == 0 {
		t.Errorf("the scenario needs a rejected step with attempts: %v", n)
	}
}

func TestListRunEventsOfARecordWithoutAMethodNameNone(t *testing.T) {
	s := remSchema(t)
	x := newRunsRig(t)
	x.openRoundWorker()
	rec := x.remRecord()
	x.remRun(rec, "", "RUN-1")
	x.remRun(rec, "", "RUN-2") // reuses under no identity, as before feature 006
	for _, run := range []string{"RUN-1", "RUN-2"} {
		evs := x.rawEvents(run)
		remCheck(t, s, evs, "")
		if n := remKinds(evs); n["StepAccepted"]+n["ResultReused"] == 0 {
			t.Errorf("%s: nothing accepted or reused: %v", run, n)
		}
	}
	lines, err := x.st.Events("RUN-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("the store holds no event of RUN-1")
	}
	for _, l := range lines {
		if strings.Contains(string(l.Data), `"method"`) {
			t.Errorf("a record written without an identity holds a method: %s", l.Data)
		}
	}
}

func TestTwoEnginesGivenTheSameIdentityRecordTheSameMethod(t *testing.T) {
	s := remSchema(t)
	methods := func(id string) []any {
		x := newRunsRig(t)
		x.remRun(x.remRecord(), id, "RUN-1")
		evs := x.rawEvents("RUN-1")
		remCheck(t, s, evs, id)
		var out []any
		for _, e := range evs {
			out = append(out, e["method"])
		}
		return out
	}
	a1, a2, b := methods(remA), methods(remA), methods(remB)
	if len(a1) == 0 || len(a1) != len(a2) {
		t.Fatalf("two engines under A recorded %d and %d events", len(a1), len(a2))
	}
	seen := false
	for i := range a1 {
		if a1[i] != a2[i] {
			t.Errorf("event %d: method %v on one engine, %v on the other", i, a1[i], a2[i])
		}
		if a1[i] != nil {
			seen = true
		}
	}
	if !seen {
		t.Error("no event carries a method")
	}
	for i := range b {
		if b[i] != nil && b[i] != remB {
			t.Errorf("event %d under B names %v", i, b[i])
		}
	}
}
