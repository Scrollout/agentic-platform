---
type: Decision
title: "ADR 0107: A run's events carry usage only for steps that reach a model"
description: A step whose worker is a procedure reaches no model, so the durable engine records no usage on its events and its tokens and cost read as not available, never as zero.
tags: [decisions, data, variance]
status: draft
decision: accepted
sources:
  - id: spec
    resource: ../../specs/012-run-variance/spec.md
    title: Feature 012 spec, Clarifications 2026-11-28
  - id: research
    resource: ../../specs/012-run-variance/research.md
    title: Feature 012 research, R6
  - id: data-model-001
    resource: ../../specs/001-prose-to-accepted-slice/data-model.md
    title: Feature 001 data model, Run record
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-28T12:00:00Z }
---

# Origin

The project owner's answer of 2026-11-28 to the US2 fresh review's blocking finding B3, recorded in
the [feature 012 spec](../../specs/012-run-variance/spec.md), Clarifications, and tasks T044 to T046.

Sources: [Feature 012 spec](../../specs/012-run-variance/spec.md).

# Context

- Feature 001's run record puts `usage` (tokens, cost) on every `AttemptMade`, and on the
  `StepRejected` of an attempt that made none, so a run's use can be summed from its record.
- A procedure step (a `procedure:` worker, such as `contract-check` or `generate`) reaches no model,
  yet recorded a usage of 0 tokens and $0; a trial set then showed its tokens and cost as a spread
  of zeros, where the spec says a fixed procedure's tokens are not available.

Sources: [Feature 001 data model](../../specs/001-prose-to-accepted-slice/data-model.md).

# Decision

- The durable engine records no `usage` on the events of a step whose worker is a procedure, as
  it already did for `fill` steps; the run's budget use is unchanged.
- An attempt is counted from its `AttemptMade`, or from a `StepRejected` with no `AttemptMade` of
  the same step, round and attempt, whether or not it carries usage.
- Records written before the change are not rewritten. The engine's in-memory driver, which only
  tests start, keeps a zero usage on procedure steps (owner 2026-11-28).
- This qualifies feature 001's rule: `usage` is on every attempt of a step that reaches a model.
  Feature 001's data model is that feature's finished record and is not edited; this decision is
  the place a reader finds the exception.

Sources: [Feature 012 research, R6](../../specs/012-run-variance/research.md).

# Consequences

- Tokens and cost of a procedure step read as not available (`-`), never as 0.
- A reader of the run record must treat a missing `usage` as "no model reached", not as an error.

Sources: [Feature 012 spec](../../specs/012-run-variance/spec.md).
