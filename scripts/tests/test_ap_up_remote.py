"""Tests for T015: remote access of the run page, `ap up --listen` and `ap start --listen` (feature 003, US4).

Spec: specs/003-one-command-start/spec.md (User Story 4, FR-012 to FR-017, Edge Cases on remote access);
research R5; data-model.md (session variables, `page-remote/`, `access.log`); contract:
src/cli/contract.md "Starting the platform" (`up`: remote access, `page`, `down`);
ADR 0062. Judges task T017 (src/deploy/ap).

Reuses the harness of test_ap_start.py (StartCase, the start stub `docker`; no real Docker). A stub
`hostname` on PATH answers `hostname` (vm.example) and `hostname -I` ("192.0.2.10 192.0.2.11").

Choices where the contract is silent (the code must match them):
- the fingerprint is printed, and written in `access.log`, as upper-case hex pairs joined by ":" of
  the 64 hex characters the stub's `sha256sum` run prints (`AB:CD:...`, as the contract's line shows);
- `access.log` lines are `<UTC time, YYYY-MM-DDTHH:MM:SSZ> on address=<a> port=<p> names=<n1,n2,...>
  sha256=<AB:CD:...>` and `<time> off` (data-model.md), names joined by commas;
- `cert.names` holds the time the certificate was made as a UTC ISO time (`YYYY-MM-DDTHH:MM:SSZ`) or
  as epoch seconds; the age tests rewrite that value (and the file's mtime) to be 400 or 100 days old;
- `names.conf` holds one `server_name <names>;` directive whose names are exactly the given ones, in
  the given order (the page's own Host is also in the file, as data-model.md says);
- `page-remote/` is `<state>/agentic-platform/page-remote` (mode 700), and `AP_PAGE_CERT_DIR` names it;
- every Compose call of a remote session, `down` included, gets `-f compose.yaml -f compose.remote.yaml`;
- the Compose `up` call has `AP_PAGE_ADDRESS` in its environment.
"""
import os
import re
import sys
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from test_ap_start import StartCase, NEW_STUB, OLD_STUB, PASSWORD  # noqa: E402

HOSTNAME_STUB = """#!/bin/sh
case "$*" in
  *-I*) echo "192.0.2.10 192.0.2.11 " ;;
  *) echo vm.example ;;
esac
"""
DEFAULT_NAMES = ["vm.example", "192.0.2.10", "192.0.2.11", "127.0.0.1", "localhost"]
PEM_CERT = "-----BEGIN CERTIFICATE-----\nZGV2LWNlcnQ=\n-----END CERTIFICATE-----\n"
PEM_KEY = "-----BEGIN PRIVATE KEY-----\nZGV2LWtleQ==\n-----END PRIVATE KEY-----\n"
FP_A = "aa" * 32
FP_B = "bb" * 32
FP_A_COLON = ":".join(["AA"] * 32)
FP_B_COLON = ":".join(["BB"] * 32)
ISO = re.compile(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ")
ON_LINE = re.compile(r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ on address=(\S+) port=(\d+) names=(\S+) "
                     r"sha256=([0-9A-F]{2}(?::[0-9A-F]{2}){31})$")
OFF_LINE = re.compile(r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ off$")


class RemoteCase(StartCase):
    def setUp(self):
        super().setUp()
        (self.bin / "hostname").write_text(HOSTNAME_STUB)
        (self.bin / "hostname").chmod(0o755)
        self.make_current()
        self.page_dir = self.sdir / "page-remote"
        self.access_log = self.sdir / "access.log"
        self.fp = {"STUB_CERT_SHA256": FP_A}

    # running ap
    def launch(self, kind, *more, **extra):
        for k, v in self.fp.items():
            extra.setdefault(k, v)
        if kind == "start":
            return self.start(*more, **extra)
        return self.run_ap("up", *more, "--credentials", str(self.cred), "--projects", str(self.proj), **extra)

    def up_remote(self, *more, **extra):
        return self.launch("up", "--listen", "0.0.0.0", *more, **extra)

    def down(self):
        r = self.run_ap("down", **self.fp)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertFalse(self.session.exists())

    def mark(self):
        return len(self.calls())

    # what the docker calls were
    @staticmethod
    def is_cert_make(c):
        return c["argv"][:1] == ["run"] and any("generate_cert" in a for a in c["argv"])

    @staticmethod
    def is_hash(c):
        return c["argv"][:1] == ["run"] and any("mkpasswd" in a for a in c["argv"])

    @staticmethod
    def is_fingerprint(c):
        return c["argv"][:1] == ["run"] and any("sha256sum" in a for a in c["argv"])

    @staticmethod
    def compose_files(argv):
        return [Path(argv[i + 1]).name for i, a in enumerate(argv) if a == "-f" and i + 1 < len(argv)]

    def cert_makes(self, since=0):
        return [c for c in self.calls()[since:] if self.is_cert_make(c)]

    # session and files
    def session_value(self, name):
        for line in self.session.read_text().splitlines():
            if line.startswith(name + "="):
                return line.split("=", 1)[1].strip("'\"")
        return None

    def secret(self):
        return self.session_value("AP_PAGE_SECRET")

    def access_lines(self):
        return self.access_log.read_text().splitlines() if self.access_log.exists() else []

    def mode(self, p):
        return p.stat().st_mode & 0o777

    def age_certificate(self, days):
        """Make cert.names say the certificate was made `days` ago (its time value and its mtime)."""
        p = self.page_dir / "cert.names"
        text = p.read_text()
        then = datetime.now(timezone.utc) - timedelta(days=days)
        if ISO.search(text):
            text = ISO.sub(then.strftime("%Y-%m-%dT%H:%M:%SZ"), text, count=1)
        elif re.search(r"\b\d{9,11}\b", text):
            text = re.sub(r"\b\d{9,11}\b", str(int(then.timestamp())), text, count=1)
        else:
            self.fail("cert.names holds no time (UTC ISO or epoch seconds): %r" % text)
        p.write_text(text)
        t = then.timestamp()
        os.utime(p, (t, t))

    def named_servers(self):
        m = re.findall(r"^\s*server_name\s+([^;]*);", (self.page_dir / "names.conf").read_text(), re.M)
        self.assertEqual(len(m), 1, "names.conf needs one server_name directive")
        return m[0].split()

    # shared assertions
    def assertRefusedNothingStarted(self, r, *words):
        self.assertEqual(r.returncode, 3, r.stderr)
        # a refusal of the option, not today's generic usage text (which names none of the new options)
        self.assertNotIn("usage:", r.stderr)
        for w in words:
            self.assertIn(w, r.stderr)
        argvs = self.argvs()
        self.assertEqual(self.compose_up_indexes(), [], "compose up was called")
        self.assertEqual([a for a in argvs if a[:2] == ["buildx", "build"]], [], "an image was built")
        self.assertEqual([c for c in self.calls() if self.is_cert_make(c) or self.is_hash(c)
                          or self.is_fingerprint(c)], [], "a certificate or password was made")
        self.assertFalse(self.session.exists(), "a session file was made")
        for f in ("cert.pem", "key.pem", "htpasswd", "names.conf"):
            self.assertFalse((self.page_dir / f).exists(), f + " was made")
        self.assertFalse(self.access_log.exists(), "access.log was written")
        self.assertNoSecret(r)

    def assertRefusedOnStarted(self, r, before_session, before_log, mark):
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertNotIn("usage:", r.stderr)
        self.assertIn("down", r.stderr)
        self.assertRegex(r.stderr, r"--listen|remote")
        made = self.calls()[mark:]
        self.assertEqual([c for c in made if c["argv"][:1] == ["compose"] and "up" in c["argv"]], [],
                         "compose up was called")
        self.assertEqual([c for c in made if self.is_cert_make(c) or self.is_hash(c)], [])
        self.assertEqual(self.session.read_text(), before_session, "the session changed")
        self.assertEqual(self.access_lines(), before_log, "access.log changed")


class RefusalTest(RemoteCase):
    def refuse(self, *opts, words=()):
        for kind in ("up", "start"):
            with self.subTest(kind=kind, opts=opts):
                self.log.unlink(missing_ok=True)
                r = self.launch(kind, *opts, COSIGN_PASSWORD=PASSWORD)
                self.assertRefusedNothingStarted(r, *words)

    def test_a_listen_value_that_is_no_address_of_the_machine_is_refused(self):
        for bad in ("10.9.9.9", "192.0.2.99", "vm.example", "0.0.0.1", "not-an-address"):
            self.refuse("--listen", bad, words=(bad,))

    def test_a_name_that_is_no_host_name_or_address_is_refused(self):
        for bad in ("a;b", "a b", "{", "-a.example", "a-.example", "a..b", "$(x)", "a/b"):
            self.refuse("--listen", "0.0.0.0", "--name", bad, words=("--name",))

    def test_name_without_listen_is_refused(self):
        self.refuse("--name", "box.example", words=("--listen",))

    def test_certificate_and_key_without_listen_are_refused(self):
        c, k = self.base / "c.pem", self.base / "k.pem"
        c.write_text(PEM_CERT)
        k.write_text(PEM_KEY)
        self.refuse("--certificate", str(c), "--key", str(k), words=("--listen",))
        self.refuse("--certificate", str(c), words=("--listen",))
        self.refuse("--key", str(k), words=("--listen",))

    def test_certificate_without_key_and_key_without_certificate_are_refused(self):
        c, k = self.base / "c.pem", self.base / "k.pem"
        c.write_text(PEM_CERT)
        k.write_text(PEM_KEY)
        self.refuse("--listen", "0.0.0.0", "--certificate", str(c), words=("--key",))
        self.refuse("--listen", "0.0.0.0", "--key", str(k), words=("--certificate",))

    def test_a_certificate_or_key_that_is_not_pem_is_refused(self):
        bad, c, k = self.base / "bad.pem", self.base / "c.pem", self.base / "k.pem"
        bad.write_text("this is not a PEM file\n")
        c.write_text(PEM_CERT)
        k.write_text(PEM_KEY)
        self.refuse("--listen", "0.0.0.0", "--certificate", str(bad), "--key", str(k),
                    words=("--certificate",))
        self.refuse("--listen", "0.0.0.0", "--certificate", str(c), "--key", str(bad), words=("--key",))
        self.refuse("--listen", "0.0.0.0", "--certificate", str(self.base / "missing.pem"), "--key", str(k),
                    words=("--certificate",))


class RemoteStartTest(RemoteCase):
    def test_listen_gives_compose_the_remote_file_after_the_first_and_the_address(self):
        r = self.up_remote()
        self.assertEqual(r.returncode, 0, r.stderr)
        composes = [a for a in self.argvs() if a[:1] == ["compose"]]
        self.assertTrue(composes)
        for a in composes:
            self.assertEqual(self.compose_files(a), ["compose.yaml", "compose.remote.yaml"], a)
        ups = [c for c in self.calls() if c["argv"][:1] == ["compose"] and "up" in c["argv"]]
        self.assertEqual(len(ups), 1)
        self.assertEqual(ups[0]["env"].get("AP_PAGE_ADDRESS"), "0.0.0.0")
        self.assertEqual(ups[0]["env"].get("AP_PAGE_CERT_DIR"), str(self.page_dir))
        d = self.run_ap("down", **self.fp)
        self.assertEqual(d.returncode, 0, d.stderr)
        downs = [a for a in self.argvs() if a[:1] == ["compose"] and "down" in a]
        self.assertTrue(downs)
        for a in downs:
            self.assertEqual(self.compose_files(a), ["compose.yaml", "compose.remote.yaml"], a)
        self.assertTrue(self.access_log.exists(), "down keeps access.log")
        self.assertTrue((self.page_dir / "cert.pem").exists(), "down keeps the certificate")

    def test_start_listen_does_the_same(self):
        r = self.launch("start", "--listen", "0.0.0.0", COSIGN_PASSWORD=PASSWORD)
        self.assertEqual(r.returncode, 0, r.stderr)
        ups = [c for c in self.calls() if c["argv"][:1] == ["compose"] and "up" in c["argv"]]
        self.assertEqual(len(ups), 1)
        self.assertEqual(self.compose_files(ups[0]["argv"]), ["compose.yaml", "compose.remote.yaml"])
        self.assertEqual(ups[0]["env"].get("AP_PAGE_ADDRESS"), "0.0.0.0")
        self.assertRegex(self.secret(), r"^[0-9a-f]{64}$")
        self.assertIn("sign in as ap with " + self.secret(), r.stderr)

    def test_the_session_gains_the_remote_variables_with_default_names(self):
        r = self.up_remote()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertRegex(self.secret(), r"^[0-9a-f]{64}$")
        self.assertEqual(self.session_value("AP_PAGE_ADDRESS"), "0.0.0.0")
        self.assertEqual(self.session_value("AP_PAGE_NAMES").split(), DEFAULT_NAMES)
        self.assertEqual(self.session_value("AP_PAGE_CERT_DIR"), str(self.page_dir))
        self.assertEqual(self.mode(self.session), 0o600)

    def test_listen_on_one_address_of_the_machine_or_on_ipv6_any(self):
        r = self.launch("up", "--listen", "192.0.2.11")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.session_value("AP_PAGE_ADDRESS"), "192.0.2.11")
        self.assertEqual(self.session_value("AP_PAGE_NAMES").split(), ["192.0.2.11"])
        self.down()
        r = self.launch("up", "--listen", "::")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.session_value("AP_PAGE_ADDRESS"), "::")
        ups = [c for c in self.calls() if c["argv"][:1] == ["compose"] and "up" in c["argv"]]
        self.assertEqual(ups[-1]["env"].get("AP_PAGE_ADDRESS"), "::")

    def test_page_remote_holds_the_files_the_forwarder_needs_all_mode_600(self):
        r = self.up_remote("--name", "box.example", "--name", "192.0.2.50")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.mode(self.page_dir), 0o700)
        for f in ("htpasswd", "names.conf", "cert.pem", "key.pem", "cert.names"):
            self.assertTrue((self.page_dir / f).is_file(), f)
            self.assertEqual(self.mode(self.page_dir / f), 0o600, f)
        lines = (self.page_dir / "htpasswd").read_text().splitlines()
        self.assertEqual(len(lines), 1)
        self.assertTrue(lines[0].startswith("ap:$6$"), lines[0])
        self.assertEqual(self.named_servers(), ["box.example", "192.0.2.50"])
        conf = (self.page_dir / "names.conf").read_text()
        for default in ("vm.example", "localhost", "192.0.2.10", "192.0.2.11"):
            self.assertNotIn(default, conf)
        self.assertEqual(self.session_value("AP_PAGE_NAMES").split(), ["box.example", "192.0.2.50"])
        self.assertIn("box.example", (self.page_dir / "cert.names").read_text())
        # the certificate is made for the names
        makes = self.cert_makes()
        self.assertEqual(len(makes), 1)
        joined = " ".join(makes[0]["argv"])
        self.assertIn("box.example", joined)
        self.assertIn("192.0.2.50", joined)
        self.assertIn("--network", makes[0]["argv"])
        self.assertIn("none", makes[0]["argv"])

    def test_the_secret_reached_mkpasswd_on_standard_input_never_in_an_argument(self):
        r = self.up_remote()
        self.assertEqual(r.returncode, 0, r.stderr)
        secret = self.secret()
        hashes = [c for c in self.calls() if self.is_hash(c)]
        self.assertEqual(len(hashes), 1)
        self.assertEqual(hashes[0]["stdin"].strip(), secret)
        for c in self.calls():
            for a in c["argv"]:
                self.assertNotIn(secret, a, "the secret is in an argument of %s" % c["argv"][:3])
        self.assertIn("mkpasswd", " ".join(hashes[0]["argv"]))
        self.assertIn("sha512", " ".join(hashes[0]["argv"]))
        self.assertTrue((self.page_dir / "htpasswd").read_text().startswith("ap:$6$stubsalt$"))

    def test_a_second_up_keeps_the_secret_and_makes_no_new_certificate(self):
        self.assertEqual(self.up_remote().returncode, 0)
        secret, htpasswd = self.secret(), (self.page_dir / "htpasswd").read_text()
        cert = (self.page_dir / "cert.pem").read_text()
        mark = self.mark()
        r = self.up_remote()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.secret(), secret)
        self.assertEqual((self.page_dir / "htpasswd").read_text(), htpasswd)
        self.assertEqual((self.page_dir / "cert.pem").read_text(), cert)
        self.assertEqual(self.cert_makes(mark), [])
        self.assertEqual(len(self.access_lines()), 1, "a second up turns nothing on")

    def test_up_after_down_makes_a_new_secret_and_keeps_the_certificate(self):
        self.assertEqual(self.up_remote().returncode, 0)
        first, hashed = self.secret(), (self.page_dir / "htpasswd").read_text()
        self.down()
        mark = self.mark()
        r = self.up_remote()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertRegex(self.secret(), r"^[0-9a-f]{64}$")
        self.assertNotEqual(self.secret(), first)
        self.assertNotEqual((self.page_dir / "htpasswd").read_text(), hashed)
        self.assertEqual(self.cert_makes(mark), [], "the same names keep the certificate")
        self.assertEqual(len([l for l in self.access_lines() if " on " in l]), 2)

    def test_other_names_after_down_make_a_new_certificate_and_print_its_fingerprint(self):
        r = self.up_remote("--name", "a.example")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn(FP_A_COLON, r.stderr)
        self.down()
        mark = self.mark()
        self.fp["STUB_CERT_SHA256"] = FP_B
        r = self.up_remote("--name", "b.example")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(len(self.cert_makes(mark)), 1)
        self.assertIn("b.example", (self.page_dir / "cert.names").read_text())
        self.assertNotIn("a.example", (self.page_dir / "cert.names").read_text())
        self.assertIn(FP_B_COLON, r.stderr)
        self.assertEqual(self.named_servers(), ["b.example"])

    def test_a_certificate_older_than_358_days_is_made_again(self):
        r = self.up_remote("--name", "a.example")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.down()
        self.age_certificate(400)
        mark = self.mark()
        self.fp["STUB_CERT_SHA256"] = FP_B
        r = self.up_remote("--name", "a.example")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(len(self.cert_makes(mark)), 1)
        self.assertIn(FP_B_COLON, r.stderr)

    def test_a_certificate_younger_than_358_days_is_kept(self):
        self.assertEqual(self.up_remote("--name", "a.example").returncode, 0)
        self.down()
        self.age_certificate(100)
        mark = self.mark()
        r = self.up_remote("--name", "a.example")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.cert_makes(mark), [])

    def test_the_developers_certificate_and_key_are_copied_and_none_is_made(self):
        c, k = self.base / "dev-cert.pem", self.base / "dev-key.pem"
        c.write_text(PEM_CERT)
        k.write_text(PEM_KEY)
        r = self.up_remote("--certificate", str(c), "--key", str(k))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.cert_makes(), [])
        self.assertEqual((self.page_dir / "cert.pem").read_text(), PEM_CERT)
        self.assertEqual((self.page_dir / "key.pem").read_text(), PEM_KEY)
        self.assertEqual(self.mode(self.page_dir / "cert.pem"), 0o600)
        self.assertEqual(self.mode(self.page_dir / "key.pem"), 0o600)
        self.assertIn(FP_A_COLON, r.stderr)

    def test_a_port_compose_cannot_publish_exits_1_naming_the_address_and_port(self):
        r = self.up_remote(STUB_COMPOSE_UP_FAIL="address already in use")
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn("0.0.0.0", r.stderr)
        self.assertIn("7878", r.stderr)
        self.assertIn("address already in use", r.stderr)

    def test_the_remote_lines_of_up_and_the_secret_only_in_the_sign_in_line(self):
        r = self.up_remote()
        self.assertEqual(r.returncode, 0, r.stderr)
        secret = self.secret()
        out = r.stdout + r.stderr
        self.assertIn("up: the run page accepts connections from other machines on 0.0.0.0:7878", r.stderr)
        self.assertIn("up: open https://vm.example:7878  (names: %s)" % ", ".join(DEFAULT_NAMES), r.stderr)
        self.assertIn("up: sign in as ap with " + secret, r.stderr)
        self.assertIn("up: certificate sha256 " + FP_A_COLON + "  (compare it once in your browser)", r.stderr)
        self.assertIn("up: recorded in %s" % self.access_log, r.stderr)
        with_secret = [l for l in out.splitlines() if secret in l]
        self.assertEqual(len(with_secret), 1, with_secret)
        self.assertIn("sign in as ap with", with_secret[0])
        self.assertNoSecret(r)

    def test_start_prints_the_secret_only_in_the_sign_in_line(self):
        r = self.launch("start", "--listen", "0.0.0.0", "--name", "box.example", COSIGN_PASSWORD=PASSWORD)
        self.assertEqual(r.returncode, 0, r.stderr)
        secret = self.secret()
        with_secret = [l for l in (r.stdout + r.stderr).splitlines() if secret in l]
        self.assertEqual(len(with_secret), 1, with_secret)
        self.assertIn("sign in as ap with " + secret, with_secret[0])
        self.assertIn("https://box.example:7878", r.stderr)

    def test_page_prints_the_https_names_the_secret_and_the_fingerprint(self):
        self.assertEqual(self.up_remote().returncode, 0)
        r = self.run_ap("page", **self.fp)
        self.assertEqual(r.returncode, 0, r.stderr)
        lines = r.stdout.splitlines()
        urls = [l for l in lines if l.startswith("https://")]
        self.assertEqual(urls, ["https://%s:7878" % n for n in DEFAULT_NAMES])
        sign = [i for i, l in enumerate(lines) if "sign in as ap with " + self.secret() in l]
        self.assertEqual(len(sign), 1, lines)
        self.assertGreater(sign[0], max(i for i, l in enumerate(lines) if l.startswith("https://")))
        self.assertIn(FP_A_COLON, "\n".join(lines[sign[0]:]))
        self.assertNotIn("http://127.0.0.1", r.stdout)


class ChangeOnStartedSessionTest(RemoteCase):
    def started_remote(self, *more):
        r = self.up_remote(*more)
        self.assertEqual(r.returncode, 0, r.stderr)
        return self.session.read_text(), self.access_lines()

    def refused(self, kind, *opts, before):
        mark = self.mark()
        r = self.launch(kind, *opts, COSIGN_PASSWORD=PASSWORD)
        self.assertRefusedOnStarted(r, before[0], before[1], mark)

    def test_listen_after_a_local_start_is_refused(self):
        self.assertEqual(self.launch("up").returncode, 0)
        before = (self.session.read_text(), self.access_lines())
        self.refused("up", "--listen", "0.0.0.0", before=before)
        self.refused("start", "--listen", "0.0.0.0", before=before)

    def test_up_without_listen_after_a_remote_start_is_refused(self):
        before = self.started_remote()
        self.refused("up", before=before)
        self.refused("start", before=before)

    def test_another_listen_address_is_refused(self):
        before = self.started_remote()
        self.refused("up", "--listen", "192.0.2.10", before=before)
        self.refused("up", "--listen", "::", before=before)

    def test_other_names_are_refused(self):
        before = self.started_remote()
        self.refused("up", "--listen", "0.0.0.0", "--name", "other.example", before=before)
        self.refused("start", "--listen", "0.0.0.0", "--name", "other.example", before=before)

    def test_another_certificate_is_refused(self):
        before = self.started_remote()
        c, k = self.base / "dev-cert.pem", self.base / "dev-key.pem"
        c.write_text(PEM_CERT)
        k.write_text(PEM_KEY)
        self.refused("up", "--listen", "0.0.0.0", "--certificate", str(c), "--key", str(k), before=before)


class AccessLogTest(RemoteCase):
    def test_on_line_has_address_port_names_and_fingerprint_and_never_the_secret(self):
        r = self.up_remote("--name", "box.example", "--name", "192.0.2.50")
        self.assertEqual(r.returncode, 0, r.stderr)
        lines = self.access_lines()
        self.assertEqual(len(lines), 1, lines)
        m = ON_LINE.match(lines[0])
        self.assertIsNotNone(m, lines[0])
        self.assertEqual(m.group(1), "0.0.0.0")
        self.assertEqual(m.group(2), "7878")
        self.assertEqual(m.group(3), "box.example,192.0.2.50")
        self.assertEqual(m.group(4), FP_A_COLON)
        self.assertNotIn(self.secret(), self.access_log.read_text())
        self.assertEqual(self.mode(self.sdir), 0o700)

    def test_one_off_line_at_the_first_start_without_listen_after_a_remote_one(self):
        self.assertEqual(self.up_remote().returncode, 0)
        self.down()
        self.assertEqual(len(self.access_lines()), 1, "down writes nothing")
        r = self.launch("up")
        self.assertEqual(r.returncode, 0, r.stderr)
        lines = self.access_lines()
        self.assertEqual(len(lines), 2, lines)
        self.assertRegex(lines[0], ON_LINE)
        self.assertRegex(lines[1], OFF_LINE)
        self.down()
        self.assertEqual(self.launch("up").returncode, 0)
        self.assertEqual(len(self.access_lines()), 2, "only the first start without it writes off")
        self.assertNoSecret(r)

    def test_start_without_listen_after_a_remote_start_writes_the_off_line_too(self):
        self.assertEqual(self.launch("start", "--listen", "0.0.0.0", COSIGN_PASSWORD=PASSWORD).returncode, 0)
        self.down()
        r = self.launch("start", COSIGN_PASSWORD=PASSWORD)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(len(self.access_lines()), 2)
        self.assertRegex(self.access_lines()[1], OFF_LINE)

    def test_a_start_that_never_had_remote_access_writes_no_line(self):
        self.assertEqual(self.launch("up").returncode, 0)
        self.down()
        self.assertEqual(self.launch("up").returncode, 0)
        self.assertEqual(self.access_lines(), [])


class WithoutListenTest(RemoteCase):
    def test_up_without_listen_makes_the_docker_calls_it_always_made_and_nothing_remote(self):
        runs = []
        for stub in (OLD_STUB, NEW_STUB):
            self.install_stub(stub)
            self.log.unlink(missing_ok=True)
            if self.session.exists():
                self.session.unlink()
            r = self.launch("up")
            self.assertEqual(r.returncode, 0, r.stderr)
            runs.append(self.argvs())
        self.assertTrue(any("up" in a for a in runs[0] if a[:1] == ["compose"]), "up must reach compose up")
        self.assertEqual(runs[0], runs[1])
        for a in runs[1]:
            if a[:1] == ["compose"]:
                self.assertEqual(self.compose_files(a), ["compose.yaml"], a)
        self.assertFalse(self.page_dir.exists(), "page-remote/ was made")
        self.assertFalse(self.access_log.exists())
        text = self.session.read_text()
        for var in ("AP_PAGE_ADDRESS", "AP_PAGE_NAMES", "AP_PAGE_SECRET", "AP_PAGE_CERT_DIR"):
            self.assertNotIn(var, text)


if __name__ == "__main__":
    unittest.main()
