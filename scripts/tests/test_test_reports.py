"""T111 (research D43, ADR 0046): the CI test jobs write JUnit reports and show them.

Written before the code, by a separate author. Three rules, each a judge function that returns
its violations as strings, each with a test on the repository's real files and planted cases
(a fixture that must fail), as test_workflows.py does:

1. scripts/check.sh's `go`, `docker` and `py-tests` steps write one JUnit file per Go module
   (`go-<module>.xml`), per docker test package (`docker-<package>.xml`) and for the Python check
   tests into AP_TEST_REPORT_DIR, and no file without it. Judged by running check.sh's steps in a
   throwaway copy of the repository with a fake `docker` first on PATH. The fake runs the
   container's shell command on the host, with each mounted container path (`/src`, `/gopath`,
   `/reports`, ...) replaced by its host path, and with fakes of `go` and of the `gotestsum` that
   `go install` (or `go build -o`) puts into the Go cache: the fake gotestsum writes a small JUnit
   file wherever `--junitfile` points and keeps its command's exit code. The docker step's fake
   test binaries print a test's lines and exit 0, or 1 for the package FAKE_FAIL_PACKAGE names.
   scripts/testrun.py is a fake that writes the file `--junit` names. The docker step runs after
   `check.sh --prepare`, as in its CI job (which installs the Go tools).
2. src/deploy/images.lock pins gotestsum as `gotest.tools/gotestsum@v<semver>`, among the Go tools.
3. .github/workflows/check.yml: the go, docker and py-tests jobs set AP_TEST_REPORT_DIR for their
   check step and, after it, run mikepenz/action-junit-report pinned by commit hash with
   `if: always()`, `annotate_only: true`, `detailed_summary: true`, `include_passed: false` and
   report_paths under that folder. The planted tree is test_workflows.py's (imported, unchanged)
   with these steps added; a baseline test proves it valid under test_workflows.py's rules too.
"""
import os
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
import xml.etree.ElementTree as ET
from pathlib import Path

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))

import gitenv  # noqa: E402
import test_workflows as wfx  # noqa: E402

REPO = HERE.parent.parent
CHECK = REPO / "scripts" / "check.sh"
LOCK = REPO / "src" / "deploy" / "images.lock"
GO_MODULES = ["check", "engine", "slice", "cli", "page", "tests"]
DOCKER_PACKAGES = ["recorded", "lint", "acceptance", "contract"]
GO_VERSION = "go1.26.11"

# ---------------------------------------------------------------- fakes

FAKE_DOCKER = r'''#!/usr/bin/env python3
"""docker run: the container's command on the host, mounted paths translated."""
import os, re, subprocess, sys
a = sys.argv[1:]
with open(os.environ["LOG"], "a") as f:
    f.write("docker " + " ".join(a) + "\n")
if not a or a[0] != "run":
    sys.exit(0)
mounts, env, wd, i = {}, {}, None, 1
VALUED = {"--network", "--user", "-u", "--name", "--entrypoint", "--platform", "--pull"}
while i < len(a) and a[i].startswith("-"):
    o = a[i]
    if "=" in o and o.startswith("--"):
        o, val = o.split("=", 1); step = 1
    else:
        val = a[i + 1] if i + 1 < len(a) else ""; step = 2
    if o in ("-v", "--volume"):
        parts = val.split(":")
        mounts[parts[1]] = parts[0]
    elif o in ("-w", "--workdir"):
        wd = val
    elif o in ("-e", "--env"):
        k, _, v = val.partition("=")
        env[k] = v
    elif o in VALUED:
        pass
    else:
        step = 1
    i += step
cmd = a[i + 1:]  # a[i] is the image
def tr(s):
    for c in sorted(mounts, key=len, reverse=True):
        s = re.sub(r"(?<![\w.-])" + re.escape(c) + r"(?=/|$|[^\w.-])", lambda m: mounts[c], s)
    return s
e = dict(os.environ)
e.update({k: tr(v) for k, v in env.items()})
e["PATH"] = os.environ["FAKE_BIN"] + os.pathsep + os.environ["PATH"]
r = subprocess.run([tr(x) for x in cmd], cwd=tr(wd) if wd else None, env=e)
sys.exit(r.returncode)
'''

FAKE_GO = r'''#!/usr/bin/env python3
import os, shutil, sys
a = sys.argv[1:]
with open(os.environ["LOG"], "a") as f:
    f.write("go " + " ".join(a) + "\n")
def put(dest, gotestsum):
    os.makedirs(os.path.dirname(dest) or ".", exist_ok=True)
    if gotestsum:
        shutil.copy(os.environ["FAKE_GOTESTSUM"], dest)
    else:
        open(dest, "w").write("#!/bin/sh\nexit 0\n")
    os.chmod(dest, 0o755)
def opt(name):
    return a[a.index(name) + 1] if name in a else None
if a[:2] == ["env", "GOVERSION"]:
    print(os.environ["FAKE_GO_VERSION"])
elif a and a[0] == "install":
    for p in a[1:]:
        if p.startswith("-"):
            continue
        name = p.split("@")[0].rstrip("/").split("/")[-1]
        gobin = os.environ.get("GOBIN") or os.path.join(os.environ.get("GOPATH", "."), "bin")
        put(os.path.join(gobin, name), "gotestsum" in p)
elif a and a[0] == "build" and "-o" in a:
    put(opt("-o"), any("gotestsum" in p for p in a))
elif a and a[0] == "test" and "-c" in a:
    out = opt("-o")
    pkg = os.path.basename(out).split(".")[0]
    os.makedirs(os.path.dirname(out) or ".", exist_ok=True)
    open(out, "w").write(
        "#!/bin/sh\n"
        'echo "test-binary %s $*" >>"$LOG"\n'
        "echo '=== RUN   TestFake'\n"
        'if [ "${FAKE_FAIL_PACKAGE:-}" = "%s" ]; then echo "--- FAIL: TestFake (0.00s)"; echo FAIL; exit 1; fi\n'
        "echo '--- PASS: TestFake (0.00s)'; echo PASS\n" % (pkg, pkg))
    os.chmod(out, 0o755)
elif a and a[0] == "test":
    print("ok  \tfake\t0.01s")
elif a[:2] == ["tool", "test2json"]:
    # go tool test2json [-p pkg] [-t] [command...]: the command's output, else stdin's
    b, i = a[2:], 0
    while i < len(b) and b[i] in ("-p", "-t"):
        i += 2 if b[i] == "-p" else 1
    if b[i:]:
        import subprocess
        r = subprocess.run(b[i:], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        code, src = r.returncode, r.stdout
    else:
        code, src = 0, sys.stdin.read()
    for line in src.splitlines():
        print('{"Action":"output","Output":%r}' % line.replace('"', "'"))
    sys.exit(code)
sys.exit(0)
'''

FAKE_GOTESTSUM = r'''#!/usr/bin/env python3
import os, subprocess, sys
a = sys.argv[1:]
with open(os.environ["LOG"], "a") as f:
    f.write("gotestsum " + " ".join(a) + "\n")
rest = a[a.index("--") + 1:] if "--" in a else []
opts = a[:a.index("--")] if "--" in a else a
junit, raw, i = None, False, 0
while i < len(opts):
    o = opts[i]
    if o.startswith("--junitfile="):
        junit = o.split("=", 1)[1]
    elif o == "--junitfile":
        junit = opts[i + 1]; i += 1
    elif o == "--raw-command":
        raw = True
    elif o in ("--format", "--jsonfile", "--packages", "--junitfile-testsuite-name",
               "--junitfile-testcase-classname", "-f"):
        i += 1
    i += 1
r = subprocess.run(rest if raw else ["go", "test"] + rest)
if junit:
    with open(junit, "w") as f:
        f.write('<?xml version="1.0" encoding="UTF-8"?>\n<testsuites><testsuite name="fake" '
                'tests="1"><testcase classname="fake" name="TestFake" time="0"/></testsuite>'
                '</testsuites>\n')
sys.exit(r.returncode)
'''

FAKE_TESTRUN = r'''#!/usr/bin/env python3
import os, sys
a = sys.argv[1:]
with open(os.environ["LOG"], "a") as f:
    f.write("testrun " + " ".join(a) + "\n")
for i, x in enumerate(a):
    path = x.split("=", 1)[1] if x.startswith("--junit=") else (a[i + 1] if x == "--junit" else None)
    if path:
        open(path, "w").write('<?xml version="1.0"?>\n<testsuites><testsuite name="py">'
                              '<testcase classname="t.T" name="test_x" time="0"/></testsuite></testsuites>\n')
sys.exit(0)
'''

WANT = {"go": ["go-%s.xml" % m for m in GO_MODULES],
        "docker": ["docker-%s.xml" % p for p in DOCKER_PACKAGES]}


class Repo:
    """A throwaway repository: check.sh under test, the real images.lock, stubs for the rest."""

    def __init__(self, tc, check_text=None):
        t = tempfile.TemporaryDirectory()
        tc.addCleanup(t.cleanup)
        self.root = Path(t.name)
        self.repo = self.root / "repo"
        (self.repo / "scripts").mkdir(parents=True)
        (self.repo / "src" / "deploy").mkdir(parents=True)
        (self.repo / "scripts" / "check.sh").write_text(check_text or CHECK.read_text())
        os.chmod(self.repo / "scripts" / "check.sh", 0o755)
        shutil.copy(LOCK, self.repo / "src" / "deploy" / "images.lock")
        lock = LOCK.read_text()
        pin = re.search(r"^godog\s+\S+@(\S+)", lock, re.M)
        assert pin, "images.lock has no godog pin"
        godog = pin.group(1)
        fx = self.repo / "src/engine/checks/testdata/steps/godog/clean"
        fx.mkdir(parents=True)
        (fx / "go.mod").write_text("module x\n\nrequire github.com/cucumber/godog %s\n" % godog)
        for m in GO_MODULES:
            (self.repo / "src" / m).mkdir(parents=True, exist_ok=True)
            (self.repo / "src" / m / "zz.go").write_text("package zz\n")
        for p in DOCKER_PACKAGES:
            (self.repo / "src" / "tests" / p).mkdir(parents=True, exist_ok=True)
        self._exe(self.repo / "scripts" / "generate.sh", "#!/bin/sh\nexit 0\n")
        self._exe(self.repo / "scripts" / "testrun.py", FAKE_TESTRUN)
        self.fakes = self.root / "fakes"
        self.fake_bin = self.root / "fakebin"   # inside the "container" only
        self._exe(self.fakes / "docker", FAKE_DOCKER)
        self._exe(self.fake_bin / "go", FAKE_GO)
        self._exe(self.root / "gotestsum-fake", FAKE_GOTESTSUM)
        self.log = self.root / "log"
        self.log.write_text("")
        self.reports = self.root / "reports"
        env = gitenv.clean_env(gitenv.make_home(tc))
        env.pop("AP_TEST_REPORT_DIR", None)
        env.update({"LOG": str(self.log), "XDG_CACHE_HOME": str(self.root / "cache"),
                    "FAKE_BIN": str(self.fake_bin), "FAKE_GO_VERSION": GO_VERSION,
                    "FAKE_GOTESTSUM": str(self.root / "gotestsum-fake")})
        self.env = gitenv.with_path(env, str(self.fakes))
        subprocess.run(["git", "init", "-q"], cwd=self.repo, env=self.env, check=True)

    @staticmethod
    def _exe(path, text):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
        os.chmod(path, 0o755)

    def run(self, step, with_dir, extra=None):
        env = dict(self.env, **(extra or {}))
        if with_dir:
            self.reports.mkdir(exist_ok=True)
            env["AP_TEST_REPORT_DIR"] = str(self.reports)
        if step == "docker":   # as the CI docker job does, which installs the Go tools first
            p = subprocess.run([str(self.repo / "scripts" / "check.sh"), "--prepare"],
                               cwd=self.repo, env=env, capture_output=True, text=True, timeout=90)
            if p.returncode != 0:
                return p
        return subprocess.run([str(self.repo / "scripts" / "check.sh"), "--only", step],
                              cwd=self.repo, env=env, capture_output=True, text=True, timeout=90)

    def calls(self):
        return self.log.read_text().splitlines()

    def xml_files(self):
        return sorted(str(p.relative_to(self.root)) for p in self.root.rglob("*.xml")
                      if "godog" not in str(p))


def judge_reports_written(tc, step, check_text=None, extra=None):
    """With AP_TEST_REPORT_DIR set, the step passes and writes its JUnit files there."""
    repo = Repo(tc, check_text)
    r = repo.run(step, True, extra)
    out = r.stdout + r.stderr
    v = []
    if r.returncode != 0:
        return ["check.sh --only %s failed (%d) with AP_TEST_REPORT_DIR set:\n%s"
                % (step, r.returncode, out[-3000:])]
    got = sorted(p.name for p in repo.reports.glob("*.xml"))
    if step == "py-tests":
        if len(got) != 1:
            v.append("py-tests wrote %s into AP_TEST_REPORT_DIR, not one file" % got)
        tr = [c for c in repo.calls() if c.startswith("testrun ")]
        if not any(re.search(r"--junit[= ]" + re.escape(str(repo.reports)) + "/", c) for c in tr):
            v.append("testrun.py was not given --junit <file in AP_TEST_REPORT_DIR>: %s" % tr)
    else:
        missing = [w for w in WANT[step] if w not in got]
        if missing:
            v.append("%s step wrote no %s into AP_TEST_REPORT_DIR (it holds %s)\n%s"
                     % (step, missing, got, "\n".join(repo.calls())[-3000:]))
        for name in got:
            try:
                ET.parse(repo.reports / name)
            except ET.ParseError as e:
                v.append("%s is not XML: %s" % (name, e))
    if step == "go" and "== go test -race (%s" % GO_VERSION not in r.stdout:
        v.append("the go step's '== go test -race (%s' line changed" % GO_VERSION)
    return v


def judge_no_reports_without_dir(tc, step, check_text=None):
    """Without AP_TEST_REPORT_DIR the step passes, writes no JUnit file and mounts no /reports."""
    repo = Repo(tc, check_text)
    r = repo.run(step, False)
    v = []
    if r.returncode != 0:
        return ["check.sh --only %s failed (%d) without AP_TEST_REPORT_DIR:\n%s"
                % (step, r.returncode, (r.stdout + r.stderr)[-3000:])]
    if repo.xml_files():
        v.append("%s step wrote %s without AP_TEST_REPORT_DIR" % (step, repo.xml_files()))
    calls = repo.calls()
    if any(":/reports" in c for c in calls if c.startswith("docker ")):
        v.append("%s step mounts /reports without AP_TEST_REPORT_DIR" % step)
    if step == "docker" and any("test2json" in c for c in calls if c.startswith("test-binary ")):
        v.append("docker step's test binaries run with test2json without AP_TEST_REPORT_DIR")
    if step == "py-tests" and any("--junit" in c for c in calls if c.startswith("testrun ")):
        v.append("py-tests passes --junit without AP_TEST_REPORT_DIR")
    return v


def no_junitfile(text):     # planted: gotestsum writes Go's JSON instead of JUnit
    return text.replace("--junitfile", "--jsonfile")


def no_junit(text):         # planted: testrun.py is not asked for a report
    return re.sub(r"--junit\b", "--top 15", text)


def default_dir(text):      # planted: a report folder is made up when the variable is unset
    return text.replace("set -eu\n", 'set -eu\n: "${AP_TEST_REPORT_DIR:=$PWD/zz-reports}"\n'
                        "mkdir -p \"$AP_TEST_REPORT_DIR\"\n", 1)


class ReportsWrittenTest(unittest.TestCase):
    def test_real_go_step(self):
        self.assertEqual(judge_reports_written(self, "go"), [])

    def test_real_docker_step(self):
        self.assertEqual(judge_reports_written(self, "docker"), [])

    def test_real_py_tests_step(self):
        self.assertEqual(judge_reports_written(self, "py-tests"), [])

    def test_real_docker_step_keeps_a_failing_binarys_exit_code_and_writes_its_report(self):
        repo = Repo(self)
        r = repo.run("docker", True, {"FAKE_FAIL_PACKAGE": "lint"})
        self.assertNotEqual(r.returncode, 0, "a failing test binary passed the step\n"
                            + r.stdout + r.stderr)
        got = sorted(p.name for p in repo.reports.glob("*.xml"))
        self.assertIn("docker-recorded.xml", got)
        self.assertIn("docker-lint.xml", got, "the failing package's report is missing")

    def test_planted_go_step_without_junitfile_fails(self):
        self.assertTrue(judge_reports_written(self, "go", no_junitfile(CHECK.read_text())))

    def test_planted_docker_step_without_junitfile_fails(self):
        self.assertTrue(judge_reports_written(self, "docker", no_junitfile(CHECK.read_text())))

    def test_planted_py_tests_without_junit_fails(self):
        self.assertTrue(judge_reports_written(self, "py-tests", no_junit(CHECK.read_text())))

    def test_the_fakes_can_write_a_report(self):
        # Control: a check.sh that runs gotestsum with --junitfile in a container is seen to
        # write every module's file, so a missing file is check.sh's, not the fakes'.
        text = ('#!/bin/sh\nset -eu\ncd "$(dirname "$0")/.."\n'
                'for m in %s; do\n'
                '  docker run --rm -v "$AP_TEST_REPORT_DIR:/reports" -v "$PWD/src:/src" -w /src img \\\n'
                '    sh -ec "cd $m && $FAKE_GOTESTSUM --junitfile /reports/go-$m.xml -- ./..."\n'
                'done\n' % " ".join(GO_MODULES))
        repo = Repo(self, text)
        repo.reports.mkdir()
        r = subprocess.run([str(repo.repo / "scripts" / "check.sh")], cwd=repo.repo,
                           env=dict(repo.env, AP_TEST_REPORT_DIR=str(repo.reports)),
                           capture_output=True, text=True, timeout=60)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(sorted(p.name for p in repo.reports.glob("*.xml")), sorted(WANT["go"]))


class NoReportsWithoutDirTest(unittest.TestCase):
    def test_real_go_step(self):
        self.assertEqual(judge_no_reports_without_dir(self, "go"), [])

    def test_real_docker_step(self):
        self.assertEqual(judge_no_reports_without_dir(self, "docker"), [])

    def test_real_py_tests_step(self):
        self.assertEqual(judge_no_reports_without_dir(self, "py-tests"), [])

    def test_planted_default_folder_fails(self):
        for step in ("go", "docker", "py-tests"):
            with self.subTest(step=step):
                self.assertTrue(judge_no_reports_without_dir(self, step,
                                                             default_dir(CHECK.read_text())))


# ---------------------------------------------------------------- images.lock

GO_TOOLS = "# Go tools: pinned by version"


def judge_gotestsum_pin(text):
    lines = text.splitlines()
    pins = [(i, l) for i, l in enumerate(lines) if l.split()[:1] == ["gotestsum"]]
    if len(pins) != 1:
        return ["images.lock has %d gotestsum pins, not one" % len(pins)]
    i, line = pins[0]
    v = []
    if not re.fullmatch(r"gotestsum\s+gotest\.tools/gotestsum@v\d+\.\d+\.\d+", line.strip()):
        v.append("gotestsum pin %r is not gotest.tools/gotestsum@v<semver>" % line.strip())
    head = [j for j, l in enumerate(lines) if l.startswith(GO_TOOLS)]
    if not head or i < head[0]:
        v.append("the gotestsum pin is not under the %r comment" % GO_TOOLS)
    else:
        between = lines[head[0] + 1:i]
        if any(l.startswith("#") and not l.startswith("# Node tools") and
               not l.startswith("# with integrity") for l in between):
            v.append("another comment block separates the gotestsum pin from %r" % GO_TOOLS)
    return v


class GotestsumPinTest(unittest.TestCase):
    def test_real_lock(self):
        self.assertEqual(judge_gotestsum_pin(LOCK.read_text()), [])

    GOOD = ("golang x\n# Go tools: pinned by version; the Go checksum database verifies each.\n"
            "oapi-codegen github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2\n"
            "gotestsum                  gotest.tools/gotestsum@v1.13.0\n"
            "# The CI runners\nci-runner ubuntu-26.04\n")

    def test_planted_good_passes(self):
        self.assertEqual(judge_gotestsum_pin(self.GOOD), [])

    def test_planted_missing_fails(self):
        self.assertTrue(judge_gotestsum_pin(self.GOOD.replace(
            "gotestsum                  gotest.tools/gotestsum@v1.13.0\n", "")))

    def test_planted_unpinned_fails(self):
        for ref in ("gotest.tools/gotestsum@latest", "gotest.tools/gotestsum",
                    "gotest.tools/gotestsum@v1.13", "github.com/x/gotestsum@v1.13.0"):
            with self.subTest(ref=ref):
                self.assertTrue(judge_gotestsum_pin(self.GOOD.replace(
                    "gotest.tools/gotestsum@v1.13.0", ref)))

    def test_planted_outside_the_go_tools_fails(self):
        moved = self.GOOD.replace("gotestsum                  gotest.tools/gotestsum@v1.13.0\n", "")
        moved += "gotestsum gotest.tools/gotestsum@v1.13.0\n"
        self.assertTrue(judge_gotestsum_pin(moved))


# ---------------------------------------------------------------- check.yml

REPORTED_JOBS = ("go", "docker", "py-tests")
ACTION = "mikepenz/action-junit-report@"
ENV = "AP_TEST_REPORT_DIR"


def truthy(x, want):
    return str(x).strip().lower() == ("true" if want else "false")


def judge_junit_reports(root):
    wf = wfx.load(root)
    v = wfx.need_files(wf, "check.yml")
    if v:
        return v
    for name in REPORTED_JOBS:
        j = wfx.by_name(wf["check.yml"], name)
        if j is None:
            v.append("check.yml has no %s job" % name)
            continue
        steps = j.get("steps") or []
        ci = wfx.find_step(j, r"check\.sh\s+--only\s+(?:[\w-]+,)*" + re.escape(name) + r"(?![\w-])")
        if ci < 0:
            v.append("%s job runs no scripts/check.sh --only %s" % (name, name))
            continue
        d = (steps[ci].get("env") or {}).get(ENV) or (j.get("env") or {}).get(ENV)
        if not d:
            v.append("%s job does not set %s for its check step" % (name, ENV))
        rep = [(i, s) for i, s in enumerate(steps) if str(s.get("uses", "")).startswith(ACTION)]
        if not rep:
            v.append("%s job has no %s step" % (name, ACTION.rstrip("@")))
            continue
        for i, s in rep:
            w = s.get("with") or {}
            if i < ci:
                v.append("%s job reports before its check step" % name)
            if not re.fullmatch(re.escape(ACTION) + r"[0-9a-f]{40}", str(s["uses"])):
                v.append("%s job: %s is not pinned by a 40-hex commit hash" % (name, s["uses"]))
            if not wfx.is_always(s.get("if")):
                v.append("%s job: the report step has no if: always()" % name)
            if not truthy(w.get("annotate_only"), True):
                v.append("%s job: the report step has no annotate_only: true" % name)
            if not truthy(w.get("detailed_summary"), True):
                v.append("%s job: the report step has no detailed_summary: true" % name)
            if not truthy(w.get("include_passed"), False):
                v.append("%s job: the report step has no include_passed: false" % name)
            paths = wfx.norm(w.get("report_paths", ""))
            under = d and paths and (paths.startswith(wfx.norm(d) + "/") or re.match(
                r"\$\{\{env\." + ENV + r"\}\}/", paths) or paths.startswith("$" + ENV + "/"))
            if not under:
                v.append("%s job: report_paths %r is not under %s (%r)"
                         % (name, w.get("report_paths"), ENV, d))
    return v


REPORT_DIR = "${{ runner.temp }}/test-reports"


def report_step():
    return {"name": "Test report", "if": "always()", "uses": ACTION + "b" * 40,
            "with": {"report_paths": REPORT_DIR + "/*.xml", "annotate_only": True,
                     "detailed_summary": True, "include_passed": False}}


def with_reports(d):
    """test_workflows.py's good tree, with a go job and the report steps T111 adds."""
    js = d["check.yml"]["jobs"]
    summary = js.pop("summary")   # the summary job stays the last job
    js["go"] = wfx.good_job("scripts/check.sh --only go")
    summary["needs"].append("go")
    js["summary"] = summary
    for name in REPORTED_JOBS:
        j = js[name]
        ci = wfx.find_step(j, r"check\.sh\s+--only\s+" + re.escape(name) + r"\b")
        j["steps"][ci]["env"] = {ENV: REPORT_DIR}
        j["steps"].append(report_step())


def planted(tc, mutate=None):
    def m(d):
        with_reports(d)
        if mutate:
            mutate(d)
    return wfx.planted(tc, m)


def job(d, name):
    return d["check.yml"]["jobs"][name]


def reporter(d, name):
    return [s for s in job(d, name)["steps"] if str(s.get("uses", "")).startswith(ACTION)][0]


class JunitReportsTest(unittest.TestCase):
    def test_real_check_yml(self):
        self.assertEqual(judge_junit_reports(REPO), [])

    def test_planted_good_tree_passes_and_keeps_every_workflow_rule(self):
        root = planted(self)
        self.assertEqual(judge_junit_reports(root), [])
        for fn in (wfx.judge_triggers, wfx.judge_permissions, wfx.judge_pinned_uses,
                   wfx.judge_summary_job, *wfx.NEW_RULES):
            self.assertEqual(fn(root), [], fn.__name__)

    def test_job_level_env_passes(self):
        def m(d):
            j = job(d, "go")
            j["env"] = {ENV: REPORT_DIR}
            for s in j["steps"]:
                s.pop("env", None)
        self.assertEqual(judge_junit_reports(planted(self, m)), [])

    def test_planted_no_report_dir_fails(self):
        for name in REPORTED_JOBS:
            with self.subTest(job=name):
                def m(d):
                    for s in job(d, name)["steps"]:
                        s.pop("env", None)
                self.assertTrue(judge_junit_reports(planted(self, m)))

    def test_planted_no_report_step_fails(self):
        for name in REPORTED_JOBS:
            with self.subTest(job=name):
                def m(d):
                    job(d, name)["steps"].remove(reporter(d, name))
                self.assertTrue(judge_junit_reports(planted(self, m)))

    def test_planted_report_before_the_check_fails(self):
        def m(d):
            st = job(d, "docker")["steps"]
            st.insert(1, st.pop())
        self.assertTrue(judge_junit_reports(planted(self, m)))

    def test_planted_unpinned_action_fails(self):
        def m(d):
            reporter(d, "py-tests")["uses"] = ACTION + "v6.7.1"
        self.assertTrue(judge_junit_reports(planted(self, m)))

    def test_planted_without_always_fails(self):
        for cond in (None, "success()", "failure()"):
            with self.subTest(cond=cond):
                def m(d):
                    s = reporter(d, "go")
                    if cond is None:
                        s.pop("if")
                    else:
                        s["if"] = cond
                self.assertTrue(judge_junit_reports(planted(self, m)))

    def test_planted_settings_fail(self):
        for key, bad in (("annotate_only", False), ("annotate_only", None),
                         ("detailed_summary", False), ("include_passed", True)):
            with self.subTest(key=key, value=bad):
                def m(d):
                    w = reporter(d, "docker")["with"]
                    if bad is None:
                        w.pop(key)
                    else:
                        w[key] = bad
                self.assertTrue(judge_junit_reports(planted(self, m)))

    def test_planted_report_paths_elsewhere_fails(self):
        for paths in ("reports/*.xml", "${{ runner.temp }}/other/*.xml", ""):
            with self.subTest(paths=paths):
                def m(d):
                    reporter(d, "go")["with"]["report_paths"] = paths
                self.assertTrue(judge_junit_reports(planted(self, m)))

    def test_env_expression_paths_pass(self):
        def m(d):
            reporter(d, "go")["with"]["report_paths"] = "${{ env.AP_TEST_REPORT_DIR }}/*.xml"
            job(d, "go")["env"] = {ENV: REPORT_DIR}
        self.assertEqual(judge_junit_reports(planted(self, m)), [])


if __name__ == "__main__":
    unittest.main()
