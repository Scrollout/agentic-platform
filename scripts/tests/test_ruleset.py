import json
import re
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
    "planted-x-limits-null.yaml": "x-limits",
    "planted-x-limits-array.yaml": "x-limits",
    "planted-boolean-value.yaml": KEYS[0],
}
# T005c. Fixtures the rule ACCEPTS today, each differing from good.yaml only in x-limits. They
# pin the rule's stated contract ("a positive integer", no upper bound; opts.keys) and its limits:
#  - 10^30 is a positive integer, so the rule accepts it (contract.go refuses it: int64 overflow).
#  - 20.0 and 1e3 reach the rule as the JavaScript numbers 20 and 1000, which it cannot tell
#    from integers (known limit, T005c; contract.go refuses both as !!float, so the engine
#    still catches them at start, the ruleset does not).
# If the rule is ever changed to check the source text, these tests must change with it.
XLIMITS_ACCEPTED = [
    "accepted-huge-integer.yaml",
    "accepted-float-with-zero-fraction.yaml",
    "accepted-exponent-value.yaml",
]


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

    def test_x_limits_accepted_fixtures_pin_current_behaviour(self):
        for name in XLIMITS_ACCEPTED:
            with self.subTest(name=name):
                self.assertTrue((REPO / XL / name).is_file(), f"fixture missing: {name}")
                r, errors = self.lint_json(f"{XL}/{name}")
                self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
                self.assertEqual(errors, [])

    def test_rule_key_list_equals_the_keys_contract_go_reads(self):
        # One fact, two homes (ruleset.yaml, contract.go): they must not drift (T005c).
        ruleset = (REPO / RULESET).read_text()
        m = re.search(r"functionOptions:\s*\n\s+keys:\s*\n((?:\s+-\s+\S+\s*\n)+)", ruleset)
        self.assertTrue(m, "no functionOptions.keys list found in ruleset.yaml")
        rule_keys = re.findall(r"-\s+(\S+)", m.group(1))
        self.assertTrue(rule_keys, "ruleset key list is empty")
        go = (REPO / "src/engine/api/contract.go").read_text()
        m = re.search(r"Limits\s+struct\s*\{(.*?)\n\s*\}\s*`yaml:\"x-limits\"`", go, re.S)
        self.assertTrue(m, "found no `x-limits` struct in contract.go; the parse needs updating")
        go_keys = re.findall(r'`yaml:"([^",]+)"`', m.group(1))
        self.assertTrue(go_keys, "found no yaml tags inside the x-limits struct in contract.go")
        self.assertEqual(sorted(rule_keys), sorted(go_keys),
                         "ruleset.yaml keys and contract.go x-limits keys differ")

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
