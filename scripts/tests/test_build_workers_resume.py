"""Tests for T053j: `ap build-workers --resume <run>`, with real Docker and gVisor.

Judged from tasks.md T053j (cases 1 to 7), research D32, ADR 0036 and src/cli/contract.md only:
it reads the run with `getRun` for its slice, project and status; refuses a run that is not
`interrupted` (exit 4 RunNotInterrupted), an unknown run (exit 4 NotFound) and, by the active-run
rule, another run of the slice that has not ended (exit 4 RunActive); accepts the existing project
folder only of that run, never one from the command line, and creates nothing in it (FR-025); refuses
a missing or non-folder project folder, `.git` or step folder with exit 3, naming it; makes the
slice's tokens if they are gone, writes the grants, starts the project's step containers with the
T053g names, labels and mounts and registers them; running it twice changes nothing; an engine
answer that is missing, not 200 or cannot be read fails closed with exit 1.

Rig and seams: test_build_workers.py's rig and test_build_workers_projects.py's stand-in engine and
terminal client stand-in, imported unchanged (see that file's docstring for control.json).

`ap down` and `ap up` are simulated, not run: `ap down` stops the whole Compose project (also the
engine stand-in) and `ap up` needs the signed images and the registry. After a real `build-workers`
for the run's project and the run's files and history written into it (as the project worker
writes them), the test does what `ap down` does to the step workers: removes the step containers,
the session's worker tokens folder and every grant file (as the engine's user). The session file,
the grants folder and the engine stand-in stay, as after `ap up` with the same paths.

Left to a real engine and project worker (T046 acceptance, T024 harness), not pinned here: that
`ap resume <run>` then continues the run without repeating an accepted step (case 1's last part),
and that a `.git` changed while the platform was down stops the resumed run with HistoryChanged
(case 6, the project worker's guard, D30). Here case 1 pins what the launcher does (getRun, the
rebuilt containers, grants and registrations, then `resume` registering them and passing the command
on), and case 6 pins that the rebuild leaves the changed `.git` exactly as it is and mounts it
read-only in every container, so the project worker sees the change.
"""
import hashlib
import json
import os
import secrets
import shutil
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

import test_build_workers as bw
import test_build_workers_projects as bwp

SLICE = bw.SLICE
OTHER = "S-5"  # a slice with no tokens, grants or containers: the refusals must not make any
PROJECT = "notes-r"
RUN = "RUN-40"
TEST_LABEL = bwp.TEST_LABEL
run_record = bwp.run_record
_T = bwp.ProjectStepContainersTest.__dict__


def full_tree(top):
    """bw.tree plus every entry's modification time and the top folder's, so a file made and removed
    again, or a folder touched, shows."""
    out = {k: v + (os.lstat(Path(top) / k).st_mtime_ns,) for k, v in bw.tree(top).items()}
    out["."] = ("dir", stat.S_IMODE(os.lstat(top).st_mode), os.lstat(top).st_mtime_ns)
    return out


class BuildWorkersResumeTest(bw.BuildWorkersRig, unittest.TestCase):
    """One project built, its run interrupted by a simulated `ap down`, then
    `ap build-workers --resume RUN-40`."""
    PROJECT = PROJECT

    # the T053g rig's helpers, taken as they are
    rewrite_session = _T["rewrite_session"]
    make_cli_stand_in = _T["make_cli_stand_in"]
    start_engine_stand_in = _T["start_engine_stand_in"]
    set_control = _T["set_control"]
    cname = _T["cname"]
    inspect_name = _T["inspect_name"]
    container_state = _T["container_state"]
    grant_state = _T["grant_state"]
    assertNothingChanged = _T["assertNothingChanged"]
    new_entries = _T["new_entries"]
    tokens = _T["tokens"]
    assertRunsRead = _T["assertRunsRead"]

    @classmethod
    def ours(cls, c):
        if (c["Config"].get("Labels") or {}).get(TEST_LABEL) == cls.name:
            return True
        return bw.BuildWorkersRig.ours.__func__(cls, c)

    @classmethod
    def cleanup(cls):
        try:
            bw.BuildWorkersRig.cleanup.__func__(cls)
        finally:
            if getattr(cls, "cli_image", None):
                bw.docker("image", "rm", "-f", cls.cli_image)

    # setup
    @classmethod
    def setUpClass(cls):
        why = bw.missing_prerequisite()
        if not why and bw.docker("image", "inspect", bw.lock_pin("alpine")).returncode != 0:
            why = "the alpine pin is not in the local image store"
        if why:
            raise unittest.SkipTest(why)
        cls.base = Path(tempfile.mkdtemp(prefix="test-bw-resume-", dir=bw.TMP_BASE)).resolve()
        cls.name = "ap-test-bwr-" + secrets.token_hex(4)
        cls.cli_image = None
        cls.addClassCleanup(cls.cleanup)
        cls.steps = bw.pattern_steps()
        cls.step_names = [s for s, _ in cls.steps]
        cls.step_folders = sorted(set().union(*(bw.writable_folders(e)[1] for _, e in cls.steps)))
        cls.make_copy()
        cls.make_session()
        cls.make_cli_stand_in()
        cls.start_engine_stand_in()
        cls.workers = cls.state / "agentic-platform/workers"
        cls.tokfile = cls.workers / SLICE
        cls.quiet_runs = [run_record("RUN-1", SLICE, "older", "ended"), run_record("RUN-2", "S-2", "other", "running")]

        # the run's start: build-workers for its new project, then the run writes files and history
        cls.set_control(runs=cls.quiet_runs, page_size=1)
        cls.first = cls.ap("build-workers", SLICE, PROJECT)
        if cls.first.returncode != 0:
            raise AssertionError("ap build-workers %s %s exited %d:\n%s" % (SLICE, PROJECT, cls.first.returncode, cls.first.stderr))
        cls.first_mounts = {}
        for s in cls.step_names:
            c = cls.inspect_name(cls.cname(s, PROJECT))
            if c is None:
                raise AssertionError("no T053g container %s after build-workers" % cls.cname(s, PROJECT))
            cls.first_mounts[s] = sorted((m.get("Type"), m.get("Source"), m.get("Destination"), bool(m.get("RW")))
                                         for m in c.get("Mounts") or [])
        p = cls.project
        (p / "api/openapi.yaml").write_text("openapi: 3.1.0\ninfo: {title: notes, version: '1'}\npaths: {}\n")
        (p / "features/slice.feature").write_text("Feature: notes\n")
        cls.git("init", "-q", "-b", "main")
        cls.git("add", "--", "api/openapi.yaml", "features/slice.feature")
        cls.git("commit", "-q", "-m", "Step contract-check accepted in " + RUN)
        head = cls.git("rev-parse", "HEAD").strip()

        # `ap down`, as it treats the step workers
        for s in cls.step_names:
            bw.docker("rm", "-f", cls.cname(s, PROJECT))
        shutil.rmtree(cls.workers)
        r = bw.docker("run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
                      "--security-opt", "no-new-privileges", "--user", cls.grants_owner, "-v",
                      "%s:/g" % cls.values["AP_GRANTS_DIR"], "--entrypoint", "find", bw.lock_pin("alpine"),
                      "/g", "-mindepth", "1", "-delete")
        if r.returncode != 0:
            raise AssertionError("cannot remove the grants as ap down does: %s" % r.stderr)
        # while the platform is down, the developer changes the history (case 6)
        (p / ".git/refs/heads/changed-while-down").write_text(head + "\n")

        # the rebuild
        cls.set_control(runs=cls.quiet_runs + [run_record(RUN, SLICE, PROJECT, "interrupted")], page_size=1, cli_exit=0)
        cls.before_tree = full_tree(cls.projects)
        cls.before_log = len(cls.log())
        cls.rebuild = cls.ap("build-workers", "--resume", RUN)
        cls.after_log = len(cls.log())
        cls.after_tree = full_tree(cls.projects)

    @classmethod
    def git(cls, *args):
        env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
        env.update(GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL="/dev/null", HOME=str(cls.base),
                   GIT_AUTHOR_NAME="agentic-platform", GIT_AUTHOR_EMAIL="noreply@localhost",
                   GIT_COMMITTER_NAME="agentic-platform", GIT_COMMITTER_EMAIL="noreply@localhost",
                   GIT_AUTHOR_DATE="2026-11-14T00:00:00Z", GIT_COMMITTER_DATE="2026-11-14T00:00:00Z")
        r = subprocess.run(["git", "-C", str(cls.project), *args], env=env, capture_output=True, text=True, timeout=60)
        if r.returncode != 0:
            raise AssertionError("git %s: %s" % (" ".join(args), r.stderr))
        return r.stdout

    # snapshots: every slice's tokens file, so no refusal makes another slice's tokens
    def snapshot(self):
        cs = sorted((c["Id"], c["Name"], c["State"].get("StartedAt"), c["State"]["Running"],
                     json.dumps(c["Config"].get("Labels") or {}, sort_keys=True))
                    for c in self.inspect_all() if self.ours(c) and c["Id"] != self.engine)
        toks = {f.name: f.read_bytes() for f in self.workers.iterdir()} if self.workers.is_dir() else None
        return {"containers": cs, "grants": self.grant_state(), "projects": full_tree(self.projects),
                "tokens": toks, "log": len(self.log())}

    def require_rebuilt(self):
        r = self.rebuild
        self.assertEqual(r.returncode, 0, "ap build-workers --resume %s exited %d:\n%s" % (RUN, r.returncode, r.stderr))

    def valid_folder(self, project):
        """A project folder as a run leaves it: every step folder, a `.git` with content, a file."""
        p = self.projects / project
        for f in self.step_folders:
            (p / f).mkdir(parents=True, exist_ok=True)
        (p / ".git").mkdir(exist_ok=True)
        (p / ".git/HEAD").write_text("ref: refs/heads/main\n")
        (p / "api/openapi.yaml").write_text("openapi: 3.1.0\n")
        return p

    # (1) the rebuild
    def test_1a_it_reads_the_run_starts_the_projects_containers_writes_the_grants_and_registers_them(self):
        self.require_rebuilt()
        new = self.log()[self.before_log:self.after_log]
        self.assertRunsRead(new, "/runs/" + RUN)
        self.assertTrue(self.tokfile.is_file(), "the slice's tokens were not made again")
        toks = self.tokens()
        regs = {e["grant"]["step"]: e for e in new if e.get("path") == "/workers" and e.get("status") == 200}
        self.assertEqual(sorted(regs), sorted(self.step_names), "not every step was registered")
        grants = self.grant_state()
        self.assertEqual(len(grants), len(self.step_names), "grant files: %s" % sorted(grants))
        for s in self.step_names:
            name = self.cname(s, PROJECT)
            with self.subTest(step=s):
                c = self.inspect_name(name)
                self.assertIsNotNone(c, "no container %s" % name)
                self.assertTrue(c["State"]["Running"], c["State"])
                self.assertEqual(c["HostConfig"].get("Runtime"), "runsc")
                labels = c["Config"].get("Labels") or {}
                self.assertEqual((labels.get("org.agentic-platform.slice"), labels.get("org.agentic-platform.step"),
                                  labels.get("org.agentic-platform.project")), (SLICE, s, PROJECT))
                mounts = sorted((m.get("Type"), m.get("Source"), m.get("Destination"), bool(m.get("RW")))
                                for m in c.get("Mounts") or [])
                self.assertEqual(mounts, self.first_mounts[s], "the rebuilt container's mounts differ from build-workers'")
                e = regs[s]
                self.assertEqual(json.loads(e["body"])["endpoint"], "http://%s:8080" % name)
                self.assertEqual(e["token"], toks[s][0], "registered without the new tokens file's worker token")
                g = grants.get(hashlib.sha256(e["token"].encode()).hexdigest())
                self.assertIsNotNone(g, "no grant file for %s's token" % s)
                self.assertEqual(json.loads(g["content"]), {"slice": SLICE, "step": s, "call_token": toks[s][1]})
                self.assertEqual(g["mode"] & 0o7777, 0o600)
                self.assertIn(toks[s][1], [x.split("=", 1)[1] for x in c["Config"]["Env"] if "=" in x],
                              "the container does not hold its call token")
        self.assertEqual([e for e in new if e.get("path") == "/__cli"], [], "build-workers --resume passed a command on")

    def test_1b_then_resume_registers_the_rebuilt_containers_and_passes_the_command_on(self):
        self.require_rebuilt()
        since = len(self.log())
        r = self.ap("resume", RUN)
        self.assertEqual(r.returncode, 0, "exit %d: %s" % (r.returncode, r.stderr))
        new = self.new_entries(since)
        regs = [(i, e) for i, e in enumerate(new) if e.get("path") == "/workers" and e.get("status") == 200]
        self.assertEqual(sorted(e["grant"]["step"] for _, e in regs), sorted(self.step_names))
        for _, e in regs:
            self.assertEqual(json.loads(e["body"])["endpoint"], "http://%s:8080" % self.cname(e["grant"]["step"], PROJECT))
        cli = [(i, e) for i, e in enumerate(new) if e.get("path") == "/__cli"]
        self.assertEqual([e["cli"] for _, e in cli], [["resume", RUN]])
        self.assertTrue(all(i < cli[0][0] for i, _ in regs))

    # (2) only the run's folder, and nothing in it changes
    def test_2_it_accepts_only_the_runs_folder_and_creates_changes_or_removes_nothing_in_it(self):
        self.require_rebuilt()
        self.assertEqual([k for k in set(self.before_tree) | set(self.after_tree)
                          if self.before_tree.get(k) != self.after_tree.get(k)], [],
                         "build-workers --resume changed the projects folder (FR-025)")
        # the project comes only from the run: a project on the command line is a usage error
        for args in (("--resume", RUN, PROJECT), ("--resume", RUN, "elsewhere"), ("--resume",), ("--resume", "S-1"),
                     ("--resume", "notes-r")):
            with self.subTest(args=args):
                before = self.snapshot()
                r = self.ap("build-workers", *args)
                self.assertEqual(r.returncode, 3, "exit %d: %s" % (r.returncode, r.stderr))
                self.assertNothingChanged(before, "build-workers %s" % " ".join(args))
                self.assertFalse((self.projects / "elsewhere").exists())

    # (3) a missing or non-folder project folder, .git or step folder
    def test_3_a_missing_or_non_folder_project_git_or_step_folder_is_refused_naming_it(self):
        def missing(p):
            shutil.rmtree(p)

        cases = []
        cases.append(("no project folder", "gone-" + secrets.token_hex(2), missing, ""))
        cases.append(("the project folder is a file", "file-" + secrets.token_hex(2),
                      lambda p: (shutil.rmtree(p), p.write_text("x\n")), ""))
        cases.append(("the project folder is a symbolic link", "link-" + secrets.token_hex(2),
                      lambda p: (shutil.rmtree(p), self.valid_folder("target-" + p.name), p.symlink_to("target-" + p.name)), ""))
        cases.append(("no .git", "nogit-" + secrets.token_hex(2), lambda p: shutil.rmtree(p / ".git"), ".git"))
        cases.append(("a .git that is a file", "gitfile-" + secrets.token_hex(2),
                      lambda p: (shutil.rmtree(p / ".git"), (p / ".git").write_text("gitdir: /elsewhere\n")), ".git"))
        first, nested = self.step_folders[0], max(self.step_folders, key=lambda f: f.count("/"))
        cases.append(("no step folder %s" % first, "nostep-" + secrets.token_hex(2), lambda p: shutil.rmtree(p / first), first))
        cases.append(("no nested step folder %s" % nested, "nonest-" + secrets.token_hex(2),
                      lambda p: shutil.rmtree(p / nested), nested))
        last = self.step_folders[-1]
        cases.append(("a step folder %s that is a file" % last, "stepfile-" + secrets.token_hex(2),
                      lambda p: (shutil.rmtree(p / last), (p / last).write_text("x\n")), last))
        cases.append(("a step folder %s that is a symbolic link" % last, "steplink-" + secrets.token_hex(2),
                      lambda p: (shutil.rmtree(p / last), (p / last).symlink_to("api")), last))
        for i, (label, project, breaks, rel) in enumerate(cases):
            with self.subTest(label):
                p = self.valid_folder(project)
                breaks(p)
                run = "RUN-%d" % (50 + i)
                self.set_control(runs=[run_record(run, OTHER, project, "interrupted")], cli_exit=0)
                before = self.snapshot()
                r = self.ap("build-workers", "--resume", run)
                self.assertEqual(r.returncode, 3, "exit %d: %s" % (r.returncode, r.stderr))
                path = p / rel if rel else p
                self.assertTrue(str(path) in r.stderr or (project in r.stderr and (rel or project) in r.stderr),
                                "the refusal does not name %s: %s" % (path, r.stderr))
                new = self.assertNothingChanged(before, label)
                self.assertRunsRead(new, "/runs/" + run)

    # (4) not interrupted, unknown, or another active run of the slice
    def test_4_a_run_that_is_not_interrupted_or_unknown_or_beside_an_active_run_is_refused(self):
        project = "valid-" + secrets.token_hex(2)
        self.valid_folder(project)
        cases = [("running", [run_record("RUN-60", OTHER, project, "running")], "RUN-60", "RunNotInterrupted"),
                 ("waiting", [run_record("RUN-60", OTHER, project, "waiting-for-developer")], "RUN-60", "RunNotInterrupted"),
                 ("ended", [run_record("RUN-60", OTHER, project, "ended")], "RUN-60", "RunNotInterrupted"),
                 ("unknown", [run_record("RUN-60", OTHER, project, "interrupted")], "RUN-61", "NotFound"),
                 ("another active run", [run_record("RUN-60", OTHER, project, "interrupted"),
                                         run_record("RUN-62", "S-2", "x", "ended"),
                                         run_record("RUN-63", OTHER, "notes-other", "running")], "RUN-60", "RunActive")]
        for label, runs, run, code in cases:
            with self.subTest(label):
                self.set_control(runs=runs, page_size=1, cli_exit=0)
                before = self.snapshot()
                r = self.ap("build-workers", "--resume", run)
                self.assertEqual(r.returncode, 4, "exit %d: %s" % (r.returncode, r.stderr))
                self.assertIn(code, r.stderr)
                new = self.assertNothingChanged(before, label)
                self.assertRunsRead(new, "/runs/" + run)
                if code == "RunActive":
                    self.assertIn("RUN-63", r.stderr)

    # (5) twice
    def test_5_running_it_twice_changes_nothing(self):
        self.require_rebuilt()
        self.set_control(runs=self.quiet_runs + [run_record(RUN, SLICE, PROJECT, "interrupted")], page_size=1, cli_exit=0)
        before = self.snapshot()
        old = {}
        for e in self.log()[self.before_log:self.after_log]:
            if e.get("path") == "/workers" and e.get("status") == 200:
                old[e["grant"]["step"]] = (e["token"], json.loads(e["body"])["endpoint"])
        r = self.ap("build-workers", "--resume", RUN)
        self.assertEqual(r.returncode, 0, "exit %d: %s" % (r.returncode, r.stderr))
        after = self.snapshot()
        self.assertEqual(after["containers"], before["containers"], "containers were replaced or restarted")
        self.assertEqual(after["grants"], before["grants"], "grant files were written again")
        self.assertEqual(after["tokens"], before["tokens"], "the tokens changed")
        self.assertEqual([k for k in set(before["projects"]) | set(after["projects"])
                          if before["projects"].get(k) != after["projects"].get(k)], [], "the projects folder changed")
        for e in self.log()[before["log"]:]:
            if e.get("path") == "/workers" and e.get("status") == 200:
                self.assertEqual((e["token"], json.loads(e["body"])["endpoint"]), old.get(e["grant"]["step"]),
                                 "the second run registered %s with another token or endpoint" % e["grant"]["step"])

    # (6) the launcher's part of the history guard
    def test_6_a_git_changed_while_down_is_left_as_it_is_and_mounted_read_only(self):
        self.require_rebuilt()
        ref = self.project / ".git/refs/heads/changed-while-down"
        self.assertTrue(ref.is_file(), "the rebuild removed the change made to .git while the platform was down")
        git_host = str(self.project / ".git")
        for s in self.step_names:
            c = self.inspect_name(self.cname(s, PROJECT))
            with self.subTest(step=s):
                self.assertIsNotNone(c)
                gm = [m for m in c.get("Mounts") or [] if m.get("Source") == git_host]
                self.assertTrue(gm, "%s does not mount .git" % s)
                self.assertTrue(all(not m.get("RW") for m in gm), "%s mounts .git writable" % s)

    # (7) fail closed
    def test_7_a_get_run_or_list_runs_answer_that_is_missing_not_200_or_unreadable_fails_closed(self):
        project = "closed-" + secrets.token_hex(2)
        self.valid_folder(project)
        good = run_record("RUN-70", OTHER, project, "interrupted")
        cases = [("get %s" % m, dict(runs=[good], get_fault=m)) for m in ("close", 500, 503, "truncated", "not-json")]
        for label, change in (("no slice", {"slice": None}), ("a slice that is no slice ID", {"slice": "S-5; true"}),
                              ("no project", {"project": None}), ("a project that is no name", {"project": "../escape"}),
                              ("no status", {"status": None})):
            bad = {k: v for k, v in dict(good, **change).items() if v is not None}
            cases.append((label, dict(runs=[bad])))
        for m in ("close", 500, "truncated"):
            cases.append(("list %s" % m, dict(runs=[good], list_fault={"page": 0, "mode": m})))
        for label, ctl in cases:
            with self.subTest(label):
                self.set_control(cli_exit=0, **ctl)
                before = self.snapshot()
                r = self.ap("build-workers", "--resume", "RUN-70")
                self.assertEqual(r.returncode, 1, "exit %d: %s" % (r.returncode, r.stderr))
                self.assertNothingChanged(before, label)
                self.assertFalse((self.projects / "escape").exists() or (self.base / "escape").exists())


if __name__ == "__main__":
    unittest.main()
