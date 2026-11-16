package main

// T101: setup's close function cancels the operations' context, waits until every operation's
// work has ended, and only then closes the store, so no run writes into ENGINE_STORE_DIR after
// close returned. The scenario is the one of
// TestTheEngineCallsTheProjectWorkerWithItsTokenAndTheContractsRestoreRequest: a run whose step
// workers keep failing is still retrying when the engine is closed. Written before the code by a
// separate author; it reuses the helpers of project_wiring_test.go and restart_test.go.

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// closeWaitsFileState is what the test compares for each entry of the store folder.
type closeWaitsFileState struct {
	dir  bool
	size int64
	mod  time.Time
}

// closeWaitsSnapshot lists every entry under dir with its kind, size and modification time.
func closeWaitsSnapshot(t *testing.T, dir string) map[string]closeWaitsFileState {
	t.Helper()
	out := map[string]closeWaitsFileState{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		out[p] = closeWaitsFileState{d.IsDir(), fi.Size(), fi.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatalf("listing the store folder: %v", err)
	}
	return out
}

// closeWithARunRetrying starts an engine whose step workers all fail, starts a run, waits until
// the run is working (the project worker was called) and closes the engine. It returns the store
// folder.
func closeWithARunRetrying(t *testing.T) string {
	t.Helper()
	lint, analyst, _, _, dir := rsSetup(t, true)
	pw, calls := fakeProjectWorker(t)
	stepWorker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	t.Cleanup(stepWorker.Close)
	grants := t.TempDir()

	e := goodEnv(lint.URL, analyst.URL, dir)
	e["AP_PROJECT_URL"], e["AP_PROJECT_TOKEN"] = pw.URL, "the-project-token"
	e["ENGINE_GRANTS_DIR"] = grants
	h, closeFn, err := setup(e.get)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeFn() })
	eng := &rsEngine{t: t, h: h, closeFn: closeFn}

	id := eng.create()
	eng.answer(id)
	for _, part := range []string{"goals", "rules", "contract"} {
		eng.approve(id, part)
	}
	for _, step := range []string{"contract-check", "generate", "tests", "code", "review"} {
		tok := "worker-" + step
		writeGrant(t, grants, tok, id, step, "call-"+step)
		req := httptest.NewRequest("POST", rsURL("/workers"), strings.NewReader(`{"endpoint":"`+strings.Replace(stepWorker.URL, "127.0.0.1", "localhost", 1)+`"}`))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code/100 != 2 {
			t.Fatalf("registerWorker %s = %d: %s", step, w.Code, w.Body)
		}
	}
	if w := rsDo(t, h, "POST", rsURL("/slices/"+id+"/runs"), `{"project":"notes-app"}`); w.Code/100 != 2 {
		t.Fatalf("startRun = %d: %s", w.Code, w.Body)
	}
	deadline := time.Now().Add(15 * time.Second)
	for len(calls()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no call reached the project worker: the run never started working")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The run is working and its step workers keep failing: it is retrying.
	if err := closeFn(); err != nil {
		t.Fatalf("closing the engine: %v", err)
	}
	return dir
}

func TestCloseReturnsOnlyWhenNoRunWritesTheStore(t *testing.T) {
	for i := 1; i <= 20; i++ {
		// Two seconds of watching for the first round, a shorter watch for the other nineteen so
		// the whole test stays short; every round also removes the folder.
		quiet := 300 * time.Millisecond
		if i == 1 {
			quiet = 2 * time.Second
		}
		t.Run(fmt.Sprintf("round-%02d", i), func(t *testing.T) {
			dir := closeWithARunRetrying(t)
			before := closeWaitsSnapshot(t, dir)
			time.Sleep(quiet)
			after := closeWaitsSnapshot(t, dir)
			if !reflect.DeepEqual(before, after) {
				for p, b := range before {
					if a, ok := after[p]; !ok || a != b {
						t.Errorf("after close returned, %s changed or was removed", p)
					}
				}
				for p := range after {
					if _, ok := before[p]; !ok {
						t.Errorf("after close returned, %s was created", p)
					}
				}
				t.Fatalf("the store folder changed in the %v after close returned: a run still wrote to it", quiet)
			}
			if err := os.RemoveAll(dir); err != nil {
				t.Fatalf("removing the store folder after close: %v", err)
			}
			time.Sleep(100 * time.Millisecond)
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("the store folder exists again after it was removed (stat error %v): a run wrote into it", err)
			}
		})
	}
}
