---
type: Decision
title: "ADR 0099: A proof compares the filled outputs, and template paths are filled"
description: Refines ADR 0097 and ADR 0092. A proof passes when its tests, code and witnesses equal the captured template, while generated code is judged by its checks; a hand-over fails a proof at once; a parameter's case forms are filled into template file paths as well as contents.
tags: [decisions, catalogue, captured-patterns]
status: draft
decision: accepted
sources:
  - id: plan
    resource: ../../specs/011-capture-pattern/plan.md
    title: Feature 011 plan, Decisions
  - id: research
    resource: ../../specs/011-capture-pattern/research.md
    title: Feature 011 research, R4 and R5
  - id: spec
    resource: ../../specs/011-capture-pattern/spec.md
    title: Feature 011 specification, Clarifications (analyze round 5)
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-26T02:00:00Z }
---

# Origin

The project owner's answers of 2026-11-26 to `/speckit-analyze` round 5 of feature 011
([specification](../../specs/011-capture-pattern/spec.md), Clarifications), recorded in the
[feature 011 plan](../../specs/011-capture-pattern/plan.md).

Sources: [Feature 011 specification, Clarifications](../../specs/011-capture-pattern/spec.md).

# Context

- [ADR 0097](/decisions/0097-proof-builds-a-slice-made-from-the-captured-copy.md) compared a
  proof's generated code with hashes kept at capture; a method change that changes generated
  bytes would then fail every later proof.
- A failing check hands a run over, which then waits for the developer, so a proof judged only at
  the run's end stayed running.
- Parameter values also occur in file paths (`cmd/notes/main.go`), which filling only contents
  left unchanged.

Sources: [Feature 011 research, R4 and R5](../../specs/011-capture-pattern/research.md).

# Decision

- A proof passes when its run ends done and its `tests`, `code` and `witnesses` equal the
  captured template; generated code is judged by its checks, as in every build (owner).
- A hand-over fails a proof at once, naming the failing check.
- A parameter's case forms are filled into template paths as well as contents; two files with the
  same filled path, or a filled path outside its step's writable entries, do not fit (owner).

Sources: [Feature 011 research, R4 and R5](../../specs/011-capture-pattern/research.md).

# Consequences

- A captured pattern survives a generator change under a new proof.
- A slice for another resource gets its own folder names, not the source's.

Sources: [Feature 011 plan](../../specs/011-capture-pattern/plan.md).
