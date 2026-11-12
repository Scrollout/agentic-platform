"""Tests for src/deploy/compose.yaml (T022): the security and isolation rules of the Compose stack.

`check_config` is one checker applied to the output of `docker compose config --format json`.
It must pass on the real file and on fixtures/compose/good.yaml, and fail on each planted
fixture for exactly the one rule that fixture violates (constitution: checks prove they can fail).
Nothing here reads compose.yaml as text; only the resolved configuration is judged.
"""
import json
import os
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent.parent
# AP_TEST_COMPOSE points the tests at another compose file (to see them fail against a mutated copy)
COMPOSE = Path(os.environ.get("AP_TEST_COMPOSE") or REPO / "src/deploy/compose.yaml")
FIXTURES = Path(__file__).resolve().parent / "fixtures/compose"
VARS = ["AP_CREDENTIALS_FILE", "AP_PROJECTS_DIR", "AP_PAGE_PORT",
        "AP_CLIENT_TOKEN", "AP_PAGE_TOKEN", "AP_WORKER_TOKEN", "AP_GATEWAY_IMAGE", "AP_HOST_USER",
        "AP_IMAGE_ENGINE", "AP_IMAGE_PAGE", "AP_IMAGE_CLI", "AP_IMAGE_AGENT", "AP_IMAGE_CHECK",
        "AP_IMAGE_TESTS"]
ISOLATED = "com.docker.network.bridge.gateway_mode_ipv4"
MASQ = "com.docker.network.bridge.enable_ip_masquerade"


def gateway_pin():
    """The nginx pin, read from its one home, src/deploy/images.lock."""
    for line in (REPO / "src/deploy/images.lock").read_text().splitlines():
        f = line.split()
        if len(f) >= 2 and f[0] == "nginx":
            return f[1]
    raise AssertionError("no nginx pin in images.lock")
KEY = "fake-model-key-TESTKEY-0123456789abcdef"  # also written in fixtures/compose/bad-key-in-env.yaml
TOKENS = {"AP_CLIENT_TOKEN": "c11e47" + "a" * 26, "AP_PAGE_TOKEN": "9a6e00" + "b" * 26,
          "AP_WORKER_TOKEN": "w0rke4" + "c" * 26}
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
RULES = ["name", "read_only", "cap_drop", "no_new_privileges", "privileged", "non_root",
         "engine_socket", "var_run_bind", "network_egress", "gateway_networks", "runtime",
         "masquerade_page_only", "limit_cpu", "limit_memory", "limit_pids", "credentials_gateway",
         "credentials_only_gateway", "key_in_env", "page_port_loopback", "other_ports",
         "token_engine", "token_page", "token_cli", "token_worker", "token_gateway", "image_digest",
         "pull_policy_never", "gateway_digest", "restart", "page_isolation", "internal_isolated"]
SOCKETS = ("docker.sock", "containerd.sock", "podman.sock")
# Services are identified by the role of the variable their image comes from (ADR 0028: the image
# is a signed digest reference the launcher passes in), not by an image name.
IMAGE_VARS = {"AP_IMAGE_ENGINE": ("engine", "engine"), "AP_IMAGE_PAGE": ("page", "page"),
              "AP_IMAGE_CLI": ("cli", "cli"), "AP_IMAGE_AGENT": ("agent", "worker"),
              "AP_IMAGE_CHECK": ("check", "worker"), "AP_IMAGE_TESTS": ("tests", "client")}
REGISTRY = "127.0.0.1:5005"
# One distinct digest reference per variable, so a resolved image maps back to its variable.
IMAGE_ENV = {v: "%s/ap-%s@sha256:%064x" % (REGISTRY, n, i + 1)
             for i, (v, (n, _)) in enumerate(IMAGE_VARS.items())}
BY_REF = {ref: IMAGE_VARS[v] for v, ref in IMAGE_ENV.items()}
SANDBOXED = ("agent", "check")

# fixture file -> the one rule it violates
PLANTED = {
    "bad-name": "name", "bad-read-only": "read_only", "bad-cap-drop": "cap_drop",
    "bad-no-new-privileges": "no_new_privileges", "bad-privileged": "privileged",
    "bad-non-root": "non_root", "bad-non-root-missing": "non_root",
    "bad-engine-socket": "engine_socket", "bad-var-run-bind": "var_run_bind",
    "bad-network-egress-page-normal-bridge": "network_egress",
    "bad-masquerade-page-only": "masquerade_page_only", "bad-token-tests": "token_cli",
    "bad-network-egress": "network_egress", "bad-network-egress-default-net": "network_egress",
    "bad-gateway-networks": "gateway_networks", "bad-runtime": "runtime",
    "bad-limit-cpu": "limit_cpu", "bad-limit-memory": "limit_memory", "bad-limit-pids": "limit_pids",
    "bad-credentials-gateway": "credentials_gateway",
    "bad-credentials-only-gateway": "credentials_only_gateway",
    "bad-credentials-only-gateway-bind": "credentials_only_gateway",
    "bad-key-in-env": "key_in_env", "bad-page-port-loopback": "page_port_loopback",
    "bad-page-port-loopback-unset": "page_port_loopback", "bad-other-ports": "other_ports",
    "bad-token-engine": "token_engine", "bad-token-page": "token_page",
    "bad-token-cli": "token_cli", "bad-token-worker": "token_worker",
    "bad-token-gateway": "token_gateway", "bad-image-tag-only": "image_digest",
    # T022b: limits on every service, ours never pulled, the gateway pinned by digest, no restart
    "bad-limit-cpu-engine": "limit_cpu", "bad-limit-cpu-gateway": "limit_cpu",
    "bad-limit-cpu-zero-cli": "limit_cpu", "bad-limit-memory-page": "limit_memory",
    "bad-limit-memory-gateway": "limit_memory", "bad-limit-pids-tests": "limit_pids",
    "bad-limit-pids-gateway": "limit_pids",
    "bad-pull-policy-missing": "pull_policy_never", "bad-pull-policy-always": "pull_policy_never",
    "bad-pull-policy-missing-worker": "pull_policy_never",
    "bad-gateway-tag-only": "gateway_digest", "bad-gateway-unpinned": "gateway_digest",
    "bad-restart-always": "restart", "bad-restart-unless-stopped": "restart",
    "bad-restart-on-failure": "restart", "bad-restart-deploy-policy": "restart",
    # T022a: the run page is on internal networks only; a publishing forwarder is the one exception
    "bad-internal-not-isolated": "internal_isolated",
    "bad-page-no-route-bridge": "page_isolation",
    "bad-forwarder-token": "page_isolation", "bad-forwarder-projects-mount": "page_isolation",
    "bad-forwarder-engine-network": "page_isolation", "bad-forwarder-egress-network": "page_isolation",
}
GOOD = {"good": ["agent-one", "check-one", "cli", "engine", "gateway", "page", "tests"],
        "good-forwarder": ["agent-one", "check-one", "cli", "engine", "gateway", "page", "page-forwarder", "tests"]}


def image_ref(svc):
    return svc.get("image") or ""


def image_kind(svc):
    """(image name, role) of a service: by the variable its image came from, else by repository name
    (a tag-only image is still judged by every other rule, and fails image_digest)."""
    ref = image_ref(svc)
    if ref in BY_REF:
        return BY_REF[ref]
    repo = ref.split("@")[0].rsplit("/", 1)[-1].split(":")[0]
    if repo == "nginx":
        return ("nginx", "gateway")
    for n, role in IMAGE_VARS.values():
        if repo == "ap-" + n:
            return (n, role)
    return ("", "other")


def role_of(svc):
    """The role by image alone. `roles_of` refines it (a second nginx is a forwarder, not the gateway)."""
    return image_kind(svc)[1]


def roles_of(cfg, env=None):
    """service name -> role: engine, page, cli, client, worker, gateway, forwarder or other.

    The gateway is the nginx service that reaches the credentials file (else the only nginx one). A
    publishing forwarder (T022a) is any other nginx service, or a service with an image that is none
    of ours and that sits on a network that is not internal; a service with an unknown image that
    is on internal networks only stays `other` (the real-file test rejects those)."""
    services = cfg.get("services") or {}
    nets = cfg.get("networks") or {}
    roles = {n: role_of(s) for n, s in services.items()}
    cred = os.path.normpath(env["AP_CREDENTIALS_FILE"]) if env else None
    nginx = [n for n, r in roles.items() if r == "gateway"]
    if len(nginx) > 1:
        def reaches(n):
            return cred is not None and any(cred == s or cred.startswith(s.rstrip("/") + "/")
                                            for s in _sources(cfg, services[n]))
        keep = [n for n in nginx if reaches(n)] or sorted(nginx)[:1]
        for n in nginx:
            if n not in keep[:1]:
                roles[n] = "forwarder"
    for n, s in services.items():
        if roles[n] == "other" and any(net_kind(nets.get(x)) != "internal" for x in _networks(s)):
            roles[n] = "forwarder"
    return roles


def _num(v):
    try:
        return float(v)
    except (TypeError, ValueError):
        return None


def _limit(svc, flat, nested):
    v = svc.get(flat)
    if v in (None, "", 0, -1, "0", "-1"):
        v = ((svc.get("deploy") or {}).get("resources") or {}).get("limits", {}).get(nested)
    if v in (None, "", 0, -1, "0", "-1"):
        return False
    n = _num(v)
    return n is None or n > 0


def _networks(svc):
    n = svc.get("networks")
    names = list(n) if n else ["default"]
    return names


def _sources(cfg, svc):
    """Host paths a service can read: file secrets, file configs, bind mounts."""
    out = []
    for kind in ("secrets", "configs"):
        for ref in svc.get(kind) or []:
            name = ref if isinstance(ref, str) else ref.get("source")
            f = (cfg.get(kind) or {}).get(name, {}).get("file")
            if f:
                out.append(os.path.normpath(f))
    for v in svc.get("volumes") or []:
        if isinstance(v, dict) and v.get("source") and v.get("type", "bind") == "bind":
            out.append(os.path.normpath(v["source"]))
    return out


def _env_values(svc):
    return [str(v) for v in (svc.get("environment") or {}).values() if v is not None]


def net_kind(net):
    """internal (internal: true), no-route (masquerade off) or egress (neither)."""
    net = net or {}
    if net.get("internal"):
        return "internal"
    if str((net.get("driver_opts") or {}).get(MASQ, "")).lower() == "false":
        return "no-route"
    return "egress"


def check_config(cfg, env):
    """Return (violations, examined): violations maps rule -> sorted services that break it."""
    bad = {}

    def fail(rule, svc):
        bad.setdefault(rule, set()).add(svc)

    services = cfg.get("services") or {}
    nets = cfg.get("networks") or {}
    cred = os.path.normpath(env["AP_CREDENTIALS_FILE"])
    tok = {k: env[k] for k in TOKENS}
    roles = roles_of(cfg, env)
    for name, svc in services.items():
        role = roles[name]
        if not re.fullmatch(r"[a-z0-9-]+", name):
            fail("name", name)
        if svc.get("read_only") is not True:
            fail("read_only", name)
        if "ALL" not in [str(c).upper() for c in svc.get("cap_drop") or []]:
            fail("cap_drop", name)
        if not any(str(o).replace("=", ":").lower() in ("no-new-privileges", "no-new-privileges:true")
                   for o in svc.get("security_opt") or []):
            fail("no_new_privileges", name)
        if svc.get("privileged"):
            fail("privileged", name)
        user = str(svc.get("user") or "").strip()
        uid = user.split(":")[0]
        if not uid or uid in ("0", "root"):
            fail("non_root", name)
        for v in svc.get("volumes") or []:
            src = str(v.get("source") or "") if isinstance(v, dict) else str(v).split(":")[0]
            tgt = str(v.get("target") or "") if isinstance(v, dict) else ":".join(str(v).split(":")[1:2])
            typ = v.get("type", "bind") if isinstance(v, dict) else "bind"
            if any(s in src or s in tgt for s in SOCKETS):
                fail("engine_socket", name)
            if typ == "bind" and (os.path.normpath(src) == "/var/run" or src.startswith("/var/run/")):
                fail("var_run_bind", name)
        # networks
        kinds = [net_kind(nets.get(n)) for n in _networks(svc)]
        if role == "forwarder":
            pass  # judged by page_isolation below
        elif role != "gateway":
            if svc.get("network_mode") not in (None, "none") or "egress" in kinds:
                fail("network_egress", name)
            elif "no-route" in kinds:
                # a bridge without masquerading is no isolation (T022a): the page may not use one
                # either, and only a publishing forwarder may
                fail("page_isolation" if role == "page" else "masquerade_page_only", name)
        else:
            if "internal" not in kinds or "egress" not in kinds or svc.get("network_mode"):
                fail("gateway_networks", name)
        # sandbox: gVisor for the workers only
        if image_kind(svc)[0] in SANDBOXED and svc.get("runtime") != "runsc":
            fail("runtime", name)
        # T022b: CPU, memory and pids limits on every service, not only the workers
        if not _limit(svc, "cpus", "cpus"):
            fail("limit_cpu", name)
        if not _limit(svc, "mem_limit", "memory"):
            fail("limit_memory", name)
        if not _limit(svc, "pids_limit", "pids"):
            fail("limit_pids", name)
        # images: ours are digest references and never pulled; the gateway (and a forwarder) are
        # upstream images pinned by digest too
        if not re.search(r"@sha256:[0-9a-f]{64}$", image_ref(svc)):
            fail("gateway_digest" if role == "gateway" else "image_digest", name)
        if role not in ("gateway", "forwarder") and svc.get("pull_policy") != "never":
            fail("pull_policy_never", name)
        # restart: unset or "no" (a stopped platform stays stopped; `ap down` is how it ends)
        restart_policy = ((svc.get("deploy") or {}).get("restart_policy") or {}).get("condition")
        if svc.get("restart") not in (None, "no", False) or restart_policy not in (None, "none"):
            fail("restart", name)
        # credentials
        reaches = any(cred == s or cred.startswith(s.rstrip("/") + "/") for s in _sources(cfg, svc))
        if role == "gateway" and not reaches:
            fail("credentials_gateway", name)
        if role != "gateway" and (reaches or cred in _env_values(svc)):
            fail("credentials_only_gateway", name)
        if any(env["KEY_VALUE"] in v for v in _env_values(svc)):
            fail("key_in_env", name)
        # ports
        ports = svc.get("ports") or []
        if role in ("page", "forwarder"):
            if any(p.get("host_ip") != "127.0.0.1" for p in ports):
                fail("page_port_loopback", name)
        elif ports:
            fail("other_ports", name)
        # tokens (by value, anywhere in the service's resolved definition)
        blob = json.dumps(svc)
        vals = _env_values(svc)
        have = {k: v in vals for k, v in tok.items()}
        allowed = {"engine": {"AP_CLIENT_TOKEN", "AP_PAGE_TOKEN", "AP_WORKER_TOKEN"},
                   "page": {"AP_PAGE_TOKEN"}, "cli": {"AP_CLIENT_TOKEN"}, "client": {"AP_CLIENT_TOKEN"},
                   "worker": {"AP_WORKER_TOKEN"}}.get(role, set())
        rule = {"engine": "token_engine", "page": "token_page", "cli": "token_cli", "client": "token_cli",
                "worker": "token_worker", "gateway": "token_gateway"}.get(role)
        if rule:
            if any(k not in allowed and v in blob for k, v in tok.items()):
                fail(rule, name)
            if role != "gateway" and any(not have[k] for k in allowed):
                fail(rule, name)
    if not any(r == "gateway" for r in roles.values()):
        fail("gateway_networks", "<no gateway>")
    # a plain internal network still reaches every host service on its bridge address; `isolated`
    # gives the bridge no host address (T022a)
    for n, net in nets.items():
        if net and net.get("internal") and (net.get("driver_opts") or {}).get(ISOLATED) != "isolated":
            fail("internal_isolated", "network:" + n)
    # the run page's port is published on 127.0.0.1 by the page or by its forwarder, by someone
    pages = [n for n, r in roles.items() if r == "page"]
    if pages and not any(services[n].get("ports") for n, r in roles.items() if r in ("page", "forwarder")):
        fail("page_port_loopback", pages[0])
    # T022a: a page on an internal network only; a forwarder is the only other service on a network
    # that is not internal, and it holds no token, no projects mount and no other network than a
    # link network shared with the page alone and its own publishing network
    projects = os.path.normpath(env["AP_PROJECTS_DIR"])
    members = {}
    for n, s in services.items():
        for x in _networks(s):
            members.setdefault(x, set()).add(n)
    for n, r in roles.items():
        if r != "forwarder":
            continue
        s = services[n]
        blob = json.dumps(s)
        if s.get("network_mode") not in (None, "none") or any(v in blob for v in tok.values()):
            fail("page_isolation", n)
        if any(projects == p or projects.startswith(p.rstrip("/") + "/") or p.startswith(projects.rstrip("/") + "/")
               for p in _sources(cfg, s)):
            fail("page_isolation", n)
        link = False
        for x in _networks(s):
            if net_kind(nets.get(x)) == "internal":
                link = link or any(roles[m] == "page" for m in members[x])
                if any(roles[m] not in ("page", "forwarder") or m != n and roles[m] == "forwarder"
                       for m in members[x]):
                    fail("page_isolation", n)
            elif members[x] != {n}:
                fail("page_isolation", n)
        if not link:
            fail("page_isolation", n)
    return {k: sorted(v) for k, v in bad.items()}, sorted(services)


def compose_config(file, env):
    e = {k: v for k, v in os.environ.items() if not k.startswith("AP_")}
    e.update(env)
    return subprocess.run(["docker", "compose", "-f", str(file), "--profile", "*", "config",
                           "--format", "json"], env=e, capture_output=True, text=True, timeout=120)


def require_docker():
    """A missing prerequisite is a failure, never a skip (constitution; T010c)."""
    if not shutil.which("docker"):
        raise AssertionError("docker is required by test_compose.py and is not on PATH")
    r = subprocess.run(["docker", "compose", "version"], capture_output=True, text=True, timeout=60)
    if r.returncode != 0:
        raise AssertionError("docker compose is required by test_compose.py: %s" % r.stderr.strip())


class ComposeTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        require_docker()
        base = tempfile.mkdtemp(prefix="test-compose-", dir=TMP_BASE)
        cls.addClassCleanup(shutil.rmtree, base, ignore_errors=True)
        key = Path(base) / "models.env"
        key.write_text(KEY + "\n")  # the file holds only the key
        key.chmod(0o600)
        projects = Path(base) / "projects"
        projects.mkdir()
        cls.env = {"AP_CREDENTIALS_FILE": str(key), "AP_PROJECTS_DIR": str(projects),
                   "AP_PAGE_PORT": "18081", **TOKENS,
                   "AP_GATEWAY_IMAGE": gateway_pin(), "AP_HOST_USER": "1234:1234", **IMAGE_ENV}

    def checker_input(self, file):
        r = compose_config(file, self.env)
        self.assertEqual(r.returncode, 0, "docker compose config failed on %s:\n%s" % (file, r.stderr))
        return json.loads(r.stdout)

    def check(self, file):
        return check_config(self.checker_input(file), {**self.env, "KEY_VALUE": KEY})

    # The checker itself: clean fixture passes, each planted fixture fails for its one rule.
    def test_clean_fixtures_pass(self):
        """Both accepted designs of the run page's publishing (T022a): the page alone on internal
        networks, and the page on internal networks with a dedicated publishing forwarder."""
        for stem, services in sorted(GOOD.items()):
            with self.subTest(fixture=stem):
                bad, examined = self.check(FIXTURES / (stem + ".yaml"))
                self.assertEqual(bad, {})
                self.assertEqual(examined, services)

    def test_root_host_user_is_flagged_by_non_root_rule(self):
        env = {**self.env, "AP_HOST_USER": "0:0"}
        for f in (FIXTURES / "good.yaml", FIXTURES / "good-forwarder.yaml", COMPOSE):
            with self.subTest(file=f.name):
                if not f.is_file():
                    self.fail("%s missing" % f)
                r = compose_config(f, env)
                self.assertEqual(r.returncode, 0, r.stderr)
                bad, _ = check_config(json.loads(r.stdout), {**env, "KEY_VALUE": KEY})
                self.assertIn("non_root", bad)

    def test_every_rule_has_a_planted_fixture(self):
        self.assertEqual(sorted(set(PLANTED.values())), sorted(RULES))
        on_disk = sorted(p.stem for p in FIXTURES.glob("bad-*.yaml"))
        self.assertEqual(on_disk, sorted(PLANTED))

    def test_planted_violations_fail_for_exactly_their_rule(self):
        for stem, rule in sorted(PLANTED.items()):
            with self.subTest(fixture=stem):
                bad, _ = self.check(FIXTURES / (stem + ".yaml"))
                self.assertEqual(sorted(bad), [rule], "%s should break only %s: %s" % (stem, rule, bad))

    # The real file.
    def real(self):
        if not COMPOSE.is_file():
            self.fail("src/deploy/compose.yaml does not exist yet")
        return self.check(COMPOSE)

    def test_real_file_satisfies_every_rule(self):
        bad, examined = self.real()
        self.assertEqual(bad, {}, "violations: %s" % bad)

    def test_real_file_examines_the_expected_services(self):
        cfg = self.checker_input(COMPOSE) if COMPOSE.is_file() else self.fail("no compose.yaml")
        by_name = roles_of(cfg, self.env)
        roles = list(by_name.values())
        for r in ("gateway", "engine", "page", "cli"):
            self.assertEqual(roles.count(r), 1, "expected exactly one %s service, got %s" % (r, by_name))
        self.assertLessEqual(roles.count("client"), 1)
        self.assertLessEqual(roles.count("forwarder"), 1, "at most one publishing forwarder: %s" % by_name)
        self.assertEqual(roles.count("other"), 0, "service with an unknown image: %s" % {
            n: cfg["services"][n].get("image") for n, r in by_name.items() if r == "other"})

    def test_real_file_images_are_exactly_the_variables_by_role(self):
        """Each non-gateway service's image is one AP_IMAGE_* variable's value, each variable is used,
        and nothing is a tag (the launcher verified these digests; ADR 0028)."""
        cfg = self.checker_input(COMPOSE) if COMPOSE.is_file() else self.fail("no compose.yaml")
        by_name = roles_of(cfg, self.env)
        refs = [s.get("image") for n, s in cfg["services"].items() if by_name[n] not in ("gateway", "forwarder")]
        self.assertEqual(sorted(set(refs)), sorted(IMAGE_ENV.values()), refs)
        for ref in refs:
            self.assertRegex(ref, r"@sha256:[0-9a-f]{64}$")
        gw = [s for n, s in cfg["services"].items() if by_name[n] == "gateway"]
        self.assertEqual([s["image"] for s in gw], [gateway_pin()])
        self.assertRegex(gateway_pin(), r"@sha256:[0-9a-f]{64}$", "the gateway pin in images.lock needs a digest")

    def test_real_file_fails_when_any_variable_is_unset(self):
        if not COMPOSE.is_file():
            self.fail("src/deploy/compose.yaml does not exist yet")
        self.assertEqual(compose_config(COMPOSE, self.env).returncode, 0)
        for var in VARS:
            with self.subTest(unset=var):
                env = {k: v for k, v in self.env.items() if k != var}
                r = compose_config(COMPOSE, env)
                self.assertNotEqual(r.returncode, 0, "config succeeded without %s" % var)
                self.assertIn(var, r.stderr, "the error should name %s" % var)


class NetworkIsolationTest(unittest.TestCase):
    """T022a, with real Docker: a container on an internal network made with the option the compose
    file uses (gateway_mode_ipv4=isolated) cannot reach a service the host listens on at 0.0.0.0, by
    the network's gateway address, docker0's or the host's LAN address; a control on a plain internal
    network does reach it, so the check can fail. The option is read from the resolved compose file."""

    @classmethod
    def setUpClass(cls):
        require_docker()
        cls.tag = "ap-test-iso-" + os.urandom(4).hex()
        cls.addClassCleanup(cls.cleanup)
        cls.nets = []
        cls.procs = []
        base = tempfile.mkdtemp(prefix="test-iso-", dir=TMP_BASE)
        cls.addClassCleanup(shutil.rmtree, base, ignore_errors=True)
        with socket.socket() as sk:
            sk.bind(("0.0.0.0", 0))
            cls.port = sk.getsockname()[1]
        (Path(base) / "index.html").write_text("host service\n")
        cls.server = subprocess.Popen([sys.executable, "-m", "http.server", str(cls.port), "--bind", "0.0.0.0"],
                                      cwd=base, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        cls.addClassCleanup(cls.stop_server)
        # the image: the golang pin (busybox wget), present because the check image is built from it
        cls.image = None
        for line in (REPO / "src/deploy/images.lock").read_text().splitlines():
            f = line.split()
            if len(f) >= 2 and f[0] == "golang":
                cls.image = f[1]
        if not cls.image or subprocess.run(["docker", "image", "inspect", cls.image], capture_output=True).returncode != 0:
            raise AssertionError("the golang pin from images.lock must be in the local image store (build.sh check pulls it)")
        # driver options of every internal network of the compose file's resolved configuration
        key = Path(base) / "k"
        key.write_text(KEY)
        key.chmod(0o600)
        env = {"AP_CREDENTIALS_FILE": str(key), "AP_PROJECTS_DIR": base, "AP_PAGE_PORT": "18081", **TOKENS,
               "AP_GATEWAY_IMAGE": gateway_pin(), "AP_HOST_USER": "1234:1234", **IMAGE_ENV}
        r = compose_config(COMPOSE, env)
        if r.returncode != 0:
            raise AssertionError(r.stderr)
        internal = [n for n in json.loads(r.stdout)["networks"].values() if n and n.get("internal")]
        if not internal:
            raise AssertionError("no internal network in %s" % COMPOSE)
        cls.opts = internal[0].get("driver_opts") or {}
        cls.isolated = cls.make("isolated", cls.opts)
        cls.plain = cls.make("plain", {})
        gws = {}
        for n in ("isolated", "plain"):
            gws[n] = json.loads(subprocess.run(["docker", "network", "inspect", cls.tag + "-" + n], capture_output=True,
                                               text=True).stdout)[0]["IPAM"]["Config"][0].get("Gateway")
        cls.gateways = gws

    @classmethod
    def make(cls, suffix, opts):
        name = cls.tag + "-" + suffix
        args = ["docker", "network", "create", "--internal"]
        for k, v in opts.items():
            args += ["-o", "%s=%s" % (k, v)]
        r = subprocess.run(args + [name], capture_output=True, text=True)
        if r.returncode != 0:
            raise AssertionError(r.stderr)
        cls.nets.append(name)
        return name

    @classmethod
    def stop_server(cls):
        cls.server.terminate()
        cls.server.wait(timeout=20)

    @classmethod
    def cleanup(cls):
        for n in cls.nets:
            subprocess.run(["docker", "network", "rm", n], capture_output=True)
        left = subprocess.run(["docker", "network", "ls", "--format", "{{.Name}}", "--filter", "name=" + cls.tag],
                              capture_output=True, text=True).stdout.split()
        if left:
            raise AssertionError("networks left behind: %s" % left)

    def host_addresses(self):
        out = set()
        r = subprocess.run(["ip", "-4", "-o", "addr", "show"], capture_output=True, text=True)
        for line in r.stdout.splitlines():
            f = line.split()
            addr = f[3].split("/")[0]
            if f[1] != "lo" and not f[1].startswith(("br-", "veth")):
                out.add(addr)
        return out

    def reaches(self, net, addr):
        r = subprocess.run(["docker", "run", "--rm", "--network", net, "--read-only", "--cap-drop", "ALL",
                            "--user", "65532:65532", "--entrypoint", "wget", self.image, "-q", "-T", "4", "-O", "/dev/null",
                            "http://%s:%d/" % (addr, self.port)], capture_output=True, text=True, timeout=60)
        return r.returncode == 0

    def test_the_compose_file_gives_its_internal_networks_the_isolating_option(self):
        self.assertEqual(self.opts.get(ISOLATED), "isolated", self.opts)

    def test_a_container_on_an_isolated_internal_network_reaches_no_host_service(self):
        targets = {a for a in (self.gateways["isolated"],) if a} | self.host_addresses() | {"172.17.0.1"}
        self.assertTrue(self.host_addresses(), "no host address found to test against")
        for addr in sorted(targets):
            with self.subTest(address=addr):
                self.assertFalse(self.reaches(self.isolated, addr), "%s is reachable from the isolated network" % addr)

    def test_the_control_on_a_plain_internal_network_does_reach_the_host_service(self):
        """Without this the test above could never fail: the plain network's gateway is the host."""
        self.assertTrue(self.gateways["plain"])
        self.assertTrue(self.reaches(self.plain, self.gateways["plain"]),
                        "the control cannot reach the listener, so the isolation test proves nothing")


class MissingDockerFailsTest(unittest.TestCase):
    """T010c: without Docker the Compose tests fail (not skip), proven by running one with no docker on PATH."""

    def test_compose_tests_fail_without_docker_on_path(self):
        empty = tempfile.mkdtemp(prefix="test-compose-nopath-", dir=TMP_BASE)
        self.addCleanup(shutil.rmtree, empty, ignore_errors=True)
        r = subprocess.run([sys.executable, "-m", "unittest", "-v", "test_compose.ComposeTest.test_clean_fixtures_pass"],
                           cwd=Path(__file__).resolve().parent, env={"PATH": empty, "HOME": empty},
                           capture_output=True, text=True, timeout=120)
        out = r.stdout + r.stderr
        self.assertNotEqual(r.returncode, 0, out)
        self.assertNotIn("skipped", out)
        self.assertIn("docker is required", out)

    def test_the_module_has_no_docker_skip(self):
        text = Path(__file__).read_text()
        self.assertNotIn("skipUnless" + "(shutil", text)
        self.assertNotIn("self." + "skipTest", text)


if __name__ == "__main__":
    unittest.main()
