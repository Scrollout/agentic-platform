---
type: Decision
title: "ADR 0073: An Idempotency-Key is held 24 h from acceptance; an ended operation stays followable 24 h after its end"
description: An operation record holds its Idempotency-Key for 24 h from its acceptance, as the contract's key window says, while the record itself stays followable until 24 h after the operation ends. Corrects the key clause of ADR 0068.
tags: [decisions, architecture, api]
status: draft
decision: accepted
sources:
  - id: contract
    resource: ../../src/engine/api/preview/2026-11-12-preview/openapi.yaml
    title: Engine API contract, x-limits.idempotency-key-retention-hours
  - id: datamodel
    resource: ../../specs/002-durable-execution/data-model.md
    title: Feature 002 data model, Operation record
  - id: plan
    resource: ../../specs/002-durable-execution/plan.md
    title: Feature 002 plan, Decisions
  - id: adr0068
    resource: /decisions/0068-operations-recorded-then-run-as-workflows.md
    title: ADR 0068
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-20T16:00:00Z }
---

# Origin

Found in feature 002's T025 by `TestIdempotencyKeyIsKeptFor24Hours`, and recorded in the [data model](../../specs/002-durable-execution/data-model.md) and the [plan](../../specs/002-durable-execution/plan.md)'s Decisions table after analyze round 19 (2026-11-20). [ADR 0068](/decisions/0068-operations-recorded-then-run-as-workflows.md) says the record and the key are both kept 24 h after the operation ends. The contract counts the key's window from the first request.

Sources: [Feature 002 data model, Operation record](../../specs/002-durable-execution/data-model.md), [ADR 0068](/decisions/0068-operations-recorded-then-run-as-workflows.md).

# Context

- The contract's `x-limits.idempotency-key-retention-hours` (24) is counted "from the first request", and the `Idempotency-Key` parameter is "Kept for" that limit.
- `getOperation` answers an ended operation until the retention after its end.

Sources: [Engine API contract](../../src/engine/api/preview/2026-11-12-preview/openapi.yaml).

# Decision

- A record holds its Idempotency-Key for 24 h from its `accepted_at`. After that, the same key starts a new operation, with a new ID.
- The record, and the operation it describes, stay followable until 24 h after its `ended_at`.
- This replaces only ADR 0068's clause that the key is kept 24 h after the operation ends. The rest of ADR 0068 stands.

Sources: [Feature 002 data model, Operation record](../../specs/002-durable-execution/data-model.md).

# Consequences

- The key window clients see matches the contract, including for an operation that runs longer than 24 h.
- A key can be free while its first operation is still followable.

Sources: [Engine API contract](../../src/engine/api/preview/2026-11-12-preview/openapi.yaml).
