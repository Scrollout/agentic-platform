//go:build docker

// T045: conformance of the engine's run operations and registerWorker to its contract. Schemathesis
// (the pinned image) runs in a container on the lab network (src/tests/lab) against the running
// engine, once for each token kind the operation allows; Go checks prove each refused token kind
// gets 403 Forbidden (D12: the client token every developer operation, the page token only GET
// operations, a worker token only registerWorker) and the registerWorker endpoint rule. A missing
// Docker is a failure, never a skip.
package contract

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/agentic-platform/tests/lab"
)

// The run operations and registerWorker: the tokens each allows. The client token is allowed on
// every developer operation; the GET operations also take the page token (the contract's
// `security`); registerWorker takes only the worker token.
var runOperations = []struct {
	id     string
	method string
	path   string // a path that matches the operation, with ids the engine does not know
	body   any
	tokens []string // allowed
}{
	{"startRun", "POST", "/slices/S-1/runs", map[string]any{"project": "notes"}, []string{lab.ClientToken}},
	{"resumeRun", "POST", "/runs/RUN-1/resume", nil, []string{lab.ClientToken}},
	{"replayRun", "POST", "/runs/RUN-1/replay", map[string]any{"project": "notes-2"}, []string{lab.ClientToken}},
	{"acceptHandOver", "POST", "/runs/RUN-1/hand-over", nil, []string{lab.ClientToken}},
	{"compareRuns", "GET", "/runs/RUN-1/comparison?with=RUN-2", nil, []string{lab.ClientToken, lab.PageToken}},
	{"listRuns", "GET", "/runs", nil, []string{lab.ClientToken, lab.PageToken}},
	{"getRun", "GET", "/runs/RUN-1", nil, []string{lab.ClientToken, lab.PageToken}},
	{"listRunEvents", "GET", "/runs/RUN-1/events", nil, []string{lab.ClientToken, lab.PageToken}},
	{"getArtifact", "GET", "/runs/RUN-1/artifacts/" + strings.Repeat("a", 64), nil, []string{lab.ClientToken, lab.PageToken}},
	{"registerWorker", "POST", "/workers", map[string]any{"endpoint": "http://svc:8080"}, []string{lab.WorkerToken}},
}

func tokenName(tok string) string {
	switch tok {
	case lab.ClientToken:
		return "client"
	case lab.PageToken:
		return "page"
	default:
		return "worker"
	}
}

func TestSchemathesisAgainstTheRunOperations(t *testing.T) {
	l := lab.Start(t)
	opts := []string{"--checks", "all", "--max-examples", "30", "--generation-deterministic",
		"--rate-limit", "15/s", "--no-color", "--coverage-no-report"}
	for _, op := range runOperations {
		for _, tok := range op.tokens {
			who := tokenName(tok)
			t.Run(op.id+"/"+who, func(t *testing.T) {
				// As in slices_test.go: the contract declares no 405; the operations with a
				// `page_token` refuse one the engine did not issue with 400 InvalidRequest though any
				// string fits the parameter's schema.
				exclude := "unsupported_method"
				if op.id == "listRuns" || op.id == "listRunEvents" || op.id == "compareRuns" {
					exclude += ",positive_data_acceptance"
				}
				out, code := l.Schemathesis(t, tok, []string{op.id}, append(opts, "--exclude-checks", exclude)...)
				if code != 0 {
					t.Errorf("schemathesis %s with the %s token exited %d:\n%s", op.id, who, code, out)
				}
				if !strings.Contains(out, "test cases") && !strings.Contains(out, "Test cases") {
					t.Errorf("schemathesis %s ran no test cases:\n%s", op.id, out)
				}
			})
		}
	}
	if logs := l.EngineLogs(); strings.Contains(logs, "panic") {
		t.Errorf("the engine panicked:\n%s", logs)
	}
}

// Every token kind an operation does not allow gets 403 Forbidden with the problem code Forbidden,
// before the request is looked at further (so an unknown run or slice is still 403, not 404).
func TestRunOperationsRefuseTheOtherTokenKinds(t *testing.T) {
	l := lab.Start(t)
	all := []string{lab.ClientToken, lab.PageToken, lab.WorkerToken}
	for _, op := range runOperations {
		allowed := map[string]bool{}
		for _, tok := range op.tokens {
			allowed[tok] = true
		}
		for _, tok := range all {
			if allowed[tok] {
				continue
			}
			t.Run(op.id+"/"+tokenName(tok), func(t *testing.T) {
				st, ct, b := l.Do(op.method, op.path, tok, op.body, nil)
				if st != 403 {
					t.Fatalf("%s %s with the %s token = %d, want 403: %s", op.method, op.path, tokenName(tok), st, b)
				}
				if !strings.HasPrefix(ct, "application/problem+json") || !strings.Contains(string(b), `"Forbidden"`) {
					t.Errorf("%s: want a Forbidden problem (%s), got %s", op.id, ct, b)
				}
			})
		}
	}
}

// registerWorker's endpoint rule: `http://<service>:<port>` with an optional trailing `/` is
// accepted (the registration is looked up, so 200 or the contract's 404 for a slice or step the
// worker token was not issued for, never 400); anything after the port other than that `/` is
// refused with 400 InvalidRequest.
func TestRegisterWorkerEndpointRule(t *testing.T) {
	l := lab.Start(t)
	for _, ep := range []string{"http://svc:8080", "http://svc:8080/"} {
		st, _, b := l.Do("POST", "/workers", lab.WorkerToken, map[string]any{"endpoint": ep}, nil)
		if st != 200 && st != 404 {
			t.Errorf("registerWorker %q = %d, want 200 (or 404: no such slice or step): %s", ep, st, b)
		}
		if st == 200 && !strings.Contains(string(b), `"endpoint"`) {
			t.Errorf("registerWorker %q returned no registration: %s", ep, b)
		}
	}
	for _, ep := range []string{"http://svc:8080/path", "http://svc:8080/a/", "http://svc:8080//", "http://svc:8080/?x=1",
		"http://svc", "https://svc:8080", "http://SVC:8080", "http://svc:8080 ", ""} {
		st, _, b := l.Do("POST", "/workers", lab.WorkerToken, map[string]any{"endpoint": ep}, nil)
		var p lab.Problem
		_ = jsonUnmarshal(b, &p)
		if st != 400 || p.Code != "InvalidRequest" {
			t.Errorf("registerWorker %q = %d %s, want 400 InvalidRequest", ep, st, b)
		}
	}
	// A body with another field, or none, is refused too.
	for _, body := range []any{map[string]any{"endpoint": "http://svc:8080", "slice": "S-1"}, map[string]any{}, "{"} {
		st, _, b := l.Do("POST", "/workers", lab.WorkerToken, body, nil)
		if st != 400 {
			t.Errorf("registerWorker %v = %d, want 400: %s", body, st, b)
		}
	}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
