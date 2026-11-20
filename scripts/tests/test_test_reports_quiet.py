"""T124 (research D47, ADR 0051): a green test report stays quiet.

Written before the code, by a separate author. Every mikepenz/action-junit-report step in
.github/workflows/check.yml has `skip_success_summary: true` (a green run adds no results table)
and `require_tests: true` (a step that finds no test result fails). The judge returns violation
strings, each naming the job and the key; it is tried on the real workflow and on planted ones.
test_test_reports.py checks the report step's other keys and is not touched.
"""
import sys
import tempfile
import unittest
from pathlib import Path

import yaml

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))

import test_workflows as wfx  # noqa: E402

REPO = HERE.parent.parent
ACTION = "mikepenz/action-junit-report@"
KEYS = ("skip_success_summary", "require_tests")
EXPECTED_JOBS = {"go", "docker", "py-tests", "stop-points-agent-step", "stop-points-procedure-step",
                 "stop-points-review-round", "stop-points-version-history-commit"}


def report_jobs(wf):
    """{job id: its report steps} for every job of check.yml that has one."""
    out = {}
    for jid, j in wfx.jobs(wf["check.yml"]).items():
        steps = [s for s in (j.get("steps") or []) if str(s.get("uses", "")).startswith(ACTION)]
        if steps:
            out[jid] = steps
    return out


def judge_quiet(root):
    wf = wfx.load(root)
    v = wfx.need_files(wf, "check.yml")
    if v:
        return v
    found = report_jobs(wf)
    print("report jobs examined: %s" % sorted(found), file=sys.stderr)
    norm = {k.replace("_", "-") for k in found}
    if norm != EXPECTED_JOBS:
        v.append("report steps are in jobs %s, not exactly %s" % (sorted(found), sorted(EXPECTED_JOBS)))
    for jid, steps in found.items():
        for s in steps:
            w = s.get("with") or {}
            for key in KEYS:
                if key not in w:
                    v.append("%s job: the report step lacks %s: true" % (jid, key))
                elif str(w[key]).strip().lower() != "true":
                    v.append("%s job: the report step sets %s to %r, not true" % (jid, key, w[key]))
    return v


def report_step(**over):
    w = {"report_paths": "${{ runner.temp }}/test-reports/*.xml", "annotate_only": True,
         "detailed_summary": True, "include_passed": False,
         "skip_success_summary": True, "require_tests": True}
    for k, val in over.items():
        if val is None:
            w.pop(k, None)
        else:
            w[k] = val
    return {"name": "Test report", "if": "always()", "uses": ACTION + "a" * 40, "with": w}


def planted(tc, per_job=None):
    """A check.yml with go, docker and py-tests report steps; per_job maps a job to step overrides."""
    per_job = per_job or {}
    jobs = {}
    for name in sorted(EXPECTED_JOBS):
        jobs[name] = {"name": name, "runs-on": "ubuntu-latest",
                      "steps": [{"run": "scripts/check.sh --only " + name},
                                report_step(**per_job.get(name, {}))]}
    t = tempfile.TemporaryDirectory()
    tc.addCleanup(t.cleanup)
    d = Path(t.name) / ".github" / "workflows"
    d.mkdir(parents=True)
    (d / "check.yml").write_text(yaml.safe_dump({"name": "check", "on": {"push": None}, "jobs": jobs}))
    return t.name


class QuietReportsTest(unittest.TestCase):
    def test_real_check_yml(self):
        self.assertEqual(judge_quiet(REPO), [])

    def test_real_check_yml_examines_exactly_go_docker_py_tests(self):
        found = report_jobs(wfx.load(REPO))
        print("report jobs examined in the real check.yml: %s" % sorted(found))
        self.assertEqual({k.replace("_", "-") for k in found}, EXPECTED_JOBS)

    def test_planted_clean_passes(self):
        self.assertEqual(judge_quiet(planted(self)), [])

    def test_planted_violations_fail_naming_job_and_key(self):
        for job in sorted(EXPECTED_JOBS):
            for key in KEYS:
                for how, val in (("lacks", None), ("false", False)):
                    with self.subTest(job=job, key=key, how=how):
                        v = judge_quiet(planted(self, {job: {key: val}}))
                        self.assertTrue(any(job in m and key in m for m in v), v)
                        self.assertEqual(len(v), 1, v)

    def test_planted_other_job_untouched_is_not_blamed(self):
        v = judge_quiet(planted(self, {"go": {"require_tests": False}}))
        self.assertFalse(any("docker job" in m or "py-tests job" in m for m in v), v)

    def test_planted_missing_job_fails(self):
        root = planted(self)
        p = Path(root) / ".github" / "workflows" / "check.yml"
        data = yaml.safe_load(p.read_text())
        del data["jobs"]["docker"]
        p.write_text(yaml.safe_dump(data))
        self.assertTrue(judge_quiet(root))


if __name__ == "__main__":
    unittest.main()
