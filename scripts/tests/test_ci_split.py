"""T067(b), ADR 0074: a push's check run runs every test of the Docker suites exactly once.

Judged from the goal, not from one implementation. The judge reads `.github/workflows/check.yml`,
finds the jobs that run Docker-suite tests, learns what each selects by RUNNING the real command
against stubs, and compares that with the tests in the Go sources.

Jobs read (a job step is a Docker-suite step when it matches one of these; anything else in it is
refused with a message, never guessed):
  * `scripts/check.sh --only <steps including docker> [args]`, optionally behind
    `scripts/ci-step.sh <label>`; the step's `run` must be ONE command line (backslash
    continuations allowed), optionally led by `NAME=value` words. Env comes from the job's and the
    step's `env:` and those words; args are passed to the real scripts/check.sh as written.
  * `src/deploy/ap test <suite> [<scenario>]`, in the same single-line form.
  * `${{ matrix.<key> }}` is expanded from `strategy.matrix` (lists of strings per key, or only
    `include:` entries; `exclude:` and computed matrices are refused); `${{ runner.temp }}` is a
    temporary folder; any other `${{ }}` in a Docker-suite step's command or env is refused.
What a step selects is what the Go test binary receives: the real command runs with a stub
`docker` that builds stub test binaries logging their argv (as test_ap_test.py and
test_check_check_e2e_skip.py stub things), and the judge reads `-test.run` / `-test.skip` (also
`--test.run=`, `-test.run=`) from it. So the selection may be passed to the binary by any means
the code author chooses (an env var or option of check.sh, ap's arguments). A selection that is
not `-test.run` / `-test.skip` on the binary's command line (for instance a variable the Go tests
read) is not accepted: its job would select everything and fail "exactly once". Two flags of one
kind, or -test.list/-test.bench/-test.fuzz, are refused. Go's matching is modelled: patterns split
at top-level `/`, each part an unanchored regex of its level, a parent matches a longer pattern
only partly, a skip skips a test only when it covers the test's whole name.
Stop points and scenarios are read from a job as `-test.run ^TestStopPoints$/^<name>$` and
`^TestPlanted$/^<name>$` (what `ap test planted <name>` runs); other second-level forms refused.

The judge is itself proved on an in-memory fixture (a clean one passes; each planted variant fails
with its message); the same judge then runs on the real files. Real-file tests that plant a change
compare with the real baseline's violations, so only the new violation counts.
"""
import copy
import itertools
import json
import os
import re
import shlex
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
WORKFLOW = REPO / ".github/workflows/check.yml"
DECLARATION = REPO / ".github/check-jobs.yaml"
CHECK_SH = REPO / "scripts/check.sh"
AP = REPO / "src/deploy/ap"
PKGS = ("acceptance", "recorded", "lint", "contract")
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
RUNNER_TEMP = "@RUNNER_TEMP@"


class Unreadable(Exception):
    """A selection or form this judge does not accept."""


# ---------------------------------------------------------------- Go sources

def lex(text):
    """Go source with comments blanked and every string/rune literal replaced by a placeholder
    "S<i>" (values in the second result), so braces, names and `func` in them or in comments
    never count."""
    out, strs, i, n = [], [], 0, len(text)
    while i < n:
        c = text[i]
        if text.startswith("//", i):
            j = text.find("\n", i)
            i = n if j < 0 else j
        elif text.startswith("/*", i):
            j = text.find("*/", i + 2)
            out.append(" ")
            i = n if j < 0 else j + 2
        elif c == '"':
            j = i + 1
            while j < n and text[j] != '"':
                j += 2 if text[j] == "\\" else 1
            strs.append(text[i + 1:j])
            out.append('"S%d"' % (len(strs) - 1))
            i = j + 1
        elif c == "`":
            j = text.find("`", i + 1)
            j = n if j < 0 else j
            strs.append(text[i + 1:j])
            out.append('"S%d"' % (len(strs) - 1))
            i = j + 1
        elif c == "'":
            j = i + 1
            while j < n and text[j] != "'":
                j += 2 if text[j] == "\\" else 1
            out.append("'r'")
            i = j + 1
        else:
            out.append(c)
            i += 1
    return "".join(out), strs


def go_test_funcs(code):
    return re.findall(r"^func\s+(Test(?![a-z])\w*)\s*\(\s*\w+\s+\*testing\.T\s*\)\s*\{", code, re.M)


def list_names(code, strs, var):
    """The `name:` of each element of `var X = []T{ {...}, ... }`, read at the element's own level
    (nested braces, comments and strings never count). None if the list is not found."""
    m = re.search(r"\bvar\s+%s\s*=\s*\[\]\s*\*?\w+\s*\{" % re.escape(var), code)
    if not m:
        return None
    i, depth, cur, entries = m.end(), 0, None, []
    while i < len(code):
        c = code[i]
        if c == "{":
            if depth == 0:
                cur = []
            depth += 1
        elif c == "}":
            if depth == 0:
                break
            depth -= 1
            if depth == 0:
                entries.append("".join(cur))
                cur = None
        elif depth == 1:
            cur.append(c)
        i += 1
    names = []
    for e in entries:
        found = re.findall(r'(?<![\w.])name\s*:\s*"S(\d+)"', e)
        if len(found) != 1:
            raise Unreadable("an element of %s has %d `name:` values, not one: %r" % (var, len(found), e.strip()[:60]))
        names.append(strs[int(found[0])])
    return names


def load_sources(root=REPO):
    out = {}
    for p in PKGS:
        d = Path(root) / "src/tests" / p
        out[p] = [f.read_text() for f in sorted(d.glob("*_test.go"))]
    return out


def go_model(sources):
    """(units, stop_names, planted_names, errors); a unit is (pkg, path elements)."""
    units, errors, lists = [], [], {}
    lexed = {pkg: [lex(t) for t in sources.get(pkg, [])] for pkg in PKGS}
    for pkg in PKGS:
        for code, _ in lexed[pkg]:
            for f in go_test_funcs(code):
                special = {"TestStopPoints": "stopPoints", "TestPlanted": "plantedScenarios"}.get(f) if pkg == "acceptance" else None
                if not special:
                    units.append((pkg, (f,)))
                    continue
                names = None
                for c2, s2 in lexed[pkg]:
                    try:
                        got = list_names(c2, s2, special)
                    except Unreadable as e:
                        errors.append(str(e))
                        continue
                    if got is not None:
                        names = got
                if not names:
                    errors.append("cannot read %s from the Go sources of acceptance" % special)
                    continue
                if len(set(names)) != len(names):
                    errors.append("%s lists a name twice" % special)
                lists[f] = names
                units += [(pkg, (f, n)) for n in names]
    return units, lists.get("TestStopPoints", []), lists.get("TestPlanted", []), errors


# ---------------------------------------------------------------- Go test selection

def split_pattern(p):
    parts, cur, depth, i = [], "", 0, 0
    while i < len(p):
        c = p[i]
        if c == "\\" and i + 1 < len(p):
            cur += p[i:i + 2]
            i += 2
            continue
        if c in "[(":
            depth += 1
        elif c in "])":
            depth = max(depth - 1, 0)
        if c == "/" and depth == 0:
            parts.append(cur)
            cur = ""
        else:
            cur += c
        i += 1
    parts.append(cur)
    return parts


def go_match(elems, path):
    """Go's simpleMatch.matches: (ok, partial)."""
    for i, s in enumerate(path):
        if i >= len(elems):
            break
        if not re.search(elems[i], s):
            return False, False
    return True, len(path) < len(elems)


class Selection:
    def __init__(self, run="", skip=""):
        self.run_text, self.skip_text = run, skip
        self.run = split_pattern(run) if run else []
        self.skip = split_pattern(skip) if skip else []

    def status(self, path):
        partial = False
        if self.run:
            ok, partial = go_match(self.run, path)
            if not ok:
                return "no"
        if self.skip:
            ok, p = go_match(self.skip, path)
            if ok and not p:
                return "no"
        return "partial" if partial else "run"


def selection_from_argv(argv):
    vals = {"run": [], "skip": []}
    i = 0
    while i < len(argv):
        a = argv[i]
        m = re.fullmatch(r"--?test\.(\w+)(=(.*))?", a, re.S)
        if m:
            name = m.group(1)
            if name in ("list", "bench", "fuzz"):
                raise Unreadable("the test binary gets -test.%s, which changes what runs" % name)
            if name in vals:
                if m.group(2) is not None:
                    v = m.group(3)
                elif i + 1 < len(argv):
                    v = argv[i + 1]
                    i += 1
                else:
                    raise Unreadable("-test.%s has no value" % name)
                vals[name].append(v)
        i += 1
    for k, v in vals.items():
        if len(v) > 1:
            raise Unreadable("the test binary gets -test.%s %d times" % (k, len(v)))
    return Selection(vals["run"][0] if vals["run"] else "", vals["skip"][0] if vals["skip"] else "")


# ---------------------------------------------------------------- the workflow

class Inst:
    def __init__(self, label, jid, kind, args, runs):
        self.label, self.jid, self.kind, self.args, self.runs = label, jid, kind, args, runs


def expand_matrix(jid, job):
    m = (job.get("strategy") or {}).get("matrix")
    if not m:
        return [{}]
    if not isinstance(m, dict) or "exclude" in m:
        raise Unreadable("job %s: a matrix with exclude or a computed matrix" % jid)
    include = m.get("include") or []
    keys = {k: v for k, v in m.items() if k != "include"}
    for k, v in keys.items():
        if not isinstance(v, list) or not all(isinstance(x, (str, int)) for x in v):
            raise Unreadable("job %s: matrix.%s is not a list of plain values" % (jid, k))
    if keys and include:
        raise Unreadable("job %s: a matrix with both lists and include" % jid)
    if keys:
        return [dict(zip(keys, c, strict=True)) for c in itertools.product(*keys.values())]
    return [dict(x) for x in include] or [{}]


def substitute(text, matrix, where):
    def rep(m):
        k = m.group(1)
        if k not in matrix:
            raise Unreadable("%s: matrix.%s is not defined" % (where, k))
        return str(matrix[k])
    text = re.sub(r"\$\{\{\s*matrix\.([\w-]+)\s*\}\}", rep, text)
    text = re.sub(r"\$\{\{\s*runner\.temp\s*\}\}", RUNNER_TEMP, text)
    if "${{" in text:
        raise Unreadable("%s: an expression this judge cannot evaluate: %s" % (where, text.strip()[:80]))
    return text


def suite_kind(run):
    if re.search(r"(^|\s)scripts/check\.sh\s+--only\s+\S*\bdocker\b", run):
        return "check"
    if re.search(r"(^|\s)src/deploy/ap\s+test\b", run):
        return "ap"
    return None


def parse_step(run, env, where):
    text = run.replace("\\\n", " ")
    lines = [x for x in text.splitlines() if x.strip() and not x.strip().startswith("#")]
    if len(lines) != 1:
        raise Unreadable("%s: the run is %d lines; a Docker-suite step must be one command line" % (where, len(lines)))
    try:
        toks = shlex.split(lines[0])
    except ValueError as e:
        raise Unreadable("%s: cannot split %r: %s" % (where, lines[0], e)) from e
    while toks and re.fullmatch(r"[A-Za-z_]\w*=.*", toks[0]):
        k, v = toks.pop(0).split("=", 1)
        env[k] = v
    if toks[:1] == ["scripts/ci-step.sh"]:
        toks = toks[2:]
    if not toks or toks[0] not in ("scripts/check.sh", "src/deploy/ap"):
        raise Unreadable("%s: the command %r is not scripts/check.sh or src/deploy/ap" % (where, lines[0]))
    return toks[0], toks[1:]


def build_instances(wf, resolver):
    insts, errors = [], []
    for jid, job in (wf.get("jobs") or {}).items():
        steps = [s for s in (job.get("steps") or []) if isinstance(s.get("run"), str) and suite_kind(s["run"])]
        if not steps:
            continue
        try:
            combos = expand_matrix(jid, job)
        except Unreadable as e:
            errors.append(str(e))
            continue
        for matrix in combos:
            label = jid + ("[%s]" % ",".join("%s=%s" % kv for kv in matrix.items()) if matrix else "")
            for step in steps:
                try:
                    env = {}
                    for src in (job.get("env") or {}, step.get("env") or {}):
                        for k, v in src.items():
                            env[k] = substitute(str(v), matrix, "%s env %s" % (label, k))
                    run = substitute(step["run"], matrix, label)
                    cmd, args = parse_step(run, env, label)
                    kind = "check" if cmd == "scripts/check.sh" else "ap"
                    runs = resolver(kind, args, env)
                except Unreadable as e:
                    errors.append("%s: %s" % (label, e))
                    continue
                insts.append(Inst(label, jid, kind, args, runs))
    return insts, errors


def named(inst, func):
    sel = inst.runs.get("acceptance")
    if sel is None or not sel.run or sel.run[0] != "^%s$" % func:
        return None, None
    if len(sel.run) == 1:
        return None, None
    # `^<name>$`, or `^(uninterrupted|<name>)$`: each stop-point trial needs the `uninterrupted`
    # baseline subtest, a setup that may run in every stop-point job and is no unit (ADR 0074).
    m = re.fullmatch(r"\^([a-z0-9-]+)\$", sel.run[1]) if len(sel.run) == 2 else None
    if m is None and func == "TestStopPoints" and len(sel.run) == 2:
        m = re.fullmatch(r"\^\(uninterrupted\|([a-z0-9-]+)\)\$", sel.run[1])
    if m is None:
        return None, "%s: cannot read a name from -test.run %r" % (inst.label, sel.run_text)
    return m.group(1), None


def judge(model, insts, errors):
    units, stop, planted, gerrors = model
    v = list(errors) + list(gerrors)
    used = {i.label: 0 for i in insts}
    for pkg, path in units:
        who, part = [], []
        for inst in insts:
            sel = inst.runs.get(pkg)
            if sel is None:
                continue
            s = sel.status(path)
            if s == "run":
                who.append(inst)
                used[inst.label] += 1
            elif s == "partial":
                part.append(inst.label)
        name = "%s.%s" % (pkg, "/".join(path))
        if part:
            v.append("%s is only partly selected by %s" % (name, ", ".join(part)))
        if not who:
            v.append("no job runs %s" % name)
        elif len(who) > 1:
            v.append("%s runs in %d jobs: %s" % (name, len(who), ", ".join(i.label for i in who)))
        if pkg == "acceptance" and path[0].startswith("TestUS2"):
            for i in who:
                if not (i.kind == "ap" and i.args[:2] == ["test", "us2"]):
                    v.append("%s runs in job %s, not in `ap test us2`" % (name, i.label))
    for label, n in used.items():
        if n == 0:
            v.append("job %s selects no test of the Docker suites" % label)
    for func, want, what in (("TestStopPoints", stop, "stop points"), ("TestPlanted", planted, "planted scenarios")):
        got = []
        for inst in insts:
            n, err = named(inst, func)
            if err:
                v.append(err)
            if n:
                got.append(n)
        if want or got:
            if set(got) != set(want) or len(got) != len(set(got)):
                v.append("the workflow's %s %s differ from the Go sources' %s: missing %s, extra %s, repeated %s" % (
                    what, sorted(got), sorted(want), sorted(set(want) - set(got)), sorted(set(got) - set(want)),
                    sorted({x for x in got if got.count(x) > 1})))
    return v


def judge_guards(wf, decl, suite_jids):
    """Each Docker-suite job is declared in check-jobs.yaml, carries the selector `if:` iff declared
    by paths, is selected together with the other Docker-suite jobs, and is in summary's needs."""
    v, sigs = [], {}
    jobs = (decl or {}).get("jobs") or {}
    needs = (wf["jobs"].get("summary") or {}).get("needs") or []
    for jid in sorted(suite_jids):
        d = jobs.get(jid)
        job = wf["jobs"][jid]
        if d is None:
            v.append("job %s is not declared in .github/check-jobs.yaml" % jid)
        else:
            sigs[jid] = json.dumps(d, sort_keys=True)
            want = None if d.get("always") else "contains(fromJSON(needs.config.outputs.jobs), '%s')" % jid
            have = job.get("if")
            if (have is None and want) or (have is not None and " ".join(str(have).split()) != want):
                v.append("job %s: if: is %r, want %r" % (jid, have, want))
        if jid not in needs:
            v.append("job %s is not in summary's needs" % jid)
    if len(set(sigs.values())) > 1:
        v.append("Docker-suite jobs are not selected together: %s" % json.dumps(sigs, sort_keys=True))
    return v


# ---------------------------------------------------------------- stubs for the real commands

FAKE_DOCKER = r'''#!/usr/bin/env python3
import json, os, re, sys
argv = sys.argv[1:]
if argv[:1] == ["version"]:
    print("29.9.3"); sys.exit(0)
if argv[:1] != ["run"]:
    sys.exit(0)
joined = " ".join(argv)
m = re.search(r"go test -c\b.*?-o (/gopath/\S+)", joined)
if m:
    mount = None
    for i, a in enumerate(argv):
        if a in ("-v", "--volume") and i + 1 < len(argv):
            p = argv[i + 1].split(":")
            if len(p) >= 2 and p[1] == "/gopath":
                mount = p[0]
    if mount is None:
        sys.stderr.write("fake docker: no /gopath mount for the build\n"); sys.exit(1)
    host = os.path.join(mount, m.group(1)[len("/gopath/"):])
    os.makedirs(os.path.dirname(host), exist_ok=True)
    with open(host, "w") as f:
        f.write("#!/usr/bin/env python3\n" + (
            "import json, os, sys\n"
            "with open(os.environ['FAKE_BIN_LOG'], 'a') as f:\n"
            "    f.write(json.dumps({'bin': os.path.basename(sys.argv[0]), 'argv': sys.argv[1:], 'cwd': os.getcwd()}) + '\\n')\n"
            "sys.exit(0)\n"))
    os.chmod(host, 0o755)
    sys.exit(0)
if "test2json" in joined:
    sys.stdin.buffer.read()
    p = os.environ.get("FAKE_JSON")
    if p:
        sys.stdout.write(open(p).read())
    sys.exit(0)
sys.exit(0)
'''

STUB_STEPS = ('for _s in $(echo "$STEP_TABLE" | cut -d" " -f1); do\n'
              '  [ "$_s" = docker ] || eval "s_$(echo "$_s" | tr - _)() { :; }"\n'
              'done\n')


class Stub:
    """A throwaway copy of the repository's scripts, a stub docker, and a log of stub test binaries."""

    def __init__(self, base, check_text=None):
        self.base = Path(base)
        self.repo = self.base / "repo"
        shutil.copytree(REPO / "scripts", self.repo / "scripts",
                        ignore=shutil.ignore_patterns("tests", "__pycache__", "node_modules"))
        (self.repo / "src/deploy").mkdir(parents=True)
        shutil.copy(REPO / "src/deploy/images.lock", self.repo / "src/deploy/images.lock")
        for p in PKGS:
            (self.repo / "src/tests" / p).mkdir(parents=True)
        text = check_text if check_text is not None else CHECK_SH.read_text()
        marker = "\nstart=$(now)\n"
        if marker not in text:
            raise Unreadable("scripts/check.sh has no 'start=$(now)' line to stub the other steps before")
        (self.repo / "scripts/check.sh").write_text(text.replace(marker, "\n" + STUB_STEPS + marker.lstrip("\n"), 1))
        os.chmod(self.repo / "scripts/check.sh", 0o700)
        self.bin = self.base / "bin"
        self.bin.mkdir()
        (self.bin / "docker").write_text(FAKE_DOCKER)
        os.chmod(self.bin / "docker", 0o700)
        for d in ("home", "cache", "tmp", "runner-temp"):
            (self.base / d).mkdir()
        self.log = self.base / "bin.log"

    def env(self, extra):
        e = {k: v for k, v in os.environ.items() if not k.startswith(("AP_", "FAKE_", "XDG_"))}
        e.update(HOME=str(self.base / "home"), XDG_CACHE_HOME=str(self.base / "cache"), TMPDIR=str(self.base / "tmp"),
                 XDG_STATE_HOME=str(self.base / "state"), XDG_CONFIG_HOME=str(self.base / "config"),
                 FAKE_BIN_LOG=str(self.log), PATH=str(self.bin) + os.pathsep + e.get("PATH", ""))
        e.update({k: v.replace(RUNNER_TEMP, str(self.base / "runner-temp")) for k, v in extra.items()})
        return e

    def binary_runs(self):
        if not self.log.exists():
            return []
        return [json.loads(x) for x in self.log.read_text().splitlines() if x]

    def run(self, cmd, args, env, json_events=""):
        self.log.unlink(missing_ok=True)
        e = self.env(env)
        if json_events is not None:
            jf = self.base / "events.json"
            jf.write_text(json_events)
            e["FAKE_JSON"] = str(jf)
        exe = [str(self.repo / "scripts/check.sh")] if cmd == "check" else ["sh", str(AP)]
        if cmd == "ap" and not args[:1] == ["test"]:
            args = ["test"] + args
        r = subprocess.run(exe + list(args), cwd=self.repo, env=e, capture_output=True, text=True, timeout=90,
                           stdin=subprocess.DEVNULL)
        return r, self.binary_runs()


def planted_events(scenarios, passing=True):
    ev = [{"Action": "start"}, {"Action": "run", "Test": "TestPlanted"}]
    for sc in scenarios:
        ev.append({"Action": "run", "Test": "TestPlanted/%s" % sc})
        for n in (1, 2, 3):
            t = "TestPlanted/%s/trial-%d" % (sc, n)
            ev += [{"Action": "run", "Test": t}, {"Action": "pass" if passing else "fail", "Test": t}]
        ev.append({"Action": "pass" if passing else "fail", "Test": "TestPlanted/%s" % sc})
    ev.append({"Action": "pass", "Test": "TestPlanted"})
    return "".join(json.dumps(e, separators=(",", ":")) + "\n" for e in ev)


class RealResolver:
    """Runs the real scripts/check.sh or src/deploy/ap against stubs; memoised."""
    memo = {}

    def __init__(self, check_text=None):
        self.check_text = check_text
        self.base = tempfile.mkdtemp(prefix="test-ci-split-", dir=TMP_BASE)
        self.n = 0

    def close(self):
        shutil.rmtree(self.base, ignore_errors=True)

    def __call__(self, kind, args, env):
        key = (self.check_text, kind, tuple(args), tuple(sorted(env.items())))
        if key in RealResolver.memo:
            return RealResolver.memo[key]
        self.n += 1
        stub = Stub(Path(self.base) / ("s%d" % self.n), None) if False else None
        d = Path(self.base) / ("s%d" % self.n)
        d.mkdir()
        stub = Stub(d, self.check_text)
        r, runs = stub.run(kind, args, env, json_events="")
        if r.returncode not in (0, 1) and not runs:
            raise Unreadable("`%s %s` exited %d without running a test binary: %s" % (
                "scripts/check.sh" if kind == "check" else "ap", " ".join(args), r.returncode, (r.stderr or r.stdout).strip()[-200:]))
        if not runs:
            raise Unreadable("`%s %s` ran no test binary: %s" % (kind, " ".join(args), (r.stderr or r.stdout).strip()[-200:]))
        out = {}
        for x in runs:
            pkg = Path(x["cwd"]).name
            if pkg in out:
                raise Unreadable("`%s %s` ran the %s binary twice" % (kind, " ".join(args), pkg))
            out[pkg] = selection_from_argv(x["argv"])
        # With AP_ACCEPTANCE_RUN set, check.sh runs only the acceptance binary (ADR 0074).
        want = {"acceptance"} if kind == "check" and env.get("AP_ACCEPTANCE_RUN") else set(PKGS)
        if kind == "check" and set(out) != want:
            raise Unreadable("`check.sh %s` ran the binaries of %s, not %s" % (" ".join(args), sorted(out), sorted(want)))
        RealResolver.memo[key] = out
        return out


def fixture_resolver(kind, args, env):
    """A pure model of the commands for the planted-variant fixtures: `check.sh --only docker
    [--run R] [--skip S]`; `ap test planted [<scenario>]`, `ap test us2` as the contract says."""
    if kind == "check":
        run = skip = ""
        a = list(args)
        if a[:2] != ["--only", "docker"]:
            raise Unreadable("fixture: check.sh args %r" % a)
        a = a[2:]
        while a:
            if a[0] == "--run" and len(a) > 1:
                run = a[1]
                a = a[2:]
            elif a[0] == "--skip" and len(a) > 1:
                skip = a[1]
                a = a[2:]
            else:
                raise Unreadable("fixture: unknown check.sh argument %r" % a[0])
        return {p: Selection(run, skip) for p in PKGS}
    suite = args[1]
    scenario = args[2] if len(args) > 2 else None
    if suite == "planted":
        return {"acceptance": Selection("^TestPlanted$" + ("/^%s$" % scenario if scenario else ""))}
    if suite == "us2" and not scenario:
        return {"acceptance": Selection("^TestUS2")}
    raise Unreadable("fixture: ap args %r" % args)


# ---------------------------------------------------------------- fixture

FIXTURE_GO = {
    "acceptance": ['''//go:build docker
package acceptance

// name: "commented-out" is not a scenario
var stopPoints = []stopPoint{
	{
		name: "agent-step", // name: "nope"
		reach: func(t *testing.T) { _ = map[string]int{"name": 1} },
	},
	/* {name: "block-comment"}, */
	{
		note: "name: \\"in-a-string\\"",
		name:   "review-round",
	},
}

var plantedScenarios = []scenario{
	{name: "outside", step: "code"},
	{name: "outside-scope", spec: "multi
line"},
	{name: "claims-done"},
}

func TestUS1(t *testing.T) {}
func TestUS2(t *testing.T) {}
func TestUS2_10_AnInterruptedRunResumes(t *testing.T) {}
func TestOperationsSurviveRestart(t *testing.T) {}
func TestStopPoints(t *testing.T) {}
func TestPlanted(t *testing.T) {}
func TestMain(m *testing.M) {}
func Testhelper(t *testing.T) {}
// func TestCommentedOut(t *testing.T) {}
'''],
    "recorded": ["func TestMatchingRequestGetsRecordedAnswer(t *testing.T) {}\n"],
    "lint": ["func TestHealthIsOpen(t *testing.T) {}\n"],
    "contract": ["func TestRunStatusAndOutcomeRule(t *testing.T) {}\n"],
}
DOCKER_SKIP = "^TestStopPoints$|^TestPlanted$|^TestUS2"


def fixture_workflow():
    def ci(label, cmd):
        return {"runs-on": "x", "needs": ["config"], "if": "contains(fromJSON(needs.config.outputs.jobs), '%s')" % label,
                "steps": [{"run": "scripts/ci-step.sh %s %s" % (label, cmd)}]}
    jobs = {"config": {"runs-on": "x"},
            "docker": ci("docker", "scripts/check.sh --only docker --skip '%s'" % DOCKER_SKIP),
            "stop": ci("stop", "scripts/check.sh --only docker --run '^TestStopPoints$/^${{ matrix.sp }}$'"),
            "planted_outside": ci("planted_outside", "src/deploy/ap test planted outside"),
            "planted_outside_scope": ci("planted_outside_scope", "src/deploy/ap test planted outside-scope"),
            "planted_claims_done": ci("planted_claims_done", "src/deploy/ap test planted claims-done"),
            "us2": ci("us2", "src/deploy/ap test us2")}
    jobs["stop"]["strategy"] = {"matrix": {"sp": ["agent-step", "review-round"]}}
    jobs["summary"] = {"if": "always()", "needs": [j for j in jobs if j != "summary"], "runs-on": "x"}
    decl = {"jobs": {j: {"paths": ["**"]} for j in jobs if j not in ("config", "summary")}}
    return {"jobs": jobs}, decl


def judge_fixture(sources=None, wf=None, decl=None):
    sources = sources or FIXTURE_GO
    if wf is None:
        wf, d = fixture_workflow()
        decl = decl or d
    model = go_model(sources)
    insts, errors = build_instances(wf, fixture_resolver)
    return judge(model, insts, errors) + judge_guards(wf, decl, {i.jid for i in insts})


def with_acceptance(extra=None, replace=None):
    s = copy.deepcopy(FIXTURE_GO)
    t = s["acceptance"][0]
    if replace:
        assert replace[0] in t, replace
        t = t.replace(*replace)
    s["acceptance"] = [t + (extra or "")]
    return s


def messages(v, *needles):
    return [m for m in v if all(n in m for n in needles)]


# ---------------------------------------------------------------- the judge proves it can fail

class JudgeFixtureTest(unittest.TestCase):
    def test_clean_fixture_passes(self):
        self.assertEqual(judge_fixture(), [])

    def test_fixture_reads_the_go_lists_robustly(self):
        # case 9: comments, a name in a comment or in a string, a nested closure, multi-line entries
        units, stop, planted, errors = go_model(FIXTURE_GO)
        self.assertEqual(errors, [])
        self.assertEqual(stop, ["agent-step", "review-round"])
        self.assertEqual(planted, ["outside", "outside-scope", "claims-done"])
        funcs = {u[1][0] for u in units if u[0] == "acceptance"}
        self.assertNotIn("TestMain", funcs)
        self.assertNotIn("Testhelper", funcs)
        self.assertNotIn("TestCommentedOut", funcs)
        self.assertNotIn("TestStopPoints", {u[1][0] for u in units if len(u[1]) == 1})

    def test_planted_a_name_only_in_a_comment_does_not_count(self):
        s = with_acceptance(replace=('name: "agent-step", // name: "nope"', 'name: "agent-step", // name: "nope"\n\t\t// name: "ghost",'))
        self.assertEqual(go_model(s)[1], ["agent-step", "review-round"])

    def test_planted_an_element_without_a_name_is_refused(self):
        s = with_acceptance(replace=('{name: "claims-done"}', '{step: "code"}'))
        self.assertTrue(messages(go_model(s)[3], "`name:` values, not one"))

    def test_planted_unreadable_go_list_is_an_error_not_an_empty_list(self):
        s = with_acceptance(replace=("var stopPoints = []stopPoint{", "var stopPointsX = []stopPoint{"))
        self.assertTrue(messages(go_model(s)[3], "cannot read stopPoints"))

    # 1
    def test_planted_new_top_level_test_that_no_job_runs_fails(self):
        for pkg in PKGS:
            with self.subTest(pkg=pkg):
                s = copy.deepcopy(FIXTURE_GO)
                s[pkg] = s[pkg] + ["func TestBrandNew(t *testing.T) {}\n"]
                v = judge_fixture(s)
                # a plain new Test is run by the Docker job (no skip covers it): the judge accepts it,
                # and a new one that a skip also drops must fail (case 6 below)
                self.assertEqual(v, [])

    def test_planted_new_test_dropped_by_the_docker_skip_runs_nowhere(self):
        s = with_acceptance("func TestPlantedX(t *testing.T) {}\nfunc TestStopPointsX(t *testing.T) {}\n")
        wf, decl = fixture_workflow()
        wf["jobs"]["docker"]["steps"][0]["run"] = wf["jobs"]["docker"]["steps"][0]["run"].replace(DOCKER_SKIP, "TestStopPoints|TestPlanted|^TestUS2")
        v = judge_fixture(s, wf, decl)
        self.assertTrue(messages(v, "no job runs", "TestPlantedX"), v)
        self.assertTrue(messages(v, "no job runs", "TestStopPointsX"), v)

    # 2
    def test_planted_a_test_run_by_two_jobs_fails(self):
        wf, decl = fixture_workflow()
        wf["jobs"]["us2_again"] = copy.deepcopy(wf["jobs"]["us2"])
        wf["jobs"]["us2_again"]["if"] = "contains(fromJSON(needs.config.outputs.jobs), 'us2_again')"
        decl["jobs"]["us2_again"] = {"paths": ["**"]}
        wf["jobs"]["summary"]["needs"].append("us2_again")
        v = judge_fixture(None, wf, decl)
        self.assertTrue(messages(v, "TestUS2", "runs in 2 jobs"), v)

    def test_planted_overlapping_selections_fail(self):
        wf, decl = fixture_workflow()
        wf["jobs"]["planted_claims_done"]["steps"][0]["run"] = "scripts/ci-step.sh x scripts/check.sh --only docker --run '^TestPlanted$'"
        v = judge_fixture(None, wf, decl)
        self.assertTrue(messages(v, "TestPlanted/outside", "runs in 2 jobs"), v)

    # 3
    def test_planted_skip_that_only_skips_the_exact_name_leaves_the_prefixed_one_twice(self):
        # TestUS2 vs TestUS2_10_...: the Docker job skips `^TestUS2$`, `ap test us2` runs `^TestUS2`
        wf, decl = fixture_workflow()
        wf["jobs"]["docker"]["steps"][0]["run"] = wf["jobs"]["docker"]["steps"][0]["run"].replace("^TestUS2", "^TestUS2$").replace("'^TestStopPoints$|^TestPlanted$|^TestUS2$'", "'^TestStopPoints$|^TestPlanted$|^TestUS2$'")
        v = judge_fixture(None, wf, decl)
        self.assertTrue(messages(v, "TestUS2_10", "runs in 2 jobs"), v)

    def test_planted_unanchored_skip_drops_a_test_that_starts_the_same(self):
        s = with_acceptance("func TestPlantedX(t *testing.T) {}\n")
        wf, decl = fixture_workflow()
        run = wf["jobs"]["docker"]["steps"][0]["run"]
        wf["jobs"]["docker"]["steps"][0]["run"] = run.replace(DOCKER_SKIP, "TestStopPoints|TestPlanted|^TestUS2")
        v = judge_fixture(s, wf, decl)
        self.assertTrue(messages(v, "no job runs", "TestPlantedX"), v)

    def test_clean_anchored_skip_keeps_a_test_that_starts_the_same_in_the_docker_job(self):
        s = with_acceptance("func TestPlantedX(t *testing.T) {}\nfunc TestStopPointsX(t *testing.T) {}\n")
        self.assertEqual(judge_fixture(s), [])

    def test_planted_unanchored_scenario_selection_runs_the_longer_name_too(self):
        wf, decl = fixture_workflow()
        wf["jobs"]["planted_outside"]["steps"][0]["run"] = "scripts/ci-step.sh x scripts/check.sh --only docker --run '^TestPlanted$/^outside'"
        v = judge_fixture(None, wf, decl)
        self.assertTrue(messages(v, "TestPlanted/outside-scope", "runs in 2 jobs"), v)

    def test_planted_scenario_prefix_of_another_is_handled_by_anchored_names(self):
        # clean: `outside` and `outside-scope` each have their own job; the fixture passes
        self.assertEqual(messages(judge_fixture(), "outside"), [])

    # 4
    def test_planted_stop_point_in_go_without_a_job_fails(self):
        s = with_acceptance(replace=("var stopPoints = []stopPoint{", 'var stopPoints = []stopPoint{\n\t{name: "extra-stop"},'))
        v = judge_fixture(s)
        self.assertTrue(messages(v, "no job runs", "TestStopPoints/extra-stop"), v)
        self.assertTrue(messages(v, "stop points", "missing ['extra-stop']"), v)

    def test_planted_scenario_in_go_without_a_job_fails(self):
        s = with_acceptance(replace=("var plantedScenarios = []scenario{", 'var plantedScenarios = []scenario{\n\t{name: "extra-scenario"},'))
        v = judge_fixture(s)
        self.assertTrue(messages(v, "no job runs", "TestPlanted/extra-scenario"), v)

    def test_planted_dropped_stop_point_job_fails(self):
        wf, decl = fixture_workflow()
        wf["jobs"]["stop"]["strategy"]["matrix"]["sp"].remove("review-round")
        v = judge_fixture(None, wf, decl)
        self.assertTrue(messages(v, "no job runs", "TestStopPoints/review-round"), v)

    def test_planted_dropped_scenario_job_fails(self):
        wf, decl = fixture_workflow()
        del wf["jobs"]["planted_claims_done"]
        v = judge_fixture(None, wf, decl)
        self.assertTrue(messages(v, "no job runs", "TestPlanted/claims-done"), v)

    def test_planted_job_for_a_stop_point_or_scenario_not_in_go_fails(self):
        wf, decl = fixture_workflow()
        wf["jobs"]["stop"]["strategy"]["matrix"]["sp"].append("ghost")
        wf["jobs"]["planted_ghost"] = copy.deepcopy(wf["jobs"]["planted_outside"])
        wf["jobs"]["planted_ghost"]["steps"][0]["run"] = "src/deploy/ap test planted ghost"
        wf["jobs"]["planted_ghost"]["if"] = "contains(fromJSON(needs.config.outputs.jobs), 'planted_ghost')"
        decl["jobs"]["planted_ghost"] = {"paths": ["**"]}
        wf["jobs"]["summary"]["needs"].append("planted_ghost")
        v = judge_fixture(None, wf, decl)
        self.assertTrue(messages(v, "stop points", "extra ['ghost']"), v)
        self.assertTrue(messages(v, "planted scenarios", "extra ['ghost']"), v)
        self.assertTrue(messages(v, "job stop[sp=ghost] selects no test"), v)

    # 5
    def test_planted_job_without_its_if_fails(self):
        wf, decl = fixture_workflow()
        del wf["jobs"]["planted_outside"]["if"]
        self.assertTrue(messages(judge_fixture(None, wf, decl), "planted_outside", "if:"))

    def test_planted_job_without_a_declaration_fails(self):
        wf, decl = fixture_workflow()
        del decl["jobs"]["us2"]
        self.assertTrue(messages(judge_fixture(None, wf, decl), "us2", "not declared"))

    def test_planted_job_not_in_summary_needs_fails(self):
        wf, decl = fixture_workflow()
        wf["jobs"]["summary"]["needs"].remove("stop")
        self.assertTrue(messages(judge_fixture(None, wf, decl), "stop", "summary"))

    def test_planted_docker_suite_jobs_selected_by_different_paths_fail(self):
        wf, decl = fixture_workflow()
        decl["jobs"]["us2"] = {"paths": ["src/**"]}
        self.assertTrue(messages(judge_fixture(None, wf, decl), "not selected together"))

    # 6
    def test_planted_docker_job_without_its_skip_fails(self):
        wf, decl = fixture_workflow()
        wf["jobs"]["docker"]["steps"][0]["run"] = "scripts/ci-step.sh docker scripts/check.sh --only docker"
        v = judge_fixture(None, wf, decl)
        self.assertTrue(messages(v, "TestUS2", "runs in 2 jobs"), v)
        self.assertTrue(messages(v, "TestPlanted/outside", "runs in 2 jobs"), v)
        self.assertTrue(messages(v, "TestStopPoints/agent-step", "runs in 2 jobs"), v)

    def test_planted_docker_job_with_an_extra_skip_fails(self):
        wf, decl = fixture_workflow()
        run = wf["jobs"]["docker"]["steps"][0]["run"]
        wf["jobs"]["docker"]["steps"][0]["run"] = run.replace(DOCKER_SKIP, DOCKER_SKIP + "|^TestHealthIsOpen$")
        v = judge_fixture(None, wf, decl)
        self.assertTrue(messages(v, "no job runs", "lint.TestHealthIsOpen"), v)

    # unreadable forms
    def test_planted_unreadable_forms_fail_with_a_message(self):
        wf, decl = fixture_workflow()
        cases = {
            "two lines": "scripts/ci-step.sh a scripts/check.sh --only docker\necho x",
            "unknown expression": "scripts/ci-step.sh a scripts/check.sh --only docker --run '${{ inputs.x }}'",
            "unknown option": "scripts/ci-step.sh a scripts/check.sh --only docker --bogus 1",
        }
        for name, run in cases.items():
            with self.subTest(name):
                w = copy.deepcopy(wf)
                w["jobs"]["us2"]["steps"][0]["run"] = run
                v = judge_fixture(None, w, decl)
                self.assertTrue(messages(v, "us2"), v)

    def test_planted_unreadable_matrix_fails_with_a_message(self):
        wf, decl = fixture_workflow()
        wf["jobs"]["stop"]["strategy"]["matrix"] = {"sp": "${{ fromJSON(needs.config.outputs.sp) }}"}
        self.assertTrue(messages(judge_fixture(None, wf, decl), "matrix.sp is not a list"))

    def test_go_matching_model(self):
        s = Selection("^TestPlanted$/^outside$")
        self.assertEqual(s.status(("TestPlanted", "outside")), "run")
        self.assertEqual(s.status(("TestPlanted", "outside-scope")), "no")
        self.assertEqual(s.status(("TestPlanted",)), "partial")
        self.assertEqual(Selection("", "^TestUS2$").status(("TestUS2_10_X",)), "run")
        self.assertEqual(Selection("", "^TestUS2").status(("TestUS2_10_X",)), "no")
        self.assertEqual(Selection("", "^TestPlanted$").status(("TestPlanted", "a")), "no")
        self.assertEqual(Selection("", "^TestPlanted$/^a$").status(("TestPlanted",)), "run")
        self.assertEqual(split_pattern("^A$/^(b/c)$/[/]"), ["^A$", "^(b/c)$", "[/]"])


# ---------------------------------------------------------------- the real files

class RealSplitTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.wf = yaml.safe_load(WORKFLOW.read_text())
        cls.decl = yaml.safe_load(DECLARATION.read_text())
        cls.sources = load_sources()
        cls.model = go_model(cls.sources)
        cls.resolver = RealResolver()
        cls.insts, cls.errors = build_instances(cls.wf, cls.resolver)
        cls.base = judge(cls.model, cls.insts, cls.errors)

    @classmethod
    def tearDownClass(cls):
        cls.resolver.close()

    def violations(self, *needles):
        return messages(self.base, *needles)

    def variant(self, wf=None, sources=None):
        model = go_model(sources) if sources else self.model
        insts, errors = build_instances(wf or self.wf, self.resolver)
        return judge(model, insts, errors)

    def new_violations(self, v):
        return [m for m in v if m not in set(self.base)]

    def test_real_go_sources_are_read(self):
        units, stop, planted, errors = self.model
        self.assertEqual(errors, [])
        self.assertTrue(stop and planted and units)
        self.assertTrue({"acceptance", "recorded", "lint", "contract"} <= {u[0] for u in units})

    # 1: every top-level test, exactly once
    def test_real_every_top_level_test_runs_in_exactly_one_job(self):
        def top(m):
            tok = m.split(" ")[3] if m.startswith("no job runs") else m.split(" ")[0]
            return re.fullmatch(r"\w+\.Test\w*", tok) is not None
        units = [u for u in self.model[0] if len(u[1]) == 1]
        bad = [m for m in self.base if top(m)]
        self.assertEqual(bad, [], "of %d top-level tests" % len(units))
        self.assertEqual(self.errors, [])

    def test_real_every_stop_point_runs_in_exactly_one_job(self):
        self.assertEqual(self.violations("TestStopPoints/"), [])

    def test_real_every_planted_scenario_runs_in_exactly_one_job_through_ap(self):
        self.assertEqual(self.violations("TestPlanted/"), [])
        names = [n for i in self.insts for n in [named(i, "TestPlanted")[0]] if n]
        self.assertEqual(sorted(names), sorted(self.model[2]))
        for i in self.insts:
            if named(i, "TestPlanted")[0]:
                self.assertEqual((i.kind, i.args[:2]), ("ap", ["test", "planted"]), "%s must run through ap test planted <scenario>" % i.label)

    def test_real_us2_tests_run_in_the_ap_us2_job_not_the_docker_job(self):
        self.assertEqual(self.violations("TestUS2"), [])
        us2 = [i for i in self.insts if i.kind == "ap" and i.args[:2] == ["test", "us2"]]
        self.assertEqual(len(us2), 1)
        docker = [i for i in self.insts if i.kind == "check"]
        self.assertTrue(docker)
        for i in docker:
            for u in self.model[0]:
                if u[0] == "acceptance" and u[1][0].startswith("TestUS2"):
                    self.assertEqual(i.runs["acceptance"].status(u[1]), "no", "%s runs %s" % (i.label, u[1][0]))

    # 2
    def test_real_workflow_lists_equal_the_go_lists(self):
        self.assertEqual(self.violations("the workflow's"), [])

    def test_real_every_job_selects_a_test_and_every_selection_is_readable(self):
        self.assertEqual(self.errors, [])
        self.assertEqual(self.violations("selects no test"), [])
        self.assertEqual(self.violations("only partly"), [])

    def test_real_whole_judgement_is_clean(self):
        self.assertEqual(self.base, [])

    # 5
    def test_real_docker_suite_jobs_are_declared_selected_together_and_in_summary(self):
        self.assertTrue(self.insts)
        self.assertEqual(judge_guards(self.wf, self.decl, {i.jid for i in self.insts}), [])
        # test_check_jobs_declaration.py and test_workflows.py prove the declaration, the `if:`
        # and summary's needs for every job generically; this repeats them only for these jobs.

    # 6
    def test_real_docker_job_carries_a_selection_that_drops_what_other_jobs_run(self):
        docker = [i for i in self.insts if i.kind == "check" and "TestStopPoints" not in (i.runs["acceptance"].run_text)]
        self.assertTrue(docker, "no check.sh job runs the Docker suites without a stop-point selection")
        self.assertTrue(any(i.runs["acceptance"].skip for i in docker), "the Docker job has no skip: it would repeat the other jobs' tests")

    # 7
    def test_real_check_sh_with_no_arguments_runs_every_docker_test(self):
        for reports in (False, True):
            with self.subTest(reports=reports):
                v = no_args_violations(CHECK_SH.read_text(), reports)
                self.assertEqual(v, [])

    def test_planted_check_sh_with_a_default_skip_fails_the_no_arguments_check(self):
        text = CHECK_SH.read_text()
        self.assertIn("-test.count=1", text)
        v = no_args_violations(text.replace("-test.count=1", "-test.count=1 -test.skip=TestPlanted"), False)
        self.assertTrue(messages(v, "-test.skip"), v)
        v = no_args_violations(text.replace("-test.count=1", "-test.count=1 -test.run=^TestUS1$"), True)
        self.assertTrue(messages(v, "-test.run"), v)

    def test_planted_check_sh_that_skips_a_package_fails_the_no_arguments_check(self):
        text = CHECK_SH.read_text()
        self.assertIn('suites="recorded lint acceptance contract"', text)
        v = no_args_violations(text.replace('suites="recorded lint acceptance contract"', 'suites="recorded lint acceptance"'), False)
        self.assertTrue(messages(v, "contract"), v)

    # planted changes of the real workflow and sources (new violation required)
    def _job_of(self, func, name):
        for i in self.insts:
            if named(i, func)[0] == name:
                return i
        self.fail("no job runs %s %s: the split does not exist yet" % (func, name))

    def _drop(self, inst):
        wf = copy.deepcopy(self.wf)
        job = wf["jobs"][inst.jid]
        m = (job.get("strategy") or {}).get("matrix")
        done = False
        if m:
            for k, vals in m.items():
                if isinstance(vals, list):
                    for x in list(vals):
                        if "%s=%s" % (k, x) in inst.label and len(vals) > 1:
                            vals.remove(x)
                            done = True
        if not done:
            del wf["jobs"][inst.jid]
        return wf

    def test_planted_real_workflow_without_one_stop_point_job_fails(self):
        i = self._job_of("TestStopPoints", self.model[1][0])
        new = self.new_violations(self.variant(self._drop(i)))
        self.assertTrue(messages(new, "no job runs", "TestStopPoints/%s" % self.model[1][0]), new)

    def test_planted_real_workflow_without_one_planted_job_fails(self):
        i = self._job_of("TestPlanted", self.model[2][0])
        new = self.new_violations(self.variant(self._drop(i)))
        self.assertTrue(messages(new, "no job runs", "TestPlanted/%s" % self.model[2][0]), new)

    def test_planted_real_workflow_with_a_scenario_twice_fails(self):
        i = self._job_of("TestPlanted", self.model[2][0])
        wf = copy.deepcopy(self.wf)
        wf["jobs"][i.jid + "_again"] = copy.deepcopy(wf["jobs"][i.jid])
        new = self.new_violations(self.variant(wf))
        self.assertTrue(messages(new, "TestPlanted/%s" % self.model[2][0], "runs in 2 jobs"), new)

    def test_planted_real_docker_job_without_its_selection_fails(self):
        docker = [i for i in self.insts if i.kind == "check" and not named(i, "TestStopPoints")[0]]
        self.assertTrue(docker, "no Docker job found")
        wf = copy.deepcopy(self.wf)
        job = wf["jobs"][docker[0].jid]
        job["env"] = {k: v for k, v in (job.get("env") or {}).items() if k == "AP_TEST_REPORT_DIR"}
        for s in job["steps"]:
            if isinstance(s.get("run"), str) and suite_kind(s["run"]) == "check":
                s["env"] = {k: v for k, v in (s.get("env") or {}).items() if k == "AP_TEST_REPORT_DIR"}
                s["run"] = "scripts/ci-step.sh docker scripts/check.sh --only docker"
        new = self.new_violations(self.variant(wf))
        # with no selection the Docker job repeats every test another job runs
        self.assertTrue(messages(new, "runs in"), new)

    def test_planted_real_go_sources_with_a_new_stop_point_or_scenario_fail(self):
        for var, anchor, func in (("stopPoints", "var stopPoints = []stopPoint{", "TestStopPoints"),
                                  ("plantedScenarios", "var plantedScenarios = []scenario{", "TestPlanted")):
            with self.subTest(var):
                s = copy.deepcopy(self.sources)
                n = 0
                for k, t in enumerate(s["acceptance"]):
                    if anchor in t:
                        s["acceptance"][k] = t.replace(anchor, anchor + '\n\t{name: "planted-extra"},')
                        n += 1
                self.assertEqual(n, 1, "cannot plant: %s not found" % anchor)
                new = self.new_violations(self.variant(sources=s))
                self.assertTrue(messages(new, "no job runs", "%s/planted-extra" % func), new)

    def test_planted_real_go_sources_with_a_new_test_func_that_a_skip_drops_fail_or_run_in_docker(self):
        for name in ("TestBrandNew", "TestPlantedX", "TestStopPointsX", "TestUS2X"):
            with self.subTest(name):
                s = copy.deepcopy(self.sources)
                s["acceptance"] = s["acceptance"] + ["func %s(t *testing.T) {}\n" % name]
                new = self.new_violations(self.variant(sources=s))
                if name == "TestUS2X":
                    # prefix of TestUS2: `ap test us2` runs it, the Docker job must not
                    self.assertEqual(messages(new, name, "runs in 2 jobs"), [], new)
                else:
                    # run by exactly one job: the Docker job (anchored skips), never zero, never two
                    self.assertEqual(messages(new, name), [], new)


def no_args_violations(check_text, reports):
    base = Path(tempfile.mkdtemp(prefix="test-ci-split-noargs-", dir=TMP_BASE))
    try:
        stub = Stub(base, check_text)
        env = {"AP_TEST_REPORT_DIR": RUNNER_TEMP + "/test-reports"} if reports else {}
        r, runs = stub.run("check", [], env, json_events="")
        v = []
        if r.returncode != 0:
            v.append("scripts/check.sh with no arguments failed (%d): %s" % (r.returncode, (r.stdout + r.stderr)[-300:]))
        seen = {}
        for x in runs:
            seen.setdefault(Path(x["cwd"]).name, []).append(x["argv"])
        for p in PKGS:
            if len(seen.get(p, [])) != 1:
                v.append("a full run ran the %s binary %d times" % (p, len(seen.get(p, []))))
        for p, lst in seen.items():
            for argv in lst:
                if any(re.match(r"--?test\.skip", a) for a in argv):
                    v.append("%s binary gets -test.skip %s" % (p, argv))
                try:
                    s = selection_from_argv(argv)
                except Unreadable as e:
                    v.append("%s: %s" % (p, e))
                    continue
                if s.run_text not in ("", ".", "^"):
                    v.append("%s binary gets -test.run %r in a full run" % (p, s.run_text))
        return v
    finally:
        shutil.rmtree(base, ignore_errors=True)


# ---------------------------------------------------------------- ap test planted <scenario>

class ApPlantedScenarioTest(unittest.TestCase):
    SCENARIOS = ["edit-judging-tests", "outside-scope", "forbidden-dependency", "claims-done", "non-converging-review"]

    def setUp(self):
        self.base = Path(tempfile.mkdtemp(prefix="test-ci-split-ap-", dir=TMP_BASE))
        self.addCleanup(shutil.rmtree, self.base, ignore_errors=True)
        self.stub = Stub(self.base)

    def ap(self, *args, events=""):
        return self.stub.run("ap", list(args), {}, json_events=events)

    def pattern(self, runs):
        self.assertEqual(len(runs), 1, runs)
        sel = [a for a in runs[0]["argv"] if a.startswith(("-test.run", "--test.run"))]
        vals = [a.split("=", 1)[1] for a in sel if "=" in a]
        vals += [runs[0]["argv"][i + 1] for i, a in enumerate(runs[0]["argv"]) if a in ("-test.run", "--test.run")]
        self.assertEqual(len(vals), 1, runs[0]["argv"])
        return vals[0]

    def assert_usage(self, *args):
        r, runs = self.ap(*args)
        self.assertEqual(r.returncode, 3, "args %r: %s" % (args, r.stdout + r.stderr))
        self.assertEqual(runs, [], "args %r ran a test binary" % (args,))

    def test_a_scenario_runs_only_itself_and_reports_one_of_one(self):
        for sc in ("claims-done", "outside-scope", "outside"):
            with self.subTest(sc):
                r, runs = self.ap("planted", sc, events=planted_events([sc]))
                self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
                self.assertEqual(self.pattern(runs), "^TestPlanted$/^%s$" % sc)
                lines = [x for x in r.stdout.splitlines() if x.strip()]
                self.assertIn("planted: 1 of 1 scenarios stopped in 3 out of 3 trials", lines)
                self.assertEqual(lines[-1], "planted: 1 of 1 scenarios stopped in 3 out of 3 trials")
                self.assertEqual(lines[:3], ["PASS  %s  trial %d" % (sc, n) for n in (1, 2, 3)])

    def test_a_scenario_name_that_prefixes_another_selects_only_its_own(self):
        r, runs = self.ap("planted", "outside", events=planted_events(["outside"]))
        p = self.pattern(runs)
        self.assertTrue(p.endswith("$") and p.startswith("^TestPlanted$/^"), p)
        elems = split_pattern(p)
        self.assertIsNone(re.search(elems[1], "outside-scope"))

    def test_empty_name_is_usage(self):
        self.assert_usage("planted", "")

    def test_uppercase_name_is_usage(self):
        self.assert_usage("planted", "Outside-scope")
        self.assert_usage("planted", "outsideScope")

    def test_name_with_a_slash_is_usage(self):
        self.assert_usage("planted", "a/b")
        self.assert_usage("planted", "outside-scope/trial-1")

    def test_names_with_regex_characters_are_usage(self):
        for ch in (".", "*", "$", "|", "^", "(", "[", "+", "?", "\\", " "):
            with self.subTest(ch=ch):
                self.assert_usage("planted", "out%sside" % ch)

    def test_leading_or_trailing_hyphen_never_runs_another_pattern(self):
        # The contract allows lowercase letters, digits and hyphens, so `-x` is well formed: it is
        # either a usage error (3) or a name no scenario has (1, no test ran); never a binary run
        # with a pattern other than the exact one.
        for name in ("-outside", "outside-", "-"):
            with self.subTest(name):
                r, runs = self.ap("planted", name)
                self.assertIn(r.returncode, (1, 3), r.stdout + r.stderr)
                for x in runs:
                    self.assertIn("^TestPlanted$/^%s$" % name, x["argv"])

    def test_a_second_argument_is_usage(self):
        self.assert_usage("planted", "outside-scope", "claims-done")
        self.assert_usage("planted", "outside-scope", "")

    def test_a_scenario_given_to_us2_is_usage(self):
        self.assert_usage("us2", "outside-scope")
        self.assert_usage("us2", "")

    def test_a_well_formed_unknown_name_runs_no_test_and_exits_1(self):
        r, runs = self.ap("planted", "no-such-scenario", events="")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("test: no test of the planted suite ran", r.stderr)
        self.assertEqual(self.pattern(runs), "^TestPlanted$/^no-such-scenario$")

    def test_planted_without_a_scenario_keeps_the_whole_suite_pattern(self):
        r, runs = self.ap("planted", events=planted_events(self.SCENARIOS))
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(self.pattern(runs), "^TestPlanted$")
        self.assertEqual(r.stdout.splitlines()[-1], "planted: 5 of 5 scenarios stopped in 3 out of 3 trials")

    def test_us2_keeps_its_pattern(self):
        ev = "".join(json.dumps(e, separators=(",", ":")) + "\n" for e in ({"Action": "run", "Test": "TestUS2"}, {"Action": "pass", "Test": "TestUS2"}))
        r, runs = self.ap("us2", events=ev)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(self.pattern(runs), "^TestUS2")


if __name__ == "__main__":
    unittest.main()
