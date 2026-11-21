"""Held-out and mutant counts in scripts/ci-evals-summary.py (T015), written before the change.

Judged from src/tests/evals/steps/README.md, "The report" and "The summary":
- a report with no `counts` on any grader result gives today's output byte for byte;
- otherwise the table gains `Held-out` and `Mutants killed` after `pass^3`, each listing the
  case's trials in order as `passed/total`, `n/a` for an agent the grader does not judge and `—`
  for a trial of that agent that reports no counts;
- after the table, one line per trial with failed held-out tests (`held-out failed: <names>`) or
  a stopped/failed new grader (`<grader>: <reason>`), in case order then trial order;
- quarantine, the totals line, tokens and cost, and the exit code are unchanged.
Not pinned (the contract is silent): the separator row's dashes, the wording of the surviving
mutant line, whether failure lines precede the tokens line, the cell of a case with no trials,
failure lines of quarantined cases, and the failure line of a trial whose grader result is absent.
"""
import json
import subprocess
import tempfile
import unittest
from pathlib import Path

from test_ci_scripts import EVALS, base_env

HEADER = "| Case | Agent | pass^3 | Held-out | Mutants killed |"
HELD = "HeldOutTestsPass"
MUT = "MutantsKilled"


def held(n_passed, failed=(), total=6):
    return {"grader": HELD, "passed": n_passed == total,
            "reason": f"{n_passed} of {total} held-out tests passed",
            "counts": {"total": total, "passed": n_passed, "failed": list(failed)}}


def mut(n_killed, failed=(), total=8):
    return {"grader": MUT, "passed": n_killed == total,
            "reason": f"{n_killed} of {total} mutants killed",
            "counts": {"total": total, "passed": n_killed, "failed": list(failed)}}


def stopped(grader, reason):
    return {"grader": grader, "passed": False, "reason": reason}


def trial(n, result=None, extra=(), usage=None, passed=None):
    results = [{"grader": "Builds", "passed": True, "reason": "ok"}, *extra]
    if result is not None:
        results.append(result)
    if passed is None:
        passed = all(r["passed"] for r in results)
    t = {"trial": n, "passed": passed, "results": results}
    if usage:
        t["usage"] = usage
    return t


def case(name, agent, trials, q=None):
    c = {"case": name, "agent": agent, "passed": all(t["passed"] for t in trials),
         "trials": trials}
    if q:
        c["quarantined_by"] = q
    return c


def code_case(name="code/example", q=None):
    return case(name, "code", [trial(1, held(6)),
                               trial(2, held(4, ["TestR1_EmptyList", "TestR1_ThreeInOrder"])),
                               trial(3, held(6))], q)


def tests_case(name="tests/example"):
    return case(name, "tests", [trial(i, mut(8)) for i in (1, 2, 3)])


def review_case(name="review/clean"):
    return case(name, "review", [trial(i) for i in (1, 2, 3)])


class CountsSummary(unittest.TestCase):
    def run_script(self, cases, env_extra=None):
        t = tempfile.TemporaryDirectory()
        self.addCleanup(t.cleanup)
        p = Path(t.name) / "report.json"
        p.write_text(json.dumps(cases), encoding="utf-8")
        r = subprocess.run(["python3", str(EVALS), str(p)], capture_output=True, text=True,
                           env=base_env(self, env_extra), cwd=t.name, timeout=60)
        r.dir = t.name
        return r

    def lines(self, r):
        return r.stdout.split("\n")

    def table(self, r):
        ls = self.lines(r)
        i = ls.index(HEADER)
        rows = []
        for ln in ls[i + 2:]:
            if not ln.startswith("|"):
                break
            rows.append(ln)
        return ls, i, rows

    def failure_lines(self, r):
        return [ln for ln in self.lines(r) if ln.startswith("- ")]

    # (a) no counts anywhere: today's output
    def test_a_report_without_counts_is_todays_output(self):
        u = {"input_tokens": 3, "output_tokens": 2, "cost_usd": 0.125}
        old_code = case("code/old", "code", [
            trial(1, {"grader": HELD, "passed": True, "reason": "ok"}, usage=u),
            trial(2, {"grader": HELD, "passed": False, "reason": "1 failed"})])
        old_tests = case("tests/old", "tests", [trial(1, usage=u)])
        quar = case("review/flaky", "review", [trial(1, passed=False)], q="T185")
        r = self.run_script([old_code, old_tests, quar])
        expected = ("## Evals: 1 of 2 counted cases pass^3, 1 quarantined\n\n"
                    "| Case | Agent | pass^3 |\n|------|-------|--------|\n"
                    "| code/old | code | ❌ fail |\n"
                    "| tests/old | tests | ✅ pass |\n"
                    "| review/flaky | review | ⚠️ quarantined (T185): fail |\n\n"
                    "Tokens: 6 in, 4 out; cost $0.25\n\n")
        self.assertEqual(r.stdout, expected)
        self.assertEqual(r.returncode, 1)

    def test_a_empty_report_is_todays_output(self):
        r = self.run_script([])
        self.assertEqual(r.stdout, "## Evals: 0 of 0 cases pass^3\n\n"
                         "| Case | Agent | pass^3 |\n|------|-------|--------|\n\n")
        self.assertEqual(r.returncode, 0)

    def test_a_counts_on_no_trial_adds_no_columns_or_lines(self):
        r = self.run_script([case("tests/x", "tests",
                                  [trial(1, stopped(MUT, "not run: Builds failed"))])])
        self.assertNotIn("Held-out", r.stdout)
        self.assertNotIn("Mutants killed", r.stdout)
        self.assertEqual(self.failure_lines(r), [])

    # (b) columns
    def test_b_columns_and_cells(self):
        r = self.run_script([code_case(), tests_case(), review_case()])
        ls, i, rows = self.table(r)
        self.assertEqual(ls[i - 2:i], ["## Evals: 2 of 3 cases pass^3", ""])
        sep = ls[i + 1]
        self.assertTrue(sep.startswith("|") and sep.endswith("|"), sep)
        self.assertEqual(sep.count("|"), 6, sep)
        self.assertEqual(rows, [
            "| code/example | code | ❌ fail | 6/6, 4/6, 6/6 | n/a |",
            "| tests/example | tests | ✅ pass | n/a | 8/8, 8/8, 8/8 |",
            "| review/clean | review | ✅ pass | n/a | n/a |"])

    def test_b_unknown_agent_is_na_for_both(self):
        r = self.run_script([code_case(), case("odd/x", "analyst", [trial(1)])])
        _, _, rows = self.table(r)
        self.assertIn("| odd/x | analyst | ✅ pass | n/a | n/a |", rows)

    def test_b_counts_on_one_trial_of_many(self):
        c = case("code/mixed", "code", [
            trial(1, held(6)), trial(2, stopped(HELD, "the code does not build")),
            trial(3, held(6))])
        r = self.run_script([c])
        _, _, rows = self.table(r)
        self.assertEqual(rows, ["| code/mixed | code | ❌ fail | 6/6, —, 6/6 | n/a |"])

    def test_b_old_code_trial_without_the_grader_shows_dash_next_to_counted_case(self):
        old = case("code/old", "code", [trial(1)])
        r = self.run_script([code_case(), old])
        _, _, rows = self.table(r)
        self.assertEqual(rows[1], "| code/old | code | ✅ pass | — | n/a |")

    def test_b_trial_without_results_is_a_dash_not_a_crash(self):
        c = case("tests/bare", "tests", [{"trial": 1, "passed": False}, trial(2, mut(8))])
        r = self.run_script([c, code_case()])
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        _, _, rows = self.table(r)
        self.assertEqual(rows[0], "| tests/bare | tests | ❌ fail | n/a | —, 8/8 |")

    def test_b_case_with_no_trials_does_not_crash(self):
        c = {"case": "code/none", "agent": "code", "trials": []}
        r = self.run_script([code_case(), c])
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        _, _, rows = self.table(r)
        self.assertTrue(rows[1].startswith("| code/none | code | ❌ fail |"), rows[1])

    # (c) held-out failure line
    def test_c_held_out_failure_line_names_failed_tests(self):
        r = self.run_script([code_case(), tests_case()])
        self.assertEqual(self.failure_lines(r), [
            "- code/example trial 2: held-out failed: TestR1_EmptyList, TestR1_ThreeInOrder"])

    def test_c_suffixes_stay_with_their_names(self):
        c = case("code/s", "code", [trial(1, held(4, ["TestR1_A (timed out)",
                                                      "TestR2_B (did not run)"]))])
        r = self.run_script([c])
        self.assertEqual(self.failure_lines(r), [
            "- code/s trial 1: held-out failed: TestR1_A (timed out), TestR2_B (did not run)"])

    def test_c_passing_trials_have_no_failure_line(self):
        r = self.run_script([code_case("a"), tests_case()])
        self.assertEqual(len(self.failure_lines(r)), 1)

    def test_c_failure_lines_follow_the_table_and_keep_case_then_trial_order(self):
        c1 = case("code/one", "code", [trial(1, held(5, ["TestR1_X"])),
                                       trial(2, held(5, ["TestR2_Y"]))])
        c2 = case("code/two", "code", [trial(1, held(5, ["TestR3_Z"]))])
        r = self.run_script([c1, c2])
        fl = self.failure_lines(r)
        self.assertEqual(fl, [
            "- code/one trial 1: held-out failed: TestR1_X",
            "- code/one trial 2: held-out failed: TestR2_Y",
            "- code/two trial 1: held-out failed: TestR3_Z"])
        ls = self.lines(r)
        self.assertGreater(ls.index(fl[0]), ls.index("| code/two | code | ❌ fail | 5/6 | n/a |"))

    # (d) stopped graders and surviving mutants
    def test_d_mutants_not_run_shows_dash_and_reason(self):
        reason = "not run: TestsBuild failed"
        c = case("tests/stopped", "tests", [trial(1, stopped(MUT, reason)),
                                            trial(2, mut(8)), trial(3, mut(8))])
        r = self.run_script([c])
        _, _, rows = self.table(r)
        self.assertEqual(rows, ["| tests/stopped | tests | ❌ fail | n/a | —, 8/8, 8/8 |"])
        self.assertEqual(self.failure_lines(r), [f"- tests/stopped trial 1: MutantsKilled: {reason}"])

    def test_d_tests_do_not_pass_against_reference(self):
        reason = "the tests do not pass against the reference implementation"
        c = case("tests/inject", "tests", [trial(1, mut(8)), trial(2, mut(8)),
                                           trial(3, stopped(MUT, reason))])
        r = self.run_script([c])
        _, _, rows = self.table(r)
        self.assertEqual(rows, ["| tests/inject | tests | ❌ fail | n/a | 8/8, 8/8, — |"])
        self.assertEqual(self.failure_lines(r), [f"- tests/inject trial 3: MutantsKilled: {reason}"])

    def test_d_code_trial_that_stopped_shows_dash_and_reason(self):
        c = case("code/nb", "code", [trial(1, stopped(HELD, "the code does not build"))])
        r = self.run_script([c, case("code/ok", "code", [trial(1, held(6))])])
        _, _, rows = self.table(r)
        self.assertEqual(rows[0], "| code/nb | code | ❌ fail | — | n/a |")
        self.assertEqual(self.failure_lines(r), ["- code/nb trial 1: HeldOutTestsPass: the code does not build"])

    def test_d_surviving_mutants_line_names_every_survivor(self):
        # The README fixes only the held-out wording; the mutant wording is a contract gap.
        c = case("tests/surv", "tests", [trial(1, mut(8)),
                                         trial(2, mut(5, ["R-2/always-one", "R-3/x (timed out)",
                                                          "R-4/y (did not run)"])),
                                         trial(3, mut(8))])
        r = self.run_script([c])
        _, _, rows = self.table(r)
        self.assertEqual(rows, ["| tests/surv | tests | ❌ fail | n/a | 8/8, 5/8, 8/8 |"])
        fl = self.failure_lines(r)
        self.assertEqual(len(fl), 1, fl)
        self.assertTrue(fl[0].startswith("- tests/surv trial 2:"), fl[0])
        for name in ("R-2/always-one", "R-3/x (timed out)", "R-4/y (did not run)"):
            self.assertIn(name, fl[0])

    # (e) quarantine and tokens as today
    def test_e_quarantined_case_with_counts_and_tokens(self):
        u = {"input_tokens": 10, "output_tokens": 5, "cost_usd": 0.25}
        c = code_case("code/flaky", q="T185")
        c["trials"][0]["usage"] = u
        t = tests_case()
        t["trials"][1]["usage"] = u
        r = self.run_script([c, t])
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("## Evals: 1 of 1 counted cases pass^3, 1 quarantined\n", r.stdout)
        _, _, rows = self.table(r)
        self.assertEqual(rows, [
            "| code/flaky | code | ⚠️ quarantined (T185): fail | 6/6, 4/6, 6/6 | n/a |",
            "| tests/example | tests | ✅ pass | n/a | 8/8, 8/8, 8/8 |"])
        self.assertIn("\nTokens: 20 in, 10 out; cost $0.50\n", r.stdout)

    # (f) exit code
    def test_f_exit_0_when_every_counted_case_passed(self):
        r = self.run_script([tests_case(), review_case(),
                             case("code/ok", "code", [trial(i, held(6)) for i in (1, 2, 3)])])
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(self.failure_lines(r), [])

    def test_f_exit_1_when_a_counted_case_failed_on_held_out(self):
        r = self.run_script([code_case(), tests_case()])
        self.assertEqual(r.returncode, 1)

    def test_f_exit_1_when_a_counted_case_failed_on_a_stopped_grader(self):
        c = case("tests/s", "tests", [trial(1, stopped(MUT, "not run: TestsBuild failed"))])
        r = self.run_script([c, review_case()])
        self.assertEqual(r.returncode, 1)

    def test_f_quarantined_failure_with_counts_exits_0(self):
        r = self.run_script([code_case(q="T185"), review_case()])
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    # step summary file gets the same text
    def test_summary_file_gets_the_same_text_as_stdout(self):
        cases = [code_case(), tests_case(), review_case()]
        plain = self.run_script(cases)
        t = tempfile.TemporaryDirectory()
        self.addCleanup(t.cleanup)
        out = Path(t.name) / "summary.md"
        filed = self.run_script(cases, {"GITHUB_STEP_SUMMARY": str(out)})
        self.assertEqual(filed.stdout, "")
        self.assertEqual(out.read_text(encoding="utf-8") + "\n", plain.stdout)


if __name__ == "__main__":
    unittest.main()
