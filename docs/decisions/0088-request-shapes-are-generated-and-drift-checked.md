---
type: Decision
title: "ADR 0088: Request shapes are generated and drift-checked"
description: The generate procedure derives each operation's request shape from the approved contract into gen/request_shapes.go, applied by one generic engine in the template, and the generate step's check RequestShapesGenerated proves the file matches the contract.
tags: [decisions, catalogue, codegen, checks]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/010-strict-requests/plan.md
    title: Feature 010 plan, Decisions
  - id: research
    resource: ../../specs/010-strict-requests/research.md
    title: Feature 010 research, R2, R3
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-25T12:00:00Z }
---

# Origin

The plan of [feature 010](../../specs/010-strict-requests/plan.md), applying
[ADR 0086](/decisions/0086-declared-values-file-has-its-own-drift-check.md)'s reasoning to a
second file the procedure writes.

Sources: [Feature 010 plan, Decisions](../../specs/010-strict-requests/plan.md).

# Context

- No mature component both checks a body's shape and removes the members the contract does not
  declare: kin-openapi's request filter validates every keyword and removes nothing; a JSON Schema
  validator removes nothing; `encoding/json/v2` needs `GOEXPERIMENT` in Go 1.26.
- `go generate` does not write a file the procedure writes itself, so GenerationRepeatable cannot
  see it drift.

Sources: [Feature 010 research, R2, R3](../../specs/010-strict-requests/research.md).

# Decision

- `engine/checks` owns `RequestShapes` and `RenderRequestShapes`; the generate procedure
  writes `gen/request_shapes.go` with them.
- The template's `gen/requests.go` is the one generic engine that applies a shape, standard
  library only.
- The generate step gains RequestShapesGenerated: the file equals the rendering of the approved
  contract, byte for byte.

Sources: [Feature 010 research, R2, R3](../../specs/010-strict-requests/research.md).

# Consequences

- The delivered project gains no module.
- A stale or edited `gen/request_shapes.go` fails the generate step; GeneratedCodeUnchanged keeps
  it unchanged in every later step.

Sources: [Feature 010 plan](../../specs/010-strict-requests/plan.md).
