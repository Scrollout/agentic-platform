"""Regression tests for two defects a fresh review of user story 1 found in `ap start` (2026-11-18).

Source: the fresh review of US1 of feature 003, 2026-11-18.
Contract: src/cli/contract.md, "Starting the platform", step 1 ("a second `start` waits for the first,
then finds nothing to do"). Spec: specs/003-one-command-start/spec.md FR-001 (one command takes the
machine from any state to started). Data model: specs/003-one-command-start/data-model.md, "Image
fingerprint" (an image is current only while the image it recorded is still there to start).

Both reuse the harness of test_ap_start.py (stub `docker`, no real Docker) and add no behaviour to it.
1. test_a_second_start_waits_while_the_first_holds_the_lock_longer_than_two_minutes
   (takes about 2.5 minutes; today the second start gives up after 120 s with exit 1)
2. test_a_recorded_image_missing_from_the_image_store_is_built_again
   (today nothing is built, because every fingerprint matches)
"""
import shutil
import subprocess
import sys
import time
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from test_ap_start import StartCase, SEVEN, PASSWORD  # noqa: E402


class StartReviewTest(StartCase):
    def test_a_second_start_waits_while_the_first_holds_the_lock_longer_than_two_minutes(self):
        """Expected time: about 2.5 minutes (it waits 130 s on purpose, then finishes at once)."""
        self.make_current()
        sleeper = subprocess.Popen(["sleep", "600"])
        self.addCleanup(sleeper.kill)
        self.start_lock.mkdir()
        (self.start_lock / "pid").write_text("%d\n" % sleeper.pid)
        p = subprocess.Popen(["sh", str(self.ap_path), *self.start_args()], env=self.env(),
                             stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                             stdin=subprocess.DEVNULL)
        self.addCleanup(p.kill)
        time.sleep(130)
        still_running = p.poll() is None
        calls = self.compose_up_indexes()
        shutil.rmtree(self.start_lock, ignore_errors=True)
        sleeper.kill()
        try:
            out, err = p.communicate(timeout=60)
        except subprocess.TimeoutExpired:
            p.kill()
            self.fail("the start did not finish within 60 s of the lock being released")
        self.assertTrue(still_running, "the second start must still wait at 130 s; it ended: " + err)
        self.assertEqual(calls, [], "no compose up while another start holds the lock")
        self.assertEqual(p.returncode, 0, err)
        self.assertIn("start: started", err)

    def test_a_recorded_image_missing_from_the_image_store_is_built_again(self):
        self.make_current()
        r = self.start(STUB_IMAGE_MISSING=1, COSIGN_PASSWORD=PASSWORD, timeout=120)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("build: building " + " ".join(SEVEN), r.stderr)
        self.assertEqual(sorted(self.pushes()), sorted(SEVEN))
        self.assertEqual(len(self.signs()), len(SEVEN))
        self.assertNoSecret(r)


if __name__ == "__main__":
    unittest.main()
