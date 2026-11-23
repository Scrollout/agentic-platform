"""The first table of scripts/ci-evals-trials.py (feature 008, T004), written before the code.

Judged from the contract in the script's docstring and specs/008-eval-variance/data-model.md.
Pinned: the `Case`, `Agent`, `Passed` and `pass^k` columns, the suite line, name escaping,
robustness, where the text goes, determinism, and the heading with no earlier run.
The table is parsed by header names. The spread columns (seconds, cost, tokens, attempts) are
checked only to exist as columns, never by value; the comparison table is not checked.

Assumptions where the contract is silent (each marked in a test):
- the suite line's p is the passed trials of every case and n is every trial, quarantined cases
  included; u counts the unstable cases that are not quarantined;
- "1 unstable case" is singular as the contract shows; for 0 and 2 either spelling is accepted;
- a line break or control character becomes one space, one for one;
- a measurement missing `measured_cases`, or one that is not an object, is "could not be read";
- "could not be read" may appear on stdout or in the summary file (all text it wrote is joined).
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
SPREAD = ["Seconds", "Cost", "Tokens in", "Tokens out", "Attempts"]
NOT_READ = "could not be read"
NO_EARLIER = "### No earlier run to compare with: no earlier run could be read"


def tr(passed=True, **extra):
    return {"passed": passed, **extra}


def trials(passed, failed=0):
    return [tr(True) for _ in range(passed)] + [tr(False) for _ in range(failed)]


def mcase(name, agent="analyst", ts=None, q=None):
    c = {"case": name, "agent": agent, "trials": ts if ts is not None else trials(3)}
    if q:
        c["quarantined_by"] = q
    return c


def meas(cases, **over):
    m = {"version": 1, "suite": "harness", "cases": "", "key": "harness", "commit": SHA,
         "branch": "main", "run_id": 7, "run_attempt": 1, "measured_cases": cases}
    m.update(over)
    return m


def cells(line):
    parts = re.split(r"(?<!\\)\|", line)
    return [p.strip() for p in parts[1:-1]]


class TrialsTable(unittest.TestCase):
    def setUp(self):
        t = tempfile.TemporaryDirectory()
        self.addCleanup(t.cleanup)
        self.dir = Path(t.name)

    def run_raw(self, text, env_extra=None, args=None, summary=None):
        p = self.dir / "measurement.json"
        p.write_text(text, encoding="utf-8")
        env = base_env(self, env_extra)
        if summary is not None:
            env["GITHUB_STEP_SUMMARY"] = str(summary)
        r = subprocess.run([sys.executable, str(TRIALS)] + (args if args is not None else [str(p)]),
                           capture_output=True, text=True, encoding="utf-8", env=env,
                           cwd=str(self.dir), timeout=60)
        return r

    def run_m(self, m, **kw):
        return self.run_raw(json.dumps(m), **kw)

    def run_cases(self, cases, **kw):
        r = self.run_m(meas(cases), **kw)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNotIn("Traceback", r.stderr)
        return r

    def table(self, r):
        ls = r.stdout.split("\n")
        idx = [i for i, ln in enumerate(ls) if ln.startswith("| Case |")]
        self.assertTrue(idx, "no table in: " + r.stdout)
        i = idx[0]
        header = cells(ls[i])
        rows = []
        for ln in ls[i + 2:]:
            if not ln.startswith("|"):
                break
            rows.append(dict(zip(header, cells(ln), strict=False)) | {"_n": len(cells(ln))})
        return ls, i, header, rows

    def one(self, c):
        _, _, _, rows = self.table(self.run_cases([c]))
        self.assertEqual(len(rows), 1)
        return rows[0]

    def suite_line(self, r):
        ls = [ln for ln in r.stdout.split("\n") if ln.startswith("## Trials:")]
        self.assertEqual(len(ls), 1, r.stdout)
        return ls[0]

    # ---- the section's shape
    def test_columns_in_order_with_spread_columns_present(self):
        _, i, header, _ = self.table(self.run_cases([mcase("a")]))
        self.assertEqual(header[:4], ["Case", "Agent", "Passed", "pass^3"])
        self.assertEqual(len(header), 9)
        for col, name in zip(header[4:], SPREAD, strict=True):
            self.assertTrue(col.startswith(name), header)

    def test_suite_line_precedes_table_and_separator_row_matches(self):
        r = self.run_cases([mcase("a")])
        ls, i, header, rows = self.table(r)
        self.assertTrue(ls[i - 2].startswith("## Trials:"), ls[:i])
        self.assertEqual(ls[i - 1], "")
        sep = cells(ls[i + 1])
        self.assertEqual(len(sep), len(header))
        self.assertTrue(all(re.fullmatch(r"-+", s) for s in sep), sep)
        self.assertEqual(rows[0]["_n"], len(header))

    def test_rows_follow_the_measurements_case_order(self):
        r = self.run_cases([mcase("zeta"), mcase("alpha"), mcase("mid")])
        _, _, _, rows = self.table(r)
        self.assertEqual([x["Case"] for x in rows], ["zeta", "alpha", "mid"])

    def test_agent_cell(self):
        row = self.one(mcase("a", agent="code-writer"))
        self.assertEqual((row["Case"], row["Agent"]), ("a", "code-writer"))

    # ---- Passed and pass^k
    def test_all_passed(self):
        row = self.one(mcase("a", ts=trials(3)))
        self.assertEqual(row["Passed"], "3/3")
        self.assertEqual(row["pass^3"], "✅ pass")

    def test_all_failed_is_not_unstable(self):
        row = self.one(mcase("a", ts=trials(0, 3)))
        self.assertEqual(row["Passed"], "0/3")
        self.assertEqual(row["pass^3"], "❌ fail")

    def test_mixed_is_unstable_and_fails(self):
        row = self.one(mcase("a", ts=trials(2, 1)))
        self.assertEqual(row["Passed"], "2/3 ⚠️ unstable")
        self.assertEqual(row["pass^3"], "❌ fail")

    def test_one_pass_among_failures_is_unstable(self):
        self.assertEqual(self.one(mcase("a", ts=trials(1, 4)))["Passed"], "1/5 ⚠️ unstable")

    def test_single_trial_cases_are_never_unstable(self):
        self.assertEqual(self.one(mcase("a", ts=trials(1)))["Passed"], "1/1")
        self.assertEqual(self.one(mcase("a", ts=trials(0, 1)))["Passed"], "0/1")

    def test_case_with_no_trials_is_0_of_0_fails_not_unstable(self):
        row = self.one(mcase("a", ts=[]))
        self.assertEqual(row["Passed"], "0/0")
        self.assertEqual(row["pass^3"], "❌ fail")

    def test_k_is_the_largest_trial_count(self):
        r = self.run_cases([mcase("one", ts=trials(1)), mcase("five", ts=trials(4, 1))])
        _, _, header, rows = self.table(r)
        self.assertEqual(header[3], "pass^5")
        # a case passes when it has a trial and every trial passed, whatever k is
        self.assertEqual(rows[0]["pass^5"], "✅ pass")
        self.assertEqual(rows[1]["pass^5"], "❌ fail")
        self.assertEqual(rows[0]["Passed"], "1/1")

    def test_k_of_one(self):
        _, _, header, _ = self.table(self.run_cases([mcase("a", ts=trials(1))]))
        self.assertEqual(header[3], "pass^1")

    def test_k_is_3_with_no_cases(self):
        r = self.run_cases([])
        self.assertIn("pass^3", r.stdout)
        self.assertRegex(self.suite_line(r), r"^## Trials: 0 of 0 passed, 0 unstable cases?$")

    def test_k_is_3_when_no_case_has_a_trial(self):
        r = self.run_cases([mcase("a", ts=[]), mcase("b", ts=[])])
        _, _, header, rows = self.table(r)
        self.assertEqual(header[3], "pass^3")
        self.assertEqual([x["Passed"] for x in rows], ["0/0", "0/0"])

    # ---- quarantine
    def test_quarantined_cell_ends_with_the_task(self):
        row = self.one(mcase("a", ts=trials(3), q="T185"))
        self.assertEqual(row["Passed"], "3/3 ⚠️ quarantined (T185)")
        self.assertEqual(row["pass^3"], "✅ pass")

    def test_quarantined_failed_case(self):
        row = self.one(mcase("a", ts=trials(0, 3), q="T9"))
        self.assertEqual(row["Passed"], "0/3 ⚠️ quarantined (T9)")
        self.assertEqual(row["pass^3"], "❌ fail")

    def test_unstable_then_quarantined_in_that_order(self):
        row = self.one(mcase("a", ts=trials(2, 1), q="T185"))
        self.assertEqual(row["Passed"], "2/3 ⚠️ unstable ⚠️ quarantined (T185)")

    def test_quarantine_fallback_task_is_shown(self):
        row = self.one(mcase("a", ts=trials(3), q="?"))
        self.assertTrue(row["Passed"].endswith("⚠️ quarantined (?)"), row["Passed"])

    def test_quarantined_case_with_no_trials(self):
        row = self.one(mcase("a", ts=[], q="T1"))
        self.assertEqual(row["Passed"], "0/0 ⚠️ quarantined (T1)")

    # ---- suite line
    def test_suite_line_singular_as_the_contract_shows(self):
        r = self.run_cases([mcase("a", ts=trials(2, 1)), mcase("b", ts=trials(3))])
        self.assertEqual(self.suite_line(r), "## Trials: 5 of 6 passed, 1 unstable case")

    def test_suite_line_counts_numbers_for_other_counts(self):
        r = self.run_cases([mcase("a", ts=trials(2, 1)), mcase("b", ts=trials(1, 2)),
                            mcase("c", ts=trials(3))])
        self.assertRegex(self.suite_line(r), r"^## Trials: 6 of 9 passed, 2 unstable cases?$")

    def test_suite_line_without_unstable_cases(self):
        r = self.run_cases([mcase("a", ts=trials(3)), mcase("b", ts=trials(0, 3))])
        self.assertRegex(self.suite_line(r), r"^## Trials: 3 of 6 passed, 0 unstable cases?$")

    def test_suite_line_counts_quarantined_trials_but_not_their_instability(self):
        r = self.run_cases([mcase("a", ts=trials(3)),
                            mcase("q", ts=trials(1, 2), q="T185")])
        self.assertRegex(self.suite_line(r), r"^## Trials: 4 of 6 passed, 0 unstable cases?$")

    def test_all_cases_quarantined(self):
        r = self.run_cases([mcase("a", ts=trials(2, 1), q="T1"), mcase("b", ts=trials(3), q="T2")])
        self.assertRegex(self.suite_line(r), r"^## Trials: 5 of 6 passed, 0 unstable cases?$")

    def test_unstable_and_quarantined_together_count_only_the_unquarantined(self):
        r = self.run_cases([mcase("a", ts=trials(2, 1)), mcase("b", ts=trials(2, 1), q="T1")])
        self.assertEqual(self.suite_line(r), "## Trials: 4 of 6 passed, 1 unstable case")

    def test_suite_line_with_different_trial_counts(self):
        r = self.run_cases([mcase("a", ts=trials(1)), mcase("b", ts=trials(4, 1))])
        self.assertEqual(self.suite_line(r), "## Trials: 5 of 6 passed, 1 unstable case")

    def test_suite_line_with_no_trials(self):
        r = self.run_cases([mcase("a", ts=[])])
        self.assertRegex(self.suite_line(r), r"^## Trials: 0 of 0 passed, 0 unstable cases?$")

    # ---- names from data
    def test_pipe_in_a_name_is_escaped_and_keeps_the_columns(self):
        _, _, header, rows = self.table(self.run_cases([mcase("a|b", agent="x|y")]))
        self.assertEqual(rows[0]["Case"], r"a\|b")
        self.assertEqual(rows[0]["Agent"], r"x\|y")
        self.assertEqual(rows[0]["_n"], len(header))

    def test_line_break_and_pipe_in_one_name(self):
        r = self.run_cases([mcase("a|b\nc", agent="p\r\nq")])
        _, _, header, rows = self.table(r)
        self.assertEqual(rows[0]["Case"], r"a\|b c")
        self.assertEqual(rows[0]["_n"], len(header))
        self.assertEqual(rows[0]["Agent"], "p  q")  # CR and LF each become one space

    def test_control_characters_become_a_space(self):
        r = self.run_cases([mcase("a\x07b\tc\x1bd\x00e")])
        self.assertEqual(self.table(r)[3][0]["Case"], "a b c d e")
        for ch in "\x07\x1b\x00\t":
            self.assertNotIn(ch, r.stdout)

    def test_unicode_name_is_kept(self):
        r = self.run_cases([mcase("écran-日本-🙂", agent="ünï")])
        row = self.table(r)[3][0]
        self.assertEqual((row["Case"], row["Agent"]), ("écran-日本-🙂", "ünï"))

    def test_markdown_in_a_name_does_not_add_table_rows(self):
        r = self.run_cases([mcase("x\n| fake | row |\n## Trials: 99 of 99 passed")])
        _, _, _, rows = self.table(r)
        self.assertEqual(len(rows), 1)
        self.assertEqual(len([ln for ln in r.stdout.split("\n") if ln.startswith("## Trials:")]), 1)

    # ---- no earlier run
    def test_no_earlier_heading_when_earlier_is_unset(self):
        r = self.run_cases([mcase("a")])
        self.assertIn(NO_EARLIER, r.stdout.split("\n"))

    def test_no_earlier_heading_when_earlier_is_empty_or_not_json_or_lacks_the_key(self):
        for earlier in ("", "not json", "[]", "null", json.dumps({"other-key": {}})):
            with self.subTest(earlier=earlier):
                r = self.run_cases([mcase("a")], env_extra={"EARLIER": earlier})
                self.assertIn(NO_EARLIER, r.stdout.split("\n"))

    def test_heading_comes_after_the_table(self):
        r = self.run_cases([mcase("a")])
        ls = r.stdout.split("\n")
        self.assertGreater(ls.index(NO_EARLIER), max(i for i, ln in enumerate(ls) if ln.startswith("| ")))

    # ---- unreadable measurements
    def assert_not_read(self, r):
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNotIn("Traceback", r.stderr)
        self.assertIn(NOT_READ, r.stdout)

    def test_wrongly_typed_fields_are_not_read(self):
        bad = {
            "trials a string": mcase("a", ts="abc"),
            "trials a number": mcase("a", ts=5),
            "passed a string": mcase("a", ts=[tr("yes")]),
            "passed a number": mcase("a", ts=[tr(1)]),
            "passed null": mcase("a", ts=[tr(None)]),
            "case a number": {"case": 5, "agent": "x", "trials": []},
            "agent a list": {"case": "a", "agent": ["x"], "trials": []},
        }
        for label, c in bad.items():
            with self.subTest(label):
                self.assert_not_read(self.run_m(meas([c])))

    def test_nan_seconds_are_not_read(self):
        raw = ('{"version":1,"suite":"harness","cases":"","key":"harness","commit":"%s",'
               '"branch":"main","run_id":7,"run_attempt":1,"measured_cases":[{"case":"a",'
               '"agent":"x","trials":[{"passed":true,"seconds":NaN}]}]}' % SHA)
        self.assert_not_read(self.run_raw(raw))

    def test_negative_or_non_numeric_seconds_are_not_read(self):
        for v in (-1, "12s", [1]):
            with self.subTest(seconds=v):
                self.assert_not_read(self.run_m(meas([mcase("a", ts=[tr(True, seconds=v)])])))

    def test_measured_cases_wrong_or_missing(self):
        m = meas([])
        del m["measured_cases"]
        self.assert_not_read(self.run_m(m))
        for v in ("x", None, {}, 3):
            with self.subTest(measured_cases=v):
                self.assert_not_read(self.run_m(meas(v)))

    def test_measurement_that_is_not_an_object(self):
        for text in ("[]", "null", "3", '"x"', "[1, 2]", "true"):
            with self.subTest(text=text):
                self.assert_not_read(self.run_raw(text))

    def test_invalid_empty_or_truncated_json(self):
        for text in ("", "{", "not json", '{"version": 1,'):
            with self.subTest(text=text):
                self.assert_not_read(self.run_raw(text))

    def test_missing_measurement_file(self):
        r = subprocess.run([sys.executable, str(TRIALS), str(self.dir / "absent.json")],
                           capture_output=True, text=True, env=base_env(self), timeout=60)
        self.assert_not_read(r)

    def test_measurement_path_that_is_a_directory(self):
        r = subprocess.run([sys.executable, str(TRIALS), str(self.dir)],
                           capture_output=True, text=True, env=base_env(self), timeout=60)
        self.assert_not_read(r)

    def test_binary_file(self):
        p = self.dir / "m.json"
        p.write_bytes(b"\xff\xfe\x00\x80garbage")
        r = subprocess.run([sys.executable, str(TRIALS), str(p)], capture_output=True,
                           text=True, env=base_env(self), timeout=60)
        self.assert_not_read(r)

    def test_unreadable_measurement_appends_the_message_to_the_summary_file(self):
        s = self.dir / "summary.md"
        r = self.run_raw("[]", summary=s)
        self.assertEqual(r.returncode, 0)
        self.assertIn(NOT_READ, r.stdout + s.read_text(encoding="utf-8"))

    def test_measure_fallbacks_are_read(self):
        m = meas([mcase("?", agent="?")], suite="?", key="?", commit="", branch="", run_id=0,
                 run_attempt=0)
        r = self.run_m(m)
        self.assertEqual(r.returncode, 0)
        self.assertNotIn(NOT_READ, r.stdout)
        self.assertEqual(self.table(r)[3][0]["Case"], "?")

    # ---- where the text goes
    def test_printed_when_summary_unset(self):
        r = self.run_cases([mcase("a")])
        self.assertIn("## Trials:", r.stdout)

    def test_appended_to_the_summary_file_and_not_printed(self):
        s = self.dir / "summary.md"
        r = self.run_cases([mcase("a")], summary=s)
        text = s.read_text(encoding="utf-8")
        self.assertIn("## Trials:", text)
        self.assertIn(NO_EARLIER, text)
        self.assertNotIn("## Trials:", r.stdout)

    def test_existing_summary_text_is_kept_and_new_text_follows(self):
        s = self.dir / "summary.md"
        s.write_text("## Earlier section\n\nkept text\n", encoding="utf-8")
        self.run_cases([mcase("a")], summary=s)
        text = s.read_text(encoding="utf-8")
        self.assertTrue(text.startswith("## Earlier section\n\nkept text\n"), text)
        self.assertGreater(text.index("## Trials:"), text.index("kept text"))

    def test_two_runs_append_two_sections(self):
        s = self.dir / "summary.md"
        self.run_cases([mcase("a")], summary=s)
        self.run_cases([mcase("a")], summary=s)
        self.assertEqual(s.read_text(encoding="utf-8").count("## Trials:"), 2)

    def test_summary_file_that_does_not_exist_yet_is_created_or_printed(self):
        s = self.dir / "new-summary.md"
        r = self.run_cases([mcase("a")], summary=s)
        got = (s.read_text(encoding="utf-8") if s.exists() else "") + r.stdout
        self.assertIn("## Trials:", got)

    def test_unwritable_summary_prints_instead_and_exits_0(self):
        r = self.run_cases([mcase("a", ts=trials(2, 1))], summary=self.dir)  # a directory
        self.assertIn("## Trials: 2 of 3 passed, 1 unstable case", r.stdout)
        self.assertIn(NO_EARLIER, r.stdout)

    def test_summary_in_a_missing_folder_prints_instead(self):
        r = self.run_cases([mcase("a")], summary=self.dir / "nope" / "s.md")
        self.assertIn("## Trials:", r.stdout)

    def test_same_input_twice_gives_the_same_text(self):
        cases = [mcase("a|b", ts=trials(2, 1), q="T3"), mcase("é", ts=[]), mcase("c")]
        a = self.run_cases(cases)
        b = self.run_cases(cases)
        self.assertEqual(a.stdout, b.stdout)
        s1, s2 = self.dir / "s1.md", self.dir / "s2.md"
        self.run_cases(cases, summary=s1)
        self.run_cases(cases, summary=s2)
        self.assertEqual(s1.read_text(encoding="utf-8"), s2.read_text(encoding="utf-8"))
        self.assertEqual(s1.read_text(encoding="utf-8"), a.stdout)

    # ---- arguments
    def test_wrong_argument_count_exits_2_and_writes_nothing(self):
        p = self.dir / "m.json"
        p.write_text(json.dumps(meas([mcase("a")])), encoding="utf-8")
        s = self.dir / "summary.md"
        for args in ([], [str(p), str(p)], [str(p), "x", "y"]):
            with self.subTest(args=len(args)):
                r = self.run_raw(json.dumps(meas([])), args=args, summary=s)
                self.assertEqual(r.returncode, 2, r.stderr)
                self.assertEqual(r.stdout, "")
                self.assertFalse(s.exists())
                self.assertNotIn("Traceback", r.stderr)

    # ---- no process
    def test_path_with_only_python_still_works(self):
        b = self.dir / "bin"
        b.mkdir()
        py = b / "python3"
        py.symlink_to(sys.executable)
        p = self.dir / "m.json"
        p.write_text(json.dumps(meas([mcase("a", ts=trials(2, 1))])), encoding="utf-8")
        env = base_env(self)
        env["PATH"] = str(b)
        r = subprocess.run([str(py), str(TRIALS), str(p)], capture_output=True, text=True,
                           encoding="utf-8", env=env, timeout=60)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("## Trials: 2 of 3 passed, 1 unstable case", r.stdout)

    def test_starts_no_gh_or_git(self):
        b = self.dir / "bin"
        b.mkdir()
        (b / "python3").symlink_to(sys.executable)
        marker = self.dir / "started"
        for tool in ("gh", "git"):
            f = b / tool
            f.write_text(f"#!/bin/sh\necho {tool} >> '{marker}'\nexit 1\n")
            f.chmod(0o755)
        p = self.dir / "m.json"
        p.write_text(json.dumps(meas([mcase("a")])), encoding="utf-8")
        env = base_env(self, {"EARLIER": json.dumps({"harness": {"run_id": 1}})})
        env["PATH"] = str(b)
        r = subprocess.run([str(b / "python3"), str(TRIALS), str(p)], capture_output=True,
                           text=True, encoding="utf-8", env=env, timeout=60)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("## Trials:", r.stdout)
        self.assertFalse(marker.exists())


if __name__ == "__main__":
    unittest.main()
