package main

// T053e (owner, 2026-11-14): AP_PROJECT_URL and AP_PROJECT_TOKEN, which reach the project worker
// (restoreStep, commitStep), are required. The engine refuses to start without either, naming the
// one that is missing or empty, and AP_PROJECT_TOKEN must differ from every other token. Written
// before the code, by a separate author.

import (
	"strings"
	"sync/atomic"
	"testing"
)

const projectTok = "project-token"

// withProject is goodEnv plus the project worker's variables.
func withProject(e env) env {
	e["AP_PROJECT_URL"] = "http://project-worker:8080"
	e["AP_PROJECT_TOKEN"] = projectTok
	return e
}

func TestSetupRefusesAMissingOrEmptyProjectVariableNamingIt(t *testing.T) {
	dir := t.TempDir()
	vars := []string{"AP_PROJECT_URL", "AP_PROJECT_TOKEN"}
	for i, name := range vars {
		other := vars[1-i]
		for _, how := range []string{"missing", "empty"} {
			e := withProject(goodEnv("http://lint:8080", "http://analyst:8080", dir))
			if how == "missing" {
				delete(e, name)
			} else {
				e[name] = ""
			}
			h, closeFn, err := setup(e.get)
			if err == nil {
				if closeFn != nil {
					closeFn()
				}
				t.Errorf("%s %s: setup succeeded, want an error", name, how)
				continue
			}
			if !strings.Contains(err.Error(), name) || strings.Contains(err.Error(), other) {
				t.Errorf("%s %s: error %q should name %s alone, not %s, which is set", name, how, err, name, other)
			}
			if h != nil {
				t.Errorf("%s %s: handler returned with an error", name, how)
			}
		}
	}
}

func TestSetupRefusesToStartWithoutTheProjectWorker(t *testing.T) {
	var calls atomic.Int32
	e := goodEnv(fakeLint(t).URL, fakeAnalyst(t, &calls).URL, t.TempDir())
	delete(e, "AP_PROJECT_URL")
	delete(e, "AP_PROJECT_TOKEN")
	h, closeFn, err := setup(e.get)
	if err == nil {
		if closeFn != nil {
			closeFn()
		}
		t.Fatal("setup succeeded with neither AP_PROJECT_URL nor AP_PROJECT_TOKEN, want an error")
	}
	if !strings.Contains(err.Error(), "AP_PROJECT_URL") && !strings.Contains(err.Error(), "AP_PROJECT_TOKEN") {
		t.Errorf("error %q names neither project variable", err)
	}
	if h != nil {
		t.Error("handler returned with an error")
	}
}

func TestSetupRefusesAProjectTokenEqualToAnotherToken(t *testing.T) {
	dir := t.TempDir()
	for _, other := range []string{"ENGINE_CLIENT_TOKEN", "ENGINE_PAGE_TOKEN", "ENGINE_WORKER_TOKEN", "AP_LINT_TOKEN",
		"AP_ANALYST_TOKEN", "AP_CHECK_TOKEN"} {
		e := withProject(goodEnv("http://lint:8080", "http://analyst:8080", dir))
		e["AP_PROJECT_TOKEN"] = e[other]
		_, closeFn, err := setup(e.get)
		if err == nil {
			if closeFn != nil {
				closeFn()
			}
			t.Errorf("AP_PROJECT_TOKEN = %s: setup succeeded, want an error", other)
			continue
		}
		if !strings.Contains(err.Error(), "AP_PROJECT_TOKEN") || !strings.Contains(err.Error(), other) {
			t.Errorf("AP_PROJECT_TOKEN = %s: error %q does not name both", other, err)
		}
	}
}

func TestSetupRefusesAProjectURLThatIsNotHTTP(t *testing.T) {
	for _, u := range []string{"project-worker:8080", "ftp://project-worker", "http://"} {
		e := withProject(goodEnv("http://lint:8080", "http://analyst:8080", t.TempDir()))
		e["AP_PROJECT_URL"] = u
		_, closeFn, err := setup(e.get)
		if err == nil {
			if closeFn != nil {
				closeFn()
			}
			t.Errorf("AP_PROJECT_URL = %q: setup succeeded, want an error", u)
			continue
		}
		if !strings.Contains(err.Error(), "AP_PROJECT_URL") {
			t.Errorf("AP_PROJECT_URL = %q: error %q does not name it", u, err)
		}
	}
}

func TestSetupStartsWithTheProjectWorkerSet(t *testing.T) {
	var calls atomic.Int32
	h, closeFn, err := setupWith(withProject(goodEnv(fakeLint(t).URL, fakeAnalyst(t, &calls).URL, t.TempDir())).get, testDurable(t))
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	if h == nil {
		t.Fatal("no handler")
	}
}
