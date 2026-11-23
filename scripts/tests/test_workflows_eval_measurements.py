"""The evals workflow keeps the measurement and finds the earlier one (T014; feature 008,
research R2 and R3), written before the workflow change.

Workflow rules, each a judge_<rule>(root) over root/.github/workflows, with a test on the real
files and planted violations in a temporary tree that is valid otherwise (a baseline test proves
it valid):

- measurement_scan: in the evals job, one step with an `id`, `if: always()` and
  `continue-on-error: true` runs `scripts/secret-scan.sh "$EVAL_MEASUREMENT_DIR"` and nothing
  that swallows its failure (`$VAR` or `${VAR}`, quoted or not, `./` optional);
- measurement_upload: one upload step (`actions/upload-artifact` at the pinned digest) with
  `if: always() && steps.<scan id>.outcome == 'success'`, `continue-on-error: true`, a name that
  starts `eval-measurements-` and then holds matrix.suite, matrix.cases and github.run_attempt in
  that order, `path: ${{ env.EVAL_MEASUREMENT_DIR }}`, `retention-days: 90` and
  `if-no-files-found: ignore`;
- measurement_warning: a step with `if: always() && steps.<scan id>.outcome != 'success'` whose
  run prints a `::warning::` line that says the measurement was not kept, and appends to
  $GITHUB_STEP_SUMMARY (`>>` or `tee -a`);
- measurement_order: failed-outputs scan, measurement scan, measurement upload, warning step,
  then the failed-outputs upload, which stays the last step;
- config_earlier: in the config job, after the step with id `select`, one step with an `id` and
  `continue-on-error: true` writes the listing to a file with
  `gh api --paginate "repos/$GITHUB_REPOSITORY/actions/artifacts?per_page=100" --jq '<filter>'`
  (the filter is the contract's, scripts/ci-evals-earlier.py's docstring; whitespace between its
  words is free) followed by `|| true`, then runs scripts/ci-evals-earlier.py on that file; env
  GH_TOKEN `${{ github.token }}`, MATRIX `${{ steps.select.outputs.matrix }}`, SHA
  `${{ github.sha }}`, RUN_ID `${{ github.run_id }}`;
- earlier_output: the config job's output `earlier` is `${{ steps.<that step's id>.outputs.earlier }}`;
- trials_earlier_env: the step that runs scripts/ci-evals-trials.py has env EARLIER
  `${{ needs.config.outputs.earlier }}`;
- permissions_unchanged: the workflow's permissions are `contents: read`, the config job's are
  `contents: read` and `actions: read`, the evals job has none of its own.

Fixture keys are not needed. The loading helpers are test_workflows.py's, imported unchanged.
"""
import copy
import re
import tempfile
import unittest
from pathlib import Path

import yaml

from test_workflows import jobs, load, need_files
from test_workflows_eval_artifacts import CHECKOUT, good_check
from test_workflows_eval_trials import (TRIALS, commands, evals_steps, good_trials_evals,
                                        strip_expr)

REPO = Path(__file__).resolve().parent.parent.parent
UPLOAD = re.compile(r"^actions/upload-artifact@")
DIGEST = "cf430e030ddbb5b0abf93d22962f4752f3646cd9"
EARLIER = "scripts/ci-evals-earlier.py"
JQ = ('.artifacts[] | select(.name | startswith("eval-measurements-")) | {id, name, expired, '
      'created_at, run_id: .workflow_run.id, head_sha: .workflow_run.head_sha}')
SCAN_ARG = re.compile(r"\"?\$(?:EVAL_MEASUREMENT_DIR|\{EVAL_MEASUREMENT_DIR\})\"?")
PATH_EXPR = re.compile(r"\$\{\{\s*env\.EVAL_MEASUREMENT_DIR\s*\}\}")
SCAN_IF = re.compile(r"always\(\)\s*&&\s*steps\.([A-Za-z_][\w-]*)\.outcome\s*==\s*(['\"])success\2")
WARN_IF = re.compile(r"always\(\)\s*&&\s*steps\.([A-Za-z_][\w-]*)\.outcome\s*!=\s*(['\"])success\2")
SUMMARY_APPEND = re.compile(r"(?:>>|\btee\s+-a)\s*\"?\$(?:GITHUB_STEP_SUMMARY|\{GITHUB_STEP_SUMMARY\})\"?")
URL = re.compile(r"repos/(?:\$GITHUB_REPOSITORY|\$\{GITHUB_REPOSITORY\}|\$\{\{\s*github\.repository\s*\}\})"
                 r"/actions/artifacts\?per_page=100")
JQ_ARG = re.compile(r"--jq\s+(?:'([^']*)'|\"((?:[^\"\\]|\\.)*)\")", re.S)
EXPR = {
    "GH_TOKEN": re.compile(r"\$\{\{\s*github\.token\s*\}\}"),
    "MATRIX": re.compile(r"\$\{\{\s*steps\.select\.outputs\.matrix\s*\}\}"),
    "SHA": re.compile(r"\$\{\{\s*github\.sha\s*\}\}"),
    "RUN_ID": re.compile(r"\$\{\{\s*github\.run_id\s*\}\}"),
}
EARLIER_EXPR = re.compile(r"\$\{\{\s*needs\.config\.outputs\.earlier\s*\}\}")
TRIALS_NAME = "Trials of the evals"


def is_true(v):
    return v is True or str(v).strip().lower() == "true"


def squash(s):
    return re.sub(r"\s+", " ", s).strip()


def norm_file(tok):
    return tok.strip().strip("\"'").replace("{", "").replace("}", "")


def evals_job(wf):
    return jobs(wf.get("evals.yml") or {}).get("evals") or {}


def config_job(wf):
    return jobs(wf.get("evals.yml") or {}).get("config") or {}


def scan_calls_measurement(st):
    return "secret-scan.sh" in str(st.get("run", "")) and "EVAL_MEASUREMENT_DIR" in str(st.get("run", ""))


def path_is_report(st):
    return "EVAL_REPORT_DIR" in str((st.get("with") or {}).get("path", ""))


def is_upload(st):
    return bool(UPLOAD.match(str(st.get("uses", ""))))


def parts(steps):
    """Indexes: the failed-outputs scan, the measurement scans, the measurement uploads, the
    failed-outputs uploads and the warning steps."""
    fs = [i for i, st in enumerate(steps)
          if "secret-scan.sh" in str(st.get("run", "")) and "EVAL_REPORT_DIR" in str(st.get("run", ""))]
    ms = [i for i, st in enumerate(steps) if scan_calls_measurement(st)]
    mu = [i for i, st in enumerate(steps) if is_upload(st) and not path_is_report(st)]
    fu = [i for i, st in enumerate(steps) if is_upload(st) and path_is_report(st)]
    wn = [i for i, st in enumerate(steps) if "::warning::" in str(st.get("run", ""))
          and "measurement" in str(st.get("run", "")).lower()]
    return fs, ms, mu, fu, wn


# ---------------------------------------------------------------- rules

def judge_measurement_scan(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    _, ms, _, _, _ = parts(evals_steps(wf))
    if len(ms) != 1:
        return [f"measurement_scan: {len(ms)} evals steps run secret-scan.sh on "
                "$EVAL_MEASUREMENT_DIR, not one"]
    st = evals_steps(wf)[ms[0]]
    if not str(st.get("id", "")).strip():
        v.append("measurement_scan: the step has no id")
    if strip_expr(st.get("if", "")) != "always()":
        v.append(f"measurement_scan: the step's if is {st.get('if')!r}, not always()")
    if not is_true(st.get("continue-on-error")):
        v.append(f"measurement_scan: continue-on-error is {st.get('continue-on-error')!r}, not true")
    cmds = [c for c in commands(st.get("run", ""))
            if not (c and c[0] == "set" and all(a.startswith("-") for a in c[1:]))]
    if len(cmds) != 1 or len(cmds[0]) != 2 or cmds[0][0] != "scripts/secret-scan.sh" \
            or not SCAN_ARG.fullmatch(cmds[0][1]):
        v.append('measurement_scan: the step does not run exactly scripts/secret-scan.sh '
                 '"$EVAL_MEASUREMENT_DIR"')
    return v


def scan_id(steps, ms):
    return str(steps[ms[0]].get("id", "")) if len(ms) == 1 else None


def judge_measurement_upload(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    steps = evals_steps(wf)
    _, ms, mu, _, _ = parts(steps)
    if len(mu) != 1:
        return [f"measurement_upload: {len(mu)} evals upload steps do not take the failed-outputs "
                "folder, not one measurement upload"]
    st = steps[mu[0]]
    if not str(st.get("uses", "")).startswith(f"actions/upload-artifact@{DIGEST}"):
        v.append(f"measurement_upload: uses is {st.get('uses')!r}, not upload-artifact at {DIGEST}")
    sid = scan_id(steps, ms)
    m = SCAN_IF.fullmatch(strip_expr(st.get("if", "")))
    if not m:
        v.append(f"measurement_upload: the if is {st.get('if')!r}, not "
                 "always() && steps.<scan id>.outcome == 'success'")
    elif not sid or m.group(1) != sid:
        v.append(f"measurement_upload: the if names step {m.group(1)!r}, not the measurement "
                 f"scan's id {sid!r}")
    if not is_true(st.get("continue-on-error")):
        v.append(f"measurement_upload: continue-on-error is {st.get('continue-on-error')!r}, not true")
    w = st.get("with") or {}
    name = str(w.get("name", ""))
    pos = 0
    ok = name.startswith("eval-measurements-")
    for part in (r"matrix\.suite", r"matrix\.cases", r"github\.run_attempt"):
        mm = re.compile(part).search(name, pos)
        if not mm:
            ok = False
            break
        pos = mm.end()
    if not ok:
        v.append(f"measurement_upload: name is {name!r}, not eval-measurements- with matrix.suite, "
                 "matrix.cases and github.run_attempt in that order")
    if not PATH_EXPR.fullmatch(str(w.get("path", "")).strip()):
        v.append(f"measurement_upload: path is {w.get('path')!r}, not ${{{{ env.EVAL_MEASUREMENT_DIR }}}}")
    if str(w.get("retention-days", "")).strip() != "90":
        v.append(f"measurement_upload: retention-days is {w.get('retention-days')!r}, not 90")
    if str(w.get("if-no-files-found", "")).strip() != "ignore":
        v.append(f"measurement_upload: if-no-files-found is {w.get('if-no-files-found')!r}, not ignore")
    return v


def judge_measurement_warning(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    steps = evals_steps(wf)
    _, ms, _, _, wn = parts(steps)
    warn = [i for i, st in enumerate(steps) if WARN_IF.fullmatch(strip_expr(st.get("if", "")))]
    if len(warn) != 1:
        return [f"measurement_warning: {len(warn)} steps have if always() && "
                "steps.<scan id>.outcome != 'success', not one"]
    st = steps[warn[0]]
    run = "\n".join(x for x in str(st.get("run", "")).split("\n") if not x.strip().startswith("#"))
    m = WARN_IF.fullmatch(strip_expr(st.get("if", "")))
    sid = scan_id(steps, ms)
    if not sid or m.group(1) != sid:
        v.append(f"measurement_warning: the if names step {m.group(1)!r}, not the measurement "
                 f"scan's id {sid!r}")
    if "::warning::" not in run:
        v.append("measurement_warning: the run prints no ::warning:: line")
    elif not re.search(r"::warning::[^\n]*not kept", run, re.I) \
            or "measurement" not in run.lower():
        v.append("measurement_warning: the ::warning:: line does not say the measurement was not kept")
    if not SUMMARY_APPEND.search(run):
        v.append("measurement_warning: the run does not append to $GITHUB_STEP_SUMMARY")
    return v


def judge_measurement_order(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    steps = evals_steps(wf)
    fs, ms, mu, fu, _ = parts(steps)
    warn = [i for i, st in enumerate(steps) if WARN_IF.fullmatch(strip_expr(st.get("if", "")))]
    if not fs or len(ms) != 1 or len(mu) != 1 or len(warn) != 1 or len(fu) != 1:
        return ["measurement_order: the failed-outputs scan and upload and the measurement scan, "
                "upload and warning step are not each present once"]
    if not fs[0] < ms[0] < mu[0] < warn[0] < fu[0]:
        v.append("measurement_order: the steps are not in the order failed-outputs scan, "
                 "measurement scan, measurement upload, warning, failed-outputs upload")
    if fu[0] != len(steps) - 1:
        v.append("measurement_order: the failed-outputs upload is not the last step")
    return v


def judge_config_earlier(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    steps = config_job(wf).get("steps") or []
    sel = [i for i, st in enumerate(steps) if st.get("id") == "select"]
    if len(sel) != 1:
        return [f"config_earlier: {len(sel)} config steps have id select, not one"]
    es = [i for i, st in enumerate(steps) if EARLIER in str(st.get("run", ""))]
    if len(es) != 1:
        return [f"config_earlier: {len(es)} config steps run {EARLIER}, not one"]
    if es[0] < sel[0]:
        return ["config_earlier: the step runs before the select step"]
    st = steps[es[0]]
    if not str(st.get("id", "")).strip():
        v.append("config_earlier: the step has no id")
    if not is_true(st.get("continue-on-error")):
        v.append(f"config_earlier: continue-on-error is {st.get('continue-on-error')!r}, not true")
    env = st.get("env") or {}
    for key, rx in EXPR.items():
        if not rx.fullmatch(str(env.get(key, "")).strip()):
            v.append(f"config_earlier: env {key} is {env.get(key)!r}, not its expression")
    text = re.sub(r"\\\n", " ", str(st.get("run", "")))
    m = JQ_ARG.search(text)
    if not m:
        v.append("config_earlier: the gh api call has no quoted --jq filter")
        return v
    filt = m.group(1) if m.group(1) is not None else m.group(2).replace('\\"', '"')
    if squash(filt) != JQ:
        v.append(f"config_earlier: the --jq filter is {squash(filt)!r}, not the contract's")
    text = text[:m.start()] + "--jq JQ" + text[m.end():]
    lines = [x.strip() for x in text.split("\n") if x.strip() and not x.strip().startswith("#")]
    gh = [x for x in lines if re.match(r"gh\s+api\b", x)]
    ea = [x for x in lines if EARLIER in x]
    if len(gh) != 1 or len(ea) != 1 or lines.index(gh[0]) > lines.index(ea[0]):
        v.append("config_earlier: the step does not run one gh api call and then "
                 f"{EARLIER} once")
        return v
    g = re.fullmatch(r"gh\s+api\s+(.*?)\s+--jq\s+JQ\s*>\s*(\S+)(?:\s+2>\s*/dev/null)?\s*\|\|\s*true", gh[0])
    if not g:
        v.append("config_earlier: the gh api call is not `gh api … --jq <filter> > <file> || true`")
        return v
    args = g.group(1).split()
    if "--paginate" not in args:
        v.append("config_earlier: the gh api call lacks --paginate")
    if not URL.search(g.group(1)):
        v.append("config_earlier: the gh api call does not list repos/$GITHUB_REPOSITORY/"
                 "actions/artifacts?per_page=100")
    e = re.fullmatch(r"(?:python3\s+)?(?:\./)?scripts/ci-evals-earlier\.py\s+(\S+)", ea[0])
    if not e or norm_file(e.group(1)) != norm_file(g.group(2)):
        v.append(f"config_earlier: {EARLIER} is not called on the file the listing is written to")
    return v


def judge_earlier_output(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    steps = config_job(wf).get("steps") or []
    es = [st for st in steps if EARLIER in str(st.get("run", ""))]
    if len(es) != 1 or not str(es[0].get("id", "")).strip():
        return [f"earlier_output: no single config step with an id runs {EARLIER}"]
    out = (config_job(wf).get("outputs") or {}).get("earlier")
    if out is None:
        return ["earlier_output: the config job declares no output earlier"]
    want = re.compile(r"\$\{\{\s*steps\." + re.escape(str(es[0]["id"])) + r"\.outputs\.earlier\s*\}\}")
    if not want.fullmatch(str(out).strip()):
        return [f"earlier_output: output earlier is {out!r}, not "
                f"${{{{ steps.{es[0]['id']}.outputs.earlier }}}}"]
    return []


def judge_trials_earlier_env(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    st = [s for s in evals_steps(wf) if TRIALS in str(s.get("run", ""))]
    if len(st) != 1:
        return [f"trials_earlier_env: {len(st)} evals steps run {TRIALS}, not one"]
    val = (st[0].get("env") or {}).get("EARLIER")
    if not EARLIER_EXPR.fullmatch(str(val).strip()):
        return [f"trials_earlier_env: env EARLIER is {val!r}, not ${{{{ needs.config.outputs.earlier }}}}"]
    return []


def judge_permissions_unchanged(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    w = wf["evals.yml"]
    if w.get("permissions") != {"contents": "read"}:
        v.append(f"permissions_unchanged: the workflow's permissions are {w.get('permissions')!r}")
    if config_job(wf).get("permissions") != {"contents": "read", "actions": "read"}:
        v.append(f"permissions_unchanged: the config job's permissions are "
                 f"{config_job(wf).get('permissions')!r}")
    if "permissions" in evals_job(wf):
        v.append(f"permissions_unchanged: the evals job has permissions "
                 f"{evals_job(wf)['permissions']!r}")
    return v


RULES = (judge_measurement_scan, judge_measurement_upload, judge_measurement_warning,
         judge_measurement_order, judge_config_earlier, judge_earlier_output,
         judge_trials_earlier_env, judge_permissions_unchanged)


# ---------------------------------------------------------------- planted tree

def M_SCAN():
    return {"id": "mscan", "name": "Scan the measurement for credentials", "if": "always()",
            "continue-on-error": True, "run": 'scripts/secret-scan.sh "$EVAL_MEASUREMENT_DIR"'}


def M_UPLOAD():
    return {"name": "Keep the measurement", "uses": f"actions/upload-artifact@{DIGEST}",
            "if": "always() && steps.mscan.outcome == 'success'", "continue-on-error": True,
            "with": {"name": "eval-measurements-${{ matrix.suite }}${{ matrix.cases && "
                             "format('-{0}', matrix.cases) || '' }}-${{ github.run_attempt }}",
                     "path": "${{ env.EVAL_MEASUREMENT_DIR }}", "retention-days": 90,
                     "if-no-files-found": "ignore"}}


def M_WARN():
    return {"name": "Say the measurement was not kept",
            "if": "always() && steps.mscan.outcome != 'success'",
            "run": 'msg="::warning::The eval measurement was not kept (scan or upload failed)"\n'
                   'echo "$msg"\necho "$msg" >>"$GITHUB_STEP_SUMMARY"'}


def SELECT_STEP():
    return {"id": "select", "name": "Choose the suites", "run": "echo matrix=[] >>\"$GITHUB_OUTPUT\""}


def EARLIER_STEP():
    return {"id": "earlier", "name": "Find the earlier measurements", "continue-on-error": True,
            "env": {"GH_TOKEN": "${{ github.token }}",
                    "MATRIX": "${{ steps.select.outputs.matrix }}",
                    "SHA": "${{ github.sha }}", "RUN_ID": "${{ github.run_id }}"},
            "run": 'gh api --paginate "repos/$GITHUB_REPOSITORY/actions/artifacts?per_page=100" '
                   "--jq '.artifacts[]\n  | select(.name | startswith(\"eval-measurements-\")) | "
                   "{id, name, expired, created_at,\n  run_id: .workflow_run.id, "
                   "head_sha: .workflow_run.head_sha}' >\"$RUNNER_TEMP/artifacts.jsonl\" || true\n"
                   f'python3 {EARLIER} "$RUNNER_TEMP/artifacts.jsonl"'}


def good_measurement_evals():
    wf = good_trials_evals()
    wf["jobs"]["config"] = {
        "runs-on": "ubuntu-latest",
        "permissions": {"contents": "read", "actions": "read"},
        "outputs": {"matrix": "${{ steps.select.outputs.matrix }}",
                    "earlier": "${{ steps.earlier.outputs.earlier }}"},
        "steps": [copy.deepcopy(CHECKOUT), SELECT_STEP(), EARLIER_STEP()]}
    steps = wf["jobs"]["evals"]["steps"]
    for st in steps:
        if TRIALS in str(st.get("run", "")):
            st["env"]["EARLIER"] = "${{ needs.config.outputs.earlier }}"
    steps[-1:-1] = [M_SCAN(), M_UPLOAD(), M_WARN()]
    return wf


def planted(tc, mutate=None):
    tmp = tempfile.TemporaryDirectory()
    tc.addCleanup(tmp.cleanup)
    root = Path(tmp.name)
    (root / ".github" / "workflows").mkdir(parents=True)
    d = {"check.yml": good_check(), "evals.yml": good_measurement_evals()}
    if mutate:
        mutate(d)
    for n, w in d.items():
        (root / ".github" / "workflows" / n).write_text(yaml.safe_dump(w, sort_keys=False))
    return root


def ev(d):
    return d["evals.yml"]["jobs"]["evals"]


def cfg(d):
    return d["evals.yml"]["jobs"]["config"]


def steps_of(d):
    return ev(d)["steps"]


def mscan(d):
    return next(s for s in steps_of(d) if scan_calls_measurement(s))


def mup(d):
    return next(s for s in steps_of(d) if is_upload(s) and not path_is_report(s))


def warn(d):
    return next(s for s in steps_of(d) if "::warning::" in str(s.get("run", "")))


def earlier(d):
    return next(s for s in cfg(d)["steps"] if EARLIER in str(s.get("run", "")))


def trials(d):
    return next(s for s in steps_of(d) if TRIALS in str(s.get("run", "")))


def fails(tc, fn, mutate, rule):
    v = fn(planted(tc, mutate))
    tc.assertTrue(v, "no violation found")
    tc.assertTrue(all(rule in x for x in v), v)


def set_or_del(obj, key, val):
    if val is None:
        obj.pop(key, None)
    else:
        obj[key] = val


# ---------------------------------------------------------------- tests

class BaselineTest(unittest.TestCase):
    def test_good_tree_passes_every_rule(self):
        root = planted(self)
        for fn in RULES:
            self.assertEqual(fn(root), [], fn.__name__)

    def test_expression_and_quoting_variants_pass(self):
        def m(d):
            s = mscan(d)
            s["if"] = "${{ always() }}"
            s["continue-on-error"] = "true"
            s["run"] = "set -e\n./scripts/secret-scan.sh ${EVAL_MEASUREMENT_DIR}"
            u = mup(d)
            u["if"] = "${{ always() && steps.mscan.outcome == \"success\" }}"
            u["continue-on-error"] = "true"
            u["with"]["retention-days"] = "90"
            u["with"]["path"] = "${{env.EVAL_MEASUREMENT_DIR}}"
            u["with"]["name"] = "eval-measurements-${{matrix.suite}}-${{ matrix.cases }}-${{ github.run_attempt }}"
            w = warn(d)
            w["if"] = "${{always() && steps.mscan.outcome != 'success'}}"
            w["run"] = ("echo \"::warning::Measurement not kept\" | tee -a \"$GITHUB_STEP_SUMMARY\"")
            e = earlier(d)
            e["continue-on-error"] = "true"
            e["env"] = {"GH_TOKEN": "${{github.token}}",
                        "MATRIX": "${{ steps.select.outputs.matrix}}",
                        "SHA": "${{ github.sha}}", "RUN_ID": "${{github.run_id }}"}
            e["run"] = ("gh api repos/${GITHUB_REPOSITORY}/actions/artifacts?per_page=100 --paginate \\\n"
                        "  --jq \".artifacts[] | select(.name | startswith(\\\"eval-measurements-\\\")) "
                        "| {id, name, expired, created_at, run_id: .workflow_run.id, "
                        "head_sha: .workflow_run.head_sha}\" > ${RUNNER_TEMP}/a.jsonl 2>/dev/null || true\n"
                        "scripts/ci-evals-earlier.py '${RUNNER_TEMP}/a.jsonl'")
            cfg(d)["outputs"]["earlier"] = "${{steps.earlier.outputs.earlier}}"
            trials(d)["env"]["EARLIER"] = "${{needs.config.outputs.earlier}}"
        root = planted(self, m)
        for fn in RULES:
            self.assertEqual(fn(root), [], fn.__name__)

    def test_other_ids_and_extra_steps_pass(self):
        def m(d):
            for s in steps_of(d):
                if s is mscan(d):
                    s["id"] = "keep_scan"
            mup(d)["if"] = "always() && steps.keep_scan.outcome == 'success'"
            warn(d)["if"] = "always() && steps.keep_scan.outcome != 'success'"
            cfg(d)["steps"].append({"run": "echo later"})
            e = earlier(d)
            e["id"] = "baseline"
            cfg(d)["outputs"]["earlier"] = "${{ steps.baseline.outputs.earlier }}"
            steps_of(d).insert(1, {"run": "echo unrelated"})
        root = planted(self, m)
        for fn in RULES:
            self.assertEqual(fn(root), [], fn.__name__)

    def test_the_planted_filter_is_the_contracts(self):
        text = (REPO / "scripts" / "ci-evals-earlier.py").read_text()
        doc = re.search(r"--jq '(.*?)'\n", text, re.S).group(1)
        self.assertEqual(squash(doc), JQ)


class MeasurementScanTest(unittest.TestCase):
    def test_real_workflow(self):
        self.assertEqual(judge_measurement_scan(REPO), [])

    def test_planted_missing_or_doubled_fails(self):
        def gone(d):
            steps_of(d).remove(mscan(d))
        def twice(d):
            s = steps_of(d)
            s.insert(s.index(mscan(d)) + 1, copy.deepcopy(mscan(d)))
        for m in (gone, twice):
            with self.subTest(m.__name__):
                fails(self, judge_measurement_scan, m, "measurement_scan")

    def test_planted_id_missing_or_empty_fails(self):
        for val in (None, ""):
            with self.subTest(val):
                fails(self, judge_measurement_scan, lambda d, v=val: set_or_del(mscan(d), "id", v),
                      "measurement_scan")

    def test_planted_conditions_fail(self):
        for cond in (None, "success()", "failure()", "always() && matrix.suite == 'steps'",
                     "failure() || steps.failed.outputs.failed == 'true'", "!cancelled()"):
            with self.subTest(cond):
                fails(self, judge_measurement_scan, lambda d, c=cond: set_or_del(mscan(d), "if", c),
                      "measurement_scan")

    def test_planted_continue_on_error_fails(self):
        for val in (None, False, "false", "${{ false }}"):
            with self.subTest(val):
                fails(self, judge_measurement_scan,
                      lambda d, v=val: set_or_del(mscan(d), "continue-on-error", v), "measurement_scan")

    def test_planted_commands_fail(self):
        runs = {
            "swallowed by || true": 'scripts/secret-scan.sh "$EVAL_MEASUREMENT_DIR" || true',
            "swallowed by || :": 'scripts/secret-scan.sh "$EVAL_MEASUREMENT_DIR" || :',
            "set +e first": 'set +e\nscripts/secret-scan.sh "$EVAL_MEASUREMENT_DIR"',
            "followed by true": 'scripts/secret-scan.sh "$EVAL_MEASUREMENT_DIR"; true',
            "report folder": 'scripts/secret-scan.sh "$EVAL_REPORT_DIR/../x" "$EVAL_MEASUREMENT_DIR"',
            "no argument": "scripts/secret-scan.sh",
            "subfolder": 'scripts/secret-scan.sh "$EVAL_MEASUREMENT_DIR/sub"',
            "other script": 'scripts/secret-scanx.sh "$EVAL_MEASUREMENT_DIR"',
            "only in a comment": '# scripts/secret-scan.sh "$EVAL_MEASUREMENT_DIR"\necho EVAL_MEASUREMENT_DIR',
            "two commands": 'scripts/secret-scan.sh "$EVAL_MEASUREMENT_DIR"\n'
                            'scripts/secret-scan.sh "$EVAL_MEASUREMENT_DIR"',
        }
        for label, run in runs.items():
            with self.subTest(label):
                fails(self, judge_measurement_scan, lambda d, r=run: mscan(d).update({"run": r}),
                      "measurement_scan")


class MeasurementUploadTest(unittest.TestCase):
    def test_real_workflow(self):
        self.assertEqual(judge_measurement_upload(REPO), [])

    def test_planted_step_missing_fails(self):
        def m(d):
            steps_of(d).remove(mup(d))
        fails(self, judge_measurement_upload, m, "measurement_upload")

    def test_planted_two_uploads_fail(self):
        def m(d):
            s = steps_of(d)
            s.insert(s.index(mup(d)) + 1, copy.deepcopy(mup(d)))
        fails(self, judge_measurement_upload, m, "measurement_upload")

    def test_planted_other_action_refs_fail(self):
        for uses in ("actions/upload-artifact@" + "a" * 40, "actions/upload-artifact@v7",
                     "actions/upload-artifact@main", "actions/upload-artifact",
                     "actions/upload-artifact@" + DIGEST[:-1] + "0"):
            with self.subTest(uses):
                fails(self, judge_measurement_upload, lambda d, u=uses: mup(d).update({"uses": u}),
                      "measurement_upload")

    def test_planted_conditions_fail(self):
        for cond in (None, "always()", "success()", "failure()",
                     "always() && steps.scan.outcome == 'success'",
                     "always() && steps.mscan.outcome != 'success'",
                     "always() && steps.mscan.outcome == 'failure'",
                     "steps.mscan.outcome == 'success'",
                     "success() && steps.mscan.outcome == 'success'",
                     "always() || steps.mscan.outcome == 'success'",
                     "failure() && steps.mscan.outcome == 'success'",
                     "(failure() || steps.failed.outputs.failed == 'true') && steps.mscan.outcome == 'success'"):
            with self.subTest(cond):
                fails(self, judge_measurement_upload, lambda d, c=cond: set_or_del(mup(d), "if", c),
                      "measurement_upload")

    def test_planted_continue_on_error_fails(self):
        for val in (None, False, "false", "${{ false }}"):
            with self.subTest(val):
                fails(self, judge_measurement_upload,
                      lambda d, v=val: set_or_del(mup(d), "continue-on-error", v), "measurement_upload")

    def test_planted_retention_fails(self):
        for val in (None, 7, 89, 91, 30, "ninety", "9"):
            with self.subTest(val):
                fails(self, judge_measurement_upload,
                      lambda d, v=val: set_or_del(mup(d)["with"], "retention-days", v), "measurement_upload")

    def test_planted_if_no_files_found_fails(self):
        for val in (None, "warn", "error"):
            with self.subTest(val):
                fails(self, judge_measurement_upload,
                      lambda d, v=val: set_or_del(mup(d)["with"], "if-no-files-found", v), "measurement_upload")

    def test_planted_paths_fail(self):
        for val in (None, "${{ env.EVAL_REPORT_DIR }}x", "${{ github.workspace }}-eval-measurement",
                    "${{ env.EVAL_MEASUREMENT_DIR }}/measurement.json", "${{ env.EVAL_MEASUREMENT_DIR }}/..",
                    "$EVAL_MEASUREMENT_DIR", "${{ env.EVAL_MEASUREMENT }}", "measurement"):
            with self.subTest(val):
                fails(self, judge_measurement_upload,
                      lambda d, v=val: set_or_del(mup(d)["with"], "path", v), "measurement_upload")

    def test_planted_path_of_the_report_folder_fails(self):
        def m(d):
            mup(d)["with"]["path"] = "${{ env.EVAL_REPORT_DIR }}"
        fails(self, judge_measurement_upload, m, "measurement_upload")

    def test_planted_names_fail(self):
        S, C, A = "${{ matrix.suite }}", "${{ matrix.cases }}", "${{ github.run_attempt }}"
        names = {
            "no name": None,
            "cases missing": f"eval-measurements-{S}-{A}",
            "suite missing": f"eval-measurements-{C}-{A}",
            "run attempt missing": f"eval-measurements-{S}-{C}",
            "all missing": "eval-measurements-",
            "constant": "eval-measurements-x",
            "wrong prefix": f"evals-{S}-{C}-{A}",
            "prefix without s": f"eval-measurement-{S}-{C}-{A}",
            "prefix not first": f"x-eval-measurements-{S}-{C}-{A}",
            "run id for attempt": f"eval-measurements-{S}-{C}-${{{{ github.run_id }}}}",
            "wrong order": f"eval-measurements-{A}-{S}-{C}",
            "cases before suite": f"eval-measurements-{C}-{S}-{A}",
        }
        for label, val in names.items():
            with self.subTest(label):
                fails(self, judge_measurement_upload,
                      lambda d, v=val: set_or_del(mup(d)["with"], "name", v), "measurement_upload")

    def test_planted_no_with_fails(self):
        def m(d):
            del mup(d)["with"]
        fails(self, judge_measurement_upload, m, "measurement_upload")

    def test_planted_no_scan_id_fails(self):
        def m(d):
            del mscan(d)["id"]
        fails(self, judge_measurement_upload, m, "measurement_upload")


class MeasurementWarningTest(unittest.TestCase):
    def test_real_workflow(self):
        self.assertEqual(judge_measurement_warning(REPO), [])

    def test_planted_step_missing_or_doubled_fails(self):
        def gone(d):
            steps_of(d).remove(warn(d))
        def twice(d):
            s = steps_of(d)
            s.insert(s.index(warn(d)) + 1, copy.deepcopy(warn(d)))
        for m in (gone, twice):
            with self.subTest(m.__name__):
                fails(self, judge_measurement_warning, m, "measurement_warning")

    def test_planted_conditions_fail(self):
        for cond in ("always()", "failure()", "always() && steps.mscan.outcome == 'success'",
                     "always() && steps.scan.outcome != 'success'",
                     "steps.mscan.outcome != 'success'", "always() && steps.mscan.outcome != 'failure'",
                     "failure() && steps.mscan.outcome != 'success'",
                     "always() || steps.mscan.outcome != 'success'"):
            with self.subTest(cond):
                fails(self, judge_measurement_warning, lambda d, c=cond: warn(d).update({"if": c}),
                      "measurement_warning")

    def test_planted_runs_fail(self):
        runs = {
            "no warning prefix": 'echo "The eval measurement was not kept" >>"$GITHUB_STEP_SUMMARY"',
            "no summary": 'echo "::warning::The eval measurement was not kept"',
            "summary by overwrite": 'echo "::warning::The eval measurement was not kept" >"$GITHUB_STEP_SUMMARY"',
            "other file": 'echo "::warning::The eval measurement was not kept" >>"$GITHUB_OUTPUT"',
            "other message": 'echo "::warning::Something else" >>"$GITHUB_STEP_SUMMARY"',
            "says kept": 'echo "::warning::The eval measurement was kept" >>"$GITHUB_STEP_SUMMARY"',
            "empty": "",
            "only a comment": '# echo "::warning::The measurement was not kept" >>"$GITHUB_STEP_SUMMARY"\ntrue',
        }
        for label, run in runs.items():
            with self.subTest(label):
                fails(self, judge_measurement_warning, lambda d, r=run: warn(d).update({"run": r}),
                      "measurement_warning")

    def test_planted_no_run_fails(self):
        def m(d):
            del warn(d)["run"]
        fails(self, judge_measurement_warning, m, "measurement_warning")


class MeasurementOrderTest(unittest.TestCase):
    def test_real_workflow(self):
        self.assertEqual(judge_measurement_order(REPO), [])

    def test_planted_orders_fail(self):
        def swap(i, j):
            def m(d):
                s = steps_of(d)
                a, b = s.index(i(d)), s.index(j(d))
                s[a], s[b] = s[b], s[a]
            return m
        def failed_scan(d):
            return next(s for s in steps_of(d) if s.get("id") == "scan")

        def failed_up(d):
            return steps_of(d)[-1]

        cases = {
            "measurement scan before the failed-outputs scan": swap(mscan, failed_scan),
            "upload before its scan": swap(mup, mscan),
            "warning before the upload": swap(warn, mup),
            "measurement upload after the failed-outputs upload": swap(mup, failed_up),
            "warning after the failed-outputs upload": swap(warn, failed_up),
            "scan after the upload and warning": swap(mscan, warn),
        }
        for label, m in cases.items():
            with self.subTest(label):
                fails(self, judge_measurement_order, m, "measurement_order")

    def test_planted_step_after_the_failed_upload_fails(self):
        def m(d):
            steps_of(d).append({"if": "always()", "run": "echo last"})
        fails(self, judge_measurement_order, m, "measurement_order")

    def test_planted_a_step_missing_fails(self):
        for pick in (mscan, mup, warn, lambda d: steps_of(d)[-1],
                     lambda d: next(s for s in steps_of(d) if s.get("id") == "scan")):
            with self.subTest(pick):
                def m(d, p=pick):
                    steps_of(d).remove(p(d))
                fails(self, judge_measurement_order, m, "measurement_order")


class ConfigEarlierTest(unittest.TestCase):
    def test_real_workflow(self):
        self.assertEqual(judge_config_earlier(REPO), [])

    def test_planted_step_missing_or_doubled_fails(self):
        def gone(d):
            cfg(d)["steps"].remove(earlier(d))
        def twice(d):
            s = cfg(d)["steps"]
            s.append(copy.deepcopy(earlier(d)))
        for m in (gone, twice):
            with self.subTest(m.__name__):
                fails(self, judge_config_earlier, m, "config_earlier")

    def test_planted_before_select_fails(self):
        def m(d):
            s = cfg(d)["steps"]
            e = earlier(d)
            s.remove(e)
            s.insert(s.index(next(x for x in s if x.get("id") == "select")), e)
        fails(self, judge_config_earlier, m, "config_earlier")

    def test_planted_no_select_fails(self):
        def m(d):
            cfg(d)["steps"].remove(next(x for x in cfg(d)["steps"] if x.get("id") == "select"))
        fails(self, judge_config_earlier, m, "config_earlier")

    def test_planted_id_missing_fails(self):
        for val in (None, ""):
            with self.subTest(val):
                fails(self, judge_config_earlier, lambda d, v=val: set_or_del(earlier(d), "id", v),
                      "config_earlier")

    def test_planted_continue_on_error_fails(self):
        for val in (None, False, "false"):
            with self.subTest(val):
                fails(self, judge_config_earlier,
                      lambda d, v=val: set_or_del(earlier(d), "continue-on-error", v), "config_earlier")

    def test_planted_env_problems_fail(self):
        bad = {"GH_TOKEN": ("${{ secrets.GITHUB_TOKEN }}", "x"),
               "MATRIX": ("${{ needs.config.outputs.matrix }}", "${{ steps.pins.outputs.matrix }}", "[]"),
               "SHA": ("${{ github.event.after }}", "abc"),
               "RUN_ID": ("${{ github.run_number }}", "${{ github.run_attempt }}", "1")}
        for key, vals in bad.items():
            for val in (None,) + vals:
                with self.subTest(f"{key}={val}"):
                    def m(d, k=key, v=val):
                        set_or_del(earlier(d)["env"], k, v)
                    v = judge_config_earlier(planted(self, m))
                    self.assertTrue(any("config_earlier" in x and key in x for x in v), v)

    def test_planted_no_env_fails(self):
        def m(d):
            del earlier(d)["env"]
        fails(self, judge_config_earlier, m, "config_earlier")

    def test_planted_runs_fail(self):
        gh = ('gh api --paginate "repos/$GITHUB_REPOSITORY/actions/artifacts?per_page=100" --jq \'' + JQ
              + "' >\"$RUNNER_TEMP/a.jsonl\"")
        py = f'python3 {EARLIER} "$RUNNER_TEMP/a.jsonl"'
        runs = {
            "no || true": f"{gh}\n{py}",
            "|| false": f"{gh} || false\n{py}",
            "set +e instead": f"set +e\n{gh}\n{py}",
            "no --paginate": f"{gh.replace('--paginate ', '')} || true\n{py}",
            "other endpoint": f"{gh.replace('artifacts?per_page=100', 'runs?per_page=100')} || true\n{py}",
            "no per_page": f"{gh.replace('?per_page=100', '')} || true\n{py}",
            "other filter": f"{gh.replace('expired, ', '')} || true\n{py}",
            "filter without select": f"{gh.replace('select(.name | startswith(\"eval-measurements-\")) | ', '')} || true\n{py}",
            "other prefix in filter": f"{gh.replace('eval-measurements-', 'evals-')} || true\n{py}",
            "no jq": f'gh api --paginate "repos/$GITHUB_REPOSITORY/actions/artifacts?per_page=100" '
                     f'>"$RUNNER_TEMP/a.jsonl" || true\n{py}',
            "not written to a file": f'{gh.split(" >")[0]} || true\n{py}',
            "other file": f'{gh} || true\npython3 {EARLIER} "$RUNNER_TEMP/b.jsonl"',
            "no argument": f"{gh} || true\npython3 {EARLIER}",
            "script first": f"{py}\n{gh} || true",
            "no script": f"{gh} || true",
            "gh only in a comment": f"# {gh} || true\n{py}",
            "two gh calls": f"{gh} || true\n{gh} || true\n{py}",
        }
        for label, run in runs.items():
            with self.subTest(label):
                fails(self, judge_config_earlier, lambda d, r=run: earlier(d).update({"run": r}),
                      "config_earlier")


class EarlierOutputTest(unittest.TestCase):
    def test_real_workflow(self):
        self.assertEqual(judge_earlier_output(REPO), [])

    def test_planted_outputs_fail(self):
        for val in (None, "", "${{ steps.select.outputs.earlier }}", "${{ steps.earlier.outputs.matrix }}",
                    "${{ steps.pins.outputs.earlier }}", "${{ steps.earlier.outcome }}", "earlier",
                    "${{ needs.config.outputs.earlier }}", "[]"):
            with self.subTest(val):
                def m(d, v=val):
                    set_or_del(cfg(d)["outputs"], "earlier", v)
                fails(self, judge_earlier_output, m, "earlier_output")

    def test_planted_no_outputs_at_all_fails(self):
        def m(d):
            del cfg(d)["outputs"]
        fails(self, judge_earlier_output, m, "earlier_output")

    def test_planted_output_on_the_evals_job_only_fails(self):
        def m(d):
            del cfg(d)["outputs"]["earlier"]
            ev(d)["outputs"] = {"earlier": "${{ steps.earlier.outputs.earlier }}"}
        fails(self, judge_earlier_output, m, "earlier_output")

    def test_planted_step_without_id_fails(self):
        def m(d):
            del earlier(d)["id"]
        fails(self, judge_earlier_output, m, "earlier_output")

    def test_planted_renamed_step_id_with_old_output_fails(self):
        def m(d):
            earlier(d)["id"] = "baseline"
        fails(self, judge_earlier_output, m, "earlier_output")


class TrialsEarlierEnvTest(unittest.TestCase):
    def test_real_workflow(self):
        self.assertEqual(judge_trials_earlier_env(REPO), [])

    def test_planted_values_fail(self):
        for val in (None, "", "${{ needs.config.outputs.matrix }}", "${{ steps.earlier.outputs.earlier }}",
                    "${{ needs.config.outputs.earlier }}x", "${{ needs.evals.outputs.earlier }}",
                    "${{ secrets.EARLIER }}", "{}"):
            with self.subTest(val):
                fails(self, judge_trials_earlier_env,
                      lambda d, v=val: set_or_del(trials(d)["env"], "EARLIER", v), "trials_earlier_env")

    def test_planted_env_on_another_step_only_fails(self):
        def m(d):
            del trials(d)["env"]["EARLIER"]
            mscan(d)["env"] = {"EARLIER": "${{ needs.config.outputs.earlier }}"}
        fails(self, judge_trials_earlier_env, m, "trials_earlier_env")

    def test_planted_no_env_or_no_step_fails(self):
        def noenv(d):
            del trials(d)["env"]
        def nostep(d):
            steps_of(d).remove(trials(d))
        for m in (noenv, nostep):
            with self.subTest(m.__name__):
                fails(self, judge_trials_earlier_env, m, "trials_earlier_env")


class PermissionsUnchangedTest(unittest.TestCase):
    def test_real_workflow(self):
        self.assertEqual(judge_permissions_unchanged(REPO), [])

    def test_planted_widened_permissions_fail(self):
        def top(d, p):
            d["evals.yml"]["permissions"] = p
        def conf(d, p):
            cfg(d)["permissions"] = p
        def evj(d, p):
            ev(d)["permissions"] = p
        cases = {
            "top actions": (top, {"contents": "read", "actions": "read"}),
            "top write": (top, {"contents": "write"}),
            "config contents write": (conf, {"contents": "write", "actions": "read"}),
            "config actions write": (conf, {"contents": "read", "actions": "write"}),
            "config artifacts": (conf, {"contents": "read", "actions": "read", "artifact-metadata": "read"}),
            "config id-token": (conf, {"contents": "read", "actions": "read", "id-token": "write"}),
            "config without actions": (conf, {"contents": "read"}),
            "config write-all": (conf, "write-all"),
            "evals read": (evj, {"contents": "read"}),
            "evals actions": (evj, {"actions": "read"}),
            "evals empty": (evj, {}),
        }
        for label, (fn, p) in cases.items():
            with self.subTest(label):
                fails(self, judge_permissions_unchanged, lambda d, f=fn, q=p: f(d, q),
                      "permissions_unchanged")

    def test_planted_permissions_removed_fail(self):
        def top(d):
            del d["evals.yml"]["permissions"]
        def conf(d):
            del cfg(d)["permissions"]
        for m in (top, conf):
            with self.subTest(m.__name__):
                fails(self, judge_permissions_unchanged, m, "permissions_unchanged")


class MissingFileTest(unittest.TestCase):
    def test_missing_evals_file_fails(self):
        def m(d):
            del d["evals.yml"]
        for fn in RULES:
            with self.subTest(fn.__name__):
                self.assertTrue(fn(planted(self, m)))

    def test_workflow_without_jobs_fails(self):
        def m(d):
            d["evals.yml"]["jobs"] = {}
        for fn in RULES:
            with self.subTest(fn.__name__):
                self.assertTrue(fn(planted(self, m)))


if __name__ == "__main__":
    unittest.main()
