---
type: Decision
title: "ADR 0007: Principle III - API-First, Declarative Management"
description: Why the constitution adopts Principle III, 'API-First, Declarative Management'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: aip-128
    resource: https://google.aip.dev/128
    title: "AIP-128"
  - id: kubernetes-controllers
    resource: https://kubernetes.io/docs/concepts/architecture/controller/
    title: "Kubernetes controllers"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T18:45:00Z }
---

# Origin

The project owner ratified the constitution, with Principle III, "API-First, Declarative
Management", on 2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into
this record, without changing any rule.

# Context

One management path means no interface can skip validation, authorization, or audit; pinned
revisions make every run reproducible and auditable (AIP-128[^aip-128]; Kubernetes
controllers[^kubernetes-controllers]).

# Decision

Adopt Principle III, "API-First, Declarative Management", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).

[^aip-128]: AIP-128
[^kubernetes-controllers]: Kubernetes controllers
