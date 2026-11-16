"""Tests for scripts/ci-evals-select.py, which picks the live eval suites a push changes (T115).

The selector's interface, fixed by these tests:

    BEFORE=<sha> SHA=<sha> EVENT=<event> python3 scripts/ci-evals-select.py

- It runs in the repository's root (the current directory) and reads, from git, the files that
  changed from BEFORE to SHA (both sides of a rename, deletions included), the declaration
  `src/tests/evals/suites.yaml` and `src/deploy/images.lock` as of SHA, and images.lock as of
  BEFORE. Its inputs come from the environment only, never from arguments.
- EVENT is the GitHub event's name (`push` or `workflow_dispatch`).
- The declaration is `suites:`, a list of {suite, cases, paths, pins}: suite is the folder under
  src/tests/evals, cases the agent or case list passed as EVAL_CASES ("" for all), paths the path
  globs the suite reads, pins the images.lock pin names it runs on. A glob is GitHub's paths
  filter form: `**` matches across `/`, `*` and `?` within one path segment.
- stdout is one JSON list, the evals job's matrix: [{"suite": ..., "cases": ...}, ...] in the
  declaration's order, each suite at most once, `[]` for none. Exit 0. Why it chose what it
  chose goes to stderr.
- A suite is chosen when a changed file matches one of its globs or one of its pins changed
  value, was added or removed between the two images.lock texts. Every suite is chosen when
  BEFORE is all zeros or empty, or when BEFORE is not a commit in the fetched history. Since T120
  a workflow_dispatch selects as a push does; the workflows give the selector RUNS (the listed
  successful evals runs) and the dispatch input ALL instead of BEFORE, judged in
  test_ci_evals_dispatch.py.

Every case runs in a throwaway git repository with a small test-local declaration, so the
selector's rules are judged apart from the real declaration; RealDeclarationTest then judges the
done criteria of T115 on the real src/tests/evals/suites.yaml and images.lock.
"""
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from gitenv import clean_env, make_home

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
SELECT = REPO / "scripts" / "ci-evals-select.py"
DECL = "src/tests/evals/suites.yaml"
LOCK = "src/deploy/images.lock"
ZEROS = "0" * 40

STEP_SHARED = ["src/tests/evals/steps/**", "src/tests/evals/runevents/**",
               "src/engine/procedures/**", "src/deploy/gateway/**", "src/deploy/Dockerfile.agent",
               "src/catalogue/api-feature-go/**"]
STEP_PINS = ["docker-agent", "golang", "nginx", "oapi-codegen"]


def step(agent):
    return {"suite": "steps", "cases": agent,
            "paths": [f"src/agents/{agent}.yaml", f"src/agents/{agent}/**"] + STEP_SHARED,
            "pins": list(STEP_PINS)}


SUITES = [
    {"suite": "analyst", "cases": "",
     "paths": ["src/agents/analyst.yaml", "src/agents/analyst/**", "src/tests/evals/analyst/**",
               "src/tests/evals/runevents/**", "src/engine/slices/**", "src/deploy/gateway/**",
               "src/catalogue/api-feature-go/pattern.yaml"],
     "pins": ["docker-agent", "nginx"]},
    step("tests"), step("code"), step("review"),
]
ALL = [("analyst", ""), ("steps", "tests"), ("steps", "code"), ("steps", "review")]
STEPS = [("steps", "tests"), ("steps", "code"), ("steps", "review")]

LOCK_TEXT = ("# pins\n"
             "docker-agent docker/docker-agent:1.0@sha256:" + "1" * 64 + "\n"
             "golang golang:1.26-alpine@sha256:" + "2" * 64 + "\n"
             "nginx nginx:1.29-alpine@sha256:" + "3" * 64 + "\n"
             "oapi-codegen github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.0\n"
             "gvisor-release 20261109.0\n"
             "alpine alpine:3.22@sha256:" + "4" * 64 + "\n")

BASE_FILES = {
    "src/agents/analyst.yaml": "a\n", "src/agents/analyst/instruction.md": "a\n",
    "src/agents/tests.yaml": "t\n", "src/agents/tests/instruction.md": "t\n",
    "src/agents/code.yaml": "c\n", "src/agents/code/instruction.md": "c\n",
    "src/agents/review.yaml": "r\n", "src/agents/review/instruction.md": "r\n",
    "src/agents/README.md": "readme\n",
    "src/tests/evals/analyst/cases.go": "package analyst\n",
    "src/tests/evals/steps/cases.go": "package steps\n",
    "src/tests/evals/steps/testdata/ref/impl/main.go": "package main\n",
    "src/tests/evals/runevents/runevents.go": "package runevents\n",
    "src/engine/slices/message.go": "package slices\n",
    "src/engine/procedures/p.go": "package procedures\n",
    "src/engine/store/store.go": "package store\n",
    "src/deploy/gateway/nginx.conf": "x\n",
    "src/deploy/Dockerfile.agent": "FROM x\n",
    "src/deploy/Dockerfile.check": "FROM y\n",
    "src/catalogue/api-feature-go/pattern.yaml": "p\n",
    "src/catalogue/api-feature-go/template/go.mod": "module t\n",
    "docs/index.md": "docs\n",
    "README.md": "readme\n",
}


def decl_text(suites=SUITES):
    import yaml
    return yaml.safe_dump({"suites": suites}, sort_keys=False)


class Repo:
    """A throwaway git repository with the test-local declaration, lock and files committed."""

    def __init__(self, tc, suites=SUITES, lock=LOCK_TEXT):
        self.tc = tc
        tmp = tempfile.TemporaryDirectory()
        tc.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.env = clean_env(make_home(tc))
        self.git("init", "-q", "-b", "main")
        self.git("config", "user.email", "t@example.invalid")
        self.git("config", "user.name", "t")
        self.git("config", "commit.gpgsign", "false")
        files = dict(BASE_FILES)
        files[DECL] = decl_text(suites) if isinstance(suites, list) else suites
        files[LOCK] = lock
        self.base = self.commit(files)

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

    def select(self, before, sha=None, event="push"):
        self.tc.assertTrue(SELECT.exists(), f"{SELECT} does not exist")
        env = dict(self.env, BEFORE=before, SHA=sha or self.git("rev-parse", "HEAD"), EVENT=event)
        r = subprocess.run([sys.executable, str(SELECT)], cwd=self.root, env=env,
                           capture_output=True, text=True, timeout=60)
        self.tc.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        print(f"ci-evals-select: {r.stderr.strip()}", file=sys.stderr)
        out = json.loads(r.stdout)
        self.tc.assertIsInstance(out, list, r.stdout)
        for m in out:
            self.tc.assertEqual(set(m), {"suite", "cases"}, m)
        pairs = [(m["suite"], m["cases"]) for m in out]
        self.tc.assertEqual(len(pairs), len(set(pairs)), f"a suite chosen twice: {pairs}")
        return pairs

    def change(self, files=None, **kw):
        """Commit a change on top of the base and return the suites the selector picks for it."""
        sha = self.commit(files, **kw)
        return self.select(self.base, sha)


class GlobRuleTest(unittest.TestCase):
    """A change under one suite's own globs chooses exactly that suite."""

    def test_each_suite_alone(self):
        cases = {"src/agents/analyst/instruction.md": [("analyst", "")],
                 "src/tests/evals/analyst/cases.go": [("analyst", "")],
                 "src/agents/tests.yaml": [("steps", "tests")],
                 "src/agents/code/instruction.md": [("steps", "code")],
                 "src/agents/review/instruction.md": [("steps", "review")],
                 "src/agents/review.yaml": [("steps", "review")]}
        for path, want in cases.items():
            with self.subTest(path):
                self.assertEqual(Repo(self).change({path: "changed\n"}), want)

    def test_new_file_under_a_glob(self):
        self.assertEqual(Repo(self).change({"src/agents/review/new.json": "{}\n"}),
                         [("steps", "review")])

    def test_deleted_file_under_a_glob(self):
        self.assertEqual(Repo(self).change(remove=["src/agents/code/instruction.md"]),
                         [("steps", "code")])

    def test_renamed_out_of_a_glob_counts_the_old_path(self):
        got = Repo(self).change(rename=("src/agents/review/instruction.md", "docs/moved.md"))
        self.assertEqual(got, [("steps", "review")])

    def test_two_suites_changed_in_declaration_order(self):
        got = Repo(self).change({"src/agents/review/instruction.md": "x\n",
                                 "src/agents/analyst.yaml": "x\n"})
        self.assertEqual(got, [("analyst", ""), ("steps", "review")])

    def test_planted_near_miss_paths_choose_none(self):
        # A prefix of a glob's folder, a sibling file, a deeper `*`-less file name: none match.
        for path in ("src/agents/reviewer/instruction.md", "src/agents/review.yaml.bak",
                     "src/agents/README.md", "src/tests/evals/stepsx/a.go",
                     "src/catalogue/api-feature-gox/pattern.yaml"):
            with self.subTest(path):
                self.assertEqual(Repo(self).change({path: "x\n"}), [])

    def test_double_star_between_matches_zero_folders(self):
        suites = [dict(SUITES[0], paths=["src/tests/evals/analyst/**/cases.go"])] + SUITES[1:]
        for path, want in (("src/tests/evals/analyst/cases.go", [("analyst", "")]),
                           ("src/tests/evals/analyst/a/b/cases.go", [("analyst", "")]),
                           ("src/tests/evals/analyst/other.go", []),
                           ("src/tests/evals/analystcases.go", [])):
            with self.subTest(path):
                self.assertEqual(Repo(self, suites).change({path: "x\n"}), want)

    def test_non_ascii_path(self):
        # git quotes such names unless asked for -z output; the selector must see the real name.
        self.assertEqual(Repo(self).change({"src/agents/review/n\u00f6tes \u6587.md": "x\n"}),
                         [("steps", "review")])
        self.assertEqual(Repo(self).change({"docs/n\u00f6tes \u6587.md": "x\n"}), [])

    def test_planted_single_star_stays_in_one_segment(self):
        suites = [dict(SUITES[0], paths=["src/agents/*.yaml"])] + SUITES[1:]
        r = Repo(self, suites)
        self.assertEqual(r.change({"src/agents/analyst.yaml": "x\n"}),
                         [("analyst", "")])
        sha = r.commit({"src/agents/deep/x.yaml": "x\n"})
        self.assertEqual(r.select(r.git("rev-parse", "HEAD~1"), sha), [])


class SharedInputTest(unittest.TestCase):
    """A file that several suites read chooses every suite that declares it, and no other."""

    def test_shared_inputs(self):
        cases = {"src/tests/evals/runevents/runevents.go": ALL,
                 "src/deploy/gateway/nginx.conf": ALL,
                 "src/engine/slices/message.go": [("analyst", "")],
                 "src/deploy/Dockerfile.agent": STEPS,
                 "src/engine/procedures/p.go": STEPS,
                 "src/tests/evals/steps/testdata/ref/impl/main.go": STEPS,
                 "src/catalogue/api-feature-go/pattern.yaml": ALL,
                 "src/catalogue/api-feature-go/template/go.mod": STEPS}
        for path, want in cases.items():
            with self.subTest(path):
                self.assertEqual(Repo(self).change({path: "changed\n"}), want)

    def test_planted_neighbours_of_shared_inputs_choose_none(self):
        for path in ("src/engine/store/store.go", "src/deploy/Dockerfile.check",
                     "src/deploy/gateway.md", "src/engine/slicesx/a.go"):
            with self.subTest(path):
                self.assertEqual(Repo(self).change({path: "changed\n"}), [])


class PinRuleTest(unittest.TestCase):
    """images.lock is judged by its pins: texts that differ only in a pin no suite runs on choose
    none; texts that differ in a pin a suite runs on choose the suites that declare it."""

    def lock_with(self, name, value):
        out = []
        for line in LOCK_TEXT.splitlines():
            f = line.split()
            out.append(f"{name} {value}" if len(f) == 2 and f[0] == name else line)
        return "\n".join(out) + "\n"

    def test_planted_pin_no_suite_runs_on_chooses_none(self):
        self.assertEqual(Repo(self).change({LOCK: self.lock_with("gvisor-release", "20261116.0")}), [])

    def test_planted_comment_only_change_chooses_none(self):
        self.assertEqual(Repo(self).change({LOCK: LOCK_TEXT.replace("# pins", "# the pins")}), [])

    def test_planted_new_undeclared_pin_chooses_none(self):
        self.assertEqual(Repo(self).change({LOCK: LOCK_TEXT + "cosign cosign:3\n"}), [])

    def test_pin_only_steps_run_on(self):
        self.assertEqual(Repo(self).change({LOCK: self.lock_with("golang", "golang:1.27")}), STEPS)
        self.assertEqual(Repo(self).change({LOCK: self.lock_with("oapi-codegen", "x@v2.6.0")}), STEPS)

    def test_pin_every_suite_runs_on(self):
        self.assertEqual(Repo(self).change({LOCK: self.lock_with("nginx", "nginx:1.30")}), ALL)

    def test_removed_declared_pin(self):
        lock = "".join(l + "\n" for l in LOCK_TEXT.splitlines() if not l.startswith("golang "))
        self.assertEqual(Repo(self).change({LOCK: lock}), STEPS)

    def test_pin_change_with_an_unrelated_file(self):
        got = Repo(self).change({LOCK: self.lock_with("gvisor-release", "1"), "docs/index.md": "x\n"})
        self.assertEqual(got, [])


class AllSuitesTest(unittest.TestCase):
    """Every suite runs when the push's start is unknown and when started by hand."""

    def test_before_all_zeros(self):
        r = Repo(self)
        self.assertEqual(r.select(ZEROS, r.commit({"docs/index.md": "x\n"})), ALL)

    def test_before_empty(self):
        r = Repo(self)
        self.assertEqual(r.select("", r.commit({"docs/index.md": "x\n"})), ALL)

    def test_before_not_in_history(self):
        r = Repo(self)
        self.assertEqual(r.select("a" * 40, r.commit({"docs/index.md": "x\n"})), ALL)

    def test_workflow_dispatch(self):
        r = Repo(self)
        sha = r.commit({"docs/index.md": "x\n"})
        # Since T120 a dispatch selects as a push does.
        self.assertEqual(r.select(r.base, sha, event="workflow_dispatch"), [])
        self.assertEqual(r.select("", sha, event="workflow_dispatch"), ALL)

    def test_planted_known_before_with_no_suite_input_chooses_none(self):
        # The counter-case of the rules above: a resolvable start and a push event.
        r = Repo(self)
        self.assertEqual(r.select(r.base, r.commit({"docs/index.md": "x\n"}), event="push"), [])

    def test_before_several_commits_back(self):
        r = Repo(self)
        r.commit({"src/agents/review/instruction.md": "x\n"})
        sha = r.commit({"docs/index.md": "x\n"})
        self.assertEqual(r.select(r.base, sha), [("steps", "review")])


class UnrelatedTest(unittest.TestCase):
    def test_planted_unrelated_files_choose_none(self):
        self.assertEqual(Repo(self).change({"README.md": "x\n", "docs/new.md": "x\n",
                                            "src/engine/store/store.go": "x\n"}), [])

    def test_empty_push_chooses_none(self):
        self.assertEqual(Repo(self).change(), [])

    def test_sha_from_env_names_the_end(self):
        # The selector diffs BEFORE..SHA from the environment, not BEFORE..HEAD: a SHA that names
        # the base gives no change even though HEAD has one.
        r = Repo(self)
        r.commit({"src/agents/review/instruction.md": "x\n"})
        self.assertEqual(r.select(r.base, r.base), [])


class RealDeclarationTest(unittest.TestCase):
    """T115's done criteria on the real declaration and the real images.lock."""

    def setUp(self):
        import yaml
        self.decl_path = REPO / DECL
        self.assertTrue(self.decl_path.exists(), f"{DECL} does not exist")
        self.decl = yaml.safe_load(self.decl_path.read_text())
        self.lock = (REPO / LOCK).read_text()
        self.r = Repo(self, self.decl_path.read_text(), self.lock)

    def test_review_instruction_chooses_only_review(self):
        self.assertEqual(self.r.change({"src/agents/review/instruction.md": "x\n"}),
                         [("steps", "review")])

    def test_pin_no_suite_runs_on_chooses_none(self):
        declared = {p for s in self.decl["suites"] for p in s.get("pins") or []}
        names = [l.split()[0] for l in self.lock.splitlines()
                 if l.strip() and not l.startswith("#") and len(l.split()) >= 2]
        free = [n for n in names if n not in declared]
        self.assertTrue(free, "every images.lock pin is declared by a suite")
        lock = "\n".join(f"{l.split()[0]} changed-{l.split()[1]}" if l.split()[:1] == [free[0]]
                         else l for l in self.lock.splitlines()) + "\n"
        self.assertEqual(self.r.change({LOCK: lock}), [])

    def test_planted_declared_pin_chooses_its_suites(self):
        s = self.decl["suites"][0]
        name = s["pins"][0]
        lock = "\n".join(f"{name} changed" if l.split()[:1] == [name] else l
                         for l in self.lock.splitlines()) + "\n"
        self.assertIn((s["suite"], s.get("cases") or ""), self.r.change({LOCK: lock}))


if __name__ == "__main__":
    unittest.main()
