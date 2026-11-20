"""CI names say what they judge (T133; research D53), written before the names change.

Only display `name:` values: the workflow's and each job's. Job ids stay. scripts/ci-summary.py
shows each job's display name, read from the workflow file `WORKFLOW` names, and the job id when
`WORKFLOW` is unset or the job has no name.
"""
import copy
import json
import unittest

import yaml

from test_ci_scripts import REPO, SUMMARY, base_env, run, tmpdir

WF = REPO / ".github" / "workflows"

CHECK_NAME = "Tests and checks"
CHECK_JOBS = {
    "config": "Plan the run (choose jobs)",
    "layout": "Repository layout",
    "knowledge": "Documentation (knowledge bundle)",
    "markdown": "Markdown style",
    "contracts": "API contracts lint",
    "contract_diff": "API breaking changes",
    "gateway": "Model gateway config",
    "go": "Go unit tests and vet",
    "arch": "Module boundaries (go-arch-lint)",
    "docker": "Service tests with Docker (recorded model)",
    "deps": "Dependencies and licences",
    "secrets": "Secret scan",
    "py_tests": "Script and hook tests (Python)",
    "stop_points_agent_step": "Acceptance: a stopped run continues (agent-step)",
    "stop_points_procedure_step": "Acceptance: a stopped run continues (procedure-step)",
    "stop_points_review_round": "Acceptance: a stopped run continues (review-round)",
    "stop_points_version_history_commit": "Acceptance: a stopped run continues (version-history-commit)",
    "ap_test_planted_edit_judging_tests": "Acceptance: planted defects are stopped (edit-judging-tests)",
    "ap_test_planted_outside_scope": "Acceptance: planted defects are stopped (outside-scope)",
    "ap_test_planted_forbidden_dependency": "Acceptance: planted defects are stopped (forbidden-dependency)",
    "ap_test_planted_claims_done": "Acceptance: planted defects are stopped (claims-done)",
    "ap_test_planted_non_converging_review": "Acceptance: planted defects are stopped (non-converging-review)",
    "ap_test_us2": "Acceptance: Story 2, slice to accepted change",
    "e2e": "End-to-end: full stack in Compose",
    "evals-current": "Agent evals up to date",
    "summary": "Run summary",
}
EVALS_NAME = "Agent evals (paid, started by hand)"
EVALS_JOBS = {"config": "Choose eval suites"}  # `evals` is checked by prefix and expressions


def judge(workflow, name, jobs, prefix_job="evals"):
    """Violations of the names in one parsed workflow."""
    v = []
    if workflow.get("name") != name:
        v.append(f"workflow name is {workflow.get('name')!r}, want {name!r}")
    actual = workflow.get("jobs") or {}
    for jid, job in actual.items():
        if jid == prefix_job and jid not in jobs:
            n = job.get("name")
            if not (isinstance(n, str) and n.startswith("Evals: ")
                    and "matrix.suite" in n and "matrix.cases" in n):
                v.append(f"job {jid} name {n!r} must start with 'Evals: ' and use "
                         "matrix.suite and matrix.cases")
        elif jid not in jobs:
            v.append(f"job {jid} is not listed with a name")
        elif job.get("name") != jobs[jid]:
            v.append(f"job {jid} name is {job.get('name')!r}, want {jobs[jid]!r}")
    for jid in jobs:
        if jid not in actual:
            v.append(f"job {jid} is missing")
    return v


def load(name):
    return yaml.safe_load((WF / name).read_text())


class RealNamesTest(unittest.TestCase):
    def test_check_yml(self):
        self.assertEqual(judge(load("check.yml"), CHECK_NAME, CHECK_JOBS), [])

    def test_evals_yml(self):
        self.assertEqual(judge(load("evals.yml"), EVALS_NAME, EVALS_JOBS), [])

    def test_summary_step_sets_workflow(self):
        steps = load("check.yml")["jobs"]["summary"]["steps"]
        step = [s for s in steps if "ci-summary.py" in (s.get("run") or "")]
        self.assertEqual(len(step), 1)
        self.assertEqual(step[0]["env"].get("WORKFLOW"), ".github/workflows/check.yml")


class PlantedNamesTest(unittest.TestCase):
    def planted(self, mutate):
        d = copy.deepcopy(load("check.yml"))
        d["name"] = CHECK_NAME
        for jid, n in CHECK_JOBS.items():
            d["jobs"][jid]["name"] = n
        self.assertEqual(judge(d, CHECK_NAME, CHECK_JOBS), [])
        mutate(d)
        return judge(d, CHECK_NAME, CHECK_JOBS)

    def test_wrong_job_name_fails_naming_job(self):
        v = self.planted(lambda d: d["jobs"]["secrets"].update(name="Secrets"))
        self.assertTrue(any("secrets" in x for x in v), v)

    def test_missing_job_name_fails_naming_job(self):
        v = self.planted(lambda d: d["jobs"]["go"].pop("name"))
        self.assertTrue(any("go" in x for x in v), v)

    def test_new_unnamed_job_fails_naming_it(self):
        v = self.planted(lambda d: d["jobs"].update(brand_new={"runs-on": "x"}))
        self.assertTrue(any("brand_new" in x for x in v), v)

    def test_wrong_workflow_name_fails(self):
        self.assertTrue(self.planted(lambda d: d.update(name="check")))


NEEDS_WF = {
    "name": "W",
    "jobs": {
        "config": {"name": "Plan the run"},
        "good_job": {"name": "Good job name"},
        "bad_job": {"name": "Bad job name"},
        "skip_job": {"name": "Skipped job name"},
        "plain": {"runs-on": "x"},
    },
}


class SummaryNamesTest(unittest.TestCase):
    def go(self, needs, workflow=True):
        t = tmpdir(self)
        extra = {"NEEDS": json.dumps(needs)}
        if workflow:
            f = t / "wf.yml"
            f.write_text(yaml.safe_dump(NEEDS_WF))
            extra["WORKFLOW"] = str(f)
        r = run(["python3", SUMMARY], env=base_env(self, extra))
        return r.stdout

    NEEDS = {
        "config": {"result": "success", "outputs": {"jobs": json.dumps(["good_job", "bad_job",
                                                                         "plain"])}},
        "good_job": {"result": "success", "outputs": {"seconds": "3"}},
        "bad_job": {"result": "failure", "outputs": {"error": "boom"}},
        "skip_job": {"result": "skipped", "outputs": {}},
        "plain": {"result": "success", "outputs": {}},
    }

    def row(self, out, text):
        return [l for l in out.splitlines() if text in l]

    def test_rows_show_display_names(self):
        out = self.go(self.NEEDS)
        for ident, shown in (("good_job", "Good job name"), ("bad_job", "Bad job name"),
                             ("skip_job", "Skipped job name")):
            rows = self.row(out, shown)
            self.assertEqual(len(rows), 1, out)
            self.assertNotIn(ident, rows[0], out)

    def test_job_without_name_shows_id(self):
        self.assertTrue(self.row(self.go(self.NEEDS), "plain"))

    def test_without_workflow_shows_ids(self):
        out = self.go(self.NEEDS, workflow=False)
        for ident in ("good_job", "bad_job", "skip_job"):
            self.assertTrue(self.row(out, ident), out)
        self.assertFalse(self.row(out, "Good job name"))


if __name__ == "__main__":
    unittest.main()
