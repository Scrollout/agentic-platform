"""Tests for T011: `ap start` defaults and prerequisite checks (feature 003, user story 2).

Spec: specs/003-one-command-start/spec.md (User Story 2, FR-004 to FR-006, SC-004); research R3.
Contract: src/cli/contract.md, "Starting the platform" (`start`: option defaults, step 2's checks, exit 3).
Reuses the harness of test_ap_start.py (stub `docker`, no real Docker). HOME and XDG_CONFIG_HOME are
the temporary folders of the harness, so the defaults are judged on what reaches `up`: the session
file records the key file and projects folder `up` was given.
"""
import os
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from test_ap_start import StartCase, PASSWORD  # noqa: E402


class StartDefaultsTest(StartCase):
    def setUp(self):
        super().setUp()
        self.default_cred = self.config / "agentic-platform/anthropic_api_key"
        self.default_proj = self.home / "projects"

    def start_with(self, *args, **extra):
        """`ap start` with exactly these arguments; a build would need the password, so give it."""
        extra.setdefault("COSIGN_PASSWORD", PASSWORD)
        return self.run_ap("start", *args, **extra)

    def session_value(self, name):
        for line in self.session.read_text().splitlines():
            if line.startswith(name + "="):
                return line.split("=", 1)[1].strip("'\"")
        return None

    def make_default_cred(self):
        self.default_cred.parent.mkdir(parents=True, exist_ok=True)
        self.default_cred.write_bytes(self.cred.read_bytes())
        self.default_cred.chmod(0o600)

    def assertRefusedEarly(self, r, *words):
        self.assertEqual(r.returncode, 3, r.stderr)
        for w in words:
            self.assertIn(w, r.stderr)
        argvs = self.argvs()
        self.assertEqual([a for a in argvs if a[:2] == ["buildx", "build"]], [], "an image was built")
        self.assertEqual([a for a in argvs if a[:1] == ["run"] and "verify" in a], [], "an image was verified")
        self.assertEqual([a for a in argvs if a[:1] == ["compose"] and "up" in a], [], "compose up was called")
        self.assertEqual(self.pushes(), [], "an image was pushed")
        self.assertEqual(self.signs(), [], "something was signed")
        self.assertFalse(self.session.exists(), "a session file was made")
        self.assertNoSecret(r)

    def test_no_options_uses_the_default_key_file_and_projects_folder(self):
        self.make_current()
        self.make_default_cred()
        self.default_proj.mkdir()
        self.default_proj.chmod(0o755)
        r = self.start_with()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(len(self.compose_up_indexes()), 1)
        self.assertEqual(os.path.realpath(self.session_value("AP_CREDENTIALS_FILE")),
                         os.path.realpath(self.default_cred))
        self.assertEqual(os.path.realpath(self.session_value("AP_PROJECTS_DIR")),
                         os.path.realpath(self.default_proj))
        self.assertNoSecret(r)

    def test_a_missing_default_projects_folder_is_made_with_mode_755_and_start_goes_on(self):
        self.make_current()
        self.make_default_cred()
        self.assertFalse(self.default_proj.exists())
        old = os.umask(0o077)  # a strict umask must not make it 700
        try:
            r = self.start_with()
        finally:
            os.umask(old)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(self.default_proj.is_dir(), "the default projects folder was not made")
        self.assertEqual(self.default_proj.stat().st_mode & 0o777, 0o755)
        self.assertEqual(len(self.compose_up_indexes()), 1)
        self.assertNoSecret(r)

    def test_a_missing_projects_folder_given_with_projects_is_refused_and_not_made(self):
        missing = self.base / "no-such-projects"
        r = self.start_with("--credentials", str(self.cred), "--projects", str(missing))
        self.assertRefusedEarly(r, str(missing))
        self.assertFalse(missing.exists(), "the folder given with --projects was made")

    def test_a_projects_folder_with_mode_700_is_refused_naming_the_folder_and_the_mode(self):
        self.proj.chmod(0o700)
        r = self.start_with("--credentials", str(self.cred), "--projects", str(self.proj))
        self.assertRefusedEarly(r, str(self.proj), "700")

    def test_a_missing_default_key_file_is_refused_naming_the_path_and_credentials(self):
        self.default_proj.mkdir()
        self.default_proj.chmod(0o755)
        self.assertFalse(self.default_cred.exists())
        r = self.start_with()
        self.assertRefusedEarly(r, str(self.default_cred), "--credentials")

    def test_a_key_file_readable_by_group_is_refused_as_up_refuses_it(self):
        self.cred.chmod(0o640)
        up = self.up()
        self.assertEqual(up.returncode, 3, up.stderr)
        self.log.unlink(missing_ok=True)
        r = self.start_with("--credentials", str(self.cred), "--projects", str(self.proj))
        self.assertRefusedEarly(r, str(self.cred), "chmod 600")

    def test_without_the_gvisor_runtime_start_is_refused_naming_gvisor_and_the_guide(self):
        r = self.start_with("--credentials", str(self.cred), "--projects", str(self.proj), STUB_RUNTIMES="runc")
        self.assertRefusedEarly(r, "gVisor", "docs/guides/install-gvisor.md")

    def test_docker_27_is_refused_as_up_refuses_it(self):
        up = self.up(STUB_DOCKER_VERSION="27.5.0")
        self.assertEqual(up.returncode, 3, up.stderr)
        self.log.unlink(missing_ok=True)
        r = self.start_with("--credentials", str(self.cred), "--projects", str(self.proj),
                            STUB_DOCKER_VERSION="27.5.0")
        self.assertRefusedEarly(r, "27.5.0", "Docker 28")


if __name__ == "__main__":
    unittest.main()
