"""T005 (feature 002, research R1, S14, SC-004): the durable-execution services running for real.
Test author's file; the code author (T004) makes it pass without changing it. Judged in CI's
`py_tests` job (real Docker); skipped with a reason when Docker or Compose is absent.

What it pins, with only `durable-store`, `durable-schema` and `durable` up from
src/deploy/compose.yaml (the engine and every other service stay down):
  * the schema job exits 0, and exits 0 again when run a second time, with the schema version of
    both databases (`temporal`, `temporal_visibility`) unchanged between the runs (read from the
    database, through `psql` inside `durable-store` as its own $POSTGRES_USER);
  * `temporal operator cluster health`, run from the admin-tools pin of src/deploy/images.lock on
    the `durable` network, says SERVING;
  * the server runs as uid 1000 and PostgreSQL as uid 70, both with a read-only root file system;
  * after 60 s idle, the two services use at most 512 MiB together (`docker stats`, SC-004).
Interface: the PostgreSQL password file is handed to Compose as AP_DURABLE_PASSWORD_FILE, pointing
at a file this test makes in a temporary folder (the same variable test_compose_durable.py fixes).

Safety: its own Compose project (unique per run); cleanup is `down -v` of that project only, also
after a failure. Nothing else on the host is touched. Times of setup, test and teardown are printed.
"""
import os
import re
import secrets
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))  # sibling test modules
import test_compose as tc  # noqa: E402  (its tokens, image references and key text)

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
COMPOSE = REPO / "src/deploy/compose.yaml"
SERVICES = ["durable-store", "durable-schema", "durable"]
FIVE_TWELVE_MIB = 512 << 20
IDLE_SECONDS = 60
DOCKER = shutil.which("docker")


def docker_ready():
    if not DOCKER:
        return "docker is not on PATH"
    for args in (["version", "--format", "{{.Server.Version}}"], ["compose", "version"]):
        try:
            r = subprocess.run([DOCKER] + args, capture_output=True, text=True, timeout=60)
        except (OSError, subprocess.TimeoutExpired) as e:
            return "docker %s failed: %s" % (args[0], e)
        if r.returncode != 0:
            return "docker %s is not usable: %s" % (args[0], r.stderr.strip())
    return None


def pin(name):
    for line in (REPO / "src/deploy/images.lock").read_text().splitlines():
        f = line.split()
        if len(f) >= 2 and f[0] == name and not line.startswith("#"):
            return f[1]
    raise AssertionError("no %s pin in images.lock" % name)


def to_bytes(text):
    m = re.fullmatch(r"([\d.]+)\s*([KMGT]?i?B)", text.strip())
    if not m:
        raise AssertionError("cannot read memory value %r" % text)
    unit = {"B": 1, "KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "kB": 10 ** 3, "MB": 10 ** 6,
            "GB": 10 ** 9}[m.group(2)]
    return int(float(m.group(1)) * unit)


@unittest.skipIf(docker_ready(), "real Docker with Compose is required: %s" % docker_ready())
class DurableServicesTest(unittest.TestCase):
    project = "aptest-durable-" + secrets.token_hex(4)
    started = False

    @classmethod
    def setUpClass(cls):
        t0 = time.monotonic()
        cls.tmp = Path(tempfile.mkdtemp(prefix="test-durable-", dir=tc.TMP_BASE))
        cls.addClassCleanup(shutil.rmtree, cls.tmp, ignore_errors=True)
        cls.addClassCleanup(cls.teardown_project)
        key = cls.tmp / "models.env"
        key.write_text(tc.KEY + "\n")
        key.chmod(0o600)
        (cls.tmp / "durable").mkdir(mode=0o700)
        (cls.tmp / "durable").chmod(0o700)
        pw = cls.tmp / "durable" / "durable-store-password"
        pw.write_text(secrets.token_hex(16) + "\n")
        pw.chmod(0o644)
        (cls.tmp / "projects").mkdir()
        env = {k: v for k, v in os.environ.items() if not k.startswith("AP_")}
        env.update({"AP_CREDENTIALS_FILE": str(key), "AP_PROJECTS_DIR": str(cls.tmp / "projects"),
                    "AP_PAGE_PORT": "18082", **tc.TOKENS, "AP_GATEWAY_IMAGE": tc.gateway_pin(),
                    "AP_HOST_USER": "%d:%d" % (os.getuid(), os.getgid()),
                    "AP_DURABLE_PASSWORD_FILE": str(pw), **tc.IMAGE_ENV})
        cls.env = env
        # Fails here, before any container starts, when a service is missing from compose.yaml.
        r = cls.compose("config", "--services", check=False)
        have = set(r.stdout.split())
        missing = [s for s in SERVICES if s not in have]
        if r.returncode != 0 or missing:
            raise AssertionError("compose.yaml cannot start %s (missing: %s)\n%s"
                                 % (SERVICES, missing, r.stderr))
        cls.started = True
        cls.compose("up", "-d", "--wait", "--no-build", "durable", timeout=900)
        print("\n[durable-services] setup %.1f s" % (time.monotonic() - t0), file=sys.stderr)
        cls.t_test = time.monotonic()

    @classmethod
    def teardown_project(cls):
        t0 = time.monotonic()
        if cls.started:
            print("[durable-services] tests %.1f s" % (t0 - cls.t_test), file=sys.stderr)
            cls.compose("down", "-v", "--remove-orphans", check=False, timeout=300)
            # leftovers of this project only (the one-off health containers carry its name prefix)
            ids = subprocess.run([DOCKER, "ps", "-aq", "--filter", "name=^%s-" % cls.project],
                                 capture_output=True, text=True).stdout.split()
            if ids:
                subprocess.run([DOCKER, "rm", "-f", "-v"] + ids, capture_output=True)
            print("[durable-services] teardown %.1f s" % (time.monotonic() - t0), file=sys.stderr)

    @classmethod
    def compose(cls, *args, check=True, timeout=300, input=None):
        r = subprocess.run([DOCKER, "compose", "-p", cls.project, "-f", str(COMPOSE), *args],
                           env=cls.env, capture_output=True, text=True, timeout=timeout)
        if check and r.returncode != 0:
            raise AssertionError("docker compose %s failed (%d):\n%s\n%s"
                                 % (" ".join(args), r.returncode, r.stdout[-2000:], r.stderr[-2000:]))
        return r

    @classmethod
    def container(cls, service):
        return cls.compose("ps", "-q", service).stdout.strip()

    @classmethod
    def schema_versions(cls):
        out = []
        for db in ("temporal", "temporal_visibility"):
            sql = ("select curr_version from schema_version where version_partition=0 "
                   "and db_name='%s'" % db)
            r = cls.compose("exec", "-T", "durable-store", "sh", "-c",
                            'psql -U "$POSTGRES_USER" -d %s -tAc "%s"' % (db, sql))
            out.append(r.stdout.strip())
        return out

    @classmethod
    def admin(cls, *args):
        """Run `temporal ...` from the admin-tools pin on the `durable` network."""
        name = "%s-admin-%s" % (cls.project, secrets.token_hex(3))
        r = subprocess.run([DOCKER, "run", "--rm", "--name", name, "--network", cls.project + "_durable",
                            "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
                            "--tmpfs", "/tmp", "--memory", "256m", pin("temporal-admin-tools"),
                            "temporal", *args, "--address", "durable:7233"],
                           capture_output=True, text=True, timeout=120)
        return r

    def test_1_schema_job_converges_and_repeats_with_the_same_version(self):
        before = self.schema_versions()
        self.assertTrue(all(re.fullmatch(r"\d+\.\d+", v) for v in before), "versions %r" % before)
        r = self.compose("run", "--rm", "--no-deps", "-T", "durable-schema", check=False, timeout=300)
        self.assertEqual(r.returncode, 0, "second run of durable-schema:\n%s\n%s" % (r.stdout, r.stderr))
        self.assertEqual(self.schema_versions(), before, "schema version changed on the second run")

    def test_2_cluster_health_is_serving(self):
        r = self.admin("operator", "cluster", "health")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("SERVING", r.stdout)

    def test_4_users_and_read_only_roots(self):
        for service, uid in (("durable", "1000"), ("durable-store", "70")):
            with self.subTest(service=service):
                got = self.compose("exec", "-T", service, "id", "-u").stdout.strip()
                self.assertEqual(got, uid)
                ro = subprocess.run([DOCKER, "inspect", "--format", "{{.HostConfig.ReadonlyRootfs}}",
                                     self.container(service)], capture_output=True, text=True).stdout.strip()
                self.assertEqual(ro, "true", "read-only root of " + service)

    def test_5_idle_memory_is_within_512_mib(self):
        time.sleep(IDLE_SECONDS)
        ids = [self.container("durable"), self.container("durable-store")]
        r = subprocess.run([DOCKER, "stats", "--no-stream", "--format", "{{.MemUsage}}"] + ids,
                           capture_output=True, text=True, timeout=120)
        self.assertEqual(r.returncode, 0, r.stderr)
        used = [to_bytes(line.split("/")[0]) for line in r.stdout.strip().splitlines()]
        self.assertEqual(len(used), 2, r.stdout)
        self.assertLessEqual(sum(used), FIVE_TWELVE_MIB, "idle memory %.0f MiB: %s"
                             % (sum(used) / (1 << 20), r.stdout.strip()))


if __name__ == "__main__":
    unittest.main()
