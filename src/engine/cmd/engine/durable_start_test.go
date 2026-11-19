package main

// T013, the engine's start over the durable service: the namespace `ap` and runs left open by
// feature 001 (ADR 0070, research R7, spec Edge Cases). Written before the code (T018) by a test
// author, with the durable service faked.
//
// Interface assumed (package main; the adapter in src/engine/durable implements both ports):
//
//	// durableLinks is what the engine needs from the durable service, given at start.
//	type durableLinks struct {
//		Runs       api.Runs          // api's port, see api/runs_durable_test.go
//		Namespaces namespaceRegistry
//	}
//	type namespaceRegistry interface {
//		// Describe says whether the namespace exists and, if so, its retention.
//		Describe(ctx context.Context, name string) (retention time.Duration, found bool, err error)
//		// Register creates the namespace with the retention.
//		Register(ctx context.Context, name string, retention time.Duration) error
//	}
//	// setupWith is setup with the durable service given; setup(getenv) calls it with the real
//	// one. It does what setup does, and before the engine serves anything it
//	//  - registers namespace "ap" with the retention api's contract gives
//	//    (x-limits.idempotency-key-retention-hours, as hours) when Describe finds none, and
//	//    leaves an existing one unchanged (no Register call);
//	//  - lists the store's runs whose record has no end event (RunCompleted, HandOverAccepted,
//	//    RunExpired, RunCancelled), asks Runs.Exists for each, and when any has no workflow
//	//    returns an error naming every such run and the fix (end it with feature 001's
//	//    version, then upgrade), with a nil handler and close function.
//	func setupWith(getenv func(string) string, d durableLinks) (http.Handler, func() error, error)

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/agentic-platform/engine/api"
	"github.com/agentic-platform/engine/store"
	"github.com/agentic-platform/slice"
)

type fakeNamespaces struct {
	mu        sync.Mutex
	existing  map[string]time.Duration
	registers []registered
}
type registered struct {
	Name      string
	Retention time.Duration
}

func (f *fakeNamespaces) Describe(_ context.Context, name string) (time.Duration, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.existing[name]
	return d, ok, nil
}
func (f *fakeNamespaces) Register(_ context.Context, name string, r time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registers = append(f.registers, registered{name, r})
	f.existing[name] = r
	return nil
}

type fakeWorkflows struct{ has map[string]bool }

func (f fakeWorkflows) Start(context.Context, api.RunStart) error        { return nil }
func (f fakeWorkflows) Signal(context.Context, string, string) error     { return nil }
func (f fakeWorkflows) Cancel(context.Context, string) error             { return nil }
func (f fakeWorkflows) Delete(context.Context, string) error             { return nil }
func (f fakeWorkflows) Exists(_ context.Context, r string) (bool, error) { return f.has[r], nil }

// contractRetentionHours reads the limit from the contract file itself.
func contractRetentionHours(t *testing.T) time.Duration {
	t.Helper()
	b, err := os.ReadFile("../../api/preview/2026-11-12-preview/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Limits map[string]int `yaml:"x-limits"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	h := doc.Limits["idempotency-key-retention-hours"]
	if h <= 0 {
		t.Fatalf("the contract's retention is %d hours", h)
	}
	return time.Duration(h) * time.Hour
}

// startFixture is a good environment over a store dir; runs holds the kinds of each run's events.
func startFixture(t *testing.T, runs map[string][]string) (env, string) {
	t.Helper()
	dir := t.TempDir()
	if len(runs) > 0 {
		ids, err := slice.LoadSchemasFS(api.ContractFiles())
		if err != nil {
			t.Fatal(err)
		}
		st, err := store.Open(dir, ids)
		if err != nil {
			t.Fatal(err)
		}
		for run, kinds := range runs { // feature 001's record: seq, run and kind are what the check reads
			for _, k := range kinds {
				if _, err := st.Append("S-1", store.Event{Run: run, Kind: k, Data: store.RawJSON(`{"event":{"run":"` + run + `","kind":"` + k + `"}}`)}); err != nil {
					t.Fatal(err)
				}
			}
		}
		st.Close()
	}
	var calls atomic.Int32
	lint, analyst := fakeLint(t), fakeAnalyst(t, &calls)
	return goodEnv(lint.URL, analyst.URL, dir), dir
}

func TestStartRegistersNamespaceApWithTheContractsRetentionWhenMissing(t *testing.T) {
	e, _ := startFixture(t, nil)
	ns := &fakeNamespaces{existing: map[string]time.Duration{}}
	_, closeFn, err := setupWith(e.get, durableLinks{Runs: fakeWorkflows{}, Namespaces: ns})
	if err != nil {
		t.Fatalf("setupWith: %v", err)
	}
	defer closeFn()
	want := []registered{{"ap", contractRetentionHours(t)}}
	if len(ns.registers) != 1 || ns.registers[0] != want[0] {
		t.Errorf("registered %v, want %v", ns.registers, want)
	}
}

func TestStartKeepsAnExistingNamespaceApUnchanged(t *testing.T) {
	e, _ := startFixture(t, nil)
	other := 72 * time.Hour // not the contract's value: the engine must not touch it
	ns := &fakeNamespaces{existing: map[string]time.Duration{"ap": other}}
	_, closeFn, err := setupWith(e.get, durableLinks{Runs: fakeWorkflows{}, Namespaces: ns})
	if err != nil {
		t.Fatalf("setupWith: %v", err)
	}
	defer closeFn()
	if len(ns.registers) != 0 || ns.existing["ap"] != other {
		t.Errorf("registered %v, retention now %v; want no registration and %v kept", ns.registers, ns.existing["ap"], other)
	}
}

func TestStartRefusesWhenARunsRecordHasNoEndAndNoWorkflowExistsNamingEachRun(t *testing.T) {
	e, _ := startFixture(t, map[string][]string{
		"RUN-1": {"RunStarted", "RunCompleted"},                // ended: nothing to do
		"RUN-2": {"RunStarted", "StepStarted"},                 // open, has a workflow: fine
		"RUN-3": {"RunStarted", "RunInterrupted"},              // open, no workflow
		"RUN-4": {"RunStarted", "RunHandedOver"},               // open, no workflow
		"RUN-5": {"RunStarted", "RunHandedOver", "RunExpired"}, // ended
	})
	ns := &fakeNamespaces{existing: map[string]time.Duration{}}
	h, closeFn, err := setupWith(e.get, durableLinks{Runs: fakeWorkflows{has: map[string]bool{"RUN-2": true}}, Namespaces: ns})
	if err == nil {
		closeFn()
		t.Fatal("setupWith succeeded, want a refusal")
	}
	if h != nil || closeFn != nil {
		t.Errorf("a refused start returned a handler or a close function")
	}
	for _, run := range []string{"RUN-3", "RUN-4"} {
		if !strings.Contains(err.Error(), run) {
			t.Errorf("error %q does not name %s", err, run)
		}
	}
	for _, run := range []string{"RUN-1", "RUN-2", "RUN-5"} {
		if strings.Contains(err.Error(), run) {
			t.Errorf("error %q names %s, which needs nothing", err, run)
		}
	}
	if !strings.Contains(err.Error(), "feature 001") {
		t.Errorf("error %q does not give the fix: end the run with feature 001's version", err)
	}
}

func TestStartAcceptsOpenRunsThatHaveAWorkflowAndEndedRunsWithout(t *testing.T) {
	e, _ := startFixture(t, map[string][]string{
		"RUN-1": {"RunStarted", "HandOverAccepted"},
		"RUN-2": {"RunStarted", "RunCancelled"},
		"RUN-3": {"RunStarted", "RunInterrupted"},
	})
	ns := &fakeNamespaces{existing: map[string]time.Duration{}}
	_, closeFn, err := setupWith(e.get, durableLinks{Runs: fakeWorkflows{has: map[string]bool{"RUN-3": true}}, Namespaces: ns})
	if err != nil {
		t.Fatalf("setupWith: %v", err)
	}
	closeFn()
}
