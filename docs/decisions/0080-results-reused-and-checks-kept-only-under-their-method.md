---
type: Decision
title: "ADR 0080: Results are reused, and slice checks kept, only under the method that produced them"
description: Supersedes ADR 0059's reuse-key clause. The reuse key also covers the method identity, every step outcome event names it, a durable run takes it from the engine at each lookup, and a slice's checks run again when they were produced under another method.
tags: [decisions, engine, reuse, run-store]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/006-method-record/plan.md
    title: Feature 006 plan, Decisions
  - id: research
    resource: ../../specs/006-method-record/research.md
    title: Feature 006 research, R3 and R4
  - id: adr-0059
    resource: /decisions/0059-run-store-is-json-lines-and-a-content-addressed-store.md
    title: ADR 0059, Run records are JSON Lines files and artifacts a content-addressed store
  - id: adr-0079
    resource: /decisions/0079-method-identity-is-a-digest-of-declared-sources.md
    title: ADR 0079, The method identity
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-22T15:00:00Z }
---

# Origin

The plan of [feature 006](../../specs/006-method-record/plan.md) (backlog T136 with T144).

Sources: [Feature 006 plan, Decisions](../../specs/006-method-record/plan.md).

# Context

- [ADR 0059](/decisions/0059-run-store-is-json-lines-and-a-content-addressed-store.md) keys a
  step's result by pattern version, step, input hashes, agent definition digest, model id and, for
  a review round, its round. After an upgrade of the checks a run reused results the new checks
  never judged.
- A slice's stored check results were restored as they were, whatever pattern judged them.
- A durable run's workflow input is recorded at its start, so a value taken from it does not
  follow an upgrade while the run resumes.

Sources: [Feature 006 research, R3 and R4](../../specs/006-method-record/research.md).

# Decision

- The reuse key also covers the method identity
  ([ADR 0079](/decisions/0079-method-identity-is-a-digest-of-declared-sources.md)), written after
  the round, so a key without one is unchanged; the engine always has one, so no key recorded
  before is matched.
- Every event that carries a reuse key (`AttemptMade`, `StepAccepted`, `ResultReused`, and the
  `StepRejected` that leaves a review round open) names the identity its key covers.
- A durable run's key is computed by the engine at each lookup, with its current identity, never
  from the workflow's input; the workflow takes the key the lookup returns.
- A slice record names the identity its checks ran under; on first use under another identity, or
  with none, every check runs again, with the usual approval rule, before the slice is shown; a
  check that could not run leaves the slice to be checked again on its next read.
- The rest of ADR 0059 stands.

Sources: [Feature 006 research, R3 and R4](../../specs/006-method-record/research.md); [ADR 0059](/decisions/0059-run-store-is-json-lines-and-a-content-addressed-store.md).

# Consequences

- After an upgrade nothing judged by the old method is reused or shown as current; the record
  says which method judged each step, and a reader can recompute its key.
- A run resumed across an upgrade judges its remaining steps under the new method; a step whose
  lookup was recorded before the upgrade keeps the old key, so its result is never reused, though
  its record names the old method.
- A step whose lookup failed keeps a key without a method: its result is never reused.
- The first read of each slice after an upgrade waits for its checks.

Sources: [Feature 006 plan](../../specs/006-method-record/plan.md).
