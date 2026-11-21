---
type: Decision
title: "ADR 0072: An operation's request body is kept in the engine store, and its work is rebuilt from its record"
description: Each accepted operation keeps its request body beside its record in the engine store, with the record's retention and erasure, so its workflow's one activity can redo the work after a stop; its end is written to the record, so Temporal holds neither the body nor the outcome. Extends ADR 0068.
tags: [decisions, architecture, api, workflow]
status: draft
decision: accepted
sources:
  - id: owner
    resource: https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np
    title: Project owner's decision, 2026-11-20
  - id: research
    resource: ../../specs/002-durable-execution/research.md
    title: Feature 002 research, R4
  - id: datamodel
    resource: ../../specs/002-durable-execution/data-model.md
    title: Feature 002 data model, Operation record
  - id: adr0068
    resource: /decisions/0068-operations-recorded-then-run-as-workflows.md
    title: ADR 0068
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-20T12:30:00Z }
---

# Origin

Found while briefing feature 002's US2 tests (T022 to T024): [ADR 0068](/decisions/0068-operations-recorded-then-run-as-workflows.md)'s record keeps only the request body's hash, so an operation a stop interrupted could not do its work again. The project owner chose on 2026-11-20 to keep the body in the engine store (research R4, amended).

Sources: [Project owner's decision, 2026-11-20](https://claude.ai/code/session_01P2TmGw3MdjVyoyhGeQK7np), [Feature 002 research, R4](../../specs/002-durable-execution/research.md).

# Context

- An operation's work needs what the request carried: `createSlice` its description, `replaceRules` its rules.
- Feature 002's FR-016 keeps request bodies and other protected content out of Temporal's state.
- ADR 0068's outbox starts, at engine start, the workflow of an operation recorded just before a stop, from its record alone.

Sources: [Feature 002 research, R4](../../specs/002-durable-execution/research.md), [ADR 0068](/decisions/0068-operations-recorded-then-run-as-workflows.md).

# Decision

- The request body is written beside the record (`operations/OP-<n>.body`) before the record, and is removed when the operation's end is written, or when its slice is erased.
- The operation's workflow runs one activity that rebuilds the work from the record and the body; every step of that work is safe to repeat, and a repeat after a stop is not a retry.
- The operation's end (status, result, error) is written to the record once; Temporal holds only the operation's ID, kind, target and its retries.
- The engine answers 503 before writing anything when the durable service does not answer; once the record is written, the operation is accepted, and a workflow that failed to start is started by the engine's outbox.

Sources: [Feature 002 data model, Operation record](../../specs/002-durable-execution/data-model.md).

# Consequences

- An operation interrupted by a stop does its work again to one end, with its retries counted across the stop.
- Ended operations are read from the engine store, without Temporal.
- The engine store holds a request body only until its operation ends, in the runtime plane.

Sources: [Feature 002 data model, Operation record](../../specs/002-durable-execution/data-model.md).
