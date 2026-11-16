package api

// The owner's decision of 2026-11-13 on ProjectExists: startRun refuses a project folder only when
// it holds a file (a regular file or a symbolic link, at any depth) or a `.git` that is not empty.
// A folder holding only empty folders, an empty `.git` included (what `ap build-workers` makes),
// is accepted. Shared helpers are workers_test.go's and runs_ops_test.go's; none is changed.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentic-platform/engine/store"
)

// skeleton makes the folders `ap build-workers` makes for notes-app: the project, an empty .git
// and each step's folders, all empty.
func skeleton(t *testing.T, projects string) string {
	t.Helper()
	p := filepath.Join(projects, "notes-app")
	for _, d := range []string{".git", "api", "features", "gen", "vendor", "tests", "internal", "cmd"} {
		if err := os.MkdirAll(filepath.Join(p, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// skeletonRig is an engine ready to start a run of S-1 (approved, every step registered) over the
// projects folder.
func skeletonRig(t *testing.T, projects string) *rig {
	t.Helper()
	st, err := store.Open(t.TempDir(), spSchemas(t))
	if err != nil {
		t.Fatal(err)
	}
	sp := newSP(t, []spResult{{p: spNotes(t)}}, st)
	sp.approveAfterSeed(t)
	ctx, cancel := context.WithCancel(context.Background())
	r := newRig(t, func(c *Config) {
		c.Context = ctx
		c.Slices, c.Store, c.Projects = sp.svc, st, projects
		c.WorkerGrants = stepGrants("S-1")
		c.StepWorker = &fakeStepWorker{st: st, data: map[string][]byte{}}
		c.StepChecker = &fakeStepChecker{}
	})
	stopEngineAtEnd(t, r, cancel)
	registerAll(t, r, "S-1")
	return r
}

func refusedAsProjectExists(t *testing.T, r *rig) {
	t.Helper()
	r.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil).problem(t, 409, "ProjectExists")
	if n := r.rec.count(); n != 0 {
		t.Errorf("Work ran %d times for a refused run", n)
	}
}

func TestProjectSkeletonOfEmptyFoldersIsAccepted(t *testing.T) {
	projects := t.TempDir()
	skeleton(t, projects)
	r := skeletonRig(t, projects)
	w := r.do("POST", spURL("S-1", "/runs"), clientToken, runBody, nil)
	var p struct{ Code string }
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != 202 || p.Code == "ProjectExists" {
		t.Fatalf("a folder of empty folders (an empty .git included): status %d, want 202: %s", w.Code, w.Body.String())
	}
}

func TestProjectSkeletonWithAFileAtAnyDepthIsRefused(t *testing.T) {
	for _, rel := range []string{"README.md", "internal/app/app.go", "gen/a/b/c/d.go", ".hidden"} {
		t.Run(rel, func(t *testing.T) {
			projects := t.TempDir()
			p := skeleton(t, projects)
			f := filepath.Join(p, rel)
			if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f, nil, 0o644); err != nil { // even an empty file
				t.Fatal(err)
			}
			refusedAsProjectExists(t, skeletonRig(t, projects))
		})
	}
}

func TestProjectSkeletonWithASymlinkIsRefused(t *testing.T) {
	for name, target := range map[string]string{
		"to a folder":     ".",
		"dangling":        "nowhere",
		"to outside":      "/etc",
		"to an empty sub": "api",
	} {
		t.Run(name, func(t *testing.T) {
			projects := t.TempDir()
			p := skeleton(t, projects)
			if err := os.Symlink(target, filepath.Join(p, "tests", "link")); err != nil {
				t.Fatal(err)
			}
			refusedAsProjectExists(t, skeletonRig(t, projects))
		})
	}
}

func TestProjectSkeletonWithANonEmptyGitIsRefused(t *testing.T) {
	for _, rel := range []string{"HEAD", "refs", "objects/ab"} {
		t.Run(rel, func(t *testing.T) {
			projects := t.TempDir()
			p := skeleton(t, projects)
			g := filepath.Join(p, ".git", rel)
			var err error
			if rel == "HEAD" {
				err = os.WriteFile(g, []byte("ref: refs/heads/main\n"), 0o644)
			} else {
				err = os.MkdirAll(g, 0o755) // an empty folder in .git: .git is not empty
			}
			if err != nil {
				t.Fatal(err)
			}
			refusedAsProjectExists(t, skeletonRig(t, projects))
		})
	}
}
