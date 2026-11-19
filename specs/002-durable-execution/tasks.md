# Tasks: Runs and operations survive a platform stop, on an adopted durable-execution engine

**Input**: Design documents from `specs/002-durable-execution/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/engine-api-changes.md](contracts/engine-api-changes.md),
[quickstart.md](quickstart.md)

**Tests**: Required. Contract-first and Outside-in put the contract and failing tests before code;
every check proves it can fail.

**Organization**: by user story, in priority order (US1 P1, US2 P2, US3 P3); within a story:
failing tests, code, existing-test changes the owner approved, push and CI, fresh review,
converge.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1, US2 or US3 (spec.md)

## Rules for every task

- **Test-first**: a test task and the code task that makes it pass are separate tasks, done by
  different authors; new tests go in new files and are seen failing before the code. The code
  author never changes a test.
- **Existing tests**: the plan lists the existing test files expected to change. No existing
  test, helper, fixture, recording or stub changes without the owner's approval of that specific
  change, asked for by the coordinator before the change is briefed and recorded in the commit
  with `Tests-approved-by:`. The change is made by a test author, never by the code author whose
  code it judges. An agent briefed to change an existing test without that approval refuses and
  reports it. Each story's "existing tests" task proposes each change (what, why, which
  assertion is kept) to the owner first.
- **Protected paths**: `src/deploy/`, `scripts/`, `.github/`, every test file and every new
  dependency need the owner's `Approved-by:`; a new Go module dependency is checked to exist, be
  the intended package and be maintained (constitution XVI) and passes the licence job.
- **Test time**: locally only the new test files and single packages, each run under 5 minutes; no
  `scripts/check.sh` full run, Docker lab, `ap test`, e2e or image build on the host (the owner's
  host is small and shared). Docker work runs in CI or, with the owner's go-ahead, on the owner's
  clean VM. The full test command is the CI run of the pushed commit; the owner approves each
  push after being told which jobs it runs. Paid eval runs only with the owner's go-ahead.
- **Fresh review**: each story ends with a fresh review by a read-only agent on another model than
  the author's, in its own worktree with the uncommitted and untracked files, judging the CI run;
  at most 3 rounds (Review converges). Then `/speckit-converge` for the story.
- **Gate**: `/speckit-analyze` (fresh reviewer) must report no CRITICAL or HIGH before T001, and
  again after any change to spec, plan, tasks or a design artifact. Passed on 2026-11-19 in round 2
  (fresh Sonnet reviewer; round 1: 6 HIGH, 6 MEDIUM, 3 LOW, fixed, FR-017 clarified by the
  owner; round 2: all closed but one LOW, plus one LOW regression, both fixed).

---

## Phase 1: Setup (contract first)

- [ ] T001 Contract: apply `contracts/engine-api-changes.md` to the authoritative copies: `src/engine/api/preview/2026-11-12-preview/openapi.yaml` (`x-limits` `retry-attempts: 3`, `retry-first-wait-ms: 1000`, `retry-longest-wait-ms: 30000`, `retry-jitter-fraction: 0.5`, each with its comment; a `503` response with problem code `Unavailable` and `Retry-After` on every state-changing operation, none on queries; the info text on operation retention, IDs never reused and keys surviving a restart) and `src/cli/contract.md` (the resend waits read from `x-limits`; the launcher's `resume` paragraph and its exit-3 message `resume: the step workers of <slice> for the project <project> are not running; run ap build-workers --resume <run>`; `interrupted`'s meaning). Regenerate `src/engine/api/gen` with the pinned oapi-codegen (`scripts/generate.sh`); `oasdiff` against `main` reports no breaking change; `python3 scripts/okf.py check` and the markdown hook pass. Delete `specs/002-durable-execution/contracts/engine-api-changes.md` and point every link and every prose reference to it or to `contracts/` (in `plan.md`, including its Constitution Check and source tree, `research.md`, `data-model.md`, `quickstart.md` and the header and T001 of `tasks.md`) at the two contracts, then re-run `/speckit-analyze` (an artifact changed). Commit alone, before any code (Contract-first)
- [ ] T002 [P] Pins: add `temporal-server` (`temporalio/server:1.32.2@sha256:7bf69188a6232fea14869b9cef3d94aa82a1601d99517f7e86c71f22da4e0670`), `temporal-admin-tools` (`temporalio/admin-tools:1.32.2@sha256:4a95866efba29d7e17ab012df5232795e8b0ad0f7d5f9d1ec4df321f7792a754`) and `postgres` (`postgres:16-alpine@sha256:1d5b02d3a9941fbd52437db4107f8c52d6297dac553ca1e5c299ebb3756b4857`) to `src/deploy/images.lock`, each with a comment naming research R1 and the rule that the server and admin-tools share one version; re-read each digest from the registry before committing (research S13, S14)

---

## Phase 2: Foundational

**Purpose**: the durable services in Compose and in the test labs, the retry rule, and the store's
write-once events; every story needs them.

- [ ] T003 [P] Test author: new file `scripts/tests/test_compose_durable.py` (`docker compose config` on `src/deploy/compose.yaml`, as `test_compose.py` does): services `durable-store`, `durable-schema` and `durable` exist with the images of T002's pins; each `read_only: true`, `cap_drop: [ALL]`, `no-new-privileges`, a tmpfs `/tmp`, a memory limit of `256m` for `durable` and `durable-store`, no published port; `durable-store` runs as `70:70` with tmpfs `/var/run/postgresql` and the volume `durable-store` at its data folder; networks `durable` (internal; the engine and `durable` only) and `durable-store` (internal; `durable`, `durable-schema` and `durable-store` only), no egress; `durable-schema` depends on `durable-store` healthy, `durable` on `durable-schema` completed, the engine on `durable` healthy; the server's dynamic config sets `frontend.enableServerVersionCheck: false`; and through the launcher's stub (`scripts/tests/fixtures/start/docker-stub`, unchanged): `ap up` makes the PostgreSQL password file mode 600 in the state folder once and keeps it, and `ap down` removes no `durable-store` volume. Show each failing against today's files
- [ ] T004 Code author: `src/deploy/compose.yaml` (the three services, the two networks, the volume, the engine's `depends_on` and its `durable` network), `src/deploy/durable/` (the dynamic config; the schema job's script: `temporal-sql-tool` `create`, `setup-schema -v 0.0` and `update-schema` for `temporal` and `temporal_visibility`, then namespace `ap` with retention 24 h when missing, every step safe to repeat), `src/deploy/ap` (the password file, passed to the two services by Compose, never printed), `src/deploy/README.md`; research R1, data model. Make T003 pass without changing it. The existing real-file checks in `scripts/tests/test_compose.py` are expected to refuse the three new services (images that are none of the platform's own, classified `other`); that change is T006's, not this author's; every other existing launcher and Compose test unchanged
- [ ] T005 [P] Test author: new file `scripts/tests/test_durable_services.py` (real Docker, CI's `py_tests` job; skipped with a reason when Docker is absent; its own Compose project name): with only `durable-store`, `durable-schema` and `durable` up, the schema job exits 0, and exits 0 again when run a second time without changing the schema version; `temporal operator cluster health` (admin-tools image) says `SERVING`; namespace `ap` has retention 24 h; the server runs as uid 1000 and PostgreSQL as uid 70, both with a read-only root; after `stop` and `start` the namespace is kept; idle memory of the two (`docker stats --no-stream`, after 60 s) is at most 512 MiB together (SC-004). Show it failing before T004
- [ ] T006 Existing tests: run `scripts/tests/test_compose.py` and the other existing Compose and launcher tests against T004's files (they run in seconds, `docker compose config` only); for each one that fails, propose to the owner the change (how the three durable services are classified and which rules they must meet: internal networks only, no egress, pinned digest, hardened), with the owner's approval a test author (not T004's author) makes it, `Tests-approved-by:` in the commit
- [ ] T007 [P] Test author: new files `src/retry/retry_test.go`, `src/engine/api/retry_rule_test.go` and `src/cli/retry_rule_test.go`: `Wait(n, r)` for `n` 0, 1, 2 lies in `[1 s, 1.5 s]`, `[2 s, 3 s]`, `[4 s, 6 s]`, never above `1.5 × 30 s`, with the random source's extremes giving the bounds; `Retries` is 3; the engine's rule is the one its embedded contract's `x-limits` gives, and the client's the one the engine contract embedded in its generated code gives, each compared with `openapi.yaml` read in the test; and a planted contract with other `retry-*` values gives the other rule, so no value can live in Go code (research R5, SC-005, data model, Retry rule). Show each failing
- [ ] T008 Code author: new module `src/retry` (no dependencies; `go.work` gains it; its archfile `src/retry/.go-arch-lint.yml` keeps research D40's rules, which `scripts/check.sh`'s `arch` step requires; the images' build contexts already copy `src/`): the rule type and `Wait`; the engine reads the rule from its embedded contract's `x-limits` (`src/engine/api/contract.go`), and the client from the engine contract its generated code embeds (`src/cli/api`, oapi-codegen's embedded spec, regenerated); `src/cli/runs.go`'s resend wait uses it (same waits as today, so `src/cli`'s existing tests pass unchanged). Make T007 pass without changing it
- [ ] T009 [P] Test author: new file `src/engine/store/record_once_test.go`: `RecordEvent` with a new `seq` appends; the same `seq` with the same content succeeds without writing (the file's size unchanged); the same `seq` with other content is refused `SeqTaken`; a `seq` beyond the next one is refused; all after a reopen of the store too (data model, Run record). Show it failing
- [ ] T010 Code author: `src/engine/store`: `RecordEvent` once per `seq` as T009 says; every existing store test unchanged. Make T009 pass
- [ ] T011 Test author, then the owner's approval: the test labs (`src/tests/lab`) start `durable-store`, `durable-schema` and `durable` for each lab, from T002's pins, on the lab's own networks, and the lab's engine joins `durable`. List every existing lab file this changes and ask the owner first; with the approval (`Tests-approved-by:`), make the change and show that today's acceptance and conformance packages still build. Judged by CI's `docker` job

**Checkpoint**: the durable services start and converge; the retry rule has one home; events are
written once.

---

## Phase 3: User Story 1 - A run survives a platform stop (Priority: P1) 🎯 MVP

**Goal**: each run is one Temporal workflow; a stop repeats only the interrupted attempt and the run
continues by itself (FR-001 to FR-007; research R2, R3, R7; data model).

**Independent Test**: stop the engine during an agent step, a procedure step, a review round and a
commit; each run ends as an uninterrupted run of the same slice, byte-identical artifacts, no
accepted step twice, no `ap resume` typed.

### Tests for User Story 1

- [ ] T012 [P] [US1] Test author: new files `src/engine/machine/workflow_*_test.go` with Temporal's `testsuite.WorkflowTestSuite` (activities mocked, time skipping): (a) an uninterrupted run records the events, in order, that feature 001's driver records for the same pattern and fakes (the test states the expected sequence); (b) a heartbeat timeout in the middle of an attempt records `RunInterrupted` with `detail` `the platform stopped`, then `RunResumed` with `detail` `continued after the platform stopped`, restores the project before the repeat, makes the same attempt again with the same `attempt` number, and counts it not as failed; a step whose checks fail 3 times across stops hands over, never a fourth attempt (FR-003); (c) a stop between `StepAccepted` and its commit commits once; (d) a stop during a review round keeps its findings, open findings and scope, and the review still runs at most 3 rounds; (e) tokens, cost and rounds used before a stop still count; the time budget sums the activities' reported running times and ignores timer and signal waits; (f) a hand-over waits for `resume`, `accept-hand-over` or `cancel` and expires 14 days after the hand-over (time skipping), not after a restart; a `resume` after a hand-over starts the resumed step's attempts at 1; (g) a worker call failing `WorkersNotReady` records `RunInterrupted` with `detail` `the step workers are not running` and waits for `workers-ready`, then records `RunResumed` and continues; (h) the model unavailable on every attempt leaves the run `interrupted` until `resume`; (i) `cancel` records `RunCancelled` and ends; (j) a failing restore or commit is retried per `src/retry` with durable sleeps whose random part comes from a side effect, then interrupts the run as feature 001 does; (k) every activity has `MaximumAttempts: 1`, a StartToClose of its call's time limit plus 30 s and a heartbeat timeout of 30 s; (l) every activity's input and result holds no prompt, completion, file content or finding text (FR-016); (m) a stop while a check runs runs the check again, and a check that cannot run still counts as failed (spec Edge Cases); (n) a run at feature 001's limits (the catalogue pattern's steps, 3 attempts per step, 3 review rounds, every check) keeps its history under 10,240 events (research R9). Show each failing (research R3)
- [ ] T013 [P] [US1] Test author: new files in `src/engine/api` and `src/engine/cmd/engine` (the engine's durable interface faked): `startRun` starts workflow `RUN-<n>` on task queue `runs` with `RejectDuplicate`; `resumeRun`, `acceptHandOver`, `cancelRun` and cancelling the run's operation send their signal or cancellation; `registerWorker` sends `workers-ready` to the run waiting for that slice's workers; a run's status and outcome come from its record; at start the engine refuses to start when a run's record has no end event and no workflow exists, naming each such run and the fix (end it with feature 001's version), per research R7 and spec Edge Cases. Show each failing
- [ ] T014 [P] [US1] Test author: new file `src/tests/acceptance/stop_points_test.go` (the recorded model, CI's `docker` job, one lab per trial): for each of four stop points (an agent step, a procedure step, a review round, a version-history commit) the lab's engine container is killed and started again; the run ends with the outcome and byte-identical artifacts of an uninterrupted run of the same slice, no accepted step twice, nobody calling `resumeRun`; 3 trials each (SC-001); the continued run, replayed, gives byte-identical artifacts without a model call (FR-007, US1 AC6); and no row of the lab's `durable-store` holds a prompt, completion or finding text from the recordings (FR-016). Use existing recordings where they fit; if new recordings are needed, say how long their synthesis takes and how many containers, and ask the owner before making them. Show it failing

- [ ] T015 [P] [US1] Test author: new file `src/engine/api/repeated_attempt_test.go` (and the run page's reader if it reads attempts: `src/page`): a record holding an `AttemptMade` twice with the same number, before and after a `RunInterrupted` and `RunResumed` pair, is listed whole by `listRunEvents`, and `compareRuns`, `getRun` and the page take the later one, so a run continued after a stop compares equal to an uninterrupted one (data model, Run record; FR-006, FR-007). If today's code already passes, record that in the task and keep the test

### Implementation for User Story 1

- [ ] T016 [US1] Code author: add `go.temporal.io/sdk` v1.51.0 to `src/engine/go.mod` (check it exists, is Temporal's official module and is maintained; the licence job passes; `Approved-by:`); new package `src/engine/durable` as the data model's sketch names it: client and worker for namespace `ap`, task queues `runs` and `operations`, and the activities that wrap the step workers, the check service, the project worker's restore and commit, the reuse lookup and `RecordEvent`, each reporting its running time and heartbeating every 10 s. Make T012's activity cases (k), (l) pass; validated before T017
- [ ] T017 [US1] Code author: `src/engine/machine`'s rules as the run workflow on T016's activities (deterministic: `workflow.Now`, `workflow.SideEffect`, no I/O; `workflow.GetVersion` ready for later changes). Make every case of T012 and T015 pass without changing them
- [ ] T018 [US1] Code author: `src/engine/api/runs.go`, `src/engine/cmd/engine`: runs through workflows and signals as T013 says; `src/deploy/ap`'s `resume` exit-3 message as the terminal contract (T001) gives it; the engine's worker starts with the engine, connects to `durable` before the engine reports ready; the old driver's resume, record replay into state, restart interruption and `historyBackoff` removed. Make T013 and T014 pass without changing them
- [ ] T019 [US1] Existing tests: list each existing test that T016, T017 and T018 break (the plan's list is the starting point, `scripts/tests/test_build_workers_projects.py`'s resume message included), and for each propose to the owner the change (what, why, which assertion is kept, or why the test is removed because the behaviour it pins is gone); with the owner's approval of each change, a test author (not T016's or T018's author) makes them, `Tests-approved-by:` in the commit
- [ ] T020 [US1] Replay: record the histories of T014's runs from CI as fixtures under `src/engine/machine/testdata/histories/`, and a new test that replays them with `worker.NewWorkflowReplayer` in CI's `go` job; a planted nondeterministic change (a reordered activity) must fail it (Checks prove they can fail)
- [ ] T021 [US1] Push (owner told which jobs run), fix until the CI run is green; fresh read-only review of US1 against the spec, research R2, R3, R7 and the data model; findings fixed or recorded as tasks; then `/speckit-converge` for US1

**Checkpoint**: a run survives a stop and continues by itself.

---

## Phase 4: User Story 2 - An accepted operation is never lost (Priority: P2)

**Goal**: an operation is recorded before its 202 and runs as a workflow with its ID; IDs never
repeat; keys survive a stop (FR-008 to FR-012; research R4, R5, R6; data model).

**Independent Test**: start an operation, stop the engine before it ends, start it again; the
same ID ends succeeded, failed or cancelled; a new operation gets an ID never used before.

### Tests for User Story 2

- [ ] T022 [P] [US2] Test author: new file `src/engine/store/operations_test.go`: an operation record `operations/OP-<n>.json` is written atomically with `id`, `kind`, `target`, `key` (SHA-256 of the caller's token identity and the Idempotency-Key; absent without a key), `body` (SHA-256 of the request body) and `accepted_at` (RFC 3339); `OP-<n>` comes from `operation-sequence.json`, "advanced durably before use; never reissued", across reopens; a record is found by its key hash; a record 24 h after its operation ended is removed; every file mode as the store's others (data model, Operation record). Show it failing
- [ ] T023 [P] [US2] Test author: new files in `src/engine/api` (the durable interface faked, the store real): a state-changing request answers 202 only after its record exists; a failing record write answers 503 `Unavailable` with `Retry-After` and nothing is accepted; with the durable service unreachable every state-changing request answers 503 and every query still answers (R6); after an engine restart an operation is followed by the same ID; the next ID was never used; the same Idempotency-Key and body give the first response after a restart, another body 409; a record whose workflow was never started is started at engine start (`RejectDuplicate`); an operation 24 h after it ended answers 404 `NotFound`; an operation workflow retries a retryable failure (`EraseSlice`) per `src/retry` with retries counted across a stop and ends failed with its reason when they are used up (workflow test). Show each failing
- [ ] T024 [P] [US2] Test author: new file `src/tests/acceptance/operations_restart_test.go` (CI's `docker` job, real durable services): an operation accepted just before the engine is killed is followed after the restart by its ID to exactly one end; IDs across the restart are distinct (SC-002); no `durable-store` row holds a request body (FR-016). Show it failing

### Implementation for User Story 2

- [ ] T025 [US2] Code author: `src/engine/store` operation records and sequence; `src/engine/api/operations.go`: record then workflow (task queue `operations`, ID = operation ID), the outbox at start, status, progress and cancellation from the workflow, the 503 answers, keys from the records; the in-memory queue, `keyStore` and `backoff` removed. Make T022 to T024 pass without changing them
- [ ] T026 [US2] Existing tests: as T019, for the operation, idempotency and retry tests T025 breaks (`skeleton_test.go`'s retry and backoff cases, `operations_wait_test.go`, `runs_ops_test.go`'s restart cases), each change proposed to and approved by the owner first
- [ ] T027 [US2] Push (owner told first), CI green, fresh read-only review of US2, findings fixed or recorded; then `/speckit-converge` for US2

**Checkpoint**: operations survive a stop.

---

## Phase 5: User Story 3 - Nothing the developer relies on changes (Priority: P3)

**Goal**: everything feature 001 delivered keeps working, the contracts show no breaking change,
and ended runs stay readable (FR-013 to FR-017; SC-003, SC-005, SC-006).

**Independent Test**: every check job, acceptance suite, planted suite and eval suite that judged
feature 001 passes on this feature.

- [ ] T028 [P] [US3] Test author: new file `src/tests/acceptance/erasure_durable_test.go` (CI's `docker` job): after a slice with ended runs and operations is deleted, no row naming its run IDs or its other operations' IDs is left in `durable-store`'s `executions`, `current_executions`, `history_node` and `executions_visibility` tables, and no operation record or folder of the slice in the engine store; the erasing operation itself is followed to `succeeded`, its record holds only the slice's ID, and it is gone once its 24 h retention has passed (a shortened retention in the test) (FR-017 as clarified 2026-11-19, SC-006, research R8); and a run recorded by feature 001's store layout that has ended is inspected, compared and replayed as before (FR-015). Show it failing
- [ ] T029 [US3] Code author: `deleteSlice`'s operation deletes each run's workflow and each other targeting operation's workflow and record (`DeleteWorkflowExecution`) before erasing the store; its own record and workflow are left to the retention. Make T028 pass without changing it
- [ ] T030 [US3] Regression: the push's CI run shows every feature 001 job green (planted suite 3 of 3 trials) and `API breaking changes` reporting no breaking change; the regression eval suites run once by dispatch with the owner's go-ahead (a change to agent logic, constitution XIX); then a fresh read-only review of US3 and `/speckit-converge` for US3

---

## Phase 6: Polish & Cross-Cutting Concerns

- [ ] T031 Knowledge bundle and feature 001 artifacts: `docs/architecture/overview.md` (durable execution on Temporal; the run record; operations; the two services and networks; ADRs 0065 to 0069), `docs/operations/local.md` and `src/engine/README.md`, `docs/overview/problem-space.md` (N-040's state), feature 001's plan without its Principle VI and Single-owner exception rows, with a line saying feature 002 removed them (SC-005), backlog T086, T138 and T158 closed as `closed: done by feature 002 (T0xx)`, and T055c amended to say its resend wait moved to `src/retry` (it stays open for its other items), `docs/log.md`; `python3 scripts/okf.py index` and `check` report OK
- [ ] T032 On the owner's clean VM (the owner's go-ahead; over SSH if the owner asks): the quickstart of feature 001 from a fresh clone of this branch reaches an accepted build in under 30 minutes, and the added services' idle memory is at most 512 MiB (SC-004), and the engine's own idle memory with its Temporal worker is recorded beside it (plan, Constraints); `quickstart.md` sections 1 to 3 run by hand; figures recorded in `quickstart.md` and research
- [ ] T033 `/speckit-converge` for the whole feature, then a final fresh read-only review against every FR and SC, judging the CI run of the last pushed commit (Review converges); every finding fixed or recorded; the owner decides the merge to `main`

## Dependencies & Execution Order

- T001 → everything. T002 → T003 to T005, T011. Foundational (T003 to T011) → every story.
  T003 → T004 → T005's judgement and T006; T007 → T008; T009 → T010; T011 needs T004's service
  definitions.
- US1 (T012 to T021) first. US2 (T022 to T027) after T018, since both change `src/engine/api`
  and the engine's start; T022 may be written alongside US1. US3 (T028 to T030) after T025, since
  erasure deletes operation records. Polish (T031 to T033) last.
- Within a story: tests (seen failing) → code → existing-test changes approved by the owner →
  push and CI → fresh review → converge.
- One unit at a time on the host; Docker work in CI or on the VM.

## Parallel Opportunities

- T002 alongside T001's review; T003, T005, T007 and T009 (different new test files).
- T012, T013 and T014 (different new test files); T022 to T024 likewise.
- T028 alongside T025's review.

## Implementation Strategy

- **MVP**: Phases 1 to 3 (US1): a run survives a stop and continues by itself, on Temporal.
- Then US2 makes operations durable, US3 proves nothing regressed and erasure reaches Temporal,
  and Polish records it all and measures it on the VM.
- Each story is pushed and judged by its CI run before the next starts.
