package api

// T053, engine side: registerWorker and worker readiness for startRun, judged from the contract
// and data-model only. Interface assumed (see runs_refusals_test.go): WorkerGrant, Config.WorkerGrants
// (a token in it is a worker token for its slice and step), Config.Projects.
//
// Ambiguities the contract leaves open, and what these tests assume:
//   - a granted token whose slice does not exist: the description says 404 "when that slice no
//     longer exists", so NotFound.
//   - the session worker token (Tokens.Worker) with no grant: "as today" it is 404 NotFound (no
//     slice and step were issued for it); the contract does not say so explicitly.
//   - "ready worker" (startRun) is taken to mean "registered".

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The steps api-feature-go declares, in order.
var declaredSteps = []string{"contract-check", "generate", "tests", "code", "review"}

func stepToken(slice, step string) string { return "worker-token-" + slice + "-" + step }

// stepGrants grants one worker token per declared step of the slice.
func stepGrants(slice string) map[string]WorkerGrant {
	g := map[string]WorkerGrant{}
	for _, st := range declaredSteps {
		g[stepToken(slice, st)] = WorkerGrant{Slice: slice, Step: st}
	}
	return g
}

func mergeGrants(ms ...map[string]WorkerGrant) map[string]WorkerGrant {
	out := map[string]WorkerGrant{}
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func endpointFor(step string) string { return "http://" + step + ":8080" }

func register(r *rig, token, endpoint string) reply {
	return r.do("POST", "/workers?api-version="+apiVersion, token, `{"endpoint":"`+endpoint+`"}`, nil)
}

// registerAll registers a worker for every declared step of the slice, each must be 200.
func registerAll(t *testing.T, r *rig, slice string) {
	t.Helper()
	for _, st := range declaredSteps {
		if w := register(r, stepToken(slice, st), endpointFor(st)); w.Code != 200 {
			t.Fatalf("register %s: status %d: %s", st, w.Code, w.Body.String())
		}
	}
}

func workersRig(t *testing.T, grants map[string]WorkerGrant) (*rig, *spRig) {
	t.Helper()
	sp := newNotes(t)
	sp.approveAfterSeed(t)
	r := newRig(t, func(c *Config) {
		c.Slices, c.Store = sp.svc, sp.store
		c.WorkerGrants = grants
	})
	return r, sp
}

type workerBody struct {
	Slice    string `json:"slice"`
	Step     string `json:"step"`
	Endpoint string `json:"endpoint"`
}

func decodeWorker(t *testing.T, w reply) workerBody {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"slice", "step", "endpoint"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("Worker lacks required field %q: %s", k, w.Body.String())
		}
	}
	if len(raw) != 3 {
		t.Errorf("Worker has fields beyond slice, step, endpoint: %s", w.Body.String())
	}
	var b workerBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRegisterWorkerReturnsTheWorkerAsTheEngineHoldsIt(t *testing.T) {
	r, _ := workersRig(t, stepGrants("S-1"))
	w := register(r, stepToken("S-1", "generate"), "http://svc:8080")
	if ct := w.Header().Get("Content-Type"); len(ct) < 16 || ct[:16] != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	got := decodeWorker(t, w)
	if want := (workerBody{"S-1", "generate", "http://svc:8080"}); got != want {
		t.Errorf("Worker = %+v, want %+v", got, want)
	}
}

func TestRegisteringAgainReplacesTheEndpoint(t *testing.T) {
	r, _ := workersRig(t, stepGrants("S-1"))
	tok := stepToken("S-1", "tests")
	decodeWorker(t, register(r, tok, "http://old:8080"))
	got := decodeWorker(t, register(r, tok, "http://new:9090"))
	if got.Endpoint != "http://new:9090" || got.Step != "tests" || got.Slice != "S-1" {
		t.Errorf("Worker = %+v, want the new endpoint for tests of S-1", got)
	}
}

func TestEndpointShapeIsChecked(t *testing.T) {
	r, _ := workersRig(t, stepGrants("S-1"))
	tok := stepToken("S-1", "code")
	for _, ep := range []string{"http://svc:8080", "http://svc:8080/"} {
		if w := register(r, tok, ep); w.Code != 200 {
			t.Errorf("%q: status %d, want 200: %s", ep, w.Code, w.Body.String())
		}
	}
	for _, ep := range []string{
		"http://svc:8080/path", "http://svc:8080//", "https://svc:8080", "ftp://svc:8080",
		"http://svc", "http://svc/", "svc:8080", "http://Svc:8080", "http://svc_x:8080", "http://:8080", "",
	} {
		register(r, tok, ep).problem(t, 400, "InvalidRequest")
	}
	// An unknown field and a missing endpoint do not match WorkerRegistration either.
	r.do("POST", "/workers?api-version="+apiVersion, tok, `{"endpoint":"http://svc:8080","x":1}`, nil).problem(t, 400, "InvalidRequest")
	r.do("POST", "/workers?api-version="+apiVersion, tok, `{}`, nil).problem(t, 400, "InvalidRequest")
}

func TestARefusedRegistrationLeavesTheEarlierEndpoint(t *testing.T) {
	r, _ := workersRig(t, stepGrants("S-1"))
	tok := stepToken("S-1", "code")
	decodeWorker(t, register(r, tok, "http://svc:8080"))
	register(r, tok, "http://svc:8080/path").problem(t, 400, "InvalidRequest")
	got := decodeWorker(t, register(r, tok, "http://svc:8080/"))
	if got.Endpoint != "http://svc:8080/" {
		t.Errorf("Worker = %+v", got)
	}
}

func TestRegisterWorkerForASliceThatDoesNotExistIsNotFound(t *testing.T) {
	const tok = "worker-token-no-such-slice"
	r, _ := workersRig(t, map[string]WorkerGrant{tok: {Slice: "S-9", Step: "generate"}})
	register(r, tok, "http://svc:8080").problem(t, 404, "NotFound")
}

func TestRegisterWorkerTokenWithoutAGrantIsNotFound(t *testing.T) {
	r, _ := workersRig(t, stepGrants("S-1"))
	register(r, workerToken, "http://svc:8080").problem(t, 404, "NotFound")
}

func TestRegisterWorkerWithAClientOrPageTokenIs403(t *testing.T) {
	r, _ := workersRig(t, stepGrants("S-1"))
	for _, tok := range []string{clientToken, pageToken} {
		register(r, tok, "http://svc:8080").problem(t, 403, "Forbidden")
	}
}

func TestAWorkerTokenMayNotCallOtherOperations(t *testing.T) {
	r, _ := workersRig(t, stepGrants("S-1"))
	r.do("GET", spURL("S-1", ""), stepToken("S-1", "generate"), "", nil).problem(t, 403, "Forbidden")
}

func TestStartRunIsWorkersNotReadyWhileAnyDeclaredStepHasNoWorker(t *testing.T) {
	for _, missing := range declaredSteps {
		t.Run(missing, func(t *testing.T) {
			r, sp := workersRig(t, stepGrants("S-1"))
			for _, st := range declaredSteps {
				if st != missing {
					register(r, stepToken("S-1", st), endpointFor(st))
				}
			}
			r.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).problem(t, 409, "WorkersNotReady")
			sp.noOperation(t)
			if n := r.rec.count(); n != 0 {
				t.Errorf("Work ran %d times for a refused run", n)
			}
		})
	}
}

func TestWorkersRegisteredForAnotherSliceDoNotCount(t *testing.T) {
	sp := newSP(t, []spResult{{p: spNotes(t)}, {p: spNotes(t)}}, nil)
	sp.approveAfterSeed(t)                // S-1
	sp.seed(t, "Let me keep more notes.") // S-2
	r := newRig(t, func(c *Config) {
		c.Slices, c.Store = sp.svc, sp.store
		c.WorkerGrants = mergeGrants(stepGrants("S-1"), stepGrants("S-2"))
	})
	registerAll(t, r, "S-2")
	r.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).problem(t, 409, "WorkersNotReady")
}

func TestStartRunWithEveryStepRegisteredIsNotWorkersNotReady(t *testing.T) {
	sp := newNotes(t)
	sp.approveAfterSeed(t)
	r := newRig(t, func(c *Config) {
		c.Slices, c.Store, c.Projects = sp.svc, sp.store, t.TempDir()
		c.WorkerGrants = stepGrants("S-1")
	})
	registerAll(t, r, "S-1")
	w := r.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil)
	var p struct{ Code string }
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p.Code == "WorkersNotReady" {
		t.Fatalf("still WorkersNotReady with every step registered: %s", w.Body.String())
	}
}

func TestAnEarlierRefusalStillWinsWithWorkersRegistered(t *testing.T) {
	sp := newNotes(t)
	sp.seed(t, "Let me keep notes.") // fits the pattern, nothing approved
	r := newRig(t, func(c *Config) {
		c.Slices, c.Store = sp.svc, sp.store
		c.WorkerGrants = stepGrants("S-1")
	})
	registerAll(t, r, "S-1")
	r.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).problem(t, 409, "NotApproved")
	sp.noOperation(t)
}

func TestProjectExistsCreatesNoOperationAndWritesNothing(t *testing.T) {
	projects := t.TempDir()
	if err := os.Mkdir(filepath.Join(projects, "notes-app"), 0o755); err != nil {
		t.Fatal(err)
	}
	sp := newNotes(t)
	sp.approveAfterSeed(t)
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
	es, err := os.ReadDir(projects)
	if err != nil || len(es) != 1 {
		t.Errorf("projects folder = %v, %v; want only the existing notes-app", es, err)
	}
}
