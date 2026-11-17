"""Tests for the CI run names (T128; research D51).

check.yml and evals.yml each carry a top-level `run-name` exactly as T128 words it, built only
from the `github` and `inputs` contexts. Each rule is a function judging a root directory; the
real files and planted copies in a temporary tree are judged by the same function, and every
violation names its workflow.
"""
import re
import sys
import tempfile
import unittest
from pathlib import Path

import yaml

REPO = Path(__file__).resolve().parent.parent.parent
EXPECTED = {
    "check.yml": "check · ${{ github.ref_name }} · ${{ github.event_name == 'workflow_dispatch' "
                 "&& format('started by {0}', github.actor) || github.event.head_commit.message }}",
    "evals.yml": "evals · ${{ github.ref_name }} · ${{ inputs.all && 'all suites' || "
                 "'changed suites' }} · started by ${{ github.actor }}",
}
ALLOWED = {"github", "inputs"}
EXPR = re.compile(r"\$\{\{(.*?)\}\}", re.S)
CONTEXT = re.compile(r"(?<![\w.'])([A-Za-z_][\w-]*)(?=\s*(?:\.|\[|\())")
STRING = re.compile(r"'(?:[^']|'')*'")


def load(root):
    wf = {}
    for name in EXPECTED:
        p = Path(root) / ".github" / "workflows" / name
        wf[name] = yaml.safe_load(p.read_text()) if p.exists() else None
    print(f"workflows examined under {root}: {sorted(wf)}", file=sys.stderr)
    return wf


def contexts(run_name):
    found = set()
    for expr in EXPR.findall(run_name):
        for m in CONTEXT.finditer(STRING.sub("''", expr)):
            if m.group(1) not in {"format", "contains", "startsWith", "endsWith", "join", "toJSON", "fromJSON"}:
                found.add(m.group(1))
    return found


def judge_run_name(root):
    out = []
    for name, w in load(root).items():
        if w is None:
            out.append(f"{name}: workflow file is missing")
            continue
        rn = w.get("run-name")
        if rn is None:
            out.append(f"{name}: no top-level run-name")
            continue
        if rn != EXPECTED[name]:
            out.append(f"{name}: run-name is {rn!r}, expected {EXPECTED[name]!r}")
        bad = contexts(str(rn)) - ALLOWED
        if bad:
            out.append(f"{name}: run-name uses contexts other than github and inputs: {sorted(bad)}")
    return out


def plant(root, **overrides):
    d = Path(root) / ".github" / "workflows"
    d.mkdir(parents=True)
    for name, rn in EXPECTED.items():
        rn = overrides.get(name, rn)
        lines = [f"name: {name[:-4]}"]
        if rn is not None:
            lines.append("run-name: " + yaml.safe_dump(rn, default_style='"', width=10**6).strip())
        lines += ["on:", "  workflow_dispatch:", "jobs:", "  j:", "    runs-on: ubuntu-latest",
                  "    steps:", "      - run: true"]
        (d / name).write_text("\n".join(lines) + "\n")


class RealWorkflows(unittest.TestCase):
    def test_both_workflows_have_the_exact_run_name(self):
        w = load(REPO)
        for name, expected in EXPECTED.items():
            self.assertEqual(w[name].get("run-name"), expected, f"{name} run-name")

    def test_real_run_names_use_only_github_and_inputs(self):
        for name, w in load(REPO).items():
            rn = w.get("run-name")
            self.assertIsNotNone(rn, f"{name} has no run-name")
            self.assertLessEqual(contexts(rn), ALLOWED, f"{name} run-name contexts")
            self.assertTrue(contexts(rn), f"{name}: no context parsed")

    def test_judge_accepts_the_real_files(self):
        self.assertEqual(judge_run_name(REPO), [])


class Planted(unittest.TestCase):
    def judged(self, **o):
        with tempfile.TemporaryDirectory() as t:
            plant(t, **o)
            return judge_run_name(t)

    def test_clean_copy_passes(self):
        self.assertEqual(self.judged(), [])

    def test_missing_run_name_fails_naming_the_workflow(self):
        for name in EXPECTED:
            v = self.judged(**{name: None})
            self.assertEqual(len(v), 1, v)
            self.assertIn(name, v[0])

    def test_different_value_fails_naming_the_workflow(self):
        for name in EXPECTED:
            v = self.judged(**{name: "something else ${{ github.ref_name }}"})
            self.assertTrue(v and all(name in x for x in v), v)

    def test_other_contexts_fail_naming_the_workflow(self):
        for ctx in ("secrets.ANTHROPIC_API_KEY", "env.FOO", "vars.X", "steps.a.outputs.b"):
            for name in EXPECTED:
                rn = EXPECTED[name] + " ${{ " + ctx + " }}"
                v = self.judged(**{name: rn})
                self.assertTrue(any(name in x and ctx.split(".")[0] in x and "contexts" in x for x in v), (ctx, v))
                other = [n for n in EXPECTED if n != name][0]
                self.assertFalse(any(other in x for x in v), v)


if __name__ == "__main__":
    unittest.main()
