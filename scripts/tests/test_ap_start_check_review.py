"""Regression tests for a defect a fresh review of user story 2 found in `ap start --check` (2026-11-18).

Source: the fresh review of US2 of feature 003, 2026-11-18.
Contract: src/cli/contract.md, "Starting the platform" (`--check` makes no projects folder, prints
`start: would make the projects folder <folder>` when the default one is missing, exits 1 when
something would change). Spec: specs/003-one-command-start/spec.md FR-018 (`--check` changes nothing)
and FR-006 (the default projects folder is made when missing).
Reuses the harness of test_ap_start.py (stub `docker`, no real Docker). HOME and XDG_CONFIG_HOME are
temporary folders, so the defaults point inside the test.
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from test_ap_start import StartCase  # noqa: E402


class StartCheckDefaultsTest(StartCase):
    def setUp(self):
        super().setUp()
        self.default_cred = self.config / "agentic-platform/anthropic_api_key"
        self.default_proj = self.home / "projects"
        self.default_cred.parent.mkdir(parents=True, exist_ok=True)
        self.default_cred.write_bytes(self.cred.read_bytes())
        self.default_cred.chmod(0o600)
        self.make_current()

    def check(self):
        return self.run_ap("start", "--check")

    def assertNothingChanged(self, r):
        self.assertEqual(self.compose_up_indexes(), [], "compose up was called")
        self.assertEqual(self.pushes(), [], "an image was pushed")
        self.assertEqual(self.signs(), [], "something was signed")
        self.assertFalse(self.session.exists(), "a session file was made")
        self.assertNoSecret(r)

    def test_check_with_no_default_projects_folder_says_it_would_make_it_and_makes_nothing(self):
        self.assertFalse(self.default_proj.exists())
        r = self.check()
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn("start: would make the projects folder %s" % self.default_proj, r.stderr)
        self.assertFalse(self.default_proj.exists(), "--check made the default projects folder")
        self.assertNothingChanged(r)

    def test_check_twice_with_no_default_projects_folder_gives_the_same_result(self):
        first = self.check()
        second = self.check()
        self.assertFalse(self.default_proj.exists(), "--check made the default projects folder")
        self.assertEqual((first.returncode, first.stdout, first.stderr),
                         (second.returncode, second.stdout, second.stderr))
        self.assertEqual(first.returncode, 1, first.stderr)
        self.assertIn("would make the projects folder", first.stderr)
        self.assertNoSecret(first)
        self.assertNoSecret(second)

    def test_check_with_the_default_projects_folder_present_does_not_say_would_make(self):
        self.default_proj.mkdir()
        self.default_proj.chmod(0o755)
        r = self.check()
        self.assertNotIn("would make", r.stderr)
        self.assertTrue(self.default_proj.is_dir())
        self.assertNothingChanged(r)


if __name__ == "__main__":
    unittest.main()
