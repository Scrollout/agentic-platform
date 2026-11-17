"""T130 (D45, ADR 0048): in MODE=pending the selector writes nothing to $GITHUB_STEP_SUMMARY when
no eval suite is pending; a pending suite is still named there as before; without MODE=pending
the summary is never written. Helpers are test_ci_evals_dispatch.py's, imported unchanged."""
import json
import os
import subprocess
import sys
import tempfile
import unittest

from test_ci_evals_dispatch import FEATURE, NAMES, REV, REVIEW, run_of, select
from test_ci_evals_select import ALL, SELECT, Repo


class PendingSummaryTest(unittest.TestCase):
    def setUp(self):
        self.r = Repo(self)
        self.runs = [run_of(self.r.base, branch="main")]
        self.docs = self.r.commit({"docs/index.md": "x\n"})
        self.rev = self.r.commit({REVIEW: "x\n"})

    def pending(self, sha, ref, runs=None):
        return select(self, self.r, sha, self.runs if runs is None else runs,
                      event="push", mode="pending", ref=ref)

    def test_none_pending_on_feature_branch_writes_no_summary(self):
        res = self.pending(self.docs, FEATURE)
        self.assertEqual((res.code, res.pairs), (0, []), res.stderr)
        self.assertEqual(res.summary, "")

    def test_none_pending_on_main_writes_no_summary_and_exits_0(self):
        res = self.pending(self.docs, "main")
        self.assertEqual((res.code, res.pairs), (0, []), res.stderr)
        self.assertEqual(res.summary, "")

    def test_existing_summary_content_is_left_byte_identical(self):
        before = "## earlier step\n\nsome é content, no trailing newline"
        for ref in (FEATURE, "main"):
            with self.subTest(ref=ref):
                tmp = tempfile.TemporaryDirectory()
                self.addCleanup(tmp.cleanup)
                summ = os.path.join(tmp.name, "summary.md")
                with open(summ, "wb") as f:
                    f.write(before.encode())
                env = {k: v for k, v in self.r.env.items()
                       if k not in ("BEFORE", "ALL", "MODE", "REF", "RUNS")}
                env.update(SHA=self.docs, EVENT="push", RUNS=json.dumps(self.runs),
                           MODE="pending", REF=ref, GITHUB_STEP_SUMMARY=summ)
                r = subprocess.run([sys.executable, str(SELECT)], cwd=self.r.root, env=env,
                                   capture_output=True, text=True, timeout=60)
                self.assertEqual(r.returncode, 0, r.stderr)
                self.assertEqual(json.loads(r.stdout), [])
                self.assertEqual(open(summ, "rb").read(), before.encode())

    def test_planted_pending_suite_still_appends_a_summary_naming_it(self):
        res = self.pending(self.rev, FEATURE)
        self.assertEqual((res.code, res.pairs), (0, REV), res.stderr)
        self.assertIn(NAMES[REV[0]], res.summary)

    def test_no_base_run_still_writes_the_summary(self):
        res = self.pending(self.docs, FEATURE, runs=[])
        self.assertEqual((res.code, res.pairs), (0, ALL), res.stderr)
        for p in ALL:
            self.assertIn(NAMES[p], res.summary)

    def test_without_pending_mode_the_summary_is_never_written(self):
        for sha in (self.docs, self.rev):
            with self.subTest(sha=sha):
                res = select(self, self.r, sha, self.runs, event="push")
                self.assertEqual(res.code, 0, res.stderr)
                self.assertEqual(res.summary, "")


if __name__ == "__main__":
    unittest.main()
