#!/usr/bin/env python3
"""Writes the live evals' summary (research D41, T109) from an eval suite's report.json.

usage: python3 scripts/ci-evals-summary.py <report.json>

One line per case: its agent and its pass^3 result, judged on its trials (every trial passed);
then the total, and the token use and cost when the trials carry `usage`
({"input_tokens", "output_tokens", "cost_usd"}). The text goes to $GITHUB_STEP_SUMMARY, or to
standard output without it. Exits 1 when a case failed or the report cannot be read.
"""
import json
import os
import sys


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print("usage: ci-evals-summary.py <report.json>", file=sys.stderr)
        return 2
    try:
        with open(argv[1], encoding="utf-8") as f:
            cases = json.load(f)
        if not isinstance(cases, list):
            raise ValueError("the report is not a list of cases")
    except (OSError, ValueError) as e:
        print(f"ci-evals-summary: cannot read {argv[1]}: {e}", file=sys.stderr)
        return 1
    rows = ["| Case | Agent | pass^3 |", "|------|-------|--------|"]
    passed = 0
    tokens_in = tokens_out = 0
    cost = 0.0
    used = False
    for c in cases:
        trials = c.get("trials") or []
        ok = bool(trials) and all(t.get("passed") is True for t in trials)
        passed += ok
        rows.append(f"| {c.get('case', '?')} | {c.get('agent', '?')} | {'✅ pass' if ok else '❌ fail'} |")
        for t in trials:
            u = t.get("usage")
            if isinstance(u, dict):
                used = True
                tokens_in += int(u.get("input_tokens", 0))
                tokens_out += int(u.get("output_tokens", 0))
                cost += float(u.get("cost_usd", 0))
    lines = [f"## Evals: {passed} of {len(cases)} cases pass^3", "", *rows, ""]
    if used:
        lines += [f"Tokens: {tokens_in} in, {tokens_out} out; cost ${cost:.2f}", ""]
    text = "\n".join(lines)
    path = os.environ.get("GITHUB_STEP_SUMMARY")
    if path:
        with open(path, "a", encoding="utf-8") as f:
            f.write(text)
    else:
        print(text)
    return 0 if passed == len(cases) else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
