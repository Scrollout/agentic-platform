# Research: Runs and operations survive a platform stop, on an adopted durable-execution engine

Phase 0 of the [plan](plan.md) for the [spec](spec.md). Each decision names its deciding source:
a cited primary source or an experiment reproduced on the owner's machines (constitution,
Evidence before action). Decisions marked *owner* were put to the project owner as direct
questions on 2026-11-19.

## Sources

| ID | Source |
|----|--------|
| S1 | [temporalio/temporal releases](https://api.github.com/repos/temporalio/temporal/releases) (server v1.32.2, 2026-11-02, MIT); [temporalio/sdk-go releases](https://api.github.com/repos/temporalio/sdk-go/releases) (v1.51.0, 2026-11-10, MIT); [temporalio/cli releases](https://api.github.com/repos/temporalio/cli/releases) (v1.10.1, 2026-11-13) |
| S2 | [Temporal's Compose examples, `samples-server/compose`](https://github.com/temporalio/samples-server/tree/main/compose): `temporalio/server` 1.32.2, `temporalio/admin-tools` 1.32.2 running `temporal-sql-tool` for both databases, PostgreSQL 16, plugin `postgres12`; [`temporalio/auto-setup`](https://hub.docker.com/r/temporalio/auto-setup) is deprecated (last tag 1.29.7); [v1.30.1 release notes](https://github.com/temporalio/temporal/releases/tag/v1.30.1): the CLI moved out of the server image into admin-tools |
| S3 | [Visibility stores](https://docs.temporal.io/self-hosted-guide/visibility) (PostgreSQL 12+, MySQL 8.0.17+, SQLite 3.31+); [CLI dev server](https://docs.temporal.io/cli/server) ("not for production"); [SQLite visibility](https://docs.temporal.io/self-hosted-guide/visibility/sqlite) (a file-based schema cannot be upgraded); [service configuration](https://docs.temporal.io/references/service-configuration) (`pluginName` `mysql` or `postgres`; `numHistoryShards` fixed at first start) |
| S4 | [Upgrading the server](https://docs.temporal.io/self-hosted-guide/upgrade-server): update the schema first, then the binary; one minor version at a time; a server refuses an older schema |
| S5 | [Go SDK: workflow basics and determinism](https://docs.temporal.io/develop/go/workflows/basics); [workflow definition constraints](https://docs.temporal.io/workflow-definition); [versioning, `GetVersion`](https://docs.temporal.io/develop/go/workflows/versioning); [testing suite, replayer](https://docs.temporal.io/develop/go/testing-suite); [`testsuite` package](https://pkg.go.dev/go.temporal.io/sdk/testsuite) |
| S6 | [Retry policies](https://docs.temporal.io/encyclopedia/retry-policies) (defaults, `MaximumAttempts` 1 = no retry; no jitter is documented); server source [`service/history/workflow/retry.go`](https://github.com/temporalio/temporal/blob/main/service/history/workflow/retry.go) computes activity retry intervals with no jitter call (code reading, not documentation) |
| S7 | [Detecting activity failures](https://docs.temporal.io/encyclopedia/detecting-activity-failures) (StartToClose always set; heartbeat timeout detects a lost worker; cancellation reaches an activity through its heartbeat); [timers](https://docs.temporal.io/develop/go/workflows/timers) (durable across outages, days to years); [message passing](https://docs.temporal.io/develop/go/workflows/message-passing) (signals, queries, updates) |
| S8 | [Workflow ID and reuse/conflict policies](https://docs.temporal.io/workflow-execution/workflowid-runid) (`RejectDuplicate`, `UseExisting`; protection lasts for the retention period) |
| S9 | [Temporal Service: deletion and retention](https://docs.temporal.io/temporal-service/temporal-server) (`DeleteWorkflowExecution` removes the execution and its history, asynchronously; retention at least 1 hour for a local namespace; a closed execution is removed when its retention ends); [CLI `workflow delete`](https://docs.temporal.io/cli/workflow) |
| S10 | [Limits](https://docs.temporal.io/workflow-execution/limits) and [Cloud limits](https://docs.temporal.io/cloud/limits): 2 MB per payload, 4 MB per gRPC message; history 51,200 events or 50 MB; at most 2,000 pending activities |
| S11 | [Self-hosted security](https://docs.temporal.io/self-hosted-guide/security): mTLS (`global.tls.frontend`), `Authorizer` and `ClaimMapper` plugins, the default authorizer allows every call; [data encryption](https://docs.temporal.io/production-deployment/data-encryption) (payload codec); server version check disabled with `frontend.enableServerVersionCheck: false` ([issue 7492](https://github.com/temporalio/temporal/issues/7492), [`version_checker.go`](https://github.com/temporalio/temporal/blob/main/service/frontend/version_checker.go)) |
| S12 | OpenSSF Scorecard, read 2026-11-19: [temporalio/temporal](https://api.securityscorecards.dev/projects/github.com/temporalio/temporal) 6.0 (2026-11-16; Maintained 10, Code-Review 9, Signed-Releases 0); [temporalio/sdk-go](https://api.securityscorecards.dev/projects/github.com/temporalio/sdk-go) 7.3 (2026-11-16); [postgres/postgres](https://api.securityscorecards.dev/projects/github.com/postgres/postgres) 6.1 (2026-10-19) |
| S13 | [PostgreSQL versioning policy](https://www.postgresql.org/support/versioning/) (each major version supported for 5 years; 16 until November 2028); [PostgreSQL licence](https://www.postgresql.org/about/licence/) (OSI-approved PostgreSQL Licence); [Docker Hub `postgres:16-alpine`](https://hub.docker.com/_/postgres) digest `sha256:1d5b02d3a9941fbd52437db4107f8c52d6297dac553ca1e5c299ebb3756b4857` (2026-11-19) |
| S14 | Experiment on the owner's clean VM (4 CPUs, 7 GB, Docker 29.10.0), 2026-11-19, the owner's choice of machine: `postgres@sha256:1d5b02d3…`, `temporalio/admin-tools@sha256:4a95866e…`, `temporalio/server@sha256:7bf69188…` (1.32.2) in Compose on an internal network, each `read_only: true`, `cap_drop: [ALL]`, `no-new-privileges`, tmpfs `/tmp` (and `/var/run/postgresql`); PostgreSQL as `user: "70:70"` with no capability; the server image's own user `temporal` (uid 1000). The admin-tools job made both databases and schemas (exit 0); the server became healthy (`operator cluster health` SERVING) and registered a namespace with 24 h retention. Idle after 1 to 2 minutes: PostgreSQL 120 to 124 MiB, server 72 to 76 MiB (about 200 MiB together, `docker stats`). After `compose stop` and `up` the server was healthy again in 4 s with the namespace kept. Startup logs show transient `shard status unknown` errors before the shards are owned. Whether `DeleteWorkflowExecution` removes every row could not be read from that run (system workflows share the tables); it is left to a test (R8). Images: server 167 MB, admin-tools 425 MB, PostgreSQL 420 MB. All containers, volumes and images were removed afterwards |
| S15 | [AWS Architecture Blog: Exponential Backoff And Jitter](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/) and [AWS Builders' Library: Timeouts, retries and backoff with jitter](https://aws.amazon.com/builders-library/timeouts-retries-and-backoff-with-jitter/): capped exponential backoff with jitter spreads retries |
| S17 | Experiment on the owner's host, 2026-11-19 (T004), Docker Compose v5.8.0: a file secret with `uid: "70"`, `gid: "70"`, `mode: 0400` was mounted unchanged (`1000 1000 600`, the host file's owner and mode), and a container user 70 could not read it |
| S16 | Feature 001: [ADR 0056](../../docs/decisions/0056-durable-execution-custom-for-the-mvp-temporal-after.md), [research D56](../001-prose-to-accepted-slice/research.md), the [plan's Complexity Tracking and Exceptions](../001-prose-to-accepted-slice/plan.md#complexity-tracking), [ADR 0059](../../docs/decisions/0059-run-store-is-json-lines-and-a-content-addressed-store.md) (run store), the engine code (`src/engine/machine`, `store`, `api/operations.go`, `api/runs.go`), the terminal client (`src/cli/runs.go`) and the terminal contract's "Idempotency and the run ID" (`src/cli/contract.md`) |

## Decisions

### R1. Temporal server and PostgreSQL in Compose

- **Decision**: three Compose services, every image pulled by digest from `src/deploy/images.lock`:
  - `durable-store`: `postgres:16-alpine`.
  - `durable-schema`: a one-off job from `temporalio/admin-tools:1.32.2`. It runs
    `temporal-sql-tool` `create`, `setup-schema -v 0.0` and `update-schema` for the `temporal`
    and `temporal_visibility` databases. Every step is safe to repeat, so the job converges the schema (Principle XVII:
    migrations stay an ordered history that the runner converges).
  - `durable`: `temporalio/server:1.32.2`, every service in one process, `numHistoryShards` 4,
    the version check off.
  - Hardened as feature 001's services are: `read_only`, `cap_drop: [ALL]`, `no-new-privileges`,
    tmpfs for `/tmp`; PostgreSQL as `70:70`; the server as its image's `temporal` user (S14).
  - Networks: `durable` (internal; the engine and the server only) and `durable-store`
    (internal; the server, the schema job and PostgreSQL only). No egress.
  - Volume `durable-store` holds the data; `ap down` keeps it (FR-010 of feature 003).
  - The namespace `ap` is registered by the engine when it connects to the server, with the
    retention `x-limits.idempotency-key-retention-hours` gives (24 h), and kept when it exists
    (owner, 2026-11-19, ADR 0070): the schema job runs before the server, so it cannot register
    it, and a fourth job on the `durable` network would widen that network. Limit: a later change
    of that retention does not reach an existing namespace; it needs its own task.
  - The PostgreSQL password is a file `ap` makes once in its own folder `durable/` in the state
    folder: the folder mode 700, so only the developer reaches it on the host, and the file mode
    644, so the containers' users (70 and 1000) can read it through the Compose secret (owner,
    2026-11-19, ADR 0070). Compose ignores a file secret's `uid`, `gid` and `mode` (S17), so a
    mode-600 file stays unreadable to them.
  - Memory limits 256 MiB each for the server and PostgreSQL; measured idle about 200 MiB
    together (S14), under SC-004's 512 MiB.
- **Owner**, 2026-11-19: PostgreSQL rather than the SQLite dev server.
- **Why**: PostgreSQL is a supported production store with a schema tool and an upgrade path
  (S3, S4). The SQLite dev server is not for production and its file schema cannot be upgraded
  (S3). `auto-setup` is deprecated (S2). The sample's layout is the vendor's own (S2).
- **Upgrades**: the schema job and the server share one version line in `images.lock`; the job
  runs before the server on every start, so an upgrade updates the schema first (S4). A bump
  moves one minor version at a time, each through the regression suites (Principle XIII).
- **Rejected**: SQLite dev server (not for production, S3); MySQL (no gain, and the vendor's
  sample and PostgreSQL's 5-year support favour PostgreSQL, S13); `auto-setup` (deprecated, S2);
  Elasticsearch visibility (a JVM on a small host; SQL visibility is supported, S3).

### R2. The run record stays the engine's; Temporal keeps only execution state

- **Decision**: the engine's run record (`slices/<slice>/events.jsonl` and the artifacts by hash,
  ADR 0059) stays the one record that inspect, compare, replay, the run page and the terminal
  client read. Temporal keeps what a run is doing (position, attempts, rounds, budgets, timers,
  waits). The workflow appends each event through an activity, `RecordEvent(slice, run, seq,
  event)`, which the store accepts once per `seq`: a repeat with the same content succeeds
  without writing, and other content under a taken `seq` is refused. A Temporal history holds
  IDs, hashes, numbers and short reasons only, never a prompt, completion, file or finding text
  (FR-016; payloads stay far below S10's 2 MB).
- **Owner**, 2026-11-19.
- **Why**: the record's schema is a published contract (FR-013) and feature 001's ended runs must
  stay readable (FR-015). A Temporal history is removed when its retention ends (S9), so it cannot
  be the lasting record. With the record outside Temporal, Temporal's history size limits (S10)
  only bound a run's step count, which feature 001's limits keep small.
- **Rejected**: deriving the record from Temporal's history (couples a published contract to
  Temporal's format and retention; migrating feature 001's runs).

### R3. One workflow per run; the engine's rules are its code, its calls are activities

- **Decision**:
  - Each run is one workflow, ID = the run ID (`RUN-<n>`), on the task queue `runs`. Started with
    `RejectDuplicate` (S8), so a run never runs twice.
  - The workflow is the step machine's rules: the pattern's steps in order, reuse, attempts,
    review rounds, budgets, hand-over and outcome. These rules stay the platform's own (ADR 0056)
    and are written as workflow code in `src/engine/durable`, keeping the names and the tests'
    expectations of `src/engine/machine`'s driver where they still hold (the driver stays, unused by
    the engine, until T040).
  - Every call is an activity: the step worker's call, each check, the project worker's restore
    and commit, the reuse lookup, `PutArtifact` and `RecordEvent`. Each activity has a
    StartToClose of the call's own time limit plus 30 s and a heartbeat timeout of 30 s, with a
    heartbeat every 10 s while it waits (S7), so a stopped engine is found within 30 s.
  - Temporal's own activity retry is off (`MaximumAttempts: 1`) for every activity; R5 retries.
  - Determinism (S5): the workflow reads time only through `workflow.Now`, randomness only
    through `workflow.SideEffect`, and does no I/O. Reuse keys and hashes are computed in
    activities or from activity results.
  - Changing workflow code while runs are in flight uses `workflow.GetVersion` (S5). CI replays
    recorded histories with `worker.NewWorkflowReplayer` so a change that breaks determinism
    fails.
- **Stops (FR-001 to FR-005)**:
  - An activity in flight when the engine stops ends with a heartbeat timeout. The workflow
    treats it as *interrupted, not failed*: it records `RunInterrupted` (detail `the platform
    stopped`), restores the project to the last accepted step (FR-002), and makes the same
    attempt again with the same number. An attempt counts as failed only when its checks fail
    (FR-003, owner).
  - When the engine is up again its Temporal worker takes the workflow up by itself (FR-005,
    owner) and records `RunResumed` (detail `continued after the platform stopped`).
  - An accepted step is never made again: the workflow's state holds every accepted step; a
    commit is repeat-safe by its stamp, as in feature 001.
  - When the step workers are gone (a reboot), the worker call fails `WorkersNotReady`. The
    workflow then waits for the signal `workers-ready`, which the engine sends when `ap
    build-workers` registers the run's workers (Edge Cases).
- **Budgets**: each activity returns how long it ran, and a step worker call or check that fails
  returns it too, with what the call used (feature 001's driver counted both; T057); the time budget sums these durations, so
  downtime and waits for workers or the developer never count (spec Assumptions). Rounds, tokens,
  cost and attempts are workflow state, so a stop grants nothing new (FR-003). Exception: an agent call cut off by its worker dying reports no usage (FR-003's limit, R10).
- **Waiting for the developer**: a hand-over or an interruption for another reason (the model
  unavailable on every attempt) waits for the signal `resume`, `accept-hand-over` or `cancel`,
  together with a durable 14-day timer from the hand-over (S7; FR-004). A `resume` after a
  hand-over starts the resumed step's attempts from 1 (owner).
- **Defects and a store that does not answer** (T058, owner 2026-11-20): a record write that fails
  is retried by the declared rule and then once a minute until written (back-pressure,
  Principle X; bounded by Temporal's history limit after about 6 days); a workflow that closes
  with no end on the record is ended by the API with `RunCancelled`, detail `the run's workflow
  failed: <reason>`, and the operation fails; the contract keeps its four outcomes and says so in
  its text ([data model](data-model.md), Run record).
- **Cancel**: `cancelRun` and cancelling the run's operation request the workflow's cancellation;
  the workflow records `RunCancelled` and ends.
- **Rejected**: one workflow per step (more histories, no gain); Temporal's activity retry
  (no jitter, R5); keeping `RunInterrupted` until the developer resumes (owner chose
  continuing by itself).

### R4. An operation is recorded before it is acknowledged, then runs as a workflow

- **Decision**:
  - A state-changing request first writes its operation record to the engine store, in one
    atomic write (as `run-sequence.json` is): `OP-<n>` from a durable `operation-sequence.json`,
    the kind, the target, the Idempotency-Key's hash with the request body's hash, and the time.
    The engine answers 202 only after that record (FR-008) and the workflow's start below (amended
    2026-11-20). A failure before that answers 503 `Unavailable`
    with `Retry-After`, a retryable error the [engine contract](../../src/engine/api/preview/2026-11-12-preview/openapi.yaml) gains.
  - The engine then starts the operation's workflow, ID = the operation ID, on the task queue
    `operations`, with `RejectDuplicate`. At start the engine starts the workflow of every
    recorded operation that has none (an outbox; constitution, Reliable events and side
    effects).
  - The operation's status, progress and outcome are read from Temporal (`DescribeWorkflowExecution`
    and a query). An ended operation can be followed while its record is kept: 24 h, the
    namespace's retention and `x-limits.idempotency-key-retention-hours` (one value), counted
    from the end for the record and from acceptance for the key (data model).
  - Idempotency keys survive a stop: the record holds the key's hash, so a resent request finds
    the same operation (FR-009); a different body under a kept key is still 409.
  - IDs never repeat across stops (FR-011): the sequence is durable before an ID is given.
  - A run's operation is the workflow that starts or resumes it; it ends when the run ends, as
    in feature 001.
  - **Amended 2026-11-20 (owner, found before T022; ADR 0072)**: the record alone cannot redo
    the work after a stop (`createSlice` needs its description, `replaceRules` its rules), and
    FR-016 keeps request bodies out of Temporal. So the request body is kept beside the record in
    the engine store (`operations/OP-<n>.body`), with the record's retention and erasure; the
    workflow's one activity, `DoOperation`, rebuilds the work from the record and the body, and
    every step of the work is safe to repeat. The end (status, result, error) is written to the
    record once, so Temporal holds no outcome text and ended operations are read without it. The
    body is removed when the end is written. The engine asks whether the durable service answers
    before it writes anything (503 when not); once the record is written (and a run operation's
    run started), the operation is accepted and the outbox, at start and every minute, starts a
    workflow that failed to start (analyze round 16).
    Answers under a key that created no operation stay in memory, as in feature 001, and a 503 is
    never kept. The [data model](data-model.md) holds the whole design.
    Rejected: an unfinished operation ends `failed` after a stop (no body kept): simpler, but
    US2's acceptance scenario 2 (retries continue after a stop) and the outbox would not hold for
    slice operations; the body inside Temporal's input: FR-016.
- **Why**: Temporal's ID policies protect a workflow ID only for the retention period (S8), and
  the contract's `OP-<n>` and its 24 h key window are client-visible facts (Principle XV), so the
  ID and the key live in the engine's store; the queue, retries, progress and cancellation are
  Temporal's.
- **Rejected**: workflow IDs as operation IDs without a record (an ID could repeat after
  retention and a key could be lost before the workflow started); finding keys through
  visibility queries (eventually consistent, S3).

### R5. One retry rule, declared in the engine contract

- **Decision**: the engine contract's `x-limits` declares the retry rule once:
  `retry-attempts: 3` (retries after the first try), `retry-first-wait-ms: 1000`,
  `retry-longest-wait-ms: 30000`, `retry-jitter-fraction: 0.5`. Retry `n` (from 0) waits
  `min(longest, first × 2^n)` plus a random part of up to `fraction` of it: 1 to 1.5 s, 2 to 3 s,
  4 to 6 s, the form the terminal contract already publishes for the client's resends.
  - A small Go module, `src/retry`, applies it (`Wait(n, random)`); the engine's workflow and
    the terminal client both use it. Neither holds the values: the engine reads them from its
    embedded contract's `x-limits`, as it reads its other limits (`src/engine/api/contract.go`),
    and the client from the engine contract embedded by its generated code (oapi-codegen's
    embedded spec, `src/cli/api`), so `x-limits` is the one place (SC-005; constitution III).
  - In the workflow the wait is a durable `workflow.Sleep` and the random part a
    `workflow.SideEffect` (S5), so a retry survives a stop. It covers the project worker's
    restore and commit, a step worker call that fails `WorkersNotReady` (R10), an operation's
    retryable failure (`EraseSlice`), and the client's resends; step attempts are not retries (they are the pattern's attempts).
  - The three hand-written forms (`backoff` in `api/operations.go`, `historyBackoff` in
    `machine/drive.go`, the client's wait in `cli/runs.go`) are removed, which closes feature
    001's Single-owner exception (SC-005).
- **Owner**, 2026-11-19.
- **Why**: Temporal documents no jitter for activity retries and its code adds none (S6), while
  Principle IV requires backoff with jitter (S15). The client's published form is kept, so the
  terminal contract does not change (Principle XV).
- **Changes existing tests**: `TestRetryBackoffGrowsExponentiallyWithJitter` (`api`) and
  `TestHistoryRetryWaitsAreExponentialWithJitter` (`machine`) pin the forms being removed; each
  change needs the owner's approval first (constitution, Existing tests change only with the
  owner's approval).
- **Rejected**: Temporal's RetryPolicy with a Principle IV exception for no jitter (owner chose
  the declared rule); full jitter everywhere (changes the published client behaviour).

### R6. Without Temporal, the engine still answers queries

- **Decision**: the engine starts after `durable` is healthy (Compose `depends_on`). While Temporal
  is unreachable, every state-changing request answers 503 `Unavailable` with `Retry-After`
  and nothing is accepted (US2 AC4; an operation record written for it is removed, R4), and the reads of slices, runs, events, artifacts and
  comparisons are served from the engine store as before. `registerWorker`, which signals the
  run waiting for its workers, is one of those state-changing requests (analyze round 3). `ap up` waits for the engine's
  readiness as today; the engine is ready once its Temporal worker is connected.
  - The 503's body is its own problem schema, `UnavailableProblem`, whose `code` is always
    `Unavailable`; the shared `ErrorCode` enum does not gain the value. Adding it there is a
    breaking change: the pinned `oasdiff` (v1.37.0) reports `response-property-enum-value-added`
    as an error on every response that carries a problem (137 errors), while a new 503 response
    with its own schema is `response-non-success-status-added`, info only (reproduced 2026-11-19,
    T001).
- **Why**: Principle X (acknowledge only what is durably recorded) and the published API's
  queries, which never needed Temporal.

### R7. Runs left open by feature 001

- **Decision**: at start the engine lists the runs whose record has no end event and checks each
  for a workflow. A run without one was left by feature 001: the engine refuses to start and
  names the runs, telling the developer to end them (resume to an end, or cancel) with feature
  001's version, then upgrade (spec Edge Cases). Ended runs need nothing (FR-015).
- **Why**: carrying a run over would mean rebuilding a workflow's state from a record the
  workflow did not write; the spec's assumption limits feature 001's runs to the owner's own
  machines.

### R8. Erasing a slice erases its workflows

- **Decision**: the `DeleteSlice` operation, after its refusal checks, deletes the workflow of
  each run of the slice and of each other operation that targets the slice
  (`DeleteWorkflowExecution`, S9), then erases the store as today, those operation records
  included. Its own record and workflow stay followable until the 24 h retention removes them;
  they hold only the slice's ID (FR-017, clarified 2026-11-19). A test reads PostgreSQL's `executions`, `history_node`, `current_executions` and
  `executions_visibility` tables for the deleted IDs and fails if any row is left (S14 could not
  settle it; S9 says the history is removed).
- **Why**: FR-017 and SC-006.

### R10. Step worker registrations outlive an engine restart

- **Found**: T014's agent-step stop point (CI run 44537905339): the stop pair was recorded, then
  the repeated `CallWorker` found the engine's in-memory registry empty (`api/workers.go`, ADR
  0036's Context) and the run waited with `the step workers are not running`; only the lab's
  re-registration let it end. On a real platform only a developer command registers again.
- **Decision** (owner, 2026-11-19): `registerWorker` writes each registration (slice, step,
  endpoint, the hash naming its grant file) to the engine store before it answers; the engine
  reads them at start; the call token is read from the grant file at each call, so the store
  holds no token and removing a grant still revokes it; erasing the slice removes its
  registrations (FR-017).
- **Rejected**: step containers registering themselves again (each would need a worker token
  inside it, a wider credential); a clarified FR-005 that needs `ap build-workers --resume` after
  every engine restart (narrows the feature's purpose).
- Record: [ADR 0071](../../docs/decisions/0071-step-worker-registrations-kept-in-the-engine-store.md).
- **Found next** (CI run 44553348919, T014 procedure step, 3 of 3): with the registration kept,
  the restarted engine's repeated call reached the step worker while it was itself restarting,
  was refused (`WorkersNotReady`), and the run waited for a `workers-ready` that nobody sends.
  **Decision** (owner, 2026-11-19): a `WorkersNotReady` call is retried by `src/retry` (3 retries,
  about 1, 2 and 4 s plus jitter, durable sleeps), as restore and commit are (T012 (j)); only then
  is the run interrupted and waits as before. **Rejected**: retrying for the whole wait (up to 14
  days, about 6 history events per try (a failed activity, a timer and the side effect), far over
  R9's 10,240), and a declared retry window (a new contract value for one case). Accepted limit:
  the retries span about 7 to 10.5 s; a worker unreachable for longer still makes the run wait
  for `ap build-workers --resume` (T014's procedure step judges the window in CI). Each stop or
  wait adds at most 3 retries (about 18 events), which T012 (n)'s bound at feature 001's limits
  still covers.
- **Found next 2** (CI run 44585526936, T014 procedure step 3 of 3 and review round 2 of 2;
  reproduced on the owner's clean VM 2026-11-19): the repeated call reached the step worker, and
  the worker's restart then closed the connection before it answered (`runProcedure generate:
  Post ...: EOF`). Only a refused dial counted as `WorkersNotReady`, so the call counted as a
  failed attempt: the procedure step made attempt 2 (FR-003 broken), and the review step's
  attempts 2 and 3 asked the model a message no recording holds, so the run was interrupted.
  **Fix** (defect against the spec's edge case "a worker that is briefly unreachable
  (restarting) is called again by the retry rule"): a worker call whose connection fails before
  the worker's answer is complete (refused, reset, or closed: `EOF`, unexpected `EOF`) is
  `WorkersNotReady`, retried as above with the same attempt number. An answer the worker gave
  (any HTTP status, a stream that ended in good order) is not. Accepted limit: what an agent step
  used before its worker died is not counted in the budgets (its stream never reported it).
- **Found next 3** (the owner's clean VM, 2026-11-19, T014 review round with T049): the review
  worker, stopped by its restart, ended its stream in good order but without `stream_stopped`
  (`the stream ended without stream_stopped`), which still counted as a failed attempt.
  **Decision** (owner, 2026-11-19): an agent stream that ends without `stream_stopped` is the
  worker gone too (`WorkersNotReady`); the agent's runtime ends every stream it finishes with
  `stream_stopped`, so a stream without it was not finished. **Rejected**: changing T014 to
  restart the paused worker before the engine calls it (a real restart that cuts a stream would
  still count as a failed attempt).
- **Found next 4** (the owner's clean VM, 2026-11-19, with T052): the repeated `create session`
  reached the review worker while the test still held it paused and failed `timeout awaiting
  response headers`. **Decision** (owner, 2026-11-19): one rule: a worker call that got no
  complete answer from the worker (any failure of the HTTP exchange itself, a header timeout
  included, or a stream ended without `stream_stopped`) is the worker gone, unless the caller
  gave up (the step's time limit or a cancel). **Rejected**: changing T014 to restart the paused
  worker before the engine starts (it removes the race in the test, not on a real platform).

### R9. Tests

- Workflow unit tests: `testsuite.WorkflowTestSuite` with activity mocks and time skipping
  (S5), in CI's `go` job. They cover attempts across a stop (a heartbeat timeout in the middle of
  an attempt), the 14-day timer, the waits for signals, and budgets that ignore waits.
- Replay tests: recorded workflow histories replayed with `worker.NewWorkflowReplayer` in the
  `go` job (S5). The histories (T020) were recorded on the owner's clean VM from `eb6e1c8`, trial 1 of
  the stop-point suite with a scratch-only export by the pinned admin tools, not from CI (owner,
  2026-11-20: recording in CI would change T014 and `check.yml` for a 40-minute run). They are
  never re-recorded to make a change pass: a change to the workflow's commands goes behind
  `workflow.GetVersion` and new histories are added beside them. One replay run is kept, since
  `replayRun` reuses every step through the same workflow code.
- Integration: the engine against the real `durable` and `durable-store` services in the
  `docker` job (the recorded model), and the stop points of SC-001 (the engine container stopped
  during an agent step, a procedure step, a review round and a commit, then started) in a new
  acceptance file in `src/tests/acceptance`, run by the `docker` job in the labs as feature 001's
  planted suite is. Each compares the artifacts with an uninterrupted run of the same slice and
  replays the continued run, 3 trials.
- History size: a run within feature 001's limits (the catalogue pattern's steps, 3 attempts, 3
  review rounds) is computed and tested to stay under Temporal's warning of 10,240 history events
  (S10); each event the run records is one activity of about three history events, and
  heartbeats add none.
- Feature 001's suites stay as they are (SC-003). Machine tests that drive the old driver are
  rewritten as workflow tests; each existing test file that changes needs the owner's approval
  first, named in the task that changes it.

## Principle VI evaluation

| Candidate | Licence | Scorecard | Health | Covers | Result |
|-----------|---------|-----------|--------|--------|--------|
| Temporal server 1.32.2 | MIT | 6.0 (2026-11-16) | releases monthly, patches 2026-10 and 2026-11; commits daily (S1, S12) | durable runs, timers, signals, queues, cancellation (S5, S7) | adopted (ADR 0056) |
| Temporal Go SDK 1.51.0 | MIT | 7.3 | release 2026-11-10 (S1) | workflow and activity code, test suite, replayer (S5) | adopted |
| PostgreSQL 16 | PostgreSQL (OSI) | 6.1 | supported to 2028-11 (S13) | Temporal's persistence and visibility (S3) | adopted |
| SQLite (Temporal dev server) | public domain | n/a | | dev server only (S3) | rejected (R1) |

What Docker Agent already provides: its agent runs (`serve api`), used for agent steps as in
feature 001; it keeps no durable run state across a platform stop, so nothing here replaces it.

Custom code kept, with its justification: the run record (R2: a published contract and
lasting history, which Temporal's retained history is not); the operation record and its
sequence (R4: client-visible IDs and keys outside Temporal's retention); the retry rule's
`src/retry` (R5: Temporal provides no jitter). Everything Temporal covers (resume, checkpoints,
timers, the queue, cancellation) is removed from the engine.

Temporal's images carry no signature or provenance the platform can verify (Scorecard
Signed-Releases 0, S12); like the gateway's nginx, the registry and cosign, which `ap` runs from `images.lock` without building them, they are pinned by digest
(Principle XIII), and the digest is the check.
