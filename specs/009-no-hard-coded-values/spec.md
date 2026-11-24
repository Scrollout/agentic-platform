# Feature Specification: Code that hard-codes a declared value is rejected

**Feature Branch**: `009-no-hard-coded-values`

**Created**: 2026-11-23

**Status**: Draft

**Input**: Need N-059, "Values that belong in data or configuration cannot be hard-coded" (owner
chose it as the next work, 2026-11-23). Description: when an agent writes code for a slice, the
platform rejects the step if the code hard-codes a value that belongs in the slice's data or in
configuration: a value the contract or the rules already declare (limits, allowed values, error
codes, messages), repeated as a literal instead of taken from its one declared home, and
deployment values (addresses, ports, credentials) written into code instead of read from
configuration. The developer sees which value, where, and where it belongs. The check is
deterministic, fixed before the step runs, and the agent cannot change it.

**Serves needs** ([problem space](../../docs/overview/problem-space.md)): N-059 (values that belong
in data or configuration cannot be hard-coded); N-051 (a result is accepted only when it passes
checks that do not depend on a model's judgement) and N-056 (the author of a change cannot alter
the checks that judge it), which the new check must meet.
**Uses terms** ([glossary](../../docs/overview/glossary.md)): Slice, Pattern, Catalogue, Run,
Step, Check, Hand-over, Agent, Method, Accepted result, Eval, Declared value, Deployment value,
Hard-coded value.
**Builds on**: [feature 001](../001-prose-to-accepted-slice/spec.md) (the slice, the catalogue's
one pattern and its steps, and how a step's checks accept or reject it; its planted failures,
SC-003), [feature 006](../006-method-record/spec.md) (an accepted result is reused only under the
method that judged it), and Constitution Principle XX ("hard-code what belongs in data" is one of
the problems the [platform overview](../../docs/overview/platform.md) names).

## Clarifications

### Session 2026-11-23

- Q: How is a literal that equals a declared value by chance told apart from a copy of it? → A:
  every literal equal to a declared value is rejected, except `0`, `1`, `-1` and the empty text,
  stated as a limit of the check; a declared value that is one of these is therefore not caught
  (owner; FR-003).
- Q: Do values stated only in the rules or their examples count as declared values? → A: No; only
  values the contract declares. Rule-stated values are behaviour the derived tests already pin; to
  have a value checked, the developer declares it in the contract (owner; FR-001).
- Found while planning: the code step's entry point writes its listen address in the code today.
  Q: May the code step's own code read environment variables itself? → A: No; configuration
  enters only through the configuration the platform declares and documents in the delivered
  project, with its defaults, so every value the service reads is documented (owner; FR-005).
- Analyze round 1 (fresh Sonnet, 60f0e6a): a deployment-value failure names the kind of value,
  never a credential's text (FR-004, SC-002; Principle XI); the deployment check's limits are
  stated (FR-002). Q: The declared-value check rejects unrelated uses of a common number (a
  `ParseInt(s, 10, 64)` when the contract declares a default of 10): what then? → A: a stated
  limit; the reason says how to write such a literal another way, and the code agent is told to
  prefer idioms without bare numbers (owner; FR-003). Q: Keep a live eval case on a slice with
  declared values? → A: No; the declared values are proven by the check's tests and the planted
  scenario, and a live case is a backlog task (owner; FR-010).
- Found while building US2 (T009 test author) and analyze round 5: FR-007's "against the approved
  contract" concerns the checks that read the contract. Q: Does the deployment-value check read the
  approved contract? → A: No; DeploymentValuesConfigured judges only the step's own files, and the
  two declared-value checks judge against the approved contract (the check contract's runCheck and
  the catalogue README, 0a476a4; FR-007).

## User Scenarios & Testing *(mandatory)*

The user is a developer who builds a slice with the platform. Today the code step is accepted when
the derived tests pass, its scope is kept and its dependencies are allowed. Code that repeats a
limit from the contract as a bare number, or listens on a port written into the code, passes every
one of those checks: the tests cannot tell a value taken from its declared home from the same value
typed again. When the contract changes later, the copy silently disagrees with it, and a deployment
cannot move the service without changing its code.

### User Story 1 - A value the slice declares is not repeated in code (Priority: P1)

A value that the approved contract declares (a length or size limit, a range bound, an allowed
value, a default) has one home: the contract. When the code step's work repeats such a value as a
literal instead of taking it from what the platform generates from the contract, the step is
rejected, and the developer sees which value, in which file and line, and where it is declared.

**Why this priority**: this is the case N-059 names first, and the one that breaks later without
any test noticing: a contract change regenerates the declared value, and the copy in the code stays
as it was.

**Independent Test**: from an approved slice whose contract declares a text limit of 200, run the
code step's checks on code that compares the text's length with the literal `200`: the step is
rejected, naming the file, the line, the value and the contract location that declares it. The same
code taking the limit from the generated code is accepted.

**Acceptance Scenarios**:

1. **Given** a contract that declares a limit, a bound, an allowed value or a default, **When** the
   code step's work repeats that value as a literal, **Then** the step is rejected, and the check
   result names the file, the line, the value and the contract location that declares it.
2. **Given** the same contract, **When** the code step's work takes the value from the code the
   platform generated from the contract, **Then** this check passes.
3. **Given** a rejected step with attempts left, **When** the next attempt runs, **Then** the agent
   is given the check result like any other failed check, and the run is handed over when the
   attempts are spent (feature 001, FR-016).
4. **Given** a contract that declares no such value, **When** the code step's checks run, **Then**
   this check passes and reports what it examined.

---

### User Story 2 - Deployment values come from configuration (Priority: P2)

A value that changes with where the service runs (the address or port it listens on, the address of
anything it calls, a credential) is read from configuration when the service starts, never written
into the code. When the code step's work writes such a value into the code, the step is rejected
with the file, the line and the kind of value.

**Why this priority**: a delivered service that cannot be moved or given a credential without
changing its code is not one the developer can deploy; it matters once Story 1's declared values
are kept.

**Independent Test**: run the code step's checks on code whose entry point listens on `:8080`
written in the code: the step is rejected, naming the file, the line and that a listen address
belongs in configuration. The same code taking the address from the configuration the platform
declares (with its documented default) is accepted.

**Acceptance Scenarios**:

1. **Given** code that writes a listen address, a port, a network address or URL of another
   service, or a credential into the code, **When** the code step's checks run, **Then** the step is
   rejected, naming the file, the line and the kind of value.
2. **Given** code that takes each such value from the configuration the platform declares,
   **When** the checks run, **Then** this check passes; code that reads the environment itself
   is rejected.
3. **Given** a delivered project, **When** the developer reads its documentation, **Then** it names
   every configuration value the service reads, with its default where it has one.

---

### User Story 3 - The check cannot be bypassed (Priority: P3)

The check is part of the pattern, fixed before the step runs. The agent whose work it judges cannot
change, skip or weaken it, and a planted hard-coded value is stopped every time.

**Why this priority**: without it the check is advice (N-056); it depends on Stories 1 and 2
existing.

**Independent Test**: plant each kind of hard-coded value in the code step's work, and try to
change the check's inputs from the step: every plant is stopped, in 3 out of 3 trials, and the
check's inputs are unchanged.

**Acceptance Scenarios**:

1. **Given** a code step whose work changes the contract, the generated code or anything the check
   reads, **When** the step is checked, **Then** the change is rejected by the existing checks and
   the declared-value checks still judge the step against the approved contract.
2. **Given** each planted hard-coded value of Stories 1 and 2, **When** the build runs, **Then** the
   step is rejected in 3 out of 3 trials.
3. **Given** a run that was accepted before this check existed, **When** it is replayed, **Then**
   its accepted results are not reused under the changed pattern (feature 006), and the step is
   judged again.

### Edge Cases

- A literal equals a declared value by chance, without standing for it (for example a loop that
  starts at `1` in a slice whose contract declares a minimum length of 1): `0`, `1`, `-1` and the
  empty text are never judged as copies, so a declared value that is one of them is not caught
  (FR-003).
- The same value is declared in two places in the contract (two fields with a limit of 200): the
  check result names every location that declares it.
- A value the rules state but the contract does not (for example "the first note gets id 1"): it is
  not a declared value and is not judged; the derived tests pin it. To have it checked, the
  developer declares it in the contract.
- A declared value written in another form (`200` as `2e2`, `0xC8`, or a computed `100 * 2`): every
  form of one literal is recognised; a value computed from several literals is not.
- A literal equal to a declared value that does not stand for it (`ParseInt(s, 10, 64)` with a
  declared default of 10): it is rejected, and the reason says to write it another way (FR-003).
- A port written as a number, or a credential under an unknown name: not recognised (FR-002).
- A literal in a comment or in a test file: only the code step's own writable code is judged; tests,
  generated code and comments are not.
- Code that cannot be parsed: the check fails closed, as a check that cannot run (feature 001,
  FR-012).
- A configuration value with a default (the listen address, defaulting to `:8080`): the default
  lives with the declared configuration the platform writes, never in the code step's code; the
  code step's code reading the environment itself is rejected (FR-005).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The code step MUST be rejected when its writable code holds a literal equal to a value
  the approved contract declares as a limit, a bound, an allowed value or a default, except where
  FR-003 states a limit.
- **FR-002**: The code step MUST be rejected when its writable code holds a listen address, a port,
  a network address or URL, or a credential, or reads the environment (FR-005). Stated limits: a
  port written as a number, a credential under a name or in a call the check does not know, and a
  value built at run time are not recognised; the forms the check recognises are listed in the
  catalogue's README.
- **FR-003**: The literals `0`, `1`, `-1` and the empty text MUST NOT be judged as copies of a
  declared value; this is a stated limit of the check: a declared value that is one of them is not
  caught. Values stated only in the rules or their examples are not declared values. Any other
  literal equal to a declared value is rejected even where it does not stand for it (a stated
  limit); the reason then says to write it another way.
- **FR-004**: Each failure MUST name the file, the line, and where the value belongs: for a declared
  value, the value and the contract location that declares it; for a deployment value, its kind
  and the configuration it belongs in. A deployment value's text (which may hold a credential)
  MUST NOT appear in a reason.
- **FR-005**: The delivered service MUST read every deployment value from configuration when it
  starts, and its project MUST document each one with its default, if any. Configuration MUST
  enter only through the configuration the platform declares in the delivered project; the code
  step's own code MUST NOT read the environment.
- **FR-006**: The platform MUST make every value FR-001 covers reachable from the generated code, so
  that code can take it from its declared home.
- **FR-007**: The check MUST be declared in the pattern before a run starts, MUST NOT call a model,
  and, where it reads the contract, MUST judge the step against the approved contract, not a copy
  the step can change (the deployment-value check does not read it).
- **FR-008**: A check that cannot read the code or the contract MUST fail (feature 001, FR-012),
  and every run of the check MUST report what it examined.
- **FR-009**: A pattern changed by this feature MUST be a new pattern version, so that accepted
  results judged without the check are not reused (feature 006).
- **FR-010**: The code agent's evals MUST pass with the new checks and gain an adversarial case
  that tells the agent to hard-code a deployment value; the planted failures MUST include one
  hard-coded declared value and one hard-coded deployment value (Constitution Principle XIX).

### Key Entities

- **Declared value**: a value the approved contract states (a limit, bound, allowed value or
  default), with its location in the contract; its one home.
- **Deployment value**: a value that changes with where the service runs: a listen address or
  port, the address of a service it calls, or a credential.
- **Hard-coded value**: a literal in the code step's writable code that equals a declared value, or
  is a deployment value not read through configuration.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Each kind of hard-coded value (a declared limit, an allowed value, a listen address,
  a service address, a credential, an environment read) is stopped by its check in every planted
  case of the checks' tests, and a planted declared limit and a planted listen address are
  stopped on the build path in 3 out of 3 trials.
- **SC-002**: 100% of the checks' failures that find a value name the file, the line and where the
  value belongs, with the value for a declared value and the kind for a deployment value; no
  reason holds a deployment value's text; a check that cannot read its input names what it could
  not read.
- **SC-003**: Code that takes every declared value from its home and every deployment value from
  configuration passes the check, on every fixture slice and in every passing eval case.
- **SC-004**: The code agent's eval suite passes at the same pass^3 as before the check is added.
- **SC-005**: A successful run still reaches accepted software in under 30 minutes (feature 001,
  SC-007).

## Assumptions

- The catalogue still holds one pattern, a Go HTTP API (feature 001, FR-022); the check is written
  for it. Other languages come with their patterns.
- Only the code step's writable code is judged. Tests legitimately repeat example values, and
  generated code is the declared home itself.
- Configuration means environment variables, read when the service starts, as the constitution
  requires for the platform's own deploy configuration ("Values have one home").
- The rule that a value has one home applies to the delivered software; the platform's own code is
  governed by the constitution and its existing checks, not by this feature.
- Adding a check to the pattern changes its method identity, so earlier runs' accepted code steps
  are judged again on replay (feature 006); this is intended.

## Dependencies

- The code agent's instructions are told about the check; they remain advice, and the check
  decides (Principle XX).
- A paid eval run of the code agent's suite is needed before merge (owner's go-ahead). Defects
  that run finds in another agent are fixed as defects against agreed intent, with the owner's
  decision (found 2026-11-24: the analyst and the tests agent, tasks T036 and T037).
