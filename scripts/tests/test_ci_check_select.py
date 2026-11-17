"""Tests for scripts/ci-check-select.py, which picks the check.yml jobs a push needs (T117;
research D50, ADR 0053), written before the script.

The selector's interface, fixed by these tests:

    RUNS=<json> SHA=<sha> EVENT=<event> python3 scripts/ci-check-select.py

- It runs in the repository's root (the current directory) and reads the declaration
  `.github/check-jobs.yaml` from the working tree: `jobs:`, a mapping of check.yml job ids, each
  `{always: true}` or `{paths: [glob, ...]}`. The globs are read in order as GitHub's `paths`
  filters are: a later pattern wins, a pattern starting with `!` excludes, no match at all is no
  match; `**/` matches any folders (none included), `**` anything, `*` and `?` stay within one
  path segment.
- RUNS is check.yml's successful runs, newest first, as a JSON list of {head_sha, head_branch,
  conclusion}. The base is the newest run with conclusion `success` on `main` or
  `001-prose-to-accepted-slice` whose head_sha is a commit here and an ancestor of SHA.
- stdout is one JSON list of job ids in the declaration's order: every always job, and every
  paths job whose patterns match a file changed from the base to SHA (both paths of a rename and
  deleted files count). Every job for EVENT=workflow_dispatch, when RUNS is unset, empty, not
  JSON or holds no qualifying run, or when SHA cannot be resolved. Reasons go to stderr. Exit 0.
  With GITHUB_OUTPUT set it also appends the line `jobs=<the same JSON>` to that file.

Every rule case runs in a throwaway git repository with a small test-local declaration, so the
rules are judged apart from the real one; RealDeclarationTest judges T117's done condition on the
real `.github/check-jobs.yaml`.
"""
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import yaml

from gitenv import clean_env, make_home

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
SELECT = REPO / "scripts" / "ci-check-select.py"
DECL = ".github/check-jobs.yaml"
FEATURE = "001-prose-to-accepted-slice"

# Test-local declaration: an always job last, so the order is the declaration's, not "always first".
LOCAL = {"jobs": {
    "layout": {"always": True},
    "markdown": {"paths": ["**.md", "scripts/tools/**"]},
    "knowledge": {"paths": ["docs/**", "specs/**"]},
    "py_tests": {"paths": ["**", "!specs/**"]},
    "go": {"paths": ["**", "!docs/**", "!specs/**", "!*.md"]},
    "keep": {"paths": ["src/**", "!src/gen/**", "src/gen/keep/**"]},
    "secrets": {"always": True},
}}
EVERY = list(LOCAL["jobs"])
ALWAYS = ["layout", "secrets"]

BASE_FILES = {
    "README.md": "readme\n",
    "docs/index.md": "docs\n",
    "docs/data.yaml": "a: 1\n",
    "specs/001-x/spec.md": "spec\n",
    "specs/001-x/data.yaml": "a: 1\n",
    "src/main.go": "package main\n",
    "src/notes.md": "notes\n",
    "src/gen/x.go": "package gen\n",
    "src/gen/keep/y.go": "package keep\n",
    "scripts/tools/t.sh": "#!/bin/sh\n",
}


def ordered(names, decl=LOCAL):
    """names in the declaration's order."""
    return [j for j in decl["jobs"] if j in set(names)]


def run_of(sha, branch=FEATURE, conclusion: "str | None" = "success"):
    return {"head_sha": sha, "head_branch": branch, "conclusion": conclusion}


class Repo:
    """A throwaway git repository with a declaration and BASE_FILES committed as its base."""

    def __init__(self, tc, decl_text=None, files=None):
        self.tc = tc
        tmp = tempfile.TemporaryDirectory()
        tc.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.env = clean_env(make_home(tc))
        for k in ("RUNS", "SHA", "EVENT", "GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY"):
            self.env.pop(k, None)
        self.git("init", "-q", "-b", "main")
        self.git("config", "user.email", "t@example.invalid")
        self.git("config", "user.name", "t")
        self.git("config", "commit.gpgsign", "false")
        f = dict(BASE_FILES if files is None else files)
        f[DECL] = decl_text if decl_text is not None else yaml.safe_dump(LOCAL, sort_keys=False)
        self.base = self.commit(f)

    def git(self, *args):
        r = subprocess.run(["git", *args], cwd=self.root, env=self.env, capture_output=True,
                           text=True, timeout=30)
        self.tc.assertEqual(r.returncode, 0, r.stderr)
        return r.stdout.strip()

    def commit(self, files=None, remove=(), rename=None):
        for p, text in (files or {}).items():
            f = self.root / p
            f.parent.mkdir(parents=True, exist_ok=True)
            f.write_text(text)
            self.git("add", p)
        for p in remove:
            self.git("rm", "-q", p)
        if rename:
            (self.root / rename[1]).parent.mkdir(parents=True, exist_ok=True)
            self.git("mv", rename[0], rename[1])
        self.git("commit", "-q", "--allow-empty", "-m", "c")
        return self.git("rev-parse", "HEAD")

    def run(self, runs, sha=None, event="push", output=None):
        """Run the selector; runs is a list (dumped as JSON), a raw string, or None (unset)."""
        self.tc.assertTrue(SELECT.exists(), f"{SELECT} does not exist")
        env = dict(self.env, EVENT=event)
        if sha is not None:
            env["SHA"] = sha
        if runs is not None:
            env["RUNS"] = runs if isinstance(runs, str) else json.dumps(runs)
        if output is not None:
            env["GITHUB_OUTPUT"] = str(output)
        r = subprocess.run([sys.executable, str(SELECT)], cwd=self.root, env=env,
                           capture_output=True, text=True, timeout=60)
        print(f"ci-check-select: {r.stderr.strip()}", file=sys.stderr)
        self.tc.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        lines = [l for l in r.stdout.splitlines() if l.strip()]
        self.tc.assertEqual(len(lines), 1, f"stdout is not one line: {r.stdout!r}")
        try:
            out = json.loads(lines[0])
        except ValueError:
            raise self.tc.failureException(f"stdout is not JSON: {r.stdout!r}\n{r.stderr}")
        self.tc.assertIsInstance(out, list, r.stdout)
        self.tc.assertTrue(all(isinstance(x, str) for x in out), out)
        self.tc.assertEqual(len(out), len(set(out)), f"a job named twice: {out}")
        return out

    def change(self, files=None, **kw):
        """Commit a change on top of the green base and return the jobs selected for it."""
        sha = self.commit(files, **kw)
        return self.run([run_of(self.base)], sha)


class PathRuleTest(unittest.TestCase):
    """A changed file selects the paths jobs whose patterns match it, plus every always job."""

    def test_specs_markdown_only(self):
        self.assertEqual(Repo(self).change({"specs/001-x/spec.md": "x\n"}),
                         ordered(ALWAYS + ["markdown", "knowledge"]))

    def test_specs_non_markdown_only(self):
        self.assertEqual(Repo(self).change({"specs/001-x/data.yaml": "b: 2\n"}),
                         ordered(ALWAYS + ["knowledge"]))

    def test_docs_markdown_only(self):
        self.assertEqual(Repo(self).change({"docs/index.md": "x\n"}),
                         ordered(ALWAYS + ["markdown", "knowledge", "py_tests"]))

    def test_docs_non_markdown_only(self):
        self.assertEqual(Repo(self).change({"docs/data.yaml": "b: 2\n"}),
                         ordered(ALWAYS + ["knowledge", "py_tests"]))

    def test_code_file_beside_docs_runs_every_job(self):
        self.assertEqual(Repo(self).change({"docs/index.md": "x\n", "specs/001-x/spec.md": "x\n",
                                            "src/main.go": "package main // x\n"}), EVERY)

    def test_planted_root_markdown_is_excluded_from_go_but_deeper_markdown_is_not(self):
        # `!*.md` stays within the root segment: src/notes.md is not excluded.
        self.assertEqual(Repo(self).change({"README.md": "x\n"}),
                         ordered(ALWAYS + ["markdown", "py_tests"]))
        self.assertEqual(Repo(self).change({"src/notes.md": "x\n"}),
                         ordered(ALWAYS + ["markdown", "py_tests", "go", "keep"]))

    def test_exclude_after_a_match_and_a_later_match_reincludes(self):
        self.assertEqual(Repo(self).change({"src/gen/x.go": "package gen // x\n"}),
                         ordered(ALWAYS + ["py_tests", "go"]))
        self.assertEqual(Repo(self).change({"src/gen/keep/y.go": "package keep // x\n"}),
                         ordered(ALWAYS + ["py_tests", "go", "keep"]))

    def test_new_file(self):
        self.assertEqual(Repo(self).change({"scripts/tools/new.sh": "#!/bin/sh\n"}),
                         ordered(ALWAYS + ["markdown", "py_tests", "go"]))

    def test_deleted_file_counts(self):
        self.assertEqual(Repo(self).change(remove=["src/main.go"]),
                         ordered(ALWAYS + ["py_tests", "go", "keep"]))

    def test_rename_counts_the_old_path(self):
        got = Repo(self).change(rename=("src/main.go", "specs/001-x/main.go"))
        self.assertEqual(got, ordered(ALWAYS + ["knowledge", "py_tests", "go", "keep"]))

    def test_rename_counts_the_new_path(self):
        got = Repo(self).change(rename=("specs/001-x/data.yaml", "src/data.yaml"))
        self.assertEqual(got, ordered(ALWAYS + ["knowledge", "py_tests", "go", "keep"]))

    def test_planted_rename_within_specs_selects_no_code_job(self):
        got = Repo(self).change(rename=("specs/001-x/data.yaml", "specs/001-x/d.yaml"))
        self.assertEqual(got, ordered(ALWAYS + ["knowledge"]))

    def test_empty_change_runs_only_always_jobs(self):
        self.assertEqual(Repo(self).change(), ALWAYS)

    def test_output_is_in_declaration_order(self):
        decl = {"jobs": {"zeta": {"paths": ["docs/**"]}, "alpha": {"always": True},
                         "mid": {"paths": ["**.md"]}}}
        r = Repo(self, yaml.safe_dump(decl, sort_keys=False))
        self.assertEqual(r.change({"docs/index.md": "x\n"}), ["zeta", "alpha", "mid"])

    def test_non_ascii_path(self):
        self.assertEqual(Repo(self).change({"docs/nötes 文.yaml": "x\n"}),
                         ordered(ALWAYS + ["knowledge", "py_tests"]))


class BaseRuleTest(unittest.TestCase):
    """The base is the newest green run on a dev branch whose head is an ancestor of SHA."""

    def setUp(self):
        self.r = Repo(self)
        self.code = self.r.commit({"src/main.go": "package main // 1\n"})
        self.docs = self.r.commit({"docs/index.md": "x\n"})
        self.docs_only = ordered(ALWAYS + ["markdown", "knowledge", "py_tests"])

    def test_newest_green_run_is_the_base(self):
        runs = [run_of(self.code), run_of(self.r.base)]
        self.assertEqual(self.r.run(runs, self.docs), self.docs_only)

    def test_a_change_whose_run_failed_or_was_cancelled_is_judged_again(self):
        for concl in ("failure", "cancelled", "timed_out", None):
            with self.subTest(concl):
                runs = [run_of(self.code, conclusion=concl), run_of(self.r.base)]
                self.assertEqual(self.r.run(runs, self.docs), EVERY)

    def test_a_change_with_no_run_listed_is_judged_again(self):
        self.assertEqual(self.r.run([run_of(self.r.base)], self.docs), EVERY)

    def test_run_on_another_branch_is_not_a_base(self):
        runs = [run_of(self.code, branch="other"), run_of(self.r.base)]
        self.assertEqual(self.r.run(runs, self.docs), EVERY)
        runs = [run_of(self.code, branch="main"), run_of(self.r.base)]
        self.assertEqual(self.r.run(runs, self.docs), self.docs_only)

    def test_run_that_is_not_an_ancestor_is_not_a_base(self):
        self.r.git("checkout", "-q", "-b", "side", self.r.base)
        side = self.r.commit({"src/main.go": "package main // side\n"})
        self.r.git("checkout", "-q", "main")
        runs = [run_of(side), run_of(self.code)]
        self.assertEqual(self.r.run(runs, self.docs), self.docs_only)
        self.assertEqual(self.r.run([run_of(side)], self.docs), EVERY)

    def test_run_whose_head_is_not_a_commit_here_is_not_a_base(self):
        runs = [run_of("e" * 40), run_of(self.code)]
        self.assertEqual(self.r.run(runs, self.docs), self.docs_only)

    def test_sha_itself_green_selects_only_always_jobs(self):
        self.assertEqual(self.r.run([run_of(self.docs)], self.docs), ALWAYS)

    def test_sha_defaults_to_head(self):
        self.assertEqual(self.r.run([run_of(self.code)]), self.docs_only)

    def test_sha_from_env_names_the_end(self):
        # SHA, not HEAD, is the commit judged: SHA = the base gives no change though HEAD has one.
        self.assertEqual(self.r.run([run_of(self.code)], self.code), ALWAYS)


class EveryJobTest(unittest.TestCase):
    """Every job runs for a dispatch, without a base, or when the commit cannot be resolved."""

    def setUp(self):
        self.r = Repo(self)
        self.docs = self.r.commit({"docs/index.md": "x\n"})
        self.green = [run_of(self.r.base)]

    def test_planted_counter_case_push_with_a_base_is_not_every_job(self):
        self.assertNotEqual(self.r.run(self.green, self.docs, event="push"), EVERY)

    def test_workflow_dispatch(self):
        self.assertEqual(self.r.run(self.green, self.docs, event="workflow_dispatch"), EVERY)

    def test_no_base_run(self):
        for runs in (None, "", "not json", "[]", "{}", '"x"', "[1, 2]",
                     [run_of(self.r.base, conclusion="failure")],
                     [run_of(self.r.base, branch="feature/other")],
                     [{"head_branch": FEATURE, "conclusion": "success"}]):
            with self.subTest(runs):
                self.assertEqual(self.r.run(runs, self.docs), EVERY)

    def test_unresolvable_sha(self):
        for sha in ("f" * 40, "not-a-ref"):
            with self.subTest(sha):
                self.assertEqual(self.r.run(self.green, sha), EVERY)

    def test_base_all_zeros(self):
        self.assertEqual(self.r.run([run_of("0" * 40)], self.docs), EVERY)


class OutputTest(unittest.TestCase):
    def test_github_output_line(self):
        r = Repo(self)
        sha = r.commit({"specs/001-x/spec.md": "x\n"})
        out = r.root.parent / (r.root.name + "-output.txt")
        self.addCleanup(lambda: out.unlink(missing_ok=True))
        out.write_text("runner=ubuntu\n")
        got = r.run([run_of(r.base)], sha, output=out)
        lines = out.read_text().splitlines()
        self.assertEqual(lines[0], "runner=ubuntu", "the earlier outputs are kept")
        jobs = [l for l in lines if l.startswith("jobs=")]
        self.assertEqual(len(jobs), 1, lines)
        self.assertEqual(json.loads(jobs[0][len("jobs="):]), got)

    def test_github_output_line_for_every_job(self):
        r = Repo(self)
        out = r.root.parent / (r.root.name + "-output2.txt")
        self.addCleanup(lambda: out.unlink(missing_ok=True))
        got = r.run(None, r.base, output=out)
        self.assertEqual(got, EVERY)
        lines = out.read_text().splitlines()
        self.assertEqual(len(lines), 1, lines)
        self.assertTrue(lines[0].startswith("jobs="), lines)
        self.assertEqual(json.loads(lines[0][len("jobs="):]), EVERY)


class RealDeclarationTest(unittest.TestCase):
    """T117's done condition on the real .github/check-jobs.yaml (D50)."""

    def setUp(self):
        p = REPO / DECL
        self.assertTrue(p.exists(), f"{DECL} does not exist")
        self.text = p.read_text()
        self.decl = yaml.safe_load(self.text)
        jobs = self.decl["jobs"]
        self.every = list(jobs)
        self.always = [j for j, e in jobs.items() if (e or {}).get("always") is True]
        self.r = Repo(self, self.text)

    def want(self, *extra):
        return ordered(self.always + list(extra), self.decl)

    def test_always_jobs(self):
        self.assertEqual(set(self.always), {"layout", "secrets", "deps", "evals-current"})

    def test_specs_only_runs_always_markdown_and_knowledge(self):
        self.assertEqual(self.r.change({"specs/001-x/spec.md": "x\n"}),
                         self.want("markdown", "knowledge"))

    def test_docs_only_adds_py_tests(self):
        self.assertEqual(self.r.change({"docs/index.md": "x\n"}),
                         self.want("markdown", "knowledge", "py_tests"))

    def test_src_change_runs_every_job(self):
        self.assertEqual(self.r.change({"docs/index.md": "x\n", "specs/001-x/spec.md": "x\n",
                                        "src/main.go": "package main // x\n"}), self.every)

    def test_ci_file_change_runs_every_job(self):
        # D50 (owner, 2026-11-17): markdown and knowledge list the CI files, so a change to the CI
        # machinery, the declaration or a pin runs every job.
        for path in (".github/workflows/check.yml", "scripts/ci_select.py",
                     "scripts/ci-check-select.py", "scripts/ci-step.sh", "scripts/check.sh",
                     "src/deploy/images.lock", "scripts/tools/requirements-ci.txt"):
            with self.subTest(path):
                r = Repo(self, self.text)
                self.assertEqual(r.change({path: "changed\n"}), self.every)

    def test_declaration_change_runs_every_job(self):
        r = Repo(self, self.text)
        self.assertEqual(r.change({DECL: self.text + "# changed\n"}), self.every)

    def test_planted_src_alone_is_not_docs_only(self):
        got = self.r.change({"src/main.go": "package main // x\n"})
        self.assertIn("go", got)
        self.assertIn("docker", got)
        self.assertNotIn("knowledge", got)


if __name__ == "__main__":
    unittest.main()
