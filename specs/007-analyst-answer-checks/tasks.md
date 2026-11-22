# Tasks: The platform refuses a revision that loses the slice's items

**Input**: Design documents from `specs/007-analyst-answer-checks/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Required. Outside-in, test-first: new tests are seen failing before the code; every
new refusal has a clean and a planted case.

**Organization**: the contract (Phase 1), then US1 (repeated key), US2 (kept IDs), US3 (the
revision evals through the platform). US1 and US2 both add faults in `Service.propose`; US2
starts after US1's code is committed.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1, US2 or US3 (spec.md)

## Rules for every task

- **Contract first**: T001 puts the contract in its authoritative places and is committed alone,
  before any test or code.
- **Test-first**: a test task and the code task that makes it pass are separate tasks, done by
  different authors; new tests go in new files. The code author never changes a test, a fixture
  or test data.
- **Existing tests**: only those T002 lists change, each after the owner approves that change;
  its commit carries `Tests-approved-by: Marius Gologan (<date>)`. Any other existing test that
  turns out to need a change is stopped and asked for, quoting what changes and why. New engine
  tests are plain Go tests (no build tag).
- **No dependency** is added; no protected path (`scripts/`, `.github/`, the arch lint file) is
  expected to change.
- **Faults**: worded as [data-model.md](data-model.md) "Faults"; they name keys and IDs only,
  never answer text (FR-010).
- **Test time**: locally only the new test files and the packages they touch (`go test ./slices/
  ./analyst/ ./api/` in `src/engine`, each capped at 10 minutes, after `scripts/check.sh
  --prepare`), with the pinned toolchain first on `PATH` (backlog T189); the evals harness
  compiled with `go vet -tags evals ./evals/analyst/` in `src/tests`. `run.sh stub` (Docker, no
  cost) runs only with the owner's go-ahead, timed. No full `scripts/check.sh`, Docker lab or
  image build. The full test command is the CI run of the pushed commit; each push is told to the
  owner (which jobs) and waits for a go-ahead. A live eval run is paid and needs the owner's
  go-ahead.
- **Briefs**: every test-author and reviewer brief says: think through the edge cases and list
  those checked; it gives a time limit and a cap on full runs.
- **Fresh review**: each story and the feature end with a fresh review by a read-only agent on
  another model than the author's, in its own worktree checked out at the branch commit with the
  uncommitted and untracked files, judging the CI run; at most 3 rounds. Then `/speckit-converge`.
- **Gate**: `/speckit-analyze` (fresh reviewer) must report no CRITICAL or HIGH before T001, and
  again after any change to spec, plan, tasks or a design artifact. Round 1 (fresh Sonnet, at
  cd97fc0): 0 CRITICAL, 3 HIGH (F1 stub cases cannot show an accepted retry, F2 case-variant keys,
  F3 the retry message with no previous proposal), 4 MEDIUM, 3 LOW, fixed at their source (spec
  Clarifications, research R1-R3, data model). Round 2 (fresh Sonnet, at f213664): all fixed; 0
  CRITICAL, 0 HIGH, N1 MEDIUM and N2 LOW (the contract draft lacked the goal exception and the
  quoting) fixed in wording. Round 3 (fresh Sonnet, at fb51519): both fixed, no finding. Gate
  passed.

---

## Phase 1: Setup (contract first)

- [X] T001 Contract (committed alone): `src/engine/README.md` section Slices gains the additions
  of [contracts/analyst-answer-rules.md](contracts/analyst-answer-rules.md) (`ParseProposal`,
  `AnswerFault`, the kept-IDs rule of `Answer`; the sentence "such as rules naming a removed goal"
  about withdrawals after `Answer` is reworded, since a revision now keeps every ID; "An analyst
  error, `NarrowDescription` and `TooManyQuestions` are returned, not retried" gains the
  `AnswerFault` exception); the
  `createSlice` and `answerQuestions` descriptions in
  `src/engine/api/preview/2026-11-12-preview/openapi.yaml` gain the sentences there;
  `scripts/generate.sh` regenerates and `scripts/generate.sh --check` passes; the draft then
  links to the authoritative copies
- [X] T002 Ask the owner, one direct question per change, quoting it (research R3, R5): (a)
  `src/engine/slices/slices_crosspart_test.go`
  `TestAnAnswerThatRevisesAPartWithdrawsApprovalsOfPartsItNowBreaks` expects the refusal
  (`OperationFailed` naming G-2, the slice's parts, approvals, questions and history unchanged)
  instead of the withdrawal; (b) `src/tests/evals/analyst/revision_evals_test.go` `runRevision`
  through `Service.Answer` and `TestHarnessStubRevision`'s fixtures and attempts (T012); (c)
  `src/tests/evals/analyst/retry_evals_test.go` `dockerAnalyst` parses with
  `slices.ParseProposal`; (d) `src/tests/evals/analyst/revision.go` header and
  `src/tests/evals/analyst/README.md` describe the retry. Record the answers in research R5

---

## Phase 2: User Story 1 - A malformed analyst answer is not taken as a proposal (Priority: P1)

**Goal**: an analyst answer that holds a key twice is refused and asked for again (FR-001,
FR-002; research R1).

**Independent Test**: T003-T005 pass; with a stub analyst whose first answer repeats `examples`
and whose second is good, `Answer` yields the second with `attempts` 2.

- [X] T003 [P] [US1] Test author: new `src/engine/slices/proposal_test.go` for
  `ParseProposal(answer []byte) (Proposal, error)`: a key repeated at the top level (`examples`),
  in a goal, in a rule, in a question and in a nested object of a question, each gives an error
  that `errors.As` reads as `*AnswerFault` whose reason is `the answer holds the key "<key>"
  twice at <pointer>` with the JSON Pointer of the object (`/`, `/rules/1`, ...); the same value
  twice is still refused; `examples:` twice inside the Gherkin string is not a repeated key; the
  same key in two different objects is not a repeat; an answer that is not JSON, an array, or
  one object followed by more data gives an error that is not an `*AnswerFault`; a good answer
  gives the same `Proposal` as `json.Unmarshal`; a key written with a JSON escape (`"a\u0062"`)
  and the same key written plainly (`"ab"`) count as one key, as do keys that differ only in
  letter case (`examples`, `Examples`); a key or pointer holding a line break or longer than 64
  characters appears in the reason quoted (`%q`, no raw line break) and cut to 64 characters
- [X] T004 [P] [US1] Test author: new `src/engine/slices/answer_fault_retry_test.go`, with stub
  analysts returning an error that wraps `*AnswerFault`: on `Create` and on `Answer`, a fault then
  a good proposal is accepted with `attempts` 2 in the `proposed`/`revised` history entry, the
  second call's `AnalystInput.Errors` holds the fault's reason and `Previous` is nil (on `Answer`
  `Current` is still set); a fault on every attempt (the pattern's `limits.attempts`) gives
  `OperationFailed` naming the key, `Create` stores nothing and `Answer` leaves the slice's
  parts, approvals, questions and history unchanged; an analyst error that is not an
  `*AnswerFault` is still returned at once, not retried
- [X] T005 [P] [US1] Test author: new `src/engine/analyst/http_repeated_key_test.go`: with the
  test server `http_test.go` uses, an answer whose content repeats a key makes `Propose` return
  an error wrapping `*slices.AnswerFault`; the session is still deleted
- [X] T006 [US1] Code author: new `src/engine/slices/proposal.go` (`AnswerFault`, `ParseProposal`:
  a walk over `encoding/json`'s `Decoder.Token` keeping the keys of each open object, then the
  decode as today); `src/engine/slices/service.go` `propose` treats an error wrapping
  `*AnswerFault` as an unusable answer (its reason a fault, the next input with no `Previous`);
  `src/engine/analyst/http.go` parses with `slices.ParseProposal` and wraps its error with `%w`
  so `errors.As` finds the `*AnswerFault`; `message.go` does not change (data model, Faults). T003-T005 pass and the
  packages' existing tests stay green
- [X] T007 [US1] Commit (code and tests in separate commits); push (owner go-ahead; say which jobs
  run) and fix until CI is green; record the run in research; fresh review of US1;
  `/speckit-converge` for US1

- [ ] T019 [US1] Review findings US1-S3 and untested cases U1-U8 (fresh Sonnet review at 79731d4,
  suggestions, not blocking): tests for a fault followed by an unusable proposal and the reverse
  (`Previous`/`Errors` after each), a fault with one attempt (no pattern), nested arrays
  (`[[{"k":1,"k":2}]]`), the empty key, a fault wrapped twice, Unicode folds (U+212A against `k`,
  `ſ` against `s`), a fault with `ErrMultipleUseCases`, invalid UTF-8 and leading whitespace;
  new test files by a test author
- [X] T020 [US1] Review findings US1-S1, S2, S4 closed: S1 (README says every `Analyst` parses
  with `ParseProposal`; the evals' `dockerAnalyst` does in T012), S2 (`/` for the top level is
  the data model's choice), S4 (a decode error after the walk stays a plain error: FR-001 covers
  repeated keys only). closed: by design or by T012

---

## Phase 3: User Story 2 - A revision keeps every item of the current proposal (Priority: P1)

**Goal**: a revision that drops or re-homes an ID of the current proposal is refused, asked for
again, then fails and changes nothing (FR-003, FR-004, FR-005, FR-010; research R2).

**Independent Test**: T008 passes; a stub revision that drops R-1 then keeps it is accepted with
`attempts` 2; one that always drops R-1 fails naming R-1 with the slice unchanged.

- [X] T008 [P] [US2] Test author: new `src/engine/slices/kept_ids_test.go`, with stub analysts on
  `Answer` from the notes fixture (`testdata/proposal-notes.json`): a revision lacking a goal, a
  term, a rule, an example (`@E-<n>`) or an operation (`operationId`) is sent back with one fault
  per lost ID (`the revision drops <ID> of the current proposal`); a kept rule serving another
  goal (`<R-n> served <G-a> and now serves <G-b>`) and a kept example under another rule (`<E-n>
  was under <R-a> and is now under <R-b>`) likewise; a following good revision is accepted with
  `attempts` 2; a revision that adds new IDs and keeps the old is accepted at once; losing on
  every attempt gives `OperationFailed` naming the IDs and the slice's parts, approvals, questions
  (no answer recorded) and history unchanged; the faults never hold the answers' text; when the
  current examples or contract do not parse (stored through `ReplaceRules`/`ReplaceContract`),
  their IDs are not checked; a kept rule whose goal the current proposal lacks (removed through
  `ReplaceGoals`) may serve any goal in the revision; `Create` is not affected; a revision that is also unusable for an
  existing reason lists the existing faults first, then the ID faults
- [X] T009 [US2] Test author (owner-approved change T002 (a)): `src/engine/slices/slices_crosspart_test.go`
  `TestAnAnswerThatRevisesAPartWithdrawsApprovalsOfPartsItNowBreaks` expects the refusal as
  approved; commit with `Tests-approved-by:`
- [X] T010 [US2] (its `kept.go` broke the arch lint; T023 reworks it) Code author: new `src/engine/slices/kept.go` (the kept IDs of a proposal, as
  [data-model.md](data-model.md) "Kept IDs", and the faults of a revision against them);
  `src/engine/slices/service.go` `propose` adds them after `unusable`'s faults when the input has
  a current proposal. T008 and T009 pass; `slices_retry2_test.go`, `slices_retry_links_test.go`
  and the other existing tests stay green
- [X] T021 [US2] Contract (research R2, design gap; committed alone): `src/slice/README.md` gains,
  beside `ExampleScenarios`: "`ExampleRules(text)` gives each example ID of Gherkin examples
  with the rule it sits under." and then its limits: each `E-<n>` a scenario under a
  `Rule: R-<n>` block carries as an `@E-<n>` tag, in document order, each once (an ID carried
  twice keeps its first rule); unlike `ExampleScenarios`, scenarios outside such a block are not
  listed; empty text gives none; an error means the text is not Gherkin
- [X] T022 [US2] Test author (not T023's author): new `src/slice/example_rules_test.go` for
  `ExampleRules` as T021 states (order, first rule kept, scenarios outside a `Rule: R-<n>` block
  or under a block not named `R-<n>` left out, several `@E` tags on one scenario, empty and blank
  text, not Gherkin); seen failing before T023
- [X] T023 [US2] Code author: `src/slice/tags.go` `ExampleRules`, reusing its tag patterns
  (T022 passes); `src/engine/slices/kept.go` uses it, no longer imports the Gherkin parser and
  drops its own rule-block and example-tag patterns; no test file is edited; T008, T009 and every existing test pass;
  `go-arch-lint` passes
- [ ] T011 [US2] Commit; push (owner go-ahead) and fix until CI is green; record the run in
  research; fresh review of US2; `/speckit-converge` for US2

---

## Phase 4: User Story 3 - The revision evals prove the platform's defence and count again (Priority: P1)

**Goal**: the revision evals grade the platform's accepted revision with its attempts, with a
planted case for each fixed failure, and pass pass^3 live (FR-006, FR-007, FR-008, SC-003;
research R3).

**Independent Test**: the stub run proves the planted cases; one live analyst run passes every
case pass^3, none quarantined.

- [X] T012 [US3] Test author (owner-approved changes T002 (b)-(d)): `src/tests/evals/analyst/revision_evals_test.go`
  `runRevision`: per trial a service built with a new helper analyst in `revision.go` that
  replays the earlier proposal on `Create` and hands each `Answer` call to a `dockerAnalyst`,
  then `Answer` with the case's answers; the graded output is the last
  attempt's answer, `GradeRevision` against the first attempt's input, the trial failing when
  `Answer` fails (its error in the trial) or a grader fails, `Trial.Attempts` the analyst calls of
  `Answer`; `TestHarnessStubRevision` adds the new fixture
  `src/tests/evals/analyst/testdata/revision/bad-revised-repeated-key.json` (the good revision with
  `examples` repeated, its last value `"Feature: Notes\n"`) and asserts the bad fixtures fail every
  trial with the pattern's `limits.attempts` attempts and `good-revised` passes with 1;
  `retry_evals_test.go` `dockerAnalyst` parses with `slices.ParseProposal`; `revision.go`'s header
  and the suite's `README.md` say the revision cases run through the platform's retry. `go vet
  -tags evals ./evals/analyst/` passes; `run.sh stub` with the owner's go-ahead, timed (research
  R3 cost); commit with `Tests-approved-by:`
- [ ] T013 [US3] Push (owner go-ahead); live analyst evals dispatch (paid, owner go-ahead; the
  branch added to environment `dev`'s branch policies first); record the run, each revision
  case's attempts and the cost in research; SC-003: every analyst and revision case pass^3, none
  quarantined. A failure keeps its outputs (ADR 0078) and becomes a backlog task; the owner
  decides the next step
- [ ] T014 [US3] Fresh review of US3; `/speckit-converge` for US3

---

## Phase 5: Polish

- [ ] T015 Docs in the same change as the behaviour: the [architecture
  overview](../../docs/architecture/overview.md) (the analyst service's proposals: the two
  refusals, ADR 0081), `docs/log.md`, the [problem space](../../docs/overview/problem-space.md)
  state of N-013 and N-051; ADR 0081 and 0082 checked against what shipped;
  `python3 scripts/okf.py check` reports `OK`
- [ ] T016 SC-004: record in research that an answer usable on the first attempt makes one
  analyst call (T004, T008 and the live run's attempts)
- [ ] T017 Final fresh review of the whole feature (rules above) and `/speckit-converge`; findings
  fixed or recorded as tasks; FR-009: `git diff main -- src/agents/ src/engine/api/preview/2026-11-12-preview/slice-proposal.schema.json`
  shows no change
- [ ] T018 Before the owner's merge: read the branch's `evals-current` output; then, after the
  merge (fast-forward, the owner's), on branch `006-method-record`: revert f757838 (the T195
  quarantine) with the owner's `Tests-approved-by:`, rebase on `main`, close T195 and T196 in
  `specs/backlog.md`, and run 006's eval dispatch (owner go-ahead); remove this branch from
  environment `dev` and delete it (owner OK)

---

## Dependencies

- T001 before everything; T002 before T009 and T012.
- US1 (T003-T007) before US2's code (T010): both change `Service.propose`.
- US3's T012 needs T006 (`ParseProposal`) and T010 (the live cases rely on the kept-IDs refusal).
- T021 before T022 before T023 before T011 (T023 fixes T010's `kept.go`).
- T013 after T012 and US2 pushed; T015-T018 after T014.

## Parallel examples

- US1: T003, T004 and T005 by three test authors at once (different files).
- US2: T008 and T009 at once (different files).

## Implementation strategy

US1 first: it alone fixes T195's failure in production and is pushed and reviewed by itself.
US2 then closes T196. US3 changes the evals last, once both refusals exist, and its live run is
the evidence before both features merge.
