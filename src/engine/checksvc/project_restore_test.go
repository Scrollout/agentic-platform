package checksvc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T053c, restoreStep: written back, removed, no folder removed, a no-op on a clean folder, 409
// on a last commit that is not `before`, and the clean-up of entries the checks would not read.

func TestProjectWorkerRestoreWritesBackAndRemoves(t *testing.T) {
	w := newPW(t)
	v1, _ := w.firstStep(t)
	v2, _ := w.secondStep(t, v1.ProjectFiles)
	committed := contents(t, w.dir)
	os.MkdirAll(filepath.Join(w.dir, "tests", "empty"), 0o755)
	os.MkdirAll(filepath.Join(w.dir, "emptydir"), 0o755)

	// What a rejected attempt left.
	write(t, w.dir, "api/openapi.yaml", []byte("changed\n"))
	os.Remove(filepath.Join(w.dir, "tests", "a_test.go"))
	write(t, w.dir, "tests/extra_test.go", []byte("package tests\n"))
	write(t, w.dir, "newdir/sub/f.txt", []byte("new\n"))
	gitSnap := snapshot(t, filepath.Join(w.dir, ".git"))
	histSnap := snapshot(t, w.history)

	r := wantRestored(t, post(t, w.base, restorePath, restoreBody(testsStep(), v2.ProjectFiles)))
	want := []string{"api/openapi.yaml", "newdir/sub/f.txt", "tests/a_test.go", "tests/extra_test.go"}
	if strings.Join(r.Restored, ",") != strings.Join(want, ",") {
		t.Errorf("restored must list the paths written back or removed, in byte order: %v, want %v", r.Restored, want)
	}
	if !samePF(r.ProjectFiles, v2.ProjectFiles) || !samePF(projectFiles(t, w.dir), v2.ProjectFiles) {
		t.Errorf("the folder must hold exactly before: %v", projectFiles(t, w.dir))
	}
	for p, c := range committed {
		if string(read(t, w.dir, p)) != string(c) {
			t.Errorf("%s must hold its committed bytes", p)
		}
	}
	for _, d := range []string{"newdir", "newdir/sub", "tests/empty", "emptydir", "tests"} {
		if fi, err := os.Lstat(filepath.Join(w.dir, d)); err != nil || !fi.IsDir() {
			t.Errorf("no folder is removed: %s is gone", d)
		}
	}
	if d := sameSnapshot(gitSnap, snapshot(t, filepath.Join(w.dir, ".git"))); d != "" {
		t.Errorf("restore does not change the version history: %s", d)
	}
	if d := sameSnapshot(histSnap, snapshot(t, w.history)); d != "" {
		t.Errorf("restore does not change the record: %s", d)
	}

	// A repeat, and a clean folder: a no-op.
	snap := w.state(t)
	r = wantRestored(t, post(t, w.base, restorePath, restoreBody(testsStep(), v2.ProjectFiles)))
	if len(r.Restored) != 0 || !samePF(r.ProjectFiles, v2.ProjectFiles) {
		t.Errorf("restore on a clean folder restores nothing: %+v", r)
	}
	w.unchanged(t, snap, "restore on a clean folder")
}

func TestProjectWorkerRestoreRefusesAnotherBefore(t *testing.T) {
	w := newPW(t)
	v1, _ := w.firstStep(t)
	v2, _ := w.secondStep(t, v1.ProjectFiles)
	write(t, w.dir, "tests/a_test.go", []byte("changed\n"))
	snap := w.state(t)
	for name, before := range map[string][]pf{
		"an earlier commit's files": v1.ProjectFiles,
		"another hash":              {v2.ProjectFiles[0], v2.ProjectFiles[1], {v2.ProjectFiles[2].Path, sum([]byte("x"))}},
		"one file more":             append(append([]pf{}, v2.ProjectFiles...), pf{"tests/z_test.go", sum([]byte("x"))}),
		"empty":                     {},
	} {
		t.Run(name, func(t *testing.T) {
			wantProblem(t, post(t, w.base, restorePath, restoreBody(testsStep(), before)), 409, "ProjectStateDiffers")
			w.unchanged(t, snap, "a restore whose before the last commit does not hold")
		})
	}
}

func TestProjectWorkerRestoreWithNoRecord(t *testing.T) {
	t.Run("before empty: needs no repository", func(t *testing.T) {
		w := newPW(t)
		write(t, w.dir, "api/openapi.yaml", []byte("an interrupted attempt's file\n"))
		r := wantRestored(t, post(t, w.base, restorePath, withAttempt(restoreBody(ccStep(), nil), 2)))
		if strings.Join(r.Restored, ",") != "api/openapi.yaml" || len(r.ProjectFiles) != 0 || len(projectFiles(t, w.dir)) != 0 {
			t.Errorf("with before empty every file outside .git is removed: %+v", r)
		}
		if es, _ := os.ReadDir(filepath.Join(w.dir, ".git")); len(es) != 0 {
			t.Error("restoreStep never creates the repository")
		}
		if fi, err := os.Lstat(filepath.Join(w.dir, "api")); err != nil || !fi.IsDir() {
			t.Error("no folder is removed")
		}
	})
	t.Run("before not empty", func(t *testing.T) {
		w := newPW(t)
		write(t, w.dir, "api/openapi.yaml", []byte("x\n"))
		snap := w.state(t)
		want409(t, post(t, w.base, restorePath, withAttempt(restoreBody(ccStep(), []pf{{"api/openapi.yaml", sum([]byte("x\n"))}}), 2)), "ProjectStateDiffers", "HistoryChanged")
		w.unchanged(t, snap, "a restore with no record and a non-empty before")
	})
}

// Below each `dir/**` folder: every symbolic link (never followed), every file whose path is
// not a ProjectPath and every folder whose name is not a ProjectPath part, with all it holds.
func TestProjectWorkerRestoreCleansTheStepsFolders(t *testing.T) {
	w := newPW(t)
	v1, _ := w.firstStep(t)
	v2, _ := w.secondStep(t, v1.ProjectFiles)
	outside := t.TempDir()
	write(t, outside, "target.txt", []byte("outside\n"))
	write(t, outside, "dir/f.txt", []byte("outside\n"))
	outSnap := snapshot(t, outside)

	link := func(target, rel string) {
		p := filepath.Join(w.dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
	}
	link(filepath.Join(outside, "target.txt"), "tests/link_test.go")
	link(filepath.Join(outside, "dir"), "tests/sub/dirlink")
	link("no-such-file", "tests/dangling")
	link("a_test.go", "tests/inside_link")
	write(t, w.dir, "tests/bad name_test.go", []byte("x\n"))
	write(t, w.dir, "tests/sub/bad#name", []byte("x\n"))
	write(t, w.dir, "tests/.GIT/config", []byte("x\n"))
	write(t, w.dir, "tests/sub/.git/HEAD", []byte("x\n"))
	write(t, w.dir, "tests/weird dir/deep/f.go", []byte("x\n"))
	os.MkdirAll(filepath.Join(w.dir, "tests", "kept", "empty"), 0o755)

	wantRestored(t, post(t, w.base, restorePath, restoreBody(testsStep(), v2.ProjectFiles)))
	for _, gone := range []string{"tests/link_test.go", "tests/sub/dirlink", "tests/dangling", "tests/inside_link", "tests/bad name_test.go",
		"tests/sub/bad#name", "tests/.GIT", "tests/sub/.git", "tests/weird dir"} {
		if _, err := os.Lstat(filepath.Join(w.dir, filepath.FromSlash(gone))); err == nil {
			t.Errorf("%s must be removed", gone)
		}
	}
	if d := sameSnapshot(outSnap, snapshot(t, outside)); d != "" {
		t.Errorf("no link is followed: outside the project %s", d)
	}
	for _, d := range []string{"tests/sub", "tests/kept/empty"} {
		if fi, err := os.Lstat(filepath.Join(w.dir, d)); err != nil || !fi.IsDir() {
			t.Errorf("a folder with a ProjectPath name is never removed: %s", d)
		}
	}
	if !samePF(projectFiles(t, w.dir), v2.ProjectFiles) {
		t.Errorf("the folder must hold before: %v", projectFiles(t, w.dir))
	}
}

// In the folder of an exact entry only such an entry its exact entries match by name is
// removed; such an entry anywhere else is 422 UnreadableProject, and nothing is changed.
func TestProjectWorkerRestoreExactEntriesAndElsewhere(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "go.mod", []byte("module outside\n"))
	target := filepath.Join(outside, "go.mod")

	t.Run("a link at go.mod for generate is removed", func(t *testing.T) {
		w := newPW(t)
		v1, _ := w.firstStep(t)
		if err := os.Symlink(target, filepath.Join(w.dir, "go.mod")); err != nil {
			t.Fatal(err)
		}
		wantRestored(t, post(t, w.base, restorePath, restoreBody(genStep(), v1.ProjectFiles)))
		if _, err := os.Lstat(filepath.Join(w.dir, "go.mod")); err == nil {
			t.Error("the link at go.mod must be removed")
		}
		if string(read(t, outside, "go.mod")) != "module outside\n" {
			t.Error("the link's target must not be touched")
		}
	})
	cases := map[string]struct {
		st    map[string]any
		setup func(t *testing.T, dir string)
	}{
		"a link in the root that generate's entries do not name": {genStep(), func(t *testing.T, dir string) {
			os.Symlink(target, filepath.Join(dir, "README.md"))
		}},
		"a link at go.mod for a step that does not write it": {testsStep(), func(t *testing.T, dir string) {
			os.Symlink(target, filepath.Join(dir, "go.mod"))
		}},
		"a link beside contract-check's exact entry, not named by it": {ccStep(), func(t *testing.T, dir string) {
			os.Symlink(target, filepath.Join(dir, "api", "other.yaml"))
		}},
		"a link outside the step's folders": {testsStep(), func(t *testing.T, dir string) {
			os.Symlink(target, filepath.Join(dir, "api", "link"))
		}},
		"a file whose path is not a ProjectPath outside the step's folders": {testsStep(), func(t *testing.T, dir string) {
			write(t, dir, "api/bad name.yaml", []byte("x\n"))
		}},
		"a .GIT folder in the root": {testsStep(), func(t *testing.T, dir string) {
			write(t, dir, ".GIT/config", []byte("x\n"))
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newPW(t)
			v1, _ := w.firstStep(t)
			write(t, w.dir, "api/openapi.yaml", []byte("changed by the attempt\n"))
			c.setup(t, w.dir)
			snap := w.state(t)
			wantProblem(t, post(t, w.base, restorePath, restoreBody(c.st, v1.ProjectFiles)), 422, "UnreadableProject")
			w.unchanged(t, snap, "a 422 restore")
		})
	}
}
