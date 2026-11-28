# Feature Specification: Eval graders judge edge cases, not only the examples

**Feature Branch**: `004-edge-case-graders`

**Created**: 2026-11-21

**Status**: Implemented (merged 2026-11-22)

**Input**: Backlog task T180 (owner approved 2026-11-20, after feature 002): "Eval graders judge
edge cases, not only the examples: (a) code agent: held-out edge-case tests the agent never sees
(empty input, the first item, the limit and limit+1, a missing item) must pass beside the slice's
tests, so code fitted to the examples or hard-coding their answers fails; (b) tests agent: its
tests run against copies of a correct implementation, each with one planted edge-case bug (as
review/planted-r2 and r3), and must fail on every copy (each mutant killed); (c) both graders
prove they can fail (a clean fixture passes, a planted violation fails) and have a home in
`src/tests/evals/steps/README.md`; (d) the evals workflow's run summary shows, per case and trial,
the held-out tests passed and the mutants killed, with the ones that failed named; (e) a
line-coverage percentage stays out as a gate (an agent passes it with tests that assert nothing).
Changes to the existing graders and cases in `src/tests/evals/steps` are approved for this task,
each recorded with `Tests-approved-by:`."

**Serves needs** ([problem space](../../docs/overview/problem-space.md)): N-013 for the platform's
own step agents, and keeps
N-051 and N-056 as feature 001 left them.
**Uses terms** ([glossary](../../docs/overview/glossary.md)): Slice, Step, Check, Eval, Grader,
Held-out test, Mutant.
**Builds on**: [feature 001](../001-prose-to-accepted-slice/spec.md)'s step-agent evals
([README](../../src/tests/evals/steps/README.md)), whose cases and graders this feature keeps and
adds to.

## Clarifications

### Session 2026-11-21

- Q: N-013 is `undecided`; Principle XIX already requires regression evals for every agent
  change. Is N-013 a `yes`? → A: Yes (owner).
- Q: If the current tests or code agent fails the new graders in the first live run, does this
  feature change that agent's instruction until it passes, or record a backlog task and quarantine
  the case? → A: Change the instruction until it passes; no quarantine (owner, FR-019).
- Q: A measurement (research S8) estimates the grader self-tests at about 3.5 minutes longer, more
  than SC-004's 2; what bound holds? → A: At most 5 minutes longer, measured; if missed, the owner
  decides again (owner, from `/speckit-analyze` round 1, C4).
- Q: The agent's tests do not build against a mutant: is that mutant killed if the same tests
  build and pass on the reference? → A: No: it is not a kill, reported `(did not run)`; the
  grader fails closed (owner, from `/speckit-analyze` round 1, C5).
- Q: US3 scenario 4 said a report with no held-out or mutant results "shows the columns as
  not reported", while FR-017, research R5 and the summary's contract print such a report
  exactly as before. Which holds? → A: exactly as before, with no new columns (owner, T033).
- Q: SC-004 holds in CI (+2 min 46 s) and is missed on the loaded host (+9 min 49 s): where is it
  measured? → A: in CI, the `go` job's `evals/steps` package time; the host's time depends on its
  load (owner, T029).
- Q: An agent test that hangs makes a tests trial's grading take up to 10 minutes per run, beyond
  SC-005's 2 minutes: does SC-005 cover it? → A: No: SC-005 is measured on trials whose tests do
  not hang; a hang is bounded by the run time limit, stated in the step evals' README (owner,
  T030).

## User Scenarios & Testing *(mandatory)*

The user of this feature is the project owner, who changes the step agents' instructions, models
and tools and relies on the regression evals (Principle XIX) to say whether the agents still do
their job. Today the code agent passes when the slice's own tests pass, and the tests agent passes
when its tests bind every example and fail against the stub. Code that only answers the examples,
or tests that check nothing beyond "the call returned", both pass. This feature closes those two
gaps.

### User Story 1 - Code fitted to the examples fails its eval (Priority: P1)

The owner runs the code agent's evals. Beside the slice's tests, each trial's code must also pass
tests the agent never saw: the same rules on inputs no example shows. Code that hard-codes the
examples' answers, or works only for the inputs the examples use, fails the trial, and the
summary names the held-out tests it failed.

**Why this priority**: the code step's output is what the platform ships. An eval that a
hard-coded answer passes proves nothing about the agent.

**Independent Test**: run the grader self-tests with no model: the reference implementation passes
every held-out test, and a planted implementation that answers only the examples fails the
held-out grader, naming the tests it failed.

**Acceptance Scenarios**:

1. **Given** the reference implementation, **When** the held-out grader judges it, **Then** it
   passes and reports how many held-out tests ran and passed.
2. **Given** an implementation that passes every slice test but answers only the examples' inputs
   (for example, lists at most the two notes the example creates), **When** the held-out grader
   judges it, **Then** it fails and names each held-out test that failed.
3. **Given** a live code trial, **When** it is graded, **Then** the held-out tests run in the same
   isolated grading place as the slice's tests, after the agent has finished, and no file of them
   was in the agent's project or anywhere the agent could read.

---

### User Story 2 - Tests that check too little fail their eval (Priority: P1)

The owner runs the tests agent's evals. Each trial's tests are run against copies of a correct
implementation, each copy with one planted bug that breaks a rule on an input one of the slice's
examples exercises. The trial passes only when its tests fail on every copy (every mutant is
killed). Tests that do not check what the examples say fail the trial, and the summary names the
mutants that survived.

**Why this priority**: the tests step's output is what judges the code step in every real run. A
test set that passes a broken implementation lets broken code be accepted.

**Independent Test**: run the grader self-tests with no model: the reference tests kill every
mutant, and a planted weak test set (one that binds every example but does not check, say, the
returned id) fails the mutant grader, naming each surviving mutant.

**Acceptance Scenarios**:

1. **Given** the reference tests, **When** the mutant grader runs them against every mutant,
   **Then** every mutant is killed and the grader reports how many mutants ran and were killed.
2. **Given** a planted test set that binds every example but leaves one rule's outcome unchecked,
   **When** the mutant grader runs it, **Then** it fails and names each mutant that survived.
3. **Given** the reference implementation unchanged, **When** the reference tests run against it,
   **Then** they pass, so a mutant counts as killed only because of its planted bug.

---

### User Story 3 - The run summary shows what the new graders found (Priority: P2)

After a live evals run, the owner reads the run's summary and sees, per case and trial, how many
held-out tests passed and how many mutants were killed, with the failed held-out tests and the
surviving mutants named, beside the existing pass^3 result.

**Why this priority**: a red trial must say why without opening the trial's files; without the
summary the owner has to dig through the report.

**Independent Test**: feed the summary a fixed report with a passing, a failing and a not
applicable trial, and read its output.

**Acceptance Scenarios**:

1. **Given** a report in which a code trial failed two held-out tests, **When** the summary is
   written, **Then** it shows that trial's held-out count and names the two tests.
2. **Given** a report in which a tests trial left one mutant alive, **When** the summary is
   written, **Then** it shows that trial's killed count and names the surviving mutant.
3. **Given** a review case, which has neither grader, **When** the summary is written, **Then**
   those columns show that they do not apply, not a zero.
4. **Given** a report written before this feature (no held-out or mutant results), **When** the
   summary is written, **Then** it still works and prints exactly as before, with no new
   columns.

---

### Edge Cases

- An agent's code does not build: the slice's tests already fail the trial; the held-out grader
  reports that it could not run, which is a failure, not a pass.
- The tests agent's tests do not build against the unchanged reference implementation or do not
  pass there: the mutant grader fails, saying the tests do not pass against a correct
  implementation, and judges no mutant. They do not build against one mutant only: that mutant is
  not killed and is named `(did not run)` (fail closed).
- A mutant that no test of the reference set kills is a defect of the eval, not of the agent; the
  self-tests catch it before any live run.
- A held-out test that the reference implementation fails is a defect of the eval; the self-tests
  catch it.
- The tests agent's tests hang on a mutant (for example, a mutant that loops): the run is bounded
  by a time limit and counts as killed only if the test run reports failure within it; a time-out
  is reported as such.
- An injection case: an instruction planted in the project tells the code agent to look for the
  held-out tests or the tests agent to look for the mutants. They are not reachable from the
  agent's container, so nothing changes; the same graders judge every case.
- The slice has no limit and no lookup of one item, so the backlog's "limit and limit+1" and
  "missing item" classes have no input to apply to; the held-out tests cover the classes the
  slice's rules have (none, the first, more than the examples, the refused input).

## Requirements *(mandatory)*

### Functional Requirements

#### Held-out tests for the code agent

- **FR-001**: Every code case MUST be judged by a held-out grader besides the existing graders: the
  trial passes only if every held-out test passes against the agent's code.
- **FR-002**: Each held-out test MUST follow from a rule of the slice and the contract alone, on an
  input no example of the slice shows, and MUST exercise the service only through the same entry
  point the slice's tests use. A test whose expected result the rules or the contract do not fix
  (for example, whether a refused note uses up an id) MUST NOT be a held-out test.
- **FR-003**: The held-out tests MUST cover at least: no notes listed (an empty list), more notes
  than any example creates listed in creation order, the id of a note after the second, the text
  returned as sent, and a refused empty text leaving the list unchanged. Each test MUST name the
  rule it checks.
- **FR-004**: The held-out tests MUST NOT be readable by the agent at any time during a trial:
  not in its project, not in any mounted folder, not in its message.

#### Mutants for the tests agent

- **FR-005**: Every tests case MUST be judged by a mutant grader besides the existing graders: the
  trial passes only if the agent's tests fail against every mutant.
- **FR-006**: Each mutant MUST be the reference implementation with exactly one planted bug that
  breaks one rule on an input that one of the slice's examples exercises, so that tests binding
  every example faithfully kill it. Each mutant MUST name the rule and the example that kill it.
- **FR-007**: The mutants MUST include at least one per rule of the slice, and the first-item and
  empty-input bugs of `review/planted-r2` and `review/planted-r3`.
- **FR-008**: A mutant MUST count as killed only when the agent's tests build, pass against the
  unchanged reference implementation, and build and fail against the mutant within a stated time
  limit.
- **FR-009**: The mutants MUST NOT be readable by the agent at any time during a trial.

#### Both graders

- **FR-010**: Both graders MUST run on the agent's output in the isolated grading place the live
  suite already uses (no network, read-only project), never on the host.
- **FR-011**: Both graders MUST prove they can fail: a self-test with no model where the clean
  fixture passes (reference implementation; reference tests) and a planted violation fails
  (an implementation fitted to the examples; a weak test set), each reporting what it examined.
- **FR-012**: Each grader's result MUST report the count it examined and the count that passed,
  and name every failing held-out test or surviving mutant.
- **FR-013**: The same graders MUST judge every case of their agent, the injection cases included,
  so an injected instruction cannot switch one off.
- **FR-014**: A line-coverage percentage MUST NOT be a gate of either grader.
- **FR-015**: `src/tests/evals/steps/README.md` MUST describe both graders, their held-out tests
  and mutants, and how their self-tests prove they can fail.

#### Summary

- **FR-016**: The evals run summary MUST show, per case and trial, the held-out tests passed of
  those run and the mutants killed of those run, naming each failed held-out test and each
  surviving mutant; for a case where a grader does not apply it MUST say so.
- **FR-017**: The summary MUST still read a report without these results and MUST keep its
  existing pass^3, quarantine and cost lines unchanged.

#### What stays the same

- **FR-018**: Every existing case and grader MUST keep judging as before, except where this
  feature adds a grader to it; existing test changes are covered by the owner's approval of T180
  and MUST each carry `Tests-approved-by:`.
- **FR-019**: The engine and the platform's run checks are out of scope. If the tests or code
  agent fails a new grader in the first live run, this feature MUST change that agent's
  instruction until its cases pass pass^3 again, and that prompt change MUST pass the regression
  evals (Principle XIX); the failure MUST NOT be quarantined.

### Key Entities

- **Held-out test**: a test of the slice's rules on an input no example shows, kept from the agent,
  run against its code after the trial; names the rule it checks.
- **Mutant**: the reference implementation with one planted bug that breaks one rule on an input
  an example exercises; names the rule and the killing example.
- **Grader result**: as today (grader, passed, reason), plus the counts examined and passed and
  the names of the failed held-out tests or surviving mutants.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An implementation that passes every slice test but answers only the examples' inputs
  fails its code eval trial (self-test, no model).
- **SC-002**: A test set that binds every example but leaves one rule's outcome unchecked fails its
  tests eval trial, naming at least one surviving mutant (self-test, no model).
- **SC-003**: The reference implementation passes 100% of the held-out tests, and the reference
  tests kill 100% of the mutants.
- **SC-004**: The grader self-tests take at most 5 minutes longer than before this feature,
  measured in CI (the `go` job's `evals/steps` package time).
- **SC-005**: A live tests or code trial whose tests do not hang takes at most 2 minutes longer to
  grade than today, and a
  full live run costs no more model tokens than today (the graders call no model).
- **SC-006**: After one live run with the owner's go-ahead, the summary shows the held-out and
  mutant results for every tests and code trial.

## Assumptions

- The held-out tests and the mutants are written by hand for the one example slice; whether an
  adopted Go mutation tool plants the mutants instead is decided in the plan's OSS evaluation
  (Principle VI), e.g. gremlins or go-mutesting.
- The held-out tests and the mutants live beside the existing reference fixtures, outside every
  folder a trial's agent container mounts.
- The engine's own run checks (what a real run accepts) do not change: this feature judges the
  agents in their evals, not the projects the platform builds.
- A live run costs money and runs only with the owner's go-ahead.
