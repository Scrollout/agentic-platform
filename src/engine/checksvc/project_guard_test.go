package checksvc_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// T053c, the history guard (info.description, The project worker's record): 409 HistoryChanged
// with nothing changed for a `.git` the worker did not make; the crash states it passes.

// thirdBody is the commitStep body after secondStep, for the guard to refuse or pass.
func (w *pw) thirdBody(t testing.TB, before []pf) map[string]any {
	write(t, w.dir, "internal/app.go", []byte("package app\n"))
	return commitBody(step("code", []string{"internal/**"}, []string{"code"}, nil, nil), before, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:03Z")
}

func TestProjectWorkerHistoryGuard(t *testing.T) {
	type tamper func(t *testing.T, w *pw, c1, c2 string)
	fsmonitorRan, fsmonitorCmd := marker(t, "fsmonitor")
	writeGit := func(rel, content string) tamper {
		return func(t *testing.T, w *pw, _, _ string) { write(t, w.dir, ".git/"+rel, []byte(content)) }
	}
	head := func(content string) tamper {
		return func(t *testing.T, w *pw, _, _ string) {
			os.WriteFile(filepath.Join(w.dir, ".git", "HEAD"), []byte(content), 0o644)
		}
	}
	cases := map[string]tamper{
		"the branch moved to a commit the worker did not make": func(t *testing.T, w *pw, c1, c2 string) {
			setMain(t, w.dir, commitWithout(t, w.dir, c2, c2))
		},
		"the branch moved to a root commit the worker did not make": func(t *testing.T, w *pw, c1, c2 string) {
			setMain(t, w.dir, commitWithout(t, w.dir, c2, ""))
		},
		"HEAD names another branch":  head("ref: refs/heads/other\n"),
		"HEAD without its line feed": head("ref: refs/heads/main"),
		"HEAD with two line feeds":   head("ref: refs/heads/main\n\n"),
		"HEAD with CR LF":            head("ref: refs/heads/main\r\n"),
		"HEAD detached at the recorded commit": func(t *testing.T, w *pw, _, c2 string) {
			os.WriteFile(filepath.Join(w.dir, ".git", "HEAD"), []byte(c2+"\n"), 0o644)
		},
		"a changed config with a core.fsmonitor command": func(t *testing.T, w *pw, _, _ string) {
			f, _ := os.OpenFile(filepath.Join(w.dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
			f.WriteString("[core]\n\tfsmonitor = " + fsmonitorCmd + "\n")
			f.Close()
		},
		"a symbolic link in .git/refs": func(t *testing.T, w *pw, _, _ string) {
			os.MkdirAll(filepath.Join(w.dir, ".git", "refs", "tags"), 0o755)
			os.Symlink("../heads/main", filepath.Join(w.dir, ".git", "refs", "tags", "t"))
		},
		"a symbolic link deep in .git/objects": func(t *testing.T, w *pw, _, _ string) {
			os.Symlink("/dev/null", filepath.Join(w.dir, ".git", "objects", "link"))
		},
		"a commondir": writeGit("commondir", "../other\n"),
		"a gitdir":    writeGit("gitdir", "/elsewhere/.git\n"),
		"info/grafts": func(t *testing.T, w *pw, c1, c2 string) { write(t, w.dir, ".git/info/grafts", []byte(c2+"\n")) },
		"a shallow":   func(t *testing.T, w *pw, c1, _ string) { write(t, w.dir, ".git/shallow", []byte(c1+"\n")) },
		"an alternates": func(t *testing.T, w *pw, _, _ string) {
			write(t, w.dir, ".git/objects/info/alternates", []byte(t.TempDir()+"\n"))
		},
		"a main entry in packed-refs beside the loose ref": func(t *testing.T, w *pw, _, c2 string) {
			write(t, w.dir, ".git/packed-refs", []byte("# pack-refs with: peeled fully-peeled sorted \n"+c2+" refs/heads/main\n"))
		},
		"main only in packed-refs": func(t *testing.T, w *pw, _, c2 string) {
			write(t, w.dir, ".git/packed-refs", []byte("# pack-refs with: peeled fully-peeled sorted \n"+c2+" refs/heads/main\n"))
			setMain(t, w.dir, "")
		},
		"the branch missing": func(t *testing.T, w *pw, _, _ string) { setMain(t, w.dir, "") },
		"the recorded commit missing": func(t *testing.T, w *pw, _, c2 string) {
			p := filepath.Join(w.dir, ".git", "objects", c2[:2], c2[2:])
			if err := os.Remove(p); err != nil {
				t.Fatalf("the recorded commit is not a loose object: %v", err)
			}
		},
		"the recorded commit with another parent": func(t *testing.T, w *pw, c1, c2 string) {
			raw := rawCommit(t, w.dir, c2)
			other := commitWithout(t, w.dir, c1, "")
			overwriteObject(t, w.dir, c2, strings.Replace(raw, "parent "+c1, "parent "+other, 1))
		},
		"the recorded commit with other trailers": func(t *testing.T, w *pw, _, c2 string) {
			raw := rawCommit(t, w.dir, c2)
			overwriteObject(t, w.dir, c2, strings.Replace(raw, "Attempt: 1\n", "Attempt: 2\n", 1))
		},
	}
	for name, tam := range cases {
		t.Run(name, func(t *testing.T) {
			w := newPW(t)
			v1, _ := w.firstStep(t)
			v2, _ := w.secondStep(t, v1.ProjectFiles)
			next := w.thirdBody(t, v2.ProjectFiles)
			tam(t, w, v1.Commit, v2.Commit)
			snap := w.state(t)
			wantProblem(t, post(t, w.base, commitPath, next), 409, "HistoryChanged")
			w.unchanged(t, snap, "a commitStep the guard refuses")
			// The repeat of the recorded step is refused too: the guard comes before the repeat.
			b2 := w.secondStepBody(v1.ProjectFiles)
			wantProblem(t, post(t, w.base, commitPath, b2), 409, "HistoryChanged")
			w.unchanged(t, snap, "a repeated commitStep the guard refuses")
			wantProblem(t, post(t, w.base, restorePath, restoreBody(testsStep(), v2.ProjectFiles)), 409, "HistoryChanged")
			w.unchanged(t, snap, "a restoreStep the guard refuses")
			neverRan(t, fsmonitorRan)
		})
	}
}

// secondStepBody is the body secondStep sent, rebuilt (the folder now holds more files).
func (w *pw) secondStepBody(before []pf) map[string]any {
	after := append(append([]pf{}, before...), pf{"tests/a_test.go", sum([]byte("package tests\n"))})
	sort.Slice(after, func(i, j int) bool { return after[i].Path < after[j].Path })
	return commitBody(testsStep(), before, after, "RUN-1", 1, "2026-11-13T10:00:02Z")
}

// A record lost (a worker over a new record folder) with a repository in `.git`: the history
// is not one this worker made, and nothing in the project changes.
func TestProjectWorkerNonEmptyGitWithNoRecord(t *testing.T) {
	w := newPW(t)
	v1, _ := w.firstStep(t)
	v2, _ := w.secondStep(t, v1.ProjectFiles)
	next := w.thirdBody(t, v2.ProjectFiles)
	w.history = t.TempDir()
	base := w.restart(t, "")
	snap := w.state(t)
	want409(t, post(t, base, commitPath, next), "HistoryChanged", "ProjectStateDiffers")
	w.unchanged(t, snap, "a commitStep with a repository and no record")
	want409(t, post(t, base, restorePath, withAttempt(restoreBody(testsStep(), v2.ProjectFiles), 2)), "HistoryChanged", "ProjectStateDiffers")
	w.unchanged(t, snap, "a restoreStep with a repository and no record")
}

// A call that stopped between writing the record and moving the branch: the same request
// passes and makes no second commit; any other request is refused.
func TestProjectWorkerStoppedBeforeTheBranchMoved(t *testing.T) {
	t.Run("simulated: main moved back to the recorded parent", func(t *testing.T) {
		w := newPW(t)
		v1, _ := w.firstStep(t)
		v2, b2 := w.secondStep(t, v1.ProjectFiles)
		setMain(t, w.dir, v1.Commit)
		other := clone(b2)
		other["accepted_at"] = "2026-11-13T10:09:09Z"
		snap := w.state(t)
		wantProblem(t, post(t, w.base, commitPath, other), 409, "HistoryChanged")
		w.unchanged(t, snap, "a recorded commit whose parent is the branch but which this request would not make")
		wantProblem(t, post(t, w.base, restorePath, restoreBody(testsStep(), v1.ProjectFiles)), 409, "HistoryChanged")
		w.unchanged(t, snap, "a restore while the branch is behind the record")

		v := wantVersion(t, post(t, w.base, commitPath, b2))
		if v.Commit != v2.Commit || mainAt(t, w.dir) != v2.Commit || commitCount(t, w.dir) != 2 {
			t.Errorf("the same request moves main to the recorded commit and makes no second one: %+v, main %s, %d commits", v, mainAt(t, w.dir), commitCount(t, w.dir))
		}
		again := wantVersion(t, post(t, w.base, commitPath, b2))
		if again.Created || again.Commit != v2.Commit {
			t.Errorf("then a repeat answers created false: %+v", again)
		}
	})
	t.Run("simulated: a first commit with main missing", func(t *testing.T) {
		w := newPW(t)
		v1, b1 := w.firstStep(t)
		setMain(t, w.dir, "")
		other := clone(b1)
		other["attempt"] = 2
		snap := w.state(t)
		wantProblem(t, post(t, w.base, commitPath, other), 409, "HistoryChanged")
		w.unchanged(t, snap, "a missing main whose recorded commit this request would not make")
		v := wantVersion(t, post(t, w.base, commitPath, b1))
		if v.Commit != v1.Commit || mainAt(t, w.dir) != v1.Commit || commitCount(t, w.dir) != 1 {
			t.Errorf("the same request passes and makes no second commit: %+v", v)
		}
	})
	t.Run("update-ref failed", func(t *testing.T) {
		w := newPW(t)
		v1, _ := w.firstStep(t)
		failing, _ := gitWrapper(t, "update-ref", "fail")
		write(t, w.dir, "tests/a_test.go", []byte("package tests\n"))
		b2 := commitBody(testsStep(), v1.ProjectFiles, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:02Z")
		if r := post(t, w.restart(t, failing), commitPath, b2); r.status == 200 {
			t.Fatalf("a commitStep whose update-ref fails cannot answer 200: %.300s", r.body)
		}
		if mainAt(t, w.dir) != v1.Commit {
			t.Fatalf("main must not have moved")
		}
		other := clone(b2)
		other["attempt"] = 2
		wantProblem(t, post(t, w.base, commitPath, other), 409, "HistoryChanged")
		v := wantVersion(t, post(t, w.base, commitPath, b2))
		if mainAt(t, w.dir) != v.Commit || commitCount(t, w.dir) != 2 {
			t.Errorf("the repeat makes the one commit: main %s, %d commits", mainAt(t, w.dir), commitCount(t, w.dir))
		}
		wantCommitObject(t, w.dir, v.Commit, v1.Commit, "2026-11-13T10:00:02Z", commitMessage("RUN-1", "tests", 0, 1, ""))
	})
}

// The record is replaced atomically: half-written temporary files beside it are ignored.
func TestProjectWorkerHalfWrittenRecordIsIgnored(t *testing.T) {
	w := newPW(t)
	v1, _ := w.firstStep(t)
	var planted int
	filepath.Walk(w.history, func(p string, fi os.FileInfo, err error) error {
		if err != nil || !fi.Mode().IsRegular() {
			return nil
		}
		b, _ := os.ReadFile(p)
		half := b[:len(b)/2]
		for _, name := range []string{p + ".tmp", p + ".new", p + "~", filepath.Join(filepath.Dir(p), ".tmp-"+filepath.Base(p)), filepath.Join(filepath.Dir(p), "."+filepath.Base(p)+".tmp")} {
			os.WriteFile(name, half, 0o600)
		}
		planted++
		return nil
	})
	if planted == 0 {
		t.Fatal("the first commit left no record in the record folder (Config.History)")
	}
	for _, name := range []string{".tmp", "tmp", pwProject + ".tmp", "." + pwProject + ".tmp"} {
		os.WriteFile(filepath.Join(w.history, name), []byte("{\"commit\": \"00"), 0o600)
	}
	v2, b2 := w.secondStep(t, v1.ProjectFiles)
	// A restarted worker reads the record, not a temporary file.
	again := wantVersion(t, post(t, w.restart(t, ""), commitPath, b2))
	if again.Created || again.Commit != v2.Commit || commitCount(t, w.dir) != 2 {
		t.Errorf("after a restart the repeat answers created false: %+v", again)
	}
}

// Crashes during the first call, before any record: the next commitStep empties the contents
// of `.git` (never the folder), runs git init again and makes one commit.
func TestProjectWorkerRecoversAnUnfinishedFirstCall(t *testing.T) {
	cases := map[string]func(t *testing.T, w *pw){
		"after git init": func(t *testing.T, w *pw) {
			mustGit(t, w.dir, "init", "--template=", "--initial-branch=main", "-q", filepath.Join(w.dir))
			write(t, w.dir, ".git/index.lock", []byte("x"))
		},
		"after objects were written": func(t *testing.T, w *pw) {
			mustGit(t, w.dir, "init", "--template=", "--initial-branch=main", "-q", filepath.Join(w.dir))
			mustGit(t, w.dir, "add", "-A")
			tree := strings.TrimSpace(mustGit(t, w.dir, "write-tree"))
			c := strings.TrimSpace(mustGit(t, w.dir, "commit-tree", tree, "-m", "unfinished"))
			write(t, w.dir, ".git/unfinished", []byte(c))
		},
	}
	for name, crash := range cases {
		t.Run(name, func(t *testing.T) {
			w := newPW(t)
			gitDir := filepath.Join(w.dir, ".git")
			fi0, _ := os.Stat(gitDir)
			write(t, w.dir, "api/openapi.yaml", []byte("openapi: 3.1.0\n"))
			crash(t, w)
			var junk string
			if b, err := os.ReadFile(filepath.Join(gitDir, "unfinished")); err == nil {
				junk = string(b)
			}
			v := wantVersion(t, post(t, w.base, commitPath, commitBody(ccStep(), nil, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:01Z")))
			if commitCount(t, w.dir) != 1 || mainAt(t, w.dir) != v.Commit {
				t.Errorf("one commit on main, got %d", commitCount(t, w.dir))
			}
			for _, left := range []string{"index.lock", "unfinished"} {
				if _, err := os.Lstat(filepath.Join(gitDir, left)); err == nil {
					t.Errorf(".git/%s must have been emptied away", left)
				}
			}
			if junk != "" {
				if _, err := gitIn(t, w.dir, nil, "cat-file", "-e", junk); err == nil {
					t.Error("the unfinished call's objects must be gone: .git's contents are emptied")
				}
			}
			if fi1, err := os.Stat(gitDir); err != nil || !os.SameFile(fi0, fi1) {
				t.Error("the .git folder itself is never removed")
			}
			wantTree(t, w.dir, v.Commit, contents(t, w.dir))
		})
	}
}

// A call that stopped after git init and the provisional record (commit-tree failed): the
// provisional record guards the config init wrote, and the branch must be missing.
func TestProjectWorkerProvisionalRecord(t *testing.T) {
	ran, cmd := marker(t, "fsmonitor")
	setup := func(t *testing.T) (*pw, map[string]any) {
		w := newPW(t)
		failing, _ := gitWrapper(t, "commit-tree", "fail")
		write(t, w.dir, "api/openapi.yaml", []byte("openapi: 3.1.0\n"))
		b := commitBody(ccStep(), nil, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:01Z")
		if r := post(t, w.restart(t, failing), commitPath, b); r.status == 200 {
			t.Fatalf("a commitStep whose commit-tree fails cannot answer 200: %.300s", r.body)
		}
		if _, err := os.Stat(filepath.Join(w.dir, ".git", "config")); err != nil {
			t.Fatalf("git init must have run before commit-tree: %v", err)
		}
		return w, b
	}
	t.Run("the same request then passes", func(t *testing.T) {
		w, b := setup(t)
		v := wantVersion(t, post(t, w.base, commitPath, b))
		if commitCount(t, w.dir) != 1 || mainAt(t, w.dir) != v.Commit {
			t.Error("one commit on main")
		}
	})
	t.Run("restore with before empty passes", func(t *testing.T) {
		w, _ := setup(t)
		wantRestored(t, post(t, w.base, restorePath, restoreBody(ccStep(), nil)))
	})
	for name, tam := range map[string]func(t *testing.T, w *pw){
		"a changed config": func(t *testing.T, w *pw) {
			f, _ := os.OpenFile(filepath.Join(w.dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
			f.WriteString("[core]\n\tfsmonitor = " + cmd + "\n")
			f.Close()
		},
		"a branch the worker did not make": func(t *testing.T, w *pw) {
			mustGit(t, w.dir, "add", "-A")
			tree := strings.TrimSpace(mustGit(t, w.dir, "write-tree"))
			os.Remove(filepath.Join(w.dir, ".git", "index"))
			setMain(t, w.dir, strings.TrimSpace(mustGit(t, w.dir, "commit-tree", tree, "-m", "planted")))
		},
		"HEAD rewritten": func(t *testing.T, w *pw) {
			os.WriteFile(filepath.Join(w.dir, ".git", "HEAD"), []byte("ref: refs/heads/other\n"), 0o644)
		},
	} {
		t.Run(name, func(t *testing.T) {
			w, b := setup(t)
			tam(t, w)
			snap := w.state(t)
			wantProblem(t, post(t, w.base, commitPath, b), 409, "HistoryChanged")
			w.unchanged(t, snap, "a commitStep over a provisional record the guard refuses")
			wantProblem(t, post(t, w.base, restorePath, restoreBody(ccStep(), nil)), 409, "HistoryChanged")
			w.unchanged(t, snap, "a restoreStep over a provisional record the guard refuses")
			neverRan(t, ran)
		})
	}
}
