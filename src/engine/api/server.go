package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/store"
)

// server implements the operations. The skeleton holds no slices or runs yet: lists are empty
// and a slice or run asked for by ID is not found. createSlice starts its operation, whose
// work Config.Work does (jobs).
type server struct {
	contract  contract
	ops       *operations
	jobs      func(ctx context.Context, job Job) (WorkResult, error) // Config.Work
	lastSlice atomic.Int64
	pages     pageKeys
	// analyst holds one place per operation that may work with the analyst at once.
	analyst chan struct{}
}

// analystCallsAtOnce is how many createSlice and answerQuestions operations may work with the
// analyst at once across the engine, as their descriptions in the contract state. It has no
// machine-readable home there: x-limits holds the limits every service declares, and the
// Spectral rule ap-limits-declared and the engine read the same keys.
const analystCallsAtOnce = 4

var _ gen.StrictServerInterface = (*server)(nil)

// pageSize is the most items per page of a list (operations, slices). The contract states it
// only in each list operation's description; it has no machine-readable home there yet.
const pageSize = 50

func notFound(what string) gen.NotFoundApplicationProblemPlusJSONResponse {
	return gen.NotFoundApplicationProblemPlusJSONResponse(newProblem(http.StatusNotFound, gen.ErrorCodeNotFound, what+" does not exist"))
}

func (s *server) CreateSlice(ctx context.Context, _ gen.CreateSliceRequestObject) (gen.CreateSliceResponseObject, error) {
	target := "S-" + strconv.FormatInt(s.lastSlice.Add(1), 10)
	op, err := s.ops.accept(ctx, "createSlice", target)
	if err != nil {
		return unavailableFor(err), nil
	}
	return gen.CreateSlice202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// work is the skeleton's work of an operation: Config.Work's job.
func (s *server) work(ctx context.Context, rec store.OperationRecord, body []byte) (WorkResult, error) {
	if s.jobs == nil {
		return WorkResult{}, errors.New("this engine has no worker for this kind of operation yet")
	}
	return s.jobs(ctx, Job{Kind: operationKinds[rec.Kind], Target: rec.Target, Body: body})
}

func (s *server) GetOperation(ctx context.Context, req gen.GetOperationRequestObject) (gen.GetOperationResponseObject, error) {
	op, ok, err := s.ops.get(ctx, req.OperationId)
	switch {
	case err != nil:
		return nil, fmt.Errorf("reading operation %s: %w", req.OperationId, err)
	case !ok:
		return gen.GetOperation404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("operation " + req.OperationId)}, nil
	}
	return gen.GetOperation200JSONResponse{OperationJSONResponse: gen.OperationJSONResponse{Body: op}}, nil
}

func (s *server) CancelOperation(ctx context.Context, req gen.CancelOperationRequestObject) (gen.CancelOperationResponseObject, error) {
	op, found, hadEnded, err := s.ops.cancel(ctx, req.OperationId)
	switch {
	case errors.Is(err, ErrUnavailable):
		return unavailableFor(err), nil
	case err != nil:
		log.Printf("engine: cancelling operation %s: %v", req.OperationId, err)
		return unavailableFor(errNotRecorded), nil
	case !found:
		return gen.CancelOperation404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("operation " + req.OperationId)}, nil
	case hadEnded:
		return gen.CancelOperation409ApplicationProblemPlusJSONResponse{ConflictApplicationProblemPlusJSONResponse: gen.ConflictApplicationProblemPlusJSONResponse(
			newProblem(http.StatusConflict, gen.ErrorCodeOperationEnded, "the operation is "+string(op.Status)))}, nil
	}
	return gen.CancelOperation202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// ListProviderOperations lists each permission name once, sorted, with the summaries of the
// operations that carry it, in contract order, as display text. A page token names the offset
// of its page (pages.go); one this list did not issue is refused.
func (s *server) ListProviderOperations(_ context.Context, req gen.ListProviderOperationsRequestObject) (gen.ListProviderOperationsResponseObject, error) {
	ops := make([]contractOp, 0, len(s.contract.ops))
	for _, op := range s.contract.ops {
		ops = append(ops, op)
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].order < ops[j].order })
	display := map[string][]string{}
	for _, op := range ops {
		display[op.name] = append(display[op.name], op.summary)
	}
	all := make([]gen.ProviderOperation, 0, len(display))
	for name, summaries := range display {
		all = append(all, gen.ProviderOperation{Name: name, Display: strings.Join(summaries, "; ")})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	from, to, next, ok := s.pages.offsetPage(operationsList, req.Params.PageToken, len(all))
	if !ok {
		return gen.ListProviderOperations400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: badPageToken()}, nil
	}
	list := gen.ProviderOperationList{Items: all[from:to], NextPageToken: next}
	return gen.ListProviderOperations200JSONResponse{ProviderOperationListJSONResponse: gen.ProviderOperationListJSONResponse(list)}, nil
}

func (s *server) ListSlices(context.Context, gen.ListSlicesRequestObject) (gen.ListSlicesResponseObject, error) {
	return gen.ListSlices200JSONResponse{SliceListJSONResponse: gen.SliceListJSONResponse{Body: gen.SliceList{Items: []gen.SliceSummary{}}}}, nil
}

func (s *server) ListRuns(context.Context, gen.ListRunsRequestObject) (gen.ListRunsResponseObject, error) {
	return gen.ListRuns200JSONResponse{RunListJSONResponse: gen.RunListJSONResponse{Body: gen.RunList{Items: []gen.Run{}}}}, nil
}

func (s *server) GetSlice(_ context.Context, _ gen.GetSliceRequestObject) (gen.GetSliceResponseObject, error) {
	return gen.GetSlice404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the slice")}, nil
}

func (s *server) DeleteSlice(_ context.Context, _ gen.DeleteSliceRequestObject) (gen.DeleteSliceResponseObject, error) {
	return gen.DeleteSlice404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the slice")}, nil
}

func (s *server) ReplaceGoals(_ context.Context, _ gen.ReplaceGoalsRequestObject) (gen.ReplaceGoalsResponseObject, error) {
	return gen.ReplaceGoals404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the slice")}, nil
}

func (s *server) ReplaceRules(_ context.Context, _ gen.ReplaceRulesRequestObject) (gen.ReplaceRulesResponseObject, error) {
	return gen.ReplaceRules404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the slice")}, nil
}

func (s *server) ReplaceContract(_ context.Context, _ gen.ReplaceContractRequestObject) (gen.ReplaceContractResponseObject, error) {
	return gen.ReplaceContract404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the slice")}, nil
}

func (s *server) AnswerQuestions(_ context.Context, _ gen.AnswerQuestionsRequestObject) (gen.AnswerQuestionsResponseObject, error) {
	return gen.AnswerQuestions404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the slice")}, nil
}

func (s *server) ApprovePart(_ context.Context, _ gen.ApprovePartRequestObject) (gen.ApprovePartResponseObject, error) {
	return gen.ApprovePart404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the slice")}, nil
}

func (s *server) StartRun(_ context.Context, _ gen.StartRunRequestObject) (gen.StartRunResponseObject, error) {
	return gen.StartRun404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the slice")}, nil
}

func (s *server) RegisterWorker(context.Context, gen.RegisterWorkerRequestObject) (gen.RegisterWorkerResponseObject, error) {
	return gen.RegisterWorker404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the token's slice")}, nil
}

func (s *server) GetRun(_ context.Context, _ gen.GetRunRequestObject) (gen.GetRunResponseObject, error) {
	return gen.GetRun404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the run")}, nil
}

func (s *server) GetArtifact(_ context.Context, _ gen.GetArtifactRequestObject) (gen.GetArtifactResponseObject, error) {
	return gen.GetArtifact404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the run")}, nil
}

func (s *server) CompareRuns(_ context.Context, _ gen.CompareRunsRequestObject) (gen.CompareRunsResponseObject, error) {
	return gen.CompareRuns404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the run")}, nil
}

func (s *server) ListRunEvents(_ context.Context, _ gen.ListRunEventsRequestObject) (gen.ListRunEventsResponseObject, error) {
	return gen.ListRunEvents404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the run")}, nil
}

func (s *server) ResumeRun(_ context.Context, _ gen.ResumeRunRequestObject) (gen.ResumeRunResponseObject, error) {
	return gen.ResumeRun404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the run")}, nil
}

func (s *server) ReplayRun(_ context.Context, _ gen.ReplayRunRequestObject) (gen.ReplayRunResponseObject, error) {
	return gen.ReplayRun404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the run")}, nil
}

func (s *server) AcceptHandOver(_ context.Context, _ gen.AcceptHandOverRequestObject) (gen.AcceptHandOverResponseObject, error) {
	return gen.AcceptHandOver404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("the run")}, nil
}
