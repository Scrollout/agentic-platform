// Package api serves the engine API, through which the terminal client, the run page and step
// workers manage slices and runs and follow the work they start, as the engine contract defines.
//
// Every request passes the same checks, in this order: its token, then the rate limit, then its
// api-version (400, except on the operations list), whether the token may call the operation
// (403), then a body's Content-Type (application/json, else 400 InvalidRequest), then the body
// against the operation's request schema in the contract (400 InvalidRequest, or InvalidProject
// for a project name alone that breaks the folder-name rule where the operation lists that
// code), then the operation itself. A request with an accepted token is counted against that
// token's limit alone (429 beyond it). A request with no token, or one the engine does not
// accept, is counted against the limit of its source address, the host part of the
// connection's peer address (never a header the client sends): 401 up to that limit, 429 beyond
// it. A refusal the contract lists under a status code is answered at once; otherwise a
// state-changing call returns 202 with an operation to follow; its work runs on its own,
// is retried when it fails for a passing reason, and ends `succeeded`, `failed` (with the
// reason) or `cancelled`. The limits a client sees (requests per second per token and per
// address, how long idempotency keys are kept, the largest body) are the contract's
// `x-limits`. Limit: the engine keeps its operations and idempotency keys in memory, so a
// restart forgets them.
package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agentic-platform/engine/api/gen"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/slices"
)

// Tokens are the three bearer tokens of the session (research D12). Empty means "no token of
// that kind"; an empty token never matches.
type Tokens struct{ Client, Page, Worker string }

// Config is what the engine API needs from its host.
type Config struct {
	Tokens Tokens
	// Now is the clock for the rate limit and for how long idempotency keys are kept. nil
	// means time.Now.
	Now func() time.Time
	// Sleep waits between retries. nil means a real wait that ends early on cancellation.
	Sleep func(d time.Duration)
	// Work does the job behind a state-changing call. It runs in its own goroutine after the
	// call is answered 202; ctx is cancelled when the operation is cancelled. Required for
	// state-changing calls.
	Work func(ctx context.Context, job Job) (WorkResult, error)
	// Context bounds every operation's work: when it is done, running operations are
	// cancelled. nil means context.Background().
	Context context.Context
	// Running, when set, counts every operation's work, of every kind: added before the work
	// starts, done when it has ended. After cancelling Context, Running.Wait returns once no
	// operation can record anything more. Limit: no request may start an operation while Wait
	// is called, so the caller stops serving requests first. nil means nothing is counted.
	Running *sync.WaitGroup
	// Slices holds the slices. With it the engine serves the slice operations itself and Work
	// does only other kinds of operation; nil means no slices: lists are empty, a slice is never
	// found, and createSlice's operation is Work's.
	Slices *slices.Service
	// Store is what the run store keeps about each slice; required with Slices.
	Store SliceStore
	// WorkerGrants maps each step worker's token to the slice and step it was issued for. A token
	// in it is accepted as a worker token, besides Tokens.Worker, and registerWorker registers
	// the worker for its grant; an empty token is ignored. nil means no worker can register
	// through it.
	WorkerGrants map[string]WorkerGrant
	// GrantsDir is a folder of grants made after the engine started (the launcher's
	// build-workers, research D31). A token that is no session token and not in WorkerGrants is
	// a worker token when the folder holds a file named the lowercase hex SHA-256 of the token
	// whose content is the JSON object {"slice","step","call_token"}, each a non-empty string; it
	// grants as a WorkerGrant with CallToken call_token. The folder is read at each such
	// authentication, so a new file grants at once and a removed one revokes. A file that cannot
	// be read, is not a regular file, is larger than 4 KiB or does not hold exactly that object
	// grants nothing (fail closed). Empty means no such folder.
	GrantsDir string
	// Projects is the projects folder: a project is a folder in it, and startRun refuses a
	// project whose folder exists (ProjectExists). Empty means no projects folder is known, so
	// only a project a recorded run builds into is refused.
	Projects string
	// Workers is the registry registerWorker fills; the StepWorker finds each step's worker in
	// it. nil means the handler keeps its own, which nothing else reads.
	Workers *Workers
	// Runs is the durable service that runs each run as a workflow (feature 002): startRun,
	// replayRun, resumeRun, acceptHandOver, cancelling a run's operation and registerWorker go
	// through it, and the handler never drives a run itself. nil means no run can start: a start
	// that passes every refusal is WorkersNotReady.
	Runs Runs
	// Record is the run record the runs' workflows write and the accepted results they reuse
	// (NewRunRecord); the engine gives the same one to its durable worker, and a start is then
	// answered once the run's RunStarted is recorded (at most 10 s). nil means the handler keeps
	// its own over Store and answers a start as soon as the durable service has taken it.
	Record *RunRecord
	// StepWorker, StepChecker and History are not used by the handler: the runs' workflows call
	// the step workers, the check service and the project worker through the engine's durable
	// worker (package durable).
	StepWorker  machine.Worker
	StepChecker machine.Checker
	History     machine.History
	// Model is the model the pattern's agent steps use, part of their reuse keys. Empty means no
	// accepted result of an agent step is reused (a replay runs those steps again).
	Model string
}

// NewHandler returns the engine's whole HTTP handler.
func NewHandler(cfg Config) http.Handler {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	sleep := waitOrCancel
	if cfg.Sleep != nil {
		sleep = func(_ context.Context, d time.Duration) { cfg.Sleep(d) }
	}
	base := cfg.Context
	if base == nil {
		base = context.Background()
	}
	c := engineContract
	srv := &server{contract: c, ops: &operations{base: base, now: now, sleep: sleep, work: cfg.Work, running: cfg.Running, ops: map[string]*operation{}},
		pages: newPageKeys(), analyst: make(chan struct{}, analystCallsAtOnce)}
	var impl gen.StrictServerInterface = srv
	if cfg.Slices != nil {
		if cfg.Store == nil {
			panic("api: Config.Slices needs Config.Store")
		}
		impl = &sliceServer{server: srv, svc: cfg.Slices, store: cfg.Store, workers: workersOf(cfg), projects: cfg.Projects, runs: newRuns(cfg, cfg.Slices)}
	}
	mux := http.NewServeMux()
	gen.HandlerWithOptions(gen.NewStrictHandlerWithOptions(impl, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: invalidRequest,
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "internal error", http.StatusInternalServerError)
		},
	}), gen.StdHTTPServerOptions{BaseRouter: mux, ErrorHandlerFunc: invalidRequest})
	mustRouteEveryOperation(mux, c)
	return &front{
		contract:  c,
		mux:       mux,
		tokens:    acceptedTokens(cfg.Tokens, cfg.WorkerGrants),
		grants:    cfg.GrantsDir,
		limit:     newLimiter(now, c.perSecond),
		anonymous: newLimiter(now, c.perAddress),
		keys:      &keyStore{now: now, keep: c.keepKeys, entries: map[string]*keyEntry{}},
	}
}

// workersOf is the handler's worker registry: Config.Workers (or a new one), reading the grants
// folder and, when Config.Store keeps registrations, holding them there (ADR 0071) and starting
// from those it kept.
func workersOf(cfg Config) *Workers {
	w := cfg.Workers
	if w == nil {
		w = NewWorkers()
	}
	w.mu.Lock()
	w.dir = cfg.GrantsDir
	w.kept, _ = cfg.Store.(registrationStore)
	w.mu.Unlock()
	if err := w.load(); err != nil {
		log.Printf("engine: reading the kept step worker registrations: %v; the workers must register again", err)
	}
	return w
}

// mustRouteEveryOperation checks that the generated routes and the contract read by
// loadContract name the same operations.
func mustRouteEveryOperation(mux *http.ServeMux, c contract) {
	param := regexp.MustCompile(`\{[^}]+\}`)
	for route := range c.ops {
		method, path, _ := strings.Cut(route, " ")
		_, pattern := mux.Handler(httptest.NewRequest(method, param.ReplaceAllString(path, "x"), nil))
		if pattern != route {
			panic(fmt.Sprintf("engine contract: %s is not served (got %q); run scripts/generate.sh", route, pattern))
		}
	}
}

type front struct {
	contract contract
	mux      *http.ServeMux
	tokens   []acceptedToken
	grants   string   // Config.GrantsDir
	limit    *limiter // per accepted token
	// anonymous counts requests without an accepted token, per source address.
	anonymous *limiter
	keys      *keyStore
}

type rawBodyKey struct{}

func (f *front) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tok := f.authenticate(r)
	if tok == nil {
		if wait, ok := f.anonymous.allow(sourceAddress(r)); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(wait))
			writeProblem(w, newProblem(http.StatusTooManyRequests, gen.ErrorCodeRateLimited,
				fmt.Sprintf("more than %d requests in one second without an accepted token from this address", f.contract.perAddress)))
			return
		}
		writeProblem(w, newProblem(http.StatusUnauthorized, gen.ErrorCodeUnauthorized, "send this session's token as `Authorization: Bearer <token>`"))
		return
	}
	caller := tok.scheme
	if wait, ok := f.limit.allow(tok.id); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(wait))
		writeProblem(w, newProblem(http.StatusTooManyRequests, gen.ErrorCodeRateLimited,
			fmt.Sprintf("more than %d requests in one second with this token", f.contract.perSecond)))
		return
	}
	_, route := f.mux.Handler(r)
	op, known := f.contract.ops[route]
	if !known {
		writeProblem(w, newProblem(http.StatusNotFound, gen.ErrorCodeNotFound, "no such operation"))
		return
	}
	if op.versioned {
		switch v, has := r.URL.Query()["api-version"]; {
		case !has:
			writeProblem(w, newProblem(http.StatusBadRequest, gen.ErrorCodeMissingApiVersionParameter, "add the api-version query parameter"))
			return
		case len(v) != 1 || v[0] != f.contract.version:
			p := newProblem(http.StatusBadRequest, gen.ErrorCodeUnsupportedApiVersionValue, "use one of supported_versions")
			p.SupportedVersions = &[]string{f.contract.version}
			writeProblem(w, p)
			return
		}
	}
	if !op.callers[caller] {
		writeProblem(w, newProblem(http.StatusForbidden, gen.ErrorCodeForbidden, "this token may not call "+op.id))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, f.contract.maxBody))
	if err != nil {
		writeProblem(w, newProblem(http.StatusBadRequest, gen.ErrorCodeInvalidRequest,
			fmt.Sprintf("the body cannot be read or is larger than %d bytes", f.contract.maxBody)))
		return
	}
	if len(body) > 0 && !jsonContent(r.Header.Values("Content-Type")) {
		writeProblem(w, newProblem(http.StatusBadRequest, gen.ErrorCodeInvalidRequest,
			"send the body as `Content-Type: application/json`"))
		return
	}
	if code, detail := op.checkBody(body); code != "" {
		writeProblem(w, newProblem(http.StatusBadRequest, gen.ErrorCode(code), detail))
		return
	}
	ctx := context.WithValue(r.Context(), rawBodyKey{}, body)
	if tok.grant != nil {
		ctx = context.WithValue(ctx, grantKey{}, *tok.grant)
	}
	if tok.file != "" {
		ctx = context.WithValue(ctx, grantFileKey{}, tok.file)
	}
	r = r.WithContext(ctx)
	r.Body = io.NopCloser(bytes.NewReader(body))

	keys := r.Header.Values("Idempotency-Key")
	if !op.idempotent || len(keys) == 0 {
		f.mux.ServeHTTP(w, r)
		return
	}
	if n := len(keys[0]); len(keys) > 1 || n < f.contract.keyMin || n > f.contract.keyMax {
		writeProblem(w, newProblem(http.StatusBadRequest, gen.ErrorCodeInvalidRequest,
			fmt.Sprintf("send one Idempotency-Key of %d to %d characters", f.contract.keyMin, f.contract.keyMax)))
		return
	}
	f.keys.serve(w, r, tok.id+"\x00"+keys[0], sha256.Sum256([]byte(r.Method+" "+r.URL.Path+"\x00"+string(body))), f.mux)
}

// jsonContent reports whether a request's Content-Type header values are exactly one media
// type, application/json, with any parameters (the contract declares no other body type).
func jsonContent(values []string) bool {
	if len(values) != 1 {
		return false
	}
	media, _, err := mime.ParseMediaType(values[0])
	return err == nil && media == "application/json"
}

// authenticate returns the accepted token the request's bearer token is, or nil for none. Every
// accepted token is compared, in constant time each; a token none of them is may still have a
// grant in the grants folder (grantFromDir).
func (f *front) authenticate(r *http.Request) *acceptedToken {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return nil
	}
	var found *acceptedToken
	for i := range f.tokens {
		if t := &f.tokens[i]; subtle.ConstantTimeCompare([]byte(t.token), []byte(token)) == 1 && found == nil {
			found = t
		}
	}
	if found == nil && f.grants != "" {
		found = grantFromDir(f.grants, token)
	}
	return found
}

// sourceAddress returns the host part of the connection's peer address, with an IPv4-mapped
// IPv6 address written as IPv4, so one peer has one key. It never reads a header the client
// sends. A peer address without a port is used whole.
func sourceAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if a, err := netip.ParseAddr(host); err == nil {
		return a.Unmap().String()
	}
	return host
}

// limiter allows each key (a token's scheme or a source address) at most perSecond requests
// in any one-second sliding window.
//
// Its memory is bounded: a window whose latest request is a second old or more holds no time
// still in the window, so it is the same as no window and may be dropped. When a new key would
// make the map hold sweepAt windows, allow first drops all such windows and sets sweepAt to
// twice what is left, at least minSweep. What is left are keys seen in the last second, so the
// map never holds more than max(minSweep, 2*A) windows of at most perSecond times each, where
// A is the most distinct keys seen in any one second. A sweep is one pass over the map and
// comes only after the map has doubled since the last one: amortized constant work per new key.
type limiter struct {
	now       func() time.Time
	perSecond int

	mu      sync.Mutex
	windows map[string]*window
	sweepAt int
}

// minSweep is the fewest windows a limiter holds before it sweeps.
const minSweep = 64

func newLimiter(now func() time.Time, perSecond int) *limiter {
	return &limiter{now: now, perSecond: perSecond, windows: map[string]*window{}, sweepAt: minSweep}
}

// window holds the times of the key's last perSecond allowed requests, oldest at next.
type window struct {
	times []time.Time
	next  int
	last  time.Time
}

// allow counts one request for the key, or says how many seconds to wait. A clock that
// moves backwards is held at the latest time seen.
func (l *limiter) allow(key string) (wait int, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w, has := l.windows[key]
	if !has {
		if len(l.windows) >= l.sweepAt {
			for k, old := range l.windows {
				if !now.Before(old.last.Add(time.Second)) {
					delete(l.windows, k)
				}
			}
			l.sweepAt = max(minSweep, 2*len(l.windows))
		}
		w = &window{}
		l.windows[key] = w
	}
	if now.Before(w.last) {
		now = w.last
	}
	w.last = now
	if len(w.times) < l.perSecond {
		w.times = append(w.times, now)
		return 0, true
	}
	if free := w.times[w.next].Add(time.Second); now.Before(free) {
		return max(1, int(math.Ceil(free.Sub(now).Seconds()))), false
	}
	w.times[w.next] = now
	w.next = (w.next + 1) % l.perSecond
	return 0, true
}

// keyStore remembers the response to each idempotency key for keep (Principle XV, research D15).
// Keys expire in the order they were stored, so expired ones are removed from the front of
// order, and the store holds no more keys than the rate limit lets in during keep.
type keyStore struct {
	now  func() time.Time
	keep time.Duration

	mu      sync.Mutex
	entries map[string]*keyEntry
	order   []*keyEntry
}

type keyEntry struct {
	key     string
	request [32]byte
	expires time.Time
	done    chan struct{}              // closed when the first call has ended
	resp    *httptest.ResponseRecorder // nil if the first call did not finish
}

func (k *keyStore) serve(w http.ResponseWriter, r *http.Request, key string, request [32]byte, next http.Handler) {
	for {
		k.mu.Lock()
		now := k.now()
		for len(k.order) > 0 && !now.Before(k.order[0].expires) {
			if old := k.order[0]; k.entries[old.key] == old {
				delete(k.entries, old.key)
			}
			k.order[0] = nil
			k.order = k.order[1:]
		}
		e, seen := k.entries[key]
		if seen && !now.Before(e.expires) { // a clock that moved backwards kept it at the back
			seen = false
		}
		if !seen {
			e = &keyEntry{key: key, request: request, expires: now.Add(k.keep), done: make(chan struct{})}
			k.entries[key] = e
			k.order = append(k.order, e)
		}
		k.mu.Unlock()

		if !seen {
			k.first(e, r, next)
		} else if e.request != request {
			writeProblem(w, newProblem(http.StatusConflict, gen.ErrorCodeIdempotencyKeyReused, "this Idempotency-Key was used with a different request"))
			return
		}
		<-e.done
		if e.resp == nil {
			if !seen {
				return // the first call panicked; net/http reports it
			}
			continue // the first call did not finish; this one takes its place
		}
		for name, values := range e.resp.Header() {
			w.Header()[name] = values
		}
		w.WriteHeader(e.resp.Code)
		_, _ = w.Write(e.resp.Body.Bytes())
		return
	}
}

// first serves the first call with a key. If it panics, the key is dropped, so no partial
// response is replayed.
func (k *keyStore) first(e *keyEntry, r *http.Request, next http.Handler) {
	rec := httptest.NewRecorder()
	defer func() {
		if e.resp == nil {
			k.mu.Lock()
			if k.entries[e.key] == e {
				delete(k.entries, e.key)
			}
			k.mu.Unlock()
		}
		close(e.done)
	}()
	next.ServeHTTP(rec, r)
	e.resp = rec
}

// newProblem returns an RFC 9457 problem. Status 0 is an operation's error, which has no HTTP status.
func newProblem(status int, code gen.ErrorCode, detail string) gen.Problem {
	p := gen.Problem{Type: "about:blank", Title: http.StatusText(status), Code: code, Detail: &detail}
	if status == 0 {
		p.Title = "The operation failed"
	} else {
		p.Status = &status
	}
	return p
}

func writeProblem(w http.ResponseWriter, p gen.Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(*p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func invalidRequest(w http.ResponseWriter, _ *http.Request, err error) {
	writeProblem(w, newProblem(http.StatusBadRequest, gen.ErrorCodeInvalidRequest, err.Error()))
}
