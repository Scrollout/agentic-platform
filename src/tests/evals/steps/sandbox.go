package steps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/agentic-platform/engine/checks"
)

// DockerRunner is a checks.GoRunner that runs the go toolchain in a throwaway container with no
// network, a read-only project and a read-only root, for projects a model wrote: the live evals
// grade code the agent produced, and a hostile test must not run on the host. The project is
// mounted at its own absolute path, so the paths the engine's checks read (go.mod, a finding's
// file) and those in the go command's output are the same inside and out. Image is any image with go on its PATH (the step agent image).
type DockerRunner struct {
	Image   string
	Timeout time.Duration
	// Label is put on every container, so a killed run can be cleaned up by label.
	Label string
	// Cache is a host folder mounted writable as GOCACHE (build phase only); empty: none, GOCACHE
	// stays on the container's fresh tmpfs.
	Cache string
	// Root is the folder mounted read-only (at its own path); empty: the dir of the call. A run
	// call's dir is a package folder inside Root.
	Root string
}

var runnerSeq atomic.Int64

// For returns a copy of d whose calls mount root read-only and share the build-cache folder cache
// ("" for none): the VariantRun.Runner of the live evals is d.For. d itself is unchanged.
func (d DockerRunner) For(root, cache string) checks.GoRunner {
	d.Root, d.Cache = root, cache
	return d
}

// Go runs `go args...` in dir, in a container of its own. An argument that follows -o is a folder
// outside the project the go command writes to (test binaries), and the folder of a
// -coverprofile= file is where a witness run writes its coverage profile
// (checks.NoBlockingFindingOpen); both are mounted writable. The argument after -t (go tool
// test2json -t <binary>) is a test binary, whose folder is mounted read-only.
func (d DockerRunner) Go(dir string, args ...string) (checks.GoResult, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return checks.GoResult{}, err
	}
	for _, p := range []*string{&d.Root, &d.Cache} {
		if *p != "" {
			if *p, err = filepath.Abs(*p); err != nil {
				return checks.GoResult{}, err
			}
		}
	}
	timeout := d.Timeout
	if timeout == 0 {
		timeout = checks.DefaultGoTimeout
	}
	name := fmt.Sprintf("apeval-go-%d-%d", time.Now().UnixNano(), runnerSeq.Add(1))
	a := d.dockerArgs(name, abs, args...)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", a...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	if ctx.Err() != nil {
		exec.Command("docker", "rm", "-f", name).Run()
		return checks.GoResult{}, fmt.Errorf("timed out after %s", timeout)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if exit.ExitCode() == 125 || exit.ExitCode() == 126 || exit.ExitCode() == 127 {
			return checks.GoResult{}, fmt.Errorf("docker could not run go: %s", bytes.TrimSpace(out.Bytes()))
		}
		return checks.GoResult{ExitCode: exit.ExitCode(), Output: out.String()}, nil
	}
	if err != nil {
		return checks.GoResult{}, err
	}
	return checks.GoResult{Output: out.String()}, nil
}

// dockerArgs is the full `docker` argument list for one call (name: the container name; dir: the
// call's absolute dir), so tests can check it without running docker. Root (or dir) is mounted
// read-only and dir is the working directory; Cache, when set, is mounted writable as GOCACHE;
// a -o folder and a -coverprofile= file's folder are mounted writable, and the folder of the
// binary after -t read-only. No folder is mounted twice.
func (d DockerRunner) dockerArgs(name, dir string, args ...string) []string {
	root := dir
	if d.Root != "" {
		root = filepath.Clean(d.Root)
	}
	gocache := "/tmp/gocache"
	if d.Cache != "" {
		gocache = filepath.Clean(d.Cache)
	}
	a := []string{"run", "--rm", "--name", name, "--network", "none", "--read-only",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--memory", "2g", "--pids-limit", "1024", "--tmpfs", fmt.Sprintf("/tmp:uid=%d,gid=%d,exec,size=2g", os.Getuid(), os.Getgid()),
		"-e", "HOME=/tmp/home", "-e", "GOCACHE=" + gocache, "-e", "GOPATH=/tmp/gopath", "-e", "GOTELEMETRY=off",
		"-e", "GOFLAGS=-buildvcs=false", "-e", "GOPROXY=off", "-e", "GOWORK=off", "-e", "GOTOOLCHAIN=local", "-e", "CGO_ENABLED=0",
		"-v", root + ":" + root + ":ro", "-w", dir}
	mounted := map[string]bool{root: true}
	mount := func(folder, mode string) {
		if mounted[folder] {
			return
		}
		mounted[folder] = true
		a = append(a, "-v", folder+":"+folder+mode)
	}
	if d.Cache != "" {
		mount(gocache, "")
	}
	if d.Label != "" {
		a = append(a, "--label", d.Label)
	}
	for i, x := range args {
		if x == "-o" && i+1 < len(args) {
			mount(filepath.Clean(args[i+1]), "")
		}
		if x == "-t" && i+1 < len(args) {
			mount(filepath.Dir(filepath.Clean(args[i+1])), ":ro")
		}
		if p, ok := strings.CutPrefix(x, "-coverprofile="); ok {
			mount(filepath.Dir(filepath.Clean(p)), "")
		}
	}
	a = append(a, "--entrypoint", "go", d.Image)
	return append(a, args...)
}
