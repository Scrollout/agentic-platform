"""Quarantined cases in scripts/ci-evals-summary.py (T185), written before the change.

A case with `quarantined_by` is run and shown but not counted: its row reads
`| <case> | <agent> | ⚠️ quarantined (<task>): pass |` (or `: fail |`), the header becomes
`## Evals: <p> of <n> counted cases pass^3` plus `, <q> quarantined` when q > 0 (n excludes the
quarantined), and the exit code is 0 when every counted case passed. A report with no quarantined
case gives today's output exactly.
"""
import json
import subprocess
import tempfile
import unittest
from pathlib import Path

from test_ci_scripts import EVALS, base_env


def case(name, passed, agent="analyst", q=None, usage=None):
    trials = [{"trial": i, "passed": passed} for i in (1, 2, 3)]
    if usage:
        trials[0]["usage"] = usage
    c = {"case": name, "agent": agent, "passed": passed, "trials": trials}
    if q:
        c["quarantined_by"] = q
    return c


class QuarantineSummary(unittest.TestCase):
    def run_script(self, cases):
        t = tempfile.TemporaryDirectory()
        self.addCleanup(t.cleanup)
        p = Path(t.name) / "report.json"
        p.write_text(json.dumps(cases), encoding="utf-8")
        return subprocess.run(["python3", str(EVALS), str(p)], capture_output=True, text=True,
                              env=base_env(self), cwd=t.name, timeout=60)

    def test_failing_quarantined_case_does_not_fail_the_run(self):
        r = self.run_script([case("example", False, q="T185"), case("a", True), case("b", True)])
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("## Evals: 2 of 2 counted cases pass^3, 1 quarantined", r.stdout)
        self.assertIn("| example | analyst | ⚠️ quarantined (T185): fail |", r.stdout)
        self.assertIn("| a | analyst | ✅ pass |", r.stdout)

    def test_failing_counted_case_still_fails_the_run(self):
        r = self.run_script([case("example", False, q="T185"), case("a", True), case("b", False)])
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("## Evals: 1 of 2 counted cases pass^3, 1 quarantined", r.stdout)
        self.assertIn("| b | analyst | ❌ fail |", r.stdout)

    def test_passing_quarantined_case_shows_pass(self):
        r = self.run_script([case("example", True, q="T185"), case("a", True)])
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("| example | analyst | ⚠️ quarantined (T185): pass |", r.stdout)
        self.assertIn("## Evals: 1 of 1 counted cases pass^3, 1 quarantined", r.stdout)

    def test_two_quarantined_cases_with_different_tasks(self):
        r = self.run_script([case("x", False, q="T185"), case("y", False, q="T190"), case("a", True)])
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("1 of 1 counted cases pass^3, 2 quarantined", r.stdout)
        self.assertIn("quarantined (T190): fail |", r.stdout)

    def test_quarantined_case_with_no_trials_is_a_fail_row_not_a_crash(self):
        c = {"case": "x", "agent": "analyst", "quarantined_by": "T185", "trials": []}
        r = self.run_script([c, case("a", True)])
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("| x | analyst | ⚠️ quarantined (T185): fail |", r.stdout)

    def test_only_quarantined_cases_counts_zero_of_zero_and_passes(self):
        r = self.run_script([case("x", False, q="T185")])
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("## Evals: 0 of 0 counted cases pass^3, 1 quarantined", r.stdout)

    def test_a_quarantined_trials_usage_still_counts_in_the_totals(self):
        u = {"input_tokens": 10, "output_tokens": 5, "cost_usd": 0.25}
        r = self.run_script([case("x", False, q="T185", usage=u), case("a", True, usage=u)])
        self.assertIn("Tokens: 20 in, 10 out; cost $0.50", r.stdout)

    def test_report_without_quarantine_is_todays_output(self):
        u = {"input_tokens": 3, "output_tokens": 2, "cost_usd": 0.125}
        cases = [case("a", True, usage=u), case("b", False, agent="steps")]
        c = {"agent": "?", "trials": []}  # missing name and no trials
        r = self.run_script(cases + [c])
        expected = ("## Evals: 1 of 3 cases pass^3\n\n"
                    "| Case | Agent | pass^3 |\n|------|-------|--------|\n"
                    "| a | analyst | ✅ pass |\n"
                    "| b | steps | ❌ fail |\n"
                    "| ? | ? | ❌ fail |\n\n"
                    "Tokens: 3 in, 2 out; cost $0.12\n\n")
        # The joined text ends with "\n" (from the last empty line) and print() adds another.
        self.assertEqual(r.stdout, expected)
        self.assertEqual(r.returncode, 1)

    def test_all_passing_report_without_quarantine_has_no_quarantine_words(self):
        r = self.run_script([case("a", True), case("b", True)])
        self.assertEqual(r.returncode, 0)
        self.assertTrue(r.stdout.startswith("## Evals: 2 of 2 cases pass^3\n"))
        self.assertNotIn("quarantine", r.stdout)


if __name__ == "__main__":
    unittest.main()
