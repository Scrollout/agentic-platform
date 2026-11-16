#!/usr/bin/env python3
"""Prints the live eval suites a push affects, as the evals job's matrix (T115; constitution XIX).

Run from the repository root by evals.yml's config job, with the push in the environment:
  BEFORE  the head commit of the branch's last successful evals run (empty when there is none)
  SHA     the pushed commit
  EVENT   push or workflow_dispatch
It reads src/tests/evals/suites.yaml, each suite's path globs and images.lock pins, and prints one
JSON list of {"suite", "cases"}: every suite whose globs a changed file matches (both paths of a
rename, and deleted files, count) or one of whose pins changed, in the declaration's order, each
once; every suite for workflow_dispatch and when BEFORE is empty, all zeros or not a commit here;
[] when the push affects none. Its reasons go to standard error. Exit 0.
"""
import json
import os
import re
import subprocess
import sys

import yaml

DECL = "src/tests/evals/suites.yaml"
LOCK = "src/deploy/images.lock"


def glob_re(glob):
    """`**` crosses folders; `*` and `?` stay within one path segment."""
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


def git(*args):
    return subprocess.run(["git", *args], capture_output=True, text=True)


def pins(text):
    out = {}
    for line in text.splitlines():
        f = line.split()
        if len(f) >= 2 and not f[0].startswith("#"):
            out[f[0]] = f[1]
    return out


def main():
    suites = yaml.safe_load(open(DECL, encoding="utf-8"))["suites"]
    every = [{"suite": s["suite"], "cases": s["cases"]} for s in suites]
    before, sha, event = os.environ.get("BEFORE", ""), os.environ.get("SHA", "HEAD"), os.environ.get("EVENT", "")
    if event == "workflow_dispatch":
        print("ci-evals-select: started by hand: every suite", file=sys.stderr)
        print(json.dumps(every))
        return 0
    if not before or set(before) == {"0"} or git("cat-file", "-e", before + "^{commit}").returncode != 0:
        print("ci-evals-select: the push's before commit is unknown here: every suite", file=sys.stderr)
        print(json.dumps(every))
        return 0
    diff = git("diff", "-z", "--name-status", "--no-renames", before, sha)
    if diff.returncode != 0:
        print("ci-evals-select: git diff failed (%s): every suite" % diff.stderr.strip(), file=sys.stderr)
        print(json.dumps(every))
        return 0
    # -z: status and path alternate, NUL-separated, paths unquoted (so non-ASCII paths match).
    parts = diff.stdout.split("\0")
    changed = [parts[k] for k in range(1, len(parts), 2) if parts[k]]
    old = git("show", "%s:%s" % (before, LOCK)).stdout
    new = git("show", "%s:%s" % (sha, LOCK)).stdout
    po, pn = pins(old), pins(new)
    moved = {k for k in set(po) | set(pn) if po.get(k) != pn.get(k)}
    out = []
    for s in suites:
        why = [p for p in changed if any(glob_re(g).match(p) for g in s["paths"])]
        why += ["pin " + k for k in sorted(moved & set(s["pins"]))]
        name = s["suite"] + (" " + s["cases"] if s["cases"] else "")
        if why:
            print("ci-evals-select: %s: %s" % (name, ", ".join(why[:5])), file=sys.stderr)
            out.append({"suite": s["suite"], "cases": s["cases"]})
    if not out:
        print("ci-evals-select: the push changes nothing an eval suite reads: no suite", file=sys.stderr)
    print(json.dumps(out))
    return 0


if __name__ == "__main__":
    sys.exit(main())
