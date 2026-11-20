"""T129 (research D49, ADR 0052): a green test job writes no summary at all.

Written before the code, by a separate author. Every mikepenz/action-junit-report step in
.github/workflows/check.yml has `job_summary: ${{ job.status != 'success' }}` (compared after
stripping surrounding whitespace). The judge returns violation strings, each naming the job; it is
tried on the real workflow and on planted ones. Other report keys are checked by
test_test_reports.py and test_test_reports_quiet.py, which are not touched.
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
WANT = "${{ job.status != 'success' }}"
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


def judge_summary(root):
    wf = wfx.load(root)
    v = wfx.need_files(wf, "check.yml")
    if v:
        return v
    found = report_jobs(wf)
    print("report jobs examined: %s" % sorted(found), file=sys.stderr)
    if {k.replace("_", "-") for k in found} != EXPECTED_JOBS:
        v.append("report steps are in jobs %s, not exactly %s" % (sorted(found), sorted(EXPECTED_JOBS)))
    for jid, steps in found.items():
        for s in steps:
            w = s.get("with") or {}
            if "job_summary" not in w:
                v.append("%s job: the report step lacks job_summary: %s" % (jid, WANT))
            elif str(w["job_summary"]).strip() != WANT:
                v.append("%s job: the report step sets job_summary to %r, not %s" % (jid, w["job_summary"], WANT))
    return v


def report_step(**over):
    w = {"report_paths": "${{ runner.temp }}/test-reports/*.xml", "annotate_only": True,
         "skip_success_summary": True, "require_tests": True, "job_summary": WANT}
    for k, val in over.items():
        if val is None:
            w.pop(k, None)
        else:
            w[k] = val
    return {"name": "Test report", "if": "always()", "uses": ACTION + "a" * 40, "with": w}


def planted(tc, per_job=None):
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


class SummaryOnFailureTest(unittest.TestCase):
    def test_real_check_yml(self):
        self.assertEqual(judge_summary(REPO), [])

    def test_real_check_yml_examines_exactly_go_docker_py_tests(self):
        found = report_jobs(wfx.load(REPO))
        print("report jobs examined in the real check.yml: %s" % sorted(found))
        self.assertEqual({k.replace("_", "-") for k in found}, EXPECTED_JOBS)

    def test_planted_clean_passes(self):
        self.assertEqual(judge_summary(planted(self)), [])

    def test_planted_whitespace_around_value_passes(self):
        self.assertEqual(judge_summary(planted(self, {"go": {"job_summary": "  " + WANT + "\n"}})), [])

    def test_planted_violations_fail_naming_job(self):
        bad = (("lacks", None), ("true", True), ("failure", "${{ failure() }}"),
               ("always", "${{ always() }}"), ("inverted", "${{ job.status == 'success' }}"))
        for job in sorted(EXPECTED_JOBS):
            for how, val in bad:
                with self.subTest(job=job, how=how):
                    v = judge_summary(planted(self, {job: {"job_summary": val}}))
                    self.assertTrue(any(job in m and "job_summary" in m for m in v), v)
                    self.assertEqual(len(v), 1, v)

    def test_planted_other_job_untouched_is_not_blamed(self):
        v = judge_summary(planted(self, {"go": {"job_summary": True}}))
        self.assertFalse(any("docker job" in m or "py-tests job" in m for m in v), v)

    def test_planted_missing_job_fails(self):
        root = planted(self)
        p = Path(root) / ".github" / "workflows" / "check.yml"
        data = yaml.safe_load(p.read_text())
        del data["jobs"]["docker"]
        p.write_text(yaml.safe_dump(data))
        self.assertTrue(judge_summary(root))


if __name__ == "__main__":
    unittest.main()
