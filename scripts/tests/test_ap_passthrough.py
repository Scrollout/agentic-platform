"""Tests for the launcher's passing on to the terminal client (T055; src/cli/contract.md, research D33).

The contract: the launcher's own lines (`ap: ...`) go to standard error, so standard output holds
only the client's output; it passes the client's exit code on unchanged (2, 7, 130 and 143
included); and Ctrl-C (SIGINT) or SIGTERM reaches the client through `docker compose run`, which
detaches and exits 130 or 143, and `ap` exits with that code. This holds for `build`, `resume` and
`replay`, which `ap` intercepts, and for every command it passes on unchanged.

Nothing real runs. The session is written as `ap up` writes it. A fake `docker` first on PATH logs
each call and stands in for:

- the engine: a `docker run ... nc ... engine 8080` call reads the HTTP request on its standard
  input and answers listRuns with no runs, getRun RUN-1 with slice S-1, project `notes` and status
  `waiting-for-developer` (so `resume` passes it on, and `replay` builds a new project), and
  registerWorker with 200;
- the step containers: `container inspect` finds none, and `compose run -d` starts one;
- the terminal client: `compose run ... cli <command>` prints CLIENT_OUT on standard output and
  CLIENT_ERR on standard error and exits FAKE_CLI_EXIT; with FAKE_CLI_WAIT it first writes the file
  FAKE_CLI_WAIT, waits for SIGINT or SIGTERM, logs it in FAKE_SIG_LOG and exits 130 or 143, as the
  client detaches (it gives up after 30 s with exit 99).
"""
import json
import os
import signal
import subprocess
import tempfile
import time
import shutil
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
AP = Path(os.environ.get("AP_TEST_AP") or REPO / "src/deploy/ap")
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None

# Output a launcher could mangle: spacing, a tab, a line that looks like the launcher's, no final break.
CLIENT_OUT = "RUN-1\n  1  2026-11-14T10:00:00Z  RunStarted\n\tindented\nap: not the launcher's\nlast line, no break"
CLIENT_ERR = "client: a line of the client's own on standard error"
TOKENS = ("AP_CLIENT_TOKEN", "AP_PAGE_TOKEN", "AP_WORKER_TOKEN", "AP_LINT_TOKEN", "AP_ANALYST_TOKEN",
          "AP_CHECK_TOKEN", "AP_PROJECT_TOKEN")
IMAGES = ("engine", "cli", "page", "agent", "check", "project", "tests", "lint")

# Each command `ap` passes on, with the arguments the client must get.
COMMANDS = {
    "build": ["build", "S-1", "notes"],
    "resume": ["resume", "RUN-1"],
    "replay": ["replay", "RUN-1", "notes-again"],
    "run": ["run", "RUN-1"],
    "compare": ["compare", "RUN-1", "RUN-2"],
    "describe": ["describe"],
}

FAKE_DOCKER = r'''#!/usr/bin/env python3
import json, os, signal, sys, time
argv = sys.argv[1:]
with open(os.environ["FAKE_LOG"], "a") as f:
    f.write(json.dumps({"argv": argv, "pid": os.getpid()}) + "\n")

def answer(status, body):
    sys.stdout.write("HTTP/1.0 %d X\r\nContent-Type: application/json\r\n\r\n%s" % (status, json.dumps(body)))
    sys.exit(0)

if argv[:1] == ["version"]:
    print("29.9.3"); sys.exit(0)
if argv[:1] == ["run"] and "nc" in argv:
    req = sys.stdin.read().split("\r\n")[0].split(" ")
    method, path = req[0], req[1] if len(req) > 1 else ""
    if method == "GET" and path.startswith("/runs?"):
        answer(200, {"items": []})
    if method == "GET" and path.startswith("/runs/RUN-1?"):
        answer(200, {"id": "RUN-1", "slice": "S-1", "project": "notes", "status": "waiting-for-developer"})
    if method == "POST" and path.startswith("/workers?"):
        answer(200, {})
    answer(404, {"code": "NotFound"})
if argv[:1] == ["run"]:
    if "-i" in argv:
        sys.stdin.read()
    sys.exit(0)
if argv[:2] == ["container", "inspect"]:
    sys.exit(1)
if argv[:1] == ["compose"] and "run" in argv and "cli" in argv and "-d" not in argv:
    sys.stdout.write(os.environ["CLIENT_OUT"]); sys.stdout.flush()
    if os.environ.get("FAKE_CLI_STDIN"):
        sys.stdout.write("\nstdin: " + sys.stdin.read()); sys.stdout.flush()
    sys.stderr.write(os.environ["CLIENT_ERR"] + "\n"); sys.stderr.flush()
    wait = os.environ.get("FAKE_CLI_WAIT")
    if wait:
        # FAKE_CLI_ON_SIGNAL "<code>:<seconds>": on the signal, exit <code> after <seconds>, not 128+sig
        on = os.environ.get("FAKE_CLI_ON_SIGNAL")
        def got(sig, _):
            with open(os.environ["FAKE_SIG_LOG"], "a") as f:
                f.write(signal.Signals(sig).name + "\n")
            if on:
                code, delay = on.split(":")
                time.sleep(float(delay))
                os._exit(int(code))
            os._exit(128 + sig)
        signal.signal(signal.SIGINT, got)
        signal.signal(signal.SIGTERM, got)
        open(wait, "w").write(str(os.getpid()))
        deadline = time.time() + 30
        while time.time() < deadline:
            time.sleep(0.05)
        sys.exit(99)
    sys.exit(int(os.environ.get("FAKE_CLI_EXIT", "0")))
sys.exit(0)
'''


class ApPassthroughTest(unittest.TestCase):
    def setUp(self):
        self.base = Path(tempfile.mkdtemp(prefix="test-ap-passthrough-", dir=TMP_BASE))
        self.addCleanup(shutil.rmtree, self.base, ignore_errors=True)
        self.bin = self.base / "bin"
        self.bin.mkdir()
        (self.bin / "docker").write_text(FAKE_DOCKER)
        (self.bin / "docker").chmod(0o755)
        self.home, self.state, self.proj = self.base / "home", self.base / "state", self.base / "projects"
        for d in (self.home, self.state, self.proj):
            d.mkdir()
        apdir = self.state / "agentic-platform"
        apdir.mkdir(mode=0o700)
        grants = apdir / "grants"
        grants.mkdir(mode=0o700)
        cred = self.base / "models.env"
        cred.write_text("fake-key\n")
        cred.chmod(0o600)
        values = {"AP_CREDENTIALS_FILE": str(cred), "AP_PROJECTS_DIR": str(self.proj), "AP_PAGE_PORT": "7878",
                  "AP_HOST_USER": "%d:%d" % (os.getuid(), os.getgid()), "AP_GATEWAY_IMAGE": "gateway@sha256:" + "0" * 64,
                  "AP_GRANTS_DIR": str(grants)}
        for i, t in enumerate(TOKENS):
            values[t] = ("%02x" % (i + 1)) * 32
        for i, n in enumerate(IMAGES):
            values["AP_IMAGE_" + n.upper()] = "127.0.0.1:5005/ap-%s@sha256:%s" % (n, ("%02x" % (i + 0x40)) * 32)
        sess = apdir / "session.env"
        sess.write_text("".join("%s='%s'\n" % kv for kv in values.items()))
        sess.chmod(0o600)
        self.log = self.base / "docker.log"
        self.sig_log = self.base / "signals.log"
        self.ready = self.base / "client-ready"

    def env(self, **extra):
        e = {k: v for k, v in os.environ.items() if not k.startswith(("AP_", "STUB_", "FAKE_", "XDG_"))}
        e.update(HOME=str(self.home), XDG_STATE_HOME=str(self.state), XDG_CONFIG_HOME=str(self.base / "config"),
                 XDG_CACHE_HOME=str(self.base / "cache"), FAKE_LOG=str(self.log), FAKE_SIG_LOG=str(self.sig_log),
                 CLIENT_OUT=CLIENT_OUT, CLIENT_ERR=CLIENT_ERR,
                 PATH=str(self.bin) + os.pathsep + os.environ.get("PATH", ""))
        e.update({k: str(v) for k, v in extra.items()})
        return e

    def ap(self, *args, **extra):
        return subprocess.run(["sh", str(AP), *args], env=self.env(**extra), capture_output=True, text=True,
                              timeout=60, stdin=subprocess.DEVNULL)

    def calls(self):
        if not self.log.exists():
            return []
        return [json.loads(x) for x in self.log.read_text().splitlines() if x]

    def client_calls(self):
        out = []
        for c in self.calls():
            a = c["argv"]
            if a[:1] == ["compose"] and "run" in a and "cli" in a and "-d" not in a:
                out.append(a[a.index("cli", a.index("run")) + 1:])
        return out

    def test_the_client_exit_code_is_passed_on_unchanged(self):
        for name, args in COMMANDS.items():
            for code in (0, 1, 2, 4, 5, 6, 7, 130, 143):
                with self.subTest(command=name, code=code):
                    self.log.unlink(missing_ok=True)
                    r = self.ap(*args, FAKE_CLI_EXIT=code)
                    self.assertEqual(self.client_calls(), [args], r.stderr)
                    self.assertEqual(r.returncode, code, r.stderr)

    def test_standard_output_is_the_clients_and_the_launchers_lines_are_on_standard_error(self):
        for name, args in COMMANDS.items():
            with self.subTest(command=name):
                self.log.unlink(missing_ok=True)
                r = self.ap(*args)
                self.assertEqual(r.returncode, 0, r.stderr)
                self.assertEqual(self.client_calls(), [args], r.stderr)
                self.assertEqual(r.stdout, CLIENT_OUT, "standard output is not exactly the client's")
                for line in r.stderr.splitlines():
                    if line == CLIENT_ERR:
                        continue
                    self.assertTrue(line.startswith("ap: "), "a line on standard error that is neither the "
                                    "client's nor the launcher's: %r" % line)
                self.assertIn(CLIENT_ERR, r.stderr.splitlines())

    def interrupt(self, args, sig, group, **extra):
        """Start ap, wait for the client to run, send sig to ap alone (group False) or to its whole
        process group, as a terminal's Ctrl-C does (group True); return ap's exit code."""
        self.ready.unlink(missing_ok=True)
        self.sig_log.unlink(missing_ok=True)
        out_f, err_f = self.base / "ap.out", self.base / "ap.err"
        with open(out_f, "wb") as o, open(err_f, "wb") as e:
            p = subprocess.Popen(["sh", str(AP), *args], env=self.env(FAKE_CLI_WAIT=self.ready, **extra),
                                 stdin=subprocess.DEVNULL, stdout=o, stderr=e, start_new_session=True)

        def stop():
            try:
                os.killpg(p.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            p.wait()
        self.addCleanup(stop)
        deadline = time.time() + 30
        while not self.ready.exists():
            if p.poll() is not None or time.time() > deadline:
                self.fail("the client never started (ap exited %s): %s" % (p.poll(), err_f.read_text()))
            time.sleep(0.02)
        time.sleep(0.1)  # the client's handlers are set before it writes the file; let ap settle too
        if group:
            os.killpg(p.pid, sig)
        else:
            os.kill(p.pid, sig)
        try:
            p.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.fail("ap did not end within 5 s of %s" % sig.name)
        got = self.sig_log.read_text().split() if self.sig_log.exists() else []
        self.assertIn(sig.name, got, "the client never got %s before ap ended (ap exited %s); ap stderr: %s"
                      % (sig.name, p.returncode, err_f.read_text()))
        self.assertEqual(out_f.read_text(), CLIENT_OUT)
        return p.returncode

    def test_a_signal_to_ap_reaches_the_client_and_ap_exits_with_its_code(self):
        for name, args in COMMANDS.items():
            for sig, code in ((signal.SIGINT, 130), (signal.SIGTERM, 143)):
                with self.subTest(command=name, signal=sig.name):
                    self.assertEqual(self.interrupt(args, sig, group=False), code)

    def test_ctrl_c_to_the_terminals_process_group_ends_ap_with_the_clients_code(self):
        for name, args in COMMANDS.items():
            for sig, code in ((signal.SIGINT, 130), (signal.SIGTERM, 143)):
                with self.subTest(command=name, signal=sig.name):
                    self.assertEqual(self.interrupt(args, sig, group=True), code)

    def test_after_a_signal_ap_exits_with_the_clients_own_code(self):
        """The client decides its code after the signal: ap waits for it, never returning the 128 +
        signal of its own interrupted wait. The client either has already exited when ap looks again
        (delay 0) or is still running and exits later (delay 0.5 s)."""
        for name in ("build", "describe"):
            for sig in (signal.SIGINT, signal.SIGTERM):
                for code, delay in ((4, "0"), (0, "0"), (4, "0.5"), (0, "0.5")):
                    with self.subTest(command=name, signal=sig.name, code=code, delay=delay):
                        got = self.interrupt(COMMANDS[name], sig, group=False,
                                             FAKE_CLI_ON_SIGNAL="%d:%s" % (code, delay))
                        self.assertEqual(got, code)

    def test_standard_input_reaches_the_client(self):
        for name, args in COMMANDS.items():
            with self.subTest(command=name):
                self.log.unlink(missing_ok=True)
                r = subprocess.run(["sh", str(AP), *args], env=self.env(FAKE_CLI_STDIN=1), capture_output=True,
                                   text=True, timeout=60, input="marker-7f3a from the pipe\n")
                self.assertEqual(r.returncode, 0, r.stderr)
                self.assertEqual(r.stdout, CLIENT_OUT + "\nstdin: marker-7f3a from the pipe\n",
                                 "the client did not read what was piped into ap")


if __name__ == "__main__":
    unittest.main()
