package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/slice"
)

// SliceStore is what the API needs from the run store about a slice.
type SliceStore interface {
	// ArtifactCount returns the number of distinct artifacts stored for the slice's runs.
	ArtifactCount(sliceID string) (int, error)
	// EraseSlice removes the slice's run records and stored artifacts; erasing again does nothing.
	EraseSlice(sliceID string) error
}

// sliceServer serves the slice operations from a slices.Service and the run store; every other
// operation is the skeleton server's. A refusal the contract lists under a status code comes at
// once, before any operation exists; every accepted change is an operation whose work calls the
// Service. Nothing can be built yet, so startRun ends in a refusal.
type sliceServer struct {
	*server
	svc   *slices.Service
	store SliceStore
}

var _ gen.StrictServerInterface = (*sliceServer)(nil)

func (s *sliceServer) CreateSlice(_ context.Context, req gen.CreateSliceRequestObject) (gen.CreateSliceResponseObject, error) {
	description := req.Body.Description
	id := s.svc.Reserve() // the operation names the slice before it exists
	acc := s.start("createSlice", gen.OperationKindProposeSlice, id, func(ctx context.Context) (WorkResult, error) {
		var res slices.Result
		err := s.withAnalyst(ctx, func() (err error) {
			res, err = s.svc.CreateAs(ctx, id, description)
			return err
		})
		switch {
		case err != nil:
			return WorkResult{}, err
		case res.Reused:
			return WorkResult{ReusedSlice: res.Slice.ID}, nil
		}
		return WorkResult{Slice: id}, nil
	})
	return gen.CreateSlice202JSONResponse{AcceptedJSONResponse: acc}, nil
}

func (s *sliceServer) ListSlices(_ context.Context, req gen.ListSlicesRequestObject) (gen.ListSlicesResponseObject, error) {
	all := s.svc.List()
	numbers := make([]int, len(all)) // List gives the slices by number, highest first
	for i, sl := range all {
		numbers[i], _ = strconv.Atoi(strings.TrimPrefix(sl.ID, "S-"))
	}
	from, to, next, ok := s.pages.keysetPage(slicesList, req.Params.PageToken, numbers)
	if !ok {
		return problemResponse(badPageToken()), nil
	}
	list := gen.SliceList{Items: make([]gen.SliceSummary, 0, to-from), NextPageToken: next}
	for _, sl := range all[from:to] {
		list.Items = append(list.Items, gen.SliceSummary{Id: sl.ID, Title: title(sl), State: gen.State(sl.State)})
	}
	return gen.ListSlices200JSONResponse{SliceListJSONResponse: gen.SliceListJSONResponse{Body: list}}, nil
}

func (s *sliceServer) GetSlice(_ context.Context, req gen.GetSliceRequestObject) (gen.GetSliceResponseObject, error) {
	sl, err := s.svc.Get(req.SliceIdParameter)
	if err != nil {
		return s.refuse("getSlice", err)
	}
	n, err := s.store.ArtifactCount(sl.ID)
	if err != nil {
		return nil, fmt.Errorf("artifact count of %s: %w", sl.ID, err)
	}
	return gen.GetSlice200JSONResponse{SliceJSONResponse: gen.SliceJSONResponse{Body: sliceView(sl, n)}}, nil
}

func (s *sliceServer) DeleteSlice(_ context.Context, req gen.DeleteSliceRequestObject) (gen.DeleteSliceResponseObject, error) {
	id := req.SliceIdParameter
	if _, err := s.svc.Get(id); err != nil {
		return s.refuse("deleteSlice", err)
	}
	// The stored records go first: erasing them again is harmless, so a retry after a failed
	// erasure completes it, and the slice disappears only once its records are gone.
	return gen.DeleteSlice202JSONResponse{AcceptedJSONResponse: s.change("deleteSlice", gen.OperationKindDeleteSlice, id, func(ctx context.Context) error {
		if err := s.store.EraseSlice(id); err != nil {
			return Retryable(err)
		}
		return s.svc.Delete(ctx, id)
	})}, nil
}

func (s *sliceServer) ReplaceGoals(_ context.Context, req gen.ReplaceGoalsRequestObject) (gen.ReplaceGoalsResponseObject, error) {
	id := req.SliceIdParameter
	if _, err := s.svc.Get(id); err != nil {
		return s.refuse("replaceGoals", err)
	}
	goals := make([]slices.Goal, len(req.Body.Goals))
	for i, g := range req.Body.Goals {
		goals[i] = slices.Goal{ID: g.Id, Text: g.Text}
	}
	return gen.ReplaceGoals202JSONResponse{AcceptedJSONResponse: s.change("replaceGoals", gen.OperationKindReplaceGoals, id, func(ctx context.Context) error {
		_, err := s.svc.ReplaceGoals(ctx, id, goals)
		return err
	})}, nil
}

func (s *sliceServer) ReplaceRules(_ context.Context, req gen.ReplaceRulesRequestObject) (gen.ReplaceRulesResponseObject, error) {
	id := req.SliceIdParameter
	if _, err := s.svc.Get(id); err != nil {
		return s.refuse("replaceRules", err)
	}
	part := slices.RulesPart{Examples: req.Body.Examples, Rules: make([]slices.Rule, len(req.Body.Rules)), Terms: make([]slices.Term, len(req.Body.Terms))}
	for i, r := range req.Body.Rules {
		part.Rules[i] = slices.Rule{ID: r.Id, Goal: r.Goal, Pattern: string(r.Pattern), Precondition: deref(r.Precondition),
			Trigger: deref(r.Trigger), Feature: deref(r.Feature), System: r.System, Response: r.Response, Terms: r.Terms}
	}
	for i, t := range req.Body.Terms {
		part.Terms[i] = slices.Term{ID: t.Id, Name: t.Name, Definition: t.Definition}
	}
	return gen.ReplaceRules202JSONResponse{AcceptedJSONResponse: s.change("replaceRules", gen.OperationKindReplaceRules, id, func(ctx context.Context) error {
		_, err := s.svc.ReplaceRules(ctx, id, part)
		return err
	})}, nil
}

func (s *sliceServer) ReplaceContract(_ context.Context, req gen.ReplaceContractRequestObject) (gen.ReplaceContractResponseObject, error) {
	id := req.SliceIdParameter
	if _, err := s.svc.Get(id); err != nil {
		return s.refuse("replaceContract", err)
	}
	contract := req.Body.Contract
	return gen.ReplaceContract202JSONResponse{AcceptedJSONResponse: s.change("replaceContract", gen.OperationKindReplaceContract, id, func(ctx context.Context) error {
		_, err := s.svc.ReplaceContract(ctx, id, contract)
		return err
	})}, nil
}

func (s *sliceServer) AnswerQuestions(_ context.Context, req gen.AnswerQuestionsRequestObject) (gen.AnswerQuestionsResponseObject, error) {
	id := req.SliceIdParameter
	answers := make([]slices.Answer, len(req.Body.Answers))
	for i, a := range req.Body.Answers {
		raw, err := a.AnswerValue.MarshalJSON()
		var v struct {
			Option *json.Number `json:"option"`
			Text   string       `json:"text"`
		}
		if err == nil {
			err = json.Unmarshal(raw, &v)
		}
		if err != nil {
			return nil, fmt.Errorf("answer %d: %w", i, err)
		}
		var option *int
		if v.Option != nil {
			n, ok := wholeNumber(*v.Option)
			if !ok {
				return s.refuse("answerQuestions", &slices.Error{Code: "InvalidRequest",
					Detail: fmt.Sprintf("the answer to %s gives option %s, which is not a whole number", a.Question, *v.Option)})
			}
			option = &n
		}
		answers[i] = slices.Answer{Question: a.Question, Option: option, Text: v.Text}
	}
	if err := s.svc.CheckAnswers(id, answers); err != nil {
		return s.refuse("answerQuestions", err)
	}
	return gen.AnswerQuestions202JSONResponse{AcceptedJSONResponse: s.change("answerQuestions", gen.OperationKindAnswerQuestions, id, func(ctx context.Context) error {
		return s.withAnalyst(ctx, func() error {
			_, err := s.svc.Answer(ctx, id, answers)
			return err
		})
	})}, nil
}

// wholeNumber reads a JSON number that is an integer as JSON Schema counts one, so 1.0 and 1e0 are
// 1. A value beyond int's range becomes the nearest int, which no option index reaches either.
func wholeNumber(n json.Number) (int, bool) {
	r, ok := new(big.Rat).SetString(n.String())
	if !ok || !r.IsInt() {
		return 0, false
	}
	switch x := r.Num(); {
	case x.Cmp(big.NewInt(math.MaxInt)) > 0:
		return math.MaxInt, true
	case x.Cmp(big.NewInt(math.MinInt)) < 0:
		return math.MinInt, true
	default:
		return int(x.Int64()), true
	}
}

func (s *sliceServer) ApprovePart(_ context.Context, req gen.ApprovePartRequestObject) (gen.ApprovePartResponseObject, error) {
	id, part, hash := req.SliceIdParameter, slices.Part(req.Body.Part), req.Body.ContentHash
	if err := s.svc.CheckApprove(id, part, hash); err != nil {
		return s.refuse("approvePart", err)
	}
	return gen.ApprovePart202JSONResponse{AcceptedJSONResponse: s.change("approvePart", gen.OperationKindApprovePart, id, func(ctx context.Context) error {
		_, err := s.svc.Approve(ctx, id, part, hash)
		return err
	})}, nil
}

// StartRun refuses in the contract's order: an unknown slice, then a pattern condition that
// fails, then a part not approved at its current hash, then a step without a ready worker. No
// step worker registers yet, so a slice that passes the rest is refused with WorkersNotReady.
func (s *sliceServer) StartRun(_ context.Context, req gen.StartRunRequestObject) (gen.StartRunResponseObject, error) {
	if err := s.svc.Buildable(req.SliceIdParameter); err != nil {
		return s.refuse("startRun", err)
	}
	return s.refuse("startRun", &slices.Error{Code: string(gen.ErrorCodeWorkersNotReady), Detail: "a step of the pattern has no registered worker"})
}

// change starts the operation of a change to slice id; its result is the slice.
func (s *sliceServer) change(opID string, kind gen.OperationKind, id string, apply func(ctx context.Context) error) gen.AcceptedJSONResponse {
	return s.start(opID, kind, id, func(ctx context.Context) (WorkResult, error) {
		if err := apply(ctx); err != nil {
			return WorkResult{}, err
		}
		return WorkResult{Slice: id}, nil
	})
}

// start starts an operation of the contract operation opID whose work calls the Service. A
// refusal of the Service keeps its code when the contract lists that code for opID's
// operation; any other refusal, such as one from a change that came between the check made at
// acceptance and the work, ends the operation OperationFailed with the refusal in its detail.
// Either way the failing checks the refusal names stay in the error's `failures`.
func (s *sliceServer) start(opID string, kind gen.OperationKind, target string, work func(ctx context.Context) (WorkResult, error)) gen.AcceptedJSONResponse {
	op := s.contract.op(opID)
	return gen.AcceptedJSONResponse{Body: s.ops.startWith(kind, target, func(ctx context.Context) (WorkResult, error) {
		res, err := work(ctx)
		var e *slices.Error
		switch {
		case err == nil || !errors.As(err, &e):
			return res, err
		case op.lists("operation", e.Code):
			return WorkResult{}, &WorkError{Code: gen.ErrorCode(e.Code), Detail: e.Detail, Failures: e.Failures}
		}
		return WorkResult{}, &WorkError{Code: gen.ErrorCodeOperationFailed, Detail: e.Error(), Failures: e.Failures}
	})}
}

// withAnalyst runs f, which may call the analyst, once one of the engine's analystCallsAtOnce
// places is free, and frees it when f returns. While it waits the
// operation stays running; cancelled while it waits, it returns ctx's error without running f.
func (s *sliceServer) withAnalyst(ctx context.Context, f func() error) error {
	select {
	case s.analyst <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.analyst }()
	if err := ctx.Err(); err != nil { // cancelled as the place came free
		return err
	}
	return f()
}

// refuse answers a refusal of the Service at once, with the status code under which the
// contract's x-error-codes list its code for opID, and the failing checks in `failures`. Any
// other error, or a code the contract does not list for opID, is the engine's fault.
func (s *sliceServer) refuse(opID string, err error) (problemResponse, error) {
	var e *slices.Error
	if !errors.As(err, &e) {
		return problemResponse{}, err
	}
	op := s.contract.op(opID)
	for _, status := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict} {
		if op.lists(strconv.Itoa(status), e.Code) {
			p := newProblem(status, gen.ErrorCode(e.Code), e.Detail)
			if len(e.Failures) > 0 {
				f := checkResults(e.Failures)
				p.Failures = &f
			}
			return problemResponse(p), nil
		}
	}
	return problemResponse{}, fmt.Errorf("%s: the contract lists no status for %w", opID, err)
}

// problemResponse is a problem as the response of any slice operation.
type problemResponse gen.Problem

func (p problemResponse) write(w http.ResponseWriter) error {
	writeProblem(w, gen.Problem(p))
	return nil
}

func (p problemResponse) VisitCreateSliceResponse(w http.ResponseWriter) error     { return p.write(w) }
func (p problemResponse) VisitListSlicesResponse(w http.ResponseWriter) error      { return p.write(w) }
func (p problemResponse) VisitGetSliceResponse(w http.ResponseWriter) error        { return p.write(w) }
func (p problemResponse) VisitDeleteSliceResponse(w http.ResponseWriter) error     { return p.write(w) }
func (p problemResponse) VisitReplaceGoalsResponse(w http.ResponseWriter) error    { return p.write(w) }
func (p problemResponse) VisitReplaceRulesResponse(w http.ResponseWriter) error    { return p.write(w) }
func (p problemResponse) VisitReplaceContractResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitAnswerQuestionsResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitApprovePartResponse(w http.ResponseWriter) error     { return p.write(w) }
func (p problemResponse) VisitStartRunResponse(w http.ResponseWriter) error        { return p.write(w) }

// title is a slice's title: its first goal, or its description while it has none.
func title(sl slices.Slice) string {
	if len(sl.Goals) > 0 {
		return sl.Goals[0].Text
	}
	return sl.Description
}

// sliceView is the slice as the contract shows it (slice.schema.json), with artifactCount
// distinct artifacts stored for its runs. No run exists yet, so latest_run is never set.
func sliceView(sl slices.Slice, artifactCount int) gen.Slice {
	v := gen.Slice{Id: sl.ID, Title: title(sl), Description: sl.Description, State: gen.SliceSchemaState(sl.State),
		Examples: sl.Examples, Contract: sl.Contract, ArtifactCount: &artifactCount, Checks: checkResults(sl.Checks),
		Goals: make([]gen.Goal, len(sl.Goals)), Terms: make([]gen.Term, len(sl.Terms)), Rules: make([]gen.Rule, len(sl.Rules)),
		Questions: make([]gen.Question, len(sl.Questions)), RuleText: map[string]string{}}
	v.Parts.Goals, v.Parts.Rules, v.Parts.Contract = sl.Parts.Goals, sl.Parts.Rules, sl.Parts.Contract
	for i, g := range sl.Goals {
		v.Goals[i] = gen.Goal{Id: g.ID, Text: g.Text}
	}
	for i, t := range sl.Terms {
		v.Terms[i] = gen.Term{Id: t.ID, Name: t.Name, Definition: t.Definition}
	}
	for i, r := range sl.Rules {
		terms := r.Terms
		if terms == nil {
			terms = []string{}
		}
		v.Rules[i] = gen.Rule{Id: r.ID, Goal: r.Goal, Pattern: gen.RulePattern(r.Pattern), Precondition: opt(r.Precondition),
			Trigger: opt(r.Trigger), Feature: opt(r.Feature), System: r.System, Response: r.Response, Terms: terms}
		// A rule EARS cannot write a sentence for fails the Schema check, which reports it.
		if text, err := slice.EARS(slice.Rule(r)); err == nil {
			v.RuleText[r.ID] = text
		}
	}
	for i, q := range sl.Questions {
		v.Questions[i] = gen.Question{Id: q.ID, About: q.About, Text: q.Text}
		if len(q.Options) > 0 {
			options := append([]string(nil), q.Options...)
			v.Questions[i].Options = &options
		}
		if q.Answer != nil {
			var a gen.AnswerValue
			if q.Answer.Option != nil {
				_ = a.FromAnswer0(gen.Answer0{Option: *q.Answer.Option}) // marshalling a struct of an int cannot fail
			} else {
				_ = a.FromAnswer1(gen.Answer1{Text: q.Answer.Text})
			}
			at := q.AnsweredAt
			v.Questions[i].AnswerValue, v.Questions[i].AnsweredAt = &a, &at
		}
	}
	sized(&v.Approvals, len(sl.Approvals))
	for i, a := range sl.Approvals {
		v.Approvals[i].Part, v.Approvals[i].ContentHash, v.Approvals[i].At = gen.Part(a.Part), a.ContentHash, a.At
	}
	sized(&v.History, len(sl.History))
	for i, h := range sl.History {
		e := &v.History[i]
		e.Kind, e.At, e.Model, e.AgentDigest = gen.SliceSchemaHistoryKind(h.Kind), h.At, opt(h.Model), opt(h.AgentDigest)
		e.Refs = make([]gen.SliceSchema_History_Refs_Item, len(h.Refs))
		for j, ref := range h.Refs {
			if h.Kind == "answered" {
				_ = e.Refs[j].FromQuestionId(ref) // marshalling a string cannot fail
			} else {
				_ = e.Refs[j].FromPartRef(ref)
			}
		}
	}
	return v
}

func checkResults(checks []slices.Check) []gen.CheckResult {
	out := make([]gen.CheckResult, len(checks))
	for i, c := range checks {
		out[i] = gen.CheckResult{Part: c.Part, Check: c.Check, Passed: c.Passed, Reason: c.Reason}
	}
	return out
}

// sized sets *s to n zero elements, for the generated list types whose elements have no name.
func sized[S ~[]E, E any](s *S, n int) { *s = make(S, n) }

func opt(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
