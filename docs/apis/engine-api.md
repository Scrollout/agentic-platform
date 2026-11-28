---
type: API
title: Engine API
description: The engine's public HTTP API, which the terminal client and the run page call to turn a description into an approved slice, build it, and follow the work.
tags: [apis, engine, openapi, slices, runs, trial-sets]
status: draft
resource: ../../src/engine/api/preview/2026-11-12-preview/openapi.yaml
sources:
  - id: contract
    resource: ../../src/engine/api/preview/2026-11-12-preview/openapi.yaml
    title: Engine OpenAPI contract, version 2026-11-12-preview
  - id: terminal
    resource: ../../src/cli/contract.md
    title: Terminal contract
  - id: adr-0100
    resource: /decisions/0100-preview-contract-gains-enum-values-in-place.md
    title: "ADR 0100: The engine's preview contract gains enum values in place"
  - id: adr-0105
    resource: /decisions/0105-preview-contract-gains-run-trials-in-place.md
    title: "ADR 0105: The engine's preview contract gains the operation kind run-trials in place"
  - id: adr-0102
    resource: /decisions/0102-trial-set-runs-fresh-builds-and-derives-its-result.md
    title: "ADR 0102: A trial set runs fresh builds of a slice and derives its result from their records"
  - id: adr-0019
    resource: /decisions/0019-principle-xv-compatible-public-apis.md
    title: "ADR 0019: Principle XV, compatible public APIs"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-28T15:00:00Z }
---

# Purpose

It turns a developer's description into an approved slice and builds it into accepted working
software. It is the only way to manage slices and runs. The terminal client (`src/cli`) and the
run page call it. The authoritative contract is
[openapi.yaml](../../src/engine/api/preview/2026-11-12-preview/openapi.yaml), OpenAPI 3.1, version
`2026-11-12-preview`. How the terminal client uses it is in the
[terminal contract](../../src/cli/contract.md). The engine calls the [check API](/apis/check-api.md)
to judge each step.

Sources: [Engine OpenAPI contract](../../src/engine/api/preview/2026-11-12-preview/openapi.yaml), [Terminal contract](../../src/cli/contract.md).

# Endpoints

Every operation needs the `api-version` query parameter, except the provider-operations list.
Each operation names its permission in `x-operation-name`.

| Group | Operations |
|-------|------------|
| slices | `POST /slices` (createSlice), `GET /slices`, `GET /slices/{sliceId}`, `DELETE /slices/{sliceId}`, `PUT /slices/{sliceId}/goals`, `/rules` and `/contract`, `POST /slices/{sliceId}/answers`, `POST /slices/{sliceId}/approvals`, `POST /slices/{sliceId}/runs` (startRun) |
| runs | `GET /runs`, `GET /runs/{runId}`, `GET /runs/{runId}/events`, `GET /runs/{runId}/artifacts/{hash}`, `GET /runs/{runId}/comparison`, `POST /runs/{runId}/resume`, `/replay` and `/hand-over` |
| operations | `GET /operations/{operationId}`, `POST /operations/{operationId}/cancel` |
| captured patterns | `POST /captured-patterns` (captureRun), `GET /captured-patterns`, `GET /captured-patterns/{capturedPatternId}`, `DELETE /captured-patterns/{capturedPatternId}`, `PUT /captured-patterns/{capturedPatternId}/parameters`, `POST /captured-patterns/{capturedPatternId}/proof-slice`, `/approval` and `/withdrawal` |
| trial sets | `POST /slices/{sliceId}/trial-sets` (startTrialSet), `GET /slices/{sliceId}/trial-sets` (listTrialSets), `GET /trial-sets/{trialSetId}` (getTrialSet) |
| workers | `POST /workers` (a step worker announces that it is ready, for one project of its slice) |
| provider | `GET /providers/Agentic.Engine/operations` (lists this service's operations) |

Every state change but `registerWorker` returns an operation (HTTP 202) that the client follows with
`getOperation`; `registerWorker` answers 200 at once and replaces the step's endpoint when repeated.

A captured pattern (feature 011) turns a run that ended done into a pattern that builds later
slices of the same kind with no model call. `startRun` takes an optional `pattern`, a captured
pattern's or the base pattern's ID; without it the most recently approved usable captured pattern
that fits is used, otherwise the base pattern, and the run records the choice and why. A captured
pattern is used only after the developer approves it following a passing proof (a run of its
proof slice) under the current method. The limits are `x-limits`
`captured-pattern-parameters-max` (32), `parameter-places-max` (10000) and
`parameter-value-max-chars` (64). The new error codes, operation kinds and the slice history kind
`made-for-proof` were added in place to this preview version
([ADR 0100](/decisions/0100-preview-contract-gains-enum-values-in-place.md)).

A trial set (feature 012) builds an approved slice `trials` times (1 to `x-limits.trials-max`, 5;
`trials-default`, 3), one run after another, into the new projects `<project_prefix>-1` to
`<project_prefix>-<trials>`, each run reusing no earlier result. `startTrialSet` refuses as
`startRun` without `pattern` does, then `WorkersNotReady` and `ProjectExists` for the first of the
projects, then `RunActive` while another set of the slice runs; its operation, of kind
`run-trials`, lasts until the set ends, and cancelling it cancels the running trial. A set stops
`budget` when one more trial would pass `trial-set-cost-usd` ($2), `time` after `trial-set-hours`
(24), `slice-changed` or `workers`; it ends `failed` with `failed_because` when its operation
fails. `getTrialSet` and `listTrialSets` (newest first, at most 50 per page) give each set's
result, computed from its trials' run records on each read and changing nothing: done out of
ended, the unstable mark, the spread (lowest, median, highest) of each step's and each trial's
attempts, review rounds, seconds, tokens and cost, which steps' files differ, and `previous`, the
change since the slice's latest earlier set that ended or stopped with an ended trial. A `WorkerRegistration` names its
project, and a run uses only its own project's workers. The operation kind `run-trials` was added
in place; every other change is additive
([ADR 0102](/decisions/0102-trial-set-runs-fresh-builds-and-derives-its-result.md),
[ADR 0105](/decisions/0105-preview-contract-gains-run-trials-in-place.md)).

Sources: [Engine OpenAPI contract](../../src/engine/api/preview/2026-11-12-preview/openapi.yaml), [ADR 0102](/decisions/0102-trial-set-runs-fresh-builds-and-derives-its-result.md), [ADR 0105](/decisions/0105-preview-contract-gains-run-trials-in-place.md).

# Authentication

Bearer tokens, issued per session. The client token may call every developer operation. The page
token may call only GET operations. A worker token, issued for one step of one slice, may call
only `registerWorker`. Any other call returns 403 `Forbidden`. A missing or unaccepted token
returns 401. Each token may make at most 20 requests in any one-second window; more return 429
with `Retry-After`. A request without an accepted token is counted by its source address, never
by a header. A request body is at most 1048576 bytes and must be `application/json`.

Sources: [Engine OpenAPI contract](../../src/engine/api/preview/2026-11-12-preview/openapi.yaml).

# Errors

Errors are RFC 9457 problems (`type`, `title`, `status`, `detail`) with a stable `code`. The
contract lists the codes, and `x-error-codes` gives the codes each status may carry per
operation. `OperationFailed` may end any operation and is not repeated per operation. Examples:
`NotFound`, `RunActive`, `NotApproved`, `StaleContentHash`, `IdempotencyKeyReused`,
`UnsupportedApiVersionValue` (with `supported_versions`). A deprecated operation is marked
`deprecated: true` with `x-sunset`, and its responses carry `Deprecation`, `Sunset` and a
`Link: rel="deprecation"` header (Principle XV).

Sources: [Engine OpenAPI contract](../../src/engine/api/preview/2026-11-12-preview/openapi.yaml), [ADR 0019](/decisions/0019-principle-xv-compatible-public-apis.md).

# Pagination and idempotency

- Lists (`listSlices`, `listRuns`, `listCapturedPatterns`, `listTrialSets`, run events, run comparison) take an opaque `page_token` and
  return `next_page_token`. Slices, runs, captured patterns and trial sets are newest first, at most 50 per page. Run events
  are in `seq` order, at most 100 per page. Until a run ends, the last page still carries a
  token, and using it later returns the events recorded since.
- State-changing operations but `registerWorker` accept an `Idempotency-Key`. The engine keeps a key and its first
  result for 24 hours. Repeating a request with the same key returns the first result. The same
  key with a different body returns 409 `IdempotencyKeyReused`.

Sources: [Engine OpenAPI contract](../../src/engine/api/preview/2026-11-12-preview/openapi.yaml), [Terminal contract](../../src/cli/contract.md).

# Examples

`POST /slices?api-version=2026-11-12-preview` with an `Idempotency-Key` header and a
`CreateSliceRequest` body returns 202 and an operation. `GET /operations/{operationId}` then
shows its progress and outcome.
