#!/usr/bin/env python3
"""Writes a CI run's summary (research D41, T110): one table, failed jobs first, one row per job.

usage: NEEDS='${{ toJSON(needs) }}' python3 scripts/ci-summary.py

NEEDS is the summary job's `needs` context: each job's `result` and its outputs `seconds` and, for
a failed step, `error` (both set by scripts/ci-step.sh). A row is ✅ for success and ❌ for anything
else (failure, cancelled, skipped), with the job's time, or "—" when it has none (a job that
failed before its step), and a failed job's first error line. A job skipped because the config
job's output `jobs` (the JSON list of the jobs scripts/ci-check-select.py chose, T117, research
D50) does not name it counts as passed, with a row that says so; without that list, every skipped
job fails. The table goes to
$GITHUB_STEP_SUMMARY, or to standard output without it. Exits 1 unless every job succeeded.
Each row shows the job's display name from the workflow file that WORKFLOW names (T133, research
D53), or its id when WORKFLOW is unset or the job has no name.
"""
import json
import os
import re
import sys


def display_names():
    """Job id to display name, from the workflow file WORKFLOW names; {} without it.

    It reads only the `  <id>:` lines and the `    name:` line right under each, with the
    standard library, so the summary needs no package and still runs when config failed.
    A missing or unreadable file gives {}: every row then shows the job id.
    """
    path = os.environ.get("WORKFLOW", "")
    try:
        with open(path, encoding="utf-8") as f:
            text = f.read()
    except OSError:
        return {}
    pairs = re.findall(r"^  ([A-Za-z0-9_-]+):\n    name: (.+)$", text, re.M)
    return {j: n.strip().strip('"').strip("'") for j, n in pairs}


def selected(needs):
    """The jobs the config job chose, or None when its output `jobs` is not a JSON list of names."""
    raw = ((needs.get("config") or {}).get("outputs") or {}).get("jobs")
    try:
        jobs = json.loads(raw) if isinstance(raw, str) else None
    except ValueError:
        return None
    if not isinstance(jobs, list) or not all(isinstance(j, str) for j in jobs):
        return None
    return set(jobs)


def main() -> int:
    needs = json.loads(os.environ.get("NEEDS", "{}"))
    chosen = selected(needs)
    names = display_names()
    rows_ok, rows_skipped, rows_bad = [], [], []
    for jid, info in needs.items():
        job = names.get(jid, jid)
        result = info.get("result", "unknown")
        outputs = info.get("outputs") or {}
        seconds = str(outputs.get("seconds") or "")
        took = f"{seconds} s" if seconds.isdigit() else "—"
        if result == "success":
            rows_ok.append(f"| ✅ | {job} | {took} | |")
        elif result == "skipped" and chosen is not None and jid not in chosen:
            rows_skipped.append(f"| ⏭️ | {job} (skipped: nothing it judges changed) | — | |")
        else:
            err = (str(outputs.get("error") or "").splitlines() or [""])[0].replace("|", "\\|")
            rows_bad.append(f"| ❌ | {job} ({result}) | {took} | {err} |")
    ok = not rows_bad
    title = "## Checks passed" if ok else f"## Checks failed: {len(rows_bad)} of {len(needs)} jobs"
    text = "\n".join([title, "", "| | Job | Time | First error |", "|---|-----|------|-------------|",
                      *rows_bad, *rows_ok, *rows_skipped, ""])
    path = os.environ.get("GITHUB_STEP_SUMMARY")
    if path:
        with open(path, "a", encoding="utf-8") as f:
            f.write(text)
    else:
        print(text)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
