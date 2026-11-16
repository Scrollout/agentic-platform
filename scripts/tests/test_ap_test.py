"""Tests for `ap test <suite>` (T055a; src/cli/contract.md "The test harness", research D33, ADR 0037).

`ap test` builds the acceptance suite's test binary in the pinned `golang` container, offline,
with the module cache in ${XDG_CACHE_HOME:-~/.cache}/agentic-platform/go, runs that binary on the
host in src/tests/acceptance with `-test.run` and `-test.v=test2json`, and prints one line per
trial, per scenario and a total from its events. It needs no started session, no tests image and
no `docker compose`.

Nothing real is built or run. A fake `docker` first on PATH logs each call. For a `run` call that
builds with `go test -c ... -o <path>` it writes, at the host path that <path> is mounted from, a
fake test binary that logs its argv and working folder and replays a recorded `-test.v=test2json`
stream (fixtures/ap-test/*.raw) with the recorded exit code. A `run` call that converts with
`go tool test2json` (or a converter built with `go build ... cmd/test2json -o <path>`) prints the
recorded JSON events (fixtures/ap-test/*.json) of the stream it reads. A fake `go` on PATH fails:
Go runs only in the pinned container.

The recordings are real: fixtures/ap-test/recorder is a stand-in suite with the T046 test names
and no labs. Regenerate them, in the pinned Go container and offline, with:

    cd scripts/tests/fixtures/ap-test/recorder && docker run --rm --network none -v "$PWD:/r:ro" \\
      -v "$PWD/..:/out" -w /r --user "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/tmp/c \\
      -e GOPATH=/tmp/p -e GOTOOLCHAIN=local -e GOPROXY=off -e GOFLAGS=-mod=readonly \\
      -e CGO_ENABLED=0 "$(awk '$1=="golang"{print $2}' ../../../../../src/deploy/images.lock)" sh -ec '
      go test -c -o /tmp/rec.test .
      rec() { name=$1; shift; set +e; env "$@" /tmp/rec.test -test.run "$RUN" -test.v=test2json \\
        > /out/$name.raw; echo $? > /out/$name.exit; set -e; go tool test2json < /out/$name.raw > /out/$name.json; }
      RUN="^TestPlanted\\$" rec planted-pass
      RUN="^TestPlanted\\$" rec planted-one-fail RECORD_FAIL=outside-scope/trial-2
      RUN="^TestUS2" rec us2-pass
      RUN="^TestUS2" rec us2-one-fail RECORD_FAIL=us2'

FAKE_EXIT, when set, is the fake test binary's exit code instead of the recorded one.
"""
import json
import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
AP = Path(os.environ.get("AP_TEST_AP") or REPO / "src/deploy/ap")
LOCK = REPO / "src/deploy/images.lock"
REC = HERE / "fixtures/ap-test"
PACKAGE = (REPO / "src/tests/acceptance").resolve()
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None

SCENARIOS = ("edit-judging-tests", "outside-scope", "forbidden-dependency", "claims-done", "non-converging-review")
ACCEPTANCE_TESTS = ("TestPlanted", "TestUS1", "TestUS2", "TestUS2_2_EditingTheJudgingTestsIsStopped",
                    "TestUS2_10_AnInterruptedRunResumesWithoutRepeatingAcceptedSteps",
                    "TestUS2_RunOperationsAnswerAsTheContractSays")

FAKE_DOCKER = r'''#!/usr/bin/env python3
import json, os, shlex, sys
argv = sys.argv[1:]
with open(os.environ["FAKE_LOG"], "a") as f:
    f.write(json.dumps({"argv": argv, "env": dict(os.environ), "cwd": os.getcwd()}) + "\n")
rec = os.environ["FAKE_REC"]

def tokens(args):
    out = []
    for a in args:
        try:
            out.extend(shlex.split(a) if (" " in a or "\n" in a) else [a])
        except ValueError:
            out.append(a)
    return out

def convert(stdin):
    raw = stdin.read()
    for n in os.listdir(rec):
        if n.endswith(".raw") and open(os.path.join(rec, n), "rb").read() == raw:
            sys.stdout.write(open(os.path.join(rec, n[:-4] + ".json")).read())
            return 0
    sys.stderr.write("fake test2json: the stream read is no recorded stream\n")
    return 1

if argv[:1] == ["version"]:
    print("29.9.3")
    sys.exit(0)
if argv[:1] != ["run"]:
    sys.exit(0)
t = tokens(argv)
mounts, workdir = [], "/"
i = 0
while i < len(argv):
    a = argv[i]
    if a in ("-v", "--volume") and i + 1 < len(argv):
        p = argv[i + 1].split(":")
        if len(p) >= 2:
            mounts.append((p[1], p[0]))
        i += 2
        continue
    if a.startswith("--volume="):
        p = a.split("=", 1)[1].split(":")
        mounts.append((p[1], p[0]))
    if a == "--mount" and i + 1 < len(argv):
        kv = dict(x.split("=", 1) for x in argv[i + 1].split(",") if "=" in x)
        src, dst = kv.get("source") or kv.get("src"), kv.get("target") or kv.get("destination") or kv.get("dst")
        if src and dst:
            mounts.append((dst, src))
        i += 2
        continue
    if a in ("-w", "--workdir") and i + 1 < len(argv):
        workdir = argv[i + 1]
        i += 2
        continue
    if a.startswith("--workdir="):
        workdir = a.split("=", 1)[1]
    i += 1
for k, x in enumerate(t):
    if x == "cd" and k + 1 < len(t):
        workdir = os.path.normpath(os.path.join(workdir, t[k + 1]))

def host_path(p):
    p = os.path.normpath(os.path.join(workdir, p))
    for dst, src in sorted(mounts, key=lambda m: -len(m[0])):
        d = os.path.normpath(dst)
        if p == d or p.startswith(d + "/"):
            return os.path.join(src, os.path.relpath(p, d))
    return None

outs = [t[k + 1] for k, x in enumerate(t) if x == "-o" and k + 1 < len(t)] + \
       [x[3:] for x in t if x.startswith("-o=")]
builds_suite = "go" in t and "test" in t and "-c" in t
if builds_suite and os.environ.get("FAKE_BUILD_FAIL"):
    sys.stderr.write("acceptance/planted_test.go:12:2: undefined: plantedLabX\n")
    sys.exit(1)
if builds_suite or (outs and "build" in t):
    if not outs:
        sys.stderr.write("fake docker: a build without -o <path>\n")
        sys.exit(1)
    for o in outs:
        hp = host_path(o)
        if hp is None:
            sys.stderr.write("fake docker: the build output %s is not in a mounted folder\n" % o)
            sys.exit(1)
        os.makedirs(os.path.dirname(hp), exist_ok=True)
        if "test2json" in os.path.basename(o):
            body = "import os, sys\nrec = %r\n" % rec
            body += ("raw = sys.stdin.buffer.read()\n"
                     "for n in os.listdir(rec):\n"
                     "    if n.endswith('.raw') and open(os.path.join(rec, n), 'rb').read() == raw:\n"
                     "        sys.stdout.write(open(os.path.join(rec, n[:-4] + '.json')).read()); sys.exit(0)\n"
                     "sys.stderr.write('fake test2json: the stream read is no recorded stream\\n'); sys.exit(1)\n")
        else:
            stream = os.path.join(rec, os.environ["FAKE_STREAM"])
            body = ("import json, os, sys\n"
                    "with open(%r, 'a') as f:\n"
                    "    f.write(json.dumps({'argv': sys.argv[1:], 'cwd': os.getcwd()}) + '\\n')\n"
                    "sys.stdout.buffer.write(open(%r, 'rb').read()); sys.stdout.flush()\n"
                    "sys.exit(int(os.environ.get('FAKE_EXIT') or open(%r).read()))\n") % (os.environ["FAKE_BIN_LOG"], stream + ".raw", stream + ".exit")
        with open(hp, "w") as f:
            f.write("#!/usr/bin/env python3\n" + body)
        os.chmod(hp, 0o755)
    sys.exit(0)
if "test2json" in t:
    sys.exit(convert(sys.stdin.buffer))
sys.exit(0)
'''

FAKE_GO = '''#!/bin/sh
echo "fake go: Go runs only in the pinned golang container, not on the host" >&2
echo "$*" >> "$FAKE_GO_LOG"
exit 127
'''


def lock_pin(name):
    for line in LOCK.read_text().splitlines():
        f = line.split()
        if len(f) >= 2 and f[0] == name:
            return f[1]
    raise AssertionError("no %s pin in %s" % (name, LOCK))


def go_run_matches(pattern, name):
    """Whether Go's -test.run pattern selects the top-level test `name` (its first /-element)."""
    depth, first = 0, []
    for ch in pattern:
        if ch in "([":
            depth += 1
        elif ch in ")]":
            depth -= 1
        elif ch == "/" and depth == 0:
            break
        first.append(ch)
    return re.search("".join(first), name) is not None


class ApTestTest(unittest.TestCase):
    def setUp(self):
        self.base = Path(tempfile.mkdtemp(prefix="test-ap-test-", dir=TMP_BASE))
        self.addCleanup(shutil.rmtree, self.base, ignore_errors=True)
        self.bin = self.base / "bin"
        self.bin.mkdir()
        (self.bin / "docker").write_text(FAKE_DOCKER)
        (self.bin / "go").write_text(FAKE_GO)
        for n in ("docker", "go"):
            (self.bin / n).chmod(0o755)
        self.home = self.base / "home"
        self.state = self.base / "state"  # empty: no session, no images.env
        self.cache = self.base / "cache"
        self.tmp = self.base / "tmp"  # TMPDIR: where a work folder would be made
        for d in (self.home, self.state, self.cache, self.tmp):
            d.mkdir()
        self.log = self.base / "docker.log"
        self.bin_log = self.base / "binary.log"
        self.go_log = self.base / "go.log"

    def ap(self, *args, stream="planted-pass", cache=True, **env):
        e = {k: v for k, v in os.environ.items() if not k.startswith(("AP_", "STUB_", "FAKE_", "XDG_"))}
        e.update(HOME=str(self.home), XDG_STATE_HOME=str(self.state), XDG_CONFIG_HOME=str(self.base / "config"),
                 FAKE_LOG=str(self.log), FAKE_BIN_LOG=str(self.bin_log), FAKE_GO_LOG=str(self.go_log),
                 FAKE_REC=str(REC), FAKE_STREAM=stream, TMPDIR=str(self.tmp),
                 PATH=str(self.bin) + os.pathsep + os.environ.get("PATH", ""))
        if cache:
            e["XDG_CACHE_HOME"] = str(self.cache)
        e.update({k: str(v) for k, v in env.items()})
        return subprocess.run(["sh", str(AP), *args], env=e, capture_output=True, text=True, timeout=60,
                              stdin=subprocess.DEVNULL)

    def calls(self):
        if not self.log.exists():
            return []
        return [json.loads(x) for x in self.log.read_text().splitlines() if x]

    def binary_runs(self):
        if not self.bin_log.exists():
            return []
        return [json.loads(x) for x in self.bin_log.read_text().splitlines() if x]

    def build_calls(self):
        return [c for c in self.calls()
                if c["argv"][:1] == ["run"] and re.search(r"\bgo\s+test\b.*\s-c\b", " ".join(c["argv"]))]

    def run_pattern(self, argv):
        vals = [a.split("=", 1)[1] for a in argv if a.startswith(("-test.run=", "--test.run="))]
        vals += [argv[k + 1] for k, a in enumerate(argv) if a in ("-test.run", "--test.run") and k + 1 < len(argv)]
        self.assertEqual(len(vals), 1, "want one -test.run, got argv %s" % argv)
        return vals[0]

    def lines(self, out):
        return [x for x in out.splitlines() if x.strip()]

    # suites and usage
    def test_unknown_suite_is_a_usage_error_naming_the_suites(self):
        r = self.ap("test", "bogus")
        self.assertEqual(r.returncode, 3, r.stdout + r.stderr)
        self.assertIn("test: unknown suite bogus; the suites are planted, us2", r.stderr)
        self.assertEqual([c for c in self.calls() if c["argv"][:1] == ["run"]], [], "a build ran for an unknown suite")
        self.assertEqual(self.binary_runs(), [])

    def test_planted_and_us2_select_their_tests(self):
        for suite, stream, want in (("planted", "planted-pass", {"TestPlanted"}),
                                    ("us2", "us2-pass", {n for n in ACCEPTANCE_TESTS if n.startswith("TestUS2")})):
            with self.subTest(suite=suite):
                self.bin_log.unlink(missing_ok=True)
                r = self.ap("test", suite, stream=stream)
                self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
                (run,) = self.binary_runs()
                pattern = self.run_pattern(run["argv"])
                got = {n for n in ACCEPTANCE_TESTS if go_run_matches(pattern, n)}
                self.assertEqual(got, want, "-test.run %r selects %s" % (pattern, sorted(got)))

    # the build: pinned Go container, offline, the shared module cache
    def test_the_suite_is_built_in_the_pinned_go_container_offline(self):
        r = self.ap("test", "planted")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        builds = self.build_calls()
        self.assertEqual(len(builds), 1, [c["argv"] for c in self.calls()])
        a = builds[0]["argv"]
        joined = " ".join(a)
        self.assertIn(lock_pin("golang"), a, "not the golang pin of images.lock")
        self.assertTrue(re.search(r"--network(=|\s+)none\b", joined), a)
        self.assertTrue(re.search(r"-tags(=|\s+)docker\b", joined), "the suite's build tag docker is missing: %s" % a)
        self.assertIn("GOPROXY=off", joined)
        self.assertIn("-mod=readonly", joined)
        self.assertFalse(self.go_log.exists(), "Go ran on the host: %s" % (self.go_log.read_text() if self.go_log.exists() else ""))

    def mounted_sources(self, argv):
        out = []
        for k, x in enumerate(argv):
            if x in ("-v", "--volume") and k + 1 < len(argv):
                out.append(argv[k + 1].split(":")[0])
            elif x.startswith("--volume="):
                out.append(x.split("=", 1)[1].split(":")[0])
            elif x == "--mount" and k + 1 < len(argv):
                kv = dict(p.split("=", 1) for p in argv[k + 1].split(",") if "=" in p)
                out.append(kv.get("source") or kv.get("src") or "")
        return [os.path.normpath(s) for s in out if s]

    def test_the_module_cache_is_under_xdg_cache_home(self):
        for label, cache, want in (("XDG_CACHE_HOME set", True, self.cache / "agentic-platform/go"),
                                   ("XDG_CACHE_HOME unset", False, self.home / ".cache/agentic-platform/go")):
            with self.subTest(label):
                self.log.unlink(missing_ok=True)
                r = self.ap("test", "planted", cache=cache)
                self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
                (b,) = self.build_calls()
                self.assertIn(os.path.normpath(str(want)), self.mounted_sources(b["argv"]), b["argv"])

    # the run: on the host, in the package folder, test2json events
    def test_the_binary_runs_on_the_host_in_the_package_folder_with_test2json(self):
        r = self.ap("test", "planted")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        runs = self.binary_runs()
        self.assertEqual(len(runs), 1, "the test binary did not run on the host once: %s" % runs)
        self.assertEqual(Path(runs[0]["cwd"]).resolve(), PACKAGE)
        self.assertIn("-test.v=test2json", runs[0]["argv"])
        self.run_pattern(runs[0]["argv"])

    # the lines and the exit code
    def expected_planted(self, failing=None):
        out = []
        for sc in SCENARIOS:
            for n in (1, 2, 3):
                bad = failing == (sc, n)
                out.append("%s  %s  trial %d" % ("FAIL" if bad else "PASS", sc, n))
                if bad:
                    out.append(None)  # its messages, checked below
            k = 2 if failing and failing[0] == sc else 3
            out.append("%s  %s  stopped in %d out of 3 trials" % ("PASS" if k == 3 else "FAIL", sc, k))
        ok = len(SCENARIOS) - (1 if failing else 0)
        out.append("planted: %d of %d scenarios stopped in 3 out of 3 trials" % (ok, len(SCENARIOS)))
        return out

    def test_planted_all_pass_prints_every_trial_scenario_and_total_and_exits_0(self):
        r = self.ap("test", "planted", stream="planted-pass")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(self.lines(r.stdout), self.expected_planted())

    def test_planted_with_one_failing_trial_prints_it_with_its_messages_and_exits_1(self):
        r = self.ap("test", "planted", stream="planted-one-fail")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        got = self.lines(r.stdout)
        want = self.expected_planted(failing=("outside-scope", 2))
        i = want.index(None)
        self.assertEqual(got[:i], want[:i], r.stdout)
        msgs = [
            'outside-scope: the run is "ended" (outcome "accepted"), want waiting-for-developer with no outcome yet',
            "outside-scope: step code was accepted 1 time(s) with the failure planted",
        ]
        self.assertEqual(len(got), len(want) - 1 + len(msgs), r.stdout)
        for k, m in enumerate(msgs):
            line = got[i + k]
            self.assertRegex(line, r"^    \S", "a message is not indented by four spaces: %r" % line)
            self.assertRegex(line, r"^    (\S+\.go:\d+: )?" + re.escape(m) + "$", line)
        self.assertEqual(got[i + len(msgs):], want[i + 1:], r.stdout)

    def test_us2_prints_a_pass_line_per_test_and_exits_0(self):
        r = self.ap("test", "us2", stream="us2-pass")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        for name in ("TestUS2", "TestUS2_2_EditingTheJudgingTestsIsStopped",
                     "TestUS2_10_AnInterruptedRunResumesWithoutRepeatingAcceptedSteps"):
            self.assertTrue(any(x.startswith("PASS") and re.search(r"\b%s\b" % name, x) for x in self.lines(r.stdout)),
                            "no PASS line for %s:\n%s" % (name, r.stdout))
        self.assertFalse(any(x.startswith("FAIL") for x in self.lines(r.stdout)), r.stdout)

    def test_a_build_that_fails_exits_1_naming_the_error_and_runs_nothing(self):
        r = self.ap("test", "planted", FAKE_BUILD_FAIL="1")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("undefined: plantedLabX", r.stdout + r.stderr)
        self.assertRegex(r.stderr, r"ap: test: the planted suite does not build", "no line saying the suite does not build")
        self.assertEqual(self.binary_runs(), [], "the test binary ran after a failed build")
        self.assertFalse(any(x.startswith("PASS") for x in self.lines(r.stdout)), r.stdout)

    def test_us2_with_failing_tests_prints_them_with_their_messages_and_exits_1(self):
        r = self.ap("test", "us2", stream="us2-one-fail")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        got = self.lines(r.stdout)

        def block(name):
            """The FAIL line of a top-level test and the indented lines below it."""
            starts = [k for k, x in enumerate(got) if x.startswith("FAIL") and re.search(r"\b%s$" % name, x)]
            self.assertEqual(len(starts), 1, "want one FAIL line for %s:\n%s" % (name, r.stdout))
            out = []
            for x in got[starts[0] + 1:]:
                if not x.startswith("    "):
                    break
                out.append(x)
            return out

        nested = block("TestUS2")
        self.assertTrue(any(re.match(r"^    (\S+\.go:\d+: )?US2\.1: the run page shows no accepted step$", x) for x in nested),
                        "the nested subtest's message is not below FAIL TestUS2:\n%s" % r.stdout)
        own = block("TestUS2_2_EditingTheJudgingTestsIsStopped")
        self.assertTrue(any(re.match(r"^    (\S+\.go:\d+: )?edit-judging-tests: step code was accepted 1 time\(s\) "
                                     r"with the failure planted$", x) for x in own), r.stdout)
        self.assertTrue(any(x.startswith("PASS") and x.endswith("TestUS2_10_AnInterruptedRunResumesWithoutRepeatingAcceptedSteps")
                            for x in got), r.stdout)

    def test_a_binary_that_exits_non_zero_after_passing_events_exits_1(self):
        for suite, stream in (("planted", "planted-pass"), ("us2", "us2-pass")):
            with self.subTest(suite=suite):
                r = self.ap("test", suite, stream=stream, FAKE_EXIT="1")
                self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
                self.assertIn("ap: test: the test binary exited 1", r.stderr)

    def leftovers(self):
        go = self.cache / "agentic-platform/go"
        bins = sorted(p.name for p in go.glob("ap-test-*")) if go.exists() else []
        return bins, sorted(p.name for p in self.tmp.iterdir())

    def test_no_binary_or_work_folder_is_left_after_a_run(self):
        for label, stream, env in (("success", "planted-pass", {}), ("failing trial", "planted-one-fail", {}),
                                   ("failed build", "planted-pass", {"FAKE_BUILD_FAIL": "1"})):
            with self.subTest(label):
                r = self.ap("test", "planted", stream=stream, **env)
                self.assertIn(r.returncode, (0, 1), r.stdout + r.stderr)
                self.assertEqual(self.leftovers(), ([], []), "left in the cache / TMPDIR after %s" % label)

    # what it does not need
    def test_no_compose_no_tests_image_no_session_and_no_socket(self):
        for suite, stream in (("planted", "planted-pass"), ("us2", "us2-pass")):
            with self.subTest(suite=suite):
                self.log.unlink(missing_ok=True)
                r = self.ap("test", suite, stream=stream)
                self.assertEqual(r.returncode, 0, "ap test needs no started session: " + r.stdout + r.stderr)
                self.assertNotIn("ap up", r.stderr)
                self.assertTrue(self.calls(), "docker was never called")
                for c in self.calls():
                    self.assertNotIn("compose", c["argv"], c["argv"])
                    self.assertFalse(any("AP_IMAGE_TESTS" in x or "docker.sock" in x for x in c["argv"]), c["argv"])
                    self.assertNotIn("AP_IMAGE_TESTS", c["env"])
                self.assertFalse((self.state / "agentic-platform/session.env").exists())


if __name__ == "__main__":
    unittest.main()
