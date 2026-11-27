package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
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
// Service. startRun and registerWorker are in workers.go.
type sliceServer struct {
	*server
	svc     *slices.Service
	store   SliceStore
	workers *Workers
	// projects is Config.Projects; runs are the slices' runs (runs.go), nil when Store keeps no
	// run record.
	projects string
	runs     *runs
	// captured keeps the captured patterns (captured.go), nil when Store keeps none; cmu
	// serialises their acceptance checks and record changes.
	captured CapturedStore
	cmu      sync.Mutex
	// trialSets keeps the trial sets (trial_sets.go), nil when Store keeps none; tmu serialises
	// their acceptance checks and record changes, and is taken before the runs' lock.
	trialSets TrialSetStore
	tmu       sync.Mutex
}

var _ gen.StrictServerInterface = (*sliceServer)(nil)

func (s *sliceServer) CreateSlice(ctx context.Context, _ gen.CreateSliceRequestObject) (gen.CreateSliceResponseObject, error) {
	op, err := s.ops.accept(ctx, "createSlice", s.svc.Reserve()) // the operation names the slice before it exists
	if err != nil {
		return unavailableFor(err), nil
	}
	return gen.CreateSlice202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// useOperations makes the operations' work the slices' and the runs', reserves again the slice
// of every createSlice that has not ended (no other slice gets it, and its work creates it), and
// lets cancelling a run operation cancel its run.
func (s *sliceServer) useOperations() {
	s.ops.work = s.work
	if open, err := s.ops.records.OpenOperations(); err == nil {
		for _, rec := range open {
			if rec.Kind == "createSlice" {
				if err := s.svc.ReserveAgain(rec.Target); err != nil {
					log.Printf("engine: reserving slice %s for operation %s again: %v", rec.Target, rec.ID, err)
				}
			}
		}
	} else {
		log.Printf("engine: reading the open operations: %v", err)
	}
	if s.runs != nil {
		total := len(s.runs.pattern.Steps)
		s.ops.progress = func(run string) *operationProgress {
			r, _, _, ok, err := s.runs.find(run)
			if err != nil || !ok {
				return nil
			}
			return &operationProgress{Done: min(len(r.Accepted), total), Total: total}
		}
		if s.runs.durable != nil {
			s.ops.cancelRun = s.runs.durable.Cancel
		}
		if s.trialSets != nil {
			s.ops.cancelTrials = s.cancelTrialSet
		}
	}
}

// work does what an accepted operation's request asked, from its record and body: a run
// operation follows its run; a run-trials operation runs its set's trials (runTrials); a slice
// operation calls the Service, and a repeat finds the work it did before (data model, the repeat
// table).
func (s *sliceServer) work(ctx context.Context, rec store.OperationRecord, body []byte) (WorkResult, error) {
	if isRunKind(rec.Kind) {
		return s.follow(ctx, rec.Target)
	}
	if isTrialsKind(rec.Kind) {
		return s.runTrials(ctx, rec, body)
	}
	if isCapturedKind(rec.Kind) {
		return s.capturedWork(ctx, rec, body)
	}
	if rec.Kind != "deleteSlice" && len(body) == 0 {
		return WorkResult{}, fmt.Errorf("operation %s has no request body", rec.ID)
	}
	id := rec.Target
	res := WorkResult{Slice: id}
	var err error
	switch rec.Kind {
	case "createSlice":
		res, err = s.createSlice(ctx, id, body)
	case "deleteSlice":
		err = s.deleteSlice(ctx, id, rec.ID)
	case "replaceGoals":
		var req gen.ReplaceGoalsJSONRequestBody
		if err = json.Unmarshal(body, &req); err == nil {
			_, err = s.svc.ReplaceGoals(ctx, id, goalsOf(req))
		}
	case "replaceRules":
		var req gen.ReplaceRulesJSONRequestBody
		if err = json.Unmarshal(body, &req); err == nil {
			_, err = s.svc.ReplaceRules(ctx, id, rulesOf(req))
		}
	case "replaceContract":
		var req gen.ReplaceContractJSONRequestBody
		if err = json.Unmarshal(body, &req); err == nil {
			_, err = s.svc.ReplaceContract(ctx, id, req.Contract)
		}
	case "answerQuestions":
		err = s.answerQuestions(ctx, id, body)
	case "approvePart":
		err = s.approvePart(ctx, id, body)
	default:
		return WorkResult{}, fmt.Errorf("operation %s: no work for the kind %s", rec.ID, rec.Kind)
	}
	if err != nil {
		return WorkResult{}, s.workError(rec.Kind, err)
	}
	return res, nil
}

// createSlice creates the slice under its reserved ID; a slice that exists already is the work
// done before a stop.
func (s *sliceServer) createSlice(ctx context.Context, id string, body []byte) (WorkResult, error) {
	var req gen.CreateSliceJSONRequestBody
	if err := json.Unmarshal(body, &req); err != nil {
		return WorkResult{}, err
	}
	if _, err := s.svc.Get(id); err == nil {
		return WorkResult{Slice: id}, nil
	}
	var res slices.Result
	err := s.withAnalyst(ctx, func() (err error) {
		res, err = s.svc.CreateAs(ctx, id, req.Description)
		return err
	})
	switch {
	case err != nil:
		return WorkResult{}, err
	case res.Reused:
		return WorkResult{ReusedSlice: res.Slice.ID}, nil
	}
	return WorkResult{Slice: id}, nil
}

// workError is a refusal of the Service as the operation's error: its code when the contract
// lists that code for opID's operation; any other refusal, such as one from a change that came
// between the check made at acceptance and the work, OperationFailed with the refusal in its
// detail. Either way the failing checks the refusal names stay in the error's `failures`.
func (s *sliceServer) workError(opID string, err error) error {
	var e *slices.Error
	switch {
	case !errors.As(err, &e):
		return err
	case s.contract.op(opID).lists("operation", e.Code):
		return &WorkError{Code: gen.ErrorCode(e.Code), Detail: e.Detail, Failures: e.Failures}
	}
	return &WorkError{Code: gen.ErrorCodeOperationFailed, Detail: e.Error(), Failures: e.Failures}
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
		list.Items = append(list.Items, gen.SliceSummary{Id: sl.ID, Title: title(sl), State: gen.State(sl.State), LatestRun: s.latestRun(sl.ID),
			ProofOf: opt(sl.ProofOf)})
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
	v := sliceView(sl, n)
	v.LatestRun = s.latestRun(sl.ID)
	return gen.GetSlice200JSONResponse{SliceJSONResponse: gen.SliceJSONResponse{Body: v}}, nil
}

// latestRun is the ID of the slice's latest run (the highest run number the record holds for
// it), nil before its first run or when Store keeps no run record.
func (s *sliceServer) latestRun(slice string) *gen.RunId {
	if s.runs == nil {
		return nil
	}
	var latest *gen.RunId
	for _, e := range s.runs.st.Runs() { // by run number, lowest first
		if e.Slice == slice {
			id := e.Run
			latest = &id
		}
	}
	return latest
}

func (s *sliceServer) DeleteSlice(ctx context.Context, req gen.DeleteSliceRequestObject) (gen.DeleteSliceResponseObject, error) {
	id := req.SliceIdParameter
	if _, err := s.svc.Get(id); err != nil {
		return s.refuse("deleteSlice", err)
	}
	if err := s.noActiveRun(id); err != nil {
		return s.refuse("deleteSlice", err)
	}
	if err := s.noRunningSet(id); err != nil {
		return s.refuse("deleteSlice", err)
	}
	op, err := s.ops.accept(ctx, "deleteSlice", id)
	if err != nil {
		return unavailableFor(err), nil
	}
	return gen.DeleteSlice202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// deleteSlice erases the slice; a slice that no longer exists is the work done before a stop.
// The stored records go first: erasing them again is harmless, so a retry after a failed
// erasure completes it, and the slice disappears only once its records are gone. The work holds
// the runs' lock and checks again, so no run starts, resumes or records between the check and
// the erasure. A proof slice's captured pattern completes its proofs first, so a finished proof
// is kept, and forgets the slice as its proof slice last; it keeps its proofs and template. Before
// the store, the durable service forgets the slice: each run's workflow,
// and each other operation's workflow and then its record (data model, "Erasing a slice"); own,
// the erasing operation, is left to the retention.
func (s *sliceServer) deleteSlice(ctx context.Context, id, own string) error {
	var e *slices.Error
	if _, err := s.svc.Get(id); errors.As(err, &e) && e.Code == string(gen.ErrorCodeNotFound) {
		return s.forgetProofSlice(id)
	}
	if s.runs != nil {
		s.runs.mu.Lock()
		defer s.runs.mu.Unlock()
	}
	return s.eraseSlice(ctx, id, own)
}

// eraseSlice is deleteSlice's erasure of an existing slice, refused RunActive while a run of it
// has not ended or a trial set of it runs. The caller holds the runs' lock (or no runs are kept).
func (s *sliceServer) eraseSlice(ctx context.Context, id, own string) error {
	if err := s.activeRun(id); err != nil {
		return err
	}
	if err := s.noRunningSet(id); err != nil {
		return err
	}
	of, err := s.svc.ProofOf(id)
	if err != nil {
		return err
	}
	if of != "" {
		if err := s.completeBeforeErasure(of, id); err != nil {
			return err
		}
	}
	if err := s.eraseDurable(ctx, id, own); err != nil {
		return err
	}
	if err := s.store.EraseSlice(id); err != nil {
		return Retryable(err)
	}
	s.workers.forget(id)
	if s.runs != nil {
		s.runs.forget(id)
	}
	if err := s.svc.Delete(ctx, id); err != nil {
		return err
	}
	return s.forgetProofSlice(id)
}

// eraseDurable deletes the workflows of the slice's runs, then the workflow and the record of
// each operation other than own whose target is the slice, one of its runs or one of its trial
// sets; a record goes only
// once its workflow is gone, so a repeat finds it again. NotFound is no error; an unavailable
// durable service is Retryable. The caller holds the runs' lock (or no runs are kept).
func (s *sliceServer) eraseDurable(ctx context.Context, id, own string) error {
	targets := map[string]bool{id: true}
	sets, err := s.setIDs(id)
	if err != nil {
		return Retryable(err)
	}
	for _, set := range sets {
		targets[set] = true
	}
	if s.runs != nil {
		for _, e := range s.runs.st.Runs() {
			if e.Slice != id {
				continue
			}
			targets[e.Run] = true
			if s.runs.durable == nil {
				continue
			}
			if err := durableError(s.runs.durable.Delete(ctx, e.Run)); err != nil {
				return err
			}
		}
	}
	recs, err := s.ops.records.Operations()
	if err != nil {
		return Retryable(err)
	}
	for _, rec := range recs {
		if rec.ID == own || !targets[rec.Target] {
			continue
		}
		if err := durableError(s.ops.durable.Delete(ctx, rec.ID)); err != nil {
			return err
		}
		if err := s.ops.records.DeleteOperation(rec.ID); err != nil {
			return Retryable(err)
		}
	}
	return nil
}

// durableError is a deletion's error as the work's: none for NotFound, Retryable when the
// durable service is unavailable.
func durableError(err error) error {
	switch {
	case err == nil, errors.Is(err, ErrNotFound):
		return nil
	case errors.Is(err, ErrUnavailable):
		return Retryable(err)
	}
	return err
}

// noActiveRun refuses RunActive while a run of the slice has not ended (running, interrupted or
// waiting for the developer), under the runs' lock.
func (s *sliceServer) noActiveRun(slice string) error {
	if s.runs == nil {
		return nil
	}
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	return s.activeRun(slice)
}

// activeRun is noActiveRun with the runs' lock held (or no runs kept).
func (s *sliceServer) activeRun(slice string) error {
	if s.runs == nil {
		return nil
	}
	run, err := s.runs.active(slice)
	switch {
	case err != nil:
		return err
	case run != "":
		return &slices.Error{Code: string(gen.ErrorCodeRunActive), Detail: fmt.Sprintf("run %s of slice %s has not ended", run, slice)}
	}
	return nil
}

func (s *sliceServer) ReplaceGoals(ctx context.Context, req gen.ReplaceGoalsRequestObject) (gen.ReplaceGoalsResponseObject, error) {
	if _, err := s.svc.Get(req.SliceIdParameter); err != nil {
		return s.refuse("replaceGoals", err)
	}
	op, err := s.ops.accept(ctx, "replaceGoals", req.SliceIdParameter)
	if err != nil {
		return unavailableFor(err), nil
	}
	return gen.ReplaceGoals202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

func goalsOf(req gen.GoalsPart) []slices.Goal {
	goals := make([]slices.Goal, len(req.Goals))
	for i, g := range req.Goals {
		goals[i] = slices.Goal{ID: g.Id, Text: g.Text}
	}
	return goals
}

func (s *sliceServer) ReplaceRules(ctx context.Context, req gen.ReplaceRulesRequestObject) (gen.ReplaceRulesResponseObject, error) {
	if _, err := s.svc.Get(req.SliceIdParameter); err != nil {
		return s.refuse("replaceRules", err)
	}
	op, err := s.ops.accept(ctx, "replaceRules", req.SliceIdParameter)
	if err != nil {
		return unavailableFor(err), nil
	}
	return gen.ReplaceRules202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

func rulesOf(req gen.RulesPart) slices.RulesPart {
	part := slices.RulesPart{Examples: req.Examples, Rules: make([]slices.Rule, len(req.Rules)), Terms: make([]slices.Term, len(req.Terms))}
	for i, r := range req.Rules {
		part.Rules[i] = slices.Rule{ID: r.Id, Goal: r.Goal, Pattern: string(r.Pattern), Precondition: deref(r.Precondition),
			Trigger: deref(r.Trigger), Feature: deref(r.Feature), System: r.System, Response: r.Response, Terms: r.Terms}
	}
	for i, t := range req.Terms {
		part.Terms[i] = slices.Term{ID: t.Id, Name: t.Name, Definition: t.Definition}
	}
	return part
}

func (s *sliceServer) ReplaceContract(ctx context.Context, req gen.ReplaceContractRequestObject) (gen.ReplaceContractResponseObject, error) {
	if _, err := s.svc.Get(req.SliceIdParameter); err != nil {
		return s.refuse("replaceContract", err)
	}
	op, err := s.ops.accept(ctx, "replaceContract", req.SliceIdParameter)
	if err != nil {
		return unavailableFor(err), nil
	}
	return gen.ReplaceContract202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

func (s *sliceServer) AnswerQuestions(ctx context.Context, req gen.AnswerQuestionsRequestObject) (gen.AnswerQuestionsResponseObject, error) {
	id := req.SliceIdParameter
	answers, err := answersOf(*req.Body)
	if err != nil {
		return s.refuse("answerQuestions", err)
	}
	if err := s.svc.CheckAnswers(id, answers); err != nil {
		return s.refuse("answerQuestions", err)
	}
	op, err := s.ops.accept(ctx, "answerQuestions", id)
	if err != nil {
		return unavailableFor(err), nil
	}
	return gen.AnswerQuestions202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// answersOf reads the answers of an answerQuestions body; an option that is no whole number is
// an InvalidRequest refusal.
func answersOf(req gen.AnswerRequest) ([]slices.Answer, error) {
	answers := make([]slices.Answer, len(req.Answers))
	for i, a := range req.Answers {
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
				return nil, &slices.Error{Code: "InvalidRequest",
					Detail: fmt.Sprintf("the answer to %s gives option %s, which is not a whole number", a.Question, *v.Option)}
			}
			option = &n
		}
		answers[i] = slices.Answer{Question: a.Question, Option: option, Text: v.Text}
	}
	return answers, nil
}

// answerQuestions records the body's answers; questions that hold them all already are the work
// done before a stop.
func (s *sliceServer) answerQuestions(ctx context.Context, id string, body []byte) error {
	var req gen.AnswerQuestionsJSONRequestBody
	if err := json.Unmarshal(body, &req); err != nil {
		return err
	}
	answers, err := answersOf(req)
	if err != nil {
		return err
	}
	if sl, err := s.svc.Get(id); err == nil && answered(sl, answers) {
		return nil
	}
	return s.withAnalyst(ctx, func() error {
		_, err := s.svc.Answer(ctx, id, answers)
		return err
	})
}

// answered reports whether every question the answers name holds that answer.
func answered(sl slices.Slice, answers []slices.Answer) bool {
	holds := func(a slices.Answer) bool {
		for _, q := range sl.Questions {
			if got := q.Answer; q.ID == a.Question && got != nil {
				return got.Text == a.Text && (got.Option == nil) == (a.Option == nil) && (got.Option == nil || *got.Option == *a.Option)
			}
		}
		return false
	}
	for _, a := range answers {
		if !holds(a) {
			return false
		}
	}
	return true
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

func (s *sliceServer) ApprovePart(ctx context.Context, req gen.ApprovePartRequestObject) (gen.ApprovePartResponseObject, error) {
	id, part, hash := req.SliceIdParameter, slices.Part(req.Body.Part), req.Body.ContentHash
	if err := s.svc.CheckApprove(id, part, hash); err != nil {
		return s.refuse("approvePart", err)
	}
	op, err := s.ops.accept(ctx, "approvePart", id)
	if err != nil {
		return unavailableFor(err), nil
	}
	return gen.ApprovePart202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// approvePart approves the part at the body's hash; a part approved at it already is the work
// done before a stop.
func (s *sliceServer) approvePart(ctx context.Context, id string, body []byte) error {
	var req gen.ApprovePartJSONRequestBody
	if err := json.Unmarshal(body, &req); err != nil {
		return err
	}
	part := slices.Part(req.Part)
	if sl, err := s.svc.Get(id); err == nil {
		for _, a := range sl.Approvals {
			if a.Part == part && a.ContentHash == req.ContentHash {
				return nil
			}
		}
	}
	_, err := s.svc.Approve(ctx, id, part, req.ContentHash)
	return err
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
// distinct artifacts stored for its runs; GetSlice sets latest_run.
func sliceView(sl slices.Slice, artifactCount int) gen.Slice {
	v := gen.Slice{Id: sl.ID, Title: title(sl), Description: sl.Description, State: gen.SliceSchemaState(sl.State),
		Examples: sl.Examples, Contract: sl.Contract, ArtifactCount: &artifactCount, Checks: checkResults(sl.Checks),
		Goals: make([]gen.Goal, len(sl.Goals)), Terms: make([]gen.Term, len(sl.Terms)), Rules: make([]gen.Rule, len(sl.Rules)),
		Questions: make([]gen.Question, len(sl.Questions)), RuleText: map[string]string{}}
	v.Parts.Goals, v.Parts.Rules, v.Parts.Contract = sl.Parts.Goals, sl.Parts.Rules, sl.Parts.Contract
	if sl.ChecksMethod != "" {
		m := sl.ChecksMethod
		v.ChecksMethod = &m
	}
	v.ProofOf = opt(sl.ProofOf)
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
		if h.Attempts > 0 {
			n := h.Attempts
			e.Attempts = &n
		}
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
		out[i] = gen.CheckResult{Part: c.Part, Check: c.Check, Passed: c.Passed, Reason: c.Reason, Pattern: c.Pattern}
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
