"""Tests for scripts/npm-tool.sh, the runner for the pinned Node tools.

Tests that could disturb the real cache or tree use a temporary copy of the script,
scripts/tools/ and src/deploy/images.lock, and their own XDG_CACHE_HOME.
"""
import json
import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent.parent
CONTRACT = "src/engine/api/preview/2026-11-12-preview/openapi.yaml"
PACKAGES = {"spectral": ("@stoplight/spectral-cli", "6.15.0"),
            "redocly": ("@redocly/cli", "2.72.1"),
            "markdownlint-cli2": ("markdownlint-cli2", "0.18.1")}
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None


def run(script, *args, cwd=None, env=None, timeout=900):
    e = dict(os.environ)
    e.update(env or {})
    return subprocess.run(["sh", str(script), *args], cwd=cwd, env=e,
                          capture_output=True, text=True, timeout=timeout)


def make_tree(root):
    """Copy what npm-tool.sh needs into root; return the script path."""
    root = Path(root)
    (root / "scripts").mkdir(parents=True)
    shutil.copy2(REPO / "scripts/npm-tool.sh", root / "scripts/npm-tool.sh")
    shutil.copytree(REPO / "scripts/tools", root / "scripts/tools",
                    ignore=shutil.ignore_patterns("node_modules"))
    (root / "src/deploy").mkdir(parents=True)
    shutil.copy2(REPO / "src/deploy/images.lock", root / "src/deploy/images.lock")
    return root / "scripts/npm-tool.sh"


@unittest.skipUnless(shutil.which("docker"), "docker not on PATH")
class NpmToolTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.base = tempfile.mkdtemp(prefix="test-npm-tool-", dir=TMP_BASE)
        cls.addClassCleanup(shutil.rmtree, cls.base, ignore_errors=True)
        # One tree and one cache shared by tests that only read.
        cls.tree = Path(cls.base) / "shared"
        cls.script = make_tree(cls.tree)
        cls.env = {"XDG_CACHE_HOME": str(Path(cls.base) / "cache")}

    def fresh(self, name):
        """A new tree plus its own empty cache."""
        root = Path(self.base) / name
        script = make_tree(root)
        return root, script, {"XDG_CACHE_HOME": str(Path(self.base) / (name + "-cache"))}

    def test_pinned_versions(self):
        declared = json.loads((REPO / "scripts/tools/package.json").read_text())
        for tool, (pkg, version) in PACKAGES.items():
            with self.subTest(tool=tool):
                self.assertEqual(declared["devDependencies"][pkg], version)
                r = run(self.script, tool, "--version", env=self.env)
                self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
                self.assertRegex(r.stdout + r.stderr,
                                 r"(?<![\d.])%s(?![\d.])" % re.escape(version))

    def test_no_arguments_exits_2(self):
        r = run(self.script, env=self.env)
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
        self.assertIn("usage", r.stderr)

    def test_unknown_tool_exits_2(self):
        r = run(self.script, "no-such-tool", env=self.env)
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
        self.assertIn("no-such-tool", r.stderr)

    def test_missing_node_pin_exits_1(self):
        root, script, env = self.fresh("nopin")
        lock = root / "src/deploy/images.lock"
        lines = [l for l in lock.read_text().splitlines() if l.split()[:1] != ["node"]]
        self.assertLess(len(lines), len(lock.read_text().splitlines()))
        lock.write_text("\n".join(lines) + "\n")
        r = run(script, "spectral", "--version", env=env)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("node", r.stderr)
        self.assertFalse(Path(env["XDG_CACHE_HOME"]).exists())

    def test_tampered_lock_fails_install(self):
        root, script, env = self.fresh("tampered")
        lock = root / "scripts/tools/package-lock.json"
        data = json.loads(lock.read_text())
        for path, pkg in data["packages"].items():
            if path and "integrity" in pkg:
                digest = pkg["integrity"]
                pkg["integrity"] = digest[:-6] + ("AAAAA=" if not digest.endswith("AAAAA=") else "BBBBB=")
                break
        else:
            self.fail("no integrity value to tamper with")
        lock.write_text(json.dumps(data, indent=2))
        r = run(script, "spectral", "--version", env=env)
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertNotRegex(r.stdout, r"6\.15\.0")
        cache = Path(env["XDG_CACHE_HOME"])
        self.assertEqual(list(cache.glob("agentic-platform/node/*/.installed")), [])

    def test_repo_readable_and_read_only(self):
        root = self.tree
        shutil.copytree(REPO / Path(CONTRACT).parent, root / Path(CONTRACT).parent,
                        dirs_exist_ok=True)
        out_dir = Path(self.base) / "out"
        out_dir.mkdir(exist_ok=True)
        # A file under the repo is readable, and a mounted directory outside it is writable.
        ok = out_dir / "bundle.yaml"
        r = run(self.script, "redocly", "bundle", str(root / CONTRACT), "-o", str(ok),
                env={**self.env, "NPM_TOOL_MOUNT": str(out_dir)}, cwd=root)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("openapi", ok.read_text())
        # A write into the repo fails: it is mounted read-only.
        inside = root / "bundle-inside.yaml"
        r = run(self.script, "redocly", "bundle", str(root / CONTRACT), "-o", str(inside),
                env=self.env, cwd=root)
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertFalse(inside.exists())
        # Without NPM_TOOL_MOUNT nothing reaches the host outside the repo either.
        stray = out_dir / "x.yaml"
        r = run(self.script, "redocly", "bundle", str(root / CONTRACT), "-o", str(stray),
                env=self.env, cwd=root)
        self.assertFalse(stray.exists(), r.stdout + r.stderr)


if __name__ == "__main__":
    unittest.main()
