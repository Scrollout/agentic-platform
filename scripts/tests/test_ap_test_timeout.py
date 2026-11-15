"""The test binary's time limit in `ap test <suite>` (T054l, T054f; research D34).

Go stops a test binary after 10 minutes unless it is given `-test.timeout`, and the planted and US2
suites run far longer (about 35 to 40 and 20 to 25 minutes, with the lab's image builds). So `cmd_test`
passes `-test.timeout 60m` for `planted` and `75m` for `us2`, which are the caps in T054f. Either
spelling Go takes counts: `-test.timeout 60m`, `-test.timeout=60m` or the same with two dashes.

This reuses the rig of test_ap_test.py (a fake `docker` that writes a fake test binary logging its
argv); it does not change that file and does not run its tests again.
"""
import unittest

import test_ap_test as base


def make_rig():
    """A built rig of test_ap_test.py. Its class is made here so that no test runner collects it
    and runs that file's tests again."""
    class Rig(base.ApTestTest):
        def runTest(self):  # a TestCase needs one method to be built by hand
            pass
    return Rig()


def timeout_of(argv):
    """The value of the one -test.timeout in argv, in any spelling Go takes, or None."""
    vals = []
    for k, a in enumerate(argv):
        for flag in ("-test.timeout", "--test.timeout"):
            if a == flag and k + 1 < len(argv):
                vals.append(argv[k + 1])
            elif a.startswith(flag + "="):
                vals.append(a.split("=", 1)[1])
    if len(vals) > 1:
        raise AssertionError("more than one -test.timeout in %s" % argv)
    return vals[0] if vals else None


class ApTestTimeoutTest(unittest.TestCase):
    def setUp(self):
        self.rig = make_rig()
        self.rig.setUp()
        self.addCleanup(self.rig.doCleanups)

    def binary_argv(self, suite, stream):
        r = self.rig.ap("test", suite, stream=stream)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        (run,) = self.rig.binary_runs()
        return run["argv"]

    def test_planted_runs_the_binary_with_a_timeout_of_60m(self):
        argv = self.binary_argv("planted", "planted-pass")
        self.assertEqual(timeout_of(argv), "60m", "argv %s" % argv)

    def test_us2_runs_the_binary_with_a_timeout_of_75m(self):
        argv = self.binary_argv("us2", "us2-pass")
        self.assertEqual(timeout_of(argv), "75m", "argv %s" % argv)

    def test_the_timeout_is_not_given_to_the_build(self):
        # `go test -c` takes -test.timeout too, but only the run needs it.
        self.rig.ap("test", "planted")
        for c in self.rig.build_calls():
            self.assertIsNone(timeout_of(c["argv"]), "the build call got a -test.timeout: %s" % c["argv"])


if __name__ == "__main__":
    unittest.main()
