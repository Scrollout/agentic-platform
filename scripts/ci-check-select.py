#!/usr/bin/env python3
"""Prints the check.yml jobs a commit needs: those whose files changed since the last green check
run (T117; research D50, ADR 0053).

Run from the repository root, with its inputs in the environment:
  RUNS    the check runs, newest first, as the GitHub API lists them unfiltered (a JSON list of
          objects with head_sha, head_branch and conclusion; research D52). The base is the
          newest run that succeeded, on any branch, whose head is a
          commit here and an ancestor of SHA (SHA itself included).
  SHA     the commit judged (default HEAD)
  EVENT   github.event_name; workflow_dispatch runs every job
It reads .github/check-jobs.yaml, where each job is `always: true` or has `paths:` (globs in
order, a later one wins, `!` excludes, as GitHub's `paths` filters), and prints one JSON list of
job ids in the declaration's order: every always job and every job one of whose paths a file
changed since the base matches (both paths of a rename, and deleted files, count). Every job for
a dispatch or without a base (RUNS unset, empty or not JSON, no qualifying run, SHA not a commit
here, git diff failing). With GITHUB_OUTPUT set it also appends `jobs=<the list>` to it. Its
reasons go to standard error. Exit 0.
"""
import json
import os
import sys

import yaml

from ci_select import base_from_runs, changed_files, is_commit, matches

DECL = ".github/check-jobs.yaml"


def say(msg):
    print("ci-check-select: " + msg, file=sys.stderr)


def choose(decl, sha):
    every = list(decl)
    if os.environ.get("EVENT", "") == "workflow_dispatch":
        say("started by hand: every job")
        return every
    if not is_commit(sha):
        say("%s is not a commit here: every job" % sha)
        return every
    before = base_from_runs(os.environ.get("RUNS", ""), sha)
    if not before:
        say("no green check run on a dev branch is an ancestor here: every job")
        return every
    changed = changed_files(before, sha)
    if changed is None:
        say("git diff %s %s failed: every job" % (before, sha))
        return every
    say("base %s, %d changed files" % (before, len(changed)))
    out = []
    for job, rule in decl.items():
        if rule.get("always") is True:
            out.append(job)
            continue
        why = [p for p in changed if matches(p, rule.get("paths") or [])]
        if why:
            say("%s: %s" % (job, ", ".join(why[:5])))
            out.append(job)
    return out


def main():
    with open(DECL, encoding="utf-8") as f:
        decl = yaml.safe_load(f)["jobs"]
    out = json.dumps(choose(decl, os.environ.get("SHA", "HEAD")))
    print(out)
    path = os.environ.get("GITHUB_OUTPUT", "")
    if path:
        with open(path, "a", encoding="utf-8") as f:
            f.write("jobs=%s\n" % out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
