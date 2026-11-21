package steps

// Variant builds for the graders that judge a fixed list (README "Graders", research R3 and R4,
// ADR 0077): every variant of a project is built first, sharing one build cache, and then each
// test binary runs alone, its per-test results read from test2json events.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/agentic-platform/engine/checks"
)

// Variant is one build of the project: Overlay replaces files for this build only
// (project-relative slash path -> full new content); nil or empty builds the project as is.
type Variant struct {
	Name    string
	Overlay map[string]string
}

// Outcome of one test, or of one binary's run as a whole.
type Outcome string

const (
	OutcomePassed    Outcome = "passed"
	OutcomeFailed    Outcome = "failed"      // a test failed or panicked
	OutcomeTimedOut  Outcome = "timed out"   // the runner returned an error whose text starts with "timed out"
	OutcomeDidNotRun Outcome = "did not run" // any other runner error, no event, or an expected name with no event
)

// Binary is one test binary's run: one package folder of the variant.
type Binary struct {
	Dir   string             // the package folder relative to the project, slash form: "tests", "tests/more", "heldout"
	Tests map[string]Outcome // every top-level test named in the events, plus every Expected name (OutcomeDidNotRun when it has no event)
	Run   Outcome            // the run as a whole: OutcomePassed only when it reported at least one passing test and none failed and every Expected name passed; OutcomeFailed when a test failed or panicked; else timed out / did not run
}

// VariantResult is one variant: either it did not build (BuildError non-empty, Binaries nil),
// or it has one Binary per package folder with tests, sorted by Dir.
type VariantResult struct {
	Name       string
	BuildError string
	Binaries   []Binary
}

// VariantRun builds every variant of a project first, then runs each test binary alone.
type VariantRun struct {
	// Runner returns the go runner for one call. copy is the scratch copy's absolute path (the
	// folder a container mounts read-only as its root), on every call. The build phase calls it
	// with the run's one build-cache folder (a fresh folder created by Variants, removed before it
	// returns); the run phase calls it with cache "" (no cache). DockerRunner.For is such a
	// function; the host runner ignores both.
	Runner   func(copy, cache string) checks.GoRunner
	Pkg      string            // "tests" or "heldout": each variant builds ./<Pkg>/...
	Expected []string          // test names every binary must report (held-out: the 6); nil: none
	Add      map[string]string // files added to the scratch copy before any build (e.g. "heldout/heldout_test.go")
}

// overlayDir is the folder inside the scratch copy that holds each variant's overlay JSON and
// replacement files during the build phase; it is removed before any binary runs.
const overlayDir = ".apeval-overlays"

// Variants tells, for each variant of project in order, whether it built and, per test binary,
// which tests passed, failed, timed out or did not run. The project itself is never written.
//
// It copies project to a fresh scratch folder (regular files and folders; a symbolic link is
// copied as a link, never followed) and adds Add. Then:
//   - build phase, every variant before any run: Runner(<copy>, cache).Go(<copy>, "test", "-c",
//     "-vet=off", "-mod=vendor", "-overlay=<json>" (only when Overlay is non-empty; the JSON and
//     the replacement files are written inside the copy and removed before the run phase), "-o",
//     "<bins>/<variant folder>/", "./<Pkg>/..."), where <variant folder> is a fresh random name
//     (16 hex characters from crypto/rand), never the variant's index or name, so a test binary
//     cannot tell from its own path whether it is the baseline. <bins> and the cache folder are fresh folders
//     outside the copy, removed before Variants returns. A non-zero exit or a runner error is
//     that variant's BuildError (its output, or the error), and only that variant's.
//   - run phase: for each built binary, Runner(<copy>, "").Go(<copy>/<package folder>, "tool",
//     "test2json", "-t", "<binary>", "-test.v=test2json", "-test.count=1"), one call per binary.
//     The outcome comes only from the test2json events in the output, never from the exit code:
//     a top-level test is failed when it (or one of its subtests) has a fail event, or when it
//     started, printed "panic:" and never ended; passed on its pass event; a test that started
//     and never ended without a panic did not run. A runner error whose text starts with
//     "timed out" makes the run and every Expected name OutcomeTimedOut; any other runner error
//     makes them OutcomeDidNotRun.
//
// Package folders are the folders at or under <copy>/<Pkg> that hold a *_test.go file, found by
// walking the copy, skipping what the go command's ./<Pkg>/... skips: folders named testdata or
// starting with "." or "_", and nested modules (a folder with its own go.mod). go test -c -o
// <dir>/ names each binary "<last element of the package folder>.test", so the binary of folder F
// is <bins>/<variant folder>/<base(F)>.test, and a folder whose binary is missing after a
// successful build gets Run and every Expected name OutcomeDidNotRun.
//
// Limit: when two package folders share their last element (tests/a/x and tests/b/x), their
// binaries would collide, so no variant is built and every variant's BuildError names the folders
// that clash.
//
// The error is only for setup failures: the project cannot be copied, an Add path is not inside
// the project, a folder cannot be made. An Overlay path that is not inside the project is that
// variant's BuildError.
func (v VariantRun) Variants(project string, vs []Variant) ([]VariantResult, error) {
	tmp, err := os.MkdirTemp("", "steps-variants-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	work := filepath.Join(tmp, "copy")
	bins := filepath.Join(tmp, "bins")
	cache := filepath.Join(tmp, "cache")
	if err := copyTree(project, work); err != nil {
		return nil, fmt.Errorf("the project cannot be copied: %w", err)
	}
	for rel, content := range v.Add {
		p, err := inside(work, rel)
		if err != nil {
			return nil, err
		}
		if err := writeFile(p, content); err != nil {
			return nil, err
		}
	}
	if len(vs) == 0 {
		return []VariantResult{}, nil
	}
	for _, d := range []string{bins, cache} {
		if err := os.Mkdir(d, 0o750); err != nil {
			return nil, err
		}
	}
	dirs, err := packageFolders(work, v.Pkg)
	if err != nil {
		return nil, err
	}
	res := make([]VariantResult, len(vs))
	for i, x := range vs {
		res[i].Name = x.Name
	}
	if clash := baseClash(dirs); clash != "" {
		for i := range res {
			res[i].BuildError = clash
		}
		return res, nil
	}

	// Build phase.
	folders := make([]string, len(vs))
	for i, x := range vs {
		if folders[i], err = randomName(); err != nil {
			return nil, err
		}
		out := filepath.Join(bins, folders[i])
		if err := os.Mkdir(out, 0o750); err != nil {
			return nil, err
		}
		args := []string{"test", "-c", "-vet=off", "-mod=vendor"}
		if len(x.Overlay) > 0 {
			o, msg, err := writeOverlay(work, folders[i], x.Overlay)
			if err != nil {
				return nil, err
			}
			if msg != "" {
				res[i].BuildError = msg
				continue
			}
			args = append(args, "-overlay="+o)
		}
		args = append(args, "-o", out+string(filepath.Separator), "./"+v.Pkg+"/...")
		r, err := v.Runner(work, cache).Go(work, args...)
		switch {
		case err != nil:
			res[i].BuildError = err.Error()
		case r.ExitCode != 0:
			res[i].BuildError = strings.TrimSpace(r.Output)
			if res[i].BuildError == "" {
				res[i].BuildError = fmt.Sprintf("go test -c exited with code %d", r.ExitCode)
			}
		}
	}
	if err := os.RemoveAll(filepath.Join(work, overlayDir)); err != nil {
		return nil, err
	}

	// Run phase.
	for i := range vs {
		if res[i].BuildError != "" {
			continue
		}
		res[i].Binaries = []Binary{}
		for _, d := range dirs {
			bin := filepath.Join(bins, folders[i], path.Base(d)+".test")
			b := Binary{Dir: d}
			if _, err := os.Stat(bin); err != nil {
				b.Tests, b.Run = v.none(OutcomeDidNotRun), OutcomeDidNotRun
			} else {
				r, err := v.Runner(work, "").Go(filepath.Join(work, filepath.FromSlash(d)),
					"tool", "test2json", "-t", bin, "-test.v=test2json", "-test.count=1")
				b.Tests, b.Run = v.judge(r, err)
			}
			res[i].Binaries = append(res[i].Binaries, b)
		}
	}
	return res, nil
}

// none gives every Expected name the outcome o.
func (v VariantRun) none(o Outcome) map[string]Outcome {
	m := map[string]Outcome{}
	for _, n := range v.Expected {
		m[n] = o
	}
	return m
}

// judge turns one run's answer into per-test outcomes and the run's outcome.
func (v VariantRun) judge(r checks.GoResult, err error) (map[string]Outcome, Outcome) {
	if err != nil {
		if strings.HasPrefix(err.Error(), "timed out") {
			return v.none(OutcomeTimedOut), OutcomeTimedOut
		}
		return v.none(OutcomeDidNotRun), OutcomeDidNotRun
	}
	tests := testOutcomes(r.Output)
	for _, n := range v.Expected {
		if _, ok := tests[n]; !ok {
			tests[n] = OutcomeDidNotRun
		}
	}
	passed := 0
	for _, o := range tests {
		switch o {
		case OutcomeFailed:
			return tests, OutcomeFailed
		case OutcomePassed:
			passed++
		}
	}
	if passed == 0 {
		return tests, OutcomeDidNotRun
	}
	for _, n := range v.Expected {
		if tests[n] != OutcomePassed {
			return tests, OutcomeDidNotRun
		}
	}
	return tests, OutcomePassed
}

// testOutcomes reads test2json events (one JSON object per line; other lines are ignored) into
// the outcome of each top-level test they name.
func testOutcomes(output string) map[string]Outcome {
	type event struct{ Action, Test, Output string }
	tests := map[string]Outcome{}
	ended := map[string]bool{}
	panicked := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		var e event
		if json.Unmarshal([]byte(line), &e) != nil || e.Test == "" {
			continue
		}
		top, _, _ := strings.Cut(e.Test, "/")
		if _, ok := tests[top]; !ok {
			tests[top] = OutcomeDidNotRun
		}
		switch e.Action {
		case "fail":
			tests[top], ended[top] = OutcomeFailed, true
		case "pass":
			if e.Test == top {
				ended[top] = true
				if tests[top] != OutcomeFailed {
					tests[top] = OutcomePassed
				}
			}
		case "skip":
			if e.Test == top {
				ended[top] = true
			}
		}
		if strings.Contains(e.Output, "panic:") {
			panicked[top] = true
		}
	}
	for n := range tests {
		if !ended[n] && panicked[n] {
			tests[n] = OutcomeFailed
		}
	}
	return tests
}

// packageFolders lists, sorted and in slash form relative to root, the folders at or under
// root/pkg that hold a *_test.go file and that ./<pkg>/... matches.
func packageFolders(root, pkg string) ([]string, error) {
	start := filepath.Join(root, filepath.FromSlash(pkg))
	if _, err := os.Stat(start); os.IsNotExist(err) {
		return nil, nil
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != start {
				n := d.Name()
				if n == "testdata" || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_") {
					return filepath.SkipDir
				}
				if _, err := os.Lstat(filepath.Join(p, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if d.Type().IsRegular() && strings.HasSuffix(d.Name(), "_test.go") {
			rel, err := filepath.Rel(root, filepath.Dir(p))
			if err != nil {
				return err
			}
			seen[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	var dirs []string
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs, err
}

// baseClash names the package folders whose last elements are the same, or returns "".
func baseClash(dirs []string) string {
	by := map[string][]string{}
	for _, d := range dirs {
		by[path.Base(d)] = append(by[path.Base(d)], d)
	}
	var clashes []string
	for _, ds := range by {
		if len(ds) > 1 {
			clashes = append(clashes, strings.Join(ds, ", "))
		}
	}
	if len(clashes) == 0 {
		return ""
	}
	sort.Strings(clashes)
	return "package folders share their last element, so their test binaries would have one name: " +
		strings.Join(clashes, "; ")
}

// randomName is 16 random hex characters.
func randomName() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// writeOverlay writes a variant's replacement files and its go -overlay JSON inside the copy and
// returns the JSON's path; msg is non-empty when an overlay path is not inside the project.
func writeOverlay(work, folder string, overlay map[string]string) (jsonPath, msg string, err error) {
	dir := filepath.Join(work, overlayDir, folder)
	replace := map[string]string{}
	n := 0
	for rel, content := range overlay {
		target, err := inside(work, rel)
		if err != nil {
			return "", err.Error(), nil
		}
		n++
		repl := filepath.Join(dir, strconv.Itoa(n)+"-"+filepath.Base(target))
		if err := writeFile(repl, content); err != nil {
			return "", "", err
		}
		replace[target] = repl
	}
	b, err := json.Marshal(struct{ Replace map[string]string }{replace})
	if err != nil {
		return "", "", err
	}
	jsonPath = filepath.Join(dir, "overlay.json")
	return jsonPath, "", writeFile(jsonPath, string(b))
}

// inside returns the path of the project-relative slash path rel below root, or an error when rel
// is absolute or leaves root.
func inside(root, rel string) (string, error) {
	c := path.Clean(rel)
	if rel == "" || path.IsAbs(c) || c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("the path %q is not inside the project", rel)
	}
	return filepath.Join(root, filepath.FromSlash(c)), nil
}

// writeFile writes content to p, making its folders. Owner-only permissions suffice: the go
// command reads the file as the invoking user, on the host and in a container (--user).
func writeFile(p, content string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), 0o600)
}

// copyTree copies the folder src to the new folder dst: folders, regular files (owner
// permissions, keeping the owner's execute bit) and symbolic links (as links, never followed).
// Anything else is an error. Both trees are opened as os.Root, so no path, link or rename made
// while it copies can make it read or write outside them.
func copyTree(src, dst string) error {
	from, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer func() { _ = from.Close() }()
	if err := os.Mkdir(dst, 0o750); err != nil {
		return err
	}
	to, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer func() { _ = to.Close() }()
	return fs.WalkDir(from.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case p == ".":
			return nil
		case d.IsDir():
			return to.Mkdir(p, 0o750)
		case d.Type()&fs.ModeSymlink != 0:
			link, err := from.Readlink(p)
			if err != nil {
				return err
			}
			return to.Symlink(link, p)
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			b, err := fs.ReadFile(from.FS(), p)
			if err != nil {
				return err
			}
			return to.WriteFile(p, b, info.Mode().Perm()&0o100|0o600)
		}
		return fmt.Errorf("%s is neither a folder, a regular file nor a link", filepath.Join(src, p))
	})
}
