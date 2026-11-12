"""T010e: scripts/hooks/commit-msg enforces ADR 0029 (existing tests change only with the owner's
approval), through real `git commit` in throwaway repositories whose core.hooksPath holds a copy
of the hook."""
import os
import shutil
import subprocess
import tempfile
import unittest

import gitenv

HOOK = os.environ.get("COMMIT_MSG_HOOK_SOURCE") or os.path.abspath(
    os.path.join(os.path.dirname(__file__), "..", "hooks", "commit-msg")
)
OWNER = "Approved-by: Owner (2026-11-13)"
TESTS_OK = "Tests-approved-by: Owner Name (2026-11-13)"

# One existing file per protected test kind.
EXISTING = [
    "src/engine/a_test.go",
    "src/engine/checks/testdata/case/in.json",
    "scripts/tests/fixtures/f.txt",
    "scripts/tests/helper.txt",
    "src/tests/evals/e1.yaml",
    "src/tests/recordings/r1.json",
    "scripts/test_thing.sh",
    "tools/test_thing.py",
    "tools/test_other.sh",
]


class TestsApprovalTest(unittest.TestCase):
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
        for p in EXISTING:
            self.write(p, "original %s\n" % p)
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

    def commit(self, message):
        return self.git("commit", "-q", "-m", message, check=False)

    def modify(self, path):
        self.write(path, "changed %s\n" % path)
        self.git("add", path)

    def msg(self, *trailers):
        return "chore: change\n\nBody text.\n\n" + "\n".join(trailers) + "\n"

    def assert_refused_naming(self, r, path):
        self.assertNotEqual(r.returncode, 0, "commit should be refused: " + r.stdout)
        self.assertIn(path, r.stderr)

    def test_modifying_each_kind_of_existing_test_is_refused_naming_it(self):
        for p in EXISTING:
            with self.subTest(path=p):
                self.setUp()
                self.modify(p)
                r = self.commit(self.msg(OWNER))
                self.assert_refused_naming(r, p)

    def test_modifying_each_kind_with_trailer_passes(self):
        for p in EXISTING:
            with self.subTest(path=p):
                self.setUp()
                self.modify(p)
                r = self.commit(self.msg(OWNER, TESTS_OK))
                self.assertEqual(r.returncode, 0, r.stderr)

    def test_deleting_existing_test_is_refused_then_passes_with_trailer(self):
        self.git("rm", "-q", "src/engine/a_test.go")
        r = self.commit(self.msg(OWNER))
        self.assert_refused_naming(r, "src/engine/a_test.go")
        r = self.commit(self.msg(OWNER, TESTS_OK))
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_deleting_testdata_file_is_refused(self):
        self.git("rm", "-q", "src/engine/checks/testdata/case/in.json")
        r = self.commit(self.msg(OWNER))
        self.assert_refused_naming(r, "src/engine/checks/testdata/case/in.json")

    def test_renaming_test_away_from_test_name_is_refused_naming_old_file(self):
        self.git("mv", "tools/test_thing.py", "tools/helper.py")
        r = self.commit(self.msg())
        self.assert_refused_naming(r, "tools/test_thing.py")

    def test_renaming_test_to_another_test_name_is_refused(self):
        self.git("mv", "src/engine/a_test.go", "src/engine/b_test.go")
        r = self.commit(self.msg(OWNER))
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("src/engine/a_test.go", r.stderr)

    def test_renaming_test_with_trailer_passes(self):
        self.git("mv", "src/engine/a_test.go", "src/engine/b_test.go")
        r = self.commit(self.msg(OWNER, TESTS_OK))
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_renaming_non_test_file_into_test_name_is_not_an_existing_test_change(self):
        self.write("tools/plain.py", "x\n")
        self.git("add", "tools/plain.py")
        self.git("commit", "-q", "--no-verify", "-m", "plain")
        self.git("mv", "tools/plain.py", "tools/test_plain.py")
        r = self.commit(self.msg())
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_only_adding_test_files_passes_without_trailer(self):
        new = ["src/engine/new_test.go", "src/engine/checks/testdata/new/in.json",
               "scripts/tests/test_new.py", "src/tests/evals/new.yaml",
               "tools/test_new.sh", "tools/test_new.py"]
        for p in new:
            self.write(p, "new\n")
            self.git("add", p)
        r = self.commit(self.msg(OWNER))
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_non_test_change_passes_without_trailer(self):
        self.write("docs/notes.md", "n\n")
        self.git("add", "docs/notes.md")
        r = self.commit("docs: notes\n")
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_all_existing_files_listed_in_refusal(self):
        paths = ["src/engine/a_test.go", "scripts/tests/fixtures/f.txt", "tools/test_other.sh"]
        for p in paths:
            self.modify(p)
        r = self.commit(self.msg(OWNER))
        self.assertNotEqual(r.returncode, 0)
        for p in paths:
            self.assertIn(p, r.stderr)

    def test_trailer_in_body_above_final_block_does_not_count(self):
        self.modify("src/engine/a_test.go")
        message = ("chore: change\n\n%s\n\nMore text in a later paragraph.\n\n%s\n"
                   % (TESTS_OK, OWNER))
        r = self.commit(message)
        self.assert_refused_naming(r, "src/engine/a_test.go")

    def test_trailer_without_date_is_refused(self):
        self.modify("src/engine/a_test.go")
        r = self.commit(self.msg(OWNER, "Tests-approved-by: Owner Name"))
        self.assert_refused_naming(r, "src/engine/a_test.go")

    def test_trailer_with_malformed_date_is_refused(self):
        self.modify("src/engine/a_test.go")
        r = self.commit(self.msg(OWNER, "Tests-approved-by: Owner Name (13-11-2026)"))
        self.assert_refused_naming(r, "src/engine/a_test.go")

    def test_trailer_with_empty_name_is_refused(self):
        self.modify("src/engine/a_test.go")
        r = self.commit(self.msg(OWNER, "Tests-approved-by:  (2026-11-13)"))
        self.assert_refused_naming(r, "src/engine/a_test.go")

    def test_approved_by_alone_does_not_approve_test_changes(self):
        self.modify("scripts/tests/helper.txt")
        r = self.commit(self.msg(OWNER))
        self.assert_refused_naming(r, "scripts/tests/helper.txt")

    def test_mixed_commit_changing_a_test_and_non_test_is_refused(self):
        self.write("docs/notes.md", "n\n")
        self.git("add", "docs/notes.md")
        self.modify("tools/test_thing.py")
        r = self.commit(self.msg())
        self.assert_refused_naming(r, "tools/test_thing.py")

    def test_smoke_existing_rule_protected_path_still_needs_approved_by(self):
        self.write("scripts/x.sh", "x\n")
        self.git("add", "scripts/x.sh")
        r = self.commit("chore: script\n")
        self.assert_refused_naming(r, "scripts/x.sh")


if __name__ == "__main__":
    unittest.main()
