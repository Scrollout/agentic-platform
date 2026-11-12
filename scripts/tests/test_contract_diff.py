"""Tests for scripts/contract-diff.sh (T024a, Principle XV): the newest version of each service's
contract must not break the previous one.

Fixture trees under fixtures/contract-diff/<case>/src/<service>/api/preview/<version>/ are copied
into a temporary directory, with src/deploy/images.lock so the script can find its pinned tool, and
the real script is run against that copy as `contract-diff.sh <repo-root>`. Each planted breaking
change must fail the check naming the service and both versions; the additive fixture must pass,
which shows the check is not simply failing everything. Missing Docker is a failure, not a skip.
"""
import os
import shutil
import subprocess
import tempfile
import unittest

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
SCRIPT = os.path.join(REPO, "scripts", "contract-diff.sh")
FIXTURES = os.path.join(os.path.dirname(os.path.abspath(__file__)), "fixtures", "contract-diff")
OLD, NEW = "2026-11-12-preview", "2027-01-01-preview"


def run(root=None):
    cmd = ["sh", SCRIPT] + ([root] if root else [])
    return subprocess.run(cmd, capture_output=True, text=True, timeout=900, cwd=root or REPO)


class ContractDiffTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if shutil.which("docker") is None:
            raise AssertionError("contract-diff.sh needs docker on PATH (it runs the pinned oasdiff "
                                 "image); docker is not installed")
        if not os.path.isfile(SCRIPT):
            raise AssertionError("scripts/contract-diff.sh does not exist yet (T024a)")
        cls.tmp = tempfile.mkdtemp(prefix="test-contract-diff-")
        cls.addClassCleanup(shutil.rmtree, cls.tmp, ignore_errors=True)

    def tree(self, case):
        """Copy a fixture case into a fresh root and return the root."""
        root = tempfile.mkdtemp(prefix=case + "-", dir=self.tmp)
        os.rmdir(root)
        shutil.copytree(os.path.join(FIXTURES, case), root)
        os.makedirs(os.path.join(root, "src", "deploy"), exist_ok=True)
        shutil.copy2(os.path.join(REPO, "src", "deploy", "images.lock"),
                     os.path.join(root, "src", "deploy", "images.lock"))
        return root

    def assertPasses(self, r, *named):
        out = r.stdout + r.stderr
        self.assertEqual(r.returncode, 0, out)
        for n in named:
            self.assertIn(n, out)

    def assertBreaks(self, case, service="shop", old=OLD, new=NEW):
        r = run(self.tree(case))
        out = r.stdout + r.stderr
        self.assertEqual(r.returncode, 1, out)
        self.assertIn(service, out)
        self.assertIn(old, out)
        self.assertIn(new, out)
        return out

    def test_real_repository_passes_and_engine_has_one_version(self):
        r = run()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("engine: one version, nothing to compare", r.stdout + r.stderr)

    def test_default_root_is_the_repository(self):
        r = run(REPO)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("engine: one version, nothing to compare", r.stdout + r.stderr)

    def test_one_version_fixture(self):
        r = run(self.tree("one-version"))
        self.assertPasses(r, "solo: one version, nothing to compare")

    def test_additive_change_passes_and_names_the_pair(self):
        r = run(self.tree("additive"))
        self.assertPasses(r, "shop", OLD, NEW)
        self.assertNotIn("one version", r.stdout + r.stderr)

    def test_removed_operation(self):
        self.assertBreaks("removed-operation")

    def test_new_required_request_property(self):
        self.assertBreaks("new-required-property")

    def test_new_required_parameter(self):
        self.assertBreaks("new-required-parameter")

    def test_removed_response_property(self):
        self.assertBreaks("removed-response-property")

    def test_narrowed_enum(self):
        self.assertBreaks("narrowed-enum")

    def test_changed_property_type(self):
        self.assertBreaks("changed-type")

    def test_change_only_inside_a_sibling_ref_is_detected(self):
        # openapi.yaml is byte-identical between the two versions; only item.schema.json differs.
        a = os.path.join(FIXTURES, "ref-only-removed-property", "src", "shop", "api", "preview")
        with open(os.path.join(a, OLD, "openapi.yaml")) as f1, \
                open(os.path.join(a, NEW, "openapi.yaml")) as f2:
            old, new = f1.read(), f2.read()
        self.assertEqual(old.split("\n", 2)[2], new.split("\n", 2)[2])  # all but the info line
        self.assertBreaks("ref-only-removed-property")

    def test_two_services_only_the_breaking_one_is_named(self):
        r = run(self.tree("two-services"))
        out = r.stdout + r.stderr
        self.assertEqual(r.returncode, 1, out)
        self.assertIn("alpha", out)
        self.assertIn(OLD, out)
        self.assertIn(NEW, out)
        # beta was examined (one line per service) but is not reported as breaking
        beta = [l for l in out.splitlines() if l.startswith("beta")]
        self.assertTrue(beta, "beta was not examined:\n" + out)
        self.assertTrue(all("no breaking" in l for l in beta), out)

    def test_versions_are_compared_in_folder_name_order(self):
        # The older folder has an extra operation. Older -> newer removes it (breaking).
        # Compared the wrong way round it would be an added operation, which passes.
        self.assertBreaks("ordering")

    def test_a_failing_check_can_fail_and_pass(self):
        # Guards the suite itself: the same tool must give both verdicts on these fixtures.
        self.assertEqual(run(self.tree("additive")).returncode, 0)
        self.assertEqual(run(self.tree("removed-operation")).returncode, 1)


if __name__ == "__main__":
    unittest.main()
