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


def run(root=None, locale=None):
    cmd = ["sh", SCRIPT] + ([root] if root else [])
    env = dict(os.environ)
    if locale:
        env.update(LC_ALL=locale, LANG=locale)
    return subprocess.run(cmd, capture_output=True, text=True, timeout=900, cwd=root or REPO, env=env)


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

    # T024b: version ordering, exit 2, three versions, spaces, exit precedence.
    # Trees are built from the "ordering" fixture's two contracts: FULL has /items/{id}, LACK lacks it.
    # Going FULL -> LACK removes an operation (breaking); LACK -> FULL only adds one (passes).

    def build(self, versions, service="shop", root=None):
        """versions: {"stable/2026-11-12": "full"|"lack"|"invalid", ...}. Returns the root."""
        if root is None:
            root = self.tree("one-version")
            shutil.rmtree(os.path.join(root, "src", "solo"), ignore_errors=True)
        src = {"full": os.path.join(FIXTURES, "ordering", "src", "shop", "api", "preview", OLD),
               "lack": os.path.join(FIXTURES, "ordering", "src", "shop", "api", "preview", NEW)}
        for rel, kind in versions.items():
            dest = os.path.join(root, "src", service, "api", *rel.split("/"))
            if kind == "invalid":
                os.makedirs(dest)
                with open(os.path.join(dest, "openapi.yaml"), "w") as f:
                    f.write("openapi: [unclosed\n")
            else:
                shutil.copytree(src[kind], dest)
        return root

    def assertCannotCompare(self, r, *named):
        out = r.stdout + r.stderr
        self.assertEqual(r.returncode, 2, out)
        for n in named:
            self.assertIn(n, out)
        return out

    def test_stable_is_newer_than_preview_of_the_same_date(self):
        # full preview -> lack stable removes an operation. If the order were reversed
        # (stable older), lack -> full would only add one and pass.
        # Run under several locales: the order must not depend on the caller's collation.
        for loc in (None, "C", "C.UTF-8", "en_US.UTF-8"):
            with self.subTest(locale=loc):
                r = run(self.build({"preview/2026-11-12-preview": "full", "stable/2026-11-12": "lack"}),
                        locale=loc)
                out = r.stdout + r.stderr
                self.assertEqual(r.returncode, 1, out)
                self.assertIn("2026-11-12-preview", out)
                self.assertIn("breaking", out)

    def test_preview_is_older_than_stable_of_the_same_date(self):
        # The counterpart: stable full, preview lack of the same date is additive only if
        # preview is OLDER; the rule says preview is older, so stable(full) is the newer: passes.
        for loc in (None, "C", "C.UTF-8", "en_US.UTF-8"):
            with self.subTest(locale=loc):
                r = run(self.build({"preview/2026-11-12-preview": "lack", "stable/2026-11-12": "full"}),
                        locale=loc)
                self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_date_beats_stability(self):
        # A newer-dated preview is newer than an older stable.
        r = run(self.build({"stable/2026-11-12": "full", "preview/2027-01-01-preview": "lack"}))
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)

    def test_three_versions_only_an_older_pair_breaks(self):
        # 2026-01-01 full -> 2026-06-01 lack breaks; the newest pair lack -> lack does not.
        r = run(self.build({"stable/2026-01-01": "full", "stable/2026-06-01": "lack",
                            "stable/2026-11-12": "lack"}))
        out = r.stdout + r.stderr
        self.assertEqual(r.returncode, 0, out)
        self.assertIn("2026-06-01", out)
        self.assertIn("2026-11-12", out)
        self.assertNotIn("2026-01-01", out)

    def test_three_versions_newest_pair_breaks_and_is_named(self):
        r = run(self.build({"stable/2026-01-01": "lack", "stable/2026-06-01": "full",
                            "stable/2026-11-12": "lack"}))
        out = r.stdout + r.stderr
        self.assertEqual(r.returncode, 1, out)
        self.assertIn("2026-06-01", out)
        self.assertNotIn("2026-01-01", out)

    def test_bad_version_names_cannot_be_compared(self):
        for bad, where in (("2026-13-01x", "stable"), ("latest", "stable"),
                           ("2026-11-12-preview", "stable"), ("2026-11-12", "preview")):
            with self.subTest(name=bad, folder=where):
                root = self.build({"stable/2026-01-01": "full"})
                self.build({where + "/" + bad: "lack"}, root=root)
                self.assertCannotCompare(run(root), bad)

    def test_invalid_yaml_cannot_be_compared(self):
        r = run(self.build({"stable/2026-01-01": "full", "stable/2026-11-12": "invalid"}))
        self.assertCannotCompare(r, "could not compare")

    def test_root_without_src_cannot_be_compared(self):
        root = tempfile.mkdtemp(prefix="nosrc-", dir=self.tmp)
        self.assertCannotCompare(run(root))

    def test_empty_src_passes_with_a_message(self):
        root = tempfile.mkdtemp(prefix="emptysrc-", dir=self.tmp)
        os.makedirs(os.path.join(root, "src"))
        r = run(root)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertTrue((r.stdout + r.stderr).strip(), "expected a message")

    def test_exit_2_wins_over_a_break_in_another_service(self):
        root = self.build({"stable/2026-01-01": "full", "stable/2026-11-12": "lack"}, service="alpha")
        self.build({"stable/2026-01-01": "full", "stable/2026-11-12": "invalid"}, service="beta", root=root)
        out = self.assertCannotCompare(run(root))
        self.assertIn("alpha", out)
        # and in the other alphabetical order
        root = self.build({"stable/2026-01-01": "full", "stable/2026-11-12": "invalid"}, service="alpha")
        self.build({"stable/2026-01-01": "full", "stable/2026-11-12": "lack"}, service="beta", root=root)
        self.assertCannotCompare(run(root))

    def test_service_name_with_a_space(self):
        root = self.build({"stable/2026-01-01": "full", "stable/2026-11-12": "lack"}, service="my shop")
        r = run(root)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("my shop", r.stdout + r.stderr)
        root = self.build({"stable/2026-01-01": "lack", "stable/2026-11-12": "full"}, service="my shop")
        self.assertEqual(run(root).returncode, 0)

    def test_root_path_with_a_space(self):
        root = self.build({"stable/2026-01-01": "full", "stable/2026-11-12": "lack"})
        spaced = os.path.join(self.tmp, "a root with spaces")
        shutil.move(root, spaced)
        self.assertEqual(run(spaced).returncode, 1)


if __name__ == "__main__":
    unittest.main()
