---
type: Decision
title: "ADR 0016: Principle XII - Tenant Isolation"
description: Why the constitution adopts Principle XII, 'Tenant Isolation'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: owasp-multi-tenant
    resource: https://cheatsheetseries.owasp.org/cheatsheets/Multi_Tenant_Security_Cheat_Sheet.html
    title: "OWASP Multi-Tenant Security"
  - id: aws-saas-tenant
    resource: https://docs.aws.amazon.com/whitepapers/latest/saas-tenant-isolation-strategies/saas-tenant-isolation-strategies.html
    title: "AWS SaaS tenant isolation"
  - id: owasp-top-10
    resource: https://genai.owasp.org/llm-top-10/
    title: "OWASP Top 10 for LLM Applications"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T18:45:00Z }
---

# Origin

The project owner ratified the constitution, with Principle XII, "Tenant Isolation", on 2026-11-11.
Constitution v1.4.0 moved this rationale out of the constitution into this record, without changing
any rule.

# Context

One installation serves several organizations, and agents add new paths for one tenant's data to
reach another (OWASP Multi-Tenant Security[^owasp-multi-tenant]; AWS SaaS tenant
isolation[^aws-saas-tenant]; OWASP Top 10 for LLM Applications[^owasp-top-10]).

# Decision

Adopt Principle XII, "Tenant Isolation", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).

[^owasp-multi-tenant]: OWASP Multi-Tenant Security
[^aws-saas-tenant]: AWS SaaS tenant isolation
[^owasp-top-10]: OWASP Top 10 for LLM Applications
