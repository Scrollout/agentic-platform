#!/usr/bin/env python3
"""Shows how each eval case's trials varied, and how they changed since the earlier run
(feature 008, research R4; ADR 0083).

usage: python3 scripts/ci-evals-trials.py <measurement.json>

It reads a measurement (scripts/ci-evals-measure.py) and $EARLIER (evals.yml's config job output
`earlier`, scripts/ci-evals-earlier.py) and takes EARLIER's entry for the measurement's key. It
appends the section below to the file $GITHUB_STEP_SUMMARY names, or prints it when that variable
is unset or the file cannot be written. The same inputs always give the same text.

  ## Trials: 5 of 6 passed, 1 unstable case

  | Case | Agent | Passed | pass^3 | Seconds (low, median, high) | Cost $ (low, median, high) | Tokens in (low, median, high) | Tokens out (low, median, high) | Attempts (low, median, high) |
  |------|-------|--------|--------|-----------------------------|----------------------------|-------------------------------|--------------------------------|------------------------------|
  | example | analyst | 2/3 ⚠️ unstable | ❌ fail | 12.1, 14.0, 30.5 | 0.0100, 0.0120, 0.0200 | 5100, 5400, 6000 | 800, 900, 950 | 1, 1, 2 |
  | flaky | analyst | 3/3 ⚠️ quarantined (T185) | ✅ pass | 10.0, 11.2, 12.0 (2 of 3 trials) | n/a | n/a | n/a | 1, 1, 1 |

  ### Since run 45071292368 (988be8f)

  | Case | Passed | Median seconds | Median cost $ |
  |------|--------|----------------|---------------|
  | example | 3/3 → 2/3 📉 less reliable | 13.0 → 14.0 | 0.0110 → 0.0120 |
  | new-case | nothing to compare with | | |

- Passed is the case's passed trials over its trials run; "⚠️ unstable" when at least one passed
  and at least one failed; "⚠️ quarantined (<task>)" after it for a quarantined case. A case with
  no trials is 0/0 and fails.
- pass^k: k is the largest trial count of the measurement's cases (3 when none has a trial); a
  case passes when it has a trial and every trial passed.
- The suite line counts every trial, a quarantined case's included, and the unstable cases among
  the cases that are not quarantined.
- Spread: the lowest, the median (the mean of the two middle values for an even count) and the
  highest over the trials that carry the measure; "(<n> of <m> trials)" when some do not; n/a
  when none does. Seconds are written as Python's format(value, ".1f") and cost as
  format(value, ".4f"); tokens and attempts as integers, except a median that falls between two
  integers (an even count), written with one decimal (1.5).
- Comparison: each case matched by name with the earlier measurement's first case of that name;
  "📉 less reliable" when now_passed × before_run < before_passed × now_run (no division); a
  median one side lacks is n/a (n/a → 14.0); a case with no trials on either side, or that the
  earlier measurement lacks, says "nothing to compare with"; a case only in the earlier one is
  not shown. The heading names the earlier run id and the commit's first 7 characters.
- With no comparison the heading reads "### No earlier run to compare with: <reason>": the
  entry's reason, or "no earlier run could be read" when EARLIER is absent, empty, not JSON or
  lacks the key. An entry whose run_id is not a positive integer, whose commit is not 40
  lowercase hex characters, or whose measurement breaks ci-evals-measure.py's rules "could not be
  read"; a measurement holding that script's fallbacks ("?", "", 0) is read.
- Text taken from data (case and agent names, reasons): a | is written \\| and a line break or
  control character becomes a space.

A measurement that cannot be read prints that it could not be read. Whatever it reads, and
whatever fails inside it, it says what went wrong and exits 0, so it never changes whether an eval
job passes. It starts no process and uses the standard library only. Exit 2 on a wrong number of
arguments, with nothing written.
"""
import json
import math
import os
import sys
import unicodedata
from fractions import Fraction

NO_EARLIER = "no earlier run could be read"
K_DEFAULT = 3
# The spread columns: header, trial field, digits after the point (0: an integer).
SPREAD = (("Seconds", "seconds", 1), ("Cost $", "cost_usd", 4), ("Tokens in", "input_tokens", 0),
          ("Tokens out", "output_tokens", 0), ("Attempts", "attempts", 0))
INTEGER_FIELDS = ("attempts", "input_tokens", "output_tokens")
NUMBER_FIELDS = ("seconds", "cost_usd")
LINE_BREAKS = "\u2028\u2029"


class Unreadable(ValueError):
    pass


def text(value: str) -> str:
    """Text from data in a table cell or heading: | escaped, a line break or control character a
    space."""
    out = []
    for ch in value:
        if ch == "|":
            out.append("\\|")
        elif ch in LINE_BREAKS or unicodedata.category(ch) == "Cc":
            out.append(" ")
        else:
            out.append(ch)
    return "".join(out)


def need(ok: bool, why: str) -> None:
    if not ok:
        raise Unreadable(why)


def read_trial(t, where: str) -> dict:
    need(isinstance(t, dict), f"{where} is not an object")
    need(isinstance(t.get("passed"), bool), f"{where} has no boolean passed")
    for f in INTEGER_FIELDS:
        if f in t:
            need(type(t[f]) is int and t[f] >= 0, f"{where}: {f} is not an integer at least 0")
    for f in NUMBER_FIELDS:
        if f in t:
            v = t[f]
            need(type(v) in (int, float) and math.isfinite(v) and v >= 0,
                 f"{where}: {f} is not a finite number at least 0")
    return t


def read_case(c, n: int) -> dict:
    where = f"case {n}"
    need(isinstance(c, dict), f"{where} is not an object")
    need(isinstance(c.get("case"), str), f"{where} has no string case")
    need(isinstance(c.get("agent"), str), f"{where} has no string agent")
    if "quarantined_by" in c:
        need(isinstance(c["quarantined_by"], str), f"{where}: quarantined_by is not a string")
    need(isinstance(c.get("trials"), list), f"{where} has no trials list")
    for i, t in enumerate(c["trials"], 1):
        read_trial(t, f"{where} trial {i}")
    return c


def read_measurement(path: str) -> dict:
    """The measurement at path, its fields of the types ci-evals-measure.py writes; Unreadable
    otherwise."""
    try:
        with open(path, encoding="utf-8") as f:
            m = json.load(f)
    except (OSError, ValueError, RecursionError) as e:
        raise Unreadable(str(e)) from None
    need(isinstance(m, dict), "not an object")
    for f in ("version", "run_id", "run_attempt"):
        need(type(m.get(f)) is int, f"{f} is not an integer")
    for f in ("suite", "cases", "key", "commit", "branch"):
        need(isinstance(m.get(f), str), f"{f} is not a string")
    need(isinstance(m.get("measured_cases"), list), "measured_cases is not a list")
    for n, c in enumerate(m["measured_cases"], 1):
        read_case(c, n)
    return m


def number(value, digits: int) -> str:
    if digits:
        return f"{float(value):.{digits}f}"
    return str(round(Fraction(value)))


def spread(trials: list, field: str, digits: int) -> str:
    """lowest, median, highest over the trials that carry the field."""
    values = sorted(Fraction(t[field]) for t in trials if field in t)
    if not values:
        return "n/a"
    mid = len(values) // 2
    median = values[mid] if len(values) % 2 else (values[mid - 1] + values[mid]) / 2
    cell = ", ".join(number(v, digits) for v in (values[0], median, values[-1]))
    if len(values) < len(trials):
        cell += f" ({len(values)} of {len(trials)} trials)"
    return cell


def unstable(trials: list) -> bool:
    return any(t["passed"] for t in trials) and not all(t["passed"] for t in trials)


def trials_table(m: dict) -> list[str]:
    cases = m["measured_cases"]
    k = max((len(c["trials"]) for c in cases), default=0) or K_DEFAULT
    header = ["Case", "Agent", "Passed", f"pass^{k}"] + [
        f"{name} (low, median, high)" for name, _, _ in SPREAD]
    rows = []
    passed = run = unstable_cases = 0
    for c in cases:
        ts = c["trials"]
        p = sum(1 for t in ts if t["passed"])
        passed += p
        run += len(ts)
        cell = f"{p}/{len(ts)}"
        if unstable(ts):
            cell += " ⚠️ unstable"
            if "quarantined_by" not in c:
                unstable_cases += 1
        if "quarantined_by" in c:
            cell += f" ⚠️ quarantined ({text(c['quarantined_by'])})"
        result = "✅ pass" if ts and p == len(ts) else "❌ fail"
        rows.append([text(c["case"]), text(c["agent"]), cell, result]
                    + [spread(ts, field, digits) for _, field, digits in SPREAD])
    plural = "case" if unstable_cases == 1 else "cases"
    lines = [f"## Trials: {passed} of {run} passed, {unstable_cases} unstable {plural}", ""]
    lines.append("| " + " | ".join(header) + " |")
    lines.append("|" + "|".join("-" * (len(h) + 2) for h in header) + "|")
    lines += ["| " + " | ".join(r) + " |" for r in rows]
    return lines


def earlier_reason(key: str) -> str:
    """Why there is no comparison: the entry's reason, or that no earlier run could be read.
    The comparison itself (an entry with a measurement) comes with US3 (T015)."""
    try:
        earlier = json.loads(os.environ.get("EARLIER") or "null")
    except (ValueError, RecursionError):
        return NO_EARLIER
    entry = earlier.get(key) if isinstance(earlier, dict) else None
    if isinstance(entry, dict) and isinstance(entry.get("none"), str) and entry["none"].strip():
        return text(entry["none"])
    return NO_EARLIER


def section(path: str) -> str:
    try:
        m = read_measurement(path)
    except Unreadable as e:
        return f"## Trials\n\nThe measurement {text(path)} could not be read: {text(str(e))}\n"
    lines = trials_table(m)
    lines += ["", f"### No earlier run to compare with: {earlier_reason(m['key'])}"]
    return "\n".join(lines) + "\n"


def emit(out: str) -> None:
    """Append to $GITHUB_STEP_SUMMARY, or print when it is unset or cannot be written."""
    path = os.environ.get("GITHUB_STEP_SUMMARY")
    if path:
        try:
            with open(path, "a", encoding="utf-8") as f:
                f.write(out)
            return
        except OSError as e:
            print(f"ci-evals-trials: cannot write {path}: {e}; printing instead", file=sys.stderr)
    sys.stdout.write(out)


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print("usage: ci-evals-trials.py <measurement.json>", file=sys.stderr)
        return 2
    try:
        emit(section(argv[1]))
    except Exception as e:  # never a gate (FR-007): say what went wrong, exit 0
        print(f"ci-evals-trials: {type(e).__name__}: {e}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
