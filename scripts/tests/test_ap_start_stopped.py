"""Tests for T166: `ap start` judges "started" from the engine container, not only the session file.

Spec: specs/003-one-command-start/spec.md, US1 acceptance scenarios 2 and 4 ("Given the platform was
stopped, When the developer runs the start command, Then it starts again") and FR-018.
Contract: src/cli/contract.md, "Starting the platform": step 5 prints `start: started` or
`start: already started; nothing changed`; `start --check` prints `start: would start` or
`start: already started`, exits 0 when nothing would change and 1 when something would.
The session file outlives the containers (reboot, stopped containers); the stub answers `docker ps`
with no container when STUB_ENGINE_STOPPED=1. Reuses the harness of test_ap_start.py (stub `docker`,
no real Docker). The second run's calls are judged from a log mark taken before it.
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from test_ap_start import StartCase, PASSWORD, ADDRESS  # noqa: E402
from test_ap_up_remote import HOSTNAME_STUB  # noqa: E402


class StartStoppedTest(StartCase):
    def started(self):
        self.make_current()
        r = self.start()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(self.session.exists(), "no started session")

    def second(self, args, **extra):
        """Run `ap start` with args; return (result, the docker calls it made)."""
        mark = len(self.calls())
        r = self.run_ap(*args, COSIGN_PASSWORD=PASSWORD, **extra)
        return r, self.calls()[mark:]

    @staticmethod
    def ups(made):
        return [c for c in made if c["argv"][:1] == ["compose"] and "up" in c["argv"]]

    def assertCheckChangedNothing(self, r, made, session):
        self.assertEqual(self.ups(made), [], "--check called compose up")
        self.assertEqual([c for c in made if c["argv"][:2] == ["buildx", "build"] and "--push" in c["argv"]], [])
        self.assertEqual([c for c in made if c["argv"][:1] == ["run"] and "sign" in c["argv"]], [], "something was signed")
        self.assertEqual(session, self.session.read_text(), "--check changed the session")
        self.assertNoSecret(r)

    # 1: US1 scenario 2/4, contract step 5
    def test_start_with_the_session_kept_and_the_engine_stopped_starts_again(self):
        self.started()
        r, made = self.second(self.start_args(), STUB_ENGINE_STOPPED="1")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNotIn("already started", r.stderr)
        self.assertIn("start: started", r.stderr)
        self.assertEqual(len(self.ups(made)), 1, "compose up was not called once")
        self.assertTrue(any(c["argv"][:1] == ["ps"] for c in made), "start never asked docker ps")
        self.assertRegex(r.stderr[r.stderr.index("start: started"):], ADDRESS)
        self.assertNoSecret(r)

    # 2: contract `start --check`
    def test_check_with_the_session_kept_and_the_engine_stopped_would_start(self):
        self.started()
        session = self.session.read_text()
        r, made = self.second(self.start_args("--check"), STUB_ENGINE_STOPPED="1")
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn("start: would start", r.stderr)
        self.assertNotIn("already started", r.stderr)
        self.assertTrue(any(c["argv"][:1] == ["ps"] for c in made), "--check never asked docker ps")
        self.assertCheckChangedNothing(r, made, session)

    # 3: regression guard (US1 scenario 1/3, contract step 5)
    def test_start_with_the_session_kept_and_the_engine_running_still_says_nothing_changed(self):
        self.started()
        r, made = self.second(self.start_args())
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("start: already started; nothing changed", r.stderr)
        self.assertRegex(r.stderr[r.stderr.index("start: already started"):], ADDRESS)

    def test_check_with_the_session_kept_and_the_engine_running_is_already_started(self):
        self.started()
        session = self.session.read_text()
        r, made = self.second(self.start_args("--check"))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("start: already started", r.stderr)
        self.assertNotIn("would", r.stderr)
        self.assertCheckChangedNothing(r, made, session)

    # edge: no session file but the engine runs: a start that makes the session is "started"
    def test_start_without_a_session_but_with_a_running_engine_says_started(self):
        self.make_current()
        r, made = self.second(self.start_args())
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("start: started", r.stderr)
        self.assertNotIn("already started", r.stderr)
        self.assertEqual(len(self.ups(made)), 1)

    def test_check_without_a_session_but_with_a_running_engine_would_start(self):
        self.make_current()
        r = self.start("--check")
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn("start: would start", r.stderr)
        self.assertNotIn("already started", r.stderr)

    # edge: a remote-access session whose engine stopped is also started again
    def test_start_of_a_remote_session_with_the_engine_stopped_starts_again(self):
        (self.bin / "hostname").write_text(HOSTNAME_STUB)
        (self.bin / "hostname").chmod(0o755)
        fp = {"STUB_CERT_SHA256": "aa" * 32}
        self.make_current()
        args = self.start_args("--listen", "0.0.0.0")  # noqa: S104 (a --listen value given to ap, no bind by the test)
        r = self.run_ap(*args, COSIGN_PASSWORD=PASSWORD, **fp)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(self.session.exists(), "no started remote session")
        mark = len(self.calls())
        r = self.run_ap(*args, COSIGN_PASSWORD=PASSWORD, STUB_ENGINE_STOPPED="1", **fp)
        made = self.calls()[mark:]
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNotIn("already started", r.stderr)
        self.assertIn("start: started", r.stderr)
        self.assertEqual(len(self.ups(made)), 1)

    # 4: a docker ps that fails must not read as "started"
    def failing_ps_docker(self):
        """A `docker` that runs the stub for every call, but whose `ps` call exits 1."""
        real = self.bin / "docker-real"
        (self.bin / "docker").rename(real)
        wrapper = self.bin / "docker"
        wrapper.write_text('#!/bin/sh\nif [ "$1" = ps ]; then\n  "%s" "$@" >/dev/null\n'
                           '  echo "Cannot connect to the Docker daemon" >&2\n  exit 1\nfi\nexec "%s" "$@"\n'
                           % (real, real))
        wrapper.chmod(0o755)

    def test_start_with_a_failing_docker_ps_does_not_claim_already_started(self):
        self.started()
        self.failing_ps_docker()
        r, made = self.second(self.start_args())
        self.assertNotIn("already started", r.stderr)
        self.assertTrue(any(c["argv"][:1] == ["ps"] for c in made), "start never asked docker ps")
        self.assertNoSecret(r)

    def test_check_with_a_failing_docker_ps_does_not_claim_already_started(self):
        self.started()
        session = self.session.read_text()
        self.failing_ps_docker()
        r, made = self.second(self.start_args("--check"))
        self.assertNotIn("already started", r.stderr)
        self.assertNotEqual(r.returncode, 0, "something would change or fail; exit 0 claims nothing would")
        self.assertCheckChangedNothing(r, made, session)


if __name__ == "__main__":
    unittest.main()
