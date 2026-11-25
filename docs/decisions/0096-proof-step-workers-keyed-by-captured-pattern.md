---
type: Decision
title: "ADR 0096: A proof's step workers are keyed by its captured pattern"
description: A proof has no slice, so its step workers are started, granted and registered for the captured pattern, as a build's are for its slice; the developer names the proof's project folder, and a proof's run has no slice.
tags: [decisions, catalogue, captured-patterns, step-workers]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/011-capture-pattern/plan.md
    title: Feature 011 plan, Decisions
  - id: research
    resource: ../../specs/011-capture-pattern/research.md
    title: Feature 011 research, S17, R2 and R5
  - id: spec
    resource: ../../specs/011-capture-pattern/spec.md
    title: Feature 011 specification, Clarifications (found at T001)
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-25T21:56:00Z }
---

# Origin

The project owner's answer of 2026-11-25 to a gap found while writing the contract of feature 011
(T001), recorded in the [feature 011 specification](../../specs/011-capture-pattern/spec.md)
(Clarifications) and the [feature 011 plan](../../specs/011-capture-pattern/plan.md).

Sources: [Feature 011 specification, Clarifications](../../specs/011-capture-pattern/spec.md).

# Context

- A proof builds the captured pattern's copy of its source slice, so it works after the slice is
  erased ([ADR 0095](/decisions/0095-captured-patterns-never-change-values-keep-their-kind.md)).
  A proof's run therefore has no slice.
- Step workers are started, granted and registered per slice: a grant names one slice and one
  step ([ADR 0034](/decisions/0034-step-worker-grants-as-files-read-at-each-authentication.md)),
  registrations are kept per slice
  ([ADR 0071](/decisions/0071-step-worker-registrations-kept-in-the-engine-store.md)), and a run
  starts only when every step of its pattern has a live worker for its slice.
- A proof's procedure steps (contract check, generate) need workers; its `fill` steps need none.
  A proof also needs a new project folder.

Sources: [Feature 011 research, S17, R2 and R5](../../specs/011-capture-pattern/research.md).

# Decision

- A grant and a registration name either a slice or a captured pattern, never both. A proof's
  step workers are those of its captured pattern.
- `ap build-workers <captured pattern> <project>` starts and registers them as it does for a
  slice, and `ap prove <captured pattern> <project>` runs it first, as `ap build` does.
- The developer names the proof's new project folder, refused as for a build.
- A proof's run has no slice and is marked as a proof.

Sources: [Feature 011 research, R2, R5 and R7](../../specs/011-capture-pattern/research.md).

# Alternatives

- A new slice made from the captured copy for each proof: proof slices would appear in the
  developer's slice list and could be erased, and a proof would need two client calls.
- The source slice's workers: no proof after the slice is erased, against ADR 0095.

Sources: [Feature 011 research, R5](../../specs/011-capture-pattern/research.md).

# Consequences

- The step worker registry, the grant files and the launcher learn a second kind of key.
- A proof runs on the same step containers and checks as a build, so it judges the same method.

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).
