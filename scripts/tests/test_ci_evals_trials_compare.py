"""The comparison of scripts/ci-evals-trials.py (feature 008, T013), written before the code.

Judged from the script's docstring ("Comparison", "With no comparison"), data-model.md
("Baseline and comparison", "Earlier map") and tasks.md T013. The comparison table is parsed by
header names (Case, Passed, Median seconds, Median cost $); the trials table is not checked.

Assumptions where the contract is silent (each marked in a test):
- a baseline entry that "could not be read" gives a heading "### No earlier run to compare with:"
  (no "### Since run" section); its reason text is not pinned beyond mentioning "read";
- a median both sides lack is written `n/a → n/a`;
- a case that is "nothing to compare with" has that text in the Passed cell and empty cells after;
- an even count's median is the mean of the middle two, written like the trials table writes it.
"""
import json
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from test_ci_scripts import SCRIPTS, base_env

TRIALS = SCRIPTS / "ci-evals-trials.py"
SHA = "0123456789abcdef0123456789abcdef01234567"
OLD = "abcdef0123456789abcdef0123456789abcdef01"
NO_EARLIER = "no earlier run could be read"
LESS = "📉 less reliable"
NOTHING = "nothing to compare with"


def tr(passed=True, **extra):
    return {"passed": passed, **extra}


def trials(passed, failed=0, **extra):
    return [tr(True, **extra) for _ in range(passed)] + [tr(False, **extra) for _ in range(failed)]


def mcase(name, ts=None, agent="analyst"):
    return {"case": name, "agent": agent, "trials": ts if ts is not None else trials(3)}


def meas(cases, **over):
    m = {"version": 1, "suite": "harness", "cases": "", "key": "harness", "commit": SHA,
         "branch": "main", "run_id": 7, "run_attempt": 1, "measured_cases": cases}
    m.update(over)
    return m


def entry(cases=None, run_id=45071292368, commit=OLD, measurement=None):
    return {"run_id": run_id, "commit": commit,
            "measurement": measurement if measurement is not None else meas(cases or [])}


def cells(line):
    parts = re.split(r"(?<!\\)\|", line)
    return [p.strip() for p in parts[1:-1]]


class TrialsCompare(unittest.TestCase):
    def setUp(self):
        t = tempfile.TemporaryDirectory()
        self.addCleanup(t.cleanup)
        self.dir = Path(t.name)

    def run_with(self, now_cases, earlier_value, raw=False):
        p = self.dir / "measurement.json"
        p.write_text(json.dumps(meas(now_cases)), encoding="utf-8")
        env_extra = {}
        if earlier_value is not None:
            env_extra["EARLIER"] = earlier_value if raw else json.dumps(earlier_value)
        r = subprocess.run([sys.executable, str(TRIALS), str(p)], capture_output=True, text=True,
                           encoding="utf-8", env=base_env(self, env_extra), cwd=str(self.dir),
                           timeout=60)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNotIn("Traceback", r.stderr)
        self.assertNotIn("Traceback", r.stdout)
        return r

    def cmp(self, now_cases, base_cases, **kw):
        return self.run_with(now_cases, {"harness": entry(base_cases, **kw)})

    def rows(self, r):
        """The comparison table's rows, by header name."""
        ls = r.stdout.split("\n")
        since = [i for i, ln in enumerate(ls) if ln.startswith("### Since run")]
        self.assertEqual(len(since), 1, r.stdout)
        rest = ls[since[0] + 1:]
        idx = [i for i, ln in enumerate(rest) if ln.startswith("| Case |")]
        self.assertTrue(idx, "no comparison table in: " + r.stdout)
        header = cells(rest[idx[0]])
        out = []
        for ln in rest[idx[0] + 2:]:
            if not ln.startswith("|"):
                break
            out.append(dict(zip(header, cells(ln), strict=False)) | {"_n": len(cells(ln))})
        return header, out

    def one(self, now, base):
        _, rows = self.rows(self.cmp([now], [base]))
        self.assertEqual(len(rows), 1)
        return rows[0]

    def passed_cell(self, now_p, now_f, before_p, before_f):
        return self.one(mcase("c", trials(now_p, now_f)), mcase("c", trials(before_p, before_f)))["Passed"]

    def assert_not_read(self, r):
        ls = r.stdout.split("\n")
        self.assertFalse([ln for ln in ls if ln.startswith("### Since run")], r.stdout)
        heads = [ln for ln in ls if ln.startswith("### No earlier run to compare with:")]
        self.assertEqual(len(heads), 1, r.stdout)
        self.assertRegex(heads[0], r"read")

    # ---- the heading and table shape
    def test_heading_names_run_id_and_first_seven_commit_characters(self):
        r = self.cmp([mcase("a")], [mcase("a")], run_id=45071292368, commit=OLD)
        self.assertIn("### Since run 45071292368 (abcdef0)", r.stdout.split("\n"))
        self.assertNotIn("### No earlier run", r.stdout)

    def test_heading_follows_the_trials_table_and_precedes_the_comparison_table(self):
        r = self.cmp([mcase("a")], [mcase("a")])
        ls = r.stdout.split("\n")
        hs = [i for i, ln in enumerate(ls) if ln.startswith("### Since run")]
        self.assertEqual(len(hs), 1, r.stdout)
        h = hs[0]
        first = next(i for i, ln in enumerate(ls) if ln.startswith("| Case |"))
        self.assertLess(first, h)
        self.assertEqual(ls[h + 1], "")
        self.assertTrue(ls[h + 2].startswith("| Case |"))
        self.assertNotIn("Since run", "\n".join(ls[:first]))

    def test_columns_and_separator_row(self):
        r = self.cmp([mcase("a")], [mcase("a")])
        header, rows = self.rows(r)
        self.assertEqual(header, ["Case", "Passed", "Median seconds", "Median cost $"])
        ls = r.stdout.split("\n")
        hs = [i for i, ln in enumerate(ls) if ln.startswith("### Since run")]
        self.assertEqual(len(hs), 1, r.stdout)
        h = hs[0]
        sep = cells(ls[h + 3])
        self.assertEqual(len(sep), 4)
        self.assertTrue(all(re.fullmatch(r"-+", s) for s in sep), sep)
        self.assertEqual(rows[0]["_n"], 4)

    def test_rows_follow_the_current_measurements_case_order(self):
        r = self.cmp([mcase("zeta"), mcase("alpha"), mcase("mid")],
                     [mcase("mid"), mcase("alpha"), mcase("zeta")])
        _, rows = self.rows(r)
        self.assertEqual([x["Case"] for x in rows], ["zeta", "alpha", "mid"])

    # ---- passed and less reliable
    def test_passed_before_and_now(self):
        self.assertEqual(self.passed_cell(3, 0, 3, 0), "3/3 → 3/3")

    def test_less_reliable_3_of_3_to_4_of_5(self):
        self.assertEqual(self.passed_cell(4, 1, 3, 0), f"3/3 → 4/5 {LESS}")

    def test_not_less_reliable_2_of_3_to_5_of_5(self):
        self.assertEqual(self.passed_cell(5, 0, 2, 1), "2/3 → 5/5")

    def test_equal_share_is_not_less_reliable_1_of_2_to_2_of_4(self):
        self.assertEqual(self.passed_cell(2, 2, 1, 1), "1/2 → 2/4")

    def test_less_reliable_all_pass_to_none_pass(self):
        self.assertEqual(self.passed_cell(0, 3, 3, 0), f"3/3 → 0/3 {LESS}")

    def test_none_passed_on_both_sides_is_not_less_reliable(self):
        self.assertEqual(self.passed_cell(0, 3, 0, 3), "0/3 → 0/3")

    def test_better_share_is_not_less_reliable(self):
        self.assertEqual(self.passed_cell(3, 0, 1, 2), "1/3 → 3/3")

    def test_less_reliable_by_a_hair_with_big_counts(self):
        # 499/1000 now against 1/2 before: 499*2 < 1*1000
        self.assertEqual(self.passed_cell(499, 501, 1, 1), f"1/2 → 499/1000 {LESS}")

    def test_the_marker_appears_once_and_only_for_that_case(self):
        r = self.cmp([mcase("worse", trials(1, 2)), mcase("same", trials(3))],
                     [mcase("worse", trials(3)), mcase("same", trials(3))])
        _, rows = self.rows(r)
        self.assertIn(LESS, rows[0]["Passed"])
        self.assertNotIn("📉", rows[1]["Passed"])
        self.assertEqual(r.stdout.count("📉"), 1)

    # ---- medians
    def test_median_seconds_and_cost_before_arrow_now(self):
        now = mcase("c", [tr(True, seconds=12.1, cost_usd=0.01), tr(True, seconds=14.0, cost_usd=0.012),
                          tr(False, seconds=30.5, cost_usd=0.02)])
        before = mcase("c", [tr(True, seconds=10.0, cost_usd=0.011), tr(True, seconds=13.0, cost_usd=0.0110),
                             tr(True, seconds=50.0, cost_usd=0.5)])
        row = self.one(now, before)
        self.assertEqual(row["Median seconds"], "13.0 → 14.0")
        self.assertEqual(row["Median cost $"], "0.0110 → 0.0120")

    def test_even_count_median_is_the_mean_of_the_middle_two(self):
        now = mcase("c", [tr(seconds=1.0, cost_usd=1.0), tr(seconds=2.0, cost_usd=1.0)])
        before = mcase("c", [tr(seconds=4.0, cost_usd=0.5), tr(seconds=7.0, cost_usd=0.25),
                             tr(seconds=100.0, cost_usd=0.75), tr(seconds=5.0, cost_usd=0.25)])
        row = self.one(now, before)
        self.assertEqual(row["Median seconds"], "6.0 → 1.5")
        self.assertEqual(row["Median cost $"], "0.3750 → 1.0000")

    def test_median_ignores_trials_without_the_measure(self):
        now = mcase("c", [tr(seconds=5.0), tr(), tr(seconds=9.0), tr(seconds=7.0)])
        before = mcase("c", [tr(seconds=1.0), tr()])
        self.assertEqual(self.one(now, before)["Median seconds"], "1.0 → 7.0")

    def test_median_before_lacks_is_na(self):
        now = mcase("c", [tr(seconds=14.0, cost_usd=0.012)])
        before = mcase("c", [tr(), tr()])
        row = self.one(now, before)
        self.assertEqual(row["Median seconds"], "n/a → 14.0")
        self.assertEqual(row["Median cost $"], "n/a → 0.0120")

    def test_median_now_lacks_is_na(self):
        now = mcase("c", [tr()])
        before = mcase("c", [tr(seconds=13.0, cost_usd=0.011)])
        row = self.one(now, before)
        self.assertEqual(row["Median seconds"], "13.0 → n/a")
        self.assertEqual(row["Median cost $"], "0.0110 → n/a")

    def test_median_lacked_by_both_sides_assumed_na_arrow_na(self):
        row = self.one(mcase("c"), mcase("c"))
        self.assertEqual(row["Median seconds"], "n/a → n/a")
        self.assertEqual(row["Median cost $"], "n/a → n/a")

    def test_zero_seconds_is_a_median_not_n_a(self):
        row = self.one(mcase("c", [tr(seconds=0)]), mcase("c", [tr(seconds=0.0)]))
        self.assertEqual(row["Median seconds"], "0.0 → 0.0")

    # ---- cases on one side only
    def test_case_only_now_has_nothing_to_compare_with(self):
        _, rows = self.rows(self.cmp([mcase("a"), mcase("new-case")], [mcase("a")]))
        self.assertEqual([x["Case"] for x in rows], ["a", "new-case"])
        self.assertEqual(rows[1]["Passed"], NOTHING)
        self.assertEqual(rows[1]["Median seconds"], "")
        self.assertEqual(rows[1]["Median cost $"], "")
        self.assertEqual(rows[1]["_n"], 4)

    def test_case_only_before_is_not_shown(self):
        r = self.cmp([mcase("a")], [mcase("gone"), mcase("a")])
        _, rows = self.rows(r)
        self.assertEqual([x["Case"] for x in rows], ["a"])
        self.assertNotIn("gone", r.stdout)

    def test_case_names_match_exactly(self):
        _, rows = self.rows(self.cmp([mcase("Case-A")], [mcase("case-a")]))
        self.assertEqual(rows[0]["Passed"], NOTHING)

    def test_no_trials_now_has_nothing_to_compare_with(self):
        row = self.one(mcase("c", []), mcase("c", trials(3)))
        self.assertEqual(row["Passed"], NOTHING)

    def test_no_trials_before_has_nothing_to_compare_with(self):
        row = self.one(mcase("c", trials(3)), mcase("c", []))
        self.assertEqual(row["Passed"], NOTHING)
        self.assertNotIn(LESS, row["Passed"])

    def test_no_trials_on_either_side_has_nothing_to_compare_with(self):
        row = self.one(mcase("c", []), mcase("c", []))
        self.assertEqual(row["Passed"], NOTHING)

    def test_no_cases_now_gives_the_heading_and_no_rows(self):
        r = self.cmp([], [mcase("a")])
        _, rows = self.rows(r)
        self.assertEqual(rows, [])

    def test_no_cases_before_everything_has_nothing_to_compare_with(self):
        _, rows = self.rows(self.cmp([mcase("a"), mcase("b")], []))
        self.assertEqual([x["Passed"] for x in rows], [NOTHING, NOTHING])

    def test_first_case_of_a_repeated_name_in_the_baseline_is_compared(self):
        _, rows = self.rows(self.cmp([mcase("c", trials(3))],
                                     [mcase("c", trials(2, 1)), mcase("c", trials(0, 3))]))
        self.assertEqual(rows[0]["Passed"], "2/3 → 3/3")

    def test_repeated_names_now_are_each_compared_with_the_baselines_first(self):
        _, rows = self.rows(self.cmp([mcase("c", trials(3)), mcase("c", trials(1, 2))],
                                     [mcase("c", trials(3)), mcase("c", trials(0, 3))]))
        self.assertEqual(len(rows), 2)
        self.assertEqual(rows[0]["Passed"], "3/3 → 3/3")
        self.assertEqual(rows[1]["Passed"], f"3/3 → 1/3 {LESS}")

    # ---- names from data
    def test_pipe_in_a_case_name_is_escaped_and_keeps_the_columns(self):
        r = self.cmp([mcase("a|b")], [mcase("a|b")])
        line = next(ln for ln in r.stdout.split("\n") if ln.startswith("| a"))
        self.assertTrue(line.startswith("| a\\|b |"), line)
        _, rows = self.rows(r)
        self.assertEqual(rows[0]["_n"], 4)
        self.assertEqual(rows[0]["Passed"], "3/3 → 3/3")

    def test_line_break_in_a_case_name_is_a_space_and_adds_no_row(self):
        r = self.cmp([mcase("a\nb")], [mcase("a\nb")])
        _, rows = self.rows(r)
        self.assertEqual([x["Case"] for x in rows], ["a b"])

    # ---- no comparison
    def test_none_entry_gives_its_reason(self):
        r = self.run_with([mcase("a")], {"harness": {"none": "no earlier run"}})
        self.assertIn("### No earlier run to compare with: no earlier run", r.stdout.split("\n"))
        self.assertNotIn("Since run", r.stdout)

    def test_none_reason_is_escaped_as_text_from_data(self):
        r = self.run_with([mcase("a")], {"harness": {"none": "a|b\nc\x07d e"}})
        self.assertIn("### No earlier run to compare with: a\\|b c d e", r.stdout.split("\n"))
        self.assertEqual(len([ln for ln in r.stdout.split("\n") if ln.startswith("### ")]), 1)

    def test_none_reason_cannot_inject_markup_lines(self):
        r = self.run_with([mcase("a")], {"harness": {"none": "x\n## Fake\n| a | b |"}})
        ls = r.stdout.split("\n")
        self.assertFalse([ln for ln in ls if ln.startswith("## Fake")], r.stdout)
        self.assertIn("### No earlier run to compare with: x ## Fake \\| a \\| b \\|", ls)

    def test_earlier_absent_empty_not_json_list_or_without_the_key(self):
        cases = {"absent": None, "empty": "", "not json": "{nope", "list": "[]",
                 "list of entries": json.dumps([entry([mcase("a")])]), "string": '"x"',
                 "null": "null", "number": "5", "other key": json.dumps({"other": entry([])}),
                 "empty object": "{}"}
        for label, v in cases.items():
            with self.subTest(label):
                r = self.run_with([mcase("a")], v, raw=True)
                self.assertIn("### No earlier run to compare with: " + NO_EARLIER, r.stdout.split("\n"))
                self.assertNotIn("Since run", r.stdout)

    def test_entry_of_the_wrong_type_is_not_read(self):
        for label, v in {"null": None, "list": [], "string": "x", "int": 3, "empty": {},
                         "none not text": {"none": 5}}.items():
            with self.subTest(label):
                self.assert_not_read(self.run_with([mcase("a")], {"harness": v}))

    def test_another_keys_entry_is_not_used(self):
        r = self.run_with([mcase("a")], {"other": entry([mcase("a")]), "harness": {"none": "why"}})
        self.assertIn("### No earlier run to compare with: why", r.stdout.split("\n"))

    # ---- baselines that cannot be read
    def test_run_id_that_is_not_a_positive_integer_is_not_read(self):
        for v in (0, -1, "5", True, False, 5.0, 1.5, None, [5], {"a": 1}):
            with self.subTest(run_id=v):
                self.assert_not_read(self.cmp([mcase("a")], [mcase("a")], run_id=v))

    def test_missing_run_id_commit_or_measurement_is_not_read(self):
        for drop in ("run_id", "commit", "measurement"):
            with self.subTest(drop=drop):
                e = entry([mcase("a")])
                del e[drop]
                self.assert_not_read(self.run_with([mcase("a")], {"harness": e}))

    def test_commit_that_is_not_40_lowercase_hex_is_not_read(self):
        bad = {"uppercase": OLD.upper(), "39 chars": OLD[:39], "41 chars": OLD + "a", "empty": "",
               "non hex": "g" * 40, "script": "<script>" + "a" * 32, "pipe": "abcdef|" + "a" * 33,
               "number": 5, "null": None, "newline": OLD[:39] + "\n", "list": [OLD],
               "mixed case": OLD[:20] + OLD[20:].upper(), "spaces": " " + OLD[1:]}
        for label, v in bad.items():
            with self.subTest(label):
                r = self.cmp([mcase("a")], [mcase("a")], commit=v)
                self.assert_not_read(r)
                self.assertNotIn("<script>", r.stdout)

    def test_planted_commit_text_never_reaches_the_output(self):
        r = self.cmp([mcase("a")], [mcase("a")], commit="<script>alert(1)</script>" + "a" * 20)
        self.assertNotIn("<script>", r.stdout)
        self.assertNotIn("alert", r.stdout)

    def test_measurement_breaking_the_measure_rules_is_not_read(self):
        good = meas([mcase("a")])
        bad = {
            "not an object": [],
            "string": "x",
            "null": None,
            "no measured_cases": {k: v for k, v in good.items() if k != "measured_cases"},
            "measured_cases not a list": {**good, "measured_cases": {}},
            "run_id string": {**good, "run_id": "7"},
            "version bool": {**good, "version": True},
            "suite number": {**good, "suite": 3},
            "key missing": {k: v for k, v in good.items() if k != "key"},
            "case without agent": meas([{"case": "a", "trials": trials(1)}]),
            "trials not a list": meas([{"case": "a", "agent": "x", "trials": "3"}]),
            "passed not bool": meas([mcase("a", [{"passed": 1}])]),
            "negative seconds": meas([mcase("a", [tr(seconds=-1)])]),
            "attempts float": meas([mcase("a", [tr(attempts=1.5)])]),
            "cost string": meas([mcase("a", [tr(cost_usd="0.1")])]),
            "case not an object": meas(["a"]),
        }
        for label, m in bad.items():
            with self.subTest(label):
                self.assert_not_read(self.run_with(
                    [mcase("a")], {"harness": {"run_id": 5, "commit": OLD, "measurement": m}}))

    def test_nan_in_the_baseline_is_not_read(self):
        raw = ('{"harness": {"run_id": 5, "commit": "%s", "measurement": {"version": 1, "suite": "s",'
               ' "cases": "", "key": "k", "commit": "c", "branch": "b", "run_id": 1, "run_attempt": 1,'
               ' "measured_cases": [{"case": "a", "agent": "x", "trials":'
               ' [{"passed": true, "seconds": NaN}]}]}}}' % OLD)
        self.assert_not_read(self.run_with([mcase("a")], raw, raw=True))

    def test_a_broken_baseline_leaves_the_trials_table_intact(self):
        r = self.cmp([mcase("a")], [mcase("a")], run_id=0)
        self.assertIn("## Trials: 3 of 3 passed, 0 unstable cases", r.stdout)
        self.assertIn("| a |", r.stdout)

    def test_baseline_with_the_measure_fallbacks_is_read(self):
        fb = meas([mcase("a", trials(2, 1))], suite="?", key="?", commit="?",
                  branch="?", run_id=0, run_attempt=0, version=0)
        r = self.run_with([mcase("a", trials(3))], {"harness": entry(measurement=fb)})
        _, rows = self.rows(r)
        self.assertEqual(rows[0]["Passed"], "2/3 → 3/3")
        self.assertIn("### Since run 45071292368 (abcdef0)", r.stdout.split("\n"))

    def test_baseline_with_empty_strings_is_read(self):
        fb = meas([mcase("a")], suite="", key="", commit="", branch="")
        r = self.run_with([mcase("a")], {"harness": entry(measurement=fb)})
        self.assertEqual(self.rows(r)[1][0]["Passed"], "3/3 → 3/3")

    def test_baseline_with_deep_nesting_does_not_crash(self):
        raw = '{"harness": {"run_id": 5, "commit": "%s", "measurement": %s}}' % (
            OLD, "[" * 20000 + "]" * 20000)
        r = self.run_with([mcase("a")], raw, raw=True)
        self.assertNotIn("Since run", r.stdout)

    # ---- determinism and exit
    def test_same_inputs_give_the_same_text(self):
        now = [mcase("a", trials(2, 1)), mcase("b"), mcase("n", trials(1))]
        base = [mcase("b", trials(1, 2)), mcase("a", trials(3))]
        self.assertEqual(self.cmp(now, base).stdout, self.cmp(now, base).stdout)

    def test_key_order_in_the_baseline_does_not_change_the_text(self):
        e = entry([mcase("a", trials(2, 1))])
        r1 = self.run_with([mcase("a")], {"harness": e, "z": {"none": "x"}})
        r2 = self.run_with([mcase("a")], {"z": {"none": "x"}, "harness": dict(reversed(list(e.items())))})
        self.assertEqual(r1.stdout, r2.stdout)

    def test_exit_0_and_two_only_on_argument_count(self):
        env = base_env(self, {"EARLIER": json.dumps({"harness": entry([mcase("a")])})})
        for args in ([], ["a", "b"]):
            r = subprocess.run([sys.executable, str(TRIALS)] + args, capture_output=True, text=True,
                               env=env, cwd=str(self.dir), timeout=60)
            self.assertEqual(r.returncode, 2)
            self.assertEqual(r.stdout, "")

    def test_comparison_goes_to_the_summary_file_when_set(self):
        p = self.dir / "m.json"
        p.write_text(json.dumps(meas([mcase("a")])), encoding="utf-8")
        s = self.dir / "summary.md"
        s.write_text("before\n", encoding="utf-8")
        env = base_env(self, {"EARLIER": json.dumps({"harness": entry([mcase("a")])}),
                              "GITHUB_STEP_SUMMARY": str(s)})
        r = subprocess.run([sys.executable, str(TRIALS), str(p)], capture_output=True, text=True,
                           env=env, cwd=str(self.dir), timeout=60)
        self.assertEqual(r.returncode, 0, r.stderr)
        t = s.read_text(encoding="utf-8")
        self.assertTrue(t.startswith("before\n"))
        self.assertIn("### Since run 45071292368 (abcdef0)", t)
        self.assertEqual(r.stdout, "")


if __name__ == "__main__":
    unittest.main()
