package api

// Tests for the slice write operations served by the engine (task T038c): createSlice,
// replaceGoals, replaceRules, replaceContract, approvePart, answerQuestions and deleteSlice. They
// are written before the code; the implementer must not change them (ADR 0029). See
// slices_read_test.go for the interface and the shared helpers.
//
// What they pin down, from the contract (openapi.yaml):
//   - every write answers 202 with an Operation whose target is the slice, followed with
//     getOperation; its kind is propose-slice, replace-goals, replace-rules, replace-contract,
//     approve-part, answer-questions or delete-slice;
//   - refusals the contract lists as 409 (PartFailsChecks, ContradictionOpen, StaleContentHash,
//     PatternDoesNotFit, QuestionNotOpen) come at once, before any operation exists, as a problem
//     with status, code and, for failing checks, `failures`; NarrowDescription and
//     TooManyQuestions are listed under `operation` and end the operation `failed`;
//   - request bodies are strict: unknown fields, wrong types and an empty description are 400
//     InvalidRequest; a slice that does not exist is 404 NotFound for every per-slice operation;
//   - the server does the work itself: Config.Work is not called.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/slices"
)

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (sp *spRig) post(method, path, body string) reply {
	return sp.do(method, path, clientToken, body, nil)
}

// ---- createSlice ----

func TestCreateSliceReturnsAProposeSliceOperationNamingTheNewSlice(t *testing.T) {
	sp := newNotes(t)
	op := sp.accepted(t, sp.post("POST", slicesURL(""), createBody), "propose-slice", "S-1")
	if op.Status != "succeeded" || op.Result["slice"] != "S-1" || len(op.Result) != 1 {
		t.Fatalf("operation %+v, want succeeded with result.slice S-1", op)
	}
	got, _ := sp.getSlice(t, "S-1")
	if got.Description != "A shopper can add an item to a basket." || len(got.Goals) == 0 {
		t.Errorf("the slice is not the proposal for the description: %+v", got)
	}
	if sp.analyst.calls() != 1 {
		t.Errorf("analyst called %d times, want 1", sp.analyst.calls())
	}
	op2 := sp.accepted(t, sp.post("POST", slicesURL(""), `{"description":"another use case"}`), "propose-slice", "S-2")
	if op2.Result["slice"] != "S-2" {
		t.Errorf("second slice %v, want S-2", op2.Result)
	}
	sp.noWork(t)
}

func TestSliceIDsAreNeverReusedAfterADelete(t *testing.T) {
	sp := newNotes(t)
	sp.accepted(t, sp.post("POST", slicesURL(""), createBody), "propose-slice", "S-1")
	sp.accepted(t, sp.post("DELETE", spURL("S-1", ""), ""), "delete-slice", "S-1")
	op := sp.accepted(t, sp.post("POST", slicesURL(""), createBody), "propose-slice", "S-2")
	if op.Result["slice"] != "S-2" {
		t.Errorf("after deleting S-1 the next slice is %v, want S-2", op.Result)
	}
}

func TestCreateSliceOfAnApprovedDescriptionOffersTheExistingSlice(t *testing.T) {
	sp := newNotes(t)
	sp.seed(t, "Let me keep notes.")
	sp.approveAll(t, "S-1")
	op := sp.accepted(t, sp.post("POST", slicesURL(""), `{"description":"Let me keep notes."}`), "propose-slice", "S-2")
	if op.Status != "succeeded" || op.Result["reused_slice"] != "S-1" || len(op.Result) != 1 {
		t.Errorf("operation %+v, want succeeded with result.reused_slice S-1", op)
	}
	if n := sp.analyst.calls(); n != 1 {
		t.Errorf("analyst called %d times, want 1 (not again for a reused slice)", n)
	}
}

func TestCreateSliceEndsFailedWithNarrowDescription(t *testing.T) {
	sp := newSP(t, []spResult{{err: slices.ErrMultipleUseCases}}, nil)
	op := sp.accepted(t, sp.post("POST", slicesURL(""), createBody), "propose-slice", "S-1")
	if op.Status != "failed" || op.Error == nil || op.Error.Code != "NarrowDescription" {
		t.Errorf("operation %+v, want failed with NarrowDescription", op)
	}
}

func TestCreateSliceEndsFailedWithTooManyQuestions(t *testing.T) {
	p := spNotes(t)
	for i := 0; i < 11; i++ {
		p.Questions = append(p.Questions, slices.QuestionDraft{About: "G-1", Text: "Question?"})
	}
	sp := newSP(t, []spResult{{p: p}}, nil)
	op := sp.accepted(t, sp.post("POST", slicesURL(""), createBody), "propose-slice", "S-1")
	if op.Status != "failed" || op.Error == nil || op.Error.Code != "TooManyQuestions" {
		t.Errorf("operation %+v, want failed with TooManyQuestions", op)
	}
}

func TestCreateSliceThatFailsLeavesNoSlice(t *testing.T) {
	sp := newSP(t, []spResult{{err: slices.ErrMultipleUseCases}}, nil)
	sp.accepted(t, sp.post("POST", slicesURL(""), createBody), "propose-slice", "S-1")
	sp.do("GET", spURL("S-1", ""), clientToken, "", nil).problem(t, 404, "NotFound")
}

// ---- replaceGoals, replaceRules, replaceContract ----

func TestReplaceGoalsChangesThePartAndWithdrawsItsApproval(t *testing.T) {
	sp := newNotes(t)
	before := sp.approveAfterSeed(t)
	goals := before.Goals
	goals[0].Text = "Keep a note for good"
	op := sp.accepted(t, sp.post("PUT", spURL("S-1", "/goals"), jsonOf(t, map[string]any{"goals": goals})), "replace-goals", "S-1")
	if op.Status != "succeeded" || op.Result["slice"] != "S-1" {
		t.Fatalf("operation %+v, want succeeded with result.slice S-1", op)
	}
	got, _ := sp.getSlice(t, "S-1")
	if got.Goals[0].Text != "Keep a note for good" {
		t.Errorf("goal text %q, not replaced", got.Goals[0].Text)
	}
	if got.Parts["goals"] == before.Parts.Goals {
		t.Errorf("the goals hash did not change")
	}
	for _, a := range got.Approvals {
		if a.Part == "goals" {
			t.Errorf("the goals approval holds after the goals changed: %+v", a)
		}
	}
	if got.History[len(got.History)-1].Kind != "changed" {
		t.Errorf("history ends with %q, want changed", got.History[len(got.History)-1].Kind)
	}
	sp.noWork(t)
}

func (sp *spRig) approveAfterSeed(t *testing.T) slices.Slice {
	t.Helper()
	sp.seed(t, "Let me keep notes.")
	return sp.approveAll(t, "S-1")
}

func TestReplaceRulesChangesThePart(t *testing.T) {
	sp := newNotes(t)
	before := sp.approveAfterSeed(t)
	rules := before.Rules
	rules[0].Response = "shall store the note durably"
	for i := range rules {
		if rules[i].Terms == nil {
			rules[i].Terms = []string{}
		}
	}
	body := jsonOf(t, slices.RulesPart{Rules: rules, Examples: before.Examples, Terms: before.Terms})
	op := sp.accepted(t, sp.post("PUT", spURL("S-1", "/rules"), body), "replace-rules", "S-1")
	if op.Status != "succeeded" || op.Result["slice"] != "S-1" {
		t.Fatalf("operation %+v, want succeeded with result.slice S-1", op)
	}
	got, _ := sp.getSlice(t, "S-1")
	if got.Rules[0].Response != "shall store the note durably" || got.Parts["rules"] == before.Parts.Rules {
		t.Errorf("rules not replaced: %+v", got.Rules[0])
	}
	for _, a := range got.Approvals {
		if a.Part == "rules" {
			t.Errorf("the rules approval holds after the rules changed")
		}
	}
	sp.noWork(t)
}

func TestReplaceContractChangesThePart(t *testing.T) {
	sp := newNotes(t)
	before := sp.approveAfterSeed(t)
	contract := before.Contract + "# reviewed\n"
	op := sp.accepted(t, sp.post("PUT", spURL("S-1", "/contract"), jsonOf(t, map[string]string{"contract": contract})), "replace-contract", "S-1")
	if op.Status != "succeeded" || op.Result["slice"] != "S-1" {
		t.Fatalf("operation %+v, want succeeded with result.slice S-1", op)
	}
	got, _ := sp.getSlice(t, "S-1")
	if got.Contract != contract || got.Parts["contract"] == before.Parts.Contract {
		t.Errorf("contract not replaced")
	}
	for _, a := range got.Approvals {
		if a.Part == "contract" {
			t.Errorf("the contract approval holds after the contract changed")
		}
	}
	sp.noWork(t)
}

// ---- approvePart ----

func approvalBody(part, hash string) string {
	return `{"part":"` + part + `","content_hash":"` + hash + `"}`
}

func TestApprovePartAppliesTheApprovalAtTheShownHash(t *testing.T) {
	sp := newNotes(t)
	sl := sp.seed(t, "Let me keep notes.")
	for i, p := range []struct{ part, hash string }{{"goals", sl.Parts.Goals}, {"rules", sl.Parts.Rules}, {"contract", sl.Parts.Contract}} {
		op := sp.accepted(t, sp.post("POST", spURL("S-1", "/approvals"), approvalBody(p.part, p.hash)), "approve-part", "S-1")
		if op.Status != "succeeded" || op.Result["slice"] != "S-1" {
			t.Fatalf("approve %s: operation %+v", p.part, op)
		}
		got, _ := sp.getSlice(t, "S-1")
		if len(got.Approvals) != i+1 || got.Approvals[i].Part != p.part || got.Approvals[i].ContentHash != p.hash {
			t.Fatalf("approvals after %s: %+v", p.part, got.Approvals)
		}
	}
	if got, _ := sp.getSlice(t, "S-1"); got.State != "approved" {
		t.Errorf("state %q after approving every part, want approved", got.State)
	}
	sp.noWork(t)
}

func TestApprovePartRefusesAPartThatFailsItsChecksAtOnce(t *testing.T) {
	sp := newNotes(t, func(c *slices.Config) {
		c.Lint = spLint{findings: []slices.LintFinding{{Level: "error", Code: "oas3-schema", Text: "invalid"}}}
	})
	sl := sp.seed(t, "Let me keep notes.")
	p := sp.post("POST", spURL("S-1", "/approvals"), approvalBody("contract", sl.Parts.Contract)).
		problem(t, http.StatusConflict, "PartFailsChecks")
	if p.Status == nil || *p.Status != 409 || p.Detail == nil {
		t.Errorf("problem lacks status 409 or detail: %+v", p)
	}
	if p.Failures == nil || len(*p.Failures) == 0 {
		t.Fatalf("problem lacks the failing checks")
	}
	for _, f := range *p.Failures {
		if f.Passed || f.Part != "contract" || f.Check == "" || f.Reason == "" {
			t.Errorf("failure %+v must be a failed contract check with a reason", f)
		}
	}
	sp.noOperation(t)
}

func TestApprovePartRefusesWhileAQuestionConcernsThePart(t *testing.T) {
	p := spNotes(t)
	p.Questions = []slices.QuestionDraft{{About: "R-1", Text: "Keep forever?", Options: []string{"yes", "no"}}}
	sp := newSP(t, []spResult{{p: p}}, nil)
	sl := sp.seed(t, "Let me keep notes.")
	sp.post("POST", spURL("S-1", "/approvals"), approvalBody("rules", sl.Parts.Rules)).problem(t, 409, "ContradictionOpen")
	sp.noOperation(t)
}

func TestApprovePartRefusesAStaleHash(t *testing.T) {
	sp := newNotes(t)
	sp.seed(t, "Let me keep notes.")
	sp.post("POST", spURL("S-1", "/approvals"), approvalBody("goals", strings.Repeat("a", 64))).problem(t, 409, "StaleContentHash")
	sp.noOperation(t)
}

func TestApprovePartRefusesAContractTheLackingPatternDoesNotFit(t *testing.T) {
	sp := newNotes(t, func(c *slices.Config) { c.Pattern = nil }) // nil fails closed
	sl := sp.seed(t, "Let me keep notes.")
	p := sp.post("POST", spURL("S-1", "/approvals"), approvalBody("contract", sl.Parts.Contract)).
		problem(t, 409, "PatternDoesNotFit")
	if p.Failures == nil || len(*p.Failures) == 0 {
		t.Errorf("PatternDoesNotFit must list the failing conditions in failures")
	}
	sp.noOperation(t)
}

// ---- answerQuestions ----

func twoQuestionRig(t *testing.T) *spRig {
	t.Helper()
	first := spNotes(t)
	first.Questions = []slices.QuestionDraft{
		{About: "R-1", Text: "Keep forever?", Options: []string{"yes", "no"}},
		{About: "G-1", Text: "Which notes?"},
	}
	revised := spNotes(t)
	revised.Goals[0].Text = "Keep a note for good"
	sp := newSP(t, []spResult{{p: first}, {p: revised}}, nil)
	sp.seed(t, "Let me keep notes.")
	return sp
}

const answersBody = `{"answers":[{"question":"Q-1","answer":{"option":0}},{"question":"Q-2","answer":{"text":"All of them"}}]}`

func TestAnswerQuestionsRevisesThroughTheAnalyst(t *testing.T) {
	sp := twoQuestionRig(t)
	op := sp.accepted(t, sp.post("POST", spURL("S-1", "/answers"), answersBody), "answer-questions", "S-1")
	if op.Status != "succeeded" || op.Result["slice"] != "S-1" {
		t.Fatalf("operation %+v, want succeeded with result.slice S-1", op)
	}
	sp.analyst.mu.Lock()
	inputs := append([]slices.AnalystInput(nil), sp.analyst.inputs...)
	sp.analyst.mu.Unlock()
	if len(inputs) != 2 {
		t.Fatalf("analyst called %d times, want 2 (proposal, revision)", len(inputs))
	}
	qs := inputs[1].Questions
	if len(qs) != 2 || qs[0].Answer == nil || qs[0].Answer.Option == nil || *qs[0].Answer.Option != 0 ||
		qs[1].Answer == nil || qs[1].Answer.Text != "All of them" {
		t.Errorf("the analyst must get the answers: %+v", qs)
	}
	got, _ := sp.getSlice(t, "S-1")
	if got.Goals[0].Text != "Keep a note for good" || got.State != "checked" {
		t.Errorf("the revision was not applied: goal %q, state %q", got.Goals[0].Text, got.State)
	}
	for _, q := range got.Questions {
		if q.Answer == nil || q.AnsweredAt == nil {
			t.Errorf("question %s lacks its answer", q.ID)
		}
	}
	sp.noWork(t)
}

func TestAnswerQuestionsRefusesAQuestionThatIsNotOpenAtOnce(t *testing.T) {
	sp := twoQuestionRig(t)
	unknown := `{"answers":[{"question":"Q-9","answer":{"text":"x"}}]}`
	sp.post("POST", spURL("S-1", "/answers"), unknown).problem(t, 409, "QuestionNotOpen")
	sp.noOperation(t)
	sp.accepted(t, sp.post("POST", spURL("S-1", "/answers"), answersBody), "answer-questions", "S-1")
	again := `{"answers":[{"question":"Q-1","answer":{"option":1}}]}`
	sp.post("POST", spURL("S-1", "/answers"), again).problem(t, 409, "QuestionNotOpen")
	if n := sp.analyst.calls(); n != 2 {
		t.Errorf("analyst called %d times, want 2: a refused answer must not reach it", n)
	}
}

func TestAnswerQuestionsRefusesAnOptionOutOfRangeWith400(t *testing.T) {
	sp := twoQuestionRig(t)
	for name, body := range map[string]string{
		"option out of range": `{"answers":[{"question":"Q-1","answer":{"option":3}}]}`,
		"option on free text": `{"answers":[{"question":"Q-2","answer":{"option":0}}]}`,
		"text over 200 chars": `{"answers":[{"question":"Q-2","answer":{"text":"` + strings.Repeat("x", 201) + `"}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			sp.post("POST", spURL("S-1", "/answers"), body).problem(t, 400, "InvalidRequest")
		})
	}
	sp.noOperation(t)
	if n := sp.analyst.calls(); n != 1 {
		t.Errorf("analyst called %d times, want 1", n)
	}
}

func TestAnswerQuestionsEndsFailedWithTooManyQuestions(t *testing.T) {
	first := spNotes(t)
	first.Questions = []slices.QuestionDraft{{About: "G-1", Text: "Which notes?"}}
	revised := spNotes(t)
	for i := 0; i < 10; i++ {
		revised.Questions = append(revised.Questions, slices.QuestionDraft{About: "G-1", Text: "More?"})
	}
	sp := newSP(t, []spResult{{p: first}, {p: revised}}, nil)
	sp.seed(t, "Let me keep notes.")
	op := sp.accepted(t, sp.post("POST", spURL("S-1", "/answers"), `{"answers":[{"question":"Q-1","answer":{"text":"all"}}]}`), "answer-questions", "S-1")
	if op.Status != "failed" || op.Error == nil || op.Error.Code != "TooManyQuestions" {
		t.Errorf("operation %+v, want failed with TooManyQuestions", op)
	}
}

// ---- deleteSlice ----

func TestDeleteSliceErasesTheSliceAndItsStoredRecords(t *testing.T) {
	sp := newNotes(t)
	sp.seed(t, "one")
	sp.seed(t, "two")
	op := sp.accepted(t, sp.post("DELETE", spURL("S-1", ""), ""), "delete-slice", "S-1")
	if op.Status != "succeeded" {
		t.Fatalf("operation %+v, want succeeded", op)
	}
	sp.do("GET", spURL("S-1", ""), clientToken, "", nil).problem(t, 404, "NotFound")
	if got := sp.store.erasures(); len(got) != 1 || got[0] != "S-1" {
		t.Errorf("EraseSlice calls %v, want [S-1]", got)
	}
	if l := sp.list(t, clientToken, ""); len(l.Items) != 1 || l.Items[0].ID != "S-2" {
		t.Errorf("list after delete: %+v, want S-2 alone", l.Items)
	}
	sp.noWork(t)
}

// ---- strict bodies ----

func TestBodiesAreStrict(t *testing.T) {
	sp := twoQuestionRig(t)
	goal := `{"id":"G-1","text":"t"}`
	for _, c := range []struct{ name, method, path, body string }{
		{"create unknown field", "POST", slicesURL(""), `{"description":"d","extra":1}`},
		{"create wrong type", "POST", slicesURL(""), `{"description":5}`},
		{"create empty description", "POST", slicesURL(""), `{"description":""}`},
		{"create missing description", "POST", slicesURL(""), `{}`},
		{"create not JSON", "POST", slicesURL(""), `not json`},
		{"goals unknown field", "PUT", spURL("S-1", "/goals"), `{"goals":[` + goal + `],"extra":1}`},
		{"goals wrong type", "PUT", spURL("S-1", "/goals"), `{"goals":"x"}`},
		{"goals empty", "PUT", spURL("S-1", "/goals"), `{"goals":[]}`},
		{"rules unknown field", "PUT", spURL("S-1", "/rules"), `{"rules":[],"examples":"x","terms":[],"extra":1}`},
		{"rules wrong type", "PUT", spURL("S-1", "/rules"), `{"rules":5,"examples":"x","terms":[]}`},
		{"contract unknown field", "PUT", spURL("S-1", "/contract"), `{"contract":"a: b\n","extra":1}`},
		{"contract wrong type", "PUT", spURL("S-1", "/contract"), `{"contract":5}`},
		{"answers unknown field", "POST", spURL("S-1", "/answers"), `{"answers":[{"question":"Q-1","answer":{"option":0}}],"extra":1}`},
		{"answers empty", "POST", spURL("S-1", "/answers"), `{"answers":[]}`},
		{"answers wrong type", "POST", spURL("S-1", "/answers"), `{"answers":"x"}`},
		{"approval unknown part", "POST", spURL("S-1", "/approvals"), approvalBody("bogus", strings.Repeat("a", 64))},
		{"approval bad hash", "POST", spURL("S-1", "/approvals"), approvalBody("goals", "XYZ")},
		{"approval unknown field", "POST", spURL("S-1", "/approvals"), `{"part":"goals","content_hash":"` + strings.Repeat("a", 64) + `","extra":1}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			sp.post(c.method, c.path, c.body).problem(t, 400, "InvalidRequest")
		})
	}
	sp.noOperation(t)
	sp.noWork(t)
	if got, _ := sp.getSlice(t, "S-1"); got.State != "questions-open" || len(got.History) != 1 {
		t.Errorf("a refused body changed the slice: state %q, %d history entries", got.State, len(got.History))
	}
}

// ---- unknown slice ----

func TestEveryPerSliceOperationAnswers404ForAnUnknownSlice(t *testing.T) {
	sp := newNotes(t)
	sp.seed(t, "one")
	goal := `{"goals":[{"id":"G-1","text":"t"}]}`
	rules := `{"rules":[{"id":"R-1","goal":"G-1","pattern":"ubiquitous","system":"s","response":"shall x","terms":[]}],"examples":"Feature: F\n","terms":[]}`
	for _, c := range []struct{ name, method, path, body string }{
		{"getSlice", "GET", spURL("S-99", ""), ""},
		{"deleteSlice", "DELETE", spURL("S-99", ""), ""},
		{"replaceGoals", "PUT", spURL("S-99", "/goals"), goal},
		{"replaceRules", "PUT", spURL("S-99", "/rules"), rules},
		{"replaceContract", "PUT", spURL("S-99", "/contract"), `{"contract":"openapi: 3.1.0\n"}`},
		{"answerQuestions", "POST", spURL("S-99", "/answers"), `{"answers":[{"question":"Q-1","answer":{"option":0}}]}`},
		{"approvePart", "POST", spURL("S-99", "/approvals"), approvalBody("goals", strings.Repeat("a", 64))},
		{"startRun", "POST", spURL("S-99", "/runs"), `{"project":"notes"}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			sp.post(c.method, c.path, c.body).problem(t, 404, "NotFound")
		})
	}
	sp.noOperation(t)
	if got := sp.store.erasures(); len(got) != 0 {
		t.Errorf("EraseSlice called for an unknown slice: %v", got)
	}
}
