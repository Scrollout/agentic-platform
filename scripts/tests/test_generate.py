"""Tests for scripts/generate.sh, run against a throwaway copy of the repository.

The real working tree is never touched: scripts/generate.sh, scripts/npm-tool.sh,
scripts/tools/ and src/ (without any gen/ directory) are copied into a temporary directory, the script is run there once
to make a baseline, and every test starts from a fresh copy of that baseline.
"""
import os
import re
import shutil
import subprocess
import tempfile
import unittest
import unittest.mock

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
GEN_DIRS = ["src/engine/api/gen", "src/cli/api/gen", "src/page/api/gen"]
GEN_FILE = "engine.gen.go"
CONTRACT = "src/engine/api/preview/2026-11-12-preview/openapi.yaml"
# A description that the generator copies into a Go comment.
CONTRACT_TEXT = "One answer per open question."

MISSING = [t for t in ("docker",) if shutil.which(t) is None]


def snapshot(root):
    """Map every file under root to (content, mtime_ns)."""
    out = {}
    for d, _, files in os.walk(root):
        for f in files:
            p = os.path.join(d, f)
            with open(p, "rb") as fh:
                out[os.path.relpath(p, root)] = (fh.read(), os.stat(p).st_mtime_ns)
    return out


class GenerateScriptTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # A missing prerequisite is a failure, never a skip (constitution; T010c).
        if MISSING:
            raise AssertionError("generate.sh needs docker on PATH; missing: %s"
                                 % ", ".join(MISSING))
        cls._root = tempfile.mkdtemp(prefix="test-generate-")
        cls.addClassCleanup(shutil.rmtree, cls._root, ignore_errors=True)
        cls.baseline = os.path.join(cls._root, "baseline")
        os.makedirs(os.path.join(cls.baseline, "scripts"))
        helpers = ["generate.sh", "npm-tool.sh"]
        if os.path.isfile(os.path.join(REPO, "scripts", "contract-versions.sh")):
            helpers.append("contract-versions.sh")  # the shared ordering rule (T024b)
        for f in helpers:
            shutil.copy2(os.path.join(REPO, "scripts", f),
                         os.path.join(cls.baseline, "scripts", f))
        shutil.copytree(os.path.join(REPO, "scripts", "tools"),
                        os.path.join(cls.baseline, "scripts", "tools"),
                        ignore=shutil.ignore_patterns("node_modules"))
        shutil.copytree(os.path.join(REPO, "src"), os.path.join(cls.baseline, "src"),
                        ignore=shutil.ignore_patterns("gen", "node_modules"))
        r = cls.run_in(cls.baseline)
        if r.returncode != 0:
            raise AssertionError("baseline generation failed:\n%s%s" % (r.stdout, r.stderr))
        for d in GEN_DIRS:
            if not os.path.isfile(os.path.join(cls.baseline, d, GEN_FILE)):
                raise AssertionError("baseline did not produce %s/%s" % (d, GEN_FILE))

    @staticmethod
    def run_in(root, *args):
        return subprocess.run(["sh", os.path.join(root, "scripts", "generate.sh"), *args],
                              cwd=root, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                              text=True, timeout=600)

    def setUp(self):
        self.work = os.path.join(self._root, "work-%d" % id(self))
        self.addCleanup(shutil.rmtree, self.work, ignore_errors=True)
        self.reset()

    def reset(self):
        """Restore the working copy to the generated baseline."""
        shutil.rmtree(self.work, ignore_errors=True)
        shutil.copytree(self.baseline, self.work)

    def path(self, *parts):
        return os.path.join(self.work, *parts)

    def gen_file(self, gen_dir="src/cli/api/gen"):
        return self.path(gen_dir, GEN_FILE)

    def check(self):
        return self.run_in(self.work, "--check")

    def assert_check_fails_naming(self, gen_dir):
        r = self.check()
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn(gen_dir, r.stdout + r.stderr)
        return r

    def assert_restored_by_generate(self):
        r = self.run_in(self.work)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        r = self.check()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    # 1. Clean

    def test_check_passes_after_generate(self):
        r = self.check()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        for d in GEN_DIRS:
            self.assertIn("same     " + d, r.stdout)

    def test_second_generate_changes_nothing(self):
        before = snapshot(self.path("src"))
        r = self.run_in(self.work)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(snapshot(self.path("src")), before)

    def test_check_changes_nothing_when_clean(self):
        before = snapshot(self.path("src"))
        self.assertEqual(self.check().returncode, 0)
        self.assertEqual(snapshot(self.path("src")), before)

    # 2 and 3. Planted violations

    def test_hand_edit_of_generated_file(self):
        for d in GEN_DIRS:
            with self.subTest(gen=d):
                self.reset()
                with open(self.gen_file(d), "a") as f:
                    f.write("\n// hand edit\n")
                before = snapshot(self.path(d))
                self.assert_check_fails_naming(d)
                self.assertEqual(snapshot(self.path(d)), before)
                self.assert_restored_by_generate()

    def test_missing_gen_directory(self):
        for d in GEN_DIRS:
            with self.subTest(gen=d):
                self.reset()
                shutil.rmtree(self.path(d))
                self.assert_check_fails_naming(d)
                self.assertFalse(os.path.exists(self.path(d)))
                self.assert_restored_by_generate()
                self.assertTrue(os.path.isfile(self.gen_file(d)))

    def test_extra_stale_file_in_gen_directory(self):
        for d in GEN_DIRS:
            with self.subTest(gen=d):
                self.reset()
                stale = self.path(d, "old.gen.go")
                with open(stale, "w") as f:
                    f.write("package gen\n")
                self.assert_check_fails_naming(d)
                self.assertTrue(os.path.isfile(stale))
                self.assert_restored_by_generate()
                self.assertFalse(os.path.exists(stale))

    def test_contract_change_without_regenerating(self):
        contract = self.path(CONTRACT)
        with open(contract) as f:
            text = f.read()
        self.assertIn(CONTRACT_TEXT, text)
        with open(contract, "w") as f:
            f.write(text.replace(CONTRACT_TEXT, "One answer for each open question."))
        before = snapshot(self.path("src"))
        r = self.check()
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        for d in GEN_DIRS:
            self.assertIn("differs  " + d, r.stdout)
        self.assertEqual(snapshot(self.path("src")), before)
        self.assert_restored_by_generate()
        with open(self.gen_file()) as f:
            self.assertIn("One answer for each open question.", f.read())

    def test_every_violation_reports_only_the_affected_directory(self):
        with open(self.gen_file("src/page/api/gen"), "a") as f:
            f.write("\n// hand edit\n")
        r = self.check()
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("differs  src/page/api/gen", r.stdout)
        self.assertIn("same     src/cli/api/gen", r.stdout)
        self.assertIn("same     src/engine/api/gen", r.stdout)

    # 4. A tool failure is not a pass

    def test_target_without_contract_fails(self):
        shutil.copy2(self.path("src/cli/api/engine.oapi-codegen.yaml"),
                     self.path("src/cli/api/nosuch.oapi-codegen.yaml"))
        for args in ((), ("--check",)):
            with self.subTest(args=args):
                r = self.run_in(self.work, *args)
                self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
                self.assertIn("nosuch", r.stderr)

    def test_invalid_contract_fails_in_both_modes(self):
        with open(self.path(CONTRACT), "w") as f:
            f.write("openapi: [unclosed\n")
        for args in ((), ("--check",)):
            with self.subTest(args=args):
                r = self.run_in(self.work, *args)
                self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
                self.assertNotIn("same     ", r.stdout)

    def test_invalid_codegen_config_fails(self):
        with open(self.path("src/cli/api/engine.oapi-codegen.yaml"), "w") as f:
            f.write("package: [unclosed\n")
        before = snapshot(self.path("src/cli/api/gen"))
        r = self.check()
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(snapshot(self.path("src/cli/api/gen")), before)

    # 5. Version selection, no targets, missing pins

    def add_version(self, folder, text):
        """Copy the baseline contract into a new version folder with a changed description."""
        dest_dir = self.path("src/engine/api", folder)
        shutil.copytree(os.path.dirname(self.path(CONTRACT)), dest_dir)  # with its schema files
        dest = os.path.join(dest_dir, "openapi.yaml")
        with open(dest) as f:
            body = f.read()
        self.assertIn(CONTRACT_TEXT, body)
        with open(dest, "w") as f:
            f.write(body.replace(CONTRACT_TEXT, text))

    def assert_generated_from(self, text, *absent):
        r = self.run_in(self.work)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        for gen_dir in GEN_DIRS:
            with open(self.gen_file(gen_dir)) as f:
                code = f.read()
            self.assertIn(text, code, gen_dir)
            for other in (CONTRACT_TEXT,) + absent:
                self.assertNotIn(other, code, gen_dir)

    def test_newest_preview_wins(self):
        self.add_version("preview/2026-12-01-preview", "Newest preview wording.")
        self.assert_generated_from("Newest preview wording.")

    def test_preview_wins_over_older_stable(self):
        self.add_version("stable/2026-11-30", "Older stable wording.")
        self.add_version("preview/2026-12-01-preview", "Newer preview wording.")
        self.assert_generated_from("Newer preview wording.", "Older stable wording.")

    def test_stable_wins_over_older_preview(self):
        self.add_version("preview/2026-12-01-preview", "Older preview wording.")
        self.add_version("stable/2026-12-15", "Newer stable wording.")
        self.assert_generated_from("Newer stable wording.", "Older preview wording.")

    def test_stable_wins_over_preview_of_the_same_date(self):
        # Baseline preview/2026-11-12-preview exists; stable of the same date is newer.
        # Run under LC_ALL=C too: the choice must not depend on the caller's collation.
        self.add_version("stable/2026-11-12", "Same-date stable wording.")
        with unittest.mock.patch.dict(os.environ, {"LC_ALL": "C", "LANG": "C"}):
            self.assert_generated_from("Same-date stable wording.")

    def test_same_date_preview_does_not_win_over_stable(self):
        self.add_version("stable/2026-12-01", "Stable wording.")
        self.add_version("preview/2026-12-01-preview", "Preview wording.")
        self.assert_generated_from("Stable wording.", "Preview wording.")

    def test_bad_version_folder_fails(self):
        for folder in ("stable/latest", "stable/2026-13-01x", "stable/2027-01-01-preview",
                       "preview/2027-01-01"):
            with self.subTest(folder=folder):
                self.reset()
                self.add_version(folder, "Bad folder wording.")
                r = self.run_in(self.work)
                self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
                self.assertIn(folder.split("/")[1], r.stdout + r.stderr)

    def test_no_targets_exits_zero(self):
        for d in GEN_DIRS:
            os.remove(self.path(os.path.dirname(d), "engine.oapi-codegen.yaml"))
        # Remove every other target generate.sh finds: src/*/api/*.oapi-codegen.yaml, files with
        # inline-generated blocks, and non-test Go files outside gen/ that embed a gen/ copy.
        begin, embed = "# BEGIN generated by scripts/generate.sh: ", re.compile(r"^//go:embed (.* )?gen/", re.M)
        for dirpath, _, names in os.walk(self.path("src")):
            if "/gen" in dirpath.replace(self.work, "") + "/":
                continue
            for name in names:
                p = os.path.join(dirpath, name)
                if name.endswith(".oapi-codegen.yaml") and os.path.basename(dirpath) == "api":
                    os.remove(p)
                elif name.endswith((".yaml", ".go")) and not name.endswith("_test.go"):
                    with open(p, encoding="utf-8", errors="replace") as f:
                        text = f.read()
                    if (name.endswith(".yaml") and begin in text) or (name.endswith(".go") and embed.search(text)):
                        os.remove(p)
        before = snapshot(self.path("src"))
        for args in ((), ("--check",)):
            with self.subTest(args=args):
                r = self.run_in(self.work, *args)
                self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
                self.assertIn("no targets", r.stdout)
                self.assertEqual(snapshot(self.path("src")), before)

    def test_missing_pin_fails_without_writing_gen(self):
        lock = self.path("src/deploy/images.lock")
        with open(lock) as f:
            original = f.read()
        for pin in ("golang", "oapi-codegen"):
            with self.subTest(pin=pin):
                self.reset()
                lines = [l for l in original.splitlines() if l.split()[:1] != [pin]]
                self.assertLess(len(lines), len(original.splitlines()))
                with open(lock, "w") as f:
                    f.write("\n".join(lines) + "\n")
                for d in GEN_DIRS:
                    shutil.rmtree(self.path(d))
                for args in ((), ("--check",)):
                    r = self.run_in(self.work, *args)
                    self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
                    self.assertNotIn("written  ", r.stdout)
                    self.assertNotIn("same     ", r.stdout)
                    for d in GEN_DIRS:
                        self.assertFalse(os.path.exists(self.path(d)), d)


if __name__ == "__main__":
    unittest.main()
