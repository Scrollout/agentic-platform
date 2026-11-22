#!/usr/bin/env python3
"""Give the platform's method one identity: the same for the same source, different when any
declared part of the method differs.

usage: scripts/method-digest.py [--check | --list]

Works on the Git work tree of the current directory. The declaration, the identity's computation
and the exit codes are the contract in src/engine/method/README.md.

  (no option)  write the identity to src/engine/method/method.sha256 when that file differs
  --check      write nothing; exit 1 when the file is missing or differs from the sources
  --list       write nothing; print the manifest

Exit 0 on success, 1 for --check on a difference, 2 on a bad declaration, a symbolic link among
the selected files, wrong use, or outside a Git work tree.
"""
import fnmatch
import hashlib
import json
import os
import subprocess
import sys

DECLARATION = "src/engine/method/method.json"
IMAGES_LOCK = "src/deploy/images.lock"
IDENTITY_FILE = "src/engine/method/method.sha256"
SCRIPT = "scripts/method-digest.py"
KEYS = ("include", "exclude", "pins")


class Refused(Exception):
    """A reason to exit 2 before anything is written."""


def work_tree():
    try:
        out = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, check=False)  # noqa: S607 (git from PATH, as every script here)
    except OSError as e:
        raise Refused(f"git cannot run: {e}") from e
    root = out.stdout.decode().rstrip("\n")
    if out.returncode != 0 or not root:
        raise Refused("the current directory is not inside a Git work tree")
    return root


def declaration(root):
    path = os.path.join(root, DECLARATION)
    try:
        with open(path, encoding="utf-8") as f:
            d = json.load(f)
    except OSError as e:
        raise Refused(f"{DECLARATION}: cannot be read: {e.strerror}") from e
    except (ValueError, UnicodeDecodeError) as e:
        raise Refused(f"{DECLARATION}: not valid JSON: {e}") from e
    if not isinstance(d, dict):
        raise Refused(f"{DECLARATION}: must be an object with the keys {', '.join(KEYS)}")
    for k in d:
        if k not in KEYS:
            raise Refused(f"{DECLARATION}: unknown key {k!r}")
    for k in KEYS:
        if k not in d:
            raise Refused(f"{DECLARATION}: missing key {k!r}")
        v = d[k]
        if not isinstance(v, list) or not v:
            raise Refused(f"{DECLARATION}: {k!r} must be a non-empty list of strings")
        seen = set()
        for e in v:
            if not isinstance(e, str) or not e:
                raise Refused(f"{DECLARATION}: {k!r} holds {e!r}, not a non-empty string")
            if e in seen:
                raise Refused(f"{DECLARATION}: {k!r} lists {e!r} twice")
            seen.add(e)
    return d


def pins(root, names):
    path = os.path.join(root, IMAGES_LOCK)
    try:
        with open(path, encoding="utf-8") as f:
            lines = f.read().splitlines()
    except OSError as e:
        raise Refused(f"{IMAGES_LOCK}: cannot be read: {e.strerror}") from e
    refs = {}
    for line in lines:
        fields = line.split()
        if len(fields) >= 2 and not fields[0].startswith("#"):
            refs.setdefault(fields[0], fields[1])
    out = []
    for n in names:
        if n not in refs:
            raise Refused(f"{DECLARATION}: pin {n!r} names no line of {IMAGES_LOCK}")
        out.append((n, refs[n]))
    return out


def selected(root, d):
    out = subprocess.run(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],  # noqa: S607 (git from PATH)
                         cwd=root, capture_output=True, check=False)
    if out.returncode != 0:
        raise Refused(f"git ls-files failed: {out.stderr.decode(errors='replace').strip()}")
    paths = sorted({p for p in out.stdout.decode("utf-8", "surrogateescape").split("\0") if p},
                   key=lambda p: p.encode("utf-8", "surrogateescape"))
    used = set()
    files = []
    for p in paths:
        hits = [g for g in d["include"] if fnmatch.fnmatchcase(p, g)]
        full = os.path.join(root, p)
        if not hits or not (os.path.islink(full) or os.path.isfile(full)):
            continue  # outside every include; or a tracked file deleted from the working tree
        used.update(hits)  # an include glob counts as matching before the excludes apply
        if any(fnmatch.fnmatchcase(p, g) for g in d["exclude"]):
            continue
        if os.path.islink(full):
            raise Refused(f"{p} is a symbolic link; the method's files must be regular files")
        files.append(p)
    for g in d["include"]:
        if g not in used:
            raise Refused(f"{DECLARATION}: include {g!r} matches no file")
    return files


def manifest(root):
    d = declaration(root)
    listed = pins(root, d["pins"])
    files = selected(root, d)
    lines = []
    for p in files:
        with open(os.path.join(root, p), "rb") as f:
            lines.append(f"{hashlib.sha256(f.read()).hexdigest()}  {p}\n")
    lines += [f"pin {n} {r}\n" for n, r in listed]
    return "".join(lines), len(files), len(listed)


def stored(root):
    try:
        with open(os.path.join(root, IDENTITY_FILE), encoding="utf-8", errors="replace") as f:
            return f.read()
    except FileNotFoundError:
        return None


def main(argv):
    if len(argv) > 1 or (argv and argv[0] not in ("--check", "--list")):
        print("usage: scripts/method-digest.py [--check | --list]", file=sys.stderr)
        return 2
    mode = argv[0] if argv else "write"
    try:
        root = work_tree()
        text, n_files, n_pins = manifest(root)
    except Refused as e:
        print(f"method: {e}", file=sys.stderr)
        return 2
    if mode == "--list":
        sys.stdout.buffer.write(text.encode("utf-8", "surrogateescape"))
        return 0
    identity = hashlib.sha256(text.encode("utf-8", "surrogateescape")).hexdigest()
    head = f"method: {identity} ({n_files} files, {n_pins} pins)"
    have = stored(root)
    if mode == "--check":
        print(head)
        if have == identity + "\n":
            return 0
        held = "nothing" if have is None else have.strip() or "nothing"
        print(f"method: {IDENTITY_FILE} holds {held}, the sources give {identity}; "
              f"run {SCRIPT} and commit the file")
        return 1
    if have == identity + "\n":
        print(f"{head} unchanged")
        return 0
    with open(os.path.join(root, IDENTITY_FILE), "w", encoding="utf-8", newline="") as f:
        f.write(identity + "\n")
    print(f"{head} written")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
