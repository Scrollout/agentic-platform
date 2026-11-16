"""The evals workflow runs only the suites scripts/ci-evals-select.py chooses (T115, T120).

Rules, each a judge_<rule>(root) over root/.github/workflows/evals.yml, with a test on the real
file and a planted violation in a temporary tree that is valid otherwise (a baseline test proves
it valid):

- matrix: the evals job's strategy.matrix.include is `${{ fromJSON(needs.config.outputs.<x>) }}`
  and nothing else, <x> is an output of the config job, and that output is a step output of the
  config step that runs scripts/ci-evals-select.py;
- skipped: the evals job's `if:` skips it when that output is `[]`;
- env: the selector step takes SHA and EVENT from `env:` (github.sha, github.event_name), and
  its run holds no `${{ }}` expression;
- history: the config job can resolve the push's start: its checkout has fetch-depth 0, or a
  step before the selector runs `git fetch`.

The base run (since T120 the selector chooses it from RUNS, the listed successful evals runs;
the workflow gives no BEFORE) is judged by scripts/tests/test_workflows_evals_current.py.

The loading helpers are scripts/tests/test_workflows.py's, imported unchanged.
"""
import copy
import re
import tempfile
import unittest
from pathlib import Path

import yaml

from test_workflows import EXPR, good_evals, jobs, load, need_files

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
SELECT = re.compile(r"scripts/ci-evals-select\.py")
FROM_JSON = re.compile(r"\s*\$\{\{\s*fromjson\(\s*needs\.config\.outputs\.([\w-]+)\s*\)\s*\}\}\s*",
                       re.I)
STEP_OUT = re.compile(r"\s*\$\{\{\s*steps\.([\w-]+)\.outputs\.([\w-]+)\s*\}\}\s*")
WANT_ENV = {"SHA": "github.sha", "EVENT": "github.event_name"}


def evals_wf(root):
    wf = load(root)
    return wf.get("evals.yml"), need_files(wf, "evals.yml")


def select_step(cfg):
    for i, st in enumerate(cfg.get("steps") or []):
        if SELECT.search(str(st.get("run", ""))):
            return i, st
    return -1, None


def matrix_output(root):
    """(output name, violations) of the evals job's matrix."""
    w, v = evals_wf(root)
    if v:
        return None, v
    ev = jobs(w).get("evals") or {}
    m = (ev.get("strategy") or {}).get("matrix")
    if not isinstance(m, dict) or set(m) != {"include"} or not isinstance(m["include"], str):
        return None, [f"evals job matrix is {m!r}, not include: ${{{{ fromJSON(needs.config.outputs.<x>) }}}} alone"]
    hit = FROM_JSON.fullmatch(m["include"])
    if not hit:
        return None, [f"evals job matrix include {m['include']!r} is not fromJSON(needs.config.outputs.<x>)"]
    return hit.group(1), []


def judge_matrix(root):
    name, v = matrix_output(root)
    if v:
        return v
    w, _ = evals_wf(root)
    cfg = jobs(w).get("config") or {}
    out = (cfg.get("outputs") or {}).get(name)
    if out is None:
        return [f"the config job has no output {name}"]
    so = STEP_OUT.fullmatch(str(out))
    i, st = select_step(cfg)
    if st is None:
        return ["no config step runs scripts/ci-evals-select.py"]
    if not so or so.group(1) != st.get("id"):
        v.append(f"config output {name} = {out!r} is not an output of the step running the selector")
    elif not re.search(r"\b" + re.escape(so.group(2)) + r"=.*GITHUB_OUTPUT", str(st.get("run", "")), re.S):
        v.append(f"the selector step does not write {so.group(2)}= to $GITHUB_OUTPUT")
    ev = jobs(w).get("evals") or {}
    needs = ev.get("needs")
    if "config" not in ([needs] if isinstance(needs, str) else needs or []):
        v.append("the evals job does not need config")
    return v


EMPTY = r"""needs\.config\.outputs\.{x}\s*!=\s*(?:'\[\]'|"\[\]")"""


def judge_skipped(root):
    name, v = matrix_output(root)
    if v:
        return v
    w, _ = evals_wf(root)
    cond = str((jobs(w).get("evals") or {}).get("if", ""))
    exprs = EXPR.findall(cond) or [cond]
    if not any(re.search(EMPTY.format(x=re.escape(name)), e) for e in exprs):
        return [f"the evals job's if {cond!r} does not skip it when needs.config.outputs.{name} is []"]
    return []


def judge_env(root):
    w, v = evals_wf(root)
    if v:
        return v
    i, st = select_step(jobs(w).get("config") or {})
    if st is None:
        return ["no config step runs scripts/ci-evals-select.py"]
    env = st.get("env") or {}
    for k, ctx in WANT_ENV.items():
        val = str(env.get(k, ""))
        if not re.fullmatch(r"\s*\$\{\{\s*" + re.escape(ctx) + r"\s*\}\}\s*", val):
            v.append(f"the selector step's env {k} is {val!r}, not ${{{{ {ctx} }}}}")
    if EXPR.search(str(st.get("run", ""))):
        v.append("the selector step's run holds an expression")
    return v


def judge_history(root):
    w, v = evals_wf(root)
    if v:
        return v
    cfg = jobs(w).get("config") or {}
    i, st = select_step(cfg)
    if st is None:
        return ["no config step runs scripts/ci-evals-select.py"]
    steps = cfg.get("steps") or []
    deep = any(str(s.get("uses", "")).startswith("actions/checkout@")
               and str((s.get("with") or {}).get("fetch-depth")) == "0" for s in steps[:i])
    fetch = any(re.search(r"\bgit\s+fetch\b", str(s.get("run", ""))) for s in steps[:i + 1])
    if not (deep or fetch):
        v.append("the config job neither checks out with fetch-depth 0 nor runs git fetch before the selector")
    return v


RULES = (judge_matrix, judge_skipped, judge_env, judge_history)
SELECT_RUN = ("git fetch -q --no-tags --unshallow origin\n"
              "RUNS=$(gh api \"repos/$GITHUB_REPOSITORY/actions/workflows/evals.yml/runs"
              "?status=success&per_page=100\" --jq '[.workflow_runs[] | {head_sha, head_branch, "
              "conclusion}]' || true)\nexport RUNS\n"
              "matrix=$(python3 scripts/ci-evals-select.py)\n"
              "echo \"matrix=$matrix\" >>\"$GITHUB_OUTPUT\"")


def good():
    w = good_evals()
    cfg = w["jobs"]["config"]
    cfg["outputs"]["matrix"] = "${{ steps.select.outputs.matrix }}"
    cfg["steps"].append({"id": "select", "run": SELECT_RUN,
                         "env": dict({k: "${{ %s }}" % c for k, c in WANT_ENV.items()},
                                     GH_TOKEN="${{ github.token }}", ALL="${{ inputs.all }}")})
    ev = w["jobs"]["evals"]
    ev["if"] = "needs.config.outputs.matrix != '[]'"
    ev["strategy"] = {"fail-fast": False,
                      "matrix": {"include": "${{ fromJSON(needs.config.outputs.matrix) }}"}}
    return w


def planted(tc, mutate=None):
    tmp = tempfile.TemporaryDirectory()
    tc.addCleanup(tmp.cleanup)
    root = Path(tmp.name)
    (root / ".github" / "workflows").mkdir(parents=True)
    w = good()
    if mutate:
        mutate(w)
    (root / ".github" / "workflows" / "evals.yml").write_text(yaml.safe_dump(w, sort_keys=False))
    return root


def cfg(w):
    return w["jobs"]["config"]


def sel(w):
    return cfg(w)["steps"][-1]


class BaselineTest(unittest.TestCase):
    def test_good_tree_passes(self):
        root = planted(self)
        for fn in RULES:
            self.assertEqual(fn(root), [], fn.__name__)

    def test_expression_forms_pass(self):
        def m(w):
            w["jobs"]["evals"]["if"] = "${{ needs.config.outputs.matrix != \"[]\" }}"
            w["jobs"]["evals"]["strategy"]["matrix"]["include"] = \
                "${{fromJson(needs.config.outputs.matrix)}}"
        root = planted(self, m)
        self.assertEqual(judge_matrix(root), [])
        self.assertEqual(judge_skipped(root), [])

    def test_deep_checkout_passes_history(self):
        def m(w):
            sel(w)["run"] = SELECT_RUN.split("\n", 1)[1]
            cfg(w)["steps"][0]["with"]["fetch-depth"] = 0
        self.assertEqual(judge_history(planted(self, m)), [])


class MatrixTest(unittest.TestCase):
    def test_real_matrix_from_config(self):
        self.assertEqual(judge_matrix(REPO), [])

    def test_planted_literal_matrix_fails(self):
        def m(w):
            w["jobs"]["evals"]["strategy"]["matrix"] = {"include": [{"suite": "analyst", "cases": ""}]}
        self.assertTrue(judge_matrix(planted(self, m)))

    def test_planted_literal_entry_beside_the_output_fails(self):
        def m(w):
            w["jobs"]["evals"]["strategy"]["matrix"]["suite"] = ["analyst"]
        self.assertTrue(judge_matrix(planted(self, m)))

    def test_planted_output_not_from_the_selector_fails(self):
        def m(w):
            cfg(w)["outputs"]["matrix"] = "${{ steps.v.outputs.runner }}"
        self.assertTrue(judge_matrix(planted(self, m)))

    def test_planted_missing_output_fails(self):
        def m(w):
            del cfg(w)["outputs"]["matrix"]
        self.assertTrue(judge_matrix(planted(self, m)))

    def test_planted_no_selector_step_fails(self):
        def m(w):
            sel(w)["run"] = "echo 'matrix=[]' >>\"$GITHUB_OUTPUT\""
        self.assertTrue(judge_matrix(planted(self, m)))

    def test_planted_selector_output_not_written_fails(self):
        def m(w):
            sel(w)["run"] = "python3 scripts/ci-evals-select.py"
        self.assertTrue(judge_matrix(planted(self, m)))


class SkippedTest(unittest.TestCase):
    def test_real_skipped_when_empty(self):
        self.assertEqual(judge_skipped(REPO), [])

    def test_planted_no_if_fails(self):
        def m(w):
            del w["jobs"]["evals"]["if"]
        self.assertTrue(judge_skipped(planted(self, m)))

    def test_planted_if_on_another_output_fails(self):
        def m(w):
            w["jobs"]["evals"]["if"] = "needs.config.outputs.runner != '[]'"
        self.assertTrue(judge_skipped(planted(self, m)))

    def test_planted_inverted_if_fails(self):
        def m(w):
            w["jobs"]["evals"]["if"] = "needs.config.outputs.matrix == '[]'"
        self.assertTrue(judge_skipped(planted(self, m)))


class EnvTest(unittest.TestCase):
    def test_real_before_through_env(self):
        self.assertEqual(judge_env(REPO), [])

    def test_planted_expression_in_run_fails(self):
        def m(w):
            sel(w)["run"] = "SHA=${{ github.sha }} " + SELECT_RUN
        self.assertTrue(any("expression" in x for x in judge_env(planted(self, m))))

    def test_planted_each_env_missing_fails(self):
        for k in WANT_ENV:
            with self.subTest(k):
                def m(w, k=k):
                    del sel(w)["env"][k]
                self.assertTrue(any(k in x for x in judge_env(planted(self, m))))

    def test_planted_wrong_context_fails(self):
        def m(w):
            sel(w)["env"]["SHA"] = "${{ github.event.after }}"
        self.assertTrue(judge_env(planted(self, m)))


class HistoryTest(unittest.TestCase):
    def test_real_history(self):
        self.assertEqual(judge_history(REPO), [])

    def test_planted_shallow_without_fetch_fails(self):
        def m(w):
            sel(w)["run"] = SELECT_RUN.split("\n", 1)[1]
        self.assertTrue(judge_history(planted(self, m)))


if __name__ == "__main__":
    unittest.main()
