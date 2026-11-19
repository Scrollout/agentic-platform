# Implementation Plan: Runs and operations survive a platform stop, on an adopted durable-execution engine

**Branch**: `002-durable-execution` | **Date**: 2026-11-19 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/002-durable-execution/spec.md` (clarified
2026-11-18)

## Summary

The engine's own durable execution (resume, checkpoints, retries, timeouts, the in-memory job
queue) is replaced by Temporal, as [ADR 0056](../../docs/decisions/0056-durable-execution-custom-for-the-mvp-temporal-after.md)
decided. Temporal's server and PostgreSQL run in Compose, pinned by digest (research R1). Each
run is one workflow whose code is the engine's step rules and whose calls (step workers, checks,
the project worker, the record) are activities (R3). A run a stop interrupted continues by itself
and makes only the interrupted attempt again (FR-003, FR-005). Each state-changing request is
recorded in the engine store before it is acknowledged and then runs as a workflow (R4). The run
record stays the engine's, with its published schema unchanged (R2). One retry rule, declared in
the engine contract, replaces three hand-written ones (R5). Everything feature 001 delivered keeps
working (US3).

## Technical Context

**Language/Version**: Go 1.26 (the engine and terminal client's stack); POSIX sh for the
launcher's Compose changes

**Primary Dependencies**: Temporal server 1.32.2 (`temporalio/server`), `temporalio/admin-tools`
1.32.2 (schema job), Temporal Go SDK `go.temporal.io/sdk` v1.51.0, PostgreSQL 16
(`postgres:16-alpine`); everything else as feature 001

**Storage**: the engine store as feature 001 (run record, artifacts, slices) plus
`operations/OP-<n>.json` and `operation-sequence.json`; Temporal's state in PostgreSQL (volume
`durable-store`) ([data model](data-model.md))

**Testing**: Go `testing` with Temporal's `testsuite` (workflow unit tests, time skipping) and
`worker.NewWorkflowReplayer` (replay) in CI's `go` job; the engine against real Temporal and
PostgreSQL in the `docker` job; the stop-point acceptance suite in the `e2e` job; feature 001's
suites unchanged

**Target Platform**: Linux with Docker 28+, Compose and gVisor, as feature 001

**Project Type**: the local multi-container tool of feature 001

**Performance Goals**: a run stopped and continued ends as an uninterrupted one (SC-001); a lost
engine is found within 30 s (activity heartbeat timeout)

**Constraints**: the added services use at most 512 MiB idle (SC-004; measured about 200 MiB,
research S14); no egress; non-root, read-only root, no capabilities; nothing protected in
Temporal's state (FR-016)

**Scale/Scope**: one developer, one machine, one run at a time (feature 001's RunActive rule)

## Constitution Check

*Evaluated before research and again after design: PASS with the Complexity Tracking rows below.
Feature 001's approved exceptions (II, V, XI partial, XII, XIII partial, XVI partial) still apply
to the platform; this feature closes its Principle VI exception and its Single-owner exception,
and extends its Principle V exception to the two added services.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Needs before solutions | Pass | Serves N-040 (owner confirmed 2026-11-18); no design taken from another project |
| II. Two planes | Pass (feature 001's exception unchanged) | Temporal and PostgreSQL join the one local deployment |
| III. API-first | Pass | No management path bypasses the API; the contract changes additively first (contracts/) |
| IV. Asynchronous operations | Pass | Every state-changing request is recorded, queued (task queue `operations`) and answered with an operation reference; retries with backoff and jitter (R5); failed operations stay visible; one task queue per workload class (`runs`, `operations`) |
| V. Zero-trust identity | Exception (Complexity Tracking) | Temporal's frontend and PostgreSQL trust their internal networks and a per-install password |
| VI. Adopt mature OSS | Pass | Temporal and PostgreSQL adopted after the evaluation (research, Principle VI evaluation); the custom code kept is justified there; feature 001's exception is removed |
| VII. Pluggable providers | Not affected | No provider changes |
| VIII. Containers | Pass | Three pinned images, non-root, read-only root, no capabilities, internal networks only (S14); no Kubernetes dependency |
| IX. Knowledge stays true | Pass | ADRs 0065 to 0069, components `temporal` and `postgresql`, the Temporal reference updated, the architecture overview at implementation |
| X. No silent loss | Pass | A run and an operation are acknowledged only after they are recorded (R4); a run continues after a stop without repeating a completed side effect (R3); upgrades update the schema before the server (R1) |
| XI. Agent data privacy | Pass (feature 001's partial exception unchanged) | Temporal holds IDs, hashes and numbers only (R2); retention 24 h; erasure deletes workflows (R8); the server's version check is off (S11) |
| XII. Tenant isolation | Not applicable | One developer |
| XIII. Secure supply chain | Pass | Images and the SDK pinned by version and digest; the schema job and server share one version line |
| XIV. Safe defaults | Pass | No new agent capability; a failed durable record blocks the request (503) |
| XV. Compatible public APIs | Pass | Additive only: `x-limits` retry values, a 503 response, the operation-retention text (contracts/); `oasdiff` gates it |
| XVI. Human control | Pass (feature 001's exception unchanged) | Protected paths and changed existing tests carry the owner's trailers |
| XVII. Declare, derive, converge | Pass | The schema job converges the database; the retry rule is declared once and its use checked against it |
| XVIII. Abstractions say what | Pass | `src/retry` has three callers; activities are named for what they do |
| XIX. Evals | Pass | No prompt, model or tool changes; the regression eval suites run before merge as for any agent-logic change |
| XX. Deterministic acceptance | Not affected | No check of agent work changes |
| Contract-first | Pass | contracts/ drafts the changes; the engine contract and terminal contract change before code |
| Existing tests change only with approval | Pass, by task | The plan names the existing tests expected to change (Project Structure); each task that changes one names it and waits for the owner's approval |
| Fresh review | Pass | A fresh read-only reviewer per story and before done, judging the CI run |
| Layout, Clean tree, Built artifacts | Pass | New code under `src/`; tests excluded by `src/.dockerignore` as before |

## Decisions

Each decision that outlives this feature has a record in `docs/decisions/`, linked back here.

| Decision | Research | Record |
|----------|----------|--------|
| Temporal's server (all services in one process) and PostgreSQL 16 run in Compose, pinned by digest, with a schema job that converges both databases and the namespace before the server starts (owner: PostgreSQL, 2026-11-19) | R1 | [ADR 0065](../../docs/decisions/0065-temporal-and-postgresql-in-compose.md) |
| The run record stays the engine's, with its schema; Temporal keeps only execution state, and every event is written by an idempotent activity (owner, 2026-11-19) | R2 | [ADR 0066](../../docs/decisions/0066-the-run-record-stays-the-engines.md) |
| A run is one workflow running the engine's step rules; every call is an activity with a heartbeat; a stop repeats only the interrupted attempt and the run continues by itself | R3 | [ADR 0067](../../docs/decisions/0067-a-run-is-one-temporal-workflow.md) |
| An operation is recorded in the engine store before it is acknowledged, then runs as a workflow with the operation's ID; the record keeps its ID and Idempotency-Key for 24 h | R4 | [ADR 0068](../../docs/decisions/0068-operations-recorded-then-run-as-workflows.md) |
| One retry rule, declared in the engine contract's `x-limits` and applied by `src/retry` with durable timers, replaces Temporal's unjittered activity retries and the three hand-written forms (owner, 2026-11-19) | R5 | [ADR 0069](../../docs/decisions/0069-one-retry-rule-declared-in-the-engine-contract.md) |

## Threat model

New elements: the Temporal server (frontend, history, matching, worker in one process), the
schema job, PostgreSQL and its volume, the engine's Temporal worker, the workflow histories, the
operation records. Trust boundaries: the `durable` network (engine to server) and the
`durable-store` network (server and schema job to PostgreSQL). No new external interface, model,
tool or agent context; no untrusted content enters a model's context through this change.

| Element | S | T | R | I | D | E | OWASP mapping | Mitigation |
|---------|---|---|---|---|---|---|---------------|------------|
| Temporal frontend | ✓ | ✓ | | ✓ | ✓ | ✓ | ASI03 identity and privilege abuse, LLM10 unbounded consumption | Reachable only on the internal `durable` network, which only the engine joins; no published port; agent and step containers are on other networks (feature 001's isolation); the default authorizer's open access is the Principle V exception below |
| PostgreSQL | ✓ | ✓ | | ✓ | ✓ | | ASI03 | Only on `durable-store`, joined by the server and the schema job; password made by `ap` per install (mode 600, in the state folder), passed by Compose from that file; runs as 70:70, no capabilities |
| Workflow histories | | ✓ | | ✓ | | | LLM02 sensitive information disclosure | Hold IDs, hashes, numbers and short reasons only (R2); retention 24 h; deleted with the slice (R8); no payload codec needed because nothing protected is in them |
| Engine's Temporal worker | | ✓ | ✓ | | ✓ | | ASI08 cascading failures | Activities are the engine's own calls with their feature 001 limits; heartbeat timeouts bound a lost call; one run at a time |
| Operation records | | ✓ | ✓ | ✓ | | | — | In the engine store (mode 700 volume), atomic writes; keys stored as SHA-256 only |
| Retry rule | | | | | ✓ | | LLM10 | Capped exponential backoff with jitter, at most 3 retries (S15) |
| Images | ✓ | ✓ | | | | | LLM03 supply chain | Pinned by digest in `images.lock`; no signature available upstream (Scorecard Signed-Releases 0), so the digest is the check, as for nginx and cosign |
| Server version check | | | | ✓ | | | — | Off (`frontend.enableServerVersionCheck: false`); the networks have no egress anyway |

## Project Structure

### Documentation (this feature)

```text
specs/002-durable-execution/
├── plan.md              # this file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   └── engine-api-changes.md   # draft; authoritative copies stay with the engine and the client
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks
```

### Source Code (repository root)

```text
src/
├── retry/                         # new module: the retry rule's Wait(n, random); go.work gains it
├── engine/
│   ├── api/preview/2026-11-12-preview/openapi.yaml   # x-limits retry values, 503 Unavailable, operation retention text
│   ├── api/operations.go          # operation record and outbox; status from the workflow; backoff removed
│   ├── api/runs.go                # start, resume, cancel and hand-over through workflows and signals
│   ├── durable/                   # new: Temporal client and worker setup, task queues, the activities
│   ├── machine/                   # the step rules as workflow code; resume, load and historyBackoff removed
│   ├── store/                     # RecordEvent once per seq; operation records and their sequence
│   └── cmd/engine/                # start the Temporal worker; refuse runs left open by feature 001 (R7)
├── cli/runs.go                    # the client's resend wait from src/retry
├── cli/contract.md                # the resume paragraph, the retry values' home, interrupted's meaning
└── deploy/
    ├── compose.yaml               # durable-store, durable-schema, durable; networks durable and durable-store
    ├── durable/                   # dynamic config, the schema job's script
    ├── images.lock                # temporal-server, temporal-admin-tools, postgres pins
    └── ap                         # the PostgreSQL password file; `down` keeps the durable-store volume
src/tests/acceptance/              # new: stop-point suite (SC-001)
```

**Structure Decision**: the change lives in the engine (`src/engine`), a new shared module
`src/retry`, the client's resend wait, and the deployment packaging. `src/check`, `src/page` and
the agents do not change.

**Existing tests expected to change** (each with the owner's approval, recorded per task with
`Tests-approved-by:`): the machine's resume and restart tests
(`resume_after_interrupt_test.go`, `restart_interrupt_test.go`, `restart_usage_test.go`,
`open_round_resume_test.go`, `fix_round_test.go`'s `TestResumedRunContinuesAFixWithTheSameRound`,
`reuse_test.go`'s resume cases, `history_test.go`'s retry and crash cases,
`history_jitter_test.go`), the API's operation and retry tests (`skeleton_test.go`'s retry and
backoff cases, `runs_ops_test.go`'s resume and restart cases, `operations_wait_test.go`), and
`cmd/engine`'s `restart_test.go` and `close_waits_test.go`. The tasks first try to keep each
assertion and change only how the test drives the engine.

## Feature 001 items this feature replaces

Backlog T086 (when `RunInterrupted` is appended: replaced by R3), T138 (attempts across a stop:
FR-003), T158 (a failed `RunCompleted` append: replaced by the activity's retry, R5) and T055c's
retry items (the client's wait: R5). T159 (which refused answers count as attempts) stays in the
backlog: it is not this feature's decision.

## Exceptions

None.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Principle V (partial), extending feature 001's exception: Temporal's frontend accepts any caller on the internal `durable` network, and PostgreSQL authenticates the server with a password `ap` makes per install | One developer on one machine; the two services are reachable only from the engine and the server on internal networks, as feature 001's services are; owner, 2026-11-19 | mTLS with a local CA and per-session certificates for both, more parts and tests for a local tool; required with hosting, with feature 001's other V and XI exceptions |

The project owner approved the Principle V extension on 2026-11-19, for this local,
single-developer version only. This feature removes feature 001's Principle VI exception (the
engine's own durable execution, D56) and its Single-owner exception (the retry wait) when it is
done (SC-005).
