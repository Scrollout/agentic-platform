# Contract draft: the eval measurement scripts

Draft for the first consumer, the evals workflow. The authoritative copy is each script's
docstring, as for the other CI helpers (`scripts/ci-evals-summary.py`). The fields are in
[data-model.md](../data-model.md).

## `scripts/ci-evals-measure.py`

Keeps what an eval run measured, without protected content.

```text
usage: python3 scripts/ci-evals-measure.py <report.json> <measurement.json>
```

- Reads the report and `SUITE`, `EVAL_CASES`, `GITHUB_SHA`, `GITHUB_REF_NAME`, `GITHUB_RUN_ID`,
  `GITHUB_RUN_ATTEMPT`; writes `<measurement.json>` with only data-model.md's fields.
- A report that cannot be read, or is not a list of cases, writes no file and prints why.
- Exit 0, also when nothing was written; 2 on a wrong number of arguments.

## `scripts/ci-evals-earlier.py`

Finds, for each eval suite about to run, the measurement of the latest earlier run in this
commit's history.

```text
usage: python3 scripts/ci-evals-earlier.py
```

- Inputs in the environment: `MATRIX` (the config job's matrix, a JSON list of
  `{"suite", "cases"}`), `ARTIFACTS` (a JSON list of the repository's artifacts as the API lists
  them, with `id`, `name`, `expired`, `created_at`, `workflow_run`), `SHA`, `RUN_ID`,
  `GITHUB_REPOSITORY`. It downloads each chosen artifact with `gh api
  repos/$GITHUB_REPOSITORY/actions/artifacts/<id>/zip`.
- Writes `earlier=<JSON object>` (data-model.md, "Earlier map") to `$GITHUB_OUTPUT`, or prints it
  when that variable is unset. Every matrix key has an entry.
- Its reasons go to standard error. Exit 0 whatever it found; 2 on arguments.

## `scripts/ci-evals-trials.py`

Shows how each case's trials varied, and how they changed since the earlier run.

```text
usage: python3 scripts/ci-evals-trials.py <measurement.json>
```

- Reads `$EARLIER` (the config job's `earlier` output) and takes the entry of the measurement's
  `key`; an absent or unreadable `EARLIER` shows "no earlier run could be read".
- Appends to `$GITHUB_STEP_SUMMARY`, or prints, the section below; the same inputs always give
  the same text.
- Exit 0, also when the measurement cannot be read (it then says so); 2 on arguments.

### The section

```text
## Trials: 8 of 9 passed, 1 unstable case

| Case | Agent | Passed | pass^3 | Seconds (low, median, high) | Cost $ (low, median, high) | Tokens in (median) | Tokens out (median) | Attempts (low, median, high) |
|------|-------|--------|--------|-----------------------------|----------------------------|--------------------|---------------------|------------------------------|
| example | analyst | 2/3 ⚠️ unstable | ❌ fail | 12.1, 14.0, 30.5 | 0.010, 0.012, 0.020 | 5400 | 900 | 1, 1, 2 |
| clean | analyst | 3/3 | ✅ pass | 10.0, 11.2, 12.0 (2 of 3 trials) | n/a | n/a | n/a | 1, 1, 1 |

### Since run 45071292368 (988be8f)

| Case | Passed | Median seconds | Median cost $ |
|------|--------|----------------|---------------|
| example | 3/3 → 2/3 📉 less reliable | 13.0 → 14.0 | 0.011 → 0.012 |
| new-case | nothing to compare with | | |
```

- The suite line counts every trial, a quarantined case's included, and the unstable cases among
  the counted ones; a quarantined case's row names its task (`⚠️ quarantined (T185)`).
- With no baseline the second heading reads `### No earlier run to compare with: <reason>`.
- Numbers: seconds with one decimal, cost with three, tokens and attempts as integers.
