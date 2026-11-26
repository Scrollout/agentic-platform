package captured_test

// T060 (feature 011, US2; T017 round 2, R1; FR-008, FR-010, the contract's `not_used` reason
// `refused-value`): a value refused for being empty is read on as the parameter's value (T052);
// a later place of that parameter in another case form must then give a refusal, never a panic.
// Planted change this catches: reading the empty value on into a capitalised form (`in` takes
// value[:1]), which panicked at 2ed1d09.

import (
	"strings"
	"testing"

	"github.com/agentic-platform/engine/captured"
)

// The notes slice with `note` (and Note, NOTE) emptied and notes -> tasks: the rest equals the
// source but for the parameters, so the only refusal is note's empty value, with no place.
func TestFitEmptyValueWithALaterCapitalisedPlaceIsTheValueRefusal(t *testing.T) {
	f := t3Load(t, "notes")
	ps := t3Params(f.src, f.template, []t3Spec{t3Notes, t3Note})
	s := t12Read(t, "notes")
	r := strings.NewReplacer("notes", "tasks", "Notes", "Tasks", "NOTES", "TASKS", "note", "", "Note", "", "NOTE", "")
	approved := s.apply(r).copy(t)
	var (
		ok  bool
		ref captured.Refusal
	)
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("Fit panicked on an empty value: %v", p)
			}
		}()
		_, ok, ref = captured.Fit(f.src, f.template, t12Writable, ps, approved)
	}()
	if ok {
		t.Fatal("a slice with an empty value fits")
	}
	if ref.Parameter == nil || ref.Parameter.Parameter != "note" || ref.Place != nil {
		t.Errorf("refusal %+v, want note's value refused and no place", ref)
	}
}

// The same empty value with a later difference in the rules: a place refusal (B1), no panic.
func TestFitEmptyValueWithALaterDifferenceIsAPlaceRefusal(t *testing.T) {
	f := t3Load(t, "notes")
	ps := t3Params(f.src, f.template, []t3Spec{t3Notes, t3Note})
	s := t12Read(t, "notes")
	r := strings.NewReplacer("notes", "tasks", "Notes", "Tasks", "NOTES", "TASKS", "note", "", "Note", "", "NOTE", "")
	approved := s.apply(r).copy(t)
	approved.Contract = append(append([]byte{}, approved.Contract[:len(approved.Contract)-1]...), ' ', approved.Contract[len(approved.Contract)-1])
	var (
		ok  bool
		ref captured.Refusal
	)
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("Fit panicked on an empty value: %v", p)
			}
		}()
		_, ok, ref = captured.Fit(f.src, f.template, t12Writable, ps, approved)
	}()
	if ok || ref.Place == nil {
		t.Errorf("ok %v refusal %+v, want a place refusal", ok, ref)
	}
}
