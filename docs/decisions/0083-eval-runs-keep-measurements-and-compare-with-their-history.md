---
type: Decision
title: "ADR 0083: Eval runs keep measurements and compare with their history"
description: Every eval job keeps a metadata-only measurement of its trials for 90 days and shows, after the unchanged summary, each case's passed trials out of k, whether it is unstable, the spread of its effort, and the change since the latest earlier measurement whose commit is in its history, fetched by the config job; measuring never fails a job.
tags: [decisions, ci, evals]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/008-eval-variance/plan.md
    title: Feature 008 plan, Decisions
  - id: research
    resource: ../../specs/008-eval-variance/research.md
    title: Feature 008 research, R1 to R5
  - id: adr0078
    resource: /decisions/0078-eval-jobs-keep-outputs-of-any-failed-trial.md
    title: ADR 0078
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-23T14:00:00Z }
---

# Origin

The plan of [feature 008](../../specs/008-eval-variance/plan.md) (need N-054); the owner chose the
storage, the baseline rule and the test changes on 2026-11-23.

Sources: [Feature 008 plan, Decisions](../../specs/008-eval-variance/plan.md).

# Context

- An eval summary showed only pass or fail per case over three trials, so a case that passed 2
  of 3 read like one that passed none, and nothing showed a drift across runs.
- Eval runs start on feature branches that are merged by fast-forward, so almost no run is on the
  default branch.
- This repository kept artifacts and logs for 1 day, its organization's maximum.

Sources: [Feature 008 research, S5, S14, R1, R3](../../specs/008-eval-variance/research.md).

# Decision

- Every eval job writes a measurement from its report: per case and trial only whether it passed,
  attempts, seconds, tokens, cost and the quarantine, and the run's suite, cases, commit, branch,
  run id and attempt. It is scanned for credentials and uploaded as
  `eval-measurements-<suite>[-<cases>]-<attempt>`, kept 90 days, whether the job passed or failed.
- The `config` job, which already holds `actions: read`, finds for each suite the newest
  unexpired measurement of an earlier run whose commit is an ancestor of this one, on any branch,
  and passes it to the eval job as a job output; the eval job gains no permission.
- After the existing summary, which stays as it was, each case shows passed/run, pass^k with k
  from the report, unstable when its trials disagree, the lowest, median and highest of its
  effort, and the change since the earlier measurement, marked less reliable when its share of
  passed trials fell.
- Measuring, keeping and comparing never fail a job; pass^k over the counted cases stays the gate.
- The organization's and the repository's artifact and log retention is 90 days (owner).
- ADR 0078 stands; its "7 days" holds only once the retention allows it.

Sources: [Feature 008 research, R1 to R5](../../specs/008-eval-variance/research.md).

# Consequences

- A flaky case is seen in the run where it is flaky, and a fall in reliability across merges is
  named with the earlier run and commit.
- Each eval job carries one public artifact more, holding no model text.
- With a shorter retention the comparison quietly has fewer baselines; it never fails.

Sources: [Feature 008 plan](../../specs/008-eval-variance/plan.md).
