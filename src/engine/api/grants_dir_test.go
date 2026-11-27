package api

// T053d, engine side: how the engine learns the grants `ap build-workers` makes after it started
// (owner, 2026-11-13). Judged from the owner's decision only:
//   - one file per grant in Config.GrantsDir, named by the lowercase hex SHA-256 of the worker's
//     token, holding JSON {"slice":"S-1","step":"code","call_token":"…"}, mode 0600;
//   - the engine reads the folder at each worker-token authentication, so a grant written after
//     NewHandler works at once and a removed file revokes it;
//   - an unreadable or malformed file is no grant: the engine fails closed (401, the token is not
//     accepted);
//   - call_token is the token the engine calls that worker with (Workers.Endpoint, which the step
//     worker reads).
// Config.WorkerGrants keeps working beside it. Shared helpers are workers_test.go's (register,
// decodeWorker, declaredSteps); none is changed.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// grantFileName is the name of a token's grant file: the lowercase hex SHA-256 of the token.
func grantFileName(token string) string {
	s := sha256.Sum256([]byte(token))
	return hex.EncodeToString(s[:])
}

// writeGrantFile writes the grant of token as the launcher does: mode 0600, the owner's JSON.
func writeGrantFile(t *testing.T, dir, token, slice, step, callToken string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"slice": slice, "step": step, "call_token": callToken})
	if err != nil {
		t.Fatal(err)
	}
	return writeRawGrantFile(t, dir, grantFileName(token), b)
}

func writeRawGrantFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// grantsDirRig is an engine with the notes slice S-1 approved, the given grants folder and a
// Workers registry the test can read. mutate, if given, changes the config further.
func grantsDirRig(t *testing.T, dir string, mutate func(*Config)) (*rig, *Workers) {
	t.Helper()
	sp := newNotes(t)
	sp.approveAfterSeed(t)
	ws := NewWorkers()
	r := newRig(t, func(c *Config) {
		c.Slices, c.Store = sp.svc, sp.store
		c.GrantsDir = dir
		c.Workers = ws
		if mutate != nil {
			mutate(c)
		}
	})
	return r, ws
}

const grantsTok = "worker-token-from-a-grant-file-0001"

func TestGrantsDirGrantWrittenAfterStartRegistersItsSliceAndStepOnly(t *testing.T) {
	dir := t.TempDir()
	r, ws := grantsDirRig(t, dir, nil)
	// Before the file exists the token is not accepted.
	register(r, grantsTok, "http://code-worker:8080").problem(t, 401, "Unauthorized")

	writeGrantFile(t, dir, grantsTok, "S-1", "code", "call-token-code")
	got := decodeWorker(t, register(r, grantsTok, "http://code-worker:8080"))
	if want := (workerBody{"S-1", "code", "http://code-worker:8080"}); got != want {
		t.Fatalf("Worker = %+v, want %+v", got, want)
	}
	if url, _, ok := ws.Endpoint("S-1", "", "code"); !ok || url != "http://code-worker:8080" {
		t.Errorf("Endpoint(S-1, code) = %q, %v; want the registered endpoint", url, ok)
	}
	for _, st := range declaredSteps {
		if st == "code" {
			continue
		}
		if _, _, ok := ws.Endpoint("S-1", "", st); ok {
			t.Errorf("a grant for code registered a worker for %s too", st)
		}
	}
	// The grant is for registerWorker alone.
	r.do("GET", spURL("S-1", ""), grantsTok, "", nil).problem(t, 403, "Forbidden")
}

func TestGrantsDirGrantForAnotherSliceOrAnUndeclaredStepIsNotFound(t *testing.T) {
	dir := t.TempDir()
	r, ws := grantsDirRig(t, dir, nil)
	writeGrantFile(t, dir, "tok-other-slice-000000000000000001", "S-9", "code", "c1")
	writeGrantFile(t, dir, "tok-undeclared-step-0000000000001", "S-1", "deploy", "c2")
	register(r, "tok-other-slice-000000000000000001", "http://svc:8080").problem(t, 404, "NotFound")
	register(r, "tok-undeclared-step-0000000000001", "http://svc:8080").problem(t, 404, "NotFound")
	for _, st := range declaredSteps {
		if _, _, ok := ws.Endpoint("S-1", "", st); ok {
			t.Errorf("a refused registration registered %s", st)
		}
	}
}

func TestGrantsDirTokenWithNoFileIs401(t *testing.T) {
	dir := t.TempDir()
	r, _ := grantsDirRig(t, dir, nil)
	writeGrantFile(t, dir, grantsTok, "S-1", "code", "call-token-code")
	register(r, "a-token-with-no-grant-file-000001", "http://svc:8080").problem(t, 401, "Unauthorized")
	// The hash itself is not a token.
	register(r, grantFileName(grantsTok), "http://svc:8080").problem(t, 401, "Unauthorized")
}

func TestGrantsDirRemovedFileRevokesTheGrant(t *testing.T) {
	dir := t.TempDir()
	r, _ := grantsDirRig(t, dir, nil)
	p := writeGrantFile(t, dir, grantsTok, "S-1", "tests", "call-token-tests")
	decodeWorker(t, register(r, grantsTok, "http://tests-worker:8080"))
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	register(r, grantsTok, "http://tests-worker:8080").problem(t, 401, "Unauthorized")
}

func TestGrantsDirIsReadAtEachAuthentication(t *testing.T) {
	dir := t.TempDir()
	r, ws := grantsDirRig(t, dir, nil)
	writeGrantFile(t, dir, grantsTok, "S-1", "tests", "call-1")
	decodeWorker(t, register(r, grantsTok, "http://w:8080"))
	// The launcher replaces the grant: the next authentication sees the new one.
	writeGrantFile(t, dir, grantsTok, "S-1", "review", "call-2")
	got := decodeWorker(t, register(r, grantsTok, "http://w2:8080"))
	if got.Step != "review" || got.Slice != "S-1" {
		t.Fatalf("Worker = %+v, want the replaced grant's step review", got)
	}
	if _, tok, ok := ws.Endpoint("S-1", "", "review"); !ok || tok != "call-2" {
		t.Errorf("Endpoint(S-1, review) token = %q, %v; want the replaced grant's call_token", tok, ok)
	}
}

func TestGrantsDirMalformedOrUnreadableFileGrantsNothing(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"not JSON", "slice=S-1 step=code"},
		{"empty", ""},
		{"a JSON array", `["S-1","code","call"]`},
		{"a JSON string", `"S-1/code"`},
		{"null", `null`},
		{"no step", `{"slice":"S-1","call_token":"c"}`},
		{"no slice", `{"step":"code","call_token":"c"}`},
		{"slice not a string", `{"slice":1,"step":"code","call_token":"c"}`},
		{"step not a string", `{"slice":"S-1","step":["code"],"call_token":"c"}`},
		{"truncated", `{"slice":"S-1","step":"co`},
		{"no call_token", `{"slice":"S-1","step":"code"}`},
		{"empty call_token", `{"slice":"S-1","step":"code","call_token":""}`},
		{"empty slice", `{"slice":"","step":"code","call_token":"c"}`},
		{"empty step", `{"slice":"S-1","step":"","call_token":"c"}`},
		{"an unknown field", `{"slice":"S-1","step":"code","call_token":"c","admin":true}`},
		{"trailing data", `{"slice":"S-1","step":"code","call_token":"c"} {"slice":"S-1"}`},
		{"trailing garbage", `{"slice":"S-1","step":"code","call_token":"c"}x`},
		{"over 4 KiB", `{"slice":"S-1","step":"code","call_token":"` + strings.Repeat("c", 4097) + `"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			r, ws := grantsDirRig(t, dir, nil)
			writeRawGrantFile(t, dir, grantFileName(grantsTok), []byte(c.content))
			register(r, grantsTok, "http://svc:8080").problem(t, 401, "Unauthorized")
			if _, _, ok := ws.Endpoint("S-1", "", "code"); ok {
				t.Error("a malformed grant registered a worker")
			}
			// The engine keeps serving: a good grant beside it still works.
			writeGrantFile(t, dir, "a-good-token-beside-it-0000000001", "S-1", "code", "c")
			decodeWorker(t, register(r, "a-good-token-beside-it-0000000001", "http://svc:8080"))
		})
	}

	t.Run("unreadable", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads a mode 000 file; this case needs a non-root user")
		}
		dir := t.TempDir()
		r, _ := grantsDirRig(t, dir, nil)
		p := writeGrantFile(t, dir, grantsTok, "S-1", "code", "c")
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
		register(r, grantsTok, "http://svc:8080").problem(t, 401, "Unauthorized")
	})

	t.Run("a folder named by the hash", func(t *testing.T) {
		dir := t.TempDir()
		r, _ := grantsDirRig(t, dir, nil)
		if err := os.Mkdir(filepath.Join(dir, grantFileName(grantsTok)), 0o700); err != nil {
			t.Fatal(err)
		}
		register(r, grantsTok, "http://svc:8080").problem(t, 401, "Unauthorized")
	})

	t.Run("a symlink to a valid grant", func(t *testing.T) {
		for name, target := range map[string]func(dir string) string{
			"outside the folder": func(string) string {
				return writeRawGrantFile(t, t.TempDir(), "grant", []byte(`{"slice":"S-1","step":"code","call_token":"c"}`))
			},
			"inside the folder": func(dir string) string {
				return writeGrantFile(t, dir, "another-token-with-a-real-grant-01", "S-1", "code", "c")
			},
		} {
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				r, ws := grantsDirRig(t, dir, nil)
				if err := os.Symlink(target(dir), filepath.Join(dir, grantFileName(grantsTok))); err != nil {
					t.Fatal(err)
				}
				register(r, grantsTok, "http://svc:8080").problem(t, 401, "Unauthorized")
				if _, _, ok := ws.Endpoint("S-1", "", "code"); ok {
					t.Error("a symlinked grant registered a worker")
				}
			})
		}
	})
}

func TestGrantsDirFileForAnotherTokensHashGrantsNothing(t *testing.T) {
	dir := t.TempDir()
	r, ws := grantsDirRig(t, dir, nil)
	// A valid grant, but filed under another token's hash.
	writeRawGrantFile(t, dir, grantFileName("some-other-token-00000000000001"),
		[]byte(`{"slice":"S-1","step":"code","call_token":"c"}`))
	register(r, grantsTok, "http://svc:8080").problem(t, 401, "Unauthorized")
	// The hash in upper case is not the file's name either.
	writeRawGrantFile(t, dir, upperHex(grantFileName(grantsTok)), []byte(`{"slice":"S-1","step":"code","call_token":"c"}`))
	register(r, grantsTok, "http://svc:8080").problem(t, 401, "Unauthorized")
	if _, _, ok := ws.Endpoint("S-1", "", "code"); ok {
		t.Error("a grant filed under another name registered a worker")
	}
}

func upperHex(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'f' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

func TestGrantsDirCallTokenIsWhatTheEngineCallsTheWorkerWith(t *testing.T) {
	dir := t.TempDir()
	r, ws := grantsDirRig(t, dir, nil)
	writeGrantFile(t, dir, grantsTok, "S-1", "generate", "the-call-token-for-generate")
	decodeWorker(t, register(r, grantsTok, "http://generate-worker:8080"))
	url, tok, ok := ws.Endpoint("S-1", "", "generate")
	if !ok || url != "http://generate-worker:8080" {
		t.Fatalf("Endpoint(S-1, generate) = %q, %v", url, ok)
	}
	if tok != "the-call-token-for-generate" {
		t.Errorf("call token = %q, want the grant file's call_token", tok)
	}
	if tok == grantsTok {
		t.Error("the engine would call the worker with the worker's own token")
	}
}

func TestGrantsDirMissingFolderAcceptsNoGrantButServesTheSession(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-made-yet")
	r, _ := grantsDirRig(t, dir, nil)
	register(r, grantsTok, "http://svc:8080").problem(t, 401, "Unauthorized")
	if w := r.do("GET", spURL("S-1", ""), clientToken, "", nil); w.Code != 200 {
		t.Errorf("client token: status %d, want 200: %s", w.Code, w.Body.String())
	}
	// Made after start, it is read at the next authentication.
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeGrantFile(t, dir, grantsTok, "S-1", "code", "c")
	decodeWorker(t, register(r, grantsTok, "http://svc:8080"))
}

func TestGrantsDirAndWorkerGrantsBothWork(t *testing.T) {
	dir := t.TempDir()
	r, ws := grantsDirRig(t, dir, func(c *Config) {
		c.WorkerGrants = map[string]WorkerGrant{"tok-from-config-000000000000001": {Slice: "S-1", Step: "tests", CallToken: "call-tests"}}
	})
	writeGrantFile(t, dir, grantsTok, "S-1", "code", "call-code")
	decodeWorker(t, register(r, "tok-from-config-000000000000001", "http://tests:8080"))
	decodeWorker(t, register(r, grantsTok, "http://code:8080"))
	if _, tok, ok := ws.Endpoint("S-1", "", "tests"); !ok || tok != "call-tests" {
		t.Errorf("Endpoint(S-1, tests) token = %q, %v", tok, ok)
	}
	if _, tok, ok := ws.Endpoint("S-1", "", "code"); !ok || tok != "call-code" {
		t.Errorf("Endpoint(S-1, code) token = %q, %v", tok, ok)
	}
	// The session worker token is unchanged: accepted, but it has no grant.
	register(r, workerToken, "http://svc:8080").problem(t, 404, "NotFound")
}
