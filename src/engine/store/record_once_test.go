// Tests for recording an event once per seq (T009, written before the implementation, T010).
// Spec: specs/002-durable-execution/data-model.md, "Run record": the store accepts each seq
// once; the same content again succeeds without writing; other content under a taken seq is
// refused (SeqTaken).
//
// Interface the code author implements (package store):
//
//	var ErrSeqTaken error // returned (wrapped is fine; callers use errors.Is) for a taken seq
//	                      // with other content, and for a seq beyond the next one
//
//	// RecordEvent records e, whose Seq the caller sets, under slice, and returns the event as
//	// stored. e.Seq is 1 + the number of events the run holds: the event is appended.
//	// e.Seq is already held and the held event equals e: it succeeds and writes nothing.
//	// "Equals" means every field of Event, Seq included (Event is comparable with ==; the
//	// stored event is what Events returns). e.Seq is held with any other content: ErrSeqTaken.
//	// e.Seq is beyond the next one (a gap), or below 1: refused, nothing written (ErrSeqTaken
//	// for a gap). Like Append it validates the slice and run IDs and the kind, and it returns
//	// only after the event is durably recorded. Holds after a reopen of the store.
//	func (s *Store) RecordEvent(sliceID string, e Event) (Event, error)
package store

import (
	"errors"
	"reflect"
	"testing"
)

const roSlice, roRun = "S-1", "RUN-1"

func rec(t *testing.T, s *Store, e Event) Event {
	t.Helper()
	got, err := s.RecordEvent(roSlice, e)
	if err != nil {
		t.Fatalf("RecordEvent(seq %d): %v", e.Seq, err)
	}
	return got
}

func ev(seq int, kind string) Event { return Event{Seq: seq, Run: roRun, Kind: kind} }

// checkRules runs the four rules against a store that holds exactly events 1 and 2.
func checkRules(t *testing.T, s *Store, dir string) {
	t.Helper()
	before := snapshot(t, dir)
	unchanged := func(what string) {
		t.Helper()
		if !reflect.DeepEqual(before, snapshot(t, dir)) {
			t.Fatalf("%s changed the files on disk", what)
		}
	}
	if got := rec(t, s, ev(2, "AttemptMade")); got != ev(2, "AttemptMade") {
		t.Fatalf("same content: returned %+v", got)
	}
	unchanged("the same seq with the same content")
	if _, err := s.RecordEvent(roSlice, ev(2, "RunFailed")); !errors.Is(err, ErrSeqTaken) {
		t.Fatalf("other content under a taken seq: err = %v, want ErrSeqTaken", err)
	}
	unchanged("other content under a taken seq")
	if _, err := s.RecordEvent(roSlice, ev(1, "RunFailed")); !errors.Is(err, ErrSeqTaken) {
		t.Fatalf("other content under seq 1: err = %v, want ErrSeqTaken", err)
	}
	if _, err := s.RecordEvent(roSlice, ev(4, "RunSucceeded")); !errors.Is(err, ErrSeqTaken) {
		t.Fatalf("seq beyond the next: err = %v, want ErrSeqTaken", err)
	}
	unchanged("a seq beyond the next one")
	if got, _ := s.Events(roRun); len(got) != 2 {
		t.Fatalf("run holds %d events, want 2", len(got))
	}
}

func TestRecordEventNewSeqAppends(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	want := []Event{ev(1, "RunStarted"), ev(2, "AttemptMade"), ev(3, "RunSucceeded")}
	for _, e := range want {
		before := snapshot(t, dir)
		if got := rec(t, s, e); got != e {
			t.Fatalf("seq %d: returned %+v, want %+v", e.Seq, got, e)
		}
		if reflect.DeepEqual(before, snapshot(t, dir)) {
			t.Fatalf("seq %d: nothing was written", e.Seq)
		}
	}
	got, err := s.Events(roRun)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Events = %+v, %v; want %+v", got, err, want)
	}
}

func TestRecordEventSameSeqSameContentWritesNothing(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	e := Event{Seq: 1, Run: roRun, Kind: "StepStarted", Step: "S1", Data: `{"a":1}`}
	rec(t, s, e)
	before := snapshot(t, dir)
	if got := rec(t, s, e); got != e {
		t.Fatalf("returned %+v, want %+v", got, e)
	}
	if !reflect.DeepEqual(before, snapshot(t, dir)) {
		t.Fatal("the repeat changed the files on disk")
	}
}

func TestRecordEventSameSeqOtherContentIsSeqTaken(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	rec(t, s, Event{Seq: 1, Run: roRun, Kind: "RunStarted", Data: `{"a":1}`})
	for _, other := range []Event{
		{Seq: 1, Run: roRun, Kind: "RunFailed", Data: `{"a":1}`},
		{Seq: 1, Run: roRun, Kind: "RunStarted", Data: `{"a":2}`},
		{Seq: 1, Run: roRun, Kind: "RunStarted", Step: "S1", Data: `{"a":1}`},
	} {
		before := snapshot(t, dir)
		if _, err := s.RecordEvent(roSlice, other); !errors.Is(err, ErrSeqTaken) {
			t.Fatalf("%+v: err = %v, want ErrSeqTaken", other, err)
		}
		if !reflect.DeepEqual(before, snapshot(t, dir)) {
			t.Fatalf("%+v: the refusal changed the files on disk", other)
		}
	}
}

func TestRecordEventSeqBeyondTheNextIsRefused(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	before := snapshot(t, dir)
	for _, seq := range []int{2, 5} {
		if _, err := s.RecordEvent(roSlice, ev(seq, "RunStarted")); !errors.Is(err, ErrSeqTaken) {
			t.Fatalf("seq %d on an empty run: err = %v, want ErrSeqTaken", seq, err)
		}
	}
	if !reflect.DeepEqual(before, snapshot(t, dir)) {
		t.Fatal("the refusal changed the files on disk")
	}
	rec(t, s, ev(1, "RunStarted"))
	if _, err := s.RecordEvent(roSlice, ev(3, "RunSucceeded")); !errors.Is(err, ErrSeqTaken) {
		t.Fatalf("seq 3 after seq 1: err = %v, want ErrSeqTaken", err)
	}
	if got, _ := s.Events(roRun); len(got) != 1 {
		t.Fatalf("run holds %d events, want 1", len(got))
	}
}

func TestRecordEventRulesHoldInOneStore(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	rec(t, s, ev(1, "RunStarted"))
	rec(t, s, ev(2, "AttemptMade"))
	checkRules(t, s, dir)
}

func TestRecordEventRulesHoldAfterReopen(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	rec(t, s, ev(1, "RunStarted"))
	rec(t, s, ev(2, "AttemptMade"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = open(t, dir)
	checkRules(t, s, dir)
	if got := rec(t, s, ev(3, "RunSucceeded")); got != ev(3, "RunSucceeded") {
		t.Fatalf("seq 3 after reopen: %+v", got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = open(t, dir)
	if got := rec(t, s, ev(3, "RunSucceeded")); got != ev(3, "RunSucceeded") {
		t.Fatalf("seq 3 repeated after a second reopen: %+v", got)
	}
}
