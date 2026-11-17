"""T132: the evals.yml runs listing in evals.yml's config job and check.yml's evals-current job
has no status= filter (any value is rejected); a query without it passes."""
import unittest

from test_workflows_evals_current import (REPO, ec_sel, ev_sel, judge_selector_base, planted)


class RunsQueryTest(unittest.TestCase):
    def test_real_workflows_have_no_status_filter(self):
        self.assertEqual(judge_selector_base(REPO), [])

    def test_query_without_status_passes(self):
        self.assertEqual(judge_selector_base(planted(self)), [])

    def test_planted_status_filter_fails(self):
        for where in (ev_sel, ec_sel):
            for val in ("success", "completed"):
                with self.subTest(where.__name__, val=val):
                    def m(d, where=where, val=val):
                        st = where(d)
                        st["run"] = st["run"].replace("runs?per_page", f"runs?status={val}&per_page")
                    msgs = judge_selector_base(planted(self, m))
                    self.assertTrue(any("status=" in x for x in msgs), msgs)


if __name__ == "__main__":
    unittest.main()
