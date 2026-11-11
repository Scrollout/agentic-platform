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

    # Network, caching and the Markdown linter

    def test_tool_cannot_reach_the_network(self):
        """A contract whose $ref points at a server on the host cannot be bundled.

        The server listens on the Docker bridge gateway, so a container with a network can
        fetch the $ref. The control run proves that: the same bundle succeeds and the server
        sees a request when networking is on. Through npm-tool.sh (--network none) the bundle
        fails and the server sees nothing.
        """
        import http.server
        import threading
        gw = subprocess.run(
            ["docker", "network", "inspect", "bridge", "-f",
             "{{(index .IPAM.Config 0).Gateway}}"], capture_output=True, text=True)
        gateway = gw.stdout.strip()
        if gw.returncode != 0 or not gateway:
            self.skipTest("cannot find the Docker bridge gateway")
        hits = []
        served = Path(self.base) / "served"
        served.mkdir(exist_ok=True)
        (served / "schema.yaml").write_text("type: string\n")

        class Handler(http.server.SimpleHTTPRequestHandler):
            def __init__(self, *a, **k):
                super().__init__(*a, directory=str(served), **k)

            def log_message(self, fmt, *args):
                hits.append(self.path)

        server = http.server.ThreadingHTTPServer(("0.0.0.0", 0), Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.shutdown)
        self.addCleanup(server.server_close)
        url = "http://%s:%d/schema.yaml" % (gateway, server.server_address[1])

        root, script, env = self.fresh("network")
        (root / "ref.yaml").write_text(
            "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n"
            "  schemas:\n    Remote:\n      $ref: '%s'\n" % url)
        out = Path(self.base) / "network-out"
        out.mkdir(exist_ok=True)
        # Install the tools, then run the same command with a network as a control.
        r = run(script, "redocly", "--version", env=env)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        tools = next(Path(env["XDG_CACHE_HOME"], "agentic-platform/node").iterdir())
        image = next(l.split()[1] for l in (root / "src/deploy/images.lock").read_text().splitlines()
                     if l.split()[:1] == ["node"])
        user = "%d:%d" % (os.getuid(), os.getgid())
        control = subprocess.run(
            ["docker", "run", "--rm", "--user", user, "-v", "%s:%s:ro" % (root, root),
             "-v", "%s:%s:ro" % (tools, tools), "-v", "%s:%s" % (out, out), "-w", str(root),
             "-e", "HOME=/tmp", "-e", "REDOCLY_TELEMETRY=off", image,
             str(tools / "node_modules/.bin/redocly"), "bundle", "ref.yaml",
             "-o", str(out / "control.yaml")], capture_output=True, text=True, timeout=300)
        if control.returncode != 0 or not hits:
            self.skipTest("control with networking could not fetch the $ref: %s%s"
                          % (control.stdout, control.stderr))
        hits.clear()
        r = run(script, "redocly", "bundle", "ref.yaml", "-o", str(out / "blocked.yaml"),
                env={**env, "NPM_TOOL_MOUNT": str(out)}, cwd=root)
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertFalse((out / "blocked.yaml").exists())
        self.assertEqual(hits, [])

    def test_second_run_does_not_reinstall(self):
        root, script, env = self.fresh("reinstall")
        r = run(script, "spectral", "--version", env=env)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        dirs = list(Path(env["XDG_CACHE_HOME"], "agentic-platform/node").iterdir())
        self.assertEqual(len(dirs), 1)
        tools = dirs[0]
        marker, modules = tools / ".installed", tools / "node_modules"
        self.assertTrue(marker.is_file())
        sentinel = tools / "sentinel"  # a reinstall removes the whole directory
        sentinel.write_text("x")
        before = (marker.stat().st_mtime_ns, modules.stat().st_mtime_ns)
        r = run(script, "spectral", "--version", env=env)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual((marker.stat().st_mtime_ns, modules.stat().st_mtime_ns), before)
        self.assertTrue(sentinel.exists())

    def test_changed_package_json_or_lock_gets_a_new_cache_directory(self):
        root, script, env = self.fresh("rekey")
        base = Path(env["XDG_CACHE_HOME"], "agentic-platform/node")
        r = run(script, "spectral", "--version", env=env)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        first = {p.name for p in base.iterdir()}
        self.assertEqual(len(first), 1)
        pkg = root / "scripts/tools/package.json"
        data = json.loads(pkg.read_text())
        data["description"] = "changed"
        pkg.write_text(json.dumps(data, indent=2) + "\n")
        r = run(script, "spectral", "--version", env=env)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        second = {p.name for p in base.iterdir()}
        self.assertEqual(len(second), 2)
        for name in second:
            self.assertTrue((base / name / ".installed").is_file())
        # A change to the lock alone gives a third directory.
        lock = root / "scripts/tools/package-lock.json"
        lock.write_text(lock.read_text() + "\n")
        r = run(script, "spectral", "--version", env=env)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(len({p.name for p in base.iterdir()}), 3)

    def test_markdownlint_with_repo_config(self):
        root = self.tree
        shutil.copy2(REPO / "scripts/repo.markdownlint-cli2.jsonc",
                     root / "scripts/repo.markdownlint-cli2.jsonc")
        clean, bad = root / "clean.md", root / "bad.md"
        clean.write_text("# Title\n\nA clean paragraph.\n")
        bad.write_text("# Title\n\n### Skipped a level\n\nText.\n")  # MD001
        args = ["markdownlint-cli2", "--config", "scripts/repo.markdownlint-cli2.jsonc"]
        r = run(self.script, *args, "clean.md", env=self.env, cwd=root)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        r = run(self.script, *args, "bad.md", env=self.env, cwd=root)
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("MD001", r.stdout + r.stderr)
        # The repo config is really in force: MD013 (line length) is off there.
        long = root / "long.md"
        long.write_text("# Title\n\n" + " ".join(["word"] * 100) + "\n")
        r = run(self.script, *args, "long.md", env=self.env, cwd=root)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)


if __name__ == "__main__":
    unittest.main()
