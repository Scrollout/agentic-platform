"""Tests for scripts/ci_select.py, the one home of the glob matching and the base-run rule that
scripts/ci-check-select.py and scripts/ci-evals-select.py share (T117; research D50, ADR 0053;
constitution: Single owner for rules), written before the module.

The module's interface, fixed by these tests:

- glob_re(glob): a compiled regex matching a whole path: `**/` any folders (none included), `**`
  anything, `*` and `?` within one path segment, everything else literal.
- matches(path, patterns): GitHub's `paths` filter: the patterns in order, the last one that
  matches decides, a pattern starting with `!` excludes; no pattern matching is False.
- base_from_runs(raw_json, sha): in the current directory's repository, the head_sha of the
  newest run (first in the list) with conclusion `success` on any branch
  whose head_sha is a commit here and an ancestor of sha (sha itself
  included); "" otherwise, also for text that is not a JSON list.
- changed_files(before, sha): the paths changed from before to sha, both paths of a rename and
  deleted files included; None when the diff fails.

ci-evals-select.py uses the module and keeps no copy of those rules (EvalsSelectorUsesModuleTest,
with a planted copy).
"""
import importlib
import json
import os
import re
import sys
import unittest
from pathlib import Path
from unittest import mock

from test_ci_check_select import Repo, run_of

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
SCRIPTS = REPO / "scripts"
MODULE = SCRIPTS / "ci_select.py"
EVALS = SCRIPTS / "ci-evals-select.py"


def load(tc):
    tc.assertTrue(MODULE.exists(), f"{MODULE} does not exist")
    if str(SCRIPTS) not in sys.path:
        sys.path.insert(0, str(SCRIPTS))
    return importlib.import_module("ci_select")


class GlobTest(unittest.TestCase):
    def setUp(self):
        self.m = load(self)

    def check(self, glob, yes, no):
        rx = self.m.glob_re(glob)
        for p in yes:
            self.assertTrue(rx.match(p), f"{glob} should match {p}")
        for p in no:
            self.assertFalse(rx.match(p), f"{glob} should not match {p}")

    def test_double_star_slash_matches_zero_or_more_folders(self):
        self.check("a/**/b.go", ["a/b.go", "a/x/b.go", "a/x/y/b.go"], ["ab.go", "a/b.gox", "b.go"])

    def test_double_star_matches_anything(self):
        self.check("**", ["a", "a/b/c.md", ".claude/x"], [])
        self.check("**.md", ["a.md", "docs/x/a.md"], ["a.mdx", "a.md/b"])
        self.check("docs/**", ["docs/a", "docs/a/b"], ["docsx/a", "a/docs/b"])

    def test_single_star_and_question_stay_in_one_segment(self):
        self.check("*.md", ["README.md", ".md"], ["docs/a.md"])
        self.check("src/?.go", ["src/a.go"], ["src/ab.go", "src/a/b.go"])
        self.check(".markdownlint*", [".markdownlint.yaml"], ["x/.markdownlint.yaml"])

    def test_literals_are_literal(self):
        self.check("a.b+c", ["a.b+c"], ["aXb+c", "a.bbc"])


class MatchesTest(unittest.TestCase):
    def setUp(self):
        self.m = load(self)

    def test_no_match_is_false(self):
        self.assertFalse(self.m.matches("src/a.go", ["docs/**"]))
        self.assertFalse(self.m.matches("src/a.go", []))

    def test_exclude_alone_matches_nothing(self):
        self.assertFalse(self.m.matches("src/a.go", ["!docs/**"]))
        self.assertFalse(self.m.matches("docs/a.md", ["!docs/**"]))

    def test_exclude_after_a_match(self):
        pats = ["**", "!docs/**", "!*.md"]
        self.assertTrue(self.m.matches("src/a.go", pats))
        self.assertTrue(self.m.matches("src/a.md", pats))
        self.assertFalse(self.m.matches("docs/a.go", pats))
        self.assertFalse(self.m.matches("README.md", pats))

    def test_later_match_reincludes(self):
        pats = ["src/**", "!src/gen/**", "src/gen/keep/**"]
        self.assertTrue(self.m.matches("src/a.go", pats))
        self.assertFalse(self.m.matches("src/gen/a.go", pats))
        self.assertTrue(self.m.matches("src/gen/keep/a.go", pats))

    def test_planted_order_matters(self):
        # The same patterns the other way round: the include comes last and wins.
        self.assertTrue(self.m.matches("docs/a.go", ["!docs/**", "**"]))
        self.assertFalse(self.m.matches("docs/a.go", ["**", "!docs/**"]))


class GitFunctionsTest(unittest.TestCase):
    """base_from_runs and changed_files in a throwaway repository as the current directory."""

    def setUp(self):
        self.m = load(self)
        self.r = Repo(self)
        self.code = self.r.commit({"src/main.go": "package main // 1\n"})
        self.head = self.r.commit({"docs/index.md": "x\n"}, remove=["src/gen/x.go"],
                                  rename=("specs/001-x/data.yaml", "src/data.yaml"))
        env = mock.patch.dict(os.environ, self.r.env, clear=True)
        env.start()
        self.addCleanup(env.stop)
        cwd = os.getcwd()
        os.chdir(self.r.root)
        self.addCleanup(os.chdir, cwd)

    def test_base_is_the_newest_qualifying_run(self):
        runs = [run_of(self.code, conclusion="failure"), run_of("e" * 40),
                run_of(self.code, branch="other"), run_of(self.code), run_of(self.r.base)]
        self.assertEqual(self.m.base_from_runs(json.dumps(runs), self.head), self.code)
        self.assertEqual(self.m.base_from_runs(json.dumps([run_of(self.code, branch="main")]),
                                               self.head), self.code)
        self.assertEqual(self.m.base_from_runs(json.dumps([run_of(self.code, branch="other")]),
                                               self.head), self.code)

    def test_sha_itself_qualifies(self):
        self.assertEqual(self.m.base_from_runs(json.dumps([run_of(self.head)]), self.head),
                         self.head)

    def test_no_base(self):
        for raw in ("", "x", "{}", "[]", json.dumps([run_of(self.code, conclusion="cancelled")]),
                    json.dumps([run_of("0" * 40)])):
            with self.subTest(raw):
                self.assertEqual(self.m.base_from_runs(raw, self.head), "")

    def test_planted_descendant_is_not_a_base(self):
        # head is not an ancestor of code.
        self.assertEqual(self.m.base_from_runs(json.dumps([run_of(self.head)]), self.code), "")

    def test_changed_files_has_both_rename_paths_and_deletions(self):
        got = self.m.changed_files(self.code, self.head)
        self.assertEqual(sorted(got), sorted(["docs/index.md", "src/gen/x.go",
                                              "specs/001-x/data.yaml", "src/data.yaml"]))

    def test_changed_files_none_when_the_diff_fails(self):
        self.assertIsNone(self.m.changed_files("f" * 40, self.head))

    def test_changed_files_empty_for_no_change(self):
        self.assertEqual(list(self.m.changed_files(self.head, self.head)), [])


DEF = re.compile(r"^\s*def\s+(glob_re|base_from_runs|matches|changed_files)\b", re.M)
COMPILE_GLOB = re.compile(r"re\.escape\(|\(\?:\.\*/\)\?")


def judge_evals_uses_module(text):
    """ci-evals-select.py imports ci_select and keeps no copy of its rules."""
    v = []
    if not re.search(r"^\s*(import\s+ci_select\b|from\s+ci_select\s+import\b)", text, re.M):
        v.append("ci-evals-select.py does not import ci_select")
    v += [f"ci-evals-select.py defines its own {m}" for m in DEF.findall(text)]
    if COMPILE_GLOB.search(text):
        v.append("ci-evals-select.py builds glob regexes itself")
    return v


class EvalsSelectorUsesModuleTest(unittest.TestCase):
    def test_real_evals_selector_uses_the_module(self):
        self.assertEqual(judge_evals_uses_module(EVALS.read_text()), [])

    def test_planted_copy_fails(self):
        good = "import json\n\nimport ci_select\n\n\ndef main():\n    return ci_select.matches\n"
        self.assertEqual(judge_evals_uses_module(good), [])
        for bad in (good.replace("import ci_select\n", ""),
                    good + "\n\ndef glob_re(glob):\n    return glob\n",
                    good + "\n\ndef base_from_runs(raw, sha):\n    return ''\n",
                    good + "\nX = '(?:.*/)?'\n"):
            with self.subTest(bad[-40:]):
                self.assertTrue(judge_evals_uses_module(bad))


if __name__ == "__main__":
    unittest.main()
