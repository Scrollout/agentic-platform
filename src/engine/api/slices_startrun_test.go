package api

// Tests for startRun's refusals (task T038c). Nothing can be built in Story 1, so a slice that
// fits the pattern and is fully approved is refused with WorkersNotReady, which the contract
// lists for startRun. The tests are written before the code; the implementer must not change
// them (ADR 0029). See slices_read_test.go for the interface and the shared helpers.
//
// Order the contract and task T038c give: unknown slice (404), then PatternDoesNotFit, then
// NotApproved, then WorkersNotReady. Every refusal is synchronous: no operation is created.

import (
	"testing"

	"github.com/agentic-platform/engine/slices"
)

const runBody = `{"project":"notes-app"}`

func TestStartRunOfAnUnknownSliceIs404(t *testing.T) {
	sp := newNotes(t)
	sp.do("POST", spURL("S-9", "/runs"), clientToken, runBody, nil).problem(t, 404, "NotFound")
	sp.noOperation(t)
}

func TestStartRunRefusesASliceWhoseContractDoesNotFitEvenWhenNothingIsApproved(t *testing.T) {
	sp := newNotes(t, func(c *slices.Config) { c.Pattern = nil }) // nil fails closed
	sp.seed(t, "Let me keep notes.")
	p := sp.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).problem(t, 409, "PatternDoesNotFit")
	if p.Failures == nil || len(*p.Failures) == 0 {
		t.Errorf("PatternDoesNotFit must list the failing conditions in failures")
	}
	sp.noOperation(t)
	sp.noWork(t)
}

func TestStartRunRefusesAFittingSliceThatIsNotApproved(t *testing.T) {
	sp := newNotes(t)
	sl := sp.seed(t, "Let me keep notes.")
	sp.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).problem(t, 409, "NotApproved")

	// Two parts approved is still not approved.
	ctx := t.Context()
	if _, err := sp.svc.Approve(ctx, "S-1", slices.PartGoals, sl.Parts.Goals); err != nil {
		t.Fatal(err)
	}
	if _, err := sp.svc.Approve(ctx, "S-1", slices.PartRules, sl.Parts.Rules); err != nil {
		t.Fatal(err)
	}
	sp.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).problem(t, 409, "NotApproved")
	sp.noOperation(t)
	sp.noWork(t)
}

func TestStartRunRefusesASliceWhoseApprovedPartChanged(t *testing.T) {
	sp := newNotes(t)
	approved := sp.approveAfterSeed(t)
	goals := approved.Goals
	goals[0].Text = "Keep a note for good"
	if _, err := sp.svc.ReplaceGoals(t.Context(), "S-1", goals); err != nil {
		t.Fatal(err)
	}
	sp.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).problem(t, 409, "NotApproved")
	sp.noOperation(t)
}

func TestStartRunOfAFittingApprovedSliceIsWorkersNotReady(t *testing.T) {
	sp := newNotes(t)
	sp.approveAfterSeed(t)
	sp.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).problem(t, 409, "WorkersNotReady")
	sp.noOperation(t)
	sp.noWork(t)
}

func TestStartRunBodyIsStrict(t *testing.T) {
	sp := newNotes(t)
	sp.seed(t, "Let me keep notes.")
	for name, body := range map[string]string{
		"unknown field": `{"project":"notes-app","extra":1}`,
		"wrong type":    `{"project":5}`,
		"missing":       `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			sp.do("POST", spURL("S-1", "/runs"), clientToken, body, nil).problem(t, 400, "InvalidRequest")
		})
	}
	// The contract lists InvalidProject for a name that is not a valid folder name.
	sp.do("POST", spURL("S-1", "/runs"), clientToken, `{"project":"Bad Name"}`, nil).problem(t, 400, "InvalidProject")
	sp.noOperation(t)
}
