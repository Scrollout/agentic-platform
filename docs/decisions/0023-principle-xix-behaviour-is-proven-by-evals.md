---
type: Decision
title: "ADR 0023: Principle XIX - Behaviour Is Proven by Evals"
description: Why the constitution adopts Principle XIX, 'Behaviour Is Proven by Evals'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: anthropic-demystifying-evals
    resource: https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents
    title: "Anthropic: Demystifying evals for AI agents"
  - id: openai-evaluation-best
    resource: https://platform.openai.com/docs/guides/evaluation-best-practices
    title: "OpenAI: Evaluation best practices"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T18:45:00Z }
---

# Origin

The project owner ratified the constitution, with Principle XIX, "Behaviour Is Proven by Evals", on
2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into this record,
without changing any rule.

# Context

Agent behaviour is non-deterministic, so tests alone cannot show that it still works; evals are the
acceptance tests of agent systems (Anthropic: Demystifying evals for AI
agents[^anthropic-demystifying-evals]; OpenAI: Evaluation best practices[^openai-evaluation-best]).

# Decision

Adopt Principle XIX, "Behaviour Is Proven by Evals", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).

[^anthropic-demystifying-evals]: Anthropic: Demystifying evals for AI agents
[^openai-evaluation-best]: OpenAI: Evaluation best practices
