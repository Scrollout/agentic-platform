---
type: Decision
title: "ADR 0066: The run record stays the engine's; Temporal keeps only execution state"
description: The engine's run record (events.jsonl and artifacts by hash) stays the one record inspect, compare, replay and the run page read, with its schema unchanged; Temporal holds only a run's execution state, and every event is written by an idempotent activity.
tags: [decisions, architecture, workflow, data]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's answers, 2026-11-19
  - id: research
    resource: ../../specs/002-durable-execution/research.md
    title: Feature 002 research
  - id: plan
    resource: ../../specs/002-durable-execution/plan.md
    title: Feature 002 plan, Decisions
  - id: adr-0059
    resource: /decisions/0059-run-store-is-json-lines-and-a-content-addressed-store.md
    title: "ADR 0059: run store"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-19T09:00:00Z }
---

# Origin

Feature 002's [plan](../../specs/002-durable-execution/plan.md) adopts Temporal for the engine's durable execution, as [ADR 0056](/decisions/0056-durable-execution-custom-for-the-mvp-temporal-after.md) decided. The project owner chose to keep the record on 2026-11-19 (research R2).

Sources: [Project owner's answers, 2026-11-19](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np), [Feature 002 research](../../specs/002-durable-execution/research.md), [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).

# Context

- The run record's schema is a published contract, and feature 001's ended runs must stay readable.
- A Temporal history is removed when its namespace retention ends, and its size is limited (2 MB per payload, 50 MB per history).

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md).

# Decision

- The record stays in the engine store ([ADR 0059](/decisions/0059-run-store-is-json-lines-and-a-content-addressed-store.md)); the run's workflow appends each event through the activity `RecordEvent`, which the store accepts once per sequence number.
- Temporal's state holds IDs, hashes, numbers and short reasons only, never a prompt, completion, file or finding text.
- A run's status and outcome are read from its record, as before.

Sources: [Feature 002 research](../../specs/002-durable-execution/research.md), [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).

# Consequences

- The record and the execution state are two stores; the record is the one that lasts, and a run's workflow is the only writer of its events.
- Temporal's retention can stay short (24 h), which keeps protected content out of it and its tables small.

Sources: [Feature 002 plan, Decisions](../../specs/002-durable-execution/plan.md).
