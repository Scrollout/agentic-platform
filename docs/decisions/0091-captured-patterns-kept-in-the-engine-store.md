---
type: Decision
title: "ADR 0091: Captured patterns are kept in the engine store"
description: Superseded in part by ADR 0095 (its version is always 1.0.0 and a change is a new capture; one operation at a time). Captured patterns are the developer's data in the engine store, managed through the engine API's /captured-patterns resource, and hold their template artifacts apart from the source slice.
tags: [decisions, catalogue, captured-patterns]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/011-capture-pattern/plan.md
    title: Feature 011 plan, Decisions
  - id: research
    resource: ../../specs/011-capture-pattern/research.md
    title: Feature 011 research, R3 and R7
  - id: spec
    resource: ../../specs/011-capture-pattern/spec.md
    title: Feature 011 specification, Clarifications
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-25T23:00:00Z }
---

# Origin

The [feature 011 plan](../../specs/011-capture-pattern/plan.md), Decisions, from the project
owner's answers of 2026-11-25 ([specification](../../specs/011-capture-pattern/spec.md),
Clarifications).

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).

# Context

> Superseded in part by [ADR 0095](/decisions/0095-captured-patterns-never-change-values-keep-their-kind.md): its version is always 1.0.0 and a change is a new capture; one operation at a time.

- Principle III: what users manage enters through the API and lives in the owning component's
  store; `src/catalogue/` holds the reviewed base patterns and is part of the method identity.

Sources: [Feature 011 research, R3 and R7](../../specs/011-capture-pattern/research.md).

# Decision

- The engine store keeps each captured pattern: its declaration, the source slice's approved
  parts, its parameters, its template's artifact hashes, its proofs, its approval and its state.
- The template artifacts are held by the captured pattern as well as by the source slice, so
  erasing the slice keeps them; erasing a captured pattern is its own confirmed operation.
- ID `<base id>-<name>`, version `1.0.0` at capture, a new version for any change after approval.
- Managed through `/captured-patterns` (capture, list, read, parameters, proofs, approval,
  withdrawal, erasure), every state change an operation.

Sources: [Feature 011 research, R3 and R7](../../specs/011-capture-pattern/research.md).

# Consequences

- Captured patterns do not change the method identity.
- A hosted version must add tenant scope, as for slices.

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).
