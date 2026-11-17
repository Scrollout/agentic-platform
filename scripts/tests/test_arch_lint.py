"""T098 (owner, 2026-11-15; research D40, D54): scripts/check.sh --only arch checks every module of
src/go.work with go-arch-lint against its archfile. Each case runs the step on a throwaway copy of
src/ and scripts/check.sh (clean environment from gitenv.py), with planted violations. The
real tool runs in the pinned golang image only; no other image is used.

Cache: the cases use the shared Go cache check.sh uses by default ($XDG_CACHE_HOME or ~/.cache,
agentic-platform/go), which holds the tool after the first run. The cases that plant a binary,
a pin or a missing tool use a temporary XDG_CACHE_HOME seeded with hard links to the module and
build caches, so the user's cache is never changed by them.
"""
import os
import re
import shutil
import subprocess
import tempfile
import unittest

import yaml

import gitenv

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
FILE_PROXY = "GOPROXY=file:///gopath/pkg/mod/cache/download"
TIMEOUT = 110


def real_cache_home():
    return os.environ.get("XDG_CACHE_HOME") or os.path.join(os.path.expanduser("~"), ".cache")


def rmtree(path):
    """Go's module cache is read-only; make it writable before removing it."""
    subprocess.run(["chmod", "-R", "u+w", path], check=False)
    shutil.rmtree(path, ignore_errors=True)


def workspace_modules(root):
    with open(os.path.join(root, "src", "go.work")) as f:
        text = f.read()
    return re.findall(r"^\s*\./(\S+)\s*$", text, re.M)


class ArchTestCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="arch-")
        self.addCleanup(rmtree, self._tmp)
        self.root = os.path.join(self._tmp, "repo")
        os.makedirs(os.path.join(self.root, "scripts"))
        shutil.copy(os.path.join(REPO, "scripts", "check.sh"), os.path.join(self.root, "scripts"))
        shutil.copytree(os.path.join(REPO, "src"), os.path.join(self.root, "src"),
                        ignore=shutil.ignore_patterns(".git"))
        self.home = gitenv.make_home(self)
        self.log = os.path.join(self._tmp, "docker.log")

    # -- helpers
    def put(self, rel, text):
        full = os.path.join(self.root, rel)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w") as f:
            f.write(text)

    def archfile(self, module):
        path = os.path.join(self.root, "src", module, ".go-arch-lint.yml")
        self.assertTrue(os.path.exists(path), "no archfile " + path)
        return path

    def seeded_cache(self):
        """A temporary XDG_CACHE_HOME with hard links to the user's module and build caches, so a
        module or binary change stays out of the user's cache."""
        # On the cache's own filesystem, so hard links work; removed with the case.
        xdg = tempfile.mkdtemp(prefix="arch-xdg-", dir=real_cache_home())
        self.addCleanup(rmtree, xdg)
        dst = os.path.join(xdg, "agentic-platform", "go")
        src = os.path.join(real_cache_home(), "agentic-platform", "go")
        os.makedirs(os.path.join(dst, "pkg", "mod", "cache"), exist_ok=True)
        os.makedirs(os.path.join(dst, "bin"), exist_ok=True)
        for part in (("pkg", "mod", "cache", "download"), ("cache",)):
            s = os.path.join(src, *part)
            if os.path.isdir(s):
                subprocess.run(["cp", "-al", s, os.path.join(dst, *part[:-1]) + "/"], check=True)
        return xdg, dst

    def run_step(self, xdg=None, stub=True, timeout=TIMEOUT):
        env = gitenv.clean_env(self.home)
        env["XDG_CACHE_HOME"] = xdg or real_cache_home()
        if stub:  # records every docker call, then runs the real docker
            real = shutil.which("docker")
            body = ('for a in "$@"; do printf \'%%s\\0\' "$a"; done >>%s\n'
                    'printf \'\\001END\\0\' >>%s\nexec %s "$@"\n') % (self.log, self.log, real)
            env = gitenv.with_path(env, gitenv.stub_dir(self, {"docker": body}))
        env["DOCKER_CONFIG"] = os.environ.get("DOCKER_CONFIG") or os.path.join(
            os.path.expanduser("~"), ".docker")
        r = subprocess.run(["sh", "scripts/check.sh", "--only", "arch"], cwd=self.root, env=env,
                           text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                           timeout=timeout)
        r.docker_calls = self.docker_calls()
        return r

    def docker_calls(self):
        if not os.path.exists(self.log):
            return []
        with open(self.log, "rb") as f:
            parts = f.read().decode().split("\0")
        calls, cur = [], []
        for p in parts:
            if p == "\x01END":
                calls.append(cur)
                cur = []
            elif p != "":
                cur.append(p)
        return calls

    def assertFails(self, r, *needles):
        self.assertNotEqual(r.returncode, 0, "the step should fail:\n" + r.stdout)
        for n in needles:
            self.assertIn(n, r.stdout)

    def assertLine(self, r, file, line):
        self.assertRegex(r.stdout, re.escape(file) + r"\D{0,12}" + str(line) + r"\b",
                         "the output should name %s at line %s" % (file, line))


class ArchLintTest(ArchTestCase):
    def test_clean_tree_passes(self):
        r = self.run_step()
        self.assertEqual(r.returncode, 0, r.stdout)
        body = "\n".join(l for l in r.stdout.splitlines() if not l.startswith("== PARTIAL"))
        for m in workspace_modules(self.root):
            self.assertRegex(body, r"(^|[\s/:'\"=])%s($|[\s/:'\",])" % re.escape(m),
                             "module %s is not named in the output" % m)

    def test_planted_forbidden_import_fails(self):
        cases = [
            ("src/engine/machine/planted.go", "machine", "github.com/agentic-platform/engine/api"),
            ("src/engine/checks/planted.go", "checks", "github.com/agentic-platform/engine/machine"),
            ("src/cli/api/gen/planted.go", "gen", "github.com/agentic-platform/cli"),
        ]
        for rel, pkg, imp in cases:
            with self.subTest(file=rel):
                self.put(rel, 'package %s\n\nimport _ "%s"\n' % (pkg, imp))
                r = self.run_step()
                self.assertFails(r, "planted.go", imp)
                self.assertLine(r, "planted.go", 3)
                self.assertEqual(r.returncode, 1, r.stdout)
                if pkg != "gen":
                    self.assertIn(pkg, r.stdout)
                os.remove(os.path.join(self.root, rel))

    def test_unmapped_package_fails(self):
        self.put("src/engine/newpkg/newpkg.go", "package newpkg\n")
        r = self.run_step()
        self.assertFails(r, "newpkg.go")
        self.assertRegex(r.stdout, r"(?i)not\s*match|unmatched|not attached")

    def test_undeclared_vendor_import_fails(self):
        self.put("src/slice/planted.go",
                 'package slice\n\nimport _ "golang.org/x/mod/modfile"\n')
        r = self.run_step()
        self.assertFails(r, "planted.go", "golang.org/x/mod/modfile")
        self.assertLine(r, "planted.go", 3)

    def test_tagged_and_test_files_are_checked(self):
        imp = "github.com/agentic-platform/engine/api"
        cases = {
            "src/engine/machine/planted.go":
                '//go:build evals\n\npackage machine\n\nimport _ "%s"\n' % imp,
            "src/engine/machine/planted_test.go":
                'package machine_test\n\nimport _ "%s"\n' % imp,
        }
        for rel, text in cases.items():
            with self.subTest(file=rel):
                self.put(rel, text)
                r = self.run_step()
                self.assertFails(r, os.path.basename(rel), imp)
                os.remove(os.path.join(self.root, rel))

    def test_every_workspace_module_has_an_archfile(self):
        with self.subTest("a use without an archfile"):
            work = os.path.join(self.root, "src", "go.work")
            with open(work) as f:
                text = f.read()
            self.assertIn("\t./tests\n)", text)
            self.put("src/go.work", text.replace("\t./tests\n)", "\t./tests\n\t./zzmod\n)"))
            self.put("src/zzmod/go.mod", "module github.com/agentic-platform/zzmod\n\ngo 1.26.0\n")
            self.put("src/zzmod/zz.go", "package zzmod\n")
            self.assertFails(self.run_step(), "zzmod")
            self.put("src/go.work", text)
            shutil.rmtree(os.path.join(self.root, "src", "zzmod"))
        with self.subTest("an existing module's archfile removed"):
            os.remove(self.archfile("page"))
            self.assertFails(self.run_step(), "page")

    def mutate(self, fn):
        path = self.archfile("engine")
        with open(path) as f:
            data = yaml.safe_load(f)
        fn(data)
        with open(path, "w") as f:
            yaml.safe_dump(data, f)
        return path

    def test_an_invalid_archfile_fails(self):
        def set_allow(key, value):
            def f(d):
                d.setdefault("allow", {})[key] = value
            return f
        def drop_allow(key):
            def f(d):
                d.setdefault("allow", {}).pop(key, None)
            return f
        def wide_in(d):
            name = sorted(d["components"])[0]
            d["components"][name]["in"] = "**"
        original = open(self.archfile("engine")).read()
        cases = [
            ("version 2", lambda d: d.update(version=2), "version"),
            ("no version", lambda d: d.pop("version"), "version"),
            ("depOnAnyVendor true", set_allow("depOnAnyVendor", True), "depOnAnyVendor"),
            ("deepScan true", set_allow("deepScan", True), "deepScan"),
            ("deepScan absent", drop_allow("deepScan"), "deepScan"),
            ("component in **", wide_in, "**"),
        ]
        for label, fn, word in cases:
            with self.subTest(label):
                path = self.mutate(fn)
                self.assertFails(self.run_step(), "engine", word)
                with open(path, "w") as f:
                    f.write(original)

    def test_runs_offline(self):
        xdg, dst = self.seeded_cache()  # module cache filled, no tool binary: the install runs
        r = self.run_step(xdg=xdg)
        self.assertEqual(r.returncode, 0, r.stdout)
        installs = [" ".join(c) for c in r.docker_calls if "go-arch-lint" in " ".join(c)
                    and "install" in " ".join(c)]
        self.assertTrue(installs, "no docker call installs go-arch-lint")
        for c in installs:
            self.assertRegex(c, r"--network\s+none")
            self.assertIn(FILE_PROXY, c)
        checks = [c for c in r.docker_calls if "go-arch-lint" in " ".join(c)
                  and "check" in " ".join(c) and "install" not in " ".join(c)]
        self.assertTrue(checks, "no docker call runs go-arch-lint check")
        for c in checks:
            j = " ".join(c)
            self.assertEqual(c[c.index("--network") + 1], "none", j)
            self.assertIn("GOPROXY=off", j)
            self.assertTrue(any(re.search(r":/src:ro(,|$)|:/src:ro\b", a) for a in c),
                            "src/ is not mounted read-only: " + j)

    def test_pin_is_enforced(self):
        with self.subTest("a binary built from another version"):
            xdg, dst = self.seeded_cache()
            work = os.path.join(self._tmp, "other")
            os.makedirs(work)
            with open(os.path.join(work, "go.mod"), "w") as f:
                f.write("module example.com/other\n\ngo 1.26.0\n")
            with open(os.path.join(work, "main.go"), "w") as f:
                f.write("package main\n\nfunc main() {}\n")
            image = subprocess.run(
                ["awk", '$1=="golang"{print $2}', os.path.join(self.root, "src/deploy/images.lock")],
                text=True, stdout=subprocess.PIPE, check=True).stdout.strip()
            subprocess.run(
                ["docker", "run", "--rm", "--network", "none", "--user", "%d:%d" % (os.getuid(), os.getgid()),
                 "-v", work + ":/w", "-w", "/w", "-e", "GOCACHE=/tmp/c", "-e", "GOTOOLCHAIN=local",
                 "-e", "GOFLAGS=-mod=readonly", image, "go", "build", "-o", "/w/go-arch-lint", "."],
                check=True, timeout=TIMEOUT)
            shutil.copy(os.path.join(work, "go-arch-lint"), os.path.join(dst, "bin", "go-arch-lint"))
            r = self.run_step(xdg=xdg)
            self.assertFails(r, "go-arch-lint")
            self.assertEqual([c for c in r.docker_calls if "go-arch-lint check" in " ".join(c)], [],
                             "a check ran before the pin was enforced")
        with self.subTest("a sum in images.lock that differs from the binary's"):
            xdg, dst = self.seeded_cache()
            lock = os.path.join(self.root, "src/deploy/images.lock")
            with open(lock) as f:
                text = f.read()
            self.assertRegex(text, r"(?m)^go-arch-lint-sum\s+h1:")
            bad = re.sub(r"(?m)^(go-arch-lint-sum\s+)h1:\S+", r"\1h1:" + "A" * 43 + "=", text)
            with open(lock, "w") as f:
                f.write(bad)
            r = self.run_step(xdg=xdg)
            self.assertFails(r, "go-arch-lint")
            self.assertEqual([c for c in r.docker_calls if "go-arch-lint check" in " ".join(c)], [],
                             "a check ran before the pin was enforced")

    def test_step_time_is_reported(self):
        r = self.run_step()
        self.assertEqual(r.returncode, 0, r.stdout)
        self.assertRegex(r.stdout, r"(?m)^\s+arch\s+\d+\.\d\s+\(\d+\.\d / \d+\.\d / \d+\.\d\)$")
        self.assertIn("== time per step", r.stdout)


if __name__ == "__main__":
    unittest.main()
