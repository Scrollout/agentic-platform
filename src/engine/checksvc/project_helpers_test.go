// T053c: failing tests, by a separate author, for the project worker (the third deployment of
// package checksvc: commitStep and restoreStep), written from the contract alone,
// src/check/api/preview/2026-11-13-preview/openapi.yaml (info.description: Version history, The
// project worker's record, Creating the repository, Folders; the two operations' descriptions).
//
// The seam: checksvc.NewHandler(checksvc.Config{Token, Project, History}). A non-empty History
// (the record folder) makes the deployment the project worker. Config.Git, when set, is the git
// binary the worker runs; some tests set it to a wrapper script that logs every invocation and
// fails or stalls one subcommand, to make the crash states the contract names. The tests inspect
// the repository with the real git CLI (plumbing only) and skip only when git is not on PATH.
// Requests are plain JSON maps, as in the other checksvc tests.
package checksvc_test

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/agentic-platform/engine/checksvc"
)

const (
	commitPath  = "/project-versions/commit"
	restorePath = "/project-versions/restore"
	pwProject   = "p1"
)

// gitBin is the real git on PATH; a test that needs it skips with a clear message without it.
func gitBin(t testing.TB) string {
	t.Helper()
	p, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH: the project worker's tests need the git CLI")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// pw is one project worker over its own projects folder and record folder, with one project.
type pw struct {
	root, history, dir, base string
}

// newPW makes a projects folder with the project p1 and its empty `.git` folder (as the launcher
// creates it), a record folder, and a project worker over both.
func newPW(t testing.TB) *pw {
	t.Helper()
	gitBin(t)
	w := &pw{root: t.TempDir(), history: t.TempDir()}
	w.dir = filepath.Join(w.root, pwProject)
	if err := os.MkdirAll(filepath.Join(w.dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	w.base = serve(t, w.config(""))
	return w
}

func (w *pw) config(git string) checksvc.Config {
	return checksvc.Config{Token: token, Project: w.root, History: w.history, Git: git}
}

// restart is a new project worker over the same folders (a restarted deployment), with git as
// its git binary (empty: git on PATH).
func (w *pw) restart(t testing.TB, git string) string {
	t.Helper()
	return serve(t, w.config(git))
}

// ---- steps and bodies ----

func ccStep() map[string]any {
	return step("contract-check", []string{"api/openapi.yaml", "features/slice.feature"}, []string{"contract", "examples"}, nil, nil)
}

func testsStep() map[string]any {
	return step("tests", []string{"tests/**"}, []string{"tests"}, nil, nil)
}

func genStep() map[string]any {
	return step("generate", []string{"gen/**", "go.mod", "go.sum", "vendor/**"}, []string{"generated-code"}, nil, nil)
}

// commitBody is a commitStep body for an accepted attempt.
func commitBody(st map[string]any, before, after []pf, run string, attempt int, at string) map[string]any {
	if before == nil {
		before = []pf{}
	}
	if after == nil {
		after = []pf{}
	}
	return map[string]any{
		"run": run, "step": st, "attempt": attempt, "project": pwProject,
		"before": before, "project_files": after, "accepted_at": at, "time_limit_seconds": 120,
	}
}

// restoreBody is a restoreStep body for the attempt about to start.
func restoreBody(st map[string]any, before []pf) map[string]any {
	return common(pwProject, st, before)
}

// ---- the project folder outside `.git` ----

// projectFiles is every regular file of dir outside the root's `.git`, in byte order.
func projectFiles(t testing.TB, dir string) []pf {
	t.Helper()
	out := []pf{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == ".git" && d.IsDir() {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out = append(out, pf{filepath.ToSlash(rel), sum(b)})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// contents is every file projectFiles lists, with its bytes.
func contents(t testing.TB, dir string) map[string][]byte {
	t.Helper()
	m := map[string][]byte{}
	for _, f := range projectFiles(t, dir) {
		m[f.Path] = read(t, dir, f.Path)
	}
	return m
}

func samePF(a, b []pf) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// state is a snapshot of the project folder (`.git` included) and of the record folder.
func (w *pw) state(t testing.TB) map[string]string {
	t.Helper()
	m := map[string]string{}
	for k, v := range snapshot(t, w.dir) {
		m["project/"+k] = v
	}
	for k, v := range snapshot(t, w.history) {
		m["history/"+k] = v
	}
	return m
}

func (w *pw) unchanged(t testing.TB, before map[string]string, what string) {
	t.Helper()
	if d := sameSnapshot(before, w.state(t)); d != "" {
		t.Errorf("%s must change nothing: %s", what, d)
	}
}

// ---- responses ----

type pwVersion struct {
	Commit       string `json:"commit"`
	Created      bool   `json:"created"`
	ProjectFiles []pf   `json:"project_files"`
}

func wantVersion(t testing.TB, r resp) pwVersion {
	t.Helper()
	if r.status != 200 {
		t.Fatalf("commitStep: want 200, got %d: %.600s", r.status, r.body)
	}
	var v pwVersion
	if err := strictJSON(r.body, &v); err != nil || len(v.Commit) != 40 || v.ProjectFiles == nil {
		t.Fatalf("not a ProjectVersion with commit and project_files: %v %.400s", err, r.body)
	}
	return v
}

type restored struct {
	Restored     []string `json:"restored"`
	ProjectFiles []pf     `json:"project_files"`
}

func wantRestored(t testing.TB, r resp) restored {
	t.Helper()
	if r.status != 200 {
		t.Fatalf("restoreStep: want 200, got %d: %.600s", r.status, r.body)
	}
	var v restored
	if err := strictJSON(r.body, &v); err != nil || v.Restored == nil || v.ProjectFiles == nil {
		t.Fatalf("not a RestoredProject with restored and project_files: %v %.400s", err, r.body)
	}
	return v
}

// want409 accepts a 409 with one of codes.
func want409(t testing.TB, r resp, codes ...string) problem {
	t.Helper()
	var p problem
	_ = json.Unmarshal(r.body, &p)
	if r.status != 409 {
		t.Fatalf("want 409 %v, got %d: %.400s", codes, r.status, r.body)
	}
	for _, c := range codes {
		if p.Code == c {
			return p
		}
	}
	t.Fatalf("want 409 %v, got %s: %.400s", codes, p.Code, r.body)
	return p
}

// ---- a short history ----

// firstStep writes contract-check's files and commits them as RUN-1's first commit.
func (w *pw) firstStep(t testing.TB) (pwVersion, map[string]any) {
	t.Helper()
	write(t, w.dir, "api/openapi.yaml", []byte("openapi: 3.1.0\n"))
	write(t, w.dir, "features/slice.feature", []byte("Feature: F\n"))
	body := commitBody(ccStep(), nil, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:01Z")
	v := wantVersion(t, post(t, w.base, commitPath, body))
	return v, body
}

// secondStep adds the tests step's file and commits it on top of the first.
func (w *pw) secondStep(t testing.TB, before []pf) (pwVersion, map[string]any) {
	t.Helper()
	write(t, w.dir, "tests/a_test.go", []byte("package tests\n"))
	body := commitBody(testsStep(), before, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:02Z")
	v := wantVersion(t, post(t, w.base, commitPath, body))
	return v, body
}

// ---- git, from the test ----

// gitIn runs the real git in the project folder with no outside configuration and no hook,
// fsmonitor or replace object, so inspecting a planted repository runs nothing it names.
func gitIn(t testing.TB, dir string, stdin []byte, args ...string) (string, error) {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "gc.auto=0", "-c", "safe.directory=*"}, args...)
	cmd := exec.Command(gitBin(t), full...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_NO_REPLACE_OBJECTS=1",
		"GIT_AUTHOR_NAME=someone", "GIT_AUTHOR_EMAIL=someone@example.com", "GIT_COMMITTER_NAME=someone", "GIT_COMMITTER_EMAIL=someone@example.com")
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("git %v: %v: %s", args, err, errb.String())
	}
	return out.String(), nil
}

func mustGit(t testing.TB, dir string, args ...string) string {
	t.Helper()
	out, err := gitIn(t, dir, nil, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// commitCount is the number of commits reachable from main.
func commitCount(t testing.TB, dir string) int {
	t.Helper()
	out := strings.TrimSpace(mustGit(t, dir, "rev-list", "--count", "refs/heads/main"))
	var n int
	fmt.Sscan(out, &n)
	return n
}

// mainAt is what `.git/refs/heads/main` names, read as a file ("" when missing).
func mainAt(t testing.TB, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".git", "refs", "heads", "main"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func setMain(t testing.TB, dir, id string) {
	t.Helper()
	p := filepath.Join(dir, ".git", "refs", "heads", "main")
	if id == "" {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(p, []byte(id+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// rawCommit is the commit object's content, as git stores it.
func rawCommit(t testing.TB, dir, id string) string {
	t.Helper()
	return mustGit(t, dir, "cat-file", "commit", id)
}

type treeEntry struct {
	mode, kind, id string
}

// tree is every entry of the commit's tree, recursively, by path.
func tree(t testing.TB, dir, commit string) map[string]treeEntry {
	t.Helper()
	out := mustGit(t, dir, "ls-tree", "-r", "-z", "--full-tree", commit)
	m := map[string]treeEntry{}
	for _, rec := range strings.Split(out, "\x00") {
		if rec == "" {
			continue
		}
		meta, path, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 {
			t.Fatalf("ls-tree gave %q", rec)
		}
		m[path] = treeEntry{f[0], f[1], f[2]}
	}
	return m
}

// wantTree fails unless the commit holds exactly files, byte for byte, each as mode 100644.
func wantTree(t testing.TB, dir, commit string, files map[string][]byte) {
	t.Helper()
	got := tree(t, dir, commit)
	for p, e := range got {
		want, ok := files[p]
		if !ok {
			t.Errorf("the commit holds %s, which project_files does not list", p)
			continue
		}
		if e.mode != "100644" || e.kind != "blob" {
			t.Errorf("%s is committed as %s %s, want a 100644 blob", p, e.mode, e.kind)
		}
		b, err := gitIn(t, dir, nil, "cat-file", "blob", e.id)
		if err != nil {
			t.Fatal(err)
		}
		if b != string(want) {
			t.Errorf("%s is committed as %q, want exactly %q", p, b, want)
		}
	}
	for p := range files {
		if _, ok := got[p]; !ok {
			t.Errorf("the commit does not hold %s", p)
		}
	}
}

// commitWithout makes a commit object with the test's own git (not the worker), from the
// commit's tree, with the given parent ("" for none); it moves no ref.
func commitWithout(t testing.TB, dir, from, parent string) string {
	t.Helper()
	treeID := strings.TrimSpace(mustGit(t, dir, "rev-parse", from+"^{tree}"))
	args := []string{"commit-tree", treeID, "-m", "not the worker's"}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	return strings.TrimSpace(mustGit(t, dir, args...))
}

// overwriteObject replaces the loose object file of id with another commit's content, so the
// object keeps its ID and git reads the planted content (git does not check the hash on read).
func overwriteObject(t testing.TB, dir, id, content string) {
	t.Helper()
	p := filepath.Join(dir, ".git", "objects", id[:2], id[2:])
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the commit %s is not a loose object: %v", id, err)
	}
	var b bytes.Buffer
	z := zlib.NewWriter(&b)
	fmt.Fprintf(z, "commit %d\x00%s", len(content), content)
	z.Close()
	os.Chmod(p, 0o644)
	if err := os.WriteFile(p, b.Bytes(), 0o444); err != nil {
		t.Fatal(err)
	}
}

// commitMessage is the message the contract gives for an accepted or reused step.
func commitMessage(run, stepName string, round, attempt int, reusedFrom string) string {
	var b strings.Builder
	if reusedFrom != "" {
		fmt.Fprintf(&b, "Step %s reused in %s\n\n", stepName, run)
	} else {
		fmt.Fprintf(&b, "Step %s accepted in %s\n\n", stepName, run)
	}
	fmt.Fprintf(&b, "Run: %s\nStep: %s\n", run, stepName)
	if round > 0 {
		fmt.Fprintf(&b, "Round: %d\n", round)
	}
	if reusedFrom != "" {
		fmt.Fprintf(&b, "Reused-from: %s\n", reusedFrom)
	} else {
		fmt.Fprintf(&b, "Attempt: %d\n", attempt)
	}
	return b.String()
}

// wantCommitObject fails unless the commit object is exactly the one the contract gives.
func wantCommitObject(t testing.TB, dir, id, parent, at, msg string) {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, at)
	if err != nil {
		t.Fatal(err)
	}
	treeID := strings.TrimSpace(mustGit(t, dir, "rev-parse", id+"^{tree}"))
	var want strings.Builder
	fmt.Fprintf(&want, "tree %s\n", treeID)
	if parent != "" {
		fmt.Fprintf(&want, "parent %s\n", parent)
	}
	who := fmt.Sprintf("agentic-platform <noreply@localhost> %d +0000", ts.Unix())
	fmt.Fprintf(&want, "author %s\ncommitter %s\n\n%s", who, who, msg)
	if got := rawCommit(t, dir, id); got != want.String() {
		t.Errorf("commit %s:\n got %q\nwant %q", id, got, want.String())
	}
}

// ---- planted commands ----

// marker is a path that must never appear, and a shell script that creates it.
func marker(t testing.TB, name string) (path, script string) {
	t.Helper()
	d := t.TempDir()
	path = filepath.Join(d, name+".ran")
	script = filepath.Join(d, name+".sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+path+"'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, script
}

func neverRan(t testing.TB, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("a planted command ran: %s exists", filepath.Base(p))
		}
	}
}

// ---- a git wrapper (Config.Git) ----

// gitWrapper is a git binary that logs each invocation (the three environment settings, then
// each argument on its own line) and, for the subcommand sub, does what action says: "fail"
// exits 1 without running git, "sleep" runs git and then sleeps 5 s. Empty sub runs every call.
func gitWrapper(t testing.TB, sub, action string) (bin, log string) {
	t.Helper()
	real := gitBin(t)
	d := t.TempDir()
	bin, log = filepath.Join(d, "git"), filepath.Join(d, "calls.log")
	var on string
	switch action {
	case "fail":
		on = "exit 1"
	case "sleep":
		on = "\"" + real + "\" \"$@\"; rc=$?; sleep 5; exit $rc"
	}
	s := "#!/bin/sh\n" +
		"{ echo CALL; echo \"ENV:${GIT_CONFIG_NOSYSTEM-unset}:${GIT_CONFIG_GLOBAL-unset}:${GIT_NO_REPLACE_OBJECTS-unset}\"; " +
		"for a in \"$@\"; do printf 'ARG:%s\\n' \"$a\"; done; echo END; } >> '" + log + "'\n"
	if sub != "" {
		s += "for a in \"$@\"; do case \"$a\" in " + sub + ") " + on + ";; esac; done\n"
	}
	s += "exec \"" + real + "\" \"$@\"\n"
	if err := os.WriteFile(bin, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

type gitCall struct {
	env  string
	args []string
}

func gitCalls(t testing.TB, log string) []gitCall {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		return nil
	}
	var calls []gitCall
	var cur *gitCall
	for _, l := range strings.Split(string(b), "\n") {
		switch {
		case l == "CALL":
			calls = append(calls, gitCall{})
			cur = &calls[len(calls)-1]
		case cur != nil && strings.HasPrefix(l, "ENV:"):
			cur.env = l
		case cur != nil && strings.HasPrefix(l, "ARG:"):
			cur.args = append(cur.args, strings.TrimPrefix(l, "ARG:"))
		}
	}
	return calls
}
