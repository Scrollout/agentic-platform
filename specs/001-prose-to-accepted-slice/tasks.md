# Tasks: From a description to accepted working software, one slice at a time

**Input**: Design documents from `specs/001-prose-to-accepted-slice/` (revised after fresh review round 1)

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Required. Contract-first and Outside-in put contracts and failing tests before code;
every check proves it can fail.

**Organization**: by user story; within a story: contract, generated code, failing tests, code,
fresh review.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1 or US2

## Rules for every task

- **Test-first**: a test task and the implementation task that makes it pass are separate tasks,
  done by different authors. The implementer may not change the tests (constitution, Tests change
  only through a fresh agent).
- **Fresh review**: each story ends with a fresh-review task, done by a reviewer on another model in
  its own copy of the repository, following Review converges (at most 3 rounds).
- **Gate**: `/speckit-analyze` (fresh review) passed in round 2 on 2026-11-12, before T001; it is
  run again after any change to spec, plan or tasks. Re-run on 2026-11-12 after T012: round 1
  failed (3 HIGH: A1 oasdiff, A2 `cancelled` outcome, A3 npm pins), fixed in `f1d80c2`; round 2
  (Sonnet, fresh) passed: every finding resolved, no regressions.
- **Done means**: tests pass, `python3 scripts/okf.py check` reports OK, `docs/log.md` records the
  change.

## Path Conventions

All code under `src/`. Go modules: `src/engine`, `src/slice`, `src/cli`, `src/page`, `src/tests`.

---

## Phase 1: Setup (Shared Infrastructure)

- [X] T001 Create the layout from plan.md, a README.md in each `src/` folder stating its purpose, and `src/go.work` listing the five modules
- [X] T002 Add the Go 1.26 module files `src/engine/go.mod`, `src/slice/go.mod`, `src/cli/go.mod`, `src/page/go.mod`, `src/tests/go.mod`, and Go ignore patterns to `.gitignore`
- [X] T003 [P] Write planted-violation and clean fixtures for the root-layout check in `scripts/tests/layout/` and a test that runs the check on both (fails until T004)
- [X] T004 Write the root-layout check `scripts/layout-check.py` against the constitution's root allowlist until T003 passes
- [X] T005 [P] (ruleset and its tests in one task; tests by a separate author, see plan Exceptions) Write the Spectral ruleset `src/engine/api/ruleset.yaml` (operation ids, descriptions, required `api-version`, paginated lists, stable error codes) with a planted-violation fixture and a test
- [X] T006 [P] Pin every image and tool by version and full digest in `src/deploy/images.lock` (Docker Agent, golang, alpine, nginx, oasdiff, Schemathesis; oapi-codegen, godog and Spectral by version). The OpenSSF Scorecard runs for Docker Agent and oasdiff need GitHub, which is not used for now; they move to T063
- [X] T007 [P] Add local git hooks in `scripts/hooks/` (no hosted forge for now): `pre-commit` runs the fast checks for what is staged (the full suite is `scripts/check.sh`), `commit-msg` refuses a change to a protected path (constitution, `scripts/`, `src/deploy/`, tests, test data, Go module files) without an `Approved-by:` trailer, and a change to Go code together with its tests without a `Tests-approved-by:` trailer; document them in `docs/playbooks/protect-changes-locally.md`
- [X] T008 [P] Dependency updates: no hosted forge for now, so automated update pull requests are deferred (Principle XIII exception approved 2026-11-12); until then, a monthly manual review of `src/deploy/images.lock` and the Go modules
- [X] T009 Create the local check script `scripts/check.sh` (no hosted CI for now): check tests, layout check, `okf.py check`, markdownlint, Spectral, the generated-code drift check (T012), `go vet` and `go test` for every module in the pinned Go container, dependency and licence scans (govulncheck, go-licenses), secret scanning
- [X] T010a [P] Have a separate test author write planted-violation tests for the commit-msg hook's two rules (protected paths, code-plus-tests) in `scripts/tests/test_hooks.py`
- [X] T010 [P] Add `NOTICE` at the repository root listing the Apache-2.0 components and their notices

**Checkpoint**: an empty but checked project; `scripts/check.sh` fails on a planted layout or contract violation.

---

## Phase 2: Foundational (Blocking Prerequisites)

**⚠️ CRITICAL**: no user story work starts until this phase is complete.

- [X] T011 Review the draft contracts with the project owner, then move them to their provider at `src/engine/api/preview/2026-11-12-preview/` (`openapi.yaml`, `slice.schema.json`, `slice-defs.schema.json`, `slice-proposal.schema.json`, `pattern.schema.json`, `run-event.schema.json`), mark the copies in `specs/001-prose-to-accepted-slice/contracts/` as drafts (removed after the 2026-11-12 analyze review; the terminal contract moved to `src/cli/contract.md`), and record the review in the commit
- [X] T011a Apply the eight non-blocking suggestions from contract review round 2 (wording of `parts`, `Worker.endpoint` pattern, 404/403 descriptions, `Run` status/outcome rule, `Difference` rendering, `info.contact`, last-page token, `OperationKind` note) in `src/engine/api/preview/2026-11-12-preview/`, or close each with a reason
- [X] T011c Have a fresh agent review the T011a contract changes (commit `91f2821`, including the new `Run` status/outcome rule) against the round-2 approval, as for T011 (analyze finding B4). Done 2026-11-12 (Sonnet): approved, no blocking findings; the rule holds for all 40 status, outcome and `ended_at` combinations; suggestions R3-1 to R3-4 and R3-6 are in T011d, R3-5 is closed (FR-015 now lists `cancelled`), R3-7 closed: no issue found
- [X] T011d (reviewed 2026-11-12 with T011b by a fresh agent, Sonnet: approved; suggestions F1 to F3 placed in T022, T045 and T011e) Apply or close the T011c suggestions in the engine contract, reviewed like T011: state "exists but not for the caller is reported as not found" in data-model.md and say what `registerWorker`'s 404 means (R3-1); name `decision` in `Difference.left`/`right` and say what an absent side means (R3-2); decide whether `Worker.endpoint` needs its trailing `/` and confirm it fits the Compose service names (R3-3); add the status/outcome combinations to T030's conformance tests (R3-4); say a client stops reading events when `next_page_token` is absent (R3-6)
- [X] T011b Make the `SliceId` and `RunId` path parameters in `src/engine/api/preview/2026-11-12-preview/openapi.yaml` reference `slice-defs.schema.json#/$defs/slice_id` and `run_id` instead of repeating their patterns (single owner for rules; found in T012); a contract change, so it is reviewed like T011
- [X] T012 (generation and its tests in one task; tests by a separate author, see plan Exceptions) Bundle the contract into one document with Redocly CLI (ADR 0004), then generate the engine's Go server interfaces and types from the bundle (oapi-codegen v2.8.2, strict server) into `src/engine/api/gen/` and the client into `src/cli/api/gen/` and `src/page/api/gen/`, with `scripts/generate.sh`; generated code is not committed (constitution, Clean tree), so the drift check is `scripts/generate.sh --check`, which `scripts/check.sh` runs after regenerating and which fails on any difference from the contract (in that run it proves generation is deterministic; the guarantee is that generated code is never committed, and `test_generate.py` proves `--check` fails on drift); planted-violation tests by a separate author in `scripts/tests/test_generate.py`
- [X] T012b Pin the Node tools (Redocly CLI, Spectral, markdownlint-cli2) with their whole dependency tree in `scripts/tools/package-lock.json`, run them without network in the pinned Node image with `scripts/npm-tool.sh`, and move every Node tool pin out of scripts into that lock (analyze finding A3); tests by a separate author in `scripts/tests/test_npm_tool.py`, `test_ruleset.py` and `test_generate.py`
- [ ] T011e Define each remaining repeated value rule once in `slice-defs.schema.json` and reference it from the engine contract and the other schemas: operation ID `^OP-[0-9]+$`, step name `^[a-z0-9-]+$`, question ID `^Q-[0-9]+$`, rule ID, pattern reference `<id>@<version>`, project name, agent reference, check name, review ID `^R-[0-9]+$`, the slice-or-run target `^(S|RUN)-[0-9]+$` of `Operation.target`, api-version, pattern name, and the SHA-256 `hash` still repeated in `pattern.schema.json` and `run-event.schema.json` (single owner for rules; found in T011b and its review); a contract change, reviewed like T011
- [X] T012a Have a separate test author add the cases T012's tests leave open in `scripts/tests/test_generate.py`: the newest contract version is chosen (stable and preview ordering), no targets exits 0, a missing pin in `src/deploy/images.lock` fails; and for `scripts/npm-tool.sh`: a tool cannot reach the network, a second run skips `npm ci`, a changed lock gets a new cache, markdownlint-cli2 fails on a planted Markdown violation. Done 2026-11-12 (Sonnet, 9 cases; 38 script tests pass). Left untested, closed: a stable and a preview with the same date (ADR 0004 forbids it); `--check` against a newer version, two targets sharing one bundle, a missing `node` pin on a warm cache and an unusual `NPM_TOOL_MOUNT` path (each covered by the same code path as a tested case); `markdownlint-cli2 --fix` (the scripts never fix, and the repository is mounted read-only)
- [ ] T013 [P] Write failing schema tests in `src/slice/schema_test.go` for fixtures in `src/slice/testdata/`: a `state` rule without `precondition`, an `event` or `unwanted` rule without `trigger`, an `option` rule without `feature`, a goal id not matching `^G-[0-9]+$`, more than 10 questions, an answer over 200 characters, a proposal containing `approvals`
- [ ] T014 [P] Write failing run-store tests in `src/engine/store/store_test.go`: events appended with increasing `seq`, an artifact stored once under the SHA-256 of its content, no event ever rewritten, erasure removes a slice's records and artifacts
- [ ] T015 [P] Write failing engine-skeleton tests in `src/engine/api/skeleton_test.go`: a missing `api-version` returns 400 `MissingApiVersionParameter`, an unknown one 400 `UnsupportedApiVersionValue`, a call without a token 401, more than 20 requests per second per token 429, a reused `Idempotency-Key` with a different body 409 `IdempotencyKeyReused`, `GET /providers/Agentic.Engine/operations` lists every operation, and operations can be followed and cancelled
- [ ] T016 Implement schema validation in `src/slice/schema.go` with santhosh-tekuri/jsonschema v6 until T013 passes
- [ ] T017 Implement the run store in `src/engine/store/store.go` until T014 passes
- [ ] T018 Implement the engine skeleton in `src/engine/api/` (server entry point, version check, token authentication, rate limit, idempotency store kept 24 hours, operation store with retries and visible failures, provider operations list) until T015 passes
- [ ] T019 [P] Write the gateway on the official nginx image in `src/deploy/gateway/` (template that injects the credential at start, only the provider host allowed, no header or body logging) and a test that a request to another host is refused
- [ ] T020 [P] Write the recorded-model replay server for tests in `src/tests/recorded/` and record the example slice's model responses
- [ ] T021 [P] Write the Dockerfiles in `src/deploy/` for the agent image (Docker Agent binary from the pinned image, Go toolchain, non-root, `TELEMETRY_ENABLED=false`), engine, check container, CLI and page, each with an ignore rule that excludes tests and evals
- [ ] T022 Write `src/deploy/compose.yaml` (worker service names only lowercase letters, digits and hyphens, as `Worker.endpoint` requires): an internal network with no egress, the gateway as the only container with egress, `runtime: runsc` with CPU, memory and process limits for step and check containers, read-only root file systems, no capabilities, `no-new-privileges`, temp folders with `exec`, no container-engine socket anywhere
- [ ] T023 Write the launcher `src/deploy/ap` (commands `up`, `down`, `build-workers`, `page`, `test`; everything else passed to the terminal client): verify image signatures before start, create per-session and per-container tokens, pass the credentials file to the gateway alone
- [ ] T024a Add an oasdiff breaking-change check to `scripts/check.sh` between each service's newest contract version and the previous one (Principle XV), with a planted-violation test by a separate author; it reports "one version, nothing to compare" until a second version exists
- [ ] T024 Add `scripts/e2e.sh` (no hosted CI for now): runs the Compose stack with gVisor against the recorded model and the `src/tests` contract and acceptance suites, never mounting the engine socket into a container; `scripts/check.sh` calls it

**Checkpoint**: `src/deploy/ap up` starts the platform; the engine answers `GET /runs?api-version=2026-11-12-preview` with an empty list; `scripts/e2e.sh` runs end to end.

---

## Phase 3: User Story 1 - Describe a use case and approve its slice (Priority: P1) 🎯 MVP

**Goal**: prose becomes a checked, linked slice; the developer answers questions, changes parts and approves goals, rules and contract.

**Independent Test**: quickstart step 2.

### Evals and tests for User Story 1 ⚠️

- [ ] T025 [P] [US1] Write the analyst's eval suite in `src/tests/evals/analyst/`: the example description must yield a schema-valid, fully linked proposal with at most 10 questions in 3 of 3 trials; adversarial cases with instructions injected into the description must not change platform-owned fields or skip checks
- [ ] T026 [P] [US1] Write failing traceability tests in `src/slice/trace_test.go`: a goal with no rule, a rule naming a missing goal, a rule using a term id not in `terms`, a rule with no scenario under a `Rule:` block tagged with its id, and an operation whose `x-rules` names a missing rule are each rejected with part and reason; a fully linked fixture passes
- [ ] T027 [P] [US1] Write failing EARS-derivation tests in `src/slice/ears_test.go`: one fixture per pattern produces the exact EARS sentence; derived `text` is never taken from input
- [ ] T028 [P] [US1] Write failing pattern-fit tests in `src/engine/checks/fit_test.go`: the notes slice fits `api-feature-go`; a slice whose contract has a non-HTTP operation, or whose rules name another system, does not, with the failing condition named
- [ ] T029 [P] [US1] Write failing tests in `src/engine/slices/slices_test.go`: part hashes are SHA-256 of RFC 8785 canonical JSON; the `rules` part covers rules, examples and terms; approving a failing part or a stale hash fails with `StaleContentHash`; changing an approved part withdraws its approval; a resubmitted description offers the existing slice; more than one use case or an empty description asks to narrow it; a first contract with an error-level oasdiff finding against an empty document, and a changed contract with a breaking change against the previously approved one, are rejected naming the change (FR-004)
- [ ] T030 [P] [US1] Write Schemathesis conformance tests for `createSlice`, `listSlices`, `getSlice`, `replaceGoals`, `replaceRules`, `replaceContract`, `deleteSlice`, `answerQuestions`, `approvePart`, `getOperation`, `cancelOperation` and `listProviderOperations` in `src/tests/contract/slices_test.go`, against the recorded model, plus every `Run` status and outcome combination (with and without `ended_at`) against the contract's status/outcome rule (T011d, R3-4)
- [ ] T031 [P] [US1] Write the acceptance test `src/tests/acceptance/us1_test.go` for spec scenarios US1.1 to US1.6, including the refusal of `startRun` on an unapproved slice (`NotApproved`) and on a slice the pattern does not fit (`PatternDoesNotFit`)

### Implementation for User Story 1

- [ ] T032 [P] [US1] Implement EARS derivation in `src/slice/ears.go` until T027 passes
- [ ] T033 [US1] Implement traceability checks in `src/slice/trace.go`, parsing examples with cucumber/gherkin, until T026 passes
- [ ] T034 [US1] Write the first pattern's declaration `src/catalogue/api-feature-go/pattern.yaml` (conditions, sources, steps contract-check, generate, tests, code, review; writable paths; allowed and forbidden dependencies; limits 3 and 3; budgets with a 30-minute run), validated against `pattern.schema.json`
- [ ] T035 [US1] Implement the pattern-fit check in `src/engine/checks/fit.go` until T028 passes
- [ ] T036 [US1] Write the analyst agent `src/agents/analyst.yaml` (Haiku through the gateway, no file-writing tools, `structured_output` bound to `slice-proposal.schema.json`) until T025 passes
- [ ] T037 [US1] Implement proposal, answers, changes, approvals and deletion in `src/engine/slices/` (run the analyst, run T016, T033, T035, Spectral and oasdiff on the contract, compute part hashes) until T029 passes
- [ ] T038 [US1] Implement the slice operations and the `startRun` refusals of the generated interface in `src/engine/api/slices.go` until T030 and the refusal parts of T031 pass
- [ ] T039 [US1] Implement `describe`, `questions`, `show`, `edit`, `approve`, `slices`, `cancel` and `delete` (summary and typed confirmation) in `src/cli/` per `src/cli/contract.md` until T031 passes
- [ ] T040 [US1] Fresh review of Story 1 in an isolated copy; every finding fixed, made a task, or closed with a reason

**Checkpoint**: Story 1 works on its own; quickstart step 2 passes.

---

## Phase 4: User Story 2 - Build accepted software from an approved slice (Priority: P2)

**Goal**: an approved slice is built into a new Go project, with checks deciding every step.

**Independent Test**: quickstart steps 3 to 5.

### Evals and tests for User Story 2 ⚠️

- [ ] T041 [P] [US2] Write the step agents' eval suites in `src/tests/evals/steps/`: tests, code and review agents on the example slice in 3 of 3 trials, plus injected instructions in earlier artifacts that try to widen scope or edit tests
- [ ] T042 [P] [US2] Write planted-violation and clean fixtures for every check in `src/engine/checks/testdata/` (scope, tests unchanged, dependencies against the pattern's allowed and forbidden lists, tests fail against the stub, all tests pass, godog `--strict`, generation drift, contract hash equals the approved hash, evidence-backed finding) and a test that runs each check on both
- [ ] T043 [P] [US2] Write failing machine tests in `src/engine/machine/machine_test.go`: a step is accepted only when all checks pass; DONE is ignored; a check that errors counts as failed; 3 rejected attempts or 3 review rounds hand over; a later review round considers only open findings and changed lines; token, cost and time budgets hand over; a hand-over expires after 14 days; cancelling a run's operation ends it `cancelled`; one active run at a time (`RunActive`); every run ends in one outcome; every event names model, agent digest, pattern version, inputs, reuse key and the refs it concerns
- [ ] T044 [P] [US2] Write failing reuse, resume and replay tests in `src/engine/machine/reuse_test.go`: equal inputs give the same reuse key; an accepted result is reused without running the step; resume repeats no accepted step; a replay's artifacts are byte-identical; comparison lists only differing fields
- [ ] T045 [P] [US2] Write Schemathesis conformance tests (including `registerWorker` with `http://svc:8080`, `http://svc:8080/` accepted and a path after the port refused) for `startRun`, `resumeRun`, `replayRun`, `acceptHandOver`, `compareRuns`, `listRuns`, `getRun`, `listRunEvents`, `getArtifact` and `registerWorker` (each with its allowed and refused token kinds) in `src/tests/contract/runs_test.go`
- [ ] T046 [P] [US2] Write the planted-failure suite `src/tests/acceptance/planted_test.go` (five scenarios, 3 trials each) and the acceptance test `src/tests/acceptance/us2_test.go` for spec scenarios US2.1 to US2.10
- [ ] T047 [P] [US2] Write failing tests for the run page in `src/page/page_test.go`: it sends only GET requests, binds to `127.0.0.1`, rejects an unexpected Host header, and shows a run's events and a comparison

### Implementation for User Story 2

- [ ] T048 [P] [US2] Implement the checks in `src/engine/checks/` until T042 passes
- [ ] T049 [P] [US2] Write the step agents `src/agents/tests.yaml`, `src/agents/code.yaml` and `src/agents/review.yaml` (Haiku through the gateway, `safety: autonomous` inside the gVisor container; review findings as JSON with rule id, file and line) until T041 passes
- [ ] T050 [US2] Implement the fixed procedures in `src/engine/procedures/`: contract check (copies the approved contract, verifies its hash and re-lints it), generate (oapi-codegen, vendored dependencies resolved offline, generated stub, run twice and compared), review gate (evidence-backed findings, narrowing rounds)
- [ ] T051 [US2] Implement the state machine, budgets, expiry and the one-active-run rule in `src/engine/machine/machine.go` until T043 passes
- [ ] T052 [US2] Implement reuse, resume, replay and comparison in `src/engine/machine/reuse.go` until T044 passes
- [ ] T053 [US2] Implement worker registration and the per-slice step containers in `src/engine/api/workers.go` and the launcher's `build-workers` (only each step's writable paths mounted writable, under gVisor), and the new project folder with its own version history
- [ ] T054 [US2] Implement the run operations in `src/engine/api/runs.go` until T045 passes
- [ ] T055 [US2] Implement `build`, `resume`, `replay`, `take-over`, `compare`, `artifact`, `runs` and `run` in `src/cli/` and the launcher's `test planted` until T046 passes
- [ ] T056 [US2] Implement the read-only run page in `src/page/` until T047 passes
- [ ] T057 [US2] Fresh review of Story 2 in an isolated copy; every finding fixed, made a task, or closed with a reason

**Checkpoint**: Stories 1 and 2 work; quickstart steps 3 to 5 pass; the walking skeleton runs end to end through `scripts/e2e.sh`.

---

## Phase 5: Polish & Cross-Cutting Concerns

- [ ] T058 [P] Sign every built image with a local key and attach an SBOM and build provenance in `scripts/release.sh`; the launcher verifies them at start (T023)
- [ ] T059 [P] Run the evals (T025, T041) from `scripts/check.sh` on every change to a prompt, agent definition, model or Docker Agent version, with a test-scoped API key that has a provider spending limit
- [ ] T060 [P] Record decision records in `docs/decisions/`: Go as the platform's stack, Docker Agent's `serve api` as the integration mode (ADR 0002 follow-up), the run store format, the slice format (structured EARS rules and Gherkin examples)
- [ ] T061 [P] (closes, before T066, the component concepts for tools adopted in earlier tasks) Add `docs/components/` concepts for Docker Agent, godog, cucumber/gherkin, oapi-codegen (with its runtime library), Redocly CLI, Spectral, oasdiff, jsonschema, Schemathesis, nginx and gVisor, and an engine API concept under `docs/apis/`
- [ ] T062 [P] Add the terms Slice, Pattern, Catalogue, Run, Step, Check and Hand-over to `docs/overview/glossary.md`
- [ ] T063 [P] Re-run the OpenSSF Scorecard for every adopted tool before release and update research.md (D22)
- [ ] T064 Update `docs/architecture/overview.md`, set the problem-space rows this spec serves to `done` only as their tests pass, and record the change in `docs/log.md`
- [ ] T065 Run quickstart.md end to end on a clean machine and record a successful build's time against SC-007
- [ ] T066 Final fresh review of the whole feature before reporting it done; then `python3 scripts/okf.py check` must report OK

---

## Dependencies & Execution Order

- **Setup (Phase 1)**: none; T004 after T003; T010a after T007 (it tests T007's hook).
- **Foundational (Phase 2)**: after Setup. T011 before T011a, T011c, T011b and T011d; T011b and T011d before T015
  and T018 (it changes generated types); T011 before T012; T012 before T012a and before every task
  that uses the generated code; T024a any time after T012; T016, T017, T018 after their tests T013, T014, T015.
- **US1 (Phase 3)**: after Foundational. Tests T025–T031 before implementation T032–T039.
- **US2 (Phase 4)**: after Foundational and T034 (the pattern) and T037 (approved slices). Tests
  T041–T047 before implementation T048–T056. T050 before T051; T053 before T054.
- **Polish (Phase 5)**: alongside each story's completion; T066 last.

### Parallel Opportunities

- Setup: T003, T005, T006, T007, T008, T010.
- Foundational: T013, T014, T015 together; T019, T020, T021 together.
- US1: T025–T031 together; then T032 with T034.
- US2: T041–T047 together; T048 and T049 together.

## Parallel Example: User Story 1

```text
T025 analyst evals        src/tests/evals/analyst/
T026 traceability tests   src/slice/trace_test.go
T027 EARS tests           src/slice/ears_test.go
T028 pattern-fit tests    src/engine/checks/fit_test.go
T029 slice tests          src/engine/slices/slices_test.go
T030 contract tests       src/tests/contract/slices_test.go
T031 acceptance test      src/tests/acceptance/us1_test.go
```

## Implementation Strategy

1. Phases 1 and 2: a checked, empty platform that runs end to end through `scripts/check.sh` and `scripts/e2e.sh`.
2. Phase 3 (US1): stop and validate quickstart step 2.
3. Phase 4 (US2): stop and validate quickstart steps 3 to 5: the first walking skeleton.
4. Phase 5 alongside, with the final fresh review last.
