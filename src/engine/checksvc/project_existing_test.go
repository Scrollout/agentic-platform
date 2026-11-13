package checksvc_test

// T053e (owner, 2026-11-14; data-model.md, "A new project"; analyze finding A1): failing tests,
// by a separate author, for the project worker's FR-025 guard. With no record for a project, the
// run's first touch of it, restoreStep for attempt 1 or a commitStep with `reused_from` (a
// replay's first write), needs the folder to hold no file or symbolic link at any depth and an
// empty `.git`; otherwise it answers 409 ProjectExists and changes nothing. Every other call with
// no record keeps the earlier rules (the first commit, a restore for attempt 2 or later, and the
// recovery of an unfinished first call, pinned in project_guard_test.go and
// project_restore_test.go). Seam: as in project_helpers_test.go.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-platform/engine/checksvc"
)

// replayFirstCommit is a replay's first commitStep: contract-check reused from RUN-1 into a
// project with no record and no commit (before empty).
func replayFirstCommit() map[string]any {
	contract := map[string][]byte{"api/openapi.yaml": []byte("openapi: 3.1.0\n")}
	examples := map[string][]byte{"features/slice.feature": []byte("Feature: F\n")}
	fc, fe := framed(contract), framed(examples)
	return map[string]any{
		"run": "RUN-2", "step": ccStep(), "reused_from": "RUN-1", "project": pwProject,
		"before": []pf{}, "accepted_at": "2026-11-14T10:00:00Z", "time_limit_seconds": 120,
		"project_files": []pf{
			{"api/openapi.yaml", sum(contract["api/openapi.yaml"])},
			{"features/slice.feature", sum(examples["features/slice.feature"])},
		},
		"outputs": []any{
			map[string]any{"name": "contract", "sha256": sum(fc), "content": b64(fc)},
			map[string]any{"name": "examples", "sha256": sum(fe), "content": b64(fe)},
		},
	}
}

// withAttempt sets a request body's attempt (a restore after a rejected attempt 1).
func withAttempt(body map[string]any, n int) map[string]any {
	body["attempt"] = n
	return body
}

// existing are the folders a project with no record must not be taken over from.
func existing() map[string]func(t *testing.T, w *pw) {
	return map[string]func(t *testing.T, w *pw){
		"a file deep down": func(t *testing.T, w *pw) {
			write(t, w.dir, "src/a/b/c/d/deep.txt", []byte("the developer's\n"))
		},
		"a symbolic link": func(t *testing.T, w *pw) {
			if err := os.MkdirAll(filepath.Join(w.dir, "notes"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("../../elsewhere", filepath.Join(w.dir, "notes", "link")); err != nil {
				t.Fatal(err)
			}
		},
		"a developer's repository with a commit": func(t *testing.T, w *pw) {
			write(t, w.dir, "README.md", []byte("# mine\n"))
			mustGit(t, w.dir, "init", "-q", "--initial-branch=main", w.dir)
			mustGit(t, w.dir, "add", "-A")
			mustGit(t, w.dir, "commit", "-q", "-m", "my work")
		},
		"a non-empty .git alone": func(t *testing.T, w *pw) {
			write(t, w.dir, ".git/HEAD", []byte("ref: refs/heads/main\n"))
		},
	}
}

func TestProjectWorkerFirstTouchOfAnExistingProjectIsRefused(t *testing.T) {
	for name, plant := range existing() {
		t.Run(name+"/restoreStep attempt 1", func(t *testing.T) {
			w := newPW(t)
			plant(t, w)
			snap := w.state(t)
			want409(t, post(t, w.base, restorePath, restoreBody(ccStep(), nil)), "ProjectExists")
			w.unchanged(t, snap, "restoreStep for attempt 1 of a project that exists")
		})
		t.Run(name+"/commitStep reused_from", func(t *testing.T) {
			w := newPW(t)
			plant(t, w)
			snap := w.state(t)
			want409(t, post(t, w.base, commitPath, replayFirstCommit()), "ProjectExists")
			w.unchanged(t, snap, "a replay's first commitStep into a project that exists")
		})
	}
}

func TestProjectWorkerEmptySkeletonIsANewProject(t *testing.T) {
	skeleton := func(t *testing.T) *pw {
		w := newPW(t)
		for _, d := range []string{"api", "features", "gen", "tests/sub", "internal"} {
			if err := os.MkdirAll(filepath.Join(w.dir, d), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return w
	}
	t.Run("restoreStep attempt 1", func(t *testing.T) {
		w := skeleton(t)
		project, record := snapshot(t, w.dir), snapshot(t, w.history)
		r := wantRestored(t, post(t, w.base, restorePath, restoreBody(ccStep(), nil)))
		if len(r.Restored) != 0 || len(r.ProjectFiles) != 0 {
			t.Errorf("an empty skeleton needs nothing restored: %+v", r)
		}
		if d := sameSnapshot(project, snapshot(t, w.dir)); d != "" {
			t.Errorf("restoreStep on an empty skeleton must leave the project folder as it was: %s", d)
		}
		// the only change in the record folder: the project's claimed record is added
		after, added := snapshot(t, w.history), 0
		for k, v := range record {
			if after[k] != v {
				t.Errorf("record folder: %s changed or was removed", k)
			}
		}
		for k, v := range after {
			if _, ok := record[k]; !ok && strings.HasPrefix(v, "file:") {
				added++
			}
		}
		if added == 0 {
			t.Error("a passing first touch must add the project's claimed record")
		}
	})
	t.Run("commitStep reused_from", func(t *testing.T) {
		w := skeleton(t)
		b := replayFirstCommit()
		v := wantVersion(t, post(t, w.base, commitPath, b))
		if !samePF(v.ProjectFiles, toPF(b["project_files"])) || commitCount(t, w.dir) != 1 {
			t.Errorf("the replay's first commit: %+v", v)
		}
		wantTree(t, w.dir, v.Commit, contents(t, w.dir))
	})
}

func TestProjectWorkerRestoreForAttemptTwoWithNoRecordStillRestores(t *testing.T) {
	w := newPW(t)
	write(t, w.dir, "api/openapi.yaml", []byte("what rejected attempt 1 wrote\n"))
	body := restoreBody(ccStep(), nil)
	body["attempt"] = 2
	r := wantRestored(t, post(t, w.base, restorePath, body))
	if len(r.Restored) != 1 || r.Restored[0] != "api/openapi.yaml" || len(projectFiles(t, w.dir)) != 0 {
		t.Errorf("attempt 2 with no record restores to before (empty): %+v", r)
	}
	if es, _ := os.ReadDir(filepath.Join(w.dir, ".git")); len(es) != 0 {
		t.Error("restoreStep never creates the repository")
	}
}

// ---- the claimed record (contract, A new project; data-model, "A new project") ----

// freshRecord is a project worker over the same projects folder with an empty record folder: the
// claim the first touch wrote is gone, so the same call is a first touch again.
func (w *pw) freshRecord(t *testing.T) string {
	t.Helper()
	return serve(t, checksvc.Config{Token: token, Project: w.root, History: t.TempDir()})
}

func TestProjectWorkerPassingFirstTouchClaimsTheProject(t *testing.T) {
	w := newPW(t)
	wantRestored(t, post(t, w.base, restorePath, restoreBody(ccStep(), nil)))
	// attempt 1 writes its files; a retry of its restore is not a first touch
	write(t, w.dir, "api/openapi.yaml", []byte("attempt 1 wrote this\n"))
	snap := w.state(t)
	want409(t, post(t, w.freshRecord(t), restorePath, restoreBody(ccStep(), nil)), "ProjectExists")
	w.unchanged(t, snap, "a first touch over a worker without the claim")
	r := wantRestored(t, post(t, w.base, restorePath, restoreBody(ccStep(), nil)))
	if strings.Join(r.Restored, ",") != "api/openapi.yaml" || len(projectFiles(t, w.dir)) != 0 {
		t.Errorf("the claimed project's retry restores to before (empty): %+v", r)
	}
}

func TestProjectWorkerResumeRepeatingAttemptOneIsNotRefused(t *testing.T) {
	w := newPW(t)
	wantRestored(t, post(t, w.base, restorePath, restoreBody(ccStep(), nil)))
	write(t, w.dir, "api/openapi.yaml", []byte("openapi: 3.1.0\n"))
	write(t, w.dir, "features/slice.feature", []byte("Feature: half done\n"))
	base := w.restart(t, "") // the platform stopped and came back: same record folder
	r := wantRestored(t, post(t, base, restorePath, restoreBody(ccStep(), nil)))
	if len(r.Restored) != 2 || len(projectFiles(t, w.dir)) != 0 {
		t.Errorf("a resume repeating attempt 1 restores to before (empty): %+v", r)
	}
	// and the run's first commit follows as usual
	write(t, w.dir, "api/openapi.yaml", []byte("openapi: 3.1.0\n"))
	v := wantVersion(t, post(t, base, commitPath, commitBody(ccStep(), nil, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-14T10:00:01Z")))
	if commitCount(t, w.dir) != 1 || mainAt(t, w.dir) != v.Commit {
		t.Error("one commit on main")
	}
}

// stopAfter is a git binary that runs git and then exits 1 for the subcommand sub: the call
// stops right after that command did its work.
func stopAfter(t *testing.T, sub string) string {
	t.Helper()
	real := gitBin(t)
	bin := filepath.Join(t.TempDir(), "git")
	s := "#!/bin/sh\nfor a in \"$@\"; do case \"$a\" in " + sub + ") \"" + real + "\" \"$@\"; exit 1;; esac; done\n" +
		"exec \"" + real + "\" \"$@\"\n"
	if err := os.WriteFile(bin, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestProjectWorkerRecoversACrashedReplayFirstCommit(t *testing.T) {
	windows := map[string]func(t *testing.T) string{
		"before git init": func(t *testing.T) string { bin, _ := gitWrapper(t, "init", "fail"); return bin },
		"between git init and the provisional record": func(t *testing.T) string { return stopAfter(t, "init") },
		"after the reused files are written":          func(t *testing.T) string { bin, _ := gitWrapper(t, "commit-tree", "fail"); return bin },
	}
	for name, git := range windows {
		t.Run(name, func(t *testing.T) {
			w := newPW(t)
			b := replayFirstCommit()
			if r := post(t, w.restart(t, git(t)), commitPath, b); r.status == 200 {
				t.Fatalf("the stopped call answered 200: %.300s", r.body)
			}
			// without the claim the retry would be refused: the folder is no longer empty
			snap := w.state(t)
			want409(t, post(t, w.freshRecord(t), commitPath, b), "ProjectExists")
			w.unchanged(t, snap, "a replay's first commit over a worker without the claim")
			v := wantVersion(t, post(t, w.base, commitPath, b))
			if commitCount(t, w.dir) != 1 || mainAt(t, w.dir) != v.Commit || !samePF(v.ProjectFiles, toPF(b["project_files"])) {
				t.Errorf("the retry makes one commit with the reused files: %+v", v)
			}
			wantTree(t, w.dir, v.Commit, contents(t, w.dir))
			wantCommitObject(t, w.dir, v.Commit, "", "2026-11-14T10:00:00Z", commitMessage("RUN-2", "contract-check", 0, 0, "RUN-1"))
		})
	}
}

func TestProjectWorkerProjectExistsNamesTheFirstPathFound(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		plant      func(t *testing.T, w *pw)
	}{
		{"a file deep down", "src/a/b/c/d/deep.txt", existing()["a file deep down"]},
		{"a symbolic link", "notes/link", existing()["a symbolic link"]},
		{"a non-empty .git alone", ".git", existing()["a non-empty .git alone"]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, call := range []struct {
				path string
				body map[string]any
			}{
				{restorePath, restoreBody(ccStep(), nil)}, {commitPath, replayFirstCommit()},
			} {
				w := newPW(t)
				tc.plant(t, w)
				p := want409(t, post(t, w.base, call.path, call.body), "ProjectExists")
				if !strings.Contains(p.Detail, tc.path) {
					t.Errorf("%s: detail %v does not name %s", call.path, p.Detail, tc.path)
				}
			}
		})
	}
}
