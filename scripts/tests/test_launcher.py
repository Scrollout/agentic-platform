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
import re
import shutil
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
REAL_AP = REPO / "src/deploy/ap"
STUB = HERE / "fixtures/launcher/docker-stub"
REFERENCE = HERE / "fixtures/launcher/reference-ap"
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
SECRET = "fake-model-key-LAUNCHERSECRET-9f8e7d6c5b4a"  # the credentials file holds only the key
TOKEN_VARS = ("AP_CLIENT_TOKEN", "AP_PAGE_TOKEN", "AP_WORKER_TOKEN")


class LauncherContract:
    """Behaviour every launcher must show. Subclasses set AP (a path) and are TestCases."""
    AP = None

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

    # helpers
    def ap(self, *args, **env):
        e = {k: v for k, v in os.environ.items() if not k.startswith(("AP_", "STUB_"))}
        e.update(REFERENCE_IMAGES_LOCK=str(REPO / "src/deploy/images.lock"), HOME=str(self.home), XDG_STATE_HOME=str(self.state), STUB_LOG=str(self.log),
                 PATH=str(self.bin) + os.pathsep + os.environ.get("PATH", ""))
        e.update({k: str(v) for k, v in env.items()})
        return subprocess.run(["sh", str(self.AP), *args], env=e, capture_output=True, text=True,
                              timeout=60, stdin=subprocess.DEVNULL)

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
        pin = [ln.split()[1] for ln in (REPO / "src/deploy/images.lock").read_text().splitlines()
               if ln.split()[:1] == ["nginx"]][0]
        self.assertEqual(env.get("AP_GATEWAY_IMAGE"), pin)
        toks = [env.get(v, "") for v in TOKEN_VARS]
        for t in toks:
            self.assertRegex(t, r"^[0-9a-fA-F]{32,}$")
        self.assertEqual(len(set(toks)), 3, "tokens must be pairwise distinct")

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

    def test_no_token_or_credential_content_in_any_argv_or_other_environment(self):
        self.up()
        self.ap("page")
        self.ap("describe", "some text")
        self.ap("down")
        tokens = {c["env"][v] for c in self.up_calls() for v in TOKEN_VARS}
        self.assertEqual(len(tokens), 3)
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


class RealLauncherTest(LauncherContract, unittest.TestCase):
    AP = REAL_AP

    def setUp(self):
        if not REAL_AP.is_file():
            self.fail("src/deploy/ap does not exist yet")
        super().setUp()


class ReferenceLauncherTest(LauncherContract, unittest.TestCase):
    AP = REFERENCE


def run_contract(ap_path):
    """Run LauncherContract against ap_path; return the set of failing test method names."""
    cls = type("Mutant", (LauncherContract, unittest.TestCase), {"AP": ap_path})
    result = unittest.TextTestRunner(stream=io.StringIO()).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(cls))
    return {re.search(r"\.(test_\w+)(?: |$)", t.id()).group(1) for t, _ in result.failures + result.errors}


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
    ("client_exit_swallowed", 'compose run --rm cli "$cmd" "$@" ;;', 'compose run --rm cli "$cmd" "$@" || true ;;',
     {"test_client_exit_code_is_returned"}),
    ("client_args_split", 'compose run --rm cli "$cmd" "$@" ;;', 'compose run --rm cli "$cmd" $* ;;',
     {"test_client_commands_are_passed_with_arguments_intact"}),
    ("build_client_first", 'build) load; build_workers "${1:-}" || exit 1; compose run --rm cli build "$@" ;;',
     'build) load; compose run --rm cli build "$@"; build_workers "${1:-}" ;;',
     {"test_build_runs_build_workers_before_the_client"}),
    ("build_ignores_workers_failure", 'build_workers "${1:-}" || exit 1;', 'build_workers "${1:-}" || true;',
     {"test_build_runs_build_workers_before_the_client"}),
    ("unknown_option_ignored", "-*) usage ;;", "-*) exit 0 ;;",
     {"test_unknown_own_option_is_usage_error"}),
    ("docker_socket_mounted", "compose run --rm cli \"$cmd\" \"$@\" ;;",
     "compose run --rm -v /var/run/docker.sock:/var/run/docker.sock cli \"$cmd\" \"$@\" ;;",
     {"test_client_commands_are_passed_with_arguments_intact"}),
]
# A variant whose build-workers works: every assertion must still hold (the other branch of the build test).
WORKING_BUILD_WORKERS = (
    'build_workers() { echo "build-workers: not available yet" >&2; return 1; }',
    'build_workers() { compose build agent check; }')


@unittest.skipUnless(shutil.which("python3"), "python3 needed by the stub")
class LauncherChecksCanFailTest(unittest.TestCase):
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
                failed = run_contract(self.mutate(old, new))
                self.assertTrue(expected <= failed, "expected %s to fail, failed: %s" % (sorted(expected), sorted(failed)))

    def test_working_build_workers_variant_passes_everything(self):
        self.assertEqual(run_contract(self.mutate(*WORKING_BUILD_WORKERS)), set())


if __name__ == "__main__":
    unittest.main()
