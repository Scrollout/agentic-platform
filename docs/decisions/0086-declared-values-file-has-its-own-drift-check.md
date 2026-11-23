---
type: Decision
title: "ADR 0086: The declared values file has its own drift check"
description: The generate step's check DeclaredValuesGenerated proves gen/declared.go is byte for byte the rendering of the approved contract's declared values, since go generate does not write that file and GenerationRepeatable cannot see it drift.
tags: [decisions, checks, catalogue, codegen]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/009-no-hard-coded-values/plan.md
    title: Feature 009 plan, Decisions
  - id: research
    resource: ../../specs/009-no-hard-coded-values/research.md
    title: Feature 009 research, R7
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-23T21:00:00Z }
---

# Origin

The plan of [feature 009](../../specs/009-no-hard-coded-values/plan.md), after its analyze round 1
(finding F12), adding to [ADR 0084](/decisions/0084-declared-values-live-in-generated-code-and-are-never-repeated.md).

Sources: [Feature 009 plan, Decisions](../../specs/009-no-hard-coded-values/plan.md).

# Context

- The generate procedure writes `gen/declared.go` itself; `go generate` does not, so
  GenerationRepeatable passes whatever the file holds.
- Principle XVII requires a drift check proving generated code matches its contract.

Sources: [Feature 009 research, R7](../../specs/009-no-hard-coded-values/research.md).

# Decision

- The generate step gains DeclaredValuesGenerated: `gen/declared.go` equals, byte for byte, the
  rendering of the approved contract's declared values by the function the procedure uses.
- GenerationRepeatable stays as it is.

Sources: [Feature 009 research, R7](../../specs/009-no-hard-coded-values/research.md).

# Consequences

- A stale or edited `gen/declared.go` fails the generate step; GeneratedCodeUnchanged keeps it
  unchanged in every later step.

Sources: [Feature 009 plan](../../specs/009-no-hard-coded-values/plan.md).
