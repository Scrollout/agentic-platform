// T054e: failing tests, by a separate author, for the check service (package checksvc, T054c
// builds cmd/check on it), written against the contract in
// src/check/api/preview/2026-11-13-preview/openapi.yaml and over HTTP only: checksvc.NewHandler
// is the one function the tests call. The requests are built as plain JSON maps, not with the
// generated client, so a drift in the generated types cannot hide a fault.
//
// Real Go runs are offline: the config's Env has GOPROXY=off and GOWORK=off. The toolchain on PATH
// and the module cache (godog v0.16.0, oapi-codegen runtime) must be there, else the tests fail
// (never skip). oapi-codegen is found as in package procedures' tests: AP_OAPI_CODEGEN (a path),
// else built once with `go install <pin from src/deploy/images.lock>` offline from the module cache.
package checksvc_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/agentic-platform/engine/checksvc"
)

const (
	token   = "test-engine-token-1"
	version = "2026-11-13-preview"
	q       = "api-version=" + version
)

var tempDirs struct {
	sync.Mutex
	list []string
}

func TestMain(m *testing.M) {
	code := m.Run()
	for _, d := range tempDirs.list {
		os.RemoveAll(d)
	}
	os.Exit(code)
}

func sharedTemp(prefix string) (string, error) {
	d, err := os.MkdirTemp("", prefix)
	if err == nil {
		tempDirs.Lock()
		tempDirs.list = append(tempDirs.list, d)
		tempDirs.Unlock()
	}
	return d, err
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// ---- configs and servers ----

func goEnv() []string {
	env := []string{"GOPROXY=off", "GOWORK=off", "CGO_ENABLED=0"}
	if m := os.Getenv("GOMODCACHE"); m != "" {
		env = append(env, "GOMODCACHE="+m)
	}
	return env
}

func templateDir(t testing.TB) string {
	abs, err := filepath.Abs(filepath.Join("testdata", "template"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// checkConfig is the check-service deployment over the projects folder root.
func checkConfig(t testing.TB, root string) checksvc.Config {
	return checksvc.Config{Token: token, Project: root, Env: goEnv(), Template: templateDir(t)}
}

// workerConfig is a procedure step's worker deployment.
func workerConfig(t testing.TB, root, procedure, step string, env ...string) checksvc.Config {
	c := checkConfig(t, root)
	c.Procedure, c.Step = procedure, step
	c.Env = append(c.Env, env...)
	return c
}

func serve(t testing.TB, c checksvc.Config) string {
	ts := httptest.NewServer(checksvc.NewHandler(c))
	t.Cleanup(ts.Close)
	return ts.URL
}

// ---- oapi-codegen ----

var (
	oapiOnce sync.Once
	oapiBin  string
	oapiErr  error
)

func oapiCodegen() (string, error) {
	if p := os.Getenv("AP_OAPI_CODEGEN"); p != "" {
		return p, nil
	}
	oapiOnce.Do(func() {
		lock, err := os.ReadFile(filepath.Join("..", "..", "deploy", "images.lock"))
		if err != nil {
			oapiErr = err
			return
		}
		var pin string
		for _, l := range strings.Split(string(lock), "\n") {
			if f := strings.Fields(l); len(f) == 2 && f[0] == "oapi-codegen" {
				pin = f[1]
			}
		}
		if pin == "" {
			oapiErr = errors.New("no oapi-codegen pin in src/deploy/images.lock")
			return
		}
		dir, err := sharedTemp("oapi-codegen-bin")
		if err != nil {
			oapiErr = err
			return
		}
		cmd := exec.Command("go", "install", pin)
		cmd.Env = append(os.Environ(), "GOBIN="+dir, "GOPROXY=off", "GOWORK=off", "GOFLAGS=-mod=mod")
		if out, err := cmd.CombinedOutput(); err != nil {
			oapiErr = fmt.Errorf("go install %s (offline): %v\n%s", pin, err, out)
			return
		}
		oapiBin = filepath.Join(dir, "oapi-codegen")
	})
	return oapiBin, oapiErr
}

func needOapi(t testing.TB) string {
	t.Helper()
	p, err := oapiCodegen()
	if err != nil {
		t.Fatalf("oapi-codegen is not available offline: %v", err)
	}
	return p
}

// withOapi is a config that can generate and run `go generate`.
func withOapi(t testing.TB, c checksvc.Config) checksvc.Config {
	c.OapiCodegen = needOapi(t)
	return c
}

// ---- files ----

func readTestdata(t testing.TB, parts ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"testdata"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func write(t testing.TB, dir, rel string, content []byte) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t testing.TB, dir, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func copyTree(t testing.TB, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// fixture copies testdata/<parts...> into a new project folder "p1" of a new projects root.
func fixture(t testing.TB, parts ...string) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, "p1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if len(parts) > 0 {
		copyTree(t, filepath.Join(append([]string{"testdata"}, parts...)...), dir)
	}
	return root, dir
}

type pf struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// listFiles is every regular file of dir by path in byte order, never following a link.
func listFiles(t testing.TB, dir string) []pf {
	t.Helper()
	out := []pf{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(dir, p)
			out = append(out, pf{filepath.ToSlash(rel), sum(b)})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// snapshot describes every entry under dir: its kind, and its hash or link target.
func snapshot(t testing.TB, dir string) map[string]string {
	t.Helper()
	m := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case d.IsDir():
			m[rel] = "dir"
		case d.Type().IsRegular():
			b, _ := os.ReadFile(p)
			m[rel] = "file:" + sum(b)
		case d.Type()&fs.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			m[rel] = "link:" + l
		default:
			m[rel] = "other"
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func sameSnapshot(a, b map[string]string) string {
	for k, v := range a {
		if w, ok := b[k]; !ok {
			return k + " was removed"
		} else if v != w {
			return k + " changed"
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			return k + " was added"
		}
	}
	return ""
}

// framed is the contract's framed form, written out here independently of canonical.Framed.
func framed(files map[string][]byte) []byte {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b bytes.Buffer
	for _, p := range paths {
		fmt.Fprintf(&b, "%s\n%d\n", p, len(files[p]))
		b.Write(files[p])
	}
	return b.Bytes()
}

// matches is the catalogue's glob dialect: `dir/**` is every path below dir, anything else one path.
func matches(entries []string, p string) bool {
	for _, e := range entries {
		if strings.HasSuffix(e, "/**") {
			if strings.HasPrefix(p, strings.TrimSuffix(e, "**")) {
				return true
			}
		} else if e == p {
			return true
		}
	}
	return false
}

// stepFiles is the files of dir matching entries, by path.
func stepFiles(t testing.TB, dir string, entries []string) map[string][]byte {
	t.Helper()
	m := map[string][]byte{}
	for _, f := range listFiles(t, dir) {
		if matches(entries, f.Path) {
			m[f.Path] = read(t, dir, f.Path)
		}
	}
	return m
}

// ---- request bodies ----

func step(name string, writable, outputs, allowed, forbidden []string) map[string]any {
	nz := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}
	return map[string]any{
		"name": name, "writable": nz(writable), "outputs": nz(outputs),
		"dependencies": map[string]any{"allowed": nz(allowed), "forbidden": nz(forbidden)},
	}
}

func common(project string, st map[string]any, before []pf) map[string]any {
	if before == nil {
		before = []pf{}
	}
	return map[string]any{
		"run": "RUN-1", "step": st, "attempt": 1, "project": project, "before": before,
		"time_limit_seconds": 120,
	}
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// part is an approved part of the slice: canonical JSON (ASCII, sorted keys: json.Marshal of a
// map gives the RFC 8785 form here) with its hash.
func part(name string, v map[string]any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return map[string]any{"part": name, "sha256": sum(b), "content": b64(b)}
}

func approvedParts(contract, examples string) []any {
	return []any{
		part("goals", map[string]any{"goals": []any{map[string]any{"id": "G-1", "text": "Greet people"}}}),
		part("rules", map[string]any{"rules": []any{map[string]any{"id": "R-1", "text": "A greeting names the person"}}, "examples": examples, "terms": []any{}}),
		part("contract", map[string]any{"contract": contract}),
	}
}

// outputsFor are the attempt's outputs by hash as the folder holds them (contract, Artifacts).
func outputsFor(t testing.TB, dir string, st map[string]any) []any {
	t.Helper()
	writable := st["writable"].([]string)
	names := st["outputs"].([]string)
	out := []any{}
	for i, n := range names {
		entries := writable
		if len(names) > 1 {
			entries = writable[i : i+1]
		}
		h := sum(framed(stepFiles(t, dir, entries)))
		if len(writable) == 0 {
			h = sum(nil)
		}
		out = append(out, map[string]any{"name": n, "sha256": h})
	}
	return out
}

// checkBody is a runCheck body for the folder as it stands now.
func checkBody(t testing.TB, dir string, st map[string]any, before []pf, contract, examples string) map[string]any {
	b := common(filepath.Base(dir), st, before)
	b["approved"] = approvedParts(contract, examples)
	b["earlier_steps"] = []any{}
	b["inputs"] = []any{}
	b["outputs"] = outputsFor(t, dir, st)
	return b
}

func clone(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

// ---- HTTP ----

type resp struct {
	status int
	header http.Header
	body   []byte
}

func do(t testing.TB, base, path, query, auth, ctype string, body []byte) resp {
	t.Helper()
	u := base + path
	if query != "" {
		u += "?" + query
	}
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp{r.StatusCode, r.Header, b}
}

func post(t testing.TB, base, path string, body any) resp {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return do(t, base, path, q, "Bearer "+token, "application/json", b)
}

type problem struct {
	Type              string   `json:"type"`
	Title             string   `json:"title"`
	Status            int      `json:"status"`
	Detail            string   `json:"detail"`
	Code              string   `json:"code"`
	SupportedVersions []string `json:"supported_versions"`
}

func wantProblem(t testing.TB, r resp, status int, code string) problem {
	t.Helper()
	var p problem
	if err := json.Unmarshal(r.body, &p); err != nil {
		t.Fatalf("want %d %s, got %d with a body that is not a problem: %.300s", status, code, r.status, r.body)
	}
	if r.status != status || p.Code != code {
		t.Fatalf("want %d %s, got %d %s: %.400s", status, code, r.status, p.Code, r.body)
	}
	if ct := r.header.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("a problem must be application/problem+json, got %q", ct)
	}
	if p.Type == "" || p.Title == "" {
		t.Errorf("a problem needs a type and a title: %s", r.body)
	}
	return p
}

type verdict struct {
	Check        string   `json:"check"`
	Passed       bool     `json:"passed"`
	Reason       string   `json:"reason"`
	Refs         []string `json:"refs"`
	ProjectFiles []pf     `json:"project_files"`
}

func strictJSON(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

func wantVerdict(t testing.TB, r resp, check string) verdict {
	t.Helper()
	if r.status != 200 {
		t.Fatalf("%s: want 200, got %d: %.600s", check, r.status, r.body)
	}
	var v verdict
	if err := strictJSON(r.body, &v); err != nil {
		t.Fatalf("%s: the response is not a CheckVerdict: %v: %.300s", check, err, r.body)
	}
	if v.Check != check || v.Reason == "" || v.Refs == nil || v.ProjectFiles == nil {
		t.Fatalf("%s: check, reason, refs and project_files must all be set: %.400s", check, r.body)
	}
	return v
}

func runCheck(t testing.TB, base, check string, body map[string]any) verdict {
	t.Helper()
	return wantVerdict(t, post(t, base, "/checks/"+check+"/run", body), check)
}

// noHost fails when a reason names a path outside the project folder.
func noHost(t testing.TB, reason string, hosts ...string) {
	t.Helper()
	cwd, _ := os.Getwd()
	for _, h := range append(hosts, cwd, os.TempDir()+"/") {
		if h != "" && strings.Contains(reason, h) {
			t.Errorf("the reason names the host path %q, it must name project paths only: %.600s", h, reason)
		}
	}
}

func execIn(dir string, env []string, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append(goEnv(), env...)...)
	return cmd
}
