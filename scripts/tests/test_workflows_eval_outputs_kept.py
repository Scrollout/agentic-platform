"""An eval job keeps its outputs when any trial failed (feature 005 T003; research R1 and R2,
ADR 0078), written before the workflow change.

Workflow rules, each a judge_<rule>(root) over root/.github/workflows, with a test on the real
files and a planted violation in a temporary tree that is valid otherwise (a baseline test proves
it valid):

- report_dir_path: the value of EVAL_REPORT_DIR in the evals job's env has no `.` or `..` path
  segment (actions/upload-artifact refuses one; a `${{ }}` expression counts as one name);
- failed_step: a step with `id: failed` and `if: always()` runs
  `python3 scripts/ci-evals-failed-trials.py "$EVAL_REPORT_DIR"`, after the step with id `run`
  and before the secret scan;
- scan_condition: the scan's `if` is `failure() || steps.failed.outputs.failed == 'true'`;
- upload_condition_failed_trial: the upload's `if` is `(failure() || steps.failed.outputs.failed ==
  'true') && steps.<scan id>.outcome == 'success'`; without the parentheses, or without the scan
  clause, a scan match would upload or a failed trial would not.

The `${{ }}` form and either quote pass. test_workflows_eval_artifacts.py keeps the rules on the
upload's pin, inputs and place.
"""
import copy
import re
import tempfile
import unittest
from pathlib import Path

import yaml

from test_workflows import jobs, load, need_files
from test_workflows_eval_artifacts import (REPO, UPLOAD, good_check, good_evals, scan_index,
                                           strip_expr)

NEW_DIR = "${{ github.workspace }}-eval-report"
FAILED_IF = "failure() || steps.failed.outputs.failed == 'true'"
FAILED_RUN = 'python3 scripts/ci-evals-failed-trials.py "$EVAL_REPORT_DIR"'
EXPR = re.compile(r"\$\{\{.*?\}\}", re.S)
SCAN_COND = re.compile(r"failure\(\)\s*\|\|\s*steps\.failed\.outputs\.failed\s*==\s*(['\"])true\1")
FAILED_CALL = re.compile(
    r"python3\s+scripts/ci-evals-failed-trials\.py\s+(?:\"\$EVAL_REPORT_DIR\"|\"\$\{EVAL_REPORT_DIR\}\""
    r"|\$EVAL_REPORT_DIR|\$\{EVAL_REPORT_DIR\})\s*")
ALWAYS = re.compile(r"\s*(?:\$\{\{\s*always\(\)\s*\}\}|always\(\))\s*")


def evals_job(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return None, v
    job = jobs(wf["evals.yml"]).get("evals")
    return (job, []) if job else (None, ["evals.yml has no evals job"])


def steps_of(root):
    job, v = evals_job(root)
    return (job.get("steps") or [], v) if job else ([], v)


# ---------------------------------------------------------------- rules

def judge_report_dir_path(root):
    job, v = evals_job(root)
    if job is None:
        return v
    val = (job.get("env") or {}).get("EVAL_REPORT_DIR")
    if val is None or not str(val).strip():
        return ["the evals job's env has no EVAL_REPORT_DIR"]
    flat = EXPR.sub("X", str(val))
    bad = [s for s in flat.split("/") if s in (".", "..")]
    if bad:
        v.append(f"EVAL_REPORT_DIR is {val!r}: it has a {bad[0]!r} path segment, which "
                 "actions/upload-artifact refuses")
    return v


def judge_failed_step(root):
    steps, v = steps_of(root)
    if v:
        return v
    idx = [i for i, st in enumerate(steps) if st.get("id") == "failed"]
    if len(idx) != 1:
        return [f"the evals job has {len(idx)} steps with id failed, not 1"]
    i, st = idx[0], steps[idx[0]]
    if not ALWAYS.fullmatch(str(st.get("if", ""))):
        v.append(f"the failed step's if is {st.get('if')!r}, not always()")
    lines = [ln.strip() for ln in str(st.get("run", "")).splitlines() if ln.strip()]
    if not any(FAILED_CALL.fullmatch(ln) for ln in lines):
        v.append(f"the failed step does not run {FAILED_RUN}")
    run = [k for k, x in enumerate(steps) if x.get("id") == "run"]
    s = scan_index(steps)
    if not run or i < run[0]:
        v.append("the failed step does not come after the live evals step (id run)")
    if s < 0 or i > s:
        v.append("the failed step does not come before the secret scan")
    return v


def judge_scan_condition(root):
    steps, v = steps_of(root)
    if v:
        return v
    s = scan_index(steps)
    if s < 0:
        return ["no step runs scripts/secret-scan.sh"]
    if not SCAN_COND.fullmatch(strip_expr(steps[s].get("if", ""))):
        v.append(f"the scan's if is {steps[s].get('if')!r}, not {FAILED_IF}")
    return v


def judge_upload_condition_failed_trial(root):
    steps, v = steps_of(root)
    if v:
        return v
    ups = [i for i, st in enumerate(steps) if UPLOAD.search(str(st.get("uses", "")))
           and str((st.get("with") or {}).get("name", "")).startswith("evals-")]
    if not ups:
        return ["the evals job has no failed-outputs upload (an upload-artifact step named evals-...)"]
    s = scan_index(steps[:ups[0]])
    sid = steps[s].get("id") if s >= 0 else None
    if not sid:
        return ["no scan step with an id before the upload"]
    want = re.compile(r"\(\s*failure\(\)\s*\|\|\s*steps\.failed\.outputs\.failed\s*==\s*(['\"])true\1\s*\)"
                      r"\s*&&\s*steps\." + re.escape(str(sid)) + r"\.outcome\s*==\s*(['\"])success\2")
    cond = strip_expr(steps[ups[0]].get("if", ""))
    if not want.fullmatch(cond):
        v.append(f"the upload's if is {steps[ups[0]].get('if')!r}, not ({FAILED_IF}) && "
                 f"steps.{sid}.outcome == 'success'")
    return v


RULES = (judge_report_dir_path, judge_failed_step, judge_scan_condition,
         judge_upload_condition_failed_trial)


# ---------------------------------------------------------------- planted tree

def good_kept():
    d = good_evals()
    ev = d["jobs"]["evals"]
    ev["env"]["EVAL_REPORT_DIR"] = NEW_DIR
    steps = ev["steps"]
    steps.insert(-2, {"id": "failed", "name": "Any failed trial?", "if": "always()", "run": FAILED_RUN})
    steps[-2]["if"] = FAILED_IF
    steps[-1]["if"] = f"({FAILED_IF}) && steps.scan.outcome == 'success'"
    return d


def planted(tc, mutate=None):
    tmp = tempfile.TemporaryDirectory()
    tc.addCleanup(tmp.cleanup)
    root = Path(tmp.name)
    (root / ".github" / "workflows").mkdir(parents=True)
    d = {"check.yml": good_check(), "evals.yml": good_kept()}
    if mutate:
        mutate(d)
    for n, w in d.items():
        (root / ".github" / "workflows" / n).write_text(yaml.safe_dump(w, sort_keys=False))
    return root


def ev(d):
    return d["evals.yml"]["jobs"]["evals"]


def step(d, id_):
    return next(s for s in ev(d)["steps"] if s.get("id") == id_)


def upload(d):
    return ev(d)["steps"][-1]


class BaselineTest(unittest.TestCase):
    def test_good_tree_passes_every_rule(self):
        root = planted(self)
        for fn in RULES:
            self.assertEqual(fn(root), [], fn.__name__)

    def test_step_order_in_the_good_tree(self):
        ids = [s.get("id") for s in good_kept()["jobs"]["evals"]["steps"]]
        self.assertLess(ids.index("run"), ids.index("failed"))
        self.assertLess(ids.index("failed"), ids.index("scan"))

    def test_expression_and_quote_forms_pass(self):
        def m(d):
            step(d, "failed")["if"] = "${{ always() }}"
            step(d, "failed")["run"] = 'python3 scripts/ci-evals-failed-trials.py "${EVAL_REPORT_DIR}"'
            step(d, "scan")["if"] = "${{ failure() || steps.failed.outputs.failed == \"true\" }}"
            upload(d)["if"] = ("${{ (failure() || steps.failed.outputs.failed == \"true\") "
                               "&& steps.scan.outcome == \"success\" }}")
            ev(d)["env"]["EVAL_REPORT_DIR"] = "${{ runner.temp }}/eval-report"
        root = planted(self, m)
        for fn in RULES:
            self.assertEqual(fn(root), [], fn.__name__)

    def test_failed_step_with_other_lines_passes(self):
        def m(d):
            step(d, "failed")["run"] = "set -e\n" + FAILED_RUN + "\n"
        self.assertEqual(judge_failed_step(planted(self, m)), [])


# ---------------------------------------------------------------- the report folder's path

class ReportDirPathTest(unittest.TestCase):
    def test_real_path_has_no_dot_segment(self):
        self.assertEqual(judge_report_dir_path(REPO), [])

    def test_planted_dot_segments_fail(self):
        for val in ("${{ github.workspace }}/../eval-report", "${{ github.workspace }}/./eval-report",
                    "../eval-report", "./eval-report", "/w/a/../b", "/w/a/./b", "/w/eval/..",
                    "/w/eval/.", "..", ".", "${{ github.workspace }}/..",
                    "${{ github.workspace }}/x/../y"):
            with self.subTest(val):
                def m(d, val=val):
                    ev(d)["env"]["EVAL_REPORT_DIR"] = val
                self.assertTrue(judge_report_dir_path(planted(self, m)))

    def test_dots_inside_names_and_expressions_pass(self):
        for val in ("${{ github.workspace }}-eval-report", "/w/eval.report", "/w/.eval-report",
                    "/w/a..b/c", "${{ github.workspace }}/eval-report.d", "/w/...", "/w/eval-report/"):
            with self.subTest(val):
                def m(d, val=val):
                    ev(d)["env"]["EVAL_REPORT_DIR"] = val
                self.assertEqual(judge_report_dir_path(planted(self, m)), [])

    def test_planted_missing_or_empty_value_fails(self):
        def gone(d):
            del ev(d)["env"]["EVAL_REPORT_DIR"]
        def empty(d):
            ev(d)["env"]["EVAL_REPORT_DIR"] = ""
        for m in (gone, empty):
            self.assertTrue(judge_report_dir_path(planted(self, m)))


# ---------------------------------------------------------------- the failed step

class FailedStepTest(unittest.TestCase):
    def test_real_failed_step(self):
        self.assertEqual(judge_failed_step(REPO), [])

    def test_planted_no_failed_step_fails(self):
        def m(d):
            ev(d)["steps"].remove(step(d, "failed"))
        self.assertTrue(judge_failed_step(planted(self, m)))

    def test_planted_two_failed_steps_fail(self):
        def m(d):
            ev(d)["steps"].insert(-2, copy.deepcopy(step(d, "failed")))
        self.assertTrue(judge_failed_step(planted(self, m)))

    def test_planted_other_id_fails(self):
        def m(d):
            step(d, "failed")["id"] = "trials"
        self.assertTrue(judge_failed_step(planted(self, m)))

    def test_planted_other_conditions_fail(self):
        for cond in (None, "failure()", "success()", "always() && false", "cancelled()"):
            with self.subTest(cond):
                def m(d, cond=cond):
                    if cond is None:
                        del step(d, "failed")["if"]
                    else:
                        step(d, "failed")["if"] = cond
                self.assertTrue(judge_failed_step(planted(self, m)))

    def test_planted_other_commands_fail(self):
        for run in ("echo failed=false", "python3 scripts/ci-evals-summary.py \"$EVAL_REPORT_DIR/report.json\"",
                    "python3 scripts/ci-evals-failed-trials.py", "python3 scripts/ci-evals-failed-trials.py src",
                    "python3 scripts/ci-evals-failed-trials.py \"$RUNNER_TEMP\"",
                    "python3 scripts/ci-evals-failed-trials.py \"$EVAL_REPORT_DIR/report.json\"",
                    "python3 scripts/ci-evals-failed-trial.py \"$EVAL_REPORT_DIR\"",
                    "# " + FAILED_RUN):
            with self.subTest(run):
                def m(d, run=run):
                    step(d, "failed")["run"] = run
                self.assertTrue(judge_failed_step(planted(self, m)))

    def test_planted_failed_step_before_the_run_step_fails(self):
        def m(d):
            s = ev(d)["steps"]
            f = step(d, "failed")
            s.remove(f)
            s.insert(next(i for i, x in enumerate(s) if x.get("id") == "run"), f)
        self.assertTrue(judge_failed_step(planted(self, m)))

    def test_planted_failed_step_after_the_scan_fails(self):
        def m(d):
            s = ev(d)["steps"]
            s[-3], s[-2] = s[-2], s[-3]
        self.assertTrue(judge_failed_step(planted(self, m)))

    def test_planted_failed_step_after_the_upload_fails(self):
        def m(d):
            s = ev(d)["steps"]
            s.append(s.pop(-3))
        self.assertTrue(judge_failed_step(planted(self, m)))


# ---------------------------------------------------------------- the scan's condition

class ScanConditionTest(unittest.TestCase):
    def test_real_scan_condition(self):
        self.assertEqual(judge_scan_condition(REPO), [])

    def test_planted_other_conditions_fail(self):
        for cond in (None, "failure()", "always()", "success()", "${{ failure() }}",
                     "steps.failed.outputs.failed == 'true'",
                     "failure() && steps.failed.outputs.failed == 'true'",
                     "failure() || steps.failed.outputs.failed == 'false'",
                     "failure() || steps.failed.outputs.failed",
                     "failure() || steps.failed.outcome == 'true'",
                     "failure() || steps.run.outputs.failed == 'true'",
                     "(failure() || steps.failed.outputs.failed == 'true') && steps.run.outcome == 'success'"):
            with self.subTest(cond):
                def m(d, cond=cond):
                    if cond is None:
                        del step(d, "scan")["if"]
                    else:
                        step(d, "scan")["if"] = cond
                self.assertTrue(judge_scan_condition(planted(self, m)))


# ---------------------------------------------------------------- the upload's condition

class UploadConditionFailedTrialTest(unittest.TestCase):
    def test_real_upload_condition(self):
        self.assertEqual(judge_upload_condition_failed_trial(REPO), [])

    def test_planted_without_parentheses_fails(self):
        def m(d):
            upload(d)["if"] = "failure() || steps.failed.outputs.failed == 'true' && steps.scan.outcome == 'success'"
        self.assertTrue(judge_upload_condition_failed_trial(planted(self, m)))

    def test_planted_upload_after_a_scan_match_on_a_green_job_fails(self):
        # a scan match fails the job, so failure() is true: only the scan clause stops the upload
        for cond in ("failure() || steps.failed.outputs.failed == 'true'",
                     "(failure() || steps.failed.outputs.failed == 'true') || steps.scan.outcome == 'success'",
                     "(failure() || steps.failed.outputs.failed == 'true') && steps.scan.outcome != 'success'",
                     "(failure() || steps.failed.outputs.failed == 'true') && steps.scan.conclusion == 'success'",
                     "(failure() || steps.failed.outputs.failed == 'true') && steps.run.outcome == 'success'",
                     "(failure() || steps.failed.outputs.failed == 'true') && steps.failed.outcome == 'success'",
                     "(failure() || steps.failed.outputs.failed == 'true') && always()"):
            with self.subTest(cond):
                def m(d, cond=cond):
                    upload(d)["if"] = cond
                self.assertTrue(judge_upload_condition_failed_trial(planted(self, m)))

    def test_planted_conditions_missing_a_clause_fail(self):
        for cond in (None, "failure() && steps.scan.outcome == 'success'", "${{ failure() }}", "always()",
                     "steps.failed.outputs.failed == 'true' && steps.scan.outcome == 'success'",
                     "(failure() && steps.failed.outputs.failed == 'true') && steps.scan.outcome == 'success'",
                     "(failure() || steps.failed.outputs.failed == 'false') && steps.scan.outcome == 'success'",
                     "(failure() || steps.failed.outputs.failed == 'true') && steps.scan.outcome == 'success' "
                     "|| always()",
                     "steps.scan.outcome == 'success'"):
            with self.subTest(cond):
                def m(d, cond=cond):
                    if cond is None:
                        del upload(d)["if"]
                    else:
                        upload(d)["if"] = cond
                self.assertTrue(judge_upload_condition_failed_trial(planted(self, m)))

    def test_condition_follows_the_scan_id(self):
        def m(d):
            step(d, "scan")["id"] = "secrets"
            upload(d)["if"] = f"({FAILED_IF}) && steps.secrets.outcome == 'success'"
        self.assertEqual(judge_upload_condition_failed_trial(planted(self, m)), [])

        def stale(d):
            step(d, "scan")["id"] = "secrets"
        self.assertTrue(judge_upload_condition_failed_trial(planted(self, stale)))


if __name__ == "__main__":
    unittest.main()
