# Feature Specification: From a description to accepted working software, one slice at a time

**Feature Branch**: `001-prose-to-accepted-slice`

**Created**: 2026-11-11

**Status**: In implementation (Story 1 done 2026-11-12; revised 2026-11-12 after fresh review round 1)

**Input**: User description: "A developer describes what they want in their own words and receives working software for one use case. Every result is accepted by checks, and the developer is asked only about what checks cannot judge." (full description in the conversation that created this spec; summarised in the stories below)

**Serves needs** ([problem space](../../docs/overview/problem-space.md)): N-003, N-050, N-051,
N-052, N-053, N-055, N-056, N-057, N-058, N-060, N-061, N-063.
**Uses terms** ([glossary](../../docs/overview/glossary.md)): Agent, Guard, State worker, Slice,
Pattern, Catalogue, Run, Step, Check, Hand-over. A *check* is a guard; a *step* is filled by a
state worker; *hand-over* is what ADR 0026 calls escalation.

## Clarifications

### Session 2026-11-11

- Q: How will the developer work with the platform in this first version? → A: In a terminal (describe, answer questions, approve, see results), plus a read-only local web page to inspect runs.
- Q: Where does the software built for a slice go? → A: Into a new project the platform creates for the slice, which the developer then owns.
- Q: In which programming language does the platform build the slice's software in this version? → A: Go only.
- Q: Should the limits stand: at most 10 questions to an approved slice, 3 attempts per step and 3 review rounds, and under 30 minutes for a successful build? → A: Yes, all three as stated.

### Session 2026-11-12

- Q: After fresh review round 1, is growing the catalogue (proposing a new pattern) part of this feature? → A: No; it moves to a later feature. This feature declines a description that no pattern fits.
- Q: After the analyze review of 2026-11-12 found the contract adds a `cancelled` outcome, may the developer cancel a run, and how does it end? → A: Yes; a run whose operation the developer cancels ends as cancelled, a fourth outcome (constitution Principle X).

### Session 2026-11-13

- Q: After the analyze review of 2026-11-13, when is a description "the same" as one submitted before (edge cases)? → A: When its text is the same, character for character; the previously approved slice is then offered for reuse.

### Session 2026-11-14

- Q: How can a review finding be confirmed so that it blocks, when the code step has already passed every derived test? → A: A blocking finding carries a new failing witness test for its rule, written by the review in its own folder; the check service runs it, and it confirms the finding when it fails at the named file and line; the fix step must make it pass without changing it.
- Q: Once the fix step makes a witness test pass, does it stay in the delivered project? → A: Yes; witness tests stay in their own folder, named for their rule, and from the round that adds them every later step's checks treat them as unchangeable, like the derived tests.
- Precise form of the first answer (after `/speckit-analyze` W1, 2026-11-14): printed text is no evidence, so "fails at the named file and line" means the witness test fails, having run the finding's line, as the coverage profile of that test's own run shows.

### Session 2026-11-17

- Q: In a review round after the first, which confirmed findings may block? → A: Only a finding that repeats an open finding the round was given (the same rule in the same file), or whose line is among the lines the fix before the round changed; any other confirmed finding is recorded as a suggestion. The platform applies this, not the review's instructions (constitution, Review converges; Principle XX). A regression on a line no fix changed is therefore only a suggestion (owner, 2026-11-17, T135).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Describe a use case and approve its slice (Priority: P1)

A developer writes, in their own words, what they want one use case to do. The platform turns it
into a slice: the goals, the business rules with at least one example for each, the terms they
use, and the contract the slice needs. Every part carries an identifier that links it to the goal
it serves. Checks prove the form of every part. Where the meaning is in doubt, the developer
answers questions, each about one identified part. The developer can change any part, and then
approves the goals, the rules (with their examples and terms) and the contract.

**Why this priority**: Everything later is derived from an approved slice. If the slice is wrong,
nothing built from it can be right, and only the developer can judge whether it matches what they
meant.

**Independent Test**: Give the platform a description of a small API feature. The developer ends
with an approved slice whose parts all pass their checks and all link to goals, and a request to
build an unapproved slice is refused.

**Acceptance Scenarios**:

1. **Given** a description of a use case that the catalogue's pattern fits, **When** the developer
   submits it, **Then** they receive goals, terms, rules with examples and a contract, every part
   linked by identifier to a goal and every part passing its checks.
2. **Given** a proposed slice in which a rule has no example, a goal has no rule, or a term is not
   defined, **When** the checks run, **Then** the slice is rejected and the developer is told
   exactly which part fails and why.
3. **Given** a description whose meaning is ambiguous, **When** the slice is proposed, **Then** the
   developer receives questions, each naming the part it concerns, and the slice reflects the
   answers.
4. **Given** a slice the developer has not approved, **When** a build is requested, **Then** it is
   refused with the reason and nothing is built.
5. **Given** the developer changes an approved part, **When** the change is saved, **Then** the
   checks run again and the changed part needs approval again before anything is built.
6. **Given** a description that the catalogue's pattern does not fit, **When** the developer
   submits it, **Then** the platform says so, names why, and builds nothing.

---

### User Story 2 - Build accepted software from an approved slice (Priority: P2)

From an approved slice, the platform builds working software into a new project, using the
reviewed pattern in its catalogue. Tests are derived from the examples and must fail before any
code exists. Code is accepted only when those tests pass. No step can change anything outside its
own scope, including the tests that judge it, and an agent's claim that it is done never counts.
Every run ends as done or handed over to the developer with the reason, and every run is recorded
so it can be inspected, replayed and compared.

**Why this priority**: This is the platform's core promise, that agent work is accepted by checks,
not trusted. It depends on an approved slice (Story 1).

**Independent Test**: From an approved slice, run the build, then run it with each planted failure.
Each failure is stopped by a check, and a replay of the same run gives byte-identical results.

**Acceptance Scenarios**:

1. **Given** an approved slice, **When** the build runs, **Then** tests derived from its examples
   exist and fail before any code is written, and the run ends as done only when they pass.
2. **Given** a build in which the work tries to change the tests that judge it, **When** the step
   is checked, **Then** the change is impossible or rejected and the tests are unchanged.
3. **Given** a build in which the work changes something outside its step's scope, **When** the
   step is checked, **Then** the change is rejected.
4. **Given** a build in which the work depends on something the pattern does not allow, **When**
   the step is checked, **Then** the step is rejected with that reason.
5. **Given** a build in which an agent reports done without the work being complete, **When** the
   step is checked, **Then** the step is not accepted.
6. **Given** a review that raises a new finding every round, each confirmed by a new failing witness
   test and each on a line the fix before its round changed, **When** 3 rounds have run, **Then** the run is handed over to the developer with the open
   findings.
7. **Given** a step that keeps failing its checks, **When** 3 attempts have failed, **Then** the
   run is handed over to the developer with the reason.
8. **Given** a run that has ended, **When** the developer replays it, **Then** every step reuses
   its accepted result (a review round left open is reused like an accepted step; owner,
   2026-11-15) and the replay's artifacts are byte-identical to the original run's.
9. **Given** any two finished runs, **When** the developer inspects or compares them, in the
   terminal or on the read-only local page, **Then** they see every artifact, link, check result
   and decision in order, and the differences between the two runs.
10. **Given** a run interrupted before it ended, **When** the developer resumes it, **Then** it
    continues without repeating any accepted step.

### Edge Cases

- The description is empty, or describes more than one use case: the platform asks the developer
  to narrow it to one use case before proposing a slice.
- More than 10 questions would be needed: the platform stops asking and asks the developer to
  narrow the description.
- The developer's answers contradict an earlier answer or rule: the contradiction is shown and must
  be resolved before approval.
- The developer declines to approve: nothing is built, and the proposed slice is kept for revision.
- The developer points the platform at an existing project: it declines, because this version
  builds each slice into a new project only.
- A build is requested while another run is active: it is refused with the reason (FR-021).
- A run waits for the developer after a hand-over for more than 14 days: it is closed as expired,
  and the record keeps the reason.
- A run reaches its budget of model tokens, cost or time: the current step stops and the run is
  handed over with that reason.
- The model is unavailable mid-run: the step fails as not accepted, is retried within its
  attempts, and the run can be resumed later (FR-020).
- The same description is submitted again: the previously approved slice is offered for reuse
  instead of being proposed again. "The same" means the same text, character for character.

## Requirements *(mandatory)*

### Functional Requirements

#### Slice from a description

- **FR-001**: The platform MUST accept a developer's description in plain prose as the only input
  needed to start a slice.
- **FR-002**: The platform MUST turn a description into one slice made of goals, business rules,
  at least one example per rule, the terms they use, and a contract.
- **FR-003**: Every goal, rule, example, term and contract operation MUST carry a stable identifier,
  and every rule, example and operation MUST link to the goal or rule it serves.
- **FR-004**: The platform MUST check that every rule follows the rule grammar, every rule has at
  least one example, every goal is covered by at least one rule, every term a rule uses is defined,
  and the contract passes its lint and has no breaking change against its previously approved
  version, if one exists.
- **FR-005**: The platform MUST reject a slice that fails any check (FR-004), naming each failing
  part and the reason.
- **FR-006**: Where meaning is in doubt, the platform MUST ask the developer questions, each naming
  the one part it concerns and offering at most 5 answer options or a free answer of at most 200
  characters, and MUST record each question and answer with the slice. It MUST ask at most 10
  questions in total for a slice.
- **FR-007**: The developer MUST be able to change any goal, rule, example, term or the contract.
  The developer MUST approve the goals, the rules (with their examples and terms) and the contract
  before anything is built, and a change to an approved part MUST withdraw its approval.

#### Build from a pattern

- **FR-008**: The platform MUST build only from the pattern in its catalogue, and MUST NOT run build
  steps or checks that the pattern does not declare.
- **FR-009**: When the catalogue's pattern does not fit a description, the platform MUST say so,
  naming why, and MUST NOT build. A pattern fits a slice when the slice's contract and rules meet
  every condition the pattern declares.
- **FR-010**: The catalogue's pattern MUST cite the sources it is based on and MUST pass the
  catalogue's checks; proposing new patterns is out of scope for this version.
- **FR-011**: Tests MUST be derived from the slice's examples, and MUST fail before any code for
  the slice exists.
- **FR-012**: A step MUST be accepted only when all of its checks pass; an agent's report that it
  is done MUST NOT count as a check, and a check that cannot run MUST count as failed.
- **FR-013**: A step MUST NOT be able to change anything outside its declared scope, including the
  tests and checks that judge it.
- **FR-014**: A step MUST be rejected if its result depends on anything its pattern does not allow.

#### Runs end and can be explained

- **FR-015**: Every run MUST end in exactly one outcome: done, handed over to the developer with
  the reason, expired after 14 days waiting for the developer, or cancelled by the developer.
- **FR-016**: A step that fails its checks MUST be tried at most 3 times (the fix between review
  rounds at most 3 times in each round), and a review MUST run at most 3 rounds, before the run is handed over to the developer. Every run MUST stop within its
  declared budget of model tokens, cost and time.
- **FR-017**: A review finding MUST block only when it carries evidence that a check confirms: a
  new witness test for its rule, written by the review in its own folder, that fails, having run
  the finding's line in a file the fix step may change; any other finding MUST be recorded as a suggestion. The fix step MUST
  make the witness test pass without changing it, and witness tests stay in the delivered project,
  unchangeable by every later step, like the derived tests. In a review round after the first, a
  confirmed finding MUST block only when it repeats an open finding the round was given (the same
  rule in the same file) or its line is among the lines the fix before the round changed; any other
  confirmed finding MUST be recorded as a suggestion.
- **FR-018**: Every artifact, link, question, answer, check result, attempt and decision MUST be
  recorded in order with the model, agent definition and pattern version that produced it, so a
  run can be inspected, replayed and compared with another run.
- **FR-019**: An accepted result for an input, and the result of a review round left with
  blocking findings open (owner, 2026-11-15), MUST be reused on later runs of the same input
  instead of being produced again.
- **FR-020**: An interrupted run MUST be resumable without repeating its accepted steps.

#### Scope of this first version

- **FR-021**: The platform MUST serve one developer on their own machine, with at most one active
  run at a time; a second build request MUST be refused while a run is active.
- **FR-022**: The catalogue MUST hold exactly one pattern: an API feature of one service, written
  in Go, covering contract check, generated code, tests, implementation and review. Software in any
  other language is out of scope for this version.
- **FR-023**: The developer MUST be able to do everything in this feature from a terminal:
  describe, answer questions, change parts, approve, build, resume, replay, compare and see each
  run's outcome.
- **FR-024**: The developer MUST be able to inspect and compare recorded runs in a read-only page on
  their own machine; the page MUST NOT change anything.
- **FR-025**: Each slice MUST be built into a new project in a folder the developer chooses; the
  platform MUST NOT change any other folder. When the run is done, the project is a plain folder
  with its own version history that the developer owns.

### Key Entities

- **Description**: the developer's prose about one use case; the only input to a slice.
- **Slice**: one use case taken from description to working software; holds goals, rules,
  examples, terms and a contract.
- **Goal**: an outcome the developer wants; every rule traces to a goal.
- **Business rule**: one statement of required behaviour, written in the rule grammar, linked to a
  goal.
- **Example**: a concrete case that illustrates one rule and becomes a test.
- **Term**: a word of the domain language with its definition, used by rules and examples.
- **Contract**: the interface the slice needs; every operation links to the rules it serves.
- **Question and answer**: a clarification about one part, recorded with the slice.
- **Approval**: the developer's recorded acceptance of the goals, the rules or the contract, bound
  to the exact content approved.
- **Pattern**: a reviewed way to build one kind of slice, with its conditions for fitting, its
  steps, each step's scope, allowed dependencies and checks, its limits and budgets, and its
  sources.
- **Catalogue**: the set of approved patterns.
- **Run**: one attempt to build a slice from a pattern, ending as done, handed over, expired or cancelled.
- **Step**: one part of a run, filled by an agent or a fixed procedure, with a scope and checks.
- **Check**: a deterministic test that decides whether a step or artifact is accepted.
- **Run record**: the ordered history of a run, from which it can be inspected, replayed and
  compared.
- **Accepted result**: a step's output that passed its checks, reused for the same input; a review round left with blocking findings open is kept and reused the same way, marked open (FR-019; data-model, An open round reused).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For a description that the pattern fits, the developer reaches an approved slice
  after at most 10 questions, each naming the part it concerns.
- **SC-002**: 100% of slices that break a structural rule (rule without example, goal without rule,
  undefined term, failing contract) are rejected, each with a reason that names the failing part.
- **SC-003**: Each of five planted failures is stopped by a check in 3 out of 3 trials: editing the
  judging tests, changing something outside scope, a forbidden dependency, claiming done falsely,
  and a non-converging review.
- **SC-004**: 100% of runs end in exactly one recorded outcome, and none continues past its limits
  of attempts, review rounds or budget.
- **SC-005**: Replaying a finished run gives byte-identical artifacts in 3 out of 3 trials.
- **SC-006**: For 100% of runs, every check result and decision names the goal, rule or example it
  concerns.
- **SC-007**: From an approved slice that the pattern fits, a run that succeeds reaches accepted
  working software in under 30 minutes without the developer's involvement.

## Assumptions

- The developer works alone on their own machine; no accounts, sharing or multiple organizations
  are in scope for this version.
- The developer can judge the meaning of goals, rules and contracts; the platform never treats its
  own checks as proof of intent.
- The first pattern's checks are those proven in the ADR 0026 spike, extended to the slice's rules
  and examples.
