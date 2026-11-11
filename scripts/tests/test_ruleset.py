import shutil
import subprocess
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent.parent
DATA = "src/engine/api/testdata/ruleset"
RULESET = "src/engine/api/ruleset.yaml"
PLANTED = [
    "planted-no-operation-id.yaml",
    "planted-no-description.yaml",
    "planted-no-api-version.yaml",
    "planted-list-without-page-token.yaml",
    "planted-problem-without-code.yaml",
]


@unittest.skipUnless(shutil.which("npx"), "npx not on PATH; cannot run Spectral")
class RulesetTest(unittest.TestCase):
    def lint(self, name):
        return subprocess.run(
            ["npx", "-y", "@stoplight/spectral-cli@6.15.0", "lint",
             f"{DATA}/{name}", "--ruleset", RULESET,
             "--fail-severity", "error"],
            cwd=REPO, capture_output=True, text=True, timeout=300)

    def test_clean_passes(self):
        r = self.lint("clean.yaml")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_planted_files_fail(self):
        for name in PLANTED:
            with self.subTest(name=name):
                r = self.lint(name)
                self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
                # Spectral exits 1 for lint errors; other codes (e.g. 2) mean
                # the tool itself failed, which must not count as detection.
                self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
                self.assertNotIn("Error running Spectral", r.stdout + r.stderr)


if __name__ == "__main__":
    unittest.main()
