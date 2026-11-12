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
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
BUILD = REPO / "src/deploy/build.sh"
LOCK = REPO / "src/deploy/images.lock"
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
NOT_SIGNED = r"no (matching )?(signatures|attestations)"  # cosign v3 wording for a refused or unsigned digest
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
        sh("docker", "rm", "-f", cls.name)
        sh("docker", "volume", "rm", "-f", cls.volume)
        sh("docker", "network", "rm", cls.net)
        for ref in cls.refs:
            sh("docker", "rmi", "-f", ref)
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
        r = sh("sh", str(REPO / "src/deploy/ap"), "up", "--credentials", str(cred), "--projects", str(projects),
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


if __name__ == "__main__":
    unittest.main()
