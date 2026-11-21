"""What changed since the last green CI run: the rules both CI selectors share (T115, T117, T120;
research D45, D50; ADR 0048, ADR 0053).

scripts/ci-evals-select.py chooses the eval suites and scripts/ci-check-select.py the check jobs a
commit needs. Both compare the commit with the newest green run of their workflow, on any branch,
whose head is an ancestor of it, and both match changed paths against globs:
  glob_re(glob)              `**/` any folders (none included), `**` anything, `*` any
                             characters and `?` one character within a path segment; every
                             other character is literal. GitHub's filter patterns give `?`, `+`
                             and `[...]` other meanings, so a declaration uses only `**`, `*`
                             and a leading `!`
  matches(path, patterns)    GitHub's `paths` filter: patterns in order, a later one wins, a
                             pattern starting with `!` excludes; no matching pattern is False
  is_commit(sha)             sha names a commit here (not empty, not all zeros)
  base_from_runs(raw, sha)   the head of the newest successful run, on any branch, that is a
                             commit here and an ancestor of sha (sha itself included), or ""
  changed_files(before, sha) the paths changed between them, both paths of a rename and deleted
                             files included; None when git diff fails
Run from the repository root.
"""
import json
import re
import subprocess

def glob_re(glob):
    out, i = "", 0
    while i < len(glob):
        if glob.startswith("**/", i):   # any folders, none included: a/**/b matches a/b
            out += "(?:.*/)?"
            i += 3
        elif glob.startswith("**", i):
            out += ".*"
            i += 2
        elif glob[i] == "*":
            out += "[^/]*"
            i += 1
        elif glob[i] == "?":
            out += "[^/]"
            i += 1
        else:
            out += re.escape(glob[i])
            i += 1
    return re.compile(out + r"\Z")


def matches(path, patterns):
    hit = False
    for p in patterns:
        exclude = p.startswith("!")
        if glob_re(p[1:] if exclude else p).match(path):
            hit = not exclude
    return hit


def git(*args):
    return subprocess.run(["git", *args], capture_output=True, text=True)


def is_commit(sha):
    return bool(sha) and set(sha) != {"0"} and git("cat-file", "-e", sha + "^{commit}").returncode == 0


def base_from_runs(raw, sha):
    try:
        runs = json.loads(raw)
    except (TypeError, ValueError):
        return ""
    if not isinstance(runs, list):
        return ""
    for r in runs:
        if not isinstance(r, dict) or r.get("conclusion") != "success":
            continue
        head = r.get("head_sha") or ""
        if isinstance(head, str) and is_commit(head) and git("merge-base", "--is-ancestor", head, sha).returncode == 0:
            return head
    return ""


def changed_files(before, sha):
    diff = git("diff", "-z", "--name-status", "--no-renames", before, sha)
    if diff.returncode != 0:
        return None
    # -z: status and path alternate, NUL-separated, paths unquoted (so non-ASCII paths match).
    parts = diff.stdout.split("\0")
    return [parts[k] for k in range(1, len(parts), 2) if parts[k]]
