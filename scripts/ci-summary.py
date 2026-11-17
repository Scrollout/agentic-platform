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
"""
import json
import os
import sys


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
    rows_ok, rows_skipped, rows_bad = [], [], []
    for job, info in needs.items():
        result = info.get("result", "unknown")
        outputs = info.get("outputs") or {}
        seconds = str(outputs.get("seconds") or "")
        took = f"{seconds} s" if seconds.isdigit() else "—"
        if result == "success":
            rows_ok.append(f"| ✅ | {job} | {took} | |")
        elif result == "skipped" and chosen is not None and job not in chosen:
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
