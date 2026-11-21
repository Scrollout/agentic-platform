//go:build evals

package steps

// The live harness. It is behind the `evals` build tag so the ordinary `go test ./...` of
// scripts/check.sh never compiles it: it costs money, needs the test-scoped key and needs Docker.
// Run it with run.sh (see README.md).
//
// Per run: a throwaway internal Docker network, the model gateway (the pinned nginx with
// src/deploy/gateway/, the only container with egress and the only one that sees the key file), and
// one throwaway step-agent container per trial with no route except the gateway, the trial's
// project mounted writable at /projects/p and src/agents read-only. The agent's own container is
// the step container's shape (research D29), run through `docker-agent run --exec` rather than
// `serve api`: the same definition and the same tools, one prompt. The graders then run the go
// toolchain on what the agent left in a second kind of throwaway container with no network at all
// (DockerRunner), never on the host. Everything is labelled ap.eval.run=<id> and removed afterwards.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentic-platform/tests/evals/runevents"
)

// ---- environment ----

func mustRoot(t testing.TB) string {
	t.Helper()
	r, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return r
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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
	t     testing.TB
	root  string
	id    string
	net   string
	label string
	image string // the step agent image
}

func newLab(t testing.TB) *lab {
	t.Helper()
	id := os.Getenv("EVAL_RUN_ID")
	if id == "" {
		id = fmt.Sprintf("%d-%d", time.Now().Unix(), os.Getpid())
	}
	l := &lab{t: t, root: mustRoot(t), id: id, net: "apeval-" + id + "-net", label: "ap.eval.run=" + id}
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
	// Runs as the invoking user: the key file is the developer's own (mode 600). Created on the
	// default bridge for egress, then joined to the internal network under the name agents use.
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

// stepImage returns the step agent image: EVAL_AGENT_IMAGE, or src/deploy/Dockerfile.agent built
// here from the pins in images.lock (Docker Agent plus the Go toolchain), unsigned and local.
func (l *lab) stepImage() string {
	l.t.Helper()
	if i := os.Getenv("EVAL_AGENT_IMAGE"); i != "" {
		return i
	}
	da, gl := pin(l.t, l.root, "docker-agent"), pin(l.t, l.root, "golang")
	tag := "apeval-step-agent:" + sum([]byte(da + gl))[:12]
	if _, _, err := docker(context.Background(), nil, "image", "inspect", tag); err == nil {
		return tag
	}
	mustDocker(l.t, "build", "-q", "-f", filepath.Join(l.root, "src", "deploy", "Dockerfile.agent"),
		"--build-arg", "DOCKER_AGENT_IMAGE="+da, "--build-arg", "GOLANG_IMAGE="+gl, "-t", tag, filepath.Join(l.root, "src"))
	return tag
}

// agent runs one throwaway step-agent container: the message on stdin, the project at
// /projects/p. It returns the agent's final answer (the last assistant message, what --last
// printed), the raw --json events (stdout), the usage they report (what was read, even when they
// do not parse) and stderr.
func (l *lab) agent(agentName, projectDir, message string) (out, events []byte, usage *runevents.Usage, errOut []byte, err error) {
	flags := strings.Fields(os.Getenv("EVAL_RUN_FLAGS"))
	if len(flags) == 0 {
		// autonomous: the platform runs the step agents so, inside the container (T049); the
		// container, not the safety mode, is the boundary.
		flags = []string{"--exec", "--safety", "autonomous"}
	}
	flags = jsonFlags(flags)
	timeout := 10 * time.Minute
	if v, e := time.ParseDuration(os.Getenv("EVAL_TRIAL_TIMEOUT")); e == nil {
		timeout = v
	}
	user := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	n := l.name(agentName)
	args := []string{"run", "--rm", "-i", "--name", n, "--label", l.label, "--network", l.net,
		"--read-only", "--user", user, "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--memory", "2g", "--pids-limit", "1024",
		"--tmpfs", fmt.Sprintf("/tmp:uid=%d,gid=%d,exec,size=2g", os.Getuid(), os.Getgid()),
		"-e", "HOME=/tmp/home", "-e", "TELEMETRY_ENABLED=false", "-e", "GOTELEMETRY=off",
		"-e", "GOFLAGS=-buildvcs=false", "-e", "GOPROXY=off", "-e", "GOWORK=off", "-e", "GOTOOLCHAIN=local",
		"-e", "GOCACHE=/tmp/gocache", "-e", "GOPATH=/tmp/gopath", "-e", "CGO_ENABLED=0",
		"-e", "MODEL_GATEWAY_URL=http://gateway:8080",
		// A placeholder so the client does not refuse to start; the gateway replaces it (D9).
		"-e", "ANTHROPIC_API_KEY=eval-placeholder-not-a-key",
	}
	for _, m := range trialMounts(projectDir, filepath.Join(l.root, "src", "agents")) {
		args = append(args, "-v", m)
	}
	args = append(args, "-w", "/projects/p", l.image, "run")
	args = append(args, flags...)
	args = append(args, "--working-dir", "/projects/p", "/agents/"+agentName+".yaml", "-")
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	events, errOut, err = docker(ctx, []byte(message), args...)
	if ctx.Err() != nil {
		docker(context.Background(), nil, "rm", "-f", n)
		err = fmt.Errorf("trial timed out after %s", timeout)
	}
	// Only docker-agent's exit code and the graders decide a trial, as with --last: a parse
	// problem is logged, and the answer (possibly empty) and usage are what the events hold.
	run, perr := runevents.Parse(bytes.NewReader(events))
	if perr != nil && err == nil {
		l.t.Logf("the docker-agent --json events: %v", perr)
	}
	return []byte(run.Answer), events, &run.Usage, errOut, err
}

// jsonFlags makes the docker-agent run flags print --json events instead of the --last answer.
func jsonFlags(flags []string) []string {
	var out []string
	for _, f := range flags {
		if f != "--last" && f != "--json" {
			out = append(out, f)
		}
	}
	return append(out, "--json")
}

// ---- trials and reports ----

// Trial is one run of one case.
type Trial struct {
	N        int      `json:"trial"`
	Passed   bool     `json:"passed"`
	Results  []Result `json:"results"`
	Duration string   `json:"duration"`
	Stderr   string   `json:"stderr,omitempty"`
	ExitErr  string   `json:"exit_error,omitempty"`
	Changed  []string `json:"changed_files,omitempty"`
	// Usage is the trial's token use and cost, from the --json events.
	Usage *runevents.Usage `json:"usage,omitempty"`
}

// CaseReport is every trial of one case; the case passes (pass^k) only when all trials pass.
type CaseReport struct {
	Name        string  `json:"case"`
	Agent       string  `json:"agent"`
	Adversarial bool    `json:"adversarial"`
	Attack      string  `json:"attack,omitempty"`
	Passed      bool    `json:"passed"`
	Trials      []Trial `json:"trials"`
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "..." + s[len(s)-n:]
	}
	return s
}

// changedFiles lists the paths the agent added, changed or removed (vendor/ aside, which no step
// but generate may touch and ScopeKept judges anyway).
func changedFiles(before, after Files) []string {
	var out []string
	for p, c := range after {
		if b, ok := before[p]; !ok || b != c {
			out = append(out, p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			out = append(out, p+" (removed)")
		}
	}
	sort.Strings(out)
	return out
}

func (l *lab) run(env Env, cases []Case, dir string) []CaseReport {
	must(l.t, os.MkdirAll(dir, 0o755))
	var reps []CaseReport
	for _, c := range cases {
		cr := CaseReport{Name: c.Name, Agent: c.Agent, Adversarial: c.Adversarial, Attack: c.Attack, Passed: true}
		before, err := c.Before()
		must(l.t, err)
		var plant *Plant
		if c.Plant != nil {
			plant, err = c.Plant(before)
			must(l.t, err)
		}
		beforeDir, err := Write(before)
		must(l.t, err)
		msg := Message(env.Steps[c.Agent], c.Slice, "/projects/p", before)
		for i := 1; i <= Trials; i++ {
			start := time.Now()
			work, err := Write(before) // each trial starts from the same files
			must(l.t, err)
			out, events, usage, stderr, runErr := l.agent(c.Agent, work, msg)
			tr := Trial{N: i, Duration: time.Since(start).Round(time.Second).String(), Usage: usage}
			if runErr != nil {
				tr.ExitErr = runErr.Error()
				tr.Stderr = tail(string(stderr), 600)
			}
			after, rerr := Read(work)
			must(l.t, rerr)
			tr.Changed = changedFiles(before, after)
			switch c.Agent {
			case AgentTests:
				tr.Results = env.GradeTests(beforeDir, work)
			case AgentCode:
				tr.Results = env.GradeCode(beforeDir, work, c.Slice.Contract)
			case AgentReview:
				tr.Results = env.GradeReview(beforeDir, work, string(out), plant)
			}
			tr.Passed = runErr == nil && Passed(tr.Results)
			// Keep the agent's answer and every file it wrote, for reading a failure.
			tdir := filepath.Join(dir, fmt.Sprintf("%s.trial%d", strings.ReplaceAll(c.Name, "/", "-"), i))
			must(l.t, os.MkdirAll(tdir, 0o755))
			must(l.t, os.WriteFile(filepath.Join(tdir, "answer.txt"), out, 0o644))
			must(l.t, os.WriteFile(filepath.Join(tdir, "events.ndjson"), events, 0o644))
			written := Files{}
			for _, p := range tr.Changed {
				if f, ok := after[p]; ok && !strings.HasPrefix(p, "vendor/") {
					written[p] = f
				}
			}
			must(l.t, WriteInto(filepath.Join(tdir, "files"), written))
			cr.Passed = cr.Passed && tr.Passed
			cr.Trials = append(cr.Trials, tr)
		}
		reps = append(reps, cr)
	}
	return reps
}

func verdictWord(b bool) string {
	if b {
		return "PASS"
	}
	return "FAIL"
}

func printReport(t testing.TB, reps []CaseReport, dir string) bool {
	all := true
	var b strings.Builder
	fmt.Fprintf(&b, "\nstep agents evals: pass^%d per case\n", Trials)
	for _, c := range reps {
		all = all && c.Passed
		kind := "functional"
		if c.Adversarial {
			kind = "adversarial: " + c.Attack
		}
		fmt.Fprintf(&b, "\n[%s] %s (%s)\n", verdictWord(c.Passed), c.Name, kind)
		for _, tr := range c.Trials {
			fmt.Fprintf(&b, "  trial %d: %s  %s  wrote %d files\n", tr.N, verdictWord(tr.Passed), tr.Duration, len(tr.Changed))
			if tr.ExitErr != "" {
				fmt.Fprintf(&b, "    run failed: %s\n      %s\n", tr.ExitErr, tr.Stderr)
			}
			for _, f := range Failures(tr.Results) {
				fmt.Fprintf(&b, "    - %s\n", f)
			}
		}
	}
	fmt.Fprintf(&b, "\nsuite: %s\n", verdictWord(all))
	t.Log(b.String())
	j, _ := json.MarshalIndent(reps, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "report.json"), j, 0o644)
	t.Logf("report, answers and the files each trial wrote are in %s", dir)
	return all
}

// ---- gates ----

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
		t.Fatalf("the live step evals need the test-scoped key file and %s is not there (%v). They do not skip: "+
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

// selected applies EVAL_CASES: a comma list of case names, or of agent names (tests, code, review).
func selected(t *testing.T, cases []Case) []Case {
	only := os.Getenv("EVAL_CASES")
	if only == "" {
		return cases
	}
	want := map[string]bool{}
	for _, n := range strings.Split(only, ",") {
		want[strings.TrimSpace(n)] = true
	}
	var sel []Case
	used := map[string]bool{}
	for _, c := range cases {
		if want[c.Name] || want[c.Agent] {
			sel = append(sel, c)
			used[c.Name], used[c.Agent] = true, true
		}
	}
	for n := range want {
		if !used[n] {
			t.Fatalf("EVAL_CASES %q names a case or agent that does not exist", n)
		}
	}
	return sel
}

// TestStepEvals is the live suite (T041): every case Trials times through the real gateway. Its
// gates, in order, each fail loudly and never skip: the three agent definitions exist (T049),
// EVAL_LIVE=1 (run.sh live sets it, so a stray `go test -tags evals` never spends money), and the
// key file is there.
func TestStepEvals(t *testing.T) {
	root := mustRoot(t)
	var missing []string
	for _, a := range []string{AgentTests, AgentCode, AgentReview} {
		for _, f := range []string{a + ".yaml", filepath.Join(a, "instruction.md")} {
			if _, err := os.Stat(filepath.Join(root, "src", "agents", f)); err != nil {
				missing = append(missing, "src/agents/"+f)
			}
		}
	}
	if len(missing) > 0 {
		t.Fatalf("agent definition missing (T049 writes them; the live evals cannot pass before): %s", strings.Join(missing, ", "))
	}
	if os.Getenv("EVAL_LIVE") != "1" {
		t.Fatal("EVAL_LIVE is not 1: this suite calls the model and costs money; start it with `sh src/tests/evals/steps/run.sh live`")
	}
	key := keyFile(t)
	steps, err := LoadPattern(root)
	must(t, err)
	cases := selected(t, Cases())
	l := newLab(t)
	l.image = l.stepImage()
	l.gateway(key)
	env := Env{Steps: steps, Runner: DockerRunner{Image: l.image, Label: l.label}}
	dir := os.Getenv("EVAL_REPORT_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "steps-evals-"+l.id)
	}
	if !printReport(t, l.run(env, cases, dir), dir) {
		t.Fatal("a step agent failed at least one case (pass^3 needs all trials to pass)")
	}
}

// TestGatewayStarts starts the real gateway with the key file and proves it comes up and refuses a
// wrong path, without sending any model request (no cost). It fails loudly without the key file.
func TestGatewayStarts(t *testing.T) {
	key := keyFile(t)
	l := newLab(t)
	l.gateway(key)
}
