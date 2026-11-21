"""T041: the pre-commit hook lints the staged changes, each stack with its own pinned linter.

For each linter (golangci-lint with gosec, Ruff, ShellCheck, Hadolint, actionlint, zizmor) a real
commit in a clone proves (Checks prove they can fail):
  (a) a clean staged change commits;
  (b) a planted violation on an added or changed line is refused, and the output names the linter
      and `<path>:<line>` (`<linter>: <path>:<line>: <text>`);
  (c) a violation already on an untouched line (committed with --no-verify) does not stop a clean
      change elsewhere in that file: a changed tail, a changed line right before it, a deletion
      of the line before it (nothing added), a pure rename, and the deletion of the whole file.
Also: a linter that cannot run (no pin in src/deploy/images.lock) refuses the commit and says it
could not run; a commit with no file for a linter does not run it; the output reports which
linters ran on how many files; only the staged snapshot is judged; odd names; two Go modules.

The clone harness is test_pre_commit.CloneCase, unchanged (clone, hook via core.hooksPath, real
commits). The clone is of the committed repository, so setUp copies the working tree's lint step
(scripts/lint-staged.py, scripts/lint-staged.yaml) and src/deploy/images.lock (the pins) into the
clone before the tests commit. Docker and the pinned linter images are prerequisites: a missing
Docker fails, never skips. Each hook run starts linter containers, so one test walks several
commits in one clone.
"""
import os
import re
import shutil
import subprocess
import unittest

import test_pre_commit as base

REPO = base.REPO
LINT_FILES = ("scripts/lint-staged.py", "scripts/lint-staged.yaml", "src/deploy/images.lock")
LINTERS = ("golangci-lint", "ruff", "shellcheck", "hadolint", "actionlint", "zizmor")
HOOK_TIMEOUT = 600

WORKFLOW_HEAD = ["name: zz", "on: push", "permissions: {}", "jobs:", "  zz:",
                 "    runs-on: ubuntu-latest", "    steps:"]


class Spec:
    """A file of head lines, a comment line, the line under test, and a tail comment.

    The line under test is line len(head) + 2; build() swaps it between its clean and its
    violating form without moving any other line, so a finding keeps its line number.
    """

    def __init__(self, linter, path, head, comment, clean, viol, alt):
        self.linter, self.path, self.head, self.comment = linter, path, head, comment
        self.clean, self.viol, self.alt = clean, viol, alt
        self.line = len(head) + 2

    def build(self, viol=False, comment="v1", tail="v1", drop_comment=False):
        c = self.comment.replace("v1", comment)
        indent = self.comment[:len(self.comment) - len(self.comment.lstrip())]
        mark = "//" if self.comment.startswith("//") else "#"
        t = "%s%s tail %s" % (indent, mark, tail)
        lines = list(self.head) + ([] if drop_comment else [c]) + \
            [self.viol if viol else self.clean] + (["", t] if mark == "//" else [t])
        return "\n".join(lines) + "\n"


SPECS = {
    "go": Spec("golangci-lint", "src/slice/zzplanted/zz.go",
               ["package zzplanted", "", 'import "os"', ""], "// note v1",
               'func Bad() error { return os.Remove("b") }', 'func Bad() { os.Remove("b") }',
               "src/slice/zzplanted/zz2.go"),
    "python": Spec("ruff", "src/agents/zz.py", ['"""Planted."""', ""], "# note v1",
                   "X = 1", "import os", "src/agents/zz2.py"),
    "shell": Spec("shellcheck", "src/agents/zz.sh", ["#!/bin/sh"], "# note v1",
                  'echo "$1"', "echo $1", "src/agents/zz2.sh"),
    "shebang": Spec("shellcheck", "src/agents/zzrun", ["#!/bin/sh"], "# note v1",
                    'echo "$1"', "echo $1", "src/agents/zzrun2"),
    "dockerfile": Spec("hadolint", "src/deploy/Dockerfile.zz", ["FROM alpine:3.22"], "# note v1",
                       "RUN true", "RUN cd /x && true", "src/deploy/Dockerfile.zz2"),
    "actionlint": Spec("actionlint", ".github/workflows/zz-actionlint.yml", WORKFLOW_HEAD,
                       "      # note v1", "      - run: echo hi",
                       "      - run: echo ${{ matrix.x }}", ".github/workflows/zz-actionlint2.yml"),
    "zizmor": Spec("zizmor", ".github/workflows/zz-zizmor.yml", WORKFLOW_HEAD,
                   "      # note v1", "      - run: echo ok",
                   "      - uses: actions/checkout@v4", ".github/workflows/zz-zizmor2.yml"),
}


class LintCase(base.CloneCase):
    """A clone that also holds the working tree's lint step and pins."""

    def setUp(self):
        super().setUp()
        # the hook's go vet step needs the user's module cache, as in test_pre_commit_quick
        self.env["XDG_CACHE_HOME"] = os.environ.get("XDG_CACHE_HOME") or os.path.join(
            os.path.expanduser("~"), ".cache")
        for rel in LINT_FILES:
            src = os.path.join(REPO, rel)
            if os.path.exists(src):
                dst = os.path.join(self.clone, rel)
                os.makedirs(os.path.dirname(dst), exist_ok=True)
                shutil.copyfile(src, dst)
                self.git("add", "--", rel)
        self.git("commit", "-q", "--no-verify", "--allow-empty", "-m", "test setup: lint step")

    def commit_ok(self, *args):
        try:
            return subprocess.run(
                ["git", "commit", "-q", "-m", "test commit", "-m", base.APPROVAL, *args],
                cwd=self.clone, env=self.env, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                text=True, timeout=HOOK_TIMEOUT)
        except subprocess.TimeoutExpired:
            self.fail("the commit took longer than %d s" % HOOK_TIMEOUT)

    def accepted(self, what):
        r = self.commit_ok()
        self.assertEqual(r.returncode, 0, "%s was refused: %s" % (what, self.out(r)))
        return r

    def refused(self, what, linter, path, line):
        before = self.head()
        r = self.commit_ok()
        self.assertNotEqual(r.returncode, 0, "%s was accepted: %s" % (what, self.out(r)))
        self.assertIn("%s: %s:%d" % (linter, path, line), self.out(r),
                      "%s: the output does not name the finding" % what)
        self.assertEqual(self.head(), before)
        return r

    def restore(self):
        self.git("reset", "-q", "--hard", "HEAD")
        self.git("clean", "-fdq")

    def no_finding_on(self, r, spec, line=None):
        self.assertNotIn("%s: %s:%d" % (spec.linter, spec.path, line or spec.line), self.out(r))

    def ran(self, out, linter, files):
        """The output names the linter on a line that also gives its file count."""
        pat = r"(?im)^.*\b%s\b.*\b%d\b.*\bfiles?\b.*$|^.*\b%d\b.*\bfiles?\b.*\b%s\b.*$" % (
            re.escape(linter), files, files, re.escape(linter))
        self.assertRegex(out, pat, "the output does not say %s ran on %d file(s)" % (linter, files))


class PrerequisitesTest(unittest.TestCase):
    def test_docker_is_installed_and_reachable(self):
        self.assertIsNotNone(shutil.which("docker"), "docker is required and missing")
        r = subprocess.run(["docker", "version", "--format", "{{.Server.Version}}"],
                           stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.assertEqual(r.returncode, 0, "docker daemon not reachable: " + r.stderr)

    def test_the_lint_step_and_its_pins_exist(self):
        for rel in LINT_FILES[:2]:
            self.assertTrue(os.path.exists(os.path.join(REPO, rel)), rel + " does not exist")
        with open(os.path.join(REPO, "src/deploy/images.lock")) as f:
            lock = f.read()
        for name in LINTERS:
            self.assertRegex(lock, r"(?m)^%s\s+\S+" % name, "no pin for " + name)


class PlantedTest(LintCase):
    """(a), (b) and (f) for each linter; the tests are added below, one per linter."""


class UntouchedTest(LintCase):
    """(c) for each linter, with deletions, renames and the line after a changed hunk."""


def add_tests():
    for key, spec in SPECS.items():
        def planted(self, spec=spec):
            # (a) a clean added file commits, and the output reports what was examined (f)
            self.stage(spec.path, spec.build())
            r = self.accepted("a clean added %s file" % spec.linter)
            self.ran(self.out(r), spec.linter, 1)
            # (b) a violation on a changed line of a tracked file is refused
            self.stage(spec.path, spec.build(viol=True))
            self.refused("a planted %s violation on a changed line" % spec.linter,
                         spec.linter, spec.path, spec.line)
            self.restore()
            # (b) a violation in an added file is refused too
            # distinct declarations: the added Go file shares a package with the first
            self.stage(spec.alt, spec.build(viol=True).replace("Bad", "Bad2"))
            self.refused("a planted %s violation in an added file" % spec.linter,
                         spec.linter, spec.alt, spec.line)
            self.restore()
            # the same file, clean again, commits (the refusals were about the violation)
            self.stage(spec.path, spec.build(tail="v2"))
            self.accepted("the clean file again")

        def untouched(self, spec=spec):
            self.raw_commit(spec.path, spec.build(viol=True))  # (c) the old finding
            # the tail changed: the finding is not kept
            self.stage(spec.path, spec.build(viol=True, tail="v2"))
            r = self.accepted("a change of the tail")
            self.ran(self.out(r), spec.linter, 1)  # it did examine the file
            self.no_finding_on(r, spec)
            # the line right before the finding changed: the finding is on the line right after
            # the changed hunk and is not kept
            self.stage(spec.path, spec.build(viol=True, tail="v2", comment="v2"))
            self.no_finding_on(self.accepted("a change right before the finding"), spec)
            # a deletion only: nothing is added, so no finding is kept (the line moved up by one)
            self.stage(spec.path, spec.build(viol=True, tail="v2", drop_comment=True))
            r = self.accepted("a deletion of the line before the finding")
            self.no_finding_on(r, spec)
            self.no_finding_on(r, spec, spec.line - 1)
            # a pure rename: no line changed
            self.git("mv", spec.path, spec.alt)
            self.accepted("a pure rename")
            # a staged deletion of the whole file: nothing to lint
            self.git("rm", "-q", spec.alt)
            r = self.accepted("the deletion of a file")
            self.assertNotIn("%s: " % spec.linter, self.out(r))

        setattr(PlantedTest, "test_%s_clean_commits_and_planted_is_refused" % key, planted)
        setattr(UntouchedTest, "test_%s_finding_on_an_untouched_line_is_not_kept" % key, untouched)


add_tests()


class RuffSettingsTest(LintCase):
    """Ruff selects E, F, W, B, S; E501 is ignored; under scripts/tests S101, S603, S607 are."""

    def test_the_selected_rules_find_their_violations(self):
        cases = {
            "src/agents/zz_s101.py": ("S101", "assert 1 == 1\n"),
            "src/agents/zz_s307.py": ("S307", "eval('1')\n"),
            "src/agents/zz_e711.py": ("E711", "X = None\nY = X == None\n"),
            "src/agents/zz_b006.py": ("B006", "def f(a=[]):\n    return a\n"),
        }
        for path, (_, text) in cases.items():
            self.stage(path, text)
        r = self.commit_ok()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        for path, (code, _) in cases.items():
            self.assertRegex(self.out(r), r"ruff: %s:\d+.*%s" % (re.escape(path), code),
                             "%s not reported for %s" % (code, path))

    def test_a_long_line_is_not_a_finding_e501_is_ignored(self):
        self.stage("src/agents/zz_long.py", "X = '%s'\n" % ("x" * 200))
        self.ran(self.out(self.accepted("a long line")), "ruff", 1)

    def test_assert_and_subprocess_are_not_findings_under_scripts_tests(self):
        self.stage("scripts/tests/test_zz_style.py",
                   "import subprocess\nimport unittest\n\n\nclass T(unittest.TestCase):\n"
                   "    def test_ok(self):\n        assert True\n"
                   "        subprocess.run(['ls'], check=True)\n")
        self.ran(self.out(self.accepted("assert and subprocess in a test")), "ruff", 1)

    def test_the_same_code_outside_scripts_tests_is_a_finding(self):
        self.stage("src/agents/zz_sub.py",
                   "import subprocess\n\nsubprocess.run(['ls'], check=True)\n")
        r = self.commit_ok()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertRegex(self.out(r), r"ruff: src/agents/zz_sub\.py:3")


class SnapshotTest(LintCase):
    """Only what is staged is judged."""

    def test_only_the_staged_form_counts(self):
        spec = SPECS["python"]
        self.raw_commit(spec.path, spec.build())
        # staged violation, clean working copy: refused
        self.stage(spec.path, spec.build(viol=True))
        self.write(spec.path, spec.build())
        self.refused("a staged violation", spec.linter, spec.path, spec.line)
        self.restore()
        # clean staged change, violation only in the working copy: commits
        self.stage(spec.path, spec.build(tail="v2"))
        self.write(spec.path, spec.build(viol=True, tail="v2"))
        r = self.accepted("a clean staged change with an unstaged violation")
        self.no_finding_on(r, spec)

    def test_an_untracked_or_unstaged_file_is_not_judged(self):
        self.write("src/agents/zz_untracked.py", "import os\n")
        self.write(SPECS["shell"].path, SPECS["shell"].build(viol=True))
        self.stage("src/agents/zz_ok.py", "X = 1\n")
        r = self.accepted("a clean change next to untracked violations")
        self.assertNotIn("zz_untracked", self.out(r))
        self.assertNotIn("shellcheck: ", self.out(r))


class NamesTest(LintCase):
    def test_a_file_name_with_a_space_is_linted(self):
        path = "src/agents/my file.py"
        self.stage(path, "import os\n")
        r = self.commit_ok()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("ruff: %s:1" % path, self.out(r))

    def test_a_clean_file_name_with_a_space_commits(self):
        self.stage("src/agents/my file.py", "X = 1\n")
        self.ran(self.out(self.accepted("a clean file with a space in its name")), "ruff", 1)

    def test_a_renamed_file_with_a_new_violation_is_refused_under_its_new_name(self):
        spec = SPECS["python"]
        self.raw_commit(spec.path, spec.build())
        self.git("mv", spec.path, "src/agents/zz renamed.py")
        self.write("src/agents/zz renamed.py", spec.build(viol=True))
        self.git("add", "--", "src/agents/zz renamed.py")
        self.refused("a renamed file with a new violation", spec.linter,
                     "src/agents/zz renamed.py", spec.line)


class ShellSelectionTest(LintCase):
    def test_an_extensionless_file_without_a_shell_shebang_is_not_shell_linted(self):
        self.stage("src/agents/zznotes", "echo $1\n")
        r = self.accepted("an extensionless file with no shebang")
        self.assertNotIn("shellcheck: ", self.out(r))

    def test_a_bash_shebang_is_shell_linted(self):
        self.stage("src/agents/zzbash", "#!/usr/bin/env bash\necho $1\n")
        r = self.commit_ok()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("shellcheck: src/agents/zzbash:2", self.out(r))


class GoModulesTest(LintCase):
    def test_two_modules_changed_at_once_are_both_judged(self):
        spec = SPECS["go"]
        a, b = "src/slice/zzplanted/zz.go", "src/engine/zzplanted/zz.go"
        self.stage(a, spec.build(viol=True))
        self.stage(b, spec.build(viol=True))
        r = self.commit_ok()
        self.assertNotEqual(r.returncode, 0, self.out(r))
        self.assertIn("golangci-lint: %s:%d" % (a, spec.line), self.out(r))
        self.assertIn("golangci-lint: %s:%d" % (b, spec.line), self.out(r))

    def test_two_clean_modules_commit_and_the_output_counts_both_files(self):
        spec = SPECS["go"]
        self.stage("src/slice/zzplanted/zz.go", spec.build())
        self.stage("src/engine/zzplanted/zz.go", spec.build())
        r = self.accepted("two clean Go modules")
        self.ran(self.out(r), "golangci-lint", 2)

    def test_a_violation_in_an_untouched_module_is_not_judged(self):
        spec = SPECS["go"]
        self.raw_commit("src/engine/zzplanted/zz.go", spec.build(viol=True))
        self.stage("src/slice/zzplanted/zz.go", spec.build())
        r = self.accepted("a clean change in another module")
        self.assertNotIn("src/engine/zzplanted/zz.go", self.out(r))


class CannotRunTest(LintCase):
    def drop_pin(self, name):
        path = os.path.join(self.clone, "src/deploy/images.lock")
        with open(path) as f:
            lines = f.read().splitlines(keepends=True)
        kept = [ln for ln in lines if not re.match(r"%s\s" % re.escape(name), ln)]
        self.assertNotEqual(len(kept), len(lines), "images.lock has no pin for %s" % name)
        with open(path, "w") as f:
            f.writelines(kept)
        self.git("add", "--", "src/deploy/images.lock")
        self.git("commit", "-q", "--no-verify", "-m", "setup: no %s pin" % name)

    def test_a_linter_without_a_pin_refuses_the_commit_and_says_so(self):
        self.drop_pin("ruff")
        self.stage("src/agents/zz_ok.py", "X = 1\n")
        before = self.head()
        r = self.commit_ok()
        self.assertNotEqual(r.returncode, 0, "a linter that cannot run was skipped: " + self.out(r))
        self.assertRegex(self.out(r), r"(?i)could not run")
        self.assertRegex(self.out(r), r"(?i)ruff")
        self.assertEqual(self.head(), before)

    def test_a_missing_pin_of_a_linter_with_no_staged_file_does_not_matter(self):
        self.drop_pin("shellcheck")
        self.stage("src/agents/zz_ok.py", "X = 1\n")
        r = self.accepted("a python change without the shellcheck pin")
        self.ran(self.out(r), "ruff", 1)


class SelectionTest(LintCase):
    def others(self, out, *used):
        return [n for n in LINTERS if n not in used and re.search(r"\b%s\b" % n, out)]

    def test_a_python_commit_runs_only_ruff(self):
        self.stage("src/agents/zz_ok.py", "X = 1\n")
        out = self.out(self.accepted("a python change"))
        self.ran(out, "ruff", 1)
        self.assertEqual(self.others(out, "ruff"), [], out)

    def test_a_shell_and_a_dockerfile_commit_runs_exactly_those_two(self):
        self.stage(SPECS["shell"].path, SPECS["shell"].build())
        self.stage(SPECS["dockerfile"].path, SPECS["dockerfile"].build())
        out = self.out(self.accepted("a shell and a Dockerfile change"))
        self.ran(out, "shellcheck", 1)
        self.ran(out, "hadolint", 1)
        self.assertEqual(self.others(out, "shellcheck", "hadolint"), [], out)

    def test_a_workflow_commit_runs_actionlint_and_zizmor_only(self):
        self.stage(SPECS["actionlint"].path, SPECS["actionlint"].build())
        out = self.out(self.accepted("a workflow change"))
        self.ran(out, "actionlint", 1)
        self.ran(out, "zizmor", 1)
        self.assertEqual(self.others(out, "actionlint", "zizmor"), [], out)

    def test_a_commit_with_no_file_for_any_linter_runs_none(self):
        self.stage("src/agents/notes.txt", "a note\n")
        out = self.out(self.accepted("a text change"))
        self.assertEqual(self.others(out), [], out)

    def test_a_deletion_only_commit_runs_no_linter(self):
        self.raw_commit(SPECS["python"].path, SPECS["python"].build(viol=True))
        self.git("rm", "-q", SPECS["python"].path)
        out = self.out(self.accepted("the deletion of a python file"))
        self.assertNotRegex(out, r"ruff: ")

    def test_the_file_count_is_the_staged_files_of_that_linter(self):
        for n in range(3):
            self.stage("src/agents/zz_%d.py" % n, "X = %d\n" % n)
        self.stage("src/agents/notes.txt", "a note\n")
        self.ran(self.out(self.accepted("three python files")), "ruff", 3)


if __name__ == "__main__":
    unittest.main()
