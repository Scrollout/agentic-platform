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
  its mechanics. Round 3 (fresh Sonnet reviewer, at e6a924a, the fixed lines only): all closed,
  0 CRITICAL/HIGH; one LOW (T005 removes the binaries folder too) fixed. Gate passed. Round 4 (fresh Sonnet reviewer, 2026-11-21, on research R3's random binary folders, found
  by the T005 author): 0 CRITICAL/HIGH; F1 (plan trust table), F2 (T004/T005 `<bins>/<random>/`)
  and F3 (a run mounts only its own variant's folder) MEDIUM fixed; F4 and F5 LOW accepted in R3;
  F6 and F7 LOW closed: the research records it, ADR 0077 is not rewritten. Round 5 (fresh Sonnet,
  at eccdb21, the fixed lines only): 0 CRITICAL/HIGH; F3's leftover (R3's `<bins>/<variant>/`)
  and N1 LOW ("that variant's binaries folder") fixed in R3. Round 6 (2026-11-21, T033's clarification): 0 CRITICAL/HIGH, MEDIUMs fixed. Round 7 (the owner's decisions on T022 to T031, SC-004 in CI): 0 CRITICAL/HIGH; 3 MEDIUM (the hang worst case per test package, SC-005 scoped to trials that do not hang, T022's limit in `Variants`' doc) and 3 LOW fixed. Round 8 (the fixed lines): 0 CRITICAL/HIGH; one LOW accepted (the 9 builds' own 10-minute limits are not added to the 90 minutes, since a build runs no test)

---

## Phase 1: Setup (contract first)

- [X] T001 Contract: move [contracts/grader-results.md](contracts/grader-results.md) into `src/tests/evals/steps/README.md`: a new section "The report" (`counts`: `total` the fixed list (6 held-out tests or 8 mutants), `0 ≤ passed ≤ total`, `failed` the `total − passed` names, sorted, with `(timed out)` or `(did not run)` after a space when that is the reason; `passed` true exactly when `counts` is present and `passed == total`; the summary's columns, `n/a` and `—`, the failure lines, and the unchanged output for a report without counts), two rows in "Graders" (`HeldOutTestsPass` for code, `MutantsKilled` for tests, each naming ADR 0075 or 0076), the held-out tests table and the mutant table from research R1 and R2, and a sentence that no coverage percentage is a gate (FR-014, FR-015). Replace the draft's body with a link to the README; `python3 scripts/okf.py check` and the markdown hook pass. Committed alone, before any test or code

---

## Phase 2: Foundational (the mutant table, the variant runner, counts)

**Purpose**: what both new graders need. No user story starts before this phase is done.

- [X] T002 [P] Test author: new file `src/tests/evals/steps/mutants_test.go`: the table `Mutants()` has the 8 mutants of research R1, in that order, with unique names `<rule>/<what>`; every `From` occurs exactly once in `testdata/ref/impl/internal/app/app.go`; every rule of `testdata/slice/slice.feature` (R-1, R-2, R-3) has at least one mutant; every `KilledBy` is an example tag of the feature file (E-1 to E-4) under the mutant's rule; `plantR2` and `plantR3` equal the rows `R-2/from-last` and `R-3/first-byte` (same `From`, `To`, rule); the review cases `review/planted-r2` and `review/planted-r3` plant exactly those rows (their `Before` differs from the reference implementation by that one line). Planted violations as table-driven checks on a copy of the table: a duplicate name, a `From` absent from the reference, a rule with no mutant, each fails the table check function (`CheckMutants`). Covers FR-006, FR-007, FR-009. Seen failing (not compiling) before T003
- [X] T003 Code: new file `src/tests/evals/steps/mutants.go`: `type Mutant struct{ Name, Rule, KilledBy string; plantSpec }` (or equivalent fields), `Mutants()` with research R1's 8 rows, `CheckMutants([]Mutant, ref string, feature string) error`; `plantR2` and `plantR3` in `cases.go` become references to the rows `R-2/from-last` and `R-3/first-byte`, keeping their names (Single owner: each planted bug has one home). T002 and the existing self-tests pass (depends on T002)
- [X] T004 [P] Test author: new file `src/tests/evals/steps/variants_test.go` with a fake `checks.GoRunner` that records each call (dir, args) and answers from a script: (a) `Variants` builds every variant with `go test -c -vet=off -mod=vendor -o <bins>/<random>/ ./<pkg>/...` before running any binary, every build call carrying the trial's one build-cache folder and no run call carrying it (research R3, ADR 0077); (b) each built binary is then run with one call of its own, `go tool test2json -t <binary> -test.v=test2json -test.count=1`, whose dir is the binary's package folder inside the scratch copy (`<copy>/tests` or `<copy>/heldout`), never the agent's project, and the agent's project files are unchanged afterwards; (c) a variant's overlay replaces exactly one file and the scratch copy is not the original project; (d) per-test results are read from test2json events by name against the variant's fixed list of expected names: pass, fail, a panic (a `fail` with `panic:` output); an expected name with no event, a run with no events or a runner error is `did not run`, a runner time-out `timed out`; (e) a build failure of one variant is reported for that variant only. A second test with the host runner (the `testEnv` runner of the self-tests) on a tiny module in a temp dir: two variants of one function, one test, one variant passing, one failing, both reported by name. Plus tests of `DockerRunner`'s argument lists (built without running docker): a build call mounts the trial's cache folder writable as `GOCACHE` and the `-o` folder writable; a binary run has `--network none`, `--read-only`, `--cap-drop ALL`, the project root mounted read-only, the binaries' folder mounted read-only, no cache folder, `-w` the package folder, a fresh `--tmpfs /tmp`, one container per call. Seen failing before T005
- [X] T005 Code: new file `src/tests/evals/steps/variants.go` (scratch copy of a project, `go test -c -vet=off -mod=vendor -overlay=<json> -o <bins>/<random>/ ./<pkg>/...` per variant in the build phase with the trial's cache folder, one run per binary through `go tool test2json -t <binary> -test.v=test2json -test.count=1` from its package folder, parse events into per-test outcomes against the fixed list) and in `src/tests/evals/steps/sandbox.go` what T004 needs (a field `Cache` naming the trial's build-cache folder, set only on the runner value the build phase uses and mounted writable as `GOCACHE`; a binary run with the project root and the binaries read-only, `-w` the package folder, no cache); the overlay JSON and each mutant's `app.go` are written inside the scratch copy, which every build container mounts read-only; the cache and binaries folders are removed after the trial; in `src/tests/evals/steps/grade.go`, `Result.Counts *Counts` with `json:"counts,omitempty"` and `Counts{Total, Passed int; Failed []string}` (`total`, `passed`, `failed`), per the README's "The report". T004 passes; the existing self-tests pass (depends on T001, T004)

**Checkpoint**: the table, the runner and the result shape exist; no grader uses them yet.

---

## Phase 3: User Story 1 - Code fitted to the examples fails its eval (Priority: P1) 🎯 MVP

**Goal**: every code case is judged by `HeldOutTestsPass` (FR-001 to FR-004, FR-010 to FR-013).

**Independent Test**: grader self-tests with no model: the reference implementation passes 6/6;
an implementation fitted to the examples passes every slice test and fails the held-out grader,
naming the tests it failed (SC-001, SC-003).

### Tests for User Story 1

- [X] T006 [P] [US1] Test author: new file `src/tests/evals/steps/testdata/heldout/heldout_test.go`, package `heldout_test`, importing only the standard library, `example.com/service/gen` and `example.com/service/internal/app`, serving `gen.Handler(app.New())` with `httptest.NewServer`, with exactly research R2's six tests (`TestR1_EmptyList`: 200 and a JSON array with no item, `null` fails; `TestR1_ThreeInOrder`; `TestR2_FiveIDs`; `TestR2_TextKept`: a text with spaces and non-ASCII letters, in the created and the listed note; `TestR3_RefusedNotListed`; `TestR3_RefusalIsAnError`: 400 and a JSON object whose `message` is a string), each a fresh service; `TestR2_TextKept`'s text has inner spaces only, no leading or trailing space; no test whose answer the rules do not fix (FR-002, FR-003). New fixture `src/tests/evals/steps/testdata/planted/fitted/internal/app/app.go`: passes the reference tests but keeps at most two notes and answers `null` for no notes
- [X] T007 [P] [US1] Test author: new file `src/tests/evals/steps/grade_heldout_test.go`: (a) the reference implementation (`codeGood`) passes `HeldOutTestsPass` with `counts` `{6, 6, []}`; (b) the fitted fixture passes `AllTestsPass` and fails `HeldOutTestsPass`, `counts.failed` naming at least `TestR1_EmptyList` and `TestR1_ThreeInOrder`, sorted; (c) `HeldOutTestsPass` called directly (not through `GradeCode`, whose fail fast would answer `not run: AllTestsPass failed`) on each of a set of single-rule breaks of the reference (the list newest first; ids from 0; empty text accepted) fails naming a held-out test of that rule; (d) code that does not build fails with a reason and no `counts`; (e) `GradeCode` includes `HeldOutTestsPass` for every code case's clean fixture; (f) FR-004: no case's `Before()` files and no `Message` contain the held-out file's path or any held-out test name, and the trial agent container's mounts do not include `testdata` (the test author first moves the mount list out of `lab.agent` in `harness_evals_test.go` into a function `trialMounts(work, agents string) []string` in a new untagged file `src/tests/evals/steps/trial_mounts.go`, used by the harness, commit with `Tests-approved-by:`); (f2) fail fast: a code project that fails `AllTestsPass` (or any earlier grader) gets `HeldOutTestsPass` failed with reason `not run: <grader> failed` and no `counts`; (g) the agent's project folder is byte-identical after grading. Covers FR-001 to FR-004, FR-010 to FR-014, SC-001, SC-003. Seen failing before T008 (depends on T005, T006)

### Implementation for User Story 1

- [X] T008 [US1] Code: new file `src/tests/evals/steps/grade_heldout.go`: `HeldOutTestsPass` (constant `GHeldOut`) copies the project the agent left, adds `testdata/heldout/heldout_test.go` as `heldout/heldout_test.go`, builds and runs it through `variants.go`, and returns a `Result` with `counts` (`total` the file's six tests) and a reason `<passed> of <total> held-out tests passed; failed: <names>`; `GradeCode` appends it after the existing graders and runs it only when they all passed (`not run: <grader> failed` otherwise; research R3). T007 passes; `sh src/tests/evals/steps/run.sh graders` passes, with its time printed and compared with the last run before this feature (SC-004: at most 5 minutes longer; cap 10 minutes) (depends on T007). Done 2026-11-21: `TestGraderHeldOut*` 96 s; full `run.sh graders` 8 min 51 s against 3 min 44 s before (+5 min 07 s, 7 s over SC-004, on a host at load average 13 to 15 of 16 CPUs); measured again in CI at T009, the owner decides if it stays over (research R3). A probe: `-trimpath` would let the host build cache serve each scratch copy (1.85 s to 0.76 s per build), a change of R3 and of `variants_test.go`, not made
- [X] T009 [US1] Push (ask the owner, naming the CI jobs it runs; the `go` job runs the grader self-tests): CI green; record the `go` job's grader self-test time before and after in research (new source S9) (depends on T008). Done: run 44913952612 (`5f2732f`) green 26/26 after one fix of a self-test (repository at `/` in CI); `evals/steps` 146 s before, 160 s and 122 s after (S9)
- [X] T010 [US1] Fresh review of US1 (read-only, another model, own worktree, judging the CI run of T009), against FR-001 to FR-004 and FR-010 to FR-013; findings fixed or recorded; then `/speckit-converge` for US1 (depends on T009). Done 2026-11-21: round 1 (fresh Sonnet, read-only, its worktree came from `main`, so it reviewed and ran an archive of 10a8def; later briefs name the commit to check out) found no blocking finding; five suggestions recorded as T022 to T025; `/speckit-converge` for US1: converged, nothing beyond T022 to T025

### Findings of the US1 fresh review (T010 round 1, fresh Sonnet reviewer, 2026-11-21, at 10a8def, judging CI run 44913952612: no blocking finding)

- [X] T022 [US1] Finding H1 (suggestion): `VariantRun.Variants` writes `Add` files and overlays with `os.WriteFile`, which follows a symbolic link the agent left in the scratch copy (`heldout/heldout_test.go` or `heldout` as a link to a host path), so a direct caller could write on the host; through `GradeCode` and `GradeTests` the scope grader fails first. Test author: a new test that a link at an `Add` or overlay path is not followed (the target is unchanged, the variant fails or the link is replaced); code: write them through the copy's `os.Root`, as `copyTree` does. 2026-11-21, the owner on the coordinator's recommendation; the limit is stated in `Variants`' doc comment; closed: through `GradeCode` and `GradeTests` the scope grader fails first, so only a direct caller in a test reaches it
- [X] T023 [US1] Finding H3 (suggestion): `TestR3_RefusalIsAnError` accepts `{"message":null}`, which the contract's `message: string` refuses; check the raw value is a JSON string (a change of the held-out test, under T180's approval, with `Tests-approved-by:`), and a planted implementation answering `null` fails it. Done 2026-11-21 (fresh Sonnet test author): the raw value must be a JSON string; new fixture `testdata/planted/null-message` and self-test `TestGraderHeldOutNullMessage` (the reference 6/6; the fixture fails exactly `TestR3_RefusalIsAnError`); `TestGraderHeldOut*` 34 s
- [X] T024 [US1] Findings H4 and H5 (suggestions, cases left untested): a self-test that `Env{Runner: DockerRunner{...}}` gives `HeldOutTestsPass` the runner's `For` (built without running docker), and one that a time-out through `HeldOutTestsPass` (fake runner) names every held-out test with the suffix `(timed out)` and `counts` present. 2026-11-21, the owner; closed: moved to the backlog as T190
- [X] T025 [US1] Finding H2 (suggestion): a test binary's results come only from its test2json events, so code the agent writes (an `init` in `internal/app`) can print forged events and exit; the same holds for every grader that runs the agent's code (`AllTestsPass` reads the exit code the same code controls). State it as a limit in the README's "Held-out tests" (the graders judge an agent that writes the service, not one that attacks the test runner; the trial's scope and the review step are the boundary), or close it with a reason the owner accepts. Done 2026-11-21: stated as a limit in the README's "Held-out tests" (owner)

**Checkpoint**: code cases are judged on held-out tests.

---

## Phase 4: User Story 2 - Tests that check too little fail their eval (Priority: P1)

**Goal**: every tests case is judged by `MutantsKilled` (FR-005 to FR-013).

**Independent Test**: grader self-tests with no model: the reference tests kill 8/8; a weak test
set that binds every example but leaves the second note's id unchecked fails, naming the surviving
mutants (SC-002, SC-003).

### Tests for User Story 2

- [X] T011 [P] [US2] Test author: new file `src/tests/evals/steps/grade_mutants_test.go`: (a) the reference tests (`testsGood`) pass `MutantsKilled` with `counts` `{8, 8, []}`; (b) a weak test set (FR-005, FR-006, SC-002), the reference tests with the step "the note has the id" made to check nothing, still passes `EveryExampleBound` and `TestsFailAgainstStub` and fails `MutantsKilled`, `counts.failed` naming `R-2/always-one` and `R-2/zero-based`, sorted; (c) tests that fail against the reference implementation (one assertion inverted) fail `MutantsKilled` with a reason saying so and no `counts`, and judge no mutant (FR-008); (d) through `GradeTests`, tests that do not build get `MutantsKilled` failed with `not run: TestsBuild failed` and no `counts`; (e) with the fake runner of T004: a mutant whose run times out is reported `<name> (timed out)` and not killed, a run that cannot start `<name> (did not run)` and not killed, and `counts.total` stays 8; (e2) tests in two packages under `tests/`, one of which fails on a mutant: the mutant is killed; (e3) fail fast: tests that fail `TestsBuild` or `EveryExampleBound` get `MutantsKilled` failed with `not run: <grader> failed` and no `counts`; (f) `GradeTests` includes `MutantsKilled` for every tests case's clean fixture; (g) FR-009: no case's `Before()` files or `Message` contain a mutant's replacement line or name; (h) the agent's project folder is byte-identical after grading. Covers FR-005 to FR-014, SC-002, SC-003. Seen failing before T012 (depends on T003, T005)

### Implementation for User Story 2

- [X] T012 [US2] Code: new file `src/tests/evals/steps/grade_mutants.go`: `MutantsKilled` (constant `GMutants`) runs only when every earlier grader of the trial passed (`not run: <grader> failed` otherwise), copies the project the agent left, builds every package under `tests/` with the reference implementation and with each mutant of `Mutants()` as an overlay of `internal/app/app.go` (`variants.go`), runs the baseline first and stops with a failing `Result` and no `counts` unless every test passed there, then counts a mutant killed only when a run of one of its binaries reports a failed test or a panic (research R4), `counts.total` always 8; reason `<killed> of <total> mutants killed; survived: <names>`; `GradeTests` appends it after the existing graders. T011 passes; `run.sh graders` passes with its time printed (SC-004) (depends on T011). Done 2026-11-21 (733ec1b): `TestGraderMutants*` 2 min 57 s; full `run.sh graders` 13 min 33 s on a host at load 12 to 16 (8 min 51 s after US1, 3 min 44 s before), over `go test`'s default 10-minute timeout, so `run.sh` gets `-timeout 20m` as CI's `go` job (owner, 2026-11-21); SC-004 judged on the CI time at T013
- [X] T013 [US2] Push (ask the owner, naming the jobs): CI green; record the `go` job's grader self-test time in research S9 (depends on T012). Done: run 44921637628 (`8e00061`) green 26/26; `evals/steps` 311.6 s against 146.0 s before (+2 min 46 s, SC-004 holds in CI; S9)
- [X] T014 [US2] Fresh review of US2 (read-only, another model, own worktree, judging T013's CI run), against FR-005 to FR-013; findings fixed or recorded; then `/speckit-converge` for US2 (depends on T013). Done 2026-11-21: round 1 (fresh Sonnet, read-only, its worktree at `8effae4`, judging CI run 44921637628; `TestGraderMutants*` 85 s there) found no blocking finding; suggestions recorded as T026 to T030; `/speckit-converge` for US2: converged (FR-005 to FR-014, SC-002, SC-003, US2's three scenarios checked; the live harness writes `counts` through `Result` into `report.json`), nothing beyond T026 to T030

### Findings of the US2 fresh review (T014 round 1, fresh Sonnet reviewer, 2026-11-21, at 8effae4, judging CI run 44921637628: no blocking finding)

- [X] T026 [US2] Finding M1 (doc, FR-008 "a stated time limit"): the README's "Mutants" says "within the run's time limit" without its value; state it (`checks.DefaultGoTimeout`, 10 minutes per build or run). README change, with T020's docs. Done 2026-11-21: the README's "Mutants" names 10 minutes per build or run
- [X] T027 [US2] Finding M2 (suggestion): the baseline runs once, first; on the host runner a stateful or flaky agent test (a file in the copy, a fixed port) could pass it and fail every mutant, so all 8 look killed (research R4's stated limit; in Docker the copy is read-only and `/tmp` fresh per call). Decide with the owner: re-run the baseline after the mutants, or keep R4's limit as stated. 2026-11-21, the owner; stated in the README's "Mutants"; closed: R4's limit kept, since in Docker no state survives a run and only the self-tests' host runner has it
- [X] T028 [US2] Finding M3 (suggestion): a test binary that ends by `os.Exit` or `log.Fatal` (no `panic:`, no `fail` event), and a package-level `fail` event from `TestMain`, give `did not run`, so such a mutant survives (fails closed); state it as a limit in the README's "Mutants". Done 2026-11-21: stated as a limit in the README's "Mutants"
- [X] T029 [US2] Finding M4 (suggestion): SC-004 is met in CI (+2 min 46 s) and missed on the loaded host (+9 min 49 s); the owner decides whether SC-004 is a CI bound (a clarification of the spec) or the host miss is recorded. Done 2026-11-21 (owner): SC-004 is measured in CI; a spec clarification
- [X] T030 [US2] Finding M5 (suggestion, case left untested): the worst-case grading time of a tests trial whose agent test hangs is up to 9 runs of 10 minutes, beyond SC-005's 2 minutes; measured in T018, the owner decides if it matters (only for a hanging test). Done 2026-11-21: no live trial hung; the README's "Mutants" states the worst case (up to 90 minutes per test package) as a limit, and a spec clarification scopes SC-005 to trials whose tests do not hang (owner)

**Checkpoint**: tests cases are judged on mutants.

---

## Phase 5: User Story 3 - The run summary shows what the new graders found (Priority: P2)

**Goal**: the evals summary shows the counts and names (FR-016, FR-017).

**Independent Test**: the summary of fixed reports, with no model.

### Tests for User Story 3

- [X] T015 [P] [US3] Test author: new file `scripts/tests/test_ci_evals_summary_counts.py` (unittest, as `test_ci_evals_summary_quarantine.py`, running `scripts/ci-evals-summary.py` on temp reports): (a) a report with no `counts` anywhere gives byte for byte the output of today's script for the same report (compared with a stored expected text written by the test author from the README, not by running the new code); (b) a report with a code case of 3 trials (6/6, 4/6, 6/6) and a tests case (8/8 thrice) and a review case gives the two columns `Held-out` and `Mutants killed` with `6/6, 4/6, 6/6`, `n/a`, `8/8, 8/8, 8/8` and `n/a` as the README says; (c) the failure line for trial 2 names its two failed held-out tests; (d) a tests trial whose `MutantsKilled` has no `counts` shows `—` and a failure line with its reason, both for `not run: <grader> failed` and for `the tests do not pass against the reference implementation`; (e) a quarantined case and the tokens line are as today; (f) the exit code is unchanged by the columns: 1 exactly when a counted case failed. Seen failing before T016 (depends on T001). Done 2026-11-21: 23 tests, 15 failing against today's script (the columns and lines), 8 passing (today's output, exit code, summary file); the author's contract gap (no wording for a surviving-mutant line) fixed in the README by one additive example line, `mutants survived: <names>`, which the test leaves unpinned; the other silent points (separator dashes, lines before or after tokens, a case with no trials, quarantined cases' lines) stay unpinned

### Implementation for User Story 3

- [X] T016 [US3] Code: `scripts/ci-evals-summary.py`: the columns and failure lines per the README's "The report", only when some trial carries `counts`; the docstring updated. T015 and every existing `scripts/tests/test_ci_evals_summary*.py` pass unchanged (depends on T015). Done 2026-11-21 (fresh Opus code author): 23 of 23 T015 tests, the quarantine tests and `test_ci_scripts`' 20 summary tests pass; where the README is silent: quarantined cases get failure lines, a case with no trials an empty cell, a trial with no result from its grader `—` and no line; failure lines go before the tokens line
- [X] T017 [US3] Push (ask the owner, naming the jobs): CI green; fresh review of US3 (read-only, another model, own worktree, judging the CI run) against FR-016 and FR-017; findings fixed or recorded; `/speckit-converge` for US3 (depends on T016). Done 2026-11-21: run 44929645585 (`c6d323e`) green 26/26; round 1 (fresh Sonnet, read-only, its worktree at `c6d323e`) found no blocking finding, suggestions recorded as T031 and T032; `/speckit-converge` for US3 appended T033 (scenario 4 against FR-017), closed by the owner's clarification

### Findings of the US3 fresh review (T017 round 1, fresh Sonnet reviewer, 2026-11-21, at c6d323e: no blocking finding)

- [X] T031 [US3] Findings S1 to S3 (suggestions, cases left untested): `scripts/ci-evals-summary.py` trusts the shape of `counts`: a `failed` that is not a list of strings raises a traceback (a string is joined letter by letter), a missing or non-integer `passed` or `total` prints `None/6`, and `passed < total` with an empty `failed` gives no failure line. The harness writes these fields from `Result`, so it does not occur today. Contract first: the README's "The report" states what the summary does with a malformed `counts` (it says the report is malformed and exits 1, no traceback); then a test author's tests and the code. Not blocking T017 or T018; done, or closed with the owner, before T021. 2026-11-21, the owner on the coordinator's recommendation; closed: the harness writes `counts` from the typed `Result`, and the summary only displays it
- [X] T032 [US3] Finding S4 (suggestion): a trial that is not an object raises `AttributeError`; closed: pre-existing (today's script reads `t.get("passed")` the same way), not changed by US3. Also reported: case names and reasons are not escaped in the Markdown table; closed: the report's names are the harness's own fixed names, as before US3

**Checkpoint**: every story works with no model.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [X] T018 Live evals run of the steps suite with the owner's go-ahead (allow `004-edge-case-graders` in the GitHub environment `dev` first): the summary shows held-out and mutant results for every tests and code trial (SC-006); record each tests and code trial's grading time against the last green run (SC-005) and the cost in research S9. If a target of SC-004 or SC-005 is missed, the owner decides (research R3) (depends on T010, T014, T017). 2026-11-21: run 44933201919 (`c6d323e`, the owner's go-ahead; branch allowed in `dev`) green: every tests, code and review case pass^3; grading +23.5 s per code trial and +46.3 s per tests trial (SC-005 holds; S9). Open: the job summaries (the columns, SC-006) and the tokens and cost line are shown only on the run's page, which the API does not return; the owner reads them there. 2026-11-22, the owner read run 44957174113's summaries: every tests trial `8/8`, every code trial `6/6`, `n/a` in the other column, the review job's table unchanged (SC-006 met); no tokens line, since the trials carry no `usage`, so the cost is not reported (SC-005's tokens hold by construction: the graders call no model). Run 44957174113 (`836dd15`, after T023, owner's go-ahead) green as well: grading +28.1 s per code trial, +48.3 s per tests trial
- [X] T019 If T018 shows a tests or code case failing a new grader: propose the change of `src/agents/tests/instruction.md` or `src/agents/code/instruction.md` to the owner (a prompt change, Principle XIX), make it, and re-run the evals with the owner's go-ahead until the cases pass pass^3; no quarantine (FR-019). If nothing failed, check this task `closed: no agent failed a new grader` (depends on T018). Closed 2026-11-21: run 44933201919 passed every tests and code case pass^3 with both new graders
- [X] T020 [P] Docs: `src/tests/evals/steps/README.md` final (cost and time of a full run with the new graders, from T018); the [problem space](../../docs/overview/problem-space.md) N-013 shows `done` once T018 and T019 are green; `docs/log.md`; backlog T180 checked with this feature's commits; `python3 scripts/okf.py check` OK (depends on T019). 2026-11-22: the measured times and cost stay in research S9, not the README: a README change is an input of the steps evals and would make them pending again, a paid run for a docs line
- [X] T021 Final `/speckit-converge` for the whole feature, then a final fresh read-only review against every FR and SC, judging the CI run of the last pushed commit; findings fixed or recorded; the owner decides the merge to `main` (depends on T020). 2026-11-22: converge for the whole feature: every FR and SC met in code except SC-006, which waits for the owner's reading of the run summary (T018); nothing appended. Final review round 1 (fresh Sonnet, read-only, its worktree at `872b852`, judging CI run 44945817364 and live run 44957174113, both green; `TestGraderHeldOut*|TestGraderMutants*` 126 s there, summary tests OK): no blocking finding. Suggestions: S1, S2 (T018, T020, N-013, backlog T180 still open) wait for the owner's SC-006 check; S3 (a mutant whose `From` is gone equals the reference and survives, fail closed; `CheckMutants` catches it in the self-tests) closed: fails closed and is checked; S4 (summary cells of a case with no trials, lines of quarantined cases) closed: the contract is silent and both are harmless. Left untested: `DockerRunner.Go` beyond its argument lists (exercised by both live runs), two real test packages (fake runner only); closed: covered by the live runs and the fake-runner test

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

## Phase 7: Convergence

- [X] T033 Reconcile US3 acceptance scenario 4 ("shows the columns as not reported") with FR-017, research R5 and the README's "The summary" (a report with no `counts` prints exactly as before, with no new columns), which the code follows: the owner clarifies the spec through `/speckit-clarify`, then `/speckit-analyze` re-runs per US3/AC4 (contradicts). Done 2026-11-21: the owner chose "exactly as before"; spec clarification and scenario 4 reworded; analyze round 6 (fresh Sonnet, the changed lines only): 0 CRITICAL/HIGH, MEDIUMs fixed in T031 and here; LOW-1 (FR-017 says "without these results", the contract "no `counts`") accepted: a stopped grader with no counts on every trial shows no line, as the contract states
