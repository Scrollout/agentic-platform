package api

// T065 (T063 review S2; data model "Accepting", FR-001, Principle X): a run Start whose answer is
// lost (the call fails with a timeout that counts as unavailable) after the durable service created
// the workflow must neither delete the run's operation record nor leave an active run without one.
// Observable invariant asserted here: after the startRun call, for the created workflow RUN-1 an
// operation exists (GET /operations/<id> answers 200 with target RUN-1) that follows it, reached
// either from the 202 answer or, when the answer is 503, from a resend of the same request under
// the same Idempotency-Key. A 503 that leaves the workflow with no operation, and a start that is
// then answered RunActive naming RUN-1, break it. Guard: a Start that fails for real (no workflow)
// still answers 503 with no record and no active run.
// The durable service is faked with lostStartRuns over fakeRuns; both are used without change.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/store"
)

// lostStartRuns is fakeRuns whose Start fails with a timeout counting as unavailable; with create
// it first creates the workflow (the answer is lost), without it nothing was created.
type lostStartRuns struct {
	*fakeRuns
	create bool
	lose   int // Starts that fail; later ones succeed
	starts int
}

func (l *lostStartRuns) Start(ctx context.Context, r RunStart) error {
	l.mu.Lock()
	l.starts++
	n := l.starts
	l.mu.Unlock()
	if n > l.lose {
		return l.fakeRuns.Start(ctx, r)
	}
	if l.create {
		l.mu.Lock()
		l.exists[r.Run] = true
		l.mu.Unlock()
	}
	return fmt.Errorf("starting %s: %w: %w", r.Run, ErrUnavailable, context.DeadlineExceeded)
}

func newLostStartRig(t *testing.T, create bool) (*durableRig, *lostStartRuns) {
	t.Helper()
	st, err := store.Open(t.TempDir(), spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	sp := newSP(t, []spResult{{p: spNotes(t)}}, st)
	sp.approveAfterSeed(t)
	d := &durableRig{t: t, sp: sp, st: st, runs: &fakeRuns{exists: map[string]bool{}},
		w: &fakeStepWorker{st: st, data: map[string][]byte{}}, c: &fakeStepChecker{}, wk: NewWorkers()}
	lr := &lostStartRuns{fakeRuns: d.runs, create: create, lose: 1}
	projects := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d.r = newRig(t, func(cfg *Config) {
		cfg.Context = ctx
		cfg.Slices, cfg.Store = sp.svc, st
		cfg.WorkerGrants = stepGrants("S-1")
		cfg.Projects = projects
		cfg.Model = "test-model"
		cfg.StepWorker, cfg.StepChecker = d.w, d.c
		cfg.Workers = d.wk
		cfg.Runs = lr
	})
	registerAll(t, d.r, "S-1")
	return d, lr
}

const lostKey = "lost-start-key"

func (x *durableRig) startKeyed(body string) reply {
	x.r.clock.Advance(80 * time.Millisecond)
	return x.r.do("POST", spURL("S-1", "/runs"), clientToken, body, map[string]string{"Idempotency-Key": lostKey})
}

func TestARunStartWhoseAnswerWasLostLeavesAnOperationThatFollowsTheCreatedWorkflow(t *testing.T) {
	x, lr := newLostStartRig(t, true)
	p := x.startKeyed(`{"project":"notes-app"}`)
	if ok, _ := x.runs.Exists(context.Background(), "RUN-1"); !ok {
		t.Fatalf("setup: the fake did not create the workflow (answer %d)", p.Code)
	}
	first := p.Code
	if first == http.StatusServiceUnavailable {
		// 503 is allowed only when the same request is then answered with the run's own operation
		p = x.startKeyed(`{"project":"notes-app"}`)
	}
	if p.Code != http.StatusAccepted {
		t.Fatalf("startRun answered %d, then %d on a resend under the same key, want an accepted operation for the created workflow RUN-1 (not a 503 with its record deleted, not RunActive): %s",
			first, p.Code, p.Body.String())
	}
	op := x.opOf(p, 202)
	if op.Target != "RUN-1" {
		t.Fatalf("the operation's target = %q, want RUN-1, the workflow that exists (Start called %d times)", op.Target, lr.starts)
	}
	got := x.r.do("GET", "/operations/"+op.Id+"?api-version="+apiVersion, clientToken, "", nil)
	if got.Code != http.StatusOK {
		t.Fatalf("GET operation %s = %d, want 200: the workflow RUN-1 runs and its operation must exist: %s", op.Id, got.Code, got.Body.String())
	}
}

func TestARunStartWhoseAnswerWasLostNeverLeavesAnActiveRunWithoutAnOperation(t *testing.T) {
	x, _ := newLostStartRig(t, true)
	first := x.do("POST", spURL("S-1", "/runs"), `{"project":"notes-app"}`) // no key
	// a request without the key: RunActive naming RUN-1 is right only when RUN-1's operation exists
	again := x.do("POST", spURL("S-1", "/runs"), `{"project":"notes-app"}`)
	if again.Code == http.StatusConflict && strings.Contains(again.Body.String(), "RUN-1") {
		if first.Code != http.StatusAccepted {
			t.Fatalf("first startRun = %d, then RunActive names RUN-1, whose operation record was deleted: %s", first.Code, again.Body.String())
		}
		return
	}
	if first.Code == http.StatusServiceUnavailable {
		// the workflow exists and runs, so a later start must not create a second run beside it
		t.Fatalf("first startRun = 503 and the workflow RUN-1 exists, yet a resend = %d %s: a second run may start beside an untracked one",
			again.Code, again.Body.String())
	}
}

// Guard: a Start that fails for real (no workflow) is a 503 with no record and no active run.
func TestARunStartThatFailedWithNoWorkflowAnswers503AndLeavesNoRecordAndNoActiveRun(t *testing.T) {
	x, _ := newLostStartRig(t, false)
	p := x.startKeyed(`{"project":"notes-app"}`)
	unavailable(t, p)
	if ok, _ := x.runs.Exists(context.Background(), "RUN-1"); ok {
		t.Fatal("setup: a workflow exists")
	}
	if ev, _, err := readRun(x.st, "RUN-1"); err == nil && len(ev) != 0 {
		t.Errorf("a record of RUN-1 exists: %v", kindsOf(ev))
	}
	if g := x.r.do("GET", "/operations/OP-1?api-version="+apiVersion, clientToken, "", nil); g.Code == http.StatusOK {
		t.Errorf("operation OP-1 still exists after a start that created nothing: %s", g.Body.String())
	}
	// the resend is accepted, not RunActive
	if p := x.startKeyed(`{"project":"notes-app"}`); p.Code != http.StatusAccepted {
		t.Errorf("the resend = %d, want 202 (no active run): %s", p.Code, p.Body.String())
	}
}

// Edge: the lost answer, then Exists unreachable. Nothing may be answered 5xx but 503 (retryable)
// and no state may be deleted blindly; an accepted answer is fine too.
func TestARunStartWhoseAnswerWasLostAndExistsIsUnreachableAnswers202Or503(t *testing.T) {
	x, _ := newLostStartRig(t, true)
	x.runs.setDown(false)
	p := x.startKeyed(`{"project":"notes-app"}`)
	if p.Code != http.StatusAccepted && p.Code != http.StatusServiceUnavailable {
		t.Fatalf("answer = %d, want 202 or 503: %s", p.Code, p.Body.String())
	}
}
