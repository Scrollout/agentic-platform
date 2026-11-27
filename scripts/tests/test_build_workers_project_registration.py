"""Tests for feature 012 T004: `ap build-workers <slice> <project>` names the project in each
registration, and a second project of the slice leaves the first one's workers alone, with real Docker
and gVisor.

Judged from specs/012-run-variance/tasks.md T004, research R3, ADR 0103 (and ADR 0036),
`WorkerRegistration.project` in the engine contract and `build-workers` in src/cli/contract.md only:

- for each step of the pattern, `build-workers <slice> <project>` registers the step's container with
  the body {"endpoint": "http://<container>:8080", "project": "<project>"}, the project's full name as
  given (also the longest the contract allows, 63 characters), and nothing else;
- building workers for a second project of the same slice registers that project's workers too and
  leaves the first project's containers (ID, start time, state, labels) as they were: none of them is
  stopped, restarted or replaced;
- building the first project again registers it again with its own project name and the same
  endpoints, never with another project's, and changes no container of either project.

The engine stand-in, its request log and the cleanup are test_build_workers.py's rig, imported
unchanged. One class makes two projects (A, and B with a 63-character name), so it starts two sets of
step containers; the rig removes every Docker object at the end. Skipped with the rig's reason
without Docker, gVisor or the images. A project name is lowercase letters, digits and hyphens, so
the body needs no JSON escaping; the tests compare the parsed body, so they hold for any escaping.
"""
import json
import unittest

import test_build_workers as bw

SLICE = bw.SLICE
PROJECT_A = "notes-a"
PROJECT_B = "b" + "-long" * 12 + "-n"  # 63 characters: the longest project name
LABEL = "org.agentic-platform."


class ProjectRegistrationTest(bw.BuildWorkersRig, unittest.TestCase):
    """Project A is built by the rig; project B and then A again are built after it."""
    PROJECT = PROJECT_A

    @classmethod
    def prepare_project(cls):
        assert len(PROJECT_B) == 63, len(PROJECT_B)

    @classmethod
    def worker_containers(cls, project):
        """step -> (ID, name, Created, StartedAt, Running, labels) of the project's step containers of
        the slice, found by their labels."""
        out = {}
        for c in cls.inspect_all():
            labels = c["Config"].get("Labels") or {}
            if cls.ours(c) and labels.get(LABEL + "project") == project and labels.get(LABEL + "slice") == SLICE:
                out[labels[LABEL + "step"]] = (c["Id"], c["Name"].lstrip("/"), c["Created"],
                                               c["State"].get("StartedAt"), c["State"]["Running"], labels)
        return out

    @classmethod
    def registrations_of(cls, entries):
        """The registerWorker calls the engine accepted, in order: (step, parsed body)."""
        return [(e["grant"]["step"], json.loads(e["body"])) for e in entries
                if e.get("url", "").split("?")[0] == "/workers" and e.get("status") == 200 and "grant" in e]

    @classmethod
    def after_build(cls):
        cls.a_first = cls.worker_containers(PROJECT_A)
        cls.log_a = len(cls.log())
        cls.b_result = cls.ap("build-workers", SLICE, PROJECT_B)
        cls.log_b = len(cls.log())
        cls.a_after_b = cls.worker_containers(PROJECT_A)
        cls.b_containers = cls.worker_containers(PROJECT_B)
        cls.again_result = cls.ap("build-workers", SLICE, PROJECT_A)
        cls.log_again = len(cls.log())
        cls.a_after_again = cls.worker_containers(PROJECT_A)
        cls.b_after_again = cls.worker_containers(PROJECT_B)

    def step_names(self):
        return sorted(s for s, _ in self.steps)

    def test_each_step_is_registered_with_its_endpoint_and_its_project_in_full(self):
        regs = self.registrations_of(self.log()[:self.log_a])
        self.assertEqual(sorted(s for s, _ in regs), self.step_names(), "one registration per step of the pattern")
        self.assertEqual(sorted(self.a_first), self.step_names(), "one labelled container per step")
        for step, body in regs:
            self.assertEqual(body, {"endpoint": "http://%s:8080" % self.a_first[step][1], "project": PROJECT_A},
                             "the registration of %s is not {endpoint, project}" % step)

    def test_the_longest_project_name_is_sent_whole_for_every_step(self):
        self.assertEqual(self.b_result.returncode, 0, self.b_result.stdout + self.b_result.stderr)
        regs = self.registrations_of(self.log()[self.log_a:self.log_b])
        self.assertEqual(sorted(s for s, _ in regs), self.step_names())
        self.assertEqual(sorted(self.b_containers), self.step_names())
        for step, body in regs:
            self.assertEqual(body, {"endpoint": "http://%s:8080" % self.b_containers[step][1], "project": PROJECT_B},
                             "the 63-character project was cut or changed for %s" % step)
        for s in self.step_names():
            self.assertNotEqual(self.b_containers[s][0], self.a_first[s][0], "projects A and B share %s's container" % s)

    def test_a_second_project_leaves_the_first_projects_containers_as_they_were(self):
        self.assertEqual(self.a_after_b, self.a_first,
                         "building project B stopped, restarted, replaced or relabelled project A's containers")
        for step, v in self.a_after_b.items():
            self.assertTrue(v[4], "project A's %s container is no longer running" % step)

    def test_both_projects_registrations_were_sent_each_under_its_own_project(self):
        by_project = {}
        for step, body in self.registrations_of(self.log()[:self.log_b]):
            by_project.setdefault(body.get("project"), []).append(step)
        self.assertEqual({p: sorted(v) for p, v in by_project.items()},
                         {PROJECT_A: self.step_names(), PROJECT_B: self.step_names()})

    def test_building_the_first_project_again_registers_it_under_its_own_name_again(self):
        self.assertEqual(self.again_result.returncode, 0, self.again_result.stdout + self.again_result.stderr)
        before = {s: b["endpoint"] for s, b in self.registrations_of(self.log()[:self.log_a])}
        again = self.registrations_of(self.log()[self.log_b:self.log_again])
        self.assertEqual(sorted(s for s, _ in again), self.step_names(), "one registration per step again")
        for step, body in again:
            self.assertEqual(body, {"endpoint": before[step], "project": PROJECT_A},
                             "the second build of A sent another body for %s" % step)
        self.assertEqual(self.a_after_again, self.a_first, "building A again changed A's containers")
        self.assertEqual(self.b_after_again, self.b_containers, "building A again changed B's containers")


if __name__ == "__main__":
    unittest.main()
