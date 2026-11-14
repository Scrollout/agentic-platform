// T054i (T054f unit 1): the lab's platform, tests written before the code by a separate author.
// This file runs without Docker; platform_docker_test.go and platform_regression_docker_test.go
// run behind the `docker` tag.
//
// SEAMS these tests assume (none exists yet; each is new code in lab, never a change to a test):
//
//   - In a file WITHOUT a build tag (so this file compiles without `docker`):
//
//     const CheckToken, ProjectToken string
//     the lab's tokens the engine calls the check service and the project worker with; they
//     differ from each other and from the five tokens of lab.go.
//
//     type Host struct {
//     Runsc bool              // the Docker daemon has the runsc runtime
//     User  string            // the developer's numeric uid:gid
//     Addrs map[string]string // a Compose host name ("engine", "gateway") -> its address on the lab network
//     }
//
//     type Settings struct {   // what the lab passes `docker run` for one container,
//     Runtime     string       // apart from its name, network, labels, mounts and image;
//     ReadOnly    bool         // "" is Docker's default runtime
//     User        string
//     CapDrop     []string
//     SecurityOpt []string     // "no-new-privileges" and "no-new-privileges:true" are the same
//     Tmpfs       []string     // as `--tmpfs` takes them, e.g. "/tmp:exec,mode=1777,size=1g"
//     CPUs        string       // as `--cpus` takes it, e.g. "2"
//     Memory      string       // as `--memory` takes it, e.g. "2g"
//     Pids        int          // `--pids-limit`
//     Env         map[string]string
//     Entrypoint  []string     // nil: the image's; Compose's list as written, `$$` read as `$`
//     Command     []string     // nil or empty: none given
//     }
//
//     func ServiceSettings(service string, h Host) (Settings, error)
//     the settings the lab starts the Compose service `service` with: "check", "project-worker",
//     and the templates "step-agent" and "procedure-worker" (before the per-container values
//     `ap build-workers` adds: STEP_AGENT and STEP_TOKEN, or CHECK_PROCEDURE, CHECK_STEP and a
//     non-empty CHECK_TOKEN, which T054j's StepWorkers adds and tests). Any other name is an error.
//
//     type ImageBuild struct {
//     Name       string            // "agent", "check", "project"
//     Dockerfile string            // <root>/src/deploy/Dockerfile.<Name>
//     Context    string            // <root>/src
//     Tag        string            // "aptest-<Name>:lab"
//     Args       map[string]string // the --build-arg values
//     }
//
//     func ImageBuilds(root string) ([]ImageBuild, error)
//     the three image builds the lab makes, root being the repository's root.
//
//   - In the docker-tagged lab (platform_docker_test.go): `type Option`; `func Build() Option`;
//     `Start(t *testing.T, opts ...Option) *Lab` (`Start(t)` keeps compiling, so the existing
//     suites start their labs unchanged); `Lab` fields `Check` and `ProjectWorker`, the names of
//     the check service's and the project worker's containers (set only with Build());
//     `func (l *Lab) RestartEngine(t testing.TB)`.
//
//   - Names and labels (research D34): every container, network and volume a lab makes is named
//     `aptest-lab-<what>-<pid>-<n>` and labelled `org.agentic-platform.lab=<pid>`; `lab.Start`
//     first removes every such resource whose process no longer runs and leaves those of a
//     running process alone.
package lab

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// repoRoot is the repository's root, from this package's folder (src/tests/lab); Root in lab.go
// is behind the docker tag.
func repoRoot(t testing.TB) string {
	t.Helper()
	d, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "src", "deploy", "compose.yaml")); err != nil {
		t.Fatalf("not the repository's root: %s: %v", d, err)
	}
	return d
}

// composeService is the part of a Compose service the lab's settings must equal.
type composeService struct {
	Runtime     string            `yaml:"runtime"`
	ReadOnly    bool              `yaml:"read_only"`
	CapDrop     []string          `yaml:"cap_drop"`
	SecurityOpt []string          `yaml:"security_opt"`
	Tmpfs       []string          `yaml:"tmpfs"`
	User        string            `yaml:"user"`
	Entrypoint  []string          `yaml:"entrypoint"`
	Command     []string          `yaml:"command"`
	Environment map[string]string `yaml:"environment"`
	ExtraHosts  map[string]string `yaml:"extra_hosts"`
	Deploy      struct {
		Resources struct {
			Limits struct {
				CPUs   string `yaml:"cpus"`
				Memory string `yaml:"memory"`
				Pids   int    `yaml:"pids"`
			} `yaml:"limits"`
		} `yaml:"resources"`
	} `yaml:"deploy"`
}

func readCompose(t testing.TB) map[string]composeService {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "src", "deploy", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]composeService `yaml:"services"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("compose.yaml: %v", err)
	}
	return doc.Services
}

var composeVar = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::[?-][^}]*)?\}`)

// interpolate reads a Compose value as Compose does: `${NAME...}` becomes vars[NAME] (a name the
// lab has no value for fails the test) and `$$` a literal `$`.
func interpolate(t testing.TB, s string, vars map[string]string) string {
	t.Helper()
	const dollar = "\x00"
	s = strings.ReplaceAll(s, "$$", dollar)
	s = composeVar.ReplaceAllStringFunc(s, func(m string) string {
		name := composeVar.FindStringSubmatch(m)[1]
		v, ok := vars[name]
		if !ok {
			t.Errorf("compose.yaml uses %s, which the lab has no value for", name)
		}
		return v
	})
	return strings.ReplaceAll(s, dollar, "$")
}

func normalOpts(opts []string) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		if o == "no-new-privileges" {
			o = "no-new-privileges:true"
		}
		out = append(out, o)
	}
	sort.Strings(out)
	return out
}

func sorted(s []string) []string {
	c := append([]string(nil), s...)
	sort.Strings(c)
	return c
}

// wantSettings is what compose.yaml declares for the service, read for the lab's host h.
func wantSettings(t testing.TB, svc composeService, h Host) Settings {
	t.Helper()
	vars := map[string]string{
		"AP_CHECK_TOKEN":   CheckToken,
		"AP_PROJECT_TOKEN": ProjectToken,
		"AP_HOST_USER":     h.User,
	}
	w := Settings{
		ReadOnly:    svc.ReadOnly,
		User:        interpolate(t, svc.User, vars),
		CapDrop:     svc.CapDrop,
		SecurityOpt: svc.SecurityOpt,
		Tmpfs:       svc.Tmpfs,
		CPUs:        svc.Deploy.Resources.Limits.CPUs,
		Memory:      svc.Deploy.Resources.Limits.Memory,
		Pids:        svc.Deploy.Resources.Limits.Pids,
		Env:         map[string]string{},
	}
	if svc.Runtime == "runsc" && h.Runsc {
		w.Runtime = "runsc"
	}
	for _, e := range svc.Entrypoint {
		w.Entrypoint = append(w.Entrypoint, interpolate(t, e, vars))
	}
	for _, c := range svc.Command {
		w.Command = append(w.Command, interpolate(t, c, vars))
	}
	for k, v := range svc.Environment {
		v = interpolate(t, v, vars)
		// Each host name is replaced by its address: the lab's containers find each other by IP.
		for name := range svc.ExtraHosts {
			addr, ok := h.Addrs[name]
			if !ok {
				t.Fatalf("the test's host has no address for %s", name)
			}
			v = strings.ReplaceAll(v, "://"+name+":", "://"+addr+":")
		}
		w.Env[k] = v
	}
	return w
}

func compareSettings(t testing.TB, service string, got, want Settings) {
	t.Helper()
	check := func(field string, g, w any) {
		if !reflect.DeepEqual(g, w) {
			t.Errorf("%s: %s = %#v, compose.yaml says %#v", service, field, g, w)
		}
	}
	check("runtime", got.Runtime, want.Runtime)
	check("read-only root", got.ReadOnly, want.ReadOnly)
	check("user", got.User, want.User)
	check("dropped capabilities", sorted(got.CapDrop), sorted(want.CapDrop))
	check("security options", normalOpts(got.SecurityOpt), normalOpts(want.SecurityOpt))
	check("tmpfs", sorted(got.Tmpfs), sorted(want.Tmpfs))
	check("CPUs", got.CPUs, want.CPUs)
	check("memory", got.Memory, want.Memory)
	check("process limit", got.Pids, want.Pids)
	check("entrypoint", got.Entrypoint, want.Entrypoint)
	if len(got.Command) != 0 || len(want.Command) != 0 {
		check("command", got.Command, want.Command)
	}
	for k, w := range want.Env {
		if g, ok := got.Env[k]; !ok {
			t.Errorf("%s: environment has no %s; compose.yaml sets it to %q", service, k, w)
		} else if g != w {
			t.Errorf("%s: environment %s = %q, compose.yaml says %q", service, k, g, w)
		}
	}
	for k, g := range got.Env {
		if _, ok := want.Env[k]; !ok {
			t.Errorf("%s: environment has %s=%q, which compose.yaml does not set", service, k, g)
		}
	}
}

// The services the lab starts from compose.yaml's declarations (research D34).
var labServices = []string{"check", "project-worker", "step-agent", "procedure-worker"}

// TestServiceSettingsEqualCompose: the lab's settings for the check service, the project worker,
// the step agents and the procedure workers are compose.yaml's (x-sandboxed and each service),
// with and without runsc on the host, each host name replaced by the lab's address for it.
func TestServiceSettingsEqualCompose(t *testing.T) {
	services := readCompose(t)
	for _, runsc := range []bool{true, false} {
		h := Host{Runsc: runsc, User: "1234:5678", Addrs: map[string]string{"engine": "192.0.2.10", "gateway": "192.0.2.11"}}
		for _, name := range labServices {
			svc, ok := services[name]
			if !ok {
				t.Fatalf("compose.yaml has no service %s", name)
			}
			got, err := ServiceSettings(name, h)
			if err != nil {
				t.Errorf("ServiceSettings(%q, runsc=%v): %v", name, runsc, err)
				continue
			}
			compareSettings(t, name+map[bool]string{true: " (runsc)", false: " (no runsc)"}[runsc], got, wantSettings(t, svc, h))
		}
	}
}

// TestServiceSettingsPinned guards the comparison itself: compose.yaml's sandbox values reach the
// four services (the merge of x-sandboxed is read), and each deployment is named.
func TestServiceSettingsPinned(t *testing.T) {
	services := readCompose(t)
	deployment := map[string]string{"check": "check-service", "project-worker": "project-worker", "procedure-worker": "procedure-worker"}
	for _, name := range labServices {
		s := services[name]
		if s.Runtime != "runsc" || !s.ReadOnly || len(s.CapDrop) == 0 || len(s.Tmpfs) == 0 ||
			s.Deploy.Resources.Limits.CPUs == "" || s.Deploy.Resources.Limits.Memory == "" || s.Deploy.Resources.Limits.Pids == 0 {
			t.Errorf("compose.yaml's %s does not carry x-sandboxed as read: %+v", name, s)
		}
		if d, ok := deployment[name]; ok && s.Environment["CHECK_DEPLOYMENT"] != d {
			t.Errorf("compose.yaml's %s: CHECK_DEPLOYMENT = %q, want %q", name, s.Environment["CHECK_DEPLOYMENT"], d)
		}
	}
	h := Host{User: "1:2", Addrs: map[string]string{"engine": "192.0.2.10", "gateway": "192.0.2.11"}}
	if _, err := ServiceSettings("engine", h); err == nil {
		t.Error("ServiceSettings(\"engine\") gave settings; the lab starts only the four services from compose.yaml")
	}
	if CheckToken == "" || ProjectToken == "" || CheckToken == ProjectToken {
		t.Errorf("CheckToken %q and ProjectToken %q must be set and differ", CheckToken, ProjectToken)
	}
}

// dockerfileArgs is every ARG a Dockerfile declares.
func dockerfileArgs(t testing.TB, path string) map[string]bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	args := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if fs := strings.Fields(sc.Text()); len(fs) >= 2 && strings.EqualFold(fs[0], "ARG") {
			name, _, _ := strings.Cut(fs[1], "=")
			args[name] = true
		}
	}
	if len(args) == 0 {
		t.Fatalf("%s declares no ARG", path)
	}
	return args
}

// buildArgPins maps each build argument build.sh passes to the images.lock name it reads it from
// (`X=$v` with `v=$(pin name)`), so the lab's values come from the same lines build.sh uses.
func buildArgPins(t testing.TB, root string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "src", "deploy", "build.sh"))
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^([a-z_]+)=\$\(pin ([A-Za-z0-9_-]+)\)`).FindAllStringSubmatch(string(b), -1) {
		vars[m[1]] = m[2]
	}
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`--build-arg "([A-Z_]+)=\$([a-z_]+)"`).FindAllStringSubmatch(string(b), -1) {
		if pin, ok := vars[m[2]]; ok {
			out[m[1]] = pin
		}
	}
	if len(out) == 0 {
		t.Fatal("build.sh passes no build argument read from images.lock")
	}
	return out
}

func lockPins(t testing.TB, root string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "src", "deploy", "images.lock"))
	if err != nil {
		t.Fatal(err)
	}
	pins := map[string]string{}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && !strings.HasPrefix(f[0], "#") {
			pins[f[0]] = f[1]
		}
	}
	return pins
}

// TestImageBuildArgsFromImagesLock: the lab builds the agent, check and project images from the
// source under test, tagged aptest-<name>:lab, passing exactly every ARG of each Dockerfile, each
// with the images.lock value build.sh passes for it.
func TestImageBuildArgsFromImagesLock(t *testing.T) {
	root := repoRoot(t)
	pins, argPin := lockPins(t, root), buildArgPins(t, root)
	builds, err := ImageBuilds(root)
	if err != nil {
		t.Fatalf("ImageBuilds: %v", err)
	}
	seen := map[string]bool{}
	for _, b := range builds {
		if seen[b.Name] {
			t.Errorf("image %s is built twice", b.Name)
		}
		seen[b.Name] = true
		df := filepath.Join(root, "src", "deploy", "Dockerfile."+b.Name)
		if b.Dockerfile != df {
			t.Errorf("%s: Dockerfile %s, want %s", b.Name, b.Dockerfile, df)
		}
		if ctx := filepath.Join(root, "src"); b.Context != ctx {
			t.Errorf("%s: build context %s, want %s", b.Name, b.Context, ctx)
		}
		if tag := "aptest-" + b.Name + ":lab"; b.Tag != tag {
			t.Errorf("%s: tag %s, want %s", b.Name, b.Tag, tag)
		}
		declared := dockerfileArgs(t, df)
		for arg := range declared {
			pin, ok := argPin[arg]
			if !ok {
				t.Errorf("%s: ARG %s is not one build.sh reads from images.lock", b.Name, arg)
				continue
			}
			if got, want := b.Args[arg], pins[pin]; got != want || want == "" {
				t.Errorf("%s: build argument %s = %q, want images.lock's %s %q", b.Name, arg, got, pin, want)
			}
		}
		for arg := range b.Args {
			if !declared[arg] {
				t.Errorf("%s: passes build argument %s, which Dockerfile.%s does not declare", b.Name, arg, b.Name)
			}
		}
	}
	for _, n := range []string{"agent", "check", "project"} {
		if !seen[n] {
			t.Errorf("ImageBuilds has no %s image", n)
		}
	}
	if len(builds) != 3 {
		t.Errorf("ImageBuilds has %d images, want the agent, check and project images only", len(builds))
	}
}
