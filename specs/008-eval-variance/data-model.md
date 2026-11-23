# Data model: Eval suites show how much their trials vary

Phase 1 of the [plan](plan.md). The entities are the spec's (Trial, Unstable case, Eval
measurement); their sources are the eval reports (research S2).

## Eval measurement (`measurement.json`)

One per eval job (one matrix entry), written by `scripts/ci-evals-measure.py`, kept as the
artifact `eval-measurements-<suite>[-<cases>]-<run_attempt>` for 90 days (research R1). It holds
exactly these fields; any other is a defect (spec SC-005).

| Field | Type | From | Rule |
|-------|------|------|------|
| `version` | integer | constant | `1`; grows only additively |
| `suite` | string | `$SUITE` | `analyst` or `steps` |
| `cases` | string | `$EVAL_CASES` | the matrix entry's cases (`""`, `tests`, `code`, `review`) |
| `key` | string | derived | `<suite>` or `<suite>-<cases>`; the baseline lookup key |
| `commit` | string | `$GITHUB_SHA` | 40 hex characters |
| `branch` | string | `$GITHUB_REF_NAME` | the branch the run was started on |
| `run_id` | integer | `$GITHUB_RUN_ID` | |
| `run_attempt` | integer | `$GITHUB_RUN_ATTEMPT` | |
| `case_list` | list of Case | the report | in the report's order |

### Case

| Field | Type | From the report | Rule |
|-------|------|-----------------|------|
| `case` | string | `case` | `"?"` when absent |
| `agent` | string | `agent`, or `suite` when absent (the analyst report has none) | |
| `quarantined_by` | string or absent | `quarantined_by` | a task id, `T<digits>` |
| `trials` | list of Trial | `trials` | in the report's order; may be empty |

### Trial

| Field | Type | From the report | Rule |
|-------|------|-----------------|------|
| `passed` | boolean | `passed` | a missing or non-boolean value is `false` (a failed trial, as `ci-evals-failed-trials.py` reads it) |
| `seconds` | number or absent | `duration` | Go duration string parsed to seconds; absent when it does not parse |
| `attempts` | integer or absent | `attempts` | analyst suite only |
| `input_tokens`, `output_tokens` | integer or absent | `usage` | absent when the trial has no `usage` object |
| `cost_usd` | number or absent | `usage.cost_usd` | Docker Agent's estimate |

Never copied: `results`, `stderr`, `exit_error`, `changed_files`, `adversarial`, `attack`, the
trial number (its position gives it).

## Derived per case (shown, not kept)

- **passed/run**: trials with `passed` true, over the trials run.
- **pass^k**: k is the largest trial count among the measurement's cases (3 when no case has a
  trial); a case passes when it has at least one trial and every trial passed.
- **Unstable case**: at least one trial passed and at least one failed.
- **Spread** of each of `seconds`, `input_tokens`, `output_tokens`, `cost_usd`, `attempts`: the
  lowest, the median (mean of the two middle values for an even count) and the highest over the
  trials that carry it; "not available" when none does; "(n of m trials)" when some do not.

## Baseline and comparison

- **Baseline** of a measurement: the measurement of the newest unexpired
  `eval-measurements-<key>-*` artifact whose run is not this run and whose commit is an ancestor of
  this commit, or this commit (research R3).
- **Earlier map** (`config` job output `earlier`): a JSON object `{key: entry}` with one entry per
  matrix key; an entry is `{"run_id", "commit", "measurement"}` or `{"none": "<reason>"}`
  (no earlier run, could not be listed, could not be read).
- **Comparison** per case present in both, matched by `case`: earlier and current passed/run,
  median `seconds`, median `cost_usd`; **less reliable** when the current share of passed trials
  is lower than the earlier one. A case only in the current measurement: "nothing to compare
  with". A case only in the baseline is not shown (spec edge cases).
