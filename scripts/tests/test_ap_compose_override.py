"""T024 unit (2): the launcher's test-only Compose override, AP_COMPOSE_OVERRIDE (src/cli/contract.md
"The recorded model, for tests only"; research D44; ADR 0047), judged only by what `ap` passes to
`docker`.

The launcher runs from a copy of src/deploy inside a throwaway tree that has links to the rest of
src/ and a real folder src/tests/e2e/ of its own, so "under src/tests/e2e/ of the launcher's
repository" is a folder this test owns. `docker` is the recording stub of test_launcher.py
(fixtures/launcher/docker-stub, imported, unchanged) behind a small wrapper that answers the
engine's two HTTP calls `build-workers` makes through `docker run ... nc engine 8080` (listRuns: no
runs; registerWorker: 200), so `build-workers` reaches its `docker compose run` calls.

Rules (contract): unset, Compose gets only compose.yaml; set to a file under src/tests/e2e/, `up`
passes `-f compose.yaml -f <file>`, prints `up: the model is the recorded model (<file>)` on standard
error, and the session keeps the file, so `build-workers`, a client command and `down` pass it too;
any other path (outside the folder, a sibling folder that only shares its prefix, a `..` that leaves
it, a symbolic link that resolves outside, a folder, a missing file) is a usage error, exit 3, and
nothing starts.
"""
import json
import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from test_launcher import IMAGE_NAMES, SECRET, STUB, image_value

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
# AP_TEST_AP points the tests at another copy of ap (to see them fail against a mutant)
REAL_AP = Path(os.environ.get("AP_TEST_AP") or REPO / "src/deploy/ap")
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
LINE = "up: the model is the recorded model ("

WRAPPER = r'''#!/usr/bin/env python3
"""docker for test_ap_compose_override: the recording stub (STUB_DELEGATE) logs the call; a
`docker run ... nc ... engine 8080` call also gets the engine's answer on standard output."""
import os, subprocess, sys
argv = sys.argv[1:]
engine = "nc" in argv and "engine" in argv
req = sys.stdin.read() if engine else ""
code = subprocess.run([sys.executable, os.environ["STUB_DELEGATE"], *argv], stdin=subprocess.DEVNULL).returncode
if os.environ.get("STUB_FAIL_OVERRIDE") and argv[:1] == ["compose"] and argv.count("-f") >= 2:
    code = 1  # Compose cannot read the override (its files are gone)
if engine:
    first = req.split("\n", 1)[0]
    body = '{"items":[]}' if first.startswith("GET /runs") else "{}"
    if first.startswith(("GET /runs", "POST /workers")):
        sys.stdout.write("HTTP/1.0 200 OK\r\nContent-Type: application/json\r\n\r\n" + body)
sys.exit(code)
'''

# Compose's global options that take a value (they come before the subcommand)
VALUE_OPTS = ("--profile", "-p", "--project-name", "--env-file", "--project-directory", "--ansi",
              "--progress", "--parallel")


def compose_files(argv):
    """The -f/--file values of a `docker compose ...` call, in order, before its subcommand."""
    assert argv[:1] == ["compose"], argv
    files, i = [], 1
    while i < len(argv):
        a = argv[i]
        if a in ("-f", "--file"):
            files.append(argv[i + 1])
            i += 2
        elif a.startswith("--file="):
            files.append(a[len("--file="):])
            i += 1
        elif a in VALUE_OPTS:
            i += 2
        elif a.startswith("-"):
            i += 1
        else:
            break
    return files


class ComposeOverrideTest(unittest.TestCase):
    def setUp(self):
        if not REAL_AP.is_file():
            self.fail("src/deploy/ap does not exist")
        self.base = Path(tempfile.mkdtemp(prefix="test-ap-override-", dir=TMP_BASE))
        self.addCleanup(shutil.rmtree, self.base, ignore_errors=True)
        # the launcher's repository: a copy of src/deploy, links to the rest of src/, own src/tests/e2e/
        self.tree = self.base / "repo"
        src = self.tree / "src"
        src.mkdir(parents=True)
        shutil.copytree(REPO / "src/deploy", src / "deploy", symlinks=True)
        shutil.copy2(REAL_AP, src / "deploy/ap")
        for p in (REPO / "src").iterdir():
            if p.name not in ("deploy", "tests"):
                (src / p.name).symlink_to(p)
        self.e2e = src / "tests/e2e"
        self.e2e.mkdir(parents=True)
        self.ap_path = src / "deploy/ap"
        self.compose_yaml = os.path.realpath(src / "deploy/compose.yaml")
        self.override = self.e2e / "compose.recorded.yaml"
        self.override.write_text("services: {}\n")
        # docker, the session's folders, the credentials and the recorded images
        self.bin = self.base / "bin"
        self.bin.mkdir()
        (self.bin / "docker").write_text(WRAPPER)
        (self.bin / "docker").chmod(0o755)
        self.home, self.state, self.config = self.base / "home", self.base / "state", self.base / "config"
        for d in (self.home, self.state):
            d.mkdir()
        self.log = self.base / "docker.log"
        self.cred = self.base / "models.env"
        self.cred.write_text(SECRET + "\n")
        self.cred.chmod(0o600)
        self.proj = self.base / "projects"
        self.proj.mkdir()
        self.session = self.state / "agentic-platform/session.env"
        signing = self.config / "agentic-platform/signing"
        signing.mkdir(parents=True)
        (signing / "cosign.pub").write_text("-----BEGIN PUBLIC KEY-----\nfake\n-----END PUBLIC KEY-----\n")
        images = self.state / "agentic-platform/images.env"
        images.parent.mkdir(parents=True)
        images.write_text("".join("AP_IMAGE_%s=%s\n" % (n.upper(), image_value(n)) for n in IMAGE_NAMES))
        images.chmod(0o600)

    # helpers
    def ap(self, *args, **env):
        e = {k: v for k, v in os.environ.items() if not k.startswith(("AP_", "STUB_"))}
        e.update(HOME=str(self.home), XDG_STATE_HOME=str(self.state), XDG_CONFIG_HOME=str(self.config),
                 STUB_LOG=str(self.log), STUB_DELEGATE=str(STUB),
                 PATH=str(self.bin) + os.pathsep + os.environ.get("PATH", ""))
        e.update({k: str(v) for k, v in env.items()})
        return subprocess.run(["sh", str(self.ap_path), *args], env=e, capture_output=True, text=True,
                              timeout=60, stdin=subprocess.DEVNULL, cwd=str(self.base))

    def up(self, **env):
        return self.ap("up", "--credentials", str(self.cred), "--projects", str(self.proj), **env)

    def calls(self):
        if not self.log.exists():
            return []
        return [json.loads(line)["argv"] for line in self.log.read_text().splitlines() if line]

    def compose_calls(self):
        return [a for a in self.calls() if a[:1] == ["compose"]]

    def clear(self):
        self.log.unlink(missing_ok=True)

    def assertFiles(self, argv, expected):
        got = [os.path.realpath(f) for f in compose_files(argv)]
        self.assertEqual(got, [os.path.realpath(f) for f in expected], argv)

    def assertEveryComposeCall(self, what, expected):
        cc = self.compose_calls()
        self.assertTrue(cc, "%s made no docker compose call" % what)
        for a in cc:
            self.assertFiles(a, expected)

    def assertNothingStarted(self, r, what):
        self.assertEqual(r.returncode, 3, "%s: %s" % (what, r.stderr))
        self.assertEqual(self.compose_calls(), [], "%s: docker compose was called" % what)
        started = [a for a in self.calls() if a[:1] in (["run"], ["create"], ["start"])
                   or a[:2] in (["container", "create"], ["container", "start"], ["container", "run"])]
        self.assertEqual(started, [], "%s: a container was started" % what)
        self.assertFalse(self.session.exists(), "%s: a session was written" % what)
        self.assertNotIn(LINE, r.stderr, what)
        self.assertIn("src/tests/e2e", r.stderr, "%s: the refusal must name the folder" % what)

    # unset: nothing changes
    def test_unset_compose_gets_only_compose_yaml(self):
        r = self.up()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNotIn("recorded model", r.stderr)
        self.assertEveryComposeCall("up", [self.compose_yaml])
        for args in (["describe"], ["down"]):
            with self.subTest(args=args):
                self.clear()
                r = self.ap(*args)
                self.assertEqual(r.returncode, 0, r.stderr)
                self.assertEveryComposeCall(args[0], [self.compose_yaml])

    def test_empty_value_is_unset(self):
        r = self.up(AP_COMPOSE_OVERRIDE="")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEveryComposeCall("up", [self.compose_yaml])

    # set to a file under src/tests/e2e/
    def test_up_passes_the_override_after_compose_yaml_and_says_so(self):
        r = self.up(AP_COMPOSE_OVERRIDE=self.override)
        self.assertEqual(r.returncode, 0, r.stderr)
        ups = [a for a in self.compose_calls() if "up" in a]
        self.assertEqual(len(ups), 1, self.compose_calls())
        self.assertEveryComposeCall("up", [self.compose_yaml, self.override])
        lines = [ln for ln in r.stderr.splitlines() if LINE in ln]
        self.assertEqual(len(lines), 1, r.stderr)
        m = re.search(re.escape(LINE) + r"(.+)\)$", lines[0])
        assert m is not None, lines[0]
        self.assertEqual(os.path.realpath(m.group(1)), os.path.realpath(self.override), lines[0])

    def test_a_relative_path_is_resolved(self):
        rel = os.path.relpath(self.override, self.base)  # ap runs with cwd = self.base
        r = self.up(AP_COMPOSE_OVERRIDE=rel)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEveryComposeCall("up", [self.compose_yaml, self.override])

    def test_the_session_keeps_the_override_for_every_later_command(self):
        r = self.up(AP_COMPOSE_OVERRIDE=self.override)
        self.assertEqual(r.returncode, 0, r.stderr)
        # later commands without the variable: the session's file is used
        for args in (["describe"], ["show", "S-1", "rules"], ["build-workers", "S-1", "notes"], ["down"]):
            with self.subTest(args=args):
                self.clear()
                # STUB_REGISTRY_ABSENT: `docker container inspect` finds no container, so no step
                # worker exists yet and build-workers starts each one
                r = self.ap(*args, STUB_REGISTRY_ABSENT="1") if args[0] == "build-workers" else self.ap(*args)
                self.assertEqual(r.returncode, 0, r.stderr)
                self.assertEveryComposeCall(args[0], [self.compose_yaml, self.override])
                if args[0] == "build-workers":
                    self.assertTrue(any("run" in a for a in self.compose_calls()),
                                    "build-workers started no worker: %s" % self.compose_calls())

    def test_after_down_a_plain_up_has_no_override(self):
        self.up(AP_COMPOSE_OVERRIDE=self.override)
        self.ap("down")
        self.clear()
        r = self.up()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNotIn("recorded model", r.stderr)
        self.assertEveryComposeCall("up", [self.compose_yaml])
        self.clear()
        self.ap("describe")
        self.assertEveryComposeCall("describe", [self.compose_yaml])

    def test_down_stops_with_compose_yaml_alone_when_the_override_cannot_be_used(self):
        """Review regression: a recorded-model session whose override Compose cannot read still stops."""
        r = self.up(AP_COMPOSE_OVERRIDE=self.override)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.clear()
        r = self.ap("down", STUB_FAIL_OVERRIDE="1")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("stopping with compose.yaml alone", r.stderr)
        downs = [a for a in self.compose_calls() if "down" in a]
        self.assertTrue(downs, self.compose_calls())
        self.assertFiles(downs[-1], [self.compose_yaml])
        self.assertFalse(self.session.exists(), "down must remove the session")

    def test_down_without_an_override_still_fails_when_compose_fails(self):
        self.up()
        r = self.ap("down", STUB_EXIT="1")
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertNotIn("stopping with compose.yaml alone", r.stderr)
        self.assertTrue(self.session.exists(), "a failed down keeps the session")

    def test_a_second_up_with_another_override_or_none_is_refused(self):
        """Review regression: the session's override is kept; another one, or none, needs ap down first."""
        r = self.up(AP_COMPOSE_OVERRIDE=self.override)
        self.assertEqual(r.returncode, 0, r.stderr)
        other = self.e2e / "other.yaml"
        other.write_text("services: {}\n")
        before = self.session.read_bytes()
        for what, env in (("another override", {"AP_COMPOSE_OVERRIDE": other}), ("none", {})):
            with self.subTest(what=what):
                self.clear()
                r = self.up(**env)
                self.assertEqual(r.returncode, 3, "%s: %s" % (what, r.stderr))
                self.assertEqual([a for a in self.compose_calls() if "up" in a], [], what)
                self.assertEqual(self.session.read_bytes(), before, "%s changed the session" % what)
        self.clear()
        r = self.up(AP_COMPOSE_OVERRIDE=self.override)
        self.assertEqual(r.returncode, 0, "the same override again: " + r.stderr)

    def test_a_second_up_with_an_override_after_a_plain_up_is_refused(self):
        self.assertEqual(self.up().returncode, 0)
        before = self.session.read_bytes()
        self.clear()
        r = self.up(AP_COMPOSE_OVERRIDE=self.override)
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertEqual([a for a in self.compose_calls() if "up" in a], [])
        self.assertEqual(self.session.read_bytes(), before)

    def test_a_symlink_chain_is_followed_to_its_end(self):
        """Review regression: every link of a chain is followed, not only the first."""
        outside = self.base / "outside.yaml"
        outside.write_text("services: {}\n")
        (self.e2e / "b.yaml").symlink_to(outside)
        (self.e2e / "a.yaml").symlink_to(self.e2e / "b.yaml")
        r = self.up(AP_COMPOSE_OVERRIDE=self.e2e / "a.yaml")
        self.assertNothingStarted(r, "a chain a.yaml -> b.yaml -> outside")
        (self.e2e / "d.yaml").symlink_to("compose.recorded.yaml")
        (self.e2e / "c.yaml").symlink_to("d.yaml")
        self.clear()
        r = self.up(AP_COMPOSE_OVERRIDE=self.e2e / "c.yaml")
        self.assertEqual(r.returncode, 0, "a chain that stays inside: " + r.stderr)
        self.assertEveryComposeCall("up", [self.compose_yaml, self.override])

    # refused: exit 3, nothing started
    def test_paths_outside_the_folder_are_refused(self):
        outside = self.base / "outside.yaml"
        outside.write_text("services: {}\n")
        sibling = self.tree / "src/tests/e2e-other"
        sibling.mkdir()
        (sibling / "x.yaml").write_text("services: {}\n")
        (self.e2e / "sub").mkdir()
        (self.e2e / "link.yaml").symlink_to(outside)
        outdir = self.base / "outdir"
        outdir.mkdir()
        (outdir / "x.yaml").write_text("services: {}\n")
        (self.e2e / "linkdir").symlink_to(outdir)
        cases = {
            "outside the repository": outside,
            "a sibling folder sharing the prefix": sibling / "x.yaml",
            "a .. that leaves the folder": str(self.e2e) + "/../../deploy/compose.yaml",
            "a .. into the sibling": str(self.e2e) + "/../e2e-other/x.yaml",
            "a symlink resolving outside": self.e2e / "link.yaml",
            "a file through a symlinked folder": self.e2e / "linkdir/x.yaml",
            "the folder itself": self.e2e,
            "a folder inside it": self.e2e / "sub",
            "a missing file": self.e2e / "missing.yaml",
            "the real repository's folder": REPO / "src/tests/e2e/compose.recorded.yaml",
        }
        for what, path in cases.items():
            with self.subTest(what=what):
                self.clear()
                self.session.unlink(missing_ok=True)
                r = self.up(AP_COMPOSE_OVERRIDE=path)
                self.assertNothingStarted(r, what)


if __name__ == "__main__":
    unittest.main()
