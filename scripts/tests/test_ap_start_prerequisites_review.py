"""Review-fix tests for T028: `ap start` names a bad `--name` in the contract's order, not first (feature 003).

Spec: specs/003-one-command-start/spec.md (FR-004). Contract: src/cli/contract.md, "Starting the platform"
(`start` step 2: each failure and its fix, in the order Docker, gVisor, key file, projects folder, then the
remote-access options as `up` checks them; exit 3; `--check` makes no projects folder).
Reuses the harness of test_ap_start.py (stub `docker`, no real Docker).
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from test_ap_start import StartCase, PASSWORD  # noqa: E402


class StartPrerequisitesReviewTest(StartCase):
    def setUp(self):
        super().setUp()
        self.make_current()

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

    def gvisor_then_name(self, check):
        r = self.run_start(check, "--credentials", str(self.cred), "--projects", str(self.proj),
                           "--listen", "0.0.0.0", "--name", "a;b", STUB_RUNTIMES="runc")
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertInOrder(r.stderr, "install-gvisor.md", "a;b")
        self.assertNothingDone(r)

    def test_start_names_gvisor_before_a_bad_name(self):
        self.gvisor_then_name(False)

    def test_check_names_gvisor_before_a_bad_name(self):
        self.gvisor_then_name(True)

    def key_then_name(self, check):
        missing = self.base / "missing-models.env"
        r = self.run_start(check, "--credentials", str(missing), "--projects", str(self.proj),
                           "--listen", "0.0.0.0", "--name", "bad name")
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertInOrder(r.stderr, str(missing), "bad name")
        self.assertNothingDone(r)

    def test_start_names_the_key_file_before_a_bad_name(self):
        self.key_then_name(False)

    def test_check_names_the_key_file_before_a_bad_name(self):
        self.key_then_name(True)

    def test_up_still_refuses_a_bad_name(self):
        r = self.run_ap("up", "--credentials", str(self.cred), "--projects", str(self.proj),
                        "--name", "a;b", "--listen", "0.0.0.0")
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertIn("a;b", r.stderr)
        self.assertEqual([a for a in self.argvs() if a[:1] == ["compose"] and "up" in a], [],
                         "compose up was called")

    def test_check_with_a_missing_default_projects_folder_says_it_would_make_it_and_makes_none(self):
        default_cred = self.config / "agentic-platform/anthropic_api_key"
        default_cred.parent.mkdir(parents=True, exist_ok=True)
        default_cred.write_bytes(self.cred.read_bytes())
        default_cred.chmod(0o600)
        default_proj = self.home / "projects"
        self.assertFalse(default_proj.exists())
        r = self.run_start(True)
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn("start: would make the projects folder", r.stdout + r.stderr)
        self.assertFalse(default_proj.exists(), "--check made the projects folder")
        self.assertEqual([a for a in self.argvs() if a[:1] == ["compose"] and "up" in a], [],
                         "compose up was called")


if __name__ == "__main__":
    unittest.main()
