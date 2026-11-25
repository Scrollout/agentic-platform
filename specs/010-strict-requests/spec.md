# Feature Specification: The delivered service refuses requests its contract does not allow

**Feature Branch**: `010-strict-requests`

**Created**: 2026-11-25

**Status**: Draft

**Input**: Backlog task T202 (feature 009 evals run 45481119822, owner 2026-11-24; owner chose it
as the next work, 2026-11-25). The service the catalogue's pattern delivers does not enforce the
contract's request schema: a member is matched by its name without regard to case and a required
member may be absent, so `POST /notes` with `{"TEXT":"x"}` stores the text `"x"` (201), although
the contract requires `text`, which is absent (400). The review agent found it on the evals' clean
reference (`review/clean`, a confirmed witness), so that case is quarantined under T202 (feature
009 T045). Fix: the delivered service reads requests strictly (exact names, required members, no
unknown members where the contract forbids them), the clean reference is clean again, and the
quarantine is lifted.

**Serves needs** ([problem space](../../docs/overview/problem-space.md)): N-055 (the project's
specs hold at every stage of the lifecycle: the approved contract is the slice's spec of its
interface), N-051 (a result is accepted only when it passes checks that do not depend on a model's
judgement) and N-052 (a proven solution is captured once and reused: request handling comes with
the pattern instead of being written again by the code agent in each slice).
**Uses terms** ([glossary](../../docs/overview/glossary.md)): Slice, Pattern, Catalogue, Run,
Step, Check, Agent, Eval, Quarantine, Method, Accepted result, Request schema.
**Builds on**: [feature 001](../001-prose-to-accepted-slice/spec.md) (the slice, the catalogue's
one pattern and its steps, the generated server and the code step), [feature 006](../006-method-record/spec.md)
(an accepted result is reused only under the method that judged it), and
[feature 009](../009-no-hard-coded-values/spec.md) (the steps suites' quarantine, `review/clean`
quarantined under T202).

## Clarifications

### Session 2026-11-25

- Q: A request carries a body member the contract does not declare: what then? → A: follow the
  contract: refused only where the contract forbids other members (`additionalProperties: false`);
  otherwise it is ignored and never read (owner; FR-003).
- Q: What body does a refusal carry, given that the contract declares its own 400 body? → A: a new
  fit condition: every operation with a request body declares a 400 response whose JSON body is an
  object that requires a string `message` and nothing else; the platform fills `message`, naming
  the member and what is wrong. A slice that does not declare it does not fit (owner; FR-004,
  FR-011).
- Q: Which request rules does the platform enforce before the code step's code runs? → A: the shape
  only: exact names, required members, types, and other members as the contract says. Limits,
  allowed values and formats stay with the code step (using the declared values of feature 009),
  so the messages the slice's examples expect for them still hold (owner; FR-005).
- Found while planning (a prototype refuses `{"TEXT":"x"}` before the code runs; research E4).
  Q: Which request body schemas may a slice use, given that the platform must know every member
  exactly? → A: simple shapes only: `type`, `properties`, `required`, `additionalProperties` as
  `true` or `false`, `items` and local `$ref`s; value keywords (limits, allowed values, formats)
  and annotations are allowed and left to the code. Any other keyword that shapes a body
  (`allOf`, `anyOf`, `oneOf`, `not`, `if`, `patternProperties`, `additionalProperties` as a schema,
  and the like) makes the slice not fit (owner; FR-012).
- Q: A request body declared with a media type other than `application/json`? → A: the slice
  does not fit (owner; FR-012).
- Q: A body names the same member twice (`{"text":"a","text":"b"}`)? → A: refused with 400, naming
  the member (owner; FR-002).
- Analyze round 1 (fresh Sonnet, at 4764bca). Q: A body with several problems (on a closed object
  `{"TEXT":"x"}` lacks `text` and holds the undeclared `TEXT`): what does the refusal name? → A:
  the first problem only, in a fixed order (owner; FR-004). Q: The declared 400 body's `message`
  could carry a limit, an allowed value or a pattern that the platform's text breaks? → A:
  `message` must be a plain string, with no value keyword; otherwise the slice does not fit
  (owner; FR-011). Q: `nullable` (OpenAPI 3.0's way of allowing `null`; 3.1 writes a type list)
  in a request body? → A: the slice does not fit; a type list is the way (owner; FR-012).
  Following these answers (analyze round 1, A3, A4, D1, D3): a schema with `properties` or
  `required` but no `type` applies them to an object value; a member named in `required` but not
  in `properties` is declared, of any type; a request body given by a `$ref` to
  `components/requestBodies` is followed; a request body without a schema, or an unknown type
  name, does not fit; the size limit, an empty required body and the body's own type are refusals
  of FR-002.

## User Scenarios & Testing *(mandatory)*

The user is a developer who builds a slice with the platform, and the clients of the service it
delivers. Today the delivered service accepts a request the approved contract does not allow when
the request only looks like an allowed one: a member written in another case is taken as the
declared member, and a required member may be missing. Neither the derived tests nor the code
step's checks notice, because the slice's examples send only well-formed requests. A client that
sends a malformed request gets a success and a stored value it never meant to send.

### User Story 1 - A request the contract does not allow is refused (Priority: P1)

The delivered service reads a request's body only by the member names the contract declares,
exactly as written. A request whose body lacks a required member, or names a member only in
another case, is refused as a bad request and changes nothing.

**Why this priority**: this is the defect T202 names, found by the review agent on the clean
reference; until it is fixed the review suite cannot count its clean case.

**Independent Test**: build the evals' notes slice with the pattern; `POST /notes` with
`{"TEXT":"x"}` is answered 400 and no note is stored; `{"text":"x"}` is still answered 201.

**Acceptance Scenarios**:

1. **Given** a contract whose request body requires a member, **When** a client sends the body
   without it, **Then** the service answers 400 and stores nothing.
2. **Given** the same contract, **When** a client sends the member under a name that differs only
   in case (`TEXT` for `text`), **Then** the member is not taken as the declared one: the request
   is treated as lacking it.
3. **Given** a body that sends both the declared name and another-case name (`{"text":"a","TEXT":"b"}`),
   **When** the request is accepted, **Then** only the declared member's value (`"a"`) is used.
4. **Given** a body that is not valid JSON, or whose member has a type the contract does not allow
   (`{"text":5}`), **When** the request is sent, **Then** the service answers 400 and stores
   nothing.
5. **Given** a well-formed request that the slice's examples send, **When** it is sent, **Then**
   the answer is the one the examples expect, as before this feature.

---

### User Story 2 - Strict reading comes with the pattern, not from the agent (Priority: P2)

How a request is read is part of what the pattern generates from the approved contract, before the
code step runs. The code agent does not write it, cannot weaken it, and cannot be the reason a slice
gets it right or wrong.

**Why this priority**: a fix the code agent has to remember in every slice is advice, not a check
(N-051, N-052); it depends on Story 1's behaviour being defined.

**Independent Test**: run the code step's checks on an implementation that reads the body itself
around the generated reading, or changes the generated code: the step is rejected. On the
pattern's generated project, with the stub in place, a malformed request is already refused before
any code exists.

**Acceptance Scenarios**:

1. **Given** a newly generated project, **When** a malformed request is sent to the stub, **Then**
   it is refused as in Story 1, before the code step has run.
2. **Given** a slice whose contract has an operation with a request body that declares no 400
   response, or one whose body is not an object requiring a string `message` and nothing else,
   **When** its contract is approved or a run starts, **Then** the slice does not fit the pattern,
   and the reason names each such operation.
3. **Given** a slice whose request body schema uses a keyword outside the simple shapes, or whose
   request body has a media type other than `application/json`, **When** its contract is approved
   or a run starts, **Then** the slice does not fit, and the reason names each such location.
4. **Given** a code step whose work changes the generated request reading, **When** it is checked,
   **Then** the existing checks reject the change.
5. **Given** a run accepted before this feature, **When** it is replayed, **Then** its accepted
   results are not reused under the changed pattern (feature 006), and its steps are judged again.

---

### User Story 3 - The clean reference is clean and counted again (Priority: P3)

The evals' clean reference implementation is rebuilt on the changed pattern, the review agent finds
nothing on it, and `review/clean` leaves the quarantine and counts in the review suite again.

**Why this priority**: it proves the fix where the defect was found, and ends T202; it depends on
Stories 1 and 2.

**Independent Test**: the steps evals' `review/clean` case runs unquarantined and passes 3 of 3
trials; the review suite reports pass^3 with it counted.

**Acceptance Scenarios**:

1. **Given** the clean reference on the changed pattern, **When** the review eval runs, **Then**
   `review/clean` passes 3 of 3 trials and counts in the suite's result.
2. **Given** the changed pattern, **When** every steps suite runs, **Then** every counted case
   passes at pass^3, as before this feature.

### Edge Cases

- A body member the contract does not declare (`{"text":"x","colour":"red"}`): ignored and never
  read, unless the contract forbids other members, then refused (FR-003).
- A member in another case where the contract forbids other members (`{"TEXT":"x"}` with
  `additionalProperties: false`): refused, naming the first problem in the fixed order: the
  missing `text` (FR-004).
- The refusal's body: an object with one member, `message`, naming the member and what is wrong
  (FR-004); a 400 body schema with optional members besides `message` fits, and those members are
  left out.
- A request member that breaks a limit, an allowed value or a format the contract declares (a text
  over its `maxLength`): not refused by the platform; the code step's code answers it as the rules
  and examples say (FR-005).
- An operation whose 400 body schema requires more than `message`, or whose `message` is not a
  string: the slice does not fit (FR-011).
- Members nested in objects or arrays: the same rules apply at every level the contract describes.
- A member named twice (`{"text":"a","text":"b"}`), at any level: refused, naming the member
  (FR-002).
- A request body schema built with `allOf`, `oneOf` or another keyword outside the simple shapes,
  or a body declared as `text/plain`: the slice does not fit (FR-012).
- A schema with no `type`: any JSON value is allowed there; when it has `properties` or
  `required`, they apply to an object value; with neither, an object is kept as sent.
- A member named in `required` but not in `properties`: declared, of any type, and kept.
- `additionalProperties: true` next to `properties`: undeclared members are removed as everywhere
  (FR-003), so a generated map of additional members is always empty (a stated limit).
- A body that is only white space: missing. A request whose `Content-Type` is not JSON: its body is
  judged as JSON all the same (a stated limit). A body over the limit is refused with 400, not 413,
  so that it carries the declared body.
- Very deep nesting (a 1 MiB body of `[[[[...`): refused as not valid JSON beyond the JSON reader's
  nesting limit, never walked.
- A `null` value for a required member whose type does not allow `null`: refused as the wrong type.
- An operation whose request body is optional and absent: accepted, as the contract allows.
- An empty body where the contract requires one: refused with 400.
- A body far larger than any the contract could describe: a stated limit of the service, refused
  before it is read in full (the limit is named in the pattern's README).
- Path and query parameters: already read by the generated code by their declared names and types;
  they are unchanged by this feature.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The delivered service MUST read each request body member only by its name exactly as
  the contract declares it; a member whose name differs, including only in case, MUST NOT be taken
  as a declared member.
- **FR-002**: The delivered service MUST answer 400, and change nothing, when a request body is
  larger than the stated limit, is empty or only white space where the contract requires it, is
  not valid JSON, names a member twice in one object at any level, is not of the type the contract
  states, lacks a member the contract requires, or holds a member whose type the contract does not
  allow.
- **FR-003**: A body member the contract does not declare MUST be refused with 400 where the
  contract forbids other members, and otherwise ignored and never read.
- **FR-004**: A refusal MUST answer 400 with the operation's declared 400 body: an object holding
  only `message`, a text that names the first problem in a fixed order: the member (by its path in
  the body) and what is wrong (missing, wrong type, not declared, named twice), or that the body
  is too large, missing, not valid JSON or of the wrong type. The text MUST be the same for the
  same request.
- **FR-005**: The platform MUST enforce before the code step's code runs only the request's shape:
  exact member names (FR-001), required members, member types, and other members (FR-003). Limits,
  allowed values and formats MUST stay with the code step's code, judged by the derived tests; this
  is a stated limit of strict reading.
- **FR-006**: The strict reading MUST be generated by the pattern from the approved contract, in
  generated code the code step cannot change (feature 001's GeneratedCodeUnchanged), and MUST apply
  to the generated stub as well as to the code step's implementation.
- **FR-007**: The pattern changed by this feature MUST be a new pattern version, so that accepted
  results judged without strict reading are not reused (feature 006).
- **FR-008**: The pattern's README MUST state which request rules the service enforces before the
  code step's code runs, how a refusal is answered, and each limit (FR-005, the body size limit).
- **FR-009**: The evals' clean reference MUST be rebuilt on the changed pattern; `review/clean`
  MUST leave the quarantine and T202 MUST be closed only when it passes 3 of 3 trials counted.
- **FR-010**: The steps evals MUST gain a case, or the planted scenarios a plant, in which a
  request with a member in another case or a missing required member is sent, and the build is
  accepted only when the service refuses it (Constitution Principle XIX: every fixed agent failure
  adds an eval case).
- **FR-011**: A slice MUST fit the pattern only when every operation with a request body declares a
  400 response with a JSON body whose schema is an object that requires a string `message` and no
  other member, where `message` carries no value keyword (a limit, an allowed value, a pattern or a
  format); the fit condition's reason MUST name each operation that does not.
- **FR-012**: A slice MUST fit the pattern only when every request body is declared only as
  `application/json`, and its schema, followed through local `$ref`s, shapes the body only with
  `type`, `properties`, `required`, `additionalProperties` (`true` or `false`) and `items`; any
  other keyword that constrains which members or items a body has MUST make the slice not fit, and
  the reason MUST name each location. A body without a schema, an unknown type name and `nullable`
  MUST make the slice not fit too. Value keywords (limits, allowed values, formats, `const`) and
  annotations are allowed.

### Key Entities

- **Request schema**: what the approved contract states a request body may hold: its members' exact
  names, which are required, their types, and whether other members are allowed.
- **Refusal**: the 400 answer to a request the request schema does not allow, with a `message`
  naming what is wrong; nothing is changed.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Every malformed request of Story 1 (a missing required member, a member only in
  another case, the wrong type, invalid JSON) is refused with 400 and changes nothing, on a freshly
  generated project and on an accepted one, in 100% of the checks' and acceptance tests' cases.
- **SC-002**: Every well-formed request in the slices' examples gets the answer the examples expect;
  no accepted fixture slice regresses.
- **SC-003**: `review/clean` passes 3 of 3 trials, counted, and every counted case of every steps
  suite passes at pass^3.
- **SC-004**: A successful run still reaches accepted software in under 30 minutes (feature 001,
  SC-007).

## Assumptions

- The catalogue still holds one pattern, a Go HTTP API (feature 001, FR-022); strict reading is
  written for it. Other patterns state their own.
- Only JSON request bodies are in scope; the pattern's contracts use `application/json`.
- The derived tests keep judging the behaviour the rules state; strict reading judges only what the
  contract's request schema states.
- The fit condition (FR-011) narrows which slices fit; the evals' notes slice and the fixture
  slices already declare such a 400 for their request bodies, or are changed to (each fixture
  change is an existing-test change and needs the owner's approval).
- Adding strict reading changes the pattern's method identity, so earlier runs' accepted steps are
  judged again on replay (feature 006); this is intended.

## Dependencies

- Lifting the quarantine changes existing tests (`src/tests/evals/steps/quarantine_test.go` and
  `quarantine_print_evals_test.go` name `review/clean` under T202) and the clean reference's
  recordings: each needs the owner's approval of that change (ADR 0029).
- The analyst proposes the slice's contract: its instruction is told about the fit condition
  (FR-011), and its evals must still pass at pass^3; the fit condition, not the instruction,
  decides (Principle XX).
- A paid eval run of the steps suites (and of the analyst suite, if its instruction changes) is
  needed before merge (owner's go-ahead).
- Any open-source component strict reading adopts is evaluated in the plan (Principle VI) and is a
  new dependency of the delivered project, which needs the owner's approval (Principle XVI).
