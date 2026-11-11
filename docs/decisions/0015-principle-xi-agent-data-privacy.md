---
type: Decision
title: "ADR 0015: Principle XI - Agent Data Privacy"
description: Why the constitution adopts Principle XI, 'Agent Data Privacy'.
tags: [decisions, constitution]
status: draft
decision: accepted
sources:
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution v1.4.0
  - id: opentelemetry-genai-conventions
    resource: https://github.com/open-telemetry/semantic-conventions-genai
    title: "OpenTelemetry GenAI conventions"
  - id: gdpr
    resource: https://eur-lex.europa.eu/eli/reg/2016/679/oj
    title: "GDPR"
  - id: edpb-opinion-28
    resource: https://www.edpb.europa.eu/our-work-tools/our-documents/opinion-board-art-64/opinion-282024-certain-data-protection-aspects_en
    title: "EDPB Opinion 28/2024"
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-11T20:40:00Z }
---

# Origin

The project owner ratified the constitution, with Principle XI, "Agent Data Privacy", on 2026-11-11.
Constitution v1.4.0 moved this rationale out of the constitution into this record, without changing
any rule.

# Context

What users tell agents is the most sensitive data the platform holds, and it is personal data under
laws such as the GDPR (OpenTelemetry GenAI conventions;
GDPR; EDPB Opinion 28/2024).

Sources: [OpenTelemetry GenAI conventions](https://github.com/open-telemetry/semantic-conventions-genai); [GDPR](https://eur-lex.europa.eu/eli/reg/2016/679/oj); [EDPB Opinion 28/2024](https://www.edpb.europa.eu/our-work-tools/our-documents/opinion-board-art-64/opinion-282024-certain-data-protection-aspects_en).

# Decision

Adopt Principle XI, "Agent Data Privacy", exactly as the
[constitution](../../.specify/memory/constitution.md) states it. The constitution holds the rules;
this record holds why they exist and what backs them.

# Consequences

- Changing the principle needs a new decision that supersedes this one, approved by the project
  owner (constitution, Governance).
