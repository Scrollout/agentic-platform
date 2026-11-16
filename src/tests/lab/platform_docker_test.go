//go:build docker

// T054i behind the `docker` tag: the build option's check service and project worker, the store
// surviving an engine restart, the sweep of a dead process's leftovers, and nothing left after a
// test. The seams are listed in platform_settings_test.go.
package lab

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// labLabel is the label every lab resource carries, with its process's ID (research D34).
const labLabel = "org.agentic-platform.lab"

func envOf(t testing.TB, container string) map[string]string {
	t.Helper()
	var env []string
	if err := json.Unmarshal([]byte(Docker(t, "inspect", "-f", "{{json .Config.Env}}", container)), &env); err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}

// callService sends POST /step-files without api-version from the lab's client container: an
// accepted token gets 400 MissingApiVersionParameter (the token is checked first and nothing runs),
// any other token 401.
func (l *Lab) callService(t testing.TB, addr, token string) (int, string) {
	t.Helper()
	args := []string{"exec", "-i", l.client, "/c/httpc", "POST", "http://" + addr + ":8080/step-files", "Content-Type: application/json"}
	if token != "" {
		args = append(args, "Authorization: Bearer "+token)
	}
	cmd := exec.Command("docker", args...)
	cmd.Stdin = strings.NewReader("{}")
	out, err := cmd.Output()
	if err != nil {
		return -1, err.Error() + ": " + string(out)
	}
	first, rest, _ := strings.Cut(string(out), "\n")
	st, _ := strconv.Atoi(strings.TrimSpace(first))
	return st, rest
}

func memBytes(s string) int64 {
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "g"):
		mult, s = 1<<30, strings.TrimSuffix(s, "g")
	case strings.HasSuffix(s, "m"):
		mult, s = 1<<20, strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "k"):
		mult, s = 1<<10, strings.TrimSuffix(s, "k")
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n * mult
}

// runningMatches compares what Docker runs for container with the lab's settings for service: the
// settings the no-Docker test proves equal to compose.yaml are the ones the lab really starts.
func (l *Lab) runningMatches(t testing.TB, container, service string) {
	t.Helper()
	runtimes, _ := Run("docker", "info", "--format", "{{json .Runtimes}}")
	h := Host{
		Runsc: strings.Contains(runtimes, `"runsc"`),
		User:  fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		Addrs: map[string]string{"engine": l.EngineIP, "gateway": ip(t, l.gateway, l.Net)},
	}
	want, err := ServiceSettings(service, h)
	if err != nil {
		t.Fatalf("ServiceSettings(%q): %v", service, err)
	}
	var c struct {
		Config struct {
			User       string
			Env        []string
			Entrypoint []string
			Cmd        []string
			Labels     map[string]string
		}
		HostConfig struct {
			Runtime        string
			ReadonlyRootfs bool
			CapDrop        []string
			SecurityOpt    []string
			Tmpfs          map[string]string
			NanoCpus       int64
			Memory         int64
			PidsLimit      *int64
		}
	}
	if err := json.Unmarshal([]byte(Docker(t, "inspect", "-f", "{{json .}}", container)), &c); err != nil {
		t.Fatal(err)
	}
	rt := c.HostConfig.Runtime
	if rt == "runc" {
		rt = ""
	}
	if rt != want.Runtime {
		t.Errorf("%s runs under %q, want %q", service, c.HostConfig.Runtime, want.Runtime)
	}
	if c.HostConfig.ReadonlyRootfs != want.ReadOnly || c.Config.User != want.User {
		t.Errorf("%s: read-only %v user %q, want %v %q", service, c.HostConfig.ReadonlyRootfs, c.Config.User, want.ReadOnly, want.User)
	}
	if strings.Join(sorted(c.HostConfig.CapDrop), ",") != strings.Join(sorted(want.CapDrop), ",") {
		t.Errorf("%s drops %v, want %v", service, c.HostConfig.CapDrop, want.CapDrop)
	}
	if strings.Join(normalOpts(c.HostConfig.SecurityOpt), ",") != strings.Join(normalOpts(want.SecurityOpt), ",") {
		t.Errorf("%s: security options %v, want %v", service, c.HostConfig.SecurityOpt, want.SecurityOpt)
	}
	for _, tm := range want.Tmpfs {
		path, opts, _ := strings.Cut(tm, ":")
		if got, ok := c.HostConfig.Tmpfs[path]; !ok || got != opts {
			t.Errorf("%s: tmpfs %s is %q (present %v), want %q", service, path, got, ok, opts)
		}
	}
	if cpus, _ := strconv.ParseFloat(want.CPUs, 64); c.HostConfig.NanoCpus != int64(cpus*1e9) {
		t.Errorf("%s: %d nano-CPUs, want %s CPUs", service, c.HostConfig.NanoCpus, want.CPUs)
	}
	if c.HostConfig.Memory != memBytes(want.Memory) {
		t.Errorf("%s: memory %d, want %s", service, c.HostConfig.Memory, want.Memory)
	}
	if c.HostConfig.PidsLimit == nil || *c.HostConfig.PidsLimit != int64(want.Pids) {
		t.Errorf("%s: process limit %v, want %d", service, c.HostConfig.PidsLimit, want.Pids)
	}
	env := map[string]string{}
	for _, e := range c.Config.Env {
		k, v, _ := strings.Cut(e, "=")
		env[k] = v
	}
	for k, v := range want.Env {
		if env[k] != v {
			t.Errorf("%s: environment %s = %q, want %q", service, k, env[k], v)
		}
	}
	if want.Entrypoint != nil && strings.Join(c.Config.Entrypoint, "\x00") != strings.Join(want.Entrypoint, "\x00") {
		t.Errorf("%s: entrypoint %q, want %q", service, c.Config.Entrypoint, want.Entrypoint)
	}
	if len(want.Command) > 0 && strings.Join(c.Config.Cmd, "\x00") != strings.Join(want.Command, "\x00") {
		t.Errorf("%s: command %q, want %q", service, c.Config.Cmd, want.Command)
	}
	if c.Config.Labels[labLabel] != strconv.Itoa(os.Getpid()) {
		t.Errorf("%s: label %s = %q, want this process's ID %d", service, labLabel, c.Config.Labels[labLabel], os.Getpid())
	}
}

// TestBuildStartsCheckServiceAndProjectWorker: `lab.Start(t, lab.Build())` has the check service
// and the project worker answering with their tokens, from the lab's images and with compose.yaml's
// settings, and the engine calls them at their addresses with those tokens.
func TestBuildStartsCheckServiceAndProjectWorker(t *testing.T) {
	l := Start(t, Build())
	if l.Check == "" || l.ProjectWorker == "" {
		t.Fatalf("lab.Build() started no check service (%q) or no project worker (%q)", l.Check, l.ProjectWorker)
	}
	checkIP, projectIP := ip(t, l.Check, l.Net), ip(t, l.ProjectWorker, l.Net)
	for _, c := range []struct {
		name, addr, token, other string
	}{
		{"check service", checkIP, CheckToken, ProjectToken},
		{"project worker", projectIP, ProjectToken, CheckToken},
	} {
		if st, b := l.callService(t, c.addr, c.token); st != 400 || !strings.Contains(b, "MissingApiVersionParameter") {
			t.Errorf("the %s answered its token with %d, want 400 MissingApiVersionParameter: %s", c.name, st, b)
		}
		if st, b := l.callService(t, c.addr, c.other); st != 401 {
			t.Errorf("the %s answered another token with %d, want 401: %s", c.name, st, b)
		}
		if st, b := l.callService(t, c.addr, ""); st != 401 {
			t.Errorf("the %s answered no token with %d, want 401: %s", c.name, st, b)
		}
	}
	env := envOf(t, l.Engine)
	for k, want := range map[string]string{
		"AP_CHECK_URL": "http://" + checkIP + ":8080", "AP_CHECK_TOKEN": CheckToken,
		"AP_PROJECT_URL": "http://" + projectIP + ":8080", "AP_PROJECT_TOKEN": ProjectToken,
	} {
		if env[k] != want {
			t.Errorf("the engine has %s=%q, want %q", k, env[k], want)
		}
	}
	for container, image := range map[string]string{l.Check: "aptest-check:lab", l.ProjectWorker: "aptest-project:lab"} {
		if got := Docker(t, "inspect", "-f", "{{.Config.Image}}", container); got != image {
			t.Errorf("%s runs %s, want the lab's image %s", container, got, image)
		}
	}
	l.runningMatches(t, l.Check, "check")
	l.runningMatches(t, l.ProjectWorker, "project-worker")
}

// TestStoreSurvivesRestartEngine: a slice created before `l.RestartEngine(t)` is there after it.
// A tmpfs `/store` would be emptied by the kill; the store is a host folder, and the engine runs
// as the developer to write it.
func TestStoreSurvivesRestartEngine(t *testing.T) {
	l := Start(t)
	id := l.Create(t, Example)
	before := Docker(t, "inspect", "-f", "{{.State.StartedAt}}", l.Engine)
	l.RestartEngine(t)
	if after := Docker(t, "inspect", "-f", "{{.State.StartedAt}}", l.Engine); after == before {
		t.Errorf("RestartEngine did not restart the engine (started %s both times)", before)
	}
	if got := l.Slice(t, id)["id"]; got != id {
		t.Errorf("after RestartEngine the slice %s reads back as id %v", id, got)
	}
	if tm := Docker(t, "inspect", "-f", "{{json .HostConfig.Tmpfs}}", l.Engine); strings.Contains(tm, "/store") {
		t.Errorf("the engine's /store is a tmpfs: %s", tm)
	}
	var mounts []struct{ Type, Destination string }
	json.Unmarshal([]byte(Docker(t, "inspect", "-f", "{{json .Mounts}}", l.Engine)), &mounts)
	bound := false
	for _, m := range mounts {
		if m.Type == "bind" && m.Destination == "/store" {
			bound = true
		}
	}
	if !bound {
		t.Errorf("the engine's /store is not a host folder: %+v", mounts)
	}
	if u, want := Docker(t, "inspect", "-f", "{{.Config.User}}", l.Engine), fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()); u != want {
		t.Errorf("the engine runs as %q, want the developer's %s", u, want)
	}
}

// planted makes a container, a network and a volume named and labelled as a lab of process pid.
func planted(t *testing.T, pid int) (container, network, volume string) {
	t.Helper()
	container = fmt.Sprintf("aptest-lab-planted-%d-1", pid)
	network = fmt.Sprintf("aptest-lab-net-%d-1", pid)
	volume = fmt.Sprintf("aptest-lab-store-%d-1", pid)
	label := fmt.Sprintf("%s=%d", labLabel, pid)
	t.Cleanup(func() {
		Run("docker", "rm", "-f", container)
		Run("docker", "network", "rm", network)
		Run("docker", "volume", "rm", "-f", volume)
	})
	Docker(t, "create", "--name", container, "--label", label, "--network", "none", Pin(t, "alpine"), "true")
	Docker(t, "network", "create", "--internal", "--label", label, network)
	Docker(t, "volume", "create", "--label", label, volume)
	return
}

func exists(kind, name string) bool {
	_, code := Run("docker", kind, "inspect", name)
	return code == 0
}

// TestStartSweepsDeadLabsOnly: a container, network and volume named and labelled as a lab of a
// process that no longer runs are removed by the next `lab.Start`; those of a running process are
// left alone.
func TestStartSweepsDeadLabsOnly(t *testing.T) {
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	live := exec.Command("sleep", "3600")
	if err := live.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { live.Process.Kill(); live.Wait() })
	dc, dn, dv := planted(t, dead.Process.Pid)
	lc, ln, lv := planted(t, live.Process.Pid)

	Start(t)

	for kind, name := range map[string]string{"container": dc, "network": dn, "volume": dv} {
		if exists(kind, name) {
			t.Errorf("lab.Start left the %s %s of the ended process %d", kind, name, dead.Process.Pid)
		}
	}
	for kind, name := range map[string]string{"container": lc, "network": ln, "volume": lv} {
		if !exists(kind, name) {
			t.Errorf("lab.Start removed the %s %s of the running process %d", kind, name, live.Process.Pid)
		}
	}
}

// leftovers lists every container, network and volume of this process's labs, by label or by name.
func leftovers(t testing.TB) []string {
	t.Helper()
	pid := strconv.Itoa(os.Getpid())
	var left []string
	for _, kind := range []struct{ list, format string }{
		{"ps -a", "{{.Names}}"}, {"network ls", "{{.Name}}"}, {"volume ls", "{{.Name}}"},
	} {
		base := append(strings.Fields(kind.list), "--format", kind.format)
		for _, filter := range []string{"label=" + labLabel + "=" + pid, "name=aptest-lab-"} {
			out := Docker(t, append(base, "--filter", filter)...)
			for _, n := range strings.Fields(out) {
				if strings.HasPrefix(filter, "label=") || strings.Contains(n, "-"+pid+"-") {
					left = append(left, kind.list+": "+n)
				}
			}
		}
	}
	return left
}

// TestLabLeavesNothing: after the test, nothing of a lab with the build option is left: no
// container, network or volume named or labelled with this process (the host folders are the
// test's temporary folders, which the testing package removes and fails on when it cannot).
func TestLabLeavesNothing(t *testing.T) {
	var started bool
	t.Run("lab", func(t *testing.T) {
		l := Start(t, Build())
		started = l.Check != "" && l.ProjectWorker != ""
		if len(leftovers(t)) == 0 {
			t.Error("a running lab has no container, network or volume named or labelled with this process")
		}
	})
	if !started {
		t.Fatal("the lab with the build option did not start")
	}
	if left := leftovers(t); len(left) > 0 {
		t.Errorf("after the test the lab left %v", left)
	}
}
