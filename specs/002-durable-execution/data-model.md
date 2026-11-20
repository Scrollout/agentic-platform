# Data model: Runs and operations survive a platform stop

The run record, slices and artifacts keep feature 001's model
([data model](../001-prose-to-accepted-slice/data-model.md),
[ADR 0059](../../docs/decisions/0059-run-store-is-json-lines-and-a-content-addressed-store.md)).
This feature adds the operation record (with its request body) and Temporal's execution state, and changes when events
are written. Research: [R2 to R5](research.md).

## Run record (unchanged schema)

`run-event.schema.json` (engine contract `2026-11-12-preview`) does not change. What changes is
who writes the events and when:

- Every event is appended by the run's workflow through the activity `RecordEvent(slice, run,
  seq, event)`. The store accepts each `seq` once: the same content again succeeds without
  writing; other content under a taken `seq` is refused (`SeqTaken`), which the workflow treats as
  a defect and fails the run's operation.
- A write of any event that fails for another reason (the engine store unavailable) is
  back-pressure, never a defect: the workflow retries it by the declared retry rule, and then
  once a minute (`recordRetryEvery`, an internal value no client sees) until it is written (T058,
  owner 2026-11-20). Each try adds about 6 history events, so an outage of about 6 days reaches
  Temporal's history limit (51,200 events, S10), which ends the workflow; the next rule then ends
  the run.
- A run whose workflow closes with no end on the record (a defect: `SeqTaken`, `InvalidRun`, or
  Temporal ending the workflow) is ended by the engine's API, the record's only other writer, in
  one place (`runs.closeIfFailed`), called by the run's operation on each poll and by `startRun`,
  `resumeRun` and `replayRun` before they answer `RunActive` (so a restart needs no sweep). It asks the durable service first
  and only then reads the record again: when the workflow has closed and the record still has no
  end, it appends `RunCancelled` as the next `seq` with `detail` `the run's workflow failed:
  <reason>` (the failure's message, or `it closed with no end`), and the run's operation fails
  with code `OperationFailed` and that detail. A write that finds the `seq` taken reads the record
  again and stops if it has an end. A workflow that closed before it recorded `RunStarted` (for
  example `InvalidRun`) leaves no record: the run is ended as never started, nothing is written,
  it no longer counts as active, `getRun` answers `NotFound` as for any run with no record, and
  its operation fails with that detail (T060; owner, 2026-11-20, no contract change). The contract keeps its four outcomes; its `RunOutcome` and
  `Operation` text say that `cancelled` also means this (T058, owner 2026-11-20). Limit: an
  engine that is down from that moment until the workflow's retention (24 h) has passed finds a
  run with no end and no workflow, and its start check refuses it as it refuses feature 001's
  leftovers (R7).
- A stop in the middle of a run adds two events, in this order, at the point where the run
  was: `RunInterrupted` with `detail` `the platform stopped`, then `RunResumed` with `detail`
  `continued after the platform stopped`. A developer's resume keeps feature 001's `RunResumed`
  without that detail.
- An attempt the stop interrupted is made again with the same `attempt` number. If its
  `AttemptMade` was already recorded, the record holds it twice, before and after the
  `RunInterrupted` and `RunResumed` pair; inspect and compare take the later one. Only an attempt
  whose checks fail counts toward the step's limit (FR-003).
- A developer's `resume` after a hand-over starts the resumed step's attempts at 1.

## Operation record (new)

One file per operation in the engine store, `operations/OP-<n>.json`, written atomically (temp
file, fsync, rename, fsync the folder), with the request body beside it in
`operations/OP-<n>.body` (ADR 0072, owner 2026-11-20):

| Field | Meaning | Rule |
|-------|---------|------|
| `id` | `OP-<n>` | From `operation-sequence.json`, advanced durably before use; never reissued, also after its record is removed (FR-011) |
| `kind` | The contract operation that accepted it: its `operationId` (`createSlice`, `startRun`, ...) | One of the contract's operations that answer with a new operation; the `kind` clients see (`propose-slice`, `run`, ...) follows from it |
| `target` | The slice or run it acts on | As the operation's response names it; a run's ID is given out before the record is written |
| `key` | SHA-256 of the caller's token identity and the Idempotency-Key | Absent when no key was sent |
| `body` | SHA-256 of the request's method, path and body, as feature 001's key check | A kept key whose record has another `body` answers 409 `IdempotencyKeyReused` |
| `accepted_at` | RFC 3339 time | The `created_at` clients see |
| `ended_at` | RFC 3339 time the operation ended | Absent while it has not ended; written once |
| `status`, `result`, `error` | The end: `succeeded` with its result, `failed` with its problem (code, detail, failures), or `cancelled` | Written with `ended_at`, once; the first end wins |

- **Kinds**: `createSlice` → `propose-slice`, `deleteSlice` → `delete-slice`, `replaceGoals` →
  `replace-goals`, `replaceRules` → `replace-rules`, `replaceContract` → `replace-contract`,
  `answerQuestions` → `answer-questions`, `approvePart` → `approve-part`; `startRun`,
  `replayRun`, `resumeRun`, `acceptHandOver` → `run`. A slice operation's `target` is its slice;
  a run operation's is its run, whose slice is on the run's record (erasure reaches it so, T029).
- **The body** (`OP-<n>.body`) holds the request's exact bytes (empty when it had none). It is
  written before the record, so a record always has its body, and it is removed when the end is
  written: it is needed only by the work. It is runtime-plane content kept only in the engine
  store, never in Temporal (FR-016). A body with no record (a stop between the two writes) is
  removed at start.
- **Accepting** (FR-008): the engine first asks whether the durable service answers
  (`Operations.Ready`); when it does not, the answer is 503 `UnavailableProblem` and nothing is
  written (R6). Then it writes the body and the record (a failed write: 503, and what was written
  is removed), for a run operation starts or signals the run (ADR 0067; a failure: 503 and the
  record is removed), then starts the operation's workflow (task queue `operations`, ID = `id`),
  and answers 202. Once the record is written and the run, if any, started, the operation is
  accepted: a workflow start that fails then is no 503; the outbox starts it. Limit: when a run's
  start fails and its record cannot be removed either, the record stays and its operation ends
  `failed` with `the run was not started; send the request again`; a resend with the same
  Idempotency-Key finds that operation.
- **One record per key**: concurrent first requests under one key are served one at a time, as
  in feature 001 (the engine's in-memory key answers); only the first writes a record.
- **The work** runs in the workflow's one activity, `DoOperation(id)`, which reads the record
  and its body and does what the request asked, as feature 001's operation did. A repeat after a
  stop (heartbeat lost) is the same work again and does not count as a retry. A repeat finds work
  already done and ends `succeeded`:

  | Kind | Already done when | Then |
  |------|-------------------|------|
  | `createSlice` | a slice with the record's `target` exists | succeeded, `slice` = target |
  | `deleteSlice` | the slice no longer exists | succeeded, `slice` = target |
  | `replaceGoals`, `replaceRules`, `replaceContract` | the part holds the body's content | succeeded (applying it again changes nothing) |
  | `answerQuestions` | every answered question holds the body's answer | succeeded |
  | `approvePart` | the part is approved at the body's hash | succeeded |
  | run kinds | the run's workflow exists | the work only follows the run |

  The slice ID of an unended `createSlice` record is reserved again at start (`slices.Service`),
  so no other slice gets it and `createSlice` can create it after a restart. An operation whose
  record is gone when its work runs (its slice erased) ends nothing and its workflow completes.
- **Run operations**: the work follows the run until its record shows its end, as in feature
  001. `resumeRun` and `acceptHandOver` signal the run and answer with the run's open operation,
  found by its record (any run-kind record whose `target` is the run and that has no `ended_at`);
  only when none is open is a new one recorded. Their own Idempotency-Key is then not in that
  record: their answer under it is kept in memory (Keys). A run operation whose run was never
  started ends `failed` with the detail `the run was not started; send the request again`.
- **Cancelling** (`cancelOperation`): an operation whose record has ended answers 409
  `OperationEnded`. A run operation cancels its run first (`Runs.Cancel`; `NotFound` means the
  run has ended, and is no error), then writes the end `cancelled` to the record, then cancels
  the operation's workflow; an unreachable durable service answers 503 and nothing is cancelled.
  So a cancelled run operation ends `cancelled`, never `failed: the run was cancelled`. A
  cancelled workflow of any kind writes `cancelled` (first end wins). Limit: a cancel
  that races the work's end may answer 202 while the operation ends `succeeded` or `failed`:
  the first end written wins, as in feature 001.
- **Ending**: the activity writes the end when the work ends (succeeded, a `WorkError`, or
  another error that is not retryable). A retryable error (`Retryable`) is retried by the
  workflow by the retry rule (durable sleeps, counted across a stop, FR-010); when the retries
  are used up the workflow's last activity, `EndOperation`, writes `failed` with `failed after
  <n> attempts: <reason>`. Every end is written once; a later one changes nothing.
- **Status** clients see: from the record once it has ended; before that `running` while its
  workflow is open (or Temporal cannot be asked) and `accepted` while it has none yet. Progress of
  a run operation comes from its run's record (`done`, `total`), as in feature 001; other kinds
  have none. Queries never need Temporal for an ended operation.
- **Outbox and sweep**, at start and then every minute (an internal value no client sees): a
  record that has not ended and has no workflow gets one (`RejectDuplicate`), so an operation
  recorded just before a stop still runs (FR-009); a record that has not ended whose workflow has
  closed (its end never written; the sweep asks Temporal first and reads the record after, as
  `runs.closeIfFailed` does, so an end written meanwhile is kept) ends `failed` with `the operation's workflow closed with no
  end`; a record ended more than 24 h ago is removed.
- **Kept**: 24 h after `ended_at` (`x-limits.idempotency-key-retention-hours`, also the
  namespace's retention). Every read checks `ended_at` itself: after that, `getOperation` answers 404
  `NotFound`, whenever the sweep runs (a key is free earlier, see Keys). A
  slice's erasure removes its operations' records at once (FR-017, T029).
- **Keys**: a request whose Idempotency-Key a record holds is answered with the record's first
  answer (202, the operation as accepted), also after a restart, for 24 h from `accepted_at`
  (`x-limits.idempotency-key-retention-hours`: "from the first request"; ADR 0073); after that the key is
  free, though the operation is still followed until 24 h after its end. Every other answer under a key
  (a refusal, `registerWorker`'s 200, `cancelOperation`'s 202, a `resumeRun` or
  `acceptHandOver` answered with an open operation) is kept in the engine's memory as in feature
  001 and is judged again after a restart; a 503 is never kept, so the same request may be sent
  again with the same key (engine contract, info).
- **Without a durable service** (`Config.Operations` nil, the API tests of feature 001): the
  same records (in memory when `Config.Store` keeps none) and the same `DoOperation` run in the
  engine's process with the same retry rule, and do not survive a stop. The engine always sets
  both.

## Step worker registrations (new, ADR 0071)

One file in the engine store, `workers.json`, written atomically (temp file and rename, mode as
the store's others): for each slice and step, the `endpoint` the worker registered and `grant`,
the lowercase hex SHA-256 naming its grant file (ADR 0034). No token. `registerWorker` writes it
before it answers (a failed write answers 503 `UnavailableProblem` and keeps nothing; when the
write succeeds but the `workers-ready` signal fails, the file is put back to the earlier entry, or
none, before the 503, so "nothing changed" holds); registering again replaces the entry; erasing
the slice removes its entries in the same step as the rest of the slice, and an entry of a slice
that no longer exists is never used (the call path answers 404 for it). A registration made with
a token given at engine start (`Config.WorkerGrants`, no grant file; tests only) is not kept: a
stated limit. At start the engine reads it; a
call reads the call token from the grant file, and an entry whose grant file is gone counts as
not registered (`WorkersNotReady`).

## Execution state in Temporal (new, not a record)

Namespace `ap`, retention 24 h (`x-limits.idempotency-key-retention-hours`), registered by the engine at start (ADR 0070). Task queues `runs` and `operations` (one worker pool per
workload class, Principle IV).

| Workflow | ID | Holds | Signals | Queries |
|----------|----|-------|---------|---------|
| Run | `RUN-<n>` | position, the accepted steps and their results by hash, attempts of the current step, round, open findings by hash, changed lines, used tokens and cost, running time, the hand-over's time | `workers-ready`, `resume`, `accept-hand-over`, `cancel` | none: a run's status and outcome are read from its record, as in feature 001 |
| Operation | `OP-<n>` | the retries made of its activity and their waits; never its body or outcome, which are the record's | none: cancelling cancels the workflow | none: status and outcome are read from the record, progress from the run's record |

- Inputs and results are IDs, hashes, numbers, check names and short reasons; never prompt,
  completion, file content or finding text (FR-016). Each payload stays far below 2 MB.
- A run's workflow runs while the run is running, interrupted for another reason, or waiting for
  the developer; it ends with the run's outcome.

## The engine's durable interface (sketch for the test authors)

Package `src/engine/durable` holds the workflows and activities; the two ports `Runs` and `Operations`, and `Operator`, are the API's, in `src/engine/api` (`durable` reads the retry rule from `api`, so `api` must not import `durable`; found in T016). Names may change in the code only together with this sketch.

| Name | Kind | Input | Result |
|------|------|-------|--------|
| `Run` | workflow, queue `runs`, ID `RUN-<n>` | run ID, slice, project, pattern, inputs' hashes, `replay_of` | outcome |
| `CallWorker` | activity | run, step, attempt, round, the step's inputs by hash | artifacts by hash, usage, running time; a call that fails is a result too: the field `Failure` holds the reason, with the model, usage and running time it reports, and the attempt fails as before (T057); `WorkersNotReady` and `ModelUnavailable` stay errors that add nothing, so a call cut off by its worker dying counts no usage (FR-003's limit, T050) |
| `RunCheck` | activity | run, step, attempt, check name, artifacts by hash | passed, reason, findings by hash, running time; a check that could not run is a result too: `Failure` holds the reason, with its running time; it is a failed check, as before (T057) |
| `RestoreProject`, `CommitStep` | activity | project, run, step, round, attempt, the stamp's fields | done, running time |
| `LookupReuse`, `PutResult` | activity | reuse key | the reused result by hash, or none |
| `RecordEvent` | activity | slice, run, seq, event | done, or `SeqTaken` |
| `Runs` (the API's port) | Go interface | `Start(run)`, `Signal(run, name)`, `Cancel(run)`, `Exists(run)`, `Closed(run)` (whether the workflow has closed, and its failure's reason; T058), `Delete(id)` | errors `Unavailable`, `NotFound` |
| `Operation` | workflow, queue `operations`, ID `OP-<n>` | operation ID, kind, target | none: the end is written to the record |
| `DoOperation`, `EndOperation` | activity | operation ID; `EndOperation` also the status and detail | done, or a retryable failure (`DoOperation`) |
| `Operations` (the API's port) | Go interface | `Ready()` (whether the durable service answers), `Start(op)` (RejectDuplicate: an existing workflow, open or closed, is no error), `Open(op)` (whether its workflow exists and has not closed), `Cancel(op)`, `Delete(op)` | errors `Unavailable`, `NotFound` |
| `Operator` (the API's, for the durable worker) | Go interface | `Do(id)` (the work of `DoOperation`), `End(id, status, detail)` (`EndOperation`) | nil, a `Retryable` error, or ctx's error |

Signals: `workers-ready`, `resume`, `accept-hand-over`, `cancel`. The API tests fake `Runs` and
`Operations` (a fake `Operations` calls the handler's `Operator` as the workflow would); the
workflow tests mock the activities. The engine gets the handler and its `Operator` together
(`api.NewEngine(cfg)`; `NewHandler(cfg)` stays, for callers that need only the handler).

## Run states (unchanged names)

```text
running ──platform stops──▶ running (RunInterrupted, RunResumed; same attempt again)
running ──engine restarts, step workers still running──▶ running (RunInterrupted, RunResumed; registrations kept, ADR 0071)
running ──a worker call fails WorkersNotReady──▶ the same call again by the retry rule (durable sleeps; nothing recorded)
running ──every retry fails WorkersNotReady (the workers are gone, a reboot)──▶ interrupted (RunInterrupted `the step workers are not running`)
interrupted (workers) ──ap build-workers --resume: workers-ready──▶ running (RunResumed; no `ap resume` needed)
running ──every attempt: model unavailable──▶ interrupted ──resume──▶ running
(the time budget runs only while running)
running ──budget, attempts or rounds used up──▶ waiting-for-developer
waiting-for-developer ──resume──▶ running (the resumed step's attempts start at 1)
waiting-for-developer ──accept-hand-over──▶ ended (handed over)
waiting-for-developer or interrupted ──14 days after the hand-over──▶ ended (expired)
any not ended ──cancel──▶ ended (cancelled)
running ──last step accepted and committed──▶ ended (done)
```

## Retry rule (new, declared in the engine contract)

`x-limits`: `retry-attempts` (3), `retry-first-wait-ms` (1000), `retry-longest-wait-ms` (30000),
`retry-jitter-fraction` (0.5). Retry `n` (from 0) waits `w = min(longest, first × 2^n)` plus a
uniform random part in `[0, fraction × w]` (research R5).
