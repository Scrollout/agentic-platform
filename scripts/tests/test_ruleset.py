import json
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


XL = "scripts/tests/fixtures/ruleset-x-limits"
CONTRACT = "src/engine/api/preview/2026-11-12-preview/openapi.yaml"
KEYS = [
    "requests-per-second-per-token",
    "requests-per-second-per-address-without-token",
    "idempotency-key-retention-hours",
    "request-body-max-bytes",
]
# fixture file -> text the x-limits error must name (the key, or x-limits itself)
XLIMITS_PLANTED = {
    "planted-x-limits-missing.yaml": "x-limits",
    "planted-x-limits-not-object.yaml": "x-limits",
    **{f"planted-missing-{k}.yaml": k for k in KEYS},
    "planted-string-value.yaml": KEYS[0],
    "planted-float-value.yaml": KEYS[2],
    "planted-zero-value.yaml": KEYS[3],
    "planted-negative-value.yaml": KEYS[1],
}


class RulesetTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # A missing prerequisite is a failure, never a skip (constitution, T010c).
        if not shutil.which("docker"):
            raise AssertionError(
                "docker is not on PATH: test_ruleset needs it to run Spectral through "
                "scripts/npm-tool.sh; install Docker (a missing prerequisite fails, it does not skip)")

    def lint(self, name):
        return subprocess.run(
            ["scripts/npm-tool.sh", "spectral", "lint",
             f"{DATA}/{name}", "--ruleset", RULESET,
             "--fail-severity", "error"],
            cwd=REPO, capture_output=True, text=True, timeout=300)

    def lint_json(self, path):
        r = subprocess.run(
            ["scripts/npm-tool.sh", "spectral", "lint", path, "--ruleset", RULESET,
             "--format", "json", "--fail-severity", "error"],
            cwd=REPO, capture_output=True, text=True, timeout=300)
        self.assertNotIn("Error running Spectral", r.stdout + r.stderr)
        try:
            # Spectral may print a status line after the JSON array.
            results, _ = json.JSONDecoder().raw_decode(r.stdout.strip() or "[]")
        except ValueError:
            self.fail(f"Spectral output is not JSON (tool failure?): {r.stdout}{r.stderr}")
        errors = [x for x in results if x.get("severity") == 0]
        return r, errors

    @staticmethod
    def text(e):
        return e.get("message", "") + " " + "/".join(str(p) for p in e.get("path", []))

    def test_real_contract_passes_x_limits_rule(self):
        r, errors = self.lint_json(CONTRACT)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(errors, [])

    def test_x_limits_good_fixture_passes(self):
        # Control: the planted files differ from this one only in x-limits.
        r, errors = self.lint_json(f"{XL}/good.yaml")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(errors, [])

    def test_x_limits_planted_files_fail_for_that_problem(self):
        examined = []
        for name, token in XLIMITS_PLANTED.items():
            with self.subTest(name=name):
                self.assertTrue((REPO / XL / name).is_file(), f"fixture missing: {name}")
                r, errors = self.lint_json(f"{XL}/{name}")
                examined.append(name)
                self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
                self.assertTrue(errors, f"no error-severity result for {name}")
                self.assertTrue(any(token in self.text(e) for e in errors),
                                f"no error names {token!r}: {errors}")
                # exactly that problem: every error is about x-limits or one of its keys
                for e in errors:
                    self.assertTrue(
                        "x-limits" in self.text(e) or any(k in self.text(e) for k in KEYS),
                        f"unrelated error for {name}: {e}")
        self.assertEqual(sorted(examined), sorted(XLIMITS_PLANTED))
        print(f"x-limits planted fixtures examined: {len(examined)}: {examined}")

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
                # Each older planted file fails only for its own problem.
                self.assertNotIn("ap-limits-declared", r.stdout + r.stderr)


if __name__ == "__main__":
    unittest.main()
