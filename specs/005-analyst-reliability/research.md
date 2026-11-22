# Research: The analyst passes its quarantined eval cases every time

Phase 0 of the [plan](plan.md) for the [spec](spec.md). Each decision names its deciding source:
a cited primary source, a CI log, or the code of the evals as they stand on `main` (`34ea36d`).
Decisions marked *owner* were put to the project owner as direct questions.

## Sources

| ID | Source |
|----|--------|
| S1 | Evals run 44888638411 (`main`, `d67edc6`, 2026-11-21), job `Evals: analyst` (136799420053), its log read through the API 2026-11-22: `example` trial 1 failed `ExampleContent` ("no scenario creates two notes and expects them listed in creation order"), trials 2 and 3 passed; `revise-inject-approvals-field` trial 2 failed `FullyLinked` (no rule has a scenario under its `Rule:` block), `ExamplesPresent` ("the examples hold no scenario"), `KeepsIDs` (every `R-n/E-m` gone) and `AnswersApplied`, while `OutputIsOneProposal` and `SchemaValid` passed; trials 1 and 3 passed. Step "Keep the failed job's eval outputs" failed: `Invalid pattern '/home/runner/work/agentic-platform/agentic-platform/../eval-report'. Relative pathing '.' and '..' is not allowed.` The run has no artifact |
| S2 | `.github/workflows/evals.yml` on `main`: job-level `EVAL_REPORT_DIR: ${{ github.workspace }}/../eval-report` ("the runner context is not available in a job-level env; the workspace's parent is"); the scan step `if: failure()`, the upload `if: failure() && steps.scan.outcome == 'success'`, `path: ${{ env.EVAL_REPORT_DIR }}`. `scripts/tests/test_workflows_eval_artifacts.py` pins the path expression, the scan's `if: failure()` and the upload condition; `scripts/tests/test_workflows.py` requires `EVAL_REPORT_DIR` in the job-level env only |
| S3 | [GitHub Actions, contexts: context availability](https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#context-availability): `jobs.<job_id>.env` may use `github`, `needs`, `strategy`, `matrix`, `vars`, `secrets`, `inputs`, not `runner`. [Workflow artifacts](https://docs.github.com/en/actions/concepts/workflows-and-actions/workflow-artifacts) |
| S4 | The analyst evals on `main`: `harness_evals_test.go` (`TestAnalystEvals`: `EVAL_CASES` selection fails with "names a case that does not exist" when a name is not an analyst case; `run` loops `Trials` times; each trial's `<case>.trial<n>.out` and `.ndjson` and `report.json` go to the report folder), `revision_evals_test.go` (`TestAnalystEvalsRevision` skips when `EVAL_CASES` names no revision case; report under `revision/`), `cases.go` (`Trials = 3`), `content.go` (`Content`, rule 4: a scenario whose non-`Then` steps hold at least two distinct quoted texts and one `Then` step holding at least two of them in that order), `run.sh live` (`-run 'TestAnalystEvals'`, which matches both tests); `grade_test.go` and `revision_test.go` assert `Trials == 3`. `scripts/ci-evals-summary.py` reads only `$EVAL_REPORT_DIR/report.json`, into which `mergeReport` (`revision_evals_test.go`) adds the revision cases; `quarantine_test.go` pins exactly the two quarantined cases; `print` (harness) prints `pass^%d` from `Trials` |
| S5 | `src/agents/analyst/instruction.md` on `main`: the Gherkin example shows a creation scenario and an empty-text scenario, no listing scenario; "Revising a proposal": "keep every item and ID the answers do not change, and output the whole revised proposal" |
| S6 | Constitution: Principle XIX (pass^k), Engineering Principles "Fix the cause", "Existing tests change only with the owner's approval", "Checks prove they can fail"; [ADR 0050](../../docs/decisions/0050-failed-eval-jobs-keep-their-outputs.md); the spec's clarification SC-001 (owner, 2026-11-22) |

## R1. Why no failing answer was kept

- **Finding**: the keep-outputs step has never worked. `actions/upload-artifact` refuses a path
  with a `..` segment (S1), and `EVAL_REPORT_DIR` has had one since the evals workflow was written
  (S2). A defect against ADR 0050, fixed in code with a regression test (spec, Clarifications).
- **Decision**: `EVAL_REPORT_DIR: ${{ github.workspace }}-eval-report`. It is a sibling of the
  checkout, as the parent folder was meant to be, needs no `runner` context (S3), and has no `.`
  or `..` segment. The path expression of the upload (`${{ env.EVAL_REPORT_DIR }}`) stays.
- **Alternatives**: a step resolving the path into a step output (one more step, and the
  existing test pins the path expression); a folder inside the checkout (the steps suite's
  fixture code finds the repository root by walking up; a report folder there is noise in it).
- **Regression test** (new file): `EVAL_REPORT_DIR`'s value has no `.` or `..` path segment.
- **Evidence**: the action's own error (S1) states the rule it applies, a `.` or `..` segment; the
  new path has neither. It is shown in CI by the first eval job with a failed trial (SC-002); if
  none occurs during the feature, a backlog task keeps the check open (T019).

## R2. Keeping a quarantined case's failed trial

- **Finding**: a quarantined case's failure leaves the job green (by design, S4), so even with R1
  fixed its outputs would not be kept, and quarantined failures are the ones a task must diagnose.
- **Decision**: a new script, `scripts/ci-evals-failed-trials.py <dir>`, reads every
  `report.json` under the report folder and writes `failed=true` to `$GITHUB_OUTPUT` when any
  trial has `"passed": false`, or a report cannot be read or has an unexpected shape (not a list
  of cases; a case without a `trials` list or with an empty one; a trial without a boolean
  `passed`): fail closed, keep the outputs. `failed=false` otherwise; it prints the reports it read and the failed trials it found. A step
  with `id: failed` runs it with `if: always()` after the live evals. The scan runs on
  `failure() || steps.failed.outputs.failed == 'true'`, the upload on
  `(failure() || steps.failed.outputs.failed == 'true') && steps.scan.outcome == 'success'`,
  parenthesised: `&&` binds tighter than `||`, so without them a scan match (which fails the job,
  so `failure()` is true) would upload. A scan match still fails the job (FR-003). This supersedes
  ADR 0050's "a passing job uploads nothing" with "a job with no failed trial uploads nothing"
  (ADR 0078).
- **Alternatives**: the harness writes a marker file (changes the harness of both suites, and the
  workflow would trust a file the job's own code writes, as it trusts `report.json` anyway); the
  summary script also writes the output (one script, two jobs: Principle XVIII "Composable").
- **Existing test change** (owner approval): `test_workflows_eval_artifacts.py`'s `scan_step`
  and `upload_condition` rules accept the new condition, and their planted violations stay
  failing.

## R3. How a failing answer is obtained before a fix

- **Finding**: today one run of only the two cases is impossible: `EVAL_CASES=example,revise-
  inject-approvals-field` stops `TestAnalystEvals` ("names a case that does not exist"), because a
  revision case is not an analyst case, and `EVAL_CASES=revise-inject-approvals-field` alone fails
  the same way (S4). k is fixed at 3 (S4).
- **Decision** (both change the harness: owner approval): (a) `EVAL_CASES` names cases of either
  suite; each test runs its own named cases, skips when it has none, and a name in neither suite
  still fails; (b) `EVAL_TRIALS` (a whole number from 1 to 20, default 3) sets k for both tests;
  it matches `^[1-9][0-9]?$` and is at most 20 (no sign, space, leading zero or exponent);
  anything else fails before a trial runs. The printed report derives k from each case's trial
  count, so `print`'s signature and its existing callers stay. Duplicate names collapse to one;
  an empty or blank name is an error. Limit: `run.sh live` has a 60-minute time-out, which a full
  suite with k above about 4 exceeds; a larger k is for a few named cases. `Trials` stays the
  constant 3, the suite's default, so the existing `Trials == 3` assertions hold.
- **Where the runs happen**: on the owner's host, `run.sh live` with
  `EVAL_CASES=example,revise-inject-approvals-field EVAL_TRIALS=10`: a gateway and one analyst
  container at a time, about 20 trials of 10 to 25 s (S1), about $0.20, outputs in the local
  report folder. Each such run needs the owner's go-ahead. The CI evals workflow gets no new
  input; the full analyst run of SC-001 is its ordinary dispatch.
- **Runs**: the first (diagnostic) run before any fix; the second (SC-001's 10 of 10) after it,
  with the quarantine already removed in the working tree.

## R4. The fix

Decided from the failing answers of R3's diagnostic run (FR-004), recorded here before any fix.
Readings to test against those answers, from S1, S4 and S5 (none is a decision yet):

- `example`: the analyst may show creation order in a form `Content` does not read (each note's
  text in its own `Then` step, a data table, or texts without quotes), which makes the grader the
  part that is wrong; or it may leave the listing rule without a two-note scenario, which makes
  the instruction the part (its Gherkin example shows no listing scenario).
- `revise-inject-approvals-field`: the schema accepted the revision while its examples held no
  scenario at all; the answer may hold a `Feature:` with no `Rule:` blocks, rules in a form the
  parser does not read, or a cut-off text. The instruction's revision section does not say that
  the examples are output in full again.

If the diagnostic run shows no failure in 10 trials of a case, the next steps are decided by the
owner (more trials, or the reading above with the best evidence).

**Diagnostic run (T011, 2026-11-22, the owner's host, `724d31c` with the quarantine in place)**:
`EVAL_CASES=example,revise-inject-approvals-field EVAL_TRIALS=10 run.sh live`: `example` 10 of 10,
`revise-inject-approvals-field` 10 of 10 (312 s; $0.038 and $0.014). No failing answer to read.
In all 10 `example` answers the order scenario names both notes, quoted, in one `Then` step
("the response lists the notes "buy milk" then "walk dog""), the form `Content` reads; in one
(trial 4) the first-listed listing scenario is the empty list, and a later one shows the order.
A form the grader would not read (each note in its own step) did not occur, so the grader
reading stays unproven. With the CI runs before 44888638411 green, the two failures look rare
(2 of 6 trials there, 0 of 20 here); the cause stays unknown until a failure is kept (US1).

## Corrected by the analyze reviewer

- An earlier reading here said the revision suite's cases never reach the summary. They do:
  `mergeReport` (`revision_evals_test.go`) adds them to the top-level `report.json`. Backlog T191,
  filed on that reading, is closed.
