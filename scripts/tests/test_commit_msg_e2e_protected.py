"""T024 (design notes, analyze round 6): scripts/hooks/commit-msg protects `src/tests/e2e/`, since the
Compose override there can change the whole stack the launcher starts; a change to it needs the
owner's `Approved-by:` (constitution XVI), and a change to an existing file there also the owner's
`Tests-approved-by:` (ADR 0029). Real `git commit` in throwaway repositories whose core.hooksPath
holds a copy of the hook."""
import os
import shutil
import subprocess
import tempfile
import unittest

import gitenv

HOOK = os.environ.get("COMMIT_MSG_HOOK_SOURCE") or os.path.abspath(
    os.path.join(os.path.dirname(__file__), "..", "hooks", "commit-msg")
)
OWNER = "Approved-by: Owner (2026-11-16)"
TESTS_OWNER = "Tests-approved-by: Owner (2026-11-16)"


class E2EProtectedTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        root = self._tmp.name
        self.repo = os.path.join(root, "repo")
        os.makedirs(self.repo)
        hooks = os.path.join(root, "hooks")
        os.makedirs(hooks)
        shutil.copy(HOOK, os.path.join(hooks, "commit-msg"))
        os.chmod(os.path.join(hooks, "commit-msg"), 0o755)
        self.env = gitenv.clean_env(gitenv.make_home(self))
        self.git("init", "-q")
        self.git("config", "user.name", "Test User")
        self.git("config", "user.email", "test@example.com")
        self.git("config", "core.hooksPath", hooks)
        self.write("README.md", "r\n")
        self.git("add", "-A")
        self.git("commit", "-q", "--no-verify", "-m", "initial")

    def git(self, *args, check=True):
        r = subprocess.run(["git", *args], cwd=self.repo, env=self.env, text=True,
                           stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if check and r.returncode != 0:
            self.fail("git %s failed: %s" % (args, r.stderr))
        return r

    def write(self, path, text):
        full = os.path.join(self.repo, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w") as f:
            f.write(text)

    def stage(self, path, text=None):
        self.write(path, text or "content of %s\n" % path)
        self.git("add", path)

    def commit(self, *trailers):
        message = "test(e2e): change\n\nBody text.\n"
        if trailers:
            message += "\n" + "\n".join(trailers) + "\n"
        return self.git("commit", "-q", "-m", message, check=False)

    def test_a_new_e2e_file_without_approved_by_is_refused_naming_it(self):
        self.stage("src/tests/e2e/x.yaml")
        r = self.commit()
        self.assertNotEqual(r.returncode, 0, "commit should be refused: " + r.stdout)
        self.assertIn("protected paths changed", r.stderr)
        self.assertIn("Approved-by", r.stderr)
        self.assertIn("src/tests/e2e/x.yaml", r.stderr)

    def test_a_new_e2e_file_with_approved_by_is_accepted(self):
        self.stage("src/tests/e2e/x.yaml")
        r = self.commit(OWNER)
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_the_override_in_a_subfolder_is_protected_too(self):
        self.stage("src/tests/e2e/sets/compose.recorded.yaml")
        r = self.commit()
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("src/tests/e2e/sets/compose.recorded.yaml", r.stderr)

    def test_changing_an_existing_e2e_file_needs_both_approvals(self):
        self.stage("src/tests/e2e/compose.recorded.yaml")
        self.assertEqual(self.commit(OWNER).returncode, 0)
        self.stage("src/tests/e2e/compose.recorded.yaml", "changed\n")
        r = self.commit(OWNER)
        self.assertNotEqual(r.returncode, 0, "an existing test file changed without Tests-approved-by")
        self.assertIn("Tests-approved-by", r.stderr)
        self.assertEqual(self.commit(OWNER, TESTS_OWNER).returncode, 0, "both approvals given")

    def test_a_like_named_folder_elsewhere_is_not_protected(self):
        for p in ["docs/src/tests/e2e/x.md", "docs/e2e/x.md"]:
            with self.subTest(path=p):
                self.stage(p)
                r = self.commit()
                self.assertEqual(r.returncode, 0, r.stderr)


if __name__ == "__main__":
    unittest.main()
