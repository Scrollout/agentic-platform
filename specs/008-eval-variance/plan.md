# Implementation Plan: Eval suites show how much their trials vary

**Branch**: `008-eval-variance` | **Date**: 2026-11-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/008-eval-variance/spec.md` (need N-054)

## Summary

Every eval job turns its `report.json` into a metadata-only eval measurement
(`scripts/ci-evals-measure.py`), shows a trials section after today's unchanged summary (passed
trials out of k, unstable cases, lowest, median and highest seconds, tokens, cost and attempts;
`scripts/ci-evals-trials.py`), and keeps the measurement as an artifact for 90 days after a
credential scan. The `config` job, which already lists workflow runs with `actions: read`, finds
each suite's latest earlier measurement whose commit is in this commit's history and downloads it
(`scripts/ci-evals-earlier.py`); the trials section compares each case with it. Nothing new can
fail a job. The owner raises the artifact and log retention from 1 to 90 days (research R1).

## Technical Context

**Language/Version**: Python 3 (`ci-python` in `src/deploy/images.lock`), standard library only,
as the other CI helper scripts; GitHub Actions workflow YAML

**Primary Dependencies**: none added; `gh` (on the hosted runner, already used by the `config`
job), `actions/upload-artifact` at the pinned v7.0.2 digest already in `evals.yml`

**Storage**: one GitHub Actions artifact per eval job, `eval-measurements-<key>-<attempt>`,
`retention-days: 90` (research R1)

**Testing**: `unittest` files in `scripts/tests/` (new files by a separate test author; two
existing workflow-rule files changed with the owner's approval, research R2); CI's check run of
the pushed commit as the full test command; one paid eval dispatch pair for Q5 with the owner's
go-ahead

**Target Platform**: GitHub-hosted runners (the `config` and `evals` jobs of `evals.yml`)

**Project Type**: CI tooling of the repository (Principle XVII); no product code changes

**Performance Goals**: SC-004: the new steps add under 1 minute per eval job and 0 trials

**Constraints**: FR-007 (never change whether a job passes); FR-009 (`ci-evals-summary.py`
byte for byte); no `actions: read` outside the jobs `judge_permissions` allows (S4); nothing from
the report outside data-model.md's allowlist (R5); `evals.yml` changes carry the owner's
`Approved-by:`; changed existing tests carry `Tests-approved-by:`

**Scale/Scope**: three scripts, about 25 cases × 3 trials per run, up to 4 matrix entries; one
workflow file; two existing and three new test files

## Constitution Check

*Evaluated before research and again after design: PASS.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Needs before solutions | Pass | Serves N-054 (owner set it in scope for the eval suites) and N-013 |
| II–V, VII, XII. Planes, API, operations, identity, providers, tenants | Not affected | No product component, API or tenant path changes |
| VI. Adopt mature OSS | Pass with justification | github-action-benchmark evaluated and rejected (research R1: needs a Pages-branch push or a 7-day cache, one value per benchmark, previous-result baseline only); the scripts are small stdlib helpers next to the existing ones |
| VIII. Containers | Not affected | CI-side scripts on the hosted runner, as the existing helpers |
| IX. Knowledge stays true | Pass | ADR 0083; reference `github-actions` gains retention, artifacts API and output limits; backlog T198 for the retention finding; log at each step |
| X. No silent loss | Not affected | |
| XI. Privacy | Pass | The measurement is an allowlist of metadata (R5), scanned before upload; no answer, prompt, reason or stderr |
| XIII. Supply chain | Pass | No dependency; the upload action is the pinned digest already used |
| XIV. Safe defaults | Pass | The baseline download runs in the `config` job, which holds no model key; the evals job gains no permission |
| XV. Compatible APIs | Pass | The measurement carries `version: 1` and grows only additively |
| XVI. Human control | Pass | `evals.yml` and existing test changes carry the owner's trailers; the retention change is the owner's |
| XVII. Declare, derive, converge | Pass | The measurement is derived from the report; the summary from the measurement and its baseline; nothing kept by hand |
| XVIII. Abstractions say what | Pass | Three scripts, each one job: keep the measurement, find the earlier one, show the trials |
| XIX. Evals | Pass | Reports pass^k as today and adds the share per case; changes no case, grader or threshold |
| XX. Deterministic acceptance | Pass | Measures inform; pass^k over counted cases stays the gate (FR-007) |
| Checks prove they can fail | Pass | Each new workflow rule and script test has a clean and a planted case |
| Existing tests change only with approval | Pass | Owner approved R2's two changes on 2026-11-23, for a separate test author |
| Fresh review | Pass | A fresh read-only reviewer per story and before done, judging the CI run |
| Layout | Pass | `scripts/`, `scripts/tests/`, `.github/workflows/evals.yml` |

## Decisions

Each decision that outlives the feature has a record in `docs/decisions/`.

| Decision | Research | Record |
|----------|----------|--------|
| Every eval job keeps a metadata-only measurement of its trials for 90 days; each run compares its cases with the latest earlier measurement whose commit is in its history, fetched in the `config` job; measuring never fails a job | R1, R3, R4, R5 | [ADR 0083](../../docs/decisions/0083-eval-runs-keep-measurements-and-compare-with-their-history.md) |

## Threat model

No external interface, model, tool or memory is added. The new elements are the measurement
artifact and the config job's download of an earlier one.

| Element | S | T | R | I | D | E | OWASP mapping | Mitigation |
|---------|---|---|---|---|---|---|---------------|------------|
| Measurement artifact (public, 90 days) | | | | ✓ | | | LLM02 sensitive information disclosure | Allowlist of fields (R5), planted-leak test, credential scan before upload; no model text |
| Earlier measurement read by the config job | | ✓ | | | ✓ | | ASI06 memory and context poisoning (a tampered baseline shapes what reviewers read) | Only artifacts of this repository's own runs whose commit is an ancestor; parsed as data, numbers and fixed fields only; malformed means "could not be read"; never a gate (FR-007) |
| Config job token (`actions: read`) | | | | | | ✓ | ASI03 identity and privilege abuse | No new permission; the token never reaches the job that holds the model key |
| Summary text | | ✓ | | | | | LLM05 improper output handling | Case names come from the repository; numbers are formatted; no report text is echoed |

## Project Structure

### Documentation (this feature)

```text
specs/008-eval-variance/
├── plan.md              # this file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   └── eval-trials-scripts.md   # draft of the three scripts' docstrings
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks
```

### Source Code (repository root)

```text
scripts/ci-evals-measure.py          # new: report.json -> measurement.json (allowlist)
scripts/ci-evals-earlier.py          # new: the config job's baseline per matrix key
scripts/ci-evals-trials.py           # new: trials section and comparison
.github/workflows/evals.yml          # config: earlier step and output; evals: measure, trials, scan, upload (Approved-by)
scripts/tests/test_ci_evals_measure.py        # new
scripts/tests/test_ci_evals_earlier.py        # new
scripts/tests/test_ci_evals_trials.py         # new
scripts/tests/test_workflows_eval_measurements.py   # new: the measurement upload's rules
scripts/tests/test_workflows_eval_artifacts.py      # changed (owner-approved, R2)
scripts/tests/test_workflows_eval_outputs_kept.py   # changed (owner-approved, R2)
```

**Structure Decision**: the measures are CI tooling over the eval reports, so they sit with the
other `scripts/ci-evals-*` helpers and are judged by the `scripts` tests; the Go eval harnesses
and their reports do not change, so no eval suite's inputs change and no paid run is started by
this feature's pushes.

**Existing tests expected to change** (approved by the owner 2026-11-23): `test_workflows_eval_artifacts.py`
and `test_workflows_eval_outputs_kept.py` (R2).

## Exceptions

None.

## Complexity Tracking

No violation.
