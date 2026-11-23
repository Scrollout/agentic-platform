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
import sys

if __name__ == "__main__":
    print("ci-evals-measure: not implemented", file=sys.stderr)
    sys.exit(1)
