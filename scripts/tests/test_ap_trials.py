"""Tests for the launcher's `ap trials <slice> <prefix> [--trials <k>]` (feature 012, task T010).

Judged from src/cli/contract.md ("Commands": the launcher intercepts `trials`; "Trial sets": the exit
codes) and specs/012-run-variance/spec.md, not from the launcher's code: `ap trials` runs
`build-workers <slice> <prefix>-<i>` for i = 1 to k (3 without `--trials`, the engine contract's
`x-limits.trials-default`; k from 1 to `x-limits.trials-max`), in that order, then passes the command
on to the terminal client. It stops at the first `build-workers` that fails, with that command's exit
code, and starts no set. A k outside 1 to the maximum is a usage error (exit 3) before anything
changes. The client's exit code is the launcher's.

Nothing real runs, and no Docker is needed. The setup is that of test_ap_passthrough.py: the session
is written as `ap up` writes it, and a fake `docker` first on PATH logs each call and stands in for

- the engine: a `docker run ... nc ... engine 8080` call reads the HTTP request on its standard input;
  listRuns answers no runs and registerWorker answers 200, recording the project it names, unless the
  project is FAKE_FAIL_PROJECT, which it answers with FAKE_FAIL_STATUS (404 is exit 4, 500 exit 1);
- the step containers: `container inspect` finds none and `compose run -d` starts one;
- the terminal client: `compose run ... cli <command>` prints CLIENT_OUT, exits FAKE_CLI_EXIT.

The projects folder is real (a temporary folder), so a project that `build-workers` made is seen on
disk. The tests fail today: the launcher does not know `trials` and passes it on at once.
"""
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
AP = Path(os.environ.get("AP_TEST_AP") or REPO / "src/deploy/ap")
OPENAPI = REPO / "src/engine/api/preview/2026-11-12-preview/openapi.yaml"
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
TOKENS = ("AP_CLIENT_TOKEN", "AP_PAGE_TOKEN", "AP_WORKER_TOKEN", "AP_LINT_TOKEN", "AP_ANALYST_TOKEN",
          "AP_CHECK_TOKEN", "AP_PROJECT_TOKEN")
IMAGES = ("engine", "cli", "page", "agent", "check", "project", "tests", "lint")
CLIENT_OUT = "TS-1  operation OP-5\ntrial 1  RUN-21  done\n"


def limit(name):
    """x-limits.<name> of the engine contract: the tests carry no limit of their own."""
    for line in OPENAPI.read_text().splitlines():
        s = line.strip()
        if s.startswith(name + ":"):
            return int(s.split(":", 1)[1])
    raise AssertionError("no x-limits.%s in %s" % (name, OPENAPI))


TRIALS_MAX = limit("trials-max")
TRIALS_DEFAULT = limit("trials-default")

FAKE_DOCKER = r'''#!/usr/bin/env python3
import json, os, sys
argv = sys.argv[1:]

def log(extra):
    with open(os.environ["FAKE_LOG"], "a") as f:
        f.write(json.dumps(dict(argv=argv, **extra)) + "\n")

def answer(status, body):
    sys.stdout.write("HTTP/1.0 %d X\r\nContent-Type: application/json\r\n\r\n%s" % (status, json.dumps(body)))
    sys.exit(0)

if argv[:1] == ["version"]:
    log({}); print("29.9.3"); sys.exit(0)
if argv[:1] == ["run"] and "nc" in argv:
    raw = sys.stdin.read()
    head, _, body = raw.partition("\r\n\r\n")
    req = head.split("\r\n")[0].split(" ")
    method, path = req[0], req[1] if len(req) > 1 else ""
    if method == "GET" and path.startswith("/runs?"):
        log({}); answer(200, {"items": []})
    if method == "POST" and path.startswith("/workers?"):
        project = json.loads(body).get("project", "")
        log({"register": project})
        if project == os.environ.get("FAKE_FAIL_PROJECT"):
            answer(int(os.environ.get("FAKE_FAIL_STATUS", "404")), {"code": "NotFound"})
        answer(200, {})
    log({}); answer(404, {"code": "NotFound"})
if argv[:1] == ["run"]:
    if "-i" in argv:
        sys.stdin.read()
    log({}); sys.exit(0)
if argv[:2] == ["container", "inspect"]:
    log({}); sys.exit(1)
if argv[:1] == ["compose"] and "run" in argv and "cli" in argv and "-d" not in argv:
    log({"client": argv[argv.index("cli", argv.index("run")) + 1:]})
    sys.stdout.write(os.environ["CLIENT_OUT"]); sys.stdout.flush()
    sys.exit(int(os.environ.get("FAKE_CLI_EXIT", "0")))
log({}); sys.exit(0)
'''


class ApTrialsTest(unittest.TestCase):
    def setUp(self):
        self.base = Path(tempfile.mkdtemp(prefix="test-ap-trials-", dir=TMP_BASE))
        self.addCleanup(shutil.rmtree, self.base, ignore_errors=True)
        self.bin = self.base / "bin"
        self.bin.mkdir()
        (self.bin / "docker").write_text(FAKE_DOCKER)
        (self.bin / "docker").chmod(0o755)
        self.home, self.state, self.proj = self.base / "home", self.base / "state", self.base / "projects"
        for d in (self.home, self.state, self.proj):
            d.mkdir()
        apdir = self.state / "agentic-platform"
        apdir.mkdir(mode=0o700)
        grants = apdir / "grants"
        grants.mkdir(mode=0o700)
        cred = self.base / "models.env"
        cred.write_text("fake-key\n")
        cred.chmod(0o600)
        values = {"AP_CREDENTIALS_FILE": str(cred), "AP_PROJECTS_DIR": str(self.proj), "AP_PAGE_PORT": "7878",
                  "AP_HOST_USER": "%d:%d" % (os.getuid(), os.getgid()), "AP_GATEWAY_IMAGE": "gateway@sha256:" + "0" * 64,
                  "AP_GRANTS_DIR": str(grants)}
        for i, t in enumerate(TOKENS):
            values[t] = ("%02x" % (i + 1)) * 32
        for i, n in enumerate(IMAGES):
            values["AP_IMAGE_" + n.upper()] = "127.0.0.1:5005/ap-%s@sha256:%s" % (n, ("%02x" % (i + 0x40)) * 32)
        sess = apdir / "session.env"
        sess.write_text("".join("%s='%s'\n" % kv for kv in values.items()))
        sess.chmod(0o600)
        self.log = self.base / "docker.log"

    def env(self, **extra):
        e = {k: v for k, v in os.environ.items() if not k.startswith(("AP_", "STUB_", "FAKE_", "XDG_"))}
        e.update(HOME=str(self.home), XDG_STATE_HOME=str(self.state), XDG_CONFIG_HOME=str(self.base / "config"),
                 XDG_CACHE_HOME=str(self.base / "cache"), FAKE_LOG=str(self.log), CLIENT_OUT=CLIENT_OUT,
                 PATH=str(self.bin) + os.pathsep + os.environ.get("PATH", ""))
        e.update({k: str(v) for k, v in extra.items()})
        return e

    def ap(self, *args, **extra):
        return subprocess.run(["sh", str(AP), *[str(a) for a in args]], env=self.env(**extra), capture_output=True,
                              text=True, timeout=60, stdin=subprocess.DEVNULL)

    def calls(self):
        if not self.log.exists():
            return []
        return [json.loads(x) for x in self.log.read_text().splitlines() if x]

    def registered(self):
        """The projects registerWorker named, in call order (each once per step)."""
        return [c["register"] for c in self.calls() if "register" in c]

    def projects_in_order(self):
        """The distinct projects of the registrations, in order of first registration."""
        out = []
        for p in self.registered():
            if p not in out:
                out.append(p)
        return out

    def client_calls(self):
        return [c["client"] for c in self.calls() if "client" in c]

    def assertBuiltInOrder(self, prefix, k):
        """Every registration of <prefix>-i comes before any of <prefix>-(i+1), all before the
        client's call; the project folders 1..k exist."""
        want = ["%s-%d" % (prefix, i) for i in range(1, k + 1)]
        self.assertEqual(self.projects_in_order(), want)
        reg = self.registered()
        for a, b in zip(want, want[1:], strict=False):
            self.assertLess(max(i for i, p in enumerate(reg) if p == a), min(i for i, p in enumerate(reg) if p == b),
                            "%s was not built completely before %s" % (a, b))
        seen_client = False
        for c in self.calls():
            if "client" in c:
                seen_client = True
            elif "register" in c:
                self.assertFalse(seen_client, "build-workers ran after the client started")
        for p in want:
            self.assertTrue((self.proj / p).is_dir(), "no project folder %s" % p)

    def assertNothingChanged(self):
        self.assertEqual(self.registered(), [], "build-workers registered a worker")
        self.assertEqual(self.client_calls(), [], "the client was called")
        self.assertEqual(sorted(p.name for p in self.proj.iterdir()), [], "a project folder was made")

    # ---- the builds, then the client ----

    def test_without_trials_it_builds_the_default_count_of_projects_then_starts_the_set(self):
        r = self.ap("trials", "S-1", "notes")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertBuiltInOrder("notes", TRIALS_DEFAULT)
        (args,) = self.client_calls()
        self.assertEqual(args[:3], ["trials", "S-1", "notes"])
        self.assertIn(args[3:], ([], ["--trials", str(TRIALS_DEFAULT)]),
                      "the client must get the command as given or with the default k")

    def test_it_builds_exactly_k_projects_for_each_k_from_1_to_the_maximum(self):
        for k in range(1, TRIALS_MAX + 1):
            with self.subTest(k=k):
                self.log.unlink(missing_ok=True)
                prefix = "run%d" % k
                r = self.ap("trials", "S-1", prefix, "--trials", k)
                self.assertEqual(r.returncode, 0, r.stderr)
                self.assertBuiltInOrder(prefix, k)
                self.assertFalse((self.proj / ("%s-%d" % (prefix, k + 1))).exists(), "more projects than k")
                (args,) = self.client_calls()
                self.assertEqual(args, ["trials", "S-1", prefix, "--trials", str(k)])

    def test_every_step_of_a_project_is_registered_before_the_next_project(self):
        r = self.ap("trials", "S-1", "steps", "--trials", 2)
        self.assertEqual(r.returncode, 0, r.stderr)
        reg = self.registered()
        per = len(reg) // 2
        self.assertGreater(per, 1, "build-workers registers one worker per step")
        self.assertEqual(reg, ["steps-1"] * per + ["steps-2"] * per)

    def test_the_client_runs_once_and_its_output_is_the_launchers_standard_output(self):
        r = self.ap("trials", "S-1", "once", "--trials", 2)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(len(self.client_calls()), 1)
        self.assertEqual(r.stdout, CLIENT_OUT)
        for line in r.stderr.splitlines():
            self.assertTrue(line.startswith("ap: "), "standard error holds a line that is not the launcher's: %r" % line)

    def test_the_clients_exit_code_is_the_launchers_for_every_code_of_the_table(self):
        for code in (0, 7, 6, 4, 1, 130, 143):
            with self.subTest(code=code):
                self.log.unlink(missing_ok=True)
                r = self.ap("trials", "S-1", "code%d" % code, "--trials", 2, FAKE_CLI_EXIT=code)
                self.assertEqual(len(self.client_calls()), 1)
                self.assertEqual(r.returncode, code, r.stderr)

    def test_a_prefix_of_the_longest_length_builds_projects_of_the_longest_name(self):
        prefix = "a" * 61  # `<prefix>-<i>` is then a project name of 63 characters, the most
        r = self.ap("trials", "S-1", prefix, "--trials", TRIALS_MAX)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertBuiltInOrder(prefix, TRIALS_MAX)

    def test_a_prefix_too_long_for_the_project_name_exits_3_and_changes_nothing(self):
        r = self.ap("trials", "S-1", "a" * 62, "--trials", 2)
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertNothingChanged()

    # ---- the first build-workers that fails stops it ----

    def test_a_failing_build_workers_stops_it_with_that_exit_code_and_starts_no_set(self):
        for k, failing in ((2, 1), (3, 1), (3, 2), (3, 3), (5, 4)):
            for status, code in ((404, 4), (500, 1)):
                with self.subTest(k=k, failing=failing, status=status):
                    self.log.unlink(missing_ok=True)
                    prefix = "f%d%d%d" % (k, failing, status)
                    r = self.ap("trials", "S-1", prefix, "--trials", k,
                                FAKE_FAIL_PROJECT="%s-%d" % (prefix, failing), FAKE_FAIL_STATUS=status)
                    self.assertEqual(r.returncode, code, r.stderr)
                    self.assertEqual(self.client_calls(), [], "a set was started after build-workers failed")
                    self.assertEqual(self.projects_in_order(), ["%s-%d" % (prefix, i) for i in range(1, failing + 1)],
                                     "build-workers went on past the first failure, or did not reach it")
                    self.assertIn("build-workers", r.stderr)

    def test_a_project_that_already_holds_a_file_stops_it_with_exit_3_and_leaves_the_others(self):
        taken = self.proj / "taken-2"
        taken.mkdir()
        (taken / "main.go").write_text("package main\n")
        r = self.ap("trials", "S-1", "taken", "--trials", 3)
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertEqual(self.client_calls(), [])
        self.assertEqual(self.projects_in_order(), ["taken-1"])
        self.assertTrue((self.proj / "taken-1").is_dir(), "the project already made stays")
        self.assertFalse((self.proj / "taken-3").exists(), "no project is made after the failing one")
        self.assertEqual((taken / "main.go").read_text(), "package main\n")

    def test_running_it_again_with_a_new_prefix_after_a_failure_builds_and_starts_the_set(self):
        r = self.ap("trials", "S-1", "again", "--trials", 2, FAKE_FAIL_PROJECT="again-2", FAKE_FAIL_STATUS=404)
        self.assertEqual(r.returncode, 4, r.stderr)
        self.log.unlink(missing_ok=True)
        r = self.ap("trials", "S-1", "again2", "--trials", 2)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertBuiltInOrder("again2", 2)
        self.assertEqual(len(self.client_calls()), 1)

    # ---- usage errors change nothing ----

    def test_a_k_outside_1_to_the_maximum_or_not_a_number_is_a_usage_error_before_anything_changes(self):
        for k in (0, TRIALS_MAX + 1, 100, -1, "abc", "1.5", "", "03x", "+2"):
            with self.subTest(k=k):
                self.log.unlink(missing_ok=True)
                r = self.ap("trials", "S-1", "usage", "--trials", k)
                self.assertEqual(r.returncode, 3, r.stderr)
                self.assertNothingChanged()

    def test_trials_with_a_missing_value_or_the_wrong_arguments_is_a_usage_error(self):
        cases = (["trials"], ["trials", "S-1"], ["trials", "S-1", "p", "--trials"], ["trials", "not-a-slice", "p"],
                 ["trials", "S-1", "Bad_Prefix"], ["trials", "S-1", "p", "--bogus"], ["trials", "S-1", "p", "extra"])
        for args in cases:
            with self.subTest(args=args):
                self.log.unlink(missing_ok=True)
                r = self.ap(*args)
                self.assertEqual(r.returncode, 3, r.stderr)
                self.assertNothingChanged()

    def test_the_bounds_one_and_the_maximum_are_not_usage_errors(self):
        for k in (1, TRIALS_MAX):
            with self.subTest(k=k):
                self.log.unlink(missing_ok=True)
                r = self.ap("trials", "S-1", "bound%d" % k, "--trials", k)
                self.assertEqual(r.returncode, 0, r.stderr)

    def test_build_still_builds_one_project_and_passes_on_unchanged(self):
        r = self.ap("build", "S-1", "solo")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.projects_in_order(), ["solo"])
        self.assertEqual(self.client_calls(), [["build", "S-1", "solo"]])


if __name__ == "__main__":
    unittest.main()
