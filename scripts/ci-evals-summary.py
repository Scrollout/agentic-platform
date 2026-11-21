#!/usr/bin/env python3
"""Writes the live evals' summary (research D41, T109) from an eval suite's report.json.

usage: python3 scripts/ci-evals-summary.py <report.json>

One line per case: its agent and its pass^3 result, judged on its trials (every trial passed);
then the total, and the token use and cost when the trials carry `usage`
({"input_tokens", "output_tokens", "cost_usd"}). The text goes to $GITHUB_STEP_SUMMARY, or to
standard output without it. A case with `quarantined_by` (a flaky case quarantined under that open
task) is shown as quarantined and not counted. Exits 1 when a counted case failed or the report
cannot be read.

When some trial's grader result carries `counts` (src/tests/evals/steps/README.md, "The report"),
the table gains `Held-out` (HeldOutTestsPass, judging `code`) and `Mutants killed` (MutantsKilled,
judging `tests`): each case's trials in order as `passed/total`, `n/a` for another agent, `—` for a
trial with no counts. After the table, one line per trial with failed held-out tests, surviving
mutants, or a failed grader of the two that reports no counts. Without counts the output is as
before these two graders.
"""
import json
import os
import sys

# The grader that judges each agent's trials, and the words naming what its counts.failed lists.
COLUMNS = (("code", "HeldOutTestsPass", "held-out failed"),
           ("tests", "MutantsKilled", "mutants survived"))


def grader_result(trial: dict, grader: str) -> dict:
    for r in trial.get("results") or []:
        if isinstance(r, dict) and r.get("grader") == grader:
            return r
    return {}


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
    with_counts = any(isinstance(grader_result(t, g).get("counts"), dict)
                      for c in cases for t in c.get("trials") or [] for _, g, _ in COLUMNS)
    rows = ["| Case | Agent | pass^3 |", "|------|-------|--------|"]
    if with_counts:
        rows = ["| Case | Agent | pass^3 | Held-out | Mutants killed |",
                "|------|-------|--------|----------|----------------|"]
    failures = []
    passed = counted = quarantined = 0
    tokens_in = tokens_out = 0
    cost = 0.0
    used = False
    for c in cases:
        trials = c.get("trials") or []
        ok = bool(trials) and all(t.get("passed") is True for t in trials)
        task = c.get("quarantined_by")
        if task:
            # A flaky case quarantined under an open task (constitution, Fix the cause): reported,
            # not counted.
            quarantined += 1
            result = f"⚠️ quarantined ({task}): {'pass' if ok else 'fail'}"
        else:
            counted += 1
            passed += ok
            result = '✅ pass' if ok else '❌ fail'
        row = f"| {c.get('case', '?')} | {c.get('agent', '?')} | {result} |"
        if with_counts:
            for agent, grader, words in COLUMNS:
                if c.get("agent") != agent:
                    row += " n/a |"
                    continue
                cells = []
                for n, t in enumerate(trials, 1):
                    r = grader_result(t, grader)
                    k = r.get("counts")
                    name = f"- {c.get('case', '?')} trial {t.get('trial', n)}"
                    if isinstance(k, dict):
                        cells.append(f"{k.get('passed')}/{k.get('total')}")
                        if k.get("failed"):
                            failures.append(f"{name}: {words}: {', '.join(k['failed'])}")
                    else:
                        cells.append("—")
                        if r.get("passed") is False:
                            failures.append(f"{name}: {grader}: {r.get('reason', '')}")
                row += f" {', '.join(cells)} |"
        rows.append(row)
        for t in trials:
            u = t.get("usage")
            if isinstance(u, dict):
                used = True
                tokens_in += int(u.get("input_tokens", 0))
                tokens_out += int(u.get("output_tokens", 0))
                cost += float(u.get("cost_usd", 0))
    if quarantined:
        head = f"## Evals: {passed} of {counted} counted cases pass^3, {quarantined} quarantined"
    else:
        head = f"## Evals: {passed} of {counted} cases pass^3"
    lines = [head, "", *rows, ""]
    if failures:
        lines += [*failures, ""]
    if used:
        lines += [f"Tokens: {tokens_in} in, {tokens_out} out; cost ${cost:.2f}", ""]
    text = "\n".join(lines)
    path = os.environ.get("GITHUB_STEP_SUMMARY")
    if path:
        with open(path, "a", encoding="utf-8") as f:
            f.write(text)
    else:
        print(text)
    return 0 if passed == counted else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
