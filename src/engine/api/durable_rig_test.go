package api

// The rig's durable service (T019, owner-approved 2026-11-19): a handler made by newRig with
// slices, a step worker and a checker, and no Runs of its own, gets Config.Runs from a durable
// test service that runs each run as the real Run workflow (package durable) in Temporal's
// in-process test environment, and Config.Record, the run record that workflow writes. The
// service lives as long as the store it writes to, so a restarted handler over the same store
// (a new newRig) finds the runs still running, as a restarted engine finds its workflows: its
// activities call the ports of the latest handler, and its clock is that handler's rig clock.
//
// Package durable imports package api, so these internal tests cannot import it; the service is
// made in durable_service_test.go (package api_test), which sets newDurableTestService.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/machine"
)

// DurablePorts are the ports a durable test service's activities call, and its clock: those of
// the latest handler over the store.
type DurablePorts struct {
	Worker    machine.Worker
	Checker   machine.Checker
	History   machine.History // nil: restoring and committing do nothing
	Record    *RunRecord
	Artifacts DurableArtifacts
	Now       func() time.Time
	// Context and Running are the handler's (Config.Context, Config.Running): the engine's durable
	// worker stops with the engine, so an activity's context ends with Context, no activity starts
	// once it has ended, and Running counts each activity until it has returned.
	Context context.Context
	Running *sync.WaitGroup
}

// DurableArtifacts is the artifact store the activities keep findings in (durable.Artifacts).
type DurableArtifacts interface {
	PutArtifact(slice string, data []byte) (string, error)
	Artifact(hash string) ([]byte, error)
}

// DurableTestService is Runs on Temporal's test environment.
type DurableTestService interface {
	Runs
	// Use makes the activities call ports from now on (a restarted handler).
	Use(p DurablePorts)
	// Tick lets every run whose durable timer is due by the clock's time go on, and returns once
	// each such run has ended or 10 s have passed.
	Tick()
	// Waiting tells whether the run's workflow holds a wait that follows the clock (a hand-over's
	// or an interruption's expiry), so that a test moves the clock only once the wait is set (T070).
	Waiting(run string) bool
	// Close ends every run still running without recording anything more, and waits for them.
	Close()
}

// NewDurableTestService is set by durable_service_test.go's init.
var NewDurableTestService func() DurableTestService

var durableServices = struct {
	sync.Mutex
	by map[any]DurableTestService // by the store the runs are recorded in
}{by: map[any]DurableTestService{}}

// waitForTimer returns once the run recorded in st holds a wait that follows the rig clock, so a
// clock move that should end that wait finds it set; it fails the test after 10 s.
func waitForTimer(t *testing.T, st SliceStore, run string) {
	t.Helper()
	durableServices.Lock()
	svc := durableServices.by[baseStore(st)]
	durableServices.Unlock()
	if svc == nil {
		t.Fatalf("no durable service records into this store")
	}
	for end := time.Now().Add(10 * time.Second); !svc.Waiting(run); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("%s holds no clock wait after 10 s", run)
		}
	}
}

// baseStore is the store under s: a hiddenRun's store, or s itself.
func baseStore(s SliceStore) any {
	if h, ok := s.(hiddenRun); ok {
		return h.Store
	}
	return s
}

// attachDurable gives cfg its durable service and run record when it has slices, a run store, a
// step worker and a checker, and no Runs of its own; it then moves the rig's clock for the runs.
func attachDurable(t *testing.T, r *rig, cfg *Config) {
	t.Helper()
	if cfg.Runs != nil || cfg.Slices == nil || cfg.StepWorker == nil || cfg.StepChecker == nil {
		return
	}
	st, ok := cfg.Store.(RunStore)
	if !ok {
		return
	}
	arts, ok := cfg.Store.(DurableArtifacts)
	if !ok {
		return
	}
	p, err := cfg.Slices.Pattern()
	if err != nil {
		return
	}
	if NewDurableTestService == nil {
		t.Fatal("rig: no durable test service (durable_service_test.go)")
	}
	base := cfg.Context
	if base == nil {
		base = context.Background()
	}
	rec := NewRunRecord(st, MachinePattern(p), cfg.Model)
	key := baseStore(cfg.Store)
	durableServices.Lock()
	svc, ok := durableServices.by[key]
	if !ok {
		svc = NewDurableTestService()
		durableServices.by[key] = svc
		t.Cleanup(func() {
			svc.Close()
			durableServices.Lock()
			delete(durableServices.by, key)
			durableServices.Unlock()
		})
	}
	durableServices.Unlock()
	svc.Use(DurablePorts{Worker: cfg.StepWorker, Checker: cfg.StepChecker, History: cfg.History, Record: rec,
		Artifacts: arts, Now: r.clock.Now, Context: base, Running: cfg.Running})
	cfg.Runs, cfg.Record = svc, rec
	r.clock.onAdvance(svc.Tick)
}
