"""scripts/secret-scan.sh fails closed on what it cannot read and follows links (T122 fresh review;
research D46).

The script's contract (its header and test_workflows_eval_artifacts.py): with DIR it scans every
file under DIR, exit 1 on a match naming only the files, never the matched text. These tests fix
the rest of it:

- a file grep cannot read (mode 000) leaves the scan not clean: exit 2, and nothing of the file's
  content is printed; a match elsewhere still wins (exit 1);
- a DIR that does not exist passes (exit 0) with a "nothing to scan" line: a job that failed
  before writing its report has nothing to upload;
- links under DIR are followed, as actions/upload-artifact follows them: a link to a file outside
  DIR that holds a key fails (exit 1) naming the link; a link to a clean file passes;
- a dangling link is a file the scan could not read, so the scan is not clean: exit 2 (the
  script's rule "grep: 1 is no match; anything else is a file it could not read"). Failing
  closed costs only the artifact of that job; passing could let an upload follow a link the scan
  never judged.

Test-only seam: SECRET_SCAN_UNDER_TEST, when set, names the script these tests run instead of
scripts/secret-scan.sh (used once to show the tests failing on the script before the fix). The
script itself has no seam. Fixture keys are built at run time by concatenation.
"""
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

from gitenv import clean_env, make_home

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
SCAN = Path(os.environ.get("SECRET_SCAN_UNDER_TEST") or REPO / "scripts" / "secret-scan.sh")
KEY = "sk-" + "ant-" + "api03-" + "R" * 26 + "_y-7"
SECRET_TEXT = "sk-" + "ant-" + "unreadable-" + "S" * 24
CLEAN = "Feature: notes\n  Rule: R-1\n"


class ScanErrorsTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.base = Path(tmp.name)
        self.dir = self.base / "eval-report"
        self.dir.mkdir()
        (self.dir / "report.json").write_text('{"cases": []}\n')

    def scan(self, target=None):
        self.assertTrue(SCAN.exists(), f"{SCAN} does not exist")
        r = subprocess.run(["sh", str(SCAN), str(target or self.dir)], cwd=REPO,
                           env=clean_env(make_home(self)), capture_output=True, text=True,
                           timeout=60)
        out = r.stdout + r.stderr
        print(f"secret-scan (exit {r.returncode}): {out.strip()}")
        return r.returncode, out

    def unreadable(self, name, text):
        p = self.dir / name
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(text)
        p.chmod(0)
        self.addCleanup(p.chmod, 0o600)
        if os.geteuid() == 0:
            self.skipTest("running as root: mode 000 does not stop root from reading the file")
        return p

    def test_unreadable_file_fails_with_exit_2(self):
        self.unreadable("steps/trial-1.out", SECRET_TEXT + "\n")
        code, out = self.scan()
        self.assertEqual(code, 2, out)
        self.assertNotIn(SECRET_TEXT, out)
        self.assertNotIn(SECRET_TEXT[:16], out)

    def test_unreadable_clean_file_fails_with_exit_2(self):
        self.unreadable("trial-2.out", CLEAN)
        code, out = self.scan()
        self.assertEqual(code, 2, out)

    def test_match_beside_an_unreadable_file_fails_with_exit_1(self):
        self.unreadable("trial-2.out", CLEAN)
        (self.dir / "trial-3.out").write_text(KEY + "\n")
        code, out = self.scan()
        self.assertEqual(code, 1, out)
        self.assertIn("trial-3.out", out)
        self.assertNotIn(KEY, out)

    def test_planted_readable_clean_folder_passes(self):
        # The counter-case of the unreadable cases: the same files, readable.
        (self.dir / "trial-2.out").write_text(CLEAN)
        self.assertEqual(self.scan()[0], 0)

    def test_missing_dir_passes_with_nothing_to_scan(self):
        code, out = self.scan(self.base / "no-such-report")
        self.assertEqual(code, 0, out)
        self.assertIn("nothing to scan", out)

    def test_link_to_a_key_outside_fails_naming_the_link(self):
        outside = self.base / "outside.txt"
        outside.write_text("token " + KEY + "\n")
        (self.dir / "linked.ndjson").symlink_to(outside)
        code, out = self.scan()
        self.assertEqual(code, 1, out)
        self.assertIn("linked.ndjson", out)
        self.assertNotIn(KEY, out)

    def test_linked_folder_with_a_key_fails(self):
        other = self.base / "other"
        other.mkdir()
        (other / "k.txt").write_text(KEY + "\n")
        (self.dir / "sub").symlink_to(other, target_is_directory=True)
        code, out = self.scan()
        self.assertEqual(code, 1, out)
        self.assertNotIn(KEY, out)

    def test_planted_link_to_a_clean_file_passes(self):
        outside = self.base / "outside.txt"
        outside.write_text(CLEAN)
        (self.dir / "linked.ndjson").symlink_to(outside)
        code, out = self.scan()
        self.assertEqual(code, 0, out)

    def test_dangling_link_fails_with_exit_2(self):
        (self.dir / "gone.ndjson").symlink_to(self.base / "removed.txt")
        code, out = self.scan()
        self.assertEqual(code, 2, out)


if __name__ == "__main__":
    unittest.main()
