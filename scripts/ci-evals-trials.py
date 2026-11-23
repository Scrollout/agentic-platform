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
  when none does. Seconds with one decimal, cost with four, tokens and attempts as integers.
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
import sys

if __name__ == "__main__":
    print("ci-evals-trials: not implemented", file=sys.stderr)
    sys.exit(1)
