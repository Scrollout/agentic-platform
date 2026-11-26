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

- **Contract first**: T001 extends the engine and check contracts additively (except the new enum values, ADR 0100), as
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
  ./durable/ ./checksvc/ ./store/ ./machine/ ./steps/ ./slices/` in `src/engine`, `go test ./...`
  in `src/slice` after a schema change, `go build ./...` in `src/check` after regenerating, `go test ./...` in `src/page` and `go vet
  ./...` in `src/tests` after regenerating, `go test ./...` in `src/cli`), each capped at
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
  again after any change to spec, plan, tasks or a design artifact. Round 1 (fresh Sonnet, at 3b74d73): C1, I1,
  I2, P1, P2 HIGH (E1's limit was a status; in-word forms; a refused value; what the proof
  proves; a proof after erasure), 10 MEDIUM, 3 LOW; the owner answered four questions (refuse at
  the fit and say why; the proof's real claim; SC-002 dropped; no versions); fixed at their source
  (spec Clarifications, Story 2, edge cases, FR-003, FR-004, FR-007, FR-008, FR-010, SC-006;
  research E1, R1, R3 to R7; data model; contract; plan threat model, Decisions, Complexity
  Tracking; ADR 0095; glossary; T001 to T003, T005, T006, T012 to T014, T018, T019). Round 2
  (fresh Sonnet, at 351e28b): every round-1 finding fixed except M1's wording in FR-011; 0
  CRITICAL, 0 HIGH; F1 MEDIUM (FR-011 "latest passing proof"), F2 to F4 LOW (a running proof;
  ADR 0090 silent on the review outputs and attempts; erasure during a run); fixed at their
  source (FR-011, an edge case, research R3, data model, plan, T018); F3 closed: the detail lives
  in research R1 and the data model, ADR 0090 records the decision. Gate passed. T001 found a
  design gap (step workers are per slice and a proof has no slice); the owner chose step workers
  keyed by the captured pattern (2026-11-25); fixed at its source (spec Clarifications and
  FR-004, research S17, R2, R5, R7, data model, contract draft, plan, ADR 0096, T001, T006, T008,
  new T027 to T029). Round 3 (fresh Sonnet, at 9609f03): 4 HIGH (a run with no slice has no
  home in the engine, the contract or the launcher; no expected hashes after the slice's
  erasure), 5 MEDIUM, 4 LOW; the owner chose a proof slice instead (2026-11-26, ADR 0097
  superseding ADR 0096); fixed at the source (spec Clarifications, FR-004, FR-013, Story 1 and 3;
  research S18, R2, R5, R7; data model; contract draft; plan; T001, T002, T006, T007, T008,
  T023, T027 to T029); M1, M2, M4 and L3 fall away with keyed workers; L2 quickstart. Round 4
  (fresh Sonnet, at fb1c8ff): round 3 fixed; 2 HIGH: H1 the proof slice has the source's
  description, so createSlice's reuse (`approvedFor`, a map) may hand it back for the source
  prose, and nothing marks a proof slice in `Slice`; H2 no task says what replay, resume and
  take-over do for a captured run or a proof run (`ReplayRun` uses the base pattern, `ready()`
  the base steps). 8 MEDIUM: M1 FR-006 and SC-003 need a proof-run carve-out and a proof-slice
  entity and edge cases; M2 the exact `Operation.target`/`result`/`OperationKind` widening and
  oasdiff evidence; M3 the pattern choice lives on `Run` and `RunStarted`, not the 202, and
  startRun's new refusal order; M4 who records a Proof and maps a run end (handed over,
  cancelled, expired, a reused result) to its outcome; M5 deleteSlice of a proof slice clears
  `proof_slice`, dangling proof run IDs, T018 versus erasing proof runs; M6 a CLI test task for
  `build --pattern` and the pattern-choice lines, output rows in T001; M7 how an approved slice
  is made with no analyst or developer approval and what a failing check answers; M8 T029 after
  T007, one author for `proof-slice`/`prove`, RunStarted fields and the proof branch in US1.
  4 LOW: plan.md:99 and ADRs 0093/0095 wording (refined by 0097); R7 command list; `ap prove`
  prints the Proof outcome; `proof_slice` cleared on erase. Gate not passed; H1 and H2 await the
  owner; the owner chose the recommended answers (2026-11-26: proof slice marked and never
  reused; resume and take-over keep the pattern, a replay follows the usable rule, a replayed
  proof is a new proof); every round-4 finding fixed at its source (spec Clarifications, FR-006,
  FR-011, SC-003, Key Entities, edge cases; research R5 to R7; data model; contract draft; plan;
  ADR 0093 and 0095 descriptions; T001, T006, T007, T009, T013, T015, T018, T027 to T029, new
  T030). Round 5 (fresh Sonnet, at b870e6c): every round-4 fix present; 1 HIGH (F1 a failing
  proof hands its run over, which waits, so the proof stayed `running`), 8 MEDIUM (F2 `not_used`
  had no storage; F3 no history kind for the engine's approval, `slice.schema.json` not named;
  F4 T006 could not pass at T007's end; F5 the proof side of `ReplayRun` had no owner; F6
  parameters could change during a proof; F7 comparing generated code fails every proof after a
  generator change; F8 paths were not filled; F9 a proof read right after its run could be
  `running`), 5 LOW (F10 a draft leftover of ADR 0096; F11 "the start's answer"; F12 a replay of
  an erased captured pattern; F13 the named fit before NotApproved; F14 per-step attempts are not
  expressible). Owner (2026-11-26): F7 filled outputs only, F8 paths filled too (ADR 0099). Fixed
  at the source (spec Clarifications, FR-003 to FR-005, FR-009, FR-010, Story 1 and 2, edge
  cases; research R1, R4, R5; data model; contract draft; plan; ADR 0097 description; T001,
  T003, T005 to T007, T010, T015, T018, T027, T029, T030). Round 6 (fresh Sonnet, at 30456ec):
  1 CRITICAL (C1 the parameter limits and name rule were not declared as contract integers or
  pinned to the code, Principle III), 7 MEDIUM (I1 US1 exits needing US3 behaviour and unclear
  owners; I2 `Check`/`Fit` signatures could not judge occurrence or paths; I3 the F7 rule not in
  the glossary and ADRs; U1 how `--add` sets name, kind, source; U2 a captured or proof run could be
  captured; U3 a proof slice unapproved by a method change; U4 an erased captured pattern's ID
  reused with another template), 6 LOW (L1 "the start's answer" leftovers; L2 busy not in the
  draft rows; L3 one completion function, a proof run without its Proof; L4 the outcome line and
  `ap show` untested; L5 a replay's parameter values; L6 the shapes of `parameters` and
  `not_used`). Fixed at the source under the owner's instruction to apply the fixes (spec
  Clarifications, FR-001, FR-003, FR-011, Story 1; research R4; data model; contract draft; plan;
  glossary; ADRs 0093, 0095, 0097; T001, T003, T005 to T007, T012, T013, T018, T020, T027, T030).
  Round 7 (fresh Sonnet, at 472576f): 0 CRITICAL, 0 HIGH (gate passed), 8 MEDIUM (T1 T005 cases
  needing US2/US3 code; T2 the proof outcome line tested only in US2; C2 `Fill` given a writable
  check it has no input for; U1 a slice changed after the run; U2 who approves; U3 no state
  table; R1 a `fill` step's reuse key missing the parameter values; A1 SC-006's derived 201-
  character text over the 64-character limit), 4 LOW (C3 the `Not used:` reason; D1 ADR 0093's
  description; P1 the plan's file list; T3 "per parameter"). The owner asked for every finding
  fixed: fixed at the source (spec Clarifications, FR-001, FR-006, SC-006; research R4; data model
  state table, approval, capture refusals, reuse key; contract draft; plan; ADR 0093 description;
  T003, T005 to T009, T013, T015, T018, T020, T027, T029, T030); ADR 0095's body line stays as
  written (an accepted decision is not rewritten), its banner says the run names it. Round 8
  (fresh Sonnet, at e9ade3e): every round-7 fix present; 0 CRITICAL, 0 HIGH; 4 MEDIUM (N1 T005's
  proven/approved cases need T029's proofs; N2 goals have no hash in `RunStarted`; N3 the `tasks`
  slice cannot be created without the analyst after the capture; N4 T013's withdrawn replay needs
  US3), 3 LOW (N5 R7's code list; N6 the approval cell; N7 per-operation error codes in T001);
  fixed at the source (spec FR-001; research R7; data model; contract draft; T001, T005, T006,
  T013, T014, T018). Round 9 (fresh Sonnet, at
  0d9fc39): every round-8 fix present; 0 CRITICAL, 0 HIGH; 4 MEDIUM (M1 the state table's
  withdraw, erase and proof-slice cells still in US1 tests; M2 no task wrote `part_hashes`, the
  machine and durable owners unnamed; M3 a replay's `part_hashes`; M4 the workflow version bump,
  ADR 0067), 2 LOW (L1 404 per id-addressed call; L2 who computes `usable`); fixed at the source
  (FR-001; data model; contract draft; plan; T001, T005 to T007, T013). Round 10 (fresh Sonnet, at 8197a1d): every
  round-9 fix present; 0 CRITICAL, 0 HIGH; 2 MEDIUM (O1 the proof-run capture refusal had no US1
  owner; O2 a Proof written "in the same transaction" the file store does not have, and no
  repair for a lost or refused start), 5 LOW (O3 `part_hashes` shape; O4 `Run` without
  `part_hashes`; O5 404 in the draft; O6 who defines `usable`; O7 a replay case that cannot fail,
  and the local package list); fixed at the source (data model; contract draft; T006, T007,
  T015, T020, Test time). Round 11 (fresh Sonnet, at 70eb864): every
  round-10 fix present; 0 CRITICAL, 0 HIGH; 6 MEDIUM (F1 research R5 and T007 kept the acceptance
  design; F2 busy unjudged between the 202 and `RunStarted`, a Proof under other parameters; F3
  empty template outputs; F4 a run under an older base pattern version; F5 the proof branch
  needs T029's marking; F6 `ap prove`'s exit for a failed proof), 3 LOW (L1 the pin test never
  seen failing; L2 `src/slice` and `src/check` in the local checks; L3 the capture name's form);
  fixed at the source (data model; research R5; contract draft; T001, T003 to T008, T028, T029,
  Test time). Round 12 (fresh Sonnet, at a07b340): every
  round-11 fix present; 0 CRITICAL, 0 HIGH; 5 MEDIUM (N1 the proof run's exit against the run
  states; N2 the base-version cause and the capture name not in the draft, spec or T001; N3 the
  run page, a second contract consumer; N4 Busy at a proof's start; N5 `writeStep`'s request and
  response), 4 LOW (N6 `parameter_value`; N7 a proof erased before it was read; N8 a no-op PUT;
  N9 T015's and T020's owners and exits); fixed at the source (spec FR-001 and an edge case;
  research R4; data model; contract draft; plan; T001, T004, T006, T008, T015, T020, T027 to
  T029, new T031, T032, Test time). Round 13 (fresh Sonnet, at 3b64d71): every
  round-12 fix present; 0 CRITICAL, 0 HIGH; 3 MEDIUM (S1 the schema's 400 versus
  `ParameterRefused`; W1 a repeated `writeStep` and the meaning of `removed`; W2 `writeStep`'s
  body and file limits), 3 LOW (P1 a running proof's line; P2 `parameter_value` and
  `capture_name` pinned too; P3 parameters' order); fixed at the source (research R4; contract
  draft; T001, T004, T005, T008). Round 14 (fresh Sonnet, at b2d6325): every
  round-13 fix present; 0 CRITICAL, 0 HIGH; 1 MEDIUM (F1 the parameters' order tested in US1's
  proof lines instead of US2's event lines and the page), 2 LOW (A1 too many entries is the
  schema's 400; A2 the check contract's preamble and `handler.go`'s body limit); fixed at the
  source (contract draft; T001, T004, T007, T008, T030, T031). Round 15 (fresh Sonnet, at 14e9eb7): every
  round-14 fix present; 0 CRITICAL, 0 HIGH; 1 MEDIUM (N1 a proof run's replay: Busy and never
  `ProofRequired`), 1 LOW (N2 the check contract's other preamble sentences and the handler's
  doc comments); fixed at the source (contract draft; T001, T007, T027). Round 16 (fresh Sonnet, at d20223f): every
  round-15 fix present; 0 CRITICAL, 0 HIGH; 1 MEDIUM (F1 no test of a withdrawn proof run's
  replay), 3 LOW (F2 T015's usable rule must leave a proof run's replay alone; F3 the state
  table; F4 `writeStep`'s files `history.go` and `contract.go`, and the `x-error-codes`
  descriptions); fixed at the source (data model; T001, T007, T013, T015, T018). Round 17
  (fresh Sonnet, at 3baaad6): every round-16 fix present; 0 CRITICAL, 0 HIGH; 1 MEDIUM (N1 how the
  `fill` worker reaches `writeStep` without changing `machine.History` and its fakes), 1 LOW (N2
  the plan's `writeStep` files); fixed at the source (plan; T007). Round 18 (fresh Sonnet, at e47852f): every
  round-17 fix present; 0 CRITICAL, 0 HIGH, 0 MEDIUM; 1 LOW (E1 T006's split between `fill` and
  proof cases); fixed at the source (T006). Round 19 (fresh Sonnet, at 9eb597e, every
  artifact read in full): E1 fixed; 0 CRITICAL, 0 HIGH; 1 MEDIUM (U1 a named unapproved captured
  pattern at `startRun`: new `CapturedPatternNotApproved`), 2 LOW (O1 the method-rule `startRun`
  cases move to T013; C1 `nOTE` and inline text in T003, the no-parameter line); fixed at the
  source (contract draft; data model; T001, T003, T008, T013, T018). Round 20 (fresh Sonnet, at f6fd1ce): U1, O1, C1
  fixed; 0 CRITICAL, 0 HIGH, 0 MEDIUM; 2 LOW (F1 R7's code list; F2 Busy applies only to a
  proof's start); fixed at the source (research R7; contract draft; data model). Round 21 (fresh Sonnet, at d46d36f): F1, F2
  fixed; 0 CRITICAL, 0 HIGH; 1 MEDIUM (D1 "only a usable captured pattern is judged" against
  `needs-proof`), 3 LOW (L1 Proof slice in the spec's terms; T1 T014 and T028 owned by T015 and
  T029 on CI; F1 `Fit`'s refusal result); fixed at the source (spec; data model; contract draft;
  T015, T029). Round 22 (fresh Sonnet, at 793ead2): D1, L1, T1,
  F1 fixed; 0 CRITICAL, 0 HIGH; 2 MEDIUM (A1 the template hashes from a reused or fixed step; A2
  the new operations' callers untested), 2 LOW (A3 T013's order case limited to T015's refusals;
  A4 the fit decides `refused-value` before `needs-proof`); fixed at the source (research R3;
  data model; T001, T005, T013, T018, T027). Round 23 stopped by the owner (2026-11-26): the
  gate has passed since round 7 and later rounds found only MEDIUM and LOW edge cases; the analyze
  loop ends here. Edge cases found from now on go to the stories' fresh reviews and
  `/speckit-converge`. Gate passed; T001 next.

---

## Phase 1: Setup (contract first)

- [x] T001 Contract (committed alone): `src/engine/api/preview/2026-11-12-preview/openapi.yaml`
  gains `/captured-patterns` (`POST` capture `{run, name?}`, `GET` list paginated with opaque page
  tokens, `GET /{id}`, `PUT /{id}/parameters`, `POST /{id}/proof-slice`, `POST /{id}/approval`,
  `POST /{id}/withdrawal`, `DELETE /{id}`), each state change a 202 operation taking an
  `Idempotency-Key`; `Operation.target` refs a new slice-defs rule `operation_target`
  (`^((S|RUN)-[0-9]+|[a-z0-9-]+)$`) in place of `slice_or_run_id`, `Operation.result` gains
  `captured_pattern`, `OperationKind` gains `capture-run`, `replace-parameters`,
  `make-proof-slice`, `approve-captured-pattern`, `withdraw-captured-pattern` and
  `erase-captured-pattern`; `startRun` of a proof slice is a proof (data model, Proof slice);
  `startRun` answers 202 with its operation as before, and `Run` gains `proof` and the pattern
  choice (data model, Run); `startRun`'s description gives the refusal order of the contract
  draft; `x-error-codes` per operation: `startRun` and `replayRun` add `CapturedPatternWithdrawn`
  and `ProofRequired` (409), `startRun` also `CapturedPatternNotApproved`, both add `CapturedPatternBusy` (a proof's start or a proof run's
  replay), and `startRun` NotFound for an unknown `pattern`; each id-addressed captured-pattern call 404 `NotFound`; every new operation's `security` is
`clientToken` only (the list and `getCapturedPattern` also `pageToken`, as other reads), each
with its `x-operation-name`; `captureRun` 404,
  409 `RunNotDone`, `RunNotCapturable`, `CapturedPatternExists`; `replaceParameters` 409
  `CapturedPatternApproved`, `CapturedPatternWithdrawn`, `CapturedPatternBusy`, operation
  `ParameterRefused`; `makeProofSlice` 409 `CapturedPatternWithdrawn`, `CapturedPatternBusy`,
  operation `PartFailsChecks`; `approveCapturedPattern` 409 `ProofRequired`,
  `CapturedPatternWithdrawn`, `CapturedPatternBusy`; `withdrawCapturedPattern` and
  `eraseCapturedPattern` 409 `CapturedPatternBusy`; each with `IdempotencyKeyReused`; `RunStarted`
  gains `part_hashes` (goals, rules, contract); `Slice` and
  `SliceSummary` gain `proof_of`, and `slice.schema.json`'s history kinds gain `made-for-proof`
  (data model, Proof slice); every new field and value is optional or added, so every change is
  additive; the `CapturedPattern` (with `proof_slice`), `Parameter` (`name` `^[a-z][a-z0-9-]*$`, `kind`
  `name` | `number` | `string`, `source` `name` | `declared-value` | `literal`, `value` at most 64
  characters, `places` with `in` `content` | `path`, `origin` `proposed` | `added`),
  `Proof` and `Approval` schemas of [data-model.md](data-model.md); `OperationKind` values for
  each; `startRun` gains an optional `pattern`; the `Run` gains `pattern_reason` (`base`, `fits`,
  `chosen`), `parameters`, `source_run` and `not_used` (each captured pattern that needs a new
  proof or was refused for a value, with the reason), read from `RunStarted`; error codes `RunNotDone`, `ParameterRefused`, `ProofRequired`, `CapturedPatternWithdrawn`,
  `CapturedPatternExists`, `CapturedPatternBusy`, `CapturedPatternApproved`, `RunNotCapturable`, `CapturedPatternNotApproved`;
  integer `x-limits` keys `captured-pattern-parameters-max: 32`, `parameter-places-max: 10000`,
  `parameter-value-max-chars: 64` and the matching `maxItems`/`maxLength`; `PUT /parameters`
  takes `{name, value}` per parameter; `parameters` and `not_used` have the data model's shapes;
  `run-event.schema.json` `RunStarted` gains
  `pattern_reason`, `parameters`, `source_run`, `proof`, `part_hashes` and `not_used`, additively. `src/check/api/preview/2026-11-13-preview/openapi.yaml`
  gains `POST /project-versions/write` (`writeStep`: the contract draft's request, `CallCommon`
  plus `files`, response `{written, removed}` and error codes; the contract's preamble names
  `writeStep` beside `commitStep` for the larger body limit and among the calls safe to
  repeat, and beside `restoreStep` in its sentences on the project worker's calls, the 404
  `VersionHistoryNotServed` of the other deployments, `UnreadableProject`, the operations that
  read `.git`, and the history check, and in the `x-error-codes` descriptions that name
  `commitStep` and `restoreStep` (WriteFailed, VersionHistoryNotServed)). The engine contract gains the
  capture `name` rule (`capture_name` in `slice-defs.schema.json`, `^[a-z0-9]+(-[a-z0-9]+)*$`,
  `maxLength` 40) and `parameter_value` (research R4); `captureRun`'s description names every
  `RunNotCapturable` cause, the base-version mismatch included; `startRun` lists
  `CapturedPatternBusy` (a proof's start); `src/cli/contract.md`'s "Run states and exit codes"
  gains the proof run's row (0 passed, 1 failed, over 2, 5 and 6). `src/page` is regenerated and
  builds. New
  `src/engine/captured/captured.go` holds the Go interface of the contract draft (`Propose`,
  `Check`, `Fit`, `Fill` with the contract draft's signatures, the declaration from the base
  pattern, the limits and the `parameter_name`, `parameter_value` and `capture_name` rules as
  named values; types, signatures, doc comments
  stating research R4's rules and limits) with bodies that return a `not implemented` error.
  New `src/engine/api/captured.go` holds the generated server's new methods with bodies that
  answer as not implemented, and `src/engine/checksvc/` the same for `writeStep`, so every module
  builds; a client line whose generated name changed (`startRun`'s body) is adjusted; no other
  code changes.
  `scripts/generate.sh` regenerates; `scripts/method-digest.py` converges; the contract lint
  passes and oasdiff reports no breaking change other than the enum values research E2 lists (the
  owner's exception, ADR 0100), run as `oasdiff breaking` from `main`'s copy of each contract
  to the new one (the preview has one version, so `scripts/contract-diff.sh` compares nothing),
  its output recorded in research; the draft then links to the authoritative copies.
  `src/cli/contract.md` gains the command rows and output lines of the draft. Done 2026-11-26:
  oasdiff as research E2 records (the ADR 0100 exception only), Spectral clean on both contracts;
  `go test` of `src/engine` `./api/ ./slices/ ./store/`, `src/slice`, `src/cli` and `src/page`
  pass with no test changed, `go vet` of every module passes; `./checksvc/` fails only its five
  tests that install oapi-codegen offline (not in this host's module cache, unrelated to T001)
- [x] T002 Owner approvals: the plan's Complexity Tracking row (Principle XII not applicable, one
  developer; the Principle XV row was approved 2026-11-26, ADR 0100); and the code author of T001 lists every existing test that fails or no
  longer compiles after T001 (expected: tests that list the contract's paths, operation kinds,
  error codes, `RunStarted`'s, `Run`'s or a slice record's fields exactly, and launcher tests
  that list the commands it intercepts); ask the owner to approve each by name and record
  each approval here with its date; a change not approved is not made, and the task that needs it
  stops
  Done 2026-11-26: the owner approved the Principle XII row (2026-11-26) and the Principle XV row
  (2026-11-26, ADR 0100); T001 broke no existing test, so no test change needed approval
  The owner approved (2026-11-26) a change to the existing helper
  `src/engine/api/durable_service_test.go`, which T006's and T027's proof cases need (T033):
  register `WriteProject`; pass the new `RunStart` fields into `durable.RunInput`; `noHistory`
  implements `machine.Writer` and `Activities` gets the captured-pattern parameter source; a seam
  that holds a run before its first recorded event, and one that alters written bytes

---

## Phase 2: User Story 1 - Capture a done run as a proven pattern (Priority: P1) 🎯 MVP

**Goal**: a done run becomes a proposed captured pattern with parameters and every place they
occur; the developer edits the parameters; a proof rebuilds the source slice with no model call;
the developer approves only a proven one (FR-001 to FR-007, FR-014; research R1 to R5, R7).

**Independent Test**: T003-T006 pass; quickstart 1 and 2: capturing the evals' notes run proposes
`note` and `notes` with every place (research E1); on the limit fixture, the declared limit is
proposed and adding the derived value works; the proof ends passed with no model request; approval before a passing proof answers
`ProofRequired`.

- [x] T003 [P] [US1] Test author: new `src/engine/captured/propose_test.go` and
  `src/engine/captured/fill_test.go` over a fixture copied from
  `src/tests/evals/steps/testdata/ref` and `testdata/slice` into
  `src/engine/captured/testdata/notes/`, and a second fixture `testdata/limit/` (the notes slice
  with feature 009's `maxLength: 200` on the new note's text and an example, stated with the
  length as a number, "a text of 201 characters", refused): `Propose` gives the names `note` and `notes` (split at case changes from
  operation and schema names, and schema property names) as candidates, and on `limit/` also the
  declared value `200`, each with every place (part, line, column, case form), `notes` places
  never counted again for `note` (longest first, never overlapping), and no status code proposed;
  `Check` refuses two parameters equal in any case form (`ParameterRefused`, naming both), a name
  value outside `[a-z][a-z0-9]*` or with fewer than two letters (`a1`), a number with a sign or
  not a number, a string outside "letters, digits, `_`, `-` and spaces", a value over 64
  characters, an added value that does not occur in the slice, more than 32 parameters, more than
  10000 places per parameter; a number is matched only as a whole number (`200` not in `2001`, `1.200`,
  `200ms` or `v200`; in `-200` and `"200"`); an empty template output fills to no file; `nOTE`, none of the three forms, is left unchanged
  (the test author adds inline text for `nOTE`, `NOTE`, `note_id` and `denote`, which the notes
  fixture lacks, research E1); `Fill` with each source value gives the input bytes
  back for every file; with `task`/`tasks` gives `NewTask`, `listTasks`, `TASK` where `NOTE` was,
  and `task_id` and `detask` where `note_id` and `denote` were (every occurrence, inside words
  too); on `limit/` with `100`/`101` every `200` and `201` place carries the new value (SC-006);
  paths are filled too (`cmd/notes/main.go` becomes `cmd/tasks/main.go`, each such place with
  `in: path`), and two template files that would get the same filled path do not fill, naming the
  paths (a filled path outside its step's writable entries is the fit's to refuse, T012). Seen
  failing
- [x] T004 [P] [US1] Test author: new `src/engine/checksvc/write_step_test.go`: `writeStep`
  with no files writes nothing and answers 200 with empty `written` and `removed`; a folder that
  does not hold `before` outside the writable entries, or whose last commit does not hold
  `before`, answers 409 `ProjectStateDiffers`; a repeated request answers the same and leaves the
  same files; more than `project-files-max` entries answers 400 `InvalidRequest` (schema) and
  more than `step-files-max-bytes` of file bytes 422 `StepFilesTooLarge`, and a
  body up to `commit-request-body-max-bytes` is accepted; it writes files under the
  step's writable entries only, refuses a path outside them with 400
  naming it (also `..`, absolute paths, a symlinked folder), commits nothing, and `restoreStep`
  afterwards removes what it wrote; an unknown project or step is refused. Seen failing
- [x] T005 [P] [US1] Test author: new `src/engine/api/captured_capture_test.go` (engine rig, no
  model): `POST /captured-patterns` on a run that ended handed over, expired, cancelled or still
  running fails `RunNotDone` naming the state; on a done run it yields a proposed captured pattern
  `<base id>-<name>` (`pattern_id` `^[a-z0-9-]+$`; without a name, the slice ID in lower case,
  `api-feature-go-s-1`; an existing ID fails `CapturedPatternExists`), version always `1.0.0`, naming the source run, slice,
  base pattern and method, holding the source slice's approved parts and the `tests`, `code` and
  `witnesses` artifact hashes; its declaration is valid against `pattern.schema.json`, every agent
  step's worker is `procedure:fill`, the review step lacks FindingsCarryEvidence and
  NoBlockingFindingOpen and has outputs `[witnesses]`, `limits.attempts` and
  `limits.review_rounds` are 1, everything else equals the base pattern's; `PUT /parameters`
  takes `{name, value}` and sets kind and source as the data model says, removes and adds,
  answers 400 `InvalidRequest` at once for a body outside the schema (33 parameters, a 65-character
  value, a value outside `parameter_value`) and `ParameterRefused` for the rest of T003's cases (a
  value outside its kind, equal values in any case form, a value not in the slice, more than
  10000 places); a run whose slice changed after
  it started (a part replaced, or its approval withdrawn) answers `RunNotCapturable` naming the
  part (compared with the part hashes its `RunStarted` records), and a run started before
  feature 011 (no part hashes) answers `RunNotCapturable` too; each call answers per state as the
  data model's state table says for `proposed` in the rows US1 owns at T007 (`replaceParameters`,
  `approveCapturedPattern`, the busy cases; `makeProofSlice` is T027's, withdrawal and erasure
  T018's); the template hashes equal the final project's files for a run whose `tests` and
  `code` were `ResultReused` and for a run whose review round's fix changed `code`;
  `approveCapturedPattern`, `replaceParameters` and `captureRun` sent with a worker or page token
  answer 403 and change nothing (`withdrawCapturedPattern` and `eraseCapturedPattern` in T018,
  `makeProofSlice` in T027); new `src/engine/api/captured_limits_contract_test.go`: the limits and
  the `parameter_name`, `parameter_value` and `capture_name` rules `engine/captured` uses equal
  the loaded contract's, and a planted
  contract copy with one limit changed fails that comparison; a capture `name` outside
  `^[a-z0-9]+(-[a-z0-9]+)*$` or over 40 characters answers 400 InvalidRequest; a run recorded
  under another base pattern version answers `RunNotCapturable` naming both; a second state change while one of its
  operations runs answers 409 `CapturedPatternBusy`; list is newest first and paginated; a reused Idempotency-Key
  with another body answers as for other calls. Seen failing
- [x] T006 [P] [US1] Test author (two groups: the `fill` cases, in
  `src/engine/durable/fill_proof_test.go`, are the workflow-level ones: a `fill` step needs no
  worker, renders, calls `writeStep`, runs its checks, commits, makes no model call, writes nothing
  for an empty output, is never attempted twice, and the recorded histories replay; every other
  case is a proof case, passing after T029): new `src/engine/durable/fill_proof_test.go` and
  `src/engine/api/captured_proof_test.go` (workflow test environment, step workers and gateway
  faked): a proof is a `startRun` of the captured pattern's proof slice (it works after the
  source slice and its runs are erased) under the captured pattern with the source values, its
  `RunStarted` naming the captured pattern, `pattern_reason` `chosen`, `source_run` and `proof:
  true`; a `fill` step needs no registered worker; each `fill` step renders, calls `writeStep`, runs the step's checks, then
  `commitStep`; no agent worker and no model gateway call is made; the proof passes when the run
  ends done and the `tests`, `code` and `witnesses` outputs' hashes equal the captured pattern's
  template hashes (generated code is not compared; a planted change of generated bytes with its
  checks passing still passes), and records run, method, parameters,
  outcome and failures (a hand-over fails it at once naming the check while the run waits;
  expired or cancelled fails naming the end; an interrupted one stays running;
  `getCapturedPattern` read right after the run's end line, and after an engine restart, shows
  the completed proof); the existing recorded workflow histories still replay (a
  regression guard, exempt from "seen failing"), and a planted history in which the `fill` path
  runs without its version gate fails the replay; a second proof under the same method reuses no earlier result and runs every step;
  a lost start answer (`startedAnyway`) keeps exactly one Proof; a `proof: true` run with no
  Proof gets one on read; `PUT /parameters` sent between the proof start's 202 and its recorded
  `RunStarted` answers `CapturedPatternBusy`; a Proof whose parameters differ from the current
  ones neither makes the pattern proven nor counts for approval; a `fill` step whose template
  output is empty (a clean review's `witnesses`) writes nothing, commits, and passes the hash
  comparison;
  `PUT /parameters` while a proof's run has not ended (also a failed proof's waiting run)
  answers `CapturedPatternBusy`; `PUT /parameters` moves a proven one back to proposed, also
  when it leaves the same values; the
  state table's proven and approved columns in the `replaceParameters` and
  `approveCapturedPattern` rows (withdrawal and erasure are T018's);
  a planted template byte change fails it naming the file; a planted
  failing check fails it naming step and check, and a `fill` step is never attempted twice;
  approval before a passing proof, or after one
  under another method, answers `ProofRequired`; approval records `by` `client`, `at` and the
  proof; an approved
  captured pattern refuses `PUT /parameters` with 409; a proof interrupted by an engine restart
  continues and is never marked passed without its recorded end. Seen failing
  2026-11-26: the proof cases (`captured_proof_test.go`, 4a15de6) fail on the stubs; the `fill`
  cases (`durable/fill_proof_test.go`, untracked) pin `machine.Writer`, `WriteRequest`,
  `WriteResult`, `durable.WriteProject`, `WriteCall`, `FillInput`, `RunInput.Fill`/`NoReuse` and
  `Activities.Captured` in its header and are committed with T007; the cases it could not reach
  are T033's
- [x] T007 [US1] Code author (done when T003 to T005 and T006's `fill` cases pass, the `fill`
  steps' reuse keys taking the parameter values' hash; T006's proof
  cases and T027 pass after T029): `src/engine/captured/` (Propose, Check, Fill, the declaration),
  captured-pattern records and the second artifact holder in `src/engine/store/`, the resource in
  new `src/engine/api/captured.go` (`captureRun`, `listCapturedPatterns`, `getCapturedPattern`,
  `replaceParameters`, `approveCapturedPattern`; capture refuses a run whose `RunStarted` has
  `proof: true` or names a pattern other than a base pattern with `RunNotCapturable`;
  `makeProofSlice` is T029's, withdrawal and
  erasure T020's, the usable rule at `startRun` and `replayRun` T015's) with its operations in `src/engine/api/operations.go`, the
  `fill` worker in `src/engine/durable/workflow.go` and `src/engine/steps/` (it reaches
  `writeStep` through a new optional interface `machine.Writer`, found by type assertion on the
  history, so `machine.History` and its existing fakes stay unchanged, and a `WriteProject`
  activity in `durable/durable.go`, `activities.go` and `client.go` behind the same
  `workflow.GetVersion` bump; render through
  `engine/captured`, `writeStep`, checks, `commitStep`; the readiness check skips `fill`),
  `writeStep` in `src/engine/checksvc/history.go` (beside `commitStep` and `restoreStep`, under
  the project worker's lock, record and history check), `contract.go` (its request schema in the
  op-to-schema map, so the schema's 400 applies) and `handler.go` (its route; its body-limit
  branch covers `writeStep` as it covers `commitStep`; the `History` and `NewHandler` doc
  comments name it), the proof as a run of the
  proof slice with `proof: true` (T029 makes the proof slice and the `startRun` branch that
  recognises it; T007 adds the `proof_of` field on `slices.Slice` that branch reads), the `RunStarted` fields `pattern_reason`, `parameters`, `source_run`, `proof` and
  `part_hashes` (written by `StartRun` and `ReplayRun` through `src/engine/api/runs.go` `newRun`,
  `src/engine/machine/` events and `src/engine/durable/` `RunInput`/`RunStart`; a replay copies
  the earlier run's `part_hashes`), `usable` computed in the Proof-completion function (T015
  applies it at `startRun` and `replayRun`), a `workflow.GetVersion` bump for the `fill` path and
  the new `RunStarted` fields (ADR 0067) so runs in flight replay on the code that started them,
  the
  Proof created by the completion function from a `proof: true` `RunStarted` and completed at a
  hand-over or the run's end (and on read), busy judged from the proof slice's runs, the base
  pattern version check at capture,
  a `no reuse` flag in the run's start for a proof, `PUT /parameters` busy while a proof's run has
  not ended, one Proof-completion function every reader uses, and
  `ready` skipping `fill` in `StartRun`, `ResumeRun` and `ReplayRun`; no test is changed. `scripts/method-digest.py` converges
  Done 2026-11-26 by four code authors (Opus): d5a359b (`engine/captured`, `writeStep`),
  b765c91 (the `fill` worker, `workflowVersion` 2), 5ea7007 (the resource, store records,
  `RunStarted` fields, `method.sha256` converged); owner-approved test fixes 5d994d5 (T003),
  362b8b3 (T006 file committed), 1013ba1 (T005, `skeleton_test.go`'s target rule). T003, T004,
  T005 (21 tests) and T006's `fill` cases pass; T006's proof cases and T027 wait for T029;
  replaying a captured or proof run answers 500 until T015/T029 (T029's brief says so)
- [x] T034 [US1] Before T029 (owner approval needed): T027's rig (`newPSRig`) uses runsRig's
  `fakeStepWorker.produce`, whose outputs are not framed file sets, so every capture in
  `captured_proof_slice_test.go` fails closed on an unreadable template; propose the change
  (a framed-file worker as T005's `capRig.work`) to the owner
  Done 2026-11-26: the owner approved; `newPSRig` sets `psWork` (each output but the findings a
  framed one-file set inside the step's first writable entry); every T027 capture now succeeds
  and each case fails only at `proof-slice` (500, T029)
- [x] T035 [US1] Open points from T007, for the US1 fresh review (T011): a `fill` step records
  no `agent_digest` (data model "everything else equal"); `engine/captured` has no function that
  gives a sent value's kind and source (the API derives it from `Propose` and `Fill`'s form
  check); a capture with more than 32 candidates fails closed (spec silent); a failed
  `writeStep` ends the run interrupted then expired, not handed over (spec silent); the review
  step's rule (`NoBlockingFindingOpen`) and the literal `procedure:fill` live in more than one
  package; `steps` History's `Write` and the store's second holder after a slice erasure have no
  test. Also for T011 (analyze at 53635b0): the launcher's own `prove` dispatch (`cmd_prove`'s
  `die 3`/`die 1`) has no test in `scripts/tests` (F5); the open points in the done notes of
  T029, T009, T032 and T033 (F9); `makeProofSlice` of a hand-changed proof slice (re-approve as
  it is, or restore, or refuse; spec, T027 and the threat model, F10); `src/cli/contract.md` on
  `--add` of an existing name and `--drop` of a missing one (F11)
  closed: judged in T011 round 1; each point has its home in T037 to T045, or was judged not an
  issue (the page escapes as HTML; a replay of a proof uses current values, as the data model says;
  `getRun` after a restart awaits the recorded start)
- [x] T033 [US1] Test author (after T007 lands its types; owner-approved T002, commit with
  `Tests-approved-by: Marius Gologan (2026-11-26)`): the `durable_service_test.go` change T002
  records, and new `src/engine/api/captured_proof_seams_test.go` with T006's cases its first
  author could not reach: a lost start answer (`startedAnyway`) keeps exactly one Proof; a
  `proof: true` run with no Proof gets one on read; `PUT /parameters` between a proof start's
  202 and its recorded `RunStarted` answers `CapturedPatternBusy`; a planted template byte change
  fails the proof naming the file; a proof under another method does not count for approval.
  Seen failing before T029
  Done 2026-11-26 (Sonnet test author): the T002 change to `durable_service_test.go` (`WriteProject`
  registered, the new `RunStart` fields passed, `noHistory.Write`, `Activities.Captured`, seams
  `DurableHoldRun` and `DurableAlterFilled`), no existing test's result changed (616 pass, the 28
  T006/T027 proof cases fail as before); the five cases in `captured_proof_seams_test.go` fail at
  `proof-slice` (500, T029), so their later steps are unproven until T029. Open for T011: the busy
  case pays `recordWait` (10 s, real time), and `newRun` holds `s.runs.mu` during that wait, so a
  `PUT /parameters` sent then blocks rather than answers busy (spec silent); the other-method case
  edits the stored Proof's `method` (the rig's events carry none)
- [x] T027 [P] [US1] Test author: new `src/engine/api/captured_proof_slice_test.go` (engine rig,
  no model): `POST /captured-patterns/{id}/proof-slice` makes an approved slice from the captured
  copy (its parts equal the copy, its description is `Proof of <captured pattern>`, `getSlice` and
  `listSlices` show `proof_of`, its history starts `made-for-proof`, then one `approved` per part) and its result names
  it;
  a part of the copy that fails a check under the current method fails the operation with
  `PartFailsChecks` and makes no slice; `createSlice` with the source's description never
  returns it;
  asked again it gives the same slice; after a method change that withdrew a part's approval it
  checks and approves the parts again (or fails `PartFailsChecks`); after that slice is erased it
  makes a new one, also after the source slice and its runs are erased; a busy one answers
  `CapturedPatternBusy`; `startRun` of the proof slice uses the captured pattern whatever its
  state, a `pattern` naming another one or a part not equal to the copy answers
  `PatternDoesNotFit`; its run carries `proof: true`; `deleteSlice` of the proof slice clears
  `proof_slice`, keeps every recorded proof (its run then NotFound) and the template artifacts; a
  replay of a proof run is a new proof with no reuse, also when the captured pattern is proposed
  or unusable (never `ProofRequired`), and answers `CapturedPatternBusy` while another of its
  operations runs; a proof's `startRun` while another of the
  captured pattern's operations runs answers `CapturedPatternBusy`; `deleteSlice` of the proof
  slice right after its proof run ended, before any read, still keeps that proof; `makeProofSlice` with a
  worker or page token answers 403; a busy proof start answers `CapturedPatternBusy` before
  `CapturedPatternNotApproved` and the rest; capturing a proof run answers
  `RunNotCapturable`. Seen failing
- [x] T028 [P] [US1] Test author: new `src/tests/lab/captured_prove_test.go` (Docker lab, CI):
  `ap prove <captured pattern> <project>` runs the client's `proof-slice`, then `build-workers`
  and `build` for the proof slice into `<project>`; an unknown captured pattern is exit 4
  NotFound and starts nothing; after the run's end line it prints `Proof of <id>: passed` or the
  failures and exits 0 for a passed proof and 1 for a failed one (handed over, expired,
  cancelled or differing files); running it again into another project builds again.
  Seen failing
  (owner go-ahead for the lab)
  2026-11-26: written (4a15de6), vetted only; the lab stands in for the launcher's `prove`
  wiring (`proof-slice`, `build-workers`, `build`), the failed case is a cancelled proof; seen
  failing on the CI run T010 records
  2026-11-26 (CI run 45819156622): no CI job runs it. `scripts/check.sh`'s docker step builds
  only the suites `recorded`, `lint`, `acceptance` and `contract`; `src/tests/lab` is their helper
  package, and the Go unit job runs it without the `docker` tag. The owner chose (2026-11-26) to
  move its cases to the acceptance suite: T036; closed: moved to T036, which carries this case list
  unchanged
- [x] T029 [US1] Code author (after T007, on its store changes; done when T006's proof cases, T033,
  T027 and T008's proof cases pass, and T028's cases (T036) on the CI run T010 records): the proof slice in `src/engine/slices/` (made by the engine, the `startRun` branch for a proof slice, history
  `made-for-proof`, `proof_of`, never reused by `createSlice`, `deleteSlice` clears
  `proof_slice`), the `proof-slice` call in new `src/engine/api/proof_slice.go`, the proof side of
  `ReplayRun` (a replay of a proof run is a new proof, no reuse), `proof-slice`, the proof outcome line and its exit code in
  `src/cli/`, `prove` in `src/deploy/ap`; no test is changed
  Done 2026-11-26 by two code authors (Opus): engine 3ad1428 (`slices/proof.go`,
  `api/proof_slice.go`, the proof branch of `startRun` and `ReplayRun`, `deleteSlice` of a proof
  slice; fixes in T007's code: a proof failed at a hand-over is completed again to name the run's
  later end, `getRun` answers a started run before its `RunStarted` is recorded;
  `method.sha256` 16fb9c3f), client 1b0f426 (with T009); owner-approved fixes to three T027 cases
  29cfb33. T006, T027, T033 and T008 pass; `./api/`, `./slices/`, `./durable/` pass (4 min 34 s);
  T028's cases (T036) pass or fail on T010's CI run, which T010 and T011 judge. Open for T011: a non-proof `startRun` naming an approved captured
  pattern answers 500 until T015; a replay of a proof uses the captured pattern's current values;
  `makeProofSlice` of a proof slice changed by hand re-approves it as it is (its proof then answers
  `PatternDoesNotFit`; spec silent); a withdrawn captured pattern at `proof-slice`, proof start or
  replay is untested until T020; `getRun` of a started, unrecorded run after a restart is 404
- [x] T008 [P] [US1] Test author: new `src/cli/captured_test.go`: `ap capture <run> [--name]`,
  `ap patterns`, `ap pattern <id>` (parameters with places, proofs, state, usable; the
  `No parameters` line for a captured pattern with none), `--drop` and
  `--add`, `ap proof-slice <id>`, `ap approve-pattern` call the contract's operations, follow them and print
  as `src/cli/contract.md` states; the proof cases: after a proof's run the `Proof of <id>:
  passed`, `failed: ...` and `running` lines (exit 0
  for passed; 1 for failed, whether the run was handed
  over, expired, cancelled or ended done with differing files), `ap slices` shows `PROOF OF` and `ap show` its `proof of`
  line; errors print their code and reason. Seen failing
- [x] T036 [US1] Test author (owner-approved 2026-11-26: removing the existing
  `src/tests/lab/captured_prove_test.go`; commit with `Tests-approved-by: Marius Gologan
  (2026-11-26)`): T028's cases as new `src/tests/acceptance/captured_prove_test.go` (package
  `acceptance`, built by the docker step, run by the "Service tests with Docker (recorded model)"
  job, whose `AP_ACCEPTANCE_SKIP` does not name it; about 35 min more in that job), using the `lab`
  package's exported helpers only, plus `Lab.ProjectsDir()` (owner-approved 2026-11-26, the one
  change to `src/tests/lab/lab.go`, for T028's project-folder checks); remove the lab file in the same commit. The code (T029, T009) landed before any CI job ran
  these cases, so they cannot be seen failing first (plan Exceptions); they must pass on the CI run
  that T010 records, and the test author vets them and names a planted change they would catch
  Done 2026-11-26 (Sonnet test author; the coordinator added `Lab.ProjectsDir()` and restored
  T028's three folder checks after the owner's approval): vetted with `-tags docker`; each case
  names its planted change; passes or fails on T010's CI run
- [x] T009 [US1] Code author: the commands in `src/cli/` (generated client only), except
  `proof-slice`, the proof outcome line, the `PROOF OF` column and the `proof of` line (T029)
  Done 2026-11-26 with T029's client part (Opus, 1b0f426): T008 passes, `src/cli` and the launcher
  tests pass; `contract.md` names the captured-pattern commands under "Idempotency and the run
  ID". Untested (for T011): `--add` of an existing name replaces it, `--drop` of a missing one is
  exit 3 (contract silent); the lost-answer lines of the new commands; `ap prove`'s build path
  (T028 lab, CI only); `pattern_id`/`parameter_name` forms copied by hand into the client
- [x] T031 [P] [US1] Test author: new `src/page/run_started_fields_test.go`: the run page shows a
  `RunStarted`'s `reason`, `proof`, `source` and `parameters` as the terminal's event line gives
  them, the parameters in byte order of their names (two parameters declared in the other
  order). Seen failing
- [x] T032 [US1] Code author (done when T031 passes): the run page's event rendering in
  `src/page/`; no test is changed
  Done 2026-11-26 (Sonnet, 1bb6569): T031 and `src/page`'s tests pass. Untested (for T011): the
  page does not escape control characters or invalid UTF-8 in values as the terminal's `esc` does
- [x] T010 [US1] (after T007, T009, T029, T032 and T036) Commit (tests and code in separate commits); fast lint; ask the owner before
  pushing (every check job runs); record the CI run here
  Done 2026-11-26: pushed with the owner's go-ahead. CI 45819156622 at a05c77f (23 of 26: the
  engine archfile lacked `captured`, fixed 2473169 with the owner's approval) and 45824972601 at
  2473169 (`method.sha256` stale after the archfile, fixed 016ceec); CI 45830105349 at 016ceec:
  all 26 jobs green; T036's `TestCapturedProve...` passed in "Service tests with Docker (recorded
  model)" (8 min 30 s; the job 34 min). `evals-current` lists analyst and steps tests/code/review
  pending (T024 before merge)
- [x] T011 [US1] Fresh review of US1 judging T010's CI run; findings fixed as new tasks or
  recorded; `/speckit-converge` for US1
  Round 1 2026-11-26 (fresh Sonnet, at ddb0393, judging CI 45830105349): B1 blocking (a capture
  with more than 32 candidates fails, FR-001, Principle XVIII Sound) -> owner: cut at 32 by a rule
  (spec Clarifications 2026-11-26, FR-003, contract `captureRun`) -> T037, T038; S1 -> T040;
  S2, S3 -> T041; S4 -> T042; S5 -> T043; S6 -> T039 (owner: a US2 task); S7, S9 -> T044; S8
  closed: US3's T019/T020 add the client's `withdraw-pattern` and `erase-pattern`; S10 -> T039's
  clarify; T035's points judged (see T035)
  Round 2 2026-11-26 (same reviewer, at 14c20a2, B1 and the changed lines only): B1 fixed (S2
  resolved too: an added value's kind and source come from `Candidates`); no new blocking finding;
  round-1 homes accepted. Suggestions: the contract's "a kept one left with no place" cannot occur
  through Propose (T039's clarify may drop it); names before declared values can displace
  meaningful declared values in a small slice of generic words (T039's clarify may revisit);
  T045 adds the cases T038 lists
  `/speckit-converge` for US1 2026-11-26 (fresh Sonnet, at 38d2418): one gap, G1 -> T046; every
  other US1 behaviour built or homed in T039 to T045
  CI 45843298744 at 6dac265 (B1's tests and code, T046's wording): all 26 jobs green
- [x] T046 [US1] (converge US1, G1; FR-002, N-053) the captured pattern names the method that
  accepted its source run, not only the method at capture (`api/captured.go` capture stores
  `currentMethod()`; the source run's step events carry the accepting method): decide through
  `/speckit-clarify` (add the accepting method beside the capture-time one, or state in FR-002 and
  the contract that the field is the capture-time method); then contract, test author (new file),
  code author; not blocking US1 (the proof is judged under the current method, FR-004, FR-006)
  closed: the owner chose the capture-time method (2026-11-26); FR-002 and the contract's
  `CapturedPattern.method` say so; no code change
- [x] T037 [US1] Test author (B1): new `src/engine/captured/propose_cap_test.go`: a slice copy
  with more than 32 candidates proposes exactly 32, the first in Propose's order (names in byte
  order, then declared values in the contract's order), each with the places found among the
  kept ones only (a left-out candidate takes no place from a kept one, e.g. a left-out long name
  frees places for a kept shorter one); exactly 32 and 33 candidates (the boundary); 33 where one
  is refused by form or case form (all 32 proposed); a kept one left with no place is left out
  (fewer than 32), with no `ParameterRefused` and no error; and new `src/engine/api/captured_capture_cap_test.go`: such a
  capture succeeds with 32 parameters, and `PUT /parameters` may then drop one and add a
  left-out one. Seen failing
  Done 2026-11-26 (Sonnet, 6dc35df): 7 Propose tests and 3 API tests, seen failing (no cut; the
  capture fails OperationFailed); a kept candidate with no place at all cannot be built through
  Propose (each name's key is its own place), so the places-limit case stands for that rule
- [x] T038 [US1] Code author (done when T037 passes, no other test changes): the cut in
  `src/engine/captured/propose.go` (the existing `ParametersMax`, kept equal to the contract by
  `captured_limits_contract_test.go`), after the form and case-form checks, the places found after
  the cut; `src/engine/api/captured.go`'s capture no longer fails for too many candidates
  Done 2026-11-26 (Opus): `captured.Candidates` (every candidate, no cut, no places) and the cut
  in `Propose`; `replaceParameters` takes an added value's kind and source from `Candidates`, so a
  left-out candidate added back keeps its kind (T037's API case needs it); `method.sha256`
  40caf4b3. Untested (T045): an added left-out declared value's source; an added left-out name
  overlapping a kept longer value; several kept ones losing their places in later rounds
- [ ] T039 [US2] Before T012 (owner: a US2 task, decided through `/speckit-clarify`): SC-006's
  limit fixture (`testdata/limit`) has the limit 200 and the derived 201 also as the HTTP statuses
  `"200"` and `"201"`, so a slice with another limit cannot equal the source filled with its
  values (a parameter cannot exclude places); decide (another fixture limit, or excluding places)
  and fix the spec, data model and fixture; also decide `makeProofSlice` of a hand-changed proof
  slice (re-approve as it is, restore from the copy, or refuse; review S10, T027, threat model)
  and two round-2 suggestions: the contract's "a kept one left with no place" (unreachable through
  Propose), and whether names should come before declared values in the cut
- [ ] T040 [US2] (review S1; FR-009) A `fill` step's `writeStep` refusal that cannot succeed on
  resume (400 outside the writable entries, 422 `StepFilesTooLarge`, `InputHashMismatch`, an
  unreadable template, a refused fill) ends the run interrupted then expired; make it a
  hand-over, keeping interrupt for 503, 504 and `ProjectStateDiffers`: test author, then code
  author, in US2 before T015
- [ ] T041 [US1] (review S2, S3; Single owner for rules; suggestion) one function in
  `engine/captured` that gives a sent value's kind and source (the API's `kindOf` derives it from
  Propose's survivors and `Fill(nil, ...)`); the literal `procedure:fill` in one place
  (`captured.FillWorker`); do with US2's T015 or leave: not blocking
- [ ] T042 [US1] (review S4) data model "Declaration": a `fill` step keeps no `agent_digest`; fix
  "everything else equal" (docs, with T023)
- [ ] T043 [US1] (review S5, T033's open point; suggestion) a proof start reads the parameter
  values, then releases the captured lock before `StartRun` takes the runs lock, so a `PUT
  /parameters` accepted in the gap gives a proof of old values (fails closed: it does not count);
  and `newRun` holds the runs lock through `recordWait`, so a `PUT /parameters` then waits up to
  10 s before answering Busy; test author, then code author, or `closed:` by the owner
- [ ] T044 [US1] (review S7, S9, T035 F5/F11; suggestion) a launcher test in `scripts/tests` for
  `ap prove`'s dispatch (`die 3` for a bad project name, `die 1` for a bad slice ID, the exit code
  passed through); `src/cli/contract.md` states `--add` of an existing name (replaces it) and
  `--drop` of a missing one (exit 3); the acceptance proof asserts no model request (SC-001)
- [ ] T045 [US1] (review round 1, untested cases; Findings have a home) for a test author, each
  a new file: `steps` History's `Write` (project and check calls, the files read back, an error);
  `store.openCaptured` after a restart and `PutCapturedPattern` with a missing artifact; an engine
  restart between a capture's artifact hold and its record; two parameters whose source values
  overlap without containment; a one-character string literal as a parameter; `kindOf` of a name
  Propose dropped; a filled output over `step-files-max-bytes`; the client's lost-answer lines of
  the new commands and its `pattern_id`/`parameter_name` forms against the contract; T038's untested cases (an added left-out declared value's
  source; an added left-out name overlapping a kept longer value; several kept ones losing their
  places in later rounds)

---

## Phase 3: User Story 2 - A later slice of the same kind is built with no model (Priority: P2)

**Goal**: a slice equal to a usable captured pattern's source except for its parameters fits it
and is built with no agent step; the newest usable fitting one is chosen unless the run names
another (FR-008 to FR-010; research R4, R6).

**Independent Test**: T012-T014, T030 pass; quickstart 3: the `tasks` slice is built done from the captured notes pattern, the gateway sees no request, the derived tests
pass, three runs give the same files.

- [ ] T012 [P] [US2] Test author: new `src/engine/captured/fit_test.go` (T003's fixtures): the
  `task`/`tasks` slice fits with those values, and on `limit/` the `100`/`101` slice (SC-006);
  SC-005's planted set does not fit and names the first place it differs: another rule, another
  example, another member, another operation, another status, two holes of one parameter with
  different values, `note_id` left unchanged where `note` became `task`; a value outside its
  parameter's kind (a space in a name, a name where a number was, a sign) and two parameters
  given equal values do not fit and name the parameter and why; values that would give two
  template files the same filled path, or a filled path outside its step's writable entries, do
  not fit, naming the paths; a captured pattern with no parameter fits only the identical slice; matching is
  linear (a 1 MiB slice with 32 parameters fits in under 1 s). Seen failing
- [ ] T013 [P] [US2] Test author: new `src/engine/api/captured_startrun_test.go`: `startRun`
  without `pattern` uses the most recently approved usable fitting captured pattern, otherwise the
  base pattern; with `pattern` naming the base pattern or a usable captured one, that one; a
  `pattern` naming a proposed or proven captured pattern answers `CapturedPatternNotApproved` and
  starts nothing, while its proof slice's own run still starts; with the engine's method changed
  after approval, `startRun` skips the captured pattern and names it in `not_used` as
  `needs-proof`, and a named one answers `ProofRequired`; a later failing proof, and a proof still
  running, make an approved one unusable the same way; a named
  captured pattern that does not fit answers `PatternDoesNotFit` with the first difference or the
  refused value; without `pattern`, a captured pattern refused only for a value is named in the
  run with the reason and the base pattern is used; `startRun` answers 202 with its operation,
  and the `Run` and `RunStarted` carry `pattern` (`<id>@<version>`), `pattern_reason` (`base`,
  `fits`, `chosen`), the parameters' values and `source_run`; the refusals T015 reaches come in the contract's
  order (the Withdrawn and Busy places are T018's and T027's); an approved, unusable captured
  pattern whose fit fails only for a value is named `refused-value`, not `needs-proof`; resume and take-over of a captured run keep its pattern and parameters, and a replay
  uses the same captured pattern, refused with `ProofRequired` when its latest proof failed (a
  run that is not a proof; a proof run's replay follows the proof rule, T027; the withdrawn
  refusal is T018's), keeping the recorded parameter values with no new fit; capturing a replay
  whose slice changed after the earlier run started, or a replay of a run with no part hashes,
  answers `RunNotCapturable`; capturing a run built
  from a captured pattern answers `RunNotCapturable`; two slices that differ only in a value that
  occurs in the goals alone never reuse each other's filled results; a run from a captured pattern makes no agent worker or
  gateway call and ends handed over, naming the failing check, when a filled check fails, with no
  agent step started; the base pattern's fit conditions still run. Seen failing
- [ ] T014 [P] [US2] Test author: new `src/tests/acceptance/captured_test.go` (Compose stack,
  recorded model): create the `tasks` slice first, from a recorded analyst answer kept as test
  data, then build the notes slice from the recorded happy set until done; capture; prove;
  approve; replace and approve the `tasks` slice's parts (the notes slice with `note`/`notes`
  replaced) through the slice API, which calls no analyst; `ap build` ends done; the gateway's
  access log has no request from the capture to the build's end (SC-001); three builds give equal file hashes (SC-004); `--pattern api-feature-go` runs
  the agents. Seen failing (owner go-ahead for the stack)
- [ ] T030 [P] [US2] Test author: new `src/cli/captured_build_test.go`: `ap build --pattern`
  sends `pattern`; `build`'s first line, the `RunStarted` event line, the `run` header and the
  `Not used:` lines (from the run's `not_used`) print as `src/cli/contract.md` states, the
  parameters in byte order of their names (a case with two parameters whose declared order
  differs from byte order); refusals print their code and reason. Seen
  failing
- [ ] T015 [US2] Code author (done when T012, T013 and T030 pass, and T014 on the CI run T016
  records; an approved but unusable captured pattern is still judged for fit, to be named
  `needs-proof`): `Fit` in `src/engine/captured/`, applying the `usable` value T007
  computes in the Proof-completion function, the choice for a fitting captured
  pattern and a named one in `src/engine/api/runs.go` (`StartRun`; `ReplayRun`'s usable rule for runs that are not proofs, leaving a proof run's
  replay to T029's proof rule;
  a captured-pattern run chosen at `startRun`, which capture then refuses by T007's rule), the `Run`'s pattern choice and `not_used`, `ap build --pattern` and the output lines in `src/cli/`
- [ ] T016 [US2] Commit; fast lint; push (owner go-ahead); record the CI run
- [ ] T017 [US2] Fresh review of US2 judging T016's CI run; `/speckit-converge` for US2

---

## Phase 4: User Story 3 - A captured pattern stays true to its method (Priority: P3)

**Goal**: a captured pattern is used only while its latest proof passed under the current method; it can be
withdrawn and erased; runs record it (FR-011 to FR-013; research R3, R5).

**Independent Test**: T018-T019 pass: after a method change a fitting slice's run uses the base
pattern and names the captured pattern as needing a new proof; a new passing proof restores it;
a withdrawn one is never chosen; erasing the source slice keeps the captured pattern usable.

- [ ] T018 [P] [US3] Test author: new `src/engine/api/captured_method_test.go`: with the engine's
  method changed after approval (the `startRun` skip and `ProofRequired` cases are T013's; here a
  regression guard), a new passing proof under the new method
  makes it usable and records the method; a failing one leaves it unusable naming what failed;
  withdrawal stops its choice and a named use answers `CapturedPatternWithdrawn`, a run already
  started finishes on it, and earlier runs' records still name it; a later failing proof under the
  current method makes an approved one unusable; erasing the source slice keeps its template
  artifacts and its use, and a new proof after the erasure passes; a proof still running makes it unusable until it passes; erasing it while a
  run built from it or its proof has not ended answers 409 `CapturedPatternBusy`; erasing the
  captured pattern removes the artifacts only it holds and its proof slice with that slice's
  runs, and keeps the records of other slices' runs built from it; a replay of a run built from
  an erased captured pattern answers NotFound naming it; `withdrawCapturedPattern` and
  `eraseCapturedPattern` with a worker or page token answer 403 and change nothing; in the
  refusal order, a withdrawn named captured pattern answers `CapturedPatternWithdrawn` before
  NotApproved and the rest; a new capture under an erased captured
  pattern's ID answers `CapturedPatternExists`; the state table's withdrawn column (parameters,
  proof slice, approval refused with `CapturedPatternWithdrawn`, withdrawal again changes
  nothing); a replay of a run built from a withdrawn captured pattern, and a replay of a proof
  run of a withdrawn captured pattern, answer `CapturedPatternWithdrawn` and start nothing; a withdrawn captured pattern's proof
  slice answers `CapturedPatternWithdrawn` at `startRun` and `proof-slice`; erasing a captured
  pattern erases its proof slice and that slice's runs, and answers `CapturedPatternBusy` while a
  proof's run has not ended. Seen failing
- [ ] T019 [P] [US3] Test author: new `src/cli/captured_lifecycle_test.go`: `ap withdraw-pattern`
  and `ap erase-pattern` (shows what will be erased, asks for confirmation, does nothing without
  it). Seen failing
- [ ] T020 [US3] Code author (done when T018 and T019 pass): withdrawal and erasure in
  `src/engine/api/` and `src/engine/store/` (`withdrawCapturedPattern`, `eraseCapturedPattern`,
  the `CapturedPatternWithdrawn` refusals at `startRun` (a named use and the proof-slice branch),
  `replayRun`, `makeProofSlice`, `replaceParameters` and `approveCapturedPattern`, busy while a run built from it or its proof
  has not ended, the proof slice erased with it, erased IDs never given again, `ReplayRun`'s
  NotFound for an erased captured pattern); the method rule's
  remaining cases (T015 applies the `usable` value T007 computes); the commands in `src/cli/`
- [ ] T021 [US3] Commit; fast lint; push (owner go-ahead); record the CI run
- [ ] T022 [US3] Fresh review of US3 judging T021's CI run; `/speckit-converge` for US3

---

## Phase 5: Polish & cross-cutting

- [ ] T023 Docs: `docs/architecture/overview.md` (captured patterns, the fill worker, `writeStep`,
  the pattern choice, the proof slice, ADR 0090 to 0100 in its Decisions), `src/catalogue/README.md`
  (captured patterns beside the base patterns), `src/engine/README.md`, `src/deploy/README.md`
  (`prove`), `docs/apis/engine-api.md` and
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

- T011 round 1 → T037 → T038 → T011 round 2 → converge US1; T041 to T045 do not block US1 done
  (each fixed, or `closed:` by the owner, before the feature is done); T039 before T012; T040
  before T015.
- T001 → T002 → US1 (T003-T006, T027, T028 in parallel → T007 → T033 → T029 → T036; T008 → T009; T031 → T032;
  T007, T009, T029, T032 and T036 → T010 → T011) → US2 (T012-T014, T030 in parallel → T015 → T016 → T017) → US3 (T018-T019 in parallel → T020 → T021 → T022) → T023 →
  T024 → T025 → T026.
- US2 needs US1's captured patterns and fill worker; US3 needs US2's choice at `startRun`.

## Parallel examples

- US1: T003, T004, T005, T006, T027 and T028 (now T036) by separate test-author sessions (different
  files); T008 after T001.
- US2: T012, T013, T014 and T030 by separate test-author sessions.

## Implementation strategy

- MVP is US1: a done run is captured and proven with no model call, which is the deterministic
  asset Principle XX asks for. US2 reuses it for new slices; US3 keeps it true across upgrades.
- No recordings change: the base pattern, its template and agents are unchanged.
