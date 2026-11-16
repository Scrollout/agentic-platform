package checksvc_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// T053c: `.git` is not part of the project for any operation; the deployments that do not keep
// version histories; the commitStep body limit.

func noGitPath(t testing.TB, what string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if p == ".git" || strings.HasPrefix(p, ".git/") || strings.Contains(strings.ToLower("/"+p+"/"), "/.git/") {
			t.Errorf("%s lists %s: nothing under .git is part of the project", what, p)
		}
	}
}

func pfPaths(l []pf) []string {
	out := []string{}
	for _, f := range l {
		out = append(out, f.Path)
	}
	return out
}

func TestProjectWorkerGitIsNeverListed(t *testing.T) {
	w := newPW(t)
	v1, _ := w.firstStep(t)
	v2, _ := w.secondStep(t, v1.ProjectFiles)
	gitSnap := snapshot(t, filepath.Join(w.dir, ".git"))
	keepGit := func(what string) {
		t.Helper()
		if d := sameSnapshot(gitSnap, snapshot(t, filepath.Join(w.dir, ".git"))); d != "" {
			t.Errorf("%s changed .git: %s", what, d)
		}
	}

	check := serve(t, checkConfig(t, w.root))
	sf := readFiles(t, post(t, check, "/step-files", common(pwProject, testsStep(), v2.ProjectFiles)))
	noGitPath(t, "readStepFiles project_files", pfPaths(sf.ProjectFiles)...)
	noGitPath(t, "readStepFiles removed", sf.Removed...)
	for _, f := range sf.Files {
		noGitPath(t, "readStepFiles files", f.Path)
	}
	if !samePF(sf.ProjectFiles, v2.ProjectFiles) {
		t.Errorf("readStepFiles project_files must be the folder outside .git: %v", sf.ProjectFiles)
	}

	body := checkBody(t, w.dir, testsStep(), v2.ProjectFiles, "c", "e")
	v := runCheck(t, check, "ScopeKept", body)
	if !v.Passed {
		t.Errorf("ScopeKept judges nothing under .git: %s", v.Reason)
	}
	noGitPath(t, "runCheck project_files", pfPaths(v.ProjectFiles)...)
	if strings.Contains(v.Reason, ".git") {
		t.Errorf("runCheck names .git: %s", v.Reason)
	}

	worker := serve(t, workerConfig(t, w.root, "contract-check", "contract-check"))
	p := wantProcedure(t, post(t, worker, "/procedures/contract-check/run", ccBody(t, w.dir, string(readTestdata(t, "contract.yaml")), "Feature: F\n")), "contract-check")
	noGitPath(t, "runProcedure project_files", pfPaths(p.ProjectFiles)...)
	for _, o := range p.Outputs {
		for _, f := range o.Files {
			noGitPath(t, "runProcedure outputs", f.Path)
		}
	}
	keepGit("runCheck, readStepFiles and runProcedure")

	// Back to the last commit: restore lists nothing under .git either.
	r := wantRestored(t, post(t, w.base, restorePath, restoreBody(testsStep(), v2.ProjectFiles)))
	noGitPath(t, "restoreStep restored", r.Restored...)
	noGitPath(t, "restoreStep project_files", pfPaths(r.ProjectFiles)...)
	keepGit("restoreStep")
}

// A `writable` entry or a project file with a part `.git` in any letter case: 400, or 422 for
// such a file in the folder.
func TestProjectWorkerGitPartsAreNotProjectPaths(t *testing.T) {
	bad := []string{".git", ".git/config", ".GIT/x", "sub/.git", "sub/.Git/HEAD", "a/.gIT/b/c"}
	badWritable := []string{".git/**", ".GIT/**", "sub/.git/**", ".Git", "a/.git", "a/.gIt/b"}

	t.Run("in a request", func(t *testing.T) {
		w := newPW(t)
		v1, _ := w.firstStep(t)
		snap := w.state(t)
		// Fresh deployments per entry keep each under the per-token rate limit.
		for _, e := range badWritable {
			check, base := serve(t, checkConfig(t, w.root)), w.restart(t, "")
			st := step("tests", []string{e}, []string{"tests"}, nil, nil)
			wantProblem(t, post(t, base, commitPath, commitBody(st, v1.ProjectFiles, v1.ProjectFiles, "RUN-1", 1, "2026-11-13T10:00:02Z")), 400, "InvalidRequest")
			wantProblem(t, post(t, base, restorePath, restoreBody(st, v1.ProjectFiles)), 400, "InvalidRequest")
			wantProblem(t, post(t, check, "/step-files", common(pwProject, st, v1.ProjectFiles)), 400, "InvalidRequest")
			wantProblem(t, post(t, check, "/checks/ScopeKept/run", checkBody(t, w.dir, st, v1.ProjectFiles, "c", "e")), 400, "InvalidRequest")
		}
		for _, p := range bad {
			base := w.restart(t, "")
			withBad := append(append([]pf{}, v1.ProjectFiles...), pf{p, sum([]byte("x"))})
			wantProblem(t, post(t, base, commitPath, commitBody(testsStep(), v1.ProjectFiles, withBad, "RUN-1", 1, "2026-11-13T10:00:02Z")), 400, "InvalidRequest")
			wantProblem(t, post(t, base, commitPath, commitBody(testsStep(), withBad, v1.ProjectFiles, "RUN-1", 1, "2026-11-13T10:00:02Z")), 400, "InvalidRequest")
			wantProblem(t, post(t, base, restorePath, restoreBody(testsStep(), withBad)), 400, "InvalidRequest")
		}
		w.unchanged(t, snap, "a 400")
	})
	t.Run("in the folder", func(t *testing.T) {
		for _, p := range []string{".GIT/config", ".Git/HEAD", "sub/.git/HEAD", "tests/.GIT/x"} {
			t.Run(p, func(t *testing.T) {
				w := newPW(t)
				v1, _ := w.firstStep(t)
				write(t, w.dir, p, []byte("x\n"))
				snap := w.state(t)
				wantProblem(t, post(t, w.base, commitPath, commitBody(step("code", []string{"internal/**"}, []string{"code"}, nil, nil), v1.ProjectFiles, v1.ProjectFiles, "RUN-1", 1, "2026-11-13T10:00:02Z")), 422, "UnreadableProject")
				w.unchanged(t, snap, "a commitStep over a folder holding "+p)
				check := serve(t, checkConfig(t, w.root))
				wantProblem(t, post(t, check, "/step-files", common(pwProject, testsStep(), v1.ProjectFiles)), 422, "UnreadableProject")
				if v := runCheck(t, check, "ScopeKept", checkBody(t, w.dir, testsStep(), v1.ProjectFiles, "c", "e")); v.Passed {
					t.Errorf("runCheck must fail on %s, naming it", p)
				}
			})
		}
	})
}

func TestProjectWorkerOtherDeploymentsServeNoVersionHistory(t *testing.T) {
	w := newPW(t)
	v1, b1 := w.firstStep(t)
	restore := restoreBody(testsStep(), v1.ProjectFiles)
	for name, base := range map[string]string{
		"the check service":    serve(t, checkConfig(t, w.root)),
		"a procedure worker":   serve(t, workerConfig(t, w.root, "contract-check", "contract-check")),
		"a generate procedure": serve(t, workerConfig(t, w.root, "generate", "generate")),
	} {
		t.Run(name, func(t *testing.T) {
			snap := w.state(t)
			wantProblem(t, post(t, base, commitPath, b1), 404, "VersionHistoryNotServed")
			wantProblem(t, post(t, base, restorePath, restore), 404, "VersionHistoryNotServed")
			w.unchanged(t, snap, "a 404")
		})
	}
	// The project worker answers the other operations with their 404s (info.description).
	t.Run("the project worker", func(t *testing.T) {
		wantProblem(t, post(t, w.base, "/checks/ScopeKept/run", checkBody(t, w.dir, testsStep(), v1.ProjectFiles, "c", "e")), 404, "UnknownCheck")
		wantProblem(t, post(t, w.base, "/step-files", common(pwProject, testsStep(), v1.ProjectFiles)), 404, "UnknownStep")
		wantProblem(t, post(t, w.base, "/procedures/contract-check/run", ccBody(t, w.dir, string(readTestdata(t, "contract.yaml")), "Feature: F\n")), 404, "UnknownProcedure")
	})
}

// commitStep reads bodies up to x-limits.commit-request-body-max-bytes (24 MiB); restoreStep,
// like every other operation, up to x-limits.request-body-max-bytes (12 MiB).
func TestProjectWorkerCommitBodyLimit(t *testing.T) {
	const limit, commitLimit = 12582912, 25165824
	w := newPW(t)
	v1, b1 := w.firstStep(t)
	pad := func(body map[string]any, n int) []byte {
		raw, _ := json.Marshal(body)
		return append(raw, bytes.Repeat([]byte(" "), n-len(raw))...)
	}
	send := func(path string, b []byte) resp {
		return do(t, w.base, path, q, "Bearer "+token, "application/json", b)
	}
	if r := send(commitPath, pad(b1, limit+1)); r.status != 200 {
		t.Errorf("commitStep must read a body above request-body-max-bytes: %d %.300s", r.status, r.body)
	}
	if r := send(commitPath, pad(b1, commitLimit)); r.status != 200 {
		t.Errorf("commitStep must read a body of exactly %d bytes: %d %.300s", commitLimit, r.status, r.body)
	}
	wantProblem(t, send(commitPath, pad(b1, commitLimit+1)), 400, "InvalidRequest")
	restore := restoreBody(ccStep(), v1.ProjectFiles)
	if r := send(restorePath, pad(restore, limit)); r.status != 200 {
		t.Errorf("restoreStep must read a body of exactly %d bytes: %d %.300s", limit, r.status, r.body)
	}
	wantProblem(t, send(restorePath, pad(restore, limit+1)), 400, "InvalidRequest")
}
