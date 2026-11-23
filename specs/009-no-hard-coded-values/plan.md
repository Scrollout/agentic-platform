# Implementation Plan: Code that hard-codes a declared value is rejected

**Branch**: `009-no-hard-coded-values` | **Date**: 2026-11-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/009-no-hard-coded-values/spec.md` (need N-059)

## Summary

The code step gains two checks over the Go files it may write. DeclaredValuesNotRepeated rejects
a literal equal to a value the approved contract declares (a length, range, size limit, default,
`const` or enum value), except `0`, `1`, `-1` and `""`; DeploymentValuesConfigured rejects an
address, port, URL or credential literal and any read of the environment. Every declared value
gets a Go home: the generate procedure writes `gen/declared.go` (one constant per value; enum
values keep oapi-codegen's constants), and the template's `gen/config.go` holds the one
deployment value, `ListenAddress()` from `LISTEN_ADDRESS` (default `:8080`), documented there.
One collector in `engine/checks` finds the declared values for the procedure and the checks, and
the generate step's new DeclaredValuesGenerated proves `gen/declared.go` matches the contract
(research R7). The pattern becomes 1.1.0, the code agent's instruction names the homes, and the
evals, the recorded code answers and the planted suite follow (owner approved each existing test
change, research R6).

## Technical Context

**Language/Version**: Go 1.26 (the engine and its checks, as feature 001)

**Primary Dependencies**: none added; the standard library (`go/ast`, `go/parser`,
`go/constant`, `net/url`, `net/netip`, `net`) and `go.yaml.in/yaml/v3`, already the fit
conditions' parser

**Storage**: none; check results keep their shape

**Testing**: Go tests in `src/engine` (`checks`, `procedures`, `checksvc`) by a separate test
author, in new files; the step evals' grader self-tests (`src/tests/evals/steps`, new files plus
the approved changes); the acceptance planted suite with the recorded model (new file
`planted_values_test.go`, new step sets); CI's check run of the pushed commit as the full test
command; one paid steps-suite eval dispatch with the owner's go-ahead

**Target Platform**: the local platform (Compose), as feature 001

**Project Type**: the engine's checks and generate procedure, the catalogue pattern, the code
agent's instruction, their tests and evals

**Performance Goals**: both checks parse only the step's own files, well under a second for a
slice (SC-005 unchanged: a successful run under 30 minutes)

**Constraints**: no change to the check contract's shapes (one additive description); the seam
and the tests step are unchanged; the review agent is unchanged; existing tests change only as
the owner approved (Existing tests expected to change)

**Scale/Scope**: one collector, three checks, one generated file, one template file, the pattern,
one agent instruction, one eval case, two planted scenarios

## Constitution Check

*Evaluated before research and again after design: PASS.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Needs before solutions | Pass | Serves N-059 (with N-051, N-056) |
| II. Two planes | Not affected | No component or flow between planes changes |
| III. API-first | Pass | The checks are named in the pattern and stated in the check contract first |
| IV. Asynchronous operations | Not affected | No operation added |
| V, XII. Identity, tenants | Not affected | No call, token or tenant path changes |
| VI. Adopt mature OSS | Pass | oapi-codegen keeps generating; go-mnd and gosec G101 evaluated and do not do the job (research E2, R2, R4); custom code justified in R1, R2, R4 |
| VII. Providers | Not affected | No provider kind changes |
| VIII. Containers | Pass | No new image or tool; the check image is unchanged but for its program |
| IX. Knowledge stays true | Pass | ADR 0084, ADR 0085, ADR 0086; oapi-codegen component, a gosec reference, catalogue README, architecture overview and log at implementation |
| X. No silent loss | Not affected | A rejected step is retried within its attempts, then handed over (feature 001) |
| XI. Privacy | Pass | A check reason names a file, line and value of the generated project only |
| XIII. Supply chain | Pass | No dependency added |
| XIV. Safe defaults | Pass | Both checks fail closed on a file or contract they cannot read; the agent cannot change their inputs (ScopeKept, ContractMatchesApproval, GeneratedCodeUnchanged) |
| XV. Compatible APIs | Pass | Additive: two check names in the preview check contract's description |
| XVI. Human control | Pass | No dependency, CI or secret change; the owner merges |
| XVII. Declare, derive, converge | Pass | The contract declares; `gen/declared.go` is derived, generated twice and drift-checked by DeclaredValuesGenerated; the listen address is declared once in the template |
| XVIII. Abstractions say what | Pass | One collector and two checks, each named for what it judges |
| XIX. Evals | Pass | The code agent's instruction changes: the steps suite runs paid before merge; an adversarial injection case is added |
| XX. Deterministic acceptance | Pass | The point of the feature: a value's home is judged by a check, not by the instruction |
| Values that check themselves | Pass | The contract is parsed once at the check's boundary; a literal is read by `go/constant` |
| Checks prove they can fail | Pass | Each check has clean fixtures and planted violations, and reports what it examined |
| Existing tests change only with approval | Pass | Owner approved each change listed below by name (2026-11-23); each commit carries `Tests-approved-by:`; no other existing test changes without a new approval |
| Fresh review | Pass | A fresh read-only reviewer per story and before done, judging the CI run |
| Fix the cause | Pass | One deliberate check change, the commit hook's rule for a module template's Go files (T029), recorded in Exceptions |
| Layout | Pass | Files under `src/`, `specs/`, `scripts/` and `docs/` only |

## Decisions

Each decision that outlives the feature has a record in `docs/decisions/`.

| Decision | Research | Record |
|----------|----------|--------|
| A value the contract declares has its Go home in `gen/` (`gen/declared.go`, written by the generate procedure; enum constants from oapi-codegen), and the code step is rejected when its code repeats one as a literal, except `0`, `1`, `-1` and `""` | R1, R2 | [ADR 0084](../../docs/decisions/0084-declared-values-live-in-generated-code-and-are-never-repeated.md) |
| The delivered service's configuration is declared once in the template's `gen/config.go`; the code step is rejected for an address, port, URL or credential literal and for reading the environment | R3, R4 | [ADR 0085](../../docs/decisions/0085-deployment-values-come-only-from-declared-configuration.md) |
| `gen/declared.go` has its own drift check, DeclaredValuesGenerated, in the generate step | R7 | [ADR 0086](../../docs/decisions/0086-declared-values-file-has-its-own-drift-check.md) |

## Threat model

No external interface, tool, model, memory or access to protected content is added. One element
changes: the code step's checks read code a model wrote.

| Element | S | T | R | I | D | E | OWASP mapping | Mitigation |
|---------|---|---|---|---|---|---|---------------|------------|
| DeclaredValuesNotRepeated, DeploymentValuesConfigured (read agent-written code) | | ✓ | | | ✓ | ✓ | LLM01 prompt injection; ASI01 agent goal hijack (an injected "hard-code it"); ASI02 tool misuse | The checks parse with `go/parser` and never run the code; their inputs (the step's files and, for DeclaredValuesNotRepeated, the approved contract from the request) are bound like every check's; a file that does not parse fails; the check call's existing time limit bounds a huge file |
| Check reasons sent back to the agent in the next attempt | | ✓ | | ✓ | | | LLM01 (a literal's text enters the model's context); LLM02 sensitive information disclosure | A declared value in a reason is quoted with `%q` and cut to 64 characters; a deployment value's text never appears in a reason (spec FR-004); the agent treats every message part as data (its instruction) |
| `gen/declared.go` (rendered from the contract the analyst model wrote) | | ✓ | | | | ✓ | LLM05 improper output handling (contract text becoming Go code) | Strings are written with `strconv.Quote`, pointers in comments with `%q`, names restricted to `[A-Za-z0-9_]`; the project must build (GeneratedCodeBuilds) |

## Project Structure

### Documentation (this feature)

```text
specs/009-no-hard-coded-values/
├── plan.md              # this file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   └── declared-values.md   # the collector, gen/declared.go, gen/config.go, the two checks
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks
```

### Source Code (repository root)

```text
src/check/api/preview/2026-11-13-preview/openapi.yaml   # runCheck states the two checks
src/engine/checks/declared.go        # new: DeclaredValues, RenderDeclared, DeclaredValuesGenerated
src/engine/checks/values.go          # new: DeclaredValuesNotRepeated, DeploymentValuesConfigured
src/engine/checksvc/checks.go        # the three names in checkByName
src/engine/procedures/generate.go    # writes gen/declared.go
src/catalogue/api-feature-go/pattern.yaml       # 1.1.0, the three checks, the code agent digest
src/catalogue/api-feature-go/README.md          # the checks, declared.go, config.go
src/catalogue/api-feature-go/template/gen/config.go   # new
src/agents/code/instruction.md       # gen.ListenAddress(), gen/declared.go, the rules
src/engine/method/method.sha256      # derived (scripts/method-digest.py)
src/tests/evals/steps/               # graders, cases, ref main.go, new slice testdata
src/tests/acceptance/planted_values_test.go     # new: the two planted scenarios
src/tests/recorded/steps/                       # renewed code answers, new planted sets
scripts/hooks/pre-commit                        # a module template's Go files judged by gofmt (T029)
scripts/tests/test_pre_commit_template_go.py    # new: the hook's template rule (T029)
```

**Structure Decision**: the collector and the checks live in `engine/checks` beside the other
step checks, and the generate procedure (which already depends on `checks`) calls the collector,
so one owner decides what a declared value is.

**Existing tests expected to change** (the owner approved each by name, 2026-11-23, after analyze
round 1; `Tests-approved-by:` on each commit; any other existing test that needs a change is
stopped and asked for):

- `src/engine/checksvc/checks_test.go`: `patternCases` gains a clean and a planted case, with
  fixtures, for DeclaredValuesGenerated, DeclaredValuesNotRepeated and DeploymentValuesConfigured
  (`TestEveryPatternCheckIsCovered` requires one for every check the pattern names).
- `src/tests/evals/steps/testdata/ref/impl/cmd/notes/main.go`: listens on `gen.ListenAddress()`.
- `src/tests/acceptance/us2_test.go` line 84: the run records `api-feature-go@1.1.0`, not `@1.0.0`
  (owner, analyze round 2).
- `src/tests/recorded/steps/{happy,resume,planted/*}`: each code answer's `main.go` the same, and
  the mappings synthesised again (the code agent's instruction changes too).
- `src/tests/evals/steps/grade.go` (code graders gain the two checks), `cases.go`
  (`code/inject-hard-code`), `grade_test.go` (its `obeyed` simulation), `README.md` (the cases and
  graders tables).

## Exceptions

| Rule | Location | Reason |
|------|----------|--------|
| The commit hook refuses a staged Go file in no module of `src/go.work` (gofmt and go vet cannot judge it) | `scripts/hooks/pre-commit` (08a9def, T029) | A pattern's module template (`src/catalogue/<pattern>/template/`) compiles only inside a generated project, which the engine's procedure tests build, so its staged Go files are judged by `gofmt -l` alone and refused when unformatted; its `testdata/` is excluded, as for a module. Every other Go file in no module is refused as before. Owner approved, 2026-11-23; tests `scripts/tests/test_pre_commit_template_go.py` |

## Complexity Tracking

No violation.
