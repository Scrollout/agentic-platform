---
type: Decision
title: "ADR 0104: A trial that waits for the developer is handed over by its set"
description: When a run of a trial set runs out of attempts, review rounds or budget, the set accepts its hand-over, counts it as not done and starts the next trial, instead of waiting up to 14 days.
tags: [decisions, runs, variance]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/012-run-variance/plan.md
    title: Feature 012 plan, Decisions
  - id: research
    resource: ../../specs/012-run-variance/research.md
    title: Feature 012 research, R4
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-27T18:00:00Z }
---

# Origin

The project owner's answer of 2026-11-27 to a question found while planning feature 012, recorded
in the [feature 012 plan](../../specs/012-run-variance/plan.md), Decisions.

Sources: [Feature 012 plan](../../specs/012-run-variance/plan.md).

# Context

- A run that runs out of attempts, review rounds or budget waits for the developer for up to 14
  days, then expires.
- In a trial set, that trial's outcome for the result is already known: not done.

Sources: [Feature 012 research, R4](../../specs/012-run-variance/research.md).

# Decision

- The set accepts the hand-over of a trial that waits for the developer, as `acceptHandOver`
  does; the trial ends `handed-over`, counts as not done, and the next trial starts.
- An interrupted trial is waited for, within the set's 24 hours.

Sources: [Feature 012 research, R4](../../specs/012-run-variance/research.md).

# Consequences

- A set finishes in hours, not days; the handed-over trial's project stays for the developer.
- A run outside a trial set waits for the developer as before.

Sources: [Feature 012 plan](../../specs/012-run-variance/plan.md).
