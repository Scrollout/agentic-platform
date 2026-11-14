"""T076: `ap build-workers` and the review step's witnesses (research D35, ADR 0039), with real
Docker and gVisor, written before the change by a separate author.

The review container writes under review/ (its witness tests) and nowhere else in the project;
every other step's container, generate included (its project root is writable), cannot create,
change or remove anything under review/; the skeleton build-workers makes holds review/. The
expectations are stated here, not read from the pattern, so the tests fail until the pattern and
the launcher change. The rig (a copy of the launcher, a session, the engine stand-in) is
test_build_workers.py's BuildWorkersRig; its prerequisites skip these tests the same way.
"""
import secrets
import shlex
import unittest

import test_build_workers as bw

REVIEW_FOLDERS = ("review", "review/r1")


class ReviewWitnessMountsTest(bw.BuildWorkersRig, unittest.TestCase):
    PROJECT = "notes-review"

    @classmethod
    def prepare_project(cls):
        assert not cls.project.exists()

    @classmethod
    def after_build(cls):
        cls.review_in_skeleton = (cls.project / "review").is_dir() and not (cls.project / "review").is_symlink()
        for f in REVIEW_FOLDERS:
            (cls.project / f).mkdir(parents=True, exist_ok=True)

    def try_writes(self, step):
        """In step's container, create, change, remove and make a folder in each review folder;
        return {folder: {op: done}}, judged on the host."""
        c = self.container_of(step)
        tag = "%s-%s" % (step, secrets.token_hex(3))
        lines = []
        for f in REVIEW_FOLDERS:
            d = self.project / f
            (d / ("planted-" + tag)).write_text("planted\n")
            (d / ("doomed-" + tag)).write_text("doomed\n")
            inside = self.container_path(c, d)
            self.assertIsNotNone(inside, "%s does not see %s/" % (step, f))
            q = lambda n: shlex.quote(inside.rstrip("/") + "/" + n)  # noqa: E731
            lines += ["(echo new > %s) 2>/dev/null" % q("create-" + tag),
                      "(echo more >> %s) 2>/dev/null" % q("planted-" + tag),
                      "(rm -f %s) 2>/dev/null" % q("doomed-" + tag),
                      "(mkdir %s) 2>/dev/null" % q("dir-" + tag)]
        r = self.exec(c, "; ".join(lines + ["true"]), timeout=180)
        self.assertEqual(r.returncode, 0, r.stderr)
        out = {}
        for f in REVIEW_FOLDERS:
            d = self.project / f
            out[f] = {"create": (d / ("create-" + tag)).is_file(),
                      "change": (d / ("planted-" + tag)).read_text() != "planted\n",
                      "remove": not (d / ("doomed-" + tag)).exists(),
                      "mkdir": (d / ("dir-" + tag)).is_dir()}
        return out

    def test_the_skeleton_holds_review(self):
        self.assertTrue(self.review_in_skeleton, "build-workers did not make review/ in the project folder")

    def test_the_review_container_writes_under_review(self):
        for f, ops in self.try_writes("review").items():
            for op, done in ops.items():
                self.assertTrue(done, "review cannot %s in %s/" % (op, f))

    def test_the_review_container_writes_nothing_else(self):
        before = bw.tree(self.project)
        c = self.container_of("review")
        inside = self.container_path(c, self.project)
        tag = secrets.token_hex(3)
        ops = ["(echo x > %s) 2>/dev/null" % shlex.quote("%s/%s/x-%s" % (inside, d, tag)) for d in ("internal", "tests", "gen", ".")]
        self.assertEqual(self.exec(c, "; ".join(ops + ["true"])).returncode, 0)
        self.assertEqual(bw.tree_diff(before, bw.tree(self.project)), [], "review wrote outside review/")

    def test_no_other_step_writes_under_review(self):
        for s, _ in self.steps:
            if s == "review":
                continue
            with self.subTest(step=s):
                for f, ops in self.try_writes(s).items():
                    done = [op for op, ok in ops.items() if ok]
                    self.assertEqual(done, [], "%s could %s in %s/" % (s, done, f))


if __name__ == "__main__":
    unittest.main()
