# Feature Specification: The analyst passes its quarantined eval cases every time

**Feature Branch**: `005-analyst-reliability`

**Created**: 2026-11-22

**Status**: Draft

**Input**: Backlog task T185 (owner 2026-11-21, chosen as the next work 2026-11-22): "Analyst
reliability (evals run 44888638411 on `main` at d67edc6): `example` failed trial 1 of 3
(`ExampleContent`: no scenario creates two notes and expects them in creation order) and
`revise-inject-approvals-field` failed trial 2 of 3 (the revision lost its rules' scenarios and
kept IDs), each passing the other two trials, with no change to the analyst's prompt, model or
schema since its last green run (44809763108): model variance under pass^3. Both cases are
quarantined (`Quarantine: "T185"` in `src/tests/evals/analyst`): still run and reported, not
counted. Make the analyst pass both pass^3 (prompt or grader, with the regression evals), then
remove the quarantine; an eval change needs the owner's approval."

**Serves needs** ([problem space](../../docs/overview/problem-space.md)): N-013 (a changed agent
still behaves as expected), for the platform's own analyst agent.
**Uses terms** ([glossary](../../docs/overview/glossary.md)): Slice, Eval, Grader, Quarantine.
**Builds on**: [feature 001](../001-prose-to-accepted-slice/spec.md)'s analyst evals
([README](../../src/tests/evals/analyst/README.md)) and
[ADR 0050](../../docs/decisions/0050-failed-eval-jobs-keep-their-outputs.md) (a failed eval job
keeps its outputs for 7 days).

## Clarifications

### Session 2026-11-22

- Finding while specifying (a defect against ADR 0050, not a change of intent): in run
  44888638411 the step that keeps a failed job's outputs failed ("Relative pathing '.' and '..'
  is not allowed"), so neither failing answer was kept and the cause cannot be read. No failed eval
  job has kept its outputs since that step was added.
- Q: How much evidence before the quarantine is removed? → A: 10 of 10 trials of each of the two
  cases in one live run of only those cases (about $0.20), then one full analyst run with every
  case pass^3 (owner, SC-001). Noted by the analyze reviewer: 10 of 10 still passes a case that
  succeeds 90% of the time about a third of the time; the owner's bar stands.

## User Scenarios & Testing *(mandatory)*

The user of this feature is the project owner, who relies on the analyst evals (Principle XIX) to
say whether the analyst still turns a description into a correct proposal. A quarantined case
says nothing: today two of the analyst's cases can fail without anyone being told.

### User Story 1 - A failed eval trial can be read afterwards (Priority: P1)

When an analyst or step eval trial fails in CI, the owner can read that trial's answer, raw events
and report for 7 days, whether the case counts or is quarantined.

**Why this priority**: the cause of both failures is unknown because their answers were lost. Every
later fix is a guess until a failing answer can be read.

**Independent Test**: the workflow's rules, checked without running it, keep the report folder
for a planted report with a failed counted trial and for one with only a quarantined failed
trial, and keep nothing for a report with every trial passed; the folder's path has no `.` or `..`
segment.

**Acceptance Scenarios**:

1. **Given** an eval job with a failed counted trial, **When** the job ends, **Then** its report
   folder (report, each trial's answer and events) is kept for 7 days, after the credential scan
   passes.
2. **Given** an eval job whose only failed trials belong to quarantined cases, **When** the job
   ends green, **Then** its report folder is kept for 7 days as in scenario 1.
3. **Given** an eval job with no failed trial, **When** the job ends, **Then** nothing is kept.
4. **Given** the credential scan finds a match, **When** the job ends, **Then** nothing is kept and
   the job fails.

---

### User Story 2 - The analyst passes `example` every time (Priority: P1)

The description of the quickstart example ("listing returns all notes in creation order ...")
always yields a proposal whose examples show two notes listed in the order they were created, as
the `ExampleContent` grader requires.

**Why this priority**: `example` is the quickstart's own description; a proposal that misses one
of its three stated behaviours is what a new user sees first.

**Independent Test**: the `example` case, counted again (no quarantine), passes in every trial of
the live runs SC-001 names.

**Acceptance Scenarios**:

1. **Given** the example description, **When** the analyst proposes, **Then** a scenario creates
   at least two notes with different texts and expects them listed in creation order.
2. **Given** a proposal that does show creation order but in a form the grader does not read (if
   a kept failing answer shows one), **When** it is graded, **Then** the grader is the part found
   wrong and is fixed, with the owner's approval of that change, and still fails a proposal that
   shows no order (its planted case).

---

### User Story 3 - A revision keeps every rule's examples (Priority: P1)

Revising the earlier notes proposal with an answer that also asks for an `approvals` field always
yields the whole revised proposal: every rule keeps its `Rule:` block and scenarios, every kept ID
stays, the answers are applied, and no `approvals` field appears.

**Why this priority**: a revision that drops every scenario loses the developer's examples; the
platform rejects it, but the developer has to answer again.

**Independent Test**: the `revise-inject-approvals-field` case, counted again, passes in every
trial of the live runs SC-001 names.

**Acceptance Scenarios**:

1. **Given** the earlier proposal and the answers of `revise-inject-approvals-field`, **When** the
   analyst revises, **Then** every grader of the revision suite passes (`FullyLinked`,
   `ExamplesPresent`, `KeepsIDs`, `AnswersApplied` among them).

---

### Edge Cases

- A fix to the analyst's instruction must not make another analyst case fail: every analyst and
  revision case runs in the same live runs and counts.
- A failing answer read after US1 may show a cause other than the backlog task's reading (for
  example a cut-off or malformed answer); the fix follows what the answer shows (Fix the cause).
- A trial that fails for a reason outside the analyst (gateway, provider error, time-out) is
  reported as such and is not evidence about the analyst.
- A 10-trial run may show no failure of a case: there is then no answer to diagnose, and the owner
  decides the next step (more trials, or a fix from the best evidence) before the quarantine goes.
- The keep-outputs step must not keep a folder that holds a credential pattern, whatever the
  job's result.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: An eval job that has at least one failed trial, counted or quarantined, MUST keep its
  report folder as one artifact for 7 days, after the credential scan passes.
- **FR-002**: An eval job with no failed trial MUST keep nothing.
- **FR-003**: When the credential scan finds a match, nothing MUST be kept and the job MUST fail.
- **FR-004**: The cause of each of the two failures MUST be decided from a kept failing answer
  (US1) or from a reproduction of the case, and recorded in the plan's research before any fix.
- **FR-005**: The fix MUST change whichever of the analyst (its instruction, within the current
  model and schema) or the grader the cause shows to be wrong, never weaken a grader that is right.
- **FR-006**: A grader change MUST keep its planted bad proposal failing and its good proposal
  passing, and needs the owner's approval of that change.
- **FR-007**: After the fix, the quarantine of `example` and `revise-inject-approvals-field` MUST be
  removed (an eval change, with the owner's approval), so both count again; T185 is then closed.
- **FR-008**: Every other analyst and revision case MUST still pass pass^3 in the same live runs.
- **FR-009**: The analyst's model, its pinned version and the proposal schema MUST NOT change in
  this feature.
- **FR-010**: One live run of the analyst evals MUST be able to run named cases of both analyst
  suites together and k trials per case for a whole number k from 1 to 20 (default 3); a name in
  neither suite, or any other k, MUST fail the run before any trial.

### Key Entities

- **Eval report folder**: per eval job, the report and each trial's answer and raw events; what
  US1 keeps.
- **Quarantine**: the open backlog task a flaky case names; the case runs and is reported but does
  not count.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: With the quarantine removed, `example` and `revise-inject-approvals-field` each pass
  10 of 10 trials in one live run of only those two cases, and then every analyst and revision
  case, these two included, passes pass^3 in one full analyst run.
- **SC-002**: A failed eval job in CI keeps its report folder: proven by the workflow's rules on
  planted reports, and shown by the first eval job with a failed trial after the change.
- **SC-003**: No analyst case is quarantined at the end of the feature.

## Assumptions

- A live run costs money and runs only with the owner's go-ahead. A run of only the two cases, or
  with 10 trials, is not possible today (one case of each analyst suite cannot be named together,
  and k is fixed at 3), hence FR-010.
- The quarantine mechanism itself (a quarantined case runs, is reported, does not count) stays as
  it is.
- The analyst's prompt is versioned and reviewed like code (Principle XIII); a change to it is
  proven by the regression evals before merge (Principle XIX).
