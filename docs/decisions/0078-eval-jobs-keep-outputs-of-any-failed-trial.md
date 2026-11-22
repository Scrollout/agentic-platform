---
type: Decision
title: "ADR 0078: An eval job keeps its outputs when any trial failed"
description: Supersedes ADR 0050's "a passing job uploads nothing". An eval job uploads its scanned report folder, kept 7 days, when the job failed or any trial failed, a quarantined case's included; a job with no failed trial uploads nothing.
tags: [decisions, ci, evals]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/005-analyst-reliability/plan.md
    title: Feature 005 plan, Decisions
  - id: research
    resource: ../../specs/005-analyst-reliability/research.md
    title: Feature 005 research, R1 and R2
  - id: adr0050
    resource: /decisions/0050-failed-eval-jobs-keep-their-outputs.md
    title: ADR 0050
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-22T12:00:00Z }
---

# Origin

The plan of [feature 005](../../specs/005-analyst-reliability/plan.md) (backlog T185, owner
2026-11-22): two analyst cases failed one trial each and were quarantined, and no failing answer
could be read.

Sources: [Feature 005 plan, Decisions](../../specs/005-analyst-reliability/plan.md).

# Context

- A quarantined case's failure does not fail the job, so under
  [ADR 0050](/decisions/0050-failed-eval-jobs-keep-their-outputs.md) its outputs are never kept,
  though they are what its open task must diagnose.
- The upload had also never worked: its path held a `..` segment, which the upload action
  refuses.

Sources: [Feature 005 research, R1 and R2](../../specs/005-analyst-reliability/research.md).

# Decision

- An eval job uploads its report folder, kept 7 days, when the job failed or any trial in any of
  its reports failed, a quarantined case's included, or a report cannot be read.
- A job with no failed trial uploads nothing.
- The credential scan runs first, as in ADR 0050; on a match nothing is uploaded and the job
  fails.
- ADR 0050's content, retention and storage reasoning stands.

Sources: [Feature 005 research, R2](../../specs/005-analyst-reliability/research.md); [ADR 0050](/decisions/0050-failed-eval-jobs-keep-their-outputs.md).

# Consequences

- A flaky case's failing answer can be read for a week, so its quarantine task can find the cause.
- A green job can now carry an artifact; its presence says some trial failed.

Sources: [Feature 005 plan](../../specs/005-analyst-reliability/plan.md).
