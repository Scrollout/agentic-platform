//go:build docker

// Package lab starts the platform's slice path on one internal Docker network, for the contract
// tests (T030) and the acceptance tests (T031): the recorded-model replay server as `gateway`, the
// analyst (Docker Agent `serve api`) on it, the contract-lint service, and the engine built from
// source. No key, no egress, no model cost. A missing Docker is a failure, never a skip.
//
// The host cannot dial a container on an isolated internal network, so requests go through a
// small client container on the same network (`docker exec`), and Schemathesis runs in a container
// on it. Every service is addressed by IP, as the platform's gVisor runtime has no Docker DNS.
package lab

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// APIVersion is the engine contract's version.
const APIVersion = "2026-11-12-preview"

// Example is the quickstart's description (step 2), which the `analyst-example-notes` recording answers.
const Example = "An in-memory notes service. Listing returns all notes in creation order. " +
	"Creating a note needs non-empty text and returns it with an id starting at 1."

// The five tokens the lab gives the engine and its callees; all differ.
const (
	ClientToken = "lab-client-token-0123456789"
	PageToken   = "lab-page-token-0123456789"
	WorkerToken = "lab-worker-token-0123456789"
	lintToken   = "lab-lint-token-0123456789"
	analystTok  = "lab-analyst-token-0123456789"
)

var seq atomic.Int64

func uniq(p string) string { return fmt.Sprintf("%s-%d-%d", p, os.Getpid(), seq.Add(1)) }

// Run runs a command and returns its combined output and exit code.
func Run(name string, args ...string) (string, int) {
	out, err := exec.Command(name, args...).CombinedOutput()
	code := 0
	if err != nil {
		code = -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	return strings.TrimSpace(string(out)), code
}

// Docker runs a docker command and fails the test unless it exits 0.
func Docker(t testing.TB, args ...string) string {
	t.Helper()
	out, code := Run("docker", args...)
	if code != 0 {
		t.Fatalf("docker %s: exit %d: %s", strings.Join(args, " "), code, out)
	}
	return out
}

// Root is the repository's root, found from the test's working directory (src/tests/<package>).
func Root(t testing.TB) string {
	t.Helper()
	d, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Pin is the image reference of name in src/deploy/images.lock.
func Pin(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(Root(t), "src", "deploy", "images.lock"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == name {
			return f[1]
		}
	}
	t.Fatalf("no %s pin in images.lock", name)
	return ""
}

func runtimeArgs() []string {
	out, _ := Run("docker", "info", "--format", "{{json .Runtimes}}")
	if strings.Contains(out, `"runsc"`) {
		return []string{"--runtime", "runsc"}
	}
	return nil
}

func hardened() []string {
	return []string{"--read-only", "--user", "65532:65532", "--cap-drop", "ALL", "--security-opt", "no-new-privileges"}
}

// Lab is a running lab.
type Lab struct {
	Net      string
	EngineIP string
	Engine   string // the engine container's name
	client   string // the client container's name
	gateway  string
	root     string

	// With Build(): the check service's and the project worker's containers and addresses.
	Check, ProjectWorker string
	checkIP, projectIP   string
	// The host folders of the projects, the engine's store and the step worker grants.
	projects, store, grants string
}

// BaseURL is the engine's address on the lab network.
func (l *Lab) BaseURL() string { return "http://" + l.EngineIP + ":8080" }

func ip(t testing.TB, container, network string) string {
	t.Helper()
	return Docker(t, "inspect", "-f", fmt.Sprintf("{{(index .NetworkSettings.Networks %q).IPAddress}}", network), container)
}

func build(t testing.TB, dir, out, pkg string) {
	t.Helper()
	root := Root(t)
	cache := filepath.Join(cacheHome(), "agentic-platform", "go") // shared with scripts/check.sh
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	// The pinned Go image, offline, as scripts/check.sh go_run runs it; dir is mounted at /out.
	b, err := exec.Command("docker", "run", "--rm", "--network", "none",
		"-v", filepath.Join(root, "src")+":/src", "-w", "/src/tests",
		"-v", cache+":/gopath", "-v", dir+":/out",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-e", "GOPATH=/gopath", "-e", "GOCACHE=/gopath/cache", "-e", "GOTOOLCHAIN=local",
		"-e", "GOPROXY=off", "-e", "GOFLAGS=-mod=readonly", "-e", "CGO_ENABLED=0", "-e", "GOOS=linux",
		Pin(t, "golang"), "go", "build", "-trimpath", "-ldflags=-s -w", "-o", "/out/"+out, pkg).CombinedOutput()
	if err != nil {
		t.Fatalf("go build %s: %v: %s", pkg, err, b)
	}
}

func cacheHome() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return d
	}
	return filepath.Join(os.Getenv("HOME"), ".cache")
}

// toolsDir makes sure the pinned Node tools are installed and returns the folder holding node_modules.
func toolsDir(t testing.TB) string {
	t.Helper()
	r := Root(t)
	if out, code := Run("sh", filepath.Join(r, "scripts/npm-tool.sh"), "spectral", "--version"); code != 0 {
		t.Fatalf("npm-tool.sh spectral --version: exit %d: %s", code, out)
	}
	pkg, _ := os.ReadFile(filepath.Join(r, "scripts/tools/package.json"))
	lock, _ := os.ReadFile(filepath.Join(r, "scripts/tools/package-lock.json"))
	cmd := exec.Command("sh", "-c", "sha256sum | cut -c1-16")
	cmd.Stdin = strings.NewReader(string(pkg) + string(lock))
	sum, _ := cmd.Output()
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		cache = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	d := filepath.Join(cache, "agentic-platform/node", strings.Fields(string(sum))[0])
	if _, err := os.Stat(filepath.Join(d, "node_modules")); err != nil {
		t.Fatalf("pinned tools not found at %s: %v", d, err)
	}
	return d
}

func (l *Lab) replay(t testing.TB, args ...string) string {
	t.Helper()
	out, err := exec.Command("sh", append([]string{filepath.Join(l.root, "src/tests/recorded/replay.sh")}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("replay.sh %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// Start builds and starts the lab and waits until the engine answers. Everything is removed when t
// ends; what a lab of an ended process left behind is removed first.
func Start(t *testing.T, opts ...Option) *Lab {
	t.Helper()
	if out, code := Run("docker", "info", "--format", "{{.ServerVersion}}"); code != 0 {
		t.Fatalf("Docker is required and is not available (no skip): %s", out)
	}
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	sweep(t)
	root := Root(t)
	l := &Lab{root: root, projects: t.TempDir(), store: t.TempDir(), grants: t.TempDir()}
	os.Chmod(l.projects, 0o755) // the check service reads it as 65532
	l.Net = uniq("aptest-lab-net")
	Docker(t, append(append([]string{"network", "create", "--internal", "-o", "com.docker.network.bridge.gateway_mode_ipv4=isolated"}, labelArgs()...), l.Net)...)
	t.Cleanup(func() { Run("docker", "network", "rm", l.Net) })

	bin := t.TempDir()
	os.Chmod(bin, 0o755)
	build(t, bin, "engine", "github.com/agentic-platform/engine/cmd/engine")
	build(t, bin, "httpc", "./lab/httpc")
	tools := toolsDir(t)
	rt := runtimeArgs()
	alpine, node := Pin(t, "alpine"), Pin(t, "node")

	// The replay server, as `gateway`.
	l.gateway = l.replay(t, "start", l.Net, uniq("aptest-lab-replay"))
	t.Cleanup(func() { Run("sh", filepath.Join(root, "src/tests/recorded/replay.sh"), "stop", l.gateway) })

	start := func(name string, args ...string) {
		a := append([]string{"run", "-d", "--name", name, "--network", l.Net}, labelArgs()...)
		a = append(a, rt...)
		a = append(a, hardened()...)
		a = append(a, args...)
		t.Cleanup(func() { Run("docker", "rm", "-f", name) })
		Docker(t, a...)
	}

	// The client container, which the host's requests go through.
	l.client = uniq("aptest-lab-client")
	start(l.client, "-v", bin+":/c:ro", "--entrypoint", "sleep", alpine, "100000")

	analyst := uniq("aptest-lab-analyst")
	start(analyst, "--memory", "1g", "--pids-limit", "512", "--tmpfs", "/tmp:uid=65532,gid=65532",
		"-e", "HOME=/tmp/home", "-e", "TELEMETRY_ENABLED=false",
		"-e", "MODEL_GATEWAY_URL=http://"+ip(t, l.gateway, l.Net)+":8080",
		"-e", "ANTHROPIC_API_KEY=placeholder-not-a-key",
		"-v", filepath.Join(root, "src", "agents")+":/agents:ro",
		Pin(t, "docker-agent"), "serve", "api", "/agents/analyst.yaml",
		"--listen", "0.0.0.0:8080", "--auth-token", analystTok, "--session-db", "/tmp/session.db")

	lint := uniq("aptest-lab-lint")
	start(lint, "--tmpfs", "/tmp",
		"-v", filepath.Join(root, "src/deploy/lint/server.mjs")+":/app/server.mjs:ro",
		"-v", tools+":/tools:ro",
		"-e", "TOOLS=/tools", "-e", "LINT_TOKEN="+lintToken, "-e", "HOME=/tmp",
		node, "node", "/app/server.mjs")

	// Without Build() nothing listens at the check service's and project worker's addresses,
	// which Story 1 never calls.
	checkURL, projectURL := "http://check.invalid:8080", "http://project-worker.invalid:8080"
	if o.build {
		l.startServices(t, rt)
		checkURL, projectURL = "http://"+l.checkIP+":8080", "http://"+l.projectIP+":8080"
	}

	// The engine runs as the developer (the later --user wins over hardened's), so it writes its
	// store, a host folder that outlives a restart, and reads the grant files the lab writes.
	l.Engine = uniq("aptest-lab-engine")
	start(l.Engine, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-v", l.store+":/store", "-v", l.grants+":/grants:ro", "-v", bin+":/e:ro",
		"-e", "ENGINE_LISTEN_ADDRESS=:8080",
		"-e", "ENGINE_CLIENT_TOKEN="+ClientToken, "-e", "ENGINE_PAGE_TOKEN="+PageToken, "-e", "ENGINE_WORKER_TOKEN="+WorkerToken,
		"-e", "AP_LINT_URL=http://"+ip(t, lint, l.Net)+":8080", "-e", "AP_LINT_TOKEN="+lintToken,
		"-e", "AP_ANALYST_URL=http://"+ip(t, analyst, l.Net)+":8080", "-e", "AP_ANALYST_TOKEN="+analystTok,
		"-e", "AP_CHECK_URL="+checkURL, "-e", "AP_CHECK_TOKEN="+CheckToken,
		"-e", "AP_PROJECT_URL="+projectURL, "-e", "AP_PROJECT_TOKEN="+ProjectToken,
		"-e", "ENGINE_STORE_DIR=/store", "-e", "ENGINE_GRANTS_DIR=/grants",
		alpine, "/e/engine")
	l.EngineIP = ip(t, l.Engine, l.Net)

	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if st, _, _ := l.Do("GET", "/providers/Agentic.Engine/operations", ClientToken, nil, nil); st == 200 {
			// The analyst and the lint service must be up too: their health is open.
			a, _ := l.direct(ip(t, analyst, l.Net), "/health")
			li, _ := l.direct(ip(t, lint, l.Net), "/health")
			if a == 200 && li == 200 && (!o.build || l.servicesUp()) {
				return l
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	logs, _ := Run("docker", "logs", l.Engine)
	t.Fatalf("the engine did not become ready: %s", logs)
	return l
}

func (l *Lab) direct(host, path string) (int, string) {
	out, code := Run("docker", "exec", l.client, "/c/httpc", "GET", "http://"+host+":8080"+path)
	if code != 0 {
		return -1, out
	}
	var s int
	first, rest, _ := strings.Cut(out, "\n")
	fmt.Sscan(first, &s)
	return s, rest
}

// Do sends one request to the engine and returns the status, the Content-Type and the body. path
// holds the path and any query; `api-version` is added unless path already has it or the path is
// the provider operations list. A nil token sends no Authorization header. A non-nil body is sent
// as JSON, unless it is a string or []byte, which is sent as it is. Extra headers are "Name: value".
func (l *Lab) Do(method, path, token string, body any, extra []string) (int, string, []byte) {
	if !strings.Contains(path, "api-version=") && !strings.HasPrefix(path, "/providers/") {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		path += sep + "api-version=" + APIVersion
	}
	return l.Raw(method, path, token, body, extra)
}

// Raw is Do without adding the `api-version` query parameter.
func (l *Lab) Raw(method, path, token string, body any, extra []string) (int, string, []byte) {
	var in string
	switch b := body.(type) {
	case nil:
	case string:
		in = b
	case []byte:
		in = string(b)
	default:
		j, err := json.Marshal(b)
		if err != nil {
			return -1, "", []byte(err.Error())
		}
		in = string(j)
	}
	args := []string{"exec", "-i", l.client, "/c/httpc", method, l.BaseURL() + path}
	if token != "" {
		args = append(args, "Authorization: Bearer "+token)
	}
	if in != "" {
		args = append(args, "Content-Type: application/json")
	}
	args = append(args, extra...)
	cmd := exec.Command("docker", args...)
	cmd.Stdin = strings.NewReader(in)
	out, err := cmd.Output()
	if err != nil {
		return -1, "", []byte(err.Error() + ": " + string(out))
	}
	s := string(out)
	first, rest, _ := strings.Cut(s, "\n")
	ct, rest, _ := strings.Cut(rest, "\n")
	var st int
	fmt.Sscan(first, &st)
	return st, ct, []byte(rest)
}

// Operation is the engine's Operation, as far as the lab reads it.
type Operation struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Target string `json:"target"`
	Result struct {
		Slice       string `json:"slice"`
		Run         string `json:"run"`
		ReusedSlice string `json:"reused_slice"`
	} `json:"result"`
	Error *Problem `json:"error"`
}

// Problem is an RFC 9457 problem with the engine's stable code.
type Problem struct {
	Type     string  `json:"type"`
	Title    string  `json:"title"`
	Status   int     `json:"status"`
	Detail   string  `json:"detail"`
	Code     string  `json:"code"`
	Failures []Check `json:"failures"`
}

// Check is one check result.
type Check struct {
	Part   string `json:"part"`
	Check  string `json:"check"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason"`
}

// Accept sends a state-changing request that must answer 202 and returns the operation's id.
func (l *Lab) Accept(t testing.TB, method, path string, body any) string {
	t.Helper()
	st, _, b := l.Do(method, path, ClientToken, body, nil)
	if st != 202 {
		t.Fatalf("%s %s = %d, want 202: %s", method, path, st, b)
	}
	var op Operation
	if err := json.Unmarshal(b, &op); err != nil || op.ID == "" {
		t.Fatalf("%s %s: no operation in %s", method, path, b)
	}
	return op.ID
}

// Wait polls an operation until it has finished (not accepted or running) and returns it.
func (l *Lab) Wait(t testing.TB, opID string) Operation {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		st, _, b := l.Do("GET", "/operations/"+url.PathEscape(opID), ClientToken, nil, nil)
		if st != 200 {
			t.Fatalf("getOperation %s = %d: %s", opID, st, b)
		}
		var op Operation
		if err := json.Unmarshal(b, &op); err != nil {
			t.Fatalf("getOperation %s: %v: %s", opID, err, b)
		}
		if op.Status != "accepted" && op.Status != "running" {
			return op
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("operation %s did not finish", opID)
	return Operation{}
}

// Refused sends a request that must be refused with the status and problem code, and returns the problem.
func (l *Lab) Refused(t testing.TB, method, path string, body any, status int, code string) Problem {
	t.Helper()
	st, ct, b := l.Do(method, path, ClientToken, body, nil)
	var p Problem
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatalf("%s %s = %d %s: not a problem: %s", method, path, st, ct, b)
	}
	if st != status || p.Code != code {
		t.Fatalf("%s %s = %d %s, want %d %s: %s", method, path, st, p.Code, status, code, b)
	}
	if !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("%s %s: Content-Type %q, want application/problem+json", method, path, ct)
	}
	return p
}

// Slice reads a slice as a generic document.
func (l *Lab) Slice(t testing.TB, id string) map[string]any {
	t.Helper()
	st, _, b := l.Do("GET", "/slices/"+id, ClientToken, nil, nil)
	if st != 200 {
		t.Fatalf("getSlice %s = %d: %s", id, st, b)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("getSlice %s: %v: %s", id, err, b)
	}
	return m
}

// Create describes a slice and waits for the proposal; it fails unless the operation succeeded.
func (l *Lab) Create(t testing.TB, description string) string {
	t.Helper()
	op := l.Wait(t, l.Accept(t, "POST", "/slices", map[string]string{"description": description}))
	if op.Status != "succeeded" || op.Result.Slice == "" {
		t.Fatalf("createSlice did not succeed: %+v (error %+v)", op, op.Error)
	}
	return op.Result.Slice
}

// Misses is how many requests the replay server has seen that no recording matched.
func (l *Lab) Misses(t testing.TB) int {
	t.Helper()
	var m struct {
		Requests []json.RawMessage `json:"requests"`
	}
	out := l.replay(t, "misses", l.gateway)
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("misses output is not JSON: %v: %s", err, out)
	}
	return len(m.Requests)
}

// MissesJSON is the replay server's list of unmatched requests.
func (l *Lab) MissesJSON(t testing.TB) string { return l.replay(t, "misses", l.gateway) }

// EngineLogs is the engine container's output.
func (l *Lab) EngineLogs() string {
	out, _ := Run("docker", "logs", l.Engine)
	return out
}

// Schemathesis runs the pinned Schemathesis in a container on the lab network against the engine's
// contract (the whole preview folder is mounted, as its $refs reach the schema files beside it),
// with the Authorization bearer token, for the named operations, and returns its output and exit code.
// It fixes the `api-version` parameter to the contract's version: the contract's rule for that
// parameter is a date pattern, and any date but the served one is refused UnsupportedApiVersionValue
// (a contract-documented refusal Schemathesis would take for a rejected valid value; the contract
// package's own test covers it).
func (l *Lab) Schemathesis(t testing.TB, token string, operations []string, extra ...string) (string, int) {
	t.Helper()
	spec := filepath.Join(l.root, "src/engine/api/preview/2026-11-12-preview")
	cfg := t.TempDir()
	os.Chmod(cfg, 0o755)
	if err := os.WriteFile(filepath.Join(cfg, "schemathesis.toml"),
		[]byte("[parameters]\napi-version = \""+APIVersion+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"run", "--rm", "--network", l.Net, "--read-only", "--user", "65532:65532", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "--tmpfs", "/tmp:uid=65532,gid=65532", "-e", "HOME=/tmp",
		"-v", spec + ":/spec:ro", "-v", cfg + ":/cfg:ro", Pin(t, "schemathesis"),
		"--config-file", "/cfg/schemathesis.toml", "run", "/spec/openapi.yaml", "--url", l.BaseURL(), "-H", "Authorization: Bearer " + token}
	for _, op := range operations {
		args = append(args, "--include-operation-id", op)
	}
	args = append(args, extra...)
	return Run("docker", args...)
}
