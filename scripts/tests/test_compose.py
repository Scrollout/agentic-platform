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
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent.parent
COMPOSE = REPO / "src/deploy/compose.yaml"
FIXTURES = Path(__file__).resolve().parent / "fixtures/compose"
VARS = ["AP_CREDENTIALS_FILE", "AP_PROJECTS_DIR", "AP_PAGE_PORT",
        "AP_CLIENT_TOKEN", "AP_PAGE_TOKEN", "AP_WORKER_TOKEN", "AP_GATEWAY_IMAGE", "AP_HOST_USER",
        "AP_IMAGE_ENGINE", "AP_IMAGE_PAGE", "AP_IMAGE_CLI", "AP_IMAGE_AGENT", "AP_IMAGE_CHECK",
        "AP_IMAGE_TESTS"]
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
         "token_engine", "token_page", "token_cli", "token_worker", "token_gateway", "image_digest"]
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
}


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
    return image_kind(svc)[1]


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
    roles = {n: role_of(s) for n, s in services.items()}
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
        if role != "gateway":
            if svc.get("network_mode") not in (None, "none") or "egress" in kinds:
                fail("network_egress", name)
            if "no-route" in kinds and role != "page":
                fail("masquerade_page_only", name)
        else:
            if "internal" not in kinds or "egress" not in kinds or svc.get("network_mode"):
                fail("gateway_networks", name)
        # sandbox
        if image_kind(svc)[0] in SANDBOXED:
            if svc.get("runtime") != "runsc":
                fail("runtime", name)
            if not _limit(svc, "cpus", "cpus"):
                fail("limit_cpu", name)
            if not _limit(svc, "mem_limit", "memory"):
                fail("limit_memory", name)
            if not _limit(svc, "pids_limit", "pids"):
                fail("limit_pids", name)
        # images: ours are digest references; the gateway's upstream pin is its own variable
        if role != "gateway" and not re.search(r"@sha256:[0-9a-f]{64}$", image_ref(svc)):
            fail("image_digest", name)
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
        if role == "page":
            if not ports or any(p.get("host_ip") != "127.0.0.1" for p in ports):
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
    return {k: sorted(v) for k, v in bad.items()}, sorted(services)


def compose_config(file, env):
    e = {k: v for k, v in os.environ.items() if not k.startswith("AP_")}
    e.update(env)
    return subprocess.run(["docker", "compose", "-f", str(file), "--profile", "*", "config",
                           "--format", "json"], env=e, capture_output=True, text=True, timeout=120)


@unittest.skipUnless(shutil.which("docker"), "docker not on PATH")
class ComposeTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
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
    def test_clean_fixture_passes(self):
        bad, examined = self.check(FIXTURES / "good.yaml")
        self.assertEqual(bad, {})
        self.assertEqual(examined, ["agent-one", "check-one", "cli", "engine", "gateway", "page", "tests"])

    def test_root_host_user_is_flagged_by_non_root_rule(self):
        env = {**self.env, "AP_HOST_USER": "0:0"}
        for f in (FIXTURES / "good.yaml", COMPOSE):
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
        roles = [role_of(s) for s in cfg["services"].values()]
        for r in ("gateway", "engine", "page", "cli"):
            self.assertEqual(roles.count(r), 1, "expected exactly one %s service, got %s" % (r, roles))
        self.assertLessEqual(roles.count("client"), 1)
        self.assertEqual(roles.count("other"), 0, "service with an unknown image: %s" % {
            n: s.get("image") for n, s in cfg["services"].items() if role_of(s) == "other"})

    def test_real_file_images_are_exactly_the_variables_by_role(self):
        """Each non-gateway service's image is one AP_IMAGE_* variable's value, each variable is used,
        and nothing is a tag (the launcher verified these digests; ADR 0028)."""
        cfg = self.checker_input(COMPOSE) if COMPOSE.is_file() else self.fail("no compose.yaml")
        refs = [s.get("image") for s in cfg["services"].values() if role_of(s) != "gateway"]
        self.assertEqual(sorted(set(refs)), sorted(IMAGE_ENV.values()), refs)
        for ref in refs:
            self.assertRegex(ref, r"@sha256:[0-9a-f]{64}$")
        gw = [s for s in cfg["services"].values() if role_of(s) == "gateway"]
        self.assertEqual([s["image"] for s in gw], [gateway_pin()])

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


if __name__ == "__main__":
    unittest.main()
