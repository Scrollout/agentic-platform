---
type: Decision
title: "ADR 0094: Any literal value of a slice may be a captured pattern's parameter"
description: Supersedes ADR 0092 in part. Besides names and declared values, the developer may add any other literal value of the source slice (a number or a string) as a parameter; a number is matched only as a whole number.
tags: [decisions, catalogue, captured-patterns]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/011-capture-pattern/plan.md
    title: Feature 011 plan, Decisions
  - id: research
    resource: ../../specs/011-capture-pattern/research.md
    title: Feature 011 research, E1 and R4
  - id: spec
    resource: ../../specs/011-capture-pattern/spec.md
    title: Feature 011 specification, Clarifications
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-25T23:30:00Z }
---

# Origin

The project owner's answer of 2026-11-25 to a question found while planning
([specification](../../specs/011-capture-pattern/spec.md), Clarifications), recorded in the
[feature 011 plan](../../specs/011-capture-pattern/plan.md).

Sources: [Feature 011 specification, Clarifications](../../specs/011-capture-pattern/spec.md).

# Context

- A slice's examples hold values derived from a declared value (a text of 201 characters for a
  limit of 200). Under [ADR 0092](/decisions/0092-captured-pattern-fit-is-exact-substitution.md)
  they are not parameters, so a slice with another limit never fits (research E1).

Sources: [Feature 011 research, E1](../../specs/011-capture-pattern/research.md).

# Decision

- The developer may add any literal value that occurs in the source slice (an integer, a decimal,
  or a string of letters, digits, `_`, `-` and spaces) as its own parameter, besides the proposed
  names and declared values.
- A number, declared or literal, is matched only as a whole number: not next to a digit, or to a
  `.` followed by a digit.
- Everything else in ADR 0092 holds.

Sources: [Feature 011 research, R4](../../specs/011-capture-pattern/research.md).

# Consequences

- A slice with another limit fits when each derived value is its own parameter; the platform does
  not compute one value from another.
- A literal that occurs in many places (a small number) is replaced in all of them; the proposal
  lists every place, and the proof judges the result.

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).
