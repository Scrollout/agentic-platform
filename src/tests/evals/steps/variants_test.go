package steps

// Tests of variants.go (T004): VariantRun.Variants builds every variant of a project first, with one
// shared build-cache folder, then runs each test binary alone, and DockerRunner's argument lists.
// Names follow `run.sh graders` (TestGrader...).

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-platform/engine/checks"
)

// ---- the fake runner ----

type vtCall struct {
	copy    string   // the copy argument the runner was made for
	variant int      // the variant the call belongs to (build order; a run: the build of its binary)
	left    []string // overlay files (JSON, replacements) that still existed when a run call started
	cache   string
	dir     string
	args    []string
	overlay string // the overlay JSON as it was while the call ran ("" when no -overlay)
	cacheOK bool   // the cache folder existed as a directory while the call ran
	binOK   bool   // for a run: the binary file existed
}

func (c vtCall) isBuild() bool { return len(c.args) > 0 && c.args[0] == "test" }
func (c vtCall) isRun() bool {
	return len(c.args) > 1 && c.args[0] == "tool" && c.args[1] == "test2json"
}

// out returns the value of the "-o" argument ("" when absent).
func (c vtCall) out() string {
	for i, a := range c.args {
		if a == "-o" && i+1 < len(c.args) {
			return c.args[i+1]
		}
	}
	return ""
}

func (c vtCall) overlayPath() string {
	for _, a := range c.args {
		if p, ok := strings.CutPrefix(a, "-overlay="); ok {
			return p
		}
	}
	return ""
}

// vtFake answers every call from build and run scripts and records it.
type vtFake struct {
	mu    sync.Mutex
	calls []vtCall
	outs  map[string]int // -o folder (cleaned) -> variant index, by the order of the build calls
	files []string       // every overlay file seen in a build call
	nb    int
	// build answers a build call, by the variant index (the order of the build calls). It returns
	// whether to write the binaries (ok), and the answer.
	build func(i int) (write func(base string) bool, res checks.GoResult, err error)
	// run answers a run call, by variant index and the binary's base name ("tests.test").
	run func(i int, base string) (checks.GoResult, error)
}

func (f *vtFake) Runner(copy, cache string) checks.GoRunner { return vtRunner{f, copy, cache} }

type vtRunner struct {
	f     *vtFake
	copy  string
	cache string
}

func (r vtRunner) Go(dir string, args ...string) (checks.GoResult, error) {
	f := r.f
	c := vtCall{copy: r.copy, cache: r.cache, dir: dir, args: append([]string(nil), args...)}
	if st, err := os.Stat(r.cache); r.cache != "" && err == nil && st.IsDir() {
		c.cacheOK = true
	}
	if p := c.overlayPath(); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			return checks.GoResult{}, fmt.Errorf("fake: cannot read overlay %s: %w", p, err)
		}
		c.overlay = string(b)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case c.isBuild():
		i := f.nb
		f.nb++
		if f.outs == nil {
			f.outs = map[string]int{}
		}
		f.outs[filepath.Clean(c.out())] = i
		c.variant = i
		if c.overlay != "" {
			f.files = append(f.files, c.overlayPath())
			var o struct{ Replace map[string]string }
			if json.Unmarshal([]byte(c.overlay), &o) == nil {
				for _, v := range o.Replace {
					f.files = append(f.files, v)
				}
			}
		}
		write := func(string) bool { return true }
		res := checks.GoResult{}
		var err error
		if f.build != nil {
			write, res, err = f.build(i)
		}
		f.calls = append(f.calls, c)
		if err != nil || res.ExitCode != 0 {
			return res, err
		}
		if err := os.MkdirAll(c.out(), 0o750); err != nil {
			return checks.GoResult{}, err
		}
		for _, d := range vtTestFolders(dir, vtPkg(c.args)) {
			base := filepath.Base(d)
			if write(base) {
				if err := os.WriteFile(filepath.Join(c.out(), base+".test"), []byte("binary"), 0o600); err != nil {
					return checks.GoResult{}, err
				}
			}
		}
		return res, nil
	case c.isRun():
		bin := ""
		for i, a := range c.args {
			if a == "-t" && i+1 < len(c.args) {
				bin = c.args[i+1]
			}
		}
		if _, err := os.Stat(bin); err == nil {
			c.binOK = true
		}
		c.variant = f.outs[filepath.Clean(filepath.Dir(bin))]
		for _, o := range f.files {
			if vtExists(o) {
				c.left = append(c.left, o)
			}
		}
		f.calls = append(f.calls, c)
		if f.run == nil {
			return checks.GoResult{}, fmt.Errorf("fake: no run script")
		}
		return f.run(c.variant, filepath.Base(bin))
	}
	f.calls = append(f.calls, c)
	return checks.GoResult{}, fmt.Errorf("fake: unexpected call %v", args)
}

func vtPkg(args []string) string {
	for _, a := range args {
		if p, ok := strings.CutPrefix(a, "./"); ok {
			return strings.TrimSuffix(p, "/...")
		}
	}
	return ""
}

// vtTestFolders are the folders at or under root/pkg holding a *_test.go file (what go test -c
// writes one binary for), sorted.
func vtTestFolders(root, pkg string) []string {
	var out []string
	_ = filepath.WalkDir(filepath.Join(root, pkg), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, "_test.go") {
			dir := filepath.Dir(p)
			if len(out) == 0 || out[len(out)-1] != dir {
				out = append(out, dir)
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func (f *vtFake) builds() (b []vtCall) {
	for _, c := range f.calls {
		if c.isBuild() {
			b = append(b, c)
		}
	}
	return
}

func (f *vtFake) runs() (r []vtCall) {
	for _, c := range f.calls {
		if c.isRun() {
			r = append(r, c)
		}
	}
	return
}

// ---- test2json events ----

type vtEv struct{ Action, Test, Output string }

func vtEvents(evs ...vtEv) checks.GoResult {
	var b strings.Builder
	for _, e := range evs {
		m := map[string]string{"Time": "2026-01-01T00:00:00Z", "Action": e.Action, "Package": "example.com/svc/tests"}
		if e.Test != "" {
			m["Test"] = e.Test
		}
		if e.Output != "" {
			m["Output"] = e.Output
		}
		j, _ := json.Marshal(m)
		b.Write(j)
		b.WriteByte('\n')
	}
	return checks.GoResult{Output: b.String()}
}

// vtTest is the events of one test that ran to the end.
func vtTest(name, action string) []vtEv {
	return []vtEv{
		{"run", name, ""},
		{"output", name, "=== RUN   " + name + "\n"},
		{"output", name, "--- " + strings.ToUpper(action) + ": " + name + " (0.00s)\n"},
		{action, name, ""},
	}
}

func vtPackage(action string) []vtEv {
	return []vtEv{{"output", "", strings.ToUpper(action) + "\n"}, {action, "", ""}}
}

func vtCat(parts ...[]vtEv) (all []vtEv) {
	for _, p := range parts {
		all = append(all, p...)
	}
	return
}

// ---- a project on disk ----

const vtApp = "package app\n\nfunc Answer() int { return 1 }\n"

func vtProject(t *testing.T, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		"go.mod":                 "module example.com/svc\n\ngo 1.26\n",
		"internal/app/app.go":    vtApp,
		"internal/app/other.go":  "package app\n\nvar Other = 2\n",
		"tests/a_test.go":        "package tests\n",
		"tests/data.txt":         "data\n",
		"tests/more/b_test.go":   "package more\n",
		"tests/empty/readme.txt": "no tests here\n",
		"other/c_test.go":        "package other\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	root := t.TempDir()
	for p, c := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func vtSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	m := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			m[rel+"/"] = ""
			return nil
		}
		b, err := os.ReadFile(p)
		m[rel] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func vtInside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func vtExists(p string) bool { _, err := os.Stat(p); return err == nil }

// allPass answers every run with all of tests passing.
func vtAllPass(tests ...string) func(int, string) (checks.GoResult, error) {
	return func(int, string) (checks.GoResult, error) {
		var evs []vtEv
		for _, n := range tests {
			evs = append(evs, vtTest(n, "pass")...)
		}
		return vtEvents(vtCat(evs, vtPackage("pass"))...), nil
	}
}

func vtBinary(t *testing.T, r VariantResult, dir string) Binary {
	t.Helper()
	for _, b := range r.Binaries {
		if b.Dir == dir {
			return b
		}
	}
	t.Fatalf("variant %q has no binary for %q: %+v", r.Name, dir, r.Binaries)
	return Binary{}
}

// ---- (a) every variant is built before any binary runs, with one shared cache ----

func TestGraderVariantsBuildPhase(t *testing.T) {
	project := vtProject(t, nil)
	f := &vtFake{run: vtAllPass("TestA")}
	vs := []Variant{
		{Name: "base"},
		{Name: "m1", Overlay: map[string]string{"internal/app/app.go": "package app\n\nfunc Answer() int { return 2 }\n"}},
		{Name: "m2", Overlay: map[string]string{"internal/app/app.go": "package app\n\nfunc Answer() int { return 3 }\n"}},
	}
	res, err := VariantRun{Runner: f.Runner, Pkg: "tests"}.Variants(project, vs)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 {
		t.Fatalf("want 3 results, got %d", len(res))
	}
	for i, r := range res {
		if r.Name != vs[i].Name {
			t.Errorf("result %d is %q, want %q (order of the variants)", i, r.Name, vs[i].Name)
		}
	}
	builds, runs := f.builds(), f.runs()
	if len(builds) != 3 {
		t.Fatalf("want one build call per variant (3), got %d: %+v", len(builds), f.calls)
	}
	// every build precedes every run
	for i, c := range f.calls {
		if i < 3 && !c.isBuild() {
			t.Fatalf("call %d is not a build, but runs came before every variant was built: %+v", i, f.calls)
		}
		if i >= 3 && !c.isRun() {
			t.Fatalf("call %d after the builds is not a run: %v", i, c.args)
		}
	}
	cache := builds[0].cache
	if cache == "" {
		t.Fatal("the build call carries no cache folder")
	}
	var bins string
	names := map[string]bool{}
	for i, c := range builds {
		if c.cache != cache {
			t.Errorf("build %d has cache %q, want the one run cache %q", i, c.cache, cache)
		}
		if !c.cacheOK {
			t.Errorf("build %d: the cache folder did not exist while it ran", i)
		}
		o := c.out()
		if !strings.HasSuffix(o, "/") {
			t.Errorf("build %d: -o %q has no trailing slash", i, o)
		}
		folder := filepath.Base(filepath.Clean(o))
		if b := filepath.Dir(filepath.Clean(o)); bins == "" {
			bins = b
		} else if b != bins {
			t.Errorf("build %d: -o folder %q is not directly under the one bins folder %q", i, o, bins)
		}
		if folder == "" || folder == "." || folder == fmt.Sprint(i) || strings.Contains(folder, vs[i].Name) {
			t.Errorf("build %d: -o folder name %q reveals the variant (index %d, name %q)", i, folder, i, vs[i].Name)
		}
		if names[folder] {
			t.Errorf("build %d: -o folder name %q is not distinct", i, folder)
		}
		names[folder] = true
		if c.copy != c.dir || c.copy == project {
			t.Errorf("build %d: Runner got copy %q for dir %q (project %q); want the scratch copy", i, c.copy, c.dir, project)
		}
		if c.dir != builds[0].dir {
			t.Errorf("build %d dir %q differs from build 0 dir %q", i, c.dir, builds[0].dir)
		}
		// exact argument list
		want := []string{"test", "-c", "-vet=off", "-mod=vendor"}
		if p := c.overlayPath(); p != "" {
			want = append(want, "-overlay="+p)
		}
		want = append(want, "-o", c.out(), "./tests/...")
		if !reflect.DeepEqual(c.args, want) {
			t.Errorf("build %d args:\n got %v\nwant %v", i, c.args, want)
		}
	}
	if builds[0].overlayPath() != "" {
		t.Errorf("the variant with no overlay got an -overlay flag: %v", builds[0].args)
	}
	for i, c := range builds[1:] {
		if c.overlayPath() == "" {
			t.Errorf("variant %d has an overlay but no -overlay flag: %v", i+1, c.args)
		}
	}
	if len(runs) != 6 { // two package folders (tests, tests/more) per variant
		t.Errorf("want 6 run calls (2 binaries x 3 variants), got %d", len(runs))
	}
	for i, c := range runs {
		if c.cache != "" {
			t.Errorf("run %d carries cache %q, want none", i, c.cache)
		}
		if c.copy != builds[0].dir || c.copy == project || !vtInside(c.dir, c.copy) {
			t.Errorf("run %d: Runner got copy %q for dir %q; want the scratch copy %q containing the dir", i, c.copy, c.dir, builds[0].dir)
		}
		bin := c.args[3]
		rel, err := filepath.Rel(bins, bin)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Errorf("run %d: binary %q is not under bins %q", i, bin, bins)
			continue
		}
		for _, comp := range strings.Split(rel, string(filepath.Separator)) {
			if comp == fmt.Sprint(c.variant) || comp == vs[c.variant].Name || strings.Contains(comp, vs[c.variant].Name) {
				t.Errorf("run %d: binary path %q reveals variant %d (%q) by component %q", i, bin, c.variant, vs[c.variant].Name, comp)
			}
		}
		if len(c.left) != 0 {
			t.Errorf("run %d: overlay files still exist when the run starts: %v", i, c.left)
		}
	}
	if cache == project || vtInside(cache, project) || vtInside(cache, builds[0].dir) {
		t.Errorf("the cache folder %q must be outside the project and the copy", cache)
	}
	if bins == "" || vtInside(bins, builds[0].dir) || vtInside(bins, project) {
		t.Errorf("the binaries folder %q must be outside the project and the copy", bins)
	}
	if vtExists(cache) {
		t.Errorf("the cache folder %q still exists after Variants returned", cache)
	}
	if bins != "" && vtExists(bins) {
		t.Errorf("the binaries folder %q still exists after Variants returned", bins)
	}
}

func TestGraderVariantsCacheIsPerRunAndFresh(t *testing.T) {
	project := vtProject(t, nil)
	var caches []string
	for i := 0; i < 2; i++ {
		f := &vtFake{run: vtAllPass("TestA")}
		if _, err := (VariantRun{Runner: f.Runner, Pkg: "tests"}).Variants(project, []Variant{{Name: "base"}}); err != nil {
			t.Fatal(err)
		}
		caches = append(caches, f.builds()[0].cache)
	}
	// a fresh folder: nothing carried from the first run (it is gone), so it was made anew
	for _, c := range caches {
		if vtExists(c) {
			t.Errorf("cache %q was left behind", c)
		}
	}
}

// ---- (b) one call per binary, from its package folder in the copy; the project is untouched ----

func TestGraderVariantsRunsEachBinaryAlone(t *testing.T) {
	project := vtProject(t, nil)
	before := vtSnapshot(t, project)
	f := &vtFake{run: vtAllPass("TestA")}
	vr := VariantRun{Runner: f.Runner, Pkg: "tests", Add: map[string]string{"tests/added_test.go": "package tests\n// added\n"}}
	res, err := vr.Variants(project, []Variant{{Name: "base"}, {Name: "two"}})
	if err != nil {
		t.Fatal(err)
	}
	copyDir := f.builds()[0].dir
	if copyDir == project || vtInside(copyDir, project) || vtInside(project, copyDir) {
		t.Fatalf("the build ran in %q, which is the project %q or related to it", copyDir, project)
	}
	runs := f.runs()
	if len(runs) != 4 {
		t.Fatalf("want 4 run calls (2 binaries x 2 variants), got %d: %+v", len(runs), runs)
	}
	seen := map[string]bool{}
	for _, c := range runs {
		bin := c.args[3]
		want := []string{"tool", "test2json", "-t", bin, "-test.v=test2json", "-test.count=1"}
		if !reflect.DeepEqual(c.args, want) {
			t.Errorf("run args:\n got %v\nwant %v", c.args, want)
		}
		if !c.binOK {
			t.Errorf("the binary %q did not exist when it was run", bin)
		}
		if !strings.HasSuffix(bin, ".test") {
			t.Errorf("binary %q is not <package folder name>.test", bin)
		}
		base := strings.TrimSuffix(filepath.Base(bin), ".test")
		var wantDir string
		switch base {
		case "tests":
			wantDir = filepath.Join(copyDir, "tests")
		case "more":
			wantDir = filepath.Join(copyDir, "tests", "more")
		default:
			t.Errorf("unexpected binary %q (folders without tests, or outside tests/, must have none)", bin)
			continue
		}
		if c.dir != wantDir {
			t.Errorf("run of %q has dir %q, want the package folder %q in the copy", bin, c.dir, wantDir)
		}
		if seen[bin] {
			t.Errorf("binary %q was run twice", bin)
		}
		seen[bin] = true
	}
	for _, r := range res {
		if len(r.Binaries) != 2 || r.Binaries[0].Dir != "tests" || r.Binaries[1].Dir != "tests/more" {
			t.Errorf("variant %q binaries %+v, want dirs [tests tests/more] sorted", r.Name, r.Binaries)
		}
	}
	// the copy had Add's file when it was built (the walk found it), the project never has it
	if after := vtSnapshot(t, project); !reflect.DeepEqual(before, after) {
		t.Errorf("the project changed: before %d entries, after %d", len(before), len(after))
	}
	if vtExists(filepath.Join(project, "tests", "added_test.go")) {
		t.Error("Add's file was written into the project")
	}
}

func TestGraderVariantsAddedFolderGetsABinary(t *testing.T) {
	project := vtProject(t, map[string]string{"tests/a_test.go": "", "tests/more/b_test.go": ""})
	_ = os.Remove(filepath.Join(project, "tests", "a_test.go"))
	_ = os.Remove(filepath.Join(project, "tests", "more", "b_test.go"))
	// project has no heldout folder; Add creates it (held-out use)
	f := &vtFake{run: vtAllPass("TestR1_EmptyList")}
	vr := VariantRun{Runner: f.Runner, Pkg: "heldout", Expected: []string{"TestR1_EmptyList"},
		Add: map[string]string{"heldout/heldout_test.go": "package heldout_test\n"}}
	res, err := vr.Variants(project, []Variant{{Name: "base"}})
	if err != nil {
		t.Fatal(err)
	}
	b := f.builds()
	if len(b) != 1 || b[0].args[len(b[0].args)-1] != "./heldout/..." {
		t.Fatalf("build args %+v, want ./heldout/...", b)
	}
	r := f.runs()
	if len(r) != 1 || r[0].dir != filepath.Join(b[0].dir, "heldout") {
		t.Fatalf("run calls %+v, want one in <copy>/heldout", r)
	}
	if got := vtBinary(t, res[0], "heldout"); got.Run != OutcomePassed || got.Tests["TestR1_EmptyList"] != OutcomePassed {
		t.Errorf("heldout binary: %+v", got)
	}
	if vtExists(filepath.Join(project, "heldout")) {
		t.Error("Add's folder was created in the project")
	}
}

// ---- (c) overlays ----

func TestGraderVariantsOverlayReplacesInTheCopy(t *testing.T) {
	project := vtProject(t, nil)
	before := vtSnapshot(t, project)
	f := &vtFake{run: vtAllPass("TestA")}
	newApp := "package app\n\nfunc Answer() int { return 99 }\n"
	vs := []Variant{
		{Name: "plain"},
		{Name: "empty", Overlay: map[string]string{}},
		{Name: "planted", Overlay: map[string]string{"internal/app/app.go": newApp}},
		{Name: "two", Overlay: map[string]string{"internal/app/app.go": newApp + "// 2\n", "internal/app/other.go": "package app\n\nvar Other = 3\n"}},
	}
	if _, err := (VariantRun{Runner: f.Runner, Pkg: "tests"}).Variants(project, vs); err != nil {
		t.Fatal(err)
	}
	builds := f.builds()
	copyDir := builds[0].dir
	for _, i := range []int{0, 1} {
		if builds[i].overlayPath() != "" {
			t.Errorf("variant %q (nil or empty overlay) got an -overlay flag: %v", vs[i].Name, builds[i].args)
		}
	}
	for _, i := range []int{2, 3} {
		c := builds[i]
		p := c.overlayPath()
		if p == "" {
			t.Fatalf("variant %q has no -overlay flag", vs[i].Name)
		}
		if !vtInside(p, copyDir) {
			t.Errorf("the overlay JSON %q is not inside the copy %q", p, copyDir)
		}
		var o struct{ Replace map[string]string }
		if err := json.Unmarshal([]byte(c.overlay), &o); err != nil {
			t.Fatalf("variant %q overlay is not JSON: %v\n%s", vs[i].Name, err, c.overlay)
		}
		if len(o.Replace) != len(vs[i].Overlay) {
			t.Errorf("variant %q: overlay replaces %d files, want exactly %d: %v", vs[i].Name, len(o.Replace), len(vs[i].Overlay), o.Replace)
		}
		for rel, content := range vs[i].Overlay {
			key := filepath.Join(copyDir, filepath.FromSlash(rel))
			repl, ok := o.Replace[key]
			if !ok {
				t.Errorf("variant %q: overlay does not replace %s (the copy's file); has %v", vs[i].Name, key, o.Replace)
				continue
			}
			if !vtInside(repl, copyDir) {
				t.Errorf("variant %q: replacement file %q is outside the copy", vs[i].Name, repl)
			}
			// the replacement must exist (checked after the run: it need not survive) — here the
			// JSON's content check is what the fake could see; the file is read by the go command
			_ = content
		}
		for k, v := range o.Replace {
			if vtInside(k, project) || vtInside(v, project) {
				t.Errorf("variant %q: overlay names a path in the project: %s -> %s", vs[i].Name, k, v)
			}
		}
	}
	if after := vtSnapshot(t, project); !reflect.DeepEqual(before, after) {
		t.Error("the project changed")
	}
	if got := before["internal/app/app.go"]; got != vtApp {
		t.Errorf("the project's app.go is %q", got)
	}
}

// The replacement file's content is what the variant says: read it while the go command runs.
func TestGraderVariantsOverlayReplacementContent(t *testing.T) {
	project := vtProject(t, nil)
	newApp := "package app\n\nfunc Answer() int { return 99 }\n"
	var got, wantOriginalInCopy string
	f := &vtFake{run: vtAllPass("TestA")}
	spy := &vtSpy{inner: f, onBuild: func(c vtCall) {
		var o struct{ Replace map[string]string }
		if json.Unmarshal([]byte(c.overlay), &o) != nil {
			return
		}
		for k, v := range o.Replace {
			if strings.HasSuffix(k, "internal/app/app.go") {
				b, _ := os.ReadFile(v)
				got = string(b)
				o, _ := os.ReadFile(k)
				wantOriginalInCopy = string(o)
			}
		}
	}}
	if _, err := (VariantRun{Runner: spy.Runner, Pkg: "tests"}).Variants(project, []Variant{{Name: "m", Overlay: map[string]string{"internal/app/app.go": newApp}}}); err != nil {
		t.Fatal(err)
	}
	if got != newApp {
		t.Errorf("replacement file content %q, want %q", got, newApp)
	}
	if wantOriginalInCopy != vtApp {
		t.Errorf("the copy's own file (which the overlay shadows) is %q, want the project's text", wantOriginalInCopy)
	}
}

type vtSpy struct {
	inner   *vtFake
	onBuild func(vtCall)
}

func (s *vtSpy) Runner(copy, cache string) checks.GoRunner { return vtSpyRunner{s, copy, cache} }

type vtSpyRunner struct {
	s     *vtSpy
	copy  string
	cache string
}

func (r vtSpyRunner) Go(dir string, args ...string) (checks.GoResult, error) {
	c := vtCall{cache: r.cache, dir: dir, args: args}
	if p := c.overlayPath(); p != "" {
		b, _ := os.ReadFile(p)
		c.overlay = string(b)
		r.s.onBuild(c)
	}
	return vtRunner{r.s.inner, r.copy, r.cache}.Go(dir, args...)
}

// ---- (d) per-test results from the events ----

func TestGraderVariantsOutcomes(t *testing.T) {
	project := vtProject(t, map[string]string{"tests/more/b_test.go": "", "tests/more/x.txt": ""})
	_ = os.RemoveAll(filepath.Join(project, "tests", "more")) // one binary: tests
	expected := []string{"TestPass", "TestFail", "TestPanic", "TestMissing", "TestAlsoPass"}
	type tc struct {
		name  string
		res   checks.GoResult
		err   error
		tests map[string]Outcome
		run   Outcome
	}
	panicEvs := []vtEv{
		{"run", "TestPanic", ""},
		{"output", "TestPanic", "=== RUN   TestPanic\n"},
		{"output", "TestPanic", "panic: boom [recovered]\n"},
		{"output", "TestPanic", "--- FAIL: TestPanic (0.00s)\n"},
		{"fail", "TestPanic", ""},
	}
	failEvs := []vtEv{
		{"run", "TestFail", ""},
		{"output", "TestFail", "    x_test.go:9: wrong\n"},
		{"output", "TestFail", "--- FAIL: TestFail (0.00s)\n"},
		{"fail", "TestFail", ""},
	}
	cases := []tc{
		{name: "all pass", res: vtEvents(vtCat(vtTest("TestPass", "pass"), vtTest("TestFail", "pass"), vtTest("TestPanic", "pass"),
			vtTest("TestMissing", "pass"), vtTest("TestAlsoPass", "pass"), vtPackage("pass"))...),
			tests: map[string]Outcome{"TestPass": OutcomePassed, "TestFail": OutcomePassed, "TestPanic": OutcomePassed, "TestMissing": OutcomePassed, "TestAlsoPass": OutcomePassed},
			run:   OutcomePassed},
		{name: "one fails, one panics, one has no event", res: vtEvents(vtCat(vtTest("TestPass", "pass"), failEvs, panicEvs, vtTest("TestAlsoPass", "pass"), vtPackage("fail"))...),
			tests: map[string]Outcome{"TestPass": OutcomePassed, "TestFail": OutcomeFailed, "TestPanic": OutcomeFailed, "TestMissing": OutcomeDidNotRun, "TestAlsoPass": OutcomePassed},
			run:   OutcomeFailed},
		{name: "an expected name with no event, nothing failed", res: vtEvents(vtCat(vtTest("TestPass", "pass"), vtTest("TestFail", "pass"), vtTest("TestPanic", "pass"),
			vtTest("TestAlsoPass", "pass"), vtPackage("pass"))...),
			tests: map[string]Outcome{"TestPass": OutcomePassed, "TestFail": OutcomePassed, "TestPanic": OutcomePassed, "TestMissing": OutcomeDidNotRun, "TestAlsoPass": OutcomePassed},
			run:   OutcomeDidNotRun},
		{name: "package-level events only", res: vtEvents(vtPackage("pass")...),
			tests: map[string]Outcome{"TestPass": OutcomeDidNotRun, "TestFail": OutcomeDidNotRun, "TestPanic": OutcomeDidNotRun, "TestMissing": OutcomeDidNotRun, "TestAlsoPass": OutcomeDidNotRun},
			run:   OutcomeDidNotRun},
		{name: "no output at all", res: checks.GoResult{},
			tests: map[string]Outcome{"TestPass": OutcomeDidNotRun, "TestFail": OutcomeDidNotRun, "TestPanic": OutcomeDidNotRun, "TestMissing": OutcomeDidNotRun, "TestAlsoPass": OutcomeDidNotRun},
			run:   OutcomeDidNotRun},
		{name: "output that is not events", res: checks.GoResult{ExitCode: 1, Output: "fork/exec: permission denied\n"},
			tests: map[string]Outcome{"TestPass": OutcomeDidNotRun, "TestFail": OutcomeDidNotRun, "TestPanic": OutcomeDidNotRun, "TestMissing": OutcomeDidNotRun, "TestAlsoPass": OutcomeDidNotRun},
			run:   OutcomeDidNotRun},
		{name: "runner error", err: fmt.Errorf("docker could not run go: no such image"),
			tests: map[string]Outcome{"TestPass": OutcomeDidNotRun, "TestFail": OutcomeDidNotRun, "TestPanic": OutcomeDidNotRun, "TestMissing": OutcomeDidNotRun, "TestAlsoPass": OutcomeDidNotRun},
			run:   OutcomeDidNotRun},
		{name: "runner time-out", err: fmt.Errorf("timed out after 3m0s"), run: OutcomeTimedOut},
		{name: "a failure beats a missing name", res: vtEvents(vtCat(failEvs, vtPackage("fail"))...),
			tests: map[string]Outcome{"TestPass": OutcomeDidNotRun, "TestFail": OutcomeFailed, "TestPanic": OutcomeDidNotRun, "TestMissing": OutcomeDidNotRun, "TestAlsoPass": OutcomeDidNotRun},
			run:   OutcomeFailed},
		{name: "a test not in Expected is named too; subtests are not top-level",
			res: vtEvents(vtCat(vtTest("TestPass", "pass"), vtTest("TestFail", "pass"), vtTest("TestPanic", "pass"), vtTest("TestMissing", "pass"), vtTest("TestAlsoPass", "pass"),
				vtTest("TestExtra", "pass"), vtTest("TestExtra/sub", "pass"), vtPackage("pass"))...),
			tests: map[string]Outcome{"TestPass": OutcomePassed, "TestFail": OutcomePassed, "TestPanic": OutcomePassed, "TestMissing": OutcomePassed, "TestAlsoPass": OutcomePassed, "TestExtra": OutcomePassed},
			run:   OutcomePassed},
		{name: "a failing subtest fails its parent", res: vtEvents(vtCat(vtTest("TestPass", "pass"), vtTest("TestFail", "pass"), vtTest("TestPanic", "pass"), vtTest("TestMissing", "pass"), vtTest("TestAlsoPass", "pass"),
			vtTest("TestExtra/sub", "fail"), vtTest("TestExtra", "fail"), vtPackage("fail"))...),
			tests: map[string]Outcome{"TestPass": OutcomePassed, "TestFail": OutcomePassed, "TestPanic": OutcomePassed, "TestMissing": OutcomePassed, "TestAlsoPass": OutcomePassed, "TestExtra": OutcomeFailed},
			run:   OutcomeFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &vtFake{run: func(int, string) (checks.GoResult, error) { return c.res, c.err }}
			res, err := VariantRun{Runner: f.Runner, Pkg: "tests", Expected: expected}.Variants(project, []Variant{{Name: "v"}})
			if err != nil {
				t.Fatal(err)
			}
			b := vtBinary(t, res[0], "tests")
			if b.Run != c.run {
				t.Errorf("Run = %q, want %q (tests %v)", b.Run, c.run, b.Tests)
			}
			if c.tests != nil && !reflect.DeepEqual(b.Tests, c.tests) {
				t.Errorf("Tests:\n got %v\nwant %v", b.Tests, c.tests)
			}
			if c.err != nil && c.run == OutcomeTimedOut {
				for n, o := range b.Tests {
					if o == OutcomePassed || o == OutcomeFailed {
						t.Errorf("%s is %q after a time-out", n, o)
					}
				}
				for _, n := range expected {
					if _, ok := b.Tests[n]; !ok {
						t.Errorf("expected name %s is not in Tests after a time-out", n)
					}
				}
			}
			if res[0].BuildError != "" {
				t.Errorf("BuildError %q", res[0].BuildError)
			}
		})
	}
}

func TestGraderVariantsOutcomesWithNoExpected(t *testing.T) {
	project := vtProject(t, nil)
	run := func(res checks.GoResult) Binary {
		f := &vtFake{run: func(int, string) (checks.GoResult, error) { return res, nil }}
		r, err := VariantRun{Runner: f.Runner, Pkg: "tests"}.Variants(project, []Variant{{Name: "v"}})
		if err != nil {
			t.Fatal(err)
		}
		return vtBinary(t, r[0], "tests")
	}
	if b := run(vtEvents(vtCat(vtTest("TestX", "pass"), vtPackage("pass"))...)); b.Run != OutcomePassed || b.Tests["TestX"] != OutcomePassed {
		t.Errorf("one passing test, nothing expected: %+v", b)
	}
	if b := run(vtEvents(vtPackage("pass")...)); b.Run != OutcomeDidNotRun || len(b.Tests) != 0 {
		t.Errorf("no test reported: %+v (Run must not be passed, Tests empty)", b)
	}
	if b := run(vtEvents(vtCat(vtTest("TestX", "fail"), vtTest("TestY", "pass"), vtPackage("fail"))...)); b.Run != OutcomeFailed ||
		b.Tests["TestX"] != OutcomeFailed || b.Tests["TestY"] != OutcomePassed {
		t.Errorf("one failing test: %+v", b)
	}
}

// Each binary of a variant is judged on its own, and so is each variant.
func TestGraderVariantsOutcomesPerBinaryAndVariant(t *testing.T) {
	project := vtProject(t, nil)
	f := &vtFake{run: func(i int, base string) (checks.GoResult, error) {
		switch {
		case i == 1 && base == "more.test":
			return vtEvents(vtCat(vtTest("TestM", "fail"), vtPackage("fail"))...), nil
		case i == 2 && base == "tests.test":
			return checks.GoResult{}, fmt.Errorf("timed out after 1m0s")
		}
		return vtEvents(vtCat(vtTest("TestM", "pass"), vtPackage("pass"))...), nil
	}}
	res, err := VariantRun{Runner: f.Runner, Pkg: "tests"}.Variants(project, []Variant{{Name: "a"}, {Name: "b"}, {Name: "c"}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]Outcome{
		"a": {OutcomePassed, OutcomePassed},
		"b": {OutcomePassed, OutcomeFailed},
		"c": {OutcomeTimedOut, OutcomePassed},
	}
	for _, r := range res {
		if got := [2]Outcome{vtBinary(t, r, "tests").Run, vtBinary(t, r, "tests/more").Run}; got != want[r.Name] {
			t.Errorf("variant %s: runs %v, want %v", r.Name, got, want[r.Name])
		}
	}
}

// A built folder whose binary go did not write is did-not-run and is not run.
func TestGraderVariantsMissingBinary(t *testing.T) {
	project := vtProject(t, nil)
	f := &vtFake{
		build: func(int) (func(string) bool, checks.GoResult, error) {
			return func(base string) bool { return base != "more" }, checks.GoResult{}, nil
		},
		run: vtAllPass("TestA"),
	}
	res, err := VariantRun{Runner: f.Runner, Pkg: "tests", Expected: []string{"TestA"}}.Variants(project, []Variant{{Name: "v"}})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].BuildError != "" {
		t.Fatalf("BuildError %q", res[0].BuildError)
	}
	more := vtBinary(t, res[0], "tests/more")
	if more.Run != OutcomeDidNotRun || more.Tests["TestA"] != OutcomeDidNotRun {
		t.Errorf("binary that was not written: %+v", more)
	}
	if tb := vtBinary(t, res[0], "tests"); tb.Run != OutcomePassed {
		t.Errorf("the other binary: %+v", tb)
	}
	if n := len(f.runs()); n != 1 {
		t.Errorf("want 1 run call (nothing to run for the missing binary), got %d", n)
	}
}

// ---- (e) a build failure is that variant's only ----

func TestGraderVariantsBuildFailureIsOneVariants(t *testing.T) {
	project := vtProject(t, nil)
	for _, mode := range []string{"exit code", "runner error"} {
		t.Run(mode, func(t *testing.T) {
			f := &vtFake{
				build: func(i int) (func(string) bool, checks.GoResult, error) {
					all := func(string) bool { return true }
					if i == 1 {
						if mode == "exit code" {
							return all, checks.GoResult{ExitCode: 2, Output: "# example.com/svc/tests\ntests/a_test.go:3:1: syntax error: unexpected }\n"}, nil
						}
						return all, checks.GoResult{}, fmt.Errorf("docker could not run go: image missing")
					}
					return all, checks.GoResult{}, nil
				},
				run: vtAllPass("TestA"),
			}
			vs := []Variant{{Name: "a"}, {Name: "broken"}, {Name: "c"}}
			res, err := VariantRun{Runner: f.Runner, Pkg: "tests", Expected: []string{"TestA"}}.Variants(project, vs)
			if err != nil {
				t.Fatalf("a build failure is not a setup error: %v", err)
			}
			if len(res) != 3 {
				t.Fatalf("want 3 results, got %d", len(res))
			}
			if res[1].Name != "broken" || res[1].BuildError == "" || res[1].Binaries != nil {
				t.Errorf("broken variant: %+v", res[1])
			}
			want := "syntax error"
			if mode == "runner error" {
				want = "image missing"
			}
			if !strings.Contains(res[1].BuildError, want) {
				t.Errorf("BuildError %q does not carry the output/error %q", res[1].BuildError, want)
			}
			for _, i := range []int{0, 2} {
				if res[i].BuildError != "" || len(res[i].Binaries) != 2 {
					t.Errorf("variant %s: %+v, want built with 2 binaries", res[i].Name, res[i])
				}
				if vtBinary(t, res[i], "tests").Run != OutcomePassed {
					t.Errorf("variant %s did not pass: %+v", res[i].Name, res[i].Binaries)
				}
			}
			if n := len(f.builds()); n != 3 {
				t.Errorf("want all 3 variants built, got %d builds", n)
			}
			if n := len(f.runs()); n != 4 {
				t.Errorf("want 4 runs (2 binaries x the 2 built variants), got %d", n)
			}
			for _, c := range f.runs() {
				if c.variant == 1 {
					t.Errorf("a binary of the broken variant was run: %v", c.args)
				}
			}
		})
	}
}

func TestGraderVariantsEveryBuildFails(t *testing.T) {
	project := vtProject(t, nil)
	f := &vtFake{build: func(int) (func(string) bool, checks.GoResult, error) {
		return nil, checks.GoResult{ExitCode: 1, Output: "boom"}, nil
	}}
	res, err := VariantRun{Runner: f.Runner, Pkg: "tests"}.Variants(project, []Variant{{Name: "a"}, {Name: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.BuildError == "" || r.Binaries != nil {
			t.Errorf("%+v", r)
		}
	}
	if len(f.runs()) != 0 {
		t.Errorf("runs after only failed builds: %v", f.runs())
	}
}

func TestGraderVariantsNoVariants(t *testing.T) {
	project := vtProject(t, nil)
	f := &vtFake{run: vtAllPass("TestA")}
	res, err := VariantRun{Runner: f.Runner, Pkg: "tests"}.Variants(project, nil)
	if err != nil || len(res) != 0 || len(f.calls) != 0 {
		t.Errorf("no variants: res %v err %v calls %v", res, err, f.calls)
	}
}

func TestGraderVariantsSetupFailure(t *testing.T) {
	f := &vtFake{run: vtAllPass("TestA")}
	_, err := VariantRun{Runner: f.Runner, Pkg: "tests"}.Variants(filepath.Join(t.TempDir(), "missing"), []Variant{{Name: "v"}})
	if err == nil {
		t.Error("a project that cannot be copied must be an error")
	}
	if len(f.calls) != 0 {
		t.Errorf("calls after a setup failure: %v", f.calls)
	}
}

// ---- the host runner on a tiny module ----

func TestGraderVariantsHostRunner(t *testing.T) {
	project := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(project, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	lib := func(body string) string { return "package lib\n\nfunc Answer() int {\n\t" + body + "\n}\n" }
	write("go.mod", "module example.com/tiny\n\ngo 1.26\n")
	write("data.txt", "hello\n")
	write("lib/lib.go", lib("return 42"))
	write("tests/tiny_test.go", `package tests

import (
	"os"
	"testing"

	"example.com/tiny/lib"
)

func TestAnswer(t *testing.T) {
	if got := lib.Answer(); got != 42 {
		t.Fatalf("Answer() = %d, want 42", got)
	}
}

// the binary runs with the package folder as its working directory
func TestDataBesideThePackage(t *testing.T) {
	b, err := os.ReadFile("../data.txt")
	if err != nil || string(b) != "hello\n" {
		t.Fatalf("../data.txt: %q, %v", b, err)
	}
}
`)
	before := vtSnapshot(t, project)
	vs := []Variant{
		{Name: "as is"},
		{Name: "returns 41", Overlay: map[string]string{"lib/lib.go": lib("return 41")}},
		{Name: "still 42", Overlay: map[string]string{"lib/lib.go": lib("x := 40\n\treturn x + 2")}},
		{Name: "panics", Overlay: map[string]string{"lib/lib.go": lib("panic(\"boom\")")}},
		{Name: "does not build", Overlay: map[string]string{"lib/lib.go": "package lib\n\nfunc {\n"}},
	}
	start := time.Now()
	res, err := VariantRun{
		Runner:   func(string, string) checks.GoRunner { return checks.NewGoRunnerWithTimeout(3 * time.Minute) },
		Pkg:      "tests",
		Expected: []string{"TestAnswer", "TestDataBesideThePackage"},
	}.Variants(project, vs)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("5 variants built and run in %s", time.Since(start).Round(time.Millisecond))
	if len(res) != len(vs) {
		t.Fatalf("got %d results", len(res))
	}
	want := map[string]map[string]Outcome{
		"as is":      {"TestAnswer": OutcomePassed, "TestDataBesideThePackage": OutcomePassed},
		"returns 41": {"TestAnswer": OutcomeFailed, "TestDataBesideThePackage": OutcomePassed},
		"still 42":   {"TestAnswer": OutcomePassed, "TestDataBesideThePackage": OutcomePassed},
		"panics":     {"TestAnswer": OutcomeFailed},
	}
	wantRun := map[string]Outcome{"as is": OutcomePassed, "returns 41": OutcomeFailed, "still 42": OutcomePassed, "panics": OutcomeFailed}
	for _, r := range res {
		if r.Name == "does not build" {
			if r.BuildError == "" || r.Binaries != nil {
				t.Errorf("does not build: %+v", r)
			}
			continue
		}
		if r.BuildError != "" {
			t.Errorf("%s: BuildError %s", r.Name, r.BuildError)
			continue
		}
		if len(r.Binaries) != 1 || r.Binaries[0].Dir != "tests" {
			t.Errorf("%s: binaries %+v", r.Name, r.Binaries)
			continue
		}
		b := r.Binaries[0]
		if b.Run != wantRun[r.Name] {
			t.Errorf("%s: Run %q, want %q (%v)", r.Name, b.Run, wantRun[r.Name], b.Tests)
		}
		for n, o := range want[r.Name] {
			if b.Tests[n] != o {
				t.Errorf("%s: %s is %q, want %q", r.Name, n, b.Tests[n], o)
			}
		}
	}
	if after := vtSnapshot(t, project); !reflect.DeepEqual(before, after) {
		t.Error("the tiny project changed")
	}
}

// ---- DockerRunner argument lists ----

// vtMounts are the -v arguments.
func vtMounts(a []string) (m []string) {
	for i, x := range a {
		if x == "-v" && i+1 < len(a) {
			m = append(m, a[i+1])
		}
	}
	return
}

func vtHasPair(a []string, k, v string) bool {
	for i := 0; i+1 < len(a); i++ {
		if a[i] == k && a[i+1] == v {
			return true
		}
	}
	return false
}

func vtHas(a []string, x string) bool {
	for _, y := range a {
		if y == x {
			return true
		}
	}
	return false
}

// vtMountOf returns the mounts whose host folder is folder (cleaned).
func vtMountOf(a []string, folder string) (m []string) {
	for _, v := range vtMounts(a) {
		host, _, _ := strings.Cut(v, ":")
		if filepath.Clean(host) == filepath.Clean(folder) {
			m = append(m, v)
		}
	}
	return
}

func vtEnv(a []string, name string) (vals []string) {
	for i := 0; i+1 < len(a); i++ {
		if a[i] == "-e" {
			if v, ok := strings.CutPrefix(a[i+1], name+"="); ok {
				vals = append(vals, v)
			}
		}
	}
	return
}

func vtSafe(t *testing.T, a []string) {
	t.Helper()
	for _, p := range [][2]string{{"--network", "none"}, {"--cap-drop", "ALL"}, {"--security-opt", "no-new-privileges"}} {
		if !vtHasPair(a, p[0], p[1]) {
			t.Errorf("missing %s %s in %v", p[0], p[1], a)
		}
	}
	for _, x := range []string{"--rm", "--read-only"} {
		if !vtHas(a, x) {
			t.Errorf("missing %s in %v", x, a)
		}
	}
	if vtHas(a, "--privileged") {
		t.Errorf("--privileged in %v", a)
	}
	tmpfs := false
	for i := 0; i+1 < len(a); i++ {
		if a[i] == "--tmpfs" && strings.HasPrefix(a[i+1], "/tmp") {
			tmpfs = true
		}
	}
	if !tmpfs {
		t.Errorf("no --tmpfs /tmp in %v", a)
	}
	if !vtHasPair(a, "-e", "GOPROXY=off") {
		t.Errorf("GOPROXY=off missing in %v", a)
	}
}

func TestGraderDockerArgsBuildCall(t *testing.T) {
	d := DockerRunner{Image: "img:1", Root: "/p/copy", Cache: "/c/cache"}
	a := d.dockerArgs("apeval-x", "/p/copy", "test", "-c", "-vet=off", "-mod=vendor", "-o", "/p/bins/0/", "./tests/...")
	vtSafe(t, a)
	if !vtHasPair(a, "-w", "/p/copy") {
		t.Errorf("-w /p/copy missing: %v", a)
	}
	if !vtHasPair(a, "--name", "apeval-x") {
		t.Errorf("--name missing: %v", a)
	}
	if m := vtMountOf(a, "/p/copy"); !reflect.DeepEqual(m, []string{"/p/copy:/p/copy:ro"}) {
		t.Errorf("Root mounts %v, want exactly the read-only /p/copy:/p/copy:ro", m)
	}
	if m := vtMountOf(a, "/c/cache"); !reflect.DeepEqual(m, []string{"/c/cache:/c/cache"}) {
		t.Errorf("cache mounts %v, want exactly the writable /c/cache:/c/cache", m)
	}
	if got := vtEnv(a, "GOCACHE"); !reflect.DeepEqual(got, []string{"/c/cache"}) {
		t.Errorf("GOCACHE = %v, want [/c/cache] only", got)
	}
	m := vtMountOf(a, "/p/bins/0")
	if len(m) != 1 || strings.HasSuffix(m[0], ":ro") || !strings.HasPrefix(m[0], "/p/bins/0") {
		t.Errorf("-o folder mounts %v, want one writable mount", m)
	}
	for _, v := range vtMounts(a) {
		if !strings.HasSuffix(v, ":ro") && !strings.HasPrefix(v, "/c/cache:") && !strings.HasPrefix(v, "/p/bins/0") {
			t.Errorf("unexpected writable mount %q", v)
		}
	}
	// the go arguments come after the image, unchanged, behind the entrypoint go
	if !vtHasPair(a, "--entrypoint", "go") {
		t.Errorf("--entrypoint go missing: %v", a)
	}
	if tail := a[len(a)-8:]; !reflect.DeepEqual(tail, []string{"img:1", "test", "-c", "-vet=off", "-mod=vendor", "-o", "/p/bins/0/", "./tests/..."}) {
		t.Errorf("tail of the argument list is %v, want the image and the go arguments", tail)
	}
}

func TestGraderDockerArgsRunCall(t *testing.T) {
	d := DockerRunner{Image: "img:1", Root: "/p/copy", Label: "apeval=1"}
	bin := "/p/bins/0/tests.test"
	a := d.dockerArgs("apeval-y", "/p/copy/tests", "tool", "test2json", "-t", bin, "-test.v=test2json", "-test.count=1")
	vtSafe(t, a)
	if !vtHasPair(a, "-w", "/p/copy/tests") {
		t.Errorf("-w must be the package folder: %v", a)
	}
	if m := vtMountOf(a, "/p/copy"); !reflect.DeepEqual(m, []string{"/p/copy:/p/copy:ro"}) {
		t.Errorf("Root mounts %v", m)
	}
	if m := vtMountOf(a, "/p/bins/0"); !reflect.DeepEqual(m, []string{"/p/bins/0:/p/bins/0:ro"}) {
		t.Errorf("the binary's folder mounts %v, want read-only /p/bins/0:/p/bins/0:ro", m)
	}
	for _, v := range vtMounts(a) {
		if !strings.HasSuffix(v, ":ro") {
			t.Errorf("a binary run has a writable mount %q: %v", v, a)
		}
	}
	for _, v := range vtEnv(a, "GOCACHE") {
		if !strings.HasPrefix(v, "/tmp") {
			t.Errorf("GOCACHE=%s is not on the container's tmpfs", v)
		}
	}
	if !vtHasPair(a, "--label", "apeval=1") {
		t.Errorf("label missing: %v", a)
	}
	// the arguments reach go: image, then them, in order
	n := 6
	if !reflect.DeepEqual(a[len(a)-n:], []string{"tool", "test2json", "-t", bin, "-test.v=test2json", "-test.count=1"}) {
		t.Errorf("tail %v", a[len(a)-n:])
	}
	// the dir of the run is not mounted twice or alone: only Root's mount carries it
	if m := vtMountOf(a, "/p/copy/tests"); len(m) != 0 {
		t.Errorf("the package folder is mounted on its own: %v", m)
	}
}

func TestGraderDockerArgsWithoutRootMountsTheDir(t *testing.T) {
	d := DockerRunner{Image: "img:1"}
	a := d.dockerArgs("n", "/p/proj", "test", "./...")
	if m := vtMountOf(a, "/p/proj"); !reflect.DeepEqual(m, []string{"/p/proj:/p/proj:ro"}) {
		t.Errorf("with no Root the dir is mounted read-only: %v", m)
	}
	if !vtHasPair(a, "-w", "/p/proj") {
		t.Errorf("-w: %v", a)
	}
	if got := vtEnv(a, "GOCACHE"); len(got) != 1 || !strings.HasPrefix(got[0], "/tmp") {
		t.Errorf("no Cache: GOCACHE %v must stay on the tmpfs", got)
	}
	for _, v := range vtMounts(a) {
		if !strings.HasSuffix(v, ":ro") {
			t.Errorf("writable mount %q with no -o and no Cache", v)
		}
	}
}

func TestGraderDockerArgsCoverprofileStillWritable(t *testing.T) {
	d := DockerRunner{Image: "img:1"}
	a := d.dockerArgs("n", "/p/proj", "test", "-coverprofile=/p/cov/c.out", "./...")
	if m := vtMountOf(a, "/p/cov"); !reflect.DeepEqual(m, []string{"/p/cov:/p/cov"}) {
		t.Errorf("coverprofile folder mounts %v, want writable", m)
	}
}

func TestGraderDockerFor(t *testing.T) {
	d := DockerRunner{Image: "img:1", Root: "/old", Cache: "/oldcache", Label: "l", Timeout: time.Minute}
	got := vtDocker(t, d.For("/p/copy", "/c/cache"))
	want := DockerRunner{Image: "img:1", Root: "/p/copy", Label: "l", Timeout: time.Minute, Cache: "/c/cache"}
	if got != want {
		t.Errorf("For = %+v, want %+v", got, want)
	}
	if d.Root != "/old" || d.Cache != "/oldcache" {
		t.Errorf("the receiver changed: %+v", d)
	}
	if run := vtDocker(t, d.For("/p/copy", "")); run.Cache != "" || run.Root != "/p/copy" {
		t.Errorf("For(root, \"\") = %+v, want Root set and no Cache", run)
	}
	// the runner it returns builds the cache mounts
	a := got.dockerArgs("n", "/p/copy", "test", "-c", "-o", "/p/bins/0/", "./tests/...")
	if !vtHasPair(a, "-e", "GOCACHE=/c/cache") {
		t.Errorf("the For runner does not set GOCACHE: %v", a)
	}
}

func vtDocker(t *testing.T, r checks.GoRunner) DockerRunner {
	t.Helper()
	switch v := r.(type) {
	case DockerRunner:
		return v
	case *DockerRunner:
		return *v
	}
	t.Fatalf("For returned %T, want a DockerRunner", r)
	return DockerRunner{}
}

// One container per call, named differently, with the argument list dockerArgs gives; checked with
// a stand-in `docker` that records its arguments.
func TestGraderDockerGoRunsOneContainerPerCall(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(bin, 0o750); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(tmp, "docker.log")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done >> \"" + log + "\"\nprintf -- '--end--\\n' >> \"" + log + "\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil { //nolint:gosec // G306: the stand-in docker must be executable by its owner
		t.Fatal(err)
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	pkg := filepath.Join(root, "tests")
	if err := os.MkdirAll(pkg, 0o750); err != nil {
		t.Fatal(err)
	}
	d := DockerRunner{Image: "img:1", Root: root}
	runArgs := []string{"tool", "test2json", "-t", "/b/0/tests.test", "-test.v=test2json", "-test.count=1"}
	for i := 0; i < 2; i++ {
		if _, err := d.Go(pkg, runArgs...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.For(root, "/c/cache").Go(root, "test", "-c", "-o", "/b/0/", "./tests/..."); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	for _, chunk := range strings.Split(string(b), "--end--\n") {
		if strings.TrimSpace(chunk) != "" {
			calls = append(calls, strings.Split(strings.TrimRight(chunk, "\n"), "\n"))
		}
	}
	if len(calls) != 3 {
		t.Fatalf("want 3 docker calls, got %d: %v", len(calls), calls)
	}
	names := map[string]bool{}
	for i, c := range calls {
		name := ""
		for j := 0; j+1 < len(c); j++ {
			if c[j] == "--name" {
				name = c[j+1]
			}
		}
		if name == "" || names[name] {
			t.Errorf("call %d: container name %q is empty or reused", i, name)
		}
		names[name] = true
		args, dir := runArgs, pkg
		d2 := d
		if i == 2 {
			args, dir, d2 = []string{"test", "-c", "-o", "/b/0/", "./tests/..."}, root, vtDocker(t, d.For(root, "/c/cache"))
		}
		if want := d2.dockerArgs(name, dir, args...); !reflect.DeepEqual(c, want) {
			t.Errorf("call %d: docker got\n %v\nwant dockerArgs\n %v", i, c, want)
		}
	}
}
