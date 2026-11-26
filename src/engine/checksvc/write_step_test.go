package checksvc_test

// Feature 011, T004: failing tests, by a separate author, for the project worker's writeStep
// (POST /project-versions/write), written from the check contract alone,
// src/check/api/preview/2026-11-13-preview/openapi.yaml (the operation's description, Version
// history, The project worker's record, Project paths, Writable paths, x-limits) and from
// specs/011-capture-pattern/contracts/captured-patterns.md ("Check API: writeStep").
//
// Requests are plain JSON maps, as in the other project worker tests; responses are decoded
// strictly into the generated gen.WrittenFiles. The limits are read from the contract's
// `x-limits`, not written here as literals. The existing helpers are used unchanged.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/agentic-platform/check/api/gen"
	"go.yaml.in/yaml/v3"
)

const writePath = "/project-versions/write"

// ---- the limits, from the contract ----

type writeLimits struct {
	ProjectFilesMax int   `yaml:"project-files-max"`
	StepFilesMax    int64 `yaml:"step-files-max-bytes"`
	CommitBodyMax   int64 `yaml:"commit-request-body-max-bytes"`
	RequestBodyMax  int64 `yaml:"request-body-max-bytes"`
	PathMaxChars    int   `yaml:"path-max-chars"`
}

func contractLimits(t testing.TB) writeLimits {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "check", "api", "preview", "2026-11-13-preview", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Limits writeLimits `yaml:"x-limits"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	l := doc.Limits
	if l.ProjectFilesMax < 1 || l.StepFilesMax < 1 || l.CommitBodyMax < 1 || l.RequestBodyMax < 1 || l.PathMaxChars < 1 {
		t.Fatalf("the contract's x-limits are not read: %+v", l)
	}
	if l.CommitBodyMax <= l.RequestBodyMax {
		t.Fatalf("the contract's commit-request-body-max-bytes must exceed request-body-max-bytes: %+v", l)
	}
	return l
}

// ---- bodies and answers ----

// wfile is one entry of `files`: its path, the SHA-256 of its bytes and the bytes in base64.
func wfile(path, content string) map[string]any {
	return map[string]any{"path": path, "sha256": sum([]byte(content)), "content": b64([]byte(content))}
}

// writeBody is a writeStep body: the attempt, `before` and `files` (an empty list, never null).
func writeBody(st map[string]any, before []pf, files ...map[string]any) map[string]any {
	b := common(pwProject, st, before)
	fs := make([]map[string]any, 0, len(files))
	fs = append(fs, files...)
	b["files"] = fs
	return b
}

// wantWritten fails unless r is a 200 WrittenFiles (both lists present, even when empty) and
// returns the two lists as strings.
func wantWritten(t testing.TB, r resp) (written, removed []string) {
	t.Helper()
	if r.status != 200 {
		t.Fatalf("writeStep: want 200, got %d: %.600s", r.status, r.body)
	}
	var v gen.WrittenFiles
	if err := strictJSON(r.body, &v); err != nil || v.Written == nil || v.Removed == nil {
		t.Fatalf("not a WrittenFiles with written and removed: %v %.400s", err, r.body)
	}
	written, removed = []string{}, []string{}
	for _, p := range v.Written {
		written = append(written, string(p))
	}
	for _, p := range v.Removed {
		removed = append(removed, string(p))
	}
	return written, removed
}

func sameList(a, b []string) bool {
	return strings.Join(a, "\n") == strings.Join(b, "\n") && len(a) == len(b)
}

func wantLists(t testing.TB, r resp, written, removed []string) {
	t.Helper()
	w, rm := wantWritten(t, r)
	if !sameList(w, written) {
		t.Errorf("written: %q, want %q", w, written)
	}
	if !sameList(rm, removed) {
		t.Errorf("removed: %q, want %q", rm, removed)
	}
}

// ---- a project with two committed steps ----

// wsScene is a project worker whose last commit holds contract-check's two files and the tests
// step's four files; before is that commit's file list.
type wsScene struct {
	*pw
	v1, v2 pwVersion
	before []pf
	// committed is every file of the last commit, with its bytes.
	committed map[string][]byte
}

func newWS(t testing.TB) *wsScene {
	t.Helper()
	w := newPW(t)
	v1, _ := w.firstStep(t)
	write(t, w.dir, "tests/a_test.go", []byte("package tests // a\n"))
	write(t, w.dir, "tests/b_test.go", []byte("package tests // b\n"))
	write(t, w.dir, "tests/sub/z_test.go", []byte("package sub // z\n"))
	write(t, w.dir, "tests/Y_test.go", []byte("package tests // Y\n"))
	v2 := wantVersion(t, post(t, w.base, commitPath, commitBody(testsStep(), v1.ProjectFiles, projectFiles(t, w.dir), "RUN-1", 1, "2026-11-13T10:00:02Z")))
	return &wsScene{pw: w, v1: v1, v2: v2, before: v2.ProjectFiles, committed: contents(t, w.dir)}
}

// wantCommitted fails unless the folder holds exactly want, byte for byte.
func wantFolder(t testing.TB, dir string, want map[string][]byte) {
	t.Helper()
	got := contents(t, dir)
	for p, b := range want {
		g, ok := got[p]
		if !ok {
			t.Errorf("the folder must hold %s", p)
		} else if !bytes.Equal(g, b) {
			t.Errorf("%s holds %q, want %q", p, g, b)
		}
	}
	for p := range got {
		if _, ok := want[p]; !ok {
			t.Errorf("the folder must not hold %s", p)
		}
	}
}

// withFiles is the committed files with the tests/** ones replaced by tests.
func (s *wsScene) withTests(tests map[string]string) map[string][]byte {
	m := map[string][]byte{}
	for p, b := range s.committed {
		if !strings.HasPrefix(p, "tests/") {
			m[p] = b
		}
	}
	for p, c := range tests {
		m[p] = []byte(c)
	}
	return m
}

// ---- the tests ----

// T004: no files writes nothing: 200 with empty `written` and `removed` (both present as [],
// never null) when the step's writable entries hold nothing in before; the writable files of
// before are removed otherwise, and the folders stay.
func TestWriteStepWithNoFiles(t *testing.T) {
	t.Run("nothing to remove", func(t *testing.T) {
		s := newWS(t)
		// The step writes `review/**`, which the last commit holds nothing under (a clean review).
		st := step("review", []string{"review/**"}, []string{"findings", "witnesses"}, nil, nil)
		snap := s.state(t)
		r := post(t, s.base, writePath, writeBody(st, s.before))
		if r.status == 200 && (!strings.Contains(string(r.body), `"written":[]`) || !strings.Contains(string(r.body), `"removed":[]`)) {
			t.Errorf("empty lists are [], never null or missing: %s", r.body)
		}
		wantLists(t, r, []string{}, []string{})
		s.unchanged(t, snap, "a writeStep with no files over a step whose entries hold nothing")
	})
	t.Run("the step's files are removed", func(t *testing.T) {
		s := newWS(t)
		os.MkdirAll(filepath.Join(s.dir, "tests", "empty"), 0o755)
		write(t, s.dir, "tests/stray_test.go", []byte("not in before\n"))
		wantLists(t, post(t, s.base, writePath, writeBody(testsStep(), s.before)),
			[]string{}, []string{"tests/Y_test.go", "tests/a_test.go", "tests/b_test.go", "tests/sub/z_test.go"})
		wantFolder(t, s.dir, s.withTests(nil))
		for _, d := range []string{"tests", "tests/sub", "tests/empty"} {
			if fi, err := os.Lstat(filepath.Join(s.dir, d)); err != nil || !fi.IsDir() {
				t.Errorf("no folder is removed: %s is gone", d)
			}
		}
	})
	t.Run("a step with no writable entry", func(t *testing.T) {
		s := newWS(t)
		snap := s.state(t)
		wantLists(t, post(t, s.base, writePath, writeBody(step("review", nil, []string{"findings"}, nil, nil), s.before)), []string{}, []string{})
		s.unchanged(t, snap, "a writeStep over a step that writes nothing")
	})
}

// T004: it writes the files under the step's writable entries only: the listed files exactly (in
// place, with their bytes), every other file matching the entries removed (those of `before`
// are listed in `removed`, a stray one not), nothing outside the entries touched, nothing
// committed; `written` and `removed` are in byte order whatever the order of `files`.
func TestWriteStepWritesOnlyTheStepsWritableEntries(t *testing.T) {
	s := newWS(t)
	commits := commitCount(t, s.dir)
	head := mainAt(t, s.dir)
	gitSnap := snapshot(t, filepath.Join(s.dir, ".git"))
	histSnap := snapshot(t, s.history)

	// What a worker left before: a stray file and an edited one under the entries, an empty folder.
	write(t, s.dir, "tests/stray_test.go", []byte("not in before\n"))
	write(t, s.dir, "tests/a_test.go", []byte("edited on disk\n"))
	os.MkdirAll(filepath.Join(s.dir, "tests", "empty"), 0o755)

	r := post(t, s.base, writePath, writeBody(testsStep(), s.before,
		wfile("tests/new_test.go", "package tests // new\n"), // sent out of byte order
		wfile("tests/Zed_test.go", "package tests // Zed\n"),
		wfile("tests/b_test.go", "package tests // b changed\n"),
	))
	wantLists(t, r,
		[]string{"tests/Zed_test.go", "tests/b_test.go", "tests/new_test.go"},
		[]string{"tests/Y_test.go", "tests/a_test.go", "tests/sub/z_test.go"})
	wantFolder(t, s.dir, s.withTests(map[string]string{
		"tests/Zed_test.go": "package tests // Zed\n",
		"tests/b_test.go":   "package tests // b changed\n",
		"tests/new_test.go": "package tests // new\n",
	}))
	for _, d := range []string{"tests", "tests/sub", "tests/empty"} {
		if fi, err := os.Lstat(filepath.Join(s.dir, d)); err != nil || !fi.IsDir() {
			t.Errorf("no folder is removed: %s is gone", d)
		}
	}
	// Nothing is committed.
	if n := commitCount(t, s.dir); n != commits {
		t.Errorf("writeStep commits nothing: %d commits, want %d", n, commits)
	}
	if mainAt(t, s.dir) != head {
		t.Error("main must still name the last commit")
	}
	if d := sameSnapshot(gitSnap, snapshot(t, filepath.Join(s.dir, ".git"))); d != "" {
		t.Errorf("writeStep does not change the version history: %s", d)
	}
	if d := sameSnapshot(histSnap, snapshot(t, s.history)); d != "" {
		t.Errorf("writeStep does not change the record: %s", d)
	}
}

// T004: with exact writable entries (no `dir/**`) only those paths are written and removed.
func TestWriteStepWithExactEntries(t *testing.T) {
	s := newWS(t)
	r := post(t, s.base, writePath, writeBody(ccStep(), s.before, wfile("api/openapi.yaml", "openapi: 3.1.0 # filled\n")))
	wantLists(t, r, []string{"api/openapi.yaml"}, []string{"features/slice.feature"})
	want := map[string][]byte{"api/openapi.yaml": []byte("openapi: 3.1.0 # filled\n")}
	for p, b := range s.committed {
		if strings.HasPrefix(p, "tests/") {
			want[p] = b
		}
	}
	wantFolder(t, s.dir, want)
	// A path the entries do not name, though beside one, is refused.
	snap := s.state(t)
	wantProblem(t, post(t, s.base, writePath, writeBody(ccStep(), s.before, wfile("api/other.yaml", "x\n"))), 400, "InvalidRequest")
	s.unchanged(t, snap, "a 400")
}

// T004: restoreStep afterwards removes what writeStep wrote, as it removes any uncommitted change,
// and writes back what it removed.
func TestWriteStepThenRestoreStepRemovesWhatItWrote(t *testing.T) {
	s := newWS(t)
	wantLists(t, post(t, s.base, writePath, writeBody(testsStep(), s.before,
		wfile("tests/new_test.go", "package tests // new\n"),
		wfile("tests/Zed_test.go", "package tests // Zed\n"),
		wfile("tests/b_test.go", "package tests // b changed\n"))),
		[]string{"tests/Zed_test.go", "tests/b_test.go", "tests/new_test.go"},
		[]string{"tests/Y_test.go", "tests/a_test.go", "tests/sub/z_test.go"})
	r := wantRestored(t, post(t, s.base, restorePath, restoreBody(testsStep(), s.before)))
	want := []string{"tests/Y_test.go", "tests/Zed_test.go", "tests/a_test.go", "tests/b_test.go", "tests/new_test.go", "tests/sub/z_test.go"}
	if !sameList(r.Restored, want) {
		t.Errorf("restored must list what writeStep wrote, removed or changed: %q, want %q", r.Restored, want)
	}
	if !samePF(r.ProjectFiles, s.before) {
		t.Errorf("the folder must hold before again: %v", r.ProjectFiles)
	}
	wantFolder(t, s.dir, s.committed)
}

// T004: a repeated request answers the same and leaves the same files; a repeat converges a
// folder a stopped call left partly written.
func TestWriteStepRepeatAnswersTheSameAndLeavesTheSameFiles(t *testing.T) {
	s := newWS(t)
	body := writeBody(testsStep(), s.before,
		wfile("tests/new_test.go", "package tests // new\n"),
		wfile("tests/b_test.go", "package tests // b changed\n"))
	r1 := post(t, s.base, writePath, body)
	w1, rm1 := wantWritten(t, r1)
	snap := s.state(t)
	for i := 0; i < 2; i++ {
		r := post(t, s.base, writePath, body)
		w, rm := wantWritten(t, r)
		if !sameList(w, w1) || !sameList(rm, rm1) {
			t.Errorf("repeat %d answers %q %q, want %q %q", i+1, w, rm, w1, rm1)
		}
		if string(r.body) != string(r1.body) {
			t.Errorf("repeat %d answers another body: %s, want %s", i+1, r.body, r1.body)
		}
		s.unchanged(t, snap, "a repeated writeStep")
	}

	// A call stopped half way: one file missing, one holding other bytes, a stray one.
	os.Remove(filepath.Join(s.dir, "tests", "new_test.go"))
	write(t, s.dir, "tests/b_test.go", []byte("half written"))
	write(t, s.dir, "tests/stray_test.go", []byte("x\n"))
	r := post(t, s.base, writePath, body)
	if string(r.body) != string(r1.body) {
		t.Errorf("a repeat over a half-written folder answers the same: %s, want %s", r.body, r1.body)
	}
	wantFolder(t, s.dir, s.withTests(map[string]string{
		"tests/new_test.go": "package tests // new\n",
		"tests/b_test.go":   "package tests // b changed\n",
	}))
}

// T004: 409 ProjectStateDiffers when files outside the writable entries differ from `before`
// (changed, missing or extra), or when the last commit does not hold `before`; nothing changes.
func TestWriteStepRefusesAStateThatDiffers(t *testing.T) {
	good := wfile("tests/n_test.go", "package tests // n\n")
	other := func(p []pf, i int) []pf { // one hash changed
		c := append([]pf{}, p...)
		c[i] = pf{c[i].Path, sum([]byte("other"))}
		return c
	}
	idx := func(s *wsScene, path string) int {
		for i, f := range s.before {
			if f.Path == path {
				return i
			}
		}
		t.Fatalf("%s is not in before", path)
		return -1
	}
	cases := map[string]struct {
		prepare func(s *wsScene) []pf // changes the folder, returns before
		names   string                // a path the detail names, when the contract says it does
	}{
		"a file outside the entries changed": {func(s *wsScene) []pf {
			write(t, s.dir, "api/openapi.yaml", []byte("changed\n"))
			return s.before
		}, "api/openapi.yaml"},
		"a file outside the entries removed": {func(s *wsScene) []pf {
			os.Remove(filepath.Join(s.dir, "features", "slice.feature"))
			return s.before
		}, "features/slice.feature"},
		"a file outside the entries added": {func(s *wsScene) []pf {
			write(t, s.dir, "README.md", []byte("new\n"))
			return s.before
		}, "README.md"},
		"two files differ: the first in byte order is named": {func(s *wsScene) []pf {
			write(t, s.dir, "features/slice.feature", []byte("changed\n"))
			write(t, s.dir, "api/openapi.yaml", []byte("changed\n"))
			return s.before
		}, "api/openapi.yaml"},
		"a file outside the entries, and one inside, changed": {func(s *wsScene) []pf {
			write(t, s.dir, "tests/a_test.go", []byte("changed\n"))
			write(t, s.dir, "go.mod", []byte("module x\n"))
			return s.before
		}, "go.mod"},
		"before names another hash outside the entries": {func(s *wsScene) []pf {
			return other(s.before, idx(s, "api/openapi.yaml"))
		}, ""},
		"before names another hash inside the entries": {func(s *wsScene) []pf {
			return other(s.before, idx(s, "tests/a_test.go"))
		}, ""},
		"before lists one file more": {func(s *wsScene) []pf {
			return append(append([]pf{}, s.before...), pf{"tests/z_test.go", sum([]byte("x"))})
		}, ""},
		"before lists one file less": {func(s *wsScene) []pf {
			i := idx(s, "tests/a_test.go")
			return append(append([]pf{}, s.before[:i]...), s.before[i+1:]...)
		}, ""},
		"before is an earlier commit's files": {func(s *wsScene) []pf { return s.v1.ProjectFiles }, ""},
		"before is empty":                     {func(s *wsScene) []pf { return nil }, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newWS(t)
			before := c.prepare(s)
			snap := s.state(t)
			p := wantProblem(t, post(t, s.base, writePath, writeBody(testsStep(), before, good)), 409, "ProjectStateDiffers")
			if c.names != "" && !strings.Contains(p.Detail, c.names) {
				t.Errorf("the detail must name the first path that differs, %s: %q", c.names, p.Detail)
			}
			s.unchanged(t, snap, "a 409 writeStep")
		})
	}
}

// T004 / contract: a file inside the entries that differs from `before` is not a difference
// (the step's own attempt may have left it); only the files outside are compared.
func TestWriteStepIgnoresDifferencesInsideTheEntries(t *testing.T) {
	s := newWS(t)
	write(t, s.dir, "tests/a_test.go", []byte("edited by the attempt\n"))
	os.Remove(filepath.Join(s.dir, "tests", "b_test.go"))
	write(t, s.dir, "tests/stray_test.go", []byte("x\n"))
	wantLists(t, post(t, s.base, writePath, writeBody(testsStep(), s.before, wfile("tests/n_test.go", "package tests // n\n"))),
		[]string{"tests/n_test.go"}, []string{"tests/Y_test.go", "tests/a_test.go", "tests/b_test.go", "tests/sub/z_test.go"})
	wantFolder(t, s.dir, s.withTests(map[string]string{"tests/n_test.go": "package tests // n\n"}))
}

// T004 / contract (2): with no record (or a claimed one) writeStep needs no repository; `before`
// must be empty, otherwise 409 ProjectStateDiffers; it never creates the repository.
func TestWriteStepWithNoRecord(t *testing.T) {
	t.Run("before empty: writes, needs no repository", func(t *testing.T) {
		w := newPW(t)
		r := post(t, w.base, writePath, writeBody(ccStep(), nil, wfile("api/openapi.yaml", "openapi: 3.1.0\n"), wfile("features/slice.feature", "Feature: F\n")))
		wantLists(t, r, []string{"api/openapi.yaml", "features/slice.feature"}, []string{})
		wantFolder(t, w.dir, map[string][]byte{"api/openapi.yaml": []byte("openapi: 3.1.0\n"), "features/slice.feature": []byte("Feature: F\n")})
		if es, _ := os.ReadDir(filepath.Join(w.dir, ".git")); len(es) != 0 {
			t.Error("writeStep never creates the repository")
		}
		if es, _ := os.ReadDir(w.history); len(es) != 0 {
			t.Error("writeStep never writes a record")
		}
	})
	t.Run("before not empty", func(t *testing.T) {
		w := newPW(t)
		write(t, w.dir, "api/openapi.yaml", []byte("x\n"))
		snap := w.state(t)
		wantProblem(t, post(t, w.base, writePath, writeBody(ccStep(), []pf{{"api/openapi.yaml", sum([]byte("x\n"))}}, wfile("api/openapi.yaml", "y\n"))), 409, "ProjectStateDiffers")
		w.unchanged(t, snap, "a writeStep with no record and a non-empty before")
	})
	t.Run("a file outside the entries with before empty", func(t *testing.T) {
		w := newPW(t)
		write(t, w.dir, "README.md", []byte("x\n"))
		snap := w.state(t)
		wantProblem(t, post(t, w.base, writePath, writeBody(ccStep(), nil, wfile("api/openapi.yaml", "y\n"))), 409, "ProjectStateDiffers")
		w.unchanged(t, snap, "a writeStep over a folder holding a file outside the entries")
	})
}

// T004 / contract (2): the history check, as restoreStep does it: a `.git` the worker did not
// make is 409 HistoryChanged and nothing changes.
func TestWriteStepRefusesAChangedHistory(t *testing.T) {
	good := wfile("tests/n_test.go", "package tests // n\n")
	tampers := map[string]func(t *testing.T, s *wsScene){
		"HEAD names another branch": func(t *testing.T, s *wsScene) {
			os.WriteFile(filepath.Join(s.dir, ".git", "HEAD"), []byte("ref: refs/heads/other\n"), 0o644)
		},
		"the branch moved to a commit the worker did not make": func(t *testing.T, s *wsScene) {
			setMain(t, s.dir, commitWithout(t, s.dir, s.v2.Commit, s.v2.Commit))
		},
		"a commondir": func(t *testing.T, s *wsScene) { write(t, s.dir, ".git/commondir", []byte("../other\n")) },
		"a changed config": func(t *testing.T, s *wsScene) {
			f, err := os.OpenFile(filepath.Join(s.dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString("[user]\n\tname = x\n"); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, tamper := range tampers {
		t.Run(name, func(t *testing.T) {
			s := newWS(t)
			tamper(t, s)
			snap := s.state(t)
			wantProblem(t, post(t, s.base, writePath, writeBody(testsStep(), s.before, good)), 409, "HistoryChanged")
			s.unchanged(t, snap, "a writeStep over a changed history")
		})
	}
	// Contract order: the path check (1) comes before the history check (2).
	t.Run("a bad path comes first", func(t *testing.T) {
		s := newWS(t)
		tampers["HEAD names another branch"](t, s)
		snap := s.state(t)
		wantProblem(t, post(t, s.base, writePath, writeBody(testsStep(), s.before, wfile("api/openapi.yaml", "x\n"))), 400, "InvalidRequest")
		s.unchanged(t, snap, "a 400")
	})
}

// T004: a path matching no writable entry is 400 InvalidRequest naming it, and a path that is
// not a ProjectPath (`..`, absolute, a `.git` part, a repeated slash, a space, too long) is 400
// too; a path sent twice is 400; nothing is read or changed, the good files of the same request
// included.
func TestWriteStepRefusesPathsOutsideTheWritableEntries(t *testing.T) {
	l := contractLimits(t)
	outside := []string{"api/openapi.yaml", "go.mod", "tests", "testsx/a_test.go", "Tests/a_test.go", "internal/x.go", "test/a_test.go"}
	notPaths := []string{
		"tests/../api/openapi.yaml", "../tests/a_test.go", "tests/..", "/tests/a_test.go", "tests//a_test.go", "tests/./a_test.go",
		"tests/.git/x", "tests/.GIT/x", "tests/.Git", "tests/a b_test.go", "tests/", "", "tests/é.go", "tests/a\x00.go", "tests\\a_test.go",
		"tests/" + strings.Repeat("a", l.PathMaxChars-len("tests/")+1), // one character too long
	}
	run := func(name, path string, names bool) {
		t.Run(name, func(t *testing.T) {
			s := newWS(t)
			snap := s.state(t)
			for _, files := range [][]map[string]any{
				{wfile(path, "x\n")},
				{wfile("tests/ok_test.go", "ok\n"), wfile(path, "x\n")},
				{wfile(path, "x\n"), wfile("tests/ok_test.go", "ok\n")},
			} {
				r := post(t, s.base, writePath, writeBody(testsStep(), s.before, files...))
				p := wantProblem(t, r, 400, "InvalidRequest")
				if names && !strings.Contains(p.Detail, path) {
					t.Errorf("the detail must name %q: %q", path, p.Detail)
				}
				s.unchanged(t, snap, "a 400 writeStep")
			}
		})
	}
	for i, p := range outside {
		run(fmt.Sprintf("outside %d %s", i, p), p, true)
	}
	for i, p := range notPaths {
		run(fmt.Sprintf("not a ProjectPath %d %q", i, p), p, false)
	}
	t.Run("a step that writes nothing", func(t *testing.T) {
		s := newWS(t)
		snap := s.state(t)
		p := wantProblem(t, post(t, s.base, writePath, writeBody(step("review", nil, []string{"findings"}, nil, nil), s.before, wfile("review/w_test.go", "x\n"))), 400, "InvalidRequest")
		if !strings.Contains(p.Detail, "review/w_test.go") {
			t.Errorf("the detail must name the path: %q", p.Detail)
		}
		s.unchanged(t, snap, "a 400")
	})
	t.Run("a path sent twice", func(t *testing.T) {
		s := newWS(t)
		snap := s.state(t)
		wantProblem(t, post(t, s.base, writePath, writeBody(testsStep(), s.before, wfile("tests/d_test.go", "one\n"), wfile("tests/d_test.go", "two\n"))), 400, "InvalidRequest")
		wantProblem(t, post(t, s.base, writePath, writeBody(testsStep(), s.before, wfile("tests/d_test.go", "same\n"), wfile("tests/d_test.go", "same\n"))), 400, "InvalidRequest")
		s.unchanged(t, snap, "a 400")
	})
	t.Run("a path of exactly path-max-chars is accepted", func(t *testing.T) {
		s := newWS(t)
		p := "tests/" + strings.Repeat("a", l.PathMaxChars-len("tests/"))
		wantLists(t, post(t, s.base, writePath, writeBody(testsStep(), s.before, wfile(p, "x\n"))), []string{p},
			[]string{"tests/Y_test.go", "tests/a_test.go", "tests/b_test.go", "tests/sub/z_test.go"})
	})
	t.Run("a name that looks like an option", func(t *testing.T) {
		s := newWS(t)
		wantLists(t, post(t, s.base, writePath, writeBody(testsStep(), s.before, wfile("tests/-rf", "x\n"), wfile("tests/--help", "y\n"))),
			[]string{"tests/--help", "tests/-rf"}, []string{"tests/Y_test.go", "tests/a_test.go", "tests/b_test.go", "tests/sub/z_test.go"})
		if string(read(t, s.dir, "tests/-rf")) != "x\n" {
			t.Error("a file named like an option is written as any file")
		}
	})
}

// T004: no symbolic link is followed. A folder on a path's way that is a symbolic link, or the
// project holding one, is 422 UnreadableProject; whatever a link points to is never touched.
func TestWriteStepFollowsNoSymbolicLink(t *testing.T) {
	good := wfile("tests/n_test.go", "package tests // n\n")
	setup := func(t *testing.T) (*wsScene, string, map[string]string) {
		s := newWS(t)
		out := t.TempDir()
		write(t, out, "target.txt", []byte("outside\n"))
		write(t, out, "dir/keep.txt", []byte("outside\n"))
		return s, out, snapshot(t, out)
	}
	cases := map[string]struct {
		prepare func(t *testing.T, s *wsScene, out string) []map[string]any
		want422 bool // false: 422 UnreadableProject or 200, the contract does not say which
	}{
		"a symlinked folder inside the entries, a file below it": {func(t *testing.T, s *wsScene, out string) []map[string]any {
			os.Symlink(filepath.Join(out, "dir"), filepath.Join(s.dir, "tests", "link"))
			return []map[string]any{wfile("tests/link/x.go", "pwned\n")}
		}, true},
		"the entries' own folder is a symbolic link": {func(t *testing.T, s *wsScene, out string) []map[string]any {
			if err := os.RemoveAll(filepath.Join(s.dir, "tests")); err != nil {
				t.Fatal(err)
			}
			os.Symlink(filepath.Join(out, "dir"), filepath.Join(s.dir, "tests"))
			return []map[string]any{wfile("tests/x.go", "pwned\n")}
		}, true},
		"a symlinked folder outside the entries": {func(t *testing.T, s *wsScene, out string) []map[string]any {
			os.Symlink(filepath.Join(out, "dir"), filepath.Join(s.dir, "docs"))
			return []map[string]any{good}
		}, true},
		"a symbolic link to a file outside the entries": {func(t *testing.T, s *wsScene, out string) []map[string]any {
			os.Symlink(filepath.Join(out, "target.txt"), filepath.Join(s.dir, "README.md"))
			return []map[string]any{good}
		}, true},
		"a symbolic link at a path to write": {func(t *testing.T, s *wsScene, out string) []map[string]any {
			os.Symlink(filepath.Join(out, "target.txt"), filepath.Join(s.dir, "tests", "link_test.go"))
			return []map[string]any{wfile("tests/link_test.go", "pwned\n")}
		}, false},
		"a symbolic link at a path to remove": {func(t *testing.T, s *wsScene, out string) []map[string]any {
			os.Remove(filepath.Join(s.dir, "tests", "a_test.go"))
			os.Symlink(filepath.Join(out, "target.txt"), filepath.Join(s.dir, "tests", "a_test.go"))
			return []map[string]any{good}
		}, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, out, outSnap := setup(t)
			files := c.prepare(t, s, out)
			r := post(t, s.base, writePath, writeBody(testsStep(), s.before, files...))
			if c.want422 {
				wantProblem(t, r, 422, "UnreadableProject")
			} else if r.status != 200 {
				wantProblem(t, r, 422, "UnreadableProject")
			}
			if d := sameSnapshot(outSnap, snapshot(t, out)); d != "" {
				t.Errorf("no link is followed: outside the project %s", d)
			}
		})
	}
}

// T004: the files limits. `files` holds at most x-limits.project-files-max entries (more is the
// schema's 400 InvalidRequest) and x-limits.step-files-max-bytes of file bytes together (more is
// 422 StepFilesTooLarge, before anything is written).
func TestWriteStepFilesLimits(t *testing.T) {
	l := contractLimits(t)
	t.Run("project-files-max entries are written, one more is 400", func(t *testing.T) {
		many := func(n int) []map[string]any {
			fs := make([]map[string]any, n)
			for i := range fs {
				fs[i] = wfile(fmt.Sprintf("tests/many/f%06d", i), "x")
			}
			return fs
		}
		// A new project, so the folder holds exactly project-files-max files afterwards.
		w := newPW(t)
		w1, rm := wantWritten(t, post(t, w.base, writePath, writeBody(testsStep(), nil, many(l.ProjectFilesMax)...)))
		if len(w1) != l.ProjectFilesMax || !sort.StringsAreSorted(w1) || len(rm) != 0 {
			t.Errorf("written holds %d paths in byte order, want %d; removed %q", len(w1), l.ProjectFilesMax, rm)
		}
		if n := len(projectFiles(t, w.dir)); n != l.ProjectFilesMax {
			t.Errorf("the folder holds %d files, want %d", n, l.ProjectFilesMax)
		}
		// One more entry: 400 and nothing is changed.
		s := newWS(t)
		snap := s.state(t)
		wantProblem(t, post(t, s.base, writePath, writeBody(testsStep(), s.before, many(l.ProjectFilesMax+1)...)), 400, "InvalidRequest")
		s.unchanged(t, snap, "a 400 writeStep")
	})
	t.Run("step-files-max-bytes of file bytes are written, one more is 422", func(t *testing.T) {
		s := newWS(t)
		big := func(n int64) string { return strings.Repeat("a", int(n)) }
		half := l.StepFilesMax / 2
		// One file of exactly the limit.
		r := post(t, s.base, writePath, writeBody(testsStep(), s.before, wfile("tests/big_test.go", big(l.StepFilesMax))))
		wantLists(t, r, []string{"tests/big_test.go"}, []string{"tests/Y_test.go", "tests/a_test.go", "tests/b_test.go", "tests/sub/z_test.go"})
		if fi, err := os.Stat(filepath.Join(s.dir, "tests", "big_test.go")); err != nil || fi.Size() != l.StepFilesMax {
			t.Errorf("the file of exactly the limit must be written whole: %v", err)
		}
		// The limit is on the bytes together: two files of exactly the limit together are accepted.
		wantRestored(t, post(t, s.base, restorePath, restoreBody(testsStep(), s.before)))
		wantLists(t, post(t, s.base, writePath, writeBody(testsStep(), s.before, wfile("tests/p1_test.go", big(half)), wfile("tests/p2_test.go", big(l.StepFilesMax-half)))),
			[]string{"tests/p1_test.go", "tests/p2_test.go"}, []string{"tests/Y_test.go", "tests/a_test.go", "tests/b_test.go", "tests/sub/z_test.go"})
		// One byte more, in one file and in two: 422 StepFilesTooLarge, nothing written or removed.
		wantRestored(t, post(t, s.base, restorePath, restoreBody(testsStep(), s.before)))
		snap := s.state(t)
		wantProblem(t, post(t, s.base, writePath, writeBody(testsStep(), s.before, wfile("tests/big_test.go", big(l.StepFilesMax+1)))), 422, "StepFilesTooLarge")
		s.unchanged(t, snap, "a 422 writeStep (one file)")
		wantProblem(t, post(t, s.base, writePath, writeBody(testsStep(), s.before, wfile("tests/small_test.go", "x\n"), wfile("tests/p1_test.go", big(half)), wfile("tests/p2_test.go", big(l.StepFilesMax-half)))), 422, "StepFilesTooLarge")
		s.unchanged(t, snap, "a 422 writeStep (several files)")
	})
}

// T004 / contract (4): every file's content is checked against its `sha256` before anything is
// written: 422 InputHashMismatch and nothing written or removed.
func TestWriteStepRefusesAContentThatIsNotItsHash(t *testing.T) {
	s := newWS(t)
	snap := s.state(t)
	bad := wfile("tests/bad_test.go", "package tests // bad\n")
	bad["sha256"] = sum([]byte("something else"))
	wantProblem(t, post(t, s.base, writePath, writeBody(testsStep(), s.before, wfile("tests/a_test.go", "fine\n"), bad)), 422, "InputHashMismatch")
	s.unchanged(t, snap, "a 422 writeStep")
}

// T004: a body up to x-limits.commit-request-body-max-bytes is accepted (above
// request-body-max-bytes, as for commitStep); one byte more is 400 InvalidRequest.
func TestWriteStepBodyLimit(t *testing.T) {
	l := contractLimits(t)
	s := newWS(t)
	raw, _ := json.Marshal(writeBody(testsStep(), s.before, wfile("tests/n_test.go", "package tests // n\n")))
	pad := func(n int64) []byte {
		return append(append([]byte{}, raw...), bytes.Repeat([]byte(" "), int(n)-len(raw))...)
	}
	send := func(b []byte) resp { return do(t, s.base, writePath, q, "Bearer "+token, "application/json", b) }
	if r := send(pad(l.RequestBodyMax + 1)); r.status != 200 {
		t.Errorf("writeStep must read a body above request-body-max-bytes: %d %.300s", r.status, r.body)
	}
	if r := send(pad(l.CommitBodyMax)); r.status != 200 {
		t.Errorf("writeStep must read a body of exactly %d bytes: %d %.300s", l.CommitBodyMax, r.status, r.body)
	}
	snap := s.state(t)
	wantProblem(t, send(pad(l.CommitBodyMax+1)), 400, "InvalidRequest")
	s.unchanged(t, snap, "a body above the limit")
	wantProblem(t, send(pad(l.CommitBodyMax*2)), 400, "InvalidRequest")
	s.unchanged(t, snap, "a body above the limit")
}

// T004: a project that does not exist, a name that is not a project name, and a step that is not
// a step are refused, with nothing changed.
func TestWriteStepRefusesAnUnknownProjectOrStep(t *testing.T) {
	s := newWS(t)
	snap := s.state(t)
	good := wfile("tests/n_test.go", "x\n")
	t.Run("a project with no folder", func(t *testing.T) {
		b := writeBody(testsStep(), nil, good)
		b["project"] = "no-such-project"
		wantProblem(t, post(t, s.base, writePath, b), 422, "UnreadableProject")
		if _, err := os.Lstat(filepath.Join(s.root, "no-such-project")); err == nil {
			t.Error("writeStep must not create a project folder")
		}
	})
	for _, name := range []string{"../p1", "P1", "p 1", "p1/", "", ".", "-p1"} {
		t.Run(fmt.Sprintf("project name %q", name), func(t *testing.T) {
			b := writeBody(testsStep(), s.before, good)
			b["project"] = name
			wantProblem(t, post(t, s.base, writePath, b), 400, "InvalidRequest")
		})
	}
	t.Run("a project that is a file", func(t *testing.T) {
		write(t, s.root, "afile", []byte("x\n"))
		b := writeBody(testsStep(), nil, good)
		b["project"] = "afile"
		wantProblem(t, post(t, s.base, writePath, b), 422, "UnreadableProject")
	})
	for name, mut := range map[string]func(b map[string]any){
		"no step":             func(b map[string]any) { delete(b, "step") },
		"a null step":         func(b map[string]any) { b["step"] = nil },
		"a step with no name": func(b map[string]any) { b["step"] = step("", []string{"tests/**"}, []string{"tests"}, nil, nil) },
		"a step name not a name": func(b map[string]any) {
			b["step"] = step("Bad Name!", []string{"tests/**"}, []string{"tests"}, nil, nil)
		},
		"a step with no writable": func(b map[string]any) { delete(b["step"].(map[string]any), "writable") },
		"a writable entry with a .git part": func(b map[string]any) {
			b["step"] = step("tests", []string{".git/**"}, []string{"tests"}, nil, nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := writeBody(testsStep(), s.before, good)
			mut(b)
			wantProblem(t, post(t, s.base, writePath, b), 400, "InvalidRequest")
		})
	}
	s.unchanged(t, snap, "refused writeStep calls")
}

// T004 / contract: a body that does not match the contract is 400 InvalidRequest and changes
// nothing: no `files`, null `files`, an unknown property, a path with no content, bad base64, a
// hash that is not a hash, a bad attempt or time limit.
func TestWriteStepRefusesABodyThatIsNotTheContracts(t *testing.T) {
	s := newWS(t)
	snap := s.state(t)
	file := func(mut func(f map[string]any)) func(b map[string]any) {
		return func(b map[string]any) {
			f := wfile("tests/n_test.go", "x\n")
			mut(f)
			b["files"] = []map[string]any{f}
		}
	}
	cases := map[string]func(b map[string]any){
		"no files":                   func(b map[string]any) { delete(b, "files") },
		"null files":                 func(b map[string]any) { b["files"] = nil },
		"an unknown property":        func(b map[string]any) { b["extra"] = 1 },
		"no before":                  func(b map[string]any) { delete(b, "before") },
		"attempt 0":                  func(b map[string]any) { b["attempt"] = 0 },
		"time limit 0":               func(b map[string]any) { b["time_limit_seconds"] = 0 },
		"no time limit":              func(b map[string]any) { delete(b, "time_limit_seconds") },
		"a file without content":     file(func(f map[string]any) { delete(f, "content") }),
		"a file without a hash":      file(func(f map[string]any) { delete(f, "sha256") }),
		"a file with another field":  file(func(f map[string]any) { f["mode"] = "0755" }),
		"content that is not base64": file(func(f map[string]any) { f["content"] = "!!not base64!!" }),
		"a hash in upper case":       file(func(f map[string]any) { f["sha256"] = strings.ToUpper(sum([]byte("x\n"))) }),
		"a hash too short":           file(func(f map[string]any) { f["sha256"] = "abc" }),
		"content as a number":        file(func(f map[string]any) { f["content"] = 5 }),
		"a file that is a string":    func(b map[string]any) { b["files"] = []string{"tests/n_test.go"} },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			b := writeBody(testsStep(), s.before)
			mut(b)
			wantProblem(t, post(t, s.base, writePath, b), 400, "InvalidRequest")
			s.unchanged(t, snap, "a 400 writeStep")
		})
	}
	t.Run("not JSON", func(t *testing.T) {
		wantProblem(t, do(t, s.base, writePath, q, "Bearer "+token, "application/json", []byte("{")), 400, "InvalidRequest")
	})
	t.Run("another content type", func(t *testing.T) {
		raw, _ := json.Marshal(writeBody(testsStep(), s.before))
		wantProblem(t, do(t, s.base, writePath, q, "Bearer "+token, "text/plain", raw), 400, "InvalidRequest")
	})
	s.unchanged(t, snap, "a 400 writeStep")
}

// T004 / contract: the engine's token and the api-version are required as for every operation.
func TestWriteStepRequiresTheTokenAndTheAPIVersion(t *testing.T) {
	s := newWS(t)
	snap := s.state(t)
	raw, _ := json.Marshal(writeBody(testsStep(), s.before, wfile("tests/n_test.go", "x\n")))
	wantProblem(t, do(t, s.base, writePath, q, "", "application/json", raw), 401, "Unauthorized")
	wantProblem(t, do(t, s.base, writePath, q, "Bearer wrong-token", "application/json", raw), 401, "Unauthorized")
	wantProblem(t, do(t, s.base, writePath, "", "Bearer "+token, "application/json", raw), 400, "MissingApiVersionParameter")
	wantProblem(t, do(t, s.base, writePath, "api-version=2000-01-01", "Bearer "+token, "application/json", raw), 400, "UnsupportedApiVersionValue")
	s.unchanged(t, snap, "a refused writeStep")
}

// T004 / contract: the check service and the procedure step workers are not the project worker:
// they answer writeStep 404 VersionHistoryNotServed and change nothing, as for the other history
// calls; the project worker answers runCheck, readStepFiles and runProcedure with their 404s.
func TestWriteStepIsServedByTheProjectWorkerOnly(t *testing.T) {
	s := newWS(t)
	body := writeBody(testsStep(), s.before, wfile("tests/n_test.go", "x\n"))
	for name, base := range map[string]string{
		"the check service":    serve(t, checkConfig(t, s.root)),
		"a procedure worker":   serve(t, workerConfig(t, s.root, "contract-check", "contract-check")),
		"a generate procedure": serve(t, workerConfig(t, s.root, "generate", "generate")),
	} {
		t.Run(name, func(t *testing.T) {
			snap := s.state(t)
			wantProblem(t, post(t, base, writePath, body), 404, "VersionHistoryNotServed")
			s.unchanged(t, snap, "a 404")
		})
	}
}
