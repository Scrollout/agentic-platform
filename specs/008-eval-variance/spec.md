# Feature Specification: Eval suites show how much their trials vary

**Feature Branch**: `008-eval-variance`

**Created**: 2026-11-23

**Status**: Draft

**Input**: Need N-054, "How much results vary from run to run is measured and shown" (owner chose it
as the next work, 2026-11-23). Owner's scope answers the same day: the eval suites, not the
developer's own runs; what varies is whether each trial passes and the effort each trial takes.
Backlog task T192 (the existing summary prints `pass^3` whatever k a report's trials show) stays
in the backlog (owner, Clarifications).

**Serves needs** ([problem space](../../docs/overview/problem-space.md)): N-054 (how much results
vary from run to run is measured and shown), for the platform's own agents in their eval suites;
N-013 (a changed agent still behaves as expected), which reads that variation.
**Uses terms** ([glossary](../../docs/overview/glossary.md)): Eval, Grader, Quarantine, Method,
Trial, Unstable case, Eval measurement.
**Builds on**: [feature 004](../004-edge-case-graders/spec.md) and
[feature 005](../005-analyst-reliability/spec.md) (the eval reports and their summary, pass^k over
three trials), [ADR 0078](../../docs/decisions/0078-eval-jobs-keep-outputs-of-any-failed-trial.md)
(a job with a failed trial keeps its outputs for 7 days), and Constitution Principle XIX
("Behaviour users rely on MUST be reported as pass^k over repeated trials").

## Clarifications

### Session 2026-11-23

- Found while planning: this repository's artifact and log retention is 1 day, and 1 day is also
  the most its organization allows, so the outputs ADR 0078 keeps "for 7 days" expire after 1 day.
  Q: Where are measurements kept for 90 days? → A: as the eval run's own kept outputs; the owner
  raises the organization's and the repository's retention to 90 days (Assumptions, Dependencies).
- Q: T192 changes the existing summary, whose tests pin its output byte for byte: change those
  tests? → A: No (owner): the existing summary stays as it is and T192 stays in the backlog; the
  trials section this feature adds names pass^k with k from the report (FR-001).
- Found while planning: eval runs are started on feature branches, which are then merged into the
  default branch by fast-forward, so almost no run is on the default branch. Q: Which earlier
  run is the baseline? → A: the latest earlier kept run whose commit is in the current run's
  commit's history, on any branch, as the suite selection already chooses its base (owner;
  FR-006).
- Found while planning: each agent's pinned model is a fact of the commit (its agent definition),
  and reading it in the eval job would need a YAML reader there; the measurement names the commit
  instead, which identifies the models, the method and every other input (FR-004).

## User Scenarios & Testing *(mandatory)*

The user is the project owner, or a maintainer, who reads an eval run's results to decide whether
a change to an agent, its prompt, its model or the platform can be merged. Today each case shows
only pass or fail over its three trials, and the suite shows its total tokens and cost. A case
that passed 2 of 3 trials reads like one that passed none; how long and how costly each trial was,
and how much that spread, is in the raw report only; and nothing tells whether a case has become
less reliable or more costly than in earlier runs. Feature 005's two quarantined cases were found
only because a trial happened to fail.

### User Story 1 - Each case shows how often and how evenly its trials passed (Priority: P1)

After an eval run, the maintainer sees for every case how many of its trials passed, next to its
pass^k result, and every case whose trials disagree is marked unstable, so a flaky case is told
apart from a case that fails every time.

**Why this priority**: an unstable case is the variation N-054 is about, and the one that decides
what to do next (quarantine and diagnose a flaky case, fix a case that always fails).

**Independent Test**: summarize a report whose cases passed 3 of 3, 2 of 3 and 0 of 3 trials: each
shows its count and its pass^k result, and only the 2-of-3 case is marked unstable.

**Acceptance Scenarios**:

1. **Given** a report in which every trial of a case passed, **When** the summary is shown,
   **Then** the case shows `3/3` (its trials' count) and pass, and is not marked unstable.
2. **Given** a case with some trials passed and some failed, **When** the summary is shown,
   **Then** the case shows how many passed out of how many, fails pass^k as today, and is marked
   unstable.
3. **Given** a case whose every trial failed, **When** the summary is shown, **Then** it shows
   `0/k`, fails, and is not marked unstable.
4. **Given** a report whose cases ran k trials each, for any k, **When** the summary is shown,
   **Then** the trials section names pass^k with that k, not a fixed 3; the existing summary
   above it is unchanged.
5. **Given** a quarantined case, **When** the summary is shown, **Then** it shows its count and
   whether it is unstable, and is still not counted, as today.
6. **Given** a whole suite, **When** the summary is shown, **Then** it also states the suite's
   trials passed out of trials run and how many counted cases are unstable.

---

### User Story 2 - Each case shows how much effort its trials took and how that spread (Priority: P1)

For every case the maintainer sees the lowest, the median and the highest duration, tokens and
cost of its trials, and attempts where the suite counts them, so a case whose trials pass but
take very different effort is visible.

**Why this priority**: effort that varies widely predicts both cost overruns and later failures
(a trial near its time limit), and is the second kind of variation the owner chose.

**Independent Test**: summarize a report with known per-trial durations, tokens, costs and
attempts: each case shows the right lowest, median and highest value of each; a trial without
usage is left out of that measure and counted as missing.

**Acceptance Scenarios**:

1. **Given** a case whose trials took 10 s, 20 s and 60 s, **When** the summary is shown,
   **Then** its duration shows 10 s, 20 s and 60 s as lowest, median and highest.
2. **Given** trials that carry their tokens and cost, **When** the summary is shown, **Then**
   each case shows the lowest, median and highest of input tokens, output tokens and cost.
3. **Given** a suite whose trials count attempts, **When** the summary is shown, **Then** each
   case also shows the lowest, median and highest attempts; a suite that does not count them shows
   none.
4. **Given** a trial that carries no usage, **When** the summary is shown, **Then** that trial is
   left out of the token and cost measures and the case says how many trials those measures
   cover.

---

### User Story 3 - Each run is compared with the earlier runs of the same cases (Priority: P2)

Every eval run keeps its measurements, and its summary shows, for every case, how its passed
trials and its median effort changed since the latest earlier kept run of the same case in this
commit's history, so a slow drift in reliability or cost is seen without reading old runs by hand.

**Why this priority**: one run with three trials says little; the change across runs is what shows
that a case has become less reliable or more costly. It needs US1 and US2's measures first.

**Independent Test**: keep the measurements of one run, then summarize a later run of the same
cases whose case passed fewer trials and cost more: the summary names that case's earlier and
current passed trials and median cost, and which earlier run it compared with.

**Acceptance Scenarios**:

1. **Given** an eval run, **When** it ends, whether it passed or failed, **Then** its
   measurements are kept, and can be read later without running any trial again.
2. **Given** a kept earlier run of the same suite whose commit is in the current commit's
   history, **When** a later run's
   summary is shown, **Then** each case that ran in both shows its earlier and current passed
   trials, median duration and median cost, and the summary names the earlier run and its commit.
3. **Given** a case whose share of passed trials is lower than in the earlier run, **When** the
   summary is shown, **Then** that case is marked as less reliable than before.
4. **Given** no kept earlier run of a case (a new case, a renamed case, or no run in the kept
   period), **When** the summary is shown, **Then** the case says there is nothing to compare with.
5. **Given** kept measurements, **When** anyone reads them, **Then** they hold no answer, prompt,
   file, error output or other protected content, only the measures and what identifies the run.

---

### Edge Cases

- A run of only some of a suite's cases (the step suite runs once per agent): only the cases that
  ran are measured and compared; a case that did not run is not reported as missing.
- Two runs whose k differ: the comparison uses each run's share of passed trials, and names both
  counts (`2/3` before, `5/5` now).
- A trial stopped by its time limit counts as failed, as today, and its duration is the time it
  ran.
- An earlier run whose commit is not in the current commit's history (an unmerged experiment on
  another branch) is not a baseline, so it cannot hide a drift; a run on an earlier commit of the
  same branch, or of a branch merged before, is.
- The earlier run differs in commit, model or method: the comparison is still shown, and names the
  earlier run's commit so the maintainer can see what changed between them.
- A report that cannot be read fails the job, as today; a kept earlier measurement that cannot be
  read is reported as such and the summary is shown without the comparison.
- Cost is Docker Agent's estimate from the price the agent declares, not the provider's bill; the
  summary says so as today.
- The median of an even number of trials is the mean of the two middle values.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Every eval run's summary MUST gain a trials section that shows, for every case, the number of its
  trials that passed out of the number run, next to its pass^k result, with k taken from the
  report's trials.
- **FR-002**: A case MUST be marked unstable when at least one of its trials passed and at least
  one failed. The suite's summary MUST state its trials passed out of trials run and its number of
  unstable counted cases. A quarantined case is measured and marked like any other and, as today,
  not counted.
- **FR-003**: The summary MUST show, for every case, the lowest, median and highest of its trials'
  duration, input tokens, output tokens and cost, and of its attempts where the suite counts
  attempts. A trial that does not carry a measure MUST be left out of that measure, and the case
  MUST state how many trials the measure covers.
- **FR-004**: Every eval run, passed or failed, MUST keep its measurements: per case and trial,
  whether it passed, its attempts, duration, tokens and cost, the case's quarantine, and per run
  the suite, the cases selected, k, the commit, the branch and the run's identity; the commit
  names the agents' pinned models. The kept measurements MUST NOT hold protected content
  (Principle XI) and MUST pass the same credential scan as kept eval outputs.
- **FR-005**: Kept measurements MUST stay readable for at least 90 days, without running a trial
  again and without access to the model key.
- **FR-006**: The summary MUST compare every case that ran with the latest earlier kept run of the
  same suite and case whose commit is in the current run's commit's history, on any branch: its earlier and current passed trials, median
  duration and median cost, naming the earlier run and its commit. A case whose share of passed
  trials fell MUST be marked as less reliable than before. A case with no earlier kept run MUST say
  so.
- **FR-007**: Measuring, keeping and comparing MUST NOT change whether an eval job passes or
  fails: pass^k over the counted cases stays the only gate. They MUST add no trial and no model
  call.
- **FR-008**: The summary MUST be derived from the report and the kept earlier measurements alone,
  so the same inputs always give the same summary.
- **FR-009**: A measure that no trial of a case carries MUST be shown as not available for that
  case. The summary shown today MUST stay byte for byte as it is; the trials section and the
  comparison follow it.

### Key Entities

- **Trial**: one run of one eval case by its agent, judged by the case's graders; passes or fails,
  and carries its duration, its tokens and cost, and, in some suites, its attempts.
- **Unstable case**: an eval case some of whose trials passed and some failed in one run.
- **Eval measurement**: what one eval run keeps about its cases and trials (FR-004): the measures
  and what identifies the run, never protected content; the basis of the comparison with later
  runs.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For 100% of the cases in an eval run's summary, the maintainer reads the passed
  trials out of trials run and the lowest, median and highest duration and cost without opening
  the report.
- **SC-002**: Every case in a planted report whose trials disagree is marked unstable, and no case
  whose trials agree is.
- **SC-003**: After two eval runs of the same suite, the second on a descendant of the first's
  commit, the second run's summary
  shows the change since the first for 100% of the cases that ran in both, and a case planted to
  pass fewer trials is marked less reliable.
- **SC-004**: Measuring adds 0 trials and $0 of model cost to an eval run, and adds no more than 1
  minute to its job.
- **SC-005**: A kept measurement holds no field outside FR-004's list, and the credential scan
  finds nothing in it.

## Assumptions

- Scope (owner, 2026-11-23): N-054 is in scope for the platform's own eval suites. How the
  developer's own runs of a slice vary is out of this feature and stays in N-054's scope for a
  later feature.
- What varies (owner, 2026-11-23): pass or fail per trial and effort per trial. Whether trials
  produced the same answer content is not measured.
- CI keeps running 3 trials per case. With so few trials the measures describe the runs; the
  summary gives no confidence interval or significance test, and the comparison marks only a fall
  in the share of passed trials, not a change in effort.
- The audience reads the eval job's summary; no new page or service is added.
- The step suite counts one agent run per trial and records no attempts; the analyst suite records
  its attempts. Neither suite's trials change.
- 90 days lets a drift over several months of merges be seen; where and how the measurements are
  kept is decided in the plan's research.
- Dependency: the owner raises the organization's and the repository's artifact and log retention
  from 1 to 90 days (Clarifications); until then kept measurements expire after 1 day and US3
  finds no baseline older than that.
