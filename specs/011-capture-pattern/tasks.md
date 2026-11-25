# Tasks: A proven slice becomes a pattern that builds without a model

**Input**: Design documents from `specs/011-capture-pattern/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Required. Outside-in, test-first: new tests are seen failing before the code; the fit
and the proof have clean cases and planted violations (SC-005's planted set).

**Organization**: the contracts (Phase 1), then US1 (capture, parameters, the fill worker, the
proof, approval), US2 (fit, the pattern choice at `startRun`, a build with no model call), US3
(the method rule, withdrawal, erasure, the record). The base pattern, its template, agents and
recordings do not change, so the recorded-model jobs stay green throughout.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1, US2 or US3 (spec.md)

## Rules for every task

- **Contract first**: T001 extends the engine and check contracts additively, as
  [contracts/captured-patterns.md](contracts/captured-patterns.md) states, and is committed alone,
  before any test or code; `scripts/generate.sh` regenerates the server, client and types; the
  generated code is never edited by hand.
- **Test-first**: a test task and the code task that makes it pass are separate tasks, done by
  different authors; new tests go in new files. The code author never changes a test, a fixture
  or test data.
- **Existing tests**: only changes the owner approves by name (T002) are made, each commit
  carrying `Tests-approved-by: Marius Gologan (<date of the approval>)`. Any other existing test
  that needs a change is stopped and asked for, quoting what changes and why.
- **Method identity**: every commit that changes a file `src/engine/method/method.json` lists
  runs `scripts/method-digest.py` and commits the converged `src/engine/method/method.sha256`.
- **Trailers**: `Approved-by: Marius Gologan (2026-11-12)` is the owner's standing approval to
  commit finished, tested work to protected paths.
- **No dependency** is added.
- **Fail closed**: a value outside its form, an unreadable template artifact, a missing proof or
  a proof under another method refuses, naming it; nothing is substituted before every value has
  been parsed into its form (Values that check themselves).
- **One owner**: candidates, case forms, matching, fit and fill live only in
  `src/engine/captured`; the API, the run choice and the workflow call it.
- **Test time**: locally only the packages a task touches (`go test ./captured/ ./api/
  ./durable/ ./checksvc/ ./store/` in `src/engine`, `go test ./...` in `src/cli`), each capped at
  10 minutes; no full `scripts/check.sh`, Docker lab, image build or acceptance suite without the
  owner's go-ahead. The full test command is the CI run of the pushed commit; every push is told
  to the owner with the jobs it runs and waits for a go-ahead. A live eval run is paid and needs
  the owner's go-ahead.
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
  gains `/captured-patterns` (`POST` capture `{run, name?}`, `GET` list paginated with opaque page
  tokens, `GET /{id}`, `PUT /{id}/parameters`, `POST /{id}/proofs`, `POST /{id}/approval`,
  `POST /{id}/withdrawal`, `DELETE /{id}`), each state change a 202 operation taking an
  `Idempotency-Key`; the `CapturedPattern`, `Parameter` (`name` `^[a-z][a-z0-9-]*$`, `kind`
  `name` | `declared-value` | `literal`, `value`, `places`, `origin` `proposed` | `added`),
  `Proof` and `Approval` schemas of [data-model.md](data-model.md); `OperationKind` values for
  each; `startRun` gains an optional `pattern`; its answer `pattern`, `pattern_reason` (`base`,
  `fits`, `chosen`) and the captured patterns that need a new proof; error codes `RunNotDone`,
  `ParameterRefused`, `ProofRequired`, `CapturedPatternWithdrawn`; `x-limits` "at most 32
  parameters, at most 10000 places per parameter"; `run-event.schema.json` `RunStarted` gains
  `pattern_reason`, `parameters` and `proof`, additively. `src/check/api/preview/2026-11-13-preview/openapi.yaml`
  gains `POST /project-versions/write` (`writeStep`: `{project, step, files}`, each path must
  match one of the step's writable entries, otherwise 400 naming it). New
  `src/engine/captured/captured.go` holds the Go interface of the contract draft (`Propose`,
  `Check`, `Fit`, `Fill`, the declaration from the base pattern; types, signatures, doc comments
  stating research R4's rules and limits) with bodies that return a `not implemented` error.
  `scripts/generate.sh` regenerates; `scripts/method-digest.py` converges; the contract lint and
  oasdiff (no breaking change) pass; the draft then links to the authoritative copies.
  `src/cli/contract.md` gains the command rows of the draft
- [ ] T002 Owner approvals: the code author of T001 lists every existing test that fails or no
  longer compiles after T001 (expected: tests that list the contract's paths, operation kinds,
  error codes or `RunStarted`'s fields exactly); ask the owner to approve each by name and record
  each approval here with its date; a change not approved is not made, and the task that needs it
  stops

---

## Phase 2: User Story 1 - Capture a done run as a proven pattern (Priority: P1) 🎯 MVP

**Goal**: a done run becomes a proposed captured pattern with parameters and every place they
occur; the developer edits the parameters; a proof rebuilds the source slice with no model call;
the developer approves only a proven one (FR-001 to FR-007, FR-014; research R1 to R5, R7).

**Independent Test**: T003-T006 pass; quickstart 1 and 2: capturing the evals' notes run proposes
`note`, `notes` and the declared limit with every place (research E1); adding the derived value
works; the proof ends passed with no model request; approval before a passing proof answers
`ProofRequired`.

- [ ] T003 [P] [US1] Test author: new `src/engine/captured/propose_test.go` and
  `src/engine/captured/fill_test.go` over a fixture copied from
  `src/tests/evals/steps/testdata/ref` and `testdata/slice` into
  `src/engine/captured/testdata/notes/`: `Propose` gives the names `note` and `notes` (split at
  case changes from operation and schema names, and schema property names) and the declared value
  `200` as candidates, each with every place (part, line, column, case form), `notes` places
  never counted again for `note` (longest first, never overlapping); `Check` refuses two
  parameters equal in any case form (`ParameterRefused`, naming both), a name value outside
  `[a-z][a-z0-9]*`, a declared or literal value outside "an integer, a decimal, or a string of
  letters, digits, `_`, `-` and spaces", an added value that does not occur in the slice, more
  than 32 parameters, more than 10000 places; a number is matched only as a whole number (`200`
  not in `2001`, `1.200` or `3.200`); `Fill` with each source value gives the input bytes back
  for every file; with `task`/`tasks`, `100`, `101` gives `NewTask`, `listTasks`, `TASK` where
  `NOTE` was, and leaves `note_id`-style spellings as they were (a stated limit). Seen failing
- [ ] T004 [P] [US1] Test author: new `src/engine/checksvc/write_step_test.go`: `writeStep`
  writes files under the step's writable entries only, refuses a path outside them with 400
  naming it (also `..`, absolute paths, a symlinked folder), commits nothing, and `restoreStep`
  afterwards removes what it wrote; an unknown project or step is refused. Seen failing
- [ ] T005 [P] [US1] Test author: new `src/engine/api/captured_capture_test.go` (engine rig, no
  model): `POST /captured-patterns` on a run that ended handed over, expired, cancelled or still
  running fails `RunNotDone` naming the state; on a done run it yields a proposed captured pattern
  `<base id>-<name>` (`pattern_id` `^[a-z0-9-]+$`), version `1.0.0`, naming the source run, slice,
  base pattern and method, holding the source slice's approved parts and the `tests`, `code` and
  `witnesses` artifact hashes; its declaration is valid against `pattern.schema.json`, every agent
  step's worker is `procedure:fill`, the review step lacks FindingsCarryEvidence and
  NoBlockingFindingOpen, everything else equals the base pattern's; `PUT /parameters` removes and
  adds, and refuses as T003 does; list is newest first and paginated; a reused Idempotency-Key
  with another body answers as for other calls. Seen failing
- [ ] T006 [P] [US1] Test author: new `src/engine/durable/fill_proof_test.go` and
  `src/engine/api/captured_proof_test.go` (workflow test environment, step workers and gateway
  faked): a proof is a run of the source slice under the captured pattern with the source values,
  in a new project; each `fill` step renders, calls `writeStep`, runs the step's checks, then
  `commitStep`; no agent worker and no model gateway call is made; the proof passes when the run
  ends done and every filled file's hash equals the accepted file's, and records run, method,
  outcome and failures; a planted template byte change fails it naming the file; a planted
  failing check fails it naming step and check; approval before a passing proof, or after one
  under another method, answers `ProofRequired`; approval records who and when; an approved
  captured pattern refuses `PUT /parameters` with 409; a proof interrupted by an engine restart
  continues and is never marked passed without its recorded end. Seen failing
- [ ] T007 [US1] Code author: `src/engine/captured/` (Propose, Check, Fill, the declaration),
  captured-pattern records and the second artifact holder in `src/engine/store/`, the resource in
  new `src/engine/api/captured.go` with its operations in `src/engine/api/operations.go`, the
  `fill` worker in `src/engine/durable/workflow.go` and `src/engine/steps/` (render through
  `engine/captured`, `writeStep`, checks, `commitStep`; the readiness check skips `fill`),
  `writeStep` in `src/engine/checksvc/project.go` and `handler.go`, the proof as a run with
  `proof: true`; no test is changed. `scripts/method-digest.py` converges
- [ ] T008 [P] [US1] Test author: new `src/cli/captured_test.go`: `ap capture <run> [--name]`,
  `ap patterns`, `ap pattern <id>` (parameters with places, proofs, state, usable), `--drop` and
  `--add`, `ap prove`, `ap approve-pattern` call the contract's operations, follow them and print
  as `src/cli/contract.md` states; errors print their code and reason. Seen failing
- [ ] T009 [US1] Code author: the commands in `src/cli/` (generated client only)
- [ ] T010 [US1] Commit (tests and code in separate commits); fast lint; ask the owner before
  pushing (every check job runs); record the CI run here
- [ ] T011 [US1] Fresh review of US1 judging T010's CI run; findings fixed as new tasks or
  recorded; `/speckit-converge` for US1

---

## Phase 3: User Story 2 - A later slice of the same kind is built with no model (Priority: P2)

**Goal**: a slice equal to a usable captured pattern's source except for its parameters fits it
and is built with no agent step; the newest usable fitting one is chosen unless the run names
another (FR-008 to FR-010; research R4, R6).

**Independent Test**: T012-T014 pass; quickstart 3: the `tasks` slice with a 100-character limit
is built done from the captured notes pattern, the gateway sees no request, the derived tests
pass, three runs give the same files.

- [ ] T012 [P] [US2] Test author: new `src/engine/captured/fit_test.go`: the `tasks`/`100`/`101`
  slice fits with those values; SC-005's planted set does not fit and names the first place it
  differs: another rule, another example, another member, another operation, another status, a
  value outside its form, two holes of one parameter with different values, a value that would
  need `note_id`; a captured pattern with no parameter fits only the identical slice; matching is
  linear (a 1 MiB slice with 32 parameters fits in under 1 s). Seen failing
- [ ] T013 [P] [US2] Test author: new `src/engine/api/captured_startrun_test.go`: `startRun`
  without `pattern` uses the most recently approved usable fitting captured pattern, otherwise the
  base pattern; with `pattern` naming the base pattern or a captured one, that one; a named
  captured pattern that does not fit answers `PatternDoesNotFit` with the first difference; the
  answer and `RunStarted` carry `pattern` (`<id>@<version>`), `pattern_reason` (`base`, `fits`,
  `chosen`) and the parameters' values; a run from a captured pattern makes no agent worker or
  gateway call and ends handed over, naming the failing check, when a filled check fails, with no
  agent step started; the base pattern's fit conditions still run. Seen failing
- [ ] T014 [P] [US2] Test author: new `src/tests/acceptance/captured_test.go` (Compose stack,
  recorded model): build the notes slice from the recorded happy set until done; capture; add the
  derived value; prove; approve; propose and approve the `tasks` slice with a 100-character limit
  through the recorded analyst or a direct slice write; `ap build` ends done; the gateway's access
  log has no request after the capture (SC-001); the run's time is under a quarter of the source
  run's (SC-002); three builds give equal file hashes (SC-004); `--pattern api-feature-go` runs
  the agents. Seen failing (owner go-ahead for the stack)
- [ ] T015 [US2] Code author: `Fit` in `src/engine/captured/`, the choice in
  `src/engine/api/runs.go`, `RunStarted` fields, `ap build --pattern` in `src/cli/`
- [ ] T016 [US2] Commit; fast lint; push (owner go-ahead); record the CI run
- [ ] T017 [US2] Fresh review of US2 judging T016's CI run; `/speckit-converge` for US2

---

## Phase 4: User Story 3 - A captured pattern stays true to its method (Priority: P3)

**Goal**: a captured pattern is used only under the method of its latest passing proof; it can be
withdrawn and erased; runs record it (FR-011 to FR-013; research R3, R5).

**Independent Test**: T018-T019 pass: after a method change a fitting slice's run uses the base
pattern and names the captured pattern as needing a new proof; a new passing proof restores it;
a withdrawn one is never chosen; erasing the source slice keeps the captured pattern usable.

- [ ] T018 [P] [US3] Test author: new `src/engine/api/captured_method_test.go`: with the engine's
  method changed after approval, `startRun` skips the captured pattern and names it as needing a
  new proof, and a named one answers `ProofRequired`; a new passing proof under the new method
  makes it usable and records the method; a failing one leaves it unusable naming what failed;
  withdrawal stops its choice and a named use answers `CapturedPatternWithdrawn`, a run already
  started finishes on it, and earlier runs' records still name it; erasing the source slice keeps
  its template artifacts and its use; erasing the captured pattern removes the artifacts only it
  holds and keeps the run records. Seen failing
- [ ] T019 [P] [US3] Test author: new `src/cli/captured_lifecycle_test.go`: `ap withdraw-pattern`
  and `ap erase-pattern` (asks for confirmation first, does nothing without it). Seen failing
- [ ] T020 [US3] Code author: the usable rule, withdrawal and erasure in `src/engine/api/` and
  `src/engine/store/`; the commands in `src/cli/`
- [ ] T021 [US3] Commit; fast lint; push (owner go-ahead); record the CI run
- [ ] T022 [US3] Fresh review of US3 judging T021's CI run; `/speckit-converge` for US3

---

## Phase 5: Polish & cross-cutting

- [ ] T023 Docs: `docs/architecture/overview.md` (captured patterns, the fill worker, `writeStep`,
  the pattern choice, ADR 0090 to 0094 in its Decisions), `src/catalogue/README.md` (captured
  patterns beside the base patterns), `src/engine/README.md`, `docs/apis/engine-api.md` and
  `docs/apis/check-api.md`, N-052 in `docs/overview/problem-space.md` (state), `docs/log.md`
- [ ] T024 Paid evals for the analyst and steps suites (owner go-ahead; add the branch to
  environment dev's branch policies): the method identity changed; every counted case pass^3
- [ ] T025 Final fresh review of the feature judging the head's CI run and T024; `/speckit-analyze`
  and `/speckit-converge` for the whole feature until clean
- [ ] T026 Merge: `evals-current` on the branch lists no pending suite; the owner fast-forwards
  `main`; remove the branch from environment dev's policies; delete the branch with the owner's OK;
  the spec's status `Implemented`

---

## Dependencies & execution order

- T001 → T002 → US1 (T003-T006 in parallel → T007; T008 → T009 → T010 → T011) → US2 (T012-T014
  in parallel → T015 → T016 → T017) → US3 (T018-T019 in parallel → T020 → T021 → T022) → T023 →
  T024 → T025 → T026.
- US2 needs US1's captured patterns and fill worker; US3 needs US2's choice at `startRun`.

## Parallel examples

- US1: T003, T004, T005 and T006 by separate test-author sessions (different files); T008 after
  T001.
- US2: T012, T013 and T014 by separate test-author sessions.

## Implementation strategy

- MVP is US1: a done run is captured and proven with no model call, which is the deterministic
  asset Principle XX asks for. US2 reuses it for new slices; US3 keeps it true across upgrades.
- No recordings change: the base pattern, its template and agents are unchanged.
