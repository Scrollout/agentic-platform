"""T024: a full `scripts/check.sh` run skips the `e2e` step and says so; `--only e2e` runs it.

Judged in a throwaway copy of the repository: check.sh (the real one, or a planted copy), a stub
scripts/e2e.sh that leaves a marker file, and every other step's function replaced by a no-op
(injected before the step loop), so nothing heavy runs.

The planted violation is a check.sh copy with the skip line removed: a full run then runs e2e and
prints no skip line, which the judge must fail.
"""
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
# AP_TEST_CHECK points the tests at another copy of check.sh (to see them fail against a mutant)
CHECK = Path(os.environ.get("AP_TEST_CHECK") or REPO / "scripts/check.sh")
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
SKIP = "== e2e: runs in CI; here only with scripts/check.sh --only e2e"
STUB_ALL = ('for _s in $(echo "$STEP_TABLE" | cut -d" " -f1); do\n'
            '  [ "$_s" = e2e ] || eval "s_$(echo "$_s" | tr - _)() { :; }"\n'
            'done\n')


def prepare(base, check_text):
    repo = base / "repo"
    (repo / "scripts").mkdir(parents=True)
    (repo / "src/deploy").mkdir(parents=True)
    shutil.copy(REPO / "src/deploy/images.lock", repo / "src/deploy/images.lock")
    assert "\nstart=$(now)\n" in check_text, "check.sh has no 'start=$(now)' line to stub the steps before"
    text = check_text.replace("\nstart=$(now)\n", "\n" + STUB_ALL + "start=$(now)\n", 1)
    (repo / "scripts/check.sh").write_text(text)
    (repo / "scripts/e2e.sh").write_text('#!/bin/sh\necho e2e-ran >"$(dirname "$0")/../e2e.marker"\n')
    for f in ("check.sh", "e2e.sh"):
        os.chmod(repo / "scripts" / f, 0o755)
    return repo


def run(repo, base, *args):
    env = {**os.environ, "HOME": str(base / "home"), "XDG_CACHE_HOME": str(base / "cache")}
    env.pop("AP_TEST_REPORT_DIR", None)
    (repo / "e2e.marker").unlink(missing_ok=True)
    r = subprocess.run([str(repo / "scripts/check.sh"), *args], cwd=repo, env=env, capture_output=True,
                       text=True, timeout=60)
    return r, (repo / "e2e.marker").exists()


def judge_full_run_skips(r, ran):
    v = []
    if r.returncode != 0:
        v.append("full run failed (%d): %s" % (r.returncode, r.stdout + r.stderr))
    if ran:
        v.append("a full run ran scripts/e2e.sh")
    if SKIP not in r.stdout.splitlines():
        v.append("a full run did not print the skip line")
    return v


def judge_only_runs(r, ran):
    v = []
    if r.returncode != 0:
        v.append("--only e2e failed (%d): %s" % (r.returncode, r.stdout + r.stderr))
    if not ran:
        v.append("--only e2e did not run scripts/e2e.sh")
    if SKIP in r.stdout:
        v.append("--only e2e printed the skip line")
    return v


class E2eSkipTest(unittest.TestCase):
    def setUp(self):
        self.base = Path(tempfile.mkdtemp(prefix="test-check-e2e-", dir=TMP_BASE))
        self.addCleanup(shutil.rmtree, self.base, ignore_errors=True)

    def test_full_run_skips_e2e(self):
        repo = prepare(self.base, CHECK.read_text())
        self.assertEqual(judge_full_run_skips(*run(repo, self.base)), [])

    def test_only_e2e_runs_it(self):
        repo = prepare(self.base, CHECK.read_text())
        self.assertEqual(judge_only_runs(*run(repo, self.base, "--only", "e2e")), [])

    def test_planted_check_without_the_skip_fails(self):
        lines = CHECK.read_text().splitlines(keepends=True)
        kept = [l for l in lines if 'step" = e2e ]' not in l or "continue" not in l]
        self.assertEqual(len(kept), len(lines) - 1, "the skip line was not found exactly once")
        repo = prepare(self.base, "".join(kept))
        v = judge_full_run_skips(*run(repo, self.base))
        self.assertTrue(any("ran scripts/e2e.sh" in x for x in v), v)
        self.assertTrue(any("skip line" in x for x in v), v)


if __name__ == "__main__":
    unittest.main()
