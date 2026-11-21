"""T113 unit (2): `ap up` waits for readiness (src/cli/contract.md: "`up` returns only when every
service that declares a readiness check is ready"), judged only by what `ap` passes to `docker`.

Rules: `up` makes exactly one `docker compose ... up` call; it has `-d` and `--wait`, and keeps every
`--scale` option it already adds (page=0, page-proxy=0 while the page image is a placeholder). When
Compose's `up` exits non-zero (as `--wait` does when a service never becomes healthy), `up` exits 1,
says so, and keeps the session file (`ap down` recovers).
The planted violation is a launcher copy without `--wait`: the judge fails it. The setup is
test_ap_compose_override.py's (imported, unchanged); only the docker stand-in differs: it fails a
`compose ... up` call when STUB_UP_EXIT is set.
"""
import unittest

import test_ap_compose_override as base

WRAPPER = '''#!/usr/bin/env python3
import os, subprocess, sys
argv = sys.argv[1:]
code = subprocess.run([sys.executable, os.environ["STUB_DELEGATE"], *argv], stdin=subprocess.DEVNULL).returncode
if argv[:1] == ["compose"] and "up" in argv and os.environ.get("STUB_UP_EXIT"):
    sys.exit(int(os.environ["STUB_UP_EXIT"]))
sys.exit(code)
'''


def up_calls(calls):
    return [a for a in calls if a[:1] == ["compose"] and "up" in a]


def violations(argv):
    out = []
    if "-d" not in argv and "--detach" not in argv:
        out.append("detach: -d missing")
    if "--wait" not in argv:
        out.append("wait: --wait missing")
    return out


class UpWaitTest(unittest.TestCase):
    def setUp(self):
        base.ComposeOverrideTest.setUp(self)  # the throwaway launcher tree, session folders, images
        (self.bin / "docker").write_text(WRAPPER)
        (self.bin / "docker").chmod(0o755)

    ap = base.ComposeOverrideTest.ap
    up = base.ComposeOverrideTest.up
    calls = base.ComposeOverrideTest.calls
    compose_calls = base.ComposeOverrideTest.compose_calls
    clear = base.ComposeOverrideTest.clear

    def one_up(self, r):
        ups = up_calls(self.calls())
        self.assertEqual(len(ups), 1, "%s\n%s" % (ups, r.stderr))
        return ups[0]

    def test_up_waits(self):
        r = self.up()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(violations(self.one_up(r)), [])

    def test_up_keeps_its_scale_options_with_wait(self):
        # no recorded page image: ap adds --scale page=0 --scale page-proxy=0
        images = self.state / "agentic-platform/images.env"
        images.write_text("".join(l + "\n" for l in images.read_text().splitlines()
                                  if not l.startswith("AP_IMAGE_PAGE=")))
        r = self.up()
        self.assertEqual(r.returncode, 0, r.stderr)
        argv = self.one_up(r)
        self.assertEqual(violations(argv), [])
        self.assertIn("page=0", argv)
        self.assertIn("page-proxy=0", argv)

    def test_failed_wait_exits_1_and_keeps_the_session(self):
        r = self.up(STUB_UP_EXIT="1")
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn("docker compose up failed", r.stderr)
        self.assertTrue(self.session.exists(), "the session file was removed")
        self.assertIn("--wait", self.one_up(r))

    # the judge can pass and fail
    def test_launcher_without_wait_fails(self):
        text = base.REAL_AP.read_text()
        self.assertTrue("--wait" in text, "the launcher has no --wait (T113)")
        self.ap_path.write_text(text.replace("compose up -d", "compose up -d", 1).replace("--wait", ""))
        self.ap_path.chmod(0o755)
        r = self.up()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(violations(self.one_up(r)), "a launcher without --wait must fail the judge")


if __name__ == "__main__":
    unittest.main()
