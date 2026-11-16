//go:build docker

// T054j behind the `docker` tag: the step containers of one project, their grants and
// registrations, a build through them, their registration again after an engine restart, their
// removal, and the container cap. The seams are listed in stepworkers_test.go.
package lab

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// labProject is the project every test here builds.
const labProject = "notes"

// stepContainer is the T053g name with the lab's prefix as the Compose project.
func stepContainer(slice, step, proj string) string {
	h := sha256.Sum256([]byte(proj))
	return fmt.Sprintf("aptest-lab-%d-worker-%s-%s-%s", os.Getpid(), strings.ToLower(slice), step, hex.EncodeToString(h[:])[:12])
}

// approvedExample creates the example slice, answers it as US1 does and approves its three parts.
func approvedExample(t *testing.T, l *Lab) string {
	t.Helper()
	id := l.Create(t, Example)
	body := map[string]any{"answers": []any{
		map[string]any{"question": "Q-1", "answer": map[string]any{"option": 0}},
		map[string]any{"question": "Q-2", "answer": map[string]any{"text": "400"}},
	}}
	if op := l.Wait(t, l.Accept(t, "POST", "/slices/"+id+"/answers", body)); op.Status != "succeeded" {
		t.Fatalf("answerQuestions: %+v (error %+v)", op, op.Error)
	}
	for _, p := range []string{"goals", "rules", "contract"} {
		l.Approve(t, id, p)
	}
	if d := l.Get(t, id); d.State != "approved" {
		t.Fatalf("the example slice is %q, want approved", d.State)
	}
	return id
}

// runningLabContainers is every running container of this process's labs, by label or by name
// (the replay server's carries no label).
func runningLabContainers(t testing.TB) []string {
	t.Helper()
	pid := strconv.Itoa(os.Getpid())
	set := map[string]bool{}
	for _, n := range strings.Fields(Docker(t, "ps", "--format", "{{.Names}}", "--filter", "label="+labLabel+"="+pid)) {
		set[n] = true
	}
	for _, n := range strings.Fields(Docker(t, "ps", "--format", "{{.Names}}", "--filter", "name=aptest-lab-")) {
		if strings.Contains(n, "-"+pid+"-") {
			set[n] = true
		}
	}
	var out []string
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

type runEvent struct {
	Seq   int    `json:"seq"`
	Kind  string `json:"kind"`
	Step  string `json:"step"`
	Check *struct {
		Name   string `json:"name"`
		Passed bool   `json:"passed"`
	} `json:"check"`
}

func (l *Lab) runEvents(t testing.TB, run string) []runEvent {
	t.Helper()
	var all []runEvent
	tok := ""
	for i := 0; i < 100; i++ {
		p := "/runs/" + url.PathEscape(run) + "/events"
		if tok != "" {
			p += "?page_token=" + url.QueryEscape(tok)
		}
		st, _, b := l.Do("GET", p, ClientToken, nil, nil)
		if st != 200 {
			t.Fatalf("listRunEvents %s = %d: %s", run, st, b)
		}
		var page struct {
			Items []runEvent `json:"items"`
			Next  string     `json:"next_page_token"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			t.Fatal(err)
		}
		all = append(all, page.Items...)
		if page.Next == "" || len(page.Items) == 0 {
			break
		}
		tok = page.Next
	}
	return all
}

func (l *Lab) runStatus(t testing.TB, run string) string {
	t.Helper()
	st, _, b := l.Do("GET", "/runs/"+url.PathEscape(run), ClientToken, nil, nil)
	if st != 200 {
		t.Fatalf("getRun %s = %d: %s", run, st, b)
	}
	var r struct {
		Status string `json:"status"`
	}
	json.Unmarshal(b, &r)
	return r.Status
}

func has(evs []runEvent, kind, step string) bool {
	for _, e := range evs {
		if e.Kind == kind && e.Step == step {
			return true
		}
	}
	return false
}

// waitEvents polls the run's record until stop holds, within limit.
func (l *Lab) waitEvents(t testing.TB, run string, limit time.Duration, poll time.Duration, stop func([]runEvent) bool) []runEvent {
	t.Helper()
	deadline := time.Now().Add(limit)
	var evs []runEvent
	for time.Now().Before(deadline) {
		if evs = l.runEvents(t, run); stop(evs) {
			return evs
		}
		time.Sleep(poll)
	}
	t.Fatalf("run %s: the wanted events did not come within %s; record %+v", run, limit, evs)
	return nil
}

func (l *Lab) startRun(t testing.TB, slice string) string {
	t.Helper()
	st, _, b := l.Do("POST", "/slices/"+slice+"/runs", ClientToken, map[string]string{"project": labProject}, nil)
	if st != 202 {
		t.Fatalf("startRun %s %s = %d, want 202: %s", slice, labProject, st, b)
	}
	var op Operation
	if err := json.Unmarshal(b, &op); err != nil || op.Target == "" {
		t.Fatalf("startRun: no operation with a target: %s", b)
	}
	return op.Target
}

// commits is the number of commits in the project's version history, read with the project
// worker's image (its git) as the developer.
func (l *Lab) commits(t testing.TB) int {
	t.Helper()
	out := Docker(t, "run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "-e", "HOME=/tmp", "--tmpfs", "/tmp",
		"-v", filepath.Join(l.projects, labProject)+":/p:ro", "--entrypoint", "git", "aptest-project:lab",
		"-c", "safe.directory=*", "-C", "/p", "rev-list", "--count", "HEAD")
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("git rev-list: %s", out)
	}
	return n
}

type stepContainerInfo struct {
	Config struct {
		Image  string
		User   string
		Env    []string
		Labels map[string]string
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
	Mounts []struct {
		Type, Source, Destination string
		RW                        bool
	}
	State struct{ Running bool }
}

func inspectStep(t testing.TB, name string) stepContainerInfo {
	t.Helper()
	var c stepContainerInfo
	if err := json.Unmarshal([]byte(Docker(t, "inspect", "-f", "{{json .}}", name)), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

type grant struct {
	Slice     string `json:"slice"`
	Step      string `json:"step"`
	CallToken string `json:"call_token"`
}

// readGrants reads the lab's grant files, by step; each name must be a SHA-256 in hex.
func (l *Lab) readGrants(t testing.TB) map[string]grant {
	t.Helper()
	ents, err := os.ReadDir(l.grants)
	if err != nil {
		t.Fatal(err)
	}
	hexName := regexp.MustCompile(`^[0-9a-f]{64}$`)
	out := map[string]grant{}
	for _, e := range ents {
		if !hexName.MatchString(e.Name()) {
			t.Errorf("grant file %q is not named by a SHA-256", e.Name())
			continue
		}
		b, err := os.ReadFile(filepath.Join(l.grants, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var g grant
		if err := json.Unmarshal(b, &g); err != nil || g.Step == "" || g.CallToken == "" {
			t.Errorf("grant %s does not hold slice, step and call token: %s", e.Name(), b)
			continue
		}
		if _, dup := out[g.Step]; dup {
			t.Errorf("two grants for the step %s", g.Step)
		}
		out[g.Step] = g
	}
	return out
}

// checkStepContainers checks the five containers of the project: T053g names and labels, the
// lab's images, compose.yaml's settings with the per-container values `ap build-workers` adds,
// StepMounts' mounts, and one grant each, holding the container's call token.
func (l *Lab) checkStepContainers(t testing.TB, slice string, writableRoot ...string) {
	t.Helper()
	root := Root(t)
	mounts, err := StepMounts(root, l.projects, labProject, writableRoot...)
	if err != nil {
		t.Fatal(err)
	}
	grants := l.readGrants(t)
	runtimes, _ := Run("docker", "info", "--format", "{{json .Runtimes}}")
	h := Host{Runsc: strings.Contains(runtimes, `"runsc"`), User: fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		Addrs: map[string]string{"engine": l.EngineIP, "gateway": ip(t, l.gateway, l.Net)}}
	for _, s := range readPattern(t, root) {
		name := stepContainer(slice, s.Name, labProject)
		c := inspectStep(t, name)
		if !c.State.Running {
			t.Errorf("%s is not running", name)
		}
		for k, v := range map[string]string{"org.agentic-platform.slice": slice, "org.agentic-platform.step": s.Name,
			"org.agentic-platform.project": labProject, labLabel: strconv.Itoa(os.Getpid())} {
			if c.Config.Labels[k] != v {
				t.Errorf("%s: label %s = %q, want %q", name, k, c.Config.Labels[k], v)
			}
		}
		kind, arg, _ := strings.Cut(s.Worker, ":")
		service, image := "procedure-worker", "aptest-check:lab"
		if kind == "agent" {
			service, image = "step-agent", "aptest-agent:lab"
		}
		if c.Config.Image != image {
			t.Errorf("%s runs %s, want %s", name, c.Config.Image, image)
		}
		want, err := ServiceSettings(service, h)
		if err != nil {
			t.Fatal(err)
		}
		g, granted := grants[s.Name]
		if !granted {
			t.Errorf("no grant for the step %s", s.Name)
		} else if g.Slice != slice {
			t.Errorf("the grant of %s names the slice %q, want %s", s.Name, g.Slice, slice)
		}
		if kind == "agent" {
			want.Env["STEP_AGENT"], want.Env["STEP_TOKEN"], want.Env["ENGINE_TOKEN"] = arg, g.CallToken, ""
		} else {
			want.Env["CHECK_PROCEDURE"], want.Env["CHECK_STEP"], want.Env["CHECK_TOKEN"] = arg, s.Name, g.CallToken
		}
		env := map[string]string{}
		for _, e := range c.Config.Env {
			k, v, _ := strings.Cut(e, "=")
			env[k] = v
		}
		for k, v := range want.Env {
			if env[k] != v {
				t.Errorf("%s: environment %s = %q, want %q", name, k, env[k], v)
			}
		}
		rt := c.HostConfig.Runtime
		if rt == "runc" {
			rt = ""
		}
		if rt != want.Runtime || c.HostConfig.ReadonlyRootfs != want.ReadOnly || c.Config.User != want.User {
			t.Errorf("%s: runtime %q read-only %v user %q, want %q %v %q", name, c.HostConfig.Runtime,
				c.HostConfig.ReadonlyRootfs, c.Config.User, want.Runtime, want.ReadOnly, want.User)
		}
		if strings.Join(sorted(c.HostConfig.CapDrop), ",") != strings.Join(sorted(want.CapDrop), ",") ||
			strings.Join(normalOpts(c.HostConfig.SecurityOpt), ",") != strings.Join(normalOpts(want.SecurityOpt), ",") {
			t.Errorf("%s: drops %v with %v, want %v with %v", name, c.HostConfig.CapDrop, c.HostConfig.SecurityOpt, want.CapDrop, want.SecurityOpt)
		}
		for _, tm := range want.Tmpfs {
			p, opts, _ := strings.Cut(tm, ":")
			if c.HostConfig.Tmpfs[p] != opts {
				t.Errorf("%s: tmpfs %s = %q, want %q", name, p, c.HostConfig.Tmpfs[p], opts)
			}
		}
		cpus, _ := strconv.ParseFloat(want.CPUs, 64)
		if c.HostConfig.NanoCpus != int64(cpus*1e9) || c.HostConfig.Memory != memBytes(want.Memory) ||
			c.HostConfig.PidsLimit == nil || *c.HostConfig.PidsLimit != int64(want.Pids) {
			t.Errorf("%s: limits %d nano-CPUs, %d bytes, %v processes; want %s, %s, %d", name,
				c.HostConfig.NanoCpus, c.HostConfig.Memory, c.HostConfig.PidsLimit, want.CPUs, want.Memory, want.Pids)
		}
		got := map[string]Mount{}
		for _, m := range c.Mounts {
			got[m.Destination] = Mount{Source: m.Source, Target: m.Destination, ReadOnly: !m.RW}
		}
		// The project mounts (StepMounts) and the service's own volumes from compose.yaml (the
		// step agents' read-only /agents), nothing else.
		expect := append(append([]Mount(nil), mounts[s.Name]...), composeVolumes(t, root, service)...)
		if len(got) != len(expect) {
			t.Errorf("%s has %d mounts, want %d: %+v", name, len(got), len(expect), c.Mounts)
		}
		for _, m := range expect {
			if got[m.Target] != m {
				t.Errorf("%s: mount at %s is %+v, want %+v", name, m.Target, got[m.Target], m)
			}
		}
	}
	if len(grants) != len(readPattern(t, root)) {
		t.Errorf("%d grant files, want one per step", len(grants))
	}
}

// TestStepWorkersBuildProcedures: before StepWorkers a build is refused WorkersNotReady; after it
// the five containers run as `ap build-workers` starts them, at most 12 lab containers run, the
// build accepts contract-check and generate (procedures, no model) with one commit each, and with
// no step recording the tests step is rejected and the replay server lists its request.
// StopStepWorkers then removes the project's five containers and nothing else.
func TestStepWorkersBuildProcedures(t *testing.T) {
	l := Start(t, Build())
	slice := approvedExample(t, l)
	l.Refused(t, "POST", "/slices/"+slice+"/runs", map[string]string{"project": labProject}, 409, "WorkersNotReady")

	l.StepWorkers(t, slice, labProject)
	l.checkStepContainers(t, slice)
	if n := runningLabContainers(t); len(n) > 12 {
		t.Errorf("%d lab containers run during the build, at most 12: %v", len(n), n)
	}
	for _, f := range mustSkeleton(t) {
		if fi, err := os.Stat(filepath.Join(l.projects, labProject, f)); err != nil || !fi.IsDir() {
			t.Errorf("the skeleton folder %s is missing: %v", f, err)
		}
	}

	run := l.startRun(t, slice)
	evs := l.waitEvents(t, run, 10*time.Minute, 2*time.Second, func(e []runEvent) bool {
		return has(e, "StepRejected", "tests") || has(e, "StepRejected", "contract-check") || has(e, "StepRejected", "generate") ||
			has(e, "RunCompleted", "") || l.runStatus(t, run) != "running"
	})
	if n := runningLabContainers(t); len(n) > 12 {
		t.Errorf("%d lab containers run during the build, at most 12: %v", len(n), n)
	}
	for _, s := range []string{"contract-check", "generate"} {
		if !has(evs, "StepAccepted", s) {
			t.Errorf("the run did not accept %s: %+v", s, evs)
		}
	}
	if !has(evs, "StepRejected", "tests") && !has(evs, "StepStarted", "tests") {
		t.Errorf("the run did not reach the tests step: %+v", evs)
	}
	if n := l.commits(t); n != 2 {
		t.Errorf("the project's .git holds %d commits, want 2 (contract-check and generate)", n)
	}
	if m := l.MissesJSON(t); !strings.Contains(m, "step tests") {
		t.Errorf("the replay server lists no request of the tests step: %s", m)
	}

	before := runningLabContainers(t)
	l.StopStepWorkers(t, slice, labProject)
	after := runningLabContainers(t)
	gone := map[string]bool{}
	for _, s := range readPattern(t, Root(t)) {
		gone[stepContainer(slice, s.Name, labProject)] = true
		if _, code := Run("docker", "container", "inspect", stepContainer(slice, s.Name, labProject)); code == 0 {
			t.Errorf("StopStepWorkers left %s", stepContainer(slice, s.Name, labProject))
		}
	}
	for _, n := range before {
		kept := false
		for _, a := range after {
			kept = kept || a == n
		}
		if !gone[n] && !kept {
			t.Errorf("StopStepWorkers removed %s, which is not one of the project's step containers", n)
		}
	}
	if len(after) != len(before)-5 {
		t.Errorf("StopStepWorkers: %d lab containers before, %d after; want 5 fewer", len(before), len(after))
	}
}

// composeVolumes is the bind mounts compose.yaml gives the service, each source read from
// src/deploy as Compose reads it.
func composeVolumes(t testing.TB, root, service string) []Mount {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "src", "deploy", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]struct {
			Volumes []struct {
				Type     string `yaml:"type"`
				Source   string `yaml:"source"`
				Target   string `yaml:"target"`
				ReadOnly bool   `yaml:"read_only"`
			} `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	svc, ok := doc.Services[service]
	if !ok {
		t.Fatalf("compose.yaml has no service %s", service)
	}
	var out []Mount
	for _, v := range svc.Volumes {
		if v.Type != "bind" {
			t.Fatalf("compose.yaml's %s has a %s volume, which the lab does not mount", service, v.Type)
		}
		out = append(out, Mount{Source: filepath.Clean(filepath.Join(root, "src", "deploy", v.Source)), Target: v.Target, ReadOnly: v.ReadOnly})
	}
	return out
}

func mustSkeleton(t testing.TB) []string {
	t.Helper()
	f, err := SkeletonFolders(Root(t))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestRestartEngineRegistersStepWorkersAgain: an engine killed while generate runs comes back
// with the running step containers registered again (registrations live in memory), so the
// interrupted run is resumed (resumeRun 202) and accepts generate.
//
// The kill falls inside generate's call every time: the generate container is paused (docker
// pause) before the run starts, so the engine's call to it cannot be answered, and it is unpaused
// only after the restart. So RestartEngine must register the containers again without waiting
// on a step container's own answer (registration is a call to the engine).
func TestRestartEngineRegistersStepWorkersAgain(t *testing.T) {
	l := Start(t, Build())
	slice := approvedExample(t, l)
	l.StepWorkers(t, slice, labProject)
	gen := stepContainer(slice, "generate", labProject)
	Docker(t, "pause", gen)
	paused := true
	unpause := func() {
		if paused {
			Run("docker", "unpause", gen)
			paused = false
		}
	}
	t.Cleanup(unpause)
	run := l.startRun(t, slice)
	evs := l.waitEvents(t, run, 5*time.Minute, time.Second, func(e []runEvent) bool { return has(e, "StepStarted", "generate") })
	if has(evs, "StepAccepted", "generate") {
		t.Fatal("generate was accepted while its container was paused")
	}
	time.Sleep(3 * time.Second) // the engine's call to the paused worker is under way
	l.RestartEngine(t)
	unpause()
	if st := l.runStatus(t, run); st != "interrupted" {
		t.Fatalf("after the restart the run is %q, want interrupted", st)
	}
	st, _, b := l.Do("POST", "/runs/"+run+"/resume", ClientToken, nil, nil)
	if st != 202 {
		t.Fatalf("resumeRun = %d, want 202 (the step containers registered again): %s", st, b)
	}
	evs = l.waitEvents(t, run, 10*time.Minute, 2*time.Second, func(e []runEvent) bool {
		resumed := false
		for _, x := range e {
			if x.Kind == "RunResumed" {
				resumed = true
			}
			if resumed && x.Kind == "StepAccepted" && x.Step == "generate" {
				return true
			}
			if resumed && x.Kind == "StepRejected" {
				return true
			}
		}
		return false
	})
	if !has(evs, "StepAccepted", "generate") {
		t.Errorf("the resumed run did not accept generate: %+v", evs)
	}
}

// TestWritableRootCodeMounts: with WritableRoot("code") the code step's container has the project
// root writable and `.git` read-only, and every other step's mounts are unchanged.
func TestWritableRootCodeMounts(t *testing.T) {
	l := Start(t, Build(), WritableRoot("code"))
	slice := approvedExample(t, l)
	l.StepWorkers(t, slice, labProject)
	l.checkStepContainers(t, slice, "code")
	c := inspectStep(t, stepContainer(slice, "code", labProject))
	for _, m := range c.Mounts {
		switch m.Destination {
		case "/projects/" + labProject:
			if !m.RW {
				t.Error("the code step's project root is read-only with WritableRoot(\"code\")")
			}
		case "/projects/" + labProject + "/.git":
			if m.RW {
				t.Error("the code step's .git is writable with WritableRoot(\"code\")")
			}
		}
	}
}
