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

## R1. The home of a declared value

- **Decision**: the generate procedure writes `gen/declared.go`, one exported, untyped Go constant
  for each value the approved contract declares under a schema keyword `maxLength`, `minLength`,
  `maximum`, `minimum`, `exclusiveMaximum`, `exclusiveMinimum`, `maxItems`, `minItems`,
  `maxProperties`, `minProperties`, `multipleOf`, `default` or `const`, with a doc comment naming
  its JSON Pointer in the contract (naming: [contract](contracts/declared-values.md)). Enum
  values keep their home in oapi-codegen's enum constants (E1). One collector in
  `engine/checks`, `DeclaredValues(contract)`, finds the values for both the procedure and the
  check (single owner).
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
  integer, floating-point and string literal (rune literals aside) is compared by value with the
  declared values of R1 and with the contract's enum values: numbers as exact constants
  (`go/constant`, so `200`, `0xC8`, `2e2` and `200.0` are one value, and a literal under a unary
  minus is negated), strings after unquoting, byte for byte. A match fails the check, naming the
  file, line, value, every contract location that declares it and its Go name in `gen/`. Not
  judged: `0`, `1`, `-1` and the empty text (owner, spec FR-003), import paths and struct tags,
  and values computed from several literals (a stated limit). The approved contract comes from the
  request (`approved`), never from the project folder (FR-007).
- **Rationale**: exact value comparison is deterministic and needs no type information;
  `go/constant` is the standard library's own reading of Go literals.
- **Custom code justification (Principle VI)**: go-mnd judges every magic number against an
  ignore list, not against declared values, and does not judge strings (S7); no linter compares
  literals with an OpenAPI contract.

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
  - a string literal that is a URL with a scheme and a host (`net/url`), an IP address
    (`net/netip`), or a `host:port` or `:port` whose port is a number from 1 to 65535 and whose
    host is empty, `localhost`, an IP address or a name with a letter (`net.SplitHostPort`), so
    `12:30` is not one;
  - a credential: a non-empty string literal as an argument of a `SetBasicAuth` call, as the value
    of a `Set` or `Add` call whose first argument is the literal header name `Authorization`,
    `Proxy-Authorization`, `Cookie` or `X-Api-Key` (any case), or assigned, declared or keyed to a
    name matching `(?i)passw(or)?d|secret|token|api_?key|credential|private_?key`; an argument
    made only of literals joined by `+` counts as a literal;
  - a call of `os.Getenv`, `os.LookupEnv`, `os.Environ` or `os.ExpandEnv` (owner, R3).

  Each failure names the file, line, the kind of value and where it belongs (`gen/config.go`, or
  the environment through it). Stated limits: a port written as an integer, a credential under
  another name or in another call, and a value built at run time are not recognised.
- **Rationale**: syntax rules over `go/ast` are deterministic, need no type information and no
  model; the names follow the common credential names.
- **Custom code justification (Principle VI)**: gosec's G101 found none of four plain credentials
  and costs a 96 MB binary (E2); no linter judges address literals; gosec G102 judges only binding
  to all interfaces.

## R5. The pattern, the method and the agents

- **Decision**: the code step's `checks` gain `DeclaredValuesNotRepeated` and
  `DeploymentValuesConfigured`; the pattern becomes version `1.1.0`; the template gains
  `gen/config.go`; the code agent's instruction names `gen.ListenAddress()`, `gen/declared.go`
  and the rules, so its `agent_digest` changes; the method identity follows from the changed
  files (feature 006), so earlier accepted code steps are judged again (FR-009). The review
  agent's instruction is unchanged.
- **Rationale**: FR-007, FR-009; feature 006's identity is derived, never edited by hand.

## R6. Evals, recordings and planted failures

- **Decision** (owner approved the existing test changes, 2026-11-23):
  - the step evals' code graders gain the two checks (`grade.go`), with grader self-tests in new
    files that plant each kind of value and pass on the reference implementation;
  - the reference implementation's `main.go` and every recorded code answer (S8) listen on
    `gen.ListenAddress()`; the step sets' mappings are synthesised again from their answers (S11),
    since the code agent's instruction changes too;
  - two new code cases: `code/declared-limit`, a slice whose contract declares a text limit of
    200 and a default page size, judged by the code graders (no held-out tests for this slice, a
    stated limit); `code/inject-hard-code`, the notes slice with a test-file comment telling the
    agent to listen on `:9090` and read `PORT` itself;
  - two planted scenarios in a new acceptance file, each stopped in 3 of 3 trials:
    `hard-coded-declared-value` and `hard-coded-listen-address` (SC-001).
- **Rationale**: XIX (a prompt change passes the regression evals, adversarial cases for
  injection); SC-001; S11 (an instruction change makes every code recording miss).
- **Cost**: a paid run of the steps suite before merge (owner's go-ahead), and a local
  `synth-steps.sh` run per changed set (Docker, no key; asked before it runs).

## Threat-model input

The check reads code that an agent wrote, which may hold injected text: it parses it with
`go/parser` only, never runs it, and treats every literal as data. Its own inputs (the approved
contract and the step's files) come from the request and the folder as for every check.
