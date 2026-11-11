---
type: Decision
title: "ADR 0011: Principle VII - Pluggable Providers"
description: Why the constitution adopts Principle VII, 'Pluggable Providers'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: azure-resource-providers
    resource: https://learn.microsoft.com/en-us/azure/azure-resource-manager/management/resource-providers-and-types
    title: "Azure resource providers"
  - id: mcp-specification
    resource: https://modelcontextprotocol.io/specification/2025-11-25
    title: "MCP specification"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T18:45:00Z }
---

# Origin

The project owner ratified the constitution, with Principle VII, "Pluggable Providers", on
2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into this record,
without changing any rule.

# Context

The best model or tool changes over time and per deployment, and one governed path keeps every
provider under the same rules (Azure resource providers[^azure-resource-providers]; MCP
specification[^mcp-specification]).

# Decision

Adopt Principle VII, "Pluggable Providers", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).

[^azure-resource-providers]: Azure resource providers
[^mcp-specification]: MCP specification
