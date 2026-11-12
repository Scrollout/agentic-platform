"""Tests for scripts/hooks/commit-msg, run against throwaway git repositories."""
import os
import subprocess
import tempfile
import unittest
from unittest import mock

import gitenv

HOOK = os.path.abspath(
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
