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

	"github.com/agentic-platform/engine/analyst"
	"github.com/agentic-platform/engine/api"
	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/contracts"
	"github.com/agentic-platform/engine/slices"
	"github.com/agentic-platform/engine/store"
	"github.com/agentic-platform/slice"
)

// What the engine embeds from outside its module. Go's embed reaches only files inside the
// package's folder, so scripts/generate.sh copies each gen/<path> named here from src/<path>
// (never committed; `generate.sh --check` proves the copies match their sources).
//
//go:embed gen/catalogue/api-feature-go/pattern.yaml
var pattern []byte

// The analyst's definition, as the analyst service mounts src/agents at /agents.
//
//go:embed gen/agents/analyst.yaml gen/agents/analyst/instruction.md
var agentFiles embed.FS

// agentRoot is where agentFiles holds src/agents.
const agentRoot = "gen/agents"

// variables are the settings setup reads, all required.
var variables = []string{
	"ENGINE_LISTEN_ADDRESS", "ENGINE_CLIENT_TOKEN", "ENGINE_PAGE_TOKEN", "ENGINE_WORKER_TOKEN",
	"AP_LINT_URL", "AP_LINT_TOKEN", "AP_ANALYST_URL", "AP_ANALYST_TOKEN", "ENGINE_STORE_DIR",
}

// tokenVariables must all differ from each other: each names one caller or one callee.
var tokenVariables = []string{"ENGINE_CLIENT_TOKEN", "ENGINE_PAGE_TOKEN", "ENGINE_WORKER_TOKEN", "AP_LINT_TOKEN", "AP_ANALYST_TOKEN"}

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
	for _, name := range []string{"AP_LINT_URL", "AP_ANALYST_URL"} {
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
	digest, err := agentDigest(agentFiles, agentRoot)
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

	ctx, cancel := context.WithCancel(context.Background())
	h := api.NewHandler(api.Config{
		Tokens:  api.Tokens{Client: env["ENGINE_CLIENT_TOKEN"], Page: env["ENGINE_PAGE_TOKEN"], Worker: env["ENGINE_WORKER_TOKEN"]},
		Context: ctx,
		Slices:  svc,
		Store:   st,
	})
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

// agentDigest is the SHA-256, in lowercase hex, of the agent definition files under root in their
// framed form (canonical.FramedHash), each by its path relative to root (the path under the
// analyst service's /agents), so that no two sets of files give the same digest.
func agentDigest(files fs.FS, root string) (string, error) {
	var set []canonical.File
	err := fs.WalkDir(files, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
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
