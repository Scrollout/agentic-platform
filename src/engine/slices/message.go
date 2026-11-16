package slices

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AnalystMessage is the analyst's user message for in. A first call is the description alone. A
// revision (in.Current set) adds the current proposal and the slice's questions, each with the
// developer's answer, and asks for a revised proposal. A retry (in.Previous or in.Errors set) adds
// the unusable proposal and what was wrong with it, and asks for a corrected one; on a revision's
// retry it follows the revision part. The analyst's instruction (src/agents/analyst/instruction.md)
// describes both.
func AnalystMessage(in AnalystInput) string {
	msg := in.Description
	if in.Current != nil {
		msg += "\n\nRevise your current proposal with the developer's answers to its questions. Keep " +
			"what the answers do not change, keep the IDs of the items you keep, and ask no question " +
			"again that is answered.\n\nCurrent proposal:\n" + indentJSON(in.Current) + "\n\nQuestions:"
		for _, q := range in.Questions {
			msg += fmt.Sprintf("\n- %s (about %s): %s\n  Answer: %s", q.ID, q.About, q.Text, answerText(q))
		}
		msg += "\n\nOutput the revised proposal as one JSON object."
	}
	if in.Previous == nil && len(in.Errors) == 0 {
		return msg
	}
	return fmt.Sprintf("%s\n\nYour previous proposal was unusable.\n\nPrevious proposal:\n%s\n\nErrors:\n- %s\n\n"+
		"Output a corrected proposal as one JSON object.", msg, indentJSON(in.Previous), strings.Join(in.Errors, "\n- "))
}

// indentJSON is p as JSON indented by two spaces; a nil p is "null". A Proposal always marshals.
func indentJSON(p *Proposal) string {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		panic(fmt.Sprintf("slices: a proposal does not marshal: %v", err))
	}
	return string(b)
}

// answerText is q's answer as the developer gave it: a chosen option by its text, or the free text.
func answerText(q Question) string {
	switch {
	case q.Answer == nil:
		return "(not answered yet)"
	case q.Answer.Option != nil && *q.Answer.Option >= 0 && *q.Answer.Option < len(q.Options):
		return q.Options[*q.Answer.Option]
	default:
		// On one line, so an answer cannot imitate the message's own question and answer lines.
		return strings.Join(strings.Fields(q.Answer.Text), " ")
	}
}
