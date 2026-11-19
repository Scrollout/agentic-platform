"""T003 (feature 002, research R1, S14): the durable-execution services in src/deploy/compose.yaml
and the launcher's handling of PostgreSQL's password. Test author's file; the code author (T004)
makes it pass without changing it.

Interface fixed here (nothing else names it), which T004 must meet:
  * The password file is `durable-store-password` in a folder `durable/` of the launcher's state
    folder (${XDG_STATE_HOME:-$HOME/.local/state}/agentic-platform/durable/durable-store-password).
    `ap up` makes the folder with mode 700 and the file with mode 644 (the containers must read it)
    and a non-empty random content when it is missing, and keeps the content of an existing one on
    every later `ap up`. `ap` never prints it (standard output, standard error,
    docker argv or environment).
  * `ap` hands the file to Compose as the variable AP_DURABLE_PASSWORD_FILE (like
    AP_CREDENTIALS_FILE), which compose.yaml reads for `durable-store` and `durable`/`durable-schema`.
    This test renders compose.yaml with that variable set to a file of its own.
  * `ap down` removes no volume: no `volume rm`/`volume prune` call and no `down -v/--volumes`
    (R1: the volume `durable-store` holds the data; FR-010 of feature 003).

Compose rules (judged on `docker compose config --format json`, as test_compose.py does; images
are read from src/deploy/images.lock, never written here):
  * services durable-store (postgres), durable-schema (temporal-admin-tools), durable
    (temporal-server) exist with the pinned images;
  * each: read_only, cap_drop [ALL], no-new-privileges, tmpfs /tmp, no published port;
  * durable and durable-store: memory limit 256m; durable-store: user 70:70, tmpfs
    /var/run/postgresql, volume `durable-store` at /var/lib/postgresql/data;
  * networks `durable` (internal; members engine and durable only) and `durable-store` (internal;
    members durable, durable-schema, durable-store only);
  * depends_on: durable-schema -> durable-store service_healthy; durable -> durable-schema
    service_completed_successfully; engine -> durable service_healthy;
  * the dynamic config file mounted into `durable` (a file under src/deploy/durable/) sets
    frontend.enableServerVersionCheck to false (R1: the version check off).
"""
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))  # sibling test modules

import test_ap_compose_override as base  # noqa: E402
import test_compose as tc  # noqa: E402
from test_launcher import IMAGE_NAMES, image_value  # noqa: F401  (used by the borrowed setUp)  # noqa: E402

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
START_STUB = HERE / "fixtures/start/docker-stub"
PASSWORD_FILE = "durable-store-password"
PASSWORD_VAR = "AP_DURABLE_PASSWORD_FILE"
DATA = "/var/lib/postgresql/data"
SERVICES = {"durable-store": "postgres", "durable-schema": "temporal-admin-tools",
            "durable": "temporal-server"}


def pin(name):
    for line in (REPO / "src/deploy/images.lock").read_text().splitlines():
        f = line.split()
        if len(f) >= 2 and f[0] == name and not line.startswith("#"):
            return f[1]
    raise AssertionError("no %s pin in images.lock" % name)


def to_bytes(v):
    if isinstance(v, (int, float)):
        return int(v)
    m = re.fullmatch(r"(\d+)\s*([kmgKMG]?)[bB]?", str(v))
    if not m:
        raise AssertionError("cannot read memory value %r" % (v,))
    return int(m.group(1)) * {"": 1, "k": 1 << 10, "m": 1 << 20, "g": 1 << 30}[m.group(2).lower()]


def mem_limit(svc):
    v = svc.get("mem_limit") or ((svc.get("deploy") or {}).get("resources") or {}).get("limits", {}).get("memory")
    return None if v in (None, "") else to_bytes(v)


class DurableComposeTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        tc.require_docker()
        d = Path(tempfile.mkdtemp(prefix="test-compose-durable-", dir=tc.TMP_BASE))
        cls.addClassCleanup(shutil.rmtree, d, ignore_errors=True)
        key = d / "models.env"
        key.write_text(tc.KEY + "\n")
        key.chmod(0o600)
        pw = d / PASSWORD_FILE
        pw.write_text("test-only-durable-password\n")
        pw.chmod(0o600)
        (d / "projects").mkdir()
        env = {"AP_CREDENTIALS_FILE": str(key), "AP_PROJECTS_DIR": str(d / "projects"),
               "AP_PAGE_PORT": "18081", **tc.TOKENS, "AP_GATEWAY_IMAGE": tc.gateway_pin(),
               "AP_HOST_USER": "1234:1234", PASSWORD_VAR: str(pw), **tc.IMAGE_ENV}
        r = tc.compose_config(tc.COMPOSE, env)
        if r.returncode != 0:
            raise AssertionError("docker compose config failed on %s:\n%s" % (tc.COMPOSE, r.stderr))
        cls.cfg = json.loads(r.stdout)

    def svc(self, name):
        s = (self.cfg.get("services") or {}).get(name)
        self.assertIsNotNone(s, "service %s is missing from %s" % (name, tc.COMPOSE))
        return s

    def test_services_exist_with_the_pinned_images(self):
        for name, pinned in SERVICES.items():
            with self.subTest(service=name):
                self.assertEqual(self.svc(name).get("image"), pin(pinned))

    def test_each_is_hardened_and_publishes_nothing(self):
        for name in SERVICES:
            with self.subTest(service=name):
                s = self.svc(name)
                self.assertIs(s.get("read_only"), True, "read_only")
                self.assertEqual(s.get("cap_drop"), ["ALL"], "cap_drop")
                self.assertTrue(any(str(o).startswith("no-new-privileges") and "false" not in str(o)
                                    for o in s.get("security_opt") or []), "no-new-privileges")
                self.assertTrue(any(str(t).split(":")[0] == "/tmp" for t in s.get("tmpfs") or []),
                                "tmpfs /tmp")
                self.assertFalse(s.get("ports"), "published port")

    def test_memory_limits(self):
        for name in ("durable", "durable-store"):
            with self.subTest(service=name):
                self.assertEqual(mem_limit(self.svc(name)), 256 << 20)

    def test_store_runs_as_postgres_with_its_volume_and_run_folder(self):
        s = self.svc("durable-store")
        self.assertEqual(str(s.get("user")), "70:70")
        self.assertTrue(any(str(t).split(":")[0] == "/var/run/postgresql" for t in s.get("tmpfs") or []),
                        "tmpfs /var/run/postgresql")
        vols = [(v.get("type"), v.get("source")) for v in s.get("volumes") or []
                if isinstance(v, dict) and v.get("target") == DATA]
        self.assertEqual(vols, [("volume", "durable-store")], "volume at " + DATA)
        self.assertIn("durable-store", self.cfg.get("volumes") or {})

    def members(self, net):
        return {n for n, s in self.cfg["services"].items() if net in (s.get("networks") or {})}

    def test_networks_are_internal_with_their_members_only(self):
        want = {"durable": {"engine", "durable"},
                "durable-store": {"durable", "durable-schema", "durable-store"}}
        for net, members in want.items():
            with self.subTest(network=net):
                n = (self.cfg.get("networks") or {}).get(net)
                self.assertIsNotNone(n, "network %s is missing" % net)
                self.assertIs(n.get("internal"), True, "internal")
                self.assertEqual(self.members(net), members)

    def test_start_order(self):
        for name, dep, cond in (("durable-schema", "durable-store", "service_healthy"),
                                ("durable", "durable-schema", "service_completed_successfully"),
                                ("engine", "durable", "service_healthy")):
            with self.subTest(service=name):
                got = (self.svc(name).get("depends_on") or {}).get(dep) or {}
                self.assertEqual(got.get("condition"), cond)

    def test_dynamic_config_turns_the_version_check_off(self):
        s = self.svc("durable")
        files = [os.path.normpath(v["source"]) for v in s.get("volumes") or []
                 if isinstance(v, dict) and v.get("type") == "bind" and v.get("source")]
        files += [os.path.normpath(c["file"]) for ref in s.get("configs") or []
                  for c in [(self.cfg.get("configs") or {}).get(ref if isinstance(ref, str) else ref.get("source"), {})]
                  if c.get("file")]
        durable_dir = os.path.normpath(str(Path(tc.COMPOSE).parent / "durable"))
        mounted = [f for f in files if f.startswith(durable_dir + os.sep) and os.path.isfile(f)]
        self.assertTrue(mounted, "no file under src/deploy/durable/ is mounted into durable: %s" % files)
        import yaml
        values = []
        for f in mounted:
            doc = yaml.safe_load(Path(f).read_text()) or {}
            v = doc.get("frontend.enableServerVersionCheck")
            if v is not None:
                values.append(v[0].get("value") if isinstance(v, list) and v and isinstance(v[0], dict) else v)
        self.assertEqual(values, [False], "frontend.enableServerVersionCheck in %s" % mounted)


class DurablePasswordTest(unittest.TestCase):
    """`ap up` and `ap down` through the start stub (fixtures/start/docker-stub, unchanged)."""
    setUp0 = base.ComposeOverrideTest.setUp

    def setUp(self):
        self.setUp0()  # throwaway launcher tree, state, credentials, recorded images
        shutil.copy2(START_STUB, self.bin / "docker")
        self.pwfile = self.state / "agentic-platform" / "durable" / PASSWORD_FILE

    def ap(self, *args):
        e = {k: v for k, v in os.environ.items() if not k.startswith(("AP_", "STUB_"))}
        e.update(HOME=str(self.home), XDG_STATE_HOME=str(self.state), XDG_CONFIG_HOME=str(self.config),
                 STUB_LOG=str(self.log), PATH=str(self.bin) + os.pathsep + os.environ.get("PATH", ""))
        return subprocess.run(["sh", str(self.ap_path), *args], env=e, capture_output=True, text=True,
                              timeout=60, stdin=subprocess.DEVNULL, cwd=str(self.base))

    def up(self):
        r = self.ap("up", "--credentials", str(self.cred), "--projects", str(self.proj))
        self.assertEqual(r.returncode, 0, r.stderr)
        return r

    calls = base.ComposeOverrideTest.calls

    def test_up_makes_the_password_file_once_and_keeps_it(self):
        r1 = self.up()
        self.assertTrue(self.pwfile.is_file(), "ap up did not create %s" % self.pwfile)
        self.assertEqual(stat.S_IMODE(self.pwfile.stat().st_mode), 0o644)
        self.assertEqual(stat.S_IMODE(self.pwfile.parent.stat().st_mode), 0o700)
        first = self.pwfile.read_text()
        self.assertTrue(first.strip(), "the password file is empty")
        r2 = self.up()
        self.assertEqual(self.pwfile.read_text(), first, "a second ap up changed the password")
        self.assertEqual(stat.S_IMODE(self.pwfile.stat().st_mode), 0o644)
        self.assertEqual(stat.S_IMODE(self.pwfile.parent.stat().st_mode), 0o700)
        for r in (r1, r2):
            self.assertNotIn(first.strip(), r.stdout + r.stderr, "ap printed the password")
        self.assertNotIn(first.strip(), self.log.read_text(), "the password reached a docker call")

    def test_down_removes_no_volume(self):
        self.up()
        self.log.unlink(missing_ok=True)
        r = self.ap("down")
        self.assertEqual(r.returncode, 0, r.stderr)
        calls = self.calls()
        self.assertTrue([a for a in calls if a[:1] == ["compose"] and "down" in a], "no compose down call")
        for a in calls:
            self.assertFalse(a[:2] in (["volume", "rm"], ["volume", "prune"]), a)
            if a[:1] == ["compose"] and "down" in a:
                self.assertFalse({"-v", "--volumes"} & set(a), a)
        self.assertTrue(self.pwfile.is_file(), "the password file is missing after up and down (ap down must keep it)")


if __name__ == "__main__":
    unittest.main()
