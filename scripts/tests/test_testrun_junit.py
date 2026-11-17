"""T111 (research D43, ADR 0046): `scripts/testrun.py --junit <file>` writes a JUnit XML report.

Written before the code, by a separate author. testrun.py runs the tests in the `tests` folder
beside itself, so each test copies the script under test into a throwaway folder and plants a
`tests` folder beside the copy holding four tests: one passing, one failing, one erroring and
one skipped, in two classes (so `--jobs 2` runs them in two child processes). The report must
hold one `testcase` per test with its class, name and time, and `failure`, `error` or `skipped`
carrying the test's message; without `--junit` no file is written.
"""
import shutil
import subprocess
import sys
import tempfile
import unittest
import xml.etree.ElementTree as ET
from pathlib import Path

HERE = Path(__file__).resolve().parent
TESTRUN = HERE.parent / "testrun.py"

MODULE = "test_zz_junit_fixture"
FIXTURE = '''import unittest


class PassFailTest(unittest.TestCase):
    def test_passes(self):
        self.assertEqual(1, 1)

    def test_fails(self):
        self.fail("PLANTED-FAILURE-MESSAGE")


class ErrorSkipTest(unittest.TestCase):
    def test_errors(self):
        raise RuntimeError("PLANTED-ERROR-MESSAGE")

    def test_skipped(self):
        self.skipTest("PLANTED-SKIP-MESSAGE")
'''
EXPECTED = {  # (classname, name): (outcome element or None, message)
    (MODULE + ".PassFailTest", "test_passes"): (None, None),
    (MODULE + ".PassFailTest", "test_fails"): ("failure", "PLANTED-FAILURE-MESSAGE"),
    (MODULE + ".ErrorSkipTest", "test_errors"): ("error", "PLANTED-ERROR-MESSAGE"),
    (MODULE + ".ErrorSkipTest", "test_skipped"): ("skipped", "PLANTED-SKIP-MESSAGE"),
}
OUTCOMES = ("failure", "error", "skipped")


class TestrunJunitTest(unittest.TestCase):
    def setUp(self):
        t = tempfile.TemporaryDirectory()
        self.addCleanup(t.cleanup)
        self.root = Path(t.name)
        scripts = self.root / "scripts"
        (scripts / "tests").mkdir(parents=True)
        shutil.copy(TESTRUN, scripts / "testrun.py")
        (scripts / "tests" / (MODULE + ".py")).write_text(FIXTURE)
        self.script = scripts / "testrun.py"
        self.out = self.root / "out"
        self.out.mkdir()

    def run_testrun(self, *args):
        r = subprocess.run([sys.executable, str(self.script), *args], cwd=self.root,
                           capture_output=True, text=True, timeout=60)
        return r, r.stdout + r.stderr

    def xml_files(self):
        return sorted(str(p.relative_to(self.root)) for p in self.root.rglob("*.xml"))

    def assert_report(self, path, out):
        self.assertTrue(path.exists(), "no JUnit file at %s\n%s" % (path, out))
        root = ET.parse(path).getroot()
        self.assertIn(root.tag, ("testsuites", "testsuite"), "not a JUnit report: %s" % root.tag)
        cases = root.iter("testcase")
        seen = {}
        for c in cases:
            key = (c.get("classname"), c.get("name"))
            self.assertNotIn(key, seen, "testcase %s twice" % (key,))
            seen[key] = c
        self.assertEqual(sorted(seen), sorted(EXPECTED), "one testcase per test, by class and name")
        for key, (outcome, message) in EXPECTED.items():
            c = seen[key]
            with self.subTest(test=key[1]):
                self.assertIsNotNone(c.get("time"), "testcase %s has no time" % key[1])
                self.assertGreaterEqual(float(c.get("time")), 0.0)
                marks = [e.tag for e in c if e.tag in OUTCOMES]
                if outcome is None:
                    self.assertEqual(marks, [], "a passing test is marked %s" % marks)
                    continue
                self.assertEqual(marks, [outcome], "%s should be marked %s" % (key[1], outcome))
                el = c.find(outcome)
                text = (el.get("message") or "") + "\n" + (el.text or "")
                self.assertIn(message, text, "%s does not carry the test's message" % outcome)

    def test_junit_one_process(self):
        report = self.out / "py-tests.xml"
        r, out = self.run_testrun("--junit", str(report))
        self.assertEqual(r.returncode, 1, "a failing test must still fail the run\n" + out)
        self.assert_report(report, out)

    def test_junit_with_jobs(self):
        report = self.out / "py-tests.xml"
        r, out = self.run_testrun("--jobs", "2", "--junit", str(report))
        self.assertEqual(r.returncode, 1, "a failing test must still fail the run\n" + out)
        self.assert_report(report, out)

    def test_junit_of_chosen_tests(self):
        # A partial run reports only the chosen tests.
        report = self.out / "py-tests.xml"
        r, out = self.run_testrun("--junit", str(report), MODULE + ".PassFailTest.test_passes")
        self.assertEqual(r.returncode, 0, out)
        self.assertTrue(report.exists(), out)
        names = [c.get("name") for c in ET.parse(report).getroot().iter("testcase")]
        self.assertEqual(names, ["test_passes"])

    def test_no_file_without_junit(self):
        for args in ((), ("--jobs", "2")):
            with self.subTest(args=args):
                r, out = self.run_testrun(*args)
                self.assertEqual(r.returncode, 1, out)
                self.assertEqual(self.xml_files(), [], "a report was written without --junit")

    def test_the_fixture_runs_as_planted(self):
        # Control: the copy runs the planted tests (so a missing report is the report's fault).
        _, out = self.run_testrun()
        self.assertIn("PLANTED-FAILURE-MESSAGE", out)
        self.assertIn("PLANTED-ERROR-MESSAGE", out)
        self.assertRegex(out, r"failures=1")
        self.assertRegex(out, r"errors=1")
        self.assertRegex(out, r"skipped=1")



EDGE_MODULE = "test_zz_junit_edge_fixture"
EDGE_FIXTURE = '''import unittest


class BrokenFixtureTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        raise RuntimeError("PLANTED-FIXTURE-ERROR")

    def test_never_runs(self):
        pass


class ControlCharsTest(unittest.TestCase):
    def test_fails_with_control_characters(self):
        self.fail("PLANTED-CTRL \\x1b[31mred\\x16x end")
'''


class TestrunJunitEdgeTest(unittest.TestCase):
    """Regressions: a class whose setUpClass raises is reported as an error, and a message
    holding control characters still gives a report ET.parse reads."""

    def setUp(self):
        t = tempfile.TemporaryDirectory()
        self.addCleanup(t.cleanup)
        self.root = Path(t.name)
        scripts = self.root / "scripts"
        (scripts / "tests").mkdir(parents=True)
        shutil.copy(TESTRUN, scripts / "testrun.py")
        (scripts / "tests" / (EDGE_MODULE + ".py")).write_text(EDGE_FIXTURE)
        self.script = scripts / "testrun.py"
        self.report = self.root / "out" / "py-tests.xml"
        self.report.parent.mkdir()

    def cases(self, *args):
        r = subprocess.run([sys.executable, str(self.script), *args, "--junit", str(self.report)],
                           cwd=self.root, capture_output=True, text=True, timeout=60)
        out = r.stdout + r.stderr
        self.assertEqual(r.returncode, 1, out)
        self.assertTrue(self.report.exists(), "no JUnit file\n" + out)
        try:
            root = ET.parse(self.report).getroot()
        except ET.ParseError as e:
            self.fail("the report is not XML: %s" % e)
        return list(root.iter("testcase"))

    @staticmethod
    def text(el):
        return (el.get("message") or "") + "\n" + (el.text or "")

    def test_setupclass_error_is_reported(self):
        for args in ((), ("--jobs", "2")):
            with self.subTest(args=args):
                errors = [c.find("error") for c in self.cases(*args)
                          if c.find("error") is not None
                          and "BrokenFixtureTest" in (c.get("classname") or "") + (c.get("name") or "")]
                self.assertTrue(errors, "the class whose setUpClass raised is not in the report")
                self.assertTrue(any("PLANTED-FIXTURE-ERROR" in self.text(e) for e in errors),
                                "the error does not carry the setUpClass message")

    def test_control_characters_keep_the_report_readable(self):
        for args in ((), ("--jobs", "2")):
            with self.subTest(args=args):
                fails = [c.find("failure") for c in self.cases(*args)
                         if c.get("name") == "test_fails_with_control_characters"]
                self.assertEqual(len(fails), 1)
                self.assertIsNotNone(fails[0], "the failing test is not marked failure")
                t = self.text(fails[0])
                for part in ("PLANTED-CTRL", "red", "end"):
                    self.assertIn(part, t, "the message lost its printable part %r" % part)


if __name__ == "__main__":
    unittest.main()
