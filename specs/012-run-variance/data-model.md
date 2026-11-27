# Data model: A developer sees how much the builds of a slice vary

Entities of the [spec](spec.md), as [research](research.md) R1 to R7 decide them. Field names are
those of the engine contract draft ([contracts/trial-sets.md](contracts/trial-sets.md)).

## Trial set (stored)

Kept in the engine store in its slice's folder; erased with the slice (FR-011).

| Field | Rule |
|-------|------|
| `id` | `TS-<n>`, from a sequence that never gives an ID again |
| `slice` | The approved slice it builds |
| `trials` | k, 1 to `x-limits.trials-max` (5); `x-limits.trials-default` (3) when not given |
| `project_prefix` | A project name of at most 61 characters; trial *i*'s project is `<prefix>-<i>` |
| `method` | The engine's method identity when the set was accepted |
| `part_hashes` | The slice's approved part hashes when the set was accepted; before each trial they are compared with the slice's (F1) |
| `budget` | `cost_usd` = `x-limits.trial-set-cost-usd` (2), `hours` = `x-limits.trial-set-hours` (24) |
| `operation` | Its `run-trials` operation |
| `state` | below |
| `stopped_because` | With `stopped`: `budget`, `time`, `slice-changed` or `workers` |
| `runs` | The trials' run IDs in order; trial *i*'s ID is written before its run is started (R1) |
| `failed_because` | With `failed` only: the detail of the operation's problem |
| `created_at`, `ended_at` | `ended_at` with every end state |

### States

| State | Meaning | Next |
|-------|---------|------|
| `running` | A trial runs, or the set waits for another run to end or an interrupted trial | `ended`, `cancelled`, `stopped`, `failed` |
| `ended` | All k trials ended | — |
| `cancelled` | Its operation was cancelled: the running trial was cancelled, none started after | — |
| `failed` | Its operation failed (a trial could not be started, or the job's retries ran out): the running trial was cancelled, none started after; `failed_because` holds the operation's problem detail (T030, ADR 0106) | — |
| `stopped` | No further trial could start: the next would pass the cost budget (`budget`), 24 hours passed and the running trial was cancelled (`time`), a part of the slice is no longer approved or differs from `part_hashes` (`slice-changed`), or the next project's workers are gone (`workers`) | — |

The set's operation succeeds on `ended` and `stopped`, ends `cancelled` on `cancelled` and
`failed` on `failed`. When the operation ends failed (US1 review B1, ADR 0106):

1. The operation's end is recorded first (the generic Operation workflow); the engine then ends the
   set `failed` with `failed_because` = the operation's problem `detail`, under the trial-set lock,
   once: a set already `ended`, `stopped`, `cancelled` or `failed` is left as it is.
2. Repair: `startTrialSet`, `deleteSlice` and the engine's start find a `running` set whose
   operation ended failed and end it the same way, so a crash between the two writes never leaves
   a set `running` (and never blocks the slice). A read (`getTrialSet`, `listTrialSets`) writes
   nothing (FR-007, SC-005): it shows such a set as `failed`, with `failed_because` and `ended_at`
   taken from its failed operation (owner 2026-11-28, T036).
3. The set's last trial run, if it has not ended, is cancelled at that end and again at each
   repair (start, delete, engine start) until its record shows an end; the set is `failed` whether
   or not the cancel has landed, and the result counts that trial as not ended until its record
   ends.
4. Which start refusals end the set how: before each trial the job stops the set `slice-changed`
   (NotApproved, PatternDoesNotFit, the captured-pattern refusals) or `workers` (WorkersNotReady);
   any other refusal of the trial's start (ProjectExists, a project made non-empty after the set was
   accepted) fails the operation, so the set ends `failed`.

### Write order and repeats

1. `startTrialSet` checks every refusal, reserves `TS-<n>`, then records the set's operation
   (target `TS-<n>`, with the request body and, beside it, the acceptance-time `method` and
   `part_hashes`), then the set (`running`). When
   the operation's write fails nothing is kept and the answer is 503, retryable. When the set's
   write fails after it, the operation stands: its job, finding no set record for its target,
   writes it from what the operation holds (body, `method`, `part_hashes`) before its first trial, so a set never exists without its
   operation and an operation never runs without its set; "one running set per slice" counts the
   slice's open `run-trials` operations as well as `running` records, so no orphan blocks it.
2. Before trial *i* the job reserves a run ID from the run sequence and writes it into `runs`, then
   starts the run with that ID (`RejectDuplicate`) and records the trial's `run` operation only
   once. A repeated job reads `runs`: a recorded ID with a workflow is followed; a recorded ID with
   neither a workflow nor a record is started again with the same ID; never a second run.
3. While following a trial, the job reads the run on every change of its record: `waiting` makes it
   send the hand-over signal; `RunNotWaiting` (the run moved on meanwhile) is read again, never a
   failure of the set.
4. `deleteSlice` is refused `RunActive` while one of the slice's sets is `running` or a
   `run-trials` operation of the slice is open.

## Slice trial (a run)

An ordinary run (feature 001) with, on `RunStarted` and `Run`:

| Field | Rule |
|-------|------|
| `trial_set` | The set's ID |
| `trial` | Its position, 1 to k |

It is started with `NoReuse` (R2). When it waits for the developer, the set accepts its hand-over
(R4), so it ends `handed-over` with the reason naming the set.

## Worker registration (changed)

| Field | Rule |
|-------|------|
| `project` | Optional on `WorkerRegistration`, returned on `Worker`: the project the worker's container serves. The engine keeps one registration per slice, step and project; one without a project serves any project of its slice (R3) |

A run uses only its project's registrations, else one without a project; `WorkersNotReady` names
the step and project missing.

## Trial result (derived, never stored)

Computed from the trials' run records on each read (R6; FR-007).

| Field | Rule |
|-------|------|
| `ended` | Trials that ended |
| `done` | Of those, trials that ended `done` |
| `unstable` | `done` ≥ 1 and `done` < `ended` |
| `outcomes` | Per trial: run ID, outcome, method, totals (below) |
| `steps` | Per step of the pattern, in order: `reached` (trials whose record has the step), each measure as `{lowest, median, highest}` or absent when not available, `files` (`same`, or groups of trials by output hashes) |
| `totals` | `{lowest, median, highest}` of each trial's totals |
| `methods_mixed` | True when the trials' runs ran under more than one method |
| `previous` | The latest earlier `ended` or `stopped` set of the slice with `ended` ≥ 1: its ID, method, `done`/`ended`, the change in share, `less_reliable` when the share fell, `methods_differ`; absent when there is none |

Measures: `attempts`, `review_rounds`, `seconds`, `tokens`, `cost_usd`. Seconds, for a step and for a
trial alike, leave out time the run was interrupted (`RunInterrupted` to `RunResumed`). The median of an even count is
the mean of the two middle values (feature 008).

## Validation

- `trials` outside 1 to 5, or a `project_prefix` that does not make valid project names: 400
  `InvalidRequest`, nothing recorded.
- The start is refused at once, in `startRun`'s order (NotFound, PatternDoesNotFit, NotApproved,
  the captured-pattern refusals, WorkersNotReady for any of the k projects, ProjectExists for any
  of them); another active run does not refuse it (the set waits).
- One set of a slice at a time: a second while one is `running` is 409 `RunActive`.
