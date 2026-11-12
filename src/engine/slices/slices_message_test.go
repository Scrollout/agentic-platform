package slices_test

// T038f: the analyst's user message, built in production code. A first call or a retry produces
// exactly what the eval harness's message() produced before the move (pinned below); a revision
// adds the current proposal and the answered questions.

import (
	"strings"
	"testing"

	"github.com/agentic-platform/engine/slices"
)

const msgDescription = "An in-memory notes service."

func msgProposal() *slices.Proposal {
	return &slices.Proposal{Goals: []slices.Goal{{ID: "G-1", Text: "Keep notes"}}}
}

const msgProposalJSON = `{
  "goals": [
    {
      "id": "G-1",
      "text": "Keep notes"
    }
  ],
  "terms": null,
  "rules": null,
  "examples": "",
  "contract": "",
  "model": "",
  "agent_digest": ""
}`

func TestAnalystMessageFirstCallIsTheDescription(t *testing.T) {
	if got := slices.AnalystMessage(slices.AnalystInput{Description: msgDescription}); got != msgDescription {
		t.Fatalf("first call = %q, want the description unchanged", got)
	}
}

func TestAnalystMessageRetryPinsTheEvalFormat(t *testing.T) {
	got := slices.AnalystMessage(slices.AnalystInput{
		Description: msgDescription,
		Previous:    msgProposal(),
		Errors:      []string{"rule R-1 names no goal", "contract is not OpenAPI"},
	})
	want := msgDescription + "\n\nYour previous proposal was unusable.\n\nPrevious proposal:\n" + msgProposalJSON +
		"\n\nErrors:\n- rule R-1 names no goal\n- contract is not OpenAPI\n\n" +
		"Output a corrected proposal as one JSON object."
	if got != want {
		t.Fatalf("retry message differs.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestAnalystMessageRetryWithErrorsOnlyPinsNullPrevious(t *testing.T) {
	got := slices.AnalystMessage(slices.AnalystInput{Description: msgDescription, Errors: []string{"e1"}})
	want := msgDescription + "\n\nYour previous proposal was unusable.\n\nPrevious proposal:\nnull\n\nErrors:\n- e1\n\n" +
		"Output a corrected proposal as one JSON object."
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAnalystMessageRevisionCarriesProposalAndAnswers(t *testing.T) {
	opt := 1
	got := slices.AnalystMessage(slices.AnalystInput{
		Description: msgDescription,
		Current:     msgProposal(),
		Questions: []slices.Question{
			{ID: "Q-1", About: "G-1", Text: "Should notes expire?", Options: []string{"yes", "no"}, Answer: &slices.Answer{Question: "Q-1", Option: &opt}},
			{ID: "Q-2", About: "T-1", Text: "Maximum note length?", Answer: &slices.Answer{Question: "Q-2", Text: "280 characters"}},
		},
	})
	for _, need := range []string{
		msgDescription, msgProposalJSON, "Should notes expire?", "Maximum note length?", "280 characters",
	} {
		if !strings.Contains(got, need) {
			t.Errorf("revision message lacks %q:\n%s", need, got)
		}
	}
	// A chosen option is shown by its text, not by its position.
	if !strings.Contains(got, "no") {
		t.Errorf("revision message lacks the chosen option text:\n%s", got)
	}
	if !strings.Contains(strings.ToLower(got), "revis") {
		t.Errorf("revision message does not say to revise:\n%s", got)
	}
	if strings.Contains(got, "Your previous proposal was unusable") {
		t.Errorf("a revision is not a retry:\n%s", got)
	}
	// Each question appears before its answer, in order.
	if strings.Index(got, "Should notes expire?") > strings.Index(got, "Maximum note length?") {
		t.Errorf("questions out of order:\n%s", got)
	}
}
