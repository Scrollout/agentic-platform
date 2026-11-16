//go:build docker

// The step workers of one project (T054j, research D31, D32, D34): the skeleton, one container per
// step of the pattern as `ap build-workers` starts it, a grant file per container, its
// registration with the engine, and their removal.
package lab

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The labels `ap build-workers` gives a step container (T053g).
const (
	sliceLabel   = "org.agentic-platform.slice"
	stepLabel    = "org.agentic-platform.step"
	projectLabel = "org.agentic-platform.project"
)

// WritableRoot makes StepWorkers mount step's project root writable, `.git` still read-only: a
// test-only deviation for the planted US2.2 and US2.3 (research D34).
func WritableRoot(step string) Option {
	return func(o *options) { o.writableRoot = append(o.writableRoot, step) }
}

func projectHash(project string) string {
	h := sha256.Sum256([]byte(project))
	return hex.EncodeToString(h[:])[:12]
}

// stepWorkerName is T053g's name with the lab's prefix as the Compose project.
func stepWorkerName(slice, step, project string) string {
	return fmt.Sprintf("aptest-lab-%d-worker-%s-%s-%s", os.Getpid(), strings.ToLower(slice), step, projectHash(project))
}

// StepWorkers makes the project's skeleton in the lab's projects folder, starts one container per
// step of the pattern (aptest-agent:lab for agent steps, aptest-check:lab for procedures) with
// ServiceSettings and StepMounts, writes their grants, waits until each answers and registers it.
// They are removed when t ends, or earlier by StopStepWorkers.
func (l *Lab) StepWorkers(t testing.TB, slice, project string) {
	t.Helper()
	steps, err := patternSteps(l.root)
	if err != nil {
		t.Fatal(err)
	}
	mounts, err := StepMounts(l.root, l.projects, project, l.opts.writableRoot...)
	if err != nil {
		t.Fatal(err)
	}
	skeleton, err := SkeletonFolders(l.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range skeleton {
		if err := os.MkdirAll(filepath.Join(l.projects, project, f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h := host(runtimeArgs(), map[string]string{"engine": l.EngineIP, "gateway": l.stepModelHost()})
	agents := filepath.Join(l.root, "src", "agents")
	type started struct{ name, ip string }
	var all []started
	for _, s := range steps {
		kind, arg, _ := strings.Cut(s.Worker, ":")
		call := stepCallToken(slice, s.Name, project)
		service, image := "procedure-worker", "aptest-check:lab"
		if kind == "agent" {
			service, image = "step-agent", "aptest-agent:lab"
		}
		set, err := ServiceSettings(service, h)
		if err != nil {
			t.Fatal(err)
		}
		var extra []string
		if kind == "agent" {
			set.Env["STEP_AGENT"], set.Env["STEP_TOKEN"], set.Env["ENGINE_TOKEN"] = arg, call, ""
			extra = append(extra, "-v", agents+":/agents:ro") // compose.yaml's step-agent volume
		} else {
			set.Env["CHECK_PROCEDURE"], set.Env["CHECK_STEP"], set.Env["CHECK_TOKEN"] = arg, s.Name, call
		}
		for _, m := range mounts[s.Name] {
			v := m.Source + ":" + m.Target
			if m.ReadOnly {
				v += ":ro"
			}
			extra = append(extra, "-v", v)
		}
		l.writeGrant(t, slice, s.Name, project)
		name := stepWorkerName(slice, s.Name, project)
		flags, cmd := set.runArgs()
		a := append([]string{"run", "-d", "--name", name, "--network", l.Net}, labelArgs()...)
		a = append(a, "--label", sliceLabel+"="+slice, "--label", stepLabel+"="+s.Name, "--label", projectLabel+"="+project)
		a = append(append(append(a, flags...), extra...), image)
		t.Cleanup(func() { Run("docker", "rm", "-f", name) })
		Docker(t, append(a, cmd...)...)
		all = append(all, started{name, ip(t, name, l.Net)})
	}
	deadline := time.Now().Add(3 * time.Minute)
	for _, c := range all {
		for {
			if st, _ := l.direct(c.ip, "/health"); st > 0 {
				break
			}
			if time.Now().After(deadline) {
				logs, _ := Run("docker", "logs", c.name)
				t.Fatalf("the step worker %s did not answer: %s", c.name, logs)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	for _, s := range steps {
		l.registerStepWorker(t, slice, s.Name, project)
	}
}

// StopStepWorkers removes the project's step containers and their grants.
func (l *Lab) StopStepWorkers(t testing.TB, slice, project string) {
	t.Helper()
	steps, err := patternSteps(l.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if out, code := Run("docker", "rm", "-f", stepWorkerName(slice, s.Name, project)); code != 0 {
			t.Errorf("docker rm %s: %s", stepWorkerName(slice, s.Name, project), out)
		}
		os.Remove(filepath.Join(l.grants, grantName(slice, s.Name, project)))
	}
}

// writeGrant writes the grant the engine reads at each authentication of the worker's token
// (research D31): named by the token's SHA-256, holding the slice, step and call token, mode 600.
func (l *Lab) writeGrant(t testing.TB, slice, step, project string) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"slice": slice, "step": step, "call_token": stepCallToken(slice, step, project)})
	tmp := filepath.Join(l.grants, "."+grantName(slice, step, project)+".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(l.grants, grantName(slice, step, project))); err != nil {
		t.Fatal(err)
	}
}

func grantName(slice, step, project string) string {
	h := sha256.Sum256([]byte(stepWorkerToken(slice, step, project)))
	return hex.EncodeToString(h[:])
}

// registerStepWorker registers one step container with the engine, with its worker token, at its
// name (the engine runs without gVisor, so Docker's DNS finds it), through the client container.
func (l *Lab) registerStepWorker(t testing.TB, slice, step, project string) {
	t.Helper()
	body := map[string]string{"endpoint": "http://" + stepWorkerName(slice, step, project) + ":8080"}
	if st, _, b := l.Do("POST", "/workers", stepWorkerToken(slice, step, project), body, nil); st != 200 {
		t.Fatalf("registerWorker %s %s = %d: %s", slice, step, st, b)
	}
}

// registerStepWorkers registers every step container of this lab that is running (or paused)
// again, from its labels; it calls only the engine, never a step container.
func (l *Lab) registerStepWorkers(t testing.TB) {
	t.Helper()
	out := Docker(t, "ps", "--filter", "label="+labelKey+"="+fmt.Sprint(os.Getpid()), "--filter", "label="+stepLabel,
		"--filter", "network="+l.Net, "--format",
		`{{.Label "`+sliceLabel+`"}} {{.Label "`+stepLabel+`"}} {{.Label "`+projectLabel+`"}}`)
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 3 {
			l.registerStepWorker(t, f[0], f[1], f[2])
		}
	}
}
