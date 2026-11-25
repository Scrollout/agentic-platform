# Feature Specification: A proven slice becomes a pattern that builds without a model

**Feature Branch**: `011-capture-pattern`

**Created**: 2026-11-25

**Status**: Draft

**Input**: The remainder of need N-052 (owner chose it as the next work, 2026-11-25). Feature 001
left "proposing a new pattern" for a later feature (its FR-010 and Clarifications). Capture an
accepted slice's proven solution as a new pattern in the catalogue, so later slices of that kind
are built from it without the model generating it again.

**Serves needs** ([problem space](../../docs/overview/problem-space.md)): N-052 (a proven solution
is captured once and reused, without the model generating it again), N-050 (the same method on the
same input gives the same accepted result), N-051 (a result is accepted only when checks that do
not depend on a model's judgement pass) and N-053 (every result can be traced to the method,
inputs and rules that produced it).
**Uses terms** ([glossary](../../docs/overview/glossary.md)): Slice, Pattern, Catalogue, Run,
Step, Check, Agent, Method, Accepted result, Declared value, Captured pattern, Parameter, Proof.
**Builds on**: [feature 001](../001-prose-to-accepted-slice/spec.md) (the slice, the catalogue,
pattern fit, runs and their outcomes), [feature 006](../006-method-record/spec.md) (results are
reused only under the method that judged them), [feature 009](../009-no-hard-coded-values/spec.md)
(declared values) and [feature 010](../010-strict-requests/spec.md) (pattern `api-feature-go@1.2.0`).

## Clarifications

### Session 2026-11-25

- Q: What does a later slice built from a captured pattern still need a model for? → A: Nothing
  in the build. The accepted slice's tests and code become the captured pattern's template, with
  named parameters (names and declared values). A later slice fits only when it equals the source
  slice except for those parameters; the build fills them in and runs only checks, no agent step
  (owner; FR-003, FR-008 to FR-010).
- Q: Who lets a captured pattern into the catalogue? → A: The developer. The platform proposes it
  and proves it; the developer approves it before any run uses it, and the approval is recorded
  (owner; FR-006, FR-007).
- Q: A name parameter's value appears in the code as `note`, `Note`, `NOTE` and `notes`; how is a
  new value filled in? → A: in its case forms: the source value written all lower case, with its
  first letter upper case, and all upper case is each replaced by the new value written the same
  way; a plural is its own parameter (owner; FR-003, FR-009).
- Q: Which values may become parameters? → A: the platform proposes the slice's names and
  declared values; the developer may remove any of them and add another name or declared value of
  the slice before the proof (owner; FR-003).
- Q: When an approved slice fits a captured pattern, is it used automatically? → A: yes, the most
  recently approved fitting captured pattern; the developer may choose the base pattern for a run,
  and the run's start names the pattern used (owner; FR-010).

## User Scenarios & Testing *(mandatory)*

The user is a developer who has built a slice with the platform: the run ended done, so its
tests and code passed every check and its review. Today every new slice runs the tests, code and
review agents again, even one that differs from an earlier slice only in its names and limits
(notes instead of tasks, a 200-character limit instead of 100). Each time the model writes the
same solution again, at a cost, and may write it differently or wrongly.

### User Story 1 - Capture a done run as a proven pattern (Priority: P1)

The developer chooses a run that ended done and asks the platform to capture it. The platform
proposes a captured pattern: the run's slice, tests and code, with the parts that may vary named
as parameters. Before the developer can approve it, the platform proves it: rebuilt from the
captured pattern with the source slice's own values, with no model call, the result passes every
check the source run passed and equals the accepted files.

**Why this priority**: nothing can be reused until a proven solution is captured; this is the
step that moves work from the probabilistic side to the deterministic side.

**Independent Test**: build the evals' notes slice with `api-feature-go` until done; capture the
run; the proposal names its parameters, the proof passes with no model call, and the developer
approves it into the catalogue.

**Acceptance Scenarios**:

1. **Given** a run that ended done, **When** the developer asks to capture it, **Then** the
   platform proposes a captured pattern that names the source run, its slice, the base pattern
   and method that accepted it, and each proposed parameter with the source value and every place
   it occurs.
2. **Given** a proposal, **When** the platform proves it, **Then** the rebuild makes no model
   call, every check passes, and every file equals the accepted file; the proof's result is
   recorded with the proposal.
3. **Given** a proposal whose proof fails, **When** the developer looks at it, **Then** it names
   each failing check or differing file, and it cannot be approved.
4. **Given** a proven proposal, **When** the developer approves it, **Then** it enters the
   catalogue with its own identity and version, and the approval is recorded with who approved
   it and when.
5. **Given** a run that ended handed over, expired or cancelled, or one still in progress,
   **When** the developer asks to capture it, **Then** the platform refuses, naming the run's
   state.

---

### User Story 2 - A later slice of the same kind is built with no model (Priority: P2)

A later slice that equals a captured pattern's source slice except for its parameters' values
fits the captured pattern. Its run fills the new values into the template and runs only the
checks. No agent step runs, and the run ends done or handed over by the checks alone.

**Why this priority**: this is the reuse the capture exists for (N-052); it depends on Story 1.

**Independent Test**: capture the notes slice with its resource name and its text limit as
parameters; approve a slice for tasks with a 200-character limit that otherwise equals it; the run
ends done with zero model tokens, and its tests (derived from the new slice's examples) pass
against the delivered service.

**Acceptance Scenarios**:

1. **Given** an approved captured pattern, **When** a slice is approved that equals its source
   slice once each parameter's value is replaced, **Then** the slice fits the captured pattern,
   and the fit names the value found for each parameter.
2. **Given** a slice that differs from the source slice anywhere outside a parameter (another
   rule, example, member or operation), **When** its fit is judged, **Then** it does not fit the
   captured pattern, and the reason names the first place it differs.
3. **Given** a slice that fits, **When** its run starts, **Then** the run makes no model call,
   runs every check the captured pattern declares, and ends done only when all pass.
4. **Given** a slice whose new values make a check fail (a value that is not a valid name, or a
   limit its examples contradict), **When** the run's checks run, **Then** the run is handed over
   with the failing check named, and no agent step is started to repair it.
5. **Given** a slice that fits both a captured pattern and its base pattern, **When** its run
   starts, **Then** the captured pattern is used unless the developer chose the base pattern for
   that run, and the run record names the pattern used.

---

### User Story 3 - A captured pattern stays true to its method (Priority: P3)

A captured pattern is used only under the method that proved it. When the platform's method
changes (its checks, procedures, template or pinned runtime), a captured pattern is not used until
it is proven again under the new method; when the developer withdraws it, no new run uses it.

**Why this priority**: a captured solution proven under old checks would be accepted without being
judged by the current ones, which breaks N-050 and N-051; it depends on Stories 1 and 2.

**Independent Test**: change the method identity after a capture; a fitting slice's run does not
use the captured pattern and says it needs a new proof; proving it again restores its use.

**Acceptance Scenarios**:

1. **Given** a captured pattern proven under an earlier method, **When** a fitting slice's run
   starts, **Then** the captured pattern is not used, and the developer is told it needs a new
   proof.
2. **Given** such a pattern, **When** the developer asks to prove it again, **Then** the proof
   runs under the current method and, when it passes, the pattern is used again with the new
   method recorded; when it fails, the pattern stays unusable and names what failed.
3. **Given** an approved captured pattern, **When** the developer withdraws it, **Then** no new
   run uses it, and earlier runs' records still name it.
4. **Given** a run built from a captured pattern, **When** its record is read, **Then** it names
   the captured pattern's identity and version, its source run, the method, and each parameter's
   value.

### Edge Cases

- A parameter's source value also occurs where it is not meant to be replaced (the word "note" in
  a description and in a name): the proposal shows every place it occurs, and the proof judges
  the rebuild with that value replaced everywhere it was named.
- Two parameters with the same source value in any case form: the proposal is refused, naming
  both, because a fit could not tell them apart.
- One source value inside another (`note` and its plural `notes`, a separate parameter): the
  longer value is matched first, and no place is replaced twice.
- A case form the code writes another way (`NoteID`, `note_id`): the forms named in FR-003 are
  replaced where they occur inside a word as well; any other spelling is not, and the proof fails
  on the file it leaves unequal or the check it breaks.
- A later slice gives two parameters the same value, or a value that breaks a name (a space, a
  reserved word): the run's checks judge the result; a failed check hands the run over (Story 2,
  scenario 4).
- A captured pattern with no parameter: only a slice identical to the source fits it, which the
  proposal states.
- The source run's slice is later erased: the captured pattern keeps its own copy of what it
  needs and remains usable; erasing the captured pattern is a separate, confirmed step.
- The base pattern gets a new version: the captured pattern stays as captured, and the method
  rule (Story 3) decides whether it needs a new proof.
- The proof is interrupted by a platform restart: it continues or is started again, and a
  proposal is never marked proven without a complete, recorded proof.
- A run already in progress when its captured pattern is withdrawn finishes on it.

## Requirements *(mandatory)*

### Functional Requirements

#### Capture

- **FR-001**: The developer MUST be able to ask the platform to capture a run that ended done; the
  platform MUST refuse a run in any other state, naming that state.
- **FR-002**: A captured pattern proposal MUST hold the source slice, its accepted tests and code,
  and the names of the source run, the base pattern and the method that accepted it.
- **FR-003**: The proposal MUST name each parameter: its name, its source value, and every place
  in the slice, tests and code where the value occurs. Parameters MAY be only the slice's names
  (resource, operation and member names) and its declared values. The platform MUST propose the
  candidates; before the proof the developer MUST be able to remove any parameter and add another
  of the slice's names or declared values. Each name parameter covers its value's three case forms:
  all lower case, first letter upper case, all upper case; a plural or other form is a separate
  parameter. Two parameters whose source values are equal in any case form MUST be refused. Where
  one source value contains another, the longer MUST be matched first, and no place is replaced
  twice.

#### Proof and approval

- **FR-004**: The platform MUST prove a proposal by building the source slice from it with no model
  call, running every check the source run passed. The proof MUST pass only when every check
  passes and every built file equals the accepted file.
- **FR-005**: A proof's result MUST be recorded with the proposal and the method it ran under,
  naming each failing check or differing file when it fails.
- **FR-006**: Only a proposal whose latest proof passed under the current method MAY be approved.
  Approval MUST be the developer's explicit act, recorded with who approved and when; until then
  no run uses the proposal.
- **FR-007**: An approved captured pattern MUST enter the catalogue with its own identity and
  version, and MUST NOT change afterwards; a change is a new version that is proven and approved
  again.

#### Fit and build

- **FR-008**: A slice MUST fit a captured pattern only when replacing each parameter's source
  value with one value per parameter turns the source slice into the approved slice exactly. The
  fit MUST name each parameter's value, and a slice that does not fit MUST be told the first place
  it differs.
- **FR-009**: A run from a captured pattern MUST fill the parameters' values into the template,
  each name in the case form its place has (FR-003),
  MUST make no model call, and MUST run every check its captured pattern declares; it MUST end
  done only when all pass, and handed over, naming the failing check, otherwise.
- **FR-010**: When a slice fits a captured pattern and another pattern, the run MUST use the
  captured pattern unless the developer chose another pattern for that run; when several captured
  patterns fit, the one approved most recently MUST be used. The answer to the run's start and the
  run record MUST name the pattern used and why.

#### Method and record

- **FR-011**: A captured pattern MUST be used only under the method its latest passing proof ran
  under. Under any other method a run MUST NOT use it and MUST tell the developer it needs a new
  proof; the developer MUST be able to start that proof.
- **FR-012**: The developer MUST be able to withdraw a captured pattern; no run started after that
  uses it, and records of earlier runs keep naming it.
- **FR-013**: A run built from a captured pattern MUST record the captured pattern's identity and
  version, its source run, the method, and each parameter's value.
- **FR-014**: Capture, proof, approval and withdrawal MUST each be operations the developer starts
  and follows to an outcome, never a blocking call.

### Key Entities

- **Captured pattern**: a pattern made from a done run: the source slice, its accepted tests and
  code as a template, its parameters, the checks it runs, and its source run, base pattern,
  identity and version. Proposed, then proven, then approved; later withdrawn or superseded.
- **Parameter**: a named part of a captured pattern's source slice that a later slice may give
  another value: a name or a declared value, with its source value and the places it occurs.
- **Proof**: one build of a captured pattern's source slice from the captured pattern with no
  model call, its check results, its file comparison and the method it ran under.
- **Approval**: the developer's recorded consent that a proven captured pattern may be used.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A run from a captured pattern uses 0 model tokens and costs 0 in model spend.
- **SC-002**: A run from a captured pattern ends in under a quarter of its source run's time.
- **SC-003**: Every approved captured pattern has a passing proof under the method it is used
  under: 0 runs use a captured pattern without one, including after a method change.
- **SC-004**: For a captured notes slice, a slice for another resource with another limit is built
  done and its derived tests pass, in 3 of 3 runs, with the same files each time.
- **SC-005**: A slice that differs from the source outside a parameter fits the captured pattern
  in 0 cases of a planted set (another rule, example, member, operation or status).

## Assumptions

- The slice is still proposed and approved as in features 001 and 007 (the analyst may help write
  it); "no model" covers the build, from the approved slice to the run's outcome.
- A run from a captured pattern has no review step: its code is the reviewed source code with new
  values, and its checks (including the tests derived from the new slice's examples) decide.
- Parameters are the slice's names and declared values only (FR-003); turning behaviour (rules or examples that differ in kind) into parameters is out of scope.
- Exact equality in fit and proof means character for character, as feature 001's reuse does.
- A captured pattern belongs to the developer's catalogue on their installation, like their
  slices; sharing captured patterns between installations or tenants is out of scope.
- Captured patterns are kept by the platform, not added to the repository's `src/catalogue/`; the
  repository's patterns remain the base patterns.
- One base pattern exists (`api-feature-go`); a captured pattern's checks are those of its base
  pattern's steps that judge the delivered project.
