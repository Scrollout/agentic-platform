package lab

// The settings the lab starts the platform's worker services with, written out here and proved
// equal to compose.yaml's by platform_settings_test.go, which runs without Docker (research D34;
// constitution, "Prove agreement"). This file has no build tag so that test compiles without
// Docker; the docker-tagged lab turns these settings into `docker run` flags (platform.go).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The tokens the lab's engine calls the check service and the project worker with; they differ
// from each other and from the five tokens of lab.go. Lab constants, never a session's.
const (
	CheckToken   = "lab-check-token-0123456789"
	ProjectToken = "lab-project-token-0123456789"
)

// Host is what the settings depend on in the machine the lab runs on.
type Host struct {
	Runsc bool              // the Docker daemon has the runsc runtime
	User  string            // the developer's numeric uid:gid
	Addrs map[string]string // a Compose host name ("engine", "gateway") -> its address on the lab network
}

// Settings is what the lab passes `docker run` for one container, apart from its name, network,
// labels, mounts and image.
type Settings struct {
	Runtime     string // "" is Docker's default runtime
	ReadOnly    bool
	User        string
	CapDrop     []string
	SecurityOpt []string
	Tmpfs       []string // as `--tmpfs` takes them
	CPUs        string   // as `--cpus` takes it
	Memory      string   // as `--memory` takes it
	Pids        int      // `--pids-limit`
	Env         map[string]string
	Entrypoint  []string // nil: the image's
	Command     []string // nil or empty: none given
}

// sandboxed is compose.yaml's x-sandboxed (with x-hardened) for the lab's host.
func sandboxed(h Host, user string) Settings {
	s := Settings{
		ReadOnly:    true,
		User:        user,
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges:true"},
		Tmpfs:       []string{"/tmp:exec,mode=1777,size=1g"},
		CPUs:        "2",
		Memory:      "2g",
		Pids:        512,
	}
	if h.Runsc {
		s.Runtime = "runsc"
	}
	return s
}

// checkEnv is the environment the check program's deployments share in compose.yaml.
func checkEnv(token, deployment string, extra map[string]string) map[string]string {
	env := map[string]string{
		"CHECK_LISTEN_ADDRESS": ":8080",
		"CHECK_TOKEN":          token,
		"CHECK_PROJECTS_DIR":   "/projects",
		"CHECK_DEPLOYMENT":     deployment,
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

var codegenEnv = map[string]string{"CHECK_OAPI_CODEGEN": "/usr/local/bin/oapi-codegen", "CHECK_TEMPLATE_DIR": "/opt/template"}

// stepAgentEntrypoint is compose.yaml's step-agent entrypoint, with `$$` read as `$`.
var stepAgentEntrypoint = []string{"sh", "-c", `if [ -z "${STEP_AGENT:-}" ] || [ -z "${STEP_TOKEN:-}" ]; then echo 'step-agent: STEP_AGENT and STEP_TOKEN must be set (ap build-workers sets them)' >&2; exit 1; fi; exec /usr/local/bin/docker-agent serve api "/agents/$STEP_AGENT.yaml" --listen 0.0.0.0:8080 --auth-token "$STEP_TOKEN" --session-db /tmp/session.db`}

// ServiceSettings is the settings the lab starts the Compose service `service` with: "check",
// "project-worker", and the templates "step-agent" and "procedure-worker" (before the
// per-container values `ap build-workers` adds). Any other name is an error.
func ServiceSettings(service string, h Host) (Settings, error) {
	addr := func(name string) (string, error) {
		a, ok := h.Addrs[name]
		if !ok || a == "" {
			return "", fmt.Errorf("%s: no address for %s", service, name)
		}
		return a, nil
	}
	switch service {
	case "check":
		s := sandboxed(h, "65532:65532")
		s.Command = []string{"/check"}
		s.Env = checkEnv(CheckToken, "check-service", codegenEnv)
		return s, nil
	case "project-worker":
		s := sandboxed(h, h.User)
		s.Command = []string{"/check"}
		s.Env = checkEnv(ProjectToken, "project-worker", map[string]string{"CHECK_HISTORY_DIR": "/var/lib/project-worker"})
		return s, nil
	case "procedure-worker":
		s := sandboxed(h, h.User)
		s.Command = []string{"/check"}
		s.Env = checkEnv("", "procedure-worker", codegenEnv)
		return s, nil
	case "step-agent":
		engine, err := addr("engine")
		if err != nil {
			return Settings{}, err
		}
		gateway, err := addr("gateway")
		if err != nil {
			return Settings{}, err
		}
		s := sandboxed(h, h.User)
		s.Entrypoint = append([]string(nil), stepAgentEntrypoint...)
		s.Command = []string{}
		s.Env = map[string]string{
			"ENGINE_URL":        "http://" + engine + ":8080",
			"ENGINE_TOKEN":      "",
			"MODEL_GATEWAY_URL": "http://" + gateway + ":8080",
			"ANTHROPIC_API_KEY": "placeholder-not-a-key",
			"TELEMETRY_ENABLED": "false",
		}
		return s, nil
	}
	return Settings{}, fmt.Errorf("the lab starts no service %q from compose.yaml (only check, project-worker, step-agent and procedure-worker)", service)
}

// ImageBuild is one image the lab builds from the source under test.
type ImageBuild struct {
	Name       string            // "agent", "check", "project"
	Dockerfile string            // <root>/src/deploy/Dockerfile.<Name>
	Context    string            // <root>/src
	Tag        string            // "aptest-<Name>:lab"
	Args       map[string]string // the --build-arg values
}

// imageArgs names, per image, each build argument and the images.lock line build.sh passes it from.
var imageArgs = []struct {
	name string
	args map[string]string
}{
	{"agent", map[string]string{"DOCKER_AGENT_IMAGE": "docker-agent", "GOLANG_IMAGE": "golang"}},
	{"check", map[string]string{"GOLANG_IMAGE": "golang", "OAPI_CODEGEN": "oapi-codegen"}},
	{"project", map[string]string{"GOLANG_IMAGE": "golang", "ALPINE_IMAGE": "alpine", "ALPINE_GIT": "alpine-git"}},
}

// ImageBuilds is the three image builds the lab makes, root being the repository's root, with
// their build arguments read from src/deploy/images.lock.
func ImageBuilds(root string) ([]ImageBuild, error) {
	b, err := os.ReadFile(filepath.Join(root, "src", "deploy", "images.lock"))
	if err != nil {
		return nil, err
	}
	pins := map[string]string{}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && !strings.HasPrefix(f[0], "#") {
			pins[f[0]] = f[1]
		}
	}
	var out []ImageBuild
	for _, im := range imageArgs {
		ib := ImageBuild{
			Name:       im.name,
			Dockerfile: filepath.Join(root, "src", "deploy", "Dockerfile."+im.name),
			Context:    filepath.Join(root, "src"),
			Tag:        "aptest-" + im.name + ":lab",
			Args:       map[string]string{},
		}
		for arg, pin := range im.args {
			v, ok := pins[pin]
			if !ok {
				return nil, fmt.Errorf("no %s pin in images.lock (build argument %s of %s)", pin, arg, im.name)
			}
			ib.Args[arg] = v
		}
		out = append(out, ib)
	}
	return out, nil
}
