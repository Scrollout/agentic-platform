"""The redesigned CI summaries (T110(b), owner-approved 2026-11-16), written before the scripts.

scripts/ci-step.sh writes a one-line job summary; scripts/ci-summary.py writes one run table.
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
        return r, self.summary.read_text()

    def clear(self):
        self.summary.unlink()

    def test_pass_is_one_line(self):
        _, s = self.step("echo ok")
        self.assertRegex(s.splitlines()[0], r"^✅ my-job — \d+ s$")
        self.assertNotIn("<details>", s)
        self.assertNotIn("```text\nok", s)

    def test_fail_is_one_line_with_exit_code(self):
        _, s = self.step("exit 7")
        self.assertRegex(s.splitlines()[0], r"^❌ my-job — \d+ s, exit 7$")

    def test_last_40_lines_collapsed_only_on_failure(self):
        _, s = self.step(NOISE + "; exit 1")
        self.assertIn("<details>", s)
        self.assertRegex(s, r"noise-100\b")
        self.assertRegex(s, r"noise-61\b")
        self.assertNotRegex(s, r"noise-60\b")
        self.clear()
        _, s = self.step(NOISE)
        self.assertNotIn("<details>", s)
        self.assertNotRegex(s, r"noise-100\b")

    def test_ap_test_result_lines_always(self):
        body = ("i=1; while [ $i -le 80 ]; do echo pre-$i; i=$((i+1)); done; "
                "echo 'PASS  alpha  ok'; echo 'FAIL  beta  bad'; echo 'planted: 1 of 2'; "
                "i=1; while [ $i -le 80 ]; do echo post-$i; i=$((i+1)); done")
        for tail in ("", "; exit 1"):
            with self.subTest(tail):
                _, s = self.step(body + tail)
                for l in ("PASS  alpha", "FAIL  beta", "planted: 1 of 2"):
                    self.assertIn(l, s)
                if not tail:
                    self.assertNotIn("<details>", s)
                self.clear()

    def test_one_short_free_disk_line(self):
        for tail in ("", "; exit 1"):
            with self.subTest(tail):
                _, s = self.step("echo x" + tail)
                lines = [l for l in s.splitlines() if "Free disk" in l]
                self.assertEqual(lines, ["Free disk: 88G of 145G"], s)
                self.clear()

    def test_no_partial_run_warning(self):
        for tail in ("", "; exit 1"):
            with self.subTest(tail):
                _, s = self.step("echo 'partial run: only some tests ran'; echo 'warning: not a full run'" + tail)
                body = s.split("<details>")[0]
                self.assertNotRegex(body, r"(?i)partial|warning|not a full run")
                self.clear()

    def test_no_time_table(self):
        for tail in ("", "; exit 1"):
            with self.subTest(tail):
                _, s = self.step(NOISE + "; " + TABLE + tail)
                self.assertNotIn("== time per step", s.split("<details>")[0])
                self.assertNotIn("12.5  (1.0", s.split("<details>")[0])
                self.clear()


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
