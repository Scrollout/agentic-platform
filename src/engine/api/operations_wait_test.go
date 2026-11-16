package api

// T101: Config.Running is a wait group that counts every operation's work: it is added to before
// the operation's goroutine starts and done when the work has ended, so a host that cancels
// Config.Context and waits on it knows no operation still runs. Judged from the Config field's
// documented behaviour only (tasks.md T101). Written before the code by a separate author.

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
)

// stubbornFor is how long a piece of work keeps going after its context is cancelled.
const stubbornFor = 300 * time.Millisecond

// stubborn is work that, once its context is cancelled, ignores it for stubbornFor and only then
// ends. It reports that it started, and records when it ended.
type stubborn struct {
	started chan struct{}
	ended   atomic.Int32
}

func newStubborn() *stubborn { return &stubborn{started: make(chan struct{}, 16)} }

func (s *stubborn) do(ctx context.Context) {
	s.started <- struct{}{}
	<-ctx.Done()
	time.Sleep(stubbornFor)
	s.ended.Add(1)
}

func (s *stubborn) awaitStarted(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-s.started:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d of %d pieces of work started", i, n)
		}
	}
}

// waitReturns runs wg.Wait and reports how long it took; it fails after 10 seconds.
func waitReturns(t *testing.T, wg *sync.WaitGroup) time.Duration {
	t.Helper()
	start := time.Now()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Running.Wait() did not return within 10 seconds of the cancellation")
	}
	return time.Since(start)
}

// runningCase cancels the engine's context while n pieces of work have started and holds the
// common assertions: Wait returns only after every piece of work returned.
func runningCase(t *testing.T, wg *sync.WaitGroup, cancel context.CancelFunc, s *stubborn, n int) {
	t.Helper()
	s.awaitStarted(t, n)
	cancel()
	took := waitReturns(t, wg)
	if got := s.ended.Load(); int(got) != n {
		t.Fatalf("Running.Wait() returned with %d of %d pieces of work ended; it must wait for every one", got, n)
	}
	if took < stubbornFor/2 {
		t.Fatalf("Running.Wait() returned %v after the cancellation, before work that ignores it for %v could end", took, stubbornFor)
	}
}

func TestRunningCountsEveryOperationUntilItsWorkHasEnded(t *testing.T) {
	t.Run("the host's Work (createSlice without Slices)", func(t *testing.T) {
		var wg sync.WaitGroup
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := newStubborn()
		r := newRig(t, func(c *Config) {
			c.Context, c.Running = ctx, &wg
			c.Work = func(ctx context.Context, job Job) (WorkResult, error) {
				s.do(ctx)
				return WorkResult{Slice: job.Target}, nil
			}
		})
		var ids []string
		for i := 0; i < 2; i++ {
			r.clock.Advance(time.Second)
			ids = append(ids, r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, http.StatusAccepted).Id)
		}
		runningCase(t, &wg, cancel, s, 2)
		for _, id := range ids {
			r.clock.Advance(time.Second)
			op := r.do("GET", "/operations/"+id+"?api-version="+apiVersion, clientToken, "", nil).operation(t, 200)
			if !ended(op.Status) {
				t.Errorf("operation %s is %q after Running.Wait() returned, want an ended status", id, op.Status)
			}
		}
	})

	t.Run("a slice operation served by the slices service", func(t *testing.T) {
		var wg sync.WaitGroup
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := newStubborn()
		sp := newSP(t, []spResult{{p: spNotes(t)}}, nil, func(c *slices.Config) {
			c.Analyst = stubbornAnalyst{s: s, next: &spAnalyst{results: []spResult{{p: spNotes(t)}}}}
		})
		r := newRig(t, func(c *Config) {
			c.Context, c.Running = ctx, &wg
			c.Slices, c.Store = sp.svc, sp.store
		})
		r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, http.StatusAccepted)
		runningCase(t, &wg, cancel, s, 1)
	})

	t.Run("a run operation", func(t *testing.T) {
		var wg sync.WaitGroup
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := newStubborn()
		x := newRunsRigWith(t, ctx, &wg)
		x.w.set(func(ctx context.Context, r machine.Request) (machine.Result, error) {
			s.do(ctx)
			return machine.Result{}, ctx.Err()
		})
		x.startOK("notes-app")
		runningCase(t, &wg, cancel, s, 1)
	})
}

func TestRunningIsOptional(t *testing.T) {
	t.Run("the host's Work", func(t *testing.T) {
		r := newRig(t, func(c *Config) { c.Running = nil })
		op := r.do("POST", slicesURL(""), clientToken, createBody, nil).operation(t, http.StatusAccepted)
		if got := r.waitFor(t, op.Id, "succeeded"); got.Result == nil {
			t.Fatalf("operation %s succeeded with no result", op.Id)
		}
	})
	t.Run("a run", func(t *testing.T) {
		x := newRunsRigWith(t, context.Background(), nil)
		op := x.startOK("notes-app")
		x.waitEnded(op.Target, "done")
	})
}

// stubbornAnalyst is an analyst that ignores its context's cancellation (see stubborn).
type stubbornAnalyst struct {
	s    *stubborn
	next slices.Analyst
}

func (a stubbornAnalyst) Propose(ctx context.Context, in slices.AnalystInput) (slices.Proposal, error) {
	a.s.do(ctx)
	return a.next.Propose(context.Background(), in)
}

// newRunsRigWith is newRunsRig with the handler's Context and Running set. A nil running leaves
// Config.Running unset.
func newRunsRigWith(t *testing.T, ctx context.Context, running *sync.WaitGroup) *runsRig {
	t.Helper()
	st, err := store.Open(t.TempDir(), spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	sp := newSP(t, []spResult{{p: spNotes(t)}}, st)
	sp.approveAfterSeed(t)
	x := &runsRig{t: t, sp: sp, st: st, projects: t.TempDir(),
		w: &fakeStepWorker{st: st, data: map[string][]byte{}}, c: &fakeStepChecker{}}
	x.r = newRig(t, func(cfg *Config) {
		cfg.Context = ctx
		cfg.Running = running
		cfg.Slices, cfg.Store = x.sp.svc, x.st
		cfg.WorkerGrants = stepGrants("S-1")
		cfg.Projects = x.projects
		cfg.Model = "test-model"
		cfg.StepWorker, cfg.StepChecker = x.w, x.c
	})
	registerAll(t, x.r, "S-1")
	return x
}
