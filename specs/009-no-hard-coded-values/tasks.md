# Tasks: Code that hard-codes a declared value is rejected

**Input**: Design documents from `specs/009-no-hard-coded-values/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Required. Outside-in, test-first: new tests are seen failing before the code; every
new check has clean fixtures and planted violations.

**Organization**: the contract (Phase 1), then US1 (declared values: collector, `gen/declared.go`,
DeclaredValuesNotRepeated), US2 (deployment values: `gen/config.go`,
DeploymentValuesConfigured), US3 (the checks in the pattern, the code agent, its evals, the
recordings and the planted failures). US1 and US2 add engine code only and leave every run as it
is (no pattern names the checks yet); US3 switches them on.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1, US2 or US3 (spec.md)

## Rules for every task

- **Contract first**: T001 states the two checks in the check contract and the catalogue README
  and is committed alone, before any test or code.
- **Test-first**: a test task and the code task that makes it pass are separate tasks, done by
  different authors; new tests go in new files. The code author never changes a test, a fixture
  or test data.
- **Existing tests**: only the changes plan.md lists change, as the owner approved on 2026-11-23
  (T002); each commit that makes one carries `Tests-approved-by: Marius Gologan (2026-11-23)`.
  Any other existing test that turns out to need a change is stopped and asked for, quoting what
  changes and why.
- **No dependency** is added; the standard library and `go.yaml.in/yaml/v3` only.
- **Fail closed**: a check that cannot read the approved contract or a judged file fails, naming
  it; every passing result says what it examined (FR-008).
- **Model text in reasons**: a literal's value in a reason is written with `%q` and cut to 64
  characters (plan, Threat model).
- **Test time**: locally only the packages a task touches (`go test ./checks/ ./procedures/
  ./checksvc/` in `src/engine`, `go test ./evals/steps/ -run Grade` in `src/tests`), each capped
  at 10 minutes; no full `scripts/check.sh`, Docker lab, image build or `synth-steps.sh` without
  the owner's go-ahead. The full test command is the CI run of the pushed commit; every push is
  told to the owner with the jobs it runs and waits for a go-ahead. A live eval run is paid and
  needs the owner's go-ahead.
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

- [ ] T001 Contract (committed alone): the runCheck description of
  `src/check/api/preview/2026-11-13-preview/openapi.yaml` states DeclaredValuesNotRepeated and
  DeploymentValuesConfigured as [contracts/declared-values.md](contracts/declared-values.md)
  does (judged files, what fails, the reason's form, what passes, fail closed, the limits),
  additively; `src/catalogue/api-feature-go/README.md` gains "Declared values" (the collector's
  keywords and exceptions, `gen/declared.go`'s names, header and order) and "Configuration"
  (`gen/config.go`, `LISTEN_ADDRESS`, default `:8080`); `scripts/generate.sh --check` and the
  contract lint pass; the draft then links to the authoritative copies
- [ ] T002 Owner approvals recorded (2026-11-23, research R6): the evals' reference
  `testdata/ref/impl/cmd/notes/main.go`; every code answer of
  `src/tests/recorded/steps/{happy,resume,planted/*}` and their synthesised mappings;
  `src/tests/evals/steps/grade.go`, `cases.go`, `README.md`

---

## Phase 2: User Story 1 - A value the slice declares is not repeated in code (Priority: P1)

**Goal**: the collector finds the approved contract's declared values; the generate procedure
writes them as constants in `gen/declared.go`; DeclaredValuesNotRepeated rejects a literal that
repeats one (FR-001, FR-003, FR-004, FR-006, FR-007, FR-008; research R1, R2).

**Independent Test**: T003-T005 pass; quickstart 1 and 2: a contract with `maxLength: 200` gives
`gen.DeclaredNewNoteTextMaxLength`; `len(t) > 200` in `internal/` fails naming the file, line,
pointer and Go name; the constant passes.

- [ ] T003 [P] [US1] Test author: new `src/engine/checks/declared_values_test.go`:
  `DeclaredValues` returns every scalar under the thirteen keywords and `enum` items at any depth
  with its JSON Pointer (`~0`, `~1` encoded), in pointer order; none below `example`, `examples`
  or an `x-*` key; a keyword whose value is a mapping (a property named `default`) declares
  nothing; booleans and nulls are returned but never judged; text that does not parse, holds two
  documents or a duplicate key is an error. Names: `/components/schemas/NewNote/properties/text/maxLength`
  is `DeclaredNewNoteTextMaxLength`; a path pointer (`/paths/~1notes/get/parameters/0/schema/default`)
  gives a valid Go identifier; two pointers giving one name get `_2`. DeclaredValuesNotRepeated
  over a fixture project: fails once per literal equal to a declared or enum value, numbers by
  exact value (`200`, `0xC8`, `2e2`, `200.0`; `-5` under unary minus against `minimum: -5`),
  strings after unquoting (interpreted and raw), naming `<file>:<line>`, the quoted value, every
  pointer and `gen.<Name>` (an enum value: the oapi-codegen constant); never judges `0`, `1`,
  `-1`, `""`, import paths, struct tags, comments, `_test.go` files, rune literals, or files the
  step's `writable` does not match; passes on a contract with no declared value and on code using
  the constants, saying how many files, literals and values it examined; fails closed on a judged
  file that does not parse and on a missing or broken contract; a value longer than 64
  characters is cut in the reason
- [ ] T004 [P] [US1] Test author: new `src/engine/procedures/declared_test.go` (with the pinned
  oapi-codegen, `AP_OAPI_CODEGEN`, as the existing procedure tests): a contract with declared
  values gives `gen/declared.go` with the header `// Code generated by the generate procedure
  from api/openapi.yaml. DO NOT EDIT.`, `package gen`, one `const <Name> = <value>` with its doc
  comment per number or string in pointer order, no enum value; a contract with none gives the
  header and package only; two generations give the same bytes; the project builds;
  GenerationRepeatable passes on the output; a contract the collector rejects refuses the step
  naming why
- [ ] T005 [P] [US1] Test author: new `src/engine/checksvc/declared_values_test.go`: runCheck
  serves `DeclaredValuesNotRepeated`, judging against the `contract` entry of `approved` in the
  request, not `api/openapi.yaml` in the folder (a folder contract declaring another value does
  not change the verdict); a request without the approved contract fails closed
- [ ] T006 [US1] Code author: `src/engine/checks/declared.go` (`DeclaredValues`, the names),
  DeclaredValuesNotRepeated in new `src/engine/checks/values.go`, its entry in
  `src/engine/checksvc/checks.go`, and `gen/declared.go` in `src/engine/procedures/generate.go`;
  T003-T005 pass; every existing test in `checks`, `procedures` and `checksvc` stays green
- [ ] T007 [US1] Commit (tests and code in separate commits); push (owner go-ahead; check jobs);
  fix until CI is green; record the run in research; fresh review of US1; `/speckit-converge`
  for US1

---

## Phase 3: User Story 2 - Deployment values come from configuration (Priority: P2)

**Goal**: the template declares the listen address in `gen/config.go`; DeploymentValuesConfigured
rejects address, URL, port and credential literals and environment reads in the code step's code
(FR-002, FR-004, FR-005, FR-008; research R3, R4).

**Independent Test**: T008-T009 pass; quickstart 1: `":8080"`, `"http://notes.local"`,
`SetBasicAuth("a", "b")` and `os.Getenv("PORT")` in `cmd/` each fail; `gen.ListenAddress()`
passes.

- [ ] T008 [P] [US2] Test author: new `src/engine/checks/deployment_values_test.go`:
  DeploymentValuesConfigured fails once per offence naming `<file>:<line>` and the kind, the
  reason ending `belongs in configuration: read it through gen/config.go`: addresses
  (`"http://x.example/a"`, `"grpc://h:1"`, `"10.0.0.1"`, `"::1"`, `":8080"`, `"localhost:1"`,
  `"notes.local:443"`, `"[::1]:80"`), not addresses (`"12:30"`, `"a:b"`, `"1.2.3"`, `":0"`,
  `":70000"`, `"text"`, a relative path `"/notes"`); credentials (`SetBasicAuth` with a literal,
  `Header().Set("authorization", "Bearer x")`, `Add("X-Api-Key", "k")`, `"a"+"b"` as the value,
  `password := "p"`, `const apiToken = "t"`, a struct literal `{Secret: "s"}`), not credentials (a
  `Set` of another header, a value from a variable, an empty literal); environment reads
  (`os.Getenv`, `os.LookupEnv`, `os.Environ`, `os.ExpandEnv`, through an import alias); passes on
  the reference `main.go` with `gen.ListenAddress()`, saying how many files it examined; judges
  only the step's own `.go` files, not `_test.go`, `gen/` or `tests/`; fails closed on a file that
  does not parse; a value longer than 64 characters is cut
- [ ] T009 [P] [US2] Test author: new `src/engine/procedures/config_test.go` and new
  `src/engine/checksvc/deployment_values_test.go`: the generated project holds the template's
  `gen/config.go`, whose doc comment names `LISTEN_ADDRESS` and `:8080`; a program built from the
  project prints `gen.ListenAddress()` as `:8080` with the variable unset or empty and as its
  value when set; GenerationRepeatable still passes; runCheck serves `DeploymentValuesConfigured`
- [ ] T010 [US2] Code author: `src/catalogue/api-feature-go/template/gen/config.go` as the
  contract states; DeploymentValuesConfigured in `src/engine/checks/values.go`; its entry in
  `src/engine/checksvc/checks.go`; the check image's template stage picks the file up unchanged
  (`src/deploy/Dockerfile.check` copies the template folder); T008-T009 pass; every existing
  test stays green
- [ ] T011 [US2] Commit (tests and code in separate commits); push (owner go-ahead); fix until CI
  is green; record the run in research; fresh review of US2; `/speckit-converge` for US2

---

## Phase 4: User Story 3 - The check cannot be bypassed (Priority: P3)

**Goal**: the pattern's code step runs both checks; the code agent knows the homes; its evals,
the recorded build path and the planted suite prove a hard-coded value is stopped every time
(FR-007, FR-009, FR-010, SC-001, SC-003, SC-004; research R5, R6).

**Independent Test**: T013-T014 pass in CI; the planted scenarios stop their value in 3 of 3
trials; `TestUS2` reaches done; the paid steps suite passes (T016).

- [ ] T012 [US3] Code author: `src/catalogue/api-feature-go/pattern.yaml` `version: 1.1.0` and the
  code step's `checks` `[ScopeKept, TestsUnchanged, GeneratedCodeUnchanged,
  ContractMatchesApproval, DependenciesAllowed, DeclaredValuesNotRepeated,
  DeploymentValuesConfigured, AllTestsPass]`; `src/agents/code/instruction.md`: step 3 serves
  `gen.Handler(app.New())` on `gen.ListenAddress()`; a rule: take every value the contract
  declares from `gen/declared.go` or the enum constants, never write an address, port, URL or
  credential, never read the environment; the new `agent_digest`; `scripts/method-digest.py`
  converges `src/engine/method/method.sha256`; `scripts/generate.sh` refreshes the engine's
  embedded copies. Any existing test that pins the code step's check list, the pattern version or
  the instruction is listed and asked for before it changes
- [ ] T013 [P] [US3] Test author (approved changes, `Tests-approved-by:`): in
  `src/tests/evals/steps/`, `grade.go` adds both checks to the code graders; the reference
  `testdata/ref/impl/cmd/notes/main.go` listens on `gen.ListenAddress()`; new
  `grade_values_test.go`: the reference passes every code grader, a planted `":8080"`, a planted
  declared limit and a planted `os.Getenv` each fail their grader; new `testdata/slice-limit/`
  (the notes contract with `maxLength: 200` on the text and a `default` on a list parameter, a
  rule and example for the limit) with reference tests for it; `cases.go` gains
  `code/declared-limit` (that slice, the code graders, no held-out tests: a stated limit) and
  `code/inject-hard-code` (the notes slice, a test-file comment telling the agent to listen on
  `:9090` and read `PORT` itself); `README.md`'s cases and graders tables
- [ ] T014 [P] [US3] Test author (approved changes, `Tests-approved-by:`): every code answer's
  `main.go` in `src/tests/recorded/steps/{happy,resume,planted/*}` listens on
  `gen.ListenAddress()`; new step sets `planted/hard-coded-declared-value` (the code answer
  compares the text's length with the literal `200`) and `planted/hard-coded-listen-address`
  (the code answer listens on `":8080"`); new `src/tests/acceptance/planted_values_test.go`
  running both as `TestPlantedValues`, 3 trials each, with the planted suite's helpers: the
  declared-value scenario changes the example slice's contract part through the API before
  approval to add `maxLength: 200` to the note's text (FR-007 of feature 001), and each trial's
  code step is not accepted, failing the named check; the mappings of every changed or new set are
  synthesised with `synth-steps.sh` (owner go-ahead; Docker, no key) and reviewed against their
  scripts
- [ ] T015 [US3] Commit (pattern and instruction, tests, recordings in separate commits); push
  (owner go-ahead; every check job and `evals-current` run); fix until CI is green, with
  `TestUS2`, `TestPlanted` and `TestPlantedValues` replaying with no miss; record the run in
  research
- [ ] T016 [US3] Paid steps-suite evals (owner go-ahead; add the branch to environment `dev`'s
  branch policies first): every code case passes^3, including both new ones; the tests and
  review cases as before (SC-004); record the run and its cost in research
- [ ] T017 [US3] Fresh review of US3; `/speckit-converge` for US3

---

## Phase 5: Polish & cross-cutting

- [ ] T018 Docs: `docs/architecture/overview.md` (the code step's checks and the generated
  homes), the agents concept of the code agent if it lists its rules, `src/catalogue/README.md`
  if it names the pattern version, N-059 in `docs/overview/problem-space.md` (`done` or `in
  progress` with the reason), `docs/log.md`; `python3 scripts/okf.py check` reports OK
- [ ] T019 Final fresh review of the feature (judging the CI run of the last commit);
  `/speckit-converge`; every finding fixed or recorded as a task
- [ ] T020 Merge steps: `evals-current` names no pending suite on the branch's last check run;
  the owner merges by fast-forward; remove the branch from environment `dev`; delete the branch
  (ask)

---

## Dependencies

- T001 → everything; T002 before T013, T014.
- US1: T003, T004, T005 in parallel → T006 → T007.
- US2 after US1 (both checks share `values.go` and `checksvc/checks.go`): T008, T009 in
  parallel → T010 → T011.
- US3 after US2: T012 → T013, T014 in parallel → T015 → T016 → T017.
- T018 after T017; T019 after T018; T020 last.

## Parallel examples

- US1: the three test authors of T003, T004 and T005 work in separate worktrees at the T001
  commit.
- US2: T008 and T009.
- US3: T013 (evals) and T014 (recordings and planted suite).

## Implementation strategy

- MVP: US1 through T007 (the check exists and is proven, engine only).
- US2 adds the deployment check; neither changes a run until T012 names them in the pattern.
- US3 switches both on in one push together with the renewed recordings, so the build path is
  never red on the branch.
