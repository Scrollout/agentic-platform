package api

// Tests for the engine skeleton (T015). They are written before the code (T018) and
// fail to compile until it exists. The tests are the specification; the author of
// T018 must not change them (constitution: "Tests change only through a fresh agent").
//
// # Constructor API the tests use (implement exactly this in package api)
//
//	// Tokens are the three bearer tokens of the session (research D12). Empty means
//	// "no token of that kind"; an empty token never matches.
//	type Tokens struct{ Client, Page, Worker string }
//
//	// Job is one unit of work behind a state-changing call.
//	type Job struct {
//		Kind   gen.OperationKind // the operation's kind, e.g. propose-slice
//		Target string            // slice or run ID the operation concerns (S-<n> or RUN-<n>)
//		Body   []byte            // the request body as received
//	}
//
//	// WorkResult is the result of a succeeded job; exactly one field is set.
//	type WorkResult struct{ Slice, Run, ReusedSlice string }
//
//	// WorkError ends the operation `failed` at once, without retries; its Code and
//	// Detail become the operation's `error` problem.
//	type WorkError struct {
//		Code   gen.ErrorCode
//		Detail string
//	}
//	func (*WorkError) Error() string
//
//	// Retryable wraps err so the operation store retries the job with exponential
//	// backoff and jitter up to 3 times after the first attempt (4 calls in all),
//	// then keeps the operation visible as `failed` with err's text in `error.detail`
//	// (research D7). Any other error from Work is not retried and also ends `failed`.
//	func Retryable(err error) error
//
//	type Config struct {
//		Tokens Tokens
//		// Now is the clock for the rate limit (20 requests per second per token) and
//		// for how long idempotency keys are kept (24 hours). nil means time.Now.
//		Now func() time.Time
//		// Sleep waits between retries. nil means a real, context-aware wait.
//		Sleep func(d time.Duration)
//		// Work does the job behind a state-changing call. The skeleton calls it in its
//		// own goroutine after answering 202; ctx is cancelled when the operation is
//		// cancelled. Work is required for state-changing calls.
//		Work func(ctx context.Context, job Job) (WorkResult, error)
//	}
//
//	// NewHandler returns the engine's whole HTTP handler: version check, token
//	// authentication and per-operation authorization, rate limit, idempotency,
//	// the operation store and the provider operations list, in front of the
//	// generated routes (gen.NewStrictHandler on net/http).
//	func NewHandler(cfg Config) http.Handler
//
// # What the skeleton serves without slice logic
//
// These operations make sense for the skeleton and are the ones the tests call:
//   - listSlices: 200 with an empty page ({"items":[]}) while no slice exists.
//   - createSlice: the one state-changing call used; it returns 202 with a
//     `propose-slice` operation whose target the skeleton allocates (S-<n>), and Work
//     decides the outcome. (The analyst and the slice store are not part of the skeleton.)
//   - getOperation and cancelOperation: operation store behaviour.
//   - listProviderOperations: derived from the contract's x-operation-name values.
//   - registerWorker is only used for its 403 (a client token is not a worker token).
// Every other operation answers through the same pipeline; its logic is later work.
//
// # Order of checks the tests assume
//
// Token (401), then rate limit (429, counted per token for every request that carries
// a known token), then api-version (400, except on the provider operations list), then
// authorization of the token for the operation (403), then the operation itself.
// The tests only ever send one defect at a time, so only the 429 order matters: a
// request over the limit gets 429 whatever else it carries.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/api/gen"
)

const (
	apiVersion   = "2026-11-12-preview"
	clientToken  = "client-token-for-tests"
	pageToken    = "page-token-for-tests"
	workerToken  = "worker-token-for-tests"
	contractPath = "preview/2026-11-12-preview/openapi.yaml"
)

// fakeClock is a clock the tests move by hand.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)}
}
func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// recorder counts calls to Work and lets a test choose what Work does.
type recorder struct {
	mu    sync.Mutex
	calls []Job
	fn    func(ctx context.Context, job Job) (WorkResult, error)
}

func (r *recorder) work(ctx context.Context, job Job) (WorkResult, error) {
	r.mu.Lock()
	r.calls = append(r.calls, job)
	fn := r.fn
	r.mu.Unlock()
	if fn == nil {
		return WorkResult{Slice: job.Target}, nil
	}
	return fn(ctx, job)
}
func (r *recorder) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.calls) }

type rig struct {
	h     http.Handler
	clock *fakeClock
	rec   *recorder
}

func newRig(t *testing.T, mutate func(*Config)) *rig {
	t.Helper()
	r := &rig{clock: newFakeClock(), rec: &recorder{}}
	cfg := Config{
		Tokens: Tokens{Client: clientToken, Page: pageToken, Worker: workerToken},
		Now:    r.clock.Now,
		Sleep:  func(time.Duration) {},
		Work:   r.rec.work,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	r.h = NewHandler(cfg)
	return r
}

type reply struct {
	*httptest.ResponseRecorder
}

func (r *rig) do(method, path, token, body string, hdr map[string]string) reply {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	return reply{w}
}

func (p reply) problem(t *testing.T, status int, code gen.ErrorCode) gen.Problem {
	t.Helper()
	if p.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", p.Code, status, p.Body.String())
	}
	if ct := p.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	var pr gen.Problem
	if err := json.Unmarshal(p.Body.Bytes(), &pr); err != nil {
		t.Fatalf("body is not a problem: %v: %s", err, p.Body.String())
	}
	if pr.Code != code {
		t.Fatalf("code = %q, want %q; body: %s", pr.Code, code, p.Body.String())
	}
	if pr.Title == "" || pr.Type == "" {
		t.Fatalf("problem lacks title or type: %s", p.Body.String())
	}
	return pr
}

func (p reply) operation(t *testing.T, status int) gen.Operation {
	t.Helper()
	if p.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", p.Code, status, p.Body.String())
	}
	if ct := p.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var op gen.Operation
	if err := json.Unmarshal(p.Body.Bytes(), &op); err != nil {
		t.Fatalf("body is not an operation: %v: %s", err, p.Body.String())
	}
	if !regexp.MustCompile(`^OP-[0-9]+$`).MatchString(op.Id) {
		t.Fatalf("operation id %q does not match ^OP-[0-9]+$", op.Id)
	}
	if !regexp.MustCompile(`^(S|RUN)-[0-9]+$`).MatchString(op.Target) {
		t.Fatalf("operation target %q does not match the contract", op.Target)
	}
	return op
}

func slicesURL(extra string) string { return "/slices?api-version=" + apiVersion + extra }

const createBody = `{"description":"A shopper can add an item to a basket."}`

// waitFor follows the operation with getOperation until its status is want.
func (r *rig) waitFor(t *testing.T, id string, want gen.OperationStatus) gen.Operation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		op := r.do("GET", "/operations/"+id+"?api-version="+apiVersion, clientToken, "", nil).operation(t, 200)
		if op.Status == want {
			return op
		}
		if time.Now().After(deadline) {
			t.Fatalf("operation %s is %q after 5s, want %q", id, op.Status, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// --- api-version (ADR 0004) ---

func TestMissingApiVersionIs400(t *testing.T) {
	r := newRig(t, nil)
	r.do("GET", "/slices", clientToken, "", nil).problem(t, 400, gen.ErrorCodeMissingApiVersionParameter)
	r.do("POST", "/slices", clientToken, createBody, nil).problem(t, 400, gen.ErrorCodeMissingApiVersionParameter)
	r.do("GET", "/operations/OP-1", clientToken, "", nil).problem(t, 400, gen.ErrorCodeMissingApiVersionParameter)
	if r.rec.count() != 0 {
		t.Fatal("Work ran for a request without api-version")
	}
}

func TestUnknownApiVersionIs400ListingSupportedVersions(t *testing.T) {
	r := newRig(t, nil)
	for _, v := range []string{"2020-01-01", "2026-11-12", "2026-11-13-preview", "banana", ""} {
		p := r.do("GET", "/slices?api-version="+v, clientToken, "", nil).problem(t, 400, gen.ErrorCodeUnsupportedApiVersionValue)
		if p.SupportedVersions == nil || len(*p.SupportedVersions) == 0 {
			t.Fatalf("version %q: supported_versions missing: %v", v, p)
		}
		found := false
		for _, s := range *p.SupportedVersions {
			found = found || s == apiVersion
		}
		if !found {
			t.Fatalf("version %q: supported_versions %v lacks %s", v, *p.SupportedVersions, apiVersion)
		}
	}
}

// --- authentication and authorization (research D12) ---

func TestNoTokenIs401(t *testing.T) {
	r := newRig(t, nil)
	r.do("GET", slicesURL(""), "", "", nil).problem(t, 401, gen.ErrorCodeUnauthorized)
	r.do("POST", slicesURL(""), "", createBody, nil).problem(t, 401, gen.ErrorCodeUnauthorized)
	r.do("GET", "/providers/Agentic.Engine/operations", "", "", nil).problem(t, 401, gen.ErrorCodeUnauthorized)
}

func TestUnknownTokenIs401(t *testing.T) {
	r := newRig(t, nil)
	r.do("GET", slicesURL(""), "not-a-token", "", nil).problem(t, 401, gen.ErrorCodeUnauthorized)
	// A non-bearer scheme is no token either.
	w := r.do("GET", slicesURL(""), "", "", map[string]string{"Authorization": "Basic " + clientToken})
	w.problem(t, 401, gen.ErrorCodeUnauthorized)
}

func TestTokenNotAllowedForTheOperationIs403(t *testing.T) {
	r := newRig(t, nil)
	// The page token may only call GET operations.
	r.do("POST", slicesURL(""), pageToken, createBody, nil).problem(t, 403, gen.ErrorCodeForbidden)
	r.do("POST", "/operations/OP-1/cancel?api-version="+apiVersion, pageToken, "", nil).problem(t, 403, gen.ErrorCodeForbidden)
	// A worker token may only call registerWorker.
	r.do("GET", slicesURL(""), workerToken, "", nil).problem(t, 403, gen.ErrorCodeForbidden)
	r.do("POST", slicesURL(""), workerToken, createBody, nil).problem(t, 403, gen.ErrorCodeForbidden)
	// The client token may not call registerWorker.
	r.do("POST", "/workers?api-version="+apiVersion, clientToken,
		`{"slice":"S-1","step":"build","endpoint":"http://worker:8080"}`, nil).problem(t, 403, gen.ErrorCodeForbidden)
	if r.rec.count() != 0 {
		t.Fatal("Work ran for a forbidden request")
	}
}

func TestPageTokenMayCallGetOperations(t *testing.T) {
	r := newRig(t, nil)
	if w := r.do("GET", slicesURL(""), pageToken, "", nil); w.Code != 200 {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
}

// --- served without slice logic ---

func TestListSlicesReturnsAnEmptyPage(t *testing.T) {
	r := newRig(t, nil)
	w := r.do("GET", slicesURL(""), clientToken, "", nil)
	if w.Code != 200 {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["items"]) != "[]" {
		t.Fatalf(`items = %s, want [] (an empty array, not null or absent)`, raw["items"])
	}
	if _, has := raw["next_page_token"]; has {
		t.Fatalf("an empty page has no next_page_token: %s", w.Body.String())
	}
}

// --- rate limit (Principle XIV, research D14) ---

func TestMoreThan20RequestsPerSecondPerTokenIs429WithRetryAfter(t *testing.T) {
	r := newRig(t, nil)
	for i := 1; i <= 20; i++ {
		if w := r.do("GET", slicesURL(""), clientToken, "", nil); w.Code != 200 {
			t.Fatalf("request %d: status = %d, want 200: %s", i, w.Code, w.Body.String())
		}
	}
	w := r.do("GET", slicesURL(""), clientToken, "", nil)
	w.problem(t, 429, gen.ErrorCodeRateLimited)
	ra, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || ra < 1 {
		t.Fatalf("Retry-After = %q, want an integer of at least 1", w.Header().Get("Retry-After"))
	}

	// The limit is per token: another token is not affected.
	if w := r.do("GET", slicesURL(""), pageToken, "", nil); w.Code != 200 {
		t.Fatalf("page token: status = %d, want 200 while the client token is limited", w.Code)
	}

	// A second later the client token may call again.
	r.clock.Advance(1100 * time.Millisecond)
	if w := r.do("GET", slicesURL(""), clientToken, "", nil); w.Code != 200 {
		t.Fatalf("after 1.1 s: status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestTwentyRequestsInASecondAreAllowedAcrossOperations(t *testing.T) {
	r := newRig(t, nil)
	for i := 1; i <= 20; i++ {
		w := r.do("GET", "/providers/Agentic.Engine/operations", clientToken, "", nil)
		if w.Code != 200 {
			t.Fatalf("request %d: status = %d, want 200", i, w.Code)
		}
	}
	r.do("GET", "/providers/Agentic.Engine/operations", clientToken, "", nil).problem(t, 429, gen.ErrorCodeRateLimited)
}

// --- idempotency (Principle XV, research D15) ---

func TestIdempotencyKeyWithTheSameBodyReturnsTheFirstResult(t *testing.T) {
	r := newRig(t, nil)
	h := map[string]string{"Idempotency-Key": "key-1"}
	first := r.do("POST", slicesURL(""), clientToken, createBody, h).operation(t, 202)
	second := r.do("POST", slicesURL(""), clientToken, createBody, h).operation(t, 202)
	if first.Id != second.Id || first.Target != second.Target {
		t.Fatalf("second call returned %s/%s, want the first result %s/%s", second.Id, second.Target, first.Id, first.Target)
	}
	r.waitFor(t, first.Id, gen.OperationStatusSucceeded)
	if n := r.rec.count(); n != 1 {
		t.Fatalf("Work ran %d times for one key, want 1", n)
	}
}

func TestIdempotencyKeyWithADifferentBodyIs409(t *testing.T) {
	r := newRig(t, nil)
	h := map[string]string{"Idempotency-Key": "key-2"}
	r.do("POST", slicesURL(""), clientToken, createBody, h).operation(t, 202)
	other := `{"description":"A shopper can remove an item from a basket."}`
	r.do("POST", slicesURL(""), clientToken, other, h).problem(t, 409, gen.ErrorCodeIdempotencyKeyReused)
}

func TestCallsWithoutAKeyAreNotDeduplicated(t *testing.T) {
	r := newRig(t, nil)
	a := r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, 202)
	b := r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, 202)
	if a.Id == b.Id {
		t.Fatalf("two calls without a key share operation %s", a.Id)
	}
}

func TestIdempotencyKeyIsKeptFor24Hours(t *testing.T) {
	r := newRig(t, nil)
	h := map[string]string{"Idempotency-Key": "key-3"}
	first := r.do("POST", slicesURL(""), clientToken, createBody, h).operation(t, 202)

	r.clock.Advance(23*time.Hour + 50*time.Minute)
	other := `{"description":"Another use case."}`
	r.do("POST", slicesURL(""), clientToken, other, h).problem(t, 409, gen.ErrorCodeIdempotencyKeyReused)

	r.clock.Advance(20 * time.Minute) // now more than 24 hours after the first call
	again := r.do("POST", slicesURL(""), clientToken, other, h).operation(t, 202)
	if again.Id == first.Id {
		t.Fatalf("the key was still bound after 24 hours")
	}
}

// --- the operations list (ADR 0004) ---

// contractOperationNames reads every x-operation-name from the contract.
func contractOperationNames(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^\s*x-operation-name:\s*(\S+)\s*$`)
	names := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
		names[m[1]] = true
	}
	if len(names) < 15 {
		t.Fatalf("found only %d operation names in %s; the pattern no longer fits the contract", len(names), contractPath)
	}
	return names
}

func TestProviderOperationsListsEveryOperationInTheContract(t *testing.T) {
	want := contractOperationNames(t)
	r := newRig(t, nil)

	got := map[string]bool{}
	path := "/providers/Agentic.Engine/operations" // no api-version
	for page := 0; ; page++ {
		if page > 10 {
			t.Fatal("more than 10 pages")
		}
		w := r.do("GET", path, pageToken, "", nil)
		if w.Code != 200 {
			t.Fatalf("status = %d: %s", w.Code, w.Body.String())
		}
		var list gen.ProviderOperationList
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Items) > 50 {
			t.Fatalf("page has %d items, at most 50", len(list.Items))
		}
		for _, it := range list.Items {
			if it.Display == "" {
				t.Errorf("operation %s has no display text", it.Name)
			}
			got[it.Name] = true
		}
		if list.NextPageToken == nil || *list.NextPageToken == "" {
			break
		}
		path = "/providers/Agentic.Engine/operations?page_token=" + *list.NextPageToken
		r.clock.Advance(time.Second) // stay under the rate limit however many pages there are
	}

	var missing, extra []string
	for n := range want {
		if !got[n] {
			missing = append(missing, n)
		}
	}
	for n := range got {
		if !want[n] {
			extra = append(extra, n)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("operations list differs from the contract: missing %v, not in the contract %v", missing, extra)
	}
}

// --- state changes are operations (research D7) ---

func TestStateChangingCallReturns202WithAFollowableOperation(t *testing.T) {
	r := newRig(t, nil)
	op := r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, 202)
	if op.Kind != gen.OperationKindProposeSlice {
		t.Fatalf("kind = %q, want propose-slice", op.Kind)
	}
	if !regexp.MustCompile(`^S-[0-9]+$`).MatchString(op.Target) {
		t.Fatalf("target = %q, want a slice ID", op.Target)
	}
	if op.Status != gen.OperationStatusAccepted && op.Status != gen.OperationStatusRunning && op.Status != gen.OperationStatusSucceeded {
		t.Fatalf("status = %q at acceptance", op.Status)
	}

	done := r.waitFor(t, op.Id, gen.OperationStatusSucceeded)
	if done.Result == nil || done.Result.Slice == nil || *done.Result.Slice != op.Target {
		t.Fatalf("result = %+v, want the slice %s", done.Result, op.Target)
	}
	if done.Error != nil {
		t.Fatalf("a succeeded operation has an error: %+v", done.Error)
	}
	if !done.UpdatedAt.After(time.Time{}) || done.CreatedAt.IsZero() {
		t.Fatalf("created_at/updated_at not set: %+v", done)
	}
	// The page token may follow it too (GET).
	if w := r.do("GET", "/operations/"+op.Id+"?api-version="+apiVersion, pageToken, "", nil); w.Code != 200 {
		t.Fatalf("page token getOperation: %d", w.Code)
	}
	// Work saw the kind, target and body.
	r.rec.mu.Lock()
	defer r.rec.mu.Unlock()
	j := r.rec.calls[0]
	if j.Kind != op.Kind || j.Target != op.Target || !bytes.Equal(j.Body, []byte(createBody)) {
		t.Fatalf("job = %+v", j)
	}
}

func TestInvalidBodyIs400InvalidRequest(t *testing.T) {
	r := newRig(t, nil)
	r.do("POST", slicesURL(""), clientToken, `{"description":""}`, nil).problem(t, 400, gen.ErrorCodeInvalidRequest)
	r.do("POST", slicesURL(""), clientToken, `{"description":"x","extra":1}`, nil).problem(t, 400, gen.ErrorCodeInvalidRequest)
	r.do("POST", slicesURL(""), clientToken, `not json`, nil).problem(t, 400, gen.ErrorCodeInvalidRequest)
}

func TestUnknownOperationIs404(t *testing.T) {
	r := newRig(t, nil)
	r.do("GET", "/operations/OP-999?api-version="+apiVersion, clientToken, "", nil).problem(t, 404, gen.ErrorCodeNotFound)
	r.do("POST", "/operations/OP-999/cancel?api-version="+apiVersion, clientToken, "", nil).problem(t, 404, gen.ErrorCodeNotFound)
}

func TestAnOperationCanBeCancelled(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	r := newRig(t, nil)
	r.rec.fn = func(ctx context.Context, job Job) (WorkResult, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return WorkResult{}, ctx.Err()
	}
	op := r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, 202)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("Work did not start")
	}
	r.waitFor(t, op.Id, gen.OperationStatusRunning)

	got := r.do("POST", "/operations/"+op.Id+"/cancel?api-version="+apiVersion, clientToken, "", nil).operation(t, 202)
	if got.Id != op.Id {
		t.Fatalf("cancel returned operation %s, want %s", got.Id, op.Id)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Work's context was not cancelled")
	}
	r.waitFor(t, op.Id, gen.OperationStatusCancelled)

	// An operation that has ended cannot be cancelled again.
	r.do("POST", "/operations/"+op.Id+"/cancel?api-version="+apiVersion, clientToken, "", nil).problem(t, 409, gen.ErrorCodeOperationEnded)
}

func TestCancellingASucceededOperationIs409(t *testing.T) {
	r := newRig(t, nil)
	op := r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, 202)
	r.waitFor(t, op.Id, gen.OperationStatusSucceeded)
	r.do("POST", "/operations/"+op.Id+"/cancel?api-version="+apiVersion, clientToken, "", nil).problem(t, 409, gen.ErrorCodeOperationEnded)
}

func TestCancelWithAReusedKeyAndADifferentBodyIs409(t *testing.T) {
	r := newRig(t, nil)
	r.rec.fn = func(ctx context.Context, job Job) (WorkResult, error) {
		<-ctx.Done()
		return WorkResult{}, ctx.Err()
	}
	a := r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, 202)
	b := r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, 202)
	h := map[string]string{"Idempotency-Key": "cancel-key"}
	r.do("POST", "/operations/"+a.Id+"/cancel?api-version="+apiVersion, clientToken, "", h).operation(t, 202)
	// Same key, a different request (another operation): a different body.
	r.do("POST", "/operations/"+b.Id+"/cancel?api-version="+apiVersion, clientToken, "", h).problem(t, 409, gen.ErrorCodeIdempotencyKeyReused)
}

// --- failures are visible, retries back off (research D7) ---

func TestAPermanentFailureIsKeptVisibleWithoutRetries(t *testing.T) {
	r := newRig(t, nil)
	r.rec.fn = func(context.Context, Job) (WorkResult, error) {
		return WorkResult{}, &WorkError{Code: gen.ErrorCodeBudgetExhausted, Detail: "tokens used up"}
	}
	op := r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, 202)
	done := r.waitFor(t, op.Id, gen.OperationStatusFailed)
	if done.Error == nil || done.Error.Code != gen.ErrorCodeBudgetExhausted {
		t.Fatalf("error = %+v, want BudgetExhausted", done.Error)
	}
	if done.Error.Detail == nil || !strings.Contains(*done.Error.Detail, "tokens used up") {
		t.Fatalf("error.detail = %v", done.Error.Detail)
	}
	if done.Result != nil {
		t.Fatalf("a failed operation has a result: %+v", done.Result)
	}
	if n := r.rec.count(); n != 1 {
		t.Fatalf("Work ran %d times, want 1 (not retried)", n)
	}
}

func TestARetryableFailureIsRetriedThreeTimesThenKeptFailed(t *testing.T) {
	var mu sync.Mutex
	var sleeps []time.Duration
	r := newRig(t, func(c *Config) {
		c.Sleep = func(d time.Duration) { mu.Lock(); sleeps = append(sleeps, d); mu.Unlock() }
	})
	r.rec.fn = func(context.Context, Job) (WorkResult, error) {
		return WorkResult{}, Retryable(errors.New("the model is unreachable"))
	}
	op := r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, 202)
	done := r.waitFor(t, op.Id, gen.OperationStatusFailed)
	if n := r.rec.count(); n != 4 {
		t.Fatalf("Work ran %d times, want 4 (first attempt and 3 retries)", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sleeps) != 3 {
		t.Fatalf("slept %d times between attempts, want 3", len(sleeps))
	}
	for _, d := range sleeps {
		if d < 0 || d > time.Minute {
			t.Fatalf("backoff %v out of range [0, 1m]", d)
		}
	}
	if done.Error == nil || done.Error.Code != gen.ErrorCodeOperationFailed || done.Error.Detail == nil ||
		!strings.Contains(*done.Error.Detail, "the model is unreachable") {
		t.Fatalf("a failed operation must keep the reason: %+v", done.Error)
	}
}

func TestARetryableFailureThatRecoversSucceeds(t *testing.T) {
	r := newRig(t, nil)
	n := 0
	r.rec.fn = func(ctx context.Context, job Job) (WorkResult, error) {
		n++ // Work calls for one operation never overlap
		if n < 3 {
			return WorkResult{}, Retryable(errors.New("not yet"))
		}
		return WorkResult{Slice: job.Target}, nil
	}
	op := r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, 202)
	done := r.waitFor(t, op.Id, gen.OperationStatusSucceeded)
	if done.Result == nil || done.Result.Slice == nil {
		t.Fatalf("result = %+v", done.Result)
	}
}

// --- contract facts, read from the contract and never as literals ---

// contractLimit reads one number from the contract's root `x-limits` object.
func contractLimit(t *testing.T, key string) int {
	t.Helper()
	raw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	in := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "x-limits:") {
			in = true
			continue
		}
		if in && !strings.HasPrefix(line, " ") {
			break
		}
		if in {
			k, v, _ := strings.Cut(strings.TrimSpace(line), ":")
			if k == key {
				n, err := strconv.Atoi(strings.TrimSpace(v))
				if err != nil || n < 1 {
					t.Fatalf("x-limits.%s = %q is not a positive number", key, v)
				}
				return n
			}
		}
	}
	t.Fatalf("x-limits.%s not found in %s", key, contractPath)
	return 0
}

type contractEntry struct{ name, summary string }

// contractEntries returns (x-operation-name, summary) for every operation under `paths:`.
// Operation keys sit at indent 6; a summary may be a plain scalar or a folded block (`>-`).
func contractEntries(t *testing.T) []contractEntry {
	t.Helper()
	raw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	var out []contractEntry
	var cur *contractEntry
	inPaths := false
	flush := func() {
		if cur != nil {
			if cur.name == "" || cur.summary == "" {
				t.Fatalf("operation without name or summary: %+v", *cur)
			}
			out = append(out, *cur)
			cur = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "paths:") {
			inPaths = true
			continue
		}
		if inPaths && line != "" && !strings.HasPrefix(line, " ") {
			break // next root key
		}
		if !inPaths || !strings.HasPrefix(line, "      ") || strings.HasPrefix(line, "       ") {
			continue
		}
		k, v, _ := strings.Cut(strings.TrimSpace(line), ":")
		v = strings.TrimSpace(v)
		switch k {
		case "operationId":
			flush()
			cur = &contractEntry{}
		case "x-operation-name":
			cur.name = v
		case "summary":
			if v == ">" || v == ">-" || v == "|" || v == "|-" {
				var parts []string
				for i+1 < len(lines) && strings.HasPrefix(lines[i+1], "       ") {
					i++
					parts = append(parts, strings.TrimSpace(lines[i]))
				}
				v = strings.Join(parts, " ")
			}
			cur.summary = strings.Trim(v, `"'`)
		}
	}
	flush()
	if len(out) < 15 {
		t.Fatalf("found only %d operations in %s", len(out), contractPath)
	}
	return out
}

// --- the rate limit holds in every one-second window ---

func TestRateLimitHoldsInAnyOneSecondWindow(t *testing.T) {
	limit := contractLimit(t, "requests-per-second-per-token")
	r := newRig(t, nil)
	burst := func(n int) (ok, limited int) {
		for i := 0; i < n; i++ {
			switch w := r.do("GET", slicesURL(""), clientToken, "", nil); w.Code {
			case 200:
				ok++
			case 429:
				limited++
			default:
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
		}
		return
	}
	if ok, lim := burst(limit); ok != limit || lim != 0 {
		t.Fatalf("t=0: %d allowed, %d limited, want %d allowed", ok, lim, limit)
	}
	r.clock.Advance(990 * time.Millisecond)
	if ok, lim := burst(limit); ok != 0 || lim != limit {
		t.Fatalf("t=0.99s: %d allowed, %d limited; the window (t-1s, t] still holds %d requests, want all limited", ok, lim, limit)
	}
	r.clock.Advance(20 * time.Millisecond) // t = 1.01 s: the first batch is out of the window
	if ok, lim := burst(limit + 1); ok != limit || lim != 1 {
		t.Fatalf("t=1.01s: %d allowed, %d limited, want %d allowed and 1 limited (limited requests do not count)", ok, lim, limit)
	}
}

// --- concurrent idempotent calls ---

func TestConcurrentCallsWithTheSameKeyRunWorkOnce(t *testing.T) {
	r := newRig(t, nil)
	const n = 15 // below the rate limit
	ids := make([]string, n)
	codes := make([]int, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w := r.do("POST", slicesURL(""), clientToken, createBody, map[string]string{"Idempotency-Key": "race-key"})
			codes[i] = w.Code
			var op gen.Operation
			if json.Unmarshal(w.Body.Bytes(), &op) == nil {
				ids[i] = op.Id
			}
		}()
	}
	close(start)
	wg.Wait()
	for i := range ids {
		if codes[i] != 202 || ids[i] != ids[0] || ids[0] == "" {
			t.Fatalf("call %d: status %d, operation %q; want 202 and %q for all", i, codes[i], ids[i], ids[0])
		}
	}
	r.waitFor(t, ids[0], gen.OperationStatusSucceeded)
	time.Sleep(50 * time.Millisecond) // a late duplicate would have started by now
	if c := r.rec.count(); c != 1 {
		t.Fatalf("Work ran %d times for one key, want 1", c)
	}
}

// --- failures without a more specific code are OperationFailed ---

func TestAnExhaustedRetryEndsFailedWithOperationFailed(t *testing.T) {
	r := newRig(t, nil)
	r.rec.fn = func(context.Context, Job) (WorkResult, error) {
		return WorkResult{}, Retryable(errors.New("still down"))
	}
	op := r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, 202)
	done := r.waitFor(t, op.Id, gen.OperationStatusFailed)
	if done.Error == nil || done.Error.Code != gen.ErrorCodeOperationFailed {
		t.Fatalf("error = %+v, want code OperationFailed", done.Error)
	}
	if done.Error.Detail == nil || !strings.Contains(*done.Error.Detail, "still down") {
		t.Fatalf("error.detail = %v, want the reason", done.Error.Detail)
	}
}

func TestAnErrorThatIsNotAWorkErrorEndsFailedWithOperationFailedAndIsNotRetried(t *testing.T) {
	r := newRig(t, nil)
	r.rec.fn = func(context.Context, Job) (WorkResult, error) {
		return WorkResult{}, errors.New("plain failure")
	}
	op := r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, 202)
	done := r.waitFor(t, op.Id, gen.OperationStatusFailed)
	if done.Error == nil || done.Error.Code != gen.ErrorCodeOperationFailed {
		t.Fatalf("error = %+v, want code OperationFailed", done.Error)
	}
	if done.Error.Detail == nil || !strings.Contains(*done.Error.Detail, "plain failure") {
		t.Fatalf("error.detail = %v, want the reason", done.Error.Detail)
	}
	if c := r.rec.count(); c != 1 {
		t.Fatalf("Work ran %d times, want 1 (only Retryable errors are retried)", c)
	}
}

// --- backoff grows, with jitter ---

// The constructor shows the backoff only through Sleep, so the test samples it. Each of the
// 3 waits of a failing job is drawn from a window that doubles from one wait to the next
// until a cap (the base and the cap are the implementer's). Over many jobs the largest wait
// seen at each position approaches its window's top (with 200 samples, a draw below 95% of
// the window has probability 0.95^200, about 3e-5). So the assertions are:
//   - the waits are not all equal at any position: there is jitter;
//   - the largest wait at position k is at most 2.2 times the largest at k-1: no window grows
//     faster than doubling (2 times, plus 10% for the sampling shortfall);
//   - the largest wait at the third position exceeds the largest at the first: the windows
//     grow (the cap is above the base);
//   - no wait is negative or longer than a minute.
func TestRetryBackoffGrowsExponentiallyWithJitter(t *testing.T) {
	const runs = 200
	var mu sync.Mutex
	var cur []time.Duration
	r := newRig(t, func(c *Config) {
		c.Sleep = func(d time.Duration) { mu.Lock(); cur = append(cur, d); mu.Unlock() }
	})
	r.rec.fn = func(context.Context, Job) (WorkResult, error) {
		return WorkResult{}, Retryable(errors.New("down"))
	}
	var waits [3][]time.Duration
	for i := 0; i < runs; i++ {
		mu.Lock()
		cur = nil
		mu.Unlock()
		op := r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, 202)
		r.waitFor(t, op.Id, gen.OperationStatusFailed)
		mu.Lock()
		if len(cur) != 3 {
			mu.Unlock()
			t.Fatalf("run %d: %d waits, want 3", i, len(cur))
		}
		for k := range waits {
			waits[k] = append(waits[k], cur[k])
		}
		mu.Unlock()
		r.clock.Advance(time.Second) // stay under the rate limit
	}
	var maxes [3]time.Duration
	for k := range waits {
		distinct := map[time.Duration]bool{}
		for _, d := range waits[k] {
			if d < 0 || d > time.Minute {
				t.Fatalf("wait %v at position %d out of range [0, 1m]", d, k)
			}
			distinct[d] = true
			maxes[k] = max(maxes[k], d)
		}
		if len(distinct) < 10 {
			t.Fatalf("position %d: only %d distinct waits in %d runs, want jitter", k, len(distinct), runs)
		}
	}
	for k := 1; k < 3; k++ {
		if float64(maxes[k]) > 2.2*float64(maxes[k-1]) {
			t.Fatalf("largest wait grew from %v to %v: faster than doubling", maxes[k-1], maxes[k])
		}
	}
	if maxes[2] <= maxes[0] {
		t.Fatalf("largest waits %v do not grow: no exponential backoff", maxes)
	}
}

// --- the operations list: its page tokens and its display texts ---

func TestProviderOperationsRejectsAPageTokenItDidNotIssue(t *testing.T) {
	r := newRig(t, nil)
	for _, tok := range []string{"not-issued", "%21%21%21", "MTAwMDA", "LTE", "e30"} { // junk, bad base64, 10000, -1, "{}"
		r.do("GET", "/providers/Agentic.Engine/operations?page_token="+tok, pageToken, "", nil).
			problem(t, 400, gen.ErrorCodeInvalidRequest)
		r.clock.Advance(time.Second)
	}
}

func TestProviderOperationsHasOneEntryPerPermissionNameWithTheJoinedSummaries(t *testing.T) {
	// Several operations share a permission name (for example slices/write). The list holds
	// one entry per name; its display is the summary of each operation with that name, in
	// contract order, joined by "; ". Both are derived from the contract.
	var order []string
	summaries := map[string][]string{}
	for _, e := range contractEntries(t) {
		if _, seen := summaries[e.name]; !seen {
			order = append(order, e.name)
		}
		summaries[e.name] = append(summaries[e.name], e.summary)
	}
	got := map[string]string{}
	r := newRig(t, nil)
	path := "/providers/Agentic.Engine/operations"
	for page := 0; page < 10; page++ {
		w := r.do("GET", path, pageToken, "", nil)
		if w.Code != 200 {
			t.Fatalf("status = %d: %s", w.Code, w.Body.String())
		}
		var list gen.ProviderOperationList
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		for _, it := range list.Items {
			if _, dup := got[it.Name]; dup {
				t.Errorf("name %s has more than one entry", it.Name)
			}
			got[it.Name] = it.Display
		}
		if list.NextPageToken == nil || *list.NextPageToken == "" {
			break
		}
		path = "/providers/Agentic.Engine/operations?page_token=" + *list.NextPageToken
		r.clock.Advance(time.Second)
	}
	for _, n := range order {
		if want := strings.Join(summaries[n], "; "); got[n] != want {
			t.Errorf("display of %s = %q, want %q", n, got[n], want)
		}
	}
	if len(got) != len(order) {
		t.Errorf("%d entries, want %d (one per permission name)", len(got), len(order))
	}
}

// --- request size ---

func TestARequestBodyOverTheLimitIs400InvalidRequest(t *testing.T) {
	limit := contractLimit(t, "request-body-max-bytes")
	r := newRig(t, nil)
	prefix, suffix := `{"description":"`, `"}`
	over := prefix + strings.Repeat("a", limit-len(prefix)-len(suffix)+1) + suffix // limit+1 bytes
	if len(over) != limit+1 {
		t.Fatalf("test body is %d bytes, want %d", len(over), limit+1)
	}
	r.do("POST", slicesURL(""), clientToken, over, nil).problem(t, 400, gen.ErrorCodeInvalidRequest)
	if r.rec.count() != 0 {
		t.Fatal("Work ran for an oversized body")
	}
	// A body of exactly the limit is accepted.
	exact := over[:len(over)-len(suffix)-1] + suffix
	r.do("POST", slicesURL(""), clientToken, exact, nil).operation(t, 202)
}

// Not tested: a contract variant with path-level `parameters` and a folded `summary: >-`
// (case 7). NewHandler takes no contract (Config has no such field; the contract is embedded),
// so the loader cannot be reached through the constructor API. contractEntries above does read
// folded summaries, so the operations-list test fails if the engine's reader mishandles them.
