#!/usr/bin/env python3
"""Keeps what an eval run measured, without protected content (feature 008, research R4, R5;
ADR 0083).

usage: python3 scripts/ci-evals-measure.py <report.json> <measurement.json>

It reads an eval suite's report (a JSON list of cases, as src/tests/evals/*/harness_evals_test.go
write it) and the environment variables SUITE, EVAL_CASES, GITHUB_SHA, GITHUB_REF_NAME,
GITHUB_RUN_ID and GITHUB_RUN_ATTEMPT, and writes <measurement.json>, creating its folder when it
does not exist. The measurement holds exactly these fields; any other is a defect. A value that
breaks its rule is written as the rule's fallback (in brackets), never copied.

  version         1 (grows only additively)
  suite           from SUITE: letters, digits and - only ("?")
  cases           from EVAL_CASES: letters, digits and - only, may be empty ("?")
  key             <suite>, or <suite>-<cases> when cases is not empty
  commit          from GITHUB_SHA: 40 lowercase hex characters ("")
  branch          from GITHUB_REF_NAME: at most 255 characters, no control character ("")
  run_id          from GITHUB_RUN_ID: a positive integer (0)
  run_attempt     from GITHUB_RUN_ATTEMPT: a positive integer (0)
  measured_cases  the report's cases in its order; an entry that is not an object is left out
    case            the case's name: a string of at most 200 characters ("?")
    agent           the case's agent, or the suite when the case has none: letters, digits and -
                    only ("?")
    quarantined_by  only when the case has it: ^T[0-9]+$ ("?", still quarantined)
    trials          the case's trials in order; not a list is empty; an entry that is not an
                    object is left out
      passed          a missing or non-boolean value is false
      seconds         the trial's Go duration string (850ms, 12s, 1m2.5s, 1h0m0s) in seconds,
                      at least 0 (absent)
      attempts        an integer at least 0 (absent)
      input_tokens, output_tokens   from usage: an integer at least 0 (absent; absent too when
                      usage is not an object)
      cost_usd        from usage.cost_usd: a finite number at least 0 (absent); Docker Agent's
                      estimate

Never copied: a case's or trial's results, stderr, exit_error, changed_files, adversarial,
attack, the trial number, and any field not named above. A measurement with commit "" or run_id
0 (a local run) is never chosen as a baseline.

A report that cannot be read or is not a list writes no file and prints why. Whatever it reads,
and whatever fails inside it (an output path that cannot be written included), it says what went
wrong and exits 0, so it never changes whether an eval job passes. It starts no process and uses
the standard library only. Exit 2 on a wrong number of arguments, with nothing written.
"""
import json
import math
import os
import re
import sys
import unicodedata
from fractions import Fraction

NAME = re.compile(r"[A-Za-z0-9-]+")
COMMIT = re.compile(r"[0-9a-f]{40}")
POSITIVE = re.compile(r"[1-9][0-9]*")
TASK = re.compile(r"T[0-9]+")
# One Go duration component: a decimal number and its unit (time.ParseDuration's units).
UNIT_SECONDS = {"ns": Fraction(1, 10 ** 9), "us": Fraction(1, 10 ** 6), "µs": Fraction(1, 10 ** 6),
                "μs": Fraction(1, 10 ** 6), "ms": Fraction(1, 1000), "s": Fraction(1),
                "m": Fraction(60), "h": Fraction(3600)}
COMPONENT = re.compile(r"([0-9]+(?:\.[0-9]*)?|\.[0-9]+)(ns|us|µs|μs|ms|s|m|h)")
DURATION = re.compile(r"(?:[0-9]+(?:\.[0-9]*)?(?:ns|us|µs|μs|ms|s|m|h)|\.[0-9]+(?:ns|us|µs|μs|ms|s|m|h))+")
CASE_MAX = 200
BRANCH_MAX = 255


def utf8(text: str) -> bool:
    """True when the text can be written as UTF-8 (no lone surrogate)."""
    try:
        text.encode("utf-8")
    except UnicodeEncodeError:
        return False
    return True


def name(value, fallback: str = "?", empty_ok: bool = False) -> str:
    """Letters, digits and - only, else the fallback."""
    if isinstance(value, str) and ((empty_ok and value == "") or NAME.fullmatch(value)):
        return value
    return fallback


def positive(value) -> int:
    return int(value) if isinstance(value, str) and POSITIVE.fullmatch(value) else 0


def branch(value) -> str:
    if (isinstance(value, str) and len(value) <= BRANCH_MAX and utf8(value)
            and not any(unicodedata.category(ch) == "Cc" for ch in value)):
        return value
    return ""


def count(value):
    """An integer at least 0 (a JSON integer, not a boolean or 3.0), else None."""
    return value if type(value) is int and value >= 0 else None


def amount(value):
    """A finite number at least 0, else None."""
    if type(value) in (int, float) and math.isfinite(value) and value >= 0:
        return value
    return None


def seconds(value):
    """A Go duration string in seconds, else None."""
    if not isinstance(value, str):
        return None
    if value == "0":
        return 0.0
    if not DURATION.fullmatch(value):
        return None
    total = sum((Fraction(n) * UNIT_SECONDS[u] for n, u in COMPONENT.findall(value)), Fraction(0))
    return float(total)


def measured_trial(t: dict) -> dict:
    out = {"passed": t.get("passed") is True}
    usage = t.get("usage") if isinstance(t.get("usage"), dict) else {}
    for field, value in (("seconds", seconds(t.get("duration"))),
                         ("attempts", count(t.get("attempts"))),
                         ("input_tokens", count(usage.get("input_tokens"))),
                         ("output_tokens", count(usage.get("output_tokens"))),
                         ("cost_usd", amount(usage.get("cost_usd")))):
        if value is not None:
            out[field] = value
    return out


def measured_case(c: dict, suite: str) -> dict:
    case = c.get("case")
    ok = isinstance(case, str) and len(case) <= CASE_MAX and utf8(case)
    out = {"case": case if ok else "?",
           "agent": name(c["agent"]) if c.get("agent") is not None else suite}
    if "quarantined_by" in c:
        q = c["quarantined_by"]
        out["quarantined_by"] = q if isinstance(q, str) and TASK.fullmatch(q) else "?"
    trials = c.get("trials")
    out["trials"] = [measured_trial(t) for t in (trials if isinstance(trials, list) else [])
                     if isinstance(t, dict)]
    return out


def measurement(cases: list, env) -> dict:
    suite = name(env.get("SUITE"))
    cases_filter = name(env.get("EVAL_CASES", ""), empty_ok=True)
    commit = env.get("GITHUB_SHA", "")
    return {"version": 1, "suite": suite, "cases": cases_filter,
            "key": f"{suite}-{cases_filter}" if cases_filter else suite,
            "commit": commit if COMMIT.fullmatch(commit) else "",
            "branch": branch(env.get("GITHUB_REF_NAME", "")),
            "run_id": positive(env.get("GITHUB_RUN_ID")),
            "run_attempt": positive(env.get("GITHUB_RUN_ATTEMPT")),
            "measured_cases": [measured_case(c, suite) for c in cases if isinstance(c, dict)]}


def measure(report: str, out: str) -> None:
    try:
        with open(report, encoding="utf-8") as f:
            cases = json.load(f)
    except (OSError, ValueError, RecursionError) as e:
        print(f"ci-evals-measure: cannot read {report}: {e}; no measurement written")
        return
    if not isinstance(cases, list):
        print(f"ci-evals-measure: {report} is not a list of cases; no measurement written")
        return
    data = (json.dumps(measurement(cases, os.environ), ensure_ascii=False, indent=2,
                       allow_nan=False) + "\n").encode("utf-8")
    try:
        folder = os.path.dirname(out)
        if folder:
            os.makedirs(folder, exist_ok=True)
        with open(out, "wb") as f:
            f.write(data)
    except OSError as e:
        print(f"ci-evals-measure: cannot write {out}: {e}")
        return
    print(f"ci-evals-measure: wrote {out}")


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print("usage: ci-evals-measure.py <report.json> <measurement.json>", file=sys.stderr)
        return 2
    try:
        measure(argv[1], argv[2])
    except Exception as e:  # never a gate (FR-007): say what went wrong, exit 0
        print(f"ci-evals-measure: {type(e).__name__}: {e}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
