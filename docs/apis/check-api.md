---
type: API
title: Check API
description: The check service's HTTP API, called only by the engine, which judges one attempt of a build step with a named check, reads step files, runs fixed procedures, and keeps a project's version history.
tags: [apis, check, openapi]
status: draft
resource: ../../src/check/api/preview/2026-11-13-preview/openapi.yaml
sources:
  - id: contract
    resource: ../../src/check/api/preview/2026-11-13-preview/openapi.yaml
    title: Check OpenAPI contract, version 2026-11-13-preview
  - id: engine-contract
    resource: ../../src/engine/api/preview/2026-11-12-preview/openapi.yaml
    title: Engine OpenAPI contract
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-27T12:00:00Z }
---

# Purpose

It judges one attempt of a build step by running one named check, returns the files an attempt
left in its step's writable paths, produces one procedure step's files by running its fixed
procedure, writes a captured pattern's filled step into the project, and keeps the project's
version history. Only the engine calls it. The authoritative
contract is
[openapi.yaml](../../src/check/api/preview/2026-11-13-preview/openapi.yaml), OpenAPI 3.1, version
`2026-11-13-preview`. The engine's own API is the [engine API](/apis/engine-api.md).

One program serves the contract in three deployments:

- the check service reads the project folder and never writes it (`runCheck`, `readStepFiles`);
- a procedure step's worker serves `runProcedure` for its step's procedure only;
- the project worker holds the projects folder writable and serves `commitStep`, `restoreStep`
  and `writeStep`.

An operation a deployment does not serve returns 404 (`UnknownCheck`, `UnknownProcedure`,
`UnknownStep` or `VersionHistoryNotServed`).

Sources: [Check OpenAPI contract](../../src/check/api/preview/2026-11-13-preview/openapi.yaml).

# Endpoints

Every operation needs the `api-version` query parameter.

| Method | Path | Description |
|--------|------|-------------|
| POST | `/checks/{checkName}/run` | Run a check on one attempt of a step (`runCheck`) |
| POST | `/step-files` | Read the files an attempt left in its step's writable paths (`readStepFiles`) |
| POST | `/procedures/{procedureName}/run` | Run a fixed procedure for one attempt of a step (`runProcedure`) |
| POST | `/project-versions/commit` | Record an accepted step, or an open review round, as the project's next commit (`commitStep`) |
| POST | `/project-versions/restore` | Restore the project to its last commit before an attempt (`restoreStep`) |
| POST | `/project-versions/write` | Make a `fill` step's writable files exactly the given ones, without committing (`writeStep`, feature 011) |

Sources: [Check OpenAPI contract](../../src/check/api/preview/2026-11-13-preview/openapi.yaml).

# Authentication

One bearer scheme, `engineToken`: the token this session issued to the engine for calling this
deployment. No other component holds it, and any other token returns 401. At most 20 requests per
token per second and 20 per source address without a token; more return 429 `RateLimited` with
`Retry-After`. At most 2 calls run at once; another returns 503 `Busy` with `Retry-After`. A
call that exceeds its `time_limit_seconds` (at most 600) is stopped and returns 504
`TimeLimitExceeded`. The limits are declared under `x-limits` in the contract.

Sources: [Check OpenAPI contract](../../src/check/api/preview/2026-11-13-preview/openapi.yaml).

# Errors, pagination and idempotency

- Errors are RFC 9457 problems with a stable `code`, as in the engine API. Codes include
  `InvalidRequest`, `MissingApiVersionParameter`, `UnsupportedApiVersionValue`,
  `ProjectStateDiffers`, `HistoryChanged`, `UnreadableProject`, `InputHashMismatch`,
  `ResponseTooLarge`, `StepFilesTooLarge` and `WriteFailed`.
- No operation is a list, so none paginates.
- No operation takes an `Idempotency-Key`. `runCheck` and `readStepFiles` change nothing,
  `runProcedure`, `restoreStep` and `writeStep` leave the same files when repeated, and a repeated
  `commitStep` makes no second commit.

Sources: [Check OpenAPI contract](../../src/check/api/preview/2026-11-13-preview/openapi.yaml).

# Examples

`POST /checks/{checkName}/run?api-version=2026-11-13-preview` with a `CheckRequest` body returns a
`CheckVerdict`: `passed`, `reason`, `refs` and `project_files`, and for NoBlockingFindingOpen the
`confirmed` positions of the request's findings. In a review round after the first the request
also carries `round_scope` (the round's open findings and the lines the fix before it changed);
the service refuses it without `round` or in round 1, and refuses its absence after round 1,
with 400 `InvalidRequest`.
