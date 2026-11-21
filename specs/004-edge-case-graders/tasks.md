# Tasks: Eval graders judge edge cases, not only the examples

**Input**: Design documents from `specs/004-edge-case-graders/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [quickstart.md](quickstart.md), the contract draft
[contracts/grader-results.md](contracts/grader-results.md), whose authoritative copy moves to the
step evals' [README](../../src/tests/evals/steps/README.md) in T001

**Tests**: Required. Contract-first and Outside-in put the contract and failing tests before code;
every grader proves it can fail (FR-011).

**Organization**: by user story, in priority order (US1 P1, US2 P1, US3 P2); within a story:
failing tests, code, push and CI, fresh review, converge.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1, US2 or US3 (spec.md)

## Rules for every task

- **Test-first**: a test task and the code task that makes it pass are separate tasks, done by
  different authors; new tests go in new files and are seen failing (or not compiling, for a
  missing name) before the code. The code author never changes a test, a fixture or test data.
- **Existing tests**: the owner approved changes to the existing graders and cases in
  `src/tests/evals/steps` for T180 (2026-11-20); each commit that changes an existing test file,
  helper or fixture there carries `Tests-approved-by: Marius Gologan (2026-11-20, T180)`. Any other
  existing test (for example under `scripts/tests/`) changes only with the owner's approval of that
  specific change, asked for before it is briefed. The design keeps `plantR2` and `plantR3` as
  names, so `grade_test.go` is expected not to change.
- **Protected paths**: `scripts/`, `.github/`, every test file and every agent instruction need the
  owner's `Approved-by:`; no dependency is added.
- **Test time**: locally only the new test files and the step evals' grader self-tests
  (`sh src/tests/evals/steps/run.sh graders`, about 3 to 4 minutes today, cap 10 minutes) and the
  summary's unit tests; no `scripts/check.sh` full run, Docker lab, `ap test` or image build on the
  host. The full test command is the CI run of the pushed commit; the owner approves each push
  after being told which jobs it runs. Paid eval runs only with the owner's go-ahead.
- **Briefs**: every test-author and reviewer brief says: think through the edge cases and list
  those checked.
- **Fresh review**: each story ends with a fresh review by a read-only agent on another model than
  the author's, in its own worktree with the uncommitted and untracked files, judging the CI run;
  at most 3 rounds (Review converges). Then `/speckit-converge` for the story.
- **Gate**: `/speckit-analyze` (fresh reviewer) must report no CRITICAL or HIGH before T001, and
  again after any change to spec, plan, tasks or a design artifact. Round 1 (fresh Sonnet
  reviewer, 2026-11-21, at 889d515): 3 HIGH (C1 shared build cache, C2 run working directory,
  C4 time), 4 MEDIUM, 4 LOW; fixed in research R3 and R4, ADR 0077 and these tasks; C4 (SC-004 now 5 minutes) and C5 (a mutant
  the tests do not build against is not a kill) answered by the owner. Round 2 (fresh Sonnet
  reviewer, at 9945369): all 11 closed, 0 CRITICAL/HIGH; N1 MEDIUM and N3 to N5 LOW fixed; N2
  LOW fixed in the contract and T015 (fail fast is a grading design, not a requirement change);
  N6 LOW closed: ADR 0076 is accepted and is not rewritten, ADR 0077 states that it supersedes
  its mechanics.

---

## Phase 1: Setup (contract first)

- [ ] T001 Contract: move [contracts/grader-results.md](contracts/grader-results.md) into `src/tests/evals/steps/README.md`: a new section "The report" (`counts`: `total` the fixed list (6 held-out tests or 8 mutants), `0 ≤ passed ≤ total`, `failed` the `total − passed` names, sorted, with `(timed out)` or `(did not run)` after a space when that is the reason; `passed` true exactly when `counts` is present and `passed == total`; the summary's columns, `n/a` and `—`, the failure lines, and the unchanged output for a report without counts), two rows in "Graders" (`HeldOutTestsPass` for code, `MutantsKilled` for tests, each naming ADR 0075 or 0076), the held-out tests table and the mutant table from research R1 and R2, and a sentence that no coverage percentage is a gate (FR-014, FR-015). Replace the draft's body with a link to the README; `python3 scripts/okf.py check` and the markdown hook pass. Committed alone, before any test or code

---

## Phase 2: Foundational (the mutant table, the variant runner, counts)

**Purpose**: what both new graders need. No user story starts before this phase is done.

- [ ] T002 [P] Test author: new file `src/tests/evals/steps/mutants_test.go`: the table `Mutants()` has the 8 mutants of research R1, in that order, with unique names `<rule>/<what>`; every `From` occurs exactly once in `testdata/ref/impl/internal/app/app.go`; every rule of `testdata/slice/slice.feature` (R-1, R-2, R-3) has at least one mutant; every `KilledBy` is an example tag of the feature file (E-1 to E-4) under the mutant's rule; `plantR2` and `plantR3` equal the rows `R-2/from-last` and `R-3/first-byte` (same `From`, `To`, rule); the review cases `review/planted-r2` and `review/planted-r3` plant exactly those rows (their `Before` differs from the reference implementation by that one line). Planted violations as table-driven checks on a copy of the table: a duplicate name, a `From` absent from the reference, a rule with no mutant, each fails the table check function (`CheckMutants`). Covers FR-006, FR-007, FR-009. Seen failing (not compiling) before T003
- [ ] T003 Code: new file `src/tests/evals/steps/mutants.go`: `type Mutant struct{ Name, Rule, KilledBy string; plantSpec }` (or equivalent fields), `Mutants()` with research R1's 8 rows, `CheckMutants([]Mutant, ref string, feature string) error`; `plantR2` and `plantR3` in `cases.go` become references to the rows `R-2/from-last` and `R-3/first-byte`, keeping their names (Single owner: each planted bug has one home). T002 and the existing self-tests pass (depends on T002)
- [ ] T004 [P] Test author: new file `src/tests/evals/steps/variants_test.go` with a fake `checks.GoRunner` that records each call (dir, args) and answers from a script: (a) `Variants` builds every variant with `go test -c -vet=off -mod=vendor -o <bins>/<variant>/ ./<pkg>/...` before running any binary, every build call carrying the trial's one build-cache folder and no run call carrying it (research R3, ADR 0077); (b) each built binary is then run with one call of its own, `go tool test2json -t <binary> -test.v=test2json -test.count=1`, whose dir is the binary's package folder inside the scratch copy (`<copy>/tests` or `<copy>/heldout`), never the agent's project, and the agent's project files are unchanged afterwards; (c) a variant's overlay replaces exactly one file and the scratch copy is not the original project; (d) per-test results are read from test2json events by name against the variant's fixed list of expected names: pass, fail, a panic (a `fail` with `panic:` output); an expected name with no event, a run with no events or a runner error is `did not run`, a runner time-out `timed out`; (e) a build failure of one variant is reported for that variant only. A second test with the host runner (the `testEnv` runner of the self-tests) on a tiny module in a temp dir: two variants of one function, one test, one variant passing, one failing, both reported by name. Plus tests of `DockerRunner`'s argument lists (built without running docker): a build call mounts the trial's cache folder writable as `GOCACHE` and the `-o` folder writable; a binary run has `--network none`, `--read-only`, `--cap-drop ALL`, the project root mounted read-only, the binaries' folder mounted read-only, no cache folder, `-w` the package folder, a fresh `--tmpfs /tmp`, one container per call. Seen failing before T005
- [ ] T005 Code: new file `src/tests/evals/steps/variants.go` (scratch copy of a project, `go test -c -vet=off -mod=vendor -overlay=<json> -o <bins>/<variant>/ ./<pkg>/...` per variant in the build phase with the trial's cache folder, one run per binary through `go tool test2json -t <binary> -test.v=test2json -test.count=1` from its package folder, parse events into per-test outcomes against the fixed list) and in `src/tests/evals/steps/sandbox.go` what T004 needs (a field `Cache` naming the trial's build-cache folder, set only on the runner value the build phase uses and mounted writable as `GOCACHE`; a binary run with the project root and the binaries read-only, `-w` the package folder, no cache); the overlay JSON and each mutant's `app.go` are written inside the scratch copy, which every build container mounts read-only; the cache folder is removed after the trial; in `src/tests/evals/steps/grade.go`, `Result.Counts *Counts` with `json:"counts,omitempty"` and `Counts{Total, Passed int; Failed []string}` (`total`, `passed`, `failed`), per the README's "The report". T004 passes; the existing self-tests pass (depends on T001, T004)

**Checkpoint**: the table, the runner and the result shape exist; no grader uses them yet.

---

## Phase 3: User Story 1 - Code fitted to the examples fails its eval (Priority: P1) 🎯 MVP

**Goal**: every code case is judged by `HeldOutTestsPass` (FR-001 to FR-004, FR-010 to FR-013).

**Independent Test**: grader self-tests with no model: the reference implementation passes 6/6;
an implementation fitted to the examples passes every slice test and fails the held-out grader,
naming the tests it failed (SC-001, SC-003).

### Tests for User Story 1

- [ ] T006 [P] [US1] Test author: new file `src/tests/evals/steps/testdata/heldout/heldout_test.go`, package `heldout_test`, importing only the standard library, `example.com/service/gen` and `example.com/service/internal/app`, serving `gen.Handler(app.New())` with `httptest.NewServer`, with exactly research R2's six tests (`TestR1_EmptyList`: 200 and a JSON array with no item, `null` fails; `TestR1_ThreeInOrder`; `TestR2_FiveIDs`; `TestR2_TextKept`: a text with spaces and non-ASCII letters, in the created and the listed note; `TestR3_RefusedNotListed`; `TestR3_RefusalIsAnError`: 400 and a JSON object whose `message` is a string), each a fresh service; `TestR2_TextKept`'s text has inner spaces only, no leading or trailing space; no test whose answer the rules do not fix (FR-002, FR-003). New fixture `src/tests/evals/steps/testdata/planted/fitted/internal/app/app.go`: passes the reference tests but keeps at most two notes and answers `null` for no notes
- [ ] T007 [P] [US1] Test author: new file `src/tests/evals/steps/grade_heldout_test.go`: (a) the reference implementation (`codeGood`) passes `HeldOutTestsPass` with `counts` `{6, 6, []}`; (b) the fitted fixture passes `AllTestsPass` and fails `HeldOutTestsPass`, `counts.failed` naming at least `TestR1_EmptyList` and `TestR1_ThreeInOrder`, sorted; (c) `HeldOutTestsPass` called directly (not through `GradeCode`, whose fail fast would answer `not run: AllTestsPass failed`) on each of a set of single-rule breaks of the reference (the list newest first; ids from 0; empty text accepted) fails naming a held-out test of that rule; (d) code that does not build fails with a reason and no `counts`; (e) `GradeCode` includes `HeldOutTestsPass` for every code case's clean fixture; (f) FR-004: no case's `Before()` files and no `Message` contain the held-out file's path or any held-out test name, and the trial agent container's mounts do not include `testdata` (the test author first moves the mount list out of `lab.agent` in `harness_evals_test.go` into a function `trialMounts(work, agents string) []string` in a new untagged file `src/tests/evals/steps/trial_mounts.go`, used by the harness, commit with `Tests-approved-by:`); (f2) fail fast: a code project that fails `AllTestsPass` (or any earlier grader) gets `HeldOutTestsPass` failed with reason `not run: <grader> failed` and no `counts`; (g) the agent's project folder is byte-identical after grading. Covers FR-001 to FR-004, FR-010 to FR-014, SC-001, SC-003. Seen failing before T008 (depends on T005, T006)

### Implementation for User Story 1

- [ ] T008 [US1] Code: new file `src/tests/evals/steps/grade_heldout.go`: `HeldOutTestsPass` (constant `GHeldOut`) copies the project the agent left, adds `testdata/heldout/heldout_test.go` as `heldout/heldout_test.go`, builds and runs it through `variants.go`, and returns a `Result` with `counts` (`total` the file's six tests) and a reason `<passed> of <total> held-out tests passed; failed: <names>`; `GradeCode` appends it after the existing graders and runs it only when they all passed (`not run: <grader> failed` otherwise; research R3). T007 passes; `sh src/tests/evals/steps/run.sh graders` passes, with its time printed and compared with the last run before this feature (SC-004: at most 5 minutes longer; cap 10 minutes) (depends on T007)
- [ ] T009 [US1] Push (ask the owner, naming the CI jobs it runs; the `go` job runs the grader self-tests): CI green; record the `go` job's grader self-test time before and after in research (new source S9) (depends on T008)
- [ ] T010 [US1] Fresh review of US1 (read-only, another model, own worktree, judging the CI run of T009), against FR-001 to FR-004 and FR-010 to FR-013; findings fixed or recorded; then `/speckit-converge` for US1 (depends on T009)

**Checkpoint**: code cases are judged on held-out tests.

---

## Phase 4: User Story 2 - Tests that check too little fail their eval (Priority: P1)

**Goal**: every tests case is judged by `MutantsKilled` (FR-005 to FR-013).

**Independent Test**: grader self-tests with no model: the reference tests kill 8/8; a weak test
set that binds every example but leaves the second note's id unchecked fails, naming the surviving
mutants (SC-002, SC-003).

### Tests for User Story 2

- [ ] T011 [P] [US2] Test author: new file `src/tests/evals/steps/grade_mutants_test.go`: (a) the reference tests (`testsGood`) pass `MutantsKilled` with `counts` `{8, 8, []}`; (b) a weak test set (FR-005, FR-006, SC-002), the reference tests with the step "the note has the id" made to check nothing, still passes `EveryExampleBound` and `TestsFailAgainstStub` and fails `MutantsKilled`, `counts.failed` naming `R-2/always-one` and `R-2/zero-based`, sorted; (c) tests that fail against the reference implementation (one assertion inverted) fail `MutantsKilled` with a reason saying so and no `counts`, and judge no mutant (FR-008); (d) through `GradeTests`, tests that do not build get `MutantsKilled` failed with `not run: TestsBuild failed` and no `counts`; (e) with the fake runner of T004: a mutant whose run times out is reported `<name> (timed out)` and not killed, a run that cannot start `<name> (did not run)` and not killed, and `counts.total` stays 8; (e2) tests in two packages under `tests/`, one of which fails on a mutant: the mutant is killed; (e3) fail fast: tests that fail `TestsBuild` or `EveryExampleBound` get `MutantsKilled` failed with `not run: <grader> failed` and no `counts`; (f) `GradeTests` includes `MutantsKilled` for every tests case's clean fixture; (g) FR-009: no case's `Before()` files or `Message` contain a mutant's replacement line or name; (h) the agent's project folder is byte-identical after grading. Covers FR-005 to FR-014, SC-002, SC-003. Seen failing before T012 (depends on T003, T005)

### Implementation for User Story 2

- [ ] T012 [US2] Code: new file `src/tests/evals/steps/grade_mutants.go`: `MutantsKilled` (constant `GMutants`) runs only when every earlier grader of the trial passed (`not run: <grader> failed` otherwise), copies the project the agent left, builds every package under `tests/` with the reference implementation and with each mutant of `Mutants()` as an overlay of `internal/app/app.go` (`variants.go`), runs the baseline first and stops with a failing `Result` and no `counts` unless every test passed there, then counts a mutant killed only when a run of one of its binaries reports a failed test or a panic (research R4), `counts.total` always 8; reason `<killed> of <total> mutants killed; survived: <names>`; `GradeTests` appends it after the existing graders. T011 passes; `run.sh graders` passes with its time printed (SC-004) (depends on T011)
- [ ] T013 [US2] Push (ask the owner, naming the jobs): CI green; record the `go` job's grader self-test time in research S9 (depends on T012)
- [ ] T014 [US2] Fresh review of US2 (read-only, another model, own worktree, judging T013's CI run), against FR-005 to FR-013; findings fixed or recorded; then `/speckit-converge` for US2 (depends on T013)

**Checkpoint**: tests cases are judged on mutants.

---

## Phase 5: User Story 3 - The run summary shows what the new graders found (Priority: P2)

**Goal**: the evals summary shows the counts and names (FR-016, FR-017).

**Independent Test**: the summary of fixed reports, with no model.

### Tests for User Story 3

- [ ] T015 [P] [US3] Test author: new file `scripts/tests/test_ci_evals_summary_counts.py` (unittest, as `test_ci_evals_summary_quarantine.py`, running `scripts/ci-evals-summary.py` on temp reports): (a) a report with no `counts` anywhere gives byte for byte the output of today's script for the same report (compared with a stored expected text written by the test author from the README, not by running the new code); (b) a report with a code case of 3 trials (6/6, 4/6, 6/6) and a tests case (8/8 thrice) and a review case gives the two columns `Held-out` and `Mutants killed` with `6/6, 4/6, 6/6`, `n/a`, `8/8, 8/8, 8/8` and `n/a` as the README says; (c) the failure line for trial 2 names its two failed held-out tests; (d) a tests trial whose `MutantsKilled` has no `counts` shows `—` and a failure line with its reason, both for `not run: <grader> failed` and for `the tests do not pass against the reference implementation`; (e) a quarantined case and the tokens line are as today; (f) the exit code is unchanged by the columns: 1 exactly when a counted case failed. Seen failing before T016 (depends on T001)

### Implementation for User Story 3

- [ ] T016 [US3] Code: `scripts/ci-evals-summary.py`: the columns and failure lines per the README's "The report", only when some trial carries `counts`; the docstring updated. T015 and every existing `scripts/tests/test_ci_evals_summary*.py` pass unchanged (depends on T015)
- [ ] T017 [US3] Push (ask the owner, naming the jobs): CI green; fresh review of US3 (read-only, another model, own worktree, judging the CI run) against FR-016 and FR-017; findings fixed or recorded; `/speckit-converge` for US3 (depends on T016)

**Checkpoint**: every story works with no model.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [ ] T018 Live evals run of the steps suite with the owner's go-ahead (allow `004-edge-case-graders` in the GitHub environment `dev` first): the summary shows held-out and mutant results for every tests and code trial (SC-006); record each tests and code trial's grading time against the last green run (SC-005) and the cost in research S9. If a target of SC-004 or SC-005 is missed, the owner decides (research R3) (depends on T010, T014, T017)
- [ ] T019 If T018 shows a tests or code case failing a new grader: propose the change of `src/agents/tests/instruction.md` or `src/agents/code/instruction.md` to the owner (a prompt change, Principle XIX), make it, and re-run the evals with the owner's go-ahead until the cases pass pass^3; no quarantine (FR-019). If nothing failed, check this task `closed: no agent failed a new grader` (depends on T018)
- [ ] T020 [P] Docs: `src/tests/evals/steps/README.md` final (cost and time of a full run with the new graders, from T018); the [problem space](../../docs/overview/problem-space.md) N-013 shows `done` once T018 and T019 are green; `docs/log.md`; backlog T180 checked with this feature's commits; `python3 scripts/okf.py check` OK (depends on T019)
- [ ] T021 Final `/speckit-converge` for the whole feature, then a final fresh read-only review against every FR and SC, judging the CI run of the last pushed commit; findings fixed or recorded; the owner decides the merge to `main` (depends on T020)

---

## Dependencies & Execution Order

- T001 first and alone. Phase 2: T002 and T004 in parallel; T003 after T002, T005 after T001 and
  T004.
- US1 (T006 to T010) and US2 (T011 to T014) both need Phase 2 and are independent of each other;
  do them one after the other to keep pushes simple (US1 first, the MVP). US3 (T015 to T017) needs
  only T001 and can run beside US1 or US2.
- Polish after all three stories.

## Parallel Opportunities

- T002 ∥ T004; T006 ∥ T007 setup and T011 (different files; T007 and T011 run only once Phase 2 is
  done); T015 ∥ any US1 or US2 task.

## Implementation Strategy

1. T001, then Phase 2.
2. US1 (MVP): the held-out grader in CI, reviewed.
3. US2: the mutant grader in CI, reviewed.
4. US3: the summary.
5. One paid live run (owner), any instruction fix it calls for, docs, final review; the owner
   merges.
