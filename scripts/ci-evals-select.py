#!/usr/bin/env python3
"""Prints the live eval suites whose inputs changed since the last green evals run (T115, T120;
research D45, ADR 0048; constitution XIX).

Run from the repository root, with its inputs in the environment:
  RUNS    the successful evals runs, newest first, as the GitHub API lists them (a JSON list of
          objects with head_sha, head_branch and conclusion). The base is the newest run that
          succeeded, on main or 001-prose-to-accepted-slice, whose head is a commit here and an
          ancestor of SHA (SHA itself included). When RUNS is unset, BEFORE names the base (T115).
  SHA     the commit judged (default HEAD)
  ALL     true chooses every suite (evals.yml's dispatch input `all`)
  MODE    pending: check.yml's evals-current job; the choice is written to $GITHUB_STEP_SUMMARY
          and the exit is 1 when REF is exactly main and a suite is pending
  REF     the branch, for MODE=pending
It reads src/tests/evals/suites.yaml, each suite's path globs and images.lock pins, and prints one
JSON list of {"suite", "cases"}: every suite whose globs a changed file matches (both paths of a
rename, and deleted files, count) or one of whose pins changed, in the declaration's order, each
once; every suite when ALL is true or there is no base (no qualifying run, RUNS empty or not
JSON, BEFORE empty, all zeros or not a commit here); [] when nothing changed affects one. Its
reasons go to standard error. Exit 0, except as MODE=pending says.
"""
import json
import os
import re
import subprocess
import sys

import yaml

DECL = "src/tests/evals/suites.yaml"
BRANCHES = ("main", "001-prose-to-accepted-slice")
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


def is_commit(sha):
    return bool(sha) and set(sha) != {"0"} and git("cat-file", "-e", sha + "^{commit}").returncode == 0


def base_from_runs(raw, sha):
    """The newest successful run on a dev branch whose head is an ancestor of sha, or ""."""
    try:
        runs = json.loads(raw)
    except ValueError:
        return ""
    if not isinstance(runs, list):
        return ""
    for r in runs:
        if not isinstance(r, dict) or r.get("conclusion") != "success" or r.get("head_branch") not in BRANCHES:
            continue
        head = r.get("head_sha") or ""
        if is_commit(head) and git("merge-base", "--is-ancestor", head, sha).returncode == 0:
            return head
    return ""


def choose(suites, every, sha):
    if os.environ.get("ALL", "") == "true":
        print("ci-evals-select: all: every suite", file=sys.stderr)
        return every
    if "RUNS" in os.environ:
        before = base_from_runs(os.environ["RUNS"], sha)
        if not before:
            print("ci-evals-select: no successful evals run on a dev branch is an ancestor here: every suite", file=sys.stderr)
            return every
    else:
        before = os.environ.get("BEFORE", "")
        if not is_commit(before):
            print("ci-evals-select: the base commit is unknown here: every suite", file=sys.stderr)
            return every
    print("ci-evals-select: base %s" % before, file=sys.stderr)
    diff = git("diff", "-z", "--name-status", "--no-renames", before, sha)
    if diff.returncode != 0:
        print("ci-evals-select: git diff failed (%s): every suite" % diff.stderr.strip(), file=sys.stderr)
        return every
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
        if why:
            print("ci-evals-select: %s: %s" % (name(s), ", ".join(why[:5])), file=sys.stderr)
            out.append({"suite": s["suite"], "cases": s["cases"]})
    if not out:
        print("ci-evals-select: nothing changed that an eval suite reads: no suite", file=sys.stderr)
    return out


def name(s):
    return s["suite"] + (" " + s["cases"] if s["cases"] else "")


def main():
    suites = yaml.safe_load(open(DECL, encoding="utf-8"))["suites"]
    every = [{"suite": s["suite"], "cases": s["cases"]} for s in suites]
    sha = os.environ.get("SHA", "HEAD")
    out = choose(suites, every, sha)
    print(json.dumps(out))
    if os.environ.get("MODE", "") != "pending":
        return 0
    ref = os.environ.get("REF", "")
    if out:
        lines = ["Evals pending (start them with `gh workflow run evals.yml --ref %s`):" % (ref or "<branch>"), ""]
        lines += ["- %s" % name(s) for s in out]
    else:
        lines = ["No eval suite is pending: every suite's inputs passed their evals."]
    summary = os.environ.get("GITHUB_STEP_SUMMARY", "")
    if summary:
        with open(summary, "a", encoding="utf-8") as f:
            f.write("\n".join(lines) + "\n")
    if out and ref == "main":
        print("ci-evals-select: main has pending evals: merge only after they pass on the feature branch (ADR 0048)", file=sys.stderr)
        return 1
    return 0

if __name__ == "__main__":
    sys.exit(main())
