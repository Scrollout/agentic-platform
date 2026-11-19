#!/usr/bin/env python3
"""Lints the lines a commit adds or changes, each stack with its own pinned linter (T041).

Usage, from the root of the staged snapshot (the pre-commit hook's working folder):

    python3 scripts/lint-staged.py <diff>

<diff> is `git diff --cached -U0 --find-renames --diff-filter=d` of the commit (with the a/ and
b/ prefixes). Every linter declared
in scripts/lint-staged.yaml that judges at least one staged path with an added or changed line runs
once (or once per Go module) in its container pinned in src/deploy/images.lock, without network,
as the caller's user, over the snapshot. Only its findings on those added or changed lines are
kept; they are printed as `<linter>: <path>:<line>: <text>`. Each linter that runs is named with how many
files it judged. It exits 1 when a finding is kept or a linter could not run (no pin, an exit code the
declaration does not name, or a findings exit with no finding it could read): a linter that
could not run refuses the commit, it never skips. Exit 0 otherwise, also when nothing is staged.
"""
import fnmatch
import os
import re
import subprocess
import sys

import yaml

DECLARATION = "scripts/lint-staged.yaml"
LOCK = "src/deploy/images.lock"


def unquote(name):
    """A path as git prints it in a diff header: C-quoted when it holds unusual characters."""
    if not (name.startswith('"') and name.endswith('"')):
        return name
    raw = name[1:-1].encode("latin-1", "backslashreplace").decode("unicode_escape")
    return raw.encode("latin-1").decode("utf-8", "replace")


def changed_lines(diff_text):
    """{path: set of new-side line numbers} for every line the diff adds or changes."""
    out, path = {}, None
    hunk = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@")
    for line in diff_text.splitlines():
        if line.startswith("+++ "):
            target = line[4:].rstrip("\t")  # git ends a name holding a space with a tab
            path = unquote(target) if target != "/dev/null" else None
            path = path[2:] if path and path.startswith("b/") else None
            continue
        m = hunk.match(line)
        if m and path is not None:
            start, count = int(m.group(1)), int(m.group(2) if m.group(2) is not None else 1)
            if count:
                out.setdefault(path, set()).update(range(start, start + count))
    return out


def pins():
    found = {}
    with open(LOCK) as f:
        for line in f:
            parts = line.split()
            if len(parts) >= 2 and not parts[0].startswith("#"):
                found[parts[0]] = parts[1]
    return found


def matches(path, linter):
    if any(fnmatch.fnmatchcase(path, g) for g in linter.get("exclude", [])):
        return False
    if any(fnmatch.fnmatchcase(path, g) for g in linter["paths"]):
        return True
    shebang = linter.get("shebang")
    if shebang and "." not in os.path.basename(path) and os.path.isfile(path):
        try:
            with open(path, "rb") as f:
                first = f.readline(256).decode("utf-8", "replace")
        except OSError:
            return False
        return re.match(shebang, first) is not None
    return False


def go_modules():
    with open("src/go.work") as f:
        text = f.read()
    return re.findall(r"^\s*\./([a-z][a-z0-9-]*)\s*$", text, re.M)


def docker(image, workdir, args, go):
    cache = os.path.join(os.environ.get("XDG_CACHE_HOME") or os.path.expanduser("~/.cache"), "agentic-platform")
    cmd = ["docker", "run", "--rm", "--network", "none", "--user", "%d:%d" % (os.getuid(), os.getgid()),
           "-v", "%s:/w" % os.getcwd(), "-w", "/w" + ("/" + workdir if workdir else ""), "-e", "HOME=/tmp"]
    if go:
        os.makedirs(os.path.join(cache, "lint"), exist_ok=True)
        cmd += ["-v", "%s:/gopath" % os.path.join(cache, "go"), "-v", "%s:/cache" % os.path.join(cache, "lint"),
                "-e", "GOPATH=/gopath", "-e", "GOMODCACHE=/gopath/pkg/mod", "-e", "GOCACHE=/cache/build",
                "-e", "GOLANGCI_LINT_CACHE=/cache/lint", "-e", "GOFLAGS=-buildvcs=false -mod=readonly",
                "-e", "GOTOOLCHAIN=local", "-e", "GOPROXY=off"]
    cmd += [image] + args
    return subprocess.run(cmd, capture_output=True, text=True, errors="replace")  # noqa: S603 (argument list from the declaration, no shell)


def judge(linter, runs, lines):
    """Runs a linter's calls; returns (kept findings, why it could not run or None)."""
    pattern = re.compile(linter["finding"])
    kept = []
    for workdir, prefix, targets in runs:
        r = docker(linter["image_ref"], workdir, list(linter["args"]) + targets, linter.get("go", False))
        if r.returncode not in linter["clean"] + linter["found"]:
            tail = (r.stdout + r.stderr).strip().splitlines()[-15:]
            return kept, "exit %d\n%s" % (r.returncode, "\n".join(tail))
        read = 0
        for out in r.stdout.splitlines():
            m = pattern.match(out)
            if not m:
                continue
            read += 1
            path = os.path.normpath(os.path.join(prefix, m.group("path")))
            line = int(m.group("line"))
            if line in lines.get(path, ()):
                kept.append("%s:%d: %s" % (path, line, out[m.end():].lstrip(": ").strip() or out))
        if r.returncode in linter["found"] and read == 0:
            tail = (r.stdout + r.stderr).strip().splitlines()[-15:]
            return kept, "exit %d with no finding it could read\n%s" % (r.returncode, "\n".join(tail))
    return kept, None


def main(argv):
    if len(argv) != 2:
        print("usage: python3 scripts/lint-staged.py <diff>", file=sys.stderr)
        return 2
    with open(argv[1], encoding="utf-8", errors="surrogateescape") as f:
        lines = changed_lines(f.read())
    with open(DECLARATION) as f:
        linters = yaml.safe_load(f)["linters"]
    lock, failed, ran = pins(), False, []
    for linter in linters:
        paths = sorted(p for p in lines if os.path.isfile(p) and matches(p, linter))
        if not paths:
            continue
        name = linter["name"]
        linter["image_ref"] = lock.get(linter["image"], "")
        if "@sha256:" not in linter["image_ref"]:
            print("lint: %s could not run: no pin named %s with a digest in %s" % (name, linter["image"], LOCK))
            failed = True
            continue
        if linter["per"] == "go-module":
            runs = []
            for mod in go_modules():
                base = "src/%s/" % mod
                pkgs = sorted({"./" + (os.path.dirname(p[len(base):]) or ".") for p in paths if p.startswith(base)})
                if pkgs:
                    runs.append((base.rstrip("/"), base.rstrip("/"), pkgs))
        else:
            runs = [("", "", [p if not p.startswith("-") else "./" + p for p in paths])]
        kept, why = judge(linter, runs, lines)
        ran.append(name)
        print("lint: %s ran on %d file%s" % (name, len(paths), "" if len(paths) == 1 else "s"))
        for k in kept:
            print("%s: %s" % (name, k))
        if why:
            print("lint: %s could not run: %s" % (name, why))
            failed = True
        elif kept:
            print("lint: %s: %d finding%s on changed lines" % (name, len(kept), "" if len(kept) == 1 else "s"))
            failed = True
    if not ran:
        print("lint: no staged file is judged by a linter")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
