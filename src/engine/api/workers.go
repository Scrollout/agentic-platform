package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sync"

	"github.com/agentic-platform/engine/api/gen"
	sl "github.com/agentic-platform/engine/slices"
)

// WorkerGrant is the slice and step a step worker's token was issued for. The token says which
// worker is calling; registerWorker takes the slice and step from it, never from the body.
// CallToken is the token the engine sends when it calls the worker that registers with this
// grant (research D12); the launcher issues both (T053a). Empty means the engine sends none.
type WorkerGrant struct{ Slice, Step, CallToken string }

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

// Workers is where the engine finds the step worker of each step of each slice: the endpoint it
// registered and the token to call it with. It is held in memory: a restart forgets it, and the
// workers register again. It is safe for concurrent use.
type Workers struct {
	mu sync.Mutex
	m  map[string]map[string]endpoint // slice -> step -> its worker
}

type endpoint struct{ url, token string }

// NewWorkers returns an empty registry.
func NewWorkers() *Workers { return &Workers{m: map[string]map[string]endpoint{}} }

// Endpoint returns the URL and call token of the worker registered for the slice's step, or
// ok false when none is.
func (w *Workers) Endpoint(slice, step string) (url, token string, ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	e, ok := w.m[slice][step]
	return e.url, e.token, ok
}

func (w *Workers) register(slice, step, url, token string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.m[slice] == nil {
		w.m[slice] = map[string]endpoint{}
	}
	w.m[slice][step] = endpoint{url, token}
}

// missing returns the first of steps with no registered worker for slice, or "" when every
// step has one.
func (w *Workers) missing(slice string, steps []string) string {
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
	s.workers.register(g.Slice, g.Step, req.Body.Endpoint, g.CallToken)
	return gen.RegisterWorker200JSONResponse{WorkerJSONResponse: gen.WorkerJSONResponse{
		Body: gen.Worker{Slice: g.Slice, Step: g.Step, Endpoint: req.Body.Endpoint}}}, nil
}

func (p problemResponse) VisitRegisterWorkerResponse(w http.ResponseWriter) error { return p.write(w) }
