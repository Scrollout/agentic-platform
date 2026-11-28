# Implementation Plan: A developer sees how much the builds of a slice vary

**Branch**: `012-run-variance` | **Date**: 2026-11-27 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/012-run-variance/spec.md` (needs N-054, N-050, N-052)

## Summary

A trial set is one engine operation of the new kind `run-trials` that builds an approved slice k
times (1 to 5, default 3), one trial after another, each an ordinary run into its own project
`<prefix>-<i>` that reuses no earlier result (`NoReuse`, as a proof) and names the set on its
`RunStarted` (research R1, R2). `ap trials` first makes the k projects and their step workers;
worker registrations now name their project, so the k projects' workers stand side by side and the
set runs with the terminal closed (R3). A trial that waits for the developer is handed over by the
set (R4). The set stops before a trial that could pass its $2 budget and after 24 hours (R5). Its
result (done out of ended, the unstable mark, per step and per trial the lowest, median and highest
attempts, review rounds, seconds, tokens and cost, which steps' files differ, and the change since
the slice's previous set) is computed from the trials' run records on each read (R6). Sets live in
the engine store with their slice, behind `/slices/{id}/trial-sets` and `/trial-sets/{id}`, and the
terminal commands `trials`, `trial-sets` and `trial-set` (R7).

## Technical Context

**Language/Version**: Go 1.26 (engine, terminal client), POSIX shell (`ap`)

**Primary Dependencies**: no new module; the terminal client requires `go.yaml.in/yaml/v3` v3.0.6
directly (already in its module graph) to read `x-limits` from its embedded contract (owner approved
2026-11-28, T061); the engine's Temporal Operation workflow and run path

**Storage**: the engine store gains trial-set records in the slice's folder; worker registrations
gain a project

**Testing**: Go tests in `src/engine` (`trials`, `api`, `durable`) and `src/cli`, by a separate test
author in new files; a new acceptance suite on the Compose stack with the recorded model (CI); CI's
check run of the pushed commit as the full test command; the analyst and steps evals once before
merge (method change; owner's go-ahead)

**Target Platform**: the local platform (Compose), as feature 001

**Project Type**: engine (new package, API resource, operation kind, registration by project),
terminal client, launcher, contracts

**Performance Goals**: reading a result reads k run records and calls no model (SC-005)

**Constraints**: the base pattern, agents and recordings are unchanged; contract changes are
additive except the `run-trials` enum value (Complexity Tracking); existing tests change only as the
owner approves; the Run workflow's new `RunStarted` fields sit behind a `workflow.GetVersion` bump to
3 ([ADR 0067](../../docs/decisions/0067-a-run-is-one-temporal-workflow.md))

**Scale/Scope**: one engine package, one resource with three calls, one operation kind, one
registration field, three terminal commands, one acceptance suite

## Constitution Check

*Evaluated before research and again after design: PASS, with one owner-approved exception.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Needs before solutions | Pass | Serves N-054, N-050, N-052 (spec) |
| II. Two planes | Pass | No new component; trials are runs in the runtime plane; the result is derived where the records are |
| III. API-first | Pass | Sets are started, listed, read and cancelled only through the engine API; limits declared in the contract |
| IV. Asynchronous operations | Pass | A set is one operation with a lifecycle and cancellation; its job is idempotent (trial run IDs recorded before each start, R1) |
| V. Identity | Pass | No new caller; a registration's project is sent with the slice and step's worker token, as the endpoint is |
| VI. Adopt mature OSS | Pass | Docker Agent's `eval --repeat` evaluated and does not apply (R8); no dependency |
| VII. Providers | Not applicable | No provider |
| VIII. Containers | Pass | No new image; the k projects' step containers are the existing ones |
| IX. Knowledge stays true | Pass | ADR 0102 to 0107, the architecture overview, the engine and terminal READMEs and the log at implementation |
| X. No silent loss | Pass | Each trial is an ordinary run; a set ends in exactly one state; its waits are bounded by 24 hours (R5) |
| XI. Privacy | Pass | A set holds IDs, counts and hashes only; it is erased with its slice |
| XII. Tenants | Not applicable | One developer by scope (feature 001 FR-021), as for slices; Complexity Tracking |
| XIII. Supply chain | Pass | No new module; `go.yaml.in/yaml/v3` made direct in `src/cli` (owner approved 2026-11-28, T061) |
| XIV. Safe defaults | Pass | Set cost and time budgets on top of each run's; a trial that could pass the budget never starts |
| XV. Compatible APIs | Exception | Additive paths and fields; paginated list; Idempotency-Key; the `run-trials` enum value is an owner-approved exception (Complexity Tracking, ADR 0105) |
| XVI. Human control | Pass | The developer asks for trials; the owner merges |
| XVII. Declare, derive, converge | Pass | Limits declared once in the contract; the result derived from the run records, never stored |
| XVIII. Abstractions say what | Pass | `trial-sets`, `trials`: named for what they do; no option exists for how |
| XIX. Evals | Pass | No agent, prompt or model change; the suites run once for the method change |
| XX. Deterministic acceptance | Pass | Each trial is accepted only by the pattern's checks; the result is counts over recorded outcomes |
| Values that check themselves | Pass | `trials` and `project_prefix` parsed at the API before anything is recorded |
| Single owner for rules | Pass | `engine/trials` owns the result's rules; the run path owns starting a run |
| Reliable events and side effects | Pass | Trial run ID recorded before the start, so a repeat follows it (R1) |
| Existing tests change only with approval | Pass | Each change listed below needs the owner's approval by name |
| Fresh review | Pass | A fresh read-only reviewer per story and before done, judging the CI run |
| Layout | Pass | Files under `src/`, `specs/`, `docs/` only |

## Decisions

Each decision that outlives the feature has a record in `docs/decisions/`.

| Decision | Research | Record |
|----------|----------|--------|
| A trial set is one engine operation that runs k fresh runs of a slice one after another and derives its result from their run records | R1, R2, R5, R6 | [ADR 0102](../../docs/decisions/0102-trial-set-runs-fresh-builds-and-derives-its-result.md) |
| Step-worker registrations name their project; a run uses only its own project's workers | R3 | [ADR 0103](../../docs/decisions/0103-worker-registrations-name-their-project.md) |
| A trial that waits for the developer is handed over by its set | R4 | [ADR 0104](../../docs/decisions/0104-trial-waiting-for-developer-is-handed-over.md) |
| The engine's preview contract gains `run-trials` in place, an exception to Principle XV | R9 | [ADR 0105](../../docs/decisions/0105-preview-contract-gains-run-trials-in-place.md) |
| A set whose operation fails ends `failed` (its running trial cancelled, the reason kept); `TrialSetState`, new in this feature, gains `failed` before release | R9 | [ADR 0106](../../docs/decisions/0106-a-failed-trial-set-ends-failed.md) |
| A step whose worker is a procedure records no `usage` (durable engine); an attempt counts from its `AttemptMade` or a `StepRejected` with none of its step, round and attempt | R6 | [ADR 0107](../../docs/decisions/0107-run-records-carry-usage-only-for-model-steps.md) |

## Threat model

| Element | S | T | R | I | D | E | OWASP mapping | Mitigation |
|---------|---|---|---|---|---|---|---------------|------------|
| `/trial-sets` (new calls) | ✓ | ✓ | ✓ | | ✓ | | API Top 10 API1, API4, API6 | Same client token, rate limit and Idempotency-Key as the engine's other calls; k at most 5; one running set per slice; operations recorded with caller and time |
| Model use multiplied by k | | | | | ✓ | | LLM10 unbounded consumption, ASI08 cascading failures | Set budget $2 and 24 hours on top of each run's $0.50 and 30 minutes; no trial starts that could pass $2; trials only on the developer's explicit request |
| Worker registration's `project` | ✓ | ✓ | | | | ✓ | ASI03 identity and privilege abuse, API1 | Accepted only with the slice and step's worker token, which only the launcher holds; a run calls only its own project's registration, so a registration cannot move another project's run; the launcher still refuses to re-register a project whose run has not ended |
| A trial's code (model-written) in k projects | | ✓ | | | | ✓ | LLM05 improper output handling, ASI05 unexpected code execution | Each trial is an ordinary run: its step containers write only their writable paths; checks run in the check sandbox (gVisor, no egress) |
| The result (derived from run records) | | ✓ | | ✓ | | | LLM02 sensitive information disclosure | Holds run IDs, outcomes, counts and hashes only; read with the client or page token as runs are; erased with the slice |

## Project Structure

### Documentation (this feature)

```text
specs/012-run-variance/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   └── trial-sets.md
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks
```

### Source Code (repository root)

```text
src/engine/api/preview/2026-11-12-preview/openapi.yaml   # trial-set paths and schemas, run-trials, x-limits, WorkerRegistration.project
src/engine/api/preview/2026-11-12-preview/run-event.schema.json  # RunStarted trial_set, trial
src/engine/api/preview/2026-11-12-preview/slice-defs.schema.json # trial_set_id
src/engine/trials/                       # new: the result from run records (R6)
src/engine/store/                        # trial-set records in the slice's folder
src/engine/api/trial_sets.go             # new: the resource and the run-trials job (R1, R4, R5)
src/engine/api/workers.go                # registrations by slice, step and project (R3)
src/engine/api/runs.go, durable.go       # RunStart.TrialSet/Trial; ready() by project
src/engine/api/operations.go             # the run-trials kind
src/engine/durable/, machine/            # RunStarted's new fields; workflow version 3
src/engine/method/method.sha256          # derived
src/deploy/ap                            # trials: build-workers for each project, then the client; register_worker sends project
src/cli/                                 # trials, trial-sets, trial-set; contract.md rows
src/tests/acceptance/                    # new suite: two trials end to end with the recorded model
```

**Structure Decision**: the result's rules have one owner, `engine/trials`, a pure function from run
records to the result, used by `getTrialSet` and `listTrialSets`; the set's job in `api` only
sequences runs through the existing run path.

**Existing tests expected to change** (each needs the owner's approval by name before its commit;
any other is stopped and asked for):

- Tests that list the engine contract's paths, operation kinds or error codes exactly (found by the
  code author, then asked for).
- Tests that list `RunStarted`'s or `Run`'s fields exactly, if any.
- Tests of worker registrations that pin a registration's fields or the registry's key
  (`api/workers_test.go`, `worker_registrations_restart_test.go`, `store/worker_registrations_test.go`),
  if they break.
- Launcher tests that list the commands it intercepts, or pin `register_worker`'s body.

## Exceptions

| Rule | Location | Reason |
|------|----------|--------|
| Outside-in, test-first: acceptance tests are seen failing before the code | `src/tests/acceptance/trials_test.go` (T012) | The Compose-stack suite runs only in CI (owner's rule: no heavy local tests). It is seen failing on the CI run of a push of T001 and T012 alone, before any code; the engine-rig tests T007 and T010 are seen failing locally. Recorded 2026-11-27 (analyze round 1, T1) |
| Outside-in, test-first: tests are seen failing before the code | `scripts/tests/test_build_workers_project_registration.py` (T004) | The build-workers rig needs real Docker and gVisor (no heavy local tests). It is seen failing on the CI run of a push of the T003 and T004 tests before T005's code. Owner, 2026-11-27 |

## Complexity Tracking

| Violation | Why needed | Simpler alternative rejected because |
|-----------|------------|--------------------------------------|
| Principle XII: trial sets have no tenant scope | One developer by scope (feature 001 FR-021) | Not applicable until the hosted version, as for slices (feature 001's row). Approved by the owner 2026-11-27 (T002) |
| Principle XV: the engine contract's `2026-11-12-preview` gains the `OperationKind` value `run-trials` in place, which the pinned oasdiff reports as breaking (`response-property-enum-value-added`, research R9) | `getOperation` must return the set's operation kind; every client is generated from this contract in the same commit | Reusing the kind `run` would make a `run` operation target something that is not a run; a new preview version needs a deprecation period and two served versions. Approved by the owner 2026-11-27 ([ADR 0105](../../docs/decisions/0105-preview-contract-gains-run-trials-in-place.md)) |
