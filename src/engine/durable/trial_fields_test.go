package durable_test

// Feature 012, T008: the trial fields of a run and the Run workflow's version 3, written before the
// code (T009/T011) by a test author. Judged from the interface sheet (RunInput.TrialSet and
// RunInput.Trial; machine.Event.TrialSet `json:"trial_set,omitempty"` and Trial
// `json:"trial,omitempty"`, set on RunStarted only; workflowVersion 3), contracts/trial-sets.md and
// tasks.md T008. Nothing here changes an existing test, helper or fixture: the rigs of
// fill_proof_test.go (newFP, fpHistories, fpReplay) are reused as they are.
//
// The names that do not exist yet: durable.RunInput.TrialSet, durable.RunInput.Trial,
// machine.Event.TrialSet, machine.Event.Trial.
//
// Edge cases checked: a run with the fields records them on RunStarted and on no other event
// (a done run, and a run that rejects a step and ends cancelled); a run without them records none
// and its RunStarted JSON has no trial_set/trial key; trial 1 (the first number) and a higher one
// are both kept; a version-1 and a version-2 history replay unchanged; a version-3 history replays
// (the code knows version 3); a version-3 history whose input carries the fields replays; a
// history that claims version 4 fails (the replay is the version gate, so it can fail).

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/agentic-platform/engine/machine"
	historypb "go.temporal.io/api/history/v1"
)

// tfStarted returns the RunStarted event of f's run, failing when there is not exactly one.
func tfStarted(t *testing.T, f *fpRun) machine.Event {
	t.Helper()
	var started []machine.Event
	for _, e := range f.events {
		if e.Event.Kind == "RunStarted" {
			started = append(started, e.Event)
		}
	}
	if len(started) != 1 {
		t.Fatalf("%d RunStarted events recorded, want 1: %v", len(started), f.kinds())
	}
	return started[0]
}

// tfNoTrialElsewhere fails for any recorded event other than RunStarted that carries a trial field.
func tfNoTrialElsewhere(t *testing.T, f *fpRun) {
	t.Helper()
	t.Logf("examined %d recorded events: %v", len(f.events), f.kinds())
	for _, e := range f.events {
		if e.Event.Kind != "RunStarted" && (e.Event.TrialSet != "" || e.Event.Trial != 0) {
			t.Errorf("%s %s records trial_set %q trial %d: the trial fields belong on RunStarted only",
				e.Event.Kind, e.Event.Step, e.Event.TrialSet, e.Event.Trial)
		}
	}
}

// T008 clause: a run started with a trial set and a trial number records them on RunStarted.
func TestARunOfATrialSetRecordsItsSetAndTrialNumberOnRunStarted(t *testing.T) {
	for _, trial := range []int{1, 3} {
		f := newFP(t)
		f.in.TrialSet, f.in.Trial = "TS-4", trial
		f.done()
		s := tfStarted(t, f)
		if s.TrialSet != "TS-4" || s.Trial != trial {
			t.Errorf("RunStarted records trial_set %q trial %d, want TS-4 and %d", s.TrialSet, s.Trial, trial)
		}
		tfNoTrialElsewhere(t, f)
	}
}

// T008 clause: the fields are on RunStarted and no other event, also in a run that rejects a step
// (more event kinds) and ends cancelled.
func TestTheTrialFieldsAreOnRunStartedAndNoOtherEventOfARunThatRejectsAStep(t *testing.T) {
	f := newFP(t)
	f.in.TrialSet, f.in.Trial = "TS-9", 2
	f.failCheck["tests/TestsBuild"] = true
	f.run()
	if s := tfStarted(t, f); s.TrialSet != "TS-9" || s.Trial != 2 {
		t.Errorf("RunStarted records trial_set %q trial %d, want TS-9 and 2", s.TrialSet, s.Trial)
	}
	if !slices.Contains(f.kinds(), "StepRejected tests") {
		t.Fatalf("the planted failing check produced no StepRejected for tests: %v", f.kinds())
	}
	tfNoTrialElsewhere(t, f)
}

// T008 clause: a run without a trial set records no trial field, in the event or in its JSON.
func TestARunWithoutATrialSetRecordsNoTrialField(t *testing.T) {
	f := newFP(t)
	f.done()
	s := tfStarted(t, f)
	if s.TrialSet != "" || s.Trial != 0 {
		t.Errorf("RunStarted records trial_set %q trial %d for a run of no trial set", s.TrialSet, s.Trial)
	}
	tfNoTrialElsewhere(t, f)
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"trial_set", "trial"} {
		if _, ok := m[k]; ok {
			t.Errorf("the RunStarted JSON has a %q key for a run of no trial set: %s", k, b)
		}
	}
}

// T008 clause: the JSON names of the fields are the interface sheet's.
func TestTheTrialFieldsOfAnEventHaveTheirContractNames(t *testing.T) {
	b, err := json.Marshal(machine.Event{Kind: "RunStarted", TrialSet: "TS-2", Trial: 5})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["trial_set"] != "TS-2" || m["trial"] != float64(5) {
		t.Errorf("the event JSON is %s, want trial_set \"TS-2\" and trial 5", b)
	}
}

// ---- histories at the versions of the Run workflow ----

// tfAtVersion sets the version the history's "run" GetVersion marker records (the recorded
// histories hold version 1) and, when set is non-empty, adds the trial fields to the run's input
// as a run started at that version would have.
func tfAtVersion(t *testing.T, h *historypb.History, version, set string, trial int) {
	t.Helper()
	patched := false
	for _, e := range h.Events {
		m := e.GetMarkerRecordedEventAttributes()
		if m == nil || m.MarkerName != "Version" {
			continue
		}
		id := m.Details["change-id"].GetPayloads()
		if len(id) == 0 || string(id[0].Data) != `"run"` {
			continue
		}
		m.Details["version"].GetPayloads()[0].Data = []byte(version)
		patched = true
	}
	if !patched {
		t.Fatal("the history has no \"run\" version marker")
	}
	if set == "" {
		return
	}
	p := h.Events[0].GetWorkflowExecutionStartedEventAttributes().GetInput().GetPayloads()[0]
	dec := json.NewDecoder(bytes.NewReader(p.Data))
	dec.UseNumber()
	var in map[string]any
	if err := dec.Decode(&in); err != nil {
		t.Fatal(err)
	}
	in["TrialSet"], in["Trial"] = set, trial
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	p.Data = b
}

// T008 clause: a history made at version 2 (and the recorded ones, made at version 1) has no trial
// fields and replays unchanged on the version-3 code.
func TestAHistoryMadeAtVersion2ReplaysUnchanged(t *testing.T) {
	for name, h := range fpHistories(t) {
		t.Run(name+"/as-recorded", func(t *testing.T) {
			if err := fpReplay(h); err != nil {
				t.Fatalf("replay of %s as recorded failed: %v", name, err)
			}
		})
	}
	for name, h := range fpHistories(t) {
		t.Run(name+"/version-2", func(t *testing.T) {
			tfAtVersion(t, h, "2", "", 0)
			t.Logf("examined %s at version 2: %d events", name, len(h.Events))
			if err := fpReplay(h); err != nil {
				t.Fatalf("replay of %s at version 2 failed: %v", name, err)
			}
		})
	}
}

// T008 clause: the code is at version 3: a history that records version 3 replays, also with the
// trial fields in its input. (Under version 2 code the replay fails: version 3 is not supported.)
func TestAHistoryMadeAtVersion3ReplaysAlsoWithTheTrialFieldsInItsInput(t *testing.T) {
	for name, h := range fpHistories(t) {
		t.Run(name+"/version-3", func(t *testing.T) {
			tfAtVersion(t, h, "3", "", 0)
			if err := fpReplay(h); err != nil {
				t.Fatalf("replay of %s at version 3 failed: %v", name, err)
			}
		})
	}
	for name, h := range fpHistories(t) {
		t.Run(name+"/version-3-trial", func(t *testing.T) {
			tfAtVersion(t, h, "3", "TS-1", 2)
			if err := fpReplay(h); err != nil {
				t.Fatalf("replay of %s at version 3 with trial fields failed: %v", name, err)
			}
		})
	}
}

// T008 clause (planted): the version marker is judged: a history that claims version 4, which the
// code does not know, fails the replay, so the replay tests above can fail.
func TestAHistoryThatClaimsAVersionTheCodeDoesNotKnowFailsTheReplay(t *testing.T) {
	for name, h := range fpHistories(t) {
		t.Run(name, func(t *testing.T) {
			tfAtVersion(t, h, "4", "", 0)
			if err := fpReplay(h); err == nil {
				t.Fatalf("replay of %s passed with a version marker of 4", name)
			}
		})
	}
}
