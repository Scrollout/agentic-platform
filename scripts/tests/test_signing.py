"""End-to-end test of image signing (T058, ADR 0028, research D24), with real Docker and no stub.

One `src/deploy/build.sh check` run builds the smallest image, pushes it to a local registry with an
SBOM and provenance, signs the digest and records it. The same signed manifest is then published and
signed under the engine, page and cli repositories, so the real `ap up` has the three images it needs.
Verification is exercised through `ap up` itself (its own cosign flags and network), with only
`docker compose` intercepted by a shim (so nothing is started and the developer's platform is never
touched): the signed images pass; another key pair and an unsigned digest each stop `up` before
Compose is called.

Isolation: the test runs its own registry under AP_REGISTRY_NAME=ap-test-<random> (container,
`<name>-data` volume, `<name>-net` network), a free port, temporary XDG dirs and its own password. It
never skips, never touches the default-named `ap-registry`, `ap-registry-data` or `ap-signing`
(a test checks they are unchanged if they exist) and removes only what it made. It is slow (one build).
"""
import json
import os
import re
import secrets
import shutil
import socket
import stat
import subprocess
import tempfile
import threading
import time
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
# AP_TEST_DEPLOY_DIR points the T058a tests at another copy of src/deploy (to see them fail against
# the committed scripts); unset, the working tree's is tested.
DEPLOY = Path(os.environ.get("AP_TEST_DEPLOY_DIR") or REPO / "src/deploy")
BUILD = DEPLOY / "build.sh"
AP = DEPLOY / "ap"
LOCK = REPO / "src/deploy/images.lock"
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
NOT_SIGNED = r"no (matching )?(signatures|attestations)"  # cosign v3 wording for a refused or unsigned digest
STALE = r"remov\w+ the stale lock"
DIGEST = r"sha256:[0-9a-f]{64}"


def pin(name):
    for line in LOCK.read_text().splitlines():
        f = line.split()
        if len(f) >= 2 and f[0] == name:
            return f[1]
    raise AssertionError("no %s pin in src/deploy/images.lock" % name)


def sh(*args, **kw):
    kw.setdefault("capture_output", True)
    kw.setdefault("text", True)
    kw.setdefault("timeout", 300)
    return subprocess.run(list(args), **kw)


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


DEFAULTS = (("container", "ap-registry"), ("volume", "ap-registry-data"), ("network", "ap-signing"))


def snapshot_defaults():
    """What the default-named objects look like now (None when absent), to prove they stay untouched."""
    out = {}
    for kind, name in DEFAULTS:
        r = sh("docker", kind, "inspect", name)
        if r.returncode != 0:
            out[name] = None
            continue
        d = json.loads(r.stdout)[0]
        out[name] = {k: d.get(k) for k in ("Id", "Created", "CreatedAt", "Name", "Mountpoint", "Labels")} | \
            {"StartedAt": (d.get("State") or {}).get("StartedAt"), "Running": (d.get("State") or {}).get("Running"),
             "Networks": sorted((d.get("NetworkSettings") or {}).get("Networks") or [])}
    return out



def remove_registry_objects(name, volume=True):
    """Remove every Docker object named `name` or `name-...` (container, volume, networks) and raise if one
    is left; `name` is always a random ap-test-<hex>, never a default-named object."""
    assert name.startswith("ap-test-") and len(name) > len("ap-test-"), name
    sh("docker", "rm", "-f", name)
    sh("docker", "volume", "rm", "-f", name + "-data")
    for n in sh("docker", "network", "ls", "--format", "{{.Name}}", "--filter", "name=" + name).stdout.split():
        if n == name or n.startswith(name + "-"):
            sh("docker", "network", "rm", n)
    left = []
    for kind, fmt in (("container", "{{.Names}}"), ("volume", "{{.Name}}"), ("network", "{{.Name}}")):
        cmd = ["docker", kind, "ls", "--format", fmt, "--filter", "name=" + name]
        if kind == "container":
            cmd.insert(3, "-a")
        left += [(kind, n) for n in sh(*cmd).stdout.split() if n == name or n.startswith(name + "-")]
    if left:
        raise AssertionError("Docker objects of the test left behind: %s" % left)


SHIM = """#!/bin/sh
# Stands in for `docker compose` only: records the call and the images it was given, starts nothing.
if [ "$1" = compose ]; then
  { echo "COMPOSE $*"; env | grep '^AP_IMAGE_'; } >> "$SHIM_LOG"
  exit 0
fi
exec "$REAL_DOCKER" "$@"
"""


class SigningEndToEndTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # A missing Docker is a failure, not a skip: this check must not pass silently.
        if not shutil.which("docker") or sh("docker", "info").returncode != 0:
            raise AssertionError("docker is required for the signing end-to-end test and is not reachable")
        cls.defaults_before = snapshot_defaults()
        cls.cosign = pin("cosign")
        pin("registry"), pin("sbom-scanner")  # both must be pinned
        cls.name = "ap-test-" + secrets.token_hex(4)
        cls.volume, cls.net = cls.name + "-data", cls.name + "-net"
        cls.base = Path(tempfile.mkdtemp(prefix="test-signing-", dir=TMP_BASE))
        cls.addClassCleanup(shutil.rmtree, cls.base, ignore_errors=True)
        cls.addClassCleanup(cls.cleanup_docker)  # runs first: removes only what this test made
        cls.config, cls.state = cls.base / "config", cls.base / "state"
        cls.config.mkdir()
        cls.state.mkdir()
        cls.port = free_port()
        cls.password = secrets.token_hex(16)
        cls.signing = cls.config / "agentic-platform/signing"
        cls.images_env = cls.state / "agentic-platform/images.env"
        cls.refs = [("127.0.0.1:%d/ap-check:dev" % cls.port)]
        cls.env = {**os.environ, "XDG_CONFIG_HOME": str(cls.config), "XDG_STATE_HOME": str(cls.state),
                   "AP_REGISTRY_PORT": str(cls.port), "AP_REGISTRY_NAME": cls.name,
                   "COSIGN_PASSWORD": cls.password}
        cls.build = sh("bash", str(BUILD), "check", env=cls.env, timeout=1500, cwd=REPO)
        cls.ref = ""
        cls.setup_error = None
        if cls.build.returncode == 0 and cls.images_env.is_file():
            cls.ref = cls.recorded().get("AP_IMAGE_CHECK", "")
        if cls.ref:
            try:
                cls.prepare()
            except Exception as e:  # reported by every test that needs it
                cls.setup_error = e

    @classmethod
    def prepare(cls):
        """Sign the same manifest under ap-engine, ap-page and ap-cli, make a second key pair, and push
        one unsigned digest under ap-page."""
        digest = cls.ref.split("@")[1]
        host = "127.0.0.1:%d" % cls.port
        cls.signed = {}
        for n in ("engine", "page", "cli"):
            tag = "%s/ap-%s:t" % (host, n)
            r = sh("docker", "buildx", "imagetools", "create", "-t", tag, "%s/ap-check@%s" % (host, digest))
            assert r.returncode == 0, r.stderr
            d = sh("docker", "buildx", "imagetools", "inspect", tag, "--format", "{{.Manifest.Digest}}").stdout.strip()
            assert re.fullmatch(DIGEST, d), d
            r = sh("docker", "run", "--rm", "--network", cls.net, "-e", "COSIGN_PASSWORD", "--read-only",
                   "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", "%d:%d" % (os.getuid(), os.getgid()),
                   "-v", "%s/cosign.key:/k/cosign.key:ro" % cls.signing,
                   "-v", "%s/signing-config.json:/k/signing-config.json:ro" % cls.signing, cls.cosign,
                   "sign", "--yes", "--key", "/k/cosign.key", "--signing-config", "/k/signing-config.json",
                   "--allow-http-registry", "%s:5000/ap-%s@%s" % (cls.name, n, d), env=cls.env)
            assert r.returncode == 0, r.stderr
            cls.signed[n] = "%s/ap-%s@%s" % (host, n, d)
        # unsigned: a different image pushed to the page repository
        meta = cls.base / "unsigned.json"
        tag = "%s/ap-page:unsigned" % host
        r = sh("docker", "buildx", "build", "--provenance=false", "--sbom=false", "--push", "-t", tag,
               "--metadata-file", str(meta), "-", input="FROM scratch\nLABEL unsigned=%s\n" % secrets.token_hex(4))
        assert r.returncode == 0, r.stderr
        cls.unsigned = "%s/ap-page@%s" % (host, json.loads(meta.read_text())["containerimage.digest"])
        assert cls.unsigned != cls.signed["page"]
        # another key pair, public half only, in its own config home
        other = cls.base / "other-key"
        other.mkdir()
        other.chmod(0o777)
        r = sh("docker", "run", "--rm", "--user", "%d:%d" % (os.getuid(), os.getgid()), "-e", "COSIGN_PASSWORD=other",
               "-v", "%s:/w" % other, "-w", "/w", cls.cosign, "generate-key-pair")
        assert r.returncode == 0, r.stderr
        cls.other_config = cls.base / "other-config"
        d = cls.other_config / "agentic-platform/signing"
        d.mkdir(parents=True)
        shutil.copy(other / "cosign.pub", d / "cosign.pub")
        assert (d / "cosign.pub").read_text() != (cls.signing / "cosign.pub").read_text()

    @classmethod
    def cleanup_docker(cls):
        for ref in cls.refs:
            sh("docker", "rmi", "-f", ref)
        remove_registry_objects(cls.name)
        after = snapshot_defaults()
        if after != cls.defaults_before:
            raise AssertionError("default-named registry objects changed during the test:\nbefore %s\nafter  %s"
                                 % (cls.defaults_before, after))

    @classmethod
    def recorded(cls, path=None):
        out = {}
        for line in (path or cls.images_env).read_text().splitlines():
            k, _, v = line.partition("=")
            out[k] = v.strip("'\"")
        return out

    def digest_ref(self):
        self.assertEqual(self.build.returncode, 0, "build.sh check failed:\n%s\n%s" % (self.build.stdout[-2000:], self.build.stderr[-3000:]))
        self.assertTrue(self.ref, "AP_IMAGE_CHECK missing from images.env")
        self.assertIsNone(self.setup_error, "test setup failed: %r" % (self.setup_error,))
        return self.ref

    # real `ap up`, with only `docker compose` intercepted
    def ap_up(self, images, config=None):
        """Run src/deploy/ap up in a fresh state dir whose images.env is `images` ({NAME: ref}).
        Returns (CompletedProcess, shim log text)."""
        d = Path(tempfile.mkdtemp(prefix="ap-run-", dir=self.base))
        state, projects, bin_ = d / "state", d / "projects", d / "bin"
        (state / "agentic-platform").mkdir(parents=True)
        projects.mkdir()
        bin_.mkdir()
        (state / "agentic-platform/images.env").write_text("".join("AP_IMAGE_%s=%s\n" % kv for kv in images.items()))
        cred = d / "models.env"
        cred.write_text("fake-model-key-SIGNINGTEST-0123456789\n")
        cred.chmod(0o600)
        shim = bin_ / "docker"
        shim.write_text(SHIM)
        shim.chmod(0o755)
        log = d / "compose.log"
        env = {**self.env, "XDG_STATE_HOME": str(state), "XDG_CONFIG_HOME": str(config or self.config),
               "PATH": "%s%s%s" % (bin_, os.pathsep, os.environ["PATH"]), "SHIM_LOG": str(log),
               "REAL_DOCKER": shutil.which("docker")}
        env.pop("AP_PAGE_PORT", None)
        r = sh("sh", str(AP), "up", "--credentials", str(cred), "--projects", str(projects),
               env=env, timeout=300, stdin=subprocess.DEVNULL)
        return r, (log.read_text() if log.exists() else "")

    def signed_images(self):
        return {"ENGINE": self.signed["engine"], "PAGE": self.signed["page"], "CLI": self.signed["cli"],
                "CHECK": self.ref}

    # build.sh
    def test_build_records_a_digest_reference(self):
        ref = self.digest_ref()
        self.assertRegex(ref, r"^127\.0\.0\.1:%d/ap-check@%s$" % (self.port, DIGEST))
        lines = [ln for ln in self.images_env.read_text().splitlines() if ln.strip()]
        self.assertEqual([ln.split("=")[0] for ln in lines], ["AP_IMAGE_CHECK"], "one line per name")
        self.assertEqual(stat.S_IMODE(self.images_env.stat().st_mode), 0o600)

    def test_signing_material_is_private_and_made_outside_the_repository(self):
        self.digest_ref()
        self.assertEqual(stat.S_IMODE(self.signing.stat().st_mode), 0o700)
        key = self.signing / "cosign.key"
        self.assertEqual(stat.S_IMODE(key.stat().st_mode), 0o600)
        self.assertIn("BEGIN PUBLIC KEY", (self.signing / "cosign.pub").read_text())
        cfg = json.loads((self.signing / "signing-config.json").read_text())
        self.assertNotIn(self.password.encode(), key.read_bytes(), "the key must be encrypted, not hold the password")
        # no transparency log service in the signing config: nothing leaves the machine
        self.assertFalse(cfg.get("rekorTlogUrls"), cfg)
        self.assertFalse(cfg.get("rekor_tlog_urls"), cfg)
        tracked = sh("git", "-C", str(REPO), "status", "--porcelain", "--ignored", "--", "src/deploy")
        self.assertNotIn("cosign.key", tracked.stdout)

    def test_registry_is_on_loopback_only_on_an_internal_network(self):
        self.digest_ref()
        r = sh("docker", "port", self.name)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(r.stdout.strip())
        for line in r.stdout.splitlines():
            self.assertIn("-> 127.0.0.1:%d" % self.port, line)
        net = json.loads(sh("docker", "network", "inspect", self.net).stdout)[0]
        self.assertTrue(net["Internal"], "%s must be an internal network" % self.net)
        self.assertEqual(sh("docker", "volume", "inspect", self.volume).returncode, 0)

    def test_image_has_sbom_and_provenance(self):
        ref = self.digest_ref()
        for what in ("SBOM", "Provenance"):
            r = sh("docker", "buildx", "imagetools", "inspect", ref, "--format", "{{json .%s}}" % what)
            self.assertEqual(r.returncode, 0, r.stderr)
            self.assertNotIn(r.stdout.strip(), ("", "null", "{}"), "no %s attached" % what)

    # verification: ap's own, against real Docker and the real registry
    def test_ap_up_accepts_the_signed_images_and_gives_compose_exactly_them(self):
        self.digest_ref()
        imgs = self.signed_images()
        r, log = self.ap_up(imgs)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(log.count("COMPOSE "), 1, log)
        for n, ref in imgs.items():
            self.assertIn("AP_IMAGE_%s=%s" % (n, ref), log)
        self.assertIn("AP_IMAGE_AGENT=ap-agent:not-built", log)  # not built: a placeholder, not verified
        self.assertIn("AP_IMAGE_TESTS=ap-tests:not-built", log)
        self.assertEqual(r.stderr.count("verified the signature"), 4, r.stderr)

    def test_ap_up_refuses_images_signed_with_another_key_pair(self):
        self.digest_ref()
        r, log = self.ap_up(self.signed_images(), config=self.other_config)
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertEqual(log, "", "Compose was called although verification failed")
        self.assertRegex(r.stderr, NOT_SIGNED)

    def test_ap_up_refuses_an_unsigned_digest_in_the_same_repository(self):
        self.digest_ref()
        imgs = {**self.signed_images(), "PAGE": self.unsigned}
        r, log = self.ap_up(imgs)
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertEqual(log, "", "Compose was called although verification failed")
        self.assertIn("page", r.stderr)
        self.assertRegex(r.stderr, NOT_SIGNED)


BARRIER_SHIM = """#!/bin/sh
# Stands in for chmod and mv. The first call of a process on a path of images.env waits (up to
# BARRIER_SECONDS) until another process has also reached one, then does the real thing. Without a
# lock both read the old images.env before either replaces it, so one build's line is lost; with
# one, the second cannot arrive while the first holds the lock and the first goes after the wait.
case "$0" in *chmod) real=/bin/chmod ;; *) real=/bin/mv ;; esac
hit=
for a in "$@"; do case $a in */images.env*) hit=1 ;; esac; done
if [ -n "$hit" ] && [ ! -e "$BARRIER_DIR/done.$PPID" ]; then
  : > "$BARRIER_DIR/done.$PPID"
  : > "$BARRIER_DIR/at.$PPID"
  i=0
  while [ "$(ls "$BARRIER_DIR" | grep -c '^at\\.')" -lt 2 ] && [ "$i" -lt "$BARRIER_SECONDS" ]; do
    sleep 1; i=$((i + 1))
  done
fi
exec "$real" "$@"
"""


class SigningHardeningTest(unittest.TestCase):
    """T058a, with real Docker and the real scripts: concurrent and repeated builds, password handling and
    the registry's convergence. Each test has its own signing and state folders under a key pair made
    once for the class; the registry is the class's own (AP_REGISTRY_NAME=ap-test-<random>)."""

    @classmethod
    def setUpClass(cls):
        if not shutil.which("docker") or sh("docker", "info").returncode != 0:
            raise AssertionError("docker is required for the signing tests and is not reachable")
        cls.defaults_before = snapshot_defaults()
        cls.cosign = pin("cosign")
        cls.registry_pin = pin("registry")
        cls.name = "ap-test-" + secrets.token_hex(4)
        cls.base = Path(tempfile.mkdtemp(prefix="test-signing-hardening-", dir=TMP_BASE))
        cls.addClassCleanup(shutil.rmtree, cls.base, ignore_errors=True)
        cls.addClassCleanup(cls.cleanup_docker)
        cls.port = free_port()
        cls.password = secrets.token_hex(16)
        cls.refs = set()
        # one key pair for the class, made here so that no test races to make it
        keys = cls.base / "keys"
        keys.mkdir()
        keys.chmod(0o777)
        r = sh("docker", "run", "--rm", "--user", "%d:%d" % (os.getuid(), os.getgid()), "-e", "COSIGN_PASSWORD=" + cls.password,
               "-v", "%s:/w" % keys, "-w", "/w", cls.cosign, "generate-key-pair")
        if r.returncode != 0:
            raise AssertionError("cannot make a key pair: %s" % r.stderr)
        cls.keys = keys
        cls.counter = 0

    @classmethod
    def cleanup_docker(cls):
        for ref in sorted(cls.refs):
            sh("docker", "rmi", "-f", ref)
        remove_registry_objects(cls.name)
        after = snapshot_defaults()
        if after != cls.defaults_before:
            raise AssertionError("default-named registry objects changed during the test:\nbefore %s\nafter  %s"
                                 % (cls.defaults_before, after))

    def folders(self, seed=True, port=None):
        """A fresh config and state home; with `seed` the class's key pair is already in the config."""
        type(self).counter += 1
        d = self.base / ("t%d" % self.counter)
        config, state = d / "config", d / "state"
        signing = config / "agentic-platform/signing"
        signing.mkdir(parents=True)
        state.mkdir()
        if seed:
            for f in ("cosign.key", "cosign.pub"):
                shutil.copy(self.keys / f, signing / f)
            (signing / "cosign.key").chmod(0o600)
        self.config, self.state, self.signing = config, state, signing
        self.images_env = state / "agentic-platform/images.env"
        return d

    def env(self, password="", port=None, **extra):
        e = {k: v for k, v in os.environ.items() if k not in ("COSIGN_PASSWORD",)}
        e.update(XDG_CONFIG_HOME=str(self.config), XDG_STATE_HOME=str(self.state),
                 AP_REGISTRY_PORT=str(port or self.port), AP_REGISTRY_NAME=self.name)
        if password is not None:
            e["COSIGN_PASSWORD"] = password or self.password
        e.update(extra)
        return e

    def build(self, image, **kw):
        kw.setdefault("timeout", 1500)
        r = sh("bash", str(BUILD), image, cwd=REPO, stdin=subprocess.DEVNULL, **kw)
        self.refs.add("127.0.0.1:%d/ap-%s:dev" % (self.port, image))
        return r

    def recorded(self):
        out = {}
        if self.images_env.is_file():
            for line in self.images_env.read_text().splitlines():
                if line.strip():
                    k, _, v = line.partition("=")
                    out[k] = v.strip("'\"")
        return out

    def assertBuilt(self, r, what):
        self.assertEqual(r.returncode, 0, "%s failed:\n%s\n%s" % (what, r.stdout[-1500:], r.stderr[-3000:]))

    # concurrent builds
    def test_two_builds_at_the_same_time_both_end_up_recorded(self):
        """`check` and `agent` are the two images that build offline from the pins (the others need
        generated code); both run at once, and a shim for chmod and mv holds each one at the moment it
        replaces images.env until the other is there too, so a build with no lock loses a line."""
        self.folders()
        # the registry is made first: two builds both making it at once is another matter
        self.assertEqual(self.ap_registry(self.port).returncode, 0)
        bar = self.base / ("barrier%d" % self.counter)
        bin_ = self.base / ("shim%d" % self.counter)
        bar.mkdir()
        bin_.mkdir()
        for n in ("chmod", "mv"):
            (bin_ / n).write_text(BARRIER_SHIM)
            (bin_ / n).chmod(0o755)
        env = self.env(PATH="%s%s%s" % (bin_, os.pathsep, os.environ["PATH"]), BARRIER_DIR=str(bar), BARRIER_SECONDS="40")
        procs = {}
        for image in ("check", "agent"):
            self.refs.add("127.0.0.1:%d/ap-%s:dev" % (self.port, image))
            procs[image] = subprocess.Popen(["bash", str(BUILD), image], cwd=REPO, env=env, stdin=subprocess.DEVNULL,
                                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        outs = {i: p.communicate(timeout=1800) for i, p in procs.items()}
        for i, p in procs.items():
            self.assertEqual(p.returncode, 0, "build %s failed:\n%s" % (i, outs[i][1][-3000:]))
        rec = self.recorded()
        self.assertEqual(sorted(rec), ["AP_IMAGE_AGENT", "AP_IMAGE_CHECK"], "a build's line was lost: %s" % rec)
        for k, v in rec.items():
            self.assertRegex(v, r"^127\.0\.0\.1:%d/ap-%s@%s$" % (self.port, k[len("AP_IMAGE_"):].lower(), DIGEST))
        self.assertEqual(stat.S_IMODE(self.images_env.stat().st_mode), 0o600)
        leftovers = [p.name for p in self.images_env.parent.iterdir() if p.name.startswith("images.env.") and p.name != "images.env"]
        self.assertEqual([n for n in leftovers if not n.endswith(".lock")], [], "temporary files left behind")
        # both builds also made one key pair's worth of signatures: the seeded pair is untouched
        self.assertEqual((self.signing / "cosign.pub").read_bytes(), (self.keys / "cosign.pub").read_bytes())

    # rebuilding
    def test_a_rebuild_replaces_that_names_line_and_keeps_the_key_pair(self):
        self.folders()
        self.assertBuilt(self.build("check", env=self.env()), "first build of check")
        self.assertBuilt(self.build("agent", env=self.env()), "build of agent")
        first = self.recorded()
        key, pub = (self.signing / "cosign.key").read_bytes(), (self.signing / "cosign.pub").read_bytes()
        self.assertBuilt(self.build("check", env=self.env()), "rebuild of check")
        again = self.recorded()
        lines = [ln for ln in self.images_env.read_text().splitlines() if ln.strip()]
        self.assertEqual(len(lines), 2, lines)
        self.assertEqual(sorted(again), ["AP_IMAGE_AGENT", "AP_IMAGE_CHECK"])
        self.assertEqual(again["AP_IMAGE_AGENT"], first["AP_IMAGE_AGENT"], "another image's line changed")
        self.assertNotEqual(again["AP_IMAGE_CHECK"], first["AP_IMAGE_CHECK"],
                            "a rebuild yields a new digest (SBOM and provenance carry the build's time)")
        self.assertEqual((self.signing / "cosign.key").read_bytes(), key, "the key pair was replaced")
        self.assertEqual((self.signing / "cosign.pub").read_bytes(), pub)
        self.assertEqual(stat.S_IMODE(self.images_env.stat().st_mode), 0o600)

    # the password
    def test_no_password_and_no_terminal_is_refused_before_anything_is_built(self):
        self.folders()
        images = lambda: set(sh("docker", "image", "ls", "-q", "--no-trunc", "127.0.0.1:%d/ap-check" % self.port).stdout.split())
        before = images()
        r = self.build("check", env=self.env(password=None), timeout=300)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("COSIGN_PASSWORD", r.stderr)
        self.assertEqual(self.recorded(), {})
        self.assertFalse(self.images_env.exists())
        self.assertEqual(images() - before, set(), "an image was built before the password was asked for")

    def test_a_wrong_password_is_refused_and_nothing_is_recorded(self):
        self.folders()
        wrong = "not-" + self.password
        r = self.build("check", env=self.env(password=wrong))
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertNotIn(wrong, r.stdout + r.stderr, "the password was printed")
        self.assertEqual(self.recorded(), {}, "an image whose signing failed was recorded")
        # a later build with the right one still works and records exactly its own line
        r = self.build("check", env=self.env())
        self.assertBuilt(r, "build with the right password")
        self.assertEqual(sorted(self.recorded()), ["AP_IMAGE_CHECK"])

    def test_a_wrong_password_leaves_the_other_recorded_images_alone(self):
        self.folders()
        self.assertBuilt(self.build("check", env=self.env()), "build of check")
        before = self.images_env.read_text()
        r = self.build("agent", env=self.env(password="not-" + self.password))
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertEqual(self.images_env.read_text(), before)

    # the registry
    def inspect(self, kind="container", name=None):
        r = sh("docker", kind, "inspect", name or self.name)
        return json.loads(r.stdout)[0] if r.returncode == 0 else None

    def ap_registry(self, port):
        return sh("sh", str(AP), "registry", env=self.env(port=port), stdin=subprocess.DEVNULL, timeout=180)

    def published(self, container):
        out = []
        for _, binds in (container["HostConfig"].get("PortBindings") or {}).items():
            out += [(b["HostIp"], b["HostPort"]) for b in binds]
        return out

    def assert_registry_networks(self, container):
        """<name>-net: internal and isolated (no host address); <name>-host: masquerade off; the registry
        is on these two and nothing else."""
        net, host = self.inspect("network", self.name + "-net"), self.inspect("network", self.name + "-host")
        self.assertIsNotNone(net)
        self.assertIsNotNone(host, "no %s-host network" % self.name)
        self.assertTrue(net["Internal"])
        self.assertEqual(net["Options"].get("com.docker.network.bridge.gateway_mode_ipv4"), "isolated", net["Options"])
        self.assertEqual(host["Options"].get("com.docker.network.bridge.enable_ip_masquerade"), "false", host["Options"])
        self.assertEqual(sorted(container["NetworkSettings"]["Networks"]), sorted([self.name + "-net", self.name + "-host"]))

    def test_ap_registry_replaces_an_old_style_registry_and_its_networks_and_keeps_the_data(self):
        """What the developer has today: a plain internal <name>-net and a registry on it and on the
        default bridge (made by the first version of `ap registry`)."""
        self.folders()
        self.addCleanup(self.ap_registry, self.port)
        sh("docker", "rm", "-f", self.name)  # start from what the developer has: the old-style objects only
        for n in ("-net", "-host"):
            sh("docker", "network", "rm", self.name + n)
        self.assertEqual(sh("docker", "network", "create", "--internal", self.name + "-net").returncode, 0)
        self.assertEqual(sh("docker", "volume", "create", self.name + "-data").returncode, 0)
        made = sh("docker", "create", "--name", self.name, "--label", "org.agentic-platform.registry=old", "--network", "bridge",
                  "-v", self.name + "-data:/var/lib/registry", "-p", "127.0.0.1:%d:5000" % self.port, self.registry_pin)
        self.assertEqual(made.returncode, 0, made.stderr)
        self.assertEqual(sh("docker", "network", "connect", self.name + "-net", self.name).returncode, 0)
        self.assertEqual(sh("docker", "start", self.name).returncode, 0)
        old = self.inspect()
        vol = self.inspect("volume", self.name + "-data")
        self.assertIn("bridge", old["NetworkSettings"]["Networks"])
        self.assertNotIn("com.docker.network.bridge.gateway_mode_ipv4", self.inspect("network", self.name + "-net")["Options"])
        r = self.ap_registry(self.port)
        self.assertEqual(r.returncode, 0, r.stderr)
        new = self.inspect()
        self.assertNotEqual(new["Id"], old["Id"], "the old registry container was kept")
        self.assertTrue(new["State"]["Running"])
        self.assert_registry_networks(new)
        self.assertEqual(self.published(new), [("127.0.0.1", str(self.port))])
        self.assertEqual(self.inspect("volume", self.name + "-data")["CreatedAt"], vol["CreatedAt"],
                         "the registry's data volume was made again")

    def test_ap_registry_converges_and_replaces_a_registry_with_another_port_or_pin(self):
        self.folders()
        self.addCleanup(self.ap_registry, self.port)  # later tests build against self.port
        first = self.ap_registry(self.port)
        self.assertEqual(first.returncode, 0, first.stderr)
        c1 = self.inspect()
        self.assertEqual(self.published(c1), [("127.0.0.1", str(self.port))])
        self.assertNotIn("bridge", c1["NetworkSettings"]["Networks"], "the registry is on Docker's default bridge")
        self.assert_registry_networks(c1)
        vol = self.inspect("volume", self.name + "-data")
        self.assertIsNotNone(vol)
        # the same again: nothing changes
        self.assertEqual(self.ap_registry(self.port).returncode, 0)
        c2 = self.inspect()
        self.assertEqual((c2["Id"], c2["State"]["StartedAt"]), (c1["Id"], c1["State"]["StartedAt"]),
                         "an unchanged registry was replaced or restarted")
        # another port: replaced, published on the new port only, data kept
        other = free_port()
        self.assertEqual(self.ap_registry(other).returncode, 0)
        c3 = self.inspect()
        self.assertNotEqual(c3["Id"], c1["Id"])
        self.assertEqual(self.published(c3), [("127.0.0.1", str(other))])
        self.assertTrue(c3["State"]["Running"])
        vol3 = self.inspect("volume", self.name + "-data")
        self.assertEqual(vol3["CreatedAt"], vol["CreatedAt"], "the registry's data volume was made again")
        # another pin (or any other run flags): a container of the same name made from the registry
        # image but not by `ap registry` is replaced as well
        sh("docker", "rm", "-f", self.name)
        made = sh("docker", "create", "--name", self.name, self.registry_pin)
        self.assertEqual(made.returncode, 0, made.stderr)
        self.assertEqual(self.ap_registry(other).returncode, 0)
        c4 = self.inspect()
        self.assertNotEqual(c4["Id"], made.stdout.strip())
        self.assertEqual(self.published(c4), [("127.0.0.1", str(other))])
        self.assertTrue(c4["State"]["Running"])

    # the record lock (build.sh: the directory <state>/images.env.lock holding the owner's pid)
    def lock_dir(self):
        d = self.images_env.parent / "images.env.lock"
        self.images_env.parent.mkdir(parents=True, exist_ok=True)
        return d

    def test_a_stale_lock_of_a_dead_process_is_removed_and_the_build_succeeds(self):
        self.folders()
        dead = subprocess.Popen(["true"])
        dead.wait()
        lock = self.lock_dir()
        lock.mkdir()
        (lock / "pid").write_text("%d\n" % dead.pid)
        r = self.build("check", env=self.env())
        self.assertBuilt(r, "build with a stale lock")
        self.assertRegex(r.stderr, STALE)
        self.assertEqual(sorted(self.recorded()), ["AP_IMAGE_CHECK"])
        self.assertFalse(lock.exists(), "the lock was left behind")

    def test_a_lock_without_a_pid_yet_is_waited_for_not_removed(self):
        """A lock being taken has no pid file for a moment; it is released after 40 s, and the build must
        not have finished before that nor have called it stale."""
        self.folders()
        lock = self.lock_dir()
        lock.mkdir()
        timer = threading.Timer(40, lambda: shutil.rmtree(lock, ignore_errors=True))
        timer.start()
        self.addCleanup(timer.cancel)
        start = time.monotonic()
        r = self.build("check", env=self.env())
        elapsed = time.monotonic() - start
        self.assertBuilt(r, "build behind a lock being taken")
        self.assertGreaterEqual(elapsed, 39, "the build did not wait for the lock (%.0f s)" % elapsed)
        self.assertNotIn("stale", r.stderr)
        self.assertEqual(sorted(self.recorded()), ["AP_IMAGE_CHECK"])

    def test_a_lock_of_a_running_process_is_waited_for_then_the_build_proceeds(self):
        """The owner is this test process, which stays alive: the build waits (it is not stale) and goes on
        once the lock is released after 20 s. The 120 s timeout itself is not tested (a constant, 2 minutes)."""
        self.folders()
        lock = self.lock_dir()
        lock.mkdir()
        (lock / "pid").write_text("%d\n" % os.getpid())
        timer = threading.Timer(20, lambda: shutil.rmtree(lock, ignore_errors=True))
        timer.start()
        self.addCleanup(timer.cancel)
        start = time.monotonic()
        r = self.build("check", env=self.env())
        elapsed = time.monotonic() - start
        self.assertBuilt(r, "build behind a running owner's lock")
        self.assertGreaterEqual(elapsed, 19, "the build did not wait for a lock whose owner is running (%.0f s)" % elapsed)
        self.assertNotRegex(r.stderr, STALE)
        self.assertEqual(sorted(self.recorded()), ["AP_IMAGE_CHECK"])

    def test_several_builds_behind_a_dead_owners_lock_remove_it_exactly_once(self):
        self.folders()
        self.assertEqual(self.ap_registry(self.port).returncode, 0)
        dead = subprocess.Popen(["true"])
        dead.wait()
        lock = self.lock_dir()
        lock.mkdir()
        (lock / "pid").write_text("%d\n" % dead.pid)
        env = self.env()
        procs = {}
        for image in ("check", "agent"):
            self.refs.add("127.0.0.1:%d/ap-%s:dev" % (self.port, image))
            procs[image] = subprocess.Popen(["bash", str(BUILD), image], cwd=REPO, env=env, stdin=subprocess.DEVNULL,
                                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        outs = {i: p.communicate(timeout=1800)[1] for i, p in procs.items()}
        for i, p in procs.items():
            self.assertEqual(p.returncode, 0, "build %s failed:\n%s" % (i, outs[i][-3000:]))
        self.assertEqual(sum(len(re.findall(STALE, o)) for o in outs.values()), 1, outs)
        self.assertEqual(sorted(self.recorded()), ["AP_IMAGE_AGENT", "AP_IMAGE_CHECK"])
        self.assertFalse(lock.exists())

    # an older recorded digest after a rebuild
    def test_up_pulls_a_recorded_older_digest_back_from_the_local_registry(self):
        """build check, keep its digest, rebuild (a new digest replaces the tag), sign the older digest's
        manifest under the engine, page and cli names, remove every local copy, and run the real `ap up`
        (only `docker compose` shimmed) with the older digests recorded."""
        self.folders()
        self.assertBuilt(self.build("check", env=self.env()), "first build of check")
        old = self.recorded()["AP_IMAGE_CHECK"]
        self.assertBuilt(self.build("check", env=self.env()), "rebuild of check")
        new = self.recorded()["AP_IMAGE_CHECK"]
        self.assertNotEqual(old, new)
        host = "127.0.0.1:%d" % self.port
        digest = old.split("@")[1]
        refs = {"CHECK": old}
        for n in ("engine", "page", "cli"):
            tag = "%s/ap-%s:t" % (host, n)
            self.refs.add(tag)
            r = sh("docker", "buildx", "imagetools", "create", "-t", tag, "%s/ap-check@%s" % (host, digest))
            self.assertEqual(r.returncode, 0, r.stderr)
            d = sh("docker", "buildx", "imagetools", "inspect", tag, "--format", "{{.Manifest.Digest}}").stdout.strip()
            r = sh("docker", "run", "--rm", "--network", self.name + "-net", "-e", "COSIGN_PASSWORD", "--read-only",
                   "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", "%d:%d" % (os.getuid(), os.getgid()),
                   "-v", "%s/cosign.key:/k/cosign.key:ro" % self.signing,
                   "-v", "%s/signing-config.json:/k/signing-config.json:ro" % self.signing, self.cosign,
                   "sign", "--yes", "--key", "/k/cosign.key", "--signing-config", "/k/signing-config.json",
                   "--allow-http-registry", "%s:5000/ap-%s@%s" % (self.name, n, d), env=self.env())
            self.assertEqual(r.returncode, 0, r.stderr)
            refs[n.upper()] = "%s/ap-%s@%s" % (host, n, d)
        for ref in refs.values():
            self.refs.add(ref)
            sh("docker", "rmi", "-f", ref)
            self.assertNotEqual(sh("docker", "image", "inspect", ref).returncode, 0, "%s is still in the local store" % ref)
        r, log = self.ap_up(refs)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(log.count("COMPOSE "), 1, log)
        for n, ref in refs.items():
            self.assertIn("AP_IMAGE_%s=%s" % (n, ref), log)
            self.assertEqual(sh("docker", "image", "inspect", ref).returncode, 0,
                             "%s was not pulled back, so Compose would fail with No such image" % ref)

    def ap_up(self, images):
        d = Path(tempfile.mkdtemp(prefix="ap-run-", dir=self.base))
        state, projects, bin_ = d / "state", d / "projects", d / "bin"
        (state / "agentic-platform").mkdir(parents=True)
        projects.mkdir()
        bin_.mkdir()
        (state / "agentic-platform/images.env").write_text("".join("AP_IMAGE_%s=%s\n" % kv for kv in images.items()))
        cred = d / "models.env"
        cred.write_text("fake-model-key-SIGNINGTEST-0123456789\n")
        cred.chmod(0o600)
        shim = bin_ / "docker"
        shim.write_text(SHIM)
        shim.chmod(0o755)
        log = d / "compose.log"
        env = self.env(XDG_STATE_HOME=str(state), PATH="%s%s%s" % (bin_, os.pathsep, os.environ["PATH"]),
                       SHIM_LOG=str(log), REAL_DOCKER=shutil.which("docker"))
        env.pop("AP_PAGE_PORT", None)
        r = sh("sh", str(AP), "up", "--credentials", str(cred), "--projects", str(projects), env=env,
               timeout=300, stdin=subprocess.DEVNULL)
        return r, (log.read_text() if log.exists() else "")


if __name__ == "__main__":
    unittest.main()
