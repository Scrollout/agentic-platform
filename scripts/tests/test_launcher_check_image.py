"""T094: the built check image's module cache is shared read-only (research D37, ADR 0041).

Failing tests, by a separate author, against the check image `src/deploy/build.sh check` built and
recorded as AP_IMAGE_CHECK in ${XDG_STATE_HOME:-~/.local/state}/agentic-platform/images.env, the
reference the launcher starts the check service from:

  - GOMODCACHE is /opt/gomodcache, owned by root, and the check user (65532, the image's USER)
    can create, change or remove nothing in it;
  - /opt/gocache-base, the warm base cache each call copies as its GOCACHE (D37 "Cost"), exists,
    holds entries, is owned by root, and the check user can create, change or remove nothing in it;
  - /opt/template is byte-identical to src/catalogue/api-feature-go/template: the same file list
    and the same bytes in each file (a build step must not rewrite the template's go.mod or go.sum);
  - the image sets no GOCACHE and no GOPATH (Dockerfile.check drops them: each call of the check
    program sets them to its own folder) and keeps HOME=/tmp/home for the service process.

No image recorded is a failure, never a skip: build it with
`COSIGN_PASSWORD=... src/deploy/build.sh check`. The container runs with no network, removed
when it ends.
"""
import hashlib
import os
import pathlib
import subprocess
import unittest
import uuid

MODCACHE = "/opt/gomodcache"
GOCACHE_BASE = "/opt/gocache-base"
TEMPLATE = "/opt/template"
REPO = pathlib.Path(__file__).resolve().parents[2]
TEMPLATE_SRC = REPO / "src/catalogue/api-feature-go/template"


def source_template():
    """{relative path: sha256} of every file of the template in the repository."""
    return {p.relative_to(TEMPLATE_SRC).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted(TEMPLATE_SRC.rglob("*")) if p.is_file()}


def parse_sums(out):
    """{relative path: sha256} from `sha256sum` lines of files under TEMPLATE."""
    sums = {}
    for line in out.splitlines():
        digest, _, path = line.partition("  ")
        if path.startswith(TEMPLATE + "/"):
            sums[path[len(TEMPLATE) + 1:]] = digest
    return sums


def template_differences(image, source):
    """What differs between the image's template and the repository's, by file; empty when none."""
    diffs = ["only in the image: %s" % p for p in sorted(set(image) - set(source))]
    diffs += ["missing from the image: %s" % p for p in sorted(set(source) - set(image))]
    diffs += ["differs: %s" % p for p in sorted(set(image) & set(source)) if image[p] != source[p]]
    return diffs
CHECK_USER = "65532:65532"


def images_env():
    state = os.environ.get("XDG_STATE_HOME") or os.path.join(os.path.expanduser("~"), ".local/state")
    return pathlib.Path(state) / "agentic-platform/images.env"


def check_image():
    path = images_env()
    if path.is_file():
        for line in path.read_text().splitlines():
            if line.startswith("AP_IMAGE_CHECK="):
                return line.split("=", 1)[1].strip()
    raise AssertionError("no AP_IMAGE_CHECK in %s: build the check image first "
                         "(COSIGN_PASSWORD=... src/deploy/build.sh check)" % path)


def image_env(ref):
    r = subprocess.run(["docker", "image", "inspect", "--format", "{{range .Config.Env}}{{println .}}{{end}}", ref],
                       capture_output=True, text=True, timeout=60)
    if r.returncode != 0:
        raise AssertionError("docker image inspect %s: %s" % (ref, r.stderr.strip()))
    env = {}
    for line in r.stdout.splitlines():
        if "=" in line:
            k, v = line.split("=", 1)
            env[k] = v
    return env


def run_as_check_user(ref, script):
    """Runs script with sh in the image as the check user, no network; returns (exit, output)."""
    name = "ap-t094-modcache-%s" % uuid.uuid4().hex[:12]
    r = subprocess.run(["docker", "run", "--rm", "--name", name, "--network", "none", "--user", CHECK_USER,
                        "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--entrypoint", "sh", ref,
                        "-c", script], capture_output=True, text=True, timeout=120)
    return r.returncode, r.stdout + r.stderr


class CheckImageModuleCacheTest(unittest.TestCase):

    @classmethod
    def setUpClass(cls):
        cls.ref = check_image()
        cls.env = image_env(cls.ref)

    def test_gomodcache_is_opt_gomodcache(self):
        self.assertEqual(self.env.get("GOMODCACHE"), MODCACHE, "the image's ENV: %s" % self.env)

    def test_gomodcache_is_owned_by_root(self):
        code, out = run_as_check_user(self.ref, "stat -c '%%u:%%g' %s && find %s ! -user 0 | head -5" % (MODCACHE, MODCACHE))
        self.assertEqual(code, 0, out)
        lines = out.split()
        self.assertTrue(lines and lines[0] == "0:0", "%s must be owned by root:root: %s" % (MODCACHE, out))
        self.assertEqual(lines[1:], [], "entries under %s not owned by root: %s" % (MODCACHE, lines[1:]))

    def test_gomodcache_is_not_writable_by_the_check_user(self):
        # Without --read-only, so only ownership and modes decide (compose's read-only root file
        # system is a second guard, tested with compose).
        script = (
            "m=%s; bad=\n"
            "touch \"$m/t094-new\" 2>/dev/null && bad=\"$bad created-file\"\n"
            "mkdir \"$m/t094-dir\" 2>/dev/null && bad=\"$bad created-folder\"\n"
            "f=$(find \"$m\" -type f -name go.mod | head -1)\n"
            "[ -n \"$f\" ] || { echo NO-MODULE-FILE; exit 3; }\n"
            "( : >>\"$f\" ) 2>/dev/null && bad=\"$bad changed-file:$f\"\n"
            "rm -f \"$f\" 2>/dev/null && bad=\"$bad removed-file:$f\"\n"
            "d=$(find \"$m\" -type d -perm -0002 | head -3); [ -z \"$d\" ] || bad=\"$bad world-writable:$d\"\n"
            "echo \"BAD:$bad\"\n" % MODCACHE)
        code, out = run_as_check_user(self.ref, script)
        self.assertEqual(code, 0, out)
        bad = [ln for ln in out.splitlines() if ln.startswith("BAD:")]
        self.assertEqual(bad, ["BAD:"], "the check user could change %s: %s" % (MODCACHE, out))

    def test_gocache_base_exists_and_is_owned_by_root(self):
        code, out = run_as_check_user(self.ref, "[ -d %s ] || { echo NO-BASE; exit 3; }; stat -c '%%u:%%g' %s; "
                                      "echo FILES $(find %s -type f | wc -l); find %s ! -user 0 | head -5"
                                      % ((GOCACHE_BASE,) * 4))
        self.assertEqual(code, 0, "%s must be a folder in the image: %s" % (GOCACHE_BASE, out))
        lines = out.splitlines()
        self.assertEqual(lines[0], "0:0", "%s must be owned by root:root: %s" % (GOCACHE_BASE, out))
        files = int(lines[1].split()[1])
        self.assertGreater(files, 0, "%s holds no cache entries: %s" % (GOCACHE_BASE, out))
        self.assertEqual(lines[2:], [], "entries under %s not owned by root: %s" % (GOCACHE_BASE, lines[2:]))

    def test_gocache_base_is_not_writable_by_the_check_user(self):
        script = (
            "m=%s; [ -d \"$m\" ] || { echo NO-BASE; exit 3; }; bad=\n"
            "touch \"$m/t094-new\" 2>/dev/null && bad=\"$bad created-file\"\n"
            "mkdir \"$m/t094-dir\" 2>/dev/null && bad=\"$bad created-folder\"\n"
            "f=$(find \"$m\" -type f | head -1)\n"
            "[ -n \"$f\" ] || { echo NO-ENTRY; exit 3; }\n"
            "( : >>\"$f\" ) 2>/dev/null && bad=\"$bad changed-file:$f\"\n"
            "rm -f \"$f\" 2>/dev/null && bad=\"$bad removed-file:$f\"\n"
            "d=$(find \"$m\" -perm -0002 | head -3); [ -z \"$d\" ] || bad=\"$bad world-writable:$d\"\n"
            "echo \"BAD:$bad\"\n" % GOCACHE_BASE)
        code, out = run_as_check_user(self.ref, script)
        self.assertEqual(code, 0, out)
        bad = [ln for ln in out.splitlines() if ln.startswith("BAD:")]
        self.assertEqual(bad, ["BAD:"], "the check user could change %s: %s" % (GOCACHE_BASE, out))

    def test_the_template_is_the_repositorys_byte_for_byte(self):
        code, out = run_as_check_user(self.ref, "[ -d %s ] || { echo NO-TEMPLATE; exit 3; }; "
                                      "find %s ! -type d ! -type f | sed 's/^/NOT-A-FILE /'; "
                                      "find %s -type f | sort | xargs sha256sum" % ((TEMPLATE,) * 3))
        self.assertEqual(code, 0, out)
        self.assertNotIn("NOT-A-FILE", out, "the image's template holds entries that are not files: %s" % out)
        image, source = parse_sums(out), source_template()
        self.assertIn("go.mod", source)
        self.assertIn("go.sum", source)
        self.assertEqual(template_differences(image, source), [],
                         "%s in the image is not %s byte for byte" % (TEMPLATE, TEMPLATE_SRC.relative_to(REPO)))

    def test_the_image_sets_no_go_cache_or_go_path(self):
        for name in ("GOCACHE", "GOPATH"):
            self.assertNotIn(name, self.env, "each call sets %s to its own folder; the image must not (%s=%s)"
                             % (name, name, self.env.get(name)))
        self.assertEqual(self.env.get("HOME"), "/tmp/home", "HOME stays /tmp/home for the service process")


if __name__ == "__main__":
    unittest.main()
