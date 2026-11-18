"""Tests for the run page's remote mode (feature 003, T014; spec FR-012 to FR-017, SC-006, SC-007).

Two files are judged:
  src/deploy/page-proxy/nginx-remote.conf  the forwarder's second, fixed configuration
  src/deploy/compose.remote.yaml           the Compose file merged after compose.yaml

The configuration is run in the pinned `nginx` image (src/deploy/images.lock) with a certificate
made as research R5 says (Go's generate_cert.go in the pinned `golang` image, no network), a
password file made by the nginx image's `mkpasswd -m sha512` with the secret on standard input,
and a stand-in page (the same nginx image) on an internal network that answers with the Host header
it received. The harness uses no container-engine socket: it only runs `docker` commands on the
host, and every container it starts is read-only, drops all capabilities and publishes one port on
127.0.0.1 only. Missing Docker is a failure, never a skip.

Fixed by T016 and assumed here: the configuration reads /etc/ap/remote/{cert.pem,key.pem,names.conf,
htpasswd}; names.conf holds `server_name ...;` and `set $page_host "127.0.0.1:<port>";`, and the
forwarder sets the page's Host from `$page_host`, not from the client's Host; the forwarder reaches the page as `page:8080` on the network it shares with it (as
nginx.conf does); the container port the TLS server listens on is the `target` of the one port
compose.remote.yaml publishes (read from the resolved Compose configuration, not assumed).
"""
import base64
import ipaddress
import json
import os
import re
import secrets
import shutil
import socket
import ssl
import subprocess
import tempfile
import time
import unittest
import http.client
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent.parent
DEPLOY = REPO / "src/deploy"
COMPOSE = DEPLOY / "compose.yaml"
REMOTE_COMPOSE = DEPLOY / "compose.remote.yaml"
REMOTE_CONF = DEPLOY / "page-proxy/nginx-remote.conf"
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
MOUNT = "/etc/ap/remote"
NAMES = "127.0.0.1 localhost vm.test"
IMAGE_VARS = ["AP_IMAGE_ENGINE", "AP_IMAGE_PAGE", "AP_IMAGE_CLI", "AP_IMAGE_AGENT",
              "AP_IMAGE_CHECK", "AP_IMAGE_TESTS", "AP_IMAGE_LINT", "AP_IMAGE_PROJECT"]
TOKEN_VARS = ["AP_CLIENT_TOKEN", "AP_PAGE_TOKEN", "AP_WORKER_TOKEN", "AP_LINT_TOKEN",
              "AP_ANALYST_TOKEN", "AP_CHECK_TOKEN", "AP_PROJECT_TOKEN"]
PAGE_CONTENT = "page-content"


def pin(name):
    """A pin read from its one home, src/deploy/images.lock."""
    for line in (DEPLOY / "images.lock").read_text().splitlines():
        f = line.split()
        if len(f) >= 2 and f[0] == name:
            return f[1]
    raise AssertionError("no %s pin in images.lock" % name)


def require_files():
    missing = [str(p.relative_to(REPO)) for p in (REMOTE_CONF, REMOTE_COMPOSE) if not p.is_file()]
    if missing:
        raise AssertionError("T016's files do not exist yet: %s" % ", ".join(missing))


def require_docker():
    if not shutil.which("docker"):
        raise AssertionError("docker is required by test_page_remote.py and is not on PATH")
    r = subprocess.run(["docker", "compose", "version"], capture_output=True, text=True, timeout=60)
    if r.returncode != 0:
        raise AssertionError("docker compose is required by test_page_remote.py: %s" % r.stderr.strip())


def docker(*args, check=True, input=None, timeout=120):
    r = subprocess.run(["docker", *args], capture_output=True, text=True, input=input, timeout=timeout)
    if check and r.returncode != 0:
        raise AssertionError("docker %s failed (%d):\n%s%s" % (" ".join(args[:3]), r.returncode,
                                                               r.stdout, r.stderr))
    return r


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def compose_env(address, cert_dir, port="18443"):
    """Dummy values for every variable compose.yaml needs; none is a real credential."""
    e = {k: v for k, v in os.environ.items() if not k.startswith("AP_")}
    base = tempfile.gettempdir()
    e.update({"AP_CREDENTIALS_FILE": base + "/ap-t014-models.env", "AP_PROJECTS_DIR": base + "/ap-t014-projects",
              "AP_PAGE_PORT": port, "AP_GATEWAY_IMAGE": pin("nginx"), "AP_HOST_USER": "1234:1234",
              "AP_PAGE_ADDRESS": address, "AP_PAGE_CERT_DIR": str(cert_dir)})
    for i, v in enumerate(TOKEN_VARS):
        e[v] = "t014%02d" % i + "a" * 26
    for i, v in enumerate(IMAGE_VARS):
        e[v] = "127.0.0.1:5005/ap-t014@sha256:%064x" % (i + 1)
    return e


def compose_json(files, env):
    cmd = ["docker", "compose"]
    for f in files:
        cmd += ["-f", str(f)]
    r = subprocess.run(cmd + ["--profile", "*", "config", "--format", "json"], env=env,
                       capture_output=True, text=True, timeout=120)
    if r.returncode != 0:
        raise AssertionError("docker compose config failed on %s:\n%s" % (files, r.stderr))
    return json.loads(r.stdout)


STAND_IN_CONF = """\
worker_processes 1;
pid /tmp/nginx.pid;
error_log /dev/stderr warn;
events { worker_connections 64; }
http {
    log_format hit 'HIT host=$http_host';
    access_log /dev/stdout hit;
    client_body_temp_path /tmp/c; proxy_temp_path /tmp/p; fastcgi_temp_path /tmp/f;
    uwsgi_temp_path /tmp/u; scgi_temp_path /tmp/s;
    server {
        listen 8080;
        location / {
            default_type text/plain;
            return 200 "%s host=$http_host\\n";
        }
    }
}
""" % PAGE_CONTENT


class Fixture:
    """The made certificate and password file, the stand-in page and the forwarder under test."""

    def __init__(self):
        self.tag = "ap-t014-%d-%s" % (os.getpid(), secrets.token_hex(3))
        self.containers = []
        self.networks = []
        self.dir = Path(tempfile.mkdtemp(prefix="test-page-remote-", dir=TMP_BASE))
        self.uid = "%d:%d" % (os.getuid(), os.getgid())
        self.secret = secrets.token_hex(32)
        try:
            self.start()
        except BaseException:
            self.close()
            raise

    def close(self):
        for c in reversed(self.containers):
            subprocess.run(["docker", "rm", "-f", c], capture_output=True, timeout=60)
        for n in reversed(self.networks):
            subprocess.run(["docker", "network", "rm", n], capture_output=True, timeout=60)
        shutil.rmtree(self.dir, ignore_errors=True)

    def run(self, name, *args):
        full = "%s-%s" % (self.tag, name)
        self.containers.append(full)
        docker("run", "--name", full, "--label", "ap-t014=" + self.tag, *args)
        return full

    def make_certificate(self):
        self.host_port = free_port()
        out = self.dir / "remote"
        out.mkdir()
        out.chmod(0o700)
        hosts = ",".join(NAMES.split())
        # research R5 / S1: the pinned golang image, no network, read-only root, executable /tmp
        self.run("cert", "--network", "none", "--read-only", "--cap-drop", "ALL",
                 "--security-opt", "no-new-privileges:true", "--user", self.uid,
                 "--tmpfs", "/tmp:exec,mode=1777,size=512m", "-e", "HOME=/tmp", "-e", "GOCACHE=/tmp/gocache",
                 "-e", "GOPATH=/tmp/go", "-e", "GOTOOLCHAIN=local",
                 "-v", "%s:/out" % out, "-w", "/out", "--entrypoint", "sh", pin("golang"), "-c",
                 'go run "$(go env GOROOT)/src/crypto/tls/generate_cert.go" --host "$0" '
                 '--ecdsa-curve P256 --duration 8760h', hosts)
        for f in ("cert.pem", "key.pem"):
            self.assertFile(out / f)
        # the password file: the nginx image's mkpasswd, the secret on standard input
        r = docker("run", "-i", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
                   "--entrypoint", "mkpasswd", pin("nginx"), "-m", "sha512", input=self.secret + "\n")
        line = r.stdout.strip()
        if not line.startswith("$6$"):
            raise AssertionError("mkpasswd -m sha512 gave no $6$ crypt line: %r" % line[:8])
        (out / "htpasswd").write_text("ap:%s\n" % line)
        # names.conf as the data model says: the names, and the page's own Host (the published port)
        (out / "names.conf").write_text('server_name %s;\nset $page_host "127.0.0.1:%d";\n' % (
            NAMES, self.host_port))
        for f in out.iterdir():
            f.chmod(0o600)
        self.cert = out / "cert.pem"
        self.certdir = out

    @staticmethod
    def assertFile(p):
        if not p.is_file():
            raise AssertionError("not made: %s" % p)

    def start(self):
        self.make_certificate()
        remote = compose_json([COMPOSE, REMOTE_COMPOSE], compose_env("127.0.0.1", self.certdir))
        ports = remote["services"]["page-proxy"].get("ports") or []
        if len(ports) != 1:
            raise AssertionError("compose.remote.yaml must publish exactly one port, got %r" % ports)
        target = int(ports[0]["target"])
        (self.dir / "stand-in.conf").write_text(STAND_IN_CONF)
        host_net, page_net = self.tag + "-host", self.tag + "-page"
        docker("network", "create", "--label", "ap-t014=" + self.tag, host_net)
        self.networks.append(host_net)
        docker("network", "create", "--internal", "--label", "ap-t014=" + self.tag, page_net)
        self.networks.append(page_net)
        self.host_net = host_net
        self.stand_in = self.run(
            "page", "-d", "--network", page_net, "--network-alias", "page", "--read-only",
            "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--user", self.uid,
            "--tmpfs", "/tmp:mode=1777,size=16m", "-v", "%s:/etc/ap/nginx.conf:ro" % (self.dir / "stand-in.conf"),
            "--entrypoint", "nginx", pin("nginx"), "-c", "/etc/ap/nginx.conf", "-g", "daemon off;")
        # the forwarder as compose.yaml's page-proxy runs it: read-only, no capabilities, /tmp tmpfs,
        # only the host network publishing, plus the internal network the page is on
        self.proxy = self.run(
            "proxy", "-d", "--network", host_net, "--read-only", "--cap-drop", "ALL",
            "--security-opt", "no-new-privileges:true", "--user", self.uid,
            "--tmpfs", "/tmp:mode=1777,size=16m", "-p", "127.0.0.1:%d:%d" % (self.host_port, target),
            "-v", "%s:/etc/ap/nginx.conf:ro" % REMOTE_CONF, "-v", "%s:%s:ro" % (self.certdir, MOUNT),
            "--entrypoint", "nginx", pin("nginx"), "-c", "/etc/ap/nginx.conf", "-g", "daemon off;")
        docker("network", "connect", page_net, self.proxy)
        self.wait_ready()

    def wait_ready(self):
        end = time.time() + 30
        while time.time() < end:
            if docker("inspect", "-f", "{{.State.Running}}", self.proxy).stdout.strip() != "true":
                break
            try:
                self.get(auth=None)
                return
            except (OSError, ssl.SSLError, http.client.HTTPException):
                time.sleep(0.3)
        raise AssertionError("the forwarder did not answer on 127.0.0.1:%d:\n%s" % (
            self.host_port, docker("logs", self.proxy, check=False).stderr))

    def connection(self):
        ctx = ssl.create_default_context(cafile=str(self.cert))
        return http.client.HTTPSConnection("127.0.0.1", self.host_port, context=ctx, timeout=10)

    def get(self, auth=None, host=None, conn=None):
        """GET / over TLS (the made certificate is the only trusted CA); returns (status, body)."""
        conn = conn or self.connection()
        headers = {"Host": host or "127.0.0.1:%d" % self.host_port}
        if auth is not None:
            headers["Authorization"] = "Basic " + base64.b64encode(auth.encode()).decode()
        conn.request("GET", "/", headers=headers)
        r = conn.getresponse()
        return r.status, r.read().decode(errors="replace")

    def stand_in_hits(self):
        r = docker("logs", self.stand_in, check=False)
        return [l for l in (r.stdout + r.stderr).splitlines() if l.startswith("HIT ")]

    def proxy_log(self):
        r = docker("logs", self.proxy, check=False)
        return r.stdout + r.stderr


class RemoteTestCase(unittest.TestCase):
    """Files are checked first, before any Docker call, so a missing file fails fast."""
    fixture = None
    fixture_error = None

    def setUp(self):
        require_files()
        require_docker()

    @classmethod
    def tearDownClass(cls):
        if RemoteTestCase.fixture is not None:
            RemoteTestCase.fixture.close()
            RemoteTestCase.fixture = None

    def fx(self):
        if RemoteTestCase.fixture_error:
            raise RemoteTestCase.fixture_error
        if RemoteTestCase.fixture is None:
            try:
                RemoteTestCase.fixture = Fixture()
            except BaseException as e:
                RemoteTestCase.fixture_error = e
                raise
        return RemoteTestCase.fixture


class ForwarderTest(RemoteTestCase):
    """nginx-remote.conf in the pinned nginx image (S1)."""

    def test_the_secret_gets_the_page_with_the_host_the_client_sent(self):
        f = self.fx()
        status, body = f.get(auth="ap:" + f.secret)
        self.assertEqual(status, 200, body)
        self.assertIn(PAGE_CONTENT, body)
        want = "127.0.0.1:%d" % f.host_port
        self.assertIn("host=%s\n" % want, body + "\n")
        self.assertIn("HIT host=%s" % want, f.stand_in_hits())

    def test_a_listed_name_gets_the_page_with_the_host_from_names_conf(self):
        f = self.fx()
        want = "127.0.0.1:%d" % f.host_port
        status, body = f.get(auth="ap:" + f.secret, host="vm.test:%d" % f.host_port)
        self.assertEqual(status, 200, body)
        self.assertIn("host=%s\n" % want, body + "\n")
        self.assertEqual(f.stand_in_hits()[-1], "HIT host=%s" % want)

    def test_no_secret_or_a_wrong_one_gets_401_and_no_page_content(self):
        f = self.fx()
        before = len(f.stand_in_hits())
        for auth in (None, "ap:wrong-" + secrets.token_hex(8), "other:" + f.secret):
            status, body = f.get(auth=auth)
            self.assertEqual(status, 401, auth)
            self.assertNotIn(PAGE_CONTENT, body)
        self.assertEqual(len(f.stand_in_hits()), before, "a refused request reached the page")

    def test_plain_http_on_the_tls_port_gets_400_and_no_page_content(self):
        f = self.fx()
        c = http.client.HTTPConnection("127.0.0.1", f.host_port, timeout=10)
        c.request("GET", "/", headers={"Authorization": "Basic " + base64.b64encode(
            ("ap:" + f.secret).encode()).decode()})
        r = c.getresponse()
        body = r.read().decode(errors="replace")
        self.assertEqual(r.status, 400, body)
        self.assertNotIn(PAGE_CONTENT, body)

    def test_a_host_not_in_the_names_gets_421(self):
        f = self.fx()
        before = len(f.stand_in_hits())
        status, body = f.get(auth="ap:" + f.secret, host="other.test")
        self.assertEqual(status, 421, body)
        self.assertNotIn(PAGE_CONTENT, body)
        self.assertEqual(len(f.stand_in_hits()), before)

    def test_the_log_names_the_client_for_a_failed_sign_in_and_holds_no_secret_tried(self):
        f = self.fx()
        tried = "tried-" + secrets.token_hex(12)
        status, _ = f.get(auth="ap:" + tried)
        self.assertEqual(status, 401)
        status, _ = f.get(auth="ap:" + f.secret)  # a good sign-in must not leak either
        self.assertIn(status, (200, 429))
        time.sleep(1)
        log = f.proxy_log()
        subnet = ipaddress.ip_network(json.loads(docker(
            "network", "inspect", f.host_net).stdout)[0]["IPAM"]["Config"][0]["Subnet"])
        failed = [l for l in log.splitlines() if re.search(r"mismatch|401", l)]
        self.assertTrue(failed, "no line in the forwarder's log for the failed sign-in:\n" + log)
        clients = [ipaddress.ip_address(m) for l in failed
                   for m in re.findall(r"\b\d{1,3}(?:\.\d{1,3}){3}\b", l)]
        self.assertTrue(any(c in subnet for c in clients),
                        "no client address in the failed sign-in lines %r (subnet %s)" % (failed, subnet))
        for s in (tried, f.secret, base64.b64encode(("ap:" + tried).encode()).decode(),
                  base64.b64encode(("ap:" + f.secret).encode()).decode()):
            self.assertNotIn(s, log)

    def test_zz_a_burst_of_wrong_requests_gets_429(self):
        f = self.fx()
        time.sleep(3)  # let the limiter's bucket refill after the other cases
        conn = f.connection()
        statuses = []
        for _ in range(40):
            status, body = f.get(auth="ap:wrong-" + secrets.token_hex(4), conn=conn)
            self.assertNotIn(PAGE_CONTENT, body)
            statuses.append(status)
        self.assertEqual(statuses[0], 401)
        self.assertEqual(statuses[-1], 429, statuses)
        self.assertLessEqual(set(statuses), {401, 429}, statuses)
        self.assertGreaterEqual(statuses.count(401), 20, "a burst of 20 must be allowed: %r" % statuses)


class ComposeRemoteTest(RemoteTestCase):
    """compose.remote.yaml merged after compose.yaml (`docker compose config`)."""

    def merged(self, address):
        d = Path(tempfile.mkdtemp(prefix="test-page-remote-cfg-", dir=TMP_BASE))
        self.addCleanup(shutil.rmtree, d, ignore_errors=True)
        env = compose_env(address, d)
        base = compose_json([COMPOSE], env)
        both = compose_json([COMPOSE, REMOTE_COMPOSE], env)
        return base, both, env

    def check(self, address):
        base, both, env = self.merged(address)
        self.assertEqual({k: v for k, v in both.items() if k != "services"},
                         {k: v for k, v in base.items() if k != "services"},
                         "compose.remote.yaml changes more than services")
        self.assertEqual(set(both["services"]), set(base["services"]))
        for name in base["services"]:
            if name != "page-proxy":
                self.assertEqual(both["services"][name], base["services"][name], name + " changed")
        old, new = base["services"]["page-proxy"], both["services"]["page-proxy"]
        self.assertNotEqual(new, old)

        ports = new.get("ports") or []
        self.assertEqual(len(ports), 1, "page-proxy must publish exactly one port: %r" % ports)
        p = ports[0]
        self.assertEqual(p["host_ip"], address)
        self.assertEqual(str(p["published"]), env["AP_PAGE_PORT"])
        self.assertEqual(p.get("protocol", "tcp"), "tcp")
        self.assertIsInstance(p["target"], int)
        self.assertFalse([q for q in ports if q.get("host_ip") == "127.0.0.1"], "a 127.0.0.1 port is left")

        self.assertEqual(new["user"], env["AP_HOST_USER"])
        self.assertIs(new.get("read_only"), True)
        self.assertEqual(new.get("cap_drop"), ["ALL"])
        self.assertEqual(new.get("security_opt"), old.get("security_opt"))
        self.assertEqual(set(new["networks"]), set(old["networks"]))
        self.assertEqual(new.get("tmpfs"), old.get("tmpfs"))

        vols = [v for v in new.get("volumes", []) if v.get("type") == "bind"]
        conf = [v for v in vols if v["source"].endswith("page-proxy/nginx-remote.conf")]
        self.assertEqual(len(conf), 1, "nginx-remote.conf is not mounted: %r" % vols)
        self.assertTrue(conf[0].get("read_only"))
        cert_dir = os.path.normpath(env["AP_PAGE_CERT_DIR"])
        for name in ("cert.pem", "key.pem", "names.conf", "htpasswd"):
            path = "%s/%s" % (MOUNT, name)
            hit = [v for v in vols if os.path.normpath(v["source"]).startswith(cert_dir)
                   and v["target"] in (MOUNT, path)]
            self.assertTrue(hit, "%s is not mounted from AP_PAGE_CERT_DIR: %r" % (path, vols))
            self.assertTrue(all(v.get("read_only") for v in hit), path + " is not read-only")

    def test_listening_on_all_ipv4_addresses_changes_only_page_proxy(self):
        self.check("0.0.0.0")

    def test_listening_on_all_ipv6_addresses_changes_only_page_proxy(self):
        self.check("::")


if __name__ == "__main__":
    unittest.main()
