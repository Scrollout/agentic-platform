"""T108: the pre-commit hook keeps only the quick static checks of what a commit changes.

Root layout, secret scan, knowledge bundle, markdown, contract lint, inline-schema drift, and
`gofmt -l` and `go vet ./...` of each changed Go module (in the pinned Go container). No test
suite runs in the hook: not the Python check tests, not `go test`; CI runs them (T107).

Each test clones this repository, commits the hook under test and commits for real through
core.hooksPath (the base class is test_pre_commit.CloneCase, unchanged). What ran is read from a
log kept by recording wrappers for `docker` and `python3` placed first on PATH: they log the call
and run the real command, except that a `go test` call or a testrun.py call is logged and
answered with success without running (so a hook that still runs a suite is caught quickly and
cheaply), and a call matching STUB_SLEEP_ON creates STUB_MARKER and sleeps (a slow step).
Docker and the pinned Go image, Node tools and module cache are prerequisites, as in
test_pre_commit.py; the caches are the user's (XDG_CACHE_HOME), the repository is the clone.
"""
import os
import re
import shutil
import signal
import subprocess
import time
import unittest

import gitenv
import test_pre_commit as base

REPO = base.REPO
APPROVAL = base.APPROVAL
REAL_CACHE = os.environ.get("XDG_CACHE_HOME") or os.path.join(os.path.expanduser("~"), ".cache")
OTHER_MODULES = ("check", "engine", "cli", "page", "tests")

GO_OK = "package zzplanted\n\nfunc F() int {\n\treturn 1\n}\n"
GO_UNFORMATTED = "package zzplanted\n\nfunc   F( ) int {\n\treturn   1\n}\n"
GO_VET_VIOLATION = 'package zzplanted\n\nimport "fmt"\n\nfunc F() {\n\tfmt.Printf("%d\\n", "s")\n}\n'
GO_FAILING_TEST = ('package zzplanted\n\nimport "testing"\n\nfunc TestPlanted(t *testing.T) {\n'
                   '\tt.Fatal("PLANTED-GO-TEST-FAILURE")\n}\n')
FAILING_SCRIPT_TEST = ("import unittest\n\n\nclass T(unittest.TestCase):\n"
                       "    def test_fails(self):\n        self.fail('PLANTED-SCRIPT-TEST-FAILURE')\n")
NO_FORGE = re.compile(r"(\bno|\bwithout a) hosted (forge|CI)\b", re.I)


class QuickCase(base.CloneCase):
    """A clone with the wrappers on PATH; log() is what the hook called."""

    wrappers = True
    hook_timeout = 420

    def setUp(self):
        super().setUp()
        self.log_path = os.path.join(self.tmp, "calls.log")
        self.marker = os.path.join(self.tmp, "slow-step-started")
        self.env["XDG_CACHE_HOME"] = REAL_CACHE
        if self.wrappers:
            docker, python = shutil.which("docker"), shutil.which("python3")
            stubs = gitenv.stub_dir(self, {
                "docker": (
                    'echo "docker $*" >>"$STUB_LOG"\n'
                    'case "$*" in *go\\ test*) echo "STUB: go test not run" >&2; exit 0;; esac\n'
                    'if [ -n "${STUB_SLEEP_ON:-}" ]; then case "$*" in *"$STUB_SLEEP_ON"*)\n'
                    '  : >"$STUB_MARKER"; exec sleep 120;; esac; fi\n'
                    'exec %s "$@"\n' % docker),
                "python3": (
                    'case "$*" in *testrun.py*) echo "python3 $*" >>"$STUB_LOG"\n'
                    '  echo "STUB: testrun not run" >&2; exit 0;; esac\n'
                    'exec %s "$@"\n' % python),
            })
            self.env = gitenv.with_path(self.env, stubs)
            self.env["STUB_LOG"] = self.log_path
            self.env["STUB_MARKER"] = self.marker
        open(self.log_path, "a").close()

    # -- running
    def commit_t(self, *args, env=None):
        try:
            return subprocess.run(
                ["git", "commit", "-q", "-m", "test commit", "-m", APPROVAL, *args],
                cwd=self.clone, env=env or self.env, stdout=subprocess.PIPE,
                stderr=subprocess.PIPE, text=True, timeout=self.hook_timeout)
        except subprocess.TimeoutExpired:
            self.fail("the commit took longer than %d s" % self.hook_timeout)

    def hook_t(self):
        try:
            return subprocess.run([self.hook], cwd=self.clone, env=self.env, stdout=subprocess.PIPE,
                                  stderr=subprocess.PIPE, text=True, timeout=self.hook_timeout)
        except subprocess.TimeoutExpired:
            self.fail("the hook took longer than %d s" % self.hook_timeout)

    # -- reading what ran
    def calls(self):
        with open(self.log_path) as f:
            return f.read().splitlines()

    def tool_calls(self, word):
        return [c for c in self.calls() if re.search(r"\b%s\b" % re.escape(word), c)]

    def modules_named(self, line):
        return sorted(m for m in ("slice",) + OTHER_MODULES
                      if re.search(r"(?<![\w.-])%s(?![\w.-])" % m, strip_mounts(line)))

    def assert_vet_and_gofmt_of(self, module):
        gofmt, vet = self.tool_calls("gofmt"), self.tool_calls("vet")
        self.assertTrue(gofmt, "gofmt did not run: %r" % self.calls())
        self.assertTrue(vet, "go vet did not run: %r" % self.calls())
        for line in gofmt + vet:
            named = self.modules_named(line)
            self.assertEqual(named, [module], "this call names %s, not only %s: %s"
                             % (named, module, line))

    def assert_no_suite(self, out=""):
        self.assertEqual(self.tool_calls("testrun.py"), [], "a Python suite ran")
        self.assertEqual([c for c in self.calls() if re.search(r"go test|-race", c)], [],
                         "go test ran")
        self.assertNotIn("check tests", out)
        self.assertNotIn("go test -race", out)

    def stage_go(self, module, name, text):
        self.stage("src/%s/zzplanted/%s" % (module, name), text)


def strip_mounts(line):
    """The words of a docker call without the mount of the whole src/ folder and the paths of
    the clone (a temporary folder whose name is not a module)."""
    line = re.sub(r"\S*/clone/\S*", "", line)
    return re.sub(r"-v \S+:/src\b", "", line)


class GoCommitTest(QuickCase):
    def test_a_go_only_commit_runs_gofmt_and_vet_of_that_module_and_no_go_test(self):
        self.stage_go("slice", "ok.go", GO_OK)
        r = self.commit_t()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assert_vet_and_gofmt_of("slice")
        self.assert_no_suite(self.out(r))

    def test_a_gofmt_violation_is_refused(self):
        self.stage_go("engine", "bad.go", GO_UNFORMATTED)
        before = self.head()
        r = self.commit_t()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("bad.go", self.out(r))
        self.assertEqual(self.head(), before)

    def test_control_the_formatted_file_is_accepted(self):
        self.stage_go("engine", "bad.go", GO_OK)
        r = self.commit_t()
        self.assertEqual(r.returncode, 0, self.out(r))

    def test_a_vet_violation_is_refused(self):
        self.stage_go("engine", "vetbad.go", GO_VET_VIOLATION)
        before = self.head()
        r = self.commit_t()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("Printf", self.out(r))
        self.assertEqual(self.head(), before)

    def test_control_the_file_without_the_vet_violation_is_accepted(self):
        self.stage_go("engine", "vetbad.go", GO_OK)
        r = self.commit_t()
        self.assertEqual(r.returncode, 0, self.out(r))

    def test_an_unstaged_violation_in_another_module_is_not_judged(self):
        self.write("src/engine/zzplanted/bad.go", GO_UNFORMATTED)  # untracked, never staged
        self.write("src/cli/zzplanted/vetbad.go", GO_VET_VIOLATION)
        self.stage_go("slice", "ok.go", GO_OK)
        r = self.commit_t()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assert_vet_and_gofmt_of("slice")

    def test_a_staged_violation_is_judged_even_if_the_working_copy_is_clean(self):
        self.stage_go("slice", "bad.go", GO_UNFORMATTED)
        self.write("src/slice/zzplanted/bad.go", GO_OK)
        r = self.commit_t()
        self.assertNotEqual(r.returncode, 0, self.out(r))

    def test_a_clean_staged_file_passes_even_if_the_working_copy_is_a_violation(self):
        self.stage_go("slice", "bad.go", GO_OK)
        self.write("src/slice/zzplanted/bad.go", GO_UNFORMATTED)
        r = self.commit_t()
        self.assertEqual(r.returncode, 0, self.out(r))


class NoSuiteTest(QuickCase):
    def test_a_failing_staged_check_test_commits_without_being_run(self):
        self.stage("scripts/tests/test_fails.py", FAILING_SCRIPT_TEST)
        self.stage("scripts/x.sh", "#!/bin/sh\necho x\n")
        r = self.commit_t()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertNotIn("PLANTED-SCRIPT-TEST-FAILURE", self.out(r))
        self.assert_no_suite(self.out(r))

    def test_a_staged_failing_go_test_commits_without_being_run(self):
        self.stage_go("slice", "planted_test.go", GO_FAILING_TEST)
        r = self.commit_t()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertNotIn("PLANTED-GO-TEST-FAILURE", self.out(r))
        self.assert_no_suite(self.out(r))

    def test_a_go_mod_change_runs_no_suite(self):
        path = os.path.join(self.clone, "src/slice/go.mod")
        with open(path, "a") as f:
            f.write("\n// planted comment\n")
        self.git("add", "src/slice/go.mod")
        r = self.commit_t()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assert_no_suite(self.out(r))

    def test_an_api_change_runs_no_suite(self):
        self.stage("src/engine/api/zzplanted.txt", "x\n")
        r = self.commit_t()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assert_no_suite(self.out(r))


class ModuleListTest(QuickCase):
    """The hook must not judge "nothing to check" when it cannot tell which modules changed."""

    def test_deleting_go_work_with_a_staged_go_file_is_refused(self):
        self.stage_go("slice", "ok.go", GO_OK)
        self.git("rm", "-q", "src/go.work")
        before = self.head()
        r = self.commit_t()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("go.work", self.out(r))
        self.assertEqual(self.head(), before)

    def test_a_go_file_outside_every_module_is_refused_and_named(self):
        self.stage("scripts/x.go", "package x\n")
        before = self.head()
        r = self.commit_t()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("scripts/x.go", self.out(r))
        self.assertEqual(self.head(), before)

    def test_two_changed_modules_give_one_gofmt_and_vet_call_each(self):
        self.stage_go("slice", "ok.go", GO_OK)
        self.stage_go("cli", "ok.go", GO_OK)
        r = self.commit_t()
        self.assertEqual(r.returncode, 0, self.out(r))
        calls = [c for c in self.calls() if re.search(r"\b(gofmt|vet)\b", c)]
        self.assertEqual(len(calls), 2, calls)
        self.assertEqual(sorted(tuple(self.modules_named(c)) for c in calls),
                         [("cli",), ("slice",)], calls)
        for c in calls:
            self.assertRegex(c, r"\bgofmt\b")
            self.assertRegex(c, r"\bvet\b")

    def test_a_go_sum_only_change_runs_gofmt_and_vet_of_that_module(self):
        path = os.path.join(self.clone, "src/slice/go.sum")
        with open(path) as f:
            lines = f.read().splitlines()
        with open(path, "a") as f:
            f.write(lines[-1] + "\n")  # a repeated line: harmless to go
        self.git("add", "src/slice/go.sum")
        r = self.commit_t()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assert_vet_and_gofmt_of("slice")
        self.assert_no_suite(self.out(r))


class QuickChecksStillRunTest(QuickCase):
    def test_a_docs_commit_runs_layout_secret_scan_bundle_and_markdown_and_no_go_step(self):
        self.stage("docs/notes-quick.txt", "x\n")
        self.stage("src/agents/notes.md", "# Title\n\ntext\n")
        r = self.commit_t()
        self.assertEqual(r.returncode, 0, self.out(r))
        for step in ("root layout", "secret scan", "knowledge bundle", "markdown"):
            self.assertIn("pre-commit: " + step, self.out(r), step)
        self.assertEqual(self.tool_calls("gofmt") + self.tool_calls("vet"), [])
        self.assert_no_suite(self.out(r))

    def test_a_secret_in_a_go_commit_is_still_refused(self):
        self.stage_go("slice", "ok.go", GO_OK + "// token: %s\n" % base.SECRET)
        r = self.commit_t()
        self.assertNotEqual(r.returncode, 0, self.out(r))

    def test_contract_lint_still_runs_when_a_contract_is_staged(self):
        path = "src/engine/api/preview/2026-11-12-preview/openapi.yaml"
        with open(os.path.join(self.clone, path)) as f:
            text = f.read()
        self.stage(path, text + "\n# touched\n")
        r = self.commit_t()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertIn("pre-commit: contracts", self.out(r))

    def test_a_contract_that_does_not_parse_is_refused(self):
        path = "src/engine/api/preview/2026-11-12-preview/openapi.yaml"
        with open(os.path.join(self.clone, path)) as f:
            text = f.read()
        self.stage(path, text + "\nplanted: [unclosed\n")
        before = self.head()
        r = self.commit_t()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertEqual(self.head(), before)

    AGENT = "src/agents/analyst.yaml"
    BEGIN = "# BEGIN generated by scripts/generate.sh: "

    def agent_text(self):
        with open(os.path.join(self.clone, self.AGENT)) as f:
            return f.read()

    def test_inline_schema_drift_still_runs_and_refuses_drift(self):
        lines = self.agent_text().split("\n")
        i = next(i for i, l in enumerate(lines) if self.BEGIN in l) + 3
        lines[i] += " planted"
        self.stage(self.AGENT, "\n".join(lines))
        r = self.commit_t()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("committed generated blocks", self.out(r))

    def test_inline_schema_check_runs_for_a_change_outside_the_block(self):
        self.stage(self.AGENT, "# a comment above the definition\n" + self.agent_text())
        r = self.commit_t()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertIn("committed generated blocks", self.out(r))


class SigtermTest(QuickCase):
    """SIGTERM during a slow quick step (the markdown lint, slowed by the wrapper): no temp
    folder is left, the commit is refused, HEAD is unchanged."""

    def slow(self):
        self.env["STUB_SLEEP_ON"] = "markdownlint"
        self.stage("src/agents/slow.md", "# Title\n\ntext\n")

    def wait_for_marker(self, p):
        deadline = time.time() + 120
        while not os.path.exists(self.marker) and time.time() < deadline and p.poll() is None:
            time.sleep(0.2)
        self.assertTrue(os.path.exists(self.marker), "the slow step never started")

    def settle(self):
        deadline = time.time() + 15
        while self.leftovers() and time.time() < deadline:
            time.sleep(0.2)
        self.assertEqual(self.leftovers(), [])

    def staged_variant(self):
        with open(self.hook) as f:
            good = f.read()
        self.stage("scripts/hooks/pre-commit", good + "\n# staged variant\n")
        self.write("scripts/hooks/pre-commit", good)  # the running copy differs: hand over

    def terminate(self, p):
        self.addCleanup(lambda: (p.poll() is None) and os.killpg(p.pid, signal.SIGKILL))
        self.wait_for_marker(p)
        self.assertGreaterEqual(len(self.leftovers()), 1, "the temp folder exists while it runs")
        os.killpg(p.pid, signal.SIGTERM)
        try:
            return p.wait(timeout=30)
        except subprocess.TimeoutExpired:
            self.fail("the hook did not end after SIGTERM")

    def test_sigterm_when_the_hook_runs_directly(self):
        self.slow()
        with open(os.path.join(self.tmp, "hook.log"), "w") as log:
            p = subprocess.Popen([self.hook], cwd=self.clone, env=self.env, stdout=log, stderr=log,
                                 start_new_session=True)
            rc = self.terminate(p)
        self.assertNotEqual(rc, 0)
        self.settle()

    def test_sigterm_when_the_hook_is_handed_over(self):
        self.slow()
        self.staged_variant()
        with open(os.path.join(self.tmp, "hook.log"), "w") as log:
            p = subprocess.Popen([self.hook], cwd=self.clone, env=self.env, stdout=log, stderr=log,
                                 start_new_session=True)
            rc = self.terminate(p)
        self.assertNotEqual(rc, 0)
        self.settle()

    def test_sigterm_during_a_real_commit_refuses_it_and_leaves_head(self):
        self.slow()
        before = self.head()
        with open(os.path.join(self.tmp, "commit.log"), "w") as log:
            p = subprocess.Popen(["git", "commit", "-q", "-m", "test commit", "-m", APPROVAL],
                                 cwd=self.clone, env=self.env, stdout=log, stderr=log,
                                 start_new_session=True)
            rc = self.terminate(p)
        self.assertNotEqual(rc, 0)
        self.settle()
        self.assertEqual(self.head(), before)

    def test_sigterm_during_a_real_commit_with_a_handed_over_hook(self):
        self.slow()
        self.staged_variant()
        before = self.head()
        with open(os.path.join(self.tmp, "commit.log"), "w") as log:
            p = subprocess.Popen(["git", "commit", "-q", "-m", "test commit", "-m", APPROVAL],
                                 cwd=self.clone, env=self.env, stdout=log, stderr=log,
                                 start_new_session=True)
            rc = self.terminate(p)
        self.assertNotEqual(rc, 0)
        self.settle()
        self.assertEqual(self.head(), before)


class TimeTest(QuickCase):
    wrappers = False
    hook_timeout = 240

    def test_a_small_real_commit_takes_less_than_two_minutes(self):
        self.stage_go("slice", "ok.go", GO_OK)
        self.stage("src/agents/notes.md", "# Title\n\ntext\n")
        t0 = time.monotonic()
        r = self.commit_t()
        elapsed = time.monotonic() - t0
        self.assertEqual(r.returncode, 0, self.out(r))
        m = re.search(r"^pre-commit: done in (\d+(?:\.\d+)?) s$", self.out(r), re.M)
        self.assertIsNotNone(m, "the hook did not print 'pre-commit: done in <n> s':\n" + self.out(r))
        print("\nT108 small commit: hook says %s s, measured %.1f s" % (m.group(1), elapsed))
        self.assertLess(float(m.group(1)), 120)
        self.assertLess(elapsed, 120)
        for step in ("root layout", "secret scan", "markdown"):
            self.assertIn("pre-commit: " + step, self.out(r), step)
        self.assertEqual(len(self.committed_files()), 2)


class NoHostedForgeTextTest(unittest.TestCase):
    def read(self, rel):
        with open(os.path.join(REPO, rel)) as f:
            return f.read()

    def test_the_matcher_finds_the_old_statements(self):  # planted violations
        for text in ("Without a hosted forge, local git hooks", "(no hosted CI for now)",
                     "There is no hosted forge."):
            self.assertTrue(NO_FORGE.search(text), text)
        self.assertFalse(NO_FORGE.search("CI runs the rest on GitHub-hosted runners."))

    def test_check_sh_header_has_no_such_statement_and_names_ci(self):
        header = "".join(self.read("scripts/check.sh").splitlines(True)[:6])
        self.assertIsNone(NO_FORGE.search(header), header)
        self.assertIn("CI", header)

    def test_the_two_docs_have_no_such_statement_and_say_ci_runs_the_rest(self):
        for rel in ("docs/playbooks/protect-changes-locally.md", "docs/operations/local.md"):
            text = self.read(rel)
            self.assertIsNone(NO_FORGE.search(text), rel)
            self.assertRegex(text, r"\bCI\b", rel)

    def test_the_playbook_description_and_its_index_line_say_what_the_hook_checks(self):
        text = self.read("docs/playbooks/protect-changes-locally.md")
        desc = re.search(r"^description: (.*)$", text, re.M).group(1)
        self.assertIsNone(NO_FORGE.search(desc), desc)
        self.assertRegex(desc, r"\bCI\b")
        index = self.read("docs/playbooks/index.md")
        line = next(l for l in index.splitlines() if "protect-changes-locally" in l)
        self.assertIsNone(NO_FORGE.search(line), line)
        self.assertRegex(line, r"\bCI\b")


if __name__ == "__main__":
    unittest.main()
