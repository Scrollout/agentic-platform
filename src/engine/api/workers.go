package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"github.com/agentic-platform/engine/api/gen"
	sl "github.com/agentic-platform/engine/slices"
)

// WorkerGrant is the slice and step a step worker's token was issued for. The token says which
// worker is calling; registerWorker takes the slice and step from it, never from the body.
type WorkerGrant struct{ Slice, Step string }

// acceptedToken is one bearer token the engine accepts: its security scheme in the contract, the
// key its requests are counted and kept under, and for a step worker's token its grant.
type acceptedToken struct {
	token, scheme, id string
	grant             *WorkerGrant
}

// acceptedTokens lists the session's tokens, then one worker token per grant. A session token's
// requests are counted under its scheme; a granted token's under its own key, so each worker
// has its own rate limit. An empty token is never accepted.
func acceptedTokens(t Tokens, grants map[string]WorkerGrant) []acceptedToken {
	var out []acceptedToken
	for _, s := range []struct{ token, scheme string }{
		{t.Client, clientScheme}, {t.Page, pageScheme}, {t.Worker, workerScheme},
	} {
		if s.token != "" {
			out = append(out, acceptedToken{token: s.token, scheme: s.scheme, id: s.scheme})
		}
	}
	n := 0
	for token, g := range grants {
		if token != "" {
			n++
			out = append(out, acceptedToken{token: token, scheme: workerScheme, id: fmt.Sprintf("%s/%d", workerScheme, n), grant: &g})
		}
	}
	return out
}

// grantKey holds the WorkerGrant of a request made with a granted worker token.
type grantKey struct{}

// workers holds the registered endpoint of each step of each slice, in memory: a restart
// forgets them, and the workers register again.
type workers struct {
	mu sync.Mutex
	m  map[string]map[string]string // slice -> step -> endpoint
}

func newWorkers() *workers { return &workers{m: map[string]map[string]string{}} }

func (w *workers) register(slice, step, endpoint string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.m[slice] == nil {
		w.m[slice] = map[string]string{}
	}
	w.m[slice][step] = endpoint
}

// missing returns the first of steps with no registered worker for slice, or "" when every
// step has one.
func (w *workers) missing(slice string, steps []string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, st := range steps {
		if _, ok := w.m[slice][st]; !ok {
			return st
		}
	}
	return ""
}

// RegisterWorker records the endpoint for the slice and step the worker token was issued for,
// replacing any earlier one. The endpoint's shape is the contract's, checked before this runs. A
// token without a grant, a slice that does not exist and a step the pattern does not declare are
// NotFound.
func (s *sliceServer) RegisterWorker(ctx context.Context, req gen.RegisterWorkerRequestObject) (gen.RegisterWorkerResponseObject, error) {
	g, ok := ctx.Value(grantKey{}).(WorkerGrant)
	if !ok {
		return s.server.RegisterWorker(ctx, req)
	}
	if _, err := s.svc.Get(g.Slice); err != nil {
		return s.refuse("registerWorker", err)
	}
	if !slices.Contains(s.svc.Steps(), g.Step) {
		return s.refuse("registerWorker", &sl.Error{Code: string(gen.ErrorCodeNotFound),
			Detail: fmt.Sprintf("the pattern of slice %s has no step %s", g.Slice, g.Step)})
	}
	s.workers.register(g.Slice, g.Step, req.Body.Endpoint)
	return gen.RegisterWorker200JSONResponse{WorkerJSONResponse: gen.WorkerJSONResponse{
		Body: gen.Worker{Slice: g.Slice, Step: g.Step, Endpoint: req.Body.Endpoint}}}, nil
}

// StartRun refuses in the contract's order, the first that applies, before any operation exists:
// an unknown slice, a pattern condition that fails, a part not approved at its current hash
// (Buildable); then WorkersNotReady while a step of the pattern has no registered worker for the
// slice; then ProjectExists when the project's folder exists under the projects folder. RunActive
// comes between those two once the engine keeps runs (T054); it keeps none yet. A run that passes
// every refusal is an operation of kind run whose work is Config.Work's, as for any job the
// engine does not do itself; the folder is not touched here.
func (s *sliceServer) StartRun(ctx context.Context, req gen.StartRunRequestObject) (gen.StartRunResponseObject, error) {
	id := req.SliceIdParameter
	if err := s.svc.Buildable(id); err != nil {
		return s.refuse("startRun", err)
	}
	steps := s.svc.Steps()
	if len(steps) == 0 {
		return s.refuse("startRun", &sl.Error{Code: string(gen.ErrorCodeWorkersNotReady), Detail: "the pattern declares no steps that a worker could run"})
	}
	if st := s.workers.missing(id, steps); st != "" {
		return s.refuse("startRun", &sl.Error{Code: string(gen.ErrorCodeWorkersNotReady),
			Detail: fmt.Sprintf("step %s of the pattern has no registered worker for slice %s", st, id)})
	}
	if s.projects != "" {
		switch _, err := os.Lstat(filepath.Join(s.projects, req.Body.Project)); {
		case err == nil:
			return s.refuse("startRun", &sl.Error{Code: string(gen.ErrorCodeProjectExists),
				Detail: fmt.Sprintf("the projects folder already holds %s", req.Body.Project)})
		case !errors.Is(err, fs.ErrNotExist):
			return nil, fmt.Errorf("whether project %s exists: %w", req.Body.Project, err)
		}
	}
	raw, _ := ctx.Value(rawBodyKey{}).([]byte) // the front checked it against the contract
	target := "RUN-" + strconv.FormatInt(s.lastRun.Add(1), 10)
	op := s.ops.start(Job{Kind: gen.OperationKindRun, Target: target, Body: raw})
	return gen.StartRun202JSONResponse{AcceptedJSONResponse: gen.AcceptedJSONResponse{Body: op}}, nil
}

func (p problemResponse) VisitRegisterWorkerResponse(w http.ResponseWriter) error { return p.write(w) }
