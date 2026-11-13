package checksvc_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
)

// T053c, commitStep: the repository, the commit's identity and message, repeats, the starting
// and ending states, reused results, empty commits, exact bytes, option-like names, ownership.

func TestProjectWorkerFirstCommitCreatesTheRepositoryOnMain(t *testing.T) {
	for _, gitFolder := range []string{"empty", "missing"} {
		t.Run(".git "+gitFolder, func(t *testing.T) {
			w := newPW(t)
			gitDir := filepath.Join(w.dir, ".git")
			if gitFolder == "missing" {
				os.Remove(gitDir)
			}
			fi0, _ := os.Stat(gitDir)
			v, body := w.firstStep(t)
			if !v.Created {
				t.Error("the first commit is made by this call: created must be true")
			}
			if !samePF(v.ProjectFiles, projectFiles(t, w.dir)) || !samePF(v.ProjectFiles, toPF(body["project_files"])) {
				t.Errorf("project_files must be the folder outside .git as committed: %v", v.ProjectFiles)
			}
			head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
			if err != nil || string(head) != "ref: refs/heads/main\n" {
				t.Errorf(".git/HEAD must be exactly ref: refs/heads/main and a line feed: %q %v", head, err)
			}
			if mainAt(t, w.dir) != v.Commit {
				t.Errorf("the loose branch main must name the commit %s, names %q", v.Commit, mainAt(t, w.dir))
			}
			if out := mustGit(t, w.dir, "for-each-ref", "--format=%(refname)"); strings.TrimSpace(out) != "refs/heads/main" {
				t.Errorf("the one ref must be refs/heads/main, got %q", out)
			}
			if n := commitCount(t, w.dir); n != 1 {
				t.Errorf("one commit, got %d", n)
			}
			wantTree(t, w.dir, v.Commit, contents(t, w.dir))
			if fi0 != nil {
				fi1, err := os.Stat(gitDir)
				if err != nil || !os.SameFile(fi0, fi1) {
					t.Error("the .git folder itself must stay (step containers mount it); only its contents are made")
				}
			}
		})
	}
}

func toPF(v any) []pf {
	if l, ok := v.([]pf); ok {
		return l
	}
	return nil
}

func TestProjectWorkerCommitIdentityDatesAndTrailers(t *testing.T) {
	w := newPW(t)
	// accepted_at with an offset: the dates are in UTC, in whole seconds.
	write(t, w.dir, "api/openapi.yaml", []byte("openapi: 3.1.0\n"))
	write(t, w.dir, "features/slice.feature", []byte("Feature: F\n"))
	at1 := "2026-11-13T12:00:01+02:00"
	b1 := commitBody(ccStep(), nil, projectFiles(t, w.dir), "RUN-7", 2, at1)
	v1 := wantVersion(t, post(t, w.base, commitPath, b1))
	wantCommitObject(t, w.dir, v1.Commit, "", at1, commitMessage("RUN-7", "contract-check", 0, 2, ""))

	// A review round: Round before Attempt.
	at2 := "2026-11-13T10:30:00Z"
	b2 := commitBody(step("review", []string{}, []string{"findings"}, nil, nil), toPF(b1["project_files"]), projectFiles(t, w.dir), "RUN-7", 3, at2)
	b2["round"] = 2
	v2 := wantVersion(t, post(t, w.base, commitPath, b2))
	wantCommitObject(t, w.dir, v2.Commit, v1.Commit, at2, commitMessage("RUN-7", "review", 2, 3, ""))
	if n := commitCount(t, w.dir); n != 2 {
		t.Errorf("one commit per call: want 2, got %d", n)
	}
	if mainAt(t, w.dir) != v2.Commit {
		t.Error("main must name the last commit")
	}
}

func TestProjectWorkerSameRequestsGiveTheSameCommit(t *testing.T) {
	// Equal requests over an equal folder and an equal last commit give the same commit.
	var ids []string
	for i := 0; i < 2; i++ {
		w := newPW(t)
		v, _ := w.firstStep(t)
		ids = append(ids, v.Commit)
	}
	if ids[0] != ids[1] {
		t.Errorf("the same request over the same folder must give the same commit: %v", ids)
	}
}

func TestProjectWorkerRepeatMakesNoSecondCommit(t *testing.T) {
	w := newPW(t)
	v1, b1 := w.firstStep(t)
	v2, b2 := w.secondStep(t, v1.ProjectFiles)
	snap := w.state(t)
	r := wantVersion(t, post(t, w.base, commitPath, b2))
	if r.Created || r.Commit != v2.Commit || !samePF(r.ProjectFiles, v2.ProjectFiles) {
		t.Errorf("a repeat answers the same commit with created false: %+v, want %s", r, v2.Commit)
	}
	w.unchanged(t, snap, "a repeat")
	if n := commitCount(t, w.dir); n != 2 {
		t.Errorf("a repeat makes no second commit: %d commits", n)
	}

	// The same trailers with another payload: 409 ProjectStateDiffers, nothing changed.
	other := func(mut func(b map[string]any)) map[string]any {
		b := clone(b2)
		mut(b)
		return b
	}
	for name, b := range map[string]map[string]any{
		"another accepted_at": other(func(b map[string]any) { b["accepted_at"] = "2026-11-13T11:00:00Z" }),
		"another project_files": other(func(b map[string]any) {
			b["project_files"] = append(b["project_files"].([]any), map[string]any{"path": "tests/z_test.go", "sha256": sum([]byte("x"))})
		}),
		"another before": other(func(b map[string]any) { b["before"] = []any{} }),
	} {
		t.Run(name, func(t *testing.T) {
			snap := w.state(t)
			wantProblem(t, post(t, w.base, commitPath, b), 409, "ProjectStateDiffers")
			w.unchanged(t, snap, "the same trailers with another payload")
		})
	}
	_ = b1
}

// After a 504 that came once the branch had moved, a repeat answers that commit, created false.
func TestProjectWorkerRepeatAfterTimeLimit(t *testing.T) {
	w := newPW(t)
	slow, _ := gitWrapper(t, "update-ref", "sleep")
	base := w.restart(t, slow)
	write(t, w.dir, "api/openapi.yaml", []byte("openapi: 3.1.0\n"))
	write(t, w.dir, "features/slice.feature", []byte("Feature: F\n"))
	body := commitBody(ccStep(), nil, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:01Z")
	body["time_limit_seconds"] = 1
	wantProblem(t, post(t, base, commitPath, body), 504, "TimeLimitExceeded")

	body["time_limit_seconds"] = 120
	v := wantVersion(t, post(t, w.restart(t, ""), commitPath, body))
	if n := commitCount(t, w.dir); n != 1 {
		t.Errorf("after a 504 at most one commit is made and a repeat makes none: %d commits", n)
	}
	if mainAt(t, w.dir) != v.Commit {
		t.Error("main must name the commit the repeat answers")
	}
	again := wantVersion(t, post(t, w.base, commitPath, body))
	if again.Created || again.Commit != v.Commit {
		t.Errorf("a further repeat answers created false and the same commit: %+v", again)
	}
}

func TestProjectWorkerStartingAndEndingStates(t *testing.T) {
	t.Run("the last commit does not hold before", func(t *testing.T) {
		w := newPW(t)
		v1, _ := w.firstStep(t)
		write(t, w.dir, "tests/a_test.go", []byte("package tests\n"))
		wrong := append([]pf{}, v1.ProjectFiles...)
		wrong[0].SHA256 = sum([]byte("other"))
		snap := w.state(t)
		wantProblem(t, post(t, w.base, commitPath, commitBody(testsStep(), wrong, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:02Z")), 409, "ProjectStateDiffers")
		w.unchanged(t, snap, "a before the last commit does not hold")
		if commitCount(t, w.dir) != 1 {
			t.Error("nothing may be committed")
		}
	})
	t.Run("the folder does not hold project_files", func(t *testing.T) {
		w := newPW(t)
		v1, _ := w.firstStep(t)
		write(t, w.dir, "tests/a_test.go", []byte("package tests\n"))
		after := projectFiles(t, w.dir)
		write(t, w.dir, "tests/extra_test.go", []byte("package tests\n"))
		snap := w.state(t)
		wantProblem(t, post(t, w.base, commitPath, commitBody(testsStep(), v1.ProjectFiles, after, "RUN-1", 1, "2026-11-13T10:00:02Z")), 409, "ProjectStateDiffers")
		w.unchanged(t, snap, "a folder that does not hold project_files")
		if commitCount(t, w.dir) != 1 {
			t.Error("nothing may be committed")
		}
	})
	// Before the first commit `before` must be empty; `.git` is emptied only in step (5), after
	// the before check and the ending state, so what it holds stays on a 409.
	for name, mut := range map[string]func(w *pw, b map[string]any){
		"a first commit with a non-empty before": func(w *pw, b map[string]any) {
			b["before"] = []any{map[string]any{"path": "api/openapi.yaml", "sha256": sum([]byte("openapi: 3.1.0\n"))}}
		},
		"a first commit over a folder that does not hold project_files": func(w *pw, b map[string]any) {
			write(t, w.dir, "README.md", []byte("not listed\n"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newPW(t)
			write(t, w.dir, ".git/leftover", []byte("an unfinished first call\n"))
			write(t, w.dir, "api/openapi.yaml", []byte("openapi: 3.1.0\n"))
			b := commitBody(ccStep(), nil, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:01Z")
			mut(w, b)
			snap := w.state(t)
			wantProblem(t, post(t, w.base, commitPath, b), 409, "ProjectStateDiffers")
			w.unchanged(t, snap, name)
		})
	}
}

func TestProjectWorkerReusedResult(t *testing.T) {
	tests := map[string][]byte{"tests/a_test.go": []byte("package tests\n"), "tests/sub/b_test.go": []byte("package sub\n")}
	setup := func(t *testing.T) (*pw, pwVersion, map[string]any) {
		w := newPW(t)
		v1, _ := w.firstStep(t)
		// Left by an interrupted attempt: a stale file in the step's writable paths, and a file
		// outside them the reused result must not touch.
		write(t, w.dir, "tests/stale_test.go", []byte("package stale\n"))
		write(t, w.dir, "tests/a_test.go", []byte("package old\n"))
		write(t, w.dir, "notes/keep.txt", []byte("kept\n"))
		want := map[string][]byte{}
		for p, c := range contents(t, w.dir) {
			if !strings.HasPrefix(p, "tests/") {
				want[p] = c
			}
		}
		for p, c := range tests {
			want[p] = c
		}
		after := []pf{}
		for _, p := range sortedKeys(want) {
			after = append(after, pf{p, sum(want[p])})
		}
		f := framed(tests)
		b := map[string]any{
			"run": "RUN-2", "step": testsStep(), "reused_from": "RUN-1", "project": pwProject,
			"before": v1.ProjectFiles, "project_files": after, "accepted_at": "2026-11-13T11:00:00Z",
			"outputs":            []any{map[string]any{"name": "tests", "sha256": sum(f), "content": b64(f)}},
			"time_limit_seconds": 120,
		}
		return w, v1, b
	}

	t.Run("written from the framed outputs, then committed", func(t *testing.T) {
		w, v1, b := setup(t)
		keep, _ := os.Stat(filepath.Join(w.dir, "notes", "keep.txt"))
		v := wantVersion(t, post(t, w.base, commitPath, b))
		if _, err := os.Lstat(filepath.Join(w.dir, "tests", "stale_test.go")); err == nil {
			t.Error("a file matching the writable entries that the outputs do not hold must be removed")
		}
		for p, c := range tests {
			if string(read(t, w.dir, p)) != string(c) {
				t.Errorf("%s must hold the output's bytes", p)
			}
		}
		if k2, err := os.Stat(filepath.Join(w.dir, "notes", "keep.txt")); err != nil || !k2.ModTime().Equal(keep.ModTime()) || string(read(t, w.dir, "notes/keep.txt")) != "kept\n" {
			t.Error("a file outside the step's writable entries must not be touched")
		}
		if !samePF(v.ProjectFiles, toPF(b["project_files"])) {
			t.Errorf("project_files: %v", v.ProjectFiles)
		}
		wantTree(t, w.dir, v.Commit, contents(t, w.dir))
		wantCommitObject(t, w.dir, v.Commit, v1.Commit, "2026-11-13T11:00:00Z", commitMessage("RUN-2", "tests", 0, 0, "RUN-1"))
		if _, err := os.Lstat(filepath.Join(w.dir, "tests", "sub")); err != nil {
			t.Error("the output's folder must be made")
		}
	})
	t.Run("InputHashMismatch before any write", func(t *testing.T) {
		w, _, b := setup(t)
		outs := b["outputs"].([]any)
		outs[0].(map[string]any)["sha256"] = sum([]byte("another artifact"))
		snap := w.state(t)
		wantProblem(t, post(t, w.base, commitPath, b), 422, "InputHashMismatch")
		w.unchanged(t, snap, "an output whose content does not have its sha256")
	})
	t.Run("exactly one of attempt and reused_from, outputs only with reused_from", func(t *testing.T) {
		w, _, b := setup(t)
		both := clone(b)
		both["attempt"] = 1
		neither := clone(b)
		delete(neither, "reused_from")
		outputsWithAttempt := clone(b)
		delete(outputsWithAttempt, "reused_from")
		outputsWithAttempt["attempt"] = 1
		snap := w.state(t)
		for name, body := range map[string]map[string]any{"both": both, "neither": neither, "outputs with attempt": outputsWithAttempt} {
			t.Run(name, func(t *testing.T) {
				wantProblem(t, post(t, w.base, commitPath, body), 400, "InvalidRequest")
			})
		}
		w.unchanged(t, snap, "a 400")
	})
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestProjectWorkerEmptyCommitForAStepThatChangedNothing(t *testing.T) {
	w := newPW(t)
	v1, _ := w.firstStep(t)
	b := commitBody(testsStep(), v1.ProjectFiles, v1.ProjectFiles, "RUN-1", 1, "2026-11-13T10:00:02Z")
	v2 := wantVersion(t, post(t, w.base, commitPath, b))
	if !v2.Created || v2.Commit == v1.Commit {
		t.Fatalf("a step that changed no file still gets its own commit: %+v", v2)
	}
	if commitCount(t, w.dir) != 2 {
		t.Error("one commit per accepted step")
	}
	t1 := mustGit(t, w.dir, "rev-parse", v1.Commit+"^{tree}")
	t2 := mustGit(t, w.dir, "rev-parse", v2.Commit+"^{tree}")
	if t1 != t2 {
		t.Error("the empty commit's tree must be its parent's")
	}
	wantCommitObject(t, w.dir, v2.Commit, v1.Commit, "2026-11-13T10:00:02Z", commitMessage("RUN-1", "tests", 0, 1, ""))
}

// Committed bytes are the folder's, whatever .gitattributes, .gitignore and info/exclude say;
// every file is 100644; nothing is added to the repository's config or info/; no hook runs.
func TestProjectWorkerCommitsExactBytesAndRunsNoHook(t *testing.T) {
	w := newPW(t)
	// `filter=planted` names a filter no configuration git reads can define (the worker's own
	// config is checked by the history guard), so it must simply apply nothing.
	files := map[string]string{
		".gitattributes":         "* text eol=crlf\n* filter=planted ident\n",
		".gitignore":             "*\n!.gitignore\n",
		"api/openapi.yaml":       "openapi: 3.1.0\nline two\n",
		"features/slice.feature": "Feature: F\r\nmixed\nendings\r\n",
		"ignored.log":            "$Id$\n",
	}
	for p, c := range files {
		write(t, w.dir, p, []byte(c))
	}
	// An executable file on disk is still committed as 100644.
	os.Chmod(filepath.Join(w.dir, "ignored.log"), 0o755)
	b1 := commitBody(ccStep(), nil, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:01Z")
	v1 := wantVersion(t, post(t, w.base, commitPath, b1))
	wantTree(t, w.dir, v1.Commit, contents(t, w.dir))
	config1 := read(t, w.dir, ".git/config")

	// Planted after the repository exists: hooks (each would leave a marker), info/exclude.
	var markers []string
	for _, h := range []string{"pre-commit", "commit-msg", "post-commit", "post-index-change", "reference-transaction", "prepare-commit-msg", "post-checkout"} {
		m, script := marker(t, h)
		markers = append(markers, m)
		body, _ := os.ReadFile(script)
		write(t, w.dir, ".git/hooks/"+h, body)
		os.Chmod(filepath.Join(w.dir, ".git", "hooks", h), 0o755)
	}
	write(t, w.dir, ".git/info/exclude", []byte("*\n"))
	write(t, w.dir, "tests/a_test.go", []byte("package tests\r\n"))
	write(t, w.dir, "tests/.gitattributes", []byte("*.go -text filter=planted\n"))
	b2 := commitBody(testsStep(), v1.ProjectFiles, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:02Z")
	v2 := wantVersion(t, post(t, w.base, commitPath, b2))
	wantTree(t, w.dir, v2.Commit, contents(t, w.dir))
	neverRan(t, markers...)

	if _, err := os.Lstat(filepath.Join(w.dir, ".git", "info", "attributes")); err == nil {
		t.Error("nothing may be added to .git/info: info/attributes exists")
	}
	if string(read(t, w.dir, ".git/config")) != string(config1) {
		t.Error("the repository's config must stay as init wrote it")
	}
	out := mustGit(t, w.dir, "config", "--file", filepath.Join(w.dir, ".git", "config"), "--list", "--name-only")
	for _, k := range strings.Fields(out) {
		k = strings.ToLower(k)
		if !strings.HasPrefix(k, "core.") || k == "core.autocrlf" || k == "core.hookspath" || k == "core.fsmonitor" || k == "core.attributesfile" || k == "core.excludesfile" || k == "core.safecrlf" || k == "core.eol" {
			t.Errorf("the repository's config holds %s: only what git init writes may be there", k)
		}
	}

	// Restore writes back the committed bytes, under the same attributes.
	write(t, w.dir, "features/slice.feature", []byte("changed\n"))
	os.Remove(filepath.Join(w.dir, "tests", "a_test.go"))
	os.Remove(filepath.Join(w.dir, "ignored.log"))
	wantRestored(t, post(t, w.base, restorePath, restoreBody(testsStep(), v2.ProjectFiles)))
	if string(read(t, w.dir, "features/slice.feature")) != files["features/slice.feature"] || string(read(t, w.dir, "tests/a_test.go")) != "package tests\r\n" || string(read(t, w.dir, "ignored.log")) != "$Id$\n" {
		t.Error("restore must write back the committed bytes exactly (no eol, ident or filter conversion)")
	}
	neverRan(t, markers...)
}

// A file named like an option is committed and restored as a file: paths reach git only on
// standard input or after `--`.
func TestProjectWorkerOptionLikeFileNames(t *testing.T) {
	w := newPW(t)
	wrap, log := gitWrapper(t, "", "")
	base := w.restart(t, wrap)
	cwd, _ := os.Getwd()
	names := []string{"--output=x", "-c", "api/--help", "api/-p"}
	for _, n := range names {
		write(t, w.dir, n, []byte("file "+n+"\n"))
	}
	write(t, w.dir, "api/openapi.yaml", []byte("openapi: 3.1.0\n"))
	b := commitBody(ccStep(), nil, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:01Z")
	v := wantVersion(t, post(t, base, commitPath, b))
	wantTree(t, w.dir, v.Commit, contents(t, w.dir))
	for _, d := range []string{w.dir, filepath.Join(w.dir, ".git"), cwd, w.root, w.history} {
		if _, err := os.Lstat(filepath.Join(d, "x")); err == nil {
			t.Errorf("a path was read as an option: %s/x was written", d)
		}
	}

	// Restored as files too.
	write(t, w.dir, "-c", []byte("changed\n"))
	os.Remove(filepath.Join(w.dir, "--output=x"))
	r := wantRestored(t, post(t, base, restorePath, restoreBody(testsStep(), v.ProjectFiles)))
	if strings.Join(r.Restored, ",") != "--output=x,-c" {
		t.Errorf("restored must list the two files, got %v", r.Restored)
	}
	for _, n := range names {
		if string(read(t, w.dir, n)) != "file "+n+"\n" {
			t.Errorf("%s was not restored as a file", n)
		}
	}

	calls := gitCalls(t, log)
	if len(calls) == 0 {
		t.Fatal("Config.Git is not used: the wrapper logged no git call")
	}
	for _, c := range calls {
		dd := len(c.args)
		for i, a := range c.args {
			if a == "--" {
				dd = i
				break
			}
		}
		for i, a := range c.args[:dd] {
			for _, n := range names {
				if a == n && n != "-c" || strings.HasSuffix(a, "/"+n) {
					t.Errorf("the path %s reached git as an argument before --: %v", n, c.args)
				}
			}
			if a == "-c" && (i+1 >= len(c.args) || !strings.Contains(c.args[i+1], "=")) {
				t.Errorf("a -c not followed by name=value: %v", c.args)
			}
		}
	}
}

// Git runs with every option fixed in code, plumbing only.
func TestProjectWorkerGitOptionsAreFixed(t *testing.T) {
	w := newPW(t)
	wrap, log := gitWrapper(t, "", "")
	base := w.restart(t, wrap)
	write(t, w.dir, "api/openapi.yaml", []byte("openapi: 3.1.0\n"))
	v1 := wantVersion(t, post(t, base, commitPath, commitBody(ccStep(), nil, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:01Z")))
	write(t, w.dir, "tests/a_test.go", []byte("package tests\n"))
	v2 := wantVersion(t, post(t, base, commitPath, commitBody(testsStep(), v1.ProjectFiles, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:02Z")))
	write(t, w.dir, "tests/a_test.go", []byte("changed\n"))
	wantRestored(t, post(t, base, restorePath, restoreBody(testsStep(), v2.ProjectFiles)))

	porcelain := map[string]bool{"add": true, "commit": true, "checkout": true, "reset": true, "restore": true, "stash": true,
		"status": true, "switch": true, "merge": true, "rebase": true, "gc": true, "apply": true, "am": true, "show": true,
		"log": true, "diff": true, "clean": true, "rm": true, "mv": true, "worktree": true, "fsck": true, "repack": true, "prune": true}
	wantOpts := []string{"core.fsmonitor=false", "core.hooksPath=/dev/null", "gc.auto=0", "commit.gpgSign=false"}
	calls := gitCalls(t, log)
	if len(calls) == 0 {
		t.Fatal("Config.Git is not used: the wrapper logged no git call")
	}
	sawHash := false
	for _, c := range calls {
		if c.env != "ENV:1:/dev/null:1" {
			t.Errorf("git must run with GIT_CONFIG_NOSYSTEM=1, GIT_CONFIG_GLOBAL=/dev/null, GIT_NO_REPLACE_OBJECTS=1: %s %v", c.env, c.args)
		}
		opts := map[string]bool{}
		sub := ""
		for i := 0; i < len(c.args); i++ {
			a := c.args[i]
			if a == "-c" && i+1 < len(c.args) {
				opts[strings.ToLower(c.args[i+1])] = true
				i++
				continue
			}
			if a == "-C" || a == "--git-dir" || a == "--work-tree" {
				i++
				continue
			}
			if strings.HasPrefix(a, "-") {
				continue
			}
			sub = a
			break
		}
		for _, o := range wantOpts {
			if !opts[strings.ToLower(o)] {
				t.Errorf("git %s runs without -c %s: %v", sub, o, c.args)
			}
		}
		if porcelain[sub] {
			t.Errorf("git %s is not plumbing: %v", sub, c.args)
		}
		if sub == "hash-object" {
			sawHash = true
			if !contains(c.args, "--no-filters") {
				t.Errorf("hash-object must apply no filter: %v", c.args)
			}
		}
	}
	if !sawHash {
		t.Error("blobs must be written with hash-object --no-filters")
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestProjectWorkerFilesOwnedByTheDeveloper(t *testing.T) {
	w := newPW(t)
	v1, _ := w.firstStep(t)
	v2, _ := w.secondStep(t, v1.ProjectFiles)
	os.Remove(filepath.Join(w.dir, "tests", "a_test.go"))
	wantRestored(t, post(t, w.base, restorePath, restoreBody(testsStep(), v2.ProjectFiles)))
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
	filepath.Walk(w.dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			t.Error(err)
			return nil
		}
		st := fi.Sys().(*syscall.Stat_t)
		if st.Uid != uid || st.Gid != gid {
			rel, _ := filepath.Rel(w.dir, p)
			t.Errorf("%s is owned by %d:%d, want the developer's %d:%d", rel, st.Uid, st.Gid, uid, gid)
		}
		return nil
	})
}

// A project file that changes after the worker read the folder and before git hashes it: the
// blobs git wrote are not the bytes checked against project_files, so nothing is committed.
func TestProjectWorkerFileChangedBeforeHashingIsNotCommitted(t *testing.T) {
	w := newPW(t)
	v1, _ := w.firstStep(t)
	target := filepath.Join(w.dir, "tests", "main.go")
	write(t, w.dir, "tests/main.go", []byte("package tests\n"))
	b2 := commitBody(testsStep(), v1.ProjectFiles, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:02Z")

	d := t.TempDir()
	bin := filepath.Join(d, "git")
	s := "#!/bin/sh\nfor a in \"$@\"; do case \"$a\" in hash-object) printf 'package changed\\n' > '" + target + "';; esac; done\n" +
		"exec \"" + gitBin(t) + "\" \"$@\"\n"
	if err := os.WriteFile(bin, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}
	histSnap := snapshot(t, w.history)
	wantProblem(t, post(t, w.restart(t, bin), commitPath, b2), 409, "ProjectStateDiffers")
	if string(read(t, w.dir, "tests/main.go")) != "package changed\n" {
		t.Fatal("the wrapper did not run before hash-object: the test proves nothing")
	}
	if mainAt(t, w.dir) != v1.Commit || commitCount(t, w.dir) != 1 {
		t.Errorf("nothing may be committed: main %s, %d commits", mainAt(t, w.dir), commitCount(t, w.dir))
	}
	if d := sameSnapshot(histSnap, snapshot(t, w.history)); d != "" {
		t.Errorf("the record must not change: %s", d)
	}
}
