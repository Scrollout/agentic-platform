# Data model: The analyst passes its quarantined eval cases every time

No stored data changes. The one structure this feature reads is the eval report folder, as both
suites write it today (research S4); its fields are unchanged.

## Eval report folder

One per eval job, at `EVAL_REPORT_DIR` (in CI `${{ github.workspace }}-eval-report`, research R1).

| Item | Meaning |
|------|---------|
| `report.json` | The suite's cases: `name`, `passed`, `quarantined_by` (absent when counted), `trials[]` with `n`, `passed`, `results[]` |
| `revision/report.json` | The analyst revision suite's cases, same fields |
| `<case>.trial<n>.out`, `.ndjson` | Each trial's answer and raw events |

## Derived: has a failed trial

`true` when any `trials[].passed` is `false` in any `report.json` under the folder, or when a
`report.json` cannot be read; `false` otherwise, including when the folder holds no report (the
job then failed earlier, and `failure()` already keeps the folder). Computed by
`scripts/ci-evals-failed-trials.py` (research R2).

## Harness inputs (research R3)

| Variable | Values | Default | Invalid |
|----------|--------|---------|---------|
| `EVAL_CASES` | comma list of case names of either analyst suite | every case | a name in neither suite: the run fails before any trial |
| `EVAL_TRIALS` | whole number 1 to 20 | 3 | anything else: the run fails before any trial |
