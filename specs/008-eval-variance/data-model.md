# Data model: Eval suites show how much their trials vary

Phase 1 of the [plan](plan.md). The entities are the spec's (Trial, Unstable case, Eval
measurement); their sources are the eval reports (research S2).

## Eval measurement (`measurement.json`)

One per eval job (one matrix entry), written by `scripts/ci-evals-measure.py`, kept as the
artifact `eval-measurements-<key>-<run_attempt>` for 90 days (research R1). It holds exactly
these fields, the ones spec FR-004 lists; any other is a defect (spec SC-005). A value that breaks
its rule is written as the rule's fallback, never copied.

| Field | Type | From | Rule (fallback) |
|-------|------|------|-----------------|
| `version` | integer | constant | `1`; grows only additively |
| `suite` | string | `$SUITE` | letters, digits and `-` only (`"?"`) |
| `cases` | string | `$EVAL_CASES` | letters, digits and `-` only, may be empty (`"?"`) |
| `key` | string | derived | `<suite>`, or `<suite>-<cases>` when `cases` is not empty; the baseline lookup key |
| `commit` | string | `$GITHUB_SHA` | 40 lowercase hex characters (`""`) |
| `branch` | string | `$GITHUB_REF_NAME` | at most 255 characters, no control character (`""`) |
| `run_id` | integer | `$GITHUB_RUN_ID` | a positive integer (`0`) |
| `run_attempt` | integer | `$GITHUB_RUN_ATTEMPT` | a positive integer (`0`) |
| `measured_cases` | list of Case | the report | in the report's order; an entry that is not an object is left out |

A measurement with `commit` `""` or `run_id` `0` (a local run) is never chosen as a baseline.

### Case

| Field | Type | From the report | Rule (fallback) |
|-------|------|-----------------|-----------------|
| `case` | string | `case` | a string of at most 200 characters (`"?"`) |
| `agent` | string | `agent`, or the suite when absent (the analyst report has none) | letters, digits and `-` only (`"?"`) |
| `quarantined_by` | string, or absent | `quarantined_by` | absent when the report has none; `T` and digits, `^T[0-9]+$` (`"?"`, still quarantined) |
| `trials` | list of Trial | `trials` | in the report's order; may be empty; not a list is empty; an entry that is not an object is left out |

### Trial

| Field | Type | From the report | Rule (fallback) |
|-------|------|-----------------|-----------------|
| `passed` | boolean | `passed` | "a missing or non-boolean value is `false`" (a failed trial, as `ci-evals-failed-trials.py` reads it) |
| `seconds` | number, or absent | `duration` | a Go duration string (`850ms`, `12s`, `1m2.5s`, `1h0m0s`) parsed to seconds, at least 0 (absent) |
| `attempts` | integer, or absent | `attempts` | an integer at least 0 (absent); the analyst suite has it |
| `input_tokens`, `output_tokens` | integer, or absent | `usage` | an integer at least 0 (absent; absent too when `usage` is not an object) |
| `cost_usd` | number, or absent | `usage.cost_usd` | a finite number at least 0 (absent); Docker Agent's estimate |

Never copied: `results`, `stderr`, `exit_error`, `changed_files`, `adversarial`, `attack`, the
trial number (its position gives it), and any field not named above.

## Derived per case (shown, not kept)

- **passed/run**: trials with `passed` true, over the trials run.
- **pass^k**: k is the largest trial count among the measurement's cases (3 when no case has a
  trial); a case passes when it has at least one trial and every trial passed.
- **Unstable case**: at least one trial passed and at least one failed.
- **Spread** of each of `seconds`, `cost_usd`, `input_tokens`, `output_tokens`, `attempts`: the
  lowest, the median ("the mean of the two middle values" for an even count) and the highest over
  the trials that carry it; `n/a` when none does; `(<n> of <m> trials)` when some do not.

## Baseline and comparison

- **Baseline** of a measurement: the measurement of the newest (by `created_at`) unexpired
  artifact named `eval-measurements-<key>-<digits>` whose run is not this run and whose commit is
  this commit or an ancestor of it (research R3).
- **Earlier map** (`config` job output `earlier`): a JSON object `{key: entry}` with one entry per
  matrix key; an entry is `{"run_id": <integer>, "commit": "<40 hex>", "measurement": {...}}` or
  `{"none": "<reason>"}` (no earlier run; the artifacts could not be listed; the download or the
  measurement could not be read). Absent or empty when the config step did not run or failed.
- **Comparison** per current case, matched by `case` with the baseline's first case of that name:
  earlier and current passed/run, median `seconds`, median `cost_usd` (`n/a` on a side that lacks
  it); **less reliable** when `now_passed × before_run < before_passed × now_run`. A case with no
  trials on either side, or that the baseline lacks: "nothing to compare with". A case only in the
  baseline is not shown (spec edge cases).
