---
type: Decision
title: "ADR 0021: Principle XVII - Declare, Derive, Converge"
description: Why the constitution adopts Principle XVII, 'Declare, Derive, Converge'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: kubernetes-controllers
    resource: https://kubernetes.io/docs/concepts/architecture/controller/
    title: "Kubernetes controllers"
  - id: terraform-plan
    resource: https://developer.hashicorp.com/terraform/cli/commands/plan
    title: "terraform plan"
  - id: the-twelve-factor
    resource: https://12factor.net/
    title: "The Twelve-Factor App"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Origin

The project owner ratified the constitution, with Principle XVII, "Declare, Derive, Converge", on
2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into this record,
without changing any rule.

# Context

A routine that depends on someone remembering its steps fails when they forget, and a fact kept in
two places drifts (Kubernetes controllers; terraform plan;
The Twelve-Factor App).

Sources: [Kubernetes controllers](https://kubernetes.io/docs/concepts/architecture/controller/); [terraform plan](https://developer.hashicorp.com/terraform/cli/commands/plan); [The Twelve-Factor App](https://12factor.net/).

# Decision

Adopt Principle XVII, "Declare, Derive, Converge", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).
