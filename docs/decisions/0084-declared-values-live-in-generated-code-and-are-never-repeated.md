---
type: Decision
title: "ADR 0084: Declared values live in generated code and are never repeated"
description: Every value the approved contract declares (a length, range or size limit, a default, a const or an enum value) has its Go home in the generated code, and the code step is rejected when its code repeats one as a literal, except 0, 1, -1 and the empty text.
tags: [decisions, checks, catalogue, codegen]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/009-no-hard-coded-values/plan.md
    title: Feature 009 plan, Decisions
  - id: research
    resource: ../../specs/009-no-hard-coded-values/research.md
    title: Feature 009 research, E1, R1 and R2
  - id: spec
    resource: ../../specs/009-no-hard-coded-values/spec.md
    title: Feature 009 specification, Clarifications
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-23T20:00:00Z }
---

# Origin

The plan of [feature 009](../../specs/009-no-hard-coded-values/plan.md) (need N-059, owner
2026-11-23); the owner chose the four exempt values and that only the contract declares values.

Sources: [Feature 009 plan, Decisions](../../specs/009-no-hard-coded-values/plan.md); [Feature 009 specification, Clarifications](../../specs/009-no-hard-coded-values/spec.md).

# Context

- Agents hard-code what belongs in data; a limit copied from the contract into the code passes
  every test and silently disagrees once the contract changes.
- oapi-codegen generates enum constants but no constant for a length, range, size or default
  keyword.

Sources: [Feature 009 research, E1](../../specs/009-no-hard-coded-values/research.md).

# Decision

- One collector in `engine/checks` finds the declared values of the approved contract: the
  scalar values of `maxLength`, `minLength`, `maximum`, `minimum`, `exclusiveMaximum`,
  `exclusiveMinimum`, `maxItems`, `minItems`, `maxProperties`, `minProperties`, `multipleOf`,
  `default`, `const` and `enum` items, outside examples and extensions.
- The generate procedure writes `gen/declared.go` with one constant per value but the enum
  values, which keep oapi-codegen's constants.
- The code step's check DeclaredValuesNotRepeated rejects a literal equal by value to a declared
  value, naming the file, line, contract location and Go name; `0`, `1`, `-1` and `""` are never
  judged, nor values stated only in the rules.

Sources: [Feature 009 research, R1 and R2](../../specs/009-no-hard-coded-values/research.md).

# Consequences

- A contract change regenerates the values, and code that used them follows.
- A declared value that is one of the four exempt values, or that the code computes from several
  literals, is not caught (stated limits).
- To have a value checked, the developer declares it in the contract.

Sources: [Feature 009 plan](../../specs/009-no-hard-coded-values/plan.md).
