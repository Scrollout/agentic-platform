// Package checksvc serves the check contract (src/check/api): the check service, which runs the
// pattern's step checks (engine/checks) over a project folder it only reads and returns the files
// an agent step left, and a procedure step's worker, which runs that step's fixed procedure
// (engine/procedures) and writes its files into the project. One program, cmd/check, serves either
// deployment (research D29).
//
// Every request passes the same checks, in this order: its token (401; a request without an
// accepted token is counted against its source address, the connection's peer address, and gets
// 429 beyond that limit), the token's rate limit (429), the api-version (400), the calls running
// (503 Busy beyond `x-limits.calls-at-once`), the body's Content-Type and size, then the body
// against the operation's request schema in the contract (400 InvalidRequest), then the
// operation, bounded by the request's `time_limit_seconds` (504, with every process it started
// killed). A response larger than `x-limits.response-body-max-bytes` is not sent (500
// ResponseTooLarge). The limits are the contract's `x-limits`.
//
// Every command a check or procedure runs is in its own process group, killed whole at the time
// limit; a process that leaves the group (setsid) escapes that kill, and only the container the
// deployment runs in (constitution VIII) contains it.
package checksvc

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentic-platform/check/api/gen"
)

// Config is what one deployment needs from its host.
type Config struct {
	// Token is the bearer token the engine calls this deployment with; required.
	Token string
	// Project is the projects folder: a request's `project` names a folder in it.
	Project string
	// Procedure and Step are the procedure and step a procedure step's worker serves (the step's
	// worker is `procedure:<Procedure>`). Both empty is the check service.
	Procedure, Step string
	// OapiCodegen is the pinned oapi-codegen binary (the generate procedure, and the go generate
	// GenerationRepeatable runs, which finds it on PATH); Template the pattern's module template
	// folder (the generate procedure).
	OapiCodegen, Template string
	// Env is added to the process's environment for every command a check or procedure runs, for
	// example GOMODCACHE, GOCACHE or GOFLAGS; a later entry wins. The service's own settings come
	// after it: no module proxy, no workspace, the local toolchain only.
	Env []string
}

// NewHandler returns the deployment's whole HTTP handler. With Procedure empty it is the check
// service (runCheck, readStepFiles); otherwise it serves that one procedure for Step
// (runProcedure). It panics on a Config without a Token or Project, or with only one of
// Procedure and Step.
func NewHandler(c Config) http.Handler {
	if c.Token == "" || c.Project == "" || (c.Procedure == "") != (c.Step == "") {
		panic("checksvc: Config needs Token and Project, and Procedure and Step together")
	}
	k := checkContract
	s := &service{cfg: c, limits: k.limits}
	mux := http.NewServeMux()
	gen.HandlerWithOptions(gen.NewStrictHandlerWithOptions(s, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeProblem(w, newProblem(http.StatusBadRequest, gen.ErrorCodeInvalidRequest, "the body does not match the contract"))
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			var p *problemError
			if errors.As(err, &p) {
				writeProblem(w, p.Problem)
				return
			}
			writeProblem(w, newProblem(http.StatusInternalServerError, gen.ErrorCodeInvalidRequest, "internal error"))
		},
	}), gen.StdHTTPServerOptions{BaseRouter: mux, ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
		writeProblem(w, newProblem(http.StatusBadRequest, gen.ErrorCodeInvalidRequest, "a parameter does not match the contract"))
	}})
	return &front{
		contract:  k,
		mux:       mux,
		token:     c.Token,
		limit:     newLimiter(time.Now, k.limits.PerToken),
		anonymous: newLimiter(time.Now, k.limits.PerAddress),
		slots:     make(chan struct{}, k.limits.CallsAtOnce),
	}
}

// routes maps the generated routes to their operationIds.
var routes = map[string]string{
	"POST /checks/{checkName}/run":         opRunCheck,
	"POST /step-files":                     opReadStepFiles,
	"POST /procedures/{procedureName}/run": opRunProcedure,
}

type front struct {
	contract  contract
	mux       *http.ServeMux
	token     string
	limit     *limiter // the engine's token
	anonymous *limiter // requests without an accepted token, per source address
	// slots holds one token per call running; a call that finds it full gets 503 Busy.
	slots chan struct{}
}

// slot is one running call's place among `x-limits.calls-at-once`. The operation takes it over
// and releases it when its work has ended, so a work that outlives its 504 holds it until its
// processes are gone; a call that never reaches its operation has it released by the front.
type slot struct {
	once    sync.Once
	taken   atomic.Bool
	release func()
}

type slotKey struct{}

func (f *front) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !f.authenticated(r) {
		if wait, ok := f.anonymous.allow(sourceAddress(r)); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(wait))
			writeProblem(w, newProblem(http.StatusTooManyRequests, gen.ErrorCodeRateLimited,
				fmt.Sprintf("more than %d requests in one second without an accepted token from this address", f.contract.limits.PerAddress)))
			return
		}
		writeProblem(w, newProblem(http.StatusUnauthorized, gen.ErrorCodeUnauthorized, "send the engine's token as `Authorization: Bearer <token>`"))
		return
	}
	if wait, ok := f.limit.allow("engine"); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(wait))
		writeProblem(w, newProblem(http.StatusTooManyRequests, gen.ErrorCodeRateLimited,
			fmt.Sprintf("more than %d requests in one second with this token", f.contract.limits.PerToken)))
		return
	}
	_, route := f.mux.Handler(r)
	op, known := routes[route]
	if !known {
		writeProblem(w, newProblem(http.StatusNotFound, gen.ErrorCodeInvalidRequest, "no such operation"))
		return
	}
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
	select {
	case f.slots <- struct{}{}:
	default:
		w.Header().Set("Retry-After", "5")
		writeProblem(w, newProblem(http.StatusServiceUnavailable, gen.ErrorCodeBusy,
			fmt.Sprintf("%d calls are already running; nothing ran", f.contract.limits.CallsAtOnce)))
		return
	}
	sl := &slot{}
	sl.release = func() { sl.once.Do(func() { <-f.slots }) }
	defer func() {
		if !sl.taken.Load() {
			sl.release()
		}
	}()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, f.contract.limits.RequestBodyMax))
	if err != nil {
		writeProblem(w, newProblem(http.StatusBadRequest, gen.ErrorCodeInvalidRequest,
			fmt.Sprintf("the body cannot be read or is larger than %d bytes", f.contract.limits.RequestBodyMax)))
		return
	}
	if !jsonContent(r.Header.Values("Content-Type")) {
		writeProblem(w, newProblem(http.StatusBadRequest, gen.ErrorCodeInvalidRequest, "send the body as `Content-Type: application/json`"))
		return
	}
	if fault := f.contract.checkBody(op, body); fault != "" {
		writeProblem(w, newProblem(http.StatusBadRequest, gen.ErrorCodeInvalidRequest, fault))
		return
	}
	if fault := stepShape(body); fault != "" {
		writeProblem(w, newProblem(http.StatusBadRequest, gen.ErrorCodeInvalidRequest, fault))
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), slotKey{}, sl))
	cw := &cappedWriter{header: http.Header{}, max: f.contract.limits.ResponseBodyMax}
	f.mux.ServeHTTP(cw, r)
	if cw.over {
		writeProblem(w, newProblem(http.StatusInternalServerError, gen.ErrorCodeResponseTooLarge,
			fmt.Sprintf("the response would be larger than %d bytes, so it was not sent", cw.max)))
		return
	}
	for k, v := range cw.header {
		w.Header()[k] = v
	}
	if cw.status == 0 {
		cw.status = http.StatusOK
	}
	w.WriteHeader(cw.status)
	_, _ = w.Write(cw.body.Bytes())
}

// cappedWriter holds an operation's response until it has ended, so one larger than
// `x-limits.response-body-max-bytes` is never sent: past the limit it keeps nothing and says so.
type cappedWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
	max    int64
	over   bool
}

func (c *cappedWriter) Header() http.Header { return c.header }

func (c *cappedWriter) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func (c *cappedWriter) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	if c.over || int64(c.body.Len()+len(b)) > c.max {
		c.over = true
		c.body.Reset()
		return len(b), nil
	}
	return c.body.Write(b)
}

// stepShape refuses a step that declares several outputs and a different number of writable
// entries (contract, Artifacts), which the schema cannot say.
func stepShape(body []byte) string {
	var b struct {
		Step struct {
			Writable []string `json:"writable"`
			Outputs  []string `json:"outputs"`
		} `json:"step"`
	}
	if json.Unmarshal(body, &b) != nil {
		return "the body does not match the contract"
	}
	if n := len(b.Step.Outputs); n > 1 && n != len(b.Step.Writable) {
		return fmt.Sprintf("the step declares %d outputs and %d writable entries; with several outputs, output i holds writable entry i", n, len(b.Step.Writable))
	}
	return ""
}

// jsonContent reports whether a request's Content-Type header values are exactly one media
// type, application/json, with any parameters.
func jsonContent(values []string) bool {
	if len(values) != 1 {
		return false
	}
	media, _, err := mime.ParseMediaType(values[0])
	return err == nil && media == "application/json"
}

// authenticated reports whether the request carries the engine's token, compared in constant time.
func (f *front) authenticated(r *http.Request) bool {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	return ok && strings.EqualFold(scheme, "Bearer") && token != "" &&
		subtle.ConstantTimeCompare([]byte(token), []byte(f.token)) == 1
}

// sourceAddress returns the host part of the connection's peer address, never a header.
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

// limiter allows each key at most perSecond requests in any one-second sliding window. A window
// whose latest request is a second old holds nothing in the window and is dropped when the map
// has grown, so its memory is bounded by the keys seen in the last second.
type limiter struct {
	now       func() time.Time
	perSecond int

	mu      sync.Mutex
	windows map[string][]time.Time
}

func newLimiter(now func() time.Time, perSecond int) *limiter {
	return &limiter{now: now, perSecond: perSecond, windows: map[string][]time.Time{}}
}

// allow counts one request for the key, or says how many seconds to wait.
func (l *limiter) allow(key string) (wait int, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.windows) > 1024 {
		for k, ts := range l.windows {
			if len(ts) == 0 || !now.Before(ts[len(ts)-1].Add(time.Second)) {
				delete(l.windows, k)
			}
		}
	}
	ts := l.windows[key]
	for len(ts) > 0 && !now.Before(ts[0].Add(time.Second)) {
		ts = ts[1:]
	}
	if len(ts) >= l.perSecond {
		l.windows[key] = ts
		return max(1, int(math.Ceil(ts[0].Add(time.Second).Sub(now).Seconds()))), false
	}
	l.windows[key] = append(ts, now)
	return 0, true
}

// problemError is a problem an operation answers with; the strict handler's error hook writes it.
type problemError struct{ gen.Problem }

func (p *problemError) Error() string { return string(p.Code) }

func fault(status int, code gen.ErrorCode, detail string) error {
	return &problemError{newProblem(status, code, detail)}
}

func newProblem(status int, code gen.ErrorCode, detail string) gen.Problem {
	return gen.Problem{Type: "about:blank", Title: http.StatusText(status), Status: &status, Code: code, Detail: &detail}
}

func writeProblem(w http.ResponseWriter, p gen.Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(*p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
