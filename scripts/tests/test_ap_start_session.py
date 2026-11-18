"""Tests for T025: `ap start` refuses early when the session was started with other inputs (feature 003).

Spec: specs/003-one-command-start/spec.md, Edge Cases ("already started with another key file or
projects folder: refuses, as today, and says to stop it first") and FR-004 (checked before it builds or
starts anything). Contract: src/cli/contract.md, "Starting the platform" (exit 3).
Reuses the harness of test_ap_start.py (stub `docker`, no real Docker). "Nothing built" is judged on the
calls of the second run only (the log is marked before it).
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from test_ap_start import StartCase, PASSWORD, SECRET  # noqa: E402


class StartSessionTest(StartCase):
    def started(self):
        self.make_current()
        r = self.start()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(self.session.exists(), "no started session")

    def other_cred(self):
        p = self.base / "other-models.env"
        p.write_text(SECRET + "\n")
        p.chmod(0o600)
        return p

    def other_proj(self):
        p = self.base / "other-projects"
        p.mkdir()
        p.chmod(0o755)
        return p

    def second(self, args):
        """Run `ap start` with args; return (result, the docker calls it made)."""
        mark = len(self.calls())
        r = self.run_ap(*args, COSIGN_PASSWORD=PASSWORD)
        return r, self.calls()[mark:]

    def assertRefused(self, r, made):
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertIn("ap down", r.stderr)
        argvs = [c["argv"] for c in made]
        self.assertEqual([a for a in argvs if a[:2] == ["buildx", "build"]], [], "the refused start built an image")
        self.assertEqual([a for a in argvs if a[:1] == ["compose"] and "up" in a], [], "compose up was called")
        self.assertEqual([a for a in argvs if a[:1] == ["run"] and "sign" in a], [], "something was signed")
        self.assertNoSecret(r)

    def test_start_with_another_credentials_file_is_refused_before_any_build(self):
        self.started()
        r, made = self.second(["start", "--credentials", str(self.other_cred()), "--projects", str(self.proj)])
        self.assertRefused(r, made)

    def test_start_with_another_projects_folder_is_refused_before_any_build(self):
        self.started()
        r, made = self.second(["start", "--credentials", str(self.cred), "--projects", str(self.other_proj())])
        self.assertRefused(r, made)

    def test_check_with_another_credentials_file_is_refused_before_any_build(self):
        self.started()
        r, made = self.second(["start", "--check", "--credentials", str(self.other_cred()), "--projects", str(self.proj)])
        self.assertRefused(r, made)

    def test_check_with_another_projects_folder_is_refused_before_any_build(self):
        self.started()
        r, made = self.second(["start", "--check", "--credentials", str(self.cred), "--projects", str(self.other_proj())])
        self.assertRefused(r, made)

    def test_start_with_the_same_key_file_and_projects_folder_still_succeeds(self):
        self.started()
        r, made = self.second(self.start_args())
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNoSecret(r)


if __name__ == "__main__":
    unittest.main()
