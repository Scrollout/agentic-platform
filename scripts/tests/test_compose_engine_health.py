"""T042: `engine` has a Compose healthcheck that runs its own `/engine health`, so
`docker compose up --wait` (`ap up`, research R7) returns only when the engine listens, after it
has reached Temporal and registered its namespace. Judged on `docker compose config --format json`
of src/deploy/compose.yaml, never on its text.

Rule (`violations()`): the engine service has a healthcheck, not disabled, whose test is exactly
["CMD", "/engine", "health"] (CMD form: the image is FROM scratch, it has no shell, see
Dockerfile.engine); interval, timeout, retries and start_period are all set. It passes on a clean
planted copy with that healthcheck and fails, naming the rule, on planted copies: no healthcheck,
the CMD-SHELL form, another command, and each of the four timing fields removed.
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
TEST = ["CMD", "/engine", "health"]


def violations(cfg):
    svc = cfg["services"].get("engine")
    if svc is None:
        return ["no engine service"]
    hc = svc.get("healthcheck")
    if not hc or hc.get("disable"):
        return ["healthcheck: engine has none"]
    out = []
    test = hc.get("test") or []
    if isinstance(test, str):
        test = ["CMD-SHELL", test]
    if test[:1] != ["CMD"]:
        out.append("healthcheck_form: the test must use the CMD form (the image has no shell)")
    elif test != TEST:
        out.append("healthcheck_command: the test must be exactly %s" % TEST)
    for k in TIMING:
        if not hc.get(k):
            out.append("healthcheck_%s: not set" % k)
    return out


def plant(text, old, new):
    assert old in text, old
    return text.replace(old, new, 1)


class EngineHealthTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        require_docker()
        cls.base_dir = Path(tempfile.mkdtemp(prefix="test-engine-health-", dir=TMP_BASE))
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

    def engine_block(self, text):
        m = re.search(r"^  engine:\n(?:(?:    .*|)\n)+", text, re.M)
        assert m, "no engine block"
        return m

    def test_the_real_file_has_the_healthcheck(self):
        self.assertEqual(violations(self.cfg(COMPOSE)), [])

    def test_the_retries_are_at_least_one(self):
        hc = self.cfg(COMPOSE)["services"]["engine"].get("healthcheck") or {}
        self.assertTrue(hc, "engine has no healthcheck (T042)")
        self.assertGreaterEqual(hc.get("retries", 0), 1)

    # the judge can pass and fail
    def test_planted_no_healthcheck_fails(self):
        def drop(t):
            m = self.engine_block(t)
            blk = re.sub(r"^    healthcheck:\n(?:      .*\n|\n)+", "", m.group(0), flags=re.M)
            blk = re.sub(r"^    healthcheck:.*\n", "", blk, flags=re.M)
            return t[:m.start()] + blk + t[m.end():]
        v = violations(self.cfg(self.copy("no-hc.yaml", drop)))
        self.assertTrue(any(x.startswith("healthcheck:") for x in v), v)

    def planted_healthcheck(self, name, hc_yaml):
        def add(t):
            m = self.engine_block(t)
            blk = m.group(0)
            blk = re.sub(r"^    healthcheck:\n(?:      .*\n|\n)+", "", blk, flags=re.M)
            blk = blk.rstrip("\n") + "\n    healthcheck:\n" + hc_yaml
            return t[:m.start()] + blk + "\n" + t[m.end():]
        return violations(self.cfg(self.copy(name + ".yaml", add)))

    GOOD = ('      test: ["CMD", "/engine", "health"]\n'
            "      interval: 5s\n      timeout: 3s\n      retries: 12\n      start_period: 60s\n")

    def test_reference_passes(self):
        self.assertEqual(self.planted_healthcheck("ref", self.GOOD), [])

    def test_planted_cmd_shell_form_fails(self):
        v = self.planted_healthcheck("shell", self.GOOD.replace(
            '["CMD", "/engine", "health"]', '["CMD-SHELL", "/engine health"]'))
        self.assertTrue(any("healthcheck_form" in x for x in v), v)

    def test_planted_string_form_fails(self):
        v = self.planted_healthcheck("string", self.GOOD.replace(
            '["CMD", "/engine", "health"]', '"/engine health"'))
        self.assertTrue(any("healthcheck_form" in x for x in v), v)

    def test_planted_another_command_fails(self):
        for name, test in (("other", '["CMD", "/engine", "ready"]'),
                           ("bare", '["CMD", "/engine"]'),
                           ("curl", '["CMD", "curl", "-f", "http://127.0.0.1:8080/"]')):
            with self.subTest(test=test):
                v = self.planted_healthcheck(name, self.GOOD.replace('["CMD", "/engine", "health"]', test))
                self.assertTrue(any("healthcheck_command" in x for x in v), v)

    def test_planted_missing_timing_fails(self):
        for k in TIMING:
            with self.subTest(field=k):
                hc = "".join(l + "\n" for l in self.GOOD.splitlines() if not l.strip().startswith(k + ":"))
                v = self.planted_healthcheck("no-" + k, hc)
                self.assertTrue(any("healthcheck_" + k in x for x in v), v)


if __name__ == "__main__":
    unittest.main()
