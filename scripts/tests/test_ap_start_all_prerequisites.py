"""Tests for T027: `ap start` names every failed prerequisite, not only the first (feature 003, user story 2).

Spec: specs/003-one-command-start/spec.md (FR-004, SC-004). Contract: src/cli/contract.md, "Starting the
platform" (`start` step 2: each failure and its fix, in the order gVisor, key file, projects folder, remote
options; exit 3; `--check` makes no projects folder).
Reuses the harness of test_ap_start.py (stub `docker`, no real Docker).
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from test_ap_start import StartCase, PASSWORD  # noqa: E402


class StartAllPrerequisitesTest(StartCase):
    def setUp(self):
        super().setUp()
        self.make_current()
        self.missing_cred = self.base / "missing-models.env"
        self.closed_proj = self.base / "closed-projects"
        self.closed_proj.mkdir()
        self.closed_proj.chmod(0o700)

    def run_start(self, check, *args, **extra):
        extra.setdefault("COSIGN_PASSWORD", PASSWORD)
        return self.run_ap("start", *(["--check"] if check else []), *args, **extra)

    def assertNothingDone(self, r):
        argvs = self.argvs()
        self.assertEqual([a for a in argvs if a[:2] == ["buildx", "build"]], [], "an image was built")
        self.assertEqual([a for a in argvs if a[:1] == ["run"] and "verify" in a], [], "an image was verified")
        self.assertEqual([a for a in argvs if a[:1] == ["compose"] and "up" in a], [], "compose up was called")
        self.assertEqual(self.pushes(), [], "an image was pushed")
        self.assertEqual(self.signs(), [], "something was signed")
        self.assertFalse(self.session.exists(), "a session file was made")
        self.assertNoSecret(r)

    def assertInOrder(self, text, *needles):
        pos = []
        for n in needles:
            self.assertIn(n, text)
            pos.append(text.index(n))
        self.assertEqual(pos, sorted(pos), "failures are not named in the contract's order: %r" % (needles,))

    def four(self, check):
        r = self.run_start(check, "--credentials", str(self.missing_cred), "--projects", str(self.closed_proj),
                           "--name", "x", STUB_RUNTIMES="runc")
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertInOrder(r.stderr, "install-gvisor.md", str(self.missing_cred), "700", "--listen")
        self.assertIn("gVisor", r.stderr)
        self.assertIn("--credentials", r.stderr)
        self.assertIn(str(self.closed_proj), r.stderr)
        self.assertNothingDone(r)

    def test_start_names_all_four_failures_in_order(self):
        self.four(False)

    def test_check_names_all_four_failures_in_order(self):
        self.four(True)

    def two(self, check):
        r = self.run_start(check, "--credentials", str(self.cred), "--projects", str(self.closed_proj),
                           STUB_RUNTIMES="runc")
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertInOrder(r.stderr, "install-gvisor.md", str(self.closed_proj), "700")
        self.assertNothingDone(r)

    def test_start_names_gvisor_and_a_mode_700_folder(self):
        self.two(False)

    def test_check_names_gvisor_and_a_mode_700_folder(self):
        self.two(True)

    def default_failure(self, check):
        default_cred = self.config / "agentic-platform/anthropic_api_key"
        default_cred.parent.mkdir(parents=True, exist_ok=True)
        default_cred.write_bytes(self.cred.read_bytes())
        default_cred.chmod(0o600)
        default_proj = self.home / "projects"
        self.assertFalse(default_proj.exists())
        r = self.run_start(check, STUB_RUNTIMES="runc")
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertIn("install-gvisor.md", r.stderr)
        self.assertFalse(default_proj.exists(), "the default projects folder was made despite a failure")
        self.assertNothingDone(r)

    def test_start_with_a_failure_does_not_make_the_default_projects_folder(self):
        self.default_failure(False)

    def test_check_with_a_failure_names_it_and_makes_no_projects_folder(self):
        self.default_failure(True)

    def test_control_all_prerequisites_fine_start_succeeds(self):
        r = self.run_start(False, "--credentials", str(self.cred), "--projects", str(self.proj))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(len(self.compose_up_indexes()), 1)
        self.assertNoSecret(r)


if __name__ == "__main__":
    unittest.main()
