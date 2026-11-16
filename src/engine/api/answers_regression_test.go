package api

// Regression: answerQuestions answered 500 for an option written as 1.0, which the contract's
// integer accepts. An integer in any JSON form is the same option.

import "testing"

func TestAnswerOptionInAnyIntegerFormIsTheSameOption(t *testing.T) {
	for _, option := range []string{"1.0", "1e0", "10e-1", "1.000"} {
		t.Run(option, func(t *testing.T) {
			sp := twoQuestionRig(t)
			body := `{"answers":[{"question":"Q-1","answer":{"option":` + option + `}},{"question":"Q-2","answer":{"text":"All of them"}}]}`
			sp.accepted(t, sp.post("POST", spURL("S-1", "/answers"), body), "answer-questions", "S-1")
			got, _ := sp.getSlice(t, "S-1")
			for _, q := range got.Questions {
				if q.ID == "Q-1" && (q.Answer == nil || q.Answer.Option == nil || *q.Answer.Option != 1) {
					t.Errorf("Q-1 must hold option 1: %+v", q.Answer)
				}
			}
		})
	}
}

func TestAnswerOptionBeyondIntIsOutOfRangeNot500(t *testing.T) {
	sp := twoQuestionRig(t)
	for _, option := range []string{"1e30", "99999999999999999999999"} {
		body := `{"answers":[{"question":"Q-1","answer":{"option":` + option + `}}]}`
		sp.post("POST", spURL("S-1", "/answers"), body).problem(t, 400, "InvalidRequest")
	}
	sp.noOperation(t)
}
