# Feature Specification: A developer sees how much the builds of a slice vary

**Feature Branch**: `012-run-variance`

**Created**: 2026-11-27

**Status**: Draft

**Input**: The remainder of need N-054 (owner chose it as the next work, 2026-11-27). Feature 008
covered the platform's own eval suites and left "how the developer's own runs of a slice vary" for
a later feature (its Assumptions). Measure and show how much the developer's builds of one slice
vary.

**Serves needs** ([problem space](../../docs/overview/problem-space.md)): N-054 (how much results
vary from run to run is measured and shown), for the developer's builds of a slice; N-050 (the same
method on the same input gives the same accepted result), whose reach this measures; N-052 (a
proven solution is captured once and reused), whose capture decision it informs.
**Uses terms** ([glossary](../../docs/overview/glossary.md)): Slice, Pattern, Run, Step, Check,
Method, Accepted result, Captured pattern, Hand-over, Trial set, Slice trial, Unstable trial set
(feature 008's Trial and Unstable case are their eval-suite counterparts).
**Builds on**: [feature 001](../001-prose-to-accepted-slice/spec.md) (runs, their steps, attempts,
outcomes and budgets; one active run at a time), [feature 006](../006-method-record/spec.md) (an
accepted result is reused for the same input under the same method; runs are compared step by
step), [feature 008](../008-eval-variance/spec.md) (passed trials out of k, the unstable mark and the
spread of effort, for eval cases) and [feature 011](../011-capture-pattern/spec.md) (a run that
ended done can be captured as a pattern).

## Clarifications

### Session 2026-11-27

- Q: Which runs are measured? → A: Only fresh trials the developer asks for, each reusing no
  earlier result; the slice's existing runs are not measured (owner).
- Q: Where does the developer ask for and read trials? → A: Through the API and the terminal
  client only; the run page is unchanged (owner; FR-012).
- Q: The largest k and the default set budget? → A: k from 1 to 5, 3 when not given; one set's
  default cost budget is $2, on top of each run's own budgets (owner; FR-001, FR-010).
- Found while planning: the engine keeps one step-worker registration per slice and step, so it
  cannot run k trials into k projects by itself. Q: How do the trials get their step workers? →
  A: all k projects' step workers are started before the set starts, and each registration names
  its project, so the set runs on its own with the terminal closed (owner; FR-013).
- Found while planning: a run that runs out of attempts or budget waits for the developer up to
  14 days. Q: What does a trial do then? → A: the set ends that trial handed over, counted as not
  done, and starts the next; its project stays (owner; FR-003).

## Why the developer's runs do not vary today

Under one method, a second run of the same slice reuses every accepted result of the first
(feature 006), so its files, checks and outcome are the same by construction and it uses no model.
How reliably the model builds the slice is therefore invisible: the developer sees one run, which
may have needed many attempts or review rounds, and cannot tell whether it was typical before
capturing it (feature 011) or relying on it. Each run's record already holds its steps' outcomes,
attempts, timestamps and model use (tokens and cost per attempt), and two runs can be compared step
by step; nothing builds a slice more than once without reuse, and nothing sums or spreads these
measures over several runs.

## User Scenarios & Testing *(mandatory)*

The user is the developer who has an approved slice and wants to know how reliably the platform
builds it before relying on it, capturing it, or changing it.

### User Story 1 - Build a slice several times and see how often it ends done (Priority: P1)

The developer asks for k independent builds (trials) of an approved slice. Each trial builds the
slice from scratch, reusing no earlier result, into its own project. When the trials have ended,
the developer sees how many ended done out of k, each trial's outcome, and whether the trials
disagree.

**Why this priority**: whether a build of the slice ends done is the variation that decides what
the developer does next (rely on it, capture it, or improve the slice).

**Independent Test**: with a recorded model that makes one of three trials run out of attempts,
ask for 3 trials of a slice: two end done, one is handed over, and the result shows `2/3`, each
trial's outcome and the unstable mark.

**Acceptance Scenarios**:

1. **Given** an approved slice whose every trial ends done, **When** the developer asks for 3
   trials, **Then** the result shows `3/3` done and no unstable mark.
2. **Given** a slice some of whose trials end done and some handed over, **When** the trials have
   ended, **Then** the result shows how many ended done out of k, names each trial's run and
   outcome, and marks the set unstable.
3. **Given** a slice whose earlier run ended done, **When** the developer asks for trials, **Then**
   no trial reuses that run's results or another trial's, and each trial uses the model as a first
   build would.
4. **Given** trials in progress, **When** one trial ends, **Then** the next starts by itself, and the
   developer can follow and cancel the trials as one piece of work.

---

### User Story 2 - See where the trials differ and how their effort spreads (Priority: P2)

For the ended trials of a slice, the developer sees, per step, the lowest, median and highest of the
trials' attempts, review rounds, duration, tokens and cost, and for each step whether its accepted
files were the same in every trial that reached it.

**Why this priority**: two trials that both end done may still differ in code or in cost; this
tells the developer which step varies and how much, which is what to improve or to capture.

**Independent Test**: with a recorded model whose code step needs 1, 1 and 2 attempts in three
trials and writes different code in the third, the code step shows attempts lowest 1, median 1,
highest 2 and its files marked as differing in the third trial, while a step that a fixed
procedure fills shows its files as the same.

**Acceptance Scenarios**:

1. **Given** ended trials, **When** the developer reads their result, **Then** every step shows the
   lowest, median and highest of each measure over the trials that reached it, and the totals per
   trial.
2. **Given** two trials whose accepted code differs, **When** the result is shown, **Then** that step
   is marked as differing and names the trials whose files differ, and the developer can compare
   any two of them step by step as today.
3. **Given** a measure no trial of a step carries (a fixed procedure uses no tokens), **When** the
   result is shown, **Then** it is shown as not available, not as zero spread.

---

### User Story 3 - The result of earlier trials stays readable and comparable (Priority: P3)

The developer reads the result of trials asked for earlier, lists all trials of a slice, and sees
whether a later set of trials of the same slice ended done more or less often than the one before
it, and under which method each ran.

**Why this priority**: the variation matters most across a change (a new method, a changed
slice); without the earlier result the developer cannot tell whether the change helped.

**Independent Test**: ask for trials of a slice twice, the second after a planted method change in
which one more trial is handed over: the second result names both methods and marks it less
reliable than the first.

**Acceptance Scenarios**:

1. **Given** trials of a slice that ended earlier, **When** the developer lists the slice's trials,
   **Then** each set shows when it ran, its method, k, how many ended done and whether it was
   unstable.
2. **Given** two sets of trials of the same slice, **When** the later one ends, **Then** its result
   shows the change in the share of trials that ended done since the earlier one, and marks a fall
   as less reliable.

### Edge Cases

- A captured pattern fits the slice: the trials build with the pattern a run would use
  (feature 011), so with a captured pattern they use no model and show no variation in outcome or
  files; the result names the pattern.
- k is 1, or larger than the limit: 1 is a single fresh build and shows no unstable mark; more than
  the limit is refused before anything starts.
- Another run is active: the set waits until it ends before starting its next trial (a run asked
  for directly is refused instead), within the set's time budget, since only one run is active at
  a time.
- A trial waits for the developer (out of attempts, review rounds or budget): the set ends it
  handed over, counted as not done, and starts the next; its project stays for inspection.
- A trial is interrupted (a platform stop, or the model unavailable on every attempt): the set waits
  until it continues or is resumed, within the set's time budget; one that ends otherwise counts as
  not done.
- A trial's project already exists, or one of the k projects has no step workers: the set is
  refused before any trial starts.
- The developer cancels the trials: the running trial is cancelled, no further trial starts, and the
  result counts only the trials that ended, naming the set as cancelled.
- The budget runs out: the trials stop, and the result says so; no trial starts beyond the budget.
- The slice changes or loses an approval while trials run (between two trials no run is active, so
  it can be edited and approved again): no further trial starts once any part differs from the part
  the set started with, and the set says why (`slice-changed`).
- The step workers of a later trial's project are gone (`ap down`): the set stops (`workers`).
- The method changes between two trials of one set (an upgrade while trials run): the result names
  each trial's method and does not compare outcomes across methods as if they were one set.
- The slice is erased: refused while one of its sets is running; afterwards its sets are erased
  with it.
- A set's 24 hours pass: its running trial is cancelled and the set stops (`time`).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The developer MUST be able to ask for k trials of an approved slice, k from 1 to 5 (3
  when not given; the contract declares both), and get the set's reference at once; the trials MUST run as one
  asynchronous operation with a queryable lifecycle and cancellation.
- **FR-002**: Each trial MUST be a run of the slice into its own new project, chosen as a run's
  pattern is chosen, that reuses no accepted result of any earlier run, and that is checked and
  ends exactly as any other run.
- **FR-003**: Trials of one set MUST run one after another; a trial MUST start only after the one
  before it ended, and none MUST start once the set is cancelled, its budget is used, or the slice
  is no longer approved. A trial that waits for the developer MUST be ended handed over by the set,
  and counts as not done.
- **FR-004**: The set's result MUST show how many trials ended done out of those that ended, each
  trial's run and outcome, and an unstable mark when at least one trial ended done and at least one
  did not; when the trials ran under more than one method, the result MUST say so.
- **FR-005**: The result MUST show per step, over the trials that reached it, the lowest, median and
  highest of attempts, review rounds, duration, tokens and cost, and the same for each trial's
  totals; a measure no such trial carries MUST be shown as not available.
- **FR-006**: The result MUST show per step whether its accepted files were the same in every trial
  that accepted it, and name the trials whose files differ.
- **FR-007**: The result MUST be derived only from the trials' run records; reading it MUST NOT call
  a model or change any run.
- **FR-008**: The developer MUST be able to list a slice's sets of trials, newest first, paginated,
  each with when it ran, its method, k, how many ended done and the unstable mark.
- **FR-009**: When a set of trials of a slice ends or stops, its result MUST show the change in the
  share of trials that ended done since the latest earlier ended or stopped set of the same slice
  with at least one ended trial, and mark a fall as less reliable; sets under different methods
  MUST be named as such. With no such set, no comparison is shown.
- **FR-010**: Trials MUST be bounded by a budget of cost (default $2) and time for the whole set,
  both declared in the contract, in addition to each run's own budgets; a trial MUST start only when
  the set's cost so far plus that run's own cost budget is within the set's budget.
- **FR-011**: A set's result and its trials MUST be kept as long as the slice and its runs are kept
  and erased with the slice; it MUST hold no content beyond what the trials' run records already
  hold.
- **FR-012**: The developer MUST ask for, follow, cancel, list and read trials through the API and
  the terminal client; the run page MUST NOT change.
- **FR-013**: The step workers of every trial's project MUST be ready before the set starts, and a
  trial MUST run only on the step workers of its own project; a set whose projects lack them, or
  one of whose projects exists, MUST be refused before any trial starts.

### Key Entities

- **Trial set** (glossary): the developer's request for k trials of one slice: its slice, k, the method at
  start, its state (running, ended, cancelled, stopped by budget or by a slice change), and its
  trials in order.
- **Slice trial** (glossary): one run of the slice in a trial set, reusing no earlier result; its outcome and
  measures come from its run record.
- **Trial result**: what a set's trials show together (FR-004 to FR-006, FR-009); derived, never
  stored apart from the runs.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For a set of 3 trials with a recorded model, the developer reads how many ended done,
  each trial's outcome and the unstable mark with one command, and the counts match the runs'
  recorded outcomes in 100% of planted cases.
- **SC-002**: Every planted set whose trials disagree in outcome is marked unstable, and no set
  whose trials agree is.
- **SC-003**: In planted sets, every step whose accepted files differ between trials is marked as
  differing, and no step whose files are the same is.
- **SC-004**: No trial reuses an accepted result: each trial's record shows model use for every step
  an agent fills when the pattern is the base pattern.
- **SC-005**: Reading a trial result adds $0 of model cost and changes no run record.
- **SC-006**: A set never uses more than its declared budget, and cancelling it starts no further
  trial.

## Assumptions

- What varies (by analogy with feature 008's owner answer): whether each trial ends done, the effort
  each trial and step takes, and whether each step's accepted files are the same. The content of the
  files is compared only by their hashes, as runs are compared today.
- Trials use the model and cost money like first builds; the developer asks for them explicitly, and
  nothing starts trials by itself.
- With few trials the measures describe the runs; no confidence interval or significance test is
  given, and the comparison marks only a fall in the share of trials that ended done.
- Each trial builds into its own project folder; the developer names a prefix and the trials'
  projects are named from it.
- Scope (owner, 2026-11-27): only fresh trials are measured; the slice's existing runs, which under
  one method are identical by reuse, are not.
- The set's time budget is declared in the contract next to its cost budget; its value is chosen in
  the plan from the runs' own time budget.
