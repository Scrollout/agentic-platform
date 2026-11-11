---
type: Decision
title: "ADR 0018: Principle XIV - Safe Defaults and Least Agency"
description: Why the constitution adopts Principle XIV, 'Safe Defaults and Least Agency'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: owasp-ai-agent
    resource: https://cheatsheetseries.owasp.org/cheatsheets/AI_Agent_Security_Cheat_Sheet.html
    title: "OWASP AI Agent Security"
  - id: owasp-llm06-excessive
    resource: https://genai.owasp.org/llmrisk/llm062025-excessive-agency/
    title: "OWASP LLM06 Excessive Agency"
  - id: mcp-security-best
    resource: https://modelcontextprotocol.io/specification/2025-11-25/basic/security_best_practices
    title: "MCP Security Best Practices"
  - id: cisa-secure-by
    resource: https://www.cisa.gov/securebydesign
    title: "CISA Secure by Design"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Origin

The project owner ratified the constitution, with Principle XIV, "Safe Defaults and Least Agency",
on 2026-11-11. Constitution v1.4.0 moved this rationale out of the constitution into this record,
without changing any rule.

# Context

Excessive agency and prompt injection are the defining risks of agent systems, and defaults decide
what an operator gets before configuring anything (OWASP AI Agent Security; OWASP
LLM06 Excessive Agency; MCP Security Best Practices;
CISA Secure by Design).

Sources: [OWASP AI Agent Security](https://cheatsheetseries.owasp.org/cheatsheets/AI_Agent_Security_Cheat_Sheet.html); [OWASP LLM06 Excessive Agency](https://genai.owasp.org/llmrisk/llm062025-excessive-agency/); [MCP Security Best Practices](https://modelcontextprotocol.io/specification/2025-11-25/basic/security_best_practices); [CISA Secure by Design](https://www.cisa.gov/securebydesign).

# Decision

Adopt Principle XIV, "Safe Defaults and Least Agency", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).
