# Tasks: Results are reused only under the method that judged them

**Input**: Design documents from `specs/006-method-record/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Required. Outside-in, test-first: new tests are seen failing before the code; every
new check has a clean and a planted case.

**Organization**: the method identity (Phase 2) blocks every story; then US1 (reuse), US2 (slice
checks), US3 (the record readable through the views).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1, US2 or US3 (spec.md)

## Rules for every task

- **Contract first**: T001 puts the contract in its authoritative places and is committed alone,
  before any test or code.
- **Test-first**: a test task and the code task that makes it pass are separate tasks, done by
  different authors; new tests go in new files. The code author never changes a test, a fixture
  or test data.
- **Existing tests**: none is expected to change (plan). Should one have to, it is asked for,
  quoting what changes and why, before it is briefed, and its commit carries
  `Tests-approved-by: Marius Gologan (<date>)`. New self-tests of the engine are plain Go tests
  (no build tag) unless they need Docker.
- **Protected paths**: `scripts/` and `src/deploy/build.sh` need the owner's `Approved-by:`; no
  dependency is added.
- **Test time**: locally only the new test files and the packages they touch (`go test` of
  `./method/... ./machine/... ./durable/... ./slices/... ./api/... ./cmd/engine/...` in
  `src/engine`, each capped at 10 minutes, after `scripts/check.sh --prepare`), and
  `python3 scripts/testrun.py` on the new Python test files (cap 5 minutes); no full
  `scripts/check.sh`, Docker lab or image build. The full test command is the CI run of the pushed
  commit; each push is told to the owner (which jobs, how long) and waits for a go-ahead.
- **Briefs**: every test-author and reviewer brief says: think through the edge cases and list
  those checked; it gives a time limit and a cap on full runs.
- **Fresh review**: each story and the feature end with a fresh review by a read-only agent on
  another model than the author's, in its own worktree checked out at the branch commit with the
  uncommitted and untracked files, judging the CI run; at most 3 rounds. Then `/speckit-converge`.
- **Gate**: `/speckit-analyze` (fresh reviewer) must report no CRITICAL or HIGH before T001, and
  again after any change to spec, plan, tasks or a design artifact.

---

## Phase 1: Setup (contract first)

- [ ] T001 Contract (committed alone): `src/engine/method/method.yaml` with the declaration of
  [contracts/method-declaration.md](contracts/method-declaration.md) and, in its header comment,
  that contract's rules, the identity's computation and the command table (the draft then links
  to it); `run-event.schema.json` gains optional `method` and `slice.schema.json` optional
  `checks_method` in `src/engine/api/preview/2026-11-12-preview/`, worded as
  [contracts/engine-api-additions.md](contracts/engine-api-additions.md) (draft then a link);
  `.gitignore` gains `src/engine/method/method.sha256`; `scripts/generate.sh` regenerates the API
  code, and `scripts/generate.sh --check` passes

---

## Phase 2: Foundational - the method identity (blocks every story)

**Purpose**: one identity, derived from the declaration, embedded in the engine (FR-001, FR-002,
FR-010; research R1, R2).

**Independent Test**: `test_method_digest.py` and `method_test.go` pass; `--check` fails on a
stale file and passes after the script runs.

- [ ] T002 [P] Test author: new `scripts/tests/test_method_digest.py` for
  `scripts/method-digest.py` on a throwaway fixture Git repository (never the real tree), per
  [contracts/method-declaration.md](contracts/method-declaration.md): the identity is the SHA-256
  of the manifest (recomputed independently in the test from the rules); a second run changes
  nothing (`unchanged`, file bytes and mtime kept); the same tree cloned into another folder gives
  the same identity; a change to an included file changes it; a change to a file matching an
  `exclude` glob (`README.md`, `_test.go`, `testdata/`), to an ignored file or to a path outside
  every `include` leaves it unchanged; an untracked, not ignored included file counts; a pin's
  reference change changes it; `--check` exits 0 when equal and 1 when the file is stale or
  missing, writing nothing; `--list` prints the manifest; exit 2, writing nothing, for each
  planted bad declaration (unknown key, missing key, empty list, duplicate entry, an `include`
  glob that matches nothing, a pin not in `images.lock`) and outside a Git work tree
- [ ] T003 [P] Test author: new `src/engine/method/method_test.go`: `Parse` accepts exactly 64
  lowercase hex digits, trims one trailing newline, and rejects empty, 63 or 65 digits, upper case,
  a non-hex character, inner or leading space, two newlines, each error naming what was found;
  `Current()` returns the parsed embedded identity (equal to `Parse` of the file's bytes)
- [ ] T004 Code author: `scripts/method-digest.py` (T002) and `src/engine/method/method.go`
  (`//go:embed method.sha256`; `ID`, `Parse`, `Current`; T003); T002 and T003 pass
- [ ] T005 Code author: `scripts/check.sh` runs `scripts/method-digest.py` in `--prepare` and
  before its Go steps, and `scripts/method-digest.py --check` among its drift checks;
  `src/deploy/build.sh` runs `scripts/method-digest.py` before it builds the Go images, failing
  the build when it fails (owner `Approved-by:` for both)
- [ ] T006 [P] Test author: new `src/engine/cmd/engine/method_start_test.go`: the engine's setup
  fails, naming `method.sha256` and `scripts/method-digest.py`, when the identity it is given does
  not parse, and passes `method.Current()` to the run machine, the durable activities and the
  slice service (observable through T009, T013 and T016's behaviour, or a setup seam the brief
  names)
- [ ] T007 Code author: `src/engine/cmd/engine/setup.go` takes the identity (T006)
- [ ] T008 Commit T001 to T007; push (owner go-ahead; say which jobs run) and fix until CI is
  green; record the run in research

**Checkpoint**: every engine build carries a true method identity.

---

## Phase 3: User Story 1 - A step's result is reused only under its method (Priority: P1)

**Goal**: FR-003, FR-004, FR-008: the reuse key covers the method; a durable run takes it from
the engine at each lookup.

**Independent Test**: the new tests below: a result accepted under A is reused under A, not under
B, nor when it was recorded without a method; a run resumed under B looks up its remaining steps
under B.

- [ ] T009 [P] [US1] Test author: new `src/engine/machine/reuse_method_test.go`: `KeyParts.Method`
  changes the key; an empty `Method` gives exactly the key without it (the key of today); a key
  with a method differs from every key without one for the same other parts; with
  `Config.Method` A, a run reuses a result accepted under A (`ResultReused`, no worker call) and
  runs a step whose result was accepted under B or recorded without a method; every
  `AttemptMade`, `StepAccepted`, `StepRejected` (open round included) and `ResultReused` carries
  `method` A, and its `reuse_key` equals `ReuseKey` of its own fields; other event kinds carry no
  `method`
- [ ] T010 [P] [US1] Test author: new `src/engine/durable/reuse_method_test.go` (the workflow test
  environment of the existing durable tests, helpers used unchanged): with `Activities.Method` A,
  `LookupReuse` computes every key itself (the workflow passes no key) with A, also when findings
  are open; a result accepted under A is reused, one under B or without a method is not; a run
  interrupted with A and resumed by activities with B looks up its remaining steps under B and its
  later step events name B, the earlier ones A; an open review round kept under A is not reused
  under B; the recorded histories in `src/engine/machine/testdata/histories/` still replay
- [ ] T011 [US1] Code author: `src/engine/machine/reuse.go` (`KeyParts.Method`, written after the
  round only when not empty), `machine.go` (`Event.Method` `json:"method,omitempty"`,
  `Config.Method`), `drive.go`; `src/engine/durable/durable.go`, `activities.go` (`Activities.Method`;
  `Reused.Method`), `workflow.go` (no key computed in the workflow; the step's events take the key
  and method `LookupReuse` returns); T009 and T010 pass, and every existing test of `machine` and
  `durable` passes unchanged
- [ ] T012 [US1] Commit; push (owner go-ahead) and fix until CI is green; fresh review of US1
  (rules above); findings fixed or recorded as tasks; `/speckit-converge` for US1

**Checkpoint**: after an upgrade no step is reused from the old method.

---

## Phase 4: User Story 2 - A slice's check results are judged again under a new method (Priority: P1)

**Goal**: FR-005, FR-006, FR-007, FR-008 (research R4).

**Independent Test**: the new tests below: a slice stored under A and read under B is re-checked
before the answer; read under A it is as stored.

- [ ] T013 [P] [US2] Test author: new `src/engine/slices/checks_method_test.go`: a slice service
  given identity B over a store holding a slice whose record has `checks_method` A (or none): `Get`,
  every operation that reads the slice (`Approve`, `Buildable`, `ReplaceGoals`, `Answer`,
  `ApprovedParts`, `CheckApprove`) and `List` run every check again before they answer
  (part checks, contract compatibility and lint, through the existing fakes), store the record
  with `checks_method` B, and keep goals, rules, examples, terms and contract byte for byte; a part
  whose check now fails loses its approval, recorded as `withdrawn` with that part in `refs`, and
  `Buildable` refuses; a compatibility or lint check that could not run is a failed check with its
  reason and `Buildable` refuses; with `checks_method` B already, nothing is checked again (the
  fakes are not called); a fresh slice created under B stores `checks_method` B; a store write that
  fails during the re-check makes the read fail with `OperationFailed` and changes nothing
- [ ] T014 [P] [US2] Test author: new `src/engine/api/slices_checks_method_test.go`: `getSlice`
  and `listSlices` answer `checks_method` (the engine's identity) on every slice, and the answer
  conforms to `slice.schema.json`; `startRun` on a slice re-checked under a new method whose check
  now fails is refused as for any failing check
- [ ] T015 [US2] Code author: `src/engine/slices/persist.go` (`checks_method` in the record),
  `service.go` (the service's identity; re-check in `find` and `List`), `checks.go` if needed;
  `src/engine/api/slices.go` (the view); T013 and T014 pass, and every existing test of `slices`
  and `api` passes unchanged
- [ ] T016 [US2] Commit; push (owner go-ahead) and fix until CI is green; fresh review of US2;
  findings fixed or recorded; `/speckit-converge` for US2

**Checkpoint**: no slice shows check results of another method.

---

## Phase 5: User Story 3 - Every result names the method that produced it (Priority: P2)

**Goal**: FR-004, FR-009, SC-002, SC-003: the method is readable through the existing run and
slice views.

**Independent Test**: `listRunEvents` of a run shows `method` on every step outcome, conforming to
`run-event.schema.json`.

- [ ] T017 [P] [US3] Test author: new `src/engine/api/run_events_method_test.go`: after a run
  under identity A, `listRunEvents` returns `method` A on every `AttemptMade`, `StepAccepted`,
  `StepRejected` and `ResultReused` and on no other kind, each event conforming to
  `run-event.schema.json`; an event read back from a run record written before feature 006 has no
  `method` and still conforms; two engines given the same identity record the same `method`
- [ ] T018 [US3] Code author: whatever T017 needs in `src/engine/api/runs.go` (the view and the
  record read back); T017 passes
- [ ] T019 [US3] Commit; push (owner go-ahead) and fix until CI is green; fresh review of US3;
  `/speckit-converge` for US3

---

## Phase 6: Polish

- [ ] T020 SC-004: from the CI run's acceptance job, record in research a fully reused run's time
  before (`main`) and after; within 1 s
- [ ] T021 Docs in the same change as the behaviour: the [architecture
  overview](../../docs/architecture/overview.md)'s reuse key (method identity, ADR 0079 and 0080),
  [feature 001 research D8](../001-prose-to-accepted-slice/research.md) gets a line "the reuse key
  also covers the method identity: feature 006, ADR 0080", `docs/log.md` (implementation), the
  problem space (N-050, N-053 state and the T136 sentence), `specs/backlog.md` T136 and T144
  checked with "closed: feature 006"; `python3 scripts/okf.py check` reports OK
- [ ] T022 Final fresh review of the whole feature (rules above) and `/speckit-converge`; findings
  fixed or recorded as tasks
- [ ] T023 Before the owner's merge: read the branch's `evals-current` output (no suite expected
  pending: no prompt, model or agent definition changes); CI green on the last commit

---

## Dependencies

- T001 first. T002, T003 and T006 in parallel after it; T004 after T002 and T003; T005 after T004;
  T007 after T006 and T004; T008 after T005 and T007.
- US1 (T009 to T012), US2 (T013 to T016) and US3 (T017 to T019) each need Phase 2; US3's events
  need T011. US1 and US2 touch different packages and can run side by side.
- T020 to T023 last.

## Parallel examples

- T002, T003, T006: three new test files, three test authors.
- T009 and T010; T013 and T014; and US1's tests beside US2's.

## Implementation strategy

Phase 2 and US1 are the MVP of the feature: with them no stale step is reused. US2 closes T144;
US3 makes the record readable. Each story is pushed and reviewed on its own.
