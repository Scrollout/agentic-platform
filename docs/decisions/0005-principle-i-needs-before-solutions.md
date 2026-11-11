---
type: Decision
title: "ADR 0005: Principle I - Needs Before Solutions"
description: Why the constitution adopts Principle I, 'Needs Before Solutions'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: spec-driven-development
    resource: https://github.com/github/spec-kit/blob/main/spec-driven.md
    title: "Spec-Driven Development"
  - id: nist-sp-800
    resource: https://csrc.nist.gov/pubs/sp/800/218/final
    title: "NIST SP 800-218 PO.1"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T18:45:00Z }
---

# Origin

The project owner ratified the constitution, with Principle I, "Needs Before Solutions", on
2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into this record,
without changing any rule.

# Context

A choice copied from elsewhere carries that context's constraints with it. Spec-driven development
starts from what users need and why (Spec-Driven Development[^spec-driven-development]; NIST SP
800-218 PO.1[^nist-sp-800]).

# Decision

Adopt Principle I, "Needs Before Solutions", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).

[^spec-driven-development]: Spec-Driven Development
[^nist-sp-800]: NIST SP 800-218 PO.1
