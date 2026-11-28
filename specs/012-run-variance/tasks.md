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
- **Method identity**: every commit that changes a file listed in `src/engine/method/method.json`
  runs `scripts/method-digest.py` and commits the converged `src/engine/method/method.sha256`.
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
  contract test pins the engine's values, and the `trials` schema's `minimum`, `maximum` and
  `default`, to them.
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
  again after any change to spec, plan, tasks or a design artifact. Round 1 (fresh Sonnet, at
  42cbf73): 0 CRITICAL, 3 HIGH (E1 the trial's run ID reserved before its start has no path
  through `newRun`; F1 a slice edited and approved again between trials; W1 `workersReady`,
  resume and replay read registrations without the project), 11 MEDIUM, 8 LOW; fixed at their
  source (spec edge cases, FR-004, FR-009, FR-010; research S2, R1, R3, R5, R6, R9; data model
  Write order and repeats, `part_hashes`, `methods_mixed`, `previous`; contract draft exit codes;
  plan Exceptions; T001, T003, T005 to T007, T009, T010, T012, T015, T020, new T029); C3 (the
  Principle XII row) stays with T002 as in feature 011; P2 (containers' memory) is measured in
  T012 and judged by the owner then. Round 2 (same reviewer, the fixes 42cbf73..41a3604): every
  round-1 finding fixed except C3 (kept) and P2 (closed); N2 HIGH (FR-004, FR-009, FR-010 changed
  without a recorded clarification) -> the owner confirmed them (spec Clarifications 2026-11-27);
  N1 MEDIUM (the set and its operation are two stores) -> data model Write order 1, T007, T029;
  N3 LOW (R5's bound) reworded; P3 closed: the run page shows no trial field and T001's
  regeneration runs `go test ./...` in `src/page`, whose drift check proves the generated client.
  Round 3 (same reviewer, 41a3604..48a3f62): N1 to N3 fixed, P3 closed; N4 MEDIUM (a set written
  late from the operation's body took `method` and `part_hashes` at job time) and N5 LOW
  (`deleteSlice` while a `run-trials` operation is open) fixed in the data model and T007. Gate
  passed: 0 CRITICAL, 0 HIGH. Review converged in 3 rounds.

---

## Phase 1: Setup (contract first)

- [X] T001 Contract (committed alone): `src/engine/api/preview/2026-11-12-preview/openapi.yaml`
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
  run the pinned oasdiff on the draft before committing and record its report in research R9
  (expected: `response-property-enum-value-added` for `run-trials`, and the warning
  `response-property-pattern-changed` for `operation_target`, which gains `TS-<n>`); any other
  error goes to the owner before the commit. Done: oasdiff report in research R9 (19
  `run-trials` errors, 19 pattern warnings, 1 `allOf` warning; nothing else); generated server
  methods answer as not implemented until T009 (`src/engine/api/trial_sets.go`); method identity
  converged; every existing engine, CLI and page test still passes (`go test` in `src/engine/api`,
  `durable`, `store`, `machine`, `src/cli`, `src/page`; `test_contract_diff`,
  `test_inline_schema`), so T002 has no broken existing test to approve for T001
- [X] T002 Owner approvals: the plan's Complexity Tracking row for Principle XII (not applicable,
  one developer); the code author of T001 lists every existing test that fails or no longer
  compiles after T001 (expected: tests that list the contract's paths, operation kinds or fields
  exactly) and asks the owner to approve each by name, recorded here with its date; a change not
  approved is not made, and the task that needs it stops. T005's author does the same, before
  T005's commit, for the existing tests and fakes that pin the registry's key or
  `registrationStore` (`api/workers_test.go`, `api/worker_registrations_restart_test.go`,
  `store/worker_registrations_test.go`)
  Recorded: the owner approved the Principle XII row on 2026-11-27; T001 broke no existing test,
  so it needs no approval. T009 (owner 2026-11-27): `src/engine/api/skeleton_test.go:234` (`operation_target` gains `TS`, as the contract since T001) and `src/engine/api/durable_service_test.go:203` (`RunInput` gains `TrialSet`, `Trial`). T005's list, approved by the owner on 2026-11-27 (each call site gains
  only a project argument, `""` for the project-less registration, as `Workers.Endpoint(slice,
  project, step)`, `steps.Endpoints` and `store.DeleteWorkerRegistration(slice, step, project)`
  gain it): `src/engine/steps/adapters_test.go:46` (the `endpoints` fake),
  `src/engine/api/grants_dir_test.go` (its 10 `Endpoint` calls), `api/grants_trailing_test.go:24`,
  `api/worker_registrations_restart_test.go:171` and `:457`, `api/runs_durable_test.go:343`,
  `src/engine/store/worker_registrations_test.go` (its 6 `DeleteWorkerRegistration` calls and the
  doc comment at `:10`). Any other existing test T005 breaks is stopped and asked for

---

## Phase 2: Foundational - step workers by project (blocks every story)

**Goal**: several projects of one slice have ready step workers at once, and a run calls only its
own project's (FR-013; research R3; ADR 0103).

- [X] T003 [P] Test author: new `src/engine/api/workers_project_test.go` (engine rig): two projects
  of one slice register their workers (`project` set) and both stay registered; a run of each
  calls only its own project's endpoint; a registration without `project` serves any project of
  its slice; `startRun` into a project whose step has no worker of its own or without a project
  answers `WorkersNotReady` naming the step and project; a project-less registration is used only
  when the project has none of its own; registering project 1's workers sends `workers-ready` only
  to a run waiting for project 1's workers, never to a waiting run of project 2 of the same slice;
  `resumeRun` and `replayRun` check the workers of their run's (or the new) project;
  registrations by project survive an engine restart; erasing the slice removes all of its
  projects' registrations; seen failing before T005
- [X] T004 [P] Test author: new `scripts/tests/test_build_workers_project_registration.py`:
  `build-workers <slice> <project>` sends `{"endpoint": …, "project": "<project>"}` for each step;
  building workers for a second project of the same slice leaves the first project's containers
  and registrations in place; seen failing before T005, on the CI run of a push of the T003 and
  T004 tests before T005's code (plan, Exceptions). Seen failing: CI 46029974303 at 3e4bd11,
  `py_tests` 4 of 5 failed (the body has no `project`), the containers test passed, nothing else
  failed
- [X] T005 Code author (done when T003 and T004 pass): `src/engine/api/workers.go` keeps
  registrations by slice, step and project (`missing`, `Endpoint` and the lookup take the run's
  project, a registration without a project only as fallback; `workersReady` and
  `waitingForWorkers` by slice and project; `forget` and `DeleteWorkerRegistration` cover every
  project of the slice); `src/engine/store/workers.go` keeps the project; `src/engine/api/runs.go`
  `ready()` takes the project in `StartRun`, `ResumeRun` and `ReplayRun`; `src/deploy/ap` `register_worker` sends
  `project`; the launcher's `refuse_active_run` stays; method identity converged. Done: 8cd8693 (method eafebed3), T003 c033975; CI 46041159126 at 73e0a4e: every job but T012's expected red passed, T004's 5 tests included (46033845550 at d0813c3 had them too, but for one intermittent Python test, backlog T211)

---

## Phase 3: User Story 1 - Build a slice several times and see how often it ends done (Priority: P1) 🎯 MVP

**Goal**: the developer asks for k trials; they run one after another, each reusing nothing, into
their own projects; the set ends in one state and shows done out of ended and the unstable mark
(FR-001 to FR-004, FR-007, FR-010 to FR-013; research R1, R2, R4, R5, R7).

**Independent Test**: T006-T008 pass; quickstart 1 and 2: a set of 3 with one trial planted to wait
for the developer shows `done 2/3`, `unstable`, each trial's run and outcome; the third trial starts
only after the second ended; every trial's `RunStarted` has `trial_set` and no `ResultReused`.

- [X] T006 [P] [US1] Test author: new `src/engine/trials/outcome_test.go` on planted run records:
  outcomes done, done, handed-over give `done 2`, `ended 3`, `unstable`; 3/3 and 0/3 are not
  unstable (SC-002); a set with 2 of 3 trials ended counts only the ended; a cancelled trial counts
  as ended and not done; an expired trial is not done; each trial's run ID, outcome and method;
  trials under two methods give `methods_mixed`, one method does not; seen failing before T009
- [X] T007 [P] [US1] Test author: new `src/engine/api/trial_sets_test.go` (engine rig, durable
  service, recorded or stub workers): `startTrialSet` answers 400 `InvalidRequest` for `trials` 0
  or 6 and for a prefix that makes an invalid project name, and records nothing; refusals in the
  data model's order, at once, with nothing started; trials start one after another, trial *i*
  only after trial *i − 1* ended, into `<prefix>-<i>`, each with `NoReuse` (no `ResultReused`
  although an earlier run of the slice is kept; SC-004) and `trial_set`/`trial` on `RunStarted`
  and `Run`; another active run makes the set wait, not fail; a trial that waits for the developer
  is handed over by the set (`handed-over`, its reason names the set) and the next starts; a set
  whose cost so far plus $0.50 would pass $2 stops `budget` and starts no further trial (SC-006);
  24 hours (a fake clock) stops it `time` and cancels its trial; the slice losing an approval stops
  it `slice-changed`, and so does a part edited and approved again between two trials (its hash
  differs from the set's `part_hashes`); the next project's workers gone stops it `workers`;
  `cancelOperation` on the set's operation cancels the running trial, starts no other and ends the
  set `cancelled`; the job repeated after a stop follows the trial run it recorded and never starts
  a second, and when the recorded ID has neither a workflow nor a record it starts that same ID
  once, with one `run` operation (data model, Write order and repeats); a trial that leaves
  `waiting` before the hand-over signal arrives (`RunNotWaiting`) does not fail the set; a failed
  operation write at `startTrialSet` answers 503 and keeps nothing; a failed set write after the
  operation's leaves the job to write the set from the operation's body, `method` and
  `part_hashes` recorded at acceptance (a slice edited meanwhile stops it `slice-changed`) before
  its first trial, and a second `startTrialSet` or a `deleteSlice` meanwhile is 409 `RunActive`; `deleteSlice` while a set of the
  slice runs is 409 `RunActive`; a second set of the slice while one runs is 409 `RunActive`;
  `getTrialSet` reads without a model call or a changed record (SC-005); erasing the slice erases
  its sets (404); limits equal the contract's `x-limits`; seen failing before T009
- [X] T008 [P] [US1] Test author: new `src/engine/durable/trial_fields_test.go`: a run started at
  workflow version 3 records `trial_set` and `trial` on `RunStarted`; a run started under version 2
  replays unchanged; seen failing before T009
- [X] T009 [US1] Code author (done when T006-T008 pass): `src/engine/trials/` (outcome part of the
  result), `src/engine/store/` trial-set records in the slice's folder with an ID sequence that
  never repeats, `src/engine/api/trial_sets.go` (`startTrialSet` with `StartRun`'s checks for every
  project, the `run-trials` job: reserve and write trial *i*'s run ID, start it through `newRun`
  (which gains an optional given run ID) with `NoReuse`, follow its record in the job's own loop,
  send the hand-over signal on `waiting`, check budget (the chosen pattern's `budgets.run.cost_usd`),
  time, approval and `part_hashes` before each trial; `getTrialSet`), `src/engine/api/slices.go`
  (`deleteSlice` refused while a set runs), `src/engine/api/operations.go` (`run-trials`), `src/engine/api/durable.go`
  `RunStart.TrialSet`/`Trial`, `src/engine/durable/` and `src/engine/machine/` (the `RunStarted`
  fields, workflow version 3); method identity converged
- [X] T010 [P] [US1] Test author: new `src/cli/trials_test.go` and
  `scripts/tests/test_ap_trials.py`: `ap trials <slice> <prefix> [--trials <k>]` runs
  `build-workers` for `<prefix>-1` to `<prefix>-<k>` before the client's `trials`, and stops at
  the first that fails, starting no set; the client starts the set, prints one line per trial as it
  ends and the `done <d>/<e>` line with `unstable` when marked; exit 0 when every trial ended done,
  the exit codes of the contract draft's table (0, 7, 6, 130, 4); `trial-set <id>` prints the same
  for an ended set and follows a running one; seen failing before T011
- [X] T011 [US1] Code author (done when T010 passes): `src/cli/` `trials` and `trial-set` (generated
  client only), `src/deploy/ap` intercepts `trials`
- [X] T012 [P] [US1] Test author: new `src/tests/acceptance/trials_test.go` (Compose stack,
  recorded model): `ap trials <slice> <prefix> --trials 2` on the notes slice: both projects get
  their workers, two runs end done one after the other, each makes model requests for every agent
  step (SC-004), `ap trial-set` prints `done 2/2` without `unstable`, and reading it adds no model
  request (SC-005); records the step containers' memory for two projects (research R3). Seen
  failing first on the CI run of a push of T001 and T012 alone (plan, Exceptions), then passing on
  T013's. Seen failing: CI 46041159126 at 73e0a4e, `docker` job: subtest 1 passed (step containers of
  2 projects 343.5 MiB in all, 181.5 and 162.0 MiB), subtests 2-6 failed (`trials` exit 3: the client
  had no `trials`); every other suite passed
- [X] T029 [P] [US1] Test author: new `src/engine/store/trial_sets_test.go`: a set's record is
  kept in its slice's folder and read back after a restart; `runs` written one by one keeps its
  order; IDs `TS-<n>` come from a sequence that never gives an ID again, also after the slice is
  erased; erasing the slice removes its sets; seen failing before T009
- [X] T013 [US1] (after T005, T009, T011, T012 and T029) Commit (tests and code in separate commits);
  fast lint; ask the owner before pushing (every check job runs); record the CI run here. Done: code d70ee51, 6997707 (T009), 36732bf (T011); tests f5d2cbb, 16737fe, 73e0a4e, 254c9b1 (owner-approved T012 fix: step workers on the parent test's t, found on the owner's VM); helper changes approved (T002). CI 46070202691 at 254c9b1: 24/24 green, TestTrials included
- [X] T014 [US1] Fresh review of US1 judging T013's CI run; findings fixed as new tasks or
  recorded; `/speckit-converge` for US1. Round 1 (fresh Sonnet, read-only, worktree at 254c9b1, CI 46070202691
  green): B1 blocking (a set whose `run-trials` operation fails stays `running` and its trial goes
  unfollowed; Principle X, plan row X, data model States) -> owner 2026-11-28: the set ends `failed`
  (spec Clarifications, ADR 0106) -> T030-T032; S1-S5 suggestions -> T033. Round 2 (same reviewer, B1 and 254c9b1..8d1a5ae): B1 fixed; no blocking
  regression; suggestions R2-1 to R2-3 -> T034. Review converged in 2 rounds. `/speckit-converge` for US1: pass 1 (at 14bd058)
  appended Phase 7 T035-T040, pass 2 (e5b5192) Phase 8 T041-T042, pass 3 (91dd22c) Phase 9 T043;
  all done or closed Analyze round 4 (fresh Sonnet, 254c9b1..f687cd1): gate passed, 0 CRITICAL/HIGH; A1 (no XV exception: the set schemas are new) to A9 fixed at their source (ADR 0106, plan, research R9, data model States, T025, T020, T030-T032)
- [X] T030 [US1] Contract (committed alone): `TrialSetState` gains `failed` ("its operation failed:
  the running trial was cancelled and none started after"), `TrialSet` gains the optional
  `failed_because` (string, the operation's problem detail; required exactly with `failed`, absent
  otherwise: a new `allOf` if/else on `state: failed` as for `stopped`; no `maxLength`, as the
  `Problem.detail` it copies (research R9); `ended_at` with it), the descriptions of `TrialSetState`, `TrialSet` and
  `ended_at` name `failed`; `src/cli/contract.md` "Trial sets": the first line shows
  `state failed  failed <reason>`, and the exit table gains a row `failed` -> 4 with the reason, read
  from the set (not the operation); `contracts/trial-sets.md` exit table and "Changed" list the
  same; regenerate; pinned oasdiff recorded in research R9 (expected: unchanged, the schemas are
  new in this feature, ADR 0106); method identity. Done: oasdiff unchanged (research R9); method 689b3ca9; engine, client and page tests still pass
- [X] T031 [US1] Test author: new `src/engine/api/trial_sets_failed_test.go` (engine rig, as T007):
  a trial whose start is refused for good (its project made non-empty after acceptance) ends the
  set `failed` with `failed_because` naming the refusal, cancels nothing else and starts no further
  trial, and the operation ends `failed`; a job whose retries run out (the durable service down
  past the retry rule) ends the set `failed`, cancels its running trial (the run ends `cancelled`)
  and the result counts it as ended and not done; `getTrialSet` and `listTrialSets` then show
  `failed`, `ended_at`, no `stopped_because`; the slice is free for a new set and `deleteSlice`
  afterwards; a set already `ended`, `stopped` or `cancelled` is never changed by its operation's
  late failure; a crash between the operation's failed end and the set's (the set left `running`)
  is repaired by the next `getTrialSet`, `listTrialSets`, `startTrialSet` or `deleteSlice`, once,
  and a repeated failed end changes nothing; a running trial whose cancel cannot land (durable
  service down) leaves the set `failed` and the trial not ended, and the next read after the
  service is back cancels it; `cancelOperation` on a `failed` set's operation answers as for an
  ended operation and the set stays `failed`; a 24-hour `time` stop racing the failure ends in
  exactly one state, the first recorded; new `src/cli/trial_set_failed_test.go`: `trial-set` of a `failed` set prints
  `state failed  failed <reason>` and exits 4; seen failing before T032
- [X] T032 [US1] Code author (done when T031 passes): in `src/engine/api/` the operation's failed
  end of a `run-trials` operation ends its set `failed` (under the trial-set lock, once) and cancels
  its running trial, and the repair on read, start and delete (data model, States); in `src/cli/`
  the client prints the state and exits 4 with the reason; method identity converged; then commit, fast lint, push (owner
  go-ahead), T014 round 2 on B1 and the changed lines
- [ ] T034 [US1] Round 2 findings: R2-1 (done: fix and test, local) a `failed` set whose operation's problem has an empty
  `detail` is answered without `failed_because`, which the contract's `allOf` requires: write a
  non-empty reason (the problem's code when the detail is empty), with a test by a separate author
  (fix before the feature is done; the contract holds); R2-2 (suggestion) an operation that fails
  before the job wrote its set leaves no set record (`getTrialSet` 404), unlike `cancelTrialSet`,
  which writes it from the operation; R2-3 (suggestion) a failed set's never-started trial is
  cancelled again (NotFound, harmless) on every read
- [ ] T033 [US1] Suggestions of the US1 review round 1 (not blocking; decide each, or close with a
  reason): S1 the pattern is chosen again before each trial (a captured pattern approved mid-set
  changes later trials' pattern; its refusal reads `slice-changed`) and a trial's project appearing
  after acceptance is not re-checked (now ends the set `failed`, T031); the result does not name a
  captured pattern (spec edge case); S2 a registration without a project satisfies every trial
  project (R3's fallback; FR-013 reads "only on the step workers of its own project"), also when the
  project's own grant file is gone; S3 untested: the 2 s wait for a trial's run operation, a stop
  while a recorded run ID never started (its outcome row), a trial interrupted mid-run waiting out
  the 24 h, cancel racing the final end, captured-pattern trials; S4 the 200 ms poll while waiting
  for another run re-reads k records each time; S5 `trial-sets` is in the terminal contract and
  README before US3 builds it; `acceptTrialHandOver` holds the runs lock up to 10 s

---

## Phase 4: User Story 2 - See where the trials differ and how their effort spreads (Priority: P2)

**Goal**: per step and per trial, the lowest, median and highest of each measure, and which steps'
files differ (FR-005, FR-006; research R6).

**Independent Test**: T015 and T016 pass; quickstart 1: a code step needing 1, 1 and 2 attempts
shows `1/1/2`, its files differing in the third trial; a fixed procedure's tokens are `-`.

- [X] T015 [P] [US2] Test author: new `src/engine/trials/measures_test.go` on planted run records:
  per step over the trials that reached it, attempts (`AttemptMade` plus a `StepRejected` with
  `usage` and no `AttemptMade`), review rounds (highest `round`), seconds (the sum of the step's
  attempt spans, `StepStarted` to its `StepAccepted` or `StepRejected`, without interrupted time), tokens and cost (sum of
  `usage`), as `{lowest, median, highest}`; the median of an even count is the mean of the two
  middle values; a measure no such trial carries is absent ("not available"), never 0; per trial
  totals, seconds from `RunStarted` to the end without interrupted time; files `same` when every
  trial that accepted the step has the same output hashes of its last `StepAccepted` or
  `ResultReused`, else the trials grouped by hashes, naming each (SC-003); a step no trial reached
  is listed with `reached 0`; seen failing before T017
- [X] T016 [P] [US2] Test author: new `src/cli/trial_result_test.go`: `trial-set <id>` prints the
  trial rows (`RUN`, `OUTCOME`, `ATTEMPTS`, `ROUNDS`, `SECONDS`, `TOKENS`, `COST`) and the step rows
  (`STEP`, `REACHED`, each measure as `lowest/median/highest` or `-`, `FILES` `same` or the
  groups), aligned as `runs`; seen failing before T017
- [X] T017 [US2] Code author (done when T015 and T016 pass): the measures and files in
  `src/engine/trials/`, served by `getTrialSet`; the rows in `src/cli/`
- [X] T018 [US2] Commit; fast lint; push (owner go-ahead); record the CI run: fast lint 0 issues; CI run 46089956544 of 5afec7b green (26 jobs)
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
  same slice with at least one ended trial (a `cancelled`, `failed` or `running` one, or one with none ended,
  is skipped), with the change in share, `less_reliable`
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
  0106), `docs/apis/engine-api.md`, `src/engine/README.md`, `src/cli/README.md` (`trials`,
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

- T001 → T002 → Phase 2 (T003, T004 in parallel → T005) → US1 (T006-T008 and T029 in parallel → T009;
  T010 → T011; T012; T005, T009, T011 and T012 → T013 → T014) → US2 (T015, T016 in parallel →
  T017 → T018 → T019) → US3 (T020, T021 in parallel → T022 → T023 → T024) → T025 → T026 → T027 →
  T028.
- US2 and US3 extend US1's result and resource; US3's comparison needs US1's sets only, so it may
  run beside US2 when their files do not overlap (`trials/previous.go` against
  `trials/measures.go`).

## Parallel examples

- Phase 2: T003 and T004 by separate test-author sessions.
- US1: T006, T007, T008, T010, T012 and T029 by separate test-author sessions (different files).
- US2: T015 and T016; US3: T020 and T021.

## Implementation strategy

- MVP is US1: the developer can ask for trials and see how often the slice builds done, which is the
  variation N-054 is about.
- US2 tells which step varies and how much; US3 shows whether a change helped.
- No recordings change: the base pattern, its template and agents are unchanged.

---

## Phase 7: Convergence

- [X] T035 CRITICAL: add a contract test (new file, separate test author) that pins the engine's trial limits to the contract's `x-limits` (`trials-max`, `trials-default`, `trial-set-cost-usd`, `trial-set-hours`) and the `minimum`, `maximum` and `default` of `TrialSetRequest.trials`, `TrialSet.trials` and `trial_number` to them, failing on a planted contract copy, and covering `trialLimitsOf`'s refusals (a key missing, zero, a default above the most, a non-numeric cost) so the engine refuses to start; code only if the test fails, per Constitution III (contract test proves they match), tasks Rule "Limits have one home", FR-001 (partial)
- [X] T036 (owner 2026-11-28: reads stay read-only; a read shows a crash-left set `failed` from its failed operation without writing; the repair (record and trial cancel) runs on `startTrialSet`, `deleteSlice` and engine start; T031 read tests change with the owner's approval per test); owner approved 2026-11-28: in `src/engine/api/trial_sets_failed_test.go` A (`TestTSFASetLeftRunningByACrash...` :269, read subtests: read shows `failed`, record stays `running`, nothing cancelled; renamed and split), B (`TestTSFATimeStopRacing...` :333, settled by a start or delete; reads show `failed` throughout), C (`TestTSFAJobWhoseRetriesRunOut...` :398, reads cancel nothing, the next start or delete cancels once; name and header comments), and the rewording of `trial_sets_failed_reason_test.go` :15 and :90's name; new cases in a new file Decide with the owner whether a read of a set may repair it (end a crash-left `running` set `failed`, cancel a failed set's unended trial, appending `RunCancelled`), then amend FR-007 and SC-005 (a read changes no run record) or keep reads read-only and repair only on `startTrialSet`, `deleteSlice` and engine start, and change the data model (States rules 2, 3), T031's tests (owner approval) and the code to match, per FR-007, SC-005, data model States (contradicts)
- [X] T037 Derive the client's usage text "(default 3)" (`src/cli/cli.go:120`) from the embedded contract's `trials-default`, or drop the number, per tasks Rule "Limits have one home" (partial)
- [ ] T038 Make US2's T016 and US3's T021 exercise the client's step table, `spreadCell`, `filesCell` and the "compared with" line already in `src/cli/trials.go`, which no test covers yet, per T016, T017, T021, T022 test-first order (unrequested)
- [X] T039 Add a test (new file) that a worker token is refused on `getTrialSet` and `listTrialSets` and a page token may read them, per Constitution V, the plan's threat model, T007 (partial)
- [X] T040 Correct T030's `maxLength` wording (none, as research R9 says), the contract's `failed_because` description (the detail, or the problem's code when the detail is empty, T034 R2-1; a contract change committed alone) and `quickstart.md` step 2's test pattern (`TestTS`), per Principle IX (partial)

---

## Phase 8: Convergence

- [X] T041 Reword ADR 0106's Decision bullet "any later reader or start ... ends the set the same way" to "a start, a delete or the engine's start ends it the same way; a read shows it failed and writes nothing", per T036, data model States rule 2, FR-007, SC-005, Principle IX (contradicts)
- [X] T042 Add a test that the client's usage shows the contract's `trials-default`, or close it with a reason, per T037, tasks rule "Limits have one home" (partial); closed: the usage line reads `trials-default` from the embedded contract by code, T035 pins that value to the engine and the schema, and the output text is unchanged; a test of the help text would judge wording only

---

## Phase 9: Convergence

- [X] T043 Say in the engine contract's `startTrialSet` and `Operation` descriptions that a `run-trials` operation ends `failed` when its set ends `failed` (a contract change committed alone; oasdiff unchanged), per ADR 0106, data model States, tasks Rule "Contract first" (partial)
