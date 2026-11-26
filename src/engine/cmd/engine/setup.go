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
	stdslices "slices"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/agentic-platform/engine/analyst"
	"github.com/agentic-platform/engine/api"
	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/contracts"
	"github.com/agentic-platform/engine/durable"
	"github.com/agentic-platform/engine/machine"
	"github.com/agentic-platform/engine/method"
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
//go:embed gen/agents/review.yaml gen/agents/review/instruction.md gen/agents/review/findings.schema.json
var agentFiles embed.FS

// agentRoot is where agentFiles holds src/agents.
const agentRoot = "gen/agents"

// variables are the settings setup reads, all required.
var variables = []string{
	"ENGINE_LISTEN_ADDRESS", "ENGINE_CLIENT_TOKEN", "ENGINE_PAGE_TOKEN", "ENGINE_WORKER_TOKEN",
	"AP_LINT_URL", "AP_LINT_TOKEN", "AP_ANALYST_URL", "AP_ANALYST_TOKEN", "AP_CHECK_URL", "AP_CHECK_TOKEN",
	"AP_PROJECT_URL", "AP_PROJECT_TOKEN", "ENGINE_STORE_DIR",
}

// tokenVariables must all differ from each other: each names one caller or one callee.
var tokenVariables = []string{"ENGINE_CLIENT_TOKEN", "ENGINE_PAGE_TOKEN", "ENGINE_WORKER_TOKEN", "AP_LINT_TOKEN", "AP_ANALYST_TOKEN", "AP_CHECK_TOKEN", "AP_PROJECT_TOKEN"}

// projectsMount is where a step container sees the projects folder (src/deploy/compose.yaml).
const projectsMount = "/projects"

// defaultDurableAddress is where the durable service listens in the deployment (compose.yaml's
// `durable` service on the network `durable`); ENGINE_DURABLE_ADDRESS overrides it.
const defaultDurableAddress = "durable:7233"

// durableWait is how long the engine tries to reach the durable service at start before it
// gives up: the service, or the network to it, may come up just after the engine.
const durableWait = 30 * time.Second

// methodIdentity gives the method identity the engine runs under; tests replace it.
var methodIdentity = method.Current

// newSlices builds the engine's slice service; tests replace it to see its Config.
var newSlices = slices.New

// durableLinks is what the engine needs from the durable service, given at start.
type durableLinks struct {
	Runs       api.Runs // api's port: the runs' workflows
	Namespaces namespaceRegistry
	// Serve starts the engine's durable worker, which runs the runs' workflows and their
	// activities on a, and returns the function that stops it. nil starts none.
	Serve func(a *durable.Activities) (stop func(), err error)
	// Operations is api's port to the operations' workflows; nil runs each operation's work in
	// the engine's process (api.Config.Operations).
	Operations api.Operations
	// ServeOperations starts the worker of the operations' workflows, whose activities call op,
	// and returns the function that stops it. nil starts none.
	ServeOperations func(op api.Operator) (stop func(), err error)
}

// namespaceRegistry registers the namespace the engine's workflows run in (ADR 0070).
type namespaceRegistry interface {
	// Describe says whether the namespace exists and, if so, its retention.
	Describe(ctx context.Context, name string) (retention time.Duration, found bool, err error)
	// Register creates the namespace with the retention.
	Register(ctx context.Context, name string, retention time.Duration) error
}

// setup builds the engine's handler from the environment (getenv) and what the binary embeds,
// over the durable service at ENGINE_DURABLE_ADDRESS (host:port; default durable:7233), which it
// reaches at its first call, then runs setupWith on it with the engine's durable worker. The
// returned function closes what setupWith opened, then the connection to the durable service.
func setup(getenv func(string) string) (http.Handler, func() error, error) {
	if _, err := environment(getenv); err != nil {
		return nil, nil, err
	}
	addr := getenv("ENGINE_DURABLE_ADDRESS")
	if addr == "" {
		addr = defaultDurableAddress
	}
	c, err := durable.Connect(addr)
	if err != nil {
		return nil, nil, fmt.Errorf("the durable service at %s: %w", addr, err)
	}
	ns, err := durable.NewNamespaces(addr)
	if err != nil {
		c.Close()
		return nil, nil, err
	}
	// Closing the connection, not stopping a worker, ends it: an activity cut off this way is a
	// platform stop (its heartbeat times out) and is made again, never failed.
	links := durableLinks{Runs: durable.NewRuns(c), Namespaces: ns, Serve: func(a *durable.Activities) (func(), error) {
		if err := durable.NewRunsWorker(c, a).Start(); err != nil {
			return nil, fmt.Errorf("the engine's durable worker: %w", err)
		}
		return func() {}, nil
	}, Operations: durable.NewOperations(c), ServeOperations: func(op api.Operator) (func(), error) {
		if err := durable.NewOperationsWorker(c, op).Start(); err != nil {
			return nil, fmt.Errorf("the engine's operations worker: %w", err)
		}
		return func() {}, nil
	}}
	h, closeFn, err := setupWith(getenv, links)
	if err != nil {
		ns.Close()
		c.Close()
		return nil, nil, err
	}
	return h, func() error {
		err := closeFn()
		ns.Close()
		c.Close()
		return err
	}, nil
}

// environment reads the settings setup needs and checks them.
func environment(getenv func(string) string) (map[string]string, error) {
	env := map[string]string{}
	for _, name := range variables {
		if env[name] = getenv(name); env[name] == "" {
			return nil, fmt.Errorf("%s is not set", name)
		}
	}
	for i, a := range tokenVariables {
		for _, b := range tokenVariables[i+1:] {
			if env[a] == env[b] {
				return nil, fmt.Errorf("%s and %s are the same; every token must differ", a, b)
			}
		}
	}
	for _, name := range []string{"AP_LINT_URL", "AP_ANALYST_URL", "AP_CHECK_URL", "AP_PROJECT_URL"} {
		if u, err := url.Parse(env[name]); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("%s is not an http(s) URL with a host", name)
		}
	}
	dir := env["ENGINE_STORE_DIR"]
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("ENGINE_STORE_DIR %q is not an existing directory", dir)
	}
	// The folder of step worker grants the launcher writes (api.Config.GrantsDir); optional:
	// unset, no step worker can register.
	if grants := getenv("ENGINE_GRANTS_DIR"); grants != "" {
		if fi, err := os.Stat(grants); err != nil || !fi.IsDir() {
			return nil, fmt.Errorf("ENGINE_GRANTS_DIR %q is not an existing directory", grants)
		}
	}
	env["ENGINE_GRANTS_DIR"] = getenv("ENGINE_GRANTS_DIR")
	return env, nil
}

// setupWith builds the engine's handler from the environment (getenv) and what the binary
// embeds: the contract's schemas, the catalogue pattern and the analyst definition's digest, with
// the durable service d. It opens the run store in ENGINE_STORE_DIR, which must exist, and serves
// the slices stored there; a slice record it cannot read or check stops it. Before the engine
// serves anything it registers the namespace ap with the contract's retention when the durable
// service has none (an existing one is kept unchanged, ADR 0070), refuses to start, naming each,
// while a run's record has no end and the run has no workflow (a run feature 001 left open;
// research R7), and starts the durable workers of both task queues (d.Serve, d.ServeOperations).
// The returned function cancels running operations, stops the durable workers and closes the
// store; calling it again does nothing.
func setupWith(getenv func(string) string, d durableLinks) (http.Handler, func() error, error) {
	env, err := environment(getenv)
	if err != nil {
		return nil, nil, err
	}
	dir, grants := env["ENGINE_STORE_DIR"], env["ENGINE_GRANTS_DIR"]
	id, err := methodIdentity()
	if err != nil {
		return nil, nil, fmt.Errorf("the embedded method identity src/engine/method/method.sha256 (run scripts/method-digest.py and commit the file): %w", err)
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
	svc, err := newSlices(slices.Config{
		Schemas: schemas,
		Analyst: analyst.NewHTTP(env["AP_ANALYST_URL"], env["AP_ANALYST_TOKEN"], digest),
		Diff:    contracts.NewDiff(),
		Lint:    contracts.NewLint(env["AP_LINT_URL"], env["AP_LINT_TOKEN"]),
		Pattern: pattern,
		Records: st,
		// Runner errors in full, for the operator; no token is ever logged.
		Log:    slog.New(slog.NewJSONHandler(os.Stderr, nil)),
		Method: id.String(),
	})
	if err != nil {
		st.Close()
		return nil, nil, fmt.Errorf("the slices in ENGINE_STORE_DIR: %w", err)
	}

	cfg := api.Config{
		Tokens:    api.Tokens{Client: env["ENGINE_CLIENT_TOKEN"], Page: env["ENGINE_PAGE_TOKEN"], Worker: env["ENGINE_WORKER_TOKEN"]},
		Slices:    svc,
		Store:     st,
		Workers:   api.NewWorkers(),
		GrantsDir: grants,
	}
	if err := wireSteps(&cfg, svc, st, env); err != nil {
		return nil, nil, errors.Join(err, st.Close())
	}
	if err := durableStart(d, st); err != nil {
		return nil, nil, errors.Join(err, st.Close())
	}
	var mp machine.Pattern
	if p, err := svc.Pattern(); err == nil {
		mp = api.MachinePattern(p)
	}
	rec := api.NewRunRecord(st, mp, cfg.Model)
	cfg.Runs, cfg.Record, cfg.Operations = d.Runs, rec, d.Operations
	ctx, cancel := context.WithCancel(context.Background())
	cfg.Context = ctx
	var running sync.WaitGroup
	cfg.Running = &running
	h, op := api.NewEngine(cfg)
	var stops []func()
	stopWorker := func() {
		for _, stop := range stops {
			stop()
		}
	}
	if d.Serve != nil {
		stop, err := d.Serve(&durable.Activities{Worker: cfg.StepWorker, Checker: cfg.StepChecker, History: cfg.History,
			Results: rec.Results(), Artifacts: st, Events: rec, Method: id, Captured: api.CapturedParameters{Store: st}})
		if err != nil {
			cancel()
			return nil, nil, errors.Join(err, st.Close())
		}
		stops = append(stops, stop)
	}
	if d.ServeOperations != nil {
		stop, err := d.ServeOperations(op)
		if err != nil {
			cancel()
			stopWorker()
			return nil, nil, errors.Join(err, st.Close())
		}
		stops = append(stops, stop)
	}
	var once sync.Once
	var closeErr error
	closeFn := func() error {
		once.Do(func() {
			// Cancel every operation and wait until each has ended; the runs go on in the durable
			// service and continue on the next start (FR-001).
			cancel()
			running.Wait()
			stopWorker()
			closeErr = st.Close()
		})
		return closeErr
	}
	return h, closeFn, nil
}

// endKinds are the events that end a run (data model, "Run states").
var endKinds = []string{"RunCompleted", "HandOverAccepted", "RunExpired", "RunCancelled"}

// durableStart registers the namespace ap with the contract's retention when the durable service
// has none, keeping an existing one unchanged (ADR 0070), and refuses, naming each, the runs whose
// record has no end event and that have no workflow (research R7).
func durableStart(d durableLinks, st *store.Store) error {
	ctx, cancel := context.WithTimeout(context.Background(), durableWait)
	defer cancel()
	// The service, or the network to it, may come up just after the engine: try until durableWait.
	var found bool
	for {
		var err error
		if _, found, err = d.Namespaces.Describe(ctx, durable.Namespace); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the durable service: %w", err)
		case <-time.After(time.Second):
		}
	}
	if !found {
		if err := d.Namespaces.Register(ctx, durable.Namespace, api.Retention()); err != nil {
			return fmt.Errorf("the durable service: %w", err)
		}
		// A new namespace is served once the service's namespace cache has it (some seconds):
		// wait until a workflow can be looked up in it.
		for {
			_, err := d.Runs.Exists(ctx, "RUN-0")
			if err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("the durable service does not serve the namespace %s yet: %w", durable.Namespace, err)
			case <-time.After(time.Second):
			}
		}
	}
	var open []string
	for _, e := range st.Runs() {
		events, err := st.Events(e.Run)
		if err != nil {
			return fmt.Errorf("the run record of %s: %w", e.Run, err)
		}
		if stdslices.ContainsFunc(events, func(ev store.Event) bool { return stdslices.Contains(endKinds, ev.Kind) }) {
			continue
		}
		has, err := d.Runs.Exists(ctx, e.Run)
		if err != nil {
			return fmt.Errorf("the durable service: %w", err)
		}
		if !has {
			open = append(open, e.Run)
		}
	}
	if len(open) > 0 {
		return fmt.Errorf("the run record holds runs that have not ended and that this engine cannot continue: %s; "+
			"end each with feature 001's version of the platform (resume it to its end, or cancel it), then upgrade", strings.Join(open, ", "))
	}
	return nil
}

// wireSteps gives the engine its step worker and checker (package steps): agent and procedure
// steps on the workers registered in cfg.Workers, checks on the check service at url. Each agent
// step's model is the one its embedded definition pins; a step whose definition is not embedded has
// none, so its attempts fail before anything is called, and cfg.Model stays empty, so no agent
// step's result is reused. cfg.Model is set only when every agent step pins the same model.
// An agent step whose `agent_digest` in the pattern is not its embedded definition's digest
// (agentDigest) stops the engine (Principle XVII: the pattern's digest is checked against the
// definition, never trusted as written). cfg.History is the project worker's version history at
// AP_PROJECT_URL.
func wireSteps(cfg *api.Config, svc *slices.Service, st *store.Store, env map[string]string) error {
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
		Pattern: mp, Endpoints: cfg.Workers, CheckURL: env["AP_CHECK_URL"], CheckToken: env["AP_CHECK_TOKEN"],
		ProjectURL: env["AP_PROJECT_URL"], ProjectToken: env["AP_PROJECT_TOKEN"],
		Lint: svc.LintContract, Models: models, Projects: projectsMount,
		Approved: svc.ApprovedParts, Load: st.Artifact,
	}
	cfg.StepWorker = &sliceWorkers{base: base, st: st, by: map[string]machine.Worker{}}
	cfg.StepChecker = steps.NewChecker(base)
	cfg.History = steps.NewHistory(base)
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
