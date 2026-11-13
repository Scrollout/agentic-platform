"""Tests for the launcher src/deploy/ap (T023), judged only by what it passes to `docker`.

A recording stub `docker` (fixtures/launcher/docker-stub) goes first on PATH and logs each call's
argv and environment. The behaviour assertions live in `LauncherContract` and run against:

- the real launcher (src/deploy/ap);
- fixtures/launcher/reference-ap, a minimal correct launcher, proving the assertions can pass;
- mutants of the reference, each breaking one rule, proving the assertions can fail
  (constitution: checks prove they can fail).
"""
import io
import json
import os
import pty
import re
import shutil
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
# AP_TEST_AP points the real-launcher tests at another copy of ap (to see them fail against a mutant)
REAL_AP = Path(os.environ.get("AP_TEST_AP") or REPO / "src/deploy/ap")
STUB = HERE / "fixtures/launcher/docker-stub"
REFERENCE = HERE / "fixtures/launcher/reference-ap"
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
SECRET = "fake-model-key-LAUNCHERSECRET-9f8e7d6c5b4a"  # the credentials file holds only the key
TOKEN_VARS = ("AP_CLIENT_TOKEN", "AP_PAGE_TOKEN", "AP_WORKER_TOKEN", "AP_LINT_TOKEN", "AP_ANALYST_TOKEN")
REAL_LOCK = REPO / "src/deploy/images.lock"
REFERENCE_LOCK = HERE / "fixtures/launcher/reference-images.lock"
IMAGE_NAMES = ("engine", "page", "cli", "agent", "check", "tests", "lint", "project")
PRIVATE_KEY = "FAKE-PRIVATE-KEY-MATERIAL-NOT-A-REAL-KEY"  # in the fake cosign.key; must never reach docker


def lock_pin(lock, name):
    for line in Path(lock).read_text().splitlines():
        f = line.split()
        if len(f) >= 2 and f[0] == name:
            return f[1]
    raise AssertionError("no %s pin in %s" % (name, lock))


def image_value(name):
    """A distinct digest reference per image, in the form build.sh records (ADR 0028)."""
    digest = ("%02x" % (IMAGE_NAMES.index(name) + 1)) * 32
    return "127.0.0.1:5005/ap-%s@sha256:%s" % (name, digest)


class LauncherContract:
    """Behaviour every launcher must show. Subclasses set AP (a path) and are TestCases."""
    AP = None
    LOCK = REAL_LOCK  # the pins the launcher under test reads; the reference reads its own fixture
    REG_ENV = {}  # {"AP_REGISTRY_NAME": ...} for a run with a named registry; empty means the defaults

    @property
    def reg(self):
        """The registry's container name, and its name on the internal network, as ap uses them."""
        return self.REG_ENV.get("AP_REGISTRY_NAME", "ap-registry")

    @property
    def net(self):
        return self.REG_ENV["AP_REGISTRY_NAME"] + "-net" if self.REG_ENV else "ap-signing"

    def setUp(self):
        self.base = Path(tempfile.mkdtemp(prefix="test-launcher-", dir=TMP_BASE))
        self.addCleanup(shutil.rmtree, self.base, ignore_errors=True)
        self.bin = self.base / "bin"
        self.bin.mkdir()
        shutil.copy2(STUB, self.bin / "docker")
        self.home = self.base / "home"
        self.state = self.base / "state"
        self.home.mkdir()
        self.state.mkdir()
        self.log = self.base / "docker.log"
        self.cred = self.base / "models.env"
        self.cred.write_text(SECRET + "\n")
        self.cred.chmod(0o600)
        self.proj = self.base / "projects"
        self.proj.mkdir()
        self.session = self.state / "agentic-platform" / "session.env"
        # signing material and the images build.sh recorded (ADR 0028)
        self.config = self.base / "config"
        self.signing = self.config / "agentic-platform" / "signing"
        self.signing.mkdir(parents=True)
        (self.signing / "cosign.pub").write_text("-----BEGIN PUBLIC KEY-----\nfake\n-----END PUBLIC KEY-----\n")
        (self.signing / "cosign.key").write_text(PRIVATE_KEY + "\n")
        (self.signing / "cosign.key").chmod(0o600)
        self.images_env = self.state / "agentic-platform" / "images.env"
        self.write_images()

    def write_images(self, names=IMAGE_NAMES):
        self.images_env.parent.mkdir(parents=True, exist_ok=True)
        self.images_env.write_text("".join("AP_IMAGE_%s=%s\n" % (n.upper(), image_value(n)) for n in names))
        self.images_env.chmod(0o600)

    def recorded(self):
        """AP_IMAGE_* of images.env as a dict."""
        out = {}
        for line in self.images_env.read_text().splitlines():
            k, _, v = line.partition("=")
            if k.startswith("AP_IMAGE_"):
                out[k] = v.strip("'\"")
        return out

    def verify_calls(self):
        return [(i, c) for i, c in enumerate(self.calls())
                if c["argv"][:1] == ["run"] and "verify" in c["argv"]]

    def up_index(self):
        return [i for i, c in enumerate(self.calls()) if c["argv"][:1] == ["compose"] and "up" in c["argv"]]
    # helpers
    def ap(self, *args, stdin=subprocess.DEVNULL, **env):
        e = {k: v for k, v in os.environ.items() if not k.startswith(("AP_", "STUB_"))}
        e.update(REFERENCE_IMAGES_LOCK=str(self.LOCK), HOME=str(self.home), XDG_STATE_HOME=str(self.state),
                 XDG_CONFIG_HOME=str(self.config), STUB_LOG=str(self.log),
                 PATH=str(self.bin) + os.pathsep + os.environ.get("PATH", ""))
        e.update(self.REG_ENV)
        e.update({k: str(v) for k, v in env.items()})
        return subprocess.run(["sh", str(self.AP), *args], env=e, capture_output=True, text=True,
                              timeout=60, stdin=stdin)

    def calls(self):
        if not self.log.exists():
            return []
        return [json.loads(line) for line in self.log.read_text().splitlines() if line]

    def clear(self):
        self.log.unlink(missing_ok=True)

    def up(self, **env):
        return self.ap("up", "--credentials", str(self.cred), "--projects", str(self.proj), **env)

    def up_calls(self):
        return [c for c in self.calls() if c["argv"][:1] == ["compose"] and "up" in c["argv"]]

    def client_calls(self):
        """(tail after the cli service name, call) for each `compose ... run ... --rm ... cli ...`."""
        out = []
        for c in self.calls():
            a = c["argv"]
            if a[:1] == ["compose"] and "run" in a:
                i = a.index("run")
                if "--rm" in a[i:] and "cli" in a[i:]:
                    out.append((a[a.index("cli", i) + 1:], c))
        return out

    def assertNoSocket(self):
        for c in self.calls():
            self.assertFalse(any("docker.sock" in x for x in c["argv"]), c["argv"])

    # usage and script shape
    def test_script_is_executable_posix_sh(self):
        self.assertTrue(os.access(self.AP, os.X_OK), "not executable")
        first = Path(self.AP).read_text().splitlines()[0]
        self.assertRegex(first, r"^#!\s*(/bin/sh|/usr/bin/env sh)\s*$")

    def test_no_arguments_is_usage_error(self):
        self.assertEqual(self.ap().returncode, 3)

    def test_unknown_own_option_is_usage_error(self):
        for args in (["--bogus"], ["-x"], ["up", "--bogus", "x"], ["up"]):
            with self.subTest(args=args):
                self.clear()
                self.assertEqual(self.ap(*args).returncode, 3)
                self.assertEqual(self.up_calls(), [])

    # up refusals
    def test_up_refuses_bad_inputs(self):
        def missing():
            self.cred.unlink()

        def empty():
            self.cred.write_text("")

        def mode(m):
            return lambda: self.cred.chmod(m)

        def no_projects():
            shutil.rmtree(self.proj)
        for label, mutate in (("missing credentials", missing), ("empty credentials", empty),
                              ("mode 0644", mode(0o644)), ("mode 0640", mode(0o640)),
                              ("missing projects folder", no_projects)):
            with self.subTest(label):
                self.setUp()
                mutate()
                r = self.up()
                self.assertEqual(r.returncode, 3, r.stderr)
                self.assertEqual(self.up_calls(), [], "docker compose up was called")
                self.assertNotIn(SECRET, r.stdout + r.stderr)

    # up
    def test_up_starts_compose_with_session_environment(self):
        r = self.up()
        self.assertEqual(r.returncode, 0, r.stderr)
        ups = self.up_calls()
        self.assertEqual(len(ups), 1)
        self.assertIn("-d", ups[0]["argv"])
        env = ups[0]["env"]
        self.assertEqual(os.path.realpath(env["AP_CREDENTIALS_FILE"]), os.path.realpath(self.cred))
        self.assertEqual(os.path.realpath(env["AP_PROJECTS_DIR"]), os.path.realpath(self.proj))
        self.assertRegex(env["AP_PAGE_PORT"], r"^[0-9]+$")
        self.assertRegex(env.get("AP_HOST_USER", ""), r"^[1-9][0-9]*:[0-9]+$")
        pin = lock_pin(self.LOCK, "nginx")
        self.assertEqual(env.get("AP_GATEWAY_IMAGE"), pin)
        toks = [env.get(v, "") for v in TOKEN_VARS]
        for t in toks:
            self.assertRegex(t, r"^[0-9a-fA-F]{32,}$")
        self.assertEqual(len(set(toks)), 5, "tokens must be pairwise distinct")

    def test_second_up_reuses_the_session(self):
        """Converge, do not sequence (constitution XVII): the same tokens, so Compose changes nothing."""
        self.up()
        self.up()
        ups = self.up_calls()
        self.assertEqual(len(ups), 2)
        for v in TOKEN_VARS + ("AP_PAGE_PORT",):
            self.assertEqual(ups[0]["env"][v], ups[1]["env"][v], v)

    def test_up_after_down_makes_new_tokens(self):
        self.up()
        self.ap("down")
        self.up()
        ups = self.up_calls()
        self.assertEqual(len(ups), 2)
        for v in TOKEN_VARS:
            self.assertNotEqual(ups[0]["env"][v], ups[1]["env"][v], v)

    def test_up_refuses_to_run_as_root(self):
        # `id` stub reporting root; ambiguous if a launcher learns the user another way
        idstub = self.bin / "id"
        idstub.write_text('#!/bin/sh\ncase "$1" in -u|-g) echo 0 ;; *) echo "uid=0(root) gid=0(root)" ;; esac\n')
        idstub.chmod(0o755)
        r = self.up()
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual(self.up_calls(), [])
        self.assertIn("root", r.stderr, "the refusal must say why (the session owner check would also stop a fake root)")

    def test_no_token_or_credential_content_in_any_argv_or_other_environment(self):
        self.up()
        self.ap("page")
        self.ap("describe", "some text")
        self.ap("down")
        tokens = {c["env"][v] for c in self.up_calls() for v in TOKEN_VARS}
        self.assertEqual(len(tokens), 5)
        self.assertTrue(self.calls())
        for c in self.calls():
            for a in c["argv"]:
                self.assertNotIn(SECRET, a)
                for t in tokens:
                    self.assertNotIn(t, a)
            for k, v in c["env"].items():
                self.assertNotIn(SECRET, v, "credential content in env %s" % k)
        self.assertNoSocket()

    # session file
    def test_session_file_is_private_and_removed_by_down(self):
        self.assertEqual(self.up().returncode, 0)
        self.assertTrue(self.session.is_file(), "no session.env at %s" % self.session)
        self.assertEqual(stat.S_IMODE(self.session.stat().st_mode), 0o600)
        content = self.session.read_text()
        for t in (self.up_calls()[0]["env"][v] for v in TOKEN_VARS):
            self.assertIn(t, content)
        self.assertNotIn(SECRET, content)
        self.clear()
        r = self.ap("down")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertFalse(self.session.exists())
        self.assertTrue(any(c["argv"][:1] == ["compose"] and "down" in c["argv"] for c in self.calls()))

    # page
    def test_page_prints_loopback_url(self):
        self.up()
        r = self.ap("page")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(any(re.match(r"^http://127\.0\.0\.1:[0-9]+", ln) for ln in r.stdout.splitlines()),
                        r.stdout)

    # client pass-through
    def test_client_commands_are_passed_with_arguments_intact(self):
        self.up()
        for args in (["describe"], ["show", "S-1", "rules"], ["questions", "S-1", "two words  here"],
                     ["delete", "S-1", "--yes"], ["artifact", "RUN-1", "sha256:ab"]):
            with self.subTest(args=args):
                self.clear()
                r = self.ap(*args)
                self.assertEqual(r.returncode, 0, r.stderr)
                cc = self.client_calls()
                self.assertEqual([t for t, _ in cc], [args])
                self.assertNoSocket()

    def test_client_exit_code_is_returned(self):
        self.up()
        for code in (1, 2, 4, 5, 6):
            with self.subTest(code=code):
                self.assertEqual(self.ap("show", "S-1", "rules", STUB_EXIT_CLI=code).returncode, code)
                self.assertEqual(self.ap("describe", STUB_EXIT_CLI=code).returncode, code)

    def test_client_call_carries_the_session_client_token(self):
        self.up()
        token = self.up_calls()[0]["env"]["AP_CLIENT_TOKEN"]
        self.clear()
        self.ap("describe")
        (_, call), = self.client_calls()
        self.assertEqual(call["env"].get("AP_CLIENT_TOKEN"), token)

    # build: build-workers first
    def test_build_runs_build_workers_before_the_client(self):
        self.up()
        self.clear()
        bw = self.ap("build-workers", "S-1")
        bw_calls = [c["argv"] for c in self.calls()]
        self.clear()
        r = self.ap("build", "S-1", "notes")
        calls = [c["argv"] for c in self.calls()]
        client = [i for i, c in enumerate(self.calls()) if c in
                  [c2 for t, c2 in self.client_calls() if t[:1] == ["build"]]]
        if bw.returncode != 0 or "not available" in (bw.stdout + bw.stderr).lower():
            self.assertNotEqual(r.returncode, 0, "build must fail when build-workers does")
            self.assertEqual(client, [], "client called although build-workers is unavailable")
            return
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual([t for t, _ in self.client_calls()], [["build", "S-1", "notes"]])
        prefix = calls[:client[0]]
        it = iter(prefix)
        self.assertTrue(all(any(x == y for y in it) for x in bw_calls),
                        "build-workers calls %s not all before the client call; got %s" % (bw_calls, prefix))

    def test_test_command_runs_in_a_container(self):
        self.up()
        self.clear()
        r = self.ap("test", "planted")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(any(c["argv"][:1] == ["compose"] and "run" in c["argv"] and "planted" in c["argv"]
                            for c in self.calls()))

    # image signatures (ADR 0028, research D24): verified before anything starts; fail closed
    def test_up_verifies_every_image_it_passes_to_compose_before_compose_up(self):
        r = self.up()
        self.assertEqual(r.returncode, 0, r.stderr)
        (up_i,) = self.up_index()
        passed = {k: v for k, v in self.calls()[up_i]["env"].items() if k.startswith("AP_IMAGE_")}
        self.assertTrue({"AP_IMAGE_ENGINE", "AP_IMAGE_PAGE"} <= set(passed), passed)
        vcalls = self.verify_calls()
        self.assertEqual(len(vcalls), len(passed), "one verification per image: %s" % [c["argv"] for _, c in vcalls])
        for k, v in passed.items():
            want = "%s:5000/" % self.reg + v.split("/", 1)[1]  # same repository and digest, by the registry's internal name
            self.assertEqual(sum(want in c["argv"] for _, c in vcalls), 1, "%s not verified once as %s" % (k, want))
        for i, c in vcalls:
            self.assertLess(i, up_i, "verification after compose up: %s" % c["argv"])

    def test_verify_call_shape(self):
        self.assertEqual(self.up().returncode, 0)
        vcalls = self.verify_calls()
        self.assertTrue(vcalls)
        pin = lock_pin(self.LOCK, "cosign")
        for _, c in vcalls:
            a = c["argv"]
            self.assertIn(pin, a, "cosign must run from its pin")
            self.assertLess(a.index(pin), a.index("verify"))
            self.assertIn("--rm", a)
            for o in a[:a.index(pin)]:
                self.assertNotIn(o, ("--privileged", "--pid=host", "--ipc=host"))
            nets = [a[i + 1] for i, x in enumerate(a) if x in ("--network", "--net") and i + 1 < len(a)] + \
                   [x.split("=", 1)[1] for x in a if x.startswith(("--network=", "--net="))]
            self.assertEqual(nets, [self.net], "verify must run on the internal %s network only" % self.net)
            self.assertFalse(any(x == "host" or "host" in x.split(":") for x in nets))
            self.assertNotIn("host", [x for x in a if x.startswith("--network") or x == "host"])
            k = a.index("--key")
            self.assertTrue(a[k + 1].endswith("cosign.pub"), "--key must be the public key: %s" % a[k + 1])
            self.assertIn("--insecure-ignore-tlog", a)
            self.assertIn("--allow-http-registry", a)

    def test_private_key_never_reaches_docker(self):
        self.up()
        self.ap("describe")
        self.ap("down")
        key = self.signing / "cosign.key"
        for c in self.calls():
            for x in c["argv"]:
                self.assertNotIn("cosign.key", x)
                self.assertNotIn(PRIVATE_KEY, x)
            for k, v in c["env"].items():
                self.assertNotIn(PRIVATE_KEY, v)
        for _, c in self.verify_calls():
            a = c["argv"]
            mounts = []
            for i, x in enumerate(a):
                if x in ("-v", "--volume") and i + 1 < len(a):
                    mounts.append(a[i + 1].split(":")[0])
                elif x.startswith("--volume="):
                    mounts.append(x.split("=", 1)[1].split(":")[0])
                elif x in ("--mount",) and i + 1 < len(a) or x.startswith("--mount="):
                    spec = a[i + 1] if x == "--mount" else x.split("=", 1)[1]
                    for kv in spec.split(","):
                        if kv.split("=")[0] in ("src", "source"):
                            mounts.append(kv.split("=", 1)[1])
            for m in mounts:
                m = os.path.realpath(m)
                self.assertFalse(str(key) == m or str(key).startswith(m.rstrip("/") + "/"),
                                 "mount %s exposes the private key" % m)

    def test_a_failing_verification_stops_up_and_names_the_image(self):
        for n in IMAGE_NAMES:
            with self.subTest(image=n):
                self.setUp()
                r = self.up(STUB_VERIFY_FAIL="/ap-%s@" % n)
                self.assertEqual(r.returncode, 1, r.stderr)
                self.assertIn("ap-%s" % n, r.stderr)
                self.assertEqual(self.up_calls(), [], "compose up ran after a failed verification")
                self.assertEqual([c for c in self.calls() if c["argv"][:1] == ["compose"]], [],
                                 "nothing may be started or run through Compose")

    def test_every_verification_failing_stops_up(self):
        r = self.up(STUB_VERIFY_FAIL="*")
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertEqual(self.up_calls(), [])

    def test_up_without_recorded_images_stops_and_names_build_sh(self):
        for label, mutate in (("no images.env", lambda: self.images_env.unlink()),
                              ("images.env without engine", lambda: self.write_images([n for n in IMAGE_NAMES if n != "engine"])),
                              ("empty images.env", lambda: self.images_env.write_text(""))):
            with self.subTest(label):
                self.setUp()
                mutate()
                r = self.up()
                self.assertEqual(r.returncode, 1, r.stderr)
                self.assertIn("build.sh engine", r.stderr)
                self.assertEqual(self.up_calls(), [])

    def test_up_without_the_public_key_stops(self):
        """Inferred from fail-closed (XIV): no key to verify with is a failure, not a skip."""
        (self.signing / "cosign.pub").unlink()
        r = self.up()
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual(self.up_calls(), [])

    def test_compose_gets_the_recorded_images_through_the_environment_only(self):
        evil = "127.0.0.1:5005/ap-engine@sha256:" + "ee" * 32
        r = self.up(AP_IMAGE_ENGINE=evil, AP_IMAGE_PAGE="ap-page:dev")  # a caller's values must not win
        self.assertEqual(r.returncode, 0, r.stderr)
        (c,) = self.up_calls()
        rec = self.recorded()
        self.assertEqual(len(rec), len(IMAGE_NAMES))
        for k, v in rec.items():
            self.assertEqual(c["env"].get(k), v, k)
        for c in self.calls():
            if c["argv"][:1] == ["compose"]:
                for x in c["argv"]:
                    self.assertNotIn("@sha256:", x, "an image reached Compose on a command line")
                    self.assertNotIn("AP_IMAGE", x)

    def test_client_and_test_commands_run_the_recorded_images(self):
        self.up()
        self.clear()
        self.ap("describe")
        self.ap("test", "planted")
        env_by_service = {}
        for c in self.calls():
            if c["argv"][:1] == ["compose"] and "run" in c["argv"]:
                env_by_service[tuple(c["argv"])] = c["env"]
        self.assertEqual(len(env_by_service), 2)
        rec = self.recorded()
        for env in env_by_service.values():
            self.assertEqual(env.get("AP_IMAGE_CLI"), rec["AP_IMAGE_CLI"])
            self.assertEqual(env.get("AP_IMAGE_TESTS"), rec["AP_IMAGE_TESTS"])

    # images not built (agent, check, tests) and images.env that is not what build.sh writes
    def test_images_not_built_are_placeholders_and_not_verified(self):
        self.write_images(["engine", "page", "cli"])
        r = self.up()
        self.assertEqual(r.returncode, 0, r.stderr)
        (c,) = self.up_calls()
        rec = self.recorded()
        for n in ("engine", "page", "cli"):
            self.assertEqual(c["env"]["AP_IMAGE_" + n.upper()], rec["AP_IMAGE_" + n.upper()])
        for n in ("agent", "check", "tests"):
            self.assertEqual(c["env"].get("AP_IMAGE_" + n.upper()), "ap-%s:not-built" % n)
        vcalls = self.verify_calls()
        self.assertEqual(len(vcalls), 3, [x["argv"] for _, x in vcalls])
        for _, x in vcalls:
            self.assertFalse(any("not-built" in a for a in x["argv"]))

    def test_test_command_with_a_placeholder_tests_image_is_not_available(self):
        self.write_images(["engine", "page", "cli"])
        self.assertEqual(self.up().returncode, 0)
        self.clear()
        r = self.ap("test", "planted")
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertEqual([x for x in self.calls() if x["argv"][:1] == ["compose"] and "run" in x["argv"]], [])

    def test_each_of_engine_cli_is_required(self):
        # the run page is optional until it exists (Story 2, T056); test_engine_store_and_page covers it
        for n in ("engine", "cli"):
            with self.subTest(missing=n):
                self.setUp()
                self.write_images([m for m in IMAGE_NAMES if m != n])
                r = self.up()
                self.assertEqual(r.returncode, 1, r.stderr)
                self.assertIn("build.sh %s" % n, r.stderr)
                self.assertEqual(self.up_calls(), [])

    def test_images_env_that_is_not_what_build_sh_writes_stops_up(self):
        good = "".join("AP_IMAGE_%s=%s\n" % (n.upper(), image_value(n)) for n in IMAGE_NAMES)
        cases = {"empty": "", "garbage": "this is not an env file\n\x00\xff\n",
                 "a tag, not a digest": good.replace(image_value("engine"), "127.0.0.1:5005/ap-engine:dev"),
                 "another image's digest": good.replace(image_value("page"), image_value("cli")),
                 "a foreign registry": good.replace("127.0.0.1:5005", "example.invalid"),
                 "a truncated digest": good.replace(image_value("cli"), image_value("cli")[:-5]),
                 "shell code in a line": good + "AP_IMAGE_X=$(touch /tmp/pwned)\n"}
        for label, text in cases.items():
            with self.subTest(label):
                self.setUp()
                self.images_env.write_bytes(text.encode("latin-1"))
                r = self.up()
                self.assertEqual(r.returncode, 1, r.stderr)
                self.assertEqual(self.up_calls(), [])
                if label != "another image's digest":  # images before the bad line may be verified first
                    self.assertEqual(self.verify_calls(), [], "verified something from a file that is not build.sh's")

    # T023b: the state folder, the session file, `-T` and a second `up` with other paths
    def compose_calls(self):
        return [c for c in self.calls() if c["argv"][:1] == ["compose"]]

    def test_up_tightens_an_existing_state_folder_to_700(self):
        folder = self.session.parent
        for mode in (0o755, 0o777, 0o750):
            with self.subTest(mode=oct(mode)):
                folder.chmod(mode)
                self.assertEqual(self.up().returncode, 0)
                self.assertEqual(stat.S_IMODE(folder.stat().st_mode), 0o700)
                self.ap("down")

    def test_a_session_file_that_is_not_mode_600_is_refused(self):
        self.assertEqual(self.up().returncode, 0)
        before = self.session.read_bytes()
        for mode in (0o644, 0o660, 0o666, 0o640, 0o700, 0o400):
            with self.subTest(mode=oct(mode)):
                self.session.chmod(mode)
                self.clear()
                r = self.up()
                self.assertIn(r.returncode, (1, 3), r.stderr)
                self.assertEqual(self.compose_calls(), [], "Compose was called with a session file of mode %o" % mode)
                self.assertEqual(self.session.read_bytes(), before)
                for t in TOKEN_VARS:
                    self.assertNotIn(re.search(t + r"='?([0-9a-f]+)", before.decode()).group(1), r.stdout + r.stderr)

    def test_a_session_file_that_is_not_mode_600_is_not_read_by_other_commands(self):
        """The session file is read as shell code by every later command, so a loose one stops them too."""
        self.assertEqual(self.up().returncode, 0)
        self.session.chmod(0o666)
        for args in (["page"], ["describe"], ["test", "planted"]):
            with self.subTest(args=args):
                self.clear()
                r = self.ap(*args)
                self.assertIn(r.returncode, (1, 3), r.stderr)
                self.assertEqual([c for c in self.compose_calls() if "run" in c["argv"]], [])
                self.assertNotIn("http://127.0.0.1", r.stdout)

    def test_a_session_file_that_is_a_symlink_to_a_mode_600_file_is_refused(self):
        self.assertEqual(self.up().returncode, 0)
        target = self.base / "elsewhere.env"
        shutil.move(str(self.session), str(target))
        target.chmod(0o600)
        self.session.symlink_to(target)
        for args in (["up", "--credentials", str(self.cred), "--projects", str(self.proj)], ["page"], ["describe"]):
            with self.subTest(args=args[:1]):
                self.clear()
                r = self.ap(*args)
                self.assertIn(r.returncode, (1, 3), r.stderr)
                self.assertEqual(self.compose_calls(), [], "a symlinked session file was used")

    def test_a_session_file_with_code_in_it_is_never_run_when_its_mode_is_loose(self):
        self.assertEqual(self.up().returncode, 0)
        marker = self.base / "pwned"
        with open(self.session, "a") as f:
            f.write("touch '%s'\n" % marker)
        self.session.chmod(0o664)
        self.up()
        self.ap("page")
        self.assertFalse(marker.exists(), "a group-writable session file was executed")

    def compose_run_flags(self, argv):
        i = argv.index("run")
        stop = next(k for k, x in enumerate(argv) if k > i and x in ("cli", "tests"))
        return argv[i + 1:stop]

    def test_compose_run_gets_T_when_stdin_is_not_a_terminal(self):
        self.assertEqual(self.up().returncode, 0)
        for args in (["describe", "x"], ["test", "planted"]):
            with self.subTest(args=args):
                self.clear()
                self.assertEqual(self.ap(*args).returncode, 0)
                runs = [c for c in self.compose_calls() if "run" in c["argv"]]
                self.assertEqual(len(runs), 1)
                self.assertIn("-T", self.compose_run_flags(runs[0]["argv"]), runs[0]["argv"])

    def test_compose_run_has_no_T_when_stdin_is_a_terminal(self):
        self.assertEqual(self.up().returncode, 0)
        master, slave = pty.openpty()
        try:
            for args in (["describe", "x"], ["test", "planted"]):
                with self.subTest(args=args):
                    self.clear()
                    self.assertEqual(self.ap(*args, stdin=slave).returncode, 0)
                    runs = [c for c in self.compose_calls() if "run" in c["argv"]]
                    self.assertEqual(len(runs), 1)
                    self.assertNotIn("-T", runs[0]["argv"])
        finally:
            os.close(master)
            os.close(slave)

    def other_credentials(self):
        other = self.base / "other-models.env"
        other.write_text(SECRET + "-other\n")
        other.chmod(0o600)
        return other

    def test_a_second_up_with_other_paths_is_refused_and_names_ap_down(self):
        self.assertEqual(self.up().returncode, 0)
        before = self.session.read_bytes()
        other_proj = self.base / "projects-two"
        other_proj.mkdir()
        cases = {"credentials": ("--credentials", str(self.other_credentials()), "--projects", str(self.proj)),
                 "projects": ("--credentials", str(self.cred), "--projects", str(other_proj)),
                 "both": ("--credentials", str(self.other_credentials()), "--projects", str(other_proj))}
        for label, args in cases.items():
            with self.subTest(differs=label):
                self.clear()
                r = self.ap("up", *args)
                self.assertEqual(r.returncode, 3, r.stderr)
                self.assertIn("ap down", r.stderr)
                self.assertEqual(self.up_calls(), [], "Compose was told to up with the other paths")
                self.assertEqual(self.compose_calls(), [])
                self.assertEqual(self.session.read_bytes(), before, "the running session was changed")

    def test_a_second_up_with_the_same_paths_spelled_differently_reuses_the_session(self):
        self.assertEqual(self.up().returncode, 0)
        link = self.base / "link"
        link.symlink_to(self.base)
        r = self.ap("up", "--credentials", str(link / "models.env"), "--projects", str(link / "projects") + "/")
        self.assertEqual(r.returncode, 0, r.stderr)
        a, b = self.up_calls()
        for v in TOKEN_VARS + ("AP_PAGE_PORT",):
            self.assertEqual(a["env"][v], b["env"][v], v)

    def test_up_with_other_paths_is_accepted_after_down(self):
        self.assertEqual(self.up().returncode, 0)
        self.assertEqual(self.ap("down").returncode, 0)
        other_proj = self.base / "projects-two"
        other_proj.mkdir()
        r = self.ap("up", "--credentials", str(self.other_credentials()), "--projects", str(other_proj))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(len(self.up_calls()), 2)

    def test_second_up_verifies_again(self):
        self.up()
        self.up()
        self.assertEqual(len(self.up_calls()), 2)
        self.assertEqual(len(self.verify_calls()), 2 * len(IMAGE_NAMES))


class RealLauncherTest(LauncherContract, unittest.TestCase):
    AP = REAL_AP

    def setUp(self):
        if not REAL_AP.is_file():
            self.fail("src/deploy/ap does not exist yet")
        super().setUp()

    # T058a, against the stub: `ap registry` converges the registry, and a recorded digest missing
    # from the local image store is pulled back before Compose starts
    def creation_calls(self, name):
        """Calls that make the registry container `name` (docker create / run --name)."""
        out = []
        for i, c in enumerate(self.calls()):
            a = c["argv"]
            if a[:1] in (["create"], ["run"]) or a[:2] == ["container", "create"]:
                if "--name" in a and a[a.index("--name") + 1] == name:
                    out.append((i, c))
        return out

    def removal_calls(self, name):
        return [(i, c) for i, c in enumerate(self.calls())
                if (c["argv"][:1] == ["rm"] or c["argv"][:2] == ["container", "rm"]) and name in c["argv"]]

    def test_ap_registry_with_another_port_makes_the_container_again_and_keeps_its_volume(self):
        """With the stub nothing is running, so the first call creates the registry on 5005 and the
        second, on 5006, must (re)create it for 5006 with the old one removed first, never removing
        the volume. That an unchanged registry is left alone is proven against real Docker in test_signing."""
        name = self.reg
        r = self.ap("registry", STUB_REGISTRY_ABSENT="1", AP_REGISTRY_PORT="5005")
        self.assertEqual(r.returncode, 0, r.stderr)
        (_, made), = self.creation_calls(name)
        self.assertIn("127.0.0.1:5005:5000", " ".join(made["argv"]))
        self.clear()
        r = self.ap("registry", AP_REGISTRY_PORT="5006")
        self.assertEqual(r.returncode, 0, r.stderr)
        (ri, _), = self.removal_calls(name)[:1]
        (ci, again), = self.creation_calls(name)
        self.assertLess(ri, ci)
        self.assertIn("127.0.0.1:5006:5000", " ".join(again["argv"]))
        self.assertNotIn("127.0.0.1:5005:5000", " ".join(again["argv"]))
        for c in self.calls():
            self.assertFalse("volume" in c["argv"][:2] and "rm" in c["argv"],
                             "the registry's data volume was removed: %s" % c["argv"])

    def test_up_pulls_back_a_recorded_digest_that_is_not_in_the_local_store(self):
        r = self.up(STUB_IMAGE_MISSING="1")
        self.assertEqual(r.returncode, 0, r.stderr)
        (up_i,) = self.up_index()
        pulls = [(i, c) for i, c in enumerate(self.calls()) if "pull" in c["argv"][:2]]
        for k, ref in self.recorded().items():
            hits = [i for i, c in pulls if ref in c["argv"]]
            self.assertEqual(len(hits), 1, "%s (%s) not pulled once: %s" % (k, ref, [c["argv"] for _, c in pulls]))
            self.assertLess(hits[0], up_i, "pulled after compose up")

    def test_a_docker_engine_older_than_28_stops_up_and_registry_with_exit_3(self):
        """Docker before 28 ignores the isolating network option, leaving the host reachable."""
        for version in ("27.5.1", "20.10.24"):
            for args in (["up", "--credentials", str(self.cred), "--projects", str(self.proj)], ["registry"]):
                with self.subTest(version=version, command=args[0]):
                    self.clear()
                    r = self.ap(*args, STUB_DOCKER_VERSION=version)
                    self.assertEqual(r.returncode, 3, r.stderr)
                    self.assertIn("Docker 28", r.stderr)
                    self.assertEqual(self.compose_calls(), [])
                    self.assertEqual([c for c in self.calls() if c["argv"][:2] in (["network", "create"], ["create"])], [])

    def test_an_unreadable_docker_version_stops_up_and_registry_with_exit_1(self):
        for args in (["up", "--credentials", str(self.cred), "--projects", str(self.proj)], ["registry"]):
            with self.subTest(command=args[0]):
                self.clear()
                r = self.ap(*args, STUB_DOCKER_VERSION="")
                self.assertEqual(r.returncode, 1, r.stderr)
                self.assertEqual(self.compose_calls(), [])

    def test_docker_28_and_later_is_accepted(self):
        for version in ("28.0.0", "29.9.3"):
            self.clear()
            self.assertEqual(self.up(STUB_DOCKER_VERSION=version).returncode, 0)
            self.ap("down")

    # T022d: a non-numeric Docker version, and a session file owned by someone else
    def test_a_non_numeric_docker_version_stops_up_and_registry_with_exit_3_and_no_docker_changes(self):
        for version in ("dev", "abc.1", "v29.0.0", "28abc.1", ".5", "-1.0", "unknown", "2x8.0", "master-28.1"):
            for args in (["up", "--credentials", str(self.cred), "--projects", str(self.proj)], ["registry"]):
                with self.subTest(version=version, command=args[0]):
                    self.clear()
                    r = self.ap(*args, STUB_DOCKER_VERSION=version)
                    self.assertEqual(r.returncode, 3, r.stderr)
                    self.assertIn(version, r.stderr, "the message should name the version it cannot read")
                    self.assertEqual(self.compose_calls(), [], "Compose was called")
                    changing = [c["argv"] for c in self.calls()
                                if c["argv"][:1] in (["create"], ["run"], ["rm"], ["start"]) or
                                c["argv"][:2] in (["network", "create"], ["network", "rm"], ["volume", "create"],
                                                  ["container", "create"], ["container", "rm"])]
                    self.assertEqual(changing, [], "Docker was changed")

    def test_a_numeric_major_with_a_suffix_is_accepted(self):
        for version in ("28.0.0-rc.1", "29.9.3+dfsg", "30"):
            with self.subTest(version=version):
                self.clear()
                self.assertEqual(self.up(STUB_DOCKER_VERSION=version).returncode, 0)
                self.ap("down")

    def ls_as_other_user(self):
        """A stub `ls` before the real one on PATH: `ls -ldn -- <session file>` (how ap reads the owner)
        reports the owner STUB_LS_OWNER instead of the real one; every other call, and every other
        file (the credentials file, whose owner `up` also reads), is the real `ls`."""
        real = shutil.which("ls")
        stub = self.bin / "ls"
        stub.write_text("#!/bin/sh\n"
                        'for last in "$@"; do :; done\n'
                        'case "$last" in\n'
                        '  */session.env) if [ -n "${STUB_LS_OWNER:-}" ]; then\n'
                        '      %s "$@" | awk -v o="$STUB_LS_OWNER" \'{ $3 = o; print }\'; exit 0; fi ;;\n'
                        'esac\n'
                        'exec %s "$@"\n' % (real, real))
        stub.chmod(0o755)

    def test_a_session_file_owned_by_another_user_is_refused(self):
        """Mode 600 and a regular file, but owned by someone else: it is code `ap` would run as the
        developer, so no command but `down` reads it."""
        self.ls_as_other_user()
        self.assertEqual(self.up().returncode, 0)
        before = self.session.read_bytes()
        other = str(os.getuid() + 1)
        # control: the stub with the developer's own uid changes nothing, so the refusals below are the owner
        self.clear()
        r = self.ap("page", STUB_LS_OWNER=str(os.getuid()))
        self.assertEqual(r.returncode, 0, r.stderr)
        for args in (["page"], ["describe"], ["test", "planted"], ["build-workers", "slice1"],
                     ["up", "--credentials", str(self.cred), "--projects", str(self.proj)]):
            with self.subTest(args=args):
                self.clear()
                r = self.ap(*args, STUB_LS_OWNER=other)
                self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
                self.assertIn("not owned by you", r.stderr)
                self.assertIn("ap down", r.stderr)
                self.assertEqual(self.compose_calls(), [], "Compose was called with a foreign session file")
                self.assertEqual(self.session.read_bytes(), before)
                self.assertNotIn("http://127.0.0.1", r.stdout)
        for t in TOKEN_VARS:
            token = re.search(t + r"='?([0-9a-f]+)", before.decode()).group(1)
            self.assertNotIn(token, r.stdout + r.stderr)
        # `down` still works, without reading the file
        self.clear()
        r = self.ap("down", STUB_LS_OWNER=other)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue([c for c in self.compose_calls() if "down" in c["argv"]], "down did not call Compose")
        self.assertFalse(self.session.exists())

    def test_a_foreign_session_file_with_code_in_it_is_never_run(self):
        self.ls_as_other_user()
        self.assertEqual(self.up().returncode, 0)
        marker = self.base / "ran-foreign-session"
        with open(self.session, "a") as f:
            f.write("touch %s\n" % marker)
        self.session.chmod(0o600)
        r = self.ap("page", STUB_LS_OWNER=str(os.getuid() + 1))
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertFalse(marker.exists(), "a session file owned by another user was run")

    def test_a_failed_pull_stops_up_before_compose(self):
        r = self.up(STUB_IMAGE_MISSING="1", STUB_EXIT_PULL="1")
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertEqual(self.up_calls(), [])


class ReferenceLauncherTest(LauncherContract, unittest.TestCase):
    AP = REFERENCE
    LOCK = REFERENCE_LOCK


NAMED = {"AP_REGISTRY_NAME": "ap-test-0123abcd"}  # a registry of its own: derived container, volume and network


class NamedRegistryChecks:
    """Only for the classes that run with AP_REGISTRY_NAME set (no skips elsewhere)."""
    REG_ENV = NAMED

    def test_a_named_registry_leaves_the_default_objects_alone(self):
        self.up()
        self.ap("registry")
        self.ap("down")
        for c in self.calls():
            for x in c["argv"]:
                for default in ("ap-registry", "ap-registry-data", "ap-signing"):
                    self.assertNotIn(default, x, "default-named object used with AP_REGISTRY_NAME set: %s" % c["argv"])


class RealLauncherNamedRegistryTest(NamedRegistryChecks, RealLauncherTest):
    pass


class ReferenceLauncherNamedRegistryTest(NamedRegistryChecks, ReferenceLauncherTest):
    pass


def run_contract(ap_path, only=None, reg_env=None):
    """Run LauncherContract (or only the named tests) against ap_path; return the failing test names."""
    bases = (NamedRegistryChecks,) if reg_env else ()
    cls = type("Mutant", (*bases, LauncherContract, unittest.TestCase), {"AP": ap_path, "LOCK": REFERENCE_LOCK, "REG_ENV": reg_env or {}})
    loader = unittest.defaultTestLoader
    suite = loader.loadTestsFromTestCase(cls) if only is None else unittest.TestSuite(map(cls, sorted(only)))
    result = unittest.TextTestRunner(stream=io.StringIO()).run(suite)
    return {re.search(r"\.(test_\w+)(?: |$)", t.id()).group(1) for t, _ in result.failures + result.errors}


VERIFY_TESTS = {"test_up_verifies_every_image_it_passes_to_compose_before_compose_up",
                "test_verify_call_shape"}
# (name, old text, new text, test methods that must fail); old text must occur in reference-ap once
MUTANTS = [
    ("no_mode_check", '  [ "$m" = 600 ] || [ "$m" = 400 ] || { echo "ap: credentials file must be mode 600" >&2; exit 3; }\n', "",
     {"test_up_refuses_bad_inputs"}),
    ("empty_credentials_accepted", '[ -f "$cred" ] && [ -s "$cred" ]', '[ -f "$cred" ]',
     {"test_up_refuses_bad_inputs"}),
    ("no_projects_check", '  [ -d "$proj" ] || { echo "ap: projects folder missing" >&2; exit 3; }\n', "",
     {"test_up_refuses_bad_inputs"}),
    ("token_in_argv", "  compose up -d ;;", '  compose up -d --label "t=$AP_CLIENT_TOKEN" ;;',
     {"test_no_token_or_credential_content_in_any_argv_or_other_environment"}),
    ("credential_content_in_env", "  compose up -d ;;", '  AP_LEAK=$(cat "$cred") compose up -d ;;',
     {"test_no_token_or_credential_content_in_any_argv_or_other_environment"}),
    ("session_world_readable", "  chmod 600 \"$sess\"\n", "  chmod 644 \"$sess\"\n",
     {"test_session_file_is_private_and_removed_by_down"}),
    ("down_keeps_session", 'compose down; rm -f "$sess"', "compose down",
     {"test_session_file_is_private_and_removed_by_down"}),
    ("down_skips_compose", 'compose down; rm -f "$sess"', 'rm -f "$sess"',
     {"test_session_file_is_private_and_removed_by_down"}),
    ("fixed_tokens", "rand() { od -An -N24 -tx1 /dev/urandom | tr -d ' \\n'; }", "rand() { echo 0123456789abcdef0123456789abcdef; }",
     {"test_up_after_down_makes_new_tokens", "test_up_starts_compose_with_session_environment"}),
    ("tokens_regenerated_on_every_up", '  [ -f "$sess" ] && . "$sess" || true\n', "",
     {"test_second_up_reuses_the_session"}),
    ("no_root_check", '  [ "$uid" != 0 ] || { echo "ap: refusing to run as root" >&2; exit 3; }\n', "",
     {"test_up_refuses_to_run_as_root"}),
    ("wrong_gateway_image", "img=$(awk '$1==\"nginx\"{print $2}' \"$lock\")", "img=nginx:latest",
     {"test_up_starts_compose_with_session_environment"}),
    ("short_tokens", "od -An -N24 -tx1", "od -An -N8 -tx1",
     {"test_up_starts_compose_with_session_environment"}),
    ("page_binds_all_interfaces", 'echo "http://127.0.0.1:$AP_PAGE_PORT"', 'echo "http://0.0.0.0:$AP_PAGE_PORT"',
     {"test_page_prints_loopback_url"}),
    ("client_exit_swallowed", 'compose run $tflag --rm cli "$cmd" "$@" ;;', 'compose run --rm cli "$cmd" "$@" || true ;;',
     {"test_client_exit_code_is_returned"}),
    ("client_args_split", 'compose run $tflag --rm cli "$cmd" "$@" ;;', 'compose run --rm cli "$cmd" $* ;;',
     {"test_client_commands_are_passed_with_arguments_intact"}),
    ("build_client_first", 'build) load; build_workers "${1:-}" || exit 1; compose run $tflag --rm cli build "$@" ;;',
     'build) load; compose run $tflag --rm cli build "$@"; build_workers "${1:-}" ;;',
     {"test_build_runs_build_workers_before_the_client"}),
    ("build_ignores_workers_failure", 'build_workers "${1:-}" || exit 1;', 'build_workers "${1:-}" || true;',
     {"test_build_runs_build_workers_before_the_client"}),
    ("unknown_option_ignored", "-*) usage ;;", "-*) exit 0 ;;",
     {"test_unknown_own_option_is_usage_error"}),
    ("docker_socket_mounted", "compose run $tflag --rm cli \"$cmd\" \"$@\" ;;",
     "compose run $tflag --rm -v /var/run/docker.sock:/var/run/docker.sock cli \"$cmd\" \"$@\" ;;",
     {"test_client_commands_are_passed_with_arguments_intact"}),
    ("verify_skipped", 'docker run --rm --network "$net"', 'true --rm --network "$net"',
     VERIFY_TESTS),
    ("verify_after_up", "  load\n  compose up -d ;;", "  load\n  compose up -d\n  for n in $names; do docker run --rm --network \"$net\" -v \"$cfg/cosign.pub:/cosign.pub:ro\" \"$cosign\" verify --key /cosign.pub --insecure-ignore-tlog --allow-http-registry \"$reg:5000/$(sed -n \"s/^AP_IMAGE_$(echo $n | tr a-z A-Z)=//p\" \"$imgs\" | sed 's|[^/]*/||')\" >/dev/null; done ;;",
     {"test_up_verifies_every_image_it_passes_to_compose_before_compose_up"}),
    ("verify_failure_ignored", '>/dev/null ||\n      { echo "ap: signature check failed', '>/dev/null || true ||\n      { echo "ap: signature check failed',
     {"test_a_failing_verification_stops_up_and_names_the_image", "test_every_verification_failing_stops_up"}),
    ("verify_failure_not_named", 'signature check failed for ap-$n ($v)', 'signature check failed',
     {"test_a_failing_verification_stops_up_and_names_the_image"}),
    ("private_key_mounted", '-v "$cfg/cosign.pub:/cosign.pub:ro"', '-v "$cfg/cosign.key:/cosign.key:ro" -v "$cfg/cosign.pub:/cosign.pub:ro"',
     {"test_private_key_never_reaches_docker"}),
    ("signing_dir_mounted", '-v "$cfg/cosign.pub:/cosign.pub:ro"', '-v "$cfg:/cosign:ro"',
     {"test_private_key_never_reaches_docker"}),
    ("verify_on_host_network", '--network "$net"', '--network host',
     {"test_verify_call_shape"}),
    ("verify_without_network_flag", '--network "$net" ', '',
     {"test_verify_call_shape"}),
    ("verify_needs_tlog", ' --insecure-ignore-tlog', '',
     {"test_verify_call_shape"}),
    ("verify_with_wrong_pin", '"$cosign" verify', 'alpine verify',
     {"test_verify_call_shape"}),
    ("missing_images_not_reported", 'build.sh $n" >&2; exit 1 ;; esac', 'build.sh $n" >&2; exit 0 ;; esac',
     {"test_up_without_recorded_images_stops_and_names_build_sh"}),
    ("missing_images_not_named", 'run: src/deploy/build.sh $n', 'run the build',
     {"test_up_without_recorded_images_stops_and_names_build_sh"}),
    ("images_not_in_environment", '  [ ! -f "$imgs" ] || . "$imgs"\n', '',
     {"test_client_and_test_commands_run_the_recorded_images",
      "test_compose_gets_the_recorded_images_through_the_environment_only"}),
    ("placeholders_verified_too", '      continue\n', '      v=ap-$n:not-built\n',
     {"test_images_not_built_are_placeholders_and_not_verified"}),
    ("missing_needed_image_tolerated", 'case " $needed " in *" $n "*) echo "ap: no image for $n; run: src/deploy/build.sh $n" >&2; exit 1 ;; esac',
     'case " $needed " in *" nothing "*) exit 1 ;; esac',
     {"test_each_of_engine_cli_is_required", "test_up_without_recorded_images_stops_and_names_build_sh"}),
    ("garbage_images_env_accepted", "grep -Evx", "grep -Ex",
     {"test_images_env_that_is_not_what_build_sh_writes_stops_up"}),
    ("test_runs_with_placeholder", '*@sha256:*) ;; *) echo "ap: test: not available yet" >&2; exit 3 ;; esac', '*) ;; esac',
     {"test_test_command_with_a_placeholder_tests_image_is_not_available"}),
    ("no_placeholders", 'eval "[ -n \\"\\${AP_IMAGE_$N:-}\\" ] || AP_IMAGE_$N=ap-$n:not-built"', ':',
     {"test_images_not_built_are_placeholders_and_not_verified"}),
    ("verifies_only_engine", 'for n in $names; do\n    N=$(echo "$n" | tr a-z A-Z)\n    v=', 'for n in engine; do\n    N=$(echo "$n" | tr a-z A-Z)\n    v=',
     {"test_up_verifies_every_image_it_passes_to_compose_before_compose_up"}),
    # T023b
    ("state_folder_not_tightened", 'mkdir -p "$state"; chmod 700 "$state"\n', 'mkdir -p "$state"\n',
     {"test_up_tightens_an_existing_state_folder_to_700"}),
    ("session_mode_not_checked", "-rw-------*) ;; *) echo", "*) ;; *) echo",
     {"test_a_session_file_that_is_not_mode_600_is_refused",
      "test_a_session_file_that_is_not_mode_600_is_not_read_by_other_commands",
      "test_a_session_file_with_code_in_it_is_never_run_when_its_mode_is_loose"}),
    ("up_reads_session_unchecked", '  check_session\n  [ -f "$sess" ] && . "$sess" || true\n', '  [ -f "$sess" ] && . "$sess" || true\n',
     {"test_a_session_file_that_is_not_mode_600_is_refused",
      "test_a_session_file_with_code_in_it_is_never_run_when_its_mode_is_loose"}),
    ("load_reads_session_unchecked", '  check_session\n  set -a; . "$sess"', '  set -a; . "$sess"',
     {"test_a_session_file_that_is_not_mode_600_is_not_read_by_other_commands"}),
    ("symlinked_session_followed", '  [ ! -L "$sess" ] || { echo "ap: the session file $sess is a link; run: ap down" >&2; exit 1; }\n', "",
     {"test_a_session_file_that_is_a_symlink_to_a_mode_600_file_is_refused"}),
    ("no_T_flag", "tflag=-T\n", "tflag=\n", {"test_compose_run_gets_T_when_stdin_is_not_a_terminal"}),
    ("T_flag_on_a_terminal", "[ -t 0 ] && tflag=\n", "", {"test_compose_run_has_no_T_when_stdin_is_a_terminal"}),
    ("other_paths_accepted", '[ -n "${AP_CREDENTIALS_FILE:-}" ] &&', "false &&",
     {"test_a_second_up_with_other_paths_is_refused_and_names_ap_down"}),
    ("other_projects_accepted", ' || [ "$AP_PROJECTS_DIR" != "$proj" ]', "",
     {"test_a_second_up_with_other_paths_is_refused_and_names_ap_down"}),
    ("other_credentials_accepted", '[ "$AP_CREDENTIALS_FILE" != "$cred" ] || ', "",
     {"test_a_second_up_with_other_paths_is_refused_and_names_ap_down"}),
    ("other_paths_exit_1", "run: ap down\" >&2; exit 3", "run: ap down\" >&2; exit 1",
     {"test_a_second_up_with_other_paths_is_refused_and_names_ap_down"}),
    ("other_paths_message_without_ap_down", "uses other --credentials or --projects; run: ap down", "uses other --credentials or --projects",
     {"test_a_second_up_with_other_paths_is_refused_and_names_ap_down"}),
    ("paths_compared_unresolved", 'cred=$(rp "$cred"); proj=$(cd "$proj" && pwd -P)\n', "",
     {"test_a_second_up_with_the_same_paths_spelled_differently_reuses_the_session"}),
]
# A variant whose build-workers works: every assertion must still hold (the other branch of the build test).
WORKING_BUILD_WORKERS = (
    'build_workers() { echo "build-workers: not available yet" >&2; return 1; }',
    'build_workers() { compose build agent check; }')


class LauncherChecksCanFailTest(unittest.TestCase):
    def setUp(self):
        # the stub is a python3 script: without python3 this must fail, not skip
        self.assertTrue(shutil.which("python3"), "python3 is required by the docker stub")

    def mutate(self, old, new):
        text = REFERENCE.read_text()
        self.assertEqual(text.count(old), 1, "mutation target not found once: %r" % old)
        d = Path(tempfile.mkdtemp(prefix="test-launcher-mutant-", dir=TMP_BASE))
        self.addCleanup(shutil.rmtree, d, ignore_errors=True)
        ap = d / "ap"
        ap.write_text(text.replace(old, new))
        ap.chmod(0o755)
        return ap

    def test_each_mutant_fails_its_tests(self):
        for name, old, new, expected in MUTANTS:
            with self.subTest(mutant=name):
                failed = run_contract(self.mutate(old, new), only=expected)
                self.assertTrue(expected <= failed, "expected %s to fail, failed: %s" % (sorted(expected), sorted(failed)))

    def test_default_registry_names_mutant_is_caught_when_a_name_is_set(self):
        failed = run_contract(self.mutate("reg=${AP_REGISTRY_NAME:-ap-registry}", "reg=ap-registry"),
                              only={"test_up_verifies_every_image_it_passes_to_compose_before_compose_up",
                                    "test_a_named_registry_leaves_the_default_objects_alone"}, reg_env=NAMED)
        self.assertTrue(failed, "ignoring AP_REGISTRY_NAME went unnoticed")
        failed = run_contract(self.mutate('net=$AP_REGISTRY_NAME-net', 'net=ap-signing'),
                              only={"test_verify_call_shape"}, reg_env=NAMED)
        self.assertEqual(failed, {"test_verify_call_shape"})

    def test_working_build_workers_variant_passes_everything(self):
        self.assertEqual(run_contract(self.mutate(*WORKING_BUILD_WORKERS)), set())


if __name__ == "__main__":
    unittest.main()
