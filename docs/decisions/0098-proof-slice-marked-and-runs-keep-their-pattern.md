---
type: Decision
title: "ADR 0098: A proof slice is marked, and continued runs keep their pattern"
description: A proof slice is described `Proof of <captured pattern>`, names the captured pattern it proves and is never returned by slice reuse; resume and take-over keep a run's recorded pattern, a replay of a run built from a captured pattern follows the usable rule, and a replay of a proof run is a new proof; a proof reuses no earlier result.
tags: [decisions, catalogue, captured-patterns]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/011-capture-pattern/plan.md
    title: Feature 011 plan, Decisions
  - id: research
    resource: ../../specs/011-capture-pattern/research.md
    title: Feature 011 research, R5 and R6
  - id: spec
    resource: ../../specs/011-capture-pattern/spec.md
    title: Feature 011 specification, Clarifications (analyze round 4)
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-26T01:10:00Z }
---

# Origin

The project owner's answers of 2026-11-26 to `/speckit-analyze` round 4 of feature 011
([specification](../../specs/011-capture-pattern/spec.md), Clarifications), recorded in the
[feature 011 plan](../../specs/011-capture-pattern/plan.md).

Sources: [Feature 011 specification, Clarifications](../../specs/011-capture-pattern/spec.md).

# Context

- A proof slice ([ADR 0097](/decisions/0097-proof-builds-a-slice-made-from-the-captured-copy.md))
  copies its source slice, so `createSlice`'s reuse of an approved slice with the same description
  could hand it back for the source's description.
- Replay builds a new run from the engine's base pattern, so a replay of a run built from a
  captured pattern would call the model agents again.

Sources: [Feature 011 research, R5](../../specs/011-capture-pattern/research.md).

# Decision

- A proof slice is described `Proof of <captured pattern>`, carries `proof_of`, is approved by the
  engine (recorded in its history) and is never returned by slice reuse.
- A proof reuses no earlier run's result, so every check runs under the current method.
- Resume and take-over keep a run's recorded pattern, parameters and source run.
- A replay of a run built from a captured pattern uses it again only while it is usable; a replay
  of a proof run is a new proof.

Sources: [Feature 011 research, R5](../../specs/011-capture-pattern/research.md).

# Consequences

- The developer's slice list shows which slices are proofs.
- A replay never silently falls back to the agents.

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).
