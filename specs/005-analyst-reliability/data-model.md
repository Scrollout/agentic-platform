# Data model: The analyst passes its quarantined eval cases every time

No stored data changes. The one structure this feature reads is the eval report folder, as both
suites write it today (research S4); its fields are unchanged.

## Eval report folder

One per eval job, at `EVAL_REPORT_DIR` (in CI `${{ github.workspace }}-eval-report`, research R1).

| Item | Meaning |
|------|---------|
| `report.json` | Every case of the job (the analyst job: both suites, merged by `mergeReport`): `case` (its name), `passed`, `quarantined_by` (absent when counted), `trials[]` with `trial` (its number), `passed`, `results[]` |
| `revision/` | The revision suite's trial files and its own `report.json` (the same cases as merged above) |
| `<case>.trial<n>.out`, `.ndjson` | Each trial's answer and raw events |

## Derived: has a failed trial

`true` when any `trials[].passed` is `false` in any `report.json` under the folder, or when a
`report.json` cannot be read or has an unexpected shape (not a list of cases; a case without a
non-empty `trials` list; a trial without a boolean `passed`); `false` otherwise, including when
the folder holds no report (the job then failed earlier, and `failure()` already keeps the
folder). Computed by
`scripts/ci-evals-failed-trials.py` (research R2).

## Harness inputs (research R3)

| Variable | Values | Default | Invalid |
|----------|--------|---------|---------|
| `EVAL_CASES` | comma list of case names of either analyst suite; spaces around a name trimmed; duplicates collapse | every case | a name in neither suite, or an empty or blank name: the run fails before any trial |
| `EVAL_TRIALS` | `^[1-9][0-9]?$`, at most 20 | 3 | anything else (`0`, `21`, `+5`, `010`, a leading space, `1e1`, `x`): the run fails before any trial |

Limit: `run.sh live` times out after 60 minutes; the full suite fits only for k up to about 4.
