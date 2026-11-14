// T054j (T054f unit 2): step workers per project, tests written before the code by a separate
// author. This file runs without Docker; stepworkers_docker_test.go runs behind the `docker` tag.
//
// SEAMS these tests assume (new code in lab; the T054i seams are in platform_settings_test.go):
//
//   - In a file WITHOUT a build tag:
//
//     type Mount struct {
//     Source   string // the host path
//     Target   string // the path inside the container
//     ReadOnly bool
//     }
//
//     func StepMounts(root, projectsDir, project string, writableRoot ...string) (map[string][]Mount, error)
//     the mounts of each step of root's src/catalogue/api-feature-go/pattern.yaml, by step name,
//     as `ap build-workers` makes them: the project folder projectsDir/project at
//     /projects/<project>, read-only, each folder the step's `writable` names writable on top (the
//     whole root writable instead when an entry is a file at the root, as generate's go.mod), and
//     `.git` read-only on top of all. A step named in writableRoot (test-only, research D34:
//     `lab.WritableRoot("code")` for the planted US2.2 and US2.3) gets its root writable and
//     `.git` read-only; no other step changes.
//
//     func SkeletonFolders(root string) ([]string, error)
//     every folder, relative to the project and sorted, that `ap build-workers` makes before the
//     step containers start: `.git`, each step's writable folders and their parents.
//
//   - In the docker-tagged lab: `func WritableRoot(step string) Option` (a Start option);
//     `func (l *Lab) StepWorkers(t testing.TB, slice, project string)` (makes the skeleton in the
//     lab's projects folder, starts the five step containers from aptest-agent:lab and
//     aptest-check:lab with ServiceSettings and StepMounts, writes their grants, registers them and
//     waits for their health); `func (l *Lab) StopStepWorkers(t testing.TB, slice, project string)`;
//     and RestartEngine registering every running step container again, as `ap resume` does.
//
//   - Names and labels: a step container is named as T053g names it, with the lab's prefix
//     `aptest-lab-<pid>` as the Compose project: `aptest-lab-<pid>-worker-<slice, lower case>-<step>-<p>`,
//     <p> the first 12 hex digits of the SHA-256 of the project name; labelled
//     `org.agentic-platform.slice`, `.step` and `.project` with the full values, and
//     `org.agentic-platform.lab=<pid>`.
package lab

import (
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type patternStep struct {
	Name     string   `yaml:"name"`
	Worker   string   `yaml:"worker"`
	Writable []string `yaml:"writable"`
}

func readPattern(t testing.TB, root string) []patternStep {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "src", "catalogue", "api-feature-go", "pattern.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Steps []patternStep `yaml:"steps"`
	}
	if err := yaml.Unmarshal(b, &p); err != nil || len(p.Steps) == 0 {
		t.Fatalf("pattern.yaml: %v (%d steps)", err, len(p.Steps))
	}
	return p.Steps
}

// stepFolders is `ap build-workers`' step_folders: `dir/**` is dir, `dir/file` is dir, a file at
// the root is ".".
func stepFolders(writable []string) []string {
	set := map[string]bool{}
	for _, e := range writable {
		switch {
		case strings.HasSuffix(e, "/**"):
			set[strings.TrimSuffix(e, "/**")] = true
		case strings.Contains(e, "/"):
			set[path.Dir(e)] = true
		default:
			set["."] = true
		}
	}
	var out []string
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// wantMounts is what `ap build-workers` mounts for a step (src/deploy/ap, build_workers).
func wantMounts(projects, project string, folders []string, rootWritable bool) []Mount {
	pdir, inside := filepath.Join(projects, project), "/projects/"+project
	var m []Mount
	for _, f := range folders {
		if f == "." {
			rootWritable = true
		}
	}
	if rootWritable {
		m = append(m, Mount{pdir, inside, false})
	} else {
		m = append(m, Mount{pdir, inside, true})
		for _, f := range folders {
			m = append(m, Mount{filepath.Join(pdir, f), inside + "/" + f, false})
		}
	}
	return append(m, Mount{filepath.Join(pdir, ".git"), inside + "/.git", true})
}

func byTarget(m []Mount) []Mount {
	c := append([]Mount(nil), m...)
	sort.Slice(c, func(i, j int) bool { return c[i].Target < c[j].Target })
	return c
}

// TestStepMountsAreBuildWorkers: each step's mounts are the ones `ap build-workers` makes: the
// project read-only, each writable folder writable, the root for generate, `.git` read-only on
// top, and the review step (writable: [] today) wholly read-only.
func TestStepMountsAreBuildWorkers(t *testing.T) {
	root := repoRoot(t)
	projects := "/host/projects"
	got, err := StepMounts(root, projects, "notes")
	if err != nil {
		t.Fatalf("StepMounts: %v", err)
	}
	steps := readPattern(t, root)
	if len(got) != len(steps) {
		t.Errorf("StepMounts has %d steps, the pattern %d", len(got), len(steps))
	}
	for _, s := range steps {
		want := wantMounts(projects, "notes", stepFolders(s.Writable), false)
		if g := byTarget(got[s.Name]); !reflect.DeepEqual(g, byTarget(want)) {
			t.Errorf("%s: mounts\n %+v\nwant\n %+v", s.Name, g, byTarget(want))
		}
	}
	// The pattern's own shape, so the comparison above means what it says.
	for _, s := range steps {
		f := stepFolders(s.Writable)
		switch s.Name {
		case "generate":
			if !reflect.DeepEqual(f, []string{".", "gen", "internal/app", "vendor"}) {
				t.Errorf("generate's writable folders are %v; this test expects the root among them", f)
			}
		case "review":
			if len(f) != 0 {
				t.Errorf("review's writable folders are %v; this test expects none (T076 adds review/)", f)
			}
		}
	}
}

// TestWritableRootWidensOnlyThatStep: WritableRoot("code") mounts the code step's root writable,
// `.git` still read-only, and changes no other step.
func TestWritableRootWidensOnlyThatStep(t *testing.T) {
	root := repoRoot(t)
	plain, err := StepMounts(root, "/p", "notes")
	if err != nil {
		t.Fatal(err)
	}
	wide, err := StepMounts(root, "/p", "notes", "code")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range readPattern(t, root) {
		if s.Name == "code" {
			want := wantMounts("/p", "notes", nil, true)
			if g := byTarget(wide["code"]); !reflect.DeepEqual(g, byTarget(want)) {
				t.Errorf("code with WritableRoot: %+v, want %+v", g, byTarget(want))
			}
			if reflect.DeepEqual(byTarget(plain["code"]), byTarget(wide["code"])) {
				t.Error("WritableRoot(\"code\") changed nothing")
			}
			continue
		}
		if !reflect.DeepEqual(byTarget(plain[s.Name]), byTarget(wide[s.Name])) {
			t.Errorf("WritableRoot(\"code\") changed the %s step's mounts: %+v, was %+v", s.Name, wide[s.Name], plain[s.Name])
		}
	}
}

// TestSkeletonIsBuildWorkers: the folders the lab makes before the step containers start are
// `.git` and every step's writable folder (mkdir -p, so with their parents).
func TestSkeletonIsBuildWorkers(t *testing.T) {
	root := repoRoot(t)
	set := map[string]bool{".git": true}
	for _, s := range readPattern(t, root) {
		for _, f := range stepFolders(s.Writable) {
			for ; f != "." && f != "/" && f != ""; f = path.Dir(f) {
				set[f] = true
			}
		}
	}
	var want []string
	for f := range set {
		want = append(want, f)
	}
	sort.Strings(want)
	got, err := SkeletonFolders(root)
	if err != nil {
		t.Fatalf("SkeletonFolders: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SkeletonFolders = %v, want %v", got, want)
	}
}
