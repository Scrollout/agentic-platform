---
type: Decision
title: "ADR 0059: Run records are JSON Lines files and artifacts a content-addressed store"
description: The run store is append-only JSON Lines run records and artifacts stored under their SHA-256 in a named volume, with a reuse key per step; the MVP keeps it, and ADR 0056 moves run history to Temporal after the MVP.
tags: [decisions, engine, run-store]
status: draft
decision: accepted
sources:
  - id: research
    resource: ../../specs/001-prose-to-accepted-slice/research.md
    title: Feature 001 research, D8
  - id: plan
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 plan, Decisions
  - id: adr-0056
    resource: /decisions/0056-durable-execution-custom-for-the-mvp-temporal-after.md
    title: ADR 0056, The MVP keeps its own durable execution
generated: { by: claude-code/claude-sonnet-5-5, at: 2026-11-17T22:00:00Z }
---

# Origin

Feature 001 chose the run store's format in its research as D8, with a follow-up to record the
store format in a decision record (task T060). The
[plan](../../specs/001-prose-to-accepted-slice/plan.md) lists it among its decisions.
[ADR 0056](/decisions/0056-durable-execution-custom-for-the-mvp-temporal-after.md) (research D56)
decides that the MVP keeps this store and that Temporal takes over run history after the MVP; this
record agrees with it.

Sources: [Feature 001 research, D8](../../specs/001-prose-to-accepted-slice/research.md), [Feature 001 plan, Decisions](../../specs/001-prose-to-accepted-slice/plan.md).

# Context

- FR-018 to FR-020 and SC-005, SC-006 need every event recorded, an interrupted run resumable
  without repeating accepted steps, and a replay of a finished run that gives identical artifacts.
- Agent output differs between runs while fixed inputs give identical generation, so reuse is what
  makes a replay identical.
- SQLite and a durable-execution engine were the alternatives. SQLite is not needed for one
  writer. The durable-execution engine was first set aside without the evaluation Principle VI
  requires, until D56 evaluated it.

Sources: [Feature 001 research, D8](../../specs/001-prose-to-accepted-slice/research.md).

# Decision

- Run records are JSON Lines files. Artifacts are stored under their SHA-256. Both live in a named
  volume.
- Every event records the run, step, attempt, model, agent definition digest, pattern version,
  input hashes, reuse key, and the goal, rule or example it concerns.
- A step's reuse key is the hash of the pattern version, step, input hashes, agent definition
  digest and model id. For a review round it also includes the round.
- An open review round's result is kept and reused like an accepted one (owner, 2026-11-15).

Sources: [Feature 001 research, D8](../../specs/001-prose-to-accepted-slice/research.md).

# Consequences

- A replay reuses accepted results by key, so it is identical.
- The plan's threat model relies on the store being append-only and content addressed by hash,
  with erasure on delete.
- After the MVP, run history moves to Temporal and artifacts keep travelling by hash. The record
  format is then superseded by a new decision, not rewritten here
  ([ADR 0056](/decisions/0056-durable-execution-custom-for-the-mvp-temporal-after.md)).

Sources: [Feature 001 plan, Decisions](../../specs/001-prose-to-accepted-slice/plan.md).
