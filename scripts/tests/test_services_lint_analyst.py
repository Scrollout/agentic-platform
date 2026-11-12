"""Tests for the `contract-lint` and `analyst` services (T038e, T038f; research D27, D28).

Judged on the resolved configuration of the real src/deploy/compose.yaml (`docker compose config`)
and on what `ap up` passes to `docker` (a recording stub), like test_compose.py and test_launcher.py.

Names the implementer follows:
  contract-lint  image variable AP_IMAGE_LINT (build.sh lint, Dockerfile.lint), token AP_LINT_TOKEN
  analyst        the docker-agent pin of images.lock (an upstream image, as nginx is for the
                 gateway, so no AP_IMAGE_* variable), token AP_ANALYST_TOKEN
  engine         environment AP_LINT_URL=http://contract-lint:<port>, AP_LINT_TOKEN,
                 AP_ANALYST_URL=http://analyst:8080, AP_ANALYST_TOKEN
  contract-lint  receives its token as LINT_TOKEN (the value of AP_LINT_TOKEN), the analyst as AP_ANALYST_TOKEN (its
                 `--auth-token` is "$AP_ANALYST_TOKEN" expanded by a shell, so the value is in its
                 environment and not on its command line)
  networks       `lint` (engine, contract-lint) and `analyst` (engine, gateway, analyst), both internal
                 and isolated
"""
import json
import os
import re
import unittest

import test_compose as tc
import test_launcher as tl

NEW_TOKENS = {"AP_CLIENT_TOKEN": tc.TOKENS["AP_CLIENT_TOKEN"], "AP_PAGE_TOKEN": tc.TOKENS["AP_PAGE_TOKEN"],
              "AP_WORKER_TOKEN": tc.TOKENS["AP_WORKER_TOKEN"],
              "AP_LINT_TOKEN": "11a7e0" + "d" * 26, "AP_ANALYST_TOKEN": "a0a1y5" + "e" * 26}
LINT_IMAGE = "%s/ap-lint@sha256:%064x" % (tc.REGISTRY, 7)
LINT, ANALYST, ENGINE, GATEWAY = "contract-lint", "analyst", "engine", "gateway"


def docker_agent_pin():
    for line in (tc.REPO / "src/deploy/images.lock").read_text().splitlines():
        f = line.split()
        if len(f) >= 2 and f[0] == "docker-agent":
            return f[1]
    raise AssertionError("no docker-agent pin in images.lock")


class Base(tc.ComposeTest):
    """Reuses ComposeTest's environment (setUpClass); only this class's own tests are run."""

    @classmethod
    def setUpClass(cls):
        super().setUpClass()
        cls.env = {**cls.env, **NEW_TOKENS, "AP_IMAGE_LINT": LINT_IMAGE}

    def cfg(self):
        return self.checker_input(tc.COMPOSE)

    def svc(self, name):
        services = self.cfg()["services"]
        self.assertIn(name, services, "no %s service in compose.yaml" % name)
        return services[name]

    def members(self, net):
        return {n for n, s in self.cfg()["services"].items() if net in tc._networks(s)}


for _n in [n for n in dir(tc.ComposeTest) if n.startswith("test_")]:
    setattr(Base, _n, None)  # do not rerun the inherited tests here


def only_nets(svc):
    return [n for n in tc._networks(svc)]


class ServicesInComposeTest(Base):
    def test_both_services_are_hardened_and_under_gvisor(self):
        for name in (LINT, ANALYST):
            s = self.svc(name)
            with self.subTest(service=name):
                self.assertEqual(s.get("runtime"), "runsc")
                self.assertIs(s.get("read_only"), True)
                self.assertIn("ALL", [str(c).upper() for c in s.get("cap_drop") or []])
                self.assertTrue(any("no-new-privileges" in str(o) for o in s.get("security_opt") or []))
                self.assertEqual(str(s.get("user")), "65532:65532")
                self.assertFalse(s.get("privileged"))
                self.assertEqual(s.get("restart"), "no")
                self.assertFalse(s.get("ports"))
                for k, n in (("cpus", "cpus"), ("mem_limit", "memory"), ("pids_limit", "pids")):
                    self.assertTrue(tc._limit(s, k, n), "no %s limit" % n)
                self.assertRegex(tc.image_ref(s), r"@sha256:[0-9a-f]{64}$")

    def test_images(self):
        self.assertEqual(self.svc(LINT)["image"], LINT_IMAGE)
        self.assertEqual(self.svc(LINT).get("pull_policy"), "never")
        self.assertEqual(self.svc(ANALYST)["image"], docker_agent_pin())

    def test_services_are_always_started_not_on_demand(self):
        for name in (LINT, ANALYST):
            self.assertFalse(self.svc(name).get("profiles"), "%s must start with `ap up`" % name)

    def test_contract_lint_is_on_an_internal_isolated_network_with_the_engine_only(self):
        nets = self.cfg()["networks"]
        own = only_nets(self.svc(LINT))
        self.assertEqual(len(own), 1, own)
        self.assertNotEqual(own[0], "default")
        self.assertEqual(tc.net_kind(nets[own[0]]), "internal")
        self.assertEqual(nets[own[0]]["driver_opts"].get(tc.ISOLATED), "isolated")
        self.assertEqual(self.members(own[0]), {LINT, ENGINE})

    def test_analyst_is_on_its_own_internal_isolated_network_with_engine_and_gateway_only(self):
        nets = self.cfg()["networks"]
        own = only_nets(self.svc(ANALYST))
        self.assertEqual(len(own), 1, own)
        self.assertEqual(tc.net_kind(nets[own[0]]), "internal")
        self.assertEqual(nets[own[0]]["driver_opts"].get(tc.ISOLATED), "isolated")
        self.assertEqual(self.members(own[0]), {ANALYST, ENGINE, GATEWAY})
        for other in ("agents", "checks", "clients", "page", "page-host", "egress", "default"):
            self.assertNotIn(other, only_nets(self.svc(ANALYST)))

    def test_the_two_services_share_no_network(self):
        self.assertFalse(set(only_nets(self.svc(LINT))) & set(only_nets(self.svc(ANALYST))))

    def test_the_analyst_has_no_route_out_but_the_gateway(self):
        """No egress network, no network_mode, no published port; it names the gateway as its model
        endpoint (a fixed address in extra_hosts, as workers do: gVisor has no Docker DNS)."""
        s = self.svc(ANALYST)
        nets = self.cfg()["networks"]
        self.assertTrue(all(tc.net_kind(nets[n]) == "internal" for n in only_nets(s)))
        self.assertIn(s.get("network_mode"), (None, "none"))
        env = s.get("environment") or {}
        self.assertRegex(env.get("MODEL_GATEWAY_URL", ""), r"^http://gateway:8080/?$")
        eh = s.get("extra_hosts") or {}
        names = list(eh) if isinstance(eh, dict) else [str(e).split("=")[0].split(":")[0] for e in eh]
        self.assertIn("gateway", names)  # Compose prints a list such as ["gateway=10.231.7.3"]
        self.assertNotIn(self.env["AP_CREDENTIALS_FILE"], tc._env_values(s))
        self.assertFalse(any(tc.KEY in v for v in tc._env_values(s)))

    def test_the_analyst_mounts_the_agent_definitions_read_only_and_nothing_else_of_the_host(self):
        s = self.svc(ANALYST)
        binds = [v for v in s.get("volumes") or [] if v.get("type", "bind") == "bind"]
        self.assertEqual([(os.path.realpath(b["source"]), b["target"], b.get("read_only")) for b in binds],
                         [(os.path.realpath(str(tc.REPO / "src/agents")), "/agents", True)])
        self.assertFalse(tc._sources(self.cfg(), s)[1:], "only src/agents may be mounted")

    def test_the_analyst_runs_serve_api_with_the_token_from_its_environment(self):
        s = self.svc(ANALYST)
        tok = NEW_TOKENS["AP_ANALYST_TOKEN"]
        argv = [str(x) for x in (s.get("entrypoint") or [])] + [str(x) for x in (s.get("command") or [])]
        line = " ".join(argv)
        self.assertIn("serve api /agents/analyst.yaml", line)
        self.assertIn("--auth-token", line)
        self.assertIn("--listen 0.0.0.0:8080", line)
        self.assertNotIn(tok, line, "the token must not be on the command line (docker inspect shows it)")
        # Compose prints a literal $ as $$
        self.assertRegex(line, r'--auth-token "?\$\$?\{?AP_ANALYST_TOKEN\}?"?', "the token comes from a shell variable")
        self.assertIn(tok, tc._env_values(s))
        # the session database lives on a tmpfs; nothing else is writable
        self.assertTrue(any(str(t).split(":")[0] == "/tmp" for t in s.get("tmpfs") or []))
        self.assertIn("/tmp/", line)

    def test_the_contract_lint_token_is_in_its_environment(self):
        # server.mjs reads LINT_TOKEN; the value is the AP_LINT_TOKEN token
        self.assertEqual((self.svc(LINT).get("environment") or {}).get("LINT_TOKEN"), NEW_TOKENS["AP_LINT_TOKEN"])

    def test_each_new_token_is_given_to_the_engine_and_its_own_service_only(self):
        services = self.cfg()["services"]
        for var, owner in (("AP_LINT_TOKEN", LINT), ("AP_ANALYST_TOKEN", ANALYST)):
            val = NEW_TOKENS[var]
            holders = sorted(n for n, s in services.items() if val in json.dumps(s))
            self.assertEqual(holders, sorted([ENGINE, owner]), "%s reaches %s" % (var, holders))
        # and the three older tokens reach neither new service
        for name in (LINT, ANALYST):
            blob = json.dumps(self.svc(name))
            for var in ("AP_CLIENT_TOKEN", "AP_PAGE_TOKEN", "AP_WORKER_TOKEN"):
                self.assertNotIn(NEW_TOKENS[var], blob, "%s holds %s" % (name, var))
        self.assertEqual(len({NEW_TOKENS[v] for v in tc.TOKENS}), len(tc.TOKENS))

    def test_the_engine_gets_both_urls_and_tokens(self):
        env = self.svc(ENGINE).get("environment") or {}
        self.assertEqual(env.get("AP_LINT_TOKEN"), NEW_TOKENS["AP_LINT_TOKEN"])
        self.assertEqual(env.get("AP_ANALYST_TOKEN"), NEW_TOKENS["AP_ANALYST_TOKEN"])
        self.assertRegex(env.get("AP_LINT_URL", ""), r"^http://[^/:]+(:[0-9]+)?/?$")
        self.assertRegex(env.get("AP_ANALYST_URL", ""), r"^http://[^/:]+:8080/?$")
        self.assertNotEqual(env["AP_LINT_URL"], env["AP_ANALYST_URL"])

    def test_the_engines_urls_name_the_services_on_a_network_they_share(self):
        """The engine runs under runc, so Docker's DNS resolves the service names."""
        env = self.svc(ENGINE).get("environment") or {}
        for var, svc in (("AP_LINT_URL", LINT), ("AP_ANALYST_URL", ANALYST)):
            self.assertEqual(re.match(r"^http://([^/:]+)", env[var]).group(1), svc)
            self.assertTrue(set(only_nets(self.svc(ENGINE))) & set(only_nets(self.svc(svc))),
                            "the engine shares no network with %s" % svc)

    def test_the_gateway_reaches_the_analyst_network_and_nothing_else_changes(self):
        gw = self.svc(GATEWAY)
        self.assertEqual(set(only_nets(gw)) - set(only_nets(self.svc(ANALYST))), {"agents", "egress"})
        self.assertNotIn(NEW_TOKENS["AP_ANALYST_TOKEN"], json.dumps(gw))


class LauncherTokensTest(tl.RealLauncherTest):
    """`ap up` generates the two new tokens like the others (against the recording docker stub)."""

    def write_images(self, names=tl.IMAGE_NAMES):
        super().write_images(names)
        with open(self.images_env, "a") as f:
            f.write("AP_IMAGE_LINT=%s\n" % LINT_IMAGE)


for _n in [n for n in dir(tl.RealLauncherTest) if n.startswith("test_")]:
    setattr(LauncherTokensTest, _n, None)


def _up_env(t):
    r = t.up()
    t.assertEqual(r.returncode, 0, r.stderr)
    ups = t.up_calls()
    t.assertEqual(len(ups), 1)
    return ups[0]["env"]


def _t_distinct_hex(self):
    env = _up_env(self)
    toks = [env.get(v, "") for v in ("AP_CLIENT_TOKEN", "AP_PAGE_TOKEN", "AP_WORKER_TOKEN",
                                       "AP_LINT_TOKEN", "AP_ANALYST_TOKEN")]
    for t in toks:
        self.assertRegex(t, r"^[0-9a-f]{32,}$")
    self.assertEqual(len(set(toks)), 5, "five pairwise distinct tokens")


def _t_reused(self):
    _up_env(self)
    self.up()
    ups = self.up_calls()
    self.assertEqual(len(ups), 2)
    for v in ("AP_LINT_TOKEN", "AP_ANALYST_TOKEN"):
        self.assertEqual(ups[0]["env"][v], ups[1]["env"][v], v)


def _t_new_after_down(self):
    _up_env(self)
    self.ap("down")
    self.up()
    ups = self.up_calls()
    for v in ("AP_LINT_TOKEN", "AP_ANALYST_TOKEN"):
        self.assertNotEqual(ups[0]["env"][v], ups[1]["env"][v], v)


def _t_no_leak(self):
    """The new tokens are in compose's environment and the private session file only; never in an argv."""
    env = _up_env(self)
    for c in self.calls():
        for v in ("AP_LINT_TOKEN", "AP_ANALYST_TOKEN"):
            self.assertFalse(any(env[v] in a for a in c["argv"]), (v, c["argv"]))
    self.assertEqual(self.session.stat().st_mode & 0o777, 0o600)
    text = self.session.read_text()
    for v in ("AP_LINT_TOKEN", "AP_ANALYST_TOKEN"):
        self.assertIn(v + "=", text)


def _t_lint_image(self):
    """The lint image is verified like the others and passed as AP_IMAGE_LINT."""
    env = _up_env(self)
    self.assertEqual(env.get("AP_IMAGE_LINT"), LINT_IMAGE)
    verified = " ".join(" ".join(c["argv"]) for _, c in self.verify_calls())
    internal = "ap-registry:5000/" + LINT_IMAGE.split("/", 1)[1]  # ADR 0028: verified by its internal name
    self.assertIn(internal, verified)


for _name, _f in (("test_up_generates_five_distinct_tokens", _t_distinct_hex),
                  ("test_the_new_tokens_are_kept_by_a_second_up", _t_reused),
                  ("test_the_new_tokens_change_after_down", _t_new_after_down),
                  ("test_the_new_tokens_are_in_no_argv_and_are_in_the_session_file", _t_no_leak),
                  ("test_the_lint_image_is_verified_and_passed", _t_lint_image)):
    setattr(LauncherTokensTest, _name, _f)


if __name__ == "__main__":
    unittest.main()
