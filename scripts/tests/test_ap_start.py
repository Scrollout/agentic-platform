"""Tests for `ap start` (feature 003, task T005, user story 1), judged by what it passes to `docker`.

Contract: src/cli/contract.md, section "Starting the platform" (`start`, its `start:` and `build:`
lines, `--check`, exit codes); specs/003-one-command-start/research.md R1, R2, R4, R7.
Judges tasks T007 (build.sh --changed) and T008 (ap start).

A recording stub `docker` (fixtures/start/docker-stub) goes first on PATH. The real `ap`, `build.sh`
and `lock.sh` run from a copy of src/deploy in a temporary tree (AP_TEST_DEPLOY_DIR names another
source copy, to see the tests fail against it), beside a `scripts/generate.sh` that reports
nothing stale, so build.sh's code-generation check never writes into the working tree. Nothing
touches a real Docker engine. Every image is "current" when its line AP_BUILT_<NAME> in built.env
equals the digest the stub gives a build of it.

Each test is a case of T005:
1. test_start_builds_the_seven_images_in_order_then_starts          (build then up)
2. test_start_with_every_image_current_builds_pushes_signs_and_asks_for_nothing
   test_start_on_a_started_session_says_nothing_changed             (start: lines, page address)
3. test_a_failing_build_exits_1_and_starts_nothing
4. test_a_second_start_waits_for_the_first_and_changes_nothing
   test_a_start_lock_left_by_a_dead_process_is_taken_over
5. test_check_* (no compose up, push, sign or session; `start: would ...`; exit 0 or 1)
6. test_up_without_the_new_options_makes_the_docker_calls_it_always_made
"""
import hashlib
import json
import os
import re
import shutil
import subprocess
import tempfile
import time
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
DEPLOY_SRC = Path(os.environ.get("AP_TEST_DEPLOY_DIR") or REPO / "src/deploy")
NEW_STUB = HERE / "fixtures/start/docker-stub"
OLD_STUB = HERE / "fixtures/launcher/docker-stub"
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
SECRET = "fake-model-key-STARTSECRET-1a2b3c4d5e6f"
SEVEN = ["engine", "cli", "page", "agent", "check", "project", "lint"]
ALL_IMAGES = SEVEN + ["tests"]
PASSWORD = "test-only-signing-password"
ADDRESS = re.compile(r"http://127\.0\.0\.1:[0-9]+")


def stub_digest(name):
    return "sha256:" + hashlib.sha256(("ap-" + name).encode()).hexdigest()


def image_ref(name):
    return "127.0.0.1:5005/ap-%s@%s" % (name, stub_digest(name))


# Stands in for `docker`: holds a `compose ... up` call while $STUB_GATE exists, then hands every call
# to the recording stub.
GATE_WRAPPER = """#!/bin/sh
case " $* " in
  *" compose "*" up "*)
    if [ -n "${STUB_GATE:-}" ] && [ -e "$STUB_GATE" ]; then
      : >"$STUB_GATE.reached"
      while [ -e "$STUB_GATE" ]; do sleep 0.1; done
    fi ;;
esac
exec "%s" "$@"
"""


class StartCase(unittest.TestCase):
    def setUp(self):
        self.base = Path(tempfile.mkdtemp(prefix="test-ap-start-", dir=TMP_BASE))
        self.addCleanup(shutil.rmtree, self.base, ignore_errors=True)
        # the launcher, builder and lock from src/deploy, with a generate.sh that finds nothing stale
        self.deploy = self.base / "repo/src/deploy"
        shutil.copytree(DEPLOY_SRC, self.deploy, ignore=shutil.ignore_patterns("__pycache__"))
        gen = self.base / "repo/scripts/generate.sh"
        gen.parent.mkdir(parents=True)
        gen.write_text("#!/bin/sh\nexit 0\n")
        gen.chmod(0o755)
        self.ap_path = self.deploy / "ap"
        self.bin = self.base / "bin"
        self.bin.mkdir()
        self.install_stub(NEW_STUB)
        self.home = self.base / "home"
        self.home.mkdir()
        self.state = self.base / "state"
        self.state.mkdir()
        self.config = self.base / "config"
        self.log = self.base / "docker.log"
        self.cred = self.base / "models.env"
        self.cred.write_text(SECRET + "\n")
        self.cred.chmod(0o600)
        self.proj = self.base / "projects"
        self.proj.mkdir()
        self.proj.chmod(0o755)
        self.sdir = self.state / "agentic-platform"
        self.sdir.mkdir(parents=True)
        self.sdir.chmod(0o700)
        self.signing = self.config / "agentic-platform/signing"
        self.signing.mkdir(parents=True)
        (self.signing / "cosign.pub").write_text("-----BEGIN PUBLIC KEY-----\nfake\n-----END PUBLIC KEY-----\n")
        (self.signing / "cosign.key").write_text("FAKE-PRIVATE-KEY\n")
        (self.signing / "cosign.key").chmod(0o600)
        (self.signing / "signing-config.json").write_text("{}\n")
        self.session = self.sdir / "session.env"
        self.images_env = self.sdir / "images.env"
        self.built_env = self.sdir / "built.env"
        self.start_lock = self.sdir / "start.lock"

    def install_stub(self, stub):
        shutil.copy2(stub, self.bin / "docker")

    # state of images
    def make_current(self, names=ALL_IMAGES):
        """images.env and built.env as a signed build of each image would have left them."""
        self.images_env.write_text("".join("AP_IMAGE_%s=%s\n" % (n.upper(), image_ref(n)) for n in names))
        self.images_env.chmod(0o600)
        self.built_env.write_text("".join("AP_BUILT_%s=%s\n" % (n.upper(), stub_digest(n))
                                          for n in names if n in SEVEN))
        self.built_env.chmod(0o600)

    def make_not_current(self, name):
        lines = [l for l in self.built_env.read_text().splitlines() if not l.startswith("AP_BUILT_%s=" % name.upper())]
        self.built_env.write_text("".join(l + "\n" for l in lines))

    # running
    def env(self, **extra):
        e = {k: v for k, v in os.environ.items() if not k.startswith(("AP_", "STUB_", "COSIGN_"))}
        e.update(HOME=str(self.home), XDG_STATE_HOME=str(self.state), XDG_CONFIG_HOME=str(self.config),
                 STUB_LOG=str(self.log), PATH=str(self.bin) + os.pathsep + os.environ.get("PATH", ""))
        e.update({k: str(v) for k, v in extra.items()})
        return e

    def run_ap(self, *args, timeout=120, **extra):
        return subprocess.run(["sh", str(self.ap_path), *args], env=self.env(**extra), capture_output=True,
                              text=True, timeout=timeout, stdin=subprocess.DEVNULL)

    def start_args(self, *more):
        return ["start", *more, "--credentials", str(self.cred), "--projects", str(self.proj)]

    def start(self, *more, **extra):
        return self.run_ap(*self.start_args(*more), **extra)

    def up(self, **extra):
        return self.run_ap("up", "--credentials", str(self.cred), "--projects", str(self.proj), **extra)

    def calls(self):
        if not self.log.exists():
            return []
        return [json.loads(l) for l in self.log.read_text().splitlines() if l]

    def argvs(self):
        return [c["argv"] for c in self.calls()]

    def compose_up_indexes(self):
        return [i for i, a in enumerate(self.argvs()) if a[:1] == ["compose"] and "up" in a]

    @staticmethod
    def build_name(argv):
        m = re.search(r"Dockerfile\.([a-z]+)$", " ".join(argv[argv.index("-f") + 1:argv.index("-f") + 2]) if "-f" in argv else "")
        return m.group(1) if m else None

    def builds(self):
        """(index, name, pushed) of each `docker buildx build`."""
        return [(i, self.build_name(a), "--push" in a) for i, a in enumerate(self.argvs())
                if a[:2] == ["buildx", "build"]]

    def candidates(self):
        return [(i, n) for i, n, pushed in self.builds() if not pushed]

    def pushes(self):
        return [n for _, n, pushed in self.builds() if pushed]

    def signs(self):
        return [a for a in self.argvs() if a[:1] == ["run"] and "sign" in a]

    def assertNoSecret(self, r):
        self.assertNotIn(SECRET, r.stdout + r.stderr)
        self.assertNotIn(PASSWORD, r.stdout + r.stderr)

    def assertNothingStartedOrChanged(self, r):
        self.assertEqual(self.compose_up_indexes(), [], "compose up was called")
        self.assertEqual(self.pushes(), [], "an image was pushed")
        self.assertEqual(self.signs(), [], "something was signed")
        self.assertFalse(self.session.exists(), "a session file was made")


class StartTest(StartCase):
    # 1
    def test_start_builds_the_seven_images_in_order_then_starts(self):
        r = self.start(COSIGN_PASSWORD=PASSWORD)
        self.assertEqual(r.returncode, 0, r.stderr)
        cands = self.candidates()
        self.assertEqual([n for _, n in cands[:len(SEVEN)]], SEVEN, "candidate builds, in this order")
        ups = self.compose_up_indexes()
        self.assertEqual(len(ups), 1, self.argvs())
        self.assertLess(max(i for i, _ in cands[:len(SEVEN)]), ups[0], "every build before compose up")
        self.assertEqual(sorted(self.pushes()), sorted(SEVEN), "with nothing built yet, every image is built and pushed")
        self.assertIn("build: building " + " ".join(SEVEN), r.stderr)
        self.assertIn("build: built and signed " + " ".join(SEVEN), r.stderr)
        self.assertIn("start: started", r.stderr)
        self.assertNoSecret(r)
        # the signing run's recorded lines make the next start find nothing to build
        built = self.built_env.read_text()
        for n in SEVEN:
            self.assertIn("AP_BUILT_%s=%s" % (n.upper(), stub_digest(n)), built)

    # 2
    def test_start_with_every_image_current_builds_pushes_signs_and_asks_for_nothing(self):
        self.make_current()
        r = self.start()  # no COSIGN_PASSWORD, no terminal: asking would fail
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.pushes(), [])
        self.assertEqual(self.signs(), [])
        self.assertNotIn("build: building", r.stderr)
        self.assertEqual(len(self.compose_up_indexes()), 1)
        self.assertIn("start: started", r.stderr)
        self.assertNotIn("already started", r.stderr)
        line = r.stderr.index("start: started")
        self.assertRegex(r.stderr[line:], ADDRESS, "the page address follows the start line")
        self.assertNoSecret(r)

    def test_start_on_a_started_session_says_nothing_changed(self):
        self.make_current()
        self.assertEqual(self.start().returncode, 0)
        tokens = self.session.read_text()
        self.log.unlink()
        r = self.start()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.pushes(), [])
        self.assertEqual(self.signs(), [])
        self.assertIn("start: already started; nothing changed", r.stderr)
        self.assertRegex(r.stderr[r.stderr.index("start: already started"):], ADDRESS)
        self.assertEqual(self.session.read_text(), tokens, "the session keeps its tokens")

    # 3
    def test_a_failing_build_exits_1_and_starts_nothing(self):
        self.make_current()
        self.make_not_current("page")
        r = self.start(STUB_BUILD_FAIL="page", COSIGN_PASSWORD=PASSWORD)
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertEqual(self.compose_up_indexes(), [], "compose up was called")
        self.assertIn("page", r.stderr)
        self.assertNotIn("start: started", r.stderr)
        self.assertNoSecret(r)

    # 4
    def test_a_second_start_waits_for_the_first_and_changes_nothing(self):
        gate = self.base / "gate"
        gate.touch()
        wrapper = self.bin / "docker"
        real = self.base / "real-bin"
        real.mkdir()
        shutil.copy2(NEW_STUB, real / "docker")
        wrapper.write_text(GATE_WRAPPER % (real / "docker"))
        wrapper.chmod(0o755)
        env = self.env(STUB_GATE=gate, COSIGN_PASSWORD=PASSWORD)
        args = ["sh", str(self.ap_path), *self.start_args()]
        kw = dict(env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, stdin=subprocess.DEVNULL)
        a = subprocess.Popen(args, **kw)
        b = None
        try:
            deadline = time.time() + 90
            while not Path(str(gate) + ".reached").exists():
                if a.poll() is not None:
                    self.fail("the first start ended before it reached compose up: %s" % a.communicate()[1])
                self.assertLess(time.time(), deadline, "the first start never reached compose up")
                time.sleep(0.1)
            self.assertTrue(self.start_lock.is_dir(), "the first start holds start.lock while it runs")
            b = subprocess.Popen(args, **kw)
            time.sleep(3)
            self.assertIsNone(b.poll(), "the second start must wait for the lock")
            self.assertEqual(self.compose_up_indexes(), [])
        finally:
            gate.unlink(missing_ok=True)
        out_a, err_a = a.communicate(timeout=120)
        out_b, err_b = b.communicate(timeout=120)
        self.assertEqual(a.returncode, 0, err_a)
        self.assertEqual(b.returncode, 0, err_b)
        self.assertEqual(sorted(self.pushes()), sorted(SEVEN), "the images were built once, by the first")
        self.assertEqual(len(self.signs()), len(SEVEN))
        self.assertIn("start: started", err_a)
        self.assertNotIn("build: building", err_b)
        self.assertIn("start: already started; nothing changed", err_b)
        self.assertFalse(self.start_lock.exists(), "start.lock is released")

    def test_a_start_lock_left_by_a_dead_process_is_taken_over(self):
        self.make_current()
        dead = subprocess.Popen(["true"])
        dead.wait()
        self.start_lock.mkdir()
        (self.start_lock / "pid").write_text("%d\n" % dead.pid)
        r = self.start(timeout=60)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertRegex(r.stderr, r"stale lock")
        self.assertEqual(len(self.compose_up_indexes()), 1)
        self.assertIn("start: started", r.stderr)
        self.assertFalse(self.start_lock.exists(), "start.lock is released")

    # 5
    def check_untouched(self, r):
        self.assertNothingStartedOrChanged(r)
        self.assertEqual([n for _, n in self.candidates()], SEVEN, "--check makes every candidate build")
        self.assertNoSecret(r)

    def test_check_with_every_image_current_and_no_session_would_start(self):
        self.make_current()
        before = (self.images_env.read_text(), self.built_env.read_text())
        r = self.start("--check")
        self.assertEqual(r.returncode, 1, r.stderr)
        self.check_untouched(r)
        self.assertIn("start: nothing to build", r.stderr)
        self.assertIn("start: would start", r.stderr)
        self.assertNotIn("would build", r.stderr)
        self.assertEqual(before, (self.images_env.read_text(), self.built_env.read_text()))

    def test_check_with_a_changed_image_names_it_and_exits_1(self):
        self.make_current()
        self.make_not_current("page")
        before = (self.images_env.read_text(), self.built_env.read_text())
        r = self.start("--check")  # no COSIGN_PASSWORD: it signs nothing, so it asks nothing
        self.assertEqual(r.returncode, 1, r.stderr)
        self.check_untouched(r)
        self.assertIn("start: would build page", r.stderr)
        self.assertNotIn("nothing to build", r.stderr)
        self.assertEqual(before, (self.images_env.read_text(), self.built_env.read_text()))

    def test_check_on_a_started_session_with_nothing_changed_exits_0(self):
        self.make_current()
        self.assertEqual(self.start().returncode, 0)
        session = self.session.read_text()
        self.log.unlink()
        r = self.start("--check")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.compose_up_indexes(), [])
        self.assertEqual(self.pushes(), [])
        self.assertEqual(self.signs(), [])
        self.assertIn("start: nothing to build", r.stderr)
        self.assertIn("start: already started", r.stderr)
        self.assertNotIn("would", r.stderr)
        self.assertEqual(session, self.session.read_text())

    def test_check_with_nothing_built_yet_would_build_all_and_asks_for_no_password(self):
        r = self.start("--check")
        self.assertEqual(r.returncode, 1, r.stderr)
        self.check_untouched(r)
        self.assertIn("start: would build " + " ".join(SEVEN), r.stderr)
        self.assertIn("start: would start", r.stderr)
        self.assertFalse(self.images_env.exists() and self.images_env.read_text().strip(), "nothing recorded")

    # 6
    def test_up_without_the_new_options_makes_the_docker_calls_it_always_made(self):
        self.make_current()
        runs = []
        for stub in (OLD_STUB, NEW_STUB):
            self.install_stub(stub)
            self.log.unlink(missing_ok=True)
            if self.session.exists():
                self.session.unlink()
            r = self.up()
            self.assertEqual(r.returncode, 0, r.stderr)
            runs.append(self.argvs())
        self.assertTrue(any("up" in a for a in runs[0] if a[:1] == ["compose"]), "up must reach compose up")
        self.assertEqual(runs[0], runs[1])


if __name__ == "__main__":
    unittest.main()
