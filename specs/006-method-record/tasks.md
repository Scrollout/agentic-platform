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
- **Protected paths**: `scripts/` and `src/engine/.go-arch-lint.yml` need the owner's
  `Approved-by:`; no dependency is added.
- **The identity file**: whoever changes a file the declaration includes runs
  `scripts/method-digest.py` and commits `src/engine/method/method.sha256` with the change.
- **Test time**: locally only the new test files and the packages they touch (`go test` of
  `./method/... ./machine/... ./slices/... ./api/... ./cmd/engine/...` in `src/engine`, each
  capped at 10 minutes, after `scripts/check.sh --prepare`), and `python3 scripts/testrun.py` on
  the new Python test files (cap 5 minutes); no full `scripts/check.sh`, Docker lab or image
  build. The full test command is the CI run of the pushed commit; each push is told to the owner
  (which jobs, how long) and waits for a go-ahead.
- **Briefs**: every test-author and reviewer brief says: think through the edge cases and list
  those checked; it gives a time limit and a cap on full runs.
- **Fresh review**: each story and the feature end with a fresh review by a read-only agent on
  another model than the author's, in its own worktree checked out at the branch commit with the
  uncommitted and untracked files, judging the CI run; at most 3 rounds. Then `/speckit-converge`.
- **Gate**: `/speckit-analyze` (fresh reviewer) must report no CRITICAL or HIGH before T001, and
  again after any change to spec, plan, tasks or a design artifact. Round 1 (fresh Sonnet, at
  4fd7522): 3 CRITICAL, 6 HIGH, 6 MEDIUM, 4 LOW, fixed at their source (research, "Analyze round
  1"; the owner chose the committed identity for F2 and F3). Round 2 (fresh Sonnet, at b58ef1e):
  0 CRITICAL, 1 HIGH (N1), 3 MEDIUM, 2 LOW, fixed (research, "Analyze round 2"). Round 3 (fresh
  Sonnet, at ab929e1): 0 CRITICAL, 0 HIGH, 1 MEDIUM (M1) fixed in wording. Gate passed.

---

## Phase 1: Setup (contract first)

- [X] T001 Contract (committed alone): `src/engine/method/README.md` (the contract of
  [contracts/method-declaration.md](contracts/method-declaration.md): what it does, the
  declaration's rules, the identity's computation, the command table, the Go side) and
  `src/engine/method/method.json` (the declaration as there); the draft then links to them;
  `run-event.schema.json` gains optional `method` and `slice.schema.json` optional
  `checks_method` in `src/engine/api/preview/2026-11-12-preview/`, worded as
  [contracts/engine-api-additions.md](contracts/engine-api-additions.md) (draft then a link);
  `src/engine/.go-arch-lint.yml` gains the component `method` (`src/engine/method`, no
  dependencies) and `method` in `mayDependOn` of `cmd-engine`, `api`, `machine`, `durable` and `slices`
  (owner `Approved-by:`); `scripts/generate.sh` regenerates the API code, and
  `scripts/generate.sh --check` passes

---

## Phase 2: Foundational - the method identity (blocks every story)

**Purpose**: one identity, derived from the declaration, committed, checked and embedded in the
engine (FR-001, FR-002, FR-010; research R1, R2).

**Independent Test**: `test_method_digest.py`, `test_method_identity_current.py` and `method_test.go`
pass; `--check` fails on a stale file and passes after the script runs.

- [X] T002 [P] Test author: new `scripts/tests/test_method_digest.py` for
  `scripts/method-digest.py` on a throwaway fixture Git repository (never the real tree), per
  [contracts/method-declaration.md](contracts/method-declaration.md): the identity is the SHA-256
  of the manifest (recomputed independently in the test from the rules); a second run changes
  nothing (`unchanged`, file bytes and mtime kept); the same tree cloned into another folder gives
  the same identity; a change to an included file changes it; a change to a file matching an
  `exclude` glob (`README.md` at the top and in a folder, `_test.go`, `testdata/`, the identity
  file itself), to an ignored file or to a path outside every `include` leaves it unchanged, while
  a file named `NOTREADME.md` is included (the exclusion names `README.md` only); matching is
  case-sensitive (`readme.md` is included); an untracked, not ignored included file counts; a
  tracked file deleted from the working tree is left out; a listed pin's reference change changes
  it, a change to a pin the declaration does not name leaves it unchanged; `--check` exits 0 when
  equal and 1 when the file is stale or missing, writing nothing, and its message names both
  values and the script; `--list` prints the manifest; exit 2, writing nothing, for each planted
  bad declaration (invalid JSON, unknown key, missing key, empty list, duplicate entry, a non-string
  entry, an `include` glob that matches nothing, a pin not in `images.lock`), for a symbolic link
  among the selected files, and outside a Git work tree
- [X] T003 [P] Test author: new `scripts/tests/test_method_identity_current.py`: runs
  `python3 scripts/method-digest.py --check` on the repository itself and fails, quoting the
  script's output, unless it exits 0 (research R2); its planted counterpart is T002's stale-file
  case, and the test names it in its docstring; it reports the identity it examined
- [X] T004 [P] Test author: new `src/engine/method/method_test.go`: `Parse` accepts exactly 64
  lowercase hex digits, trims one trailing newline, and rejects empty, 63 or 65 digits, upper case,
  a non-hex character, inner or leading space, two newlines, each error naming what was found;
  `Current()` returns the parsed embedded identity (equal to `Parse` of the file's bytes)
- [X] T005 Code author: `scripts/method-digest.py` (T002); `src/engine/method/method.go`
  (`//go:embed method.sha256`; `ID`, `Parse`, `Current`; T004); `src/engine/method/method.sha256`
  written by the script and committed (owner `Approved-by:` for the script); T002 to T004 pass
- [X] T006 [P] Test author: new `src/engine/cmd/engine/method_start_test.go`: the engine's setup
  fails, naming `src/engine/method/method.sha256` and `scripts/method-digest.py`, when the identity
  it is given does not parse, and gives the durable activities and the slice service the identity
  it was given (production builds no non-durable machine; its `Config.Method` is judged by T009) (through a setup seam the brief names, e.g. the identity as a
  setup parameter, `method.Current()` by default)
- [X] T007 Code author: `src/engine/cmd/engine/setup.go` takes the identity (T006); T006 passes
- [X] T008 Commit T001 to T007; push (owner go-ahead; say which jobs run) and fix until CI is
  green; record the run in research

**Checkpoint**: every engine build carries a true method identity.

---

## Phase 3: User Story 1 - A step's result is reused only under its method (Priority: P1)

**Goal**: FR-003, FR-004, FR-008: the reuse key covers the method; a durable run takes it from
the engine at each lookup.

**Independent Test**: the new tests below: a result accepted under A is reused under A, not under
B, nor when it was recorded without a method; a run resumed under B looks up its remaining steps
under B.

- [X] T009 [P] [US1] Test author: new `src/engine/machine/reuse_method_test.go`: `KeyParts.Method`
  changes the key; an empty `Method` gives exactly the key without it (the key of today); a key
  with a method differs from every key without one for the same other parts, round or not; with
  `Config.Method` A, the non-durable machine reuses a result accepted under A (`ResultReused`, no
  worker call) and runs a step whose result was accepted under B or recorded without a method;
  every `AttemptMade`, `StepAccepted`, open-round `StepRejected` and `ResultReused` carries
  `method` A, and its `reuse_key` equals `ReuseKey` of its own fields; no other event kind carries
  `method`
- [X] T010 [P] [US1] Test author: new `src/engine/machine/workflow_reuse_method_test.go` (package
  `machine_test`, beside the existing workflow tests; their helpers `newWF`, `wfRegister`,
  `wfValidate` used unchanged; a lookup registered with the real `durable.Activities` and an
  in-memory result store): with `Activities.Method` A, `LookupReuse` computes the key itself from
  the parts with A (whatever key the workflow passes), also with open findings and changed lines,
  and returns `Method` A; the workflow records the returned key, inputs and method on the step's
  events, which conform to `run-event.schema.json`; a result accepted under A is reused, one under
  B or without a method is not; an open review round kept under A is not reused under B; a run
  interrupted with A and resumed by activities with B looks up its remaining steps under B and its
  later events name B, the earlier ones A; a lookup that fails leaves the step's method-less key
  and no `method` on its events, and its result is not reused by a later lookup under A; with
  `Activities.Method` empty every key is today's
- [X] T011 [US1] Code author: `src/engine/machine/reuse.go` (`KeyParts.Method`, written after the
  round only when not empty), `machine.go` (`Event.Method` `json:"method,omitempty"`,
  `Config.Method`), `drive.go`; `src/engine/durable/durable.go`, `activities.go`
  (`Activities.Method`; `Reused.Method`; the key from the parts with the method when it is set),
  `workflow.go` (the lookup's `Key`, `Inputs` and `Method` taken whenever its `Key` is not empty;
  no activity call, order or condition changes); T009 and T010 pass, every existing test of
  `machine` and `durable` passes unchanged, and the recorded histories replay
- [X] T012 [US1] Commit; push (owner go-ahead) and fix until CI is green; fresh review of US1
  (rules above); findings fixed or recorded as tasks; `/speckit-converge` for US1

- [ ] T024 [US1] Review finding US1-S1 (fresh Sonnet review at 21dcc6b, suggestion): a selected
  file or the identity file that cannot be read (permission, vanished, a folder) makes
  `scripts/method-digest.py` exit 1 with a traceback, which `--check` cannot tell from a stale
  identity; refuse it with exit 2 like a bad declaration (contract line, a new planted test,
  then the code). Non-blocking; outside the MVP unless the owner pulls it in
- [X] T025 [US1] Review finding US1-S2 (suggestion: no test that an include glob matching only
  excluded files is accepted) closed: covered by `test_method_digest.py`
  `test_a_changed_declaration_changes_it` (its `docs/*` include matches only excluded files after
  the change, and the run must succeed)
- [X] T026 [US1] Review finding US1-S3 (suggestion: a local `--check` counts untracked, not
  ignored files) closed: the contract's rule (research R1, S8), what a Docker build of `src/` sees

**Checkpoint**: after an upgrade no step is reused from the old method.

---

## Phase 4: User Story 2 - A slice's check results are judged again under a new method (Priority: P1)

**Goal**: FR-005, FR-006, FR-007, FR-008 (research R4).

**Independent Test**: the new tests below: a slice stored under A and read under B is re-checked
before the answer; read under A it is as stored.

- [X] T013 [P] [US2] Test author: new `src/engine/slices/checks_method_test.go`: a slice service
  given identity B over a store holding a slice whose record has `checks_method` A (or none): `Get`,
  every operation that reads the slice (`Approve`, `Buildable`, `ReplaceGoals`, `Answer`,
  `ApprovedParts`, `CheckApprove`), `List`, and `Create` with the description of an approved stale
  slice run every check again before they answer (part checks, contract compatibility and lint,
  through the existing fakes), store the record with `checks_method` B, and keep goals, rules,
  examples, terms and contract byte for byte; a part whose check now fails loses its approval,
  recorded as `withdrawn` with that part in `refs`, `Buildable` refuses, and `Create` does not
  offer that slice as approved; a compatibility or lint check that could not run is a failed check
  with its reason, `Buildable` refuses, the record keeps no `checks_method`, and the next read
  checks again; with `checks_method` B already, nothing is checked again (the fakes are not
  called); a fresh slice created under B stores `checks_method` B; a store write that fails during
  the re-check leaves the record stale, the answer shows the new results, and the next read checks
  again; a slow lint during one slice's re-check does not hold a read of another slice (the
  re-check runs outside the service's lock); two reads of one stale slice at once run its checks
  once; an `Approve` or `Answer` that lands during a re-check (between its start and its store) is
  kept, and the slice's results are current afterwards (research R4 "How"); a service without an
  identity re-checks nothing
- [X] T014 [P] [US2] Test author: new `src/engine/api/slices_checks_method_test.go`: `getSlice`
  answers `checks_method` (the engine's identity) when every check ran and none while one could
  not run, and the answer conforms to `slice.schema.json`; `listSlices`' summaries show the state
  that follows the re-check; `startRun` on a slice re-checked under a new method whose check now
  fails is refused as for any failing check
- [X] T015 [US2] Code author: `src/engine/slices/persist.go` (`checks_method` in the record),
  `service.go` (the service's identity; the re-check before every read of a slice, outside the
  lock, under the slice's `changing` mutex, as research R4 "How" says; `find` keeps its contract), `checks.go` (whether each check ran); `src/engine/api/slices.go`
  (the view); T013 and T014 pass, and every existing test of `slices` and `api` passes unchanged
- [ ] T016 [US2] Commit; push (owner go-ahead) and fix until CI is green; fresh review of US2;
  findings fixed or recorded; `/speckit-converge` for US2

**Checkpoint**: no slice shows check results of another method.

---

## Phase 5: User Story 3 - Every result names the method that produced it (Priority: P2)

**Goal**: FR-004, FR-009, SC-002, SC-003: the method is readable through the existing run and
slice views.

**Independent Test**: `listRunEvents` of a run shows `method` on every event that carries a reuse key, conforming to
`run-event.schema.json`.

- [X] T017 [P] [US3] Test author: new `src/engine/api/run_events_method_test.go`: after a run
  under identity A, `listRunEvents` returns `method` A on every `AttemptMade`, `StepAccepted`,
  `ResultReused` and open-round `StepRejected` and on no other kind, each event conforming to
  `run-event.schema.json`; an event read back from a run record written before feature 006 has no
  `method` and still conforms; two engines given the same identity record the same `method`
- [X] T018 [US3] Code author: whatever T017 needs in `src/engine/api/runs.go` (the view and the
  record read back); T017 passes. Nothing needed: the generated run-event type carries `method`
  and `contractEvent` keeps it; T017 passed at once and failed with a planted omission of
  `AttemptMade`'s method (3 of its 4 tests)
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

- T001 first. T002, T003, T004 and T006 in parallel after it; T005 after T002 to T004; T007
  after T006 and T005; T008 after T007.
- US1 (T009 to T012), US2 (T013 to T016) and US3 (T017 to T019) each need Phase 2; US3's events
  need T011. US1 and US2 touch different packages and can run side by side.
- T020 to T023 last.

## Parallel examples

- T002, T003, T004, T006: four new test files, test authors working in parallel.
- T009 and T010; T013 and T014; and US1's tests beside US2's.

## Implementation strategy

Phase 2 and US1 are the MVP of the feature: with them no stale step is reused. US2 closes T144;
US3 makes the record readable. Each story is pushed and reviewed on its own.
