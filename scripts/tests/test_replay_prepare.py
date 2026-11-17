"""T024 unit (2): `src/tests/recorded/replay.sh prepare <set> <out>` (tasks.md T024 design notes,
research D44), judged on the folder it writes. No Docker: prepare only writes files.

Rules: <out> gets `mappings/` and `__files/` holding every file of `recordings/` and of
`steps/<set>/` (and nothing else of the set, such as answers.json or files/); it refuses what
`start` refuses (a mapping that proxies, a mapping that does not match the exact URL, an empty set),
and an unknown set or one that leaves `steps/`, leaving no mappings in <out>; and it writes the
replay server's hardening, which has one home in replay.sh, as the env file `<out>/wiremock.env`
the Compose override reads: one line `WIREMOCK_OPTIONS=...` with the admin API behind basic auth
(`--admin-api-basic-auth <user>:<password>`, a random password of at least 32 characters, new on
each prepare), a request-journal limit (`--max-request-journal-entries <n>`) and no banner
(`--disable-banner`); the file is readable by its owner only.

Planted cases run against a copy of src/tests/recorded in a throwaway folder, so the repository's
recordings are never changed.
"""
import json
import os
import re
import shutil
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
RECORDED = REPO / "src/tests/recorded"
# AP_TEST_REPLAY points the tests at another copy of replay.sh (to see them fail against a mutant)
REPLAY = Path(os.environ.get("AP_TEST_REPLAY") or RECORDED / "replay.sh")
TMP_BASE = os.environ.get("XDG_RUNTIME_DIR") or None
SETS = ("happy", "resume", "planted/outside-scope")


def files_of(folder):
    """{basename: bytes} of every file under folder (WireMock reads subfolders too)."""
    out = {}
    for d, _, names in os.walk(folder):
        for n in names:
            assert n not in out, "%s twice under %s" % (n, folder)
            out[n] = Path(d, n).read_bytes()
    return out


def options(out):
    env = Path(out) / "wiremock.env"
    lines = [ln for ln in env.read_text().splitlines() if ln.startswith("WIREMOCK_OPTIONS=")]
    assert len(lines) == 1, env.read_text()
    v = lines[0][len("WIREMOCK_OPTIONS="):]
    if len(v) >= 2 and v[0] == v[-1] and v[0] in "'\"":
        v = v[1:-1]
    return v


class ReplayPrepareTest(unittest.TestCase):
    def setUp(self):
        self.base = Path(tempfile.mkdtemp(prefix="test-replay-prepare-", dir=TMP_BASE))
        self.addCleanup(shutil.rmtree, self.base, ignore_errors=True)
        # a copy of src/tests/recorded (recordings and the step sets) beside the script under test
        self.root = self.base / "repo"
        self.rec = self.root / "src/tests/recorded"
        self.rec.mkdir(parents=True)
        shutil.copy2(REPLAY, self.rec / "replay.sh")
        shutil.copytree(RECORDED / "recordings", self.rec / "recordings")
        shutil.copytree(RECORDED / "steps", self.rec / "steps")
        (self.root / "src/deploy").mkdir()
        shutil.copy2(REPO / "src/deploy/images.lock", self.root / "src/deploy/images.lock")
        self.bin = self.base / "bin"
        self.bin.mkdir()
        # prepare starts nothing: a docker that fails loudly proves it is never called
        (self.bin / "docker").write_text("#!/bin/sh\necho \"docker called: $*\" >&2\nexit 99\n")
        (self.bin / "docker").chmod(0o755)

    def prepare(self, set_, out, script=None, umask=None):
        e = {k: v for k, v in os.environ.items() if not k.startswith(("REPLAY_", "AP_"))}
        e["PATH"] = str(self.bin) + os.pathsep + e.get("PATH", "")
        return subprocess.run(["sh", str(script or self.rec / "replay.sh"), "prepare", set_, str(out)],
                              env=e, capture_output=True, text=True, timeout=60, cwd=str(self.base),
                              preexec_fn=(lambda: os.umask(umask)) if umask is not None else None)

    def assertRefused(self, r, out, what):
        self.assertNotEqual(r.returncode, 0, "%s was not refused: %s" % (what, r.stderr))
        self.assertNotIn("docker called", r.stderr)
        self.assertFalse((Path(out) / "mappings").exists() and any(os.scandir(Path(out) / "mappings")),
                         "%s: mappings were written to %s" % (what, out))

    def assertCleanPrepares(self):
        """The unplanted copy prepares, so a refusal below is for the planted case, not for prepare missing."""
        out = self.base / "out-clean"
        r = self.prepare("happy", out)
        self.assertEqual(r.returncode, 0, "the clean copy does not prepare: " + r.stderr)
        shutil.rmtree(out)

    def plant(self, where, name, mapping):
        d = self.rec / where / "mappings"
        (d / name).write_text(json.dumps(mapping))

    # the merge
    def test_prepare_merges_the_recordings_and_the_set(self):
        for s in SETS:
            with self.subTest(set=s):
                out = self.base / ("out-" + s)
                r = self.prepare(s, out)
                self.assertEqual(r.returncode, 0, r.stderr)
                self.assertNotIn("docker called", r.stderr)
                for sub in ("mappings", "__files"):
                    want = {**files_of(self.rec / "recordings" / sub), **files_of(self.rec / "steps" / s / sub)}
                    self.assertTrue(want, sub)
                    self.assertEqual(files_of(out / sub), want, "%s/%s" % (s, sub))
                left = {p.name for p in out.iterdir()} - {"mappings", "__files", "wiremock.env"}
                self.assertEqual(left, set(), "only mappings/, __files/ and wiremock.env belong in <out>")

    def test_prepare_works_against_the_repository_script_in_place(self):
        out = self.base / "out"
        r = self.prepare("happy", out, script=RECORDED / "replay.sh")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(files_of(out / "mappings"),
                         {**files_of(RECORDED / "recordings/mappings"), **files_of(RECORDED / "steps/happy/mappings")})

    # the options env file
    def test_prepare_writes_the_wiremock_options(self):
        out = self.base / "out"
        r = self.prepare("happy", out)
        self.assertEqual(r.returncode, 0, r.stderr)
        env = out / "wiremock.env"
        mode = stat.S_IMODE(env.stat().st_mode)
        self.assertEqual(mode & 0o077, 0, "wiremock.env holds the admin password; mode %o" % mode)
        o = options(out)
        m = re.search(r"(?:^| )--admin-api-basic-auth ([^ :]+):([^ ]+)(?: |$)", o)
        assert m is not None, o
        self.assertRegex(m.group(2), r"^[A-Za-z0-9]{32,}$", "the password must be random and long")
        j = re.search(r"(?:^| )--max-request-journal-entries ([0-9]+)(?: |$)", o)
        assert j is not None, o
        self.assertGreater(int(j.group(1)), 0)
        self.assertRegex(o, r"(?:^| )--disable-banner(?: |$)")
        self.assertNotIn("proxy", o.lower(), "no proxying option")

    def test_each_prepare_makes_a_new_password(self):
        pws = set()
        for i in range(2):
            out = self.base / ("out%d" % i)
            self.assertEqual(self.prepare("happy", out).returncode, 0)
            m = re.search(r"--admin-api-basic-auth [^ :]+:([^ ]+)", options(out))
            assert m is not None, options(out)
            pws.add(m.group(1))
        self.assertEqual(len(pws), 2, "the same admin password twice")

    # refusals
    def test_an_unknown_set_is_refused(self):
        self.assertCleanPrepares()
        for s in ("nope", "", "../recordings", "happy/../../recordings", "/etc", "happy/mappings"):
            with self.subTest(set=s):
                out = self.base / ("out-%d" % abs(hash(s)))
                self.assertRefused(self.prepare(s, out), out, "the set %r" % s)

    def test_a_proxying_mapping_is_refused_in_the_set_and_in_the_recordings(self):
        proxy = {"request": {"method": "POST", "url": "/v1/messages"},
                 "response": {"proxyBaseUrl": "https://api.anthropic.com"}}
        self.assertCleanPrepares()
        for where in ("steps/happy", "recordings"):
            with self.subTest(where=where):
                self.setUp()
                self.plant(where, "zz-planted-proxy.json", proxy)
                out = self.base / "out"
                self.assertRefused(self.prepare("happy", out), out, "a proxying mapping in " + where)

    def test_a_mapping_that_does_not_match_the_exact_url_is_refused(self):
        loose = {
            "urlPath": {"request": {"method": "POST", "urlPath": "/v1/messages"}, "response": {"status": 200}},
            "urlPattern": {"request": {"method": "POST", "urlPattern": "/v1/.*"}, "response": {"status": 200}},
            "urlPathPattern": {"request": {"method": "POST", "urlPathPattern": "/v1/.*"}, "response": {"status": 200}},
            "queryParameters": {"request": {"method": "POST", "url": "/v1/messages",
                                            "queryParameters": {"beta": {"equalTo": "true"}}},
                                "response": {"status": 200}},
            "no url": {"request": {"method": "POST"}, "response": {"status": 200}},
        }
        self.assertCleanPrepares()
        for what, m in loose.items():
            with self.subTest(what=what):
                self.setUp()
                self.plant("steps/happy", "zz-planted.json", m)
                out = self.base / "out"
                self.assertRefused(self.prepare("happy", out), out, "a mapping with " + what)

    def test_a_mapping_in_a_subfolder_is_judged_too(self):
        self.assertCleanPrepares()
        d = self.rec / "steps/happy/mappings/deeper"
        d.mkdir()
        (d / "p.json").write_text(json.dumps({"request": {"url": "/v1/messages"},
                                              "response": {"proxyBaseUrl": "https://api.anthropic.com"}}))
        out = self.base / "out"
        self.assertRefused(self.prepare("happy", out), out, "a proxying mapping in a subfolder")

    def test_a_mapping_that_is_not_json_is_refused(self):
        self.assertCleanPrepares()
        (self.rec / "steps/happy/mappings/zz-broken.json").write_text("{not json")
        out = self.base / "out"
        self.assertRefused(self.prepare("happy", out), out, "a broken mapping")

    # review regressions
    def test_modes_are_readable_by_the_replay_server_whatever_the_umask(self):
        """The server runs as another user (65532): folders 0755, files 0644, wiremock.env 0600."""
        out = self.base / "out"
        r = self.prepare("happy", out, umask=0o077)
        self.assertEqual(r.returncode, 0, r.stderr)
        for d, dirs, names in os.walk(out):
            self.assertEqual(stat.S_IMODE(os.stat(d).st_mode), 0o755, d)
            for n in names:
                f = Path(d, n)
                want = 0o600 if f == out / "wiremock.env" else 0o644
                self.assertEqual(stat.S_IMODE(f.stat().st_mode), want, f)

    def test_a_body_file_in_the_recordings_and_in_the_set_is_refused(self):
        self.assertCleanPrepares()
        name = sorted(p.name for p in (self.rec / "recordings/__files").iterdir())[0]
        (self.rec / "steps/happy/__files" / name).write_text("another body\n")
        out = self.base / "out"
        r = self.prepare("happy", out)
        self.assertRefused(r, out, "a body file named %s in both" % name)
        self.assertIn(name, r.stderr)

    def test_an_existing_out_that_prepare_did_not_write_is_refused_and_left_untouched(self):
        self.assertCleanPrepares()
        for what in ("a folder holding a file", "an empty folder", "a file"):
            with self.subTest(what=what):
                out = self.base / ("out-" + what.replace(" ", "-"))
                if what == "a file":
                    out.write_text("mine\n")
                else:
                    out.mkdir()
                    if what == "a folder holding a file":
                        (out / "keep.txt").write_text("mine\n")
                before = sorted(p.name for p in out.iterdir()) if out.is_dir() else out.read_text()
                r = self.prepare("happy", out)
                self.assertNotEqual(r.returncode, 0, "%s was overwritten: %s" % (what, r.stderr))
                after = sorted(p.name for p in out.iterdir()) if out.is_dir() else out.read_text()
                self.assertEqual(after, before, "%s was changed" % what)
                if what == "a folder holding a file":
                    self.assertEqual((out / "keep.txt").read_text(), "mine\n")

    def test_an_out_an_earlier_prepare_wrote_is_prepared_again(self):
        out = self.base / "out"
        self.assertEqual(self.prepare("happy", out).returncode, 0)
        r = self.prepare("resume", out)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(files_of(out / "__files"),
                         {**files_of(self.rec / "recordings/__files"), **files_of(self.rec / "steps/resume/__files")})

    def test_wrong_use_exits_2(self):
        for args in (["prepare"], ["prepare", "happy"]):
            with self.subTest(args=args):
                r = subprocess.run(["sh", str(self.rec / "replay.sh"), *args], capture_output=True,
                                   text=True, timeout=60)
                self.assertEqual(r.returncode, 2, r.stderr)


if __name__ == "__main__":
    unittest.main()
