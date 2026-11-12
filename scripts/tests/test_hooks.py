"""Tests for scripts/hooks/commit-msg, run against throwaway git repositories."""
import os
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

import gitenv

# COMMIT_MSG_HOOK_SOURCE may name another commit-msg hook file to test (for example a copy of the
# committed version).
HOOK = os.environ.get("COMMIT_MSG_HOOK_SOURCE") or os.path.abspath(
    os.path.join(os.path.dirname(__file__), "..", "hooks", "commit-msg")
)


class CommitMsgHookTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.repo = self._tmp.name
        self.env = gitenv.clean_env(gitenv.make_home(self))
        self.git("init", "-q")
        self.git("config", "user.name", "Test User")
        self.git("config", "user.email", "test@example.com")

    def git(self, *args):
        subprocess.run(["git", *args], cwd=self.repo, check=True, env=self.env,
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE)

    def stage(self, *paths):
        for p in paths:
            full = os.path.join(self.repo, p)
            os.makedirs(os.path.dirname(full), exist_ok=True)
            with open(full, "w") as f:
                f.write("content of %s\n" % p)
            self.git("add", p)

    def run_hook(self, message):
        msg_file = os.path.join(self._tmp.name, "..", "msg-%d.txt" % id(self))
        msg_file = os.path.abspath(msg_file)
        with open(msg_file, "w") as f:
            f.write(message)
        self.addCleanup(lambda: os.path.exists(msg_file) and os.remove(msg_file))
        return subprocess.run([HOOK, msg_file], cwd=self.repo, env=self.env,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                              text=True)

    def test_unprotected_change_without_trailers_passes(self):
        self.stage("docs/notes.md")
        r = self.run_hook("docs: add notes\n")
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_protected_path_without_approval_fails_and_names_file(self):
        self.stage("scripts/x.sh")
        r = self.run_hook("chore: add script\n")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("scripts/x.sh", r.stderr)

    def test_protected_path_with_approval_trailer_passes(self):
        self.stage("scripts/x.sh")
        r = self.run_hook("chore: add script\n\nApproved-by: Owner 2026-11-12\n")
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_approval_in_body_paragraph_not_final_block_fails(self):
        self.stage("scripts/x.sh")
        r = self.run_hook(
            "chore: add script\n\nApproved-by: Owner 2026-11-12\n\n"
            "More explanation in a later paragraph.\n"
        )
        self.assertNotEqual(r.returncode, 0)

    def test_code_and_tests_without_tests_approved_fails(self):
        self.stage("src/engine/a.go", "src/engine/a_test.go")
        r = self.run_hook("feat: engine\n\nApproved-by: Owner 2026-11-12\n")
        self.assertNotEqual(r.returncode, 0)

    def test_code_and_tests_with_both_trailers_passes(self):
        self.stage("src/engine/a.go", "src/engine/a_test.go")
        r = self.run_hook(
            "feat: engine\n\nApproved-by: Owner 2026-11-12\n"
            "Tests-approved-by: Fresh Reviewer\n"
        )
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_code_alone_is_unprotected_and_passes(self):
        self.stage("src/engine/a.go")
        r = self.run_hook("feat: engine\n")
        self.assertEqual(r.returncode, 0, r.stderr)

    # T009d: the hook that judges is the staged scripts/hooks/commit-msg when it differs from the
    # running file (here the running file is outside the repository, so any staged copy differs).
    def stage_text(self, path, text):
        full = os.path.join(self.repo, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w") as f:
            f.write(text)
        self.git("add", path)

    def test_staged_commit_msg_that_refuses_everything_decides(self):
        self.stage_text("scripts/hooks/commit-msg", "#!/bin/sh\necho STAGED-REFUSES >&2\nexit 1\n")
        self.stage("docs/notes.md")
        r = self.run_hook("docs: add notes\n\nApproved-by: Owner 2026-11-12\n")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("STAGED-REFUSES", r.stderr)

    def test_staged_commit_msg_that_accepts_everything_decides(self):
        self.stage_text("scripts/hooks/commit-msg", "#!/bin/sh\nexit 0\n")
        r = self.run_hook("chore: add script\n")
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_staged_copy_of_the_same_rules_judges_the_forwarded_message(self):
        with open(HOOK) as f:
            text = f.read()
        self.stage_text("scripts/hooks/commit-msg", text + "\n# staged variant\n")
        self.stage("scripts/x.sh")
        bad = self.run_hook("chore: add script\n")
        self.assertNotEqual(bad.returncode, 0)
        self.assertIn("scripts/x.sh", bad.stderr)
        good = self.run_hook("chore: add script\n\nApproved-by: Owner 2026-11-12\n")
        self.assertEqual(good.returncode, 0, good.stderr)

    def test_caller_variable_does_not_make_the_running_commit_msg_decide(self):
        self.stage_text("scripts/hooks/commit-msg", "#!/bin/sh\necho STAGED-REFUSES >&2\nexit 1\n")
        self.env["AP_PRE_COMMIT_STAGED_HOOK"] = "1"
        self.env["AP_COMMIT_MSG_STAGED_HOOK"] = "1"
        r = self.run_hook("docs: add notes\n")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("STAGED-REFUSES", r.stderr)

    # T009d review: fail closed.
    def test_failing_git_diff_refuses(self):
        self.stage("docs/notes.md")
        real = shutil.which("git")
        stub = gitenv.stub_dir(self, {"git": (
            'if [ "$1" = diff ]; then echo "stub git: diff failed" >&2; exit 3; fi\n'
            'exec %s "$@"\n' % real)})
        env = self.env
        self.env = gitenv.with_path(self.env, stub)
        try:
            r = self.run_hook("docs: add notes\n")
        finally:
            self.env = env
        self.assertNotEqual(r.returncode, 0, r.stderr)

    def test_control_pass_through_git_stub_accepts(self):
        self.stage("docs/notes.md")
        stub = gitenv.stub_dir(self, {"git": 'exec %s "$@"\n' % shutil.which("git")})
        env = self.env
        self.env = gitenv.with_path(self.env, stub)
        try:
            r = self.run_hook("docs: add notes\n")
        finally:
            self.env = env
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_go_file_with_a_newline_in_its_name_counts_as_code(self):
        # "src/a\nb.go" is one name; split at the newline neither half looks like Go code
        self.stage("src/a\nb.go", "src/a_test.go")
        r = self.run_hook("feat: engine\n\nApproved-by: Owner 2026-11-12\n")
        self.assertNotEqual(r.returncode, 0, r.stderr)
        self.assertIn("Tests-approved-by", r.stderr)

    def test_go_file_with_a_newline_in_its_name_and_both_trailers_passes(self):
        self.stage("src/a\nb.go", "src/a_test.go")
        r = self.run_hook("feat: engine\n\nApproved-by: Owner 2026-11-12\n"
                          "Tests-approved-by: Fresh Reviewer\n")
        self.assertEqual(r.returncode, 0, r.stderr)


class IsolationTest(unittest.TestCase):
    """The helpers must never touch the repository that encloses the test run."""

    def _git(self, cwd, *args):
        return subprocess.run(["git", *args], cwd=cwd, check=True, env=self.clean,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    def test_leaked_git_variables_do_not_reach_the_enclosing_repository(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        home = os.path.join(tmp.name, "home")
        os.makedirs(home)
        self.clean = gitenv.clean_env(home)
        decoy = os.path.join(tmp.name, "decoy")
        os.makedirs(decoy)
        self._git(decoy, "init", "-q")
        with open(os.path.join(decoy, "seed.txt"), "w") as f:
            f.write("seed\n")
        self._git(decoy, "add", "seed.txt")
        gd = os.path.join(decoy, ".git")

        def read(name):
            with open(os.path.join(gd, name), "rb") as f:
                return f.read()

        config_before, index_before = read("config"), read("index")
        head_before = self._git(decoy, "rev-parse", "--git-dir").stdout
        leaked = {
            "GIT_DIR": gd,
            "GIT_WORK_TREE": decoy,
            "GIT_INDEX_FILE": os.path.join(gd, "index"),
            "GIT_PREFIX": "",
            "GIT_OBJECT_DIRECTORY": os.path.join(gd, "objects"),
            "GIT_ALTERNATE_OBJECT_DIRECTORIES": os.path.join(gd, "objects"),
            "GIT_COMMON_DIR": gd,
            "GIT_AUTHOR_NAME": "Leaked",
        }
        with mock.patch.dict(os.environ, leaked):
            for name in ("test_unprotected_change_without_trailers_passes",
                         "test_protected_path_without_approval_fails_and_names_file",
                         "test_code_and_tests_with_both_trailers_passes"):
                t = CommitMsgHookTest(name)
                t.setUp()
                try:
                    getattr(t, name)()
                finally:
                    t.doCleanups()
        self.assertEqual(read("config"), config_before)
        self.assertEqual(read("index"), index_before)
        self.assertNotIn(b"bare = true", read("config"))
        self.assertNotIn(b"Test User", read("config"))
        bare = self._git(decoy, "config", "--get", "core.bare").stdout.strip()
        self.assertIn(bare, ("", "false"))
        self.assertEqual(self._git(decoy, "rev-parse", "--git-dir").stdout, head_before)

    def test_clean_env_drops_every_git_variable_and_moves_home(self):
        with mock.patch.dict(os.environ, {"GIT_DIR": "/x", "GIT_FOO": "1", "HOME": "/real"}):
            env = gitenv.clean_env("/scratch")
        self.assertEqual(env["HOME"], "/scratch")
        self.assertNotIn("GIT_DIR", env)
        self.assertNotIn("GIT_FOO", env)
        self.assertTrue(all(k in ("GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_GLOBAL")
                            for k in env if k.startswith("GIT_")))


if __name__ == "__main__":
    unittest.main()
