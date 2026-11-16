"""Tests for the launcher's `ap build-workers <slice> <project>` (T053d), with real Docker and gVisor.

Judged from tasks.md T053d, research D29 and D30, ADRs 0030, 0032 and 0033, and the owner's
decision of 2026-11-13 on grants: `ap build-workers` writes one file per grant, named by the lowercase
hex SHA-256 of the worker's token, holding {"slice", "step", "call_token"}, mode 0600, into a host
folder only the engine mounts, read-only; the engine reads it at each worker-token authentication.

How the tests drive the real launcher (the seams this file assumes):

- A copy of the launcher's folder (src/deploy) beside links to the rest of the repository, so ap
  finds what it finds in the repository. In the copy's compose.yaml only the Compose project name
  and the platform's fixed 10.231.0.0/16 subnets are changed (to a free 10.N.0.0/16, every fixed
  address moved with them), so the test's containers and networks never meet a running platform's.
- A session of its own: XDG_STATE_HOME, XDG_CONFIG_HOME and HOME are fresh folders, and the test
  writes session.env as `ap up` does, with every variable ap's `vars` line lists. The images are the
  ones build.sh recorded in the developer's images.env (the agent and check images must be built);
  `build-workers` uses the session's images as `up` verified them. A session variable this file does
  not know that ends in _DIR is given a new private folder `agentic-platform/<name>` in the state
  folder (for example AP_GRANTS_DIR: agentic-platform/grants, then handed to the engine service's
  user with mode 700, as compose.yaml says `up` leaves it); any other unknown one is an error. The
  grants folder is read only from inside the engine stand-in, as the engine's user.
- An engine stand-in: the copy's own `engine` service, started by the test before `build-workers`
  with a Compose override that only swaps its image for the node pin and runs fake-engine.js. It keeps
  the service's user, networks, addresses, environment and mounts, so it reads the grants folder
  exactly where, and as whom, the engine would (ENGINE_GRANTS_DIR). It answers registerWorker by the
  owner's rule (200 for a token whose grant file is readable and well formed, else 401) and records
  every request. `build-workers` must leave it running (it changes nothing it does not own).
- The pattern's steps and writable entries are read from src/catalogue/api-feature-go/pattern.yaml:
  a `dir/**` entry makes `dir` writable, an exact path its folder (contract-check: api/ and
  features/), and an exact path in the root the whole root (generate only, a stated limit).

A step's container is found by the endpoint `build-workers` registered for it (a name or address
the container has); each write is tried inside the container (`docker exec`, as its user) and judged
on the host. Without Docker, the runsc runtime, git, PyYAML, the node pin or the built agent and
check images, the tests are skipped with the reason.
"""
import hashlib
import ipaddress
import json
import os
import random
import re
import secrets
import shlex
import shutil
import stat
import subprocess
import tempfile
import time
import unittest
from pathlib import Path
from urllib.parse import urlparse

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
# AP_TEST_AP points the tests at another copy of ap (to see them fail against a mutant); it replaces ap in the copy
REAL_AP = Path(os.environ.get("AP_TEST_AP") or REPO / "src/deploy/ap")
PATTERN = REPO / "src/catalogue/api-feature-go/pattern.yaml"
LOCK = REPO / "src/deploy/images.lock"
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
REAL_HOME = Path.home()
REAL_IMAGES_ENV = Path(os.environ.get("XDG_STATE_HOME") or REAL_HOME / ".local/state") / "agentic-platform/images.env"
SLICE = "S-1"
API_VERSION = "2026-11-12-preview"
HEX64 = re.compile(r"^[0-9a-f]{64}$")
SECRET = "fake-model-key-BUILDWORKERS-0123456789"

FAKE_ENGINE_JS = r"""'use strict';
// The engine stand-in of test_build_workers.py: registerWorker by the owner's grant rule, every
// request recorded as one JSON line in $FAKE_LOG.
const http = require('http'), fs = require('fs'), crypto = require('crypto'), path = require('path');
const LOG = process.env.FAKE_LOG, DIR = process.env.ENGINE_GRANTS_DIR || '';
function log(o) { fs.appendFileSync(LOG, JSON.stringify(o) + '\n'); }
// A grant is the file named by the token's SHA-256 in ENGINE_GRANTS_DIR; unreadable or malformed
// is no grant, and without the folder nothing is granted.
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
function problem(status, title, code) { return { type: 'about:blank', title, status, code }; }
http.createServer((req, res) => {
  let body = '';
  req.on('data', c => { body += c; });
  req.on('end', () => {
    const m = /^Bearer (.+)$/i.exec(req.headers['authorization'] || '');
    const token = m ? m[1] : '';
    const entry = { method: req.method, url: req.url, token, body };
    let status = 404, out = problem(404, 'Not Found', 'NotFound');
    if (req.method === 'POST' && req.url.split('?')[0] === '/workers') {
      const found = grantOf(token);
      entry.err = found.err;
      if (!found.grant) {
        status = 401; out = problem(401, 'Unauthorized', 'Unauthorized');
      } else {
        let ep = '';
        try { ep = JSON.parse(body).endpoint; } catch (e) {}
        if (typeof ep !== 'string' || !ep) {
          status = 400; out = problem(400, 'Bad Request', 'InvalidRequest');
        } else {
          status = 200; entry.grant = found.grant;
          out = { slice: found.grant.slice, step: found.grant.step, endpoint: ep };
        }
      }
    } else if (req.method === 'GET' && req.url.split('?')[0] === '/runs') {
      // listRuns (T053g, owner 2026-11-14): no runs, for the session's client token only
      if (token && token === (process.env.ENGINE_CLIENT_TOKEN || '')) { status = 200; out = { items: [] }; }
      else { status = 401; out = problem(401, 'Unauthorized', 'Unauthorized'); }
    }
    entry.status = status;
    log(entry);
    res.writeHead(status, { 'Content-Type': status === 200 ? 'application/json' : 'application/problem+json' });
    res.end(JSON.stringify(out));
  });
}).listen(8080, '0.0.0.0', () => log({ ready: true, grants_dir: DIR }));
"""


# Lists the grants folder inside the engine stand-in, as the engine's user.
READ_GRANTS_JS = (
    "const fs=require('fs'),p=require('path'),d=process.env.ENGINE_GRANTS_DIR,o={};"
    "for(const n of fs.readdirSync(d)){const f=p.join(d,n),s=fs.lstatSync(f);"
    "o[n]={mode:s.mode,uid:s.uid,file:s.isFile(),content:s.isFile()?fs.readFileSync(f,'utf8'):null}}"
    "process.stdout.write(JSON.stringify(o))")


def docker(*args, **kw):
    kw.setdefault("capture_output", True)
    kw.setdefault("text", True)
    kw.setdefault("timeout", 120)
    return subprocess.run(["docker", *args], **kw)


def lock_pin(name):
    for line in LOCK.read_text().splitlines():
        f = line.split()
        if len(f) >= 2 and f[0] == name:
            return f[1]
    raise AssertionError("no %s pin in %s" % (name, LOCK))


def recorded_images():
    """AP_IMAGE_* of the developer's images.env (what build.sh recorded), as a dict."""
    out = {}
    if REAL_IMAGES_ENV.is_file():
        for line in REAL_IMAGES_ENV.read_text().splitlines():
            k, _, v = line.partition("=")
            if k.startswith("AP_IMAGE_"):
                out[k] = v.strip("'\"")
    return out


def missing_prerequisite():
    """Why the tests cannot run here, or None."""
    if not shutil.which("docker"):
        return "docker is not on PATH"
    r = docker("info", "--format", "{{json .Runtimes}}")
    if r.returncode != 0:
        return "Docker does not answer: %s" % r.stderr.strip()
    if '"runsc"' not in r.stdout:
        return "Docker has no runsc runtime (gVisor)"
    if not shutil.which("git"):
        return "git is not on PATH"
    try:
        import yaml  # noqa: F401
    except ImportError:
        return "PyYAML is not installed"
    if docker("image", "inspect", lock_pin("node")).returncode != 0:
        return "the node pin (the engine stand-in's image) is not in the local image store"
    imgs = recorded_images()
    for v in ("AP_IMAGE_AGENT", "AP_IMAGE_CHECK"):
        if not imgs.get(v):
            return "%s is not recorded in %s; build it with src/deploy/build.sh" % (v, REAL_IMAGES_ENV)
        if docker("image", "inspect", imgs[v]).returncode != 0:
            return "%s (%s) is not in the local image store" % (v, imgs[v])
    return None


def pattern_steps():
    """[(step, writable entries)] of the pattern, in order."""
    import yaml
    doc = yaml.safe_load(PATTERN.read_text())
    return [(s["name"], list(s.get("writable") or [])) for s in doc["steps"]]


def writable_folders(entries):
    """(root writable, folders writable) for a step's writable entries: `dir/**` makes dir writable,
    an exact path its folder, an exact path in the root the whole root."""
    root, folders = False, set()
    for w in entries:
        if w.endswith("/**"):
            folders.add(w[:-3])
        elif "/" in w:
            folders.add(w.rsplit("/", 1)[0])
        else:
            root = True
    return root, folders


def may_write(step_entries, folder):
    root, folders = writable_folders(step_entries)
    return root or any(folder == f or folder.startswith(f + "/") for f in folders)


def tree(top):
    """Every path under top: relative path -> (kind, mode, content hash or link target)."""
    out = {}
    top = Path(top)
    for d, dirs, files in os.walk(top):
        for n in dirs + files:
            p = Path(d) / n
            st = p.lstat()
            rel = str(p.relative_to(top))
            if stat.S_ISLNK(st.st_mode):
                out[rel] = ("link", stat.S_IMODE(st.st_mode), os.readlink(p))
            elif stat.S_ISDIR(st.st_mode):
                out[rel] = ("dir", stat.S_IMODE(st.st_mode), None)
            else:
                out[rel] = ("file", stat.S_IMODE(st.st_mode), hashlib.sha256(p.read_bytes()).hexdigest())
    return out


def tree_diff(a, b):
    return sorted(k for k in set(a) | set(b) if a.get(k) != b.get(k))


def free_octet():
    """A second octet N with no Docker network in 10.N.0.0/16."""
    ids = docker("network", "ls", "-q").stdout.split()
    used = []
    if ids:
        r = docker("network", "inspect", "-f", "{{range .IPAM.Config}}{{.Subnet}} {{end}}", *ids)
        for s in r.stdout.split():
            try:
                used.append(ipaddress.ip_network(s, strict=False))
            except ValueError:
                pass
    choices = list(range(100, 250))
    random.shuffle(choices)
    for n in choices:
        net = ipaddress.ip_network("10.%d.0.0/16" % n)
        if n != 231 and not any(u.version == 4 and u.overlaps(net) for u in used):
            return n
    raise AssertionError("no free 10.N.0.0/16 for the test's networks")


def quote(v):
    return "'" + v.replace("'", "'\\''") + "'"


class BuildWorkersRig:
    """One copy of the launcher, one session, the engine stand-in and one `ap build-workers` run per
    class. Subclasses set PROJECT and may override prepare_project (called before build-workers)."""
    PROJECT = None

    # setup
    @classmethod
    def setUpClass(cls):
        why = missing_prerequisite()
        if why:
            raise unittest.SkipTest(why)
        cls.base = Path(tempfile.mkdtemp(prefix="test-build-workers-", dir=TMP_BASE)).resolve()
        cls.name = "ap-test-bw-" + secrets.token_hex(4)
        cls.addClassCleanup(cls.cleanup)
        cls.steps = pattern_steps()
        cls.writable = dict(cls.steps)
        cls.make_copy()
        cls.make_session()
        cls.start_engine_stand_in()
        cls.prepare_project()
        t0 = time.monotonic()
        cls.first = cls.ap("build-workers", SLICE, cls.PROJECT)
        cls.build_seconds = time.monotonic() - t0
        if cls.first.returncode != 0:
            raise AssertionError("ap build-workers %s %s exited %d:\n%s%s" % (
                SLICE, cls.PROJECT, cls.first.returncode, cls.first.stdout, cls.first.stderr))
        if cls.engine_id() != cls.engine:
            raise AssertionError("ap build-workers replaced the running engine container; it must change "
                                 "nothing it does not own (and these tests' engine stand-in relies on it)")
        cls.discover()
        cls.after_build()

    @classmethod
    def make_copy(cls):
        root = cls.base / "repo"
        (root / "src").mkdir(parents=True)
        shutil.copytree(REPO / "src/deploy", root / "src/deploy", symlinks=True)
        for e in (REPO / "src").iterdir():
            if e.name != "deploy":
                (root / "src" / e.name).symlink_to(e)
        for e in REPO.iterdir():
            if e.name not in ("src", ".git"):
                (root / e.name).symlink_to(e)
        cls.ap_path = root / "src/deploy/ap"
        if REAL_AP.resolve() != (REPO / "src/deploy/ap").resolve():
            shutil.copy2(REAL_AP, cls.ap_path)
        cls.compose_file = root / "src/deploy/compose.yaml"
        text = cls.compose_file.read_text()
        octet = free_octet()
        new, n_name = re.subn(r"(?m)^name: agentic-platform$", "name: " + cls.name, text)
        new, n_net = re.subn(r"\b10\.231\.", "10.%d." % octet, new)
        if n_name != 1 or n_net == 0:
            raise AssertionError("compose.yaml no longer has `name: agentic-platform` and 10.231.x subnets; "
                                 "this test's way of isolating its copy must be revised")
        cls.compose_file.write_text(new)

    @classmethod
    def session_vars(cls):
        """(token variables, every session variable), read from the launcher under test."""
        script = ('eval "$(grep -E \'^(needed_images|optional_images|image_vars|token_vars|vars)=\' "$1")"; '
                  'printf "%s\\n" "$token_vars"; printf "%s\\n" "$vars"')
        r = subprocess.run(["sh", "-c", script, "sh", str(cls.ap_path)], capture_output=True, text=True, timeout=30)
        lines = r.stdout.splitlines()
        if r.returncode != 0 or len(lines) < 2 or not lines[1].split():
            raise AssertionError("cannot read the session variables from %s: %s" % (cls.ap_path, r.stderr))
        return lines[0].split(), lines[1].split()

    @classmethod
    def make_session(cls):
        cls.home, cls.state, cls.config = cls.base / "home", cls.base / "state", cls.base / "config"
        for d in (cls.home, cls.state, cls.config):
            d.mkdir(mode=0o700)
        cls.projects = cls.base / "projects"
        cls.projects.mkdir()
        cls.project = cls.projects / cls.PROJECT
        cred = cls.base / "models.env"
        cred.write_text(SECRET + "\n")
        cred.chmod(0o600)
        apdir = cls.state / "agentic-platform"
        apdir.mkdir(mode=0o700)
        shutil.copy2(REAL_IMAGES_ENV, apdir / "images.env")
        (apdir / "images.env").chmod(0o600)
        images = recorded_images()
        token_vars, all_vars = cls.session_vars()
        known = {"AP_CREDENTIALS_FILE": str(cred), "AP_PROJECTS_DIR": str(cls.projects), "AP_PAGE_PORT": "7878",
                 "AP_HOST_USER": "%d:%d" % (os.getuid(), os.getgid()), "AP_GATEWAY_IMAGE": lock_pin("nginx")}
        values = {}
        for v in all_vars:
            if v in known:
                values[v] = known[v]
            elif v in token_vars or v.endswith("_TOKEN"):
                values[v] = secrets.token_hex(32)
            elif v.startswith("AP_IMAGE_"):
                values[v] = images.get(v) or "ap-%s:not-built" % v[len("AP_IMAGE_"):].lower()
            elif v.endswith("_DIR"):
                d = apdir / v[len("AP_"):-len("_DIR")].lower()
                d.mkdir(mode=0o700, exist_ok=True)
                values[v] = str(d)
            else:
                raise AssertionError("session variable %s is new to this test; tell its author what it holds" % v)
        cls.values = values
        sess = apdir / "session.env"
        sess.write_text("# agentic-platform session, written by test_build_workers.py as ap up writes it\n" +
                        "".join("%s=%s\n" % (k, quote(values[k])) for k in all_vars))
        sess.chmod(0o600)
        cls.env = {k: v for k, v in os.environ.items() if not k.startswith(("AP_", "COMPOSE_"))}
        cls.env.update(HOME=str(cls.home), XDG_STATE_HOME=str(cls.state), XDG_CONFIG_HOME=str(cls.config))
        if "DOCKER_CONFIG" not in cls.env and (REAL_HOME / ".docker").is_dir():
            cls.env["DOCKER_CONFIG"] = str(REAL_HOME / ".docker")

    @classmethod
    def ap(cls, *args, timeout=600):
        return subprocess.run(["sh", str(cls.ap_path), *args], env=cls.env, capture_output=True, text=True,
                              timeout=timeout, stdin=subprocess.DEVNULL)

    @classmethod
    def compose(cls, *args, timeout=300):
        env = dict(cls.env, **cls.values)
        return subprocess.run(["docker", "compose", "-f", str(cls.compose_file), "-f", str(cls.override), *args],
                              env=env, capture_output=True, text=True, timeout=timeout)

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
            "image": lock_pin("node"), "entrypoint": ["node", "/fake/engine.js"], "command": [],
            "environment": {"FAKE_LOG": "/fakelog/requests.jsonl"},
            "volumes": [{"type": "bind", "source": str(fake), "target": "/fake", "read_only": True},
                        {"type": "bind", "source": str(cls.fakelog), "target": "/fakelog"}]}}}))
        r = cls.compose("config", "--format", "json")
        if r.returncode != 0:
            raise AssertionError("docker compose config failed on the copy:\n" + r.stderr)
        cls.cfg = json.loads(r.stdout)
        cls.own_grants_dir()
        r = cls.compose("up", "-d", "--no-deps", "engine")
        if r.returncode != 0:
            raise AssertionError("cannot start the engine stand-in (is the grants folder in the session, "
                                 "AP_GRANTS_DIR?):\n" + r.stderr)
        for _ in range(60):
            if any(e.get("ready") for e in cls.log()):
                break
            time.sleep(0.5)
        else:
            raise AssertionError("the engine stand-in did not start: %s" % docker("logs", cls.engine_id()).stderr)
        cls.engine = cls.engine_id()

    @classmethod
    def own_grants_dir(cls):
        """AP_GRANTS_DIR as compose.yaml says `ap up` leaves it: owned by the engine's user, mode 700
        (this test writes the session itself, so it does that part of `up`)."""
        d = cls.values.get("AP_GRANTS_DIR")
        user = cls.cfg["services"]["engine"].get("user") or ""
        if not d or not user:
            return
        Path(d).chmod(0o700)
        r = docker("run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--cap-add", "CHOWN",
                   "--security-opt", "no-new-privileges", "--user", "0:0", "-v", "%s:/g" % d,
                   "--entrypoint", "chown", lock_pin("alpine"), user, "/g")
        if r.returncode != 0:
            raise AssertionError("cannot hand %s to the engine's user %s: %s" % (d, user, r.stderr))
        cls.grants_owner = user

    @classmethod
    def empty_grants_dir(cls):
        """Remove what the engine's user owns in AP_GRANTS_DIR, so the folder can be removed."""
        d, user = getattr(cls, "values", {}).get("AP_GRANTS_DIR"), getattr(cls, "grants_owner", None)
        if d and user and Path(d).exists():
            docker("run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
                   "--security-opt", "no-new-privileges", "--user", user, "-v", "%s:/g" % d,
                   "--entrypoint", "find", lock_pin("alpine"), "/g", "-mindepth", "1", "-delete")
            # and hand the folder back to the developer, so it can be removed (and read if not empty)
            docker("run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--cap-add", "CHOWN",
                   "--security-opt", "no-new-privileges", "--user", "0:0", "-v", "%s:/g" % d,
                   "--entrypoint", "chown", lock_pin("alpine"), "%d:%d" % (os.getuid(), os.getgid()), "/g")

    @classmethod
    def engine_id(cls):
        return cls.compose("ps", "-q", "engine").stdout.strip()

    @classmethod
    def prepare_project(cls):
        pass

    @classmethod
    def after_build(cls):
        pass

    @classmethod
    def log(cls):
        p = cls.fakelog / "requests.jsonl"
        if not p.exists():
            return []
        return [json.loads(line) for line in p.read_text().splitlines() if line]

    @classmethod
    def inspect_all(cls):
        ids = docker("ps", "-aq").stdout.split()
        return json.loads(docker("inspect", *ids).stdout) if ids else []

    @classmethod
    def ours(cls, c):
        """True for a container of the test: of its Compose project, or mounting anything in its folder."""
        if (c["Config"].get("Labels") or {}).get("com.docker.compose.project") == cls.name:
            return True
        return any(str(m.get("Source", "")).startswith(str(cls.base) + "/") for m in c.get("Mounts") or [])

    @classmethod
    def discover(cls):
        """The step containers, by the endpoint each step was registered with, and the grants folder."""
        cls.engine_inspect = json.loads(docker("inspect", cls.engine).stdout)[0]
        others = [c for c in cls.inspect_all() if cls.ours(c) and c["Id"] != cls.engine]
        cls.step_containers = others
        regs = [e for e in cls.log() if e.get("status") == 200 and "grant" in e]
        cls.registrations = {}
        for e in regs:
            cls.registrations[e["grant"]["step"]] = e
        cls.by_step = {}
        for step, e in cls.registrations.items():
            host = urlparse(json.loads(e["body"])["endpoint"]).hostname
            match = [c for c in others if host in cls.names(c)]
            if len(match) == 1:
                cls.by_step[step] = match[0]
        env = dict(x.split("=", 1) for x in cls.engine_inspect["Config"]["Env"] if "=" in x)
        cls.grants_in_engine = env.get("ENGINE_GRANTS_DIR", "")
        cls.grants_host = None
        if cls.grants_in_engine:
            cls.grants_host = cls.host_path(cls.engine_inspect, cls.grants_in_engine)

    @staticmethod
    def names(c):
        out = {c["Name"].lstrip("/"), c["Config"].get("Hostname", ""), c["Id"], c["Id"][:12]}
        for n in (c["NetworkSettings"].get("Networks") or {}).values():
            out.update(n.get("Aliases") or [])
            out.update(n.get("DNSNames") or [])
            if n.get("IPAddress"):
                out.add(n["IPAddress"])
        out.discard("")
        return out

    @staticmethod
    def host_path(c, inside):
        """The host path of a path inside the container, by its mounts, or None."""
        best = None
        for m in c.get("Mounts") or []:
            d = m["Destination"].rstrip("/") or "/"
            if inside == d or inside.startswith(d.rstrip("/") + "/"):
                if best is None or len(d) > len(best[0]):
                    best = (d, m)
        if best is None or best[1].get("Type") != "bind":
            return None
        d, m = best
        return Path(m["Source"] + inside[len(d):])

    @staticmethod
    def container_path(c, host):
        """The path inside the container of a host path, by its bind mounts, or None."""
        host = str(host)
        best = None
        for m in c.get("Mounts") or []:
            s = str(m.get("Source", "")).rstrip("/")
            if m.get("Type") == "bind" and (host == s or host.startswith(s + "/")):
                if best is None or len(s) > len(best[0]):
                    best = (s, m)
        if best is None:
            return None
        s, m = best
        return m["Destination"].rstrip("/") + host[len(s):] if host != s else m["Destination"]

    @classmethod
    def cleanup(cls):
        try:
            if getattr(cls, "override", None) and cls.override.exists():
                cls.compose("--profile", "*", "down", "-v", "--remove-orphans")
            for c in cls.inspect_all():
                if cls.ours(c):
                    docker("rm", "-f", c["Id"])
            for kind in ("network", "volume"):
                for n in docker(kind, "ls", "-q", "--filter", "label=com.docker.compose.project=" + cls.name).stdout.split():
                    docker(kind, "rm", n)
            left = [c["Name"] for c in cls.inspect_all() if cls.ours(c)] + \
                docker("network", "ls", "-q", "--filter", "label=com.docker.compose.project=" + cls.name).stdout.split()
        finally:
            cls.empty_grants_dir()
            shutil.rmtree(cls.base, ignore_errors=True)
        if left:
            raise AssertionError("Docker objects of the test left behind: %s" % left)

    # helpers for the tests
    def container_of(self, step):
        c = self.by_step.get(step)
        if c is None:
            self.fail("no container registered for step %s (registrations: %s)" % (
                step, {s: json.loads(e["body"])["endpoint"] for s, e in self.registrations.items()}))
        return c

    def exec(self, c, script, timeout=120):
        return docker("exec", c["Id"], "sh", "-c", script, timeout=timeout)

    def grant_files(self):
        """The grants folder as the engine sees it, read inside the engine stand-in (as the engine's
        user): name -> {mode, uid, file, content}."""
        if not self.grants_in_engine or self.grants_host is None:
            self.fail("the engine has no grants folder (ENGINE_GRANTS_DIR %r, mounted from %s)" % (
                self.grants_in_engine, self.grants_host))
        r = docker("exec", self.engine, "node", "-e", READ_GRANTS_JS)
        self.assertEqual(r.returncode, 0, "the engine's user cannot read the grants folder: %s" % r.stderr)
        return json.loads(r.stdout)


class FreshProjectTest(BuildWorkersRig, unittest.TestCase):
    """`ap build-workers S-1 notes-app` for a project folder that does not exist yet."""
    PROJECT = "notes-app"

    @classmethod
    def prepare_project(cls):
        assert not cls.project.exists()

    def test_the_project_folder_an_empty_git_folder_and_every_step_folder_are_made(self):
        self.assertTrue(self.project.is_dir() and not self.project.is_symlink(), "no project folder")
        git = self.project / ".git"
        self.assertTrue(git.is_dir() and not git.is_symlink(), "no .git folder")
        self.assertEqual(list(git.iterdir()), [], ".git is not empty")
        want = set()
        for _, entries in self.steps:
            want |= writable_folders(entries)[1]
        self.assertIn("gen", want)  # the pattern still has dir/** entries
        for f in sorted(want):
            self.assertTrue((self.project / f).is_dir(), "the folder %s/ was not made" % f)
        files = [k for k, v in tree(self.project).items() if v[0] != "dir"]
        self.assertEqual(files, [], "empty folders are no project files: build-workers wrote files")

    def test_one_container_per_step_each_registered_for_its_step_with_its_own_endpoint(self):
        names = [s for s, _ in self.steps]
        self.assertEqual(len(self.step_containers), len(names),
                         "containers: %s" % [c["Name"] for c in self.step_containers])
        self.assertEqual(sorted(self.registrations), sorted(names))
        for s, e in self.registrations.items():
            self.assertEqual(e["grant"]["slice"], SLICE)
        self.assertEqual(sorted(self.by_step), sorted(names),
                         "each registered endpoint must name exactly one step container")
        self.assertEqual(len({c["Id"] for c in self.by_step.values()}), len(names), "two steps share a container")

    def test_every_step_container_runs_under_gvisor_read_only_and_unprivileged(self):
        for s, _ in self.steps:
            c = self.container_of(s)
            with self.subTest(step=s):
                self.assertTrue(c["State"]["Running"], "not running: %s" % c["State"])
                self.assertEqual(c["HostConfig"].get("Runtime"), "runsc")
                self.assertTrue(c["HostConfig"].get("ReadonlyRootfs"))
                self.assertFalse(c["HostConfig"].get("Privileged"))
                self.assertNotIn("docker.sock", json.dumps(c.get("Mounts")))

    def test_one_grant_file_per_container_mode_0600_named_by_its_token_hash(self):
        files = self.grant_files()
        self.assertEqual(len(files), len(self.steps), "grant files: %s" % sorted(files))
        steps, calls = [], []
        engine_uid = (self.engine_inspect["Config"].get("User") or "").split(":")[0]
        for name, info in files.items():
            with self.subTest(file=name):
                self.assertRegex(name, HEX64)
                self.assertTrue(info["file"], "not a regular file")
                self.assertEqual(info["mode"] & 0o7777, 0o600)
                if engine_uid.isdigit():  # mode 0600 and readable by the engine's user: it owns them
                    self.assertEqual(info["uid"], int(engine_uid))
                g = json.loads(info["content"])
                self.assertEqual(sorted(g), ["call_token", "slice", "step"])
                self.assertEqual(g["slice"], SLICE)
                steps.append(g["step"])
                calls.append(g["call_token"])
                reg = self.registrations.get(g["step"])
                self.assertIsNotNone(reg, "no registration for %s" % g["step"])
                self.assertEqual(hashlib.sha256(reg["token"].encode()).hexdigest(), name,
                                 "the file is not named by the SHA-256 of the token %s was registered with" % g["step"])
                self.assertNotEqual(g["call_token"], reg["token"], "the call token is the worker's own token")
                self.assertGreaterEqual(len(g["call_token"]), 32)
        self.assertEqual(sorted(steps), sorted(s for s, _ in self.steps))
        self.assertEqual(len(set(calls)), len(calls), "two workers share a call token")
        session = {v for k, v in self.values.items() if k.endswith("_TOKEN")}
        tokens = set(calls) | {e["token"] for e in self.registrations.values()}
        self.assertEqual(tokens & session, set(), "a worker token is one of the session's tokens")

    def test_each_container_holds_its_call_token_in_its_environment_never_on_its_command_line(self):
        files = self.grant_files()
        for info in files.values():
            g = json.loads(info["content"])
            c = self.container_of(g["step"])
            with self.subTest(step=g["step"]):
                env = [x.split("=", 1)[1] for x in c["Config"]["Env"] if "=" in x]
                self.assertIn(g["call_token"], env, "the container was not given its call token")
                argv = json.dumps([c.get("Path"), c.get("Args"), c["Config"].get("Cmd"), c["Config"].get("Entrypoint")])
                self.assertNotIn(g["call_token"], argv)
                for other in files.values():
                    og = json.loads(other["content"])
                    if og["step"] != g["step"]:
                        self.assertNotIn(og["call_token"], json.dumps(c["Config"]["Env"]),
                                         "the container holds %s's call token" % og["step"])

    def test_the_grants_are_read_by_the_engine_as_its_own_user(self):
        user = (self.cfg["services"]["engine"].get("user") or "")
        self.assertEqual(self.engine_inspect["Config"].get("User") or "", user,
                         "the stand-in does not run as the engine service's user")
        for s, e in self.registrations.items():
            self.assertFalse(e.get("err"), "%s: the engine could not read its grant: %s" % (s, e.get("err")))
        bad = [e for e in self.log() if e.get("url", "").startswith("/workers") and e.get("err")]
        self.assertEqual(bad, [], "grant files the engine's user cannot read or parse")

    def test_only_the_engine_mounts_the_grants_folder_and_read_only(self):
        self.grant_files()
        g = str(self.grants_host)
        mine = [m for m in self.engine_inspect["Mounts"] if g == m["Source"] or g.startswith(m["Source"].rstrip("/") + "/")]
        self.assertTrue(mine, "the engine does not mount the grants folder")
        for m in mine:
            self.assertFalse(m["RW"], "the engine mounts the grants folder writable: %s" % m)
        for c in self.inspect_all():
            if c["Id"] == self.engine:
                continue
            for m in c.get("Mounts") or []:
                src = str(m.get("Source", "")).rstrip("/")
                if src and (g == src or g.startswith(src + "/") or src.startswith(g + "/")):
                    self.fail("%s mounts the grants folder (%s)" % (c["Name"], src))

    def test_a_project_folder_with_a_file_a_symlink_or_a_history_is_refused_and_left_alone(self):
        """The engine's ProjectExists rule (owner, 2026-11-13): a folder holding any file or symlink at
        any depth, or a non-empty .git, is refused; nothing is started, granted or changed."""
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
                before = (tree(p), sorted(c["Id"] for c in self.inspect_all() if self.ours(c)),
                          sorted(self.grant_files()), len(self.log()))
                r = self.ap("build-workers", SLICE, p.name)
                self.assertNotEqual(r.returncode, 0, "build-workers accepted a folder with %s" % label)
                self.assertTrue(str(p) in r.stderr or p.name in r.stderr, "the refusal does not name the folder: %s" % r.stderr)
                after = (tree(p), sorted(c["Id"] for c in self.inspect_all() if self.ours(c)),
                         sorted(self.grant_files()), len(self.log()))
                self.assertEqual(tree_diff(before[0], after[0]), [], "the folder was changed")
                self.assertEqual(after[1], before[1], "containers were started or removed")
                self.assertEqual(after[2], before[2], "grants were written or removed")
                self.assertEqual(after[3], before[3], "the engine was called")

    def test_running_build_workers_again_changes_nothing(self):
        def snapshot():
            cs = sorted((c["Id"], c["Created"], c["State"].get("StartedAt"), c["RestartCount"])
                        for c in self.inspect_all() if self.ours(c))
            files = self.grant_files()
            grants = {n: (i["content"], i["mode"], i["uid"]) for n, i in files.items()}
            return cs, grants, tree(self.project)
        before = snapshot()
        seen = len(self.log())
        r = self.ap("build-workers", SLICE, self.PROJECT)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        after = snapshot()
        self.assertEqual(after[0], before[0], "containers were replaced or restarted")
        self.assertEqual(sorted(after[1]), sorted(before[1]), "grant files were added or removed")
        self.assertEqual(after[1], before[1], "grant files changed")
        self.assertEqual(tree_diff(before[2], after[2]), [], "the project folder changed")
        old = {s: (e["token"], json.loads(e["body"])["endpoint"]) for s, e in self.registrations.items()}
        for e in self.log()[seen:]:
            if e.get("url", "").startswith("/workers") and e.get("status") == 200:
                s = e["grant"]["step"]
                self.assertEqual((e["token"], json.loads(e["body"])["endpoint"]), old.get(s),
                                 "the second run registered %s with another token or endpoint" % s)


class ProjectWithHistoryTest(BuildWorkersRig, unittest.TestCase):
    """`ap build-workers S-1 notes-history` for a new project, whose version history (a git repository
    with a first commit, as the project worker leaves it after contract-check) and files are then
    made by the host while the containers run, as the project worker would make them."""
    PROJECT = "notes-history"
    PROBE_FOLDERS = ("", "api", "features", "gen", "vendor", "tests", "internal", "internal/app", "cmd")

    @classmethod
    def git(cls, *args):
        env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
        env.update(GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL="/dev/null", HOME=str(cls.base),
                   GIT_AUTHOR_NAME="agentic-platform", GIT_AUTHOR_EMAIL="noreply@localhost",
                   GIT_COMMITTER_NAME="agentic-platform", GIT_COMMITTER_EMAIL="noreply@localhost",
                   GIT_AUTHOR_DATE="2026-11-13T00:00:00Z", GIT_COMMITTER_DATE="2026-11-13T00:00:00Z")
        r = subprocess.run(["git", "-C", str(cls.project), *args], env=env, capture_output=True, text=True, timeout=60)
        if r.returncode != 0:
            raise AssertionError("git %s: %s" % (" ".join(args), r.stderr))
        return r.stdout

    @classmethod
    def prepare_project(cls):
        assert not cls.project.exists()

    @classmethod
    def after_build(cls):
        for f in cls.PROBE_FOLDERS:
            (cls.project / f).mkdir(parents=True, exist_ok=True)
        (cls.project / "api/openapi.yaml").write_text("openapi: 3.1.0\ninfo: {title: notes, version: '1'}\npaths: {}\n")
        (cls.project / "features/slice.feature").write_text("Feature: notes\n")
        cls.git("init", "-q", "-b", "main")
        cls.git("add", "--", "api/openapi.yaml", "features/slice.feature")
        cls.git("commit", "-q", "-m", "Step contract-check accepted in R-1")
        cls.head = cls.git("rev-parse", "HEAD").strip()

    # probing
    def plant(self, tag):
        for f in self.PROBE_FOLDERS:
            d = self.project / f
            (d / ("planted-" + tag)).write_text("planted\n")
            (d / ("doomed-" + tag)).write_text("doomed\n")
            (d / ("movable-" + tag)).write_text("movable\n")

    def probe(self, step):
        """Try, in step's container, to create, change, remove, rename and make a folder in every probe
        folder; return the failures against the step's writable entries, judged on the host."""
        c = self.container_of(step)
        tag = "%s-%s" % (step, secrets.token_hex(3))
        self.plant(tag)
        before = tree(self.project)
        lines, expect = [], {}
        for f in self.PROBE_FOLDERS:
            inside = self.container_path(c, self.project / f)
            allowed = may_write(self.writable[step], f)
            expect[f] = allowed
            if inside is None:
                continue
            q = lambda n: shlex.quote(inside.rstrip("/") + "/" + n)  # noqa: E731
            lines += ["(echo new > %s) 2>/dev/null" % q("create-" + tag),
                      "(echo more >> %s) 2>/dev/null" % q("planted-" + tag),
                      "(rm -f %s) 2>/dev/null" % q("doomed-" + tag),
                      "(mv %s %s) 2>/dev/null" % (q("movable-" + tag), q("moved-" + tag)),
                      "(mkdir %s) 2>/dev/null" % q("dir-" + tag)]
        r = self.exec(c, "; ".join(lines + ["true"]), timeout=180)
        self.assertEqual(r.returncode, 0, r.stderr)
        after = tree(self.project)
        failures, targets = [], set()
        for f, allowed in expect.items():
            d = self.project / f
            p = lambda n: str((Path(f) / n)) if f else n  # noqa: E731
            done = {"create": (d / ("create-" + tag)).is_file(),
                    "change": (d / ("planted-" + tag)).read_text() != "planted\n" if (d / ("planted-" + tag)).exists() else True,
                    "remove": not (d / ("doomed-" + tag)).exists(),
                    "rename": (d / ("moved-" + tag)).exists(),
                    "mkdir": (d / ("dir-" + tag)).is_dir()}
            for op, ok in done.items():
                if ok != allowed:
                    failures.append("%s %s in %s/: %s" % (step, op, f or ".", "done" if ok else "refused"))
            if allowed:
                targets |= {p(n + tag) for n in ("create-", "planted-", "doomed-", "movable-", "moved-", "dir-")}
        stray = [k for k in tree_diff(before, after) if k not in targets]
        if stray:
            failures.append("%s changed paths outside its writable folders: %s" % (step, stray[:20]))
        return failures

    def git_state(self):
        return tree(self.project / ".git")

    def test_the_review_container_writes_only_its_witness_folder(self):
        self.assertEqual(self.writable["review"], ["review/**"])
        before = self.git_state()
        self.assertEqual(self.probe("review"), [])
        self.assertEqual(tree_diff(before, self.git_state()), [])

    def test_each_container_but_generates_writes_only_its_own_writable_paths(self):
        for s, _ in self.steps:
            if s == "generate":
                continue
            with self.subTest(step=s):
                self.assertEqual(self.probe(s), [])

    def test_contract_check_writes_only_api_and_features(self):
        self.assertEqual(writable_folders(self.writable["contract-check"]), (False, {"api", "features"}))
        self.assertEqual(self.probe("contract-check"), [])

    def test_the_generate_container_writes_the_project_root_but_not_git(self):
        self.assertTrue(writable_folders(self.writable["generate"])[0], "generate no longer writes the root")
        before = self.git_state()
        self.assertEqual(self.probe("generate"), [])
        self.assertEqual(tree_diff(before, self.git_state()), [], ".git changed")

    def test_no_step_container_can_create_change_or_remove_anything_in_git(self):
        objects = sorted(p for p in (self.project / ".git/objects").rglob("*") if p.is_file())
        self.assertTrue(objects, "the repository has no objects")
        for s, _ in self.steps:
            c = self.container_of(s)
            with self.subTest(step=s):
                g = self.container_path(c, self.project / ".git")
                self.assertIsNotNone(g, "%s does not see .git" % s)
                obj = self.container_path(c, objects[0])
                q = lambda rel: shlex.quote(g.rstrip("/") + "/" + rel)  # noqa: E731
                tag = secrets.token_hex(3)
                ops = ["echo x > %s" % q("new-" + tag),
                       "echo x >> %s" % q("HEAD"),
                       "echo '[core]' > %s" % q("config"),
                       "printf 0000000000000000000000000000000000000000 > %s" % q("refs/heads/main"),
                       "mkdir %s" % q("refs/heads/x-" + tag),
                       "mkdir %s" % q("hooks"),
                       "echo x > %s" % q("hooks/pre-commit"),
                       "chmod 777 %s" % shlex.quote(g),
                       "mv %s %s" % (q("HEAD"), q("HEAD-" + tag)),
                       "rm -f %s" % shlex.quote(obj),
                       "rm -f %s" % q("index"),
                       "rm -rf %s/*" % shlex.quote(g),
                       "mv %s %s" % (shlex.quote(g), shlex.quote(g.rstrip("/") + "-" + tag)),
                       "rmdir %s" % shlex.quote(g),
                       "rm -rf %s" % shlex.quote(g)]
                before = self.git_state()
                before_mode = stat.S_IMODE((self.project / ".git").stat().st_mode)
                r = self.exec(c, "; ".join("(%s) 2>/dev/null" % o for o in ops) + "; true", timeout=180)
                self.assertEqual(r.returncode, 0, r.stderr)
                self.assertTrue((self.project / ".git").is_dir(), ".git is gone")
                self.assertEqual(tree_diff(before, self.git_state()), [], "%s changed .git" % s)
                self.assertEqual(stat.S_IMODE((self.project / ".git").stat().st_mode), before_mode)
                self.assertEqual(self.git("rev-parse", "HEAD").strip(), self.head)
                self.assertFalse(any(p.name.startswith(".git-") for p in self.project.iterdir()), "a copy of .git appeared")

    def test_a_file_written_on_the_host_is_seen_at_once_in_every_step_container(self):
        tag = secrets.token_hex(4)
        api = self.project / "api/openapi.yaml"
        feature = self.project / "features/slice.feature"
        fresh = self.project / "internal" / ("seen-" + tag)
        ref = self.project / ".git/refs/heads" / ("seen-" + tag)
        gone = self.project / "cmd" / ("gone-" + tag)
        gone.write_text("soon gone\n")
        for s, _ in self.steps:  # every container sees the file before it is removed
            c = self.container_of(s)
            r = self.exec(c, "cat %s" % shlex.quote(self.container_path(c, gone) or "/nonexistent"))
            self.assertEqual(r.stdout, "soon gone\n", "%s: %s" % (s, r.stderr))
        # as the project worker writes: in place, by a new file, by rename over, in .git, and a removal
        with open(api, "r+") as fh:
            fh.write("openapi: 3.1.0 # " + tag + "\n")
            fh.truncate()
        fresh.write_text("fresh " + tag + "\n")
        tmp = feature.with_name(".tmp-" + tag)
        tmp.write_text("Feature: renamed " + tag + "\n")
        os.replace(tmp, feature)
        ref.write_text(self.head + "\n")
        gone.unlink()
        want = {api: api.read_text(), fresh: fresh.read_text(), feature: feature.read_text(), ref: ref.read_text()}
        for s, _ in self.steps:
            c = self.container_of(s)
            with self.subTest(step=s):
                for host, text in want.items():
                    inside = self.container_path(c, host)
                    self.assertIsNotNone(inside, "%s does not see %s" % (s, host))
                    r = self.exec(c, "cat %s" % shlex.quote(inside))
                    self.assertEqual(r.stdout, text, "%s sees stale %s: %s" % (s, host.name, r.stderr))
                r = self.exec(c, "test -e %s" % shlex.quote(self.container_path(c, gone)))
                self.assertNotEqual(r.returncode, 0, "%s still sees a removed file" % s)

    def test_a_step_container_without_its_token_cannot_register(self):
        url = "http://engine:8080/workers?api-version=" + API_VERSION
        session_worker = self.values.get("AP_WORKER_TOKEN", "")
        for s, _ in self.steps:
            c = self.container_of(s)
            with self.subTest(step=s):
                body = shlex.quote(json.dumps({"endpoint": "http://intruder-%s:8080" % s}))
                base = "wget -q -O - -T 20 --header 'Content-Type: application/json' --post-data %s" % body
                seen = len(self.log())
                for label, hdr in (("no token", ""),
                                   ("a made-up token", "--header %s" % shlex.quote("Authorization: Bearer " + secrets.token_hex(32))),
                                   ("the session's worker token", "--header %s" % shlex.quote("Authorization: Bearer " + session_worker))):
                    r = self.exec(c, "%s %s %s 2>&1; echo exit=$?" % (base, hdr, shlex.quote(url)))
                    self.assertNotIn("exit=0", r.stdout, "%s registered with %s: %s" % (s, label, r.stdout))
                new = [e for e in self.log()[seen:] if e.get("url", "").startswith("/workers")]
                self.assertEqual(len(new), 3, "the requests did not reach the engine: %s" % r.stdout)
                self.assertTrue(all(e["status"] == 401 for e in new), new)
                self.assertNotIn("intruder", json.dumps([e for e in self.log() if e.get("status") == 200]))


if __name__ == "__main__":
    unittest.main()
