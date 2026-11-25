---
type: Decision
title: "ADR 0093: A captured pattern is used only under its proven method"
description: Refined by ADR 0097 (a proof is a build of the captured pattern's proof slice, compared with the expected hashes it keeps). Superseded in part by ADR 0095 (a proof builds from the captured pattern's copy of the slice and compares generated files; usable needs the latest proof to have passed). A captured pattern may be approved and used only while the method of its latest passing proof is the engine's current one; the proof is a run of the source slice whose files equal the accepted ones; startRun picks the newest usable fitting captured pattern unless the run names another.
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
    title: Feature 011 specification, Clarifications
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-26T01:10:00Z }
---

# Origin

The [feature 011 plan](../../specs/011-capture-pattern/plan.md), Decisions, from the project
owner's answers of 2026-11-25 ([specification](../../specs/011-capture-pattern/spec.md),
Clarifications).

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).

# Context

> Superseded in part by [ADR 0095](/decisions/0095-captured-patterns-never-change-values-keep-their-kind.md): a proof builds from the captured pattern's copy of the slice and compares generated files; usable needs the latest proof to have passed.

- A solution proven under old checks must not be accepted under new ones without being judged
  (features 006 and 011).
- Principle VII: the rule that chooses between providers of one kind must be specified.

Sources: [Feature 011 research, R5 and R6](../../specs/011-capture-pattern/research.md).

# Decision

- A proof is an ordinary run of the source slice under the captured pattern with the source
  values, in a new project; it passes when the run ends done and every filled file equals the
  accepted file. Each proof keeps its run, outcome, failures and method.
- Approval needs a passing proof under the current method; use needs the same, so a method change
  makes an approved captured pattern unusable until the developer proves it again.
- At `startRun` the newest usable fitting captured pattern is used, unless the request names a
  pattern; the answer and `RunStarted` name the pattern and why.

Sources: [Feature 011 research, R5 and R6](../../specs/011-capture-pattern/research.md).

# Consequences

- Every engine upgrade asks for new proofs before captured patterns are used again.
- A captured pattern never overrides the developer's choice for a run.

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).
