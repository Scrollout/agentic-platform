#!/usr/bin/env python3
"""Run the check tests in scripts/tests, timed, all of them or only chosen ones.

usage: python3 scripts/testrun.py [--list] [--top N] [--jobs N] [--junit FILE] [NAME ...]

Without a NAME every test in scripts/tests runs, exactly as
`python3 -m unittest discover -s scripts/tests -p 'test_*.py'` runs them.
A NAME is a module (test_hooks), a class (test_hooks.HookTest) or one test
(test_hooks.HookTest.test_x). An unknown NAME is refused before any test or
setup runs (exit 2). A run of chosen tests says it is partial, at its start and
at its end, and runs the setup of the chosen tests' classes and modules only.

Every run reports the time spent in setup (setUpModule, setUpClass, setUp),
in the tests themselves and in teardown (tearDown, tearDownClass,
tearDownModule), per module and in all, and names the slowest parts. With
--jobs N (N > 1) up to N test classes run at once, each in its own process and
each class's tests in their usual order. With --junit FILE the run also writes every
test's outcome, message and time as a JUnit XML report (research D43). Exit 0 when every test passed,
1 when one failed or errored, 2 on wrong use.
"""
import argparse
import functools
import json
import subprocess
import tempfile
from concurrent.futures import ThreadPoolExecutor
import os
import re
import sys
import time
import unittest
import xml.etree.ElementTree as ET

HERE = os.path.dirname(os.path.abspath(__file__))
TESTS = os.path.join(HERE, "tests")

timings = []  # (phase, name, seconds)
cases = []  # (classname, name, seconds, outcome or None, message): the JUnit report's test cases


class Recording(unittest.TextTestResult):
    """A text result that also keeps every test's outcome for the JUnit report."""

    def startTest(self, test):
        self._t = time.perf_counter()
        self._outcome = (None, None)
        super().startTest(test)

    def _mark(self, outcome, message):
        self._outcome = (outcome, message)

    def _fixture(self, test, outcome, message):
        # A class or module fixture's error (setUpClass, tearDownModule, ...) comes without
        # startTest or stopTest: it is a case of its own.
        name = test.id()
        cases.append((name, name, 0.0, outcome, message))

    def addFailure(self, test, err):
        super().addFailure(test, err)
        if not isinstance(test, unittest.TestCase):
            return self._fixture(test, "failure", self.failures[-1][1])
        self._mark("failure", self.failures[-1][1])

    def addError(self, test, err):
        super().addError(test, err)
        if not isinstance(test, unittest.TestCase):
            return self._fixture(test, "error", self.errors[-1][1])
        self._mark("error", self.errors[-1][1])

    def addSkip(self, test, reason):
        super().addSkip(test, reason)
        self._mark("skipped", reason)

    def addExpectedFailure(self, test, err):
        super().addExpectedFailure(test, err)

    def addUnexpectedSuccess(self, test):
        super().addUnexpectedSuccess(test)
        self._mark("failure", "unexpected success")

    def addSubTest(self, test, subtest, err):
        super().addSubTest(test, subtest, err)
        if err is not None:
            kind = "failure" if issubclass(err[0], test.failureException) else "error"
            self._mark(kind, self._exc_info_to_string(err, test))

    def stopTest(self, test):
        super().stopTest(test)
        tid = test.id()
        cls, _, name = tid.rpartition(".")
        if isinstance(test, unittest.loader._FailedTest) or not cls:
            cls, name = tid, tid
        cases.append((cls, name, time.perf_counter() - getattr(self, "_t", time.perf_counter()),
                      *self._outcome))


_NOT_XML = re.compile("[^\t\n\r\x20-\ud7ff\ue000-\ufffd\U00010000-\U0010ffff]")


def xml_text(s):
    """s without the characters XML 1.0 cannot hold (terminal colours, test2json framing)."""
    return _NOT_XML.sub("", s or "")


def write_junit(path):
    suite = ET.Element("testsuite", name="scripts/tests", tests=str(len(cases)),
                       failures=str(sum(1 for c in cases if c[3] == "failure")),
                       errors=str(sum(1 for c in cases if c[3] == "error")),
                       skipped=str(sum(1 for c in cases if c[3] == "skipped")),
                       time="%.3f" % sum(c[2] for c in cases))
    for cls, name, secs, outcome, message in cases:
        case = ET.SubElement(suite, "testcase", classname=cls, name=name, time="%.3f" % secs)
        if outcome:
            text = xml_text(message)
            first = text.strip().splitlines()
            el = ET.SubElement(case, outcome, message=first[-1] if first else "")
            el.text = text
    os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
    root = ET.Element("testsuites")
    root.append(suite)
    ET.ElementTree(root).write(path, encoding="utf-8", xml_declaration=True)


def timed(phase, name, fn):
    @functools.wraps(fn)
    def run(*a, **kw):
        t = time.perf_counter()
        try:
            return fn(*a, **kw)
        finally:
            timings.append((phase, name, time.perf_counter() - t))
    return run


def tests_of(suite):
    for t in suite:
        if isinstance(t, unittest.TestSuite):
            yield from tests_of(t)
        else:
            yield t


def instrument(suite):
    """Time every fixture and test of the suite in place."""
    classes, modules = set(), set()
    for test in tests_of(suite):
        if isinstance(test, unittest.loader._FailedTest):  # an import error: let it report itself
            continue
        cls = type(test)
        name = "%s.%s" % (cls.__module__, cls.__qualname__)
        if cls not in classes:
            classes.add(cls)
            for phase, attr in (("setup", "setUpClass"), ("teardown", "tearDownClass")):
                fn = getattr(cls, attr).__func__
                setattr(cls, attr, classmethod(timed(phase, name + "." + attr, fn)))
        mod = sys.modules.get(cls.__module__)
        if mod is not None and mod not in modules:
            modules.add(mod)
            for phase, attr in (("setup", "setUpModule"), ("teardown", "tearDownModule")):
                if hasattr(mod, attr):
                    setattr(mod, attr, timed(phase, mod.__name__ + "." + attr, getattr(mod, attr)))
        tid = test.id()
        test.setUp = timed("setup", tid + " setUp", test.setUp)
        test.tearDown = timed("teardown", tid + " tearDown", test.tearDown)
        method = test._testMethodName
        setattr(test, method, timed("test", tid, getattr(test, method)))


def resolve(loader, names):
    """Load the chosen names, refusing any that does not exist; nothing runs here."""
    suite, unknown = unittest.TestSuite(), []
    for n in names:
        try:
            s = loader.loadTestsFromName(n)
        except (ImportError, AttributeError):
            unknown.append(n)
            continue
        found = list(tests_of(s))
        if not found or any(isinstance(t, unittest.loader._FailedTest) for t in found):
            unknown.append(n)
            continue
        suite.addTest(s)
    return suite, unknown


def report(top, elapsed, partial, names):
    total = {p: sum(s for ph, _, s in timings if ph == p) for p in ("setup", "test", "teardown")}
    per = {}
    for ph, name, s in timings:
        per[name.split(".")[0]] = per.get(name.split(".")[0], 0) + s
    print("\n== time per module:")
    for m, s in sorted(per.items(), key=lambda x: -x[1]):
        print("  %7.1f s  %s" % (s, m))
    print("== time: %.1f s in all (wall clock); setup %.1f s, tests %.1f s, teardown %.1f s"
          % (elapsed, total["setup"], total["test"], total["teardown"]))
    slowest = max(total, key=total.get)
    print("== the slowest part is %s (%.0f%% of the time spent)" % (slowest, 100 * total[slowest] / max(sum(total.values()), 1e-9)))
    print("== slowest %d:" % top)
    for ph, name, s in sorted(timings, key=lambda x: -x[2])[:top]:
        print("  %7.2f s  %-8s %s" % (s, ph, name))
    if partial:
        print("== PARTIAL RUN: only %s; a change is accepted only after a full run" % " ".join(names))


def main(argv):
    ap = argparse.ArgumentParser(usage=__doc__.split("\n\n")[1])
    ap.add_argument("names", nargs="*")
    ap.add_argument("--list", action="store_true", help="print every test's name and exit")
    ap.add_argument("--top", type=int, default=15, help="how many of the slowest parts to name")
    ap.add_argument("--jobs", type=int, default=1, help="test classes run at once (default 1)")
    ap.add_argument("--junit", metavar="FILE", help="also write a JUnit XML report to FILE")
    ap.add_argument("--child", help=argparse.SUPPRESS)  # internal: write timings and result here
    a = ap.parse_args(argv)
    sys.path.insert(0, TESTS)  # as `discover -s scripts/tests` does
    loader = unittest.TestLoader()
    if a.names:
        suite, unknown = resolve(loader, a.names)
        if unknown:
            print("testrun: unknown test(s): %s; nothing was run (see --list)" % " ".join(unknown), file=sys.stderr)
            return 2
    else:
        suite = loader.discover(TESTS, pattern="test_*.py", top_level_dir=TESTS)
    if a.list:
        for t in tests_of(suite):
            print(t.id())
        return 0
    if a.names and not a.child:
        print("== PARTIAL RUN: only %s" % " ".join(a.names))
    t = time.perf_counter()
    if a.jobs > 1 and not a.child:
        ok = run_parallel(suite, a.jobs)
    else:
        instrument(suite)
        ok = unittest.TextTestRunner(verbosity=1, resultclass=Recording).run(suite).wasSuccessful()
    if a.child:
        with open(a.child, "w") as f:
            json.dump({"ok": ok, "timings": timings, "cases": cases}, f)
        return 0 if ok else 1
    report(a.top, time.perf_counter() - t, bool(a.names), a.names)
    if a.junit:
        write_junit(a.junit)
    return 0 if ok else 1


def run_parallel(suite, jobs):
    """Run each test class of the suite in its own process, up to jobs at once."""
    groups = {}
    for test in tests_of(suite):
        groups.setdefault(test.id().rsplit(".", 1)[0], []).append(test.id())
    # The classes with the most tests first, so the longest ones do not start last.
    order = sorted(groups, key=lambda m: -len(groups[m]))
    def one(module):
        with tempfile.NamedTemporaryFile(suffix=".json") as out:
            p = subprocess.run([sys.executable, os.path.abspath(__file__), "--child", out.name] + groups[module],
                               stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
            try:
                with open(out.name) as f:
                    data = json.load(f)
            except (OSError, ValueError):
                data = {"ok": False, "timings": [], "cases": [[module, module, 0.0, "error", p.stdout]]}
            return module, p.returncode == 0 and data["ok"], data["timings"], data.get("cases", []), p.stdout
    ok = True
    with ThreadPoolExecutor(jobs) as pool:
        for module, passed, t, c, out in pool.map(one, order):
            timings.extend(tuple(x) for x in t)
            cases.extend(tuple(x) for x in c)
            ok = ok and passed
            print("== %s: %s" % (module, "passed" if passed else "FAILED"))
            if not passed:
                print(out)
    return ok


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
