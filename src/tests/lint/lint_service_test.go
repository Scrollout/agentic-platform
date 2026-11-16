//go:build docker

package lint

// The Spectral lint service's test suite (T038e, research D28). It starts src/deploy/lint/server.mjs
// in the pinned node image, hardened as the platform runs it (gVisor runsc when Docker lists it,
// read-only, user 65532, all capabilities dropped, no-new-privileges, internal network only), and
// drives it from a second container on that network. A missing Docker is a failure, never a skip.
// Needs the pinned Node tools (scripts/npm-tool.sh installs them on first use); no model key, no
// egress. The service is started with TOOLS=/tools (the pinned node_modules' parent, read-only)
// and LINT_TOKEN; its code is mounted read-only at /app/server.mjs.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const token = "test-token-0123456789"

var seq atomic.Int64

func uniq(p string) string { return fmt.Sprintf("%s-%d-%d", p, os.Getpid(), seq.Add(1)) }

func runIn(stdin, name string, args ...string) (string, int) {
	cmd := exec.Command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		code = -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	return strings.TrimSpace(string(out)), code
}

func run(name string, args ...string) (string, int) { return runIn("", name, args...) }

func docker(t *testing.T, args ...string) string {
	t.Helper()
	out, code := run("docker", args...)
	if code != 0 {
		t.Fatalf("docker %s: exit %d: %s", strings.Join(args, " "), code, out)
	}
	return out
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func repo(t *testing.T) string {
	t.Helper()
	d, err := filepath.Abs("../../..")
	must(t, err)
	return d
}

func nodeImage(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo(t), "src/deploy/images.lock"))
	must(t, err)
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "node" {
			return f[1]
		}
	}
	t.Fatal("no node pin in images.lock")
	return ""
}

// toolsDir makes sure the pinned Node tools are installed and returns the folder holding node_modules.
func toolsDir(t *testing.T) string {
	t.Helper()
	r := repo(t)
	if out, code := run("sh", filepath.Join(r, "scripts/npm-tool.sh"), "spectral", "--version"); code != 0 {
		t.Fatalf("npm-tool.sh spectral --version: exit %d: %s", code, out)
	}
	pkg, _ := os.ReadFile(filepath.Join(r, "scripts/tools/package.json"))
	lock, _ := os.ReadFile(filepath.Join(r, "scripts/tools/package-lock.json"))
	sum, _ := runIn(string(pkg)+string(lock), "sh", "-c", "sha256sum | cut -c1-16")
	sum = strings.Fields(sum)[0]
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		cache = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	d := filepath.Join(cache, "agentic-platform/node", sum)
	if _, err := os.Stat(filepath.Join(d, "node_modules")); err != nil {
		t.Fatalf("pinned tools not found at %s: %v", d, err)
	}
	return d
}

func runtimeArgs() []string {
	out, _ := run("docker", "info", "--format", "{{json .Runtimes}}")
	if strings.Contains(out, `"runsc"`) {
		return []string{"--runtime", "runsc"}
	}
	return nil
}

type lab struct {
	net, name, img string
}

func start(t *testing.T) lab {
	t.Helper()
	if out, code := run("docker", "info", "--format", "{{.ServerVersion}}"); code != 0 {
		t.Fatalf("Docker is required and is not available (no skip): %s", out)
	}
	r := repo(t)
	img := nodeImage(t)
	tools := toolsDir(t)
	n := uniq("aptest-lint-net")
	docker(t, "network", "create", "--internal", "-o", "com.docker.network.bridge.gateway_mode_ipv4=isolated", n)
	t.Cleanup(func() { run("docker", "network", "rm", n) })
	name := uniq("aptest-lint")
	args := []string{"run", "-d", "--name", name, "--network", n}
	args = append(args, runtimeArgs()...)
	args = append(args, "--read-only", "--user", "65532:65532", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "--tmpfs", "/tmp",
		"-v", filepath.Join(r, "src/deploy/lint/server.mjs")+":/app/server.mjs:ro",
		"-v", tools+":/tools:ro",
		"-e", "TOOLS=/tools", "-e", "LINT_TOKEN="+token, "-e", "HOME=/tmp",
		img, "node", "/app/server.mjs")
	t.Cleanup(func() { run("docker", "rm", "-f", name) })
	docker(t, args...)
	l := lab{n, name, img}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := l.req("GET", "/health", "", ""); st == 200 {
			return l
		}
		time.Sleep(300 * time.Millisecond)
	}
	logs, _ := run("docker", "logs", name)
	t.Fatalf("lint service did not become healthy: %s", logs)
	return l
}

// clientJS sends one request: argv = method, url, auth header value ("" none); stdin = body.
// It prints "<status>\n<body>".
const clientJS = `
const [m,u,a]=process.argv.slice(1);
let b=[];process.stdin.on("data",c=>b.push(c)).on("end",async()=>{
 const body=Buffer.concat(b);
 const h={};if(a)h.authorization=a;
 try{const r=await fetch(u,{method:m,headers:h,body:m==="GET"||m==="HEAD"?undefined:body});
 console.log(r.status);console.log(await r.text());}catch(e){console.log("ERR "+e.message)}
});`

// req sends a request from a separate container on the same network, as the engine would.
func (l lab) req(method, path, auth, body string) (int, string) {
	out, code := runIn(body, "docker", "run", "--rm", "-i", "--network", l.net, "--entrypoint", "node", l.img,
		"-e", clientJS, method, "http://"+l.name+":8080"+path, auth)
	if code != 0 {
		return -1, out
	}
	first, rest, _ := strings.Cut(out, "\n")
	var s int
	if _, err := fmt.Sscan(first, &s); err != nil {
		return -1, out
	}
	return s, rest
}

type finding struct{ Level, Code, Text, Path string }

func (l lab) lint(t *testing.T, contract string) []finding {
	t.Helper()
	st, body := l.req("POST", "/lint", "Bearer "+token, contract)
	if st != 200 {
		t.Fatalf("POST /lint = %d: %s", st, body)
	}
	var fs []finding
	if err := json.Unmarshal([]byte(body), &fs); err != nil {
		t.Fatalf("body is not a finding list: %v: %s", err, body)
	}
	return fs
}

func errorsOf(fs []finding) []finding {
	var e []finding
	for _, f := range fs {
		if f.Level == "error" {
			e = append(e, f)
		}
	}
	return e
}

func goodContract(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo(t), "src/tests/evals/analyst/testdata/proposals/good-notes.json"))
	must(t, err)
	var d struct{ Contract string }
	must(t, json.Unmarshal(b, &d))
	if d.Contract == "" {
		t.Fatal("no contract in good-notes.json")
	}
	return d.Contract
}

func TestHealthIsOpen(t *testing.T) {
	l := start(t)
	st, body := l.req("GET", "/health", "", "")
	if st != 200 {
		t.Fatalf("GET /health without a token = %d: %s", st, body)
	}
}

func TestExampleContractHasNoErrors(t *testing.T) {
	l := start(t)
	fs := l.lint(t, goodContract(t))
	if e := errorsOf(fs); len(e) != 0 {
		t.Fatalf("the example contract gave errors under spectral:oas: %+v", e)
	}
	for _, f := range fs {
		if f.Code == "" || f.Text == "" {
			t.Errorf("finding without code or text: %+v", f)
		}
		switch f.Level {
		case "error", "warn", "info", "hint":
		default:
			t.Errorf("unknown level %q", f.Level)
		}
	}
}

func TestSpectralOASErrorNamesTheRule(t *testing.T) {
	l := start(t)
	bad := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n  /notes/{id}:\n    get:\n      operationId: getNote\n      responses: {'200': {description: ok}}\n"
	e := errorsOf(l.lint(t, bad))
	if len(e) == 0 {
		t.Fatal("an undeclared path parameter gave no error finding")
	}
	found := false
	for _, f := range e {
		if f.Code == "path-params" {
			found = true
		}
	}
	if !found {
		t.Errorf("no error with code path-params: %+v", e)
	}
}

func TestJSONContractIsAccepted(t *testing.T) {
	l := start(t)
	doc := `{"openapi":"3.1.0","info":{"title":"t","version":"1"},"paths":{"/a/{id}":{"get":{"operationId":"g","responses":{"200":{"description":"ok"}}}}}}`
	if len(errorsOf(l.lint(t, doc))) == 0 {
		t.Fatal("a JSON contract with an undeclared path parameter gave no error")
	}
}

// TestExternalRefsAreErrorsAndNothingIsFetched proves the closed resolver: each external $ref is an
// error finding, and a recorder on the same network got no request.
func TestExternalRefsAreErrorsAndNothingIsFetched(t *testing.T) {
	l := start(t)
	rec := uniq("aptest-recorder")
	t.Cleanup(func() { run("docker", "rm", "-f", rec) })
	docker(t, "run", "-d", "--name", rec, "--network", l.net, "--entrypoint", "node", l.img, "-e",
		`require("http").createServer((q,s)=>{console.log("REQUEST "+q.method+" "+q.url);s.end("{}")}).listen(8080)`)
	for i := 0; i < 50; i++ {
		if st, _ := l.req("GET", "/health", "", ""); st == 200 {
			break
		}
	}
	time.Sleep(time.Second)
	refs := []string{
		"file:///etc/passwd",
		"../../../etc/passwd",
		"/etc/passwd",
		"other.yaml#/components/schemas/X",
		"http://" + rec + ":8080/secret.yaml",
		"https://" + rec + ":8080/secret.yaml",
	}
	for _, ref := range refs {
		doc := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n  /a:\n    get:\n      operationId: g\n      responses:\n        '200':\n          description: ok\n          content:\n            application/json:\n              schema:\n                $ref: '" + ref + "'\n"
		fs := l.lint(t, doc)
		if len(errorsOf(fs)) == 0 {
			t.Errorf("$ref %q gave no error finding: %+v", ref, fs)
		}
	}
	logs, _ := run("docker", "logs", rec)
	if strings.Contains(logs, "REQUEST") {
		t.Errorf("the recorder received a request, so a $ref was fetched:\n%s", logs)
	}
	// The service container must not have read a local file either: its answer carries no file content.
	st, body := l.req("POST", "/lint", "Bearer "+token, "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents: {schemas: {X: {$ref: 'file:///etc/passwd'}}}\n")
	if st != 200 || strings.Contains(body, "root:") {
		t.Errorf("status %d, body leaks a local file: %s", st, body)
	}
}

func TestAuth(t *testing.T) {
	l := start(t)
	good := goodContract(t)
	for name, auth := range map[string]string{
		"none":         "",
		"wrong":        "Bearer nope",
		"wrong scheme": "Basic " + token,
		"bare token":   token,
		"prefix":       "Bearer " + token[:5],
		"extra":        "Bearer " + token + "x",
	} {
		if st, body := l.req("POST", "/lint", auth, good); st != 401 {
			t.Errorf("%s: POST /lint = %d, want 401: %s", name, st, body)
		}
	}
	if st, _ := l.req("POST", "/lint", "Bearer "+token, good); st != 200 {
		t.Errorf("right token = %d, want 200", st)
	}
}

func TestBodyOverOneMiBIs413(t *testing.T) {
	l := start(t)
	if st, body := l.req("POST", "/lint", "Bearer "+token, strings.Repeat("a", 1<<20+1)); st != 413 {
		t.Errorf("1 MiB + 1 byte = %d, want 413: %.200s", st, body)
	}
	// The limit is on size, not on content: exactly 1 MiB of a valid document is still linted.
	pad := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n#"
	doc := pad + strings.Repeat("a", 1<<20-len(pad))
	if st, body := l.req("POST", "/lint", "Bearer "+token, doc); st != 200 {
		t.Errorf("exactly 1 MiB = %d, want 200: %.200s", st, body)
	}
}

func TestMethodsAndPaths(t *testing.T) {
	l := start(t)
	auth := "Bearer " + token
	for _, m := range []string{"GET", "PUT", "DELETE", "PATCH"} {
		if st, body := l.req(m, "/lint", auth, "x"); st != 405 {
			t.Errorf("%s /lint = %d, want 405: %s", m, st, body)
		}
	}
	for _, p := range []string{"/", "/lint/", "/lints", "/admin", "/health/x"} {
		if st, body := l.req("POST", p, auth, "x"); st != 404 {
			t.Errorf("POST %s = %d, want 404: %s", p, st, body)
		}
	}
	if st, _ := l.req("POST", "/health", auth, "x"); st != 405 && st != 404 {
		t.Errorf("POST /health = %d, want 405 or 404", st)
	}
}

func TestHardenedContainerRuns(t *testing.T) {
	l := start(t)
	out := docker(t, "inspect", "--format",
		"{{.HostConfig.ReadonlyRootfs}} {{.Config.User}} {{.HostConfig.CapDrop}} {{.HostConfig.SecurityOpt}}", l.name)
	for _, w := range []string{"true", "65532", "ALL", "no-new-privileges"} {
		if !strings.Contains(out, w) {
			t.Errorf("container config %q lacks %q", out, w)
		}
	}
	if rt := runtimeArgs(); rt != nil {
		if got := docker(t, "inspect", "--format", "{{.HostConfig.Runtime}}", l.name); got != "runsc" {
			t.Errorf("runtime = %q, want runsc", got)
		}
	}
}
