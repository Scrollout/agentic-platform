---
type: Decision
title: "ADR 0014: Principle X - No Silent Loss of Accepted Runs"
description: Why the constitution adopts Principle X, 'No Silent Loss of Accepted Runs'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: temporal-durable-ai
    resource: https://docs.temporal.io/ai
    title: "Temporal: Durable AI"
  - id: ai-agent-orchestration
    resource: https://learn.microsoft.com/en-us/azure/architecture/ai-ml/guide/ai-agent-design-patterns
    title: "AI agent orchestration patterns"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T18:45:00Z }
---

# Origin

The project owner ratified the constitution, with Principle X, "No Silent Loss of Accepted Runs", on
2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into this record,
without changing any rule.

# Context

An accepted run that vanishes is invisible to its caller, and an unbounded agent loop is the
agent-era form of silent failure (Temporal: Durable AI[^temporal-durable-ai]; AI agent orchestration
patterns[^ai-agent-orchestration]).

# Decision

Adopt Principle X, "No Silent Loss of Accepted Runs", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).

[^temporal-durable-ai]: Temporal: Durable AI
[^ai-agent-orchestration]: AI agent orchestration patterns
