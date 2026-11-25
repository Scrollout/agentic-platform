# Research: The delivered service refuses requests its contract does not allow

Phase 0 of the [plan](plan.md) for the [spec](spec.md). Each decision names its deciding source:
the code on `main` (`5dea1d5`), a reproduced experiment, or a cited document. Decisions marked
*owner* were put to the project owner as direct questions on 2026-11-25 (spec, Clarifications).

## Sources

| ID | Source |
|----|--------|
| S1 | `src/catalogue/api-feature-go/README.md` "Steps and project layout" and "Module template": the seam `internal/app/app.go`, `New() gen.ServerInterface`; the tests serve `gen.Handler(app.New())`; the code step writes `internal/**`, `cmd/**`; generate writes `gen/**` |
| S2 | `src/tests/evals/steps/testdata/ref/impl/internal/app/app.go`: the clean reference decodes the body itself with `json.NewDecoder(r.Body).Decode(&in)` into `gen.NewNote`; so does every recorded code answer. The generated `ServerInterface` hands the code the raw request |
| S3 | Experiment E1 (oapi-codegen v2.8.2 output for the evals' notes contract) |
| S4 | Experiment E2 (Go's `encoding/json` v1 on member names) |
| S5 | `oapi-codegen` v2.8.2 source, `pkg/codegen/configuration.go` `OutputOptions.UserTemplates` ("Override built-in templates from user-provided files") and `codegen.go` (user templates parsed after the built-in ones, replacing a template of the same name); `pkg/codegen/templates/stdhttp/std-http-handler.tmpl`: `Handler`, `HandlerFromMux`, `HandlerFromMuxWithBaseURL` all call `HandlerWithOptions`, which registers each operation with `m.HandleFunc(<method> + " " + options.BaseURL + <path>, wrapper.<Handler>)` |
| S6 | Experiment E4 (a prototype of the user template and a request-shape check) |
| S7 | [kin-openapi](/components/kin-openapi.md) v0.153.0 README and `openapi3filter/options.go` (module cache, read 2026-11-25): supports OpenAPI 3.1; `openapi3filter` validates a request against the whole schema (every keyword; its options exclude whole parts such as the body, not single keywords) and never removes a member; oapi-codegen's `nethttp-middleware` wraps it |
| S8 | [Go JSON duplicate names](/references/go-json-duplicate-names.md): v1 keeps the last of two equal names; `encoding/json/v2` is behind `GOEXPERIMENT=jsonv2` in Go 1.26 (the pattern's Go, `images.lock` `golang:1.26`); reproduced again: the v2 sources carry `//go:build goexperiment.jsonv2` |
| S9 | `src/engine/checks/fit.go`: fit conditions are named in `pattern.yaml`, mapped in `conditions`, evaluated on the contract parsed once as `go.yaml.in/yaml/v3` nodes; `src/engine/checks/declared.go`: the walk over those nodes, `RenderDeclared`, `DeclaredValuesGenerated` (ADR 0086) |
| S10 | `src/tests/recorded/recordings/__files/analyst-notes-answered.sse`: the acceptance tests' example contract declares `createNote` with no `requestBody` and a content-less 400; `src/tests/acceptance/planted_values_test.go` adds a `requestBody` and no 400 body |
| S11 | `src/engine/api/pattern_flag_test.go` line 25 and `src/engine/slices/slices_patternflag_test.go` line 27: both list the pattern's fit conditions by name and fail on a condition result they do not list |
| S12 | Constitution: Principle VI (extend an adopted tool through its extension points; custom code needs a written justification), XVII (derive, drift-check), XIX (a fixed agent failure adds an eval case), XX (a proven solution is captured as a deterministic asset); Engineering Principles "Values that check themselves" (input parsed at every trust boundary) |
| S14 | Go's `encoding/json` source (`/usr/local/go/src/encoding/json/scanner.go`, Go 1.27.1, same in 1.26): `maxNestingDepth = 10000` is enforced by the scanner that `Valid`, `Unmarshal` and `Decoder.Decode` use; `Decoder.Token` keeps its own stack with no depth limit (analyze round 1, S1) |
| S13 | [RFC 6901](https://www.rfc-editor.org/rfc/rfc6901) (JSON Pointer) and [JSON Schema 2020-12 Validation, 6.1.1](https://json-schema.org/draft/2020-12/json-schema-validation#section-6.1.1) (`type` names `null`, `boolean`, `object`, `array`, `number`, `string`, `integer`; an integer is any number with a zero fractional part), the schema dialect of OpenAPI 3.1 |

## E1. Where the delivered service reads a request body

oapi-codegen v2.8.2, run with the template's `oapi-codegen.yaml` over
`src/tests/evals/steps/testdata/slice/contract.yaml`:

- The strict server's `CreateNote` decodes with `runtime.BindJSONBody` and answers a decode error
  with `http.Error` (text, not the contract's `Error` body).
- But the seam serves the plain `ServerInterface` (S1), so the strict server is not used: the
  code step's own code reads the body (S2).
- `HandlerWithOptions` is the one place every operation is registered; `Middlewares` from its
  options wrap each operation inside `ServerInterfaceWrapper`, but only when the caller passes
  them, so the code's `cmd/` could serve without them.

## E2. How Go's encoding/json v1 reads member names

Reproduced with Go 1.27.1 (same `encoding/json` v1 behaviour as 1.26), decoding into
`struct{ Text string \`json:"text"\` }`:

| Body | Result |
|------|--------|
| `{"TEXT":"x"}` | `"x"`, no error |
| `{"text":"a","TEXT":"b"}` | `"b"` (the last of the names that fold to `text`) |
| `{"TEXT":"b","text":"a"}` | `"a"` |
| `{"text":"a","text":"b"}` | `"b"` |
| `{}`, `{"text":null}` | `""`, no error |
| `{"text":5}` | error |

So validating the body (S7) is not enough: a body that passes validation (`{"text":"a","TEXT":"b"}`
with other members allowed) still gives the code `"b"`. Only a body that holds nothing but the
declared members, under their exact names, is read the same by every decoder.

## E4. A prototype

In a scratch module: the template's `oapi-codegen.yaml` plus
`output-options.user-templates: {stdhttp/std-http-handler.tmpl: std-http-handler.tmpl}`, where the
file is v2.8.2's template with one change, each registration wrapped:
`m.HandleFunc(..., refuseMalformed("<METHOD> <contract path>", wrapper.<Handler>))`, written
`refuseMalformed({{.Method | toGoString}}+" "+{{.Path | toGoString}}, ...)` (an earlier run with
`{{.OperationId}}` gave oapi-codegen's Go name, `CreateNote`, so the key is the method and the
contract's path instead, which the contract states directly); a hand-written
`refuseMalformed` that checks `required` and types and re-encodes only the declared members; the
clean reference's `internal/app` unchanged. Served with `gen.Handler(app.New())`:

| Body | Answer |
|------|--------|
| `{"TEXT":"x"}` | 400 `{"message":"member \"text\" is required"}` |
| `{"text":"a","TEXT":"b"}` | 201, note text `"a"` |
| `{"text":5}` | 400, must be a string |
| `nope` | 400, not valid JSON |
| `{"text":"x","colour":"red"}` | 201, text `"x"` |

And `gen.HandlerWithOptions(app.New(), gen.StdHTTPServerOptions{})` refuses `{"TEXT":"x"}` too: the
wrap is in the registration, which every entry point reaches. The clean reference needs no change.

## R1. Where requests are refused

- **Decision**: the pattern's template overrides oapi-codegen's `stdhttp/std-http-handler.tmpl`
  through `user-templates` (S5): the same text as the pinned release's, except that each
  operation's registration is wrapped by the generated request check
  (`refuseMalformed("<METHOD> <contract path>", wrapper.<Handler>)`, E4). The file lives at
  `template/gen/std-http-handler.tmpl`, named in `template/gen/oapi-codegen.yaml`, so
  `go generate` (GenerationRepeatable) uses it too. The tests, the seam, the stub and the code
  step's `cmd/` are unchanged; every entry point (`Handler`, `HandlerFromMux...`,
  `HandlerWithOptions`) refuses, so the code cannot serve around it (E4), and
  GeneratedCodeUnchanged keeps `gen/` as generated (FR-006).
- **Drift with upstream**: the file's first line is a Go-template comment (`{{/* ... */}}`, not
  written into `gen/api.gen.go`) naming the release it was copied from
  (`oapi-codegen v2.8.2 pkg/codegen/templates/stdhttp/std-http-handler.tmpl`); a new test in
  `scripts/tests/` fails when that release differs from `images.lock`'s oapi-codegen pin, so a
  bump of the tool makes someone copy the new template and reapply the one change. That test
  proves the label only; that the rest equals upstream is checked by the reviewer of each bump
  (analyze G2). A missing template file makes oapi-codegen fetch the name as a URL, which fails
  offline: the procedure test covers that generation then fails (analyze D5).
- **Rationale**: user templates are oapi-codegen's own extension point (S12, Principle VI);
  one wrapped line, in the one function every operation passes through (S5).
- **Alternatives**:
  - *Serve the strict server through the seam* (`app.New()` returning a `StrictServerInterface`):
    the strict server still decodes with v1 (case-insensitive, E2), its decode error is text, and
    the seam, the tests agent's and code agent's instructions, every recording and the reference
    change; rejected.
  - *A new seam the tests serve* (`gen.Server(app.New())`): the code's `cmd/` could still serve
    `gen.Handler` without it; rejected.
  - *A code-step check that the code reads bodies only through a generated decoder*: static
    analysis of every way to read a body is open-ended; rejected.

## R2. What checks a request (OSS evaluation, Principle VI)

- **Decision**: a small generic engine, `template/gen/requests.go`, checks a body against a
  *request shape* and re-encodes it with only the declared members; the shapes are derived from
  the approved contract into `gen/request_shapes.go` by the generate procedure (R3). Standard
  library only (`encoding/json` with `UseNumber` and its token stream, `Valid`), so the
  delivered project gains no dependency.
- **Candidates**:
  - *kin-openapi `openapi3filter` with oapi-codegen's `nethttp-middleware`* (S7; kin-openapi is
    already adopted, [component](/components/kin-openapi.md)): validates every keyword, which the
    owner ruled out for the platform (limits stay with the code, spec FR-005); never removes a
    member, so `{"text":"a","TEXT":"b"}` still reaches the code as `"b"` (E2); its errors are
    text, not the declared 400 body; and it adds kin-openapi, its YAML and JSON Pointer modules and
    the middleware to every delivered project's `go.mod` and `vendor/`. Rejected.
  - *A JSON Schema validator over a derived shape-only schema* (for example
    `santhosh-tekuri/jsonschema`, the [jsonschema](/components/jsonschema.md) component's
    project): validates, but cannot remove members either, so the walk over the shape is needed
    anyway; it would then do half the job and add a dependency. Rejected.
  - *`encoding/json/v2`* (case-sensitive, refuses duplicate names): behind `GOEXPERIMENT` in the
    pattern's Go 1.26 (S8), and the code's own v1 decoder would still fold case. Rejected for now;
    the engine's duplicate-name detection reads v1's token stream instead.
- **Custom code justification**: no mature component both checks a body's shape and removes the
  members the contract does not declare; the walk is bounded by the owner's simple shapes (R4):
  about 200 lines, one generic engine with its tests (Principle XVII "Keep the how small").

## R3. The request shapes and their drift check

- **Decision**: `checks.RequestShapes(contract)` (beside `DeclaredValues`, one owner: S9) walks the
  approved contract's operations; for every request body it resolves local `$ref`s and builds a
  shape: `types` (the `type` keyword's names, empty = any), `properties` (exact names, in contract
  order), `required` (contract order), `closed` (`additionalProperties: false`), `items`.
  `checks.RenderRequestShapes` writes `gen/request_shapes.go`: one Go map from the operation's
  method and contract path (`"POST /notes"`, the key the template passes, E4) to its shape.
  Every shape is an unexported variable `requestShape<N>` (N from 1, in document order of first
  use), declared without links; `init()` then sets each shape's members and items, so a recursive
  schema is a cycle of pointers without a Go initialisation cycle (analyze A5). Names starting
  `requestShape` cannot meet oapi-codegen's exported names or `gen/declared.go`'s `Declared...`
  constants; the procedure writes `gen/request_shapes.go` before `gen/declared.go`, so the names
  `GenNames` sees are the same at generation and in DeclaredValuesGenerated (analyze U1). The
  generate procedure writes the file; a new generate-step check, `RequestShapesGenerated`, passes when the file is byte for byte
  `RenderRequestShapes` of the approved contract (as ADR 0086 for `gen/declared.go`).
- **Rationale**: Principle XVII (derived, drift-checked); `go generate` does not write the file, so
  GenerationRepeatable cannot see it drift (ADR 0086's reasoning).
- **Derivation details** (analyze A3, A4, owner's answers): `properties`, `required` and
  `additionalProperties` apply to an object value whether or not `type` is present; a name in
  `required` but not in `properties` is a declared member of any type; a `requestBody` that is a
  `$ref` to `#/components/requestBodies/...` is followed; every `$ref` walk keeps a visited set, so
  a cycle ends.
- **Keys**: the method in upper case and the path exactly as the contract writes it, so the
  procedure needs nothing from oapi-codegen's naming rules (E4).

## R4. Which contracts fit (two fit conditions)

- **Decision** (owner, spec FR-011, FR-012): two new fit conditions on the contract, both on the
  YAML nodes the other conditions parse (S9):
  - `RefusalDeclared`: every operation with a `requestBody` declares a `400` response whose
    `application/json` schema (followed through local `$ref`s) is `type: object` with `required`
    exactly `[message]` and `properties.message` of `type: string`; other optional properties are
    allowed; `message` carries no value keyword (`maxLength`, `minLength`, `pattern`, `format`,
    `enum`, `const`; owner, analyze round 1). The `400` key counts as written `400` or `"400"`;
    `4XX` and `default` do not count. The reason names each operation (`paths//notes/post`).
  - `RequestBodiesSupported`: every `requestBody`'s `content` has exactly the key
    `application/json`, and its schema, through local `$ref`s, uses as shaping keywords only
    `type`, `properties`, `required`, `additionalProperties` (boolean only) and `items` (a schema);
    a body with no `schema`, a `type` naming anything but the seven JSON Schema types, a type
    list other than one type and `"null"` (oapi-codegen v2.8.2 answers any other with
    "unhandled Schema type"; found by T004's author, owner 2026-11-25), `nullable`
    (owner, analyze round 1), and any of `allOf`, `anyOf`, `oneOf`, `not`, `if`, `then`, `else`, `dependentSchemas`,
    `dependentRequired`, `patternProperties`, `propertyNames`, `prefixItems`, `contains`,
    `unevaluatedProperties`, `unevaluatedItems`, `$dynamicRef`, a non-boolean
    `additionalProperties`, or an unknown keyword that is not an `x-` extension fails, naming its
    location. Value keywords (`maxLength` ... `const`, `enum`, `format`, `pattern`,
    `minProperties`, `maxProperties`, `minItems`, `maxItems`, `uniqueItems`) and annotations
    (`title`, `description`, `default`, `examples`, `example`, `deprecated`, `readOnly`,
    `writeOnly`, `$comment`) are allowed (fail closed: the list is the README's).
- **Rationale**: the platform can only refuse with the contract's own 400 body if that body is one
  it can fill (owner); and only strip members of a shape it knows exactly (owner).
- **Existing tests** (S10, S11): the two tests that list the fit conditions by name, and the
  planted declared-value scenario's added `requestBody` (it must declare the 400 body), change
  with the owner's approval.

## R5. The engine's behaviour (FR-001 to FR-005)

- **Decision**: for an operation with a shape, before anything else of the operation runs:
  1. Read at most 1 MiB (`http.MaxBytesReader`); more is refused ("larger than 1048576 bytes"),
     with 400 so that it carries the declared body.
  2. An empty body, or one of JSON white space only: refused when the contract's
     `requestBody.required` is true, else passed on as sent.
  3. `json.Valid`: not exactly one JSON value, or nested deeper than its 10000 levels, is refused
     as not valid JSON (S14), so nothing deeper is ever walked.
  4. Read the value with `json.Decoder.Token` (`UseNumber`), building the tree; the first name
     that appears twice in one object, anywhere in the document (document order, in subtrees that
     will be removed too), is refused (owner: refused at any level).
  5. Walk value and shape together, depth first, reporting the first problem (owner: one message)
     in this order at each object: a required member missing (contract order), an undeclared
     member when `closed` (body order), then each declared member present (contract order); array
     items in index order. A value whose JSON type is not in `types` is refused. `integer` takes a
     number whose text has a zero fractional part (S13), decided on the text (digits, fraction,
     exponent) without building the number; one with a fraction or exponent is written again as a
     plain integer, and one whose plain form would have more than 32 digits is refused as not an
     integer (a stated limit; analyze S2); a plain integer literal is kept as sent at any length,
     and with `number` among the types nothing is rewritten (analyze round 2, N2, N8).
  6. Re-encode the value with only declared members (exact names) where `properties` is declared;
     a schema with no `properties` keeps the object as it is. Replace `r.Body` and
     `r.ContentLength`, then call the operation.
  7. A refusal answers `400`, `Content-Type: application/json`, body `{"message": "<text>"}`.
- **Messages** (FR-004; same request, same text): `the request body is missing`,
  `the request body is not valid JSON`, `the request body is larger than 1048576 bytes`,
  `the request body must be <types>`, `member <pointer> is required`,
  `member <pointer> must be <types>`, `member <pointer> is not declared`,
  `member <pointer> is named twice`, where `<pointer>` is the member's RFC 6901 JSON Pointer
  (S13), cut to its first 64 runes (no ellipsis), then quoted with `strconv.Quote` (analyze D2), and `<types>` the type names joined
  with " or ", each with its article (`a string`, `an integer`, `null`).
- **Rationale**: deterministic, no model, refuses before any state changes (FR-002); the limits
  (1 MiB) are stated in the README (FR-008).

## R6. Pattern version, method, agents, evals and recordings

- **Decision**:
  - the pattern becomes `1.2.0` (FR-007); the generate step gains `RequestShapesGenerated`; the
    method identity follows from the changed engine and template files (`scripts/method-digest.py`,
    as features 006 and 009);
  - the analyst's instruction says that every operation with a request body declares the 400
    body (`message`), and request bodies are simple JSON shapes (Dependencies); its recordings are
    renewed by the recorded README's text replacement, and its suite runs paid before merge;
  - the code agent's instruction says that malformed requests are refused before its code runs
    and need no handling; the tests agent's and review agent's instructions are unchanged;
  - every step recording misses (the generated project's files change, as in feature 009's
    T015): the step sets are synthesised again (Docker, no key; on the clean VM as in 009,
    with the owner's go-ahead);
  - evals (FR-010, Principle XIX): a new grader on the code step's built project sends
    `{"TEXT":"x"}`, `{"text":"a","TEXT":"b"}` and `{"text":"a","text":"b"}` to `POST /notes` and
    passes only on 400, 201 with `"a"`, and 400; `review/clean` leaves the quarantine (owner
    approval for `cases.go`, `quarantine_test.go`, `quarantine_print_evals_test.go`); a new
    planted scenario `malformed-request-accepted` is not needed, since the shape check is
    generated and the code cannot change it (R1): the engine's procedure tests prove it.
- **Rationale**: FR-007, FR-009, FR-010; Principle XIX; S12.
- **Cost**: one paid evals dispatch (analyst and steps suites) with the owner's go-ahead; one
  resynthesis of the step sets.

## Runs

| Commit | Run | Result |
|--------|-----|--------|

## Threat-model input

The shape check is the first code of the delivered service that reads untrusted request bodies.
It never evaluates them; it bounds their size (1 MiB), checks them with `json.Valid` before
reading them, whose scanner refuses more than 10000 levels of nesting (S14; `Decoder.Token` alone
has no limit), decides integers on their text without building the number, and walks at most as
deep as that limit. A refusal echoes a member's name only JSON-encoded and cut to 64
characters.
