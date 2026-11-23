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
import io
import json
import os
import re
import subprocess
import sys
import zipfile

from ci_select import git, is_commit

HEX40 = re.compile(r"[0-9a-f]{40}")
MEASUREMENT = "measurement.json"
MAX_BYTES = 1 << 20   # a job output holds at most 1 MB (research R3)
GH_TIMEOUT = 300


def say(msg: str) -> None:
    print(f"ci-evals-earlier: {msg}", file=sys.stderr)


def keys(raw) -> list[str]:
    """The matrix's keys, in its order; [] for MATRIX unset or not JSON."""
    if raw is None:
        return []
    try:
        matrix = json.loads(raw)
    except (ValueError, RecursionError) as e:
        say(f"MATRIX is not JSON ({e}); no earlier measurement is looked for")
        return []
    if not isinstance(matrix, list):
        say("MATRIX is not a JSON list; no earlier measurement is looked for")
        return []
    out = []
    for m in matrix:
        if not isinstance(m, dict) or not isinstance(m.get("suite"), str) or not m["suite"] \
                or not isinstance(m.get("cases", ""), str):
            say(f"left out a matrix entry that is not {{suite, cases}}: {m!r}")
            continue
        cases = m.get("cases", "")
        key = f"{m['suite']}-{cases}" if cases else m["suite"]
        if key not in out:
            out.append(key)
    return out


def listing(path: str) -> list[dict] | None:
    """The artifacts of the listing that carry what the choice needs; None when unreadable."""
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            lines = f.read().splitlines()
    except (OSError, ValueError) as e:
        say(f"the artifacts could not be listed: {path}: {e}")
        return None
    out = []
    for line in lines:
        try:
            a = json.loads(line)
        except (ValueError, RecursionError):
            continue
        if not isinstance(a, dict):
            continue
        aid, run_id = a.get("id"), a.get("run_id")
        if type(aid) is not int or aid <= 0 or type(run_id) is not int or run_id <= 0:
            continue
        if not isinstance(a.get("name"), str) or not isinstance(a.get("created_at"), str):
            continue
        if a.get("expired") is not False:
            continue
        head = a.get("head_sha")
        if not isinstance(head, str) or not HEX40.fullmatch(head):
            continue
        out.append(a)
    return out


def ancestors(artifacts: list[dict], sha: str) -> set[str]:
    """The artifacts' head commits that are commits here and sha or an ancestor of it."""
    if not HEX40.fullmatch(sha) or not is_commit(sha):
        say(f"SHA {sha!r} is not a commit here; no earlier measurement is in its history")
        return set()
    out = set()
    for head in sorted({a["head_sha"] for a in artifacts}):
        if is_commit(head) and git("merge-base", "--is-ancestor", head, sha).returncode == 0:
            out.add(head)
    return out


def download(repo: str, a: dict) -> dict:
    """The entry of artifact a: its run, commit and measurement, or none naming what failed."""
    where = f"artifact {a['id']} of run {a['run_id']}"
    try:
        r = subprocess.run(["gh", "api", f"repos/{repo}/actions/artifacts/{a['id']}/zip"],  # noqa: S603, S607 (gh from PATH; a fixed argument list, the id a checked integer)
                           capture_output=True, timeout=GH_TIMEOUT)
    except (OSError, subprocess.SubprocessError) as e:
        return {"none": f"the download of {where} failed: {e}"}
    if r.returncode != 0:
        err = r.stderr.decode("utf-8", "replace").strip().splitlines()
        return {"none": f"the download of {where} failed: {err[-1] if err else f'exit {r.returncode}'}"}
    try:
        with zipfile.ZipFile(io.BytesIO(r.stdout)) as z:
            try:
                info = z.getinfo(MEASUREMENT)
            except KeyError:
                return {"none": f"the zip of {where} holds no {MEASUREMENT}"}
            if info.file_size > MAX_BYTES:
                return {"none": f"the {MEASUREMENT} of {where} is larger than {MAX_BYTES} bytes"}
            data = z.read(info)
    except (zipfile.BadZipFile, zipfile.LargeZipFile, OSError, ValueError, RuntimeError) as e:
        return {"none": f"the zip of {where} could not be read: {e}"}
    try:
        m = json.loads(data.decode("utf-8"))
    except (UnicodeDecodeError, ValueError, RecursionError) as e:
        return {"none": f"the {MEASUREMENT} of {where} is not JSON: {e}"}
    if not isinstance(m, dict):
        return {"none": f"the {MEASUREMENT} of {where} is not a JSON object"}
    return {"run_id": a["run_id"], "commit": a["head_sha"], "measurement": m}


def earlier(path: str) -> dict:
    ks = keys(os.environ.get("MATRIX"))
    if not ks:
        return {}
    artifacts = listing(path)
    if artifacts is None:
        return {k: {"none": "the artifacts could not be listed"} for k in ks}
    run_id = os.environ.get("RUN_ID", "")
    artifacts = [a for a in artifacts if str(a["run_id"]) != run_id]
    names = {k: re.compile("eval-measurements-" + re.escape(k) + "-[0-9]+") for k in ks}
    artifacts = [a for a in artifacts if any(n.fullmatch(a["name"]) for n in names.values())]
    here = ancestors(artifacts, os.environ.get("SHA", "")) if artifacts else set()
    repo = os.environ.get("GITHUB_REPOSITORY", "")
    out = {}
    for k in ks:
        mine = [a for a in artifacts if a["head_sha"] in here and names[k].fullmatch(a["name"])]
        if not mine:
            out[k] = {"none": "no earlier run of this suite in this commit's history kept a measurement"}
        elif not repo:
            out[k] = {"none": "GITHUB_REPOSITORY is unset, so the earlier measurement could not be downloaded"}
        else:
            # Newest by created_at; on a tie, the first in the listing.
            newest = max(mine, key=lambda a: a["created_at"])
            out[k] = download(repo, newest)
        if "none" in out[k]:
            say(f"{k}: {out[k]['none']}")
    return out


def emit(result: dict) -> None:
    """One line earlier=<JSON> appended to $GITHUB_OUTPUT, or printed when it is unset or cannot
    be written."""
    line = "earlier=" + json.dumps(result, separators=(",", ":")) + "\n"
    path = os.environ.get("GITHUB_OUTPUT")
    if path:
        try:
            with open(path, "a", encoding="utf-8") as f:
                f.write(line)
            return
        except OSError as e:
            say(f"cannot write {path}: {e}; printing instead")
    sys.stdout.write(line)


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print("usage: ci-evals-earlier.py <artifacts.jsonl>", file=sys.stderr)
        return 2
    try:
        result = earlier(argv[1])
    except Exception as e:  # never a gate (FR-007): say what went wrong, exit 0
        say(f"{type(e).__name__}: {e}")
        result = {}
    try:
        emit(result)
    except Exception as e:
        say(f"{type(e).__name__}: {e}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
