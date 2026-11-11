---
type: Decision
title: "ADR 0022: Principle XVIII - Abstractions Say What, Not How"
description: Why the constitution adopts Principle XVIII, 'Abstractions Say What, Not How'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: a-philosophy-of
    resource: https://web.stanford.edu/~ouster/cgi-bin/book.php
    title: "A Philosophy of Software Design"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T18:45:00Z }
---

# Origin

The project owner ratified the constitution, with Principle XVIII, "Abstractions Say What, Not How",
on 2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into this record,
without changing any rule.

# Context

An interface shaped by its implementation leaks every change of the how to every caller; deep
modules with small interfaces keep the system understandable (A Philosophy of Software
Design[^a-philosophy-of]).

# Decision

Adopt Principle XVIII, "Abstractions Say What, Not How", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).

[^a-philosophy-of]: A Philosophy of Software Design
