# Tasks: A developer sees how much the builds of a slice vary

**Input**: Design documents from `specs/012-run-variance/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Required. Outside-in, test-first: new tests are seen failing before the code; the
result's rules have clean cases and planted ones (SC-002, SC-003).

**Organization**: the contract (Phase 1), registrations by project (Phase 2, needed by every
story), then US1 (start, sequence and end a set; done out of ended; unstable), US2 (per-step and
per-trial spread; which files differ), US3 (list sets; compare with the previous set). The base
pattern, its template, agents and recordings do not change, so the recorded-model jobs stay green.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1, US2 or US3 (spec.md)

## Rules for every task

- **Contract first**: T001 extends the engine contract and the terminal contract as
  [contracts/trial-sets.md](contracts/trial-sets.md) states, additively except `run-trials`
  ([ADR 0105](../../docs/decisions/0105-preview-contract-gains-run-trials-in-place.md)), and is
  committed alone, before any test or code; `scripts/generate.sh` regenerates the server, client
  and types; generated code is never edited by hand.
- **Test-first**: a test task and the code task that makes it pass are separate tasks, done by
  different authors; new tests go in new files. The code author never changes a test, a fixture
  or test data.
- **Existing tests**: only changes the owner approves by name (T002) are made, each commit carrying
  `Tests-approved-by: Marius Gologan (<date of the approval>)`. Any other existing test that needs a
  change is stopped and asked for, quoting what changes and why.
- **Method identity**: every commit that changes a file `src/engine/method/method.json` lists runs
  `scripts/method-digest.py` and commits the converged `src/engine/method/method.sha256`.
- **Trailers**: `Approved-by: Marius Gologan (2026-11-12)` is the owner's standing approval to
  commit finished, tested work to protected paths.
- **No dependency** is added.
- **Fail closed**: `trials` outside 1 to 5, a `project_prefix` that does not make valid project
  names, a missing worker for any project, an existing project: refused before anything is recorded
  or started, naming it (Values that check themselves).
- **One owner**: the result's rules live only in `src/engine/trials` (a pure function from run
  records to the result); the set's job in `src/engine/api` only sequences runs through the
  existing run path (`StartRun`'s checks, `newRun`, `follow`, the hand-over signal).
- **Limits have one home**: `trials-max: 5`, `trials-default: 3`, `trial-set-cost-usd: 2`,
  `trial-set-hours: 24` are read from the contract's `x-limits`, never repeated as literals; a
  contract test pins the engine's values to them.
- **Test time**: locally only the packages a task touches (`go test ./trials/ ./api/ ./durable/
  ./store/ ./machine/` in `src/engine`, `go test ./...` in `src/cli`, `go vet ./...` in `src/tests`
  and `go test ./...` in `src/page` after regenerating, the touched `scripts/tests/test_*.py`
  files), each capped at 10 minutes; no full `scripts/check.sh`, Docker lab, image build or
  acceptance suite without the owner's go-ahead. The full test command is the CI run of the pushed
  commit; every push is told to the owner with the jobs it runs and waits for a go-ahead. A live
  eval run is paid and needs the owner's go-ahead.
- **Briefs**: every test-author and reviewer brief says: think through the edge cases and list
  those checked; it gives a time limit and a cap on full runs.
- **Fresh review**: each story and the feature end with a fresh review by a read-only agent on
  another model than the author's, in its own worktree at the branch commit with the uncommitted
  and untracked files, judging the CI run; at most 3 rounds. Then `/speckit-converge`.
- **Lint**: before each push of Go changes, the pinned golangci-lint with gosec on the changed
  lines.
- **Gate**: `/speckit-analyze` (fresh reviewer) must report no CRITICAL or HIGH before T001, and
  again after any change to spec, plan, tasks or a design artifact.

---

## Phase 1: Setup (contract first)

- [ ] T001 Contract (committed alone): `src/engine/api/preview/2026-11-12-preview/openapi.yaml`
  gains `POST /slices/{sliceId}/trial-sets` (`startTrialSet`, body `{project_prefix, trials?}`,
  `trials` integer `minimum: 1`, `maximum: 5`, default 3; 202 operation; `Idempotency-Key`; at
  once 404 `NotFound`, 409 `PatternDoesNotFit`, `NotApproved`, the captured-pattern refusals of
  `startRun`, `WorkersNotReady` naming the project and step, `ProjectExists` naming the project,
  `RunActive` while another set of the slice is `running`), `GET /slices/{sliceId}/trial-sets`
  (`listTrialSets`, newest first, at most 50 per page, opaque page tokens; 404 for an unknown
  slice), `GET /trial-sets/{trialSetId}` (`getTrialSet`; 404 for an unknown or erased set), the
  schemas `TrialSet`, `TrialSetList`, `TrialResult` with every field of the data model (Trial set,
  Trial result: `id` `TS-<n>`, `state` `running|ended|cancelled|stopped`, `stopped_because`
  `budget|time|slice-changed|workers`, measures `attempts`, `review_rounds`, `seconds`, `tokens`,
  `cost_usd` each `{lowest, median, highest}` or absent), `x-limits` `trials-max: 5`,
  `trials-default: 3`, `trial-set-cost-usd: 2`, `trial-set-hours: 24`; `OperationKind` gains
  `run-trials` and `Operation.target` accepts a trial-set ID; `WorkerRegistration` and `Worker`
  gain the optional `project` (`project_name`); `run-event.schema.json` `RunStarted` and `Run` gain
  `trial_set` and `trial` (1 to 5); `slice-defs.schema.json` gains `trial_set_id`
  (`^TS-[0-9]+$`). `src/cli/contract.md` gains the rows and output of `trials`, `trial-sets` and
  `trial-set` (contract draft, Terminal client) and `ap`'s interception of `trials`. Regenerate;
  record the pinned oasdiff's report (expected: only `response-property-enum-value-added` for
  `run-trials`) in research R9
- [ ] T002 Owner approvals: the plan's Complexity Tracking row for Principle XII (not applicable,
  one developer); the code author of T001 lists every existing test that fails or no longer
  compiles after T001 (expected: tests that list the contract's paths, operation kinds or fields
  exactly) and asks the owner to approve each by name, recorded here with its date; a change not
  approved is not made, and the task that needs it stops

---

## Phase 2: Foundational - step workers by project (blocks every story)

**Goal**: several projects of one slice have ready step workers at once, and a run calls only its
own project's (FR-013; research R3; ADR 0103).

- [ ] T003 [P] Test author: new `src/engine/api/workers_project_test.go` (engine rig): two projects
  of one slice register their workers (`project` set) and both stay registered; a run of each
  calls only its own project's endpoint; a registration without `project` serves any project of
  its slice; `startRun` into a project whose step has no worker of its own or without a project
  answers `WorkersNotReady` naming the step and project; registrations by project survive an
  engine restart; erasing the slice removes all of them; seen failing before T005
- [ ] T004 [P] Test author: new `scripts/tests/test_build_workers_project_registration.py`:
  `build-workers <slice> <project>` sends `{"endpoint": …, "project": "<project>"}` for each step;
  building workers for a second project of the same slice leaves the first project's containers
  and registrations in place; seen failing before T005
- [ ] T005 Code author (done when T003 and T004 pass): `src/engine/api/workers.go` keeps
  registrations by slice, step and project (`missing` and the lookup take the run's project, a
  registration without a project as fallback); `src/engine/store/workers.go` keeps the project;
  `src/engine/api/runs.go` `ready()` takes the project; `src/deploy/ap` `register_worker` sends
  `project`; the launcher's `refuse_active_run` stays; method identity converged

---

## Phase 3: User Story 1 - Build a slice several times and see how often it ends done (Priority: P1) 🎯 MVP

**Goal**: the developer asks for k trials; they run one after another, each reusing nothing, into
their own projects; the set ends in one state and shows done out of ended and the unstable mark
(FR-001 to FR-004, FR-007, FR-010 to FR-013; research R1, R2, R4, R5, R7).

**Independent Test**: T006-T008 pass; quickstart 1 and 2: a set of 3 with one trial planted to wait
for the developer shows `done 2/3`, `unstable`, each trial's run and outcome; the third trial starts
only after the second ended; every trial's `RunStarted` has `trial_set` and no `ResultReused`.

- [ ] T006 [P] [US1] Test author: new `src/engine/trials/outcome_test.go` on planted run records:
  outcomes done, done, handed-over give `done 2`, `ended 3`, `unstable`; 3/3 and 0/3 are not
  unstable (SC-002); a set with 2 of 3 trials ended counts only the ended; a cancelled trial counts
  as ended and not done; an expired trial is not done; each trial's run ID, outcome and method;
  seen failing before T009
- [ ] T007 [P] [US1] Test author: new `src/engine/api/trial_sets_test.go` (engine rig, durable
  service, recorded or stub workers): `startTrialSet` answers 400 `InvalidRequest` for `trials` 0
  or 6 and for a prefix that makes an invalid project name, and records nothing; refusals in the
  data model's order, at once, with nothing started; trials start one after another, trial *i*
  only after trial *i − 1* ended, into `<prefix>-<i>`, each with `NoReuse` (no `ResultReused`
  although an earlier run of the slice is kept; SC-004) and `trial_set`/`trial` on `RunStarted`
  and `Run`; another active run makes the set wait, not fail; a trial that waits for the developer
  is handed over by the set (`handed-over`, its reason names the set) and the next starts; a set
  whose cost so far plus $0.50 would pass $2 stops `budget` and starts no further trial (SC-006);
  24 hours (a fake clock) stops it `time` and cancels its trial; the slice losing an approval stops
  it `slice-changed`; `cancelOperation` on the set's operation cancels the running trial, starts no
  other and ends the set `cancelled`; the job repeated after a stop follows the trial run it
  recorded and never starts a second; a second set of the slice while one runs is 409 `RunActive`;
  `getTrialSet` reads without a model call or a changed record (SC-005); erasing the slice erases
  its sets (404); limits equal the contract's `x-limits`; seen failing before T009
- [ ] T008 [P] [US1] Test author: new `src/engine/durable/trial_fields_test.go`: a run started at
  workflow version 3 records `trial_set` and `trial` on `RunStarted`; a run started under version 2
  replays unchanged; seen failing before T009
- [ ] T009 [US1] Code author (done when T006-T008 pass): `src/engine/trials/` (outcome part of the
  result), `src/engine/store/` trial-set records in the slice's folder with an ID sequence that
  never repeats, `src/engine/api/trial_sets.go` (`startTrialSet` with `StartRun`'s checks for every
  project, the `run-trials` job: write trial *i*'s run ID, start it through `newRun` with
  `NoReuse`, follow it, accept a hand-over, check budget, time and approval before each trial;
  `getTrialSet`), `src/engine/api/operations.go` (`run-trials`), `src/engine/api/durable.go`
  `RunStart.TrialSet`/`Trial`, `src/engine/durable/` and `src/engine/machine/` (the `RunStarted`
  fields, workflow version 3); method identity converged
- [ ] T010 [P] [US1] Test author: new `src/cli/trials_test.go` and
  `scripts/tests/test_ap_trials.py`: `ap trials <slice> <prefix> [--trials <k>]` runs
  `build-workers` for `<prefix>-1` to `<prefix>-<k>` before the client's `trials`, and stops at
  the first that fails, starting no set; the client starts the set, prints one line per trial as it
  ends and the `done <d>/<e>` line with `unstable` when marked; exit 0 when every trial ended done,
  7 when any did not; `trial-set <id>` prints the same for an ended set and follows a running one;
  detaching exits 130 naming the operation; seen failing before T011
- [ ] T011 [US1] Code author (done when T010 passes): `src/cli/` `trials` and `trial-set` (generated
  client only), `src/deploy/ap` intercepts `trials`
- [ ] T012 [P] [US1] Test author: new `src/tests/acceptance/trials_test.go` (Compose stack,
  recorded model): `ap trials <slice> <prefix> --trials 2` on the notes slice: both projects get
  their workers, two runs end done one after the other, each makes model requests for every agent
  step (SC-004), `ap trial-set` prints `done 2/2` without `unstable`, and reading it adds no model
  request (SC-005); records the step containers' memory for two projects (research R3). Passes or
  fails on T013's CI run
- [ ] T013 [US1] (after T005, T009, T011 and T012) Commit (tests and code in separate commits);
  fast lint; ask the owner before pushing (every check job runs); record the CI run here
- [ ] T014 [US1] Fresh review of US1 judging T013's CI run; findings fixed as new tasks or
  recorded; `/speckit-converge` for US1

---

## Phase 4: User Story 2 - See where the trials differ and how their effort spreads (Priority: P2)

**Goal**: per step and per trial, the lowest, median and highest of each measure, and which steps'
files differ (FR-005, FR-006; research R6).

**Independent Test**: T015 and T016 pass; quickstart 1: a code step needing 1, 1 and 2 attempts
shows `1/1/2`, its files differing in the third trial; a fixed procedure's tokens are `-`.

- [ ] T015 [P] [US2] Test author: new `src/engine/trials/measures_test.go` on planted run records:
  per step over the trials that reached it, attempts (`AttemptMade` plus a `StepRejected` with
  `usage` and no `AttemptMade`), review rounds (highest `round`), seconds (the sum of the step's
  attempt spans, `StepStarted` to its `StepAccepted` or `StepRejected`), tokens and cost (sum of
  `usage`), as `{lowest, median, highest}`; the median of an even count is the mean of the two
  middle values; a measure no such trial carries is absent ("not available"), never 0; per trial
  totals, seconds from `RunStarted` to the end without interrupted time; files `same` when every
  trial that accepted the step has the same output hashes of its last `StepAccepted` or
  `ResultReused`, else the trials grouped by hashes, naming each (SC-003); a step no trial reached
  is listed with `reached 0`; seen failing before T017
- [ ] T016 [P] [US2] Test author: new `src/cli/trial_result_test.go`: `trial-set <id>` prints the
  trial rows (`RUN`, `OUTCOME`, `ATTEMPTS`, `ROUNDS`, `SECONDS`, `TOKENS`, `COST`) and the step rows
  (`STEP`, `REACHED`, each measure as `lowest/median/highest` or `-`, `FILES` `same` or the
  groups), aligned as `runs`; seen failing before T017
- [ ] T017 [US2] Code author (done when T015 and T016 pass): the measures and files in
  `src/engine/trials/`, served by `getTrialSet`; the rows in `src/cli/`
- [ ] T018 [US2] Commit; fast lint; push (owner go-ahead); record the CI run
- [ ] T019 [US2] Fresh review of US2 judging T018's CI run; `/speckit-converge` for US2

---

## Phase 5: User Story 3 - The result of earlier trials stays readable and comparable (Priority: P3)

**Goal**: sets are listed, and each is compared with the slice's previous set (FR-008, FR-009;
research R6, R7).

**Independent Test**: T020 and T021 pass; a second set with one more trial handed over, under a
planted other method, names both methods and is `less reliable`.

- [ ] T020 [P] [US3] Test author: new `src/engine/trials/previous_test.go` and
  `src/engine/api/trial_sets_list_test.go`: `listTrialSets` newest first, at most 50 per page,
  opaque page tokens, a token from another list refused 400, each item with state, k, method,
  `done`/`ended` and `unstable`; `previous` is the latest earlier `ended` or `stopped` set of the
  same slice (a `cancelled` or `running` one is skipped), with the change in share, `less_reliable`
  only when the share fell, `methods_differ` when the methods differ; the first set has no
  `previous`; seen failing before T022
- [ ] T021 [P] [US3] Test author: new `src/cli/trial_sets_test.go`: `trial-sets <slice>` prints one
  table (`SET`, `STATE`, `TRIALS`, `DONE`, `UNSTABLE`, `METHOD`, `CREATED`), every page read; with
  none, a line naming `trials`; `trial-set` prints the comparison line; seen failing before T022
- [ ] T022 [US3] Code author (done when T020 and T021 pass): `previous` in `src/engine/trials/`,
  `listTrialSets` in `src/engine/api/trial_sets.go`, `trial-sets` and the comparison line in
  `src/cli/`
- [ ] T023 [US3] Commit; fast lint; push (owner go-ahead); record the CI run
- [ ] T024 [US3] Fresh review of US3 judging T023's CI run; `/speckit-converge` for US3

---

## Phase 6: Polish & cross-cutting

- [ ] T025 Docs: `docs/architecture/overview.md` (trial sets, registrations by project, ADR 0102 to
  0105), `docs/apis/engine-api.md`, `src/engine/README.md`, `src/cli/README.md` (`trials`,
  `trial-sets`, `trial-set`), `src/deploy/README.md` (`ap trials`), N-054 in
  `docs/overview/problem-space.md` (state), `docs/log.md`; `python3 scripts/okf.py check` OK
- [ ] T026 Paid evals for the analyst and steps suites (owner go-ahead; add the branch to
  environment dev's branch policies): the method identity changed; every counted case pass^3
- [ ] T027 Final fresh review of the feature judging the head's CI run and T026; `/speckit-analyze`
  and `/speckit-converge` for the whole feature until clean
- [ ] T028 Merge: every task above is checked (done or `closed:`); `evals-current` on the branch
  lists no pending suite; the owner fast-forwards `main`; remove the branch from environment dev's
  policies; delete the branch with the owner's OK; the spec's status `Implemented`

---

## Dependencies & execution order

- T001 → T002 → Phase 2 (T003, T004 in parallel → T005) → US1 (T006-T008 in parallel → T009;
  T010 → T011; T012; T005, T009, T011 and T012 → T013 → T014) → US2 (T015, T016 in parallel →
  T017 → T018 → T019) → US3 (T020, T021 in parallel → T022 → T023 → T024) → T025 → T026 → T027 →
  T028.
- US2 and US3 extend US1's result and resource; US3's comparison needs US1's sets only, so it may
  run beside US2 when their files do not overlap (`trials/previous.go` against
  `trials/measures.go`).

## Parallel examples

- Phase 2: T003 and T004 by separate test-author sessions.
- US1: T006, T007, T008, T010 and T012 by separate test-author sessions (different files).
- US2: T015 and T016; US3: T020 and T021.

## Implementation strategy

- MVP is US1: the developer can ask for trials and see how often the slice builds done, which is the
  variation N-054 is about.
- US2 tells which step varies and how much; US3 shows whether a change helped.
- No recordings change: the base pattern, its template and agents are unchanged.
