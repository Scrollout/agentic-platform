package api

// T062 (feature 002): a client that half-closes its connection after sending a complete request
// (busybox `nc`, as `ap build-workers` uses) must get the request's real answer. Go's server
// cancels the request's context on that read EOF; the front's Ready check must not be asked with
// it. The fake durable service below honours its context as the real health check does.
//
// Edge cases checked: the client closes fully before the answer (no panic, nothing asserted);
// a GET query is unaffected by a half-close; the body is complete before the half-close, so a
// half-close during the body read is not applicable; a Ready that really fails still gives 503
// after a half-close; the same requests without a half-close.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	slicesvc "github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
)

// hcOps is dopOps whose Ready waits (up to 500 ms) for its context to end, then reports the
// context's error, or ErrUnavailable when down.
type hcOps struct {
	*dopOps
	down bool
}

func (o *hcOps) Ready(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(500 * time.Millisecond):
	}
	if o.down {
		return fmt.Errorf("fake durable service: %w", ErrUnavailable)
	}
	return nil
}

func hcServer(t *testing.T, down bool) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	clock := newFakeClock()
	log := &dopLog{}
	ops := newDopOps(t, dir, log)
	st, err := store.Open(dir, spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := slicesvc.New(slicesvc.Config{Schemas: spSchemas(t), Analyst: &spAnalyst{results: []spResult{{p: spNotes(t)}}},
		Diff: spDiff{}, Lint: spLint{}, Pattern: spPattern(t), Records: st})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(context.Background(), "Let me keep notes."); err != nil {
		t.Fatal(err)
	}
	(&spRig{svc: svc}).approveAll(t, "S-1")
	ctx, cancel := context.WithCancel(context.Background())
	running := &sync.WaitGroup{}
	ops.set(func(f *dopOps) { f.ctx = ctx })
	h, op := NewEngine(Config{
		Tokens: Tokens{Client: clientToken, Page: pageToken, Worker: workerToken}, Now: clock.Now,
		Sleep: func(time.Duration) {}, Work: (&recorder{}).work, Context: ctx, Running: running,
		Slices: svc, Store: st, WorkerGrants: stepGrants("S-1"), Projects: t.TempDir(), Model: "test-model",
		StepWorker: &fakeStepWorker{st: st, data: map[string][]byte{}}, StepChecker: &fakeStepChecker{},
		Workers: NewWorkers(), Runs: newDopRuns(log), Operations: &hcOps{dopOps: ops, down: down},
	})
	ops.use(op)
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		srv.Close()
		cancel()
		ops.wait(t)
		waitReturns(t, running)
	})
	return srv
}

// hcDo sends one raw request; halfClose: CloseWrite after the request; fullClose: close without
// reading. It returns the status code.
func hcDo(t *testing.T, srv *httptest.Server, method, path, token, body string, halfClose bool) int {
	t.Helper()
	c, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	req := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: engine\r\nAuthorization: Bearer %s\r\nConnection: close\r\n", method, path, token)
	if body != "" {
		req += fmt.Sprintf("Content-Type: application/json\r\nContent-Length: %d\r\n", len(body))
	}
	if _, err := io.WriteString(c, req+"\r\n"+body); err != nil {
		t.Fatal(err)
	}
	if halfClose {
		if err := c.(*net.TCPConn).CloseWrite(); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatalf("reading the answer: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	t.Logf("%s %s -> %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	hcLast.Store(string(b))
	return resp.StatusCode
}

// hcLast is the body of the last answer hcDo read (tests here run one at a time).
var hcLast atomic.Value

func hcWorkerPath() string { return "/workers?api-version=" + apiVersion }
func hcWorkerBody() string { return `{"endpoint":"http://code-worker:8080"}` }

func TestRegisterWorkerOfAHalfClosedClientIsAnswered200(t *testing.T) {
	srv := hcServer(t, false)
	if got := hcDo(t, srv, "POST", hcWorkerPath(), stepToken("S-1", "code"), hcWorkerBody(), true); got != 200 {
		t.Errorf("registerWorker after a half-close = %d, want 200", got)
	}
}

func TestAnOperationOfAHalfClosedClientIsAnswered202(t *testing.T) {
	srv := hcServer(t, false)
	if got := hcDo(t, srv, "POST", slicesURL(""), clientToken, createBody, true); got != 202 {
		t.Errorf("createSlice after a half-close = %d, want 202", got)
	}
}

func TestWithoutAHalfCloseTheRequestsAreAnswered(t *testing.T) {
	srv := hcServer(t, false)
	if got := hcDo(t, srv, "POST", hcWorkerPath(), stepToken("S-1", "code"), hcWorkerBody(), false); got != 200 {
		t.Errorf("registerWorker = %d, want 200", got)
	}
	if got := hcDo(t, srv, "POST", slicesURL(""), clientToken, createBody, false); got != 202 {
		t.Errorf("createSlice = %d, want 202", got)
	}
}

func TestAQueryOfAHalfClosedClientIsAnswered(t *testing.T) {
	srv := hcServer(t, false)
	if got := hcDo(t, srv, "GET", slicesURL(""), clientToken, "", true); got != 200 {
		t.Errorf("listSlices after a half-close = %d, want 200", got)
	}
}

func TestADurableServiceThatIsDownStillGives503AfterAHalfClose(t *testing.T) {
	srv := hcServer(t, true)
	if got := hcDo(t, srv, "POST", hcWorkerPath(), stepToken("S-1", "code"), hcWorkerBody(), true); got != 503 {
		t.Errorf("registerWorker, service down, half-close = %d, want 503", got)
	}
	if got := hcDo(t, srv, "POST", slicesURL(""), clientToken, createBody, true); got != 503 {
		t.Errorf("createSlice, service down, half-close = %d, want 503", got)
	}
	// The 503 must come from the service being down, not from the client's half-close.
	if b, _ := hcLast.Load().(string); !strings.Contains(b, "fake durable service") {
		t.Errorf("the 503 says %s; want it to name the durable service being unavailable, not a cancelled context", b)
	}
}

func TestAClientThatClosesFullyBeforeTheAnswerDoesNotBreakTheServer(t *testing.T) {
	srv := hcServer(t, false)
	c, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(c, "POST %s HTTP/1.1\r\nHost: engine\r\nAuthorization: Bearer %s\r\nConnection: close\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s",
		hcWorkerPath(), stepToken("S-1", "code"), len(hcWorkerBody()), hcWorkerBody())
	_ = c.Close()
	time.Sleep(700 * time.Millisecond)
	if got := hcDo(t, srv, "GET", slicesURL(""), clientToken, "", false); got != 200 {
		t.Errorf("a query after the abrupt close = %d, want 200", got)
	}
}
