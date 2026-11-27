package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"

	"github.com/agentic-platform/engine/api/gen"
	sl "github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
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
	file              string // the grant file's name, for a token granted by a file
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

// grantFileMaxBytes bounds a grant file; a larger one grants nothing.
const grantFileMaxBytes = 4096

// grantFromDir returns the worker token that token is by its grant file in dir, or nil when dir
// holds none or the file is unreadable, not a regular file, too large or malformed. The file is
// named the lowercase hex SHA-256 of the token, so the token never appears in a file name, and it
// is read again at every call, so removing it revokes the grant. Its requests are counted under
// a key of their own, as each granted token's are.
func grantFromDir(dir, token string) *acceptedToken {
	sum := sha256.Sum256([]byte(token))
	name := hex.EncodeToString(sum[:])
	g := grantFile(dir, name)
	if g == nil {
		return nil
	}
	return &acceptedToken{token: token, scheme: workerScheme, id: workerScheme + "/" + name, grant: g, file: name}
}

// grantFile reads the grant file name in dir, or nil when dir holds none or the file is
// unreadable, not a regular file, too large or malformed.
func grantFile(dir, name string) *WorkerGrant {
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(f, grantFileMaxBytes+1))
	if err != nil || len(b) > grantFileMaxBytes {
		return nil
	}
	var g struct {
		Slice     *string `json:"slice"`
		Step      *string `json:"step"`
		CallToken *string `json:"call_token"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil || dec.More() {
		return nil
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil
	}
	if g.Slice == nil || g.Step == nil || g.CallToken == nil || *g.Slice == "" || *g.Step == "" || *g.CallToken == "" {
		return nil
	}
	return &WorkerGrant{Slice: *g.Slice, Step: *g.Step, CallToken: *g.CallToken}
}

// grantKey holds the WorkerGrant of a request made with a granted worker token; grantFileKey the
// name of its grant file, for a token granted by a file.
type grantKey struct{}
type grantFileKey struct{}

// registrationStore keeps the step workers' registrations across a restart (ADR 0071); the
// engine store (*store.Store) is one.
type registrationStore interface {
	WorkerRegistrations() ([]store.WorkerRegistration, error)
	PutWorkerRegistration(store.WorkerRegistration) error
	DeleteWorkerRegistration(slice, step, project string) error
}

// Workers is where the engine finds the step worker of each step of each slice for each project:
// the endpoint it registered and the token to call it with. It keeps one registration per slice,
// step and project; one registered without a project serves every project of its slice that has
// none of its own (ADR 0103). A worker granted by a grant file is found by that file, read at each
// lookup, so removing the file unregisters it at once; the handler keeps such registrations in
// its store across a restart (ADR 0071). One granted at the engine's start (Config.WorkerGrants)
// holds its call token in memory only. It is safe for concurrent use.
type Workers struct {
	mu   sync.Mutex
	m    map[string]map[stepOf]endpoint // slice -> step and project -> its worker
	dir  string                         // the grants folder (Config.GrantsDir)
	kept registrationStore              // nil: registrations are not kept
}

// stepOf names a registration within its slice: the step, and the project ("" for any).
type stepOf struct{ step, project string }

// endpoint is a registered worker: its URL, and its grant file's name (file) or, for a worker
// granted at start, its call token.
type endpoint struct{ url, token, file string }

// live returns the worker's call token, read from its grant file for a file-granted worker, or
// ok false when that file is gone or no longer grants the slice's step.
func (w *Workers) live(slice, step string, e endpoint) (string, bool) {
	if e.file == "" {
		return e.token, true
	}
	if w.dir == "" {
		return "", false
	}
	g := grantFile(w.dir, e.file)
	if g == nil || g.Slice != slice || g.Step != step {
		return "", false
	}
	return g.CallToken, true
}

// load adds the kept registrations to w; a failure to read them leaves w as it was.
func (w *Workers) load() error {
	if w.kept == nil {
		return nil
	}
	all, err := w.kept.WorkerRegistrations()
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, r := range all {
		if w.m[r.Slice] == nil {
			w.m[r.Slice] = map[stepOf]endpoint{}
		}
		w.m[r.Slice][stepOf{r.Step, r.Project}] = endpoint{url: r.Endpoint, file: r.Grant}
	}
	return nil
}

// forget drops the slice's registrations, of every project, from memory, once its store entries
// are erased.
func (w *Workers) forget(slice string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.m, slice)
}

// NewWorkers returns an empty registry.
func NewWorkers() *Workers { return &Workers{m: map[string]map[stepOf]endpoint{}} }

// Endpoint returns the URL and call token of the worker that serves the slice's step for the
// project: the project's own registration, else the one registered without a project; ok false
// when neither is registered. Project "" finds only the one registered without a project.
func (w *Workers) Endpoint(slice, project, step string) (url, token string, ok bool) {
	tried := []string{project}
	if project != "" {
		tried = append(tried, "")
	}
	for _, p := range tried {
		if e, ok := w.endpoint(slice, stepOf{step, p}); ok {
			if token, ok := w.live(slice, step, e); ok {
				return e.url, token, true
			}
		}
	}
	return "", "", false
}

func (w *Workers) register(slice string, k stepOf, e endpoint) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.m[slice] == nil {
		w.m[slice] = map[stepOf]endpoint{}
	}
	w.m[slice][k] = e
}

// keep writes the registration of a file-granted worker to the store; others are not kept.
func (w *Workers) keep(slice string, k stepOf, e endpoint) error {
	if w.kept == nil || e.file == "" {
		return nil
	}
	return w.kept.PutWorkerRegistration(store.WorkerRegistration{Slice: slice, Step: k.step, Project: k.project, Endpoint: e.url, Grant: e.file})
}

// unkeep puts the store back to the registration before a register (had false: none).
func (w *Workers) unkeep(slice string, k stepOf, before endpoint, had bool) error {
	if w.kept == nil {
		return nil
	}
	if had && before.file != "" {
		return w.kept.PutWorkerRegistration(store.WorkerRegistration{Slice: slice, Step: k.step, Project: k.project, Endpoint: before.url, Grant: before.file})
	}
	return w.kept.DeleteWorkerRegistration(slice, k.step, k.project)
}

// restore puts back the worker the slice's step had for the project before a register (had
// false: none).
func (w *Workers) restore(slice string, k stepOf, before endpoint, had bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if had {
		w.m[slice][k] = before
		return
	}
	delete(w.m[slice], k)
}

func (w *Workers) endpoint(slice string, k stepOf) (endpoint, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	e, ok := w.m[slice][k]
	return e, ok
}

// missing returns the first of steps with no worker that serves the slice's project (Endpoint),
// or "" when every step has one.
func (w *Workers) missing(slice, project string, steps []string) string {
	for _, st := range steps {
		if _, _, ok := w.Endpoint(slice, project, st); !ok {
			return st
		}
	}
	return ""
}

// RegisterWorker records the endpoint for the slice and step the worker token was issued for and
// the project the body names (none: for any project of the slice), replacing any earlier one of
// the same slice, step and project. The endpoint's and project's shapes are the contract's,
// checked before this runs. A token without a grant, a slice that does not exist and a step the
// pattern does not declare are NotFound. Once every step of the pattern has a worker for a
// project, the run of the slice into that project that waits for its step workers is sent
// `workers-ready` and continues; while the durable service cannot be reached the answer is 503
// and the worker is not registered.
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
	file, _ := ctx.Value(grantFileKey{}).(string)
	e := endpoint{url: req.Body.Endpoint, token: g.CallToken, file: file}
	if file != "" {
		e.token = "" // read from the grant file at each call
	}
	k := stepOf{step: g.Step}
	if req.Body.Project != nil {
		k.project = *req.Body.Project
	}
	before, had := s.workers.endpoint(g.Slice, k)
	if err := s.workers.keep(g.Slice, k, e); err != nil {
		return unavailableFor(fmt.Errorf("the registration could not be recorded: %w", err)), nil
	}
	s.workers.register(g.Slice, k, e)
	if err := s.workersReady(ctx, g.Slice, k.project); err != nil {
		s.workers.restore(g.Slice, k, before, had)
		if uerr := s.workers.unkeep(g.Slice, k, before, had); uerr != nil {
			log.Printf("engine: registerWorker: putting back the registration of %s %s %q: %v", g.Slice, g.Step, k.project, uerr)
		}
		if errors.Is(err, ErrUnavailable) {
			return unavailableFor(err), nil
		}
		return nil, err
	}
	return gen.RegisterWorker200JSONResponse{WorkerJSONResponse: gen.WorkerJSONResponse{
		Body: gen.Worker{Slice: g.Slice, Step: g.Step, Endpoint: req.Body.Endpoint, Project: req.Body.Project}}}, nil
}

// workersReady sends `workers-ready` to the run of the slice that waits for its step workers and
// whose project a registration for project serves (project "": any of the slice's), once each
// step of the pattern has a worker for that run's project; a run whose workflow has ended needs
// none.
func (s *sliceServer) workersReady(ctx context.Context, slice, project string) error {
	if s.runs == nil || s.runs.durable == nil {
		return nil
	}
	steps := s.svc.Steps()
	run, err := s.runs.waitingForWorkers(slice, func(p string) bool {
		return (project == "" || p == project) && s.workers.missing(slice, p, steps) == ""
	})
	if err != nil || run == "" {
		return err
	}
	if err := s.signal(ctx, run, SignalWorkersReady); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

func (p problemResponse) VisitRegisterWorkerResponse(w http.ResponseWriter) error { return p.write(w) }
