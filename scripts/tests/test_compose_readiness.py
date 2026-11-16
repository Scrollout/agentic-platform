"""T113 unit (1): `contract-lint` has a Compose healthcheck on its own `GET /health`, so
`docker compose up --wait` (src/cli/contract.md, `up`) returns only when the lint listens.
Judged on `docker compose config --format json` of src/deploy/compose.yaml, never on its text.

Rule (`violations()`): the healthcheck's test runs `node` (the image's only interpreter, see
Dockerfile.lint) and requests /health on 127.0.0.1:8080 (server.mjs listens on 8080); interval,
timeout, retries and start_period are all set. It passes on the real file and fails, naming the
rule, on planted copies: no healthcheck, a test that never asks /health, a test on another port
or without node, and each of the four timing fields removed.
A missing docker compose is a failure, never a skip.
"""
import json
import os
import re
import shutil
import tempfile
import unittest
from pathlib import Path

from test_compose import IMAGE_ENV, KEY, TOKENS, require_docker
from test_e2e_override import config

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
# AP_TEST_COMPOSE points the tests at another compose.yaml (to see them fail against a mutant)
COMPOSE = Path(os.environ.get("AP_TEST_COMPOSE") or REPO / "src/deploy/compose.yaml")
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
TIMING = ("interval", "timeout", "retries", "start_period")


def violations(cfg):
    svc = cfg["services"].get("contract-lint")
    if svc is None:
        return ["no contract-lint service"]
    hc = svc.get("healthcheck")
    if not hc or hc.get("disable"):
        return ["healthcheck: contract-lint has none"]
    out = []
    test = hc.get("test") or []
    if isinstance(test, str):
        test = ["CMD-SHELL", test]
    text = " ".join(test)
    if not (test[:1] in (["CMD"], ["CMD-SHELL"]) and re.search(r"\bnode\b", text)):
        out.append("healthcheck_node: the test must run node")
    if "/health" not in text:
        out.append("healthcheck_path: the test must request /health")
    if not re.search(r"127\.0\.0\.1:8080", text):
        out.append("healthcheck_address: the test must use 127.0.0.1:8080")
    for k in TIMING:
        if not hc.get(k):
            out.append("healthcheck_%s: not set" % k)
    return out


def seconds(v):
    """Compose's duration as seconds: a number (nanoseconds) or a string such as "1m0s", "500ms"."""
    if isinstance(v, (int, float)):
        return v / 1e9
    unit = {"h": 3600, "m": 60, "s": 1, "ms": 0.001, "us": 1e-6, "ns": 1e-9}
    return sum(float(n) * unit[u] for n, u in re.findall(r"([\d.]+)(ms|us|ns|h|m|s)", str(v)))


def plant(text, old, new):
    assert old in text, old
    return text.replace(old, new, 1)


class ReadinessTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        require_docker()
        cls.base_dir = Path(tempfile.mkdtemp(prefix="test-readiness-", dir=TMP_BASE))
        cls.env = {**IMAGE_ENV, **TOKENS, "AP_HOST_USER": "1000:1000", "AP_PAGE_PORT": "8765",
                   "AP_CREDENTIALS_FILE": str(cls.base_dir / "models.env"),
                   "AP_PROJECTS_DIR": str(cls.base_dir / "projects"),
                   "AP_GRANTS_DIR": str(cls.base_dir / "grants"),
                   "AP_GATEWAY_IMAGE": "nginx:1", "AP_MODEL_KEY": KEY}

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.base_dir, ignore_errors=True)

    def cfg(self, path):
        r = config([path], self.env)
        self.assertEqual(r.returncode, 0, "docker compose config failed for %s:\n%s" % (path, r.stderr))
        return json.loads(r.stdout)

    def copy(self, name, mutate):
        p = self.base_dir / name  # beside nothing else: Compose resolves relative paths from here
        p.write_text(mutate(COMPOSE.read_text()))
        return p

    def lint_block(self, text):
        m = re.search(r"^  contract-lint:\n(?:(?:    .*|)\n)+", text, re.M)
        assert m, "no contract-lint block"
        return m

    def test_the_real_file_has_the_healthcheck(self):
        self.assertEqual(violations(self.cfg(COMPOSE)), [])

    def test_the_timing_is_sane(self):
        hc = self.cfg(COMPOSE)["services"]["contract-lint"].get("healthcheck") or {}
        self.assertTrue(hc, "contract-lint has no healthcheck (T113)")
        self.assertGreaterEqual(hc.get("retries", 0), 1)
        self.assertGreater(seconds(hc.get("start_period", 0)), 0)

    # the judge can pass and fail
    def test_planted_no_healthcheck_fails(self):
        def drop(t):
            m = self.lint_block(t)
            blk = re.sub(r"^    healthcheck:\n(?:      .*\n|\n)+", "", m.group(0), flags=re.M)
            blk = re.sub(r"^    healthcheck:.*\n", "", blk, flags=re.M)
            return t[:m.start()] + blk + t[m.end():]
        v = violations(self.cfg(self.copy("no-hc.yaml", drop)))
        self.assertTrue(any(x.startswith("healthcheck:") for x in v), v)

    def planted_healthcheck(self, name, hc_yaml):
        def add(t):
            m = self.lint_block(t)
            blk = m.group(0)
            blk = re.sub(r"^    healthcheck:\n(?:      .*\n|\n)+", "", blk, flags=re.M)
            blk = blk.rstrip("\n") + "\n    healthcheck:\n" + hc_yaml
            return t[:m.start()] + blk + "\n" + t[m.end():]
        return violations(self.cfg(self.copy(name + ".yaml", add)))

    GOOD = ('      test: ["CMD", "node", "-e", "require(\'http\').get(\'http://127.0.0.1:8080/health\',r=>process.exit(r.statusCode===200?0:1)).on(\'error\',()=>process.exit(1))"]\n'
            "      interval: 5s\n      timeout: 3s\n      retries: 12\n      start_period: 20s\n")

    def test_reference_passes(self):
        self.assertEqual(self.planted_healthcheck("ref", self.GOOD), [])

    def test_planted_test_not_hitting_health_fails(self):
        v = self.planted_healthcheck("nopath", self.GOOD.replace("/health", "/"))
        self.assertTrue(any("healthcheck_path" in x for x in v), v)

    def test_planted_wrong_port_fails(self):
        v = self.planted_healthcheck("port", self.GOOD.replace("8080", "9090"))
        self.assertTrue(any("healthcheck_address" in x for x in v), v)

    def test_planted_without_node_fails(self):
        v = self.planted_healthcheck("nonode", '      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8080/health"]\n'
                                     + self.GOOD.split("\n", 1)[1])
        self.assertTrue(any("healthcheck_node" in x for x in v), v)

    def test_planted_missing_timing_fails(self):
        for k in TIMING:
            with self.subTest(field=k):
                hc = "".join(l + "\n" for l in self.GOOD.splitlines() if not l.strip().startswith(k + ":"))
                v = self.planted_healthcheck("no-" + k, hc)
                self.assertTrue(any("healthcheck_" + k in x for x in v), v)


if __name__ == "__main__":
    unittest.main()
