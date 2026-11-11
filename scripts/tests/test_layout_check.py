import re
import subprocess
import sys
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
CHECK = REPO / "scripts" / "layout-check.py"
FIXTURES = HERE / "layout"


def run_check(root=None):
    cmd = [sys.executable, str(CHECK)]
    if root is not None:
        cmd.append(str(root))
    return subprocess.run(cmd, capture_output=True, text=True, timeout=60)


class LayoutCheckTest(unittest.TestCase):
    def test_clean_fixture_passes(self):
        root = FIXTURES / "clean"
        r = run_check(root)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(r.stderr, "")
        first = r.stdout.splitlines()[0]
        self.assertRegex(first, r"^layout-check: examined \d+ entries in ")
        self.assertIn(str(root), first)
        self.assertGreater(int(re.search(r"examined (\d+)", first).group(1)), 0)

    def test_planted_fixture_fails_with_exact_violations(self):
        r = run_check(FIXTURES / "planted")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        lines = [l.strip() for l in r.stdout.splitlines() if l.strip()]
        self.assertTrue(lines[0].startswith("layout-check: examined "), lines)
        reported = set(lines[1:])
        self.assertEqual(reported, {"stray.txt", "node_modules", "backend"})
        for allowed in ("src", "docs", "specs", "scripts", ".specify",
                        "README.md", "LICENSE", "AGENTS.md", ".gitignore"):
            self.assertNotIn(allowed, reported)

    def test_real_repository_root_passes(self):
        r = run_check(REPO)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(r.stderr, "")


if __name__ == "__main__":
    unittest.main()
