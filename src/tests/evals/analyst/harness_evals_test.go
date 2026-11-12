//go:build evals

package analyst

// The live harness. It is behind the `evals` build tag so the ordinary `go test ./...` of
// scripts/check.sh never compiles it: it costs money, needs the test-scoped key and needs Docker.
// Run it with run.sh (see README.md).
//
// Per run it makes a throwaway internal Docker network, starts the model gateway (the pinned
// nginx with src/deploy/gateway/, the only container with egress, and the only one that sees the
// key file) and then one throwaway analyst container per trial, with no route except the
// gateway. Everything is labelled ap.eval.run=<id> and removed at the end; run.sh removes by that
// label too if the test is killed.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentic-platform/slice"
)

// ---- environment ----

func repoRoot(t testing.TB) string {
	t.Helper()
	d, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(d, "src", "deploy", "images.lock")); err == nil {
			return d
		}
		p := filepath.Dir(d)
		if p == d {
			t.Fatal("repository root not found (no src/deploy/images.lock above the working directory)")
		}
		d = p
	}
}

func pin(t testing.TB, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "src", "deploy", "images.lock"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) == 2 && f[0] == name {
			return f[1]
		}
	}
	t.Fatalf("no %s pin in images.lock", name)
	return ""
}

func docker(ctx context.Context, stdin []byte, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	err := cmd.Run()
	return o.Bytes(), e.Bytes(), err
}

func mustDocker(t testing.TB, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	o, e, err := docker(ctx, nil, args...)
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", args[0], err, e)
	}
	return strings.TrimSpace(string(o))
}

var seq atomic.Int64

// lab is one throwaway run: a network and its containers.
type lab struct {
	t      testing.TB
	root   string
	id     string
	net    string
	label  string
	report string
}

func newLab(t testing.TB) *lab {
	t.Helper()
	id := os.Getenv("EVAL_RUN_ID")
	if id == "" {
		id = fmt.Sprintf("%d-%d", time.Now().Unix(), os.Getpid())
	}
	l := &lab{t: t, root: repoRoot(t), id: id, net: "apeval-" + id + "-net", label: "ap.eval.run=" + id}
	if _, _, err := docker(context.Background(), nil, "version"); err != nil {
		t.Fatalf("docker is not usable: %v", err)
	}
	// `isolated` (Docker 28+) gives the bridge no host address, as in src/deploy/compose.yaml.
	mustDocker(t, "network", "create", "--internal", "--label", l.label,
		"-o", "com.docker.network.bridge.gateway_mode_ipv4=isolated", l.net)
	t.Cleanup(l.close)
	return l
}

func (l *lab) close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	o, _, _ := docker(ctx, nil, "ps", "-aq", "--filter", "label="+l.label)
	if ids := strings.Fields(string(o)); len(ids) > 0 {
		docker(ctx, nil, append([]string{"rm", "-f"}, ids...)...)
	}
	docker(ctx, nil, "network", "rm", l.net)
}

func (l *lab) name(s string) string { return fmt.Sprintf("apeval-%s-%s-%d", l.id, s, seq.Add(1)) }

// waitFor polls cmd (a shell command run inside container c) until it succeeds.
func (l *lab) waitFor(c, sh string) {
	l.t.Helper()
	for i := 0; i < 60; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _, err := docker(ctx, nil, "exec", c, "sh", "-c", sh)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	logs, _, _ := docker(context.Background(), nil, "logs", c)
	l.t.Fatalf("container %s did not come up; logs:\n%s", c, logs)
}

// gateway starts the real gateway. keyFile is mounted into it alone and never read here.
func (l *lab) gateway(keyFile string) {
	l.t.Helper()
	gw := filepath.Join(l.root, "src", "deploy", "gateway")
	n := l.name("gateway")
	uid := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	own := fmt.Sprintf("uid=%d,gid=%d", os.Getuid(), os.Getgid())
	// Runs as the invoking user, not 101: the key file is the developer's own (mode 600), so only
	// that user can read the mount. Created on the default bridge for egress, then joined to the
	// internal network under the name agents use.
	mustDocker(l.t, "create", "--name", n, "--label", l.label, "--read-only", "--user", uid,
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", "256m", "--pids-limit", "128",
		"--tmpfs", "/etc/nginx/conf.d:"+own+",mode=0755", "--tmpfs", "/var/cache/nginx:"+own,
		"--tmpfs", "/var/run:"+own, "--tmpfs", "/tmp:"+own,
		"-v", gw+":/etc/nginx/templates:ro",
		"-v", filepath.Join(gw, "credential.envsh")+":/docker-entrypoint.d/05-credential.envsh:ro",
		"-v", keyFile+":/run/secrets/anthropic_api_key:ro",
		"-e", "GATEWAY_SERVER_NAME=gateway",
		"-e", "NGINX_ENVSUBST_FILTER=^(ANTHROPIC_API_KEY|GATEWAY_SERVER_NAME)$",
		pin(l.t, l.root, "nginx"))
	mustDocker(l.t, "network", "connect", "--alias", "gateway", l.net, n)
	mustDocker(l.t, "start", n)
	l.waitFor(n, "wget -q -S -O /dev/null --header 'Host: gateway' http://127.0.0.1:8080/probe 2>&1 | grep -q 403")
}

// stub starts a stand-in for the gateway on the internal network only: every request gets the
// canned Anthropic streaming response whose one text block is text. No egress, no key.
func (l *lab) stub(text string) {
	l.t.Helper()
	dir := l.t.(interface{ TempDir() string }).TempDir()
	ev := func(typ string, v any) string {
		b, _ := json.Marshal(v)
		return "event: " + typ + "\ndata: " + string(b) + "\n\n"
	}
	sse := ev("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_stub", "type": "message", "role": "assistant", "model": "claude-haiku-5-5",
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 10, "output_tokens": 1}}}) +
		ev("content_block_start", map[string]any{"type": "content_block_start", "index": 0,
			"content_block": map[string]any{"type": "text", "text": ""}}) +
		ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": text}}) +
		ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}) +
		ev("message_delta", map[string]any{"type": "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
			"usage": map[string]any{"output_tokens": 100}}) +
		ev("message_stop", map[string]any{"type": "message_stop"})
	conf := "server { listen 8080; location = /resp.sse { root /stub; default_type text/event-stream; error_page 405 =200 $uri; }\n  location / { rewrite ^ /resp.sse last; } }\n"
	must(l.t, os.WriteFile(filepath.Join(dir, "resp.sse"), []byte(sse), 0o644))
	must(l.t, os.WriteFile(filepath.Join(dir, "stub.conf"), []byte(conf), 0o644))
	n := l.name("stub")
	own := fmt.Sprintf("uid=%d,gid=%d", os.Getuid(), os.Getgid())
	mustDocker(l.t, "create", "--name", n, "--label", l.label, "--read-only", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--network", l.net, "--network-alias", "gateway",
		"--tmpfs", "/var/cache/nginx:"+own, "--tmpfs", "/var/run:"+own, "--tmpfs", "/tmp:"+own,
		"-v", dir+":/stub:ro", "-v", filepath.Join(dir, "stub.conf")+":/etc/nginx/conf.d/default.conf:ro",
		pin(l.t, l.root, "nginx"))
	mustDocker(l.t, "start", n)
	l.waitFor(n, "wget -q -O /dev/null --post-data '{}' http://127.0.0.1:8080/v1/messages")
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Trial is one run of one case.
type Trial struct {
	N        int      `json:"trial"`
	Attempts int      `json:"attempts"`
	Passed   bool     `json:"passed"`
	Results  []Result `json:"results"`
	Duration string   `json:"duration"`
	Stderr   string   `json:"stderr,omitempty"`
	ExitErr  string   `json:"exit_error,omitempty"`
}

// CaseReport is every trial of one case; the case passes (pass^k) only when all trials pass.
type CaseReport struct {
	Name        string  `json:"case"`
	Adversarial bool    `json:"adversarial"`
	Attack      string  `json:"attack,omitempty"`
	Passed      bool    `json:"passed"`
	Trials      []Trial `json:"trials"`
}

// analyst runs one throwaway analyst container with description on stdin and returns stdout.
// agentDir is mounted read-only at /agents. The container has only the internal network.
func (l *lab) analyst(agentDir, agentFile, description string) (out []byte, errOut []byte, err error) {
	flags := strings.Fields(os.Getenv("EVAL_RUN_FLAGS"))
	if len(flags) == 0 {
		flags = []string{"--exec", "--last"}
	}
	timeout := 5 * time.Minute
	if v, e := time.ParseDuration(os.Getenv("EVAL_TRIAL_TIMEOUT")); e == nil {
		timeout = v
	}
	n := l.name("analyst")
	args := []string{"run", "--rm", "-i", "--name", n, "--label", l.label, "--network", l.net,
		"--read-only", "--user", "65532:65532", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--memory", "1g", "--pids-limit", "512", "--tmpfs", "/tmp:uid=65532,gid=65532,exec",
		"-e", "HOME=/tmp/home", "-e", "TELEMETRY_ENABLED=false",
		"-e", "MODEL_GATEWAY_URL=http://gateway:8080",
		// A placeholder so the client does not refuse to start; the gateway replaces it (D9).
		"-e", "ANTHROPIC_API_KEY=eval-placeholder-not-a-key",
		"-v", agentDir + ":/agents:ro", pin(l.t, l.root, "docker-agent"), "run"}
	args = append(args, flags...)
	args = append(args, "/agents/"+agentFile, "-")
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, errOut, err = docker(ctx, []byte(description), args...)
	if ctx.Err() != nil {
		docker(context.Background(), nil, "rm", "-f", n)
		err = fmt.Errorf("trial timed out after %s", timeout)
	}
	return
}

func (l *lab) grader() *Grader {
	l.t.Helper()
	s, err := slice.LoadSchemas(filepath.Join(l.root, "src", "engine", "api", "preview", "2026-11-12-preview"))
	must(l.t, err)
	p, err := os.ReadFile(filepath.Join(l.root, "src", "catalogue", "api-feature-go", "pattern.yaml"))
	must(l.t, err)
	return &Grader{Schemas: s, Pattern: p}
}

// run runs every case Trials times in order and returns the reports. Outputs go to dir.
func (l *lab) run(cases []Case, agentDir, agentFile, dir string) []CaseReport {
	g := l.grader()
	must(l.t, os.MkdirAll(dir, 0o755))
	var reps []CaseReport
	for _, c := range cases {
		cr := CaseReport{Name: c.Name, Adversarial: c.Adversarial, Attack: c.Attack, Passed: true}
		for i := 1; i <= Trials; i++ {
			start := time.Now()
			out, stderr, attempts, err := l.analystWithRetry(agentDir, agentFile, c.Description)
			tr := Trial{N: i, Attempts: attempts, Duration: time.Since(start).Round(time.Millisecond).String()}
			if err != nil {
				tr.ExitErr = err.Error()
				tr.Stderr = tail(string(stderr), 600)
			}
			tr.Results = GradeCase(g, c.Name, out)
			tr.Passed = err == nil && Passed(tr.Results)
			must(l.t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s.trial%d.out", c.Name, i)), out, 0o644))
			cr.Passed = cr.Passed && tr.Passed
			cr.Trials = append(cr.Trials, tr)
		}
		reps = append(reps, cr)
	}
	return reps
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "..." + s[len(s)-n:]
	}
	return s
}

// print writes the per-case, per-trial report and the JSON file, and returns whether all passed.
func print(t testing.TB, reps []CaseReport, dir string) bool {
	all := true
	var b strings.Builder
	fmt.Fprintf(&b, "\nanalyst evals: pass^%d per case\n", Trials)
	for _, c := range reps {
		all = all && c.Passed
		kind := "functional"
		if c.Adversarial {
			kind = "adversarial: " + c.Attack
		}
		fmt.Fprintf(&b, "\n[%s] %s (%s)\n", verdict(c.Passed), c.Name, kind)
		for _, tr := range c.Trials {
			fmt.Fprintf(&b, "  trial %d: %s  %s  attempts=%d\n", tr.N, verdict(tr.Passed), tr.Duration, tr.Attempts)
			if tr.ExitErr != "" {
				fmt.Fprintf(&b, "    run failed: %s\n      %s\n", tr.ExitErr, tr.Stderr)
			}
			for _, f := range Failures(tr.Results) {
				fmt.Fprintf(&b, "    - %s\n", f)
			}
		}
	}
	fmt.Fprintf(&b, "\nsuite: %s\n", verdict(all))
	t.Log(b.String())
	j, _ := json.MarshalIndent(reps, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "report.json"), j, 0o644)
	t.Logf("report and raw outputs in %s", dir)
	return all
}

func verdict(b bool) string {
	if b {
		return "PASS"
	}
	return "FAIL"
}

func reportDir(l *lab) string {
	if d := os.Getenv("EVAL_REPORT_DIR"); d != "" {
		return d
	}
	return filepath.Join(os.TempDir(), "analyst-evals-"+l.id)
}

// ---- tests ----

// keyFile is the test-scoped key file (D19): outside the repository, a file holding only the key.
// It is checked for existence and size and never read here: only the gateway reads it.
func keyFile(t *testing.T) string {
	t.Helper()
	p := os.Getenv("EVAL_KEY_FILE")
	if p == "" {
		h, _ := os.UserHomeDir()
		p = filepath.Join(h, ".config", "agentic-platform", "anthropic_api_key")
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatalf("the live analyst evals need the test-scoped key file and %s is not there (%v). They do not skip: "+
			"put the key in that file (or set EVAL_KEY_FILE), or run only the model-free tests.", p, err)
	}
	if st.IsDir() || st.Size() == 0 {
		t.Fatalf("the key file %s is a directory or empty", p)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("the key file %s is not readable by this user: %v", p, err)
	}
	f.Close()
	abs, _ := filepath.Abs(p)
	return abs
}

// TestAnalystEvals is the live suite (T025): every case Trials times through the real gateway.
func TestAnalystEvals(t *testing.T) {
	key := keyFile(t) // fails loudly, never skips
	root := repoRoot(t)
	agentFile := filepath.Join(root, "src", "agents", "analyst.yaml")
	if _, err := os.Stat(agentFile); err != nil {
		t.Fatalf("the analyst agent definition is missing: %v", err)
	}
	cases := Cases()
	if only := os.Getenv("EVAL_CASES"); only != "" {
		want := map[string]bool{}
		for _, n := range strings.Split(only, ",") {
			want[strings.TrimSpace(n)] = true
		}
		var sel []Case
		for _, c := range cases {
			if want[c.Name] {
				sel = append(sel, c)
			}
		}
		if len(sel) != len(want) {
			t.Fatalf("EVAL_CASES %q names a case that does not exist", only)
		}
		cases = sel
	}
	l := newLab(t)
	l.gateway(key)
	dir := reportDir(l)
	if !print(t, l.run(cases, filepath.Dir(agentFile), "analyst.yaml", dir), dir) {
		t.Fatal("the analyst failed at least one case (pass^3 needs all trials to pass)")
	}
}

// TestHarnessStub proves the harness with no model and no key: a stub gateway returns a canned
// Anthropic response, a minimal agent definition (testdata/stub-agent.yaml, not the analyst)
// reads it, and the harness must pass a good proposal and fail a bad one, trial by trial.
func TestHarnessStub(t *testing.T) {
	root := repoRoot(t)
	dir, err := filepath.Abs("testdata")
	must(t, err)
	good := fixtureProposal(t, "good-notes")
	for _, c := range []struct {
		fixture string
		want    bool
	}{{"good-notes", true}, {"bad-approvals-field", false}} {
		t.Run(c.fixture, func(t *testing.T) {
			l := newLab(t)
			_ = root
			l.stub(string(fixtureProposal(t, c.fixture)))
			rd := t.TempDir()
			reps := l.run([]Case{{Name: "example", Description: Example}}, dir, "stub-agent.yaml", rd)
			print(t, reps, rd)
			if reps[0].Passed != c.want {
				t.Fatalf("case passed=%v, want %v", reps[0].Passed, c.want)
			}
			for _, tr := range reps[0].Trials {
				if tr.ExitErr != "" {
					t.Fatalf("the agent container failed: %s %s", tr.ExitErr, tr.Stderr)
				}
			}
			if c.want {
				out, _ := os.ReadFile(filepath.Join(rd, "example.trial1.out"))
				if strings.TrimSpace(string(out)) != strings.TrimSpace(string(good)) {
					t.Fatalf("the agent's output is not the canned proposal:\n%s", out)
				}
			}
		})
	}
}

// TestGatewayStarts starts the real gateway with the key file and proves it comes up and refuses a
// wrong path, without sending any model request (no cost). It fails loudly without the key file.
func TestGatewayStarts(t *testing.T) {
	key := keyFile(t)
	l := newLab(t)
	l.gateway(key) // waits for the 403 on a path the gateway does not allow
}
