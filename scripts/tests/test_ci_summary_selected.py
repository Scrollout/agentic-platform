"""The run summary's rule for jobs the selector skipped (T117; research D50, ADR 0053), written
before the change to scripts/ci-summary.py.

NEEDS (the summary job's `needs` context) holds `config`, whose output `jobs` is the JSON list of
job ids scripts/ci-check-select.py chose (a string, as every job output is). A job whose result is
`skipped` and whose id is not in that list counts as passed: the run passes when every other job
succeeded, and the job keeps one row that says it was skipped, with no ❌. A skipped job whose id
is in the list, and every skipped job when config's `jobs` output is missing or not a JSON list of
strings, fails as before. Failure and cancelled fail whatever the list says.
"""
import json
import re
import unittest

from test_ci_scripts import SUMMARY, base_env, run, tmpdir


def need(result, seconds=None, **outputs):
    o = dict(outputs)
    if seconds is not None:
        o["seconds"] = seconds
    return {"result": result, "outputs": o}


def config(jobs):
    """The config job's needs entry; jobs is a list (dumped), a raw string, or None (no output)."""
    o = {"runner": "ubuntu"}
    if jobs is not None:
        o["jobs"] = jobs if isinstance(jobs, str) else json.dumps(jobs)
    return {"result": "success", "outputs": o}


class SelectedSummaryTest(unittest.TestCase):
    def setUp(self):
        self.t = tmpdir(self)
        self.file = self.t / "summary.md"

    def go(self, needs):
        self.file.unlink(missing_ok=True)
        r = run(["python3", SUMMARY], env=base_env(self, {"NEEDS": json.dumps(needs),
                                                          "GITHUB_STEP_SUMMARY": str(self.file)}),
                cwd=self.t)
        return r, self.file.read_text() if self.file.exists() else ""

    def row(self, text, name):
        rows = [l for l in text.splitlines() if l.startswith("|") and re.search(r"\b%s\b" % re.escape(name), l)]
        self.assertEqual(len(rows), 1, f"{name}: {rows}\n{text}")
        return rows[0]

    def test_selector_skipped_job_passes_and_is_listed_as_skipped(self):
        r, out = self.go({"config": config(["layout", "knowledge"]),
                          "layout": need("success", "5"), "knowledge": need("success", "9"),
                          "docker": need("skipped"), "go": need("skipped")})
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr + out)
        self.assertIn("Checks passed", out)
        for j in ("docker", "go"):
            row = self.row(out, j)
            self.assertIn("skipped", row)
            self.assertNotIn("❌", row)
        self.assertIn("✅", self.row(out, "layout"))
        self.assertNotIn("skipped", self.row(out, "layout"))

    def test_planted_skipped_job_the_selector_chose_fails(self):
        r, out = self.go({"config": config(["layout", "docker"]),
                          "layout": need("success", "5"), "docker": need("skipped")})
        self.assertEqual(r.returncode, 1, out)
        self.assertIn("❌", self.row(out, "docker"))
        self.assertIn("Checks failed", out)

    def test_planted_failure_or_cancel_of_an_unselected_job_fails(self):
        for res in ("failure", "cancelled"):
            with self.subTest(res):
                r, out = self.go({"config": config(["layout"]), "layout": need("success", "1"),
                                  "docker": need(res, "3")})
                self.assertEqual(r.returncode, 1, out)
                self.assertIn("❌", self.row(out, "docker"))

    def test_planted_missing_or_garbled_jobs_output_fails_skipped_jobs(self):
        for jobs in (None, "", "not json", '{"docker": false}', '"layout"', "null",
                     "layout,knowledge"):
            with self.subTest(jobs):
                r, out = self.go({"config": config(jobs), "layout": need("success", "1"),
                                  "docker": need("skipped")})
                self.assertEqual(r.returncode, 1, out)
                self.assertIn("❌", self.row(out, "docker"))

    def test_planted_config_failed_fails_the_run(self):
        r, out = self.go({"config": {"result": "failure", "outputs": {}},
                          "docker": need("skipped")})
        self.assertEqual(r.returncode, 1, out)
        self.assertIn("❌", self.row(out, "config"))
        self.assertIn("❌", self.row(out, "docker"))

    def test_one_row_per_job_with_skipped_rows_after_failed_ones(self):
        r, out = self.go({"config": config(["layout", "go"]), "docker": need("skipped"),
                          "layout": need("success", "1"), "go": need("failure", "2", error="boom")})
        self.assertEqual(r.returncode, 1, out)
        rows = [l for l in out.splitlines() if l.startswith("|") and not re.match(r"^\|[-| ]+\|$", l)][1:]
        self.assertEqual(len(rows), 4, rows)
        self.assertIn("go", rows[0])
        self.assertIn("boom", rows[0])
        self.assertIn("Checks failed: 1 of", out)


if __name__ == "__main__":
    unittest.main()
