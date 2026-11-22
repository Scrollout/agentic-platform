package main

// T006, the engine's start and the method identity (feature 006, contract src/engine/method/README.md,
// "The Go side"). Written before the code (T007) by a test author.
//
// Seams assumed in setup.go (package main):
//
//	var methodIdentity func() (method.ID, error) = method.Current // read once per setupWith
//	var newSlices func(slices.Config) (*slices.Service, error) = slices.New // setupWith builds its slice service through it
//
// and the fields durable.Activities.Method (a method.ID) and slices.Config.Method (a string, the identity's hex).

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/method"
	"github.com/agentic-platform/engine/slices"
)

const (
	identityA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	identityB = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// withIdentity replaces the engine's method identity for the test; restored at its end.
func withIdentity(t *testing.T, f func() (method.ID, error)) {
	t.Helper()
	old := methodIdentity
	methodIdentity = f
	t.Cleanup(func() { methodIdentity = old })
}

func fixedIdentity(t *testing.T, s string) func() (method.ID, error) {
	t.Helper()
	id, err := method.Parse(s)
	if err != nil {
		t.Fatalf("method.Parse(%q): %v", s, err)
	}
	return func() (method.ID, error) { return id, nil }
}

// recordSlices wraps the slice service's constructor, recording each Config it is given.
func recordSlices(t *testing.T) *[]slices.Config {
	t.Helper()
	var got []slices.Config
	old := newSlices
	newSlices = func(c slices.Config) (*slices.Service, error) {
		got = append(got, c)
		return old(c)
	}
	t.Cleanup(func() { newSlices = old })
	return &got
}

// startOnce starts the engine on dir with a testDurable and returns the served activities.
func startOnce(t *testing.T, e env, d durableLinks) (*durable.Activities, func() error, error) {
	t.Helper()
	var served *durable.Activities
	serve := d.Serve
	d.Serve = func(a *durable.Activities) (func(), error) {
		served = a
		return serve(a)
	}
	h, closeFn, err := setupWith(e.get, d)
	if err != nil {
		if h != nil || closeFn != nil {
			t.Errorf("a refused start returned a handler or a close function")
		}
		return nil, nil, err
	}
	return served, closeFn, nil
}

func methodEnv(t *testing.T) env {
	t.Helper()
	return goodEnv(fakeLint(t).URL, fakeAnalyst(t, new(atomic.Int32)).URL, t.TempDir())
}

func TestStartFailsNamingTheIdentityFileAndScriptWhenTheIdentityDoesNotParse(t *testing.T) {
	e := methodEnv(t)
	_, parseErr := method.Parse("not hex")
	if parseErr == nil {
		t.Fatal("method.Parse accepted \"not hex\"")
	}
	withIdentity(t, func() (method.ID, error) { return method.Parse("not hex") })
	_, _, err := startOnce(t, e, testDurable(t))
	if err == nil {
		t.Fatal("setupWith started with an identity that does not parse")
	}
	for _, want := range []string{"src/engine/method/method.sha256", "scripts/method-digest.py"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}

	// Nothing is left open: the same store directory starts once the identity is good.
	withIdentity(t, fixedIdentity(t, identityA))
	_, closeFn, err := startOnce(t, e, testDurable(t))
	if err != nil {
		t.Fatalf("second start on the same store directory: %v", err)
	}
	if err := closeFn(); err != nil {
		t.Error(err)
	}
}

func TestStartGivesTheDurableActivitiesAndTheSliceServiceTheIdentity(t *testing.T) {
	e := methodEnv(t)
	withIdentity(t, fixedIdentity(t, identityA))
	cfgs := recordSlices(t)
	acts, closeFn, err := startOnce(t, e, testDurable(t))
	if err != nil {
		t.Fatalf("setupWith: %v", err)
	}
	defer func() {
		if err := closeFn(); err != nil {
			t.Error(err)
		}
	}()
	if acts == nil {
		t.Fatal("no activities were served")
	}
	if got := acts.Method.String(); got != identityA {
		t.Errorf("activities' Method = %q, want %q", got, identityA)
	}
	if len(*cfgs) != 1 {
		t.Fatalf("the slice service was built %d times, want 1", len(*cfgs))
	}
	if got := (*cfgs)[0].Method; got != identityA {
		t.Errorf("slices.Config.Method = %q, want %q", got, identityA)
	}
}

func TestStartReadsTheIdentityAtEachStartAndKeepsNoneBetweenStarts(t *testing.T) {
	e := methodEnv(t)
	cfgs := recordSlices(t)
	for _, want := range []string{identityA, identityB} {
		withIdentity(t, fixedIdentity(t, want))
		acts, closeFn, err := startOnce(t, e, testDurable(t))
		if err != nil {
			t.Fatalf("setupWith: %v", err)
		}
		if got := acts.Method.String(); got != want {
			t.Errorf("activities' Method = %q, want %q", got, want)
		}
		if err := closeFn(); err != nil {
			t.Error(err)
		}
	}
	if len(*cfgs) != 2 {
		t.Fatalf("the slice service was built %d times, want 2", len(*cfgs))
	}
	for i, want := range []string{identityA, identityB} {
		if got := (*cfgs)[i].Method; got != want {
			t.Errorf("start %d: slices.Config.Method = %q, want %q", i+1, got, want)
		}
	}
}
