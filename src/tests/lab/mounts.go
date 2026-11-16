package lab

// The step containers' mounts and the project skeleton, as `ap build-workers` makes them (T053d,
// research D30, D34), proved equal by stepworkers_test.go, which runs without Docker.

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Mount is one bind mount of a step container.
type Mount struct {
	Source   string // the host path
	Target   string // the path inside the container
	ReadOnly bool
}

// step is one step of the pattern, as far as the lab reads it.
type step struct {
	Name     string   `yaml:"name"`
	Worker   string   `yaml:"worker"`
	Writable []string `yaml:"writable"`
}

// patternSteps reads the steps of root's src/catalogue/api-feature-go/pattern.yaml.
func patternSteps(root string) ([]step, error) {
	b, err := os.ReadFile(filepath.Join(root, "src", "catalogue", "api-feature-go", "pattern.yaml"))
	if err != nil {
		return nil, err
	}
	var p struct {
		Steps []step `yaml:"steps"`
	}
	if err := yaml.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("pattern.yaml: %w", err)
	}
	if len(p.Steps) == 0 {
		return nil, fmt.Errorf("pattern.yaml has no steps")
	}
	return p.Steps, nil
}

// writableFolders is `ap build-workers`' step_folders: the folder of each `dir/**`, the folder an
// exact path is in, or "." (the project root) for an exact path at the root; sorted, once each.
func writableFolders(writable []string) []string {
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
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// StepMounts is the mounts of each step of the pattern, by step name, as `ap build-workers` makes
// them: projectsDir/project at /projects/<project> read-only, each writable folder writable (the
// whole root instead when one is the root, with review/ read-only on top), and `.git` read-only on
// top. Only the review step, whose writable folder it is, writes review/ (owner, 2026-11-14). A
// step named in writableRoot (test-only, research D34) gets its root writable, `.git` and
// review/ still read-only.
func StepMounts(root, projectsDir, project string, writableRoot ...string) (map[string][]Mount, error) {
	steps, err := patternSteps(root)
	if err != nil {
		return nil, err
	}
	pdir, inside := filepath.Join(projectsDir, project), "/projects/"+project
	out := map[string][]Mount{}
	for _, s := range steps {
		folders := writableFolders(s.Writable)
		var m []Mount
		if slices.Contains(writableRoot, s.Name) || slices.Contains(folders, ".") {
			m = append(m, Mount{pdir, inside, false}, Mount{filepath.Join(pdir, "review"), inside + "/review", true})
		} else {
			m = append(m, Mount{pdir, inside, true})
			for _, f := range folders {
				m = append(m, Mount{filepath.Join(pdir, f), inside + "/" + f, false})
			}
		}
		out[s.Name] = append(m, Mount{filepath.Join(pdir, ".git"), inside + "/.git", true})
	}
	return out, nil
}

// SkeletonFolders is every folder, relative to the project and sorted, that `ap build-workers`
// makes before the step containers start: `.git`, `review` (which a writable root mounts
// read-only), each step's writable folders and their parents.
func SkeletonFolders(root string) ([]string, error) {
	steps, err := patternSteps(root)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{".git": true, "review": true}
	for _, s := range steps {
		for _, f := range writableFolders(s.Writable) {
			for ; f != "." && f != "/" && f != ""; f = path.Dir(f) {
				set[f] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out, nil
}
