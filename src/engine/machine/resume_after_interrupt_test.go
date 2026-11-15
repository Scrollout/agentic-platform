package machine_test

// T102 (owner, 2026-11-15; FR-020): a resume right after a run is seen `interrupted` is not refused.
// The machine records RunInterrupted and releases its lock, and only later marks the run as no longer
// driven; a Resume in that gap must wait for the driver to stop and then resume the run, not answer
// ErrRunActive. Written before the code by a separate author. No seam is needed to catch the gap: a
// poller calls Resume the moment Get shows `interrupted`, repeated many times. A deterministic hold
// between recording RunInterrupted and the end of driving would need a hook that does not exist in
// the machine, so only the repeated version is written.

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
)

// alwaysFailingWorker returns an error on every attempt, so every run is interrupted. It keeps no
// state, so it is safe to call from the driver goroutines of Start and Resume.
type alwaysFailingWorker struct{}

func (alwaysFailingWorker) Run(context.Context, machine.Request) (machine.Result, error) {
	return machine.Result{}, errors.New("model unavailable")
}

func newInterruptingMachine() (*machine.Machine, *sink) {
	s := &sink{}
	return machine.New(machine.Config{Pattern: pattern(), Worker: alwaysFailingWorker{}, Checker: &checker{},
		Clock: &clock{now: t0}, Events: s}), s
}

func startParams(id string) machine.StartParams {
	return (&fixture{}).params(id)
}

func TestResumeRightAfterInterruptedIsAccepted(t *testing.T) {
	const repetitions = 200
	deadline := time.Now().Add(100 * time.Second)
	for i := 0; i < repetitions; i++ {
		if time.Now().After(deadline) {
			t.Fatalf("out of time after %d of %d repetitions", i, repetitions)
		}
		m, _ := newInterruptingMachine()
		started := make(chan error, 1)
		go func() {
			_, err := m.Start(context.Background(), startParams("RUN-1"))
			started <- err
		}()

		// Resume the moment Get shows interrupted.
		for {
			r, found := m.Get("RUN-1")
			if found && r.Status == machine.StatusInterrupted {
				break
			}
			runtime.Gosched()
		}
		rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		r, err := m.Resume(rctx, "RUN-1")
		cancel()
		if err != nil {
			t.Fatalf("repetition %d: Resume as soon as Get showed interrupted: %v (want it accepted, never ErrRunActive)", i, err)
		}
		// the worker still fails, so the resumed run is interrupted again
		if r.Status != machine.StatusInterrupted {
			t.Fatalf("repetition %d: resumed run is %s, want interrupted again", i, r.Status)
		}
		if err := <-started; err != nil {
			t.Fatalf("repetition %d: Start: %v", i, err)
		}
	}
}

func TestResumeOfARunStillRunningIsStillRefused(t *testing.T) {
	var resumed atomic.Bool
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	w := &gatedWorker{run: func(machine.Request) (machine.Result, error) {
		if !resumed.Load() {
			return machine.Result{}, errors.New("model unavailable")
		}
		once.Do(func() { close(entered) })
		<-release
		return machine.Result{}, errors.New("model unavailable")
	}}
	s := &sink{}
	m := machine.New(machine.Config{Pattern: pattern(), Worker: w, Checker: &checker{}, Clock: &clock{now: t0}, Events: s})

	// A run that is running, not interrupted: Resume answers ErrNotInterrupted.
	running := make(chan struct{})
	var once2 sync.Once
	w2 := &gatedWorker{run: func(machine.Request) (machine.Result, error) {
		once2.Do(func() { close(running) })
		<-release
		return machine.Result{}, errors.New("model unavailable")
	}}
	m2 := machine.New(machine.Config{Pattern: pattern(), Worker: w2, Checker: &checker{}, Clock: &clock{now: t0}, Events: &sink{}})
	done2 := make(chan struct{})
	go func() { defer close(done2); m2.Start(context.Background(), startParams("RUN-1")) }()
	<-running
	if r, _ := m2.Get("RUN-1"); r.Status != machine.StatusRunning {
		t.Fatalf("setup: run is %s, want running", r.Status)
	}
	if _, err := m2.Resume(context.Background(), "RUN-1"); !errors.Is(err, machine.ErrNotInterrupted) {
		t.Fatalf("Resume of a running run: err = %v, want ErrNotInterrupted", err)
	}

	// An interrupted run that is resumed and still driving: a second Resume is refused.
	if r, err := m.Start(context.Background(), startParams("RUN-1")); err != nil || r.Status != machine.StatusInterrupted {
		t.Fatalf("setup: Start = %s, %v; want interrupted", r.Status, err)
	}
	resumed.Store(true)
	first := make(chan error, 1)
	go func() { _, err := m.Resume(context.Background(), "RUN-1"); first <- err }()
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("the first Resume never reached the worker")
	}
	before := s.count("RunResumed")
	// While the first drives, the run is running, so it is "not interrupted".
	_, err := m.Resume(context.Background(), "RUN-1")
	if !errors.Is(err, machine.ErrNotInterrupted) {
		t.Fatalf("second Resume while the first drives: err = %v, want ErrNotInterrupted", err)
	}
	if err == nil || s.count("RunResumed") != before {
		t.Fatalf("second Resume was accepted or left an event: err=%v RunResumed %d -> %d", err, before, s.count("RunResumed"))
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first Resume: %v", err)
	}
	<-done2
}

type gatedWorker struct {
	run func(machine.Request) (machine.Result, error)
}

func (g *gatedWorker) Run(_ context.Context, r machine.Request) (machine.Result, error) {
	return g.run(r)
}
