"""Tests for the dispatch and base-run rules of scripts/ci-evals-select.py (T120; research D45,
ADR 0048).

The selector's interface added by T120, fixed by these tests (T115's, in test_ci_evals_select.py,
stays: inputs from the environment only, the matrix on stdout, reasons on stderr):

    RUNS=<json> SHA=<sha> EVENT=<event> [ALL=true|false] [MODE=pending REF=<branch>] \
        python3 scripts/ci-evals-select.py

- RUNS is the successful evals runs the workflow listed from the GitHub API, newest first, as a
  JSON list of objects with at least `head_sha`, `head_branch` and `conclusion` (the fields of
  `.workflow_runs[]`; others are ignored). The selector, the one owner of the base-run rule,
  chooses the base: the first (newest) run whose conclusion is `success`, whose head_branch is
  `main` or `001-prose-to-accepted-slice`, and whose head_sha is a commit here that is an ancestor
  of SHA (SHA itself included). When RUNS is set, BEFORE is not read.
- Every suite when there is no such run: RUNS empty (the API did not answer), not JSON, `[]`, or
  holding no run that qualifies (failed, on another branch, not an ancestor, not a commit here).
- ALL is the dispatch input `all` as the workflow's `env:` gives it: `true` chooses every suite;
  `false` or unset chooses as for a push. EVENT no longer matters: workflow_dispatch selects as a
  push does.
- MODE=pending is check.yml's evals-current job: the same choice, written to
  $GITHUB_STEP_SUMMARY (each pending suite by name, `analyst` or `steps <agent>`, or a line that
  no suite is pending); exit 1 when REF is exactly `main` and a suite is pending, exit 0 otherwise.
  Without MODE=pending the selector always exits 0 (evals.yml's config on a main dispatch).

Each rule has a planted counter-case. The throwaway repository and the test-local declaration are
test_ci_evals_select.py's, imported unchanged.
"""
import json
import os
import subprocess
import sys
import tempfile
import unittest

from test_ci_evals_select import ALL, SELECT, Repo

FEATURE = "001-prose-to-accepted-slice"
REVIEW = "src/agents/review/instruction.md"
ANALYST = "src/agents/analyst/instruction.md"
REV = [("steps", "review")]
NAMES = {("analyst", ""): "analyst", ("steps", "tests"): "steps tests",
         ("steps", "code"): "steps code", ("steps", "review"): "steps review"}


def run_of(sha, branch=FEATURE, conclusion="success"):
    return {"head_sha": sha, "head_branch": branch, "conclusion": conclusion, "id": 1}


class Result:
    def __init__(self, code, pairs, summary, stderr):
        self.code, self.pairs, self.summary, self.stderr = code, pairs, summary, stderr


def select(tc, repo, sha, runs, all_=None, event="workflow_dispatch", mode=None, ref=None,
           extra=None):
    """Run the selector; runs is a list (dumped as JSON) or a raw string for RUNS."""
    tc.assertTrue(SELECT.exists(), f"{SELECT} does not exist")
    env = {k: v for k, v in repo.env.items() if k not in ("BEFORE", "ALL", "MODE", "REF", "RUNS")}
    env.update(SHA=sha, EVENT=event, RUNS=runs if isinstance(runs, str) else json.dumps(runs))
    if all_ is not None:
        env["ALL"] = all_
    if mode is not None:
        env["MODE"] = mode
    if ref is not None:
        env["REF"] = ref
    env.update(extra or {})
    tmp = tempfile.TemporaryDirectory()
    tc.addCleanup(tmp.cleanup)
    summ = os.path.join(tmp.name, "summary.md")
    open(summ, "w").close()
    env["GITHUB_STEP_SUMMARY"] = summ
    r = subprocess.run([sys.executable, str(SELECT)], cwd=repo.root, env=env,
                       capture_output=True, text=True, timeout=60)
    print(f"ci-evals-select (exit {r.returncode}): {r.stderr.strip()}", file=sys.stderr)
    try:
        out = json.loads(r.stdout)
    except ValueError:
        tc.fail(f"stdout is not one JSON list: {r.stdout!r}\n{r.stderr}")
    tc.assertIsInstance(out, list, r.stdout)
    pairs = [(m["suite"], m["cases"]) for m in out]
    return Result(r.returncode, pairs, open(summ).read(), r.stderr)


def pick(tc, repo, sha, runs, **kw):
    res = select(tc, repo, sha, runs, **kw)
    tc.assertEqual(res.code, 0, res.stderr)
    return res.pairs


def feature(repo):
    repo.git("checkout", "-q", "-b", FEATURE)


class AllInputTest(unittest.TestCase):
    """The dispatch input `all`: true chooses every suite, false chooses by what changed."""

    def setUp(self):
        self.r = Repo(self)
        feature(self.r)
        self.runs = [run_of(self.r.base)]
        self.docs = self.r.commit({"docs/index.md": "x\n"})

    def test_all_true_chooses_every_suite(self):
        self.assertEqual(pick(self, self.r, self.docs, self.runs, all_="true"), ALL)

    def test_planted_all_false_chooses_by_change(self):
        self.assertEqual(pick(self, self.r, self.docs, self.runs, all_="false"), [])
        sha = self.r.commit({REVIEW: "x\n"})
        self.assertEqual(pick(self, self.r, sha, self.runs, all_="false"), REV)

    def test_all_unset_or_empty_is_false(self):
        self.assertEqual(pick(self, self.r, self.docs, self.runs), [])
        self.assertEqual(pick(self, self.r, self.docs, self.runs, all_=""), [])

    def test_dispatch_selects_as_a_push(self):
        # Before T120 a workflow_dispatch chose every suite whatever changed.
        sha = self.r.commit({REVIEW: "x\n"})
        for event in ("workflow_dispatch", "push"):
            with self.subTest(event):
                self.assertEqual(pick(self, self.r, self.docs, self.runs, event=event), [])
                self.assertEqual(pick(self, self.r, sha, self.runs, event=event), REV)

    def test_dispatch_on_the_green_commit_chooses_none(self):
        self.assertEqual(pick(self, self.r, self.r.base, self.runs), [])
        self.assertEqual(pick(self, self.r, self.r.base, self.runs, all_="true"), ALL)

    def test_runs_set_ignores_before(self):
        # RUNS names the base; a BEFORE in the environment (T115's input) is not read.
        sha = self.r.commit({REVIEW: "x\n"})
        self.assertEqual(pick(self, self.r, sha, self.runs, extra={"BEFORE": sha}), REV)


class NoBaseTest(unittest.TestCase):
    """Every suite when no successful run on a dev branch is an ancestor."""

    def setUp(self):
        self.r = Repo(self)
        feature(self.r)
        self.sha = self.r.commit({"docs/index.md": "x\n"})

    def test_no_run(self):
        self.assertEqual(pick(self, self.r, self.sha, []), ALL)

    def test_api_did_not_answer(self):
        self.assertEqual(pick(self, self.r, self.sha, ""), ALL)

    def test_runs_not_json(self):
        self.assertEqual(pick(self, self.r, self.sha, "gh: HTTP 502"), ALL)

    def test_planted_known_base_chooses_none(self):
        # The counter-case of the three above.
        self.assertEqual(pick(self, self.r, self.sha, [run_of(self.r.base)]), [])

    def test_failed_run_does_not_count(self):
        for c in ("failure", "cancelled", None):
            with self.subTest(c):
                self.assertEqual(pick(self, self.r, self.sha, [run_of(self.r.base, conclusion=c)]),
                                 ALL)

    def test_run_on_another_branch_does_not_count(self):
        for b in ("feature-x", "main-old", "001-prose-to-accepted-slice-2", None):
            with self.subTest(b):
                self.assertEqual(pick(self, self.r, self.sha, [run_of(self.r.base, branch=b)]),
                                 ALL)
        self.assertEqual(pick(self, self.r, self.sha, [run_of(self.r.base, branch="main")]), [])


class UnresolvableBaseTest(unittest.TestCase):
    def test_unknown_commit_chooses_every_suite(self):
        r = Repo(self)
        sha = r.commit({"docs/index.md": "x\n"})
        for bad in ("a" * 40, "0" * 40, "", "not-a-sha"):
            with self.subTest(bad):
                self.assertEqual(pick(self, r, sha, [run_of(bad, branch="main")]), ALL)

    def test_older_resolvable_run_is_the_base(self):
        # The newest run is not a commit here; the older one is, and is an ancestor.
        r = Repo(self)
        r.commit({REVIEW: "x\n"})
        sha = r.commit({"docs/index.md": "x\n"})
        runs = [run_of("a" * 40, branch="main"), run_of(r.base, branch="main")]
        self.assertEqual(pick(self, r, sha, runs), REV)


class AncestorTest(unittest.TestCase):
    """The base is the newest successful run on main or the feature branch whose head is an
    ancestor of the commit."""

    def test_main_run_counts_on_the_feature_branch(self):
        r = Repo(self)                     # base on main, green there
        feature(r)
        sha = r.commit({REVIEW: "x\n"})
        self.assertEqual(pick(self, r, sha, [run_of(r.base, branch="main")]), REV)

    def test_feature_run_counts_on_main(self):
        r = Repo(self)
        feature(r)
        f1 = r.commit({REVIEW: "x\n"})
        r.git("checkout", "-q", "main")
        r.git("merge", "-q", "--ff-only", FEATURE)
        sha = r.commit({ANALYST: "x\n"})
        self.assertEqual(pick(self, r, sha, [run_of(f1)], ref="main"), [("analyst", "")])

    def test_planted_run_not_an_ancestor_does_not_count(self):
        r = Repo(self)
        feature(r)
        f1 = r.commit({REVIEW: "x\n"})
        r.git("checkout", "-q", "main")
        m1 = r.commit({"docs/index.md": "x\n"})          # a sibling of f1, green on main
        self.assertEqual(pick(self, r, f1, [run_of(m1, branch="main")]), ALL)
        # The newest ancestor below it is the base.
        self.assertEqual(pick(self, r, f1, [run_of(m1, branch="main"),
                                            run_of(r.base, branch="main")]), REV)

    def test_newest_ancestor_is_the_base(self):
        r = Repo(self)
        feature(r)
        f1 = r.commit({REVIEW: "x\n"})
        sha = r.commit({"docs/index.md": "y\n"})
        self.assertEqual(pick(self, r, sha, [run_of(f1), run_of(r.base, branch="main")]), [])
        # Counter-case: with only the older run, the review change is pending.
        self.assertEqual(pick(self, r, sha, [run_of(r.base, branch="main")]), REV)

    def test_merge_commit_whose_feature_parent_is_green_chooses_none(self):
        r = Repo(self)
        feature(r)
        f1 = r.commit({REVIEW: "x\n"})
        r.git("checkout", "-q", "main")
        r.commit({"docs/index.md": "x\n"})
        r.git("merge", "-q", "--no-ff", "-m", "merge", FEATURE)
        merge = r.git("rev-parse", "HEAD")
        self.assertEqual(len(r.git("rev-list", "--parents", "-n1", merge).split()), 3)
        runs = [run_of(f1), run_of(r.base, branch="main")]
        self.assertEqual(pick(self, r, merge, runs, ref="main"), [])
        res = select(self, r, merge, runs, mode="pending", ref="main")
        self.assertEqual((res.code, res.pairs), (0, []), res.stderr)

    def test_planted_merge_bringing_a_suite_change_from_main_chooses_it(self):
        # Counter-case: main's own side of the merge changed the analyst after f1's green run.
        r = Repo(self)
        feature(r)
        f1 = r.commit({REVIEW: "x\n"})
        r.git("checkout", "-q", "main")
        r.commit({ANALYST: "x\n"})
        r.git("merge", "-q", "--no-ff", "-m", "merge", FEATURE)
        merge = r.git("rev-parse", "HEAD")
        runs = [run_of(f1)]
        self.assertEqual(pick(self, r, merge, runs, ref="main"), [("analyst", "")])
        res = select(self, r, merge, runs, mode="pending", ref="main")
        self.assertEqual((res.code, res.pairs), (1, [("analyst", "")]), res.stderr)


class PendingModeTest(unittest.TestCase):
    """check.yml's evals-current: a notice on the feature branch, a failure on main."""

    def setUp(self):
        self.r = Repo(self)
        self.runs = [run_of(self.r.base, branch="main")]
        self.docs = self.r.commit({"docs/index.md": "x\n"})
        self.rev = self.r.commit({REVIEW: "x\n"})

    def pending(self, sha, ref, runs=None):
        return select(self, self.r, sha, self.runs if runs is None else runs,
                      event="push", mode="pending", ref=ref)

    def assert_names(self, summary, pairs):
        for p in pairs:
            self.assertIn(NAMES[p], summary)
        for p in set(NAMES) - set(pairs):
            if not any(NAMES[p] in NAMES[q] for q in pairs):
                self.assertNotIn(NAMES[p], summary)

    def test_feature_branch_passes_naming_the_pending_suite(self):
        res = self.pending(self.rev, FEATURE)
        self.assertEqual((res.code, res.pairs), (0, REV), res.stderr)
        self.assert_names(res.summary, REV)

    def test_feature_branch_says_none_is_pending(self):
        res = self.pending(self.docs, FEATURE)
        self.assertEqual((res.code, res.pairs), (0, []), res.stderr)
        self.assertTrue(res.summary.strip(), "the summary is empty")
        self.assert_names(res.summary, [])

    def test_planted_main_with_a_pending_suite_fails(self):
        res = self.pending(self.rev, "main")
        self.assertEqual((res.code, res.pairs), (1, REV), res.stderr)
        self.assert_names(res.summary, REV)

    def test_main_with_none_pending_passes(self):
        res = self.pending(self.docs, "main")
        self.assertEqual((res.code, res.pairs), (0, []), res.stderr)

    def test_planted_main_without_a_base_fails_with_every_suite(self):
        res = self.pending(self.docs, "main", runs=[])
        self.assertEqual((res.code, res.pairs), (1, ALL), res.stderr)
        self.assert_names(res.summary, ALL)

    def test_feature_branch_without_a_base_passes_with_every_suite(self):
        res = self.pending(self.docs, FEATURE, runs=[])
        self.assertEqual((res.code, res.pairs), (0, ALL), res.stderr)
        self.assert_names(res.summary, ALL)

    def test_only_main_exactly_fails(self):
        for ref in ("main-x", "xmain", "refs/heads/main-old", ""):
            with self.subTest(ref):
                self.assertEqual(self.pending(self.rev, ref).code, 0)

    def test_planted_no_pending_mode_never_fails(self):
        # evals.yml's config job, dispatched on main: it chooses, it does not judge.
        res = select(self, self.r, self.rev, self.runs, ref="main")
        self.assertEqual((res.code, res.pairs), (0, REV), res.stderr)


if __name__ == "__main__":
    unittest.main()
