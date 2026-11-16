"""T107 (first part): scripts/hooks/commit-msg protects `.github/` (constitution XVI: changing CI
needs the owner's recorded approval), through real `git commit` in throwaway repositories whose
core.hooksPath holds a copy of the hook."""
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


class GithubProtectedTest(unittest.TestCase):
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

    def stage(self, path):
        self.write(path, "content of %s\n" % path)
        self.git("add", path)

    def commit(self, *trailers):
        message = "ci: change\n\nBody text.\n"
        if trailers:
            message += "\n" + "\n".join(trailers) + "\n"
        return self.git("commit", "-q", "-m", message, check=False)

    def test_workflow_without_approved_by_is_refused_naming_the_reason(self):
        self.stage(".github/workflows/check.yml")
        r = self.commit()
        self.assertNotEqual(r.returncode, 0, "commit should be refused: " + r.stdout)
        self.assertIn("protected paths changed", r.stderr)
        self.assertIn("Approved-by", r.stderr)
        self.assertIn(".github/workflows/check.yml", r.stderr)

    def test_workflow_with_approved_by_is_accepted(self):
        self.stage(".github/workflows/check.yml")
        r = self.commit(OWNER)
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_unprotected_path_without_trailer_is_accepted(self):
        self.stage("docs/x.md")
        r = self.commit()
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_dot_github_named_file_elsewhere_is_not_protected(self):
        for p in ["docs/.github-notes.md", "docs/.github/x.md", "notes.github/x.md"]:
            with self.subTest(path=p):
                self.stage(p)
                r = self.commit()
                self.assertEqual(r.returncode, 0, r.stderr)

    def test_any_file_under_dot_github_is_protected(self):
        self.stage(".github/dependabot.yml")
        r = self.commit()
        self.assertNotEqual(r.returncode, 0)
        self.assertIn(".github/dependabot.yml", r.stderr)


if __name__ == "__main__":
    unittest.main()
