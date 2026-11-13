package api

// T067 (FR-008, FR-025 and the edge case "points the platform at an existing project"): two
// refusals, written before the code. The tests are the specification (ADR 0029).
//
// registerWorker for a step the pattern does not declare is 404 NotFound: the contract's
// description says "It returns 404 when that slice no longer exists or the slice's pattern has no
// such step", and its x-error-codes list only NotFound for 404.
//
// startRun naming a folder that already exists under the projects folder is 409 ProjectExists
// (listed for startRun), after every other refusal, before any step runs: no operation is created.
//
// Interface assumed (new, smallest needed; the contract does not say how a worker token is bound
// to its slice and step, nor where the projects folder is):
//
//	type WorkerGrant struct{ Slice, Step string }
//	Config.WorkerGrants map[string]WorkerGrant // worker token -> the slice and step it was issued for
//	Config.Projects     string                 // the projects folder; a project is a folder in it
//
// With WorkerGrants set, a token in it replaces Tokens.Worker as a worker token.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterWorkerForAStepThePatternDoesNotDeclareIsNotFound(t *testing.T) {
	const tok = "worker-token-undeclared-step"
	sp := newNotes(t)
	sp.approveAfterSeed(t)
	r := newRig(t, func(c *Config) {
		c.Slices, c.Store = sp.svc, sp.store
		c.WorkerGrants = map[string]WorkerGrant{tok: {Slice: "S-1", Step: "deploy"}} // not a step of api-feature-go
	})
	r.do("POST", "/workers?api-version="+apiVersion, tok, `{"endpoint":"http://worker:8080"}`, nil).problem(t, 404, "NotFound")
}

func TestRegisterWorkerForADeclaredStepIsNotRefusedAsUnknown(t *testing.T) {
	const tok = "worker-token-declared-step"
	sp := newNotes(t)
	sp.approveAfterSeed(t)
	r := newRig(t, func(c *Config) {
		c.Slices, c.Store = sp.svc, sp.store
		c.WorkerGrants = map[string]WorkerGrant{tok: {Slice: "S-1", Step: "generate"}}
	})
	w := r.do("POST", "/workers?api-version="+apiVersion, tok, `{"endpoint":"http://worker:8080"}`, nil)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestStartRunRefusesAProjectFolderThatAlreadyExistsBeforeAnyStepRuns(t *testing.T) {
	projects := t.TempDir()
	if err := os.Mkdir(filepath.Join(projects, "notes-app"), 0o755); err != nil {
		t.Fatal(err)
	}
	sp := newNotes(t)
	sp.approveAfterSeed(t)
	// WorkersNotReady comes before ProjectExists, so every declared step has a worker first.
	r := newRig(t, func(c *Config) {
		c.Slices, c.Store, c.Projects = sp.svc, sp.store, projects
		c.WorkerGrants = stepGrants("S-1")
	})
	registerAll(t, r, "S-1")
	r.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).problem(t, 409, "ProjectExists")
	r.do("GET", "/operations/OP-1?api-version="+apiVersion, clientToken, "", nil).problem(t, 404, "NotFound")
	if n := r.rec.count(); n != 0 {
		t.Errorf("Work ran %d times for a refused run", n)
	}
	if es, _ := os.ReadDir(filepath.Join(projects, "notes-app")); len(es) != 0 {
		t.Errorf("the existing folder was written to: %v", es)
	}
}
