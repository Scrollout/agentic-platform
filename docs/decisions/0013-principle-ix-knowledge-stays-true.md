---
type: Decision
title: "ADR 0013: Principle IX - Knowledge Stays True"
description: Why the constitution adopts Principle IX, 'Knowledge Stays True'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: docs-as-code
    resource: https://www.writethedocs.org/guide/docs-as-code/
    title: "Docs as Code"
  - id: documenting-architecture-decisions
    resource: https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions
    title: "Documenting Architecture Decisions"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Origin

The project owner ratified the constitution, with Principle IX, "Knowledge Stays True", on
2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into this record,
without changing any rule.

# Context

Specs describe what is being built now; the knowledge bundle in `docs/` is what stays true
afterwards, and it is only useful while it is accurate (Docs as Code; Documenting
Architecture Decisions).

Sources: [Docs as Code](https://www.writethedocs.org/guide/docs-as-code/); [Documenting Architecture Decisions](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions).

# Decision

Adopt Principle IX, "Knowledge Stays True", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).
