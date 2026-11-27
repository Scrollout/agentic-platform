---
type: Decision
title: "ADR 0106: A trial set whose operation fails ends failed"
description: When a trial set's run-trials operation fails, its running trial is cancelled and the set ends in the new state failed with the operation's reason; TrialSetState gains failed in the engine's preview contract in place, an owner-approved exception to Principle XV.
tags: [decisions, apis, variance]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/012-run-variance/plan.md
    title: Feature 012 plan, Decisions and Complexity Tracking
  - id: spec
    resource: ../../specs/012-run-variance/spec.md
    title: Feature 012 spec, Clarifications 2026-11-28
  - id: adr-0105
    resource: /decisions/0105-preview-contract-gains-run-trials-in-place.md
    title: "ADR 0105: The engine's preview contract gains the operation kind run-trials in place"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-28T08:00:00Z }
---

# Origin

The project owner's answer of 2026-11-28 to the US1 fresh review's blocking finding B1, recorded in
the [feature 012 spec](../../specs/012-run-variance/spec.md), Clarifications, and the
[plan](../../specs/012-run-variance/plan.md), Decisions and Complexity Tracking.

Sources: [Feature 012 spec](../../specs/012-run-variance/spec.md).

# Context

- A set's work is one operation; the operation ends `failed` when its job fails for good (a trial
  cannot be started) or its retries run out.
- Nothing ended the set then: its record stayed `running` and its running trial went on with no
  one following it, against "every set ends in exactly one state" (Principle X).
- The pinned oasdiff reports an added response enum value as breaking
  ([ADR 0105](/decisions/0105-preview-contract-gains-run-trials-in-place.md)).

Sources: [Feature 012 plan](../../specs/012-run-variance/plan.md).

# Decision

- When a set's operation ends failed, its running trial is cancelled and the set ends `failed`,
  with `failed_because` holding the operation's problem detail; no further trial starts.
- `TrialSetState` in the engine contract's `2026-11-12-preview` gains `failed` in place, and
  `TrialSet` gains the optional `failed_because`.

Sources: [Feature 012 spec](../../specs/012-run-variance/spec.md).

# Consequences

- A client built from the old contract that reads such a set sees an unknown state value.
- The terminal client's exit code for a failed set's operation (4, with the problem) is unchanged.

Sources: [Feature 012 plan](../../specs/012-run-variance/plan.md).
