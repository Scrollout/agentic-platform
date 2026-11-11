"""Tests for scripts/hooks/commit-msg, run against throwaway git repositories."""
import os
import subprocess
import tempfile
import unittest

HOOK = os.path.abspath(
    os.path.join(os.path.dirname(__file__), "..", "hooks", "commit-msg")
)


class CommitMsgHookTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.repo = self._tmp.name
        self.git("init", "-q")
        self.git("config", "user.name", "Test User")
        self.git("config", "user.email", "test@example.com")

    def git(self, *args):
        subprocess.run(["git", *args], cwd=self.repo, check=True,
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
        return subprocess.run([HOOK, msg_file], cwd=self.repo,
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


if __name__ == "__main__":
    unittest.main()
