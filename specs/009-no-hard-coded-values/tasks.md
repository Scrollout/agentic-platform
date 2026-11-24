# Tasks: Code that hard-codes a declared value is rejected

**Input**: Design documents from `specs/009-no-hard-coded-values/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Required. Outside-in, test-first: new tests are seen failing before the code; every
new check has clean fixtures and planted violations.

**Organization**: the contract (Phase 1), then US1 (declared values: collector, `gen/declared.go`,
DeclaredValuesGenerated, DeclaredValuesNotRepeated), US2 (deployment values: `gen/config.go`,
DeploymentValuesConfigured), US3 (the checks in the pattern, the code agent, its evals, the
recordings and the planted failures). US1 and US2 add engine code that no pattern names yet, so
no step is judged by the new checks; the method identity still changes with each such commit
(feature 006), so earlier accepted steps are judged again from then on. US3 switches the checks
on.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1, US2 or US3 (spec.md)

## Rules for every task

- **Contract first**: T001 states the two checks in the check contract and the catalogue README
  and is committed alone, before any test or code.
- **Test-first**: a test task and the code task that makes it pass are separate tasks, done by
  different authors; new tests go in new files. The code author never changes a test, a fixture
  or test data.
- **Existing tests**: only the changes plan.md lists by name change, each approved by the owner on
  2026-11-23 (T002); each commit that makes one carries `Tests-approved-by: Marius Gologan
  (2026-11-23)`. Any other existing test that turns out to need a change is stopped and asked
  for, quoting what changes and why; no approval covers a class of tests.
- **Method identity**: every commit that changes a file `src/engine/method/method.json` lists
  (the engine, the check contract, the catalogue, the agents) runs `scripts/method-digest.py` and
  commits the converged `src/engine/method/method.sha256` with it.
- **Trailers**: `Approved-by: Marius Gologan (2026-11-12)` is the owner's standing approval to
  commit finished, tested work to protected paths (the commit hook's `Approved-by`); a check
  change or an existing test's change carries its own dated approval (`Tests-approved-by:`).
- **No dependency** is added; the standard library and `go.yaml.in/yaml/v3` only.
- **Fail closed**: a check that cannot read the approved contract or a judged file fails, naming
  it; every passing result says what it examined (FR-008).
- **Model text in reasons**: a declared value in a reason is written with `%q` and cut to 64
  characters; a deployment value's text never appears in a reason (spec FR-004; plan, Threat
  model).
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
  again after any change to spec, plan, tasks or a design artifact. Round 1 (fresh Sonnet, at
  60f0e6a): 1 CRITICAL (F1 US3 code before its tests), 5 HIGH (F2, F3 existing tests not named;
  F4 one author for a grader and its test; F5 a credential's text in a reason; F6 no Go
  interface), 11 MEDIUM, 7 LOW; the owner approved the named test changes, dropped the live
  declared-limit case (backlog T201) and kept collisions as a stated limit; fixed at their source
  (spec FR-002 to FR-004, FR-010, SC-001, SC-002, edge cases; research E3, R1, R2, R4 to R7;
  contract; data model; plan; ADR 0086; these rules and tasks). Round 2 (fresh Sonnet, at
  bfa2f04): F1-F24 fixed but F2's residual; 3 HIGH (N1 floats rendered as `1/10`; N2
  `us2_test.go` pins the pattern version, owner approved its change; N3 an address quoted with its
  password), N4 MEDIUM (`010`, `1_000` read with Go's meaning), N5, N6 LOW; fixed at their source
  (spec FR-004, SC-002; research R1, R4, R6; contract; plan; T002, T003, T008, T013). Round 3
  (fresh Sonnet, at 4217c58): N1-N6 fixed, 0 CRITICAL, 0 HIGH; R3-1 LOW (research R2's enum hint)
  fixed, R3-2 LOW (a dated clarification's narrower wording) left as history. Gate passed. Round 4
  (fresh Sonnet, at 2b1aad3, after T001-T006 and the strategy note): 0 CRITICAL, 0 HIGH; R4-1,
  R4-2 MEDIUM, R4-3 to R4-5 LOW, fixed at their source (T007, T011, Dependencies, quickstart, this
  line). Gate passed. Round 5 (fresh Sonnet, at 03c06c9, after T008-T010, T028, T029): C1 CRITICAL
  (the hook change not in plan Exceptions), I1, I2, D1 MEDIUM, I3-I5, A1, A2 LOW; fixed at their
  source in cad9d44 (plan Exceptions and Constitution Check, structure, Layout; spec clarification,
  FR-007, US3 scenario 1; playbook; Dependencies; T029). Round 6 (fresh Sonnet, at cad9d44): all
  fixed but A1 (reflow on the next touch of the contract) and A2 (informational); N1-N3 LOW, N1 and
  N2 fixed. Gate passed. Round 7 (fresh Sonnet, at 31bbc22, after US2): 0 CRITICAL, 0 HIGH; I1-I4
MEDIUM fixed in 5432f24 (environment read is any use; T031-T033 ordered; trailers), A1-A3, C1 LOW.
Round 8 (at 5432f24): all fixed; N1 LOW (the draft's limit) fixed, N2 LOW left. Gate passed.
Round 9 (fresh Sonnet, at 7c3ab29, after T012-T015 and the runs_ops_test.go approval): 0 findings.
Gate passed. Round 10 (at c4cfd2a, after T036, T037): H1 (T017's cost), M1-M3, L1, L2; fixed in
1a860e5. Round 11 (at 970802a): 0 CRITICAL, 0 HIGH; F1, F2 MEDIUM (plan scope and the approved
list), F3, F6 LOW fixed at their source; F4, F5, F7 optional, left. Gate passed. Round 12 (fresh Sonnet, at 819a442, after T019, T023-T026, T031-T033): R12-1 HIGH
(N-059 `done` before T020, T021 and the head's evals), R12-2, R12-3 MEDIUM, R12-5, R12-6, R12-8
LOW fixed at their source (problem space, log, T019, catalogue README); R12-4 MEDIUM is T040;
R12-7 LOW accepted as the stated limit T032 chose. Round 13 (at a24cfe8): all fixed, 0 CRITICAL, HIGH or
MEDIUM; R13-1, R13-3 LOW fixed, R13-2 LOW left. Gate passed.
Round 14 (fresh Sonnet, at 1da7e99, after T020, T040, T041): 0 CRITICAL, 0 HIGH; 1 MEDIUM, 4 LOW.
F1, F2 (catalogue README wording) are T043, after the merge; F3, F4, F5 fixed at their source.
Gate passed.

---

## Phase 1: Setup (contract first)

- [x] T001 Contract (committed alone): the runCheck description of
  `src/check/api/preview/2026-11-13-preview/openapi.yaml` states DeclaredValuesGenerated,
  DeclaredValuesNotRepeated and DeploymentValuesConfigured as
  [contracts/declared-values.md](contracts/declared-values.md) does (judged files, what fails, the
  reason's form, what passes, fail closed, the limits), additively;
  `src/catalogue/api-feature-go/README.md` gains "Declared values" (the collector's keywords,
  walk, kinds and exceptions; `gen/declared.go`'s header, names and order; the stated limits,
  collisions among them), "Configuration" (`gen/config.go`, `LISTEN_ADDRESS`, default `:8080`)
  and "Deployment values" (the forms the check recognises and its limits); new
  `src/engine/checks/declared.go` and `src/engine/checks/values.go` hold the Go interface of the
  contract draft (types, signatures and doc comments) with bodies that return a failing
  `not implemented` result or error; `scripts/generate.sh` refreshes the embedded contract
  copies; `scripts/method-digest.py` converges; the contract lint passes; the draft then links to
  the authoritative copies
- [x] T002 Owner approvals recorded (2026-11-23, research R6): the evals' reference
  `testdata/ref/impl/cmd/notes/main.go`; every code answer of
  `src/tests/recorded/steps/{happy,resume,planted/*}` and their synthesised mappings;
  `src/tests/evals/steps/grade.go`, `cases.go`, `grade_test.go` (the `obeyed` simulation of
  `code/inject-hard-code`), `README.md`; `src/engine/checksvc/checks_test.go` (`patternCases` and
  fixtures for the three new checks); `src/tests/acceptance/us2_test.go` line 84
  (`api-feature-go@1.1.0`, approved at analyze round 2); `src/engine/api/runs_ops_test.go` lines
  449-450 (`api-feature-go@1.1.0`, approved 2026-11-24, `Tests-approved-by: Marius Gologan
  (2026-11-24)`). No live case with declared values (backlog T201). Addendum (owner, 2026-11-24,
  choosing the T036 and T037 fixes, whose stated steps named these changes): the three analyst
  recordings `src/tests/recorded/recordings/mappings/analyst-*.json` renewed by the recorded
  README's text replacement (the analyst instruction's new paragraph and the analyst digest,
  answers byte-identical), and every step set's tests-step mappings synthesised again (the tests
  agent's instruction changed); trailer `Tests-approved-by: Marius Gologan (2026-11-24)`

---

## Phase 2: User Story 1 - A value the slice declares is not repeated in code (Priority: P1)

**Goal**: the collector finds the approved contract's declared values; the generate procedure
writes them as constants in `gen/declared.go`, which DeclaredValuesGenerated keeps true;
DeclaredValuesNotRepeated rejects a literal that repeats one (FR-001, FR-003, FR-004, FR-006,
FR-007, FR-008; research R1, R2, R7).

**Independent Test**: T003-T005 pass; quickstart 1 and 2: a contract with `maxLength: 200` gives
`gen.DeclaredNewNoteTextMaxLength`; `len(t) > 200` in `internal/` fails naming the file, line,
pointer and Go name; the constant passes.

- [x] T003 [P] [US1] Test author: new `src/engine/checks/declared_values_test.go`:
  `DeclaredValues` returns every scalar under the thirteen keywords and `enum` items at any depth
  with its JSON Pointer (`~0`, `~1` encoded, sequence items by index), in document order; none
  below `example`, `examples` or an `x-*` key (a property so named included); a keyword whose
  value is a mapping (a property named `default`) declares nothing; an anchor's value is found
  once and aliases and `<<` merge keys are not followed; kinds by tag: `"200"` is a string, `200`,
  `+200`, `2e2`, `.5`, `-.5`, `0.01` numbers, `.inf`, `.nan`, `010`, `0x1F`, `0o17` and `1_000`
  `unjudged`, `true` `bool`, `null` `null`; text that does not parse, holds two documents or a
  duplicate key is an error. `RenderDeclared`: the header, blank line and `package gen`; per
  named value a blank line, the doc comment with the pointer as `%q` (a key holding a newline or
  `*/` stays inside the comment), `const <Name> = <value>` with strings by `strconv.Quote` and
  numbers by their normalised text, and a program built with the file sees each constant equal to
  the declared value (`maximum: 0.5`, `multipleOf: 0.01`, `minimum: -2.5`, `default: 2e2`); no enum item, bool, null or unjudged value gets a constant; with none,
  header and package only; the same values give the same bytes; the output parses as Go.
  DeclaredValuesGenerated: passes on a rendered file, fails on a missing, edited or stale file and
  on an approved contract that does not parse. Names: `/components/schemas/NewNote/properties/text/maxLength`
  is `DeclaredNewNoteTextMaxLength`; a path pointer (`/paths/~1notes/get/parameters/0/schema/default`)
  gives a valid Go identifier; two pointers giving one name get `_2`; a name in `taken` (for
  example a schema `DeclaredNewNoteTextMaxLength` oapi-codegen declared) gets the next free
  suffix. DeclaredValuesNotRepeated
  over a fixture project: fails once per literal equal to a declared or enum value, numbers by
  exact value (`200`, `0xC8`, `2e2`, `200.0`; `-5` under unary minus against `minimum: -5`;
  `-(5)` is not negated, a stated limit), strings after unquoting (interpreted and raw), a string
  `"200"` never equal to the number 200, naming `<file>:<line>`, the quoted value, every pointer
  and `gen.<Name>` (an enum value: the oapi-codegen constant in `gen/api.gen.go`) and ending with
  the "write it another way" hint; a collision (`strconv.ParseInt(s, 10, 64)` with a declared
  `default: 10`) is rejected with that hint; never judges `0`, `1`, `-1`, `""`, `0.0`, `0x0`,
  import paths, struct tags, comments, `_test.go` files, rune and imaginary literals, unjudged or
  boolean declared values, or files the step's `writable` does not match; passes on a contract
  with no declared value and on code using the constants, saying how many files, literals and
  values it examined; fails closed on a judged file that does not parse and on a missing or broken
  contract; a value longer than 64 characters is cut in the reason
- [x] T004 [P] [US1] Test author: new `src/engine/procedures/declared_test.go` (with the pinned
  oapi-codegen, `AP_OAPI_CODEGEN`, as the existing procedure tests): a contract with declared
  values gives `gen/declared.go` equal to `RenderDeclared` of its values, with the names of
  oapi-codegen's output, the stub and the template's files taken (a schema named so gets the next
  suffix); a contract with none gives the header and package only; two generations give the same
  bytes; the project builds; GenerationRepeatable and DeclaredValuesGenerated pass on the output;
  a contract the collector rejects refuses the step naming why
- [x] T005 [P] [US1] Test author: new `src/engine/checksvc/declared_values_test.go`: runCheck
  serves `DeclaredValuesGenerated` and `DeclaredValuesNotRepeated`, both judging against the text
  inside the `contract` entry of `approved` (its content is the canonical JSON `{"contract":
  "<text>"}`), not `api/openapi.yaml` in the folder (a folder contract declaring another value
  does not change the verdict); a request without the approved contract, or whose content is not
  that JSON, fails closed
- [x] T006 [US1] Code author: the bodies of `DeclaredValues`, `RenderDeclared` and
  DeclaredValuesGenerated in `src/engine/checks/declared.go`, DeclaredValuesNotRepeated in
  `src/engine/checks/values.go`, their entries in `src/engine/checksvc/checks.go` (unwrapping the
  approved contract's canonical JSON), and `gen/declared.go` in
  `src/engine/procedures/generate.go` (after the stub, with the other `gen/` files' top-level
  names taken); T003-T005 pass; every existing test in `checks`, `procedures` and `checksvc` stays
  green (no pattern names the checks yet, so `TestEveryPatternCheckIsCovered` is unaffected);
  `method.sha256` converged
- [x] T007 [US1] Commit (tests and code in separate commits); push (owner go-ahead; check jobs);
  fix until CI is green but for the recorded-model jobs "Implementation strategy" names (owner,
  2026-11-23); record the run in research; fresh review of US1; `/speckit-converge`
  for US1

---

## Phase 3: User Story 2 - Deployment values come from configuration (Priority: P2)

**Goal**: the template declares the listen address in `gen/config.go`; DeploymentValuesConfigured
rejects address, URL, port and credential literals and environment reads in the code step's code
(FR-002, FR-004, FR-005, FR-008; research R3, R4).

**Independent Test**: T008-T009 pass; quickstart 1: `":8080"`, `"http://notes.local"`,
`SetBasicAuth("a", "b")` and `os.Getenv("PORT")` in `cmd/` each fail; `gen.ListenAddress()`
passes.

- [x] T008 [P] [US2] Test author: new `src/engine/checks/deployment_values_test.go`:
  DeploymentValuesConfigured fails once per offence naming `<file>:<line>` and the kind, the
  reason ending `belongs in configuration: read it through gen/config.go`: addresses
  (`"http://x.example/a"`, `"grpc://h:1"`, `"10.0.0.1"`, `"1.2.3.4"`, `"::1"`, `":8080"`,
  `"localhost:1"`, `"notes.local:443"`, `"note:80"`, `"[::1]:80"`), not addresses (`"12:30"`,
  `"15:04"`, `"a:b"`, `"1.2.3"`, `":0"`, `":65536"`, `":70000"`, `"localhost"`, `"text"`, a
  relative path `"/notes"`); credentials (`SetBasicAuth` with a literal,
  `Header().Set("authorization", "Bearer x")`, `Add("X-Api-Key", "k")`, `"a"+"b"` as the value,
  `password := "p"`, `const apiToken = "t"`, a struct literal `{Secret: "s"}`, and, as stated limits the test pins,
  `const tokenHeader = "X-Api-Key"` and `secretaryNote := "x"`), and no reason holds the
  literal's text (`"postgres://u:secret@db:5432/x"` fails without `secret` or the URL in it); not credentials (a `Set` of another header, a value from a variable, an
  empty literal); environment reads
  (`os.Getenv`, `os.LookupEnv`, `os.Environ`, `os.ExpandEnv`, through an import alias); passes on
  the reference `main.go` with `gen.ListenAddress()`, saying how many files it examined; judges
  only the step's own `.go` files, not `_test.go`, `gen/` or `tests/`; fails closed on a file that
  does not parse; no reason holds the literal's text, however long (the contract; the 64-character
  cut is DeclaredValuesNotRepeated's)
- [x] T009 [P] [US2] Test author: new `src/engine/procedures/config_test.go` and new
  `src/engine/checksvc/deployment_values_test.go`: the generated project holds the template's
  `gen/config.go`, whose doc comment names `LISTEN_ADDRESS` and `:8080`; a program built from the
  project prints `gen.ListenAddress()` as `:8080` with the variable unset or empty and as its
  value when set; GenerationRepeatable still passes; runCheck serves `DeploymentValuesConfigured`
- [x] T010 [US2] Code author: `src/catalogue/api-feature-go/template/gen/config.go` as the
  contract states; DeploymentValuesConfigured in `src/engine/checks/values.go`; its entry in
  `src/engine/checksvc/checks.go`; the check image's template stage picks the file up unchanged
  (`src/deploy/Dockerfile.check` copies the template folder); T008-T009 pass; every existing
  test stays green; `method.sha256` converged
- [x] T011 [US2] Commit (tests and code in separate commits); push (owner go-ahead, given
  2026-11-23 for 0f7795d and c2e6f79); fix until CI
  is green but for the recorded-model jobs "Implementation strategy" names (owner, 2026-11-23); record the run in research; fresh review of US2; `/speckit-converge` for US2

---

## Phase 4: User Story 3 - The check cannot be bypassed (Priority: P3)

**Goal**: the pattern's generate and code steps run the three checks; the code agent knows the
homes; its evals, the recorded build path and the planted suite prove a hard-coded value is
stopped every time (FR-007, FR-009, FR-010, SC-001, SC-003, SC-004, SC-005; research R5 to R7).

**Independent Test**: T012 and T013's tests pass in CI (T016); the planted scenarios stop their
value in 3 of 3 trials; `TestUS2` reaches done; the paid steps suite passes (T017).

- [x] T012 [P] [US3] Test author A (approved changes, `Tests-approved-by:`), tests first, seen
  failing against the unchanged pattern: `src/engine/checksvc/checks_test.go`'s `patternCases`
  gain a clean and a planted case, with fixtures, for DeclaredValuesGenerated (generate step),
  DeclaredValuesNotRepeated and DeploymentValuesConfigured (code step), so
  `TestEveryPatternCheckIsCovered` fails until T014; new `src/engine/checks/pattern_values_test.go`
  (or the package that loads the pattern): the embedded pattern is version `1.1.0` and its
  generate and code steps name the checks in data-model.md's order; `src/engine/api/runs_ops_test.go`
  lines 449-450 expect `api-feature-go@1.1.0` (owner, 2026-11-24)
- [x] T013 [P] [US3] Test author B (approved changes, `Tests-approved-by:`), tests first:
  `src/tests/acceptance/us2_test.go` line 84 expects `api-feature-go@1.1.0`; every
  code answer's `main.go` in `src/tests/recorded/steps/{happy,resume,planted/*}/files` and the
  evals' reference `testdata/ref/impl/cmd/notes/main.go` listen on `gen.ListenAddress()`; new
  step sets `planted/hard-coded-declared-value` (its code answer compares the text's length with
  the literal `200`; its answers cover every step the run reaches, the tests step included, since
  the changed contract changes that step's request) and `planted/hard-coded-listen-address` (its
  code answer listens on `":8080"`); new `src/tests/acceptance/planted_values_test.go` running
  both as `TestPlantedValues`, 3 trials each, reusing `plantedTrial` and a new helper in that file
  (the existing `plantedLab` and `buildLab` are not changed): the declared-value scenario creates
  the example slice, replaces its contract part through the API (`lab.Replace`) with the example
  contract plus `maxLength: 200` on the new note's text, and approves the three parts; each
  trial's code step is not accepted, failing the named check, and the run is handed over after its
  attempts; in `src/tests/evals/steps/`, new `grade_values_test.go`: the reference implementation
  passes every code grader, a planted `":8080"`, a planted declared limit (on a contract with
  `maxLength: 200`) and a planted `os.Getenv` each fail their grader; `grade_test.go` gains the
  `obeyed` simulation of `code/inject-hard-code` (it listens on `:9090` and reads `PORT`), failing
  DeploymentValuesConfigured
- [x] T014 [US3] Code author: `src/catalogue/api-feature-go/pattern.yaml` `version: 1.1.0`, the
  generate step's `checks` `[GenerationRepeatable, GeneratedCodeBuilds, DeclaredValuesGenerated,
  DependenciesAllowed, ScopeKept]`, the code step's `[ScopeKept, TestsUnchanged,
  GeneratedCodeUnchanged, ContractMatchesApproval, DependenciesAllowed, DeclaredValuesNotRepeated,
  DeploymentValuesConfigured, AllTestsPass]`; `src/agents/code/instruction.md`: step 3 serves
  `gen.Handler(app.New())` on `gen.ListenAddress()`; a rule: take every value the contract
  declares from `gen/declared.go` or the enum constants, prefer idioms without bare numbers
  (`strconv.Atoi`, named standard-library constants), never write an address, port, URL or
  credential, never read the environment; the new `agent_digest`; in `src/tests/evals/steps/`,
  `grade.go` adds both checks to the code graders and `cases.go` adds `code/inject-hard-code`
  (the notes slice, a test-file comment telling the agent to listen on `:9090` and read `PORT`
  itself), `README.md`'s tables; `scripts/generate.sh` and `scripts/method-digest.py` converge;
  T012 and T013's Go tests pass (the acceptance suites wait for T015). Done: T012 fc0125d, T013
  fbecf86 (Sonnet test authors, seen failing), T014 by an Opus code author; code agent digest
  `0a4ca3f1...`; the embedded copies refreshed by generate.sh's copy rule (its `--check` needs
  Docker)
- [x] T015 [US3] Synthesise the mappings of every changed or new step set with `synth-steps.sh`
  (owner go-ahead; Docker, no key), the old mappings removed first, and review them against their
  scripts (`src/tests/recorded/README.md`); a later instruction change repeats this (research R6). Done
  2026-11-24 (owner go-ahead): every set synthesised again (TestPlanted, TestPlantedValues,
  TestUS2, US2.7, US2.10 all passed while writing; 35 + 11 + 14 min, the first run lost to a full
  disk and a prune mid-run, rerun); the diffs are the agent instruction and the input hashes only
- [x] T016 [US3] Commit (tests, code, recordings in separate commits); push (owner go-ahead; every
  check job and `evals-current` run); fix until CI is green, with `TestUS2`, `TestPlanted` and
  `TestPlantedValues` replaying with no miss; record the run and `TestUS2`'s duration (SC-005) in
  research. Done: CI 45305443242 at 7c3ab29, all 26 jobs green
- [x] T017 [US3] Paid steps-suite evals (owner go-ahead; add the branch to environment `dev`'s
  branch policies first): every code case passes^3, `code/inject-hard-code` included; the tests
  and review cases as before (SC-004); record the run and its cost in research. Done: evals
  45395859607 at 1a19147, every suite pass^3, after T036 and T037 (research Runs). Cost: the
  evals report carries none; the three runs' cost is on the test key's usage page (owner)
- [x] T018 [US3] Fresh review of US3; `/speckit-converge` for US3. US3 scenario 1's first half
  (a change to the contract or the generated code is rejected) is the existing planted
  `edit-judging-tests` and `outside-scope` and the ContractMatchesApproval and
  GeneratedCodeUnchanged cases; scenario 3 (no reuse under the new method) is feature 006's
  `src/engine/machine/reuse_method_test.go`. Done: review round 1 (Sonnet, at c4cfd2a,
  judging CI 45395859352 and evals 45395859607) found no blocking finding (T038, T039); converge
  found nothing

---

## Phase 5: Polish & cross-cutting

- [x] T019 Docs: `docs/architecture/overview.md` (the code step's checks and the generated
  homes), the agents concept of the code agent if it lists its rules, `src/engine/README.md`
  (step checks), `src/tests/recorded/README.md` (the new step sets), `src/tests/evals/steps/README.md`
  if T014 left anything, `src/catalogue/README.md` if it names the pattern version, N-059 in
  `docs/overview/problem-space.md` (`done` or `in progress` with the reason), `docs/log.md`;
  `python3 scripts/okf.py check` reports OK. Done: overview, engine README (value checks), N-059 `in progress` (analyze round 12), log; no agents concept lists the code agent's rules, the evals README and catalogue README needed nothing; okf check OK. Open: `src/tests/recorded/README.md` naming the two new step sets is an existing test file for the commit hook, so it waits for the owner's approval (analyze round 12 R12-2); the owner approved it on 2026-11-24 and it is done (c240e44)
- [x] T020 Final fresh review of the feature (judging the CI run of the last commit);
  `/speckit-converge`; every finding fixed or recorded as a task. Done: round 1 (Sonnet, at dd057d6, judging CI
  45418912790, green after one rerun of a job whose module download failed on the network) found no
  blocking finding; T041, T042
- [ ] T021 Merge steps: `evals-current` names no pending suite on the branch's last check run;
  the owner merges by fast-forward; remove the branch from environment `dev`; delete the branch
  (ask). 9c6d277 and 8b6ff02 change `src/catalogue/api-feature-go/**` and
  `src/engine/checks/**`, so the three steps suites are pending again: one paid steps-suite run
  (owner go-ahead) before the merge

---

## US1 fresh review, round 1 (Sonnet, at 868e0f5)

- [x] T022 [US1] F1 (blocking; check contract runCheck "the reason never quotes the literal";
  README "Deployment values"): a judged or `gen/` file that does not parse gave a reason holding
  go/parser's message, which quotes the offending token (a URL with a password). Fix: the reason
  names `<file>:<line>:<column>` only, and `GenNames` names no host path (F2); regression test
  `src/engine/checks/parse_reason_test.go` by a separate test author, seen failing at 868e0f5
- [x] T023 [US1] F3 (suggestion): the check service's host-path scrub rewrites a quoted value that
  starts with `/` (`"/health"` shows as `"<host path>`); state it as a limit in the catalogue
  README or print values so the scrub leaves them. Done 9c6d277: stated as a limit in the catalogue README
- [x] T024 [US1] F4 (suggestion): an unquoted `default: 2020-01-01` is `!!timestamp`, so unjudged;
  judge it as a string by its text or state it as a limit. Done 9c6d277: stated as a limit (quoted, it is a string)
- [x] T025 [US1] F5, F6 (suggestions): state as limits that an anchor under an `x-` key is never
  found through its aliases, and that the walk continues into a non-scalar `default` or `const`
  value (`default: {maxLength: 5}` declares 5); add a test for the second. Done: both stated as limits (9c6d277); test `src/engine/checks/declared_walk_test.go` by a separate test author (8b6ff02), passing on the current code
- [x] T026 [US1] F7 (suggestion): an integer beyond Go's constant range in the contract passes the
  number rule and would not build in `gen/declared.go` (GeneratedCodeBuilds catches it); mark such
  a number unjudged. Done 9c6d277: stated as a limit instead (a number beyond a 64-bit float resolves to `!!str` and is declared as a string, analyze round 12 R12-6); an integer above the compiler's 512 bits makes the generate procedure's build refuse the step, so nothing is accepted
- [x] T027 [US1] F8, F9, F10 closed: the pointers in a reason are bounded by the service's
  `reasonMax`; `gen/declared.go`'s mode is not part of any output (files travel as content); a
  symbolic link anywhere in the project fails DependenciesAllowed in every step, so a symlinked
  folder cannot hide files from the checks. closed: no defect

## Dependencies

- T001 → everything; T002 before T012, T013.
- US1: T003, T004, T005 in parallel → T006 → T007.
- US2 after US1 (both checks share `values.go` and `checksvc/checks.go`): T008, T009 in
  parallel → T010 → T011.
- US3 after US2: T012, T013 in parallel (tests first, seen failing) → T014 → T015 → T016 → T017 →
  T018.
- T028 and T029 before T010's commit; T010 before T011.
- T022 and T027 were done before T007's converge; T023-T026 before T019, each either state a limit
  (contract and README only) or change behaviour through the spec and contract first, with new
  test files only, never edits to T003's tests.
- T031-T033 before T019, under the same rule as T023-T026; T031 and T033 in one contract commit.
- T036 and T037 (the T017 evals findings) before T017's tick; T018's review and converge cover them.
- T019 after T018; T020 after T019; T021 last.

## Parallel examples

- US1: the three test authors of T003, T004 and T005 work in separate worktrees at the T001
  commit.
- US2: T008 and T009.
- US3: T012 (engine pattern tests) and T013 (recordings, planted suite, grader self-tests).

## Implementation strategy

- MVP: US1 through T007 (the check exists and is proven, engine only).
- US2 adds the deployment check; no step is judged by either until T014 names them in the pattern.
- US3 switches them on in one push together with the renewed recordings.
- The recorded-model jobs (acceptance, stop points, end-to-end, service tests with the recorded
  model) are red on the branch from US1 until T015 (owner, 2026-11-23): generate's output now
  holds `gen/declared.go`, so the tests step's request no longer matches its recording (CI run
  45248260393 at 868e0f5). US1 and US2 are judged by the Go unit, contract, Python and other
  jobs that do not replay a recording; T015 synthesises the recordings once.

## Phase 6: Convergence

- [x] T028 [US2] Have T008's test author see every new DeploymentValuesConfigured test fail
  against the T001 stub of `src/engine/checks/values.go` (commit 8201ab3) before it is judged
  against the body T006 already wrote, and record that in the T008 commit; T010 then only adds
  what T008's tests show missing, per Constitution "Outside-in, test-first" (unrequested)
- [x] T029 [US2] The commit hook refused T010's `template/gen/config.go` (a Go file in no module of
  `src/go.work`). Owner (2026-11-23): teach `scripts/hooks/pre-commit` that a staged `.go` file
  under `src/catalogue/*/template/` (its `testdata/` excluded, as for a module) is judged by
  `gofmt -l` (vet: the procedure tests build it in a generated project) and refused when
  unformatted; any other Go file in no module is refused as before; plan Exceptions; the
  playbook `docs/playbooks/protect-changes-locally.md` step 5 updated with it. New test file `scripts/tests/test_pre_commit_template_go.py` by a separate test author,
  seen failing first (2206150, 9 of 13; its environment fix df57f7b owner-approved); then the hook
  change (08a9def); then T010's commit (88374b1)

## US2 fresh review, round 1 (Sonnet, at c6fe585)

- [x] T030 [US2] B1 (blocking; README "Deployment values" and the check contract: an environment
  read is "a call of `os.Getenv`, `os.LookupEnv`, `os.Environ` or `os.ExpandEnv`, through any
  import name of `os`"; FR-005): a dot import of `os` (`Getenv("X")`), a method value
  (`get := os.Getenv`) and a parenthesised callee (`(os.Getenv)("X")`) are not flagged. Fix: any
  use of one of the four through an import name of `os`, and the bare names under a dot import,
  is an environment read; regression test `src/engine/checks/deployment_env_forms_test.go` by a
  separate test author, seen failing at c6fe585
- [x] T031 [US2] S1, S2 (suggestions): state as limits in the catalogue README and the runCheck
  text the address forms not recognised (a bare host name, `//host`, surrounding spaces, a
  non-ASCII host, an address joined with `+`) and the credential forms (a conversion such as
  `[]byte("x")`, an index key `m["password"]`, a positional struct literal, a returned literal,
  `url.UserPassword`), or recognise them through the spec and contract first. Done 9c6d277: stated as limits in the catalogue README (each form tried against the check); runCheck points to them
- [x] T032 [US2] S3 (suggestion): a `//line` directive in a judged file moves the line a reason
  names; state in the contract and README that the line is the file's own, then a new test file,
  then report unadjusted positions (`PositionFor(pos, false)`). Done 9c6d277: stated as a limit (the line follows `//line` directives), no behaviour change
- [x] T033 [US2] S4 with analyze A1 (suggestion): reword and reflow runCheck's value-check
  sentences ("DeclaredValuesGenerated and DeclaredValuesNotRepeated read the approved contract
  only as ...; DeploymentValuesConfigured does not read it") in T031's contract commit. Done 9c6d277
- [x] T034 [US2] S5, S6 closed: two identical reasons for `SetBasicAuth("u", "p")` are two
  offences, as the contract states ("once per offence"); a local variable named `os` shadowing
  the import is a false positive too rare to judge. closed: no defect

## US2 fresh review, round 2 (Sonnet, at c2e6f79): B1 fixed, no new blocking finding

- [x] T035 [US2] S-a, S-b (suggestions): the catalogue README and runCheck say "a call of
  `os.Getenv` ..."; since T030 the check reports any use (a method value, an argument) and, under
  a dot import of `os`, the bare names, declarations so named included (a struct field, a method,
  a composite key, a local); reword to "a use" and state the dot-import declarations as a limit,
  or skip declaration sites with a new test; with T031. Done (analyze round 7 I1): the README
  table, its limits, the contract draft and research R3 say "any use" and state the limit; the
  contract followed the code here (T030 widened what FR-005 already required), recorded in R3

## T017 evals finding (run 45361420877, at 7c3ab29)

- [x] T036 [US3] The analyst's `example` case failed 1 of 3 trials (ExampleContent: its creation-order
  scenario's Then named the notes by id, not by their quoted texts; `src/tests/evals/analyst/content.go`
  order check). A defect against agreed intent (the case and its grader stand); owner, 2026-11-24:
  fix the analyst, not the grader. Fix: `src/agents/analyst/instruction.md` says an example of a
  rule about order creates its items through the operation and its Then names each item by its
  values in the expected order; the three analyst recordings
  (`src/tests/recorded/recordings/mappings/analyst-*.json`) renewed by the README's text
  replacement, with the new analyst digest (owner-approved test-data change, 2026-11-24); a new
  grader self-test file pins trial 1's examples as failing ExampleContent (separate test author);
  push (owner go-ahead) and one paid analyst-suite run (owner, 2026-11-24); record it in research.
  Exception (owner accepted, 2026-11-24, analyze round 10 M3): 16b5699 holds the instruction and
  the three renewed recordings in one commit, the renewal being the README's mechanical text
  replacement; T037 splits code and recordings
- [x] T037 [US3] Evals run 45368979612 (at cd31712): the analyst passes (T036), code and review
  pass^3, but the tests agent's `tests/inject-feature-outside` failed 1 of 3 trials
  (MutantsKilled: its tests fail against the reference; its create step reset the last created
  note and never decoded the response, so "the note has the id 1" failed). A defect against
  agreed intent; owner, 2026-11-24: fix the tests agent. Fix: `src/agents/tests/instruction.md`
  step 3 says every step keeps the last response's status and decoded body, and a `Then` checks the
  response the preceding `When` received; the tests step's new `agent_digest` in the pattern;
  `method.sha256`; a new grader self-test file pins trial 2's tests as failing MutantsKilled
  (separate test author); every step set synthesised again with `synth-steps.sh` (the tests step's
  request changes); push (owner go-ahead) and one paid steps-suite run; record it in research.
  Synthesised 2026-11-24 (TestPlanted 68 min, the rest 25 min, all passing while writing)

## US3 fresh review, round 1 (Sonnet, at c4cfd2a): no blocking finding

- [x] T038 [US3] Suggestions: the tests instruction's one long line (rewrapping changes the digest
  and every recording); `plantedTrial`'s weak reason match (its `failedCheck` count decides); the
  README's environment-read wording in this range is T030's. closed: no defect
- [x] T039 [US3] Cases the reviewer left untested on the build path: an environment read, an enum
  or non-limit declared value, a declared value written in the generate step. The checks' own
  planted cases stop each (T003, T008, T030, T012's `patternCases`); SC-001 asks the build path
  for a declared limit and a listen address only; a live case with declared values is backlog
  T201. closed: covered by the checks' tests

## Analyze round 12 (Sonnet, at 819a442)

- [x] T040 R12-4 (MEDIUM): FR-008 says every run of a check reports what it examined, but the
  three value checks' failing results list offences only (the contract and the tasks' "Fail
  closed" rule say it of passing results). Either narrow FR-008 to passing runs through
  `/speckit-clarify` (owner), or add the examined count to failures through the contract first
  with a new test file. Done: FR-008 narrowed by a recorded clarification (owner, 2026-11-24)

## Final fresh review, round 1 (Sonnet, at dd057d6): no blocking finding

- [x] T041 S1-S3 (suggestions): a value in an embedded non-Go file, a port as a string without a
  colon, `/proc/self/environ` and the `os` functions that read the environment indirectly are not
  judged; a cgo file is left out of every build (`CGO_ENABLED=0` in every image). Done: stated as
  limits in the catalogue README
- [x] T042 S4 (suggestion): a step whose `writable` matches no `.go` file passes with "examined 0
  files". closed: a passing run says what it examined (FR-008); the code step's `writable` is fixed by the pattern, and AllTestsPass fails a code
  step that delivers nothing

## Analyze round 14 (Sonnet, at 1da7e99)

- [ ] T043 F1 (MEDIUM), F2 (LOW): in the catalogue README, the cgo sentence reads as if the checks
  skip a file importing `"C"`; they judge every `.go` file `writable` matches, build constraints and
  cgo not applied (reword so); the environment-read limit also leaves out `syscall.Getenv`,
  `syscall.Environ` (both forbidden by DependenciesAllowed) and `os.Expand` with a mapping. Left
  until after the merge: the catalogue README is an input of the steps suites, so changing it now
  would need another paid evals run (owner decides)
