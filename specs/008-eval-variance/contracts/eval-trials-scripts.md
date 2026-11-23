# Contract draft: the eval measurement scripts

Draft for the first consumer, the evals workflow. The authoritative copy is each script's
docstring, as for the other CI helpers (`scripts/ci-evals-summary.py`). The fields are in
[data-model.md](../data-model.md).

## What every script promises

- **Never a gate** (FR-007): whatever it reads, and whatever fails inside it (an unreadable or
  malformed input of any shape, a value of the wrong type, a write that fails), it says what went
  wrong and exits 0. Exit 2 only on a wrong number of arguments, with nothing written.
- **No model, no network but `gh`**: `ci-evals-measure.py` and `ci-evals-trials.py` start no
  process; `ci-evals-earlier.py` starts only `git` and `gh`.
- **Python**: the evals job runs them with the runner's system `python3` (`ci-runner`
  `ubuntu-26.04`, Python 3.14.4 by the [GitHub Actions reference](../../../docs/references/github-actions.md));
  their tests run on `ci-python` (3.14.10) in the `py_tests` job; standard library only.
- **Text it prints from data** (case and agent names, reasons): a `|` is written `\|` and a line
  break or control character becomes a space, so no value can break the table.

## `scripts/ci-evals-measure.py`

Keeps what an eval run measured, without protected content.

```text
usage: python3 scripts/ci-evals-measure.py <report.json> <measurement.json>
```

- Reads the report and `SUITE`, `EVAL_CASES`, `GITHUB_SHA`, `GITHUB_REF_NAME`, `GITHUB_RUN_ID`,
  `GITHUB_RUN_ATTEMPT`; writes `<measurement.json>` with only data-model.md's fields, by their
  rules (a value that breaks a rule is written as the rule's fallback, never copied).
- A report that cannot be read, or is not a list, writes no file and prints why; a case or trial
  that is not an object is left out and counted in a printed line.

## `scripts/ci-evals-earlier.py`

Finds, for each eval suite about to run, the measurement of the latest earlier run in this
commit's history.

```text
usage: python3 scripts/ci-evals-earlier.py <artifacts.jsonl>
```

- `<artifacts.jsonl>`: one JSON object per line, as the config job writes it with
  `gh api --paginate "repos/$GITHUB_REPOSITORY/actions/artifacts?per_page=100" --jq '.artifacts[]
  | select(.name | startswith("eval-measurements-")) | {id, name, expired, created_at,
  run_id: .workflow_run.id, head_sha: .workflow_run.head_sha}'` (a file, since the list can
  outgrow an environment variable). A missing file, or a line that is not such an object, is left
  out.
- Environment: `MATRIX` (the select step's matrix, a JSON list of `{"suite", "cases"}`), `SHA`,
  `RUN_ID`, `GITHUB_REPOSITORY`. For each key it takes the newest by `created_at` of the
  artifacts named `eval-measurements-<key>-<digits>`, not expired, of another run than `RUN_ID`,
  whose `head_sha` is a commit here and `SHA` or an ancestor of it (`git merge-base
  --is-ancestor`), and downloads it with `gh api repos/$GITHUB_REPOSITORY/actions/artifacts/<id>/zip`.
- Writes `earlier=<JSON object>` (data-model.md, "Earlier map") as one line to `$GITHUB_OUTPUT`,
  or prints it when that variable is unset; every matrix key has an entry. Its reasons go to
  standard error.

## `scripts/ci-evals-trials.py`

Shows how each case's trials varied, and how they changed since the earlier run.

```text
usage: python3 scripts/ci-evals-trials.py <measurement.json>
```

- Reads `$EARLIER` (the config job's `earlier` output) and takes the entry of the measurement's
  `key`. An absent, empty or unreadable `EARLIER`, or one without the key, shows "no earlier run
  could be read". A baseline whose `run_id` is not a positive integer, whose `commit` is not 40
  lowercase hex characters, or whose measurement breaks data-model.md's rules is "could not be
  read".
- Appends to `$GITHUB_STEP_SUMMARY`, or prints when it is unset or cannot be written, the
  section below; the same inputs always give the same text.

### The section

```text
## Trials: 8 of 9 passed, 1 unstable case

| Case | Agent | Passed | pass^3 | Seconds (low, median, high) | Cost $ (low, median, high) | Tokens in (low, median, high) | Tokens out (low, median, high) | Attempts (low, median, high) |
|------|-------|--------|--------|-----------------------------|----------------------------|-------------------------------|--------------------------------|------------------------------|
| example | analyst | 2/3 ⚠️ unstable | ❌ fail | 12.1, 14.0, 30.5 | 0.0100, 0.0120, 0.0200 | 5100, 5400, 6000 | 800, 900, 950 | 1, 1, 2 |
| flaky | analyst | 3/3 ⚠️ quarantined (T185) | ✅ pass | 10.0, 11.2, 12.0 (2 of 3 trials) | n/a | n/a | n/a | 1, 1, 1 |

### Since run 45071292368 (988be8f)

| Case | Passed | Median seconds | Median cost $ |
|------|--------|----------------|---------------|
| example | 3/3 → 2/3 📉 less reliable | 13.0 → 14.0 | 0.0110 → 0.0120 |
| new-case | nothing to compare with | | |
```

- The suite line counts every trial, a quarantined case's included, and the unstable cases among
  the counted ones. The `Passed` cell of a quarantined case ends with `⚠️ quarantined (<task>)`,
  after `⚠️ unstable` when both apply; it is not counted in the `pass^k` column's total, as today.
- `pass^k`: k is the largest trial count of the measurement's cases (3 when none has a trial).
- With no baseline the second heading reads `### No earlier run to compare with: <reason>`.
- Comparison: matched by case name, the first case of each name; `less reliable` when
  `now_passed × before_run < before_passed × now_run` (whole numbers, no division); a case with no
  trials now or none before says `nothing to compare with`; a median one side lacks is `n/a`
  (`n/a → 14.0`).
- Numbers: seconds with one decimal, cost with four, tokens and attempts as integers.
