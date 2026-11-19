# Data model: Runs and operations survive a platform stop

The run record, slices and artifacts keep feature 001's model
([data model](../001-prose-to-accepted-slice/data-model.md),
[ADR 0059](../../docs/decisions/0059-run-store-is-json-lines-and-a-content-addressed-store.md)).
This feature adds the operation record and Temporal's execution state, and changes when events
are written. Research: [R2 to R5](research.md).

## Run record (unchanged schema)

`run-event.schema.json` (engine contract `2026-11-12-preview`) does not change. What changes is
who writes the events and when:

- Every event is appended by the run's workflow through the activity `RecordEvent(slice, run,
  seq, event)`. The store accepts each `seq` once: the same content again succeeds without
  writing; other content under a taken `seq` is refused (`SeqTaken`), which the workflow treats as
  a defect and fails the run's operation.
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
file, fsync, rename, fsync the folder) before the engine answers 202:

| Field | Meaning | Rule |
|-------|---------|------|
| `id` | `OP-<n>` | From `operation-sequence.json`, advanced durably before use; never reissued (FR-011) |
| `kind` | The API operation's name (`x-operation-name`) | One of the contract's state-changing operations |
| `target` | The slice, run or project it acts on | As the operation's response names it today |
| `key` | SHA-256 of the caller's token identity and the Idempotency-Key | Absent when no key was sent |
| `body` | SHA-256 of the request body | A kept key with another body answers 409, as today |
| `accepted_at` | RFC 3339 time | |

- **Status** (`accepted`, `running`, `succeeded`, `failed`, `cancelled`), progress and error are
  read from the operation's workflow (ID = `id`), not stored here.
- **Kept**: 24 h after the operation ends, the namespace's retention and
  `x-limits.idempotency-key-retention-hours`; then the record and its workflow are removed.
  A slice's erasure removes its operations' records at once (FR-017).
- **Outbox**: at start the engine starts the workflow of every record that has none
  (`RejectDuplicate`), so an operation recorded just before a stop still runs (FR-009).

## Execution state in Temporal (new, not a record)

Namespace `ap`, retention 24 h. Task queues `runs` and `operations` (one worker pool per
workload class, Principle IV).

| Workflow | ID | Holds | Signals | Queries |
|----------|----|-------|---------|---------|
| Run | `RUN-<n>` | position, the accepted steps and their results by hash, attempts of the current step, round, open findings by hash, changed lines, used tokens and cost, running time, the hand-over's time | `workers-ready`, `resume`, `accept-hand-over`, `cancel` | none: a run's status and outcome are read from its record, as in feature 001 |
| Operation | `OP-<n>` | the operation's progress and outcome | `cancel` | `progress` |

- Inputs and results are IDs, hashes, numbers, check names and short reasons; never prompt,
  completion, file content or finding text (FR-016). Each payload stays far below 2 MB.
- A run's workflow runs while the run is running, interrupted for another reason, or waiting for
  the developer; it ends with the run's outcome.

## The engine's durable interface (sketch for the test authors)

Package `src/engine/durable`; names may change in the code only together with this sketch.

| Name | Kind | Input | Result |
|------|------|-------|--------|
| `Run` | workflow, queue `runs`, ID `RUN-<n>` | run ID, slice, project, pattern, inputs' hashes, `replay_of` | outcome |
| `Operation` | workflow, queue `operations`, ID `OP-<n>` | operation ID, kind, target | succeeded, or the error's code and reason |
| `CallWorker` | activity | run, step, attempt, round, the step's inputs by hash | artifacts by hash, usage, running time, or `WorkersNotReady`, `ModelUnavailable` |
| `RunCheck` | activity | run, step, attempt, check name, artifacts by hash | passed, reason, findings by hash, running time |
| `RestoreProject`, `CommitStep` | activity | project, run, step, round, attempt, the stamp's fields | done, running time |
| `LookupReuse`, `PutResult` | activity | reuse key | the reused result by hash, or none |
| `RecordEvent` | activity | slice, run, seq, event | done, or `SeqTaken` |
| `Runs` (the API's port) | Go interface | `Start(run)`, `Signal(run, name)`, `Cancel(run)`, `Exists(run)`, `Delete(id)` | errors `Unavailable`, `NotFound` |
| `Operations` (the API's port) | Go interface | `Start(op)`, `Describe(op)`, `Cancel(op)`, `Delete(op)` | status, progress, error; `Unavailable`, `NotFound` |

Signals: `workers-ready`, `resume`, `accept-hand-over`, `cancel`. The API tests fake `Runs` and
`Operations`; the workflow tests mock the activities.

## Run states (unchanged names)

```text
running ──platform stops──▶ running (RunInterrupted, RunResumed; same attempt again)
running ──step workers gone (a reboot)──▶ interrupted (RunInterrupted `the step workers are not running`)
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
