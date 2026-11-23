"""The evals job writes the measurement and the trials section (T005; feature 008, research R2).

Workflow rules, each a judge_<rule>(root) over root/.github/workflows, with a test on the real
files and planted violations in a temporary tree that is valid otherwise (a baseline test proves
it valid):

- measurement_env: the evals job's env has EVAL_MEASUREMENT_DIR:
  `${{ github.workspace }}-eval-measurement` (no `.` or `..` segment);
- measurement_step: the step right after `Summary of the evals` has `if: always()` and
  `continue-on-error: true`, runs scripts/ci-evals-measure.py "$EVAL_REPORT_DIR/report.json"
  "$EVAL_MEASUREMENT_DIR/measurement.json" and then scripts/ci-evals-trials.py on that same file
  (separate lines or joined by `&&` or `;`; `python3` optional; `$VAR` or `${VAR}`), has env
  SUITE `${{ matrix.suite }}` and EVAL_CASES `${{ matrix.cases }}` and no `secrets.` expression,
  and its run text never holds `eval-report` (test_workflows.judge_evals_report_dir forbids it);
- summary_unchanged: the step `Summary of the evals` is as before: `if: always()`, run
  `python3 scripts/ci-evals-summary.py "$EVAL_REPORT_DIR/report.json"`, and nothing else.

Fixture keys are not needed. The loading helpers are test_workflows.py's, imported unchanged.
"""
import copy
import re
import shlex
import tempfile
import unittest
from pathlib import Path

import yaml

from test_workflows import jobs, load, need_files
from test_workflows_eval_artifacts import good_check, good_evals

REPO = Path(__file__).resolve().parent.parent.parent
SUMMARY = "Summary of the evals"
SUMMARY_RUN = 'python3 scripts/ci-evals-summary.py "$EVAL_REPORT_DIR/report.json"'
MEASURE = "scripts/ci-evals-measure.py"
TRIALS = "scripts/ci-evals-trials.py"
DIR_EXPR = re.compile(r"\$\{\{\s*github\.workspace\s*\}\}-eval-measurement")
SUITE_EXPR = re.compile(r"\$\{\{\s*matrix\.suite\s*\}\}")
CASES_EXPR = re.compile(r"\$\{\{\s*matrix\.cases\s*\}\}")
REPORT_FILE = re.compile(r"\$\{?EVAL_REPORT_DIR\}?/report\.json")
MEASURE_FILE = re.compile(r"\$\{?EVAL_MEASUREMENT_DIR\}?/measurement\.json")
SECRET_EXPR = re.compile(r"\bsecrets\b")


def strip_expr(cond):
    c = str(cond).strip()
    m = re.fullmatch(r"\$\{\{(.*)\}\}", c, re.S)
    return (m.group(1) if m else c).strip()


def evals_steps(wf):
    return (jobs(wf.get("evals.yml") or {}).get("evals") or {}).get("steps") or []


def summary_indexes(steps):
    return [i for i, st in enumerate(steps) if st.get("name") == SUMMARY]


def commands(run):
    """The run text as a list of argument lists, split at newlines, `&&` and `;`."""
    text = re.sub(r"\\\n", " ", str(run))
    out = []
    for part in re.split(r"&&|;|\n", text):
        part = part.strip()
        if not part or part.startswith("#"):
            continue
        try:
            words = shlex.split(part)
        except ValueError:
            words = part.split()
        if words and words[0] == "python3":
            words = words[1:]
        if words and words[0].startswith("./"):
            words[0] = words[0][2:]
        out.append(words)
    return out


# ---------------------------------------------------------------- rules

def judge_measurement_env(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    env = (jobs(wf["evals.yml"]).get("evals") or {}).get("env") or {}
    val = env.get("EVAL_MEASUREMENT_DIR")
    if val is None:
        return ["measurement_env: the evals job's env has no EVAL_MEASUREMENT_DIR"]
    if not DIR_EXPR.fullmatch(str(val).strip()):
        return [f"measurement_env: EVAL_MEASUREMENT_DIR is {val!r}, not "
                "${{ github.workspace }}-eval-measurement"]
    return []


def judge_measurement_step(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    steps = evals_steps(wf)
    s = summary_indexes(steps)
    if len(s) != 1:
        return [f"measurement_step: {len(s)} steps are named {SUMMARY!r}, not one"]
    if s[0] + 1 >= len(steps):
        return ["measurement_step: no step follows the summary step"]
    st = steps[s[0] + 1]
    run = str(st.get("run", ""))
    if strip_expr(st.get("if", "")) != "always()":
        v.append(f"measurement_step: the step's if is {st.get('if')!r}, not always()")
    if st.get("continue-on-error") not in (True, "true"):
        v.append(f"measurement_step: continue-on-error is {st.get('continue-on-error')!r}, not true")
    cmds = commands(run)
    mi = [i for i, c in enumerate(cmds) if c and c[0] == MEASURE]
    ti = [i for i, c in enumerate(cmds) if c and c[0] == TRIALS]
    if len(mi) != 1 or len(ti) != 1 or mi[0] > ti[0]:
        v.append(f"measurement_step: the step does not run {MEASURE} once and then {TRIALS} once")
    else:
        m, t = cmds[mi[0]], cmds[ti[0]]
        if len(m) != 3 or not REPORT_FILE.fullmatch(m[1]) or not MEASURE_FILE.fullmatch(m[2]):
            v.append(f"measurement_step: {MEASURE} is not called with "
                     '"$EVAL_REPORT_DIR/report.json" "$EVAL_MEASUREMENT_DIR/measurement.json"')
        elif len(t) != 2 or not MEASURE_FILE.fullmatch(t[1]):
            v.append(f"measurement_step: {TRIALS} is not called on "
                     '"$EVAL_MEASUREMENT_DIR/measurement.json" alone')
    env = st.get("env") or {}
    if not SUITE_EXPR.fullmatch(str(env.get("SUITE", "")).strip()):
        v.append(f"measurement_step: env SUITE is {env.get('SUITE')!r}, not ${{{{ matrix.suite }}}}")
    if not CASES_EXPR.fullmatch(str(env.get("EVAL_CASES", "")).strip()):
        v.append(f"measurement_step: env EVAL_CASES is {env.get('EVAL_CASES')!r}, "
                 "not ${{ matrix.cases }}")
    if SECRET_EXPR.search(str(env)):
        v.append("measurement_step: the step's env holds a secrets expression")
    if "eval-report" in run:
        v.append("measurement_step: the step's run text holds eval-report")
    return v


def judge_summary_unchanged(root):
    wf = load(root)
    v = need_files(wf, "evals.yml")
    if v:
        return v
    steps = evals_steps(wf)
    s = summary_indexes(steps)
    if len(s) != 1:
        return [f"summary_unchanged: {len(s)} steps are named {SUMMARY!r}, not one"]
    st = steps[s[0]]
    if set(st) != {"name", "if", "run"}:
        v.append(f"summary_unchanged: the step has keys {sorted(st)}, not name, if, run")
    if strip_expr(st.get("if", "")) != "always()":
        v.append(f"summary_unchanged: the step's if is {st.get('if')!r}, not always()")
    if str(st.get("run", "")).strip() != SUMMARY_RUN:
        v.append(f"summary_unchanged: the step's run is {st.get('run')!r}, not {SUMMARY_RUN}")
    return v


RULES = (judge_measurement_env, judge_measurement_step, judge_summary_unchanged)


# ---------------------------------------------------------------- planted tree

def MEASURE_STEP():
    return {"name": "Measure the trials", "if": "always()", "continue-on-error": True,
            "env": {"SUITE": "${{ matrix.suite }}", "EVAL_CASES": "${{ matrix.cases }}"},
            "run": f'python3 {MEASURE} "$EVAL_REPORT_DIR/report.json" '
                   '"$EVAL_MEASUREMENT_DIR/measurement.json"\n'
                   f'python3 {TRIALS} "$EVAL_MEASUREMENT_DIR/measurement.json"'}


def good_trials_evals():
    wf = good_evals()
    ev = wf["jobs"]["evals"]
    ev["env"]["EVAL_MEASUREMENT_DIR"] = "${{ github.workspace }}-eval-measurement"
    steps = ev["steps"]
    i = next(k for k, st in enumerate(steps) if "ci-evals-summary" in str(st.get("run", "")))
    steps[i] = {"name": SUMMARY, "if": "always()", "run": SUMMARY_RUN}
    steps.insert(i + 1, MEASURE_STEP())
    return wf


def planted(tc, mutate=None):
    tmp = tempfile.TemporaryDirectory()
    tc.addCleanup(tmp.cleanup)
    root = Path(tmp.name)
    (root / ".github" / "workflows").mkdir(parents=True)
    d = {"check.yml": good_check(), "evals.yml": good_trials_evals()}
    if mutate:
        mutate(d)
    for n, w in d.items():
        (root / ".github" / "workflows" / n).write_text(yaml.safe_dump(w, sort_keys=False))
    return root


def ev(d):
    return d["evals.yml"]["jobs"]["evals"]


def steps_of(d):
    return ev(d)["steps"]


def summary(d):
    return next(s for s in steps_of(d) if s.get("name") == SUMMARY)


def measure(d):
    st = steps_of(d)
    return st[st.index(summary(d)) + 1]


def fails(tc, fn, mutate, rule):
    v = fn(planted(tc, mutate))
    tc.assertTrue(v, "no violation found")
    tc.assertTrue(all(rule in x for x in v), v)


# ---------------------------------------------------------------- tests

class BaselineTest(unittest.TestCase):
    def test_good_tree_passes_every_rule(self):
        root = planted(self)
        for fn in RULES:
            self.assertEqual(fn(root), [], fn.__name__)

    def test_variants_pass(self):
        variants = [
            "scripts/ci-evals-measure.py \"$EVAL_REPORT_DIR/report.json\" "
            "\"$EVAL_MEASUREMENT_DIR/measurement.json\" && scripts/ci-evals-trials.py "
            "\"$EVAL_MEASUREMENT_DIR/measurement.json\"",
            "python3 ./scripts/ci-evals-measure.py ${EVAL_REPORT_DIR}/report.json "
            "${EVAL_MEASUREMENT_DIR}/measurement.json; python3 ./scripts/ci-evals-trials.py "
            "'${EVAL_MEASUREMENT_DIR}/measurement.json'",
            "set -e\npython3 scripts/ci-evals-measure.py \"$EVAL_REPORT_DIR/report.json\" \\\n"
            "  \"$EVAL_MEASUREMENT_DIR/measurement.json\"\n"
            "python3 scripts/ci-evals-trials.py \"$EVAL_MEASUREMENT_DIR/measurement.json\"\n",
        ]
        for run in variants:
            with self.subTest(run):
                def m(d, run=run):
                    measure(d)["run"] = run
                root = planted(self, m)
                self.assertEqual(judge_measurement_step(root), [])

    def test_expression_forms_pass(self):
        def m(d):
            ev(d)["env"]["EVAL_MEASUREMENT_DIR"] = "${{github.workspace}}-eval-measurement"
            st = measure(d)
            st["if"] = "${{ always() }}"
            st["continue-on-error"] = "true"
            st["env"] = {"SUITE": "${{matrix.suite}}", "EVAL_CASES": "${{  matrix.cases }}"}
            summary(d)["if"] = "${{ always() }}"
        root = planted(self, m)
        for fn in RULES:
            self.assertEqual(fn(root), [], fn.__name__)

    def test_other_env_and_unrelated_steps_pass(self):
        def m(d):
            measure(d)["env"]["GITHUB_STEP_SUMMARY_NOTE"] = "x"
            steps_of(d).append({"run": "echo mentions eval-report elsewhere"})
        root = planted(self, m)
        self.assertEqual(judge_measurement_step(root), [])


class MeasurementEnvTest(unittest.TestCase):
    def test_real_env(self):
        self.assertEqual(judge_measurement_env(REPO), [])

    def test_planted_wrong_values_fail(self):
        for val in (None, "", "${{ github.workspace }}/../eval-measurement",
                    "${{ github.workspace }}/eval-measurement", "${{ github.workspace }}-eval-report",
                    "${{ github.workspace }}-measurement", "${{ runner.temp }}-eval-measurement",
                    "/var/run/eval-measurement", "${{ github.workspace }}-eval-measurement/..",
                    "${{ github.workspace }}-eval-measurement/.",
                    "${{ github.workspace }}/.-eval-measurement"):
            with self.subTest(val):
                def m(d, val=val):
                    if val is None:
                        del ev(d)["env"]["EVAL_MEASUREMENT_DIR"]
                    else:
                        ev(d)["env"]["EVAL_MEASUREMENT_DIR"] = val
                fails(self, judge_measurement_env, m, "measurement_env")

    def test_planted_env_only_on_a_step_fails(self):
        def m(d):
            del ev(d)["env"]["EVAL_MEASUREMENT_DIR"]
            measure(d)["env"]["EVAL_MEASUREMENT_DIR"] = "${{ github.workspace }}-eval-measurement"
        fails(self, judge_measurement_env, m, "measurement_env")

    def test_planted_no_job_env_fails(self):
        def m(d):
            del ev(d)["env"]
        fails(self, judge_measurement_env, m, "measurement_env")


class MeasurementStepTest(unittest.TestCase):
    def test_real_step(self):
        self.assertEqual(judge_measurement_step(REPO), [])

    def test_planted_step_missing_fails(self):
        def m(d):
            steps_of(d).remove(measure(d))
        fails(self, judge_measurement_step, m, "measurement_step")

    def test_planted_summary_last_step_fails(self):
        def m(d):
            ev(d)["steps"] = [summary(d)]
        fails(self, judge_measurement_step, m, "measurement_step")

    def test_planted_step_not_right_after_the_summary_fails(self):
        def before(d):
            s = steps_of(d)
            i = s.index(summary(d))
            s[i], s[i + 1] = s[i + 1], s[i]
        def gap(d):
            s = steps_of(d)
            s.insert(s.index(summary(d)) + 1, {"if": "always()", "run": "echo gap"})
        def far(d):
            s = steps_of(d)
            st = s.pop(s.index(summary(d)) + 1)
            s.append(st)
        for m in (before, gap, far):
            with self.subTest(m.__name__):
                fails(self, judge_measurement_step, m, "measurement_step")

    def test_planted_two_summary_steps_fail(self):
        def m(d):
            s = steps_of(d)
            s.insert(0, copy.deepcopy(summary(d)))
        fails(self, judge_measurement_step, m, "measurement_step")

    def test_planted_conditions_fail(self):
        for cond in (None, "success()", "failure()", "always() && matrix.suite == 'steps'",
                     "${{ success() }}", "!cancelled()", "never()"):
            with self.subTest(cond):
                def m(d, cond=cond):
                    if cond is None:
                        del measure(d)["if"]
                    else:
                        measure(d)["if"] = cond
                fails(self, judge_measurement_step, m, "measurement_step")

    def test_planted_continue_on_error_fails(self):
        for val in (None, False, "false", "${{ false }}"):
            with self.subTest(val):
                def m(d, val=val):
                    if val is None:
                        del measure(d)["continue-on-error"]
                    else:
                        measure(d)["continue-on-error"] = val
                fails(self, judge_measurement_step, m, "measurement_step")

    def test_planted_wrong_commands_fail(self):
        a, b = '"$EVAL_REPORT_DIR/report.json"', '"$EVAL_MEASUREMENT_DIR/measurement.json"'
        runs = {
            "no arguments": f"python3 {MEASURE}\npython3 {TRIALS} {b}",
            "report only": f"python3 {MEASURE} {a}\npython3 {TRIALS} {b}",
            "arguments swapped": f"python3 {MEASURE} {b} {a}\npython3 {TRIALS} {b}",
            "extra argument": f"python3 {MEASURE} {a} {b} x\npython3 {TRIALS} {b}",
            "other report": f'python3 {MEASURE} "$EVAL_REPORT_DIR/other.json" {b}\npython3 {TRIALS} {b}',
            "other output name": f'python3 {MEASURE} {a} "$EVAL_MEASUREMENT_DIR/m.json"\n'
                                 f'python3 {TRIALS} "$EVAL_MEASUREMENT_DIR/m.json"',
            "dotdot output": f'python3 {MEASURE} {a} "$EVAL_MEASUREMENT_DIR/../measurement.json"\n'
                             f'python3 {TRIALS} "$EVAL_MEASUREMENT_DIR/../measurement.json"',
            "trials on the report": f"python3 {MEASURE} {a} {b}\npython3 {TRIALS} {a}",
            "trials on another file": f'python3 {MEASURE} {a} {b}\n'
                                      f'python3 {TRIALS} "$EVAL_MEASUREMENT_DIR/other.json"',
            "trials without argument": f"python3 {MEASURE} {a} {b}\npython3 {TRIALS}",
            "trials first": f"python3 {TRIALS} {b}\npython3 {MEASURE} {a} {b}",
            "measure only": f"python3 {MEASURE} {a} {b}",
            "trials only": f"python3 {TRIALS} {b}",
            "other script names": f"python3 scripts/ci-evals-measurex.py {a} {b}\n"
                                  f"python3 scripts/ci-evals-trialsx.py {b}",
            "measure twice": f"python3 {MEASURE} {a} {b}\npython3 {MEASURE} {a} {b}\n"
                             f"python3 {TRIALS} {b}",
            "empty": "",
        }
        for label, run in runs.items():
            with self.subTest(label):
                def m(d, run=run):
                    measure(d)["run"] = run
                fails(self, judge_measurement_step, m, "measurement_step")

    def test_planted_command_in_a_comment_only_fails(self):
        def m(d):
            measure(d)["run"] = (f"# python3 {MEASURE} \"$EVAL_REPORT_DIR/report.json\" "
                                 "\"$EVAL_MEASUREMENT_DIR/measurement.json\"\n"
                                 f"# python3 {TRIALS} \"$EVAL_MEASUREMENT_DIR/measurement.json\"")
        fails(self, judge_measurement_step, m, "measurement_step")

    def test_planted_env_problems_fail(self):
        cases = [("SUITE", None), ("SUITE", "steps"), ("SUITE", "${{ matrix.cases }}"),
                 ("SUITE", "${{ matrix.suite }}-x"), ("EVAL_CASES", None),
                 ("EVAL_CASES", ""), ("EVAL_CASES", "${{ matrix.suite }}"),
                 ("EVAL_CASES", "analyst")]
        for key, val in cases:
            with self.subTest(f"{key}={val}"):
                def m(d, key=key, val=val):
                    if val is None:
                        del measure(d)["env"][key]
                    else:
                        measure(d)["env"][key] = val
                v = judge_measurement_step(planted(self, m))
                self.assertTrue(any("measurement_step" in x and key in x for x in v), v)

    def test_planted_no_env_fails(self):
        def m(d):
            del measure(d)["env"]
        v = judge_measurement_step(planted(self, m))
        self.assertTrue(any("SUITE" in x for x in v) and any("EVAL_CASES" in x for x in v), v)

    def test_planted_secret_in_env_fails(self):
        for val in ("${{ secrets.ANTHROPIC_API_KEY }}", "${{secrets.X}}",
                    "${{ secrets['ANTHROPIC_API_KEY'] }}", "${{ toJSON(secrets) }}"):
            with self.subTest(val):
                def m(d, val=val):
                    measure(d)["env"]["ANTHROPIC_API_KEY"] = val
                fails(self, judge_measurement_step, m, "measurement_step")

    def test_planted_secret_in_suite_value_fails(self):
        def m(d):
            measure(d)["env"]["SUITE"] = "${{ matrix.suite }}${{ secrets.X }}"
        self.assertTrue(judge_measurement_step(planted(self, m)))

    def test_planted_eval_report_in_run_fails(self):
        for extra in ("echo eval-report", "# eval-report", "ls ${{ github.workspace }}-eval-report"):
            with self.subTest(extra):
                def m(d, extra=extra):
                    measure(d)["run"] += "\n" + extra
                v = judge_measurement_step(planted(self, m))
                self.assertTrue(any("eval-report" in x for x in v), v)


class SummaryUnchangedTest(unittest.TestCase):
    def test_real_summary(self):
        self.assertEqual(judge_summary_unchanged(REPO), [])

    def test_planted_changes_fail(self):
        changes = {
            "other script": {"run": 'python3 scripts/ci-evals-summary.py "$EVAL_REPORT_DIR/x.json"'},
            "no python3": {"run": 'scripts/ci-evals-summary.py "$EVAL_REPORT_DIR/report.json"'},
            "extra command": {"run": SUMMARY_RUN + "\npython3 scripts/ci-evals-trials.py x"},
            "joined command": {"run": SUMMARY_RUN + " && true"},
            "if success": {"if": "success()"},
            "if failure": {"if": "failure()"},
            "extra key": {"continue-on-error": True},
            "extra env": {"env": {"SUITE": "x"}},
            "extra id": {"id": "summary"},
        }
        for label, change in changes.items():
            with self.subTest(label):
                def m(d, change=change):
                    summary(d).update(change)
                fails(self, judge_summary_unchanged, m, "summary_unchanged")

    def test_planted_if_removed_fails(self):
        def m(d):
            del summary(d)["if"]
        fails(self, judge_summary_unchanged, m, "summary_unchanged")

    def test_planted_summary_missing_or_renamed_fails(self):
        def gone(d):
            steps_of(d).remove(summary(d))
        def renamed(d):
            summary(d)["name"] = "Summary of the eval runs"
        for m in (gone, renamed):
            with self.subTest(m.__name__):
                fails(self, judge_summary_unchanged, m, "summary_unchanged")

    def test_missing_evals_file_fails(self):
        def m(d):
            del d["evals.yml"]
        for fn in RULES:
            with self.subTest(fn.__name__):
                self.assertTrue(fn(planted(self, m)))


if __name__ == "__main__":
    unittest.main()
