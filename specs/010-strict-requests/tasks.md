# Tasks: The delivered service refuses requests its contract does not allow

**Input**: Design documents from `specs/010-strict-requests/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Required. Outside-in, test-first: new tests are seen failing before the code; every
new fit condition and check has clean fixtures and planted violations.

**Organization**: the contract (Phase 1), then US1 (the request check: shapes, the template's
engine and handler override, `gen/request_shapes.go`, RequestShapesGenerated as engine code),
US2 (the pattern switches it on: 1.2.0, the two fit conditions, the check in the generate step,
the agents' instructions, the approved test changes, the recordings), US3 (the evals: the
malformed-request grader, `review/clean` out of the quarantine, the paid run). The template
change of US1 changes every generated project at once, so the recorded-model jobs miss from T008
until the recordings are synthesised again in T017 (as feature 009's T007 to T015; the owner is
asked before T008 is pushed).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an unfinished task)
- **[Story]**: US1, US2 or US3 (spec.md)

## Rules for every task

- **Contract first**: T001 states the request check, the two fit conditions and
  RequestShapesGenerated in the check contract and the catalogue README and is committed alone,
  before any test or code.
- **Test-first**: a test task and the code task that makes it pass are separate tasks, done by
  different authors; new tests go in new files. The code author never changes a test, a fixture
  or test data.
- **Existing tests**: only the changes plan.md lists by name change, each approved by the owner
  (T002); each commit that makes one carries `Tests-approved-by: Marius Gologan (<date of the
  approval>)`. Any other existing test that turns out to need a change is stopped and asked for,
  quoting what changes and why; no approval covers a class of tests.
- **Method identity**: every commit that changes a file `src/engine/method/method.json` lists
  (the engine, the check contract, the catalogue, the agents) runs `scripts/method-digest.py` and
  commits the converged `src/engine/method/method.sha256` with it.
- **Trailers**: `Approved-by: Marius Gologan (2026-11-12)` is the owner's standing approval to
  commit finished, tested work to protected paths; an existing test's change carries its own dated
  `Tests-approved-by:`.
- **No dependency** is added: the delivered project uses the standard library only; the engine
  `go.yaml.in/yaml/v3`, as now.
- **Fail closed**: a fit condition or check that cannot read the contract or a file fails, naming
  it; every passing result says what it examined.
- **Model text**: names and paths from the contract are written into Go with `strconv.Quote`;
  a refusal's pointer is cut to 64 runes, then quoted, and never carries a member's value.
- **Test time**: locally only the packages a task touches (`go test ./checks/ ./procedures/
  ./checksvc/` in `src/engine`, `go test ./evals/steps/ -run Grade` in `src/tests`, the one new
  Python test), each capped at 10 minutes; no full `scripts/check.sh`, Docker lab, image build or
  `synth-steps.sh` without the owner's go-ahead. The full test command is the CI run of the pushed
  commit; every push is told to the owner with the jobs it runs and waits for a go-ahead. A live
  eval run is paid and needs the owner's go-ahead.
- **Briefs**: every test-author and reviewer brief says: think through the edge cases and list
  those checked; it gives a time limit and a cap on full runs.
- **Fresh review**: each story and the feature end with a fresh review by a read-only agent on
  another model than the author's, in its own worktree at the branch commit with the uncommitted
  and untracked files, judging the CI run; at most 3 rounds. Then `/speckit-converge`.
- **Lint**: before each push of Go changes, the pinned golangci-lint with gosec on the changed
  lines.
- **Gate**: `/speckit-analyze` (fresh reviewer) must report no CRITICAL or HIGH before T001, and
  again after any change to spec, plan, tasks or a design artifact. Round 1 (fresh Sonnet, at
  4764bca): C1 CRITICAL (the code author changing `cases.go`; recordings without a named author),
  I1, I2, U1, S1 HIGH (`pattern_values_test.go` not listed; the spec's "naming both"; shape
  variable names; the nesting limit `Decoder.Token` does not have), 11 MEDIUM, 7 LOW; the owner
  answered four questions (one message, a plain `message`, no `nullable`, the recorded jobs red
  from T008 to T017 as a plan exception); fixed at their source (spec clarifications, FR-002,
  FR-004, FR-011, FR-012, edge cases; research S14, R1, R3, R4, R5; contract; data model; plan
  threat model, approved list, Exceptions; ADR 0089; T002, T003, T004, T006, T007, T009 to T011,
  T013, T015 to T017). Round 2 (fresh Sonnet, at f75fb74): every round-1 finding fixed; 0
  CRITICAL, 0 HIGH; N1-N4 MEDIUM (the `Properties` nil rule; the 32-digit rule's wording and the
  README's limits; `fit_test.go` not listed; the grader given to the code author), N5-N9 LOW;
  fixed at their source (spec FR-008, edge cases; research R5; contract; data model; plan
  approved list; ADR 0089; T001, T003, T004, T009, T010, T012, T015, T016). Gate passed.
- **Recordings** are test data: the analyst recordings (T013) and the synthesised step sets (T017)
  are made and committed by the coordinating session, which writes no code and no test of this
  feature, never by a code author.

---

## Phase 1: Setup (contract first)

- [x] T001 Contract (committed alone): the runCheck description of
  `src/check/api/preview/2026-11-13-preview/openapi.yaml` states RequestShapesGenerated as
  [contracts/strict-requests.md](contracts/strict-requests.md) does (what it compares, how it
  fails, fail closed), additively; `src/catalogue/api-feature-go/README.md` gains RefusalDeclared
  and RequestBodiesSupported in the fit-condition table (passes when, reason) with the allowed
  keyword lists, a "Request check" section (the handler template override and its one changed
  line, `gen/requests.go`, `gen/request_shapes.go`, the walk order, the messages table, the
  1048576-byte limit, the 10000-level nesting limit, the 64-rune cut, the 32-digit integer rule,
  what is left to the code: limits, allowed values, formats) and the module
  template table's new files; new `src/engine/checks/shapes.go` holds the Go interface of the
  contract draft (types, signatures, doc comments) with bodies that return an error or a failing
  `not implemented` result; `scripts/generate.sh` refreshes the embedded contract copies;
  `scripts/method-digest.py` converges; the contract lint passes; the draft then links to the
  authoritative copies
- [x] T002 Owner approvals: ask the owner to approve, by name, each existing test change plan.md
  lists ("Existing tests expected to change"), and record each approval here with its date; a
  change not approved is not made, and the task that needs it stops. Done: the owner approved all
  eleven, named one by one (2026-11-25): `fit_test.go` line 34 (scope corrected by the owner, 2026-11-25, to the five contract
  conditions, T010); `pattern_values_test.go`;
  `pattern_flag_test.go` line 25 and `slices_patternflag_test.go` line 27; `checks_test.go`
  `patternCases`; `us2_test.go` line 84 and `runs_ops_test.go` lines 449-450;
  `planted_values_test.go` (the second anchored replacement); `cases.go` (`review/clean`
  unquarantined); `quarantine_test.go` and `quarantine_print_evals_test.go`; `grade.go` and its
  README; the analyst mappings; every step set's mappings. Trailer: `Tests-approved-by: Marius
  Gologan (2026-11-25)`

---

## Phase 2: User Story 1 - A request the contract does not allow is refused (Priority: P1) 🎯 MVP

**Goal**: a generated project refuses a malformed body before any operation code runs and passes
on only the declared members under their exact names (FR-001 to FR-006, FR-008; research E4, R1,
R2, R3, R5).

**Independent Test**: T003-T006 pass; quickstart 2 and 3: the notes contract's generated project
served with the stub answers `{"TEXT":"x"}` with 400 `{"message":"member \"/text\" is required"}`;
with the clean reference, `{"text":"a","TEXT":"b"}` gives 201 with `"a"`.

- [x] T003 [P] [US1] Test author: new `src/engine/checks/request_shapes_test.go`: `RequestShapes`
  returns one `RequestBody` per operation with a `requestBody`, in document order, `Key` the
  method in upper case and the contract's path (`POST /notes`, `PUT /notes/{id}`), `Required`
  from `requestBody.required` (absent is false); shapes: `Types` from `type` as a name or a list
  (`[string, "null"]`), empty when absent; `Properties` in contract order with exact names
  (`text` and `Text` are two members), then names only in `required` in `required` order; nil
  when the schema has neither `properties` nor `required` nor `additionalProperties: false`;
  `properties: {}` non-nil and empty; `additionalProperties: false` with no `properties` closed,
  every member undeclared; an empty type list, a non-string type item (unquoted YAML `null`), a
  boolean schema and a YAML anchor or alias are errors naming the location;
  `RequiredMembers` in contract order; `Closed` only for `additionalProperties: false`; `Items`;
  a local `$ref` target is one shared shape (two uses give the same pointer; a recursive schema is
  a cycle and the walk ends); value keywords and annotations ignored; a body schema with an
  unsupported keyword, a non-boolean `additionalProperties`, a non-local `$ref`, or a media type
  other than `application/json` is an error naming the location; text that does not parse, holds
  two documents or a duplicate key is an error. A name in
  `required` but absent from `properties` makes it a member of any type; `properties` and
  `required` without `type` apply to an object value; a `requestBody` `$ref` to
  `#/components/requestBodies/...` is followed; a recursive schema, a self-alias (`A: {$ref: A}`),
  a `$ref` chain and a `$ref` loop end (visited set). `RenderRequestShapes`: the header and
  `package gen`; keys, member names written with `strconv.Quote` (a name holding `"`, a newline or
  `*/` stays a valid string); every shape an unexported `requestShapeN` in document order of first
  use, linked in `init()`; a recursive schema's file builds and type-checks together with
  oapi-codegen's output (a schema named `NewNote` or `RequestShape1` does not collide); the same
  bodies give the same bytes; with none, a valid file with an empty map. GeneratedCodeUnchanged
  fails a code step that edits `gen/requests.go`, `gen/request_shapes.go` or
  `gen/std-http-handler.tmpl` (one planted case each; spec US2 scenario 4).
  RequestShapesGenerated over a fixture project: passes on a rendered file, saying what it
  examined; fails on a missing, edited or stale file and on an approved contract that does not
  parse, naming what
- [x] T004 [P] [US1] Test author: new `src/engine/procedures/request_check_test.go` (with the
  pinned oapi-codegen, `AP_OAPI_CODEGEN`, as the existing procedure tests): generating the evals'
  notes contract (`src/tests/evals/steps/testdata/slice/contract.yaml`, copied as a fixture)
  gives `gen/request_shapes.go` equal to `RenderRequestShapes` of its bodies, the template's
  `gen/requests.go` and `gen/std-http-handler.tmpl`, and a `gen/api.gen.go` whose every
  registration calls `refuseMalformed("<METHOD> <path>", ...)`; two generations give the same
  bytes; GenerationRepeatable and RequestShapesGenerated pass on the output; a built test program
  serves the project over `httptest` and, through `gen.Handler(gen.Stub{})`,
  `gen.HandlerFromMux` and `gen.HandlerWithOptions` alike, answers with `Content-Type:
  application/json` and the exact messages of the contract draft: `{"TEXT":"x"}` 400
  `member "/text" is required`; `{"text":5}` and `{"text":null}` 400 `member "/text" must be a
  string`; `{"text":"a","text":"b"}` 400 `member "/text" is named twice`; `nope`, `{"text":"a"}x`
  and two JSON values 400 `the request body is not valid JSON`; an empty body 400 `the request
  body is missing`; `[]` 400 `the request body must be an object`; a body of 1048577 bytes 400
  `the request body is larger than 1048576 bytes`; `{"text":"x"}`, `{"text":"a","TEXT":"b"}` and
  `{"text":"x","colour":"red"}` reach the stub (501); `GET /notes` with a body is not judged; a
  body of white space only 400 `the request body is missing`; a 1 MiB body nested 20000 levels
  deep 400 `the request body is not valid JSON`, and the service still answers the next request;
  a duplicate inside a member the shape removes (`{"text":"x","colour":{"a":1,"a":2}}`) 400
  `member "/colour/a" is named twice`, and a duplicate before a missing member at another level
  is the one named (document order first); a `Content-Type: text/plain` request judged the same;
  a pointer over 64 runes, with multibyte runes at the cut, cut to 64 runes then quoted.
  A second fixture contract covers: nested objects and arrays (`/items/0/name` in the pointer,
  `~1` and `~0` encoded), `additionalProperties: false` (`{"text":"x","TEXT":"y"}` 400
  `member "/TEXT" is not declared`; `{"TEXT":"y"}` 400 `member "/text" is required`, the
  required check coming first), the walk order at one object (a name twice before a missing member before an
  undeclared one before a type), `integer` (`1` and `1.0` and `1e0` and `10e-1` pass and reach the
  handler as `1`; `1.5` refused; `1e31` passes written as 32 digits; `1e32` and `1e999999`
  refused as not an integer, quickly, without building the number), a type list with `null`, a schema with no `type` (any value), a schema with
  no `properties` (the object kept as sent), an optional body absent (reaches the handler), a
  member name over 64 runes (cut in the message); a 40-digit plain integer kept as sent; an
  exponent beyond int64 (`1e99999999999999999999`) refused quickly; `[integer, number]` with
  `1.0` kept as sent; `-0.0` for `integer` written as `0`; duplicates ordered by their second
  occurrence (`{"a":1,"b":{"c":1,"c":2},"a":3}` names `/b/c`), a recursive schema; and, with a recording
  handler in place of the stub, the body the handler reads holds only declared members under
  exact names, with `Content-Length` matching; generation fails, naming the file, when the
  template's `gen/std-http-handler.tmpl` is absent (oapi-codegen would fetch the name as a URL);
  the template file's first line, a template comment, does not appear in `gen/api.gen.go`
- [x] T005 [P] [US1] Test author: new `src/engine/checksvc/request_shapes_test.go`: runCheck
  serves `RequestShapesGenerated`, judging against the text inside the `contract` entry of
  `approved` (canonical JSON `{"contract": "<text>"}`), not `api/openapi.yaml` in the folder; a
  request without the approved contract, or whose content is not that JSON, fails closed
- [x] T006 [P] [US1] Test author: new `scripts/tests/test_handler_template_pin.py`: the first line
  of `src/catalogue/api-feature-go/template/gen/std-http-handler.tmpl` names the oapi-codegen
  release `src/deploy/images.lock` pins (`v2.8.2` today); a fixture copy naming another release
  fails, naming both; the test reports the file and the pin it compared, and its docstring says it
  proves the label only (the rest is checked by the reviewer of each bump)
- [x] T007 [US1] Code author: `src/engine/checks/shapes.go` (`RequestShapes`,
  `RenderRequestShapes`, RequestShapesGenerated, on the YAML nodes the fit conditions parse); its
  entry in `src/engine/checksvc/checks.go`; `gen/request_shapes.go` written in
  `src/engine/procedures/generate.go` before `gen/declared.go`, so `GenNames` sees the same files
  at generation and in DeclaredValuesGenerated (a contract `RequestShapes` rejects refuses the
  step naming why); the template's `gen/oapi-codegen.yaml` gains
  `output-options.user-templates: {stdhttp/std-http-handler.tmpl: std-http-handler.tmpl}`; new
  `template/gen/std-http-handler.tmpl` (v2.8.2's text, its first line naming that release, each
  registration wrapped by `refuseMalformed({{printf "%s %s" .Method .Path | toGoString}}, ...)` (one literal, `"POST /notes"`));
  new `template/gen/requests.go` (the generic engine of the contract draft's Behaviour and
  Messages, standard library only: `json.Valid` before `Decoder.Token`, integers decided on their
  text); `template/vendortools/tools.go` and the template's `go.mod`
  unchanged; T003-T006 pass; every existing test in `checks`, `procedures` and `checksvc` stays
  green or is stopped and asked for; `method.sha256` converged
- [x] T008 [US1] Commit (tests and code in separate commits); ask the owner before pushing (the
  recorded-model jobs will miss until T017); push (check jobs); fix until CI is green but for
  those jobs; record the run in research; fresh review of US1; `/speckit-converge` for US1.
  CI 45581805361 at e061694 (research Runs). Review round 1 (fresh Sonnet, at d6ba493): F1, F2
  blocking (`additionalProperties: False` and `required: True` read as not set, since only the
  text `false`/`true` counts; README "Request check", Principle XIV), F3 (a non-shaping key beside
  a `$ref` neither refused nor read), F4 (a 1 MiB array of `{}` rebuilds the shape's member map per
  object) and F5 (only POST bodies served in the tests): T024 to T026; F6 (the "escaped
  duplicate" case at `request_check_test.go:706` holds no escape; T024 adds a real one, so the
  mislabelled case changes only with the owner's approval): T027; F7 (aliases followed outside
  schemas) closed: consistent with the YAML reading of the whole contract, and an alias in a schema
  is refused; F8 (the GeneratedCodeUnchanged test supplies its own `gen/**`): T028; F9 (the pin
  test proves the label only) closed: stated in its docstring and the README. `/speckit-converge`
  for US1 (after review round 2, at 6755b51): converged, nothing appended
- [x] T024 [US1] Test author (new files): `src/engine/checks/request_shapes_regress_test.go`:
  `additionalProperties: False` and `FALSE` close a shape and `True` does not; `required: True`
  and `TRUE` make a body required; any key beside a `$ref` other than an annotation or an `x-` key
  (`not`, `allOf`, `nullable`, `maxLength`) is an error naming the location;
  `src/engine/procedures/request_regress_test.go` (served, as T004): `PUT`, `PATCH` and `DELETE`
  bodies on a path with a parameter are refused and rewritten like `POST`; a duplicate written with
  an escape (`{"text":"a","\u0074ext":"b"}`) is named twice; an escaped declared name passes byte
  for byte; a 1 MiB array of `{}` against a shape of 300 properties is answered within 2 seconds;
  seen failing where the code is wrong
- [x] T025 [US1] Code author: read a YAML boolean as YAML does (F1, F2), refuse every key beside a
  `$ref` but annotations and `x-` keys (F3) in `src/engine/checks/shapes.go`; build each shape's
  member lookup once, not per object (F4), in `template/gen/requests.go` (the fixture templates' copies keep
  compiling with the same types and need no change); T003-T006 and T024 pass
- [x] T026 [US1] Commit, push (owner go-ahead), record the run; review round 2 checks only F1-F5
  and the changed lines. Done: CI 45591199302 at 822e79a (research Runs); review round 2 (fresh
  Sonnet): F1-F5 fixed, no blocking finding, so US1's review converged in 2 rounds. N1 (the
  "escape" cases of `request_regress_test.go` lines 45-46 and 49-50 hold no escape, as F6): T029;
  N2 (no object of 16 or more members exercises the duplicate check's switch to a map): T030; N3
  (a non-boolean `requestBody.required` is refused, unstated): fixed in the README
- [x] T027 [US1] Owner approval to make `request_check_test.go:706` carry the escape it is named
  for (or drop it as a duplicate of line 705, T024 covering the escape). Owner, 2026-11-25: fix it (the case carries the escape it is named for), in a new
  session; trailer `Tests-approved-by: Marius Gologan (2026-11-25)`
- [x] T029 [US1] Owner approval to make the "escape" cases (four named at approval, five found) of
  `src/engine/procedures/request_regress_test.go` (lines 45-46, 49-50) carry real JSON escapes
  (`\u0074ext`), as T027 for line 706; with T027 and T028 in a new session. Owner, 2026-11-25:
  approved; trailer `Tests-approved-by: Marius Gologan (2026-11-25)`. Owner, 2026-11-25 (new
  session): the file holds five such cases (lines 45, 46, 50-51, 52-53, 54); all five carry a
  real escape
- [x] T030 [US1] Test author (new file): a body object of 17 or more members with a duplicate at
  positions 1 and 17, 16 and 17, and 17 and 18, each named twice (the request check's switch from a
  scan to a map at 16 members); with T027 to T029
- [x] T028 [US1] Owner approval for `TestGeneratedCodeUnchangedCoversTheRequestCheckFiles` to read
  the generate step's `writable` from the real `pattern.yaml` instead of its own `gen/**` (F8). Owner,
  2026-11-25: approved, in a new session (with T027), by a test author; trailer
  `Tests-approved-by: Marius Gologan (2026-11-25)`. Done (T027-T030): a3fdd87, tests by a test author
  (Sonnet), committed by the coordinating session (Opus, its trailer); the escapes verified on disk; T030 seen failing on two planted faults in a scratch
  copy (the map not seeded; names after the switch not recorded)

---

## Phase 3: User Story 2 - Strict reading comes with the pattern, not from the agent (Priority: P2)

**Goal**: the pattern 1.2.0 names RequestShapesGenerated and the two fit conditions; contracts
that it cannot serve do not fit; the analyst writes contracts that fit and the code agent knows
malformed requests are refused for it (FR-006, FR-007, FR-011, FR-012; research R4, R6).

**Independent Test**: T009-T011 pass; quickstart 1: a contract without the 400 body, with
`allOf`, or with `text/plain` does not fit, naming the location; `TestUS1`, `TestUS2`,
`TestPlanted` and `TestPlantedValues` replay with no miss (T018).

- [ ] T009 [P] [US2] Test author: new `src/engine/checks/fit_requests_test.go`: RefusalDeclared
  passes on the evals' notes contract, on the acceptance example (no `requestBody`) and with the
  `Error` schema behind a `$ref` or inline, with optional properties besides `message`; fails,
  naming each operation (`paths//notes/post`) in document order, when the 400 is absent, has no
  `content`, no `application/json`, a schema that is not `type: object`, `required` other than
  exactly `[message]` (missing, `[message, code]`, `[]`), `message` absent or not `type: string`,
  or a `$ref` that does not resolve; `message` with `maxLength`, `minLength`, `pattern`, `format`,
  `enum` or `const`; the key written `400` and `"400"` both count, `4XX` and `default` alone do
  not; a `$ref` loop in the 400 schema ends (visited set); the 400 object schema with
  `minProperties`, `not`, `allOf`, `const` or `enum`; both `400` and `"400"` present; a `400`
  given by a `$ref` to `#/components/responses/...` is followed (`400` and `"400"` together
  are one duplicate key to the contract's reader: unreadable, so it fails closed). RequestBodiesSupported passes on the notes contract and on a
  schema using every allowed keyword, annotation and an `x-` key; fails, naming the location, for
  each of `allOf`, `anyOf`, `oneOf`, `not`, `if`, `then`, `else`, `dependentSchemas`,
  `dependentRequired`, `patternProperties`, `propertyNames`, `prefixItems`, `contains`,
  `unevaluatedProperties`, `unevaluatedItems`, `$dynamicRef`, an unknown key, an
  `additionalProperties` schema, a nested property or `items` using one of them, one reached only
  through a `$ref`, `nullable: true`, a shaping key beside a `$ref`, a name twice in
  `required`, a `requestBody` without `content`, `type: [integer, number]` and `[string, integer, "null"]`
  (only one type and `"null"` may be listed), a body with no `schema`, a `type` of `str` or `[string,
  date]`, a `$ref` loop (it ends, visited set; a recursive schema passes and its walk ends, as
  `RequestShapes` builds it), and a `content` with
  `text/plain` alone or beside `application/json`, an empty type list, an unquoted YAML `null`
  in a type list, a boolean schema, a YAML anchor or alias; a property named `oneOf` or
  `nullable` and an `x-` value holding `allOf` pass (names, not keywords); both
  conditions fail every slice whose contract does not parse (fail closed) and say what they
  examined on a pass
- [ ] T010 [P] [US2] Test author A (approved changes, `Tests-approved-by:`), tests first, seen
  failing against the unchanged pattern: `src/engine/checks/fit_test.go` line 34's `conditions`
  gains RefusalDeclared and RequestBodiesSupported and `contractConditions` becomes the five
  contract conditions, listed by name or built by dropping RulesNameOneSystem from `conditions`,
  not a slice (every condition but RulesNameOneSystem; owner, 2026-11-25, as "the first
  five" held RulesNameOneSystem and missed RequestBodiesSupported); `src/engine/cmd/engine/pattern_values_test.go` expects
  version `1.2.0` (lines 3, 5, 49-50) and the generate step's checks at line 43 with
  RequestShapesGenerated after DeclaredValuesGenerated; `src/engine/api/pattern_flag_test.go` line 25 and
  `src/engine/slices/slices_patternflag_test.go` line 27 list RefusalDeclared and
  RequestBodiesSupported; `src/engine/checksvc/checks_test.go` `patternCases` gain a clean and a
  planted case, with fixtures, for RequestShapesGenerated; `src/engine/api/runs_ops_test.go`
  lines 449-450 and `src/tests/acceptance/us2_test.go` line 84 expect `api-feature-go@1.2.0`;
  `src/tests/acceptance/planted_values_test.go`: besides the anchored edit that adds
  `createNote`'s `requestBody`, a second anchored replacement replaces its content-less `400`
  (`'400': {description: empty text}`) with an `application/json` body
  `$ref: '#/components/schemas/Error'`, and the appended `components` gains `Error` (object,
  required `[message]`, `message` a string), so the scenario's contract still fits
- [ ] T011 [P] [US2] Test author B: new `src/engine/cmd/engine/pattern_conditions_test.go`: the
  embedded and source pattern's conditions are the four of today followed by RefusalDeclared and
  RequestBodiesSupported (the version and the generate step's checks are
  `pattern_values_test.go`'s, T010)
- [ ] T012 [US2] Code author: RefusalDeclared and RequestBodiesSupported in
  `src/engine/checks/fit.go` (sharing the keyword lists with `shapes.go`: one owner);
  `src/catalogue/api-feature-go/pattern.yaml` `version: 1.2.0`, both conditions, the generate
  step's `checks` `[GenerationRepeatable, GeneratedCodeBuilds, DeclaredValuesGenerated,
  RequestShapesGenerated, DependenciesAllowed, ScopeKept]`, the new agent digests;
  `src/agents/analyst/instruction.md`: every operation with a request body declares a 400
  response whose JSON body is an object requiring only the string `message`, and request bodies
  are `application/json` objects built only with `type`, `properties`, `required`,
  `additionalProperties` (`true` or `false`) and `items`, every body with a schema, types named
  only from the seven JSON Schema types and `null` written in a type list (`[string, "null"]`),
  never `nullable`, `allOf`, `oneOf` or the like; `message` a plain string with no limit, allowed
  value, pattern or format; `src/agents/code/instruction.md`: a malformed body (missing
  or misnamed members, wrong types) is refused before its code runs, so its code checks only the
  rules' values; `scripts/generate.sh` and `scripts/method-digest.py` converge; T009-T011 pass
- [ ] T013 [US2] Analyst recordings (the coordinating session, not the code author): renew `src/tests/recorded/recordings/mappings/analyst-*.json`
  by the recorded README's text replacement for the analyst's instruction change and digest
  (approved in T002, `Tests-approved-by:`); the answers stay byte-identical
- [ ] T014 [US2] Commit (tests, code, recordings in separate commits); push (owner go-ahead; check
  jobs); fix until CI is green but for the step recordings' misses (T017); record the run; fresh
  review of US2; `/speckit-converge` for US2

---

## Phase 4: User Story 3 - The clean reference is clean and counted again (Priority: P3)

**Goal**: the evals judge malformed requests; `review/clean` leaves the quarantine; every suite
passes at pass^3 on the changed pattern (FR-009, FR-010, SC-003; research R6).

**Independent Test**: T015 passes; quickstart 5: the paid run reports every counted case pass^3,
`review/clean` counted.

- [ ] T015 [P] [US3] Test author B (approved changes, `Tests-approved-by:`), tests first: new
  `src/tests/evals/steps/grade_requests_test.go`, the grader's self-test: the malformed-request grader passes on the
  reference implementation in a project generated on the changed pattern (`{"TEXT":"x"}` 400,
  `{"text":"a","TEXT":"b"}` 201 with `"a"`, `{"text":"a","text":"b"}` 400) and fails, naming the
  request, on a project whose generated handler was replaced by the unwrapped one;
  `src/tests/evals/steps/quarantine_test.go` and `quarantine_print_evals_test.go` expect no case
  quarantined (`review/clean` counted), the quarantine's report form still tested with a
  synthetic case
- [ ] T016 [US3] Test author A, not T015's (approved changes, `Tests-approved-by:`; a grader and an
  eval case are tests): the malformed-request grader in `src/tests/evals/steps/grade.go` among the
  code graders, and in its `README.md` table; `src/tests/evals/steps/cases.go` drops
  `Quarantine: "T202"` from `review/clean`; T015 passes
- [ ] T017 [US3] The coordinating session (not the code author) synthesises the mappings of every step set with `synth-steps.sh` (owner go-ahead;
  Docker, no key; on the clean VM as in feature 009, `scripts/check.sh --prepare` first on a fresh
  clone), the old mappings removed first; review the diffs against their scripts
  (`src/tests/recorded/README.md`): the generated files, the instruction and the input hashes only
  (approved in T002, `Tests-approved-by:`)
- [ ] T018 [US3] Commit (tests, code, recordings in separate commits); push (owner go-ahead; every
  check job and `evals-current`); fix until CI is green with `TestUS1`, `TestUS2`, `TestPlanted`
  and `TestPlantedValues` replaying with no miss; record the run and `TestUS2`'s duration (SC-004)
  in research
- [ ] T019 [US3] Paid evals for the analyst and steps suites (owner go-ahead; add the branch to
  environment `dev`'s branch policies first): every counted case passes^3, `review/clean` counted
  among them; record the run in research; a failing case is assessed as a defect against agreed
  intent with the owner (ADR 0035), never retried until it passes
- [ ] T020 [US3] Fresh review of US3; `/speckit-converge` for US3. US2 scenario 4 (a change to the
  generated code is rejected) is the existing GeneratedCodeUnchanged cases and planted
  `edit-judging-tests`; scenario 5 (no reuse under the new method) is feature 006's
  `src/engine/machine/reuse_method_test.go`

---

## Phase 5: Polish & cross-cutting

- [ ] T021 Docs: `docs/architecture/overview.md` (the request check in the delivered service, the
  two fit conditions), `src/engine/README.md` (the check), `src/tests/evals/steps/README.md` (no
  case quarantined), N-051, N-052 and N-055 in `docs/overview/problem-space.md` (scope and state),
  backlog T202 checked `closed: feature 010` in `specs/backlog.md`, `docs/log.md`;
  `python3 scripts/okf.py check` reports OK
- [ ] T022 Final fresh review of the feature, judging the CI run of the head and the paid evals
  run; `/speckit-analyze` until no CRITICAL or HIGH; `/speckit-converge` until it finds nothing
- [ ] T023 Merge: `evals-current` on the branch lists no pending suite; the owner fast-forwards
  `main`; remove the branch from environment `dev`'s policies; delete the branch with the owner's
  OK; the spec's status says merged

---

## Dependencies & execution order

- T001 → T002 → US1 (T003-T006 in parallel → T007 → T008) → US2 (T009-T011 in parallel → T012 →
  T013 → T014) → US3 (T015 → T016 → T017 → T018 → T019 → T020) → T021 → T022 → T023.
- US2 needs US1's engine code (RequestShapesGenerated must exist before the pattern names it);
  US3 needs US2's pattern and instructions (the recordings are synthesised once, from the final
  instructions, T017).
- A paid run that forces an instruction change repeats T017 (about 80 minutes on the VM).

## Parallel examples

- US1: T003, T004, T005 and T006 by separate test-author sessions (different files).
- US2: T009, T010 and T011 by separate test-author sessions.

## Implementation strategy

- MVP is US1: once T007 lands, every newly generated project refuses malformed requests, the
  defect T202 names. US2 makes contracts fit what the check serves and the agents aware; US3
  proves it on the evals and lifts the quarantine.
- The recorded-model jobs miss from T008 to T017 (the generated files change); the owner is asked
  before T008's push, as in feature 009.
