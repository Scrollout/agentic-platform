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
Step, Check, Agent, Method, Accepted result, Declared value, Captured pattern, Parameter, Proof, Proof slice.
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
- Found while planning (research E1): the examples hold values derived from a declared value (a
  text of 201 characters for a limit of 200), which are not names or declared values, so a slice
  with another limit would not fit (SC-004). Q: May other values become parameters? → A: yes, the
  developer may add any other literal value of the slice (a number or a string) as its own
  parameter (owner; FR-003).
- Analyze round 1 (fresh Sonnet, at 3b74d73). C1: research E1 was wrong: the evals' notes slice
  declares no limit (its `200` is a status), so Story 2's independent test and SC-004 use another
  resource only; another limit with its derived values is proven on a fixture that declares one
  (SC-006). Q: A parameter value outside its form (a space, a reserved word; which words: Session
  2026-11-27)? → A: the slice does
  not fit that captured pattern, and the run's start says why; nothing reaches code (owner;
  FR-008, FR-010). Q: What does the proof prove? → A: that the captured pattern rebuilds the
  source slice with no model call, every check it declares passes, and the filled and generated
  files equal the accepted ones; whether other values work is judged by each later run's checks
  (owner; FR-004). Q: SC-002's time target? → A: dropped; SC-001 states the gain (owner). Q: A
  change to an approved captured pattern? → A: none: it never changes; the developer captures the
  run again under a new name and withdraws the old one (owner; FR-007). I1: every occurrence of a
  case form is replaced wherever it sits, inside a word too (`note_id` becomes `task_id`), as the
  owner's case-form answer says (FR-003).
- Found at T001 (writing the contract): step workers are started, granted and registered per
  slice, and a proof has no slice (ADR 0095), so a proof's procedure steps had no workers and its
  project folder no name. Q: How does a proof get its project and step workers? → A: the
  developer names a new project folder for the proof, as for a build; the step workers are
  started and registered for the captured pattern, as they are for a slice, before the proof
  starts; a proof's run has no slice and is marked as a proof (owner; FR-004).
- Analyze round 3 (fresh Sonnet, at 9609f03): a run with no slice has no home in the engine
  (runs, events, findings, reuse, the one-active-run rule, resume, replay and take-over are all
  found by slice), breaks the contract (`Run.slice` is required) and the launcher, and after the
  slice's erasure no expected hashes remain. Q: Keep it or switch? → A: the proof makes its own
  slice: each captured pattern has one proof slice, an approved slice made from its copy of the
  source slice, and a proof is an ordinary run of it under the captured pattern; the captured
  pattern keeps the source run's output hashes (owner; FR-004, supersedes the answer above).
- Analyze round 4 (fresh Sonnet, at fb1c8ff), owner 2026-11-26 (the recommended answers): a proof
  slice is marked as one (its own description `Proof of <captured pattern>` and the captured
  pattern it proves), and describing the source again never returns it; resume and take-over keep
  a run's recorded pattern, a replay of a run built from a captured pattern follows the usable
  rule, and a replay of a proof run is a new proof (FR-006, FR-011, FR-013).
- Analyze round 5 (fresh Sonnet, at b870e6c). Q: A method change can change generated bytes, so
  comparing generated code would fail every later proof; what does a proof compare? → A: the
  filled outputs only (tests, code, witnesses); generated code is judged by its checks, as in
  every build (owner; FR-004). Q: Values occur in file paths (`cmd/notes/`); are paths filled? →
  A: yes, in the three case forms; two files that would get the same path are refused (owner;
  FR-003, FR-009).
- Analyze round 6 (fresh Sonnet, at 30456ec), decided under the owner's instruction to apply the
  fixes: a run built from a captured pattern, or a proof, is not captured (its template was never
  reviewed); an erased captured pattern's ID is never given again; a replay keeps the recorded
  parameter values (FR-001, FR-011).
- Analyze round 7 (fresh Sonnet, at 472576f), same instruction: a run whose slice changed after it
  started (a part's hash differs or is no longer approved) is not captured; who approved is the
  developer's client, the only caller that may approve; a limit's derived value is stated as a
  number in the examples (a text of 201 characters), since a literal over 64 characters cannot be
  a parameter (FR-001, FR-006, SC-006).

### Session 2026-11-26

- Q (US1 fresh review round 1, B1): a done run whose slice gives more than 32 candidates (the
  parameters limit) failed to be captured; what then? → A: the proposal keeps the first 32
  candidates in its order (the names in byte order, then the declared values in the contract's
  order) and leaves the others out; the developer may add a left-out one after removing another
  (owner; FR-003).
- Q (T039, review S6): SC-006's limit fixture has the limit 200 and the derived 201, which are
  also the HTTP statuses `200` and `201`, so a slice with another limit cannot fit; how is SC-006
  proven? → A: on a new fixture whose limit and derived value equal no other literal of the
  slice; the existing fixture and its tests stay; a value that also occurs as another literal (a
  status) is replaced there too, so it varies only together with it (owner; SC-006, Edge Cases).
- Q (T039, review S10): a proof slice changed by hand, then asked for again (`proof-slice`)? → A:
  its changed parts are restored from the captured pattern's copy and approved again, recorded in
  its history, so asking again always gives a slice that equals the copy (owner; FR-004, Edge
  Cases).
- Q (T039, round-2 suggestion): keep "a kept candidate then left with no place is left out" though
  Propose reaches it today only through the places limit? → A: keep it, as a rule that holds for
  every input (owner; FR-003).
- Q (T039, round-2 suggestion): names before declared values in the cut? → A: keep names first;
  the developer may add a left-out declared value after removing a name (owner; FR-003).
- Q (T017 review S5, T057): FR-009 says a run from a captured pattern runs every check, but a
  later build of the same slice and values reuses the results kept for it, so no check runs; is
  that allowed? → A: yes: a step whose result is reused (the same pattern, parameter values,
  inputs and method) runs no check again, as for any pattern; FR-009's checks apply to the steps
  that are filled (owner; FR-009).
- Q (T040, review S1 of US1): a filled step's write into the project is refused; is the run
  interrupted or handed over? → A: a refusal only the developer can fix hands the run over,
  naming the step and the refusal: the write outside the step's writable files, its files too
  large or not matching their hashes, an unreadable project folder, a version history changed
  under the run, an unknown project or an unauthorised call, a response too large, files read
  back that differ from those written, an unreadable template or a refused fill; a refusal a
  repeat can pass interrupts the run (resumable): the project not in the state the run left it,
  rate limited, busy, a time limit, a failed write, a lost connection (owner; FR-009).

### Session 2026-11-27

- Q (converge US2, T061): which words are reserved (US2 scenario 5)? → A: the 25 keywords of the
  Go specification: a later slice's name value equal to one does not fit, refused where the fit
  reads the value, naming the parameter and the word; the proposal, the check and changing the
  parameters refuse none, and a longer word (`types`) is not reserved; another case form (`Type`)
  is outside the name form, refused for its form as before (owner; FR-008, FR-010).
- Q (T062): a captured pattern whose source value is itself a Go keyword (the proposal offers
  it)? → A: a later value equal to that parameter's own source value is not refused as a keyword,
  since the source run passed every check with it; any other keyword value is (owner; FR-008).

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
check the captured pattern declares and its filled files equal the accepted ones.

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
2. **Given** a proposal, **When** the developer proves it into a new project folder, **Then** the rebuild makes no model
   call, every check the captured pattern declares passes, and every filled file equals the
   accepted one; the proof's result is
   recorded with the proposal.
3. **Given** a proposal whose proof fails, **When** the developer looks at it, **Then** it names
   each failing check or differing file, and it cannot be approved.
4. **Given** a proven proposal, **When** the developer approves it, **Then** it enters the
   catalogue with its own identity, and the approval is recorded with who approved
   it and when.
5. **Given** a run that ended handed over, expired or cancelled, or one still in progress,
   **When** the developer asks to capture it, **Then** the platform refuses, naming the run's
   state.

---

### User Story 2 - A later slice of the same kind is built with no model (Priority: P2)

A later slice that equals a captured pattern's source slice except for its parameters' values
fits the captured pattern. Its run fills the new values into the template and runs only the
checks of the steps it fills (a step whose result is reused runs none again). No agent step runs,
and the run ends done or handed over by the checks alone.

**Why this priority**: this is the reuse the capture exists for (N-052); it depends on Story 1.

**Independent Test**: capture the notes slice with its resource names as parameters; approve a
slice for tasks that otherwise equals it; the run ends done with zero model tokens, and its tests
(derived from the new slice's examples) pass against the delivered service. On a slice that
declares a text limit, capture the limit and the examples' value derived from it as parameters; a
slice with another limit and derived value fits and is filled with them.

**Acceptance Scenarios**:

1. **Given** an approved captured pattern, **When** a slice is approved that equals its source
   slice once each parameter's value is replaced, **Then** the slice fits the captured pattern,
   and the fit names the value found for each parameter.
2. **Given** a slice that differs from the source slice anywhere outside a parameter (another
   rule, example, member or operation), **When** its fit is judged, **Then** it does not fit the
   captured pattern, and the reason names the first place it differs.
3. **Given** a slice that fits, **When** its run starts, **Then** the run makes no model call,
   runs every check the captured pattern declares on each step it fills (a reused result runs
   none again), and ends done only when all pass.
4. **Given** a slice whose new values make a check fail (a limit its examples contradict), **When**
   the run's checks run, **Then** the run is handed over with the failing check named, and no
   agent step is started to repair it.
5. **Given** a slice that would fit except that a value is outside its parameter's form (a space
   in a name, a reserved word (a Go keyword, at the fit only; Clarifications 2026-11-27), a name
   where a number was), **When** its run starts, **Then** it
   does not fit that captured pattern, and the run names the captured pattern, the
   parameter and why; the run uses another pattern as for any slice that does not fit.
6. **Given** a slice that fits both a captured pattern and its base pattern, **When** its run
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
2. **Given** such a pattern, **When** the developer asks to prove it again into another new project folder, **Then** the proof
   runs under the current method and, when it passes, the pattern is used again with the new
   method recorded; when it fails, the pattern stays unusable and names what failed.
3. **Given** an approved captured pattern, **When** the developer withdraws it, **Then** no new
   run uses it, and earlier runs' records still name it.
4. **Given** a run built from a captured pattern, **When** its record is read, **Then** it names
   the captured pattern's identity and version, its source run, the method, and each parameter's
   value.

### Edge Cases

- A slice gives more than 32 candidates: the first 32 in the proposal's order are proposed and the
  others left out; the developer may add a left-out one after removing another (FR-003).
- A parameter's source value also occurs where it is not meant to be replaced (the word "note" in
  a description and in a name): the proposal shows every place it occurs, and the proof judges
  the rebuild with that value replaced everywhere it was named.
- Two parameters with the same source value in any case form: the proposal is refused, naming
  both, because a fit could not tell them apart.
- One source value inside another (`note` and its plural `notes`, a separate parameter): the
  longer value is matched first, and no place is replaced twice.
- A case form inside a longer word (`NoteID`, `note_id`, `denote`): it is replaced there too, so
  `note_id` becomes `task_id` and `denote` becomes `detask`; the proposal lists every place, so the
  developer sees it, and a slice whose text has the word unchanged does not fit.
- A spelling that is none of the three case forms (`nOTE`): it is not replaced; a later slice's
  build is judged by its checks.
- A later slice gives two parameters the same value: the fit refuses it, naming both parameters,
  because filling them would not be reversible.
- A later slice gives a value outside its parameter's form, or a name value that is a Go keyword
  other than its parameter's source value: it does not fit (Story 2, scenario 5).
- A captured pattern with no parameter: only a slice identical to the source fits it, which the
  proposal states.
- The source run's slice is later erased: the captured pattern keeps its own copy of what it
  needs, remains usable, and a new proof builds from that copy; erasing the captured pattern is a
  separate, confirmed step.
- The base pattern gets a new version: the captured pattern stays as captured, and the method
  rule (Story 3) decides whether it needs a new proof; a run built under the older base version
  is no longer captured (refused, naming both versions).
- The proof is interrupted by a platform restart: it continues or is started again, and a
  proposal is never marked proven without a complete, recorded proof.
- A run already in progress when its captured pattern is withdrawn finishes on it; erasing a
  captured pattern is refused while a run built from it, or its proof, has not ended.
- The proof slice is erased by the developer: the proofs already recorded stay with the captured
  pattern, and the next proof makes a new proof slice.
- A part of the proof slice is changed: it no longer equals the captured copy, so its build is
  refused as not fitting; asking for the proof slice again restores that part from the copy and
  approves it again, recorded in the slice's history.
- A parameter's source value is also another literal of the slice (a limit of 200 and the status
  `200`): it is replaced there too, so a later slice fits only when that literal has the new value
  as well; such a limit cannot vary alone.
- The captured pattern is withdrawn: its proof slice's builds are refused, naming the withdrawal.
- A run built from a captured pattern is resumed, taken over or replayed: resume and take-over keep
  its pattern; a replay uses the same captured pattern only while it is usable, and is refused
  otherwise, naming why; a replay of a proof run is a new proof; a replay of a run whose captured
  pattern was erased is refused as not found, naming it.
- The developer changes the parameters while a proof runs: refused until the proof's run ends, so
  a passing proof always proved the parameters it is recorded with.

## Requirements *(mandatory)*

### Functional Requirements

#### Capture

- **FR-001**: The developer MUST be able to ask the platform to capture a run that ended done; the
  platform MUST refuse a run in any other state, naming that state, and a run built from a
  captured pattern or a proof, naming why (its template was never reviewed), and a run whose
  slice changed after it started (a part's hash differs from the one its start recorded, or a part
  is no longer approved), naming the part, and a run built under another base pattern version
  than the current one, naming both; a run whose start recorded no part hashes is refused
  the same way; a replay carries the part hashes of the run it replays.
- **FR-002**: A captured pattern proposal MUST hold the source slice, its accepted tests and code,
  and the names of the source run, the base pattern and the engine's method at capture (the
  source run's own events keep the method that accepted each step; owner 2026-11-26, T046).
- **FR-003**: The proposal MUST name each parameter: its name, its source value, and every place
  in the slice, tests and code where the value occurs, in a file's content or its path. Parameters MAY be the slice's names
  (resource, operation and member names), its declared values and its other literal values (a
  number or a string). The platform MUST propose the names and declared values as candidates;
  when there are more than the parameters limit (32), it MUST propose the first 32 in that order
  (the names in byte order, then the declared values in the contract's order) and leave the others
  out, so a capture never fails for having too many (the cut comes after the form and case-form
  checks and before the places are found, so a left-out candidate takes no place from a kept one; a
  kept candidate then left with no place is left out too);
  before the proof the developer MUST be able to remove any parameter and add another of the
  slice's names, declared values or literal values. A number is matched only as a whole number,
  never inside a longer one. Each name parameter covers its value's three case forms:
  all lower case, first letter upper case, all upper case, each replaced wherever it occurs,
  inside a longer word too; a plural or other form is a separate parameter. Two parameters whose source values are equal in any case form MUST be refused. Where
  one source value contains another, the longer MUST be matched first, and no place is replaced
  twice. File paths are filled like contents; a slice whose values would give two files the same
  path, or a path outside its step's writable entries, MUST NOT fit.

#### Proof and approval

- **FR-004**: The platform MUST prove a proposal by building its proof slice (an approved slice
  made from the captured pattern's copy of the source slice) from it into a new project folder
  the developer names, with the source values and no model call, running every check the
  captured pattern declares. A proof is an ordinary run of that slice, marked as a proof; the
  proof slice is made again when it was erased, so a proof works after the source slice is
  erased, and a part of it changed since it was made is restored from the copy and approved again
  when the proof slice is asked for (a restore is refused while a proof run has not ended). The proof MUST
  pass only when every check passes and every filled file (tests, code, witnesses) equals the source run's accepted one; generated code is judged by its checks, as in
  every build. The proof shows that the captured pattern
  reproduces its source under the current method; whether other values work is judged by each
  later run's checks.
- **FR-005**: A proof's result MUST be recorded with the proposal and the method it ran under,
  naming each failing check or differing file when it fails; a proof whose run is handed over
  fails at once, naming why.
- **FR-006**: Only a proposal whose latest proof passed under the current method MAY be approved.
  Approval MUST be the developer's explicit act, recorded with who approved (the developer's
  client, the only caller that may approve) and when; until then
  no run other than its proofs uses the proposal.
- **FR-007**: An approved captured pattern MUST enter the catalogue with its own identity and MUST
  NOT change afterwards; to change it, the developer captures the run again under another name and
  withdraws the old one.

#### Fit and build

- **FR-008**: A slice MUST fit a captured pattern only when replacing each parameter's source
  value with one value per parameter turns the source slice into the approved slice exactly, and
  every value is of its parameter's form, is not a Go keyword (a name other than its source
  value; Clarifications 2026-11-27), and differs from every other parameter's value. The fit
  MUST name each parameter's value, and a slice that does not fit MUST be told the first place it
  differs or the value refused and why: a place first (in the order goals, rules, contract), a
  value only when every part otherwise matches, then the filled paths.
- **FR-009**: A run from a captured pattern MUST fill the parameters' values into the template's
  contents and paths, each name in the case form its place has (FR-003),
  MUST make no model call, and MUST run every check its captured pattern declares on each step it
  fills (a step whose result is reused runs none again, Clarifications 2026-11-26); it MUST end
  done only when all pass, and handed over, naming the failing check, otherwise; a filled step
  whose write the developer must fix hands the run over naming the step and the refusal, and one
  a repeat can pass interrupts it (Clarifications 2026-11-26, T040).
- **FR-010**: When a slice fits a captured pattern and another pattern, the run MUST use the
  captured pattern unless the developer chose another pattern for that run; when several captured
  patterns fit, the one approved most recently MUST be used. The run and its record MUST name the
  pattern used and why, and each captured pattern that fit but needs a new proof or did not fit
  only because of a refused value, with the reason.

#### Method and record

- **FR-011**: A captured pattern MUST be used only while its latest proof passed under the current
  method. Otherwise (a method change, a later failing proof, a proof still running) a run MUST
  NOT use it and MUST tell the developer it needs a new
  proof; the developer MUST be able to start that proof. A resumed or taken-over run MUST keep
  its recorded pattern; a replay of a run built from a captured pattern MUST use it only while it
  is usable and is otherwise refused, naming why, with the parameter values the run recorded; a
  replay of a proof run MUST be a new proof.
- **FR-012**: The developer MUST be able to withdraw a captured pattern; no run started after that
  uses it, and records of earlier runs keep naming it.
- **FR-013**: A run built from a captured pattern MUST record the captured pattern's identity and
  version, its source run, the method, and each parameter's value. The source run is named by
  its ID, which keeps naming it after the source slice and its runs are erased.
- **FR-014**: Capture, proof, approval and withdrawal MUST each be operations the developer starts
  and follows to an outcome, never a blocking call.

### Key Entities

- **Captured pattern**: a pattern made from a done run: the source slice, its accepted tests and
  code as a template, its parameters, the checks it runs, and its source run, base pattern,
  identity and version. Proposed, then proven, then approved; later withdrawn or erased.
- **Parameter**: a named part of a captured pattern's source slice that a later slice may give
  another value: a name, a declared value or another literal value, with its source value and the
  places it occurs.
- **Proof**: one build of the captured pattern's copy of its source slice from the captured
  pattern with the source values and no model call, its check results, its file comparison and the
  method it ran under.
- **Proof slice**: the approved slice the platform makes from a captured pattern's copy of its
  source slice, described `Proof of <captured pattern>` and naming the captured pattern it proves;
  each of its runs is a proof, and describing the source again never returns it.
- **Approval**: the developer's recorded consent that a proven captured pattern may be used.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A run from a captured pattern uses 0 model tokens and costs 0 in model spend.
- **SC-003**: Every approved captured pattern has a passing proof under the method it is used
  under: 0 runs other than its proofs use a captured pattern without one, including after a
  method change.
- **SC-004**: For a captured notes slice, a slice for another resource is built done and its
  derived tests pass, in 3 of 3 runs, with the same files each time.
- **SC-005**: A slice that differs from the source outside a parameter fits the captured pattern
  in 0 cases of a planted set (another rule, example, member, operation or status).
- **SC-006**: For a captured slice that declares a text limit, with the limit and its derived value
  as parameters, a slice with another limit and derived value fits, and the filled files carry the
  new values in every place the old ones were (the examples state the derived value as a number,
  "a text of 201 characters"; a literal text over 64 characters cannot be a parameter); the
  limit and the derived value equal no other literal of the source slice, such as a status.

## Assumptions

- The slice is still proposed and approved as in features 001 and 007 (the analyst may help write
  it); "no model" covers the build, from the approved slice to the run's outcome.
- A run from a captured pattern has no review step: its code is the reviewed source code with new
  values, and its checks (including the tests derived from the new slice's examples) decide.
- Parameters are the slice's names, declared values and literal values (FR-003); turning behaviour (rules or examples that differ in kind) into parameters is out of scope.
- Exact equality in fit and proof means character for character, as feature 001's reuse does.
- A captured pattern belongs to the developer's catalogue on their installation, like their
  slices; sharing captured patterns between installations or tenants is out of scope.
- Captured patterns are kept by the platform, not added to the repository's `src/catalogue/`; the
  repository's patterns remain the base patterns.
- One base pattern exists (`api-feature-go`); a captured pattern's checks are those of its base
  pattern's steps that judge the delivered project.
