// T094: failing tests, by a separate author, for the check program's per-call folder (research
// D37, ADR 0041, data-model "How a check runs Go"), over HTTP only, against the check contract
// (src/check/api/preview/2026-11-13-preview/openapi.yaml, runCheck and runProcedure): "Each call
// gets a new, empty temporary folder, removed when it ends; every command of the call has
// `GOCACHE`, `GOTMPDIR`, `TMPDIR`, `HOME` and `GOPATH` inside it, and every `go test` carries
// `-count=1`, so nothing a check writes there survives the call. The pinned modules' cache is
// shared read-only."
//
// How the tests see the commands: a `go` script first on this process's PATH (the service runs
// in-process, and finds `go` on it) records, for every go command a call starts, its arguments,
// the five variables and GOMODCACHE, and, for the first command of each TMPDIR, every entry that
// is not a folder under the five folders. Then it acts by its mode file: `real` runs the real go,
// the others answer as a fake (fail, slowfail, sleep, witness) so that no call compiles anything
// it does not need. The generate procedure's oapi-codegen is a recording script too, which runs
// the real oapi-codegen.
//
// The service process's own settings stand for the image's shared ones (Dockerfile.check today:
// GOCACHE=/tmp/go-cache, HOME=/tmp/home, GOPATH=/tmp/go): GOCACHE, HOME and GOPATH are set to
// folders of this test, which no call's command may use. A call's folder is the deepest folder
// holding all five variables of a command; it must lie below os.TempDir() (/tmp in the check
// container) and outside the test's shared folders.
package checksvc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// cfVars are the variables every command of a call has inside the call's folder.
var cfVars = []string{"GOCACHE", "GOTMPDIR", "TMPDIR", "HOME", "GOPATH"}

// cfOld are the shared folders of the image's settings today, which no call may use.
var cfOld = []string{"/tmp/go-cache", "/tmp/home", "/tmp/go"}

// cfRecord is one command a call started.
type cfRecord struct {
	tool  string // go or oapi-codegen
	args  []string
	vars  map[string]string // set variables only
	first bool              // the first command seen with its TMPDIR
	found []string          // entries (not folders) under the five folders, recorded on first
	file  string
}

func (r cfRecord) String() string {
	return fmt.Sprintf("%s %s", r.tool, strings.Join(r.args, " "))
}

// cfEnv is a test's recording toolchain and its shared settings.
type cfEnv struct {
	t      *testing.T
	wrap   string // folder of the go script and its mode file
	log    string // folder of the records
	oapi   string // the recording oapi-codegen (only when asked for)
	shared string // the folder of the test's shared GOCACHE, HOME and GOPATH
	modc   string // the module cache commands must keep
}

const cfGoScript = `#!/bin/sh
rec=$(mktemp "@LOG@/rec.XXXXXXXX") || exit 97
{
  printf 'TOOL %s\n' "@TOOL@"
  for a in "$@"; do printf 'ARG %s\n' "$a"; done
  for v in GOCACHE GOTMPDIR TMPDIR HOME GOPATH GOMODCACHE; do
    eval "isset=\${$v+x}"
    eval "val=\${$v-}"
    if [ -n "$isset" ]; then printf 'VAR %s %s\n' "$v" "$val"; fi
  done
  key=$(printf '%s' "${TMPDIR-}" | tr '/' '_')
  if mkdir "@LOG@/first.$key" 2>/dev/null; then
    printf 'FIRST\n'
    for v in GOCACHE GOTMPDIR TMPDIR HOME GOPATH; do
      eval "d=\${$v-}"
      if [ -n "$d" ] && [ -d "$d" ]; then find "$d" ! -type d | sed 's/^/ENTRY /'; fi
    done
  fi
} >"$rec"
mv "$rec" "$rec.done"
`

const cfGoModes = `mode=$(cat "@WRAP@/mode" 2>/dev/null || echo fail)
case "$mode" in
real) exec "@REAL@" "$@" ;;
sleep) exec sleep 300 ;;
slowfail) sleep 2; echo "t094 fake go: fails on purpose"; exit 1 ;;
witness)
  prof=; run=; prev=
  for a in "$@"; do
    case "$a" in -coverprofile=*) prof=${a#-coverprofile=} ;; esac
    if [ "$prev" = "-run" ]; then run=$a; fi
    prev=$a
  done
  test=${run#^}; test=${test%\$}
  pkg=example.com/w/review/r1
  if [ -z "$test" ]; then
    printf 'mode: set\nexample.com/w/internal/greet/greet.go:23.2,23.17 1 0\n' >"$prof"
    printf '{"Action":"pass","Package":"%s"}\n' "$pkg"
    exit 0
  fi
  printf 'mode: set\nexample.com/w/internal/greet/greet.go:23.2,23.17 1 1\n' >"$prof"
  printf '{"Action":"run","Package":"%s","Test":"%s"}\n' "$pkg" "$test"
  printf '{"Action":"output","Package":"%s","Test":"%s","Output":"    name_test.go:17: Greet(\\"Ada\\") = \\"Hello!\\"\\n"}\n' "$pkg" "$test"
  printf '{"Action":"fail","Package":"%s","Test":"%s"}\n' "$pkg" "$test"
  printf '{"Action":"fail","Package":"%s"}\n' "$pkg"
  exit 1 ;;
*) echo "t094 fake go: fails on purpose"; exit 1 ;;
esac
`

const cfOapiScript = `exec "@REAL@" "$@"
`

// newCfEnv puts the recording go first on PATH and gives this process shared GOCACHE, HOME and
// GOPATH folders (the stand-ins for the image's /tmp/go-cache, /tmp/home and /tmp/go), keeping
// the module cache go uses now. With oapi, a recording oapi-codegen is made too.
func newCfEnv(t *testing.T, oapi bool) *cfEnv {
	t.Helper()
	real, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("no go on PATH: %v", err)
	}
	modc := os.Getenv("GOMODCACHE")
	if modc == "" {
		out, err := exec.Command(real, "env", "GOMODCACHE").Output()
		if err != nil {
			t.Fatalf("go env GOMODCACHE: %v", err)
		}
		modc = strings.TrimSpace(string(out))
	}
	e := &cfEnv{t: t, wrap: t.TempDir(), log: t.TempDir(), shared: t.TempDir(), modc: modc}
	script := func(path, tool, realBin, tail string) {
		body := strings.NewReplacer("@LOG@", e.log, "@TOOL@", tool, "@WRAP@", e.wrap, "@REAL@", realBin).Replace(cfGoScript + tail)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script(filepath.Join(e.wrap, "go"), "go", real, cfGoModes)
	if oapi {
		dir := filepath.Join(e.wrap, "oapi")
		os.MkdirAll(dir, 0o755)
		e.oapi = filepath.Join(dir, "oapi-codegen")
		script(e.oapi, "oapi-codegen", needOapi(t), cfOapiScript)
	}
	for _, d := range []string{"go-cache", "home", "go"} {
		os.MkdirAll(filepath.Join(e.shared, d), 0o755)
	}
	t.Setenv("PATH", e.wrap+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOMODCACHE", modc)
	t.Setenv("GOCACHE", filepath.Join(e.shared, "go-cache"))
	t.Setenv("HOME", filepath.Join(e.shared, "home"))
	t.Setenv("GOPATH", filepath.Join(e.shared, "go"))
	e.mode("fail")
	return e
}

func (e *cfEnv) mode(m string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.wrap, "mode"), []byte(m), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// reset forgets every record so far; which TMPDIRs were seen is kept, so a call that reuses
// another's TMPDIR has no first command.
func (e *cfEnv) reset() {
	e.t.Helper()
	ms, _ := filepath.Glob(filepath.Join(e.log, "rec.*"))
	for _, m := range ms {
		os.Remove(m)
	}
}

// records are the commands recorded since the last reset.
func (e *cfEnv) records() []cfRecord {
	e.t.Helper()
	ms, _ := filepath.Glob(filepath.Join(e.log, "rec.*.done"))
	sort.Strings(ms)
	var out []cfRecord
	for _, m := range ms {
		b, err := os.ReadFile(m)
		if err != nil {
			e.t.Fatal(err)
		}
		r := cfRecord{vars: map[string]string{}, file: m}
		for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			k, v, _ := strings.Cut(l, " ")
			switch k {
			case "TOOL":
				r.tool = v
			case "ARG":
				r.args = append(r.args, v)
			case "VAR":
				name, val, _ := strings.Cut(v, " ")
				r.vars[name] = val
			case "FIRST":
				r.first = true
			case "ENTRY":
				r.found = append(r.found, v)
			}
		}
		out = append(out, r)
	}
	return out
}

// waitRecords waits until at least n commands are recorded.
func (e *cfEnv) waitRecords(n int, within time.Duration) []cfRecord {
	e.t.Helper()
	end := time.Now().Add(within)
	for {
		rs := e.records()
		if len(rs) >= n || time.Now().After(end) {
			return rs
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// inside reports whether p is dir or below it.
func inside(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// callFolder is the call's folder a command shows: the deepest folder holding all five
// variables, which must be strictly below os.TempDir() and outside the test's shared folder and
// the image's shared ones. why says what is wrong when there is none.
func (e *cfEnv) callFolder(r cfRecord) (folder, why string) {
	var vals []string
	for _, v := range cfVars {
		val, ok := r.vars[v]
		if !ok || val == "" {
			return "", fmt.Sprintf("%s has no %s: every command of a call has it inside the call's folder (vars %v)", r, v, r.vars)
		}
		if !filepath.IsAbs(val) {
			return "", fmt.Sprintf("%s has %s=%q, not an absolute folder", r, v, val)
		}
		for _, old := range cfOld {
			if inside(filepath.Clean(val), old) {
				return "", fmt.Sprintf("%s has %s=%s, inside the shared %s", r, v, val, old)
			}
		}
		if inside(filepath.Clean(val), e.shared) {
			return "", fmt.Sprintf("%s has %s=%s, the service process's shared setting, not the call's own folder", r, v, val)
		}
		vals = append(vals, filepath.Clean(val))
	}
	common := vals[0]
	for _, v := range vals[1:] {
		for !inside(v, common) {
			common = filepath.Dir(common)
		}
	}
	tmp := filepath.Clean(os.TempDir())
	if common == tmp || !inside(common, tmp) {
		return "", fmt.Sprintf("%s: the five variables %v share no folder of their own below %s (deepest common folder %s)", r, r.vars, tmp, common)
	}
	if inside(e.shared, common) {
		return "", fmt.Sprintf("%s: the folder %s holds the service's shared settings too, so it is not the call's own", r, common)
	}
	return common, ""
}

// oneFolder is the one folder every command of a call shows; it fails the test when a command
// shows none or two commands show different ones.
func (e *cfEnv) oneFolder(what string, rs []cfRecord) string {
	e.t.Helper()
	if len(rs) == 0 {
		e.t.Errorf("%s: no command was recorded", what)
		return ""
	}
	folder := ""
	for _, r := range rs {
		f, why := e.callFolder(r)
		if why != "" {
			e.t.Errorf("%s: %s", what, why)
			continue
		}
		if folder != "" && f != folder {
			e.t.Errorf("%s: two commands of one call use two folders, %s and %s", what, folder, f)
		}
		folder = f
	}
	return folder
}

// wantGone fails when the call's folder still exists after within.
func wantGone(t *testing.T, what, folder string, within time.Duration) {
	t.Helper()
	if folder == "" {
		return
	}
	end := time.Now().Add(within)
	for {
		_, err := os.Lstat(folder)
		if os.IsNotExist(err) {
			return
		}
		if time.Now().After(end) {
			t.Errorf("%s: the call's folder %s still exists after the call ended", what, folder)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// cfStatus posts a call and returns its status only (the verdict's fields are other tests').
func cfStatus(t *testing.T, base, path string, body map[string]any) (int, string) {
	t.Helper()
	r := post(t, base, path, body)
	return r.status, string(r.body)
}

// cfVerdict is the verdict's passed and reason, read leniently.
func cfVerdict(t *testing.T, r resp) (bool, string) {
	t.Helper()
	if r.status != 200 {
		t.Fatalf("want 200, got %d: %.600s", r.status, r.body)
	}
	var v struct {
		Passed bool   `json:"passed"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(r.body, &v); err != nil {
		t.Fatalf("not a verdict: %v: %.300s", err, r.body)
	}
	return v.Passed, v.Reason
}

// ---- 1. each call its own empty folder ----

func TestEachCallGetsItsOwnEmptyFolder(t *testing.T) {
	e := newCfEnv(t, false)
	root := t.TempDir()
	for _, p := range []string{"p1", "p2"} {
		d := filepath.Join(root, p)
		os.MkdirAll(d, 0o755)
		copyTree(t, filepath.Join("testdata", "testspass"), d)
	}
	cfg := checkConfig(t, root)
	cfg.GoCacheBase = "" // this test judges an empty folder; the warm base's copy is tested in call_cache_base_test.go
	base := serve(t, cfg)
	body := func(p string) map[string]any { return sleepCheckBody(t, filepath.Join(root, p), 60) }

	check := func(what string, rs []cfRecord) string {
		folder := e.oneFolder(what, rs)
		for _, r := range rs {
			if r.first && len(r.found) > 0 {
				t.Errorf("%s: the call's folder is not empty when its first command starts: %v", what, r.found)
			}
		}
		if len(rs) > 0 && !anyFirst(rs) {
			t.Errorf("%s: no command of the call was the first with its TMPDIR, so the call reused another's", what)
		}
		return folder
	}

	// One after the other.
	var seq []string
	for i, p := range []string{"p1", "p1"} {
		e.reset()
		if st, b := cfStatus(t, base, "/checks/AllTestsPass/run", body(p)); st != 200 {
			t.Fatalf("call %d: %d %.300s", i+1, st, b)
		}
		f := check(fmt.Sprintf("sequential call %d", i+1), e.records())
		wantGone(t, fmt.Sprintf("sequential call %d", i+1), f, 0)
		seq = append(seq, f)
	}
	if seq[0] != "" && seq[0] == seq[1] {
		t.Errorf("two calls one after the other used the same folder %s", seq[0])
	}

	// At once: two calls overlap (each go command takes 2 seconds).
	e.reset()
	e.mode("slowfail")
	var wg sync.WaitGroup
	status := make([]int, 2)
	for i, p := range []string{"p1", "p2"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status[i], _ = cfStatus(t, base, "/checks/AllTestsPass/run", body(p))
		}()
	}
	wg.Wait()
	for i, s := range status {
		if s != 200 {
			t.Fatalf("concurrent call %d: status %d", i+1, s)
		}
	}
	rs := e.records()
	if len(rs) != 2 {
		t.Fatalf("two concurrent calls of AllTestsPass ran %d go commands, want 2: %v", len(rs), rs)
	}
	a := check("concurrent call 1", rs[:1])
	b := check("concurrent call 2", rs[1:])
	if a != "" && a == b {
		t.Errorf("two calls at once used the same folder %s", a)
	}
	wantGone(t, "concurrent call 1", a, 0)
	wantGone(t, "concurrent call 2", b, 0)
}

func anyFirst(rs []cfRecord) bool {
	for _, r := range rs {
		if r.first {
			return true
		}
	}
	return false
}

// ---- 2. nothing a check writes survives the call ----

// cfWriterTest is a declared test that writes one file to TMPDIR (os.TempDir), one to HOME and
// one into GOCACHE, appending their paths to a log outside the project; first it fails when a
// file of an earlier call's log, or one of the same name in its own folders, still exists.
const cfWriterTest = `package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const logPath = %q

func TestWritesItsFolders(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("no HOME: %%v", err)
	}
	cache := os.Getenv("GOCACHE")
	if cache == "" {
		t.Fatal("GOCACHE is not set for the test")
	}
	mine := []string{
		filepath.Join(os.TempDir(), "t094-marker-tmpdir"),
		filepath.Join(home, "t094-marker-home"),
		filepath.Join(cache, "t094-marker-gocache"),
	}
	earlier, _ := os.ReadFile(logPath)
	for _, p := range append(strings.Fields(string(earlier)), mine...) {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("SURVIVED %%s", p)
		}
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, p := range mine {
		if err := os.WriteFile(p, []byte("written by a check's test"), 0o644); err != nil {
			t.Fatalf("cannot write %%s: %%v", p, err)
		}
		f.WriteString(p + "\n")
	}
}
`

func TestNothingACheckWritesSurvivesTheCall(t *testing.T) {
	e := newCfEnv(t, false)
	root, dir := fixture(t)
	logPath := filepath.Join(t.TempDir(), "written.log")
	write(t, dir, "go.mod", []byte("module example.com/p\n\ngo 1.26\n"))
	write(t, dir, "tests/writes_test.go", []byte(fmt.Sprintf(cfWriterTest, logPath)))
	t.Cleanup(func() { // never leave a marker behind, whatever the service did
		b, _ := os.ReadFile(logPath)
		for _, p := range strings.Fields(string(b)) {
			os.Remove(p)
		}
	})
	base := serve(t, checkConfig(t, root))
	cbody := func(limit int) map[string]any { return sleepCheckBody(t, dir, limit) }

	// A passing call, twice: the second call's test fails when anything the first wrote is there.
	e.mode("real")
	for i := 1; i <= 2; i++ {
		e.reset()
		passed, reason := cfVerdict(t, post(t, base, "/checks/AllTestsPass/run", cbody(120)))
		what := fmt.Sprintf("passing call %d", i)
		if !passed {
			t.Errorf("%s: AllTestsPass failed (a file an earlier call wrote survived, or the test could not write): %.600s", what, reason)
		}
		folder := e.oneFolder(what, e.records())
		wantGone(t, what, folder, 0)
		b, _ := os.ReadFile(logPath)
		paths := strings.Fields(string(b))
		if len(paths) != 3*i {
			t.Fatalf("%s: the test logged %d written paths, want %d: %q", what, len(paths), 3*i, paths)
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				t.Errorf("%s: %s, which a check's test wrote, still exists after the call", what, p)
			}
		}
		for _, p := range paths[3*(i-1):] {
			if folder != "" && !inside(p, folder) {
				t.Errorf("%s: the test's TMPDIR, HOME or GOCACHE file %s is outside the call's folder %s", what, p, folder)
			}
		}
	}

	// A failing call.
	e.reset()
	e.mode("fail")
	if passed, _ := cfVerdict(t, post(t, base, "/checks/AllTestsPass/run", cbody(120))); passed {
		t.Fatal("the fake go fails, so AllTestsPass must fail")
	}
	wantGone(t, "failing call", e.oneFolder("failing call", e.records()), 0)

	// A call stopped at its time limit.
	e.reset()
	e.mode("sleep")
	wantProblem(t, post(t, base, "/checks/AllTestsPass/run", cbody(3)), 504, "TimeLimitExceeded")
	wantGone(t, "timed-out call", e.oneFolder("timed-out call", e.records()), 5*time.Second)

	// A call whose request is cancelled while its command runs.
	e.reset()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		b, _ := json.Marshal(cbody(120))
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/checks/AllTestsPass/run?"+q, strings.NewReader(string(b)))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		r, err := http.DefaultClient.Do(req)
		if err == nil {
			r.Body.Close()
		}
		done <- err
	}()
	rs := e.waitRecords(1, 30*time.Second)
	if len(rs) == 0 {
		t.Fatal("the cancelled call's go command never started")
	}
	cancel()
	<-done
	wantGone(t, "cancelled call", e.oneFolder("cancelled call", rs), 15*time.Second)
}

// ---- 3. the module cache is not writable ----

const cfModcacheTest = `package tests

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCannotChangeTheModuleCache(t *testing.T) {
	m := os.Getenv("GOMODCACHE")
	if m == "" {
		t.Fatal("GOMODCACHE is not set for the test")
	}
	if err := os.WriteFile(filepath.Join(m, "t094-new"), []byte("x"), 0o644); err == nil {
		t.Errorf("CREATED a file in the module cache %%s", m)
	}
	if err := os.MkdirAll(filepath.Join(m, "t094-dir", "x"), 0o755); err == nil {
		t.Errorf("CREATED a folder in the module cache %%s", m)
	}
	if f, err := os.OpenFile(filepath.Join(m, %q), os.O_WRONLY|os.O_TRUNC, 0); err == nil {
		f.Close()
		t.Errorf("CHANGED a module's file in the module cache %%s", m)
	}
}
`

func TestTheModuleCacheIsNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("run as root, a read-only module cache can be written, so this cannot be judged: run the tests as a user (scripts/check.sh does)")
	}
	e := newCfEnv(t, false)
	// A module cache as the image has it: read-only, holding a module's file.
	modc := t.TempDir()
	const modFile = "example.com/ro@v1.0.0/ro.go"
	write(t, modc, modFile, []byte("package ro\n"))
	cfChmodTree(t, modc, 0o555, 0o444)
	t.Cleanup(func() { cfChmodTree(t, modc, 0o755, 0o644) })

	root, dir := fixture(t)
	write(t, dir, "go.mod", []byte("module example.com/p\n\ngo 1.26\n"))
	write(t, dir, "tests/modcache_test.go", []byte(fmt.Sprintf(cfModcacheTest, modFile)))
	before := snapshot(t, modc)
	c := checkConfig(t, root)
	c.Env = append(c.Env, "GOMODCACHE="+modc)
	base := serve(t, c)

	e.mode("real")
	passed, reason := cfVerdict(t, post(t, base, "/checks/AllTestsPass/run", sleepCheckBody(t, dir, 120)))
	if !passed {
		t.Errorf("the project's test could change the module cache, or the check judged it otherwise: %.600s", reason)
	}
	if d := sameSnapshot(before, snapshot(t, modc)); d != "" {
		t.Errorf("the module cache changed during the call: %s", d)
	}
	rs := e.records()
	folder := e.oneFolder("AllTestsPass", rs)
	for _, r := range rs {
		if got := r.vars["GOMODCACHE"]; got != modc {
			t.Errorf("%s: GOMODCACHE=%q, want the shared module cache %s", r, got, modc)
		}
		if folder != "" && inside(r.vars["GOMODCACHE"], folder) {
			t.Errorf("%s: the module cache is inside the call's folder %s, not shared", r, folder)
		}
	}
}

func cfChmodTree(t *testing.T, root string, dirMode, fileMode os.FileMode) {
	t.Helper()
	var dirs []string
	filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			dirs = append(dirs, p)
		} else {
			os.Chmod(p, fileMode)
		}
		return nil
	})
	// Deepest first when closing, shallowest first when opening, so the walk above stays possible.
	if dirMode&0o200 == 0 {
		sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	} else {
		sort.Strings(dirs)
	}
	for _, d := range dirs {
		os.Chmod(d, dirMode)
	}
}

// ---- 4. every go command of a call uses its folder ----

// cfGoChecks are the checks that run go, each with the fake mode its first commands need.
var cfGoChecks = []struct{ check, mode string }{
	{"GenerationRepeatable", "fail"},
	{"GeneratedCodeBuilds", "fail"},
	{"TestsBuild", "fail"},
	{"EveryExampleBound", "fail"},
	{"TestsFailAgainstStub", "fail"},
	{"AllTestsPass", "fail"},
	{"FindingsCarryEvidence", "witness"},
	{"NoBlockingFindingOpen", "witness"},
}

func cfCheckBody(t *testing.T, dir, check string) map[string]any {
	if check == "FindingsCarryEvidence" || check == "NoBlockingFindingOpen" {
		return witBody(t, dir, false, witFinding("R-1", witGreet, 23, witR1, witT1))
	}
	return checkBody(t, dir, step("code", codeWritable, []string{"code"}, nil, nil), nil, "c", "e")
}

func TestEveryGoCommandOfACallUsesItsFolder(t *testing.T) {
	e := newCfEnv(t, true)
	root, dir := fixture(t, "witness")
	c := checkConfig(t, root)
	c.OapiCodegen = e.oapi
	base := serve(t, c)
	seen := map[string]string{}
	use := func(what, folder string) {
		if folder == "" {
			return
		}
		if other, ok := seen[folder]; ok {
			t.Errorf("%s and %s used the same folder %s", other, what, folder)
		}
		seen[folder] = what
	}
	for _, gc := range cfGoChecks {
		e.reset()
		e.mode(gc.mode)
		if st, b := cfStatus(t, base, "/checks/"+gc.check+"/run", cfCheckBody(t, dir, gc.check)); st != 200 {
			t.Errorf("%s: %d %.300s", gc.check, st, b)
			continue
		}
		rs := e.records()
		if gc.mode == "witness" && len(rs) < 2 {
			t.Errorf("%s: the witness run and its baseline are two go commands, %d were recorded: %v", gc.check, len(rs), rs)
		}
		folder := e.oneFolder(gc.check, rs)
		wantGone(t, gc.check, folder, 0)
		use(gc.check, folder)
	}

	// runProcedure: generate runs oapi-codegen, then go (the fake fails `go mod vendor`).
	groot, gdir := fixture(t)
	w := workerConfig(t, groot, "generate", "generate")
	w.OapiCodegen = e.oapi
	wbase := serve(t, w)
	e.reset()
	e.mode("fail")
	if st, b := cfStatus(t, wbase, "/procedures/generate/run", generateBody(gdir, readTestdata(t, "contract.yaml"), 120)); st != 200 {
		t.Errorf("runProcedure generate: %d %.300s", st, b)
	}
	rs := e.records()
	var tools []string
	for _, r := range rs {
		tools = append(tools, r.tool)
	}
	if !strings.Contains(strings.Join(tools, " "), "oapi-codegen") || !strings.Contains(strings.Join(tools, " "), "go") {
		t.Errorf("runProcedure generate: want oapi-codegen and go recorded, got %v", tools)
	}
	folder := e.oneFolder("runProcedure generate", rs)
	wantGone(t, "runProcedure generate", folder, 0)
	use("runProcedure generate", folder)

	for f := range seen {
		for _, old := range cfOld {
			if inside(f, old) || inside(old, f) {
				t.Errorf("the call folder %s overlaps the shared %s", f, old)
			}
		}
	}
}

// ---- 5. every go test carries -count=1 ----

func TestEveryGoTestCarriesCount1(t *testing.T) {
	e := newCfEnv(t, false)
	root, dir := fixture(t, "witness")
	base := serve(t, checkConfig(t, root))
	for _, gc := range cfGoChecks {
		if gc.check == "GenerationRepeatable" || gc.check == "GeneratedCodeBuilds" {
			continue // they run no go test
		}
		e.reset()
		e.mode(gc.mode)
		if st, b := cfStatus(t, base, "/checks/"+gc.check+"/run", cfCheckBody(t, dir, gc.check)); st != 200 {
			t.Errorf("%s: %d %.300s", gc.check, st, b)
			continue
		}
		tests := 0
		for _, r := range e.records() {
			if r.tool != "go" || len(r.args) == 0 || r.args[0] != "test" {
				continue
			}
			tests++
			found := false
			for _, a := range r.args {
				if a == "-count=1" {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: `%s` has no -count=1, so a cached result could answer it", gc.check, r)
			}
		}
		want := 1
		if gc.mode == "witness" {
			want = 2
		}
		if tests < want {
			t.Errorf("%s: %d go test commands were recorded, want at least %d", gc.check, tests, want)
		}
	}
}
