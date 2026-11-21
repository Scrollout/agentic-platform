---
type: Decision
title: "ADR 0068: An operation is recorded before it is acknowledged, then runs as a workflow"
description: Each state-changing request writes its operation record (a durable `OP-<n>` ID, the Idempotency-Key's hash, the body's hash) to the engine store before the 202, then runs as a Temporal workflow with that ID; the record and key are kept 24 h after the operation ends.
tags: [decisions, architecture, api, workflow]
status: draft
decision: accepted
sources:
  - id: research
    resource: ../../specs/002-durable-execution/research.md
    title: Feature 002 research
  - id: plan
    resource: ../../specs/002-durable-execution/plan.md
    title: Feature 002 plan, Decisions
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-19T09:00:00Z }
---

# Origin

Feature 002's [plan](../../specs/002-durable-execution/plan.md) adopts Temporal for the engine's durable execution, as [ADR 0056](/decisions/0056-durable-execution-custom-for-the-mvp-temporal-after.md) decided. It settles feature 002's US2 (research R4).

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md), [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).

# Context

- Feature 001's operations lived in memory: a restart lost them, and their IDs started again.
- Temporal protects a workflow ID only for the retention period, and visibility queries are eventually consistent.
- The `OP-<n>` form and the 24 h Idempotency-Key window are facts clients see.

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md).

# Decision

- The record is written atomically, with an ID from a durable sequence, before the engine answers 202; a failure before that answers 503 `Unavailable` with `Retry-After`.
- The engine then starts the workflow with the operation's ID on the task queue `operations`; at start it starts the workflow of every record that has none (an outbox).
- Status, progress and cancellation come from the workflow; the record and its workflow are removed 24 h after the operation ends.

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md), [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).

# Consequences

- Operations and their keys survive a stop, and an ID is never given twice.
- The engine contract gains a 503 response on its state-changing operations, an additive change.

Sources: [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).
