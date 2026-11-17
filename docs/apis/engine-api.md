---
type: API
title: Engine API
description: The engine's public HTTP API, which the terminal client and the run page call to turn a description into an approved slice, build it, and follow the work.
tags: [apis, engine, openapi, slices, runs]
status: draft
resource: ../../src/engine/api/preview/2026-11-12-preview/openapi.yaml
sources:
  - id: contract
    resource: ../../src/engine/api/preview/2026-11-12-preview/openapi.yaml
    title: Engine OpenAPI contract, version 2026-11-12-preview
  - id: terminal
    resource: ../../src/cli/contract.md
    title: Terminal contract
  - id: adr-0019
    resource: /decisions/0019-principle-xv-compatible-public-apis.md
    title: "ADR 0019: Principle XV, compatible public APIs"
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-17T22:00:00Z }
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
| workers | `POST /workers` (a step worker announces that it is ready) |
| provider | `GET /providers/Agentic.Engine/operations` (lists this service's operations) |

Every state change returns an operation (HTTP 202) that the client follows with `getOperation`.

Sources: [Engine OpenAPI contract](../../src/engine/api/preview/2026-11-12-preview/openapi.yaml).

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

- Lists (`listSlices`, `listRuns`, run events, run comparison) take an opaque `page_token` and
  return `next_page_token`. Slices and runs are newest first, at most 50 per page. Run events
  are in `seq` order, at most 100 per page. Until a run ends, the last page still carries a
  token, and using it later returns the events recorded since.
- State-changing operations accept an `Idempotency-Key`. The engine keeps a key and its first
  result for 24 hours. Repeating a request with the same key returns the first result. The same
  key with a different body returns 409 `IdempotencyKeyReused`.

Sources: [Engine OpenAPI contract](../../src/engine/api/preview/2026-11-12-preview/openapi.yaml), [Terminal contract](../../src/cli/contract.md).

# Examples

`POST /slices?api-version=2026-11-12-preview` with an `Idempotency-Key` header and a
`CreateSliceRequest` body returns 202 and an operation. `GET /operations/{operationId}` then
shows its progress and outcome.
