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
	DeleteWorkerRegistration(slice, step string) error
}

// Workers is where the engine finds the step worker of each step of each slice: the endpoint it
// registered and the token to call it with. A worker granted by a grant file is found by that
// file, read at each lookup, so removing the file unregisters it at once; the handler keeps such
// registrations in its store across a restart (ADR 0071). One granted at the engine's start
// (Config.WorkerGrants) holds its call token in memory only. It is safe for concurrent use.
type Workers struct {
	mu   sync.Mutex
	m    map[string]map[string]endpoint // slice -> step -> its worker
	dir  string                         // the grants folder (Config.GrantsDir)
	kept registrationStore              // nil: registrations are not kept
}

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
			w.m[r.Slice] = map[string]endpoint{}
		}
		w.m[r.Slice][r.Step] = endpoint{url: r.Endpoint, file: r.Grant}
	}
	return nil
}

// forget drops the slice's registrations from memory, once its store entries are erased.
func (w *Workers) forget(slice string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.m, slice)
}

// NewWorkers returns an empty registry.
func NewWorkers() *Workers { return &Workers{m: map[string]map[string]endpoint{}} }

// Endpoint returns the URL and call token of the worker registered for the slice's step, or
// ok false when none is.
func (w *Workers) Endpoint(slice, step string) (url, token string, ok bool) {
	e, ok := w.endpoint(slice, step)
	if !ok {
		return "", "", false
	}
	token, ok = w.live(slice, step, e)
	if !ok {
		return "", "", false
	}
	return e.url, token, true
}

func (w *Workers) register(slice, step string, e endpoint) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.m[slice] == nil {
		w.m[slice] = map[string]endpoint{}
	}
	w.m[slice][step] = e
}

// keep writes the registration of a file-granted worker to the store; others are not kept.
func (w *Workers) keep(slice, step string, e endpoint) error {
	if w.kept == nil || e.file == "" {
		return nil
	}
	return w.kept.PutWorkerRegistration(store.WorkerRegistration{Slice: slice, Step: step, Endpoint: e.url, Grant: e.file})
}

// unkeep puts the store back to the registration before a register (had false: none).
func (w *Workers) unkeep(slice, step string, before endpoint, had bool) error {
	if w.kept == nil {
		return nil
	}
	if had && before.file != "" {
		return w.kept.PutWorkerRegistration(store.WorkerRegistration{Slice: slice, Step: step, Endpoint: before.url, Grant: before.file})
	}
	return w.kept.DeleteWorkerRegistration(slice, step)
}

// restore puts back the worker the slice's step had before a register (had false: none).
func (w *Workers) restore(slice, step string, before endpoint, had bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if had {
		w.m[slice][step] = before
		return
	}
	delete(w.m[slice], step)
}

func (w *Workers) endpoint(slice, step string) (endpoint, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	e, ok := w.m[slice][step]
	return e, ok
}

// missing returns the first of steps with no registered worker for slice, or "" when every
// step has one.
func (w *Workers) missing(slice string, steps []string) string {
	for _, st := range steps {
		e, ok := w.endpoint(slice, st)
		if !ok {
			return st
		}
		if _, ok := w.live(slice, st, e); !ok {
			return st
		}
	}
	return ""
}

// RegisterWorker records the endpoint for the slice and step the worker token was issued for,
// replacing any earlier one. The endpoint's shape is the contract's, checked before this runs. A
// token without a grant, a slice that does not exist and a step the pattern does not declare are
// NotFound. Once every step of the pattern has a worker for the slice, the run of the slice that
// waits for its step workers is sent `workers-ready` and continues; while the durable service
// cannot be reached the answer is 503 and the worker is not registered.
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
	before, had := s.workers.endpoint(g.Slice, g.Step)
	if err := s.workers.keep(g.Slice, g.Step, e); err != nil {
		return unavailableFor(fmt.Errorf("the registration could not be recorded: %w", err)), nil
	}
	s.workers.register(g.Slice, g.Step, e)
	if err := s.workersReady(ctx, g.Slice); err != nil {
		s.workers.restore(g.Slice, g.Step, before, had)
		if uerr := s.workers.unkeep(g.Slice, g.Step, before, had); uerr != nil {
			log.Printf("engine: registerWorker: putting back the registration of %s %s: %v", g.Slice, g.Step, uerr)
		}
		if errors.Is(err, ErrUnavailable) {
			return unavailableFor(err), nil
		}
		return nil, err
	}
	return gen.RegisterWorker200JSONResponse{WorkerJSONResponse: gen.WorkerJSONResponse{
		Body: gen.Worker{Slice: g.Slice, Step: g.Step, Endpoint: req.Body.Endpoint}}}, nil
}

// workersReady sends `workers-ready` to the run of the slice that waits for the slice's step
// workers, once each step of the pattern has one; a run whose workflow has ended needs none.
func (s *sliceServer) workersReady(ctx context.Context, slice string) error {
	if s.runs == nil || s.runs.durable == nil || s.workers.missing(slice, s.svc.Steps()) != "" {
		return nil
	}
	run, err := s.runs.waitingForWorkers(slice)
	if err != nil || run == "" {
		return err
	}
	if err := s.signal(ctx, run, SignalWorkersReady); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

func (p problemResponse) VisitRegisterWorkerResponse(w http.ResponseWriter) error { return p.write(w) }
