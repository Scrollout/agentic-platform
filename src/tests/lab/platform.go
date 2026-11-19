//go:build docker

// The lab's platform (T054i, research D34): the build option, the lab's images built once per
// test process, the check service and the project worker started from ServiceSettings, the
// engine's restart, and the sweep of what a lab of an ended process left behind.
package lab

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// labelKey is the label every lab resource carries, with its process's ID (research D34).
const labelKey = "org.agentic-platform.lab"

// labelArgs is the `--label` flag of every container, network and volume a lab makes.
func labelArgs() []string { return []string{"--label", labelKey + "=" + strconv.Itoa(os.Getpid())} }

// Option changes what Start starts.
type Option func(*options)

type options struct {
	build        bool     // start the check service and the project worker from the lab's images
	writableRoot []string // steps whose project root StepWorkers mounts writable (WritableRoot)
	steps        string   // the step recording set (Steps), "" for none
}

// Build makes Start build the lab's images (once per test process) and start the check service
// and the project worker, which the engine then calls.
func Build() Option { return func(o *options) { o.build = true } }

var (
	imagesOnce sync.Once
	imagesErr  error
)

// buildImages builds the agent, check and project images from the source under test, tagged
// aptest-<name>:lab, unsigned and never pushed; once per process, whatever the outcome.
func buildImages(t testing.TB, root string) {
	t.Helper()
	imagesOnce.Do(func() {
		builds, err := ImageBuilds(root)
		if err != nil {
			imagesErr = err
			return
		}
		for _, b := range builds {
			args := []string{"buildx", "build", "--load", "-f", b.Dockerfile, "-t", b.Tag}
			for k, v := range b.Args {
				args = append(args, "--build-arg", k+"="+v)
			}
			start := time.Now()
			out, err := exec.Command("docker", append(args, b.Context)...).CombinedOutput()
			if err != nil {
				imagesErr = fmt.Errorf("docker buildx build %s: %v: %s", b.Tag, err, out)
				return
			}
			fmt.Fprintf(os.Stderr, "lab: built %s in %.1fs\n", b.Tag, time.Since(start).Seconds())
		}
	})
	if imagesErr != nil {
		t.Fatalf("the lab's images: %v", imagesErr)
	}
}

// host is the lab's Host: runsc when Docker has it, the developer's uid:gid, the given addresses.
func host(rt []string, addrs map[string]string) Host {
	return Host{Runsc: len(rt) > 0, User: fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), Addrs: addrs}
}

// runArgs is `docker run` flags for s; a multi-part entrypoint is its first part as --entrypoint
// and the rest before the command, which runs the same process.
func (s Settings) runArgs() (flags, command []string) {
	if s.Runtime != "" {
		flags = append(flags, "--runtime", s.Runtime)
	}
	if s.ReadOnly {
		flags = append(flags, "--read-only")
	}
	if s.User != "" {
		flags = append(flags, "--user", s.User)
	}
	for _, c := range s.CapDrop {
		flags = append(flags, "--cap-drop", c)
	}
	for _, o := range s.SecurityOpt {
		flags = append(flags, "--security-opt", o)
	}
	for _, m := range s.Tmpfs {
		flags = append(flags, "--tmpfs", m)
	}
	if s.CPUs != "" {
		flags = append(flags, "--cpus", s.CPUs)
	}
	if s.Memory != "" {
		flags = append(flags, "--memory", s.Memory)
	}
	if s.Pids != 0 {
		flags = append(flags, "--pids-limit", strconv.Itoa(s.Pids))
	}
	for k, v := range s.Env {
		flags = append(flags, "-e", k+"="+v)
	}
	if len(s.Entrypoint) > 0 {
		flags = append(flags, "--entrypoint", s.Entrypoint[0])
		command = append(command, s.Entrypoint[1:]...)
	}
	if s.HealthCmd != "" {
		flags = append(flags, "--health-cmd", s.HealthCmd, "--health-interval", "5s", "--health-timeout", "5s",
			"--health-retries", "60", "--health-start-period", s.Healthy)
	}
	return flags, append(command, s.Command...)
}

// startDurable starts the lab's own durable-store, durable-schema and durable, as compose.yaml
// declares them, each on the lab's two internal networks (the store's, and `durable`, where the
// server has the alias `durable`), and waits until the server is healthy. It returns the name of
// the durable network, which the engine joins. Costs about 200 MiB and a few seconds per lab.
func (l *Lab) startDurable(t *testing.T) (durableNet string) {
	t.Helper()
	netArgs := func(name string) {
		Docker(t, append(append([]string{"network", "create", "--internal", "-o", "com.docker.network.bridge.gateway_mode_ipv4=isolated"}, labelArgs()...), name)...)
		t.Cleanup(func() { Run("docker", "network", "rm", name) })
	}
	durableNet, storeNet := uniq("aptest-lab-durable-net"), uniq("aptest-lab-store-net")
	netArgs(durableNet)
	netArgs(storeNet)

	// The password file as ADR 0070 has it: a private folder (mode 700) holding a mode-644 file.
	dir, err := os.MkdirTemp("", "aptest-lab-durable-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pw := make([]byte, 16)
	if _, err := rand.Read(pw); err != nil {
		t.Fatal(err)
	}
	pwFile := filepath.Join(dir, "durable-store-password")
	if err := os.WriteFile(pwFile, []byte(hex.EncodeToString(pw)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(pwFile, 0o644); err != nil { // not narrowed by the umask
		t.Fatal(err)
	}
	secret := []string{"-v", pwFile + ":/run/secrets/durable-store-password:ro"}
	durableDir := filepath.Join(l.root, "src", "deploy", "durable")

	data := uniq("aptest-lab-durable-data")
	Docker(t, append(append([]string{"volume", "create"}, labelArgs()...), data)...)
	t.Cleanup(func() { Run("docker", "volume", "rm", "-f", data) })

	run := func(service, image string, detach bool, networks []string, alias string, extra ...string) string {
		s, err := DurableSettings(service)
		if err != nil {
			t.Fatal(err)
		}
		flags, cmd := s.runArgs()
		name := uniq("aptest-lab-" + service)
		// With a second network the container is created, connected, then started, so both networks
		// are attached before it starts, as Compose does.
		verb := "run"
		if len(networks) > 1 {
			verb = "create"
		}
		a := []string{verb, "--name", name, "--network", networks[0]}
		if detach && verb == "run" {
			a = append(a, "-d")
		}
		if alias != "" {
			a = append(a, "--network-alias", alias)
		}
		a = append(append(append(append(a, labelArgs()...), flags...), extra...), image)
		t.Cleanup(func() { Run("docker", "rm", "-f", name) })
		Docker(t, append(a, cmd...)...)
		for _, n := range networks[1:] {
			Docker(t, "network", "connect", n, name)
		}
		if verb == "create" {
			Docker(t, "start", name)
		}
		return name
	}
	healthy := func(name string) {
		deadline := time.Now().Add(180 * time.Second)
		for time.Now().Before(deadline) {
			if out, _ := Run("docker", "inspect", "-f", "{{.State.Health.Status}}", name); out == "healthy" {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		logs, _ := Run("docker", "logs", name)
		t.Fatalf("%s did not become healthy: %s", name, logs)
	}

	store := run("durable-store", Pin(t, "postgres"), true, []string{storeNet}, "durable-store",
		append([]string{"-v", data + ":/var/lib/postgresql/data"}, secret...)...)
	healthy(store)
	// The schema job runs to its end (exit 0) before the server starts.
	run("durable-schema", Pin(t, "temporal-admin-tools"), false, []string{storeNet}, "",
		append([]string{"-v", filepath.Join(durableDir, "schema.sh") + ":/durable/schema.sh:ro"}, secret...)...)
	server := run("durable", Pin(t, "temporal-server"), true, []string{durableNet, storeNet}, "durable",
		append([]string{"-v", filepath.Join(durableDir, "dynamicconfig.yaml") + ":/etc/temporal/config/dynamicconfig/ap.yaml:ro"}, secret...)...)
	healthy(server)
	return durableNet
}

// startServices starts the check service and the project worker as compose.yaml declares them,
// over the lab's projects folder (read-only for the check service), the project worker's records
// on a lab volume, and waits until both answer.
func (l *Lab) startServices(t *testing.T, rt []string) {
	t.Helper()
	buildImages(t, l.root)
	h := host(rt, nil)
	state := uniq("aptest-lab-pwstate")
	Docker(t, append(append([]string{"volume", "create"}, labelArgs()...), state)...)
	t.Cleanup(func() { Run("docker", "volume", "rm", "-f", state) })

	run := func(name, service, image string, mounts ...string) {
		s, err := ServiceSettings(service, h)
		if err != nil {
			t.Fatal(err)
		}
		flags, cmd := s.runArgs()
		a := append([]string{"run", "-d", "--name", name, "--network", l.Net}, labelArgs()...)
		a = append(append(append(a, flags...), mounts...), image)
		t.Cleanup(func() { Run("docker", "rm", "-f", name) })
		Docker(t, append(a, cmd...)...)
	}
	l.Check = uniq("aptest-lab-check")
	run(l.Check, "check", "aptest-check:lab", "-v", l.projects+":/projects:ro")
	l.ProjectWorker = uniq("aptest-lab-project")
	run(l.ProjectWorker, "project-worker", "aptest-project:lab",
		"-v", l.projects+":/projects", "-v", state+":/var/lib/project-worker")
	l.checkIP, l.projectIP = ip(t, l.Check, l.Net), ip(t, l.ProjectWorker, l.Net)
}

// servicesUp is whether the check service and the project worker answer (any HTTP status).
func (l *Lab) servicesUp() bool {
	for _, addr := range []string{l.checkIP, l.projectIP} {
		if st, _ := l.direct(addr, "/"); st <= 0 {
			return false
		}
	}
	return true
}

// RestartEngine kills and starts the engine, reads its address again and waits until it answers.
// The store is a host folder, so what the engine stored before is there after.
func (l *Lab) RestartEngine(t testing.TB) {
	t.Helper()
	Docker(t, "kill", l.Engine)
	Docker(t, "start", l.Engine)
	l.EngineIP = ip(t, l.Engine, l.Net)
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if st, _, _ := l.Do("GET", "/providers/Agentic.Engine/operations", ClientToken, nil, nil); st == 200 {
			l.registerStepWorkers(t) // registrations live in the engine's memory, as `ap resume` does
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	logs, _ := Run("docker", "logs", l.Engine)
	t.Fatalf("the engine did not answer after its restart: %s", logs)
}

// labPID is the process ID in a lab resource's name, aptest-lab-<what>-<pid>-<n>, or 0.
func labPID(name string) int {
	if !strings.HasPrefix(name, "aptest-lab-") {
		return 0
	}
	f := strings.Split(name, "-")
	if len(f) < 5 {
		return 0
	}
	if _, err := strconv.Atoi(f[len(f)-1]); err != nil {
		return 0
	}
	pid, err := strconv.Atoi(f[len(f)-2])
	if err != nil {
		return 0
	}
	return pid
}

// running is whether the process pid runs (or exists under another user).
func running(pid int) bool {
	if pid <= 0 {
		return true // unknown: leave it alone
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// sweep removes every container, network and volume of a lab whose process no longer runs, found
// by its label or by its name (replay.sh cannot label its container), and leaves the others.
func sweep(t testing.TB) {
	t.Helper()
	for _, kind := range []struct {
		list, format string
		rm           []string
	}{
		{"ps -a", "{{.Names}}\t{{.Label \"" + labelKey + "\"}}", []string{"rm", "-f"}},
		{"network ls", "{{.Name}}\t{{.Label \"" + labelKey + "\"}}", []string{"network", "rm"}},
		{"volume ls", "{{.Name}}\t{{.Label \"" + labelKey + "\"}}", []string{"volume", "rm", "-f"}},
	} {
		dead := map[string]bool{}
		for _, filter := range []string{"label=" + labelKey, "name=aptest-lab-"} {
			out, code := Run("docker", append(strings.Fields(kind.list), "--format", kind.format, "--filter", filter)...)
			if code != 0 {
				t.Fatalf("docker %s: %s", kind.list, out)
			}
			for _, line := range strings.Split(out, "\n") {
				name, label, _ := strings.Cut(strings.TrimSpace(line), "\t")
				if name == "" {
					continue
				}
				pid := labPID(name)
				if p, err := strconv.Atoi(label); err == nil {
					pid = p
				}
				if pid > 0 && !running(pid) {
					dead[name] = true
				}
			}
		}
		for name := range dead {
			if out, code := Run("docker", append(kind.rm, name)...); code != 0 {
				t.Logf("lab: could not remove the stale %s %s: %s", kind.list, name, out)
			}
		}
	}
}
