---
type: Decision
title: "ADR 0009: Principle V - Zero-Trust Identity and Separate Permissions"
description: Why the constitution adopts Principle V, 'Zero-Trust Identity and Separate Permissions'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: nist-sp-800
    resource: https://csrc.nist.gov/pubs/sp/800/207/final
    title: "NIST SP 800-207"
  - id: spiffe
    resource: https://spiffe.io/docs/latest/spiffe-about/overview/
    title: "SPIFFE"
  - id: rfc-8693
    resource: https://www.rfc-editor.org/rfc/rfc8693
    title: "RFC 8693"
  - id: mcp-authorization
    resource: https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
    title: "MCP Authorization"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T18:45:00Z }
---

# Origin

The project owner ratified the constitution, with Principle V, "Zero-Trust Identity and Separate
Permissions", on 2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into
this record, without changing any rule.

# Context

Least privilege per component, per person, and per agent; an agent with borrowed credentials is a
confused deputy waiting to happen (NIST SP 800-207[^nist-sp-800]; SPIFFE[^spiffe]; RFC
8693[^rfc-8693]; MCP Authorization[^mcp-authorization]).

# Decision

Adopt Principle V, "Zero-Trust Identity and Separate Permissions", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).

[^nist-sp-800]: NIST SP 800-207
[^spiffe]: SPIFFE
[^rfc-8693]: RFC 8693
[^mcp-authorization]: MCP Authorization
