// Package steps holds the step agents' eval suites (task T041): the tests, code and review agents
// of the api-feature-go pattern, judged by deterministic graders that reuse engine/checks (no model
// judge, constitution XIX and XX), on the repository's example slice (a notes service).
//
// This file builds the project state before each step the way the platform does: the engine's
// fixed procedures (contract-check, then generate) produce the files, so no byte of generated code
// is written by hand. The reference tests and the reference implementation under testdata/ stand
// in for the earlier agent steps; they are what the grader self-tests judge, never what the live
// evals judge.
package steps

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/procedures"
)

//go:embed all:testdata
var data embed.FS

// Slice is the approved part of a slice the pattern's procedures take: the contract's text and the
// rules part's examples.
type Slice struct {
	Contract, Examples string
}

func read(p string) string {
	b, err := data.ReadFile(path.Join("testdata", p))
	if err != nil {
		panic(err)
	}
	return string(b)
}

// ExampleSlice is the repository's example slice: the notes service of the quickstart (an
// in-memory list, ids from 1, empty text refused), with its contract and its Gherkin examples.
func ExampleSlice() Slice {
	return Slice{Contract: read("slice/contract.yaml"), Examples: read("slice/slice.feature")}
}

// Files is a set of project files by slash-separated path.
type Files map[string]string

// Ref returns a reference file set from testdata/ref/<name>: the project files below it.
func Ref(name string) Files {
	out := Files{}
	root := path.Join("testdata", "ref", name)
	err := fs.WalkDir(data, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := data.ReadFile(p)
		out[strings.TrimPrefix(p, root+"/")] = string(b)
		return err
	})
	if err != nil {
		panic(err)
	}
	return out
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// oapiCodegen finds the pinned oapi-codegen: AP_OAPI_CODEGEN, else oapi-codegen on PATH, else built once offline from the pin
// in src/deploy/images.lock (as the engine's own procedure tests do).
var (
	oapiOnce sync.Once
	oapiPath string
	oapiErr  error
	oapiDir  string
)

func repoRoot() (string, error) {
	d, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(d, "src", "deploy", "images.lock")); err == nil {
			return d, nil
		}
		p := filepath.Dir(d)
		if p == d {
			return "", fmt.Errorf("repository root not found (no src/deploy/images.lock above the working directory)")
		}
		d = p
	}
}

func oapiCodegen(root string) (string, error) {
	if p := os.Getenv("AP_OAPI_CODEGEN"); p != "" {
		return p, nil
	}
	oapiOnce.Do(func() {
		if p, err := exec.LookPath("oapi-codegen"); err == nil {
			oapiPath = p
			return
		}
		lock, err := os.ReadFile(filepath.Join(root, "src", "deploy", "images.lock"))
		if err != nil {
			oapiErr = err
			return
		}
		var pin string
		for _, l := range strings.Split(string(lock), "\n") {
			if f := strings.Fields(l); len(f) == 2 && f[0] == "oapi-codegen" {
				pin = f[1]
			}
		}
		if pin == "" {
			oapiErr = fmt.Errorf("no oapi-codegen pin in src/deploy/images.lock")
			return
		}
		oapiDir, err = os.MkdirTemp("", "steps-oapi-")
		if err != nil {
			oapiErr = err
			return
		}
		cmd := exec.Command("go", "install", pin)
		cmd.Env = append(os.Environ(), "GOBIN="+oapiDir, "GOPROXY=off", "GOWORK=off", "GOFLAGS=-mod=mod")
		if out, err := cmd.CombinedOutput(); err != nil {
			oapiErr = fmt.Errorf("go install %s (offline): %v\n%s", pin, err, out)
			return
		}
		oapiPath = filepath.Join(oapiDir, "oapi-codegen")
	})
	return oapiPath, oapiErr
}

// base caches the project after contract-check and generate, per slice, for the process.
var (
	baseMu sync.Mutex
	bases  = map[Slice]Files{}
	// tmp holds every folder this process made; Cleanup removes them.
	tmpMu sync.Mutex
	tmps  []string
)

// Cleanup removes every folder the fixtures made. Call it from TestMain.
func Cleanup() {
	tmpMu.Lock()
	defer tmpMu.Unlock()
	for _, d := range tmps {
		os.RemoveAll(d)
	}
	tmps = nil
	if oapiDir != "" {
		os.RemoveAll(oapiDir)
	}
}

// Base returns the project after the contract-check and generate steps for s, produced by the
// engine's own procedures: api/openapi.yaml, features/slice.feature, gen/**, internal/app/app.go (the
// seam), go.mod, go.sum and vendor/**. The result is cached; callers get a copy.
func Base(s Slice) (Files, error) {
	baseMu.Lock()
	defer baseMu.Unlock()
	if f, ok := bases[s]; ok {
		return clone(f), nil
	}
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}
	oapi, err := oapiCodegen(root)
	if err != nil {
		return nil, fmt.Errorf("oapi-codegen is not available offline: %w", err)
	}
	ctx := context.Background()
	h, err := canonical.ContractHash(s.Contract)
	if err != nil {
		return nil, err
	}
	cb, _ := json.Marshal(map[string]any{"contract": s.Contract})
	rb, _ := json.Marshal(map[string]any{"rules": []any{}, "examples": s.Examples, "terms": []any{}})
	cc, err := procedures.ContractCheck(ctx, []procedures.Input{
		{Name: "slice/contract", SHA256: h, Content: cb},
		{Name: "slice/rules", SHA256: sum(rb), Content: rb},
	})
	if err != nil || !cc.Completed {
		return nil, fmt.Errorf("contract-check: %v %s", err, cc.Reason)
	}
	files := Files{}
	for _, o := range cc.Outputs {
		for _, f := range o.Files {
			files[f.Path] = string(f.Content)
		}
	}
	fr := procedures.Framed([]procedures.File{{Path: "api/openapi.yaml", Content: []byte(files["api/openapi.yaml"])}})
	env := []string{"GOMODCACHE=" + os.Getenv("GOMODCACHE"), "GOFLAGS=-mod=mod", "CGO_ENABLED=0"}
	if os.Getenv("GOMODCACHE") == "" {
		env = []string{"GOFLAGS=-mod=mod", "CGO_ENABLED=0"}
	}
	gr, err := procedures.Generate(ctx, procedures.GenerateConfig{
		OapiCodegen: oapi,
		Template:    filepath.Join(root, "src", "catalogue", "api-feature-go", "template"),
		Env:         env,
	}, []procedures.Input{{Name: "contract", SHA256: sum(fr), Content: fr}})
	if err != nil || !gr.Completed {
		return nil, fmt.Errorf("generate: %v %s", err, gr.Reason)
	}
	for _, o := range gr.Outputs {
		for _, f := range o.Files {
			files[f.Path] = string(f.Content)
		}
	}
	bases[s] = files
	return clone(files), nil
}

func clone(f Files) Files {
	out := Files{}
	for k, v := range f {
		out[k] = v
	}
	return out
}

// Overlay returns a copy of f with every file of the others added or replaced; an empty value in
// an overlay deletes the file.
func Overlay(f Files, others ...Files) Files {
	out := clone(f)
	for _, o := range others {
		for k, v := range o {
			if v == "" {
				delete(out, k)
			} else {
				out[k] = v
			}
		}
	}
	return out
}

// Write writes files into a new folder and returns it. The folder is removed by Cleanup.
func Write(files Files) (string, error) {
	dir, err := os.MkdirTemp("", "steps-project-")
	if err != nil {
		return "", err
	}
	tmpMu.Lock()
	tmps = append(tmps, dir)
	tmpMu.Unlock()
	return dir, WriteInto(dir, files)
}

// WriteInto writes files below dir.
func WriteInto(dir string, files Files) error {
	for p, c := range files {
		fp := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(fp, []byte(c), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Read returns every regular file below dir.
func Read(dir string) (Files, error) {
	out := Files{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		b, err := os.ReadFile(p)
		out[filepath.ToSlash(rel)] = string(b)
		return err
	})
	return out, err
}
