"""Workflow rules of T120 (research D45, ADR 0048): the evals start only by hand, and check.yml's
keyless evals-current job lists the pending suites.

Rules, each a judge_<rule>(root) over root/.github/workflows, with a test on the real files and a
planted violation in a temporary tree that is valid otherwise (a baseline test proves it valid):

- dispatch_only: evals.yml's only trigger is workflow_dispatch (no push, no other event);
- all_input: workflow_dispatch has the input `all`, type boolean, default false;
- all_through_env: the input reaches the selector only through its step's `env:` (ALL is
  `${{ inputs.all }}` or `${{ github.event.inputs.all }}`); no `run:` in evals.yml holds an
  expression naming `inputs`;
- selector_base: in evals.yml's config job and in check.yml's evals-current job, the step that
  runs scripts/ci-evals-select.py gets RUNS (the successful evals runs, listed with `gh api` on
  actions/workflows/evals.yml/runs with status=success and GH_TOKEN `${{ github.token }}`) and no
  BEFORE: the selector, not the workflow, chooses the base run; each job has the history the
  ancestry test needs (checkout fetch-depth 0, or `git fetch --unshallow` before the selector);
- evals_current: check.yml has a job evals-current: no secret, no environment, permissions exactly
  {contents: read, actions: read}; its selector step's env has MODE `pending`, REF
  `${{ github.ref_name }}` and SHA `${{ github.sha }}`; Python with the hashed install before it;
  no `${{ }}` in its runs. It fails only on main: the selector exits 1 there while a suite is
  pending (test_ci_evals_dispatch.py), and nothing hides that exit or skips the job: no `if:` on
  the job, no `continue-on-error`, no `|| true`, `|| :` or `set +e` in the selector's step; and
  check.yml's push trigger has no paths or branches filter, so every push, a merge to main
  included, runs it.

The loading helpers are scripts/tests/test_workflows.py's, imported unchanged.
"""
import copy
import re
import tempfile
import unittest
from pathlib import Path

import yaml

from test_workflows import EXPR, jobs, load, need_files, secret_refs, triggers

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
HASH = "a" * 40
SELECT = re.compile(r"scripts/ci-evals-select\.py")
API = [re.compile(r"\bgh\s+api\b"), re.compile(r"actions/workflows/evals\.yml/runs\b"),
       re.compile(r"\bstatus=success\b")]
TOKEN = re.compile(r"\s*\$\{\{\s*github\.token\s*\}\}\s*")
ALL_ENV = re.compile(r"\s*\$\{\{\s*(?:github\.event\.)?inputs\.all\s*\}\}\s*")
REF_ENV = re.compile(r"\s*\$\{\{\s*github\.ref_name\s*\}\}\s*")
SHA_ENV = re.compile(r"\s*\$\{\{\s*github\.sha\s*\}\}\s*")
SWALLOW = re.compile(r"\|\|\s*(?:true|:)(?:\s|$|;|\))")


def evals_current(w):
    for k, j in jobs(w).items():
        if k.replace("_", "-") == "evals-current" or j.get("name") == "evals-current":
            return k, j
    return None, None


def select_step(job):
    for i, st in enumerate((job or {}).get("steps") or []):
        if SELECT.search(str(st.get("run", ""))):
            return i, st
    return -1, None


# ---------------------------------------------------------------- rules

def judge_dispatch_only(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    t = triggers(wf["evals.yml"])
    if set(t) != {"workflow_dispatch"}:
        v.append(f"evals.yml triggers are {sorted(t)}, not workflow_dispatch alone")
    return v


def judge_all_input(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    d = triggers(wf["evals.yml"]).get("workflow_dispatch") or {}
    inp = ((d.get("inputs") or {}) if isinstance(d, dict) else {}).get("all")
    if not isinstance(inp, dict):
        return ["evals.yml workflow_dispatch has no input all"]
    if inp.get("type") != "boolean":
        v.append(f"input all has type {inp.get('type')!r}, not boolean")
    if inp.get("default") not in (False, "false"):
        v.append(f"input all has default {inp.get('default')!r}, not false")
    return v


def judge_all_through_env(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    w = wf["evals.yml"]
    for jn, job in jobs(w).items():
        for st in job.get("steps") or []:
            for e in EXPR.findall(str(st.get("run", ""))):
                if re.search(r"\binputs\b", e):
                    v.append(f"evals.yml job {jn}: run holds the expression {e.strip()!r}")
    _, st = select_step(jobs(w).get("config"))
    if st is None:
        return v + ["evals.yml config job has no step running scripts/ci-evals-select.py"]
    val = (st.get("env") or {}).get("ALL")
    if val is None or not ALL_ENV.fullmatch(str(val)):
        v.append(f"the selector step's env ALL is {val!r}, not ${{{{ inputs.all }}}}")
    return v


def base_violations(label, job):
    v = []
    i, st = select_step(job)
    if st is None:
        return [f"{label}: no step runs scripts/ci-evals-select.py"]
    env = st.get("env") or {}
    if "BEFORE" in env or re.search(r"\bBEFORE=", str(st.get("run", ""))):
        v.append(f"{label}: the workflow gives the selector BEFORE; the selector chooses the base")
    steps = job.get("steps") or []
    asking = [s for s in steps[:i + 1] if all(p.search(str(s.get("run", ""))) for p in API)]
    if "RUNS" not in env and not re.search(r"\bRUNS=", str(st.get("run", ""))):
        v.append(f"{label}: the selector step gets no RUNS")
    if not asking:
        v.append(f"{label}: no step up to the selector lists evals.yml runs with gh api and status=success")
    for s in asking:
        tok = (s.get("env") or {}).get("GH_TOKEN")
        if tok is None or not TOKEN.fullmatch(str(tok)):
            v.append(f"{label}: GH_TOKEN is {tok!r}, not ${{{{ github.token }}}}")
    deep = any(str(s.get("uses", "")).startswith("actions/checkout@")
               and str((s.get("with") or {}).get("fetch-depth")) == "0" for s in steps[:i])
    unshallow = any(re.search(r"\bgit\s+fetch\b[^\n]*--unshallow", str(s.get("run", "")))
                    for s in steps[:i + 1])
    if not (deep or unshallow):
        v.append(f"{label}: no full history before the selector (fetch-depth 0 or git fetch --unshallow)")
    return v


def judge_selector_base(root):
    wf = load(root)
    v = need_files(wf, "check.yml", "evals.yml")
    if v:
        return v
    v += base_violations("evals.yml config", jobs(wf["evals.yml"]).get("config") or {})
    _, ec = evals_current(wf["check.yml"])
    if ec is None:
        return v + ["check.yml has no job evals-current"]
    return v + base_violations("check.yml evals-current", ec)


def judge_evals_current(root):
    wf = load(root)
    v = need_files(wf, "check.yml")
    if v:
        return v
    w = wf["check.yml"]
    jn, job = evals_current(w)
    if job is None:
        return ["check.yml has no job evals-current"]
    if secret_refs(job):
        v.append(f"evals-current references secrets: {secret_refs(job)}")
    if "environment" in job:
        v.append("evals-current sets environment")
    if job.get("permissions") != {"contents": "read", "actions": "read"}:
        v.append(f"evals-current permissions are {job.get('permissions')!r}, "
                 "not exactly contents: read, actions: read")
    if w.get("permissions") != {"contents": "read"}:
        v.append(f"check.yml top-level permissions are {w.get('permissions')!r}")
    if "if" in job:
        v.append(f"evals-current has if: {job['if']!r}; it must run on every push")
    if job.get("continue-on-error") not in (None, False, "false"):
        v.append("evals-current has continue-on-error")
    steps = job.get("steps") or []
    for st in steps:
        if st.get("continue-on-error") not in (None, False, "false"):
            v.append("a step of evals-current has continue-on-error")
        for e in EXPR.findall(str(st.get("run", ""))):
            v.append(f"evals-current: run holds the expression {e.strip()!r}")
    i, st = select_step(job)
    if st is None:
        return v + ["evals-current runs no scripts/ci-evals-select.py"]
    run = str(st.get("run", ""))
    if re.search(r"\bset\s+\+e\b", run) or any(SWALLOW.search(l) for l in run.splitlines()
                                                 if SELECT.search(l)):
        v.append("evals-current's selector step hides the selector's exit")
    env = st.get("env") or {}
    if str(env.get("MODE", "")).strip() != "pending":
        v.append(f"evals-current's selector env MODE is {env.get('MODE')!r}, not pending")
    if not REF_ENV.fullmatch(str(env.get("REF", ""))):
        v.append(f"evals-current's selector env REF is {env.get('REF')!r}, not ${{{{ github.ref_name }}}}")
    if not SHA_ENV.fullmatch(str(env.get("SHA", ""))):
        v.append(f"evals-current's selector env SHA is {env.get('SHA')!r}, not ${{{{ github.sha }}}}")
    sp = [k for k, s in enumerate(steps) if str(s.get("uses", "")).startswith("actions/setup-python@")]
    ins = [k for k, s in enumerate(steps) if "--require-hashes" in str(s.get("run", ""))
           and "requirements-ci.txt" in str(s.get("run", ""))]
    if not sp or not ins or not (sp[0] < ins[0] < i):
        v.append("evals-current has no setup-python then --require-hashes install before the selector")
    push = triggers(w).get("push") or {}
    for k in ("paths", "paths-ignore", "branches", "branches-ignore"):
        if k in push:
            v.append(f"check.yml push trigger has {k}; evals-current must run on every push")
    return v


RULES = (judge_dispatch_only, judge_all_input, judge_all_through_env, judge_selector_base,
         judge_evals_current)

# ---------------------------------------------------------------- planted tree

CHECKOUT = lambda deep=False: {"uses": f"actions/checkout@{HASH}", "with": dict(
    {"persist-credentials": False}, **({"fetch-depth": 0} if deep else {}))}
SETUP_PY = {"uses": f"actions/setup-python@{HASH}",
            "with": {"python-version": "${{ needs.config.outputs.python }}"}}
PIP = {"run": "python3 -m pip install --require-hashes -r scripts/tools/requirements-ci.txt"}
LIST_RUNS = ("RUNS=$(gh api \"repos/$GITHUB_REPOSITORY/actions/workflows/evals.yml/runs"
             "?status=success&per_page=100\" --jq '[.workflow_runs[] | {head_sha, head_branch, "
             "conclusion}]' || true)\nexport RUNS\n")


def selector_step(**env):
    return {"id": "select", "env": dict({"GH_TOKEN": "${{ github.token }}",
                                         "SHA": "${{ github.sha }}"}, **env),
            "run": LIST_RUNS + "matrix=$(python3 scripts/ci-evals-select.py)\n"
                               "echo \"matrix=$matrix\" >>\"$GITHUB_OUTPUT\""}


def good_check():
    ec = {"name": "evals-current", "needs": "config",
          "runs-on": "${{ needs.config.outputs.runner }}",
          "permissions": {"contents": "read", "actions": "read"},
          "steps": [CHECKOUT(True), copy.deepcopy(SETUP_PY), dict(PIP),
                    selector_step(MODE="pending", REF="${{ github.ref_name }}")]}
    return {"name": "check", "on": {"push": None, "workflow_dispatch": None},
            "permissions": {"contents": "read"},
            "jobs": {"config": {"runs-on": "ubuntu-latest", "steps": [CHECKOUT()]},
                     "evals-current": ec}}


def good_evals():
    cfg = {"runs-on": "ubuntu-latest",
           "permissions": {"contents": "read", "actions": "read"},
           "steps": [CHECKOUT(True), copy.deepcopy(SETUP_PY), dict(PIP),
                     selector_step(ALL="${{ inputs.all }}", EVENT="${{ github.event_name }}")]}
    return {"name": "evals",
            "on": {"workflow_dispatch": {"inputs": {"all": {
                "description": "Run every suite", "type": "boolean", "default": False}}}},
            "permissions": {"contents": "read"},
            "jobs": {"config": cfg, "evals": {"needs": "config", "runs-on": "x",
                                              "environment": "dev", "steps": []}}}


def planted(tc, mutate=None):
    tmp = tempfile.TemporaryDirectory()
    tc.addCleanup(tmp.cleanup)
    root = Path(tmp.name)
    (root / ".github" / "workflows").mkdir(parents=True)
    d = {"check.yml": good_check(), "evals.yml": good_evals()}
    if mutate:
        mutate(d)
    for n, w in d.items():
        if w is not None:
            (root / ".github" / "workflows" / n).write_text(yaml.safe_dump(w, sort_keys=False))
    return root


def ec(d):
    return d["check.yml"]["jobs"]["evals-current"]


def ec_sel(d):
    return ec(d)["steps"][-1]


def ev_sel(d):
    return d["evals.yml"]["jobs"]["config"]["steps"][-1]


def dispatch(d):
    return d["evals.yml"]["on"]["workflow_dispatch"]


# ---------------------------------------------------------------- tests

class BaselineTest(unittest.TestCase):
    def test_good_tree_passes_every_rule(self):
        root = planted(self)
        for fn in RULES:
            self.assertEqual(fn(root), [], fn.__name__)

    def test_unquoted_on_and_github_event_inputs_pass(self):
        def m(d):
            ev_sel(d)["env"]["ALL"] = "${{ github.event.inputs.all }}"
        root = planted(self, m)
        p = root / ".github/workflows/evals.yml"
        p.write_text(p.read_text().replace("'on':", "on:"))
        for fn in RULES:
            self.assertEqual(fn(root), [], fn.__name__)

    def test_unshallow_fetch_passes_history(self):
        def m(d):
            for s in (ec(d)["steps"], d["evals.yml"]["jobs"]["config"]["steps"]):
                s[0] = CHECKOUT()
                s[-1]["run"] = "git fetch -q --unshallow origin\n" + s[-1]["run"]
        self.assertEqual(judge_selector_base(planted(self, m)), [])


class DispatchOnlyTest(unittest.TestCase):
    def test_real_evals_has_no_push_trigger(self):
        self.assertEqual(judge_dispatch_only(REPO), [])

    def test_planted_push_trigger_fails(self):
        for push in (None, {"branches": ["main"]}, {"paths": ["src/agents/**"]}):
            with self.subTest(push):
                def m(d, push=push):
                    d["evals.yml"]["on"]["push"] = push
                self.assertTrue(judge_dispatch_only(planted(self, m)))

    def test_planted_other_trigger_fails(self):
        for ev in ("schedule", "workflow_run", "pull_request"):
            with self.subTest(ev):
                def m(d, ev=ev):
                    d["evals.yml"]["on"][ev] = None
                self.assertTrue(judge_dispatch_only(planted(self, m)))

    def test_planted_no_dispatch_fails(self):
        def m(d):
            d["evals.yml"]["on"] = {"push": None}
        self.assertTrue(judge_dispatch_only(planted(self, m)))

    def test_planted_list_form_with_push_fails(self):
        def m(d):
            d["evals.yml"]["on"] = ["workflow_dispatch", "push"]
        self.assertTrue(judge_dispatch_only(planted(self, m)))


class AllInputTest(unittest.TestCase):
    def test_real_all_input(self):
        self.assertEqual(judge_all_input(REPO), [])

    def test_planted_no_input_fails(self):
        def m(d):
            d["evals.yml"]["on"]["workflow_dispatch"] = None
        self.assertTrue(judge_all_input(planted(self, m)))

    def test_planted_string_type_fails(self):
        def m(d):
            dispatch(d)["inputs"]["all"]["type"] = "string"
        self.assertTrue(judge_all_input(planted(self, m)))

    def test_planted_default_true_fails(self):
        for dflt in (True, "true", None):
            with self.subTest(dflt):
                def m(d, dflt=dflt):
                    a = dispatch(d)["inputs"]["all"]
                    if dflt is None:
                        del a["default"]
                    else:
                        a["default"] = dflt
                self.assertTrue(judge_all_input(planted(self, m)))


class AllThroughEnvTest(unittest.TestCase):
    def test_real_all_through_env(self):
        self.assertEqual(judge_all_through_env(REPO), [])

    def test_planted_expression_in_run_fails(self):
        for e in ("${{ inputs.all }}", "${{ github.event.inputs.all }}", "${{ toJSON(inputs) }}"):
            with self.subTest(e):
                def m(d, e=e):
                    ev_sel(d)["run"] = f"ALL={e}\n" + ev_sel(d)["run"]
                self.assertTrue(judge_all_through_env(planted(self, m)))

    def test_planted_expression_in_another_job_fails(self):
        def m(d):
            d["evals.yml"]["jobs"]["evals"]["steps"] = [{"run": "echo ${{ inputs.all }}"}]
        self.assertTrue(judge_all_through_env(planted(self, m)))

    def test_planted_env_missing_or_literal_fails(self):
        for val in (None, "true", "${{ inputs.other }}"):
            with self.subTest(val):
                def m(d, val=val):
                    if val is None:
                        del ev_sel(d)["env"]["ALL"]
                    else:
                        ev_sel(d)["env"]["ALL"] = val
                self.assertTrue(judge_all_through_env(planted(self, m)))


class SelectorBaseTest(unittest.TestCase):
    def test_real_selector_chooses_the_base(self):
        self.assertEqual(judge_selector_base(REPO), [])

    def test_planted_before_given_fails(self):
        for where in (ev_sel, ec_sel):
            with self.subTest(where.__name__):
                def m(d, where=where):
                    where(d)["env"]["BEFORE"] = "${{ steps.base.outputs.before }}"
                self.assertTrue(judge_selector_base(planted(self, m)))

    def test_planted_no_runs_fails(self):
        for where in (ev_sel, ec_sel):
            with self.subTest(where.__name__):
                def m(d, where=where):
                    where(d)["run"] = "matrix=$(python3 scripts/ci-evals-select.py)"
                self.assertTrue(judge_selector_base(planted(self, m)))

    def test_planted_token_from_a_secret_fails(self):
        def m(d):
            ec_sel(d)["env"]["GH_TOKEN"] = "${{ secrets.GH_PAT }}"
        self.assertTrue(judge_selector_base(planted(self, m)))

    def test_planted_shallow_history_fails(self):
        for job in (ec, lambda d: d["evals.yml"]["jobs"]["config"]):
            with self.subTest(job):
                def m(d, job=job):
                    job(d)["steps"][0] = CHECKOUT()
                self.assertTrue(judge_selector_base(planted(self, m)))

    def test_planted_depth_one_fetch_is_not_history(self):
        def m(d):
            ec(d)["steps"][0] = CHECKOUT()
            ec_sel(d)["run"] = "git fetch -q --depth=1 origin \"$SHA\"\n" + ec_sel(d)["run"]
        self.assertTrue(judge_selector_base(planted(self, m)))


class EvalsCurrentTest(unittest.TestCase):
    def test_real_evals_current(self):
        self.assertEqual(judge_evals_current(REPO), [])

    def test_planted_no_job_fails(self):
        def m(d):
            del d["check.yml"]["jobs"]["evals-current"]
        self.assertTrue(judge_evals_current(planted(self, m)))

    def test_underscore_id_is_found(self):
        def m(d):
            js = d["check.yml"]["jobs"]
            js["evals_current"] = js.pop("evals-current")
            del js["evals_current"]["name"]
        self.assertEqual(judge_evals_current(planted(self, m)), [])

    def test_planted_secret_fails(self):
        for where in ("job", "step"):
            with self.subTest(where):
                def m(d, where=where):
                    tgt = ec(d) if where == "job" else ec_sel(d)
                    tgt.setdefault("env", {})["K"] = "${{ secrets.ANTHROPIC_API_KEY }}"
                self.assertTrue(judge_evals_current(planted(self, m)))

    def test_planted_environment_fails(self):
        def m(d):
            ec(d)["environment"] = "dev"
        self.assertTrue(judge_evals_current(planted(self, m)))

    def test_planted_other_permissions_fail(self):
        for perms in (None, {"contents": "read"}, {"actions": "read"},
                      {"contents": "read", "actions": "write"},
                      {"contents": "read", "actions": "read", "checks": "write"}, "read-all"):
            with self.subTest(perms):
                def m(d, perms=perms):
                    if perms is None:
                        del ec(d)["permissions"]
                    else:
                        ec(d)["permissions"] = perms
                self.assertTrue(judge_evals_current(planted(self, m)))

    def test_planted_permission_at_top_level_fails(self):
        def m(d):
            d["check.yml"]["permissions"] = {"contents": "read", "actions": "read"}
        self.assertTrue(judge_evals_current(planted(self, m)))

    def test_planted_job_condition_fails(self):
        for cond in ("github.ref_name != 'main'", "needs.config.outputs.docs_only != 'true'",
                     "always()"):
            with self.subTest(cond):
                def m(d, cond=cond):
                    ec(d)["if"] = cond
                self.assertTrue(judge_evals_current(planted(self, m)))

    def test_planted_continue_on_error_fails(self):
        for where in ("job", "step"):
            with self.subTest(where):
                def m(d, where=where):
                    (ec(d) if where == "job" else ec_sel(d))["continue-on-error"] = True
                self.assertTrue(judge_evals_current(planted(self, m)))

    def test_planted_swallowed_exit_fails(self):
        for run in ("python3 scripts/ci-evals-select.py || true",
                    "python3 scripts/ci-evals-select.py || :",
                    "set +e\npython3 scripts/ci-evals-select.py"):
            with self.subTest(run):
                def m(d, run=run):
                    ec_sel(d)["run"] = LIST_RUNS + run
                self.assertTrue(judge_evals_current(planted(self, m)))

    def test_planted_selector_env_wrong_fails(self):
        for key, val in (("MODE", None), ("MODE", "current"), ("REF", None),
                         ("REF", "main"), ("REF", "${{ github.ref }}"), ("SHA", None)):
            with self.subTest(f"{key}={val}"):
                def m(d, key=key, val=val):
                    if val is None:
                        del ec_sel(d)["env"][key]
                    else:
                        ec_sel(d)["env"][key] = val
                self.assertTrue(judge_evals_current(planted(self, m)))

    def test_planted_expression_in_run_fails(self):
        def m(d):
            ec_sel(d)["run"] = "REF=${{ github.ref_name }}\n" + ec_sel(d)["run"]
        self.assertTrue(judge_evals_current(planted(self, m)))

    def test_planted_no_python_setup_fails(self):
        for drop in (1, 2):
            with self.subTest(drop):
                def m(d, drop=drop):
                    del ec(d)["steps"][drop]
                self.assertTrue(judge_evals_current(planted(self, m)))

    def test_planted_no_selector_fails(self):
        def m(d):
            ec_sel(d)["run"] = LIST_RUNS + "python3 scripts/ci-summary.py"
        self.assertTrue(judge_evals_current(planted(self, m)))

    def test_planted_push_filter_fails(self):
        for k, val in (("paths", ["src/**"]), ("paths-ignore", ["docs/**"]),
                       ("branches", ["001-prose-to-accepted-slice"]), ("branches-ignore", ["main"])):
            with self.subTest(k):
                def m(d, k=k, val=val):
                    d["check.yml"]["on"]["push"] = {k: val}
                self.assertTrue(judge_evals_current(planted(self, m)))


if __name__ == "__main__":
    unittest.main()
