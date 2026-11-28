---
type: Decision
title: "ADR 0106: A trial set whose operation fails ends failed"
description: When a trial set's run-trials operation fails, its running trial is cancelled and the set ends in the new state failed with the operation's reason; the set's schemas are new in feature 012, so the contract change is additive.
tags: [decisions, apis, variance]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/012-run-variance/plan.md
    title: Feature 012 plan, Decisions
  - id: spec
    resource: ../../specs/012-run-variance/spec.md
    title: Feature 012 spec, Clarifications 2026-11-28
  - id: research
    resource: ../../specs/012-run-variance/research.md
    title: Feature 012 research, R9
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-28T08:00:00Z }
---

# Origin

The project owner's answer of 2026-11-28 to the US1 fresh review's blocking finding B1, recorded in
the [feature 012 spec](../../specs/012-run-variance/spec.md), Clarifications, and the
[plan](../../specs/012-run-variance/plan.md), Decisions.

Sources: [Feature 012 spec](../../specs/012-run-variance/spec.md).

# Context

- A set's work is one operation; the operation ends `failed` when its job fails for good (a trial
  cannot be started) or its retries run out.
- Nothing ended the set then: its record stayed `running` and its running trial went on with no
  one following it, against "every set ends in exactly one state" (Principle X).
- The process that ends an operation (the generic Operation workflow) and the set's record are
  two stores, so the set's end must be repairable after a crash between the two writes.

Sources: [Feature 012 plan](../../specs/012-run-variance/plan.md).

# Decision

- When a set's operation ends failed, the set ends `failed`, with `failed_because` holding the
  operation's problem `detail` as every operation exposes it; no further trial starts; its running
  trial is cancelled.
- The operation's end comes first; the set is ended right after it. A start, a delete or the
  engine's start that finds a `running` set whose operation ended failed ends it the same way, once;
  a read shows such a set `failed` and writes nothing (owner 2026-11-28).
- `TrialSetState` (new in feature 012) gains `failed`, and `TrialSet` the optional
  `failed_because`; additive, no exception to Principle XV.

Sources: [Feature 012 research, R9](../../specs/012-run-variance/research.md).

# Consequences

- Every set ends in exactly one recorded state even when its work fails (Principle X).
- The terminal client ends with 4 and the reason for a `failed` set, read from the set.

Sources: [Feature 012 plan](../../specs/012-run-variance/plan.md).
