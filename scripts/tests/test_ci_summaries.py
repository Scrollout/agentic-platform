"""The redesigned CI summaries (T110(b), owner-approved 2026-11-16), written before the scripts.

scripts/ci-step.sh writes nothing to the job summary (T126, owner-approved 2026-11-17: the run
table is enough; the output stays in the job's log); scripts/ci-summary.py writes one run table.
Assumption (owner decision): a failed job's first error line arrives as the job output `error`.
"""
import json
import re
import unittest

import gitenv
from test_ci_scripts import (NOISE, STEP, SUMMARY, TABLE, base_env, run, tmpdir)

ERR_KEY = "error"


class CiStepSummaryTest(unittest.TestCase):
    def setUp(self):
        self.t = tmpdir(self)
        self.summary = self.t / "summary.md"
        stubs = gitenv.stub_dir(self, {"df": "echo 'Filesystem Size Used Avail Use% Mounted on'\necho '/dev/root 145G 58G 88G 40% /'\n"})
        self.env = gitenv.with_path(base_env(self, {"GITHUB_STEP_SUMMARY": str(self.summary)}), stubs)

    def step(self, script):
        r = run([STEP, "my-job", "sh", "-c", script], env=self.env, cwd=self.t)
        return r, self.summary.read_text() if self.summary.exists() else ""

    def test_writes_nothing_to_the_summary_on_success_or_failure(self):
        """T126: no job line, no `ap test` result lines, no last-40-lines section; the log has them."""
        results = ("echo 'PASS  alpha  ok'; echo 'FAIL  beta  bad'; echo 'planted: 1 of 2'; echo 'us2: 1 of 2'")
        cases = {"pass": ("echo ok", 0), "fail": ("exit 7", 7), "noise-fail": (NOISE + "; exit 1", 1),
                 "noise-pass": (NOISE, 0), "results-pass": (NOISE + "; " + results, 0),
                 "results-fail": (NOISE + "; " + results + "; exit 1", 1),
                 "partial-run-pass": ("echo 'partial run: only some tests ran'", 0),
                 "partial-run-fail": ("echo 'warning: not a full run'; exit 1", 1),
                 "time-table-pass": (NOISE + "; " + TABLE, 0), "time-table-fail": (NOISE + "; " + TABLE + "; exit 1", 1)}
        for label, (script, code) in cases.items():
            with self.subTest(label):
                self.summary.unlink(missing_ok=True)
                r, s = self.step(script)
                self.assertEqual(s, "")
                self.assertEqual(r.returncode, code)

    def test_the_outputs_keep_seconds_and_on_failure_error(self):
        out = self.t / "output.txt"
        self.env["GITHUB_OUTPUT"] = str(out)
        self.step("echo ok")
        self.assertRegex(out.read_text(), r"^seconds=\d+\n$")
        out.unlink()
        r, s = self.step("echo 'error: boom'; exit 7")
        self.assertRegex(out.read_text(), r"^seconds=\d+\nerror=error: boom\n$")
        self.assertEqual(s, "")
        self.assertIn("error: boom", r.stdout)


class CiRunTableTest(unittest.TestCase):
    def setUp(self):
        self.t = tmpdir(self)
        self.file = self.t / "summary.md"

    def go(self, needs):
        r = run(["python3", SUMMARY], env=base_env(self, {"NEEDS": json.dumps(needs),
                                                          "GITHUB_STEP_SUMMARY": str(self.file)}), cwd=self.t)
        return r, self.file.read_text()

    @staticmethod
    def need(result, seconds=None, error=None):
        o = {}
        if seconds is not None:
            o["seconds"] = seconds
        if error is not None:
            o[ERR_KEY] = error
        return {"result": result, "outputs": o}

    @staticmethod
    def rows(text):
        return [l for l in text.splitlines() if l.startswith("|") and not re.match(r"^\|[-| ]+\|$", l)][1:]

    def test_failed_jobs_come_first(self):
        _, out = self.go({"a-ok": self.need("success", "1"), "b-bad": self.need("failure", "2"),
                          "c-ok": self.need("success", "3"), "d-cancel": self.need("cancelled"),
                          "e-bad": self.need("failure", "4")})
        names = [re.search(r"[a-e]-\w+", r).group(0) for r in self.rows(out)]
        self.assertEqual(names, ["b-bad", "d-cancel", "e-bad", "a-ok", "c-ok"])

    def test_failed_row_holds_the_first_error_line(self):
        _, out = self.go({"bad-job": self.need("failure", "2", "boom: x\nsecond line"),
                          "ok-job": self.need("success", "1", "should-not-show")})
        bad = [r for r in self.rows(out) if "bad-job" in r]
        self.assertEqual(len(bad), 1)
        self.assertIn("boom: x", bad[0])
        self.assertNotIn("second line", out)
        self.assertNotIn("should-not-show", out)

    def test_job_that_failed_before_its_step_appears_with_its_result(self):
        r, out = self.go({"config": {"result": "failure", "outputs": {}}, "ok-job": self.need("success", "1")})
        self.assertEqual(r.returncode, 1, r.stderr)
        row = [x for x in self.rows(out) if "config" in x]
        self.assertEqual(len(row), 1, out)
        self.assertIn("❌", row[0])
        self.assertIn("—", row[0])

    def test_one_table_one_row_per_job(self):
        _, out = self.go({"alpha-job": self.need("success", "1"), "beta-job": self.need("failure", "2", "e")})
        self.assertEqual(len(self.rows(out)), 2)
        self.assertEqual(len([l for l in out.splitlines() if re.match(r"^\|[-| ]+\|$", l)]), 1)
        for n in ("alpha-job", "beta-job"):
            self.assertEqual(len([l for l in out.splitlines() if n in l]), 1)

    def test_every_row_is_pass_or_fail_marked(self):
        _, out = self.go({"a": self.need("success", "1"), "b": self.need("failure", "1"),
                          "c": self.need("cancelled"), "d": self.need("skipped")})
        for row in self.rows(out):
            self.assertRegex(row, r"✅|❌")
            self.assertNotRegex(row, r"⏹|⏭")
        self.assertEqual(len([r for r in self.rows(out) if "✅" in r]), 1)


if __name__ == "__main__":
    unittest.main()
