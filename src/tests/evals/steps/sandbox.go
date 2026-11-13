package steps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/agentic-platform/engine/checks"
)

// DockerRunner is a checks.GoRunner that runs the go toolchain in a throwaway container with no
// network, a read-only project and a read-only root, for projects a model wrote: the live evals
// grade code the agent produced, and a hostile test must not run on the host. The project is
// mounted at its own absolute path, so the paths in a stack trace are the ones the engine's rerun
// (checks.FindingsRerun) looks for. Image is any image with go on its PATH (the step agent image).
type DockerRunner struct {
	Image   string
	Timeout time.Duration
	// Label is put on every container, so a killed run can be cleaned up by label.
	Label string
}

var runnerSeq atomic.Int64

// Go runs `go args...` in dir. An argument that follows -o is a folder outside the project the go
// command writes to (TestsBuild's test binaries); it is mounted writable.
func (d DockerRunner) Go(dir string, args ...string) (checks.GoResult, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return checks.GoResult{}, err
	}
	timeout := d.Timeout
	if timeout == 0 {
		timeout = checks.DefaultGoTimeout
	}
	name := fmt.Sprintf("apeval-go-%d-%d", time.Now().UnixNano(), runnerSeq.Add(1))
	a := []string{"run", "--rm", "--name", name, "--network", "none", "--read-only",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--memory", "2g", "--pids-limit", "1024", "--tmpfs", fmt.Sprintf("/tmp:uid=%d,gid=%d,exec,size=2g", os.Getuid(), os.Getgid()),
		"-e", "HOME=/tmp/home", "-e", "GOCACHE=/tmp/gocache", "-e", "GOPATH=/tmp/gopath", "-e", "GOTELEMETRY=off",
		"-e", "GOFLAGS=-buildvcs=false", "-e", "GOPROXY=off", "-e", "GOWORK=off", "-e", "GOTOOLCHAIN=local", "-e", "CGO_ENABLED=0",
		"-v", abs + ":" + abs + ":ro", "-w", abs}
	if d.Label != "" {
		a = append(a, "--label", d.Label)
	}
	for i, x := range args {
		if x == "-o" && i+1 < len(args) {
			out := filepath.Clean(args[i+1])
			a = append(a, "-v", out+":"+out)
		}
	}
	a = append(a, "--entrypoint", "go", d.Image)
	a = append(a, args...)

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
