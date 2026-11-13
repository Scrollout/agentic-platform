package procedures

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/agentic-platform/engine/canonical"
	"github.com/agentic-platform/engine/checks"
)

// GenerateConfig is where the generate procedure finds its tools. None of it is an input of the
// step: the same contract gives the same files whatever the host.
type GenerateConfig struct {
	// OapiCodegen is the path of the pinned oapi-codegen binary (src/deploy/images.lock).
	OapiCodegen string
	// Template is the pattern's module template folder (src/catalogue/api-feature-go/template).
	Template string
	// Env is added to the minimal environment of every command the procedure runs
	// (checks.CommandEnv: PATH, HOME and the Go caches and settings, never a token), for example
	// GOMODCACHE or GOCACHE; a later entry wins. checks.OfflineGo comes after it: no module proxy,
	// no workspace, the local toolchain only.
	Env []string
}

// vendorOnlyDir is the template folder whose package lists what to vendor besides the packages
// the generated code imports; it is used only while vendoring and is never part of the output.
const vendorOnlyDir = "vendortools"

// generateDirective is the start of the line in the template's gen/*.go that says how oapi-codegen
// runs: the procedure runs that command, with OapiCodegen as the binary, in gen/, and the
// GenerationRepeatable check runs the same line with go generate.
const generateDirective = "//go:generate oapi-codegen "

// Generate is the generate procedure. From the contract output of contract-check (the framed form
// of api/openapi.yaml) it produces the output generated-code: the template's files (go.mod, go.sum,
// gen/), the code oapi-codegen generates into gen/ by the template's directive, gen/stub.go (a
// server that answers every operation 501 Not Implemented) and vendor/ from `go mod vendor`, all
// offline. It runs the whole generation twice in fresh folders and completes only when both give
// the same bytes, go.mod and go.sum are the template's byte for byte (so the template's go.sum
// verified every module), and the module builds from vendor/ alone.
//
// A wrong input set, a contract oapi-codegen rejects, a go.sum that disagrees with a module and
// a module that does not build are Completed false, with a reason naming project paths only. A
// content that does not have its hash is ErrInputHashMismatch; a tool or template that cannot be
// used is another error.
func Generate(ctx context.Context, cfg GenerateConfig, in []Input) (Result, error) {
	got, reason, err := takeInputs(in, "contract")
	if err != nil || reason != "" {
		return refused("%s", reason), err
	}
	files, err := canonical.Unframe(got["contract"].Content)
	if err != nil {
		return refused("the contract input is not a framed set of files: %v", err), nil
	}
	if len(files) != 1 || files[0].Path != contractPath {
		var paths []string
		for _, f := range files {
			paths = append(paths, f.Path)
		}
		return refused("the contract input must hold %s only, not [%s]", contractPath, strings.Join(paths, ", ")), nil
	}
	if cfg.OapiCodegen == "" || cfg.Template == "" {
		return Result{}, errors.New("generate: GenerateConfig needs OapiCodegen and Template")
	}
	var runs [2][]File
	for i := range runs {
		files, reason, err := generateOnce(ctx, cfg, files[0].Content)
		if err != nil || reason != "" {
			return refused("%s", reason), err
		}
		runs[i] = files
	}
	if diff := differing(runs[0], runs[1]); len(diff) > 0 {
		return refused("two generations from the same contract differ in %s", strings.Join(diff, ", ")), nil
	}
	o := output("generated-code", runs[0])
	return Result{
		Completed: true,
		Reason:    fmt.Sprintf("generated gen/ from %s and vendored the dependencies offline: %d files, the same in two runs", contractPath, len(o.Files)),
		Outputs:   []Output{o},
	}, nil
}

// generateOnce runs one generation in a fresh folder and returns the output's files, or a reason.
func generateOnce(ctx context.Context, cfg GenerateConfig, contract []byte) ([]File, string, error) {
	dir, err := os.MkdirTemp("", "generate-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(dir)
	template, err := readFolder(cfg.Template)
	if err != nil {
		return nil, "", fmt.Errorf("generate: the template cannot be read: %w", err)
	}
	var directive []string
	for _, f := range template {
		if strings.HasPrefix(f.Path, "gen/") && strings.HasSuffix(f.Path, ".go") && !strings.Contains(f.Path[len("gen/"):], "/") {
			for l := range strings.Lines(string(f.Content)) {
				if args, ok := strings.CutPrefix(strings.TrimRight(l, "\r\n"), generateDirective); ok {
					if directive != nil {
						return nil, "", errors.New("generate: the template's gen/ has more than one oapi-codegen directive")
					}
					directive = strings.Fields(args)
				}
			}
		}
	}
	if directive == nil {
		return nil, "", fmt.Errorf("generate: the template's gen/ has no %q line", strings.TrimSpace(generateDirective))
	}
	keep := map[string][]byte{}
	for _, f := range template {
		keep[f.Path] = f.Content
	}
	if keep["go.mod"] == nil || keep["go.sum"] == nil {
		return nil, "", errors.New("generate: the template has no go.mod or no go.sum")
	}
	if _, ok := keep[contractPath]; ok {
		return nil, "", fmt.Errorf("generate: the template holds %s, which is the step's input", contractPath)
	}
	for _, f := range append(template, File{Path: contractPath, Content: contract}) {
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, "", err
		}
		if err := os.WriteFile(p, f.Content, 0o644); err != nil {
			return nil, "", err
		}
	}
	gen := filepath.Join(dir, "gen")
	run := func(what, workdir string, env []string, name string, args ...string) (string, error) {
		out, err := command(ctx, workdir, checks.CommandEnv(append(append([]string{}, cfg.Env...), env...)...), name, args...)
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Sprintf("%s failed: %s", what, scrub(out, dir)), nil
		}
		if err != nil {
			return "", fmt.Errorf("generate: %s could not run: %w", what, err)
		}
		return "", nil
	}
	if reason, err := run("oapi-codegen on "+contractPath, gen, nil, cfg.OapiCodegen, directive...); err != nil || reason != "" {
		return nil, reason, err
	}
	stub, err := stubFor(gen)
	if err != nil {
		return nil, "the generated code in gen/ cannot be read for the stub: " + scrub([]byte(err.Error()), dir), nil
	}
	if err := os.WriteFile(filepath.Join(gen, "stub.go"), stub, 0o644); err != nil {
		return nil, "", err
	}
	// -mod=mod lets go mod vendor read the module cache; go.sum then verifies every module, and
	// any line the vendoring would add or change is caught below.
	if reason, err := run("go mod vendor", dir, []string{"GOFLAGS=-mod=mod"}, "go", "mod", "vendor"); err != nil || reason != "" {
		return nil, reason, err
	}
	for _, p := range []string{"go.mod", "go.sum"} {
		b, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil {
			return nil, "", err
		}
		if !bytes.Equal(b, keep[p]) {
			return nil, fmt.Sprintf("go mod vendor changed %s, so the template's go.mod and go.sum do not cover the generated code's dependencies", p), nil
		}
	}
	if reason, err := run("go build ./... from vendor/", dir, []string{"GOFLAGS=-mod=vendor"}, "go", "build", "./..."); err != nil || reason != "" {
		return nil, reason, err
	}
	all, err := readFolder(dir)
	if err != nil {
		return nil, "", err
	}
	var files []File
	for _, f := range all {
		if f.Path == contractPath || strings.HasPrefix(f.Path, vendorOnlyDir+"/") {
			continue
		}
		if !IsProjectPath(f.Path) {
			return nil, fmt.Sprintf("the generation wrote %q, which is not a project path", f.Path), nil
		}
		files = append(files, f)
	}
	return files, "", nil
}

// readFolder reads every regular file below root by its slash-separated path. Anything that is
// neither a folder nor a regular file (a symbolic link, for example) is refused.
func readFolder(root string) ([]File, error) {
	var files []File
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", rel)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, File{Path: rel, Content: b})
		return nil
	})
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return files, err
}

// differing lists the paths whose files differ between two sorted sets, or are in one only.
func differing(a, b []File) []string {
	m := map[string][]byte{}
	for _, f := range a {
		m[f.Path] = f.Content
	}
	var out []string
	for _, f := range b {
		if c, ok := m[f.Path]; !ok || !bytes.Equal(c, f.Content) {
			out = append(out, f.Path)
		}
		delete(m, f.Path)
	}
	for p := range m {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// command runs name in dir, in its own process group so that a cancelled ctx kills every process
// it started, and returns the combined output.
func command(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

// scrub keeps a tool's output to its last lines, with the work folder's paths made relative to
// it and every other absolute path replaced, so a reason names project paths only.
func scrub(out []byte, dir string) string {
	s := checks.HidePaths(string(out), dir)
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	s = strings.Join(lines, "; ")
	if len(s) > 4000 {
		s = s[len(s)-4000:]
	}
	if s == "" {
		s = "no output"
	}
	return s
}
