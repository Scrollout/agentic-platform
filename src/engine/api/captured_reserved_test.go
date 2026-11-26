package api

// Feature 011, T062 (US2; T061 option A, owner decision 2026-11-27; spec Clarification "Q (converge
// US2, T061)", FR-008, FR-010, User Story 2 scenario 5; the engine contract's `not_used` reason
// `refused-value`: "it would fit but for a value outside its parameter's form, or equal to
// another parameter's in a case form"; a named start answers PatternDoesNotFit). On the rig of
// captured_startrun_test.go (newSR, addSlice, startRun, refused, wantChoice, wantNotUsed: called,
// not changed). Written before the code (T063) by a test author.
//
// A Go keyword as a later slice's `name` value does not fit, so an unnamed start uses the base
// pattern and names the approved, usable captured pattern `refused-value` with the parameter; a
// named start is PatternDoesNotFit naming the parameter. A longer word (`types`) fits.
//
// Edge cases checked: the singular (`note`, first read in the rules part) and the plural (`notes`,
// first read in the goals) each holding a keyword; the unnamed and the named start; the same when
// the captured pattern is also unusable (a method change: the fit decides first, as T051); `type`
// (the YAML/OpenAPI key word, which the slice's contract also uses as `type: object`) and `func`;
// the guard `types`. Left out: every one of the 25 words (captured package covers them), a
// keyword plus a later difference (the captured package covers the order of refusals), two
// keywords at once.

import (
	"strings"
	"testing"

	"github.com/agentic-platform/engine/api/gen"
)

// T062, FR-008, FR-010, scenario 5: see the file comment. Expected: the slices' values are what the
// swap puts in (srSwap: note, notes), so `type` is the singular's value and `func` the plural's;
// the captured pattern's parameter names come from srParam, not from the values. Seen failing
// before the fix: the slice fits, so the captured pattern is used (reason `fits`) and the named
// start is accepted.
func TestSRGoKeywordNameValueIsRefusedValue(t *testing.T) {
	s := newSR(t, srProposal(t, srSwap("type", "tasks")), srProposal(t, srSwap("task", "func")))
	singular := s.addSlice("Let me keep types.", true)
	plural := s.addSlice("Let me keep funcs.", true)
	registerAll(t, s.r, singular)
	registerAll(t, s.r, plural)
	id := s.approved("notes")
	cp := s.captured(id)
	noteName := srParam(t, cp, "note")
	notesName := srParam(t, cp, "notes")

	for _, c := range []struct {
		slice, project, param string
	}{{singular, "type-app", noteName}, {plural, "func-app", notesName}} {
		// unnamed: the base pattern, the captured pattern named refused-value with the parameter
		op := s.startRun(c.slice, c.project, "")
		base := s.getRunView(op.Target).Pattern
		s.wantChoice(op.Target, base, gen.PatternReasonBase, nil, "")
		s.wantNotUsed(op.Target, srUnused{pattern: id, reason: "refused-value", parameter: c.param})
		s.waitEnded(op.Target, "done")

		// named: PatternDoesNotFit naming the parameter, starting nothing
		p := s.refused(c.slice, c.project+"-named", id, 409, gen.ErrorCodePatternDoesNotFit)
		if p.Detail == nil || !strings.Contains(*p.Detail, c.param) {
			t.Errorf("the detail %v does not name the refused parameter %q", p.Detail, c.param)
		}

	}

	// the fit decides first: an unusable captured pattern is still `refused-value`
	s.setProofMethod(id, cmMethodB)
	op := s.startRun(singular, "type-two", "")
	s.wantNotUsed(op.Target, srUnused{pattern: id, reason: "refused-value", parameter: noteName})
	s.waitEnded(op.Target, "done")
	op = s.startRun(plural, "func-two", "")
	s.wantNotUsed(op.Target, srUnused{pattern: id, reason: "refused-value", parameter: notesName})
	s.waitEnded(op.Target, "done")
}

// T062 (guard): a longer word that merely starts with a keyword is not reserved: the slice fits,
// the captured pattern is used with the values (`fits`), and nothing is named `not_used`.
func TestSRWordLongerThanAGoKeywordFits(t *testing.T) {
	s := newSR(t, srProposal(t, srSwap("types", "tasks")))
	later := s.addSlice("Let me keep tasks of types.", true)
	s.registerSteps(later, cpFillOnly)
	id := s.approved("notes")
	cp := s.captured(id)
	want := srWant(cp, map[string]string{"note": "types", "notes": "tasks"})

	op := s.startRun(later, "types-app", "")
	s.wantChoice(op.Target, id+"@"+string(cp.Version), gen.PatternReasonFits, want, cp.Source.Run)
	s.wantNotUsed(op.Target)
	s.waitEnded(op.Target, "done")
}
