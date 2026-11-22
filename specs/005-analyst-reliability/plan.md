# Implementation Plan: The analyst passes its quarantined eval cases every time

**Branch**: `005-analyst-reliability` | **Date**: 2026-11-22 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/005-analyst-reliability/spec.md` (clarified
2026-11-22)

## Summary

First make failures readable: the evals workflow's report folder loses its `..` segment, which
the artifact upload refuses (research R1), and a job keeps its outputs whenever any trial failed,
quarantined ones included (R2, ADR 0078). Then the analyst harness learns to run named cases of
both its suites in one run and k trials other than 3 (R3), so a diagnostic run of the two cases
with 10 trials each, on the owner's host, yields failing answers. The fix follows what those
answers show: the analyst's instruction, or a grader that misreads a correct proposal (R4). A
second 10-trial run must pass 10 of 10 per case, the quarantine goes, and a full analyst run in CI
passes pass^3 (SC-001).

## Technical Context

**Language/Version**: Go 1.26 (the analyst evals harness, as feature 001); Python 3 (the CI
script); GitHub Actions YAML

**Primary Dependencies**: none added; `actions/upload-artifact` as pinned today

**Storage**: none; the eval report folder ([data model](data-model.md))

**Testing**: Python `unittest` in `scripts/tests` (workflow rules, the new script); the analyst
graders' self-tests (`run.sh graders`) and harness stub run (`run.sh stub`); two live runs of the
two cases on the host and one full analyst run in CI, each with the owner's go-ahead

**Target Platform**: GitHub-hosted runners (the evals workflow); the owner's host for the
10-trial runs

**Project Type**: test harness and CI of the existing platform; the analyst's instruction

**Performance Goals**: none new; a 10-trial run of the two cases takes about 6 minutes (R3)

**Constraints**: model, model pin and proposal schema unchanged (FR-009); no grader weakened
(FR-005); every live run needs the owner's go-ahead; each existing test change needs the owner's
approval of that change

**Scale/Scope**: two eval cases; one workflow; one new script; the analyst harness; the
analyst's instruction or one grader

## Constitution Check

*Evaluated before research and again after design: PASS.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Needs before solutions | Pass | Serves N-013 |
| II-V, VII, X, XII | Not affected | No component, call, store or tenant path changes |
| VI. Adopt mature OSS | Pass | No capability added; the upload stays `actions/upload-artifact` |
| VIII. Containers and sandboxing | Not affected | The eval containers are unchanged |
| IX. Knowledge stays true | Pass | ADR 0078 supersedes part of ADR 0050; the analyst evals' README; the log |
| XI. Agent data privacy | Pass | The kept outputs are the same synthetic eval content ADR 0050 accepted; scanned before upload |
| XIII. Secure supply chain | Pass | No new action, dependency or image; the instruction change is reviewed like code |
| XIV. Safe defaults | Pass | An unreadable report keeps the outputs (fail closed, R2); a scan match keeps nothing and fails the job |
| XV. Compatible public APIs | Not affected | Internal report and workflow |
| XVI. Human control | Pass | `evals.yml` and `scripts/` carry the owner's `Approved-by:`; changed existing tests carry `Tests-approved-by:` |
| XVII. Declare, derive, converge | Pass | k and the case selection stay inputs of the one harness, not copies of it |
| XVIII. Abstractions say what | Pass | One script, named for what it reports; `EVAL_TRIALS` and `EVAL_CASES` documented in the README |
| XIX. Evals | Pass | The instruction change passes the full analyst suite (SC-001); deterministic graders only; a grader change keeps its planted cases failing (FR-006) |
| XX. Deterministic acceptance | Not affected | Engine checks unchanged |
| Checks prove they can fail | Pass | Each new workflow rule and the script have a clean and a planted case |
| Existing tests change only with approval | Pass, by approval | Listed below; each asked for before its brief; the harness, graders and eval cases are changed by test authors, never by the author of the code they judge (the analyst's instruction) |
| Fix the cause | Pass | The cause is read from failing answers before any change (FR-004); the quarantine ends with its task |
| Fresh review | Pass | A fresh read-only reviewer per story and before done, judging the CI run |
| Layout, Clean tree | Pass | `scripts/`, `src/tests`, `src/agents`, `.github` only |

## Decisions

Each decision that outlives the feature has a record in `docs/decisions/`.

| Decision | Research | Record |
|----------|----------|--------|
| An eval job keeps its report folder for 7 days when any trial failed, quarantined ones included, or the job failed; a job with no failed trial keeps nothing | R2 | [ADR 0078](../../docs/decisions/0078-eval-jobs-keep-outputs-of-any-failed-trial.md), superseding part of [ADR 0050](../../docs/decisions/0050-failed-eval-jobs-keep-their-outputs.md) |

R1 (the folder's path) and R3 (harness inputs) are fixes and conveniences inside existing
decisions, recorded in research only.

## Threat model

No external interface, tool, model or memory is added. Two elements change:

| Element | S | T | R | I | D | E | OWASP mapping | Mitigation |
|---------|---|---|---|---|---|---|---------------|------------|
| Kept eval outputs (public artifact) | | | | ✓ | | | LLM02 sensitive information disclosure | Synthetic inputs and public prompts only (ADR 0050); the credential scan runs first and nothing is kept on a match; the key never enters the folder |
| The analyst's instruction (if R4 changes it) | | ✓ | | | | ✓ | LLM01 prompt injection, ASI01 goal manipulation | Every adversarial analyst and revision case runs in the full run and counts (FR-008); the "description is data" section is kept |

## Project Structure

### Documentation (this feature)

```text
specs/005-analyst-reliability/
├── plan.md              # this file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks
```

No `contracts/`: the report folder and the workflow are internal; the report's fields are
unchanged.

### Source Code (repository root)

```text
.github/workflows/evals.yml                      # EVAL_REPORT_DIR; step `failed`; scan and upload conditions
scripts/ci-evals-failed-trials.py                # new (R2)
scripts/tests/test_ci_evals_failed_trials.py     # new
scripts/tests/test_workflows_eval_outputs_kept.py  # new: no `..` in EVAL_REPORT_DIR; the `failed` step and its use
scripts/tests/test_workflows_eval_artifacts.py   # existing: scan_step and upload_condition rules (approval)
src/tests/evals/analyst/
├── inputs.go                 # new: Selection, TrialCount (research R3)
├── inputs_test.go            # new
├── harness_evals_test.go     # existing: EVAL_CASES across both suites, EVAL_TRIALS (approval)
├── revision_evals_test.go    # existing: the same (approval)
├── cases.go, revision.go     # existing: quarantine removed (approval)
├── quarantine_test.go        # existing: no case quarantined any more (approval)
├── content.go                # existing: only if R4 finds the grader wrong (approval)
└── README.md                 # EVAL_TRIALS, EVAL_CASES, no quarantine
src/agents/analyst/instruction.md                # only if R4 finds the analyst wrong
specs/backlog.md                                 # T185 checked
```

**Structure Decision**: the change lives in the evals workflow, one new script, the analyst
evals harness and, depending on R4, the analyst's instruction or its content grader. The engine,
the model pin and the proposal schema do not change.

**Existing tests expected to change** (each needs the owner's approval of that change, asked
before its brief): `test_workflows_eval_artifacts.py` (R2); `harness_evals_test.go` and
`revision_evals_test.go` (R3); `cases.go`, `revision.go` and `quarantine_test.go` (quarantine
removed); `content.go` and
its self-test fixtures only if R4 finds the grader wrong.

## Exceptions

None.

## Complexity Tracking

No violation.
