---
type: Decision
title: "ADR 0102: A trial set runs fresh builds of a slice and derives its result from their records"
description: A trial set is one engine operation that builds an approved slice k times, one run after another, each reusing no earlier result, and computes how much they vary from the runs' records on each read.
tags: [decisions, runs, variance]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/012-run-variance/plan.md
    title: Feature 012 plan, Decisions
  - id: research
    resource: ../../specs/012-run-variance/research.md
    title: Feature 012 research, R1, R2, R5, R6
  - id: adr-0068
    resource: /decisions/0068-operations-recorded-then-run-as-workflows.md
    title: "ADR 0068: Operations are recorded, then run as workflows"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-27T18:00:00Z }
---

# Origin

The [feature 012 plan](../../specs/012-run-variance/plan.md), Decisions (need N-054). The project
owner chose fresh trials, k from 1 to 5 and a $2 budget per set on 2026-11-27 (spec,
Clarifications).

Sources: [Feature 012 plan](../../specs/012-run-variance/plan.md).

# Context

- Under one method a second run of a slice reuses every result of the first, so a developer's runs
  do not vary and the model's reliability on the slice is invisible.
- Each run's record already holds every step's attempts, rounds, times, model use and output
  hashes; the Operation workflow already runs one job durably with cancellation.

Sources: [Feature 012 research, R1 and R6](../../specs/012-run-variance/research.md).

# Decision

- A trial set is one operation of kind `run-trials`. Its job starts k ordinary runs of the slice
  one after another, each with `NoReuse` into its own project, and writes each trial's run ID into
  the set's record before starting it.
- A trial starts only while the set's cost so far plus one run's budget stays within the set's
  ($2); a set still running after 24 hours cancels its trial and stops. The limits are declared in
  the engine contract.
- The result (done out of ended, unstable, per step and trial lowest, median and highest of
  attempts, review rounds, seconds, tokens and cost, which steps' files differ, the change since the
  slice's previous set) is computed from the trials' run records on each read and never stored.

Sources: [Feature 012 research, R1, R2, R5, R6](../../specs/012-run-variance/research.md).

# Consequences

- A set costs up to $2 of model use; nothing starts one but the developer.
- Trials never replace a kept result, so later ordinary builds reuse what they did before.
- The result always agrees with the run records, which stay the one home of each measure.

Sources: [Feature 012 plan](../../specs/012-run-variance/plan.md).
