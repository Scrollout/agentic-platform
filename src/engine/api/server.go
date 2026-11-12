package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/agentic-platform/engine/api/gen"
)

// server implements the operations. The skeleton holds no slices or runs yet: lists are empty
// and a slice or run asked for by ID is not found. createSlice starts its operation, whose
// work Config.Work does.
type server struct {
	contract  contract
	ops       *operations
	lastSlice atomic.Int64
}

var _ gen.StrictServerInterface = (*server)(nil)

// pageSize is the most operations per page of the operations list. The contract states it
// only in the operation's description; it has no machine-readable home there yet.
const pageSize = 50

func notFound(what string) gen.NotFoundApplicationProblemPlusJSONResponse {
	return gen.NotFoundApplicationProblemPlusJSONResponse(newProblem(http.StatusNotFound, gen.ErrorCodeNotFound, what+" does not exist"))
}

// parseCreateSlice reads a createSlice body as the contract defines it: an object with a
// non-empty `description` and nothing else.
func parseCreateSlice(raw []byte) (gen.CreateSliceRequest, error) {
	var req gen.CreateSliceRequest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return req, errors.New("the body holds more than one JSON value")
	}
	if req.Description == "" {
		return req, errors.New("description must not be empty")
	}
	return req, nil
}

func (s *server) CreateSlice(ctx context.Context, _ gen.CreateSliceRequestObject) (gen.CreateSliceResponseObject, error) {
	raw, _ := ctx.Value(rawBodyKey{}).([]byte)
	if _, err := parseCreateSlice(raw); err != nil {
		return gen.CreateSlice400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: gen.BadRequestApplicationProblemPlusJSONResponse(
			newProblem(http.StatusBadRequest, gen.ErrorCodeInvalidRequest, err.Error()))}, nil
	}
	target := "S-" + strconv.FormatInt(s.lastSlice.Add(1), 10)
	op := s.ops.start(Job{Kind: gen.OperationKindProposeSlice, Target: target, Body: raw})
	return gen.CreateSlice202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

func (s *server) GetOperation(_ context.Context, req gen.GetOperationRequestObject) (gen.GetOperationResponseObject, error) {
	op, ok := s.ops.get(req.OperationId)
	if !ok {
		return gen.GetOperation404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("operation " + req.OperationId)}, nil
	}
	return gen.GetOperation200JSONResponse{OperationJSONResponse: gen.OperationJSONResponse{Body: op}}, nil
}

func (s *server) CancelOperation(_ context.Context, req gen.CancelOperationRequestObject) (gen.CancelOperationResponseObject, error) {
	op, found, hadEnded := s.ops.cancelOp(req.OperationId)
	switch {
	case !found:
		return gen.CancelOperation404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("operation " + req.OperationId)}, nil
	case hadEnded:
		return gen.CancelOperation409ApplicationProblemPlusJSONResponse{ConflictApplicationProblemPlusJSONResponse: gen.ConflictApplicationProblemPlusJSONResponse(
			newProblem(http.StatusConflict, gen.ErrorCodeOperationEnded, "the operation is "+string(op.Status)))}, nil
	}
	return gen.CancelOperation202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

// ListProviderOperations lists each permission name once, sorted, with the summaries of the
// operations that carry it, in contract order, as display text. A page token is the offset of
// its page; one this service did not issue is refused.
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

	from := 0
	if req.Params.PageToken != nil {
		raw, err := base64.RawURLEncoding.DecodeString(*req.Params.PageToken)
		n, err2 := strconv.Atoi(string(raw))
		if err != nil || err2 != nil || n <= 0 || n%pageSize != 0 || n >= len(all) || issuedPageToken(n) != *req.Params.PageToken {
			return gen.ListProviderOperations400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: gen.BadRequestApplicationProblemPlusJSONResponse(
				newProblem(http.StatusBadRequest, gen.ErrorCodeInvalidRequest, "page_token was not issued by this service"))}, nil
		}
		from = n
	}
	to := min(from+pageSize, len(all))
	list := gen.ProviderOperationList{Items: all[from:to]}
	if to < len(all) {
		next := issuedPageToken(to)
		list.NextPageToken = &next
	}
	return gen.ListProviderOperations200JSONResponse{ProviderOperationListJSONResponse: gen.ProviderOperationListJSONResponse(list)}, nil
}

func issuedPageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
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
