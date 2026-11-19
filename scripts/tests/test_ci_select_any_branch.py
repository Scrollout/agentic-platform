"""A green run on any branch, such as a later feature branch, is the base of both CI selectors
(T056; ADR 0053): scripts/ci-check-select.py (check jobs) and scripts/ci-evals-select.py (eval
suites) choose only what the files changed since it need. A green run whose head is not an
ancestor, a failed or cancelled run, and a run whose head is not a commit here still do not
count; with none qualifying, every job and every suite runs. The throwaway repositories and
helpers are those of test_ci_check_select.py and test_ci_evals_dispatch.py, imported unchanged.
"""
import unittest

import test_ci_check_select as chk
import test_ci_evals_dispatch as evl

BRANCH = "002-durable-execution"


class CheckJobsTest(unittest.TestCase):
    def setUp(self):
        self.r = chk.Repo(self)
        self.code = self.r.commit({"src/main.go": "package main // 1\n"})
        self.docs = self.r.commit({"docs/index.md": "x\n"})
        self.docs_only = chk.ordered(chk.ALWAYS + ["markdown", "knowledge", "py_tests"])

    def test_feature_branch_run_is_the_base(self):
        self.assertEqual(self.r.run([chk.run_of(self.code, branch=BRANCH)], self.docs),
                         self.docs_only)

    def test_newest_qualifying_run_wins_over_an_older_one(self):
        runs = [chk.run_of(self.code, branch=BRANCH), chk.run_of(self.r.base, branch="main")]
        self.assertEqual(self.r.run(runs, self.docs), self.docs_only)

    def test_branch_missing_counts(self):
        self.assertEqual(self.r.run([chk.run_of(self.code, branch=None)], self.docs),
                         self.docs_only)

    def test_sha_itself_green_on_the_branch(self):
        self.assertEqual(self.r.run([chk.run_of(self.docs, branch=BRANCH)], self.docs),
                         chk.ALWAYS)

    def test_sibling_branch_run_not_an_ancestor_does_not_count(self):
        self.r.git("checkout", "-q", "-b", "side", self.r.base)
        side = self.r.commit({"src/main.go": "package main // side\n"})
        self.r.git("checkout", "-q", "main")
        self.assertEqual(self.r.run([chk.run_of(side, branch=BRANCH)], self.docs), chk.EVERY)
        runs = [chk.run_of(side, branch=BRANCH), chk.run_of(self.code, branch=BRANCH)]
        self.assertEqual(self.r.run(runs, self.docs), self.docs_only)

    def test_failed_or_cancelled_run_does_not_count(self):
        for c in ("failure", "cancelled", None):
            with self.subTest(c):
                self.assertEqual(
                    self.r.run([chk.run_of(self.code, branch=BRANCH, conclusion=c)], self.docs),
                    chk.EVERY)

    def test_run_whose_head_is_not_a_commit_here_does_not_count(self):
        self.assertEqual(self.r.run([chk.run_of("e" * 40, branch=BRANCH)], self.docs), chk.EVERY)

    def test_no_usable_runs_runs_every_job(self):
        for raw in ([], "", "gh: HTTP 502"):
            with self.subTest(raw):
                self.assertEqual(self.r.run(raw, self.docs), chk.EVERY)


class EvalSuitesTest(unittest.TestCase):
    def setUp(self):
        self.r = evl.Repo(self)
        evl.feature(self.r)
        self.review = self.r.commit({evl.REVIEW: "x\n"})
        self.docs = self.r.commit({"docs/index.md": "x\n"})

    def pick(self, runs, sha=None):
        return evl.pick(self, self.r, sha or self.docs, runs)

    def test_feature_branch_run_is_the_base(self):
        # Base before the review change: the review suite is chosen; base after it: none.
        self.assertEqual(self.pick([evl.run_of(self.r.base, branch=BRANCH)]), evl.REV)
        self.assertEqual(self.pick([evl.run_of(self.review, branch=BRANCH)]), [])

    def test_newest_qualifying_run_wins_over_an_older_one(self):
        runs = [evl.run_of(self.review, branch=BRANCH), evl.run_of(self.r.base, branch="main")]
        self.assertEqual(self.pick(runs), [])

    def test_branch_missing_counts(self):
        self.assertEqual(self.pick([evl.run_of(self.review, branch=None)]), [])

    def test_sha_itself_green_on_the_branch(self):
        self.assertEqual(self.pick([evl.run_of(self.docs, branch=BRANCH)]), [])

    def test_sibling_branch_run_not_an_ancestor_does_not_count(self):
        self.r.git("checkout", "-q", "-b", "side", self.r.base)
        side = self.r.commit({evl.ANALYST: "side\n"})
        self.r.git("checkout", "-q", evl.FEATURE)
        self.assertEqual(self.pick([evl.run_of(side, branch=BRANCH)]), evl.ALL)
        runs = [evl.run_of(side, branch=BRANCH), evl.run_of(self.review, branch=BRANCH)]
        self.assertEqual(self.pick(runs), [])

    def test_failed_or_cancelled_run_does_not_count(self):
        for c in ("failure", "cancelled", None):
            with self.subTest(c):
                self.assertEqual(
                    self.pick([evl.run_of(self.review, branch=BRANCH, conclusion=c)]), evl.ALL)

    def test_run_whose_head_is_not_a_commit_here_does_not_count(self):
        self.assertEqual(self.pick([evl.run_of("e" * 40, branch=BRANCH)]), evl.ALL)

    def test_no_usable_runs_runs_every_suite(self):
        for raw in ([], "", "gh: HTTP 502"):
            with self.subTest(raw):
                self.assertEqual(self.pick(raw), evl.ALL)


if __name__ == "__main__":
    unittest.main()
