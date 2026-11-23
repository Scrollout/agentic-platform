"""A failed live eval job keeps its outputs, scanned for secrets first (T122; research D46,
ADR 0050).

Workflow rules, each a judge_<rule>(root) over root/.github/workflows, with a test on the real
files and a planted violation in a temporary tree that is valid otherwise (a baseline test proves
it valid):

- only_evals_uploads: the only steps in all workflows that use an upload-artifact action are in
  evals.yml's evals job: exactly one failed-outputs upload (`with.name` starts `evals-`) and at
  most one measurements upload (`with.name` starts `eval-measurements-`, feature 008, research
  R2); an upload with any other name, or without a name, fails. The other rules below judge the
  failed-outputs upload, found by its name, not as the first upload;
- upload_pinned: that step uses actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9
  (v7.0.2);
- upload_with: path `${{ env.EVAL_REPORT_DIR }}`, retention-days 7, if-no-files-found warn, and a
  name `evals-...` built from matrix.suite, matrix.cases and github.run_attempt;
- upload_last: the upload is the evals job's last step, and the scan step comes after the step
  that runs the live evals (id `run`) and before the upload;
- scan_step: a step with an `id` and `if: failure() || steps.failed.outputs.failed == 'true'`
  (feature 005, ADR 0078; the `${{ }}` form and either quote pass) runs `scripts/secret-scan.sh`
  on $EVAL_REPORT_DIR; it holds no copy of the patterns, and nothing hides its exit
  (continue-on-error, `|| true`, `|| :`, `set +e`);
- upload_condition: the upload's `if` is `(failure() || steps.failed.outputs.failed == 'true') &&
  steps.<scan id>.outcome == 'success'` (the `${{ }}` form and either quote pass; the parentheses
  are required, since `&&` binds tighter than `||`), so nothing is uploaded when the scan
  matched.

The scan's one home (interface chosen for T122): `scripts/secret-scan.sh [DIR]`.
- With DIR it scans every file under DIR, recursively, binary files included, for the patterns
  of check.sh's secrets step; it prints what it examined (DIR) and, on a match, the matching
  files' paths, never the matched text (the run's log is public); exit 1 on a match, 0 otherwise.
- Without DIR it scans the tracked files of the git repository in the current directory, as
  check.sh's `secrets` step did (`git grep`), with the same exit codes.
- check.sh's `s_secrets` calls it and holds no copy of the patterns.

Fixture keys are built at run time by concatenation, so this file does not match the patterns.
The loading helpers are scripts/tests/test_workflows.py's, imported unchanged.
"""
import copy
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

import yaml

from gitenv import clean_env, make_home
from test_workflows import jobs, load, need_files

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
SCAN = REPO / "scripts" / "secret-scan.sh"
CHECK_SH = REPO / "scripts" / "check.sh"
PIN = "actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9"
HASH = "a" * 40
UPLOAD = re.compile(r"upload-artifact", re.I)
SCAN_CALL = re.compile(r"(?:^|[\s;&|(])(?:\./)?scripts/secret-scan\.sh\s+\"?\$\{?EVAL_REPORT_DIR\}?\"?(?:\s|$|;)")
COPY = re.compile(r"sk-ant|ghp_")
SWALLOW = re.compile(r"\|\|\s*(?:true|:)(?:\s|$|;|\))|\bset\s+\+e\b")
PATH_EXPR = re.compile(r"\s*\$\{\{\s*env\.EVAL_REPORT_DIR\s*\}\}\s*")
SCAN_IF = re.compile(r"failure\(\)\s*\|\|\s*steps\.failed\.outputs\.failed\s*==\s*(['\"])true\1")
KEY = "sk-" + "ant-" + "api03-" + "Q" * 24 + "_x-9"
GH_KEY = "gh" + "p_" + "Z9" * 18


def strip_expr(cond):
    c = str(cond).strip()
    m = re.fullmatch(r"\$\{\{(.*)\}\}", c, re.S)
    return (m.group(1) if m else c).strip()


def is_upload(st):
    return bool(UPLOAD.search(str(st.get("uses", ""))))


def upload_name(st):
    w = st.get("with")
    return str(w.get("name")) if isinstance(w, dict) and w.get("name") is not None else ""


def is_failed_outputs_upload(st):
    return is_upload(st) and upload_name(st).startswith("evals-")


def is_measurements_upload(st):
    return is_upload(st) and upload_name(st).startswith("eval-measurements-")


def upload_steps(wf):
    out = []
    for name, w in wf.items():
        for jn, job in jobs(w).items():
            for i, st in enumerate(job.get("steps") or []):
                if UPLOAD.search(str(st.get("uses", ""))):
                    out.append((name, jn, i, st))
    return out


def evals_steps(wf):
    return (jobs(wf.get("evals.yml") or {}).get("evals") or {}).get("steps") or []


def the_upload(root):
    """(evals job steps, index of the upload, the upload step, violations)."""
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return [], -1, None, v
    steps = evals_steps(wf)
    ups = [i for i, st in enumerate(steps) if is_failed_outputs_upload(st)]
    if not ups:
        return steps, -1, None, ["evals.yml's evals job has no failed-outputs upload "
                                 "(an upload-artifact step named evals-...)"]
    return steps, ups[0], steps[ups[0]], []


def scan_index(steps):
    for i, st in enumerate(steps):
        if "scripts/secret-scan.sh" in str(st.get("run", "")):
            return i
    return -1


# ---------------------------------------------------------------- rules

def judge_only_evals_uploads(root):
    wf = load(root)
    v = need_files(wf, "check.yml", "evals.yml")
    ups = upload_steps(wf)
    for name, jn, _, _ in ups:
        if (name, jn) != ("evals.yml", "evals"):
            v.append(f"{name} job {jn} uploads an artifact")
    mine = [st for n, j, _, st in ups if (n, j) == ("evals.yml", "evals")]
    failed = [st for st in mine if is_failed_outputs_upload(st)]
    meas = [st for st in mine if is_measurements_upload(st)]
    if not failed:
        v.append("evals.yml's evals job has no failed-outputs upload (name evals-...)")
    if len(failed) > 1:
        v.append("evals.yml's evals job has more than one failed-outputs upload")
    if len(meas) > 1:
        v.append("evals.yml's evals job has more than one measurements upload")
    for st in mine:
        if st not in failed and st not in meas:
            v.append(f"evals.yml's evals job uploads under the name {upload_name(st)!r}, "
                     "neither evals-... nor eval-measurements-...")
    return v


def judge_upload_pinned(root):
    _, _, st, v = the_upload(root)
    if st is not None and st.get("uses") != PIN:
        v.append(f"the upload uses {st.get('uses')!r}, not {PIN}")
    return v


def judge_upload_with(root):
    _, _, st, v = the_upload(root)
    if st is None:
        return v
    w = st.get("with") or {}
    if not PATH_EXPR.fullmatch(str(w.get("path", ""))):
        v.append(f"the upload's path is {w.get('path')!r}, not ${{{{ env.EVAL_REPORT_DIR }}}}")
    if str(w.get("retention-days", "")).strip() != "7":
        v.append(f"the upload's retention-days is {w.get('retention-days')!r}, not 7")
    if str(w.get("if-no-files-found", "")).strip() != "warn":
        v.append(f"the upload's if-no-files-found is {w.get('if-no-files-found')!r}, not warn")
    name = str(w.get("name", ""))
    if not name.startswith("evals-"):
        v.append(f"the upload's name {name!r} does not start with evals-")
    exprs = re.findall(r"\$\{\{(.*?)\}\}", name, re.S)
    for part in (r"matrix\.suite", r"matrix\.cases", r"github\.run_attempt"):
        if not any(re.search(r"\b" + part + r"\b", e) for e in exprs):
            v.append(f"the upload's name {name!r} is not built from {part.replace(chr(92), '')}")
    return v


def judge_upload_last(root):
    steps, i, st, v = the_upload(root)
    if st is None:
        return v
    if i != len(steps) - 1:
        v.append("the upload is not the evals job's last step")
    s = scan_index(steps)
    run = [k for k, x in enumerate(steps) if x.get("id") == "run"]
    if s < 0 or s > i:
        v.append("no secret scan step before the upload")
    elif not run or s < run[0]:
        v.append("the secret scan does not come after the live evals step (id run)")
    return v


def judge_scan_step(root):
    steps, i, st, v = the_upload(root)
    if st is None:
        return v
    s = scan_index(steps[:i])
    if s < 0:
        return v + ["no step before the upload runs scripts/secret-scan.sh"]
    sc = steps[s]
    run = str(sc.get("run", ""))
    if not sc.get("id"):
        v.append("the scan step has no id")
    if not SCAN_IF.fullmatch(strip_expr(sc.get("if", ""))):
        v.append(f"the scan step's if is {sc.get('if')!r}, not "
                 "failure() || steps.failed.outputs.failed == 'true'")
    if not any(SCAN_CALL.search(l) for l in run.splitlines()):
        v.append("the scan step does not run scripts/secret-scan.sh on $EVAL_REPORT_DIR")
    if COPY.search(run) or COPY.search(str(sc.get("env", ""))):
        v.append("the scan step holds a copy of the secret patterns")
    if SWALLOW.search(run) or sc.get("continue-on-error") not in (None, False, "false"):
        v.append("the scan step hides the scan's exit")
    return v


def judge_upload_condition(root):
    steps, i, st, v = the_upload(root)
    if st is None:
        return v
    s = scan_index(steps[:i])
    sid = steps[s].get("id") if s >= 0 else None
    if not sid:
        return v + ["no scan step with an id before the upload"]
    want = re.compile(r"\(\s*failure\(\)\s*\|\|\s*steps\.failed\.outputs\.failed\s*==\s*(['\"])true\1\s*\)"
                      r"\s*&&\s*steps\." + re.escape(str(sid))
                      + r"\.outcome\s*==\s*(['\"])success\2")
    cond = strip_expr(st.get("if", ""))
    if not want.fullmatch(cond):
        v.append(f"the upload's if is {st.get('if')!r}, not (failure() || "
                 f"steps.failed.outputs.failed == 'true') && steps.{sid}.outcome == 'success'")
    return v


RULES = (judge_only_evals_uploads, judge_upload_pinned, judge_upload_with, judge_upload_last,
         judge_scan_step, judge_upload_condition)


def judge_one_home(check_sh_text):
    """check.sh's s_secrets calls scripts/secret-scan.sh; check.sh holds no copy of the patterns."""
    v = []
    m = re.search(r"^s_secrets\(\)\s*\{(.*?)^\}", check_sh_text, re.S | re.M)
    if not m:
        return ["check.sh has no s_secrets function"]
    if not re.search(r"(?:^|[\s;&|(])(?:\./)?(?:\"?\$\w+\"?/)?scripts/secret-scan\.sh\b", m.group(1)):
        v.append("s_secrets does not call scripts/secret-scan.sh")
    if COPY.search(check_sh_text):
        v.append("check.sh holds a copy of the secret patterns")
    return v


# ---------------------------------------------------------------- planted tree

CHECKOUT = {"uses": f"actions/checkout@{HASH}", "with": {"persist-credentials": False}}
def SCAN_STEP():
    return {"id": "scan", "name": "Scan the report for secrets",
            "if": "failure() || steps.failed.outputs.failed == 'true'",
            "run": "scripts/secret-scan.sh \"$EVAL_REPORT_DIR\""}


UPLOAD_STEP = lambda: {
    "name": "Keep the failed job's outputs", "uses": PIN,
    "if": "(failure() || steps.failed.outputs.failed == 'true') && steps.scan.outcome == 'success'",
    "with": {"name": "evals-${{ matrix.suite }}${{ matrix.cases && format('-{0}', matrix.cases) "
                     "|| '' }}-${{ github.run_attempt }}",
             "path": "${{ env.EVAL_REPORT_DIR }}", "retention-days": 7,
             "if-no-files-found": "warn"}}


def good_evals():
    ev = {"needs": "config", "runs-on": "${{ needs.config.outputs.runner }}",
          "environment": "dev",
          "env": {"EVAL_REPORT_DIR": "${{ github.workspace }}/../eval-report"},
          "steps": [copy.deepcopy(CHECKOUT),
                    {"name": "Write the key file", "run": "umask 077\nprintf x >\"$RUNNER_TEMP/k\""},
                    {"id": "run", "run": "sh src/tests/evals/steps/run.sh live"},
                    {"if": "always()", "run": "python3 scripts/ci-evals-summary.py \"$EVAL_REPORT_DIR/report.json\""},
                    {"if": "always()", "run": "rm -f \"$RUNNER_TEMP/k\""},
                    SCAN_STEP(), UPLOAD_STEP()]}
    return {"name": "evals", "on": {"workflow_dispatch": None},
            "permissions": {"contents": "read"},
            "jobs": {"config": {"runs-on": "ubuntu-latest", "steps": [copy.deepcopy(CHECKOUT)]},
                     "evals": ev}}


def good_check():
    return {"name": "check", "on": {"push": None}, "permissions": {"contents": "read"},
            "jobs": {"config": {"runs-on": "ubuntu-latest", "steps": [copy.deepcopy(CHECKOUT)]},
                     "py_tests": {"runs-on": "x", "steps": [copy.deepcopy(CHECKOUT),
                                                            {"run": "scripts/check.sh --only py-tests"}]}}}


def planted(tc, mutate=None):
    tmp = tempfile.TemporaryDirectory()
    tc.addCleanup(tmp.cleanup)
    root = Path(tmp.name)
    (root / ".github" / "workflows").mkdir(parents=True)
    d = {"check.yml": good_check(), "evals.yml": good_evals()}
    if mutate:
        mutate(d)
    for n, w in d.items():
        (root / ".github" / "workflows" / n).write_text(yaml.safe_dump(w, sort_keys=False))
    return root


def ev(d):
    return d["evals.yml"]["jobs"]["evals"]


def up(d):
    return ev(d)["steps"][-1]


def sc(d):
    return ev(d)["steps"][-2]


# ---------------------------------------------------------------- workflow tests

class BaselineTest(unittest.TestCase):
    def test_good_tree_passes_every_rule(self):
        root = planted(self)
        for fn in RULES:
            self.assertEqual(fn(root), [], fn.__name__)

    def test_expression_and_quote_forms_pass(self):
        def m(d):
            up(d)["if"] = ("${{ (failure() || steps.failed.outputs.failed == \"true\") "
                           "&& steps.scan.outcome == \"success\" }}")
            sc(d)["if"] = "${{ failure() || steps.failed.outputs.failed == \"true\" }}"
            sc(d)["run"] = "./scripts/secret-scan.sh ${EVAL_REPORT_DIR}"
            up(d)["with"]["retention-days"] = "7"
        root = planted(self, m)
        for fn in RULES:
            self.assertEqual(fn(root), [], fn.__name__)


class OnlyEvalsUploadsTest(unittest.TestCase):
    def test_real_only_the_evals_job_uploads(self):
        self.assertEqual(judge_only_evals_uploads(REPO), [])

    def test_planted_upload_in_check_fails(self):
        def m(d):
            d["check.yml"]["jobs"]["py_tests"]["steps"].append(UPLOAD_STEP())
        self.assertTrue(judge_only_evals_uploads(planted(self, m)))

    def test_planted_upload_in_evals_config_fails(self):
        def m(d):
            d["evals.yml"]["jobs"]["config"]["steps"].append(UPLOAD_STEP())
        self.assertTrue(judge_only_evals_uploads(planted(self, m)))

    def test_planted_other_upload_action_fails(self):
        def m(d):
            d["check.yml"]["jobs"]["py_tests"]["steps"].append(
                {"uses": f"someone/upload-artifact-fork@{HASH}", "with": {"path": "."}})
        self.assertTrue(judge_only_evals_uploads(planted(self, m)))

    def test_planted_second_failed_outputs_upload_in_evals_fails(self):
        def m(d):
            ev(d)["steps"].insert(3, UPLOAD_STEP())
        self.assertTrue(judge_only_evals_uploads(planted(self, m)))

    def test_planted_upload_with_another_name_fails(self):
        for name in ("other-${{ github.run_id }}", "eval-measurement-x", "measurements-x", ""):
            with self.subTest(name=name):
                def m(d, name=name):
                    st = UPLOAD_STEP()
                    st["with"]["name"] = name
                    ev(d)["steps"].insert(3, st)
                self.assertTrue(judge_only_evals_uploads(planted(self, m)))

    def test_planted_upload_without_a_name_fails(self):
        def m(d):
            st = UPLOAD_STEP()
            del st["with"]["name"]
            ev(d)["steps"].insert(3, st)
        self.assertTrue(judge_only_evals_uploads(planted(self, m)))

    def test_planted_upload_without_with_fails(self):
        def m(d):
            ev(d)["steps"].insert(3, {"uses": PIN})
        self.assertTrue(judge_only_evals_uploads(planted(self, m)))

    def test_one_measurements_upload_in_evals_passes(self):
        # the name is built from expressions, and the first upload is the measurements one
        def m(d):
            st = UPLOAD_STEP()
            st["with"]["name"] = "eval-measurements-${{ matrix.suite }}-${{ github.run_attempt }}"
            ev(d)["steps"].insert(3, st)
        root = planted(self, m)
        self.assertEqual(judge_only_evals_uploads(root), [])
        # the other rules still judge the failed-outputs upload, not the first upload
        for fn in (judge_upload_pinned, judge_upload_with, judge_upload_condition):
            self.assertEqual(fn(root), [], fn.__name__)

    def test_planted_two_measurements_uploads_fail(self):
        def m(d):
            for k in (3, 4):
                st = UPLOAD_STEP()
                st["with"]["name"] = f"eval-measurements-{k}"
                ev(d)["steps"].insert(k, st)
        self.assertTrue(judge_only_evals_uploads(planted(self, m)))

    def test_planted_measurements_upload_outside_evals_fails(self):
        def put(d, where):
            st = UPLOAD_STEP()
            st["with"]["name"] = "eval-measurements-x"
            where(d).append(st)
        for where in (lambda d: d["check.yml"]["jobs"]["py_tests"]["steps"],
                      lambda d: d["evals.yml"]["jobs"]["config"]["steps"]):
            with self.subTest():
                self.assertTrue(judge_only_evals_uploads(planted(self, lambda d, where=where: put(d, where))))

    def test_planted_only_a_measurements_upload_fails(self):
        def m(d):
            up(d)["with"]["name"] = "eval-measurements-x"
        self.assertTrue(judge_only_evals_uploads(planted(self, m)))

    def test_planted_no_upload_fails(self):
        def m(d):
            ev(d)["steps"].pop()
        self.assertTrue(judge_only_evals_uploads(planted(self, m)))


class UploadPinnedTest(unittest.TestCase):
    def test_real_pin(self):
        self.assertEqual(judge_upload_pinned(REPO), [])

    def test_planted_other_refs_fail(self):
        for ref in ("actions/upload-artifact@v7", "actions/upload-artifact@v7.0.2",
                    "actions/upload-artifact@" + "b" * 40, "actions/upload-artifact@cf430e0"):
            with self.subTest(ref):
                def m(d, ref=ref):
                    up(d)["uses"] = ref
                self.assertTrue(judge_upload_pinned(planted(self, m)))


class UploadWithTest(unittest.TestCase):
    def test_real_with(self):
        self.assertEqual(judge_upload_with(REPO), [])

    def test_planted_each_wrong_input_fails(self):
        cases = [("path", "eval-report"), ("path", "${{ runner.temp }}"),
                 ("path", "${{ env.EVAL_REPORT_DIR }}/report.json"), ("path", None),
                 ("retention-days", 90), ("retention-days", 1), ("retention-days", None),
                 ("if-no-files-found", "error"), ("if-no-files-found", "ignore"),
                 ("if-no-files-found", None),
                 ("name", "evals-${{ matrix.suite }}-${{ github.run_attempt }}"),
                 ("name", "evals-${{ matrix.suite }}${{ matrix.cases }}"),
                 ("name", "report-${{ matrix.suite }}-${{ matrix.cases }}-${{ github.run_attempt }}"),
                 ("name", "evals-matrix.suite-matrix.cases-github.run_attempt"), ("name", None)]
        for key, val in cases:
            with self.subTest(f"{key}={val}"):
                def m(d, key=key, val=val):
                    if val is None:
                        del up(d)["with"][key]
                    else:
                        up(d)["with"][key] = val
                v = judge_upload_with(planted(self, m))
                self.assertTrue(any(key in x for x in v), v)


class UploadLastTest(unittest.TestCase):
    def test_real_upload_last(self):
        self.assertEqual(judge_upload_last(REPO), [])

    def test_planted_step_after_the_upload_fails(self):
        def m(d):
            ev(d)["steps"].append({"if": "always()", "run": "echo done"})
        self.assertTrue(judge_upload_last(planted(self, m)))

    def test_planted_scan_after_the_upload_fails(self):
        def m(d):
            s = ev(d)["steps"]
            s[-1], s[-2] = s[-2], s[-1]
        self.assertTrue(judge_upload_last(planted(self, m)))

    def test_planted_scan_before_the_evals_fails(self):
        def m(d):
            s = ev(d)["steps"]
            s.insert(2, s.pop(-2))
        self.assertTrue(judge_upload_last(planted(self, m)))


class ScanStepTest(unittest.TestCase):
    def test_real_scan_step(self):
        self.assertEqual(judge_scan_step(REPO), [])

    def test_planted_no_scan_fails(self):
        def m(d):
            del ev(d)["steps"][-2]
        self.assertTrue(judge_scan_step(planted(self, m)))

    def test_planted_no_id_fails(self):
        def m(d):
            del sc(d)["id"]
        self.assertTrue(judge_scan_step(planted(self, m)))

    def test_planted_other_conditions_fail(self):
        for cond in (None, "always()", "success()", "failure() || cancelled()", "failure()",
                     "${{ failure() }}", "steps.failed.outputs.failed == 'true'",
                     "failure() && steps.failed.outputs.failed == 'true'",
                     "failure() || steps.failed.outputs.failed == 'false'",
                     "failure() || steps.run.outputs.failed == 'true'"):
            with self.subTest(cond):
                def m(d, cond=cond):
                    if cond is None:
                        del sc(d)["if"]
                    else:
                        sc(d)["if"] = cond
                self.assertTrue(judge_scan_step(planted(self, m)))

    def test_planted_copied_patterns_fail(self):
        pat = "sk-" + "ant-[A-Za-z0-9_-]{20,}|gh" + "p_[A-Za-z0-9]{30,}"
        def m(d):
            sc(d)["run"] = f"! grep -rnE '{pat}' \"$EVAL_REPORT_DIR\"\n: scripts/secret-scan.sh \"$EVAL_REPORT_DIR\""
        self.assertTrue(judge_scan_step(planted(self, m)))

    def test_planted_scan_of_another_folder_fails(self):
        for run in ("scripts/secret-scan.sh \"$RUNNER_TEMP\"", "scripts/secret-scan.sh",
                    "scripts/secret-scan.sh src"):
            with self.subTest(run):
                def m(d, run=run):
                    sc(d)["run"] = run
                self.assertTrue(judge_scan_step(planted(self, m)))

    def test_planted_hidden_exit_fails(self):
        for change in ({"run": "scripts/secret-scan.sh \"$EVAL_REPORT_DIR\" || true"},
                       {"run": "set +e\nscripts/secret-scan.sh \"$EVAL_REPORT_DIR\""},
                       {"continue-on-error": True}):
            with self.subTest(change):
                def m(d, change=change):
                    sc(d).update(change)
                self.assertTrue(judge_scan_step(planted(self, m)))


class UploadConditionTest(unittest.TestCase):
    def test_real_condition(self):
        self.assertEqual(judge_upload_condition(REPO), [])

    def test_planted_conditions_without_the_outcome_fail(self):
        for cond in (None, "failure()", "${{ failure() }}", "always()",
                     "failure() && steps.scan.outcome == 'success'",
                     "failure() || steps.failed.outputs.failed == 'true' && steps.scan.outcome == 'success'",
                     "failure() || steps.failed.outputs.failed == 'true'",
                     "(failure() || steps.failed.outputs.failed == 'true') || steps.scan.outcome == 'success'",
                     "steps.failed.outputs.failed == 'true' && steps.scan.outcome == 'success'",
                     "always() && steps.scan.outcome == 'success'",
                     "failure() && steps.run.outcome == 'success'",
                     "failure() && steps.scan.outcome != 'success'",
                     "failure() || steps.scan.outcome == 'success'",
                     "failure() && steps.scan.conclusion == 'success'",
                     "steps.scan.outcome == 'success'"):
            with self.subTest(cond):
                def m(d, cond=cond):
                    if cond is None:
                        del up(d)["if"]
                    else:
                        up(d)["if"] = cond
                self.assertTrue(judge_upload_condition(planted(self, m)))

    def test_condition_follows_the_scan_id(self):
        def m(d):
            sc(d)["id"] = "secrets"
            up(d)["if"] = ("(failure() || steps.failed.outputs.failed == 'true') "
                           "&& steps.secrets.outcome == 'success'")
        self.assertEqual(judge_upload_condition(planted(self, m)), [])


# ---------------------------------------------------------------- the scan itself

def scan(tc, *args, cwd=None):
    tc.assertTrue(SCAN.exists(), f"{SCAN} does not exist")
    env = clean_env(make_home(tc))
    r = subprocess.run(["sh", str(SCAN), *map(str, args)], cwd=cwd or REPO, env=env,
                       capture_output=True, text=True, timeout=60)
    return r.returncode, r.stdout + r.stderr


def folder(tc, files):
    tmp = tempfile.TemporaryDirectory()
    tc.addCleanup(tmp.cleanup)
    root = Path(tmp.name) / "eval-report"
    root.mkdir()
    for rel, data in files.items():
        p = root / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_bytes(data if isinstance(data, bytes) else data.encode())
    return root


CLEAN = {"report.json": '{"cases": [{"id": "inject-skip-links", "pass": false}]}\n',
         "analyst/inject-skip-links/trial-1.out": "Feature: notes\n  Rule: R-1\n",
         "analyst/inject-skip-links/trial-1.ndjson": '{"type": "token_usage"}\n'}


class ScanFolderTest(unittest.TestCase):
    def test_clean_folder_passes_and_says_what_it_examined(self):
        root = folder(self, CLEAN)
        code, out = scan(self, root)
        self.assertEqual(code, 0, out)
        self.assertIn(str(root), out)

    def test_empty_folder_passes(self):
        code, out = scan(self, folder(self, {}))
        self.assertEqual(code, 0, out)

    def test_planted_key_fails_naming_the_file_not_the_key(self):
        for key in (KEY, GH_KEY):
            with self.subTest(key[:4]):
                files = dict(CLEAN)
                files["steps/review/r1 trial/x.ndjson"] = '{"content": "token ' + key + '"}\n'
                code, out = scan(self, folder(self, files))
                self.assertEqual(code, 1, out)
                self.assertIn("x.ndjson", out)
                self.assertNotIn(key, out)
                self.assertNotIn(key[:20], out)

    def test_planted_key_in_a_binary_file_fails(self):
        files = dict(CLEAN, **{"blob.bin": b"\x00\x01\xff" + KEY.encode() + b"\x00"})
        code, out = scan(self, folder(self, files))
        self.assertEqual(code, 1, out)

    def test_near_misses_pass(self):
        for text in ("sk-" + "ant-" + "short", "gh" + "p_" + "A" * 20, "sk-" + "ant " + "Q" * 30):
            with self.subTest(text):
                code, out = scan(self, folder(self, dict(CLEAN, **{"n.txt": text + "\n"})))
                self.assertEqual(code, 0, out)


class ScanRepositoryTest(unittest.TestCase):
    """Without a folder the scan judges the tracked files, as check.sh's secrets step does."""

    def repo(self, files):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = Path(tmp.name)
        env = clean_env(make_home(self))
        for args in (["init", "-q"], ["config", "user.email", "t@example.invalid"],
                     ["config", "user.name", "t"]):
            subprocess.run(["git", *args], cwd=root, env=env, check=True)
        for rel, text in files.items():
            (root / rel).parent.mkdir(parents=True, exist_ok=True)
            (root / rel).write_text(text)
        subprocess.run(["git", "add", "-A"], cwd=root, env=env, check=True)
        return root

    def test_clean_repository_passes(self):
        code, out = scan(self, cwd=self.repo({"a.md": "x\n"}))
        self.assertEqual(code, 0, out)

    def test_planted_tracked_key_fails(self):
        code, out = scan(self, cwd=self.repo({"a.md": "x\n", "src/k.txt": KEY + "\n"}))
        self.assertEqual(code, 1, out)

    def test_untracked_key_is_not_judged(self):
        root = self.repo({"a.md": "x\n"})
        (root / "untracked.txt").write_text(KEY + "\n")
        code, out = scan(self, cwd=root)
        self.assertEqual(code, 0, out)


class OneHomeTest(unittest.TestCase):
    def test_real_check_sh_calls_the_shared_scan(self):
        self.assertEqual(judge_one_home(CHECK_SH.read_text()), [])

    def test_real_scan_holds_the_patterns(self):
        self.assertTrue(SCAN.exists(), f"{SCAN} does not exist")
        self.assertTrue(COPY.search(SCAN.read_text()), "secret-scan.sh names no pattern")

    def test_planted_copy_in_check_sh_fails(self):
        pat = "sk-" + "ant-[A-Za-z0-9_-]{20,}"
        text = f"s_secrets() {{\n  scripts/secret-scan.sh\n}}\nx='{pat}'\n"
        self.assertTrue(judge_one_home(text))

    def test_planted_s_secrets_with_its_own_grep_fails(self):
        pat = "sk-" + "ant-[A-Za-z0-9_-]{20,}"
        self.assertTrue(judge_one_home(f"s_secrets() {{\n  git grep -nE '{pat}'\n}}\n"))
        self.assertTrue(judge_one_home("s_secrets() {\n  git grep -nE \"$P\" -- .\n}\n"))

    def test_clean_s_secrets_passes(self):
        self.assertEqual(judge_one_home("s_secrets() {\n  scripts/secret-scan.sh\n}\n"), [])
        self.assertEqual(judge_one_home("s_secrets() {\n  \"$root\"/scripts/secret-scan.sh\n}\n"), [])


if __name__ == "__main__":
    unittest.main()
