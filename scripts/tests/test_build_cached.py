"""Tests for the cached digest of `src/deploy/build.sh --changed` (feature 003, task T029).

Judges T030. Rules: specs/003-one-command-start/research.md R1 ("Cached digest" bullet and its
Order, Which and Failure sub-bullets; sources S11, S12), data-model.md "Image fingerprint
(`built.env`)" and "Cached digest (`cached.env`)", docs/decisions/0064, and SC-002 (a second start
with nothing changed under 30 s).

Two classes:
- CachedDigestTest: the start stub (fixtures/start/docker-stub, unchanged) behind a small wrapper
  that gives each KIND of build of an image its own digest (so a test can tell which build a
  recorded digest came from) and can fail only the cached build. The real build.sh, lock.sh and ap
  run from a copy of src/deploy (AP_TEST_DEPLOY_DIR names another source copy, to see the tests
  fail against it). Nothing touches a real Docker engine.
- CachedDigestRealBuildKitTest: after `build.sh cli`, two `--changed cli` runs build nothing and
  leave cached.env unchanged. Real Docker and BuildKit, its own registry; skipped, with its
  reason, when Docker is not reachable, as test_build_changed.py is. CI's py_tests job runs it.

How the builds are told apart by their argv (build.sh passes them as `--output <value>`):
- candidate:    type=image with rewrite-timestamp=true, no --push
- cached build: type=image without rewrite-timestamp, no --push
- signed build: --push

Test names and the T029 cases:
(a) test_a_signed_build_writes_cached_env_with_digest_and_fingerprint
    test_a_plain_build_writes_cached_env_too
(b) test_changed_with_matching_pairs_makes_only_cached_builds_and_records_nothing
    test_changed_check_with_matching_pairs_makes_only_cached_builds
    test_start_with_matching_pairs_makes_only_cached_builds
(c) test_a_pair_that_does_not_hold_falls_back_to_the_candidate (subtests: digest, fingerprint,
    missing line, malformed line, recorded image missing from the store)
(d) test_a_current_image_found_by_its_candidate_gets_a_new_pair
    test_changed_check_writes_no_pair
(e) test_each_record_file_keeps_only_its_own_lines
(f) test_every_cached_build_comes_before_every_candidate
    test_the_pair_holds_the_cached_digest_of_the_run_and_the_candidate_as_fingerprint
    test_check_without_pairs_makes_candidates_alone
(g) test_a_failing_cached_build_writes_no_pair_and_the_candidate_decides
    test_a_failing_build_is_reported_as_before_and_writes_no_pair
(h) test_ap_down_keeps_cached_env
Real BuildKit: CachedDigestRealBuildKitTest.test_two_changed_runs_after_a_build_build_nothing
"""
import hashlib
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
DEPLOY_SRC = Path(os.environ.get("AP_TEST_DEPLOY_DIR") or REPO / "src/deploy")
STUB = HERE / "fixtures/start/docker-stub"
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
SEVEN = ["engine", "cli", "page", "agent", "check", "project", "lint"]
PASSWORD = "test-only-signing-password"
SECRET = "fake-model-key-CACHEDSECRET-1a2b3c4d5e6f"
DIGEST = r"sha256:[0-9a-f]{64}"


def digest(name, kind):
    """The digest the wrapper gives a build of <name> of this kind: cand, cached or push."""
    return "sha256:" + hashlib.sha256(("ap-%s-%s" % (name, kind)).encode()).hexdigest()


def image_ref(name):
    return "127.0.0.1:5005/ap-%s@%s" % (name, digest(name, "push"))


# Stands in for `docker`: tells the kind of a `buildx build` from its argv, gives it that kind's
# digest, can fail only the cached build of the images named in WRAP_FAIL_CACHED, and hands every
# call to the recording stub (which logs the call and writes the metadata file).
WRAPPER = r'''#!/usr/bin/env python3
import hashlib, os, re, sys
argv = sys.argv[1:]
if argv[:2] == ["buildx", "build"]:
    def opt(n):
        for i, a in enumerate(argv):
            if a == n and i + 1 < len(argv):
                return argv[i + 1]
            if a.startswith(n + "="):
                return a[len(n) + 1:]
    m = re.search(r"Dockerfile\.([a-z]+)$", opt("-f") or "")
    name = m.group(1) if m else "unknown"
    out = opt("--output") or ""
    kind = "push" if "--push" in argv else ("cand" if "rewrite-timestamp=true" in out else "cached")
    if kind == "cached" and name in os.environ.get("WRAP_FAIL_CACHED", "").split():
        with open(os.environ["STUB_LOG"], "a") as f:
            f.write('{"argv": %s, "env": {}}\n' % __import__("json").dumps(argv))
        sys.stderr.write("ERROR: failed to solve: the cached build of %s failed\n" % name)
        sys.exit(1)
    var = "STUB_BUILD_DIGEST_" + name.upper()
    if var not in os.environ:
        os.environ[var] = "sha256:" + hashlib.sha256(("ap-%s-%s" % (name, kind)).encode()).hexdigest()
os.execv(os.path.join(os.path.dirname(os.path.realpath(__file__)), "stub-docker"),
         ["stub-docker"] + argv)
'''


class CachedCase(unittest.TestCase):
    def setUp(self):
        self.base = Path(tempfile.mkdtemp(prefix="test-build-cached-", dir=TMP_BASE))
        self.addCleanup(shutil.rmtree, self.base, ignore_errors=True)
        self.deploy = self.base / "repo/src/deploy"
        shutil.copytree(DEPLOY_SRC, self.deploy, ignore=shutil.ignore_patterns("__pycache__"))
        gen = self.base / "repo/scripts/generate.sh"
        gen.parent.mkdir(parents=True)
        gen.write_text("#!/bin/sh\nexit 0\n")  # nothing stale: build.sh never writes into the tree
        gen.chmod(0o755)
        self.bin = self.base / "bin"
        self.bin.mkdir()
        shutil.copy2(STUB, self.bin / "stub-docker")
        (self.bin / "docker").write_text(WRAPPER)
        (self.bin / "docker").chmod(0o755)
        self.home = self.base / "home"
        self.home.mkdir()
        self.state = self.base / "state"
        self.config = self.base / "config"
        self.log = self.base / "docker.log"
        self.cred = self.base / "models.env"
        self.cred.write_text(SECRET + "\n")
        self.cred.chmod(0o600)
        self.proj = self.base / "projects"
        self.proj.mkdir()
        self.proj.chmod(0o755)
        self.sdir = self.state / "agentic-platform"
        self.sdir.mkdir(parents=True)
        self.sdir.chmod(0o700)
        self.signing = self.config / "agentic-platform/signing"
        self.signing.mkdir(parents=True)
        (self.signing / "cosign.pub").write_text("-----BEGIN PUBLIC KEY-----\nfake\n-----END PUBLIC KEY-----\n")
        (self.signing / "cosign.key").write_text("FAKE-PRIVATE-KEY\n")
        (self.signing / "cosign.key").chmod(0o600)
        (self.signing / "signing-config.json").write_text("{}\n")
        self.images_env = self.sdir / "images.env"
        self.built_env = self.sdir / "built.env"
        self.cached_env = self.sdir / "cached.env"
        self.session = self.sdir / "session.env"

    # ---- state
    def write_env(self, path, text):
        path.write_text(text)
        path.chmod(0o600)

    def make_current(self, names, pairs=True):
        """images.env, built.env (the candidate digest) and, with pairs, cached.env as signed builds left them."""
        self.write_env(self.images_env, "".join("AP_IMAGE_%s=%s\n" % (n.upper(), image_ref(n)) for n in names))
        self.write_env(self.built_env, "".join("AP_BUILT_%s=%s\n" % (n.upper(), digest(n, "cand")) for n in names))
        if pairs:
            self.write_env(self.cached_env, "".join(
                "AP_CACHED_%s=%s %s\n" % (n.upper(), digest(n, "cached"), digest(n, "cand")) for n in names))

    def snapshot(self):
        return {p.name: (p.read_bytes(), stat.S_IMODE(p.stat().st_mode))
                for p in (self.images_env, self.built_env, self.cached_env) if p.exists()}

    # ---- running
    def env(self, **extra):
        e = {k: v for k, v in os.environ.items() if not k.startswith(("AP_", "STUB_", "COSIGN_", "WRAP_"))}
        e.update(HOME=str(self.home), XDG_STATE_HOME=str(self.state), XDG_CONFIG_HOME=str(self.config),
                 STUB_LOG=str(self.log), PATH=str(self.bin) + os.pathsep + os.environ.get("PATH", ""))
        e.update({k: str(v) for k, v in extra.items()})
        return e

    def build(self, *args, **extra):
        extra.setdefault("COSIGN_PASSWORD", PASSWORD)
        return subprocess.run(["bash", str(self.deploy / "build.sh"), *args], env=self.env(**extra),
                              capture_output=True, text=True, timeout=120, stdin=subprocess.DEVNULL)

    def changed(self, *names, check=False, **extra):
        return self.build("--changed", *(["--check"] if check else []), *names, **extra)

    def ap(self, *args, **extra):
        return subprocess.run(["sh", str(self.deploy / "ap"), *args], env=self.env(**extra),
                              capture_output=True, text=True, timeout=120, stdin=subprocess.DEVNULL)

    # ---- what was called
    def argvs(self):
        if not self.log.exists():
            return []
        return [json.loads(l)["argv"] for l in self.log.read_text().splitlines() if l]

    @staticmethod
    def output_of(argv):
        for i, a in enumerate(argv):
            if a == "--output" and i + 1 < len(argv):
                return argv[i + 1]
            if a.startswith("--output="):
                return a[9:]
        return ""

    def builds(self):
        """(index, name, kind) of every `docker buildx build`; kind is cand, cached or push."""
        out = []
        for i, a in enumerate(self.argvs()):
            if a[:2] != ["buildx", "build"]:
                continue
            m = re.search(r"Dockerfile\.([a-z]+)$", a[a.index("-f") + 1] if "-f" in a else "")
            o = self.output_of(a)
            if "--push" in a:
                kind = "push"
            elif "rewrite-timestamp=true" in o:
                kind = "cand"
            elif re.search(r"(^|,)type=image(,|$)", o) and "rewrite-timestamp" not in o:
                kind = "cached"
            else:
                kind = "other"
            out.append((i, m.group(1) if m else None, kind))
        return out

    def of_kind(self, kind):
        return [n for _, n, k in self.builds() if k == kind]

    def signs(self):
        return [a for a in self.argvs() if a[:1] == ["run"] and "sign" in a]

    def assertNothingRecordedPushedOrSigned(self, before):
        self.assertEqual(self.of_kind("push"), [], "an image was pushed")
        self.assertEqual(self.signs(), [], "something was signed")
        self.assertEqual(self.snapshot(), before, "a record file changed")

    def cached_lines(self):
        if not self.cached_env.exists():
            return {}
        return dict(l.split("=", 1) for l in self.cached_env.read_text().splitlines() if l)


class CachedDigestTest(CachedCase):
    # (a)
    def test_a_signed_build_writes_cached_env_with_digest_and_fingerprint(self):
        """Data model `cached.env` (Written: every signed build); research R1 Cached digest: each signed
        build records its cached build's digest paired with the fingerprint (= AP_BUILT_<NAME>),
        mode 600, one line per image signed."""
        r = self.changed("engine", "cli")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(self.cached_env.exists(), "cached.env was not written")
        self.assertEqual(stat.S_IMODE(self.cached_env.stat().st_mode), 0o600)
        built = dict(l.split("=", 1) for l in self.built_env.read_text().splitlines())
        lines = self.cached_lines()
        self.assertEqual(sorted(lines), ["AP_CACHED_CLI", "AP_CACHED_ENGINE"])
        for n in ("engine", "cli"):
            d, fp = lines["AP_CACHED_%s" % n.upper()].split(" ")
            self.assertRegex(d, "^%s$" % DIGEST)
            self.assertEqual(fp, built["AP_BUILT_%s" % n.upper()], "the second value is the fingerprint")

    def test_a_plain_build_writes_cached_env_too(self):
        """Research R1 Which: a plain `build.sh <name>` makes the cached build, then the candidate, then
        the signed build, and records the pair like a signed --changed build."""
        r = self.build("cli")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual([k for _, n, k in self.builds() if n == "cli"], ["cached", "cand", "push"])
        self.assertEqual(self.cached_lines().get("AP_CACHED_CLI"), "%s %s" % (digest("cli", "cached"), digest("cli", "cand")))
        self.assertEqual(stat.S_IMODE(self.cached_env.stat().st_mode), 0o600)

    # (b)
    def test_changed_with_matching_pairs_makes_only_cached_builds_and_records_nothing(self):
        """Research R1 Cached digest: with the pair's digest equal to the cached build's, its fingerprint
        equal to AP_BUILT and the image in the store, the image is current with no candidate: one build
        without rewrite-timestamp per image, none with it; nothing pushed, signed or recorded (SC-002)."""
        self.make_current(SEVEN)
        before = self.snapshot()
        r = self.changed(*SEVEN)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stderr.strip(), "", "nothing to build prints nothing")
        self.assertEqual(sorted(self.of_kind("cached")), sorted(SEVEN))
        self.assertEqual(self.of_kind("cand"), [], "no candidate build")
        self.assertEqual(len(self.builds()), len(SEVEN))
        self.assertNothingRecordedPushedOrSigned(before)

    def test_changed_check_with_matching_pairs_makes_only_cached_builds(self):
        """Research R1 Which and the contract (T032): `--check` also makes the cached build of an image
        with a pair, and no candidate when it settles the image; prints `nothing to build`, exit 0."""
        self.make_current(SEVEN)
        before = self.snapshot()
        r = self.changed(*SEVEN, check=True)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("build: nothing to build", r.stderr)
        self.assertEqual(sorted(self.of_kind("cached")), sorted(SEVEN))
        self.assertEqual(self.of_kind("cand"), [])
        self.assertNothingRecordedPushedOrSigned(before)

    def test_start_with_matching_pairs_makes_only_cached_builds(self):
        """SC-002 through `ap start`: with every image current and a matching pair, start builds each
        image once without rewrite-timestamp and never with it (research R1)."""
        self.make_current(SEVEN)
        r = self.ap("start", "--credentials", str(self.cred), "--projects", str(self.proj))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(sorted(self.of_kind("cached")), sorted(SEVEN))
        self.assertEqual(self.of_kind("cand"), [])
        self.assertEqual(self.of_kind("push"), [])

    # (c)
    def test_a_pair_that_does_not_hold_falls_back_to_the_candidate(self):
        """Research R1 Cached digest ("Otherwise the candidate decides") and the data model Rule: a pair
        whose digest differs, whose fingerprint is not AP_BUILT, a missing or malformed line, or a
        recorded image missing from the store makes the image's candidate (a build with
        rewrite-timestamp) decide as before. The image is current by its fingerprint, so nothing is
        pushed (except the missing-image case, where it is not current)."""
        cached, cand = digest("cli", "cached"), digest("cli", "cand")
        other = "sha256:" + "0" * 64
        cases = {
            "digest differs": ("AP_CACHED_CLI=%s %s\n" % (other, cand), {}),
            "fingerprint is not AP_BUILT": ("AP_CACHED_CLI=%s %s\n" % (cached, other), {}),
            "missing line": ("AP_CACHED_PAGE=%s %s\n" % (digest("page", "cached"), digest("page", "cand")), {}),
            "malformed line": ("AP_CACHED_CLI=not-a-digest\n", {}),
            "digest only": ("AP_CACHED_CLI=%s\n" % cached, {}),
            "image missing from the store": ("AP_CACHED_CLI=%s %s\n" % (cached, cand), {"STUB_IMAGE_MISSING": "1"}),
        }
        for label, (line, extra) in cases.items():
            with self.subTest(label):
                if self.log.exists():
                    self.log.unlink()
                self.make_current(["cli"], pairs=False)
                self.write_env(self.cached_env, line)
                r = self.changed("cli", **extra)
                self.assertEqual(r.returncode, 0, r.stderr)
                self.assertEqual(self.of_kind("cached"), ["cli"], "the cached build was made: %s" % self.builds())
                self.assertEqual(self.of_kind("cand"), ["cli"], "the candidate decided: %s" % self.builds())
                if "missing from the store" in label:
                    self.assertEqual(self.of_kind("push"), ["cli"], "not in the store: built again")
                else:
                    self.assertEqual(self.of_kind("push"), [], "current by its fingerprint: nothing pushed")

    # (d)
    def test_a_current_image_found_by_its_candidate_gets_a_new_pair(self):
        """Data model `cached.env` Written, research R1 Which: when the candidate finds an image current,
        `--changed` writes a new pair, the digest from the cached build made before the candidate,
        the fingerprint AP_BUILT; nothing is pushed or signed, images.env and built.env are unchanged."""
        self.make_current(["cli"], pairs=False)
        self.write_env(self.cached_env, "AP_CACHED_CLI=sha256:%s %s\n" % ("0" * 64, digest("cli", "cand")))
        images, built = self.images_env.read_bytes(), self.built_env.read_bytes()
        r = self.changed("cli")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.of_kind("cand"), ["cli"])
        self.assertEqual(self.of_kind("push"), [])
        self.assertEqual(self.signs(), [])
        self.assertEqual(self.cached_lines(), {"AP_CACHED_CLI": "%s %s" % (digest("cli", "cached"), digest("cli", "cand"))})
        self.assertEqual((self.images_env.read_bytes(), self.built_env.read_bytes()), (images, built))
        self.assertEqual(stat.S_IMODE(self.cached_env.stat().st_mode), 0o600)

    def test_changed_check_writes_no_pair(self):
        """Research R1 Cached digest and data model: `--check` records nothing. A current image with no
        pair, and one with a pair that does not hold, leave cached.env as it was (absent, unchanged)."""
        self.make_current(["cli", "page"], pairs=False)
        before = self.snapshot()
        r = self.changed("cli", "page", check=True)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertNothingRecordedPushedOrSigned(before)
        self.assertFalse(self.cached_env.exists(), "--check made cached.env")
        stale = "AP_CACHED_CLI=sha256:%s %s\n" % ("0" * 64, digest("cli", "cand"))
        self.write_env(self.cached_env, stale)
        before = self.snapshot()
        self.log.unlink()
        r = self.changed("cli", "page", check=True)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.of_kind("cached"), ["cli"], "only the image with a pair gets a cached build")
        self.assertEqual(self.of_kind("cand").count("cli"), 1, "the candidate decided for cli")
        self.assertEqual(self.snapshot(), before)

    # (e)
    def test_each_record_file_keeps_only_its_own_lines(self):
        """Data model: images.env keeps only AP_IMAGE_* lines, built.env only AP_BUILT_* and cached.env only
        AP_CACHED_* (`ap up` refuses another line in images.env), after a build and after a run
        that renews a pair."""
        def only(path, prefix):
            lines = path.read_text().splitlines()
            self.assertTrue(lines, "%s is empty" % path.name)
            for l in lines:
                self.assertRegex(l, r"^%s[A-Z]+=" % prefix, "%s holds a line that is not its own" % path.name)
        r = self.changed("engine", "cli")
        self.assertEqual(r.returncode, 0, r.stderr)
        for p, pre in ((self.images_env, "AP_IMAGE_"), (self.built_env, "AP_BUILT_"), (self.cached_env, "AP_CACHED_")):
            only(p, pre)
        self.write_env(self.cached_env, "AP_CACHED_CLI=sha256:%s %s\n" % ("0" * 64, digest("cli", "cand")))
        r = self.changed("engine", "cli")
        self.assertEqual(r.returncode, 0, r.stderr)
        for p, pre in ((self.images_env, "AP_IMAGE_"), (self.built_env, "AP_BUILT_"), (self.cached_env, "AP_CACHED_")):
            only(p, pre)

    # (f)
    def test_every_cached_build_comes_before_every_candidate(self):
        """Research R1 Order: a run makes every cached build first, then the candidates it still needs,
        then the signed builds. Here page has no pair and cli's does not hold, so both need a candidate."""
        self.make_current(["engine", "cli", "page"], pairs=False)
        self.write_env(self.cached_env, "AP_CACHED_CLI=sha256:%s %s\n" % ("0" * 64, digest("cli", "cand")))
        r = self.changed("engine", "cli", "page")
        self.assertEqual(r.returncode, 0, r.stderr)
        kinds = [k for _, _, k in self.builds()]
        self.assertEqual(sorted(self.of_kind("cached")), ["cli", "engine", "page"])
        self.assertTrue(self.of_kind("cand"), "a candidate was needed")
        last_cached = max(i for i, _, k in self.builds() if k == "cached")
        first_cand = min(i for i, _, k in self.builds() if k == "cand")
        self.assertLess(last_cached, first_cand, "cached builds first: %s" % kinds)
        # and with an image to sign: its signed build comes after every candidate
        self.log.unlink()
        self.make_current(["engine", "page"], pairs=False)  # cli not recorded: it must be built
        r = self.changed("engine", "cli", "page")
        self.assertEqual(r.returncode, 0, r.stderr)
        b = self.builds()
        self.assertLess(max(i for i, _, k in b if k == "cached"), min(i for i, _, k in b if k == "cand"))
        self.assertLess(max(i for i, _, k in b if k == "cand"), min(i for i, _, k in b if k == "push"))

    def test_the_pair_holds_the_cached_digest_of_the_run_and_the_candidate_as_fingerprint(self):
        """Research R1 Order: the pair is the cached digest taken before that image's candidate (here
        recognisable by the wrapper's per-kind digests), and its fingerprint is the candidate's, which
        is also AP_BUILT; neither is the pushed image's digest."""
        r = self.changed("cli")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.cached_lines(), {"AP_CACHED_CLI": "%s %s" % (digest("cli", "cached"), digest("cli", "cand"))})
        self.assertEqual(self.built_env.read_text().strip(), "AP_BUILT_CLI=%s" % digest("cli", "cand"))
        self.assertNotIn(digest("cli", "push"), self.cached_env.read_text())

    def test_check_without_pairs_makes_candidates_alone(self):
        """Research R1 Which: `--check` makes a cached build only for an image with a pair, so with no
        cached.env it makes the candidates alone, as before."""
        self.make_current(SEVEN, pairs=False)
        before = self.snapshot()
        r = self.changed(*SEVEN, check=True)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.of_kind("cached"), [], "no cached build without a pair")
        self.assertEqual(sorted(self.of_kind("cand")), sorted(SEVEN))
        self.assertNothingRecordedPushedOrSigned(before)

    # (g)
    def test_a_failing_cached_build_writes_no_pair_and_the_candidate_decides(self):
        """Research R1 Failure: a cached build that fails counts as no pair and records none; the
        candidate then runs and decides. cli's cached build fails, page's does not."""
        self.make_current(["cli", "page"], pairs=False)
        self.write_env(self.cached_env, "AP_CACHED_CLI=sha256:%s %s\n" % ("0" * 64, digest("cli", "cand")))
        r = self.changed("cli", "page", WRAP_FAIL_CACHED="cli")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("cli", self.of_kind("cand"), "the candidate ran after the failed cached build")
        self.assertEqual(self.of_kind("push"), [])
        lines = self.cached_lines()
        self.assertEqual(lines.get("AP_CACHED_CLI"), "sha256:%s %s" % ("0" * 64, digest("cli", "cand")),
                         "no pair was written for the failed image")
        self.assertEqual(lines.get("AP_CACHED_PAGE"), "%s %s" % (digest("page", "cached"), digest("page", "cand")))

    def test_a_failing_build_is_reported_as_before_and_writes_no_pair(self):
        """Research R1 Failure: when the image's builds fail, the candidate reports the failure as
        before (`the build of <name> failed`), exit 1, and no pair is written for it."""
        self.make_current(["page"])
        r = self.changed("cli", "page", STUB_BUILD_FAIL="cli")
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn("the build of cli failed", r.stderr)
        self.assertNotIn("AP_CACHED_CLI", self.cached_env.read_text() if self.cached_env.exists() else "")
        self.assertEqual(self.of_kind("push"), [])

    # (h)
    def test_ap_down_keeps_cached_env(self):
        """Data model `cached.env` Kept: across `ap down`; only a later pair replaces a line. Started
        through `ap start` on pairs that match, then stopped."""
        self.make_current(SEVEN)
        r = self.ap("start", "--credentials", str(self.cred), "--projects", str(self.proj))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(self.session.exists(), "start made no session")
        before = (self.cached_env.read_bytes(), stat.S_IMODE(self.cached_env.stat().st_mode))
        r = self.ap("down")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertFalse(self.session.exists(), "down kept the session")
        self.assertTrue(self.cached_env.exists(), "ap down removed cached.env")
        self.assertEqual((self.cached_env.read_bytes(), stat.S_IMODE(self.cached_env.stat().st_mode)), before)


def sh(*args, **kw):
    kw.setdefault("capture_output", True)
    kw.setdefault("text", True)
    kw.setdefault("timeout", 300)
    return subprocess.run(list(args), **kw)


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class CachedDigestRealBuildKitTest(unittest.TestCase):
    """SC-002 with real BuildKit (research R1 Cached digest, S11, S12): after `build.sh cli`, two
    `--changed cli` runs build nothing (one cached build, no candidate, no push) and leave cached.env
    unchanged. Isolation as test_build_changed.py: own registry under AP_REGISTRY_NAME=ap-test-<hex>
    on a free port, a test key password, XDG_* in a temporary folder, a copy of src/ and scripts/.
    Skipped, with its reason, when Docker is not reachable. Not meant for the small host: CI's
    py_tests job runs it (as it runs test_build_changed.py, with no environment variable)."""

    @classmethod
    def setUpClass(cls):
        if not shutil.which("docker") or sh("docker", "info").returncode != 0:
            raise unittest.SkipTest("docker is not reachable: this test needs real Docker and BuildKit")
        cls.real_docker = shutil.which("docker")
        cls.name = "ap-test-" + secrets.token_hex(4)
        cls.port = free_port()
        cls.password = secrets.token_hex(16)
        cls.base = Path(tempfile.mkdtemp(prefix="test-build-cached-real-", dir=TMP_BASE))
        cls.addClassCleanup(shutil.rmtree, cls.base, ignore_errors=True)
        cls.addClassCleanup(cls.cleanup_docker)
        cls.config, cls.state, cls.bin = cls.base / "config", cls.base / "state", cls.base / "bin"
        for d in (cls.config, cls.state, cls.bin):
            d.mkdir()
        cls.repo = cls.base / "repo"
        ignore = shutil.ignore_patterns("node_modules", ".git", "__pycache__", "*.pyc")
        shutil.copytree(REPO / "src", cls.repo / "src", ignore=ignore)
        shutil.copytree(REPO / "scripts", cls.repo / "scripts", ignore=ignore)
        if DEPLOY_SRC != REPO / "src/deploy":
            shutil.copytree(DEPLOY_SRC, cls.repo / "src/deploy", dirs_exist_ok=True)
        cls.dlog = cls.base / "docker-calls.log"
        # a recording `docker` first on PATH that hands every call to the real one
        (cls.bin / "docker").write_text('#!/bin/sh\nprintf \'%%s\\n\' "$*" >> "%s"\nexec "%s" "$@"\n'
                                        % (cls.dlog, cls.real_docker))
        (cls.bin / "docker").chmod(0o755)
        cls.build = cls.repo / "src/deploy/build.sh"
        cls.sdir = cls.state / "agentic-platform"

    @classmethod
    def cleanup_docker(cls):
        ids = sh("docker", "image", "ls", "-a", "--filter", "reference=127.0.0.1:%d/*" % cls.port,
                 "--format", "{{.ID}}").stdout.split()
        if ids:
            sh("docker", "rmi", "-f", *sorted(set(ids)))
        assert cls.name.startswith("ap-test-") and len(cls.name) > len("ap-test-")
        sh("docker", "rm", "-f", cls.name)
        sh("docker", "volume", "rm", "-f", cls.name + "-data")
        for n in sh("docker", "network", "ls", "--format", "{{.Name}}", "--filter", "name=" + cls.name).stdout.split():
            if n == cls.name or n.startswith(cls.name + "-"):
                sh("docker", "network", "rm", n)

    def run_build(self, *args):
        env = {**os.environ, "XDG_CONFIG_HOME": str(self.config), "XDG_STATE_HOME": str(self.state),
               "AP_REGISTRY_PORT": str(self.port), "AP_REGISTRY_NAME": self.name,
               "COSIGN_PASSWORD": self.password, "PATH": str(self.bin) + os.pathsep + os.environ["PATH"]}
        self.dlog.write_text("")
        r = sh("bash", str(self.build), *args, env=env, cwd=self.repo, timeout=900, stdin=subprocess.DEVNULL)
        calls = [l for l in self.dlog.read_text().splitlines() if l.startswith("buildx build")]
        return r, calls

    def test_two_changed_runs_after_a_build_build_nothing(self):
        r, _ = self.run_build("cli")
        self.assertEqual(r.returncode, 0, r.stderr)
        cached = self.sdir / "cached.env"
        self.assertTrue(cached.exists(), "build.sh cli wrote no cached.env")
        self.assertEqual(stat.S_IMODE(cached.stat().st_mode), 0o600)
        files = {p: p.read_bytes() for p in (cached, self.sdir / "images.env", self.sdir / "built.env")}
        for run in (1, 2):
            r, calls = self.run_build("--changed", "cli")
            self.assertEqual(r.returncode, 0, "run %d: %s" % (run, r.stderr))
            self.assertEqual(r.stderr.strip(), "", "run %d printed something" % run)
            self.assertEqual(len(calls), 1, "run %d: one cached build only: %s" % (run, calls))
            self.assertNotIn("rewrite-timestamp", calls[0], "run %d made a candidate" % run)
            self.assertNotIn("--push", calls[0], "run %d pushed" % run)
            for p, content in files.items():
                self.assertEqual(p.read_bytes(), content, "run %d changed %s" % (run, p.name))


if __name__ == "__main__":
    unittest.main()
