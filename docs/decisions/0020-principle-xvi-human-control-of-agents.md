---
type: Decision
title: "ADR 0020: Principle XVI - Human Control of Agents"
description: Why the constitution adopts Principle XVI, 'Human Control of Agents'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: linux-kernel-ai
    resource: https://docs.kernel.org/process/coding-assistants.html
    title: "Linux kernel: AI Coding Assistants"
  - id: openssf-guide-for
    resource: https://best.openssf.org/Security-Focused-Guide-for-AI-Code-Assistant-Instructions
    title: "OpenSSF guide for AI code assistants"
  - id: github-protected-branches
    resource: https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches
    title: "GitHub protected branches"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T18:45:00Z }
---

# Origin

The project owner ratified the constitution, with Principle XVI, "Human Control of Agents", on
2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into this record,
without changing any rule.

# Context

Agents write most of this project, act fast and at scale, and some of their actions can weaken the
project's safeguards (Linux kernel: AI Coding Assistants[^linux-kernel-ai]; OpenSSF guide for AI
code assistants[^openssf-guide-for]; GitHub protected branches[^github-protected-branches]).

# Decision

Adopt Principle XVI, "Human Control of Agents", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).

[^linux-kernel-ai]: Linux kernel: AI Coding Assistants
[^openssf-guide-for]: OpenSSF guide for AI code assistants
[^github-protected-branches]: GitHub protected branches
