"""scripts/ci-evals-failed-trials.py <dir> (feature 005 T002; research R2; ADR 0078), written
before the script.

It says whether any eval trial failed, so the job keeps its outputs: it reads every `report.json`
under <dir> (top level and `revision/`, any depth) and writes one line, `failed=true` or
`failed=false`, appended to the file named by $GITHUB_OUTPUT, or to standard output when that
variable is unset or empty. `failed=true` when any `trials[].passed` is false (a quarantined
case's included), or a report cannot be read or has an unexpected shape (not a list of cases; a
case that is not an object or has no non-empty `trials` list; a trial that is not an object or
has no boolean `passed`): fail closed. `failed=false` when every trial passed or no report
exists. It prints each report it read and each failed trial as `<case> trial <n>`. Exit 0 in
all these cases; exit 2 on a wrong number of arguments, with nothing written to $GITHUB_OUTPUT.

Every fixture is built in a temporary folder; the case name is under "case" and the trial number
under "trial", as the harness writes them.
"""
import json
import subprocess
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
SCRIPT = HERE.parent / "ci-evals-failed-trials.py"


def trial(n, passed=True):
    return {"trial": n, "passed": passed}


def case(name, *passed, q=None):
    c = {"case": name, "agent": "analyst", "passed": all(passed),
         "trials": [trial(i + 1, p) for i, p in enumerate(passed)]}
    if q:
        c["quarantined_by"] = q
    return c


class FailedTrialsTest(unittest.TestCase):
    def setUp(self):
        t = tempfile.TemporaryDirectory()
        self.addCleanup(t.cleanup)
        self.tmp = Path(t.name)
        self.dir = self.tmp / "eval-report"
        self.dir.mkdir()

    def put(self, rel, data, raw=False):
        p = self.dir / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        if isinstance(data, bytes):
            p.write_bytes(data)
        else:
            p.write_text(data if raw else json.dumps(data), encoding="utf-8")
        return p

    def run_script(self, *args, out_file="default", env_out=True, preset=None):
        """Run with $GITHUB_OUTPUT at self.tmp/out (the default), unset (env_out False)."""
        self.assertTrue(SCRIPT.exists(), f"{SCRIPT} does not exist")
        out = self.tmp / "github_output"
        if preset is not None:
            out.write_text(preset)
        env = {"PATH": "/usr/local/bin:/usr/bin:/bin", "HOME": str(self.tmp)}
        if env_out:
            env["GITHUB_OUTPUT"] = str(out)
        if out_file == "empty":
            env["GITHUB_OUTPUT"] = ""
        r = subprocess.run(["python3", str(SCRIPT), *map(str, args)], capture_output=True,
                           text=True, env=env, cwd=str(self.tmp), timeout=60)
        written = out.read_text() if out.exists() else None
        return r, written

    def verdict(self, expected, *, reports=None, **kw):
        """Run on self.dir; the line goes to $GITHUB_OUTPUT, not to stdout; exit 0."""
        r, written = self.run_script(self.dir, **kw)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(written, f"failed={expected}\n", r.stdout + r.stderr)
        self.assertNotIn(f"failed={expected}", r.stdout.splitlines())
        return r

    # ------------------------------------------------------------ all passed

    def test_all_trials_passed_is_false_and_names_the_report(self):
        p = self.put("report.json", [case("a", True, True, True), case("b", True, True, True)])
        r = self.verdict("false")
        self.assertIn(str(p), r.stdout)
        self.assertNotIn(" trial ", r.stdout)

    def test_no_report_is_false(self):
        self.put("analyst/a.trial1.out", "answer")
        self.put("other.json", "not a report, not json")
        self.put("report.json.bak", "{")
        self.verdict("false")

    def test_empty_folder_is_false(self):
        self.verdict("false")

    def test_missing_folder_is_false(self):
        r, written = self.run_script(self.tmp / "nowhere")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(written, "failed=false\n")

    def test_empty_case_list_is_false(self):
        self.put("report.json", [])
        self.verdict("false")

    def test_one_case_one_trial_passed_is_false(self):
        self.put("report.json", [case("only", True)])
        self.verdict("false")

    # ------------------------------------------------------------ a failed trial

    def test_one_failed_trial_is_true_and_named(self):
        p = self.put("report.json", [case("a", True, True, True), case("example", True, False, True)])
        r = self.verdict("true")
        self.assertIn("example trial 2", r.stdout)
        self.assertNotIn("a trial", r.stdout.replace("example trial 2", ""))
        self.assertIn(str(p), r.stdout)

    def test_every_failed_trial_is_named(self):
        self.put("report.json", [case("x", False, True, False), case("y", True, True, False)])
        r = self.verdict("true")
        for line in ("x trial 1", "x trial 3", "y trial 3"):
            self.assertIn(line, r.stdout)
        self.assertNotIn("x trial 2", r.stdout)
        self.assertNotIn("y trial 1", r.stdout)

    def test_quarantined_failure_counts(self):
        self.put("report.json", [case("q", True, False, True, q="T185"), case("a", True, True, True)])
        r = self.verdict("true")
        self.assertIn("q trial 2", r.stdout)

    def test_quarantined_case_that_passed_is_false(self):
        self.put("report.json", [case("q", True, True, True, q="T185")])
        self.verdict("false")

    def test_revision_report_alone_failing_is_true(self):
        self.put("report.json", [case("a", True, True, True)])
        p = self.put("revision/report.json", [case("rev", True, True, False)])
        r = self.verdict("true")
        self.assertIn("rev trial 3", r.stdout)
        self.assertIn(str(p), r.stdout)

    def test_top_level_failing_with_revision_passing_is_true(self):
        self.put("report.json", [case("a", False, True, True)])
        self.put("revision/report.json", [case("rev", True, True, True)])
        self.verdict("true")

    def test_both_reports_read_and_named_when_all_pass(self):
        a = self.put("report.json", [case("a", True)])
        b = self.put("revision/report.json", [case("rev", True)])
        r = self.verdict("false")
        self.assertIn(str(a), r.stdout)
        self.assertIn(str(b), r.stdout)

    def test_deeply_nested_report_is_found(self):
        self.put("x/y/z/report.json", [case("deep", True, False)])
        r = self.verdict("true")
        self.assertIn("deep trial 2", r.stdout)

    def test_a_failure_after_many_passing_cases_is_found(self):
        cases = [case(f"c{i}", True, True, True) for i in range(40)] + [case("last", True, True, False)]
        self.put("report.json", cases)
        self.verdict("true")

    # ------------------------------------------------------------ fail closed

    def test_unreadable_or_odd_json_is_true(self):
        bad = {
            "invalid json": "{not json",
            "empty file": "",
            "truncated": '[{"case": "a", "trials": [{"trial": 1, "passed": tr',
            "null": "null",
            "object top level": '{"cases": []}',
            "string top level": '"report"',
            "number top level": "3",
            "true top level": "true",
        }
        for name, text in bad.items():
            with self.subTest(name):
                p = self.put("report.json", text, raw=True)
                self.verdict("true", preset=None)
                p.unlink()
                (self.tmp / "github_output").unlink(missing_ok=True)

    def test_not_utf8_is_true(self):
        self.put("report.json", b"\xff\xfe\x00[")
        self.verdict("true")

    def test_report_json_that_is_a_folder_is_true(self):
        (self.dir / "report.json").mkdir()
        self.verdict("true")

    def test_unexpected_shapes_are_true(self):
        good_trial = trial(1, True)
        shapes = {
            "case is a number": [3],
            "case is null": [None],
            "case is a string": ["a"],
            "case without trials": [{"case": "a", "passed": True}],
            "trials null": [{"case": "a", "trials": None}],
            "trials empty (zero trials)": [{"case": "a", "passed": True, "trials": []}],
            "trials is an object": [{"case": "a", "trials": {"trial": 1, "passed": True}}],
            "trials is a string": [{"case": "a", "trials": "ppp"}],
            "trial is a number": [{"case": "a", "trials": [1]}],
            "trial is null": [{"case": "a", "trials": [None]}],
            "trial without passed": [{"case": "a", "trials": [{"trial": 1}]}],
            "passed null": [{"case": "a", "trials": [{"trial": 1, "passed": None}]}],
            "passed string true": [{"case": "a", "trials": [{"trial": 1, "passed": "true"}]}],
            "passed string false": [{"case": "a", "trials": [{"trial": 1, "passed": "false"}]}],
            "passed 1": [{"case": "a", "trials": [{"trial": 1, "passed": 1}]}],
            "passed 0": [{"case": "a", "trials": [{"trial": 1, "passed": 0}]}],
            "passed list": [{"case": "a", "trials": [{"trial": 1, "passed": [True]}]}],
            "second trial malformed": [{"case": "a", "trials": [good_trial, {"trial": 2}]}],
            "second case malformed": [case("ok", True), {"case": "b", "trials": []}],
            "only the case-level passed": [{"case": "a", "passed": True, "trials": [{"trial": 1}]}],
        }
        for name, data in shapes.items():
            with self.subTest(name):
                p = self.put("report.json", data)
                (self.tmp / "github_output").unlink(missing_ok=True)
                r, written = self.run_script(self.dir)
                self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
                self.assertEqual(written, "failed=true\n", name)
                p.unlink()

    def test_unreadable_revision_report_is_true_though_top_level_passes(self):
        self.put("report.json", [case("a", True, True, True)])
        self.put("revision/report.json", "{broken", raw=True)
        self.verdict("true")

    def test_bad_report_among_good_ones_is_true(self):
        self.put("report.json", [case("a", True)])
        self.put("revision/report.json", [case("b", True)])
        self.put("extra/report.json", [{"case": "c"}])
        self.verdict("true")

    def test_failed_trial_in_a_report_with_an_unreadable_sibling_is_true(self):
        self.put("report.json", [case("x", False)])
        self.put("revision/report.json", "{", raw=True)
        r = self.verdict("true")
        self.assertIn("x trial 1", r.stdout)

    # ------------------------------------------------------------ where the line goes

    def test_without_github_output_the_line_is_on_stdout(self):
        self.put("report.json", [case("a", True, False)])
        r, written = self.run_script(self.dir, env_out=False)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIsNone(written)
        self.assertIn("failed=true", r.stdout.splitlines())
        self.assertIn("a trial 2", r.stdout)

    def test_without_github_output_all_passed_prints_false(self):
        self.put("report.json", [case("a", True)])
        r, _ = self.run_script(self.dir, env_out=False)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("failed=false", r.stdout.splitlines())

    def test_empty_github_output_counts_as_unset(self):
        self.put("report.json", [case("a", True)])
        r, written = self.run_script(self.dir, out_file="empty")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("failed=false", r.stdout.splitlines())

    def test_github_output_is_appended_not_overwritten(self):
        self.put("report.json", [case("a", True, False)])
        r, written = self.run_script(self.dir, preset="runner=x\nmatrix=[]\n")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(written, "runner=x\nmatrix=[]\nfailed=true\n")

    def test_github_output_appended_when_the_file_does_not_exist(self):
        self.put("report.json", [case("a", True)])
        r, written = self.run_script(self.dir)
        self.assertEqual(written, "failed=false\n")

    def test_line_written_exactly_once_and_not_to_stdout(self):
        self.put("report.json", [case("a", False), case("b", False)])
        r, written = self.run_script(self.dir)
        self.assertEqual(written.count("failed="), 1)
        self.assertNotIn("failed=", r.stdout)

    # ------------------------------------------------------------ arguments

    def test_wrong_argument_count_exits_2_and_writes_nothing(self):
        self.put("report.json", [case("a", True)])
        for args in ((), (self.dir, self.dir), (self.dir, "extra", "more")):
            with self.subTest(len(args)):
                (self.tmp / "github_output").unlink(missing_ok=True)
                r, written = self.run_script(*args)
                self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
                self.assertIsNone(written)
                self.assertNotIn("failed=", r.stdout)
                self.assertTrue(r.stderr.strip(), "usage goes to standard error")

    def test_the_folder_is_not_changed(self):
        p = self.put("report.json", [case("a", True, False)])
        before = p.read_bytes()
        self.run_script(self.dir)
        self.assertEqual(p.read_bytes(), before)
        self.assertEqual(sorted(x.name for x in self.dir.iterdir()), ["report.json"])


if __name__ == "__main__":
    unittest.main()
