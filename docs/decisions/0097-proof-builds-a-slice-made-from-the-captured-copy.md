---
type: Decision
title: "ADR 0097: A proof builds a slice made from the captured pattern's copy"
description: Refined by ADR 0099 (a proof compares only the filled outputs with the template). Supersedes ADR 0096. Each captured pattern has one proof slice, an approved slice made from its copy of the source slice; a proof is an ordinary run of that slice under the captured pattern, so step workers, run records, the launcher and the API stay keyed by slice; the captured pattern keeps the expected output hashes.
tags: [decisions, catalogue, captured-patterns]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/011-capture-pattern/plan.md
    title: Feature 011 plan, Decisions
  - id: research
    resource: ../../specs/011-capture-pattern/research.md
    title: Feature 011 research, S17, S18 and R5
  - id: spec
    resource: ../../specs/011-capture-pattern/spec.md
    title: Feature 011 specification, Clarifications (analyze round 3)
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-26T03:00:00Z }
---

# Origin

The project owner's answer of 2026-11-26 to `/speckit-analyze` round 3 of feature 011
([specification](../../specs/011-capture-pattern/spec.md), Clarifications), recorded in the
[feature 011 plan](../../specs/011-capture-pattern/plan.md).

Sources: [Feature 011 specification, Clarifications](../../specs/011-capture-pattern/spec.md).

# Context

> Refined by [ADR 0099](/decisions/0099-proof-compares-filled-outputs-and-paths-are-filled.md):
> no generated-code hash is kept; a proof compares only the filled outputs with the template.

- [ADR 0096](/decisions/0096-proof-step-workers-keyed-by-captured-pattern.md) gave a proof no
  slice and keyed its step workers by the captured pattern.
- Analyze round 3 found that runs, their events, findings, reuse lookups, the one-active-run rule,
  resume, replay and take-over are all stored and found by slice; `Run.slice` and `Worker.slice`
  are required in the engine contract; the launcher refuses any run without a slice; and after
  the source slice's erasure no expected hashes remained to compare a proof with.

Sources: [Feature 011 research, S17 and S18](../../specs/011-capture-pattern/research.md).

# Decision

- Each captured pattern has one proof slice: an approved slice made from its copy of the source
  slice, created by the first proof request and again after it was erased.
- A proof is a run of the proof slice under the captured pattern with the source values, an
  ordinary run with a slice and `proof: true`; `ap prove <captured pattern> <project>` asks the
  engine for the proof slice, then builds it as `ap build` does.
- At capture the captured pattern keeps the source run's `generated-code`, `tests`, `code` and
  `witnesses` hashes, held as artifacts, so a proof compares with them after the source slice is
  erased.

Sources: [Feature 011 research, R2, R5 and R7](../../specs/011-capture-pattern/research.md).

# Alternatives

- No slice for a proof (ADR 0096): a second home for runs in the store, the workflows, the API
  and the launcher, and a breaking contract change.
- The source slice: no proof after it is erased, against ADR 0095.

Sources: [Feature 011 research, R5](../../specs/011-capture-pattern/research.md).

# Consequences

- The contract change stays additive; step workers, run records and the launcher are unchanged.
- A proof slice appears in the developer's slice list; erasing it is allowed, and the next proof
  makes a new one.

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).
