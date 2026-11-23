#!/usr/bin/env python3
"""Finds, for each eval suite about to run, the measurement of the latest earlier run in this
commit's history (feature 008, research R3; ADR 0083).

usage: python3 scripts/ci-evals-earlier.py <artifacts.jsonl>

<artifacts.jsonl> holds one JSON object per line, as evals.yml's config job writes it:
  gh api --paginate "repos/$GITHUB_REPOSITORY/actions/artifacts?per_page=100" --jq '.artifacts[]
    | select(.name | startswith("eval-measurements-")) | {id, name, expired, created_at,
    run_id: .workflow_run.id, head_sha: .workflow_run.head_sha}'
A missing file, or a line that is not such an object, is left out.

Environment: MATRIX (the select step's matrix, a JSON list of {"suite", "cases"}), SHA (this
run's commit), RUN_ID (this run), GITHUB_REPOSITORY. Each matrix entry's key is <suite>, or
<suite>-<cases> when cases is not empty. For each key it takes, of the artifacts named
eval-measurements-<key>-<digits> that are not expired and belong to another run than RUN_ID, whose
head_sha is a commit here and SHA or an ancestor of it (git merge-base --is-ancestor), the newest
by created_at, and downloads it with gh api repos/$GITHUB_REPOSITORY/actions/artifacts/<id>/zip.

It writes one line, earlier=<JSON object>, to the file $GITHUB_OUTPUT names, or prints it when
that variable is unset. The object has an entry for every matrix key:
  {"run_id": <integer>, "commit": "<40 hex>", "measurement": {...}}   the earlier measurement
  {"none": "<reason>"}   no earlier run, or the download, the zip's measurement.json or its JSON
                          could not be read
MATRIX unset or not JSON gives earlier={}. Its reasons go to standard error.

Whatever it reads, and whatever fails inside it, it says what went wrong and exits 0, so it never
changes whether an eval job passes. It starts no process but git and gh, and uses the standard
library only. Exit 2 on a wrong number of arguments, with nothing written.
"""
import sys

if __name__ == "__main__":
    print("ci-evals-earlier: not implemented", file=sys.stderr)
    sys.exit(1)
