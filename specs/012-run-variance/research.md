# Research: A developer sees how much the builds of a slice vary

Phase 0 of the [plan](plan.md) for the [spec](spec.md). Each decision names its deciding source:
the code on branch `012-run-variance` at `b30c7ef` (the same as `main` `aa3858a` outside `specs/`
and `docs/`), or a cited document. Decisions marked *owner* were put to the project owner as direct
questions on 2026-11-27 (spec, Clarifications).

## Sources

| ID | Source |
|----|--------|
| S1 | `src/engine/durable/workflow.go:339`: a step reuses a kept result unless the run's code version has `fill` steps and `RunInput.NoReuse` (workflow version 2, feature 011); `api/proof_slice.go:186` sets `NoReuse` for a proof. Any run started under version 2 can reuse nothing |
| S2 | `src/engine/machine/reuse.go:110`: "the first result under a key stays", so a run that reuses nothing never replaces a kept result; a trial's result under a key already kept is not kept again (its files stay on its events) |
| S3 | `src/engine/api/workers.go:131`: `Workers` maps slice → step → one endpoint; `missing` (`:250`) checks a slice's steps only. `src/deploy/ap:1137` `register_worker` sends `{"endpoint": "http://<container>:8080"}` with the slice's worker token; the container name holds the project's hash. `ap:1407`: "The engine keeps one registration per slice and step, so none is moved away from a run of the slice that has not ended". Each project has its own containers (ADR 0036) |
| S4 | `src/engine/api/preview/2026-11-12-preview/run-event.schema.json`: every event has `at`; `AttemptMade` and an attempt's `StepRejected` carry `usage` (`tokens`, `cost_usd`); `attempt` and `round` are on step events; `StepAccepted` and `ResultReused` carry the step's output `artifacts`. A run's record therefore holds every measure FR-005 and FR-006 need |
| S5 | `src/engine/durable/operation.go:53`: the generic Operation workflow runs one job (`Operator.Do`) with heartbeats for up to 30 days, repeats it after a stop and writes `cancelled` when cancelled ([ADR 0068](../../docs/decisions/0068-operations-recorded-then-run-as-workflows.md)). `api/runs.go:486` `follow` is a run operation's job: it waits on the run's record until the run ends |
| S6 | `src/engine/api/runs.go:774` `StartRun`: refusals in order (NotFound, PatternDoesNotFit, NotApproved, the captured-pattern checks, WorkersNotReady, RunActive, ProjectExists), then `newRun` (`:583`) records the run and its `run` operation and starts its workflow. `AcceptHandOver` (`:995`) signals `accept-hand-over` to a waiting run |
| S7 | `src/engine/api/preview/2026-11-12-preview/openapi.yaml:1297` `cancelOperation`: cancelling a run's operation ends the run `cancelled` in any state; `:2299` `OperationKind` lists 14 kinds, `run` covering resume, replay and hand-over |
| S8 | `src/catalogue/api-feature-go/pattern.yaml:47`: a run's budget is 3,000,000 tokens, $0.50 and 30 minutes; the comment at `:40` puts a happy run at about $0.04 and the worst at about $0.12 with Claude Haiku |
| S9 | `docs/references/docker-agent.md:200`: `docker agent eval --repeat` reports pass@k and pass^k over recorded sessions of one agent ([Docker Agent - Evaluation](https://docker.github.io/docker-agent/features/evaluation/)) |
| S10 | Feature 008 research R4 (`specs/008-eval-variance/research.md`): lowest, median (the mean of the two middle values for an even count) and highest; unstable when some passed and some failed; "less reliable" when the share falls ([ADR 0083](../../docs/decisions/0083-eval-runs-keep-measurements-and-compare-with-their-history.md)) |
| S11 | `src/engine/store/captured.go` and `store/records.go:109` `sliceDir`: the engine store keeps records per slice in the slice's folder; `EraseSlice` (`store/store.go:323`) removes the folder and the artifacts only it holds |
| S12 | Constitution: Principles IV (operations), VI (OSS first), X (bounded runs, waits with a timeout), XIV (budgets), XV (compatible APIs); Engineering Principles "Values have one home", "Reliable events and side effects" |

## R1. What runs the trials

- **Decision**: a trial set is one engine operation of a new kind, `run-trials`, whose job starts
  the set's trials one after another. Each trial is an ordinary run started as `startRun` starts
  one (same pattern choice and refusals), with `NoReuse` and the set's ID on its `RunStarted`; the
  job follows it with the run operation's own wait (`follow`) and starts the next when it ends.
- **Repeats** (analyze round 1, E1): `newRun` (`api/runs.go:583`) takes its ID from
  `NextRunID` itself, so it gains an optional given ID: the job reserves the ID, writes it into
  the set, then calls `newRun` with it (`RejectDuplicate`). A repeated job that finds the ID with a
  workflow follows it; with neither a workflow nor a record, it starts it again with the same ID;
  the trial's `run` operation is recorded once (data model, Write order and repeats).
- **Hand-over while following** (U2): `follow` returns only at the run's end, so the job uses its
  own loop over the run's record changes (`rec.next`): `waiting` sends `accept-hand-over` as
  `AcceptHandOver` does; a `RunNotWaiting` is read again.
- **Slice changes** (F1): the set keeps the slice's approved part hashes at start; before each trial
  they are compared with the slice's current ones, so an edit approved again between two trials
  stops the set `slice-changed`. `deleteSlice` is refused while a set of the slice runs (E2).
- **Rationale**: S5, S6. The Operation workflow already gives the set acceptance, a queryable
  lifecycle, cancellation, retries and survival across a stop (FR-001, Principle IV); the run path
  already checks, records and ends a run exactly once (FR-002, Principle X). Before starting trial
  *i* the job writes the run ID it will use into the set's record, so a repeated job after a stop
  follows that run instead of starting a second one (idempotent, Principle IV).
- **Alternatives**: a Temporal parent workflow with child Run workflows (a second way to run
  operations, against ADR 0068); the terminal client looping `startRun` (the set would stop with
  the terminal, against FR-001; the owner chose the engine, Clarifications).

## R2. A trial reuses nothing

- **Decision**: trials run with `NoReuse` (S1). Their accepted results are kept as any run's, under
  "the first result stays" (S2), so trials never change what a later ordinary build reuses.
- **Rationale**: FR-002, SC-004. No new mechanism: feature 011's proof uses the same field.
- **Consequence**: a trial of a slice whose earlier run is kept still pays the model for every
  agent step; budgets bound it (R5).

## R3. Each trial runs on its own project's step workers

- **Decision** (*owner*, Clarifications): `ap trials` makes the k projects `<prefix>-1` to
  `<prefix>-k` and starts each one's step workers with `build-workers` before it asks for the set.
  `WorkerRegistration` gains an optional `project`; the launcher always sends it. The engine keeps
  registrations by slice, step and project, a run uses only its own project's workers, and a
  registration without a project (one kept from before) serves any project of its slice, as today.
  The set is refused `WorkersNotReady` at once when any of its projects lacks a step's worker.
- **Every reader of registrations** (analyze round 1, W1) takes the project: `missing` and
  `Endpoint`; `ready()` in `StartRun`, `ResumeRun` and `ReplayRun` (each with the run's project);
  `workersReady` signals `workers-ready` only to a run waiting for workers of the registering
  project (`waitingForWorkers` by slice and project); `forget`, `DeleteWorkerRegistration` and
  erasure remove all of a slice's projects. A registration without a project is used only when the
  project has none of its own.
- **Rationale**: S3. With one registration per slice and step, registering project 2's workers
  moves the slice away from project 1 and a run would write into another project's folder. Naming
  the project lets k projects' workers stand side by side (FR-013) and lets the set run with the
  terminal closed. The launcher's `refuse_active_run` stays: it protects the registration of a
  project with a run that has not ended.
- **Cost**: k projects' containers (three agent steps and the procedure steps per project, at most
  5 projects) stay up until `ap down`, as every project's containers do today (ADR 0036). The
  quickstart measures their memory on the host.
- **Alternatives**: the launcher starts each trial's workers just before it (the set would advance
  only while `ap trials` runs; owner rejected); the engine derives the project from the endpoint's
  container name (the engine would depend on the launcher's naming, against Principle XVIII).

## R4. A trial that waits for the developer

- **Decision** (*owner*, Clarifications): when a trial waits for the developer (`waiting-for-developer`:
  out of attempts, review rounds or budget), the job accepts its hand-over as `acceptHandOver`
  does (S6), so the trial ends `handed-over`, and starts the next. An interrupted trial (a platform
  stop, or the model unavailable on every attempt) is waited for, within the set's time budget.
- **Rationale**: a hand-over waits up to 14 days; a set that waits for it would stall k − 1 trials
  for an outcome already known (not done). The project stays, so the developer can still read it.
- **Alternative**: the set waits like any run (owner rejected).

## R5. Limits and budgets

- **Decision**: the engine contract declares in `x-limits`: `trials-max: 5`, `trials-default: 3`,
  `trial-set-cost-usd: 2`, `trial-set-hours: 24` (*owner* for the first three). The job starts a
  trial only while the set's cost used so far plus the chosen pattern's own run cost budget
  (`budgets.run.cost_usd`, $0.50 for the base pattern, S8; read from the pattern, never a literal)
  is at most the set's, so the set never exceeds $2 (SC-006). A run's budget is checked after each attempt
  (`durable/workflow.go:718` `overBudget`), which then hands the run over, so a run's cost can pass
  its budget by at most that one attempt, itself bounded by the step budget ($0.20); SC-006 is
  therefore "within $2 plus at most one step budget" in the worst case, and the rule's margin (one
  whole run reserved) covers it. With five costly trials the fifth may not start: the set stops
  `budget`; a set still running 24 hours after it started
  cancels its trial and ends `stopped`. A trial's own run budget is unchanged.
- **Rationale**: S8. Five happy trials cost about $0.20; the rule stops a set only after costly
  trials. 24 hours bounds waiting for an active run or an interrupted trial (Principle X: a wait
  has a timeout) and is well above 5 × 30 minutes. "Values have one home": client-visible limits
  live in the contract, read by the engine and the client.
- **Alternatives**: a per-set token budget (tokens and cost move together, S8; the spec names cost);
  a time budget summed over the trials' running time (equals 5 × 30 minutes, adding nothing).

## R6. How the result is computed and shown

- **Decision**: the result is computed from the trials' run records when it is read (FR-007):
  - **Done**: trials that ended `done` over trials that ended; unstable when at least one ended done
    and at least one did not (S10).
  - **Per step**, over the trials whose record has the step: attempts (its `AttemptMade` events,
    plus `StepRejected` events carrying `usage` without an `AttemptMade`), review rounds (the
    highest `round`), duration (the sum of the step's attempt spans, `StepStarted` to its
    `StepAccepted` or `StepRejected`), tokens and cost (sum of `usage`). Lowest, median and
    highest as feature 008 (S10). A measure no such trial carries is "not available".
  - **Per trial totals**: the same measures summed, duration from `RunStarted` to the run's end
    without the time it was interrupted.
  - **Files**: per step, the `artifacts` of its last `StepAccepted` (or `ResultReused`); "same" when
    every trial that accepted the step has the same hashes, else the trials grouped by hashes.
  - **Comparison**: with the latest earlier `ended` or `stopped` set of the same slice that has at
    least one ended trial: the change in the share of done trials, "less reliable" when it fell,
    both sets' methods named, and "methods differ" when they do (FR-009); none when there is no
    such set.
  - **Mixed methods** (I2): `methods_mixed` when the trials' runs (the `method` on their step
    events) ran under more than one method; the counts stay over all trials and the result says so.
  - **Interrupted time** (U4) is left out of step and trial seconds alike.
- **Rationale**: S4 holds every input; computing on read keeps one home for each fact (the run
  record) and needs no stored result to keep in step (Principle XVII, "Derive the rest").
- **Alternative**: storing the measures with the set (a second copy of the run records).

## R7. Where sets live and how they are reached

- **Decision**: a set's record (ID `TS-<n>`, slice, k, prefix, method, budget, state, trial run IDs,
  start and end) is kept in the engine store in the slice's folder, so erasing the slice erases it
  (S11, FR-011). The engine contract gains `POST /slices/{sliceId}/trial-sets` (202 with the
  operation), `GET /slices/{sliceId}/trial-sets` (paginated, newest first) and
  `GET /trial-sets/{trialSetId}` (the set with its derived result); cancelling uses
  `cancelOperation`. The terminal client gains `trials`, `trial-sets` and `trial-set`; the launcher
  intercepts `trials` to make the projects and their workers first (R3). The run page is unchanged
  (FR-012, *owner*).
- **Rationale**: Principle III (every action through the API), XV (Idempotency-Key, page tokens);
  the same shape as `/captured-patterns` (feature 011).

## R8. OSS and Docker Agent (Principle VI)

- Docker Agent's `eval --repeat` gives pass^k over recorded sessions of one agent (S9). A trial is
  a whole run of a pattern (several agents, fixed procedures, checks, review rounds) on the
  developer's slice, not a recorded session, so it does not apply; the platform's eval suites
  already use pass^k for their agents (feature 008).
- The engine already adopts Temporal for durable operations; the set uses its existing Operation
  workflow (R1). No other project runs a pattern's build and summarises its record; the result is a
  few counts and order statistics over the run record (R6), which needs no library. No dependency
  is added.

## R9. Contract compatibility

- The new paths, schemas, `x-limits` keys, `WorkerRegistration.project`, `Worker.project`, and
  `RunStarted`/`Run` fields `trial_set` and `trial` are additive.
- `Operation.target`'s rule `operation_target` (`^((S|RUN)-[0-9]+|[a-z0-9-]+)$`) widens to
  `TS-<n>`; feature 011's widening of the same rule was reported as the warning
  `response-property-pattern-changed` (011 research E2), not an error. T001 runs the pinned oasdiff
  on its draft and records the report here before it is committed; any error beyond the
  `run-trials` value goes to the owner before T001 is committed (analyze round 1, C1).
- `OperationKind` gains `run-trials`. The pinned oasdiff reports an added response enum value as
  breaking (`response-property-enum-value-added`, feature 011 research E2), so this is an exception
  to Principle XV like [ADR 0100](../../docs/decisions/0100-preview-contract-gains-enum-values-in-place.md),
  which needs the owner's approval for this feature (plan, Complexity Tracking). The alternative,
  reusing the kind `run` for a set, would make an operation of kind `run` target something that is
  not a run.
- The Run workflow records the new `RunStarted` fields, so its version goes from 2 to 3 behind
  `workflow.GetVersion` ([ADR 0067](../../docs/decisions/0067-a-run-is-one-temporal-workflow.md)).
