# Contract draft: trial sets

A draft for the first consumer (the terminal client). The authoritative copies are the engine
contract (`src/engine/api/preview/2026-11-12-preview/openapi.yaml` with `run-event.schema.json` and
`slice-defs.schema.json`) and the terminal contract (`src/cli/contract.md`), extended before any
code (Contract-first). Where this draft and they differ, they hold. Fields are in the
[data model](../data-model.md).

## Engine API

Build a slice several times, reusing nothing, and see how much the builds vary.

| Call | What it does | Answers |
|------|--------------|---------|
| `POST /slices/{sliceId}/trial-sets` `{project_prefix, trials?}` | Accepts a set of `trials` (1 to 5, default 3) fresh builds of the slice into `<prefix>-1` … `<prefix>-<k>`, run one after another | 202 operation of kind `run-trials`, its target the set; 400 `InvalidRequest`; at once 404 `NotFound`, 409 `PatternDoesNotFit`, `NotApproved`, the captured-pattern refusals of `startRun`, `WorkersNotReady` (naming the project and step), `ProjectExists` (naming the project), `RunActive` (another set of the slice is running) |
| `GET /slices/{sliceId}/trial-sets` | The slice's sets, newest first, each with its state, k, method, `done`/`ended` and `unstable` | 200, at most 50 per page, opaque page tokens; 404 for an unknown slice |
| `GET /trial-sets/{trialSetId}` | One set with its trials and its result (data model, Trial result) | 200; 404 for an unknown or erased set |

Cancelling is `cancelOperation` on the set's operation: the running trial ends `cancelled` and no
other starts. Every state change takes an `Idempotency-Key` (held 24 h); a reused key with another
body answers 409 `IdempotencyKeyReused`. Reading a set calls no model and changes no run.

`x-limits`: `trials-max: 5`, `trials-default: 3`, `trial-set-cost-usd: 2`, `trial-set-hours: 24`.

Changed: `WorkerRegistration` and `Worker` gain the optional `project`; `RunStarted` and `Run`
gain `trial_set` and `trial`; `OperationKind` gains `run-trials` (plan, Complexity Tracking).

## Terminal client

| Command | What it does | Calls |
|---------|--------------|-------|
| `trials <slice> <prefix> [--trials <k>]` | The launcher makes the k projects and their step workers (`build-workers` for each), then the client starts the set and follows it: one line per trial as it ends, then the result | `startTrialSet`, `getOperation`, `getTrialSet` |
| `trial-sets <slice>` | Lists the slice's sets | `listTrialSets` |
| `trial-set <id>` | Shows a set's result; follows it while it runs | `getTrialSet`, `getOperation` |

`trial-set` prints `done <d>/<e>` and `unstable` when marked, one row per trial (`RUN`, `OUTCOME`,
`ATTEMPTS`, `ROUNDS`, `SECONDS`, `TOKENS`, `COST`), one row per step (`STEP`, `REACHED`, each measure
as `lowest/median/highest` or `-`, `FILES` `same` or the groups), and the comparison line naming
the previous set. Exit 0 for a set that ended with every trial done, 7 for one that ended or
stopped with any trial not done (as `compare`'s "differ"), the run exit codes otherwise.
