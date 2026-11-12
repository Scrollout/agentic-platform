"""Tests for scripts/hooks/pre-commit: it must check the staged snapshot, not the working tree.

Each test clones this repository (read-only) into a temporary directory, installs the hook
from the working tree under test into the clone, and runs it there with GIT_* cleared. Only
the cheap checks run (layout check and secret scan): staged files are plain text files
outside docs/, scripts/, *.md, *.go and openapi.yaml.

PRE_COMMIT_HOOK_SOURCE may name another hook file to test (for example a copy of the
committed version).
"""
import hashlib
import os
import shutil
import subprocess
import tempfile
import unittest

import gitenv

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
HOOK_SOURCE = os.environ.get("PRE_COMMIT_HOOK_SOURCE") or os.path.join(
    REPO, "scripts", "hooks", "pre-commit")
SECRET = "sk" + "-ant-" + "A1b2C3d4" * 4  # built at runtime: no secret pattern in this file


class PreCommitSnapshotTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        home = os.path.join(tmp.name, "home")
        os.makedirs(home)
        self.env = gitenv.clean_env(home)
        self.clone = os.path.join(tmp.name, "clone")
        subprocess.run(["git", "clone", "--quiet", "--no-hardlinks", REPO, self.clone],
                       check=True, env=self.env, stdout=subprocess.PIPE,
                       stderr=subprocess.PIPE)
        self.git("config", "user.name", "Test User")
        self.git("config", "user.email", "test@example.com")
        self.hook = os.path.join(self.clone, "scripts", "hooks", "pre-commit")
        shutil.copyfile(HOOK_SOURCE, self.hook)
        os.chmod(self.hook, 0o755)
        # The installed hook differs from HEAD; keep it out of the staged set.
        self.git("update-index", "--assume-unchanged", "scripts/hooks/pre-commit")

    def git(self, *args):
        return subprocess.run(["git", *args], cwd=self.clone, check=True, env=self.env,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    def write(self, path, text):
        full = os.path.join(self.clone, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w") as f:
            f.write(text)

    def stage(self, path, text):
        self.write(path, text)
        self.git("add", path)

    def run_hook(self):
        return subprocess.run([self.hook], cwd=self.clone, env=self.env,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    def snapshot(self):
        files, dirs = {}, []
        for base, dnames, fnames in os.walk(self.clone):
            if ".git" in dnames:
                dnames.remove(".git")
            dnames.sort()
            for d in dnames:
                dirs.append(os.path.relpath(os.path.join(base, d), self.clone))
            for n in sorted(fnames):
                p = os.path.join(base, n)
                with open(p, "rb") as f:
                    files[os.path.relpath(p, self.clone)] = hashlib.sha256(f.read()).hexdigest()
        with open(os.path.join(self.clone, ".git", "config"), "rb") as f:
            config = f.read()
        return {
            "files": files, "dirs": sorted(dirs), "config": config,
            "index": self.git("ls-files", "-s").stdout,
            "gitdir": sorted(os.listdir(os.path.join(self.clone, ".git"))),
        }

    # a
    def test_untracked_layout_violation_does_not_fail_a_clean_staged_change(self):
        self.stage("src/agents/notes.txt", "a clean note\n")
        self.write("stray-root-file.txt", "untracked and forbidden at the root\n")
        r = self.run_hook()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    # b
    def test_staged_layout_violation_fails(self):
        self.stage("stray-root-file.txt", "forbidden at the root\n")
        r = self.run_hook()
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("stray-root-file.txt", r.stdout + r.stderr)

    # c
    def test_secret_in_staged_form_fails_even_if_working_tree_copy_is_clean(self):
        self.stage("src/agents/notes.txt", "token: %s\n" % SECRET)
        self.write("src/agents/notes.txt", "token removed\n")
        r = self.run_hook()
        self.assertNotEqual(r.returncode, 0)

    # d
    def test_secret_only_in_working_tree_copy_passes(self):
        self.stage("src/agents/notes.txt", "token removed\n")
        self.write("src/agents/notes.txt", "token: %s\n" % SECRET)
        r = self.run_hook()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    # control: the secret scan can fail at all on a plainly staged secret
    def test_secret_staged_and_in_working_tree_fails(self):
        self.stage("src/agents/notes.txt", "token: %s\n" % SECRET)
        self.assertNotEqual(self.run_hook().returncode, 0)

    # untracked secret file never matters
    def test_untracked_file_with_secret_passes(self):
        self.stage("src/agents/notes.txt", "clean\n")
        self.write("src/agents/untracked.txt", "token: %s\n" % SECRET)
        r = self.run_hook()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    # e
    def test_hook_leaves_working_tree_index_config_and_repo_unchanged(self):
        self.stage("src/agents/notes.txt", "staged version\n")
        self.write("src/agents/notes.txt", "working tree version\n")
        self.write("stray-root-file.txt", "untracked\n")
        before = self.snapshot()
        self.run_hook()
        self.assertEqual(self.snapshot(), before)
        # same for a failing run
        self.stage("stray-root-file.txt", "now staged\n")
        before = self.snapshot()
        self.assertNotEqual(self.run_hook().returncode, 0)
        self.assertEqual(self.snapshot(), before)

    def test_nothing_staged_passes(self):
        r = self.run_hook()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)


if __name__ == "__main__":
    unittest.main()
