package checksvc_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// An operation under test, with the deployment that serves it and a valid body.
type operation struct {
	name   string
	path   string
	worker bool
	body   func(t testing.TB, dir string) map[string]any
}

func contractCheckBody(t testing.TB, dir string) map[string]any {
	return ccBody(t, dir, string(readTestdata(t, "contract.yaml")), "Feature: F\n")
}

var operations = []operation{
	{"runCheck", "/checks/ScopeKept/run", false, func(t testing.TB, dir string) map[string]any {
		st := step("generate", []string{"gen/**"}, []string{"generated-code"}, nil, nil)
		return checkBody(t, dir, st, nil, "c", "e")
	}},
	{"readStepFiles", "/step-files", false, func(t testing.TB, dir string) map[string]any {
		return common(filepath.Base(dir), step("generate", []string{"gen/**"}, []string{"generated-code"}, nil, nil), nil)
	}},
	{"runProcedure", "/procedures/contract-check/run", true, contractCheckBody},
}

// opServer starts the deployment that serves op over a project folder p1 holding a gen/x.go.
func opServer(t testing.TB, op operation) (string, string, map[string]any) {
	root, dir := fixture(t)
	write(t, dir, "gen/x.go", []byte("package gen\n"))
	c := checkConfig(t, root)
	if op.worker {
		c = workerConfig(t, root, "contract-check", "contract-check")
	}
	return serve(t, c), dir, op.body(t, dir)
}

// ---- 1. auth, version and body ----

func TestAuthRequired(t *testing.T) {
	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			base, _, body := opServer(t, op)
			raw, _ := json.Marshal(body)
			// A good request first, so a 401 below is about the token and nothing else.
			if r := post(t, base, op.path, body); r.status != 200 {
				t.Fatalf("the valid request is not 200: %d %.400s", r.status, r.body)
			}
			for _, auth := range map[string]string{
				"no token":       "",
				"wrong token":    "Bearer " + token + "x",
				"empty bearer":   "Bearer ",
				"other scheme":   "Basic " + token,
				"bare token":     token,
				"lower-case tok": "Bearer " + strings.ToUpper(token),
			} {
				wantProblem(t, do(t, base, op.path, q, auth, "application/json", raw), 401, "Unauthorized")
			}
			// The token is a header, never a query parameter.
			wantProblem(t, do(t, base, op.path, q+"&token="+token, "", "application/json", raw), 401, "Unauthorized")
		})
	}
}

func TestAPIVersion(t *testing.T) {
	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			base, _, body := opServer(t, op)
			raw, _ := json.Marshal(body)
			auth := "Bearer " + token
			wantProblem(t, do(t, base, op.path, "", auth, "application/json", raw), 400, "MissingApiVersionParameter")
			for _, v := range []string{"2026-11-13", "2026-11-12-preview", "1999-01-01", "latest", "2026-11-13-preview2"} {
				p := wantProblem(t, do(t, base, op.path, "api-version="+v, auth, "application/json", raw), 400, "UnsupportedApiVersionValue")
				found := false
				for _, s := range p.SupportedVersions {
					found = found || s == version
				}
				if !found {
					t.Errorf("api-version %s: supported_versions must list %s: %v", v, version, p.SupportedVersions)
				}
			}
		})
	}
}

func TestContentTypeAndBody(t *testing.T) {
	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			base, _, body := opServer(t, op)
			raw, _ := json.Marshal(body)
			auth := "Bearer " + token
			bad := func(name, ctype string, b []byte) {
				t.Helper()
				t.Logf("%s", name)
				wantProblem(t, do(t, base, op.path, q, auth, ctype, b), 400, "InvalidRequest")
			}
			bad("no content type", "", raw)
			bad("text/plain", "text/plain", raw)
			bad("form", "application/x-www-form-urlencoded", raw)
			bad("json-like", "application/json-seq", raw)
			bad("empty body", "application/json", nil)
			bad("not json", "application/json", []byte("{"))
			bad("two documents", "application/json", append(append([]byte{}, raw...), raw...))
			bad("garbage after", "application/json", append(append([]byte{}, raw...), []byte(" x")...))
			bad("an array", "application/json", []byte("[]"))
			bad("null", "application/json", []byte("null"))
			// Parameters may follow application/json.
			if r := do(t, base, op.path, q, auth, "application/json; charset=utf-8", raw); r.status != 200 {
				t.Errorf("application/json; charset=utf-8 must be accepted: %d %.300s", r.status, r.body)
			}
		})
	}
}

// Every mutation of a valid request makes it 400 InvalidRequest, on every operation it applies to.
func TestInvalidRequests(t *testing.T) {
	type mut struct {
		name string
		f    func(m map[string]any)
		only string // "" = every operation; else the operation it applies to
	}
	set := func(path ...any) func(m map[string]any) {
		return func(m map[string]any) {
			var cur any = m
			for i, p := range path[:len(path)-2] {
				switch k := p.(type) {
				case string:
					cur = cur.(map[string]any)[k]
				case int:
					cur = cur.([]any)[k]
				}
				_ = i
			}
			last := path[len(path)-2]
			switch k := last.(type) {
			case string:
				if path[len(path)-1] == nil {
					delete(cur.(map[string]any), k)
				} else {
					cur.(map[string]any)[k] = path[len(path)-1]
				}
			case int:
				cur.([]any)[k] = path[len(path)-1]
			}
		}
	}
	muts := []mut{
		{"extra top-level field", set("extra", true), ""},
		{"extra field in step", set("step", "extra", 1), ""},
		{"extra field in dependencies", set("step", "dependencies", "extra", 1), ""},
		{"no run", set("run", nil), ""},
		{"no step", set("step", nil), ""},
		{"no attempt", set("attempt", nil), ""},
		{"no project", set("project", nil), ""},
		{"no before", set("before", nil), ""},
		{"no time limit", set("time_limit_seconds", nil), ""},
		{"no step name", set("step", "name", nil), ""},
		{"no step writable", set("step", "writable", nil), ""},
		{"no step outputs", set("step", "outputs", nil), ""},
		{"no step dependencies", set("step", "dependencies", nil), ""},
		{"run not RUN-n", set("run", "run-1"), ""},
		{"run null", set("run", nil), ""},
		{"attempt 0", set("attempt", 0), ""},
		{"attempt text", set("attempt", "1"), ""},
		{"round 0", set("round", 0), ""},
		{"time limit 0", set("time_limit_seconds", 0), ""},
		{"time limit over 600", set("time_limit_seconds", 601), ""},
		{"time limit fraction", set("time_limit_seconds", 1.5), ""},
		{"time limit text", set("time_limit_seconds", "5"), ""},
		{"step name upper case", set("step", "name", "Generate"), ""},
		{"writable escapes", set("step", "writable", []any{"../x"}), ""},
		{"writable absolute", set("step", "writable", []any{"/etc/passwd"}), ""},
		{"writable trailing slash", set("step", "writable", []any{"gen/"}), ""},
		{"writable dot part", set("step", "writable", []any{"gen/./x"}), ""},
		{"writable glob inside", set("step", "writable", []any{"gen/**/x"}), ""},
		{"writable with space", set("step", "writable", []any{"a b"}), ""},
		{"writable empty", set("step", "writable", []any{""}), ""},
		{"writable control character", set("step", "writable", []any{"a\u0001b"}), ""},
		{"writable not a list", set("step", "writable", "gen/**"), ""},
		{"several outputs and another number of writable entries", set("step", "outputs", []any{"a", "b", "c"}), ""},
		{"before path escapes", set("before", []any{map[string]any{"path": "../x", "sha256": strings.Repeat("a", 64)}}), ""},
		{"before path absolute", set("before", []any{map[string]any{"path": "/etc/passwd", "sha256": strings.Repeat("a", 64)}}), ""},
		{"before path trailing slash", set("before", []any{map[string]any{"path": "gen/", "sha256": strings.Repeat("a", 64)}}), ""},
		{"before path with control character", set("before", []any{map[string]any{"path": "gen/a\nb", "sha256": strings.Repeat("a", 64)}}), ""},
		{"before path with a space", set("before", []any{map[string]any{"path": "a b", "sha256": strings.Repeat("a", 64)}}), ""},
		{"before path too long", set("before", []any{map[string]any{"path": strings.Repeat("a", 257), "sha256": strings.Repeat("a", 64)}}), ""},
		{"before hash upper case", set("before", []any{map[string]any{"path": "a", "sha256": strings.Repeat("A", 64)}}), ""},
		{"before hash short", set("before", []any{map[string]any{"path": "a", "sha256": "abc"}}), ""},
		{"before entry extra field", set("before", []any{map[string]any{"path": "a", "sha256": strings.Repeat("a", 64), "size": 1}}), ""},
		{"before not a list", set("before", "none"), ""},
		{"project escapes", set("project", "../p1"), ""},
		{"project absolute", set("project", "/etc"), ""},
		{"project trailing slash", set("project", "p1/"), ""},
		{"project with a slash", set("project", "p1/gen"), ""},
		{"project dot", set("project", "."), ""},
		{"project dot dot", set("project", ".."), ""},
		{"project with a control character", set("project", "p1\u0000"), ""},
		{"project with a newline", set("project", "p1\n"), ""},
		{"project upper case", set("project", "P1"), ""},
		{"project empty", set("project", ""), ""},
		{"project too long", set("project", strings.Repeat("a", 64)), ""},
		{"approved with two parts", func(m map[string]any) { m["approved"] = m["approved"].([]any)[:2] }, "runCheck"},
		{"approved content not base64", set("approved", 0, "content", "***"), "runCheck"},
		{"approved unknown part", set("approved", 0, "part", "terms"), "runCheck"},
		{"approved extra field", set("approved", 0, "extra", 1), "runCheck"},
		{"inputs not a list", set("inputs", 1), "runCheck"},
		{"outputs entry extra field", set("outputs", 0, "extra", 1), "runCheck"},
		{"no approved", set("approved", nil), "runCheck"},
		{"no earlier_steps", set("earlier_steps", nil), "runCheck"},
		{"no inputs", set("inputs", nil), "runCheck,runProcedure"},
		{"finding file escapes", func(m map[string]any) {
			m["findings"] = []any{map[string]any{"rule": "R-1", "file": "../x.go", "line": 1, "text": "t", "blocking": false}}
		}, "runCheck"},
		{"finding file absolute", func(m map[string]any) {
			m["findings"] = []any{map[string]any{"rule": "R-1", "file": "/etc/passwd", "line": 1, "text": "t", "blocking": false}}
		}, "runCheck"},
		{"finding text over 4096", func(m map[string]any) {
			m["findings"] = []any{map[string]any{"rule": "R-1", "file": "a.go", "line": 1, "text": strings.Repeat("x", 4097), "blocking": false}}
		}, "runCheck"},
		{"finding rule not R-n", func(m map[string]any) {
			m["findings"] = []any{map[string]any{"rule": "X-1", "text": "t", "blocking": false}}
		}, "runCheck"},
		{"blocking finding without a line", func(m map[string]any) {
			m["findings"] = []any{map[string]any{"rule": "R-1", "file": "a.go", "text": "t", "blocking": true, "check": "AllTestsPass"}}
		}, "runCheck"},
		{"101 findings", func(m map[string]any) {
			f := []any{}
			for i := 0; i < 101; i++ {
				f = append(f, map[string]any{"rule": "R-1", "text": "t", "blocking": false})
			}
			m["findings"] = f
		}, "runCheck"},
		{"confirmation extra field", func(m map[string]any) {
			m["confirmations"] = []any{map[string]any{"check": "AllTestsPass", "passed": true, "reason": "r", "extra": 1}}
		}, "runCheck"},
		{"2001 changed lines", func(m map[string]any) {
			c := []any{}
			for i := 0; i < 2001; i++ {
				c = append(c, map[string]any{"file": "a.go", "line": 1})
			}
			m["changed_lines"] = c
		}, "runCheck"},
		{"five inputs", func(m map[string]any) {
			in := m["inputs"].([]any)
			m["inputs"] = append(in, in[0], in[0], in[0])
		}, "runProcedure"},
		{"input extra field", set("inputs", 0, "extra", 1), "runProcedure"},
		{"input content not base64", set("inputs", 0, "content", "***"), "runProcedure"},
		{"input without content", set("inputs", 0, "content", nil), "runProcedure"},
		{"findings in a procedure request", set("findings", []any{}), "runProcedure"},
		{"approved in a step-files request", set("approved", []any{}), "readStepFiles"},
	}
	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			base, _, body := opServer(t, op)
			if r := post(t, base, op.path, body); r.status != 200 {
				t.Fatalf("the valid request is not 200: %d %.400s", r.status, r.body)
			}
			sent := 1
			for _, m := range muts {
				if m.only != "" && !strings.Contains(m.only, op.name) {
					continue
				}
				// A fresh server (and so a fresh rate window) every 15 requests: the token may make 20 a second.
				if sent++; sent > 15 {
					base, _, _ = opServer(t, op)
					sent = 1
				}
				b := clone(body)
				m.f(b)
				r := post(t, base, op.path, b)
				if r.status != 400 {
					t.Errorf("%s: want 400 InvalidRequest, got %d: %.300s", m.name, r.status, r.body)
					continue
				}
				wantProblem(t, r, 400, "InvalidRequest")
			}
		})
	}
}

// The body limit is x-limits.request-body-max-bytes (12 MiB): that many bytes are read, one more is
// 400 InvalidRequest. Padding with white space keeps the JSON valid.
func TestBodySizeBoundary(t *testing.T) {
	const limit = 12582912
	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			base, _, body := opServer(t, op)
			raw, _ := json.Marshal(body)
			pad := func(n int) []byte { return append(append([]byte{}, raw...), bytes.Repeat([]byte(" "), n-len(raw))...) }
			if r := do(t, base, op.path, q, "Bearer "+token, "application/json", pad(limit)); r.status != 200 {
				t.Errorf("a body of exactly %d bytes must be read: %d %.300s", limit, r.status, r.body)
			}
			wantProblem(t, do(t, base, op.path, q, "Bearer "+token, "application/json", pad(limit+1)), 400, "InvalidRequest")
			wantProblem(t, do(t, base, op.path, q, "Bearer "+token, "application/json", pad(limit*2)), 400, "InvalidRequest")
		})
	}
}

// A `before` list is at most 10000 files.
func TestBeforeListLimit(t *testing.T) {
	base, dir, body := opServer(t, operations[1])
	_ = dir
	mk := func(n int) []pf {
		l := make([]pf, n)
		for i := range l {
			l[i] = pf{fmt.Sprintf("f/%05d", i), strings.Repeat("a", 64)}
		}
		return l
	}
	body["before"] = mk(10000)
	if r := post(t, base, operations[1].path, body); r.status != 200 {
		t.Fatalf("10000 files in before must be read: %d %.300s", r.status, r.body)
	}
	body["before"] = mk(10001)
	wantProblem(t, post(t, base, operations[1].path, body), 400, "InvalidRequest")
}

// ---- 2. deployment split ----

func TestCheckDeploymentServesNoProcedure(t *testing.T) {
	root, dir := fixture(t)
	base := serve(t, checkConfig(t, root))
	for _, name := range []string{"contract-check", "generate", "tests", "no-such-procedure"} {
		wantProblem(t, post(t, base, "/procedures/"+name+"/run", contractCheckBody(t, dir)), 404, "UnknownProcedure")
	}
}

func TestProcedureWorkerServesItsProcedureOnly(t *testing.T) {
	root, dir := fixture(t)
	base := serve(t, workerConfig(t, root, "generate", "generate"))
	// Any check, valid or not, is unknown; the pattern's own names too.
	checkB := operations[0].body(t, dir)
	for _, c := range []string{"ScopeKept", "AllTestsPass", "ContractMatchesApproval", "NoBlockingFindingOpen", "ContractLintPasses", "NoSuchCheck"} {
		wantProblem(t, post(t, base, "/checks/"+c+"/run", checkB), 404, "UnknownCheck")
	}
	wantProblem(t, post(t, base, "/step-files", operations[1].body(t, dir)), 404, "UnknownStep")
	// Another procedure than the one it serves, and one the pattern does not have.
	for _, name := range []string{"contract-check", "tests", "no-such-procedure"} {
		wantProblem(t, post(t, base, "/procedures/"+name+"/run", contractCheckBody(t, dir)), 404, "UnknownProcedure")
	}
}

func TestContractLintIsNotServed(t *testing.T) {
	root, dir := fixture(t)
	base := serve(t, checkConfig(t, root))
	p := wantProblem(t, post(t, base, "/checks/ContractLintPasses/run", operations[0].body(t, dir)), 404, "UnknownCheck")
	if !strings.Contains(p.Detail, "ContractLintPasses") {
		t.Logf("detail does not name the check (not required): %q", p.Detail)
	}
	wantProblem(t, post(t, base, "/checks/NoSuchCheck/run", operations[0].body(t, dir)), 404, "UnknownCheck")
}

// ---- rate limits ----

func TestRateLimitPerToken(t *testing.T) {
	root, dir := fixture(t)
	base := serve(t, checkConfig(t, root))
	body := operations[1].body(t, dir)
	limited := 0
	for i := 1; i <= 40; i++ {
		r := post(t, base, operations[1].path, body)
		if r.status == 429 {
			if i <= 20 && limited == 0 {
				t.Fatalf("request %d of the first second is 429; the token may make 20", i)
			}
			p := wantProblem(t, r, 429, "RateLimited")
			_ = p
			if n, err := strconv.Atoi(r.header.Get("Retry-After")); err != nil || n < 1 {
				t.Errorf("429 needs Retry-After of at least 1: %q", r.header.Get("Retry-After"))
			}
			limited++
		} else if r.status != 200 {
			t.Fatalf("request %d: %d %.200s", i, r.status, r.body)
		}
	}
	if limited == 0 {
		t.Fatal("40 requests in a moment were never limited (limit 20 per second per token)")
	}
}

func TestRateLimitWithoutToken(t *testing.T) {
	root, dir := fixture(t)
	base := serve(t, checkConfig(t, root))
	raw, _ := json.Marshal(operations[1].body(t, dir))
	got401, got429 := 0, 0
	for i := 1; i <= 40; i++ {
		r := do(t, base, operations[1].path, q, "Bearer "+token+"x", "application/json", raw)
		switch r.status {
		case 401:
			if got429 > 0 {
				break
			}
			got401++
		case 429:
			wantProblem(t, r, 429, "RateLimited")
			if r.header.Get("Retry-After") == "" {
				t.Error("429 needs Retry-After")
			}
			got429++
		default:
			t.Fatalf("request %d: want 401 or 429, got %d", i, r.status)
		}
	}
	if got401 < 20 || got429 == 0 {
		t.Fatalf("without a token the first 20 requests of a second get 401 and more get 429: got %d 401s and %d 429s", got401, got429)
	}
}

// ---- 503 Busy ----

func sleepCheckBody(t testing.TB, dir string, limit int) map[string]any {
	st := step("code", []string{"internal/**", "cmd/**"}, []string{"code"}, nil, nil)
	b := checkBody(t, dir, st, nil, "c", "e")
	b["time_limit_seconds"] = limit
	return b
}

func pidFiles(dir string) []int {
	es, _ := os.ReadDir(dir)
	var pids []int
	for _, e := range es {
		if n, err := strconv.Atoi(e.Name()); err == nil {
			pids = append(pids, n)
		}
	}
	return pids
}

func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	// "pid (name) S ...": a zombie is dead.
	s := string(b)
	if i := strings.LastIndex(s, ")"); i >= 0 && len(s) > i+2 {
		return s[i+2] != 'Z'
	}
	return true
}

func TestBusyBeyondTwoCalls(t *testing.T) {
	root := t.TempDir()
	pids := t.TempDir()
	for _, p := range []string{"p1", "p2"} {
		d := filepath.Join(root, p)
		os.MkdirAll(d, 0o755)
		copyTree(t, filepath.Join("testdata", "testssleep"), d)
	}
	env := []string{"AP_TEST_PIDDIR=" + pids}
	c := checkConfig(t, root)
	c.Env = append(c.Env, env...)
	base := serve(t, c)

	var wg sync.WaitGroup
	results := make([]resp, 2)
	for i, p := range []string{"p1", "p2"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = post(t, base, "/checks/AllTestsPass/run", sleepCheckBody(t, filepath.Join(root, p), 60))
		}()
	}
	deadline := time.Now().Add(60 * time.Second)
	for len(pidFiles(pids)) < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if len(pidFiles(pids)) < 2 {
		t.Fatal("the two sleeping tests did not both start: the service is not running two calls at once")
	}
	// A third call runs nothing and says when to come back.
	r := post(t, base, "/checks/ScopeKept/run", operations[0].body(t, filepath.Join(root, "p1")))
	wantProblem(t, r, 503, "Busy")
	if n, err := strconv.Atoi(r.header.Get("Retry-After")); err != nil || n < 1 {
		t.Errorf("503 needs Retry-After of at least 1: %q", r.header.Get("Retry-After"))
	}
	r = post(t, base, "/step-files", operations[1].body(t, filepath.Join(root, "p1")))
	wantProblem(t, r, 503, "Busy")
	wg.Wait()
	for i, r := range results {
		wantProblem(t, r, 504, "TimeLimitExceeded")
		_ = i
	}
	// The slots are free again.
	if r := post(t, base, "/step-files", operations[1].body(t, filepath.Join(root, "p1"))); r.status != 200 {
		t.Fatalf("after both calls ended a new call must run: %d %.300s", r.status, r.body)
	}
}

// ---- 9. 504 and the process tree ----

func TestTimeLimitKillsProcessTree(t *testing.T) {
	root, dir := fixture(t, "testssleep")
	pids := t.TempDir()
	c := checkConfig(t, root)
	c.Env = append(c.Env, "AP_TEST_PIDDIR="+pids)
	base := serve(t, c)
	start := time.Now()
	r := post(t, base, "/checks/AllTestsPass/run", sleepCheckBody(t, dir, 30))
	took := time.Since(start)
	wantProblem(t, r, 504, "TimeLimitExceeded")
	if took > 40*time.Second {
		t.Errorf("the call took %s for a time limit of 5 seconds", took)
	}
	ps := pidFiles(pids)
	if len(ps) != 1 {
		t.Fatalf("the sleeping test should have started once, pid files: %v", ps)
	}
	for end := time.Now().Add(5 * time.Second); alive(ps[0]) && time.Now().Before(end); {
		time.Sleep(50 * time.Millisecond)
	}
	if alive(ps[0]) {
		syscall.Kill(ps[0], syscall.SIGKILL)
		t.Fatalf("the test process %d is still running after the 504: the process tree was not stopped", ps[0])
	}
	// readStepFiles and a check on the same project still work after a 504.
	if r := post(t, base, "/step-files", operations[1].body(t, dir)); r.status != 200 {
		t.Fatalf("after a 504 the service must answer: %d %.300s", r.status, r.body)
	}
}
