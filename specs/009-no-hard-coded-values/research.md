# Research: Code that hard-codes a declared value is rejected

Phase 0 of the [plan](plan.md) for the [spec](spec.md). Each decision names its deciding source:
the code on `main` (`64ce30a`), a reproduced experiment, or a cited document. Decisions marked
*owner* were put to the project owner as direct questions on 2026-11-23.

## Sources

| ID | Source |
|----|--------|
| S1 | `src/catalogue/api-feature-go/pattern.yaml`: the code step writes `internal/**`, `cmd/**`; its checks are ScopeKept, TestsUnchanged, GeneratedCodeUnchanged, ContractMatchesApproval, DependenciesAllowed, AllTestsPass; generate writes `gen/**`, `go.mod`, `go.sum`, `vendor/**`, the seam |
| S2 | `src/engine/procedures/generate.go` `generateOnce`: copies the template's files (its `gen/` among them), runs oapi-codegen by the template's directive, writes `gen/stub.go` and the seam; `src/engine/checks/steps.go` `GenerationDrift` runs `go generate` in a copy and compares the files under `gen/` (files the procedure wrote besides oapi-codegen's survive `go generate`, as `gen/stub.go` does today) |
| S3 | `src/engine/checks/steps.go` `dependencies`: the pattern's judged-files rule (a step's own `writable` globs), `go/parser` over each judged `.go` file, one failing result per offence naming the file; `src/engine/checksvc/checks.go` `checkByName`, `approved(gen.ApprovedPartPartContract)`: the approved contract's content reaches every check call |
| S4 | `src/check/api/preview/2026-11-13-preview/openapi.yaml` runCheck: the served checks are stated one by one in its description (contract-first: a new check is stated there first) |
| S5 | Experiment E1 (oapi-codegen v2.8.2 source, the `images.lock` pin) |
| S6 | Experiment E2 (gosec v2.30.0, G101) |
| S7 | [go-mnd](https://pkg.go.dev/github.com/tommy-muehle/go-mnd), read 2026-11-23: reports numeric literals in argument, case, condition, operation, return and assign positions, with a list of ignored numbers; it has no notion of where a value is declared and does not judge strings |
| S8 | `src/tests/evals/steps/testdata/ref/impl/cmd/notes/main.go` and every code answer of `src/tests/recorded/steps/{happy,resume,planted/*}`: `http.ListenAndServe(":8080", gen.Handler(app.New()))` |
| S9 | `src/agents/code/instruction.md` step 3: "Write a `main` package under `cmd/<service>/` that serves `gen.Handler(app.New())` with `net/http`" (no address named) |
| S10 | Constitution: Engineering Principles "Values have one home" ("Deploy configuration (endpoints, ports, store addresses) MUST come from environment variables"); Principle VI (custom code needs a written justification); XVII ("Declare the what once", "Derive the rest"); XIX (a prompt change passes the regression evals); XX (a proven solution is captured as a deterministic asset) |
| S11 | `src/tests/recorded/README.md` "After a change to an agent's instruction" and the step sets section: step recordings are synthesised from `answers.json` by `synth-steps.sh`; an instruction change makes every recording of that agent miss |
| S12 | `src/engine/checks/fit.go`: the contract is parsed once as a single YAML document with `go.yaml.in/yaml/v3` nodes; text that does not parse, holds several documents or a duplicate key fails (fail closed) |
| S13 | Experiment E3 (oapi-codegen v2.8.2 over enum shapes) |
| S14 | OpenSSF Scorecard API and GitHub, read 2026-11-23: go-mnd 1.8 (MIT, last push 2023-12-21); goconst 3.3 (MIT, last push 2026-11-07); revive 6.8 (MIT). revive's `add-constant` rule and goconst report repeated or magic literals against allow lists or repeat counts; neither knows a declaration outside the code |
| S15 | `src/engine/checksvc/checks_test.go` `TestEveryPatternCheckIsCovered` and `patternCases`: every check the pattern names needs a clean and a planted case there; `src/tests/evals/steps/grade_test.go`: every adversarial case needs an `obeyed` simulation; the grader self-tests and held-out tests assume the notes slice |
| S16 | `src/engine/checksvc/checks.go` `approved`: an approved part's `content` is the canonical JSON `{"contract": "<the YAML text>"}` (`canonical.ContractHash`) |

## E1. What oapi-codegen generates from schema keywords

Read in `github.com/oapi-codegen/oapi-codegen/v2@v2.8.2/pkg/codegen` (the pinned tool, S5):

- `templates/constants.tmpl` generates, for every enum, one typed constant per value and a
  `Valid()` method: an enum value already has a Go home in `gen/`.
- No file under `pkg/codegen` mentions `maxLength`, `MaxLength` or any other length, range, size
  or `default` keyword (`grep -rln 'MaxLength\|maxLength'` finds none); its extensions
  (`extension.go`: `x-go-type`, `x-go-name`, `x-oapi-codegen-extra-tags`, ...) change Go names and
  types, never emit a value. So a limit or a default declared in the contract has no Go home
  today.

## E2. Whether gosec's hard-coded credential rule finds a credential in service code

gosec v2.30.0 (published 2026-10-23; OpenSSF Scorecard 6.6 on 2026-10-19; Apache-2.0), installed
in a scratch folder, `gosec -include=G101 -fmt=json ./...` on a module with
`const apiToken = "s3cr3t-Value-1234567890"`, `var password = "hunter2"`,
`r.SetBasicAuth("admin", "Adm1nPassw0rd!")` and
`r.Header.Set("Authorization", "Bearer abcdefABCDEF0123456789")`: `found: 0`. Its binary is 96 MB.
So the mature rule (since v2.18 "only match on more specific patterns") does not find these
credentials, and would multiply the check image's tool size for nothing.

## E3. Which enum shapes get an oapi-codegen constant

With the pinned oapi-codegen v2.8.2 and the template's `oapi-codegen.yaml`
(`always-prefix-enum-values: true`), a contract with enums on a component property, array items,
an integer property, a nullable property, an inline query parameter (string and integer) and an
inline response property: every one got a typed constant per value
(`NoteStatusOpen`, `NoteTagsRed`, `NoteLevelN1`, `ListNotesParamsOrderAsc`,
`ListNotesParamsSizeN10`, `ListNotes200JSONResponseBodyKindPage`); a `null` enum item became
`"<nil>"`; a component schema no operation references got no type and no constant.

## R1. The home of a declared value

- **Decision**: the generate procedure writes `gen/declared.go`, one exported, untyped Go constant
  for each number or string the approved contract declares under a schema keyword `maxLength`,
  `minLength`, `maximum`, `minimum`, `exclusiveMaximum`, `exclusiveMinimum`, `maxItems`,
  `minItems`, `maxProperties`, `minProperties`, `multipleOf`, `default` or `const`, with a doc
  comment naming its JSON Pointer (written with `%q`, so no key can end the comment). Enum values
  keep their home in oapi-codegen's enum constants, which every enum an operation reaches gets
  (E3). One collector in `engine/checks`, `DeclaredValues`, finds the values for the procedure,
  for the new generate-step check (R7) and for the code-step check (single owner).
- **Scalars** (analyze F8): a value's kind comes from the YAML node's resolved tag. `!!str` is a
  string, written with `strconv.Quote`. An `!!int` or `!!float` whose text, after removing a
  leading `+` and putting `0` before a leading `.`, is a plain decimal (optional fraction and
  exponent, no leading zero) is a number: its value is `go/constant.MakeFromLiteral` of that text
  and `gen/declared.go` writes that text, never `ExactString()`, which writes `0.1` as `1/10`, an
  integer division in Go (analyze round 2, N1, reproduced). Any other number text (`.inf`,
  `.nan`, `010`, `0x1F`, `1_000`, which `MakeFromLiteral` would read with Go's meaning, not
  YAML's: N4) is recorded, gets no constant and is never judged (a stated limit). `!!bool` and `!!null` are recorded, never
  judged. A quoted `"200"` is a string, never the number 200.
- **Walk** (F9): document order, depth first, mapping keys in their written order; that order
  sorts the constants and decides which of two equal names gets `_2`. Aliases and merge keys
  (`<<`) are not followed: a value under an anchor is found once, at the anchor. A key `example`,
  `examples` or `x-*` hides its whole subtree, a property so named included (a stated limit).
- **Names** (F10): as the contract states; a name already declared at the top level of another
  file in `gen/` (oapi-codegen's output, the stub, the template's files) gets the next free
  suffix. Names follow the contract: adding a value before another can rename it, and code using
  the old name then fails to build, which the tests show.
- **Rationale**: FR-006 needs a home the code can name; S10 says derive, never keep by hand; the
  approved contract is the one source (feature 001).
- **Custom code justification (Principle VI)**: oapi-codegen emits no constant for these keywords
  and has no extension that does (E1); a second generator for the same contract (for example
  ogen) would replace the pattern's generator and every recording and eval built on it. The
  collector is a walk over the YAML nodes the fit conditions already parse (S12).
- **Alternatives**: request validation middleware from the embedded contract (oapi-codegen's
  `nethttp-middleware` with kin-openapi): enforces limits without code, but adds two modules to the
  pattern's allowed dependencies, changes the seam the tests serve, and still leaves a default the
  code needs without a home; rejected for this feature.

## R2. Which literals are copies of a declared value (DeclaredValuesNotRepeated)

- **Decision**: in every `.go` file the code step may write, except `_test.go` files, each
  integer, floating-point and string literal (rune and imaginary literals aside) is compared by
  value with the judged values of R1 and with the contract's enum values: numbers as exact
  constants (`go/constant.Compare`, so `200`, `0xC8`, `2e2` and `200.0` are one value; a literal
  whose parent is a unary `-` is negated, a parenthesised one is not: a stated limit), strings
  after `strconv.Unquote`, byte for byte, and a string never equals a number. A match fails the
  check, naming the file, line, the quoted value (cut to 64 characters), every contract location
  that declares it and `gen.<Name>` (for an enum value: "the enum constant oapi-codegen generated
  for it in gen/api.gen.go, where it exists"), and ending "if the literal does not stand for this value, write it
  another way". Not judged: `0`, `1`, `-1` and the empty text (owner, spec FR-003), import paths
  and struct tags, and values computed from several literals (a stated limit). The approved
  contract comes from the request's `approved` contract entry, whose content is the canonical
  JSON `{"contract": <text>}` (S16), never from the project folder (FR-007).
- **Collisions** (analyze F7, owner): a literal that equals a declared value without standing for
  it is still rejected (`ParseInt(s, 10, 64)` with a declared default of 10); the reason says to
  write it another way, the code agent's instruction prefers idioms without bare numbers
  (`strconv.Atoi`, named constants of the standard library), and the limit is stated in the
  catalogue README. A collision costs one attempt.
- **Rationale**: exact value comparison is deterministic and needs no type information;
  `go/constant` is the standard library's own reading of Go literals.
- **Custom code justification (Principle VI)**: go-mnd, goconst and revive's `add-constant` judge
  literals against allow lists or repeat counts, not against a declaration outside the code, and
  go-mnd does not judge strings (S7, S14); no linter compares literals with an OpenAPI contract.

## R3. The home of the deployment values

- **Decision**: the pattern's template gains `gen/config.go`: `ListenAddress()` returns the
  environment variable `LISTEN_ADDRESS`, or `:8080` when it is unset or empty, and its doc comment
  lists every configuration value with its default (FR-005). The code agent's `main` serves
  `gen.Handler(app.New())` on `gen.ListenAddress()`. The code step's own code reads no
  environment variable (owner): every configuration value is then the declared, documented one.
- **Rationale**: one home, written once by the platform as a deterministic asset (S10, XX); the
  single-service pattern has one deployment value, its listen address; the constitution names
  environment variables as the source (S10).
- **Alternatives**: the code step reading the environment itself with a default in its code
  (owner chose to forbid; the default would be a literal the check cannot tell from a hard-coded
  one); moving `cmd/` to the generate procedure (removes `cmd/**` from the code step's
  `writable`, which changes more existing tests and recordings than R3).

## R4. Which literals are deployment values (DeploymentValuesConfigured)

- **Decision**: in the same files as R2, the check fails on:
  - an **address**: a string literal that is a URL with a scheme and a host (`net/url`), an IP
    address (`net/netip`; so a four-part version `"1.2.3.4"` counts: a stated limit), or a
    `host:port` or `:port` (`net.SplitHostPort`) whose port is a decimal number from 1 to 65535 and
    whose host is empty, `localhost`, an IP address or a name holding a letter (so `"12:30"` is
    not one, `"note:80"` is); `":0"` (any free port) is not judged;
  - a **credential**: a non-empty string literal as an argument of a `SetBasicAuth` call, as the
    second argument of a `Set` or `Add` call whose first argument is the literal header name
    `Authorization`, `Proxy-Authorization`, `Cookie` or `X-Api-Key` (any case), or assigned,
    declared or keyed to a name matching `(?i)passw(or)?d|secret|token|api_?key|credential|private_?key`
    (so `const tokenHeader = "X-Api-Key"` counts: a stated limit); an argument made only of
    literals joined by `+` counts as a literal;
  - an **environment read**: any use of `os.Getenv`, `os.LookupEnv`, `os.Environ` or `os.ExpandEnv`
    (a call, a function value, an argument) through any import name of `os`, and their bare names
    under a dot import (owner, R3; widened by the US2 review, T030, so that no form of reading the
    environment passes, FR-005).

  Each failure names the file, the line and the kind, never the literal: a URL can carry a
  password in its userinfo (spec FR-004, Principle XI; analyze round 2, N3). The reason ends `belongs in
  configuration: read it through gen/config.go`. Stated limits: a port written as a number, a
  credential under another name or in another call, and a value built at run time are not
  recognised.
- **Rationale**: syntax rules over `go/ast` are deterministic, need no type information and no
  model; the names follow the common credential names.
- **Custom code justification (Principle VI)**: gosec's G101 found none of four plain credentials
  and costs a 96 MB binary (E2); no linter judges address literals; gosec G102 judges only binding
  to all interfaces.

## R5. The pattern, the method and the agents

- **Decision**: the code step's `checks` gain `DeclaredValuesNotRepeated` and
  `DeploymentValuesConfigured`, the generate step's `DeclaredValuesGenerated` (R7); the pattern
  becomes version `1.1.0`; the template gains
  `gen/config.go`; the code agent's instruction names `gen.ListenAddress()`, `gen/declared.go`
  and the rules, so its `agent_digest` changes; the method identity follows from the changed
  files (feature 006), so earlier accepted code steps are judged again (FR-009). The identity
  already changes with every commit that touches the engine, the check contract or the template
  (`src/engine/method/method.json`), so `scripts/method-digest.py` converges it in each such commit
  (analyze F13). The review agent's instruction is unchanged.
- **Rationale**: FR-007, FR-009; feature 006's identity is derived, never edited by hand.

## R6. Evals, recordings and planted failures

- **Decision** (owner approved each existing test change, 2026-11-23, after analyze round 1):
  - the step evals' code graders gain the two checks (`grade.go`, by the code author), with
    grader self-tests in a new file by a separate test author, planting each kind of value and
    passing on the reference implementation;
  - the reference implementation's `main.go` and every recorded code answer (S8) listen on
    `gen.ListenAddress()`; the step sets' mappings are synthesised again from their answers (S11),
    since the code agent's instruction changes too;
  - one new adversarial code case, `code/inject-hard-code`: the notes slice with a test-file
    comment telling the agent to listen on `:9090` and read `PORT` itself, with its `obeyed`
    simulation in `grade_test.go` (S15);
  - `checksvc/checks_test.go`'s `patternCases` gain a clean and a planted case, with fixtures, for
    each of the three new checks (S15);
  - `src/tests/acceptance/us2_test.go`'s assertion that a run records `api-feature-go@1.0.0`
    becomes `@1.1.0` (owner, analyze round 2, N2);
  - `src/engine/api/runs_ops_test.go` lines 449-450 (the run's pattern from the real
    `pattern.yaml`) become `@1.1.0` (owner, 2026-11-24, found before T012);
  - two planted scenarios in a new acceptance file, each stopped in 3 of 3 trials:
    `hard-coded-declared-value` (the example slice's contract replaced through the API with one
    declaring `maxLength: 200`, then approved) and `hard-coded-listen-address` (SC-001).
- **No live case with declared values** (owner): the grader self-tests and held-out tests assume
  the notes slice, which declares none; a live case on another slice becomes a backlog task.
- **Order** (analyze F1, F17): the new tests come first and are seen failing; then the pattern,
  instruction and graders; then the recordings are synthesised from the final instruction. A paid
  eval run that forces an instruction change repeats the synthesis (about 20 minutes, Docker, no
  key): a known repeat.
- **Rationale**: XIX (a prompt change passes the regression evals, adversarial cases for
  injection); SC-001; S11 (an instruction change makes every code recording miss).
- **Cost**: a paid run of the steps suite before merge (owner's go-ahead), and a local
  `synth-steps.sh` run per changed set (Docker, no key; asked before it runs).

## R7. A drift check for gen/declared.go (DeclaredValuesGenerated)

- **Decision** (analyze F12): the generate step gains the check DeclaredValuesGenerated: the
  project's `gen/declared.go` is byte for byte the rendering of the approved contract's declared
  values (the same function the procedure writes it with). GenerationRepeatable stays as it is
  (`go generate` does not write the file), and GeneratedCodeUnchanged keeps it unchanged after the
  generate step.
- **Rationale**: Principle XVII, "a drift check MUST prove it matches its contract"; a separate
  check leaves GenerationRepeatable's existing tests and fixtures as they are.

## Runs

The evals reports carry no cost; the paid runs' cost is on the test key's usage page (owner).

| Commit | Run | Result |
|--------|-----|--------|
| 868e0f5 (US1, T001-T006) | 45248260393 | Go unit tests and vet, contracts lint, API breaking changes, knowledge, markdown, layout, secrets, dependencies, module boundaries, gateway, evals-current green; the recorded-model planted jobs failed on a recording miss in the tests step (`gen/declared.go` in generate's output), expected until T015 (owner, 2026-11-23); run cancelled after the Go job |
| 2b1aad3 (US1 review fix T022) | 45254495030 | Go unit tests and vet, Python script and hook tests and every static job green; the recorded-model jobs (planted, Story 2, end-to-end, service tests) failed only on the same recording miss; cancelled after the Go and Python jobs. US1 review converged in 2 rounds (F1 fixed, F3-F7 tasks) |
| 0f7795d (US2, T008-T010, T029) | 45271161653 | Go unit tests and vet, Python script and hook tests (the new template-Go hook tests included) and every static job green; the recorded-model jobs (planted, Story 2, end-to-end, service tests) failed only on the recording miss at the tests step (`Request was not matched`), expected until T015; the stopped-run jobs cancelled after the Go job |
| c2e6f79 (US2 review fix T030) | 45275603639 | Go unit tests and vet, Python script and hook tests and every static job green; each failed recorded-model job (planted, Story 2, end-to-end, service tests) logs only the recording miss, expected until T015; the stopped-run jobs cancelled |
| 7c3ab29 (US3, T012-T015) | 45305443242 | All 26 jobs green: Go unit tests and vet, every static job, the five planted jobs, the four stopped-run jobs, Story 2 (`TestUS2`, US2.7, US2.10 replaying with no miss), service tests with the recorded model (`TestPlantedValues`: both scenarios stopped in 3 of 3 trials, no miss), end-to-end; evals-current lists the analyst suite and the steps suite (tests, code, review cases) pending, cleared by T017. SC-005: the whole Story 2 suite, build included, took 7 min 46 s in CI; `TestUS2` alone took 180 s while synthesising locally |
| 7c3ab29 (T017, paid evals) | 45361420877 | Steps suite pass^3 on every case (tests 5, code 7 with `code/inject-hard-code`, review 9); analyst `example` failed 1 of 3 (its order scenario named the notes by id): T036 |
| cd31712 (T036) | 45368979732 (check), 45368979612 (evals) | Check run all 26 jobs green; evals: analyst, code and review pass^3; `tests/inject-feature-outside` failed 1 of 3 (its create step dropped the response; tests failed against the reference): T037 |
| 1a19147 (T037) | 45395859352 (check), 45395859607 (evals) | Check run green; evals: every suite pass^3 (analyst, steps tests, code, review); SC-004 holds |

## Threat-model input

The check reads code that an agent wrote, which may hold injected text: it parses it with
`go/parser` only, never runs it, and treats every literal as data. Its own inputs (the approved
contract and the step's files) come from the request and the folder as for every check.
