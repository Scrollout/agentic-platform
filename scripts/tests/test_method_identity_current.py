"""T003 (feature 006, research R2): the committed method identity is current.

Runs `python3 scripts/method-digest.py --check` on this repository itself (src/engine/method/README.md,
"The command") and fails, quoting the script's standard output and standard error, unless it exits 0.
So the CI run of a commit whose `src/engine/method/method.sha256` is stale, missing, or whose
declaration is bad, fails. The identity the script examined is printed (its `method: <identity> ...`
line) so the run shows what was checked.

Its planted counterpart, proving this check can fail, is the stale-file case of
scripts/tests/test_method_digest.py.
"""
import os
import subprocess
import sys
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent.parent
SCRIPT = REPO / "scripts" / "method-digest.py"


def clean_env():
    # A hook exports GIT_DIR, GIT_INDEX_FILE and friends; the script must see the repository from
    # its cwd, so they are dropped. HOME stays real: the script only reads the work tree.
    return {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}


class MethodIdentityCurrent(unittest.TestCase):
    def test_committed_identity_matches_the_sources(self):
        self.assertTrue(SCRIPT.is_file(),
                        "scripts/method-digest.py does not exist (research R2): %s" % SCRIPT)
        try:
            done = subprocess.run([sys.executable, str(SCRIPT), "--check"], cwd=str(REPO),
                                  env=clean_env(), capture_output=True, text=True, timeout=120)
        except (OSError, subprocess.TimeoutExpired) as e:
            self.fail("could not run scripts/method-digest.py --check: %r" % (e,))
        print("method identity examined: %s" % (done.stdout.strip() or "(no output)"),
              file=sys.stderr)
        self.assertEqual(
            done.returncode, 0,
            "scripts/method-digest.py --check exited %d; run scripts/method-digest.py and commit "
            "src/engine/method/method.sha256\nstdout:\n%s\nstderr:\n%s"
            % (done.returncode, done.stdout, done.stderr))
        self.assertIn("method: ", done.stdout, "--check printed no identity line:\n%s" % done.stdout)


if __name__ == "__main__":
    unittest.main()
