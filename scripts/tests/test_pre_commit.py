"""Tests for scripts/hooks/pre-commit: it must check the staged snapshot, not the working tree.

Each test clones this repository (read-only) into a temporary directory, installs the hook
from the working tree under test into the clone, and runs it there with GIT_* cleared. Only
the cheap checks run (layout check and secret scan): staged files are plain text files
outside docs/, scripts/, *.md, *.go and openapi.yaml.

Run from the repository root with
`python3 -m unittest discover -s scripts/tests -p 'test_pre_commit.py'` (discover puts
scripts/tests on sys.path, which `import gitenv` needs; `unittest scripts/tests/test_pre_commit.py`
does not).

PRE_COMMIT_HOOK_SOURCE may name another pre-commit hook file to test (for example a copy of the
committed version); COMMIT_MSG_HOOK_SOURCE does the same for the commit-msg hook.

The second half (T009b and T009c) commits for real through core.hooksPath in the clone. Its
clone commits the hook under test (so the staged and committed hook is that file) and trims
scripts/tests/ to one trivial passing test. The hook runs no test suite (T108): the Python
check tests run in CI; the new tests of the quick checks are in test_pre_commit_quick.py.
git and docker (markdownlint runs in a container) are prerequisites: a missing one fails.
"""
import hashlib
import os
import shutil
import signal
import subprocess
import tempfile
import time
import unittest

import gitenv

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
HOOK_SOURCE = os.environ.get("PRE_COMMIT_HOOK_SOURCE") or os.path.join(
    REPO, "scripts", "hooks", "pre-commit")
COMMIT_MSG_SOURCE = os.environ.get("COMMIT_MSG_HOOK_SOURCE") or os.path.join(
    REPO, "scripts", "hooks", "commit-msg")
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
        # Commit the hook under test, so the staged and committed copy (the one that judges a
        # commit) is that file, and it stays out of the staged set.
        self.git("add", "scripts/hooks/pre-commit")
        if self.git("diff", "--cached", "--quiet", check=False).returncode:
            self.git("commit", "-q", "--no-verify", "-m", "test setup: hook under test")

    def git(self, *args, check=True):
        return subprocess.run(["git", *args], cwd=self.clone, check=check, env=self.env,
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


TRIVIAL_TEST = "import unittest\n\n\nclass T(unittest.TestCase):\n    def test_ok(self):\n        pass\n"
APPROVAL = "Approved-by: test"


class CloneCase(unittest.TestCase):
    """A throwaway clone with the hook under test committed, wired through core.hooksPath."""

    trim = True

    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.tmp = tmp.name
        home = os.path.join(self.tmp, "home")
        os.makedirs(home)
        self.env = gitenv.clean_env(home)
        self.tmproot = os.path.join(self.tmp, "tmproot")  # TMPDIR of the hook: leaks show here
        os.makedirs(self.tmproot)
        self.env["TMPDIR"] = self.tmproot
        self.clone = os.path.join(self.tmp, "clone")
        subprocess.run(["git", "clone", "--quiet", "--no-hardlinks", REPO, self.clone],
                       check=True, env=self.env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.git("config", "user.name", "Test User")
        self.git("config", "user.email", "test@example.com")
        self.git("config", "commit.gpgsign", "false")
        self.hook = os.path.join(self.clone, "scripts", "hooks", "pre-commit")
        shutil.copyfile(HOOK_SOURCE, self.hook)
        os.chmod(self.hook, 0o755)
        self.msg_hook = os.path.join(self.clone, "scripts", "hooks", "commit-msg")
        shutil.copyfile(COMMIT_MSG_SOURCE, self.msg_hook)
        os.chmod(self.msg_hook, 0o755)
        if self.trim:
            for name in os.listdir(os.path.join(self.clone, "scripts", "tests")):
                if name.startswith("test_") and name.endswith(".py"):
                    self.git("rm", "-q", "-f", "scripts/tests/" + name)
            self.write("scripts/tests/test_trivial.py", TRIVIAL_TEST)
            self.git("add", "scripts/tests/test_trivial.py")
        self.git("add", "scripts/hooks/pre-commit", "scripts/hooks/commit-msg")
        self.git("commit", "-q", "--no-verify", "-m", "test setup: hooks under test, trimmed tests")
        self.git("config", "core.hooksPath", os.path.join(self.clone, "scripts", "hooks"))

    def git(self, *args, check=True, env=None):
        return subprocess.run(["git", *args], cwd=self.clone, check=check, env=env or self.env,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    def write(self, path, text):
        full = os.path.join(self.clone, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w") as f:
            f.write(text)

    def stage(self, path, text):
        self.write(path, text)
        self.git("add", "--", path)

    def raw_commit(self, path, text):
        """Commit a file without running hooks (setup of tracked state)."""
        self.stage(path, text)
        self.git("commit", "-q", "--no-verify", "-m", "setup " + path)

    def commit(self, *args, env=None):
        """A real commit through core.hooksPath (pre-commit and commit-msg run)."""
        return self.git("commit", "-q", "-m", "test commit", "-m", APPROVAL, *args, check=False,
                        env=env)

    def run_hook(self, env=None):
        return subprocess.run([self.hook], cwd=self.clone, env=env or self.env,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    def head(self):
        return self.git("rev-parse", "HEAD").stdout.strip()

    def committed_files(self, rev="HEAD"):
        out = self.git("diff-tree", "--no-commit-id", "--name-only", "-r", "-z", "--root", rev).stdout
        return sorted(n for n in out.split("\0") if n)

    def leftovers(self):
        return [n for n in os.listdir(self.tmproot) if n.startswith("pre-commit")]

    def out(self, r):
        return r.stdout + r.stderr


class PrerequisitesTest(unittest.TestCase):
    def test_git_and_docker_are_installed(self):
        for tool in ("git", "docker"):
            self.assertIsNotNone(shutil.which(tool), tool + " is required and missing")
        r = subprocess.run(["docker", "version", "--format", "{{.Server.Version}}"],
                           stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.assertEqual(r.returncode, 0, "docker daemon not reachable: " + r.stderr)


class NamesTest(CloneCase):
    """T009b: staged names are passed NUL-separated, so odd names are still checked."""

    def test_secret_in_file_with_space_in_name_fails(self):
        self.stage("src/agents/my notes.txt", "token: %s\n" % SECRET)
        r = self.run_hook()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("my notes.txt", self.out(r))

    def test_secret_in_file_with_newline_in_name_fails(self):
        self.stage("src/agents/a\nb.txt", "token: %s\n" % SECRET)
        r = self.run_hook()
        self.assertNotEqual(r.returncode, 0, self.out(r))

    def test_file_with_space_and_newline_names_but_no_secret_passes(self):
        # control: the refusals above are about the secret, not about the odd names
        self.stage("src/agents/my notes.txt", "clean\n")
        self.stage("src/agents/a\nb.txt", "clean\n")
        r = self.run_hook()
        self.assertEqual(r.returncode, 0, self.out(r))

    def test_secret_with_space_name_refuses_a_real_commit(self):
        self.stage("src/agents/my notes.txt", "token: %s\n" % SECRET)
        before = self.head()
        self.assertNotEqual(self.commit().returncode, 0)
        self.assertEqual(self.head(), before)

    def test_markdown_lint_sees_a_name_with_a_space(self):
        # MD012 (two blank lines in a row) in "my notes.md"; split on the space the hook lints
        # two names that do not exist and the violation is never seen.
        self.stage("src/agents/my notes.md", "# Title\n\n\n\ntext\n")
        r = self.run_hook()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("MD012", self.out(r))

    def test_markdown_with_space_name_and_no_violation_passes(self):
        self.stage("src/agents/my notes.md", "# Title\n\ntext\n")
        r = self.run_hook()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertIn("markdown", self.out(r))

    def test_markdown_lint_sees_a_name_with_a_newline(self):
        self.stage("src/agents/a\nb.md", "# Title\n\n\n\ntext\n")
        r = self.run_hook()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("MD012", self.out(r))


class DeletionTest(CloneCase):
    """T009b: a commit that only deletes files still runs the checks on the snapshot."""

    def test_harmless_deletion_passes_and_reports_the_snapshot(self):
        self.raw_commit("src/agents/old.txt", "old\n")
        self.git("rm", "-q", "src/agents/old.txt")
        r = self.run_hook()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertIn("snapshot", r.stdout)
        self.assertIn("src/agents/old.txt", r.stdout)  # the deleted file is listed as staged

    def test_harmless_deletion_commits_for_real(self):
        self.raw_commit("src/agents/old.txt", "old\n")
        self.git("rm", "-q", "src/agents/old.txt")
        r = self.commit()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertIn("snapshot", self.out(r))
        self.assertEqual(self.git("ls-files", "src/agents/old.txt").stdout, "")

    def test_deleting_the_layout_check_is_refused(self):
        self.git("rm", "-q", "scripts/layout-check.py")
        before = self.head()
        r = self.run_hook()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertNotEqual(self.commit().returncode, 0)
        self.assertEqual(self.head(), before)

    def test_deleting_a_file_that_breaks_the_knowledge_bundle_is_refused(self):
        # docs/index.md links to docs/overview/glossary.md: the snapshot without it fails
        # `okf.py check` although nothing was added or modified.
        self.git("rm", "-q", "docs/overview/glossary.md")
        r = self.run_hook()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("knowledge bundle", r.stdout)


class ExcludesTest(CloneCase):
    """T009b: a global core.excludesFile must not drop tracked files from the snapshot repository."""

    def raw_commit_ignored(self, path, text):
        self.write(path, text)
        self.git("add", "-f", "--", path)
        self.git("commit", "-q", "--no-verify", "-m", "setup " + path)


class HookIdentityTest(CloneCase):
    """T009b: the hook that judges a commit is the staged one, not the working-tree copy."""

    def read_hook(self):
        with open(self.hook) as f:
            return f.read()

    def test_unstaged_edit_that_breaks_the_hook_does_not_decide_another_commit(self):
        good = self.read_hook()
        # Breakage at the very end: a shim that re-runs the committed hook near the top of the
        # file never reaches it; the working-tree hook as it stands reaches it after its checks.
        self.write("scripts/hooks/pre-commit",
                   good + "\necho 'WORKING-TREE-HOOK-BROKEN' >&2\nexit 1\n")
        self.stage("src/agents/notes.txt", "clean\n")
        before = self.head()
        r = self.commit()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertNotIn("WORKING-TREE-HOOK-BROKEN", self.out(r))
        self.assertNotEqual(self.head(), before)
        # the unstaged edit is left alone
        self.assertIn("WORKING-TREE-HOOK-BROKEN", self.read_hook())

    def test_unstaged_secret_scan_removal_does_not_weaken_the_staged_hook(self):
        # A half-edited working-tree hook that no longer scans for secrets must not let a
        # staged secret through.
        good = self.read_hook()
        self.write("scripts/hooks/pre-commit", good.replace("sk-ant-", "sk-nothing-"))
        self.stage("src/agents/notes.txt", "token: %s\n" % SECRET)
        before = self.head()
        self.assertNotEqual(self.commit().returncode, 0)
        self.assertEqual(self.head(), before)

    def test_staged_broken_hook_fails_the_commit_even_if_the_working_copy_is_good(self):
        good = self.read_hook()
        broken = good + "\necho 'STAGED-HOOK-BROKEN' >&2\nexit 1\n"
        self.stage("scripts/hooks/pre-commit", broken)
        self.write("scripts/hooks/pre-commit", good)  # the working copy is fine; the staged one is not
        before = self.head()
        r = self.commit()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("STAGED-HOOK-BROKEN", self.out(r))
        self.assertEqual(self.head(), before)

    def test_staged_broken_hook_fails_when_working_copy_is_also_broken(self):
        # control: the failure is observable at all (passes before and after T009b)
        broken = self.read_hook() + "\necho 'STAGED-HOOK-BROKEN' >&2\nexit 1\n"
        self.stage("scripts/hooks/pre-commit", broken)
        r = self.commit()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("STAGED-HOOK-BROKEN", self.out(r))


class RealCommitTest(CloneCase):
    """T009c: real `git commit -- <path>` and `git commit -a` (git's temporary index)."""

    def test_commit_path_ignores_unrelated_staged_secret_and_unstaged_edits(self):
        self.raw_commit("src/agents/a.txt", "a\n")
        self.raw_commit("src/agents/b.txt", "b\n")
        self.raw_commit("src/agents/c.txt", "c\n")
        self.stage("src/agents/a.txt", "staged but unrelated: %s\n" % SECRET)  # not in this commit
        self.write("src/agents/c.txt", "unstaged edit: %s\n" % SECRET)         # not in this commit
        self.write("stray-root-file.txt", "untracked\n")                        # not in this commit
        self.write("src/agents/b.txt", "b changed\n")
        r = self.commit("--", "src/agents/b.txt")
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertEqual(self.committed_files(), ["src/agents/b.txt"])
        # what was not committed is still where it was
        self.assertEqual(self.git("diff", "--cached", "--name-only").stdout.split(), ["src/agents/a.txt"])

    def test_commit_path_is_judged_on_that_paths_working_content(self):
        self.raw_commit("src/agents/b.txt", "b\n")
        self.write("src/agents/b.txt", "token: %s\n" % SECRET)
        before = self.head()
        r = self.commit("--", "src/agents/b.txt")
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertEqual(self.head(), before)

    def test_commit_path_does_not_judge_other_staged_content_that_would_fail(self):
        # a staged layout violation outside the path is not committed, so it must not refuse
        self.raw_commit("src/agents/b.txt", "b\n")
        self.stage("stray-root-file.txt", "forbidden at the root, staged, not committed here\n")
        self.write("src/agents/b.txt", "b changed\n")
        r = self.commit("--", "src/agents/b.txt")
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertEqual(self.committed_files(), ["src/agents/b.txt"])

    def test_commit_all_judges_unstaged_tracked_edit_with_secret(self):
        self.raw_commit("src/agents/a.txt", "a\n")
        self.write("src/agents/a.txt", "token: %s\n" % SECRET)
        before = self.head()
        r = self.commit("-a")
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertEqual(self.head(), before)

    def test_commit_all_ignores_untracked_files_and_commits_tracked_edits(self):
        self.raw_commit("src/agents/a.txt", "a\n")
        self.write("src/agents/a.txt", "a changed\n")
        self.write("stray-root-file.txt", "untracked and forbidden\n")
        self.write("src/agents/untracked.txt", "token: %s\n" % SECRET)
        r = self.commit("-a")
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertEqual(self.committed_files(), ["src/agents/a.txt"])

    def test_commit_all_includes_unstaged_deletion(self):
        self.raw_commit("src/agents/a.txt", "a\n")
        os.remove(os.path.join(self.clone, "src/agents/a.txt"))
        r = self.commit("-a")
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertIn("snapshot", self.out(r))
        self.assertEqual(self.committed_files(), ["src/agents/a.txt"])
        self.assertEqual(self.git("ls-files", "src/agents/a.txt").stdout, "")

    def test_plain_commit_with_staged_secret_is_refused(self):
        # control for the cases above: the real-commit path can refuse at all
        self.stage("src/agents/a.txt", "token: %s\n" % SECRET)
        before = self.head()
        self.assertNotEqual(self.commit().returncode, 0)
        self.assertEqual(self.head(), before)


class TempFoldersTest(CloneCase):
    """T009c: every $TMPDIR/pre-commit* entry (snapshot folder, hook copy) is removed on success, failure and SIGTERM."""

    def test_removed_after_success(self):
        self.stage("src/agents/a.txt", "clean\n")
        r = self.run_hook()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertIn(self.tmproot, r.stdout)  # the snapshot really was made under TMPDIR
        self.assertEqual(self.leftovers(), [])

    def test_removed_after_failure(self):
        self.stage("src/agents/a.txt", "token: %s\n" % SECRET)
        r = self.run_hook()
        self.assertNotEqual(r.returncode, 0)
        self.assertIn(self.tmproot, r.stdout)
        self.assertEqual(self.leftovers(), [])

    def test_removed_after_success_and_failure_in_real_commits(self):
        self.stage("src/agents/a.txt", "clean\n")
        self.assertEqual(self.commit().returncode, 0)
        self.assertEqual(self.leftovers(), [])
        self.stage("src/agents/b.txt", "token: %s\n" % SECRET)
        self.assertNotEqual(self.commit().returncode, 0)
        self.assertEqual(self.leftovers(), [])



class ScriptTestsTest(CloneCase):
    """T009c: a failing script test fails the hook."""

    def test_staged_passing_script_test_passes_the_hook(self):
        # control: same path, nothing planted
        self.stage("scripts/tests/test_passes.py", TRIVIAL_TEST)
        r = self.commit()
        self.assertEqual(r.returncode, 0, self.out(r))

    def test_unstaged_failing_script_test_does_not_fail_the_hook(self):
        self.stage("scripts/tests/test_passes.py", TRIVIAL_TEST)
        self.write("scripts/tests/test_unstaged_fails.py",
                   "import unittest\n\n\nclass T(unittest.TestCase):\n    def test_f(self):\n        self.fail()\n")
        r = self.commit()
        self.assertEqual(r.returncode, 0, self.out(r))


class SpecialEntriesTest(CloneCase):
    """T009c: renames, symlinks and gitlinks do not crash the hook."""

    def test_staged_rename_passes(self):
        self.raw_commit("src/agents/old-name.txt", "content\n")
        self.git("mv", "src/agents/old-name.txt", "src/agents/new-name.txt")
        r = self.commit()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertEqual(self.git("ls-files", "src/agents/new-name.txt").stdout.strip(),
                         "src/agents/new-name.txt")
        self.assertNotIn("Traceback", self.out(r))

    def test_staged_rename_with_a_secret_added_is_refused(self):
        self.raw_commit("src/agents/old-name.txt", "content\n")
        self.git("mv", "src/agents/old-name.txt", "src/agents/new-name.txt")
        self.stage("src/agents/new-name.txt", "content %s\n" % SECRET)
        before = self.head()
        self.assertNotEqual(self.commit().returncode, 0)
        self.assertEqual(self.head(), before)

    def test_staged_symlinks_pass(self):
        os.makedirs(os.path.join(self.clone, "src/agents"), exist_ok=True)
        self.stage("src/agents/target.txt", "clean\n")
        os.symlink("target.txt", os.path.join(self.clone, "src/agents/link-file"))
        os.symlink("does-not-exist", os.path.join(self.clone, "src/agents/link-dangling"))
        os.symlink(".", os.path.join(self.clone, "src/agents/link-dir"))
        self.git("add", "src/agents/link-file", "src/agents/link-dangling", "src/agents/link-dir")
        r = self.commit()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertNotIn("Traceback", self.out(r))
        self.assertTrue(self.git("ls-tree", "HEAD", "src/agents/link-dangling").stdout.startswith("120000"))

    def test_staged_dangling_markdown_symlink_does_not_crash(self):
        os.symlink("does-not-exist.md", os.path.join(self.clone, "src/agents/dangling.md"))
        self.git("add", "src/agents/dangling.md")
        r = self.commit()
        self.assertEqual(r.returncode, 0, self.out(r))

    def test_staged_symlink_to_a_secret_file_checks_the_file_that_is_staged_with_it(self):
        # the file is refused by itself; the link must not make the failure disappear
        self.stage("src/agents/target.txt", "token: %s\n" % SECRET)
        os.symlink("target.txt", os.path.join(self.clone, "src/agents/link-file"))
        self.git("add", "src/agents/link-file")
        self.assertNotEqual(self.commit().returncode, 0)

    def test_staged_dangling_symlink_with_a_secret_elsewhere_is_refused(self):
        # grep exits 2 on an unreadable entry even when it found a match; xargs then reports
        # 123 and a naive `if ... | xargs grep` reads that as "no secret".
        self.stage("src/agents/a.txt", "token: %s\n" % SECRET)
        os.symlink("does-not-exist", os.path.join(self.clone, "src/agents/link-dangling"))
        self.git("add", "src/agents/link-dangling")
        before = self.head()
        self.assertNotEqual(self.commit().returncode, 0)
        self.assertEqual(self.head(), before)

    def test_staged_gitlink_passes(self):
        self.git("update-index", "--add", "--cacheinfo", "160000,%s,src/agents/sub" % self.head())
        r = self.commit()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertNotIn("Traceback", self.out(r))
        self.assertTrue(self.git("ls-tree", "HEAD", "src/agents/sub").stdout.startswith("160000"))

    def test_staged_gitlink_with_script_change_passes(self):
        # a gitlink in the commit does not stop the hook
        self.git("update-index", "--add", "--cacheinfo", "160000,%s,src/agents/sub" % self.head())
        self.stage("scripts/tests/test_passes.py", TRIVIAL_TEST)
        r = self.commit()
        self.assertEqual(r.returncode, 0, self.out(r))

    def test_staged_gitlink_with_a_secret_elsewhere_is_refused(self):
        self.git("update-index", "--add", "--cacheinfo", "160000,%s,src/agents/sub" % self.head())
        self.stage("src/agents/a.txt", "token: %s\n" % SECRET)
        self.assertNotEqual(self.commit().returncode, 0)


class GoVersionGuardTest(CloneCase):
    """T009c: scripts/check.sh --go-only compares the golang and golang-race pins.

    Docker is replaced by a stub on PATH and scripts/generate.sh by a no-op in the clone: the
    guard is the code under test, and the pinned images (and network) are not needed. The stub
    answers `go env GOVERSION` per image and succeeds for every other call. The control case
    (same versions) runs the rest of go_checks to the end, so a guard that always failed would
    be caught.
    """

    def setUp(self):
        super().setUp()
        self.lock = os.path.join(self.clone, "src", "deploy", "images.lock")
        with open(self.lock) as f:
            self.pins = {l.split()[0]: l.split()[1] for l in f if len(l.split()) >= 2}
        self.assertIn("golang", self.pins)
        self.assertIn("golang-race", self.pins)
        gen = os.path.join(self.clone, "scripts", "generate.sh")
        with open(gen, "w") as f:
            f.write("#!/bin/sh\nexit 0\n")
        os.chmod(gen, 0o755)
        self.bin = os.path.join(self.tmp, "bin")
        os.makedirs(self.bin)
        stub = os.path.join(self.bin, "docker")
        with open(stub, "w") as f:
            f.write('#!/bin/sh\n'
                    'case "$*" in *"go env GOVERSION"*)\n'
                    '  case "$*" in *"$STUB_RACE_IMAGE"*) echo "$STUB_RACE_VERSION";; *) echo "$STUB_GO_VERSION";; esac;;\n'
                    'esac\nexit 0\n')
        os.chmod(stub, 0o755)

    def check(self, go_version, race_version):
        env = dict(self.env)
        env["PATH"] = self.bin + os.pathsep + env["PATH"]
        env["STUB_RACE_IMAGE"] = self.pins["golang-race"]
        env["STUB_GO_VERSION"] = go_version
        env["STUB_RACE_VERSION"] = race_version
        return subprocess.run(["sh", "scripts/check.sh", "--go-only"], cwd=self.clone, env=env,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    def test_same_versions_pass(self):
        r = self.check("go1.25.3", "go1.25.3")
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertIn("go test -race (go1.25.3", r.stdout)

    def test_different_versions_fail(self):
        r = self.check("go1.25.3", "go1.24.9")
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("golang is go1.25.3 but golang-race is go1.24.9", r.stderr)
        self.assertNotIn("go test -race", r.stdout)

    def test_missing_race_pin_fails(self):
        with open(self.lock) as f:
            lines = f.readlines()
        with open(self.lock, "w") as f:
            f.writelines(l for l in lines if not l.startswith("golang-race"))
        r = self.check("go1.25.3", "go1.25.3")
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("no golang-race pin", r.stderr)

    def test_empty_race_pin_fails(self):
        with open(self.lock) as f:
            lines = f.readlines()
        with open(self.lock, "w") as f:
            f.writelines(("golang-race\n" if l.startswith("golang-race") else l) for l in lines)
        r = self.check("go1.25.3", "go1.25.3")
        self.assertNotEqual(r.returncode, 0, self.out(r))

def real(name):
    path = shutil.which(name)
    if path is None:
        raise AssertionError("%s is a prerequisite" % name)
    return path


class FailClosedToolsTest(CloneCase):
    """T009d: a failing tool is a failing hook, never "no secret found"."""

    def with_stubs(self, stubs):
        return gitenv.with_path(self.env, gitenv.stub_dir(self, stubs))

    def refused(self, stubs, staged=None):
        self.stage(*(staged or ("src/agents/a.txt", "clean, no secret here\n")))
        r = self.run_hook(env=self.with_stubs(stubs))
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertNotIn("staged snapshot passed", self.out(r))
        self.assertTrue(self.out(r).strip(), "a refusal needs a message")
        self.assertEqual(self.leftovers(), [])

    def test_control_pass_through_stubs_still_pass(self):
        stubs = {"grep": 'exec %s "$@"\n' % real("grep"), "xargs": 'exec %s "$@"\n' % real("xargs")}
        self.stage("src/agents/a.txt", "clean\n")
        r = self.run_hook(env=self.with_stubs(stubs))
        self.assertEqual(r.returncode, 0, self.out(r))

    def test_control_pass_through_stubs_still_find_a_secret(self):
        stubs = {"grep": 'exec %s "$@"\n' % real("grep"), "xargs": 'exec %s "$@"\n' % real("xargs")}
        self.stage("src/agents/a.txt", "token: %s\n" % SECRET)
        self.assertNotEqual(self.run_hook(env=self.with_stubs(stubs)).returncode, 0)

    def test_grep_exit_127_refuses(self):
        self.refused({"grep": "exit 127\n"})

    def test_grep_exit_126_refuses(self):
        self.refused({"grep": "exit 126\n"})

    def test_grep_killed_by_a_signal_refuses(self):
        self.refused({"grep": "kill -KILL $$\n"})

    def test_xargs_exit_127_refuses(self):
        self.refused({"xargs": "exit 127\n"})

    def test_xargs_exit_126_refuses(self):
        self.refused({"xargs": "exit 126\n"})

    def test_xargs_killed_by_a_signal_refuses(self):
        self.refused({"xargs": "kill -KILL $$\n"})

    def test_tool_failure_refuses_a_real_commit(self):
        self.stage("src/agents/a.txt", "clean\n")
        before = self.head()
        r = self.commit(env=self.with_stubs({"xargs": "exit 127\n"}))
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertEqual(self.head(), before)


class SortingStepWriteFailureTest(CloneCase):
    """T009d review: the step that sorts staged entries into files and links fails closed."""

    def stub_mktemp(self, name):
        # The hook's work folder gets `name` (files or links) as a link to /dev/full: every
        # write to it fails with ENOSPC and the file looks empty (size 0) to `[ -s ]`.
        body = (
            'out=$(%s "$@") || exit $?\n'
            'case "$*" in *"/pre-commit.XXXXXX"*) ln -s /dev/full "$out/%s";; esac\n'
            'printf "%%s\\n" "$out"\n' % (real("mktemp"), name))
        return gitenv.with_path(self.env, gitenv.stub_dir(self, {"mktemp": body}))

    def test_control_pass_through_mktemp_passes(self):
        self.stage("src/agents/a.txt", "clean\n")
        body = 'exec %s "$@"\n' % real("mktemp")
        env = gitenv.with_path(self.env, gitenv.stub_dir(self, {"mktemp": body}))
        r = self.run_hook(env=env)
        self.assertEqual(r.returncode, 0, self.out(r))

    def test_failed_write_to_the_regular_file_list_is_not_masked_by_a_later_link(self):
        self.stage("src/agents/a.txt", "token: %s\n" % SECRET)
        os.symlink("harmless", os.path.join(self.clone, "src/agents/z-link"))
        self.git("add", "--", "src/agents/z-link")
        r = self.run_hook(env=self.stub_mktemp("files"))
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertNotIn("staged snapshot passed", self.out(r))
        self.assertEqual(self.leftovers(), [])

    def test_failed_write_to_the_regular_file_list_with_a_clean_file_refuses(self):
        self.stage("src/agents/a.txt", "clean\n")
        r = self.run_hook(env=self.stub_mktemp("files"))
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertNotIn("staged snapshot passed", self.out(r))

    def test_failed_write_to_the_link_list_is_not_masked_by_a_later_file(self):
        self.write("src/agents/placeholder", "x\n")
        os.symlink("elsewhere-" + SECRET, os.path.join(self.clone, "src/agents/a-link"))
        self.git("add", "--", "src/agents/a-link")
        self.stage("src/agents/z.txt", "clean\n")
        r = self.run_hook(env=self.stub_mktemp("links"))
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertNotIn("staged snapshot passed", self.out(r))


class CraftedZeroTest(CloneCase):
    """T009d review: a hook run by a crafted path never deletes a folder outside its own."""

    def crafted(self, hook, variant, *args):
        victim = os.path.join(self.tmproot, "victim")
        os.makedirs(victim)
        os.makedirs(os.path.join(self.tmproot, "%s-hook.a" % os.path.basename(hook)))
        with open(os.path.join(victim, "keep.txt"), "w") as f:
            f.write("precious\n")
        with open(hook) as f:
            text = f.read()
        path = os.path.join(victim, os.path.basename(hook))
        with open(path, "w") as f:
            f.write(text + variant)
        os.chmod(path, 0o755)
        zero = os.path.join(self.tmproot, "%s-hook.a" % os.path.basename(hook), "..", "victim",
                            os.path.basename(hook))
        r = subprocess.run([zero, *args], cwd=self.clone, env=self.env, stdout=subprocess.PIPE,
                           stderr=subprocess.PIPE, text=True)
        self.assertTrue(os.path.exists(os.path.join(victim, "keep.txt")), self.out(r))
        return r

    def test_pre_commit_identical_to_the_staged_copy(self):
        self.crafted(self.hook, "")

    def test_pre_commit_differing_from_the_staged_copy(self):
        self.crafted(self.hook, "\n# differs\n")

    def test_commit_msg_identical_to_the_staged_copy(self):
        msg = os.path.join(self.tmp, "msg.txt")
        with open(msg, "w") as f:
            f.write("docs: x\n")
        self.crafted(self.msg_hook, "", msg)

    def test_commit_msg_differing_from_the_staged_copy(self):
        msg = os.path.join(self.tmp, "msg.txt")
        with open(msg, "w") as f:
            f.write("docs: x\n")
        self.crafted(self.msg_hook, "\n# differs\n", msg)


class SymlinkScanTest(CloneCase):
    """T009d: a staged symlink's target text is scanned; a link out of the repository is not followed."""

    def outside(self, text):
        path = os.path.join(self.tmp, "outside-secret.txt")
        with open(path, "w") as f:
            f.write(text)
        return path

    def link(self, name, target):
        os.symlink(target, os.path.join(self.clone, name))
        self.git("add", "--", name)

    def test_symlink_to_an_outside_file_with_a_secret_is_not_followed(self):
        target = self.outside("token: %s\n" % SECRET)
        self.link("src/agents/link-out", target)
        r = self.run_hook()
        self.assertNotIn(SECRET, self.out(r))
        self.assertEqual(r.returncode, 0, self.out(r))

    def test_relative_symlink_out_of_the_repository_is_not_followed(self):
        target = self.outside("token: %s\n" % SECRET)
        rel = os.path.relpath(target, os.path.join(self.clone, "src", "agents"))
        self.link("src/agents/link-out", rel)
        r = self.run_hook()
        self.assertNotIn(SECRET, self.out(r))
        self.assertEqual(r.returncode, 0, self.out(r))

    def test_the_decision_does_not_depend_on_the_outside_content(self):
        target = self.outside("nothing here\n")
        self.link("src/agents/link-out", target)
        clean = self.run_hook()
        self.assertEqual(clean.returncode, 0, self.out(clean))
        self.outside("token: %s\n" % SECRET)
        secret = self.run_hook()
        self.assertEqual(secret.returncode, clean.returncode, self.out(secret))
        self.assertNotIn(SECRET, self.out(secret))

    def test_symlink_whose_target_text_holds_a_secret_is_refused(self):
        self.link("src/agents/link-text", "elsewhere-" + SECRET)
        r = self.run_hook()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("secret", self.out(r))

    def test_symlink_whose_target_text_holds_a_secret_refuses_a_real_commit(self):
        self.link("src/agents/link-text", "elsewhere-" + SECRET)
        before = self.head()
        self.assertNotEqual(self.commit().returncode, 0)
        self.assertEqual(self.head(), before)

    def test_control_symlink_with_a_harmless_target_text_passes(self):
        self.link("src/agents/link-text", "elsewhere-harmless")
        r = self.run_hook()
        self.assertEqual(r.returncode, 0, self.out(r))


class HandoverCallerEnvTest(CloneCase):
    """T009d: AP_PRE_COMMIT_STAGED_HOOK set by the caller does not skip the handover."""

    def broken_staged_hook(self):
        with open(self.hook) as f:
            good = f.read()
        self.stage("scripts/hooks/pre-commit", good + "\necho 'STAGED-HOOK-BROKEN' >&2\nexit 1\n")
        self.write("scripts/hooks/pre-commit", good)

    def test_caller_variable_does_not_skip_the_handover_in_a_real_commit(self):
        self.broken_staged_hook()
        before = self.head()
        env = dict(self.env, AP_PRE_COMMIT_STAGED_HOOK="1")
        r = self.commit(env=env)
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("STAGED-HOOK-BROKEN", self.out(r))
        self.assertEqual(self.head(), before)

    def test_caller_variable_does_not_skip_the_handover_when_run_directly(self):
        self.broken_staged_hook()
        r = self.run_hook(env=dict(self.env, AP_PRE_COMMIT_STAGED_HOOK="1"))
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("STAGED-HOOK-BROKEN", self.out(r))

    def test_caller_variable_does_not_hide_a_staged_secret(self):
        self.stage("src/agents/a.txt", "token: %s\n" % SECRET)
        r = self.run_hook(env=dict(self.env, AP_PRE_COMMIT_STAGED_HOOK="1"))
        self.assertNotEqual(r.returncode, 0, self.out(r))


class HandoverSigtermTest(CloneCase):
    """T009d: SIGTERM while the handed-over hook runs leaves no temp folders and refuses the commit."""



class CommitMsgHandoverTest(CloneCase):
    """T009d: the staged scripts/hooks/commit-msg judges the message, through a real git commit."""

    REFUSE = "#!/bin/sh\necho 'STAGED-COMMIT-MSG-REFUSES' >&2\nexit 1\n"

    def read_msg_hook(self):
        with open(self.msg_hook) as f:
            return f.read()

    def test_unstaged_broken_commit_msg_does_not_decide_a_commit(self):
        good = self.read_msg_hook()
        self.write("scripts/hooks/commit-msg", good + "\necho 'WORKING-COMMIT-MSG-BROKEN' >&2\nexit 1\n")
        self.stage("src/agents/notes.txt", "clean\n")
        before = self.head()
        r = self.commit()
        self.assertEqual(r.returncode, 0, self.out(r))
        self.assertNotIn("WORKING-COMMIT-MSG-BROKEN", self.out(r))
        self.assertNotEqual(self.head(), before)
        self.assertIn("WORKING-COMMIT-MSG-BROKEN", self.read_msg_hook())

    def test_unstaged_weakened_commit_msg_does_not_waive_the_approval(self):
        # the working copy no longer treats scripts/ as protected; the staged one still does
        # (the edit is after the handover block: an edit to the block itself takes effect)
        weak = self.read_msg_hook().replace("scripts/|", "no-such-dir/|")
        self.assertNotEqual(weak, self.read_msg_hook())
        self.write("scripts/hooks/commit-msg", weak)
        self.stage("scripts/x.sh", "echo x\n")
        before = self.head()
        r = self.git("commit", "-q", "-m", "no trailer", check=False)
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertEqual(self.head(), before)

    def test_staged_commit_msg_that_refuses_everything_refuses(self):
        good = self.read_msg_hook()
        self.stage("scripts/hooks/commit-msg", self.REFUSE)
        self.write("scripts/hooks/commit-msg", good)  # the working copy is fine
        before = self.head()
        r = self.commit()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("STAGED-COMMIT-MSG-REFUSES", self.out(r))
        self.assertEqual(self.head(), before)

    def test_staged_commit_msg_that_refuses_everything_refuses_when_working_copy_is_too(self):
        # control: the refusal is observable at all
        self.stage("scripts/hooks/commit-msg", self.REFUSE)
        r = self.commit()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("STAGED-COMMIT-MSG-REFUSES", self.out(r))

    def test_protected_path_without_approved_by_is_refused(self):
        self.stage("scripts/x.sh", "echo x\n")
        before = self.head()
        r = self.git("commit", "-q", "-m", "no trailer", check=False)
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("scripts/x.sh", self.out(r))
        self.assertEqual(self.head(), before)

    def test_protected_path_with_approved_by_commits(self):
        self.stage("scripts/x.sh", "#!/bin/sh\necho x\n")
        r = self.commit()
        self.assertEqual(r.returncode, 0, self.out(r))

    def test_unprotected_path_commits_without_a_trailer(self):
        self.stage("docs/notes-unprotected.txt", "x\n")
        r = self.git("commit", "-q", "-m", "plain", check=False)
        self.assertEqual(r.returncode, 0, self.out(r))

    def test_handed_over_commit_msg_still_gets_the_message_file(self):
        # a staged copy that differs only by a comment must judge the same message
        self.stage("scripts/hooks/commit-msg", self.read_msg_hook() + "\n# staged variant\n")
        self.write("scripts/hooks/commit-msg", self.read_msg_hook())
        before = self.head()
        r = self.git("commit", "-q", "-m", "no trailer", check=False)
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertEqual(self.head(), before)
        r = self.commit()
        self.assertEqual(r.returncode, 0, self.out(r))


if __name__ == "__main__":
    unittest.main()
