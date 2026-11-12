//go:build docker

package analystsvc

// T038f: the engine's analyst client against the real pinned Docker Agent `serve api`, hardened as
// research D27 says, whose model is the recorded-model replay server on an internal network. No
// key, no egress, no model cost. A missing Docker is a failure, never a skip.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/slices"
	ev "github.com/agentic-platform/tests/evals/analyst"
)

func run(name string, args ...string) (string, int) {
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

func docker(t *testing.T, args ...string) string {
	t.Helper()
	out, code := run("docker", args...)
	if code != 0 {
		t.Fatalf("docker %s: exit %d: %s", strings.Join(args, " "), code, out)
	}
	return out
}

func pin(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "src", "deploy", "images.lock"))
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

func ip(t *testing.T, container, network string) string {
	t.Helper()
	return docker(t, "inspect", "-f", fmt.Sprintf("{{(index .NetworkSettings.Networks %q).IPAddress}}", network), container)
}

func TestAnalystServiceReplaysTheQuickstartProposal(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if out, code := run("docker", "info", "--format", "{{.ServerVersion}}"); code != 0 {
		t.Fatalf("Docker is required and is not available (no skip): %s", out)
	}
	id := fmt.Sprintf("%d", os.Getpid())
	net := "aptest-analystsvc-" + id
	docker(t, "network", "create", "--internal", "-o", "com.docker.network.bridge.gateway_mode_ipv4=isolated", net)
	t.Cleanup(func() { run("docker", "network", "rm", net) })

	replay := func(args ...string) string {
		cmd := exec.Command("sh", append([]string{filepath.Join(root, "src/tests/recorded/replay.sh")}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("replay.sh %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	gw := replay("start", net, "aptest-replay-"+id)
	t.Cleanup(func() { run("sh", filepath.Join(root, "src/tests/recorded/replay.sh"), "stop", gw) })

	runtime := "runc"
	if out, _ := run("docker", "info", "--format", "{{json .Runtimes}}"); strings.Contains(out, `"runsc"`) {
		runtime = "runsc" // the platform requires gVisor; the fallback keeps the lab usable without it
	}
	tok := "tok-" + id
	svc := "aptest-analystsvc-" + id
	docker(t, "run", "-d", "--name", svc, "--network", net, "--runtime", runtime,
		"--read-only", "--user", "65532:65532", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--memory", "1g", "--pids-limit", "512", "--tmpfs", "/tmp:uid=65532,gid=65532",
		"-e", "HOME=/tmp/home", "-e", "TELEMETRY_ENABLED=false",
		"-e", "MODEL_GATEWAY_URL=http://"+ip(t, gw, net)+":8080", // runsc has no Docker DNS
		"-e", "ANTHROPIC_API_KEY=placeholder-not-a-key",
		"-v", filepath.Join(root, "src", "agents")+":/agents:ro",
		pin(t, root, "docker-agent"), "serve", "api", "/agents/analyst.yaml",
		"--listen", "0.0.0.0:8080", "--auth-token", tok, "--session-db", "/tmp/session.db")
	t.Cleanup(func() { run("docker", "rm", "-f", svc) })

	// The host cannot dial a container on an isolated internal network, so the client runs in a
	// throwaway container on that network: a static binary of analystsvc/client.
	bin := t.TempDir()
	os.Chmod(bin, 0o755)
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "client"), "./client")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the client: %v: %s", err, out)
	}
	base := "http://" + ip(t, svc, net) + ":8080"
	cli := exec.Command("docker", "run", "--rm", "--network", net, "--read-only", "--user", "65532:65532",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges", "-v", bin+":/c:ro",
		pin(t, root, "alpine"), "/c/client", base, tok, "sha256:test-digest", ev.Example)
	var stderr strings.Builder
	cli.Stderr = &stderr
	out, err := cli.Output()
	if err != nil {
		logs, _ := run("docker", "logs", svc)
		t.Fatalf("the client failed: %v\n%s\nIf the error is a model 404 or a body mismatch, the recording does not match the "+
			"current analyst prompt (it needs re-synthesising); otherwise the client or service is at fault.\nservice logs:\n%s", err, stderr.String(), logs)
	}
	var p slices.Proposal
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatalf("the client printed no proposal: %v: %s", err, out)
	}

	want, err := os.ReadFile(filepath.Join(root, "src/tests/evals/analyst/testdata/proposals/good-notes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wp slices.Proposal
	if err := json.Unmarshal(want, &wp); err != nil {
		t.Fatal(err)
	}
	wp.Model, wp.AgentDigest = "anthropic/claude-haiku-5-5", "sha256:test-digest"
	gb, _ := json.Marshal(p)
	wb, _ := json.Marshal(wp)
	if string(gb) != string(wb) {
		t.Errorf("proposal differs from good-notes.json.\ngot:  %s\nwant: %s", gb, wb)
	}
	var misses struct {
		Requests []json.RawMessage `json:"requests"`
	}
	m := replay("misses", gw)
	if err := json.Unmarshal([]byte(m), &misses); err != nil {
		t.Fatalf("misses output is not JSON: %v: %s", err, m)
	}
	if len(misses.Requests) != 0 {
		t.Errorf("the replay server saw an unrecorded request (a title call?): %s", m)
	}
}
