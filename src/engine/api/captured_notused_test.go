package api

// Feature 011, T051 (US2, review B1 of T017; FR-008, FR-010; the engine contract's `not_used`
// reason `refused-value`: "it would fit but for a value outside its parameter's form, or equal to
// another parameter's in a case form"; contract "Engine API: /captured-patterns", startRun;
// data model "States": "a fit refused only for a value gives `refused-value`"). On the rig of
// captured_startrun_test.go (newSR, addSlice, startRun, wantChoice, wantNotUsed: called, not
// changed). Written before the code (T052) by a test author; the code author must not change it
// (ADR 0029).
//
// A pattern that does not fit is not named at all, per the contract: a value refused together
// with a later difference is not "only because of a refused value", so an unnamed start names no
// `refused-value` for it, uses the base pattern, and `not_used` is empty. The slice used has a
// space in the name where the source had `note` (srRefused's "to do") and a later difference of
// the rules part (a rule's response, srDiffers's change): the value is read in the feature text
// ("every to do"), which the rules part holds before its rules array, and the difference lies in
// the rules array, so it comes after the refused value.
//
// Edge cases checked: the unnamed start (base, `not_used` empty on the Run and on RunStarted);
// the same slice without the difference still names the captured pattern `refused-value` with the
// parameter (guard); a named start of the slice with the difference is PatternDoesNotFit naming
// the place (the rules part), not the refused parameter, and starts nothing; the same when the
// captured pattern is also unusable (a method change): a pattern that does not fit is not named
// `needs-proof` either. Left out: a difference in the contract part or the goals (the captured
// package test covers every part; this test covers the choice made on its answer), several
// captured patterns (T013's order cases).

import (
	"strings"
	"testing"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/slices"
)

// srRefusedAndDiffers has the refused value (a space in the name) and a later difference outside
// every parameter.
func srRefusedAndDiffers(t *testing.T) slices.Proposal {
	return srProposal(t, srSwap("to do", "tasks"), srReplace("shall list tasks", "shall list every item"))
}

// T051, FR-010, contract `not_used`: an unnamed start of a slice with a refused value and a later
// structural difference uses the base pattern and names the captured pattern nowhere in
// `not_used`; the same slice without the difference names it `refused-value` with the parameter.
// Seen failing before the fix: the value refusal is returned before the difference is read, so
// the captured pattern is named `refused-value`.
func TestSRRefusedValueWithALaterDifferenceIsNotNamedRefusedValue(t *testing.T) {
	s := newSR(t, srRefusedAndDiffers(t), srRefused(t))
	both := s.addSlice("Let me keep to dos and list every item.", true)
	valueOnly := s.addSlice("Let me keep to dos.", true)
	registerAll(t, s.r, both)
	registerAll(t, s.r, valueOnly)
	id := s.approved("notes")
	noteName := srParam(t, s.captured(id), "note")

	op := s.startRun(both, "both-app", "")
	base := s.getRunView(op.Target).Pattern
	s.wantChoice(op.Target, base, gen.PatternReasonBase, nil, "")
	s.wantNotUsed(op.Target) // not named at all: it does not fit
	s.waitEnded(op.Target, "done")

	// the fit decides first, also for an unusable captured pattern: not named `needs-proof` either
	s.setProofMethod(id, cmMethodB)
	op = s.startRun(both, "both-two", "")
	s.wantChoice(op.Target, base, gen.PatternReasonBase, nil, "")
	s.wantNotUsed(op.Target)
	s.waitEnded(op.Target, "done")

	// guard: the same slice without the difference is refused only for a value
	op = s.startRun(valueOnly, "value-app", "")
	s.wantChoice(op.Target, base, gen.PatternReasonBase, nil, "")
	s.wantNotUsed(op.Target, srUnused{pattern: id, reason: "refused-value", parameter: noteName})
	s.waitEnded(op.Target, "done")
}

// T051, FR-008, FR-010: a named captured pattern the slice does not fit because of a later
// difference is PatternDoesNotFit that tells the place of the difference (the rules part), not
// the refused value, and starts nothing. Seen failing before the fix: the detail names the
// parameter.
func TestSRNamedCapturedPatternWithRefusedValueAndLaterDifferenceNamesThePlace(t *testing.T) {
	s := newSR(t, srRefusedAndDiffers(t))
	both := s.addSlice("Let me keep to dos and list every item.", true)
	registerAll(t, s.r, both)
	id := s.approved("notes")

	p := s.refused(both, "both-app", id, 409, gen.ErrorCodePatternDoesNotFit)
	if p.Detail == nil || !strings.Contains(*p.Detail, "rules") {
		t.Errorf("the detail %q does not name the first place that differs (the rules part)", derefStr(p.Detail))
	}
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
