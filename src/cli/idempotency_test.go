// T055: one Idempotency-Key per invocation of build, resume, replay and take-over, resent unchanged
// with the same body after a lost answer, at most 4 tries (contract.md, "Idempotency and the run ID").
// The waits are the contract's own (1, 2 and 4 s, up to half again of jitter); the slow tests run in
// parallel. Seams: runs_fake_test.go.
package cli_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

var keyShape = regexp.MustCompile(`^[0-9a-f]{32}$`)

// mutating is one run command, its mutating call and what serves the rest of it.
type mutating struct {
	name, opName, route string
	args                []string
	body                string // the request body, "" for none
	serve               func(t *testing.T, e *eng)
	accepted            map[string]any
}

func mutatingCommands() []mutating {
	doneRun := func(run, op string) func(t *testing.T, e *eng) {
		return func(t *testing.T, e *eng) {
			stateRun(t, run, op, endStates(run, op)[0], 0).register(e)
		}
	}
	return []mutating{
		{name: "build", opName: "startRun", route: "POST /slices/S-1/runs", args: []string{"build", "S-1", "notes"},
			body: `{"project":"notes"}`, serve: doneRun("RUN-7", "OP-12"),
			accepted: operation("OP-12", "run", "accepted", "RUN-7", nil)},
		{name: "resume", opName: "resumeRun", route: "POST /runs/RUN-7/resume", args: []string{"resume", "RUN-7"},
			serve: doneRun("RUN-7", "OP-12"), accepted: operation("OP-12", "run", "running", "RUN-7", nil)},
		{name: "replay", opName: "replayRun", route: "POST /runs/RUN-3/replay", args: []string{"replay", "RUN-3", "notes-2"},
			body: `{"project":"notes-2"}`, serve: doneRun("RUN-8", "OP-13"),
			accepted: operation("OP-13", "run", "accepted", "RUN-8", nil)},
		{name: "take-over", opName: "acceptHandOver", route: "POST /runs/RUN-7/hand-over", args: []string{"take-over", "RUN-7"},
			serve: func(t *testing.T, e *eng) {
				e.reply("GET /operations/OP-12", 200, operation("OP-12", "run", "succeeded", "RUN-7", map[string]any{"run": "RUN-7"}))
				e.reply("GET /runs/RUN-7", 200, runJSON(runSpec{id: "RUN-7", status: "ended", outcome: "handed-over", reason: "out of attempts"}))
			},
			accepted: operation("OP-12", "run", "running", "RUN-7", nil)},
	}
}

func splitRoute(route string) (string, string) {
	m, p, _ := strings.Cut(route, " ")
	return m, p
}

// lossy answers the route's first len(losses) tries with them ("drop", or a status), then 202.
func lossy(e *eng, m mutating, losses ...string) {
	var mu sync.Mutex
	n := 0
	e.on(m.route, func(w http.ResponseWriter, _ req) {
		mu.Lock()
		i := n
		n++
		mu.Unlock()
		if i < len(losses) {
			switch losses[i] {
			case "drop":
				drop(w)
			case "429", "429-3":
				w.Header().Set("Retry-After", map[string]string{"429": "1", "429-3": "3"}[losses[i]])
				writeProblem(w, 429, prob("RateLimited", "RateLimited", ""))
			default:
				var status int
				fmt.Sscan(losses[i], &status)
				w.WriteHeader(status)
				_, _ = w.Write([]byte("bad gateway from a proxy"))
			}
			return
		}
		writeJSON(w, 202, m.accepted)
	})
}

func wantOneKey(t *testing.T, tries []req, body string) string {
	t.Helper()
	if len(tries) == 0 {
		t.Fatal("the mutating call was never sent")
	}
	key := tries[0].Header.Get("Idempotency-Key")
	if !keyShape.MatchString(key) {
		t.Errorf("Idempotency-Key = %q, want 32 lowercase hex digits", key)
	}
	for i, c := range tries {
		if got := c.Header.Get("Idempotency-Key"); got != key {
			t.Errorf("try %d: Idempotency-Key %q, want the first try's %q", i+1, got, key)
		}
		if string(c.Body) != string(tries[0].Body) {
			t.Errorf("try %d: body %q, want the first try's %q", i+1, c.Body, tries[0].Body)
		}
		if body != "" && oneLine(string(c.Body)) != body {
			t.Errorf("try %d: body %q, want %s", i+1, c.Body, body)
		}
	}
	return key
}

func TestEachInvocationSendsOneNewIdempotencyKey(t *testing.T) {
	for _, m := range mutatingCommands() {
		t.Run(m.name, func(t *testing.T) {
			var keys []string
			for i := 0; i < 2; i++ {
				e := newEng(t)
				e.reply(m.route, 202, m.accepted)
				m.serve(t, e)
				wantExit(t, e.run(m.args...), 0)
				tries := e.got(splitRoute(m.route))
				if len(tries) != 1 {
					t.Fatalf("%s calls = %d, want 1 per invocation", m.opName, len(tries))
				}
				keys = append(keys, wantOneKey(t, tries, m.body))
			}
			if keys[0] == keys[1] {
				t.Errorf("two invocations sent the same Idempotency-Key %q", keys[0])
			}
		})
	}
}

func TestALostAnswerIsResentWithTheSameKeyAndBody(t *testing.T) {
	t.Parallel()
	for _, m := range mutatingCommands() {
		for _, loss := range []string{"drop", "502", "503", "504"} {
			t.Run(m.name+"/"+loss, func(t *testing.T) {
				t.Parallel()
				e := newEng(t)
				lossy(e, m, loss)
				m.serve(t, e)
				r := e.run(m.args...)
				wantExit(t, r, 0)
				tries := e.got(splitRoute(m.route))
				if len(tries) != 2 {
					t.Fatalf("%s calls = %d, want the lost one and one resend", m.opName, len(tries))
				}
				wantOneKey(t, tries, m.body)
				if gap := tries[1].At.Sub(tries[0].At); gap < 950*time.Millisecond || gap > 2*time.Second {
					t.Errorf("the resend came %v after the first try, want 1 s with up to half again of jitter", gap)
				}
			})
		}
	}
}

func TestA429WaitsItsRetryAfterAndResendsTheSameKey(t *testing.T) {
	t.Parallel()
	m := mutatingCommands()[0]
	e := newEng(t)
	lossy(e, m, "429")
	m.serve(t, e)
	r := e.run(m.args...)
	wantExit(t, r, 0)
	tries := e.got(splitRoute(m.route))
	if len(tries) != 2 {
		t.Fatalf("startRun calls = %d, want 2", len(tries))
	}
	wantOneKey(t, tries, m.body)
	if gap := tries[1].At.Sub(tries[0].At); gap < 950*time.Millisecond {
		t.Errorf("resent %v after a 429 with Retry-After: 1", gap)
	}
}

// A Retry-After longer than the first backoff (1 s, at most 1.5 s with jitter) is waited in full.
func TestARetryAfterLongerThanTheBackoffIsWaitedInFull(t *testing.T) {
	t.Parallel()
	m := mutatingCommands()[0]
	e := newEng(t)
	lossy(e, m, "429-3")
	m.serve(t, e)
	r := e.run(m.args...)
	wantExit(t, r, 0)
	tries := e.got(splitRoute(m.route))
	if len(tries) != 2 {
		t.Fatalf("startRun calls = %d, want 2", len(tries))
	}
	wantOneKey(t, tries, m.body)
	if gap := tries[1].At.Sub(tries[0].At); gap < 3*time.Second-50*time.Millisecond {
		t.Errorf("resent %v after a 429 with Retry-After: 3, want at least 3 s", gap)
	}
}

func TestEveryTryLostExits1NamingTheKey(t *testing.T) {
	t.Parallel()
	for _, m := range mutatingCommands() {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			e := newEng(t)
			lossy(e, m, "drop", "502", "503", "504", "503", "503")
			m.serve(t, e)
			r := e.run(m.args...)
			wantExit(t, r, 1)
			tries := e.got(splitRoute(m.route))
			if len(tries) != 4 {
				t.Fatalf("%s calls = %d, want 4 tries", m.opName, len(tries))
			}
			key := wantOneKey(t, tries, m.body)
			want := fmt.Sprintf("engine: no answer to %s after 4 tries (Idempotency-Key %s); it may have been recorded: see runs", m.opName, key)
			if got := strings.TrimRight(r.err, "\n"); got != want {
				t.Errorf("stderr = %q, want %q", got, want)
			}
			if r.out != "" {
				t.Errorf("stdout = %q, want nothing", r.out)
			}
			for i, base := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
				gap := tries[i+1].At.Sub(tries[i].At)
				if gap < base-50*time.Millisecond || gap > base*3/2+500*time.Millisecond {
					t.Errorf("try %d came %v after try %d, want %v with up to half again of jitter", i+2, gap, i+1, base)
				}
			}
		})
	}
}

func TestAnyOtherAnswerIsFinal(t *testing.T) {
	for _, c := range []struct {
		status int
		code   string
	}{{500, "OperationFailed"}, {409, "IdempotencyKeyReused"}, {409, "RunActive"}, {400, "InvalidProject"}} {
		t.Run(fmt.Sprint(c.status, c.code), func(t *testing.T) {
			e := newEng(t)
			e.refuse("POST /slices/S-1/runs", c.status, prob(c.code, c.code, ""))
			r := e.run("build", "S-1", "notes")
			wantExit(t, r, 4)
			if n := len(e.got("POST", "/slices/S-1/runs")); n != 1 {
				t.Errorf("startRun calls = %d, want 1: a problem is not resent", n)
			}
			wantContains(t, "stderr", r.err, "refused: "+c.code)
			if r.out != "" {
				t.Errorf("stdout = %q, want nothing for a refusal", r.out)
			}
		})
	}
}
