"""The spread columns of scripts/ci-evals-trials.py (feature 008, T008), written before the code.

Judged from the Spread bullet of the script's docstring, how it writes numbers (seconds
format(v, ".1f"), cost format(v, ".4f"), tokens and attempts integers, an even-count median between
two integers with one decimal) and specs/008-eval-variance/data-model.md ("Derived per case").
Each spread cell is "<low>, <median>, <high>", followed by " (<n> of <m> trials)" exactly when
some of the case's m trials lack the measure, and "n/a" alone when none carries it. The table is
parsed by header names; the other columns are not checked here.

Assumptions where the contract is silent (each marked in a test):
- the suffix is separated from the numbers by one space, as the contract's example shows;
- m is the case's own trial count, not the suite's k;
- a case with no trials has no trial carrying any measure, so every spread cell is n/a;
- an integer median (odd count, or an even count whose middles sum to an even number) has no
  decimal: 3, not 3.0.
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
COLS = {"seconds": "Seconds", "cost_usd": "Cost $", "input_tokens": "Tokens in",
        "output_tokens": "Tokens out", "attempts": "Attempts"}


def cells(line):
    parts = re.split(r"(?<!\\)\|", line)
    return [p.strip() for p in parts[1:-1]]


def tr(passed=True, **extra):
    return {"passed": passed, **extra}


def mcase(name, ts, q=None):
    c = {"case": name, "agent": "analyst", "trials": ts}
    if q:
        c["quarantined_by"] = q
    return c


def meas(cases):
    return {"version": 1, "suite": "harness", "cases": "", "key": "harness", "commit": SHA,
            "branch": "main", "run_id": 7, "run_attempt": 1, "measured_cases": cases}


def with_field(field, values, passed=True):
    """One trial per value; None leaves the field out of that trial."""
    return [tr(passed) if v is None else tr(passed, **{field: v}) for v in values]


class SpreadColumns(unittest.TestCase):
    def setUp(self):
        t = tempfile.TemporaryDirectory()
        self.addCleanup(t.cleanup)
        self.dir = Path(t.name)

    def rows(self, cases):
        p = self.dir / "measurement.json"
        p.write_text(json.dumps(meas(cases)), encoding="utf-8")
        r = subprocess.run([sys.executable, str(TRIALS), str(p)], capture_output=True, text=True,
                           encoding="utf-8", env=base_env(self), cwd=str(self.dir), timeout=60)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNotIn("Traceback", r.stderr)
        ls = r.stdout.split("\n")
        idx = [i for i, ln in enumerate(ls) if ln.startswith("| Case |")]
        self.assertTrue(idx, "no table in: " + r.stdout)
        header = cells(ls[idx[0]])
        out = []
        for ln in ls[idx[0] + 2:]:
            if not ln.startswith("|"):
                break
            c = cells(ln)
            self.assertEqual(len(c), len(header), ln)
            out.append(dict(zip(header, c, strict=True)))
        return header, out

    def cell(self, field, values, **kw):
        """The spread cell of `field` for one case whose trials carry `values`."""
        header, rows = self.rows([mcase("a", with_field(field, values, **kw))])
        self.assertEqual(len(rows), 1)
        col = [h for h in header if h.startswith(COLS[field])]
        self.assertEqual(len(col), 1, header)
        return rows[0][col[0]]

    def test_the_header_names_each_spread_with_low_median_high(self):
        header, _ = self.rows([mcase("a", [tr()])])
        for name in COLS.values():
            col = [h for h in header if h.startswith(name)]
            self.assertEqual(len(col), 1, header)
            self.assertIn("low, median, high", col[0])

    # ---- seconds: one decimal
    def test_seconds_quickstart_10_20_60(self):
        self.assertEqual(self.cell("seconds", [10, 20, 60]), "10.0, 20.0, 60.0")

    def test_seconds_floats_odd_count(self):
        self.assertEqual(self.cell("seconds", [12.5, 14.0, 30.5]), "12.5, 14.0, 30.5")

    def test_seconds_even_count_median_is_the_mean_of_the_middle_two(self):
        self.assertEqual(self.cell("seconds", [10.0, 20.0, 30.0, 50.0]), "10.0, 25.0, 50.0")

    def test_seconds_unsorted_input(self):
        self.assertEqual(self.cell("seconds", [60.0, 10.0, 20.0]), "10.0, 20.0, 60.0")
        self.assertEqual(self.cell("seconds", [50.0, 10.0, 30.0, 20.0]), "10.0, 25.0, 50.0")

    def test_seconds_one_trial_gives_the_same_value_three_times(self):
        self.assertEqual(self.cell("seconds", [7.5]), "7.5, 7.5, 7.5")

    def test_seconds_two_trials_median_is_their_mean(self):
        self.assertEqual(self.cell("seconds", [10.0, 20.0]), "10.0, 15.0, 20.0")

    def test_seconds_are_written_as_format_dot_1f(self):
        v = [0.04, 1.23456, 99.97]  # rounding away from the digits shown
        lo, md, hi = (format(x, ".1f") for x in v)
        self.assertEqual(self.cell("seconds", v), f"{lo}, {md}, {hi}")
        self.assertEqual(self.cell("seconds", v), "0.0, 1.2, 100.0")

    def test_seconds_zero_is_a_number(self):
        self.assertEqual(self.cell("seconds", [0, 0, 0]), "0.0, 0.0, 0.0")
        self.assertEqual(self.cell("seconds", [0, 2.0, 4.0]), "0.0, 2.0, 4.0")

    def test_seconds_all_equal(self):
        self.assertEqual(self.cell("seconds", [3.0, 3.0, 3.0, 3.0]), "3.0, 3.0, 3.0")

    def test_seconds_long_run(self):
        self.assertEqual(self.cell("seconds", [3600.0, 7200.0, 5400.0]),
                         "3600.0, 5400.0, 7200.0")

    # ---- cost: four decimals
    def test_cost_odd_count(self):
        self.assertEqual(self.cell("cost_usd", [0.02, 0.01, 0.012]), "0.0100, 0.0120, 0.0200")

    def test_cost_even_count_median_is_the_mean_of_the_middle_two(self):
        self.assertEqual(self.cell("cost_usd", [0.01, 0.03, 0.02, 0.05]),
                         "0.0100, 0.0250, 0.0500")

    def test_cost_tiny_value_rounds_to_four_decimals(self):
        self.assertEqual(self.cell("cost_usd", [0.00004, 0.00004, 0.00004]),
                         "0.0000, 0.0000, 0.0000")

    def test_cost_zero_is_a_number_not_na(self):
        self.assertEqual(self.cell("cost_usd", [0, 0, 0]), "0.0000, 0.0000, 0.0000")
        self.assertEqual(self.cell("cost_usd", [0.0]), "0.0000, 0.0000, 0.0000")

    def test_cost_zero_among_others(self):
        self.assertEqual(self.cell("cost_usd", [0.02, 0, 0.01]), "0.0000, 0.0100, 0.0200")

    def test_cost_one_trial(self):
        self.assertEqual(self.cell("cost_usd", [0.0123]), "0.0123, 0.0123, 0.0123")

    def test_cost_is_written_as_format_dot_4f(self):
        v = [0.00123, 0.5, 12.34567]
        expect = ", ".join(format(x, ".4f") for x in v)
        self.assertEqual(self.cell("cost_usd", v), expect)
        self.assertEqual(expect, "0.0012, 0.5000, 12.3457")

    def test_cost_integer_in_the_measurement(self):
        self.assertEqual(self.cell("cost_usd", [1, 2, 3]), "1.0000, 2.0000, 3.0000")

    # ---- tokens and attempts: integers
    def test_tokens_in_odd_count(self):
        self.assertEqual(self.cell("input_tokens", [6000, 5100, 5400]), "5100, 5400, 6000")

    def test_tokens_out_odd_count(self):
        self.assertEqual(self.cell("output_tokens", [950, 800, 900]), "800, 900, 950")

    def test_attempts_odd_count(self):
        self.assertEqual(self.cell("attempts", [2, 1, 1]), "1, 1, 2")

    def test_even_count_median_between_two_integers_has_one_decimal(self):
        for f in ("input_tokens", "output_tokens", "attempts"):
            with self.subTest(f=f):
                self.assertEqual(self.cell(f, [1, 2]), "1, 1.5, 2")
                self.assertEqual(self.cell(f, [1, 1, 2, 2]), "1, 1.5, 2")

    def test_even_count_median_that_is_an_integer_has_no_decimal(self):
        for f in ("input_tokens", "output_tokens", "attempts"):
            with self.subTest(f=f):
                self.assertEqual(self.cell(f, [2, 4]), "2, 3, 4")
                self.assertEqual(self.cell(f, [5, 5]), "5, 5, 5")
                self.assertEqual(self.cell(f, [1, 3, 5, 9]), "1, 4, 9")

    def test_even_count_unsorted(self):
        self.assertEqual(self.cell("attempts", [4, 1, 3, 2]), "1, 2.5, 4")

    def test_one_trial_integers(self):
        for f in ("input_tokens", "output_tokens", "attempts"):
            with self.subTest(f=f):
                self.assertEqual(self.cell(f, [42]), "42, 42, 42")

    def test_zero_tokens_and_attempts_are_numbers_not_na(self):
        for f in ("input_tokens", "output_tokens", "attempts"):
            with self.subTest(f=f):
                self.assertEqual(self.cell(f, [0, 0, 0]), "0, 0, 0")
                self.assertEqual(self.cell(f, [0]), "0, 0, 0")
                self.assertEqual(self.cell(f, [0, 1]), "0, 0.5, 1")

    def test_large_token_counts_are_whole_numbers_without_exponent(self):
        self.assertEqual(self.cell("input_tokens", [1000000, 123456789, 2500000]),
                         "1000000, 2500000, 123456789")
        self.assertEqual(self.cell("output_tokens", [10 ** 12, 3 * 10 ** 12, 2 * 10 ** 12]),
                         "1000000000000, 2000000000000, 3000000000000")

    def test_large_even_count_median(self):
        self.assertEqual(self.cell("input_tokens", [123456789012, 123456789013]),
                         "123456789012, 123456789012.5, 123456789013")
        self.assertEqual(self.cell("input_tokens", [1000000, 3000000]),
                         "1000000, 2000000, 3000000")

    # ---- some trials lack the measure
    def test_some_trials_lack_the_measure_adds_the_suffix(self):
        self.assertEqual(self.cell("seconds", [12.0, None, 18.0]),
                         "12.0, 15.0, 18.0 (2 of 3 trials)")

    def test_one_of_three_trials_carries_it(self):
        self.assertEqual(self.cell("seconds", [None, 5.0, None]),
                         "5.0, 5.0, 5.0 (1 of 3 trials)")

    def test_suffix_for_each_measure(self):
        expect = {"cost_usd": "0.0100, 0.0100, 0.0100 (1 of 2 trials)",
                  "input_tokens": "10, 10, 10 (1 of 2 trials)",
                  "output_tokens": "10, 10, 10 (1 of 2 trials)",
                  "attempts": "10, 10, 10 (1 of 2 trials)"}
        for f, want in expect.items():
            with self.subTest(f=f):
                v = 0.01 if f == "cost_usd" else 10
                self.assertEqual(self.cell(f, [None, v]), want)

    def test_suffix_counts_over_the_cases_own_trials_not_k(self):
        header, rows = self.rows([
            mcase("short", with_field("seconds", [4.0, None])),
            mcase("long", with_field("seconds", [1.0, 2.0, 3.0, 4.0, 5.0])),
        ])
        col = [h for h in header if h.startswith("Seconds")][0]
        self.assertEqual(rows[0][col], "4.0, 4.0, 4.0 (1 of 2 trials)")
        self.assertEqual(rows[1][col], "1.0, 3.0, 5.0")  # no suffix: every trial carries it

    def test_no_suffix_when_every_trial_carries_it(self):
        for f, v in (("seconds", 1.0), ("cost_usd", 0.5), ("input_tokens", 3),
                     ("output_tokens", 3), ("attempts", 3)):
            with self.subTest(f=f):
                self.assertNotIn("trials)", self.cell(f, [v, v, v]))

    def test_even_count_of_carrying_trials_with_a_missing_one(self):
        self.assertEqual(self.cell("attempts", [1, 2, None]), "1, 1.5, 2 (2 of 3 trials)")

    def test_failed_trials_count_like_passed_ones(self):
        header, rows = self.rows([mcase("a", [tr(True, seconds=10.0), tr(False, seconds=30.0),
                                              tr(False, seconds=20.0)])])
        col = [h for h in header if h.startswith("Seconds")][0]
        self.assertEqual(rows[0][col], "10.0, 20.0, 30.0")

    def test_a_missing_measure_is_independent_of_the_others(self):
        ts = [tr(seconds=10.0, cost_usd=0.01, attempts=1),
              tr(seconds=20.0, input_tokens=100, output_tokens=5, attempts=2),
              tr(seconds=30.0, cost_usd=0.03, input_tokens=300, attempts=3)]
        header, rows = self.rows([mcase("a", ts)])
        row = {k.split(" (")[0]: v for k, v in rows[0].items()}
        self.assertEqual(row["Seconds"], "10.0, 20.0, 30.0")
        self.assertEqual(row["Cost $"], "0.0100, 0.0200, 0.0300 (2 of 3 trials)")
        self.assertEqual(row["Tokens in"], "100, 200, 300 (2 of 3 trials)")
        self.assertEqual(row["Tokens out"], "5, 5, 5 (1 of 3 trials)")
        self.assertEqual(row["Attempts"], "1, 2, 3")

    # ---- no trial carries it
    def test_na_when_no_trial_carries_the_measure(self):
        for f in COLS:
            with self.subTest(f=f):
                self.assertEqual(self.cell(f, [None, None, None]), "n/a")
                self.assertEqual(self.cell(f, [None]), "n/a")

    def test_steps_measurement_without_attempts_shows_na_for_attempts(self):
        ts = [tr(seconds=12.0), tr(seconds=14.0), tr(seconds=16.0)]
        header, rows = self.rows([mcase("step", ts)])
        row = {k.split(" (")[0]: v for k, v in rows[0].items()}
        self.assertEqual(row["Attempts"], "n/a")
        self.assertEqual(row["Seconds"], "12.0, 14.0, 16.0")
        self.assertEqual(row["Cost $"], "n/a")
        self.assertEqual(row["Tokens in"], "n/a")
        self.assertEqual(row["Tokens out"], "n/a")

    def test_trials_with_only_passed_are_all_na(self):
        header, rows = self.rows([mcase("a", [tr(), tr(False), tr()])])
        for h in header[4:]:
            self.assertEqual(rows[0][h], "n/a", h)

    def test_case_with_no_trials_has_na_spread(self):
        # Assumption: no trial carries a measure, so n/a (not a suffix over 0 trials).
        header, rows = self.rows([mcase("a", [])])
        self.assertEqual(len(header), 9)
        for h in header[4:]:
            self.assertEqual(rows[0][h], "n/a", h)

    def test_zero_is_a_value_while_absent_is_not(self):
        self.assertEqual(self.cell("cost_usd", [0, None, 0]), "0.0000, 0.0000, 0.0000 (2 of 3 trials)")
        self.assertEqual(self.cell("input_tokens", [0, None]), "0, 0, 0 (1 of 2 trials)")

    # ---- several cases
    def test_each_case_has_its_own_spread(self):
        header, rows = self.rows([
            mcase("one", with_field("seconds", [1.0, 2.0, 3.0])),
            mcase("two", with_field("seconds", [10.0, 20.0, 60.0])),
            mcase("none", [tr(), tr(), tr()]),
        ])
        col = [h for h in header if h.startswith("Seconds")][0]
        self.assertEqual([r[col] for r in rows], ["1.0, 2.0, 3.0", "10.0, 20.0, 60.0", "n/a"])

    def test_quarantined_case_shows_its_spread(self):
        header, rows = self.rows([mcase("q", with_field("attempts", [1, 1, 2]), q="T185")])
        col = [h for h in header if h.startswith("Attempts")][0]
        self.assertEqual(rows[0][col], "1, 1, 2")

    def test_the_contract_example_row(self):
        ts = [tr(True, seconds=12.1, cost_usd=0.01, input_tokens=5100, output_tokens=800,
                 attempts=1),
              tr(True, seconds=14.0, cost_usd=0.012, input_tokens=5400, output_tokens=900,
                 attempts=1),
              tr(False, seconds=30.5, cost_usd=0.02, input_tokens=6000, output_tokens=950,
                 attempts=2)]
        header, rows = self.rows([mcase("example", ts)])
        row = {k.split(" (")[0]: v for k, v in rows[0].items()}
        self.assertEqual(row["Seconds"], "12.1, 14.0, 30.5")
        self.assertEqual(row["Cost $"], "0.0100, 0.0120, 0.0200")
        self.assertEqual(row["Tokens in"], "5100, 5400, 6000")
        self.assertEqual(row["Tokens out"], "800, 900, 950")
        self.assertEqual(row["Attempts"], "1, 1, 2")

    def test_the_contract_flaky_row(self):
        ts = [tr(seconds=10.0, attempts=1), tr(seconds=11.2, attempts=1), tr(attempts=1)]
        header, rows = self.rows([mcase("flaky", ts)])
        row = {k.split(" (")[0]: v for k, v in rows[0].items()}
        self.assertEqual(row["Seconds"], "10.0, 10.6, 11.2 (2 of 3 trials)")
        self.assertEqual(row["Cost $"], "n/a")
        self.assertEqual(row["Attempts"], "1, 1, 1")

if __name__ == "__main__":
    unittest.main()
