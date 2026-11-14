"""Tests for T053g: step containers per project, and the set-up of `ap resume` and `ap replay`,
with real Docker and gVisor.

Judged from tasks.md T053g (cases 1 to 8), research D32, ADR 0036 and src/cli/contract.md only:

- a step container is named `<compose project>-worker-<slice, lowercase>-<step>-<p>`, `<p>` the first
  12 hex digits of the SHA-256 of the project name, and labelled org.agentic-platform.slice, .step and
  .project with the slice, step and project in full; each project has its own containers, and a
  container of the wanted name labelled with another project is refused with exit 1;
- `build-workers` refuses while a run of the slice has not ended (exit 4, RunActive), found with
  `listRuns` (every page) and the session's client token, before it changes anything;
- `resume <run>` reads the run with `getRun`; for an interrupted run whose containers run it registers
  them again and passes the command on, without `build-workers`; when they are gone, or the slice's
  tokens are, it exits 3 with the contract's message; a run that is not interrupted is passed on;
- `replay <run> <project>` reads the run with `getRun` for its slice and runs `build-workers` for it;
- an unknown run is exit 4 NotFound; an engine answer that is missing, not 200 or cannot be read is
  exit 1, changing nothing.

Seams (the rest is test_build_workers.py's rig, imported unchanged):

- The engine stand-in is the copy's `engine` service running fake-engine.js from this file. Besides
  registerWorker (the owner's grant rule, as in test_build_workers.py) it serves `GET /runs` (pages of
  `page_size`, `next_page_token` `page-<n>`) and `GET /runs/{id}` from runs the test chooses, only
  for the session's client token (ENGINE_CLIENT_TOKEN) and the API version; else 401 or 400. The test
  steers it through control.json in its log folder, read at each request: `runs`, `page_size`,
  `list_fault` ({page, mode}) and `get_fault` (mode), where a mode is `close` (no answer), a status
  code, `truncated`, `not-json` or `no-items`; `unknown_slices` (registerWorker answers 404 NotFound
  for a grant of such a slice) and `cli_exit`. Every request is logged.
- The terminal client is a stand-in image made by this test (`docker commit` of the alpine pin) and
  set as the session's AP_IMAGE_CLI: it posts its arguments, one per line, with its ENGINE_TOKEN to
  the stand-in's `POST /__cli` (logged) and exits with the number the stand-in answers (`cli_exit`).
  So "the command is passed on" is a logged /__cli request, ordered against the registrations.
- "Changes nothing": the test's containers (ID, start time, state, labels), the grant files (content,
  mode, inode, modification time, read inside the stand-in as the engine's user), the projects folder
  tree, the slice's tokens file, and no registerWorker or client request.

Steps run in one class sharing three built projects (A, B with a 63-character name, and replay's D),
so the class starts 15 gVisor containers; every Docker object it makes is removed at the end.
"""
import hashlib
import json
import os
import secrets
import shutil
import subprocess
import tempfile
import time
import unittest
from pathlib import Path
from urllib.parse import parse_qs, urlparse

import test_build_workers as bw

SLICE = bw.SLICE
API_VERSION = bw.API_VERSION
PROJECT_A = "notes-a"
PROJECT_B = "b" + "-long" * 12 + "-n"  # 63 characters: the longest project name
TEST_LABEL = "org.agentic-platform.test"

FAKE_ENGINE_JS = r"""'use strict';
// The engine stand-in of test_build_workers_projects.py: registerWorker by the owner's grant rule,
// listRuns and getRun from control.json, and the terminal client stand-in's /__cli; every request is
// logged as one JSON line in $FAKE_LOG.
const http = require('http'), fs = require('fs'), crypto = require('crypto'), path = require('path');
const LOG = process.env.FAKE_LOG, CTL = process.env.FAKE_CONTROL, DIR = process.env.ENGINE_GRANTS_DIR || '';
const CLIENT = process.env.ENGINE_CLIENT_TOKEN || '', API = process.env.FAKE_API_VERSION;
function log(o) { fs.appendFileSync(LOG, JSON.stringify(o) + '\n'); }
function control() { try { return JSON.parse(fs.readFileSync(CTL, 'utf8')); } catch (e) { return {}; } }
function grantOf(token) {
  if (!DIR || !token) return {};
  const p = path.join(DIR, crypto.createHash('sha256').update(token).digest('hex'));
  let text;
  try { text = fs.readFileSync(p, 'utf8'); } catch (e) { return e.code === 'ENOENT' ? {} : { err: e.code || String(e) }; }
  try {
    const g = JSON.parse(text);
    if (g && typeof g.slice === 'string' && g.slice && typeof g.step === 'string' && g.step &&
        typeof g.call_token === 'string') return { grant: g };
  } catch (e) {}
  return { err: 'malformed' };
}
function problem(status, code) { return { type: 'about:blank', title: code, status, code }; }
// Answers with a fault mode, or returns false when there is none.
function fault(res, req, mode, entry) {
  if (!mode) return false;
  entry.fault = mode; entry.status = 'fault:' + mode; log(entry);
  if (mode === 'close') { req.socket.destroy(); return true; }
  if (typeof mode === 'number') {
    res.writeHead(mode, { 'Content-Type': 'application/problem+json' });
    res.end(JSON.stringify(problem(mode, mode >= 500 ? 'Internal' : 'InvalidRequest'))); return true;
  }
  res.writeHead(200, { 'Content-Type': 'application/json' });
  if (mode === 'truncated') res.end('{"items":[{"id":"RUN-1","slice":"S-1","proj');
  else if (mode === 'not-json') res.end('<html><body>gateway</body></html>');
  else if (mode === 'no-items') res.end('{"runs":[]}');
  else res.end('');
  return true;
}
http.createServer((req, res) => {
  let body = '';
  req.on('data', c => { body += c; });
  req.on('end', () => {
    const m = /^Bearer (.+)$/i.exec(req.headers['authorization'] || '');
    const token = m ? m[1] : '';
    const u = new URL(req.url, 'http://engine');
    const p = u.pathname, ctl = control();
    const entry = { method: req.method, url: req.url, path: p, token, body };
    let status = 404, out = problem(404, 'NotFound');
    if (req.method === 'POST' && p === '/__cli') {
      entry.cli = body === '' ? [] : body.split('\n'); entry.status = 200; log(entry);
      res.writeHead(200, { 'Content-Type': 'text/plain' }); res.end(String(ctl.cli_exit || 0)); return;
    }
    if (req.method === 'POST' && p === '/workers') {
      const found = grantOf(token);
      entry.err = found.err;
      if (!found.grant) {
        status = 401; out = problem(401, 'Unauthorized');
      } else if ((ctl.unknown_slices || []).includes(found.grant.slice)) {
        status = 404; out = problem(404, 'NotFound'); entry.grant = found.grant;
      } else {
        let ep = '';
        try { ep = JSON.parse(body).endpoint; } catch (e) {}
        if (typeof ep !== 'string' || !ep) { status = 400; out = problem(400, 'InvalidRequest'); }
        else { status = 200; entry.grant = found.grant; out = { slice: found.grant.slice, step: found.grant.step, endpoint: ep }; }
      }
    } else if (req.method === 'GET' && (p === '/runs' || p.startsWith('/runs/'))) {
      const runs = ctl.runs || [];
      if (!CLIENT || token !== CLIENT) { status = 401; out = problem(401, 'Unauthorized'); }
      else if (u.searchParams.get('api-version') !== API) { status = 400; out = problem(400, 'MissingApiVersionParameter'); }
      else if (p === '/runs') {
        const size = ctl.page_size || 50, pt = u.searchParams.get('page_token');
        let page = 0;
        if (pt !== null) page = /^page-[1-9][0-9]*$/.test(pt) ? parseInt(pt.slice(5), 10) : -1;
        entry.page = page;
        if (page < 0 || (page > 0 && page * size >= runs.length)) { status = 400; out = problem(400, 'InvalidRequest'); }
        else {
          const f = ctl.list_fault;
          if (f && f.page === page && fault(res, req, f.mode, entry)) return;
          status = 200; out = { items: runs.slice(page * size, (page + 1) * size) };
          if ((page + 1) * size < runs.length) out.next_page_token = 'page-' + (page + 1);
        }
      } else {
        if (fault(res, req, ctl.get_fault, entry)) return;
        const id = decodeURIComponent(p.slice('/runs/'.length));
        const run = runs.find(r => r.id === id);
        if (run) { status = 200; out = run; }
      }
    }
    entry.status = status;
    log(entry);
    res.writeHead(status, { 'Content-Type': status === 200 ? 'application/json' : 'application/problem+json' });
    res.end(JSON.stringify(out));
  });
}).listen(8080, '0.0.0.0', () => log({ ready: true, grants_dir: DIR }));
"""

# The terminal client stand-in: its arguments, one per line, to the engine stand-in; exits as told.
FAKE_CLI_SH = r"""#!/bin/sh
out=$(wget -q -O - -T 30 --header "Authorization: Bearer ${ENGINE_TOKEN:-}" \
  --post-data "$(printf '%s\n' "$@")" "${ENGINE_URL:-http://engine:8080}/__cli") ||
  { echo "fake cli: the engine stand-in did not answer" >&2; exit 99; }
case $out in '' | *[!0-9]*) exit 98 ;; esac
exit "$out"
"""

# The grants folder inside the engine stand-in, as the engine's user, with what shows a rewrite.
READ_GRANTS_JS = (
    "const fs=require('fs'),p=require('path'),d=process.env.ENGINE_GRANTS_DIR,o={};"
    "for(const n of fs.readdirSync(d)){const f=p.join(d,n),s=fs.lstatSync(f);"
    "o[n]={mode:s.mode,uid:s.uid,ino:s.ino,mtime:s.mtimeMs,file:s.isFile(),"
    "content:s.isFile()?fs.readFileSync(f,'utf8'):null}}"
    "process.stdout.write(JSON.stringify(o))")

T0 = "2026-11-14T10:00:00Z"


def run_record(rid, slice_, project, status):
    r = {"id": rid, "slice": slice_, "pattern": "api-feature-go@1.0.0", "project": project, "status": status,
         "operation": "OP-%s" % rid.split("-")[-1], "created_at": T0, "updated_at": T0}
    if status == "ended":
        r.update(outcome="done", ended_at=T0)
    elif status in ("interrupted", "waiting-for-developer"):
        r["expires_at"] = "2026-11-28T10:00:00Z"
    return r


def phash(project):
    return hashlib.sha256(project.encode()).hexdigest()[:12]


class ProjectStepContainersTest(bw.BuildWorkersRig, unittest.TestCase):
    """Projects A and B of S-1 built one after the other with `ap build-workers`, then the resume and
    replay set-up driven against the engine stand-in's runs."""
    PROJECT = PROJECT_A

    # setup
    @classmethod
    def setUpClass(cls):
        why = bw.missing_prerequisite()
        if not why and bw.docker("image", "inspect", bw.lock_pin("alpine")).returncode != 0:
            why = "the alpine pin is not in the local image store"
        if why:
            raise unittest.SkipTest(why)
        assert len(PROJECT_B) == 63, len(PROJECT_B)
        cls.base = Path(tempfile.mkdtemp(prefix="test-bw-projects-", dir=bw.TMP_BASE)).resolve()
        cls.name = "ap-test-bwp-" + secrets.token_hex(4)
        cls.extra_containers = []
        cls.cli_image = None
        cls.addClassCleanup(cls.cleanup)
        cls.steps = bw.pattern_steps()
        cls.step_names = [s for s, _ in cls.steps]
        cls.writable = dict(cls.steps)
        cls.make_copy()
        cls.make_session()
        cls.make_cli_stand_in()
        cls.start_engine_stand_in()
        cls.tokfile = cls.state / "agentic-platform/workers" / SLICE
        # Runs that never block a build of S-1: an ended run of S-1 and an active run of another
        # slice, one per page, so build-workers reads more than one page.
        cls.quiet_runs = [run_record("RUN-1", SLICE, "older", "ended"), run_record("RUN-2", "S-2", "other", "running"),
                          run_record("RUN-3", SLICE, "old-replay", "ended")]
        cls.set_control(runs=cls.quiet_runs, page_size=1)
        cls.results, cls.spans = {}, {}
        for p in (PROJECT_A, PROJECT_B):
            start = len(cls.log())
            cls.results[p] = cls.ap("build-workers", SLICE, p)
            cls.spans[p] = (start, len(cls.log()))
            if p == PROJECT_A:
                cls.a_after_a = cls.container_state(PROJECT_A)

    @classmethod
    def rewrite_session(cls):
        _, all_vars = cls.session_vars()
        sess = cls.state / "agentic-platform/session.env"
        sess.write_text("# agentic-platform session, written by test_build_workers_projects.py as ap up writes it\n" +
                        "".join("%s=%s\n" % (k, bw.quote(cls.values[k])) for k in all_vars))
        sess.chmod(0o600)

    @classmethod
    def make_cli_stand_in(cls):
        script = cls.base / "fake-cli"
        script.write_text(FAKE_CLI_SH)
        script.chmod(0o755)
        tmp = "%s-cli-build" % cls.name
        r = bw.docker("create", "--name", tmp, "--network", "none", "--label", "%s=%s" % (TEST_LABEL, cls.name),
                      bw.lock_pin("alpine"), "true")
        if r.returncode != 0:
            raise AssertionError("cannot make the terminal client stand-in: %s" % r.stderr)
        try:
            for args in (("cp", str(script), tmp + ":/fake-cli"),
                         ("commit", "--change", 'ENTRYPOINT ["/fake-cli"]', "--change", "CMD []",
                          tmp, "%s-cli:test" % cls.name)):
                r = bw.docker(*args)
                if r.returncode != 0:
                    raise AssertionError("cannot make the terminal client stand-in: %s" % r.stderr)
        finally:
            bw.docker("rm", "-f", tmp)
        cls.cli_image = "%s-cli:test" % cls.name
        cls.values["AP_IMAGE_CLI"] = cls.cli_image
        cls.rewrite_session()

    @classmethod
    def start_engine_stand_in(cls):
        fake = cls.base / "fake"
        fake.mkdir(mode=0o755)
        (fake / "engine.js").write_text(FAKE_ENGINE_JS)
        (fake / "engine.js").chmod(0o644)
        cls.fakelog = cls.base / "fakelog"
        cls.fakelog.mkdir()
        cls.fakelog.chmod(0o777)  # the stand-in runs as the engine service's user
        cls.override = cls.base / "engine-stand-in.yaml"
        cls.override.write_text(json.dumps({"services": {"engine": {
            "image": bw.lock_pin("node"), "entrypoint": ["node", "/fake/engine.js"], "command": [],
            "environment": {"FAKE_LOG": "/fakelog/requests.jsonl", "FAKE_CONTROL": "/fakelog/control.json",
                            "FAKE_API_VERSION": API_VERSION},
            "volumes": [{"type": "bind", "source": str(fake), "target": "/fake", "read_only": True},
                        {"type": "bind", "source": str(cls.fakelog), "target": "/fakelog"}]}}}))
        r = cls.compose("config", "--format", "json")
        if r.returncode != 0:
            raise AssertionError("docker compose config failed on the copy:\n" + r.stderr)
        cls.cfg = json.loads(r.stdout)
        cls.own_grants_dir()
        r = cls.compose("up", "-d", "--no-deps", "engine")
        if r.returncode != 0:
            raise AssertionError("cannot start the engine stand-in:\n" + r.stderr)
        for _ in range(60):
            if any(e.get("ready") for e in cls.log()):
                break
            time.sleep(0.5)
        else:
            raise AssertionError("the engine stand-in did not start: %s" % bw.docker("logs", cls.engine_id()).stderr)
        cls.engine = cls.engine_id()

    @classmethod
    def set_control(cls, **ctl):
        tmp = cls.fakelog / ".control.tmp"
        tmp.write_text(json.dumps(ctl))
        tmp.chmod(0o644)
        os.replace(tmp, cls.fakelog / "control.json")

    @classmethod
    def ours(cls, c):
        if (c["Config"].get("Labels") or {}).get(TEST_LABEL) == cls.name:
            return True
        return super().ours(c)

    @classmethod
    def cleanup(cls):
        try:
            for cid in getattr(cls, "extra_containers", []):
                bw.docker("rm", "-f", cid)
            super().cleanup()
        finally:
            if getattr(cls, "cli_image", None):
                bw.docker("image", "rm", "-f", cls.cli_image)

    # what the launcher must make
    @classmethod
    def cname(cls, step, project, compose_project=None):
        return "%s-worker-%s-%s-%s" % (compose_project or cls.name, SLICE.lower(), step, phash(project))

    @classmethod
    def inspect_name(cls, name):
        r = bw.docker("container", "inspect", name)
        return json.loads(r.stdout)[0] if r.returncode == 0 else None

    @classmethod
    def container_state(cls, project):
        """step -> (ID, Created, StartedAt, Running, labels) of the project's container by its
        T053g name, or None."""
        out = {}
        for s in cls.step_names:
            c = cls.inspect_name(cls.cname(s, project))
            out[s] = None if c is None else (c["Id"], c["Created"], c["State"].get("StartedAt"),
                                             c["State"]["Running"], c["Config"].get("Labels") or {})
        return out

    # snapshots
    def grant_state(self):
        r = bw.docker("exec", self.engine, "node", "-e", READ_GRANTS_JS)
        self.assertEqual(r.returncode, 0, "the engine's user cannot read the grants folder: %s" % r.stderr)
        return json.loads(r.stdout)

    def snapshot(self):
        cs = sorted((c["Id"], c["Name"], c["State"].get("StartedAt"), c["State"]["Running"],
                     json.dumps(c["Config"].get("Labels") or {}, sort_keys=True))
                    for c in self.inspect_all() if self.ours(c) and c["Id"] != self.engine)
        tok = self.tokfile.read_bytes() if self.tokfile.exists() else None
        return {"containers": cs, "grants": self.grant_state(), "projects": bw.tree(self.projects),
                "tokens": tok, "log": len(self.log())}

    def assertNothingChanged(self, before, what):
        after = self.snapshot()
        self.assertEqual(after["containers"], before["containers"], "%s: containers were started, stopped or removed" % what)
        self.assertEqual(after["grants"], before["grants"], "%s: grant files were written or removed" % what)
        self.assertEqual(bw.tree_diff(before["projects"], after["projects"]), [], "%s: the projects folder changed" % what)
        self.assertEqual(after["tokens"], before["tokens"], "%s: the slice's tokens file changed" % what)
        new = self.log()[before["log"]:]
        self.assertEqual([e for e in new if e.get("path") == "/workers"], [], "%s: a worker was registered" % what)
        self.assertEqual([e["cli"] for e in new if e.get("path") == "/__cli"], [], "%s: the command was passed on" % what)
        return new

    def new_entries(self, since):
        return self.log()[since:]

    def tokens(self):
        """step -> (worker token, call token) of the slice's tokens file."""
        out = {}
        for line in self.tokfile.read_text().splitlines():
            f = line.split()
            if len(f) == 3:
                out[f[0]] = (f[1], f[2])
        return out

    def require_built(self, project):
        r = self.results[project]
        self.assertEqual(r.returncode, 0, "ap build-workers %s %s exited %d:\n%s" % (SLICE, project, r.returncode, r.stderr))

    def assertRunsRead(self, entries, path):
        reads = [e for e in entries if e.get("method") == "GET" and e.get("path") == path]
        self.assertTrue(reads, "the launcher did not read %s from the engine (requests: %s)" % (
            path, [(e.get("method"), e.get("url")) for e in entries]))
        client = self.values["AP_CLIENT_TOKEN"]
        for e in reads:
            self.assertEqual(e["token"], client, "%s was read without the session's client token" % path)
            self.assertEqual(parse_qs(urlparse(e["url"]).query).get("api-version"), [API_VERSION])
        return reads

    # the seams themselves (pass before T053g too)
    def test_0_the_stand_ins_answer_as_this_file_says(self):
        self.set_control(runs=self.quiet_runs, page_size=2, cli_exit=5)
        since = len(self.log())
        r = self.ap("runs")  # passed unchanged to the terminal client
        self.assertEqual(r.returncode, 5, "the client stand-in's exit code is not returned: %s" % r.stderr)
        cli = [e for e in self.new_entries(since) if e.get("path") == "/__cli"]
        self.assertEqual([e["cli"] for e in cli], [["runs"]])
        self.assertEqual(cli[0]["token"], self.values["AP_CLIENT_TOKEN"])
        get = ("const h=require('http');h.get({host:'127.0.0.1',port:8080,path:process.argv[1],"
               "headers:{Authorization:'Bearer '+process.argv[2]}},r=>{let b='';r.on('data',c=>b+=c);"
               "r.on('end',()=>process.stdout.write(JSON.stringify({s:r.statusCode,b:b})))})")
        q = "?api-version=" + API_VERSION
        def ask(path, token=self.values["AP_CLIENT_TOKEN"]):
            out = bw.docker("exec", self.engine, "node", "-e", get, path, token)
            self.assertEqual(out.returncode, 0, out.stderr)
            a = json.loads(out.stdout)
            return a["s"], json.loads(a["b"])
        s, b = ask("/runs" + q)
        self.assertEqual((s, [x["id"] for x in b["items"]], b.get("next_page_token")), (200, ["RUN-1", "RUN-2"], "page-1"))
        s, b = ask("/runs" + q + "&page_token=page-1")
        self.assertEqual((s, [x["id"] for x in b["items"]], b.get("next_page_token")), (200, ["RUN-3"], None))
        self.assertEqual(ask("/runs/RUN-2" + q), (200, self.quiet_runs[1]))
        self.assertEqual(ask("/runs/RUN-404" + q)[0], 404)
        self.assertEqual(ask("/runs" + q, token=self.values["AP_WORKER_TOKEN"])[0], 401)
        self.assertEqual(ask("/runs")[0], 400)

    # (1) names and labels
    def test_1_each_step_container_is_named_by_slice_step_and_project_hash_and_labelled_in_full(self):
        for project in (PROJECT_A, PROJECT_B):
            self.require_built(project)
            start, end = self.spans[project]
            regs = {e["grant"]["step"]: json.loads(e["body"])["endpoint"]
                    for e in self.log()[start:end] if e.get("path") == "/workers" and e.get("status") == 200}
            for s in self.step_names:
                name = self.cname(s, project)
                with self.subTest(project=project, step=s):
                    self.assertLessEqual(len(name), 63, "the name %s does not fit one DNS label" % name)
                    self.assertLessEqual(len(self.cname(s, project, "agentic-platform")), 63)
                    c = self.inspect_name(name)
                    self.assertIsNotNone(c, "no container named %s; the test's containers: %s" % (
                        name, sorted(x["Name"] for x in self.inspect_all() if self.ours(x))))
                    self.assertTrue(c["State"]["Running"], c["State"])
                    labels = c["Config"].get("Labels") or {}
                    self.assertEqual(labels.get("org.agentic-platform.slice"), SLICE)
                    self.assertEqual(labels.get("org.agentic-platform.step"), s)
                    self.assertEqual(labels.get("org.agentic-platform.project"), project)
                    self.assertEqual(regs.get(s), "http://%s:8080" % name,
                                     "build-workers registered %s of %s with another endpoint" % (s, project))
                    self.assertEqual(c["HostConfig"].get("Runtime"), "runsc")

    # (2) per project
    def test_2a_building_project_b_leaves_project_a_running_and_unchanged(self):
        self.require_built(PROJECT_A)
        self.require_built(PROJECT_B)
        now = self.container_state(PROJECT_A)
        for s in self.step_names:
            with self.subTest(step=s):
                self.assertIsNotNone(self.a_after_a[s], "A had no container for %s after its build" % s)
                self.assertEqual(now[s], self.a_after_a[s], "building B changed A's %s container" % s)
                self.assertTrue(now[s][3], "A's %s container is not running" % s)
        b = self.container_state(PROJECT_B)
        ids = {v[0] for v in list(now.values()) + list(b.values()) if v}
        self.assertEqual(len(ids), 2 * len(self.step_names), "A and B do not each have their own containers")

    def test_2b_a_container_of_the_wanted_name_labelled_with_another_project_is_refused(self):
        project = "collide-" + secrets.token_hex(3)
        last = self.step_names[-1]
        r = bw.docker("create", "--name", self.cname(last, project), "--network", "none",
                      "--label", "%s=%s" % (TEST_LABEL, self.name), "--label", "org.agentic-platform.slice=" + SLICE,
                      "--label", "org.agentic-platform.step=" + last, "--label", "org.agentic-platform.project=someone-else",
                      bw.lock_pin("alpine"), "true")
        self.assertEqual(r.returncode, 0, r.stderr)
        planted = r.stdout.strip()
        type(self).extra_containers.append(planted)
        self.set_control(runs=self.quiet_runs, page_size=1)
        before = self.snapshot()
        r = self.ap("build-workers", SLICE, project)
        self.assertEqual(r.returncode, 1, "a container of the wanted name for another project was not refused "
                         "with exit 1 (exit %d): %s" % (r.returncode, r.stderr))
        self.assertNothingChanged(before, "a refused name collision")
        self.assertFalse((self.projects / project).exists(), "the project folder was made")
        c = self.inspect_name(self.cname(last, project))
        self.assertIsNotNone(c, "the other project's container was removed")
        self.assertEqual(c["Id"], planted, "the other project's container was replaced")

    # (3) an active run of the slice
    def test_3_build_workers_while_a_run_of_the_slice_has_not_ended_is_refused_with_RunActive(self):
        self.require_built(PROJECT_A)
        for status in ("running", "interrupted", "waiting-for-developer"):
            active = run_record("RUN-7", SLICE, PROJECT_A, status)
            # the active run is on the last page: every page must be read
            self.set_control(runs=self.quiet_runs + [active], page_size=1)
            for project in ("fresh-" + secrets.token_hex(3), PROJECT_A):
                with self.subTest(status=status, project=project):
                    before = self.snapshot()
                    r = self.ap("build-workers", SLICE, project)
                    self.assertEqual(r.returncode, 4, "exit %d with a %s run of the slice: %s" % (r.returncode, status, r.stderr))
                    self.assertIn("build-workers: the run RUN-7 of %s has not ended (RunActive); resume, take over or "
                                  "cancel it first" % SLICE, r.stderr)
                    new = self.assertNothingChanged(before, "a refusal for an active run")
                    if project != PROJECT_A:
                        self.assertFalse((self.projects / project).exists(), "the project folder was made")
                    reads = self.assertRunsRead(new, "/runs")
                    pages = sorted({e.get("page") for e in reads})
                    self.assertEqual(pages, list(range(len(self.quiet_runs) + 1)), "not every page of listRuns was read")
                if r.returncode == 0:
                    return  # it built; the other cases would only build again

    # (4) resume with the containers running
    def test_4a_resume_of_an_interrupted_run_registers_its_containers_again_and_passes_it_on(self):
        self.require_built(PROJECT_A)
        self.require_built(PROJECT_B)
        # the run has written files and history, so a build-workers would refuse the folder
        pa = self.projects / PROJECT_A
        (pa / "api/openapi.yaml").write_text("openapi: 3.1.0\n")
        (pa / ".git/HEAD").write_text("ref: refs/heads/main\n")
        self.set_control(runs=self.quiet_runs + [run_record("RUN-11", SLICE, PROJECT_A, "interrupted")],
                         page_size=1, cli_exit=0)
        before = self.snapshot()
        r = self.ap("resume", "RUN-11")
        self.assertEqual(r.returncode, 0, "exit %d: %s" % (r.returncode, r.stderr))
        new = self.new_entries(before["log"])
        self.assertRunsRead(new, "/runs/RUN-11")
        toks = self.tokens()
        regs = [(i, e) for i, e in enumerate(new) if e.get("path") == "/workers"]
        self.assertEqual(sorted(e.get("grant", {}).get("step", "?") for _, e in regs), sorted(self.step_names),
                         "resume did not register each step once: %s" % [(e.get("status"), e.get("body")) for _, e in regs])
        for _, e in regs:
            s = e["grant"]["step"]
            with self.subTest(step=s):
                self.assertEqual(e["status"], 200)
                self.assertEqual(e["token"], toks[s][0], "registered without the step's worker token")
                self.assertEqual(json.loads(e["body"])["endpoint"], "http://%s:8080" % self.cname(s, PROJECT_A),
                                 "resume registered %s with another project's container" % s)
        cli = [(i, e) for i, e in enumerate(new) if e.get("path") == "/__cli"]
        self.assertEqual([e["cli"] for _, e in cli], [["resume", "RUN-11"]])
        self.assertEqual(cli[0][1]["token"], self.values["AP_CLIENT_TOKEN"])
        self.assertTrue(all(i < cli[0][0] for i, _ in regs), "the command was passed on before the registrations")
        after = self.snapshot()
        self.assertEqual(after["containers"], before["containers"], "resume started, replaced or stopped containers")
        self.assertEqual(after["grants"], before["grants"], "resume wrote grant files")
        self.assertEqual(bw.tree_diff(before["projects"], after["projects"]), [], "resume changed the projects folder")
        self.assertEqual(after["tokens"], before["tokens"], "resume changed the slice's tokens")

    def test_4b_resume_of_a_run_that_is_not_interrupted_is_passed_on_unchanged(self):
        for status in ("running", "ended"):
            with self.subTest(status=status):
                self.set_control(runs=[run_record("RUN-13", SLICE, PROJECT_A, status)], cli_exit=4)
                before = self.snapshot()
                r = self.ap("resume", "RUN-13")
                new = self.new_entries(before["log"])
                self.assertEqual(r.returncode, 4, "the client's exit code is not returned: %s" % r.stderr)
                self.assertRunsRead(new, "/runs/RUN-13")
                self.assertEqual([e for e in new if e.get("path") == "/workers"], [], "a worker was registered")
                self.assertEqual([e["cli"] for e in new if e.get("path") == "/__cli"], [["resume", "RUN-13"]])

    # (5) resume when the containers or the tokens are gone
    def resume_refused(self, run, project, what):
        before = self.snapshot()
        r = self.ap("resume", run)
        self.assertEqual(r.returncode, 3, "%s: exit %d: %s" % (what, r.returncode, r.stderr))
        self.assertIn("resume: the step workers of %s for the project %s are not running; run ap build-workers "
                      "--resume %s, then ap resume %s" % (SLICE, project, run, run), r.stderr)
        new = self.assertNothingChanged(before, what)
        self.assertRunsRead(new, "/runs/" + run)

    def test_5a_resume_whose_containers_were_never_started_is_refused(self):
        project = "never-built"
        self.set_control(runs=[run_record("RUN-12", SLICE, project, "interrupted")], cli_exit=0)
        self.resume_refused("RUN-12", project, "no containers")
        self.assertFalse((self.projects / project).exists(), "resume made the project folder")

    def test_5b_resume_with_one_container_stopped_is_refused(self):
        self.require_built(PROJECT_B)
        name = self.cname("code", PROJECT_B)
        self.set_control(runs=[run_record("RUN-14", SLICE, PROJECT_B, "interrupted")], cli_exit=0)
        r = bw.docker("stop", "-t", "5", name)
        self.assertEqual(r.returncode, 0, r.stderr)
        try:
            self.resume_refused("RUN-14", PROJECT_B, "a stopped container")
        finally:
            bw.docker("start", name)

    def test_5c_resume_without_the_slices_tokens_is_refused(self):
        self.require_built(PROJECT_A)
        self.set_control(runs=[run_record("RUN-15", SLICE, PROJECT_A, "interrupted")], cli_exit=0)
        aside = self.base / "tokens-aside"
        os.replace(self.tokfile, aside)
        try:
            self.resume_refused("RUN-15", PROJECT_A, "no tokens file")
        finally:
            if self.tokfile.exists():
                self.tokfile.unlink()
            os.replace(aside, self.tokfile)

    def test_5d_resume_whose_containers_hold_other_tokens_than_the_session_is_refused(self):
        """The slice's tokens file was made again (its `id` line changed) while the containers kept
        running with the old tokens: they are not this session's workers (review finding M6)."""
        self.require_built(PROJECT_A)
        self.set_control(runs=[run_record("RUN-16", SLICE, PROJECT_A, "interrupted")], cli_exit=0)
        original = self.tokfile.read_bytes()
        lines = original.decode().splitlines()
        ids = [i for i, line in enumerate(lines) if line.split()[:1] == ["id"]]
        self.assertEqual(len(ids), 1, "the tokens file has no single `id` line: %r" % lines)
        lines[ids[0]] = "id " + secrets.token_hex(8)
        self.tokfile.write_text("\n".join(lines) + "\n")
        try:
            self.resume_refused("RUN-16", PROJECT_A, "containers with another tokens id")
        finally:
            self.tokfile.write_bytes(original)
            self.tokfile.chmod(0o600)

    def test_5e_resume_whose_container_has_the_name_but_another_slice_step_or_project_label_is_refused(self):
        """A container with the T053g name, running and started with this session's tokens, but whose
        slice, step or project label names another one, is not the run's worker (review finding M12).
        The containers are stand-ins (the alpine pin, no network): only their names and labels count."""
        project = "labelled-" + secrets.token_hex(3)
        toks_id = [line.split()[1] for line in self.tokfile.read_text().splitlines() if line.split()[:1] == ["id"]]
        self.assertEqual(len(toks_id), 1)
        last = self.step_names[-1]
        for wrong in ("project", "slice", "step"):
            with self.subTest(wrong=wrong):
                made = []
                try:
                    for st in self.step_names:
                        labels = {"slice": SLICE, "step": st, "project": project}
                        if st == last:
                            labels[wrong] = {"project": "someone-else", "slice": "S-2", "step": self.step_names[0]}[wrong]
                        # the worker label as build-workers writes it: service image project step checksum tokens-id
                        worker = "procedure-worker %s %s %s 0 %s" % (bw.lock_pin("alpine"), project, st, toks_id[0])
                        args = ["run", "-d", "--name", self.cname(st, project), "--network", "none",
                                "--label", "%s=%s" % (TEST_LABEL, self.name), "--label", "org.agentic-platform.worker=" + worker]
                        for k, v in labels.items():
                            args += ["--label", "org.agentic-platform.%s=%s" % (k, v)]
                        r = bw.docker(*args, bw.lock_pin("alpine"), "sleep", "600")
                        self.assertEqual(r.returncode, 0, r.stderr)
                        made.append(r.stdout.strip())
                        type(self).extra_containers.append(made[-1])
                    self.set_control(runs=[run_record("RUN-17", SLICE, project, "interrupted")], cli_exit=0)
                    self.resume_refused("RUN-17", project, "a container with another %s label" % wrong)
                finally:
                    for cid in made:
                        bw.docker("rm", "-f", cid)

    # (6) replay
    def test_6a_replay_into_a_folder_holding_files_or_a_history_is_refused(self):
        self.set_control(runs=self.quiet_runs + [run_record("RUN-20", SLICE, "notes-first", "ended")], page_size=1, cli_exit=0)

        def file_deep(p):
            (p / "internal/app").mkdir(parents=True)
            (p / "internal/app/app.go").write_text("")

        def symlink(p):
            (p / "tests").mkdir(parents=True)
            (p / "tests/link").symlink_to("..")

        def history(p):
            (p / ".git/refs").mkdir(parents=True)

        for label, make in (("a file deep down", file_deep), ("a symlink", symlink), ("a non-empty .git", history)):
            with self.subTest(label):
                p = self.projects / ("taken-" + secrets.token_hex(3))
                p.mkdir()
                make(p)
                before = self.snapshot()
                r = self.ap("replay", "RUN-20", p.name)
                self.assertEqual(r.returncode, 3, "exit %d for a folder with %s: %s" % (r.returncode, label, r.stderr))
                self.assertIn(str(p), r.stderr, "the refusal does not name the folder")
                new = self.assertNothingChanged(before, "a replay into a folder with " + label)
                self.assertRunsRead(new, "/runs/RUN-20")

    def test_6b_replay_into_a_new_project_builds_its_workers_then_passes_it_on(self):
        self.require_built(PROJECT_A)
        project = "notes-d"
        self.set_control(runs=self.quiet_runs + [run_record("RUN-20", SLICE, "notes-first", "ended")], page_size=1, cli_exit=0)
        a_before = self.container_state(PROJECT_A)
        since = len(self.log())
        r = self.ap("replay", "RUN-20", project)
        self.assertEqual(r.returncode, 0, "exit %d: %s" % (r.returncode, r.stderr))
        new = self.new_entries(since)
        self.assertRunsRead(new, "/runs/RUN-20")
        self.assertRunsRead(new, "/runs")  # build-workers' active-run check
        self.assertTrue((self.projects / project / ".git").is_dir(), "build-workers did not make the project")
        regs = [(i, e) for i, e in enumerate(new) if e.get("path") == "/workers" and e.get("status") == 200]
        endpoints = {e["grant"]["step"]: json.loads(e["body"])["endpoint"] for _, e in regs}
        for s in self.step_names:
            with self.subTest(step=s):
                c = self.inspect_name(self.cname(s, project))
                self.assertIsNotNone(c, "no container %s" % self.cname(s, project))
                self.assertTrue(c["State"]["Running"])
                self.assertEqual((c["Config"].get("Labels") or {}).get("org.agentic-platform.project"), project)
                self.assertEqual(endpoints.get(s), "http://%s:8080" % self.cname(s, project))
        cli = [(i, e) for i, e in enumerate(new) if e.get("path") == "/__cli"]
        self.assertEqual([e["cli"] for _, e in cli], [["replay", "RUN-20", project]])
        self.assertTrue(all(i < cli[0][0] for i, _ in regs), "the command was passed on before the registrations")
        self.assertEqual(self.container_state(PROJECT_A), a_before, "replay changed project A's containers")

    def test_6c_replay_builds_the_workers_of_the_runs_own_slice(self):
        self.set_control(runs=[run_record("RUN-21", "S-9", "notes-nine", "ended")], unknown_slices=["S-9"], cli_exit=0)
        project = "nine-" + secrets.token_hex(3)
        since = len(self.log())
        r = self.ap("replay", "RUN-21", project)
        new = self.new_entries(since)
        self.assertEqual(r.returncode, 4, "exit %d: %s" % (r.returncode, r.stderr))
        self.assertIn("NotFound", r.stderr)
        self.assertRunsRead(new, "/runs/RUN-21")
        slices = {e.get("grant", {}).get("slice") for e in new if e.get("path") == "/workers"}
        self.assertEqual(slices, {"S-9"}, "build-workers did not run for the run's slice S-9")
        self.assertEqual([e for e in new if e.get("path") == "/__cli"], [])

    # (7) an unknown run
    def test_7_an_unknown_run_is_NotFound_for_resume_and_replay(self):
        self.set_control(runs=self.quiet_runs, page_size=1, cli_exit=0)
        for args in (("resume", "RUN-99"), ("replay", "RUN-99", "unknown-" + secrets.token_hex(3))):
            with self.subTest(args=args):
                before = self.snapshot()
                r = self.ap(*args)
                self.assertEqual(r.returncode, 4, "exit %d: %s" % (r.returncode, r.stderr))
                self.assertIn("NotFound", r.stderr)
                new = self.assertNothingChanged(before, "an unknown run")
                self.assertRunsRead(new, "/runs/RUN-99")

    # (8) fail closed
    def test_8a_a_list_runs_answer_that_is_missing_not_200_or_unreadable_fails_build_workers_closed(self):
        two_pages = [run_record("RUN-1", SLICE, "older", "ended"), run_record("RUN-3", SLICE, "old-replay", "ended")]
        for page, mode in ((0, "close"), (0, 500), (0, 503), (0, "truncated"), (0, "not-json"), (0, "no-items"),
                           (1, "close"), (1, 500), (1, "truncated")):
            project = "closed-" + secrets.token_hex(3)
            with self.subTest(page=page, mode=mode):
                self.set_control(runs=two_pages, page_size=1, list_fault={"page": page, "mode": mode})
                before = self.snapshot()
                r = self.ap("build-workers", SLICE, project)
                self.assertEqual(r.returncode, 1, "exit %d: %s" % (r.returncode, r.stderr))
                new = self.assertNothingChanged(before, "listRuns %s on page %d" % (mode, page))
                self.assertFalse((self.projects / project).exists(), "the project folder was made")
                self.assertTrue([e for e in new if e.get("fault")], "the faulty answer was never asked for")
            if r.returncode == 0:
                return  # it built; the other cases would only build again

    def get_run_faults(self, status, fields=("slice", "project", "status")):
        """(label, control) pairs: a getRun answer that is missing, not 200 or cannot be read; the
        answer's fields the command reads are each missing or not a value of their kind."""
        good = run_record("RUN-30", SLICE, PROJECT_A, status)
        out = [("get %s" % m, dict(runs=[good], get_fault=m)) for m in ("close", 500, 503, "truncated", "not-json")]
        bad_values = {"slice": [("no slice", {"slice": None}), ("a slice that is no slice ID", {"slice": "S-1; true"})],
                      "project": [("no project", {"project": None}), ("a project that is no name", {"project": "../escape"})],
                      "status": [("no status", {"status": None})]}
        for label, change in [c for f in fields for c in bad_values[f]]:
            bad = dict(good, **change)
            out.append((label, dict(runs=[{k: v for k, v in bad.items() if v is not None}])))
        return out

    def test_8b_a_get_run_answer_that_is_missing_not_200_or_unreadable_fails_resume_closed(self):
        self.require_built(PROJECT_A)
        for label, ctl in self.get_run_faults("interrupted"):
            with self.subTest(label):
                self.set_control(cli_exit=0, **ctl)
                before = self.snapshot()
                r = self.ap("resume", "RUN-30")
                self.assertEqual(r.returncode, 1, "exit %d: %s" % (r.returncode, r.stderr))
                new = self.assertNothingChanged(before, "resume with " + label)
                self.assertRunsRead(new, "/runs/RUN-30")

    def test_8c_a_get_run_answer_that_is_missing_not_200_or_unreadable_fails_replay_closed(self):
        for label, ctl in self.get_run_faults("ended", fields=("slice",)):
            project = "replay-closed-" + secrets.token_hex(3)
            with self.subTest(label):
                self.set_control(cli_exit=0, **ctl)
                before = self.snapshot()
                r = self.ap("replay", "RUN-30", project)
                self.assertEqual(r.returncode, 1, "exit %d: %s" % (r.returncode, r.stderr))
                self.assertNothingChanged(before, "replay with " + label)
                self.assertFalse((self.projects / project).exists(), "the project folder was made")


if __name__ == "__main__":
    unittest.main()
