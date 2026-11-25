---
type: Decision
title: "ADR 0095: Captured patterns never change, and parameter values keep their kind"
description: Refined by ADR 0099 (a proof compares only the filled outputs). Refined by ADR 0097 (the copy is built as the captured pattern's proof slice). Supersedes ADR 0091, ADR 0092 and ADR 0093 in part. An approved captured pattern never changes (version always 1.0.0; a change is a new capture); a value keeps its parameter's kind and is refused at the fit otherwise; a name has at least two letters; a proof builds from the captured pattern's copy of the slice and compares generated files too; a later failing proof makes it unusable.
tags: [decisions, catalogue, captured-patterns]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/011-capture-pattern/plan.md
    title: Feature 011 plan, Decisions
  - id: research
    resource: ../../specs/011-capture-pattern/research.md
    title: Feature 011 research, R3, R4 and R5
  - id: spec
    resource: ../../specs/011-capture-pattern/spec.md
    title: Feature 011 specification, Clarifications (analyze round 1)
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-26T03:00:00Z }
---

# Origin

The project owner's answers of 2026-11-25 to `/speckit-analyze` round 1 of feature 011
([specification](../../specs/011-capture-pattern/spec.md), Clarifications), recorded in the
[feature 011 plan](../../specs/011-capture-pattern/plan.md).

Sources: [Feature 011 specification, Clarifications](../../specs/011-capture-pattern/spec.md).

# Context

> Refined by [ADR 0099](/decisions/0099-proof-compares-filled-outputs-and-paths-are-filled.md):
> a proof compares only the filled outputs (tests, code, witnesses); generated code is judged by
> its checks. The run, not the start's answer, names why a captured pattern was not used.

- No operation made a new version of an approved captured pattern (ADR 0091).
- A value outside its form, refused at the fit, left the run's start silent about why the base
  pattern ran; a number slot could take a string (ADR 0092).
- A name such as `a1` has equal capitalised and upper forms, so a fill could not tell them apart.
- A proof "of the source slice" could not run after the slice was erased, and a later failing
  proof under the same method left the captured pattern usable (ADR 0093).

Sources: [Feature 011 research, R3 to R5](../../specs/011-capture-pattern/research.md).

# Decision

- An approved captured pattern never changes; its version is always `1.0.0`; to change it, the
  developer captures the run again under another name and withdraws the old one (owner).
- A parameter's kind (`name`, `number`, `string`) fixes the form of every value it takes; values
  are at most 64 characters; a name has at least two letters. A value outside its kind, or two
  parameters given equal values, make the slice not fit, and the start's answer names the
  captured pattern, the parameter and why (owner).
- Every occurrence of a name's three case forms is replaced, inside longer words too.
- A proof runs from the captured pattern's copy of the source slice and passes when the run ends
  done and the generated code, tests, code and witnesses equal the source run's (owner).
- Usable means approved and the latest proof passed under the current method.
- One operation at a time per captured pattern.

Sources: [Feature 011 research, R3 to R5](../../specs/011-capture-pattern/research.md).

# Consequences

- Fewer captured-pattern states; a change is a visible new capture.
- A slice with an odd value is built by the base pattern, and the developer is told why.

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).
