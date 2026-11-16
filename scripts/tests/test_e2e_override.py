"""T024 unit (2): the merged configuration of src/deploy/compose.yaml with the test-only override
src/tests/e2e/compose.recorded.yaml (research D44, ADR 0047, tasks.md T024 design notes), judged on
`docker compose config --format json` of the two files, never on the override's text.

The override's inputs, which scripts/e2e.sh sets (the seam these tests fix):
  AP_REPLAY_IMAGE  the `wiremock` pin from src/deploy/images.lock (its one home)
  AP_REPLAY_DIR    the folder `replay.sh prepare <set> <out>` wrote: mappings/, __files/ and
                   wiremock.env (WIREMOCK_OPTIONS, with the admin API's basic auth)
and every variable `ap up` sets for compose.yaml.

Rules judged by `violations()`: the gateway service is the pinned replay image; it is on exactly
the gateway's networks of compose.yaml other than `egress`, at the same fixed addresses, and every
network it joins is internal; it has no secret, no nginx template or entrypoint mount, every mount
read-only and the prepared folder at /home/wiremock; it is read-only, non-root, drops every
capability and has no-new-privileges; its environment holds no nginx variable and no provider key,
and its WIREMOCK_OPTIONS put the admin API behind the basic auth of the env file, with no banner and
a journal limit; the engine's store and the project worker's history are on volumes other than the
developer's. `violations()` passes on a reference override written below and on the real one, and
fails, naming the rule, on each planted violation: a third Compose file breaking one rule, and
copies of the override whose `!override`/`!reset` tags became a plain merge.

A missing docker compose is a failure, never a skip (as in test_compose.py).
"""
import json
import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from test_compose import IMAGE_ENV, KEY, TOKENS, gateway_pin, require_docker

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
COMPOSE = REPO / "src/deploy/compose.yaml"
# AP_TEST_E2E_OVERRIDE points the tests at another override (to see them fail against a mutated copy)
OVERRIDE = Path(os.environ.get("AP_TEST_E2E_OVERRIDE") or REPO / "src/tests/e2e/compose.recorded.yaml")
# AP_TEST_REPLAY points the tests at another copy of replay.sh (as in test_replay_prepare.py)
REPLAY = Path(os.environ.get("AP_TEST_REPLAY") or REPO / "src/tests/recorded/replay.sh")
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
PASSWORD = "0123456789abcdef0123456789abcdef"  # the reference's env file; the real one is random


def wiremock_pin():
    for line in (REPO / "src/deploy/images.lock").read_text().splitlines():
        f = line.split()
        if len(f) >= 2 and f[0] == "wiremock":
            return f[1]
    raise AssertionError("no wiremock pin in images.lock")


# A minimal correct override, proving violations() can pass; written from the rules above.
REFERENCE = """\
services:
  gateway:
    image: ${AP_REPLAY_IMAGE:?set AP_REPLAY_IMAGE}
    user: "65532:65532"
    environment: !override {}
    env_file:
      - { path: "${AP_REPLAY_DIR:?set AP_REPLAY_DIR}/wiremock.env", required: true }
    secrets: !reset []
    volumes: !override
      - { type: bind, source: "${AP_REPLAY_DIR:?set AP_REPLAY_DIR}", target: /home/wiremock, read_only: true }
    tmpfs: !override
      - /tmp:uid=65532,gid=65532,size=64m
    entrypoint: !reset null
    command: ["--port", "8080"]
    networks: !override
      agents: { ipv4_address: 10.231.1.3 }
      analyst: { ipv4_address: 10.231.7.3 }
  engine:
    volumes:
      - { type: volume, source: e2e-engine-store, target: /var/lib/engine }
  project-worker:
    volumes:
      - { type: volume, source: e2e-project-worker-state, target: /var/lib/project-worker }
volumes:
  e2e-engine-store: {}
  e2e-project-worker-state: {}
"""

# Planted violations: a third Compose file after the override, each breaking one rule.
PLANTED = {
    "image": "services:\n  gateway:\n    image: ${AP_GATEWAY_IMAGE}\n",
    "egress": "services:\n  gateway:\n    networks:\n      egress: {}\n",
    "addresses": "services:\n  gateway:\n    networks:\n      agents: { ipv4_address: 10.231.1.9 }\n",
    "networks": "services:\n  gateway:\n    networks: !override\n      agents: { ipv4_address: 10.231.1.3 }\n",
    "secret": "services:\n  gateway:\n    secrets: [anthropic_api_key]\n",
    "nginx_mount": ("services:\n  gateway:\n    volumes:\n      - { type: bind, source: ./gateway/default.conf.template,"
                    " target: /etc/nginx/templates/default.conf.template, read_only: true }\n"),
    "writable_mount": ("services:\n  gateway:\n    volumes: !override\n      - { type: bind, source: \"${AP_REPLAY_DIR}\","
                       " target: /home/wiremock, read_only: false }\n"),
    "replay_dir": "services:\n  gateway:\n    volumes: !reset []\n",
    "read_only": "services:\n  gateway:\n    read_only: false\n",
    "non_root": "services:\n  gateway:\n    user: \"0:0\"\n",
    "cap_drop": "services:\n  gateway:\n    cap_drop: !reset []\n",
    "no_new_privileges": "services:\n  gateway:\n    security_opt: !reset []\n",
    "nginx_env": "services:\n  gateway:\n    environment:\n      NGINX_ENVSUBST_FILTER: x\n",
    "admin_auth": ("services:\n  gateway:\n    env_file: !reset []\n    environment:\n"
                   "      WIREMOCK_OPTIONS: --disable-banner --max-request-journal-entries 100\n"),
    "banner": ("services:\n  gateway:\n    env_file: !reset []\n    environment:\n"
               "      WIREMOCK_OPTIONS: --admin-api-basic-auth replay:%s --max-request-journal-entries 100\n" % PASSWORD),
    "journal": ("services:\n  gateway:\n    env_file: !reset []\n    environment:\n"
                "      WIREMOCK_OPTIONS: --admin-api-basic-auth replay:%s --disable-banner\n" % PASSWORD),
    "engine_volume": "services:\n  engine:\n    volumes:\n      - { type: volume, source: engine-store, target: /var/lib/engine }\n",
    "project_volume": ("services:\n  project-worker:\n    volumes:\n"
                       "      - { type: volume, source: project-worker-state, target: /var/lib/project-worker }\n"),
}

# Copies of an override whose replacing tags became a plain merge (what is left of compose.yaml)
UNTAGGED = {
    "networks: !override": ("networks:", "egress"),
    "secrets: !reset []": ("secrets: []", "secret"),
    "volumes: !override\n      - { type: bind, source: \"${AP_REPLAY_DIR": ("volumes:\n      - { type: bind, source: \"${AP_REPLAY_DIR",
                                                                        "nginx_mount"),
    "environment: !override {}": ("environment: {}", "nginx_env"),
}


def config(files, env):
    e = {k: v for k, v in os.environ.items() if not k.startswith("AP_")}
    e.update(env)
    args = ["docker", "compose"]
    for f in files:
        args += ["-f", str(f)]
    return subprocess.run(args + ["--profile", "*", "config", "--format", "json"], env=e,
                          capture_output=True, text=True, timeout=120)


def uid_of(user):
    u = str(user or "").split(":")[0]
    return u


def options_of(svc):
    env = svc.get("environment") or {}
    if isinstance(env, list):
        env = dict(x.split("=", 1) for x in env)
    return env.get("WIREMOCK_OPTIONS") or ""


def volume_name(cfg, svc_name, target):
    """The resolved Docker volume name mounted at target in the service (None if no volume there)."""
    for v in cfg["services"][svc_name].get("volumes") or []:
        if v.get("target") == target and v.get("type") == "volume":
            top = (cfg.get("volumes") or {}).get(v.get("source")) or {}
            return top.get("name") or v.get("source")
    return None


def violations(cfg, base, replay_dir, password):
    """The rules broken by the merged configuration cfg, given base = the config of compose.yaml alone."""
    bad = set()
    g = cfg["services"]["gateway"]
    bg = base["services"]["gateway"]
    if g.get("image") != wiremock_pin():
        bad.add("image")
    nets = g.get("networks") or {}
    want = {n: (v or {}).get("ipv4_address") for n, v in (bg.get("networks") or {}).items() if n != "egress"}
    have = {n: (v or {}).get("ipv4_address") for n, v in nets.items()}
    if "egress" in have:
        bad.add("egress")
    for n in have:
        if not (cfg.get("networks") or {}).get(n, {}).get("internal"):
            bad.add("egress")
    if {n: a for n, a in have.items() if n != "egress"} != want:
        if set(have) - {"egress"} == set(want):
            bad.add("addresses")
        else:
            bad.add("networks")
    if g.get("secrets"):
        bad.add("secret")
    vols = g.get("volumes") or []
    gateway_dir = str(REPO / "src/deploy/gateway")
    for v in vols:
        t, s = str(v.get("target", "")), str(v.get("source", ""))
        if t.startswith(("/etc/nginx", "/docker-entrypoint.d")) or s.startswith(gateway_dir):
            bad.add("nginx_mount")
        if not v.get("read_only"):
            bad.add("writable_mount")
    if not any(v.get("type") == "bind" and v.get("target") == "/home/wiremock" and
               os.path.realpath(str(v.get("source"))) == os.path.realpath(replay_dir) for v in vols):
        bad.add("replay_dir")
    if g.get("read_only") is not True:
        bad.add("read_only")
    if uid_of(g.get("user")) in ("", "0", "root"):
        bad.add("non_root")
    if "ALL" not in (g.get("cap_drop") or []):
        bad.add("cap_drop")
    if not any(re.fullmatch(r"no-new-privileges(:true)?", s) for s in g.get("security_opt") or []):
        bad.add("no_new_privileges")
    env = g.get("environment") or {}
    if any(k.startswith("NGINX_") or k == "GATEWAY_SERVER_NAME" or "API_KEY" in k for k in env):
        bad.add("nginx_env")
    opts = options_of(g)
    words = opts.split() + [str(x) for x in (g.get("command") or [])]
    m = re.search(r"(?:^| )--admin-api-basic-auth [^ :]+:([^ ]+)(?: |$)", opts)
    if not m or m.group(1) != password:
        bad.add("admin_auth")
    if "--disable-banner" not in words:
        bad.add("banner")
    if "--max-request-journal-entries" not in words:
        bad.add("journal")
    for svc, target, rule in (("engine", "/var/lib/engine", "engine_volume"),
                              ("project-worker", "/var/lib/project-worker", "project_volume")):
        name, dev = volume_name(cfg, svc, target), volume_name(base, svc, target)
        if name is None or name == dev:
            bad.add(rule)
    return sorted(bad)


class E2EOverrideTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        require_docker()
        cls.base_dir = Path(tempfile.mkdtemp(prefix="test-e2e-override-", dir=TMP_BASE))
        cls.addClassCleanup(shutil.rmtree, cls.base_dir, ignore_errors=True)
        key = cls.base_dir / "models.env"
        key.write_text(KEY + "\n")
        key.chmod(0o600)
        projects = cls.base_dir / "projects"
        projects.mkdir()
        grants = cls.base_dir / "grants"
        grants.mkdir()
        cls.env = {"AP_CREDENTIALS_FILE": str(key), "AP_PROJECTS_DIR": str(projects), "AP_PAGE_PORT": "18081",
                   **TOKENS, "AP_GATEWAY_IMAGE": gateway_pin(), "AP_HOST_USER": "1234:1234",
                   "AP_GRANTS_DIR": str(grants), **IMAGE_ENV, "AP_REPLAY_IMAGE": wiremock_pin()}
        r = config([COMPOSE], cls.env)
        assert r.returncode == 0, r.stderr
        cls.base = json.loads(r.stdout)
        # the reference's replay folder, with an env file of the shape replay.sh prepare writes
        cls.ref_dir = cls.base_dir / "ref-replay"
        (cls.ref_dir / "mappings").mkdir(parents=True)
        (cls.ref_dir / "wiremock.env").write_text(
            "WIREMOCK_OPTIONS=--admin-api-basic-auth replay:%s --disable-banner --max-request-journal-entries 100\n" % PASSWORD)
        cls.reference = cls.base_dir / "reference.yaml"
        cls.reference.write_text(REFERENCE)

    def merged(self, files, replay_dir):
        r = config([COMPOSE, *files], {**self.env, "AP_REPLAY_DIR": str(replay_dir)})
        self.assertEqual(r.returncode, 0, "docker compose config failed for %s:\n%s" % (files, r.stderr))
        return json.loads(r.stdout)

    def planted_file(self, rule):
        p = self.base_dir / ("planted-%s.yaml" % rule)
        p.write_text(PLANTED[rule])
        return p

    def real_replay_dir(self):
        """The folder the real replay.sh prepare writes, and its admin password."""
        out = Path(tempfile.mkdtemp(prefix="replay-", dir=self.base_dir)) / "out"
        r = subprocess.run(["sh", str(REPLAY), "prepare", "happy", str(out)], capture_output=True,
                           text=True, timeout=60)
        self.assertEqual(r.returncode, 0, "replay.sh prepare happy failed: " + r.stderr)
        text = (out / "wiremock.env").read_text()
        m = re.search(r"--admin-api-basic-auth [^ :]+:([^ \"'\n]+)", text)
        assert m is not None, text
        return out, m.group(1)

    def real_override(self):
        self.assertTrue(OVERRIDE.is_file(), "%s does not exist yet (T024)" % OVERRIDE)
        return OVERRIDE

    # the judge can pass and fail
    def test_the_judge_passes_on_the_reference_override(self):
        self.assertEqual(violations(self.merged([self.reference], self.ref_dir), self.base, self.ref_dir, PASSWORD), [])

    def test_the_judge_fails_compose_yaml_alone_on_the_gateway_rules(self):
        got = violations(self.base, self.base, self.ref_dir, PASSWORD)
        for rule in ("image", "egress", "secret", "nginx_mount", "replay_dir", "nginx_env", "admin_auth",
                     "engine_volume", "project_volume"):
            self.assertIn(rule, got)

    def test_each_planted_violation_of_the_reference_fails_its_rule(self):
        for rule in PLANTED:
            with self.subTest(rule=rule):
                cfg = self.merged([self.reference, self.planted_file(rule)], self.ref_dir)
                got = violations(cfg, self.base, self.ref_dir, PASSWORD)
                self.assertIn(rule, got)

    def test_a_plain_merge_instead_of_override_or_reset_fails(self):
        for tagged, (plain, rule) in UNTAGGED.items():
            with self.subTest(tag=tagged.splitlines()[0]):
                self.assertIn(tagged, REFERENCE)
                p = self.base_dir / "untagged.yaml"
                p.write_text(REFERENCE.replace(tagged, plain))
                got = violations(self.merged([p], self.ref_dir), self.base, self.ref_dir, PASSWORD)
                self.assertIn(rule, got)

    # the real override
    def test_the_real_override_obeys_every_rule(self):
        out, pw = self.real_replay_dir()
        cfg = self.merged([self.real_override()], out)
        self.assertEqual(violations(cfg, self.base, out, pw), [])

    def test_the_real_override_does_not_change_the_other_services_images_or_networks(self):
        out, _ = self.real_replay_dir()
        cfg = self.merged([self.real_override()], out)
        for name, svc in self.base["services"].items():
            if name == "gateway":
                continue
            with self.subTest(service=name):
                self.assertIn(name, cfg["services"])
                self.assertEqual(cfg["services"][name].get("image"), svc.get("image"))
                self.assertEqual(cfg["services"][name].get("networks"), svc.get("networks"))
        # Compose drops a network no service joins, so egress may be gone; the others are unchanged
        drop = lambda nets: {k: v for k, v in (nets or {}).items() if k != "egress"}
        self.assertEqual(drop(cfg.get("networks")), drop(self.base.get("networks")))

    def test_the_real_override_reads_the_pin_from_images_lock_not_from_its_text(self):
        text = self.real_override().read_text()
        digest = wiremock_pin().split("@", 1)[1]
        self.assertNotIn(digest, text, "the wiremock pin has one home, images.lock")

    def test_each_planted_violation_of_the_real_override_fails_its_rule(self):
        out, pw = self.real_replay_dir()
        for rule in PLANTED:
            if rule in ("banner", "journal"):
                continue  # the real override may give these flags as the command, which a plant of the options cannot remove
            with self.subTest(rule=rule):
                cfg = self.merged([self.real_override(), self.planted_file(rule)], out)
                self.assertIn(rule, violations(cfg, self.base, out, pw))

    def test_the_real_override_stripped_of_its_replacing_tags_fails(self):
        text = self.real_override().read_text()
        self.assertRegex(text, r"!override|!reset", "the override replaces, never plain-merges (T024 design notes)")
        plain = re.sub(r"!reset\s*(\[\]|null|\{\})?", "", re.sub(r"!override\s*", "", text))
        p = self.base_dir / "real-untagged.yaml"
        p.write_text(plain)
        out, pw = self.real_replay_dir()
        r = config([COMPOSE, p], {**self.env, "AP_REPLAY_DIR": str(out)})
        if r.returncode != 0:
            return  # a plain merge Compose cannot even read is refused too
        self.assertNotEqual(violations(json.loads(r.stdout), self.base, out, pw), [])


if __name__ == "__main__":
    unittest.main()
