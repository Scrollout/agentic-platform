# Implementation Plan: The delivered service refuses requests its contract does not allow

**Branch**: `010-strict-requests` | **Date**: 2026-11-25 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/010-strict-requests/spec.md` (backlog T202; needs
N-051, N-052, N-055)

## Summary

The pattern's template overrides one oapi-codegen template, `stdhttp/std-http-handler.tmpl`,
through oapi-codegen's `user-templates`: every operation's registration is wrapped by a request
check, so every way of serving the generated handler refuses a malformed body before the stub or
the code step's code runs (research E4, R1). The check is a small generic engine in the template,
`gen/requests.go` (standard library only), over request shapes the generate procedure derives
from the approved contract into `gen/request_shapes.go`, kept by a new drift check,
RequestShapesGenerated (R2, R3). It refuses a missing body, invalid JSON, a name twice, a missing
required member, a wrong type and, where the contract closes an object, an undeclared member,
with `400 {"message": ...}`; it passes on a body holding only the declared members under their
exact names (R5). Two new fit conditions keep contracts to what this can serve: RefusalDeclared
(every request body's operation declares that 400 body) and RequestBodiesSupported (JSON bodies of
simple shapes) (R4). The pattern becomes 1.2.0; the analyst and code agents' instructions say so;
the step recordings are synthesised again; the evals gain a grader for malformed requests and
`review/clean` leaves the quarantine (R6).

## Technical Context

**Language/Version**: Go 1.26 (the engine, its checks and procedures, and the delivered project)

**Primary Dependencies**: none added. oapi-codegen v2.8.2 (pinned) through its `user-templates`
extension point; the standard library (`encoding/json`, `net/http`, `strconv`, `unicode/utf8`) in the
delivered project; `go.yaml.in/yaml/v3` in the engine, already the fit conditions' parser

**Storage**: none; check results keep their shape

**Testing**: Go tests in `src/engine` (`checks`, `procedures`, `checksvc`) by a separate test
author, in new files, including a generated project served over HTTP; a Python test in
`scripts/tests/` for the template's upstream pin; the step evals' grader self-tests
(`src/tests/evals/steps`, new files plus approved changes); the acceptance suites with the
recorded model (renewed step sets); CI's check run of the pushed commit as the full test command;
one paid evals dispatch (analyst and steps suites) with the owner's go-ahead

**Target Platform**: the local platform (Compose), as feature 001

**Project Type**: the engine's fit conditions, checks and generate procedure, the catalogue
pattern and its template, two agent instructions, their tests, evals and recordings

**Performance Goals**: the request check reads at most 1 MiB and walks it once; generation adds
one file (SC-004 unchanged: a successful run under 30 minutes)

**Constraints**: the seam, the tests the tests agent writes and the clean reference's code are
unchanged; the delivered project gains no module; existing tests change only as the owner
approves (Existing tests expected to change)

**Scale/Scope**: two fit conditions, one collector and renderer, one check, one template override,
one template engine file, the pattern, two agent instructions, one grader, the quarantine lifted

## Constitution Check

*Evaluated before research and again after design: PASS.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Needs before solutions | Pass | Serves N-055, N-051, N-052 (spec); T202 is the backlog's statement of the defect |
| II. Two planes | Not affected | No component or flow between planes changes |
| III. API-first | Pass | The fit conditions and the check are named in the pattern and stated in the check contract and the catalogue README first |
| IV. Asynchronous operations | Not affected | No operation added |
| V, XII. Identity, tenants | Not affected | No call, token or tenant path changes |
| VI. Adopt mature OSS | Pass | oapi-codegen extended through its own `user-templates`; kin-openapi's request filter, a JSON Schema validator and `encoding/json/v2` evaluated and do not do the job (research R2); custom code justified there |
| VII. Providers | Not affected | No provider kind changes |
| VIII. Containers | Pass | No new image or tool |
| IX. Knowledge stays true | Pass | ADR 0087, 0088, 0089; the oapi-codegen component, the Go JSON reference, the catalogue README, the architecture overview and the log at implementation |
| X. No silent loss | Not affected | A refused request changes nothing; a failing step is retried within its attempts (feature 001) |
| XI. Privacy | Pass | A refusal names a member's pointer, never its value; check reasons name contract locations only |
| XIII. Supply chain | Pass | No dependency added; the template copy names its upstream release and is checked against the pin |
| XIV. Safe defaults | Pass | Refuses before any state changes; fails closed on a body it cannot read; a contract the check cannot serve does not fit |
| XV. Compatible APIs | Pass | Additive: one check name in the preview check contract's description; the delivered service's answers to well-formed requests are unchanged |
| XVI. Human control | Pass | No dependency, CI or secret change; the owner merges |
| XVII. Declare, derive, converge | Pass | The contract declares; `gen/request_shapes.go` is derived and drift-checked; the engine is one generic file with tests |
| XVIII. Abstractions say what | Pass | `refuseMalformed`, `RequestShapes`, RefusalDeclared, RequestBodiesSupported: each named for what it does |
| XIX. Evals | Pass | Two instructions change: the analyst and steps suites run paid before merge; a grader for malformed requests is added (the fixed failure); `review/clean` counts again |
| XX. Deterministic acceptance | Pass | Strict reading is a generated, checked asset, not an instruction (N-052) |
| Values that check themselves | Pass | The delivered service parses each body against its shape at its trust boundary, before any decision |
| Checks prove they can fail | Pass | Each new condition and check has clean fixtures and planted violations, and reports what it examined |
| Existing tests change only with approval | Pass | Each change listed below needs the owner's approval by name; each commit carries `Tests-approved-by:` |
| Fresh review | Pass | A fresh read-only reviewer per story and before done, judging the CI run |
| Layout | Pass | Files under `src/`, `specs/`, `scripts/` and `docs/` only |

## Decisions

Each decision that outlives the feature has a record in `docs/decisions/`.

| Decision | Research | Record |
|----------|----------|--------|
| The delivered service refuses a request its contract's shape does not allow before any of its code runs: the pattern overrides oapi-codegen's handler template so every operation's registration is wrapped by the generated request check | R1, R5 | [ADR 0087](../../docs/decisions/0087-delivered-service-refuses-malformed-requests-before-its-code.md) |
| Request shapes are derived from the approved contract into `gen/request_shapes.go` and checked by a generic engine in the template; RequestShapesGenerated is their drift check | R2, R3 | [ADR 0088](../../docs/decisions/0088-request-shapes-are-generated-and-drift-checked.md) |
| A slice fits only when each request body is a simple JSON shape and its operation declares the 400 body the platform fills (RefusalDeclared, RequestBodiesSupported) | R4 | [ADR 0089](../../docs/decisions/0089-contracts-declare-the-refusal-and-simple-request-shapes.md) |

## Threat model

One element is added to the delivered service: the request check, the first code that reads an
untrusted request body. No external interface of the platform, tool, model, memory or access to
protected content is added; the engine elements read the contract the analyst model wrote, as
before.

| Element | S | T | R | I | D | E | OWASP mapping | Mitigation |
|---------|---|---|---|---|---|---|---------------|------------|
| Request check (`gen/requests.go`, in the delivered service) | | ✓ | | ✓ | ✓ | ✓ | OWASP API Top 10 API3 (property-level authorization: an undeclared member reaching the code), API4 (resource consumption: size, nesting, huge numbers); parser differentials (E2) | Only declared members under exact names reach the code; a name twice is refused; 1 MiB cap before reading; `json.Valid` first, whose scanner refuses more than 10000 levels (`Decoder.Token` alone has no limit; research S14); integers decided on their text, at most 32 digits written again; the walk never evaluates a value; a refusal echoes only a JSON-encoded pointer cut to 64 runes, never a value |
| `gen/request_shapes.go`, the handler template (rendered from the contract the analyst model wrote) | | ✓ | | | | ✓ | LLM05 improper output handling (contract text becoming Go code) | Names and paths written with `strconv.Quote`; RequestShapesGenerated and GeneratedCodeUnchanged keep the files as derived; the project must build (GeneratedCodeBuilds) |
| RefusalDeclared, RequestBodiesSupported, RequestShapes (read the analyst's contract) | | ✓ | | | ✓ | | LLM01 (an injected contract), ASI02 | Parsed once as YAML nodes, never run; every `$ref` walk keeps a visited set, so a cycle ends; unknown shaping keywords fail closed; reasons name locations quoted |

## Project Structure

### Documentation (this feature)

```text
specs/010-strict-requests/
├── plan.md              # this file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   └── strict-requests.md   # the shapes, the engine's behaviour, the conditions, the check
├── checklists/
│   └── requirements.md
└── tasks.md             # /speckit-tasks
```

### Source Code (repository root)

```text
src/check/api/preview/2026-11-13-preview/openapi.yaml   # runCheck states RequestShapesGenerated
src/engine/checks/fit.go                 # RefusalDeclared, RequestBodiesSupported
src/engine/checks/shapes.go              # new: RequestShapes, RenderRequestShapes, RequestShapesGenerated
src/engine/checksvc/checks.go            # the check's name in checkByName
src/engine/procedures/generate.go        # writes gen/request_shapes.go
src/catalogue/api-feature-go/pattern.yaml            # 1.2.0, two conditions, the check, agent digests
src/catalogue/api-feature-go/README.md               # conditions, the request check, its limits
src/catalogue/api-feature-go/template/gen/oapi-codegen.yaml        # user-templates
src/catalogue/api-feature-go/template/gen/std-http-handler.tmpl    # new: v2.8.2's, one line changed
src/catalogue/api-feature-go/template/gen/requests.go              # new: the generic request check
src/agents/analyst/instruction.md        # the 400 body and simple shapes
src/agents/code/instruction.md           # malformed requests are refused before its code
src/engine/method/method.sha256          # derived (scripts/method-digest.py)
scripts/tests/test_handler_template_pin.py   # new: the template names images.lock's oapi-codegen
src/tests/evals/steps/                   # the malformed-request grader, review/clean unquarantined
src/tests/recorded/                      # analyst recordings renewed, step sets synthesised again
```

**Structure Decision**: the shape collector and its check live in `engine/checks` beside the
declared values, and the generate procedure calls them, so one owner decides what a request shape
is; the engine that applies shapes ships in the template, because it runs in the delivered
service.

**Existing tests expected to change** (each needs the owner's approval by name before its commit;
`Tests-approved-by:` on each commit; any other existing test that needs a change is stopped and
asked for):

- `src/engine/cmd/engine/pattern_values_test.go` (feature 009 T012): the pattern is version
  `1.2.0` (lines 3, 5, 49-50) and the generate step's checks at line 43 gain
  RequestShapesGenerated after DeclaredValuesGenerated (analyze round 1, I1).
- `src/engine/checks/fit_test.go` line 34: `conditions` gains RefusalDeclared and
  RequestBodiesSupported, and `contractConditions` becomes the first five, since its `expect`
  runs the real `pattern.yaml` (analyze round 2, N3).
- `src/engine/api/pattern_flag_test.go` line 25 and `src/engine/slices/slices_patternflag_test.go`
  line 27: the fit conditions listed gain RefusalDeclared and RequestBodiesSupported.
- `src/engine/checksvc/checks_test.go` `patternCases`: a clean and a planted case for
  RequestShapesGenerated (`TestEveryPatternCheckIsCovered` requires one per check).
- `src/tests/acceptance/planted_values_test.go`: the example contract's content-less `400` of
  `createNote` (`'400': {description: empty text}`) is replaced by a second anchored replacement,
  besides the one that adds the `requestBody`, by one with an
  `application/json` body `$ref: '#/components/schemas/Error'`, and the appended `components`
  gains `Error` (object, required `[message]`, `message` a string), so the scenario's contract
  still fits (analyze D6).
- `src/tests/acceptance/us2_test.go` line 84 and `src/engine/api/runs_ops_test.go` lines 449-450:
  the run records `api-feature-go@1.2.0`.
- `src/tests/evals/steps/cases.go`, `quarantine_test.go`, `quarantine_print_evals_test.go`:
  `review/clean` is no longer quarantined (T202 closed); the quarantine mechanism stays, with no
  case under it.
- `src/tests/evals/steps/grade.go` and its README: the code graders gain the malformed-request
  grader; a grader is a test, so a test author writes it, and another test author its self-test
  (analyze round 2, N4).
- `src/tests/recorded/recordings/mappings/analyst-*.json`: renewed for the analyst's instruction
  change (the recorded README's text replacement).
- `src/tests/recorded/steps/{happy,resume,planted/*}`: every mapping synthesised again, since the
  generated project's files and the code agent's instruction change.
- Any engine procedure test that compares a generated project's file list or bytes (found by the
  code author, then asked for).

## Exceptions

| Rule | Location | Reason |
|------|----------|--------|
| A change is done only when its CI run is green (ADR 0049) | the pushes of T008 and T014, until T017 | The template change makes every generated project's files differ, so every recorded step request misses until the step sets are synthesised again from the final instructions (T017), as feature 009's T007 to T015. Every other job must be green; the reviews of US1 and US2 judge the rest of the run. Owner approved, 2026-11-25 (analyze round 1, P1) |

## Complexity Tracking

No violation.
