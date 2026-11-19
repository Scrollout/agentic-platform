# Feature Specification: Runs and operations survive a platform stop, on an adopted durable-execution engine

**Feature Branch**: `002-durable-execution`

**Created**: 2026-11-18

**Status**: Draft

**Input**: Feature 001's task T151 (owner, 2026-11-17; [ADR 0056](../../docs/decisions/0056-durable-execution-custom-for-the-mvp-temporal-after.md)):
"Adopt Temporal for the engine's durable execution: the step machine's resume, checkpoints,
retries and timeouts, the run history and the job queue behind operations, with the checks,
review rules, reuse keys, slices and artifacts as its workflow and activities and artifacts passed
by hash; the client's retry wait gets one owner. Done when Temporal and its database run in
Compose pinned by digest, the engine's runs resume through Temporal with every check job green,
the retry wait has one owner, and the plan's Principle VI exception and its Single owner row are
removed."

**Serves needs** ([problem space](../../docs/overview/problem-space.md)): N-040, and keeps
N-050, N-051, N-053, N-056, N-057, N-060 and N-063 as feature 001 left them.
**Uses terms** ([glossary](../../docs/overview/glossary.md)): Run, Step, Check, Hand-over,
Operation, Durable execution.
**Builds on**: [feature 001](../001-prose-to-accepted-slice/spec.md), whose behaviour this
feature keeps except where a requirement below changes it.

## Clarifications

### Session 2026-11-18

- Q: After the platform is up again, does a run the stop interrupted continue by itself, or wait
  for the developer to resume it? → A: It continues by itself once its step workers are available
  (Principle X: a restart resumes the run) (owner).
- Q: Do a step's attempts count across a stop, so that a step fails its checks at most 3 times
  in all (backlog T138)? → A: Yes across a platform stop: only the interrupted attempt runs again.
  A developer's resume after a hand-over is a person's choice and starts a fresh count (owner).
- Q: Is need N-040 (never silently lose work the platform has accepted) a `yes`? → A: Yes,
  confirmed (owner).

### Session 2026-11-19

- Q: Erasing a slice is itself an operation that must stay followable (FR-009); does FR-017's
  erasure include that operation? → A: No: the erasing operation stays followable until its
  retention ends (24 h), holding only the slice's ID and no run content, and is then removed;
  every other run and operation of the slice is erased at once (owner, from `/speckit-analyze`).
- Q: After an engine-only restart nothing registers the step workers again, so a stopped run
  waits for them (CI run 44537905339, T014); how does FR-005 hold? → A: the engine keeps the step
  workers' registrations across its restart, so the run reaches the still-running step workers
  and continues by itself; a worker that is really gone still makes the run wait for
  `ap build-workers --resume` (owner, 2026-11-19; ADR 0071).
- Q: A step worker that is restarting when the run repeats its call refuses it, and nothing then
  tells the waiting run that the kept worker is back (CI run 44553348919, T014 procedure step);
  what should the run do? → A: retry the same call by the platform's one retry rule, as for the
  project's restore and commit; only when every retry fails does the run wait for its workers as
  before (owner, 2026-11-19).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A run survives a platform stop (Priority: P1)

A developer starts a build. The platform stops in the middle of it: the engine crashes, is
killed, or the machine restarts. When the platform is up again, the run continues from where it
was. No accepted step runs again, no change to the project's version history is made twice, and
the run ends with the same outcome and the same artifacts it would have had without the stop.
It continues by itself, without the developer resuming it, once its step workers are available.

**Why this priority**: The constitution's Principle X requires that an accepted run is never
lost and that a restart never repeats a completed side effect. Feature 001 meets this only with
its own code, which Principle VI does not allow once mature open source covers it (ADR 0056).

**Independent Test**: Stop the engine during each kind of step (an agent step, a procedure step,
a review round, a version-history commit) and start it again. Each run ends as an uninterrupted
run of the same slice does, with byte-identical artifacts, and its record shows no accepted step
run twice.

**Acceptance Scenarios**:

1. **Given** a run whose engine stops while an agent step is being attempted, **When** the
   platform is up again, **Then** the run continues by itself, the project is restored to its
   last accepted step, the interrupted attempt is made again with its own attempt number, and no
   accepted step runs again.
2. **Given** a run whose engine stops after a step was accepted but before the project's version
   history recorded it, **When** the run continues, **Then** the version history records the step
   exactly once.
3. **Given** a run whose engine stops during a review round, **When** the run continues, **Then**
   the round's findings, open findings and scope are those it had before the stop, and the review
   still runs at most 3 rounds.
4. **Given** a run that had used review rounds, step attempts and part of its budget before the
   stop, **When** it continues, **Then** those rounds, attempts and that spend still count against
   its limits (FR-003).
5. **Given** a run waiting for the developer after a hand-over, **When** the platform stops and
   starts again, **Then** the run is still waiting, and it expires 14 days after the hand-over,
   not after the restart.
6. **Given** a run that continued after a stop and ended done, **When** the developer replays it,
   **Then** every step is reused without a model call and the artifacts are byte-identical.

---

### User Story 2 - An accepted operation is never lost (Priority: P2)

A developer asks for a change (propose a slice, approve a part, start, resume or cancel a run).
The platform accepts it and answers with an operation the developer follows. If the platform
stops before the operation ends, the operation is still there when it is up again: it continues
or ends with a recorded outcome, and the developer can follow it with the same identifier.

**Why this priority**: Today an operation lives only in the engine's memory, so a stop loses
every accepted operation and its identifiers start again from the first, which breaks
Principle IV (accepted, recorded, queued) and Principle X (acknowledged only once durably
recorded).

**Independent Test**: Start an operation, stop the engine before it ends, start it again, and
follow the operation by its identifier: it ends succeeded, failed or cancelled, and a new
operation gets a new identifier.

**Acceptance Scenarios**:

1. **Given** an operation accepted before a stop, **When** the developer follows it after the
   restart, **Then** they see its status and it reaches exactly one end: succeeded, failed or
   cancelled.
2. **Given** an operation that failed and was retried before the stop, **When** it continues,
   **Then** the retries already made still count, and once they are used up it stays visible as
   failed with the reason.
3. **Given** operations created before a stop, **When** a new operation is accepted after it,
   **Then** its identifier was never given to another operation.
4. **Given** the part of the platform that records runs and operations is unavailable, **When**
   the developer asks for a change, **Then** the request is refused as retryable and nothing is
   reported as accepted.

---

### User Story 3 - Nothing the developer relies on changes (Priority: P3)

Everything feature 001 delivered keeps working as it did: describing and approving a slice,
building it, the five planted defects being stopped, hand-over, cancel, replay, compare, the run
page and the terminal client. The engine's API, the terminal client's commands, the run record
and its events keep their published shape.

**Why this priority**: The change replaces how runs are kept, not what the platform does. A
regression would undo the accepted MVP.

**Independent Test**: Every check job, acceptance suite, planted suite and eval suite that judged
feature 001 passes on this feature's code.

**Acceptance Scenarios**:

1. **Given** feature 001's acceptance scenarios, planted defects and eval suites, **When** they
   run against this feature, **Then** they all pass, and any existing test that had to change was
   approved by the project owner before it changed.
2. **Given** the engine API, terminal client and run-event contracts of feature 001, **When** this
   feature's contracts are compared with them, **Then** there is no breaking change.
3. **Given** a run recorded by feature 001 that has ended, **When** the developer inspects,
   compares or replays it after the upgrade, **Then** they get what they got before it.

### Edge Cases

- The platform stops during a step attempt: the attempt's partial writes are undone before the
  attempt is made again; the interrupted attempt does not count as failed (FR-003).
- The platform stops while a check is running: the check runs again; a check that cannot run
  still counts as failed (FR-012 of feature 001).
- The model is unavailable on every attempt of a step: the run is interrupted with that reason,
  as in feature 001, and the developer resumes it.
- Only the engine restarts and the step containers keep running: the run continues by itself
  with the same step workers; nobody registers them again (FR-005). A worker that is briefly
  unreachable (restarting) is called again by the retry rule before the run waits for it.
- The machine restarts and the step containers are gone: the run waits until the developer
  rebuilds them for the run (as `ap build-workers --resume` does today), then continues by itself
  (FR-005); its time budget does not run while it waits.
- The developer cancels a run while the platform is down: the request is refused as retryable;
  once the platform is up the cancel is accepted and the run ends cancelled.
- A run recorded before the upgrade has no outcome yet: the upgraded platform refuses to start,
  naming the run, until the developer has ended it (resumed to an end, or cancelled) with the
  version that recorded it.
- The developer erases a slice: nothing of its runs is left in any store the platform keeps,
  including the one that keeps runs and operations durable; only the erasing operation itself
  stays followable until its retention ends (FR-017).
- A run's record would exceed what the durable-execution engine holds for one run: artifacts are
  kept by hash outside it, so a run within feature 001's limits always fits.

## Requirements *(mandatory)*

### Functional Requirements

#### Runs

- **FR-001**: A run MUST continue after a platform stop from its last recorded step, and MUST NOT
  run an accepted step again or make a version-history change twice.
- **FR-002**: Before a step is attempted again after a stop, the project MUST be restored to its
  last accepted step, as before every attempt.
- **FR-003**: Review rounds, and spent tokens, cost and time MUST count across a stop: a run MUST
  NOT get new rounds or budget because the platform stopped. The time budget counts only the time
  the platform is running the run. A step's attempts MUST count across a stop: only the
  interrupted attempt is made again, with its own number, and a step fails its checks at most 3
  times across stops. A developer's resume after a hand-over starts a fresh count for the step it
  resumes (feature 001 starts the count again at 1 after every resume, backlog T138).
- **FR-004**: A run waiting for the developer MUST keep waiting across a stop, and MUST expire 14
  days after its hand-over.
- **FR-005**: After the platform is up again, a run interrupted by the stop MUST continue by
  itself once its step workers are available, without the developer resuming it (feature 001
  waits for `ap resume`). A run interrupted for another reason (for example the model unavailable
  on every attempt) still waits for the developer.
- **FR-006**: Every run MUST still end in exactly one recorded outcome (done, handed over,
  expired, cancelled), and its record MUST keep feature 001's events and order, with every stop
  and continuation recorded.
- **FR-007**: A run that continued after a stop MUST be replayable, reusing every accepted result
  without a model call, with byte-identical artifacts.

#### Operations

- **FR-008**: The platform MUST acknowledge a state-changing request only after its operation is
  durably recorded; a failure before that MUST be returned as retryable.
- **FR-009**: An accepted operation MUST survive a stop: after it, the operation MUST be
  followable by the same identifier, MUST continue or end, and MUST end in exactly one status
  (succeeded, failed, cancelled).
- **FR-010**: Retries of a failed operation MUST count across a stop, and an operation whose
  retries are used up MUST stay visible as failed with its reason.
- **FR-011**: An operation identifier MUST never be given to two operations, including across
  stops.

#### Retries have one rule

- **FR-012**: The retry rule (how many times, how long to wait, the jitter and the longest wait)
  for operations, version-history calls and the terminal client's waits MUST be declared once and
  applied from that one place.

#### What stays the same

- **FR-013**: The engine API, the terminal client's commands and outputs, the run-event schema
  and the run page MUST show no breaking change against feature 001.
- **FR-014**: Feature 001's requirements FR-001 to FR-025 MUST still hold, except where this
  feature changes them (FR-003, FR-005).
- **FR-015**: Runs recorded by feature 001 that have ended MUST stay inspectable, comparable and
  replayable after the upgrade.

#### Data

- **FR-016**: The durable record of runs and operations MUST hold artifacts by hash, never their
  content, and MUST hold no prompt, completion or other protected content beyond what feature
  001's run record holds.
- **FR-017**: Erasing a slice MUST erase its runs and operations from every store the platform
  keeps, except the erasing operation itself, which MUST stay followable until its retention ends
  (24 h after it ends) and MUST hold only the slice's ID.

### Key Entities

- **Run**: as in feature 001; it now also survives a platform stop, keeping its attempts,
  rounds, budgets and expiry.
- **Operation**: a state-changing request the platform accepted, followed by its identifier until
  it ends succeeded, failed or cancelled; it now survives a platform stop.
- **Run record**: as in feature 001, the ordered history of a run, from which it is inspected,
  replayed and compared; it also records each stop and continuation.
- **Retry rule**: the one declaration of how a failed call is retried: attempts, waits, jitter and
  the longest wait.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For each of four stop points (an agent step, a procedure step, a review round, a
  version-history commit), a run stopped there and continued ends with the same outcome and
  byte-identical artifacts as the same run without a stop, in 3 out of 3 trials, with no accepted
  step run twice.
- **SC-002**: 100% of operations accepted before a stop can be followed after it and end in
  exactly one status, and no operation identifier is given out twice.
- **SC-003**: Every check job, acceptance suite, planted suite (5 planted defects, 3 out of 3
  trials) and eval suite that judged feature 001 passes on this feature.
- **SC-004**: On a clean machine, the quickstart still reaches an accepted build in under 30
  minutes, and the parts this feature adds use at most 512 MiB of memory when no run is active.
- **SC-005**: The retry rule is declared in exactly one place, and the plan of feature 001
  carries no exception for durable execution or for the retry wait.
- **SC-006**: After a slice is erased, no store the platform keeps holds any of its runs or
  operations other than the erasing operation, which is gone 24 h after it ends.

## Assumptions

- One developer on their own machine, as in feature 001; tenants, sharing and hosting stay out of
  scope.
- The durable-execution engine is Temporal, decided in
  [ADR 0056](../../docs/decisions/0056-durable-execution-custom-for-the-mvp-temporal-after.md)
  under Principle VI; the plan settles its store, its version and how the engine's own rules
  become its workflow and activities. The engine's checks, review rules, reuse keys, slices and
  artifacts stay the platform's own.
- Every part this feature adds runs as feature 001's parts do (Principles VIII and XIII): in a
  container, pinned by version and digest, with no egress, non-root and with a read-only root
  where it allows.
- Runs recorded before the upgrade come only from the project owner's own machines, so a run
  left without an outcome is ended with the old version before upgrading (Edge Cases) rather than
  carried over.
- The 512 MiB memory bound (SC-004) reflects the owner's small, shared host; the plan measures the
  real figure and the owner may set another.
- The time a run waits while the platform is down, or for its step workers to be rebuilt, does
  not count against its time budget, which bounds the platform's work on the run.
- Feature 001's open tasks in [the backlog](../backlog.md) stay there unless the plan finds that
  this feature replaces the code they concern (for example T086, T138, T158 and T055c's retry
  items), in which case the plan names them.
