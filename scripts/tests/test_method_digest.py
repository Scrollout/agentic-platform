"""Tests for `scripts/method-digest.py` (feature 006, task T002); they judge T005.

Rules: src/engine/method/README.md ("The declaration", "The identity", "The command") and
research R1 and R2 of specs/006-method-record. Every case runs the real script, by absolute path,
with its working directory inside a throwaway Git repository built here (never the real tree), on
a small declaration of its own. The expected identity is recomputed in this file from the README's
rules, without the script: the files are found by walking the folder (not by git), matched with
fnmatch.fnmatchcase, hashed, ordered by the bytes of the path, then the pin lines in declared order.

Cases (class MethodDigestTest unless noted):
- identity: test_identity_is_the_sha256_of_the_manifest, test_line_and_file_content,
  test_second_run_changes_nothing, test_a_stale_file_is_rewritten,
  test_same_tree_cloned_elsewhere_gives_the_same_identity,
  test_runs_from_a_subfolder_of_the_work_tree
- files: test_changing_an_included_file_changes_it, test_a_changed_declaration_changes_it,
  test_excluded_files_do_not_matter, test_ignored_file_does_not_matter,
  test_a_path_outside_every_include_does_not_matter, test_readme_exclusion_is_exact_and_case_sensitive,
  test_untracked_included_file_counts, test_tracked_file_deleted_from_the_work_tree_is_left_out,
  test_star_matches_slash_and_nesting, test_odd_names_and_empty_file, test_file_mode_does_not_matter,
  test_content_not_name_matters (rename keeps content but changes path)
- pins: test_listed_pin_reference_change_changes_it, test_unlisted_pin_change_does_not,
  test_images_lock_comments_and_alignment_do_not_matter, test_pins_come_in_declared_order
- --check: test_check_equal, test_check_stale, test_check_missing
- --list: test_list_prints_the_manifest_and_writes_nothing
- exit 2, writing nothing: test_bad_declaration_exits_2_writing_nothing (subtests),
  test_symbolic_link_among_selected_files, test_symbolic_link_outside_the_selection_is_fine,
  test_outside_a_git_work_tree, test_unknown_option (class UsageTest not needed: folded here)
- the independent computation checks itself: ModelSelfTest.
Edge cases thought through and checked: paths with spaces, an empty file, nested directories,
`*` across `/`, a top-level file matched by `README.md`, `NOTREADME.md`, `readme.md`, non-ASCII
names, the identity file present with stale content, file mode changes, a rename with the same
content, comments and column alignment in images.lock, a declaration that is not an object, an
empty-string entry, every key in each bad-declaration kind.
"""
import fnmatch
import hashlib
import json
import os
import subprocess
import sys
import tempfile
import unittest

import gitenv

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(os.path.dirname(HERE), "method-digest.py")
IDENTITY_FILE = "src/engine/method/method.sha256"
DECL_FILE = "src/engine/method/method.json"
LOCK_FILE = "src/deploy/images.lock"

DECLARATION = {
    "include": ["src/engine/*", "src/x/*", "docs/*", "README*"],
    "exclude": ["README.md", "*/README.md", "*_test.go", "*/testdata/*", IDENTITY_FILE],
    "pins": ["golang", "alpine"],
}

IMAGES_LOCK = """# Every image, pinned.
# name                     reference
docker-agent               docker/docker-agent:1.0.0@sha256:aaaa
golang                     golang:1.26-alpine@sha256:bbbb
# golang-race: a comment between lines.
golang-race                golang:1.26.11-bookworm@sha256:cccc
alpine                     alpine:3.22@sha256:dddd
nginx                      nginx:1.29-alpine@sha256:eeee
"""

# Files of the fixture: path -> bytes. Every one is tracked unless IGNORED.
FILES = {
    "README.md": b"top readme\n",
    "README.txt": b"top readme text\n",
    ".gitignore": b"ignored.log\ngen/\n",
    "docs/README.md": b"docs readme\n",
    "docs/guide.md": b"guide\n",
    "src/engine/a.go": b"package a\n",
    "src/engine/a_test.go": b"package a\n// test\n",
    "src/engine/README.md": b"engine readme\n",
    "src/engine/sub/b.go": b"package b\n",
    "src/engine/sub/deep/c.go": b"package c\n",
    "src/engine/sub/README.md": b"sub readme\n",
    "src/engine/testdata/t.txt": b"data\n",
    "src/engine/method/README.md": b"contract\n",
    "src/x/with space.txt": b"spaced\n",
    "src/x/empty": b"",
    "src/x/NOTREADME.md": b"not a readme\n",
    "src/x/readme.md": b"lower readme\n",
    "src/x/héllo.txt": b"accent\n",
    "src/other/o.txt": b"outside every include\n",
    "src/deploy/Dockerfile": b"FROM x\n",
}
IGNORED = {
    "src/engine/ignored.log": b"ignored by name\n",
    "src/engine/gen/g.go": b"ignored by folder\n",
}


def sha(b):
    return hashlib.sha256(b).hexdigest()


def read_lock(text):
    """Pin name -> reference as the lock writes it: the first field, then the rest of the line."""
    pins = {}
    for line in text.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        name, _, ref = line.strip().partition(" ")
        pins[name] = ref.strip()
    return pins


def expected_manifest(root, decl, ignored=frozenset()):
    """The manifest, from the README's rules, found by walking `root` (not by git)."""
    files = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d != ".git"]
        for n in filenames:
            full = os.path.join(dirpath, n)
            rel = os.path.relpath(full, root).replace(os.sep, "/")
            if rel in ignored or os.path.islink(full):
                continue
            if not any(fnmatch.fnmatchcase(rel, g) for g in decl["include"]):
                continue
            if any(fnmatch.fnmatchcase(rel, g) for g in decl["exclude"]):
                continue
            files.append(rel)
    files.sort(key=lambda p: p.encode("utf-8"))
    out = ""
    for rel in files:
        with open(os.path.join(root, rel), "rb") as f:
            out += "%s  %s\n" % (sha(f.read()), rel)
    with open(os.path.join(root, LOCK_FILE)) as f:
        pins = read_lock(f.read())
    for name in decl["pins"]:
        out += "pin %s %s\n" % (name, pins[name])
    return out, len(files)


class ModelSelfTest(unittest.TestCase):
    """The independent computation is right on a case small enough to check by eye."""

    def test_known_manifest(self):
        self.assertEqual(sha(b"x"), "2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881")
        with tempfile.TemporaryDirectory() as d:
            os.makedirs(os.path.join(d, "src/deploy"))
            os.makedirs(os.path.join(d, "src/engine"))
            for p, c in (("src/engine/b", b"x"), ("src/engine/B", b"x"), ("src/engine/a_test.go", b"y"),
                         ("src/deploy/images.lock", b"# c\nfoo     bar:1@sha256:z\n")):
                with open(os.path.join(d, p), "wb") as f:
                    f.write(c)
            m, n = expected_manifest(d, {"include": ["src/engine/*"], "exclude": ["*_test.go"], "pins": ["foo"]})
        self.assertEqual(n, 2)
        # byte order: "B" (0x42) before "b" (0x62)
        h = "2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881"
        self.assertEqual(m, "%s  src/engine/B\n%s  src/engine/b\npin foo bar:1@sha256:z\n" % (h, h))


class MethodDigestTest(unittest.TestCase):
    def setUp(self):
        self.home = gitenv.make_home(self)
        self.env = gitenv.clean_env(self.home)
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = os.path.join(tmp.name, "repo")
        self.tmp = tmp.name
        os.makedirs(self.root)
        self.git("init", "-q")
        for p, c in FILES.items():
            self.put(p, c)
        self.write_lock(IMAGES_LOCK)
        self.write_decl(DECLARATION)
        self.git("add", "-A")
        for p, c in IGNORED.items():
            self.put(p, c)
        self.decl = DECLARATION

    # helpers

    def git(self, *args, cwd=None):
        r = subprocess.run(["git", "-c", "user.name=t", "-c", "user.email=t@example.com",
                            "-c", "commit.gpgsign=false"] + list(args), cwd=cwd or self.root,
                           env=self.env, capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, "git %s: %s" % (args, r.stderr))
        return r

    def put(self, rel, content, root=None):
        full = os.path.join(root or self.root, rel)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "wb") as f:
            f.write(content)

    def write_lock(self, text):
        self.put(LOCK_FILE, text.encode())

    def write_decl(self, decl):
        text = decl if isinstance(decl, str) else json.dumps(decl, indent=2)
        self.put(DECL_FILE, text.encode())

    def run_script(self, *args, cwd=None):
        self.assertTrue(os.path.isfile(SCRIPT), "scripts/method-digest.py does not exist")
        return subprocess.run([sys.executable, SCRIPT] + list(args), cwd=cwd or self.root,
                              env=self.env, capture_output=True, text=True, timeout=60)

    def identity_path(self, root=None):
        return os.path.join(root or self.root, IDENTITY_FILE)

    def stored(self):
        try:
            with open(self.identity_path(), "rb") as f:
                return f.read()
        except FileNotFoundError:
            return None

    def expect(self, decl=None):
        """(identity, files, pins, manifest) computed here from the tree as it now is."""
        decl = decl or self.decl
        m, n = expected_manifest(self.root, decl, ignored=set(IGNORED))
        return sha(m.encode()), n, len(decl["pins"]), m

    def line(self, ident, n, m):
        return "method: %s (%d files, %d pins)" % (ident, n, m)

    def generate(self):
        r = self.run_script()
        self.assertEqual(r.returncode, 0, "generate failed: %r %r" % (r.stdout, r.stderr))
        return self.stored().decode().strip()

    def assertSame(self, before, what):
        after = self.generate()
        self.assertEqual(before, after, "%s changed the identity: %s -> %s" % (what, before, after))

    def assertChanged(self, before, what):
        after = self.generate()
        self.assertNotEqual(before, after, "%s left the identity at %s" % (what, after))

    # identity

    def test_identity_is_the_sha256_of_the_manifest(self):
        ident, n, m, manifest = self.expect()
        r = self.run_script()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(self.stored(), (ident + "\n").encode(),
                         "expected %s from manifest:\n%s\nscript said: %s" % (ident, manifest, r.stdout))
        self.assertRegex(ident, r"^[0-9a-f]{64}$")

    def test_line_and_file_content(self):
        ident, n, m, _ = self.expect()
        r = self.run_script()
        self.assertEqual(r.returncode, 0)
        self.assertTrue(any(ln.startswith(self.line(ident, n, m)) for ln in r.stdout.splitlines()),
                        "no line starting %r in stdout: %r" % (self.line(ident, n, m), r.stdout))
        self.assertIn("written", r.stdout.split())
        self.assertNotIn("unchanged", r.stdout.split())
        self.assertEqual(self.stored(), (ident + "\n").encode())
        self.assertEqual(self.stored().count(b"\n"), 1)

    def test_second_run_changes_nothing(self):
        self.generate()
        old = (1_000_000_000, 1_000_000_000)
        os.utime(self.identity_path(), old)
        before = self.stored()
        r = self.run_script()
        self.assertEqual(r.returncode, 0)
        self.assertIn("unchanged", r.stdout.split(), r.stdout)
        self.assertNotIn("written", r.stdout.split())
        self.assertEqual(self.stored(), before)
        self.assertEqual(os.stat(self.identity_path()).st_mtime, old[1], "mtime moved")

    def test_a_stale_file_is_rewritten(self):
        self.put(IDENTITY_FILE, b"0" * 64 + b"\n")
        ident = self.expect()[0]
        r = self.run_script()
        self.assertEqual(r.returncode, 0)
        self.assertIn("written", r.stdout.split())
        self.assertEqual(self.stored(), (ident + "\n").encode())

    def test_same_tree_cloned_elsewhere_gives_the_same_identity(self):
        ident = self.generate()
        self.git("add", "-A")
        self.git("commit", "-q", "-m", "x")
        clone = os.path.join(self.tmp, "elsewhere", "deeper", "clone")
        os.makedirs(os.path.dirname(clone))
        self.git("clone", "-q", self.root, clone, cwd=self.tmp)
        r = self.run_script("--check", cwd=clone)
        self.assertEqual(r.returncode, 0, "clone check: %r %r (identity %s)" % (r.stdout, r.stderr, ident))
        self.assertIn(ident, r.stdout)
        os.remove(self.identity_path(clone))
        r = self.run_script(cwd=clone)
        self.assertEqual(r.returncode, 0)
        with open(self.identity_path(clone)) as f:
            self.assertEqual(f.read(), ident + "\n")

    def test_runs_from_a_subfolder_of_the_work_tree(self):
        ident = self.expect()[0]
        r = self.run_script(cwd=os.path.join(self.root, "src", "x"))
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(self.stored(), (ident + "\n").encode())
        self.assertFalse(os.path.exists(os.path.join(self.root, "src", "x", IDENTITY_FILE)))

    # files

    def test_changing_an_included_file_changes_it(self):
        base = self.generate()
        self.put("src/engine/a.go", b"package a // changed\n")
        self.assertChanged(base, "an edit of src/engine/a.go")
        self.assertEqual(self.stored().decode().strip(), self.expect()[0])

    def test_a_changed_declaration_changes_it(self):
        base = self.generate()
        self.decl = dict(DECLARATION, exclude=DECLARATION["exclude"] + ["docs/guide.md"])
        self.write_decl(self.decl)
        self.assertChanged(base, "a new exclude glob (the declaration lies inside src/engine/)")
        self.assertEqual(self.stored().decode().strip(), self.expect()[0])

    def test_excluded_files_do_not_matter(self):
        base = self.generate()
        for rel in ("README.md", "docs/README.md", "src/engine/README.md", "src/engine/sub/README.md",
                    "src/engine/a_test.go", "src/engine/testdata/t.txt"):
            with self.subTest(rel):
                self.put(rel, b"changed " + rel.encode() + b"\n")
                self.assertSame(base, "a change to excluded " + rel)
        with self.subTest(IDENTITY_FILE):
            self.put(IDENTITY_FILE, b"edited by hand\n")
            self.assertEqual(self.run_script().returncode, 0)
            self.assertEqual(self.stored().decode().strip(), base)
        for rel in ("src/engine/new_test.go", "src/engine/testdata/new.txt", "src/engine/sub/README.md"):
            with self.subTest("added " + rel):
                self.put(rel, b"added\n")
                self.assertSame(base, "adding excluded " + rel)

    def test_ignored_file_does_not_matter(self):
        base = self.generate()
        self.put("src/engine/ignored.log", b"changed\n")
        self.put("src/engine/gen/g.go", b"changed\n")
        self.put("src/engine/gen/more.go", b"new\n")
        self.assertSame(base, "a change to ignored files")

    def test_a_path_outside_every_include_does_not_matter(self):
        base = self.generate()
        self.put("src/other/o.txt", b"changed\n")
        self.put("src/deploy/Dockerfile", b"FROM y\n")
        self.put("src/other/new.txt", b"new\n")
        self.assertSame(base, "a change outside every include")

    def test_readme_exclusion_is_exact_and_case_sensitive(self):
        manifest = self.expect()[3]
        for rel in ("src/x/NOTREADME.md", "src/x/readme.md"):
            self.assertIn("  " + rel + "\n", manifest, "model: %s must be included" % rel)
        r = self.run_script("--list")
        self.assertEqual(r.returncode, 0)
        for rel in ("src/x/NOTREADME.md", "src/x/readme.md", "README.txt"):
            self.assertIn("  " + rel + "\n", r.stdout, "%s missing from the manifest:\n%s" % (rel, r.stdout))
        for rel in ("README.md", "docs/README.md", "src/engine/README.md"):
            self.assertNotIn("  " + rel + "\n", r.stdout, "%s must be excluded" % rel)
        base = self.generate()
        self.put("src/x/NOTREADME.md", b"changed\n")
        self.assertChanged(base, "an edit of NOTREADME.md")
        base = self.stored().decode().strip()
        self.put("src/x/readme.md", b"changed\n")
        self.assertChanged(base, "an edit of readme.md")

    def test_untracked_included_file_counts(self):
        base = self.generate()
        self.put("src/engine/untracked.go", b"package u\n")
        self.assertChanged(base, "an untracked, not ignored included file")
        self.assertEqual(self.stored().decode().strip(), self.expect()[0])
        self.assertEqual(self.git("status", "--porcelain").stdout.count("untracked.go"), 1)

    def test_tracked_file_deleted_from_the_work_tree_is_left_out(self):
        base = self.generate()
        os.remove(os.path.join(self.root, "src/engine/sub/b.go"))
        self.assertChanged(base, "deleting a tracked included file")
        ident, n, m, manifest = self.expect()
        self.assertNotIn("src/engine/sub/b.go", manifest)
        self.assertEqual(self.stored().decode().strip(), ident)
        r = self.run_script("--list")
        self.assertEqual(r.returncode, 0)
        self.assertNotIn("src/engine/sub/b.go", r.stdout)

    def test_star_matches_slash_and_nesting(self):
        r = self.run_script("--list")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("  src/engine/sub/deep/c.go\n", r.stdout, "src/engine/* must reach nested paths")
        base = self.generate()
        self.put("src/engine/sub/deep/c.go", b"package c // changed\n")
        self.assertChanged(base, "a change in src/engine/sub/deep")

    def test_odd_names_and_empty_file(self):
        ident, n, m, manifest = self.expect()
        self.assertIn("%s  src/x/empty\n" % sha(b""), manifest)
        r = self.run_script("--list")
        self.assertEqual(r.returncode, 0)
        self.assertEqual(r.stdout, manifest, "names with a space, an accent and an empty file")
        self.assertEqual(self.generate(), ident)

    def test_file_mode_does_not_matter(self):
        base = self.generate()
        os.chmod(os.path.join(self.root, "src/engine/a.go"), 0o755)  # noqa: S103 (the mode change is what the test examines)
        self.assertSame(base, "a mode change")

    def test_content_not_name_matters(self):
        base = self.generate()
        os.rename(os.path.join(self.root, "src/x/empty"), os.path.join(self.root, "src/x/empty2"))
        self.assertChanged(base, "renaming a file with the same content")

    # pins

    def test_listed_pin_reference_change_changes_it(self):
        base = self.generate()
        self.write_lock(IMAGES_LOCK.replace("@sha256:bbbb", "@sha256:b2b2"))
        self.assertChanged(base, "a change of the golang pin")
        self.assertEqual(self.stored().decode().strip(), self.expect()[0])

    def test_unlisted_pin_change_does_not(self):
        base = self.generate()
        self.write_lock(IMAGES_LOCK.replace("@sha256:eeee", "@sha256:e2e2")
                        .replace("@sha256:cccc", "@sha256:c2c2"))
        self.assertSame(base, "a change of pins the declaration does not name")

    def test_images_lock_comments_and_alignment_do_not_matter(self):
        base = self.generate()
        lock = IMAGES_LOCK.replace("# Every image, pinned.", "# Reworded comment.").replace(
            "golang                     golang", "golang      golang").replace("alpine                     alpine", "alpine \t alpine")
        self.assertNotEqual(lock, IMAGES_LOCK)
        self.write_lock(lock)
        self.assertSame(base, "comment and column changes in images.lock")

    def test_pins_come_in_declared_order(self):
        self.decl = dict(DECLARATION, pins=["alpine", "golang"])
        self.write_decl(self.decl)
        r = self.run_script("--list")
        self.assertEqual(r.returncode, 0)
        tail = r.stdout.splitlines()[-2:]
        self.assertEqual(tail, ["pin alpine alpine:3.22@sha256:dddd", "pin golang golang:1.26-alpine@sha256:bbbb"],
                         r.stdout)
        self.assertEqual(self.generate(), self.expect()[0])

    # --check

    def test_check_equal(self):
        self.generate()
        before = self.stored()
        r = self.run_script("--check")
        ident, n, m, _ = self.expect()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertTrue(any(ln.startswith(self.line(ident, n, m)) for ln in r.stdout.splitlines()), r.stdout)
        self.assertEqual(self.stored(), before)

    def test_check_stale(self):
        stale = "1" * 64
        self.put(IDENTITY_FILE, (stale + "\n").encode())
        os.utime(self.identity_path(), (1_000_000_000, 1_000_000_000))
        ident, n, m, _ = self.expect()
        r = self.run_script("--check")
        out = r.stdout + r.stderr
        self.assertEqual(r.returncode, 1, out)
        for needle in (stale, ident, "scripts/method-digest.py", IDENTITY_FILE):
            self.assertIn(needle, out, "message lacks %r (identity expected %s): %s" % (needle, ident, out))
        self.assertEqual(self.stored(), (stale + "\n").encode())
        self.assertEqual(os.stat(self.identity_path()).st_mtime, 1_000_000_000)

    def test_check_missing(self):
        ident = self.expect()[0]
        r = self.run_script("--check")
        out = r.stdout + r.stderr
        self.assertEqual(r.returncode, 1, out)
        for needle in ("nothing", ident, "scripts/method-digest.py"):
            self.assertIn(needle, out)
        self.assertIsNone(self.stored(), "--check must not create the file")

    # --list

    def test_list_prints_the_manifest_and_writes_nothing(self):
        ident, n, m, manifest = self.expect()
        r = self.run_script("--list")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout, manifest, "identity would be %s" % ident)
        self.assertIsNone(self.stored())
        self.put(IDENTITY_FILE, b"keep\n")
        self.run_script("--list")
        self.assertEqual(self.stored(), b"keep\n")

    # exit 2

    def assertRefused(self, r, what):
        self.assertEqual(r.returncode, 2, "%s: exit %s, out %r err %r" % (what, r.returncode, r.stdout, r.stderr))

    def test_bad_declaration_exits_2_writing_nothing(self):
        d = DECLARATION
        bad = {"invalid JSON": "{not json", "empty file": "", "not an object": json.dumps(["include"]),
               "unknown key": dict(d, extra=["x"]),
               "unknown key instead of one": {"include": d["include"], "exclude": d["exclude"], "pin": ["golang"]},
               "include glob matching nothing": dict(d, include=d["include"] + ["src/nothing/*"]),
               "pin not in images.lock": dict(d, pins=["golang", "nope"]),
               "pin only in a comment": dict(d, pins=["golang", "golang-race:"])}
        for key in ("include", "exclude", "pins"):
            bad["missing " + key] = {k: v for k, v in d.items() if k != key}
            bad["empty " + key] = dict(d, **{key: []})
            bad["duplicate in " + key] = dict(d, **{key: d[key] + [d[key][0]]})
            bad["non-string in " + key] = dict(d, **{key: d[key] + [5]})
            bad["empty string in " + key] = dict(d, **{key: d[key] + [""]})
            bad[key + " not a list"] = dict(d, **{key: "x"})
        for name, decl in bad.items():
            for prior in (None, b"0" * 64 + b"\n"):
                with self.subTest(name, prior=prior is not None):
                    self.write_decl(decl)
                    if prior is None:
                        if os.path.exists(self.identity_path()):
                            os.remove(self.identity_path())
                    else:
                        self.put(IDENTITY_FILE, prior)
                        os.utime(self.identity_path(), (1_000_000_000, 1_000_000_000))
                    for args in ([], ["--check"], ["--list"]):
                        r = self.run_script(*args)
                        self.assertRefused(r, "%s %s" % (name, args))
                        self.assertTrue((r.stdout + r.stderr).strip(), "exit 2 must say why")
                        self.assertEqual(self.stored(), prior, "%s %s wrote the identity file" % (name, args))
                        if prior:
                            self.assertEqual(os.stat(self.identity_path()).st_mtime, 1_000_000_000)

    def test_symbolic_link_among_selected_files(self):
        os.symlink("a.go", os.path.join(self.root, "src/engine/link.go"))
        r = self.run_script()
        self.assertRefused(r, "a symbolic link matching an include")
        self.assertIn("link.go", r.stdout + r.stderr, "the message should name the link")
        self.assertIsNone(self.stored())

    def test_symbolic_link_outside_the_selection_is_fine(self):
        os.symlink("o.txt", os.path.join(self.root, "src/other/link"))
        os.symlink("a_test.go", os.path.join(self.root, "src/engine/link_test.go"))
        r = self.run_script()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(self.stored(), (self.expect()[0] + "\n").encode())

    def test_outside_a_git_work_tree(self):
        plain = os.path.join(self.tmp, "plain")
        for rel in (DECL_FILE, LOCK_FILE):
            with open(os.path.join(self.root, rel), "rb") as f:
                self.put(rel, f.read(), root=plain)
        self.put("src/engine/a.go", b"x\n", root=plain)
        for args in ([], ["--check"], ["--list"]):
            r = self.run_script(*args, cwd=plain)
            self.assertRefused(r, "outside a work tree %s" % args)
            self.assertFalse(os.path.exists(self.identity_path(plain)))

    def test_unknown_option_exits_nonzero_writing_nothing(self):
        r = self.run_script("--bogus")
        self.assertNotEqual(r.returncode, 0)
        self.assertIsNone(self.stored())


if __name__ == "__main__":
    unittest.main()
