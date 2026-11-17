"""T098 (owner, 2026-11-17): check.yml has the job `arch`, "Module boundaries (go-arch-lint)",
running `scripts/ci-step.sh arch scripts/check.sh --only arch` as the `go` job runs its step,
with the selector's `if:`, listed in the `summary` job's `needs:`; .github/check-jobs.yaml
declares it with the same paths as `go`. judge_arch(workflow, declaration) lists violations; a
test on the real files and a planted counter-case for each rule on a modified copy."""
import unittest
from pathlib import Path

import yaml

REPO = Path(__file__).resolve().parent.parent.parent
CHECK = REPO / ".github" / "workflows" / "check.yml"
DECL = REPO / ".github" / "check-jobs.yaml"
NAME = "Module boundaries (go-arch-lint)"
RUN = "scripts/ci-step.sh arch scripts/check.sh --only arch"
IF = "contains(fromJSON(needs.config.outputs.jobs), 'arch')"


def judge_arch(workflow, decl):
    v = []
    jobs = workflow.get("jobs") or {}
    job = jobs.get("arch")
    if not isinstance(job, dict):
        v.append("check.yml has no job arch")
    else:
        if job.get("name") != NAME:
            v.append(f"arch job name is {job.get('name')!r}, want {NAME!r}")
        if job.get("if") != IF:
            v.append(f"arch job if is {job.get('if')!r}, want {IF!r}")
        if job.get("needs") != "config":
            v.append(f"arch job needs is {job.get('needs')!r}, want 'config'")
        go = jobs.get("go") or {}
        for key in ("runs-on", "timeout-minutes", "outputs"):
            if job.get(key) != go.get(key):
                v.append(f"arch job {key} differs from the go job's")
        runs = [s for s in job.get("steps") or [] if s.get("run") == RUN]
        if len(runs) != 1:
            v.append(f"arch job has {len(runs)} steps running {RUN!r}, want 1")
        elif runs[0].get("id") != "run":
            v.append("the arch job's running step has no `id: run` (its outputs read it)")
    if "arch" not in (jobs.get("summary") or {}).get("needs", []):
        v.append("the summary job's needs does not list arch")
    d = (decl.get("jobs") or {})
    if "arch" not in d:
        v.append("check-jobs.yaml does not declare arch")
    elif d["arch"] != d.get("go"):
        v.append(f"check-jobs.yaml declares arch as {d['arch']!r}, not as go's {d.get('go')!r}")
    return v


def real():
    return yaml.safe_load(CHECK.read_text()), yaml.safe_load(DECL.read_text())


class ArchJobTest(unittest.TestCase):
    def test_real_files_have_the_arch_job(self):
        wf, decl = real()
        self.assertEqual(judge_arch(wf, decl), [])

    def planted(self, mutate, want):
        wf, decl = real()
        # A counter-case counts only on a valid baseline: the real files, judged clean first.
        self.assertEqual(judge_arch(wf, decl), [], "the real files are not a valid baseline")
        mutate(wf, decl)
        v = judge_arch(wf, decl)
        self.assertTrue(any(want in x for x in v), f"want a violation with {want!r}, got {v}")

    def test_missing_job_is_caught(self):
        self.planted(lambda wf, d: wf["jobs"].pop("arch"),
                     "no job arch")

    def test_wrong_name_is_caught(self):
        self.planted(lambda wf, d: wf["jobs"]["arch"].update(name="Architecture"), "name")

    def test_wrong_run_command_is_caught(self):
        def m(wf, d):
            for s in wf["jobs"]["arch"]["steps"]:
                if s.get("run") == RUN:
                    s["run"] = "scripts/ci-step.sh arch scripts/check.sh --only go"
        self.planted(m, "steps running")

    def test_wrong_if_is_caught(self):
        self.planted(lambda wf, d: wf["jobs"]["arch"].update(
            {"if": "contains(fromJSON(needs.config.outputs.jobs), 'go')"}), "if is")

    def test_missing_if_is_caught(self):
        self.planted(lambda wf, d: wf["jobs"]["arch"].pop("if"), "if is")

    def test_not_in_summary_needs_is_caught(self):
        def m(wf, d):
            wf["jobs"]["summary"]["needs"] = [n for n in wf["jobs"]["summary"]["needs"]
                                              if n != "arch"]
        self.planted(m, "summary")

    def test_undeclared_is_caught(self):
        self.planted(lambda wf, d: d["jobs"].pop("arch"), "does not declare")

    def test_other_paths_than_go_are_caught(self):
        self.planted(lambda wf, d: d["jobs"].update(arch={"paths": ["src/**"]}), "not as go")

    def test_always_instead_of_paths_is_caught(self):
        self.planted(lambda wf, d: d["jobs"].update(arch={"always": True}), "not as go")


if __name__ == "__main__":
    unittest.main()
