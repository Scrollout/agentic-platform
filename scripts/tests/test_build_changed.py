"""Real-BuildKit test of `src/deploy/build.sh --changed <name>...` (feature 003, T004; T006, T007 are judged here).

One scenario, run once in setUpClass, drives the builder through the cases in an order that is safe
(each step leaves the state the next one needs); every test then asserts what one step produced, so
a failing case names the behaviour. The images are the smallest that prove each case: `cli` and
`page` (small Go programs) and `check` (only for case (d), where its candidate digest must not move
when `src/page/` changes).

Isolation, as in test_signing.py: its own registry under AP_REGISTRY_NAME=ap-test-<hex> on a free
port, a test signing key pair and password, XDG_CONFIG_HOME and XDG_STATE_HOME in temporary
folders; it never touches the developer's `ap-registry`. The class cleanups remove the registry, its
networks and volume, and the images it pushed.

Why a copy of the repository: the cases change source files (a string in the cli or page program, a
planted syntax error, a pin). That must never touch the working tree, so the test copies `src/` and
`scripts/` into a temporary folder and runs the copy's `src/deploy/build.sh`; build.sh finds
`images.lock`, the build context (`src/`) and `scripts/generate.sh` relative to itself, so the copy
is a complete repository for it. BuildKit's cache is the machine's, so an unchanged layer of the
copy is a cache hit. In the copy, `scripts/generate.sh` is replaced by a wrapper that logs its
arguments and runs the real script (case (i) counts the logged calls). AP_TEST_DEPLOY_DIR points
the test at another copy of src/deploy (to see it fail against the committed scripts).

Expected time on a warm BuildKit cache: 10 to 15 minutes; cold, add the check image's module and
warm-cache stage (a few minutes). About 12 image builds with SBOM and provenance and about as many
candidate builds (cached except where a source changed). The test skips, with its reason, when
Docker is not reachable.
"""
import json
import os
import pty
import re
import secrets
import select
import shutil
import socket
import stat
import subprocess
import tempfile
import time
import unittest
import urllib.request
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
DEPLOY = Path(os.environ.get("AP_TEST_DEPLOY_DIR") or REPO / "src/deploy")
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
DIGEST = r"sha256:[0-9a-f]{64}"
CANDIDATE_IMAGES = ("cli", "page", "check")


def sh(*args, **kw):
    kw.setdefault("capture_output", True)
    kw.setdefault("text", True)
    kw.setdefault("timeout", 300)
    return subprocess.run(list(args), **kw)


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def remove_registry_objects(name):
    """Remove every Docker object named `name` or `name-...` (container, volume, networks)."""
    assert name.startswith("ap-test-") and len(name) > len("ap-test-"), name
    sh("docker", "rm", "-f", name)
    sh("docker", "volume", "rm", "-f", name + "-data")
    for n in sh("docker", "network", "ls", "--format", "{{.Name}}", "--filter", "name=" + name).stdout.split():
        if n == name or n.startswith(name + "-"):
            sh("docker", "network", "rm", n)


def env_file(path):
    out = {}
    if Path(path).is_file():
        for line in Path(path).read_text().splitlines():
            k, _, v = line.partition("=")
            if k:
                out[k] = v.strip("'\"")
    return out


class Run:
    """One build.sh run: exit code, output, the generate.sh calls it made, and the state around it."""

    def __init__(self, rc, out, err, gen, before=None, after=None, prompts=0, error=None):
        self.rc, self.out, self.err, self.gen = rc, out, err, gen
        self.before, self.after, self.prompts, self.error = before, after, prompts, error

    @property
    def text(self):
        return (self.out or "") + (self.err or "")

    def lines(self):
        return [ln.strip() for ln in (self.err or "").splitlines()]


class BuildChangedTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if not shutil.which("docker") or sh("docker", "info").returncode != 0:
            raise unittest.SkipTest("docker is not reachable: this test needs real Docker and BuildKit")
        cls.name = "ap-test-" + secrets.token_hex(4)
        cls.port = free_port()
        cls.password = secrets.token_hex(16)
        cls.base = Path(tempfile.mkdtemp(prefix="test-build-changed-", dir=TMP_BASE))
        cls.addClassCleanup(shutil.rmtree, cls.base, ignore_errors=True)
        cls.addClassCleanup(cls.cleanup_docker)
        cls.config, cls.config2 = cls.base / "config", cls.base / "config2"
        cls.state = cls.base / "state"
        for d in (cls.config, cls.config2, cls.state):
            d.mkdir()
        cls.images_env = cls.state / "agentic-platform/images.env"
        cls.built_env = cls.state / "agentic-platform/built.env"
        cls.repo = cls.base / "repo"
        cls.make_copy()
        cls.build = cls.repo / "src/deploy/build.sh"
        cls.runs = {}
        cls.facts = {}
        try:
            cls.scenario()
        except Exception as e:  # reported by every test that needs the step that did not run
            cls.facts["scenario_error"] = repr(e)

    # ---------- the copy of the repository

    @classmethod
    def make_copy(cls):
        ignore = shutil.ignore_patterns("node_modules", ".git", "__pycache__", "*.pyc")
        shutil.copytree(REPO / "src", cls.repo / "src", ignore=ignore)
        shutil.copytree(REPO / "scripts", cls.repo / "scripts", ignore=ignore)
        if DEPLOY != REPO / "src/deploy":
            shutil.copytree(DEPLOY, cls.repo / "src/deploy", dirs_exist_ok=True)
        real = cls.repo / "scripts/generate.sh"
        shutil.move(str(real), str(cls.repo / "scripts/generate-real.sh"))
        real.write_text('#!/bin/sh\nprintf \'%s\\n\' "$*" >> "$GEN_LOG"\nexec "$(dirname "$0")/generate-real.sh" "$@"\n')
        real.chmod(0o755)

    @classmethod
    def cleanup_docker(cls):
        ids = sh("docker", "image", "ls", "-a", "--filter", "reference=127.0.0.1:%d/*" % cls.port,
                 "--format", "{{.ID}}").stdout.split()
        if ids:
            sh("docker", "rmi", "-f", *sorted(set(ids)))
        remove_registry_objects(cls.name)

    # ---------- running build.sh

    @classmethod
    def env(cls, config=None, password=True, state=None, **extra):
        env = {**os.environ, "XDG_CONFIG_HOME": str(config or cls.config), "XDG_STATE_HOME": str(state or cls.state),
               "AP_REGISTRY_PORT": str(cls.port), "AP_REGISTRY_NAME": cls.name}
        env.pop("COSIGN_PASSWORD", None)
        if password:
            env["COSIGN_PASSWORD"] = cls.password
        env.update(extra)
        return env

    @classmethod
    def registry(cls):
        """repository -> sorted tags (signatures are tags of the repository); None when unreachable."""
        out = {}
        try:
            base = "http://127.0.0.1:%d/v2/" % cls.port
            for repo in json.load(urllib.request.urlopen(base + "_catalog", timeout=10))["repositories"]:
                tags = json.load(urllib.request.urlopen(base + repo + "/tags/list", timeout=10))["tags"] or []
                out[repo] = sorted(tags)
        except Exception:
            return None
        return out

    @classmethod
    def tree(cls, root):
        out = {}
        for p in sorted(Path(root).rglob("*")):
            if p.is_file() and not p.name.endswith(".lock"):
                out[str(p.relative_to(root))] = p.read_bytes()
        return out

    @classmethod
    def snapshot(cls, state=None, config=None):
        state, config = state or cls.state, config or cls.config
        return {"images": env_file(state / "agentic-platform/images.env"),
                "built": env_file(state / "agentic-platform/built.env"),
                "registry": cls.registry(), "config": cls.tree(config)}

    @classmethod
    def run(cls, key, args, state=None, config=None, password=True, timeout=900):
        """Run build.sh with no terminal. Records the Run under `key`."""
        gen = cls.base / ("gen-%s.log" % key)
        gen.write_text("")
        before = cls.snapshot(state, config)
        try:
            r = sh("bash", str(cls.build), *args, env=cls.env(config, password, state, GEN_LOG=str(gen)),
                   cwd=cls.repo, timeout=timeout, stdin=subprocess.DEVNULL)
            run = Run(r.returncode, r.stdout, r.stderr, gen.read_text().splitlines())
        except subprocess.TimeoutExpired as e:
            run = Run(None, "", "", gen.read_text().splitlines(), error="timed out after %ss: %s" % (timeout, e))
        run.before, run.after = before, cls.snapshot(state, config)
        cls.runs[key] = run
        return run

    @classmethod
    def run_pty(cls, key, args, answers, config, timeout=900):
        """Run build.sh on a terminal. Each password prompt (an unfinished line of the output that ends
        in a colon after the word password) is answered with the next of `answers`, after a pause so
        that echo is already off. Records the Run, with the number of prompts answered."""
        gen = cls.base / ("gen-%s.log" % key)
        gen.write_text("")
        before = cls.snapshot(None, config)
        env = cls.env(config, password=False, GEN_LOG=str(gen))
        pid, fd = pty.fork()
        if pid == 0:
            os.chdir(str(cls.repo))
            os.execvpe("bash", ["bash", str(cls.build), *args], env)
        buf, answered, last_at, deadline = b"", 0, -1, time.time() + timeout
        status = None
        while time.time() < deadline:
            ready, _, _ = select.select([fd], [], [], 1.0)
            if ready:
                try:
                    chunk = os.read(fd, 65536)
                except OSError:
                    chunk = b""
                if not chunk:
                    break
                buf += chunk
                tail = buf.decode(errors="replace").replace("\r", "\n").rsplit("\n", 1)[-1]
                if re.search(r"(?i)password[^\n]*[:?] *$", tail) and len(buf) != last_at:
                    time.sleep(0.5)
                    last_at = len(buf)
                    os.write(fd, (answers[min(answered, len(answers) - 1)] + "\n").encode())
                    answered += 1
        else:
            os.kill(pid, 9)
        try:
            _, status = os.waitpid(pid, 0)
        except ChildProcessError:
            pass
        rc = os.waitstatus_to_exitcode(status) if status is not None else None
        text = buf.decode(errors="replace").replace("\r\n", "\n")
        run = Run(rc, "", text, gen.read_text().splitlines(), before, cls.snapshot(None, config), prompts=answered)
        cls.runs[key] = run  # a terminal does not separate the streams: all of it is in err
        return run

    # ---------- changing the copy

    @classmethod
    def marker(cls, module, tag):
        """Add a string the program carries in its binary, so the image's result changes."""
        folder = cls.repo / "src" / module / "cmd" / module
        (folder / "t004_marker.go").write_text(
            'package main\n\nimport "os"\n\nfunc init() {\n\tif os.Getenv("AP_T004_MARKER") == "%s" {\n\t\tprintln("%s")\n\t}\n}\n'
            % (tag, tag))

    # ---------- the scenario

    @classmethod
    def scenario(cls):
        pw = cls.password
        # 0. plain builds: record the images and their fingerprints (case (a)); makes the key pair
        for n in CANDIDATE_IMAGES:
            cls.run("plain-" + n, [n], timeout=1500)
        cls.facts["after_plain"] = cls.snapshot()

        # (b) nothing changed, in a copy of the state with no key pair and no signing configuration
        state_b, config_b = cls.base / "state-b", cls.base / "config-b"
        shutil.copytree(cls.state, state_b)
        config_b.mkdir()
        cls.run("b", ["--changed", "cli"], state=state_b, config=config_b, password=False, timeout=600)

        # (c) a change that changes the cli binary; page is untouched
        cls.marker("cli", "t004-cli-1")
        cls.run("c", ["--changed", "cli", "page"])

        # (d) a string in page; check must not move (its own stage)
        cls.marker("page", "t004-page-1")
        cls.run("d", ["--changed", "page", "check"])

        # (e) no AP_BUILT_CLI line: built again, and, the source being the same, to the same result
        lines = [ln for ln in cls.built_env.read_text().splitlines() if not ln.startswith("AP_BUILT_CLI=")]
        cls.built_env.write_text("".join(ln + "\n" for ln in lines))
        cls.run("e", ["--changed", "cli"])

        # (f) two images to sign on a terminal, key pair present: one prompt
        cls.marker("cli", "t004-cli-2")
        cls.marker("page", "t004-page-2")
        cls.run_pty("f1", ["--changed", "cli", "page"], [pw], cls.config)
        # (f) one image to sign on a terminal, no key pair yet (a new config folder): enter and confirm
        cls.marker("cli", "t004-cli-3")
        cls.run_pty("f2", ["--changed", "cli"], [pw], cls.config2)

        # (g) no terminal, no password, an image to sign
        cls.marker("cli", "t004-cli-4")
        cls.run("g", ["--changed", "cli"], password=False, timeout=600)

        # (h) a failing build (page does not compile), then the second run with it fixed
        broken = cls.repo / "src/page/cmd/page/t004_broken.go"
        broken.write_text("package main\n\nfunc {\n")
        cls.run("h1", ["--changed", "cli", "page"])
        broken.unlink()
        cls.run("h2", ["--changed", "cli", "page"])

        # (j) --check: nothing to build, then a change
        cls.run("j1", ["--changed", "--check", "cli", "page", "check"], password=False, timeout=600)
        cls.marker("page", "t004-page-5")
        cls.run("j2", ["--changed", "--check", "cli", "page", "check"], password=False, timeout=600)

        # (k) a first build that needs the network: the golang pin names a registry nothing listens on
        # (127.0.0.1:1), so the base image cannot be fetched whatever the machine's cache holds
        lock = cls.repo / "src/deploy/images.lock"
        text = lock.read_text()
        lock.write_text(re.sub(r"(?m)^(golang\s+)\S+$", r"\g<1>127.0.0.1:1/golang:1.26-alpine@sha256:" + "0" * 64, text))
        cls.run("k", ["--changed", "cli"], timeout=600)
        lock.write_text(text)

    # ---------- helpers for the tests

    def step(self, key):
        self.assertNotIn("scenario_error", self.facts, "the scenario stopped early: %s" % self.facts.get("scenario_error"))
        run = self.runs.get(key)
        self.assertIsNotNone(run, "step %s did not run" % key)
        self.assertIsNone(run.error, run.error)
        return run

    def changed_keys(self, run, field):
        a, b = run.before[field], run.after[field]
        return sorted(k for k in set(a) | set(b) if a.get(k) != b.get(k))

    def unchanged_state(self, run):
        for field in ("images", "built", "registry", "config"):
            self.assertEqual(run.before[field], run.after[field], "%s changed:\n%s" % (field, run.text[-1500:]))

    def building_line(self, run):
        return [ln for ln in run.lines() if re.fullmatch(r"build: building [a-z ]+", ln)]

    def built_line(self, run):
        return [ln for ln in run.lines() if re.fullmatch(r"build: built and signed [a-z ]+", ln)]

    # ---------- (a)

    def test_plain_build_records_the_image_fingerprint_in_built_env(self):
        """(a) After `build.sh cli`, built.env holds AP_BUILT_CLI=sha256:<64 hex> (mode 600) and
        images.env only AP_IMAGE_* lines. No time of its own: the setup builds."""
        self.step("plain-cli")
        snap = self.facts["after_plain"]
        self.assertRegex(snap["built"].get("AP_BUILT_CLI", ""), r"^%s$" % DIGEST, snap["built"])
        self.assertEqual(stat.S_IMODE(self.built_env.stat().st_mode), 0o600)
        lines = [ln for ln in self.images_env.read_text().splitlines() if ln.strip()]
        self.assertTrue(lines)
        for ln in lines:
            self.assertRegex(ln, r"^AP_IMAGE_[A-Z]+=")

    # ---------- (b)

    def test_nothing_changed_pushes_signs_records_and_prints_nothing_and_makes_no_key(self):
        """(b) `--changed cli` with cli current: exit 0, no output, registry, images.env and built.env
        unchanged, no password asked (no terminal, none set), and no key pair or signing
        configuration made in an empty config folder. About 5 s (a cached candidate)."""
        run = self.step("b")
        self.assertEqual(run.rc, 0, run.text[-1500:])
        self.assertEqual(run.text, "", "it must print nothing")
        for field in ("images", "built", "registry"):
            self.assertEqual(run.before[field], run.after[field], field)
        self.assertEqual(run.after["config"], {}, "a key pair or signing configuration was made: %s" % list(run.after["config"]))

    # ---------- (c)

    def test_changed_cli_is_built_signed_and_recorded_and_page_is_left_alone(self):
        """(c) After a change to the cli program, `--changed cli page` prints `build: building cli`
        before it builds and `build: built and signed cli` after, and records cli only.
        About 1 minute (one candidate, one full build, one candidate of page)."""
        run = self.step("c")
        self.assertEqual(run.rc, 0, run.text[-2000:])
        self.assertEqual(self.building_line(run), ["build: building cli"], run.text[-1500:])
        self.assertEqual(self.built_line(run), ["build: built and signed cli"], run.text[-1500:])
        lines = run.lines()
        self.assertLess(lines.index("build: building cli"), lines.index("build: built and signed cli"))
        self.assertEqual(self.changed_keys(run, "built"), ["AP_BUILT_CLI"])
        self.assertEqual(self.changed_keys(run, "images"), ["AP_IMAGE_CLI"])

    # ---------- (d)

    def test_a_string_in_page_rebuilds_page_only_and_leaves_the_check_image_current(self):
        """(d) After a change to a string in src/page/, `--changed page check` builds page only; the
        check image's candidate digest is unchanged (T006: its own stage). About 1 minute."""
        run = self.step("d")
        self.assertEqual(run.rc, 0, run.text[-2000:])
        self.assertEqual(self.building_line(run), ["build: building page"], run.text[-1500:])
        self.assertEqual(self.built_line(run), ["build: built and signed page"], run.text[-1500:])
        self.assertEqual(self.changed_keys(run, "built"), ["AP_BUILT_PAGE"])
        self.assertEqual(self.changed_keys(run, "images"), ["AP_IMAGE_PAGE"])
        self.assertEqual(run.before["built"]["AP_BUILT_CHECK"], run.after["built"]["AP_BUILT_CHECK"])

    # ---------- (e)

    def test_an_image_with_no_recorded_fingerprint_is_built_to_the_same_result(self):
        """(e) With the AP_BUILT_CLI line removed, `--changed cli` builds cli (and the unchanged source
        gives back the very digest that was removed: reproducible). About 1 minute."""
        run = self.step("e")
        self.assertEqual(run.rc, 0, run.text[-2000:])
        self.assertEqual(self.building_line(run), ["build: building cli"], run.text[-1500:])
        self.assertEqual(self.built_line(run), ["build: built and signed cli"], run.text[-1500:])
        self.assertNotIn("AP_BUILT_CLI", run.before["built"])
        self.assertEqual(run.after["built"].get("AP_BUILT_CLI"), self.runs["c"].after["built"]["AP_BUILT_CLI"],
                         "the same source must give the same candidate digest")

    # ---------- (f)

    def password_nowhere(self, run):
        self.assertNotIn(self.password, run.text, "the password was echoed")
        for root in (self.config, self.config2, self.state):
            for p in Path(root).rglob("*"):
                if p.is_file():
                    self.assertNotIn(self.password.encode(), p.read_bytes(), "the password is in %s" % p)

    def test_two_images_from_a_terminal_ask_for_the_password_once_without_echo(self):
        """(f) On a terminal, COSIGN_PASSWORD unset, key pair present, two images to sign: one password
        prompt in all, not echoed, the password in no file under XDG_*. About 2 minutes."""
        run = self.step("f1")
        self.assertEqual(run.rc, 0, run.text[-2000:])
        self.assertEqual(self.building_line(run), ["build: building cli page"], run.text[-1500:])
        self.assertEqual(run.prompts, 1, "password prompts: %s\n%s" % (run.prompts, run.text[-1500:]))
        self.assertEqual(self.changed_keys(run, "built"), ["AP_BUILT_CLI", "AP_BUILT_PAGE"])
        self.password_nowhere(run)

    def test_a_new_key_pair_asks_for_the_password_and_its_confirmation(self):
        """(f) On a terminal with no key pair yet: two prompts (enter, confirm), then the pair exists.
        About 1 minute."""
        run = self.step("f2")
        self.assertEqual(run.rc, 0, run.text[-2000:])
        self.assertEqual(run.prompts, 2, "password prompts: %s\n%s" % (run.prompts, run.text[-1500:]))
        signing = self.config2 / "agentic-platform/signing"
        self.assertTrue((signing / "cosign.key").is_file())
        self.assertTrue((signing / "cosign.pub").is_file())
        self.password_nowhere(run)

    # ---------- (g)

    def test_without_terminal_and_password_it_names_the_images_and_signs_nothing(self):
        """(g) No terminal, no COSIGN_PASSWORD, cli to sign: exit 1 naming cli; nothing signed or
        recorded. About 30 s (one candidate)."""
        run = self.step("g")
        self.assertEqual(run.rc, 1, run.text[-1500:])
        self.assertRegex(run.err, r"\bcli\b")
        self.assertIn("COSIGN_PASSWORD", run.err)
        for field in ("images", "built", "registry"):
            self.assertEqual(run.before[field], run.after[field], field)

    # ---------- (h)

    def test_a_failing_build_names_the_image_and_the_second_run_builds_only_what_is_left(self):
        """(h) page does not compile (a syntax error planted in the copy): `--changed cli page` exits 1
        naming page and records nothing for page; what it recorded of cli (built before it, if the
        builder works image by image) is kept; a second `--changed` builds exactly the images that
        are still not recorded. About 2 minutes."""
        run = self.step("h1")
        self.assertEqual(run.rc, 1, run.text[-1500:])
        self.assertRegex(run.err, r"\bpage\b")
        self.assertNotIn("AP_BUILT_PAGE", self.changed_keys(run, "built"))
        self.assertNotIn("AP_IMAGE_PAGE", self.changed_keys(run, "images"))
        for field in ("images", "built"):  # no recorded line of another image is lost
            for k in run.before[field]:
                self.assertIn(k, run.after[field])
        cli_kept = run.before["built"].get("AP_BUILT_CLI") != run.after["built"].get("AP_BUILT_CLI")
        left = ["page"] if cli_kept else ["cli", "page"]
        run2 = self.step("h2")
        self.assertEqual(run2.rc, 0, run2.text[-2000:])
        self.assertEqual(self.building_line(run2), ["build: building " + " ".join(left)], run2.text[-1500:])
        self.assertEqual(self.built_line(run2), ["build: built and signed " + " ".join(left)], run2.text[-1500:])

    # ---------- (i)

    def test_generate_check_runs_once_per_changed_run(self):
        """(i) `scripts/generate.sh --check` is called exactly once by every `--changed` run (counted
        by the wrapper in the copy), whatever the number of Go images. No time of its own."""
        self.step("c")
        runs = [k for k in ("b", "c", "d", "e", "f1", "f2", "g", "h1", "h2", "j1", "j2", "k") if k in self.runs]
        self.assertEqual(len(runs), 12, "steps run: %s" % runs)
        for k in runs:
            self.assertEqual(self.runs[k].gen.count("--check"), 1, "step %s called generate.sh %s" % (k, self.runs[k].gen))

    # ---------- (j)

    def test_check_says_nothing_to_build_and_exits_0_when_all_is_current(self):
        """(j) `--changed --check cli page check` with all current: `nothing to build`, exit 0, no
        change to images.env, built.env, registry, signatures. About 20 s (cached candidates)."""
        run = self.step("j1")
        self.assertEqual(run.rc, 0, run.text[-1500:])
        self.assertIn("nothing to build", run.text)
        self.assertNotIn("would build", run.text)
        self.unchanged_state(run)

    def test_check_says_what_would_be_built_and_exits_1_changing_nothing(self):
        """(j) After a change to page: `would build page`, exit 1, no change to images.env, built.env,
        registry, signatures, and no password needed. About 30 s (one candidate)."""
        run = self.step("j2")
        self.assertEqual(run.rc, 1, run.text[-1500:])
        self.assertRegex(run.text, r"would build page\s*$", run.text[-1500:])
        self.unchanged_state(run)

    # ---------- (k)

    def test_a_first_build_with_no_network_names_the_image_and_the_network(self):
        """(k) The base image cannot be fetched (the copy's golang pin names a registry nothing listens
        on, so no cache can answer): `--changed cli` exits 1 naming cli and the network, and records
        nothing. About 10 s."""
        run = self.step("k")
        self.assertEqual(run.rc, 1, run.text[-1500:])
        self.assertRegex(run.err, r"\bcli\b")
        self.assertRegex(run.err, r"(?i)network")
        for field in ("images", "built", "registry"):
            self.assertEqual(run.before[field], run.after[field], field)


if __name__ == "__main__":
    unittest.main()
