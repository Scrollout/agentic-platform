# Research: Eval suites show how much their trials vary

Phase 0 of the [plan](plan.md) for the [spec](spec.md). Each decision names its deciding source:
the code on `main` (`9d8d678`), a reproduced experiment, or a cited document. Decisions marked
*owner* were put to the project owner as direct questions on 2026-11-23.

## Sources

| ID | Source |
|----|--------|
| S1 | `scripts/ci-evals-summary.py` (docstring and code): one table row per case with its pass^3 result, a `Held-out`/`Mutants killed` column when counts exist, the total tokens and cost; exit 1 when a counted case failed. `scripts/tests/test_ci_evals_summary_counts.py` and `test_ci_evals_summary_quarantine.py` pin its whole output with `assertEqual`, with fixtures of 1 and 2 trials labelled `pass^3` |
| S2 | `src/tests/evals/analyst/harness_evals_test.go` `Trial` (`trial`, `attempts`, `passed`, `results`, `duration`, `stderr`, `exit_error`, `usage`) and `CaseReport` (`case`, `adversarial`, `attack`, `passed`, `trials`, `quarantined_by`); `src/tests/evals/steps/harness_evals_test.go` `Trial` has no `attempts` and has `changed_files`, `CaseReport` has `agent`. `duration` is Go's `Duration.String()` (analyst rounded to the millisecond, steps to the second); `usage` is `{input_tokens, output_tokens, cost_usd}` (`src/tests/evals/runevents`) |
| S3 | `.github/workflows/evals.yml`: the `config` job (permissions `contents: read, actions: read`, setup-python with `scripts/tools/requirements-ci.txt`, `git fetch --unshallow`, `gh api .../workflows/evals.yml/runs`) chooses the matrix; the `evals` job (environment `dev`, the model key, no `actions` permission) runs one suite per matrix entry, writes the summary, finds failed trials, scans and uploads the report folder when a trial failed, `retention-days: 7` |
| S4 | `scripts/tests/test_workflows.py` `judge_permissions`: `actions: read` only on evals.yml `config` and check.yml `config` and `evals-current`. `scripts/tests/test_workflows_eval_artifacts.py`: exactly one upload step in all workflows, in the evals job, the evals job's last step, `retention-days` 7, its `if` the failed-trial condition, found as the first upload; `scripts/tests/test_workflows_eval_outputs_kept.py` `judge_upload_condition_failed_trial` takes the first upload too |
| S5 | `gh api repos/Scrollout/agentic-platform/actions/permissions/artifact-and-log-retention`, 2026-11-23: `{"days":1,"maximum_allowed_days":1}`; the repository's artifacts `evals-analyst-1` and `-2` of run 45051980598 (created 2026-11-22T18:09Z, uploaded with `retention-days: 7`) expire 2026-11-23T17:53Z |
| S6 | [Configuring the retention period for GitHub Actions artifacts and logs in your organization](https://docs.github.com/en/organizations/managing-organization-settings/configuring-the-retention-period-for-github-actions-artifacts-and-logs-in-your-organization), read 2026-11-23: "For public repositories: you can change this retention period to anywhere between 1 day or 90 days"; "the maximum retention period cannot exceed the limit set by the managing organization or enterprise"; by default artifacts and logs "are retained for 90 days"; "Starting October 1, 2026, these policies will apply to checks, workflow runs, and commit statuses in addition to artifacts and logs"; a change "does not retroactively apply to existing objects" |
| S7 | [actions/upload-artifact README](https://github.com/actions/upload-artifact), read 2026-11-23: "The retention period must be between 1 and 90 inclusive"; "Artifact names must be unique"; "Artifacts created by upload-artifact@v4 are immutable" |
| S8 | [REST API, artifacts](https://docs.github.com/en/rest/actions/artifacts), read 2026-11-23: list artifacts for a repository takes `name`, `per_page` ("max 100") and `page`; each artifact has `id`, `name`, `expired`, `created_at`, `expires_at` and `workflow_run` (`id`, `head_branch`, `head_sha`); download answers 302 with a URL that "expires after 1 minute", `archive_format` "must be zip" |
| S9 | [actions/download-artifact README](https://github.com/actions/download-artifact), read 2026-11-23: `github-token` "is required when downloading artifacts from a different repository or from a different workflow run"; its cross-run example is commented "token with actions:read permissions on target repo" |
| S10 | [Workflow syntax, `jobs.<job_id>.outputs`](https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions), read 2026-11-23: "Outputs can be a maximum of 1 MB per job"; "The total of all outputs in a workflow run can be a maximum of 50 MB" |
| S11 | [github-action-benchmark README](https://github.com/benchmark-action/github-action-benchmark), read 2026-11-23: MIT; stores results in a GitHub Pages branch (its auto-push example needs `contents: write`) or in an external JSON file that the example keeps with `actions/cache`; custom input is one `name`, `unit`, `value` (optional `range`, `extra`) per benchmark; it compares "with the previous result" by a ratio threshold (default 200%) |
| S12 | `scripts/ci_select.py` `base_from_runs`, `is_commit`, `git`: the base is the newest qualifying run whose `head_sha` is a commit here and an ancestor of SHA (`git merge-base --is-ancestor`); used by `ci-evals-select.py` |
| S13 | `scripts/secret-scan.sh DIR`: scans every file under DIR for the credential patterns, prints only paths, exit 1 on a match |
| S14 | Evals runs on this repository, `gh api .../workflows/evals.yml/runs`, 2026-11-23: every run since feature 001 was started on a feature branch (`005-…`, `006-…`, `007-…`), none on `main`; the branches were merged by fast-forward (`docs/log.md`) |
| S15 | Constitution: Principle XIX ("Behaviour users rely on MUST be reported as pass^k over repeated trials"), XI (protected content not in logs, metrics or traces), XVI (agent credentials in CI least privilege), Engineering Principles "Existing tests change only with the owner's approval", "Checks prove they can fail", Technology "Stack" (reuse an existing stack) |

## R1. Where the measurements are kept for 90 days

- **Decision** (*owner*): every eval job uploads one artifact, `eval-measurements-<suite>[-<cases>]-<run_attempt>`,
  holding `measurement.json` (data-model.md), with `retention-days: 90`, whether the job passed or
  failed, after `scripts/secret-scan.sh` passed on it. The owner raises the organization's and the
  repository's artifact and log retention to 90 days (S5, S6); until then the artifact expires
  after 1 day, the comparison finds no older baseline, and nothing fails.
- **Rationale**: an artifact needs no write permission in CI (S15, XVI) and no new service; 90 days
  is the most a public repository allows (S6, S7). Names are unique per run and attempt, as the
  failed-outputs artifact's are (S7).
- **Alternatives**: a data file committed to the repository (kept forever, but a manual commit
  after every run and history-like files in the tree); an orphan data branch pushed by CI (needs
  `contents: write` in a job that holds the model key); `actions/cache` (evicted after 7 days
  without use); github-action-benchmark (S11: Pages-branch push or cache, one value per benchmark,
  only the previous result as baseline, so neither the per-case share of passed trials nor the
  ancestor rule of R3 fits).
- **Found**: the outputs ADR 0078 keeps "for 7 days" are kept 1 day (S5), and since 2026-10-01 the
  same setting deletes workflow runs after 1 day (S6), so `ci-evals-select.py` finds no base run
  older than a day and runs every suite (S12). Raising the retention fixes both; backlog T198
  records it until the owner has done it.

## R2. Which existing tests change, and how the new upload is judged

- **Decision** (*owner*, approved 2026-11-23): two existing test files change, by a separate test
  author, with `Tests-approved-by:`:
  - `test_workflows_eval_artifacts.py`: `the_upload` and `judge_only_evals_uploads` find the
    failed-outputs upload by its name (`evals-…`) instead of as the first upload, and allow exactly
    one more upload step, in the evals job, named `eval-measurements-…`; the planted case "a second
    upload in the evals job fails" becomes "a second failed-outputs upload, or an upload with any
    other name, fails".
  - `test_workflows_eval_outputs_kept.py`: `judge_upload_condition_failed_trial` finds that upload by
    name, not as the first one.

  The measurement upload's own rules (pinned action, `retention-days: 90`, path, name, its `if`
  following its own scan, the scan running `secret-scan.sh` on the measurement folder) go in a new
  test file with clean and planted cases.
- **Order in the evals job**: the step that writes the measurement and the trials section runs
  right after `Summary of the evals`, with `if: always()`; the measurement's scan and upload run
  after the failed-outputs scan and before the failed-outputs upload, which stays last (S4: the
  first `secret-scan.sh` step must stay the failed-outputs scan, and that upload must be the last
  step).
- **Never a gate** (analyze round 1, F1, M1): the measurement's scan step and its upload carry
  `continue-on-error: true`, so a match (exit 1), a scan that cannot read (exit 2) or an upload
  error leaves the job's result as it was; the upload's `if` reads the scan's `outcome` (its result
  before `continue-on-error`), so nothing is uploaded after a match, and a following step prints a
  warning that the measurement was not kept and appends the same line to the job summary
  (spec FR-007). The failed-outputs scan keeps failing the job on a
  match, as ADR 0078 says.
- **Rationale**: S4's rules otherwise refuse any second upload. The baseline is fetched in the
  `config` job, which already holds `actions: read` (R3), so `judge_permissions` does not change.

## R3. Which earlier run is the baseline, and how the eval job gets it

- **Decision** (*owner* for the rule): the baseline of a matrix entry is the newest unexpired
  `eval-measurements-<suite>[-<cases>]-*` artifact of an earlier workflow run (not this run's id)
  whose `workflow_run.head_sha` is a commit here and an ancestor of this run's commit (the commit
  itself included: a re-dispatch on the same commit compares with the run before), on any branch.
  The `config` job, after choosing the matrix, lists the repository's artifacts (`gh api --paginate`,
  S8), and `scripts/ci-evals-earlier.py` picks each entry's baseline with `ci_select`'s ancestor
  test (S12), downloads its zip with `gh api` and the job's token, and writes one job output,
  `earlier`, a JSON object from each entry's key to the baseline's measurement, or to the reason
  there is none. The evals job reads `needs.config.outputs.earlier`.
- **Listing** (analyze round 1, E1): the step writes the listing to a file, one trimmed JSON object
  per line, with `gh api --paginate ... --jq` (contract): `--paginate` prints one result per page,
  and 90 days of artifacts can outgrow the 128 KiB a single environment variable may hold on Linux
  (`MAX_ARG_STRLEN`), so it is not passed in the environment.
- **Never a gate** (analyze round 1, F2): the step's `gh api` is followed by `|| true` (as the
  select step's is), the script exits 0, and the step carries `continue-on-error: true`; a failed
  step leaves `earlier` unset, which the trials script shows as "no earlier run could be read". The
  evals job's `needs: config` is unchanged, since the matrix comes from that job.
- **Rationale**: evals run on feature branches merged by fast-forward (S14), so a default-branch
  rule would find almost nothing; the ancestor rule is the suite selection's (S12), and leaves out
  unmerged experiments. Fetching in `config` keeps `actions: read` out of the job that holds the
  model key (XVI) and needs no permission change (S4). A measurement is a few kilobytes (25 cases ×
  3 trials), far under the 1 MB per job output (S10).
- **Limit**: the GitHub token's `actions: read` downloading another run's artifact is documented
  for a token "with actions:read permissions" (S9) but not shown with `GITHUB_TOKEN`; the first
  eval run of this feature proves it (quickstart Q5). A failed download is reported in the summary
  as "earlier run could not be read" and nothing fails (spec edge cases, FR-007).

## R4. How the measures are computed and shown

- **Decision**: two new scripts, stdlib Python like the other CI helpers (S15, Stack):
  - `scripts/ci-evals-measure.py <report.json> <measurement.json>` writes the eval measurement
    from the report and the run's environment, copying only the fields data-model.md lists
    (an allowlist, never the report's `results`, `stderr`, `exit_error`, `changed_files`).
  - `scripts/ci-evals-trials.py <measurement.json>` appends the trials section (US1, US2) and the
    comparison (US3) to the job summary, reading the baseline from `$EARLIER` (the config job's
    output) by the entry's key.
  Each catches every error past its argument check, says what went wrong and exits 0, so it
  never changes whether the job passes (FR-007); exit 2 only on a wrong number of arguments. The
  step that runs them carries `continue-on-error: true` as well (analyze round 1, F3).
- **Python** (analyze round 1, T1): the evals job has no `setup-python`, so the scripts run on the
  runner's system `python3`: `ci-runner` is `ubuntu-26.04`, whose image has Python 3.14.4
  (reference `github-actions`), against `ci-python` 3.14.10 in the `py_tests` job; adding
  `setup-python` to the job that holds the model key would only add a step.
- **Measures**: per case `passed/run` and pass^k with k the largest trial count in the
  measurement; unstable when some trial passed and some failed; lowest, median (mean of the two
  middle values for an even count) and highest of duration (seconds, parsed from Go's duration
  string), input tokens, output tokens, cost and attempts, each over the trials that carry it,
  with the count covered when it is not every trial. The comparison per case: earlier and current
  `passed/run`, median duration and median cost, and "less reliable" when the current share is
  lower, computed as `now_passed × before_run < before_passed × now_run` (no division, so a
  case with no trials cannot divide by zero; such a case has nothing to compare with); the
  earlier run's id and short commit head the section, after checking that they are an integer
  and 40 hex characters (analyze round 1, F3, E3).
- **Rationale**: keeping `ci-evals-summary.py` byte for byte (*owner*: T192 stays in the backlog)
  needs no existing test change (S1); the summary's job stays the gate (FR-007). Median and range
  over 3 trials describe without claiming significance (spec Assumptions).
- **Alternatives**: extending `ci-evals-summary.py` (changes pinned output, S1); a Go tool in the
  harness (adds the suites' paths, so every change would start paid eval runs, S3; the harness
  report already has every input).

## R5. What the measurement may hold

- **Decision**: only the fields data-model.md lists: case names, booleans, numbers, the quarantine
  task id, and the run's suite, cases, commit, branch, run id and attempt. The test of
  `ci-evals-measure.py` plants report fields holding a credential-like string and answer text in
  `stderr`, `results[].reason` and `changed_files`, and proves none reaches the measurement; the
  workflow also scans the folder (S13) before uploading.
- **Rationale**: grader reasons and stderr can quote model output (protected content, XI); an
  allowlist cannot leak a field added later to the report.

## R6. Live runs (quickstart Q5, T018)

Two dispatches of `evals.yml -f all=true` on this branch at 086720d, owner's go-ahead
(2026-11-23):

| Run | Result | Passed trials | Unstable cases | Trials' cost |
|-----|--------|---------------|----------------|--------------|
| 45163722619 | all four jobs green | 123 of 123 | none | $0.2368 |
| 45170628365 | `analyst` failed, the three steps jobs green | 122 of 123 | `example` (2/3) | $0.2309 |

- Each eval job of both runs uploaded `eval-measurements-<key>-1` (0.7 to 1.7 kB), expiring 90
  days after the run (2027-02-21).
- Run 2's config job chose run 1's measurement for every key (`EARLIER` names run 45163722619,
  commit 086720d, for `analyst`, `steps-tests`, `steps-code` and `steps-review`): R3's limit is
  settled, the config job's `GITHUB_TOKEN` with `actions: read` downloads another run's artifact.
- Run 2's analyst trials section shows `example | 2/3 ⚠️ unstable` and, under `### Since run
  45163722619 (086720d)`, `example | 3/3 → 2/3 📉 less reliable` (read from the measurements with
  the same scripts, since the job summary has no API).
- The new steps (trials, measurement scan, upload, warning) took 0 to 2 s per job, under SC-004's
  60 s; run 2's config step took under 4 s.
- The `example` failure (trial 2, `ExampleContent`: no scenario creates two notes and expects them
  in creation order) is the variance backlog T185 saw, with no suite input changed by this
  feature: backlog T199. Its kept outputs (`evals-analyst-1`, 7 days) hold `report.json`,
  `revision/` and all 63 trials' files, which closes backlog T193.
