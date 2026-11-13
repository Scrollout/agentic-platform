package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	"github.com/agentic-platform/engine/analyst"
	"github.com/agentic-platform/engine/api"
	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/contracts"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/steps"
	"github.com/agentic-platform/engine/store"
	"github.com/agentic-platform/slice"
)

// What the engine embeds from outside its module. Go's embed reaches only files inside the
// package's folder, so scripts/generate.sh copies each gen/<path> named here from src/<path>
// (never committed; `generate.sh --check` proves the copies match their sources).
//
//go:embed gen/catalogue/api-feature-go/pattern.yaml
var pattern []byte

// The agent definitions, as the analyst service and each agent step's container mount src/agents
// at /agents: the analyst's and each agent step's (`agent:<name>` in the pattern).
//
//go:embed gen/agents/analyst.yaml gen/agents/analyst/instruction.md
//go:embed gen/agents/tests.yaml gen/agents/tests/instruction.md
//go:embed gen/agents/code.yaml gen/agents/code/instruction.md
//go:embed gen/agents/review.yaml gen/agents/review/instruction.md
var agentFiles embed.FS

// agentRoot is where agentFiles holds src/agents.
const agentRoot = "gen/agents"

// variables are the settings setup reads, all required.
var variables = []string{
	"ENGINE_LISTEN_ADDRESS", "ENGINE_CLIENT_TOKEN", "ENGINE_PAGE_TOKEN", "ENGINE_WORKER_TOKEN",
	"AP_LINT_URL", "AP_LINT_TOKEN", "AP_ANALYST_URL", "AP_ANALYST_TOKEN", "AP_CHECK_URL", "AP_CHECK_TOKEN",
	"ENGINE_STORE_DIR",
}

// tokenVariables must all differ from each other: each names one caller or one callee.
var tokenVariables = []string{"ENGINE_CLIENT_TOKEN", "ENGINE_PAGE_TOKEN", "ENGINE_WORKER_TOKEN", "AP_LINT_TOKEN", "AP_ANALYST_TOKEN", "AP_CHECK_TOKEN"}

// projectsMount is where a step container sees the projects folder (src/deploy/compose.yaml).
const projectsMount = "/projects"

// setup builds the engine's handler from the environment (getenv) and what the binary embeds:
// the contract's schemas, the catalogue pattern and the analyst definition's digest. It opens
// the run store in ENGINE_STORE_DIR, which must exist, and serves the slices stored there; a
// slice record it cannot read or check stops it. The returned function cancels running
// operations and closes the store; calling it again does nothing.
func setup(getenv func(string) string) (http.Handler, func() error, error) {
	env := map[string]string{}
	for _, name := range variables {
		if env[name] = getenv(name); env[name] == "" {
			return nil, nil, fmt.Errorf("%s is not set", name)
		}
	}
	for i, a := range tokenVariables {
		for _, b := range tokenVariables[i+1:] {
			if env[a] == env[b] {
				return nil, nil, fmt.Errorf("%s and %s are the same; every token must differ", a, b)
			}
		}
	}
	for _, name := range []string{"AP_LINT_URL", "AP_ANALYST_URL", "AP_CHECK_URL"} {
		if u, err := url.Parse(env[name]); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, nil, fmt.Errorf("%s is not an http(s) URL with a host", name)
		}
	}
	dir := env["ENGINE_STORE_DIR"]
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, nil, fmt.Errorf("ENGINE_STORE_DIR %q is not an existing directory", dir)
	}

	schemas, err := slice.LoadSchemasFS(api.ContractFiles())
	if err != nil {
		return nil, nil, fmt.Errorf("the embedded contract's schemas: %w", err)
	}
	digest, err := agentDigest(agentFiles, agentRoot, "analyst")
	if err != nil {
		return nil, nil, fmt.Errorf("the embedded analyst definition: %w", err)
	}
	st, err := store.Open(dir, schemas)
	if err != nil {
		return nil, nil, fmt.Errorf("the run store in ENGINE_STORE_DIR: %w", err)
	}
	svc, err := slices.New(slices.Config{
		Schemas: schemas,
		Analyst: analyst.NewHTTP(env["AP_ANALYST_URL"], env["AP_ANALYST_TOKEN"], digest),
		Diff:    contracts.NewDiff(),
		Lint:    contracts.NewLint(env["AP_LINT_URL"], env["AP_LINT_TOKEN"]),
		Pattern: pattern,
		Records: st,
		// Runner errors in full, for the operator; no token is ever logged.
		Log: slog.New(slog.NewJSONHandler(os.Stderr, nil)),
	})
	if err != nil {
		st.Close()
		return nil, nil, fmt.Errorf("the slices in ENGINE_STORE_DIR: %w", err)
	}

	cfg := api.Config{
		Tokens:  api.Tokens{Client: env["ENGINE_CLIENT_TOKEN"], Page: env["ENGINE_PAGE_TOKEN"], Worker: env["ENGINE_WORKER_TOKEN"]},
		Slices:  svc,
		Store:   st,
		Workers: api.NewWorkers(),
	}
	if err := wireSteps(&cfg, svc, st, env["AP_CHECK_URL"], env["AP_CHECK_TOKEN"]); err != nil {
		st.Close()
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	cfg.Context = ctx
	h := api.NewHandler(cfg)
	var once sync.Once
	var closeErr error
	closeFn := func() error {
		once.Do(func() {
			cancel()
			closeErr = st.Close()
		})
		return closeErr
	}
	return h, closeFn, nil
}

// wireSteps gives the engine its step worker and checker (package steps): agent and procedure
// steps on the workers registered in cfg.Workers, checks on the check service at url. Each agent
// step's model is the one its embedded definition pins; a step whose definition is not embedded has
// none, so its attempts fail before anything is called, and cfg.Model stays empty, so no agent
// step's result is reused. cfg.Model is set only when every agent step pins the same model.
// An agent step whose `agent_digest` in the pattern is not its embedded definition's digest
// (agentDigest) stops the engine (Principle XVII: the pattern's digest is checked against the
// definition, never trusted as written).
func wireSteps(cfg *api.Config, svc *slices.Service, st *store.Store, url, token string) error {
	p, err := svc.Pattern()
	if err != nil {
		return nil // no pattern: the engine refuses every run (WorkersNotReady) and says why
	}
	mp := api.MachinePattern(p)
	models := map[string]string{}
	common, all := "", true
	for _, s := range mp.Steps {
		name, ok := strings.CutPrefix(s.Worker, "agent:")
		if !ok {
			continue
		}
		if d, err := agentDigest(agentFiles, agentRoot, name); err == nil && d != s.AgentDigest {
			return fmt.Errorf("pattern step %s: agent_digest %s is not the digest of its definition src/agents/%s.yaml and src/agents/%s/, %s", s.Name, s.AgentDigest, name, name, d)
		}
		m, err := pinnedModel(agentFiles, agentRoot+"/"+name+".yaml")
		switch {
		case err != nil:
			return fmt.Errorf("the embedded definition of agent %s: %w", name, err)
		case m == "":
			all = false
		case common == "" || common == m:
			models[s.Name], common = m, m
		default:
			models[s.Name], all = m, false
		}
	}
	if all {
		cfg.Model = common
	}
	base := steps.Config{
		Pattern: mp, Endpoints: cfg.Workers, CheckURL: url, CheckToken: token,
		Lint: svc.LintContract, Models: models, Projects: projectsMount,
		Approved: svc.ApprovedParts, Load: st.Artifact,
	}
	cfg.StepWorker = &sliceWorkers{base: base, st: st, by: map[string]machine.Worker{}}
	cfg.StepChecker = steps.NewChecker(base)
	return nil
}

// sliceWorkers keeps one step worker per slice, so each stores its outputs as that slice's
// artifacts (the store erases them with the slice).
type sliceWorkers struct {
	base steps.Config
	st   *store.Store
	mu   sync.Mutex
	by   map[string]machine.Worker
}

func (w *sliceWorkers) Run(ctx context.Context, r machine.Request) (machine.Result, error) {
	w.mu.Lock()
	sw, ok := w.by[r.Slice]
	if !ok {
		c := w.base
		slice := r.Slice
		c.Store = func(b []byte) (string, error) { return w.st.PutArtifact(slice, b) }
		sw = steps.NewWorker(c)
		w.by[r.Slice] = sw
	}
	w.mu.Unlock()
	return sw.Run(ctx, r)
}

// pinnedModel is the model an agent definition's root agent pins, as `<provider>/<model>`, the
// form the service reports in agent_info; "" when files holds no such definition.
func pinnedModel(files fs.FS, path string) (string, error) {
	b, err := fs.ReadFile(files, path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var def struct {
		Models map[string]struct {
			Provider string `yaml:"provider"`
			Model    string `yaml:"model"`
		} `yaml:"models"`
		Agents map[string]struct {
			Model string `yaml:"model"`
		} `yaml:"agents"`
	}
	if err := yaml.Unmarshal(b, &def); err != nil {
		return "", err
	}
	m, ok := def.Models[def.Agents["root"].Model]
	if !ok || m.Provider == "" || m.Model == "" {
		return "", errors.New("its root agent names no model with a provider and a model id")
	}
	return m.Provider + "/" + m.Model, nil
}

// agentDigest is the SHA-256, in lowercase hex, of agent name's definition files under root in
// their framed form (canonical.FramedHash): `<name>.yaml` and every file below `<name>/`, each by
// its path relative to root (the path under the container's /agents), so that no two sets of
// files give the same digest and no other agent's files change it. It fails when `<name>.yaml` is
// not there.
func agentDigest(files fs.FS, root, name string) (string, error) {
	if _, err := fs.Stat(files, root+"/"+name+".yaml"); err != nil {
		return "", err
	}
	var set []canonical.File
	err := fs.WalkDir(files, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if rel := strings.TrimPrefix(p, root+"/"); rel != name+".yaml" && !strings.HasPrefix(rel, name+"/") {
			return nil
		}
		b, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}
		set = append(set, canonical.File{Path: strings.TrimPrefix(p, root+"/"), Content: b})
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(set) == 0 {
		return "", errors.New("no files")
	}
	return canonical.FramedHash(set), nil
}
