#!/usr/bin/env python3
"""Writes a CI run's summary (research D41): one row per job, with its result and time.

usage: NEEDS='${{ toJSON(needs) }}' python3 scripts/ci-summary.py

NEEDS is the summary job's `needs` context: each job's `result` and its `outputs.seconds` (set by
scripts/ci-step.sh). The table goes to $GITHUB_STEP_SUMMARY, or to standard output without it.
Exits 1 when any job did not succeed, so the run's last job shows the run's result.
"""
import json
import os
import sys


def main() -> int:
    needs = json.loads(os.environ.get("NEEDS", "{}"))
    icon = {"success": "✅", "failure": "❌", "cancelled": "⏹️", "skipped": "⏭️"}
    rows = ["| Job | Result | Time |", "|-----|--------|------|"]
    ok = True
    for job, info in needs.items():
        result = info.get("result", "unknown")
        ok = ok and result == "success"
        seconds = (info.get("outputs") or {}).get("seconds")
        took = f"{seconds} s ({int(seconds) // 60} min {int(seconds) % 60} s)" if seconds and seconds.isdigit() else "—"
        rows.append(f"| {job} | {icon.get(result, '')} {result} | {took} |")
    title = "## Checks: every job passed" if ok else "## Checks: some jobs did not pass"
    text = "\n".join([title, "", *rows, ""])
    path = os.environ.get("GITHUB_STEP_SUMMARY")
    if path:
        with open(path, "a", encoding="utf-8") as f:
            f.write(text)
    else:
        print(text)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
