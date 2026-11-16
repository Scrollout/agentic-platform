"""Tests for the GitHub Actions workflows (T107, T109, T120; research D41, D45, ADR 0044, ADR 0048).

Since T120 evals.yml has no push trigger: it runs only when started by hand (workflow_dispatch
with the boolean input `all`); scripts/tests/test_workflows_evals_current.py judges that and
check.yml's evals-current job.

Each rule is one function, judge_<rule>(root), that reads the workflow files under root and
returns the violations as strings. Every rule has a test on the repository's real files and a
planted case: a temporary tree that is valid except for one violation, judged by the same
function. A baseline test proves the temporary tree is valid, so a planted failure is the
planted violation and nothing else.
"""
import copy
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
CHECK_SH = REPO / "scripts" / "check.sh"
HOOK = REPO / "scripts" / "hooks" / "commit-msg"
SLUG = "https://github.com/Scrollout/agentic-platform/actions/workflows"
HASH = "a" * 40


# ---------------------------------------------------------------- loading

def real_steps():
    r = subprocess.run([str(CHECK_SH), "--list"], capture_output=True, text=True,
                       timeout=60, cwd=REPO)
    assert r.returncode == 0, r.stdout + r.stderr
    steps = [l.strip().split()[0] for l in r.stdout.splitlines() if l.strip()]
    assert steps, "scripts/check.sh --list printed no step"
    return steps


def load(root):
    """Return {file name: parsed workflow} for root/.github/workflows/*.yml, and say what was read."""
    wf = {}
    for p in sorted((Path(root) / ".github" / "workflows").glob("*.yml")):
        data = yaml.safe_load(p.read_text()) or {}
        if True in data and "on" not in data:   # YAML 1.1 reads the key `on` as True
            data["on"] = data.pop(True)
        wf[p.name] = data
    print(f"workflows examined under {root}: {sorted(wf) or 'none'}", file=sys.stderr)
    return wf


def triggers(w):
    on = w.get("on")
    if isinstance(on, str):
        return {on: None}
    if isinstance(on, list):
        return {k: None for k in on}
    return dict(on or {})


def jobs(w):
    return w.get("jobs") or {}


def need_files(wf, *names):
    return [f"{n} is missing" for n in names if n not in wf]


def walk(x):
    if isinstance(x, dict):
        for k, v in x.items():
            yield k, v
            yield from walk(v)
    elif isinstance(x, list):
        for v in x:
            yield from walk(v)


# ---------------------------------------------------------------- rules

def judge_steps_have_jobs(root, steps):
    wf = load(root)
    v = need_files(wf, "check.yml")
    if v:
        return v
    runs = {}
    for name, job in jobs(wf["check.yml"]).items():
        runs[name] = "\n".join(str(s.get("run", "")) for s in job.get("steps") or [])
    for step in steps:
        pat = re.compile(r"scripts/check\.sh\s+--only\s+" + re.escape(step) + r"(\s|$)")
        if not any(pat.search(t) for t in runs.values()):
            v.append(f"no job in check.yml runs scripts/check.sh --only {step}")
    return v


def judge_triggers(root):
    wf = load(root)
    v = need_files(wf, "check.yml", "evals.yml")
    for name, w in wf.items():
        t = triggers(w)
        for bad in ("pull_request", "pull_request_target"):
            if bad in t:
                v.append(f"{name} has a {bad} trigger")
    if "check.yml" in wf:
        t = triggers(wf["check.yml"])
        if set(t) != {"push", "workflow_dispatch"}:
            v.append(f"check.yml triggers are {sorted(t)}, not push and workflow_dispatch")
    if "evals.yml" in wf:
        t = triggers(wf["evals.yml"])
        if set(t) != {"workflow_dispatch"}:
            v.append(f"evals.yml triggers are {sorted(t)}, not workflow_dispatch alone")
    return v


def judge_permissions(root):
    wf = load(root)
    v = need_files(wf, "check.yml", "evals.yml")
    for name, w in wf.items():
        if w.get("permissions") != {"contents": "read"}:
            v.append(f"{name} top-level permissions is {w.get('permissions')!r}, not contents: read")
        for jn, job in jobs(w).items():
            # T115, T117, T120 (owner, 2026-11-16): listing workflow runs needs actions: read, on
            # evals.yml's config job and check.yml's config and evals-current jobs alone.
            allowed = [{"contents": "read"}]
            if (name, jn) in (("evals.yml", "config"), ("check.yml", "config"),
                              ("check.yml", "evals-current")):
                allowed.append({"contents": "read", "actions": "read"})
            if "permissions" in job and job["permissions"] not in allowed:
                v.append(f"{name} job {jn} sets permissions {job['permissions']!r}")
    return v


def judge_pinned_uses(root):
    wf = load(root)
    v = need_files(wf, "check.yml", "evals.yml")
    ok = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(/[^@\s]+)?@[0-9a-f]{40}$")
    for name, w in wf.items():
        for k, val in walk(w):
            if k == "uses" and not (isinstance(val, str) and
                                    (val.startswith("./") or ok.match(val))):
                v.append(f"{name}: uses {val!r} is not pinned to a 40-hex commit hash")
    return v


EXPR = re.compile(r"\$\{\{(.*?)\}\}", re.S)
SECRETS_WORD = re.compile(r"(?<![\w.-])secrets(?![\w-])")


def secret_refs(x):
    """Every way a workflow can reach a secret: secrets.X, secrets['X'], toJSON(secrets) inside an
    expression (or an `if:`, which is one), and a `secrets:` key (a reusable-workflow call)."""
    out = []
    for k, val in walk(x):
        if k == "secrets":
            out.append("a secrets: key")
        if isinstance(val, str):
            exprs = EXPR.findall(val) + ([val] if k == "if" else [])
            out += [e.strip() for e in exprs if SECRETS_WORD.search(e)]
    return out


def judge_secrets_environment(root):
    wf = load(root)
    v = need_files(wf, "check.yml", "evals.yml")
    for name, w in wf.items():
        top = {k: x for k, x in w.items() if k != "jobs"}
        if any(k == "environment" for k, _ in walk(top)) or secret_refs(top):
            v.append(f"{name} names an environment or a secret outside any job")
        for jn, job in jobs(w).items():
            is_evals = name == "evals.yml" and jn == "evals"
            env = job.get("environment")
            envname = env.get("name") if isinstance(env, dict) else env
            if is_evals:
                if envname != "dev":
                    v.append("evals.yml job evals does not have environment: dev")
            else:
                if secret_refs(job):
                    v.append(f"{name} job {jn} references secrets")
                if "environment" in job:
                    v.append(f"{name} job {jn} sets environment")
    if "evals.yml" in wf and "evals" not in jobs(wf["evals.yml"]):
        v.append("evals.yml has no job named evals")
    return v


def judge_summary_job(root):
    wf = load(root)
    v = need_files(wf, "check.yml")
    if v:
        return v
    js = jobs(wf["check.yml"])
    others_of = lambda n: set(js) - {n}
    cands = []
    for n, j in js.items():
        cond = str(j.get("if", ""))
        needs = j.get("needs") or []
        needs = {needs} if isinstance(needs, str) else set(needs)
        if re.fullmatch(r"\s*(\$\{\{\s*always\(\)\s*\}\}|always\(\))\s*", cond) \
                and others_of(n) and needs == others_of(n):
            cands.append(n)
    if not cands:
        v.append("check.yml has no summary job with if: always() that needs every other job")
    elif list(js)[-1] not in cands:
        v.append("check.yml's summary job is not the last job")
    return v


def judge_hook_protects_github(root):
    p = Path(root) / "scripts" / "hooks" / "commit-msg"
    print(f"hook examined: {p}", file=sys.stderr)
    if not p.exists():
        return [f"{p} is missing"]
    lines = [l for l in p.read_text().splitlines() if re.match(r"\s*protected=", l)]
    if not lines:
        return ["commit-msg has no protected= line"]
    m = re.search(r"protected=(['\"])(.*)\1", lines[0])
    if not m:
        return ["commit-msg protected= line is not a quoted pattern"]
    pat = re.compile(m.group(2))
    v = []
    if not pat.search(".github/workflows/x.yml"):
        v.append("commit-msg protected= does not match .github/workflows/x.yml")
    if pat.search("docs/.github/x.yml") and not pat.search("docs/scripts/x"):
        v.append("commit-msg protected= matches .github/ below another directory, not ^\\.github/")
    return v


def judge_badges(root):
    p = Path(root) / "README.md"
    print(f"README examined: {p}", file=sys.stderr)
    if not p.exists():
        return [f"{p} is missing"]
    text = p.read_text()
    wfdir = Path(root) / ".github" / "workflows"
    names = sorted(x.name for x in wfdir.glob("*.yml")) or ["check.yml", "evals.yml"]
    v = [f"README.md shows no badge for {n}" for n in ("check.yml", "evals.yml")
         if f"{SLUG}/{n}/badge.svg" not in text]
    v += [f"README.md shows no badge for {n}" for n in names
          if n not in ("check.yml", "evals.yml") and f"{SLUG}/{n}/badge.svg" not in text]
    return v


# ---------------------------------------------------------------- T107/T109 rules

RUNNER_EXPR = "${{needs.config.outputs.runner}}"


def norm(x):
    return re.sub(r"\s+", "", str(x))


def needs_of(job):
    n = job.get("needs") or []
    return {n} if isinstance(n, str) else set(n)


def runs(job):
    return [str(s.get("run", "")) for s in job.get("steps") or []]


def is_always(cond):
    return re.fullmatch(r"\s*(\$\{\{\s*always\(\)\s*\}\}|always\(\))\s*", str(cond)) is not None


def judge_runs_on(root):
    wf = load(root)
    v = need_files(wf, "check.yml", "evals.yml")
    for name, w in wf.items():
        js = jobs(w)
        if "config" not in js:
            v.append(f"{name} has no config job")
        for jn, job in js.items():
            ro = norm(job.get("runs-on"))
            if jn in ("config", "summary"):
                if ro != "ubuntu-latest":
                    v.append(f"{name} job {jn} runs on {job.get('runs-on')!r}, not ubuntu-latest")
            else:
                if ro != RUNNER_EXPR:
                    v.append(f"{name} job {jn} runs on {job.get('runs-on')!r}, "
                             "not ${{ needs.config.outputs.runner }}")
                if "config" not in needs_of(job):
                    v.append(f"{name} job {jn} does not need config")
    return v


def judge_config_job(root):
    wf = load(root)
    v = need_files(wf, "check.yml", "evals.yml")
    for name, w in wf.items():
        cfg = jobs(w).get("config")
        if cfg is None:
            v.append(f"{name} has no config job")
            continue
        if "runner" not in (cfg.get("outputs") or {}):
            v.append(f"{name} config job has no output named runner")
        text = "\n".join(runs(cfg))
        if "images.lock" not in text or "GITHUB_OUTPUT" not in text:
            v.append(f"{name} config job does not read images.lock into $GITHUB_OUTPUT")
    return v


LITERALS = [r"ubuntu-\d", r"(?i)pyyaml\s*[=<>~!]=?\s*\d", r"\bgo-?1\.\d+", r"python-?3\.\d+",
            r"golang:\d"]


def judge_no_literal_versions(root):
    wf = load(root)
    v = need_files(wf, "check.yml", "evals.yml")
    for name, w in wf.items():
        for k, val in walk(w):
            if k in ("python-version", "go-version") and re.search(r"\d", str(val)) \
                    and "${{" not in str(val):
                v.append(f"{name}: {k} is the literal {val!r}")
            if isinstance(val, str):
                for pat in LITERALS:
                    if re.search(pat, val):
                        v.append(f"{name}: {k}: {val!r} holds a literal version or label ({pat})")
    return v


def read_lock(root):
    p = Path(root) / "src" / "deploy" / "images.lock"
    print(f"images.lock examined: {p}", file=sys.stderr)
    if not p.exists():
        return None
    out = {}
    for l in p.read_text().splitlines():
        if l.strip() and not l.lstrip().startswith("#"):
            parts = l.split(None, 1)
            out[parts[0]] = parts[1].strip() if len(parts) > 1 else ""
    return out


def judge_images_lock(root):
    lock = read_lock(root)
    if lock is None:
        return ["src/deploy/images.lock is missing"]
    v = [f"images.lock has no {k}" for k in
         ("ci-runner", "ci-python", "gvisor-release", "gvisor-sha512") if not lock.get(k)]
    if lock.get("ci-runner") and not re.fullmatch(r"ubuntu-\d\d\.\d\d", lock["ci-runner"]):
        v.append(f"images.lock ci-runner {lock['ci-runner']!r} is not an ubuntu-NN.NN label")
    if lock.get("ci-python") and not re.fullmatch(r"\d+\.\d+(\.\d+)?", lock["ci-python"]):
        v.append(f"images.lock ci-python {lock['ci-python']!r} is not a version")
    if lock.get("gvisor-sha512") and not re.fullmatch(r"[0-9a-f]{128}", lock["gvisor-sha512"]):
        v.append("images.lock gvisor-sha512 is not 128 lower-case hex digits")
    return v


def judge_requirements(root):
    p = Path(root) / "scripts" / "tools" / "requirements-ci.txt"
    print(f"requirements examined: {p}", file=sys.stderr)
    if not p.exists():
        return [f"{p} is missing"]
    lines = p.read_text().replace("\\\n", " ").splitlines()
    py = [l for l in lines if re.match(r"(?i)\s*pyyaml==\d", l)]
    if not py:
        return ["requirements-ci.txt does not pin PyYAML with =="]
    if not re.search(r"--hash=sha256:[0-9a-f]{64}", py[0]):
        return ["requirements-ci.txt PyYAML has no --hash=sha256:<64 hex>"]
    return []


def judge_install_requires_hashes(root):
    wf = load(root)
    v = need_files(wf, "check.yml", "evals.yml")
    found = False
    for name, w in wf.items():
        for jn, job in jobs(w).items():
            for t in runs(job):
                for l in t.replace("\\\n", " ").splitlines():
                    if re.search(r"\bpip3?\s+install\b|-m\s+pip\s+install\b", l):
                        found = True
                        if "--require-hashes" not in l or "requirements-ci.txt" not in l:
                            v.append(f"{name} job {jn}: pip install without --require-hashes "
                                     f"-r scripts/tools/requirements-ci.txt: {l.strip()}")
    if not found and not v:
        v.append("no workflow installs PyYAML from scripts/tools/requirements-ci.txt")
    return v


def judge_checkout_credentials(root):
    wf = load(root)
    v = need_files(wf, "check.yml", "evals.yml")
    for name, w in wf.items():
        for jn, job in jobs(w).items():
            for s in job.get("steps") or []:
                if str(s.get("uses", "")).startswith("actions/checkout@"):
                    if (s.get("with") or {}).get("persist-credentials") not in (False, "false"):
                        v.append(f"{name} job {jn}: checkout without persist-credentials: false")
    return v


EVENT_EXPR = re.compile(r"github\.event(?![\w])|github\[\s*['\"]event|head_ref")


def judge_no_event_in_run(root):
    wf = load(root)
    v = need_files(wf, "check.yml", "evals.yml")
    for name, w in wf.items():
        for jn, job in jobs(w).items():
            for t in runs(job):
                for e in EXPR.findall(t):
                    if EVENT_EXPR.search(e):
                        v.append(f"{name} job {jn}: run holds the expression {e.strip()!r}")
    return v


def pos(text, pat):
    m = re.search(pat, text)
    return m.start() if m else -1


def judge_gvisor_jobs(root, steps):
    wf = load(root)
    v = need_files(wf, "check.yml")
    if v:
        return v
    for cmd in ("planted", "us2"):
        pat = r"\bap\s+test\s+" + cmd + r"\b"
        hit = [(n, j) for n, j in jobs(wf["check.yml"]).items()
               if re.search(pat, "\n".join(runs(j)))]
        if not hit:
            v.append(f"no job runs ap test {cmd}")
        for n, j in hit:
            text = "\n".join(runs(j))
            g, t = pos(text, r"scripts/ci-gvisor\.sh"), pos(text, pat)
            if g < 0 or g > t:
                v.append(f"job {n} does not run scripts/ci-gvisor.sh before ap test {cmd}")
    return v


PREPARED = (r"scripts/check\.sh\s+--only\s+(?:[\w-]+,)*(?:docker|deps|py-tests)(?:,[\w-]+)*(?:\s|$)"
            r"|\bap\s+test\b")


def judge_prepare(root):
    wf = load(root)
    v = need_files(wf, "check.yml")
    if v:
        return v
    for n, j in jobs(wf["check.yml"]).items():
        text = "\n".join(runs(j))
        t = pos(text, PREPARED)
        if t >= 0:
            p = pos(text, r"scripts/check\.sh\s+--prepare\b")
            if p < 0 or p > t:
                v.append(f"job {n} runs docker, deps or ap test without scripts/check.sh --prepare first")
    return v


def evals_job(wf):
    return jobs(wf["evals.yml"]).get("evals") or {}


def judge_evals_key_file(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    steps = evals_job(wf).get("steps") or []
    w = [i for i, s in enumerate(steps) if re.search(r"\bumask\s+0?077\b", str(s.get("run", "")))]
    if not w:
        return ["evals job writes no key file under umask 077"]
    r = [i for i, s in enumerate(steps) if is_always(s.get("if", "")) and
         re.search(r"\brm\b", str(s.get("run", "")))]
    if not r or max(r) <= w[0]:
        v.append("evals job has no `if: always()` step after the key file's step that removes it")
    return v


def judge_evals_summary(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    if not any("scripts/ci-evals-summary.py" in t for t in runs(evals_job(wf))):
        v.append("evals job does not run scripts/ci-evals-summary.py")
    return v


def by_name(wf_data, name):
    for k, j in jobs(wf_data).items():
        if k.replace("_", "-") == name or j.get("name") == name:
            return j
    return None


def find_step(job, pat):
    for i, st in enumerate(job.get("steps") or []):
        if re.search(pat, str(st.get("run", ""))):
            return i
    return -1


def judge_py_tests_images(root):
    wf = load(root)
    v = need_files(wf, "check.yml")
    if v:
        return v
    j = by_name(wf["check.yml"], "py-tests")
    if j is None:
        return ["check.yml has no py-tests job"]
    text = "\n".join(runs(j))
    last = pos(text, r"scripts/check\.sh\s+--only\s+py-tests\b")
    for what, pat in (("scripts/ci-gvisor.sh", r"scripts/ci-gvisor\.sh"),
                      ("build.sh check", r"src/deploy/build\.sh\s+check\b"),
                      ("build.sh agent", r"src/deploy/build\.sh\s+agent\b")):
        p = pos(text, pat)
        if p < 0 or last < 0 or p > last:
            v.append(f"py-tests does not run {what} before its step")
    dump = yaml.dump(j)
    made = re.search(r"COSIGN_PASSWORD=\S", "\n".join(runs(j))) or any(
        "COSIGN_PASSWORD" in (st.get("env") or {}) or "COSIGN_PASSWORD" in (j.get("env") or {})
        for st in j.get("steps") or [])
    if "COSIGN_PASSWORD" not in dump or not made:
        v.append("py-tests does not make COSIGN_PASSWORD in the job")
    if secret_refs(j):
        v.append("py-tests takes COSIGN_PASSWORD (or something) from a secret")
    d = by_name(wf["check.yml"], "docker")
    if d is None:
        v.append("check.yml has no docker job")
    else:
        t = "\n".join(runs(d))
        g, o = pos(t, r"scripts/ci-gvisor\.sh"), pos(t, r"check\.sh\s+--only\s+(?:[\w-]+,)*docker\b")
        if g < 0 or o < 0 or g > o:
            v.append("docker job does not run scripts/ci-gvisor.sh before its step")
    return v


def judge_yaml_jobs_setup(root):
    wf = load(root)
    v = need_files(wf, "check.yml")
    if v:
        return v
    for name in ("py-tests", "knowledge"):
        j = by_name(wf["check.yml"], name)
        if j is None:
            v.append(f"check.yml has no {name} job")
            continue
        steps = j.get("steps") or []
        sp = [i for i, st in enumerate(steps) if str(st.get("uses", "")).startswith("actions/setup-python@")]
        ins = [i for i, st in enumerate(steps) if "--require-hashes" in str(st.get("run", ""))
               and "requirements-ci.txt" in str(st.get("run", ""))]
        cmd = find_step(j, r"check\.sh\s+--only\s+" + name + r"\b")
        if not sp or not ins or cmd < 0 or not (sp[0] < ins[0] < cmd):
            v.append(f"{name}: no setup-python then --require-hashes install before its step")
        elif "${{" not in str((steps[sp[0]].get("with") or {}).get("python-version", "")):
            v.append(f"{name}: setup-python's version is not from the config job")
    return v


def judge_config_pins(root):
    wf = load(root)
    v = need_files(wf, "check.yml", "evals.yml")
    for name, w in wf.items():
        cfg = jobs(w).get("config")
        if cfg is None:
            continue   # judge_config_job reports it
        outs = cfg.get("outputs") or {}
        v += [f"{name} config job has no output named {k}" for k in ("runner", "python", "go")
              if k not in outs]
        text = "\n".join(runs(cfg))
        tests = re.findall(r"\[\[?\s+-[nz]\s|\btest\s+-[nz]\s", text)
        loop = re.search(r"for\s+\w+\s+in[^;\n]*\brunner\b[^;\n]*\bpython\b[^;\n]*\bgo\b", text)
        if len(tests) < 3 and not (tests and loop):
            v.append(f"{name} config step does not check that each of runner, python and go is non-empty")
    return v


def judge_fetch_depth(root):
    wf = load(root)
    v = need_files(wf, "check.yml", "evals.yml")
    need = {"contract-diff", "py-tests"}
    for name, w in wf.items():
        for jn, job in jobs(w).items():
            deep = any(str((st.get("with") or {}).get("fetch-depth")) == "0"
                       for st in job.get("steps") or [])
            key = jn.replace("_", "-")
            if deep and key not in need and job.get("name") not in need:
                v.append(f"{name} job {jn} has fetch-depth: 0 but needs no history")
            if not deep and name == "check.yml" and (key in need or job.get("name") in need):
                v.append(f"{name} job {jn} needs history (fetch-depth: 0)")
    return v


def judge_evals_report_dir(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    j = evals_job(wf)
    if "EVAL_REPORT_DIR" not in (j.get("env") or {}):
        v.append("evals job does not set EVAL_REPORT_DIR in its job-level env")
    for st in j.get("steps") or []:
        if "EVAL_REPORT_DIR" in (st.get("env") or {}):
            v.append("a step of the evals job sets EVAL_REPORT_DIR again")
    summ = [t for t in runs(j) if "scripts/ci-evals-summary.py" in t]
    if not any(re.search(r"\$\{?EVAL_REPORT_DIR\}?/report\.json", t) for t in summ):
        v.append("the evals summary step does not read $EVAL_REPORT_DIR/report.json")
    if any("eval-report" in t for t in runs(j)):
        v.append("an evals run step names the report folder literally")
    return v


# ---------------------------------------------------------------- planted tree

STEPS = ["alpha", "beta"]
RUNNER = "${{ needs.config.outputs.runner }}"
CHECKOUT = lambda depth=False: {"uses": f"actions/checkout@{HASH}", "with": dict(
    {"persist-credentials": False}, **({"fetch-depth": 0} if depth else {}))}
SETUP_PY = lambda: {"uses": f"actions/setup-python@{HASH}",
                    "with": {"python-version": "${{ needs.config.outputs.python }}"}}
CONFIG_RUN = (
    "pin() { awk -v n=\"$1\" '$1 == n { print $2 }' src/deploy/images.lock; }\n"
    "runner=$(pin ci-runner); python=$(pin ci-python); go=$(pin golang-race)\n"
    "[ -n \"$runner\" ] || exit 1\n[ -n \"$python\" ] || exit 1\n[ -n \"$go\" ] || exit 1\n"
    "printf 'runner=%s\\npython=%s\\ngo=%s\\n' \"$runner\" \"$python\" \"$go\" >> \"$GITHUB_OUTPUT\"")
CONFIG = lambda: {"runs-on": "ubuntu-latest",
                  "outputs": {k: "${{ steps.v.outputs.%s }}" % k for k in ("runner", "python", "go")},
                  "steps": [CHECKOUT(), {"id": "v", "run": CONFIG_RUN}]}
PIP = "python3 -m pip install --require-hashes -r scripts/tools/requirements-ci.txt"


def good_job(*cmds, pip=False, depth=False):
    steps = [CHECKOUT(depth)] + [{"run": c} for c in cmds]
    if pip:
        steps.append({"run": PIP})
    return {"runs-on": RUNNER, "needs": ["config"], "steps": steps}


def py_job(step, depth=False, *before):
    """A job whose step runs Python that imports yaml: setup-python, the hashed install, the step."""
    return {"runs-on": RUNNER, "needs": ["config"],
            "steps": [CHECKOUT(depth), SETUP_PY(), {"run": PIP}] + [{"run": c} for c in before]
            + [{"run": f"scripts/check.sh --only {step}"}]}


def good_check():
    js = {"config": CONFIG(),
          "alpha": good_job("scripts/check.sh --only alpha", pip=True),
          "beta": good_job("scripts/check.sh --only beta"),
          "docker": good_job("scripts/ci-gvisor.sh", "scripts/check.sh --prepare",
                             "scripts/check.sh --only docker"),
          "py-tests": py_job("py-tests", True, "scripts/ci-gvisor.sh",
                             "scripts/check.sh --prepare",
                             "COSIGN_PASSWORD=$(openssl rand -hex 16)\n"
                             "echo \"COSIGN_PASSWORD=$COSIGN_PASSWORD\" >> \"$GITHUB_ENV\"",
                             "src/deploy/build.sh check", "src/deploy/build.sh agent"),
          "knowledge": py_job("knowledge"),
          "contract-diff": good_job("scripts/check.sh --only contract-diff", depth=True),
          "deps": good_job("scripts/check.sh --prepare", "scripts/check.sh --only deps"),
          "ap-planted": good_job("scripts/ci-gvisor.sh", "scripts/check.sh --prepare",
                                 "src/deploy/ap test planted"),
          "ap-us2": good_job("scripts/ci-gvisor.sh", "scripts/check.sh --prepare",
                             "src/deploy/ap test us2")}
    js["summary"] = {"runs-on": "ubuntu-latest", "if": "always()", "needs": list(js),
                     "steps": [{"run": "python3 scripts/ci-summary.py"}]}
    return {"name": "check", "on": {"push": None, "workflow_dispatch": None},
            "permissions": {"contents": "read"}, "jobs": js}


def good_evals():
    ev = good_job("umask 077\nprintf %s \"$K\" > \"$RUNNER_TEMP/key\"",
                  "src/tests/evals/steps/run.sh live",
                  "python3 scripts/ci-evals-summary.py \"$EVAL_REPORT_DIR/report.json\"")
    ev["steps"][1]["env"] = {"K": "${{ secrets.ANTHROPIC_API_KEY }}"}
    ev["steps"].append({"if": "always()", "run": "rm -f \"$RUNNER_TEMP/key\""})
    ev["environment"] = "dev"
    ev["env"] = {"EVAL_REPORT_DIR": "${{ runner.temp }}/eval-report"}
    return {"name": "evals",
            "on": {"workflow_dispatch": {"inputs": {"all": {
                "description": "Run every suite", "type": "boolean", "default": False}}}},
            "permissions": {"contents": "read"},
            "jobs": {"config": CONFIG(), "evals": ev}}


GOOD_LOCK = ("# pins\nci-runner ubuntu-26.04\nci-python 3.14\ngvisor-release 20261109.0\n"
             "gvisor-sha512 " + "a" * 128 + "\n")
GOOD_REQS = "pyyaml==6.0.3 \\\n    --hash=sha256:" + "b" * 64 + "\n"


def good_files():
    hook = "#!/bin/sh\nprotected='^(\\.github/|scripts/|src/deploy/)'\n"
    readme = "# x\n" + "".join(f"[![{n}]({SLUG}/{n}/badge.svg)]({SLUG}/{n})\n"
                               for n in ("check.yml", "evals.yml"))
    return good_check(), good_evals(), hook, readme


class Planted:
    """A temporary tree, valid unless the test mutates it."""

    def __init__(self, testcase, mutate=None):
        check, evals, hook, readme = good_files()
        self.tmp = tempfile.TemporaryDirectory()
        testcase.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / ".github" / "workflows").mkdir(parents=True)
        (self.root / "scripts" / "hooks").mkdir(parents=True)
        self.data = {"check.yml": check, "evals.yml": evals, "hook": hook, "readme": readme,
                     "lock": GOOD_LOCK, "reqs": GOOD_REQS}
        if mutate:
            mutate(self.data)
        for n in ("check.yml", "evals.yml"):
            if self.data.get(n) is not None:
                # dump with the key `on` quoted as real files may write it either way
                (self.root / ".github" / "workflows" / n).write_text(
                    yaml.safe_dump(self.data[n], sort_keys=False))
        (self.root / "scripts" / "hooks" / "commit-msg").write_text(self.data["hook"])
        (self.root / "README.md").write_text(self.data["readme"])
        for key, rel in (("lock", "src/deploy/images.lock"),
                         ("reqs", "scripts/tools/requirements-ci.txt")):
            if self.data.get(key) is not None:
                (self.root / rel).parent.mkdir(parents=True, exist_ok=True)
                (self.root / rel).write_text(self.data[key])


def planted(tc, mutate=None):
    return Planted(tc, mutate).root


NEW_RULES = (judge_runs_on, judge_config_job, judge_no_literal_versions, judge_images_lock,
             judge_requirements, judge_install_requires_hashes, judge_checkout_credentials,
             judge_no_event_in_run, judge_prepare, judge_evals_key_file,
             judge_evals_summary, judge_py_tests_images, judge_yaml_jobs_setup,
             judge_config_pins, judge_fetch_depth, judge_evals_report_dir)

# ---------------------------------------------------------------- tests

class BaselineTest(unittest.TestCase):
    """The planted tree is valid, so a planted failure comes from the planted violation only."""

    def test_good_tree_passes_every_rule(self):
        root = planted(self)
        for fn in (judge_triggers, judge_permissions, judge_pinned_uses,
                   judge_secrets_environment, judge_summary_job,
                   judge_hook_protects_github, judge_badges, *NEW_RULES):
            self.assertEqual(fn(root), [], fn.__name__)
        self.assertEqual(judge_steps_have_jobs(root, STEPS), [])
        self.assertEqual(judge_gvisor_jobs(root, STEPS), [])

    def test_yaml_on_key_read_as_boolean_is_handled(self):
        root = planted(self)
        text = (root / ".github/workflows/check.yml").read_text()
        self.assertTrue(re.search(r"^'?on'?:", text, re.M), text)
        # PyYAML dumps `on` quoted; the unquoted form parses to True and must work too
        (root / ".github/workflows/check.yml").write_text(text.replace("'on':", "on:"))
        self.assertIn(True, yaml.safe_load((root / ".github/workflows/check.yml").read_text()))
        self.assertEqual(judge_triggers(root), [])


class StepJobsTest(unittest.TestCase):
    def test_real_every_step_has_a_job(self):
        self.assertEqual(judge_steps_have_jobs(REPO, real_steps()), [])

    def test_planted_step_without_job_fails(self):
        def m(d):
            del d["check.yml"]["jobs"]["beta"]
            d["check.yml"]["jobs"]["summary"]["needs"].remove("beta")
        v = judge_steps_have_jobs(planted(self, m), STEPS)
        self.assertEqual(len(v), 1, v)
        self.assertIn("beta", v[0])

    def test_planted_job_running_another_step_fails(self):
        def m(d):
            d["check.yml"]["jobs"]["beta"]["steps"][1]["run"] = "scripts/check.sh --only alpha"
        v = judge_steps_have_jobs(planted(self, m), STEPS)
        self.assertEqual(len(v), 1, v)
        self.assertIn("beta", v[0])

    def test_planted_prefix_of_step_name_does_not_count(self):
        def m(d):
            d["check.yml"]["jobs"]["beta"]["steps"][1]["run"] = "scripts/check.sh --only betamax"
        self.assertTrue(judge_steps_have_jobs(planted(self, m), STEPS))


class TriggersTest(unittest.TestCase):
    def test_real_triggers(self):
        self.assertEqual(judge_triggers(REPO), [])

    def test_planted_pull_request_fails(self):
        def m(d):
            d["check.yml"]["on"]["pull_request"] = None
        v = judge_triggers(planted(self, m))
        self.assertTrue(any("pull_request" in x for x in v), v)

    def test_planted_pull_request_target_in_evals_fails(self):
        def m(d):
            d["evals.yml"]["on"]["pull_request_target"] = None
        v = judge_triggers(planted(self, m))
        self.assertTrue(any("pull_request_target" in x for x in v), v)

    def test_planted_list_form_pull_request_fails(self):
        def m(d):
            d["check.yml"]["on"] = ["push", "workflow_dispatch", "pull_request"]
        self.assertTrue(judge_triggers(planted(self, m)))

    def test_planted_check_without_push_fails(self):
        def m(d):
            del d["check.yml"]["on"]["push"]
        self.assertTrue(judge_triggers(planted(self, m)))

    def test_planted_evals_push_trigger_fails(self):
        for push in (None, {"branches": ["main"], "paths": ["src/agents/**"]}):
            with self.subTest(push):
                def m(d, push=push):
                    d["evals.yml"]["on"]["push"] = push
                self.assertTrue(judge_triggers(planted(self, m)))

    def test_planted_evals_without_dispatch_fails(self):
        def m(d):
            del d["evals.yml"]["on"]["workflow_dispatch"]
        self.assertTrue(judge_triggers(planted(self, m)))


class PermissionsTest(unittest.TestCase):
    def test_real_permissions(self):
        self.assertEqual(judge_permissions(REPO), [])

    def test_planted_missing_top_level_fails(self):
        def m(d):
            del d["check.yml"]["permissions"]
        self.assertTrue(judge_permissions(planted(self, m)))

    def test_planted_write_all_fails(self):
        def m(d):
            d["evals.yml"]["permissions"] = "write-all"
        self.assertTrue(judge_permissions(planted(self, m)))

    def test_planted_extra_permission_fails(self):
        def m(d):
            d["check.yml"]["permissions"]["packages"] = "write"
        self.assertTrue(judge_permissions(planted(self, m)))

    def test_planted_job_widens_fails(self):
        def m(d):
            d["check.yml"]["jobs"]["alpha"]["permissions"] = {"contents": "write"}
        v = judge_permissions(planted(self, m))
        self.assertTrue(any("alpha" in x for x in v), v)

    def test_evals_config_actions_read_passes(self):
        def m(d):
            d["evals.yml"]["jobs"]["config"]["permissions"] = {"contents": "read", "actions": "read"}
        self.assertEqual(judge_permissions(planted(self, m)), [])

    def test_check_config_and_evals_current_actions_read_pass(self):
        for jn in ("config", "evals-current"):
            with self.subTest(jn):
                def m(d, jn=jn):
                    js = d["check.yml"]["jobs"]
                    js.setdefault(jn, good_job("python3 scripts/ci-evals-select.py"))
                    js[jn]["permissions"] = {"contents": "read", "actions": "read"}
                self.assertEqual(judge_permissions(planted(self, m)), [])

    def test_planted_other_permission_on_check_config_and_evals_current_fails(self):
        for jn in ("config", "evals-current"):
            for perms in ({"contents": "read", "actions": "write"},
                          {"contents": "read", "actions": "read", "packages": "read"},
                          {"actions": "read"}, {"contents": "write", "actions": "read"}):
                with self.subTest(f"{jn} {perms}"):
                    def m(d, jn=jn, perms=perms):
                        js = d["check.yml"]["jobs"]
                        js.setdefault(jn, good_job("python3 scripts/ci-evals-select.py"))
                        js[jn]["permissions"] = perms
                    v = judge_permissions(planted(self, m))
                    self.assertTrue(any(jn in x for x in v), v)

    def test_planted_actions_read_on_another_job_fails(self):
        for wf, jn in (("evals.yml", "evals"), ("check.yml", "alpha")):
            with self.subTest(f"{wf} {jn}"):
                def m(d, wf=wf, jn=jn):
                    d[wf]["jobs"][jn]["permissions"] = {"contents": "read", "actions": "read"}
                v = judge_permissions(planted(self, m))
                self.assertTrue(any(jn in x for x in v), v)

    def test_planted_other_permission_on_evals_config_fails(self):
        for perms in ({"contents": "read", "actions": "write"},
                      {"contents": "read", "actions": "read", "packages": "read"},
                      {"actions": "read"}, {"contents": "write", "actions": "read"}):
            with self.subTest(perms):
                def m(d, perms=perms):
                    d["evals.yml"]["jobs"]["config"]["permissions"] = perms
                v = judge_permissions(planted(self, m))
                self.assertTrue(any("config" in x for x in v), v)

    def test_planted_actions_read_at_top_level_fails(self):
        def m(d):
            d["evals.yml"]["permissions"] = {"contents": "read", "actions": "read"}
        self.assertTrue(judge_permissions(planted(self, m)))


class PinnedUsesTest(unittest.TestCase):
    def test_real_uses_are_pinned(self):
        self.assertEqual(judge_pinned_uses(REPO), [])

    def _plant(self, ref):
        def m(d):
            d["check.yml"]["jobs"]["alpha"]["steps"][0]["uses"] = ref
        return judge_pinned_uses(planted(self, m))

    def test_planted_tag_fails(self):
        self.assertTrue(self._plant("actions/checkout@v4"))

    def test_planted_branch_fails(self):
        self.assertTrue(self._plant("actions/checkout@main"))

    def test_planted_short_hash_fails(self):
        self.assertTrue(self._plant("actions/checkout@" + "a" * 39))

    def test_planted_uppercase_hash_fails(self):
        self.assertTrue(self._plant("actions/checkout@" + "A" * 40))

    def test_planted_docker_uses_fails(self):
        self.assertTrue(self._plant("docker://alpine:3"))

    def test_planted_unpinned_in_evals_fails(self):
        def m(d):
            d["evals.yml"]["jobs"]["evals"]["steps"][0]["uses"] = "actions/checkout@v4"
        self.assertTrue(judge_pinned_uses(planted(self, m)))

    def test_path_action_and_local_action_pass(self):
        self.assertEqual(self._plant(f"github/codeql-action/init@{HASH}"), [])
        self.assertEqual(self._plant("./.github/actions/x"), [])


class SecretsEnvironmentTest(unittest.TestCase):
    def test_real_secrets_and_environment_confined(self):
        self.assertEqual(judge_secrets_environment(REPO), [])

    def test_planted_secret_in_check_job_fails(self):
        def m(d):
            d["check.yml"]["jobs"]["alpha"]["steps"][1]["env"] = {"K": "${{ secrets.ANTHROPIC_API_KEY }}"}
        v = judge_secrets_environment(planted(self, m))
        self.assertTrue(any("alpha" in x and "secrets" in x for x in v), v)

    def test_planted_github_token_secret_in_check_fails(self):
        def m(d):
            d["check.yml"]["jobs"]["summary"]["steps"][0]["run"] = "echo ${{ secrets.GITHUB_TOKEN }}"
        self.assertTrue(judge_secrets_environment(planted(self, m)))

    def test_planted_environment_in_check_job_fails(self):
        def m(d):
            d["check.yml"]["jobs"]["beta"]["environment"] = "dev"
        v = judge_secrets_environment(planted(self, m))
        self.assertTrue(any("beta" in x and "environment" in x for x in v), v)

    def test_planted_secret_in_second_evals_job_fails(self):
        def m(d):
            d["evals.yml"]["jobs"]["other"] = {"runs-on": "ubuntu-24.04", "environment": "dev",
                                               "steps": [{"run": "echo ${{ secrets.X }}"}]}
        v = judge_secrets_environment(planted(self, m))
        self.assertTrue(any("other" in x for x in v), v)

    def test_planted_workflow_level_secret_fails(self):
        def m(d):
            d["check.yml"]["env"] = {"K": "${{ secrets.X }}"}
        self.assertTrue(judge_secrets_environment(planted(self, m)))

    def test_planted_evals_job_without_environment_dev_fails(self):
        def m(d):
            del d["evals.yml"]["jobs"]["evals"]["environment"]
        self.assertTrue(judge_secrets_environment(planted(self, m)))

    def test_planted_evals_job_with_other_environment_fails(self):
        def m(d):
            d["evals.yml"]["jobs"]["evals"]["environment"] = "prod"
        self.assertTrue(judge_secrets_environment(planted(self, m)))

    def test_environment_given_as_mapping_passes(self):
        def m(d):
            d["evals.yml"]["jobs"]["evals"]["environment"] = {"name": "dev"}
        self.assertEqual(judge_secrets_environment(planted(self, m)), [])


class SummaryJobTest(unittest.TestCase):
    def test_real_summary_job(self):
        self.assertEqual(judge_summary_job(REPO), [])

    def test_planted_missing_if_always_fails(self):
        def m(d):
            del d["check.yml"]["jobs"]["summary"]["if"]
        self.assertTrue(judge_summary_job(planted(self, m)))

    def test_planted_missing_need_fails(self):
        def m(d):
            d["check.yml"]["jobs"]["summary"]["needs"].remove("alpha")
        self.assertTrue(judge_summary_job(planted(self, m)))

    def test_planted_job_added_after_summary_fails(self):
        def m(d):
            d["check.yml"]["jobs"]["late"] = {"runs-on": "ubuntu-24.04", "steps": [{"run": "true"}]}
        self.assertTrue(judge_summary_job(planted(self, m)))

    def test_planted_no_summary_job_fails(self):
        def m(d):
            del d["check.yml"]["jobs"]["summary"]
        self.assertTrue(judge_summary_job(planted(self, m)))

    def test_planted_success_condition_fails(self):
        def m(d):
            d["check.yml"]["jobs"]["summary"]["if"] = "success()"
        self.assertTrue(judge_summary_job(planted(self, m)))

    def test_expression_form_of_always_passes(self):
        def m(d):
            d["check.yml"]["jobs"]["summary"]["if"] = "${{ always() }}"
        self.assertEqual(judge_summary_job(planted(self, m)), [])


class HookTest(unittest.TestCase):
    def test_real_hook_protects_github(self):
        self.assertEqual(judge_hook_protects_github(REPO), [])

    def test_planted_hook_without_github_fails(self):
        def m(d):
            d["hook"] = "protected='^(scripts/|src/deploy/)'\n"
        self.assertTrue(judge_hook_protects_github(planted(self, m)))

    def test_planted_github_only_in_a_comment_fails(self):
        def m(d):
            d["hook"] = "# ^\\.github/ is protected\nprotected='^(scripts/)'\n"
        self.assertTrue(judge_hook_protects_github(planted(self, m)))

    def test_planted_unanchored_github_fails(self):
        def m(d):
            d["hook"] = "protected='^(scripts/)|(^|/)\\.github/'\n"
        self.assertTrue(judge_hook_protects_github(planted(self, m)))

    def test_anchored_github_inside_group_passes(self):
        def m(d):
            d["hook"] = "protected='^(scripts/|\\.github/)'\n"
        self.assertEqual(judge_hook_protects_github(planted(self, m)), [])


class BadgeTest(unittest.TestCase):
    def test_real_readme_badges(self):
        self.assertEqual(judge_badges(REPO), [])

    def test_planted_each_missing_badge_fails(self):
        for n in ("check.yml", "evals.yml"):
            with self.subTest(workflow=n):
                def m(d, n=n):
                    d["readme"] = d["readme"].replace(f"{SLUG}/{n}/badge.svg", "x")
                v = judge_badges(planted(self, m))
                self.assertTrue(any(n in x for x in v), v)

    def test_planted_wrong_repository_fails(self):
        def m(d):
            d["readme"] = d["readme"].replace("Scrollout", "someone")
        self.assertTrue(judge_badges(planted(self, m)))


def job(d, name="alpha", wf="check.yml"):
    return d[wf]["jobs"][name]


class RunsOnTest(unittest.TestCase):
    def test_real_runs_on(self):
        self.assertEqual(judge_runs_on(REPO), [])

    def test_planted_literal_label_fails(self):
        def m(d):
            job(d)["runs-on"] = "ubuntu-26.04"
        v = judge_runs_on(planted(self, m))
        self.assertTrue(any("alpha" in x for x in v), v)

    def test_planted_latest_on_a_step_job_fails(self):
        def m(d):
            job(d, "beta")["runs-on"] = "ubuntu-latest"
        self.assertTrue(judge_runs_on(planted(self, m)))

    def test_planted_config_on_the_runner_output_fails(self):
        def m(d):
            job(d, "config")["runs-on"] = RUNNER
        self.assertTrue(judge_runs_on(planted(self, m)))

    def test_planted_summary_on_a_literal_fails(self):
        def m(d):
            job(d, "summary")["runs-on"] = "ubuntu-24.04"
        self.assertTrue(judge_runs_on(planted(self, m)))

    def test_planted_job_without_needs_config_fails(self):
        def m(d):
            del job(d, "beta")["needs"]
        self.assertTrue(judge_runs_on(planted(self, m)))

    def test_planted_evals_job_on_a_literal_fails(self):
        def m(d):
            job(d, "evals", "evals.yml")["runs-on"] = "ubuntu-latest"
        self.assertTrue(judge_runs_on(planted(self, m)))

    def test_planted_no_config_job_fails(self):
        def m(d):
            del d["check.yml"]["jobs"]["config"]
        self.assertTrue(judge_runs_on(planted(self, m)))
        self.assertTrue(judge_config_job(planted(self, m)))

    def test_planted_config_without_runner_output_fails(self):
        def m(d):
            job(d, "config")["outputs"] = {}
        self.assertTrue(judge_config_job(planted(self, m)))

    def test_planted_config_not_reading_images_lock_fails(self):
        def m(d):
            job(d, "config")["steps"][1]["run"] = "echo runner=x >> \"$GITHUB_OUTPUT\""
        self.assertTrue(judge_config_job(planted(self, m)))

    def test_real_config_job(self):
        self.assertEqual(judge_config_job(REPO), [])


class NoLiteralVersionsTest(unittest.TestCase):
    def test_real_workflows_have_no_literal(self):
        self.assertEqual(judge_no_literal_versions(REPO), [])

    def _plant(self, key, val, where="beta"):
        def m(d):
            job(d, where)["steps"].append({"uses": f"actions/setup-python@{HASH}",
                                           "with": {key: val}})
        return judge_no_literal_versions(planted(self, m))

    def test_planted_python_version_fails(self):
        self.assertTrue(self._plant("python-version", "3.13"))

    def test_planted_python_version_number_fails(self):
        self.assertTrue(self._plant("python-version", 3.13))

    def test_planted_go_version_fails(self):
        self.assertTrue(self._plant("go-version", "1.26.11"))

    def test_config_output_passes(self):
        self.assertEqual(self._plant("python-version", "${{ needs.config.outputs.python }}"), [])

    def test_planted_pyyaml_version_in_run_fails(self):
        def m(d):
            job(d, "beta")["steps"].append({"run": "pip install PyYAML==6.0.3"})
        self.assertTrue(judge_no_literal_versions(planted(self, m)))

    def test_planted_ubuntu_label_in_env_fails(self):
        def m(d):
            job(d, "beta")["steps"][1]["env"] = {"RUNNER": "ubuntu-24.04"}
        self.assertTrue(judge_no_literal_versions(planted(self, m)))

    def test_planted_literal_in_evals_fails(self):
        def m(d):
            job(d, "evals", "evals.yml")["steps"].append(
                {"uses": f"actions/setup-go@{HASH}", "with": {"go-version": "1.26"}})
        self.assertTrue(judge_no_literal_versions(planted(self, m)))

    def test_planted_golang_image_literal_fails(self):
        def m(d):
            job(d, "beta")["steps"].append({"run": "docker run golang:1.26 go version"})
        self.assertTrue(judge_no_literal_versions(planted(self, m)))


class ImagesLockTest(unittest.TestCase):
    def test_real_images_lock_has_the_ci_pins(self):
        self.assertEqual(judge_images_lock(REPO), [])

    def test_planted_each_missing_pin_fails(self):
        for k in ("ci-runner", "ci-python", "gvisor-release", "gvisor-sha512"):
            with self.subTest(k):
                def m(d, k=k):
                    d["lock"] = "".join(l + "\n" for l in GOOD_LOCK.splitlines()
                                        if not l.startswith(k + " "))
                v = judge_images_lock(planted(self, m))
                self.assertTrue(any(k in x for x in v), v)

    def test_planted_no_lock_fails(self):
        def m(d):
            d["lock"] = None
        self.assertTrue(judge_images_lock(planted(self, m)))

    def test_planted_short_checksum_fails(self):
        def m(d):
            d["lock"] = GOOD_LOCK.replace("a" * 128, "a" * 64)
        self.assertTrue(judge_images_lock(planted(self, m)))

    def test_planted_runner_not_a_label_fails(self):
        def m(d):
            d["lock"] = GOOD_LOCK.replace("ubuntu-26.04", "latest")
        self.assertTrue(judge_images_lock(planted(self, m)))


class RequirementsTest(unittest.TestCase):
    def test_real_requirements_pin_pyyaml_with_a_hash(self):
        self.assertEqual(judge_requirements(REPO), [])

    def test_planted_no_file_fails(self):
        def m(d):
            d["reqs"] = None
        self.assertTrue(judge_requirements(planted(self, m)))

    def test_planted_no_hash_fails(self):
        def m(d):
            d["reqs"] = "pyyaml==6.0.3\n"
        self.assertTrue(judge_requirements(planted(self, m)))

    def test_planted_md5_hash_fails(self):
        def m(d):
            d["reqs"] = "pyyaml==6.0.3 --hash=md5:" + "b" * 32 + "\n"
        self.assertTrue(judge_requirements(planted(self, m)))

    def test_planted_range_not_a_pin_fails(self):
        def m(d):
            d["reqs"] = "pyyaml>=6 --hash=sha256:" + "b" * 64 + "\n"
        self.assertTrue(judge_requirements(planted(self, m)))

    def test_real_install_uses_require_hashes(self):
        self.assertEqual(judge_install_requires_hashes(REPO), [])

    def test_planted_install_without_require_hashes_fails(self):
        def m(d):
            job(d)["steps"][-1]["run"] = "pip install -r scripts/tools/requirements-ci.txt"
        self.assertTrue(judge_install_requires_hashes(planted(self, m)))

    def test_planted_install_of_a_bare_package_fails(self):
        def m(d):
            job(d, "beta")["steps"].append({"run": "pip install pyyaml"})
        self.assertTrue(judge_install_requires_hashes(planted(self, m)))

    def test_planted_no_install_at_all_fails(self):
        def m(d):
            for j in d["check.yml"]["jobs"].values():
                j["steps"] = [x for x in j["steps"] if x.get("run") != PIP]
        self.assertTrue(judge_install_requires_hashes(planted(self, m)))


class CheckoutTest(unittest.TestCase):
    def test_real_checkouts_do_not_persist_credentials(self):
        self.assertEqual(judge_checkout_credentials(REPO), [])

    def test_planted_missing_with_fails(self):
        def m(d):
            del job(d)["steps"][0]["with"]
        self.assertTrue(judge_checkout_credentials(planted(self, m)))

    def test_planted_true_fails(self):
        def m(d):
            job(d)["steps"][0]["with"]["persist-credentials"] = True
        self.assertTrue(judge_checkout_credentials(planted(self, m)))

    def test_planted_in_evals_fails(self):
        def m(d):
            job(d, "evals", "evals.yml")["steps"][0]["with"] = {"fetch-depth": 1}
        self.assertTrue(judge_checkout_credentials(planted(self, m)))

    def test_string_false_passes(self):
        def m(d):
            job(d)["steps"][0]["with"]["persist-credentials"] = "false"
        self.assertEqual(judge_checkout_credentials(planted(self, m)), [])


class SecretFormsTest(unittest.TestCase):
    FORMS = ["${{ secrets.X }}", "${{ secrets['X'] }}", '${{ secrets["X"] }}',
             "${{ toJSON(secrets) }}", "${{ toJson( secrets ) }}",
             "${{ format('{0}', secrets.X) }}"]

    def test_planted_each_form_in_env_fails(self):
        for f in self.FORMS:
            with self.subTest(f):
                def m(d, f=f):
                    job(d)["steps"][1]["env"] = {"K": f}
                v = judge_secrets_environment(planted(self, m))
                self.assertTrue(any("alpha" in x for x in v), v)

    def test_planted_each_form_in_run_fails(self):
        for f in self.FORMS:
            with self.subTest(f):
                def m(d, f=f):
                    job(d, "beta")["steps"][1]["run"] = f"echo {f}"
                self.assertTrue(judge_secrets_environment(planted(self, m)))

    def test_planted_form_in_if_fails(self):
        def m(d):
            job(d)["steps"][1]["if"] = "toJSON(secrets) != ''"
        self.assertTrue(judge_secrets_environment(planted(self, m)))

    def test_planted_reusable_workflow_secrets_inherit_fails(self):
        def m(d):
            d["check.yml"]["jobs"]["call"] = {"uses": "./.github/workflows/x.yml",
                                              "secrets": "inherit"}
        v = judge_secrets_environment(planted(self, m))
        self.assertTrue(any("call" in x for x in v), v)

    def test_planted_reusable_workflow_secrets_mapping_fails(self):
        def m(d):
            d["check.yml"]["jobs"]["call"] = {"uses": "./.github/workflows/x.yml",
                                              "secrets": {"K": "literal"}}
        self.assertTrue(judge_secrets_environment(planted(self, m)))

    def test_planted_form_at_workflow_level_fails(self):
        def m(d):
            d["check.yml"]["env"] = {"ALL": "${{ toJSON(secrets) }}"}
        self.assertTrue(judge_secrets_environment(planted(self, m)))

    def test_forms_in_the_evals_job_pass(self):
        def m(d):
            job(d, "evals", "evals.yml")["steps"][1]["env"] = {"K": "${{ secrets['ANTHROPIC_API_KEY'] }}"}
        self.assertEqual(judge_secrets_environment(planted(self, m)), [])

    def test_the_word_in_a_plain_string_passes(self):
        def m(d):
            job(d)["steps"][1]["run"] = "echo 'no secrets here' ; echo ${{ github.sha }}"
        self.assertEqual(judge_secrets_environment(planted(self, m)), [])


class NoEventInRunTest(unittest.TestCase):
    def test_real_runs_hold_no_event_expression(self):
        self.assertEqual(judge_no_event_in_run(REPO), [])

    def test_planted_expressions_fail(self):
        for e in ("${{ github.event.head_commit.message }}", "${{ github.head_ref }}",
                  "${{ github.event.inputs.x }}", "${{ toJSON(github.event) }}",
                  "${{ github['event']['ref'] }}",
                  "${{ github.event.pull_request.title || 'x' }}"):
            with self.subTest(e):
                def m(d, e=e):
                    job(d)["steps"][1]["run"] = f"echo {e}"
                self.assertTrue(judge_no_event_in_run(planted(self, m)))

    def test_planted_in_evals_fails(self):
        def m(d):
            job(d, "evals", "evals.yml")["steps"][2]["run"] = "echo ${{ github.head_ref }}"
        self.assertTrue(judge_no_event_in_run(planted(self, m)))

    def test_env_form_passes(self):
        def m(d):
            job(d)["steps"][1]["env"] = {"MSG": "${{ github.event.head_commit.message }}"}
            job(d)["steps"][1]["run"] = 'echo "$MSG" ${{ github.event_name }} ${{ github.sha }}'
        self.assertEqual(judge_no_event_in_run(planted(self, m)), [])


class GvisorJobsTest(unittest.TestCase):
    def test_real_jobs_run_gvisor_first(self):
        self.assertEqual(judge_gvisor_jobs(REPO, real_steps()), [])

    def test_planted_gvisor_after_the_suite_fails(self):
        def m(d):
            s = job(d, "ap-planted")["steps"]
            s[1], s[3] = s[3], s[1]
        v = judge_gvisor_jobs(planted(self, m), STEPS)
        self.assertTrue(any("ap-planted" in x for x in v), v)

    def test_planted_no_gvisor_in_us2_fails(self):
        def m(d):
            job(d, "ap-us2")["steps"].pop(1)
        v = judge_gvisor_jobs(planted(self, m), STEPS)
        self.assertTrue(any("ap-us2" in x for x in v), v)

    def test_planted_no_us2_job_fails(self):
        def m(d):
            del d["check.yml"]["jobs"]["ap-us2"]
        self.assertTrue(judge_gvisor_jobs(planted(self, m), STEPS))

    def test_planted_no_planted_job_fails(self):
        def m(d):
            del d["check.yml"]["jobs"]["ap-planted"]
        self.assertTrue(judge_gvisor_jobs(planted(self, m), STEPS))

    def test_one_run_step_with_both_in_order_passes(self):
        def m(d):
            job(d, "ap-us2")["steps"][1:] = [{"run": "scripts/ci-gvisor.sh\nsrc/deploy/ap test us2"}]
        self.assertEqual(judge_gvisor_jobs(planted(self, m), STEPS), [])

    def test_planted_one_run_step_in_wrong_order_fails(self):
        def m(d):
            job(d, "ap-us2")["steps"][1:] = [{"run": "src/deploy/ap test us2\nscripts/ci-gvisor.sh"}]
        self.assertTrue(judge_gvisor_jobs(planted(self, m), STEPS))

    def test_the_e2e_step_does_not_replace_the_two_jobs(self):
        def m(d):
            del d["check.yml"]["jobs"]["ap-us2"]
            del d["check.yml"]["jobs"]["ap-planted"]
        self.assertTrue(judge_gvisor_jobs(planted(self, m), STEPS + ["e2e"]))


class PrepareTest(unittest.TestCase):
    def test_real_jobs_prepare_first(self):
        self.assertEqual(judge_prepare(REPO), [])

    def test_planted_each_job_without_prepare_fails(self):
        for n in ("docker", "deps", "ap-planted", "ap-us2", "py-tests"):
            with self.subTest(n):
                def m(d, n=n):
                    s = job(d, n)["steps"]
                    s[:] = [x for x in s if "--prepare" not in str(x.get("run", ""))]
                v = judge_prepare(planted(self, m))
                self.assertTrue(any(n in x for x in v), v)

    def test_planted_prepare_after_the_command_fails(self):
        def m(d):
            s = job(d, "docker")["steps"]
            s[2], s[3] = s[3], s[2]
        self.assertTrue(judge_prepare(planted(self, m)))

    def test_planted_combined_only_list_needs_prepare(self):
        def m(d):
            job(d, "beta")["steps"][1]["run"] = "scripts/check.sh --only go,docker"
        self.assertTrue(judge_prepare(planted(self, m)))

    def test_other_jobs_need_no_prepare(self):
        self.assertEqual(judge_prepare(planted(self)), [])


class EvalsHardeningTest(unittest.TestCase):
    def test_real_key_file(self):
        self.assertEqual(judge_evals_key_file(REPO), [])

    def test_planted_no_umask_fails(self):
        def m(d):
            s = job(d, "evals", "evals.yml")["steps"][1]
            s["run"] = s["run"].replace("umask 077\n", "")
        self.assertTrue(judge_evals_key_file(planted(self, m)))

    def test_planted_other_umask_fails(self):
        def m(d):
            s = job(d, "evals", "evals.yml")["steps"][1]
            s["run"] = s["run"].replace("umask 077", "umask 022")
        self.assertTrue(judge_evals_key_file(planted(self, m)))

    def test_planted_removal_without_always_fails(self):
        def m(d):
            del job(d, "evals", "evals.yml")["steps"][-1]["if"]
        self.assertTrue(judge_evals_key_file(planted(self, m)))

    def test_planted_removal_on_success_only_fails(self):
        def m(d):
            job(d, "evals", "evals.yml")["steps"][-1]["if"] = "success()"
        self.assertTrue(judge_evals_key_file(planted(self, m)))

    def test_planted_no_removal_fails(self):
        def m(d):
            job(d, "evals", "evals.yml")["steps"].pop()
        self.assertTrue(judge_evals_key_file(planted(self, m)))

    def test_planted_removal_before_the_write_fails(self):
        def m(d):
            s = job(d, "evals", "evals.yml")["steps"]
            s.insert(1, s.pop())
        self.assertTrue(judge_evals_key_file(planted(self, m)))

    def test_expression_form_of_always_passes(self):
        def m(d):
            job(d, "evals", "evals.yml")["steps"][-1]["if"] = "${{ always() }}"
        self.assertEqual(judge_evals_key_file(planted(self, m)), [])

    def test_real_summary(self):
        self.assertEqual(judge_evals_summary(REPO), [])

    def test_planted_no_summary_script_fails(self):
        def m(d):
            s = job(d, "evals", "evals.yml")["steps"]
            s[:] = [x for x in s if "ci-evals-summary" not in str(x.get("run", ""))]
        self.assertTrue(judge_evals_summary(planted(self, m)))


def py_steps(d, name="py-tests"):
    return job(d, name)["steps"]


class PyTestsImagesTest(unittest.TestCase):
    def test_real_py_tests_and_docker_jobs(self):
        self.assertEqual(judge_py_tests_images(REPO), [])

    def test_planted_no_gvisor_fails(self):
        def m(d):
            s = py_steps(d)
            s[:] = [x for x in s if "ci-gvisor" not in str(x.get("run", ""))]
        self.assertTrue(judge_py_tests_images(planted(self, m)))

    def test_planted_each_missing_image_build_fails(self):
        for img in ("check", "agent"):
            with self.subTest(img):
                def m(d, img=img):
                    s = py_steps(d)
                    s[:] = [x for x in s if f"build.sh {img}" not in str(x.get("run", ""))]
                v = judge_py_tests_images(planted(self, m))
                self.assertTrue(any(img in x for x in v), v)

    def test_planted_builds_after_the_step_fail(self):
        def m(d):
            s = py_steps(d)
            s.append(s.pop(next(i for i, x in enumerate(s) if "only py-tests" in str(x.get("run", "")))))
            s.reverse()
        self.assertTrue(judge_py_tests_images(planted(self, m)))

    def test_planted_no_cosign_password_fails(self):
        def m(d):
            s = py_steps(d)
            s[:] = [x for x in s if "COSIGN_PASSWORD" not in str(x.get("run", ""))]
        self.assertTrue(judge_py_tests_images(planted(self, m)))

    def test_planted_cosign_password_from_a_secret_fails(self):
        def m(d):
            py_steps(d)[-1]["env"] = {"COSIGN_PASSWORD": "${{ secrets.COSIGN_PASSWORD }}"}
        self.assertTrue(judge_py_tests_images(planted(self, m)))

    def test_planted_docker_job_without_gvisor_fails(self):
        def m(d):
            s = job(d, "docker")["steps"]
            s[:] = [x for x in s if "ci-gvisor" not in str(x.get("run", ""))]
        v = judge_py_tests_images(planted(self, m))
        self.assertTrue(any("docker" in x for x in v), v)

    def test_planted_no_py_tests_job_fails(self):
        def m(d):
            del d["check.yml"]["jobs"]["py-tests"]
        self.assertTrue(judge_py_tests_images(planted(self, m)))

    def test_job_named_with_an_underscore_key_is_found(self):
        def m(d):
            jobs_ = d["check.yml"]["jobs"]
            d["check.yml"]["jobs"] = {("py_tests" if k == "py-tests" else k): v for k, v in jobs_.items()}
            d["check.yml"]["jobs"]["summary"]["needs"] = [("py_tests" if n == "py-tests" else n)
                                                          for n in d["check.yml"]["jobs"]["summary"]["needs"]]
        self.assertEqual(judge_py_tests_images(planted(self, m)), [])


class YamlJobsSetupTest(unittest.TestCase):
    def test_real_jobs(self):
        self.assertEqual(judge_yaml_jobs_setup(REPO), [])

    def test_planted_each_job_without_setup_python_fails(self):
        for n in ("py-tests", "knowledge"):
            with self.subTest(n):
                def m(d, n=n):
                    s = py_steps(d, n)
                    s[:] = [x for x in s if "uses" not in x or "checkout" in x["uses"]]
                v = judge_yaml_jobs_setup(planted(self, m))
                self.assertTrue(any(n in x for x in v), v)

    def test_planted_each_job_without_the_hashed_install_fails(self):
        for n in ("py-tests", "knowledge"):
            with self.subTest(n):
                def m(d, n=n):
                    s = py_steps(d, n)
                    s[:] = [x for x in s if x.get("run") != PIP]
                v = judge_yaml_jobs_setup(planted(self, m))
                self.assertTrue(any(n in x for x in v), v)

    def test_planted_install_without_require_hashes_fails(self):
        def m(d):
            for x in py_steps(d, "knowledge"):
                if x.get("run") == PIP:
                    x["run"] = "python3 -m pip install -r scripts/tools/requirements-ci.txt"
        self.assertTrue(judge_yaml_jobs_setup(planted(self, m)))

    def test_planted_install_after_the_step_fails(self):
        def m(d):
            s = py_steps(d, "knowledge")
            s.append(s.pop(2))
        self.assertTrue(judge_yaml_jobs_setup(planted(self, m)))

    def test_planted_literal_python_version_fails(self):
        def m(d):
            py_steps(d, "py-tests")[1]["with"]["python-version"] = "3.14"
        self.assertTrue(judge_yaml_jobs_setup(planted(self, m)))


class ConfigPinsTest(unittest.TestCase):
    def test_real_config_job_outputs_and_checks(self):
        self.assertEqual(judge_config_pins(REPO), [])

    def test_planted_each_missing_output_fails(self):
        for k in ("runner", "python", "go"):
            with self.subTest(k):
                def m(d, k=k):
                    del job(d, "config")["outputs"][k]
                v = judge_config_pins(planted(self, m))
                self.assertTrue(any(k in x for x in v), v)

    def test_planted_config_without_the_empty_check_fails(self):
        def m(d):
            job(d, "config")["steps"][1]["run"] = "\n".join(
                l for l in CONFIG_RUN.splitlines() if "-n" not in l)
        self.assertTrue(judge_config_pins(planted(self, m)))

    def test_planted_config_checking_only_one_value_fails(self):
        def m(d):
            job(d, "config")["steps"][1]["run"] = "\n".join(
                l for l in CONFIG_RUN.splitlines() if "-n" not in l or "$runner" in l)
        self.assertTrue(judge_config_pins(planted(self, m)))

    def test_planted_evals_config_without_the_check_fails(self):
        def m(d):
            job(d, "config", "evals.yml")["steps"][1]["run"] = "echo runner=x >> $GITHUB_OUTPUT"
        self.assertTrue(judge_config_pins(planted(self, m)))

    def test_test_form_and_a_loop_pass(self):
        def m(d):
            job(d, "config")["steps"][1]["run"] = (
                "runner=a python=b go=c\nfor v in runner python go; do\n"
                "  eval \"x=\\$$v\"; test -n \"$x\" || exit 1\ndone\n")
        self.assertEqual(judge_config_pins(planted(self, m)), [])


class FetchDepthTest(unittest.TestCase):
    def test_real_fetch_depth(self):
        self.assertEqual(judge_fetch_depth(REPO), [])

    def test_planted_deep_checkout_in_another_job_fails(self):
        def m(d):
            job(d, "beta")["steps"][0]["with"]["fetch-depth"] = 0
        v = judge_fetch_depth(planted(self, m))
        self.assertTrue(any("beta" in x for x in v), v)

    def test_planted_deep_checkout_in_evals_fails(self):
        def m(d):
            job(d, "evals", "evals.yml")["steps"][0]["with"]["fetch-depth"] = 0
        self.assertTrue(judge_fetch_depth(planted(self, m)))

    def test_planted_each_history_job_shallow_fails(self):
        for n in ("contract-diff", "py-tests"):
            with self.subTest(n):
                def m(d, n=n):
                    del job(d, n)["steps"][0]["with"]["fetch-depth"]
                v = judge_fetch_depth(planted(self, m))
                self.assertTrue(any(n in x for x in v), v)

    def test_shallow_depth_one_passes(self):
        def m(d):
            job(d, "beta")["steps"][0]["with"]["fetch-depth"] = 1
        self.assertEqual(judge_fetch_depth(planted(self, m)), [])


class EvalsReportDirTest(unittest.TestCase):
    def test_real_report_dir(self):
        self.assertEqual(judge_evals_report_dir(REPO), [])

    def test_planted_not_set_at_job_level_fails(self):
        def m(d):
            del job(d, "evals", "evals.yml")["env"]
        self.assertTrue(judge_evals_report_dir(planted(self, m)))

    def test_planted_set_again_on_a_step_fails(self):
        def m(d):
            job(d, "evals", "evals.yml")["steps"][2]["env"] = {"EVAL_REPORT_DIR": "/x"}
        self.assertTrue(judge_evals_report_dir(planted(self, m)))

    def test_planted_summary_reads_a_literal_path_fails(self):
        def m(d):
            for x in job(d, "evals", "evals.yml")["steps"]:
                if "ci-evals-summary" in str(x.get("run", "")):
                    x["run"] = 'python3 scripts/ci-evals-summary.py "$RUNNER_TEMP/eval-report/report.json"'
        self.assertTrue(judge_evals_report_dir(planted(self, m)))

    def test_braced_variable_passes(self):
        def m(d):
            for x in job(d, "evals", "evals.yml")["steps"]:
                if "ci-evals-summary" in str(x.get("run", "")):
                    x["run"] = 'python3 scripts/ci-evals-summary.py "${EVAL_REPORT_DIR}/report.json"'
        self.assertEqual(judge_evals_report_dir(planted(self, m)), [])


class NothingFoundTest(unittest.TestCase):
    def test_tree_without_workflows_fails_every_workflow_rule(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(lambda: __import__("shutil").rmtree(root, ignore_errors=True))
        for fn in (judge_triggers, judge_permissions, judge_pinned_uses,
                   judge_secrets_environment, judge_summary_job, *NEW_RULES):
            self.assertTrue(fn(root), fn.__name__)
        self.assertTrue(judge_steps_have_jobs(root, STEPS))


if __name__ == "__main__":
    unittest.main()
