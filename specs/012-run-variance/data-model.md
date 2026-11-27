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
| `created_at`, `ended_at` | `ended_at` with every end state |

### States

| State | Meaning | Next |
|-------|---------|------|
| `running` | A trial runs, or the set waits for another run to end or an interrupted trial | `ended`, `cancelled`, `stopped` |
| `ended` | All k trials ended | — |
| `cancelled` | Its operation was cancelled: the running trial was cancelled, none started after | — |
| `stopped` | No further trial could start: the next would pass the cost budget (`budget`), 24 hours passed and the running trial was cancelled (`time`), a part of the slice is no longer approved or differs from `part_hashes` (`slice-changed`), or the next project's workers are gone (`workers`) | — |

The set's operation succeeds on `ended` and `stopped` and ends `cancelled` on `cancelled`.

### Write order and repeats

1. `startTrialSet` checks every refusal, then records the set (`running`) and its operation in one
   store write; when that write fails nothing is kept and the answer is 503, retryable. A set is
   never recorded without its operation, so no orphan `running` set blocks the slice.
2. Before trial *i* the job reserves a run ID from the run sequence and writes it into `runs`, then
   starts the run with that ID (`RejectDuplicate`) and records the trial's `run` operation only
   once. A repeated job reads `runs`: a recorded ID with a workflow is followed; a recorded ID with
   neither a workflow nor a record is started again with the same ID; never a second run.
3. While following a trial, the job reads the run on every change of its record: `waiting` makes it
   send the hand-over signal; `RunNotWaiting` (the run moved on meanwhile) is read again, never a
   failure of the set.
4. `deleteSlice` is refused `RunActive` while one of the slice's sets is `running`.

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
