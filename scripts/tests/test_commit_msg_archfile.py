"""T098 (owner, 2026-11-15): scripts/hooks/commit-msg protects every `.go-arch-lint.yml` (an
archfile is a check, constitution XVII): a commit that adds, changes or deletes one needs an
`Approved-by:` trailer, through real `git commit` in throwaway repositories whose core.hooksPath
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
OWNER = "Approved-by: Owner (2026-11-15)"
ARCHFILES = ["src/engine/.go-arch-lint.yml", "src/slice/.go-arch-lint.yml", ".go-arch-lint.yml"]


class ArchfileProtectedTest(unittest.TestCase):
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
        self.write("src/engine/.go-arch-lint.yml", "version: 3\n")
        self.write("docs/x.go-arch-lint.yml.md", "x\n")
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
        os.makedirs(os.path.dirname(full) or ".", exist_ok=True)
        with open(full, "w") as f:
            f.write(text)

    def commit(self, *trailers):
        message = "chore: change\n\nBody text.\n"
        if trailers:
            message += "\n" + "\n".join(trailers) + "\n"
        return self.git("commit", "-q", "-m", message, check=False)

    def refused(self, path):
        r = self.commit()
        self.assertNotEqual(r.returncode, 0, "commit should be refused: " + r.stdout)
        self.assertIn("protected paths changed", r.stderr)
        self.assertIn("Approved-by", r.stderr)
        self.assertIn(path, r.stderr)

    def test_add_without_approved_by_is_refused_naming_the_file(self):
        for path in ARCHFILES[1:]:
            with self.subTest(path=path):
                self.write(path, "version: 3\n")
                self.git("add", path)
                self.refused(path)
                self.git("reset", "-q")
                os.remove(os.path.join(self.repo, path))

    def test_change_without_approved_by_is_refused_naming_the_file(self):
        self.write("src/engine/.go-arch-lint.yml", "version: 3\n# changed\n")
        self.git("add", "-A")
        self.refused("src/engine/.go-arch-lint.yml")

    def test_delete_without_approved_by_is_refused_naming_the_file(self):
        self.git("rm", "-q", "src/engine/.go-arch-lint.yml")
        self.refused("src/engine/.go-arch-lint.yml")

    def test_add_change_delete_with_approved_by_are_accepted(self):
        self.write("src/slice/.go-arch-lint.yml", "version: 3\n")
        self.git("add", "-A")
        r = self.commit(OWNER)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.write("src/slice/.go-arch-lint.yml", "version: 3\n# changed\n")
        self.git("add", "-A")
        r = self.commit(OWNER)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.git("rm", "-q", "src/slice/.go-arch-lint.yml")
        r = self.commit(OWNER)
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_a_file_merely_named_like_it_is_not_protected(self):
        for path in ("docs/x.go-arch-lint.yml.md", "docs/y.go-arch-lint.yml",
                     "src/engine/go-arch-lint.yml"):
            with self.subTest(path=path):
                self.write(path, "changed %s\n" % path)
                self.git("add", path)
                r = self.commit()
                self.assertEqual(r.returncode, 0, "not protected, yet refused: " + r.stderr)


if __name__ == "__main__":
    unittest.main()
