#!/usr/bin/env python3
"""Says whether any eval trial failed, so an eval job keeps its outputs (research R2, ADR 0078).

usage: python3 scripts/ci-evals-failed-trials.py <dir>

It reads every file named `report.json` under <dir>, at any depth, and writes one line,
`failed=true` or `failed=false`. The line is appended to the file named by $GITHUB_OUTPUT, or
printed on standard output when that variable is unset or empty.

- `failed=true` when any trial has `"passed": false` (a quarantined case's included), or when a
  report cannot be read or has an unexpected shape: not a list of cases; a case that is not an
  object or has no non-empty `trials` list; a trial that is not an object or has no boolean
  `passed`. A `report.json` that is not a file, or a folder under <dir> that cannot be listed,
  counts as an unreadable report. It fails closed: the outputs are kept.
- `failed=false` when every trial passed, or when no report exists (<dir> missing or empty).

It prints each report it read and each failed trial as `<case> trial <n>`. Exit 0 in all these
cases; 1 when $GITHUB_OUTPUT is set but cannot be written (the step fails, so the job keeps its
outputs through failure()); 2 on a wrong number of arguments, with nothing written.
Symlinked folders are not followed; a symlinked `report.json` file is read.
"""
import json
import os
import sys

REPORT = "report.json"


def label(value) -> str:
    """A case name as printed: itself when it is printable text, otherwise its repr."""
    return value if isinstance(value, str) and value and value.isprintable() else repr(value)


def failed_trials(cases) -> list[str]:
    """The failed trials of one report as `<case> trial <n>`; ValueError on an unexpected shape."""
    if not isinstance(cases, list):
        raise ValueError("not a list of cases")
    found = []
    for i, c in enumerate(cases, 1):
        if not isinstance(c, dict):
            raise ValueError(f"case {i} is not an object")
        trials = c.get("trials")
        if not isinstance(trials, list) or not trials:
            raise ValueError(f"case {label(c.get('case'))} has no non-empty trials list")
        for n, t in enumerate(trials, 1):
            if not isinstance(t, dict) or not isinstance(t.get("passed"), bool):
                raise ValueError(f"case {label(c.get('case'))}: trial {n} has no boolean passed")
            if not t["passed"]:
                found.append(f"{label(c.get('case'))} trial {t.get('trial', n)}")
    return found


def scan(root: str) -> bool:
    """True when a report under root names a failed trial or cannot be judged."""
    if not os.path.lexists(root):
        print(f"no report folder at {root}")
        return False
    if not os.path.isdir(root):
        print(f"cannot read {root}: not a folder", file=sys.stderr)
        return True
    failed = False
    unlisted = []
    reports = 0
    for here, dirs, files in os.walk(root, onerror=unlisted.append):
        dirs.sort()
        if REPORT in dirs:
            # Not descended into as a report folder of its own, but still a report.json to judge.
            print(f"cannot read {os.path.join(here, REPORT)}: not a file", file=sys.stderr)
            failed = True
        if REPORT not in files:
            continue
        path = os.path.join(here, REPORT)
        reports += 1
        try:
            with open(path, encoding="utf-8") as f:
                found = failed_trials(json.load(f))
        except (OSError, ValueError, RecursionError) as e:
            print(f"cannot read {path}: {e}", file=sys.stderr)
            failed = True
            continue
        print(f"read {path}")
        for line in found:
            print(line)
        failed = failed or bool(found)
    for e in unlisted:
        print(f"cannot list {e.filename}: {e.strerror}", file=sys.stderr)
        failed = True
    if not reports:
        print(f"no {REPORT} under {root}")
    return failed


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print("usage: ci-evals-failed-trials.py <dir>", file=sys.stderr)
        return 2
    line = f"failed={'true' if scan(argv[1]) else 'false'}\n"
    path = os.environ.get("GITHUB_OUTPUT")
    if not path:
        sys.stdout.write(line)
        return 0
    try:
        with open(path, "a", encoding="utf-8") as f:
            f.write(line)
    except OSError as e:
        print(f"ci-evals-failed-trials: cannot write {path}: {e}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
